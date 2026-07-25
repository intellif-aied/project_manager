# 第八阶段：生产运行证据驱动优化

> 状态：成果检查点筛选已撤销；Projection 已收敛为“有效事实完整覆盖 + 确定性结构去噪”，自动化和 31 份生产样本离线重放通过；本轮不进入 Agent 验证
> 日期：2026-07-23
> API 部署版本：`main@815938c` / `sha256:44347c49fab97e189d251cc8f84286c934063ec44ab330cf3a3c929ee1bcaf7e`

## 阶段目标

用生产真实运行记录还原报告 Agent 从 Aida Run 到 Agent Session、MCP 调用、模型调用和报告写回的完整链路，先确认问题发生位置，再实施最小、可验证的优化。

本阶段不把单个失败案例直接转化为通用规则。报告模型由 Agent 平台规定，Aida 不切换、降级或覆盖模型，也不以模型变化掩盖输入、Skill、MCP 或平台执行问题。

## 文档导航

| 文档 | 状态 | 用途 |
| --- | --- | --- |
| [01-需求文档.md](01-需求文档.md) | 已定稿 | 固定目标、证据口径、产品边界和验收标准 |
| [02-架构设计.md](02-架构设计.md) | 已定稿，代码已落地 | 固定数据链路、Agent-facing Context 和兼容决策 |
| [03-开发方案.md](03-开发方案.md) | D-01～D-09 已执行，D-07 已取消 | 固定无 LLM 的确定性 Projection、Summary、Profile、Prompt、Toolset 和 Skill 收敛 |
| [04-测试与验收.md](04-测试与验收.md) | Digest→Projection 自动化与 31 份真实载荷重放通过 | 固定事实完整度、确定性瘦身、现有链路回归与平台故障边界 |
| [05-生产数据分析执行单.md](05-生产数据分析执行单.md) | 首轮已执行 | 固定采样、取证、归因和输出格式 |
| [06-首轮生产基线-20260723.md](06-首轮生产基线-20260723.md) | 已完成首批取证 | 记录身份关联、规模分布、四个脱敏样本和当前可确认结论 |
| [07-Agent流程稳定性与一致性-20260723.md](07-Agent流程稳定性与一致性-20260723.md) | 已完成新流程全量分析 | 仅统计 `get_report_context` 流程，核对 Skill、工具顺序、用户追问、写回和失败日志完整性 |
| [08-报告内容组织与目标归并-20260723.md](08-报告内容组织与目标归并-20260723.md) | 已完成高频用户反馈定位 | 用 GPT-5.5 直接成功样本证明过度拆分来自扁平 Digest 和 Skill 归并规则，固定目标成果组织验收 |
| [09-Agent-facing-Context收敛方向-20260723.md](09-Agent-facing-Context收敛方向-20260723.md) | 决策已更新 | 删除分页和额外 LLM，使用确定性 Projection 收敛单次输入 |
| [10-presentation-profile主流设计调研.md](10-presentation-profile主流设计调研.md) | 已完成 | 用一手资料核对 Skill、Context 和结构化写回的设计边界 |
| [报告摘要与Presentation-Profile](报告摘要与Presentation-Profile/README.md) | 代码、自动化、测试 Skill `1.0.50` 与真实写回已通过 | 固定六类 Summary、Profile、持久化、前端和验收方案 |

## 当前交付边界

- 代码已合并 `main@3a6c3f5`，测试服 API 使用镜像 `sha256:bb8436d15590c21c6eb0060b0299be2a44ea182ae174eb952b74146b9ea7cc38`；
- 测试服使用 `100866/aida-report@1.0.50`，Registry 正文与 API 生成正文逐字节一致，真实 Session 已确认加载该版本；
- 真实 Run `4ba163dc-...` 已确认 Agent 只收到 `/aida-report + run_id`，完成 Skill、一次 `get_report_context` 和一次写回尝试；332,428 字符 Context 使模型处理约 7 分 30 秒，重复 Summary 写回被拒绝后 Run 超时；
- 结果首段版真实 Run `6987645c-...` 已成功写回，重复 Summary 兼容有效；但 MCP 仍有 64,103 字符、模型输入 47,665 Token、总耗时 620.7 秒，日报仍过度展开；
- 旧 Session+工作类别首末状态 Projection 的真实 Run `31808223-...` 冻结 85 条事实、22,596 bytes Context，MCP 正文 16,807 字符；Skill 正文 3,901 字符，模型在写回调用时输入 30,420 Token，约 401 秒完成，Summary 290 字、正文 3,430 字；该结果只作为历史性能基线；
- Run `bb3d10d0-...` 证明按时间或状态选择检查点会误删纠正和独立成果；当前代码不再按 `status`、`category` 或新旧顺序淘汰 Work Unit，只删除代码围栏、纯命令、纯路径、纯 Git、传输包装和内部过程话术，并保留其中附带的业务结论；
- 15 个本机 Codex Session 共 45,598,852 raw bytes、30,181 events，Digest→Projection 结构验收 15/15 通过；309 条 ResultStatement 中 304 条有效结果进入 Projection，5 条计划/Git/交接噪声过滤；
- 31 份保存的生产 Context 离线重放：8,713 条 Digest ResultStatement 中 8,626 条有效事实全部进入 Projection，87 条过滤内容为过程话术、指令确认或结构噪声，终态业务误删为 0；精确归并后为 2,101 facts、2,102 observations，`occurrence_count` 合计仍为 8,626；完整 Context 从 16,518,069 bytes 降到 1,401,694 bytes，缩小 91.51%，最大输出 208,787 bytes；
- 当前 Selection `1d1e4d73-...` 的 36 条真实 ResultStatement 全部进入 Projection，结果文本从 49,442 bytes 降到 24,179 bytes，缩小 51.10%，无状态筛选或结构误删；
- 扁平 Digest 中的 Markdown 围栏按有序边界处理：只删除已知代码正文，围栏前后、文本围栏、未知类型和未闭合围栏后文保留；真实报告仍可能保留少量无法与业务结果安全拆分的 Git 痕迹，Projection 删除纯 Git 和可独立分离的 Git 片段，但禁止从 Git 关键词处截断交付清单、验证或故障结论；
- 原始生产载荷保存到本机 Git 仓库外的权限受控缓存，每个新流程样本同时保留 Context、原始 JSONL 和结构化 Session，不提交 Git；
- 不修改 Agent Platform、不切换模型，不用外部平台故障掩盖 Aida 验收结果。

## 固定主链路

```text
Aida ai_run
  -> external_session_id
  -> Agent Platform Session
  -> Thread / Subagent Event
  -> MCP 与模型调用
  -> Agent 最终输出
  -> Aida 报告写回与 ai_run 最终状态
```

## 阶段完成条件

1. `run_id -> external_session_id -> 模型 Trace` 关联方式已用真实记录验证；
2. 已完成 30～50 个代表性 Session 的分层分析；
3. 每个问题都有运行标识、事件时间线、影响范围、根因层级和基线指标；
4. P0/P1/P2 清单已确认，且每项代码任务可追溯到需求和架构决策；
5. 本轮 Digest→Projection 同输入对照和既有后端链路回归全部通过；Agent 结果验收不属于本轮门禁；
6. 形成独立发布与回退清单后，才允许进入发布阶段。
