# Report Brief 领域术语

本文件只定义统一语言，不记录实现方案。

## 核心术语

**Session Digest**：从 Session 事件中确定性提取的报告证据。它不使用 LLM，不负责最终语义归并或写作。

**Report Projection**：把冻结 Digest 转换为 Agent 可读取 `work_evidence` 的确定性过程。它负责结构清理和精确去重，不决定工作主题。

**Report Context**：一次 Report Run 冻结的不可变事实快照，是 Report Brief 唯一允许使用的事实边界。

**工作连续性上下文（Continuity Context）**：个人日报 Report Context 中的非证据归类提示。它从报告日期之前最近三份用户已保存日报提取一级主题、直接子主题和稳定名称，只帮助当前 Evidence Facts 识别跨日项目归属；其中的历史成果、状态、指标、日期和结论均不能作为当前日报事实。

**项目记忆（Project Memory）**：从用户已保存或已提交日报中增量形成的稳定项目名称、别名、出现日期和来源记录。它只用于项目候选召回与归类，不是当天成果证据。第一阶段以影子模式运行，解析结果不会进入 Report Agent Context。

**最终工作概览（Final Overview）**：一份日报最终保留内容中的“工作概览”或等价顶层编号事项。Project Memory 只从这里识别用户选择的工作主题，不从 Work Detail 反推项目。

**自动结转稿（Auto-carried Report）**：由 System Report Agent 生成、用户未编辑也未显式保存，但按产品规则默认保留的日报。它可以作为弱历史参考，但不等同于用户确认。

**记忆提议（Memory Proposal）**：Memory Resolver Agent 对当天主题与既有 Project Memory 关系给出的结构化建议。它不是数据库命令，必须经过 Aida 校验后才能应用。

**记忆快照（Memory Snapshot）**：某个用户最近一次成功整理后的 Project Memory 版本。新的夜间整理失败时，报告生成继续使用上一份成功快照。

**历史项目提示（Historical Project Hint）**：Report Agent 可见的非证据归类提示。它只包含与当天 Evidence Facts 高置信度相关的项目名称、别名和有限历史概览，用于命名与归并，不得成为当天成果证据。

**夜间记忆整理任务（Nightly Memory Consolidation Job）**：为当天产生或更新有效日报的 Aida 用户执行的夜间增量任务。它生成 Memory Proposal 并由 Aida 应用为新的 Memory Snapshot；任务失败不得影响日报生成。

**系统 Project Memory Agent（System Project Memory Agent）**：归属 Aida 系统专用账号、由平台统一维护模型、Prompt、Project Memory Skill 和 Project Memory MCP 的夜间整理 Agent。测试环境 owner 为 `100866`，生产环境 owner 为 `10086`；它不出现在普通用户资产列表，也不生成日报。

**Project Memory Skill**：系统 Project Memory Agent 唯一加载的系统 Skill。它定义项目命名、父子归并、历史使用和 Memory Proposal 输出规则，不与 Report Skill 共用。

**Project Memory MCP**：Project Memory Agent 的唯一数据通道。它使用绑定实际 Aida 用户和单个 Nightly Memory Consolidation Job 的短期凭证，提供一次有界 Context 读取和一次 Proposal 写回；系统执行账号不是被整理的数据归属者。

**Project Memory Job Token**：Aida 为单个夜间记忆任务签发的短期 JWT，其中包含实际 Aida 用户身份和不可变 `project_memory_job_ref`。Project Memory MCP 依赖它隔离用户及任务，Agent 无需接触用户登录 Token。

**Evidence Fact**：Report Context 中一条可引用的结果或未解决事实。每条事实拥有仅在当前 Context 内稳定的 `fact_ref`。

**Report Brief**：Report Agent 第一次语义处理产生并由 Aida 校验接受的结构化中间产物。它把 Evidence Facts 组织为工作主题和交付物，但不是新的业务事实。

**Summary 主标题（Summary Headline）**：带 Subject 的系统个人日报 Workstream 的 `title`。它只表达该工作对象的一至两个主要成果，是工作概览的唯一来源；Demo、测试案例、验证场景、指标和其他支撑信息只能进入 Deliverables 与正文。Final Report 不再从 Deliverables 二次选择 Summary 内容。

**Workstream**：围绕同一用户目标归并的一组交付物，最终日报通常以其作为内容主题。

**Subject（稳定工作对象）**：系统默认个人日报中用于标识 Workstream 归属对象的最短、稳定名称。它可以是产品、项目、协议或业务事项，但不能是走读、调研、部署、修复、测试等活动，也不承担跨 Run 的持久业务身份。相同 Brief 内大小写不敏感且完全相同的 Subject 会合并为一个 Workstream。

**可汇报主题（Reportable Theme）**：经过完整事实审阅后，因其代表当期主要目标、明确成果或必须关注的问题而进入 Final Report 的 Workstream。可核验不等于可汇报。

**编辑选择（Editorial Selection）**：从已核验的候选工作中选择少数可汇报主题的语义判断。它决定报告重点和篇幅，不改变 Evidence Fact 的真实性、状态或环境。

**支撑事实（Supporting Fact）**：用于说明某个可汇报主题的结果、验证、状态或后续动作，但不应独立生成 Workstream 的 Evidence Fact。

**次要活动（Secondary Activity）**：有事实依据、但相对当期主要目标不值得占用独立日报条目的工作。它不是噪声，也不代表工作没有发生。

**未选事实（Unselected Fact）**：System Report Flow 已审阅、但未被 Agent 选入正常 Report Brief，也未被 Agent 用具体原因显式排除的 Evidence Fact。Report Brief Module 以内部原因 `not_selected` 自动归档它，保证证据守恒；该原因不允许 Agent 主动提交，也不返回给 Final Report 写作阶段。

**工作主线（Work Thread）**：用户围绕同一业务目标持续推进的一组工作事实。它可以跨 Session、跨代码仓库和跨日期存在，是 Workstream 归并的首要依据。

**业务项目（Business Project）**：用户工作语境中的项目或事项边界。业务项目不等同于本地目录、代码仓库或 Git 分支；一个业务项目可以涉及多个仓库，一个仓库也可以服务多个业务项目。

**技术关联信号（Technical Correlation Signal）**：用于辅助判断工作事实关系的结构化技术信息，例如 Git 仓库身份、分支、任务编号和模块路径。它只能增强工作主线判断，不能单独定义业务项目。

**工作单元字典（Work Thread Dictionary）**：Report Context 中由短 `thread_ref` 和 Digest 目标组成的内部关联字典。Evidence Fact 通过短引用关联字典项；原始 Session 和 Work Unit 标识不提供给 Agent。该字典只帮助 Report Brief 归并，不进入 Brief 契约和最终报告。

**Deliverable**：Workstream 内具有独立结果和交付状态的事项。测试服验证与生产发布必须拆成不同状态，不得互相替代。

**Final Report**：用户最终看到并保存的日报或周报内容。两阶段个人日报只能基于已接受的 Report Brief 生成。

**生产日报形态基线（Production Report Pattern Baseline）**：从多用户、多日期的生产日报中聚合出的结构分布，包括常见主题数量、层级、篇幅、合并颗粒度和表达方式。它约束整体呈现风格，不决定某个具体 Fact 是否应当出现。

**员工最终稿参考（Employee Final Reference）**：某位员工已保存的最终日报，只反映该用户当次的取舍和表达偏好。它不是该用户日的标准答案，不能单独决定 Session 中其他真实主题应被保留或排除。

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
