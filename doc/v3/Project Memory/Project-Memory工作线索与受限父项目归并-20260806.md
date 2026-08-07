# Project Memory 工作线索与受限父项目归并

> 日期：2026-08-06  
> 环境：14.157 测试服  
> 状态：已开发并完成小范围真实回归，未提交、未发布生产

## 1. 问题

Project Memory 已保存稳定项目名和少量别名，但不能持续记住调用执行、CLI、版本流、调度器等属于同一父项目的工作对象。Report Agent 即使收到高置信项目提示，也可能把这些子能力拆成多个日报项目。

黄咏驰样本中，人工确认当天全部工作均属于“芯片验证平台”。旧结果仍拆成调用执行、CLI 工具、工单与调度器等多个顶层事项。

## 2. 当前流程

1. 当天日报成为 Project Memory Nightly 输入。
2. 专用 Project Memory Agent 读取当前主题、Brief 交付物、候选项目与近期概览。
3. Agent 输出项目归属、别名和稳定 `workstream_cues`。
4. 服务端校验并把 cue 连同来源日报、权重和日期保存到 Project Occurrence。
5. 下一次日报生成按项目名、别名、cue、Workspace 和当前 Fact 召回最多 3 个项目候选。
6. Report Agent 仍以当天 Fact 为唯一成果证据；Project Memory 只辅助父项目命名和归并。
7. Report Compiler 只对“高置信语义锚点 + 已知项目术语”执行受限父项目归并。

## 3. 新增 Project Profile 内容

每个项目除 `canonical_name`、`aliases`、Workspace 关系外，增加最多 8 个稳定 `workstream_cues`。cue 只能是模块、工具、协议、工作流或长期工作对象，不保存测试结果、发布状态、进度、指标、日期和成果句。

黄咏驰测试数据形成的项目档案：

- canonical：芯片验证平台
- aliases：工单处理流程、版本流、缺陷闭环 Agent、调用执行
- cues：CLI、工单、执行计划、批次、版本流、调度器、调用执行、工单转交与改派

## 4. 受限归并边界

服务端归并必须同时满足：

1. Hint 不是 `candidate_only`；
2. `match_basis` 是 `semantic` 或 `workspace_semantic`；
3. 置信度不低于 0.88；
4. 至少两个当前 Fact 构成语义锚点；
5. Agent Workstream 的所有引用 Fact 都在锚点内；
6. Agent 自己使用了 canonical、alias 或 cue 作为 subject。

仅由 Workspace 召回、陌生 subject、未锚定 Fact、明确新项目或冲突目标均不归并。该机制不会用历史成果补充当天内容。

## 5. Brief 改造影响

单次提案 + 服务端 Compiler 明显降低了 Brief 重试、格式失败和任务卡死，但不会自动补足项目语义。旧的二阶段重试同样不会产生新的项目证据。

改造后父项目判断只执行一次，因此 Project Memory 的输入质量和 Compiler 的受限归并更重要；Compiler 只处理已经被当前语义锚定的项目术语，不承担通用项目推断。

## 6. 测试结果

- Project Memory Skill：`aida-project-memory@project-memory-v7`
- Report Skill：`aida-report@1.1.43`
- Project Memory Job：2026-08-05、2026-08-06 均成功
- 模拟次日召回：3/3 Fact 命中“芯片验证平台”，置信度 0.99
- 最终 Report Run：`055e997f-512a-4b64-be40-d35683334125`

最终日报：

1. 芯片验证平台
   - 实现工单转交功能并完善权限、并发更新及前端处理入口。
   - 重构缺陷闭环 Agent 协议并同步版本流相关工作。
   - 统一 Agent 文件下载链接格式。

相关 Go 测试通过；测试服 API、数据库迁移和系统资产同步正常。生产环境未改动。
