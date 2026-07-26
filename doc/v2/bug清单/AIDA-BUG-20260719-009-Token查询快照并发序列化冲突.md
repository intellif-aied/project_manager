# AIDA-BUG-20260719-009：Token 查询快照并发序列化冲突

> 优先级：P1
> 状态：已修复并发布生产，真实并发验收通过
> 环境：生产
> 发现时间：2026-07-19

## 1. 现象

生产请求：

```text
GET /api/v1/token-analytics/summary?scope=management&from=2026-07-17&to=2026-07-19
```

偶发返回：

```json
{"error":"pq: could not serialize access due to read/write dependencies among transactions (40001)"}
```

生产日志证据：同一时间窗口一次 HTTP 500，随后相同 Token 统计请求恢复 HTTP 200；此前 `scope=mine` 并发窗口也出现过同类 500。失败请求事务会回滚，没有发现数据写坏。

## 2. 根因

`/token-analytics/summary` 每次首次请求都会创建查询快照。快照创建使用 PostgreSQL `SERIALIZABLE` 事务，并写入 `token_query_snapshots` 及其引用数据。

当管理端首屏同时创建全局、部门、小组或个人等不同范围的 Token 查询快照时，多个事务会读取重叠的 Active Rollup，再分别写入自己的快照引用。快照创建原先使用 `SERIALIZABLE`，PostgreSQL 会把这种正常并发识别为可能破坏串行化顺序的读写依赖，并主动取消其中一个事务，返回 SQLSTATE `40001`。

2026-07-26 生产再次发生：三个管理范围 Summary 在同一秒创建快照，其中全局三日范围请求在 7.86 秒后返回 HTTP 500。代码已有 50ms、150ms 两次短退避，但生产管理查询持续约 3～4 秒，三次事务尝试仍处于其他事务执行窗口内，最终继续暴露 PostgreSQL 原始错误。因此此前实现属于部分缓解，不是完整修复。

该问题与 Session 内容瘦身、MinIO、Digest、旧归档表和本次 cutover 无关；它在迁移前的并发复现中已经存在。

## 3. 修复方案

### 3.1 后端必须修复

1. 快照创建事务固定使用 `REPEATABLE READ`。它保证一次事务内所有来源读取来自同一 MVCC 快照，同时避免 `SERIALIZABLE` 对正常并发快照创建产生不必要的读写依赖冲突。
2. 快照仍在一个事务内创建：Snapshot、Members、Rollup References 和统计计数必须原子提交，不能拆成部分可见结果。
3. 仅对 SQLSTATE `40001` 保留三次有限重试，退避 50ms、150ms、350ms，并增加不超过基础退避 25% 的随机抖动；其他数据库错误不得重试。
4. 每次重试重新开启完整事务并重新生成快照 Token，不能复用已回滚事务或快照 ID。
5. 达到重试上限后记录原始数据库错误，并返回稳定错误 `TOKEN_SNAPSHOT_BUSY`；HTTP 状态固定为 503，不向用户暴露 PostgreSQL 原文。

### 3.2 前端调用约束

1. 首次只创建一次 summary 快照；trends、rankings、sessions 必须复用返回的 `query_snapshot_token`。
2. 页面刷新或筛选条件变化时废弃旧 Token，再按同一顺序创建新快照。
3. 不通过无限重试或同时发起多个 summary 请求掩盖后端问题。

### 3.3 可选优化

在确认重试仍不足以解决高并发后，再评估按“用户 + 过滤条件”做短时 singleflight。不得直接用全局锁串行化所有 Token 查询。

## 4. 测试与验收

- 20～50 个不同管理范围的并发 Summary 请求，HTTP 500 为 0；允许少量请求发生内部重试，但最终应返回 200。
- summary 与 trends/rankings/sessions 并发加载，全部复用同一 `query_snapshot_token`。
- 同时存在 Usage/Rollup 更新事务时，统计接口仍能自动恢复；非 40001 错误不被错误重试。
- 生产连续观察 24 小时：`TOKEN_SNAPSHOT_BUSY` 为 0，40001 未处理错误为 0，Token 统计结果与 Rollup 对账一致。

## 5. 2026-07-26 开发结果

- `CreateSummary` 的 Rollup V2 快照事务已改为 `REPEATABLE READ`；快照内容、统计口径和 Token 复用流程未改变。
- 重试固定为 50ms、150ms、350ms 加 0～25% 抖动；重试耗尽返回 `ErrSnapshotBusy`。
- HTTP 层已固定映射为 `503 + TOKEN_SNAPSHOT_BUSY`。
- 单元测试覆盖事务隔离级别、四次事务尝试后稳定失败和错误原文不泄露。
- 独立 PostgreSQL 临时库中，全局、部门、小组、个人四种过滤条件共 24 个并发 Summary 全部成功。
- 测试服与生产均已部署。生产内部测试账号覆盖四种合法管理过滤条件，24 个并发 Summary 全部返回 200；同一快照 Token 的 Trends、Rankings、Sessions 均返回 200。
- 生产发布后日志未出现 `40001`、HTTP 500、`TOKEN_SNAPSHOT_BUSY`、panic 或 deadlock；PostgreSQL 无 Lock 等待类型。
- 生产 24 小时持续观察尚未完成，不影响本次实时验收结论。
