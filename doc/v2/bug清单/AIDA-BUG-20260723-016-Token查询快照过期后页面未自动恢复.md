# AIDA-BUG-20260723-016：Token 查询快照过期后页面未自动恢复

> 优先级：P1
> 状态：代码已完成，待测试服部署与浏览器验收
> 发现日期：2026-07-23
> 范围：Token Analytics 前端查询恢复

## 1. 问题

Token Analytics 页面先请求 Summary 创建查询快照，再将 `query_snapshot_token` 传给 Trends、Rankings 和 Sessions。页面长时间打开、浏览器挂起后恢复或旧请求重试时，子查询可能继续使用超过 15 分钟有效期的旧 token，后端返回：

```json
{
  "code": "QUERY_SNAPSHOT_EXPIRED",
  "error": "query snapshot expired; restart from summary"
}
```

旧前端没有根据该稳定错误码重新请求 Summary，页面因此停留在失败状态，需要用户刷新页面才能恢复。

## 2. 已确认事实

- 快照有效期固定为 15 分钟：`api/internal/tokenanalytics/service.go`；
- 快照过期或已被清理时，`loadSnapshot` 返回 `ErrSnapshotExpired`；
- 后台 Reconciler 会清理过期快照：`api/internal/tokenrollup/reconciler.go`；
- `TokenAnalyticsPage.tsx` 先请求 Summary，再由多个子查询复用 Summary 返回的 token；
- 错误表示查询快照已失效，通常不代表 Token 数据损坏、Rollup 丢失或统计口径错误。

## 3. 根因

前端只实现了正常的 Summary → 子查询链路，没有实现后端已经声明的恢复协议：

```text
QUERY_SNAPSHOT_EXPIRED
  -> 重新请求对应 Summary
  -> 获得新 query_snapshot_token
  -> 子查询随新 token 自动重新执行
```

同时，页面存在三类相互独立的快照：

- 当前概览筛选条件的 Summary token；
- 管理视图小组选项使用的 scope Summary token；
- Session 搜索条件存在时的 session Summary token。

恢复时必须刷新产生该 token 的 Summary，不能统一刷新错误的查询。

## 4. 固定修复口径

1. 后端 15 分钟 TTL 保持不变；
2. Trends、Rankings 或 Sessions 返回 `QUERY_SNAPSHOT_EXPIRED` 时，前端不再对同一个旧 token重复重试；
3. 前端根据失败子查询使用的 token，重新请求对应 Summary；
4. Summary 返回新 token 后，React Query 通过 query key 变化自动重新执行相关子查询；
5. 同一个旧 token 的多个并发失败最多触发一次 Summary 刷新；
6. 概览、scope 和带搜索条件的 Session 快照分别恢复；
7. 非快照过期错误继续使用现有重试和错误展示逻辑；
8. 不延长 TTL、不修改 Token 数据、不新增后端兼容接口，也不要求用户手动刷新页面。

## 5. 当前修复结果

2026-07-23 已完成：

- 增加 `QUERY_SNAPSHOT_EXPIRED` 的稳定识别；
- 增加基于旧 token 的单飞恢复标记；
- 快照过期错误立即停止旧 token 重试；
- 概览子查询过期时刷新 `summaryQuery`；
- 小组选项子查询过期时刷新 `scopeSummaryQuery`；
- 带搜索条件的 Session 子查询过期时刷新 `sessionSummaryQuery`；
- 新 token 进入现有 query key 后自动恢复 Trends、Rankings 和 Sessions。

本次只修改前端 Token Analytics 页面、纯函数和对应工作流测试，没有修改后端快照、Token 汇总、Session 上传或统计口径。

## 6. 自动化验证

以下检查已通过：

```text
pnpm test
pnpm typecheck
pnpm lint
pnpm build
```

回归覆盖：

- 第一次收到快照过期错误时触发恢复；
- 同一个 token 并发失败只恢复一次；
- 普通网络错误不误触发快照重建；
- 快照过期错误不再重试旧 token；
- 三类 Summary 均存在明确恢复入口。

插件项目校验还报告了仓库原有的模板文件不同步、其他页面 Table 内滚动和 Modal 规则问题；这些问题与本次修改无关，未扩大范围处理。

## 7. 测试服验收

关闭前必须完成：

1. 打开“我的 Token”和管理视图，确认初始查询正常；
2. 使用已过期 token 触发 Trends、Rankings 和 Sessions 请求；
3. 确认页面自动重新请求对应 Summary，并使用新 token 恢复数据；
4. 确认同一旧 token 的多个并发失败只产生一次 Summary 恢复请求；
5. 分别验证无搜索 Session、带搜索 Session 和管理视图小组选项；
6. 确认恢复过程中不要求刷新页面，不显示长期错误状态；
7. 确认普通 4xx、5xx 和网络失败仍按原逻辑重试或展示错误。
