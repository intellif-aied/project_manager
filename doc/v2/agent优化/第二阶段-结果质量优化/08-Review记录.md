# Session Digest v2 Review 记录

> Review 日期：2026-07-16
> Review 范围：方案四轮 Review + 实施 Review + 当前会话三轮真实 Agent Review
> 结论：代码和 14.157 测试环境接入完成；v2.4 当前会话真实全链路通过，生产接入仍受完整 corpus、Human Gold、正式配对 A/B 和资源门禁约束

## 第一轮：需求与根因 Review

### 初始问题

最初方向容易退化为：

- 增加 `outcomes` 数量；
- 强化日报提示词；
- 让另一个模型对 v1 再总结一次。

这些方式没有解决 Work Unit 边界和结果证据缺失。

### 发现

1. target Session 有 243 个 task complete，最后消息不代表主要结果；
2. `AGENTS.md` 经 JSON 字符串包装后绕过前缀过滤；
3. blocker 关键词判断会把产品讨论误判为真实阻塞；
4. files/validations/outcomes 没有归属关系；
5. 只改 Skill 无法恢复已丢失结构。

### 修改

- 引入 Work Unit；
- 引入 category/status/evidence grade；
- 区分 `agent_claim` 与 `derived_evidence`；
- 增加 final/task_complete 镜像事件去重；
- blocker 改为时序状态机；
- Digest 与 Report Skill 同时进入范围；
- 明确 v2 不调用 LLM。

### 结论

通过。方案从“更好的文本摘要”调整为“结果证据模型”。

## 第二轮：评测与 14.159 Review

### 初始问题

只把 target Session 当 Golden，容易过拟合；“使用 14.159 大量 Session 验证”缺少：

- corpus 身份；
- 全量与抽样边界；
- 人工 Gold；
- holdout；
- 多次 trial；
- 失败样本记录；
- 环境真值。

### 发现

1. 初稿错误地把 14.159 当成需要从 14.157 远程登录的主机；复核后确认当前执行环境就是 14.159，应在本机只读采集；
2. 生产已上传数据仅能确认 target Session，不能冒充主机全量；
3. 所有 Session 都跑真实 Agent 成本过高；
4. 只用 LLM judge 无法可靠判断测试、commit 和部署；
5. 大 Session full/raw 基线可能重新触发容量问题。

### 修改

- 修正执行边界：14.157 维护项目代码，14.159 本机直接执行 read-only inventory；
- 增加 read-only manifest；
- 分为 Full Corpus、Stratified Benchmark、Human Gold、Holdout；
- Full Corpus 全量结构回放，真实 Agent 只跑分层样本；
- 引入 executable/repository/decision/human oracle；
- 大 Session raw 基线允许明确 not-run，不能伪装通过；
- 增加 corpus SHA、逐样本 hash 和中断恢复；
- target Session 强制进入所有评测层。

### 结论

通过。方案能够诚实区分“全量结构验证”和“抽样真实 Agent 验证”。

## 第三轮：兼容、安全与发布 Review

### 初始问题

若直接修改 v1：

- 历史 selection 契约漂移；
- Skill 与 payload 可能错配；
- 回滚困难；
- 测试账号可能实际未使用新版本。

### 发现

1. 数据库 check constraint 当前只允许 `full/digest_v1`；
2. v2 需要新的 read mode 和 Skill；
3. 同切片双版本构建需要 Reconciler/Processor 支持；
4. Agent 结果质量不能以扩大 payload 到接近原文为代价；
5. 隐藏 canary 会再次造成“用户测试的不是刚更新版本”。

### 修改

- 明确 `session-digest/v2.1` + `digest_v2`；
- 复用现有表，仅增加小型 constraint migration；
- 同切片允许 v1/v2 Revision；
- selection/run 冻结版本；
- 保留 64/128 KiB；
- 明确禁止擅自隐藏灰度；
- 增加 v1 快速回退和旧 API 兼容步骤；
- 增加实际 run trace 的 Skill/content_mode 核验。

### 结论

通过。版本、发布和回滚边界明确。

## 第四轮：RTK 借鉴、任务隔离与存量链路 Review

### 初始问题

RTK 提供了有效的命令输出压缩思路，但若直接接入，或只在现有 v1 Worker 上
增加 v2 分支，会引入新的运行时和任务竞争风险。初稿也没有充分回答：

- v1/v2 Worker 是否会领取同一类任务；
- v2 是否会影响 Session 上传和 Token 统计；
- RTK 的 Hook、tee、raw fallback 是否与 Aida 的安全边界冲突；
- v2 与 content/usage/metering Worker 共用 API/数据库时如何止损；
- migration 编号是否仍与仓库实际状态一致。

### 发现

1. RTK 是命令输出代理，不是历史 Session 摘要器；直接引入二进制、Hook 或
   命令改写会扩大 Aida 客户端和运行时边界；
2. 若 v1/v2 共用 `build_content_slice_digest`，两个 Worker 可能互相 claim，
   导致错误版本处理、失败重试或 Revision 缺失；
3. 仓库已经存在 `019_session_upload_content_status.sql`，固定使用
   `019_report_digest_v2_read_mode.sql` 会产生 migration 冲突；
4. v2 虽不进入上传事务，但 Digest/content/usage/metering Worker 运行在同一
   API 进程并共享 PostgreSQL，仍可能通过 CPU、内存、连接和 IO 竞争间接拖慢
   上传与 Token projection；
5. Token 数值来自独立 usage/metering 链路，Session 上传由
   Prepare/Chunk/Finalize 完成。v2 不需要修改这些值和协议，但必须用固定
   fixture 证明“未改”；
6. 专用 parser 若依赖关键词、读取 tee 文件或把不认识的输出回退为原文，会
   重新引入误判、泄密和容量风险。

### 修改

- RTK 仅作为 Reducer Registry、结构化格式优先、失败聚焦和稳定去重的参考；
- 明确禁止 RTK 二进制依赖、Hook、命令改写、tee 和 raw fallback；
- 新增独立 `api/internal/sessiondigestv2` 包，暂不重构 v1；
- 新增 `build_content_slice_digest_v2`，v1/v2 Reconciler 和 Worker 使用独立
  job type 与 claim 范围；
- v2 Worker 默认关闭，首期并发/batch 固定为 1，回填限速；
- migration 改为开发时按目标分支和生产最高版本选择真实下一个编号；
- 增加 Session 上传一致性、Token 数值一致性、核心 job age、API/数据库资源
  门禁；
- 明确 v2 异常时先切回 v1，再关闭 v2 build，不删除上传、job 或 Revision；
- 修正 14.159 执行边界为当前主机本机只读采集。

### 结论

有条件通过。架构已从“修改现有 Digest Worker”收口为“独立、默认关闭、可
快速停用的 v2 旁路”。只有上传、Token 和资源隔离测试通过后，才能进入生产
接入；文档通过不等于实现已通过。

## 跨文档一致性检查

| 项目 | 一致值 |
| --- | --- |
| Digest version | 当前 `session-digest/v2.4`；v2.1 为报告期优先级基线 |
| read/content mode | `digest_v2` |
| Revision / period item / selection target-hard | 64 KiB / 16 KiB / 64-128 KiB |
| Work Unit status | completed/partial/blocked/failed/pending/unknown |
| evidence grade | A/B/C/D |
| corpus 层级 | Full/Benchmark/Gold/Holdout |
| 生产 Builder LLM | 禁止 |
| raw fallback | 禁止 |
| RTK 使用方式 | 只借鉴 reducer，不引入运行时依赖 |
| v1 job type | `build_content_slice_digest` |
| v2 job type | `build_content_slice_digest_v2` |
| 生产默认开关 / 首期并发 / batch | 关闭 / 1 / 1；14.157 测试环境显式开启 |
| 隐藏灰度 | 禁止 |
| Session 上传协议 | 不修改 |
| Token 数值与口径 | 不修改，必须固定 fixture 校验 |
| target Session | `019f4575-4fe7-72a1-86d8-b6a4c719a73e` |

## 需求追踪检查

- `SDR2-FR-001` 至 `SDR2-FR-014` 均在测试文档有映射；
- NFR 的确定性、容量、安全、性能、兼容和质量均有门禁；
- 运行隔离 NFR 已覆盖上传、Token、任务 claim、队列和进程资源；
- target Session 有 Digest 和日报双层断言；
- 14.159 本机执行边界已明确；全量 inventory 未执行前不误报为完成；
- 第一阶段 v1 保留，不回退原文；
- Session 上传和 Token 统计不做功能改动，但作为非回归门禁纳入测试和发布。

## 第五轮：实现架构与兼容 Review

### 检查

- v1/v2 是否共用 job type；
- v2 是否进入 Session 上传事务；
- v2 模式下是否还能继续构建 v1，便于快速回退；
- Compose 是否真正把 v2 配置传入容器；
- migration 是否与仓库当前 `019` 冲突。

### 发现与修正

1. v2 已使用独立 `build_content_slice_digest_v2`，Worker 只 claim 该类型；
2. 上传 Prepare/Chunk/Finalize、usage/metering 和客户端协议均未修改；
3. 初版主进程在 `digest_v2` 模式下没有继续启动 v1 Builder，已修正为同时构建 v1；
4. 初版 Compose 未透传 `SESSION_DIGEST_V2_*`，会导致代码存在但容器永远无法启用，已同时修正开发 Compose 和生产单端口模板；
5. migration 使用真实下一个编号 `020`，在独立库和 14.157 开发库均执行成功。

### 结论

通过。v2 是可关闭的旁路，不改变上传和 Token 数据流；v1 快速回退仍可用。

## 第六轮：质量、安全与容量 Review

### 检查

- 系统指令、approval assessment、reasoning、凭据和大工具正文是否进入摘要；
- Agent 最终回复是否被误当成无条件事实；
- 单 item 是否具有真正的硬字节上限；
- 目标 Session 和大 Session 是否仍以结果为主。

### 发现与修正

1. approval-assessment 初版可能先创建工作单元再识别评估头，已改为整段 Session 忽略并清空历史状态；
2. Agent 最终回复只以 `agent_claim_with_evidence` 进入结果候选，Skill 明确禁止在缺少独立证据时复述精确 commit、部署状态或 clean worktree；
3. Top 30 回放未发现系统指令被识别为目标，reasoning、凭据和大工具正文未进入 Digest；
4. Review 构造出 5,000 个 `evidence_refs` 的病理输入，发现单条结果引用可能突破 16 KiB；已增加引用裁剪和最小安全表示，新增硬预算测试；
5. 目标 Session 从问答式扁平摘要提升为 2 个结果型 Work Unit，保留 8 条结果、16 项变更、6 项验证和 3 个未完成项。

### 结论

通过。当前实现无 raw fallback，单 item 具有确定性硬预算；语义型 Agent claim
仍是候选证据，不是无条件事实。

## 第七轮：数据库、并发与存量链路 Review

### 检查

- migration、冻结、单页读取和写回完整性；
- v1、上传、Usage、Token Analytics 非回归；
- race/vet；
- 生产与离线 evaluator 的资源模型差异；
- Skill 不可变版本和显式全量切换。

### 结果

- `go test ./...`：通过；
- `go vet ./...`：通过；
- `go test -race ./internal/sessiondigestv2 ./internal/reportsource`：通过；
- 独立 PostgreSQL 中 `db/sessiondigest/sessiondigestv2/reportsource/sessionsync/usage/tokenanalytics`：通过；
- 14.157 migration `019 -> 020`：通过；
- v2 selection freeze/read/write guard：通过；
- 测试 Skill 使用不可变版本链；该轮为 1.0.10，当前最终版本为 `100866/aida-report@1.0.13`；
- 14.157 显式配置 `digest_v2`、v2 build true、batch 1、canary 空，未使用 shadow。

### 剩余风险

1. 离线 evaluator 会整体物化单个 JSONL；它不能替代生产 Processor 混合负载门禁；
2. v2 与 content/usage/metering 仍共享 API 进程和 PostgreSQL，回填期间必须观察队列年龄和资源；
3. reducer 首期只覆盖常见测试、Git、Docker 和 HTTP 命令，未知输出保持 unknown，不回退原文；
4. selection 过多时仍可能触发 128 KiB hard limit，行为是明确失败而不是 raw fallback；
5. 完整 corpus、Human Gold 和正式 v1/v2.1 配对 A/B 尚未完成。

### 结论

测试环境接入通过；生产发布尚不满足全部质量门禁。

## 第八轮：报告期优先级与 v2.1 Review

### 真实问题

首版 v2.0 把完整 Session 先压缩到 16 KiB，再按日报 period 选择内容。目标
Session 跨越多日，历史高分 Work Unit 会先占满预算，导致 2026-07-16 的真实
结果被裁掉。真实 Agent 因此只能读取旧内容，甚至可能借用已有日报补全。

### 修正

- Digest 升为 `session-digest/v2.1`；
- Revision 预算调整为 64 KiB；
- 在 Work Unit 详细裁剪前按 Asia/Shanghai 业务日期生成 `daily_summaries`；
- selection 冻结时生成 `report_period_summary`；
- 报告期单 item 使用 16 KiB 预算，优先保留当天 highlights；
- 个人 managed report 有 selection 时禁止调用 `get_existing_report`。

### 验证

- 目标本地文件 53,743,142 bytes、19,545 events；
- 2026-07-16 投影 14,035 bytes，保留 8 个 highlights；
- 14.157 冻结 selection `coverage.complete=true`，最终 payload 10,916 bytes；
- v1 Revision、上传、Token 和历史 selection 不改。

### 结论

通过。容量边界仍受控，报告期结果不再被跨日历史内容挤掉。

## 第九轮：真实 Agent 轨迹与输出质量 Review

### 发现

Skill 1.0.9 的首次真实 run 成功写回，但暴露两点：

1. 模型把 UUID 形态的 selection ID 误放入 `selected_session_slice_keys`，失败后
   才改用 `report_source_selection_id`；
2. 正文出现“完成 5 项，其中 1 项完成”的状态矛盾，并过度突出文件数量。

### 修正

- MCP schema 明确 selection UUID 只能放入 `report_source_selection_id`；
- Skill 和启动提示同步禁止混用旧字段；
- 混合状态使用“有结果工作项”，分别说明 completed/partial；
- 文件清单和 `change_count` 只作证据，不作为成果主语。

### 最终验证

- Skill：`100866/aida-report@1.0.10`；
- run：`94657d2f-1c00-4ec9-9ea7-ac368a610ac9`；
- `get_sessions` 1 次，`get_existing_report` 0 次，`write_report_result` 1 次；
- 约 20 秒成功写回；
- 正文按 1 项完成、4 项进行中组织，不再逐轮复述聊天。

### 结论

通过。真实 Agent 主链路与结果优先产品形态成立；低价值 partial、汇总状态和
文件计数仍可能形成展示噪声，应由后续 Gold/Pairwise 指标继续约束。

## 第十轮：当前会话 v2.2 真实流程 Review

### 发现

- 当前会话经 Aida 上传后形成多个真实切片，说明只优化单 item 不足以解决结果
  重复；
- 日报包含 7 条流水账；
- v2.1/v2.2、旧阶段与最终阶段并列；
- 主机、测试和验证信息进入正文。

### 修正

- 引入选择级 `report_period_summary`；
- Report Agent 只使用选择级 highlights；
- 相同主题按时间和最终状态归并；
- Skill 限制个人日报条目和固定产品结构。

### 结论

未通过，作为 v2.3 修正基线。

## 第十一轮：v2.3 选择级归并 Review

### 结果

第二次真实 Agent 已收敛为 4 条完成和 1 条待跟进，跨切片重复明显减少。

### 剩余问题

正文仍出现 URL、文件路径、文件名、版本号和测试验证描述。仅靠 Prompt 无法
保证模型严格遵守。

### 修正

- 扩大 Skill 禁止输出范围；
- 写回端增加确定性正文质量门禁；
- Digest 升级为 v2.4，强化版本化产物和同主题最终状态覆盖。

### 结论

核心归并有效，但正文产品化未通过。

## 第十二轮：v2.4 最终真实 Agent Review

### 验收证据

- selection：`f9472e92-3d15-4f49-b5d3-ef5cce8ab228`；
- run：`152bc18d-8fd4-4470-9740-c16975fa31a7`；
- Agent Session：`d8aab2fc-c636-4281-81c1-3f5e602853a2`；
- Skill：`100866/aida-report@1.0.13`；
- `get_sessions` 1 次，`get_existing_report` 0 次；
- `write_report_result` 前两次被质量门禁拒绝，第三次保存；
- 最终正文为 4 条成果和 1 条待跟进；
- 不含文件变更、验证状态、URL、路径、主机、账号和精确资产版本。

### 结论

通过当前会话真实全链路验收。服务端质量门禁已证明能阻止不合格正文落库。
首稿仍可能被拒绝两次，后续应继续优化选择级摘要表达，并监控重试 Token 和
时延，但不阻塞本次测试环境验收。

## Review 后剩余前置

这些不是文档缺陷，但会阻止真实数据验收：

1. 完成当前 14.159 全量只读 inventory；
2. corpus manifest 冻结；
3. Human Gold 标注人确认；
4. 正式 v1/v2.1 配对 A/B 和多 trial；
5. Session 上传、Token 完整性和混合负载基线采集；
6. 14.159 corpus 的 reducer 覆盖率与 `unknown` 分布确认。

## 最终结论

方案 Review、实施 Review 和当前会话三轮真实 Agent Review 均已完成。v2.4
代码、migration、Skill 1.0.13 与 14.157 测试环境显式切换已经完成，上传与
Token 固定 fixture 非回归通过，当前会话真实全链路通过。当前仍不能宣称
14.159 全量验证、Human Gold、正式配对 A/B 或生产资源门禁完成，也不能据此
直接发布生产。
