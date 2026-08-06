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
在任何 Digest、Context 或生成阶段之前，从固定 Session Slice 冻结的规范事件证据及其 Hash，是不同 Variant 的共同上游输入。
_Avoid_: Digest、Context、标准日报

**Evidence Baseline**:
从 Source Evidence 结构化标注的必选、可选、排除证据，事实状态、环境、主题关系和禁止新增内容。
_Avoid_: AI 答案、标准日报全文

**Production Report Pattern Baseline**:
从多用户、多日期生产日报聚合出的主题数量、结构层级、篇幅、合并颗粒度和表达方式分布。
_Avoid_: 某位员工最终稿、标准日报模板

**Employee Final Reference**:
某位员工已保存的最终日报，用于观察该用户当次的取舍和表达偏好，不作为该用户日的标准答案。
_Avoid_: Ground Truth、Evidence Baseline

**Evaluation Runtime**:
由服务端明确证明为测试环境且开启评测能力的 Aida 运行实例；一次 Version Comparison 可以使用多个隔离 Runtime。
_Avoid_: 用户在命令行自称的 test、生产实例

**Runtime Attestation**:
Evaluation Runtime 对环境、评测开关和构建 Revision 给出的服务端证明，执行任何写操作前必须校验。
_Avoid_: `--environment=test` 参数

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

**Project Association Regression Case**:
由固定当日事实、Workspace 证据、历史 Project Memory 快照和 Gold Association Baseline 组成的项目归并回归样本。
_Avoid_: 某员工案例、临时 A/B Run

**Gold Association Baseline**:
人工确认哪些当天事实必须、可以或不得归入哪个项目，以及哪些工作必须保持独立；它只判断项目关联，不规定日报全文。
_Avoid_: 标准日报、Employee Final Reference

**Controlled Source Archive**:
位于受控评测存储中的原始 Session、Slice、Context 和运行映射；代码仓库只保存匿名 Case、Hash 和 Gold Association Baseline。
_Avoid_: Git 测试数据、生产实时数据
