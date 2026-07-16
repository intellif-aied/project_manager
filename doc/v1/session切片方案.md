# Session 活动切片方案

## 1. 背景与问题

当前日报、周报、Report MCP、Token 明细里对 session 的时间理解存在偏差：

1. `Report MCP get_sessions` 当前按 `sessions.started_at` 过滤。
2. `/api/v1/tokens/sessions?from=&to=` 当前按 `DATE(sessions.started_at)` 过滤。
3. `/api/v1/tokens` 当前按 `token_usage.recorded_at` 聚合，而 `recorded_at` 是服务端入库时间，不等于 session 内真实活动时间。
4. daemon 上传的是整条 session 的 `started_at / ended_at / token_usage` 总量，没有把跨天 session 拆成日期范围内的活动内容。

这会导致跨天长 session 出现两个严重问题：

1. session 开始于前一天，但今天仍有大量工作，今天的日报和 MCP 取不到。
2. 即使把跨天 session 查出来，如果仍返回整条 session summary 和整条 token，Agent 也会把非当天内容写进当天日报。

示例：

```text
session A
started_at = 2026-06-30 10:00 +08:00
ended_at   = 2026-07-02 18:00 +08:00

当前查询 2026-07-02：
- MCP get_sessions 查不到，因为 started_at 不在 2026-07-02。
- /tokens/sessions 查不到，因为 DATE(started_at) 不是 2026-07-02。
- 如果改成 started_at/ended_at 交集能查到，也仍可能返回 6/30-7/2 的整条内容。
```

因此，这不是单纯 SQL 条件问题，而是 session 时间语义建模问题。

## 2. 目标

本方案目标是把 session 从“按开始时间归属”改为“按真实活动日期归属”。

### 2.1 必须达到的目标

1. Report MCP 返回的 session 内容必须落在调用方传入的 `date_range` 内。
2. Agent 调 skill 生成日报/周报时，拿到的是范围内活动内容，不是整条跨天 session。
3. `/tokens/sessions` 的 `from/to` 必须按活动日期筛选，不再按 `sessions.started_at`。
4. `/tokens` 与 `/tokens/sessions` 必须使用同一套时间口径，避免总数和明细不一致。
5. session 上传重复执行必须幂等，不能导致切片或 token 重复累加。
6. 业务日期按 `Asia/Shanghai` 计算，不能依赖数据库或容器默认时区。

### 2.2 非目标

当前仍处于开发期，不考虑历史 API 兼容，不新增 MCP 版本，不保留旧 started_at 过滤语义。

本方案不解决以下问题：

1. 普通用户是否可以在日报页面选择 Agent。
2. 日报/周报 UI 的生成流程设计。
3. Agent 平台的选择器、默认 Agent、Skill 配置。
4. 历史无 raw log 数据的精确恢复。

这些属于后续产品链路和历史数据治理问题。

## 3. 设计原则

### 3.1 session 元数据与活动事实分离

`sessions` 表表示整条会话元数据：

- session_ref
- user_id
- agent_type
- started_at
- ended_at
- raw_log_url
- 整体 summary

它不能继续承担日报/周报的日期归属职责。

新增活动切片表表示某个 session 在某个业务日期内发生的活动：

- activity_date
- activity_start_at
- activity_end_at
- activity_summary
- activity_excerpt
- 当日 token
- 当日 tool calls

日报、周报、MCP、Token 明细都应基于活动切片。

### 3.2 时间范围必须返回范围内内容

MCP `get_sessions` 的 `date_range` 语义应定义为：

> 返回当前用户权限范围内，在该日期范围内产生过活动的 session 活动切片。

不是：

> 返回 started_at 在该范围内的 session。

也不是：

> 返回 uploaded_at 在该范围内的 session。

### 3.3 Token 统计必须按活动日期聚合

`recorded_at` 只能表示 token 记录写入时间。它不适合做业务日期筛选。

Token 统计应该以活动切片的 `activity_date` 为准。

### 3.4 开发期直接替换旧语义

由于当前仍在开发期：

1. 不保留旧 MCP 返回语义。
2. 不新增 MCP v2。
3. 不保留 `/tokens/sessions` 的 started_at 过滤。
4. 相关测试、文档、前端展示同步改成 activity 语义。

### 3.5 主流平台参考

主流 LLM observability / tracing 平台通常不是把 session 当作最小事实单元，而是采用类似下面的分层：

```text
Session / Thread
  逻辑分组：一次对话、一次 agent workflow、一次评估 run

Trace / Run
  一次请求、一次用户 turn、一次工作流执行

Span / Observation / Request / Event
  具体步骤：LLM 调用、tool call、retrieval、API 调用、用户消息
  这里承载 start/end time、event timestamp、token、cost、latency、input/output
```

参考平台：

| 平台 | 主流建模方式 | 对本方案的启发 |
| --- | --- | --- |
| [Langfuse](https://langfuse.com/docs/observability/data-model) | `Observation -> Trace -> Session`，Session 通过 `sessionId` 聚合多个 traces/observations | session 是分组容器，时间和 token 应落在 observation/generation 上 |
| [LangSmith](https://docs.langchain.com/langsmith/observability-concepts) | `Run -> Trace -> Thread`，多轮对话通过 `session_id/thread_id` 关联 | thread/session 不应承担单条工作事实和 token 事实 |
| [OpenTelemetry](https://opentelemetry.io/docs/concepts/signals/traces/) | `Span` 是工作单元，`Event` 有独立 timestamp | 需要保留事件/步骤时间，而不是只看容器开始时间 |
| [Helicone](https://docs.helicone.ai/features/sessions) | Session 聚合相关 requests，request 自带 `request_created_at` | session 查询应回到请求/事件时间 |
| [Galileo](https://docs.galileo.ai/concepts/logging/sessions/sessions-overview) | Session 聚合 traces/events/spans | session 用于观察整体流程，细节仍在 span/event |

因此，本项目的正确方向不是“把 session 拆成新主表”，而是：

```text
sessions
  只做 CLI 会话容器和元数据

raw JSONL / session_events
  作为事实源，记录带 timestamp 的 message、tool call、LLM usage、token delta

session_activity_slices
  作为面向日报、周报、MCP、Token 页面的按日物化聚合层
```

### 3.6 本项目采用的分层口径

本方案第一期可以不直接落完整 `session_events` 表，但必须在语义上承认：

1. raw JSONL 是第一期事实源。
2. `session_activity_slices` 是由事实源生成的 daily rollup。
3. MCP 和 Token 接口为了性能读取 rollup，但不能把 rollup 当作唯一原始事实。
4. 后续如需审计、重放、精确模型/任务维度统计，应补 `session_events` 或 `session_observations`。

这能避免把一个业务聚合表设计成平台事实源，后面扩展会更稳。

## 4. 当前实现问题定位

### 4.1 Report MCP get_sessions

当前实现位置：

```text
api/handler/report_mcp_read.go
```

当前核心条件：

```sql
WHERE s.started_at >= $1
  AND s.started_at < ($2::date + 1)
  AND s.user_id = ANY($3)
```

问题：

1. 只能查到开始时间在范围内的 session。
2. 跨天长 session 后续日期查不到。
3. 返回的 `date` 是 `DATE(s.started_at)`，不是活动日期。
4. 返回的 `summary` 是整条 session summary，不是范围内 activity summary。

### 4.2 /tokens/sessions

当前实现位置：

```text
api/handler/token.go
```

当前核心条件：

```sql
WHERE DATE(s.started_at) >= $from
  AND DATE(s.started_at) <= $to
```

问题：

1. `from/to` 实际筛的是 session 开始日期。
2. token 值来自 `token_usage` 的整条 session 总量。
3. 跨天 session 在后续日期查不到。
4. 查询多天范围时，也无法只统计该范围内的 token。

### 4.3 /tokens

当前 `/tokens` 聚合按：

```sql
tu.recorded_at >= $from
AND tu.recorded_at < $to + 1 day
```

问题：

1. `recorded_at` 是入库时间，不是工作发生时间。
2. 如果 session 是今天补传昨天数据，token 会被统计到今天。
3. 如果 `/tokens/sessions` 改用 activity，但 `/tokens` 仍用 `recorded_at`，总数和明细会不一致。

## 5. 数据模型设计

### 5.1 目标数据分层

目标数据模型建议分三层：

| 层级 | 表/来源 | 职责 | 第一期是否必须 |
| --- | --- | --- | --- |
| 会话容器层 | `sessions` | 保存 CLI session 元数据、raw log 地址、整条 session 展示信息 | 是 |
| 事实事件层 | `raw JSONL` / `session_events` | 保存或表示带 timestamp 的 message、tool call、LLM usage、token delta | raw JSONL 必须保留，`session_events` 可后置 |
| 业务聚合层 | `session_activity_slices` | 面向日报/周报/MCP/Token 页面，按业务日期聚合活动内容和 token | 是 |

第一期建议先落 `session_activity_slices`，并把 raw JSONL 作为事实源保留。这样能最快修复日报、MCP、Token 的时间口径。

如果后续要做更强审计、重放、精确 model/task/requirement 归因，再补 `session_events` 表。

### 5.2 session_events 事实层设计位

`session_events` 不是第一期必做项，但需要提前定义边界，避免后续发现 `session_activity_slices` 无法支撑精确追溯。

建议后续事实层结构：

```sql
CREATE TABLE session_events (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id            UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id               BIGINT NOT NULL REFERENCES users(id),
    event_ref             TEXT,
    event_type            TEXT NOT NULL,
    event_time            TIMESTAMPTZ NOT NULL,
    activity_date         DATE NOT NULL,
    timezone              TEXT NOT NULL DEFAULT 'Asia/Shanghai',

    role                  TEXT,
    model                 TEXT,
    input                 TEXT,
    output                TEXT,
    tool_name             TEXT,
    tool_call_id          TEXT,
    metadata_json         JSONB NOT NULL DEFAULT '{}',

    input_tokens          BIGINT NOT NULL DEFAULT 0,
    output_tokens         BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens     BIGINT NOT NULL DEFAULT 0,
    total_tokens          BIGINT NOT NULL DEFAULT 0,

    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_session_events_session_time
    ON session_events(session_id, event_time);

CREATE INDEX idx_session_events_user_date
    ON session_events(user_id, activity_date DESC);
```

`session_events` 的语义更接近 Langfuse observation、LangSmith run、OpenTelemetry span/event。它解决的是精确追溯问题，不是日报查询性能问题。

第一期如果不落这张表，需要满足两个条件：

1. raw JSONL 必须可靠保存，方便未来 backfill。
2. `session_activity_slices` 必须记录 `source_has_raw_log / token_slice_strategy / is_estimated`，明确精度来源。

### 5.3 新增 session_activity_slices 表

建议新增按 session + activity_date 聚合的一日一行切片表。

```sql
CREATE TABLE session_activity_slices (
    session_id             UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id                BIGINT NOT NULL REFERENCES users(id),
    activity_date          DATE NOT NULL,
    activity_start_at      TIMESTAMPTZ NOT NULL,
    activity_end_at        TIMESTAMPTZ NOT NULL,
    timezone               TEXT NOT NULL DEFAULT 'Asia/Shanghai',

    agent_type             TEXT NOT NULL DEFAULT 'claude_code',
    model                  TEXT,
    models                 TEXT[] NOT NULL DEFAULT '{}',

    summary                TEXT,
    excerpt                TEXT,
    message_count          INTEGER NOT NULL DEFAULT 0,
    source_event_count     INTEGER NOT NULL DEFAULT 0,
    tool_calls_json        JSONB NOT NULL DEFAULT '{}',
    git_commits            TEXT[] NOT NULL DEFAULT '{}',

    task_id                UUID REFERENCES tasks(id),
    requirement_id         UUID REFERENCES requirements(id),

    input_tokens           BIGINT NOT NULL DEFAULT 0,
    output_tokens          BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens  BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens      BIGINT NOT NULL DEFAULT 0,
    total_tokens           BIGINT NOT NULL DEFAULT 0,

    source_has_raw_log     BOOLEAN NOT NULL DEFAULT false,
    token_slice_strategy   TEXT NOT NULL DEFAULT 'exact',
    summary_strategy       TEXT NOT NULL DEFAULT 'rule',
    parser_version         TEXT NOT NULL DEFAULT 'v1',
    slice_version          INTEGER NOT NULL DEFAULT 1,
    is_estimated           BOOLEAN NOT NULL DEFAULT false,

    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (session_id, activity_date)
);

CREATE INDEX idx_session_activity_user_date
    ON session_activity_slices(user_id, activity_date DESC);

CREATE INDEX idx_session_activity_date
    ON session_activity_slices(activity_date DESC);

CREATE INDEX idx_session_activity_task_date
    ON session_activity_slices(task_id, activity_date DESC)
    WHERE task_id IS NOT NULL;

CREATE INDEX idx_session_activity_requirement_date
    ON session_activity_slices(requirement_id, activity_date DESC)
    WHERE requirement_id IS NOT NULL;
```

### 5.4 字段说明

| 字段 | 说明 |
| --- | --- |
| `session_id` | 原始 session ID |
| `user_id` | 冗余用户 ID，方便权限过滤和聚合 |
| `activity_date` | 业务日期，按 `Asia/Shanghai` 计算 |
| `activity_start_at` | 该 session 在该日期内第一条活动时间 |
| `activity_end_at` | 该 session 在该日期内最后一条活动时间 |
| `summary` | 该日期内活动摘要 |
| `excerpt` | 该日期内关键原文摘录，受长度限制 |
| `message_count` | 范围内消息/事件数量 |
| `source_event_count` | 生成该 slice 的原始事件数量 |
| `tool_calls_json` | 范围内工具调用统计 |
| `input_tokens` 等 | 范围内 token |
| `source_has_raw_log` | 是否由 raw JSONL 解析得到 |
| `token_slice_strategy` | `exact` / `delta` / `estimated` / `unknown` |
| `summary_strategy` | `rule` / `ai` / `empty` / `unknown` |
| `parser_version` | 生成该 slice 的 parser 版本 |
| `slice_version` | slice 结构或算法版本 |
| `is_estimated` | 是否存在估算 |

### 5.5 为什么第一期使用一日一行

第一期建议使用 session + date 聚合切片，而不是直接落全量事件表。

原因：

1. 日报/周报的核心查询粒度是日期，不是单条事件。
2. MCP 需要的是可控摘要，不应该默认暴露所有 raw event。
3. Token 和来源选择可以通过日切片满足。
4. 性能和数据量更可控。

边界：

1. `session_activity_slices` 是日报/周报聚合层，不是事实源。
2. 多模型、跨任务、精确工具调用链路等问题，不能只靠一日一行永久解决。
3. 后续如需更细粒度追溯，应增加 `session_events` 或 `session_activity_events`。

## 6. 上传与解析方案

### 6.1 daemon 负责解析 activity slices

建议让 daemon 解析 JSONL 并上传 `activity_slices`，后端只负责校验和持久化。

理由：

1. daemon 已经存在 Claude / Codex JSONL 解析逻辑。
2. daemon 更靠近原始本地文件，能更容易处理不同 CLI 的格式。
3. 后端不需要直接理解所有 agent runtime 的 raw log 格式。
4. 上传 payload 可以同时带 session 元数据和切片结果，便于幂等落库。

上传结构建议扩展：

```json
{
  "session_ref": "xxx",
  "started_at": "2026-06-30T10:00:00+08:00",
  "ended_at": "2026-07-02T18:00:00+08:00",
  "summary": "整条 session 摘要",
  "token_usage": {
    "total_tokens": 30000
  },
  "activity_slices": [
    {
      "activity_date": "2026-07-01",
      "activity_start_at": "2026-07-01T09:12:00+08:00",
      "activity_end_at": "2026-07-01T19:40:00+08:00",
      "summary": "当天活动摘要",
      "excerpt": "当天关键内容摘录",
      "message_count": 18,
      "tool_calls": {
        "Bash": 3,
        "Edit": 5
      },
      "input_tokens": 10000,
      "output_tokens": 2000,
      "cache_creation_tokens": 0,
      "cache_read_tokens": 3000,
      "total_tokens": 15000,
      "models": ["claude-sonnet-4-5"],
      "token_slice_strategy": "exact"
    }
  ]
}
```

### 6.2 Claude JSONL 切片规则

Claude session JSONL 通常每条事件有 timestamp，assistant message 内可能有 usage。

建议规则：

1. 事件按 timestamp 归属到 `Asia/Shanghai` 的 `activity_date`。
2. user message、assistant text、tool_use、tool_result 都计入当天 activity。
3. assistant usage 归属到该 assistant message 的 timestamp 所在日期。
4. 当天第一条事件为 `activity_start_at`。
5. 当天最后一条事件为 `activity_end_at`。
6. tool calls 按当天统计。
7. summary / excerpt 只使用当天事件内容生成，不使用整条 session summary。

### 6.3 Codex JSONL 切片规则

Codex token 可能通过累计 `token_count` 暴露，不能把最后一个 total 直接归属到最后一天。

建议规则：

1. 普通事件按 timestamp 归属到 activity_date。
2. `token_count` 事件按累计值计算 delta：

```text
delta = current_total - previous_total
```

3. delta 归属到当前 `token_count` 事件 timestamp 所在日期。
4. 如果 delta < 0，说明计数器重置或格式异常：
   - 当前事件作为新的 baseline；
   - 不计入负数；
   - 当前 slice 标记 `is_estimated=true` 或 `token_slice_strategy=unknown`。
5. 如果缺少中间 token_count，只能按可见增量归属。
6. 如果没有 token_count，但有 message 事件，则 activity 仍应入切片，token 为 0 或 unknown。

### 6.4 没有 raw log 的数据处理

没有 raw JSONL 的历史 session 无法精确切片。

不建议把整条 session token 平摊到每一天，因为这会制造看似准确的错误数据。

建议策略：

1. 有 raw log：解析切片，`source_has_raw_log=true`。
2. 无 raw log 但 session 未跨天：可以生成单日切片，`is_estimated=true`，`token_slice_strategy=estimated`。
3. 无 raw log 且跨天：不生成精确切片，或只生成 legacy 标记切片，不参与日报 MCP 默认来源。

开发期可以接受历史数据不完整，但不能把不准确数据伪装成准确数据。

### 6.5 后端校验边界

虽然 slice 由 daemon 解析生成，但后端不能完全信任上传结果。

后端至少需要校验：

1. `activity_slices` 中同一个 `activity_date` 不能重复。
2. `activity_start_at <= activity_end_at`。
3. `activity_date` 必须等于 `activity_start_at` 按 `Asia/Shanghai` 计算出的日期。
4. slice 时间应落在 session 的合理范围内；允许少量边界容差，但异常值要拒绝或标记 warning。
5. token 字段不能为负数。
6. slice token 合计不能明显超过 session `token_usage.total_tokens`；如果超过，拒绝或标记 `token_slice_strategy=unknown`。
7. `models`、`tool_calls_json`、`summary`、`excerpt` 长度需要限制，避免异常 payload 撑爆数据库或 MCP 上下文。
8. `activity_date` 必须在允许的业务日期范围内，防止错误时间戳写入极远日期。
9. `source_has_raw_log=false` 且跨天的估算 slice 不能默认进入 MCP 报告来源。

建议上传接口返回 per-session warning：

```json
{
  "session_ref": "xxx",
  "status": "updated",
  "warnings": [
    "slice token total differs from session token total",
    "one slice marked estimated"
  ]
}
```

这样 daemon 能继续上传元数据，但平台不会默默污染日报事实。

### 6.6 summary / excerpt 生成边界

第一期不建议在切片阶段引入模型生成摘要，否则会把“数据归属修复”变成“摘要质量工程”。

建议第一期采用规则摘要：

1. 按 activity_date 收集当天 user message、assistant text、tool call 名称、关键错误信息。
2. `summary` 使用当天前若干条用户意图 + 工具调用统计 + 最后一条有效 assistant 结果拼接生成。
3. `excerpt` 保留当天关键原文片段，按长度截断。
4. 不使用整条 session summary 填充当天 summary。
5. 如果当天只有 token_count 或系统事件，没有可读文本，则 summary 标记为“当天有活动，但暂无可读摘要”。
6. 后续可以增加异步 AI 摘要任务，但 AI 摘要必须保留 source slice 和生成时间，不能覆盖事实字段。

建议字段边界：

| 字段 | 来源 | 是否可被 AI 摘要覆盖 |
| --- | --- | --- |
| `excerpt` | raw event 文本截断 | 否 |
| `summary` | 规则摘要或 AI 摘要 | 可以，但要标记生成方式 |
| `message_count` | raw event 计数 | 否 |
| `tool_calls_json` | raw event 统计 | 否 |
| `token_*` | LLM usage / token delta | 否 |

## 7. 后端落库方案

### 7.1 上传事务

`POST /sessions/batch` 保存 session 时，需要处理数据库和 MinIO 的一致性。

严格来说，MinIO 对象上传不能放进数据库事务里回滚，所以不应表述为“同一事务完成 raw log 保存和 DB 提交”。推荐流程：

```text
1. 解析 multipart metadata 和 raw file。
2. 先 upsert sessions、删除旧 slices、插入新 slices，在 DB 事务中完成。
3. DB 事务提交后，再上传 raw log 到 MinIO。
4. MinIO 上传成功后，更新 sessions.raw_log_url。
5. 如果 MinIO 上传失败，保留 session 和 slices，但返回 warning，并保持 raw_log_url 为空或旧值。
```

也可以反过来先上传 MinIO，再提交 DB，但必须有补偿清理：

```text
1. 先上传 raw log 到临时 object key。
2. DB 事务提交成功后，把 object 标记为正式 key 或写入 raw_log_url。
3. DB 失败时删除临时 object。
```

第一期建议采用“DB 先提交、MinIO 失败 warning”的简单策略，因为日报/MCP 依赖的是 slices，不直接依赖 raw log 实时可用。

DB 事务内部应完成：

1. upsert `sessions`。
2. 校验 slices。
3. 删除该 `session_id` 下旧的 `session_activity_slices`。
4. 插入本次上传的新切片。
5. 可选：重建 `token_usage` 作为整条 session 总量缓存。
6. 提交事务。

幂等要求：

```text
同一个 user_id + session_ref 重复上传
-> session_id 不变
-> 旧 slices 被替换
-> token 不重复累加
```

### 7.2 token_usage 的处理

当前 `token_usage` 是 session 总量表，且 `recorded_at` 默认当前时间。

建议开发期直接明确：

1. `/tokens` 不再依赖 `token_usage.recorded_at`。
2. `/tokens/sessions` 不再依赖 `token_usage` 的时间。
3. `token_usage` 可以暂时保留为 session 总量缓存，用于非日期场景或兼容内部代码。
4. 日期范围统计一律从 `session_activity_slices` 聚合。

如果后续要彻底简化，可以删除 `token_usage` 或把它改成由 slices 汇总生成的物化缓存。

### 7.3 session 表保留字段

`sessions.started_at / ended_at / uploaded_at` 仍有价值：

| 字段 | 用途 |
| --- | --- |
| `started_at` | 整条 session 开始时间展示 |
| `ended_at` | 整条 session 结束时间展示 |
| `uploaded_at` | 同步到平台的时间 |
| `raw_log_url` | 原始日志下载与 backfill |

但这些字段不再用于日报/周报/MCP/Token 的业务日期筛选。

## 8. MCP 改造方案

### 8.1 get_sessions 查询语义

旧语义：

```text
返回 started_at 在 date_range 内的 session
```

新语义：

```text
返回 activity_date 在 date_range 内的 session 活动切片
```

查询基础：

```sql
FROM session_activity_slices sas
JOIN sessions s ON s.id = sas.session_id
JOIN users u ON u.id = sas.user_id
WHERE sas.activity_date >= $1::date
  AND sas.activity_date <= $2::date
  AND sas.user_id = ANY($3)
```

### 8.2 get_sessions 返回结构

不保留旧版本兼容，建议直接改返回结构：

```json
{
  "sessions": [
    {
      "id": "session uuid",
      "session_ref": "xxx",
      "user_id": "303",
      "username": "张三",
      "role": "employee",
      "team_id": "team uuid",
      "agent_type": "codex",

      "started_at": "2026-06-30T10:00:00+08:00",
      "ended_at": "2026-07-02T18:00:00+08:00",

      "activity_start_at": "2026-07-02T09:20:00+08:00",
      "activity_end_at": "2026-07-02T18:00:00+08:00",
      "activity_dates": ["2026-07-02"],

      "summary": "仅 date_range 内活动摘要",
      "excerpt": "仅 date_range 内关键内容摘录",
      "message_count": 12,
      "tool_calls": {
        "Bash": 3,
        "Edit": 2
      },
      "task_refs": [],
      "requirement_refs": [],
      "input_tokens": 1000,
      "output_tokens": 200,
      "cache_creation_tokens": 0,
      "cache_read_tokens": 300,
      "total_tokens": 1500,
      "source_has_raw_log": true,
      "token_slice_strategy": "exact",
      "is_estimated": false,
      "truncated": false,
      "slice_count": 1,
      "slices": [
        {
          "activity_date": "2026-07-02",
          "activity_start_at": "2026-07-02T09:20:00+08:00",
          "activity_end_at": "2026-07-02T18:00:00+08:00",
          "summary": "当天活动摘要",
          "source_has_raw_log": true,
          "token_slice_strategy": "exact",
          "is_estimated": false
        }
      ]
    }
  ],
  "summary": {
    "session_count": 1,
    "slice_count": 1,
    "by_date": [
      { "date": "2026-07-02", "count": 1 }
    ],
    "truncated": false
  }
}
```

### 8.3 聚合方式

当 date_range 覆盖多天时，MCP 可以按 session 聚合返回：

1. 一条 session 对象包含多个 slices。
2. token 为范围内 slices 的合计。
3. `activity_start_at` 为范围内最早活动时间。
4. `activity_end_at` 为范围内最晚活动时间。
5. `summary` 为范围内 slice summary 拼接或合并摘要。

也可以在 `include_detail=true` 时返回完整 slices，默认只返回聚合 summary。

### 8.4 截断策略

MCP 面向 Agent，必须控制上下文大小。

建议：

1. 默认 `limit=100`，最大 `limit=200`。
2. 单个 slice `excerpt` 限制长度，例如 1000 字。
3. 单个 session 聚合 summary 限制长度，例如 1500 字。
4. `include_detail=false` 时不返回完整 excerpt，只返回 summary。
5. 返回 `truncated=true` 让 Agent 知道数据被截断。
6. 返回 `source_has_raw_log / token_slice_strategy / is_estimated`，让 Agent 知道是否可以把该 slice 当作强事实。

### 8.5 Agent 使用规则

Skill 中应明确：

1. 优先使用 `is_estimated=false` 且 `source_has_raw_log=true` 的 slices。
2. `is_estimated=true` 的 slices 可以作为线索，但报告中不能写成确定事实。
3. `truncated=true` 时只能基于已返回内容总结，不能假设未返回内容。
4. `activity_dates` 外的 session 元数据只能用于定位，不得作为报告事实。
5. 如果 date_range 内无 activity slices，应明确说明“当前范围内未找到已上传活动记录”，不要根据 session started_at 补写。

### 8.6 权限模型

权限仍沿用现有 Report MCP scope 规则：

1. employee：只看自己。
2. team_leader：看小组成员。
3. pm：按当前产品定义的团队/项目范围。
4. director/admin：看允许范围内全局或部门数据。

权限过滤必须基于 `session_activity_slices.user_id`，不要只依赖 join 后的 session。

## 9. Token 接口改造方案

### 9.1 /tokens/sessions

旧语义：

```text
列出 started_at 在 from/to 内的 session，并展示整条 session token。
```

新语义：

```text
列出 activity_date 在 from/to 内有活动的 session，并展示范围内 token。
```

查询建议：

```sql
SELECT
    s.id,
    s.session_ref,
    sas.user_id,
    user_name,
    s.agent_type,
    ARRAY_AGG(DISTINCT m) AS models,
    MIN(sas.activity_start_at) AS activity_start_at,
    MAX(sas.activity_end_at) AS activity_end_at,
    ARRAY_AGG(DISTINCT sas.activity_date ORDER BY sas.activity_date) AS activity_dates,
    SUM(sas.input_tokens) AS input_tokens,
    SUM(sas.output_tokens) AS output_tokens,
    SUM(sas.cache_creation_tokens) AS cache_creation_tokens,
    SUM(sas.cache_read_tokens) AS cache_read_tokens,
    SUM(sas.total_tokens) AS total_tokens
FROM session_activity_slices sas
JOIN sessions s ON s.id = sas.session_id
JOIN users u ON u.id = sas.user_id
WHERE sas.activity_date >= $from::date
  AND sas.activity_date <= $to::date
  AND <scope>
GROUP BY s.id, sas.user_id, user_name, s.agent_type
ORDER BY MAX(sas.activity_end_at) DESC, s.id DESC
LIMIT $limit OFFSET $offset;
```

返回结构建议增加：

```json
{
  "activity_start_at": "...",
  "activity_end_at": "...",
  "activity_dates": ["2026-07-01", "2026-07-02"],
  "slice_count": 2
}
```

`started_at` 可以保留展示整条 session 开始时间，但不能再让用户误解为筛选依据。

### 9.2 /tokens

建议同步改为从 `session_activity_slices` 聚合。

日期条件：

```sql
WHERE sas.activity_date >= $from::date
  AND sas.activity_date <= $to::date
```

分组：

| group_by | 数据来源 |
| --- | --- |
| team | `sas.user_id -> users.team_id` |
| user | `sas.user_id` |
| requirement | `sas.requirement_id` |
| task | `sas.task_id` |
| model | `sas.model` 或后续更细的 model slice |

Series：

```sql
SELECT activity_date, SUM(total_tokens)
FROM session_activity_slices
WHERE activity_date BETWEEN $from AND $to
GROUP BY activity_date
ORDER BY activity_date;
```

### 9.3 model 统计风险

如果一个 session 当天使用多个 model，而 slice 只有一行，`group_by=model` 会不够精确。

第一期可以：

1. 使用主 model 作为 `model`。
2. `models` 用于展示多模型。
3. 文档标注 model 维度是主模型口径。

如果后续要求精确 model 维度，需要把 slice 再细分为 `session_activity_model_slices`。

## 10. 报告生成链路影响

### 10.1 Agent + Skill

Skill 中 `get_sessions` 的含义变更为活动范围内 session 内容。

这会直接修复：

1. personal_daily 查不到跨天 session 的问题。
2. personal_weekly 混用整条 session 的问题。
3. team_daily/team_weekly 统计成员活动时漏 session 的问题。
4. department 周报间接依赖下级数据时的来源不完整问题。

### 10.2 报告页面

日报/周报页面后续如果恢复“选择 session 生成草稿”，候选 session 应来自 activity slices。

选择列表应展示：

1. 活动日期。
2. 范围内活动时间。
3. 范围内 summary。
4. 范围内 token。

不要再展示成“session 开始时间就是日报日期”。

## 11. 迁移与 Backfill

### 11.1 开发环境迁移

开发期不考虑兼容，可以直接：

1. 新增表。
2. 修改上传接口。
3. 修改 daemon 上传 payload。
4. 修改 MCP 和 token 查询。
5. 更新测试。

### 11.2 历史数据处理

历史数据分三类：

1. 有 raw log：可以写 backfill 脚本解析生成 slices。
2. 无 raw log，未跨天：可以生成单日估算切片。
3. 无 raw log，跨天：不做精确切片，避免污染日报数据。

建议 backfill 输出报告：

```text
total_sessions
with_raw_log
backfilled_exact
single_day_estimated
cross_day_skipped
failed
```

### 11.3 是否允许估算数据进入 MCP

建议默认：

1. `is_estimated=false` 的 slice 可以进入 MCP 默认结果。
2. `is_estimated=true` 的 slice 可以进入 token 统计，但 MCP 返回时标注。
3. 跨天无 raw log 的估算数据不进入日报生成默认来源。

## 12. 风险清单

### 12.1 JSONL 格式差异风险

Claude、Codex、后续其他 agent 的 JSONL 结构不完全一致。

风险：

1. timestamp 字段路径不同。
2. usage 字段路径不同。
3. tool call 表示方式不同。
4. 子任务/subagent 文件分布不同。

应对：

1. parser 按 agent_type 分离。
2. 每种 parser 独立单测。
3. 无法识别 usage 时，activity 仍入库，token 标记 unknown。
4. 解析失败不能影响 session 元数据上传，但要在结果中返回 warning。

### 12.2 token delta 计算风险

Codex token 可能是累计值，必须做 delta。

风险：

1. token_count 丢失中间事件。
2. 累计值重置。
3. 多模型 token 混在同一个累计值里。

应对：

1. delta < 0 时重置 baseline。
2. 记录 `token_slice_strategy=delta`。
3. model 维度第一期按主模型口径。
4. 单测覆盖累计增长、重置、跨天三类场景。

### 12.3 summary 串天风险

如果继续使用整条 session summary，日报仍会混入非当天内容。

应对：

1. slice summary 必须由当天事件生成。
2. MCP 默认返回 slice summary。
3. 整条 session summary 只作为 session metadata，不作为日报事实。

### 12.4 时区风险

如果 activity_date 依赖数据库默认时区，UTC 和中国日期会错位。

应对：

1. daemon 和后端统一使用 `Asia/Shanghai`。
2. activity_date 入库时已固定为业务日期。
3. SQL 查询只比较 date，不再对 timestamptz 做隐式 `DATE()`。
4. 单测覆盖 `2026-07-02T00:30:00+08:00`。

### 12.5 重复上传风险

daemon 可能定时重复上传同一 session。

风险：

1. slices 重复。
2. token 翻倍。
3. token_usage 和 slices 不一致。

应对：

1. `PRIMARY KEY(session_id, activity_date)`。
2. 上传时先删除该 session 旧 slices，再插入新 slices。
3. token_usage 从 slices 汇总重建。
4. 整个流程事务化。

### 12.6 权限风险

slice 表冗余 `user_id` 后，权限过滤必须以 slice 的 user_id 为准。

风险：

1. team scope 查到非本团队成员。
2. director/admin 范围误收敛。
3. session join 异常导致权限绕过。

应对：

1. 所有查询先按 `sas.user_id` scope。
2. MCP、tokens/sessions、tokens 聚合共用 scope helper。
3. 权限单测覆盖 employee、TL、PM、director、admin。

### 12.7 上下文过大风险

周报可能覆盖大量 session slices。

风险：

1. MCP 返回过大。
2. Agent 上下文超限。
3. 报告生成成本升高。

应对：

1. 默认返回 summary，不返回完整 raw。
2. `limit` 最大 200。
3. excerpt 截断。
4. 返回 `truncated`。
5. 周报优先使用日报/任务/需求，再补充 session summary。

### 12.8 任务/需求关联粗粒度风险

当前 task_id / requirement_id 在 session 级别。

风险：

一个跨天 session 可能包含多个任务，但所有切片都继承同一个 task_id。

应对：

1. 第一期继承 session 级别关联。
2. 文档标注这是已知限制。
3. 后续支持 slice 级别任务识别。

### 12.9 历史数据准确性风险

历史 session 未必都有 raw log。

风险：

1. backfill 不完整。
2. 产品验收时旧数据看起来缺失。

应对：

1. 明确历史无 raw log 不保证精确。
2. backfill 报告输出 skipped 数。
3. 验收使用新上传的带 raw log 数据。

### 12.10 缺少事实事件层的风险

第一期如果只落 `session_activity_slices`，没有 `session_events`，会存在追溯能力不足的问题。

风险：

1. 无法从数据库直接还原某天 slice 由哪些事件组成。
2. summary 质量问题难以定位。
3. model/task/requirement 细粒度归因无法精确补齐。
4. 后续审计和重放仍依赖 raw JSONL。

应对：

1. 第一阶段必须可靠保存 raw JSONL。
2. slice 记录 `source_has_raw_log / token_slice_strategy / is_estimated`。
3. 文档明确 slice 是 rollup，不是事实源。
4. 第二阶段规划 `session_events` backfill。

### 12.11 物化聚合层漂移风险

`session_activity_slices` 是由 raw JSONL 解析生成的物化结果，可能与 raw log 或 token_usage 缓存不一致。

风险：

1. daemon parser 升级后，新旧 slice 口径不同。
2. 手动修复 task/requirement 后，slice 仍保留旧关联。
3. token_usage 总量与 slice 合计不一致。

应对：

1. parser 增加版本字段，例如 `parser_version` 或 `slice_version`。
2. session 关联任务/需求变化时，同步更新 slice 上的 `task_id / requirement_id`，或触发重建。
3. 定期校验 session 总 token 与 slice 合计差异。
4. 重要 parser 变更后提供重建脚本。

### 12.12 MinIO 与数据库一致性风险

raw log 文件在 MinIO，slice 在数据库，二者不能用同一个数据库事务保证原子性。

风险：

1. DB 提交成功但 raw log 上传失败。
2. raw log 上传成功但 DB 提交失败，产生孤儿对象。
3. raw_log_url 指向不存在对象。

应对：

1. 第一阶段采用 DB 优先，MinIO 失败返回 warning。
2. raw_log_url 只在 MinIO 上传成功后写入。
3. 增加定期巡检：raw_log_url 存在但对象不可下载时报警。
4. 如改为 MinIO 先上传，必须使用临时 object key 和失败清理。

### 12.13 多维统计精度风险

一日一行 slice 对日报足够，但对 model、task、requirement 的精确统计有天然限制。

风险：

1. 同一天同一 session 使用多个模型，`group_by=model` 只能近似。
2. 同一天同一 session 处理多个任务，task 统计只能继承 session 级关联。
3. Token 页面如果要求精确到模型或任务，会和 slice 粒度冲突。

应对：

1. 第一期明确 model/task/requirement 是 slice 主归因口径。
2. UI 文案避免暗示绝对精确。
3. 后续按需要增加 `session_activity_model_slices` 或 `session_events`。
4. 对日报生成来说，优先保证日期范围内容正确，细粒度归因可后置。

## 13. 测试方案

### 13.1 MCP get_sessions

用例 1：跨天 session 第二天有活动。

```text
session started_at = 2026-07-01 18:00
slice 2026-07-01 token=1000
slice 2026-07-02 token=2000
```

查询：

```json
{
  "date_range": {
    "start": "2026-07-02",
    "end": "2026-07-02"
  }
}
```

预期：

1. 返回该 session。
2. `activity_dates=["2026-07-02"]`。
3. `total_tokens=2000`。
4. summary 不包含 2026-07-01 内容。

用例 2：跨天 session 第二天无活动。

预期：

1. 查询 2026-07-02 不返回该 session。

用例 3：周范围查询。

预期：

1. 返回范围内所有 slices。
2. token 为范围内合计。
3. 不返回范围外 activity_date。

### 13.2 /tokens/sessions

构造同一 session 两天切片：

| activity_date | total_tokens |
| --- | ---: |
| 2026-07-01 | 1000 |
| 2026-07-02 | 2000 |

请求：

```text
GET /api/v1/tokens/sessions?from=2026-07-02&to=2026-07-02
```

预期：

1. 返回该 session。
2. `total_tokens=2000`。
3. `activity_dates=["2026-07-02"]`。
4. 不因为 `sessions.started_at=2026-07-01` 被排除。

### 13.3 /tokens 聚合

请求：

```text
GET /api/v1/tokens?period=range&from=2026-07-02&to=2026-07-02&group_by=user
```

预期：

1. 总 token 与 `/tokens/sessions` 当前范围内明细求和一致。
2. series 中 2026-07-02 的值等于当日 slices 合计。

### 13.4 幂等上传

同一 session 上传两次。

预期：

1. `sessions` 只有一条。
2. `session_activity_slices` 每个 `activity_date` 只有一条。
3. token 总量不翻倍。

### 13.5 时区测试

事件：

```text
2026-07-02T00:30:00+08:00
```

预期：

1. `activity_date=2026-07-02`。
2. 不被算到 2026-07-01。

### 13.6 后端校验测试

构造异常 payload：

1. slice token 为负数。
2. `activity_start_at > activity_end_at`。
3. `activity_date` 与 `activity_start_at` 的 `Asia/Shanghai` 日期不一致。
4. 同一个 session 上传重复 `activity_date`。
5. slice token 合计明显超过 session total token。

预期：

1. 非法数据被拒绝或返回 warning。
2. 不产生脏 slice。
3. session 元数据和 raw log 状态清晰可判断。

### 13.7 MinIO 失败测试

模拟 raw log 上传失败。

预期：

1. session metadata 和 slices 可以保存。
2. `raw_log_url` 不写入不存在对象。
3. 上传结果返回 warning。
4. MCP 不依赖 raw log 下载即可读取 slices。

### 13.8 物化层重建测试

同一 raw JSONL 使用新 parser 版本重建。

预期：

1. 旧 slices 被替换。
2. `slice_version/parser_version` 更新。
3. token 不重复累加。
4. `/tokens` 与 `/tokens/sessions` 仍一致。

### 13.9 多模型边界测试

同一天同一 session 使用两个模型。

预期：

1. `models` 展示多模型。
2. `model` 主归因口径明确。
3. `group_by=model` 的统计结果符合第一期定义。
4. 文档或接口字段不暗示模型维度绝对精确。

## 14. 实施步骤

### Phase 1：数据模型与上传

1. 新增 `session_activity_slices` 表。
2. 扩展 `SessionUpload`，支持 `activity_slices`。
3. daemon 解析 Claude / Codex JSONL 生成 slices。
4. 后端校验 slices，删除旧 slices 并插入新 slices。
5. 处理 MinIO 与 DB 的最终一致策略。
6. 增加上传幂等、校验、MinIO warning 单测。

### Phase 2：查询语义切换

1. MCP `get_sessions` 改为查 `session_activity_slices`。
2. `/tokens/sessions` 改为查 `session_activity_slices`。
3. `/tokens` 改为查 `session_activity_slices`。
4. 移除 started_at 作为业务日期筛选的逻辑。
5. 更新相关测试。

### Phase 3：回填与验证

1. 对有 raw log 的历史 session 执行 backfill。
2. 输出 backfill 报告。
3. 用跨天 session 验证 MCP 和 token 页面。
4. 用 Report Agent 生成日报，确认不会混入范围外内容。
5. 对 parser 版本变化提供 slice 重建脚本。

### Phase 4：报告入口增强

该阶段不属于本方案第一目标，但依赖切片能力：

1. 日报/周报页面恢复“生成草稿”入口。
2. 选择 session 时展示 activity summary。
3. 选择 Agent 时只展示报告类 Agent。
4. 默认走系统报告 Agent。

## 15. 验收标准

1. MCP `get_sessions` 不再按 `sessions.started_at` 过滤。
2. MCP 返回的 summary/excerpt 只包含 date_range 内活动。
3. `/tokens/sessions` 不再按 `DATE(s.started_at)` 过滤。
4. `/tokens` 不再按 `token_usage.recorded_at` 表示业务日期。
5. 跨天 session 在有活动的日期能查到，在无活动的日期查不到。
6. 当日 token 明细合计与当日 token 聚合一致。
7. 重复上传不会造成切片或 token 翻倍。
8. 业务日期按 `Asia/Shanghai` 落库和查询。
9. 历史无 raw log 跨天 session 不伪造成精确切片。
10. 后端会校验 slice payload，不会盲信 daemon。
11. raw JSONL 是事实源，`session_activity_slices` 是 rollup，这个边界在代码和文档中保持一致。
12. MinIO 上传失败不会产生错误的 `raw_log_url`，并会返回 warning。
13. MCP 返回 `source_has_raw_log / token_slice_strategy / is_estimated / truncated` 等可信度字段。
14. 多模型、多任务等第一期非精确维度有明确口径，不在 UI/API 中暗示绝对精确。

## 16. 推荐结论

这次问题不建议只用 SQL 的 started_at/ended_at 交集修补。

交集查询只能解决“查不到跨天 session”，但不能解决“返回内容串天”和“token 串天”。

更合理、也更接近主流 observability 平台的修复是：

1. 保持 `sessions` 作为逻辑容器。
2. 保留 raw JSONL 作为第一期事实源，后续可演进为 `session_events`。
3. 建立 `session_activity_slices` 作为日报、周报、MCP、Token 页面的 daily rollup。
4. daemon 上传时按 JSONL 事件时间生成切片。
5. 后端校验并幂等持久化切片。
6. MCP、`/tokens/sessions`、`/tokens` 全部改用 activity_date。
7. 开发期直接替换旧语义，不做兼容分支。

这样日报/周报、Agent Skill、Token 页面才能共享同一套准确的时间口径。
