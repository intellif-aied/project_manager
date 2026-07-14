# Aida Report MCP 使用说明

这份文档给编写自定义 Skill 的用户使用。Aida Report MCP 用来让 Agent / Skill 读取 Aida 数据并回写日报、周报。

普通用户不需要手动配置 MCP 地址和 token。默认报告 Agent 已由平台绑定 MCP，Skill 里只需要说明应该调用哪些 MCP 工具。

## 1. 能做什么

Aida Report MCP 当前提供 9 个工具：

| 工具 | 用途 |
| --- | --- |
| `get_existing_report` | 读取当前日期/周期已有的报告内容 |
| `get_sessions` | 获取当前用户可见的 Session 切片 |
| `get_tasks` | 获取当前用户可见的任务 |
| `get_requirements` | 获取当前用户可见的需求 |
| `get_daily_reports` | 获取日报列表，可读个人/小组/部门日报 |
| `get_weekly_reports` | 获取周报列表，可读个人/小组/部门周报 |
| `get_report_inventory` | 统计报告提交情况，例如应交、已交、缺失 |
| `write_report_result` | 写入 Agent 生成的报告内容 |
| `write_report_failure` | 记录 Agent 生成失败原因 |

## 2. Skill 输入

报告 Agent 运行时会给 Skill 传入这些字段：

| 字段 | 说明 |
| --- | --- |
| `report_type` | 报告类型 |
| `period` | 日期或周范围 |
| `target` | 生成对象 |
| `run_id` | 本次生成任务 ID，写回报告时必须原样使用 |
| `selected_session_slice_keys` | 可选。用户手动选择的 Session 切片，只用于个人日报/周报 |

支持的 `report_type`：

| report_type | 说明 |
| --- | --- |
| `personal_daily` | 个人日报 |
| `personal_weekly` | 个人周报 |
| `team_daily` | 小组日报 |
| `team_weekly` | 小组周报 |
| `department_daily` | 部门日报 |
| `department_weekly` | 部门周报 |

`period` 示例：

```json
{"date":"2026-07-06"}
```

```json
{"week_start":"2026-07-06","week_end":"2026-07-12"}
```

`selected_session_slice_keys` 示例：

```json
[
  "3273abd1-3ca7-4c4e-9aa9-5c8fcdb862b8:2026-07-06",
  "eb1671e2-05fb-4e81-af4c-fe4e97d46588:2026-07-06"
]
```

## 3. 通用参数

日期范围：

```json
{"start":"2026-07-06","end":"2026-07-06"}
```

周范围：

```json
{"week_start":"2026-07-06","week_end":"2026-07-12"}
```

scope：

```json
{"type":"self"}
```

```json
{"type":"team"}
```

```json
{"type":"department"}
```

target 由平台运行输入提供，Skill 不要自己编造用户 ID、小组 ID 或部门 ID。

## 4. 工具调用示例

下面示例都是 MCP 工具的 arguments，不是 HTTP 请求体。Skill 应该直接调用绑定好的 MCP 工具，不要手写 URL。

MCP 工具结果通常是 JSON 文本内容。读取结果时先解析 `content[0].text`，再使用里面的字段。

### 4.1 读取已有报告

生成前先调用一次，避免覆盖用户已有内容时没有上下文。

```json
{
  "report_type": "personal_daily",
  "period": {"date":"2026-07-06"},
  "target": {"type":"self"}
}
```

### 4.2 获取 Session 切片

个人日报/周报使用。小组和部门报告不要默认读取成员 Session。

```json
{
  "scope": {"type":"self"},
  "target": {"type":"self"},
  "date_range": {"start":"2026-07-06","end":"2026-07-06"},
  "include_summary": true,
  "selected_session_slice_keys": [
    "3273abd1-3ca7-4c4e-9aa9-5c8fcdb862b8:2026-07-06"
  ]
}
```

如果没有 `selected_session_slice_keys`，可以不传这个字段，MCP 会按日期范围返回可见 Session 切片。

### 4.3 获取任务

```json
{
  "scope": {"type":"self"},
  "target": {"type":"self"},
  "date_range": {"start":"2026-07-06","end":"2026-07-06"},
  "include_requirement": true
}
```

### 4.4 获取需求

```json
{
  "scope": {"type":"self"},
  "target": {"type":"self"},
  "date_range": {"start":"2026-07-06","end":"2026-07-06"},
  "include_tasks": true,
  "include_risks": true
}
```

### 4.5 获取日报列表

小组日报通常读取成员个人日报，部门日报通常读取小组日报。

```json
{
  "scope": {"type":"team"},
  "target": {"type":"team"},
  "date_range": {"start":"2026-07-06","end":"2026-07-06"},
  "report_scope": "personal",
  "include_content": true
}
```

### 4.6 获取周报列表

```json
{
  "scope": {"type":"team"},
  "target": {"type":"team"},
  "week_range": {"week_start":"2026-07-06","week_end":"2026-07-12"},
  "report_scope": "personal",
  "include_content": true
}
```

### 4.7 获取报告提交情况

用于小组/部门报告，确认谁已提交、谁缺失。

```json
{
  "scope": {"type":"team"},
  "target": {"type":"team"},
  "report_scope": "personal",
  "report_kind": "daily",
  "date_range": {"start":"2026-07-06","end":"2026-07-06"}
}
```

周报也要传 `date_range`，同时可以传 `week_range`：

```json
{
  "scope": {"type":"team"},
  "target": {"type":"team"},
  "report_scope": "personal",
  "report_kind": "weekly",
  "date_range": {"start":"2026-07-06","end":"2026-07-12"},
  "week_range": {"week_start":"2026-07-06","week_end":"2026-07-12"}
}
```

### 4.8 写入报告结果

生成成功后必须调用。

```json
{
  "report_type": "personal_daily",
  "period": {"date":"2026-07-06"},
  "target": {"type":"self"},
  "run_id": "平台传入的 run_id",
  "content": "生成的 Markdown 报告正文",
  "summary": "可选摘要"
}
```

### 4.9 写入失败原因

生成失败时调用，不要直接静默失败。

```json
{
  "report_type": "personal_daily",
  "period": {"date":"2026-07-06"},
  "target": {"type":"self"},
  "run_id": "平台传入的 run_id",
  "error_message": "没有找到可用于生成日报的上下文"
}
```

## 5. 推荐调用流程

### 个人日报

1. `get_existing_report`
2. `get_sessions`
3. `get_tasks`
4. `get_requirements`
5. 生成 Markdown
6. `write_report_result`

### 个人周报

1. `get_existing_report`
2. `get_daily_reports`，读取本周个人日报
3. `get_tasks`
4. `get_requirements`
5. 如果用户选择了 Session 切片，再调用 `get_sessions`
6. 生成 Markdown
7. `write_report_result`

### 小组日报

1. `get_existing_report`
2. `get_daily_reports`，`report_scope` 使用 `personal`
3. `get_report_inventory`，`report_scope` 使用 `personal`，`report_kind` 使用 `daily`
4. 必要时调用 `get_tasks` / `get_requirements`
5. 生成 Markdown
6. `write_report_result`

### 小组周报

1. `get_existing_report`
2. `get_weekly_reports`，`report_scope` 使用 `personal`
3. `get_daily_reports`，补充本周个人日报
4. `get_report_inventory`，`report_scope` 使用 `personal`，`report_kind` 使用 `weekly`
5. 必要时调用 `get_tasks` / `get_requirements`
6. 生成 Markdown
7. `write_report_result`

### 部门日报

1. `get_existing_report`
2. `get_daily_reports`，`report_scope` 使用 `team`
3. `get_report_inventory`，`report_scope` 使用 `team`，`report_kind` 使用 `daily`
4. 必要时调用 `get_requirements`
5. 生成 Markdown
6. `write_report_result`

### 部门周报

1. `get_existing_report`
2. `get_weekly_reports`，`report_scope` 使用 `team`
3. `get_daily_reports`，补充小组日报
4. `get_report_inventory`，`report_scope` 使用 `team`，`report_kind` 使用 `weekly`
5. 必要时调用 `get_requirements`
6. 生成 Markdown
7. `write_report_result`

## 6. 写 Skill 时可以直接使用的提示词片段

```md
Use Aida Report MCP tools to generate reports.

Input includes report_type, period, target, run_id, and optional selected_session_slice_keys.

Rules:
- First call get_existing_report with report_type, period, and target.
- Use only facts returned by MCP tools. Do not invent sessions, tasks, blockers, members, teams, or departments.
- For personal_daily, call get_sessions, get_tasks, and get_requirements.
- For personal_weekly, use get_daily_reports first; call get_sessions only when selected_session_slice_keys is provided or daily reports are insufficient.
- For team reports, use personal daily/weekly reports and get_report_inventory. Do not read all team member sessions by default.
- For department reports, use team daily/weekly reports and get_report_inventory. Do not read all member sessions by default.
- selected_session_slice_keys only applies to personal reports.
- Write the final Markdown with write_report_result.
- If generation fails, call write_report_failure.
- Never ask the user for MCP URL, token, credentials, run_id, or session ids.
```

## 7. 常见错误

- 不要在 Skill 里写 `curl`、`WebFetch` 或手动请求 MCP URL。
- 不要让用户提供 token。平台会在运行时注入权限。
- 不要把 `period` 传给 `get_sessions`、`get_tasks`、`get_requirements`、`get_daily_reports`、`get_weekly_reports`；这些工具使用 `date_range` 或 `week_range`。
- 不要把 `date_range` 或 `week_range` 传给 `write_report_result`、`write_report_failure`、`get_existing_report`。
- 不要把个人 Session 切片用于小组或部门报告。
- 不要用 Session 数量当作小组/部门成员名单。小组/部门成员范围以 MCP 返回的 `scope_context` 或 `get_report_inventory` 为准。
- 不要在用户可见报告里输出 `run_id`、MCP 地址、token、credential slot、用户 ID、团队 ID、Session ID。
- 不要只生成文本不写回。报告生成成功后必须调用 `write_report_result`。

## 8. 输出要求

报告正文建议使用简洁中文 Markdown。

如果数据不足，直接说明“当前上下文不足”，不要补造任务、进度、风险、成员或结论。
