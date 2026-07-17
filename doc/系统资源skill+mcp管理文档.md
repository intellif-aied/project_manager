# 系统资源 Skill 与 MCP 管理文档

> 最近核对：2026-07-16
>
> 适用项目：`/home/intellif/dev/project_manager`

## 1. 文档定位

本文档定义 Aida 系统级报告 Skill、报告 MCP 和默认报告 Agent 的产品边界、资源归属、权限及版本规则。

生产发布的环境检查、镜像升级、验证和回滚步骤统一见
[AI Coding Console（Aida）部署与发布文档](Aida部署与运维基准.md)。

本文档不再保存容易过期的 SkillID、SHA256、镜像标签和单次测试结果。此类信息应写入对应发布记录。

Managed Agent Platform 只作为 Aida 的运行依赖和接口约束参考。处理本模块时禁止修改：

```text
/home/intellif/dev/sandboxed-agent-platform
```

## 2. 当前产品形态

Aida 的默认报告能力由三部分组成：

```text
系统账号维护的公共 Report Skill
              │
              ▼
普通用户自己的默认 Report Agent
              │
              ▼
Aida 内联 Report MCP + 当前运行用户 Token
```

边界如下：

- 系统报告 Skill 由环境专用系统账号集中发布；
- 普通用户只引用系统 Skill，不创建或复制默认系统 Skill；
- 默认报告 Agent 归属于当前普通用户；
- 报告 MCP 由 Aida 以内联 MCP Server 注入默认 Agent；
- MCP 读取业务数据时使用当前运行用户身份；
- 系统账号 Token 不得用于读取普通用户、小组或部门数据；
- 普通用户自定义 Skill、MCP 和 Agent 能力保持不变。

## 3. 环境隔离

开发和生产共用 Managed Agent Platform 的公共 Registry，因此必须通过稳定 owner 隔离。

| 环境 | 系统账号显示名 | Registry owner / AIHub username | 默认用途 |
| --- | --- | --- | --- |
| 生产 | `aida-system` | `10086` | 生产默认报告 Skill |
| 14.157 测试 | `aida-test` | `100866` | 测试默认报告 Skill |

硬规则：

- 生产配置只能引用 owner `10086`；
- 14.157 只能引用 owner `100866`；
- 显示名不是 SkillRef owner；
- owner 对应的是 AIHub 登录 username，必须保持稳定；
- 禁止生产引用测试 Skill，禁止测试修改、归档或删除生产 Skill；
- 禁止使用真实员工账号、临时测试账号或当前登录用户作为系统 owner fallback。

## 4. 系统 Skill

### 4.1 资源标识

系统 Skill 的逻辑标识为：

```text
<owner>/aida-report@<version>
```

生产示例：

```json
{
  "owner": "10086",
  "slug": "aida-report",
  "version": "<已发布版本>"
}
```

测试示例：

```json
{
  "owner": "100866",
  "slug": "aida-report",
  "version": "<已发布版本>"
}
```

Skill 正文的代码侧来源为：

```text
api/service/daily_report_skill.go
```

环境实际使用的版本由 `MANAGED_AGENT_REPORT_SKILL_VERSION` 决定。文档不永久写死某个版本号。

### 4.2 解析契约

Aida 在创建、修复或运行默认报告 Agent 前，必须执行：

```text
使用当前运行用户对应的 Client 查询公共 Skill Registry
-> 精确匹配配置 owner + slug + version
-> 排除归档资源
-> 要求匹配结果唯一
-> 要求平台返回非空 skill_id 和真实 owner
-> 使用平台返回值构造 SkillRef
-> 成功后才允许修改 Agent
```

当前代码入口：

```text
api/handler/managed_agent.go
resolveSystemReportSkill(...)
```

以下情况必须返回明确配置错误，并保持现有 Agent 不变：

- `MANAGED_AGENT_REPORT_SKILL_OWNER` 为空；
- Managed Agent Platform 未配置；
- 系统 Skill 不存在或已归档；
- 公共 Registry 返回 owner 为空；
- 同一 owner/slug/version 存在多个未归档结果；
- 匹配资源没有 `skill_id`。

禁止：

```text
读取环境 owner
-> 未经 Registry 核验直接拼接 SkillRef
```

也禁止解析失败后回退到当前 Aida 用户名、用户 ID 或空 owner。

### 4.3 默认 Agent 修复

默认报告 Agent 的创建、手动运行、定时运行和依赖修复必须共享同一套系统 Skill 解析规则。

正确顺序：

```text
解析系统 Skill
-> 构造完整 Agent 配置
-> 创建或更新 Agent
-> 启动运行
```

Aida 始终修复用户默认报告 Agent 中的旧 SkillRef 和旧报告 MCP 依赖，但只能修复为已经成功解析的当前环境资源。修复属于默认 Agent 的产品行为，不提供生产环境开关。

## 5. 报告 MCP

### 5.1 执行形态

默认报告 Agent 使用内联 MCP Server：

```text
name: aida-report-mcp
url: <当前环境 Aida>/api/v1/mcp/reports
credential slot: AIDA_REPORT_MCP_AUTH
auth header: Authorization
auth scheme: Bearer
```

默认报告 Agent：

- `MCPServers` 包含 Aida 内联报告 MCP；
- 报告 MCP 对应的 `MCPBindings` 为空；
- 历史报告 MCP Binding 会被移除；
- 用户自己的其他自定义 MCP Binding 保留。

代码入口：

```text
defaultReportMCPServer(...)
ensureCurrentReportMCPServer(...)
removeReportMCPBindings(...)
```

### 5.2 身份与权限

运行时身份固定为：

| 场景 | MCP 使用的身份 |
| --- | --- |
| 手动运行 | 当前登录用户 Token |
| 定时运行 | 定时任务 owner 对应用户 Token |

系统账号只负责维护公共 Skill，不参与用户数据授权。

禁止：

- 把 `10086` 或 `100866` 的系统账号 Token 注入用户报告 Agent；
- 让系统账号跨用户读取 Session、任务、需求或报告；
- 为默认报告 Agent 重新引入 Registry MCP Binding；
- 把 Token 写进 Skill、Agent instructions、日志或文档。

### 5.3 Registry MCP 条目

Aida 仍可能通过 `ensureUserReportMCPEntry(...)` 为当前用户创建或检查一个可见的 MCP Registry 条目，用于 AI 资产列表和兼容性管理。

该条目不是默认报告 Agent 的执行依赖。默认报告执行仍以内联 MCP Server 为准。

检查规则：

- 精确匹配 API 镜像内定义的 slug/version；
- 已存在条目的 URL 与当前环境 URL 不一致时拒绝继续；
- 处理 URL 变更时，修正错误 Registry 数据，或在代码中增加 MCP 版本并发布新 API 镜像；
- 禁止让测试与生产复用同一 slug/version 却指向不同 URL。

## 6. 配置项

### 6.1 必需配置

```env
MANAGED_AGENT_URL=<managed-agent-platform>
MANAGED_AGENT_TOKEN=<生产管理 Token>
MANAGED_AGENT_DEFAULT_ENGINE=<默认执行引擎>
MANAGED_AGENT_DEFAULT_MODEL_ID=<默认模型>
MANAGED_AGENT_REPORT_SKILL_OWNER=<10086 或 100866>
MANAGED_AGENT_REPORT_SKILL_VERSION=<已发布版本>
MANAGED_AGENT_REPORT_MCP_URL=<当前 Aida>/api/v1/mcp/reports
PUBLIC_BASE_URL=<当前 Aida 外部根地址>
```

生产 Compose 从 `PUBLIC_BASE_URL` 派生 API 内部地址。任何秘密只保存在部署环境，不写入仓库。

### 6.2 内容与读取策略

以下内容属于 API 镜像内部的报告运行协议，不是部署配置：

- Skill/MCP slug、MCP 协议版本和凭据槽；
- 默认 Agent 名称、instructions、启动提示和资产修复策略；
- Digest 读取模式、算法版本、脱敏版本和容量预算；
- Digest Worker 开关、并发与批量。

调整这些策略必须修改代码，同时核对 API、Skill 内容、MCP 返回结构和报告回归用例，并发布新的不可变 API 镜像。生产 `.env` 不提供灰度、canary、shadow 或切回原文读取的旁路。

## 7. 发布与版本规则

### 7.1 Skill

- Skill 版本视为不可变资产；
- 内容变化应发布新补丁版本；
- 新版本必须先发布到对应环境 owner，再修改 Aida 配置；
- 配置切换前用普通环境用户确认公共 Registry 可见且唯一；
- 发布记录保存 owner、slug、version、SkillID、内容 SHA256 和发布时间；
- 禁止删除同版本数据库记录后让请求链路“自动重建”；
- 禁止直接操作 Managed Agent Platform 数据库完成日常发布。

### 7.2 MCP

- URL、认证协议或工具协议发生不兼容变化时增加 MCP 版本；
- 同一环境内发现错误 URL 时，应先判断是配置错误还是 Registry 脏数据；
- 测试和生产 URL 不得在同一不可变版本下互相覆盖。

### 7.3 Aida API

系统 Skill 或 MCP 配置变更通常只需要发布 API，不需要发布 Web 或 CLI。具体操作按部署文档的 API-only 流程执行。

## 8. 失败处理

| 现象 | 优先检查 | 正确处理 |
| --- | --- | --- |
| `SKILL_NOT_FOUND` | owner、slug、version、归档状态 | 修正环境配置或发布正确 Skill；不做用户 fallback |
| 系统 Skill 不唯一 | Registry 中重复未归档资产 | 由系统资源维护流程清理重复项，Aida 保持失败 |
| owner 为空 | Agent 平台响应和资源发布账号 | 修正平台资源，不从本地用户猜 owner |
| MCP URL 不一致 | slug/version 是否被其他环境复用 | 修正错误条目或增加 MCP 版本 |
| 默认 Agent 仍引用旧 owner | 系统 Skill 解析结果和资产修复日志 | 先成功解析系统 Skill，再受控修复 Agent |
| 报告越权或数据范围异常 | 运行时 Token、scope、任务 owner | 立即停止发布并检查当前用户身份传递 |
| 日期或星期异常 | `TZ`、calendar_context、业务时区转换 | 固定 `Asia/Shanghai`，禁止模型自行推算 |

## 9. 必须覆盖的回归

系统资源相关改动至少覆盖：

- 本地用户名与 Registry owner 不同仍可正确解析；
- 配置 owner 错误时不发出 Agent Create/PUT；
- owner 为空时禁止 fallback；
- Skill 不存在、归档、重复时均失败且不污染 Agent；
- 历史错误 owner 可以在成功解析后修复；
- 两个普通用户引用同一系统 Skill，MCP 数据仍互相隔离；
- `include_system=true` 只注入当前环境的一份系统报告 Skill；
- `include_system=false` 不注入系统 Skill；
- 手动运行和定时运行均使用各自业务用户身份；
- 自定义 MCP Binding 不因默认报告 Agent 修复而丢失；
- 测试和生产 owner、Skill 版本、MCP URL 不交叉；
- 报告日期、星期和业务时间均按 `Asia/Shanghai`。

详细业务 Case 维护在：

[报告任务测试用例](v1/报告任务测试用例.md)

## 10. 禁止事项

- 禁止修改 Managed Agent Platform 源码来绕过 Aida 资源问题；
- 禁止直接拼接未经 Registry 核验的系统 SkillRef；
- 禁止 owner 解析失败后回退到当前用户；
- 禁止普通用户请求链路自动创建系统报告 Skill；
- 禁止生产引用 `100866`，测试引用 `10086`；
- 禁止系统账号 Token 读取用户业务数据；
- 禁止默认报告 Agent 依赖 Registry MCP Binding；
- 禁止同版本覆盖 Skill 内容；
- 禁止直接删除 Agent 平台数据库记录刷新 Skill；
- 禁止在日志、文档、命令历史或提交中泄露任何 Token。

## 11. Source of Truth

| 内容 | 位置 |
| --- | --- |
| 报告 Skill 正文 | `api/service/daily_report_skill.go` |
| 系统 Skill 解析、默认 Agent、内联 MCP | `api/handler/managed_agent.go` |
| 外部环境绑定 | `api/config/config.go` |
| Digest 产品策略 | `api/internal/reportsource/digest.go` 的 `ProductConfig()` |
| 报告 MCP 接口 | `api/handler` 中 `/api/v1/mcp/reports` 相关实现 |
| 生产发布与回滚 | `doc/Aida部署与运维基准.md` |
| 业务测试 Case | `doc/v1/报告任务测试用例.md` |

最终边界固定为：

> 系统账号集中维护公共报告 Skill；普通用户拥有自己的默认报告 Agent；Aida 按环境内联注入报告 MCP，并始终使用当前运行用户身份访问数据。
