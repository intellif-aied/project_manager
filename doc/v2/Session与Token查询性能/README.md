# Session 与 Token 存储查询优化

本目录是 Session 内容存储与 Token 查询优化的开发依据。

## 文档

1. [需求与技术方案](./01-需求与技术方案.md)：产品边界、当前架构、目标架构、数据模型和兼容设计。
2. [开发实施计划](./02-开发实施计划.md)：开发顺序、代码边界、交付物、完成条件和当前进度。
3. [测试与发布方案](./03-测试与发布方案.md)：功能、性能、数据迁移、生产发布和回滚门禁。

## 当前进度

| 范围 | 状态 |
| --- | --- |
| Report Source 轻量目录与分页 | 代码已完成，待最终性能回归 |
| Token Contribution、Session Family 和 Rollup | 已完成并上线 |
| Token API、Web 和 MCP 查询切换 | 已完成并上线 |
| MinIO 统一内容读取器 | **未开始，下一个开发项** |
| Digest、MCP full、Session 内容读取迁移 | 未开始 |
| 停止 PostgreSQL 完整 Payload 写入 | 未开始 |
| 历史 Payload 清理与空间回收 | 未开始 |

项目尚未完成，不具备停止 Payload 写入或清理生产历史数据的条件。
