# AIDA-BUG-20260722-011：新增客户端上传未复用统一 Session 选择器

> 优先级：P0

> 影响版本：Aida CLI `0.1.17`

> 状态：测试分发 `0.1.19` 部署完成，待功能验收

## 1. 问题现象

用户执行：

```text
aida upload-client openclaw
```

CLI 只打印 opaque Session ref 和一条要求重新执行命令的提示，然后以退出码 `0` 结束：

```text
openclaw sessions:
  openclaw-<opaque-ref>  <summary>

Run: aida upload-client openclaw <session-ref>
```

OpenCode、Kimi Code 使用同一新增客户端入口，无显式 ref 时也是这套两段式操作。它与 `aida upload` 为 Claude Code/Codex 提供的搜索、分页、勾选和确认页面不一致，用户无法按已有操作习惯完成上传。

同时，`aida upload-client --help` 会被解析成不支持的客户端 `--help`，没有进入子命令帮助。

## 2. 已确认产品规则

- `aida upload-client <client>` 必须复用 `aida upload` 的 Session Picker，页面结构、搜索、分页、逐项选择、确认和取消操作保持一致；
- 选择完成后，Claude Code/Codex 继续走 legacy uploader，新增客户端继续走各自 Adapter 和 canonical uploader，不能因为统一页面而合并上传协议；
- OpenClaw 可能包含私人或非编码会话，继续禁止 `--all` 和页面内全选，但必须允许在统一页面逐项选择；
- 非交互调用继续允许显式传入 `session-ref`；
- 本修复不得改变 Claude Code/Codex 的扫描、上传、Token 和成本链路。

## 3. 根因

### 3.1 已验证代码事实

- `daemon/device_client.go` 的 `cmdUpload` 在无编号和 `--all` 时调用 `selectSessionsWithTUI` 或 `selectSessionsInteractively`；
- `daemon/multi_client_command.go` 的 `cmdUploadClient` 在无 ref 时调用 `printAdditionalSessions`，打印 ref 后直接 `return 0`；
- `daemon/internal/sessionadapter/adapter.go` 的 `Descriptor` 已包含 ref、时间、CWD、项目和摘要等选择页所需字段；
- `daemon/multi_client_command_test.go` 原先只验证 OpenClaw 拒绝 `--all`，未覆盖无参选择、帮助或选择后上传链路；
- 新增客户端方案将“现有 Session 选择器”排除在交付范围之外，把上传协议隔离错误扩大成了用户交互分叉。

### 3.2 根因结论

新增客户端完成了 Adapter、canonical materialize 和上传能力，但没有接入 CLI 的公共 Session 选择层。安全限制只应影响允许的选择动作，不应产生第二套页面或要求用户复制 opaque ref。

## 4. 最小修复决策

```text
对应需求：新增客户端与 Claude Code/Codex 使用相同选择页面和操作
当前代码事实：旧选择器输入为 SessionInfo；新增客户端发现结果为 sessionadapter.Descriptor
待解决问题：两种发现模型无法直接共享选择器
候选方案：重写新选择器 / 合并上传协议 / 增加 Descriptor 到选择视图的薄适配层
选择方案：薄适配层；选择后按 NativeSessionRef 映射回原 Descriptor
验证证据：公开 cmdUploadClient 测试、TUI/非 TTY 选择测试、daemon 全量回归、真实 OpenClaw 取消 smoke
是否已确认：是
```

选择器增加最小选项 `AllowSelectAll`。默认保持旧行为；OpenClaw 设置为 `false` 并显示逐项选择提示。该选项同时作用于 Bubble Tea TUI 和非 TTY 分页选择器。

## 5. 修改范围

- `daemon/multi_client_command.go`：无 ref 时进入公共选择器；Descriptor 与选择视图双向映射；补子命令帮助；
- `daemon/session_tui.go`：增加可配置的全选能力，并完整显示新增客户端名称；
- `daemon/session_pagination.go`：非 TTY 选择器应用相同全选限制；
- 对应 `*_test.go`：覆盖公开命令、TUI、非 TTY、帮助和旧全选回归。

不修改 API、数据库、Web、Adapter 内容解析、canonical 协议、Claude Code/Codex uploader 或生产服务。

## 6. 验收标准

1. `aida upload-client opencode`、`kimi_code`、`openclaw` 无 ref 时均进入现有 Session Picker；
2. 页面显示客户端、最近活动时间、项目/CWD、Session ref 和摘要；
3. 搜索、分页、Space/编号选择、Enter/`d` 完成和取消行为与 `aida upload` 一致；
4. OpenCode/Kimi Code 保持页面全选和 `--all`；
5. OpenClaw 页面不提供全选，输入 `a/all` 不选中任何 Session 并显示原因，`--all/-a` 继续返回退出码 `2`；
6. 取消或未选择时不调用 Materialize，不产生 canonical 文件和上传请求；
7. 显式 ref 上传行为保持兼容；
8. `aida upload-client --help` 和 `aida upload-client openclaw --help` 返回退出码 `0`，不执行鉴权、发现或上传；
9. `cd daemon && go test ./...`、`go vet ./...` 和构建通过；
10. 使用真实 OpenClaw 执行无 ref 命令，进入选择器后取消，确认没有上传。

## 7. 发布与关闭条件

- 修复必须发布为高于 `0.1.17` 的新 CLI 版本，不能覆盖已发布二进制；
- 本任务只准备代码和测试，不自动执行生产发布；
- 新版本发布后，分别完成一个 OpenCode、Kimi Code、OpenClaw 真实选择上传 smoke；
- 自动化、真实选择上传和发布记录全部补齐后，才能从未修复清单移除。

## 8. 当前验证记录

- TDD 红灯已分别证明：新增客户端未进入公共选择器、OpenClaw 非 TTY/TUI 全选未受限、完整客户端名被截断、子命令帮助触发 Adapter 发现；
- 对应最小实现完成后，上述用例均转绿；
- `cd daemon && go test ./...`：通过；
- `cd daemon && go vet ./...`：通过；
- 使用版本 `0.1.18` 构建临时 Linux CLI：通过；
- node157 真实 OpenClaw smoke：无 ref 时成功进入公共 Bubble Tea Session Picker，显示最近活动时间、完整 `openclaw`、opaque ref 和摘要；按 `q` 取消后输出“未选择 Session”，未进入 Materialize 或上传；
- node157 已安装 CLI 仍为 `0.1.17`，未被本次测试替换；
- 已通过正式测试发布流程构建 Linux、macOS、Windows 多平台 `0.1.18` 产物，构建目录校验全部通过；
- 已将 `0.1.18` 文件上传到 14.157 测试分发，下载复验六项 SHA256 全部通过，测试版本指针已切换到 `0.1.18`；
- 下载复验目录为 `/home/intellif/aida-release-verifications/20260722T033347Z-0.1.18`；
- node157 已安装 CLI 仍为 `0.1.17`，本次测试发布没有在服务器本机安装 CLI；
- 尚未执行真实选择后的上传，也未进行生产发布。

## 9. OpenClaw 真实事件兼容修复

`0.1.18` 使用真实 OpenClaw `2026.6.33` Materialize 时发现：OpenClaw 导出 `user.message`、`assistant.message`，而 Adapter 测试夹具和实现只识别 `message.user`、`message.assistant`，导致有 transcript 的 Session 被误报为无可读事件。

`0.1.19` 调整为：

- 消息优先依据 `data.message.role` 和可读内容识别，不依赖固定事件名；
- 兼容两组现有消息事件名；
- 结构兼容的新正整数 schema 版本继续解析；
- 未识别 transcript 事件跳过，不使整个 Session 失败；
- 导出路径、Session ID、trace ID、序号、时间戳和大小校验继续严格。

真实 OpenClaw Discover + Materialize、daemon 全量测试、vet、Adapter race、多平台构建和下载 SHA256 均已通过。测试分发版本文件已切换到 `0.1.19`；尚未进行生产发布。

首次测试上传暴露测试 API 仍运行在 canonical 路由提交之前的旧镜像，prepare 返回 HTTP 404。确认 migration `026` 后，只重建测试 `api` 容器；migration 已从 `025` 更新到 `026`，`/health` 正常，带有效认证的 canonical prepare 空请求返回预期 `400` 而非 `404`。DB、MinIO 和 Web 容器未重启。
