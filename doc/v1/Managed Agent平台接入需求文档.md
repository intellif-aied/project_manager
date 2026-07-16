# Managed Agent 平台接入需求文档

版本：v0.1
状态：需求草案
依据：`3rdparty/openapi.json`，Sandboxed Agent Platform Gateway v0.3.0

---

## 1. 背景

Aida 当前围绕“需求拆解、任务执行、Session 上报、日报生成”提供研发协作管理能力。现有日报生成链路主要基于用户上传的 Claude Code / Codex session，由服务端默认 report generator 生成草稿。

后续平台需要接入一个 Managed Agent 平台。该平台已提供以下能力：

1. Skill 注册、版本管理、文件读取和反向引用查询。
2. MCP 注册、版本管理、归档和凭据绑定。
3. 用户个人 Managed Agent 创建、更新、归档和列表查询。
4. Agent 运行时 Session 创建、事件流、追问、打断、结束和结果查询。
5. Task 式提交、状态查询、结果查询、取消和反馈。
6. 凭据托管和文件上传。

本需求的核心是让 Aida 支持“我的 Skills、我的 MCP、我的 Agent”，但 Agent 的实际运行不在 Aida 本地实现，而是通过 Managed Agent 平台接口完成。后续日报生成、周报生成、需求总结、风险摘要等 AI 任务，可以选择用户自己的 Agent 来生成。

---

## 2. 目标

### 2.1 产品目标

1. 用户可以在 Aida 中管理自己的 Skills。
2. 用户可以在 Aida 中管理自己的 MCP。
3. 用户可以在 Aida 中创建和维护自己的 Managed Agent。
4. Agent 创建时可以绑定用户选择的 Skills、MCP、子 Agent、凭据槽、默认模型和启动模板。
5. Aida 调用 Agent 时，统一走 Managed Agent 平台的运行接口。
6. 日报生成等后续 AI 任务可以选择个人 Agent 执行，而不是固定使用系统内置默认生成器。
7. Aida 保留业务事实源：需求、任务、Session、日报、权限和审计仍归 Aida 管理。

### 2.2 技术目标

1. 将 Managed Agent 平台作为外部运行时接入，不复制其 sandbox、事件流、归档、文件浏览等底层能力。
2. Aida 保存必要的外部资源引用，例如 `skill owner/slug/version`、`mcp owner/slug/version`、`agent_id`、`agent_version_id`、`session_id`、`task_id`。
3. 对外部接口做统一封装，避免业务页面直接拼 Managed Agent API。
4. 运行过程支持状态轮询和 SSE 事件流两种模式，P0 可优先采用轮询。
5. 生成结果必须回写到 Aida 对应业务对象，例如日报草稿、任务建议、风险摘要。
6. Managed Agent 平台鉴权由 Aida 服务端配置文件统一管理，所有外部调用使用同一个平台 token，不在前端暴露 token。

---

## 3. 非目标

P0 不做以下事情：

1. 不在 Aida 内实现 Agent sandbox。
2. 不复制 Managed Agent 平台的 Ops Console。
3. 不做完整 Skill 编辑器和在线文件树编辑。
4. 不做复杂 MCP 调试台。
5. 不做多人协同发布 Skill 的完整治理流程。
6. 不把日报事实源迁移到 Managed Agent 平台。
7. 不允许 Agent 运行结果自动修改需求、任务、日报，必须由用户确认后写入。
8. 不把外部平台凭据明文保存到 Aida 数据库。

---

## 4. 术语

| 术语 | 定义 |
| --- | --- |
| Managed Agent 平台 | `3rdparty/openapi.json` 描述的外部 Agent Gateway。 |
| Skill | 可版本化的 Agent 能力包，包含 `SKILL.md` 或 artifact。 |
| MCP | Agent 可绑定的外部工具服务定义，支持 transport、command/url、headers、credential。 |
| 我的 Agent | 当前用户在 Managed Agent 平台创建的个人 Agent。 |
| Agent Session | 交互式运行，一次创建后可通过事件流观察、追问、打断和结束。 |
| Agent Task | 一次性任务提交，适合后台生成日报、摘要、评审结果等。 |
| 个人 Agent 生成 | 用户选择自己的 Agent 执行业务生成任务，例如日报草稿。 |

---

## 5. 用户故事

1. 作为员工，我希望把自己的日报 Skill 上传到平台，并在生成日报时使用它。
2. 作为员工，我希望注册自己的 MCP，例如内部知识库、代码搜索或工单系统，使我的 Agent 可以调用。
3. 作为员工，我希望创建一个个人日报 Agent，绑定默认日报 Skill 和必要 MCP。
4. 作为员工，我希望在 Aida 生成日报时选择我的个人 Agent，并看到生成进度和结果。
5. 作为 TL，我希望用自己的 Agent 总结团队 Session、任务进展和阻塞风险。
6. 作为 PM，我希望用自己的 Agent 汇总需求状态和验收风险，但结果必须可编辑确认。
7. 作为管理员，我希望知道外部 Agent 运行失败时是凭据问题、平台问题、超时还是业务生成失败。

---

## 6. 总体产品结构

```text
Aida
├─ 控制台
├─ 需求看板
├─ 日报 / 周报
└─ 我的 AI 资产
   ├─ 我的 Skills
   ├─ 我的 MCP
   └─ 我的 Agents

Managed Agent 平台
├─ Skill Registry
├─ MCP Registry
├─ My Agents
├─ Sessions Runtime
├─ Tasks Runtime
└─ Credentials / Files
```

Aida 页面展示和业务状态以自身数据库为准；AI 资产和运行状态通过 Managed Agent 平台同步或实时查询。

---

## 7. 功能需求

### 7.1 我的 Skills

用户可以在 Aida 中完成：

1. 查看 Skill 列表。
2. 按作用域筛选 Skill，固定支持 `mine`、`public`、`all`。
3. 注册一个新的 Skill 版本。
4. 查看 Skill 详情和版本列表。
5. 查看某个版本的文件列表。
6. 读取某个版本的 `SKILL.md` 或其他文本文件。
7. 下载 Skill artifact。
8. 归档或恢复自己的 Skill 版本。
9. 查看 Skill 被哪些 Agent 使用。

Managed Agent 接口映射：

| 能力 | 接口 |
| --- | --- |
| 注册 Skill 版本 | `POST /api/skill` |
| Skill 列表 | `GET /api/skill/list?scope=` |
| Skill 详情 | `GET /api/skill/{owner}/{slug}` |
| 派生新版本 | `POST /api/skill/{owner}/{slug}/derive` |
| 文件列表 | `GET /api/skill/{owner}/{slug}/{version}/files` |
| 读取文件 | `GET /api/skill/{owner}/{slug}/{version}/file?path=` |
| 下载 artifact | `GET /api/skill/{owner}/{slug}/{version}/download` |
| 被引用列表 | `GET /api/skill/{owner}/{slug}/{version}/used-by` |
| 归档/恢复 | `POST /api/skill/{slug}/{version}/archive` |

P0 约束：

1. 注册 Skill 支持上传 artifact 或直接提交 `skill_md`。
2. `version` 不能使用字面值 `latest`。
3. 同一 `slug@version` 重复发布应提示失败。
4. Aida 只保存 Skill 引用，不保存完整 artifact。
5. 读取 Skill 文件时只展示文本文件；二进制文件只显示元信息。
6. 平台会提供日报生成等基础 Skill；Aida P0 可优先引用 Managed Agent 平台已有 Skill，不强制用户先上传 Skill。

### 7.2 我的 MCP

用户可以在 Aida 中完成：

1. 创建 MCP entry。
2. 查看 MCP 列表。
3. 按作用域筛选 MCP，固定支持 `mine`、`public`、`all`。
4. 归档或恢复 MCP entry。
5. 删除 MCP entry。
6. 将 MCP 绑定到个人 Agent。

MCP entry 支持字段：

| 字段 | 说明 |
| --- | --- |
| `slug` / `version` | MCP 唯一版本标识。 |
| `name` / `description` | 展示名称和说明。 |
| `transport` | 传输类型，例如 stdio、http、sse 等，以外部平台实际支持为准。 |
| `command` / `args` | 本地命令型 MCP 启动参数。 |
| `url` | 远程 MCP 地址。 |
| `headers` | 远程请求头。 |
| `requires_credential` | 是否需要凭据。 |
| `credential_env` | 注入凭据的环境变量名。 |
| `auth_scheme` / `auth_header` | 认证方案和 header 名称。 |
| `env` | MCP 启动环境变量。 |

Managed Agent 接口映射：

| 能力 | 接口 |
| --- | --- |
| 注册 MCP | `POST /api/mcp` |
| MCP 列表 | `GET /api/mcp/list?scope=` |
| 删除 MCP | `DELETE /api/mcp/{slug}/{version}` |
| 归档/恢复 | `POST /api/mcp/{slug}/{version}/archive` |

P0 约束：

1. Aida 不保存 MCP 凭据明文。
2. 对 `headers`、`env` 中疑似 secret 的值做脱敏展示。
3. 需要凭据的 MCP 必须通过凭据槽绑定，不允许在 Agent 配置中硬编码 secret。

### 7.3 我的 Agents

用户可以在 Aida 中完成：

1. 查看个人 Agent 列表。
2. 创建个人 Managed Agent。
3. 更新个人 Agent 配置。
4. 归档或恢复个人 Agent。
5. 选择 Agent 的 Skills。
6. 选择 Agent 的 MCP bindings。
7. 配置子 Agent。
8. 配置默认模型、启动提示模板和输入参数。
9. 配置凭据槽，并在运行时选择具体凭据。

Agent 配置核心字段：

| 字段 | 说明 |
| --- | --- |
| `agent_id` | 外部平台 Agent ID。 |
| `name` / `description` | 展示名称和说明。 |
| `engine` | Agent 引擎类型。 |
| `instructions` | Agent 系统指令。 |
| `skills` | 绑定的 Skill 引用，包含 `owner/slug/version`。 |
| `mcp_bindings` | 绑定的 MCP 引用，包含 `owner/slug/version/credential_slot`。 |
| `subagents` | 子 Agent 引用，包含 `agent_id/version/alias`。 |
| `credential_slots` | 凭据槽定义。 |
| `default_model_id` | 默认模型。 |
| `start_prompt_template` | 启动提示模板。 |
| `input_schema` | 运行入参 schema。 |
| `default_bindings` | 默认凭据或参数绑定。 |

Managed Agent 接口映射：

| 能力 | 接口 |
| --- | --- |
| 我的 Agent 列表 | `GET /api/my/agents` |
| 创建个人 Agent | `POST /api/my/agents` |
| 更新个人 Agent | `PUT /api/my/agents/{agentId}` |
| 归档/恢复个人 Agent | `POST /api/my/agents/{agentId}/archive` |
| 可见 Agent 列表 | `GET /api/agent/list` |

P0 约束：

1. Aida 优先使用 `/api/my/agents` 管理当前用户个人 Agent。
2. 每次有效配置变更由外部平台生成新的 immutable Agent Version。
3. Aida 必须保存当前业务配置引用到 `agent_id` 和 `current_version_id`，避免后续生成结果无法追溯。
4. 创建 Agent 时至少要求填写名称、引擎、指令或启动模板。

### 7.4 凭据管理

用户可以在 Aida 中完成：

1. 保存外部平台凭据。
2. 查看凭据列表。
3. 删除凭据。
4. 在运行 Agent 时选择凭据覆盖。

Managed Agent 接口映射：

| 能力 | 接口 |
| --- | --- |
| 保存凭据 | `POST /api/credential` |
| 凭据列表 | `GET /api/credential/list` |
| 删除凭据 | `DELETE /api/credential/{credentialId}` |

P0 约束：

1. Aida 不落库保存 `value`。
2. Aida 只保存外部 `credential_id`、名称、用途和绑定关系。
3. 凭据列表展示必须脱敏。
4. 删除凭据前提示受影响的 Agent / MCP。

### 7.5 Agent 运行时

Agent 运行必须使用 Managed Agent 平台接口。Aida 不直接启动容器、不直接执行模型、不直接挂载 MCP。

运行模式分两类：

#### 7.5.1 Session 模式

适合交互式任务，例如用户和个人 Agent 连续对话生成日报草稿、追问和修改。

接口映射：

| 能力 | 接口 |
| --- | --- |
| 创建 Session | `POST /api/session` |
| Session 列表 | `GET /api/session/list` |
| Session 详情 | `GET /api/session/{sessionId}` |
| SSE 事件流 | `GET /api/session/{sessionId}/events` |
| 追问 | `POST /api/session/{sessionId}/events` |
| 打断 | `POST /api/session/{sessionId}/interrupt` |
| 结束 | `POST /api/session/{sessionId}/end` |
| rerun 预填 | `GET /api/session/{sessionId}/rerun-prefill` |

创建 Session 请求关键字段：

| 字段 | 说明 |
| --- | --- |
| `agent_id` | 必填，运行的 Agent。 |
| `message` | 初始用户消息。 |
| `model_id` | 可选，覆盖默认模型。 |
| `start_prompt_values` | 启动模板变量。 |
| `credential_overrides` | 凭据槽到凭据 ID 的覆盖。 |
| `input_files` | 输入文件。 |

#### 7.5.2 Task 模式

适合后台一次性生成，例如日报草稿、周报草稿、需求风险摘要。

接口映射：

| 能力 | 接口 |
| --- | --- |
| 提交 Task | `POST /api/task/submit` |
| Task 列表 | `GET /api/task/list` |
| Task 状态 | `GET /api/task/{taskId}/status` |
| Task 结果 | `GET /api/task/{taskId}/result` |
| Task 事件流 | `GET /api/task/{taskId}/events` |
| 取消 Task | `POST /api/task/{taskId}/cancel` |
| 反馈 | `GET/PUT /api/task/{taskId}/feedback` |
| 指标反馈 | `GET/POST /api/task/{taskId}/metric-feedback` |

提交 Task 请求关键字段：

| 字段 | 说明 |
| --- | --- |
| `agent_id` | 必填，运行的 Agent。 |
| `model_id` | 可选，覆盖默认模型。 |
| `params` | 字符串参数，承载业务上下文引用或 prompt 参数。 |
| `input_files` | 输入文件 ID 列表。 |

P0 推荐：

1. 日报生成优先使用 Task 模式。
2. 需要用户连续修改时，再创建 Session 模式。
3. Aida 保存外部 `task_id/session_id` 和业务对象的映射。
4. 轮询 `status/result` 即可完成 P0；SSE 可作为 P1 优化实时体验。

---

## 8. 日报生成接入需求

### 8.1 当前日报生成问题

当前 Aida 日报生成偏向系统默认生成器。用户可以上传或选择 Skill，但缺少“使用我的个人 Agent 运行”的完整闭环。

### 8.2 目标流程

```text
用户打开日报生成
        ↓
选择日期和 session
        ↓
选择生成方式
  ├─ 系统默认生成器
  └─ 我的个人 Agent
        ↓
选择 Agent / 模型 / 凭据覆盖
        ↓
Aida 组装业务上下文
        ↓
POST /api/task/submit
        ↓
轮询 Task 状态和结果
        ↓
解析生成结果为日报草稿和任务建议
        ↓
用户编辑确认
        ↓
写入 Aida daily_reports / task suggestions
```

### 8.3 上下文输入

Aida 调用个人 Agent 生成日报时，必须提供结构化上下文：

1. 当前用户信息。
2. 报告日期。
3. 用户选择的 Aida session 列表。
4. session 摘要、时间、模型、token、关联任务和需求。
5. 当天任务进度。
6. 阻塞、deadline、风险项。
7. 输出协议要求。

P0 可以把上下文放入 `params` 或上传为输入文件，具体取决于 Managed Agent 平台对 `params` 长度和文件接口的实际限制。

### 8.4 输出协议

个人 Agent 生成结果由 Managed Agent 平台负责产出并上传，Aida 通过 Task 结果接口读取。结果内容必须能被 Aida 解析，建议统一为 JSON：

```json
{
  "report_markdown": "## 今日完成\n...",
  "task_progress_suggestions": [
    {
      "task_id": "task_1",
      "suggested_status": "in_progress",
      "suggested_progress": 70,
      "reason": "基于 session 证据的说明",
      "evidence_session_ids": ["session_1"]
    }
  ],
  "risks": [
    {
      "type": "blocked",
      "title": "依赖接口未完成",
      "evidence": "session 中提到等待接口联调"
    }
  ]
}
```

解析规则：

1. `report_markdown` 为空则生成失败。
2. 任务建议只展示，不自动更新。
3. 建议中的 `task_id` 必须属于当前用户可访问范围。
4. 证据 session 必须来自本次选择的 session。
5. JSON 解析失败时保留原始结果供用户查看，并标记为需要人工处理。
6. Aida 不要求 Managed Agent 运行产生的 session 同步进入 Aida 原有 session 统计体系；Aida 只记录外部 `task_id/session_id` 作为生成溯源。

### 8.5 回写规则

1. 生成完成后先写入日报草稿，不直接发送。
2. 用户点击保存后写入 Aida `daily_reports`。
3. 用户确认任务建议后，才调用任务更新接口。
4. Aida 保存生成来源：`managed_agent_task_id`、`agent_id`、`agent_version_id`、`model_id`、`selected_session_ids`。

---

## 9. 数据需求

建议新增或扩展以下 Aida 数据对象。

### 9.1 外部 AI 资产引用

```text
ai_asset_refs
├─ id
├─ user_id
├─ asset_type: skill | mcp | agent | credential
├─ external_owner
├─ external_slug
├─ external_version
├─ external_id
├─ display_name
├─ archived
├─ metadata_json
├─ created_at
└─ updated_at
```

用途：

1. 缓存列表展示。
2. 记录 Aida 业务对象引用的外部资源。
3. 支持外部平台不可用时展示最后一次同步状态。

### 9.2 AI 运行记录

```text
ai_runs
├─ id
├─ user_id
├─ business_type: daily_report | weekly_report | requirement_summary | risk_summary
├─ business_id
├─ runtime_type: managed_task | managed_session
├─ agent_id
├─ agent_version_id
├─ external_task_id
├─ external_session_id
├─ model_id
├─ status
├─ input_ref_json
├─ output_ref_json
├─ error_message
├─ started_at
├─ finished_at
└─ created_at
```

用途：

1. 追踪外部运行状态。
2. 支持失败重试和审计。
3. 关联日报草稿、任务建议和风险摘要。

### 9.3 日报扩展字段

建议扩展 `daily_reports` 或新增关联表记录：

1. `generation_mode`: `default` 或 `managed_agent`。
2. `managed_agent_run_id`。
3. `agent_id`。
4. `agent_version_id`。
5. `model_id`。
6. `selected_session_ids`。

---

## 10. 权限与安全

1. 用户只能管理自己的个人 Agent。
2. 用户只能使用自己有权限访问的 Skills、MCP 和凭据。
3. 公开 Skill/MCP 可被引用，但归档版本不可新绑定。
4. 已生成历史必须保留当时的 `version` 引用，不随 latest 漂移。
5. Aida 不保存凭据明文，只保存外部凭据 ID 和绑定关系。
6. Agent 运行上下文不得包含用户无权限访问的需求、任务和 session。
7. 个人 Agent 生成团队/部门报告时，必须按 Aida 角色权限裁剪上下文。
8. 外部平台错误信息展示给用户时必须脱敏。
9. 所有外部调用需记录审计日志：调用人、业务类型、外部资源、开始/结束时间、状态。
10. Managed Agent 平台 token 只允许存在于 Aida 服务端配置文件或运行时环境变量中，前端和数据库不保存该 token。

---

## 11. 错误处理

| 场景 | 处理 |
| --- | --- |
| Managed Agent 平台不可用 | 生成入口提示不可用，可回退系统默认生成器。 |
| Agent 不存在或已归档 | 禁止发起运行，提示重新选择 Agent。 |
| Skill/MCP 版本不存在 | 禁止保存 Agent 配置或提示重新绑定。 |
| 凭据缺失 | 创建运行前拦截，提示绑定凭据。 |
| Task 超时 | 标记 `ai_runs.status=timeout`，允许重试。 |
| Task 失败 | 展示外部错误摘要，保留运行记录。 |
| 输出 JSON 解析失败 | 保存原始输出，进入人工编辑模式。 |
| 任务建议越权 | 丢弃该建议并记录安全告警。 |

---

## 12. 分期范围

### P0：Managed Agent 运行接入闭环

1. 增加“我的 AI 资产”入口。
2. 支持查看我的 Skills、我的 MCP、我的 Agents。
3. 支持创建和更新个人 Agent，绑定已有 Skill/MCP。
4. 支持选择个人 Agent 生成日报草稿。
5. 通过 `POST /api/task/submit` 发起生成。
6. 通过 `GET /api/task/{taskId}/status` 和 `GET /api/task/{taskId}/result` 获取结果。
7. 解析 JSON 输出为日报草稿和任务建议。
8. 保存日报时回写 Aida。
9. 保留外部运行记录和错误信息。

### P1：资产管理增强

1. 支持 Skill 上传、派生版本、查看文件树和下载 artifact。
2. 支持 MCP 创建、归档、删除和凭据绑定。
3. 支持凭据保存、列表和删除。
4. 支持 Session 模式生成和追问。
5. 支持 SSE 展示实时运行事件。
6. 支持周报、需求摘要、风险摘要选择个人 Agent。

### P2：治理与运营

1. 支持管理员查看平台健康、运行诊断和强制终止。
2. 支持 Agent 使用统计、质量反馈和指标反馈。
3. 支持团队共享 Agent 模板。
4. 支持外部运行结果成本统计。
5. 支持失败重试队列和批量生成。

---

## 13. 验收标准

### 13.1 我的 AI 资产

1. 用户进入“我的 AI 资产”后，可以看到 Skills、MCP、Agents 三类资源。
2. 列表数据来自 Managed Agent 平台接口。
3. 外部平台不可用时，页面明确提示同步失败，不影响 Aida 其他页面。
4. Skill/MCP/Agent 的 `owner/slug/version/agent_id` 信息展示准确。

### 13.2 我的 Agent

1. 用户可以创建个人 Agent。
2. 用户可以绑定至少一个 Skill。
3. 用户可以绑定至少一个 MCP。
4. 用户保存配置后，Managed Agent 平台返回 `agent_id` 和 `managed_version`。
5. Aida 能记录该 Agent 引用，用于后续日报生成。

### 13.3 日报生成

1. 用户在日报生成入口可以选择“我的个人 Agent”。
2. 用户选择 Agent 后，Aida 调用 Managed Agent 的 Task 接口生成。
3. 生成过程有明确状态：排队、运行中、成功、失败、超时。
4. 成功后展示可编辑 Markdown 草稿。
5. 任务进展建议只展示，不自动更新任务。
6. 保存日报后，日报内容和生成来源可追溯。
7. Managed Agent 平台失败时，用户可以回退系统默认生成器。

### 13.4 安全

1. 用户不能使用无权限的 Aida session 生成日报。
2. 用户不能看到凭据明文。
3. Agent 输出中越权任务建议不会被应用。
4. 历史日报能追溯到当时使用的 `agent_id`、`agent_version_id` 和外部运行 ID。

---

## 14. 接口清单

本需求涉及的 Managed Agent 平台接口：

| 模块 | 接口 |
| --- | --- |
| Auth | `POST /api/auth/login` |
| Skills | `POST /api/skill`、`GET /api/skill/list`、`GET /api/skill/{owner}/{slug}`、`POST /api/skill/{owner}/{slug}/derive`、`GET /api/skill/{owner}/{slug}/{version}/files`、`GET /api/skill/{owner}/{slug}/{version}/file`、`GET /api/skill/{owner}/{slug}/{version}/download`、`GET /api/skill/{owner}/{slug}/{version}/used-by` |
| MCP | `POST /api/mcp`、`GET /api/mcp/list`、`DELETE /api/mcp/{slug}/{version}`、`POST /api/mcp/{slug}/{version}/archive` |
| My Agents | `GET /api/my/agents`、`POST /api/my/agents`、`PUT /api/my/agents/{agentId}`、`POST /api/my/agents/{agentId}/archive` |
| Sessions | `POST /api/session`、`GET /api/session/list`、`GET /api/session/{sessionId}`、`GET /api/session/{sessionId}/events`、`POST /api/session/{sessionId}/events`、`POST /api/session/{sessionId}/interrupt`、`POST /api/session/{sessionId}/end` |
| Tasks | `POST /api/task/submit`、`GET /api/task/list`、`GET /api/task/{taskId}/status`、`GET /api/task/{taskId}/result`、`GET /api/task/{taskId}/events`、`POST /api/task/{taskId}/cancel` |
| Credentials | `POST /api/credential`、`GET /api/credential/list`、`DELETE /api/credential/{credentialId}` |
| Files | `POST /api/file/upload` |

---

## 15. 待确认问题

1. `POST /api/task/submit` 的 `params` 长度限制是多少，日报上下文是否必须走文件上传。
2. Agent Task 输出是否有固定 result 格式，还是只能从 `result` 字符串中解析。
3. MCP transport 的枚举值和运行网络边界。
4. 凭据 ID 是否可以跨 Agent/MCP 复用，是否支持团队共享。
5. 外部平台是否返回成本、token、耗时等可统计字段。
