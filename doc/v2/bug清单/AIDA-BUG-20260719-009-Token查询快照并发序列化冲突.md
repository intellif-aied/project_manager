# AIDA-BUG-20260719-009：Token 查询快照并发序列化冲突

> 优先级：P1
> 状态：未修复
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

当管理端首屏同时请求多个 Token 统计接口，或快照创建与其他 Token 快照/汇总写入事务重叠时，PostgreSQL 为保证串行化一致性会主动取消其中一个事务，返回 SQLSTATE `40001`。当前 API 没有针对该错误做自动重试，因此一次并发冲突直接暴露为 HTTP 500。

该问题与 Session 内容瘦身、MinIO、Digest、旧归档表和本次 cutover 无关；它在迁移前的并发复现中已经存在。

## 3. 修复方案

### 3.1 后端必须修复

1. 只对快照创建事务增加 SQLSTATE `40001` 重试：最多 3 次，退避 50ms、150ms、350ms 加随机抖动；其他数据库错误不得重试。
2. 每次重试必须重新开启完整事务并重新生成快照 Token，不能复用已回滚事务或快照 ID。
3. 达到重试上限后返回稳定错误码 `TOKEN_SNAPSHOT_BUSY`，不要把 PostgreSQL 原始错误直接返回给用户。
4. 记录 `token_snapshot_serialization_retry_total`、最终失败次数和耗时，按 scope、接口统计。

### 3.2 前端调用约束

1. 首次只创建一次 summary 快照；trends、rankings、sessions 必须复用返回的 `query_snapshot_token`。
2. 页面刷新或筛选条件变化时废弃旧 Token，再按同一顺序创建新快照。
3. 不通过无限重试或同时发起多个 summary 请求掩盖后端问题。

### 3.3 可选优化

在确认重试仍不足以解决高并发后，再评估按“用户 + 过滤条件”做短时 singleflight。不得直接用全局锁串行化所有 Token 查询。

## 4. 测试与验收

- 20～50 个并发 summary 请求，HTTP 500 为 0；允许少量请求发生内部重试，但最终应返回 200。
- summary 与 trends/rankings/sessions 并发加载，全部复用同一 `query_snapshot_token`。
- 同时存在 Usage/Rollup 更新事务时，统计接口仍能自动恢复；非 40001 错误不被错误重试。
- 生产连续观察 24 小时：`TOKEN_SNAPSHOT_BUSY` 为 0，40001 未处理错误为 0，Token 统计结果与 Rollup 对账一致。

本问题当前只完成记录和原因确认，未修改代码、未发布修复。
