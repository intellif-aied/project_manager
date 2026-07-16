# Agent 默认配置方案文档

## 1. 背景与问题

原先方案是在用户启用账号时自动创建一个默认报告 Agent、一个 Aida Report Skill、一个 Aida Report MCP，用来降低首次配置门槛。代码里曾通过 `AuthHandler` 挂载默认资产初始化器，在 bootstrap admin 登录、管理员启用用户、批量添加用户时触发。

这个模型需要删除，原因是：

1. 账号启用阶段不应静默创建用户没有主动创建过的 AI 资产。
2. Aida Report MCP / Aida Report Skill 是报告 Agent 的系统运行依赖，不应混入“我的 MCP / 我的 Skill”。
3. Report MCP / Skill 作为普通资产暴露后，用户会误以为可以编辑、归档、删除。
4. 默认 Skill / MCP 如果复制成每个用户自己的资产，后续平台发布时很难统一升级。
5. ensure 自愈应该服务于“创建/运行报告 Agent 前兜底”，而不是服务于“账号启用初始化资产”。

目标产品模型改为：账号启用不创建 AI 资产；用户进入 `/ai-assets` 后，若当前用户没有 report agent，才展示“创建默认报告 Agent”；Aida Report MCP / Skill 作为平台托管系统能力，只读展示在“报告系统能力”入口。

## 2. 当前代码现状

### 2.1 后端相关文件、函数、接口

- 路由入口：`api/main.go`
  - AI Assets 路由注册在 `main()` 内，包括 `GET /api/v1/ai-assets/skills`、`GET /api/v1/ai-assets/mcp`、`GET /api/v1/ai-assets/agents`、`POST /api/v1/ai-assets/agents`、`POST /api/v1/ai-assets/report-agents/{agentId}/runs`。
  - 当前新增显式接口：`POST /api/v1/ai-assets/report-agents/default`，绑定 `ManagedAgentHandler.CreateDefaultReportAgent`。
  - 账号启用阶段不再调用默认资产初始化器；原 admin 回填路由不再注册。
- 账号与用户启用：`api/handler/auth.go`
  - `Login` 只负责 AIHub 登录、bootstrap admin 建档、返回用户信息，不再初始化 Agent / Skill / MCP。
  - `AdminUpdateUser` 只更新本地启用状态、角色和团队，不再在 `local_enabled=false -> true` 时创建默认资产。
  - `AdminBatchAddUsers` 只批量建档，不再为 `local_enabled=true` 用户创建默认资产。
- Managed Agent 主逻辑：`api/handler/managed_agent.go`
  - `ManagedAgentDefaults`：保存 `ReportMCPSlug`、`ReportMCPVersion`、`AIDAPublicBaseURL` 等平台默认配置。
  - `ListSkills` / `ListMCPEntries`：普通用户资产列表会过滤 Aida Report Skill / MCP。
  - `ArchiveSkill` / `DeleteSkill`：命中 `service.ReportSkillSlug + service.ReportSkillVersion` 时返回 `409 REPORT_SKILL_PROTECTED`。
  - `CreateMCPEntry` / `ArchiveMCPEntry` / `DeleteMCPEntry`：命中 `h.defaults.ReportMCPSlug + h.defaults.ReportMCPVersion` 时返回 `409 REPORT_MCP_PROTECTED`。
  - `CreateDefaultReportAgent`：显式按需创建默认报告 Agent。先判断当前用户是否已有 report agent；已有则幂等返回；没有则 ensure Report Skill / MCP 后创建默认报告 Agent。
  - `CreateMyAgent` / `UpdateMyAgent`：当 `business_type=report` 或既有 profile 是 report agent 时，后端强制 ensure Report Skill / MCP，并补齐 Report Skill ref、Report MCP binding 和 credential slot。
  - `StartReportAgentRun`：运行 report agent 前 ensure Report Skill / MCP；若 Agent 缺 Report MCP binding，会尝试用 `repairedDefaultReportAgentRequest` 自动修复。
  - `DailyReportIntegration`：`GET /api/v1/ai-assets/daily-report-integration` 只读返回 Report MCP / Skill 信息，供“报告系统能力”入口展示。
- Report MCP 服务端入口：`api/handler/report_mcp.go`
  - `ReportMCPHandler.Serve` 处理 `POST /api/v1/mcp/reports`，提供报告上下文读取与写回工具。
- Report Skill 模板：`api/service/daily_report_skill.go`
  - `ReportSkillSlug = "aida-report"`
  - `ReportSkillVersion = "1.0.0"`
  - `ReportSkillMarkdown(...)` 生成报告 Agent 的平台默认 Skill Markdown。
- 报告草稿旧链路：`api/service/report_draft.go`
  - `DefaultDailyReportSkillID = "default_daily"` 是报告草稿逻辑 ID，不是 Managed Agent Skill 资产。

### 2.2 前端相关页面、组件、接口调用

- AI Assets 主页面：`web/src/features/aidashboard/ai-assets/pages/AIAssetsPage.tsx`
  - 拉取 `fetchManagedSkills(scope)`、`fetchManagedMCPEntries(scope)`、`fetchManagedAgents()`、`fetchManagedAgentRuns(...)`、`fetchManagedAgentSchedules()`。
  - 使用 `isReportSystemSkill` / `isReportSystemMCP` 做前端防御性过滤。
  - 顶部统计中 Skills / MCP 只统计过滤后的用户资产。
  - “我的 Skills / 我的 MCP”表格只展示过滤后的用户资产。
  - 使用 `hasReportAgent = agents.some(isReportAgentAsset)` 判断是否展示“创建默认报告 Agent”，不是判断 `agents.length`。
  - 右上角入口已改为“报告系统能力”，只读展示 Report MCP / Skill。
- Agent 创建页：`web/src/features/aidashboard/ai-assets/pages/AgentCreatePage.tsx`
  - 以 `include_system=true` 拉取 Skills / MCP，作为 Agent 资源绑定候选。
  - 候选列表包含用户自建资源和系统内置 Aida Report Skill / MCP，不分组、不置顶、不特殊排序。
- Agent 编辑页：`web/src/features/aidashboard/ai-assets/pages/AgentEditPage.tsx`
  - 以 `include_system=true` 拉取 Skills / MCP，保证已绑定的系统内置资源可正常回显。
- Agent 表单组件：`web/src/features/aidashboard/ai-assets/components/AgentEditor.tsx`
  - 不按 Agent 类型隐藏资源选择器。
  - Skill / MCP 选择体验对普通 Agent 和报告 Agent 保持一致。
  - 系统内置资源只显示“系统内置”标识或名称后缀。
- Agent 运行页：`web/src/features/aidashboard/ai-assets/pages/AgentRunPage.tsx`
  - 优先使用 `business_type=report` 判断 report agent。
  - marker 只作为补充识别。
  - report agent 运行调用 `startReportAgentRun`；普通 Agent 运行调用 `startManagedAgentRun`。
- 前端 API 封装：`web/src/features/aidashboard/api/client.ts`
  - 新增 `createDefaultReportAgent()`，调用 `POST /ai-assets/report-agents/default`。
  - `fetchDailyReportAgentIntegration()` 继续服务“报告系统能力”只读展示。
- 前端类型：`web/src/features/aidashboard/api/types.ts`
  - `DailyReportAgentIntegration` 增加 MCP slug/version/status/managed 与 Skill status/managed 字段。
- 前端资产工具：`web/src/features/aidashboard/ai-assets/utils/agentAssets.ts`
  - `isReportSystemSkill`
  - `isReportSystemMCP`
  - `isReportAgentAsset`
  - `REPORT_SYSTEM_MARKER`
  - `REPORT_AGENT_MARKER`

### 2.3 当前默认资产创建流程

当前目标流程已经从“账号启用自动创建”改为“用户按需创建”：

1. 用户启用账号：不创建 Agent / Skill / MCP。
2. 用户进入 `/ai-assets`：前端拉取 agents 后判断是否存在 report agent。
3. 没有 report agent：展示“创建默认报告 Agent”按钮。
4. 点击按钮：调用 `POST /api/v1/ai-assets/report-agents/default`。
5. 后端：
   - 用 `managed_agent_profiles.business_type=report` 优先判断已有 report agent。
   - 没有 report agent 时 ensure Aida Report Skill / MCP。
   - 使用 `defaultReportAgentRequest` 创建默认报告 Agent。
   - 写入 `managed_agent_profiles`，`business_type=report`。
6. 运行 report agent 前：`StartReportAgentRun` 再次 ensure Report Skill / MCP，并修复缺失的 Report MCP binding。

### 2.4 当前 Aida Report MCP / Skill 的识别方式

- Report MCP：
  - 后端保护与过滤使用 `h.defaults.ReportMCPSlug + h.defaults.ReportMCPVersion`。
  - 默认配置来自 `ManagedAgentDefaults`，最终由配置项注入，不在保护逻辑里写死字符串。
  - `AIDA_REPORT_DEFAULT:true` 只作为展示/前端防御辅助标记。
- Report Skill：
  - 后端保护与过滤使用 `service.ReportSkillSlug + service.ReportSkillVersion`。
  - 当前值为 `aida-report@1.0.0`。
  - `AIDA_REPORT_DEFAULT:true` 只作为展示/前端防御辅助标记。
- report agent：
  - 后端优先读取 `managed_agent_profiles.business_type=report`。
  - 默认报告 Agent marker 作为补充识别。
  - 前端使用同样口径判断是否展示“创建默认报告 Agent”。

### 2.5 当前 report agent 创建和运行流程

- 默认报告 Agent 创建：
  - 前端入口：`AIAssetsPage.tsx` 的引导区按钮。
  - 前端接口：`createDefaultReportAgent()`。
  - 后端接口：`ManagedAgentHandler.CreateDefaultReportAgent`。
  - 后端模板：`defaultReportAgentRequest`、`defaultReportAgentInstructions`、`defaultReportAgentStartPromptTemplate`。
- 普通 Agent 创建 / 编辑：
  - 通用接口仍是 `POST /api/v1/ai-assets/agents`、`PUT /api/v1/ai-assets/agents/{agentId}`。
  - 后端按前端提交的资源绑定保存，不因 `business_type=report` 强制追加 Skill / MCP。
- report agent 运行：
  - 前端运行页只让用户选择 `report_type`、周期、目标和可选消息。
  - 后端 `StartReportAgentRun` 注入 `run_id`、`report_type`、`period_json`、`target_json`、`mcp_url`、`AIDA_REPORT_MCP_AUTH`。
  - 普通 Agent 不能走 report run；后端会返回 `NOT_REPORT_AGENT`。

### 2.6 当前风险点

1. 外部 Managed Agent 服务 schema 不完全由 Aida 控制，短期仍可能要求 Skill / MCP 在用户维度存在一条可引用记录。
2. ensure 重建 MCP 后，已存在 Agent 的 binding 仍可能缺失或悬空；当前运行前会修复当前 Agent，不扫描所有 Agent。
3. 如果未来允许用户编辑默认报告 Agent，需要明确哪些字段可升级、哪些字段属于用户自定义。
4. 前端隐藏系统资产必须持续配合后端保护，不能只靠 UI。

## 3. 目标产品模型

### 3.1 用户资产

用户资产是用户主动创建和管理的对象：

- 普通 Agent
- 用户主动创建的 Skill
- 用户主动创建的 MCP
- 用户创建的定时任务
- 默认报告 Agent 的用户实例

默认报告 Agent 属于用户实例，因为它有用户维度的运行记录、归档状态和未来可能的个性化字段。

### 3.2 平台托管资产

平台托管资产由 Aida 平台维护，不作为普通用户资产展示：

- Aida Report MCP
- Aida Report Skill
- 默认报告 Agent 模板
- 默认 prompt / instructions
- 默认运行配置
- 报告写回契约和系统保留 prompt key

### 3.3 默认报告 Agent

默认报告 Agent 是用户维度 Agent 实例，但引用平台托管配置：

- 用户可以拥有一个默认报告 Agent。
- 通过“创建默认报告 Agent”按需创建时，默认绑定 Aida Report MCP / Skill。
- 用户进入创建 / 编辑页后，Aida Report MCP / Skill 与用户自建资源一起作为普通候选项展示。
- 后端只在默认报告 Agent 创建和 report agent 运行前兜底 ensure 系统依赖，不在通用 Agent 创建 / 编辑时强制覆盖用户选择。
- 后续模板升级时，应只更新平台托管配置和系统保留字段，不误覆盖用户自定义字段。

### 3.4 Report MCP / Report Skill

- Report MCP 用于读取报告上下文并回写报告结果。
- Report Skill 用于约束报告 Agent 的生成流程、工具使用和输出格式。
- 二者都是系统内置、平台托管、不可编辑、不可删除、不可归档。
- 二者不进入“我的 MCP / 我的 Skill”普通列表，不计入顶部统计。

### 3.5 报告系统能力入口

`/ai-assets` 右上角入口统一命名为“报告系统能力”。

入口只读展示：

- Aida Report MCP 名称、slug、version、transport、endpoint、状态
- Aida Report Skill 名称、slug、version、状态、Markdown
- 标签：系统内置、平台托管
- 管理方式：不可编辑、不可删除、不可归档

## 4. 目标交互流程

### 4.1 新用户启用账号

管理员启用用户或批量添加用户后：

1. 用户可以登录系统。
2. 系统不自动创建 Agent / Skill / MCP。
3. `/ai-assets` 里的用户资产为空或只包含用户主动创建过的资产。

### 4.2 首次进入 `/ai-assets`

1. 页面加载 Agents / Skills / MCP / 定时任务。
2. “我的 Skills / 我的 MCP”按用户资产口径过滤系统内置项。
3. 页面基于 `hasReportAgent` 判断当前用户是否已有 report agent。

### 4.3 没有 report agent

- 在“我的 Agents”tab 表格上方展示引导区。
- 标题：你还没有报告 Agent。
- 按钮：创建默认报告 Agent。
- 如果用户已经有普通 Agent，但没有 report agent，仍展示该引导区。

### 4.4 点击“创建默认报告 Agent”

1. 不弹窗。
2. 按钮进入 loading。
3. 前端调用 `POST /api/v1/ai-assets/report-agents/default`。
4. 后端确保 Report MCP / Skill 可用，然后创建默认报告 Agent。
5. 创建成功后提示“默认报告 Agent 已创建”。
6. 刷新 Agents 列表，引导区消失。

### 4.5 已有 report agent

- 不展示“创建默认报告 Agent”按钮。
- report agent 正常出现在“我的 Agents”列表。
- 用户可进入运行页生成个人日报、周报、小组报告或部门报告，具体权限仍由报告运行权限控制。

### 4.6 查看报告系统能力

- 点击右上角“报告系统能力”。
- 打开只读 Modal。
- 展示 Report MCP / Skill 信息和管理方式。
- 不提供编辑、删除、归档、禁用按钮。

### 4.7 Report MCP / Skill 异常态

- 普通列表不回退展示系统 MCP / Skill。
- “报告系统能力”入口展示加载错误。
- 创建或运行 report agent 时后端返回明确错误；前端保留引导或展示错误提示。
- 如果只是当前 report agent 缺少 Report MCP binding，运行前优先自动修复。

## 5. 后端方案

### 5.1 删除账号启用自动创建

已删除的触发面：

- `AuthHandler.SetDefaultReportAssetsInitializer`
- `AuthHandler.initializeDefaultReportAssetsBestEffort`
- `Login` 中 bootstrap admin 首次建档后的默认资产初始化调用
- `AdminUpdateUser` 中 `local_enabled=false -> true` 后的默认资产初始化调用
- `AdminBatchAddUsers` 中 `local_enabled=true` 后的默认资产初始化调用
- `BackfillDefaultReportAssets` 管理员回填路由

账号启用只负责用户可访问 Aida，不再创建 AI 资产。

### 5.2 新增“创建默认报告 Agent”接口

接口：

```http
POST /api/v1/ai-assets/report-agents/default
```

处理函数：

- `ManagedAgentHandler.CreateDefaultReportAgent`

行为：

1. 当前登录用户调用。
2. 调用 `ListMyAgents` 获取当前用户 Agents。
3. 调用 `selectReportAgentForUser` 判断是否已有 report agent。
4. 已有 report agent：ensure Report Skill / MCP 后幂等返回现有 Agent，不重复创建 Agent。
5. 没有 report agent：ensure Report Skill / MCP，使用 `defaultReportAgentRequest` 创建默认报告 Agent。
6. 创建后写入 `managed_agent_profiles`，`business_type=report`。
7. 返回 `ManagedAgent` 基本信息。

### 5.3 report agent 判断口径

后端：

1. 优先使用 `managed_agent_profiles.business_type=report`。
2. 默认报告 Agent marker 作为补充。
3. 不使用“是否存在任意 Agent”。

前端：

1. 使用 `isReportAgentAsset(agent)`。
2. `business_type=report` 优先。
3. 默认 marker 作为补充。
4. `agents.length > 0` 不参与创建按钮判断。

### 5.4 Report MCP / Skill ensure 边界

保留 ensure，但只在以下路径调用：

1. 创建默认报告 Agent 前。
2. Agent 创建 / 编辑页以 `include_system=true` 拉取资源候选时，确保系统内置 Skill / MCP 可被选择。
3. 运行 report agent 前。

不再在以下路径调用：

1. 用户登录。
2. 管理员启用用户。
3. 管理员批量添加用户。
4. 服务启动。

### 5.5 Report MCP / Skill 保护

MCP：

- `CreateMCPEntry`
- `ArchiveMCPEntry`
- `DeleteMCPEntry`

命中 `h.defaults.ReportMCPSlug + h.defaults.ReportMCPVersion` 时返回：

```json
{
  "code": "REPORT_MCP_PROTECTED",
  "error": "Aida Report MCP 是系统内置资源，不可修改、删除或归档"
}
```

Skill：

- `CreateSkill`
- `ArchiveSkill`
- `DeleteSkill`

命中 `service.ReportSkillSlug + service.ReportSkillVersion` 时返回：

```json
{
  "code": "REPORT_SKILL_PROTECTED",
  "error": "Aida Report Skill 是系统内置资源，不可修改、删除或归档"
}
```

当前代码没有独立 update / disable / rename / version 修改接口；如果后续新增，必须复用同一保护判断。

### 5.6 普通列表过滤

后端：

- `ListSkills` 默认过滤 Aida Report Skill。
- `ListMCPEntries` 默认过滤 Aida Report MCP。
- 当查询参数 `include_system=true` 时，保留系统内置资源，用于 Agent 创建 / 编辑资源候选。

前端：

- `AIAssetsPage.tsx` 再做防御性过滤。
- `AgentCreatePage.tsx` / `AgentEditPage.tsx` 使用 `include_system=true`，不再过滤系统内置资源。

### 5.7 Report Agent 创建 / 编辑 / 运行约束

创建普通 Agent：

- 普通 `business_type=generic` 不自动挂 Report MCP / Skill。
- `business_type=report` 也不因类型自动追加 Report MCP / Skill，资源绑定按用户在 AgentEditor 中的选择保存。

编辑 report agent：

- 如果既有 profile 是 report agent，即使前端 payload 没传 report 类型，后端仍按 report agent 处理。
- 后端不在通用编辑接口里强制追加 Report MCP / Skill；运行 report agent 前再做系统依赖兜底修复。

运行 report agent：

- `StartReportAgentRun` 运行前 ensure Report Skill / MCP。
- 如果缺 Report MCP binding，优先自动修复当前 Agent。
- `agentId=default` 只查找已有 report agent，不创建；找不到返回 404，让前端引导创建。

### 5.8 开发期清理策略

当前项目仍在开发期，不做生产旧数据适配设计，不新增生产启动清理逻辑。

开发/测试环境中如果存在旧自动创建的默认资产，可按以下方式处理：

1. 确认环境是本地、开发或测试环境。
2. 通过外部 Managed Agent 平台或测试脚本清理旧默认 Agent / Skill / MCP。
3. 清理只作为一次性开发环境整理，不写入生产启动流程。
4. 不为了旧脏数据增加长期分支。

## 6. 前端方案

### 6.1 AI Assets 顶部统计

- Agents：包含 report agent，因为 report agent 是用户实例。
- Skills：只统计 `visibleSkills`，不包含 Aida Report Skill。
- MCP：只统计 `visibleMCPEntries`，不包含 Aida Report MCP。
- 定时任务：保持原逻辑。

### 6.2 我的 Skills / 我的 MCP 列表过滤

- 后端普通列表已过滤。
- 前端 `AIAssetsPage.tsx` 再过滤一次。
- 系统 MCP / Skill 不会出现操作按钮。

### 6.3 创建默认报告 Agent 引导区

位置：

- “我的 Agents”tab 表格上方。

展示条件：

- `hasReportAgent === false`。

交互：

- 点击“创建默认报告 Agent”调用 `createDefaultReportAgent()`。
- 不弹配置弹窗。
- 成功后刷新 `managed-agents`、`managed-skills`、`managed-mcp` query。

### 6.4 报告系统能力入口

- 右上角按钮文本：“报告系统能力”。
- 数据来源：`fetchDailyReportAgentIntegration()`。
- 展示 MCP / Skill 基础信息、用途、管理方式、只读 Markdown。

### 6.5 AgentEditor 收敛

- 创建 / 编辑页始终展示 Skills / MCP 选择器。
- 候选项包含用户自建资源和系统内置 Aida Report Skill / MCP。
- 系统内置资源不分组、不置顶、不特殊排序，只显示“系统内置”标识或名称后缀。
- 普通 Agent 和报告 Agent 的资源选择体验保持一致。

### 6.6 异常态、空态、成功态

- 无 report agent：展示引导。
- 有普通 Agent 但无 report agent：仍展示引导。
- 创建成功：提示“默认报告 Agent 已创建”，刷新列表。
- 创建失败：提示错误，引导保留。
- Managed Agent 平台不可用：沿用页面平台错误态。

## 7. 平台配置资产与版本更新策略

### 7.1 平台托管配置

平台托管配置包括：

- Report MCP slug/version/endpoint/auth 约定。
- Report Skill slug/version/Markdown。
- 默认报告 Agent 模板。
- 默认 instructions。
- 默认 start prompt template。
- 系统保留 prompt key。
- credential slot：`AIDA_REPORT_MCP_AUTH`。

### 7.2 后续平台发布更新

可以统一更新：

1. Report MCP endpoint 和鉴权约定。
2. Report Skill Markdown。
3. 默认报告 Agent 模板。
4. 系统保留字段与运行契约。

不能强覆盖：

1. 用户主动修改的普通 Agent。
2. 用户主动创建的 Skill / MCP。
3. 未来允许用户自定义的默认报告 Agent 非系统字段。

### 7.3 `customized=false / true`

当前代码尚无该字段。后续如果允许编辑默认报告 Agent，建议增加：

- `customized=false`：平台可同步默认模板。
- `customized=true`：平台只更新系统保留字段，不覆盖用户自定义内容。

## 8. 开发期清理策略

当前项目没有生产存量用户，不做生产数据搬迁方案。

旧自动创建方案产生的数据视为开发期脏数据：

1. 已自动创建的默认 Agent：可在开发/测试环境中归档或删除，不为其保留长期识别分支。
2. 已自动创建的默认 Skill：可在开发/测试环境中清理；新代码普通列表会隐藏并保护系统 slug/version。
3. 已自动创建的 Aida Report MCP：可在开发/测试环境中清理；新代码普通列表会隐藏并保护系统 slug/version。
4. 清理前必须确认不是生产环境。
5. 清理逻辑不得放入 API 启动流程。
6. 不根据名称模糊清理，避免误删用户真实创建的资产。

## 9. 风险与边界

1. report agent 判断不能用 `agents.length`。
   - 有普通 Agent 但没有 report agent 时仍要展示创建按钮。
2. 前端隐藏必须配合后端保护。
   - 系统 MCP / Skill 即使被直接调接口，也必须返回 409。
3. ensure 重建 MCP 后 binding 仍可能悬空。
   - 当前只在运行当前 report agent 时自动修复当前 Agent，不全量扫描。
4. 外部 Managed Agent 服务 schema 不可控。
   - Aida 需要继续通过 slug/version/profile/marker 做映射。
5. 平台配置更新不能误覆盖用户未来自定义字段。
   - 后续应引入 `customized` 或等价标记。
6. Report MCP / Skill 在底层平台可能仍是用户维度记录。
   - 产品层必须把它们收口为平台托管系统能力，不作为普通用户资产展示。

## 10. 实施步骤

### Phase 1：删除账号启用自动创建

1. 删除 `AuthHandler` 默认资产初始化挂钩。
2. 删除登录、启用用户、批量添加用户里的自动创建调用。
3. 删除管理员回填默认资产路由。

### Phase 2：新增按需创建默认报告 Agent

1. 新增 `POST /api/v1/ai-assets/report-agents/default`。
2. 使用 report agent 判断口径做幂等。
3. 创建前 ensure Report Skill / MCP。
4. 创建后写入 `managed_agent_profiles.business_type=report`。

### Phase 3：过滤系统 MCP / Skill

1. 后端普通列表过滤。
2. 前端普通列表过滤。
3. 顶部统计过滤。
4. AgentEditor 资源选择器改用 `include_system=true` 候选列表，保留系统内置资源。

### Phase 4：后端保护 Report MCP / Skill

1. MCP 创建、归档、删除保护。
2. Skill 创建、归档、删除保护。
3. 统一返回 409 + 保护 code。
4. 后续如新增 update/disable 接口，复用同一保护。

### Phase 5：开发期清理旧自动资产

1. 只在本地/开发/测试环境清理。
2. 不写生产清理逻辑。
3. 不为旧脏数据增加长期分支。

### Phase 6：测试验证

1. 后端单元测试。
2. 前端 typecheck / build / lint。
3. 浏览器或接口级真实账号流程验证。

## 11. 验收标准

1. 新用户启用账号后不会自动出现默认 Agent / Skill / MCP。
2. 进入 AI Assets 且没有 report agent 时展示“创建默认报告 Agent”。
3. 有普通 Agent 但没有 report agent 时仍展示创建按钮。
4. 点击按钮后不弹窗，直接创建默认报告 Agent。
5. 创建成功后刷新 Agents 列表，引导按钮消失。
6. Aida Report MCP 不出现在“我的 MCP”。
7. Aida Report Skill 不出现在“我的 Skill”。
8. Aida Report MCP / Skill 不计入顶部统计。
9. Aida Report MCP 不可删除、不可归档、不可编辑、不可禁用。
10. Aida Report Skill 不可删除、不可归档、不可编辑、不可禁用。
11. “报告系统能力”入口只读展示 Report MCP / Skill。
12. report agent 运行前会 ensure 系统依赖。
13. 缺失 Report MCP binding 的 report agent 运行前会优先自动修复。
14. 普通 Agent 不能走 report agent run。
15. 报告 Agent 能正常进入 managed agent run 流程。
