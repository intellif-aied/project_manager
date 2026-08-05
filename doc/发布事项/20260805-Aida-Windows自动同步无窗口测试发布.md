# Aida Windows 自动同步无窗口测试发布

- 日期：2026-08-05 01:31 UTC
- 环境：测试服 `192.168.14.157`
- 源码版本：`main` / `2d61795` 加当前 Windows 无窗口启动未提交改动
- 改动范围：Windows 自动同步改为无窗口启动，后台子进程禁止创建控制台窗口
- 部署组件：仅 AIDA CLI `0.1.27-test.20260805.1`；未部署或重启 API、Web、数据库、MinIO 服务
- 构建验证：`go test ./... -count=1`、`go vet ./...`、Windows amd64 与 Darwin arm64 交叉编译通过
- 发布验证：测试发布包七个文件 SHA256 回下载校验通过；Linux 产物输出 `aida 0.1.27-test.20260805.1`；版本文件返回 `0.1.27-test.20260805.1`
- 版本策略：测试版本使用 `-test.<YYYYMMDD>.<序号>` 后缀，根目录正式候选版本仍为 `0.1.27`
- 回滚产物：`/tmp/aida-test-release-backup-0.1.27.6fQ6fI`；更早的 `0.1.26` 回滚包位于 `/tmp/aida-test-release-backup-0.1.26.5wZV3G`
- 提交与推送：本轮无窗口启动改动尚未提交、未推送
- 生产环境：未涉及
