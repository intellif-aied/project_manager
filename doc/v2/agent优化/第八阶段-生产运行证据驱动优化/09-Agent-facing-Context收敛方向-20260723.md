# Agent-facing Context 收敛方向（2026-07-23）

> 状态：分页、列式和成果检查点筛选均已取消；“有效事实完整覆盖 + 确定性结构去噪”的自动化和 31 份生产样本离线重放已完成；本轮不进入 Agent 验证
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

### 2.4 完整 Digest 与报告事实分层

原始 Session、完整 Digest WorkUnit 和冻结来源保持不变。Report Projection 不把完整 Agent 回复原样复制到日报 Context，而是对每个包含有效报告事实的 Work Unit 做确定性结构去噪：删除已知代码围栏、纯命令、纯路径、纯 Git 流水、传输包装和内部交接/执行过程话术；Markdown 标题与表格分隔语法规范化但表格事实保留；完整保留成果、验证、失败、风险和未解决事项，不按字符数截断。扁平文本围栏按出现顺序成对解析，只删除已知代码或序列化语言正文；围栏前后正文继续读取，`text/plaintext/txt` 和未知类型正文保留，未闭合围栏不截断后文。Git 只删除可独立识别的纯操作片段，混合业务结果无法安全拆分时整段保留。`status`、`category` 和时间先后只参与排序与表达，不作为删除整个 Work Unit 的依据；相互矛盾的业务结论均按时间保留。完全相同事实仅在日期、类别和状态也相同时聚合为首次/最新时间与 `occurrence_count`；不同类别或状态分开保留，不做近似去重。该规则只作用于报告表示，不修改 Digest Revision，完整正文仍可追溯。

Projection 的完整性不是 Digest 原文逐字复制：代码、工具过程和内部流水可以删除；但任何会改变“做了什么、结果如何、如何验证、是否失败或阻塞、还有什么未解决”的正文都必须保留。技术细节章节的噪声被删除后，解析必须继续读取其后的结果、验证和风险章节，禁止遇到详情标题后直接截断整条回复。

### 2.5 Digest 和 Projection 禁止新增 LLM

本期 Digest 与 Report Projection 的外部模型调用数固定为 0。不得新增模型 Client、模型配置、Prompt、模型重试、LLM Processing Job 或独立队列。语义 Workstream 归并继续由最终 Report Agent 完成，整个报告链路只保留现有最终 Agent 这一处模型执行。

### 2.6 Projection 是确定性内部模块

Projection 位于现有 Report Context Builder 内部，固定执行：冻结 Payload → 删除完全相同的 Digest 双写 → 校验每个 Highlight 的内部 Session 来源属于冻结 Item → 对所有状态执行相同的段落级结构去噪 → 保留全部有效结果和未解决项 → 按日期、`observed_at`、sequence 和事实身份稳定排序 → 构建普通 `facts[]` → 按 `kind + text + source` 精确归并 → 按日期+类别+状态聚合重复 observation 的首次/最新时间和出现次数 → 冻结单份 Context。内部 `source_ref` 不进入 Agent Context。

Projection 不做主题归并、不改变 Digest Revision，不创建新表、Job、Worker 或队列。语义主题归并仍由最终 Report Agent 完成；Projection 只执行上述固定报告事实规则。失败时 Run 明确失败，不回退超大 Context。

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
2. 再统一报告事实表达：每条结果在段落级结构去噪后完整保留成果、验证、失败、风险和未解决事项，不按字符数截断，不做主题判断、状态筛选或近似匹配；不把代码围栏、纯命令、纯路径、纯 Git、原始 Goal、内部引用和交接 Prompt 当成报告事实；
3. 若单个 Digest 在确定性 Projection 后仍异常大，另行分析 Digest 内部确定性表达，不引入 LLM；
4. 最大样本一次读取和队列指标通过后，才进入真实 Agent 验收。

## 5. Context 收敛准入条件

只有同时满足以下条件，才能进入下一轮代码修改：

- 完整 Digest 保持可追溯，Agent-facing Projection 排除代码围栏、纯命令、纯路径、纯 Git 和内部交接，但完整保留剩余的多段成果、验证和风险；
- 每条保留事实仍能追溯到冻结来源；
- 最大生产样本一次 MCP 调用不超过执行引擎限制；
- 同输入、同模型的事实覆盖不下降；
- 精确输入 Token、耗时和成本有修改前后对照；
- Codex、Claude Code 上传、Token 统计、Selection、Context 权限和报告写回不受影响。
- Digest 与 Projection 外部模型调用数为 0，Processing Job 数量和类型不变；
- Digest 构建耗时、Claim 延迟和队列最老等待时间不得因本改动劣化。

## 6. 当前发布结论

第一版普通对象 Projection 只做完整正文精确去重。真实 Run `4ba163dc-c785-436b-9421-44b934e11da8` 仍返回 332,428 字符、372 条完整结果，GLM-5.2 处理约 7 分 30 秒后才调用写回；生成内容过度展开历史过程，且重复 Summary 触发写回拒绝，Run 最终超时。

第二版结果首段真实 Run `6987645c-...` 已成功写回：MCP 64,103 字符，模型输入 47,665 Token，总耗时 620.7 秒。它证明单次读取和重复 Summary 写回兼容已闭环，但 347 条事实仍导致模型长时间归并并生成过长日报。

第三版 Session+工作类别首末状态方案真实 Run `31808223-...` 冻结 85 条事实、22,596 bytes Context，MCP 正文 16,807 字符；配合 3,901 字符的 `aida-report@1.0.50`，约 401 秒完成写回，Summary 290 字、正文 3,430 字。多任务 Session 通过保留不同 Digest Category 降低中间成果遗漏风险。完整 Digest、数据库、队列、模型、前端、上传和 Token 统计均未修改。

后续 Run `bb3d10d0-...` 和生产样本审计证明，首末及成果检查点方案都会删除无法由规则判断正误的独立事实，因此两者均已撤销。240 字首句和 1000 字单条上限也不再使用。当前实现按段落删除确定性结构噪声，所有状态使用相同规则。15 个本机 Codex Session 共 45,598,852 raw bytes、30,181 events，Digest→Projection 结构验收 15/15 通过；309 条 ResultStatement 中 304 条有效结果进入 Projection，5 条确定性噪声过滤。31 份保存的生产 Context 中，8,713 条 ResultStatement 有 8,626 条有效结果进入 Projection，87 条过滤项为过程话术、指令确认或结构噪声；精确归并后的 `occurrence_count` 合计仍为 8,626。完整 Context 从 16,518,069 bytes 降到 1,401,694 bytes，缩小 91.51%，最大输出 208,787 bytes。逐条复核发现并修复了扁平围栏、Git 截断和 Git 分析统计误判三类段内业务事实丢失，终态业务误删为 0。完整 Digest 未修改；测试服两个真实 Run 已验证 Context 冻结和单次 MCP 读取，外部模型失败不作为本轮 Projection 结论。

真实正文仍出现少量分支、提交号、worktree 和 HEAD 信息，说明 Skill 禁止规则不能单独作为数据清洗。Projection 固定删除纯 Git 事实和可独立分离的 Git 片段；混合结果中只要同时存在功能、验证、部署、失败或交付清单，就保留业务句，禁止从“提交并推送”等关键词开始截断全部后文。无法安全拆分时允许少量 Git 痕迹随业务事实保留。该规则由确定性单元测试和生产样本逐条审计验收，不追加昂贵的模型回归。

测试服真实 Agent 验收通过前，不提交发布申请，不部署生产。

生产发布顺序固定为：发布并验证经批准的新 Skill → 将默认及依赖报告 Agent 切换到新 Skill → 部署 API。不得让仍写死旧路径的 `aida-report@1.0.16` 接收 `work_evidence` Context。
