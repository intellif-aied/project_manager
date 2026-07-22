# OpenClaw Token Session 可见性修复测试发布记录

> 日期：2026-07-22
>
> 环境：14.157 测试服
>
> 状态：API 和 Web 已部署，待人工页面验收

## 1. 源码与范围

- 构建基线：`main@ed1c148`；
- 隔离工作树：`/home/intellif/dev/project_manager_worktrees/token-unavailable-sessions-20260722`；
- 部署组件：API、Web；
- 不涉及：CLI、DB migration、MinIO、生产环境、`fea.0.0.2`。

## 2. 构建和验证

| 项目 | 结果 |
| --- | --- |
| API `go test ./... -count=1` | 通过 |
| API `go vet ./...` | 通过 |
| Token Analytics 独立数据库完整集成测试 | 通过；包含 18,030 条 usage facts |
| Web 完整测试 | 通过 |
| Web typecheck | 通过 |
| Web build | 通过 |
| Web lint | 被 `main` 已有 `HelpCenter.tsx:120` 错误阻断；本次未修改该文件 |

## 3. 部署结果

| 组件 | 新镜像 | 回退镜像 |
| --- | --- | --- |
| API | `sha256:6816ecaaf89dc4358ebbcc432973f8dcf5fd74044a160ebe8690fca9e838612b` | `sha256:a872fd4cb4a45026dc2d81ba1395cd68e08cf4fd69ba787130e4c88037de371b` |
| Web | `sha256:e8a4a010f9c1ca08f5c6f2c955a3e0f862ce47111b996a8fa16e017a3abfe6f0` | `sha256:5bc3766becee01ad70d2e3024019b417b28c637f090b90a55d86444cb9fb5399` |

- API `/health` 返回成功；
- Web `http://192.168.14.157:13000/` 返回 HTTP 200；
- 只依次重建 API 和 Web 容器；DB、MinIO 容器 ID 保持不变；
- 旧镜像已增加 `rollback-pre-token-unavailable-20260722` 回退标签。

## 4. 真实接口验证

通过 `13000` Web 代理创建新 Token Analytics 快照后，第一页真实返回：

- Session：`openclaw-cc2547d867b424f73a6ce1691100aa90`；
- `agent_type=openclaw`；
- `usage_status=unavailable`；
- Token 与成本字段均为 `null`。

原查询快照已超过 15 分钟有效期，按既有规则返回 HTTP 410；使用新快照验证通过。

## 5. 发布边界

- 未提交、未推送、未合并；
- 未发布 CLI 或生产环境；
- `fea.0.0.2` 保持 wait，未参与本次构建和部署。
