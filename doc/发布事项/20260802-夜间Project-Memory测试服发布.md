# 夜间 Project Memory 测试服发布

## 1. 发布元数据

| 字段 | 内容 |
| --- | --- |
| 发布编号 | `20260802-01-nightly-project-memory` |
| 目标环境 | AIDA 测试服（14.157） |
| 发布负责人 | Codex |
| 发布目标 | 每晚为当天有日报的用户整理限量 Project Memory，并在后续系统日报中提供非证据历史项目提示 |
| Git 基线 | API：`main@b40691e` + 当前未提交工作区；Web：`main@b40691e`，本次未发布；CLI：不涉及 |
| 版本 | API image `sha256:1da9e87a742c8185912d2cd8a7ffd66e9a3925be3be0f4e33893669a1aa5865c`；Resolver `project-memory-resolver/v3`；Agent managed v3；migration 036 |
| 时间 | 2026-08-02 11:24～12:07，中国标准时间 |

## 2. 发布范围

| 范围 | 状态 | 内容 |
| --- | --- | --- |
| API/后台 | 发布 | 夜间 Job、Resolver Adapter、Snapshot、Historical Project Hint、入队钩子与配置 |
| Web | 不涉及 | 本次无前端文件和交互变更，容器未重启 |
| AIDA CLI | 不涉及 | Session 上传协议与 CLI 产物未修改 |
| PostgreSQL migration | 发布 | `034`、`035` 为同工作区前置；本次新增 `036_report_project_memory_nightly.sql`，测试库最高版本 36 |
| 历史数据回填 | 不涉及 | 仅用测试用户 305 的既有生产来源样本做回归，不执行批量回填 |
| 数据清理/删除 | 不涉及 | 未删除业务数据；仅清理一次性测试执行器文件 |
| Report Skill | 仅核对 | 测试服仍使用 `aida-report@1.1.26`，本次未发布新 Skill |
| MCP/默认 Agent | 发布/仅核对 | 新增测试专用 `aida-project-memory-resolver-test` managed v3；Report MCP 与默认 Report Agent 仅核对 |
| MinIO/对象存储 | 不涉及 | 无对象写入，MinIO 容器未重启 |
| Digest/报告链路 | 仅核对 | 同一 3 个生产来源 Session 切片完成 A/B 与最终报告写回 |
| 监控/配置/文档 | 发布 | 新增 3 个 `PROJECT_MEMORY_*` 配置；更新 doc/v3 方案、Context/README 与系统资源登记 |

## 3. 版本与代码证据

- 分支与基线：`main@b40691e`，`origin/main@b40691e`。
- 当前实现尚未 commit/push；发布镜像以本记录中的不可变 image ID 为准。
- API 容器：`bfebd7b5c943353aa63e78411b9638c6119c81fa957dc5e9ef96199812eaa795`。
- API 镜像：`sha256:1da9e87a742c8185912d2cd8a7ffd66e9a3925be3be0f4e33893669a1aa5865c`。
- 上一测试镜像：`sha256:a4a41371fa1b94e150fbe6780d03dc47ed927fe3e54a9939be4be6c1355b2201`。
- Memory Agent：owner `100866`，Agent ID `aida-project-memory-resolver-test`，managed v3，模型 `deepseek-v4-flash`，无 Skill/MCP/Credential。

## 4. 变更项清单

| 变更项 | 文件/提交/版本 | 状态 | 验证命令或运行证据 | 影响范围 |
| --- | --- | --- | --- | --- |
| 夜间队列与 Snapshot | `api/internal/reportmemory/*`、migration 036 | 已发布 | Resolver task `1a958d24-41d1-400e-83ff-0b9fff5fdaa6`；Snapshot `f70be967-8d6c-4ca5-acee-269f1f5cdc69` | 系统个人日报 |
| 日报变更入队 | `api/handler/report.go`、`report_mcp_write.go` | 已发布 | B Report 写回后对应用户日 Job 为 pending | 保存、提交、AI 写回 |
| 历史 Hint | `reportcontext/service.go`、`reportmemory/hints.go` | 已发布 | Run `6ea1d569-df08-4978-857c-a907a5dbe81d` Context 仅含最新成功 Snapshot 的 3 个项目 | 系统默认个人日报 |
| Resolver Agent | managed v3 | 已发布 | v3 Proposal 包含 IF-Knowledge 的 `儿童睡前卡通动画生成` 子主题别名 | 测试服夜间任务 |
| 质量评测分类 | `api/internal/reporteval/scorecard.go` | 已发布 | 全量 Go 测试通过 | 测试服评测 |
| 方案与资源文档 | doc/v3 20、CONTEXT、README、系统资源文档 | 已发布 | 文件存在且 `git diff --check` 通过 | 协作与发布治理 |

## 5. 发布前检查

- [x] 工作区状态、分支与 Git 基线已记录；当前为未提交测试版本，未伪装为 commit SHA。
- [x] API 全量 `go test ./...` 通过，`git diff --check` 通过。
- [x] migration 036 已在测试库真实执行；版本、表与真实写入均已对账。
- [x] 发布前 API、DB、Web、MinIO 容器状态已记录。
- [x] 本次为测试服加法 migration；未执行生产发布或生产数据库操作。
- [x] Memory Agent owner、Agent ID、managed version、模型和零资产绑定已核对。
- [x] 测试账号为用户 305；生产只读来源数据已映射到测试服，不使用生产 Token 写入。
- [x] Web、CLI、MinIO 不涉及项已完成容器与依赖检查。
- [x] 停止条件与回退边界已填写。

## 6. 执行步骤

| 步骤 | 执行主机 | 操作/命令 | 预期结果 | 实际结果 |
| --- | --- | --- | --- | --- |
| 1 | 14.157 | 检查 `git status --short --branch`、分支与最近提交 | 确认 main 基线并保留既有改动 | `main@b40691e`，相关未提交改动保留 |
| 2 | 14.157 | `go test ./...`、`git diff --check` | 自动化通过 | 全部通过 |
| 3 | 测试 Agent 平台 | 创建并迭代 Memory Resolver | 无 Skill/MCP，输出 Proposal JSON | `aida-project-memory-resolver-test` managed v3 |
| 4 | 14.157 | `docker compose up -d --build --no-deps api` | 仅替换 API | API 已替换；DB/Web/MinIO container ID 均未变化 |
| 5 | 14.157 | `/health` 与 migration 对账 | HTTP 200、版本 36 | `{"status":"ok"}`、`max(version)=36` |
| 6 | 测试账号 305 | 夜间任务 + 同源 Report A/B | Snapshot 成功，B 改善父子归并 | Snapshot 与 B Report 均成功 |

## 7. 发布后验收

| 验收类别 | 用例 | 账号/数据 | 预期结果 | 实际结果 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 自动化 | API 全量测试 | 当前工作区 | 全部通过 | 通过 | `cd api && go test ./...` |
| API/接口 | 健康检查 | 测试服 | HTTP 200 | `{"status":"ok"}` | `:18090/health` |
| AIDA CLI | 不涉及 | 不涉及 | 不改变 | 未发布 CLI | 容器与源码范围 |
| Report Skill/MCP/Agent | v3 Proposal | 用户 305 的 2026-08-01 日报 | 3 个父项目、保留 Brief 父子关系 | 成功 | task `1a958d24-41d1-400e-83ff-0b9fff5fdaa6` |
| Digest/报告 | 同 3 切片 A/B | 3 个生产来源 Session 切片 | 子场景不再上浮 Summary | A 为 4 项；B 为 3 项且子场景归入 IF-Knowledge | A Report `3bfda930-eaac-4640-959d-5b7a91a95bed`；B Report `805bb8e1-0cbf-4cac-bc56-178e62cc79d7` |
| Token/Session | Memory 输入预算 | Snapshot v3 | 小于 8000 | 输入估算 1911、输出估算 426 | Snapshot `f70be967-8d6c-4ca5-acee-269f1f5cdc69` |
| Web 浏览器 | 不涉及 | 不涉及 | Web 不重启 | Web container ID 未变化 | 发布命令输出 |

## 8. 回滚与停止条件

- API：关闭 `PROJECT_MEMORY_NIGHTLY_ENABLED` 并恢复镜像 `sha256:a4a41371fa1b94e150fbe6780d03dc47ed927fe3e54a9939be4be6c1355b2201`；日报会自动回退旧 continuity Context。
- Web、CLI：本次未发布，无回滚动作。
- Skill/MCP/Agent：停止夜间开关即可隔离 Resolver；Agent v3 不被 Report Agent 直接引用。
- migration：036 为加法结构，测试服不做 DROP/TRUNCATE；问题使用 forward fix，关闭开关后表保持静默。
- 数据回填与清理：未执行，无回滚动作。
- 立即停止条件：API 持续 5xx、Report Agent 生成成功率下降、历史事实进入当天成果、错误项目合并、DB 锁等待异常或 Snapshot 越权。

## 9. 最终结果

```text
发布范围已完整列出：是
发布项执行结果已记录：是
发布后验收已完成：是（代表性真实数据；30～50 用户日扩大评测未执行）
阻断项数量：0
最终状态：已发布（测试服）
```
