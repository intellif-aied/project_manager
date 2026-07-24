# 20260724 Report Projection P0 与 Agent 超时归一测试发布

## 1. 发布元数据

| 字段 | 内容 |
| --- | --- |
| 发布编号 | `20260724-03-report-projection-p0` |
| 目标环境 | 14.157 测试服 |
| 发布负责人 | Codex（测试服执行） |
| 发布目标 | 合法 pending Digest 不再导致 Report Projection 失败，并将 Agent 平台约 10 分钟终止归一为用户可理解的报告超时 |
| Git 基线 | API：`3a6c3f5`；Web：`3a6c3f5`（未发布）；AIDA CLI：`3a6c3f5`（未发布） |
| 版本 | API 镜像 `sha256:bb8436d15590c21c6eb0060b0299be2a44ea182ae174eb952b74146b9ea7cc38`；Report Skill `100866/aida-report@1.0.48`；MCP `aida-report-mcp@report-v1`；Web/CLI 不涉及 |
| 时间 | 2026-07-24 11:22～11:31 UTC |

## 2. 发布范围

| 范围 | 状态 | 内容 |
| --- | --- | --- |
| API/后台 | 发布 | Projection 合法 pending 兼容、Projection 不安全时回退冻结 Digest、Agent 超时和基础设施故障错误归一 |
| Web | 仅核对 | 容器未重建、未重启；继续使用现有 Run 状态和错误字段 |
| AIDA CLI | 不涉及 | 本次无 CLI 文件、接口或产物变化 |
| PostgreSQL migration | 不涉及 | 无 migration 和表结构变化 |
| 历史数据回填 | 不涉及 | 无回填命令和数据写入 |
| 数据清理/删除 | 不涉及 | 未删除或清理数据 |
| Report Skill | 发布 | `100866/aida-report@1.0.48`，Skill ID `32a9108b-9213-4d3a-bc9c-25b9b5a44677`，Registry SHA256 `dcc25478435f6d9a37185a7f8f6842c774289c86704c03bea32ca43145b52250` |
| MCP/默认 Agent | 仅核对 | MCP 版本不变；真实 Session 加载 Skill `1.0.48` 并一次调用 `get_report_context` |
| MinIO/对象存储 | 仅核对 | 容器未重启；本次无对象格式、路径和权限变更 |
| Digest/报告链路 | 发布 | 37/37 Digest 来源冻结，Context 构建成功；最终写回因 Agent 平台执行时限未通过 |
| 监控/配置/文档 | 发布 | API 配置切换到 Skill `1.0.48`；更新阶段状态、资源登记和本发布记录 |

## 3. 版本与代码证据

| 项目 | 证据 |
| --- | --- |
| 主分支 | `main@3a6c3f5`，包含 `06911be`、`ea414ba`、`79c6106` |
| API 新镜像 | `sha256:bb8436d15590c21c6eb0060b0299be2a44ea182ae174eb952b74146b9ea7cc38` |
| API 回滚镜像 | `project_manager-api:rollback-before-agent-timeout-20260724` → `sha256:9724eb9893020e1c6ec03c59f57bc0017665b5aa2cb7f071a614f22eda51a348` |
| Skill 正文 | 生成正文 SHA256 `50c0c4d5d3baaf90354bc8637b6e218889ca63ff52af639b897ad4ad58e2c672`，Registry 下载后逐字节一致 |
| Skill Registry | `100866/aida-report@1.0.48`，Skill ID `32a9108b-9213-4d3a-bc9c-25b9b5a44677`，Registry SHA256 `dcc25478435f6d9a37185a7f8f6842c774289c86704c03bea32ca43145b52250` |
| 真实验收 Run | Run `ec2b7608-c1b1-40bb-ae9b-c14dfa79bf05`；Selection `5a22eb0d-fa65-4e55-a4e3-cd88cdea66c9`；Session `4ea627c0-9149-48bc-bd91-943ad3109d73` |

## 4. 变更项清单

| 变更项 | 文件/提交/版本 | 状态 | 验证命令或运行证据 | 影响范围 |
| --- | --- | --- | --- | --- |
| 保留合法 pending 事实 | `api/internal/reportcontext/*`，`06911be` | 完成 | 定向测试、全量 Go 测试、真实 Context 中 12 条 pending 保留 | 新建报告 Run |
| Projection 安全回退 | `api/internal/reportcontext/*`，`ea414ba` | 完成 | 不安全 Projection 回退单元测试；真实 Run 未触发回退 | 新建报告 Run |
| Agent 超时归一 | `api/service/managed_agent*.go`，`79c6106` | 完成 | 596 秒样本和两条 SQL 状态同步回归测试 | 报告 Agent Run 状态同步 |
| Skill 发布 | `100866/aida-report@1.0.48` | 完成 | Registry 正文逐字节核对，真实 Session SkillRef | 测试服新报告 Run |
| 文档同步 | 本发布记录及第八阶段文档 | 完成 | `git diff --check` 和版本残留检索 | 开发与发布记录 |

## 5. 发布前检查

- [x] 工作区状态和发布 SHA 已冻结；代码合并前 `main` 干净；
- [x] API `go test ./...`、`go vet ./...` 和 `git diff --check` 通过；Web/CLI 未变更；
- [x] PostgreSQL migration 不涉及，依据为本次差异无 migration 文件；
- [x] 测试服现有 API 镜像、容器和 Skill 配置已记录；
- [x] API 回滚镜像已标记；数据库和配置无结构性变更；
- [x] Skill owner、version、Skill ID、正文和 Registry 哈希已核对；MCP 版本不变；
- [x] 使用测试服账号和测试数据，未操作生产；
- [x] Web、CLI、PostgreSQL、MinIO 的不涉及边界已核对；
- [x] 停止条件和各组件回滚方式已填写。

## 6. 执行步骤

| 步骤 | 执行主机 | 操作/命令 | 预期结果 | 实际结果 |
| --- | --- | --- | --- | --- |
| 1 | 14.157 | 合并 `fix/report-projection-pending` 到 `main` | 主分支包含 P0 和错误归一修复 | `main@3a6c3f5` |
| 2 | 14.157 | 标记当前 API 镜像为 `project_manager-api:rollback-before-agent-timeout-20260724` | 可一条命令恢复上一 API | 指向 `sha256:9724eb...` |
| 3 | 14.157 | `docker compose up -d --build --no-deps api` | 只重建并替换 API | API 使用 `sha256:bb8436d...`；Web、DB、MinIO 未重启 |
| 4 | 14.157 | `curl http://127.0.0.1:18090/health` | HTTP 200 | HTTP 200，`{"status":"ok"}` |
| 5 | 14.157 | 核对容器环境变量 | Skill 固定为 `1.0.48` | `MANAGED_AGENT_REPORT_SKILL_VERSION=1.0.48` |

## 7. 发布后验收

| 验收类别 | 用例 | 账号/数据 | 预期结果 | 实际结果 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 自动化 | API 全量测试和静态检查 | 仓库测试夹具 | 全部通过 | 通过 | `go test ./...`、`go vet ./...`、`git diff --check` |
| API/接口 | 健康检查 | 测试服 API | HTTP 200 | HTTP 200 | `/health` 返回 `{"status":"ok"}` |
| AIDA CLI | 不涉及 | 不涉及 | 不涉及 | 未执行 | 无 CLI 变更 |
| Report Skill/MCP/Agent | Skill 和 Context 读取 | Session `4ea627c0-...` | 加载 `1.0.48`，单次读取 Context | Skill 一次、MCP 一次、无重复询问 | Agent Session 事件 52 条 |
| Digest/报告 | pending、Projection 和 Context | Run `ec2b7608-...` | Context 构建成功且事实不丢失 | 37/37 来源，894,223 bytes，12 条 pending 保留；最终写回未发生 | Run、Selection 与 Context 记录 |
| Token/Session | 既有链路回归 | 自动化用例 | 无回归 | 通过 | API 全量测试 |
| Web 浏览器 | 现有 Run 状态展示 | 未执行新浏览器交互 | 前端无需改动 | 未执行；Web 未重启 | 容器已连续运行 44 小时以上 |
| 错误归一 | 10 分钟终止和提前故障 | 596 秒真实任务时间 + 测试夹具 | 超时与普通失败分开，内部错误不进入响应 JSON | 通过 | 两条 `managed_agent_run_status_syncer` 回归测试 |

## 8. 回滚与停止条件

| 范围 | 回滚方式 |
| --- | --- |
| API | 将 Compose API 镜像恢复为 `project_manager-api:rollback-before-agent-timeout-20260724` 并只重建 API；恢复后检查 `/health` |
| Web | 未发布，无回滚 |
| AIDA CLI | 未发布，无回滚 |
| Skill/MCP/Agent | 将测试服 `MANAGED_AGENT_REPORT_SKILL_VERSION` 恢复为 `1.0.47` 并只重启 API；MCP 未变化 |
| PostgreSQL migration | 不涉及，无回滚 |
| 历史数据回填 | 不涉及，无回滚 |
| 数据清理 | 不涉及，无恢复动作 |

停止条件：API 健康检查持续失败、旧上传或 Token 主链路回归、Context 无法构建、SkillRef 与配置不一致、用户响应再次出现平台内部错误时，立即停止后续测试并执行 API 回滚。真实 Agent 最终写回未通过，因此禁止据此进入生产发布。

## 9. 最终结果

```text
发布范围已完整列出：是
发布项执行结果已记录：是
发布后验收已完成：是（测试发布范围内；真实 Agent 最终写回未通过）
阻断项数量：0（测试服发布）；1（生产发布：真实 Agent 最终写回）
最终状态：已发布（测试服）
```
