# 报告摘要与 Presentation Profile

> 文档状态：代码与自动化完成，待测试服真实 Agent 验收
> 日期：2026-07-24
> 适用范围：个人日报、个人周报、小组日报、小组周报、部门日报、部门周报
> 设计基准：[开发方案设计基准](../../../../开发方案设计基准.md)

## 1. 目标

解决三个已经由用户反馈和代码确认的问题：

1. 完整报告内容较长，但用户看不到能够快速说明主要成果和整体状态的 Summary；
2. 全局 Report Skill 同时包含六类报告的来源、组织和表达规则，规则继续增加会提高模型遗漏和冲突风险。
3. Session Evidence 中的 Git 提交和分支操作会被模型机械展开，掩盖实际完成的业务工作。

本方案不重新定义六类报告的事实来源。原有来源继续以以下文档和代码为准：

- [第六阶段需求文档](../../第六阶段-分层来源与组织覆盖/01-需求文档.md)第 4 节；
- [第六阶段架构设计](../../第六阶段-分层来源与组织覆盖/02-架构设计.md)第 5、6 节；
- `api/internal/reportcontext/queries.go`；
- `api/internal/reportcontext/types.go`。

## 2. 固定结论

```text
创建并冻结 Report Context
  -> 后端按已冻结 report_type 选择唯一 presentation_profile
  -> presentation_profile 与原有 Evidence 一起进入不可变 Context
  -> Agent 只接收 run_id
  -> get_report_context(run_id) 一次取得当前 Profile 与全部 Evidence
  -> 同一次 Agent 执行生成非空 summary 和完整 content
  -> write_report_result(run_id, summary, content)
  -> 后端确定性组合为“工作总结 + 完整正文”
  -> 继续写入现有报告 content 字段
  -> 现有接口和前端按原流程展示单一 content
```

固定边界：

- Digest 和原有 Report Context 来源不改变、不裁剪；
- Profile 只决定展示结构，不决定事实范围和事实价值；
- Git 信息仅作为后台辅助溯源材料，不能单独证明工作成果，也不得作为独立工作项或操作流水展示；
- 每个 Context 只有当前报告类型的一份 Profile；
- 全局 Skill 只保留通用执行、证据和写回规则；
- 默认 Agent 四层 Prompt 按唯一职责收敛：Instructions 只保证 Skill 真实加载，Start Prompt 只传 `/aida-report + run_id`，运行时 Message 只传用户补充，报告流程只存在于 Skill；
- 默认 Agent 的 MCP Toolset 只暴露 Context 读取和成功/失败写回三个工具；旧工具不删除，自定义和历史 Agent 不受影响；
- Summary 与 Content 由同一次平台固定模型执行生成，不新增模型调用；
- `summary` 只属于 Agent 写回内部契约，不成为报告表、报告接口或前端字段；
- 不新增数据库迁移，不修改报告 DTO、页面、编辑、复制或提交接口；
- 不新增 Profile 表、配置中心、模板服务、Agent、队列或工作流框架；
- 不切换、降级、备用或覆盖 Agent Platform 规定的模型。

## 3. 文档导航

| 文档 | 唯一职责 |
| --- | --- |
| [01-需求与现状差距.md](01-需求与现状差距.md) | 产品规则、六类展示口径、代码事实和差距 |
| [02-架构与数据契约.md](02-架构与数据契约.md) | Profile、Context、写回、存储、兼容和失败边界 |
| [03-开发方案.md](03-开发方案.md) | 确定的代码位置、任务顺序、完成标准和回退 |
| [04-测试与验收.md](04-测试与验收.md) | 自动化、六类真实报告、人工质量和回归门禁 |
| [主流设计调研](../10-presentation-profile主流设计调研.md) | 一手资料依据及其不能替代本项目验收的边界 |

## 4. 当前状态

方案代码和自动化已经完成：Presentation Profile、Summary 组合、默认 Agent Prompt、MCP Toolset、全局 Skill 与 Git 信息证据规则均已落地。`go test ./...`、`go vet ./...`、验收脚本语法检查和 `git diff --check` 已通过。没有数据库迁移和前端改动；正式 Skill 资源尚未发布，测试服真实 Agent 和六类报告尚未验收，生产未部署。
