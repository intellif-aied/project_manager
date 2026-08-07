# Project Memory 分层历史窗口

## 目标

扩大 Project Memory 的项目连续性覆盖，同时避免把旧日报细节当成当天事实，也避免历史内容挤占当前主题和候选父项目的输入预算。

## 输入结构

历史按“有内容的日报份数”计算，不按自然日计算：

1. `recent_overviews`：最近 10 份日报，每份最多 300 字。AI 日报优先使用 Brief 的工作线名称，人工日报使用工作概览。
2. `historical_project_anchors`：再往前最多 10 份日报，每份最多 120 字，只保留可复用项目名，不保留成果、指标、状态和过程描述。两层合计最多 20 份历史日报。
3. `candidate_projects`：从最近 20 个 Project Memory 快照召回父项目，再按当天主题、工作线索和 Workspace 证据排序，最多给 Agent 8 个候选。每个候选携带 `matched_theme_refs`；同一候选覆盖多个当天主题时，作为父项目强证据。

输入仍受 8,000 token 硬上限保护。超限时按以下顺序裁剪：

1. 最老的项目锚点；
2. 最老的近期概览；
3. 排名最低的候选项目。

当前主题始终保留，历史不能挤掉当天证据。

## Agent 规则

- 父项目是长期稳定对象，模型、模块、实验、缺陷、文档和优化项通常只是工作线索，不能覆盖已有父项目。
- 服务端从正式项目名派生 `nnp412`、`GLM-5.2` 这类短项目键；仅在当天 Fact 精确出现项目键时用于高置信召回，不降低全局相似度阈值。
- 近 10 份概览和较早最多 10 份锚点只辅助命名与归并，不能提供当天成果事实。
- 当前内容与历史冲突时以当前内容为准；证据不足时返回 `unresolved`，不得强行关联。
- Report Agent 最多仍接收 3 个高相关 Project Memory 提示，Project Memory 规模扩大不等于把全部历史塞进日报生成。

## 版本

- Consolidation Input：`project-memory-consolidation-input/v2`
- Resolver：`project-memory-resolver/v11`
- Project Memory Skill：`project-memory-v10`
- Report 侧召回 K：`3`

## 验收重点

使用 `project-association-regression/v2` 全量回归，重点检查：

- `AI Coding 提效支撑` 不再被长期拆成多个模型训练和数据生成项目；
- `KV Cache 压缩算法研发` 不被 `split-K`、OSCAR 单项优化覆盖；
- `nnp412量化适配` 能通过历史项目名或稳定工作线索召回；
- 换项目样本不得被旧父项目强行带偏；
- Project Memory 失败仍不能阻塞日报生成。
