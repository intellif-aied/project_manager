# 本机 Session 样本盘点与人工 Golden Case 链路

> 阶段：第七阶段——Digest 样本验证与确定性策略增强
> 盘点日期：2026-07-21
> 状态：历史盘点材料；第七阶段已封板，不再继续批量标注或修改 Digest 规则

## 1. 目的和边界

本轮先用本机真实 Session 建立 Digest 的事实基线，再决定需要优化什么。顺序固定为：

1. 盘点真实 Session；
2. 选择有代表性的只读样本；
3. 人工逐段标注 Atomic Work Unit 和客观证据；
4. 用当前 Digest 回放同一份样本；
5. 对比 Golden 结果，定位稳定可复现的误判或漏判；
6. 只针对已经被样本证明的问题设计最小确定性修复；
7. 将修复转成自动化回归用例。

这条链路不在 Digest Builder 中接入 LLM，也不标注或生成 Work Stream。人工标注使用模型辅助阅读，是离线评测活动；Digest 生产链路仍只执行确定性解析。

本文件只记录结构统计和 opaque Session ID，不保存用户原文、Agent 长回答、完整命令、完整 diff、Base64、凭据或原始 JSONL 副本。

## 2. 扫描范围和方法

### 2.1 数据源

本轮只读扫描以下本机默认目录：

| 来源 | 结果 |
| --- | --- |
| Codex `~/.codex/sessions` | 存在，发现 254 个 JSONL 文件 |
| Claude Code `~/.claude/projects` | 不存在 |
| Claude Code `~/.config/claude/projects` | 不存在 |
| Claude Code `~/.claude/sessions` | 目录存在，但没有 JSONL 文件 |

因此，本轮真实语料只能建立 Codex 基线。Claude Code 事件结构、版本差异和工具配对暂时不能验收，不能用“修改 Codex 字段”的伪样本代替真实 Claude Session。

### 2.2 只读统计项

对每个 JSONL 流式读取并仅计算：

- 文件字节数、事件行数、首末事件日期；
- 每行能否解析为 JSON；
- 文件末尾是否为完整换行；
- 顶层事件 `type` 和 `payload.type` 的计数；
- `session_meta` 中的 source、originator 和 CLI version；
- 用户消息、Agent 消息、reasoning、工具调用、工具结果、patch、compaction、中断和回滚等结构信号；
- 只用于候选筛选的测试、构建和提交命令类型信号，不输出命令正文。

候选筛选中的“可能包含测试/构建/提交”只是结构和命令族信号，不是 Golden 事实。是否真的执行、是否成功、属于哪个 Work Unit，必须在人工标注时根据调用、结果和退出状态复核。

## 3. 本机 Codex 语料现状

### 3.1 总体规模

| 指标 | 扫描结果 |
| --- | ---: |
| JSONL 文件 | 254 |
| 总字节数 | 1,363,570,465 bytes，约 1.27 GiB |
| 事件行数 | 549,060 |
| 可解析 JSON 行 | 549,060 |
| 解析异常行 | 0 |
| 尾部没有换行的文件 | 0 |

当前快照没有天然的“残缺尾行/字段截断”样本。该场景后续应基于已经脱敏的最小 Golden Fixture 做受控截断和续传测试，不能把原始 Session 破坏后再当作样本。

### 3.2 事件结构分布

直接来自本机 JSONL 的主要顶层事件如下：

| 顶层类型 | 数量 |
| --- | ---: |
| `response_item` | 280,079 |
| `event_msg` | 253,833 |
| `turn_context` | 11,643 |
| `world_state` | 1,505 |
| `compacted` | 1,018 |
| `inter_agent_communication_metadata` | 629 |
| `session_meta` | 353 |

主要 payload 结构包括：

| payload 类型 | 数量 | 对 Digest 的意义 |
| --- | ---: | --- |
| `user_message` | 11,121 | 候选 Atomic Work Unit 边界 |
| `agent_message` | 45,789 | Agent 自述和最终回答候选 |
| `reasoning` | 71,980 | 应过滤的推理噪声 |
| `token_count` | 145,156 | 应过滤的 Token 噪声 |
| `function_call` / output | 38,594 / 38,593 | 调用与结果配对、跨批次切点 |
| `custom_tool_call` / output | 40,387 / 40,386 | patch 等工具证据与配对 |
| `patch_apply_end` | 17,458 | 文件修改证据候选 |
| `task_complete` | 10,408 | Turn 生命周期信号，不等于业务完成 |
| `turn_aborted` | 421 | 中断、恢复和保守状态判断 |
| `context_compacted` | 1,018 | 长 Session 和压缩后续执行 |
| `thread_rolled_back` | 85 | 前序方案被撤回或重新执行的结构信号 |

全语料中调用和结果各差一条，可能包含扫描时仍未闭合的活动 Session，不能直接判定为数据错误。Golden 样本应优先选择已经停止增长的文件并单独核对调用配对。

### 3.3 来源和版本差异

按每个文件第一条 `session_meta` 分类：

| source | 文件数 |
| --- | ---: |
| subagent | 153 |
| cli | 79 |
| vscode | 12 |
| exec | 10 |

共观察到 18 个 CLI version 值，范围从 `0.1.15`、`0.139.0` 到 `0.144.6`，其中还包含 alpha 版本。说明本机语料足以先验证 Codex 多入口和多版本兼容性。

首批 Golden 标注优先使用 cli、vscode 和 exec 的主 Session。Subagent Session 暂不混入主样本，因为其上游输入、镜像消息和父子关系与直接用户 Turn 不同；它们留作第二批的重复事件、父子关联和噪声过滤专项样本。

## 4. 首批推荐候选

下表只使用 `session_meta.id` 作为 opaque 定位符。日期是首末事件日期，工具计数均为结构计数；“适合场景”是选样理由，不是尚未完成的人工结论。

| 顺序 | Opaque Session ID | 入口 / 版本 | 日期 | 大小 / 事件数 | 结构特征 | 优先验证场景 |
| ---: | --- | --- | --- | ---: | --- | --- |
| 1 | `019f5a98-7c2d-7303-bf78-a227f5a119cc` | exec / 0.144.1 | 2026-07-13 | 0.12 MiB / 62 | 1 条用户消息；6 组 function、3 组 custom tool；3 个 patch 结束事件；有 task complete | 单轮修改；不同修改工具；最小标注口径校准 |
| 2 | `019f5f52-5a5e-7c02-9544-89c9600f1fdb` | cli / 0.144.3 | 2026-07-14 | 0.07 MiB / 29 | 1 条用户消息；6 组 function 调用/结果；Turn 以 aborted 结束 | 只分析或未完成任务；中断状态；不得误报完成 |
| 3 | `019eca7b-dcb1-7920-853d-6fb2c720fa3d` | vscode / 0.140.0-alpha.2 | 2026-06-15 | 0.08 MiB / 48 | 1 条用户消息；6 组 function 调用/结果；有 task complete | VS Code 入口差异；单轮基准；旧 alpha 结构兼容 |
| 4 | `019f17de-cc70-7641-b0af-c02f1bbecad0` | cli / 0.142.4 | 2026-07-01 | 0.66 MiB / 269 | 2 条用户消息；81 组工具调用/结果；3 个 patch；1 complete、1 aborted | 两个 Work Unit 边界；修改和测试信号；完成后继续或中断 |
| 5 | `019ef753-5544-7611-ac6a-db4b95a4e3cd` | cli / 0.141.0 | 2026-06-24 | 0.54 MiB / 349 | 6 条用户消息；77 组工具调用/结果；6 个 patch；5 complete、1 aborted、1 rollback | 多轮边界；测试信号；回滚；前序事实是否仍被错误保留 |
| 6 | `019f69cd-5cbc-7e31-af3a-595d22c392a3` | cli / 0.144.4 | 2026-07-16 至 07-17 | 4.37 MiB / 1,488 | 18 条用户消息；290 组工具调用/结果；20 个 patch；1 次 compaction；存在短跟进消息信号 | “继续/再检查”类 Atomic Unit；多轮持续处理；compaction |
| 7 | `019eca80-d89d-76c0-8222-4939c889bd06` | cli / 0.139.0 | 2026-06-15 至 06-22 | 24.76 MiB / 19,464 | 57 条用户消息；3,453 组 function/custom 调用结果；31 个 web search；20 次 compaction；1 aborted | 旧版本兼容；长 Session；测试/提交信号；搜索噪声过滤 |
| 8 | `019f2241-458a-7250-869f-ef36f0d1617a` | cli / 0.142.5 | 2026-07-02 至 07-17 | 41.04 MiB / 19,545 | 212 条用户消息；4,220 组 function/custom 调用结果；353 个 patch；23 次 compaction；14 aborted；1 rollback | 大量多轮、修改、测试和构建信号；中断恢复；容量中档 |
| 9 | `019f1d50-19f8-7253-a141-0c2ce417d6c0` | cli / 0.142.5 | 2026-07-01 至 07-09 | 178.38 MiB / 40,995 | 482 条用户消息；8,440 组 function/custom 调用结果；939 个 patch；54 次 compaction；13 aborted；1 rollback；存在单行大于 1 MiB | 超大 Session；容量稳定性；噪声压缩；大量证据召回 |

首轮不应直接从第 9 个超大样本开始逐字标注。先用 1～5 校准口径，确认同一事件由不同标注轮次得到一致结果，再扩大到 6～9。

## 5. 人工标注执行链路

### 5.1 样本冻结

标注开始前记录：

- opaque Session ID；
- 文件大小；
- SHA-256；
- 事件总数；
- 首末事件时间；
- 是否完整换行。

如果文件仍在增长，本轮不做 Golden 标注。原始 JSONL 始终保留在本机只读位置，不复制到远程仓库。

### 5.2 第一遍：只定边界

按字节 offset 和行序逐段读取，仅查看：

- `session_meta`、`turn_context`；
- `event_msg.user_message`；
- 对应的 user-role `response_item.message`；
- `task_started`、`task_complete`、`turn_aborted`、`thread_rolled_back`。

输出候选 Work Unit 边界，并标注消息是有效用户目标、镜像、系统/Skill 注入还是无法确定。这里不根据“继续”“还是不对”的语义把多个 Turn 合成 Work Stream。

### 5.3 第二遍：挂接客观证据

在每个候选 Work Unit 的范围内继续读取：

- `function_call → function_call_output`；
- `custom_tool_call → custom_tool_call_output`；
- `call_id` 或同类结构化关联键；
- `patch_apply_end`；
- 命令退出状态；
- 测试、构建、提交和接口验证的必要短摘要；
- Agent 最终回答和用户后续确认/否定。

证据优先按关联键，其次按 byte offset、事件顺序和 Turn 生命周期挂接。晚到结果不能因为“最近”而直接挂到最新 Work Unit。

### 5.4 第三遍：拆分事实状态

每个 Work Unit 分别标注：

- `change_state`：changed / unchanged / unknown；
- `validation_state`：passed / failed / mixed / not_run / unknown；
- `agent_claim_state`：complete / partial / failed / unclear；
- `user_feedback_state`：confirmed / rejected / none / unclear；
- `evidence_level`：claim_only / change_evidence / validation_evidence / no_evidence；
- `objective_completion`：verified / unverified / unknown，不使用“有修改或一次测试通过 = completed”。

### 5.5 复核和脱敏

第二遍独立复核以下高风险点：

- Work Unit 是否多建或漏建；
- 调用结果是否挂错；
- 失败后是否还有后续成功，或成功后是否被后续推翻；
- Agent 自述是否被误当成客观事实；
- patch、测试、构建、提交是否遗漏；
- reasoning、系统注入、Token、完整日志、完整 diff、Base64 是否误入 Golden 结果。

进入仓库的 Golden Fixture 必须经过脱敏：路径替换为稳定占位符，用户文本只保留判断边界所需的最短摘要，命令只保留命令族和退出状态，凭据与业务数据全部删除或占位。脱敏前后的结构关联键应保持稳定。

## 6. Golden Case 最小格式

建议每个 case 至少保存以下字段：

```yaml
case_id: codex-gc-001
source:
  opaque_session_id: 019f...
  source_sha256: <sha256>
  cli_source: exec
  cli_version: 0.144.1
range:
  start_offset: 0
  end_offset: 0
work_units:
  - id: wu-001
    goal:
      kind: valid_user_goal
      source_offset: 0
      summary: <脱敏短摘要>
    changes: []
    validations: []
    agent_claim_state: unclear
    user_feedback_state: none
    objective_facts:
      change_state: unknown
      validation_state: unknown
      evidence_level: no_evidence
    evidence_refs: []
annotation:
  annotator: codex-assisted-human-review
  review_state: first_pass
  unresolved: []
```

`source_sha256` 用于证明标注对应哪一份本地只读原始文件；仓库中不保存原始 Session。

## 7. 从标注到指标和修复

完成前 5 个小样本后，先生成当前 Digest 基线，不立即调规则：

| 检查 | 从 Golden 得到的判定方式 |
| --- | --- |
| 边界准确率 | 有效用户目标的漏建、多建，以及镜像/注入误建 |
| 关联保持率 | 修改、调用结果、final answer 是否属于正确 Work Unit |
| 证据召回率 | Golden 中修改、测试、构建、提交证据被提取的比例 |
| 证据准确率 | 普通查询、Agent 自述是否被误识别为客观证据 |
| 事实错误率 | failed→passed、claim-only→verified 等关键错误；目标为 0 |
| 跨批次一致性 | 在 Work Unit 边界、call/output 之间、patch 前后设置 cut point，分批与一次处理的规范化结果必须一致 |
| 幂等性 | 相同原始 SHA、版本和配置重复回放，规范化 JSON 与 hash 必须一致 |
| 噪声压缩率 | 同时记录原始字节、Digest 字节和关键事实保留率，不能只追求体积小 |
| 容量稳定性 | 对第 7～9 个样本记录 wall time、峰值 RSS、读取字节、输出字节和失败状态 |

只有同一种误判在真实样本或受控 Golden 变体中稳定复现，才进入确定性策略修改。每个修复必须同时带上原始失败 case、修复后的结构断言和分批/幂等回归，不根据单份报告观感追加自然语言关键词。

## 8. 封板时状态

### 已完成

- 完成本机 Codex 语料的全量结构扫描；
- 确认语料规模、事件类型、入口和版本分布；
- 选出从 0.07 MiB 到 178.38 MiB 的首批 9 个候选；
- 确定先小样本校准、再长 Session 扩容的顺序；
- 明确原始 Session 不上传、Golden 必须脱敏。

### 未覆盖但不再作为本阶段待办

- 本机没有真实 Claude Code JSONL，场景 18 的 Claude 部分仍缺样本；
- 当前快照没有天然 JSONL 截断或残缺尾行，需由脱敏 Fixture 构造受控 cut case；
- 候选 1～5 的测试、构建和提交信号尚未逐条人工核验；
- 已另外完成首个短只读校准 Case `GC-S09`，候选清单中的单轮修改 Case 尚未开始标注。

### 封板处理

- 候选清单只作为未来问题定位的索引，不继续按顺序执行；
- 不因本次盘点扩充 `classifyCommand`、建设长期 Golden 平台或引入 LLM；
- 只有生产故障或可复现的报告质量问题明确归因到 Digest 时，才从候选中选择最小样本重新立项；
- 原始 Session 继续留在本机，不上传、不提交。
