# Report Brief 单次提案与服务端编译方案

> 日期：2026-08-06  
> 状态：测试服实现并完成固定数据集回放，尚未批准生产  
> 范围：System Report Flow 的默认个人日报 `personal_daily`

## 1. 决策

保留 Report Brief，但把它从“Agent 必须反复通过的两阶段协议”改为“AIDA 内部保存的编译产物”。

新的主链路只需要一次模型语义处理：

```text
get_report_context
        ↓
Agent 一次性提交结构化 Report Draft
        ↓
AIDA Report Compiler
  ├─ 修复与归一化
  ├─ 局部安全过滤
  ├─ 保存 Report Brief
  ├─ 确定性渲染日报
  └─ 完成 Report Run
```

不再执行：

```text
write_report_brief → 报错 → 重写完整 Brief → 再报错 →
write_report_result → 再校验 → 降级写入
```

一句话概括：**一次语义提案，服务端编译到底；质量可以降级，日报不能因为 Brief 协议失败。**

## 2. 为什么 Brief 仍然保留

Brief 仍有三项不可替代的价值：

- 把 Facts 归并成 Workstream，是日报质量最关键的语义结果；
- 保存 `fact_ref`，可以审计日报每条内容来自哪里；
- Project Memory、质量评测和生成快照需要结构化主题，不能只解析最终 Markdown。

问题不在 Brief 本身，而在当前 Interface：Agent 既负责语义判断，又被迫理解 JSON 闭合、字段长度、敏感模式、Fact 归档、重试计数和最终写回协议。这个 Interface 太浅，把服务端复杂度暴露给了模型。

## 3. 当前机制的问题

### 3.1 重试没有新信息

`get_report_context` 只读取一次，后续每次 Brief 修正面对的是同一份冻结证据。重试主要在修改格式和躲避服务端正则，不是在改善事实理解。

### 3.2 一条内容拖垮整份 Brief

当前 `normalizeDraft` 会聚合结构、Fact 引用、长度、敏感内容、运维痕迹和 Agent 判断问题，只要存在一项就拒绝整个 Brief。模型需要重新生成完整 JSON，可能修好旧问题又引入新问题。

### 3.3 第二阶段已经不是模型任务

接受 Brief 后，服务端 `ReaderReport()` 已经确定性生成个人日报正文。Agent 再传一次 `summary/content/brief_hash` 并没有新的语义价值，只增加一次工具调用和失败机会。

### 3.4 降级仍依赖 Agent 配合

虽然 Skill 要求重试耗尽后不带 `brief_hash` 写入，但 Agent 可能停住、调用 `write_report_failure` 或没有写回。兜底不应依赖刚刚已经违反协议的模型继续正确执行协议。

## 4. 新的深 Module

新增或深化 `Report Compiler` Module。它的外部 Interface 建议只有一个入口：

```go
type ReportCompiler interface {
    CompileAndStore(ctx context.Context, request CompileRequest) (CompileResult, error)
}

type CompileRequest struct {
    UserID string
    RunID  string
    Draft  ReportDraft
}

type CompileResult struct {
    BriefHash  string
    Content    string
    Summary    string
    Mode       string   // accepted | repaired | fallback
    Warnings   []string // 只用于内部观测
}
```

该 Module 的 Implementation 隐藏：

- Run 与用户身份、Context Hash 和可写状态校验；
- Draft 解析、结构归一化、Fact 绑定和自动未选归档；
- 内容安全过滤、部分成果保留和 Context 兜底；
- Report Brief、日报、生成快照和 Run 状态的事务写入；
- 幂等、并发和编译元数据。

MCP Handler 只做绑定身份和输入适配，不再复制重试、修复或渲染逻辑。测试直接跨 `CompileAndStore` Interface 验证最终 Brief、日报和降级模式。

## 5. Agent 可见 Interface

System Report Agent 的 managed toolset 保留：

1. `get_report_context({})`
2. `write_report_result(<structured draft>)`
3. `write_report_failure(...)`，仅用于 Context 无法读取或真实系统错误

其中 managed `write_report_result` 改为直接接收：

```json
{
  "workstreams": [
    {
      "subject": "稳定项目或工作对象",
      "title": "用户可读的一句话成果",
      "deliverables": [
        {"result": "关键推进", "fact_refs": ["fact-001"]}
      ]
    }
  ],
  "no_reportable_work": false
}
```

不再要求 Agent：

- 把对象二次序列化进 `brief_json` 字符串；
- 处理 `brief_hash`；
- 读取 Accepted Brief 后再提交一次相同内容；
- 根据多条 `REPORT_BRIEF_INVALID` 重写完整 Draft；
- 理解自动 Fact 归档、重试计数和降级协议。

Personal Report Agent、周报、团队报告和部门报告继续使用现有 `content/summary` Interface，不受本方案影响。新旧 managed 协议必须由 Run 中的协议版本固定，不能只依赖当前开关。

## 6. 服务端编译策略

### 6.1 错误分级

| 类型 | 示例 | 行为 |
| --- | --- | --- |
| 系统完整性错误 | Token/Run 错配、Context 不存在、数据库失败 | Run 失败；前端只显示用户可理解的通用错误 |
| 可修复结构问题 | 重复引用、未知 Fact、超长、缺少可选字段、重复 Subject | 服务端确定性修复并记录 warning |
| 局部内容风险 | 某条含凭证、地址、运维痕迹或 Agent 建议 | 只丢弃或清理该条，不拒绝其他 Workstream |
| 质量问题 | 项目名一般、拆分略多、信息偏短 | 接受并进入评测，不作为生成失败条件 |

安全规则不能放松：凭证、内部地址和敏感标识仍不得进入日报。变化是从“发现一条就退回整份 Brief”改为“隔离不安全片段，保留其他安全成果”。

### 6.2 编译顺序

1. 读取冻结 Context 和有效 `fact_ref` 集；
2. 解析 Agent Draft，JSON-RPC 参数本身无效时进入空 Draft 兜底；
3. 去除未知、重复引用并自动归档未选 Facts；
4. 合并相同 Subject，删除空 Workstream；
5. 每个 Workstream 最多保留 3 个安全 Deliverable；
6. 对超长文本按完整句收缩，不翻译或改写专业名称；
7. 某条命中凭证、内部位置或明显 Agent 建议时丢弃该条；
8. 仍有有效 Workstream 时保存 Brief 并确定性渲染；
9. 没有有效 Workstream 时进入 Context Fallback；
10. 同一 Run 重复提交返回第一次成功编译结果，不再次生成。

### 6.3 Context Fallback

Fallback 不是再调用一次模型，而是从冻结 `work_evidence.facts` 生成最低可用日报：

- 排除纯 trace、Assistant 建议、凭证和内部位置；
- 优先保留有明确工作对象、用户行为或问题定位的 Fact；
- 按 Thread/Workspace 去重，最多选 3 条；
- 只做安全清理和长度收缩，不推导项目名、完成、测试通过或发布状态；
- 如果没有任何安全 Fact，写入现有标准文本 `本期无可核验的工作记录`，Run 仍以成功但 `fallback` 模式结束。

Project Memory 只能帮助 Fallback 给已锚定 Fact 使用稳定项目名，不能为无锚点 Fact 创造归属。

### 6.4 Missing Writeback 兜底

如果 Agent Task 已结束但从未调用写入工具，Run Processor 不再标记 `REPORT_WRITEBACK_MISSING` 后直接失败，而是调用：

```go
CompileAndStore(ctx, CompileRequest{UserID: userID, RunID: runID, Draft: ReportDraft{}})
```

由同一 Context Fallback 完成日报。这样空参数、模型提前结束或 Agent 平台工具调用丢失都不会绕过兜底。

## 7. Skill 调整

Skill 只保留模型真正擅长的语义规则：

- 阅读全部 Facts，但只选择读者值得看到的成果；
- 按稳定项目、产品或能力归并；
- Project Memory 是可忽略的命名参考；
- 不把 Git、测试、构建、发布和 Agent 建议写成结果；
- 专业名称保持原文，普通动作使用自然中文；
- 一次调用 `write_report_result` 后结束。

删除 JSON 闭合检查、错误码分支、重试次数、Brief Hash、兼容字段和降级写回说明。Skill 变短后，模型可以把注意力放在项目关联和内容取舍上。

## 8. 数据与观测

Report Brief 继续保存，另记录以下编译元数据：

```json
{
  "protocol_version": "compiled-draft/v1",
  "compile_mode": "accepted|repaired|fallback",
  "repair_codes": ["unknown_fact_dropped", "unsafe_deliverable_dropped"],
  "used_fact_count": 6,
  "auto_archived_fact_count": 21,
  "fallback_reason": "empty_agent_draft",
  "model_id": "...",
  "skill_version": "..."
}
```

这些字段只用于开发评测和后台统计，不返回普通用户，也不写入日报。

## 9. 评测方案

### 9.1 确定性测试

- 正常 Draft 一次完成并保存 Brief/日报；
- `{}`、截断字段、未知 Fact、重复 Fact、超长内容均能产生日报；
- 某个 Deliverable 含敏感内容时只删除该条；
- Agent 未写回时触发同一 Fallback；
- 同一 Run 重复调用幂等；
- Context Hash、用户或 Run 错配仍然拒绝；
- Personal Report Flow 和其他报告类型完全不变。

### 9.2 真实 A/B

使用 Project Association v2 的 12 个固定 Case，并补充以下协议故障 Case：

- 空 Draft；
- 部分 Workstream 可用、部分无效；
- 过长内容；
- 运维痕迹和 Agent 建议混入；
- Source Coverage 不足；
- 同 Workspace 出现新项目，验证旧 Project Memory 不吸附。

对同一冻结 Context 比较旧流程与新流程，至少记录：

- Run 成功率必须为 100%；
- managed 工具链正常为 `Context → Result` 两次调用；
- `fallback` 比例和原因；
- 项目关联门禁；
- 每份主题数、Deliverable 数、正文长度；
- 人工判断的信息完整度、误合并、历史污染和可读性。

不以“重试次数减少”单独证明日报质量。

## 10. 实施阶段

1. 在现有 `reportbrief` 基础上实现 `Report Compiler`，先只做 Interface 测试；
2. 增加 `compiled-draft/v1` managed tool schema 和短 Skill，保持旧协议可回退；
3. 测试服运行确定性故障 Case 和 12 Case A/B；
4. 人工确认日报结果后再形成生产发布清单；
5. 生产切换后观察 `accepted/repaired/fallback` 分布，再决定是否删除旧 Brief 重试代码。

本方案不要求引入第三方 Agent 框架，也不调整 Project Memory、Digest 或 Projection。第三方项目只作为控制循环和结构化输出设计参考。

## 12. 测试服实施结果（2026-08-06）

- 测试 API 已实现 `Context → structured write_report_result` 单次语义提交，System Agent 不再暴露 `write_report_brief`。
- Report Compiler 对未知 Fact、局部不安全结果和空 Draft 采用 `accepted / repaired / fallback` 编译模式；Agent 漏写时由状态同步器调用同一兜底路径。
- `100866/aida-report@1.1.42` 的 Registry `SKILL.md` 与 API 生成正文 SHA256 均为 `fdae9376120c301fb0ca6aa924a4efb3af02555442ae75e65e792ac2306d6350`。
- Project Association v2 的 12 个固定 Case 均成功生成日报：8 个 `accepted`、3 个 `repaired`、1 个 `fallback`，生成成功率 12/12。
- 项目关联门禁仅通过 5/12；失败集中在父项目名称召回和独立评测套件命名。该结果证明协议稳定性改善，不证明项目关联质量通过。
- 受控结果：`/home/intellif/evaluation/project-association-regression-v2/candidate-1.1.42.json`、`replay-1.1.42/results.psv`、`replay-1.1.42/evaluation.json`。

## 11. 风险与回退

| 风险 | 控制 |
| --- | --- |
| 服务端修复掩盖模型质量下降 | 保存 `compile_mode` 和 repair codes；后台单独统计 |
| Fallback 内容较差 | 只在没有可用 Draft 时启用；保留人工编辑能力 |
| 安全清理误伤技术术语 | 仅删除命中的危险片段，不翻译或同义改写剩余内容 |
| 新旧 Skill/工具不一致 | Run 固定协议版本，测试与生产资产成套发布 |
| 影响个人 Agent | Personal toolset 和写入逻辑不改 |

回退时把 System Report Run 的协议版本切回现有 `brief-v1`，旧表和旧 Skill 保留到新流程稳定后再清理。
