# 多客户端支持

> 文档状态：P0 已实现，等待真实客户端人工验收；尚未发布
> 更新时间：2026-07-21
> 优先级：低于 Aida CLI 稳定性、升级通道和现有 Claude Code/Codex 链路

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
