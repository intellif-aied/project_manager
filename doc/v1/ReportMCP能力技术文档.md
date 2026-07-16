# Report MCP 能力技术文档

## 1. 文档目的

本文档定义 Aida Report MCP 的能力边界、工具集合、权限模型、参数约定、返回结构和写回规则。

Report MCP 的定位是：

> Aida 提供给 Agent 的报告数据能力层。它不负责决定报告怎么写，只提供受权限控制的数据读取能力和报告写回能力。

## 2. 核心边界

### 2.1 MCP 负责什么

Report MCP 负责：

1. 校验调用者身份；
2. 按当前用户权限提供受控数据读取能力；
3. 提供 session、日报、周报、任务、需求、已有报告等结构化数据；
4. 接收 Agent 生成结果并写回对应报告事实源；
5. 接收 Agent 失败结果并记录；
6. 保证数据范围、权限边界、写回目标正确；
7. 保证 token、用户身份、run_id、report_type 的安全约束。

### 2.2 MCP 不负责什么

Report MCP 不负责：

1. 决定报告应该读取哪些材料；
2. 决定日报 / 周报 / 小组 / 部门报告的来源优先级；
3. 决定报告结构；
4. 决定报告语气；
5. 决定内容取舍；
6. 生成最终报告文本；
7. 管理 Agent prompt / skill；
8. 管理 Agent 定时任务；
9. 管理 AI Assets 页面交互。

以上属于 Agent / Skill / Prompt / AI Assets 的职责。

## 3. 整体架构关系

```text
AI Assets
  - 配置 Agent
  - 运行 Agent
  - 定时运行 Agent
  - 查看运行记录

Agent / Skill
  - 决定报告生成策略
  - 决定调用哪些 MCP 能力
  - 决定如何组织报告内容
  - 调用 MCP 写回结果

Report MCP
  - 提供受控数据读取能力
  - 提供报告写回能力
  - 提供失败记录能力

报告弹窗
  - 查看报告产物
  - 编辑报告产物
  - 保存报告产物
```

## 4. report_type 定义

Report MCP 支持 6 类报告写回目标：

| report_type       | 中文名称 | 写回目标      |
| ----------------- | ---- | --------- |
| personal_daily    | 个人日报 | 某用户某日个人日报 |
| personal_weekly   | 个人周报 | 某用户某周个人周报 |
| team_daily        | 小组日报 | 某小组某日小组日报 |
| team_weekly       | 小组周报 | 某小组某周小组周报 |
| department_daily  | 部门日报 | 某部门某日部门日报 |
| department_weekly | 部门周报 | 某部门某周部门周报 |

### 4.1 report_type 的职责

`report_type` 只负责：

1. 表示写回哪类报告；
2. 校验当前用户是否有该类报告的写回权限；
3. 决定报告事实源；
4. 决定报告状态归属；
5. 决定 `business_id` 关联目标。

### 4.2 report_type 不负责

`report_type` 不负责自动决定：

1. 要不要读取 session；
2. 要不要读取日报；
3. 要不要读取周报；
4. 要不要读取任务；
5. 来源优先级；
6. 报告输出结构；
7. 汇总策略。

这些由 Agent / Skill 决定。

## 5. MCP Endpoint

统一使用：

```http
POST /api/v1/mcp/reports
```

协议使用 JSON-RPC MCP 形式。

旧接口如：

```http
POST /api/v1/mcp/daily-report
```

可作为兼容入口，但新能力统一收敛到：

```http
POST /api/v1/mcp/reports
```

## 6. 工具能力总览

Report MCP 提供两类工具。

### 6.1 数据读取类工具

| Tool                 | 作用                   |
| -------------------- | -------------------- |
| get_sessions         | 获取权限范围内日期范围的 session |
| get_daily_reports    | 获取权限范围内日期范围的日报       |
| get_weekly_reports   | 获取权限范围内周范围的周报        |
| get_tasks            | 获取权限范围内任务数据          |
| get_requirements     | 获取权限范围内需求数据          |
| get_existing_report  | 获取目标报告已有内容           |
| get_report_inventory | 获取某范围内报告完整度 / 缺失情况   |

### 6.2 报告写回类工具

| Tool                 | 作用            |
| -------------------- | ------------- |
| write_report_result  | 写回 Agent 生成结果 |
| write_report_failure | 记录 Agent 生成失败 |

## 7. 身份与权限模型

### 7.1 调用身份

Agent 调用 MCP 时必须使用当前 Aida 用户身份。

推荐鉴权链路：

```text
AIDA_REPORT_MCP_AUTH credential slot
→ 当前用户 token
→ Aida AuthMiddleware
→ 当前用户 user_id / role / team_id / department scope
```

禁止：

1. 使用平台管理员 token 代替用户身份；
2. 使用全局固定 token；
3. 让 Agent 绕过当前用户权限；
4. 前端传明文 token；
5. 在 input_ref_json、日志、运行记录中保存完整 token。

### 7.2 日报 / 周报 / session 权限规则

日报、周报、session 读取能力统一遵循以下权限模型：

| 用户角色     | 可读写能力                                                       |
| -------- | ----------------------------------------------------------- |
| employee | 自己的个人日报 / 个人周报 / session                                    |
| PM       | 自己的个人日报 / 个人周报 / session                                    |
| TL       | 自己的个人日报 / 周报 / session；所属小组成员的日报 / 周报 / session；所属小组日报 / 周报 |
| Director | 自己的个人日报 / 周报 / session；部门日报 / 周报；部门所有员工的日报 / 周报 / session   |
| Admin    | 所有人的日报 / 周报 / session；所有小组 / 部门报告                           |

### 7.3 PM 口径

PM 没有默认小组管理权限。

PM 在 MCP 权限上等同“个人用户”：

```text
PM 可读取 / 写回自己的个人日报、个人周报、session。
```

如果部门报告 Agent 需要汇总 PM，应该由 Director 或 Admin 身份读取部门范围数据，而不是由 PM 自己获得跨范围权限。

### 7.4 TL 口径

TL 拥有：

1. 自己的个人日报 / 周报 / session；
2. 所属小组成员的个人日报 / 周报 / session；
3. 所属小组的小组日报 / 小组周报。

TL 不拥有部门级报告权限。

### 7.5 Director 口径

Director 拥有：

1. 自己的个人日报 / 周报 / session；
2. 部门日报 / 部门周报；
3. 部门范围内所有员工的日报 / 周报 / session。

Director 读取部门范围数据，是为了生成部门日报 / 部门周报。

### 7.6 Admin 口径

Admin 拥有全局报告与 session 权限：

1. 所有员工的日报 / 周报 / session；
2. 所有小组日报 / 周报；
3. 所有部门日报 / 周报。

Admin 权限用于平台管理、联调和兜底，不应被 Agent 默认使用为普通用户身份。

## 8. scope 模型

读取类工具可以接受 `scope`，但最终范围必须由后端根据当前用户权限收敛。

```json
{
  "scope": {
    "type": "self | team | department | all",
    "team_id": "optional",
    "department_id": "optional",
    "user_ids": ["optional"]
  }
}
```

### 8.1 scope.type 说明

| scope.type | 含义                 |
| ---------- | ------------------ |
| self       | 当前用户自己             |
| team       | 当前 TL 所属小组         |
| department | 当前 Director 可见部门范围 |
| all        | Admin 全局范围         |

### 8.2 scope 收敛规则

MCP 必须按照当前用户身份收敛 scope：

| 当前角色     | 允许 scope                    |
| -------- | --------------------------- |
| employee | self                        |
| PM       | self                        |
| TL       | self, team                  |
| Director | self, department            |
| Admin    | self, team, department, all |

如果 Agent 传入越权 scope：

1. 可以直接返回 `FORBIDDEN`；
2. 或按当前用户权限自动收敛；
3. 不允许扩大权限。

建议实现上优先返回明确错误，避免 Agent 误以为已获取完整数据。

## 9. 时间参数约定

### 9.1 date_range

用于 session、日报查询：

```json
{
  "date_range": {
    "start": "YYYY-MM-DD",
    "end": "YYYY-MM-DD"
  }
}
```

### 9.2 week_range

用于周报查询：

```json
{
  "week_range": {
    "week_start": "YYYY-MM-DD",
    "week_end": "YYYY-MM-DD"
  }
}
```

### 9.3 period

用于报告写回。

日报类：

```json
{
  "period": {
    "date": "YYYY-MM-DD"
  }
}
```

周报类：

```json
{
  "period": {
    "week_start": "YYYY-MM-DD",
    "week_end": "YYYY-MM-DD"
  }
}
```

## 10. Tool 设计

## 10.1 get_sessions

### 作用

获取当前用户权限范围内指定日期范围的 session 数据。

### 请求参数

```json
{
  "scope": {
    "type": "self | team | department | all"
  },
  "date_range": {
    "start": "YYYY-MM-DD",
    "end": "YYYY-MM-DD"
  },
  "user_ids": ["optional"],
  "limit": 100,
  "include_summary": true
}
```

### 参数说明

* `scope.type=self`：读取当前用户 session；
* `scope.type=team`：读取 TL 所属小组成员 session；
* `scope.type=department`：读取 Director 部门范围内所有员工 session；
* `scope.type=all`：Admin 读取全局 session；
* `user_ids` 只能用于进一步缩小范围，不能扩大范围；
* `limit` 用于控制返回数量，避免 Agent 一次拉取过多 session；
* `include_summary` 控制是否返回聚合摘要。

### 返回结构

```json
{
  "sessions": [
    {
      "id": "session_id",
      "user_id": 307,
      "username": "t05",
      "role": "employee",
      "team_id": "team-a",
      "session_ref": "p0-report-mcp-test-1782749025",
      "started_at": "2026-06-29T09:00:00+08:00",
      "ended_at": "2026-06-29T10:00:00+08:00",
      "date": "2026-06-29",
      "summary": "完成 Report MCP 验证",
      "tags": [],
      "task_refs": [],
      "requirement_refs": []
    }
  ],
  "summary": {
    "total": 1,
    "by_date": [
      {
        "date": "2026-06-29",
        "count": 1
      }
    ],
    "by_user": [
      {
        "user_id": 307,
        "username": "t05",
        "count": 1
      }
    ],
    "truncated": false
  }
}
```

### 权限要求

| 角色       | get_sessions 能力     |
| -------- | ------------------- |
| employee | 自己的 session         |
| PM       | 自己的 session         |
| TL       | 自己 + 所属小组成员 session |
| Director | 自己 + 部门所有员工 session |
| Admin    | 所有人 session         |

## 10.2 get_daily_reports

### 作用

获取权限范围内指定日期范围的日报。

### 请求参数

```json
{
  "scope": {
    "type": "self | team | department | all"
  },
  "date_range": {
    "start": "YYYY-MM-DD",
    "end": "YYYY-MM-DD"
  },
  "report_scope": "personal | team | department",
  "user_ids": ["optional"],
  "include_content": true
}
```

### 参数说明

* `scope.type`：权限范围；
* `report_scope`：日报事实源类型；

  * `personal`：个人日报；
  * `team`：小组日报；
  * `department`：部门日报；
* `user_ids` 只能进一步缩小个人日报查询范围；
* `include_content` 控制是否返回正文。

### 返回结构

```json
{
  "reports": [
    {
      "id": "report_id",
      "report_scope": "personal",
      "date": "2026-06-29",
      "owner": {
        "user_id": 307,
        "username": "t05",
        "role": "employee",
        "team_id": "team-a"
      },
      "content": "日报正文",
      "product_status": "ai_generated",
      "generation_mode": "managed_agent",
      "edited": false,
      "managed_agent_run_id": "run_id",
      "updated_at": "2026-06-29T18:00:00+08:00"
    }
  ],
  "missing": [
    {
      "date": "2026-06-30",
      "owner_type": "user",
      "owner_id": 308,
      "username": "t06",
      "reason": "missing_report"
    }
  ],
  "summary": {
    "total_expected": 5,
    "total_existing": 4,
    "total_missing": 1
  }
}
```

### 权限要求

| 角色       | personal 日报 | team 日报       | department 日报 |
| -------- | ----------- | ------------- | ------------- |
| employee | 自己          | 不允许           | 不允许           |
| PM       | 自己          | 不允许           | 不允许           |
| TL       | 自己 + 小组成员   | 所属小组          | 不允许           |
| Director | 自己 + 部门所有员工 | 部门范围内小组，如系统支持 | 部门            |
| Admin    | 所有人         | 所有小组          | 所有部门          |

## 10.3 get_weekly_reports

### 作用

获取权限范围内指定周范围的周报。

### 请求参数

```json
{
  "scope": {
    "type": "self | team | department | all"
  },
  "week_range": {
    "week_start": "YYYY-MM-DD",
    "week_end": "YYYY-MM-DD"
  },
  "report_scope": "personal | team | department",
  "user_ids": ["optional"],
  "include_content": true
}
```

### 返回结构

```json
{
  "reports": [
    {
      "id": "weekly_report_id",
      "report_scope": "personal",
      "week_start": "2026-06-29",
      "week_end": "2026-07-05",
      "owner": {
        "user_id": 307,
        "username": "t05",
        "role": "employee",
        "team_id": "team-a"
      },
      "content": "周报正文",
      "product_status": "modified",
      "generation_mode": "managed_agent",
      "edited": true,
      "managed_agent_run_id": "run_id",
      "updated_at": "2026-07-05T18:00:00+08:00"
    }
  ],
  "missing": [],
  "summary": {
    "total_expected": 1,
    "total_existing": 1,
    "total_missing": 0
  }
}
```

### 权限要求

| 角色       | personal 周报 | team 周报       | department 周报 |
| -------- | ----------- | ------------- | ------------- |
| employee | 自己          | 不允许           | 不允许           |
| PM       | 自己          | 不允许           | 不允许           |
| TL       | 自己 + 小组成员   | 所属小组          | 不允许           |
| Director | 自己 + 部门所有员工 | 部门范围内小组，如系统支持 | 部门            |
| Admin    | 所有人         | 所有小组          | 所有部门          |

## 10.4 get_tasks

### 作用

获取当前用户权限范围内任务数据。

### 请求参数

```json
{
  "scope": {
    "type": "self | team | department | all"
  },
  "date_range": {
    "start": "YYYY-MM-DD",
    "end": "YYYY-MM-DD"
  },
  "status": ["todo", "in_progress", "done", "blocked"],
  "include_requirement": true
}
```

### 返回结构

```json
{
  "tasks": [
    {
      "id": "task_id",
      "title": "任务标题",
      "status": "in_progress",
      "progress": 70,
      "assignee": {
        "user_id": 307,
        "username": "t05"
      },
      "requirement": {
        "id": "requirement_id",
        "title": "需求标题"
      },
      "risk_types": [],
      "blocked": false,
      "updated_at": "2026-06-29T17:00:00+08:00"
    }
  ],
  "summary": {
    "total": 1,
    "blocked": 0,
    "done": 0,
    "in_progress": 1
  }
}
```

### 权限要求

任务权限沿用当前需求 / 任务模块已有权限规则。

Report MCP 不重新定义任务权限，只通过现有权限能力读取当前用户可见任务。

## 10.5 get_requirements

### 作用

获取当前用户权限范围内需求数据。

### 请求参数

```json
{
  "scope": {
    "type": "self | team | department | all"
  },
  "date_range": {
    "start": "YYYY-MM-DD",
    "end": "YYYY-MM-DD"
  },
  "include_tasks": false,
  "include_risks": true
}
```

### 返回结构

```json
{
  "requirements": [
    {
      "id": "requirement_id",
      "title": "需求标题",
      "status": "in_progress",
      "owner": {
        "user_id": 100,
        "username": "pm01"
      },
      "teams": ["team-a"],
      "risks": [],
      "updated_at": "2026-06-29T17:00:00+08:00"
    }
  ],
  "summary": {
    "total": 1,
    "risk_count": 0
  }
}
```

### 权限要求

需求权限沿用当前需求模块已有权限规则。

Report MCP 不额外扩大需求可见范围。

## 10.6 get_existing_report

### 作用

获取目标报告当前已有内容。

### 请求参数

日报：

```json
{
  "report_type": "personal_daily",
  "period": {
    "date": "YYYY-MM-DD"
  }
}
```

周报：

```json
{
  "report_type": "personal_weekly",
  "period": {
    "week_start": "YYYY-MM-DD",
    "week_end": "YYYY-MM-DD"
  }
}
```

### 返回结构

存在报告：

```json
{
  "report": {
    "id": "report_id",
    "report_type": "personal_daily",
    "period": {
      "date": "2026-06-29"
    },
    "content": "已有报告正文",
    "product_status": "modified",
    "generation_mode": "managed_agent",
    "edited": true,
    "managed_agent_run_id": "run_id",
    "updated_at": "2026-06-29T18:30:00+08:00"
  }
}
```

不存在报告：

```json
{
  "report": null,
  "product_status": "missing"
}
```

### 权限要求

根据 `report_type` 校验当前用户是否有读取目标报告的权限。

示例：

| report_type       | employee / PM | TL        | Director      | Admin |
| ----------------- | ------------- | --------- | ------------- | ----- |
| personal_daily    | 仅自己           | 自己 + 小组成员 | 自己 + 部门员工     | 所有人   |
| personal_weekly   | 仅自己           | 自己 + 小组成员 | 自己 + 部门员工     | 所有人   |
| team_daily        | 不允许           | 所属小组      | 部门范围内小组，如系统支持 | 所有小组  |
| team_weekly       | 不允许           | 所属小组      | 部门范围内小组，如系统支持 | 所有小组  |
| department_daily  | 不允许           | 不允许       | 部门            | 所有部门  |
| department_weekly | 不允许           | 不允许       | 部门            | 所有部门  |

## 10.7 get_report_inventory

### 作用

获取某一范围内报告完整度，用于 Agent 判断来源缺失情况。

### 请求参数

```json
{
  "scope": {
    "type": "team"
  },
  "report_scope": "personal",
  "report_kind": "daily",
  "date_range": {
    "start": "YYYY-MM-DD",
    "end": "YYYY-MM-DD"
  }
}
```

### 返回结构

```json
{
  "inventory": {
    "expected": [
      {
        "owner_type": "user",
        "owner_id": 307,
        "username": "t05",
        "dates": ["2026-06-29", "2026-06-30"]
      }
    ],
    "existing": [
      {
        "owner_type": "user",
        "owner_id": 307,
        "date": "2026-06-29",
        "report_id": "report_id",
        "product_status": "ai_generated"
      }
    ],
    "missing": [
      {
        "owner_type": "user",
        "owner_id": 307,
        "username": "t05",
        "date": "2026-06-30"
      }
    ]
  },
  "summary": {
    "total_expected": 2,
    "total_existing": 1,
    "total_missing": 1
  }
}
```

### 产品口径

`get_report_inventory` 只提供完整度，不决定是否阻断生成。

是否因为缺失来源而生成、降级生成、标记风险，由 Agent / Skill 决定。

## 10.8 write_report_result

### 作用

写回 Agent 生成的报告内容。

### 请求参数

日报：

```json
{
  "report_type": "personal_daily",
  "period": {
    "date": "YYYY-MM-DD"
  },
  "run_id": "ai_run_id",
  "content": "报告正文",
  "summary": "可选摘要",
  "source_refs": [
    {
      "type": "session",
      "id": "session_id"
    }
  ],
  "metadata": {
    "agent_output_version": "v1"
  }
}
```

周报：

```json
{
  "report_type": "personal_weekly",
  "period": {
    "week_start": "YYYY-MM-DD",
    "week_end": "YYYY-MM-DD"
  },
  "run_id": "ai_run_id",
  "content": "周报正文"
}
```

### 写回规则

1. 根据 `report_type + period + 当前用户权限` 定位写回目标；
2. `run_id` 必须存在且属于当前用户本次可写运行；
3. 写回后报告状态为：

   * `generation_mode=managed_agent`
   * `edited=false`
   * `managed_agent_run_id=run_id`
   * `product_status=ai_generated`
4. `ai_runs.status=succeeded`；
5. `ai_runs.business_id=report_id`；
6. 如果报告已被用户编辑，需要防覆盖。

### 写回权限要求

| report_type       | employee | PM  | TL                              | Director                 | Admin |
| ----------------- | -------- | --- | ------------------------------- | ------------------------ | ----- |
| personal_daily    | 自己       | 自己  | 自己；小组成员如作为 TL 生成汇总来源时不直接写成员个人报告 | 自己；部门员工如有明确管理写权限才允许，否则只读 | 所有人   |
| personal_weekly   | 自己       | 自己  | 自己；小组成员如有明确管理写权限才允许，否则只读        | 自己；部门员工如有明确管理写权限才允许，否则只读 | 所有人   |
| team_daily        | 不允许      | 不允许 | 所属小组                            | 部门范围内小组如系统允许             | 所有小组  |
| team_weekly       | 不允许      | 不允许 | 所属小组                            | 部门范围内小组如系统允许             | 所有小组  |
| department_daily  | 不允许      | 不允许 | 不允许                             | 部门                       | 所有部门  |
| department_weekly | 不允许      | 不允许 | 不允许                             | 部门                       | 所有部门  |

重要说明：

* 读取权限和写回权限不能简单等同；
* Director 可读取部门所有员工日报 / 周报 / session，但不一定默认可以覆盖员工个人日报 / 周报；
* TL 可读取小组成员日报 / 周报 / session，但不一定默认可以覆盖成员个人日报 / 周报；
* 跨人写个人报告应谨慎，除非产品明确允许；
* Admin 拥有全局写回能力。

### 防覆盖规则

如果已有报告满足：

```text
edited=true
且 updated_at > ai_runs.created_at
```

则拒绝覆盖，返回：

```json
{
  "error": {
    "code": "REPORT_EDIT_CONFLICT",
    "message": "Report has been modified by user"
  }
}
```

同时：

1. 不覆盖原报告正文；
2. `ai_runs.status=failed`；
3. `ai_runs.error_message` 记录冲突原因；
4. `ai_runs.finished_at` 有值。

### 返回结构

```json
{
  "status": "saved",
  "report_type": "personal_daily",
  "report_id": "report_id",
  "agent_run_id": "ai_run_id",
  "managed_agent_run_id": "ai_run_id",
  "product_status": "ai_generated",
  "origin": "ai",
  "updated_by_user": false
}
```

## 10.9 write_report_failure

### 作用

记录 Agent 生成失败。

### 请求参数

```json
{
  "report_type": "personal_daily",
  "period": {
    "date": "YYYY-MM-DD"
  },
  "run_id": "ai_run_id",
  "error_code": "MODEL_ERROR",
  "error_message": "生成失败"
}
```

### 写入规则

1. 只更新 `ai_runs`；
2. 不创建空报告；
3. 不修改已有报告正文；
4. 不覆盖用户内容；
5. `ai_runs.status=failed`；
6. `ai_runs.error_message` 记录失败原因；
7. `ai_runs.finished_at` 有值。

### 返回结构

```json
{
  "status": "recorded",
  "run_id": "ai_run_id",
  "report_type": "personal_daily"
}
```

## 11. product_status 计算规则

MCP 可在返回报告时输出计算态 `product_status`。

| 条件                                           | product_status    |
| -------------------------------------------- | ----------------- |
| 不存在报告                                        | missing           |
| generation_mode=managed_agent 且 edited=false | ai_generated      |
| generation_mode=managed_agent 且 edited=true  | modified          |
| generation_mode!=managed_agent 或手写保存         | manual            |
| 生成失败且无报告                                     | generation_failed |

注意：

`product_status` 是产品计算态，不一定替代数据库旧 `status` 字段。

## 12. 错误码

| 错误码                       | 说明               |
| ------------------------- | ---------------- |
| UNAUTHORIZED              | 未登录或 token 无效    |
| FORBIDDEN                 | 当前用户无权限访问该范围     |
| REPORT_TYPE_NOT_SUPPORTED | report_type 暂不支持 |
| INVALID_PERIOD            | period 参数错误      |
| INVALID_SCOPE             | scope 参数错误       |
| RUN_NOT_FOUND             | run_id 不存在       |
| RUN_FORBIDDEN             | run_id 不属于当前用户   |
| REPORT_EDIT_CONFLICT      | 报告已被用户编辑，拒绝覆盖    |
| REPORT_NOT_FOUND          | 目标报告不存在          |
| MCP_INTERNAL_ERROR        | MCP 内部错误         |

## 13. 安全要求

1. MCP 必须通过 Aida AuthMiddleware 识别当前用户；
2. 不允许管理员 token 作为普通 Agent 通用 token；
3. 不允许跨用户读取个人日报 / 周报 / session；
4. 不允许跨小组读取小组报告；
5. 不允许跨部门读取部门报告；
6. 不允许在日志中打印完整 token；
7. 不允许在 `input_ref_json` 中保存完整 token；
8. 失败信息不应暴露敏感凭据；
9. Agent 传入的 scope 只能收敛，不能扩大权限；
10. 写回必须绑定 `run_id`；
11. 读取权限和写回权限必须分别校验；
12. Director / TL 的跨人读取权限不应自动等价为跨人写回权限。

## 14. Agent / Skill 使用方式示例

### 14.1 个人周报 Agent 示例策略

Agent / Skill 可以自行决定：

```text
1. 调 get_daily_reports 读取本周个人日报；
2. 调 get_sessions 读取本周 session 作为补充；
3. 调 get_tasks 读取本周任务推进；
4. 调 get_existing_report 查看已有周报；
5. 生成周报；
6. 调 write_report_result 写回 personal_weekly。
```

MCP 不内置这些步骤。

### 14.2 小组日报 Agent 示例策略

```text
1. 调 get_daily_reports 读取小组成员当天个人日报；
2. 调 get_report_inventory 查看缺失成员；
3. 调 get_tasks 获取小组任务；
4. 生成小组日报；
5. 调 write_report_result 写回 team_daily。
```

### 14.3 部门周报 Agent 示例策略

```text
1. 调 get_weekly_reports 读取小组周报；
2. 调 get_daily_reports 读取部门日报；
3. 调 get_weekly_reports 读取部门员工个人周报；
4. 调 get_requirements 获取部门重点需求；
5. 生成部门周报；
6. 调 write_report_result 写回 department_weekly。
```

这些是 Agent 配置策略，不是 MCP 固定流程。

## 15. 实施分期建议

### Phase 1：稳定 personal_daily

已完成或接近完成：

1. personal_daily 写回；
2. get_sessions 基础能力；
3. get_existing_report；
4. write_report_result；
5. write_report_failure；
6. Agent E2E。

### Phase 2：补齐原子读取能力

新增 / 完善：

1. get_daily_reports；
2. get_weekly_reports；
3. get_tasks；
4. get_requirements；
5. get_report_inventory。

### Phase 3：支持 6 类写回

扩展 `write_report_result` / `write_report_failure` 支持：

1. personal_weekly；
2. team_daily；
3. team_weekly；
4. department_daily；
5. department_weekly。

### Phase 4：Agent 配置与 Skill

为不同 report_type 配置不同 Agent / Skill：

1. 个人日报 Agent；
2. 个人周报 Agent；
3. 小组日报 Agent；
4. 小组周报 Agent；
5. 部门日报 Agent；
6. 部门周报 Agent。

### Phase 5：定时任务

AI Assets / 定时任务负责触发 Agent，不由报告弹窗触发。

## 16. 验收标准

### 16.1 MCP 能力验收

1. 所有读取工具都按当前用户权限返回数据；
2. Agent 无法越权读取其他用户 / 小组 / 部门数据；
3. session 权限与日报 / 周报权限范围一致；
4. Director 可读取部门所有员工日报 / 周报 / session；
5. Admin 可读取全局日报 / 周报 / session；
6. write_report_result 能正确写回 6 类报告；
7. write_report_failure 不修改报告正文；
8. 防覆盖规则生效；
9. product_status 计算正确；
10. token 不泄露；
11. 旧报告弹窗能读取 Agent 写回内容。

### 16.2 产品边界验收

1. 报告弹窗不触发 Agent；
2. AI Assets 是 Agent 运行入口；
3. MCP 不决定报告生成策略；
4. Agent / Skill 决定材料选择和报告结构；
5. report_type 只决定写回目标和权限校验；
6. 读取权限和写回权限分开校验。
