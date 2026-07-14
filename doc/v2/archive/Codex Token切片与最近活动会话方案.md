# Codex Token切片与最近活动会话方案

## 背景与目标

本方案处理两个相互关联的问题：

1. Codex 长会话的 `token_count` 事件存在累计值回退时，Aida 会将同一批 Token 重复计入 activity slice，导致 Token 统计严重膨胀。
2. `aida upload` 当前按 session 启动时间排序和展示。长时间持续使用的 session 即使今天仍有活动，也会被较晚启动的 session 挤到列表后面。

目标是保证 Token 总量不虚增，同时使用户在上传和浏览 session 时优先看到最近实际使用的会话。

本方案不修改 managed agent platform，不修改生产数据库表结构，不改变 `aida upload --all` 上传全部 session 的语义。

## 已确认事实

生产 session `019f1ad3-d349-7b82-ae5c-0eb82d59c764` 的原始 Codex JSONL 中：

- 有 3,320 条 `token_count` 事件。
- 最终和最大 `total_token_usage.total_tokens` 都是 `370,607,977`。
- 序列中有 371 次累计值回退，例如 `257,615,822 -> 258,400`。
- 现有 activity slice 却将 2026-07-02 写为 `66,660,550,879` Token。

这证明原始日志没有 667 亿 Token；膨胀发生在 Aida daemon 的 Codex 解析过程，而不是生产 API、数据库或 MinIO。

## 根因

`daemon/codex_scan.go` 将每条 `token_count.total_token_usage` 当作整个 session 的单调递增累计值，并在 `addCodexTokenDelta` 中用相邻事件差值分配 Token。

Codex 实际日志会在同一 session 内交替出现不同统计上下文或重新初始化的累计值。例如：

```text
257,615,822 -> 258,400 -> 258,209,157
```

现有逻辑在 `257,615,822 -> 258,400` 时识别出负差并跳过，但仍将 `258,400` 作为下一次基线。下一条升至 `258,209,157` 时，代码将约 2.58 亿视为新增 Token，再次累加。该模式反复出现，造成重复记账。

另有状态表达问题：`addCodexTokenDelta` 遇到负差后会将切片标记为 `unknown` 和 `is_estimated=true`，调用方随后无条件将策略覆盖为 `delta`。因此生产数据会出现 `is_estimated=true` 但 `token_slice_strategy=delta` 的矛盾状态。

## 方案一：Codex Token切片

### 口径

- session 总 Token：以原始日志最后一条 `token_count.total_token_usage` 为准。
- 每日精确 Token：仅在整个 session 的 input、cache read、output、total 四类累计字段均单调递增时计算。
- 存在任一累计字段回退时：原始日志无法可靠提供逐日精确 Token 分摊，不能继续使用差分累加。

### daemon 改动

1. 为 Codex 解析维护 session 级别的累计值校验状态，检查 input、cache read、output、total 四个字段。
2. 未发生回退的 session：保留现有按活动日期分配差分的逻辑，写入 `token_slice_strategy=delta`、`is_estimated=false`。
3. 发生回退的 session：
   - 保留各日活动摘要、消息数、工具调用和活动时间；
   - 清除已经计算出的各日差分 Token，避免保留部分重复数据；
   - 将 session 最终 Token 总量仅归集到最后活动日；
   - 标记该 slice：`token_slice_strategy=session_total_last_activity`、`is_estimated=true`；
   - 其他活动日 Token 为 0，仅保留内容活动信息。
4. 不再无条件覆盖异常策略。只有整个 session 未发生回退时才设置为 `delta`。

### 为什么归集到最后活动日

异常日志没有可靠逐日 Token 边界；将最终 session 总量归集到最后活动日有三个优点：

- 总 Token 与 Codex 最终累计值一致，不会膨胀；
- 不需要猜测或伪造逐日消耗；
- 与用户感知的“最近使用 session”一致。

代价是异常的跨天 Codex session 不再提供精确逐日 Token 分布。该限制必须通过 `is_estimated` 和策略字段明确表达。

### 后端与前端影响

- 不需要新增数据库字段；现有 `is_estimated` 和 `token_slice_strategy` 足够表达。
- `/tokens` 继续按 activity slice 聚合。异常 session 只有最后活动日承载 Token，因此汇总不会重复计数。
- session 明细应对 `is_estimated=true` 显示轻量提示，例如“Token 按最后活动时间归集”。
- 报告生成仍可读取所有日期的 session 内容摘要；本次只改变 Token 数值归属，不改变报告内容数据。

### 数据修复

1. 先发布新版 Aida CLI。
2. 受影响用户使用新版执行 `aida upload --all` 重新上传。
3. 现有接口会替换同一 session 的 activity slices，无需手工修改生产库数值。
4. 当前生产库中，`001898` 有 3 条 Codex slice 标记为估算；其中 `019f1ad3-d349-7b82-ae5c-0eb82d59c764` 的膨胀最明显，应优先重传验证。

不建议直接将 667 亿改成某个手工数值，因为这不能修复原始切片结构，也无法验证新解析逻辑。

## 方案二：最近活动 session 排序

### 时间语义

需要区分两个不同概念：

- `StartedAt`：session 启动时间，用于保留会话历史。
- `LastActiveAt`：最近有效日志事件时间，用于判断用户最近实际使用过哪些 session。

`EndedAt` 在当前扫描逻辑中表示最后一条有效日志事件时间，而不保证代表 session 已正式结束。因此 CLI 文案使用“最近活动”，不使用“结束时间”。

### daemon 改动

1. 新增 `LastActiveAt()`：优先 `EndedAt`，其次 `StartedAt`，最后回退日志文件修改时间。
2. 新增按 `LastActiveAt()` 倒序的排序函数。
3. `aida upload` 的交互选择列表和 `--all` 上传前预览使用最近活动排序。
4. `aida sessions` 使用相同排序，避免浏览和上传入口顺序不一致。
5. 列头由 `Started` 改为 `最近活动`，显示最近活动日期和时间。
6. 上传结果行也显示最近活动时间，而不是只显示启动时刻。
7. `aida upload --all` 仍上传所有 session；改动仅影响展示与人工选择顺序。

## 回归测试

### Codex Token

1. 单调递增事件：按天差分，slice Token 总和等于 session 最终总 Token。
2. 回退事件：例如 `257M -> 258K -> 258M`，slice Token 总和仍等于最终总 Token，不重复累计。
3. 回退 session：只有最后活动日承载 Token，`is_estimated=true`，策略为 `session_total_last_activity`。
4. 无回退 session：`is_estimated=false`，策略为 `delta`。
5. `/tokens` 聚合、session 明细和 `token_usage` 的 session 总值一致。
6. 重传同一 session 后，旧的膨胀 activity slices 被替换。

### 最近活动排序

1. 一个早启动、今天仍有活动的 session 排在今天新启动但更早停止的 session 前。
2. `EndedAt` 缺失时回退到 `StartedAt`。
3. 两个时间都缺失时回退到文件修改时间。
4. `aida upload` 交互选择、`--all` 预览和 `aida sessions` 顺序一致。
5. `aida upload --all` 仍包含历史 session，不因排序变更遗漏。

## 发布顺序

1. 完成 daemon 代码和测试。
2. 构建并发布新版 Aida CLI 安装包。
3. 在开发环境使用包含回退序列的 Codex 原始日志进行端到端上传验证。
4. 部署生产 API/Web（仅当增加前端估算提示时需要 Web 更新）。
5. 让受影响用户升级 CLI 并重传 session。
6. 核对生产 `/tokens`、session 明细和报告上下文中的 Token 结果。

## 方案三：安全下线 legacy consumer

### 结论和边界

当前六类报告的正式生成路径是：

```text
页面 AI 生成或定时报告任务
-> /ai-assets/report-agents/{agentId}/runs
-> managed agent platform
-> Aida Report MCP
-> write_report_result
```

该路径不依赖 `consumer` 容器。

`consumer` 当前运行的是 `aida serve`，仅监听旧版报告生成 HTTP 接口；它不运行 `aida consume`，不扫描用户本机日志，也不是定时报告任务执行器。

前端源码中旧的生成函数仍存在，但当前 Dashboard 已明确将 legacy report workflow 标记为 disabled：旧 mutation 仅存在于未绑定渲染事件的 dead code 中。当前报告页面使用 `ReportAIGenerateControls` 调用 Report Agent。

本次下线只删除旧的“生成代理”能力。必须保留报告读取、保存、编辑、来源查询、权限控制、已有报告数据、session 上传 CLI、Report Agent 和 managed agent scheduler。

### 删除范围

#### 前端

删除 Dashboard 中已禁用的 legacy report workflow，包括其状态、mutation、draft helper、旧 Skill 上传辅助逻辑和仅服务于该流程的类型引用。

删除 API client 中以下旧生成函数，并在删除前通过全仓库搜索确认没有调用方：

- `generateTodayReport`
- `generateTodayReportDraft`
- `generateTeamReport`
- `generateDepartmentReport`
- `generatePersonalWeeklyReport`
- `generateTeamWeeklyReport`
- `generateDepartmentWeeklyReport`

保留所有报告查询、保存、编辑、来源和详情接口；这些不依赖 consumer。

#### API

删除以下旧生成路由及其对应 `ReportHandler` 方法：

```text
POST /reports/today/draft
POST /reports/today/generate
POST /reports/weekly/mine/current/generate
POST /reports/team/today/generate
POST /reports/team/weekly/current/generate
POST /reports/department/today/generate
POST /reports/department/weekly/current/generate
```

同时删除：

- `config.Config.ReportGeneratorURL`
- `REPORT_GENERATOR_URL` 配置读取与 Compose 环境变量
- `NewReportHandler(database, reportGeneratorURL)` 的 generator URL 参数
- 上述旧生成路由的 handler 测试和 mock generator 测试

保留 `ReportHandler` 的其他读写能力，不能因为移除生成代理而删除整个 handler。

#### daemon 和部署

删除以下 legacy 能力：

- `daemon/server_reports.go` 的 HTTP report generator、旧版本地 Claude 报告生成和 draft 解析逻辑；
- `aida consume` 子命令、`ConsumerConfig`、`runConsumerOnce`、`runServerConsumerOnce`、`filterSessionsForReport`；
- consumer 专用测试，例如 `daemon/report_draft_test.go`；
- `web/scripts/dashboard_report_workflow_test.mjs`，并移除 `web/package.json` 对该 legacy contract test 的执行；
- 根目录和 `deploy/` Compose 文件中的 `consumer` 服务；
- consumer 镜像构建、推送、拉取、重启步骤。

保留 `daemon/device_client.go` 中的 `aida upload`、`aida sessions`、Claude/Codex 扫描、上传 payload 和登录配置。它们仍是用户将本机 session 上传到平台的必需能力。

`daemon/Dockerfile` 仅在确认没有其他构建流程将其用于 Aida CLI 分发后再删除；不能因为 consumer 服务移除而假设它没有其他用途。

### 不删除的线上能力

- `/ai-assets/report-agents/*` 和 `/ai-assets/agent-schedules/*`；
- Aida Report MCP 和默认 Report Agent；
- `daily_reports`、`personal_weekly_reports`、`team_reports`、`team_weekly_reports`、`department_reports`、`department_weekly_reports` 数据；
- 报告 GET、PUT、保存、详情、来源查询接口；
- session 上传、Token、需求和任务能力；
- MinIO 原始 session 日志存储。

不需要数据库迁移或清理历史报告数据。

### 风险控制与发布顺序

1. 静态确认：在可执行代码与部署文件中搜索旧 7 个生成 API、`REPORT_GENERATOR_URL`、`consumer:8090`、`aida consume`，范围为 `api/`、`web/src/`、`web/scripts/`、`daemon/`、Compose、`deploy/` 和 CI/构建脚本。历史测试报告可保留；活跃部署、接口和测试文档必须更新或标记已废弃。
2. 运行确认：检查生产 API/access log 的旧 7 个路径；若存在外部调用方，先通知并迁移调用方，不能直接删除路由。
3. 前端验证：Dashboard、日报、周报页面可加载；六类报告的 AI 生成功能均能发起 Agent run、轮询完成并回写报告。
4. 后端验证：创建和执行个人、小组、部门的日报/周报定时报告任务，确认 consumer 不存在时 scheduler 仍正常触发。
5. 构建验证：前端 build、API handler 测试、daemon upload 相关测试全部通过；只处理与本次删除直接相关的失败。
6. 部署验证：先部署 API/Web 版本并保留旧 consumer 容器运行，但 API 不再引用它；确认无旧接口调用后，再从 Compose 删除 consumer。
7. 回滚准备：保留前一个完整镜像 tag 和 Compose 文件。若出现未识别的旧接口调用，恢复旧 API/Web 与 consumer 服务，而不是只恢复其中一个。

### 验收条件

- `docker compose ps` 中不存在 consumer，API/Web/DB/MinIO 正常；
- API 配置和日志中不存在 `REPORT_GENERATOR_URL`、`consumer:8090`；
- 当前前端没有旧 `/reports/*/generate` 请求；
- 六类报告手动生成和定时生成均创建 managed Agent run，不创建 consumer 请求；
- `aida upload`、`aida sessions` 和 Token 页面正常；
- 已有报告仍可查看、编辑和保存。
