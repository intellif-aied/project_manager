# AI Assets 整体交互改造方案

## 1. 背景和目标

当前 Aida 已完成 personal_daily Agent 化闭环：

- Report MCP 已实现 `/api/v1/mcp/reports`；
- MCP tools 已包含 `get_report_context`、`write_report_result`、`write_report_failure`；
- personal_daily Agent E2E 已真实启动 Managed Agent；
- Agent 已真实调用 `get_report_context` 和 `write_report_result`；
- `daily_reports` 已写入，现有日报接口可回读；
- `ai_runs.status=succeeded`；
- 默认 personal_daily Agent 已支持查找 / 创建 / 修复；
- Report MCP binding 和 `AIDA_REPORT_MCP_AUTH` credential slot 已接入。

但产品边界已经调整：日报 / 周报报告弹窗不再直接触发 Agent run，只负责报告产物的查看、编辑、保存。Agent 生成入口应放在 AI Assets / Agent / 定时任务模块，MCP 只是 Agent 读写 Aida 报告数据的工具通道。

因此本次不是只改“日报 Agent 运行页”，也不是只删某个接口，而是要收敛整个 AI Assets 页面交互：

- AI Assets 整体页面结构向第三方 Managed Agent 平台靠拢；
- 普通 Agent 保留通用运行能力；
- Aida Report Agent 使用业务化运行页；
- 报告弹窗和 AI Assets 的产品职责分离；
- token、credential、MCP 内部参数不暴露给普通用户。

## 2. 第三方平台参考

第三方 Managed Agent 平台代码位置：

```text
/home/intellif/dev/sandboxed-agent-platform
```

重点参考文件：

- `frontend/src/views/AgentsView.jsx`
- `frontend/src/views/AgentDetailPage.jsx`
- `frontend/src/components/AgentEditor.jsx`
- `frontend/src/views/RunView.jsx`
- `frontend/src/views/ResourcesView.jsx`
- `frontend/src/views/MCPPanel.jsx`
- `frontend/src/views/SkillsPanel.jsx`
- `frontend/src/views/HistoryView.jsx`
- `frontend/src/views/ResultView.jsx`
- `frontend/src/utils/schema.js`
- `frontend/src/utils/promptTemplate.js`
- `frontend/src/utils/agentResources.js`

### Agent list

第三方平台的 AgentsView 是统一 Agent catalog：

- 支持“全部 / 我的”切换；
- “我的”可显示归档 Agent；
- Agent 以卡片方式展示；
- 卡片只负责打开 Agent 详情，不在卡片上堆叠运行、编辑、归档等复杂操作；
- 新建 Agent 从列表页进入独立创建流程。

### Agent create / edit

第三方平台的 AgentEditor 采用 owner-model：

- 基础字段：Agent ID、名称、描述；
- 运行字段：Engine、默认模型、超时；
- Prompt 字段：Instructions、Start Prompt 模板；
- Model API Key 共享能力；
- Skill picker；
- MCP picker；
- Subagent picker；
- Credentialed MCP 会自动声明 credential slot，可选默认 credential binding；
- 编辑时保留本地 draft，取消时有明确放弃草稿确认；
- 配置变更会生成新版本，运行中的 Session 不受影响。

### Agent run

第三方平台 RunView 是独立运行页，绑定单个 Agent：

- 左侧展示 Agent 信息：名称、说明、默认模型、运行方式、重跑来源；
- 右侧展示运行表单；
- Managed Agent 使用 `start_prompt_template` 提取 `{{ variable }}`，渲染 Start Prompt Values；
- 无 Start Prompt Template 时展示 Initial Message；
- 非 Managed Agent 使用 `input_schema` / `env` 渲染 Params / Env；
- `env` 中敏感字段按 password 样式显示；
- credential slots 渲染为“使用默认绑定 / 选择凭据”，不展示明文 token；
- model_id 是可选覆盖项，默认使用 Agent 默认模型；
- 文件上传单独分区；
- 提交后进入任务 / Session 结果链路。

### MCP / Skill / Credential binding

第三方平台的 ResourcesView 把 Skill Registry 和 MCP Registry 放在统一资源库：

- Skills 按 owner / slug / version 组织；
- MCP Server 按 slug / version 组织；
- MCP 支持 HTTP / stdio；
- MCP 可声明是否需要凭据；
- 需要凭据的 MCP 在绑定到 Agent 时声明 credential slot；
- credential 只以绑定选择出现，不回显明文。

### run history / Session result

第三方平台将运行历史和结果详情独立展示：

- 运行页负责提交；
- History / Result 负责查看状态、结果、文件、日志；
- 结果展示和参数配置分离；
- 运行事实可审计，但用户不需要在提交表单中理解底层运行 ID。

### 对 Aida 的参考结论

Aida 应对齐第三方平台的页面结构和字段组织：

- Agent 列表只做发现与进入详情；
- Agent 详情承载运行、编辑、归档；
- Agent 创建 / 编辑按基础信息、模型、Prompt、Skill、MCP、Credential 分区；
- 普通 Agent 运行页按 Agent 输入契约渲染；
- 运行历史和结果独立展示；
- credential 不展示明文。

但 Aida 不能直接照搬：

- Report Agent 不应把所有 Start Prompt 变量渲染为用户输入；
- Report Agent 不应展示 `run_id / mcp_url / mcp_authorization / credential slot`；
- Report Agent 是业务场景，运行页应展示报告日期、报告类型等业务字段。

## 3. 当前 Aida AI Assets 现状和问题

### 首页 / 列表页

当前 Aida `/ai-assets` 页面把 Skills、MCP、Agents、运行记录、定时任务放在同一个页面中，通过 tab 和 modal 管理。

问题：

- 页面更像“代理第三方平台 API 的管理台”，不像一套完整 Agent 产品；
- Agent 列表中的操作直接打开运行 modal，缺少详情页层级；
- Skills / MCP / Agent / Runs / Schedules 都在同一页里，信息密度高；
- 普通用户和高级用户的入口没有分层。

### Agent 创建

当前 Aida 创建 Agent modal 支持：

- name；
- engine；
- default_model_id；
- start_prompt_template；
- description；
- instructions；
- skills；
- mcp_bindings。

问题：

- Engine 是自由输入，不像第三方平台用枚举；
- 默认模型只是普通输入；
- Skill / MCP 绑定是 Select，多数上下文信息不足；
- credential slots / default bindings 没有完整 UI；
- 没有版本语义展示；
- 没有 draft / 取消保护。

### Agent 编辑

当前编辑 Agent 复用创建 modal。

问题：

- 缺少详情页；
- 缺少配置分区；
- 缺少 MCP credential binding 细节；
- 缺少“配置变更生成新版本”的用户心智；
- Report Agent 的稳定标记只隐含在 description / instructions 中，页面没有业务化识别。

### Agent 运行

当前 Aida 运行 modal 提供：

- model_id 输入；
- Start Prompt 模板原文展示；
- 启动参数 key=value；
- 可选补充指令；
- 运行状态；
- 运行结果。

问题：

- 运行页偏底层参数调试器；
- Report Agent 的系统参数可能被暴露或要求用户填写；
- `report_type` 可能成为自由文本；正确形态应是枚举选择；
- `period.date` 和 `report_date` 容易同时出现；
- `run_id`、`mcp_url`、`mcp_authorization` 不应让用户理解；
- 不区分普通 Agent 和 Report Agent；
- model override 应与第三方平台一致保留为可选覆盖项，可放入高级设置，但不应禁止。

### 运行历史

当前 Aida 运行记录基于 `ai_runs` 展示，支持轮询单个 run。

问题：

- 运行历史和运行页绑定较弱；
- Report Agent 成功后缺少“去我的日报查看”的业务结果引导；
- input_ref/output_ref 的技术字段需要脱敏和分层展示；
- 对于 failed 的 Report Agent，普通用户不应看到 token、MCP、credential 等技术词。

### MCP / Skill / 定时任务入口

当前 Aida 已有 Skills、MCP、定时任务入口。

问题：

- 与第三方平台资源库的组织方式不一致；
- MCP credential 语义没有完整呈现；
- 定时任务作为 Agent 生成入口应保留，但需要复用 Report Agent 的业务参数收敛，不能让用户填写 `mcp_authorization` 等系统参数。

### Report Agent 系统参数暴露问题

错误暴露字段包括：

- `run_id`
- `report_type` 自由文本输入
- `period.date`
- `mcp_url`
- `mcp_authorization`
- credential slot
- token
- MCP internal config
- START PROMPT VALUES 原始变量

这些都必须从 Report Agent 运行页移除。

## 4. 整体改造原则

1. 页面结构向第三方 Managed Agent 平台靠拢。
2. 普通 Agent 保持通用自由度，作为高级用户能力。
3. Report Agent 做业务化收敛，只暴露业务参数。
4. 报告弹窗不触发 Agent。
5. AI Assets / Agent / 定时任务是 Agent 运行入口。
6. MCP 是 Agent 读写 Aida 数据的通道，不是页面按钮逻辑。
7. token / credential / MCP 内部细节不暴露给用户。
8. Report Agent 系统参数由 Aida 后端接管。
9. 固定的是系统参数，业务自由度保留给用户：运行页可选择 `report_type` 枚举、日期 / 周期、model override、补充说明；Agent 编辑页可调整 `default_model_id`、instructions、start_prompt_template。
10. 不为统一前端而 mock 真实能力；没有接口就明确不可用或后续阶段实现。

## 5. AI Assets 首页 / 列表页方案

建议将 AI Assets 从“一个大 tab 页面”逐步改造成更接近第三方平台的信息架构：

```text
AI Assets
├── Agents
│   ├── 全部 Agent
│   ├── 我的 Agent
│   ├── Agent 详情
│   ├── 新建 Agent
│   └── 编辑 Agent
├── Resources
│   ├── Skills
│   └── MCP Servers
├── Runs
│   ├── 运行历史
│   └── 运行结果详情
└── Schedules
    ├── 定时任务列表
    └── 新建 / 编辑定时任务
```

短期不强制大规模路由重构，先在当前 `AIAssetsPage` 内实现“列表 → 详情 → 运行 / 编辑 / 历史”的页面结构，优先改用户可见交互，不因为路由重构拖慢进度。后续如果代码复杂度上升，再拆成真实路由：

- `/ai-assets/agents`
- `/ai-assets/agents/:id`
- `/ai-assets/agents/:id/run`
- `/ai-assets/runs/:id`

Agents 列表建议：

- 卡片或表格展示 Agent；
- 支持“我的 / 全部”；
- 支持归档筛选；
- 主操作为“打开详情”；
- 不在列表行里直接堆叠复杂运行参数；
- Report Agent 可显示业务标签，例如“个人日报 Agent”。

Skills / MCP 建议：

- 作为 Resources 区块；
- 按 slug / version / owner 组织；
- MCP 显示 transport、目标、是否需要凭据；
- 不展示明文 credential；
- 和 Agent 创建 / 编辑页中的 picker 保持一致。

Runs 建议：

- 独立运行历史；
- 支持按 Agent、状态、业务类型筛选；
- Report Agent run 成功后显示“已写回日报”等业务结果；
- 详情页可查看技术信息，但敏感字段必须脱敏。

Schedules 建议：

- 保留为 AI Assets 下的 Agent 自动运行入口；
- Report Agent 定时任务也应使用业务化参数，例如“每天 19:00 生成个人日报”，而不是让用户填 MCP 参数。

## 6. Agent 创建页方案

Agent 创建页应向第三方平台 AgentEditor 对齐。

字段分区：

### 基础信息

- Agent ID：新建时可选或系统生成；编辑后不可改；
- name；
- description；
- archived 状态只在详情 / 编辑中呈现。

### 运行配置

- engine：枚举，例如 `claude-code`、`codex`；
- default_model_id；
- timeout，如当前平台支持；
- model override 不在这里处理，运行页只做可选覆盖。

### Prompt 配置

- instructions；
- start_prompt_template；
- 明确提示：普通 Agent 的 `{{ variable }}` 会在运行页生成输入框；
- Report Agent 的系统变量不应依赖用户填写，默认 Agent 模板应避免暴露系统变量为必填项。

### Resources

- skills；
- mcp_bindings；
- credential slots；
- default credential bindings；
- subagents 如后续需要再引入。

### 版本语义

- 显示当前 version / managed_version；
- 配置变更生成新版本；
- 运行中的 session / task 不受影响。

Report Agent 创建 / 修复注意：

- 默认 personal_daily Agent 由 Aida 后端创建 / 修复；
- 稳定标记写入 description 或 metadata；
- UI 可展示“默认个人日报 Agent”标签；
- 不要求普通用户手动理解 MCP binding 和 credential slot。

## 7. Agent 编辑页方案

编辑页应从 modal 逐步升级为详情页 / 编辑页：

- 详情页展示基础信息、默认模型、运行方式、版本、Skills、MCP、Credential 绑定；
- 编辑页按创建页同样分区；
- 支持继续编辑草稿；
- 取消编辑时按项目已有交互处理未保存修改；
- 归档 / 恢复在详情页处理；
- 对非本人 Agent 只展示可运行信息，不展示完整配置。

Report Agent 编辑页可展示：

- 类型：个人日报 Agent；
- 默认模型；
- instructions；
- start_prompt_template；
- Report MCP binding；
- credential slot 状态。

但普通用户不应在运行页看到这些系统字段。高级用户可在编辑页查看和调整 Agent 配置。

## 8. 普通 Agent 运行页方案

普通 Agent 保留通用运行自由度，并向第三方平台 RunView 靠拢。

建议结构：

- 左侧 / 顶部 Agent 信息：
  - 名称；
  - 描述；
  - 默认模型；
  - 运行方式；
  - 版本；
- 表单区：
  - Start Prompt Values；
  - Initial Message；
  - Params；
  - Env；
  - Files；
  - model_id 可选覆盖；
  - additional instruction；
  - credentials 选择；
- 状态区：
  - 未提交；
  - pending；
  - running；
  - succeeded；
  - failed；
- 结果区：
  - 运行结果；
  - 输出文件；
  - 错误摘要；
  - 跳转结果详情。

普通 Agent 可以保留 key=value 高级参数，但建议：

- 默认根据 schema / start prompt 渲染表单；
- key=value 放入“高级参数”折叠区；
- 敏感 key 使用 password 或脱敏；
- 不保存完整 token 到 `input_ref_json`。
- model override 与第三方平台保持一致，默认使用 Agent `default_model_id`，用户可按需切换模型。

## 9. Report Agent 业务化运行页方案

### 识别方式

通过稳定标记识别 Report Agent：

```text
AIDA_REPORT_AGENT:personal_daily
AIDA_REPORT_AGENT:personal_weekly
AIDA_REPORT_AGENT:team_daily
AIDA_REPORT_AGENT:team_weekly
AIDA_REPORT_AGENT:department_daily
AIDA_REPORT_AGENT:department_weekly
AIDA_MANAGED_DEFAULT_AGENT:true
```

P0 当前已验证的能力是：

```text
AIDA_REPORT_AGENT:personal_daily
```

Report Agent 的 `report_type` 是业务参数，应以枚举选择呈现。P0 可以默认选中 `personal_daily`，如果当前 Agent 只支持 personal_daily，则下拉里只有“个人日报”，也可以表现为单选项或只读态。

### P0 personal_daily 页面

```text
运行 日报

Agent 信息：
- 名称：日报
- 默认模型：MiniMax-M2.5
- 类型：个人日报 Agent

表单：
- 报告类型：[个人日报 ▼]
- 报告日期：[日期选择器，默认今天]
- 模型：默认 MiniMax-M2.5，可在高级设置中切换

按钮：
- 运行

结果：
- 运行中
- 已完成：日报已生成，可在我的日报中查看
- 失败：展示普通错误摘要
```

### 用户可见字段

- `report_type` 枚举；
- `report_date`
- `model_id` 可选覆盖；
- 后续可选 `additional_instruction`

### 隐藏字段

- `run_id`
- `period.date`
- `mcp_url`
- `mcp_authorization`
- credential slot；
- token；
- MCP 技术词；
- START PROMPT VALUES；
- 通用 key=value params。

### 固定配置与自由度边界

系统固定 / 自动注入的是：

- `run_id`；
- `mcp_url`；
- `mcp_authorization`；
- credential slot；
- token；
- period 内部结构；
- MCP internal config。

用户仍然可以配置或选择的是：

- `report_type` 枚举；
- `report_date / week_start / week_end`；
- model override；
- `additional_instruction`；
- Agent 编辑页里的 `default_model_id`；
- Agent 编辑页里的 `instructions`；
- Agent 编辑页里的 `start_prompt_template`。

### 后端注入字段

```json
{
  "run_id": "Aida 自动创建的 ai_run_id",
  "report_type": "personal_daily",
  "period": {
    "date": "由 report_date 映射"
  },
  "report_date": "用户选择日期",
  "mcp_url": "AIDA_PUBLIC_BASE_URL + /api/v1/mcp/reports",
  "mcp_authorization_source": "AIDA_REPORT_MCP_AUTH"
}
```

### report_type 处理

`report_type` 是业务参数，不是系统参数。交互口径：

- 不允许自由文本输入；
- 必须是枚举下拉；
- 可选项来自 Agent 支持的报告类型 + 当前用户角色权限；
- P0 可默认选中 `personal_daily`；
- 如果当前 Agent 只支持 `personal_daily`，下拉里只有“个人日报”，可以表现为单选项或只读态；
- 如果未来 Agent 支持多个 report_type，用户可以在下拉中选择；
- 后端必须根据 Agent 标记和当前用户权限再次校验，不能只依赖前端隐藏。

支持的枚举包括：

- `personal_daily`：个人日报；
- `personal_weekly`：个人周报；
- `team_daily`：小组日报；
- `team_weekly`：小组周报；
- `department_daily`：部门日报；
- `department_weekly`：部门周报。

不同 report_type 的字段展示：

- 日报显示 `report_date`；
- 周报显示 `week_start / week_end`，或先提供“本周”选择并由系统推导；
- team / department 范围由服务端根据身份推导，不让用户手填 `team_id / department_id`。

### token 避免暴露

- 前端请求不传 token；
- 前端不传 `mcp_authorization`；
- 后端从当前登录态生成或选择用户级凭据；
- 通过 `AIDA_REPORT_MCP_AUTH` credential slot / credential overrides 注入；
- `input_ref_json` 只记录脱敏信息；
- 日志不得打印完整 token。

## 10. 运行历史 / 结果展示方案

运行历史建议从 AI Assets 中独立出来：

- 列表展示 agent name、business_type、status、model、created_at、finished_at；
- Report Agent run 增加业务列，例如 report_type、report_date；
- 成功时显示“日报已生成”；
- 失败时显示错误摘要；
- 支持打开详情。

Report Agent 结果详情：

- succeeded：展示“日报已生成，可在我的日报中查看”；
- 提供“打开我的日报”入口；
- failed：展示普通错误摘要；
- 高级详情可折叠展示 run id、external task/session id、错误码；
- `mcp_authorization`、token、credential secret 永不展示。

普通 Agent 结果详情：

- 保留原始结果；
- 保留文件 / 日志入口；
- 保留参数摘要，但敏感字段脱敏。

## 11. 报告弹窗边界

6 类报告入口：

- 个人日报；
- 个人周报；
- 小组日报；
- 小组周报；
- 部门日报；
- 部门周报。

报告弹窗统一职责：

```text
打开弹窗
→ 查看报告内容
→ 编辑报告内容
→ 保存
→ 展示状态
```

报告弹窗不出现：

- 智能生成；
- 重新生成；
- 生成中；
- Agent；
- MCP；
- model；
- run；
- token；
- credential；
- 选择 session；
- 选择来源；
- skill 生成。

Agent 通过 Report MCP 写回报告后，报告弹窗只负责通过现有读取接口展示内容，并允许用户编辑保存。

## 12. 接口调整方案

### 删除旧 reports 触发接口

旧接口：

```http
POST /api/v1/reports/today/default-managed-agent-runs
```

该接口边界不合理，因为它把“报告模块触发 Agent 生成”固化到了 reports 模块里。

方案：

- 删除该接口；
- 如果当前代码中仍有 route / handler / client / type / test，应后续迁移或删除；
- 不作为正式能力保留；
- 报告页面不再调用 Agent run。

### 新增 AI Assets 下的 Report Agent run 接口

推荐新接口：

```http
POST /api/v1/ai-assets/report-agents/{agentId}/runs
```

P0 请求体：

```json
{
  "report_type": "personal_daily",
  "report_date": "YYYY-MM-DD",
  "model_id": "可选模型覆盖"
}
```

前端不传：

- token；
- `mcp_authorization`；
- `mcp_url`；
- `run_id`；
- `period.date`；
- `session_ids`；
- 来源列表。

前端可传：

- `report_type` 枚举值；
- `report_date`，或周报阶段的 `week_start / week_end`；
- `model_id` 可选覆盖；
- `additional_instruction` 可选补充说明。

后端自动注入：

```json
{
  "run_id": "Aida 自动创建的 ai_run_id",
  "report_type": "personal_daily",
  "period": {
    "date": "report_date"
  },
  "report_date": "用户选择日期",
  "mcp_url": "AIDA_PUBLIC_BASE_URL + /api/v1/mcp/reports",
  "mcp_authorization_source": "AIDA_REPORT_MCP_AUTH"
}
```

后端要求：

- 创建本地 `ai_runs`；
- 校验 Agent 支持所选 report_type；
- 校验当前用户角色权限允许运行所选 report_type；
- 使用当前用户身份；
- 通过 credential slot / credential overrides 注入 Report MCP 鉴权；
- `input_ref_json` 不保存明文 token；
- 返回 `ai_run_id / agent_id / status / model_id / external_task_id 或 external_session_id`；
- 运行失败时写入 `ai_runs.error_message`，不污染报告正文。

### 与第三方 API 的对齐

Report Agent 正式运行链路必须使用第三方 `/api/session`，不要继续依赖 `/api/task/submit`。

原因：

- Managed Agent 平台不会自动把 prompt / run params 里的 `mcp_authorization` 转成 HTTP Authorization header；
- Report MCP 鉴权必须依赖 credential slot / credential_overrides；
- `AIDA_REPORT_MCP_AUTH` 应作为 Report MCP 的用户级凭据槽；
- 继续使用 `/api/task/submit` 容易把 token 退化成普通 params，破坏鉴权边界。

Report Agent 调用第三方 `/api/session` 时：

- 使用 `start_prompt_values`；
- 使用 `credential_overrides`；
- 让 `AIDA_REPORT_MCP_AUTH` 成为真正 credential slot；
- 避免把 token 放进普通 params。
- `run_id / report_type / period / mcp_url` 等系统和业务参数由 Aida 后端组织后传入 session；
- `mcp_authorization` 不作为普通 params 传递。

普通 Agent 如果当前仍走 `/api/task/submit`，可以暂时保持现状，不阻塞 Report Agent 走 `/api/session`。

## 13. 分阶段实施计划

### 阶段 1：文档与边界收口

- 完成本方案；
- 删除旧窄范围文档；
- 明确报告弹窗与 AI Assets 边界；
- 冻结“报告页面不触发 Agent run”的产品口径。

### 阶段 2：AI Assets 页面结构对齐

- 首页 / 列表 / 运行历史 / 普通 Agent 基础交互向第三方平台靠拢；
- Agent 列表主操作改为打开详情；
- 短期在当前 `AIAssetsPage` 内实现“列表 → 详情 → 运行 / 编辑 / 历史”，不强制先拆路由；
- 创建 / 编辑 Agent 分区；
- Skill / MCP 资源库组织对齐；
- 普通 Agent 运行页保留自由度。

### 阶段 3：Report Agent 业务化运行页

- Report Agent 运行页展示 report_type 枚举、日期 / 周期、model override、可选补充说明；
- P0 默认选中 personal_daily，当前 Agent 只支持 personal_daily 时只展示一个可选项；
- 新增 AI Assets 下的 Report Agent run 接口；
- Report Agent run 后端正式走第三方 `/api/session`；
- 使用 `AIDA_REPORT_MCP_AUTH` credential slot / credential_overrides 注入当前用户级鉴权；
- 删除 reports 下旧 run 接口；
- 前端不传 token / MCP 参数；
- E2E 验证 Agent 写回日报。

### 阶段 4：报告弹窗统一为编辑器

- 6 类报告弹窗统一查看 / 编辑 / 保存；
- 移除所有生成入口；
- 保留 Agent 写回内容的读取展示；
- 保留手写保存。

### 阶段 5：再继续 personal_weekly / team / department

- 边界稳定后再扩展 Report MCP；
- 扩展默认 Report Agent；
- 扩展 Report Agent 业务化运行页；
- 再考虑定时生成与汇总报告。

## 14. 测试计划

1. 普通 Agent 运行页仍可用。
2. 普通 Agent 可继续传 message / params / files。
3. 普通 Agent credential 不展示明文 token。
4. Report Agent 运行页不展示 `run_id`。
5. Report Agent 运行页不展示 `mcp_url`。
6. Report Agent 运行页不展示 `mcp_authorization`。
7. Report Agent 运行页不展示 credential slot / token / MCP internal config。
8. P0 personal_daily 默认选中“个人日报”，只需选择报告日期即可运行。
9. `report_type` 是枚举选择，不是自由文本。
10. 前端请求不包含 token。
11. 前端请求不包含 `mcp_authorization / mcp_url / run_id / period.date / session_ids`。
12. 后端自动创建 `ai_run`。
13. Report Agent run 通过第三方 `/api/session` 创建。
14. `AIDA_REPORT_MCP_AUTH` 通过 credential overrides 注入。
15. Agent 成功调用 Aida Report MCP。
16. `daily_reports` 写入成功。
17. 日报弹窗可以查看 Agent 写回结果。
18. `default-managed-agent-runs` 不再存在。
19. 6 类报告弹窗不出现 Agent 生成入口。
20. 不影响 `/api/v1/mcp/reports` tools。
21. 不影响 AI Assets 现有 Agent 列表、MCP、Skill、定时任务基础能力。
22. A 用户运行 Report Agent 不能读写 B 用户 personal_daily。

## 15. 风险和人工确认问题

风险：

- Aida 当前 AI Assets 页面是单页 tab + modal 结构，短期在当前页面内模拟详情结构时，要控制组件复杂度，避免形成更大的单文件。
- Report Agent 正式切到 `/api/session` 后，需要确认第三方平台返回字段与当前 `ai_runs` 状态同步逻辑兼容。
- 默认 personal_daily Agent 的 start prompt template 仍包含系统变量说明，后续要避免这些变量被运行页渲染成用户输入。
- 定时任务如果继续使用通用 params，也可能再次暴露系统参数，需要和 Report Agent 运行页同口径收敛。

需要人工确认：

1. 是否接受新增 `POST /api/v1/ai-assets/report-agents/{agentId}/runs` 作为正式 AI Assets 入口。建议接受。
2. 普通 Agent 是否继续保留高级 key=value 参数。建议保留，但折叠并脱敏。
3. Report Agent 的 model override 放在主表单还是高级设置。建议放在高级设置。
4. `additional_instruction` 是否 P0 就实现。建议 P0 可先预留，不阻塞 report_date + report_type 闭环。
5. AI Assets 真实路由拆分的触发条件。建议短期不强制拆路由，等当前页面组件复杂度明显上升后再拆。

## 16. 本次文档处理

本次仅整理文档，不修改业务代码、前端页面、后端接口、数据库、MCP tool、personal_weekly、team / department 或定时任务。

已删除旧窄范围文档：

```text
doc/AI Assets Agent运行页交互改造方案.md
```

已新增整体方案文档：

```text
doc/v1/AI Assets整体交互改造方案.md
```

旧文档中关于第三方 RunView、Report Agent 业务化运行页、后端注入参数、token 脱敏、测试计划等内容已迁移并扩展到本文档。

## 17. 本轮实现结果

本轮已进入实现，不再停留在方案阶段。最终落地口径如下：

- AI Assets 是 Agent 创建、编辑、运行和运行历史入口。
- 报告弹窗只负责报告产物查看、编辑、保存和状态展示。
- 普通 Agent 保留通用运行页：model override、message、params、start prompt template、运行状态、运行结果。
- Report Agent 使用业务化运行页：`report_type` 枚举、日期 / 周期、model override、可选补充说明。
- Report Agent 运行页不展示 `run_id`、`mcp_url`、`mcp_authorization`、token、credential slot、MCP internal config、`period.date`、原始 START PROMPT VALUES 或通用 key=value 参数。
- 新增正式入口：`POST /api/v1/ai-assets/report-agents/{agentId}/runs`。
- Report Agent 正式走第三方 `/api/session + credential_overrides`。
- `AIDA_REPORT_MCP_AUTH` credential slot 注入当前用户级 Aida token，前端不传 token，`ai_runs.input_ref_json` 不保存明文 token。
- `report_type` 可选项来自 Agent 稳定标记和当前用户角色权限；P0 后端仅真实执行 `personal_daily`，其它类型返回 `REPORT_TYPE_NOT_SUPPORTED`。
- 旧 `POST /api/v1/reports/today/default-managed-agent-runs` 不作为正式入口，当前代码中已无 route / handler / frontend client 调用。
- personal_weekly、team、department、定时任务 Report Agent 收敛仍放到后续阶段，不在本轮扩展。
