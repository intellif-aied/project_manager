# 编码 Agent 长期记忆调研与 Project Memory 结构建议

> 日期：2026-08-05
> 状态：调研与结构建议；不改变当前线上逻辑
> 场景：AIDA 调用 Agent 平台，在临时沙盒内运行 Claude Code，并基于用户 Session 生成日报

## 1. 结论

Project Memory 不应继续演进为“最近几份日报摘要的集合”，也不应依赖 Claude Code 沙盒自己的本地 Auto Memory。

适合 AIDA 的结构是四层：

```text
Evidence Ledger（不可变证据）
        ↓
Workspace Identity（确定性工作空间身份）
        ↓
Project Memory（Agent 整理的可重建语义投影）
        ↓
Report Retrieval Projection（当次只读项目候选）
```

其中：

- AIDA 数据库是长期记忆的唯一持久化主体；临时 Claude Code 沙盒只是消费者。
- Git/CWD 用于判断工作连续性，不直接决定业务项目名称。
- 人工日报父级标题、用户在 Session 中明确表达的项目名用于命名。
- Project Memory Agent 只提交带证据引用的变更提案，服务端校验后落库。
- Report Agent 只读取少量候选，不能修改 Memory，也不能把历史进度、测试或发布状态当成当天事实。

这比“把前三份日报 Summary 全量放进 Prompt”更接近主流编码 Agent 的长期记忆方式，也更适合当前 AIDA 的证据链和多用户隔离要求。

## 2. 官方实现中真正可借鉴的机制

### 2.1 Claude Code：按 Git 项目隔离，小索引常驻，详情按需读取

Claude Code 每个会话都从新的 Context 开始，跨会话信息分成两类：人工维护的 `CLAUDE.md` 与 Agent 自己维护的 Auto Memory。Auto Memory 的目录由 Git 仓库派生，同一仓库的 worktree 和子目录共享；非 Git 环境才退回项目根目录。[Claude Code Memory 官方文档](https://code.claude.com/docs/en/memory)

其 Auto Memory 不是把所有历史都装入 Context：

- `MEMORY.md` 是简短索引，每次会话只加载前 200 行或 25KB；
- 详细内容进入主题文件，需要时由 Agent 使用文件工具读取；
- 接近上限时要求合并重复项、移走细节、删除过期项；
- Memory 是普通 Markdown，可以审计、修改和删除；
- `CLAUDE.md` 和 Auto Memory 都是 Context，不是强制配置；冲突指令可能被模型任意选择。

对 AIDA 的启发：

1. Project Memory 应以 Git 仓库/工作空间为重要 Scope，而不只是“用户最近日报”；
2. Report Agent 常驻输入只能是小型项目索引，详细来源按需保留在 AIDA；
3. Memory 是参考信息，不应伪装成系统事实或硬性归属要求；
4. 沙盒通常是临时的，且系统 Agent 被多个 AIDA 用户复用，不能把 `~/.claude/projects/.../memory` 当成跨 Run 的可信存储，否则会面临丢失或串用户风险。

当前 Agent 平台每次 Run 都创建独立沙盒，AIDA/平台把 `CLAUDE.md` 物化进本次工作区，Run 结束后沙盒随之销毁。因此 `CLAUDE.md` 可以作为“当次 Context 运输层”，不能作为长期存储：若要把 Project Memory 放进 `CLAUDE.md`，必须在 Claude Code 启动前由 AIDA 按 `run_id + user_id` 生成只读内容；运行中才确定的候选仍应通过绑定 Run 的 MCP 返回。Claude 在沙盒内对 Memory 文件的任何写入都不得回灌 AIDA DB。

### 2.2 OpenHands：原始事件不被摘要替代，压缩只是可回放视图

OpenHands 的 Condenser 在历史超过阈值时保留头部和近期事件，把中间事件压缩成一条 `Condensation`；该记录保存被折叠事件的 ID，原始事件历史仍是可追踪来源。[OpenHands Condenser 架构](https://docs.openhands.dev/sdk/arch/condenser)

OpenHands 的 Persistent Memory 进一步使用两层文件：用户级与项目级。每层仅把精简 `MEMORY.md` 注入 Prompt，详细 Daily Log 不自动注入；组合索引约有 6000 字符预算，并把 Agent 写入内容显式包裹成 `UNTRUSTED_CONTENT`。它还要求 Agent 在任务接近结束时写入耐久知识、合并重复项、清理过期项，并跳过容易重新发现的信息和凭证。[OpenHands Persistent Memory](https://docs.openhands.dev/sdk/guides/persistent-memory)

对 AIDA 的启发：

- Session、Digest、日报和 Git/CWD 观察应进入不可变 Evidence Ledger；
- Project Memory 是从证据生成的 Projection，随时可以按新算法重建；
- 压缩、合并或遗忘 Memory 时不能删除原始证据；
- 注入 Report Agent 的历史项目参考应明确标记为“不可信提示、不是当天证据”；
- Memory 整理失败必须回退到上一个成功版本或无 Memory，不能影响日报生成。

### 2.3 Letta：核心 Memory 常驻，归档内容检索，写入要避免整块覆盖

Letta 把少量重要内容放进始终可见的 Memory Blocks；大规模内容放在 Archival Memory、Files 或外部 RAG/MCP 中，需要时再检索。Memory Block 可以设置为只读，并依赖清晰的 `label`、`description` 与大小限制告诉 Agent 如何使用。[Letta Memory Blocks](https://docs.letta.com/guides/core-concepts/memory/memory-blocks) [Letta Context Hierarchy](https://docs.letta.com/guides/core-concepts/memory/context-hierarchy)

Letta 官方还明确提示，共享 Block 被多个进程修改时是 last-write-wins，可能覆盖并发更新；只读 Block 或应用侧受控写入更安全。[Letta Memory Blocks：并发写入说明](https://docs.letta.com/guides/core-concepts/memory/memory-blocks)

对 AIDA 的启发：

- Report Agent 只接收小型、只读的 `project_reference` 投影；
- 完整项目历史和证据保存在 AIDA DB，通过服务端/MCP 检索；
- Project Memory Agent 不直接覆盖整个 Snapshot，只输出增量 Proposal；
- 服务端使用版本号或 CAS 校验后原子应用，避免并发任务覆盖彼此结果。

### 2.4 LangGraph / LangMem：Thread 状态与跨 Thread 长期记忆分离

LangGraph 把 Checkpointer 定义为单 Thread 的短期状态，把 Store 定义为跨 Thread 的长期记忆；Store 使用层级 Namespace 组织数据，并支持直接读取、Metadata Filter 与语义检索。[LangGraph Persistence](https://langchain-ai.github.io/langgraph/concepts/time-travel/) [LangMem Concepts](https://langchain-ai.github.io/langmem/concepts/conceptual_guide/)

LangMem 同时区分前台写入和后台整理：前台可以即时更新关键内容，但增加主链路延迟；后台反思适合跨多次交互做归并、摘要和模式提取，不影响用户当前调用。

对 AIDA 的启发：

- 单次 Agent Run、Session 时间段是短期连续状态；Project Memory 是跨 Run 的长期 Store，不能把二者混成一个大 Thread；
- 当前夜间专用 Project Memory Agent 的方向合理；
- Namespace 至少包含 AIDA 用户与工作空间身份，Report Agent 的系统管理员账号不能成为 Memory Scope；
- 检索应先做 `user + workspace identity` Metadata 精确过滤，再在候选中做语义排序。

### 2.5 Mem0：可借鉴作用域、过滤和冲突处理，不适合直接成为真相层

Mem0 的写入管线会从输入抽取事实、检查既有 Memory 的重复或冲突，再保存；搜索支持用户/Agent/Run 作用域、Metadata Filter 与语义相关性。[Mem0 Add Memory](https://docs.mem0.ai/core-concepts/memory-operations/add) [Mem0 Python Quickstart](https://docs.mem0.ai/open-source/python-quickstart)

对 AIDA 有价值的是“先强作用域过滤，再做语义召回”以及 Memory 的完整 CRUD/审计能力。不适合直接复制的是自动事实合并：项目归属存在一对多、改名、临时工作和跨仓库情况，必须保留来源与关系，不能让向量相似度或 LLM 自动把旧项目覆盖成所谓最新真相。

## 3. AIDA 建议结构

### 3.1 Evidence Ledger：不可变来源账本

保存 Project Memory 所依据的原始观察，每条都有稳定 `evidence_ref`：

```json
{
  "evidence_ref": "...",
  "user_id": "...",
  "session_id": "...",
  "observed_at": "...",
  "evidence_type": "git_remote|repo_root|cwd|explicit_project_name|manual_report_parent|ai_brief_subject",
  "value": "...",
  "source_ref": "session-slice/report/run",
  "source_weight": 0.95
}
```

规则：

- Evidence 只追加，不因 Memory 改名、合并或过期而覆盖；
- 原始 Session/Digest 不直接进入 Report Agent 的历史提示；
- 路径和 remote 可以在内部标准化并哈希，Report Agent 不需要看到真实绝对路径；
- 用户手工日报不是绝对 GT，但其中明确的父级项目标题是高权重命名证据；
- AI 自动生成稿只能提供较低权重的 Brief Subject，不能反向证明项目归属。

### 3.2 Workspace Identity：确定性连续工作身份

Workspace Identity 解决“这些 Session 片段是否属于同一代码工作空间”，不负责命名业务项目。

```json
{
  "workspace_ref": "...",
  "user_id": "...",
  "codebase_key": "hash(normalized_git_remote + repo_root)",
  "workspace_key": "hash(normalized_cwd)",
  "first_seen_at": "...",
  "last_seen_at": "...",
  "evidence_refs": ["..."],
  "identity_strength": "git_exact|cwd_exact|session_continuity"
}
```

提取顺序：

1. Git remote 标准化后与仓库根目录组合，形成最稳定的 `codebase_key`；
2. 没有 Git 时用规范化 CWD 形成用户内可比的 `workspace_key`；
3. 同一 Session 前段出现 Git，后续片段仍在同一工作空间时，可以继承该身份；
4. 一旦 Git 根、remote 或工作目录发生明确切换，切成新的时间段，不能把旧项目贯穿整条长 Session；
5. 同一仓库可以承载多个项目，一个项目也可以跨多个仓库，因此 Workspace 与 Project 必须是多对多关系。

这层应由服务端确定性代码完成，不需要 LLM。

### 3.3 Project Memory：Agent 整理的语义投影

Project Memory 只回答“这项持续工作通常叫什么、有哪些稳定工作线索”，不保存每天的成果详情：

```json
{
  "project_ref": "...",
  "user_id": "...",
  "canonical_name": "AI Coding 提效支撑",
  "aliases": ["AI Coding"],
  "workstream_cues": ["Qwen3-4B 训练", "GLM5.2-FP8 数据生成"],
  "workspace_links": [
    {
      "workspace_ref": "...",
      "confidence": 0.92,
      "valid_from": "...",
      "valid_to": null,
      "evidence_refs": ["..."]
    }
  ],
  "status": "active|dormant|superseded|conflicted",
  "first_seen_on": "...",
  "last_seen_on": "...",
  "version": 7
}
```

必须排除：

- “训练进度 40%”“已生成 2.4 万条”等历史进度；
- “测试通过、验收完成、已经发布”等外部状态；
- 某日具体交付结果、下一步计划、模型自行评价；
- 命令、commit hash、内部 ID 和长成果句 alias。

允许保留：

- 明确项目名称；
- 同一个工作对象的稳定简称；
- 模型、模块、产品或任务类型等用于召回的工作线索；
- Workspace 关系、来源、时间、置信度与版本。

### 3.4 Report Retrieval Projection：当次只读候选

日报生成时不加载完整 Project Memory。服务端根据当天 Facts 与 Workspace Identity 生成最多 2～3 个只读候选：

```json
{
  "project_reference": [
    {
      "project_ref": "...",
      "candidate_name": "AI Coding 提效支撑",
      "matched_cues": ["Qwen3-4B 训练", "同一 codebase"],
      "related_fact_refs": ["fact-012", "fact-018"],
      "confidence": "high",
      "last_confirmed_on": "2026-08-04",
      "instruction": "仅用于理解项目名称和归并；不是当天事实，可忽略"
    }
  ]
}
```

该投影应通过现有、绑定 `run_id + AIDA user_id` 的 Report Context/MCP 返回，而不是写入系统管理员 Agent 的沙盒 Auto Memory：

- 保证每次 Run 的用户隔离；
- 方便审计实际注入了什么；
- 沙盒销毁不影响记忆；
- 个人 Agent 只能读取与其 Report Run 绑定的系统 MCP 数据；
- Report Agent 无 Memory 写工具，不能把自己的输出反写为真相。

如果 Agent 平台支持在启动 Claude Code 前注入动态 `CLAUDE.md`，可以把极短的只读索引同时物化进去，减少 Agent 忘记检索的概率；但其数据仍来自 AIDA Snapshot，并在沙盒销毁时丢弃。现阶段以 Report Context/MCP 作为权威入口更符合现有 Run 绑定和用户隔离机制。

可以借鉴 OpenHands 的做法，将该块明确标记为 `UNTRUSTED_REFERENCE` 或等价语义，使 Claude Code 把它当背景，而不是指令或事实。

## 4. 写入时机与更新协议

### 4.1 Session 上传/更新：只做确定性观察

在 Session 上传、增量更新或 Projection 构建时：

- 解析完整 Session 时间线中的 Git remote、repo root、CWD 切换；
- 形成 Workspace Segment 和 Evidence；
- 同一 Session 后续无 Git 的片段可以继承同一 Segment 的身份；
- 不在这一步调用 LLM，不创建业务项目名。

### 4.2 日报保存或默认留存：增加命名观察

- 人工日报父级标题、AI 后人工改写的父级标题进入高权重 Evidence；
- 未修改 AI 稿仅取结构化 Brief Subject，权重较低；
- 子项成果、进度和测试/发布结论不成为 alias；
- 旧格式的“非编号父级标题 + 子列表”需要保留父级关系，不能只抓子项。

### 4.3 夜间 Project Memory Agent：输出 Patch Proposal

夜间任务只处理当日有日报或新 Evidence 的用户。Memory Agent 输入：

- 当天新增 Evidence；
- 命中的 Workspace Identity；
- 少量现有 Project Profile；
- 当前版本号。

只允许输出：

- `create_project`
- `link_workspace`
- `add_alias`
- `add_workstream_cue`
- `supersede_name`
- `mark_conflict`
- `mark_dormant`
- `unresolved`

每个动作必须引用输入中的 `evidence_ref`。服务端执行：

1. 校验证据属于当前 AIDA 用户；
2. 校验项目、Workspace 与时间范围；
3. 校验 alias/cue 不是成果句或历史状态；
4. 使用 `expected_version` 做 CAS；
5. 原子应用通过的动作并生成新 Snapshot；
6. 失败时保留上一份成功 Snapshot，不阻塞日报。

这保留了 Agent 对项目语义和层级的理解能力，同时避免它自由覆盖整份 Memory。

## 5. 召回与置信度

召回顺序固定为：

1. `user_id + codebase_key` 精确命中；
2. `user_id + workspace_key` 精确命中；
3. 同一 Session、同一 Workspace Segment 的连续继承；
4. 当天明确项目名/alias/workstream cue 的字面命中；
5. 在上述过滤后的候选中做语义排序；
6. 只有纯语义相似、没有 Workspace 或命名锚点时，只能作为低置信候选或不注入。

推荐把置信度拆成可解释信号，而不是只保存一个 LLM 分数：

```text
identity_score + naming_score + continuity_score + recency_score - conflict_penalty
```

Report Agent 看到命中原因，服务端和评测也能判断是 Git/CWD、人工项目名还是纯语义导致的关联。

## 6. 冲突、遗忘和项目切换

- 同一 Workspace 新出现另一个明确项目名时，不覆盖旧项目；新增一条带有效期的关系并标记冲突，等待后续证据收敛。
- 用户明确改名时，旧名称保留为 `superseded` 或 alias，证据仍可追踪。
- 弱 alias 和只由 AI 自动稿产生的 cue 长期未命中时可降权或进入 dormant；人工明确项目名不因短期未出现而删除。
- Project Memory 的“遗忘”是停止召回或降低权重，不是删除 Evidence。
- 用户删除数据或执行隐私清除时，才按数据治理要求删除对应 Evidence 和派生 Snapshot。
- 不跨用户传播项目归属。未来若做团队共享项目，必须由明确团队/项目成员关系或共享代码库登记建立，不使用语义相似度自动扩散。

## 7. 两个现有案例如何工作

### 刘乐 / 黄咏驰：芯片验证平台

- 稳定 CWD 先形成各自用户空间内的 Workspace Identity；
- 历史人工日报中的“芯片验证平台”作为高权重命名 Evidence；
- 后续同 Workspace 的版本流、用例筛选、测试执行模块可以获得“芯片验证平台”高相关候选；
- 候选仍可被 Report Agent 忽略，不能因为 CWD 相同强制归属；
- 两人的 Memory 不因同一部门自动互通，除非未来建立明确团队项目映射。

### AI Coding 提效支撑

历史人工日报结构：

```text
AI Coding 提效支撑
  - Qwen3-4B 训练
  - GLM5.2-FP8 数据生成
```

应形成父项目、两个稳定工作线索及可能的 Workspace 关联。以后再次出现相同模型、任务类型且 Workspace/Session 连续时，可以把“AI Coding 提效支撑”作为高相关候选。

Memory 不能继承训练 40%、已生成 2.4 万条、step=9000 结果等历史状态；这些内容必须由当天 Facts 再次证明，才能进入当天日报。

## 8. 与当前方案的主要差异

当前 `reportmemory` 已具备用户隔离、夜间 Agent、Snapshot、只读 Hint 和失败回退，方向正确；主要缺口不是再增加 Prompt 规则，而是 Memory 数据结构仍偏“日报主题词表”：

| 当前结构 | 建议调整 |
|---|---|
| Project / Alias / Occurrence / Snapshot | 增加 Evidence Ledger、Workspace Identity、Project-Workspace 有效期关系 |
| Memory Agent 直接理解最近主题和历史概览 | 服务端先确定性提取 Workspace，再让 Agent整理项目语义 |
| Fact 与 alias 相似度主导召回 | 先 user + workspace 精确过滤，再做名称/语义排序 |
| 单个 confidence | 保存可解释的 identity/naming/continuity/conflict 信号 |
| Snapshot 是主要记忆产物 | Evidence 是真相；Snapshot 是可版本化、可重建 Projection |
| 候选项目名与大量 related Fact | 每次只投影 2～3 个候选及少量锚点，明确可忽略 |

## 9. 不建议采用的方向

1. **不直接启用沙盒 Claude Code Auto Memory 作为 AIDA Project Memory。** 沙盒生命周期、系统 Agent 多用户复用和审计边界均不合适。
2. **不把最近三份日报 Summary 全量常驻。** 它会混入历史状态、成果句和 AI 自己的错误，并随时间增长。
3. **不依赖向量相似度自动确定项目。** 相似工作可能属于不同项目，同一项目也可能跨仓库。
4. **不把 CWD 或 Git 仓库直接当项目名。** 它们是身份和连续性信号，不是业务命名。
5. **不让 Report Agent 同时写 Memory。** 生成结果会反向强化自身错误，并增加用户间或并发 Run 污染风险。
6. **不先上知识图谱或通用 Memory 框架。** 当前核心是可靠 Scope、证据、版本和召回顺序；现有 PostgreSQL + 独立 Agent/MCP 足以实现并评测。
7. **不把 Memory 设计成归属校验器。** 它只帮助理解项目名称和工作连续性，日报事实仍只来自当天 Session/Digest/Fact。

## 10. 推荐实施顺序

### Phase 1：先补确定性身份层

- 增加 Session Workspace Segment 与 Evidence Ledger；
- 从完整 Session 时间线提取 Git/CWD，而不只看当天选中切片；
- 建立 Project-Workspace 多对多关系；
- 不改变 Report Agent 输出，先影子观测命中率与错误合并。

### Phase 2：调整 Memory Agent 更新契约

- 从整份 Snapshot 覆盖改成带 `evidence_ref + expected_version` 的 Patch Proposal；
- 支持冲突、Dormant、Superseded，而不是只做 link/create；
- 用历史数据重建 Project Memory，旧 Snapshot 保留用于对照。

### Phase 3：改为分层召回与小型只读投影

- 精确 Workspace 过滤后再语义排序；
- 每次最多注入 2～3 个候选；
- 明确显示命中依据、新鲜度和可忽略语义；
- 与无 Memory 基线做重复 A/B，重点观察项目名召回、错误吸附和历史状态泄漏。

### Phase 4：再决定是否做团队共享

只有个人内 Workspace Memory 稳定后，才评估团队 Project Registry。团队共享必须有组织项目关系、明确成员或共享代码库登记，不从两名员工 Session 相似度推断。

## 11. 验收重点

- 同一 Session 前段出现 Git、后段无 Git 时，后段仍继承正确 Workspace；
- Session 切换仓库/CWD 后不继承旧项目；
- 同一仓库多项目和同一项目多仓库都能保持多对多关系；
- 人工父级项目名能绑定 Workspace，子项不会替代父项目；
- Report Context 不出现历史进度、测试通过、发布状态或成果句；
- Memory Agent 错误、超时、冲突或 CAS 失败均不影响日报成功；
- 能从每个候选追踪到 Workspace/Evidence，并能按新算法重建；
- 默认系统 Agent 和个人 Agent 均按 AIDA 用户与 Run 隔离，系统管理员账号不成为 Memory Owner。
