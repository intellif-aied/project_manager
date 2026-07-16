# Agent 模块方案设计

## 1. 设计原则

Report Agent 不应被设计成一套独立的新产品形态。

第三方 Managed Agent 平台的标准 Agent 模型是：

- 创建时配置 Agent ID、名称、描述、Engine、默认模型、Instructions、Start Prompt Template、Credentials。
- 运行时根据 Start Prompt Template 提取变量，展示 Start Prompt Values。

Aida Report Agent 必须继续遵守这个标准模型。

Report Agent 只是标准 Agent 的一个业务用途：

- `business_type=report`
- 必须绑定 Aida Report MCP
- Start Prompt Template 里会包含 `report_type`、`period_json`、`target_json`、`run_id`、`mcp_url`、`AIDA_REPORT_MCP_AUTH` 等系统变量或系统能力引用
- 运行时这些系统变量由 Aida 后端自动生成并注入
- 前端不能让用户填写这些系统变量
- 前端只额外展示报告类型、日期 / 周期
- 其他普通变量仍然按标准 Start Prompt Values 展示
- Initial Message 仍然保留普通 Agent 的心智

最终效果：

```text
普通 Agent = 标准运行页
Report Agent = 标准运行页 + 报告类型/日期周期 - 系统保留变量 + 固定 Aida Report MCP
```

## 2. 背景

当前 AI Assets 已支持普通 Agent 和 Aida Report Agent：

- 普通 Agent：运行页解析 `start_prompt_template` 中的 `{{ variable }}`，让用户填写 Start Prompt Values。
- Report Agent：运行页展示报告类型、日期 / 周期、模型等业务参数，由后端注入 `run_id`、`mcp_url`、`period_json`、`target_json`、`AIDA_REPORT_MCP_AUTH` 等系统参数或系统能力，并通过 Aida Report MCP 写回报告。

Aida 报告主流程已经完整实现，本方案不补报告能力，也不重做报告模块。当前已实现并正式支持 6 类报告类型：

| report_type | 产品展示 |
|---|---|
| `personal_daily` | 个人日报 |
| `personal_weekly` | 个人周报 |
| `team_daily` | 小组日报 |
| `team_weekly` | 小组周报 |
| `department_daily` | 部门日报 |
| `department_weekly` | 部门周报 |

现有功能已能跑通报告生成和写回，但存在两个核心问题：

1. Report Agent 识别依赖文本 marker，例如 `AIDA_REPORT_AGENT:default`。
2. Report Agent 运行页没有展示用户自定义 `{{}}` 变量输入，只展示了报告业务参数。

本次改造只发生在 AI Assets / Agent 层：

- Report Agent 的结构化识别。
- Report Agent 创建 / 编辑时的业务标识。
- Report Agent 运行页对系统变量的隐藏和自动注入。
- Aida Report MCP 的固定绑定。
- legacy marker 的兼容迁移。

产品定义统一为：

```text
Report Agent = 标准 Agent + Aida 报告业务上下文 + 固定 Aida Report MCP
```

Report Agent 不是独立于普通 Agent 的新产品形态，也不是新的报告中心。本方案的目标是在标准 Agent 创建 / 运行模型上补齐 Aida 报告业务约束。

## 3. 当前实现梳理

### 3.1 普通 Agent 标准模型

前端运行页：

```text
web/src/features/aidashboard/ai-assets/pages/AgentRunPage.tsx
GenericAgentRunForm
```

当前普通 Agent 逻辑：

1. 读取 `agent.start_prompt_template`。
2. 使用 `extractPromptVariables` 提取 `{{ variable }}`。
3. 如果有变量，展示 Start Prompt Values。
4. 如果无变量，展示 Initial Message。
5. 提交到：

```text
POST /api/v1/ai-assets/agents/{agentId}/runs
```

后端入口：

```text
api/handler/managed_agent.go
StartAgentRun
```

这是标准 Agent 运行模型，Report Agent 也应该复用这个心智。

### 3.2 当前 Report Agent 特判

当前 Report Agent 由前端扫描文本 marker 判断：

```text
AIDA_REPORT_AGENT:default
AIDA_REPORT_AGENT_TYPES:
```

判断来源：

```text
agent.description
agent.instructions
agent.start_prompt_template
```

如果命中 marker，前端展示 `ReportAgentRunForm`，而不是标准 Start Prompt Values。

问题：

- marker 是 magic string，用户编辑描述或 instructions 时可能误删。
- 普通 Agent 文案中误出现 marker 会被误判。
- Report Agent 与标准 Agent 运行模型割裂。
- 用户自定义的普通 `{{}}` 变量没有输入入口。

### 3.3 当前 Report Agent 系统变量

默认 Report Agent Start Prompt Template 包含：

```text
report_type={{ report_type }}
period={{ period_json }}
target={{ target_json }}
run_id={{ run_id }}
mcp_url={{ mcp_url }}
当前用户凭据已通过 AIDA_REPORT_MCP_AUTH credential slot 注入，请通过 Aida Report MCP 获取上下文并回写生成结果。
```

这些变量分两类：

| 类型 | 变量 | 谁填写 |
|---|---|---|
| Aida 系统变量 / 系统能力 | `report_type`、`period_json`、`target_json`、`run_id`、`mcp_url`、`credential_slot`、`AIDA_REPORT_MCP_AUTH` | Aida 后端自动注入 |
| 用户普通变量 | `tone`、`project_name`、`extra_instruction` 等 | 用户在运行页填写 |

P0 要解决的是：前端继续解析模板，但系统变量不展示，普通变量继续展示。

## 4. P0 改造范围

P0 只做必要改造，不改变第三方 Agent 标准模型。

1. Aida 新增 agent profile，保存 `business_type` 和 `report_types`。
2. 默认 Report Agent 初始化 / 修复时写入 profile。
3. Agent 创建 / 编辑时可选择业务类型：
   - `generic`
   - `report`
4. Report Agent 必须绑定 Aida Report MCP。
5. 前端 Agent 运行页优先按 profile 判断：
   - `business_type=report`：展示标准运行页的 Report 扩展版
   - `business_type=generic` 或空：展示标准运行页
   - 无 profile 时 fallback legacy marker
6. Report Agent 运行页继续展示报告类型、日期 / 周期。目标对象由后端按当前用户和 report_type 自动解析。
7. Report Agent 运行页仍解析 Start Prompt Template。
8. Report Agent 运行页过滤系统保留变量。
9. Report Agent 运行页展示其他普通变量，按标准 Start Prompt Values 填写。
10. `StartReportAgentRun` 后端合并：
    - Aida 系统变量
    - 用户普通变量
11. 后端拒绝用户覆盖系统变量。
12. marker 只做旧数据兼容，不再作为新 Agent 主识别方式。
13. 本轮不做系统模板片段彻底抽离，保留为 P1。

## 5. Agent Profile 设计

### 5.1 表结构

新增 Aida 本地 profile 表：

```sql
CREATE TABLE managed_agent_profiles (
  agent_id TEXT PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  business_type TEXT NOT NULL DEFAULT 'generic',
  report_types JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

字段说明：

| 字段 | 含义 |
|---|---|
| `agent_id` | Managed Agent 平台 Agent ID |
| `user_id` | Aida 用户 ID |
| `business_type` | Aida 业务类型，P0 支持 `generic` / `report` |
| `report_types` | Report Agent 支持的报告类型 |
| `created_at` / `updated_at` | 本地 profile 时间 |

`business_type` 约定：

```text
generic - 标准普通 Agent
report  - 标准 Agent 的报告业务用途
```

`report_types` 保存 Report Agent 可用报告类型。当前 Aida 报告能力已正式支持以下 6 类，默认 Report Agent 使用完整列表：

```json
[
  "personal_daily",
  "personal_weekly",
  "team_daily",
  "team_weekly",
  "department_daily",
  "department_weekly"
]
```

### 5.2 DTO 扩展

后端返回 Agent 时合并 profile。

前端 `ManagedAgent` 增加：

```ts
business_type?: "generic" | "report";
report_types?: ReportType[];
```

后端 model 增加：

```go
BusinessType string   `json:"business_type,omitempty"`
ReportTypes  []string `json:"report_types,omitempty"`
```

说明：

- `business_type / report_types` 是 Aida 业务字段，不要求 Managed Agent 平台理解。
- `/ai-assets/agents` 从平台获取 Agent 列表后，按 `agent_id` 合并本地 profile。
- 无 profile 的历史 Agent，前端 fallback legacy marker。

## 6. 创建 / 编辑 Agent 设计

### 6.1 标准字段保持不变

创建 / 编辑 Agent 仍围绕第三方标准模型：

- Agent ID
- 名称
- 描述
- Agent 类型：普通 Agent / 报告 Agent
- Engine
- 默认模型
- 超时
- Instructions
- Start Prompt Template
- Credentials / MCP 绑定说明

Report Agent 不新增一套独立配置页，只是在标准 Agent 表单上增加 `business_type=report` 业务标识。创建 / 编辑页不展示报告类型多选。

### 6.2 Agent 类型

新增字段：

```text
Agent 类型
- 普通 Agent
- 报告 Agent
```

保存到 profile：

```json
{
  "business_type": "generic"
}
```

或：

```json
{
  "business_type": "report"
}
```

如果后端需要保存 supported report types，默认保存全部 6 类，不在创建 / 编辑页主表单展示。

### 6.3 Report Agent 的 MCP 约束

选择 `business_type=report` 后：

1. 必须绑定 Aida Report MCP。
2. 必须配置 `AIDA_REPORT_MCP_AUTH` credential slot。
3. 支持的报告类型由后端默认保存为全部 6 类。

创建 / 编辑页只展示说明：

```text
报告 Agent 会固定绑定 Aida Report MCP；运行时选择报告类型、日期/周期；目标对象由后端自动解析；report_type、period_json、target_json、run_id、mcp_url、AIDA_REPORT_MCP_AUTH 由 Aida 后端自动注入。
```

Report Agent 仍可以保留用户自定义 Start Prompt Template。

示例用户模板：

```text
请用 {{ tone }} 的风格生成报告。
重点关注项目：{{ project_name }}。
```

系统变量可以继续存在于模板中，但由 Aida 托管注入。

## 7. 运行页设计

实现原则：不新增 Report Agent 独立运行产品形态。前端应抽取标准 Agent 运行能力，包括 Start Prompt Values、Initial Message、Model、Prompt Preview、Run Status。普通 Agent 直接使用标准运行能力；Report Agent 在标准运行能力之上增加报告类型、日期 / 周期，并过滤系统保留变量；目标对象由后端按当前用户和 report_type 自动解析。

### 7.1 普通 Agent

普通 Agent 完全保持标准运行页：

```text
标准运行页 = Start Prompt Template 变量输入 + Initial Message fallback + 模型 override + 运行状态
```

普通 Agent 继续保持第三方标准 Agent 心智：

- Instructions
- Start Prompt Template
- Start Prompt Values
- Initial Message
- Model
- Credentials

判断：

```ts
business_type === "generic" || !business_type && !legacyMarker
```

展示：

- 如果模板有 `{{}}`，展示 Start Prompt Values。
- 如果模板无 `{{}}`，展示 Initial Message。
- 不展示报告类型、日期 / 周期。

### 7.2 Report Agent

Report Agent 是标准运行页的扩展：

```text
Report Agent 运行页 = 标准运行页 + 报告类型/日期周期 - 系统保留变量 + 固定 Aida Report MCP
```

判断顺序：

```ts
function isReportAgent(agent: ManagedAgent) {
  if (agent.business_type === "report") return true;
  if (agent.business_type === "generic") return false;
  return legacySupportedReportTypes(agent).length > 0;
}
```

支持报告类型：

```ts
function getSupportedReportTypes(agent: ManagedAgent) {
  if (agent.business_type === "report" && agent.report_types?.length) {
    return agent.report_types;
  }
  if (agent.business_type === "generic") {
    return [];
  }
  return legacySupportedReportTypes(agent);
}
```

Report Agent 运行页展示：

- 报告类型
- 日期 / 周期
- 标准 Start Prompt Values 中的用户普通变量
- Initial Message fallback
- 模型 override
- 运行状态

报告类型是用户可感知字段，但不是文本输入框，应展示为下拉选项：

| 值 | 展示 |
|---|---|
| `personal_daily` | 个人日报 |
| `personal_weekly` | 个人周报 |
| `team_daily` | 小组日报 |
| `team_weekly` | 小组周报 |
| `department_daily` | 部门日报 |
| `department_weekly` | 部门周报 |

`period_json / target_json` 不是产品字段。产品上展示日期 / 周期；目标对象不作为当前运行页字段，由后端按当前用户和 `report_type` 解析后转换为 `target_json` 注入给 Agent。

Report Agent 运行页不展示：

- `run_id`
- `mcp_url`
- `period_json`
- `target_json`
- `credential_slot`
- `AIDA_REPORT_MCP_AUTH`
- token / authorization

### 7.3 系统变量过滤

定义系统保留变量：

```ts
const REPORT_SYSTEM_PROMPT_KEYS = new Set([
  "report_type",
  "period_json",
  "target_json",
  "run_id",
  "mcp_url",
  "credential_slot",
  "AIDA_REPORT_MCP_AUTH"
]);
```

运行页仍按标准方式解析 `start_prompt_template`：

```ts
const allVars = extractPromptVariables(agent.start_prompt_template);
const userVars = allVars.filter((key) => !REPORT_SYSTEM_PROMPT_KEYS.has(key));
```

展示规则：

- 普通 Agent：展示 Start Prompt Template 中所有用户变量。
- Report Agent：展示 Start Prompt Template 中所有非系统变量。
- Report Agent 的系统保留变量必须过滤，不允许用户填写或覆盖。
- `userVars` 为空：不展示额外 Start Prompt Values。
- `userVars` 非空：按普通 Agent 的 Start Prompt Values 方式展示。

示例：

```text
report_type={{ report_type }}
period={{ period_json }}
run_id={{ run_id }}
credential={{ AIDA_REPORT_MCP_AUTH }}
请用 {{ tone }} 的风格生成报告。
```

运行页只展示：

```text
tone
```

不展示：

```text
report_type
period_json
run_id
AIDA_REPORT_MCP_AUTH
```

### 7.4 Initial Message 心智

Report Agent 仍应保留普通 Agent 的 Initial Message 心智。

建议：

- 如果 Report Agent 的模板没有用户普通变量，可以展示一个可选 Initial Message / 补充要求输入。
- 该内容作为普通用户变量或 `message` 合并到后端请求。
- 不应让用户理解 MCP、credential、run_id、`AIDA_REPORT_MCP_AUTH` 等系统概念。

示例：

```json
{
  "message": "请突出今日风险和阻塞。"
}
```

后端可把它合并为：

```text
additional_instruction
```

或直接作为 `message` 传入 session，具体实现保持与普通 Agent 心智一致即可。

## 8. Report Agent Run API

### 8.1 请求结构

当前 Report Agent run 请求保留报告业务参数，并新增普通 Start Prompt Values：

```json
{
  "report_type": "personal_daily",
  "period": { "date": "2026-07-01" },
  "target": { "type": "self" },
  "model_id": "MiniMax-M2.5",
  "start_prompt_values": {
    "tone": "简洁",
    "project_name": "Aida"
  },
  "message": "请突出风险和阻塞。"
}
```

说明：

- `report_type / period / target` 是报告业务参数。
- `report_type` 对应运行页的报告类型下拉。
- `period` 对应运行页的日期 / 周期。
- `target` 当前由前端固定传 `self` 或由后端默认解析，不作为运行页可选字段。
- `start_prompt_values` 是标准 Start Prompt Values 中的用户普通变量。
- `message` 是 Initial Message / 补充要求。
- `run_id / mcp_url / period_json / target_json / credential_slot / AIDA_REPORT_MCP_AUTH` 不允许由前端传入。

### 8.2 后端合并逻辑

后端先生成 Aida 系统变量：

```go
systemValues := reportAgentStartPromptValues(
    runID,
    reportType,
    date,
    weekStart,
    weekEnd,
    target,
    mcpURL,
)
```

再合并用户普通变量：

```go
for key, value := range req.StartPromptValues {
    key = strings.TrimSpace(key)
    if key == "" {
        continue
    }
    if isReportSystemPromptKey(key) {
        return RESERVED_PROMPT_VALUE
    }
    systemValues[key] = strings.TrimSpace(value)
}
```

如果有 `message`，也按普通 Agent 心智合并：

```go
if strings.TrimSpace(req.Message) != "" {
    systemValues["message"] = strings.TrimSpace(req.Message)
}
```

或使用明确字段：

```go
systemValues["additional_instruction"] = strings.TrimSpace(req.Message)
```

### 8.3 保留变量保护

用户请求中不能覆盖：

```text
report_type
period_json
target_json
run_id
mcp_url
credential_slot
AIDA_REPORT_MCP_AUTH
```

如果传入，后端返回 400：

```json
{
  "code": "RESERVED_PROMPT_VALUE",
  "error": "run_id is managed by Aida"
}
```

### 8.4 报告类型校验

后端校验：

1. 如果 profile 存在，`report_type` 必须在 `report_types` 中。
2. 如果 profile 不存在，fallback legacy marker 支持类型。
3. `business_type=generic` 的 Agent 不允许走 Report Agent run API。
4. Report Agent 必须绑定 Aida Report MCP，否则拒绝运行。

Aida Report MCP 是 Report Agent 的固定能力。运行页不允许用户选择、替换或覆盖 Aida Report MCP，也不展示 `AIDA_REPORT_MCP_AUTH`。运行时由后端通过 `credential_overrides` 或系统注入完成凭据传递。

## 9. 默认 Report Agent

默认 Report Agent 初始化 / 修复时：

1. 创建 / 修复标准 Agent 配置。
2. 绑定 Aida Report Skill。
3. 绑定 Aida Report MCP。
4. 配置 `AIDA_REPORT_MCP_AUTH` credential slot。
5. 写入 Aida profile：

```json
{
  "business_type": "report",
  "report_types": [
    "personal_daily",
    "personal_weekly",
    "team_daily",
    "team_weekly",
    "department_daily",
    "department_weekly"
  ]
}
```

如果发现旧默认 Report Agent 只有 marker、没有 profile，则 backfill profile。

## 10. 迁移步骤

### 10.1 阶段 1：profile 表和查询合并

1. 新增迁移 `managed_agent_profiles`。
2. 后端新增 profile 读写。
3. `/ai-assets/agents` 返回前合并 profile。
4. 无 profile 时前端继续 fallback legacy marker。

验收：

- 普通 Agent 列表不受影响。
- 旧 Report Agent 仍能展示报告运行页。

### 10.2 阶段 2：默认 Report Agent 写入 profile

1. 默认 Report Agent 初始化时写入 profile。
2. 默认 Report Agent 修复时补齐 profile。
3. 旧 marker Report Agent 自动 backfill profile。

验收：

- 默认 Report Agent 返回 `business_type=report`。
- 默认 Report Agent 返回完整 `report_types`。

### 10.3 阶段 3：Agent 创建 / 编辑支持业务类型

1. 创建 / 编辑页增加 Agent 类型。
2. 选择报告 Agent 时展示固定绑定 Aida Report MCP 的说明。
3. 保存时写入 profile，后端默认保存全部 6 类 supported report types。
4. 报告 Agent 必须绑定 Aida Report MCP。

验收：

- 用户可以创建普通 Agent。
- 用户可以创建报告 Agent。
- 报告 Agent 仍使用标准 Agent 表单。
- 创建 / 编辑页不展示 `personal_daily / personal_weekly / team_daily / team_weekly / department_daily / department_weekly` 多选。

### 10.4 阶段 4：运行页标准化扩展

1. Report Agent 运行页基于标准 Start Prompt Values 逻辑。
2. 增加报告类型、日期 / 周期输入。
3. 过滤系统保留变量。
4. 展示用户普通变量。
5. 保留 Initial Message / 补充要求心智。

验收：

- 模板中 `{{ tone }}` 会显示输入框。
- 模板中 `{{ run_id }}` 不会显示输入框。
- Report Agent 仍显示报告类型、日期 / 周期。
- 普通 Agent 运行页不受影响。

### 10.5 阶段 5：后端合并和保护

1. `StartReportAgentRun` 增加 `start_prompt_values`。
2. 后端合并系统变量和用户变量。
3. 后端拒绝覆盖系统变量。
4. 后端校验 Report Agent 绑定 Aida Report MCP。

验收：

- 用户变量能进入 Agent Start Prompt Values。
- 用户传 `run_id` 返回 `RESERVED_PROMPT_VALUE`。
- 报告仍能成功写回并关联 `ai_runs`。

## 11. P1 事项

P0 不做以下内容。

### 11.1 系统模板片段彻底抽离

P1 再考虑把系统片段从用户 `start_prompt_template` 中拆出。

目标形态：

```text
{Aida Report Agent 系统片段}

{用户自定义 start_prompt_template}
```

编辑页：

- 系统片段只读。
- 用户模板可编辑。
- 运行或保存时由 Aida 合成最终模板。

### 11.2 平台 metadata

P0 使用 Aida 本地 profile，不依赖 Managed Agent 平台改 schema。

P1 可评估把 profile 上移到平台 metadata：

```json
{
  "metadata": {
    "aida_business_type": "report",
    "aida_report_types": ["personal_daily"]
  }
}
```

### 11.3 移除 legacy marker

P0 保留 marker fallback。

P1 在历史数据完成 backfill 后，再移除：

```text
AIDA_REPORT_AGENT:default
AIDA_REPORT_AGENT_TYPES:
```

## 12. 验收标准

1. Aida 存在 `managed_agent_profiles`。
2. 新建普通 Agent 后 profile 为 `business_type=generic`。
3. 新建报告 Agent 后 profile 为 `business_type=report`，后端 supported report types 默认为全部 6 类。
4. 默认 Report Agent 初始化 / 修复会写入 profile。
5. Agent 运行页优先按 profile 判断运行形态。
6. `business_type=report` 展示标准运行页的 Report 扩展版。
7. `business_type=generic` 或空且无 marker 展示标准普通运行页。
8. 无 profile 的旧 marker Agent 仍展示 Report 扩展运行页。
9. Report Agent 运行页展示报告类型、日期 / 周期。
10. Report Agent 运行页不展示 `run_id / mcp_url / credential_slot / AIDA_REPORT_MCP_AUTH`。
11. Report Agent 运行页解析用户自定义 `{{}}` 变量并展示输入框。
12. Report Agent 运行页过滤系统变量，不展示系统变量输入框。
13. Report Agent 运行页保留 Initial Message / 补充要求心智。
14. 后端合并 Aida 系统变量和用户普通变量。
15. 后端拒绝用户覆盖系统变量。
16. Report Agent 必须绑定 Aida Report MCP 才能运行。
17. 成功运行后报告仍能写回并关联 `ai_runs`。

## 13. 风险和注意事项

### 13.1 profile 与平台 Agent 状态不同步

P0 profile 存在 Aida 本地。如果用户绕过 Aida 在 Managed Agent 平台删除、复制或修改 Agent，profile 可能不同步。

缓解：

- Aida 只对平台返回的 Agent 合并 profile。
- 找不到平台 Agent 时，profile 不单独展示。
- 删除 Agent 能力补齐时，再决定是否清理 profile。

### 13.2 旧 marker 兼容期

迁移期间必须保留 legacy marker fallback，否则旧默认 Report Agent 可能退回普通运行页。

### 13.3 系统模板仍可被用户误改

P0 不抽离系统模板片段，所以用户仍可能在 Agent 编辑页误删 `run_id / mcp_url` 等模板内容。

缓解：

- 后端仍注入系统 Start Prompt Values。
- 运行页不展示系统变量。
- 后端禁止覆盖系统变量。
- P1 再做系统模板只读和运行时合成。

### 13.4 run_id 仍是内部关联 ID

P0 不取消 `run_id` 机制。`run_id` 仍用于 MCP 写回关联、权限校验和 `ai_runs` 状态更新。

本轮只保证：

- 前端不让用户填写 `run_id`。
- 用户普通变量不能覆盖 `run_id`。
- marker 不再作为新 Agent 主识别方式。

## 14. 推荐结论

P0 推荐按以下顺序落地：

1. 增加 Aida 本地 `managed_agent_profiles`。
2. 默认 Report Agent 初始化 / 修复写入 profile。
3. Agent 创建 / 编辑保存 `business_type`，Report Agent 的 supported report types 由后端默认保存全部 6 类。
4. Agent 运行页优先按 profile 判断运行形态。
5. Report Agent 运行页基于标准 Agent 运行页扩展。
6. Report Agent 运行页增加报告类型、日期 / 周期。
7. Report Agent 运行页过滤系统保留变量。
8. Report Agent 运行页展示其他普通 Start Prompt Values。
9. StartReportAgentRun 合并系统变量和用户普通变量，并拒绝覆盖系统变量。
10. marker 仅作为旧数据 fallback。

这样 Report Agent 仍然是标准 Agent，只是多了 Aida 报告业务用途和固定 Report MCP 约束，不会演化成一套独立的新产品形态。
