# Report Agent 两阶段生成设计 Review

> 日期：2026-07-27
> Review结论：通过，允许开发

## 1. Review问题

本次Review检查最终方案是否绕过现有 `Digest → Report Projection → Report Context`，是否重复现有模块职责，以及是否会影响其他报告、Agent、身份和权限业务。

## 2. 纠偏记录

| 初始方案问题 | 纠偏结果 |
|---|---|
| 直接从Context增加Outline，未分析现有Report Projection | 明确Projection保留确定性职责，Report Brief位于Projection之后 |
| 使用`Report Outline/submit_report_outline` | 统一为可追溯事实和交付状态的`Report Brief/write_report_brief` |
| Workstream只有一个总状态 | 改为Deliverable独立保存state/environment/next_action |
| 拟设置日报正文服务端硬长度 | 删除硬长度门禁，避免截断事实；长度只做Skill和GT质量目标 |
| 计划两个实验Agent和实验Skill进入生产 | 生产只保留一个默认Report Agent、一份Skill、一个MCP和一个Agent Session |
| 中间自然语言摘要不可追溯 | Context Facts增加`fact_ref`，Brief要求全部引用或排除 |
| 技术词直接进入日报 | 固化领域词表和禁用表达，技术名称转换为读者结果 |
| 初版仅校验Brief，Flash在最终写作重新引用Context内部细节 | 最终`write_report_result`复用读者文本安全策略，失败后只允许基于已接受Brief修正 |
| 同名同目标Workstream被模型拆成多项 | ReportBrief规范化时确定性合并Deliverables |

## 3. 与现有链路的职责关系

| 模块 | 保留职责 | 本次是否改变 |
|---|---|---|
| Session Digest | 确定性提取WorkUnits和结果证据 | 否 |
| Report Projection | 确定性过滤结构噪声、精确去重、生成work_evidence | 仅增加稳定fact_ref，不改变选择语义 |
| Report Context | 冻结run事实边界 | 否 |
| ReportBrief Module | LLM结果的Schema、覆盖、状态、安全、Context绑定和持久化 | 新增 |
| Report Skill | 第一次语义归并与第二次读者写作 | 修改同一Skill |
| Report MCP | 暴露Brief写入和最终写回门禁 | 增加一个工具 |

## 4. 风险Review

| 风险 | 结论 | 处理 |
|---|---|---|
| Brief复制Context敏感内容 | 可控 | run/user权限、大小限制、禁用格式校验、不提供普通查询入口 |
| Flash不严格遵循Prompt | 已在实验中出现 | MCP Schema和写回门禁保证流程；质量继续由Skill、GT和人工Review保障 |
| 新增模型轮次增加耗时 | 真实存在 | 测试服记录两阶段耗时；不隐藏成本，未达标则关闭开关 |
| Skill/API版本错配 | 高风险但可控 | 不可变Skill版本、API和开关配套部署 |
| 影响其他报告 | 不允许 | 工具和门禁只在开关开启的默认个人日报生效 |
| 事实语义归并错误 | 规则无法完全证明 | 保留fact_refs、Brief持久化和配对GT，允许定位第一阶段问题 |

## 5. 防跑偏结论

- 方案没有绕过Digest和Projection，而是在其确定性输出之后增加语义组织；
- 新增ReportBrief模块具有独立且必要的校验、持久化和幂等职责，不是MCP Handler的浅转发；
- 不需要第二个Skill、MCP Server、Agent Session或Report Run；
- 首期范围严格限定默认个人日报，并提供关闭开关回退；
- 需求、架构和开发任务可完整追溯，允许开始开发。

## 6. 开发后复核

首轮真实测试证明“Brief已清理”不等于“最终正文不会回忆原Context”，原设计缺少最终写回安全校验。该缺口已在既定ReportBrief/MCP边界内修正，没有改变Digest、Projection、Context、账号、权限、其他报告或普通Agent业务。复测工具链和内容安全门禁均生效，方案方向未跑偏；最终文字质量仍需用户人工Review。
