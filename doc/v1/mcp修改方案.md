# Report MCP 修改方案

> 本文档基于 `doc/v1/ReportMCP能力技术文档.md`（最新需求）与当前 `fea.0.0.1` 分支代码现状梳理产出，作为 Report MCP 后续改造的执行依据。

## 1. 当前 MCP 实现盘点

> 当前项目仍处于开发期，无线上兼容包袱。本盘点仅用于说明现状，所有"旧"实现均按**待删除/待重构**处理，不保留兼容入口、不保留旧 tool、不保留旧数据流。

### 1.1 入口与路由

- 文件：`api/handler/daily_report_mcp.go`
- 当前路由（`api/main.go:193-194`）：
  - `POST /api/v1/mcp/daily-report` → `DailyReportMCPHandler.Serve`（**待删除**：旧入口，仅 personal_daily 上下文 + 草稿保存）
  - `POST /api/v1/mcp/reports` → `DailyReportMCPHandler.ServeReports`（**收敛为唯一入口**）
- 目标路由（Phase 1 完成后）：仅保留 `POST /api/v1/mcp/reports`，删除 `/api/v1/mcp/daily-report` 注册。
- 协议：JSON-RPC MCP（`initialize` / `ping` / `tools/list` / `tools/call` / `notifications/*`），协议版本 `2024-11-05`。
- 鉴权：复用 `AuthMiddleware`（`api/handler/middleware.go`），通过 Bearer Token 解析 AIHub UID，加载 Aida `User`，写入 `r.Context()`；`getUser(r)` 取出当前用户 `ID / Role / TeamID`。

### 1.2 已实现工具（待删除/待重构）

`/api/v1/mcp/reports` 当前通过 `reportMCPTools()` 暴露 3 个工具，全部硬编码 `report_type=personal_daily`：

| Tool                   | 位置                                     | 现状处理                                                                 |
| ---------------------- | ---------------------------------------- | ------------------------------------------------------------------------ |
| `get_report_context`   | `daily_report_mcp.go:278` `getReportContext` | **待删除**：大上下文工具，不符合原子能力方向；personal_daily 改走新工具链 |
| `write_report_result`  | `daily_report_mcp.go:351` `writeReportResult` | **待重构**：按 report_type 分发到 6 类报告                                 |
| `write_report_failure` | `daily_report_mcp.go:459` `writeReportFailure` | **待重构**：去 personal_daily 硬编码，按 report_type 分发                  |

旧入口 `/mcp/daily-report` 提供的 `aida_daily_report_get_context` 和 `aida_daily_report_save_draft` 同样**待删除**，不保留兼容。

新 `/api/v1/mcp/reports` 的 `tools/list` 最终只包含 9 个原子工具（见 §3.8），不返回 `get_report_context` / `aida_daily_report_get_context` / `aida_daily_report_save_draft`。

### 1.3 关键数据流（旧实现，待删除/待重构）

- `getReportContext`（**待删除**）：
  1. 校验 `report_type == personal_daily`（`validatePersonalDailyReportArgs`）；
  2. 解析 `period.date`（`requireReportDate` 强制 `YYYY-MM-DD`）；
  3. 若传 `run_id`，调 `loadAIRunForUser` 校验归属；
  4. `loadDailyReportSessionIDs`：从 `daily_reports.session_ids` 拿用户当日选中的 session 列表；
  5. `loadDraftSessions` / `loadDraftTaskCandidates` / `loadPersonalDailyReport`；
  6. 返回 `report / actor / current_user / source_summary / context{...} / constraints / output_contract`。
  该数据流将完全删除；personal_daily Agent 改为调用原子工具组合（见 §3.4 与 §4 Phase 1）。
- `writeReportResult`（**待重构**）：
  1. 校验 `report_type`、`period.date`、`run_id`；
  2. `loadAIRunForUser` 获取 run；
  3. `BEGIN Tx` → `loadPersonalDailyReportForUpdate`（`SELECT ... FOR UPDATE`）；
  4. **防覆盖**：`existing.Edited && existing.UpdatedAt > run.CreatedAt` → 标记 run `failed` 并返回 `REPORT_EDIT_CONFLICT`；
  5. `INSERT ... ON CONFLICT (user_id, report_date) DO UPDATE` 写入 `content / edited=false / generation_mode='managed_agent' / managed_agent_run_id / agent_id / model_id / status='saved' / saved_at=now()`；
  6. `UPDATE ai_runs SET status='succeeded', business_id=report_id, output_ref_json, error_message=NULL, finished_at=now()`；
  7. 返回 `status / report_id / report_type / product_status=ai_generated / origin=ai / updated_by_user=false / agent_run_id / managed_agent_run_id / usable_for_rollup=true`。
  该逻辑保留防覆盖与 `ai_runs` 状态机，外层改为按 `report_type` 分发到 6 类报告（§3.5.1）。
- `writeReportFailure`（**待重构**）：仅 `UPDATE ai_runs SET status='failed', error_message, finished_at`，不写报告。去 personal_daily 硬编码，按 `report_type` 分发。

### 1.4 数据模型与已有 schema

`daily_reports`（`api/db/migrations/001_init.sql:172`）已具备 `generation_mode / managed_agent_run_id / agent_id / agent_version_id / model_id` 列，并有 `daily_reports_managed_agent_run_fk` 外键。

`ai_runs`（同文件 `:284`）字段：`business_type / business_id / runtime_type / agent_id / agent_version_id / external_task_id / external_session_id / model_id / status / input_ref_json / output_ref_json / error_message / started_at / finished_at`。

其余报告事实源表：

| 表                            | 现有列（与写回相关）                                                                                          | 缺失字段（Agent 写回需要）                                                                                     |
| ----------------------------- | ------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `personal_weekly_reports`     | `content / status / saved_at / submitted_at / source_daily_report_ids / source_session_ids / source_task_ids` | `generation_mode / managed_agent_run_id / agent_id / agent_version_id / model_id / edited`                  |
| `team_reports`                | `content / status / saved_at / submitted_at / source_daily_report_ids / member_report_ids / session_ids`     | 同上 + `agent_run_id` 关联                                                                                       |
| `team_weekly_reports`        | `content / source_daily_report_ids / source_team_report_ids / source_task_ids / source_personal_weekly_report_ids` | 同上                                                                                                          |
| `department_reports`         | `content / status / saved_at / archived_at / source_team_report_ids`                                          | `generation_mode / managed_agent_run_id / agent_id / agent_version_id / model_id / edited`                  |
| `department_weekly_reports`  | `content / archived_at / source_team_weekly_report_ids`                                                       | 同上                                                                                                          |

### 1.5 Managed Agent 调用链

`ManagedAgentClient`（`api/service/managed_agent.go`）已封装：`ListSkills / ListMCPEntries / CreateMCPEntry / ListMyAgents / CreateMyAgent / UpdateMyAgent / CreateCredential / CreateSession / SubmitTask / GetTaskResult / GetTaskStatus`，并支持 `WithToken`（按当前用户 token 克隆 client）。

`ManagedAgentHandler.StartDailyReportRun`（`managed_agent.go:1069`）已实现：拉 session 列表 → 校验 → 构造 `inputRef` → `SubmitTask` → `insertAIRun(business_type="daily_report")` → 返回 `AIRun`。`refreshAIRun`（`:1169`）轮询 task 状态并解析 draft。

### 1.6 权限现状

- `AuthMiddleware` 仅识别身份，不做 scope 收敛。
- `SessionHandler.List`（`session.go:30`）按角色收敛：employee → 自己；team_leader/pm → `team_id` 成员；director/admin → 全局。**未对 PM 收敛为 self**（PM 当前等同 team_leader，与文档 §7.3 冲突）。
- `ReportHandler` 系列（`report.go:28-1850`）已有 team/department 列表与写回，但前端按钮和权限校验混在一起，缺少可复用的 scope 校验工具。
- MCP 现有实现完全没做 scope：旧 `getReportContext` 只读自己，无 `team / department / all` 能力（该实现待删除）。

## 2. 与需求文档的差距

按 `ReportMCP能力技术文档.md` 章节对照：

### 2.1 §4 report_type 覆盖

| 需求 report_type       | 当前状态                                       |
| ---------------------- | ------------------------------------------ |
| `personal_daily`       | ⚠️ 旧实现（待迁移）：走旧 `get_report_context` + personal_daily 写回 |
| `personal_weekly`     | ❌ 无写回、无上下文                                |
| `team_daily`          | ❌ 无写回、无上下文                                |
| `team_weekly`         | ❌ 无写回、无上下文                                |
| `department_daily`    | ❌ 无写回、无上下文                                |
| `department_weekly`   | ❌ 无写回、无上下文                                |

### 2.2 §6 工具能力覆盖

| 需求 Tool               | 当前实现                                              | 差距                                            |
| ---------------------- | ------------------------------------------------- | --------------------------------------------- |
| `get_sessions`        | ❌ 无                                              | 需新建：按 scope+date_range 查 session，含 summary |
| `get_daily_reports`   | ❌ 无                                              | 需新建：支持 personal/team/department 三种 report_scope |
| `get_weekly_reports`  | ❌ 无                                              | 需新建                                           |
| `get_tasks`           | ❌ 无                                              | 需新建：按 scope 收敛                              |
| `get_requirements`    | ❌ 无                                              | 需新建                                           |
| `get_existing_report` | ⚠️ 隐式（在旧 `getReportContext` 里返回 current_report） | 需新建为独立工具：按 report_type+period+target 直查（旧 `getReportContext` 待删除） |
| `get_report_inventory` | ❌ 无                                              | 需新建：返回 expected/existing/missing           |
| `write_report_result` | ⚠️ 仅 personal_daily（待重构）                          | 扩展支持 6 类 report_type                          |
| `write_report_failure` | ⚠️ 仅 personal_daily（待重构）                          | 扩展支持 6 类 report_type                          |

### 2.3 §7-8 权限/scope 模型

- 当前 MCP：无 scope 参数，所有读取隐式按 `self`。
- `SessionHandler` PM 口径与文档冲突（PM 应等同个人用户）。
- 无 `scope` 收敛中间件/工具函数。
- 写回权限未与读权限分离校验（`writeReportResult` 只校验 run 归属，未校验目标报告归属——当前 personal_daily 场景下天然正确，但扩展到 team/department 后会越权）。

### 2.4 §9 时间参数

- 当前：仅 `period.date`，无 `week_range`。
- 需新增：`week_range{week_start, week_end}`、`date_range{start, end}`，并在写回时按 report_type 分支校验。

### 2.5 §11 product_status

- 当前：`personalDailyProductStatus` 返回 `missing / ai_generated / modified / manual`，缺 `generation_failed`。
- 需补全：`generation_failed`（目标 period 无报告，且最近一次对应 report_type 的 `ai_run` failed 时返回，见 §3.6）。

### 2.6 §12 错误码

- 当前：`REPORT_EDIT_CONFLICT` 已定义；其余用裸 `fmt.Errorf`，错误码缺失。
- 需补全：`UNAUTHORIZED / FORBIDDEN / REPORT_TYPE_NOT_SUPPORTED / INVALID_PERIOD / INVALID_SCOPE / RUN_NOT_FOUND / RUN_FORBIDDEN / REPORT_NOT_FOUND / MCP_INTERNAL_ERROR`。

### 2.7 §13 安全要求

- 当前 token 通过 `Authorization: Bearer` 透传到 Managed Agent，未在 `input_ref_json` 中保存（✅）。
- 但 `StartDailyReportRun` 把 `urls` 数组写进 `input_ref`，URL 里不含 token（✅）。
- 未做 scope 收敛即等价于"全部按 self"（✅ 安全，但能力受限）。

## 3. 修改方案

### 3.1 总体结构

将 `DailyReportMCPHandler` 重构为 `ReportMCPHandler`（不保留旧名别名，直接改名以彻底切干净），按"report_type + target"双维度分发。旧 `get_report_context` 与 `/mcp/daily-report` 入口完全删除，不保留兼容：

```
ReportMCPHandler
├── serve()              // JSON-RPC 入口（/mcp/reports，唯一入口）
├── callReportTool()     // 按 tool.Name 分发到 9 个原子工具
│   ├── get_sessions
│   ├── get_daily_reports
│   ├── get_weekly_reports
│   ├── get_tasks
│   ├── get_requirements
│   ├── get_existing_report
│   ├── get_report_inventory
│   ├── write_report_result
│   └── write_report_failure
├── scopeResolver        // 新增：按当前用户角色收敛 scope（读权限边界）
├── targetResolver       // 新增：校验 + 收敛 target（写回目标 / 读缩小范围）
├── reportStore          // 新增：封装 6 类报告的读写
└── aiRunGuard           // 新增：run_id 归属与状态校验
```

文件拆分：

- `api/handler/report_mcp.go`：主 handler + JSON-RPC 协议层（`Serve` / `serve` / `initializeResult` / `reportMCPTools`）。
- `api/handler/report_mcp_scope.go`：scope 收敛 + target 收敛 + 权限校验。
- `api/handler/report_mcp_read.go`：7 个读取工具实现。
- `api/handler/report_mcp_write.go`：`write_report_result` / `write_report_failure` 按 report_type 分支。
- `api/handler/report_mcp_test.go`：MCP 协议、tools/list、9 工具的单元测试。
- `api/handler/daily_report_mcp.go`：旧文件。其中旧 `/mcp/daily-report` 入口、`getReportContext`、`saveDailyReportDraft`、`aida_daily_report_get_context` / `aida_daily_report_save_draft` tool 定义、`dailyReportMCPTools()`、旧 `daily_report_mcp_test.go` 中对应 personal_daily 专用测试**全部删除或迁移到新文件**，不作为兼容入口保留。

### 3.2 数据模型扩展

#### 3.2.1 数据库迁移

新增迁移 `api/db/migrations/002_report_agent_fields.sql`（前向迁移，符合 CLAUDE.md "prefer forward migrations" 约定）：

```sql
-- personal_weekly_reports
ALTER TABLE personal_weekly_reports
  ADD COLUMN IF NOT EXISTS generation_mode      TEXT NOT NULL DEFAULT 'default',
  ADD COLUMN IF NOT EXISTS managed_agent_run_id UUID,
  ADD COLUMN IF NOT EXISTS agent_id             TEXT,
  ADD COLUMN IF NOT EXISTS agent_version_id    INTEGER,
  ADD COLUMN IF NOT EXISTS model_id             TEXT,
  ADD COLUMN IF NOT EXISTS edited               BOOLEAN NOT NULL DEFAULT false;

-- team_reports
ALTER TABLE team_reports
  ADD COLUMN IF NOT EXISTS generation_mode      TEXT NOT NULL DEFAULT 'default',
  ADD COLUMN IF NOT EXISTS managed_agent_run_id UUID,
  ADD COLUMN IF NOT EXISTS agent_id             TEXT,
  ADD COLUMN IF NOT EXISTS agent_version_id    INTEGER,
  ADD COLUMN IF NOT EXISTS model_id             TEXT,
  ADD COLUMN IF NOT EXISTS edited               BOOLEAN NOT NULL DEFAULT false;

-- team_weekly_reports
ALTER TABLE team_weekly_reports
  ADD COLUMN IF NOT EXISTS generation_mode      TEXT NOT NULL DEFAULT 'default',
  ADD COLUMN IF NOT EXISTS managed_agent_run_id UUID,
  ADD COLUMN IF NOT EXISTS agent_id             TEXT,
  ADD COLUMN IF NOT EXISTS agent_version_id    INTEGER,
  ADD COLUMN IF NOT EXISTS model_id             TEXT,
  ADD COLUMN IF NOT EXISTS edited               BOOLEAN NOT NULL DEFAULT false;

-- department_reports
ALTER TABLE department_reports
  ADD COLUMN IF NOT EXISTS generation_mode      TEXT NOT NULL DEFAULT 'default',
  ADD COLUMN IF NOT EXISTS managed_agent_run_id UUID,
  ADD COLUMN IF NOT EXISTS agent_id             TEXT,
  ADD COLUMN IF NOT EXISTS agent_version_id    INTEGER,
  ADD COLUMN IF NOT EXISTS model_id             TEXT,
  ADD COLUMN IF NOT EXISTS edited               BOOLEAN NOT NULL DEFAULT false;

-- department_weekly_reports
ALTER TABLE department_weekly_reports
  ADD COLUMN IF NOT EXISTS generation_mode      TEXT NOT NULL DEFAULT 'default',
  ADD COLUMN IF NOT EXISTS managed_agent_run_id UUID,
  ADD COLUMN IF NOT EXISTS agent_id             TEXT,
  ADD COLUMN IF NOT EXISTS agent_version_id    INTEGER,
  ADD COLUMN IF NOT EXISTS model_id             TEXT,
  ADD COLUMN IF NOT EXISTS edited               BOOLEAN NOT NULL DEFAULT false;

-- 外键（ai_runs 已存在）。使用 DO + IF NOT EXISTS 保证可重复执行，避免重复 ADD CONSTRAINT 失败。
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'personal_weekly_reports_run_fk') THEN
    ALTER TABLE personal_weekly_reports ADD CONSTRAINT personal_weekly_reports_run_fk FOREIGN KEY (managed_agent_run_id) REFERENCES ai_runs(id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'team_reports_run_fk') THEN
    ALTER TABLE team_reports ADD CONSTRAINT team_reports_run_fk FOREIGN KEY (managed_agent_run_id) REFERENCES ai_runs(id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'team_weekly_reports_run_fk') THEN
    ALTER TABLE team_weekly_reports ADD CONSTRAINT team_weekly_reports_run_fk FOREIGN KEY (managed_agent_run_id) REFERENCES ai_runs(id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'department_reports_run_fk') THEN
    ALTER TABLE department_reports ADD CONSTRAINT department_reports_run_fk FOREIGN KEY (managed_agent_run_id) REFERENCES ai_runs(id);
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'department_weekly_reports_run_fk') THEN
    ALTER TABLE department_weekly_reports ADD CONSTRAINT department_weekly_reports_run_fk FOREIGN KEY (managed_agent_run_id) REFERENCES ai_runs(id);
  END IF;
END $$;
```

> 注：`daily_reports` 已有这些字段，无需变更。

#### 3.2.2 Model 扩展

在 `api/model/models.go` 给 5 个 weekly/team/department 报告 struct 追加字段（参考 `DailyReport` 已有写法）：

```go
type PersonalWeeklyReport struct {
  // ... 已有字段 ...
  GenerationMode    string     `json:"generation_mode,omitempty"`
  ManagedAgentRunID *string   `json:"managed_agent_run_id,omitempty"`
  AgentRunID        *string    `json:"agent_run_id,omitempty"`
  AgentID           *string    `json:"agent_id,omitempty"`
  AgentVersionID    *int       `json:"agent_version_id,omitempty"`
  ModelID           *string    `json:"model_id,omitempty"`
  Edited            bool       `json:"edited"`
  GeneratedAt       *time.Time `json:"generated_at,omitempty"`
  ProductStatus     string     `json:"product_status,omitempty"`
  Origin            string    `json:"origin,omitempty"`
  UpdatedByUser     bool       `json:"updated_by_user"`
}
// 对 TeamReport / TeamWeeklyReport / DepartmentReport / DepartmentWeeklyReport 同样追加
```

### 3.3 scope 收敛

新建 `api/handler/report_mcp_scope.go`：

```go
type reportScope struct {
  Type         string   // self | team | department | all
  TeamID       string
  DepartmentID string
  UserIDs      []string
}

// resolveScope 按当前用户角色收敛 Agent 传入的 scope。
// 越权时返回 FORBIDDEN 错误（不静默扩大权限，遵循 §8.2 建议）。
func resolveScope(u *model.User, in reportScope) (reportScope, error) {
  allowed := map[string][]string{
    "employee":     {"self"},
    "pm":           {"self"},
    "team_leader":  {"self", "team"},
    "director":    {"self", "department"},
    "admin":        {"self", "team", "department", "all"},
  }
  if !contains(allowed[u.Role], in.Type) {
    return reportScope{}, &mcpError{Code: "FORBIDDEN", Message: "scope not allowed for current role"}
  }
  // team scope 强制收敛为当前 TL 的 team_id
  if in.Type == "team" && u.Role == "team_leader" && u.TeamID != nil {
    in.TeamID = *u.TeamID
  }
  // department scope 收敛为当前 Director 管辖范围（按 teams.director_user_id）
  if in.Type == "department" && u.Role == "director" {
    in.DepartmentID = u.ID // Director 用户 ID 即其管辖部门标识
  }
  // user_ids 只能缩小范围，不能扩大
  return in, nil
}

// visibleUserIDs 返回某 scope 下可见的 user_id 列表，供 session/task/requirement 查询使用。
func (h *ReportMCPHandler) visibleUserIDs(ctx context.Context, u *model.User, scope reportScope) ([]string, error)
```

同时修复 `SessionHandler.List`（`session.go:59`）：将 `"team_leader", "pm"` 分支中的 `pm` 移到 employee 分支，让 PM 等同个人用户（§7.3）。PM 不再等同 team_leader，只能看自己的 session。跨团队 / 部门 session 汇总由 Director 或 Admin 身份完成。

#### 3.3.1 target 维度

`scope` 是读权限边界（self / team / department / all），但不足以覆盖 Admin / Director / team / department 写回场景。例如 Admin 写某员工个人日报时，需明确 `target.user_id`；Director 写某小组日报时，需明确 `target.team_id`。因此所有读取和写回参数新增 `target`：

```json
{
  "target": {
    "type": "self" | "user" | "team" | "department",
    "user_id": "optional",
    "team_id": "optional",
    "department_id": "optional"
  }
}
```

`target` 只能缩小范围，不能扩大权限。所有 `target` 必须经过 `targetResolver` 校验：

- `target.type=self`：默认值，等价于当前用户自己；
- `target.type=user` + `user_id`：Admin 可指定任意用户；Director 仅可指定部门内员工（且仅读，不可写个人报告）；TL 仅可指定小组成员（仅读）；employee/PM 仅可指定自己（等价 self）；
- `target.type=team` + `team_id`：TL 仅可指定所属小组；Director 可指定部门范围内小组（仅读，写需 §3.5.2 校验）；Admin 任意；
- `target.type=department` + `department_id`：Director 仅可指定自己管辖部门；Admin 任意。

`targetResolver` 返回最终目标标识（user_id / team_id / department_id），供 `reportStore` 定位报告。若 `target` 越权，返回 `FORBIDDEN`。

### 3.4 读取工具实现

每个工具遵循统一模板：解析 args → `resolveScope`（scope 收敛）→ `targetResolver`（target 校验与收敛）→ SQL 查询 → 结构化返回。所有读取工具同时接受 `scope`（权限边界）与 `target`（可选缩小范围），二者都经过收敛后用于 SQL `WHERE` 条件。下面给出关键签名与 SQL 骨架。

#### 3.4.1 `get_sessions`（`report_mcp_read.go`）

```go
func (h *ReportMCPHandler) toolGetSessions(r *http.Request, rawArgs json.RawMessage) (any, error) {
  u := getUser(r)
  var args struct {
    Scope          reportScope  `json:"scope"`
    Target         reportTarget `json:"target,omitempty"`
    DateRange      dateRange   `json:"date_range"`
    UserIDs        []string    `json:"user_ids,omitempty"`
    Limit          int         `json:"limit,omitempty"`
    IncludeSummary bool        `json:"include_summary,omitempty"`
  }
  // ... 解析 + resolveScope + targetResolver + visibleUserIDs ...
  rows, err := h.db.Query(`
    SELECT s.id::text, s.user_id::text, COALESCE(NULLIF(u.nickname,''),u.username), u.role, u.team_id::text,
           s.session_ref, s.started_at, s.ended_at, DATE(s.started_at), s.summary
    FROM sessions s JOIN users u ON u.id = s.user_id
    WHERE s.started_at >= $1 AND s.started_at < ($2::date + 1)
      AND s.user_id = ANY($3)
    ORDER BY s.started_at DESC LIMIT $4`, start, end, pq.Array(visible), limit)
  // 按 §10.1 返回结构组装 sessions + summary{total, by_date, by_user, truncated}
}
```

#### 3.4.2 `get_daily_reports`

支持 `report_scope ∈ {personal, team, department}`，分别查 `daily_reports / team_reports / department_reports`。`missing` 列表由 expected（scope 内所有应写报告的 owner×date）与 existing 做差集得到。

#### 3.4.3 `get_weekly_reports`

同上，按 `week_range` 查 3 张周报表。

#### 3.4.4 `get_tasks`

复用 `loadDraftTaskCandidates` 的查询，扩展为 `WHERE assignee_id = ANY($visible)`，并按 `status` 过滤。

#### 3.4.5 `get_requirements`

按 scope 内 user_ids 查 `requirements` 表（沿用 `requirement.go` 已有可见性规则，不重新定义权限，符合 §10.5）。

#### 3.4.6 `get_existing_report`

按 `report_type + period + target` 直查目标报告，返回 `{report: {...} | null, product_status}`。`target` 用于定位具体报告：personal 报告用 `target.user_id`，team 报告用 `target.team_id`，department 报告用 `target.department_id`。`product_status` 计算见 §3.6。

#### 3.4.7 `get_report_inventory`

按 `scope + target + report_scope + report_kind + date_range` 计算 expected/existing/missing，返回结构见 §10.7。**只提供完整度，不阻断生成**（§10.7 产品口径）。

### 3.5 写回工具扩展

#### 3.5.1 `write_report_result` 重构

将 `writeReportResult`（`daily_report_mcp.go:351`）重构为按 `report_type` 分发：

```go
func (h *ReportMCPHandler) writeReportResult(r *http.Request, rawArgs json.RawMessage) (any, error) {
  u := getUser(r)
  var args reportWriteResultArgs
  // ... 解析 args（含 target）...
  target, err := h.targetResolver(u, args.Target, args.ReportType, true)  // 写回 target 校验
  if err != nil { return nil, err }
  run, err := h.aiRunGuard(args.RunID, u.ID)         // 校验 run 归属
  if err != nil { return nil, err }

  switch args.ReportType {
  case "personal_daily":      return h.writePersonalDaily(r, u, run, args)
  case "personal_weekly":    return h.writePersonalWeekly(r, u, run, args)
  case "team_daily":         return h.writeTeamDaily(r, u, run, args)
  case "team_weekly":        return h.writeTeamWeekly(r, u, run, args)
  case "department_daily":   return h.writeDepartmentDaily(r, u, run, args)
  case "department_weekly":  return h.writeDepartmentWeekly(r, u, run, args)
  default:
    return nil, &mcpError{Code: "REPORT_TYPE_NOT_SUPPORTED", Message: "unsupported report_type: " + args.ReportType}
  }
}
```

每个分支遵循 `writePersonalDaily` 现有逻辑（§1.3）：

1. 校验 `period`（daily→`date`，weekly→`week_start+week_end`）；
2. 校验写回权限（§3.5.2）；
3. `BEGIN Tx` → `SELECT ... FOR UPDATE`；
4. 防覆盖检查（`edited && updated_at > run.created_at` → `REPORT_EDIT_CONFLICT`）；
5. `INSERT ... ON CONFLICT DO UPDATE` 写回；
6. `UPDATE ai_runs SET status='succeeded', business_id, output_ref_json, finished_at`；
7. 返回统一结构 `{status, report_type, report_id, agent_run_id, managed_agent_run_id, product_status, origin, updated_by_user}`。

#### 3.5.2 写回权限校验

新建 `assertWritePermission(u, reportType, target)`，写回权限与读取权限分开校验：

| report_type       | employee | PM  | TL       | Director  | Admin  |
| ----------------- | -------- | --- | -------- | --------- | ------ |
| personal_daily    | 自己      | 自己 | 仅自己     | 仅自己      | 所有人   |
| personal_weekly   | 自己      | 自己 | 仅自己     | 仅自己      | 所有人   |
| team_daily        | ❌       | ❌   | 所属小组   | ❌        | 所有小组  |
| team_weekly       | ❌       | ❌   | 所属小组   | ❌        | 所有小组  |
| department_daily  | ❌       | ❌   | ❌        | 部门       | 所有部门  |
| department_weekly | ❌       | ❌   | ❌        | 部门       | 所有部门  |

关键口径：
- TL 默认不能写小组成员个人日报 / 周报（`personal_daily` / `personal_weekly`），只能读；
- Director 默认不能写部门员工个人日报 / 周报，只能读；
- TL 写所属小组报告（`team_daily` / `team_weekly`），`target.team_id` 必须为本人所属小组；
- Director 写部门报告（`department_daily` / `department_weekly`），`target.department_id` 必须为本人管辖部门；
- Director 不写小组报告（`team_daily` / `team_weekly`）；TL 不写部门报告；
- Admin 可以全局写所有 report_type。

#### 3.5.3 `write_report_failure` 重构

按 `report_type` 校验 `period` 合法性后，仅更新 `ai_runs`（逻辑同现有 `writeReportFailure`，但移除 `validatePersonalDailyReportArgs` 硬编码）。同样接受 `target`，校验当前用户对目标 period + report_type 的写回权限（与 `write_report_result` 一致），但不写报告正文。

### 3.6 product_status 计算

将 `personalDailyProductStatus`（`daily_report_mcp.go:735`）泛化为 `computeProductStatus(report, lastRun)`：

```go
func computeProductStatus(report *reportSnapshot, lastRun *aiRunSnapshot) string {
  if report == nil {
    // 目标 period 无报告：检查最近一次对应 report_type 的 ai_run
    if lastRun != nil && lastRun.Status == "failed" {
      return "generation_failed"
    }
    return "missing"
  }
  if report.GenerationMode == "managed_agent" && !report.Edited { return "ai_generated" }
  if report.GenerationMode == "managed_agent" && report.Edited  { return "modified" }
  return "manual"
}
```

`reportSnapshot` 是 6 类报告的统一只读视图，由各 `loadXxxReport` 填充。

`lastRun` 由调用方按 `(report_type, period, target)` 查询 `ai_runs` 表中最近一条记录得到（`business_type = report_type`，`business_id` 匹配目标报告或为空，按 `created_at DESC LIMIT 1`）。`generation_failed` **不**从报告表自身判断——报告表无内容时无法知道上次 run 是否失败，必须查 `ai_runs`。

### 3.7 错误码标准化

新建 `api/handler/report_mcp_errors.go`：

```go
type mcpError struct {
  Code    string
  Message string
}
func (e *mcpError) Error() string { return e.Code + ": " + e.Message }

var (
  errUnauthorized        = &mcpError{"UNAUTHORIZED", "..."}
  errForbidden           = &mcpError{"FORBIDDEN", "..."}
  errReportTypeNotSupported = &mcpError{"REPORT_TYPE_NOT_SUPPORTED", "..."}
  errInvalidPeriod       = &mcpError{"INVALID_PERIOD", "..."}
  errInvalidScope       = &mcpError{"INVALID_SCOPE", "..."}
  errRunNotFound        = &mcpError{"RUN_NOT_FOUND", "..."}
  errRunForbidden       = &mcpError{"RUN_FORBIDDEN", "..."}
  errReportEditConflict = &mcpError{"REPORT_EDIT_CONFLICT", "Report has been modified by user"}
  errReportNotFound     = &mcpError{"REPORT_NOT_FOUND", "..."}
  errMCPInternal        = &mcpError{"MCP_INTERNAL_ERROR", "..."}
)
```

`serve()` 在 `err != nil` 时，若是 `*mcpError` 则按错误码返回结构化 JSON-RPC error（`{code, message}`），否则返回 `MCP_INTERNAL_ERROR`。

### 3.8 tools/list 输出

`reportMCPTools()` 输出 9 个原子工具的 schema，**且只输出这 9 个**：

```text
get_sessions
get_daily_reports
get_weekly_reports
get_tasks
get_requirements
get_existing_report
get_report_inventory
write_report_result
write_report_failure
```

`tools/list` 不返回 `get_report_context` / `aida_daily_report_get_context` / `aida_daily_report_save_draft`（旧 tool 全部删除）。每个工具的 `inputSchema` 按 §10 各小节定义生成，所有读取和写回工具的 `inputSchema` 均包含 `target` 字段。`report_type` 的 `enum` 包含全部 6 类。

### 3.9 Managed Agent 调用链对齐

`ManagedAgentHandler.StartDailyReportRun` 当前硬编码 `business_type="daily_report"`。需要：

1. 重命名为 `StartReportRun`，接受 `report_type` 参数；
2. 所有 `report_type` 统一使用 `/mcp/reports` 作为 MCP 入口（不保留 `/mcp/daily-report`）；
3. `business_type` 存 `report_type`（如 `"personal_daily"` / `"personal_weekly"` / `"team_daily"` 等），便于 `ai_runs` 与报告关联；
4. `input_ref_json` 增加 `report_type`、`period` 字段。

`refreshAIRun`（`managed_agent.go:1169`）：**只更新 `ai_runs.status`，不作为新 Report Agent 写回路径**。Agent 显式调用 `write_report_result` 写回报告。`refreshAIRun` 现有 personal_daily 自动写回逻辑（`run.BusinessType == "daily_report"` 分支）**删除**——开发期无兼容包袱，不再保留任何自动写回分支。新 report_type 的 Managed Agent run 完成后，由 Agent 通过 MCP 写回，`refreshAIRun` 只负责更新 `ai_runs.status`、`finished_at`、`error_message`。

### 3.10 前端对齐

- `web/src/features/aidashboard/api/types.ts`：给 `PersonalWeeklyReport / TeamReport / TeamWeeklyReport / DepartmentReport / DepartmentWeeklyReport` 接口追加 `product_status / origin / updated_by_user / generated_at / agent_run_id` 字段（与 `DailyReport` 已有字段对齐）。
- `ReportsPage.tsx` / `WeeklyReportsPage.tsx`：状态 Tag 渲染统一走 `product_status`（已部分完成于 `DailyReportGenerateModal.tsx`）。
- Managed Agent 报告运行入口（`AIAssetsPage.tsx`）：扩展为可选 `report_type`。

## 4. 实施分期

按需求文档 §15 分期，结合"开发期不保留旧兼容"口径调整：

### Phase 1：Report MCP 新入口和 9 工具骨架

- `/api/v1/mcp/reports` `tools/list` 只返回 9 个原子工具（§3.8）。
- 删除 `api/main.go` 中 `/api/v1/mcp/daily-report` 路由注册。
- 删除 `get_report_context` 正式能力（`getReportContext` / `reportGetContextTool` 常量 / `reportContextArgs` 等相关代码与测试）。
- 删除 `aida_daily_report_get_context` / `aida_daily_report_save_draft` tool 定义。
- `personal_daily` 迁移到原子工具链：Agent / Skill 调用 `get_existing_report` + `get_sessions` + `get_tasks` + `get_requirements` + `write_report_result` 组合（Agent 策略示例，非 MCP 固定流程）。
- `ReportMCPHandler` 主体替换 `DailyReportMCPHandler`，文件按 §3.1 拆分。
- 错误码标准化（§3.7）。
- `SessionHandler.List` 修复 PM 口径（§3.3）。
- 补 `generation_failed` 状态（§3.6）。

### Phase 2：原子读取工具实现

新建 7 个读取工具，按优先级：

1. `get_existing_report`（最简单，单点查询）
2. `get_sessions`（session 是日报核心来源）
3. `get_daily_reports`
4. `get_weekly_reports`
5. `get_tasks`
6. `get_requirements`
7. `get_report_inventory`（依赖前几个的查询能力）

每个工具配套单元测试（新建 `report_mcp_test.go`，不沿用旧 `daily_report_mcp_test.go` 的 personal_daily 专用测试）。

### Phase 3：6 类写回扩展

1. 数据库迁移（§3.2.1）。
2. Model 字段扩展（§3.2.2）。
3. `write_report_result` 按 `report_type` 分支（§3.5.1）。
4. 写回权限校验（§3.5.2）。
5. `write_report_failure` 去硬编码。

### Phase 4：Managed Agent / Skill 配置迁移

1. `StartReportRun`（原 `StartDailyReportRun`）支持 `report_type` 参数。
2. `refreshAIRun` 删除 personal_daily 自动写回分支，只更新 `ai_runs.status`。
3. `ai_runs.business_type` 用 `report_type` 值。
4. Agent 配置中 MCP URL 统一指向 `/api/v1/mcp/reports`，不引用 `/mcp/daily-report`。
5. personal_daily Agent / Skill 改为调用原子工具组合。

### Phase 5：前端和 E2E

1. 前端类型与状态渲染（§3.10）。
2. AI Assets 配置 6 类 Agent / Skill。
3. E2E 验证 personal_daily → team_daily → department_daily → weekly 全链路。

## 5. 风险与约束

1. **数据库迁移风险**：5 张表加列 + 外键，需在低峰期执行；`ADD COLUMN IF NOT EXISTS` + `DEFAULT 'default'` 保证向后兼容；外键用 `DO $$ ... IF NOT EXISTS ... END $$` 保证可重复执行。
2. **scope 收敛的 SQL 注入面**：所有 `user_ids` 必须走 `pq.Array` + 参数化，绝不拼字符串。
3. **跨人写回权限**：TL/Director 跨人写个人报告默认拒绝，需产品确认是否有"代写"场景（当前方案偏保守）。
4. **Managed Agent 写回路径**：所有 report_type 的 Agent 必须显式调 `write_report_result`，`refreshAIRun` 不再自动写回（personal_daily 自动写回分支删除）。需保证 Agent Skill 配置中包含写回步骤，否则 run 成功但报告不更新。
5. **旧入口删除的连带影响**：删除 `/mcp/daily-report` 后，任何引用该 endpoint 的 Agent 配置、文档、脚本需同步清理；开发期无外部调用方，风险可控。
6. **PM 口径修复**：`SessionHandler.List` 改 PM 为 self 后，需确认 PM 历史依赖该能力的页面（如团队 session 看 board）是否仍由 TL/Director 角色承担。

## 6. 验收清单（对应需求 §16）

- [ ] `/api/v1/mcp/daily-report` 路由不再注册，访问返回 404；
- [ ] `tools/list` 只返回 9 个原子工具，不返回 `get_report_context` / `aida_daily_report_get_context` / `aida_daily_report_save_draft`；
- [ ] 9 个工具按当前用户权限返回数据；
- [ ] 越权 scope 返回 `FORBIDDEN`；
- [ ] 越权 target 返回 `FORBIDDEN`；
- [ ] session 权限与日报/周报权限一致；
- [ ] Director 可读部门所有员工日报/周报/session；
- [ ] Admin 可读全局；
- [ ] `write_report_result` 正确写回 6 类报告；
- [ ] `write_report_failure` 不修改报告正文；
- [ ] 防覆盖规则在 6 类报告上均生效；
- [ ] `product_status` 5 种取值计算正确（含基于 `ai_runs` 的 `generation_failed`）；
- [ ] token 不出现在日志 / `input_ref_json`；
- [ ] personal_daily 通过新原子工具链跑通（不再依赖 `get_report_context`）；
- [ ] Agent 配置不引用 `/mcp/daily-report`；
- [ ] 报告弹窗能读取 Agent 写回内容。

## 7. 测试计划

### 7.1 MCP 协议与入口

- `POST /api/v1/mcp/daily-report` 返回 404（路由已删除）。
- `POST /api/v1/mcp/reports` `initialize` / `ping` / `tools/list` / `tools/call` 正常。
- `tools/list` 返回的 tool `name` 集合严格等于 9 个原子工具。
- `tools/list` 不包含 `get_report_context` / `aida_daily_report_get_context` / `aida_daily_report_save_draft`。

### 7.2 读取工具

每个工具按角色 × scope × target 矩阵覆盖：

- employee / PM：`scope=self` 只读自己；`scope=team/department/all` 返回 `FORBIDDEN`。
- TL：`scope=self/team`；`target.user_id` 为非小组成员返回 `FORBIDDEN`。
- Director：`scope=self/department`；`target.team_id` 越权返回 `FORBIDDEN`。
- Admin：全部 scope 可用；任意 target 合法。
- `get_sessions` 按 `date_range` 过滤；`include_summary` 返回聚合。
- `get_daily_reports` / `get_weekly_reports` 按 `report_scope` 分支。
- `get_existing_report` 按 `report_type + period + target` 直查；不存在返回 `{report: null, product_status: "missing" | "generation_failed"}`。
- `get_report_inventory` 返回 expected/existing/missing 三集合，summary 数量一致。

### 7.3 写回工具

- `write_report_result` 6 类 report_type 各覆盖一次正常写回。
- 防覆盖：已有报告 `edited=true && updated_at > run.created_at` → 返回 `REPORT_EDIT_CONFLICT`，`ai_runs.status=failed`，报告正文不变。
- TL 写小组成员个人日报 / 周报 → `FORBIDDEN`。
- Director 写部门员工个人日报 / 周报 → `FORBIDDEN`。
- Director 写小组报告 → `FORBIDDEN`。
- TL 写非所属小组报告 → `FORBIDDEN`。
- `write_report_failure` 只更新 `ai_runs`，不创建/修改报告正文。
- `run_id` 不存在 → `RUN_NOT_FOUND`；`run_id` 不属于当前用户 → `RUN_FORBIDDEN`。

### 7.4 product_status

- 无报告 + 最近 `ai_run.status=failed` → `generation_failed`。
- 无报告 + 无失败 run → `missing`。
- `generation_mode=managed_agent && edited=false` → `ai_generated`。
- `generation_mode=managed_agent && edited=true` → `modified`。
- 其他 → `manual`。

### 7.5 旧测试清理

- `api/handler/daily_report_mcp_test.go` 中针对 `getReportContext` / `aida_daily_report_get_context` / `aida_daily_report_save_draft` / `/mcp/daily-report` 的测试**删除或改写**为基于新原子工具的测试。
- 新测试统一放在 `api/handler/report_mcp_test.go`。

### 7.6 Agent 配置

- Agent 配置中 MCP URL 字段为 `/api/v1/mcp/reports`，不出现 `/mcp/daily-report`。
- personal_daily Agent / Skill 的 prompt 中调用的是 `get_existing_report` / `get_sessions` / `get_tasks` / `get_requirements` / `write_report_result`，不调用 `get_report_context`。
