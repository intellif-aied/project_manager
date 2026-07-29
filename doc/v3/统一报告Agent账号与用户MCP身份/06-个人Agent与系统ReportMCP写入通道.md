# V3 调整：个人 Agent 与系统 Report MCP 写入通道

> 文档状态：已实现，待测试服真实链路验证
> 日期：2026-07-29
> 覆盖范围：个人默认 Report Agent 的运行依赖、启动方式和写回门禁

## 1. 核心结论

系统 Agent 和个人 Agent 是两条独立生成流程：

| 项目 | 系统 Agent | 个人 Agent |
|---|---|---|
| Agent、模型与额度 | 系统专用账号 | 当前用户账号 |
| Prompt | 系统维护 | 用户维护 |
| Skill | 系统 Report Skill | 用户自己的 Skill，Aida 不注入系统 Skill |
| 第三方 MCP | 系统固定配置 | 用户自己的配置 |
| 系统 Report MCP | 强制提供 | 强制提供 |
| Report Brief | 必须 | 不要求 |
| 报告写入身份 | 当前 Aida 用户 | 当前 Aida 用户 |

用户选择个人 Agent，代表选择了完整的个人生成逻辑。Aida 只补充“把结果安全写入本次日报或周报”所必需的系统 Report MCP，不接管个人 Agent 的内容生成方式。

## 2. 当前问题

现有实现把个人 Agent 当成系统 Agent 修复：

1. 向个人 Agent 注入系统 Report Skill；
2. 使用 `/aida-report` 启动个人 Session；
3. 要求个人 Agent 先调用 `write_report_brief`；
4. 个人 Agent 即使按自己的 Skill 直接写入，也会被 `REPORT_BRIEF_REQUIRED` 拒绝。

这会让用户配置的 Skill 失效，并把个人流程错误地绑定到系统两阶段生成协议。

已有的 Report Run Token 机制是正确的：每次运行签发包含 `report_run_id` 的短期 JWT，通过 MCP 凭证覆盖传入 Session。Report MCP 据此锁定当前运行和当前用户，Agent 不需要、也不能通过参数切换到其他运行。

## 3. 目标流程

### 3.1 系统 Agent

1. Aida 选择系统专用账号和 System Report Agent；
2. 为本次运行签发 Report Run Token；
3. 挂载系统 Report Skill 和 managed Report MCP 工具集；
4. 使用 `/aida-report` 启动；
5. 先写入 Report Brief，再写入 Final Report。

### 3.2 个人 Agent

1. Aida 选择当前用户及其 Personal Report Agent；
2. 保留用户配置的模型、Prompt、Skill 和第三方 MCP；
3. 为本次运行签发 Report Run Token；
4. 强制补充只服务本次运行的 Personal Report MCP 工具集；
5. 不发送 `/aida-report`，而是按次注入最小启动消息，只要求调用 `get_report_context`、按个人 Agent 自身配置生成，并调用 `write_report_result` 保存；
6. 个人 Agent 可直接写入 Final Report，不经过 Report Brief 门禁。

Personal Report MCP 至少提供：读取本次 Report Context、写入报告结果、写入失败状态。它不提供 `write_report_brief`，避免个人 Skill 被系统两阶段协议误导。

报告格式由执行来源决定：系统 Report MCP 的 `write_report_result` 工具要求 `format_mode=standard`，继续执行 Aida 标准格式规范化；个人 Report MCP 不暴露该参数，服务端原样保留个人 Skill 提交的 Markdown，只执行非空、Run 身份、来源完整性和保存校验。

## 4. 修改方案

### 4.1 显式执行策略

Report Run 的 `execution_input` 增加 `report_agent_source`：

- `system`：系统两阶段生成；
- `personal`：个人生成、系统 MCP 写回。

Agent 修复、Session 启动消息、MCP 工具集和 Brief 门禁统一读取这一执行策略，不再通过 Agent 名称、默认状态或账号来源分散猜测。

为兼容更新前已经创建的运行，字段缺失时按 `system` 处理，避免旧系统运行绕过 Brief 门禁。

### 4.2 依赖修复边界

- 系统 Agent：继续校验并修复系统 Report Skill、managed Report MCP 和固定模型；
- 个人 Agent：只确保 Personal Report MCP 及其凭证槽存在；
- 个人 Agent 的自定义 Prompt、Skill 和其他 MCP 不得被覆盖；
- 对历史上由 Aida 注入的系统 Report Skill、Instructions 和系统启动模板，只移除可精确识别的系统托管残留，不删除用户资产；清理请求必须显式发送空字段，不能因 JSON 省略规则导致清理失效。

### 4.3 保留 Run 身份绑定

继续沿用现有机制：

1. 每次运行单独签发含 `report_run_id` 的 JWT；
2. Session 创建时通过 Report MCP credential override 注入；
3. MCP 服务端优先从 token 读取 run_id；
4. 请求参数携带不同 run_id 时拒绝执行。

本次不改 JWT 结构、不引入永久 token、不改变用户和组织数据权限。

## 5. 验收标准

1. 系统 Agent 仍使用系统 Skill、`/aida-report` 和 Report Brief 两阶段流程；
2. 个人 Agent 的自定义 Prompt、Skill、模型和第三方 MCP 保持不变；
3. 个人 Agent 自动具备系统 Report MCP，且能把结果写入当前 Report Run；
4. 个人 Agent 不出现 `write_report_brief`，直接写报告不会触发 `REPORT_BRIEF_REQUIRED`；
5. 系统 Agent 输出继续规范化为 Aida 标准报告结构，个人 Agent 输出不被增加“工作概览 / 工作详情”等系统标题；
6. 两种流程的 MCP 调用都由 token 绑定到正确的 run_id 和 Aida 用户；
7. 个人 Agent 不会因系统 Skill 或 `/aida-report` 被带入系统生成流程；
8. 旧运行和系统默认生成行为保持兼容；
9. 不新增数据库表或迁移，不影响报告外的其他业务。

## 6. 非目标

- 不为个人 Agent 提供或复制系统 Report Skill；
- 不统一用户自定义 Skill 的格式和生成质量；
- 不替用户修改第三方 MCP；
- 不改变日报、周报的数据模型、Digest 或 Projection；
- 不修改系统 Agent 的两阶段生成质量策略。
