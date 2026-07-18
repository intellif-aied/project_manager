# 04：七轮 Review 记录

> Review 日期：2026-07-17；范围：本目录全部方案文档。
> 结论：R1 已完成；R5A/R5B 已获开发授权并完成隔离环境首轮实现，14.157 切换仍受本文件门槛约束。

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

通过。Report Source 和报告业务语义不变；Token 三维统计作为独立发布单元，API/前端/MCP 在同一内测版本协同切换，旧字段不静默改义。

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
- 是否能在测试服全量验证并用旧镜像回滚；
- 是否把 Token 改造绑进内容迁移；
- 性能验收是否覆盖有无日期和高页码；
- 是否存在未经授权的生产操作；
- Digest、MCP、Session 内容和 Token API 是否逐项回归；
- 三维 Token 是否有可执行的等式、回填和回滚门槛。

### 发现与修正

1. 增加执行计划硬门槛：候选 SQL 不得出现事件/Usage 明细表。
2. 将方案拆成 R1～R4 与 R5A/R5B/R5C，Token Contribution 建设、API 切换、兼容下线分离。
3. 将每个发布单元固定为“测试服全量对账 → 同版本整体切换 → 异常回滚旧镜像”。
4. 增加有/无显式活动日期、q、首页/中间页/末页的同路径性能用例。
5. 增加 MinIO 故障、哈希错误、游标断裂、迁移期兼容漏写和回滚用例。
6. 明确文档确认、开发授权和生产发布授权是三件不同的事。
7. 增加 `REG-*` 接口矩阵，Digest/MCP/其他 API 必须逐项回归。
8. 增加 `TOK-3D-001` 至 `TOK-3D-015`，覆盖 Subagent、跨 Chunk Advance、跨日、重试、重建和成本。

### 结论

通过。方案可执行且破坏面可控，不需要一次性重写现有系统。

## 第四轮：开发前执行性 Review

### 检查问题

- 目标表是否已经被其他会话部分实现，导致方案与当前代码重叠；
- 当前候选、Digest、Usage、MCP 路径是否仍与现状文档一致；
- 数据迁移、测试服全量对账、整体切换、镜像回滚是否足以约束开发不跑偏；
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
7. Q-01～Q-04 均已明确默认处理或后续发布门槛，不阻塞 R1 的 Catalog、回填、测试服全量对账和整体切换开发。

### 开发可执行性判断

| 范围 | 结论 | 原因 |
| --- | --- | --- |
| R1：Report Source Slice Catalog | **GO** | 产品语义、字段、索引、写入时机、查询白名单、全量对账、性能和镜像回滚均已明确 |
| R2：MinIO Reader | 条件 GO | 可以设计开发，但整体切换前必须完成 legacy object ledger 与对象完整性盘点 |
| R3：停止写完整 Payload | NO-GO | 必须等待全部消费者迁移和 MinIO 全量对账通过 |
| R4：历史载荷下线 | NO-GO | 必须另行取得生产清理授权、备份、观察期和容量方案 |
| R5A：Contribution/三维 Rollup | 条件 GO | 模型已可实现，但应单独开发、回填与全量对账，不与 R1 首批代码并行混提 |
| R5B/R5C：API 切换与旧路径下线 | NO-GO | 必须等待 R5A 数据对账、字段兼容和 MCP ad-hoc 回归 |

### 开始 R1 前的五项动作

1. 以已提交的本目录方案为基线，为 R1 创建干净的独立分支/worktree；不得把 `doc/v1/db-backups/` 混入开发提交。
2. 开始编码前重新检查 HEAD、最高 Migration 与其他会话改动，再分配 Migration 编号；当前虽然最高为 020，但文档不提前锁死下一个编号。
3. 先保存现有候选接口契约和代表性真实数据基线，再新增 Catalog；不得以新结果替代基线定义。
4. R1 只允许“新表 + 写入/回填 + 测试服全量对账 + 目标读路径整体切换”；不增读取模式开关，不得在前台请求内同步跑两套重查询。
5. 切换 catalog 读取前执行 `REG-RS-*`、分页 SQL `EXPLAIN`、有无显式活动日期、空页/深页、权限和前台 p95 验收。

### 第四轮结论

通过。方案层面已经可以执行开发，但当前只正式放行 R1。R2、R5A 可以在各自前置检查完成后单独立项；R3、R4、R5B、R5C 不能因本次 Review 自动开始。

本目录方案已在 `fa6c615` 提交。当前远端 HEAD 又合入了其他会话的 Digest 改动，且仍有无关未跟踪目录 `doc/v1/db-backups/`。这不阻塞本轮文档修正，但 R1 真正编码前必须重新确认 HEAD 并隔离干净 worktree，避免与其他会话混提。

## 第五轮：内测开发期简化 Review

### 用户确认的新边界

- 当前产品仍在内测开发期，不需要用户灰度、百分比 rollout 或多读写模式配置；
- 过渡期新旧表临时并存只用于附加式迁移和可回退数据边界，不包装成发布开关；
- 数据 `revision/version` 仍必须保留，但只服务于报告冻结、Token 重算、family 关系、价格和审计正确性。

### 一致性复核

1. README、总方案、架构、关键决策、迁移测试、Digest/MCP 回归、Token 三维和端到端数据流已统一为同一切换模型。
2. 每个发布单元执行 `开发 -> 14.157 全量回填/对账/回归 -> 必要时上线附加式准备版本完成生产回填 -> 切换版本整体切换 -> 异常回滚旧镜像`；准备版本不改变用户读路径。
3. 删表、清理旧 Payload 和下线旧 Token 路径仍是后续独立发布单元，不与读路径切换同时执行。
4. 因此删除灰度机制不会弱化数据安全和可回滚性，只会减少内测期无效配置和分支组合。

### 当前环境复核

- 2026-07-17 复核时远端分支为 `fea.0.0.1`，HEAD 为 `58b77ed`，文档基线 `fa6c615` 已在其历史中；
- HEAD 包含其他会话的 Digest Stage 3 合并，工作区只见无关未跟踪 `doc/v1/db-backups/`；
- 本轮仍只修正方案，不因第五轮 Review 自动开始 R1 开发。

### 第五轮结论

通过。R1 的技术方案仍为 **GO**，但发布设计收敛为内测期最小必要流程；后续 Codex 不得自行恢复灰度开关、用户分组或双读路径。

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
| 测试服全量对账、整体切换、镜像回滚完整 | 通过 |
| 生产清理保护条件明确 | 通过 |
| 端到端数据流与各状态边界一致 | 通过 |
| Digest 未就绪竞态已独立登记且不弱化门禁 | 通过 |

## Review 后仍需人工决定

- MinIO 保留和备份策略；
- R4 旧载荷表下线观察期；
- R5B 新增 Token 字段的最终前端展示和成员下钻入口。

## 第六轮：Token 在线查询事故补充 Review（2026-07-18）

### 新证据

1. R1 已完成 Catalog 开发、测试服全量回填/对账和目标接口切换，Report Source 候选查询不再扫描事件表；
2. 用户 `303` 的三天 Token 范围只有 64 个 Session，却被
   `/token-analytics/summary` 物化为 18,030 个 Snapshot Item，冷查询超过 30 秒；
3. `/tokens`、`/tokens/sessions` 和工作台仍读取停止更新的
   `session_activity_slices`，七条返回数据只对应三个旧 Session；
4. 现有 R5 的 Contribution、三维 Rollup 和轻量 Snapshot 方向正确，但消费者清单遗漏了
   两个旧 Token API、工作台 `fetchAllSessionTokens`，以及需求/任务“关联工作记录”。

### 结论

- R5A：**GO**。在独立 worktree 开发附加表、异步构建、历史回填和全量对账，不改变在线读路径；
- R5B：保持条件 GO。只有 R5A 三维等式、成本、Subagent 和真实高基数性能全部通过后，
  才允许 API、工作台、Token 页面和 MCP ad-hoc 同版本整体切换；
- R5C：本期不执行，不删除旧 Snapshot、旧表或兼容字段；
- 不增加灰度/双读配置，不通过增加 30 秒前端超时或同步回填旧表止血。

用户已于 2026-07-18 明确授权开始本开发单元；生产迁移、生产发布和历史数据清理仍需单独授权。

## 第七轮：R5 首轮实现一致性 Review（2026-07-18）

### 代码与方案对照

1. Contribution 只在 Claude Advance/Codex checkpoint 产生不可变增量，重复 Observation 不增数；
2. family total = daily = chunk = Contribution = active Component 在激活事务内校验；
3. 新 Snapshot 只冻结 root family Rollup 引用，summary/trends/rankings/sessions 不读 Component；
4. `/tokens*`、工作台、需求/任务关联和 MCP ad-hoc 均已纳入同版本消费者迁移；
5. 发现并补上版本化 Rollup 的容量边界：HTTP 不再做批量删除，过期 Snapshot 与无引用
   superseded Rollup 由有锁/语句超时的小批后台任务回收；
6. 回填检查新增 `unsafe_sources`，Chunk 游标或 Metering 输入不完整时不得宣告完成；
7. 64 root / 18,030 逻辑事实的隔离用例只物化 64 条引用，耗时低于 25ms；这不是 14.157 p95/p99 结论。

### 无灰度的实际发布边界

不增加运行时开关，但必须形成两个独立 Git/镜像提交：

1. **R5A 准备提交**：Migration、parser v5、Contribution、成本、family/Rollup、回填和后台回收；在线读路径仍保持上一版本；
2. 在 14.157 部署 R5A，低压回填至 `unsafe/missing/building/failed/dead/reconciliation` 全为零，完成三维与成本对账；
3. **R5B 切换提交**：API、工作台、需求/任务关联和 MCP ad-hoc 一次性改读 Rollup；
4. R5B 回归失败直接部署上一已验证 R5A 镜像，不做用户灰度或双读切换。

14.157 在部署前发现已有无 Contribution/Family 实表的 `schema_migrations.version=22`；
R5A 迁移因此改用新的正向编号 `023`，禁止删除历史版本记录或复用 `022`。

### 第七轮结论

- 继续开发与提交：**GO**；
- 14.157 部署 R5A 准备版本：**已执行**；迁移 023 、API 健康和压力保护正常；
- 14.157 部署 R5B：**已执行**。14.157 已确认为全部可丢弃测试数据，因此历史异常来源不再作为测试服部署门禁；
- 生产迁移/发布/清理：未授权。

14.157 真实回填的当前门禁结果为 `eligible=228 / active=215 / failed=8 / building=5 /
dead_jobs=3 / reconciliation=0`。其中 3 个 building 来源的 MinIO key 不存在，另 2 个为
replacement revision 回退非稳定事实。2026-07-18 已确认这些均为测试环境旧数据，不继续修复，
也不据此阻断 14.157 的 R5B 功能与性能验证。生产环境仍必须使用生产数据重新执行完整回填、
对账和来源处置，不得引用本次测试环境结论跳过生产门禁。
