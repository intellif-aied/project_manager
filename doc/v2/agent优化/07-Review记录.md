# 报告 Agent Session 摘要优化 Review 记录

> 状态：三轮方案 Review + 三轮实现 Review 已完成
> 日期：2026-07-15 至 2026-07-16
> Review 对象：本目录 1.0 版需求、架构、契约、开发、测试、发布方案和候选 Skill 评估
> 结论：服务端实现与自动化验证完成；功能尚未部署，真实 Agent 与生产灰度仍须按发布门禁执行

## 1. Review 方法

本次采用三次相互独立的审查视角，每轮都先找问题、再修改文档、最后做跨文档复核：

1. 产品与术语一致性；
2. 架构可实现性、性能与安全；
3. 测试可验收性、灰度、运维与回退。

审查依据包括：

- 上位产品基准 `doc/v2/Session增量上传与报告来源/`；
- 14.157 当前 `session_content_slices`、内容投影、`session_processing_jobs`、report source 和 MCP 实现；
- `tmp/session-digest` 脚本的输入边界、依赖和输出规模分析；
- 本目录的需求编号、数据字段、错误码、预算、配置和测试追踪关系。

## 2. 第一轮：产品与术语一致性

### 2.1 发现与优化

| 发现 | 风险 | 已完成优化 |
| --- | --- | --- |
| 个人周报的“已保存个人日报优先”只在测试文档中出现 | 实现可能让 Session Digest 越级成为周报主来源 | 在用户场景和 SDR-FR-009 中固定“个人日报为主、Session 仅作既有补充” |
| “完整覆盖”可能被理解为所有原始事件文本都进入 Digest | 与摘要压缩目标矛盾，也会误导验收 | 统一定义为每个 selection item 被遍历并表示，事件取舍由 included/omitted 计数披露 |
| 架构要求每个 compact item 必须有 headline，但契约禁止无证据时编造 | 空或纯工具 Session 无法同时满足两条规则 | 改为元数据和 coverage 必填，goal/outcome 有可靠证据时才保留 |
| `AMBIGUOUS_REPORT_SOURCE` 只在测试中出现 | 错误码没有权威定义 | 补入数据契约，并将 MCP 测试中的缩写统一为完整错误码 |

### 2.2 本轮复核

- SDR-FR-001 至 012、SDR-NFR-001 至 008 均在测试追踪矩阵中出现；
- 个人日报、个人周报、组织报告、Token/成本和完整切片边界一致；
- `coverage.complete`、`completeness=complete` 和 omitted 计数语义不冲突；
- 本轮未改变既有六类报告 scope 和来源层级。

## 3. 第二轮：架构、性能与安全

### 3.1 发现与优化

| 发现 | 风险 | 已完成优化 |
| --- | --- | --- |
| 初稿只在内容投影激活后创建 Digest 任务 | active generation 后续增量切片不一定再次 activation；若改成上传事务内补建，又会让摘要故障阻断上传 | 改为独立 Reconciler 扫描已提交 ready slice，唯一约束幂等入队，上传只可选发 wake-up |
| 把 `read_completed_at` 写成新增字段 | 当前 migration 已存在该列，实施会重复设计 | 改为复用现有列，只新增 mode、payload 和 hash 等字段 |
| 新 job 只描述了外键，未覆盖队列完整契约 | job type CHECK、epoch 约束或 repository Scan 任一遗漏都会导致 claim 失败 | 补齐 CHECK、`ProcessingJob`、RETURNING/Scan、唯一任务和独立 worker 要求 |
| 现有队列会保存 processor error 到 `last_error` | 原始解析错误可能把敏感 Session 内容写入数据库或日志 | 要求 Digest worker 只传稳定错误码和无正文诊断，并增加安全集成测试 |
| Assembler 可能逐 item 读库，`actual_bytes` 又包含自身数值 | 200 items 产生 N+1；字节门禁可能在位数边界不准确 | 要求批量读取、定点序列化、规范 JSON hash 和查询次数测试 |
| compact item 的 hash 指代不清 | compact 文本无法复算 detailed Revision hash | 明确 item hash 绑定不可变 Revision，最终 payload 使用 selection-level hash |

### 3.2 本轮复核

- 切片先形成、投影先完成、projection 重建和进程重启均由 Reconciler 收敛；
- Builder 不阻塞上传，不在报告请求中同步扫描数 MB 原文；
- 规则提取、脱敏、预算、hash 与版本均可确定性重放；
- 原始 payload、reasoning、工具输出、凭据和 Token 遥测均在模型边界前排除；
- full、shadow、digest worker 与 Token/计费 worker 的职责保持隔离。

## 4. 第三轮：测试、发布与回退

### 4.1 发现与优化

| 发现 | 风险 | 已完成优化 |
| --- | --- | --- |
| 发布要求 allowlist 和 5%/25%/50% canary，但只有全局 read mode | 无法执行稳定小流量，也无法区分计划内 full 与异常 fallback | 增加测试用户 allowlist、稳定单调分桶、rollout percent、决策指标和边界测试 |
| “Session 来源相关 Token”没有可直接审计的统一字段 | A/B 可以选取有利口径，缓存顺序也会造成偏差 | 改用平台既有整次报告输入 Token；Claude/Codex 各至少 10 个大来源配对样本并交替 A/B 顺序 |
| 初稿在 Agent 调用 MCP 时才执行 Assembler，但需求要求超限时启动 Agent 前失败 | LIMIT_EXCEEDED 只能在 Agent 已启动后发现 | 改为 run 创建前预组装并冻结 selection-level payload，MCP 只校验和返回不可变快照 |
| 灰度告警使用“明显恶化”等相对描述 | 值班人员无法一致判断是否停灰度 | 要求发布前冻结阈值和窗口，并提供 0.1%、10 分钟积压、P95 +10% 的建议起点 |
| 缺少冻结 payload 损坏、版本变化和生命周期验证 | 重试可能返回不同内容，或过期 selection 残留摘要 | 增加逐字节幂等、hash 损坏、代码/配置变化和随 selection 清理用例 |

### 4.2 本轮复核

- hard limit 在外部 Agent Session 创建前验证；
- canary 未命中产生计划内 full，已冻结 digest 后改读 full 才计 raw fallback；
- full -> shadow -> allowlist -> 百分比灰度 -> enforce -> full 回退形成闭环；
- Golden、单元、PostgreSQL 集成、MCP、真实 Agent、六类报告、安全、性能和回退均有测试入口；
- 真实 Token 优化必须与关键事实 100%、无新增幻觉和 Token/成本对账不变同时成立。

## 5. 最终一致性清单

| 项目 | 统一结果 |
| --- | --- |
| Digest 版本 | `session-digest/v1` |
| 脱敏版本 | `report-redaction/v1` |
| 正常目标 / 硬上限 | 65536 / 131072 UTF-8 bytes |
| 模型可见字节边界 | 规范序列化的 `get_sessions` result payload |
| 生产生成方式 | Aida 服务端、规则提取、无 LLM、无 jq |
| 摘要粒度 | 一次完整上传切片一个不可变 Digest Revision |
| selection 行为 | 所有 item 一一表示；attach 前冻结 selection payload |
| 读取模式 | 服务端按 run 冻结；Agent 无权选择或回退 |
| 受影响报告 | 托管个人日报和个人周报的 Session 补充来源 |
| 不受影响 | 组织报告层级、上传、Token/成本、权限、原始内容保留 |
| 失败策略 | NOT_READY/FAILED/LIMIT/VERSION/MODE 明确失败，不静默 full |
| 灰度策略 | shadow、allowlist、稳定百分比分桶、全面 enforce |
| 回退策略 | 仅新 run 切 full；已附着 run 保持冻结契约；不破坏性回滚数据库 |

## 6. 文档校验结果

- 需求编号：20 个，测试追踪矩阵无缺失；
- 文档链接：本目录相对链接均存在；
- 重复定义：预算、版本、错误码、模式和来源层级已按权威顺序复核；
- Markdown：已清理行尾空白，无未决占位标记；
- 文件范围：仅新增 `doc/v2/agent优化/`，不修改并行会话的代码和既有文档；
- 实现状态：服务端代码、迁移和自动化已完成；真实 Agent、浏览器、Token A/B 和生产灰度尚未执行，不能据此宣称已上线。

## 7. 上线前仍需现场确认

这些项目不改变方案，但必须在对应阶段形成证据：

1. 实施时重新选择无冲突 migration 编号，并重新检查共享工作树；
2. 用最终模型与平台计量字段冻结 A/B 查询和验收脚本；
3. 用容量测试与 14.157 基线冻结生产告警阈值；
4. Golden Fixture 期望值必须人工 Review，不能由实现自我批准；
5. 默认 `aida-report@1.0.0` 的实际生效内容必须在平台侧核对。

## 8. 2026-07-16 三轮实现 Review

### 8.1 第一轮：数据、事务与生命周期

发现并完成：

- 历史 `read_completed_at` 必须回填为 `read_completed_mode=full`，避免升级后误拒绝在途报告；
- ready Digest 与 attached selection payload 增加数据库不可变保护；
- `read_completed_mode` 必须等于 `required_read_mode`；
- 默认 selection 与显式 selection 在冻结前统一获取 Session share lock；
- Digest 读取和写回均复核 payload bytes/hash、版本、Revision 绑定、content epoch 和来源可用性；
- Assembler 批量读取、批量绑定，消除逐 item N+1。

### 8.2 第二轮：安全、容量与提取质量

发现并完成：

- Builder SQL 只投影 allow-list 的有界字段，未知事件、reasoning、图片、summary/excerpt 和完整工具输出不跨越数据库边界；
- 函数输出只保留首尾状态窗口，百万字节 fixture 验证正文不泄漏；
- 补齐 JSON/Basic Authorization、凭据槽、URI userinfo、query token、PEM 等脱敏；
- 结果字段优先保留最近成果，validation 超限优先保留失败项；
- 修正通用非零退出码、UTF-8 极小字节边界、路径穿越和 home 用户目录泄漏；
- Digest 中的 prompt injection 只作为不可信证据，Skill 明确禁止执行。

### 8.3 第三轮：兼容、重试与可验收性

发现并完成：

- 现有 `ba20665 fix: preserve complete report source slices` 保留且作为完整来源前置，不回退；
- full 默认、shadow 构建、digest 稳定 canary 分桶和 allowlist 均由服务端配置；
- NOT_READY/FAILED/LIMIT/VERSION/MODE 在外部 Agent Session 创建前或 MCP 边界稳定失败，无 raw fallback；
- job `last_error` 只写稳定码；Revision 失败/失效状态写入失败时保留队列 attempt，避免 dead job + building Revision 永久卡死；
- MCP 重复读取返回冻结的同一 payload，legacy full 分页回归保持通过；
- fresh PostgreSQL、全量 Go、vet 和 race 均形成实际执行证据。

### 8.4 实现 Review 结论

三轮未发现仍会阻断代码合入的问题。剩余风险均属于部署后的环境验收：历史回填资源、真实模型输入 Token、报告事实质量、P95/队列积压和回退操作，必须在 shadow/canary 阶段验证。
