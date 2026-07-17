# 07：Token 三维统计与对账模型

> 目标：同一个顶层 Session 同时具备“包含 Subagent 的总量、按天、按新增 Chunk”三种统计，并且三种结果都从同一套增量事实生成、可精确对账。

## 1. 三个统计维度

| 维度 | 主键/范围 | 产品含义 |
| --- | --- | --- |
| Session 家族总量 | `root_session_id` | 顶层 Session 自身及全部层级 Subagent 的累计 Token |
| Session 家族按天 | `root_session_id + activity_date` | 该 Session 家族在上海业务日实际新增的 Token |
| 新增 Chunk Token | `root_session_id + member_session_id + chunk_id` | 本次被接受 Chunk 新增的 Token 贡献，不是 Chunk 内看到的累计值 |

“Session 总量”默认指生命周期总量。Token Analytics 的日期筛选返回区间小计，必须明确叫 `range_total_tokens`，不能覆盖或冒充生命周期总量。

本文的 Chunk 明确指 `session_upload_chunks` 中一次被服务端接受的物理增量包，不等同于 Report Source 的业务 slice。若未来需要按 slice 对账，只能按 cursor 汇总其覆盖的 Chunk Contribution，不能把 Token 字段重新塞回 Report Source 候选查询。

## 2. 为什么现有 Component 不能直接支持 Chunk Token

当前 `session_usage_components` 保存一个 Logical Usage Event 的当前有效最终值。以 Claude 为例，同一 `message.id` 可能跨两个 Chunk 从 partial usage 推进到 final usage：

```text
Chunk A：message-1 = 100 Token
Chunk B：message-1 = 160 Token
```

当前 Component 会关闭 100 的旧行，并在 Chunk B 写入 160 的最终行。如果直接按 Component 的 `chunk_id` 汇总，会得到：

```text
Chunk A = 0
Chunk B = 160
```

正确的新增 Chunk 统计应为：

```text
Chunk A = 100
Chunk B = 60
Session 总量 = 160
```

因此不能只扩展 `session_daily_usage`，也不能按当前 Component 的 Chunk 归属直接求和。需要独立、不可变的增量贡献账本。

## 3. 唯一可加总事实：Usage Contribution

建议新增 `session_usage_contributions`。每行表示一个 Logical Usage Event 从上一个可信 Observation 推进到当前 Observation 时新增的 Token 向量。

建议字段：

| 类别 | 字段 |
| --- | --- |
| 版本 | `revision_id`、`generation_id`、`parser_version`、`normalizer_version` |
| 事件推进 | `logical_usage_event_id`、`from_observation_id`、`to_observation_id`、`contribution_kind` |
| 原始归属 | `member_session_id`、`user_id`、组织快照、`chunk_id`、`activity_date`、`occurred_at` |
| 模型 | `provider`、`canonical_model`、`billing_variant` |
| Token | 五类互斥 Token 字段、`total_tokens` |
| 质量 | `quality_status`、`is_estimated`、`assumptions_json` |
| 审计 | `created_at`、Contribution hash/唯一键 |

唯一性建议：

```text
(revision_id, logical_usage_event_id, to_observation_id,
 canonical_model, billing_variant)
```

Contribution 规则：

- Claude 首次 Observation：相对 0 的完整贡献；
- Claude 单调 Advance：只写 `next - previous`；
- Claude Duplicate：贡献为 0，不写新行；
- Codex 累计计数：使用 parser checkpoint 算出的 delta；
- Conflict/负数/无法证明边界：不激活 revision，不猜测补数；
- 跨 Source 的稳定 Provider Event 仍由 `session_usage_event_claims` 确保只有一个 active owner；
- Parser 修正通过新 Metrics Revision 重建，旧 Contribution 不原地修改。

`session_usage_components` 可在迁移期继续作为“逻辑事件当前值”兼容表；Token 统计、成本和三个 Rollup 最终以 active revision 的 Contribution 为唯一可加总来源。

Contribution 不固化 `root_session_id`。父关系可能迟到或修正，root 由指定版本的 `session_family_memberships` 在构建 Rollup 时解析；否则修改父关系会迫使重写不可变 Contribution。

## 4. Session 家族关系

Subagent 拥有自己的 `session_id`，通过 `parent_session_ref` 关联父 Session。查询时递归查父子关系既慢又容易受迟到元数据影响，因此建议维护版本化的 `session_family_memberships`：

```text
root_session_id
member_session_id
parent_session_id
depth
relation_source
relation_version
quality_status
valid_from / valid_to
```

规则：

- 顶层 Session：`root_session_id = member_session_id`；
- 所有层级 Subagent 归入同一个 root；
- parent 必须属于同一用户，且关系不能形成环；
- parent 尚未同步时，子 Session 暂时以自己为 root 并标记 pending；
- parent 后到时生成新 relation version、重建家族 Rollup，并使相关 Token Snapshot 失效；
- 冲突父引用不得任选一个父节点，必须标记 conflict。

默认 Session 列表只展示 root Session 一行，避免父行已含子 Agent、页面又展示子行造成重复求和。需要排查时可下钻成员明细，但成员总量必须标记 `included_in_family_total=true`。

## 5. 三套派生 Rollup

### 5.1 `session_family_token_totals`

粒度：`root_session_id + 用户/组织归属快照 + provider + model + billing_variant`。

除五类 Token 与成本外，保留：

- `self_tokens`；
- `subagent_tokens`；
- `family_total_tokens = self_tokens + subagent_tokens`；
- member/subagent 数量；
- active revision set hash；
- family relation version；
- quality 与数据高水位。

### 5.2 `session_family_daily_usage`

粒度：`root_session_id + activity_date + 用户/组织归属快照 + provider + model + billing_variant`。

`activity_date` 固定使用 `Asia/Shanghai`。跨天 Session 分日累加，所有日期总和必须等于 Session 家族生命周期总量。

### 5.3 `session_chunk_usage`

粒度：`root_session_id + member_session_id + chunk_id + activity_date + 用户/组织归属快照 + provider + model + billing_variant`。一个 Chunk 可包含多个业务日，因此同一 `chunk_id` 可以有多行，Chunk 总量为这些行之和。

Chunk 总量是该 Chunk 引入的 Contribution 之和：

- 不读取客户端上报的 Session 累计总 Token；
- 不把 Chunk 中最后一个累计计数直接当 Chunk Token；
- 重复上传同一 Chunk 不增加 Token；
- generation rebuild 只激活一套 revision；
- 子 Agent Chunk 保留实际 `member_session_id`，同时可汇总到 root。

## 6. 必须成立的对账等式

在相同 active revision set、family relation version、价格版本和数据高水位下：

```text
family_total_tokens
  = self_tokens + subagent_tokens
  = Σ session_family_daily_usage.total_tokens
  = Σ session_chunk_usage.total_tokens
  = Σ active session_usage_contributions.total_tokens
```

并且：

```text
total_tokens
  = uncached_input
  + cache_read
  + cache_write_5m
  + cache_write_1h
  + output
```

全平台、用户、小组、部门汇总必须直接对 Contribution 或互不重叠的 root family Rollup 求和，禁止同时把父 Session 家族行和 Subagent 成员行相加。

`session_activity_slices` 是兼容数据，切换后不得与 Contribution/Rollup 同时求和。

## 7. Subagent 防重复规则

- fork/subagent 复制的父历史只用于建立累计基线，不生成 Contribution；
- Codex `source.subagent.thread_spawn.parent_thread_id` 必须等到可信 `inter_agent_communication_metadata.trigger_turn=true` 后才开始计算子 Agent 新增量；
- 无可信父基线时，第一个累计计数只建立 baseline，不计费；
- 子 Agent 后续 delta 只属于子 `member_session_id`，同时汇入 root family；
- 多层 Subagent 每份 Contribution 在全局只能出现一次；
- 不允许仅按时间或 Token 数值相似度猜测父子关系。

## 8. 写入与激活流程

```text
Chunk accepted
  -> Usage Parser 顺序读取 MinIO
  -> Observation / Logical Event Fold
  -> 生成不可变 Contribution
  -> 构建 Chunk、Daily、Family Rollup
  -> 校验三维对账等式
  -> 原子激活 Metrics Revision + Rollup Version
  -> 失效受影响的 Token Query Snapshot
```

上传请求不等待上述流程。前端通过 pending source/data-through cursor 判断数据是否完整，不能把仍在处理的统计显示成最终值。

## 9. Query Snapshot

`query_snapshot_token` 继续保留，但 Snapshot 应冻结：

- 每个 Session family 的 active revision set hash；
- family relation version；
- 数据截止时间/高水位；
- 权限和组织范围；
- 查询条件；
- 已使用的成本版本。

Snapshot 不再复制高基数 `session_usage_components`。summary、trends、rankings、sessions 读取同一批 Rollup/版本，因此新增 Chunk、父关系补齐或显式重计价都不会让同一页面的四个模块互相矛盾。

## 10. Token API 语义

避免继续使用含义不清的单一 `total_tokens`，Session 结果建议以附加字段过渡：

```text
self_total_tokens
subagent_total_tokens
family_total_tokens
lifetime_total_tokens
range_total_tokens
family_root_session_ref
member_count
quality_status
data_through_cursor
```

过渡期：

- 旧 `total_tokens` 字段不静默改义；
- 新旧字段影子对账后，前端显式切换到 family/range 字段；
- 精确搜索 Subagent 时返回所属 root family，并标记命中的 member；
- MCP ad-hoc `get_sessions` 的旧 Token 字段在完成相同对账前不删除。

## 11. 成本口径

当前 `session_activity_costs` 绑定 Component。R5A 应新增绑定 Contribution 的版本化成本记录（建议 `session_usage_contribution_costs`），或以等价方式让成本明确引用 Contribution；不能继续把 Component 最终值成本直接分配给新增 Chunk。

成本由 Contribution 的 Token 分项按其活动日期、模型、价格版本和汇率版本计算，再生成三套同粒度成本 Rollup：

- family cost = self cost + subagent cost；
- daily cost 之和 = family cost；
- chunk cost 之和 = family cost；
- 未计价 Contribution 仍计入 Token，但成本状态为 pending/unpriced；
- 显式重计价生成新成本版本和 Rollup，普通查询不重新计价。

## 12. 核心测试

| 编号 | 场景 | 通过标准 |
| --- | --- | --- |
| TOK-3D-001 | 根 Session + 3 个 Subagent | family = self + 3 个子树，Contribution 全局只算一次 |
| TOK-3D-002 | 两层嵌套 Subagent | root、parent、depth 正确，无环无重复 |
| TOK-3D-003 | Subagent 复制父历史 | 父历史不生成子 Contribution |
| TOK-3D-004 | trigger_turn=false/true | true 前只建 baseline，true 后才计新增量 |
| TOK-3D-005 | Claude message 跨 Chunk 100→160 | Chunk A=100、Chunk B=60、总量=160 |
| TOK-3D-006 | Codex 累计计数拆成 1/3/20 Chunk | Session、日、Chunk 合计一致 |
| TOK-3D-007 | Session 跨上海业务日 | 日汇总之和等于 family 总量 |
| TOK-3D-008 | 重复上传与 Worker 重试 | 三维统计均不增加 |
| TOK-3D-009 | generation rebuild/revision supersede | 同一时刻只有一套 Rollup 生效 |
| TOK-3D-010 | parent 元数据迟到 | 新 relation 原子替换，Snapshot 内结果稳定 |
| TOK-3D-011 | parent 冲突/环 | 不错误合并，quality 明确为 conflict |
| TOK-3D-012 | 多模型与缓存 Token | 五分项、模型和三维总量一致 |
| TOK-3D-013 | 显式重计价 | Token 不变，三维成本同步切换版本 |
| TOK-3D-014 | 内容清理后 Metering Envelope 重放 | Token/成本不增加、不丢失 |
| TOK-3D-015 | summary/trends/rankings/sessions | 同一 Snapshot 下全部可对账 |

## 13. 发布拆分

### R5A：Contribution 与家族关系

- 新增 Contribution、family membership 和三套 Rollup；
- 继续保留现有 Component、Daily Usage 和查询路径；
- 历史回填与三维影子对账，不影响用户响应。

### R5B：Snapshot 与 API 切换

- Snapshot 改为冻结 revision/family/Rollup 版本；
- Token API 添加明确的 self/subagent/family/range 字段；
- 前端和 MCP ad-hoc 路径分别灰度；
- 统计、权限、成本与性能全部通过后切换。

### R5C：兼容路径下线

- 停止从 Component 物化大 Snapshot；
- 保留审计和旧 Snapshot 到自然过期；
- 旧字段和旧表是否删除需要独立兼容评审，不与 R5B 同时执行。
