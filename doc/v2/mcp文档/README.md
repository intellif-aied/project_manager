# 报告生成 MCP 基准文档

> 文档类型：当前代码契约基准
> 建立日期：2026-07-18
> 适用入口：`/api/v1/mcp/reports`
> 当前开发候选：`report-context/v1`，仅在 14.157 开发测试环境验收，未发布生产

## 1. 文档职责

本文档是报告生成 MCP 的统一契约入口，回答以下问题：

- MCP 当前暴露哪些工具；
- 默认个人报告实际使用哪些工具；
- `get_report_context` 返回哪些数据；
- 哪些数据不属于 Context V1；
- 权限、冻结、完整性和写回边界是什么。

以后任何 Agent 优化如果新增、删除、重命名 MCP 工具，修改入参、返回值、路由、权限、完整性或写回语义，必须在同一次代码变更中同步更新本文档。未更新本文档的 MCP 改动不算完成。

## 2. 当前 MCP 工具清单

| 工具 | 职责 | 当前路由 |
| --- | --- | --- |
| `get_report_context` | 按 `run_id` 读取服务端冻结的个人报告 Context | Context V1 主路径 |
| `write_report_result` | 写回最终报告 | 所有报告共用 |
| `write_report_failure` | 写回可识别的生成失败 | 失败路径 |
| `get_sessions` | 读取 Session/历史报告来源 | 兼容旧路由；Context V1 个人报告禁止调用 |
| `get_daily_reports` | 读取日报 | 团队/部门等兼容路由 |
| `get_weekly_reports` | 读取周报 | 团队/部门等兼容路由 |
| `get_tasks` | 读取任务 | 兼容旧路由 |
| `get_requirements` | 读取需求 | 兼容旧路由 |
| `get_existing_report` | 读取已有报告 | 编辑/兼容路由 |
| `get_report_inventory` | 读取报告清单 | 团队/部门等兼容路由 |

注意：MCP 服务当前仍暴露 10 个工具，不是只剩 2 个。对存在冻结 Selection 的默认个人日报/周报，正常成功路径只使用：

```text
get_report_context(run_id) -> Agent 语义归并与表达 -> write_report_result(...)
```

## 3. Context V1 的创建时序

1. 前端提交报告类型、周期、目标和可选的 `report_source_selection_id`。
2. 用户手动选择 Session 时，服务端使用 `explicit selection`。
3. 用户未选择 Session 时，服务端自动创建 `default selection`。
4. `default selection` 选择活动时间与报告周期存在交集的完整 Slice，日期按 `Asia/Shanghai` 判定。
5. 服务端校验 Slice 可用性和 Digest 完整性，冻结 Selection Digest。
6. 创建 Report Run，生成 `report-context/v1` 并写入 `report_run_contexts`。
7. Agent 只传 `run_id` 调用一次 `get_report_context`。
8. Agent 生成报告后调用 `write_report_result`写回。

与报告日期有交集的跨天 Slice 会整个进入 Selection，不会在 Slice 中间按日期裁断。

## 4. `get_report_context` 调用契约

请求参数：

```json
{
  "run_id": "<report-run-id>"
}
```

权限规则：

- 必须存在登录用户；
- Run 必须属于当前用户；
- Run 的 `business_type` 必须为 `report_agent_run`；
- 只能读取与该 Run 绑定的唯一冻结 Context。

MCP 返回标准 text content，text 内容是下面的完整 JSON：

```json
{
  "schema_version": "report-context/v1",
  "run": {
    "run_id": "<report-run-id>",
    "report_type": "personal_daily",
    "period": {
      "start": "2026-07-16",
      "end": "2026-07-16"
    },
    "target": {
      "type": "self"
    }
  },
  "source_state": {
    "mode": "digest_v2",
    "coverage_complete": true
  },
  "sources": {
    "session_digest": {}
  }
}
```

## 5. Context 字段构成

### 5.1 `run`

- `run_id`：本次报告 Run；
- `report_type`：当前 V1 覆盖 `personal_daily` 和 `personal_weekly`；
- `period`：报告开始、结束日期；
- `target`：报告对象，个人报告通常为 `self`。

### 5.2 `source_state`

- `mode`：当前为 `digest_v1` 或 `digest_v2`；
- `coverage_complete`：Context Builder 已完整读取服务端冻结的 Selection Digest，不存在未翻完的分页。

`coverage_complete=true` 不表示原始 Session 的每个字符都进入 Digest；它表示当前冻结 Digest 被 Context Builder 完整接收。

### 5.3 `sources.session_digest`

该字段保存 Selection 的原样冻结 Digest payload，Context Builder 不再做二次摘要、Top-K 或事实删减。当前 Digest V2 包含：

- `source_mode`：`explicit` 或 `default`；
- `content_mode`、`timezone`、`digest_version`、`redaction_version`；
- `content_snapshot_at`、`completeness`、`returned_item_count`、`has_more`；
- `coverage`：来源项、代表项、事件数、省略数和截断状态；
- `budget`：目标大小、硬上限、实际大小和压缩档位；
- `report_period_summary`：本次报告周期的统一时序成果视图；
- `items`：每个 Session Slice 的活动时间、Digest hash、coverage 和结构化 Digest。

`report_period_summary` 包含工作单元、有结果工作单元、主要成果、验证成果、变更、验证、未解决事项、状态统计和每日 highlights。

每个 Slice Digest 的 `work_units` 可包含：

- 目标、类别、时间和报告周期关系；
- 完成、部分完成、阻塞、失败等状态；
- 结果陈述和 Agent claim；
- 证据摘要、变更、验证和未解决事项；
- 证据引用和 coverage 信息。

## 6. Context V1 明确不包含的数据

当前 V1 不包含：

- 需求列表和任务列表；
- 关注事项和组织上下文；
- 历史日报/周报正文；
- 原始 Session JSONL；
- 原始 MCP 大结果、图片和二进制内容；
- Agent 在运行中自由扫描出的其他业务数据。

`source_selection_id`、Context SHA-256 和 Context bytes 保存在服务端数据库/Run 元数据中，不重复放入 MCP Context 正文。

## 7. 持久化与可追溯边界

Context V1 使用表 `report_run_contexts`，每个 Report Run 保存一条轻量 Context 记录：

- `run_id`；
- `schema_version`；
- `source_selection_id`；
- `context_hash`；
- `context_payload`；
- `context_bytes`；
- 创建时间。

该设计不另存一份原始 Session，不引入对象存储、Evidence Artifact 或多版本 Context 快照。

## 8. 当前边界与验收标准

- Context V1 只覆盖存在冻结 Selection 的个人日报/周报；
- 团队、部门和兼容报告仍可使用旧读取工具；
- Context Builder 只做确定性组装与完整性校验，不代替 Agent 做语义归并；
- Agent 负责成果识别、跨 Session 归并、状态理解和最终表达；
- Context 完整性不能替代 `write_report_result` 写回成功校验。

14.157 开发测试环境的 B01-B10 验收结果为：10/10 成功，每个 Run 均为一次 `get_report_context`、零次 `get_sessions`、一次 `write_report_result`。

## 9. 强制更新检查清单

以后变更报告 MCP 时，必须同步核对：

1. 工具名称和数量；
2. input schema 和必填字段；
3. 返回 JSON 字段及语义；
4. 个人、团队、部门的工具路由；
5. 认证、Run owner 和数据权限；
6. Selection 冻结、完整性、分页和大上下文协议；
7. Agent Prompt、运行消息和 Report Skill 是否与契约一致；
8. `write_report_result` / `write_report_failure` 的终态语义；
9. MCP tools/list 和真实 Agent 调用测试；
10. 本文档的版本记录。

## 10. 版本记录

| 日期 | 契约版本 | 变更 |
| --- | --- | --- |
| 2026-07-18 | `report-context/v1` | 建立 MCP 基准文档；记录 Context V1、10 个工具和个人报告两工具成功路径。 |

## 11. 代码真实来源

- MCP 入口与调度：`api/handler/report_mcp.go`；
- MCP tools/list schema：`api/handler/report_mcp_tools.go`；
- Context 构建与读取：`api/internal/reportcontext/service.go`；
- Selection 创建与冻结：`api/internal/reportsource/service.go`；
- Digest V2 冻结结构：`api/internal/reportsource/digest_v2.go`；
- Digest V2 数据模型：`api/internal/sessiondigestv2/model.go`；
- 数据库迁移：`api/db/migrations/022_report_context_v1.sql`。
