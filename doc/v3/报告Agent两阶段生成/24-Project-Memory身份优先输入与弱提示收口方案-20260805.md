# Project Memory 身份优先输入与弱提示收口方案

> 日期：2026-08-05
> 状态：测试服候选方案

## 1. 现状

Project Memory 的定位已经确定为“类似 Agent 长期上下文的可选背景”，但当前输入仍有两处偏差：

1. 未修改的 AI 日报同时提供 Brief Subject 和完整日报标题，Memory Agent 仍可能把“项目名：当天成果”保存成 alias；
2. 一个历史项目命中大量当天 Facts 时，会把几十个 `related_fact_refs` 全部传给 Report Agent，使弱提示在视觉和语义上接近强制归属。

测试用户 305 的现有 Memory 已出现 `InfoAgent：完成稳定化治理与安全门禁，并发布私有版本`、`IF-Knowledge：完成 GPGPU AI Coding 全景调研与 Knowledge Map 方案收敛` 等成果句 alias。问题来自“身份”和“当天成果”仍混在同一输入层，不是缺少某个术语规则。

## 2. 调整原则

- Project Memory 只维护“这件工作叫什么”，不保存“今天做了什么”。
- 人工最终稿优先：人工日报或人工修改稿继续从最终可见正文理解主题。
- 未修改 AI 稿优先使用结构化 Brief Subject；完整标题和 Deliverable 只用于理解，不作为项目名或 alias。
- alias 只表示同一个工作对象的另一种短名称；子项目、场景、模块和成果句不再当作 alias。
- Report Agent 只拿少量代表性相关 Fact 引用；这些引用仍是可能联系，不是归属证明。

## 3. 实现

1. Project Memory `current_themes` 增加来源：
   - `brief_subject`：未修改 AI 日报，主题来自 Brief Subject；
   - `final_report_item`：人工日报或人工修改稿，主题来自最终正文。
2. 最近历史摘要对未修改 AI 日报同样只传 Brief Subject，避免重复输入成果句。
   - 旧 AI 稿没有结构化 Brief 时不再回灌正文；原报告仍保留，只是不作为命名记忆输入。
3. alias 限制为最多 24 个字符、最多 3 个，只允许同一工作对象的替代名称；不截断长文本后继续接收。
4. 候选项目、快照和 Report Context 输出统一过滤不符合短 alias 契约的旧数据，不直接删除历史数据库记录。
5. 每个 Project Hint 最多携带 12 个 `related_fact_refs`，降低提示强迫感和 Context 噪声。
6. Resolver 版本提升为 `project-memory-resolver/v5`，Project Memory Skill 提升为 `project-memory-v5`。

## 4. 边界

- 不改变当天 Facts 的证据地位，不让 Memory 生成日报事实。
- 不要求 Report Agent 必须使用历史项目名，也不恢复强制归并。
- 不为“儿童睡前”“芯片验证平台”等单一样本增加关键字规则。
- 本期不批量删除生产 Memory；先通过新输入和输出过滤验证，再决定是否需要一次性重算。
- Report Skill、Report MCP 和个人 Agent 不因本调整改变。

## 5. 验收

- 同一份未修改 AI 日报进入 Memory Agent 时，`current_themes` 只包含 Brief Subject，不包含当天成果句。
- 人工修改稿仍按最终正文进入，不被旧 Brief 覆盖。
- 长成果句不再进入候选 alias、快照或 Report Context。
- 一个项目命中大量 Facts 时，Hint 仍不超过 12 个相关引用。
- A/B 回放中，Memory 组可帮助识别连续项目，但不会比无 Memory 组增加错误吸附或历史成果泄漏。
