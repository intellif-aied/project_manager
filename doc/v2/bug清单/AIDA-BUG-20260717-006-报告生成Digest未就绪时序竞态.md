# AIDA-BUG-20260717-006：报告生成 Digest 未就绪时序竞态

> 优先级：P1
> 状态：已确认，待后续优化
> 发现时间：2026-07-17
> 发现环境：测试服 `192.168.14.157`
> 影响范围：个人日报、个人周报的 Session 来源选择与 AI 生成启动
> 用户错误：`REPORT_SOURCE_DIGEST_NOT_READY` / `report source digest is not ready; retry later`

## 1. 问题结论

这是“内容切片已经可选，但该切片的 Digest 尚未完成”产生的时序竞态，不是 Digest 内容损坏。

当前候选列表只要求内容 Projection 覆盖完整切片，因此 Session 可以先出现在选择列表；Digest 由 Reconciler 和 Worker 异步生成。用户在两者之间立即点击 AI 生成时，启动接口冻结来源失败并返回 HTTP 409，前端没有等待或自动重试，直接展示原始英文错误。

周报通常选择多个 Slice，只要任意一个 Slice 的 Digest 仍为 missing、`pending` 或 `building`，整份来源选择都不能冻结，因此比单 Slice 日报更容易遇到。

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

### 3.2 调度窗口

当前 Digest V2：

- Reconciler 每 5 秒运行一次；
- `ReconcileBatch = 1`，每轮只发现一个符合条件的 Slice；
- Worker 每 2 秒轮询一次；
- `WorkerBatch = 1`，每批只处理一个 Digest Job。

因此多个新 Slice 同时就绪时，即使没有任务失败，后面的 Slice 也可能等待数十秒才进入 Digest 构建。

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

以上延迟是过去 24 小时 v2 各版本 Revision 的聚合值，不包含 Slice 等待 Reconciler 发现的时间。当前无积压说明本次错误更符合短暂竞态；不能据此认为交互问题不存在。

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
