# Token 成本统计与价格管理方案

## 文档状态

- 状态：已完成产品/技术交叉审查，待人工评审，未开始开发。
- 日期：2026-07-13。
- 适用项目：`/home/intellif/dev/project_manager`。
- 目标用户：工程师、PM、小组长、总监、系统管理员。
- 产品形态、菜单、页签、角色可见范围和页面文案以 `doc/v2/Token使用与成本分析产品需求方案.md` 为唯一依据；本文只定义数据、接口、计算、权限和上线方式，不得自行增加页面入口或改变产品口径。

## 背景

当前 Token 页面已能展示时间范围内的 Token 总量和 session 明细，后端 `/tokens` 也已支持按 `team`、`user`、`model`、`requirement` 和 `task` 聚合，但前端只请求 `group_by=model`，且未展示接口返回的 `groups` 和 `series`。

因此，总监当前无法直观看到：

- 小组 Token 使用排名。
- 成员 Token 使用排名。
- 模型使用结构和日趋势。
- 小组、成员和模型的成本排名。
- API 等价成本和未计价模型。

生产已经存在可用的 session activity slice 和 Token 数据。本方案不重做 Token 采集，而是补齐计费口径、价格治理、成本固化和管理端分析。

## 核心决策

1. **平台内部已审核价格是唯一计费基准**。管理员依据供应商官方页面或企业合同人工录入、审核并发布价格。
2. **成本在 Aida 服务端计算**。Aida CLI 上传模型、Token 分类和可确认的计费变体；日志自带成本仅保存为对照值。服务端使用统一价格版本计算平台权威的 API 等价成本。
3. **使用时间决定价格**。根据 Token 实际发生的 activity 时间匹配当时有效的价格，不使用上传时间或查询时间。
4. **历史成本必须可重现**。计算结果保存价格版本、单价快照、计费 Token 和计算器版本。新价格不静默修改历史成本。
5. **未知价格不按零成本处理**。无法匹配价格时仍正常保存 Token，状态标记为“未计价”，并从成本总额中排除。
6. **不将 API 等价成本冒充实际账单**。页面必须标识价格手册类型和成本口径。
7. **真实 Session ID 是 `sessions.session_ref`**。数据库 `sessions.id`只用于内部关联，禁止作为页面 Session ID 展示。
8. **Session 和 activity slice 严格区分**。Session 数按 `sessions.id`去重；列表一个 Session 一行，逐日 slice 只在详情中展示。
9. **无数据不等于未上传**。V1 不新增上传回执；没有 Token 或 activity slice 只能标记“无数据”，不能推断用户没有执行上传。
10. **V1 不计算订阅利用率或 Credits 等价**。Plus、Pro 月费、套餐 allowance、超限 credits 和 API 等价成本不得互相换算。
11. **USD 计价、CNY 展示**。官方模型单价按 USD 保存，用户页面统一展示按平台核算汇率折算后的人民币；价格原值、汇率和人民币结果必须可追溯。

## 价格来源和运行边界

本方案不引入新的网关、价格同步服务或第三方计价工具。运行链路只有：

```text
用户电脑
Claude Code / Codex 日志 -> Aida CLI -> /sessions/batch
                                  |
                                  v
Aida 服务端
Token 数据 -> 管理员已审核价格表 -> 成本计算器 -> 成本快照与统计页面
```

官方 API 单价由管理员依据供应商官方价格页面人工录入，固定使用 USD/百万 Token；平台另行维护带生效日期的 USD/CNY 核算汇率。两者审核发布后才参与计算。当前方案不设计定时自动同步。

管理端提供“AI 辅助查询”，用于查询供应商官方价格页面并将结构化候选值回填到表单。AI 返回必须包含官方来源 URL、查询时间、模型标识、USD 单价和无法确认的字段；没有官方依据时必须明确失败。AI 不能直接创建或发布价格，管理员必须人工核对后提交。

## 成本口径

### 价格手册

价格手册用于区分不同成本口径，禁止在同一总额中混用：

| 类型 | 用途 | 说明 |
| --- | --- | --- |
| 官方 API 等价 | 默认管理分析 | 使用经人工核对的官方标价，属于估算值 |
| 企业合同价 | 实际采购口径 | 需管理员录入并保护敏感价格 |
| 内部模型成本 | 私有部署核算 | 由内部成本口径给出，不根据公开模型价格自动推断 |

V1 默认只启用“官方 API 等价”价格手册，源价格币种固定为 USD，展示币种固定为 CNY。其他手册预留数据结构，不强制首期上线。

Plus、Pro 等订阅套餐和 Credits 不建价格手册，也不从本地 Token 反推。未来如能接入供应商官方账号 usage 数据，应另建独立模块，不能复用 API 等价成本字段。

### 计费 Token 必须互斥

计算器只接收下列互斥字段：

```text
uncached_input_tokens
cache_read_tokens
cache_write_5m_tokens
cache_write_1h_tokens
output_tokens
```

- Claude Code 日志中的 `input_tokens`、`cache_creation_input_tokens` 和 `cache_read_input_tokens` 通常是分立口径。
- Codex 日志中 `input_tokens` 包含 `cached_input_tokens`，因此 Codex 未缓存输入必须按 `input_tokens - cached_input_tokens` 归一化。
- 缓存写入时长无法从日志区分时，必须使用价格版本中的审核默认值，并标记计算假设。

### 多模型 session

当前 activity slice 保存了 `model` 和 `models[]`，但 Token 只有 slice 总数。如果同一天的 session 中切换过模型，把整个 slice 的 Token 都按最后一个模型计价会产生错误。

因此 Aida CLI 需要按 `activity_date + canonical_model + billing_variant` 生成模型用量明细。服务端按明细计价后再汇总到 slice、session、用户和小组。

旧数据只有一个可确认模型时可回填；`models[]` 存在多个模型但没有 Token 拆分时，标记为“模型分摊不可用”，不强行计价。

### 计算公式与人民币折算

```text
cost_usd =
  uncached_input_tokens * input_price_per_mtok / 1,000,000
  + cache_read_tokens * cache_read_price_per_mtok / 1,000,000
  + cache_write_5m_tokens * cache_write_5m_price_per_mtok / 1,000,000
  + cache_write_1h_tokens * cache_write_1h_price_per_mtok / 1,000,000
  + output_tokens * output_price_per_mtok / 1,000,000

cost_cny = cost_usd * usd_to_cny_rate
```

USD 单价统一保存为“每百万 Token”。`usd_to_cny_rate`表示 `1 USD = N CNY`。单价、汇率和成本计算均使用高精度 decimal，禁止使用浮点数作为数据库权威值。

V1 的模型用量按 `Asia/Shanghai` 自然日聚合，因此价格版本和汇率版本都必须从业务自然日 00:00 起生效，不支持日内切换。计算时按 activity 日期分别匹配当时有效的模型价格版本和汇率版本。

### 计费变体

模型名称不足以唯一决定价格。价格版本还应区分：

- standard / priority / fast / batch。
- global / regional / US-only 等区域变体。
- 长上下文阶梯价。
- 缓存写入 5 分钟 / 1 小时。

日志无法确定变体时，按价格手册的审核默认变体计算，并在成本记录中保存 `assumptions` 与 `confidence`。

## 价格生命周期

### 人工审核与发布

1. 管理员选择模型和价格手册。
2. 核对标准模型、USD Token 单价、计费变体、生效时间和官方依据。
3. 发布前预览影响：新数据计价、可补算的未计价数据、与旧价格的差额。
4. 发布会创建不可变价格版本，并关闭同一价格手册、模型和变体的上一个有效时间段。
5. 生效时间段禁止重叠；起始时间必须明确。

### 美元汇率审核与发布

1. 平台只维护 USD/CNY 核算汇率，含汇率值、生效日期、来源和备注。
2. 管理员可手工填写，也可使用 AI 辅助查询来源，但必须人工确认发布。
3. 发布新汇率版本会关闭上一版本的有效时间段，不修改历史成本。
4. 汇率按 activity 日期匹配，不使用上传日期、查询日期或浏览器实时汇率。
5. 首个模型价格发布前必须存在覆盖其生效日期的已发布汇率，否则拒绝发布。

### 历史修正

- 新价格只影响其生效时间范围内的 Token。
- 发布后可通过受控补算流程处理相同模型和有效时间范围内的“未计价”记录。
- 已计价记录不随当前价格修改而变化。
- 已发布价格错误时，新增纠正版本，由管理员通过“影响预览 -> 确认重算”显式修正。
- 重算保留旧成本记录和原因，不做无审计的原地覆盖。

## 数据模型

### `price_books`

价格手册：

- `id`、`name`、`cost_basis`、`currency`、`is_default`、`enabled`。
- `cost_basis` 枚举：`official_api_equivalent`、`enterprise_contract`、`internal_model`。V1 只启用 `official_api_equivalent`。
- V1 的 `currency`固定为 `USD`；人民币是统计展示币种，不作为模型单价录入币种。

### `model_aliases`

模型别名归一化：

- `provider`、`raw_model`、`canonical_model`。
- `valid_from`、`valid_to`，用于处理别名随时间变更。
- `source`、`reviewed_by`、`reviewed_at`。
- 发布前校验同一 raw model 的有效区间不重叠。

### `model_price_versions`

不可变的价格版本：

- `price_book_id`、`provider`、`canonical_model`、`billing_variant`。
- `input_price_per_mtok`、`cache_read_price_per_mtok`、`cache_write_5m_price_per_mtok`、`cache_write_1h_price_per_mtok`、`output_price_per_mtok`。
- `effective_from`、`effective_to`。
- `source_type`、`source_url`、`notes`。
- `status`、`published_by`、`published_at`。

所有单价使用 `NUMERIC`，禁止 `FLOAT/DOUBLE`。

### `usd_cny_rate_versions`

平台美元兑人民币核算汇率版本：

- `id`、`rate`，其中 `rate`表示 `1 USD = N CNY`。
- `effective_from`、`effective_to`，按 `Asia/Shanghai`自然日生效且有效区间不得重叠。
- `source_url`、`source_checked_at`、`notes`。
- `status`、`published_by`、`published_at`。

汇率使用 `NUMERIC`。已发布版本不可原地修改；修正时新增版本并保留审计记录。

### `session_activity_model_usage`

每个 activity slice 的模型级 Token 明细：

- `session_id`、`activity_date`、`provider`、`raw_model`、`canonical_model`、`billing_variant`。
- 五类互斥计费 Token。
- `source_recorded_cost_usd`：Claude Code 日志存在 `costUSD` 时保存，只作对照。
- `normalization_strategy`、`parser_version`、`is_estimated`。

### `session_activity_costs`

计价结果与审计快照：

- 关联 model usage、`model_price_version_id`和`usd_cny_rate_version_id`。
- 保存实际使用的五类 USD 单价快照和 USD/CNY 汇率快照。
- `estimated_cost_usd`、`estimated_cost_cny`、`pricing_status`、`confidence`、`assumptions`。
- `calculator_version`、`calculated_at`、`calculation_reason`。
- `supersedes_id`、`superseded_at`，用于保留显式重算历史。

计价聚合必须产生明确状态：

- `priced`：范围内全部 Token 已计价。
- `partially_priced`：仅部分 Token 已计价，返回已计价金额和未计价 Token，但不计算百分比覆盖率。
- `unpriced`：全部 Token 未计价，金额返回 `null`，不能返回 `0`。

### Session 查询聚合口径

- Session 列表以 `sessions.id`为内部分组键，一个 Session 一行。
- 页面显示和搜索 `sessions.session_ref`，不得显示内部 `sessions.id`。
- 选定时间范围内的 Token 和成本对该 Session 的 activity slice 求和。
- `session_count`使用 `COUNT(DISTINCT sessions.id)`，禁止使用 slice 行数。
- 最近活动时间取范围内最大的 `activity_end_at`。
- 概览优先取范围内最新的非空 `session_activity_slices.summary`，再回退 `excerpt`、`sessions.summary`。
- 点击 Session 后才返回逐日 activity slice，不能在主列表重复展示跨天 Session。

## 上传与计算流程

```text
Claude Code / Codex 本地日志
  -> Aida CLI 按日期、模型和计费变体解析 Token
  -> POST /api/v1/sessions/batch
  -> 开启数据库事务并验证 Token 互斥性和分组总量
  -> 幂等写入 Session、activity slice 和模型用量
  -> 批量归一化模型并按 activity 时间匹配价格版本和汇率版本
  -> 固化 USD 原值、汇率和 CNY 成本；价格或汇率缺失写未计价状态而不是失败
  -> 提交事务，提交成功才视为本次上传成功
  -> Token/成本分析查询
```

价格缺失、汇率缺失、别名未审核或模型 Token 无法拆分不应阻断 session 上传，统一写未计价状态。计价必须按本批次涉及的模型和日期批量查询价格及汇率，禁止逐 Token 或逐消息查询，避免显著增加大批量上传耗时。

## API 方案

### 价格管理

- `GET /api/v1/admin/pricing/books`
- `GET /api/v1/admin/pricing/models`
- `POST /api/v1/admin/pricing/models`：创建待发布价格。
- `POST /api/v1/admin/pricing/models/{id}/publish`：人工确认后发布。
- `GET/POST /api/v1/admin/pricing/aliases`
- `POST /api/v1/admin/pricing/models/ai-lookup`：管理员主动触发 AI 查询官方价格并返回表单候选值，不落生效价格。
- `GET /api/v1/admin/pricing/exchange-rates`
- `POST /api/v1/admin/pricing/exchange-rates`：创建待发布 USD/CNY 汇率。
- `POST /api/v1/admin/pricing/exchange-rates/{id}/publish`
- `POST /api/v1/admin/pricing/exchange-rates/ai-lookup`：管理员主动触发 AI 查询汇率来源并返回候选值，不自动发布。

### 补算与重算

- `POST /api/v1/admin/pricing/recalculate/preview`
- `POST /api/v1/admin/pricing/recalculate/apply`
- apply 请求必须带 preview ID、影响范围、原因和二次确认。

### Token/成本分析

- `GET /api/v1/tokens`：总量、模型/小组/人员聚合和日趋势。
- `GET /api/v1/tokens/sessions`：一个 Session 一行的分页查询，不再把 activity slice 当成 Session 返回。
- `GET /api/v1/tokens/session-records/{record_id}/slices`：使用列表返回的内部 opaque `record_id`读取逐日 activity slice，并再次校验角色权限。`record_id`只用于前端取详情，不在页面展示或搜索。
- `GET /api/v1/tokens/rankings`：返回包含零 Token 人员在内的分页完整排名，以及前 5和后 5摘要。
- 服务端内部使用平台唯一启用的“官方 API 等价”价格手册；V1 请求不接受 `price_book_id`切换，前端也不显示价格手册选择器。
- 支持 `group_by=team|user|model`的 Token 与成本同步聚合。
- 支持 `team_id`下钻；总监只能选择自己管理范围内的小组，管理员可选择任意小组。
- 用户统计统一返回 `estimated_cost_cny`、`priced_tokens`、`unpriced_tokens`、`pricing_status`、`display_currency=CNY`和`cost_basis`；详情和管理员接口可额外返回 `estimated_cost_usd`、汇率值及两个版本 ID，用于追溯。
- `estimated_cost_cny`在部分计价时只是已计价小计，必须同时返回 `pricing_status=partially_priced`和 `unpriced_tokens`；全部未计价时返回 `null`。
- 任何成本聚合都必须带价格手册，禁止跨手册直接相加。

Session 查询参数：

- `q`：真实 Session ID 或概览关键词。
- `from`、`to`、`team_id`、`user_id`、`model`、`page`、`page_size`。
- 服务端先以 `sessions.session_ref = q`在授权范围内做精确匹配；命中时忽略日期、`team_id`、`user_id`和`model`筛选，按该 Session 全部已上传 activity slice 聚合 Token 和成本，并返回实际活动日期范围、`search_mode=exact_session_ref`和`cross_filter_lookup=true`。
- 精确匹配未命中时，`q`遵循日期和其他筛选条件，在 `sessions.session_ref`、activity summary、activity excerpt 和 session summary 上做不区分大小写的包含搜索，响应返回 `search_mode=filtered_contains`。
- 搜索在数据库端执行并分页，禁止只过滤当前页。
- Session 列表返回 `record_id`和展示字段 `session_ref`。由于数据库只保证 `(session_ref, user_id)`唯一，同一个 `session_ref`在授权范围内可能命中多个用户，必须分别返回，不能合并或随机取一条。

角色范围：

- `scope=mine`：所有角色只看自己。
- `scope=management`：仅小组长、总监和管理员可用；PM 和工程师请求时返回 403。
- 前端现有 `scope=team`调用必须迁移到 `scope=management`；迁移完成后服务端不再使用含义模糊的 `team`值。
- 缺失或非法 `scope`、`group_by`必须返回 400，禁止通过 default/fallthrough 扩大到全平台或静默改成其他维度。
- 小组长：仅 `users.team_id = 当前用户.team_id`。
- 总监：仅属于 `teams.director_user_id = 当前用户.id`的小组成员，禁止沿用当前代码中 director 默认全平台的行为。
- 管理员：全平台，可用 `director_user_id`或 `team_id`缩小范围。
- 排名从授权范围内所有已启用用户出发 LEFT JOIN 用量，确保零 Token 人员仍出现；不能只从 activity slice 分组得到排名。
- 授权范围人数少于 10 时，响应只返回一份完整有序列表，不构造重复的前 5/后 5；达到 10 人时才返回不重叠的 `top_items`和`bottom_items`各 5 条。
- P0 的总量、趋势、人数和排名统一只包含当前已启用用户，保证汇总等于明细之和。停用、归档用户的数据仍保留，但不进入这两个 Token 页面；历史离职人员分析不在 V1 范围。
- Token 和活跃天数并列时按最近活动时间、人员显示名称稳定排序。
- 成本排名只对 `pricing_status=priced`的用户排序；`partially_priced`和`unpriced`单独返回不可完整排名列表，禁止按已计价小计与完整成本混排。
- 所有日期范围按 Asia/Shanghai 自然日解释；默认最近 7 天是今天及前 6 天。

## 页面方案

### 两个 Token 页面

前端必须严格实现产品文档定义的两个菜单，不得合并成大而全页面：

1. `我的 Token`：所有角色可见，固定 `scope=mine`，展示个人 Token、API 等价成本、趋势、模型和 Session。
2. `Token 使用分析`：只对小组长、总监和管理员可见，使用 `scope=management`；内部页签为概览、排名和 `Session 查询`。

`Session 查询`只是“Token 使用分析”内部页签，不是独立菜单、独立路由入口或额外按钮。工程师和 PM 在“我的 Token”页面内查询自己的 Session。

路由固定为：

- `/tokens`：我的 Token。
- `/tokens/analysis`：Token 使用分析；无管理权限时路由守卫拒绝访问。
- `/tokens/pricing`：成本价格管理；仅 admin 可访问。
- “Session 查询”通过页面内部 tab state 或 `?tab=sessions`表示，不新增 `/tokens/sessions`前端页面路由。

页面固定展示说明：

> 成本按平台已审核价格估算，不代表实际订阅或企业合同账单；部分或全部无法计价时会明确标识未计价。

前端不得展示上传覆盖率、价格覆盖率、订阅利用率或 Credits 利用率。没有 activity 数据的人员只显示“无数据”，不得推断为未上传；价格不完整通过 `pricing_status`和“部分未计价/未计价”表示。

### 成本管理页

仅 admin 可进入和编辑。总监不显示价格管理菜单。页面包含：

- 当前生效价格和最近生效时间。
- 价格历史和来源证据。
- 模型别名映射。
- 未计价模型、Token 量、影响小组和最近出现时间。
- 发布、补算和纠正重算的影响预览。
- 操作审计记录。

## 边界与非目标

- V1 不对接 OpenAI/Anthropic 账单 API，不声称平台估算值等于实际账单。
- V1 不要求每个用户配置价格。
- V1 不自动给内部模型推测 GPU 成本。
- V1 不维护 Plus、Pro 席位，不计算套餐 allowance、Credits 或订阅利用率。
- V1 不新增独立部门表；总监管理范围由 `teams.director_user_id`确定。
- 价格或汇率缺失不影响 session 上传、Token 统计和报告生成。
- 价格模块不修改 managed agent platform。

## 迁移与上线顺序

### 阶段 1：价格基础设施

1. 新增价格手册、别名、价格版本、USD/CNY 汇率版本和审计表。
2. 管理员人工录入并核对当前生产中已出现的公开模型价格和初始核算汇率。
3. 上线价格、汇率发布及有效时间冲突校验。
4. 上线管理员主动触发的 AI 辅助查询；验证无官方来源时不会给出确定候选值，也不能自动发布。

### 阶段 2：模型级 Token 和计价

1. Aida CLI 增加每日每模型 Token 拆分。
2. API 扩展上传 schema 并验证模型明细与 slice 总数。
3. 上传事务中批量匹配价格并固化成本；价格缺失写未计价状态。
4. 通过新版 Aida CLI 重新上传可获得精确多模型拆分。

### 阶段 3：管理分析页

1. 上线所有角色的“我的 Token”页面。
2. 上线小组长、总监和管理员的“Token 使用分析”页面及角色范围校验。
3. 增加小组、成员、模型排名、趋势和总监下钻。
4. 增加真实 Session ID/概览服务端搜索、去重 Session 列表和 slice 详情。
5. 增加未计价状态、无数据人员和价格手册说明，不展示百分比覆盖率，也不推断未上传。

### 阶段 4：历史数据

1. 单模型旧 slice 按 activity 时间和已审核价格回填。
2. 多模型但无法拆分的旧数据保持未计价，除非用新 CLI 重新解析上传。
3. 回填前生成预览报告，核对已计价、部分未计价、未计价状态和金额。

## 测试要求

### 价格管理

- 未审核价格不参与计算。
- 未审核汇率不参与计算；缺少有效汇率时不得生成或展示人民币成本。
- 生效时间重叠必须拒绝。
- 已发布版本不可直接编辑和删除。
- 来源链接、审核人和审核时间完整保留。
- AI 查询结果不落生效数据，管理员确认发布后才参与计算。

### 计算器

- Claude Code 的 input/cache create/cache read/output 分立计价。
- Codex 的 cached input 从 input 中扣除，不重复计价。
- standard/priority/fast 分别匹配对应价格。
- 活动日期处于价格切换前后时，分别使用旧版和新版价格。
- 活动日期处于汇率切换前后时，分别使用旧版和新版汇率，历史人民币成本不随当前汇率变化。
- `cost_cny = cost_usd * usd_to_cny_rate`，汇总、排名、趋势与明细结果一致。
- 价格后续更新后，历史成本保持不变。
- 多模型拆分成本之和等于 slice 成本。
- 未计价数据不影响 Token 总量，不按零计入成本。
- decimal 精度和大 Token 数量下无浮点误差。

### 权限与聚合

- 所有角色通过“我的 Token”只能查看自己。
- PM 和工程师请求 `scope=management`返回 403。
- 小组长通过管理分析只能查看本小组。
- 总监只能查看 `teams.director_user_id = 当前用户.id`的管理范围，不能读取全平台，不能修改价格。
- 只有管理员可审核和发布价格。
- Token 排名、成本排名、明细和趋势在相同时间范围下口径一致。
- 排名包含授权范围内零 Token 的已启用用户。
- 已归档、停用用户不进入 V1 的总量、人数、排名和明细；V1 只展示当前已启用用户。

### Session 与搜索

- 跨天 Session 在主列表只出现一行，`session_count`去重，逐日 slice 总和与 Session 汇总一致。
- 页面和搜索使用 `session_ref`，内部 `sessions.id`不出现在响应展示字段中。
- 完整 `session_ref`可跨日期定位，但必须受角色范围约束。
- ID 片段和概览关键词遵循当前筛选并由数据库分页查询。
- 同一 `session_ref`在不同用户下发生冲突时，只返回当前权限内数据，并以内部 `sessions.id`分别聚合。
- 概览回退顺序固定，空值与大小写搜索行为有测试覆盖。

## 验收标准

1. 所有角色都能在“我的 Token”查看个人 Token、API 等价成本、趋势、模型和去重 Session。
2. 小组长、总监和管理员能在独立的“Token 使用分析”查看授权范围；PM 和工程师没有该菜单且接口访问返回 403。
3. 总监只能读取自己负责的小组集合，不能因角色为 director 获得全平台数据。
4. 管理角色能查看小组、成员和模型的 Token/成本排名，并看到每个人的 API 等价成本。
5. 页面不显示上传覆盖率、价格覆盖率、订阅利用率或 Credits 利用率。
6. 已计价、部分未计价和未计价状态准确；未计价部分不会被当作 0 或完整成本。
7. 用户能按真实 Session ID（`session_ref`）和概览搜索；跨天 Session 不重复计数，搜索不越权。
8. 无 activity 数据的人员显示“无数据”，不能被标记为未上传。
9. 任一成本数值都能追溯到模型用量、价格版本、汇率版本、USD 单价快照、汇率快照、计算器版本和操作人。
10. 价格或汇率更新不会静默改变历史成本，内部模型或未知别名不会被错误计为零成本。
11. session 上传不因价格或汇率缺失失败。

## 已确认开发约束

1. V1 默认且只启用“官方 API 等价”价格手册；源价格为 USD，用户展示统一为 CNY。
2. V1 维护带生效日期的 USD/CNY 平台核算汇率；企业合同价、内部模型成本、订阅席位和 Credits 不在 V1 范围。
3. 所有角色能看自己的 API 等价成本；小组长能看本组每个人；总监能看自己管理范围内每个人；管理员能看全平台。
4. 当前只支持管理员人工确认并发布价格和汇率；AI 辅助查询只回填候选值，不设计定时自动同步或自动发布。
5. 产品文档未明确授权的页面、菜单、筛选项和指标一律不得自行增加。

## 产品需求到实现的强制映射

| 产品要求 | 技术实现约束 |
| --- | --- |
| 所有角色有个人概览和个人成本 | `/tokens`固定 `scope=mine`，不得因角色是 TL/总监而默认进入管理范围 |
| TL/总监另有管理分析 | `/tokens/analysis`使用 `scope=management`，PM/工程师前端隐藏且后端返回 403 |
| TL/总监查看每个人成本 | 人员聚合返回 Token、`estimated_cost_cny`和`pricing_status`，按角色范围授权 |
| 不展示覆盖率 | 用户 API 和前端不提供百分比覆盖率；只返回未计价状态和未计价 Token |
| Session 查询是页签 | 只作为管理分析内部 tab，不新增第三个业务菜单或独立前端页面 |
| 搜索真实 Session ID | 展示和搜索 `session_ref`；`sessions.id`仅作为不可见的详情关联键 |
| 搜索概览 | 服务端按固定概览回退字段做不区分大小写包含搜索并分页 |
| 跨天 Session 不重复 | 列表按 `sessions.id`聚合，Session 数使用 distinct，slice 只在详情中展示 |
| 无数据人员口径准确 | 只表示所选范围没有 activity 数据，不能显示成未上传 |
| 总监只看自己的部门管理范围 | 按 `teams.director_user_id`授权，禁止 director 默认全平台 |
| 不推算订阅利用率 | V1 只将官方 USD API 等价成本按平台汇率折算为人民币，不建 Plus/Pro/Credits 换算 |
