# 生产日报价值观察领域

本领域描述生产 AI 日报从生成到用户结果的可观察价值，不评价测试候选方案。

## Language

**Generated Draft**:
某次 Report Run 首次成功写入业务日报的不可变内容快照。
_Avoid_: 当前日报、用户稿

**Generation Variant**:
产生 Generated Draft 的完整生成方案快照；模型只是其中一个阶段配置。
_Avoid_: 模型版本

**User Outcome**:
用户对 Generated Draft 明确保存、提交或删除形成的追加式结果。
_Avoid_: 当前状态、自动 saved

**Comparable User Outcome**:
能够唯一对应某个 Generated Draft 的用户明确保存或提交内容。
_Avoid_: 当前日报内容、自动保存结果

**Confirmed Direct Use**:
用户明确保存或提交，且对应用户内容与 Generated Draft 规范化后完全一致。
_Avoid_: 自动 saved、页面未编辑

**Observed Unchanged**:
观察截止时当前日报与最近 Generated Draft 一致，但没有用户明确操作证据。
_Avoid_: 用户接受、Confirmed Direct Use

**Draft Retention**:
Generated Draft 中被用户稿保留的规范化文本比例。
_Avoid_: 正确率、接受率

**Summary Region**:
日报中 `## 工作总结` 到下一个同级标题之间的内容区域。
_Avoid_: 列表摘要、正文首段

**Summary Outcome**:
Summary Region 在用户稿中保持不变、被修改、缩短或被删除的确定性结果。
_Avoid_: 内容质量结论

**Downstream Reuse**:
个人 AI 日报被小组或部门报告以明确来源关系实际采用。
_Avoid_: 页面浏览、用户接受

**Production Daily Value Observation**:
管理员按指定报告日期查看 AI 日报覆盖、生成、保留、修改和用户结果的内部生产观察。
_Avoid_: 测试服评测、员工绩效

**Production Observation Snapshot**:
绑定 `report_date`、`observed_at` 和筛选条件的生产观察导出。
_Avoid_: 持续变化的实时查询、测试数据集
