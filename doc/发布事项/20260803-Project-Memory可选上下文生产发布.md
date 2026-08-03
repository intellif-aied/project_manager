# Project Memory 可选上下文生产发布

## 1. 发布元数据

| 项目 | 内容 |
| --- | --- |
| 发布编号 | `20260803-01-project-memory-optional-context-prod` |
| 目标环境 | Aida 生产服，`192.168.14.182:/home/luoxian/aida` |
| 变更目标 | 上线 Project Memory 夜间整理，并将其作为系统日报的可选历史命名上下文，不强制项目归属 |
| Git 基线 | API `980ff3f8819b3f51bf2de9150d789d0db8cd7620`；Web、CLI 不变 |
| API 镜像 | `20260803-980ff3f-project-memory-context`，digest `sha256:9b6d53537aafa3042df536829d62d3f6c40cf6caa1cd4252b0e83565e1bfc5c4` |
| Report Skill | `10086/aida-report@1.1.29` |
| Project Memory 资产 | Agent `aida-project-memory-system-prod-v1`；Skill `project-memory-v4`；MCP `project-memory-v1` |
| 发布时间 | 2026-08-03 |

## 2. 完整范围清单

| 范围 | 本次状态 | 版本/提交/迁移 | 验收证据 | 影响与依赖 |
| --- | --- | --- | --- | --- |
| API/后台 | 发布 | `980ff3f` | 全量 Go 测试、vet、生产健康检查 | 仅替换 API |
| Web | 仅核对 | 保持 `20260729-3f139b7-current-report` | 容器未重建，`/reports` HTTP 200 | 无前端改动 |
| PostgreSQL migration | 发布 | `034`、`035`、`036` | 生产 `schema_migrations` 最高版本为 36 | 仅新增 Project Memory 表、字段和约束 |
| 历史数据回填 | 不涉及 | 未执行 | Project Memory Job 初始数量为 0 | 不扫描或改写历史日报 |
| 数据清理/删除 | 不涉及 | 未执行 | 无 DROP/TRUNCATE/DELETE | 无 |
| Report Skill | 发布 | `10086/aida-report@1.1.29` | Registry 唯一、未归档，正文回读一致 | 系统默认日报新 Run 使用 |
| MCP/默认 Agent | 发布 | Project Memory 专用 Agent/Skill/MCP | owner、版本、URL、Credential 绑定核对 | 不投影到普通用户资产页 |
| CLI/安装包 | 不涉及 | 无代码差异 | 未构建、未发布 | 无 |
| MinIO/对象存储 | 不涉及 | 容器未重建 | MinIO 保持 healthy | 无对象迁移 |
| Digest/报告链路 | 发布并核对 | Project Memory optional context | 刘乐冻结样本 3/3 命中“芯片验证平台”；生产报告只读接口 200 | 历史提示不是当天成果证据 |
| 文档/配置 | 发布 | 生产 `.env`、Compose 增加 Project Memory 变量 | Compose config 通过，容器实际环境一致 | 夜间任务已启用 |

## 3. 发布前证据

- `main == origin/main == 980ff3f8819b3f51bf2de9150d789d0db8cd7620`，工作区干净。
- `cd api && go test ./... -count=1`、`go vet ./...` 通过；API 与 Compose 代码 `git diff --check` 通过。
- 刘乐 2026-07-31 的 8 个冻结 Session 切片使用新 Context 与 `aida-report@1.1.29` 重复 3 次：3/3 生成“芯片验证平台”或“芯片验证平台使用手册”；测试执行模块未被强制合并；3 次最终报告均成功。
- 发布前生产 API 为 `20260801-75af0c6-selected-evidence-brief`，Report Skill 为 `1.0.22`。
- 在线备份目录：`/home/luoxian/aida/backups/project-memory-20260803T-production`；PostgreSQL dump 约 1.8 GB，`SHA256SUMS.txt` 校验通过；包含 `.env`、Compose 和旧 API inspect。

## 4. 实际执行结果

1. 推送 `main@980ff3f`。
2. 构建并推送不可变 API 镜像，Registry digest 为 `sha256:9b6d5353...`。
3. 从生产 `aida-report@1.0.22` owner-qualified derive 发布 `10086/aida-report@1.1.29`；Skill ID `78dec1fb-1f9d-4351-abba-8dc8c200e44d`，资源 SHA256 `fad1841426d602a093de2740a3d84d4da2d588819566e736913e00a23f8d07`，回读正文 SHA256 `13f03ce291d6eb7851a5a8f0907e5e66edf6117735fc55a5991eb9de6e66a2cb`。
4. 创建生产 Project Memory Skill `project-memory-v4`、MCP `project-memory-v1` 和 managed Agent v1；模型为 `deepseek-v4-flash`。
5. 只替换 API，自动执行 migration `034`～`036`；DB、Web、MinIO 未重启。
6. 核对生产配置后开启 `PROJECT_MEMORY_NIGHTLY_ENABLED=true`；首个真实用户日 Job 由生产用户后续保存或生成日报触发。

## 5. 发布后验收

| 验收项 | 结果 |
| --- | --- |
| API `/health` | HTTP 200，`{"status":"ok"}` |
| Web `/reports` | HTTP 200 |
| 生产报告只读接口 | 使用有效业务 Token 返回 HTTP 200 |
| migration | `34`、`35`、`36` 已应用 |
| API 镜像 | 容器 image ID 与 Registry digest 一致 |
| 系统资产 | owner `10086`，Report/Memory Skill、MCP、Agent 绑定一致 |
| 数据库锁 | 发布窗口 Lock waiters 为 0 |
| API 日志 | 发布窗口无 panic、fatal、dead、failed、error |
| Project Memory Job | 发布完成时为 0；等待真实用户日报事件触发，失败不阻塞日报 |

## 6. 回退与停止条件

- API：将 `API_IMAGE_TAG` 恢复为 `20260801-75af0c6-selected-evidence-brief`，仅替换 API。
- Report Skill：恢复 `MANAGED_AGENT_REPORT_SKILL_VERSION=1.0.22`。
- Project Memory：先设置 `PROJECT_MEMORY_NIGHTLY_ENABLED=false`；最近成功 Snapshot 可保留，不向日报写入历史成果。
- migration：`034`～`036` 为加法结构，回退不执行 DROP；旧 API 不读取新表。
- Web、CLI、MinIO、历史数据和清理不涉及回退。
- 若出现持续 5xx、报告成功率下降、历史成果进入当天日报、项目错误合并、DB 锁等待或 Memory Job 持续失败，立即关闭夜间任务并恢复上一 API/Skill 组合。

## 7. 最终判定

```text
本次范围已完整列出：是
所有发布项均有证据：是
所有阻断项均为 0：是
生产可以发布：是
最终状态：已发布；首批真实用户 Job 进入持续观察
```
