# Dashboard / 需求 / 日报 / 周报 Codex 上线前回归总报告

执行时间：2026-07-05T17:18:10.323044Z
测试入口：`doc/Dashboard需求日报周报上线前Codex执行版测试用例.md`
测试环境：`http://192.168.14.157:5173`
测试前缀：`REG-20260705-CODEX-171541`

## 汇总

| 批次 | 范围 | 总数 | 通过 | 失败 | 报告 |
| --- | --- | ---: | ---: | ---: | --- |
| Batch 0 | 文档存在、前端 typecheck、后端 go test | 3 | 3 | 0 | 命令输出通过 |
| API | 鉴权、Dashboard、Token、需求、任务、关注、日报、周报、负向 | 290 | 283 | 7 | `doc/test-reports/REG-20260705-CODEX-171541-api.md` |
| UI | 4角色 × 2视口 × Dashboard/需求/日报/周报 | 32 | 32 | 0 | `doc/test-reports/REG-20260705-CODEX-171541-ui.md` |

截图目录：`doc/test-reports/REG-20260705-CODEX-171541-screenshots/`

## 已确认通过

- PM、总监、TL-A、员工-A1 的 `/auth/me` 均通过，角色匹配。
- Dashboard 基础接口通过：`/dashboard/my-items`、`/dashboard/risks`、`/dashboard/follows`。
- Token 接口通过，非法 period/range 参数返回 400。
- 需求创建、列表、详情、编辑、AC 读取通过。
- 任务创建、列表、详情、进度更新、状态切换通过。
- 关注/取消关注需求和任务通过。
- 个人日报真实链路通过：`GET /reports/today?report_date=...` 获取 id 后 `PUT /reports/{id}` 保存。
- TL 小组日报 `PUT /reports/team/today` 保存通过，员工保存返回 403。
- 总监部门日报 `PUT /reports/department/today` 保存通过，TL 保存返回 403。
- 默认报告 Agent 运行入口没有 500。
- UI 页面可达性通过：4 个角色在 1366x768 和 1440x900 下打开 Dashboard、需求、日报、周报均未跳登录页、未空白、无 API 5xx。
- `web pnpm typecheck` 通过。
- `api go test ./...` 通过。

## 失败项分类

### BUG-1：`PUT /api/v1/reports/today` 返回 500

请求：

```text
PUT /api/v1/reports/today
```

实际：

```text
500 {"error":"pq: invalid input syntax for type uuid: \"today\" (22P02)"}
```

判断：真实前端保存链路不是这个接口，但错误路径不应落到数据库 UUID 转换后返回 500。建议后端对 `/reports/{id}` 的 id 做 UUID 校验，非法 id 返回 400/404。

### NEED-CONFIRM-1：PM 访问小组周报带 team_id 返回 400

请求：

```text
GET /api/v1/reports/team/weekly/current?week_start=2026-06-29&team_id=3f05e6ed-c3bc-4900-8d7b-ea89843e157a
GET /api/v1/reports/team/weekly/sources?week_start=2026-06-29&team_id=3f05e6ed-c3bc-4900-8d7b-ea89843e157a
```

PM 实际：`400 {"error":"no team specified"}`

总监同接口：200。

判断：如果 PM 不应该看小组周报，这个是测试期望要调整；如果 PM 应该能看小组周报，则后端 PM 的 team_id 解析/权限判断有问题。

### TEST-1：`POST /requirements/{id}/regenerate-ac` 返回 400 invalid request

4 条失败均为：

```text
POST /requirements/{id}/regenerate-ac -> 400 {"error":"invalid request"}
```

判断：接口路由存在，但测试脚本没有传前端真实 payload。这更像测试用例执行参数不完整，不直接判业务 bug。后续需要根据前端真实调用补齐请求体。

## UI 说明

UI 报告中所有页面均为 PASS，但浏览器 console 记录了 Vite HMR WebSocket 连接失败：

```text
ws://192.168.28.25:5173/?token=... net::ERR_CONNECTION_REFUSED
```

判断：这是 dev server HMR 地址问题，不影响页面功能和 API 测试，本轮未作为失败处理。

## 结论

本轮按 Codex 执行版文档完成了一轮可执行回归。主链路可用，UI 可达性通过，工程类型检查和后端单测通过。

上线前建议优先处理：

1. 修复 `PUT /reports/today` 返回 500 的后端健壮性问题。
2. 确认 PM 是否应该访问小组周报接口。
3. 补齐 `regenerate-ac` 的测试 payload，避免后续测试误报。
