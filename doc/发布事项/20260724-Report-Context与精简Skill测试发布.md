# 20260724 Report Context 与精简 Skill 测试发布

## 1. 发布元数据

| 字段 | 内容 |
| --- | --- |
| 发布编号 | `20260724-04-report-context-concise-skill` |
| 目标环境 | 14.157 测试服 |
| 发布负责人 | 未记录 |
| 发布目标 | 验证精简 Report Context 与精简 Skill 的单次读取和报告写回 |
| Git 基线 | API `main@815938c`；Web、CLI 不涉及 |
| 版本 | API 镜像 `sha256:44347c49fab97e189d251cc8f84286c934063ec44ab330cf3a3c929ee1bcaf7e`；Skill `100866/aida-report@1.0.50`；MCP `aida-report-mcp@report-v1` |
| 时间 | 2026-07-24；开始、结束时刻未记录；时区未记录 |

## 2. 发布范围

| 范围 | 状态 | 内容与核对依据 |
| --- | --- | --- |
| API/后台 | 发布 | Session+Digest Category Projection、冻结 Context 兼容、`source_ref` 校验、Git 轨迹剥离 |
| Web | 不涉及 | Web 契约未修改 |
| AIDA CLI | 不涉及 | CLI 未修改 |
| PostgreSQL migration | 不涉及 | 无 migration |
| 历史数据回填 | 不涉及 | 无回填 |
| 数据清理/删除 | 不涉及 | 无删除 |
| Report Skill | 发布 | `100866/aida-report@1.0.50`，从 `1.0.49` 派生 |
| MCP/默认 Agent | 仅核对 | MCP 版本不变，验证单次 Context 读取和写回 |
| MinIO/对象存储 | 不涉及 | 存储契约未修改 |
| Digest/报告链路 | 发布 | Context Projection 与历史冻结 Context 兼容 |
| 监控/配置/文档 | 发布 | 测试配置切换 Skill `1.0.50` |

## 3. 版本与代码证据

- API：`main@815938c`，镜像 ID 如发布元数据所列。
- Skill ID：`75a1a329-c5b2-4d4a-b9b9-9e7f263763d0`。
- Registry SHA256：`5e795b2bbc4fb4037cf35004007840f85ad00f976df38e6bbbd6e1012bd79ecd`。
- `SKILL.md` SHA256：`0c06a74ee84e7ed9aa15c4f71cd8ab4c15f6dcaada6dbeeac2c2264205f21222`。
- Registry 下载正文与 API 生成文件逐字节一致。

## 4. 变更项清单

| 变更项 | 文件/提交/版本 | 状态 | 验证命令或运行证据 | 影响范围 |
| --- | --- | --- | --- | --- |
| Report Context Projection | `815938c` | 已部署 | API 全量测试、真实 Run | 报告 Context |
| 精简 Skill | `1.0.50` | 已发布 | Registry hash、真实 Run | 默认报告 Agent |
| Git 尾句剥离增量 | `815938c` 工作树 | 已部署 | 自动化测试 | Context 内容质量 |

## 5. 发布前检查

- [x] 发布 SHA 和 API 镜像已记录。
- [x] API 自动化和脚本语法检查已通过；Web、CLI 不涉及。
- [x] migration、回填和删除不涉及。
- [x] API 回退镜像与 Skill 回退版本已记录。
- [x] Skill owner、version、hash 与 MCP 版本已核对。
- [x] 使用测试环境，未操作生产。
- [x] 停止条件：健康检查失败、Context 来源校验回归或 Skill 解析不唯一时停止。

## 6. 执行步骤

| 步骤 | 执行主机 | 操作/命令 | 预期结果 | 实际结果 |
| --- | --- | --- | --- | --- |
| 1 | 14.157 测试服 | 构建并只替换 API | DB、MinIO、Web 不重启 | 完成 |
| 2 | 14.157 测试服 | 发布 Skill `1.0.50` 并更新配置 | Registry 唯一可解析 | 完成 |
| 3 | 14.157 测试服 | API 健康检查 | HTTP 200 | 返回 `{"status":"ok"}` |
| 4 | 14.157 测试服 | 执行两次真实报告 Run | 验证 Context 与 Skill | 第一次超时；第二次成功写回 |

## 7. 发布后验收

| 验收类别 | 用例 | 账号/数据 | 预期结果 | 实际结果 | 证据 |
| --- | --- | --- | --- | --- | --- |
| 自动化 | API、脚本、diff | 测试代码 | 全部通过 | 通过 | `go test ./...`、`py_compile`、`git diff --check` |
| API/接口 | 健康检查 | 14.157 | HTTP 200 | 通过 | `/health` |
| Report Skill/MCP/Agent | 精简 Skill 与同一 Context | Run `31808223-26f5-403b-b4bf-87d995816467` | 单次加载、单次读取、单次写回 | 通过，约 401 秒完成 | Agent Session `f8e71b95-b062-4844-af07-70af1d154cf3` |
| Digest/报告 | Context 收敛 | Run `643d185f-bf8b-4da1-85d8-62a29d6c0bc6` | 消除完整回复膨胀 | Context 22,596 bytes；旧 Skill 下模型超时 | Agent Session `2618bf71-cc21-43be-9206-a6de97d8f565` |
| AIDA CLI | 不涉及 | - | - | 不涉及 | CLI 未修改 |
| Token/Session | 不涉及 | - | - | 不涉及 | 链路未修改 |
| Web 浏览器 | 不涉及 | - | - | 不涉及 | Web 未修改 |

## 8. 回滚与停止条件

- API：恢复 `project_manager-api:rollback-before-concise-skill-20260724`，只重建 API。
- Skill：配置恢复为 `1.0.49`，不可变 `1.0.50` 不删除、不覆盖。
- Web、CLI、migration、回填、数据清理：不涉及。
- API 健康异常、Context 来源校验回归或 Skill 解析不唯一时立即停止。

## 9. 最终结果

```text
发布范围已完整列出：是
发布项执行结果已记录：是
发布后验收已完成：是
阻断项数量：0
最终状态：已发布
```
