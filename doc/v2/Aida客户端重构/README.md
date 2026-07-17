# Aida 客户端重构

> 文档状态：持续维护
> 更新时间：2026-07-17
> 当前稳定基线：Aida CLI `0.1.11`

## 1. 目录边界

本目录只讨论 Aida CLI 自身能力：安装升级、Session 发现与选择、摘要质量、上传状态和终端交互。

本目录可以修改：

- `daemon/` 中现有 Claude Code/Codex 的本地发现和展示；
- Session 本地索引和摘要提取；
- Bubble Tea Session 选择器；
- 自更新、安装脚本和发布构建；
- Session Sync readiness 客户端交互。

本目录不讨论：

- OpenCode、Kimi Code、OpenClaw、Harness、ZCode 等新客户端接入；
- Canonical Event 和 Adapter 框架；
- 新客户端 Token 与成本能力。

这些内容统一进入 [`多客户端支持`](../多客户端支持/README.md)。

## 2. 当前原则

- 不因列表展示优化改变 Session 分组和上传成员。
- 用户选择根 Session 后，仍自动上传其全部 sub-agent。
- 目录和 sub-agent 信息可以作为隐藏搜索字段，但不占用默认列表空间。
- 摘要必须来自有效用户输入，不能展示 IDE、权限、Skill、Plugin 或系统注入内容。
- 客户端优化不得破坏 Prepare、Chunk、Finalize、checkpoint 和增量 cursor 语义。
- 功能方案不提前绑定新版本号，正式发布时再确定版本。

## 3. 文档索引

- [客户端上传与升级方案](./客户端上传与升级方案.md)：上传 readiness、自更新和发布边界。
- [Session 选择器与摘要优化方案](./Session选择器与摘要优化方案.md)：双行列表、Session ID、摘要清理与缓存升级。

## 4. 验收底线

任何客户端重构至少需要验证：

1. Claude Code 与 Codex 的真实 Session 扫描；
2. 根 Session 和 sub-agent 自动成组上传；
3. 无新增内容时 `chunks=0`；
4. Token 和成本升级前后对账一致；
5. Windows、Linux、macOS 的终端与非 TTY 行为；
6. 本地索引升级后不会继续复用旧错误摘要。
