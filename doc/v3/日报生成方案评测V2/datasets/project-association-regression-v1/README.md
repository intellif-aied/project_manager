# Project Association Regression V1

统一入口和完整操作方式见：`doc/评测数据集/README.md`、`doc/评测数据集/Project关联评测数据集使用手册.md`。

该数据集固定 3 组、7 个用户日，用于验证 Project Memory 是否帮助日报恢复稳定父项目，同时避免把新工作误并入旧项目。

## 样本角色

- `pa-001`：困难正样本。当天 Facts 主要是模块名，必须借助 Workspace + 语义锚点恢复“芯片验证平台”。
- `pa-101`～`pa-103`：连续工作样本。`pa-101` 是无历史记忆的种子日；`pa-102`、`pa-103` 必须稳定归入“AI Coding 提效支撑”。
- `pa-201`～`pa-203`：边界样本。`pa-201` 是种子日；后续 OSCAR 工作应归入“KV Cache 压缩算法研发”，最后一天的“v5 精度评测套件”必须保持独立。

## 保存边界

- Git：`manifest.json`、匿名标签、Source Set SHA-256、Gold Association Baseline。
- 受控目录：`/home/intellif/evaluation/project-association-regression-v1`，保存测试账号映射、Report/Selection/Slice/Run 标识及原始证据引用。
- 不在 Git 保存员工姓名、生产用户 ID、Token、原始 Session 正文或完整日报。

## 回归方式

1. 每次代码变更先执行 Manifest 校验和确定性关联测试，不调用 Agent。
2. 候选准备发布时，才在测试服按受控 Archive 重放 7 个 Case。
3. 项目名称命中、必须分开和不得误并由 Gold Association Baseline 判断；AI Review 仅辅助评价文字是否自然、详略是否合适。
4. 数据集发布后不可原地修改；新增样本或修订标注时创建 `v2`。

结构校验：

    go run ./cmd/daily-report-eval validate-association --manifest ../doc/v3/日报生成方案评测V2/datasets/project-association-regression-v1/manifest.json

候选结果使用 Brief 的 `workstreams[].subject`，由确定性门槛判定，不调用额外 AI Review：

    go run ./cmd/daily-report-eval evaluate-association \
      --manifest ../doc/v3/日报生成方案评测V2/datasets/project-association-regression-v1/manifest.json \
      --candidates /home/intellif/evaluation/project-association-regression-v1/candidate-1.1.36.json
