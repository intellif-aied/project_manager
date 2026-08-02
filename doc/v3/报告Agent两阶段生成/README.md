# Report Agent 两阶段生成

> 日期：2026-07-27
> 状态：两阶段生成已恢复；主题契约开发与重复盲评已完成，工程上可合并，尚未进入本轮生产发布清单
> 首期范围：默认 Report Agent 生成个人日报 `personal_daily`

## 核心目标

保持 `Session Digest → Report Projection → Report Context` 的确定性证据链不变，让同一个 Report Agent 在同一个 Agent Session 内完成两次语义处理：

1. 根据完整 Report Context 生成结构化 Report Brief；
2. 只根据 Aida 校验并返回的 Report Brief 生成最终日报。

系统仍然只有一个默认 Report Agent、一份系统 Report Skill 和一个 Report MCP。Report MCP 新增 `write_report_brief`；首期个人日报的 `write_report_result` 增加 Brief 门禁。

## 已验证问题与实验结论

- 代表性长 Session 的 Projection 仍包含 45 条 Evidence Facts；现有单次生成同时承担归并、降噪、状态判断和写作，结果容易冗长或混淆交付状态。
- 测试服使用 `deepseek-v4-flash` 完成多轮两阶段实验后，45 条事实可归并为 2～4 个 Workstreams，并排除准备、讨论、重复和轨迹信息。
- 两阶段能显著改善主题归并和敏感信息清理，但必须在 Brief 中按 Deliverable 独立保存 `state`、`environment` 和 `next_action`，否则测试服验证会被误写成生产发布。
- “报表”“通知深链”“报告Agent”等不自然表达来自上游 Brief 直接保留技术词，正式 Skill 必须使用本目录统一语言。

## 文档导航

- [领域术语](CONTEXT.md)
- [需求文档](01-需求文档.md)
- [架构设计](02-架构设计.md)
- [开发方案](03-开发方案.md)
- [测试与验收](04-测试与验收.md)
- [设计 Review](05-设计Review.md)
- [实验记录](06-两阶段实验记录-20260727.md)
- [批量真实 Session 测试](07-批量真实Session测试-20260727.md)
- [批量测试日报样本](08-批量测试日报样本-20260727.md)
- [Agent 有界重试策略](09-Agent有界重试策略.md)
- [Agent 有界重试同批对比测试](10-Agent有界重试对比测试-20260727.md)
- [Agent 有界重试批量日报样本](11-Agent有界重试批量日报样本-20260727.md)
- [生产空参数故障与 Run 身份绑定修复](12-生产空参数故障与Run身份绑定修复-20260728.md)
- [工作主线关联改动方案](13-工作主线关联改动方案.md)
- [日报主题显著性与编辑选择方案](14-日报主题显著性与编辑选择方案.md)
- [日报主题契约与盲评结论](15-日报主题契约与盲评结论-20260730.md)
- [选择性 Brief 与自动 Fact 归档方案](16-选择性Brief与自动Fact归档方案-20260731.md)
- [Report Brief 主标题驱动 Summary 方案](17-Report-Brief主标题驱动Summary方案-20260801.md)
- [最近三份日报连续主题上下文方案](18-最近三份日报连续主题上下文方案-20260801.md)
- [Project Memory 轻量项目记忆与影子解析方案](19-Project-Memory轻量项目记忆与影子解析方案-20260801.md)
- [夜间 Project Memory 整理与日报历史参考方案](20-夜间Project-Memory整理与日报历史参考方案-20260802.md)
- [Project Memory 批量评测结论与整改方案](21-Project-Memory批量评测结论与整改方案-20260802.md)

## 强制边界

- Digest 和 Report Projection 不接入 LLM；
- 不创建第二个 Skill、MCP Server、Agent Session 或 Report Run；
- 不改变专用模型账号、当前用户 Report MCP 身份、报告权限和 Session 来源；
- 首期只强制默认个人日报，其他五类报告维持当前行为；
- Report Brief 只能组织已有事实，不能成为新的事实来源。
