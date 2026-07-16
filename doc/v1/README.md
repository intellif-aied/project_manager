# Aida 文档说明

> 归档状态：本目录为 V1 历史资料，只用于追溯，不代表当前 V2 产品或实现口径。当前开发请从 [`doc/README.md`](../README.md) 和 [`doc/v2/`](../v2/) 进入。

这个目录用于放置 Aida 的产品方向、阶段性决策和历史材料。

## 当前状态

Aida 正在从当前 demo 向一期方案收敛。当前优先冻结的是“角色化首页”，因为它和现有 demo 的差距最大，也决定了产品给用户的第一印象。

需求、任务、AC、Session、报告和 Token 相关流程仍然重要，但在首页方向稳定前，不应先大规模重做这些链路。

## 阅读优先级

按以下顺序阅读：

1. `decisions/001-homepage-v1.md`：当前已确认的首页一期方向。
2. `aida-requirement-thinking.md`：历史方向收敛稿。
3. `aida-platform-summary.md`：早期完整产品设想。
4. `prototypes/aida-p0-homepage/`：历史 P0 首页原型。

凡是首页相关问题，`decisions/001-homepage-v1.md` 优先级高于所有旧文档。

## 文档状态

| 文档 | 状态 | 说明 |
| --- | --- | --- |
| `decisions/001-homepage-v1.md` | 当前有效决策 | 首页调整优先读取这份。 |
| `aida-requirement-thinking.md` | 历史参考 | 可用于理解来龙去脉，但不再代表最新首页结论。 |
| `aida-platform-summary.md` | 历史参考 | 早期完整设想，不应把其中所有内容都视为一期范围。 |
| `prototypes/aida-p0-homepage/` | 历史原型 | 可参考角色首页探索方式，但不是实现合同。 |

## 工作规则

不要从旧文档中自动扩展需求，除非新的决策文档明确重新启用这些内容。某个主题没有被当前有效决策覆盖时，应视为“未定”，不要从历史草稿里推断为已确认范围。
