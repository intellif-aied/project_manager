# Session 与 Token 查询性能专项

> 状态：本专项完整版本未验收通过。2026-07-18 曾发生一次不完整发布：Token Rollup/API/Web/Skill 已上线，但 MinIO Reader、停止新增 PostgreSQL 完整 Payload 和历史 JSONB 清理未完成；该发布不代表本专项完成。后续所有生产发布必须使用 [完整发布清单](./09-完整发布清单.md)。

## 现在要做什么

开发进度唯一入口：[R1～R5 开发计划与进度](./00-R1-R5开发计划与进度.md)。

```text
R1：已完成
R2：进行中，下一步 R2.2 统一 MinIO Content Reader
R3：阻断，等待 R2
R4：阻断，等待 R2/R3 与清理演练
R5A/R5B：已完成并已发布
R5C：未开始
```

每次开发状态只按 R 编号更新；没有 R 编号、修改文件和验收结果的内容不算进度。

## 一句话结论

当前问题不是“分页参数没生效”，而是分页之前仍在从内容事件明细计算候选集合。正确方案是：

- Report Source 只查询切片目录读模型，分页成本只与候选切片数量有关；
- MinIO 保存唯一的原始 JSONL，PostgreSQL 只保留可重建的索引、摘要、Usage 和业务状态；
- Token Analytics 使用不可变的 Usage Contribution，预生成 Session 家族、按天、按新增 Chunk 三种可对账 Rollup；
- 开发阶段采用附加表、测试服全量对账、分阶段整体切换和 Git/镜像回滚；完整版本通过所有内容消费者门禁后，按 [完整发布清单](./09-完整发布清单.md) 执行历史 Payload 下线与空间回收。

2026-07-18 的测试服复核又确认了 Token 读路径断层：`/token-analytics/summary`
会把 64 个 Session 展开为 18,030 条 Component Snapshot Item，冷查询超过 30 秒；
`/tokens`、`/tokens/sessions` 和工作台仍读取停止更新的 `session_activity_slices`。
需求/任务的“关联工作记录”也复用了 `/tokens/sessions`。这不是目标架构方向错误，而是
原 R5 方案未完整列出旧接口及前端消费者，现已纳入
[AIDA-BUG-20260718-007](../bug清单/AIDA-BUG-20260718-007-Token查询快照超时与旧接口数据断层.md)及 R5B 切换范围。

## 已确认的硬边界

1. 一次成功增量上传形成一个可追踪切片，Session 无须结束。
2. `period_start`、`period_end` 是报告上下文，不是候选列表的隐式日期过滤。
3. 只有用户显式传入 `activity_from`、`activity_to` 时才过滤活动时间。
4. 候选分页 SQL 不得访问 `session_content_events`、Usage 明细或现场生成 Digest。
5. Report Source 不再展示或计算 Token，候选接口不得为 Token 统计付出成本。
6. MinIO 原始对象没有完成完整性校验、消费者迁移、测试服全量对账和回滚演练前，不得清理 PostgreSQL 历史载荷。
7. Report Source、原始内容存储治理、Token Analytics 可以分开发包，但生产发布必须按完整清单汇总验收；不得把任一子包宣称为专项完成。
8. 顶层 Session 总量包含全部层级 Subagent，但父历史不得在子 Agent 重复计数。
9. Session 家族总量、逐日总量、全部新增 Chunk 总量在同一版本下必须精确相等。
10. Digest 和 MCP 只允许内部换数据源，外部契约与完整性门禁不能弱化。
11. PostgreSQL Payload 清理不是可选优化，而是本专项完整版本的必选验收项；未完成内容消费者迁移前禁止清理，未完成清理前禁止写“原始内容只保留一份”。

## 文档导航

1. [R1～R5 开发计划与进度](./00-R1-R5开发计划与进度.md)
2. [生产事故复盘](./00-生产事故复盘-20260717.md)
3. [专项设计方案](./Session与Token查询性能专项设计方案.md)
4. [现状产品形态与核心边界](./01-现状产品形态与核心边界.md)
5. [目标架构与最小破坏演进](./02-目标架构与最小破坏演进.md)
6. [关键决策与待确认项](./03-关键决策与待评审问题.md)
7. [七轮 Review 记录](./04-Review记录.md)
8. [开发、迁移与测试验收](./05-开发迁移与测试验收.md)
9. [Digest 与接口影响回归矩阵](./06-Digest与接口影响回归矩阵.md)
10. [Token 三维统计与对账模型](./07-Token三维统计与对账模型.md)
11. [端到端数据流与状态流转](./08-端到端数据流与状态流转.md)
12. [完整发布清单](./09-完整发布清单.md)

## 推荐实施顺序

```text
Report Source 切片目录
  -> 内容读取从 PostgreSQL 全载荷迁到 MinIO
  -> 停止新增 PostgreSQL 全载荷副本
  -> 观察期结束后再清理历史副本

Token Contribution + Session 家族/按天/按 Chunk Rollup
  -> 轻量 Snapshot
  -> 旧 Token API、工作台、需求/任务关联、MCP ad-hoc 同步迁移
  -> 测试服功能、性能与新上传链路回归
  -> Token API、前端、MCP ad-hoc 同版本整体切换
```

## 当前执行边界

- 当前只允许在 `fea.0.0.1` 开发 R2；157 用于后续功能验证，不把测试数据结论直接继承到生产；
- 在完整发布清单未通过前，不删除、置空或批量更新生产 `content_payload`；通过 R2/R3/R4 全部门禁后，才允许按批准的清理步骤执行。
- 不为了变快而默认增加日期条件；
- 不重做 Token Analytics；Digest、MCP 和报告只迁移内容读取来源，不改变外部契约或业务语义；
- 内测开发期不建设用户灰度、百分比 rollout、双读 Shadow 或读写模式配置；
- 不在生产环境执行压测或破坏性迁移。
- 任何发布前必须先完成 [完整发布清单](./09-完整发布清单.md)；清单有一项不是“通过”，就只能继续开发，不能切换生产。

## 当前实现证据

- Migration `023_token_contribution_and_rollups.sql` 已建立 Contribution、成本、family、三维 Rollup、明确的 Rollup Revision Refs 和轻量 Snapshot 引用表；14.157 的 `schema_migrations` 已存在无对应表结构的历史版本 `22`，本次不删除或改写该记录，通过新的正向编号 `023` 避免跳过迁移；
- parser v5 已覆盖 Claude Advance、Codex checkpoint delta、迟到 parent 合并和幂等回填；
- 新 Snapshot 不再写 `token_query_snapshot_items`，在线清理也已移出 HTTP 请求，改为小批量后台回收；
- `/token-analytics/*`、`/tokens*`、工作台和 MCP ad-hoc 已在隔离分支切换到 Rollup；`/tokens/sessions` 分页复用首页 Snapshot，需求/任务关联按 root Session 保存；
- 隔离 PostgreSQL 的 64 个 root Session / 18,030 个逻辑事实样本，Snapshot 只写 64 条 Rollup 引用，单次用例耗时低于 25ms；该数值是合成回归证据，不替代 14.157 真实数据 p95/p99 验收；
- 14.157 已生成 78,230 条 active Contribution，三维/成本对账失败为 0；8 个 revision 未通过 conflict/incomplete quality gate、3 个测试来源的 MinIO key 缺失、2 个替换 revision 回退了非稳定事实。确认该环境全部为测试数据后，不再修复这 13 个历史来源，R5B 已切换；该处置不得复制为生产迁移规则。
- R5B 切换后，用户 303 的 `/tokens/sessions` 首次实测约 136ms、同 Snapshot 后续页约 14ms，三天 `/token-analytics/summary` 首次实测约 86ms；集成镜像重启后的冒烟请求分别约 31ms 和 14ms。以上是单次测试服证据，不冒充 p95/p99。
- 集成镜像稳定后各串行请求 20 次：`/tokens/sessions` p50 约 19.5ms、p95 约 27.6ms、最大约 35.8ms；三天 Summary p50 约 14.7ms、p95 约 16.8ms、最大约 17.0ms。该短样本不含并发负载，不能替代生产容量测试。
- 192.168.14.159 使用 Aida `0.1.15` 完成新上传回归：root `019f5eb3-0b81-79a0-a4e5-d0fbb526f940` 新增 9 个 Chunk，Usage 游标从 36,597,221 推进到 45,180,732 并由 pending 回到 ready；9 个内容索引、9 个 Usage、Digest v1/v2 任务全部完成。新增 933 条 Contribution、123,160,551 Token，family/daily/chunk 均为 412,854,348，self 377,828,027 + Subagent 35,026,321 与 family 精确相等；933 条新增成本全部绑定同一有效价格/汇率版本。两个稳定 Session 重复上传均为 `unchanged/chunks=0`，Contribution 和 Rollup 未变化。
