# V3 调整：系统与个人报告 Agent 双通道

> 文档状态：已确认并进入开发
> 日期：2026-07-29
> 覆盖范围：本目录早期文档中的统一默认执行与个人资产前端屏蔽规则

## 1. 目标

报告生成保留稳定的系统默认能力，同时恢复用户沿用个人 Skill 的方式：

1. 未选择个人默认 Agent 时，日报、周报继续使用系统管理员归属的 Report Agent；
2. 用户将自己的报告 Agent 设为默认后，日报、周报使用该个人 Agent 及其模型、Skill、MCP 配置；
3. 系统 Agent 出现在 Agent 列表中，只允许设为默认，不允许查看、编辑、手动运行或删除；
4. Report MCP 始终使用当前 AIDA 用户身份，系统与个人执行通道均不得改变报告数据权限；
5. 恢复 AI 资产菜单和个人 Agent、Skill、MCP 管理入口。

## 2. 产品规则

| 场景 | Agent 与模型身份 | Skill/MCP 配置 | Report MCP 数据身份 |
|---|---|---|---|
| 未设置个人默认 | 系统专用账号 | 系统 Report Agent 固定配置 | 当前 AIDA 用户 |
| 系统 Agent 设为默认 | 系统专用账号 | 系统 Report Agent 固定配置 | 当前 AIDA 用户 |
| 个人 Agent 设为默认 | 当前用户的平台身份 | 个人 Agent 保存的配置 | 当前 AIDA 用户 |

个人默认 Agent 不可用时，不静默切回系统 Agent，避免用户误以为个人 Skill 已生效。用户需要回到 Agent 列表重新选择。

## 3. 权限模型

Agent 列表返回来源与能力，不依赖前端按名称猜测：

```json
{
  "source": "system",
  "permissions": {
    "can_run": false,
    "can_set_default": true,
    "can_view": false,
    "can_edit": false,
    "can_archive": false
  }
}
```

系统 Agent 只返回列表展示所需的名称、说明、引擎、版本、报告类型与默认状态，不返回 instructions、模型、Skill、MCP 或凭证绑定。

## 4. 实现边界

- 复用 `managed_agent_profiles.is_default_report` 保存用户选择，不新增表和 migration；
- `/ai-assets/agents` 合并当前用户的个人 Agent 与一个只读系统 Agent 投影；
- `/ai-assets/report-agents/{agentId}/default` 同时接受个人报告 Agent 和系统 Agent；
- `/ai-assets/report-agents/default/runs` 先读取用户默认配置，再选择个人客户端或系统专用客户端；
- 只有系统通道写入 `system_report_account=true` 并固定系统报告模型；个人通道沿用个人 Agent 的模型配置；
- 不修改报告来源、Digest、Report Context、写回协议、组织权限与其他业务。

## 5. 验收标准

1. Agent 列表同时显示个人 Agent 和带“系统”标识的系统 Agent；
2. 系统 Agent 只有“设为默认”操作，响应不暴露内部配置；
3. 个人 Agent 可创建、编辑、运行、删除和设为默认；
4. 选择系统 Agent 后从报告页生成，平台 Session 使用系统专用账号；
5. 选择个人 Agent 后从报告页生成，平台 Session 使用当前用户账号及该 Agent 的 Skill；
6. 两条通道调用 Report MCP 时，读取和写回都属于当前 AIDA 用户；
7. AI 资产菜单、个人 Skill 和个人 MCP 管理入口可见；
8. 无数据库迁移，既有报告与历史运行不受影响。
