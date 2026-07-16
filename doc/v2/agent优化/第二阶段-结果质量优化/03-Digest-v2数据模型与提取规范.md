# Session Digest v2 数据模型与提取规范

> 状态：v2.1 已实施的跨模块权威契约
> Digest 版本：`session-digest/v2.1`
> 读取模式：`digest_v2`
> 脱敏版本：沿用 `report-redaction/v1`，规则变化时单独升版

## 1. 设计目标

v2 不追求复述完整对话，而是提供一份可审计的工作结果索引：

```text
用户目标
  -> Work Unit
  -> 文件/测试/提交/部署/决定等 Evidence
  -> 完成状态
  -> Result Statement
```

Report Agent 可以合并语言，但不能绕过证据状态。

## 2. 顶层响应

`get_sessions` 在 `digest_v2` 模式下仍返回单页冻结 payload：

```json
{
  "source_mode": "explicit",
  "content_mode": "digest_v2",
  "digest_version": "session-digest/v2.1",
  "redaction_version": "report-redaction/v1",
  "content_snapshot_at": "2026-07-16T16:30:00+08:00",
  "report_period": {
    "start": "2026-07-16",
    "end": "2026-07-16",
    "timezone": "Asia/Shanghai"
  },
  "completeness": "complete",
  "has_more": false,
  "next_cursor": null,
  "coverage": {
    "complete": true,
    "source_item_count": 1,
    "represented_item_count": 1,
    "source_event_count": 18008,
    "included_event_count": 980,
    "omitted_event_count": 17028,
    "source_work_unit_count": 265,
    "detailed_work_unit_count": 18,
    "aggregated_work_unit_count": 247,
    "truncated_item_count": 1
  },
  "budget": {
    "target_bytes": 65536,
    "hard_limit_bytes": 131072,
    "actual_bytes": 32768,
    "compaction": "result_focused"
  },
  "items": []
}
```

与 v1 相同，禁止返回原始 events、payload、reasoning、完整工具输出、源码、diff、图片、Token 遥测或凭据。

## 3. Item 数据模型

```json
{
  "source_item_ref": "<opaque>",
  "session_ref": "<internal>",
  "agent_type": "codex",
  "activity_start_at": "2026-07-14T11:00:00+08:00",
  "activity_end_at": "2026-07-16T16:22:20+08:00",
  "digest_sha256": "<sha256>",
  "session_summary": {
    "primary_result_count": 3,
    "verified_result_count": 2,
    "decision_count": 6,
    "unresolved_count": 1,
    "status_counts": {
      "completed": 7,
      "partial": 2,
      "blocked": 0,
      "failed": 0,
      "pending": 4,
      "unknown": 1
    }
  },
  "work_units": [],
  "discussion_aggregates": [],
  "coverage": {
    "source_event_count": 18008,
    "included_event_count": 980,
    "omitted_event_count": 17028,
    "source_work_unit_count": 265,
    "detailed_work_unit_count": 18,
    "aggregated_work_unit_count": 247,
    "truncated": true,
    "representation": "result_focused"
  }
}
```

`session_ref` 和所有 ref 只用于 Agent 内部关联，不得进入报告正文。

## 4. Work Unit

### 4.1 结构

```json
{
  "work_unit_ref": "wu:7c4d...",
  "sequence": 42,
  "activity_start_at": "2026-07-16T09:10:00+08:00",
  "activity_end_at": "2026-07-16T09:42:00+08:00",
  "period_relation": "overlap",
  "goal": {
    "text": "实现 AIHUB 展示名缓存与管理员刷新",
    "source": "user_message"
  },
  "category": "implementation",
  "status": "partial",
  "evidence_grade": "A",
  "result_statements": [
    {
      "text": "相关后端和设计文档发生持久化变更",
      "source": "derived_evidence",
      "evidence_refs": ["ev:patch-1", "ev:patch-2"]
    },
    {
      "text": "go test 与前端 build 成功，但 npm test 仍失败",
      "source": "derived_evidence",
      "evidence_refs": ["ev:test-1", "ev:build-1", "ev:test-2"]
    }
  ],
  "agent_claims": [
    {
      "text": "已完成展示名缓存和管理员刷新机制",
      "support": "partially_supported"
    }
  ],
  "changes": [
    {
      "path": "gateway/internal/logic/loginLogic.go",
      "operation": "update",
      "evidence_ref": "ev:patch-1"
    }
  ],
  "validations": [
    {
      "name": "go test",
      "attempts": 1,
      "last_status": "passed",
      "last_occurred_at": "2026-07-16T09:40:00+08:00",
      "evidence_ref": "ev:test-1"
    },
    {
      "name": "npm test",
      "attempts": 1,
      "last_status": "failed",
      "last_occurred_at": "2026-07-16T09:41:00+08:00",
      "evidence_ref": "ev:test-2"
    }
  ],
  "deliveries": [],
  "decisions": [],
  "unresolved": [
    {
      "text": "前端测试失败仍需定位",
      "evidence_ref": "ev:test-2"
    }
  ]
}
```

### 4.2 边界

1. 一个有效用户消息开启一个候选 Work Unit；
2. 直到下一个有效用户消息、turn abort/rollback 或 Session 结束；
3. `task_complete`、final answer 和对应工具结果属于当前 Work Unit；
4. compaction 前后的事件按 cursor 连续关系处理；
5. rollback 后被撤销的分支标记为 `superseded`，不作为最终结果；
6. 同一 Agent 回复在 `response_item.message`、`event_msg.agent_message`、`event_msg.task_complete` 中的镜像事件按 turn、规范文本和 cursor 邻接关系去重；
7. 自动注入的环境、权限、AGENTS、系统说明不创建 Work Unit。

每个候选用户轮次都被遍历。没有结果证据的纯问答可以进入 `discussion_aggregates`，但 coverage 必须计数。

## 5. Category

固定枚举：

| category | 含义 |
| --- | --- |
| `implementation` | 代码、配置、数据或运行行为发生变更 |
| `document` | PRD、ADR、设计、说明或其他文档产出 |
| `decision` | 用户明确接受或否决一个方案 |
| `investigation` | 完成诊断、分析、盘点或结论 |
| `verification` | 主要工作是测试、构建或核验 |
| `deployment` | 构建镜像、发布、重启、健康检查或线上验证 |
| `discussion` | 提问、建议、澄清，尚未形成交付 |
| `administrative` | 环境说明、状态查询等低价值元信息 |

分类优先使用工具和持久化证据；只有自然语言时最多判为 `discussion`、`decision` 或 `investigation`。

## 6. Status 状态机

### 6.1 基本规则

| 状态 | 必要条件 |
| --- | --- |
| `completed` | 目标有对应结果，且没有未解决的关键失败；实现类至少有 B 级证据 |
| `partial` | 有持久化结果，但部分验证失败、范围未完或 Agent 明确说明剩余项 |
| `blocked` | 存在阻止继续的外部/权限/依赖问题，且后续没有解决证据 |
| `failed` | 执行已终止并有明确失败证据 |
| `pending` | 仍在等待用户决定、下一步执行或尚未开始 |
| `unknown` | 证据不足，不能可靠判断 |

### 6.2 验证归并

验证 key 由规范命令族、工作目录和 Work Unit 组成。按时间记录 attempts：

```text
failed -> failed          => 最终 failed
failed -> passed          => 最终 passed，不保留为 blocker
passed -> failed          => 最终 failed
unknown -> passed/failed  => 使用后者
```

不同 Work Unit 的同名命令不得互相覆盖。

### 6.3 Blocker

只有以下证据可形成 unresolved blocker：

- 明确非零退出或平台失败，且影响目标完成；
- Agent 最终明确说明无法继续，并能关联当前 Work Unit；
- 权限、外部服务、来源、预算或版本错误阻止执行；
- 等待用户提供必需信息，且当前任务无法继续。

以下文本不能单独成为 blocker：

- “阻塞项页面如何展示”；
- “不提供全部强制终止”；
- “考虑失败场景”；
- 文档中引用“未完成/blocked”；
- 已在后续解决的临时错误。

## 7. Evidence

### 7.1 证据类型

| kind | 证据 |
| --- | --- |
| `file_change` | apply_patch、Edit、Write 等持久化写入 |
| `validation` | test/lint/typecheck/build 及退出状态 |
| `commit` | Git commit 成功和 commit SHA |
| `runtime_change` | Docker/服务/进程启动或切换 |
| `health_check` | 健康检查、端口或状态确认 |
| `api_check` | 有界 HTTP 状态与关键响应断言 |
| `artifact` | 镜像、包、文档、报告等不可变产物 |
| `accepted_decision` | 用户明确同意/否决的决定 |
| `agent_claim` | Agent 最终声明，仅作弱证据 |
| `failure` | 未解决的命令、平台或依赖失败 |

### 7.2 证据等级

| 等级 | 定义 | 可用于日报 |
| --- | --- | --- |
| A | 可执行或外部状态验证 | 可写“已验证/已发布/运行正常” |
| B | 持久化文件、commit 或 artifact | 可写“已实现/已产出”，需说明验证状态 |
| C | 只有 Agent final claim | 只能写“Agent 声明完成，缺少验证证据”或降低强度 |
| D | 提议、问题、讨论 | 只能写“讨论/形成建议/待确认” |

Work Unit 的 `evidence_grade` 为其中最高等级，但完成状态仍需考虑失败和未完成项。

### 7.3 Evidence Ref

Evidence ref 使用稳定、不可猜业务 ID 的摘要引用：

```text
ev:<kind>:<sha256-prefix>
```

hash 输入包括 Digest 版本、event content hash、cursor 和规范 kind。MCP 返回短 ref，数据库保留完整内部映射。报告正文禁止输出 ref。

### 7.4 工具输出归约规则

工具证据采用固定优先级：

```text
结构化工具事件/显式退出码
> 已识别的 JSON 或 NDJSON 格式
> 有完整 fixture 的稳定文本状态机
> 有界 unknown
```

禁止通过“包含 passed/success/failed/error”等通用关键词单独确定状态。专用 reducer 必须返回：

- `recognized`：是否确认输入属于受支持格式；
- `command_family`：规范命令族；
- `status`：`passed/failed/unknown`；
- `exit_code`：存在时保留；
- `attempt_summary`：有界统计；
- `failure_summary`：失败时最多 256 bytes；
- `source_event_refs`：内部事件引用。

首期 reducer 范围：

| reducer | 启用条件 | 输出 |
| --- | --- | --- |
| `process_exit` | 存在结构化退出码或平台工具状态 | 命令族、exit code、passed/failed |
| `file_change` | apply_patch/Edit/Write 等结构化调用 | operation、规范路径 |
| `go_test_ndjson` | 输出逐行满足 Go test JSON 事件格式 | package/test 统计、最终失败 |
| `test_summary` | pytest/Jest/Vitest 等已建立 fixture 的稳定摘要 | pass/fail/skip 统计、关键失败 |
| `git` | commit/status/diff-stat 等明确子命令和格式 | commit 或有界变更统计 |
| `runtime_check` | Docker、health、HTTP 状态具有结构化结果 | 服务、状态码或健康结论 |
| `generic_unknown` | 其他或格式不匹配 | 命令族和 `unknown`，不保留原文 |

约束：

1. reducer 只解析已上传并已投影的历史事件，不执行或改写命令；
2. 不依赖 RTK 二进制、Hook、tee 文件或本地配置；
3. 专用 reducer 无法识别时必须降级 `generic_unknown`；
4. 不存在 raw fallback；原始 stdout/stderr、Header、body、diff 和日志正文不得进入 Digest；
5. reducer 版本由 Digest 版本冻结，规则变化必须升 Digest 版本；
6. reducer 内部诊断信息进入评测产物或服务指标，不为了调试扩大 MCP payload。

## 8. Result Statement

结果声明来源分为：

- `derived_evidence`：根据结构化证据生成的有限模板，可信度较高；
- `agent_final`：来自最终回复，必须评估 support；
- `accepted_decision`：来自用户明确确认；
- `human_annotation`：只用于离线 Gold，不进入生产 Digest。

`support` 固定为：

- `supported`
- `partially_supported`
- `unsupported`
- `not_applicable`

生产 Builder 不用 LLM判断语义等价。保守规则无法证明时不得升级为 `supported`。

## 9. 时间与报告周期

Digest Revision 本身与完整切片绑定，保存每个 Work Unit 的开始/结束时间。

selection assembler 根据报告 period 计算：

- `before`
- `overlap`
- `after`
- `unknown`

规则：

1. 显式选择来源仍是强制证据，不因 period relation 被删除；
2. 默认来源优先详细表示 `overlap` 的结果 Work Unit；
3. 非 overlap Work Unit 可以聚合，但必须保留 coverage；
4. 日报 Agent 必须把跨期结果表述为补充或持续工作，不能全部写成当天新完成。

## 10. Discussion Aggregate

大量问答式设计 Session 不应占满 `outcomes`。无 A/B 级证据的相邻讨论 Work Unit 可聚合：

```json
{
  "topic": "Chat Session 能力管理边界",
  "work_unit_count": 18,
  "activity_start_at": "...",
  "activity_end_at": "...",
  "accepted_decision_count": 4,
  "pending_question_count": 3,
  "headline_decisions": [
    "P0 能力集合采用 append-only",
    "Agent 只能提出能力需求，不能自行启用"
  ]
}
```

topic 优先来自文档路径、明确标题或重复关键词；无法确定时使用“产品/设计讨论”，不得由 LLM生成。

## 11. 噪声和注入识别

在文本过滤前必须先递归解析字符串中的 JSON content block，不能只对原字符串做前缀判断。

排除：

- `# AGENTS.md instructions for ...`；
- `<INSTRUCTIONS>`；
- `<environment_context>`；
- `<permissions instructions>`；
- system/developer 规则；
- session meta、turn context、thread settings；
- reasoning/thinking；
- token_count、usage、cost；
- MCP 和工具完整输入输出；
- base64、图片和二进制；
- 自动恢复/压缩历史包装。

用户正文中引用这些内容做业务讨论时，可作为目标文本保留，但必须来自明确 user role，而不是自动注入事件。

## 12. Budget

### 12.1 单切片

- 持久化 Revision 硬预算：64 KiB，用于先保留跨日 `daily_summaries`，再做报告期投影；
- 报告期单 item 投影预算：16 KiB，只保留目标日期的高价值 highlights；
- 先保留 A/B 级结果、最终失败、明确决定和 report-period overlap；
- C/D 级讨论优先聚合；
- 每个 Work Unit 的 goal/claim 单条上限 384/512 bytes；
- evidence summary 单条上限 256 bytes；
- 文件路径单条上限 256 bytes。

### 12.2 Selection

- 正常目标仍为 64 KiB；
- 硬上限仍为 128 KiB；
- 所有 item 公平压缩；
- 不得删除整个来源 item；
- 超过硬上限明确失败，不回退 full。

### 12.3 优先级

```text
未解决失败
> A 级结果
> B 级结果
> 已接受决定
> report-period overlap 的调查结论
> C 级 Agent 声明
> D 级讨论聚合
> administrative
```

## 13. 稳定性与版本

- v2.1 使用新的稳定 JSON 字段顺序和 hash；
- v1 Revision 永不原地改写；
- 相同完整切片可以同时拥有 v1/v2.1 Revision；
- v1 与 v2 使用不同 processing job type，两个 Worker 不得互相 claim；
- selection 附着后冻结具体 digest version、redaction version、payload、hash 和预算；
- v2 extractor、报告期投影或 reducer 规则变化必须升 Digest 版本，不能只改代码不升版；
- v2 构建失败只改变 v2 Revision/job 状态，不修改 Session content、usage、metering 或 v1 Revision。

## 14. Report Skill 使用约束

Report Agent 必须按以下顺序使用信息：

1. `completed/partial` 且 A/B 级的结果；
2. 验证状态；
3. accepted decisions；
4. unresolved/failed；
5. discussion aggregates。

禁止：

- 根据文件名猜业务效果；
- 把 `agent_claim` 当成已验证事实；
- 把 D 级讨论写成实现；
- 隐藏失败验证；
- 输出 refs、hash、UUID 或内部字段。
