# FINALIZE 增量校验与执行方案

## 1. 目标

在不重新扫描 321 万条历史记录、不长时间阻塞业务的前提下，删除已经不再使用的 `session_content_events_payload_archive`，完成空间回收，并将 compaction 状态置为 `finalized`。

## 2. 当前状态

- 当前 compaction state 为 `swapped`。
- `session_content_events` 是线上使用的新表，无 `content_payload`。
- `session_content_events_payload_archive` 是切换前的旧表，仅用于回滚。
- 切换前的全量对账已经通过；切换后的新增写入由回滚窗口镜像触发器同步到两张表。
- 上次 finalize 失败的原因是：持有排他锁时再次执行全量深度对账，触发 statement timeout；事务已回滚，未删除数据。

## 3. 核心原则

1. 已通过的历史全量对账结果不得在 finalize 中重复执行。
2. finalize 只校验上次全量对账基准点之后的增量数据。
3. 增量校验必须在锁外完成；锁内只做状态确认、触发器确认和删除。
4. 任何校验失败、基准点缺失、触发器缺失或锁等待超时，都必须保持 `swapped`，不得删除旧表。
5. 禁止手工 `DROP TABLE` 绕过工具保护。

## 4. 开发改动

### 4.1 持久化校验基准

在 compaction state 中记录最后一次通过的校验基准，至少包括：基准建立时间、当前表和归档表的最大事件游标/最大事件 ID、基准范围内的行数和内容摘要、校验状态和生成时间。

基准必须来自已经通过的全量 `verify`，不能由 finalize 临时猜测。

### 4.2 增量校验

finalize 的锁外阶段只读取基准点之后的记录，按稳定主键匹配新旧表，校验新增记录数量、主键、Session、chunk、cursor、事件类型、内容哈希和 MinIO 引用，并确认没有 missing、extra、mismatch。

增量校验结果写入一次性 finalize 校验令牌，并绑定当前 compaction state、表名和触发器定义版本。令牌过期、状态变化或触发器变化时必须重新校验。

### 4.3 锁内 finalize

拿到短时 `ACCESS EXCLUSIVE` 锁后只执行：确认 state 仍为 `swapped`、确认增量校验令牌有效、确认两侧回滚镜像触发器存在、删除镜像触发器和旧归档表、删除镜像函数、重命名约束和索引、更新 state 为 `finalized`。

锁内禁止执行全表 `COUNT`、深度对账或逐行读取。

## 5. 测试要求

- 已有全量基准 + 少量新增记录：只扫描增量，finalize 成功。
- 增量出现 missing、extra 或 mismatch：finalize 拒绝，旧表保留。
- 校验期间继续写入：重新生成增量校验，不使用过期令牌。
- 触发器缺失、state 非 `swapped`、令牌过期：拒绝删除。
- 锁等待超时：事务回滚，state 仍为 `swapped`。
- finalize 中途失败后重试：不破坏两张表，可继续执行。
- 生产规模副本验证：确认锁内阶段不执行全表扫描，锁等待和锁持有时间满足门限。

## 6. 生产执行顺序

1. 发布包含本改动的 API 镜像。
2. 测试服完成上述测试并留存结果。
3. 生产重新备份数据库并校验备份可读。
4. 生成并确认最新增量校验结果。
5. 安排短维护窗口执行 finalize。
6. 验证 state、表、触发器、空间和日报/周报/Session/Token/MCP 接口。
7. 验收通过后才关闭 `FINALIZE_PENDING`。

## 7. 完成标准

- 不再重复 321 万条历史记录的锁内全量对比；
- finalize 锁内只执行短事务；
- 旧归档表已删除且无数据丢失；
- state=`finalized`；
- 当前表无 `content_payload`；
- 业务接口和上传链路正常；
- 数据库空间已回收并记录前后大小。
