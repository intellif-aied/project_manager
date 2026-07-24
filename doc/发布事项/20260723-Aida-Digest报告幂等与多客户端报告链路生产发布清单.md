# 20260723-Aida-Digest报告幂等与多客户端报告链路生产发布清单

## 1. 发布元数据

| 字段 | 内容 |
| --- | --- |
| 发布编号 | `20260723-01-digest-report-multiclient` |
| 目标环境 | 生产 |
| 发布负责人 | 待填写 |
| 发布目标 | 发布 Digest、报告 Run 幂等、多客户端上传和报告链路变更 |
| Git 基线 | API/Web/CLI：`77d293f4624be5f7b24e9d3de306929b537f892c` |
| 对比基线 | `origin/main`：`7a553f46f6eeb104d478b2115078676f239f458f` |
| 版本 | API/Web/CLI：`0.1.21`；Skill/MCP：待填写 |
| 时间 | 开始：待填写；结束：待填写；时区：Asia/Shanghai |

## 2. 发布范围

| 范围 | 状态 | 版本/提交/迁移 | 验收证据 | 影响与依赖 |
| --- | --- | --- | --- | --- |
| API/后台 | 发布 | `main` / `77d293f`; 当前生产 `20260721-6ebd113-multiclient` | `go test ./...` 通过；生产未切换 | Digest、Report Run、MCP、Token Analytics |
| Web | 发布 | main / 77d293f; 生产 20260723-77d293f-release | clean build、typecheck、变更文件 lint 和 workflow contract 通过 | 报告运行跟踪、Token 快照恢复 |
| AIDA CLI | 发布 | `VERSION=0.1.21`；daemon 代码同一基线 | daemon 测试通过；生产包待构建 | Codex、Claude Code、OpenClaw、Kimi Code、OpenCode 上传 |
| PostgreSQL migration | 发布 | `api/db/migrations/027_digest_on_demand_report_runs.sql`; 当前生产最高 `26` | 空库/生产数据副本/生产执行待验证 | Digest、Report Run 持久化和队列 |
| 历史数据回填 | 不涉及 | 无 | 无回填命令 | 不改变历史 Session、Digest、Report Context |
| 数据清理/删除 | 不涉及 | 无 | 本次未执行生产清理 | 不删除生产数据 |
| Report Skill | 仅核对 | 生产 owner `10086`；当前配置 `1.0.16` | owner、version、SHA256 待核对 | 托管报告 Agent |
| MCP/默认 Agent | 仅核对 | MCP URL、Credential Slot、Agent 版本待填写 | 真实调用待完成 | Report Context 读取和报告写回 |
| MinIO/对象存储 | 仅核对 | 现有生产配置 | 对象可读性、hash、权限待核对 | Session 原文和内容读取 |
| Digest/报告链路 | 发布 | Digest v2.10、Report Context V1、Run Processor/Reconciler | 测试服主链路成功；生产验收待执行 | 上传、来源选择、报告生成 |
| 监控/配置/文档 | 发布 | `deploy/monitoring/digest-report-rules.yml`、本清单 | 指标、告警和文档待核对 | 运行监控和故障止损 |

## 3. 版本与代码证据

| 组件 | 代码/配置 | 证据 |
| --- | --- | --- |
| API | `d29dfc1`、`0c5e030`、`e7e5e94`、`77d293f` | `go test ./...` 全部通过 |
| Report Run 幂等 | `api/internal/reportsource/run_submission.go` | 测试服同一幂等键并发两次 HTTP 200，返回同一 Run |
| Digest Coordinator | `api/internal/sessiondigestv2/coordinator.go` | Digest/报告相关测试通过 |
| Report Agent 契约 | `api/handler/report_run_submitter.go`、`report_mcp_write.go`、`daily_report_skill.go` | Report Agent workflow contract 通过 |
| Token Analytics | `web/src/features/aidashboard/tokens/pages/TokenAnalyticsPage.tsx` | Token Analytics workflow contract 通过 |
| CLI 多客户端 | `daemon/internal/adapters/openclaw/adapter.go`、`kimicode/adapter.go`、`opencode/adapter.go` | daemon 测试通过 |
| 数据库 | `027_digest_on_demand_report_runs.sql` | 独立数据库验证待执行 |

## 4. 变更项清单

| 变更项 | 文件/提交/版本 | 状态 | 验证命令或运行证据 | 影响范围 |
| --- | --- | --- | --- | --- |
| Digest 按需构建、等待和恢复 | `d29dfc1`、`0c5e030` | 已实现 | `go test ./...`；测试服真实 Digest ready | 新报告来源准备 |
| 报告 Run `40001` 事务重试 | `run_submission.go`、`d2655aa` | 已实现 | 并发两次返回同一 Run | 双击、重试、多实例并发 |
| Report Agent 使用 `run_id` | `77d293f` | 已实现 | API/Report Agent contract 测试 | Context 读取和写回 |
| Token 快照过期自动恢复 | `e7e5e94`、Web Token Analytics 页面 | 已实现 | workflow contract；浏览器验收待执行 | Trends、Rankings、Sessions |
| Canonical 多客户端事件 | `aabab62`、`443717d` | 已实现 | canonical regression、daemon 测试 | 多客户端 Digest |
| OpenClaw/Kimi Code/OpenCode CLI | daemon adapters | 已实现 | daemon 测试；生产 CLI 待构建 | CLI Session 上传 |
| Digest 报告 migration | `027_digest_on_demand_report_runs.sql` | 待执行 | 空库和生产副本验证待执行 | API 启动和数据结构 |

## 5. 发布前检查

- [ ] 工作区状态、API/Web/CLI SHA 已冻结；
- [ ] API 镜像使用不可变 tag 和 digest；
- [ ] Web 依赖安装完成，`pnpm typecheck`、`pnpm lint`、`pnpm build` 通过；
- [ ] CLI 使用 `make release-prod-dir` 构建；
- [ ] CLI 三平台产物、`install.sh`、`install.ps1`、`SHA256SUMS.txt`、`aida-latest.txt` 已生成；
- [ ] CLI 从生产地址下载、版本、路由和 SHA256 校验通过；
- [ ] migration 在空库和生产数据副本验证；
- [ ] 生产 PostgreSQL、配置和回退镜像备份已记录；
- [ ] 生产 Skill owner、slug、version、SHA256 已核对；
- [ ] 默认 Agent、MCP URL、Credential Slot 已核对；
- [ ] 生产测试小组账号已确认；
- [ ] 生产停止条件和回滚边界已填写。

## 6. 执行步骤

| 步骤 | 执行主机 | 操作/命令 | 预期结果 | 实际结果 |
| --- | --- | --- | --- | --- |
| 1 | 发布机 | 冻结 `77d293f`、版本和本清单 | 范围固定 | 待执行 |
| 2 | `192.168.14.182:/home/luoxian/aida` | 记录容器、镜像、配置、migration、Skill/MCP、CLI 分发 | 现状证据完整 | 已读取：API/Web `20260721-6ebd113-multiclient`；migration `26`；Skill `1.0.16`；db/minio 正常 |
| 3 | 生产备份环境 | 备份 PostgreSQL、配置和回退镜像 | 备份路径和 SHA256 可追溯 | 待执行 |
| 4 | 发布机 | 构建并推送 API/Web 不可变镜像 | 镜像 digest 固定 | 待执行 |
| 5 | 生产主机 | 更新 API，执行正向 migration | API 健康，migration 完成 | 未执行；当前最高 migration `26` |
| 6 | 生产主机 | 检查 Digest、队列、Report Context、MCP | 报告读取链路正常 | `/health` 返回 `{"status":"ok"}`；业务链路未执行 |
| 7 | Managed Agent 平台 | 发布或核对生产 Skill、MCP、默认 Agent | owner/version/hash/slot 一致 | 待执行 |
| 8 | 生产主机 | 更新 Web | Web 使用目标镜像 | 待执行 |
| 9 | 发布机/静态分发 | `make release-prod-dir`，上传 CLI 产物后切换 `aida-latest.txt` | 生产下载和校验通过 | 待执行 |
| 10 | 生产验收机 | 使用生产测试小组账号执行 API、Web、CLI 和报告验收 | 结果和证据完整 | 待执行 |

## 7. 发布后验收

| 验收类别 | 用例 | 账号/数据 | 预期结果 | 实际结果 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 自动化 | API 全量测试 | 测试数据/独立数据库 | 全部通过 | 已通过测试服代码测试；生产待执行 | 待补 |
| API/接口 | 健康、认证、来源候选、Selection、Run | 生产测试小组账号 | HTTP 状态和字段正确 | 待执行 | 待补 |
| AIDA CLI | 安装、版本、`aida update` | 生产测试小组账号 | 版本、路由、SHA256 正确 | 待执行 | 待补 |
| AIDA CLI | Codex 上传 | 生产测试小组 Session | 上传、Projection、Digest ready | 待执行 | 待补 |
| AIDA CLI | Claude Code 上传 | 生产测试小组 Session | 上传、Projection、Digest ready | 待执行 | 待补 |
| AIDA CLI | OpenClaw 上传 | 生产测试小组 Session | Canonical、Digest、Session 可见 | 待执行 | 待补 |
| AIDA CLI | Kimi Code 上传 | 生产测试小组 Session | Canonical、Digest、Session 可见 | 待执行 | 待补 |
| AIDA CLI | OpenCode 上传 | 生产测试小组 Session | Canonical、Digest、Session 可见 | 待执行 | 待补 |
| Digest/报告 | 上传→Digest→Selection→Context→Run→Report | 生产测试小组账号 | 报告成功写回 | 测试服主链路成功；生产待执行 | `3c90063b...` |
| Digest/报告 | Digest pending、关闭页面、恢复 | 生产测试小组账号 | Run 自动推进或进入明确终态 | 待执行 | 待补 |
| 报告幂等 | 同一 UUID v4 并发提交 | 生产测试小组账号 | 同一 Run，不出现 `40001` | 测试服通过；生产待执行 | `b8255c95...` |
| Report Skill/MCP/Agent | `run_id` Context 读取和写回 | 生产测试小组账号 | 读取范围和写回归属正确 | 待执行 | 待补 |
| Token/Session | Summary、Trends、Rankings、Sessions | 生产测试小组账号 | token 复用、过期后恢复 | 待执行 | 待补 |
| Web 浏览器 | 报告创建、等待、成功/失败、Token 页面 | 生产测试小组账号 | 页面状态和错误展示正确 | 待执行 | 待补 |

## 8. 回滚与停止条件

| 范围 | 回滚方式 | 停止条件 |
| --- | --- | --- |
| API | 恢复上一不可变 API 镜像；migration 保留已执行结构 | API 持续 5xx、数据库锁或兼容性错误 |
| Web | 恢复上一不可变 Web 镜像 | 页面关键流程回归 |
| AIDA CLI | 版本发现文件停止切换；已升级客户端执行 forward fix 或人工重装 | 产物、版本、地址或 SHA256 不一致 |
| Skill/MCP/Agent | 恢复历史不可变版本和配置引用 | owner、version、hash、凭据槽不一致或调用失败 |
| migration | 不执行反向 DROP；使用 forward fix | migration 失败或数据对账不一致 |
| 数据回填 | 本次不涉及 | 出现未授权回填 |
| 数据清理 | 本次不涉及 | 出现未授权删除、清理或清卷 |

## 9. 最终结果

```text
发布范围已完整列出：是
发布项执行结果已记录：否（发布步骤和生产验收仍有待执行项）
发布后验收已完成：否
阻断项数量：1（生产发布执行结果和生产验收证据不完整）
最终状态：阻断
```

测试服真实证据：

- Session `019f8441-0096-74f3-b825-bf639db1197b` 上传成功；
- Slice `d2030d72-82c5-4ee6-98b7-6f8b3348f198` 状态 `available/ready`；
- Run `3c90063b-3ed9-43a6-9340-c4256911f0b9` 最终 `succeeded`；
- Report `2f974781-abf7-4138-bec9-9da775a4d913` 写回成功；
- 并发幂等 Run `b8255c95-4c61-4974-92a5-1546caa905c1` 创建接口两次 HTTP 200 且返回同一 Run；
- 该 Run 后续外部状态为 infrastructure_failure，归类为 GLM-5.2 托管环境限制，不作为本次代码发布阻断。
