# AIDA-BUG-20260725-018：Aida 上传成功但 Session 不可见并触发数据库死锁

> 优先级：P0
> 状态：生产发布与真实上传验收通过，已关闭
> 发现日期：2026-07-25
> 影响范围：Aida CLI 增量上传、Session 列表、Content Projection、Token Usage 后台处理

## 1. 问题

生产环境使用 Aida CLI `0.1.21` 选择两个顶层 Session 上传。CLI 长时间停留在：

```text
正在上传 2 个 Session...
```

上传最终返回成功，但用户在服务端当天列表中只能看到一个顶层 Session。该现象包含两个 P0 缺陷：

1. 历史 Session 增量上传成功后，服务端数据已经 ready，但当天 Session 列表无法查询；
2. Chunk 上传与 Token Usage 后台处理存在固定锁顺序反转，生产数据库连续死锁，直接造成 Chunk 500 和长时间等待。

数据库中存在不能作为关闭条件。CLI 返回成功后，用户实际使用的列表接口必须能够查询本次成功上报的顶层 Session。

## 2. 生产证据

### 2.1 实际上传范围

CLI 选择的两个顶层 Session 为：

```text
019f8327-b7bf-7ed0-b13c-96d3fd1f0f13
019f974b-cfee-77c2-b65f-4edd2a4a06dc
```

第一个顶层 Session 包含四个 Subagent，因此实际处理六个来源，合计约 103 MB、92 个 Chunk。生产数据库确认六个来源最终均为：

```text
generation_status = active
content_status = available
```

### 2.2 用户可见缺失

第一个顶层 Session 在 2026-07-21 首次上传，本次是增量更新；第二个在 2026-07-25 首次上传。

增量上传成功后，第一个 Session 只更新了 `updated_at`，`uploaded_at` 仍是历史时间。现有 Session 列表固定使用：

```sql
DATE(s.uploaded_at) = :date
ORDER BY s.uploaded_at DESC
```

因此用户在 2026-07-25 当天列表中无法看到当天成功增量更新的历史 Session。

### 2.3 上传耗时与死锁

第一条大 Session 从 21:31:42 开始上传，到 21:32:56 才 Finalize，约 74 秒。期间：

- 正常 Chunk 耗时为几十毫秒；
- 拥塞后单个 Chunk 上升到 1 至 6 秒；
- 21:31:44 的 `/session-chunks/batch` 返回 HTTP 500；
- CLI 串行提交 Chunk，并在 5xx 后有限重试；
- Finalize 后继续等待 Content Projection ready。

生产 PostgreSQL 同期连续记录 `deadlock detected`：

```text
Chunk 上传事务
  -> 锁 sessions
  -> 锁 session_source_generations

Token Usage 后台事务
  -> 通过 Metrics Revision、Generation 外键先持有 Generation 相关锁
  -> 写 session_usage_components 时通过外键获取 sessions 锁
```

形成固定锁顺序反转：

```text
上传持有 sessions，等待 generation
Usage 持有 generation，等待 sessions
```

PostgreSQL 终止其中一个事务后，Chunk 返回 500；其余请求继续等待，上传耗时线性增加。

### 2.4 首次生产验收发现的第二条反向锁序

2026-07-26 首次发布 `4e9c28b` 后，用生产测试账号执行两条全新 Session 并发上传。上传本身成功，但 PostgreSQL 在发布后的真实链路中记录到一次新的死锁：

```text
Digest Coordinator
  -> 已锁 Digest Revision
  -> 插入 session_processing_jobs 时通过外键等待 Session

Usage Activation
  -> 已锁 Session
  -> 等待 Generation / Metrics Revision
```

这证明 P0 最初只覆盖 Usage 两个入口仍不完整。最终修复把 `Digest Coordinator.EnsureDigest` 也纳入同一规则：事务开始后先锁 Session，再读取或创建 Digest Revision、再创建或提权 Digest Job。修复提交为 `55cda31`。

## 3. 代码事实与差距

| 需求/验收项 | 当前能力 | 代码证据 | 差距 | 最小改动 |
| --- | --- | --- | --- | --- |
| CLI 成功后当天列表可见 | Generation 和 Projection 已持久化 | `content_projection_processor.go` | 增量成功不更新 `uploaded_at` | Projection 激活时更新 `uploaded_at` |
| 上传不得与 Token 后台处理死锁 | Chunk、Finalize、Projection 已先锁 Session | `postgres_repository.go`、`sync_service.go`、`content_projection_processor.go` | Usage 先锁 Generation/Revision，后写 Session 关联数据 | Usage 改为先锁 Session，再执行现有锁和写入 |
| Digest 后台入队不得与 Usage 激活死锁 | Digest Job 通过外键关联 Session 和 Generation | `sessiondigestv2/coordinator.go` | Coordinator 原先先锁 Digest Revision，插入 Job 时才获取 Session 外键锁 | `EnsureDigest` 事务先锁 Session，再处理 Revision 和 Job |
| 瞬时数据库冲突不得直接返回 500 | Prepare 已有限重试 `40P01` | `prepare_retry.go` | Chunk、Finalize 未复用 | 统一数据库冲突重试 |
| Token 数值和 Fork 归属不变 | Usage Revision 链路已运行 | `usage/processor.go` | 无 | 不修改解析、归属、聚合和去重 |
| CLI 只在服务端 ready 后成功 | CLI 已等待 Generation、Content、Projection | `daemon/session_sync_client.go` | 无 | 不修改成功判定 |

## 4. 固定产品口径

1. `sessions.started_at` 表示 Session 真实开始时间；
2. `sessions.uploaded_at` 表示最近一次内容成功完成 Projection、可供业务读取的时间；
3. 历史 Session 当天增量上传成功后必须出现在当天 Session 列表；
4. Projection 未激活或失败时不得提前更新 `uploaded_at`；
5. CLI 成功表示所选顶层 Session 的全部来源均达到现有 `ready_for_reports`；
6. 顶层 Session 与 Subagent 的父子关系保持不变；
7. Token 解析、Fork 归属、去重、汇总和成本口径保持不变。

## 5. 修复设计

### 5.1 用户可见一致性

修改 `api/internal/sessionsync/content_projection_processor.go`。`uploaded_at` 只在以下两个确定时点更新：

1. 新 Content Projection Revision 首次成功激活；
2. 已 active Revision 完成增量 Chunk，且 `content_indexed_cursor` 追平当前 Generation `expected_cursor`。

首次激活时在同一事务执行：

```text
激活 Projection Revision
  -> 更新 active_content_projection_revision_id
  -> 更新 sessions.content_status = available
  -> 更新 sessions.uploaded_at = now()
  -> 更新 sessions.updated_at = now()
  -> 激活 Report Source Catalog Revision
  -> 提交事务
```

已 active Revision 增量追加时，由最后一个推进 cursor 的 Content Projection Chunk 事务更新 `uploaded_at`。该事务必须同时满足 Revision 和 Generation 均为 `active`，并且本次 `chunk.end_cursor = generation.expected_cursor`。

重复投递已完成的 Activation Job 或已 indexed Chunk 沿用现有幂等返回，不重复刷新时间。

`uploaded_at` 不在 Prepare、Chunk 或 Finalize 阶段更新。只有新的 Projection 首次成功激活、内容已经可读后才更新时间。

现有 `/api/v1/sessions?date=YYYY-MM-DD` 和前端保持不变，不新增接口或字段。

### 5.2 统一 Session 行锁顺序

在 `api/internal/sessionsync/session_lock.go` 新增项目内部公共函数：

```go
func LockSessionForUpdate(ctx context.Context, tx *sql.Tx, sessionID string) error
```

该函数位于 Go `internal` 包内，仅导出给项目内部的 Usage 包调用，不形成外部 API。

实现固定使用现有 `sessions` 行锁：

```sql
SELECT id FROM sessions WHERE id = $1 FOR UPDATE
```

统一事务顺序固定为：

```text
Begin transaction
  -> 锁 sessions
  -> 执行现有 Source、Generation、Revision 行锁
  -> 执行业务写入
  -> Commit 或 Rollback 自动释放锁
```

代码核对结果：Chunk、Finalize 和 Content Projection 已按 Session 在前的顺序加锁，不重复改造。以下四个入口必须在现有 advisory lock 和任何 Generation、Metrics Revision 或 Digest Revision 行锁之前调用 `LockSessionForUpdate`：

1. Usage Processor 的 Chunk 解析事务；
2. Usage Processor 的 Metrics Revision 激活事务；
3. Digest Coordinator 的 `EnsureDigest` 事务；
4. Digest V2 Reconciler 创建 Revision 和后台 Job 的事务；批量 Session 按 ID 固定顺序加锁。

同一 Session 的上传、Projection 和 Usage 数据库写入按统一顺序串行；对象下载和内容解析继续在事务外执行。不同 Session 和用户不共享行锁，保持并发。

不新增 advisory lock、数据库表、字段、队列、Worker 类型或分布式锁。

### 5.3 数据库冲突有限重试

将 `prepare_retry.go` 收敛为 Session Sync 内部数据库冲突重试方法，固定识别：

```text
40P01：deadlock detected
40001：serialization failure
```

固定执行规则：

```text
首次执行
  -> 失败后等待 50 ms
  -> 第二次失败后等待 100 ms
  -> 第三次失败后返回错误
```

覆盖 Prepare、CommitChunk 和 Finalize。每次重试必须重新开始并完整执行整个数据库事务，禁止在原事务内重试单条 SQL。

参数、权限、cursor、prefix hash、content epoch、内容损坏、MinIO 和其他数据库错误禁止自动重试。

Chunk 的 Generation、cursor 和内容 hash 已构成现有幂等身份。失败事务不会提交 Chunk、Processing Job 或 Token 数据；有限重试不得改变现有幂等规则。

### 5.4 CLI 边界

本期不修改 CLI 成功判定，不要求 CLI 再调用 Session 列表确认。现有 CLI 已等待：

```text
Generation active
+ Content available
+ Projection active
+ indexed cursor >= expected cursor
```

细粒度上传进度展示列为后续增强，不进入 P0 开发任务。

## 6. 业务影响范围

### 6.1 直接影响

- Codex 增量上传；
- Claude Code 增量上传；
- 使用 `/session-chunks/batch` 的 Canonical 新客户端；
- Session 管理页和产品页的上报时间、日期筛选与排序；
- Content Projection Worker；
- Token Usage Worker 的并发执行。
- Digest Coordinator 的数据库锁顺序和后台 Job 入队。

### 6.2 明确不影响

- Codex/Claude Token 数值；
- Subagent/Fork Token 归属与去重；
- Token Analytics 汇总和成本；
- Session 原始 JSONL 和 MinIO 对象；
- Digest 内容、版本、Worker 容量、Report Projection 和报告生成结果；
- Report Skill、MCP 和 Agent；
- 前端代码；
- 数据库 migration 和历史数据结构；
- 已生成报告和已冻结 Report Context。

## 7. 风险与控制

### 7.1 同一 Session 数据库写入串行

同一 Session 的 Chunk、Content Projection 和 Usage 本来就共同修改 Session、Source 或 Generation 关联数据。只串行数据库事务；对象下载和解析不持锁。验收必须证明两个不同 Session 可以同时处理。

### 7.2 列表排序变化

增量上传成功后，Session 按最近成功上报时间移动到列表前部，并归入本次上报日期。这是确定的产品行为。

`started_at`、Activity Slice 日期和 Token activity date 均不修改，因此报告周期和 Token 日期不受影响。

### 7.3 重试重复写入

仅重试 PostgreSQL 已回滚的 `40P01/40001`。现有 Chunk 唯一身份、cursor CAS 和事务原子性继续负责幂等。自动化必须证明重试后只有一个 Chunk、一组 Processing Job 和一份 Token Contribution。

### 7.4 Session 关联写入入口遗漏

Usage 的 Chunk 解析、Metrics Revision 激活、Digest Coordinator 和 Digest V2 Reconciler 的 Job 入队必须全部改为 Session 在前。只修改其中一部分或只增加重试不能关闭问题。

### 7.5 回滚

问题由提交 `a0b1d51` 引入的既有 Session Sync 与 Token Analytics V2 并发关系触发，回退 2026-07-25 报告版本不能根治。

本次修复无 migration。发布出现回归时只回退 API 镜像；回退后恢复为已知 P0 状态，不能宣称上传链路已恢复。

## 8. 开发任务

| 顺序 | 对应需求 | 代码位置 | 确定改动 | 完成标准 | 依赖 |
| --- | --- | --- | --- | --- | --- |
| 1 | 稳定复现死锁 | Session Sync、Usage 集成测试 | 真实 PostgreSQL 和受控并发屏障复现 Chunk/Usage 锁冲突 | 修复前稳定出现 `40P01` | 独立测试库 |
| 2 | 消除死锁 | `sessionsync/session_lock.go`、`usage/processor.go` | 新增 `LockSessionForUpdate`，Usage 两类事务先锁 Session | 上传与 Usage 并发测试无死锁 | 任务 1 |
| 3 | 补齐 Digest 锁顺序 | `sessiondigestv2/coordinator.go` | `EnsureDigest` 先锁 Session，再处理 Digest Revision 和 Job | Digest 入队与 Usage 激活并发无死锁 | 任务 1 |
| 4 | 屏蔽瞬时冲突 | `prepare_retry.go`、Chunk、Finalize | 复用固定三次冲突重试 | 只重试 `40P01/40001`，无重复数据 | 任务 2 |
| 5 | 当天可见 | `content_projection_processor.go` | Projection 激活时更新 `uploaded_at` | 增量 Session 可由当天列表查询 | 无 |
| 6 | 全量回归 | API、Token Golden、Session Sync 集成测试 | 完整测试和真实上传 | Token、Fork、内容、报告来源无回归 | 任务 2 至 5 |

## 9. 自动化测试

必须覆盖：

1. 历史 Session 增量上传后，Projection 激活更新 `uploaded_at`；
2. 已 active Revision 增量追加时，只有最终 Chunk 追平 Generation cursor 后更新 `uploaded_at`；
3. 重复 Activation Job 和重复 Chunk 不刷新 `uploaded_at`；
4. Projection pending、failed 或事务回滚时不更新 `uploaded_at`；
5. `/sessions?date=当天` 返回当天成功增量上传的历史 Session；
6. Chunk 与 Usage Chunk Processor 并发不出现 `40P01`；
7. Finalize 与 Usage Revision Activation 并发不出现死锁；
8. 同一 Session 的 Usage 与上传事务按 Session 在前的顺序执行；
9. Digest Coordinator 在 Digest Revision 和 Job 之前获取 Session 锁；
10. Digest V2 Reconciler 批量按固定 Session 顺序加锁后才创建 Revision 和 Job；
11. 不同 Session 可以并发；
12. `40P01/40001` 前两次失败后成功，只产生一份业务数据；
13. 第三次仍失败时返回错误，不无限重试；
14. 其他错误不重试；
15. Codex、Claude Code Token Golden 完全不变；
16. Subagent/Fork 父子归属、去重和总 Token 不变。

## 10. 测试服与生产验收

使用两个顶层 Session：一个历史 Session 有新增内容，一个当天新 Session；历史 Session 必须包含 Subagent。

关闭条件固定为：

```text
aida upload 成功 2 个顶层 Session
两个顶层 Session 均可由当天 Session 列表查询
全部子 Agent 来源均为 active + available
原始内容可下载且 hash 一致
Token 统计与上传前后增量一致
重复上传不产生重复 Session、Chunk 或 Token
API 日志没有 /session-chunks/batch 500
PostgreSQL 日志没有新增 deadlock detected
不同用户和不同 Session 上传仍可并发
```

测试服通过后只发布 API：不发布 Web、CLI、Skill/MCP，不执行 migration，不修改 Worker 并发配置。

## 11. 明确不做

- 不新增 Session 上传请求表或状态机；
- 不引入 Redis、River、Temporal 或分布式锁；
- 不修改 Token 解析和统计口径；
- 不重新生成历史 Token Revision；
- 不修改 Digest 内容、版本、Worker 容量或报告生成业务流程；Digest Coordinator 只调整数据库锁顺序；
- 不通过降低 Worker 并发掩盖死锁；
- 不依赖 CLI 重试掩盖服务端 500；
- 不把数据库存在当作用户可见验收通过；
- 不在本期实现细粒度上传进度条。

## 12. 开发顺序

```text
真实 PostgreSQL 并发测试稳定复现
  -> 统一 Usage、Digest Coordinator、Digest V2 Reconciler 与上传的 Session 行锁顺序
  -> 接入有限数据库冲突重试
  -> 修正 Projection 激活时 uploaded_at
  -> API 与 Token Golden 全量回归
  -> 测试服两个真实顶层 Session 验收
  -> 只发布生产 API
  -> 生产复测并观察 PostgreSQL deadlock
```

自动化、测试服真实上传和生产用户可见链路全部通过前，本问题保持 P0 未关闭。

## 13. 实现与验证记录

2026-07-25 已完成代码修改：

- Usage Chunk 和 Metrics Revision Activation 统一先锁 Session；
- Prepare、CommitChunk、Finalize 统一对 `40P01/40001` 执行最多三次完整事务；
- Projection 首次激活及 active Revision 增量追平 cursor 时更新 `uploaded_at`；
- 重复 Activation 和重复 Chunk 不刷新 `uploaded_at`。

已通过：

```text
go test ./internal/sessionsync -count=1
go test ./internal/usage -run TestProcessorLocksSessionBeforeUsageRevisionIntegration -count=1
go test ./...
cd daemon && go test ./...
```

Usage 全量数据库集成测试仍有一条既有断言失败：`TestProcessorStableProviderClaimPreventsCrossSourceDoubleCountIntegration` 的 Token 总数为预期的 250，但第二个重复来源 Revision 当前为 `active/estimated`，测试仍期望 `failed/conflict`。该测试与本次 Session 锁、重试和 `uploaded_at` 修改无代码交集，本期不修改 Token 状态口径。

2026-07-26 测试服使用账号 `304/t02` 和两条真实 Codex Session 完成首次、增量和无变化重复上传：

- 三轮均成功处理两个顶层 Session；
- 增量后两条 Session 均进入 2026-07-26 当天列表；
- `expected_cursor = content_indexed_cursor`；
- Token 保持 `37291` 和 `414129`；
- 重复上传后 `uploaded_at`、Chunk 数和 Token 均未变化；
- API `/session-chunks/batch` 500 为 0，PostgreSQL `deadlock detected` 为 0。

2026-07-26 生产发布经历两次 API 镜像切换：

1. `4e9c28b` 首次真实上传暴露 Digest Coordinator 与 Usage Activation 的第二条反向锁序，记录 1 次 `deadlock detected`，未将该轮判为通过；
2. `55cda31` 补齐 Digest Coordinator 的 Session-first 锁顺序，并增加真实 PostgreSQL 测试 `TestEnsureDigestLocksSessionBeforeDigestRevision`。

首次关闭时的生产镜像为 `20260726-55cda31-upload-consistency`。2026-07-26 旧 Payload 归档表清理后的真实上传又暴露 Digest V2 Reconciler 直接创建后台 Job 时未先锁 Session，产生相同的 Session/Generation 反向锁序。提交 `c35c93f` 已补齐该入口，并将批量 Session 按 ID 固定顺序加锁；对应真实 PostgreSQL 并发测试已加入同一锁顺序测试。

最终生产镜像为 `20260726-c35c93f-archive-finalize`，digest 为 `sha256:ae56d8d1390a0b0a9d1222d554cb0838756f877fe0646ffb87412926c310e9be`。生产测试账号 `307/t05` 使用隔离的真实 Codex Session 副本完成：

- 两条全新 Session 并发首次上传成功；
- 一条 Session 增量上传成功，`uploaded_at` 和 `last_activity_at` 按新内容更新；
- 无变化重复上传成功，`uploaded_at`、Generation 数和事件数不漂移；
- 两条 Session 均能从 2026-07-26 当天列表查询，状态为 `available`；
- 两条 Session 均满足 `expected_cursor = content_indexed_cursor`，事件计数与 Projection 计数一致；
- 每条 Session 只有一个 active Generation；
- 最终镜像启动后的 Session Sync 500 为 0，PostgreSQL `deadlock detected` 为 0。

代码、自动化、测试服和生产真实链路均满足关闭条件，本问题于 2026-07-26 关闭。
