# AIDA 报告 V3

> 日期：2026-07-28
> 状态：方案持续演进
> 适用范围：默认 Report Agent 生成链路

## 核心需求

V3 当前包含五个相互独立的核心需求：

1. [统一报告 Agent 账号与用户 MCP 身份](统一报告Agent账号与用户MCP身份/README.md)：统一模型额度，保持 Report MCP 使用当前 AIDA 用户身份；
2. [Report Agent 两阶段生成](报告Agent两阶段生成/README.md)：同一 Agent 先提交结构化 Report Brief，再根据 Brief 写最终报告；
3. [生产日报价值观察 V1](生产日报价值观察V1/README.md)：管理员按日查看生产个人日报的 AI 覆盖、生成稳定性、用户修改、内容保留和下游采用；
4. [日报生成方案评测 V2](日报生成方案评测V2/README.md)：用固定测试集、AI Review 和 Gold Review 比较从 Source Evidence 到 Final 的完整生成方案。
5. [报告语义审核与有界纠错](报告Agent语义审核与有界纠错/README.md)：Project Memory 只提供项目候选；系统默认个人日报在写回前执行一次独立语义审核，审核失败时保守降级但不阻断日报。

账号与两阶段需求共用同一套默认 Report Agent、系统 Report Skill 和 Aida Report MCP。生产价值观察记录实际生成稿和用户结果；测试服方案评测比较候选 Variant。两者共享 Generation Snapshot、Variant Manifest 和确定性差异基础，但产品入口、数据和结论彼此独立。

## V3 总体边界

- Session Digest 继续由现有确定性链路生成，不引入 LLM；
- Report Context 继续作为一次报告运行的不可变事实快照；
- Report Brief 是 Agent 基于 Context 生成的可审计中间产物，不属于事实来源；
- 启用两阶段生成的个人日报只能在 Brief 校验通过后写回；
- 默认报告生成仍只有一个 Report Agent Run；仅系统默认个人日报允许追加一个系统归属、用户不可见的 Reviewer Agent Run，并使用隔离的 Review Skill 与 Review MCP；
- Reviewer 只能审核冻结 Facts、候选 Brief 和只读项目候选，不能读取其他用户数据、更新 Project Memory 或自由扩展日报事实；
- 普通 Agent、个人 Skill/MCP 数据和非报告业务不受影响。
