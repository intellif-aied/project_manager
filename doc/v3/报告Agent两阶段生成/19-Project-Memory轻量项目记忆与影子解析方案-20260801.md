# Project Memory 轻量项目记忆与影子解析方案

> 日期：2026-08-01  
> 范围：系统默认个人日报 `personal_daily`  
> 阶段：V2 第一阶段（影子运行）

## 1. 目标

把当前“最近三份已保存日报主题树”从临时 Prompt 上下文，演进为可追溯、可拒绝、不会污染当天事实的轻量 Project Memory。

本阶段只回答两个问题：

1. 当天 Fact 可能属于哪些历史项目；
2. 候选判断是否足够稳定，可以进入后续 A/B。

影子结果不进入 Report Agent 输入，不改变 Brief、Summary 和最终日报；解析失败也不得导致日报失败。

## 2. 核心边界

- `today_facts` 是当天成果、状态和结论的唯一证据。
- Project Memory 只保存项目名称、别名、子主题、出现日期和来源日报。
- 只有用户保存或提交的日报可以更新记忆，Generated Draft 不参与。
- 同一用户的项目记忆严格隔离。
- 候选解析必须允许 `unmatched`，不得为了归并强行选择项目。
- 每次解析记录算法版本、候选得分和来源，支持复现和回退。

## 3. Module 与 seam

新增 `reportmemory` Module，对外只提供一个 Interface：

```go
ResolveShadow(ctx, tx, ResolveRequest) (ResolutionSnapshot, error)
```

- `ResolveShadow` 隐藏最终稿来源识别、历史补齐、主题提取、项目 upsert、候选召回、打分、拒绝策略和快照持久化。
- 历史来源指纹变化时幂等重建该用户的项目记忆；保存/提交后的最终稿会在下次解析时进入。
- 冻结 Report Context 时调用 `ResolveShadow`，但返回结果不写入 Agent Context。
- 影子解析错误只记录，不中断原日报链路。

## 4. 数据模型

### `report_projects`

- `id`
- `user_id`
- `canonical_name`
- `normalized_name`
- `status`: `active | paused | ended`
- `first_seen_on / last_seen_on`

同一用户、同一标准化名称唯一。第一阶段不自动合并改名项目。

### `report_project_aliases`

- `project_id`
- `alias / normalized_alias`
- `alias_type`: `canonical | child_topic`
- `source_report_id / source_report_date`
- `confidence`
- `source_type / source_weight`

### `report_project_occurrences`

记录项目在哪一份用户最终日报中出现，以及当时标题和直接子主题。用于来源追踪和重新计算。

### 最终稿来源与主题优先级

Project Memory 记录的是用户最终选择的重点，不从“工作详情”反推项目：

1. 完全人工日报：读取“工作概览”；没有该标题时读取顶层编号/列表事项，权重 `1.00`；
2. AI 生成后人工修改：读取用户最终稿的“工作概览”或顶层事项，权重 `0.95`；
3. AI 原样保存且存在结构化 Brief：读取 `workstreams[].subject`，它是概览主题的结构化来源，权重 `0.75`；
4. 旧 AI 稿没有结构化 Brief：回退读取概览，但仅作为弱提示，权重 `0.55`；
5. Generated Draft：不进入。

来源权重只影响项目命名与候选排序，不能让历史内容成为当天成果证据。

### `report_project_resolution_snapshots`

按 `run_id` 保存当天每个 Fact 的候选项目、得分、命中信号、是否达到高置信度及算法版本。该表是观测数据，不是生成输入。

## 5. 候选策略 V1

候选文本由 Fact 文本和其 `thread_ref` 对应的 Goal 组合，不使用 Cwd、Git 提交或发布状态作为日报结论。

召回信号：

1. 标准名称或别名完整命中；
2. 标准化字符 n-gram 相似度；
3. 最近出现时间衰减；
4. 同一项目多个别名共同命中。
5. 最终稿来源权重；人工最终稿高于 AI 原样保存稿。

第一阶段只产出候选和置信度：

- 完整名称/稳定别名命中可标记高置信度；
- 只有宽泛技术词相似时保持 `unmatched`；
- 每个 Fact 最多保留 3 个候选；
- 不把候选直接交给 Agent。

## 6. 历史补齐

为避免上线后只有新保存日报才有记忆，首次为用户解析时，按有效最终稿规则补齐历史日报：

- `status = saved/submitted` 且内容非空；
- `edited=true`、已提交或存在用户保存/提交事件；
- Generated Draft 且从未保存的不补齐；
- 补齐幂等，以 `report_id + project_id` 唯一约束防重复。

本阶段不设置“只看最近三份”硬窗口；时间只参与候选排序。

## 7. 验收

至少覆盖：

1. 同一项目跨日期、不同子主题能够召回同一 `project_id`；
2. 无关项目不会因通用词被高置信度合并；
3. Draft 不写入记忆；保存/提交会写入；
4. 历史项目内容不会进入 Agent Context；
5. 影子解析失败时原日报仍能继续；
6. 相同输入重复解析结果稳定；
7. 快照可以追到来源日报和算法版本。

## 8. 后续启用条件

影子数据满足以下条件后，才进入第二阶段 A/B：

- false merge 明显低于当前 Prompt 直接归并；
- 对持续项目的 false split 有可重复改善；
- 历史污染为零；
- 解析失败不影响日报成功率。

第二阶段只把高置信度 `project_ref` 作为命名/归类提示提供给 Agent；中低置信度继续保持独立。完整 GraphRAG、图数据库和自动项目改名合并不在当前范围。
