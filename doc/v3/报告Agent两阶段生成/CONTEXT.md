# Report Brief 领域术语

本文件只定义统一语言，不记录实现方案。

## 核心术语

**Session Digest**：从 Session 事件中确定性提取的报告证据。它不使用 LLM，不负责最终语义归并或写作。

**Report Projection**：把冻结 Digest 转换为 Agent 可读取 `work_evidence` 的确定性过程。它负责结构清理和精确去重，不决定工作主题。

**Report Context**：一次 Report Run 冻结的不可变事实快照，是 Report Brief 唯一允许使用的事实边界。

**Evidence Fact**：Report Context 中一条可引用的结果或未解决事实。每条事实拥有仅在当前 Context 内稳定的 `fact_ref`。

**Report Brief**：Report Agent 第一次语义处理产生并由 Aida 校验接受的结构化中间产物。它把 Evidence Facts 组织为工作主题和交付物，但不是新的业务事实。

**Workstream**：围绕同一用户目标归并的一组交付物，最终日报通常以其作为内容主题。

**Deliverable**：Workstream 内具有独立结果和交付状态的事项。测试服验证与生产发布必须拆成不同状态，不得互相替代。

**Final Report**：用户最终看到并保存的日报或周报内容。两阶段个人日报只能基于已接受的 Report Brief 生成。

**System Report Agent**：归属 Aida 系统专用账号、由平台统一维护模型、Prompt、系统 Report Skill 和系统 Report MCP 的默认报告 Agent。

**Personal Report Agent**：归属当前用户、由用户自行维护模型、Prompt、Skill 和第三方 MCP 的报告 Agent。Aida 不向它注入或替换系统 Report Skill。

**System Report MCP**：由 Aida 提供的报告基础设施，使用绑定当前 Report Run 的短期凭证读取报告上下文并把结果写入 Aida。系统和个人 Report Agent 均必须具备此 MCP，但可见工具集不同。

**Report Run Token**：Aida 为单次 Report Run 签发的短期 JWT，其中包含 `report_run_id` 和当前 Aida 用户身份。Report MCP 依赖它把调用限制在该次运行，Agent 无需自行传递 `run_id`。

**System Report Flow**：System Report Agent 执行的两阶段生成流程。必须先产生并接受 Report Brief，再生成 Final Report。

**Personal Report Flow**：Personal Report Agent 使用用户自有 Prompt、Skill 和第三方 MCP 的生成流程。系统只提供 Report MCP 写入通道，不要求 Report Brief，也不注入系统 Report Skill。

Report Brief 是 System Report Flow 的中间产物，不是所有报告写入的通用门禁。

## 交付状态

**released**：有明确证据证明已经发布到生产环境。

**validated**：已经在测试环境完成验证，但不能表述为生产上线。

**completed**：工作本身已经完成，但没有环境验证或生产发布证据。

**in_progress**：工作仍在推进。

**blocked**：存在明确阻塞。

## 表达约定

- 用户可见业务对象使用“报告、日报、周报”，不使用“报表”。
- 技术术语按原名保留：`Report Agent`、`Report MCP`、`Report Context`、`Skill`、`Agent`、`MCP`。
- 技术方案名必须转换为读者结果，例如“通知深链”表达为“点击通知直接打开对应报告”。
- “写回”在用户可见内容中表达为“自动保存到报告”。
