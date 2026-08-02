# Project Memory 系统资产闭环测试服发布

## 1. 发布元数据

| 字段 | 内容 |
| --- | --- |
| 发布编号 | `20260802-02-project-memory-system-assets` |
| 目标环境 | AIDA 测试服（14.157） |
| Git 基线 | `main@b40691e` + 当前未提交 Project Memory 相关工作区 |
| API 镜像 | `sha256:8e439abd2f3b3affb831b9e918181eae1ae99c3dd0cfd68cf8553b9335666925` |
| 数据库迁移 | 无新增；继续使用 migration 036 |
| Web/CLI | 不涉及、未重启 |

## 2. 变更范围

- 将旧的 JSON payload 直传 Resolver 改为专用 Agent + Skill + MCP。
- 新增 `/api/v1/mcp/project-memory`，只暴露 `get_project_memory_context` 与 `write_project_memory_result`。
- 新增绑定实际 Aida 用户与单个 Memory Job 的短期 Token 和 Credential。
- Agent Session 使用系统 owner 的模型额度；Project Memory 数据仍按实际 Aida `user_id` 隔离。
- 新增可重复的 `cmd/project-memory-assets` 系统资产配置命令。
- Resolver 版本升级为 `project-memory-resolver/v4`。

## 3. 测试系统资源

| 资源 | 值 |
| --- | --- |
| owner | `100866` |
| Agent | `aida-project-memory-system-test-v1`，managed v1 |
| Skill | `aida-project-memory@project-memory-v1` |
| Skill ID / SHA256 | `1d1c764f-8c59-4a36-8ca7-b6d6551c2042` / `da0d326ea73e0b4f238855ab3590dc1148ef8769054f6c5bdcf7d1f62ee747f8` |
| MCP | `aida-project-memory-mcp@project-memory-v1` |
| MCP Entry ID | `e0911fd6-5789-4b3a-adec-7e1f5e1f6853` |
| 模型 | `deepseek-v4-flash` |

旧测试 Agent `aida-project-memory-resolver-test` 已归档，不再被运行配置引用。

## 4. 验收结果

- `cd api && go test ./...`：通过。
- API 健康检查：`{"status":"ok"}`。
- 用户 305、2026-08-04 的真实 Nightly Job：成功。
- Snapshot：`bfc0adad-5e68-4b36-a0c4-dabefdf8f9bf`。
- Agent Session：`9bc83bb9-4403-4704-a031-561752782c19`。
- Session 轨迹：加载 `aida-project-memory` Skill；调用 `get_project_memory_context`；调用 `write_project_memory_result`。
- 输入估算 2044 tokens；输出估算 310 tokens。
- 用户 305 的 AIDA Agent 列表中 Project Memory Agent 数量为 0。
- DB、Web、MinIO 容器未重启；仅 API 被替换。

## 5. 生产发布约束

- 生产系统 owner 固定为 `10086`。
- 生产必须创建独立 Agent、Skill、MCP，不得引用测试 owner `100866`。
- 生产资产未登记并完成最小真实 Job 验收前，`PROJECT_MEMORY_NIGHTLY_ENABLED=false`。
- 回退时关闭夜间开关并恢复上一 API 镜像；最近成功 Snapshot 继续可读，日报主链路不受影响。

## 6. 当前结论

专用系统资产闭环已在测试服通过。30～50 个用户日、每组 3 次重复与人工盲评尚未执行，因此当前结论是“可进入扩大评测”，不是“可直接发布生产”。
