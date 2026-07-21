# 多客户端支持

> 文档状态：P0 已实现；本次按“生产开启、用户公开测试、仅报告内容”发布
> 更新时间：2026-07-21
> 优先级：低于 Aida CLI 稳定性、升级通道和现有 Claude Code/Codex 链路

## 0. 本次生产发布唯一执行口径

本节是本次发布的执行基准。开发、合并、发布和回滚不得在执行过程中临时改成其他口径；同目录历史评审中的“真实验收后再开放”调整为本次生产公开测试策略，但 Token 精确性门禁不变。

### 0.1 发布目标

- 本次必须以**开启态**发布 OpenCode、Kimi Code、OpenClaw 的报告内容上传能力，供生产用户使用真实客户端测试；不能因为开发者本地未安装这些客户端而改成关闭态或只发布空壳。
- 这是“仅报告内容”的公开测试，不宣称新增客户端已达到与 Claude Code/Codex 相同的完整 Token 支持。
- 腾讯 WorkBuddy 本次保持 `detected-only`：可以显示检测结果，但不得读取私有数据库、Materialize 或 Upload。

### 0.2 旧流程冻结

- `aida upload` 和 `aida push` 仍只扫描 `~/.claude/projects` 与 `~/.codex/sessions`，不得发现、列出或上传任何新增客户端。
- `aida upload --all` 和自动同步仍只上传 Claude Code/Codex，不得调用新增 Adapter 或 canonical 上传入口。
- Claude Code/Codex 继续使用原 Prepare/Chunk/Finalize、原生 parser、usage fingerprint、claim、contribution、family rollup 和成本逻辑；不得迁移到统一 Adapter 或 Canonical Event。
- 自动上传与手动上传继续共用上传锁；任一上传进行中时禁止另一上传并发执行。

### 0.3 新客户端生产入口

- `aida clients`：检测 OpenCode、Kimi Code、OpenClaw、WorkBuddy，并输出版本或脱敏诊断。
- `aida upload-client opencode [session-ref...] [--all]`：开启。
- `aida upload-client kimi_code [session-ref...] [--all]`：开启。
- `aida upload-client openclaw <session-ref...>`：开启，但必须显式指定 Session；禁止 `--all`。
- `aida upload-client workbuddy ...`：禁止，保持明确的 detected-only 提示。
- 新客户端只走独立 `/api/v1/canonical-session-syncs/prepare` 和 canonical 内容处理；失败时不得回退到旧上传接口。

### 0.4 服务端发布策略

- 生产 `ReleasePolicy` 必须显式放行 OpenCode、Kimi Code、OpenClaw，不能传空策略。
- 三个客户端分别锁定本次发布的 Aida `client_version` 和各自 Adapter 版本：`opencode-v1`、`kimi-code-v1`、`openclaw-v1`。
- 三个客户端的最大 `usage_capability` 均为 `unavailable`；客户端不能自行提升为 `estimated` 或 `exact`。
- WorkBuddy 不得加入生产 `ReleasePolicy`。
- 后续某个客户端需要停用时，只关闭该客户端的 canonical release 项，不得修改 Claude Code/Codex 旧流程。

### 0.5 Token 与 subagent/fork 口径

- OpenCode、Kimi Code、OpenClaw 本次不统计 Token、不统计成本、不显示错误的 0，也不将 canonical 原始 usage 累加到报告。
- 新客户端只有完成真实逐调用对账、父子乱序上传、fork 继承历史和至少二级 subagent 验收后，才允许独立提升到 `exact`。
- Claude Code/Codex 的精确 Token 与 subagent/fork 归属保持现有实现和结果，不因本次发布重算历史数据。

### 0.6 发布检查与停止条件

发布前必须确认：

1. `aida upload` 的扫描目录仍只有 Claude Code/Codex；
2. auto-sync 仍通过旧 `cmdUpload --all`，且未引用新增 Adapter；
3. Claude Code/Codex legacy Golden、API/daemon 全量测试和 migration 026 全新数据库回归通过；
4. 生产策略已开启三个 report-only 客户端，且服务端允许的 `client_version` 与发布 CLI 版本完全一致；
5. OpenClaw 仍禁止 `--all`，WorkBuddy 仍禁止上传；
6. 新客户端 Token 能力仍为 `unavailable`。

只有 Claude Code/Codex 旧上传、Token、成本或自动同步出现回归时，才停止整次发布。新增客户端公开测试中单个 Adapter 失败时，按客户端独立修复或关闭，不回退、重构或改写旧链路。

## 1. 目录边界

本目录只讨论 Aida 对新增编码客户端的接入，不承担 Aida CLI 当前版本的交互和发布说明。

候选客户端包括：

- OpenCode；
- Kimi Code；
- OpenClaw；
- harness.lol；
- ZCode；
- 腾讯 WorkBuddy 及后续经过评审的客户端。

以下内容不属于本目录：

- Aida 自动升级和安装方式；
- 现有 Claude Code/Codex Session 选择器；
- Session 摘要清理和双行展示；
- 当前 CLI 发布版本和发布步骤；
- 已验证的 Claude Code/Codex Token 解析器重构。

上述内容统一进入 [`Aida客户端重构`](../Aida客户端重构/README.md)。

## 2. 当前结论

多客户端支持具备可行性，但不应影响已经验收的 Claude Code/Codex 上传、Token、成本和默认 Agent 流程。

P0 已按以下顺序完成代码实现：

1. 获取脱敏真实样本和稳定版本证据；
2. 冻结 Adapter 与 Canonical Event 契约；
3. 先提供报告内容能力；
4. Token 按客户端独立解析和对账，但统一输出 Aida Usage 事实；
5. 每个客户端使用独立开关、独立验收和独立回滚。

当前实现状态：

- 原 `aida upload`、Claude Code/Codex Prepare/Chunk/Finalize 和原生 Token parser 未迁移、未改写；legacy Golden 与数据库集成回归通过。
- 新增 `aida clients` 与 `aida upload-client`，仅走独立 canonical Prepare；OpenCode、Kimi Code、OpenClaw 已具备报告内容候选能力，等待真实版本人工验证。
- OpenClaw 只允许显式选择单个 Session；自动同步、`aida upload --all` 和 `aida upload-client openclaw --all` 均不包含 OpenClaw，优先保证 Claude Code/Codex 自动同步稳定性。
- Canonical Usage 的 exact 字段、服务端解析、owner 归属与跨父子 source claim 已通过合成父子 Session 数据库集成测试；OpenCode、Kimi Code、OpenClaw 在真实逐调用对账前仍上报 `usage_capability=unavailable`，不会显示错误的 0 或估算值。
- 腾讯 WorkBuddy 仅做发现与明确诊断，不读取未公开本地数据库，不允许 Materialize/Upload；等待官方机器可读导出契约。
- 功能分支已提交，且已将当前本地 `main` 新代码合入功能分支用于联合测试；尚未推送、合回 `main` 或发布，也未在现有测试服务数据库执行 migration 026。

新增客户端默认能力为：

```text
content_capability = report
usage_capability = unavailable
```

没有逐事件真实对账证据时，不得把 Token 显示为 0，也不得进入成本统计。对外称为“完整支持”的客户端必须达到 `report + exact`；只达到 `report` 的客户端必须明确标记为“仅报告内容”。

## 3. 不变约束

- **最高约束：不得影响 Claude Code/Codex 现有统计。** 两者当前的扫描、Session 分组、Prepare/Chunk/Finalize、原生 parser、usage fingerprint、claim、contribution、family rollup、成本和展示结果全部冻结；新增客户端不得要求它们迁移到 Adapter、Canonical Event 或新的上传流程。
- 新增客户端只能通过独立的 canonical seam 接入；任何 Claude Code/Codex 逐事实、Session、family 或成本回归都立即停止本期开发和发布。
- 不把“能读取会话正文”等同于“能准确统计 Token”。
- 主 Session、subagent 和 fork 共享的历史 usage 只能归属并计数一次；缺少稳定计费事实身份或 fork 基线时不得标记 `exact`。
- 不读取第三方客户端凭据、全局诊断日志或未选择的私人会话。
- 不以标题、时间或摘要代替稳定 Session ID。
- 不因一个新增客户端失败而影响其他客户端上传。
- Mock 和单元测试不能代替 14.157 的真实 Session、真实接口和默认 Agent 验收。

## 4. 文档索引

- [产品需求](./产品需求.md)：能力边界、客户端优先级和分阶段交付。
- [开发方案](./开发方案.md)：Adapter、Canonical Event 和服务端扩展设计。
- [测试与验收方案](./测试与验收方案.md)：Fixture、协议、报告、Token 和安全验收。
- [评审记录](./评审记录.md)：可行性、风险和历史评审结论。
- [OpenClaw 接入调研](./OpenClaw接入调研.md)：官方 CLI/trajectory 契约、身份、隐私和 Token 能力结论。

## 5. 启动条件

同时满足以下条件后，才进入开发排期：

1. 明确第一批客户端及支持版本；
2. 每个客户端至少有三份可合法使用的脱敏真实样本；
3. 明确稳定 Session ID、增量边界和内容导出来源；
4. 产品接受首期 Token 显示“暂不支持”；
5. Claude Code/Codex 核心回归基线已冻结，包含原始 fixture、上传协议结果、逐事实 Token、Session/family 汇总和成本 Golden；
6. 完成新一轮产品、架构和测试一致性评审。
