# 夜间 Project Memory 整理与日报历史参考方案

> 日期：2026-08-02  
> 范围：系统默认个人日报 `personal_daily`  
> 状态：方案 Review、测试服开发与代表性真实数据验收完成；尚未批准生产

## 1. 一页决策差异

### 行为变化

- 每晚只为当天产生或更新有效日报的用户整理一次 Project Memory。
- Report Agent 只获得与当天 Evidence Facts 相关的少量历史项目参考，不再直接依赖最近三份日报主题树完成跨日归并。
- Memory Agent 或 Agent 平台失败时沿用上一次成功记忆；日报生成主链路必须继续。
- 生产样本可以只读冻结后在测试服 A/B 回放，不要求先发布生产。

### 代码变化

- 把当前 `ResolveShadow` 的历史同步职责从 Report Context 构建链路移到独立夜间任务。
- `reportmemory` Module 新增夜间整理 Interface，并在内部完成候选召回、Agent 输入裁剪、Proposal 校验和持久化。
- Report Context 只读取最新成功 Project Memory，为当天 Fact 附加有限的历史项目提示。
- `reporteval` 冻结数据增加历史报告、Project Memory、token、耗时和重复运行结果。

### 新增资源

- Project Memory 夜间任务与执行记录。
- Memory Proposal 与成功快照。
- 系统专用 Project Memory Agent、Project Memory Skill 和 Project Memory MCP 配置。

### 未决项

- 无阻断开发的产品决策。自动 rename/merge 暂不启用。

### 明确不做

- 不把 Session、Digest、Work Detail、Git 状态或完整历史日报交给 Memory Agent。
- 不让 Memory Agent 直接修改数据库。
- 不把历史内容当成当天工作证据。
- 不为 Personal Report Agent 注入系统 Skill；Project Memory 系统资产与报告生成资产完全分离。
- 不在首期建设 GraphRAG、图数据库或用户工作画像。

## 2. 需求基线

以下均为已确认的 Product rule：

1. 每晚自动执行一次，只处理当天产生日报的用户；同一用户当晚最多执行一次。
2. Agent 输入必须设置数量、字符和 token 上限，避免输入完整历史造成不必要消耗。
3. Report Agent 获得的历史数据只辅助项目命名和跨日归并；当天 Facts 仍是日报事实的唯一来源。
4. 必须建立可重复的 A/B 评测，覆盖质量、稳定性、成本和失败回退。
5. 必须使用生产真实数据验收，但不得要求未经验证的代码先发布生产。
6. 没有日报的日期直接跳过；周末和节假日不做特殊过滤。
7. 人工日报的工作概览是最强来源；AI 默认保留稿可以参与，但权重较低。

## 3. 当前代码事实

- `api/internal/reportmemory.ResolveShadow` 当前在 `reportcontext.Service.Build` 中同步执行，既重建历史项目，又解析当天 Facts。
- `ResolveShadow` 的错误已被隔离，不会中断 Context，但仍占用白天日报生成请求的数据库时间。
- 当前历史来源查询只纳入 `saved/submitted` 且人工编辑、提交或存在保存/提交事件的日报；未操作但默认保留的 AI 日报语义尚未单独建模。
- `report_projects`、`report_project_aliases`、`report_project_occurrences`、状态表和影子快照已由迁移 034/035 定义。
- `service.ManagedAgentClient` 已支持提交任务和查询任务结果，可复用现有 Agent 平台连接。
- `autodailyreport` 已有轮询、租约、`FOR UPDATE SKIP LOCKED` 和幂等状态机，可作为后台任务实现参考，但其业务状态不能与 Project Memory 共表。
- `reporteval` 已能冻结来源并导出 Context、Brief 和 Generated Draft，但 token 归因当前明确为 `not_available`，也没有多日历史快照。

## 4. 领域模型

### 4.1 Final Overview

一份日报最终保留内容中的“工作概览”或等价顶层编号事项。Project Memory 只从这里识别用户选择的工作主题，不从 Work Detail 反推项目。

### 4.2 Auto-carried Report

由系统 Report Agent 生成、用户未编辑也未显式保存，但按产品规则默认保留的日报。它是有效历史参考，但来源权重低于人工保存和人工编辑稿。

### 4.3 Memory Proposal

Memory Resolver Agent 对本次主题与既有 Project Memory 关系提出的结构化建议。动作只允许：

- `link_existing`
- `create_new`
- `unresolved`
- `suggest_rename`
- `suggest_merge`

Proposal 不是数据库命令；必须由 Aida 校验后才能应用。

### 4.4 Memory Snapshot

某用户最近一次成功整理后的 Project Memory 版本。夜间任务失败时，Report Agent 继续读取上一份成功 Snapshot。

### 4.5 Historical Project Hint

Report Agent 可见的非证据提示。它把当天 `fact_ref` 与少量历史项目名称、别名和最近概览关联起来，只用于命名与归并。

### 4.6 Nightly Memory Consolidation Job

按 Aida 用户执行的夜间任务。任务从当天日报变更事件形成，负责生成并应用一份新的 Memory Snapshot。

## 5. 总体流程

```text
日报生成/保存/修改
        │
        └─ 标记 user + report_date 的 Memory Job 待处理
                         │
                每日 02:00 后领取
                         │
        服务端裁剪 Final Overview、Brief Subject、
        最近有效概览和候选 Project Memory
                         │
               System Project Memory Agent
                         │
             加载 Project Memory Skill
                         │
      Project Memory MCP 读取绑定 Job 的 Context
                         │
                  Memory Proposal
                         │
      Project Memory MCP 校验并写回绑定 Job
                         │
        Aida 校验用户归属、动作、覆盖率、长度和置信度
                         │
          ┌──────────────┴──────────────┐
          │成功                         │失败
          ▼                             ▼
   原子提交新 Snapshot          保留旧 Snapshot，记录重试
          │
          ▼
次日 Report Context 对当天 Facts 做候选召回并注入 Historical Project Hint
```

## 6. 夜间调度与状态

### 6.1 入队

- 报告首次由 Report Agent 成功写入时入队。
- 用户保存、提交或修改报告时刷新该用户当日任务的 `source_fingerprint`。
- 同一用户同一报告日期只有一个待处理状态；多次变化合并为最后版本。
- 夜间任务按“中国标准时间”计算业务日，默认 02:00 后开始领取前一业务日及上次水位线后迟到的变更。
- 不通过“扫描所有用户”发现任务，避免空跑和漏掉临近午夜的数据。

### 6.2 状态

`pending → submitting → running → succeeded | failed`

- 使用租约和 `FOR UPDATE SKIP LOCKED`，允许多 API 实例但同一任务只被一个实例领取。
- `failed` 使用有界退避重试；超过次数后等待下一夜，不阻塞日报。
- `succeeded` 记录输入指纹、Resolver 版本、模型、token、耗时和 Snapshot 版本。
- API 启动或重启后可以继续领取过期租约。

### 6.3 并发与资源

- 夜间并发默认 3，可配置为 1～5。
- 每个用户同一时刻最多一个运行任务。
- 06:00 后不再发起新 Agent 请求，正在执行的任务允许完成。
- Report Agent 始终读取最近成功 Snapshot，不等待夜间任务。

## 7. Memory Agent 输入与 token 预算

### 7.1 输入优先级

1. 当天 Final Overview：最多 8 个主题、1200 个 Unicode 字符。
2. 当天 accepted Brief 轮廓：最多 8 个 Workstream，每个只提供 Subject 与最多 4 条、每条 180 字的 Deliverable；它用于保留用户已接受的父子主题关系，不读取原始工作详情。
3. 服务端召回的既有项目：最多 8 个；每个包含标准名、最多 5 个别名、最近日期和来源权重。
4. 最近 5 份有效日报概览：每份最多 400 字；跳过无日报日期，不跳过周末或节假日。
5. 每个候选最多附带 2 条与当前主题相关的历史概览摘录。

严禁输入原始 Session、Digest、Work Detail、完整 Final Report、Git 提交/发布状态和 Agent 运行轨迹。

### 7.2 预算

- 输入预算上限：服务端以保守 token 估算器裁剪到 8000 tokens 以内，并同时执行字符和条数硬限制。
- 输出接受上限：1500 估算 tokens；超限结果不应用。
- 当前 Agent 平台任务接口没有调用级 `max_tokens` 参数，不能把 Prompt 要求描述为模型侧硬上限。首期同时使用短任务超时、单轮 JSON 输出和输出验收控制实际消耗；评测记录平台能够返回的真实用量。若后续平台提供调用级上限，Aida Adapter 再透传该参数。
- 超限时按“当天概览 → 高相关候选 → 最近历史”的顺序保留。
- 一名用户一天只发起一次正常请求；结构或传输错误最多重试一次，语义低置信度不重试。

### 7.3 Agent 输出

每个当天主题必须且只能出现一次。`link_existing` 的 `project_ref` 必须来自输入候选；`create_new` 必须给出稳定、简短的工作对象名称；无法判断时必须返回 `unresolved`。

Agent 只能建议 rename/merge，首期不得直接执行。

## 8. 服务端校验与应用

- Proposal 必须是合法 JSON，主题集合必须与输入一致。
- 允许 Agent 在 Proposal 顶层回显 `user_ref`、`report_ref` 等无害请求元数据；`decisions[]` 仍按严格字段契约解析，避免格式小偏差导致整夜任务失败。
- 所有项目必须属于当前用户。
- `link_existing` 低于高置信度阈值时降级为 `unresolved`。
- `create_new` 的名称不得是“修复、测试、部署、调研、优化”等单独活动词。
- 不完整、超长、重复或引用越权的 Proposal 整体不应用，保留旧 Snapshot。
- 首期只自动应用高置信度 `link_existing` 和合规 `create_new`。
- Snapshot 提交必须原子完成，并保留原始 Proposal、校验结果、来源指纹和版本。
- 新 Snapshot 只继承上一份成功 Snapshot 中的项目，并合并本次通过校验的项目；不得把旧影子解析遗留数据直接带入正式记忆。

## 9. Report Agent 历史参考契约

Report Context 最多提供 5 个 `HistoricalProjectHint`：

```json
{
  "project_ref": "project-uuid",
  "canonical_name": "Report Agent",
  "aliases": ["AI 日报", "日报生成优化"],
  "matched_fact_refs": ["fact-012", "fact-018"],
  "recent_context": [
    {
      "date": "2026-07-31",
      "overview": "优化日报主题归并与 Summary 稳定性",
      "source_type": "human_edited"
    }
  ],
  "instruction": "仅用于项目命名和归并，不得作为当天成果证据"
}
```

约束：

- 每个 Hint 必须至少命中一个当天 `fact_ref`。
- 每个项目最多 5 个别名和 2 条历史概览。
- 只注入高置信度候选；中低置信度不进入 Agent Context。
- 历史文本不得带入当前日报的交付状态、发布结论、指标或后续建议。
- 未发现 Snapshot、Snapshot 过期或读取失败时，字段省略并继续当前生成流程。

## 10. 来源权重

| 来源 | 领域名称 | 权重 |
|---|---|---:|
| 完全人工日报 | `manual_final` | 1.00 |
| AI 后人工修改 | `human_edited` | 0.95 |
| AI 原样显式保存/提交 | `explicit_saved` | 0.75 |
| AI 未操作默认保留 | `auto_carried` | 0.50 |
| 无法确认来源的旧数据 | `legacy` | 0.40 |

来源权重只影响候选排序和项目命名，不改变当天事实权重。

## 11. 评测方案

### 11.1 数据集

- 至少 30～50 个真实用户日。
- 至少 5～10 组连续多日样本。
- 覆盖单项目、多项目、主题模糊、长 Session、人工日报、AI 原样保存和 Auto-carried Report。
- 员工最终稿只作为取舍与表达参考，不作为唯一 GT。

### 11.2 A/B

- A：当前 `continuity_context` 流程。
- B：Project Memory + Historical Project Hint。
- 同一冻结来源、同一模型、同一参数，每组至少重复 3 次。
- 人工评审隐藏版本信息，分别评分后再聚合。

### 11.3 硬门槛

- B 的报告成功率不得低于 A；Memory 失败必须回退成功。
- 历史成果污染为零。
- 不同项目错误合并不得高于 A。
- Agent 平台原始错误不得暴露给普通用户。

### 11.4 质量指标

- 持续项目被正确归并的比例。
- 同一项目被拆分为多个 Summary 条目的比例。
- 不同项目被错误合并的比例。
- 支撑案例被错误提升为 Summary 主事项的比例。
- 专业术语保真和生造术语数量。
- 人工盲评的整体可接受率。

### 11.5 运行指标

- 每用户输入/输出 token、耗时和估算成本。
- 夜间任务成功率、重试率、陈旧 Snapshot 比例。
- A/B 三次重复生成的一致性。

## 12. 生产数据在测试服验收

生产不部署候选代码，只提供受控只读导出：

1. 选择已获授权的生产用户日。
2. 冻结当天来源身份、Digest、Report Context、Brief、最终日报、此前 5 份有效概览和当时 Project Memory。
3. 去除用户 Token、Agent Token、数据库凭证和无关个人资料。
4. 以内容哈希和来源指纹形成不可变评测包。
5. 测试服使用映射测试账号导入，生成新 UUID，保留原始日期和关联关系。
6. A/B 运行只写评测表和产物目录，不覆盖测试账号正常日报。

生成质量优先从冻结 Digest/Context 回放，避免 Session 解析变化干扰对比；最终再选择少量样本从生产 Session 切片完成端到端回归。

因此，Project Memory 可以在测试服完成完整质量验收；生产只在通过后验证真实夜间调度、模型配额和运行负载。

## 13. 架构决策

### D1：夜间任务与日报生成解耦

- 对应需求：1、3。
- 当前代码事实：`ResolveShadow` 当前由 Report Context 构建同步触发。
- 待解决问题：白天重复同步历史，并把记忆维护与日报延迟绑定。
- 候选方案：继续同步；每三天批处理；每晚增量任务。
- 选择方案：每晚增量任务，日报只读最近成功 Snapshot。
- 验证证据：现有自动日报任务已证明租约和并发领取模式可用；还需测试服故障注入验证。
- 是否已确认：是。

### D2：Agent 提议、服务端裁决

- 对应需求：2、3。
- 当前代码事实：规则匹配可以召回显式名称，但无法稳定理解跨日改名和同义主题。
- 待解决问题：提高语义关联能力，同时避免 Agent 直接污染长期记忆。
- 候选方案：纯规则；Agent 直接维护；Agent Proposal + 服务端校验。
- 选择方案：Agent Proposal + 服务端校验。
- 验证证据：需要冻结生产样本 A/B。
- 是否已确认：是。

### D3：只向 Report Agent 暴露相关 Hint

- 对应需求：3。
- 当前代码事实：当前 `continuity_context` 整体提供最近三份主题树。
- 待解决问题：全局历史文本容易被提升为当天成果，并增加 Prompt 噪声。
- 候选方案：完整记忆；最近三份原文；按当天 Fact 召回的少量 Hint。
- 选择方案：按当天 Fact 召回的少量 Hint。
- 验证证据：需要 A/B 检查 false merge、false split 和历史污染。
- 是否已确认：是。

### D4：测试服回放生产冻结数据

- 对应需求：4、5。
- 当前代码事实：`reporteval` 已支持冻结来源与导出运行产物。
- 待解决问题：测试服普通数据不能代表生产连续工作场景，但候选代码不能先上生产。
- 候选方案：生产灰度；复制生产库；受控冻结包回放。
- 选择方案：受控冻结包回放，少量 Session 端到端回归。
- 验证证据：现有评测冻结接口和生产样本回放流程可复用；需增加历史快照。
- 是否已确认：是。

## 14. 开发前 Review 结论

Review 结论：通过，没有阻断开发的方向性问题。

确认的边界：

- `reportmemory` 作为独立 Module 持有入队、输入裁剪、Proposal 校验、Snapshot 与 Hint 读取，Report Context 只依赖 Hint Interface。
- 夜间失败不修改最近成功 Snapshot，也不改变日报生成结果；普通用户不接触 Memory Agent 原始错误。
- Agent 平台当前不支持调用级 `max_tokens`，因此 token 控制由确定性输入裁剪、短单轮 JSON、输出验收和任务超时共同完成，不虚构模型侧硬限制。
- 正式 Snapshot 以自身成功版本为继承边界，不继承 034/035 影子解析产生的候选，避免历史试验数据污染新流程。
- 评测沿用现有冻结 Digest/Context/Brief/Draft 能力；`project_memory_context` 已包含在 Context 产物中，新增错误类型用于衡量错误拆分、错误合并、历史事实泄漏、支撑细节上浮和术语失真。

## 15. 测试服实现与验收记录

### 15.1 已实现

- 新增夜间 Job、成功 Snapshot、来源指纹、租约、最多两次尝试和 02:00～06:00 中国标准时间提交窗口。
- 日报 Agent 写回、用户保存和提交后按 `user + report_date` 合并入队；入队失败只记录服务端日志，不回滚已成功的日报。
- Memory Resolver 输入只包含 Final Overview、accepted Brief 的限长父子轮廓、上一成功 Snapshot 候选与最近 5 份概览，估算输入上限 8000 tokens。
- Proposal 只应用合规的 `link_existing/create_new`；低置信度关联降级为 `unresolved`，rename/merge 仅保留建议。
- Report Context 最多注入 5 个与当天 Fact 高置信度匹配的 Historical Project Hint；存在 Hint 时替换旧 `continuity_context`，无 Snapshot 或无命中时保留旧回退。
- Project Memory 使用独立系统 Agent + 独立系统 Skill + 独立系统 MCP；测试 owner 为 `100866`，生产 owner 为 `10086`。
- 每次任务签发绑定实际 Aida 用户与单个 Memory Job 的短期凭证；系统 owner 只承担 Agent 资产归属和模型额度。
- Agent 通过 `get_project_memory_context` 读取服务端冻结的有界输入，通过 `write_project_memory_result` 写回 Proposal；不再把业务 JSON 放入启动参数。

### 15.2 自动化与真实链路证据

- `cd api && go test ./...`：通过。
- `git diff --check`：通过。
- 测试服 migration：版本 036 已执行；API `/health` 返回 `{"status":"ok"}`。
- 真实夜间任务：用户 305、报告日期 2026-08-01，Resolver v3 task `1a958d24-41d1-400e-83ff-0b9fff5fdaa6` 成功。
- 真实资源：输入估算 1911 tokens、输出估算 426 tokens。
- 成功 Snapshot：`f70be967-8d6c-4ca5-acee-269f1f5cdc69`，仅包含 `IF-Knowledge`、`InfoAgent`、`GPGPU 内网安全信息系统` 三个项目，没有继承旧影子候选。
- Snapshot 从上一份 accepted Brief 识别出 `儿童睡前卡通动画生成` 是 IF-Knowledge 下 knowledge-map-search Skill 的应用场景，并以子主题别名保存；不是读取工作详情或针对固定词语写规则。
- 真实 Report Context：Run `6ea1d569-df08-4978-857c-a907a5dbe81d` 已注入上述三个 Hint，旧 `continuity_context` 未同时出现；每个 Hint 都包含当天匹配的 `fact_ref` 和“历史不是当天证据”约束。
- 同源 A/B：A Run `d2115486-d04a-4592-92f2-58dc5fea7604` 将儿童动画场景提升为第 4 个 Workstream；B Run 的首份 Brief 只有 3 个 Workstream，并把同一场景作为 IF-Knowledge 的 Deliverable，父子归并符合预期。
- B 最终 Report `805bb8e1-0cbf-4cac-bc56-178e62cc79d7` 成功写回；工作概览只保留 `IF-Knowledge`、`GPGPU 内网安全信息系统`、`InfoAgent` 三项，儿童动画场景保留在 IF-Knowledge 工作详情中。

### 15.3 当前验收状态

- 夜间队列、Agent Proposal、服务端校验、Snapshot、历史 Hint 注入和回退边界已通过测试服真实链路验收。
- 代表性同源 A/B 已完成并支持当前方向；另有一个 8 切片 Run 在读取大 Context 后超时终止，已作为 Report Agent 生成稳定性样本保留，不归因于 Memory 成功。
- 扩大到 30～50 个用户日、连续样本三次重复与人工盲评前，状态保持“测试服验证”，不自动进入生产。

### 15.4 v4 系统资产闭环验收

- Resolver v4 不再把业务 JSON 放入 Agent 启动参数；Session 只启动 `/aida-project-memory`。
- 测试系统资源归属 `100866`：Agent `aida-project-memory-system-test-v1`、Skill `aida-project-memory@project-memory-v1`、MCP `aida-project-memory-mcp@project-memory-v1`。
- 每次 Session 注入绑定实际 Aida 用户和单个 Memory Job 的短期 Credential；MCP 服务端按 `user_id + project_memory_job_ref` 读取和写回。
- 用户 305、2026-08-04 的真实 Job 成功：Snapshot `bfc0adad-5e68-4b36-a0c4-dabefdf8f9bf`，Agent Session `9bc83bb9-4403-4704-a031-561752782c19`，输入估算 2044 tokens、输出估算 310 tokens。
- Session 轨迹确认依次加载专用 Skill、调用 `get_project_memory_context`、调用 `write_project_memory_result`；没有调用 Report Skill 或 Report MCP。
- AIDA 用户 305 的 `/api/v1/ai-assets/agents` 实测不返回任何 Project Memory Agent；系统资源不写入用户 Agent Profile。

## 14. 开发任务

1. 根据第 6、13.1 节新增夜间任务状态、领取、重试和水位线。
2. 根据第 7、13.2 节实现有界输入构建、Agent Adapter、Proposal 校验与成功 Snapshot。
3. 根据第 9、13.3 节移除生成期历史重建，改为只读并注入相关 Hint。
4. 根据第 10 节把 `ai_confirmed` 拆分为 `explicit_saved` 与 `auto_carried`。
5. 根据第 11、12、13.4 节扩展冻结数据与 A/B 指标。
6. 依次完成单元测试、数据库集成测试、Agent 失败回退、测试服生产样本 A/B 和端到端验收。

## 15. 验收结论模板

只有同时满足以下条件才建议进入生产发布清单：

- 全量 Go 测试通过；迁移可前向应用。
- 夜间任务重复执行幂等，过期租约可恢复。
- Agent 超时、非法 JSON、越权项目、低置信度均不影响次日日报生成。
- 测试服至少 30 个用户日 A/B 完成，硬门槛全部通过。
- 人工盲评确认持续项目归并改善，且没有系统性过度合并。
- token、耗时和成本数据可查询，符合第 7 节预算。
