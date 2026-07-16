# Dashboard / 需求 / 日报 / 周报上线前 Codex 执行版测试用例

生成日期：2026-07-05

本文件是给 Codex / Agent 执行上线前回归用的入口文档。

不要把 `Dashboard需求日报周报上线前全量测试用例.md` 里的 12512 行矩阵逐行机械执行。那份文件是覆盖索引。本文件定义如何把矩阵归并成可执行批次、如何造数、如何跑 UI/API、如何记录失败、什么时候停止。

## 1. 执行目标

覆盖以下 4 个核心页面和流程：

- Dashboard：我的事项、风险提示、报告入口、Token 用量、点击打开需求抽屉/任务 Modal。
- 需求：需求看板、需求列表、需求详情抽屉、任务详情 Modal、关注、负责人、参与团队、任务进度、状态流转、依赖、关联 session。
- 日报：个人日报、小组日报、部门日报、历史日期、AI 生成入口、保存/编辑/权限。
- 周报：个人周报、小组周报、部门周报、周范围、来源数据、保存/编辑/权限。

本轮回归必须同时覆盖：

- 4 个角色：PM、总监、TL-A、员工-A1。
- UI：1366x768 和 1440x900。
- API：正向、权限、参数错误、刷新后持久化。
- 错误体验：403 只能 toast / 接口错误，不允许整页跳 403。

## 2. 环境和账号

默认测试环境：

- 前端：`http://192.168.14.157:5173`
- API：`http://192.168.14.157:5173/api/v1`
- 远程项目目录：`/home/intellif/dev/project_manager`
- 测试账号来源：`doc/v1/测试账号文档.md`

角色账号：

| 角色 | 账号 | UID | 口径 |
| --- | --- | ---: | --- |
| PM | `t01` | 303 | 需求/任务全量管理；日报/周报个人；Token 个人 |
| 总监 | `t02` | 304 | 需求/任务全量管理；部门日报/周报；Token 全团队 |
| TL-A | `t03` | 305 | 小组A需求/任务管理；小组日报/周报；Token 本组 |
| 员工-A1 | `t05` | 307 | 小组A需求查看；本人任务操作；日报/周报个人；Token 个人 |

Codex 执行时不要手写 token。必须从 `doc/v1/测试账号文档.md` 读取。

## 3. 执行方式总则

### 3.1 不逐行执行矩阵

矩阵文件的 12512 条来自：

```text
测试点 × 角色 × UI1366 × UI1440 × API × RELOAD
```

Codex 执行时要按以下规则归并：

- 同一模块、同一功能、同一角色的 API 用例合并为一个 API 批次。
- 同一页面、同一角色、同一视口的 UI 用例合并为一个 Playwright 页面批次。
- RELOAD 用例只在写操作后执行，不对每条读操作重复刷新。
- P0 必须全跑；P1 可以按模块批量跑，但不能跳过模块。
- 如果某条失败，不要中断全量，继续跑并记录失败。只有环境级阻断才停止。

### 3.2 必须生成唯一测试前缀

每轮执行先生成：

```text
REG-YYYYMMDD-HHMMSS
```

所有新增需求、任务、日报内容、周报内容必须带此前缀，方便清理和定位。

### 3.3 写操作规则

- 可以创建测试需求和测试任务。
- 可以编辑本轮创建的测试需求和任务。
- 可以保存测试账号自己的日报/周报。
- 不要删除或覆盖非本轮前缀的数据。
- 如果需要清理，只清理标题包含本轮前缀的数据。

## 4. 阻断条件

出现以下情况可以停止，并直接写失败报告：

1. 4 个测试账号全部 `/auth/me` 失败。
2. 前端 `/dashboard` 无法打开。
3. API 基础路径 `/api/v1/auth/me` 不可达。
4. 数据库或服务返回大量 5xx，且不同模块连续 10 条以上失败。
5. 登录态无法注入，导致 UI 批次全部跳登录页。

以下情况不能停止，必须继续跑：

- 某个角色 403。
- 某个功能按钮不可见。
- 某个接口返回 400/404。
- 某个 Modal 样式异常。
- 某个页面 console error。

这些都要作为失败项记录。

## 5. 标准执行批次

### Batch 0：环境准备

目的：确认服务、账号、工程测试环境可用。

必须执行：

```bash
ssh 157 'cd /home/intellif/dev/project_manager && test -f doc/v1/测试账号文档.md && test -f doc/v1/Dashboard需求日报周报上线前全量测试用例.md'
ssh 157 'bash -lc "source ~/.nvm/nvm.sh && cd /home/intellif/dev/project_manager/web && pnpm typecheck"'
ssh 157 'bash -lc "source ~/.nvm/nvm.sh && cd /home/intellif/dev/project_manager/api && go test ./..."'
```

必须验证：

- PM、总监、TL-A、员工-A1 的 `/auth/me` 均为 200。
- 返回角色和账号文档一致。
- TL-A 和员工-A1 都属于小组A。

失败记录：

- 命令、状态码、响应摘要。

### Batch 1：基础 API 和权限边界

角色：4 个角色全跑。

接口：

- `GET /auth/me`
- `GET /users`
- `GET /teams`
- `GET /task-assignees`
- `GET /dashboard/my-items`
- `GET /dashboard/risks`
- `GET /dashboard/follows`
- `GET /tokens/sessions?from=2026-07-01&to=2026-07-05&scope=mine&page=1&page_size=10`
- `GET /tokens/sessions?from=2026-07-01&to=2026-07-05&scope=team&page=1&page_size=10`
- `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=model&scope=mine`
- `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=user&scope=mine`
- `GET /tokens?period=range&from=2026-07-01&to=2026-07-05&group_by=team`

权限重点：

- PM 和员工在 Token 模块只能看到本人数据。
- TL-A 的 team scope 只能是小组A。
- 总监可以看到团队聚合。
- 非法 token 返回 401。
- 非法 period / 缺少 range 参数返回 400。

### Batch 2：需求和任务完整主链路

使用 PM 创建以下测试数据：

| 编号 | 标题后缀 | 团队 | 负责人 | 任务 |
| --- | --- | --- | --- | --- |
| R1 | `NPU 频控验证链路` | 小组A | PM、TL-A | SDK freeze、频控脚本、Agent 审计 |
| R2 | `Agent 报告生成依赖追溯` | 小组A + 小组B | PM、TL-A | MCP session 切片、运行历史回填 |
| R3 | `跨团队需求分配验证` | 小组A + 小组B | TL-A、员工A1 | 跨团队可见性、负责人验证 |
| R4 | `可删除空需求验证` | 无团队或小组A | 无 | 不创建任务 |

使用 TL-A 创建：

| 编号 | 标题后缀 | 团队 | 负责人 | 任务 |
| --- | --- | --- | --- | --- |
| R5 | `TL 本组推进验证` | 小组A | TL-A、员工A1 | TL 拆解任务 |

必须验证：

- PM / 总监可以查看和编辑 R1-R5。
- TL-A 可以管理小组A相关需求。
- 员工-A1 可以查看小组A需求。
- 员工创建需求应返回 403。
- 需求可以修改标题、描述、优先级、截止日期、负责人、参与团队。
- 需求详情抽屉打开后任务列表可见。
- 任务可以创建、编辑、更新进度、切换状态：未开始、进行中、已完成。
- 任务依赖可以创建并在任务详情 Modal 中可见。
- 关注/取消关注需求和任务后，关注人员和 Dashboard 数据刷新正确。

写操作后必须执行：

- 重新 GET 对象详情。
- 刷新前端页面后重新打开详情。
- 检查 `base_version` 冲突返回 409 或合理错误。

### Batch 3：Dashboard UI

工具：优先使用当前主机 Playwright。

如果当前主机 Playwright 可用：

```bash
cd /home/aied/lx/playwright-local
node --input-type=module -e "import { chromium } from 'playwright'; console.log(typeof chromium.launch)"
```

如果当前主机不可用，使用远程 web 依赖：

```bash
ssh 157 'bash -lc "source ~/.nvm/nvm.sh && cd /home/intellif/dev/project_manager/web && node --input-type=module -e '\''import { chromium } from \"@playwright/test\"; console.log(typeof chromium.launch);'\''"'
```

角色：PM、总监、TL-A、员工-A1。

视口：

- 1366x768
- 1440x900

页面：

- `/dashboard`

必须操作：

- 打开页面，确认不跳登录页。
- 检查菜单高亮、header 用户角色。
- 我的事项默认最多 5 条。
- 点击“展开全部/收起”，列表内部滚动，高度不撑爆页面。
- 点击需求事项，在 Dashboard 内打开需求详情抽屉，不跳转需求页。
- 点击任务事项，在 Dashboard 内打开任务详情 Modal，不跳转需求页。
- 风险提示默认最多 5 条。
- 点击风险提示里的需求/任务，打开正确对象。
- Token 用量切换：昨天、近 3 天、近 7 天。
- 近 7 天图表不撑高、不重叠。
- 报告入口能打开对应日报/周报弹窗。

失败必须截图。

### Batch 4：需求页面 UI

页面：

- `/requirements?scope=mine`
- `/requirements?scope=following`
- `/requirements?scope=owner`
- `/requirements?scope=created`
- `/requirements?scope=all`

角色：4 个角色全跑。

视口：1366x768、1440x900。

必须操作：

- 业务范围 tab 切换。
- 阶段看板 / 需求列表切换。
- 搜索需求标题、任务标题、负责人。
- 阶段筛选、优先级筛选、风险筛选、重置。
- 新建需求按钮。
- 点击需求卡片打开详情抽屉。
- 需求详情抽屉中切换 Tab：任务、验收、关联 Session、信息。
- 点击任务“查看”打开任务详情 Modal。
- 任务 Modal 中编辑、删除按钮视觉和权限正确。
- 任务状态切换。
- 任务进度滑块。
- 上游依赖点击打开其他任务后，能继续关闭返回当前上下文。
- 关联 Session 弹窗无横向滚动，已关联 session 仍在列表里可勾掉。

视觉必须检查：

- 长标题不撑爆。
- 多负责人、多团队显示 `+N`。
- 风险 chip 不恢复大红框。
- Drawer 和 Modal 高度不超过屏幕。
- 403 不整页跳转。

### Batch 5：日报页面

页面：

- `/reports/daily`
- Dashboard 中日报入口。

角色：

- PM：个人日报。
- 员工-A1：个人日报。
- TL-A：个人日报、小组日报。
- 总监：个人日报、部门日报。

必须操作：

- 打开日报弹窗。
- 日期切换到今天、昨天、2026-07-03。
- 填写日报。
- 保存。
- 关闭弹窗后重新打开，内容仍存在。
- 从历史记录打开。
- 从历史记录编辑时日期不可切换。
- AI 生成入口：没有默认 agent 时必须提示配置，不允许直接打 `/default/runs` 报错。
- 有默认 agent 时允许触发运行，并显示 loading / 轮询状态。
- 个人日报 AI 设置里可以选择 session slice；小组/部门日报不显示个人 session 选择。

接口注意：

- 个人日报保存真实链路是先 `GET /reports/today?report_date=...` 获取 id，再 `PUT /reports/{id}`。
- 不要把 `PUT /reports/today` 当作正常保存链路。
- 但必须额外测一次 `PUT /reports/today`，它不应该返回 500；如果返回 500，记录为后端健壮性 bug。
- 小组日报保存使用 `PUT /reports/team/today`。
- 部门日报保存使用 `PUT /reports/department/today`。

### Batch 6：周报页面

页面：

- `/reports/weekly`
- Dashboard 中周报入口。

角色：

- PM：个人周报。
- 员工-A1：个人周报。
- TL-A：个人周报、小组周报。
- 总监：个人周报、部门周报。

必须操作：

- 打开当前周。
- 切换周范围。
- 保存周报。
- 关闭重开后内容仍存在。
- 小组周报来源数据正确。
- 部门周报来源数据正确。
- 权限不匹配入口不可见或接口返回 403。

接口注意：

- 个人周报：`/reports/weekly/mine/current*`
- 小组周报：`/reports/team/weekly/current*`
- 部门周报：`/reports/department/weekly/current*`
- 如果 PM 访问小组周报接口返回 `400 no team specified`，不要直接判 bug。先看产品口径：PM 是否应该能看小组周报。

### Batch 7：负向和异常体验

必须覆盖：

- 无 token 访问返回 401。
- 伪 token 返回 401。
- 员工创建需求返回 403。
- 员工保存小组日报返回 403。
- TL 保存部门日报返回 403。
- base_version 旧版本更新返回 409 或合理错误。
- 不存在的需求/任务 id 返回 404，不返回 500。
- 非法 UUID 不返回 500。
- 所有 403 前端表现为 toast / message，不跳整页 403。

## 6. 输出报告要求

每轮必须输出 3 类报告。

### 6.1 API 报告

路径：

```text
tmp/test-reports/dashboard-requirements-reports-<PREFIX>.md
```

必须包含：

- 总数、通过、失败。
- 分模块统计。
- 失败明细。
- 每条失败的 curl 或等价请求信息。
- 状态码。
- 响应摘要。
- 是否需要修代码，还是测试期望不匹配。

### 6.2 UI 报告

路径：

```text
tmp/test-reports/<PREFIX>-ui.md
tmp/test-reports/<PREFIX>-screenshots/
```

必须包含：

- 角色。
- 视口。
- 页面。
- 是否跳登录页。
- 是否出现 API 5xx。
- 是否有 pageerror / console error。
- 截图路径。

### 6.3 总报告

路径：

```text
tmp/test-reports/dashboard-requirements-reports-summary-<YYYYMMDD>.md
```

必须包含：

- 执行批次。
- 每批通过率。
- 阻断问题。
- 非阻断问题。
- 需要产品确认的问题。
- 下一步建议。

## 7. 失败分类规则

| 类型 | 判断标准 | 处理 |
| --- | --- | --- |
| 真 bug | 5xx、权限明显错误、数据保存丢失、UI 崩溃、403 整页跳转 | 记录为 BUG，建议修复 |
| 测试期望错误 | 接口方法或路径和当前代码不一致 | 修正文档或测试脚本 |
| 产品口径待确认 | PM 是否能看小组周报这类权限边界不清 | 标记为待确认 |
| 环境问题 | 服务不可达、依赖缺失、Playwright 不可用 | 标记环境阻断 |
| 视觉问题 | 截图可见错位、溢出、遮挡 | 截图并记录位置 |

## 8. 已知测试陷阱

1. 不要用 `PUT /reports/today` 保存个人日报。当前真实前端链路是 `GET /reports/today` 获取 id 后 `PUT /reports/{id}`。
2. 但 `PUT /reports/today` 不应返回 500，应该作为后端健壮性负向用例保留。
3. 需求验收标准当前没有 `PUT /requirements/{id}/ac` 路由，只有 `GET /requirements/{id}/ac` 和 `POST /requirements/{id}/regenerate-ac`。
4. 小组日报保存是 `PUT /reports/team/today`，不是 `POST`。
5. 部门日报保存是 `PUT /reports/department/today`，不是 `POST`。
6. PM 访问小组周报接口的预期需要产品口径确认，不要擅自判定。
7. `web pnpm test` 里可能有源码正则契约测试，如果失败要先判断测试脚本是否过期，不要直接等同于业务失败。

## 9. 最小可接受上线前回归

如果时间紧，至少跑：

1. Batch 0。
2. Batch 1。
3. Batch 2 中 PM + TL-A + 员工-A1 的需求/任务主链路。
4. Batch 3 Dashboard UI。
5. Batch 4 需求页面 UI。
6. Batch 5 个人日报 + 小组日报 + 部门日报保存。
7. Batch 6 个人周报 + 小组周报 + 部门周报读取。
8. Batch 7 中所有 401/403/500 负向。

最小回归也必须输出总报告。

## 10. Codex 执行提示词

以后可以直接这样下发：

```text
按 doc/v1/Dashboard需求日报周报上线前Codex执行版测试用例.md 跑一轮上线前回归。
不要逐行机械执行矩阵，按文档批次归并执行。
必须生成 API 报告、UI 报告、截图目录和总报告。
失败不要中断，除非命中阻断条件。
```

