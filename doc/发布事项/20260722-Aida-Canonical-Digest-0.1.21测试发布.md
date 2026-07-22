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
- Report Run UUID 浏览器兼容修复提交：`a5f8d0c`；
- CLI 版本：`0.1.21`；
- 部署组件：API、Web、测试 CLI；
- 未重启 DB、MinIO，未涉及生产环境。

## 2. 构建与发布结果

- `cd api && go test -count=1 ./...`：通过；
- `cd daemon && go test -count=1 ./...`：通过；
- `cd daemon && go vet ./...`：通过；
- 最终 API 镜像：`sha256:941ba34a9c143716710c5f8d4e31193df7bf409a82fcc5dda3324b48802513ef`；
- API `/health`：通过；
- Web 工作流测试、typecheck 和生产 build：通过；全局 lint 被既有 HelpCenter `react-hooks/set-state-in-effect` 错误阻断，未顺带修改；
- 最终 Web 镜像：`sha256:1d25c32fa53e684c6d97dcbbf07ea25c44f7db769f1257c8b027b036757491b0`；
- 测试 Web `http://192.168.14.157:13000/`：HTTP 200；线上 bundle 为 `index-DVzDRXfa.js`；
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
- 首轮发布只更新 API 和 CLI，漏部署了并发提交中已增加 `idempotency_key` 的 Web，造成新 API 与旧页面契约不兼容，页面点击稳定返回 `400 INVALID_IDEMPOTENCY_KEY`；补部署当前 `main` Web 后，线上 bundle 已包含该字段。
- 补部署后又发现页面在 HTTP IP 环境直接调用 `crypto.randomUUID()` 会报 `crypto.randomUUID is not a function`；已引入 `uuid@14.0.1`，两个 Report Run 入口统一改用 `uuidv4()`，工作流测试禁止业务代码再次直接依赖该浏览器 API。

## 4. 发布边界

- 当前提交尚未推送远端；
- 未发布生产环境；
- 未修改或清理测试数据库；
- 无关文件 `doc/测试服账号文档.md` 未纳入本次提交。
