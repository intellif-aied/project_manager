# Agent Action 触发架构与实现方案

## 1. 结论

当前阶段不建议为每个业务页面建设独立的 Agent 配置页。

推荐采用 **Agent Action Registry**：

- 页面按钮、路由、定时任务、事件回调都只触发 `action_key`。
- 后端根据 `action_key` 解析 Agent、模型、Start Prompt、参数映射、MCP 工具、Skill 规则和输出处理器。
- Managed Agent 平台只作为运行时执行引擎，由后端统一调用。
- 定时任务复用同一个 Action Runner，不单独建设一套配置页面。

核心链路：

```text
业务页面按钮 / 定时任务 / 业务事件
  -> POST /api/v1/agent-actions/{action_key}/runs
  -> Action Registry 解析动作定义
  -> Context Provider 补齐业务上下文
  -> Prompt Renderer 渲染 Start Prompt
  -> Managed Agent Client 提交运行
  -> Run Store 保存运行记录
  -> Output Handler 保存草稿或业务结果
```

## 2. 目标

### 2.1 产品目标

- 普通用户可以在日报页面手动生成个人日报。
- Team Leader 可以在团队日报页面生成团队日报。
- 系统可以按固定时间自动生成草稿。
- 用户不需要理解 Managed Agent、MCP、Skill、Prompt 配置细节。
- 管理员后续可以统一维护业务 Action，而不是分散到各个页面。

### 2.2 技术目标

- 前端只关心当前页面有哪些可用 Action。
- 后端统一管理鉴权 token、权限、审计、运行历史和错误处理。
- 同一个 Action 可被手动、定时、事件三种触发方式复用。
- P0 先用代码或配置文件注册 Action，避免过早建设复杂配置后台。

## 3. 概念边界

| 概念 | 职责 | 示例 |
|---|---|---|
| Agent Action | 产品触发层，描述一个可运行的业务动作 | `daily_report.generate` |
| Managed Agent | 外部平台中的 Agent 运行实例 | `aida-daily-report-agent` |
| Skill | 生成规则、写作结构、输出协议 | 日报写作 Skill |
| MCP | 数据读取或写入工具能力 | 读取 session、任务、日报草稿 |
| Context Provider | Aida 后端本地上下文装配器 | 获取用户当天工作记录 |
| Output Handler | 运行完成后的业务落库逻辑 | 保存日报草稿 |
| Run Store | 运行记录、状态、错误和输出索引 | `ai_runs` / `agent_action_runs` |

关键判断：

- MCP/Skill 是能力层，不是产品触发层。
- 页面按钮不应该直接调用 MCP 或 Managed Agent。
- 定时任务不是一种新配置模型，只是 Action 的一种触发来源。

## 4. P0 架构

### 4.1 Action Registry

P0 使用代码或 YAML 配置注册 Action。

示例：

```yaml
actions:
  - action_key: daily_report.generate
    name: 生成个人日报
    surface: reports
    managed_agent_id: aida-daily-report-agent
    model_id: Kimi-K2.6
    permission_scope: self
    trigger_types:
      - manual
      - schedule
    start_prompt_template: |
      基于 {{report_date}} 的工作上下文，为 {{user_name}} 生成个人日报草稿。
      输出必须包含：今日完成、阻塞风险、明日计划。
    params:
      report_date:
        source: request
        required: true
      user_id:
        source: current_user
        required: true
      sessions:
        source: context_provider
        provider: daily_report_context
      tasks:
        source: context_provider
        provider: daily_report_context
    mcp_tools:
      - aida_context.get_daily_sessions
      - aida_context.get_task_changes
    skill: daily_report_writing
    output_handler: save_daily_report_draft
```

### 4.2 后端模块

建议新增或演进以下模块：

| 模块 | 职责 |
|---|---|
| `api/service/agent_action.go` | Action Runner 主流程 |
| `api/service/agent_action_registry.go` | 加载和查询 Action 定义 |
| `api/service/action_context.go` | 根据参数来源装配上下文 |
| `api/service/action_prompt.go` | 渲染 Start Prompt |
| `api/service/action_output.go` | 运行完成后的输出处理 |
| `api/handler/agent_action.go` | 暴露 Action API |
| `api/service/managed_agent.go` | 继续封装 Managed Agent 平台调用 |

P0 可以复用已实现的 `ai_runs` 表；如果需要更清晰的业务语义，再新增 `agent_action_runs`。

### 4.3 后端运行流程

```text
RunAction(action_key, actor, request_params)
  1. 读取 Action 定义
  2. 校验 actor 是否有权限触发
  3. 根据 params.source 装配参数
  4. 渲染 start_prompt_template
  5. 创建本地运行记录
  6. 调 Managed Agent 平台提交运行
  7. 保存 external_run_id / task_id
  8. 轮询或异步刷新运行状态
  9. 成功后调用 output_handler
  10. 返回运行记录和业务输出
```

## 5. API 设计

### 5.1 查询页面可用 Action

```http
GET /api/v1/agent-actions?surface=reports
```

返回：

```json
{
  "items": [
    {
      "action_key": "daily_report.generate",
      "name": "生成日报",
      "surface": "reports",
      "trigger_types": ["manual", "schedule"],
      "permission_scope": "self",
      "enabled": true
    }
  ]
}
```

### 5.2 手动触发 Action

```http
POST /api/v1/agent-actions/daily_report.generate/runs
Content-Type: application/json

{
  "surface": "reports",
  "params": {
    "report_date": "2026-06-26"
  }
}
```

返回：

```json
{
  "run_id": "run_20260626_0924",
  "action_key": "daily_report.generate",
  "status": "pending",
  "managed_agent_id": "aida-daily-report-agent",
  "external_run_id": "task_xxx"
}
```

### 5.3 查询运行历史

```http
GET /api/v1/agent-action-runs?surface=reports&action_key=daily_report.generate&page_size=20
```

### 5.4 查询单次运行

```http
GET /api/v1/agent-action-runs/{run_id}
```

### 5.5 用户定时开关

P0 可以先不做复杂配置页，只提供轻量开关：

```http
PATCH /api/v1/me/agent-actions/daily_report.generate/schedule
Content-Type: application/json

{
  "enabled": true
}
```

时间策略由系统默认配置控制，例如工作日 19:00。

## 6. 数据模型

### 6.1 P0 推荐

P0 最小可行方案：

- Action 定义：代码或 YAML 文件。
- 运行记录：复用 `ai_runs`，增加 `action_key`、`surface`、`trigger_source` 字段。
- 定时开关：新增用户级订阅表。

### 6.2 表结构建议

```sql
CREATE TABLE agent_action_subscriptions (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL,
  action_key TEXT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT true,
  schedule_policy TEXT NOT NULL DEFAULT 'system_default',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, action_key)
);
```

如果后续需要后台可视化维护 Action，再增加：

```sql
CREATE TABLE agent_actions (
  action_key TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  surface TEXT NOT NULL,
  managed_agent_id TEXT NOT NULL,
  model_id TEXT,
  permission_scope TEXT NOT NULL,
  trigger_types JSONB NOT NULL DEFAULT '[]'::jsonb,
  start_prompt_template TEXT NOT NULL,
  params_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
  mcp_tools JSONB NOT NULL DEFAULT '[]'::jsonb,
  skill TEXT,
  output_handler TEXT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## 7. 前端设计

### 7.1 页面接入方式

业务页面不维护 Agent 配置，只接入通用组件。

建议组件：

| 组件 | 职责 |
|---|---|
| `AgentActionButton` | 根据 `actionKey` 触发运行 |
| `AgentActionDrawer` | 展示参数、提交状态、运行结果 |
| `AgentRunHistoryPanel` | 展示当前页面或当前 Action 的运行历史 |
| `AgentScheduleSwitch` | 用户级启停定时任务 |

日报页面示例：

```tsx
<AgentActionButton
  actionKey="daily_report.generate"
  surface="reports"
  params={{ report_date: selectedDate }}
/>
```

团队日报页面示例：

```tsx
<AgentActionButton
  actionKey="team_report.generate"
  surface="team-reports"
  params={{ report_date: selectedDate, team_id: currentTeamId }}
/>
```

### 7.2 页面体验

日报页面只展示：

- 生成日报按钮
- 最近运行状态
- 草稿生成结果
- 可选的自动生成开关

不展示：

- Agent ID 配置
- MCP 配置
- Skill 配置
- Start Prompt 编辑器
- Managed Agent token

这些都属于后端 Registry 或管理员能力。

## 8. 定时任务设计

定时任务调用同一个 Action Runner。

```text
Scheduler Tick
  -> 查询启用的 action subscription
  -> 生成 request_params
  -> RunAction(action_key, actor, params)
```

P0 策略：

- 个人日报：工作日 19:00 自动生成草稿。
- 用户可以开启或关闭。
- 不允许普通用户自定义 Prompt、Agent 和 MCP。
- 失败后重试一次，仍失败则进入运行历史并显示错误。

后续 P1 再支持：

- 自定义时间。
- 周末策略。
- 失败通知。
- 团队级定时任务。

## 9. 权限设计

| scope | 允许范围 |
|---|---|
| `self` | 只能读取和生成自己的内容 |
| `team` | Team Leader 可读取团队成员日报上下文 |
| `project` | 项目角色可读取项目需求、任务、风险 |
| `admin` | 管理员维护 Action 定义 |

权限必须在后端校验，不能依赖前端隐藏按钮。

## 10. 输出处理

Managed Agent 返回结果后，不直接把文本丢给页面，需要经过 `output_handler`。

常见处理器：

| output_handler | 行为 |
|---|---|
| `save_daily_report_draft` | 保存个人日报草稿 |
| `save_team_report_draft` | 保存团队日报草稿 |
| `save_requirement_risk_note` | 保存需求风险摘要 |
| `append_agent_run_artifact` | 仅保存运行产物 |

处理失败时：

- 保留原始运行输出。
- 运行状态标记为 `output_failed` 或记录 `error_message`。
- 页面提示“生成成功但保存失败”，方便人工恢复。

## 11. 实施阶段

### P0：日报 Action 跑通

- 定义 `daily_report.generate`。
- 新增 Action Runner。
- 日报页面接入 `AgentActionButton`。
- 复用 Managed Agent Client。
- 运行记录进入统一历史。
- 系统级定时任务每天生成草稿。

### P1：团队日报和用户定时开关

- 定义 `team_report.generate`。
- 增加 `AgentScheduleSwitch`。
- 增加 `agent_action_subscriptions`。
- 支持 Team Leader 权限校验。

### P2：管理员配置中心

- 将 Action Registry 从代码配置升级为数据库配置。
- 增加管理员 Action 编辑页。
- 支持 Prompt 版本、灰度、禁用和回滚。
- 支持事件触发 Action。

## 12. 验收标准

P0 验收：

- 日报页面点击“生成日报”可触发 `daily_report.generate`。
- 前端不需要传 Agent token。
- 后端运行记录包含 `action_key`、`surface`、`trigger_source`、`managed_agent_id`、`external_run_id`。
- 运行失败时页面能展示明确错误。
- 用户能看到自己的运行历史。
- 定时任务和手动按钮复用同一套 Action Runner。
- 普通用户不能查看或编辑 Agent 定义。

## 13. 原型

原型文件：

- `doc/v1/prototypes/agent-action-trigger-prototype.html`

该原型表达：

- 业务页面只放 Action 入口。
- Action Registry 是后端统一解析层。
- 定时任务复用 Action，不需要每个页面单独配置。
- 运行历史按 Action 统一沉淀。
