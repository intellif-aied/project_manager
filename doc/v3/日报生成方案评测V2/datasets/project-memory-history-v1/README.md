# Project Memory History Regression V1

该数据集是 Project Association 回归的连续历史参数集，不是新的日报 Gold Baseline。它冻结 4 名典型用户在生产环境最近 10 个自然日内实际存在的 34 份日报，用来验证 Project Memory 在连续历史下的项目命名、父子项目归并和换项目边界。

## 保存位置

- Git：本目录仅保存匿名 Subject、日期范围、数量和受控文件 SHA-256。
- 受控数据：`/home/intellif/evaluation/project-memory-history-v1/`。
- 员工姓名、生产用户 ID、日报正文、Brief 和保存行为只存在受控目录，不进入 Git。

## 作为 Project Memory 参数使用

对目标用户日 `D`：

1. 按受控 Manifest 将 `subject_ref` 映射到隔离测试用户；不同 Subject 不得复用同一个测试用户。
2. 只选择同一 Subject 中 `report_date < D` 的日报，按日期倒序最多取 20 份；不得把 `D` 当天或未来日报作为历史输入。
3. 最近 10 份进入 `recent_overviews`，再早最多 10 份进入 `historical_project_anchors`。
4. 保留 `generation_mode`、`edited`、`brief_payload` 和 `outcome_actions`，让服务端按生产规则判断人工/AI来源权重。
5. 历史只辅助项目命名与归并；当天成果仍必须来自目标用户日的 Session Facts。

评测前先执行：

    cd /home/intellif/evaluation/project-memory-history-v1
    sha256sum -c SHA256SUMS.txt

受控目录中的 `case-params/pa-*.json` 已按上述规则生成，可直接作为对应 Case 的历史 Fixture；不要再次从完整 Subject 文件人工挑选。

若修改样本范围、正文或 Subject 映射，必须创建 V2，不得原地改写本版本。
