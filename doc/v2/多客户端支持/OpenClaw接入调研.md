# OpenClaw 接入调研

> 调研日期：2026-07-21
>
> 调研对象：OpenClaw 官方文档与官方仓库 `main`，源码快照 `aa9cf01b5602f9c731912421467b7fb7c3a2a288`
>
> 文档性质：接入事实核对与 P0 决策依据，不表示 OpenClaw 已实现或已发布
>
> 结论：P0 可以提供 **report-only** 手动接入；`content_capability=report`，`usage_capability=unavailable`；必须排除所有自动同步和 `upload-all` 路径。

## 1. 决策摘要

| 问题 | 结论 | 分类 |
|---|---|---|
| P0 能否用于报告内容 | 可以，但只允许用户明确选择单个 Session 后，通过官方 CLI 导出并做 Aida 侧最小化过滤 | 建议 |
| P0 Usage 能力 | `unavailable`，不是 `estimated` 或 `exact` | 必要结论 |
| 是否进入自动同步 | 不进入；自动同步、后台轮询、`upload-all` 必须排除 OpenClaw | 必要结论 |
| 正式读取入口 | `openclaw sessions --all-agents --json` 发现；`openclaw sessions export-trajectory --session-key ... --workspace ... --json` 读取用户选中的单个 Session | 已验证官方事实 |
| 是否直读本地库 | 不直读。当前主线运行时 Session 与 transcript 都由 Gateway 管理并存储在 per-agent SQLite；旧 `sessions.json`/JSONL 只是迁移或归档兼容面 | 已验证官方事实 + 建议 |
| Aida 原生身份 | 以 `agentId + sessionId` 作为原生 Session 身份；`sessionKey` 只作为导出定位器和必要关系元数据，不作为上传展示标题 | 建议 |
| 主要风险 | Session 可能来自私人聊天、群聊、cron、webhook、subagent；导出包包含 prompt、消息、工具参数/结果、运行配置和本地路径，官方脱敏仍是 best-effort | 已验证官方事实 |

P0 的“report-only”含义是：会话正文可形成 Aida Canonical Event 并供个人报告读取，但不产生 Canonical Usage、Token 或成本记录。没有 Usage 时展示“暂不支持”或 `--`，不能展示 `0`。

## 2. 当前官方存储模型

OpenClaw 当前由单个 Gateway 作为 Session 状态权威。每个 Agent 的运行时 Session row 与 transcript event 均存储在：

```text
~/.openclaw/agents/<agentId>/agent/openclaw-agent.sqlite
```

旧路径 `~/.openclaw/agents/<agentId>/sessions/`、`sessions.json` 与历史 JSONL 仍可能存在，但官方把它们定义为 legacy migration input、归档或离线维护目标。新建 Session 可能只存在于 SQLite，旧版文件运行时看不到。因此 Aida 不应扫描、锁定或解析 OpenClaw SQLite，也不应把旧 JSONL 路径当成稳定生产契约；应使用官方 CLI，让 OpenClaw 自己处理当前/旧版存储差异。

依据：

- [Session management deep dive：两层 SQLite 存储、磁盘位置与 legacy 边界](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/docs/reference/session-management-compaction.md)
- [Sessions CLI：列表、分页及 all-agents 范围](https://docs.openclaw.ai/cli/sessions)

## 3. 发现命令与 JSON shape

正式发现命令：

```bash
openclaw sessions --all-agents --limit all --json
```

默认只返回最新 100 条；Aida 如果实现全量手动列表，必须显式传 `--limit all`，或者使用明确分页/上限策略，不能把默认 100 条误当完整集合。官方文档给出的顶层 JSON 字段为：

```text
path, stores, allAgents, count, totalCount, limitApplied,
hasMore, activeMinutes, sessions[]
```

官方 CLI 示例只展示了最小 Session row（`agentId`、`key`、`model`），但源码的 `SessionDisplayRow` 及 JSON 分支还会输出：

```text
key, updatedAt, ageMs, sessionId, sessionFile,
spawnedBy, parentSessionKey, forkedFromParent, spawnDepth,
sessionStartedAt, lastInteractionAt, lastActivityAt,
label, displayName, inputTokens, outputTokens, totalTokens,
totalTokensFresh, model, modelProvider, contextTokens, ...
```

其中 Token 字段可能缺失；JSON 分支会把无法解析的 `totalTokens` 输出为 `null`，不能据此推导为 0。

源码依据：

- [`SessionDisplayRow` 与 row 映射](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/src/commands/sessions-table.ts)
- [Sessions JSON 输出实现](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/src/commands/sessions.ts)

P0 发现时应先过滤以下非普通用户编码会话：cron、hook、heartbeat、ACP、model-run probe，以及无法证明是用户准备上传的 subagent。渠道 direct/group/room 会话也不能默认视为编码工作，只能展示必要的最小元数据供用户主动选择。

## 4. Session 身份和生命周期

### 4.1 `sessionKey` 与 `sessionId` 不同

- `sessionKey` 是路由和隔离桶，例如 `agent:<agentId>:main`、`agent:<agentId>:<channel>:group:<id>`、`cron:<jobId>`、`hook:<uuid>`。
- `sessionId` 是当前 transcript 的身份。`/new`、`/reset`，以及启用后的 daily/idle reset 会让同一个 `sessionKey` 指向新的 `sessionId`。
- 当前默认不自动 reset，Session 通常持续并由 compaction 控制上下文长度，但这不改变 reset 后 `sessionId` 会轮换的事实。

因此：

1. `sessionKey` 不是一次固定 transcript 的稳定 ID，而且可能直接携带渠道、群组或对端身份信息。
2. `sessionId` 在一次 transcript 生命周期内稳定，适合作为 Aida 的 `native_session_id`。
3. 为避免不同 Agent 命名空间碰撞，Aida 原生身份应至少包含 `agentId + sessionId`；Aida 自身仍由当前用户与 `agent_type=openclaw` 隔离。
4. `sessionKey` 只保留在本机定位和父子关系处理所需的私有元数据中，不应原样出现在普通报告或日志。

依据：[官方 Session key/id/reset 语义](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/docs/reference/session-management-compaction.md)。

## 5. 单 Session 导出命令与返回格式

P0 只对用户明确选择的 Session 执行：

```bash
openclaw sessions export-trajectory \
  --session-key "<exact-session-key>" \
  --workspace "<Aida-private-temp-workspace>" \
  --output "<Aida-generated-relative-name>" \
  --json
```

参数必须作为 argv 传入，不经过 shell 拼接。`--output` 只能是 `.openclaw/trajectory-exports/` 内的相对目录；绝对路径、`~`、越界和现有符号链接目录会被官方实现拒绝。

`--json` 的 stdout 是导出摘要，不是 Session 正文，字段为：

```json
{
  "outputDir": "...",
  "displayPath": "...",
  "sessionId": "...",
  "eventCount": 123,
  "runtimeEventCount": 45,
  "transcriptEventCount": 78,
  "files": ["manifest.json", "events.jsonl", "session-branch.json", "..."]
}
```

源码中 CLI 先用 `sessionKey` 只读加载 Session row，取得 `sessionId` 和 transcript target，再调用 exporter；这使 Aida 无需理解 SQLite schema。

依据：

- [官方 trajectory 命令、访问与输出目录约束](https://docs.openclaw.ai/tools/trajectory)
- [CLI 只读解析和 JSON summary](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/src/commands/export-trajectory.ts)
- [导出目录安全检查与 summary 字段](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/src/trajectory/command-export.ts)

## 6. Bundle 文件和可提取字段

Bundle 使用 `traceSchema=openclaw-trajectory`、`schemaVersion=1`。

| 文件 | 官方内容 | P0 是否读取 |
|---|---|---|
| `manifest.json` | schema、生成时间、`sessionId`、`sessionKey`、redacted workspace、leaf、事件数量、实际文件清单、warnings | 是，用于格式校验和完整性检查 |
| `events.jsonl` | runtime 与 active transcript branch 合并、按时间排序的事件流 | 是，只取允许的内容事件 |
| `session-branch.json` | `{header, leafId, entries}`；完整 redacted active branch | 可作为校验/降级，不应与 `events.jsonl` 重复上传 |
| `metadata.json` | harness、model、config、plugins、skills、prompting 等 | 否 |
| `artifacts.json` | 最终状态、错误、usage、prompt cache、assistant text、工具元数据等 | 否；P0 不采 Usage |
| `prompts.json` | system、submitted prompts、skills prompt 等 | 否 |
| `system-prompt.txt` | 编译后的 system prompt | 否 |
| `tools.json` | 发给模型的工具定义 | 否 |

`manifest.json` 的 `contents` 才是某次 bundle 实际存在的文件清单；补充文件会随捕获情况缺失，不能假定总是存在。

### 6.1 `events.jsonl` envelope

每行公共字段：

```text
traceSchema, schemaVersion, traceId, source, type, ts, seq,
sourceSeq?, sessionId, sessionKey?, runId?, workspaceDir?,
provider?, modelId?, modelApi?, entryId?, parentEntryId?, data?
```

主要 transcript-derived `type/data`：

| type | data 关键字段 | P0 用途 |
|---|---|---|
| `message.user` / `message.assistant` / tool-result 对应 message type | `message`（官方已做 diagnostic sanitize） | 用户目标、Agent 可读结果；按 role/content 再做 Aida allowlist |
| `tool.call` | `toolCallId`, `name`, `arguments`, `assistantEntryId`, `blockIndex` | 只保留工具名；参数默认不上传 |
| `session.compaction` | `summary`, `firstKeptEntryId`, `tokensBefore`, `details`, `fromHook` | 可保留 summary；`tokensBefore` 不作为 Usage |
| `session.branch_summary` | `fromId`, `summary`, `details`, `fromHook` | 可保留 summary |
| `session.custom` | `customType`, `data` | 默认排除 |
| `session.custom_message` | `customType`, `content`, `details`, `display` | 默认排除，除非后续逐类型批准 |
| `session.thinking_level_change` | `thinkingLevel` | 排除 |
| `session.model_change` | `provider`, `modelId` | 可作为非敏感元数据 |
| `session.label` | `targetId`, `label` | 可用于显示候选，不能作身份 |
| `session.info` | `name` | 标题优先候选，不能作身份 |

Runtime types 包括 `session.started`、`trace.metadata`、`context.compiled`、`prompt.submitted`、`model.fallback_step`、`model.completed`、`trace.artifacts`、`session.ended`。P0 不上传 runtime event 原文；它们可能含 system prompt、完整 submitted prompt、工具信息、错误、usage 与本地运行配置。

源码依据：

- [Trajectory event/manifest 类型定义](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/src/trajectory/types.ts)
- [Transcript event 映射及 bundle 文件生成](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/src/trajectory/export.ts)
- [Runtime event envelope 与 `runId`](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/src/trajectory/runtime.ts)

### 6.2 时间、cwd 和标题

- 开始时间：`session-branch.json.header.timestamp`。
- 活动时间：优先列表 row 的 `lastActivityAt`/`updatedAt`，正文事件使用每行 `ts`；最终以最大合法内容事件时间作为报告内容结束时间。
- cwd：header 有 `cwd`，但导出会对 workspace/home/local path 做脱敏，可能只剩 `$WORKSPACE_DIR` 或脱敏路径；P0 只能将其作为可选展示提示，不能依赖其恢复真实项目路径。
- 标题：优先 `session.info.data.name`，其次非空 label，再其次首个有效 user message 的截断摘要；没有时显示明确降级文案。标题、时间和 cwd 均不得参与唯一身份。

Transcript header 的官方类型为 `type`, `version?`, `id`, `timestamp`, `cwd`, `parentSession?`；普通 entry 有 `id`, `parentId`, `timestamp`。依据：[Session manager 类型定义](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/src/agents/sessions/session-manager-types.ts)。

## 7. Subagent 与 fork 关系

OpenClaw subagent 使用独立 Session，key 形如：

```text
agent:<agentId>:subagent:<uuid>
```

Session row 可提供：

- `spawnedBy`：生成该 Session 的父 Session key；
- `parentSessionKey`：显式父 Session key；
- `forkedFromParent`：是否从父 transcript fork；
- `spawnDepth`：subagent 深度；
- transcript header `parentSession`：父 Session 身份/路径关系。

默认 native subagent 是 isolated；显式 `context: fork` 会分支父 transcript。线程绑定 spawn 默认可能是 fork。父 active branch 超过内部上限时，OpenClaw 会自动降级为 isolated，而不是保证 fork。operator fork 同样会创建新 Session，并以 fresh token counters 开始。

因此 P0：

1. 只在父子两端信息一致时写入 Aida `parent_session_ref`；冲突或只有 route key 时不猜。
2. `spawnedBy`/`parentSessionKey` 是关系证据，不是计费事实归属证据。
3. fork 可能复制父 active branch；即使 entry id 看似可去重，也没有真实版本样本证明所有 provider、compaction 和 isolated fallback 下都维持同一计费事实身份。
4. P0 允许 subagent 内容作为独立 report-only Session，但不做 family Token。

依据：

- [官方 subagent key、isolated/fork 与关系说明](https://docs.openclaw.ai/tools/subagents)
- [SessionEntry 父子/fork 字段](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/src/config/sessions/types.ts)
- [fork 限制与 operator fork](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/docs/reference/session-management-compaction.md)

## 8. Token usage 结论

### 8.1 官方能提供什么

OpenClaw 归一化的 Token 字段包括：

```text
input, output, cacheRead, cacheWrite, reasoningTokens, total
```

Session row 还有 `inputTokens`, `outputTokens`, `totalTokens`, `totalTokensFresh`, `cacheRead`, `cacheWrite`, `contextTokens`。Assistant transcript entry会保留 provider 返回的归一化 usage；runtime `model.completed`/`trace.artifacts` 也会带 attempt-level usage，event envelope 另有 `runId`。

但官方同时明确：

- Token counters 是 best-effort / provider-dependent；
- `totalTokens` 可能 stale/unknown，`totalTokensFresh=false` 时不能作为新鲜上下文统计；
- provider `usage.total` 可能包含 cache、output 和多个 tool-loop model calls，不能等同当前 context；
- usage 成本是本地定价估算，不能作为 Aida 原始成本事实；
- trajectory capture 可以被 `OPENCLAW_TRAJECTORY=0` 关闭，此时 runtime-only usage 可能缺失。

依据：

- [Token use and costs](https://docs.openclaw.ai/reference/token-use)
- [Usage 归一化字段及 provider aliases](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/src/agents/usage.ts)
- [`model.completed` 记录 attempt usage](https://github.com/openclaw/openclaw/blob/aa9cf01b5602f9c731912421467b7fb7c3a2a288/src/agents/embedded-agent-runner/run/attempt-finalize.ts)

### 8.2 为什么 P0 是 `unavailable`

`exact` 不成立：官方输出虽有 usage 和若干 event/run/entry ID，但没有公开承诺一个跨 transcript generation、跨 fork/subagent 复制、跨 compaction 的全局稳定“计费事实 ID”；也尚无 Aida 支持版本的真实逐调用、单 Session、family 三层零误差对账。

`estimated` 也不应使用：当前缺口不是简单的近似误差，而是 provider 缺失、runtime capture 可关闭、累计/attempt/message 范围混合以及 fork 原始归属未验证。把可见总量标成 estimated 仍可能重复计入父子历史，产生结构性错误。

所以 P0 必须：

```text
content_capability = report
usage_capability = unavailable
```

Adapter 不输出 Canonical Usage；服务端不产生 0 Token 记录，不进入成本计算。以后只有在固定 OpenClaw 版本上证明 assistant entry usage 的字段语义、稳定事实 ID、owner Session、fork 复制和 compaction 行为，并完成三层零误差对账后，才允许直接升到 `exact`。没有这些证据时保持 `unavailable`，不以 session 总量折中为 `estimated`。

## 9. 隐私与安全边界

官方明确 trajectory bundle 可能包含 prompt、system prompt、model message、tool schema、tool arguments/results、runtime event、本地路径、plugins、skills 和配置。官方会脱敏 credentials、已知 secret-like 字段、image data、state/workspace/home path，但声明脱敏是 best-effort，不能识别所有应用秘密。

因此 P0 必须执行：

1. 只在用户明确选择并确认敏感内容提示后，导出该一个 `sessionKey`。
2. 不使用 `--all-agents` 结果自动逐个导出，不后台轮询正文，不读取其他 Session。
3. 使用 Aida 私有临时 workspace，目录权限仅当前用户可读；成功、失败和中断后都清理整个本次 export 目录。
4. 不上传原始 bundle；只上传 Aida allowlist 后的 Canonical Event。
5. 默认排除 `metadata.json`、`artifacts.json`、`prompts.json`、`system-prompt.txt`、`tools.json`。
6. `events.jsonl` 中仅保留用户可读目标、assistant 可读结果、工具名、必要时间/模型元数据；默认删除 tool arguments、tool result body、system/custom/runtime payload、本地绝对路径、渠道对端、Session key。
7. 把 `manifest.contents`、文件大小、事件上限和 schemaVersion 当输入校验；拒绝符号链接、越界文件和未知大文件，不遍历 bundle 外路径。
8. 即使官方已脱敏，Aida 上传前仍显示“可能包含私人聊天、代码、命令输出和秘密”的提示，不宣称二次过滤可以发现所有秘密。

官方来源：[Trajectory privacy and limits](https://docs.openclaw.ai/tools/trajectory)。

## 10. P0 接入流程与自动同步排除

允许的手动流程：

```text
aida clients / upload-client
  -> 固定 argv 调用 openclaw sessions --all-agents --limit all --json
  -> 本地最小化列表，用户明确选择
  -> 固定 argv 导出单个 sessionKey 到私有临时 workspace
  -> 校验 manifest + schema v1
  -> events.jsonl allowlist 转 Canonical Event
  -> canonical prepare/chunk/finalize
  -> 清理临时导出
```

必须排除：

```text
auto-sync
aida auto-sync upload-all
aida upload --all
任何 daemon 定时扫描/后台正文导出
```

原因不是性能，而是产品授权边界：OpenClaw Session 可来自私人渠道、多 Agent、群聊、cron、webhook 和 subagent；官方导出本身被定位为需批准、需复核的支持包。自动扫描索引尚可用于未来受控发现讨论，但自动导出和自动上传正文在 P0 明确禁止。

OpenClaw adapter 的失败必须局部化：未安装、CLI 版本不支持、Gateway/store 不可读、Session 已 reset/删除、trajectory capture 缺失、export 超限或 schema 不支持时，只返回该客户端/Session 的明确诊断，不影响 Claude Code、Codex 或其他手动客户端。

## 11. 发布门禁

P0 report-only 发布前至少需要：

1. 锁定一个 OpenClaw 最低版本，并记录 `openclaw --version` 与 `openclaw sessions ... --help`。
2. 三份合法脱敏真实样本：普通 main、渠道会话、subagent/fork；另做 trajectory disabled 样本。
3. 验证 list JSON 完整性、`--limit all`、reset 前后 sessionId 轮换、Session 删除/清理竞态。
4. 验证 bundle schema v1、可选文件缺失、warnings、50 MiB/事件上限和 malformed row。
5. 验证标题/时间/content 质量，以及私密字段不会进入 Canonical Event。
6. 验证临时目录在成功、失败、Ctrl-C 后都清理。
7. 验证 OpenClaw 永远不出现在 auto-sync/upload-all 扫描集。
8. 完整回归 Claude Code/Codex 自动同步、手动上传、Token、成本和报告来源。

Usage 不属于这次 P0 发布门禁；保持 `unavailable` 即为预期行为。
