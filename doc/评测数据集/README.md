# AIDA 评测数据集入口

> Codex 或人工准备任何 AIDA 评测时，先读本文件，再进入具体数据集说明。不要从历史会话推测数据位置、样本含义或验收结论。

## 当前数据集目录

| 数据集 | 状态 | 解决的问题 | Git Manifest | 详细说明 |
| --- | --- | --- | --- | --- |
| `project-association-regression/v1` | active | Project Memory 能否恢复稳定父项目，并避免误合并 | `doc/v3/日报生成方案评测V2/datasets/project-association-regression-v1/manifest.json` | [Project关联评测数据集使用手册](Project关联评测数据集使用手册.md) |

`active` 表示当前回归基线；`deprecated` 表示不再用于新版本门禁，但历史结果仍保留。数据集不能通过删除目录表达停用。

## Codex 固定工作流

1. 从上表选择数据集，阅读对应使用手册和 Manifest。
2. 先执行结构校验，不调用 Agent。
3. 只有候选版本需要真实生成效果时，才读取受控数据映射并在测试服重放。
4. 从 `report_run_briefs.workstreams[].subject` 导出候选结果，使用确定性评测命令判断项目关联。
5. AI Review 只能辅助检查文字自然度、详略和可读性，不能修改 Gold Baseline，也不能代替确定性门禁。
6. 结论必须同时记录数据集版本、代码版本、Skill 版本、Run ID 和评测输出；没有这些证据不得声称“评测通过”。

## 数据保存边界

- Git：匿名 Case、Source Set Hash、样本数量、Gold Baseline、使用说明。
- 受控目录 `/home/intellif/evaluation/`：员工映射、测试账号、Selection/Slice/Run 标识、原始证据引用和候选结果。
- 禁止进入 Git：Token、员工姓名、生产用户 ID、原始 Session 正文、完整生产日报。
- 测试数据不得写回生产；默认只在 14.157 测试服回放。

## 增删改规则

- 新增：人工确认样本边界后创建新版本，例如 `project-association-regression/v2`；同步建立新的受控 Archive。
- 修改：已发布版本不可原地修改样本语义、Gold Baseline 或 Source Hash；修订同样创建新版本并说明差异。
- 删除：正常情况下禁止删除。停止使用时在本目录将状态改为 `deprecated`，保留历史 Manifest、Hash 和评测结果。
- 紧急清理：只有凭证或敏感正文误入 Git 时才执行清理，并单独记录影响范围；不能把普通样本调整当成删除理由。

## 当前命令边界

目前已经实现：

- `validate-association`：校验 Project Association Manifest。
- `evaluate-association`：基于结构化 Brief subject 执行确定性门禁。

目前没有实现统一的 `dataset list/show/replay/export` 子命令。Codex 不得引用或执行这些不存在的命令；发现、重放和候选导出按具体数据集手册执行。
