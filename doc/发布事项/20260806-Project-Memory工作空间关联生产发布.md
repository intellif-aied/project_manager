# Project Memory 工作空间关联生产发布

> 日期：2026-08-06
> 生产目标：`192.168.14.182:/home/luoxian/aida`

## 发布元数据

| 项目 | 内容 |
| --- | --- |
| Git 基线 | `main@ab8f50fc3b566dc7b66e8593841227ea3fdf034c` |
| API 镜像 | `20260806-ab8f50f-project-association` |
| Registry digest | `sha256:2b6f2e8e86d02bda8a4650ef474c349ddd33c1e1a650c45516125b76ea70a09c` |
| Report Skill | `10086/aida-report@1.1.31` |
| Project Memory | `10086/aida-project-memory@project-memory-v5`；Agent managed v3；`MiniMax-M2.5` |
| Migration | `037`、`038` |

## 发布范围

| 范围 | 状态 | 说明 |
| --- | --- | --- |
| API/后台 | 发布 | Workspace Identity、Project Memory 关联证据分层、日报父项目归并 |
| Web | 不涉及 | 未构建、未替换 |
| PostgreSQL migration | 发布 | 新增 `sessions.repository_key` 和 6 张 Workspace/Project 关联表 |
| 历史数据回填 | 不涉及 | 旧 Session 保持 `repository_key=NULL`，不回写历史日报 |
| 数据清理/删除 | 不涉及 | 无业务数据 DELETE、DROP、TRUNCATE |
| Report Skill | 发布 | `1.1.30` → `1.1.31` |
| MCP/默认 Agent | 发布并核对 | Project Memory Skill v5；Report MCP、Project Memory MCP 版本不变 |
| CLI | 不涉及 | 当前生产仍为 0.1.27；Git Repository Key 随后续客户端升级逐步进入 |
| MinIO | 不涉及 | 容器未替换、未重启 |
| 文档/配置 | 发布 | 更新两个系统 Skill 版本与 API 镜像标签 |

## 发布前验证

- API、Daemon 全量 Go 测试通过，`git diff --check` 通过。
- 固定 Project Association 数据集 7/7 通过；刘乐、张垒超、田贤材关键 Case 均通过。
- 生产副本演练恢复完整 Schema、45 个用户和 16,398 个 Session，migration 037/038 通过；旧 Session 兼容为空 Repository Key。
- 发布前生产 migration 最高为 36，API 为 `20260805-37a6a12-single-layer-daily`，Report Skill 为 1.1.30，Project Memory Skill 为 v4。

## 备份

- 目录：`/home/luoxian/aida/backups/project-memory-association-20260806T122739CST`。
- PostgreSQL custom dump：约 2.0 GB；SHA256 `bdb19abe8cb79ae788d005b445768e71b1ca118366d373dd51aa218443aab1ef`；`pg_restore --list` 通过。
- 同目录保存发布前 `.env` 和 `docker-compose.yml`。
- 首次发布前 dump 因远端进程未结束而无效；已删除并替换为 migration 完成后的完整一致性 dump。业务表未执行回填或清理，旧 API 可直接读取该备份。

## 实际执行与异常记录

1. 发布生产 Report Skill 1.1.31：Skill ID `337467d9-b0dd-42c2-9b1a-6900c472fd91`，资源 SHA256 `dcdf56dfe745cbcc406469f2f71cd53873f9cdf1dd68a1481e70f3d28abfa0d6`，Registry 文件 SHA256 `338902393a0172d6a6515995c2fa09baf1e363f883e09c8f26dbc72d84caa9af`。
2. 发布 Project Memory v5：Skill ID `f21207a3-cabb-437e-a5be-1a1fcb5da141`，资源 SHA256 `0d7c8ed8614e5fd6bcf79c48a7cd69b4d630e65633fdda79aa7c20b3dff5dc3e`，Registry 文件 SHA256 `3fb83beb5c0846a398ba545101398fb42178fcd74641a50769167af267bd291b`。
3. 仅替换 API；DB、Web、MinIO 未重启。
4. 首次备份命令的远端 `pg_dump` 在工具连接返回后仍运行，migration 037 等待其只读长事务，健康入口约 68 秒返回 502。确认阻塞 PID 后取消两个 pg_dump，migration 037/038 随即完成，API 恢复；无业务写入失败、无数据库死锁。原未完成 dump 不作为备份证据，随后重新执行完整 detached 备份并校验。

## 发布后验收

| 项目 | 结果 |
| --- | --- |
| `/health`、`/health/ready` | HTTP 200 |
| Web `/reports` | HTTP 200 |
| 管理员日报价值接口 | HTTP 200，响应结构正常 |
| migration | 37、38 已登记 |
| API 镜像 | 容器 ID 与 Registry digest 一致 |
| 系统 Report Agent | `aida-agent-dk9dxzv7b0fe` 已绑定 `10086/aida-report@1.1.31` |
| Project Memory Agent | 已绑定 v5、MCP v1、MiniMax-M2.5 |
| 数据库锁 | Lock waiters 为 0 |
| API 日志 | migration 完成后无 panic、fatal、deadlock、serialization error |

## 回退

1. `API_IMAGE_TAG` 恢复为 `20260805-37a6a12-single-layer-daily`。
2. `MANAGED_AGENT_REPORT_SKILL_VERSION` 恢复为 `1.1.30`。
3. `PROJECT_MEMORY_SKILL_VERSION` 恢复为 `project-memory-v4`；必要时将 Project Memory Agent 恢复绑定 v4。
4. 仅执行 `docker compose up -d --no-deps api`。
5. migration 037/038 为加法结构，回退不执行 DROP；旧 API 不读取新增字段和表。

## 最终判定

```text
本次范围已完整列出：是
所有发布项均有证据：是
所有阻断项均为 0：是
生产可以发布：是
最终状态：已发布
```
