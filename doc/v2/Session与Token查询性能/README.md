# Session 与 Token 查询性能专项

> 状态：R1 已在 14.157 完成；R5A 已在 14.157 执行迁移和低压回填，228 个来源中 215 个激活，剩余 13 个被内容质量、对象缺失或替换覆盖门禁阻断；R5B 明确 NO-GO，未切换在线 Token/API/前端/MCP 读路径，未执行生产操作；更新时间：2026-07-18。
> 当前授权只覆盖独立 worktree 中的 R5A/R5B 开发和 14.157 验证，不包含生产迁移、生产发布或历史数据清理。

## 一句话结论

当前问题不是“分页参数没生效”，而是分页之前仍在从内容事件明细计算候选集合。正确方案是：

- Report Source 只查询切片目录读模型，分页成本只与候选切片数量有关；
- MinIO 保存唯一的原始 JSONL，PostgreSQL 只保留可重建的索引、摘要、Usage 和业务状态；
- Token Analytics 使用不可变的 Usage Contribution，预生成 Session 家族、按天、按新增 Chunk 三种可对账 Rollup；
- 所有改造均采用附加表、测试服全量对账、分阶段整体切换和 Git/镜像回滚，不直接改写或删除现有 7GB 事件表。

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
7. Report Source、原始内容存储治理、Token Analytics 分三条发布链路，禁止一次性大迁移。
8. 顶层 Session 总量包含全部层级 Subagent，但父历史不得在子 Agent 重复计数。
9. Session 家族总量、逐日总量、全部新增 Chunk 总量在同一版本下必须精确相等。
10. Digest 和 MCP 只允许内部换数据源，外部契约与完整性门禁不能弱化。

## 文档导航

1. [生产事故复盘](./00-生产事故复盘-20260717.md)
2. [专项设计方案](./Session与Token查询性能专项设计方案.md)
3. [现状产品形态与核心边界](./01-现状产品形态与核心边界.md)
4. [目标架构与最小破坏演进](./02-目标架构与最小破坏演进.md)
5. [关键决策与待确认项](./03-关键决策与待评审问题.md)
6. [七轮 Review 记录](./04-Review记录.md)
7. [开发、迁移与测试验收](./05-开发迁移与测试验收.md)
8. [Digest 与接口影响回归矩阵](./06-Digest与接口影响回归矩阵.md)
9. [Token 三维统计与对账模型](./07-Token三维统计与对账模型.md)
10. [端到端数据流与状态流转](./08-端到端数据流与状态流转.md)

## 推荐实施顺序

```text
Report Source 切片目录
  -> 内容读取从 PostgreSQL 全载荷迁到 MinIO
  -> 停止新增 PostgreSQL 全载荷副本
  -> 观察期结束后再清理历史副本

Token Contribution + Session 家族/按天/按 Chunk Rollup
  -> 轻量 Snapshot
  -> 旧 Token API、工作台、需求/任务关联、MCP ad-hoc 同步迁移
  -> 测试服全量回归与对账
  -> Token API、前端、MCP ad-hoc 同版本整体切换
```

## 当前执行边界

- R5A/R5B 只在独立 worktree 开发；R5A 对账通过前不切换在线 Token 读路径；
- 不删除、置空或批量更新生产 `content_payload`；
- 不为了变快而默认增加日期条件；
- 不把 Token Analytics、Digest、MCP 或报告生成一起重写；
- 内测开发期不建设用户灰度、百分比 rollout、双读 Shadow 或读写模式配置；
- 不在生产环境执行压测或破坏性迁移。

## 当前实现证据

- Migration `023_token_contribution_and_rollups.sql` 已建立 Contribution、成本、family、三维 Rollup、明确的 Rollup Revision Refs 和轻量 Snapshot 引用表；14.157 的 `schema_migrations` 已存在无对应表结构的历史版本 `22`，本次不删除或改写该记录，通过新的正向编号 `023` 避免跳过迁移；
- parser v5 已覆盖 Claude Advance、Codex checkpoint delta、迟到 parent 合并和幂等回填；
- 新 Snapshot 不再写 `token_query_snapshot_items`，在线清理也已移出 HTTP 请求，改为小批量后台回收；
- `/token-analytics/*`、`/tokens*`、工作台和 MCP ad-hoc 已在隔离分支切换到 Rollup；`/tokens/sessions` 分页复用首页 Snapshot，需求/任务关联按 root Session 保存；
- 隔离 PostgreSQL 的 64 个 root Session / 18,030 个逻辑事实样本，Snapshot 只写 64 条 Rollup 引用，单次用例耗时低于 25ms；该数值是合成回归证据，不替代 14.157 真实数据 p95/p99 验收；
- 14.157 已生成 78,230 条 active Contribution，三维/成本对账失败为 0；但 8 个 revision 未通过 conflict/incomplete quality gate、3 个测试来源的 MinIO key 缺失、2 个替换 revision 回退了非稳定事实，因此 R5B 不得切换。
