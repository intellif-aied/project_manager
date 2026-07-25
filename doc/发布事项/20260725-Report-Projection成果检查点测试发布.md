# 20260725 Report Projection 成果检查点测试发布

## 1. 发布元数据

| 字段 | 内容 |
| --- | --- |
| 目标环境 | 14.157 测试服 |
| 源码分支 | `fix/report-context-readable-facts` |
| Git 版本 | `94a058f` |
| API 镜像 | `sha256:0cf25ae6ea87dc58acaa7c19571cce41bd007913fd9763d60992e36f49f4b461` |
| 回退镜像 | `project_manager-api:rollback-before-complete-projection-20260725` |
| 生产环境 | 未修改 |

## 2. 发布范围

- Report Context Projection 改为成果检查点选择，保留全部终态、全部未解决项和最新可报告非终态。
- 结构性排除代码围栏、纯命令、纯路径、纯 Git 流水和内部交接。
- 有效 Agent 结果不设置单条字符上限，不按 240 字或 1000 字截断。
- Digest Revision、上传、Token 统计、数据库、Web、CLI、Skill 和 MCP 版本均未修改。

## 3. 自动化验证

- `cd api && go test ./...`：通过。
- `cd api && go vet ./...`：通过。
- `git diff --check`：通过。
- 当前真实 Session 离线重放：10 个 Highlight 生成 4 个事实，5,371 bytes。
- 37 来源大样本离线重放：1,105 个 Highlight 生成 105 个事实和 105 个 observations，102,013 bytes。
- 相比 1000 字实验版本，大样本只增加 699 bytes、341 个 Unicode 字符，确认主要收敛来自检查点选择而非单条截断。

## 4. t02 真实 Agent 验收

| 字段 | 内容 |
| --- | --- |
| 用户 | `304 / t02 / director` |
| 报告 | `personal_daily / 2026-07-24 / self` |
| Run ID | `7ae42c2c-6cb9-4950-b959-4ad511988fb4` |
| Selection ID | `ad416966-e19b-4dfc-ae3d-02723ecd0cca` |
| Agent Session | `99f8d50e-7181-4828-b5d0-68d560d5af0e` |
| Report ID | `c834e07f-9334-4671-93d5-1492b095bc26` |
| 模型 | `GLM-5.2` |
| 冻结 Context | 105,604 bytes |
| MCP Context 响应 | 110,279 bytes，约 9.5 秒，HTTP 200 |
| Run 时间 | `03:47:38Z` 创建，`03:56:22Z` 完成 |
| 结果 | `succeeded`，报告成功写回 |

实际链路为一次创建 Run、后台构建 Context、一次读取完整 Context、一次写回报告。页面没有参与推进；没有出现工具结果超限、临时文件分块读取或 `infrastructure_failure`。

最终报告正文 10,552 个 Unicode 字符，首个“工作总结”段落 335 字，共 9 个二级主题。摘要与正文继续按产品约定组合在现有 `content` 字段中，前端契约未修改。

## 5. 验收结论与遗留项

- 链路稳定性：通过。
- 有效结果不按单条字符数截断：通过。
- 大 Context 单次 MCP 读取：通过。
- 最终报告写回及 Run 关联：通过。
- Git 过程噪声：未完全通过。正文仍包含 4 个提交 SHA、1 条 Git 命令和 1 处 worktree 描述；这些信息嵌在有效成果段落中，现有纯 Git 和尾句过滤未全部剥离。

Git 噪声不影响本次链路稳定性发布，但必须作为后续 Projection 内容质量修正项处理。本次不继续增加临时字符串补丁，不修改 Skill，不重新运行第二个 Agent。

## 6. 部署与回退

- 仅从独立 worktree 构建并替换测试服 API。
- API 健康检查返回 `{"status":"ok"}`。
- DB、MinIO、Web 和 CLI 未重启或发布。
- 回退时将 `project_manager-api:rollback-before-complete-projection-20260725` 重新标记为 `project_manager-api:latest`，然后在主目录执行 `docker compose up -d --no-deps api` 并检查 `/health`。
