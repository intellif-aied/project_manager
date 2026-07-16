# Codex fork Session 重复 Token 统计事故修复方案

## 1. 事故结论

- 事故编号：AIDA-BUG-20260716-001
- 优先级：P0
- 状态：待修复，生产历史数据尚未正式重算
- 影响模块：Aida 客户端上传、后端 Codex Token 解析、Token 分析和成本统计

Codex 创建 fork 或 subagent Session 时，新 JSONL 文件可能包含父 Session 的历史事件和累计 `token_count`。当前客户端没有上传可靠的父引用，后端也没有排除继承历史，导致父 Token 在多个 fork generation 中重复统计。

客户端只补传 `parent_session_ref` 不能解决历史重复量。永久修复必须同时包含客户端元数据修复和后端旧客户端防御。

## 2. 生产影响

### 2.1 已确认案例

用户 1066 已确认：

- 9 个 fork/subagent Session 的 `parent_session_ref` 均为空。
- 2026-07-15 统计 1,783,250,631 Token，其中确认重复 1,005,245,943 Token。
- 2026-07-10 至 2026-07-16 确认重复 2,235,102,122 Token，对应已定价重复成本约 8,807.75 元。
- 解析和 rebuild 均完成，不是任务积压。

### 2.2 其他生产候选

筛选口径：根元数据标识 fork/subagent、数据库父引用为空、单 Session Token 不低于 100,000。

| 工号 | 候选 Session | 候选 Token | 说明 |
| --- | ---: | ---: | --- |
| 1238 | 3 | 997,664,130 | user fork，父 ID 存在 |
| 001898 | 19 | 679,007,525 | subagent，父 ID 存在 |
| 1422 | 24 | 672,516,813 | user fork 与 subagent |
| 1621 | 83 | 239,586,191 | subagent，父 ID 存在 |
| 001753 | 27 | 204,254,323 | 旧格式，多数缺少父 ID |
| 001164 | 3 | 1,425,468 | fork/subagent |

候选 Token 不是确认重复量。生产修正必须逐 Session 建立 fork 基线，不能直接扣除候选总量。低于筛选阈值的异常 Session 仍可能存在。

## 3. 根因

### 3.1 客户端

- `daemon/codex_scan.go` 只解析 `id`、`timestamp` 和 `cwd`。
- 未解析：
  - `source.subagent.thread_spawn.parent_thread_id`
  - `forked_from_id`
  - `parent_thread_id`
  - 旧格式 `thread_source=subagent` 下与 `id` 不同的 `session_id`
- 后续复制进来的父 `session_meta` 会覆盖子 Session 开始时间。
- `uploadSessionGroupIncremental` 只给上传组中第二个以后的文件设置父引用。Codex fork 是独立主文件，因此父引用为空。

### 3.2 后端

- Codex `token_count` 按单 generation 的累计计数解析。
- Codex identity 包含 generation 和 cursor，复制到 fork generation 的父 usage 会获得新 identity。
- claim 机制无法识别跨 fork generation 的继承历史。
- 后端没有从原始根 `session_meta` 恢复父关系和 fork 基线。

## 4. 生产手动调整

### 4.1 定位与限制

这是正式代码发布前的临时止损，不是永久修复。旧客户端再次上传或 generation 重建仍可能恢复重复量。

生产操作要求：

1. 只处理已确认的 fork/subagent Session。
2. 使用事故批次号、事务和记录级备份。
3. 不硬删除 Session、usage、Token 或成本。
4. 不修改已有正常父关系。
5. 代码发布后仍执行正式 metrics revision 重建。

### 4.2 事故清单

为每个候选 Session 保存：

- `incident_id`、`user_id`
- `fork_session_id`、`fork_session_ref`
- `raw_parent_session_ref`
- `forked_at`，取首条根 `session_meta.timestamp`
- 元数据来源、客户端版本和判定方式
- 当前 metrics revision
- 修正前 Token、成本、component 数和日期范围

父引用按以下顺序解析：

1. `source.subagent.thread_spawn.parent_thread_id`
2. `forked_from_id`
3. `parent_thread_id`
4. `thread_source=subagent` 且 `session_id` 与根 `id` 不同

无法取得父 ID 的旧格式 Session 进入人工确认，不能凭 Token 相似直接扣减。

### 4.3 备份

按事故批次备份：

- `sessions`
- `session_metrics_revisions`
- `session_usage_components`
- `session_daily_usage`
- `session_activity_costs`
- `session_usage_event_claims`
- 未过期的 `token_query_snapshots` 及 items

备份包含原主键和批次号，保留到正式 revision 重算验收完成。

### 4.4 计算继承量

按原始文件顺序处理每个 fork Session：

1. 首条根 `session_meta` 确定子 Session、父引用和 `forked_at`。
2. fork 边界以前的父 `token_count` 只更新累计基线，不计入子 Session。
3. 边界后的第一个子计数减去最后一个继承计数，得到子 Session 第一个增量。
4. 后续计数继续按累计差值计算。
5. 时间边界冲突、计数回退或缺少基线时停止自动修正。

禁止全局按 `raw_usage_hash` 去重。必须结合用户、父子关系、fork 时刻和文件顺序判断。

### 4.5 临时写入

1. 回填可确认 Session 的 `sessions.parent_session_ref`。
2. 将确认属于继承历史的 component 通过 `valid_to` 软失效，不删除。
3. 将对应成本记录通过 `superseded_at` 软失效。
4. 根据剩余有效 component 重建受影响用户的 `session_daily_usage`。
5. 使受影响用户未过期的 Token 查询快照失效。
6. 保存每个 Session 修正前后的 Token、成本和 component 数。

不能证明属于继承历史的 component 必须保留并标记待复核。

### 4.6 验收与回滚

验收：

- 1066 已确认重复量从日、周和自定义范围消失。
- Token 汇总、Session、模型、小组、部门和成本维度一致。
- 正常 Codex、Claude Session 和未修正用户统计不变。
- 不出现负数、日期漂移或成本与 Token 不一致。

回滚：

- 按批次恢复 component 的 `valid_to` 和成本的 `superseded_at`。
- 恢复 `sessions` 原父引用和时间。
- 重建 `session_daily_usage` 并使查询快照失效。
- 与修正前备份对账。

## 5. 开发修复

### 5.1 Aida 客户端

修改 `daemon/codex_scan.go`、`daemon/device_client.go`、`daemon/session_sync_client.go`：

1. 给 `SessionInfo` 增加 `ParentSessionRef`、`ForkedAt` 和 fork 来源。
2. 兼容第 4.2 节中的全部父引用格式。
3. 仅首条有效根 `session_meta` 能设置 Session ID、开始时间、目录、父引用和 fork 来源。
4. 后续父 `session_meta` 不得覆盖子 Session 元数据。
5. 上传优先使用 `item.info.ParentSessionRef`；Claude 子文件保留现有上传组回退逻辑。
6. 不增加用户必填项。

### 5.2 后端旧客户端防御

1. Codex 源从 cursor 0 处理时解析首条根 `session_meta`。
2. 请求未传父引用时，从原始元数据恢复父引用、fork 类型和 `forked_at`。
3. 客户端父引用与原始元数据冲突时，将新 metrics revision 标记为 conflict。
4. fork-aware 解析状态跨 chunk 保存继承累计基线。
5. fork 边界前的 usage 更新基线但不生成计费 component。
6. 边界后的第一个计数减去继承基线，只产生子 Session 增量。
7. 无可信基线时不得把完整累计值计费。
8. 独立 Codex 保持 generation/cursor identity，不做全局 hash 去重。

后端元数据识别不能依赖 content projection job 先完成，避免异步任务竞态。

### 5.3 正式历史重算

代码发布后：

1. 对事故清单中的 active generation 触发新 `rebuild_metrics_revision`。
2. 新 revision 使用 fork-aware 解析，旧 revision 正常 supersede。
3. 对新 component 执行成本重算。
4. 重建日汇总并使 Token 查询快照失效。
5. 验收通过后，临时纠偏只保留审计记录。

## 6. 测试方案

### 6.1 客户端测试

- 覆盖嵌套 parent、`forked_from_id`、`parent_thread_id` 和旧格式 subagent。
- 后续父 `session_meta` 不覆盖子 ID、时间、目录和父引用。
- 普通 Codex 不产生父引用。
- Codex fork 作为独立上传项仍发送父引用。
- Claude 主从上传不回归。

### 6.2 后端测试

- 父历史和子新增 usage 在同一 chunk。
- 父历史与子新增 usage 跨 chunk，基线正确续接。
- 第一个子计数只计算相对父基线的增量。
- 重复上传、增量上传和 generation rebuild 后总量不变。
- 旧客户端不传父引用，后端仍能识别。
- 父引用冲突、计数回退、缺少基线时不产生错误计费。
- 普通 Codex、Claude 和模型切换不回归。

### 6.3 脱敏真实 fixture

至少覆盖：

- 1066：Codex Desktop 嵌套 subagent。
- 001898：codex-tui subagent。
- 1422：user fork 与 subagent。
- 001753：旧格式且父 ID 缺失。

fixture 保留事件顺序、根元数据、fork 边界和 `token_count`，删除正文、路径、仓库、提示词和工具结果。

### 6.4 14.157 真实验收

1. 测试账号创建一个父 Codex Session 和至少 3 个 subagent。
2. 每个子 Session 产生独立新增 Token。
3. 旧客户端上传一组，验证后端防御。
4. 修复客户端重复上传同一组。
5. 再执行增量上传和 generation rebuild。
6. 核对用户、Session、模型、小组、部门和成本维度。

通过标准：

- 子 Session 正确关联父 Session。
- 父历史全局只统计一次。
- 子 Session 只统计 fork 后增量。
- 重复上传、增量上传和 rebuild 后 Token、成本不变。
- 日、周、自定义范围和 Session 明细一致。

## 7. 发布顺序

1. 完成客户端、后端和兼容代码。
2. 在 14.157 完成单元、集成和真实 Session 验收。
3. 先发布后端防御，阻止旧客户端扩大事故。
4. 再发布 Aida 客户端并推动升级。
5. 对生产事故清单执行正式 revision 和成本重算。
6. 完成生产多维度验收后关闭事故。
