# Digest 按需构建与报告等待

> 状态：P0 开发基线，尚未开发
> 对应问题：[`AIDA-BUG-20260717-006`](../bug清单/AIDA-BUG-20260717-006-报告生成Digest未就绪时序竞态.md)
> 设计基准：[`开发方案设计基准.md`](../../开发方案设计基准.md)

## 1. 固定产品口径、需求编号与设计结论

用户点击“智能生成”后，仍按现有产品语义创建一次持久化 `ai_run` 并立即返回 `run_id`：

```text
用户点击智能生成
  -> 校验请求并冻结本次生成配置
  -> 新请求创建 Selection；重复请求返回已有 Run 和已有 Selection
  -> 创建 ai_run(status=pending)
  -> 返回 run_id
  -> 前端继续展示现有“生成中”

后端 Report Run Processor
  -> 确保 Digest
  -> 构建不可变 Report Context
  -> 提交 Agent
  -> Agent 读取 Context 并写回报告
  -> ai_run succeeded / failed / timeout
```

Digest 是 `ai_run` 的内部执行依赖，不形成新的前端业务流程。页面关闭、网络断开或前端进程退出后，后端必须继续推进。

本期需求使用以下稳定编号，后续架构、开发任务和测试均引用这些编号：

| 编号 | 产品规则/验收目标 |
| --- | --- |
| R1 | 一次用户提交在本地最多创建一个 Selection 和一个持久化 `ai_run`；HTTP 立即返回 `run_id` |
| R2 | Digest missing/pending/building 时由后端创建、提权或复用 Job；ready/failed 后自动恢复或终结原 Run，不依赖前端在线 |
| R3 | Run 创建时冻结报告范围、Session 来源身份、版本和运行配置；Digest ready 后一次性冻结 Digest 与完整 Report Context |
| R4 | Selection attached 与 Digest frozen 分离；Context Builder、Report MCP 和写回必须经过统一冻结完整性门禁 |
| R5 | Report Run Processor 使用阶段、lease、heartbeat、有界重试和 Reconciler 幂等推进；等待 Digest 时立即释放执行槽和 lease；其并发由独立的报告执行配置控制，不属于 Digest 容量 |
| R6 | Digest 只有 background、interactive 两类 Worker，分别使用独立且无实现版本号的运行配置；首发均为 20；修改并发只需修改部署配置并重启进程，不得修改代码或重建镜像；5000 个 background Job 不得造成 interactive Claim 随队列长度线性退化 |
| R7 | pre-Agent Run 不进入 Agent Status Sync；外部 Session ID 已落库时不重提，提交结果未知时保守失败 |
| R8 | 用户只提交一次；Digest 和 Report Context 不设大小硬上限；完整 Context 超过 1 MiB 时只记录并展示一次非阻断提醒，Run 继续执行，不增加前端二次确认或重试 |
| R9 | 保持 Selection、Digest hash、Context hash、MCP 写回门禁以及 Claude Code/Codex 上传和 Token 统计链路不变 |
| R10 | 上线时必须具备 Digest 两类队列、Claim/构建、Run 各阶段、lease、唤醒、Reconciler 和受控重建的指标、告警与运行手册 |
| R11 | 新生成的 Digest 使用 `session-digest/v2.10.0`：不按总字节数压缩或删除 Work Unit，不对进入 Digest schema 的文本做固定长度截断，不设置单条 Digest 或 Selection Digest 总大小硬上限；脱敏、schema 事件筛选、空白规范化和完全相同内容去重继续保留；历史 v2.9 Digest/Run 只读兼容 |

本目录四份文档共同构成本期 P0 基线。关联 Bug 文档只保留事故事实和关闭状态，不再承载另一套实现流程。

本期明确采用：

- `ai_run` 是唯一的报告生成请求，不新增 `report_generation_requests`；
- `ai_run.status` 保持现有 `pending/running/succeeded/failed/timeout`；
- 在 `ai_run` 上增加最小内部阶段和执行 lease，不引入通用状态机；
- Report Run Processor 直接从 `ai_runs` 领取待推进 Run，不把 Report Run 塞入要求 `session_id` 的 `session_processing_jobs`；
- Digest 继续使用现有 `session_processing_jobs`、Digest Worker、lease 和重试；
- Selection 只冻结来源选择，不表达等待、重试或执行状态；
- 前端只创建 Run 和读取 Run 状态，不推动后端继续执行；
- 不引入 River、Temporal、Celery 或其他队列/工作流框架。

## 2. 已知风险与固定处理

| 风险 | 用户实际会遇到什么 | 本期固定处理 | 是否阻塞本期开发/发布 |
| --- | --- | --- | --- |
| AIHub 不支持按 Run 幂等创建或查询 Session | CreateSession 请求超时后，Aida 无法确认 AIHub 是否已经创建 Session | Run 以 `EXTERNAL_SUBMISSION_STATE_UNKNOWN` 失败并停止自动重试；不得宣称外部 Session 绝对只创建一次 | 不阻塞开发；P04 必须通过后才可发布 |
| AIHub 不支持指定 Agent Version | 用户点击后、真正提交前 Agent 配置被修改，本次执行可能使用修改后的配置 | 创建 Run 时冻结 Agent ID、模型和输入；`agent_version_id` 在 AIHub 返回实际任务信息后记录，不声称点击时已冻结 Agent Version | 不阻塞开发；发布说明必须写明 |
| 完整 Context 晚于用户点击生成 | Digest 等待期间，需求、任务、下级报告或组织信息发生修改时，报告使用 Context 构建时读到的内容 | 点击时冻结 Session 来源；Digest ready 后一次性构建 Context，创建后不再重新取数 | 已确认的产品口径，不阻塞发布 |
| Context 不设大小硬上限 | 极大 Context 会增加内存、数据库、Token 和 AIHub 压力；所选模型也可能因上下文长度不足而失败 | 超过 1 MiB 只做一次非阻断提醒；不截断、不阻止提交；AIHub 明确拒绝时 Run failed，用户改用大上下文模型或减少来源后重新生成 | 已确认的产品口径，不阻塞发布 |
| Digest 从 v2.9 升级到 v2.10 | 部署后所有 active Slice 都缺少 v2.10 Revision，会形成一轮 background 重建；大 Digest 不再压缩，数据库、内存和模型输入都会增加 | 旧 v2.9 数据不删除、不重写；新 Run 只使用 v2.10；旧 Run 继续读取已冻结的 v2.9；interactive Worker 独享 20 个槽；P10 未通过不得发布 | P10 阻塞发布 |
| 三类执行角色首发均为 20 | 最忙时可同时存在 20 个 Report Run Processor、20 个 background Digest Worker 和 20 个 interactive Digest Worker；内存正常不代表数据库和 AIHub 一定能承受 | 测试服用相同配置持续满载 10 分钟；P09 未通过不得发布，不得现场改验收标准 | P09 阻塞发布 |
| Claude Code/Codex 上传和 Token 统计与 Digest 共用部分任务基础设施 | 错误修改通用 Claim 会破坏现有上传或统计 | 只新增 Digest 专用 Claim；P06 未通过不得发布 | P06 阻塞发布 |

## 3. 文档结构与唯一职责

| 文档 | 唯一职责 | 不承载 |
| --- | --- | --- |
| `README.md` | 固定产品口径、R1-R11、范围和阅读入口 | 状态机细节、代码任务、测试步骤 |
| [`架构设计.md`](架构设计.md) | 目标流程、模块接口、状态、冻结、调度、lease、失败与外部边界 | 文件级开发排期、测试用例正文 |
| [`开发方案.md`](开发方案.md) | 代码事实与差距、D01-D12、真实依赖和开发顺序 | 重复定义架构状态或验收标准 |
| [`测试与验收.md`](测试与验收.md) | T01-T43、P01-P10 和 R-D-T-P 追踪 | 新增产品规则或实现方案 |

阅读与执行顺序：

```text
README 固定需求
  -> 架构设计确定状态和模块接口
  -> 开发方案落实代码任务
  -> 测试与验收判定是否完成
```

变更规则：

- 产品口径变化先修改本文件的 R 编号，再同步检查架构、开发任务和测试追踪；
- 架构规则只修改《架构设计》，开发方案只引用其章节；
- 文件与任务范围只修改《开发方案》，测试文档只引用 D 编号；
- 通过标准只修改《测试与验收》，其他文档不得复制另一套标准；
- 任一 R 编号必须同时存在架构落点、D 任务和 T/P 验收；
- 不新增第五份平行方案文档。
