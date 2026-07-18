# 06：Digest 与接口影响回归矩阵

> 发布门禁：本矩阵是内容存储迁移和 PG JSONB 清理的硬门禁。Digest、MCP full、Session 内容详情和恢复链路任一项未通过，禁止停止新增 `content_payload`，更禁止清理历史 Payload。Token 查询性能通过不能替代本矩阵。生产发布必须同时满足 [完整发布清单](./09-完整发布清单.md)。

> 目标：明确内容存储和 Token 读模型调整后，哪些接口只是内部换数据源，哪些链路必须逐项回归。任何一项未通过，都不能清理 PostgreSQL 完整 Payload。

## 1. Digest 当前发生时机

```text
上传 Chunk 被接受
  -> 内容 projection 按 cursor 完成
  -> 切片达到 fully indexed
  -> Reconciler 周期扫描（当前约 5 秒）
  -> 创建每个 slice/version 的 Digest Job
  -> Digest Worker 异步生成 session_slice_digest_revisions
  -> 用户选择来源时组合并冻结已就绪 Digest
  -> Agent 通过 MCP 读取 selection_digest_payload
```

Digest 不是用户点击“AI 生成”后才开始生成。用户选择来源时只允许组合和冻结已就绪 Digest；未就绪继续返回 `REPORT_SOURCE_DIGEST_NOT_READY`，不得临时同步扫描完整 JSONL。

## 2. 方案对 Digest 的唯一实质变化

| 环节 | 当前 | 目标 |
| --- | --- | --- |
| 触发条件 | slice fully indexed | 不变 |
| 调度方式 | Reconciler + 异步 Worker | 不变 |
| 原始读取 | PostgreSQL `content_payload` | MinIO Content Event Reader |
| Digest revision | `session_slice_digest_revisions` | 不变 |
| 版本/脱敏/hash | 版本化并校验 | 不变 |
| 选择冻结 | 保存选择级冻结 Payload | 不变 |
| MCP 读取 | 读取冻结 Payload | 不变 |

MinIO 故障只允许使尚未生成的 Digest 延迟或失败，不能生成空 Digest。已经冻结的 Digest 保存在 PostgreSQL，MCP 读取不依赖实时 MinIO。

## 3. MCP 的三条 Session 读取路径

### 3.1 Attached Digest 模式

个人日报/周报 Agent 携带 `run_id + report_source_selection_id` 调用 `get_sessions`：

- `digest_v1/v2` 直接读取 PostgreSQL 中冻结的 selection Payload；
- 校验 selection、run、period、digest version、hash 和完整覆盖；
- 读取完成后记录 `read_completed_mode`；
- `write_report_result` 必须再次验证读取完整性。

该模式不应因 MinIO Reader 上线增加在线延迟，也不改变 MCP Schema。

### 3.2 Attached full 兼容模式

当前 full 模式会分页读取 `session_content_events.content_payload`。它是 R2 必须迁移的消费者：

- 外部 MCP 参数、分页 cursor 和返回事件结构保持不变；
- 内部改为按冻结 revision/cursor 从 MinIO 流式读取；
- full 模式尚未在测试服完成全量对账前，禁止停止新增 PostgreSQL 完整 Payload，更禁止清理旧 Payload。

### 3.3 Ad-hoc 日期查询模式

未附加 selection 的 `get_sessions` 在 R5B 前读取 `session_activity_slices`，R5B 改读
active family + family daily Rollup，仍按日期/权限返回。它不读取 `content_payload`：

- 本专项不删除该路径的 Token 字段；
- Report Source 选择器不再显示 Token，不代表 MCP ad-hoc `get_sessions` 可以删除 Token；
- 后续 Token 三维统计切换时，该路径必须纳入测试服全量对账，并与 Token API/前端同版本切换。

其余报告、任务、需求等 MCP 工具不读取 Session 完整事件，本专项不改变其工具定义和返回结构。

## 4. 接口影响总表

| 模块/接口 | 内部变化 | 外部契约 | 发布单元 |
| --- | --- | --- | --- |
| Chunk 上传 | 异步附加 catalog/index/Contribution 任务 | 接收语义和响应不变 | R1/R2/R5A |
| `/report-source-sessions` | 改读 slice catalog | 参数、字段、日期和分页语义不变 | R1 |
| 创建来源选择 | 继续绑定 slice/revision/Digest | 不变 | R1 回归 |
| Digest Worker | 从 MinIO Reader 读取 | 无外部接口变化 | R2 |
| MCP attached digest | 继续读冻结 Payload | Schema、错误码、完整性门禁不变 | R2 回归 |
| MCP attached full | PG Payload 改为 MinIO Reader | 分页和事件结构不变 | R2 |
| MCP ad-hoc sessions | 改读 active family + daily Rollup | 日期、权限和 Token 字段保持；`token_slice_strategy=family_rollup_v2` | R5B |
| MCP `write_report_result` | 无数据源变化 | 读取完成、版本、hash 门禁不变 | 全阶段回归 |
| Session 内容详情/导出 | 改为 MinIO Reader | 返回内容和权限不变 | R2 |
| 内容清理/恢复 | 同步处理对象、索引、Digest、Rollup | 授权和审计语义不变 | R2/R5A |
| Token Analytics | 改读三维 Rollup + 轻量 Snapshot | 权限、筛选和 snapshot token 保持；字段语义显式，API/前端/MCP 同版本切换 | R5B |
| `/tokens` | 从停止更新的 `session_activity_slices` 切到同一 Rollup Snapshot | 原字段兼容，数值与 Token Analytics 对账 | R5B |
| `/tokens/sessions` | 从日期切片旧表切到不重复 root Session Rollup | 分页真正作用于 Session Rollup；`total` 不再是切片行数；后续页复用首页 `query_snapshot_token` | R5B |
| 工作台 Token 卡片 | 删除 `fetchAllSessionTokens` 全量翻页和浏览器汇总 | 改读 summary/trends/rankings/`session_count` | R5B |
| 需求/任务关联工作记录 | `/tokens/sessions` 从日期切片变为 root Session | family v2 行以 root Session ID 保存；旧切片关联仍可解除 | R5B |
| Snapshot/Rollup 回收 | 从请求内批量删除改为有超时的小批后台任务 | 无用户接口变化；未过期 Snapshot 结果稳定 | R5B |
| 其他报告/任务/需求 MCP | 无变化 | 无变化 | 冒烟回归 |

## 5. 必须逐项执行的回归用例

### 上传与 Digest

| 编号 | 场景 | 通过标准 |
| --- | --- | --- |
| REG-UP-001 | 新 Chunk 上传 | 上传响应不等待 projection、Digest 或 Rollup |
| REG-DIG-001 | fully indexed slice | 周期任务只创建一个目标版本 Digest Job |
| REG-DIG-002 | Digest PostgreSQL/MinIO 离线对账 | 事件顺序、source hash、数量和最终 digest hash 一致 |
| REG-DIG-003 | Digest 重试 | 不产生重复 ready revision |
| REG-DIG-004 | MinIO 对象缺失/hash 错误 | 明确失败，不生成空 Digest |
| REG-DIG-005 | Digest 版本升级 | 新旧 revision 并存，旧冻结报告仍可读 |
| REG-DIG-006 | Digest 尚未就绪即选择 | 仍返回 NOT_READY，不同步生成 |

### Report Source

| 编号 | 场景 | 通过标准 |
| --- | --- | --- |
| REG-RS-001 | period-only 候选 | period 不过滤，结果与当前契约一致 |
| REG-RS-002 | 显式 activity 范围 | 只过滤相交切片 |
| REG-RS-003 | 创建显式选择 | slice/revision/cursor 精确冻结 |
| REG-RS-004 | 默认来源选择 | 按报告周期选择并冻结完整切片 |
| REG-RS-005 | Session 后续新增 Chunk | 已冻结选择不漂移 |
| REG-RS-006 | 内容被清理 | 返回 CONTENT_CLEARED，不读取残留派生数据 |

### MCP

| 编号 | 场景 | 通过标准 |
| --- | --- | --- |
| REG-MCP-001 | 9 个工具 Schema | 工具名、参数、描述兼容 |
| REG-MCP-002 | attached digest_v2 | 返回冻结字节，完整覆盖且无分页 |
| REG-MCP-003 | attached digest_v1 | 版本、hash、覆盖校验不变 |
| REG-MCP-004 | attached full 多页 | MinIO Reader 分页无重复、无遗漏，cursor 可重试 |
| REG-MCP-005 | selection/run/period 不匹配 | 错误码保持 REPORT_SOURCE_MISMATCH |
| REG-MCP-006 | 未完整读取即写报告 | `write_report_result` 拒绝并返回 INCOMPLETE |
| REG-MCP-007 | 完整读取后写报告 | 正常保存且重复写幂等 |
| REG-MCP-008 | ad-hoc 日期查询 | 权限、日期、Session、Token 字段保持 |
| REG-MCP-009 | ad-hoc 显式选择 Subagent/root | 命中正确 family，Token 不重复，日期范围外显式选择仍生效 |
| REG-MCP-010 | 任务/需求/日报/周报等其他工具 | 响应与基线一致 |

### 其他 API

| 编号 | 场景 | 通过标准 |
| --- | --- | --- |
| REG-API-001 | Session 内容详情 | 事件数量、顺序、脱敏和分页一致 |
| REG-API-002 | Session 导出 | 字节边界、权限和取消行为一致 |
| REG-API-003 | Token summary/trends/rankings/sessions | 同一 Snapshot 总量可对账 |
| REG-API-004 | 内容清理与恢复 | MinIO、派生索引、Digest、Token 生命周期一致 |
| REG-API-005 | 前台并发 | 回填/离线对账开启后既有接口 p95 回归不超过 5% |
| REG-API-006 | `/tokens` 与 `/tokens/sessions` | 不读 `session_activity_slices`，与同范围 Token Analytics 一致 |
| REG-API-007 | 工作台 Token 卡片 | 不全量翻页；总量、趋势、成员排行、唯一 Session 数与服务端一致 |
| REG-API-008 | 64 Session/18,030 Component 冷查询 | 首次 Snapshot 不物化 Component，p99 不超过 3 秒 |
| REG-API-009 | 需求/任务关联新增 family v2 Session | 保存 root ID，不携带伪造日期，刷新后关联仍存在 |
| REG-API-010 | 旧日期切片关联迁移/解除 | 可正常取消，随后保存 root Session 不重复 |
| REG-API-011 | Snapshot/Rollup 后台回收 | HTTP 不执行清理 SQL；有效 Snapshot 引用不删，过期后小批回收 |
| REG-API-012 | `/tokens/sessions` 多页 | 后续页复用首页 Snapshot；不重复写入全量 Rollup 引用；过期后可自动重建 |
| REG-API-013 | MCP ad-hoc 活动时间 | `activity_start_at/end_at` 等于当日 Contribution 真实最早/最晚时间，不伪造整天边界 |

## 6. 停止条件

- 任一 Digest hash 或事件覆盖差异未解释；
- MCP full 模式仍直接依赖 `content_payload`；
- attached Digest 读取开始依赖实时 MinIO；
- `write_report_result` 完整性门禁被弱化；
- ad-hoc MCP Token 字段被误删或口径未完成测试服全量对账；
- 回填/离线对账任务造成现有接口明显回归。
