# GitHub 同类 Agent 规划与结构化输出调研

> 日期：2026-08-06  
> 目的：判断 AIDA 是否应继续让 Report Agent 反复提交完整 Report Brief。

## 1. 结论

同类 Agent 会使用规划、中间状态和结构化输出，但没有证据支持 AIDA 当前这种“冻结 Context 不变、模型反复重写整份 Brief，直到所有结构、内容和词法规则同时通过”的方式。

更普遍的做法是：

- 规划与执行分离，中间产物只生成一次或由独立控制器维护；
- 校验聚焦局部格式或明确输出 Schema；
- 重试预算小且由框架控制，不依赖 Prompt 自觉；
- 重试只修正具体错误，不把错误历史污染正式上下文；
- 达到上限后明确收口，并尽量保留已经产生的可用结果。

对 AIDA 的直接启发：Report Brief 应保留为内部结构化产物和评测依据，但不应继续成为 Agent 必须多轮通过的交互协议。

## 2. OpenHands software-agent-sdk

OpenHands 的 Planning Agent 只读代码并生成 `PLAN.md`，Execution Agent 再读取计划执行；这是真正的职责分离，不是同一个 Agent 因字段校验反复重写完整计划。[规划/执行示例](https://github.com/OpenHands/software-agent-sdk/blob/199618a59b08fedca2af87351d3e569b47a903ce/examples/01_standalone_sdk/24_planning_agent_workflow.py#L63-L116)

需要循环时，OpenHands 把循环放在 Agent 外围：每轮正常结束后由独立 Judge 判断目标是否完成，`max_iterations` 提供硬上限，最终区分完成和达到上限。[Goal loop runner](https://github.com/OpenHands/software-agent-sdk/blob/199618a59b08fedca2af87351d3e569b47a903ce/openhands-sdk/openhands/sdk/conversation/goal/runner.py#L30-L57) [Goal controller](https://github.com/OpenHands/software-agent-sdk/blob/199618a59b08fedca2af87351d3e569b47a903ce/openhands-sdk/openhands/sdk/conversation/goal/controller.py#L78-L133)

Judge 解析失败时保守返回未完成，再由外围上限收口；Agent 层还专门识别重复 Action、错误循环和上下文循环。[Judge 降级](https://github.com/OpenHands/software-agent-sdk/blob/199618a59b08fedca2af87351d3e569b47a903ce/openhands-sdk/openhands/sdk/conversation/goal/judge.py#L43-L70) [Stuck detector](https://github.com/OpenHands/software-agent-sdk/blob/199618a59b08fedca2af87351d3e569b47a903ce/openhands-sdk/openhands/sdk/conversation/stuck_detector.py#L18-L48)

对 AIDA 的意义：只有环境状态发生变化、Agent 可以获得新证据时，重新规划才有价值。AIDA 的 Report Context 已冻结，重复提交 Brief 不会获得新事实，因此不应套用开放式任务循环。

## 3. SWE-agent

SWE-agent 的 Parser 只对当前 Action 做局部格式检查，例如没有合法 Action 才产生 `FormatError`，没有对整个任务计划做全量语义门禁。[Parser](https://github.com/SWE-agent/SWE-agent/blob/3ea751c087f32b16e039a2233dd6eefecef325d5/sweagent/tools/parsing.py#L109-L165)

格式、blocklist 或 shell syntax 错误进入临时纠错 history；成功后这段错误对话不进入正式 history，但仍保留在 trajectory 中用于审计。[临时纠错上下文](https://github.com/SWE-agent/SWE-agent/blob/3ea751c087f32b16e039a2233dd6eefecef325d5/sweagent/agent/agents.py#L788-L821)

`max_requeries` 默认最多 3 次；超限、Context 或环境失败后停止继续询问，并尝试 autosubmit 已经产生的 Patch。[有界 requery](https://github.com/SWE-agent/SWE-agent/blob/3ea751c087f32b16e039a2233dd6eefecef325d5/sweagent/agent/agents.py#L1106-L1218) [保留已有成果](https://github.com/SWE-agent/SWE-agent/blob/3ea751c087f32b16e039a2233dd6eefecef325d5/sweagent/agent/agents.py#L823-L868)

对 AIDA 的意义：错误修正应针对局部，达到上限后使用已有可用内容，而不是把生成任务整体判失败。

## 4. PydanticAI

PydanticAI 把工具调用 retry 与最终 output validation retry 设置为两个独立预算，避免一种错误吞掉所有重试机会。[Agent retry interface](https://github.com/pydantic/pydantic-ai/blob/916fc83e8929470679db5ac1b3065bda5d5f4253/pydantic_ai_slim/pydantic_ai/agent/abstract.py#L94-L110)

ValidationError 或 ModelRetry 会转换为结构化 `RetryPromptPart`，把具体问题反馈给模型；output retry 统一计数，超过预算明确抛出 `UnexpectedModelBehavior`。[结构化错误反馈](https://github.com/pydantic/pydantic-ai/blob/916fc83e8929470679db5ac1b3065bda5d5f4253/pydantic_ai_slim/pydantic_ai/_output.py#L140-L178) [输出重试预算](https://github.com/pydantic/pydantic-ai/blob/916fc83e8929470679db5ac1b3065bda5d5f4253/pydantic_ai_slim/pydantic_ai/_agent_graph.py#L1314-L1327)

空输出和不可执行输出也消耗同一预算；若 Token 已耗尽则立即失败，不进行没有意义的重试。[空输出收口](https://github.com/pydantic/pydantic-ai/blob/916fc83e8929470679db5ac1b3065bda5d5f4253/pydantic_ai_slim/pydantic_ai/_agent_graph.py#L1431-L1481)

对 AIDA 的意义：如果仍保留模型修正，应由服务端控制且只允许一次局部修正；但 AIDA 还需要产品级兜底，不能把框架异常直接转成用户日报失败。

## 5. 与 AIDA 的本质差异

| 场景 | Context 是否变化 | 重试价值 |
| --- | --- | --- |
| OpenHands/SWE-agent 执行任务 | 每轮工具执行后环境发生变化 | 可以根据新 Observation 调整 |
| PydanticAI 结构化输出 | 输出 Schema 固定 | 少量、局部修正有价值 |
| AIDA Report Brief | Report Context 从第一次读取后完全冻结 | 反复重写完整 Brief 收益低、随机性高 |

AIDA 的 Brief 错误目前混合了三类问题：

1. 身份、Context、数据库等系统完整性问题；
2. JSON、长度、重复引用等可确定修复的结构问题；
3. 项目归并、用词、信息密度等质量判断。

把三类问题全部作为 `REPORT_BRIEF_INVALID` 返回给同一模型，会把一次报告生成变成协议调试循环。合理方案应拆开处理：系统问题失败；结构问题由服务端修复；质量问题记录告警并尽量接受可用部分。

## 6. 建议

- 保留一次 Agent 语义整理，保留 Report Brief 数据结构；
- 不再让 Agent 反复提交完整 Brief；
- 服务端像编译器一样执行解析、归一化、局部丢弃、安全过滤、Brief 持久化和最终渲染；
- 即使 Agent 提交空对象、部分字段或某条内容不安全，也应从可用部分或冻结 Facts 生成降级日报；
- 仅真实系统错误使 Run 失败，质量降级不向用户展示内部错误码；
- 通过 A/B 评测验证“成功率、项目归并、信息密度和错误吸附”，而不是用重试次数证明质量。
