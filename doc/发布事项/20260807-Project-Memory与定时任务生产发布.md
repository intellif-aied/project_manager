# Project Memory 与定时任务生产发布

> 日期：2026-08-07
> 生产目标：`192.168.14.182:/home/luoxian/aida`

## 发布元数据

| 项目 | 内容 |
| --- | --- |
| Git 基线 | `main@1c9f12f` |
| API 镜像 | `20260807-1c9f12f-project-memory-v10`；`sha256:be0ab2ff6e28fe73d4d8cd396d9b74cad09e2704e5899b820771dc253af648e3` |
| Web 镜像 | `20260807-1c9f12f-project-memory-v10`；`sha256:24e7be1592a731ff857a7d1322c0baef3a341dca58e58e2f05bd7b93e2df2d32` |
| Report Skill | `10086/aida-report@1.1.33` |
| Project Memory | `10086/aida-project-memory@project-memory-v10` |
| Migration | `039_daily_report_email.sql`、`040_report_project_workstream_cues.sql` |

## 发布范围

| 范围 | 状态 | 说明 |
| --- | --- | --- |
| API/后台 | 发布 | Project Memory 分层历史窗口、工作线索、Report Compiler 单次提案；保留系统 Agent 定时任务路由 hotfix |
| Web | 发布 | 保留定时任务单一友好错误提示 hotfix |
| PostgreSQL migration | 发布 | 应用已进入 `main` 但尚未上线的 039，以及 Project Memory `workstream_cues` 的 040 |
| 历史数据回填 | 不涉及 | 不批量重算 Project Memory，不改写历史日报 |
| 数据清理/删除 | 不涉及 | 无 `DELETE`、`DROP`、`TRUNCATE` |
| Report Skill | 发布 | 发布不可变版本 `1.1.33` |
| MCP/默认 Agent | 发布并核对 | MCP 协议不变；Project Memory Skill 发布 v10；核对两个系统 Agent 绑定 |
| CLI/安装包 | 不涉及 | 本次不构建、不切换 CLI |
| MinIO/对象存储 | 不涉及 | 不替换、不重启 |
| Digest/报告链路 | 发布后核对 | 核对 Report Context、Brief 提交、报告写入和 Project Memory 提示 |
| 文档/配置 | 发布 | 记录镜像、迁移、Skill 版本和验收证据 |

> 日报邮件能力已存在于此次 `main` 基线，但生产未配置 SMTP 且 `REPORT_EMAIL_ENABLED` 默认为 false，本次不会启动邮件 Worker、不会发送邮件。

## 发布前验证

- API `go test ./...` 通过。
- Web 工作流契约测试与生产构建通过。
- Project Memory/Report Compiler 已在测试服使用固定数据集与真实 Session 回归。
- 生产当前 API：`20260807-bb351b7-schedule-source-message`；Web：`20260807-0530b60-schedule-single-toast`；migration 最高 `38`。

## 备份与回退

- 涉及正向 migration，已保存发布前 `.env`、Compose、当前容器和镜像信息，并在 migration 完成后生成完整 PostgreSQL 一致备份。
- 备份目录：`/home/luoxian/aida/backups/project-memory-v10-20260807T1518CST`。首次 `pg_dump` 在连接返回后仍运行，导致 migration 040 等待表锁；已取消该备份会话，migration 完成后以可追踪后台任务重新生成完整一致备份。被取消的文件移入 `cancelled-partial/`，不作为备份证据。
- 有效 PostgreSQL custom dump：2,208,634,265 字节；SHA256 `fe39ae612795bec59e27ea4c301cded9b6e635b31725403f53e302df8f822ad8`；`pg_restore --list` 952 项通过。
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
| Git/镜像 | 已发布 | `main@1c9f12f`；API/Web digest 与本文发布元数据一致 |
| PostgreSQL 备份/migration | 已完成 | migration 最高 `40`；完整备份 SHA256 `fe39ae612795bec59e27ea4c301cded9b6e635b31725403f53e302df8f822ad8` |
| Report Skill/Project Memory Skill | 已发布 | Report Skill SHA256 `d78b861f6098e395e607a50a14442167ee02f36872a365944146838183a60c41`；Project Memory Skill SHA256 `aa4cf83108b0b8c54b620b34f72bba71548226981340fd2bf2ca3494de67c7dd` |
| API/Web 容器 | 已发布 | API/Web 均使用 `20260807-1c9f12f-project-memory-v10`；DB、MinIO 未重启 |
| 健康检查与关键链路 | 已通过 | `/health`、`/reports`、Report Integration、日报价值接口、定时任务列表均为 HTTP 200；数据库未授予锁为 0 |

```text
本次范围已完整列出：是
所有发布项均有证据：是
所有阻断项均为 0：是
生产可以发布：是
最终状态：已发布
```
