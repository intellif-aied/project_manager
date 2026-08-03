# Project Memory 可选长期上下文契约

> 日期：2026-08-03  
> 状态：代码完成并通过隔离测试服真实回放  
> 取代范围：文档 21 中 `anchor_fact_refs + workstream_subject + max_workstreams` 的强归并语义

## 定位

Project Memory 类似 Codex 的长期工作上下文。它让 Report Agent 知道用户近期可能涉及的项目名称和别名，但不替 Agent 判断当天工作归属。

优先级固定为：

1. 当天 Session、Digest 和 Evidence Facts 是唯一事实来源；
2. 当天用户明确表达的项目名称、目标和边界是最高命名依据；
3. Project Memory 只用于辅助理解连续性、术语和可能的项目名称；
4. 冲突或无法确认时忽略 Memory，使用当天 Facts 支持的中性名称。

## Context 契约

Report Context 只暴露历史项目名称、短 alias、置信度和 `related_fact_refs`。`related_fact_refs` 只表示名称或别名存在可能联系，不是项目归属证明。

以下强制语义移除：

- `anchor_fact_refs`；
- `workstream_subject`；
- `max_workstreams`；
- “命中后必须采用历史项目名”；
- “同一 Hint 必须归并为一条主线”。

未命中的近期项目仍可作为 `candidate_only` 背景，但通常应忽略；只有当天 Facts 自身明确给出对应项目名称或归属时才可参考，不能因为历史相似而合并工作。

## 保留的硬边界

- Project Memory 不是当天成果证据，不能生成合法的当天 `fact_ref`；
- 不得复制历史成果、状态、指标、日期、发布结论或下一步计划；
- Brief 中每项成果仍必须由当天 Fact 支撑；
- 最终日报仍只从已接受 Brief 生成，不重新读取历史内容。

服务端不校验 Brief 是否采用某个历史项目名，也不强制多个 Facts 归入历史项目。这样可以避免用户切换到相似新项目时被旧上下文吸附。

## 评测口径

评测 Project Memory 的价值，不检查“是否遵循 Memory”，而比较：

- 项目名称是否更自然；
- 连续工作是否更容易理解；
- 相似的新旧项目是否被错误合并；
- 多项目是否保持独立；
- 无法确认时是否使用中性表达；
- 是否出现历史成果泄漏。

必须同时包含“近期持续同一项目”和“切换到内容相似的新项目”两类样本，防止只优化连续性召回而忽略错误吸附。

## 首轮真实回放

隔离测试环境使用 Report Skill `aida-report@1.1.29` 完成三份生产 Session 回放：

- case-024：Context 中有 `调度策略`、`DEMO-CHIP`、`GPU 监控`、`Server Admin` 四个相关历史名称，Brief 仍按当天 Facts 使用 `CATP 平台使用手册`、`平台前端导航`、`GPU 监控数据审计`，没有服从历史标题；
- case-025：Context 将 `DEMO-CHIP` 标为可能相关，Brief 选择当天事实支持的 `测试执行模块`，没有被历史项目吸附；
- case-012：当天 Facts 自身支持 `ai-testgen-sessions` 时继续使用该名称，同时把测试代码补全 Agent 打包工作保持为独立主线。

三份 Run 均成功。case-012 的 Brief 校验重试耗尽后由既有降级链路写入最终结果；该现象归入 Report Brief 稳定性观察，不作为 Project Memory 关联结论，也不在本次调整中扩大处理范围。
