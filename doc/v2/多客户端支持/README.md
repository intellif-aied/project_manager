# 多客户端支持

> 文档状态：方案储备，尚未定稿、尚未开发
> 更新时间：2026-07-17
> 优先级：低于 Aida CLI 稳定性、升级通道和现有 Claude Code/Codex 链路

## 1. 目录边界

本目录只讨论 Aida 对新增编码客户端的接入，不承担 Aida CLI 当前版本的交互和发布说明。

候选客户端包括：

- OpenCode；
- Kimi Code；
- OpenClaw；
- harness.lol；
- ZCode 及后续经过评审的客户端。

以下内容不属于本目录：

- Aida 自动升级和安装方式；
- 现有 Claude Code/Codex Session 选择器；
- Session 摘要清理和双行展示；
- 当前 CLI 发布版本和发布步骤；
- 已验证的 Claude Code/Codex Token 解析器重构。

上述内容统一进入 [`Aida客户端重构`](../Aida客户端重构/README.md)。

## 2. 当前结论

多客户端支持具备可行性，但不应影响已经验收的 Claude Code/Codex 上传、Token、成本和默认 Agent 流程。

现阶段不进入正式开发。后续启动时遵循以下顺序：

1. 获取脱敏真实样本和稳定版本证据；
2. 冻结 Adapter 与 Canonical Event 契约；
3. 先提供报告内容能力；
4. Token 与成本按客户端独立立项和对账；
5. 每个客户端使用独立开关、独立验收和独立回滚。

新增客户端默认能力为：

```text
content_capability = report
usage_capability = unavailable
```

没有逐事件真实对账证据时，不得把 Token 显示为 0，也不得进入成本统计。

## 3. 不变约束

- 不修改 Claude Code/Codex 已验证的原生上传和 Token 口径。
- 不把“能读取会话正文”等同于“能准确统计 Token”。
- 不读取第三方客户端凭据、全局诊断日志或未选择的私人会话。
- 不以标题、时间或摘要代替稳定 Session ID。
- 不因一个新增客户端失败而影响其他客户端上传。
- Mock 和单元测试不能代替 14.157 的真实 Session、真实接口和默认 Agent 验收。

## 4. 文档索引

- [产品需求](./产品需求.md)：能力边界、客户端优先级和分阶段交付。
- [开发方案](./开发方案.md)：Adapter、Canonical Event 和服务端扩展设计。
- [测试与验收方案](./测试与验收方案.md)：Fixture、协议、报告、Token 和安全验收。
- [评审记录](./评审记录.md)：可行性、风险和历史评审结论。

## 5. 启动条件

同时满足以下条件后，才进入开发排期：

1. 明确第一批客户端及支持版本；
2. 每个客户端至少有三份可合法使用的脱敏真实样本；
3. 明确稳定 Session ID、增量边界和内容导出来源；
4. 产品接受首期 Token 显示“暂不支持”；
5. Claude Code/Codex 核心回归基线已冻结；
6. 完成新一轮产品、架构和测试一致性评审。
