# Project Memory V2 与日报主项归并生产发布

> 日期：2026-08-08
> 生产目标：`192.168.14.182:/home/luoxian/aida`

## 发布元数据

| 项目 | 内容 |
| --- | --- |
| 发布编号 | `20260808-01` |
| 目标环境 | 生产 |
| Git 基线 | `main@1b83dd9` |
| API 镜像 | `20260808-1b83dd9-project-memory-v2-density`（发布后补充 digest） |
| Web 镜像 | 不涉及；保留 `20260807-1c9f12f-project-memory-v10` |
| Report Skill | 计划从 `10086/aida-report@1.1.33` derive `1.1.34` |
| Project Memory | 计划发布 `10086/aida-project-memory@project-memory-v11` |
| Report Reviewer | 计划发布 `10086/aida-report-review@report-review-v10` |
| Migration | `041_report_semantic_review.sql`、`042_report_review_run_stages.sql`、`043_project_memory_v2.sql` |
| 开始/结束时间 | 2026-08-08 / 待完成 |

## 完整范围清单

| 范围 | 本次状态 | 版本/提交/迁移 | 验收证据 | 影响与依赖 |
| --- | --- | --- | --- | --- |
| API/后台 | 发布 | `1b83dd9` | `go test ./...` 通过；测试服 7/7 报告成功 | API 短暂重建；DB、MinIO、Web 不重启 |
| Web | 不涉及 | 保留原镜像 | 页面接口契约不变 | 无前端文件变更 |
| PostgreSQL migration | 发布 | 041、042、043 | 测试服已应用，完整后端测试通过 | 正向加法迁移；043 会合并 Project Memory 队列并进入待重建状态 |
| 历史数据回填 | 不涉及 | 无单独回填脚本 | Project Memory 夜间 Worker 按队列渐进重建 | 不同步阻塞发布 |
| 数据清理/删除 | 不涉及 | 无业务数据清理 | 043 仅删除同用户重复队列行，不删除日报、Session 或 Project Memory 历史快照 | 无不可恢复用户数据删除 |
| Report Skill | 发布 | `1.1.34` | 测试服 `1.1.44` 固定数据集 7/7 成功 | 生产 owner 10086，不复用测试版本 |
| MCP/默认 Agent | 发布并核对 | Project Memory v11；Reviewer v10 | 测试服 V2 快照和 Reviewer 全链路通过 | MCP 协议地址不变；生产新增 Reviewer 配置透传 |
| CLI/安装包 | 不涉及 | 保留现状 | 无 CLI 文件变更 | 不切换版本发现文件 |
| MinIO/对象存储 | 不涉及 | 保留现状 | 不替换、不重启 | 无对象写入迁移 |
| Digest/报告链路 | 仅核对 | `digest_v2` 不变 | 测试服 7 份真实回放成功 | 核对 Context、Reviewer、报告写回 |
| 文档/配置 | 发布 | 本清单；生产 `.env`/Compose Reviewer 配置 | 发布后记录实际值与 hash | 密钥不写入文档 |

## 发布前门禁

- [x] 工作区未纳入的邮件通讯录文档不进入本次提交。
- [x] API 提交已冻结并推送：`1b83dd9`。
- [x] 完整后端测试 `go test ./...` 通过。
- [x] 测试服 Report Skill `1.1.44`、Reviewer `v10`，7/7 报告与 7/7 Reviewer 成功。
- [x] Project Memory V2 为可选辅助上下文，不强制项目归属。
- [ ] 生产 PostgreSQL、配置和旧镜像信息完成备份。
- [ ] 生产不可变 API 镜像完成构建、推送和 digest 核对。
- [ ] 生产 Skill、Agent、migration 和关键链路验收完成。

## 回退与停止条件

- API：恢复 `20260807-1c9f12f-project-memory-v10`。
- Web：不涉及，保持原镜像。
- Report Skill：恢复 `10086/aida-report@1.1.33`。
- Project Memory：恢复 `project-memory-v10`；Reviewer 可关闭并移除生产环境引用。
- migration：041～043 保留已执行结构，不执行反向 `DROP`；旧 API 不读取新增结构。
- 数据回填/清理：无独立任务；停止 Project Memory Worker 即可暂停后续重建。

立即停止条件：生产持续 5xx、migration 失败或数据库锁异常、报告队列 dead、Skill/MCP/Agent 版本或 hash 不一致、报告写回为空。

## 最终发布判定

```text
本次范围已完整列出：是
所有发布项均有证据：待发布后确认
所有阻断项均为 0：待发布后确认
生产可以发布：是
最终状态：发布中
```

## 上线结果

| 项目 | 实际结果 | 证据 |
| --- | --- | --- |
| API/Web 容器 | 待填写 |  |
| migration | 待填写 |  |
| 数据备份 | 待填写 |  |
| Skill/MCP/Agent | 待填写 |  |
| 健康检查 | 待填写 |  |
| 关键接口 | 待填写 |  |
| 观察窗口 | 待填写 |  |
| 未完成项与后续任务 | 待填写 |  |
