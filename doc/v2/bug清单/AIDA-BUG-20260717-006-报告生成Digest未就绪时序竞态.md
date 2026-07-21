# AIDA-BUG-20260717-006：报告生成 Digest 未就绪时序竞态

> 优先级：P0（2026-07-21 生产事故升级；原 P1）
> 状态：生产已止血，根治未完成
> 发现时间：2026-07-17
> 首次发现环境：测试服 `192.168.14.157`
> 生产事故时间：2026-07-21 18:05～18:49（北京时间）
> 影响范围：个人日报、个人周报的 Session 来源选择与 AI 生成启动
> 用户错误：`REPORT_SOURCE_DIGEST_NOT_READY` / `report source digest is not ready; retry later`

## 1. 问题结论

这是“内容切片已经可选，但该切片的 Digest 尚未完成”产生的时序竞态，不是 Digest 内容损坏，也不是单个 Session 过大。

当前候选列表只要求内容 Projection 覆盖完整切片，因此 Session 可以先出现在选择列表；Digest 由 Reconciler 和 Worker 异步生成。用户在两者之间立即点击 AI 生成时，启动接口冻结来源失败并返回 HTTP 409，前端没有等待或自动重试，直接展示原始英文错误。

周报通常选择多个 Slice，只要任意一个 Slice 的 Digest 仍为 missing、`pending` 或 `building`，整份来源选择都不能冻结，因此比单 Slice 日报更容易遇到。

2026-07-21 的生产事故证明，该问题不只存在数秒竞态窗口：多用户集中执行 `upload all` 时，新切片产生速度超过 Digest Reconciler 的发现与入队能力，缺口会持续增长；旧切片又因“最新优先”排序被不断插队。前端没有承接后台准备状态，最终把可恢复的等待过程表现为核心报告功能失败，因此事故等级由 P1 升为 P0。

事故期间后端完整性门禁正常生效，没有创建缺少来源快照的 `ai_run`，未发现 Digest 损坏或业务数据丢失。但用户无法生成当日日报，属于核心功能可用性事故。

## 2. 当前真实时序

```text
Upload Finalize
  -> Content Projection fully indexed
  -> Slice 进入 Report Source 候选列表
  -> Digest Reconciler 发现 Slice
  -> 创建 Digest Revision + Job
  -> Digest Worker 构建
  -> Digest Revision ready
```

竞态窗口：

```text
Slice 已进入候选
  -> 用户创建 Selection
  -> 用户立即启动 Agent Run
  -> 后端尝试冻结 Selection Digest
  -> 任一 Digest missing/pending/building
  -> REPORT_SOURCE_DIGEST_NOT_READY
```

此时事务不会创建无来源快照的 `ai_run`，后端完整性门禁是正确的；问题在于前端把一个可等待的准备阶段直接表现成“AI 生成失败”。

## 3. 代码与运行证据

### 3.1 代码行为

- `ListCandidates` 只判断 Session 内容可用、active Projection 和 `content_indexed_cursor >= slice.end_cursor`，并把 `content_index_status` 返回为 ready；它不判断 Digest ready。
- 创建显式 Selection 时只冻结 Slice、Generation、Projection Revision 和 cursor 身份，不要求 Digest 当场完成。
- 启动报告 Agent Run 时，`freezeSelectionV2ForRun` 才读取 Digest Revision；缺失或状态为 `pending/building` 时返回 `ErrDigestNotReady`。
- Handler 将其映射为 HTTP 409 和 `REPORT_SOURCE_DIGEST_NOT_READY`。
- 前端当前只对大上下文确认和默认 Agent 不可用做特殊处理；Digest not ready 直接进入通用 `onError`，显示“AI 生成失败”。

### 3.2 事故前调度配置

事故前 Digest V2：

- Reconciler 每 5 秒运行一次；
- `ReconcileBatch = 1`，每轮只发现一个符合条件的 Slice；
- Worker 每 2 秒轮询一次；
- `WorkerBatch = 1`，每批只处理一个 Digest Job。

因此多个新 Slice 同时就绪时，即使没有任务失败，后面的 Slice 也可能等待数十秒才进入 Digest 构建。

2026-07-21 生产止血后配置为：

- `ReconcileBatch = 10`，仍每 5 秒运行一次；
- Digest V2 Worker 实际实例数从 1 增至 2；
- 每个 Worker 的 `WorkerBatch` 仍为 1；
- 两个 Worker 使用不同 lease owner，真正并行处理两个 Job，而不是把单 Worker 的领取批量改大。

以上只解决本次吞吐止血，不代表交互、调度公平性和监控已经根治。

### 3.3 2026-07-17 测试服只读检查

检查时当前 `session-digest/v2.8.0` 已追平：

| 指标 | 结果 |
| --- | ---: |
| fully indexed 但没有 v2.8 Revision 的 Slice | 0 |
| 当前 pending/building/failed v2 Digest | 0 |
| v2 Digest Job | 全部 completed |
| Revision 创建到 ready p50 | 2.00 秒 |
| Revision 创建到 ready p95 | 4.75 秒 |
| Revision 创建到 ready p99 | 17.54 秒 |
| Revision 创建到 ready 最大值 | 37.46 秒 |

以上延迟是当时过去 24 小时 v2 各版本 Revision 的聚合值，不包含 Slice 等待 Reconciler 发现的时间。当时测试服无积压，只能说明该次测试服错误更符合短暂竞态；2026-07-21 生产事故已经证明持续突发流量会形成真实积压。

## 4. 目标行为

```text
用户点击 AI 生成
  -> Selection 已准备，但 Digest 未全部 ready
  -> UI 显示“正在准备报告数据”
  -> 使用同一个 prepared Selection 有界等待/重试
  -> 全部 Digest ready
  -> 自动冻结来源并启动 Agent Run
```

用户不应看到内部英文错误，也不应手工反复点击“重新生成”。

## 5. 后续优化建议

### 5.1 最小交互修复

1. 前端识别 `REPORT_SOURCE_DIGEST_NOT_READY`；
2. 保留本次 `report_source_selection_id`，不能每次轮询重新创建 Selection；
3. 进入“正在准备报告数据”阶段，按有界间隔重试同一个启动请求；
4. Digest ready 后自动进入现有 Agent Run 流程；
5. 超过等待上限时显示可理解的中文提示，并允许用户稍后使用同一来源重新发起；
6. 页面关闭、网络重试和重复点击不能产生多个 Run。

### 5.2 后端时序优化

优先评估在 Projection 覆盖 Slice `end_cursor` 后直接幂等入队 Digest Job，以 Reconciler 继续承担漏单恢复，而不是只靠每 5 秒、每轮一个 Slice 的扫描发现。

如果调整 Reconcile Batch 或 Worker 并发，必须先验证数据库 CPU、I/O、连接池、Digest 构建内存和前台接口 p95，不能为了缩短等待抢占现有请求资源。

### 5.3 可选的轻量状态表达

候选列表的 `content ready` 与 `digest ready` 必须保持为两个状态。若未来需要提前展示“报告数据准备中”，只能使用轻量状态字段或状态接口，不能让候选分页 JOIN Digest JSON、事件表或现场生成 Digest。

## 6. 明确禁止

- 不在用户启动请求内同步扫描完整 JSONL 生成 Digest；
- 不因 Digest 未就绪回退到未冻结的原始内容；
- 不生成空 Digest 或跳过缺失 Slice 后继续报告；
- 不弱化 `write_report_result` 的完整读取、版本和 hash 门禁；
- 不把 `content ready` 重命名为 Digest ready 来掩盖状态差异；
- 不把本 Bug 顺带扩展成 Session/Token 性能专项的一次性重构。

## 7. 回归用例

| 编号 | 场景 | 通过标准 |
| --- | --- | --- |
| DIGEST-RACE-001 | 新 Slice 刚 fully indexed 即点击 AI 生成 | 不展示英文错误，进入准备状态并自动继续 |
| DIGEST-RACE-002 | 周报选择多个 Slice，部分 ready、部分 pending | 等待全部 ready 后只创建一个 Run |
| DIGEST-RACE-003 | Digest Worker 正常延迟 | 使用同一个 Selection 重试，不创建垃圾 Selection/Run |
| DIGEST-RACE-004 | Digest 最终 failed | 停止等待，显示明确中文失败，不自动降级 |
| DIGEST-RACE-005 | 等待超时 | 给出可恢复提示，重复操作幂等 |
| DIGEST-RACE-006 | 用户关闭并重新打开弹窗 | 状态可恢复，不重复启动 Run |
| DIGEST-RACE-007 | 多用户同时上传和生成 | Digest 后台任务不造成前台 p95 明显回归 |
| DIGEST-RACE-008 | Digest ready 后生成报告 | attached Digest、MCP 完整读取与写回门禁保持 |

## 8. 与 Session/Token 性能专项的关系

- 本 Bug 不阻塞 [Session 与 Token 查询性能专项](../Session与Token查询性能/README.md) 的 R1 切片目录开发；
- R1 Catalog 的 ready 仍表示“内容切片可用于候选”，不能偷偷改成 Digest ready；
- R1 必须保留 Slice/Projection Revision/cursor 身份，供后续 Digest 状态关联；
- 后续修复不得把 Digest JSON 或 Digest 构建重新放进候选查询；
- 后端 `REPORT_SOURCE_DIGEST_NOT_READY` 门禁继续保留，优化的是等待衔接和任务触发时延。

## 9. 关闭条件

只有满足以下条件才能关闭：

1. fresh Slice 和多 Slice 周报不再向用户暴露 `REPORT_SOURCE_DIGEST_NOT_READY`；
2. 同一操作只产生一个 Selection 和一个 Agent Run；
3. Digest failed、超时、页面关闭和网络重试均有明确且可恢复的状态；
4. attached Digest、MCP 与 `write_report_result` 完整性门禁没有弱化；
5. 测试服和生产均完成真实上传后立即生成的回归，并观察 Digest 队列与前台 p95。

## 10. 2026-07-21 生产 P0 事故记录

### 10.1 事故定级与用户影响

- **事故等级：P0。** 正常的多用户批量上传触发核心日报生成不可用，用户无法通过页面自行恢复。
- **直接表现：** 创建个人日报 Run 返回 HTTP 409，错误码为 `REPORT_SOURCE_DIGEST_NOT_READY`，前端直接展示失败。
- **实际影响：** 故障账号当天选择的 7 个来源切片中只有 1 个 Digest ready，另外 6 个尚未创建或仍在等待构建，因此整份报告无法启动。
- **数据完整性：** 冻结门禁阻止了无完整来源快照的 Run 创建；未发现 Digest 损坏、报告错写或业务数据丢失。
- **安全要求：** 事故排查请求使用的用户凭证不得写入文档、日志或提交记录；暴露的凭证应立即失效并重新登录。

### 10.2 已验证时间线

以下均为北京时间，数据库原始 UTC 时间已换算：

| 时间 | 事件与证据 |
| --- | --- |
| 18:05 起 | 生产开始出现集中 Session finalize 和内容切片生成。约 25 分钟内，4 个用户共完成 571 个 generation；最大单一用户 487 个，故障账号 55 个。 |
| 18:24:36 | 对生产请求进行一次受控复现，38.872 ms 返回 HTTP 409：`REPORT_SOURCE_DIGEST_NOT_READY`。该响应发生在模型调用前。 |
| 18:25:41 | 生产 Selection `59e5cdfe-4ed5-4d2a-805d-a6343fad0fe1` 已 prepared，共 7 个来源；当时仅 1 个具有 `session-digest/v2.9.0` ready Revision。 |
| 18:35 左右 | 修正后的只读盘点观察到缺失量峰值为 365；故障账号有 44 个当前切片缺少 v2.9 Digest。Worker 侧没有 failed/dead 积压。 |
| 18:40:26 | 第一阶段止血镜像上线：`ReconcileBatch` 从 1 提升到 10，Worker 仍为 1。启动日志确认参数生效。 |
| 18:40:47～18:41:55 | missing 从 284 降至 154，证明发现吞吐改善；同时 pending 从 40 增至 136，瓶颈转移到单 Worker。 |
| 18:46:13 | 第二阶段止血镜像上线：保持 `ReconcileBatch=10`，启动两个独立 Digest V2 Worker。 |
| 18:48:55 | 本次日报 7/7 Digest ready、0 failed；随后 `build_content_slice_digest_v2` 非 completed 队列清零。 |

### 10.3 根因链路

本次事故由四个条件叠加产生，不能只归因于“并发高”或“Session 大”：

1. **突发输入超过发现吞吐。** 事故前 Reconciler 每 5 秒只发现 1 条，理论上约 12 条/分钟；生产实际平均约 23 个 generation/分钟，分钟峰值超过 40。
2. **发现与构建是串联异步链路。** Slice 已可进入报告候选，并不表示 Digest 已 ready；报告启动时才执行严格冻结检查。
3. **调度缺少公平性。** Reconciler 使用 `ORDER BY sl.created_at DESC`，持续优先最新 Slice；旧 Slice 在持续上传期间可能反复被插队。
4. **前端没有等待语义。** 前端把 `REPORT_SOURCE_DIGEST_NOT_READY` 当作最终失败，没有保留同一 Selection、有界重试并自动继续。

缺少 missing、pending、最老等待时间和失败告警，使得上述容量缺口未被平台主动发现，是事故扩大和发现滞后的重要原因。

### 10.4 排除项

- 不是单个 Session 过大：本次失败发生在 Digest Revision 未创建或未 ready 阶段。
- 不是模型 `MiniMax-M2.5` 异常：请求在调用模型前即返回 409。
- 不是鉴权、Nginx 或网络问题：请求已通过鉴权并由业务 Handler 返回明确错误码。
- 不是 Worker 构建失败：事故盘点时 v2 Worker Job 以 completed 为主，没有 failed/dead 积压。
- 不是主机资源不足：15 GiB 主机内存约 14 GiB available；止血期间 API 内存约 10～15 MiB，无 OOM、无容器重启。

### 10.5 统计口径纠正

排查过程中曾报告“生产积压 6313 个”，该数字错误，严禁用于事故复盘、容量估算或验收。

错误原因是只读 SQL 将真实脱敏版本 `report-redaction/v1` 写成了不存在的 `session-digest-redaction/v1`，导致已存在的 v2.9 Revision 也被误判为缺失。使用生产真实版本重新计算后：

- 当时真实缺失为 306，后续观察到的最高快照为 365；
- 缺失切片全部产生于 2026-07-21 18:05 之后，不存在 6313 条历史缺口；
- 故障账号真实缺失为 44，而不是此前错误查询中的 55；55 是该账号本轮 finalize 的 generation 数。

后续所有生产 Digest 盘点必须先从表中读取实际 `digest_version` 与 `redaction_version`，不得在诊断 SQL 中猜测版本字符串。

### 10.6 已执行止血

| 项目 | 结果 |
| --- | --- |
| 第一阶段代码 | `228577b`：`ReconcileBatch` 默认值与零值归一化从 1 调整为 10 |
| 第二阶段代码 | `c90a8b8`：启动两个独立 Digest V2 Worker，保持每个 Worker 的 `WorkerBatch=1` |
| 生产镜像 | `20260721-2694a86-digest-workers2-hotfix` |
| 生产镜像 digest | `sha256:b25733fe1d877091ab4070fcb799bb899966bd6bbeecb42b26e0e3a4237a46c7` |
| 回退镜像 | `20260721-792bf86-digest-batch10-hotfix`；如需回到事故前则使用 `20260720-542c123-report-context-v1` |
| 配置备份 | `/home/luoxian/aida/backups/digest-batch10-20260721T103937Z` |
| 发布范围 | 只替换 API；无数据库迁移、无 Web/CLI/MinIO 变更 |

两个生产热修复最初基于生产提交 `542c123` 在隔离 detached worktree 构建，随后已 cherry-pick 回 `main`。生产镜像不包含 `main` 上其他未发布功能。

### 10.7 止血验收

| 检查项 | 结果 |
| --- | --- |
| API 全量普通测试 | `cd api && go test ./...` 通过 |
| API 健康 | `/health` 返回 `{"status":"ok"}` |
| 启动参数 | `reconcile_batch=10 worker_count=2 worker_batch=1 read_mode=digest_v2` |
| 本次日报来源 | 7/7 ready，0 pending/building，0 failed |
| Digest V2 队列 | 最终仅 completed，非 completed 队列清零 |
| API 资源 | 约 15 MiB；无 OOM；容器重启次数 0 |
| 数据库资源 | 追赶时观察到 CPU 约 21%，清空后回落；内存约 3.3 GiB |

止血验收只证明本轮生产积压已清除、吞吐提高，不等于本 Bug 已关闭。

### 10.8 尚未完成的根治项

| 项目 | 状态 | 完成标准 |
| --- | --- | --- |
| Reconciler 公平性 | 未完成 | 持续新上传时旧 Slice 不会无限等待；排序或调度策略有自动化与生产容量验证 |
| Projection ready 后主动入队 | 未完成 | 正常链路直接幂等创建 Digest Job，周期 Reconciler 仅承担漏单恢复 |
| 前端等待交互 | 未完成 | 用户看到“正在准备报告数据”，同一 Selection 自动重试并只创建一个 Run |
| 有界失败与恢复 | 未完成 | Digest failed、等待超时、页面关闭和网络重试均有明确且可恢复的状态 |
| 生产监控告警 | 未完成 | 至少覆盖 missing 数、pending 数、最老等待时间、failed/dead、处理吞吐和前台 409 |
| 并发与容量回归 | 未完成 | 使用生产等价 `upload all` 突发量验证 DB CPU、API 内存、接口 p95 和清空时间 |

在以上根治项完成并通过第 9 节关闭条件前，本事故状态保持“P0 已止血，未关闭”。
