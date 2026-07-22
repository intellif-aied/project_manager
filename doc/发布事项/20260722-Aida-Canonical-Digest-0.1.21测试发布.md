# Aida Canonical Digest 0.1.21 测试发布记录

> 日期：2026-07-22
>
> 环境：14.157 测试服
>
> 状态：部署完成，OpenClaw 已验证至 Report Context；最终报告受外部模型配置阻断

## 1. 源码与范围

- 分支：`main`；
- Canonical Digest 修复提交：`aabab62`；
- Canonical Content Reader 一致性修复提交：`443717d`；
- Report Run Context SQL 类型修复提交：`066e9ef`、`52271d8`；
- CLI 版本：`0.1.21`；
- 部署组件：API、测试 CLI；
- 未部署 Web，未重启 DB、MinIO 或 Web，未涉及生产环境。

## 2. 构建与发布结果

- `cd api && go test -count=1 ./...`：通过；
- `cd daemon && go test -count=1 ./...`：通过；
- `cd daemon && go vet ./...`：通过；
- 最终 API 镜像：`sha256:941ba34a9c143716710c5f8d4e31193df7bf409a82fcc5dda3324b48802513ef`；
- API `/health`：通过；
- 测试 CLI 分发版本：`0.1.21`；
- 三个平台二进制、两个安装脚本、`SHA256SUMS.txt` 和 `aida-latest.txt`：远端下载校验全部通过；
- 最终复验目录：`/home/intellif/aida-release-verifications/20260722-0.1.21-final-Fhe5kf`。

## 3. 真实数据验收

- 14.157 测试账号 CLI 已从 `0.1.20` 更新到 `0.1.21`；
- OpenClaw `2026.6.33` 的 Session `openclaw-cc2547d867b424f73a6ce1691100aa90` 已重新上传成功；
- 首次真实上传暴露 Content Reader 使用通用 Parser 回读 Canonical 对象的问题；已增加真实 Parser 一致性回归测试并通过 `443717d` 修复；
- 修复后受控交互式重建完成：v2.10 Digest 为 `ready`，13 条可读事件中 4 条进入 Digest，形成 4 个 Atomic Work Unit；
- 测试句“这条消息是为了测试日报生成”进入第 3 个 Work Unit；
- Report Context 成功冻结，大小为 6153 字节，并确认包含测试句；
- 真实运行额外暴露 Report Run 从 `building_context` 转入 `submitting_agent` 时 PostgreSQL 无法推断 JSON 参数类型；已为 hash 和 bytes 增加显式类型并补充回归测试；
- 修复后 Run 成功进入 `agent_running`，但连续两次被 AIHub 外部模型配置阻断：`MiniMax-M2.5` 不存在或当前账号无权限；未将该独立问题归因于 Session/Digest。

## 4. 发布边界

- 当前提交尚未推送远端；
- 未发布生产环境；
- 未修改或清理测试数据库；
- 无关文件 `doc/测试服账号文档.md` 未纳入本次提交。
