# AIDA-BUG-20260718-007：Token 查询快照超时与旧接口数据断层

> 优先级：P0
> 状态：R5A 已部署并完成安全来源回填，R5B 受数据质量门禁阻断
> 环境：14.157 测试服
> 发现日期：2026-07-18

## 1. 现象

同一用户的两套 Token 接口同时失效，但表现相反：

1. `GET /api/v1/token-analytics/summary?scope=mine&from=2026-07-16&to=2026-07-18`
   - 首次请求 30.021 秒后返回 500；
   - 下一次请求 17.146 秒后返回 200；
   - 数据库缓存热后直连仍需 1.476 秒。
2. `GET /api/v1/tokens/sessions?scope=mine&page=1&page_size=10`
   - 返回 `total=7`，且七条均为 2026-07-01 至 2026-07-06 的旧日期切片；
   - 七条记录实际只属于三个 Session，不是七个当前 Session。

## 2. 真实数据证据

用户 `303` 在 2026-07-16 至 2026-07-18 范围内：

| 口径 | 数量 |
| --- | ---: |
| Active Session | 64 |
| Active `session_usage_components` | 18,030 |
| 平均每 Session Component | 281.7 |
| 单 Session 最大 Component | 5,120 |
| Active `session_daily_usage` Rollup | 161 |

测试库累计 584 个 Token 查询快照，已经产生约 203 万次
`token_query_snapshot_items` 插入和 196 万次删除。高基数快照不是一次偶发慢 SQL，
而是会随每次页面访问持续制造写放大。

旧路径的数据断层同样明确：

| 数据源 | 行数 | 不重复 Session | 最新日期 |
| --- | ---: | ---: | --- |
| `session_activity_slices` | 7 | 3 | 2026-07-06 |
| Active Usage Component | 48,545 | 103 | 2026-07-18 |
| `session_daily_usage` | 860 | 103 | 2026-07-18 |

## 3. 根因

### 3.1 新接口在分页前复制全部明细

`/token-analytics/summary` 会创建 `token_query_snapshots`，并把日期、权限范围内的
每条 Active Component 复制到 `token_query_snapshot_items`。summary、trends、rankings、
sessions 虽然共享同一 Snapshot，但 Snapshot 冻结的是高基数明细，而不是
revision/family/Rollup/成本版本引用。

因此 Session 页面最终只有 64 行、分页只返回 10 行，也必须先处理并写入 18,030 条明细。
分页只限制最终返回数据，无法减少前置快照成本。

### 3.2 旧接口仍读取停止更新的兼容表

`/tokens` 和 `/tokens/sessions` 仍读取 `session_activity_slices`。该表由旧
`/sessions/batch` 的 `replaceActivitySlices` 写入，但旧批量上传入口已经禁用；当前
Aida 使用 `/session-syncs/*` 和 `/session-chunks/batch` 增量上传，新 Usage 流程不会写入
`session_activity_slices`。

结果是旧接口响应很快但数据停滞，新 Token Analytics 数据较新但在线快照成本失控。

### 3.3 工作台继续在浏览器侧全量翻页聚合

工作台 `fetchAllSessionTokens` 会遍历 `/tokens/sessions` 全部分页，再在浏览器计算总量、
日期趋势、成员排行和 Session 数量。它既读取了错误的旧数据源，又把同一 Session 的
多日切片数量误当成 Session 数量。

### 3.4 需求/任务关联复用了同一旧接口

需求/任务的“关联工作记录”弹窗也调用 `/tokens/sessions`，并把
`session_id:activity_date` 当作关联键。若后端直接切成 root Session 而前端不改，保存时会把
family 范围小计伪装成某一天的切片，甚至继续更新已经停止维护的
`session_activity_slices`。因此它是 R5B 的正式消费者，不能只回归工作台。

## 4. 修复边界

本问题按 [Session 与 Token 查询性能专项](../Session与Token查询性能/README.md) 的
R5A/R5B 修复：

1. R5A 建立不可变 Usage Contribution、Session Family 和 family total/daily/chunk 三套 Rollup；
2. 上传响应不等待 Rollup，异步构建完成后原子激活；
3. Rollup 保留明确的 Source/Generation/Metrics Revision/cursor 引用；R5B 的 Snapshot 只冻结 family/Rollup 版本和权限；
4. `/token-analytics/*` 全部读取同一轻量 Snapshot；
5. `/tokens`、`/tokens/sessions` 改为同一服务的兼容入口，不再读取 `session_activity_slices`；多页列表复用首页 Snapshot；
6. 工作台删除 `fetchAllSessionTokens`，直接使用 summary、trends、rankings 和明确的
   `session_count`；
7. 需求/任务关联对 family v2 行保存 root Session ID，不再伪造日期切片；
8. MCP ad-hoc `get_sessions` 与 API/前端在同一 R5B 版本完成回归和整体切换；
9. Snapshot 过期和 superseded Rollup 使用小批量后台回收，不在用户查询中执行批量删除。

不通过增加前端超时、缩短默认日期、同步回填 `session_activity_slices` 或给旧查询补缓存
作为关闭条件。

## 5. 验收条件

- 用户 `303` 的三天范围返回 64 个不重复 root Session；
- family total = daily sum = chunk sum = active contribution sum；
- 三套 Rollup `contribution_count` 与 active Contribution 行数一致，日期/模型筛选不回扫明细；
- `/tokens`、`/tokens/sessions`、`/token-analytics/*` 和工作台口径一致；
- 需求/任务关联新增、全选、取消和旧切片迁移均可用；
- Token 在线 SQL 不访问 `session_usage_components`、`session_activity_slices` 或
  `session_content_events`；
- 默认三天首次 Snapshot p95 不超过 1.5 秒、p99 不超过 3 秒；
- 后续分页/模块查询 p95 不超过 500 毫秒、p99 不超过 1 秒；
- `/tokens/sessions` 后续页不新增另一套全量 Snapshot 引用；
- 新 Chunk 上传后的 pending/ready 可见性、重复上传、Subagent 防重复和成本版本全部通过回归；
- R5A 全量对账通过前不得切换 R5B，R5B 通过前不得关闭本问题。

## 6. 当前进度

- 独立 worktree 已完成 Contribution、三维 Rollup、轻量 Snapshot、旧 Token API、工作台、
  需求/任务关联、MCP ad-hoc 和后台回收的首轮实现；
- 隔离 PostgreSQL 的 64 root / 18,030 逻辑事实回归只写 64 条 Snapshot Rollup 引用，
  用例耗时低于 25ms；
- 上述隔离用例只证明轻量 Snapshot 实现边界，真实数据门禁以下方 14.157 回填结果为准；
- 14.157 已执行 R5A 迁移 023 和低压回填：228 个来源中 215 个 active，生成
  78,230 条 active Contribution，三维/成本对账失败为 0；
- 未通过的 13 个来源中，8 个为 conflict/incomplete quality gate，3 个为历史测试数据
  `object_status=available` 但 MinIO key 不存在，2 个为 replacement revision 回退非稳定事实；
- 当前 API 仍是 R5A 旧读路径，Web/MCP 未切换。在上述 13 个来源的处置策略经人工确认前，
  R5B 继续 NO-GO，Bug 不关闭。
