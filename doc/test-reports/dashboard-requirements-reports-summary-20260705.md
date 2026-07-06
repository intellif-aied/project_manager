# Dashboard / 需求 / 日报 / 周报上线前回归执行总报告

执行时间：2026-07-05T16:41:22.768026Z
测试用例文档：`doc/Dashboard需求日报周报上线前全量测试用例.md`
测试环境：`http://192.168.14.157:5173`

## 执行汇总

| 批次 | 范围 | 总数 | 通过 | 失败 | 报告 |
| --- | --- | ---: | ---: | ---: | --- |
| API 第一批 | 4 角色鉴权、Dashboard、Token、需求、任务、关注、日报周报读取/写入、AI 生成默认入口 | 251 | 238 | 13 | `doc/test-reports/dashboard-requirements-reports-REG-20260705-AUTO-163705.md` |
| 失败项复跑 | 按当前真实路由修正后的日报/小组日报/部门日报/team weekly 验证 | 13 | 10 | 3 | `doc/test-reports/dashboard-requirements-reports-followup-REG-20260705-FOLLOWUP-164022.md` |
| UI 批次 | 4 角色 × 2 视口 × Dashboard/需求/日报/周报 | 32 | 32 | 0 | `doc/test-reports/REG-20260705-UI-20260705163837.md` |
| 前端类型检查 | `web pnpm typecheck` | 1 | 1 | 0 | 命令输出通过 |
| 后端单测 | `api go test ./...` | 1 | 1 | 0 | 命令输出通过 |

## 当前确认通过的核心链路

- 4 个测试角色 `PM / 总监 / TL-A / 员工-A1` 的 `/auth/me` 均返回 200，角色和小组信息正确。
- Dashboard 三个核心接口 `/dashboard/my-items`、`/dashboard/risks`、`/dashboard/follows` 4 角色均可访问。
- Token 页面和 Dashboard Token 数据接口 4 角色均可访问；非法 period / range 参数返回 400。
- 需求创建、需求列表、需求详情、需求编辑、任务创建、任务列表、任务详情、任务进度、任务状态、关注/取消关注核心链路通过。
- 个人日报真实链路通过：`GET /reports/today?report_date=...` 创建/读取后，`PUT /reports/<built-in function id>` 保存成功。
- TL 小组日报 `PUT /reports/team/today` 保存成功，员工保存小组日报返回 403。
- 总监部门日报 `PUT /reports/department/today` 保存成功，TL 保存部门日报返回 403。
- 默认报告 Agent 运行入口没有 500，返回在预期范围内。
- UI 页面可达性通过：Dashboard、需求、日报、周报在 1366x768 和 1440x900 下，4 个角色均未出现登录跳转、首屏空白或 API 5xx。
- `web pnpm typecheck` 通过。
- `api go test ./...` 通过。

## 需要处理 / 确认的问题

### BUG-1：`PUT /api/v1/reports/today` 返回 500

复现：

```bash
curl -X PUT 'http://192.168.14.157:5173/api/v1/reports/today' \
  -H 'Authorization: Bearer <PM token>' \
  -H 'Content-Type: application/json' \
  --data '{"report_date":"2026-07-05","content":"should not 500","session_ids":[],"status":"saved"}'
```

实际：`500 {"error":"pq: invalid input syntax for type uuid: \"today\" (22P02)"}`

判断：前端当前真实保存链路不是这个接口，而是先 `GET /reports/today` 得到日报 id，再 `PUT /reports/<built-in function id>`。但后端动态路由 `/reports/<built-in function id>` 吞掉了 `today`，非法 id 不应该打到数据库后返回 500，建议改成 400/404/405。

### BUG-2 / 待确认：PM 访问 team weekly 带 `team_id` 仍返回 400

复现：

```bash
GET /api/v1/reports/team/weekly/current?week_start=2026-06-29&team_id=3f05e6ed-c3bc-4900-8d7b-ea89843e157a
```

PM 实际：`400 {"error":"no team specified"}`

总监同接口：200。

判断：如果 PM 设计上只允许个人日报/周报，这是测试期望需要调整，不算 bug；如果 PM 应该能查看小组周报，则后端对 PM 的 team_id 解析逻辑有问题。

### TEST-1：`web pnpm test` 里的 Dashboard 静态契约测试已过期

执行：`cd web && pnpm test`

结果：失败在 `scripts/dashboard_report_workflow_test.mjs`：

```text
AssertionError: Dashboard should query today's reports without creating a report
```

判断：这个脚本是源码正则契约测试，断言仍停留在旧 Dashboard 实现。当前 `pnpm typecheck` 和真实 UI/API 批次通过，但 `pnpm test` 作为 CI 命令目前不可用，需要更新或删除过期断言。

### TEST-2：第一批中的 405 多数是测试脚本方法假设错误

- `PUT /requirements/{id}/ac`：当前路由只有 `GET /requirements/{id}/ac` 和 `POST /requirements/{id}/regenerate-ac`。
- `POST /reports/team/today`、`POST /reports/department/today`：当前保存路由是 `PUT`。

复跑已按真实路由验证，相关保存链路通过。

## UI 截图

截图目录已上传：

`doc/test-reports/REG-20260705-UI-20260705163837-screenshots/`

覆盖：

- PM / 总监 / TL-A / 员工-A1
- 1366x768 / 1440x900
- Dashboard / 需求 / 日报 / 周报

## 结论

本轮自动化已经覆盖了上线前最关键的主链路。当前阻断级问题只有一个明确后端健壮性问题：`PUT /reports/today` 返回 500。PM team weekly 行为需要产品口径确认。现有 `pnpm test` 脚本过期，需要修正，否则 CI 会误报失败。
