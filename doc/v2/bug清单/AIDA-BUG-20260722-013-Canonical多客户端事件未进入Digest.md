# AIDA-BUG-20260722-013：Canonical 多客户端事件未进入 Digest

> 优先级：P0
>
> 状态：测试服已验证至 Report Context，待外部模型恢复后完成最终日报验收
>
> 影响范围：OpenCode、Kimi Code、OpenClaw；ZCode 不在本次范围

## 1. 结论

当前新增客户端只完成了发现、物化、Canonical 上传和 Session 展示，尚未真正接通 Digest 与日报主流程。

OpenClaw 真实数据已证明：原始结构化事件共有 18 条，Digest 的 `included_event_count=0`、`omitted_event_count=18`。测试语句“这条消息是为了测试日报生成”存在于 `canonical.message`，cursor 位于 Digest Slice 范围内，但未进入 Digest、Report Source Selection、Report Context 和最终日报。

这不是自然语言关键词或正则误杀，而是类型链路缺失：`sessiondigestv2` 没有处理任何 `canonical.*` 事件。OpenCode、Kimi Code 使用同一 Canonical 上传链路，因此报告能力同样不可视为完成。

建议直接在当前尚未部署的 `session-digest/v2.10.0` 中补齐 Canonical 语义支持，避免先发布 v2.10、随后再升级 v2.11，造成全量 Digest 二次重建。

## 2. 现有实现链路

### 2.1 客户端 Adapter

- `daemon/internal/adapters/opencode/adapter.go` 的 `canonicalExportEvents` 将所有消息写成 `canonical.EventMessage`，payload 为 `message + native`。
- `daemon/internal/adapters/kimicode/adapter.go` 的 `Materialize` 将 wire 事件写成 `canonical.EventMessage`，payload 为 `agent + message + native`。
- `daemon/internal/adapters/openclaw/adapter.go` 的 `readTranscriptEvents` 生成 `message/tool_call/tool_result`，但 message payload 只有 `message + source_event_type + entry_id`，已识别出的 `user/assistant` role 没有写入 Canonical payload。

三者最终都通过 `daemon/internal/canonical/event.go` 写成 `aida.session.event.v1`。

### 2.2 服务端投影与不可变内容

- `api/internal/sessionsync/canonical_content_parser.go` 将 Canonical 类型投影为 `canonical.message`、`canonical.tool_call`、`canonical.tool_result` 等结构化事件。
- `session_content_events` 只保存 cursor、时间、类型、summary、excerpt 和哈希，不保存完整 payload。
- `api/internal/contentreader/reader.go` 在构建 Digest 时按冻结 revision 和 cursor 从 MinIO 不可变对象重新读取原始 Canonical JSONL，并校验索引与对象完整性。

因此本次不需要增加数据库字段或复制完整正文到 PostgreSQL。

### 2.3 Digest v2

- `api/internal/sessiondigestv2/safe_projection.go` 的 `projectSafeEvent` 只支持 Codex、Claude 事件；`canonical.*` 进入 `default` 后不产生安全 payload。
- `api/internal/sessiondigestv2/extractor.go` 的 `Extractor.Consume` 同样没有 `canonical.*` 分支，事件不会贡献 goal、Agent claim、命令或验证证据。
- `included_event_count` 只在事件产生事实贡献时增加，因此当前所有 Canonical 事件都会被计入 omitted。

### 2.4 刚合入的并发体系

- `api/internal/sessiondigestv2/coordinator.go` 负责 Digest revision 的确保、等待、失败分类、受控重建和 interactive/background 优先级。
- `api/internal/reportrun` 与 `api/internal/reportsource` 负责冻结 Selection、等待 Digest、构建 Report Context 和提交 Report Agent。

这些模块只负责“何时构建以及如何等待”，不负责“事件表示什么”。Canonical 适配不得写进 Coordinator、Report Run 或 Report Agent Prompt。

## 3. 目标与非目标

### 3.1 本次目标

1. OpenCode、Kimi Code、OpenClaw 的有效用户消息进入 Atomic Work Unit goal。
2. Assistant 消息只能进入 Agent claim，不能被误判成用户目标。
3. 有可靠结构化字段时，提取工具调用、工具结果和失败状态等客观证据。
4. 未知 role、未知事件和字段缺失时保守 omitted，不制造事实，也不阻断整个 Session 上传。
5. 保持 Digest 增量、幂等、不可变 revision、Selection 冻结和 Report Context 追溯能力。

### 3.2 明确不做

- 不引入 LLM、关键词分类器或客户端专属 Prompt。
- 不让 Digest 读取完整数据库镜像或绕过 `contentreader.Reader`。
- 不新增 Work Stream、语义归并表或通用事件平台。
- 不修改 Report Agent 的语义归并职责。
- 不实现 OpenClaw Token 统计。
- 不把 ZCode 从 gated 状态提前放开。
- 不因为工具证据不完整就猜测“已完成”或“验证通过”。

## 4. 推荐设计

### 4.1 将 Canonical Wire Contract 作为唯一 seam

客户端差异只允许存在于各 Adapter 内部；服务端 Digest 只认识 Canonical 语义，不读取 OpenClaw trajectory、Kimi wire 或 OpenCode export 的私有结构。

保持 envelope schema 为 `aida.session.event.v1`，在 payload 中补充向后兼容的标准字段：

```json
{"type":"message","payload":{"role":"user|assistant","message":"...","phase":"final|intermediate|unknown"}}
{"type":"tool_call","payload":{"call_id":"可空","name":"...","input_summary":"可空"}}
{"type":"tool_result","payload":{"call_id":"可空","status":"success|failure|unknown","output_summary":"可空"}}
{"type":"result","payload":{"status":"success|failure|unknown","summary":"..."}}
{"type":"error","payload":{"summary":"..."}}
```

规则：

- `role` 只能由 Adapter 从结构化字段确定；无法确定时写 `unknown` 或不写，Digest 保守忽略。
- `phase` 无可靠依据时为 `unknown`；不能把所有 assistant 文本都当最终回答。
- `call_id` 缺失时不得强行把 tool result 关联到最近调用。
- 未知字段允许保留并忽略，避免客户端小版本升级导致整个上传失败。
- Safe Projection 只提取上述白名单字段，继续过滤完整 native、完整日志、完整 diff、Base64 和凭据。

### 4.2 Adapter 修改

#### OpenClaw

- `projectTranscriptEvent` 返回 event type、role、摘要和可用关联 ID。
- 已经通过 `message.role` 识别出的 role 必须写入 payload。
- 对 `message.user/user.message/message.assistant/assistant.message` 可确定性映射 role。
- 对 `conversation.input` 等新类型，仅当原始 `message.role` 存在时采用；不根据自然语言或上下文猜测。

#### OpenCode

- 从 export message 的结构化 role 字段生成标准 `role`。
- `native` 可暂时保留在原始 Canonical 对象中，但 Digest Safe Projection 不读取它。
- 无 role 的记录仍可上传和展示，但不进入 Work Unit。

#### Kimi Code

- 只对已验证 wire 结构提取 role、message、tool 信息。
- 无法确认的 wire 事件保留为未知事件，不把 agent 名称当 role。
- 正在写入的尾部不完整行继续沿用现有增量处理，不扩大本次范围。

### 4.3 Digest v2 修改

在 `projectSafeEvent` 增加 `canonical.message/tool_call/tool_result/result/error` 的安全投影；在 `Extractor.Consume` 增加对应确定性提取：

- `canonical.message + role=user` → `consumeGoal`；
- `canonical.message + role=assistant + phase=final` → `addAgentClaim`；
- intermediate/unknown assistant 只作为有限上下文，不直接形成完成结论；
- tool call/result 只有稳定 `call_id` 时关联；
- error/failure 作为失败证据，不改变业务完成状态；
- 未识别或证据不足的事件仍计入 omitted。

不要为 Canonical 新建第二套 Work Unit Builder，应复用现有 `consumeGoal`、`addAgentClaim`、`addCommandCall`、`completeCommand` 和证据模型。

### 4.4 版本与不可变性

当前 `main` 已包含但尚未部署 `session-digest/v2.10.0`。本修复应在 v2.10 首次发布前完成：

- 不再升级到 v2.11；
- v2.9 revision、已冻结 Selection、既有 Report Context 和历史报告保持不可变；
- 部署 v2.10 后，由现有 Reconciler/Coordinator 创建新的 revision；
- 不 UPDATE 旧 digest JSON，不覆盖旧 Report Context。

如果任何环境已经发布并生成 v2.10 revision，则必须停止使用本策略并升新版本，不能在同一 Digest version 下改变算法。

### 4.5 已上传旧 Canonical Session

旧 Canonical 对象缺少统一 role，服务端不应长期解析各客户端的 `native` 私有结构。

推荐处理：

1. 发布新 CLI 和新服务端；
2. 用户重新上传所需 Session，Adapter 生成带标准 role 的新 generation；
3. 新 generation 产生新的 projection、Slice 和 v2.10 Digest；
4. 旧 generation 和旧 revision 保留追溯，不直接改写。

仅可为当前 OpenClaw 已知 `source_event_type` 做有明确枚举的短期兼容；未知类型必须 omitted。该兼容不得演变为服务端维护各客户端私有格式解析器。

## 5. 方案评审

### 5.1 备选方案

| 方案 | 结论 | 原因 |
| --- | --- | --- |
| Digest 直接解析各客户端 native | 拒绝 | 客户端升级会迫使服务端同步升级，破坏 Canonical seam，测试和维护成本持续增长 |
| 把所有 `canonical.message` 当用户消息 | 拒绝 | 会把 Assistant 自述当用户目标，产生严重事实错误 |
| 只使用 summary/excerpt | 拒绝 | summary 没有可靠 role、phase 和 call_id，无法形成正确 Work Unit 与证据关联 |
| 在 Report Agent Prompt 中补读完整 Session | 拒绝 | 重复读取大 JSONL，绕过 Digest，增加 Token、超时和职责重复 |
| Adapter 标准化 + Digest 只读 Canonical 字段 | 通过 | seam 清晰、无新增存储、确定性、可测试，并复用现有 Work Unit 实现 |

### 5.2 对现有体系的影响判断

- **上传协议**：envelope 与事件类型不变，仅增加 payload 可选字段，兼容风险可控。
- **内容投影**：无需迁移；索引仍保存 cursor/type/summary/hash，原文仍在 MinIO。
- **Digest**：新增事件分支，复用现有事实提取；这是主要代码变化。
- **并发协调**：接口与状态机不变，只处理新的 v2.10 revision。
- **Report Source/Context**：结构不变；只会收到更完整的 Work Unit。
- **Report Agent**：无需修改 Prompt 或 Skill。
- **Token 统计**：不受影响，Canonical 客户端仍保持 `usage_capability=unavailable`。

评审结论：**有条件通过**。下列五项是开发和发布阻断条件：

1. role 必须来自结构化字段，未知 role 不得进入 goal/claim；
2. 必须在 v2.10 首次部署前完成或改升新 Digest version；
3. 三个客户端均有 Adapter → Canonical → Digest 的端到端 fixture；
4. Codex、Claude 现有 Golden Cases 零回归；
5. 发布前完成一次版本升级引起的 Digest 重建容量验证。

## 6. 风险清单

| 风险 | 等级 | 影响 | 控制措施 |
| --- | --- | --- | --- |
| role 误判 | P0 | Assistant 文本被当用户目标，日报制造错误事实 | role 只取结构化字段；unknown omitted；用户/Assistant 对照 fixture |
| v2.10 已在某环境生成后继续改算法 | P0 | 同版本 Digest 输出不一致，破坏追溯和幂等 | 发布前检查运行时版本与 revision；一旦存在则升 v2.11 |
| Digest 版本升级触发全量后台重建 | P0 | 20 个 background worker 加 5 秒 Reconciler 可能造成 DB/MinIO 压力 | 在测试服测 backlog、吞吐和 DB/MinIO；生产错峰；监控队列和失败率；不得二次升版本 |
| CLI/服务端混部 | P0 | 旧 CLI 上传无 role，新服务端仍产生空 Digest | envelope 向后兼容；UI/CLI 明示需更新和重新上传；验收覆盖旧 CLI 数据的保守 omitted |
| tool result 错挂 | P1 | 失败被挂到错误 Work Unit 或错误命令 | 仅稳定 call_id 关联；缺失时保守保存独立事实或 omitted |
| 新 Canonical 字段携带敏感正文 | P1 | Report Context 泄露完整日志、参数或凭据 | Safe Projection 白名单和长度限制；禁止投影 native、完整参数、Base64 |
| Canonical 内容变多导致 Digest 体积上升 | P1 | 构建耗时和 Context 体积增加 | 继续使用现有单事件限制、Work Unit 压缩和 selection envelope；增加容量 fixture |
| 旧 Session 无法自动修复 | P1 | 已上传 Session 仍缺日报内容 | 明确重新上传生成新 generation；不修改旧 revision |
| 修改 Coordinator 造成竞态回归 | P1 | 再次出现 409 或重复 run | 本方案禁止修改 Coordinator/Report Run 状态机；复跑现有并发测试 |
| 顺带接入 ZCode | P2 | 扩大范围并引入无稳定契约的数据源 | 保持 gated，不进入本次实现和发布 |

## 7. 测试方案

### 7.1 Adapter Contract

每个已发布客户端至少覆盖：

1. user message 输出 `role=user`；
2. assistant message 输出 `role=assistant`；
3. 未知类型或未知 role 不报整 Session 失败；
4. tool call/result 有关联 ID时保持关联；
5. 凭据、完整 native、Base64 不进入 Digest 安全投影。

### 7.2 Digest Golden Cases

- GC-CAN-001：用户消息形成一个 Atomic Work Unit；
- GC-CAN-002：Assistant 消息不会形成用户 goal；
- GC-CAN-003：最终回答只形成 Agent claim，不作为客观验证；
- GC-CAN-004：有 call_id 的失败结果关联正确 Work Unit；
- GC-CAN-005：无 call_id 不错误关联；
- GC-CAN-006：未知事件计入 omitted，但其他事件仍正常输出；
- GC-CAN-007：同一输入重复构建，Digest JSON 与 hash 完全一致；
- GC-CAN-008：分批上传和一次上传的最终 Digest 一致；
- GC-CAN-009：Codex、Claude 既有样本输出不变；
- GC-CAN-010：OpenClaw 真实测试句从原始事件一直进入 Report Context。

### 7.3 端到端验收

对 OpenCode、Kimi Code、OpenClaw 分别执行真实客户端黑盒流程：

1. 使用正式测试分发 CLI 上传一个明确选择的 Session；
2. 等待服务端 authoritative readiness；
3. 查询 Slice 与 Digest coverage，要求有效消息 `included_event_count > 0`；
4. 创建 Report Source Selection；
5. 生成个人日报；
6. 在 Report Context 和最终日报中确认用户目标可见；
7. 验证 Token 统计仍显示 unavailable/null，不伪造 Token。

## 8. 发布与回滚

建议提交顺序：

1. Canonical payload typed contract 与三个 Adapter fixture；
2. Digest v2 Canonical Safe Projection、Extractor 和 Golden Cases；
3. 真实 Session 端到端测试与发布说明。

发布顺序：测试服 API/Web → 测试 CLI → 三客户端真实上传验收 → 生产 API → 生产 CLI。生产发布前确认没有环境已经生成语义不同的 v2.10 revision。

回滚只回滚 API/CLI 镜像，不删除数据库、不删除 MinIO、不改写 revision。新生成的 v2.10 revision 保留追溯；旧 v2.9 revision 与历史 Report Context 始终可审计。

## 9. 验收标准

1. OpenClaw 当前真实样本不再是 `included=0/omitted=18`；测试句进入 Work Unit 和 Report Context。
2. OpenCode、Kimi Code 各有一份真实 Session 完成同等链路。
3. 未知 role 不产生 goal 或 Agent claim，事实错误率为 0。
4. Codex、Claude Digest Golden Cases 全部通过且输出无非预期变化。
5. 同一 Canonical JSONL 重跑 hash 一致，分批与单次上传最终 Digest 一致。
6. Digest backlog、构建失败率、DB 与 MinIO 负载满足当前 v2.10 容量基线。
7. 无新增表、无 LLM、无 Report Agent Prompt 变更、无 Coordinator 状态机变更。

## 10. 当前实现结果（2026-07-22）

已完成：

- OpenCode、Kimi Code、OpenClaw Adapter 将可确定的消息角色写入 Canonical payload；无法确定时统一为 `unknown`。
- OpenClaw 在存在稳定 `call_id` 时输出标准 tool call/result 关联字段；缺少关联 ID 时 Digest 保守忽略，不挂到最近 Work Unit。
- Digest Safe Projection 已支持 `canonical.message/tool_call/tool_result`，且不读取 `native`、完整参数或完整日志。
- Digest Extractor 已支持 Canonical 用户目标、明确 final 的 Agent claim，以及有稳定 `call_id` 的工具验证证据。
- 增加 Canonical 原始事件到 Digest 的管线回归测试，并固化 Codex、Claude 当前 v2.10 Digest JSON 哈希基线。

已验证：

- 修复已提交并部署到 14.157 测试服；测试 CLI 已发布为 `0.1.21`。
- API 全量测试通过；daemon 全包测试与 `go vet` 通过，覆盖 OpenCode、Kimi Code、OpenClaw Adapter contract。
- `git diff --check` 通过；Digest 版本仍为 `session-digest/v2.10.0`，未修改 Coordinator、Report Agent Prompt、数据库表或 Web。
- OpenClaw `2026.6.33` 真实 Session 重新上传成功；v2.10 Digest 从旧版 `included=0/omitted=18` 修复为 `included=4/omitted=9`，形成 4 个 Atomic Work Unit。
- 测试句已进入第 3 个 Work Unit，并进入 6153 字节的不可变 Report Context。
- 真实链路额外发现并修复 Canonical 建索引与回读 Parser 不一致，以及 Report Context 转阶段 SQL 参数缺少显式类型两个问题。

尚未完成：

- 最终 Report Agent 连续两次被外部模型配置阻断：AIHub 返回 `MiniMax-M2.5` 不存在或当前账号无权限；这发生在 Digest 和 Report Context 成功之后。
- 尚未用 OpenCode、Kimi Code 的真实 Session 完成同等黑盒验收。
- 尚未执行 v2.10 后台重建容量验证，因此本缺陷不能标记为已修复。
- 当前提交尚未推送远端。
