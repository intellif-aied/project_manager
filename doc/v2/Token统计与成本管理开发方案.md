# Token 统计与成本管理开发方案

## 1. 文档状态

- 状态：开发中；阶段 2B 与阶段 3 已作为当前实现落地，待真实数据对账和部署验收
- 产品基线：`doc/v2/V2产品需求总稿.md` 第 12 至 21.4 章
- 共享契约：`doc/v2/V2核心数据契约.md`
- 前置依赖：Session 原始 Chunk、raw/write generation、可靠处理任务和 Source 状态已实现
- 输出：可信 Token Usage、历史组织统计、价格/汇率快照、API 等价成本和分析页面

### 当前实现边界（2026-07-14）

- 已实现 migration 020 影子表、Claude/Codex Parser、Observation/Event 折叠、Normalizer、Metrics Revision 激活与 Source 级事实 claim。
- 已实现指定 parser/normalizer revision 重建，修正允许原子降低错误 Token，普通追加和 generation 替换禁止丢失已确认事实。
- 已实现内容清除前 Metering Envelope 全 cursor/计数/checksum 门禁、对象物理删除，以及清除后从信封重放 Token。
- 已验证 100 Chunk 乱序/并发 catch-up、跨 Source 稳定事实去重、generation 替换门禁、真实 PostgreSQL/MinIO 清除与重放。
- 已实现组织有效期、活动时点组织快照、价格手册、已审核模型映射、不可变价格/汇率版本、decimal 成本及显式重算审计。
- 已实现服务端物化查询快照、个人/管理分析 API、价格管理 API 和页面；快照固定 Token、成本、显示名称和组织名称，追加、revision 切换、重计价或 Session 删除不会改变有效快照。
- 已完成空数据库 001 至 022 migration、历史调组、总监/TL 权限、精确成本、价格修正、查询快照稳定、无 component pending Source 和 HTTP feature gate 集成验证。
- 未完成旧数据回填、全量 Golden Fixture 对账、全部 `TOK-*` 验收、真实用户验收和部署切换。
- Token 分析 API 和页面不设置用户级开关，统一按用户角色及组织资源鉴权。`AIDA_SESSION_SYNC_USAGE_WORKER_ENABLED` 默认 `false`，仅控制后台解析任务是否消费，不改变接口授权或查询口径。

### 本专题负责

- 服务端 Claude Code、Codex Provider Parser；
- Usage Parser checkpoint、不可变 Usage Observation 和 Logical Usage Event 账本；
- 快照折叠、事件级去重、累计值差分、Token 归一化和质量状态；
- 内容清除前的 Metering Envelope 及重算边界；
- Metrics Revision、影子计算、逐层对账和原子切换；
- 按日期、模型、人员、小组、部门聚合；
- 用户组织有效期和历史归属；
- 模型别名、价格版本、汇率版本、成本快照；
- 我的 Token、Token 使用分析和价格管理页面。

### 本专题不负责

- 不修改 CLI 原始 Chunk 上传协议；
- 不接受客户端 Session 总 Token 作为权威值；
- 不定义个人报告来源选择；
- 不计算 Plus、Pro、Credits 或企业订阅实际账单；
- 不运行外部模型网关参与在线统计；
- 不修改 managed Agent 平台源码。

### 允许修改模块

- `api` 中 Usage Parser、统计、价格、成本和查询；
- parser worker/consumer 中与 Usage 派生有关的代码；
- Token 和价格管理前端；
- migration、回填工具、Golden Fixtures 和测试。

### 禁止顺带修改

- Aida CLI 上传选择和 Chunk 边界；
- Report MCP、默认 Skill 和报告来源 UI；
- 六类报告业务规则。

## 2. 权威数据流

```text
已接收原始 Chunk / 清除后 Metering Envelope
  -> Provider Parser 按 cursor 顺序生成 Usage Observation
  -> 按 Provider 规则折叠为 Logical Usage Event
  -> Versioned Normalizer 只消费当前有效 Observation
  -> 生成可 supersede 的互斥 Token component
  -> Metrics Revision 逐层对账
  -> 原子激活 revision
  -> 按日期/模型/用户/小组/部门查询
  -> 按活动日期匹配价格和汇率
  -> 不可变成本快照
```

CLI 本地显示的 Token 只用于上传前预览。服务端 Token 页面、排名和成本只能读取 active Metrics Revision；报告内容读取使用独立 Content Projection，不以 Token 聚合作为来源。

## 3. 数据模型

共享身份、状态机和唯一性遵循 `V2核心数据契约.md`。本节定义本专题拥有的表。

### 3.1 `session_parser_checkpoints`

```text
id
revision_id
generation_id
provider
parsed_cursor
previous_token_counters_json
counter_segment
active_model
usage_event_watermark
parser_version
normalizer_version
checkpoint_hash
updated_at
```

约束：

```text
UNIQUE(revision_id, provider)
```

checkpoint 与该 cursor 前所有 Observation、Logical Event 和 usage component 在同一数据库事务中推进。任务失败不能只推进 cursor。

### 3.2 `session_usage_observations`

```text
id
revision_id
generation_id
chunk_id
provider
source_start_cursor
source_end_cursor
occurred_at
raw_model
raw_usage_json
parsed_counters_json
raw_usage_hash
parser_version
quality_status
quality_reason
created_at
```

约束：

```text
UNIQUE(revision_id, provider, generation_id, source_start_cursor, source_end_cursor, raw_usage_hash)
```

Observation 一经写入不修改、不删除。该表只保存 usage、模型、时间和必要诊断字段，不复制对话正文。Parser 修正写入新 revision 的 Observation，不原地更新旧 revision。

### 3.3 `session_logical_usage_events`

```text
id
revision_id
generation_id
provider
usage_event_key
identity_strategy
current_observation_id
logical_occurred_at
logical_raw_model
fold_status (current|incomplete|conflict)
observation_count
duplicate_observation_count
advance_count
fold_reason
provider_event_fingerprint
created_at
updated_at
```

约束：

```text
UNIQUE(revision_id, provider, usage_event_key)
```

Logical Event 可以原子推进 `current_observation_id`，但历史 Observation 不改变。推进只更新 Usage 数值，不改变 `logical_occurred_at`、Activity Date、组织归属和模型；语义元数据分叉进入 estimated/conflict。非单调分叉进入 conflict，不允许为同一逻辑事件保留两组 active Component。

### 3.4 `session_usage_event_claims`

```text
id
user_id
provider
provider_event_fingerprint
active_source_id
active_generation_id
active_revision_id
active_logical_usage_event_id
claimed_at
transferred_at
```

约束：

```text
UNIQUE(user_id, provider, provider_event_fingerprint)
```

- 有稳定 Provider ID 的事件必须先 claim 才能进入 active 聚合。
- 同一 Source 的 generation/revision 原子切换可以转移 claim；不同 Source 已持有 claim 时 staging revision 标记 conflict。
- 无稳定 Provider ID 的 Codex 等来源使用经过 Golden Fixture 审核的事实指纹策略；不能证明跨 Source 唯一性时不得标记 exact。

### 3.5 `session_source_metrics_states`

```text
source_id
active_revision_id
target_generation_id
status (ready|pending|rebuilding|error)
active_usage_parsed_cursor
source_high_water_cursor
last_error
updated_at
```

Token 查询只从 `active_revision_id` 读取。raw/write generation 切换或新 Chunk 到达不会自动清空该指针；新 revision 完成高水位追赶和对账后，在同一事务切换指针并更新状态。

`target_generation_id` 是水位所属 generation 的身份锚点。同 generation 追加才能对 cursor 取最大值；跨 generation 切换时 cursor 不可比，必须重置为目标 generation 的水位，禁止使用 `GREATEST` 合并。

### 3.6 `session_metrics_revisions`

```text
id
source_id
generation_id
parser_version
normalizer_version
status (building|validated|active|failed|superseded)
quality_status
build_start_cursor
validated_through_cursor
source_high_water_cursor
scanned_event_count
usage_observation_count
usage_event_count
advanced_observation_count
duplicate_usage_event_count
malformed_event_count
unknown_usage_event_count
conflict_usage_event_count
reconciliation_json
calculation_reason
created_at
validated_at
activated_at
superseded_at
```

数据库必须保证同一 generation 同时最多一个 active revision。

### 3.7 `session_usage_components`

一个 Logical Usage Event 的当前 Observation 可以生成一条或少量模型级贡献：

```text
id
revision_id
logical_usage_event_id
observation_id
chunk_id
session_id
user_id
team_id_snapshot
department_id_snapshot
department_attribution_source
activity_date
occurred_at
provider
raw_model
canonical_model
billing_variant
uncached_input_tokens
cache_read_tokens
cache_write_5m_tokens
cache_write_1h_tokens
output_tokens
normalized_total_tokens
normalization_strategy
is_estimated
assumptions_json
valid_from
valid_to
```

约束：

- Token 字段非负；
- `normalized_total_tokens` 等于五类互斥 Token 之和；
- usage component 必须属于同一 generation 的 revision、Logical Event 和当前 Observation；
- 同一 Logical Event、revision、模型/variant 同时最多一条 `valid_to IS NULL` 的 active 贡献；
- Logical Event 推进 Observation 时旧 Component 与旧聚合在同一事务 supersede，新旧不得同时进入查询。
- `department_attribution_source` 只能是 `direct|via_team|unknown`；直接部门与经小组部门冲突时保留个人 Token，将部门归属设为 unknown 并记录原因，不能任选一个部门，也不能因此阻断个人 Metrics Revision。

### 3.8 `session_daily_usage`

查询加速表，不是新的事实来源：

```text
revision_id
session_id
user_id
team_id_snapshot
department_id_snapshot
activity_date
provider
canonical_model
billing_variant
five_token_fields
total_tokens
quality_status
valid_from
valid_to
```

它只能从 active revision 的 usage component 重建。普通追加或修正不能原地覆盖旧聚合行：关闭旧 `valid_to` 并创建新版本，使查询快照可以读取历史时点。禁止客户端直接写入，禁止与 legacy activity slice 同时求和。

### 3.9 组织有效期

#### `user_team_memberships`

```text
id
user_id
team_id
effective_from
effective_to
source
created_at
```

同一用户有效期不重叠。当前 AIHub 用户资料同步不提供可靠小组历史；本地创建小组、批量导入、调组和移除成员的所有写入口必须在同一事务关闭旧区间并建立新区间。初次迁移从 V2 切换时刻建立当前区间，无法证明的更早归属为 unknown。

#### `team_department_memberships`

```text
id
team_id
department_id
effective_from
effective_to
source
created_at
```

同一小组的部门有效期不得重叠。小组调整部门时关闭旧区间并建立新区间，不能改写已经生成的历史 department snapshot。

#### `user_department_memberships`

```text
id
user_id
department_id
effective_from
effective_to
source
created_at
```

仅记录 PM、总监等直接部门成员。工程师和小组长通过 `user_team_memberships -> team_department_memberships` 解析部门，不重复写直接部门关系。三类关系不组成通用组织树。

### 3.10 价格与汇率

#### `price_books`

```text
id
name
pricing_basis (official_api_equivalent)
source_currency (USD)
display_currency (CNY)
status
```

#### `model_aliases`

```text
provider
raw_model_pattern
canonical_model
status
reviewed_by
reviewed_at
```

未审核 alias 不参与自动计价。

#### `model_price_versions`

```text
id
price_book_id
canonical_model
billing_variant
input_per_million
cache_read_per_million
cache_write_5m_per_million
cache_write_1h_per_million
output_per_million
effective_from
effective_to
source_url
source_checked_at
notes
status
published_by
published_at
supersedes_id
superseded_at
```

#### `usd_cny_rate_versions`

```text
id
rate
effective_from
effective_to
source_url
source_checked_at
notes
status
published_by
published_at
supersedes_id
superseded_at
```

已发布价格和汇率不可原地编辑或删除；当前有效版本的时间范围不得重叠。错误版本只能由管理员发布同一口径、同一有效区间的新版本修正，并通过 `supersedes_id/superseded_at` 关闭旧版本。修正价格或汇率不会自动改写既有成本，只有显式重算 apply 才生成新的 superseding cost。

### 3.11 `session_activity_costs`

```text
id
usage_component_id
price_version_id
rate_version_id
calculator_version
unit_price_snapshot_json
usd_cny_rate_snapshot
estimated_cost_usd
estimated_cost_cny
pricing_status
confidence
assumptions_json
calculation_reason
calculated_at
supersedes_id
superseded_at
```

`calculated_at`/`superseded_at` 构成成本有效区间。同一 usage component 和 calculator version 同时最多一条 active 成本记录；实时查询排除已 superseded 记录，快照查询按有效区间读取。

### 3.12 `session_metering_envelope_manifests` 与 `session_metering_envelope_chunks`

```text
id
generation_id
content_epoch
envelope_version
status (building|validated|failed)
metering_exported_cursor
source_high_water_cursor
source_record_count
potential_usage_record_count
envelope_record_count
source_checksum
envelope_checksum
failure_reason
created_at
validated_at
```

每个 manifest 还必须按 Chunk 保存 cursor 范围、原始记录数、潜在 Usage 记录数和 Source checksum。只有待删除原始对象的每个 generation 都存在 `status=validated`、`metering_exported_cursor=source_high_water_cursor`，且 Source/Envelope checksum 非空、记录数和每 Chunk 覆盖对账通过，才能满足 Session 内容物理清除门禁。

### 3.13 `session_metering_envelopes`

```text
id
manifest_id
generation_id
chunk_id
source_start_cursor
source_end_cursor
provider
usage_event_key
identity_strategy
provider_event_fingerprint
occurred_at
raw_model
raw_usage_json
parsed_counters_json
raw_usage_hash
source_record_hash
quality_status
quality_reason
envelope_version
created_at
```

计量信封从结构扫描中提取所有潜在 usage/token 元数据，只排除对话和工具正文。它必须覆盖 generation 全部已接收 cursor，并保存总记录数、潜在计量记录数和 checksum。内容清除后 Usage Parser 可以从信封创建新 Metrics Revision；信封未覆盖的未知内容语义不承诺可恢复。

### 3.14 `token_query_snapshots` 与物化快照项

首个 summary 请求创建短期服务端快照：

```text
token_query_snapshots
token_query_snapshot_items
token_query_snapshot_members
```

- 客户端只持有随机 opaque token，数据库只保存其 hash；token 绑定认证用户、scope 和全部筛选条件。
- 快照项复制本次查询命中的 Token、成本、人员显示名和历史组织显示名，不在后续分页时重新读取 active revision 或当前组织字段。
- 当前成员表单独物化，用于零 Token 成员和停用成员口径；停用不删除历史聚合，但不进入当前成员排名。
- 快照 TTL 为 15 分钟，同一用户最多保留 10 个活动快照；过期返回 `QUERY_SNAPSHOT_EXPIRED`，参数不一致返回 `QUERY_SNAPSHOT_MISMATCH`。
- `pending_source_count` 按目标日期内尚未追平的原始 Chunk 计算，包括尚未产生任何 usage component 的 Source；管理范围仍按当前权限约束，不能借 pending 状态探测越权 Session。

### 3.15 已验证的真实日志假设

阶段 0 抽样已经确认：

- Claude 样本 14,800 条 assistant usage 中有 4,145 组重复 `message.id`；424 组数值不同且全部组件级单调推进，最终记录为完整最大快照。因此“同 ID 不同 usage 一律 conflict”禁止实现。
- Codex 样本 10,686 条 `token_count` 全部满足 `total_tokens = input_tokens + output_tokens`；`reasoning_output_tokens` 是 output 子集，禁止重复累加。
- 同一批 Codex 样本没有出现累计值下降，因此 reset 分段不能仅凭推测标记 exact，必须补充专项 fixture 或保守降级。

这些数字是设计证据，不是上线验收样本量。正式 Golden Fixtures 必须保存脱敏原始事件、期望 Observation/Event 数和逐字段整数结果。

## 4. Provider Parser

### 4.1 通用规则

- Parser 读取服务端已保存原始 Chunk，不读取 CLI 汇总值。
- 权威 Parser 实现在服务端/worker 可直接依赖的 Go module 中；`daemon` 的本地预览 parser 不能复制成第二套权威实现。若提取共享包，服务端与预览必须运行同一 Golden Fixtures，但只有服务端结果入账。
- 按 generation、cursor 顺序执行。
- 每个 Chunk 解析结果和 parser checkpoint 在同一事务中提交。
- Usage Parser 忽略非计量正文；Session 活动和内容索引由 Session Content Parser 独立负责。
- malformed、unknown 和 conflict 必须计数并影响 revision 质量。
- Parser 任务重试必须产生相同 Observation 身份、Logical Event key 和归一化结果。

### 4.2 Claude Code

权威事件键：

```text
message.id
```

规则：

1. 仅解析具有合法 usage 的 assistant message。
2. 每条合法记录先写不可变 Observation；相同来源 cursor/hash 的任务重试不增加 Observation。
3. 相同 `message.id`、相同 raw usage hash 记录 duplicate，不改变 Logical Event。
4. 相同 `message.id` 的 Usage 比较向量组件级单调增长时推进 current Observation，旧 Component supersede，只计算新快照。
5. 同一 `message.id` 的字段有增有减或不可解释回退标记 conflict，revision 不激活。
6. 解析 `input_tokens`、`cache_creation_input_tokens`、`cache_read_input_tokens`、`output_tokens`；缺少必需字段标记 incomplete。
7. `<synthetic>` 等非真实模型不覆盖当前有效模型。
8. 缺少 message ID 使用核心契约定义的 cursor/hash 回退键并标记 estimated。

不得继续使用“遍历每条 assistant 记录直接累加 usage”，也不得无审计地取第一条、最后一条或逐字段最大值。

### 4.3 Codex

权威事件键：

```text
generation_id + token_count event source cursor
```

规则：

1. `token_count` 按累计快照处理。
2. 使用 parser checkpoint 的 previous counters 计算当前事件差值。
3. 任一累计字段下降时先查找 Provider 明确 reset/turn 边界；只有 Golden Fixture 已证明该边界表示新计费 segment，才结束旧 segment。
4. 有明确 reset 证据的新 segment 第一条快照相对零基线计算，并记录边界、原因和时间。
5. 没有明确 reset 证据的下降不能标记 exact；可解释但不能精确拆分时标记 estimated，字段分叉或无法解释时标记 conflict。
6. `cached_input_tokens` 是 input 子集。
7. `reasoning_output_tokens` 是 output 明细子集，不重复加入 output/total。
8. 如果 Provider `total_tokens` 可用，必须与归一化总量对账。
9. 模型切换事件必须先更新 active model，再把后续 Token 差值归给对应模型；缺少 Token 快照边界时标记 estimated。

## 5. Token Normalizer

### 5.1 Claude 映射

```text
uncached_input_tokens = input_tokens
cache_read_tokens = cache_read_input_tokens
cache_write_tokens = cache_creation_input_tokens
output_tokens = output_tokens
```

如果日志不能区分 5m/1h cache write：

- Token 账本保存总 cache creation；
- usage component 按已审核默认 variant 拆分；
- `is_estimated=true` 并记录假设；
- 缺少审核默认值时 Token 可统计但成本 unpriced。

### 5.2 Codex 映射

```text
uncached_input_tokens = input_tokens - cached_input_tokens
cache_read_tokens = cached_input_tokens
cache_write_5m_tokens = 0
cache_write_1h_tokens = 0
output_tokens = output_tokens
```

`reasoning_output_tokens` 仅保留在 raw usage 诊断中，不再相加。

### 5.3 版本和未知字段

- Normalizer 使用独立版本号。
- 新 Provider schema 不得在旧 normalizer 中猜测映射。
- 未知字段导致可能漏算时 revision 为 incomplete。
- 修复映射通过新 Metrics Revision 重放原始 Chunk；内容已清除时只允许重放 Metering Envelope，不更新旧 component。

## 6. 解析、对账和激活

### 6.1 普通新增 Chunk

1. Session 专题接收原始 Chunk 并投递 `parse_usage_chunk` job。
2. Parser 从 checkpoint 顺序解析。
3. 幂等写 Observation、折叠 Logical Event，并只为 current Observation 生成 active component。
4. 重算受影响的 activity date/materialized usage。
5. 执行事件、Chunk、日期、模型、Session 五层对账。
6. 成功后提交 component 和 checkpoint。
7. Logical Event 单调推进时，旧 component、旧日期贡献和旧成本在同一修正链路 supersede；新旧不得同时进入查询。
8. 计价任务异步消费新增 active component；计价完成前返回 `pricing_pending`。

### 6.2 完整重建或 Parser 修正

1. 创建 building revision，保存 `build_start_cursor=source.expected_cursor`。
2. 从目标 raw/write generation cursor 0 重放所有原始 Chunk；内容已清除时从完整 Metering Envelope 重放。
3. 不读取旧 revision component 作为输入。
4. 完成去重、归一化和全量对账。
5. 读取当前 `source.expected_cursor`，增量追赶构建期间新增 Chunk，直到 `validated_through_cursor` 等于当前高水位。
6. 生成修正前后预览：Session 数、Token 增减、成本影响、质量变化。
7. 激活事务锁定 Source/generation，再次比较高水位；发生变化则退出激活并继续 catch-up。
8. 高水位一致且逐层对账通过后原子切换 active。
9. 旧 revision 和成本记录 supersede，退出查询；锁释放后的新增 Chunk只进入新 active revision。
10. 内容恢复产生的新 generation 仍按同一 Source 完整替换。激活前使用旧 active revision/Metering Envelope 核对清除前已确认事实：新日志允许追加事实，但缺少或冲突的既有事实不能替换旧 Metrics Revision。激活事务同时切换 Source metrics 指针并转移该 Source 的 event claim；旧 revision 退出查询后新 revision 才能生效，不能按 generation 把两者相加。

Parser 修正允许总量下降。普通重试和普通追加不允许覆盖或降低已确认历史贡献。

### 6.3 失败行为

- 原始 Chunk 保持已接收，不要求用户重新上传。
- 当前可信 active revision 继续提供查询。
- Session 标记存在 `metrics_pending` 或 `metrics_error`。
- 页面不把旧值描述为包含最新活动。
- 修复后服务端重放原始 Chunk；内容已清除时仅重放已校验 Metering Envelope。
- Token API 返回 `usage_parsed_cursor/source_high_water_cursor` 和 `metrics_pending|metrics_error`，不得隐去统计滞后。

## 7. 活动日期、模型和子 Agent

- 所有 Activity Date 按核心契约转换。
- Token 差值归属于 usage 事件时间。
- 跨日期/模型且无法拆分的差值标记 estimated，不能伪装 exact。
- 多模型 Session 使用模型级 component，禁止按最后模型归集整日 Token。
- sub-agent 独立 Session 单独统计；父/子日志可能包含相同 usage 时必须通过事件身份或 Provider fixture 去重。
- 活动状态、Token 质量状态、计价状态分别保存；不能用 `total_tokens > 0` 判断是否有工作活动。

## 8. 组织统计口径

### 8.1 历史归属

- 每个 usage component 固化 `user_id`、`team_id_snapshot`、`department_id_snapshot` 和 `department_attribution_source`。
- 工程师/小组长先根据事件时间匹配 `user_team_memberships`，再匹配 `team_department_memberships`；PM/总监匹配直接 `user_department_memberships`。
- 没有可匹配组织区间时个人统计保留，小组/部门归属显示 unknown，不使用当前用户或小组字段静默回填。
- 用户调组或小组调部门后，旧活动仍留在原小组/部门历史，新增活动进入新组织。
- 用户停用后历史 Token、趋势和成本保留。
- Admin 单人更新、批量导入、调组、移除成员和部门调整均调用同一 membership 写服务；禁止只修改 `users.team_id`、`users.department_id` 或 `teams.department_id`。
- 直接 SQL 修复必须先执行区间重叠检测并生成审计，不作为日常组织维护路径。
- 历史 membership 只有管理员通过“预览 -> 确认”修正；修正后重建受影响的 team/department snapshot 和组织聚合，但个人 Token 事件和总量不变，并保留修正前后区间与操作者。

### 8.2 权限

- `scope=mine`：所有角色只看自己。
- `scope=management`：小组长、总监、管理员可用，PM/工程师返回 403。
- 小组长：当前管理小组及所选日期内归属于该小组的历史 component。
- 总监：当前 `departments.director_user_id=当前用户.id` 的部门；统计读取所选日期内 `department_id_snapshot` 属于该部门的 component。
- 管理员：全平台，可按 department/team/user 缩小。
- `departments.director_user_id` 是当前总监权限的唯一权威来源；`teams.director_user_id` 不参与 V2 授权。
- 人员 Session 下钻必须再次验证所选日期内的部门/小组归属和当前管理权限。
- 停用人员不进入当前成员人数和当前成员排名，但历史总量/趋势不能因此降低。

## 9. 成本计算

### 9.1 产品口径

- 计算的是官方 API 等价成本，不是 Plus、Pro、Credits 或企业合同账单。
- 平台审核发布的价格和汇率是唯一权威计算输入。
- 外部开源模型价格表、AI 查询和供应商页面只生成待审核建议，不能自动发布。
- Aida 服务运行不依赖外部价格服务可用性。

### 9.2 计算公式

所有单价按每百万 Token 保存：

```text
cost_usd =
  uncached_input_tokens / 1_000_000 * input_price
  + cache_read_tokens / 1_000_000 * cache_read_price
  + cache_write_5m_tokens / 1_000_000 * cache_write_5m_price
  + cache_write_1h_tokens / 1_000_000 * cache_write_1h_price
  + output_tokens / 1_000_000 * output_price

cost_cny = cost_usd * usd_cny_rate
```

使用 Logical Usage Event 当前 Observation 的 Activity Date 匹配价格和汇率版本。数据库计算使用固定精度 `NUMERIC/DECIMAL`，禁止 float/double；分项先以未展示舍入值求和，最后按产品展示精度舍入。保存实际单价、汇率、舍入规则和 calculator version 快照。

### 9.3 计价状态

| 状态 | 含义 |
|---|---|
| `pricing_pending` | Token 已进入 active revision，但成本水位尚未追平；金额为 `null` 或仅返回明确的已计价部分 |
| `priced` | 全部 active Token 已计价 |
| `partially_priced` | 部分模型/variant 缺价格，返回已计价金额和未计价 Token |
| `unpriced` | 全部无法计价，金额为 `null` |

Token quality 非 exact 时额外返回质量状态。`pricing_pending`/`unpriced` 不能显示为 0 元；incomplete Token 不能冒充完整成本。API 不得把旧 revision 的成本拼接到新 active revision 的 Token。

### 9.4 修正

- 新价格不重写已有历史成本。
- 迟到历史活动使用对应 Activity Date 的已发布价格/汇率新增成本。
- 显式修正价格、汇率或 calculator 时创建 superseding cost。
- 重算必须幂等，同一 component 同时只有一条 active cost。

## 10. API

### 10.1 我的 Token

```http
GET /api/v1/token-analytics/summary?scope=mine&from=YYYY-MM-DD&to=YYYY-MM-DD
GET /api/v1/token-analytics/trends?scope=mine&from=...&to=...&query_snapshot_token=...
GET /api/v1/token-analytics/rankings?scope=mine&from=...&to=...&group_by=model&query_snapshot_token=...
GET /api/v1/token-analytics/sessions?scope=mine&from=...&to=...&page=1&page_size=20&query_snapshot_token=...
```

- 默认最近 7 天，前端必须显式传递不可清空的日期范围。
- 统计卡片和 Session 明细使用同一日期范围。
- 返回 Token quality、pricing status、未计价 Token 和人民币 API 等价成本。
- 跨 Session 查询返回 `metrics_snapshot_at`、绑定用户和筛选条件的 `query_snapshot_token`、`pending_source_count`、`pricing_pending_source_count` 和 `data_freshness`；不能返回单一 `metrics_revision_id` 冒充全局版本。
- 每条 Session 明细可返回自身 `revision_id`、`usage_parsed_cursor`、`source_high_water_cursor` 和 `cost_calculated_cursor`，用于定位局部滞后。
- 任一 Source 的 `usage_parsed_cursor < source_high_water_cursor` 时整体返回 `data_freshness=pending`，总量只能描述为“已统计部分”，不能描述为所选范围完整总量。
- 前端先请求 `/token-analytics/summary` 建立 snapshot，再将返回的 `query_snapshot_token` 传给 Session 明细、趋势和排名；token 与认证用户、scope、from/to、model/team/user/q 等筛选绑定，参数不一致返回 400。
- 所有 Token 整数和 USD/CNY 金额在 JSON 中返回十进制字符串；前端不得先转 JavaScript Number 再聚合或排序。
- 当前 Token 页面统一使用 `/token-analytics/*`；旧 `/tokens` 和 `/tokens/sessions` 只保留接口兼容，前端不得按用户回退，也不得把两套结果相加。

### 10.2 管理分析

```http
GET /api/v1/token-analytics/summary
GET /api/v1/token-analytics/trends
GET /api/v1/token-analytics/rankings
GET /api/v1/token-analytics/sessions
```

统一参数：

```text
scope=management
from/to
team_id
department_id
user_id
model
q
page/page_size
query_snapshot_token
```

- 缺失或非法 scope/group_by 返回 400，禁止 default/fallthrough 扩大权限。
- 部门、小组、人员、模型排名与卡片使用同一 active revision 和组织归属口径。
- 同一次管理查询必须固定 `metrics_snapshot_at/query_snapshot_token`；分页过程中继续读取原快照，快照 TTL 到期返回 `QUERY_SNAPSHOT_EXPIRED` 并要求从第一页重查，不能跨 revision 拼页。
- 管理页面先请求 summary 建立 snapshot，trends/rankings/sessions 必须复用返回 token；不能并行发起四个无 snapshot 的独立首请求后再在前端拼接。
- `query_snapshot_token` 必须是服务端存储的随机句柄或带完整性保护的 opaque token，不接受客户端可篡改的 revision/time/filter JSON；设置明确 TTL 和最大活动快照数量。
- 排名人数少于 10 返回完整有序列表，不拼重复 top/bottom。
- Token 并列按最近活动时间和显示名称稳定排序。
- 成本排名只比较 priced 数据；其他状态单独列出。

### 10.3 Session 搜索

- `q` 支持真实 `session_ref` 精确匹配和 summary 关键词。
- 完整 Session ID 命中可跨当前日期/模型筛选定位，但仍受管理权限限制。
- 返回实际活动范围和 `search_mode`。
- 无结果不能泄露无权限 Session 是否存在。
- 内容已清除的 Session 可按 ID 查询统计，但不参与旧 summary 搜索。

### 10.4 价格管理

只有管理员可用：

```http
GET/POST /api/v1/admin/price-books
GET/POST /api/v1/admin/model-aliases
GET/POST /api/v1/admin/model-price-versions
GET/POST /api/v1/admin/exchange-rate-versions
POST /api/v1/admin/pricing/import-suggestions
GET /api/v1/admin/pricing/unpriced-models
GET /api/v1/admin/pricing/recalculation-runs
POST /api/v1/admin/pricing/recalculate/preview
POST /api/v1/admin/pricing/recalculate/apply
```

所有发布和重算 apply 记录操作人、来源、原因和影响预览。

## 11. 页面

### 11.1 我的 Token

所有角色都有：

- 最近 7 天不可清空日期选择；
- 总 Token、输入、缓存、输出、活跃天数；
- 人民币 API 等价成本和计价状态；
- 按日期趋势、模型分布；
- Session 列表、搜索和逐日明细；
- exact/incomplete/estimated/conflict 提示。

### 11.2 Token 使用分析

小组长、总监、管理员可见：

- 管理范围总量和趋势；
- 部门、小组、人员、模型排名；管理员可查看部门排名，总监固定为本部门；
- 零 Token 当前成员；
- 停用/调组历史数据说明；
- 下钻到成员 Session；
- priced、partially_priced、unpriced 分开呈现。

PM 和工程师不显示入口，直接请求返回 403。

### 11.3 价格管理

管理员可见：

- 模型 alias 待审核；
- 当前和历史价格版本；
- USD/CNY 汇率版本；
- 外部/AI 建议与人工确认；
- 未计价模型列表；
- 重算预览、应用和审计。

普通用户只读人民币结果，不展示价格编辑入口。

## 12. 迁移与上线

### 阶段 1：Golden Fixtures 与 Parser 原型

从真实日志建立脱敏 fixture，固定以下期望：Claude 完全重复与单调快照、Claude 非单调分叉、Codex 跨 Chunk 累计、可证明和不可证明的 counter reset、cache、reasoning、多模型、跨日、sub-agent、malformed 和 unknown schema。先在无数据库的纯 Parser/Reducer 测试中通过，再开始 migration。

### 阶段 2：新账本影子计算

1. 新建表和 parser worker。
2. 对回填和测试账号 Chunk 生成 Observation、Logical Event 和 Metrics Revision。
3. 在切换当前 Token 页面前完成旧值、新值和 Golden 预期对账；切换后旧 API 只作兼容，不参与页面统计。
4. 输出按 Session 的旧值、新值、Golden 预期、差异原因和质量状态。

旧值不是正确性基准；Golden Fixture 和逐层对账才是基准。

当前代码已完成本阶段的数据结构、Parser/Reducer、Revision 激活、Metering Envelope、物理清除基础和新 Token 页面。Usage worker 启停仍是运行控制，且尚未完成旧数据全量回填和真实数据对账，因此不表示阶段 2 已完成部署验收。

### 阶段 3：组织和成本

1. 初始化当前组织有效期，不伪造未知历史。
2. 发布已审核价格和汇率。
3. 为 active usage component 生成成本快照。
4. 核对个人、小组、部门和全平台聚合。

当前代码已完成阶段 3 实现：migration 022、组织区间触发器、价格/汇率修正、成本 supersede、重算审计、物化查询快照、权限 API 和前端页面均已落地。空库集成测试已证明调组前后历史归属、总监/TL 权限、decimal 成本、重计价后旧查询快照稳定，以及尚无 component 的 pending Source 不会被漏报。当前无用户级分析开关；尚未进行旧数据回填、真实用户对账和部署验收。

### 阶段 4：切换

1. 影子统计、报告内容投影和 MCP 契约共同通过发布门槛。
2. 完成旧入口最后一次增量 catch-up，并确认所有 Source/Revision 高水位一致。
3. Token API、Report MCP 和 Chunk CLI 最低版本门禁在同一发布窗口切换。
4. 删除 legacy slice 与新 usage 同时求和路径。
5. 保留 revision 和成本审计，不保留两套页面口径。

## 13. 测试与验收

### Parser 与幂等

- `TOK-001`：真实 Claude fixture 中重复 `message.id` 只计算一次。
- `TOK-002`：Claude 同一 message ID 的 `0 -> partial -> final` 单调 Observation 只计算 final，旧 Component 不进入聚合。
- `TOK-003`：Codex 累计值拆成 1、3、20 个 Chunk 结果一致。
- `TOK-004`：Parser 任务重试不增加 Observation、Logical Event 或 active component。
- `TOK-005`：malformed/unknown usage 不被静默标记 exact。
- `TOK-006`：本地状态丢失和换电脑不影响服务端 parser checkpoint。

### 归一化与对账

- `TOK-007`：Codex cached input 从 input 中扣除一次。
- `TOK-008`：Codex reasoning 不重复加入 output。
- `TOK-009`：Claude input/cache create/cache read/output 分立。
- `TOK-010`：各事件、Chunk、日期、模型、Session 总量精确相等。
- `TOK-011`：多模型 Session 不归给最后模型。
- `TOK-012`：跨日无法精确拆分的累计差值标记 estimated。
- `TOK-013`：父/子 Agent fixture 不重复统计。

### Revision 与修正

- `TOK-014`：building/failed/superseded revision 不进入查询。
- `TOK-015`：Parser 修正可以降低错误 Token，并原子替换旧 revision。
- `TOK-016`：普通追加只影响新事件所属日期。
- `TOK-017`：内容恢复不增加第二份 Token 和成本。

### 组织、权限和查询

- `TOK-018`：用户调组、小组调部门前后 Token 分别留在对应历史小组和部门。
- `TOK-019`：用户停用后历史总量/趋势不下降，当前人数/排名排除该用户。
- `TOK-020`：总监只能查看当前 `departments.director_user_id` 对应部门，不能通过 team/user 参数扩大范围。
- `TOK-021`：伪造 team/user 下钻返回 403。
- `TOK-022`：完整 Session ID 搜索受权限限制且可跨日期定位。
- `TOK-023`：统计卡片、趋势、排名和明细在相同范围下总量一致。

### 价格与成本

- `TOK-024`：未审核价格和汇率不参与计算。
- `TOK-025`：价格/汇率切换前后按 Activity Date 使用不同快照。
- `TOK-026`：价格后续更新不改变历史成本。
- `TOK-027`：重算 apply 幂等，同一 component 只有一条 active 成本。
- `TOK-028`：unpriced 返回 `null`，不显示 0 元。
- `TOK-029`：外部价格建议未经管理员发布不影响任何成本。
- `TOK-030`：USD/CNY 公式、分项、Session、人员、小组和部门汇总一致。

### 并发、清除与接口一致性

- `TOK-031`：Claude 同一 message ID 的非单调字段分叉产生 conflict，revision 不激活。
- `TOK-032`：同一 message ID 的 Observation 跨多个 Chunk 时，与单 Chunk 完整解析结果逐字段一致。
- `TOK-033`：building revision 期间并发追加至少 100 个 Chunk，激活后 `validated_through_cursor=source_high_water_cursor` 且不重不漏。
- `TOK-034`：清除前从原始 Chunk 与清除后从 Metering Envelope 重建，Observation/Event/component 和逐日结果一致。
- `TOK-035`：Session 任一待删除 generation 的 Metering Envelope 缺 cursor、计数或 checksum 时，禁止物理删除全部原始对象。
- `TOK-036`：Token 已激活而成本未完成时返回 `pricing_pending`，不拼接旧成本且不显示 0 元。
- `TOK-037`：卡片、趋势、排名、Session 明细和导出固定同一 query snapshot；revision 切换期间不跨版本拼页。
- `TOK-038`：Admin 单人更新、批量导入、调组、移除成员四条路径均生成连续且不重叠的 membership 区间。
- `TOK-039`：Usage parser lag 时 API 返回已统计水位和 pending，刷新完成后只增加/修正受影响范围。
- `TOK-040`：Parser worker lease 过期、重启、重复投递和 dead-letter 重放不改变最终 Token。
- `TOK-041`：父/子或不同 Source 重复稳定 Provider 事件时只允许一个 active claim；同 Source generation/revision 切换正确转移 claim。
- `TOK-042`：无稳定跨 Source ID 的 fixture 未证明不重叠时不能标记 exact。
- `TOK-043`：同一 Claude message 的 final Observation 在后续 Chunk/次日到达时，增量仍归属于 Logical Event 原发生日期和模型；元数据分叉不静默迁移历史 Token。
- `TOK-044`：首次统计请求后并发追加/修正 Usage，复用 query snapshot 的卡片、趋势、排名、明细和分页结果保持不变；新 snapshot 才看到新值。
- `TOK-045`：query snapshot token 被其他用户使用、修改筛选或过期时确定失败，不扩大权限也不自动换新口径。
- `TOK-046`：raw generation 切换后新 revision 构建失败/未追平时，source metrics 指针继续提供旧可信统计并标记 rebuilding；成功后原子切换且不双计。
- `TOK-047`：显式修正历史 membership 后只重建组织归属/聚合，不改变个人 Token 总量，并保留修正审计。
- `TOK-048`：总量超过 `2^53-1` 时数据库聚合、JSON 十进制字符串、前端显示和排序逐位一致。
- `TOK-049`：价格与汇率使用 decimal 计算，分项和总成本在规定舍入点一致，不因请求顺序或聚合层级产生浮点误差。
- `TOK-050`：PM/总监直接部门归属与工程师/TL 经小组归属产生相同口径的 department snapshot；冲突关系保留个人 Token，但不进入任一部门聚合。
- `TOK-051`：总监变更后旧总监立即失去管理查询权限，新总监可查看该部门历史统计，但历史 Usage 事实和 department snapshot 不被改写。
- `TOK-052`：内容恢复新 generation 的 revision 覆盖全部清除前既有事实时原子替换并正确转移 claim；缺少/冲突既有事实时不激活，同一查询快照始终只包含一版 Token 和成本。
- `TOK-053`：任意已认证账号的 Token capability 均按服务可用性返回 enabled，不依赖用户 ID 配置；employee、TL、总监、admin 仍严格按角色和组织资源控制管理查询，worker 停止时返回 pending/滞后状态而不是回退旧页面。

### 自动化验证方式

1. Golden Fixtures 对每个 Provider 固定原始事件数、Observation 数、Logical Event 数、五类 Token、日期、模型和质量状态。
2. Property Test 随机改变 Chunk 边界、批次大小、重试次数和任务顺序，最终结果必须与一次完整顺序解析逐字段相等。
3. SQL invariant test 检查负数、active revision 重复、Logical Event 多个 active Component、聚合不平和跨 revision 混合。
4. Concurrency/chaos test 覆盖上传与 rebuild 并发、worker 崩溃、数据库事务回滚和对象存储短暂失败。
5. 影子对账按 Session、日期、模型、用户、小组和部门维度输出差异；旧值只用于发现差异，不作为正确性基准。

当前已自动化覆盖 `TOK-001`、`TOK-002`、`TOK-003`、`TOK-004`、`TOK-005`、`TOK-007`、`TOK-008`、`TOK-009`、`TOK-010`、`TOK-014`、`TOK-015`、`TOK-017`、`TOK-031`、`TOK-032`、`TOK-033`、`TOK-034`、`TOK-035`、`TOK-040`、`TOK-041`、`TOK-046` 和 `TOK-052` 的 Parser/Revision 当前后端边界。

阶段 3 空库 PostgreSQL 集成测试另覆盖：`TOK-018` 的用户调组历史归属、`TOK-020` 的总监部门权限、`TOK-023` 的卡片/趋势/排名/Session 同快照对账、`TOK-030` 的单价/汇率/人民币成本公式、`TOK-037/TOK-044` 的追加和重计价后旧快照稳定、`TOK-039` 的零 component pending Source、以及价格版本不可原地修改和组织区间重叠拒绝。HTTP 单元测试覆盖统一服务可用性、认证/角色边界、管理员价格权限和 `QUERY_SNAPSHOT_*` 错误码。

上述测试只证明列出的边界，不代表完整 `TOK-001` 至 `TOK-053` 已通过。多模型、超大整数、价格跨日期、停用成员、总监变更、全量 Golden Fixtures、前端浏览器交互和真实用户数据仍必须单独执行。

### 发布门槛

1. `CORE-001` 至 `CORE-031` 全部通过。
2. `TOK-001` 至 `TOK-053` 全部通过。
3. 真实 Claude/Codex Golden Fixtures 零未解释差异。
4. 影子运行中不存在 active revision 重复、负 Token、详细分项大于 Provider 总量或旧新链路双计。
5. 权限、调组、停用和彻底删除回归通过。

满足 Golden Fixtures、Parser 原型和数据结构审查后，本专题才能标记为“可开发”；满足全部发布门槛后才能切换生产 Token 查询。
