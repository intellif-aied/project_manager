# 日报周报Agent化开发执行记录

本文档记录日报 / 周报 Agent 化改造的阶段性执行口径。长期产品架构以 `日报周报产品架构设计文档.md` 为准，本文件只记录已确认的阶段结果和下一阶段实施边界。

## 阶段 A：AI Assets 基础可用性修复结果

目标：让 `/ai-assets` 页面在 Managed Agent 平台未配置或不可用时有清晰、稳定、可诊断的表现，不影响日报 / 周报业务页面。

已确认结果：

1. 后端 Aida 代理层区分 Managed Agent 平台错误类型：未配置、不可达、上游错误、权限不足、正常空数据。
2. 前端 `/ai-assets` 页面局部处理外部平台依赖失败，避免多个接口失败时连续弹多个“服务异常，请稍后重试”。
3. 平台未配置时，外部平台依赖操作置灰或给明确提示；本地可读的运行记录 / 定时任务列表不强行隐藏。
4. 本阶段不开发 Report MCP，不新增 MCP tool，不接日报 / 周报智能生成，不改数据库或迁移。

## 阶段 B：Report MCP tools 设计冻结结果

目标：冻结 Report MCP tools 的协议方向，避免后续把旧 session 来源选择接口包装成新 Agent 自取数接口。

已确认结果：

1. MCP 协议层定义 6 类 `report_type`：`personal_daily`、`personal_weekly`、`team_daily`、`team_weekly`、`department_daily`、`department_weekly`。
2. 实现分期：P0 做 `personal_daily`，P1 做 `personal_weekly`，P2 做小组日报 / 周报，P3 做部门日报 / 周报。
3. MCP tools 按动作拆分，不按报告类型硬拆：`get_report_context`、`write_report_result`、`write_report_failure`。
4. 前端只触发 `report_type + period`，不传 session / 日报 / 周报来源列表。
5. 服务端 / MCP 层根据当前登录态、角色、`team_id` 和部门关系判断范围和权限。
6. 回写落到现有 Aida 日报 / 周报记录，前端仍通过现有读取接口刷新。
7. PM 作为 `independent_user_report`，不属于任何 TL 小组。
8. P0 状态字段不新增强状态枚举，`product_status` 作为计算态输出。

## 阶段 C：P0 personal_daily Report MCP 实现计划

### 目标链路

```text
Agent 调用 get_report_context
  -> MCP 根据当前用户身份读取 personal_daily 上下文
  -> Agent 生成日报内容
  -> Agent 调用 write_report_result
  -> MCP 回写到现有个人日报记录
  -> 前端通过现有我的日报读取接口看到生成内容
```

### 最终 endpoint

1. 新增通用 MCP endpoint：`POST /api/v1/mcp/reports`。
2. 复用现有 `DailyReportMCPHandler` 的 JSON-RPC 骨架和鉴权逻辑。
3. 旧 `POST /api/v1/mcp/daily-report` 保留兼容。
4. `/mcp/reports` P0 只支持 `report_type=personal_daily`。

### Tools

| Tool | P0 行为 |
| --- | --- |
| `get_report_context` | 按当前登录用户和 `period.date` 自动读取个人日报上下文 |
| `write_report_result` | 将 Agent 生成结果写入现有 `daily_reports` |
| `write_report_failure` | 记录 Agent 运行失败，不修改日报正文 |

`get_report_context` 不接收 `session_ids`，不得恢复旧来源选择心智。
`get_report_context` 的 `run_id` 可选；如果传入则校验 `run_id` 是否存在且属于当前用户，如果未传入也允许读取 `personal_daily` 上下文。

### 字段复用方案

P0 不新增数据库迁移，复用现有字段：

| 冻结口径 | 复用字段 |
| --- | --- |
| `origin` | `daily_reports.generation_mode` |
| `updated_by_user` | `daily_reports.edited` |
| `agent_run_id` | `daily_reports.managed_agent_run_id` |
| `generated_at` | `ai_runs.finished_at` |
| `agent_id` | `daily_reports.agent_id` |
| `model_id` | `daily_reports.model_id` |

`agent_run_id` 作为 response 语义别名输出，底层不新增字段，不与 `managed_agent_run_id` 双写。

### product_status 计算规则

| 条件 | product_status |
| --- | --- |
| 无 `daily_reports` | `missing` |
| `generation_mode=managed_agent` 且 `edited=false` | `ai_generated` |
| `generation_mode=managed_agent` 且 `edited=true` | `modified` |
| `generation_mode` 非 `managed_agent` 或无 Agent run | `manual` |

P0 中 `manual` 表示“非本轮 managed_agent 生成来源”。旧 report-generator 生成的历史内容如果不属于 `generation_mode=managed_agent`，P0 也会归入 `manual`，不在本轮做历史数据重判。

P0 不强行把失败 run 合并成 `daily_reports` 的 `generation_failed`；失败先体现在 `ai_runs`。

### write_report_result 防覆盖规则

1. `write_report_result` 必须要求 `run_id`。
2. `run_id` 必须存在，并属于当前用户可用的 `ai_runs`。
3. 只允许当前用户写自己的 `personal_daily`。
4. 如果当日个人日报已存在，且 `edited=true`，且 `daily_reports.updated_at > ai_runs.created_at`，返回 `REPORT_EDIT_CONFLICT`。
5. conflict 时不覆盖日报正文，同时将 `ai_runs` 标记为 `failed`，写入 `error_message`，并设置 `finished_at`。
6. P0 不支持 force 覆盖。

### write_report_failure 行为

1. 必须要求 `run_id`。
2. 只更新 `ai_runs.status`、`error_message`、`finished_at`。
3. 不修改 `daily_reports` 正文。

### 涉及文件

| 文件 | 计划修改点 |
| --- | --- |
| `api/handler/daily_report_mcp.go` | 增加 `/mcp/reports` 下新 tools 分发与 P0 personal_daily 行为 |
| `api/main.go` | 注册 `POST /api/v1/mcp/reports`，保留旧 `/mcp/daily-report` |
| `api/model/models.go` | 补充 response 计算态字段或新增轻量 response struct |
| `api/handler/report.go` | 复用或抽取个人日报读取、session 读取、任务候选读取逻辑 |
| `api/handler/daily_report_mcp_test.go` | 补 P0 Report MCP 单元测试 |

### 测试计划

1. `tools/list` 包含 `get_report_context`、`write_report_result`、`write_report_failure`。
2. `/mcp/reports` 支持 `report_type=personal_daily`。
3. 非 `personal_daily` 返回 unsupported。
4. `get_report_context` 不传 `session_ids`，能自动读取当前用户上下文。
5. `write_report_result` 无报告时创建日报。
6. `write_report_result` 未编辑 AI 报告时可更新。
7. `write_report_result` 用户已编辑且 `updated_at > run.created_at` 时返回 `REPORT_EDIT_CONFLICT`，不覆盖正文。
8. conflict 时 `ai_runs` 标记 failed。
9. `write_report_failure` 只更新 `ai_runs`，不改日报正文。
10. 旧 `/mcp/daily-report` 兼容测试不破坏。

### 本阶段不做

1. 不做 personal_weekly。
2. 不做 team_daily / team_weekly。
3. 不做 department_daily / department_weekly。
4. 不做 PM 独立个人来源。
5. 不做部门范围。
6. 不做 Admin 特殊能力。
7. 不做定时生成。
8. 不接日报 / 周报前端智能生成按钮。
9. 不改 AI Assets 页面。
10. 不新增数据库迁移。
11. 不新增旧来源型生成逻辑。
12. 不传 `session_ids`。

## 阶段 D0：默认 personal_daily Agent 配置与修复机制

目标：为 Agent 模块提供默认 personal_daily 日报 Agent 能力。普通用户不需要先进入 `/ai-assets` 理解和手动配置 Agent、MCP、模型或凭据。

### 已确认策略

1. 默认日报 Agent 采用“每用户默认 Agent”，不使用系统共享 Agent。
2. 默认 Agent 用途通过稳定标记识别：
   - `AIDA_REPORT_AGENT:personal_daily`
   - `AIDA_MANAGED_DEFAULT_AGENT:true`
3. 优先使用 `description` 标记；平台字段不足时允许在 `instructions` 中保留同样标记。
4. 不只依赖 `name == "日报"` 识别 Agent；名称只作为绑定目标 Report MCP 后的降级候选条件。

### 默认配置项

| 配置项 | 默认值 | 作用 |
| --- | --- | --- |
| `MANAGED_AGENT_DEFAULT_ENGINE` | `claude-code` | 默认日报 Agent engine |
| `MANAGED_AGENT_DEFAULT_MODEL_ID` | `MiniMax-M2.5` | 默认日报 Agent `default_model_id` |
| `MANAGED_AGENT_REPORT_MCP_SLUG` | `aida-report-mcp-p0` | Report MCP Registry slug |
| `MANAGED_AGENT_REPORT_MCP_VERSION` | `personal-daily-v1` | Report MCP Registry version |
| `AIDA_PUBLIC_BASE_URL` | 空 | Managed Agent 第三方服务可访问的 Aida 外部地址 |

`engine` 和 `model` 只从配置读取，业务逻辑不硬编码具体模型。`AIDA_PUBLIC_BASE_URL` 缺失时，默认 Agent 运行链路返回明确配置错误。

### 默认 Agent 查找 / 创建 / 修复

1. 读取当前用户在 Managed Agent 平台上的 `my agents`。
2. 优先选择带稳定标记的 `personal_daily` 默认 Agent。
3. 如果没有稳定标记，再选择 `name == "日报"` 且已绑定目标 Report MCP 的候选 Agent。
4. 如果仍没有，则自动创建默认“日报”Agent。
5. 如果找到 Agent 但配置不完整，则只修复一个最合适的候选，不重复创建多个“日报”Agent。

允许自动修复：

1. 缺 `default_model_id`：补为 `MANAGED_AGENT_DEFAULT_MODEL_ID`。
2. 缺目标 Report MCP binding：追加 configured slug/version binding。
3. `instructions` 为空：写入默认 personal_daily instructions。
4. 旧默认 instructions：替换为当前默认 instructions。

不强制覆盖：

1. 用户明显自定义过的 instructions。
2. 用户自定义 engine。
3. 用户自定义模型，除非 `default_model_id` 为空。

### Report MCP Registry

1. 默认链路先通过 `ListMCPEntries(scope=mine)` 查找 configured slug/version。
2. 不存在时调用 `CreateMCPEntry` 创建，URL 为：
   `AIDA_PUBLIC_BASE_URL + "/api/v1/mcp/reports"`。
3. transport 使用当前平台可用的 `http`。
4. 如果同 slug/version 已存在但 URL 与配置目标明显不一致，D0 不自动覆盖，返回可诊断配置错误。

### 当前用户 token 透传

1. `mcp_authorization` 来自当前请求 `Authorization: Bearer ...`。
2. 该 token 语义是当前 Aida 登录用户身份，不使用平台管理员 token 或固定全局 token。
3. Agent 调用 `/api/v1/mcp/reports` 时必须经由 Aida AuthMiddleware 还原当前用户。
4. P0/D0 暂不实现短期 scoped MCP token；后续可迁移到 credential slot，但语义仍必须是用户级身份。
5. 日志和测试报告不得输出完整 token。

### personal_daily run 参数

默认 Agent 运行时由后端注入：

```json
{
  "run_id": "...",
  "report_type": "personal_daily",
  "period.date": "YYYY-MM-DD",
  "report_date": "YYYY-MM-DD",
  "mcp_url": "https://aida.example.com/api/v1/mcp/reports",
  "mcp_authorization": "Bearer 当前用户 token"
}
```

说明：

1. `period.date` 保留为业务协议字段。
2. `report_date` 作为当前 Managed Agent 兼容层的扁平模板参数，避免 prompt 模板无法解析带点号的 key。
3. 不传 `session_ids`、来源列表、`model_id`。
4. `agent_id` 由后端默认 Agent ensure 逻辑得到，不要求普通用户传入。

### 涉及文件

| 文件 | 修改点 |
| --- | --- |
| `api/config/config.go` | 增加默认 engine/model、Report MCP slug/version、`AIDA_PUBLIC_BASE_URL` 配置 |
| `api/handler/managed_agent.go` | 增加默认 personal_daily Agent 查找 / 创建 / 修复、Report MCP Registry ensure、默认 run 入口 |
| `api/main.go` | 注入默认配置并注册默认 personal_daily run endpoint |
| `docker-compose.yml` | 补充 Managed Agent 默认配置环境变量示例 |
| `api/handler/managed_agent_test.go` | 增加默认 Agent 与 run 参数测试 |

### 测试覆盖

1. Managed Agent 平台未配置时返回 `MANAGED_AGENT_NOT_CONFIGURED`。
2. 缺 `AIDA_PUBLIC_BASE_URL` 时返回配置错误。
3. 用户无日报 Agent 时自动创建默认“日报”Agent。
4. 已有完整默认日报 Agent 时直接复用，不创建第二个。
5. 已有默认日报 Agent 缺 `default_model_id`、Report MCP binding 或空 instructions 时自动修复。
6. 用户自定义 instructions 不强制覆盖。
7. 多个候选 Agent 时选择配置最完整的候选。
8. Agent run 不传 `model_id`，依赖 Agent `default_model_id`。
9. run params 不包含 `session_ids` 或来源列表。
10. `mcp_authorization` 使用当前用户 token。

### 本阶段不做

1. 不接日报 / 周报前端智能生成按钮。
2. 不做 personal_weekly。
3. 不做 team_daily / team_weekly。
4. 不做 department_daily / department_weekly。
5. 不做 PM 独立个人来源。
6. 不做定时生成。
7. 不新增数据库迁移。
8. 不恢复 `session_ids` 来源选择。
9. 不包装旧 report-generator。

## 阶段 E：产品边界修正

### 最新产品边界

1. 日报 / 周报模块只负责“报告产物”的查看、编辑、保存。
2. AI Assets / Agent 模块负责报告生成，包括手动运行 Agent 或后续通过定时任务运行 Agent。
3. Report MCP 仍作为 Agent 读写 Aida 报告数据的工具接口。
4. Agent 通过 Report MCP 写回 `daily_reports` 等报告事实源后，日报 / 周报页面只读取已写回的报告内容。
5. 日报 / 周报页面不直接触发 Agent run，不展示 Agent、MCP、model、token、credential、run 状态。

### 本轮调整结论

1. “我的日报”弹窗移除“智能生成”“重新生成”“生成中”和 run 轮询，只保留正文编辑、保存和报告状态展示。
2. 无日报内容时展示普通空态：“暂无日报，可直接填写。”
3. 前端不再调用 `POST /api/v1/reports/today/default-managed-agent-runs`。
4. `default-managed-agent-runs` 是前一版 personal_daily E2E 联调用入口，不作为最终日报页面产品入口，本轮已删除后端路由和 handler。
5. 后续 Agent 生成入口应迁移到 AI Assets / Agent 模块或定时任务模块。
6. `/api/v1/mcp/reports`、`get_report_context`、`write_report_result`、`write_report_failure`、默认 Agent 查找 / 创建 / 修复机制继续保留。

### 6 类报告弹窗统一口径

1. `personal_daily`、`personal_weekly`、`team_daily`、`team_weekly`、`department_daily`、`department_weekly` 均按“报告内容编辑弹窗”处理。
2. 弹窗只读取已有报告、展示状态、编辑正文、保存正文。
3. 无内容时直接展示空编辑区和“暂无报告，可直接填写。”文案。
4. 弹窗不展示智能生成、重新生成、生成中、Agent、MCP、model、run、credential、session 来源选择。
5. Agent 通过 MCP 写回的报告内容仍通过现有读取接口展示，用户可继续编辑保存。

### 本阶段不做

1. 不接日报 / 周报前端智能生成按钮。
2. 不做 personal_weekly。
3. 不做 team_daily / team_weekly。
4. 不做 department_daily / department_weekly。
5. 不做 PM 独立个人来源。
6. 不做定时任务。
7. 不新增数据库迁移。
8. 不恢复 `session_ids` 来源选择。

## 阶段 F：默认 Report 配置初始化口径修正

本节修正并覆盖阶段 D0 中“默认 personal_daily Agent ensure”的旧口径。

### 最终口径

1. 默认配置不是系统模板资产，也不是只读官方模板。
2. 默认配置是用户自己的普通 AI Assets 个人配置，只是在账号生效时由系统自动创建。
3. 默认配置只包含一套：
   - `Aida Report Skill`
   - `Aida Report MCP`
   - `报告生成 Agent`
4. 不再默认创建 6 个 Agent。
5. 不在 AI Assets 页面加载或列表接口中自动创建默认配置。
6. 用户删除默认配置后，刷新页面不会自动恢复。
7. 存量账号通过显式 backfill 补齐。

### 默认资产

1. Skill：`aida-report@1.0.0`，普通用户 Skill，包含 Report MCP 原子工具说明。
2. MCP：`aida-report-mcp@report-v1`，endpoint 为 `/api/v1/mcp/reports`，只声明 `AIDA_REPORT_MCP_AUTH` credential slot，不保存 token。
3. Agent：`报告生成 Agent`，绑定当前用户自己的 Skill / MCP，标记为：
   - `AIDA_REPORT_AGENT:default`
   - `AIDA_REPORT_AGENT_TYPES:personal_daily,personal_weekly,team_daily,team_weekly,department_daily,department_weekly`
   - `AIDA_MANAGED_DEFAULT_AGENT:true`

### 初始化时机

1. admin 创建用户且用户可登录时初始化。
2. 用户从 disabled 变为 active 时初始化。
3. bootstrap admin 首次创建时初始化。
4. 存量账号通过 `POST /api/v1/admin/ai-assets/default-report-assets/backfill` 显式补齐。

### Run 边界

1. Report Agent run 使用 AI Assets / Agent 模块入口。
2. run 接口只消费已有的用户个人 Agent，不再隐藏创建默认资产。
3. token 只通过运行时 credential override 注入，不进入前端参数、Agent config、MCP config、`input_ref_json` 或日志。
