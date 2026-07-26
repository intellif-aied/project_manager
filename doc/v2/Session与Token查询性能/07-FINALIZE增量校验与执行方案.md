# FINALIZE 静态归档校验与执行方案

## 1. 状态

> 状态：2026-07-26 生产执行完成

`session_content_events_payload_archive` 已完成独立备份、测试恢复和生产删除。`session_content_events_compaction_state.phase` 已从 `swapped` 更新为 `finalized`。

## 2. 最终产品口径

- `session_content_events` 是唯一线上业务表；
- 当前表不包含 `content_payload`；
- 旧归档表只用于观察期回滚，不参与业务读写；
- 观察期结束后，旧表必须先独立备份并验证恢复，再从生产数据库删除；
- 删除完成后，普通 PostgreSQL 全库备份不再需要旧表排除规则；
- 禁止手工 `DROP TABLE` 绕过 `compact-session-events finalize` 的状态和结构门禁。

## 3. 独立备份

生产备份目录：

```text
/home/luoxian/aida/backups/session-content-payload-archive-20260726T040000Z
```

归档文件：

```text
session_content_events_payload_archive.dump
```

备份结果：

| 项目 | 结果 |
| --- | --- |
| 格式 | PostgreSQL Custom Format |
| 压缩后大小 | 2.7GB |
| SHA256 | `b100610beb56e6520bd70ed432a7b186d193f8548bed03f2a08d098fafc1fb5e` |
| `pg_restore --list` | 通过，包含表、数据、主键、三个索引和两个外键定义 |
| 配套 Schema 备份 | `database_schema_without_archive.dump` |
| Schema SHA256 | `75e37a397818a20c93e715c511ab225950f79e8853e91856140ee71885469658` |

## 4. 测试恢复

备份复制到测试服务器后，在一次性 PostgreSQL 数据库中完成恢复。隔离库没有生产 Projection Revision 和 Upload Chunk 数据，因此恢复时只跳过两个外键创建；外键定义仍保留在备份清单中，生产原库恢复时依赖表仍存在。

精确恢复结果：

```text
总行数：3,321,938
content_payload 非空：3,224,995
最早 created_at：2026-07-15 12:57:26.152946+00
最晚 created_at：2026-07-19 04:57:06.514840+00
恢复后 relation：6,859MB
```

恢复库存在：

- `session_content_events_pkey`；
- `idx_session_content_events_occurred`；
- `idx_session_content_events_revision_cursor`；
- `idx_session_content_events_source_range`；
- cursor、hash、type 三个检查约束。

最大 `created_at` 距清理时间已超过一周，证明旧表观察期内没有继续写入。

## 5. finalize 确定性门禁

提交 `2f35019` 将 `finalize` 收敛为静态归档清理，不再在排他锁内扫描 332 万行。

锁前和锁内固定检查：

1. state 必须为 `swapped`；
2. state 中 source、shadow、archive 表身份必须与固定名称一致；
3. 当前业务表和旧归档表必须同时存在；
4. 当前业务表不得包含 `content_payload`；
5. 获取两张表的 `ACCESS EXCLUSIVE` 锁，锁等待受 `--lock-timeout` 限制；
6. 两个回滚窗口同步触发器必须已经移除；
7. `--confirm-drop` 必须精确等于 `session_content_events_payload_archive`。

满足后只执行：

- 删除旧归档表；
- 删除两个旧镜像函数；
- 将当前表约束和索引从 compact 名称改回正式名称；
- 将 state 更新为 `finalized`；
- 事务提交后 `ANALYZE session_content_events`。

任一步失败均回滚，旧表和 state 保持原状。

## 6. 自动化验证

- finalize 拒绝错误的 `--confirm-drop`；
- 同步触发器仍存在时拒绝删除；
- 当前表仍含 `content_payload` 时拒绝删除；
- 正确静态归档状态下删除成功；
- 删除后 archive 不存在、state 为 `finalized`；
- API `go test ./...` 通过；
- `go vet ./...` 通过；
- 真实 PostgreSQL Content Compaction 端到端集成测试通过。

## 7. 生产执行结果

2026-07-26 04:52:06 UTC 在短维护窗口执行：

```text
compact-session-events
  --action finalize
  --apply
  --confirm-drop session_content_events_payload_archive
  --lock-timeout 5s
  --statement-timeout 30s
  --timeout 2m
```

结果：

- state=`finalized`；
- 旧归档表不存在；
- 新表无 `content_payload`；
- 新表主键、外键、检查约束和三个业务索引均为正式名称；
- API 自动恢复并通过健康检查；
- 磁盘可用空间由 363GB 增至 368GB；
- 独立备份保留 2.7GB，因此生产磁盘净增加约 5GB。

## 8. 生产验收

- 全新真实 Codex Session 上传成功；
- Session Generation 与 Content Projection cursor 一致；
- Projection 事件计数与实际事件计数一致；
- Session、Token Summary、个人日报、个人周报、Report Source 接口均为 HTTP 200；
- Report MCP initialize 为 HTTP 200；
- 最终 API 镜像启动后 Session Sync 500 为 0；
- 最终 API 镜像启动后 PostgreSQL deadlock 为 0。

## 9. 恢复边界

删除后不能通过表名交换回滚。需要恢复旧 Payload 时，使用本专项 Custom Format 备份；恢复前必须校验 SHA256，并在隔离数据库确认清单和目标范围。普通业务回滚不得重新把应用切回旧表。
