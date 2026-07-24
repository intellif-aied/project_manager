# Agent-facing Context 收敛方向（2026-07-23）

> 状态：分页方案已取消；Context 体积分析、Projection 实现和离线验证完成，测试服真实 Agent 写回未通过
> 范围：托管报告 Agent 的 get_report_context(run_id) 输入

## 1. 已确认问题

生产曾出现单次 get_report_context 工具结果达到 1,580,502 字符并超过执行引擎限制。现有最大冻结样本约 3.77MB。字符数不等于 Token 数，精确 Token 必须来自模型或 Langfuse usage，但该规模对日报生成已经异常。

超大 Context 即使通过分页完成传输，模型仍需在同一个会话中理解全部内容，因此分页不能减少总 Token，也不能保证事实归并准确。

## 2. 固定决策

### 2.1 保留一个读取接口

    Agent 启动只接收 run_id
      -> get_report_context(run_id)
      -> 一次获得适合报告生成的紧凑 Context
      -> Agent 生成并写回报告

不新增 get_report_evidence，不增加 Cursor、页码、读取状态、写回门禁或页大小配置。

### 2.2 不以分页掩盖总输入过大

分页会增加工具调用、顺序状态和 Prompt 约束，并使相关事实分散在不同页。它只规避单次结果上限，不解决总输入、compact、成本和理解质量，因此从本期架构及代码中删除。

### 2.3 不擅自控制发布版本

开发代码不填写环境实际 Skill 版本，运行时版本只取 MANAGED_AGENT_REPORT_SKILL_VERSION；本次没有不兼容 MCP 协议变化，MCP 保持 report-v1。Skill 正文变化所需的新补丁版本由正式发布决策确定，开发实现不得通过 Skill 主版本自动切换 Context 契约。

### 2.4 不静默裁剪事实

Digest 不替 Agent 判断用户内容价值。本期不使用 Top-K、固定主题数、字符硬截断或随机抽样制造“小 Context”。

### 2.5 Digest 和 Projection 禁止新增 LLM

本期 Digest 与 Report Projection 的外部模型调用数固定为 0。不得新增模型 Client、模型配置、Prompt、模型重试、LLM Processing Job 或独立队列。语义 Workstream 归并继续由最终 Report Agent 完成，整个报告链路只保留现有最终 Agent 这一处模型执行。

### 2.6 Projection 是确定性内部模块

Projection 位于现有 Report Context Builder 内部，固定执行：冻结 Payload → 删除完全相同的 Digest 双写 → 按完全相同目标文本组织结果事实 → 将事实、结果、未决项和来源编码为带列定义的行数组 → 日期、枚举和完全相同结果文本进入一基查找表 → 校验每条结果型 Work Unit、来源和证据引用均被保留 → 冻结单份紧凑 Context。

Projection 不判断重要性、不做主题归并、不删除独立事实、不改变 Digest Revision，不创建新表、Job、Worker 或队列。失败时 Run 明确失败，不回退超大 Context。

## 3. 当前代码处理结果

已删除未提交的以下实现：

- get_report_evidence MCP Tool；
- Agent-facing 分页、签名 Cursor 和续读顺序；
- report_context_read_state；
- REPORT_CONTEXT_NOT_FULLY_READ 写回门禁；
- REPORT_CONTEXT_PAGE_MAX_BYTES 及 Compose 配置；
- Skill 和 Prompt 中的分页循环；
- Skill 2.0.0、MCP report-v2 以及按 Skill 主版本切换 Context 的逻辑。

当前只保留 Skill 的全局内容组织规则：按真实目标和成果归并相关功能、文档、部署、验证、修复和调查；不按会话片段或小功能机械拆分。

## 4. 已完成的确定性 Projection

代码已落在 `api/internal/reportcontext/projection.go`，仅在 Run 创建时已冻结 `report_context_representation=work_evidence` 的新 Context 上执行。没有该标记的历史 Run 继续使用旧结构。生产冻结样本仍保持只读，离线对照输出以下可复算数据：

1. 完整 Context 总字节和精确输入 Token；
2. sessions、sources、source_reports、requirements、tasks、coverage 和元数据的字节占比；
3. 完整重复、字段级重复和相同事实多种表达的字节占比；
4. 每个 Session Digest 的字节、事实条数和时间范围；
5. 删除确定性重复后的体积与事实覆盖差异；
6. 同一输入和 Agent Platform 规定的同一模型下，记录成功、耗时、compact、成本和报告事实覆盖。
7. Digest Worker 当前平均/P95 构建耗时、Claim 延迟、队列数量和最老等待时间；
8. Projection 在最大样本上的 CPU、内存和耗时；
9. 确认 Digest 和 Projection 外部模型调用数为 0；
10. 确认没有新增 Processing Job、Worker、队列或重试。

分析结果必须区分：

- 单个 Digest 本身异常膨胀；
- 多个 Digest 正常但报告周期累计过多；
- Report Context 重复装载同一 Digest；
- 同一事实被 highlight、work unit、result、原始文本等多种结构重复表达；
- JSON/MCP 包装开销。

固定实现顺序：

1. 先消除 Report Context 中可证明的整块重复，不修改 Digest Job；
2. 再统一同一事实的重复结构表达：列名只出现一次，重复值进入查找表；保留全部结果型 Work Unit、状态、时间、来源和 `evidence_refs`，并保留非结果 Work Unit 覆盖计数；
3. 若单个 Digest 在确定性 Projection 后仍异常大，另行分析 Digest 内部确定性表达，不引入 LLM；
4. 最大样本一次读取和队列指标通过后，才进入真实 Agent 验收。

## 5. Context 收敛准入条件

只有同时满足以下条件，才能进入下一轮代码修改：

- 被删除的是可证明的重复表达或 Agent 不需要理解的存储结构，不是用户事实；
- 每条保留事实仍能追溯到冻结来源；
- 最大生产样本一次 MCP 调用不超过执行引擎限制；
- 同输入、同模型的事实覆盖不下降；
- 精确输入 Token、耗时和成本有修改前后对照；
- Codex、Claude Code 上传、Token 统计、Selection、Context 权限和报告写回不受影响。
- Digest 与 Projection 外部模型调用数为 0，Processing Job 数量和类型不变；
- Digest 构建耗时、Claim 延迟和队列最老等待时间不得因本改动劣化。

## 6. 当前发布结论

离线实现结果：31 份包含 Digest V2 的冻结样本全部投影成功。三个最大样本的 MCP 包装字符数为 381,583、267,676、187,850，均低于 500,000 字符硬上限；最大样本 Projection 基准约 20ms、4.6MB 分配。该结果只证明结构、容量和本地性能，不代替真实 Agent 内容质量验收。

代码和自动化门禁已完成并合并 `main`，测试服 API 与 `100866/aida-report@1.0.48` 已发布。新 Run 的紧凑 Context 为 894,223 bytes，37/37 来源冻结完成，真实 Session 已加载 Skill 并一次调用 `get_report_context(run_id)`。Agent Platform 在 596 秒执行后终止，最终报告写回仍未通过；Aida 已将该错误归一为报告超时。本期不修改平台、不切换模型，也不新增事件监控。

测试服真实 Agent 验收通过前，不提交发布申请，不部署生产。

生产发布顺序固定为：发布并验证经批准的新 Skill → 将默认及依赖报告 Agent 切换到新 Skill → 部署 API。不得让仍写死旧路径的 `aida-report@1.0.16` 接收 `work_evidence` Context。
