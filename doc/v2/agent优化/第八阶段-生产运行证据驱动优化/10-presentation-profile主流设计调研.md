# Presentation Profile 主流设计调研

> 日期：2026-07-24
> 范围：单一托管报告 Skill、冻结 Report Context、六类报告展示规则、内部 `summary + content` 写回与单一最终 Content
> 不涉及：模型选择或切换、Digest 事实裁剪、分页、增加 Agent/工作流框架

## 1. 结论

采用以下设计：

1. 全局 Report Skill 只保留所有报告共用的稳定执行规则，不再同时讲解六类报告的内容结构。
2. 后端根据 Run 已冻结的 `report_type`，选择唯一一份 `presentation_profile`，和本次冻结证据一起写入 Report Context。
3. Agent 仍只接收 `run_id`，只调用一次 `get_report_context({"run_id": run_id})`。Agent 不选择 Profile，也不补传日期、周期、目标或 Selection。
4. Agent 按 Context 中唯一的 Profile 组织内容，同时生成非空 `summary` 和完整正文 `content`，再调用 `write_report_result`。
5. `summary` 只属于 Agent 写回内部契约；后端确定性组合为“工作总结 + 正文”，继续写入现有报告 Content，数据库、报告接口和前端不新增 Summary 字段。
6. 工具 Schema 和服务端校验保证字段存在、类型和调用边界；事实是否准确仍由冻结 Evidence、证据约束和真实样本验收保证。
7. Profile 是本项目的确定性配置，不是新的 Prompt 服务、模板 Agent、模型调用或业务流程。

该方向与主流的一手设计原则一致：只提供当前任务所需的高信号上下文；使用短而明确的稳定指令；通过 Schema 定义工具输入输出；让确定性系统承担可验证的路由和结构约束，把语义归并留给模型。

`presentation_profile` 这个字段名及其冻结方式是本项目的架构决策，不是 Anthropic、OpenAI、MCP 或 JSON Schema 规定的标准字段。

## 2. 当前系统基础

当前仓库已经定义六类 `report_type`，也已经在 Report Context Builder 中分别确定了来源范围：

| report_type | 当前 Context 的主要报告来源 | 范围 |
| --- | --- | --- |
| `personal_daily` | 冻结 Session Evidence | 当前用户、当天 |
| `personal_weekly` | 个人日报；显式选中时可包含冻结 Session Evidence | 当前用户、本周 |
| `team_daily` | 成员个人日报 | 当前小组、当天 |
| `team_weekly` | 成员个人周报 | 当前小组、本周 |
| `department_daily` | 小组日报；部门直属成员使用个人日报补齐 | 当前部门、当天 |
| `department_weekly` | 小组周报；部门直属成员使用个人周报补齐 | 当前部门、本周 |

需求、任务、组织 Scope、来源覆盖和缺失项也已经按个人、小组、部门范围由后端读取。对应实现位于：

- `api/internal/reportcontext/types.go`
- `api/internal/reportcontext/queries.go`
- `api/internal/reportcontext/service.go`
- `doc/v2/agent优化/第五阶段-Report-Context-V1/`

因此，本次不能重新设计六类报告的事实来源，也不能让 `presentation_profile` 改写或筛选 Evidence。Profile 只定义“同一批冻结事实如何展示”，原有 Context 继续定义“本次 Run 有哪些事实”。

## 3. 一手设计依据

### 3.1 保持 Skill 简短，只加载当前任务需要的规则

Anthropic 的 Agent Skills 设计把“渐进披露”作为核心原则：启动时只加载足够判断用途的元数据，具体任务触发后才加载相关 Skill，场景专用内容继续放在额外文件中，避免把所有内容一次性放进上下文。该原则直接支持“全局 Skill 只保留通用流程、当前报告类型规则按本次运行提供”，但不规定必须使用 Context 字段实现。[Anthropic：Equipping agents for the real world with Agent Skills](https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills)

Anthropic 的 Context Engineering 文档把目标表述为：在有限注意力预算内选择尽可能小的高信号 Token 集合；系统指令应清晰、直接并处于合适抽象层级，既不能堆积脆弱的复杂逻辑，也不能只给模糊高层要求。把六类互斥规则从全局 Skill 移出、每次只提供当前类型规则，符合这一原则。[Anthropic：Effective context engineering for AI agents](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents)

Anthropic 的提示工程指南要求指令清晰、显式，控制格式时优先明确“应该做什么”，并确保示例和细节与期望行为一致。Profile 因而应使用少量正向、确定的写作要求，不应重新堆积成长篇禁止项。[Anthropic：Claude 提示工程最佳实践](https://docs.anthropic.com/zh-CN/docs/build-with-claude/prompt-engineering/claude-4-best-practices)

### 3.2 由后端提供一个完整、明确的 Context 工具

Anthropic 的工具设计经验明确指出：工具应有清晰且不同的用途；与其让 Agent 调多个低层工具并自行拼装，不如提供一个汇总当前对象相关信息的 Context 工具；工具响应应返回高信号信息并通过真实评测选择结构。本项目继续使用单一 `get_report_context(run_id)`，与这一模式一致。[Anthropic：Writing effective tools for AI agents](https://www.anthropic.com/engineering/writing-tools-for-agents)

MCP 官方架构把 Server 定义为向 AI 应用提供 Context 的组件，同时把 Tools、Resources 和 Prompts 作为不同的协议原语。MCP 只规定 Context 交换，不规定应用必须如何组织模型上下文。因此，Aida MCP 返回冻结 Evidence 加当前 Profile 符合协议边界，但具体 Profile 内容仍由 Aida 产品定义。[MCP：Architecture overview](https://modelcontextprotocol.io/docs/learn/architecture)

### 3.3 使用 Schema 保证内部 `summary + content` 的结构

MCP Tool 定义包含 JSON Schema `inputSchema`，并可声明 `outputSchema`。声明输出 Schema 后，Server 必须返回符合 Schema 的结构，Client 应进行校验。该机制支持 Context 返回稳定的 `presentation_profile + evidence` 结构，也支持写回工具用明确参数承载 `run_id + summary + content`。[MCP Tools 规范](https://modelcontextprotocol.io/specification/2025-06-18/server/tools)

JSON Schema 官方文档明确区分结构约束和说明性注解：`type`、`properties`、`required` 等用于验证，`title`、`description` 用于解释字段目的。Summary 不能只靠 Skill 自然语言要求；本项目还要兼容历史 Run，因此 Schema 声明 Summary 类型，服务端再按 Run 的冻结表示强制新 Run 必填。[JSON Schema：Getting started](https://json-schema.org/learn/getting-started-step-by-step)、[JSON Schema：Annotations](https://json-schema.org/understanding-json-schema/reference/annotations)

OpenAI Structured Outputs 的官方说明同样区分“结构遵循”和“字段值正确”：严格 Schema 可以约束输出形状，但不能消除字段内容本身的事实错误。这支持把内部 Summary 是否存在交给 Schema/服务端，把 Summary 是否忠于 Evidence 交给事实规则和验收，不能宣称结构化写回等于报告质量已经可靠。结构化写回也不要求产品数据库和前端暴露同样的字段；服务端可以将其转换成现有领域模型。[OpenAI：Introducing Structured Outputs in the API](https://openai.com/index/introducing-structured-outputs-in-the-api/)

### 3.4 简单、确定的外围流程优于继续增加 Agent 决策

Anthropic 将 Workflow 定义为由代码预先编排 LLM 和工具的系统，将 Agent 定义为由模型动态决定过程和工具使用的系统；对于定义清楚的任务，Workflow 更可预测。报告类型、Profile 选择、Run 身份、写回字段都属于确定性业务规则，应由后端完成；目标归并和自然语言表达才属于模型工作。[Anthropic：Building effective agents](https://www.anthropic.com/engineering/building-effective-agents)

## 4. 本项目的确定契约

### 4.1 Skill 只保留六条通用规则

全局 Skill 只表达：

1. 输入只有 `run_id`。
2. 只调用一次 `get_report_context({"run_id": run_id})`。
3. 完整使用 Context 中的 `presentation_profile` 和 Evidence。
4. 只根据 Evidence 归并事实，不虚构风险或完成状态，不推测未来计划。
5. 同一次生成同时产出非空 `summary` 和完整 `content`。
6. 成功调用 `write_report_result`；失败调用 `write_report_failure`。

日期、周期、目标、Selection、来源路由、六类报告差异、Profile 选择和字段必填校验均不放回 Skill。

### 4.2 默认 Agent Prompt 只负责触发 Skill

主流“高信号、按需提供 Context”的原则不仅适用于 Skill，也适用于 Agent Instructions 和启动消息。当前同一协议在四层重复不会增加事实可靠性，只会增加 Token、冲突面和模型注意力负担。本项目固定为：Instructions 只保证 Skill 被真实加载；Start Prompt 只传 `/aida-report + run_id`；运行时 Message 只传一次用户补充；Context/MCP/凭据/范围/写回和报告规则只由 Skill 定义。自定义 Agent 不参与该收敛。

同一原则也适用于 Tool Schema。默认 Agent 的确定流程只使用 `get_report_context`、`write_report_result`、`write_report_failure`，因此通过现有 MCP Server Header 为它返回这三个 Tool Schema。旧工具实现和无 Header 的完整 tools/list 保留，避免为减少 Prompt 而破坏自定义或历史 Agent。该选择是本项目基于真实工具集作出的实现决策，不是 MCP 规范要求。

### 4.3 Profile 是 Context 的一部分

固定结构为：

```json
{
  "presentation_profile": {
    "summary_focus": "概括个人当天完成的核心工作、主要成果和整体状态",
    "content_grouping": "按真实工作目标归并"
  },
  "work_evidence": {}
}
```

上述字段用于表达展示规则，不携带新的事实。Profile 必须满足：

- 后端按冻结 `report_type` 一对一选择；
- 一个 Context 只存在一个 Profile；
- Profile 与 Evidence 一次返回，不增加 Agent 工具调用；
- Profile 写入冻结 Context 并参与 `context_hash`；
- 同一个 Run 重复读取返回相同 Profile；
- Profile 配置更新只影响之后新建的 Run，不改变历史 Run；
- Agent 不允许传入或覆盖 Profile；
- Profile 不得包含 Top-K、事实删除、模型选择或切换规则。

### 4.4 六类 Profile 只改变展示层级
- 不增加独立 Profile ID 或版本字段：`run.report_type` 已经标识唯一 Profile，完整 Profile 随 Context Hash 冻结。

| report_type | Summary 聚焦 | Content 归并层级 |
| --- | --- | --- |
| `personal_daily` | 个人当天的核心工作、成果和整体状态 | 个人工作目标，直接使用动态主题，不设置“重点工作”总标题 |
| `personal_weekly` | 个人本周的核心进展、里程碑和整体状态 | 跨天持续目标，不按日期流水账 |
| `team_daily` | 小组当天推进的共同目标和交付 | 小组共同目标，不默认逐人罗列 |
| `team_weekly` | 小组本周交付、里程碑和协作风险 | 小组业务目标和里程碑 |
| `department_daily` | 部门当天重要进展和管理关注项 | 部门级目标，不机械罗列小组 |
| `department_weekly` | 部门本周整体成果、关键进度和跨团队风险 | 部门级目标和关键里程碑 |

所有 Profile 共用同一条件：只有 Evidence 明确支持时才生成“风险与待处理”。Profile 不负责计划，六类报告均不生成独立计划章节；Evidence 已明确记录的后续动作只能保留在对应工作主题中，不得扩写成预测性计划。既有 `next_day_plan` 继续由用户维护。Profile 不得改变第 2 节列出的既有来源定义。最终两字段内容以[架构与数据契约](报告摘要与Presentation-Profile/02-架构与数据契约.md)为唯一实现口径。

### 4.5 写回结构

`write_report_result` 的业务参数固定为：

```json
{
  "run_id": "...",
  "summary": "必填的高层工作总结",
  "content": "必填的完整 Markdown 报告正文"
}
```

Schema 声明 run_id、summary、content 的类型并固定要求 run_id/content；服务端根据 `run_id` 读取冻结的报告身份，对新 `work_evidence` Run 条件校验 Summary 非空，不接受 Agent 重新声明日期、周期、报告类型、目标或 Selection。Agent Content 不重复 Summary；服务端将归一后的 Summary 作为“工作总结”章节前置到正文，只把组合后的最终 Content 写入现有报告字段。

报告产品契约保持不变：数据库、详情/列表接口、前端、编辑、复制和提交仍只有一个 Content。现有 Content Hash 继续对组合后的最终 Content 计算；历史 Run 仍对原 Content 计算，因此不新增 Hash 版本和兼容分支。

## 5. 验证要求

该设计不能只靠文档判断效果，必须复用当前六类原始 Context 和真实报告样本验证：

1. 六类 Run 各自只收到对应 Profile，不出现其他五类规则。
2. 同一 Run 重复读取的 Profile、Evidence 和 `context_hash` 一致。
3. 新 Run 缺失或空 `summary`、缺失或空 `content` 无法完成写回；历史 Run 保持原契约。
4. Summary 能独立说明主要工作、成果和整体状态，不只是复制标题或正文首句。
5. Content 保留原 Context 的独立事实，相关工作按真实目标归并，不因 Profile 丢失 Evidence。
6. 小组和部门报告不默认按成员或下级组织机械罗列。
7. 没有明确证据时不生成风险；任何报告都不推测未来计划。
8. 正文直接按真实工作主题使用动态标题，不输出固定“重点工作”标题，不让模型判断重点等级。
9. 后端只写一个最终 Content，报告数据库、公开接口和前端无 Summary 字段或改动；现有提交快照包含完整组合结果。
10. Agent 只调用一次 Context 读取和一次成功写回；不再请求日期、周期、目标或 Selection。
11. 默认 Agent 四层 Prompt 无协议重复；用户补充最多出现一次；自定义 Agent 不变。
12. 默认 Agent 只收到三个必要 MCP Tool Schema；旧工具、历史 Agent 和自定义 Agent 不变。
13. 使用 Agent Platform 规定的同一模型完成修改前后对照；本方案不增加任何模型选择或切换逻辑。

## 6. 不能从主流资料推出的结论

主流资料不能证明：

- 名为 `presentation_profile` 的字段是行业标准；
- 单一 Profile 一定提升六类报告质量；
- JSON Schema 能保证 Summary 的事实准确性；
- Profile 可以代替冻结 Evidence 或现有来源范围；
- 应为六类报告分别创建 Skill 或 Agent；
- 需要分页、第二次模型调用或新的工作流系统；
- 应切换、降级或备用报告模型。

这些边界必须保留。最终质量结论只以本项目六类报告自动化契约测试和真实样本验收为准。
