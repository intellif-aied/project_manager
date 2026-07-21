# Digest 按需构建与报告等待

> 状态：方案已建立，尚未开发
> 对应问题：[`AIDA-BUG-20260717-006`](../bug清单/AIDA-BUG-20260717-006-报告生成Digest未就绪时序竞态.md)
> 设计基准：[`开发方案设计基准.md`](../开发方案设计基准.md)

## 1. 本期目标

根治“Session 内容已经可选，但 Digest 尚未 ready，用户生成日报直接失败”的时序竞态，同时满足：

- 上传后的正常链路异步预生成 Digest；
- 用户生成报告时，缺失 Digest 可以按需创建或提权；
- 同一个 Digest 只能有一个实际构建任务；
- 报告等待 Digest 时不占用 Agent Worker；
- 用户等待任务拥有独立预留的 Digest 构建容量；
- 不弱化 Selection、Digest hash、MCP 和报告写回完整性门禁。

## 2. 代码事实与差距

| 需求/验收项 | 当前能力 | 代码证据 | 差距 | 最小改动 |
| --- | --- | --- | --- | --- |
| 上传后预生成 | Reconciler 每 5 秒扫描 fully indexed Slice | `api/internal/sessiondigestv2/reconciler.go` | 正常链路依赖周期扫描，突发上传会形成发现积压 | Content Projection fully indexed 后主动幂等入队；Reconciler 保留为漏单修复 |
| 按需创建或提权 | Report Run 冻结时只检查 Digest，未 ready 返回 409 | `api/internal/reportsource/digest_v2.go` | 用户请求不能推动缺失任务 | 增加统一 `EnsureDigest`，对 missing 创建、对 pending/retry 提权 |
| 单飞构建 | Revision 和 Job 已有唯一索引，Worker 有 lease | migration `018`、`020`，`sessiondigestv2/worker.go` | 创建逻辑只在 Reconciler，尚无统一入口和提权语义 | 复用唯一索引与 `ON CONFLICT`，不增加进程内锁 |
| 等待不占 Agent Worker | 当前 Digest 未 ready 时不创建 `ai_run` | `reportsource.Service.CreateAttachedRun` | 用户只能收到 409，不能持久等待、自动继续 | 增加不可领取的 `preparing_sources` 状态；ready 后原子转 `pending` |
| 按需预留容量 | 当前两个 Worker 从同一普通队列领取 | `api/main.go`、`sessiondigestv2/worker.go` | background 已占满时，提权任务仍需等待 | 为 interactive 保留至少一个独立领取槽位 |
| 后台公平性 | Job Claim 按最老 ready 时间；Reconciler 按最新 Slice 扫描 | `sessionsync/job_repository.go`、`sessiondigestv2/reconciler.go` | 持续上传时旧 Slice 可能长期未被发现 | Reconciler 改为最老 eligible Slice 优先 |

## 3. 目标流程

```text
Upload Finalize
  -> Content Projection fully indexed
  -> EnsureDigest(slice, background)
  -> 异步预生成

用户点击生成报告
  -> 创建或复用唯一 Report Run（preparing_sources）
  -> EnsureDigest(selection slices, interactive)
     -> ready：直接使用
     -> missing：创建 Revision + 高优先级 Job
     -> pending/retry：提升为 interactive
     -> building：等待现有 Job，不重复创建
     -> failed/dead：进入明确失败，不生成残缺报告
  -> 全部 ready
  -> 原子冻结 Selection Digest
  -> Run preparing_sources -> pending
  -> Agent Worker 开始执行
```

## 4. 最小架构

### 4.1 Digest Coordinator

新增一个内部深模块，统一隐藏创建、去重、提权和状态汇总：

```text
EnsureDigest(sliceIdentity, urgency) -> ready | waiting | failed
EnsureSelection(selectionID, urgency) -> ready | waiting | failed
```

调用方只有三个：

1. Content Projection 完成：`background`；
2. 用户生成报告：`interactive`；
3. Reconciler 漏单恢复：`background`。

不得让三个调用方分别实现 Revision/Job 创建 SQL。

### 4.2 单飞事实

单飞依赖数据库现有唯一事实：

- Digest Revision identity：Slice、Projection Revision、content epoch、Digest version、redaction version；
- Digest Job identity：job type + target Digest Revision；
- Worker 通过 lease 保证同一 Job 只有一个执行者。

并发调用统一使用事务和 `INSERT ... ON CONFLICT`。不得新增仅单进程有效的 mutex 或内存 singleflight。

### 4.3 Report Run 等待状态

增加 `preparing_sources`：

- 保存用户已经发起报告生成的事实和唯一 Run ID；
- 不创建外部 Agent Task、Credential 或 Session；
- `started_at` 在真正提交 Agent 时设置；
- Agent Worker 和状态同步器不得领取该状态；
- Digest 全部 ready 后，通过条件更新只允许一次转为 `pending`；
- Digest failed、等待超时或取消分别进入明确终态；
- 定时报告的“已有活动 Run”判断必须包含 `preparing_sources`，避免重复创建。

### 4.4 两级队列与预留容量

继续使用现有 `session_processing_jobs`，不新建第二套队列系统：

- `interactive`：用户已有 Report Run 正在等待；
- `background`：上传后预生成和 Reconciler 补漏。

至少保留一个只领取 interactive Digest Job 的 Worker 槽位。普通 background Worker 不得占用该槽位。同一优先级内部按最老 ready 时间执行。

提权只作用于尚未领取的 pending/retry Job；已经 building 的 Job 继续执行，新的请求等待同一结果，不能复制或抢占构建。

## 5. 本期必须完成

1. 抽取统一 Digest Coordinator；
2. Projection ready 后主动幂等入队；
3. 报告生成时按需 Ensure 并提权；
4. 增加 `preparing_sources` 和原子状态迁移；
5. 增加 interactive/background 优先级与预留 Worker；
6. Reconciler 改为最老 Slice 优先；
7. 前端展示“正在准备报告数据”，恢复同一 Run，不要求用户重复点击；
8. 增加 missing、interactive waiting、最老等待时间、failed/dead 和吞吐观测。

## 6. 必要失败边界

- Digest failed/dead：Run 明确失败，不回退原始 Session 内容；
- 等待超时：Run 标记 timeout，可使用相同 Selection 重新发起；
- 页面关闭：Run 和等待状态保留，重新打开可恢复；
- 重复点击：返回同一个活动 Run，不创建重复 Selection、Run 或 Digest Job；
- Digest 版本变化：旧 Run 继续使用冻结版本；新 Run 使用当前版本；
- background 持续积压：不得占满 interactive 预留容量；
- interactive 持续高峰：需要用户级并发限制，且 background 不得永久饥饿。

## 7. 明确不做

- 不在 HTTP 请求内同步扫描 JSONL 构建 Digest；
- 不建立新的独立队列服务；
- 不生成空 Digest 或跳过缺失 Slice；
- 不修改 Claude Code/Codex 上传和 Token 统计；
- 不把候选列表的 content ready 偷换成 Digest ready；
- 不把 Digest JSON 加入候选分页查询；
- 不通过继续增加 ReconcileBatch 代替根治。

## 8. 验收条件

1. fresh Slice 立即生成报告时进入 `preparing_sources`，最终自动开始，只产生一个 Run；
2. 多 Slice 周报部分 ready 时，只为缺失项创建或提权，全部 ready 后一次性冻结；
3. 20～50 个并发请求同一 Digest 时，只有一个 Revision、一个 Job、一个实际构建；
4. background Worker 满载时，interactive 仍能使用预留容量；
5. 持续新上传时，旧 Slice 不会无限等待；
6. 页面关闭、重开、重复点击、网络重试不产生重复 Run；
7. Digest failed、dead、timeout 和取消均有明确状态与中文提示；
8. 等待阶段没有创建外部 Agent Task，不占 Agent Worker；
9. attached Digest、MCP 读取和 `write_report_result` hash/完整性门禁保持；
10. 生产等价 `upload all` 突发下，报告生成不再返回 `REPORT_SOURCE_DIGEST_NOT_READY`，API/DB p95 无明显回归。

## 9. 后续增强

只记录当前已发现但不阻塞本期的增强：

- 根据真实生产等待分布动态调整预留容量；
- 管理端展示 Digest 队列分层和等待 Run；
- 对长时间无人使用的预生成 Digest 做独立成本分析。
