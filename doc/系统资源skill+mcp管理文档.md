# 系统资源 Skill 与 MCP 管理

> 最近核对：2026-07-28
>
> 适用项目：/home/intellif/dev/project_manager

## 1. 这份文档解决什么

本文件只负责五件事：

1. 登记测试和生产当前使用的 Report Skill 与 MCP；
2. 说明运行时从哪里读取版本；
3. 固定发布、切换、验证和回退步骤；
4. 固定生产与测试的 owner 隔离；
5. 禁止同版本覆盖、默认版本回退和跨环境引用。

部署命令见 Aida部署与运维基准.md，单次发布结果写入 doc/发布事项。本文件不保存操作日志。

## 2. 当前生效资源

下表记录最近一次已验证的实际运行版本。每次 Skill 或 MCP 切换必须在同一次发布中更新本表。

| 环境 | Skill owner | Skill | MCP | 最近验证 | 验证依据 |
| --- | --- | --- | --- | --- | --- |
| 生产 | 10086 | aida-report@1.0.17 | aida-report-mcp@report-v1 | 2026-07-28 | `20260728-报告Agent两阶段生成生产发布`、公共Registry及真实Run SkillRef |
| 14.157 测试 | 100866 | aida-report@1.0.50 | aida-report-mcp@report-v1 | 2026-07-24 | `20260724-04-report-context-concise-skill` 发布记录、运行环境、公共 Registry 与真实 Session SkillRef |

规则：

- 表中版本与运行环境不一致时，以运行环境为故障现场，不允许直接改表掩盖差异；
- 必须核对环境变量、公共 Registry、默认 Agent SkillRef 三者一致后才能更新本表；
- 新开发分支不得自行填写尚未发布的版本；
- 发布记录必须保存 owner、slug、version、SkillID、SHA256、发布时间和回退版本。

## 3. 唯一运行时配置

API 运行时只读取以下配置：

    MANAGED_AGENT_REPORT_SKILL_OWNER
    MANAGED_AGENT_REPORT_SKILL_VERSION
    MANAGED_AGENT_REPORT_MCP_URL

环境约束：

| 环境 | MANAGED_AGENT_REPORT_SKILL_OWNER |
| --- | --- |
| 生产 | 10086 |
| 14.157 测试 | 100866 |

MANAGED_AGENT_REPORT_SKILL_VERSION 没有默认值。缺失或空白时 API 必须拒绝启动，禁止回退到 1.0.0 或任意历史版本。

代码入口：

- 配置读取和启动校验：api/config/config.go、api/main.go；
- Skill 正文：api/service/daily_report_skill.go；
- Skill 解析和默认 Agent 修复：api/handler/managed_agent.go；
- Report MCP：api/handler/report_mcp*.go。

## 4. 运行边界

    环境系统账号发布公共 aida-report
      -> 普通用户默认报告 Agent 引用公共 Skill
      -> Aida 内联 aida-report-mcp
      -> MCP 使用当前报告用户 Token 读取数据

固定规则：

- 系统账号只发布 Skill，不读取普通用户业务数据；
- 默认报告 Agent 属于普通用户；
- 报告 MCP 是 Aida 内联 Server，不依赖 Registry MCP Binding；
- 手动报告使用当前登录用户身份；
- 定时报告使用定时任务 owner 身份；
- 生产不能引用 100866，测试不能引用 10086；
- 禁止修改 /home/intellif/dev/sandboxed-agent-platform 规避资源问题。

## 5. Skill 解析与 Agent 修复

创建、运行或修复默认报告 Agent 前必须执行：

    使用当前业务用户 Client 查询公共 Skill Registry
      -> 精确匹配配置 owner + aida-report + version
      -> 排除归档资源
      -> 要求结果唯一
      -> 要求 skill_id 和 owner 非空
      -> 使用平台返回值构造 SkillRef
      -> 成功后才允许创建或更新 Agent

以下任一情况必须失败并保持现有 Agent 不变：

- owner 或 version 未配置；
- Skill 不存在、已归档或不唯一；
- Registry 返回空 owner 或空 skill_id；
- Managed Agent Platform 未配置或不可达。

禁止根据环境配置直接拼接 SkillRef，也禁止回退到当前用户名、用户 ID、空 owner 或代码默认版本。

## 6. Skill 发布与切换

Skill 是不可变资产。正文变化只允许发布新补丁版本，不允许覆盖同版本。

固定顺序：

1. 从目标环境当前版本派生新补丁版本；
2. 使用目标环境系统 owner 执行 owner-qualified derive；
3. 上传字段固定为 file:SKILL.md；
4. 核对响应 owner、slug、version 和 SHA256；
5. 用普通环境用户查询公共 Registry，确认唯一、未归档且正文哈希一致；
6. 修改 MANAGED_AGENT_REPORT_SKILL_VERSION；
7. 重启 API，触发一个新报告 Run；
8. 核对默认 Agent SkillRef、Session 实际加载版本和报告写回；
9. 更新本文件当前生效资源表和对应发布记录。

生产 derive 目标：

    POST /api/skill/10086/aida-report/derive
    base_version=<当前生产版本>
    version=<批准的新补丁版本>
    file:SKILL.md=@<API 生成的 SKILL.md>

测试环境使用 owner 100866。禁止普通 POST /api/skill 冒充系统 owner。

## 7. MCP 版本规则

默认执行 MCP：

    name: aida-report-mcp
    url: <当前环境 Aida>/api/v1/mcp/reports
    credential slot: AIDA_REPORT_MCP_AUTH
    auth: Authorization Bearer

- URL、认证方式或工具协议发生不兼容变化时才增加 MCP 版本；
- Skill 正文变化不自动触发 MCP 升级；
- 测试和生产不能用同一 Registry MCP 版本指向不同 URL；
- Registry MCP 条目只用于资产展示和兼容检查，不是默认 Agent 执行依赖。

## 8. 发布验证

每次系统资源发布至少验证：

- 环境变量 owner/version 与本文件一致；
- Registry 中目标 Skill 唯一、未归档、SHA256 正确；
- 新 Run 的默认 Agent 精确引用目标 owner/slug/version；
- Agent Session 实际加载目标 Skill；
- MCP 使用当前业务用户身份；
- get_report_context、write_report_result 和失败写回正常；
- 两个普通用户的数据权限相互隔离；
- 定时和手动报告均成功；
- 测试与生产资源没有交叉；
- 仓库和日志中没有 Token、Secret 或生产正文。

## 9. 回退

回退只切换到上一份已验证的不可变版本：

1. 将 MANAGED_AGENT_REPORT_SKILL_VERSION 恢复为发布记录中的上一版本；
2. 重启 API；
3. 触发新 Run，核对 Agent SkillRef 和实际加载版本；
