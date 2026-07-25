# 20260725 Report Projection 成果检查点测试发布

## 1. 发布元数据

| 字段 | 内容 |
| --- | --- |
| 发布编号 | `20260725-01-report-projection-checkpoint` |
| 目标环境 | 14.157 测试服 |
| 发布负责人 | 未记录 |
| 发布目标 | 验证成果检查点 Projection、完整 Context 单次读取与报告写回 |
| Git 基线 | API `94a058f`；Web、CLI 不涉及 |
| 版本 | API 镜像 `sha256:0cf25ae6ea87dc58acaa7c19571cce41bd007913fd9763d60992e36f49f4b461`；Skill、MCP 未变更 |
| 时间 | 2026-07-25；开始、结束时刻未记录；时区未记录 |

## 2. 发布范围

| 范围 | 状态 | 内容与核对依据 |
| --- | --- | --- |
| API/后台 | 发布 | Report Context Projection 成果检查点与结构去噪 |
| Web | 不涉及 | 前端契约未修改 |
| AIDA CLI | 不涉及 | CLI 未修改 |
| PostgreSQL migration | 不涉及 | 无 migration |
| 历史数据回填 | 不涉及 | 无回填 |
| 数据清理/删除 | 不涉及 | 无删除 |
| Report Skill | 不涉及 | Skill 未修改 |
| MCP/默认 Agent | 仅核对 | 版本不变，验证单次 Context 读取和写回 |
| MinIO/对象存储 | 不涉及 | 存储未修改 |
| Digest/报告链路 | 发布 | 只修改 Digest 到 Context 的 Projection |
| 监控/配置/文档 | 仅核对 | 无配置变更，记录测试结果 |

## 3. 版本与代码证据

- 分支：`fix/report-context-readable-facts`。
- Git 版本：`94a058f`。
- API 镜像 ID 如发布元数据所列。
- 回退镜像：`project_manager-api:rollback-before-complete-projection-20260725`。
- 生产环境未修改。

## 4. 变更项清单

| 变更项 | 文件/提交/版本 | 状态 | 验证命令或运行证据 | 影响范围 |
| --- | --- | --- | --- | --- |
| 成果检查点选择 | `94a058f` | 已部署 | 自动化与离线重放 | Context 内容 |
| 结构性噪声排除 | `94a058f` | 已部署 | 专项测试 | Context 大小 |
| 有效结果不按字符截断 | `94a058f` | 已部署 | 大样本对比 | 事实完整性 |

## 5. 发布前检查

- [x] 发布 SHA 与镜像 ID 已冻结。
- [x] API 测试、vet 与 diff 检查通过；Web、CLI 不涉及。
- [x] migration、回填和数据删除不涉及。
- [x] API 回退镜像已记录。
- [x] Skill、MCP、模型和 Agent 配置未修改。
- [x] 使用测试账号 t02，未操作生产。
- [x] 停止条件：有效业务结果被截断、Context 工具超限或报告无法写回时停止。

## 6. 执行步骤

| 步骤 | 执行主机 | 操作/命令 | 预期结果 | 实际结果 |
| --- | --- | --- | --- | --- |
| 1 | 14.157 测试服 | 构建并只替换 API | 其他服务不重启 | 完成 |
| 2 | 14.157 测试服 | API 健康检查 | HTTP 200 | 返回 `{"status":"ok"}` |
| 3 | 14.157 测试服 | t02 创建个人日报 Run | Context 单次读取、报告写回 | 成功 |

## 7. 发布后验收

| 验收类别 | 用例 | 账号/数据 | 预期结果 | 实际结果 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 自动化 | API、vet、diff | 测试代码 | 全部通过 | 通过 | `go test ./...`、`go vet ./...`、`git diff --check` |
| Digest/报告 | 离线重放 | 当前 Session、37 来源样本 | 有效结果不截断 | 通过；105 个事实、102,013 bytes | 离线指标 |
| Report Skill/MCP/Agent | t02 真实报告 | Run `7ae42c2c-6cb9-4950-b959-4ad511988fb4` | 单次读取和写回 | `succeeded`，约 524 秒 | Agent Session `99f8d50e-7181-4828-b5d0-68d560d5af0e`，Report `c834e07f-9334-4671-93d5-1492b095bc26` |
| API/接口 | MCP Context | 同上 | HTTP 200 且不超限 | 110,279 bytes，约 9.5 秒 | 冻结 Context 105,604 bytes |
| AIDA CLI | 不涉及 | - | - | 不涉及 | CLI 未修改 |
| Token/Session | 回归边界 | 现有链路 | 不受影响 | 代码未修改；未记录单独接口验收 | 变更范围核对 |
| Web 浏览器 | 不涉及 | - | - | 不涉及 | Web 未修改 |

已识别遗留：最终正文仍含少量提交 SHA、Git 命令和 worktree 描述。本次未追加字符串补丁、未修改 Skill、未为此重复调用模型；后续由 Projection 内容质量修复处理。

## 8. 回滚与停止条件

- API：将 `project_manager-api:rollback-before-complete-projection-20260725` 重新标记为 `project_manager-api:latest`，然后只重建 API 并检查 `/health`。
- Web、CLI、Skill/MCP/Agent、migration、回填和数据清理：不涉及。
- 有效业务结果被截断、Context 工具超限或报告无法写回时立即停止。

## 9. 最终结果

```text
发布范围已完整列出：是
发布项执行结果已记录：是
发布后验收已完成：是
阻断项数量：0
最终状态：已发布
```
