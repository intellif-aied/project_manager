# V1 数据模型与 MCP 契约

## 1. 一张新表

```sql
report_run_contexts
-------------------
run_id                 primary key / foreign key
schema_version         text not null
source_selection_id    uuid null
digest_version         text null
context_hash           text not null
context_payload        jsonb not null
context_bytes          bigint not null default 0
created_at             timestamptz not null
```

开发前根据现有 Report Run 和 selection 表确认真实字段类型与外键，不在设计阶段指定迁移序号。

一条 Run 只保存一条规范化 Context，不复制原始 Session、Digest 或完整业务对象。

## 2. Context payload

```json
{
  "schema_version": "report-context/v1",
  "run": {
    "run_id": "...",
    "report_type": "personal_daily",
    "period": {"date": "2026-07-18"},
    "target": {"type": "self"}
  },
  "calendar": {},
  "identity": {},
  "source_state": {
    "mode": "sessions_only",
    "coverage_complete": true
  },
  "facts": [
    {
      "fact_id": "...",
      "work_object": "Report Context V1",
      "statement": "完成方案设计",
      "status_hint": "completed",
      "source_type": "session_digest",
      "source_ref": "...",
      "source_updated_at": "..."
    }
  ],
  "coverage": {
    "complete": true,
    "source_count": 1,
    "represented_count": 1
  },
  "context_hash": "...",
  "context_bytes": 12345
}
```

说明：

- `facts` 保存低损事实，不是日报段落；
- `status_hint` 是来源状态，不是服务端对最终完成度的裁决；
- `source_ref` 用于后台追溯，V1 不提供 Agent 回查接口；
- 多个相互冲突的事实同时保留；
- hash 在字段规范化后计算，不包含构建时间等易变字段。

## 3. `get_report_context`

请求只包含：

```json
{"run_id":"..."}
```

服务端必须：

- 校验当前认证用户和 Run 归属；
- 校验 Run 状态；
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
- 若超过技术硬上限，Context 构建失败并更新 Run，不启动 Agent；
- 提醒阈值和技术硬上限必须分开配置和表达。

## 6. V1 不建设

- `get_report_evidence`；
- Context 对象存储；
- Context TTL 和归档系统；
- confidence 评分；
- 来源版本平台；
- Fact/Evidence 子表；
- 定制 Agent MCP 接入。
