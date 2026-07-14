# Session 增量上传与内容管理开发方案

## 1. 文档状态

- 状态：开发中；核心协议、Golden Fixtures、Prepare/Chunk/Finalize、Content Projection 影子链路、CLI 增量上传与内容生命周期入口已落地，Usage 解析和物理清除仍待下一专题
- 产品基线：`doc/v2/V2产品需求总稿.md` 第 6 至 9、19、21 章
- 共享契约：`doc/v2/V2核心数据契约.md`
- 上游输入：Claude Code、Codex 本地 JSONL 日志
- 下游输出：稳定 Session 身份、active raw/write generation、不可变原始 Chunk、内容投影、独立处理水位和可靠处理任务

### 本专题负责

- CLI 专用认证、Session 发现、分页和上传交互；
- Source、Generation、Chunk 的增量同步；
- 服务端权威上传游标、连续性和幂等；
- 原始内容持久化、内容事件投影及 Usage 解析任务投递；
- 通用 worker/outbox、租约、重试、死信和处理水位；
- summary 诊断、macOS 展示；
- 内容清除、显式恢复和管理员彻底删除。

### 本专题不负责

- 不计算服务端权威 Token；
- 不定义 Usage Observation/Event 折叠、Token 归一化或成本公式；
- 不定义个人报告来源参数；
- 不修改 managed Agent 平台源码；
- 不实现报告内容压缩。

### 允许修改模块

- `cmd/aida`、`daemon` 中 Session 扫描、CLI 展示和上传客户端；
- `api` 中 Session 同步、内容管理、对象存储和任务投递；
- `web` 中 Session 内容管理入口；
- 对应 migration、测试和部署配置。

### 禁止顺带修改

- Report MCP、默认 Skill 和 managed Agent 平台；
- Token 页面、价格管理和成本计算；
- 六类报告生成业务规则。

### 当前实现边界（2026-07-14）

- 已实现：认证用户统一使用 Prepare/Chunk/Finalize、对象校验、连续 cursor/hash、幂等 ACK、Content Projection worker、lease/retry/dead-letter、CLI 专用认证、CLI 本地游标、流式上传、分页/跨页选择、`sessions --json` 诊断和 80 列终端展示。
- 已实现：`clear-content` 受理后递增 `content_epoch`、立即关闭内容读取并投递 `build_metering_envelope`；`restore-content` 只在 `cleared` 后建立限时恢复授权，CLI 自动建立 restore generation。
- 已由 Token 专题实现：Usage Parser、Metering Envelope、原始对象物理删除及其完整性门禁；本专题不重复定义 Token 统计口径。
- `clear-content` 对认证用户统一开放，并受 Metering Envelope 完整性门禁保护；不得人工改状态或提前删除对象。
- `AIDA_SESSION_SYNC_CONTENT_WORKER_ENABLED` 和 `AIDA_SESSION_SYNC_USAGE_WORKER_ENABLED` 仅用于后台 worker 启停，默认 `false`；它们不控制用户是否可访问 Session Sync API。

## 2. 目标行为

1. 同一 Session 长期使用时平台主列表始终只有一条记录。
2. 首次上传按多个原始 Chunk 导入，后续只上传新增完整行。
3. 无新增内容时不发送历史日志。
4. 网络超时后重试不重复保存、不重复触发有效统计。
5. CLI 升级、本地状态丢失和换电脑后从服务端进度继续。
6. 文件截断或重写不会拼接到旧 generation。
7. 用户清除内容后普通 upload 不自动恢复。
8. 内容、Token/成本和已保存报告遵循核心契约中的独立生命周期。

## 3. 数据模型

所有状态机和唯一性以 `V2核心数据契约.md` 为准。本节只定义本专题拥有的结构。

### 3.1 `sessions`

调整 Session 唯一约束：

```text
UNIQUE(user_id, agent_type, session_ref)
```

主要字段：

```text
id
user_id
agent_type
session_ref
parent_session_ref
started_at
last_activity_at
cwd
project_name
content_status (available|clearing|clearing_failed|cleared|deleted)
content_epoch
active_source_count
created_at
updated_at
```

`summary`、`excerpt`、模型和 Token 不再作为 Session 身份的一部分。`started_at` 不用于最近活动排序。

### 3.2 `session_sources`

```text
id
session_id
source_role
source_key
active_generation_id
staging_generation_id
active_content_projection_revision_id
created_at
updated_at
```

约束：

```text
UNIQUE(session_id, source_role)
```

`active_generation_id` 是当前 raw/write generation，不等于报告当前读取的内容版本。`source_key` 使用逻辑身份，例如 `codex:{session_ref}:main`，不得包含绝对路径、inode、主机名或设备 ID；它只在所属 Session 内用于来源校验，不能建立跨用户全局唯一约束。

### 3.3 `session_source_generations`

```text
id
source_id
status (staging|active|superseded|abandoned)
expected_cursor
prefix_checkpoint_hash
prefix_checkpoint_algorithm_version
prefix_checkpoint_state
prefix_checkpoint_state_format
source_size
started_at
finalized_at
superseded_at
```

- generation ID 由服务端创建。
- active generation 普通追加直接推进 `expected_cursor`。
- staging generation 只有 finalize 后才能替换 raw/write active；内容仍读取旧 `active_content_projection_revision_id`，直到新 projection validated 后原子切换。
- `prefix_checkpoint_state` 和 `prefix_checkpoint_state_format` 是服务端内部的版本化 SHA256 增量状态，不对客户端返回，也不接受客户端写入；服务端用它在已验证 Chunk 上推进完整前缀 hash。状态缺失、格式不兼容或与 hash 不一致时必须从已保存对象重放恢复，恢复前不得继续 ack。

### 3.4 `session_upload_chunks`

```text
id
generation_id
start_cursor
end_cursor
start_line
end_line
content_sha256
content_epoch
event_start_at
event_end_at
raw_object_key
object_status (pending|available|delete_pending|deleted)
content_index_status (pending|processing|indexed|failed)
usage_parse_status (pending|processing|parsed|failed)
accepted_at
```

约束和错误码直接使用核心契约第 4、14 章。

### 3.5 `session_content_projection_revisions`

```text
id
generation_id
content_parser_version
status (building|validated|active|failed|superseded)
build_start_cursor
content_indexed_cursor
source_high_water_cursor
event_count
malformed_event_count
created_at
validated_at
activated_at
superseded_at
```

- 同一 generation 同时最多一个 active content projection revision。
- 候选 Session、来源快照和 MCP 只能读取 active revision 且范围不能超过 `content_indexed_cursor`。
- 内容 Parser 修正创建 staging revision，不原地修改已被 report selection 引用的 revision。
- staging projection 首轮构建后追赶新增 Chunk；只有 `content_indexed_cursor=source.expected_cursor` 时才可 validated。
- 激活事务锁定 Source 并重新比较高水位；发生变化则继续 catch-up，不得带缺口切换 `active_content_projection_revision_id`。

### 3.6 `session_content_events`

逻辑结构如下；实现可以按消息、工具和摘要拆表，但对外契约必须一致：

```text
id
content_projection_revision_id
chunk_id
source_start_cursor
source_end_cursor
occurred_at
event_type
summary
excerpt
content_payload
content_sha256
created_at
```

约束：

- 同一 revision 的 cursor 范围稳定、连续且可回溯到原始 Chunk；
- summary、excerpt、消息、工具和提交均来自该 revision，不从 Token Daily Usage 反推；
- 内容事件不得作为 Token 事实来源；Usage 字段由 Token 专题独立解析；
- 内容物理清除时删除可读 payload 和索引，selection 审计只保留不可读的范围与计数。

### 3.7 `session_content_tombstones`

```text
id
session_id
cleared_by
cleared_at
reason
last_active_generation_ids
restored_at
restored_by
```

同一 Session 同时最多一条未恢复 tombstone。清除请求受理后先进入 `clearing` 并立即禁止读取；计量门禁完成后进入 `cleared`，失败进入 `clearing_failed`。

未恢复 tombstone 同时保存最小恢复状态：

```text
restore_status (none|waiting_upload|building|failed|restored)
restore_generation_id
restore_requested_at
restore_expires_at
```

一次只允许一个有效恢复流程，不引入独立设备租约。

### 3.8 `session_processing_jobs`

统一承载内容索引、Usage 解析投递和对象清理：

```text
id
job_type (index_content_chunk|parse_usage_chunk|rebuild_content_revision|rebuild_metrics_revision|build_metering_envelope|delete_object|purge_session)
session_id
generation_id
chunk_id
target_revision_id
content_epoch
payload
status (pending|leased|retry_wait|completed|dead)
attempts
max_attempts
lease_owner
lease_until
heartbeat_at
next_retry_at
last_error
created_at
started_at
completed_at
```

数据库状态和对象存储不能假设为一个事务。事务内写业务记录和 job，worker 幂等执行外部对象操作。运行约束：

1. worker 使用 `FOR UPDATE SKIP LOCKED` 或等价机制领取任务，并使用 lease/heartbeat 防止进程退出后永久卡住。
2. 同一 `generation_id + projection_type` 使用 advisory lock 或等价串行锁，严格按 cursor 顺序处理；不同 Session 可并行。
3. 解析 worker 作为独立部署进程，不放入 API 请求 goroutine，也不与定时报告 runner 共用无界并发池。
4. 必须配置全局并发、单用户并发、任务超时、最大尝试次数和 dead-letter 人工重试入口。
5. 监控至少包含队列深度、最老 pending 时长、parse lag、dead 数量、单任务耗时和内存峰值。
6. Chunk 接收使用流式读取，并配置压缩体、解压后体积、单行体积和解压倍率上限；具体默认值必须在真实最大日志 benchmark 后写入部署配置和契约测试，不允许沿用整包上传的内存读取方式。
7. 会写入可读内容或改变内容状态的任务必须携带创建时的 `content_epoch`；执行写入前重新比较 Session 当前 epoch，不一致时以 stale 完成且不写入。

## 4. CLI 本地状态

默认位置：

```text
~/.aida/upload-state.json
```

每个本地 source 保存：

```text
session_ref
agent_type
source_key
generation_id
last_acked_cursor
prefix_checkpoint_hash
prefix_checkpoint_algorithm_version
last_acked_chunk_hash
last_prepare_at
last_upload_at
```

要求：

- 权限 `0600`；
- 临时文件写入后原子替换；
- 服务端确认 accepted/duplicate 后才推进本地游标；
- 本地状态不是权威，不保存服务端 Token parser checkpoint；
- 本地状态丢失时先调用 prepare；
- 本地文件短于服务端进度时停止并提示来源不完整；
- prefix checkpoint 不一致时进入显式 generation 重建，不能从头重复追加。
- 本地状态可信且 generation/cursor 与服务端一致时直接续传；换电脑或状态丢失时按服务端算法重新计算已确认前缀摘要。

## 5. Chunk 生成

1. 以原始文件字节偏移作为 cursor。
2. Chunk 必须由完整 JSONL 行组成。
3. 上限同时受完整事件条数和压缩后字节数控制，达到任一上限结束。
4. Chunk 可以跨业务自然日，不在午夜强制切分。
5. 文件末尾半行本次跳过，下次扫描补传。
6. Chunk 内容 hash 使用未压缩原始字节计算，压缩方式变化不改变幂等身份。
7. CLI 不向服务端提交权威 `activity_contributions`、Session 总 Token 或 Daily Usage。
8. CLI 可解析 summary、Token 供本地列表预览，但服务端查询只使用服务端派生结果。

## 6. 同步 API

### 6.1 Prepare

```http
POST /api/v1/session-syncs/prepare
```

请求：

```json
{
  "client_version": "...",
  "sessions": [
    {
      "session_ref": "...",
      "agent_type": "codex",
      "parent_session_ref": null,
      "started_at": "2026-07-14T01:00:00Z",
      "last_activity_at": "2026-07-14T03:00:00Z",
      "cwd": "/workspace/project",
      "project_name": "project",
      "sources": [
        {
          "source_role": "main",
          "source_key": "codex:019f...:main",
          "local_size": 1310720,
          "prefix_checkpoint_hash": "...",
          "prefix_checkpoint_algorithm_version": "sha256-prefix-v1"
        }
      ]
    }
  ]
}
```

响应使用与批量请求对应的 `results` 数组；每个 Source 一项：

```json
{
	"results": [{
	  "session_ref": "...",
	  "source_key": "...",
	  "generation_id": "...",
	  "generation_status": "active",
	  "expected_cursor": 1048576,
	  "prefix_checkpoint_hash": "...",
	  "prefix_checkpoint_algorithm_version": "sha256-prefix-v1",
	  "content_status": "available",
	  "action": "append|unchanged|rebuild_required|content_cleared|restore"
	}]
}
```

服务端根据 prefix checkpoint 决定继续 active generation 或要求创建 staging generation。客户端不能自行增加 generation 版本号。

Session 为 `clearing|clearing_failed` 时始终拒绝上传；`cleared` 且没有有效恢复请求时返回 `content_cleared`。用户已在 Web 确认恢复时，prepare 自动返回该 Session 的恢复 staging generation 和 `action=restore`，不要求用户手工复制内部 ID。

### 6.2 Chunk 上传

```http
POST /api/v1/session-chunks/batch
```

使用 multipart 上传，`metadata` 字段描述所有文件；文件超过内存阈值时由 HTTP 层落临时文件，再逐个流式校验并写对象存储，禁止把多个大 Session 一次读入内存。每个 Chunk metadata：

```json
{
  "generation_id": "...",
  "file_field": "chunk_0",
  "content_encoding": "identity|gzip",
  "uncompressed_size": 262144,
  "start_cursor": 1048576,
  "end_cursor": 1310720,
  "start_line": 1201,
  "end_line": 1500,
  "content_sha256": "...",
  "event_start_at": "...",
  "event_end_at": "..."
}
```

逐项响应也放在 `results` 数组中：

```json
{"results": [{
  "status": "accepted|duplicate|rejected|content_cleared",
  "generation_id": "...",
  "content_sha256": "...",
  "acked_cursor": 1310720,
  "expected_cursor": 1310720,
  "content_index_status": "pending",
  "usage_parse_status": "pending",
  "error_code": null
}]}
```

当前受控实现限制单请求 256 MiB、单 Chunk 压缩体 16 MiB、解压后 64 MiB、最大解压倍率 100；这些值不是最终产品默认值，阶段 2 必须依据真实最大日志 benchmark 再确定部署配置。

接收事务：

1. 校验用户、Session、generation 和游标；
2. 传输层允许压缩；服务端流式解压并执行压缩体积、解压后体积和倍率限制，将校验后的未压缩原始字节写入由 generation、cursor 范围和 hash 决定的对象 key，再校验原始 size/hash 且对象可读取；
3. 开启数据库事务，按固定顺序锁定 Session 和 generation，重新检查 `content_status/content_epoch`，并用 `expected_cursor=start_cursor` 做 CAS；
4. 幂等写 `object_status=available` 的 Chunk metadata，推进 generation 上传游标；
5. 写携带当前 `content_epoch` 的 `index_content_chunk` job，并独立写 `parse_usage_chunk` job；Usage 解析不以内容 epoch 作为统计失效依据；
6. 数据库提交后返回 ack。

对象写入成功但数据库失败时不推进 cursor，由孤儿清理任务在确认没有 Chunk 记录引用该 object key 后删除对象；数据库提交后响应丢失时，相同范围和 hash 的重试返回 duplicate。不同 cursor 范围允许使用相同内容 hash。

解析失败不要求客户端重新上传相同原始 Chunk，服务端可以使用已保存对象重试。

### 6.3 Finalize

```http
POST /api/v1/session-syncs/{generation_id}/finalize
```

请求：

```json
{
  "declared_end_cursor": 1310720,
  "prefix_checkpoint_hash": "...",
  "prefix_checkpoint_algorithm_version": "sha256-prefix-v1"
}
```

首次导入或来源重建时调用。校验：

- 从 0 到 declared end cursor 连续；
- 所有 Chunk 原始对象 available；
- 文件末尾 checkpoint 一致；
- 当前用户和 source 未发生冲突。

Finalize 只切换 raw/write generation，不直接切换 `active_content_projection_revision_id` 或 Token active Metrics Revision。新内容投影 validated 后独立切换内容指针；Token 指针由 Token 专题独立切换。准备期间旧可信内容/Token 继续服务并明确显示 rebuilding/pending。

恢复流程中，Finalize 后 Session 仍保持 `cleared` 且 `restore_status=building`。只有新 Content Projection validated 并原子切换内容指针后，才设置 `content_status=available`；Metrics Revision 独立完成对账和切换，不阻塞内容重新可读，也不能与旧 revision 同时计入查询。

### 6.4 阶段 1 运行控制

- Prepare/Chunk/Finalize 是认证用户的统一当前链路，不设置用户级白名单，也不按环境区分两套产品行为。
- Session Sync、Token 查询和 Report MCP 必须遵循同一 Source/Revision 契约；不得通过前端回退把新旧统计结果相加。
- 一旦某 Session 已创建 `session_sources`，旧 `/sessions/batch` 对该 Session 返回 `CLI_UPGRADE_REQUIRED`，避免整包上传覆盖增量状态；未进入 V2 的 Session 继续走旧链路。
- Content Projection worker 和 Usage worker 分别由 `AIDA_SESSION_SYNC_CONTENT_WORKER_ENABLED`、`AIDA_SESSION_SYNC_USAGE_WORKER_ENABLED` 控制；开关只用于运行维护、故障止损和任务排查，不参与用户授权。

### 6.5 Processing Job 租约

数据库领取使用 `FOR UPDATE SKIP LOCKED`，每次领取原子递增 `attempts` 并写 `lease_owner/lease_until/heartbeat_at`。仅租约所有者可 heartbeat、complete 或 fail；租约过期可被其他 worker 领取，达到 `max_attempts` 后进入 `dead`，不会自动再次执行。

Content Projection worker 按 revision 串行检查 cursor：乱序任务进入短延迟重试，重复任务不重复写事件，旧 `content_epoch` 任务作为 stale 完成且不写内容。只有 `content_indexed_cursor == generation.expected_cursor` 时才能把 building revision 激活；active generation 后续追加仍写同一 active revision，并在新 Chunk 索引完成前保留真实 pending 水位。

## 7. CLI Session 列表

### 7.1 默认范围和分页

- `aida sessions`：显示最近 48 小时有活动的 Session；
- `aida sessions --all`：显示全部本地历史；
- 排序：`last_activity_at DESC, session_ref ASC`；
- 默认每页 20 条，支持上一页、下一页和跳页；
- Session 超过 500 条仍按流式索引或分页读取，不能一次完整载入全部日志内容。

### 7.2 固定展示字段

分页改造后不得丢失：

```text
Agent
最近活动时间
Session ID
summary
项目/CWD
模型
持续时长
Token 本地预览
sub-agent 数量
同步状态
```

本地 Token 显示必须标注为本机日志预览；上传后平台权威值由服务端解析产生。

### 7.3 Summary

- `aida sessions --json` 输出 `summary`、`summary_status` 和 `summary_source`；
- `summary_status` 至少区分 `ok|empty|parse_error|unsupported`；
- Claude/Codex parser 使用同一优先级：首条有效用户消息，过滤系统块和纯命令噪声；
- 交互表格必须先为 summary 保留最小宽度，再压缩 CWD/模型等次要列；
- macOS 80 列终端必须看到可识别的 summary，不得只靠颜色区分状态。

## 8. `aida upload` 交互

- 候选集为本次固定扫描中全部存在待上传内容的 Session。
- 分页只用于浏览，不改变候选集。
- 跨页选择必须保留。
- “选择全部待上传”覆盖全部页面并显示总数。
- `aida upload --all` 检查全部本地可发现 Session，仅同步有新增内容的来源；不受最近 48 小时和当前页影响。
- 上传前展示选中 Session 数和估算新增字节，不展示为重新上传全部历史。
- 部分失败不回退成功项，结束时列出成功、无变化、已存在、内容已清除和失败数量。

## 9. CLI 专用认证

- CLI 专用同步路由可以沿用“不因 JWT `exp` 自动失效”的内部约定。
- 每次请求仍验证签名、算法、用户标识、本地用户存在、`local_enabled=true`、账号未停用且允许 Aida。
- 跳过 `exp` 只能存在于 CLI 专用认证中间件，不能影响 Web 或其他 API。
- CLI 升级不得清除已有 Token；更换电脑输入同一有效凭证后可以读取服务端同步进度。

## 10. 内容清除、恢复和彻底删除

### 10.1 用户清除内容

```http
POST /api/v1/sessions/{id}/clear-content
```

事务内：

1. 锁定 Session 并校验所有者；
2. 创建 tombstone，递增 `content_epoch`；
3. 设置 `content_status=clearing`，立即关闭 prepare/chunk、summary、excerpt、下载、报告候选和 MCP 内容读取；
4. 为 Session 下每个仍有原始对象的 Source Generation 投递 `build_metering_envelope`，逐个校验 `metering_exported_cursor == expected_cursor`、事件计数和 checksum；
5. 门禁通过后删除可读内容投影并为原始对象写 `delete_object` job；
6. 所有原始对象删除完成后设置 `content_status=cleared`；
7. 保留核心契约规定的 Metering Envelope、Usage、Token、成本和业务关联。

计量信封或对象删除失败时进入 `clearing_failed`。此状态仍禁止内容读取，但保留原始对象供服务端重试，不能为了完成清除而破坏 Token 重算能力。

普通 upload 返回 `CONTENT_CLEARED`，不能自动恢复。

阶段 1 只实现上述事务的第 1 至 4 步和立即不可读门禁。第 5 至 7 步由 Token 专题提供 Metering Envelope 后再启用；在此之前接口保持 `clearing`，不能用超时或人工状态修改绕过门禁。

### 10.2 显式恢复

```http
POST /api/v1/sessions/{id}/restore-content
```

1. 只有 `content_status=cleared` 可以恢复。用户在 Web 二次确认后递增 `content_epoch`，把未恢复 tombstone 设置为 `waiting_upload` 并记录过期时间；页面提示运行 CLI 重新上传。
2. CLI prepare 根据同一用户、Session 和未过期 tombstone 自动创建或复用 staging generation，返回 `action=restore`。
3. CLI 从本地完整原始来源按普通 Chunk 协议上传并 finalize；本机没有完整日志时返回明确错误，Session 继续保持 cleared。
4. 内容投影 validated 后原子切换内容指针，将 `content_status` 设置为 available，tombstone 标记 restored。
5. Token 层从新 generation 建立 Metrics Revision，完成同 Source 对账、active 指针替换和 event claim 转移后再激活；旧、新 revision 和成本不得同时进入查询。
6. 任一步失败记录原因并允许重新发起恢复；新尝试再次递增 epoch，旧 staging generation 标记 abandoned，不暴露 staging 内容，也不把普通 upload 当作恢复成功。

### 10.3 管理员彻底删除

删除前 API 返回影响预览：用户、Session ID、日期范围、Token、成本、对象、报告、任务和需求关联。管理员填写原因并二次确认后执行；数据库使用事务，外部对象使用 outbox，保留审计记录。

该接口在阶段 1 不注册。必须等 Token/成本影响预览和 Metering Envelope 完成后实施，避免提供一个看似可用但无法证明可重算性的危险删除入口。

## 11. 迁移与切换

迁移期间不能出现“新 CLI 上传成功，但旧 Token 页面和 Report MCP 查不到”的双轨空窗。采用以下唯一发布顺序：

1. 先部署表、worker、内容投影和 Usage 链路；部署前用测试账号完成端到端验证。
2. 迁移前检查 `(user_id, agent_type, session_ref)` 重复和来源对象冲突，输出合并清单；存在无法确定归属或原始对象的 Session 不进入本批切换。
3. 将现有原始对象回填为 Source/Generation/Chunk，并生成内容投影和 Metrics Revision；回填完成并对账后才创建新唯一约束，旧 API 继续提供线上查询。
4. 对回填数据完成 Token 对账、报告 MCP 契约回归和最终增量 catch-up。
5. 测试账号验证 Chunk CLI、Token 查询和 Report MCP 使用同一 Source/Revision 数据后，统一发布当前链路；不建立按用户双轨开关。
6. 切换窗口短暂停止旧写入或使用等价写入锁，完成最终 catch-up 后，同时启用 Token 查询、Report MCP 内容读取和 Chunk CLI 最低版本门禁。
7. 已进入 Chunk 模式的 Session 拒绝旧接口覆盖并返回 `CLI_UPGRADE_REQUIRED`；未完成切换时不得 ack 只有新链路可见的数据。
8. 稳定期结束后删除整包上传、`replaceActivitySlices` 和 legacy slice 查询路径，禁止两套统计求和。

回填必须输出用户/Session/Source/原始对象四级清单。原始对象缺失、损坏或无法连续导入的 Source 必须标记为需要重新上传/人工处理，不得把未完成迁移的数据混入完整统计，也不得把新旧统计求和。

普通用户首次 V2 Chunk ack 后不允许把查询回退到不包含该 Chunk 的 legacy 数据并声称完整。故障时应暂停新写入、显示 pending/error，并从已保存 Chunk forward-fix/replay；旧原始对象和 legacy 表在稳定期结束前保留，只用于核对和切换前回退。

如实施团队选择迁移期写 legacy 兼容投影，必须另行评审双写事务、对账和删除方案；不得在开发中临时增加未经契约定义的双写。

## 12. 测试与验收

- `SES-001`：大 JSONL 从 cursor 0 分成多个完整行 Chunk。
- `SES-002`：末尾半行不推进游标，补全后只上传一次。
- `SES-003`：相同 Chunk 超时重试返回 duplicate。
- `SES-004`：游标缺口、重叠和相同范围不同 hash 分别返回确定错误。
- `SES-005`：本地状态丢失后从服务端进度续传。
- `SES-006`：换电脑且文件前缀一致时续传，不一致时要求重建。
- `SES-007`：文件截断创建 staging generation，不覆盖 active。
- `SES-008`：500 个 Session 分页、跳页和跨页选择正确。
- `SES-009`：`upload --all` 覆盖全部页面且只上传新增内容。
- `SES-010`：Linux 和 macOS 80 列终端 summary 可见，JSON 诊断字段完整。
- `SES-011`：Chunk 接收成功、解析任务失败时不要求重新上传原始 Chunk。
- `SES-012`：内容清除进入 clearing 后立即不可读，计量信封完整后才物理删除，Token/成本仍存在且可重建。
- `SES-013`：普通 upload 不能恢复，显式恢复成功且不重复统计。
- `SES-014`：管理员彻底删除有影响预览、二次确认和审计。
- `SES-015`：CLI 专用凭证超过 `exp` 后仍可同步，签名错误或用户停用时拒绝。
- `SES-016`：内容索引失败不影响原始 Chunk ack，但候选 API/MCP 不读取超过 `content_indexed_cursor` 的范围。
- `SES-017`：Usage 解析失败不阻断内容索引；页面分别显示内容可用与 Token pending/error。
- `SES-018`：worker 在 lease 过期、进程重启和重复领取后不重复内容事件或 Usage 贡献。
- `SES-019`：同一 generation 的乱序并发任务被串行化，不越过 cursor 推进 checkpoint。
- `SES-020`：超大压缩比、超限单行和超限解压体被确定拒绝，API 进程内存不随整包大小线性增长。
- `SES-021`：旧链路回填与最终 catch-up 后，新旧 Session 集合、活动范围和 summary 零未解释差异。
- `SES-022`：普通用户使用新 CLI 前，Token 查询和 Report MCP 已同时切换或有经过评审的兼容投影，不存在上传成功但页面/MCP 不可见。
- `SES-023`：raw generation 切换时内容/Token 指针不随之提前切换；新 projection/revision 分别 validated 后原子换指针。
- `SES-024`：content projection 重建期间并发追加至少 100 个 Chunk，激活前追平 Source 高水位，MCP 快照不漏范围。
- `SES-025`：回填存在缺失/损坏对象时用户保持 legacy，不发生新旧来源混算；清单数量与数据库/对象存储逐项一致。
- `SES-026`：不同 cursor 的两个 Chunk 内容 hash 相同也能正常接收，只有相同范围才按 hash 判定 duplicate/conflict。
- `SES-027`：两个客户端并发上传同一 expected cursor 时只有一个 CAS 成功，最终 cursor 连续且任务不重复。
- `SES-028`：对象成功而数据库回滚时不 ack；提交后响应丢失的重试返回 duplicate。
- `SES-029`：clear-content 与上传、内容索引、来源快照并发时，旧 epoch 任务不能重新写入可读内容。
- `SES-030`：Web 确认恢复、CLI 完整重传、内容投影激活和 Token revision 替换全流程成功且不重复统计。
- `SES-031`：恢复中断或本地日志不完整时继续保持 cleared，不暴露 staging 内容。
- `SES-032`：旧 Session 存在身份重复或对象冲突时阻止切换并输出明确清单，不创建错误唯一约束。

上述用例和核心契约 `CORE-001`、`CORE-005`、`CORE-012`、`CORE-015`、`CORE-016`、`CORE-019`、`CORE-024`、`CORE-027` 至 `CORE-030` 通过后，本专题才可标记为“可开发”。
