# Dashboard / 需求 / 日报 / 周报自动化测试执行报告

执行时间：2026-07-05T16:37:16.283870Z
测试前缀：`REG-20260705-AUTO-163705`
API_BASE：`http://192.168.14.157:5173/api/v1`
测试用例来源：`doc/Dashboard需求日报周报上线前全量测试用例.md`

## 汇总

| 总数 | 通过 | 失败 |
| ---: | ---: | ---: |
| 251 | 238 | 13 |

## 分模块

| 模块 | 总数 | 通过 | 失败 |
| --- | ---: | ---: | ---: |
| AI生成 | 1 | 1 | 0 |
| Dashboard/Token | 32 | 32 | 0 |
| Token参数 | 2 | 2 | 0 |
| 任务写入 | 4 | 4 | 0 |
| 任务列表 | 20 | 20 | 0 |
| 任务状态 | 3 | 3 | 0 |
| 任务详情 | 18 | 18 | 0 |
| 任务进度 | 3 | 3 | 0 |
| 关注 | 24 | 24 | 0 |
| 基础数据 | 12 | 12 | 0 |
| 小组日报权限 | 2 | 0 | 2 |
| 日报写入 | 2 | 0 | 2 |
| 日报周报读取 | 48 | 44 | 4 |
| 部门日报权限 | 2 | 0 | 2 |
| 鉴权 | 6 | 6 | 0 |
| 需求写入 | 4 | 4 | 0 |
| 需求列表 | 44 | 44 | 0 |
| 需求编辑 | 3 | 3 | 0 |
| 需求详情 | 15 | 15 | 0 |
| 需求验收 | 6 | 3 | 3 |

## 失败明细

| ID | 模块 | 角色 | 请求 | 状态 | 期望 | 说明 | 响应摘要 |
| --- | --- | --- | --- | ---: | --- | --- | --- |
| AUTO-0108 | 需求验收 | PM | `PUT /requirements/c17ee3c0-a855-44ae-9881-d278bd70eac7/ac` | 405 | 200 |  |  |
| AUTO-0116 | 需求验收 | PM | `PUT /requirements/5d5064dd-258d-4215-8e83-cb5c556f3095/ac` | 405 | 200 |  |  |
| AUTO-0124 | 需求验收 | PM | `PUT /requirements/f498ba0e-b0da-482b-b9d7-af9cec13e52f/ac` | 405 | 200 |  |  |
| AUTO-0205 | 日报周报读取 | PM | `GET /reports/team/weekly/current?week_start=2026-06-29` | 400 | [200, 403, 404] |  | {"error":"no team specified"}  |
| AUTO-0206 | 日报周报读取 | PM | `GET /reports/team/weekly/sources?week_start=2026-06-29` | 400 | [200, 403, 404] |  | {"error":"no team specified"}  |
| AUTO-0217 | 日报周报读取 | DIR | `GET /reports/team/weekly/current?week_start=2026-06-29` | 400 | [200, 403, 404] |  | {"error":"team_id is required"}  |
| AUTO-0218 | 日报周报读取 | DIR | `GET /reports/team/weekly/sources?week_start=2026-06-29` | 400 | [200, 403, 404] |  | {"error":"team_id is required"}  |
| AUTO-0245 | 日报写入 | PM | `PUT /reports/today` | 500 | 200 |  | {"error":"pq: invalid input syntax for type uuid: \"today\" (22P02)"}  |
| AUTO-0246 | 日报写入 | EMP | `PUT /reports/today` | 500 | 200 |  | {"error":"pq: invalid input syntax for type uuid: \"today\" (22P02)"}  |
| AUTO-0247 | 小组日报权限 | TL | `POST /reports/team/today` | 405 | [200, 201] |  |  |
| AUTO-0248 | 小组日报权限 | EMP | `POST /reports/team/today` | 405 | 403 |  |  |
| AUTO-0249 | 部门日报权限 | DIR | `POST /reports/department/today` | 405 | [200, 201] |  |  |
| AUTO-0250 | 部门日报权限 | TL | `POST /reports/department/today` | 405 | 403 |  |  |

## 全量明细

| ID | 模块 | 角色 | 请求 | 状态 | 结果 | 说明 |
| --- | --- | --- | --- | ---: | --- | --- |
| AUTO-0001 | 鉴权 | PM | `GET /auth/me` | 200 | PASS | 账号登录 |
| AUTO-0002 | 鉴权 | DIR | `GET /auth/me` | 200 | PASS | 账号登录 |
| AUTO-0003 | 鉴权 | TL | `GET /auth/me` | 200 | PASS | 账号登录 |
| AUTO-0004 | 鉴权 | EMP | `GET /auth/me` | 200 | PASS | 账号登录 |
| AUTO-0005 | 鉴权 | ANON | `GET /auth/me` | 401 | PASS | 无 token |
| AUTO-0006 | 鉴权 | ANON | `GET /auth/me` | 401 | PASS | 伪 token |
| AUTO-0007 | 基础数据 | PM | `GET /users` | 200 | PASS |  |
| AUTO-0008 | 基础数据 | PM | `GET /teams` | 200 | PASS |  |
| AUTO-0009 | 基础数据 | PM | `GET /task-assignees` | 200 | PASS |  |
| AUTO-0010 | 基础数据 | DIR | `GET /users` | 200 | PASS |  |
| AUTO-0011 | 基础数据 | DIR | `GET /teams` | 200 | PASS |  |
| AUTO-0012 | 基础数据 | DIR | `GET /task-assignees` | 200 | PASS |  |
| AUTO-0013 | 基础数据 | TL | `GET /users` | 200 | PASS |  |
| AUTO-0014 | 基础数据 | TL | `GET /teams` | 200 | PASS |  |
| AUTO-0015 | 基础数据 | TL | `GET /task-assignees` | 200 | PASS |  |
| AUTO-0016 | 基础数据 | EMP | `GET /users` | 200 | PASS |  |
| AUTO-0017 | 基础数据 | EMP | `GET /teams` | 200 | PASS |  |
| AUTO-0018 | 基础数据 | EMP | `GET /task-assignees` | 200 | PASS |  |
| AUTO-0019 | Dashboard/Token | PM | `GET /dashboard/my-items` | 200 | PASS |  |
| AUTO-0020 | Dashboard/Token | PM | `GET /dashboard/risks` | 200 | PASS |  |
| AUTO-0021 | Dashboard/Token | PM | `GET /dashboard/follows` | 200 | PASS |  |
| AUTO-0022 | Dashboard/Token | PM | `GET /tokens/sessions?from=2026-07-01&to=2026-07-05&scope=mine&page=1&page_size=10` | 200 | PASS |  |
| AUTO-0023 | Dashboard/Token | PM | `GET /tokens/sessions?from=2026-07-01&to=2026-07-05&scope=team&page=1&page_size=10` | 200 | PASS |  |
| AUTO-0024 | Dashboard/Token | PM | `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=model&scope=mine` | 200 | PASS |  |
| AUTO-0025 | Dashboard/Token | PM | `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=user&scope=mine` | 200 | PASS |  |
| AUTO-0026 | Dashboard/Token | PM | `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=team` | 200 | PASS |  |
| AUTO-0027 | Dashboard/Token | DIR | `GET /dashboard/my-items` | 200 | PASS |  |
| AUTO-0028 | Dashboard/Token | DIR | `GET /dashboard/risks` | 200 | PASS |  |
| AUTO-0029 | Dashboard/Token | DIR | `GET /dashboard/follows` | 200 | PASS |  |
| AUTO-0030 | Dashboard/Token | DIR | `GET /tokens/sessions?from=2026-07-01&to=2026-07-05&scope=mine&page=1&page_size=10` | 200 | PASS |  |
| AUTO-0031 | Dashboard/Token | DIR | `GET /tokens/sessions?from=2026-07-01&to=2026-07-05&scope=team&page=1&page_size=10` | 200 | PASS |  |
| AUTO-0032 | Dashboard/Token | DIR | `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=model&scope=mine` | 200 | PASS |  |
| AUTO-0033 | Dashboard/Token | DIR | `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=user&scope=mine` | 200 | PASS |  |
| AUTO-0034 | Dashboard/Token | DIR | `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=team` | 200 | PASS |  |
| AUTO-0035 | Dashboard/Token | TL | `GET /dashboard/my-items` | 200 | PASS |  |
| AUTO-0036 | Dashboard/Token | TL | `GET /dashboard/risks` | 200 | PASS |  |
| AUTO-0037 | Dashboard/Token | TL | `GET /dashboard/follows` | 200 | PASS |  |
| AUTO-0038 | Dashboard/Token | TL | `GET /tokens/sessions?from=2026-07-01&to=2026-07-05&scope=mine&page=1&page_size=10` | 200 | PASS |  |
| AUTO-0039 | Dashboard/Token | TL | `GET /tokens/sessions?from=2026-07-01&to=2026-07-05&scope=team&page=1&page_size=10` | 200 | PASS |  |
| AUTO-0040 | Dashboard/Token | TL | `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=model&scope=mine` | 200 | PASS |  |
| AUTO-0041 | Dashboard/Token | TL | `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=user&scope=mine` | 200 | PASS |  |
| AUTO-0042 | Dashboard/Token | TL | `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=team` | 200 | PASS |  |
| AUTO-0043 | Dashboard/Token | EMP | `GET /dashboard/my-items` | 200 | PASS |  |
| AUTO-0044 | Dashboard/Token | EMP | `GET /dashboard/risks` | 200 | PASS |  |
| AUTO-0045 | Dashboard/Token | EMP | `GET /dashboard/follows` | 200 | PASS |  |
| AUTO-0046 | Dashboard/Token | EMP | `GET /tokens/sessions?from=2026-07-01&to=2026-07-05&scope=mine&page=1&page_size=10` | 200 | PASS |  |
| AUTO-0047 | Dashboard/Token | EMP | `GET /tokens/sessions?from=2026-07-01&to=2026-07-05&scope=team&page=1&page_size=10` | 200 | PASS |  |
| AUTO-0048 | Dashboard/Token | EMP | `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=model&scope=mine` | 200 | PASS |  |
| AUTO-0049 | Dashboard/Token | EMP | `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=user&scope=mine` | 200 | PASS |  |
| AUTO-0050 | Dashboard/Token | EMP | `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=team` | 200 | PASS |  |
| AUTO-0051 | Token参数 | PM | `GET /tokens?period=last3days&group_by=model&scope=mine` | 400 | PASS |  |
| AUTO-0052 | Token参数 | PM | `GET /tokens?period=range&group_by=model&scope=mine` | 400 | PASS |  |
| AUTO-0053 | 需求写入 | PM | `POST /requirements` | 201 | PASS | PM跨团队需求 |
| AUTO-0054 | 需求写入 | DIR | `POST /requirements` | 201 | PASS | 总监需求 |
| AUTO-0055 | 需求写入 | TL | `POST /requirements` | 201 | PASS | TL本组需求 |
| AUTO-0056 | 需求写入 | EMP | `POST /requirements` | 403 | PASS | 员工创建需求应拒绝 |
| AUTO-0057 | 需求列表 | PM | `GET /requirements` | 200 | PASS |  |
| AUTO-0058 | 需求列表 | PM | `GET /requirements?scope=mine` | 200 | PASS |  |
| AUTO-0059 | 需求列表 | PM | `GET /requirements?scope=following` | 200 | PASS |  |
| AUTO-0060 | 需求列表 | PM | `GET /requirements?scope=owner` | 200 | PASS |  |
| AUTO-0061 | 需求列表 | PM | `GET /requirements?scope=created` | 200 | PASS |  |
| AUTO-0062 | 需求列表 | PM | `GET /requirements?scope=all` | 200 | PASS |  |
| AUTO-0063 | 需求列表 | PM | `GET /requirements?stage=todo` | 200 | PASS |  |
| AUTO-0064 | 需求列表 | PM | `GET /requirements?stage=review` | 200 | PASS |  |
| AUTO-0065 | 需求列表 | PM | `GET /requirements?priority=medium` | 200 | PASS |  |
| AUTO-0066 | 需求列表 | PM | `GET /requirements?risk_type=overdue` | 200 | PASS |  |
| AUTO-0067 | 需求列表 | PM | `GET /requirements?page=1&page_size=10` | 200 | PASS |  |
| AUTO-0068 | 需求列表 | DIR | `GET /requirements` | 200 | PASS |  |
| AUTO-0069 | 需求列表 | DIR | `GET /requirements?scope=mine` | 200 | PASS |  |
| AUTO-0070 | 需求列表 | DIR | `GET /requirements?scope=following` | 200 | PASS |  |
| AUTO-0071 | 需求列表 | DIR | `GET /requirements?scope=owner` | 200 | PASS |  |
| AUTO-0072 | 需求列表 | DIR | `GET /requirements?scope=created` | 200 | PASS |  |
| AUTO-0073 | 需求列表 | DIR | `GET /requirements?scope=all` | 200 | PASS |  |
| AUTO-0074 | 需求列表 | DIR | `GET /requirements?stage=todo` | 200 | PASS |  |
| AUTO-0075 | 需求列表 | DIR | `GET /requirements?stage=review` | 200 | PASS |  |
| AUTO-0076 | 需求列表 | DIR | `GET /requirements?priority=medium` | 200 | PASS |  |
| AUTO-0077 | 需求列表 | DIR | `GET /requirements?risk_type=overdue` | 200 | PASS |  |
| AUTO-0078 | 需求列表 | DIR | `GET /requirements?page=1&page_size=10` | 200 | PASS |  |
| AUTO-0079 | 需求列表 | TL | `GET /requirements` | 200 | PASS |  |
| AUTO-0080 | 需求列表 | TL | `GET /requirements?scope=mine` | 200 | PASS |  |
| AUTO-0081 | 需求列表 | TL | `GET /requirements?scope=following` | 200 | PASS |  |
| AUTO-0082 | 需求列表 | TL | `GET /requirements?scope=owner` | 200 | PASS |  |
| AUTO-0083 | 需求列表 | TL | `GET /requirements?scope=created` | 200 | PASS |  |
| AUTO-0084 | 需求列表 | TL | `GET /requirements?scope=all` | 200 | PASS |  |
| AUTO-0085 | 需求列表 | TL | `GET /requirements?stage=todo` | 200 | PASS |  |
| AUTO-0086 | 需求列表 | TL | `GET /requirements?stage=review` | 200 | PASS |  |
| AUTO-0087 | 需求列表 | TL | `GET /requirements?priority=medium` | 200 | PASS |  |
| AUTO-0088 | 需求列表 | TL | `GET /requirements?risk_type=overdue` | 200 | PASS |  |
| AUTO-0089 | 需求列表 | TL | `GET /requirements?page=1&page_size=10` | 200 | PASS |  |
| AUTO-0090 | 需求列表 | EMP | `GET /requirements` | 200 | PASS |  |
| AUTO-0091 | 需求列表 | EMP | `GET /requirements?scope=mine` | 200 | PASS |  |
| AUTO-0092 | 需求列表 | EMP | `GET /requirements?scope=following` | 200 | PASS |  |
| AUTO-0093 | 需求列表 | EMP | `GET /requirements?scope=owner` | 200 | PASS |  |
| AUTO-0094 | 需求列表 | EMP | `GET /requirements?scope=created` | 200 | PASS |  |
| AUTO-0095 | 需求列表 | EMP | `GET /requirements?scope=all` | 200 | PASS |  |
| AUTO-0096 | 需求列表 | EMP | `GET /requirements?stage=todo` | 200 | PASS |  |
| AUTO-0097 | 需求列表 | EMP | `GET /requirements?stage=review` | 200 | PASS |  |
| AUTO-0098 | 需求列表 | EMP | `GET /requirements?priority=medium` | 200 | PASS |  |
| AUTO-0099 | 需求列表 | EMP | `GET /requirements?risk_type=overdue` | 200 | PASS |  |
| AUTO-0100 | 需求列表 | EMP | `GET /requirements?page=1&page_size=10` | 200 | PASS |  |
| AUTO-0101 | 需求详情 | PM | `GET /requirements/c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0102 | 需求详情 | DIR | `GET /requirements/c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0103 | 需求详情 | TL | `GET /requirements/c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0104 | 需求详情 | EMP | `GET /requirements/c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0105 | 需求编辑 | PM | `PUT /requirements/c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0106 | 需求详情 | PM | `GET /requirements/c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0107 | 需求验收 | PM | `GET /requirements/c17ee3c0-a855-44ae-9881-d278bd70eac7/ac` | 200 | PASS |  |
| AUTO-0108 | 需求验收 | PM | `PUT /requirements/c17ee3c0-a855-44ae-9881-d278bd70eac7/ac` | 405 | FAIL |  |
| AUTO-0109 | 需求详情 | PM | `GET /requirements/5d5064dd-258d-4215-8e83-cb5c556f3095` | 200 | PASS |  |
| AUTO-0110 | 需求详情 | DIR | `GET /requirements/5d5064dd-258d-4215-8e83-cb5c556f3095` | 200 | PASS |  |
| AUTO-0111 | 需求详情 | TL | `GET /requirements/5d5064dd-258d-4215-8e83-cb5c556f3095` | 200 | PASS |  |
| AUTO-0112 | 需求详情 | EMP | `GET /requirements/5d5064dd-258d-4215-8e83-cb5c556f3095` | 200 | PASS |  |
| AUTO-0113 | 需求编辑 | PM | `PUT /requirements/5d5064dd-258d-4215-8e83-cb5c556f3095` | 200 | PASS |  |
| AUTO-0114 | 需求详情 | PM | `GET /requirements/5d5064dd-258d-4215-8e83-cb5c556f3095` | 200 | PASS |  |
| AUTO-0115 | 需求验收 | PM | `GET /requirements/5d5064dd-258d-4215-8e83-cb5c556f3095/ac` | 200 | PASS |  |
| AUTO-0116 | 需求验收 | PM | `PUT /requirements/5d5064dd-258d-4215-8e83-cb5c556f3095/ac` | 405 | FAIL |  |
| AUTO-0117 | 需求详情 | PM | `GET /requirements/f498ba0e-b0da-482b-b9d7-af9cec13e52f` | 200 | PASS |  |
| AUTO-0118 | 需求详情 | DIR | `GET /requirements/f498ba0e-b0da-482b-b9d7-af9cec13e52f` | 200 | PASS |  |
| AUTO-0119 | 需求详情 | TL | `GET /requirements/f498ba0e-b0da-482b-b9d7-af9cec13e52f` | 200 | PASS |  |
| AUTO-0120 | 需求详情 | EMP | `GET /requirements/f498ba0e-b0da-482b-b9d7-af9cec13e52f` | 200 | PASS |  |
| AUTO-0121 | 需求编辑 | PM | `PUT /requirements/f498ba0e-b0da-482b-b9d7-af9cec13e52f` | 200 | PASS |  |
| AUTO-0122 | 需求详情 | PM | `GET /requirements/f498ba0e-b0da-482b-b9d7-af9cec13e52f` | 200 | PASS |  |
| AUTO-0123 | 需求验收 | PM | `GET /requirements/f498ba0e-b0da-482b-b9d7-af9cec13e52f/ac` | 200 | PASS |  |
| AUTO-0124 | 需求验收 | PM | `PUT /requirements/f498ba0e-b0da-482b-b9d7-af9cec13e52f/ac` | 405 | FAIL |  |
| AUTO-0125 | 任务写入 | PM | `POST /tasks` | 201 | PASS | NPU SDK freeze |
| AUTO-0126 | 任务写入 | PM | `POST /tasks` | 201 | PASS | Agent 审计联调 |
| AUTO-0127 | 任务写入 | TL | `POST /tasks` | 201 | PASS | TL拆解本组任务 |
| AUTO-0128 | 任务写入 | EMP | `POST /tasks` | 201 | PASS | 员工任务权限按当前实现 |
| AUTO-0129 | 任务列表 | PM | `GET /tasks` | 200 | PASS |  |
| AUTO-0130 | 任务列表 | PM | `GET /tasks?status=todo` | 200 | PASS |  |
| AUTO-0131 | 任务列表 | PM | `GET /tasks?status=in_progress` | 200 | PASS |  |
| AUTO-0132 | 任务列表 | PM | `GET /tasks?status=done` | 200 | PASS |  |
| AUTO-0133 | 任务列表 | PM | `GET /tasks?page=1&page_size=10` | 200 | PASS |  |
| AUTO-0134 | 任务列表 | DIR | `GET /tasks` | 200 | PASS |  |
| AUTO-0135 | 任务列表 | DIR | `GET /tasks?status=todo` | 200 | PASS |  |
| AUTO-0136 | 任务列表 | DIR | `GET /tasks?status=in_progress` | 200 | PASS |  |
| AUTO-0137 | 任务列表 | DIR | `GET /tasks?status=done` | 200 | PASS |  |
| AUTO-0138 | 任务列表 | DIR | `GET /tasks?page=1&page_size=10` | 200 | PASS |  |
| AUTO-0139 | 任务列表 | TL | `GET /tasks` | 200 | PASS |  |
| AUTO-0140 | 任务列表 | TL | `GET /tasks?status=todo` | 200 | PASS |  |
| AUTO-0141 | 任务列表 | TL | `GET /tasks?status=in_progress` | 200 | PASS |  |
| AUTO-0142 | 任务列表 | TL | `GET /tasks?status=done` | 200 | PASS |  |
| AUTO-0143 | 任务列表 | TL | `GET /tasks?page=1&page_size=10` | 200 | PASS |  |
| AUTO-0144 | 任务列表 | EMP | `GET /tasks` | 200 | PASS |  |
| AUTO-0145 | 任务列表 | EMP | `GET /tasks?status=todo` | 200 | PASS |  |
| AUTO-0146 | 任务列表 | EMP | `GET /tasks?status=in_progress` | 200 | PASS |  |
| AUTO-0147 | 任务列表 | EMP | `GET /tasks?status=done` | 200 | PASS |  |
| AUTO-0148 | 任务列表 | EMP | `GET /tasks?page=1&page_size=10` | 200 | PASS |  |
| AUTO-0149 | 任务详情 | PM | `GET /tasks/f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0150 | 任务详情 | DIR | `GET /tasks/f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0151 | 任务详情 | TL | `GET /tasks/f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0152 | 任务详情 | EMP | `GET /tasks/f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0153 | 任务详情 | PM | `GET /tasks/f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0154 | 任务进度 | PM | `PUT /tasks/f2e60019-ee1d-41cc-ae39-6a60e652d27e/progress` | 200 | PASS |  |
| AUTO-0155 | 任务详情 | PM | `GET /tasks/f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0156 | 任务状态 | PM | `PUT /tasks/f2e60019-ee1d-41cc-ae39-6a60e652d27e/status` | 200 | PASS |  |
| AUTO-0157 | 任务详情 | PM | `GET /tasks/5553cb5e-5210-4f16-81d0-d5a073c9591f` | 200 | PASS |  |
| AUTO-0158 | 任务详情 | DIR | `GET /tasks/5553cb5e-5210-4f16-81d0-d5a073c9591f` | 200 | PASS |  |
| AUTO-0159 | 任务详情 | TL | `GET /tasks/5553cb5e-5210-4f16-81d0-d5a073c9591f` | 200 | PASS |  |
| AUTO-0160 | 任务详情 | EMP | `GET /tasks/5553cb5e-5210-4f16-81d0-d5a073c9591f` | 200 | PASS |  |
| AUTO-0161 | 任务详情 | PM | `GET /tasks/5553cb5e-5210-4f16-81d0-d5a073c9591f` | 200 | PASS |  |
| AUTO-0162 | 任务进度 | PM | `PUT /tasks/5553cb5e-5210-4f16-81d0-d5a073c9591f/progress` | 200 | PASS |  |
| AUTO-0163 | 任务详情 | PM | `GET /tasks/5553cb5e-5210-4f16-81d0-d5a073c9591f` | 200 | PASS |  |
| AUTO-0164 | 任务状态 | PM | `PUT /tasks/5553cb5e-5210-4f16-81d0-d5a073c9591f/status` | 200 | PASS |  |
| AUTO-0165 | 任务详情 | PM | `GET /tasks/40112e2f-a1c7-4063-b503-bee71089caea` | 200 | PASS |  |
| AUTO-0166 | 任务详情 | DIR | `GET /tasks/40112e2f-a1c7-4063-b503-bee71089caea` | 200 | PASS |  |
| AUTO-0167 | 任务详情 | TL | `GET /tasks/40112e2f-a1c7-4063-b503-bee71089caea` | 200 | PASS |  |
| AUTO-0168 | 任务详情 | EMP | `GET /tasks/40112e2f-a1c7-4063-b503-bee71089caea` | 200 | PASS |  |
| AUTO-0169 | 任务详情 | PM | `GET /tasks/40112e2f-a1c7-4063-b503-bee71089caea` | 200 | PASS |  |
| AUTO-0170 | 任务进度 | PM | `PUT /tasks/40112e2f-a1c7-4063-b503-bee71089caea/progress` | 200 | PASS |  |
| AUTO-0171 | 任务详情 | PM | `GET /tasks/40112e2f-a1c7-4063-b503-bee71089caea` | 200 | PASS |  |
| AUTO-0172 | 任务状态 | PM | `PUT /tasks/40112e2f-a1c7-4063-b503-bee71089caea/status` | 200 | PASS |  |
| AUTO-0173 | 关注 | PM | `POST /follows` | 200 | PASS |  |
| AUTO-0174 | 关注 | PM | `GET /follows/followers?target_type=requirement&target_id=c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0175 | 关注 | PM | `DELETE /follows/requirement/c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0176 | 关注 | DIR | `POST /follows` | 200 | PASS |  |
| AUTO-0177 | 关注 | DIR | `GET /follows/followers?target_type=requirement&target_id=c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0178 | 关注 | DIR | `DELETE /follows/requirement/c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0179 | 关注 | TL | `POST /follows` | 200 | PASS |  |
| AUTO-0180 | 关注 | TL | `GET /follows/followers?target_type=requirement&target_id=c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0181 | 关注 | TL | `DELETE /follows/requirement/c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0182 | 关注 | EMP | `POST /follows` | 200 | PASS |  |
| AUTO-0183 | 关注 | EMP | `GET /follows/followers?target_type=requirement&target_id=c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0184 | 关注 | EMP | `DELETE /follows/requirement/c17ee3c0-a855-44ae-9881-d278bd70eac7` | 200 | PASS |  |
| AUTO-0185 | 关注 | PM | `POST /follows` | 200 | PASS |  |
| AUTO-0186 | 关注 | PM | `GET /follows/followers?target_type=task&target_id=f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0187 | 关注 | PM | `DELETE /follows/task/f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0188 | 关注 | DIR | `POST /follows` | 200 | PASS |  |
| AUTO-0189 | 关注 | DIR | `GET /follows/followers?target_type=task&target_id=f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0190 | 关注 | DIR | `DELETE /follows/task/f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0191 | 关注 | TL | `POST /follows` | 200 | PASS |  |
| AUTO-0192 | 关注 | TL | `GET /follows/followers?target_type=task&target_id=f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0193 | 关注 | TL | `DELETE /follows/task/f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0194 | 关注 | EMP | `POST /follows` | 200 | PASS |  |
| AUTO-0195 | 关注 | EMP | `GET /follows/followers?target_type=task&target_id=f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0196 | 关注 | EMP | `DELETE /follows/task/f2e60019-ee1d-41cc-ae39-6a60e652d27e` | 200 | PASS |  |
| AUTO-0197 | 日报周报读取 | PM | `GET /reports/mine?from=2026-07-01&to=2026-07-05&page=1&page_size=10` | 200 | PASS |  |
| AUTO-0198 | 日报周报读取 | PM | `GET /reports/today?report_date=2026-07-05` | 200 | PASS |  |
| AUTO-0199 | 日报周报读取 | PM | `GET /reports/team/today?report_date=2026-07-05` | 404 | PASS |  |
| AUTO-0200 | 日报周报读取 | PM | `GET /reports/team/today/sources?report_date=2026-07-05` | 404 | PASS |  |
| AUTO-0201 | 日报周报读取 | PM | `GET /reports/department/today?report_date=2026-07-05` | 403 | PASS |  |
| AUTO-0202 | 日报周报读取 | PM | `GET /reports/department/today/sources?report_date=2026-07-05` | 404 | PASS |  |
| AUTO-0203 | 日报周报读取 | PM | `GET /reports/weekly/mine/current?week_start=2026-06-29` | 200 | PASS |  |
| AUTO-0204 | 日报周报读取 | PM | `GET /reports/weekly/mine/sources?week_start=2026-06-29` | 200 | PASS |  |
| AUTO-0205 | 日报周报读取 | PM | `GET /reports/team/weekly/current?week_start=2026-06-29` | 400 | FAIL |  |
| AUTO-0206 | 日报周报读取 | PM | `GET /reports/team/weekly/sources?week_start=2026-06-29` | 400 | FAIL |  |
| AUTO-0207 | 日报周报读取 | PM | `GET /reports/department/weekly/current?week_start=2026-06-29` | 403 | PASS |  |
| AUTO-0208 | 日报周报读取 | PM | `GET /reports/department/weekly/sources?week_start=2026-06-29` | 403 | PASS |  |
| AUTO-0209 | 日报周报读取 | DIR | `GET /reports/mine?from=2026-07-01&to=2026-07-05&page=1&page_size=10` | 200 | PASS |  |
| AUTO-0210 | 日报周报读取 | DIR | `GET /reports/today?report_date=2026-07-05` | 200 | PASS |  |
| AUTO-0211 | 日报周报读取 | DIR | `GET /reports/team/today?report_date=2026-07-05` | 404 | PASS |  |
| AUTO-0212 | 日报周报读取 | DIR | `GET /reports/team/today/sources?report_date=2026-07-05` | 404 | PASS |  |
| AUTO-0213 | 日报周报读取 | DIR | `GET /reports/department/today?report_date=2026-07-05` | 200 | PASS |  |
| AUTO-0214 | 日报周报读取 | DIR | `GET /reports/department/today/sources?report_date=2026-07-05` | 404 | PASS |  |
| AUTO-0215 | 日报周报读取 | DIR | `GET /reports/weekly/mine/current?week_start=2026-06-29` | 200 | PASS |  |
| AUTO-0216 | 日报周报读取 | DIR | `GET /reports/weekly/mine/sources?week_start=2026-06-29` | 200 | PASS |  |
| AUTO-0217 | 日报周报读取 | DIR | `GET /reports/team/weekly/current?week_start=2026-06-29` | 400 | FAIL |  |
| AUTO-0218 | 日报周报读取 | DIR | `GET /reports/team/weekly/sources?week_start=2026-06-29` | 400 | FAIL |  |
| AUTO-0219 | 日报周报读取 | DIR | `GET /reports/department/weekly/current?week_start=2026-06-29` | 200 | PASS |  |
| AUTO-0220 | 日报周报读取 | DIR | `GET /reports/department/weekly/sources?week_start=2026-06-29` | 200 | PASS |  |
| AUTO-0221 | 日报周报读取 | TL | `GET /reports/mine?from=2026-07-01&to=2026-07-05&page=1&page_size=10` | 200 | PASS |  |
| AUTO-0222 | 日报周报读取 | TL | `GET /reports/today?report_date=2026-07-05` | 200 | PASS |  |
| AUTO-0223 | 日报周报读取 | TL | `GET /reports/team/today?report_date=2026-07-05` | 404 | PASS |  |
| AUTO-0224 | 日报周报读取 | TL | `GET /reports/team/today/sources?report_date=2026-07-05` | 404 | PASS |  |
| AUTO-0225 | 日报周报读取 | TL | `GET /reports/department/today?report_date=2026-07-05` | 403 | PASS |  |
| AUTO-0226 | 日报周报读取 | TL | `GET /reports/department/today/sources?report_date=2026-07-05` | 404 | PASS |  |
| AUTO-0227 | 日报周报读取 | TL | `GET /reports/weekly/mine/current?week_start=2026-06-29` | 200 | PASS |  |
| AUTO-0228 | 日报周报读取 | TL | `GET /reports/weekly/mine/sources?week_start=2026-06-29` | 200 | PASS |  |
| AUTO-0229 | 日报周报读取 | TL | `GET /reports/team/weekly/current?week_start=2026-06-29` | 200 | PASS |  |
| AUTO-0230 | 日报周报读取 | TL | `GET /reports/team/weekly/sources?week_start=2026-06-29` | 200 | PASS |  |
| AUTO-0231 | 日报周报读取 | TL | `GET /reports/department/weekly/current?week_start=2026-06-29` | 403 | PASS |  |
| AUTO-0232 | 日报周报读取 | TL | `GET /reports/department/weekly/sources?week_start=2026-06-29` | 403 | PASS |  |
| AUTO-0233 | 日报周报读取 | EMP | `GET /reports/mine?from=2026-07-01&to=2026-07-05&page=1&page_size=10` | 200 | PASS |  |
| AUTO-0234 | 日报周报读取 | EMP | `GET /reports/today?report_date=2026-07-05` | 200 | PASS |  |
| AUTO-0235 | 日报周报读取 | EMP | `GET /reports/team/today?report_date=2026-07-05` | 404 | PASS |  |
| AUTO-0236 | 日报周报读取 | EMP | `GET /reports/team/today/sources?report_date=2026-07-05` | 404 | PASS |  |
| AUTO-0237 | 日报周报读取 | EMP | `GET /reports/department/today?report_date=2026-07-05` | 403 | PASS |  |
| AUTO-0238 | 日报周报读取 | EMP | `GET /reports/department/today/sources?report_date=2026-07-05` | 404 | PASS |  |
| AUTO-0239 | 日报周报读取 | EMP | `GET /reports/weekly/mine/current?week_start=2026-06-29` | 200 | PASS |  |
| AUTO-0240 | 日报周报读取 | EMP | `GET /reports/weekly/mine/sources?week_start=2026-06-29` | 200 | PASS |  |
| AUTO-0241 | 日报周报读取 | EMP | `GET /reports/team/weekly/current?week_start=2026-06-29` | 403 | PASS |  |
| AUTO-0242 | 日报周报读取 | EMP | `GET /reports/team/weekly/sources?week_start=2026-06-29` | 403 | PASS |  |
| AUTO-0243 | 日报周报读取 | EMP | `GET /reports/department/weekly/current?week_start=2026-06-29` | 403 | PASS |  |
| AUTO-0244 | 日报周报读取 | EMP | `GET /reports/department/weekly/sources?week_start=2026-06-29` | 403 | PASS |  |
| AUTO-0245 | 日报写入 | PM | `PUT /reports/today` | 500 | FAIL |  |
| AUTO-0246 | 日报写入 | EMP | `PUT /reports/today` | 500 | FAIL |  |
| AUTO-0247 | 小组日报权限 | TL | `POST /reports/team/today` | 405 | FAIL |  |
| AUTO-0248 | 小组日报权限 | EMP | `POST /reports/team/today` | 405 | FAIL |  |
| AUTO-0249 | 部门日报权限 | DIR | `POST /reports/department/today` | 405 | FAIL |  |
| AUTO-0250 | 部门日报权限 | TL | `POST /reports/department/today` | 405 | FAIL |  |
| AUTO-0251 | AI生成 | PM | `POST /ai-assets/report-agents/default/runs` | 200 | PASS |  |