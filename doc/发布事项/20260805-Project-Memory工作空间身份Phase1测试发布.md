# Project Memory 工作空间身份 Phase 1 测试发布

- 日期：2026-08-05
- 环境：测试服 `192.168.14.157`
- 源码版本：`main` / `ea4697d` 加当前未提交的 Project Memory Workspace Identity Phase 1 改动
- 改动范围：客户端生成脱敏 Git Repository Key；API 保存 Repository Key；新增 Workspace、Identity Key 与 Evidence 影子表；夜间 Project Memory 输入阶段进行最佳努力影子物化
- 部署组件：API；未部署或重启 Web、数据库容器和 MinIO
- 数据结构：API 启动时成功应用 `037_report_workspace_identity_shadow.sql`
- API 验证：`go test ./... -count=1` 通过；API 容器运行正常；`/health` 返回 `{"status":"ok"}`
- CLI 验证：Daemon `go test ./... -count=1`、`go vet ./...` 通过；构建 `0.1.27-test.20260805.2` 三平台产物并完成本地 SHA256 校验
- CLI 分发：清理 MinIO 孤儿对象后完成；测试版本发现已切换为 `0.1.27-test.20260805.2`，七个文件回下载 SHA256 校验通过
- 完整 CLI 验证：使用隔离 HOME 运行新 Linux 二进制，真实 Codex Session 上传成功；Session 为 `available`、Generation 为 `active`，`repository_key` 已入库；未替换本机 AIDA
- 影子数据验证：真实报告 40 条 Source Item 归并为 13 个 Workspace、写入 40 条 CWD Evidence；第二次执行新增 Evidence 为 0
- Git/CWD 联合验证：测试日报 `e3bb8da7-d15e-44cf-a2bf-b363bf1fba81` 的 1 个真实 Session 物化为 1 个 Workspace，同时挂载 `cwd` 与 `git_repository` 两类 Identity Key，并写入两类 Evidence
- 测试数据：用户 305，报告 `311bc951-9b20-440e-8811-34534560be4b`，Source Selection `019a14e4-7356-4fc9-8d4e-ed6253e441dc`
- 临时清理：仅删除本轮在 MinIO 容器 `/tmp` 创建的两个临时发布目录；测试对象、数据库业务数据和服务未删除
- 提交与推送：未执行
- 生产环境：未涉及
