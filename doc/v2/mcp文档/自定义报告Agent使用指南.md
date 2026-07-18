# 自定义报告 Agent 使用指南

> 面向用户：需要在 Aida 中创建或定制报告 Agent 的普通用户
> 建立日期：2026-07-18
> 适用范围：Aida Report MCP 当前版本

## 1. 先选择适合的使用方式

如果你只是想生成正常的日报或周报，建议直接使用平台提供的默认报告 Agent。默认 Agent 已由平台配置 Skill、MCP、身份和写回流程，用户不需要填写 MCP 地址或 Token。

仅在以下情况才建议创建自定义报告 Agent：

- 希望使用自己的报告格式或表达风格；
- 希望增加固定栏目；
- 需要针对特定岗位调整成果归并方式；
- 需要设置自定义定时报告。

## 2. 创建 Agent

1. 进入“AI 资产”的 Agent 创建页。
2. Agent 类型选择“报告 Agent”，不要创建成普通聊天 Agent。
3. 选择平台内置的 Aida Report MCP。
4. 保留平台内置 Report Skill 作为运行协议；如果需要自定义格式，再额外绑定你自己编写的报告 Skill。
5. 选择可用模型并保存 Agent。

不要在 Agent Instructions 或 Skill 中写入：

- Aida 登录 Token；
- MCP URL；
- 固定用户 ID、小组 ID 或部门 ID；
- Session ID 或 Selection ID；
- 系统账号凭据。

这些内容由 Aida 在运行时按当前用户和当前报告 Run 安全注入。

报告 Agent 运行前，Aida 会检查并补齐当前平台 Report Skill 和 Report MCP。用户自定义 Skill 用于增加格式和表达要求，不应删除或改写平台的读取、权限和写回协议。

## 3. 正确启动报告

自定义 Agent 必须通过 Aida 的报告生成入口或报告 Agent 运行页启动。平台会为每次运行创建 `run_id`，并注入：

- `run_id`；
- `report_type`；
- `period`；
- `target`；
- 个人报告的冻结来源标识。

不要从普通聊天会话中直接输入“帮我生成今天的日报”来代替报告运行。普通聊天不一定拥有有效 `run_id`，也无法完成 Aida 报告写回。

## 4. 个人日报/周报的标准流程

当运行参数包含平台准备的个人报告来源时，标准流程只有三步：

```text
get_report_context(run_id)
        ↓
根据 Context 识别成果、归并工作并生成 Markdown
        ↓
write_report_result(...)
```

### 4.1 读取 Context

```json
{
  "run_id": "平台注入的 run_id"
}
```

调用规则：

- 只调用一次 `get_report_context`；
- `run_id` 必须原样使用；
- Context 已包含本次报告冻结的完整 Session Digest；
- 不要再调用 `get_sessions`、`get_tasks` 或 `get_requirements` 重新扫描数据；
- 不要要求用户再提供 Session ID、URL 或 Token。

Context 中的 `report_period_summary` 是本次报告周期的统一成果视图，应优先用于生成报告；`items` 保留各 Session Slice 的结构化 Digest，可用于核对来源和补足细节。

### 4.2 生成报告

Agent 仍然负责真正需要语义判断的工作：

- 识别实际工作成果；
- 归并属于同一目标的多条记录；
- 区分已完成、进行中、失败和阻塞；
- 保留重要结果、决策、验证和未解决事项；
- 按用户自定义 Skill 的格式生成最终 Markdown。

平台不强制用户使用固定日报栏目或固定成果数量。

### 4.3 写回结果

```json
{
  "report_type": "personal_daily",
  "period": {"date": "2026-07-16"},
  "target": {"type": "self"},
  "run_id": "平台注入的 run_id",
  "content": "# 工作日报\n\n最终 Markdown 正文",
  "summary": "可选的简短摘要"
}
```

不能只把报告放在 Agent 的最终对话回复中。只有 `write_report_result` 成功，才表示报告已真正保存到 Aida。

## 5. 生成失败时

如果 Context 无法读取，或 Agent 确实无法生成可信的报告，应调用 `write_report_failure`：

```json
{
  "report_type": "personal_daily",
  "period": {"date": "2026-07-16"},
  "target": {"type": "self"},
  "run_id": "平台注入的 run_id",
  "error_code": "REPORT_GENERATION_FAILED",
  "error_message": "无法从当前报告上下文生成可信报告"
}
```

`write_report_failure` 不会修改已有报告正文。

## 6. 团队和部门报告

Context V1 当前只用于个人日报/周报。团队和部门报告仍使用平台兼容工具：

- `get_daily_reports`；
- `get_weekly_reports`；
- `get_report_inventory`；
- 当报告 Skill 明确需要时，使用 `get_tasks` 或 `get_requirements`。

小组报告应聚合当前权限内的成员报告，部门报告应聚合小组报告。不要默认读取成员原始 Session，也不要自己编造用户、小组或部门范围。

团队/部门工具的具体 input schema 以 Agent 运行时 MCP `tools/list` 返回的契约为准。

## 7. 可直接放入自定义 Skill 的运行协议

下面文本只规定数据读取和写回协议，不限制你的报告栏目和表达风格：

```markdown
## Aida 报告运行协议

1. 读取平台注入的 run_id、report_type、period 和 target，不要自行猜测或改写。
2. 个人日报/周报存在平台准备的报告来源时，只传 run_id 调用一次 get_report_context。
3. 上述个人报告不再调用 get_sessions、get_tasks 或 get_requirements 重新扫描数据。
4. 从 Context 中识别成果、归并同一目标的记录、理解完成度，并按本 Skill 的格式生成 Markdown。
5. 生成成功后，必须使用原始运行参数和原始 run_id 调用 write_report_result。
6. 无法生成可信报告时，调用 write_report_failure，不要伪造成功结果。
7. 不要要求用户提供 MCP URL、Token、Session ID、Selection ID 或其他内部参数。
```

## 8. 常见问题

### 为什么 `get_report_context` 只接受 `run_id`？

报告日期、用户身份、Session 选择和权限范围已由平台绑定到当前 Run。Agent 只传 `run_id`，可以避免读错日期或越权读取其他来源。

### 用户没有手动选择 Session 时怎么办？

服务端会按报告周期自动冻结默认 Session 来源。与报告日期有交集的完整 Slice 会进入当前 Context，Agent 无需自行扫描 Session。

### 可以在 Skill 中改变报告格式吗？

可以。Report MCP 只负责按权限提供数据和写回结果，报告格式、语气、栏目和详细程度由用户的 Skill 和模型决定。

### Agent 显示 completed，但 Aida 中没有报告怎么办？

检查运行记录中是否真正调用并成功完成 `write_report_result`。Agent 对话结束不等于 Aida 报告写回成功。

### 为什么不建议自己组装 HTTP 请求？

MCP 由平台绑定并注入当前用户身份。手写 URL 和 Authorization 容易造成凭据泄漏、环境混用和权限错误。

## 9. 前端使用指南同步要求

前端“使用指南”后续接入本文档时，至少要同步：

- AI 资产页增加“创建自定义报告 Agent”文章；
- 说明默认 Agent 适用于大多数用户；
- 说明用户无需填写 MCP URL 和 Token；
- 提供本文档第 7 节的可复制运行协议；
- 将个人报告说明修正为“只读取平台准备的 Report Context”；
- 删除“个人报告可以继续自由读取任务、需求和已有报告”的旧描述；
- 说明报告必须经过 `write_report_result` 真正写回。

前端文案只展示用户需要理解的产品流程，不展示 Digest 版本、Context hash、数据库表、内部 MCP URL 或凭据注入实现。

## 10. 相关文档

- 研发契约：[`README.md`](README.md)；
- Agent 优化总纲：[`../agent优化/总纲.md`](../agent优化/总纲.md)。
