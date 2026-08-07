# Project Memory 与定时任务生产发布

> 日期：2026-08-07
> 生产目标：`192.168.14.182:/home/luoxian/aida`

## 发布元数据

| 项目 | 内容 |
| --- | --- |
| Git 基线 | 待冻结 |
| API 镜像 | 待构建 |
| Web 镜像 | 待构建 |
| Report Skill | `10086/aida-report@1.1.33`
| Project Memory | `10086/aida-project-memory@project-memory-v10`
| Migration | `040_report_project_workstream_cues.sql`

## 发布范围

| 范围 | 状态 | 说明 |
| --- | --- | --- |
| API/后台 | 发布 | Project Memory 分层历史窗口、工作线索、Report Compiler 单次提案；保留系统 Agent 定时任务路由 hotfix |
| Web | 发布 | 保留定时任务单一友好错误提示 hotfix |
| PostgreSQL migration | 发布 | 新增 Project Memory `workstream_cues` 字段 |
| 历史数据回填 | 不涉及 | 不批量重算 Project Memory，不改写历史日报 |
| 数据清理/删除 | 不涉及 | 无 `DELETE`、`DROP`、`TRUNCATE` |
| Report Skill | 发布 | 发布不可变版本 `1.1.33` |
| MCP/默认 Agent | 发布并核对 | MCP 协议不变；Project Memory Skill 发布 v10；核对两个系统 Agent 绑定 |
| CLI/安装包 | 不涉及 | 本次不构建、不切换 CLI |
| MinIO/对象存储 | 不涉及 | 不替换、不重启 |
| Digest/报告链路 | 发布后核对 | 核对 Report Context、Brief 提交、报告写入和 Project Memory 提示 |
| 文档/配置 | 发布 | 记录镜像、迁移、Skill 版本和验收证据 |

## 发布前验证

- API `go test ./...` 通过。
- Web 工作流契约测试与生产构建通过。
- Project Memory/Report Compiler 已在测试服使用固定数据集与真实 Session 回归。
- 生产当前 API：`20260807-bb351b7-schedule-source-message`；Web：`20260807-0530b60-schedule-single-toast`；migration 最高 `38`。

## 备份与回退

- 涉及正向 migration，发布前完整备份 PostgreSQL，并保存 `.env`、Compose、当前容器和镜像信息。
- API 回退至 `20260807-bb351b7-schedule-source-message`。
- Web 回退至 `20260807-0530b60-schedule-single-toast`。
- Report Skill 回退引用至 `1.1.32`；Project Memory 回退引用至 `project-memory-v5`。
- Migration 040 为加法字段，回退 API 时保留字段，不执行反向 `DROP`。

## 停止条件

- API 持续 5xx、migration 失败或数据库锁异常；
- Report Skill/Project Memory Skill 版本或 hash 无法核对；
- 系统 Agent 无法启动日报或 Project Memory 任务；
- Report Context、Brief 或日报读写出现空内容或不兼容。

## 上线结果

| 项目 | 实际结果 | 证据 |
| --- | --- | --- |
| Git/镜像 | 待执行 |  |
| PostgreSQL 备份/migration | 待执行 |  |
| Report Skill/Project Memory Skill | 待执行 |  |
| API/Web 容器 | 待执行 |  |
| 健康检查与关键链路 | 待执行 |  |

```text
本次范围已完整列出：是
所有发布项均有证据：待执行
所有阻断项均为 0：待执行
生产可以发布：是
最终状态：发布中
```
