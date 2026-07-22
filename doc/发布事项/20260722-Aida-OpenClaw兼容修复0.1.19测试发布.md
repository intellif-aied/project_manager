# Aida OpenClaw 兼容修复 0.1.19 测试发布记录

> 日期：2026-07-22
>
> 环境：14.157 测试服
>
> 状态：测试分发部署完成

## 1. 部署范围

- Aida CLI：`0.1.19`；
- OpenClaw transcript 按消息结构宽松识别，兼容当前真实事件名称；
- 保留导出路径、Session 身份、文件大小、事件身份和时间戳安全校验；
- 部署 canonical Session prepare API，并执行测试库 migration `026`；
- Web、DB 容器、MinIO 容器和生产环境不涉及。

## 2. 源码与检查

| 项目 | 结果 |
| --- | --- |
| 构建目录 | `/home/intellif/dev/project_manager_worktrees/p0-multiclient-picker-20260722` |
| Git 基线 | `81b5715` + 本次 worktree 中已列出的 CLI 修改 |
| 目标版本 | `0.1.19` |
| OpenClaw 真实 Materialize | 通过；使用此前失败的同一 Session ref，不执行上传 |
| `go test ./... -count=1` | 通过 |
| `go vet ./...` | 通过 |
| OpenClaw Adapter race | 通过 |
| API `go test ./... -count=1` | 通过 |
| 多平台构建和包内 SHA256 | 通过 |

## 3. 测试分发结果

| 项目 | 结果 |
| --- | --- |
| 发布地址 | `http://192.168.14.157:9000/statics-live/aida` |
| 发布前版本 | `0.1.18` |
| 发布后版本 | `0.1.19` |
| 下载复验目录 | `/home/intellif/aida-release-verifications/20260722-0.1.19-Z8qEaf` |
| 下载 SHA256 | Linux、macOS、Windows、两个安装脚本和版本文件全部通过 |
| 下载 Linux 版本 | `aida 0.1.19` |
| 安装脚本地址 | 测试 CLI 和测试 API 地址正确，不含生产地址 |

上传时先发布三个二进制、两个安装脚本和 `SHA256SUMS.txt`。从测试 HTTP 地址验证通过后，最后将 `aida-latest.txt` 从 `0.1.18` 切换到 `0.1.19`，随后再次执行完整下载校验。

## 4. 服务状态

- API 镜像由 `sha256:13661cd2...` 更新为 `sha256:1a96ffaf...`，只重建 `api` 容器；
- 测试库 migration 从 `025` 更新到 `026`，新增字段和约束已确认；
- `/health` 返回成功；
- 带有效认证的 canonical prepare 空请求返回 `400`，不再返回 `404`；
- `web`、`db`、`minio` 容器 ID 未变化且保持运行；
- 未安装或替换服务器账号已有的 Aida；
- 未提交、推送或发布生产环境。
