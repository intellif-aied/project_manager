# Session 与 Token 查询性能专项设计方案

> 方案类型：V2 架构与开发方案；状态：开发前 Review 已通过，可进入 R1，尚未授权开发；适用范围：Report Source、Session 内容存储、Token Analytics。
> 核心原则：原始内容只保留一份，在线查询只读取面向业务的读模型。

## 1. 要解决的三个问题

### 1.1 Report Source 分页是假分页

接口虽然接收 `page`、`page_size`，但候选集合、活动时间、摘要和总数可能先从事件明细计算，最后才分页。事件越多，第一页也越慢。

### 1.2 原始内容重复存储

MinIO 已保存原始 JSONL，PostgreSQL `session_content_events.content_payload` 又保存清洗后的完整事件 JSON。PostgreSQL 应承担在线关系查询，不应继续作为第二份原始对象仓库。

### 1.3 Token Analytics 聚合成本继续增长

Token 页面需要同一查询快照保证 summary、trends、rankings、sessions 一致，但快照不应反复复制和聚合高基数明细。它需要独立的 Usage 分析事实，不能复用 Report Source 切片目录。

## 2. 目标架构

```text
                       共享身份与版本契约
        session / upload chunk / slice / generation / revision
                                   │
Aida 增量上传 ──> MinIO 原始 JSONL（唯一字节级事实源）
                                   │
                    ┌──────────────┴──────────────┐
                    │                             │
             内容投影/元数据索引             Usage 规范化
                    │                             │
       ┌────────────┴────────────┐                │
       │                         │                │
Report Source 切片目录     MinIO 内容读取器    Token 分析事实
       │                         │                │
候选筛选/排序/分页        Digest/冻结内容       轻量查询快照
```

权威边界：

- MinIO：原始 JSONL、分块对象和字节级恢复；
- PostgreSQL：切片身份、游标、哈希、时间、摘要、状态、Usage 与计费事实；
- Report Source 目录：只回答“哪些切片可选”；
- Token 分析事实：只回答“指定范围使用了多少 Token、成本是多少”；
- Digest：读取冻结切片内容，不参与候选列表分页。

## 3. Report Source 目标方案

新增切片目录读模型，建议命名 `report_source_slice_catalog`。一行代表一个成功上传并可追踪的切片。

建议字段：

| 类别 | 字段 |
| --- | --- |
| 主键与归属 | `slice_id`、`user_id`、`session_id`、`session_ref`、`agent_type` |
| 来源版本 | `source_id`、`generation_id`、`content_projection_revision_id`、`content_epoch` |
| 内容边界 | `start_cursor`、`end_cursor`、`event_count` |
| 列表字段 | `activity_start_at`、`activity_end_at`、`last_activity_at`、`summary`、`cwd`、`models` |
| 可用性 | `status`、`ready_at`、`created_at`、`updated_at` |

目录不保存 `total_tokens`，也不读取 Usage 计算 Token。

基础索引：

```sql
(user_id, status, activity_end_at DESC, session_ref, slice_id)
(user_id, status, activity_start_at, activity_end_at)
```

关键词搜索先在目录规模内执行。只有实际数据证明模糊搜索仍超标时，才增加 trigram 索引；禁止为搜索回退到事件表。

候选接口只允许：

- 用户/权限过滤；
- 显式活动日期相交过滤；
- 关键词过滤；
- 排序、精确总数和分页。

候选接口禁止：

- 扫描 `session_content_events`；
- 扫描 Usage/Token 明细；
- 现场生成摘要或 Digest；
- 推进 projection、Digest 或修复任务状态。

## 4. 日期与分页契约

请求中的日期分成两类：

| 参数 | 含义 | 是否过滤候选 |
| --- | --- | --- |
| `period_start`、`period_end` | 当前日报/周报周期上下文 | 否 |
| `activity_from`、`activity_to` | 用户显式选择的活动日期范围 | 是 |

无论是否传显式活动日期，SQL 都只查询切片目录。日期可以改变结果数量，但不能把查询路径切回事件表。

分页验收的准确表述是：

> 返回第 1 页、第 N 页或总数的成本，可以随候选切片数量增长，但不得随原始事件数量、JSONL 字节数或是否传报告周期日期增长。

## 5. 内容存储目标方案

### 5.1 新增轻量事件索引

建议新增 `session_content_event_index`，只保留：

- revision/chunk 与游标范围；
- `occurred_at`、`event_type`；
- 有长度上限的 `summary`、`excerpt`；
- `content_sha256`；
- MinIO object key、对象哈希/版本等恢复引用。

不保存完整 `content_payload`。

### 5.2 建立统一内容读取器

Digest、冻结来源读取和 Session 内容详情不得继续自行查询 JSONB。统一通过 Content Event Reader：

1. 根据 slice/revision/cursor 找到相关上传分块；
2. 从 MinIO 流式读取对应 JSONL；
3. 按游标截取事件并校验哈希；
4. 产出与当前消费者兼容的事件结构；
5. 对对象缺失、哈希错误、游标断裂返回明确错误，禁止静默降级为空内容。

### 5.3 为什么新增索引表而不是批量置空

对现有 7GB 表直接置空 `content_payload`：

- 会产生大规模 WAL、锁竞争和磁盘临时峰值；
- 普通 UPDATE 后也不会立即归还磁盘；
- 会同时破坏当前 Digest 读取；
- 回滚困难。

安全路径是新增轻量表、双写/回填、切换消费者、完成观察期后再整体下线旧载荷表。

## 6. Token Analytics 目标方案

Token Analytics 保留 `query_snapshot_token`，但不能继续把“逻辑事件最终值”直接当作 Chunk 增量。目标以不可变 `session_usage_contributions` 为唯一可加总事实：

```text
Observation 推进产生的 Token Delta
  -> 归属 member Session + root Session family + Chunk + activity_date
  -> Session 家族总量 Rollup
  -> Session 家族按天 Rollup
  -> 新增 Chunk Rollup
```

顶层 Session 家族总量包含自身及全部层级 Subagent。复制到 Subagent 的父历史只建立累计基线，不生成新的 Contribution。三种维度必须在同一 active revision set 与 family relation version 下满足：

```text
Session 家族总量
  = self + all subagents
  = 所有业务日之和
  = 所有新增 Chunk 之和
  = 所有 active Contribution 之和
```

现有 `session_usage_components` 保留逻辑事件当前最终值，不能直接承担 Chunk Delta；现有 `session_daily_usage` 在迁移期继续兼容，目标改为从 Contribution 重建。Token 和成本事实仍在上传后的异步 Usage/Metering 流程固定，查询不重新解析 JSONL 或重新计价。

轻量快照只冻结：

- active revision set、family relation version 和数据高水位；
- 权限范围；
- 查询条件；
- 成本版本及 Rollup 版本引用。

summary、trends、rankings、sessions 继续读取同一快照。默认 Session 列表按 root Session 展示一行，成员/Subagent 仅下钻展示并标记已包含在家族总量中，避免页面重复求和。详细定义见 [Token 三维统计与对账模型](./07-Token三维统计与对账模型.md)。

## 7. 发布拆分

| 发布单元 | 内容 | 明确不包含 |
| --- | --- | --- |
| R1 | Report Source 切片目录、回填、影子对账、灰度读取 | MinIO 读取切换、历史清理、Token 重构 |
| R2 | 轻量事件索引、统一 MinIO 内容读取器、消费者影子校验 | 停写旧载荷、删除历史数据 |
| R3 | 新写入不再保存 PostgreSQL 完整载荷 | 历史载荷删除 |
| R4 | 观察期后下线旧载荷表并回收空间 | Token Analytics |
| R5A | Usage Contribution、Session family、三维 Rollup 影子建设 | Token API 切换 |
| R5B | 轻量 Snapshot、Token API/前端/MCP ad-hoc 分别灰度 | 旧路径删除 |
| R5C | 兼容 Snapshot/字段/表的独立下线评审 | Report Source/内容存储改造 |

每个发布单元都必须有独立开关和旧路径回退能力。

## 8. 成功标准

- Report Source p95 不超过 300ms、p99 不超过 800ms；
- 无日期与显式活动日期请求使用相同的目录查询路径；
- 执行计划不出现内容事件或 Usage 明细表；
- 新写入不再增加 PostgreSQL 完整 JSON 载荷；
- Digest、冻结来源、Session 内容读取与当前结果一致；
- Digest、MCP 和其他接口逐项通过 [接口回归矩阵](./06-Digest与接口影响回归矩阵.md)；
- Token 家族总量、逐日和新增 Chunk 三维精确对账，Subagent 父历史不重复；
- Token 页面各模块保持同一快照，统计、成本、权限和质量状态一致；
- 任一新路径异常时，可以通过配置开关回旧读路径，不需要回滚数据迁移。

详细步骤与测试见 [开发、迁移与测试验收](./05-开发迁移与测试验收.md)。
