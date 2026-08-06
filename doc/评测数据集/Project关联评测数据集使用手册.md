# Project 关联评测数据集使用手册

## 1. 适用范围

数据集标识：`project-association-regression/v1`。

用于验证：

- 当天 Session 只出现模块、模型或子任务名称时，Project Memory 能否辅助恢复正确父项目；
- 同一项目连续多日是否保持稳定名称；
- 新项目或独立工作是否被历史记忆错误合并；
- 强化关联后，是否仍以当天 Facts 为准，没有把历史成果写进当天日报。

它不负责评价日报文笔，也不证明所有员工或所有日期的整体生成质量。

## 2. 文件位置

仓库内：

    doc/v3/日报生成方案评测V2/datasets/project-association-regression-v1/
      README.md
      manifest.json

受控数据：

    /home/intellif/evaluation/project-association-regression-v1/
      manifest.json
      candidate-<skill-version>.json

原始 Session、Slice 或冻结来源可能保存在其他受控目录，具体路径由受控 `manifest.json` 的 `source_archive` 指向。不要依靠历史会话中的路径。

## 3. V1 样本含义

| Case | 角色 | 固定门槛 |
| --- | --- | --- |
| `pa-001` | 困难正样本 | 必须归入“芯片验证平台” |
| `pa-101` | 冷启动种子 | 建立“AI Coding 提效支撑”方向，不要求历史召回 |
| `pa-102`、`pa-103` | 连续项目 | 必须归入“AI Coding 提效支撑” |
| `pa-201` | 冷启动种子 | 建立“KV Cache 压缩算法研发”方向，不要求历史召回 |
| `pa-202` | 连续项目 | 必须归入“KV Cache 压缩算法研发” |
| `pa-203` | 项目边界 | “KV Cache 压缩算法研发”和“精度评测套件”必须同时存在且保持独立 |

员工姓名只存在于受控 Archive；不要写回 Git Manifest。

## 4. 开始评测

在测试服务器执行：

    ssh 157
    cd /home/intellif/dev/project_manager/api

先校验 Manifest：

    go run ./cmd/daily-report-eval validate-association \
      --manifest ../doc/v3/日报生成方案评测V2/datasets/project-association-regression-v1/manifest.json

预期输出包含：

    valid Project Association dataset: project-association-regression/v1 (7 cases)

如果这里失败，停止评测并修复数据结构；不要调用 Agent 绕过校验。

## 5. 什么时候需要重放 Agent

以下改动需要重放 7 个 Case：

- Project Memory Context、Workspace Identity、关联证据或 Resolver 变化；
- Report Skill 的项目归并指令变化；
- Brief Workstream 结构或读取规则变化；
- 准备发布涉及项目关联的候选版本。

纯文案、无关前端或不影响上述输入输出的改动，不需要重复消耗模型运行。

重放必须使用受控 `manifest.json` 中的测试用户、日期和 Source 映射。不得临时从生产挑选“看起来合适”的 Session 替换固定样本。测试 Token 只在执行时生成，不写入文档或 Archive。

当前尚未提供一键 `replay` 命令。Codex 必须明确记录每个 Case 对应的新 Run ID；不得因为只运行了部分样本而声称全量通过。

## 6. 导出候选结果

门禁读取结构化 Brief，不解析 Markdown 日报，也不让另一个 AI 猜测父项目。每个 Case 保存：

    {
      "case_id": "pa-001",
      "run_id": "实际 Run ID",
      "workstream_subjects": ["芯片验证平台"]
    }

subject 必须来自数据库中的 `report_run_briefs.brief_payload.workstreams[].subject`，不能根据最终日报手工改写。核对 SQL：

    SELECT
      b.run_id,
      jsonb_agg(item.workstream ->> 'subject' ORDER BY item.ordinality) AS workstream_subjects
    FROM report_run_briefs b
    CROSS JOIN LATERAL jsonb_array_elements(b.brief_payload -> 'workstreams')
      WITH ORDINALITY AS item(workstream, ordinality)
    WHERE b.run_id IN ('<本轮 Run ID>')
    GROUP BY b.run_id;

将结果按受控 Manifest 的 Case 映射写入：

    /home/intellif/evaluation/project-association-regression-v1/candidate-<skill-version>.json

候选文件必须包含：

    {
      "schema_version": "project-association-candidates/v1",
      "dataset_version": "project-association-regression/v1",
      "cases": []
    }

## 7. 执行确定性评测

    go run ./cmd/daily-report-eval evaluate-association \
      --manifest ../doc/v3/日报生成方案评测V2/datasets/project-association-regression-v1/manifest.json \
      --candidates /home/intellif/evaluation/project-association-regression-v1/candidate-<skill-version>.json

通过条件：

- 所有 `required` 项目命中 canonical name 或允许别名；
- `forbidden` 项目没有出现；
- `forbidden_merges` 中的两个项目没有落入同一 Workstream；
- 命令返回码为 0，输出顶层 `passed: true`。

两个冷启动种子 Case 没有父项目强制命中门槛，但仍要求生成成功并进入候选记录。

## 8. 人工复核边界

自动门禁通过后，人工只检查：

- 项目名称对员工和 Leader 是否自然；
- 详情是否太短、太长或重复；
- 专业术语是否保留原意；
- 是否出现当天 Facts 无法支持的结论。

人工不得为了让候选通过而临时修改 v1 Gold Baseline。若确认原标注错误，创建 v2 并记录修订原因。

## 9. 结论记录模板

每轮至少记录：

    数据集：project-association-regression/v1
    代码版本：<commit 或明确的 dirty revision>
    Report Skill：<owner/slug@version>
    测试环境：14.157
    Case 数：7
    Run ID：<逐项列出>
    自动门禁：pass/fail
    人工复核：范围与问题
    生产发布：是/否

只记录“效果不错”“已经测过”或“AI 认为通过”都不构成有效评测结论。

## 10. 新增失败样本

1. 先确认是可复现的通用边界，不是某次随机措辞差异。
2. 冻结来源并计算 Source Set SHA-256；敏感正文进入受控 Archive。
3. 人工标注父项目、允许别名、冷启动属性和禁止合并关系。
4. 创建 `project-association-regression/v2`，不要修改 v1。
5. 同时更新 `doc/评测数据集/README.md` 的目录状态和版本说明。
6. v2 通过 Manifest 校验后，才能作为新的发布门禁。
