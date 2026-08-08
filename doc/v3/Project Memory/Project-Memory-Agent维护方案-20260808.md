# Project Memory Agent 维护方案

> 日期：2026-08-08
> 状态：开发方案
> 范围：内部 Project Memory、专用 Agent 增量维护、AI Signal 生命周期、日报只读参考

## 1. 定位

AIDA 已经具备较完整的需求/任务模块，但当前用户仍主要在飞书维护需求，AIDA 尚未成为日常权威来源。本期不接入 Requirement/Task，也不为其预先增加实现。

本期不新增“我的项目”、项目管理页面或另一套项目 CRUD。Project Memory 定位为内部推断层，帮助 Report Agent 理解跨日持续工作。

```text
Session / 已有 Workspace 信号 / 历史日报 → Project Memory Agent → 日报候选背景
```

Project Memory 最终通过 Report Context 注入 Agent，但它不是一段自由 Prompt，也不是当天成果证据。

## 2. 本期目标与边界

### 2.1 目标

1. 专用 Project Memory Agent 根据新增证据持续维护用户的项目记忆。
2. 用户修改后的日报和手写日报具有较高权重。
3. AI 可以自动整理项目名称、别名、稳定工作线索与 Workspace 关联。
4. Report Agent 只读取少量相关候选，不修改 Project Memory。
5. Memory 更新失败、缺失或不确定时不影响日报生成。

### 2.2 不做

- 不增加“我的项目”菜单或管理页面；
- 不让用户维护第二套项目数据；
- 不开发用户可见的版本管理；
- 不增加逐条 AI 建议确认流程；
- 不做通用 Persona、知识图谱或跨用户项目共享；
- 不让 Report Agent 或普通个人 Agent直接写 Project Memory；
- 不把历史项目作为当天工作归属的硬约束。
- 不补强 AIDA CLI Git 采集；沿用当前已有的 `repository_key`。
- 不接入 Requirement/Task，也不为其预先增加本期实现。

## 3. 职责划分

### 3.1 Project Memory Agent

负责语义判断和增量整理：

- 判断当天主题是已有项目、新项目、项目别名还是无法确定；
- 归纳稳定项目名称和简短别名；
- 从多日证据中提取模块、模型、产品、任务类型等工作线索；
- 根据 Workspace/Git 线索建立或调整候选关联；
- 发现重复项目、名称演化和错误吸附，并输出修正操作；
- 新证据不足时选择 `noop/unresolved`，而不是强行归类。

Agent 每次读取“当前有效 Memory + 本轮新增/修改证据”，输出结构化操作，不从零重写整份 Memory。

### 3.2 服务端

服务端不代替 Agent 做语义归类，只负责确定性边界：

- AIDA 用户隔离；
- 报告日期与历史可见范围；
- Evidence、Workspace 和来源引用；
- Schema、长度、数量和引用合法性校验；
- 非破坏性应用 Agent 操作；
- 当前有效 Snapshot 持久化；
- 超时、失败、重试和无 Memory 降级；
- 冻结每次 Report Run 实际读取的 Memory 候选。

现有 Snapshot 足够用于排查，不建设用户版本产品。项目合并采用软关联或重定向，不物理删除旧证据，便于 Agent 后续纠正。

### 3.3 Report Agent

- 只读 `project_memory_context`；
- 只把 Memory 用于项目命名和同项目工作归并；
- 当天 Session/Facts 是唯一成果来源；
- 当前事实冲突、出现新项目或证据不足时忽略 Memory；
- Git、Workspace、路径和仓库信息不进入日报正文。

### 3.4 用户

用户不直接维护 Project Memory。系统从现有自然行为获得高权重反馈：

- 用户手写日报的一级工作主题；
- 用户修改 AI 日报一级主题并保存；
- 用户明确关联的 Session。

关闭日报弹窗、查看但未保存、未修改 AI 稿均不视为用户确认。

## 4. 记忆内容与权重

Project Memory 当前有效内容包括：

- 稳定项目名称；
- 简短别名；
- 稳定工作线索 `workstream_cues`；
- Workspace/Git 候选关联；
- 首次和最近出现日期；
- 来源类型、覆盖次数和 Agent 置信度。

不保存到项目记忆：

- 某天的完成事项和下一步计划；
- 历史进度、数量和测试结果；
- 发布、验收、上线等外部状态结论；
- commit hash、命令、内部 ID；
- 成果句形式的长别名。

服务端和 Agent共同遵循以下权威顺序：

1. 用户手写或明显修改后的日报；
2. 用户主动关联的 Session；
3. 多日重复且一致的项目线索；
4. 单日未修改 AI 日报；
5. 已有 Git repository、Workspace、CWD 等机器信号；
6. Agent 单次语义推断。

权重不向普通用户展示。高权重信息仍只是历史项目身份参考，不能替代当天 Facts。

## 5. Project Memory Agent 增量流程

### 5.1 触发

- 每晚处理当天有日报、历史日报发生修改或已有 Session/Workspace Evidence 变化的用户；
- Session/Workspace 新证据只推进用户待处理水位，不在上传主链路调用模型；
- 历史日报修改后重新排队对应用户，从受影响日期开始按时间顺序重建 V2 投影；重建期间受影响日期之后的日报按无 Memory 运行；
- 冷启动读取最近 10～20 份有内容日报，但按日报日期逐份结算并生成 as-of Snapshot，不把整个窗口截断后误记为完成。

### 5.2 输入

- 当前有效 Project Memory 紧凑快照；
- 本轮新增或修改的日报主题；
- 用户编辑与未编辑 AI 稿的来源类型；
- 新增 Workspace/Git 证据；
- 服务端预召回的少量候选项目。

不把完整历史 Session 或所有日报全文无界注入 Agent。

### 5.3 输出

建议操作集：

- `create_project`：创建新的内部项目候选；
- `link_existing`：把当前主题关联到已有项目；
- `upsert_signal/retire_signal`：维护 AI alias 与 workstream cue；
- `link_workspace/unlink_workspace`：维护 Workspace 候选关系；
- `archive_project`：在证据充分时归档低权威 AI 项目；
- `unresolved/noop`：证据不足或无需变化。

每项操作必须引用输入中的 theme/evidence/workspace，不能凭空创建关联。高风险操作不直接删除历史对象，只改变当前有效投影。

### 5.4 纠错

下一轮 Agent 同时看到当前 Memory 和新证据，可以撤销低质量 AI 线索、解除错误 Workspace 关联或修正 soft merge。用户后续修改日报时，该修改作为更高权重证据参与下一次整理，不要求用户进入专门页面。

## 6. 日报产品交互

第一期不增加任何 Project Memory 页面，也不要求日报生成前确认项目。

日报编辑继续使用现有一级主题。用户修改一级主题并保存后，后台将其识别为高权重命名证据。这样纠错发生在用户原本就会使用的日报编辑流程里。

若后续观察到用户仍需要明确纠错，可以在日报一级主题旁增加极轻入口：

> 项目归属不准确？调整标题即可影响后续识别

该入口不展示 Project Memory、权重或 Agent Proposal，也不在本期强制实现。

## 7. Report Agent 读取

生成日报时，服务端根据报告日期、当天 Facts 和 Workspace Identity 召回最多 3 个候选并冻结为本次 Run 的只读快照：

```json
{
  "project_ref": "project-001",
  "canonical_name": "芯片验证平台",
  "source_authority": "human_edited_report",
  "aliases": ["验证平台"],
  "workstream_cues": ["批量测试", "用例筛选", "RTL"],
  "matched_fact_refs": ["fact-012"],
  "match_basis": ["repository", "workstream_cue"],
  "instruction": "仅用于项目命名和归并参考，不是当天成果证据；冲突或不确定时忽略。"
}
```

Repository/Workspace 精确命中只能提高候选排序，不能独立证明当天工作属于该项目。没有合适候选时自然退回当天 Facts。

## 8. 验收

1. 原始 remote、凭证、commit 和路径不进入 Report Context。
2. 用户修改日报一级标题后，后续 Memory 优先吸收该命名。
3. Agent 可持续维护 AI 工作线索，并能随新证据纠正旧线索。
4. 同仓库出现新项目时，不因已有 `repository_key` 相同而强制归入旧项目。
5. Project Memory Agent 失败或无候选时，不影响 Session 上传和日报生成。
6. Report Agent 不能把历史成果、进度或外部状态写入当天日报。

## 9. 当前实现评审

当前链路已经具备可复用基础：专用系统 Agent 与 Skill、MCP 读写、夜间队列、来源权重、项目/别名/Workspace 表、不可变 Snapshot、Report Context 最多 3 个候选以及 Memory 失败不阻塞日报。

需要优化的不是重新建设，而是以下结构性差距：

1. **Job 粒度是用户日，不是用户 Memory。** `report_project_memory_jobs` 以 `user_id + report_date` 为主键；连续多日或历史修改会产生多次彼此独立的 Agent Run，难以保证同一用户 Memory 按顺序收敛。
2. **输入仍以“当天报告分类”为中心。** 每次要求处理当天所有 `current_themes`，并重复携带最近 10 份概览和 10 份历史锚点；它更接近日报主题归档，而不是读取当前 Memory 后处理增量证据。
3. **Proposal 过于表单化。** 当前校验要求每个 theme 必须有且只有一个 decision；Agent 即使认为某条不值得更新，也必须显式填 `unresolved`。一次字段异常会让整份 Proposal 失败。
4. **Memory 基本只增不减。** 当前可应用动作主要是 `link_existing/create_new`；`suggest_rename/suggest_merge` 只记录不应用。Alias 和 workstream cue 会随着 occurrence 累积，缺少降权、移除、解除 Workspace 关联和 AI 自我纠错。
5. **服务端语义规则与 Agent 重叠。** `applyStrongParentScope` 会依据来源权重和多主题命中改写 Agent 决策，Skill 内同时存在大量父项目归并规则。问题出现时很难区分是 Agent 判断、Skill 指令还是服务端改写导致。
6. **存储结构不利于维护线索生命周期。** workstream cue 主要存放在 occurrence JSON 中，适合追溯某日来源，不适合作为可独立增减、降权和标记失效的长期线索。

## 10. 优化后的深模块

把 `api/internal/reportmemory` 收敛为一个深模块，外部只暴露三个入口：

```go
type ProjectMemory interface {
    Observe(ctx context.Context, change EvidenceChange) error
    RunDue(ctx context.Context, now time.Time) (MaintenanceBatchResult, error)
    Resolve(ctx context.Context, query CandidateQuery) (FrozenCandidateContext, error)
}
```

### 10.1 Interface 语义

- `Observe`：接收“某个证据来源发生变化”的引用，幂等合并到该用户待处理水位；调用方不需要知道 Job、Snapshot、Agent 或表结构。
- `RunDue`：夜间 Worker 调用；同一用户严格串行，不同用户可并发。它隐藏输入构建、Agent 调用、操作校验、应用、重跑和失败降级。
- `Resolve`：Report Context 调用；按用户、报告日期、当天 Facts 和 Workspace 返回最多 3 个冻结候选。失败时返回空候选，不影响日报。

复杂的历史窗口、权重、Agent Proposal、CAS、软合并、线索衰减和 Snapshot 都留在模块实现内部。调用方不直接访问这些内部概念。

### 10.2 依赖与 Adapter

- PostgreSQL 属于本地可替代依赖，模块测试通过测试数据库验证，不把 Repository 接口暴露给调用方。
- Agent 平台属于远程但自有依赖，在内部 seam 定义 `MemoryResolver` port；生产使用 Agent 平台 Adapter，测试使用内存 Adapter。

## 11. 用户级增量维护

### 11.1 队列模型

将“一个用户一天一个 Job”调整为“一个用户一条维护状态”：

```text
user_id
desired_evidence_watermark
claimed_evidence_watermark
status
due_at
attempts
external_task_id
last_success_snapshot_id
```

日报保存、历史日报修改、Session 关联变化和 Workspace 新证据都只推进用户的 `desired_evidence_watermark`。运行中又出现新证据时，本轮完成后自动再跑一次，不并发覆盖同一用户 Memory。

Evidence 使用稳定、单调的序列号。历史日报发生修改时，从该修改影响到的最早 Evidence 水位开始按序重放受影响投影，不能把旧日期修改直接作为“最新事实”覆盖之后形成的 Memory。

维护批次以一份日报为最小结算单元。同一用户按日期串行推进 `dirty_from_date`，每份日报成功后才推进到下一份；尚有未处理日报时 Job 保持 `pending`。因此 Token 上限只限制单次 Agent 输入，不会丢弃 Evidence 或提前清空水位。历史修改触发时先废弃该用户现有 V2 可变投影和受影响 Snapshot，再从最近 20 份原始日报的最早一份顺序重建，避免未来状态泄漏到历史 Snapshot。

### 11.2 Agent 输入

每轮只提供：

- 当前有效 Memory 的紧凑投影；
- 上次成功水位之后新增、修改或撤销的 Evidence；
- 每条 Evidence 的来源类型、日期和权威等级；
- 服务端通过名称、Workspace 和现有 Evidence 引用预召回的少量候选；
- 本轮 Token、项目数和操作数预算。

历史日报全文不重复灌入。冷启动与历史重建按日期依次处理最近 10～20 份有内容日报；正常增量只处理新增或修改的日报。每次成功都冻结该证据日期的 Snapshot，失败则不推进水位。

### 11.3 Agent 输出

Proposal V2 改为可为空的操作列表，不要求逐项覆盖所有 theme：

```json
{
  "schema_version": "project-memory-maintenance/v2",
  "operations": [
    {
      "operation_id": "op-001",
      "operation": "upsert_signal",
      "project_ref": "project-001",
      "signal_type": "workstream_cue",
      "value": "用例筛选",
      "evidence_refs": ["evidence-018"],
      "confidence": 0.86,
      "reason": "连续多日出现在同一项目相关主题中"
    }
  ]
}
```

允许 `operations: []`。同一 Proposal 按数组顺序执行；新建项目使用本轮唯一 `temp_ref`，后续操作通过 `depends_on` 引用前置 `operation_id`。前置操作失败时，其依赖操作不执行并保留待处理。

每个操作必须引用本轮输入中的 Evidence，并独立校验、独立记录结果。单个语义操作不合法时拒绝该操作，合法操作照常应用，本轮以“成功但含拒绝项”结算水位，不因原样重试而重复累计或最终屏蔽有效 Memory；拒绝原因进入 Snapshot，后续新 Evidence 到来时 Agent 可再次纠正。只有 JSON 无法解析、身份不匹配、Agent 运行失败或数据库应用失败才视为整轮失败并整体回滚。

### 11.4 服务端硬规则

服务端只保留不可由模型决定的规则：

- 用户隔离和 Evidence 所有权；
- 报告日期和未来数据可见性；
- 引用必须来自本轮输入；
- 来源权威不可降级覆盖：`human_edited/manual_report` 信号只能被更新的人类证据替换或失效，AI 只能新增旁路候选，不能 retire、remove 或 merge 这些权威信号；
- 字段长度、数量和 Token 预算；
- 物理删除禁用、合并必须可撤销；
- 幂等键和用户级串行应用。

父项目语义、是否同一项目、应该增加还是淘汰某个 AI 线索交给 Project Memory Agent。移除 `applyStrongParentScope` 这类服务端语义改写，避免双重决策。

## 12. 可维护的 AI 线索

保留现有 `report_projects` 作为稳定项目身份；新增或正规化项目 Signal 存储，而不是继续只从 occurrence JSON 汇总：

```text
project_id
signal_type: alias | workstream_cue
normalized_value
authority: human_edited | manual_report | ai_inferred | machine
confidence
evidence_count
first_seen_on / last_seen_on
status: active | retired | rejected
last_agent_run_id
```

- occurrence 和 Evidence 继续保存来源历史；Signal 表保存当前有效投影。
- AI 线索可以被 Agent 降权或 `retired`，后续新证据可重新激活。
- 用户修改日报形成的名称证据权重高，但不转换成需要用户维护的项目档案。
- Workspace Link 同样增加 `active/retired` 语义，避免错误关联只能继续累积。

## 13. Report Context 收口

保留现有最多 3 个候选和只读语义，但调整召回顺序：

1. 当天项目名称或高权重 alias 命中；
2. workstream cue 与 Workspace 同时命中；
3. 单独 workstream cue 命中；
4. 单独 Workspace 命中只能作为 `candidate_only`。

Context 只包含项目名、短 alias、稳定 cue、匹配 Fact 和来源权威，不包含历史日报正文、进度、结论或命令。Report Agent 是否采用候选仍由当天 Facts 决定。

Snapshot 必须冻结项目名、Signal 和 Workspace 引用，并记录其 Evidence 水位和有效日期。`Resolve(report_date)` 只能读取报告日期之前已形成的冻结 Snapshot，不得回查当前可变投影；重生成历史日报时不得读取未来日报、未来项目名或未来线索。

## 14. 开发顺序

1. **先改内部维护模型**：用户级串行队列、delta input、Proposal V2、逐操作校验和 AI Signal 生命周期。
2. **再简化 Skill**：Skill 只定义 Project Memory 领域目标、证据边界和操作契约，不继续累加单案例规则。
3. **从原始证据冷启动**：不迁移 V1 的 AI 项目、alias、occurrence cue、Workspace 归属和累计置信度。只读取原始日报、用户修改结果和现有 Session/Workspace/Git Evidence；明确来自手写或用户修改日报的项目名称可作为高权重输入重新整理。
4. **接入 Report Context**：使用新 Signal 投影，按报告日期截断，保持 fail-open 和最多 3 个候选。

不建议先做页面、向量库、第二个 Memory Agent 或新的 Report Agent Loop。当前最重要的是让一个专用 Agent 真正具备“增量维护、淘汰旧线索和自我纠错”的闭环。

### 14.1 生产替换策略

V1 当前关联质量不足，错误 Memory 比无 Memory 风险更高，因此生产不做 V1/V2 双轨，也不把 V1 作为失败回退：

1. 发布 V2 时停止 V1 夜间维护；
2. Report Context 停止读取 V1 Snapshot；
3. V2 按用户从原始证据执行冷启动；
4. 已成功生成 V2 Snapshot 的用户使用 V2；
5. 尚未完成、没有有效 Snapshot 或维护失败的用户按无 Memory 流程生成日报；
6. 某次 V2 维护失败时，本次 Report Run 不注入 Memory；修复并成功维护后才重新启用该用户的 V2 Snapshot，不恢复 V1；
7. V1 Snapshot 和表暂时只读保留用于发布后问题定位，不进入任何生成链路；稳定后再单独安排清理。

生产回退开关只需要关闭 V2 的 `project_memory_context` 注入。日报主链路已经支持无 Memory 正常工作，不需要恢复旧 Agent、旧 Skill 或旧关联逻辑。

## 15. 评测补充

除现有项目名命中外，新增以下评测：

- **自我纠错**：先注入错误 AI 线索，再提供用户修改日报，下一轮能否降权或解除错误关联；
- **新项目边界**：同仓库出现新项目时是否保持独立；
- **线索淘汰**：一次性噪声是否不会永久保留；
- **稳定性**：同一 delta 重放是否幂等，同一用户多日变更是否按顺序收敛；
- **来源升级**：用户修改证据出现后是否替代低权重 AI 命名；
- **历史泄漏**：Memory 不能向当天日报贡献历史成果和状态；
- **成本**：增量 Run 的输入 Token 应显著低于重复发送 10＋10 份历史报告。

## 16. 参考

- `doc/v3/报告Agent两阶段生成/22-Project-Memory可选长期上下文契约-20260803.md`
- `doc/v3/报告Agent两阶段生成/26-Project-Memory工作空间关联与分层日报正文方案-20260805.md`
- `doc/v3/Project Memory/外部Memory项目调研-20260806.md`
- 本机参考项目：`/home/aied/lx/github/TencentDB-Agent-Memory`，commit `fe3230f176f1`
- 研究记录：`/home/aied/lx/pm/research/tencentdb-agent-memory-project-memory-reference-20260808.md`
