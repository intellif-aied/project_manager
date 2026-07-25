# 20260725 Report Projection 最终复核测试发布

## 1. 发布元数据

| 字段 | 内容 |
| --- | --- |
| 发布编号 | `20260725-03-report-projection-final-review` |
| 目标环境 | 14.157 测试服 |
| 发布负责人 | Codex（测试服执行） |
| 发布目标 | 验证 Projection 完整性、Git 元数据降噪和 Report Agent 稳定错误展示 |
| Git 基线 | API `fix/report-projection-final-review@a5fd7d5` 加本次未提交修改；Web、CLI 不涉及 |
| 版本 | API 镜像 `sha256:9c04f7fc372c9daf22d56f6cbe8f6e0c32375829ca2039d8956fba398bffa1d2`；Skill `100866/aida-report@1.0.50`；MCP `aida-report-mcp@report-v1` |
| 时间 | 2026-07-25 19:00–19:34，Asia/Shanghai |

## 2. 发布范围

| 范围 | 状态 | 内容与核对依据 |
| --- | --- | --- |
| API/后台 | 发布 | Projection 围栏与 Git 元数据处理、Projection 失败禁止回退、报告 Agent 错误归一 |
| Web | 不涉及 | Web 代码未由本次修改；部署时未重建 Web |
| AIDA CLI | 不涉及 | CLI 代码和产物未修改 |
| PostgreSQL migration | 不涉及 | 无 migration |
| 历史数据回填 | 不涉及 | 无回填 |
| 数据清理/删除 | 不涉及 | 无删除 |
| Report Skill | 仅核对 | 继续使用 `100866/aida-report@1.0.50`，本次未发布 Skill |
| MCP/默认 Agent | 仅核对 | MCP 和 Agent 配置未修改；真实 Run 验证单次读取和写回 |
| MinIO/对象存储 | 不涉及 | 对象存储契约未修改 |
| Digest/报告链路 | 发布 | 只修改 Digest 到 Report Context 的 Projection 与 Run 错误展示 |
| 监控/配置/文档 | 发布 | 同步需求、架构、开发、测试和发布记录；运行配置未修改 |

## 3. 版本与代码证据

- 隔离 worktree：`/home/intellif/dev/project_manager-worktrees/report-projection-final-review`。
- 分支：`fix/report-projection-final-review`；已快进包含当前 `main@a5fd7d5` 的 API 图片接口和前端改动。
- 最终 API 镜像：`project_manager-api:test-report-projection-final-review-r4-20260725`，镜像 ID 如发布元数据所列。
- 即时回退镜像：`project_manager-api:rollback-before-report-projection-final-review-r4-20260725`，镜像 ID `sha256:034c73f9900631fdcba32307d7b46cf4381b73486620562b0b17f86beb4256b2`。
- 本次代码尚未提交、推送或合并；生产环境未修改。

## 4. 变更项清单

| 变更项 | 文件/提交/版本 | 状态 | 验证命令或运行证据 | 影响范围 |
| --- | --- | --- | --- | --- |
| Projection 失败不回退原始 Digest | `api/internal/reportcontext/projection.go` | 已部署 | 不完整 Projection 回归 | Report Context |
| 围栏与业务事实完整性 | `projection.go`、`service_test.go` | 已部署 | 已知、未知、文本和未闭合围栏测试 | Report Context |
| Git 元数据降噪 | 同上 | 已部署 | 中文冒号、换行、提交 SHA、分支名和业务同句回归 | Report Context 内容 |
| Agent 错误归一 | `managed_agent_run_status_syncer.go`、测试 | 已部署 | GLM 500、超时、缺失写回回归及真实 Run | 用户错误展示 |
| 设计与测试文档 | 第八阶段四份文档、Presentation Profile 三份文档 | 已更新 | 口径检索、`git diff --check` | 开发与验收基线 |
| 历史发布记录格式 | 4 份既有测试发布记录 | 已更新 | 固定九章节检查 | 发布文档 |

## 5. 发布前检查

- [x] 分支、worktree、Git 基线和未提交状态已记录。
- [x] `cd api && go test ./... -count=1`、`go vet ./...`、`git diff --check` 全部通过。
- [x] Web、CLI、migration、回填、数据删除不涉及。
- [x] 当前测试 API 镜像在每次替换前均保留回退标签。
- [x] Skill owner/version、MCP 和 Agent 配置未改变。
- [x] 使用测试账号 t03 和测试服数据，未操作生产。
- [x] 停止条件：API 健康异常、Projection 丢失业务结果、重复 Agent Session 或错误泄露底层模型信息。

## 6. 执行步骤

| 步骤 | 执行主机 | 操作/命令 | 预期结果 | 实际结果 |
| --- | --- | --- | --- | --- |
| 1 | 157 worktree | 合入当前 `main`，运行 API 全量测试与 vet | 包含最新 API 修复且测试通过 | 通过 |
| 2 | 157 测试服 | 构建临时测试镜像并只重建 API | DB、MinIO、Web 不重启 | 完成 |
| 3 | 14.157 测试服 | 创建团队日报 Run | 验证 Context、单外部 Session和错误归一 | Context 6,871 bytes；平台长尾后映射为友好 timeout |
| 4 | 14.157 测试服 | 创建指定 Session Slice 的个人日报 Run | 验证 Projection 和真实写回 | Context 48,969 bytes、28 条事实；约 416 秒成功写回 |
| 5 | 157 worktree | 按真实结果补提交 SHA/分支格式回归并重测 | Git 元数据删除、业务结果保留 | 通过 |
| 6 | 157 测试服 | 构建并替换最终 r4 API 镜像 | 健康且可回退 | 完成，`/health` 返回 `{"status":"ok"}` |

## 7. 发布后验收

| 验收类别 | 用例 | 账号/数据 | 预期结果 | 实际结果 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 自动化 | API 全量、vet、diff | 最终 worktree | 全部通过 | 通过 | 非缓存 `go test ./... -count=1`、`go vet ./...`、`git diff --check` |
| API/接口 | 健康检查 | 最终 r4 镜像 | HTTP 200 | 通过 | `{"status":"ok"}` |
| Digest/报告 | Digest 到 Projection | t03 Session Slice `6c8fa614-b280-417d-a7cb-4109eaab6393` | 不回退原始 Digest、Context 可控 | Digest 1,337,003 bytes；Context 48,969 bytes、28 条事实 | Run `3cd0face-868a-4269-85da-cd0701e16c87` |
| Report Skill/MCP/Agent | 个人日报真实写回 | t03 | 单 Session、单 Context 读取、单写回 | `succeeded`，约 416 秒 | Agent Session `612880d2-5411-4167-b484-6d9a9e147cd0` |
| Report Skill/MCP/Agent | 团队日报模型长尾 | t03 | 底层错误不对用户暴露 | 顶层 `timeout`，错误为“报告生成超时，请稍后重试” | Run `2c41c67b-05a8-40ef-af29-009937346678` |
| Report Skill/MCP/Agent | Agent 完成但缺失写回 | t03 | 稳定失败文案且保留内部错误码 | 真实 Run 复现；最终 r4 回归通过，错误码为 `REPORT_WRITEBACK_MISSING` | Run `1e8df778-f668-46ad-9b4a-e6377d7f3ab8` |
| Git 降噪 | 真实格式回归 | 成功 Run 暴露的三种模式 | 只删 Git 元数据，保留业务结果 | 最终 r4 单元回归通过 | `service_test.go` |
| AIDA CLI | 不涉及 | - | - | 不涉及 | CLI 未修改 |
| Token/Session | 回归边界 | 现有链路 | 不受影响 | 代码未修改；未执行单独业务验收 | 变更范围核对 |
| Web 浏览器 | 不涉及 | - | - | 不涉及 | Web 未重建 |

## 8. 回滚与停止条件

- API：将 `project_manager-api:rollback-before-report-projection-final-review-r4-20260725` 标记为 `project_manager-api:latest`，在主目录只强制重建 API，并检查 `/health`。
- Web、CLI、Skill/MCP/Agent、migration、回填和数据清理：本次未修改，无回滚动作。
- API 持续 5xx、业务事实误删、同一 Run 创建多个 Agent Session、Report Context 回退原始 Digest或用户错误泄露底层模型信息时立即停止。

## 9. 最终结果

```text
发布范围已完整列出：是
发布项执行结果已记录：是
发布后验收已完成：是
阻断项数量：0
最终状态：已发布
```
