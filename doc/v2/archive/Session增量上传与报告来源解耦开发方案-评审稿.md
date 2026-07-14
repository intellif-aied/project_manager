# Session 增量上传与报告来源解耦开发方案

## 1. 文档状态

- 状态：开发前方案
- 优先级：P0，高于 Token 成本分析页面开发
- 适用范围：Aida CLI、Aida API、Session 管理页、个人日报/周报的 Session 选择、Aida Report MCP
- 不修改：`sandboxed-agent-platform` 源码

本文解决 Session 上传、Token 统计、报告来源选择被同一个“按天切片”概念混用的问题，并补充 CLI 分页、Session 自助删除以及 macOS 下 Session summary 不可见的问题。

本文确定的新契约实施后，以下旧结论不再作为新实现依据：

- `aida upload` 每次上传完整 Session 文件并整体替换服务端切片；
- 报告来源使用 `session_id:YYYY-MM-DD` 的按日切片键；
- Codex 计数异常时把完整 Session Token 归集到最后活动日。

`doc/v2/Codex Token切片与最近活动会话方案.md` 保留为历史问题和 legacy consumer 下线记录，其中“最近活动排序”和 legacy consumer 下线结论仍有效；其整包重传、整表替换和最后活动日归集方案由本文替代。

## 2. 当前代码核对结论

### 2.1 CLI 当前上传的是完整 Session

`daemon/device_client.go` 当前行为：

- `aida sessions` 扫描全部 Claude Code/Codex 日志后一次性输出，没有分页；
- `aida upload` 的交互选择一次性输出全部 Session；
- 每次上传都会重新解析完整文件，并将完整原始日志作为 multipart 文件上传；
- metadata 同时包含 Session 总 Token 和 `activity_slices`；
- `--all` 表示上传所有本地 Session，不区分“未上传”和“本次有新增内容”。

这意味着一个持续数天的 Session 每次上传都会重复传输和重新计算全部历史内容。

### 2.2 服务端当前整体替换按日切片

`api/handler/session.go` 的 `replaceActivitySlices` 当前先执行：

```sql
DELETE FROM session_activity_slices WHERE session_id = $1
```

再插入本次 payload 中的全部切片。只要新一次解析遗漏、异常或采用了不同估算策略，已经保存的历史日期就可能减少、消失或被挪到其他日期。

### 2.3 报告选择被绑定到 Token 的按日切片

当前个人报告使用：

```text
selected_session_slice_keys = ["session_id:YYYY-MM-DD"]
```

前端、默认 Report Skill、managed Agent 参数和 Report MCP 都依赖该字段。它把“用于 Token 日统计的 activity slice”错误地同时当作“用户选择的报告证据”。

实际产品要求是：

- 个人日报、个人周报可以自由选择跨天、跨报告周期的一个或多个 Session 内容范围；
- 小组、部门报告默认汇总下级已保存的个人/小组报告，不应默认读取每个人的原始 Session；
- `get_sessions` 仍保留为自定义 Skill 可按需取 Session 的能力入口。

### 2.4 Session 删除已有入口，但语义不完整

当前页面已有“撤回”按钮，API 已有 `DELETE /sessions/{id}`，会删除 Session 及级联数据。但本地原始日志仍在，下次执行 `aida upload --all` 会再次创建同一 Session。

因此当前只能完成一次数据库删除，不能满足“用户明确删除后，不被自动重新上传”的完整预期。

### 2.5 macOS summary 不可见存在两类可能原因

当前尚无 macOS 失败样本，不能直接认定单一根因。代码中已经确认存在两个风险点：

1. Claude summary 截断使用字节下标 `summary[:197]`，可能截断中文或 emoji 的 UTF-8 字节，生成无效字符串；
2. CLI 使用宽度约 156 字符的固定单行表格，summary 位于最后一列。macOS 常见的窄终端会把最后一列换行或挤到可视区域之外。

还需排查第三种情况：macOS 上的 Claude/Codex 日志 schema、内容类型或目录结构不同，导致解析阶段就没有提取出 summary。

因此修复必须先区分：

- `summary` 已解析，但终端未正确显示；
- `summary` 在解析结果中为空；
- `summary` 含控制字符、换行或无效 UTF-8，显示被破坏。

## 3. 核心设计结论

“切片”必须拆成四种不同对象，不再共用一个结构和主键。

| 对象 | 含义 | 主要用途 | 是否按天 |
| --- | --- | --- | --- |
| Session | 一次持续会话的稳定身份 | 列表、搜索、权限、任务关联 | 否 |
| Upload Chunk | 从上次成功游标之后新增的日志范围 | 增量上传、幂等、原始证据 | 否 |
| Daily Usage | 从已接收 Chunk 派生的业务自然日用量 | Token、成本、趋势 | 是 |
| Report Source | 用户选中的 Session 内容范围快照 | 个人日报/周报生成 | 否，可跨天 |

关键约束：

1. 上传边界由源日志游标决定，不由自然日决定。
2. Token 按天统计是服务端派生结果，不是上传协议的身份。
3. 报告来源是用户选择的内容范围，不复用 Token 日切片键。
4. 普通增量上传只能增加新 Chunk，不能因为本次 payload 未携带旧数据而删除历史数据。
5. 删除只能由用户显式操作触发，不能由上传缺项、解析警告或重试触发。

## 4. 目标与非目标

### 4.1 目标

1. 长期 Session 第二天继续使用并上传时，只上传新增内容。
2. 重试、超时和重复上传不重复计算 Token，不重复保存报告证据。
3. 已上传的历史日期数据不会因后续增量上传减少或迁移。
4. 个人报告可以选择跨天、多个 Session 或同一 Session 的连续内容范围。
5. CLI Session 列表和上传选择支持分页。
6. 用户可以删除自己的 Session，普通 `upload --all` 不会立即把它重新上传。
7. macOS、Linux 对中文、emoji、多行 summary 的解析和显示一致。
8. 不改变小组/部门报告以已保存下级报告为主的数据路径。

### 4.2 非目标

- 本文不设计模型价格、汇率和人民币成本页面；
- 本文不让小组/部门系统默认 Skill 批量读取所有成员原始 Session；
- 本文不删除用户本机的 Claude Code/Codex 原始日志；
- 本文不修改 managed Agent 平台的 Skill、MCP 或任务执行实现；
- 本文不保证从缺失时间戳或无法识别的累计计数中推断出精确逐日 Token；
- 本文不保留旧 `selected_session_slice_keys` 的长期双协议兼容。

## 5. 数据模型

### 5.1 `sessions`

继续保存一个用户的一次会话身份，稳定业务键为：

```text
(user_id, agent_type, session_ref)
```

`sessions.id` 仍是内部关联 ID，不在页面展示为 Session ID。页面和搜索继续使用 `session_ref`。

Session 级字段只保存可由 Chunk 汇总或最后状态得到的数据，例如：

- `started_at`
- `last_active_at`
- `summary`
- `models`
- `total_tokens`
- `last_chunk_at`

Session 总数值必须从已接收 Chunk 派生，不能直接信任客户端每次上传的完整 Session 总量覆盖历史值。

### 5.2 `session_upload_chunks`

新增不可变增量块表，建议字段：

```text
id
session_id
source_key
source_generation
start_cursor
end_cursor
event_start_at
event_end_at
content_sha256
parser_name
parser_version
metrics_quality
raw_object_key
accepted_at
```

约束：

- `content_sha256` 在同一 Session 内唯一，用于重试幂等；
- `(session_id, source_key, source_generation, start_cursor, end_cursor)` 唯一；
- Chunk 一经接收不可修改；
- `source_key` 是本地源文件的不可逆标识，不上传绝对路径；
- 文件被截断、重写或 inode/文件身份发生变化时增加 `source_generation`，不得把新文件错误接在旧游标后。

游标优先使用字节偏移，同时记录结束行号用于诊断。JSONL Chunk 必须从完整行边界开始并在完整行边界结束。

### 5.3 `session_chunk_activity_slices`

每个 Chunk 可以跨越多个自然日。解析器按 `Asia/Shanghai` 将该 Chunk 内的事件派生成一到多个日贡献：

```text
chunk_id
activity_date
activity_start_at
activity_end_at
model_usage
summary
excerpt
message_count
source_event_count
token_slice_strategy
is_estimated
```

它表示“这个 Chunk 对某一天的贡献”，不是用户可选择的报告来源身份。

### 5.4 `session_activity_slices`

保留现有表作为 Session 日聚合结果，以降低 Token、任务、需求和 MCP 查询的改造范围。它由同一 Session、同一日期的全部已接收 Chunk 贡献聚合而来。

普通上传只重算本次 Chunk 影响到的日期，禁止先删除 Session 全部日期再重建。

聚合不变量：

```text
session_activity_slices(date)
= SUM(session_chunk_activity_slices(date))
```

摘要和 excerpt 不做字符串简单拼接。按 Chunk 时间顺序选取有效内容并执行长度上限，保留来源 Chunk ID 以便追溯。

### 5.5 `session_report_sources`

新增报告来源快照表，保存一次报告运行真正使用的 Session 内容范围：

```text
report_run_id
session_id (nullable, ON DELETE SET NULL)
session_ref_snapshot
range_start_at
range_end_at
start_chunk_id
end_chunk_id
source_revision_hash
selected_order
deleted_at
```

报告生成开始时固定来源快照。后续继续上传同一 Session，不得静默改变已经开始或已经完成的报告运行证据。

### 5.6 `session_upload_tombstones`

用户删除平台 Session 后，内容、Token、派生切片和原始对象应删除，同时保留最小墓碑：

```text
user_id
agent_type
session_ref
deleted_at
```

墓碑只用于阻止 `aida upload --all` 自动恢复已明确删除的 Session，不保留日志内容、summary 或 Token。

用户显式执行“恢复并重新上传”后删除墓碑，从本地源日志重新构建该 Session。

## 6. CLI 方案

### 6.1 本地上传状态

新增本地状态文件，例如：

```text
~/.aida/upload-state.json
```

按本地源文件记录：

- `session_ref`
- `source_key`
- `source_generation`
- `last_acked_cursor`
- `last_acked_chunk_hash`
- 解析累计值所需的最小基线状态
- 最近成功上传时间

要求：

- 文件权限 `0600`；
- 临时文件写入后原子替换；
- 只有服务端确认 Chunk 已接收或已幂等存在后才推进游标；
- 本地状态只是性能优化，服务端唯一键仍是最终幂等依据；
- 状态文件丢失时可以从头分块扫描并依靠服务端 hash 去重，不能重复计数。

### 6.2 增量切块

默认 Chunk 上限同时受两项限制：

- 完整 JSONL 事件条数；
- 压缩后字节数。

达到任一上限即结束当前 Chunk。Chunk 可以跨天，不应在午夜强制切断。

首次使用新版 CLI 时从游标 0 开始分块上传历史文件。之后只读取 `last_acked_cursor` 之后新增的完整行。

如果文件末尾存在尚未写完的 JSON 行，本次跳过该行，下次扫描补传，不能把半行记录为已确认游标。

### 6.3 `aida sessions` 分页

新增：

```bash
aida sessions --page 1 --page-size 20
aida sessions --json
```

规则：

- 默认每页 20 条；
- `page-size` 可选 10、20、50、100，最大 100；
- 按 `LastActiveAt` 倒序；
- 展示总数、当前页、总页数；
- `--json` 不做终端截断，用于区分解析问题和渲染问题。

### 6.4 `aida upload` 分页选择

交互选择使用一次扫描得到的固定快照，避免翻页时序号变化：

```text
[n] 下一页  [p] 上一页  [g] 跳页  [a] 选择全部待上传
[1,3,5] 切换选择  [enter] 确认  [q] 退出
```

要求：

- 使用全局序号，例如第二页继续显示 21 到 40；
- 已选择项跨页保留；
- 确认前显示已选数量和 Session ID/summary；
- `--all` 改为上传所有“存在待上传 Chunk”的 Session，不重复上传无变化的完整日志；
- 保留直接输入数字的现有习惯，但数字仅对本次固定扫描快照有效；
- 非交互自动化优先支持 `--session <session_ref>`，避免依赖可能变化的序号。

### 6.5 上传结果

每个 Session 返回以下状态之一：

- `uploaded`：上传了一个或多个新 Chunk；
- `unchanged`：没有新增完整事件；
- `duplicate`：服务端已接收相同 Chunk；
- `deleted`：服务端存在删除墓碑，普通上传已跳过；
- `failed`：上传失败且本地游标未推进。

超时重试必须使用相同 Chunk hash。客户端不能因为没收到响应就生成不同 Chunk 身份。

### 6.6 删除与恢复

平台页面继续提供用户本人 Session 删除。CLI 补充：

```bash
aida sessions delete --session <session_ref>
aida upload --restore --session <session_ref>
```

删除确认必须明确：

- 删除的是 Aida 平台副本、Token 派生数据和平台原始日志对象；
- 不删除本机 Claude Code/Codex 文件；
- 普通 `upload --all` 不会自动恢复；
- 用户可以显式恢复并重新上传。

服务端只能删除当前用户自己的 Session。小组长、总监不通过该入口删除成员 Session；管理员清理另设管理操作，不复用用户自助删除权限。

## 7. macOS summary 修复方案

### 7.1 先建立可诊断性

`aida sessions --json` 至少返回：

```json
{
  "session_ref": "...",
  "agent_type": "codex",
  "summary": "...",
  "summary_source": "first_user_text",
  "summary_status": "ok"
}
```

`summary_status` 可取：

- `ok`
- `empty_source`
- `unsupported_content`
- `invalid_utf8_repaired`

这样可以先确认 summary 是否已被解析，而不是只看终端表格猜测。

### 7.2 统一 summary 提取

Claude Code 和 Codex 使用同一套最终清洗函数：

1. 从第一条有意义的用户文本提取；
2. 忽略仅包含工具结果、空白、系统注入或纯结构化元数据的内容；
3. 将 `\r\n`、`\r` 统一为 `\n`，列表展示时折叠为空格；
4. 清除 ANSI 转义序列和不可见控制字符；
5. 修复或替换无效 UTF-8；
6. 全部截断按 rune 执行，禁止 `summary[:N]` 字节截断；
7. 持久化摘要和显示摘要使用明确的不同长度上限。

没有可用用户文本时，不伪造工作内容。CLI 显示“暂无摘要”，同时保留 `summary_status`；页面可回退展示项目名和 Session ID，但不得把该回退值保存成真实 summary。

### 7.3 自适应终端布局

固定 156 字符单行表格改为按终端宽度选择布局：

- 宽终端：保留单行列布局；
- 窄终端：第一行显示序号、Agent、最近活动、Token、Session ID，第二行缩进显示项目和 summary；
- 非 TTY 输出：使用稳定、无 ANSI 的文本格式；
- `--json` 永远输出完整字段，不受终端宽度影响。

不能仅通过缩短 summary 解决，因为固定前置列本身已经可能超过 macOS 终端宽度。

### 7.4 macOS 验证

必须加入脱敏后的真实 macOS Claude Code/Codex JSONL fixture，覆盖：

- Intel 和 Apple Silicon 不同路径下的扫描；
- 中文、英文、emoji、组合字符；
- 多行首条用户消息；
- ANSI 和控制字符；
- 第一条 user 不是纯文本、后续才有有效文本；
- 80、100、120、160 列终端快照；
- `--json` 有 summary、窄终端也可见 summary；
- 输出始终是有效 UTF-8。

仅做 Darwin 交叉编译不能证明终端显示正确，至少需要一台真实 macOS 做安装包 E2E。

## 8. 上传 API 契约

新增增量接口，不继续扩展旧整包替换语义：

```text
POST /api/v1/session-chunks/batch
```

请求示例：

```json
{
  "client_version": "...",
  "parser_version": "...",
  "sessions": [
    {
      "session_ref": "...",
      "agent_type": "codex",
      "chunks": [
        {
          "source_key": "sha256:...",
          "source_generation": 1,
          "start_cursor": 1048576,
          "end_cursor": 1310720,
          "content_sha256": "...",
          "event_start_at": "...",
          "event_end_at": "...",
          "activity_contributions": []
        }
      ]
    }
  ]
}
```

原始 Chunk 内容使用流式 multipart 或压缩 body 上传，不能把大文件全部读入内存后再发出。

响应逐 Chunk 返回：

```json
{
  "status": "accepted|duplicate|deleted|rejected",
  "content_sha256": "...",
  "acked_cursor": 1310720,
  "error_code": null
}
```

服务端处理一个 Chunk 时必须在同一事务中完成：

1. 校验用户、Session 和游标；
2. 幂等插入 Chunk；
3. 保存 Chunk 的日贡献；
4. 重算受影响日期聚合；
5. 重算 Session 汇总；
6. 提交后才返回 `accepted`。

若游标有缺口，返回 `CURSOR_GAP` 并给出服务端期望游标；不能接受后一个 Chunk 后静默遗漏中间内容。

## 9. Token 与每日统计边界

Token 统计仍按业务自然日展示，但依据是已接收 Chunk 中带时间戳的用量贡献。

### 9.1 长 Session 的正确行为

示例：

1. 7 月 13 日上传 Session A 的 Chunk 1；
2. 7 月 14 日继续使用 Session A；
3. 再次上传时只上传 Chunk 2；
4. 7 月 13 日数据保持不变；
5. 7 月 14 日增加 Chunk 2 的对应贡献。

如果 7 月 14 日追加的日志事件时间戳属于 7 月 13 日，则允许 7 月 13 日数据单调增加，但不得减少、清空或迁移到 7 月 14 日。

### 9.2 Codex 累计值回退

增量 Chunk 只包含新增日志事件，因此旧事件不会在每次完整重扫时被重复累加。解析器仍需识别累计值重置或上下文切换：

- 每个来源 generation 保存最小计数基线；
- 发现回退后结束当前计数段，新建计数段；
- 无法精确归属的仅标记受影响 Chunk/日期为 `is_estimated=true`；
- 禁止把完整 Session 总量移动到最后活动日；
- 禁止为了修复当前 Chunk 删除以前已确认 Chunk 的日贡献。

模型明细、计价和汇率继续由 `Token成本统计与价格管理方案.md` 定义。该文档实施时必须改为消费 Chunk 派生的 Daily Usage，而不是客户端整包 Session 总量。

## 10. 报告来源新契约

### 10.1 替换字段

个人报告将：

```text
selected_session_slice_keys
```

替换为：

```json
{
  "selected_session_sources": [
    {
      "session_ref": "019f...",
      "range_start_at": "2026-07-12T09:30:00+08:00",
      "range_end_at": "2026-07-14T18:20:00+08:00",
      "source_revision_hash": "sha256:..."
    }
  ]
}
```

`range_start_at`、`range_end_at` 可以跨自然日。选择完整 Session 时可省略范围，但后端仍在启动报告运行时解析并保存具体 Chunk 快照。

前端不得向用户展示数据库 `sessions.id`。请求使用 `session_ref` 和服务端返回的 opaque selection token/revision，后端再次校验所有权。

### 10.2 个人报告 UI

个人日报、个人周报的 Session 设置弹窗：

- 一行展示一个 Session，不按日把同一 Session 重复成多行；
- 展示 summary、项目、最近活动、活动时间范围；
- 支持分页、关键词和真实 Session ID 搜索；
- 展开 Session 后可以选择完整 Session 或连续活动范围；
- 允许跨报告日期选择；
- 明确显示已选 Session 数和时间范围。

Token 日切片只可作为时间轴提示，不作为最终选择键。

### 10.3 MCP

`get_sessions` 增加 `selected_session_sources`，并在有显式选择时按固定来源快照读取。返回内容必须包含：

- `session_ref`
- `range_start_at`、`range_end_at`
- summary/excerpt/必要的压缩内容
- `source_revision_hash`
- 数据是否完整、是否已删除

`get_sessions` 仍支持按 scope 和日期范围供自定义 Skill 查询，但系统默认 Skill 的规则是：

- 个人日报/周报：显式选择优先；未选择时使用报告周期内的个人来源；
- 小组日报/周报：以成员已保存个人报告为主，不默认遍历成员原始 Session；
- 部门日报/周报：以已保存小组报告和必要的个人报告为主，不默认遍历部门原始 Session。

### 10.4 删除后的报告

`session_ref_snapshot` 和范围字段是报告运行的审计快照，不随 Session 删除而丢失；原始内容仍按删除要求清除。

Session 删除后：

- 已生成并保存的报告正文保留；
- 报告来源记录显示“来源 Session 已删除”，不再提供原始内容；
- 未开始的待运行报告如果依赖已删除来源，应在启动前返回明确错误并要求重新选择；
- 已运行任务不得因为后续上传同一 Session 而换用新内容。

## 11. 删除流程

当前 `DELETE /sessions/{id}` 改造为“数据库删除事务 + 对象存储清理任务”。数据库和 MinIO 无法组成一个真实原子事务，不能把二者描述成一次提交：

1. 校验 Session 所有者；
2. 在一个数据库事务中写最小 tombstone、将报告来源的 `session_id` 置空并标记已删除、删除 Chunk/日贡献/日聚合/Token/成本派生/任务需求关联，同时写入对象清理 outbox；
3. 提交数据库事务后，Session 立即不再可查询或下载；
4. 后台清理任务删除 MinIO 中完整日志或 Chunk 对象；
5. 对象存储删除失败时重试并记录告警，管理端可以看到待清理状态；
6. 已保存报告正文和最小来源快照保留，但原始来源内容不再可用。

恢复上传必须由用户显式触发，删除 tombstone 后从本地游标 0 重新分块上传。恢复创建新的 Session 内部记录和新的来源 revision，不复用已删除报告的旧来源身份。

## 12. 迁移和发布方案

本次是协议级改造，不做长期双写。

### 阶段 1：数据层与 API

1. 新增 Chunk、Chunk 日贡献、报告来源和 tombstone 表；
2. 实现增量上传事务和幂等测试；
3. 保留现有 `session_activity_slices` 对查询端的结构；
4. 旧 `/sessions/batch` 暂时可用，但一旦某 Session 已进入 Chunk 模式，旧接口必须拒绝覆盖该 Session，并返回 `CLI_UPGRADE_REQUIRED`。

### 阶段 2：CLI

1. 发布支持游标、分块、分页、summary 诊断和删除/恢复的 CLI；
2. 首次运行从完整本地文件按多个 Chunk 导入，不上传一个完整大包；
3. 服务端记录满足率后，设置最低 CLI 版本；
4. 旧 CLI 收到清晰升级提示，不能继续整体替换新数据。

### 阶段 3：报告来源

1. 前端改为 Session/时间范围选择；
2. managed Agent 参数改为 `selected_session_sources`；
3. Report MCP 和默认 Skill 同步改为新字段；
4. 个人日报、周报做跨天来源 E2E；
5. 小组、部门报告验证不会默认读取所有成员 Session。

该阶段需要 Aida API、Web、默认 Skill 同步发布，但不需要修改 managed Agent 平台源码。

### 阶段 4：清理旧协议

1. 停止接受旧整包上传；
2. 删除 `replaceActivitySlices` 的整 Session 删除再插入路径；
3. 删除 `selected_session_slice_keys` 前后端及 Skill/MCP 契约；
4. 旧测试数据不做兼容转换；已保存报告正文保留。

## 13. 测试方案

### 13.1 CLI 单元测试

1. 从游标 0 将大 JSONL 切成多个完整行 Chunk；
2. 第二次扫描无变化，结果为 `unchanged`；
3. 追加事件后只生成一个或少量新 Chunk；
4. 末尾半行不推进游标；
5. 超时重试产生相同 hash；
6. 本地状态文件原子写入且权限为 `0600`；
7. 文件截断或重写产生新的 generation；
8. 500 个 Session 分页正确，跨页选择不丢失；
9. `--all` 只上传有新增内容的 Session；
10. 删除后的 Session 普通上传显示 `deleted`，显式 restore 后可重新上传。

### 13.2 API 单元与集成测试

1. 同一 Chunk 重传只保存一次；
2. 游标缺口返回 `CURSOR_GAP`；
3. 一个 Chunk 跨两天时生成两条日贡献；
4. 新 Chunk 只重算受影响日期；
5. payload 缺少旧日期不会删除旧日期；
6. 任一事务步骤失败时 Chunk、日贡献和 Session 汇总全部回滚；
7. 非所有者不能删除或恢复 Session；
8. 删除级联清理 Token、成本和原始对象；
9. tombstone 阻止普通重传；
10. 旧整包接口不能覆盖已经进入 Chunk 模式的 Session。

### 13.3 长 Session E2E

固定构造一个跨三天 Session：

1. 第一天上传，记录 Session、日统计和报告来源；
2. 第二天追加内容并上传，证明第一天数值不减少；
3. 第三天再次追加，证明只新增第三天贡献；
4. 重传第二天 Chunk，证明 Token 不增加；
5. 追加一个时间戳属于第一天的迟到事件，证明只使第一天单调增加；
6. 模拟 Codex 累计值回退，证明旧 Chunk 不被重复计算，异常只标记受影响贡献；
7. 验证 Session 列表始终只有一行，详情可看到三天 Daily Usage。

### 13.4 报告 E2E

1. 个人日报选择跨两天的一个 Session 连续范围；
2. 个人周报选择多个 Session，其中包含报告周期外的显式来源；
3. MCP 返回所有显式来源，默认 Skill 正文实际使用其工作内容；
4. 报告运行后继续上传同一 Session，已完成报告来源 revision 不变；
5. 删除来源 Session 后，已保存报告正文仍可查看且来源标记已删除；
6. 小组报告以成员个人报告为主，不批量读取原始 Session；
7. 部门报告以小组报告为主，不批量读取部门原始 Session；
8. 自定义 Skill 仍可通过 `get_sessions` 在授权范围内查询 Session。

### 13.5 macOS E2E

1. 在真实 macOS 安装最新 Aida CLI；
2. 扫描真实脱敏 Claude Code 和 Codex 日志；
3. `aida sessions --json` 的 summary 非空且 UTF-8 合法；
4. 80 列终端中 summary 在第二行可见；
5. 中文、emoji、多行内容显示正常；
6. 分页选择并上传一个 Session；
7. 服务端 summary 与 CLI JSON 输出一致；
8. 删除后执行 `upload --all` 不会恢复，显式 restore 可恢复。

## 14. 验收标准

全部满足后才认为方案完成：

1. 第二次上传持续 Session 时，网络请求不再携带完整历史 JSONL；
2. 重试同一 Chunk 不改变 Session Token 和日报来源内容；
3. 后续上传不能删除或降低既有日期的统计；
4. 一个跨天 Session 在主列表只显示一行；
5. Daily Usage 可按天统计，但个人报告可以选择跨天内容范围；
6. 小组、部门报告默认不读取所有成员原始 Session；
7. CLI 500 条 Session 可分页浏览和跨页选择；
8. 删除后的平台 Session 不会被普通 `--all` 自动恢复；
9. macOS 真实终端和 `--json` 都能看到合法、可读的 summary；
10. Linux 现有 Claude/Codex 扫描与上传功能无回归；
11. 现有 Token、任务、需求查询继续通过 `session_activity_slices` 得到一致结果；
12. 不依赖修改 `sandboxed-agent-platform`。

## 15. 实施顺序建议

1. 先修 summary UTF-8 和终端布局，并加入 macOS fixture，改动独立、可快速验证；
2. 实现 Chunk 表、增量 API、幂等和事务；
3. 实现 CLI 游标、切块、分页和上传状态；
4. 实现删除 tombstone 与显式恢复；
5. 将 Token 日聚合切换为 Chunk 派生；
6. 最后改个人报告来源契约、前端、MCP 和默认 Skill；
7. 完成三天长 Session、跨天个人报告及小组/部门报告回归后再发布。

不能先只改前端选择字段，也不能只改 CLI 上传而继续让服务端整表替换。上传、聚合和报告来源必须按上述阶段形成闭环。
