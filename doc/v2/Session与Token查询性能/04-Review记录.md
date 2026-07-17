# 04：四轮 Review 记录

> Review 日期：2026-07-17；范围：本目录全部方案文档。
> 结论：方案内部一致、具备分阶段开发条件；开发前执行性 Review 放行 R1，是否开始开发仍需人工明确授权。

## 第一轮：产品语义 Review

### 检查问题

- 是否把 Session 结束当作切片可用前提；
- 是否用报告周期日期隐式过滤候选；
- 是否仍为 Report Source 计算/展示 Token；
- 是否改变完整切片冻结语义；
- 是否把 processing/failed 运维状态混进选择器；
- 是否把 Report Source 和 Token Analytics 合并；
- Session 总量是否明确包含 Subagent，生命周期总量与日期小计是否混淆；
- MCP 三条 Session 读取路径是否被误当成同一接口。

### 发现与修正

1. 旧文档仍写 Report Source 展示“Token 提示值”，已删除。
2. 明确 `period_start/end` 只作为报告上下文，不能过滤候选。
3. 明确只有 `activity_from/to` 是显式活动日期筛选。
4. 保留“一次上传一个切片、选择后冻结完整切片”。
5. Report Source 与 Token Analytics 保持两套读模型。
6. 明确 root Session 总量包含全部 Subagent，默认列表按 root family 去重。
7. 明确生命周期、按天、按新增 Chunk 三维统计和各自字段语义。
8. 区分 MCP attached digest、attached full、ad-hoc 日期查询三条路径。

### 结论

通过。Report Source 和报告业务语义不变；Token 三维统计作为明确新增需求独立灰度，旧字段不静默改义。

## 第二轮：数据与存储 Review

### 检查问题

- PostgreSQL 与 MinIO 谁是原始内容权威源；
- 是否错误地认为删除 superseded 数据就足够；
- 是否存在直接清空 `content_payload` 的破坏性路径；
- Digest 等现有消费者能否继续工作；
- 历史空间如何安全回收；
- 当前 Usage Component 能否准确表达新增 Chunk Delta；
- Subagent 父历史是否可能再次进入子 Agent 总量。

### 发现与修正

1. 生产证据表明 PostgreSQL TOAST 与 MinIO 原始内容形成大载荷重复，已明确 MinIO 为唯一字节级事实源。
2. superseded 事件约占 4.4%，不能作为主体优化。
3. 否决批量 `UPDATE content_payload=NULL`，改为新增轻量事件索引并最终整体下线旧载荷表。
4. 增加统一 Content Event Reader，Digest/冻结来源先迁移，再停止写 Payload。
5. 增加对象哈希、游标、缺失对象和恢复验证门槛。
6. 确认 Component 是 Logical Event 当前值；Claude 跨 Chunk Advance 不能直接按 Component `chunk_id` 汇总。
7. 增加不可变 Usage Contribution，以 Observation 增量统一生成 family/daily/chunk Rollup。
8. family membership 版本化，父历史在可信 fork 边界前只建立 baseline。

### 结论

通过。内容存储和 Token 均有唯一权威边界；方案不会提前删除生产数据，也不会建立三套互不相干的 Token 口径。

## 第三轮：开发、回滚与测试 Review

### 检查问题

- 分页是否真正与事件量解耦；
- 开发单元是否过大；
- 是否能影子验证和单独回滚；
- 是否把 Token 改造绑进内容迁移；
- 性能验收是否覆盖有无日期和高页码；
- 是否存在未经授权的生产操作；
- Digest、MCP、Session 内容和 Token API 是否逐项回归；
- 三维 Token 是否有可执行的等式、回填和回滚门槛。

### 发现与修正

1. 增加执行计划硬门槛：候选 SQL 不得出现事件/Usage 明细表。
2. 将方案拆成 R1～R4 与 R5A/R5B/R5C，Token Contribution 建设、API 切换、兼容下线分离。
3. 增加独立读写开关，`shadow` 不改变用户响应。
4. 增加有/无显式活动日期、q、首页/中间页/末页的同路径性能用例。
5. 增加 MinIO 故障、哈希错误、游标断裂、双写漏写和回滚用例。
6. 明确文档确认、开发授权和生产发布授权是三件不同的事。
7. 增加 `REG-*` 接口矩阵，Digest/MCP/其他 API 必须逐项回归。
8. 增加 `TOK-3D-001` 至 `TOK-3D-015`，覆盖 Subagent、跨 Chunk Advance、跨日、重试、重建和成本。

### 结论

通过。方案可执行且破坏面可控，不需要一次性重写现有系统。

## 第四轮：开发前执行性 Review

### 检查问题

- 目标表是否已经被其他会话部分实现，导致方案与当前代码重叠；
- 当前候选、Digest、Usage、MCP 路径是否仍与现状文档一致；
- 数据迁移、开关、影子、回滚和测试是否足以约束开发不跑偏；
- 尚待人工确认项是否会阻塞第一发布单元；
- 当前分支、Migration、运行服务和工作区是否适合立即开始写代码；
- 新发现的 Digest 未就绪竞态是否需要并入本专项或阻塞 R1。

### 当前代码与环境证据

1. 当前分支为 `fea.0.0.1`，Review 时 HEAD 为 `caca7ed`；最高数据库 Migration 为 `020_report_digest_v2.sql`。
2. `api`、`db`、`minio`、`web` 均在运行，数据库与 MinIO 健康；本轮未重启或修改任何服务。
3. 当前代码不存在 `report_source_slice_catalog`、`session_content_event_index`、`session_usage_contributions`、`session_family_memberships` 和三维 Rollup 表，未发现需要接管的半成品实现。
4. 当前 `ListCandidates` 仍扫描 `session_content_events` 计算活动时间与摘要，R1 的问题入口和替换边界仍然成立。
5. 当前 Digest V2 在内容 fully indexed 后由 5 秒 Reconciler 和单批 Worker 异步生成；候选 ready 早于 Digest ready 的竞态已单独登记为 [AIDA-BUG-20260717-006](../bug清单/AIDA-BUG-20260717-006-报告生成Digest未就绪时序竞态.md)。
6. Digest 竞态不改变 R1 Catalog 的内容就绪语义，也不允许候选查询 JOIN Digest JSON；因此它作为后续交互/调度优化，不阻塞 R1。
7. Q-01～Q-04 均已明确默认处理或后续发布门槛，不阻塞 R1 的 Catalog、回填、影子和灰度开发。

### 开发可执行性判断

| 范围 | 结论 | 原因 |
| --- | --- | --- |
| R1：Report Source Slice Catalog | **GO** | 产品语义、字段、索引、写入时机、查询白名单、影子对账、性能和回滚均已明确 |
| R2：MinIO Reader | 条件 GO | 可以设计开发，但进入灰度前必须完成 legacy object ledger 与对象完整性盘点 |
| R3：停止写完整 Payload | NO-GO | 必须等待全部消费者迁移和 MinIO shadow 通过 |
| R4：历史载荷下线 | NO-GO | 必须另行取得生产清理授权、备份、观察期和容量方案 |
| R5A：Contribution/三维 Rollup | 条件 GO | 模型已可实现，但应单独开发和影子，不与 R1 首批代码并行混提 |
| R5B/R5C：API 切换与旧路径下线 | NO-GO | 必须等待 R5A 数据对账、字段兼容和 MCP ad-hoc 回归 |

### 开始 R1 前的五项动作

1. 先提交本目录方案文档，或为 R1 创建干净的独立分支/worktree；不得把 `doc/v1/db-backups/` 混入开发提交。
2. 开始编码前重新检查 HEAD、最高 Migration 与其他会话改动，再分配 Migration 编号；当前虽然最高为 020，但文档不提前锁死下一个编号。
3. 先保存现有候选接口契约和代表性真实数据基线，再新增 Catalog；不得以新结果替代基线定义。
4. R1 第一批只允许“新表 + 写入/回填 + 异步 Shadow”，读取开关默认保持 legacy；Shadow 不得在前台请求内同步跑两套重查询。
5. 切换 catalog 读取前执行 `REG-RS-*`、分页 SQL `EXPLAIN`、有无显式活动日期、空页/深页、权限和前台 p95 验收。

### 第四轮结论

通过。方案层面已经可以执行开发，但当前只正式放行 R1。R2、R5A 可以在各自前置检查完成后单独立项；R3、R4、R5B、R5C 不能因本次 Review 自动开始。

当前远端工作区仍包含整套未提交方案文档和无关的 `doc/v1/db-backups/`。这不是架构阻塞，但在真正写代码前必须先提交文档或隔离 worktree，否则后续无法形成干净、可审计的 R1 提交。

## 最终一致性检查

| 检查项 | 结果 |
| --- | --- |
| 产品契约在各文档中一致 | 通过 |
| Report Source 不再包含 Token | 通过 |
| period 与 activity 日期语义一致 | 通过 |
| MinIO/PG 权威边界一致 | 通过 |
| 候选分页不触碰事件表 | 通过 |
| Digest 消费者迁移先于 Payload 清理 | 通过 |
| Digest/MCP 外部契约和完整性门禁保持 | 通过 |
| Token 家族/按天/按 Chunk 共用 Contribution | 通过 |
| Subagent 父历史不重复、成员行不与父行重复求和 | 通过 |
| 生命周期总量与范围小计字段分离 | 通过 |
| Token Analytics 独立分阶段演进 | 通过 |
| 双写、影子、灰度、回滚完整 | 通过 |
| 生产清理保护条件明确 | 通过 |
| 端到端数据流与各状态边界一致 | 通过 |
| Digest 未就绪竞态已独立登记且不弱化门禁 | 通过 |

## Review 后仍需人工决定

- 是否授权开始 R1 开发，以及开发前采用提交文档还是独立 worktree；
- MinIO 保留和备份策略；
- R4 旧载荷表下线观察期；
- R5B 新增 Token 字段的最终前端展示和成员下钻入口。
