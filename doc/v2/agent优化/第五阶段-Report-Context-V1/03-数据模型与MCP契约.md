# V1 数据模型与 MCP 契约

## 1. 一张新表

```sql
report_run_contexts
-------------------
run_id                 primary key / foreign key
schema_version         text not null
source_selection_id    uuid null
    context_hash           text not null
    context_payload        jsonb not null
    context_bytes          bigint not null
created_at             timestamptz not null
```

开发前根据现有 Report Run 和 selection 表确认真实字段类型与外键，不在设计阶段指定迁移序号。

一条 Run 只保存一条 Context。它不复制原始 Session/JSONL，但会保存 selection 已冻结、已裁剪到保护上限内的完整 Digest，保证 Agent 读取与本次 Run 严格一致。

## 2. Context payload

```json
{
  "schema_version": "report-context/v1",
  "run": {
    "run_id": "...",
    "report_type": "personal_daily",
    "period": {"start": "2026-07-18", "end": "2026-07-18"},
    "target": {"type": "self"}
  },
  "source_state": {
    "mode": "digest_v2",
    "coverage_complete": true
  },
  "sources": {
    "session_digest": {"content_mode": "digest_v2", "coverage": {"complete": true}}
  }
}
```

说明：

- `sources.session_digest` 原样保留冻结 Digest 的完整 JSON，不做第二次 Top-K 或事实筛选；
- 完整性由现有 selection 读取校验和 Context 非空/模式校验共同保证；
- `context_hash/context_bytes` 保存在表和 Run 元数据，不塞入给 Agent 的事实正文；
- hash 对最终 payload 字节计算，不包含构建时间。

## 3. `get_report_context`

请求只包含：

```json
{"run_id":"..."}
```

服务端必须：

- 校验当前认证用户和 Run 归属；
- 校验 Run 类型和归属；
- 从 `report_run_contexts` 返回当前 Run 的 Context；
- 不接受 report_type、period、target、selection 或 scope 覆盖参数；
- 不允许 run_id 代替认证凭据。

响应为 `report-context/v1`。

## 4. 写回

继续复用：

```text
write_report_result(run_id, content, summary?)
write_report_failure(run_id, error_message)
```

服务端继续校验授权、Run 状态、幂等和写冲突，不增加正文内容审查。

## 5. Context 大小

- `context_bytes` 用于监控和产品成本提示；
- 不通过 Top-K 或重要性排序静默缩小；
- 原始 JSONL 不进入 Context；
- selection/Digest 已有 1 MiB 保护上限继续生效；
- Context V1 不新增可运维配置项或第二套大小限制。

## 6. V1 不建设

- `get_report_evidence`；
- Context 对象存储；
- Context TTL 和归档系统；
- confidence 评分；
- 来源版本平台；
- Fact/Evidence 子表；
- 定制 Agent MCP 接入。
