# 20260725 Report Projection 完整性修复测试发布

## 1. 发布元数据

| 字段 | 内容 |
| --- | --- |
| 发布编号 | `20260725-02-report-projection-completeness` |
| 目标环境 | 14.157 测试服 |
| 发布负责人 | 未记录 |
| 发布目标 | 验证 Report Projection 完整保留业务结果并显著缩小 Context |
| Git 基线 | API 基点 `65054c0`，发布时包含未提交修改；Web、CLI 不涉及 |
| 版本 | API 二进制 SHA256 `fce874f54a60baa87b1d5b50604fdc7e4877d171721a4c80913871a6a2b3d56b`；Skill、MCP 未变更 |
| 时间 | 2026-07-25；开始、结束时刻未记录；时区未记录 |

## 2. 发布范围

| 范围 | 状态 | 内容与核对依据 |
| --- | --- | --- |
| API/后台 | 发布 | Projection 结构去噪、围栏处理、Git 识别与完整性修复 |
| Web | 不涉及 | Web 未修改 |
| AIDA CLI | 不涉及 | CLI 未修改 |
| PostgreSQL migration | 不涉及 | 无 migration |
| 历史数据回填 | 不涉及 | 无回填 |
| 数据清理/删除 | 不涉及 | 无删除 |
| Report Skill | 不涉及 | Skill 未修改 |
| MCP/默认 Agent | 仅核对 | 接口未修改；真实 Run 核对单次执行 |
| MinIO/对象存储 | 不涉及 | 存储未修改 |
| Digest/报告链路 | 发布 | 只修改 Digest 到 Report Context 的 Projection |
| 监控/配置/文档 | 仅核对 | 无配置变更；记录测试结果 |

## 3. 版本与代码证据

- 分支：`fix/report-context-readable-facts`。
- Git 基点：`65054c0`；发布时的最终改动未提交，不能仅由该 SHA 重建。
- API 二进制 SHA256 如发布元数据所列。
- 生产环境未修改。

## 4. 变更项清单

| 变更项 | 文件/提交/版本 | 状态 | 验证命令或运行证据 | 影响范围 |
| --- | --- | --- | --- | --- |
| 取消少量成果检查点选择 | Projection 工作树修改 | 已部署 | 31 份 Context 离线对照 | Context 完整性 |
| 围栏与 Git 误删修复 | Projection 工作树修改 | 已部署 | 专项单测与真实 Run | Context 内容 |
| 技术标题整段丢弃修复 | Projection 工作树修改 | 已部署 | 全量测试 | Context 内容 |

## 5. 发布前检查

- [x] 发布分支、Git 基点和二进制 hash 已记录；发布时存在未提交修改已明确标注。
- [x] API 测试、vet 与 diff 检查通过；Web、CLI 不涉及。
- [x] migration、回填、数据删除不涉及。
- [x] 替换前二进制与回退镜像已保留。
- [x] Skill、MCP 和模型均未修改。
- [x] 使用测试环境，未操作生产。
- [x] 停止条件：完整性样本出现业务结果误删或 API 健康异常时停止。

## 6. 执行步骤

| 步骤 | 执行主机 | 操作/命令 | 预期结果 | 实际结果 |
| --- | --- | --- | --- | --- |
| 1 | 14.157 测试服 | 尝试 Docker 构建 | 获得新镜像 | 构建客户端停滞，旧 API 保持健康，停止构建客户端 |
| 2 | 14.157 测试服 | 编译静态 API 并通过 `docker cp` 替换 | 只更新 API | 完成，其他容器未重启 |
| 3 | 14.157 测试服 | 健康检查 | HTTP 200 | 返回 `{"status":"ok"}` |
| 4 | 14.157 测试服 | 执行两个真实 Run | 验证 Projection 与单飞执行 | 两个 Run 均因外部模型 500/503 失败，无重复执行 |

## 7. 发布后验收

| 验收类别 | 用例 | 账号/数据 | 预期结果 | 实际结果 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 自动化 | API、vet、diff | 测试代码 | 全部通过 | 通过 | `go test ./...`、`go vet ./...`、`git diff --check` |
| Digest/报告 | 离线完整性 | 15 个本机 Session、31 份冻结 Context | 有效结果不丢失 | 15/15、31/31 通过；8,626 条有效结果完整映射 | 记录指标 |
| Report Skill/MCP/Agent | t02/t03 真实 Run | Run `ba855153-14fe-4ca6-bd79-2de014c112de`、`1d0ede08-8878-4887-87ca-f9da2f213147` | 单次 Agent 执行 | 外部模型 503/500；无重复 Skill、Context 或写回 | 对应 Agent Session |
| API/接口 | 健康检查 | 14.157 | HTTP 200 | 通过 | `/health` |
| AIDA CLI | 不涉及 | - | - | 不涉及 | CLI 未修改 |
| Token/Session | 回归边界 | 现有链路 | 不受影响 | 代码未修改；未记录单独接口验收 | 变更范围核对 |
| Web 浏览器 | 不涉及 | - | - | 不涉及 | Web 未修改 |

## 8. 回滚与停止条件

- API：将 `/tmp/aida-api-before-balanced-projection-v2` 复制回 API 容器并只重启 API；或使用 `project_manager-api:rollback-before-balanced-projection-20260725` 强制重建 API。
- Web、CLI、Skill/MCP/Agent、migration、回填和数据清理：不涉及。
- 发现业务结果误删、API 健康异常或 Report 主链路回归时立即停止。

## 9. 最终结果

```text
发布范围已完整列出：是
发布项执行结果已记录：是
发布后验收已完成：是，Projection 验收通过；模型成功率不属于本轮验收条件
阻断项数量：0
最终状态：已发布
```
