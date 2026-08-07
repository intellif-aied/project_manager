# Project Association Regression V2

统一入口和完整操作方式见：`doc/评测数据集/README.md`、`doc/评测数据集/Project关联评测数据集使用手册.md`。

该数据集在 V1 的 3 组、7 个用户日基础上，增加 2026-08-06 的 5 个典型用户日，共 5 组、12 个 Case。它用于验证 Project Memory 是否能作为辅助上下文恢复稳定父项目，同时区分“模型没有正确选择”与“当天 Source 本身覆盖不足”。

## V2 新增样本

- `pa-002`：模块名充分、父项目名缺失，必须恢复“芯片验证平台”。
- `pa-104`：连续项目但 Source 只覆盖部分最终状态，仍需保持“AI Coding 提效支撑”父项目；内容完整度另行人工复核。
- `pa-204`：大 Context 中存在多个历史分支，必须选择当天 OSCAR 工作并恢复“KV Cache 压缩算法研发”。
- `pa-301`：单一 Source 且主要为 Agent Claim，必须恢复“nnp412量化适配”，同时人工检查是否保留不确定性。
- `pa-401`：Source 覆盖不足的边界样本。项目名称只允许命中、不强制命中，但“KV Cache 压缩算法研发”与“Wan2.1 推理加速”不得误并；该 Case 主要用于人工检查 Source Coverage。

## 保存边界

- Git：`manifest.json`、匿名标签、Source Set SHA-256、Gold Association Baseline。
- 受控目录：`/home/intellif/evaluation/project-association-regression-v2`，保存员工映射、生产只读基线、Context、Source Item 和后续测试 Run。
- 不在 Git 保存员工姓名、生产用户 ID、Token、原始 Session 正文或完整日报。

## 回归方式

1. 每次代码变更先执行 Manifest 校验和确定性关联测试，不调用 Agent。
2. 候选准备发布时，才在测试服按受控 Archive 重放 12 个 Case。
3. `required` 项目、禁止合并由确定性门槛判断；`allowed` 只表达可以使用的辅助关联，不强迫 Agent 根据缺失证据输出项目名。
4. Source 覆盖、事实确定性、文字自然度和详略由人工复核，不能由项目名称门槛代替。
5. V2 发布后不可原地修改；新增样本或修订标注时创建 V3。

结构校验：

    go run ./cmd/daily-report-eval validate-association --manifest ../doc/v3/日报生成方案评测V2/datasets/project-association-regression-v2/manifest.json

候选结果评测：

    go run ./cmd/daily-report-eval evaluate-association \
      --manifest ../doc/v3/日报生成方案评测V2/datasets/project-association-regression-v2/manifest.json \
      --candidates /home/intellif/evaluation/project-association-regression-v2/candidate-<skill-version>.json
