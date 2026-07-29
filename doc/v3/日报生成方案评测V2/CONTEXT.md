# 日报生成方案评测领域

本领域用于对完整日报生成方案做可重复、受控的测试服比较。

## Language

**Generation Scheme**:
从 Source Evidence 到 Final 的完整生成思路和阶段契约。
_Avoid_: 模型方案、Pipeline 配置

**Generation Variant**:
一个可执行、不可变的 Generation Scheme 快照，包含实际阶段、模型、Prompt、Skill、规则、代码和产物契约版本。
_Avoid_: 模型版本、当前流程

**Evaluation Case**:
由报告任务、Source Evidence Snapshot 和 Evidence Baseline 组成的固定评测样本。
_Avoid_: Run、日报样例

**Source Evidence Snapshot**:
不同 Variant 端到端比较的共同上游输入。
_Avoid_: Context、标准日报

**Evidence Baseline**:
从 Source Evidence 标注的重要事实、状态、环境、允许排除项和禁止新增内容。
_Avoid_: AI 答案、标准日报全文

**Directly Usable**:
日报不存在需要用户纠正的实质问题，可以直接使用或只需非必要风格调整。
_Avoid_: 生成成功、用户未编辑

**First Bad Stage**:
一个端到端问题在实际 Variant 阶段中首次出现的位置。
_Avoid_: 最终失败阶段

**AI Review**:
Reviewer 根据 Evidence、Rubric 和 Artifact 产生的结构化语义初审。
_Avoid_: Ground Truth

**Gold Review**:
人工根据 Source Evidence 确认的评审结论。
_Avoid_: AI Review、用户稿

**Version Comparison**:
同一 Dataset 和 Rubric 上对基线与候选 Variant 的配对比较。
_Avoid_: 不同时间窗口对比

**Evaluation Conclusion**:
一次 Version Comparison 对候选 Variant 给出的内部质量证据结论。
_Avoid_: 发布结论、模型排名

**Evaluation Bundle**:
包含 Manifest、Case、Source Evidence、Artifact、Final 和运行指标的不可变评测输入包。
_Avoid_: 数据库连接、临时 Prompt
