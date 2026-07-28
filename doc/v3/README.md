# AIDA 报告 V3

> 日期：2026-07-27
> 状态：方案持续演进
> 适用范围：默认 Report Agent 生成链路

## 核心需求

V3 当前包含两个相互独立、共同生效的核心需求：

1. [统一报告 Agent 账号与用户 MCP 身份](统一报告Agent账号与用户MCP身份/README.md)：统一模型额度，保持 Report MCP 使用当前 AIDA 用户身份；
2. [Report Agent 两阶段生成](报告Agent两阶段生成/README.md)：同一 Agent 先提交结构化 Report Brief，再根据 Brief 写最终报告。

两个需求共用同一套默认 Report Agent、同一个系统 Report Skill 和同一个 Aida Report MCP。两阶段生成不得改变专用平台账号、用户 MCP Credential、组织权限和报告来源边界。

## V3 总体边界

- Session Digest 继续由现有确定性链路生成，不引入 LLM；
- Report Context 继续作为一次报告运行的不可变事实快照；
- Report Brief 是 Agent 基于 Context 生成的可审计中间产物，不属于事实来源；
- 启用两阶段生成的个人日报只能在 Brief 校验通过后写回；
- 不新增第二个 Skill、第二个 MCP Server 或第二个 Agent Run；
- 普通 Agent、个人 Skill/MCP 数据和非报告业务不受影响。
