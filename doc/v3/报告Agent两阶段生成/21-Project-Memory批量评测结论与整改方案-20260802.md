# Project Memory 批量评测结论与整改方案

> 日期：2026-08-02  
> 状态：P0/P1 已完成测试服结构与代表样本重复回归，完整 A/B/C 盲评待执行  
> 适用范围：默认个人日报的历史项目辅助信息

## 一、结论

Project Memory 的夜间整理、快照和日报注入链路已能稳定运行，但当前版本对日报质量的净增益很小，不应直接作为默认方案发布。

30 个用户日、每组 3 次的对照评测共运行 180 次：基线成功 87/90，候选成功 90/90。候选链路没有降低生成成功率，但真正拿到 Project Memory Hint 的样本只有 12/30；这些样本的标题稳定性仅由 0.297 提升到 0.312。评测还发现 alias 被历史成果句污染、项目名过泛、无匹配时混入旧 Continuity Context 等问题。

本轮先修正记忆接口边界和容错，不用新增词表或针对单一样本补规则。修正后再进行三组盲评，达到门槛后才进入发布清单。

## 二、已验证事实

- 评测产物位于 `/home/intellif/evaluation/project-memory-ab-20260802/human-review-v1`；
- 180 次运行中，基线 3 次超时，候选无失败；
- 78 份 Project Memory Snapshot 覆盖 29 个用户；
- 解析决策共 415 条：`create_new` 262 条、`unresolved` 141 条、`link_existing` 12 条；
- 最新快照累计 136 个项目，平均每人 4.69 个，最高 13 个；
- 夜间整理平均输入约 783 tokens、输出约 228 tokens、耗时约 72.5 秒；
- 候选组中 12 个样本实际拿到 `project_memory_context`，13 个样本回退到 `continuity_context`，5 个样本没有历史提示；
- 正向样本能把多条当日事实归到 Symphony、Server Admin、GPU 等稳定主线；
- 负向样本出现 `Milestone`、`环境配置`、`调度策略`、`工单通知` 等过泛项目名；
- 当前实现把日报工作概览原句同时写入 occurrence 和 alias，导致完整历史成果句进入后续 Report Context。

## 三、问题定义

### 3.1 名称与事实混在一起

项目名和 alias 应表达“这件工作叫什么”，日报主题原句表达“当天做了什么”。当前两者同时写入 alias，长期运行会让记忆越来越像历史日报正文，增加历史结果被误写为当天成果的风险。

### 3.2 单个坏决策会放大成整批失败

Agent 给出一个不合格项目名时，当前校验会拒绝整份 proposal。项目记忆是辅助链路，单个低质量判断应当放弃，不应影响同批其他可靠判断，更不能影响用户生成日报。

### 3.3 两套历史上下文混用

有 Project Memory Hint 时使用结构化记忆；没有 Hint 时又回退到包含历史工作概览的 Continuity Context。这样候选组不是单一变量，也会继续把历史结果句暴露给 Report Agent。

### 3.4 评测覆盖与处理组不一致

30 个候选样本中只有 12 个实际使用 Project Memory。把全部样本平均后，无法准确回答 Project Memory 对“可关联工作”的真实增益。

## 四、确定的整改设计

### D1：Project Memory 只保存名称关系

- `canonical_name`：短且稳定的项目/产品/工作主线名称；
- `aliases`：同一对象的简称、旧称或明确子主题名称；
- 当日工作概览原句只进入 occurrence，不再自动成为 alias；
- Report Context 不注入历史 overview、日期、状态、指标和结论。

名称校验只做通用结构约束：长度、单行、非 Markdown 列表、非完整句标点。禁止为某个异常样本维护业务词黑名单。

### D2：不确定时单项放弃

- 低置信度 `link_existing` 继续降级为 `unresolved`；
- 不合格的 `create_new` 也降级为 `unresolved`；
- 不合格 alias 直接丢弃；
- 只有 JSON/Schema 损坏、未知引用、重复 `theme_ref` 等结构性错误才拒绝整份 proposal；
- Project Memory 失败不得阻断 Report Agent 主流程。

### D3：Report Agent 使用紧凑 Hint

默认系统 Report Agent 只接收：

- `project_ref`；
- `canonical_name`；
- 经过过滤的短 aliases；
- `matched_fact_refs`；
- 匹配置信度；
- “仅用于命名和归并，不是当天事实”的明确约束。

系统 Report Agent 不再自动回退到旧 Continuity Context。个人自定义 Agent 维持原有历史上下文行为，避免影响其他业务。

### D4：下一轮采用三组受控评测

- A：无历史上下文；
- B：仅旧 Continuity Context；
- C：仅 Project Memory Context；
- 处理集只选择确实能命中 Project Memory Hint 的用户日；
- 冻结相同 Session、Fact、模型和生成参数，每组至少重复 3 次；
- 人工盲评不展示组别和实现名称。

### D5：matched Fact 是锚点，不是项目全集

真实回归表明，同一 Project Memory Hint 只会高置信命中少量包含项目名或 alias 的当天 Fact。`matched_fact_refs` 的语义必须是“识别项目的锚点”，不能被 Agent 理解为该项目当天工作的完整清单。

Report Context 需要把这项要求表达成结构化契约，而不只是一段自然语言：`anchor_fact_refs` 表示锚点，`workstream_subject` 指定顶层项目名，`max_workstreams=1` 规定同一个 Hint 在 Brief 中最多形成一个主线。Agent 再只依据其他当天 Facts 的目标和语义判断是否属于同一项目。同一项目下的协议、产品、原型、开发、文档和验证是子成果，不应因此拆成多个工作概览条目。无法由当天 Facts 证明关联时保持独立；服务端不使用历史成果句或模糊相似度强制扩散。

## 五、验收门槛

必须同时满足：

1. 历史成果、状态、指标、日期泄漏为 0；
2. 当天主要工作遗漏率不高于无历史组；
3. 错误合并率不高于无历史组；
4. 同一项目被拆散的比例相对无历史组有明确改善；
5. 报告生成成功率不低于现有默认链路；
6. Project Memory 的任何失败都只能降级为“无历史提示”，不能导致日报失败。

## 六、开发与验证清单

- [x] 停止把当日主题原句写入 alias；
- [x] 增加短名称/alias 通用结构校验；
- [x] 无效 `create_new` 单项降级为 `unresolved`；
- [x] Historical Hint 移除 `recent_context`；
- [x] 默认系统 Report Agent 禁用旧 Continuity Context 回退；
- [x] 增加解析、降级和上下文开关单元测试；
- [x] 将 matched Fact 的锚点语义写入 Project Memory Context，并完成重复真实回归；
- [x] 未命中的最近有效项目以 `candidate_only` 进入 Context，且不得强制归并；
- [x] 同一短 alias 同时命中多个项目时保持未匹配；
- [x] Project Memory Agent 先归并父项目，再保留完整子名与安全短 alias；
- [ ] 在测试服使用冻结生产样本完成 A/B/C 重复评测；
- [ ] 人工盲评通过后，再决定是否进入生产发布清单。

## 七、非目标

- 不改变 Digest 和 Projection 的确定性事实链；
- 不让历史日报成为当天工作证据；
- 不引入向量库、知识图谱或全量 Session 历史检索；
- 不修改个人自定义 Agent 的 Skill 与 MCP 使用方式；
- 不在本轮根据单个用户或单个术语增加专用规则。

## 八、P0 测试服回归记录

测试服 API 以 `98ea65f + P0 working tree` 构建，相关包测试和 `go test ./...` 均通过，健康检查返回 `ok`。

使用冻结生产回放数据完成两条真实 Report Run：

1. 有 Project Memory 命中的 `case-016`：Context 仅包含 `Symphony` 的 canonical name、3 个短 alias、4 个 matched fact refs 和 confidence；不包含 `continuity_context` 与 `recent_context`。两次最终日报均未复制历史部署时间、历史状态或下一步计划；第一次将协议、产品方案和实时原型归并为一条 Symphony 主线，第二次仍拆为协议、原型和产品方案 3 条，说明历史泄漏已消除，但归并稳定性尚未达到质量验收门槛；
2. 无 Project Memory 命中的 `case-001`：Context 同时不包含 `project_memory_context` 与 `continuity_context`。最终日报只写 Codex CLI 旧版本定位和 zsh 环境清理两项当天工作，未带入历史主题。

本次回归证明 P0 的接口边界和降级路径生效，不替代第 D4 节的完整 A/B/C 重复盲评，也不形成生产发布结论。

### P1 锚点契约重复回归

仅使用自然语言说明“锚点不是完整清单”时，同一 `case-016` 的两次运行分别生成 1 条和 3 条 Symphony 主线，说明 Agent 已识别同一项目，但自然语言约束不能稳定控制 Brief 粒度。

改为结构化 `anchor_fact_refs + workstream_subject + max_workstreams` 后，同一冻结 Context 连续 3 次均生成 1 条 Symphony 主线，协议、实时原型和首个 Demo 产品方案保留为 3 个子成果，没有复制历史状态：

1. `Symphony：任务分发协议设计深化与实时原型演示`；
2. `Symphony 任务编排：收敛任务分发协议设计，落地可真实执行 Agent 的看板原型`；
3. `Symphony：确定任务分发协议核心定义，交付实时原型与首个 Demo 产品方案`。

其中第一次因 Agent 连续遗漏 Brief JSON 最外层闭合符而走降级写入，最终日报仍成功；其余两次正常通过 Brief。该问题与项目归并无关，但会造成无意义重试，因此 MCP 输入适配层增加了有界 JSON 尾部修复：只补齐缺失的 `}`/`]`，不修改字符串、字段、逗号和值，修复后仍执行完整 Brief 语义校验。

### P2 刘乐冻结生产切片回归

使用生产用户 21 在 2026-07-30、2026-07-31 的最终日报，以及 2026-07-31 原始 Report Run 选择的 8 个 Session 切片进行测试服回放。Session 文件严格截断到原 Run 的 `end_cursor`，生产环境只读。

首次对照揭示了真实断点：夜间 Agent 已把历史主题归并为“芯片验证平台”和“算力日志”，但没有保存可跨日匹配的短 alias，因此候选 Report Context 中不存在 `project_memory_context`；基线首次 Agent Session 卡住，重试成功。仅注入两个无锚点项目名后，Report Agent 仍输出“使用手册 / 测试执行模块”，证明候选名称本身不足以形成可靠关联。

整改后采用以下通用边界：

1. Project Memory Agent 先比较全部当日主题并归并稳定父项目；
2. alias 只允许稳定工作对象或明确子能力，禁止脚本、组件实现、部署、指标和成果句；父项目加子能力时同时保留完整子名与不少于 3 个字符的字面短名；
3. 无确定 Fact 锚点的最近有效项目只作为 `candidate_only` 名称词表；
4. 同一短 alias 同时属于多个项目时，服务端判为未匹配，不任选项目；
5. Report Skill 明确区分锚点 Hint 与可选 Candidate，历史信息仍不是当天事实。

最终 Memory 快照包含“芯片验证平台”和“算力日志”；“芯片验证平台”保留 `芯片验证平台使用手册`、`使用手册` 两个安全 alias。7 月 31 日 Context 通过 `使用手册` 命中 20 个当天 Fact 锚点，连续 3 次 Report Run 均成功收敛为一条“芯片验证平台”主线：

1. `芯片验证平台：完成使用手册整体收尾与测试执行模块改造方案设计`；
2. `芯片验证平台：完成测试执行模块改造设计，收尾使用手册改造`；
3. `芯片验证平台：使用手册改版收尾与测试执行模块改造方案定稿`。

三份日报均未复制 7 月 30 日历史成果，也未误用无锚点的“算力日志”。第 1、3 份保留“恢复本地开发环境”次要事项，第 2 份未保留，属于编辑选择波动，不是项目关联错误。完整产物位于 `/home/intellif/evaluation/liule-project-memory-20260802/final-candidate-v4/runs.json`。

为满足 Report Skill 既有 11,000 字符上限，Project Memory 使用契约随后压缩为等价短句并发布测试版 `aida-report@1.1.28`。烟测 Run `3ca461ae-cd77-44cc-b352-e4b74bcb6330` 明确加载 Agent Version 3 和 Skill `1.1.28`，成功生成 `芯片验证平台：使用手册改造收尾，完成测试执行模块改造方案设计`，精简未改变项目归并结果。
