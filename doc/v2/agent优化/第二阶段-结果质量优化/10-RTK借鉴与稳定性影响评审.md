# RTK 借鉴与 Session Digest v2.1 稳定性影响评审

> Review 日期：2026-07-16
> 状态：v2.1 已在 14.157 测试环境显式启用；生产仍默认关闭
> 结论：已借鉴 RTK 的归约思想但未集成 RTK；v2.1 是独立任务类型、可立即停用的服务端旁路
> 范围：架构合理性、潜在风险、模块影响、Token 统计、Session 上传与 Aida 客户端

## 1. 结论摘要

本期已按以下最小改动落地：

1. 保留当前 `session-digest/v1` 全部行为；
2. 新增独立 `api/internal/sessiondigestv2`；
3. 新增独立 `build_content_slice_digest_v2` job type；
4. 只在 v2 Evidence Extractor 内借鉴 RTK 的“按命令类型选择有界 parser”；
5. 生产 v2 Worker 默认关闭，14.157 测试环境显式开启，首期并发和 batch 都为 1；
6. 不修改 Aida 客户端、Session 上传协议、Token 解析和统计口径；
7. 已完成 14.159 Top 30 大 Session 结构回放，全量 corpus 仍待完成；
8. 上传状态机和 Token 固定 fixture 非回归已通过；混合负载资源门禁通过后，
   才允许生产启用 v2 build。

这不是把 RTK 加入 Aida，也不是让 Agent 执行一次 RTK。RTK 只作为历史工具
输出清洗的设计参考，正式实现仍是 Aida 服务端 Go 代码。

## 2. RTK 可以借鉴什么

参考：

- [RTK 项目](https://github.com/rtk-ai/rtk)
- [RTK Architecture](https://github.com/rtk-ai/rtk/blob/develop/docs/contributing/ARCHITECTURE.md)
- [Go output reducer](https://github.com/rtk-ai/rtk/blob/develop/src/cmds/go/go_cmd.rs)
- [Pytest output reducer](https://github.com/rtk-ai/rtk/blob/develop/src/cmds/python/pytest_cmd.rs)
- [Log reducer](https://github.com/rtk-ai/rtk/blob/develop/src/cmds/system/log_cmd.rs)

| 可借鉴点 | 在 Digest v2 中的用法 |
| --- | --- |
| 按命令族选择 parser | Reducer Registry，只处理已识别格式 |
| JSON/NDJSON 优先 | 结构化字段优先于自然语言关键词 |
| 失败聚焦 | 保留失败状态、短原因和最终 attempt |
| 分组与去重 | 重复日志、重复测试事件稳定归并 |
| 有界输出 | Evidence 只有允许字段和长度上限 |
| 保留真实退出状态 | exit code 优先于“成功/失败”文本 |

### 2.1 本期明确不借鉴

- 不安装或调用 RTK 二进制；
- 不增加 shell Hook 或改写用户命令；
- 不重新执行 Session 中的任何命令；
- 不读取 RTK tee 文件；
- 不把解析失败的输出回退成原文；
- 不引入 RTK analytics；
- 不修改 Aida CLI/daemon；
- 不复制整段 RTK 源码。

如未来确需复制 Apache-2.0 代码片段，必须单独完成许可证和 NOTICE 审核。本期
采用独立实现，不增加该发布依赖。

## 3. 稳定版数据流程

```text
Aida 客户端
  |
  | Prepare / Chunk / Finalize（不变）
  v
原始 Session 对象 + upload state（不变）
  |
  v
Content Projection（不变）
  |
  +--------------------------+---------------------------+
  |                          |                           |
  v                          v                           v
Usage / Metering         Digest v1                  Digest v2.1
job types（不变）        job type（不变）           独立 job type
  |                          |                       生产默认关闭/1 并发
  v                          v                           |
Token 统计               v1 Revision                    v
                                                     Reducer Registry
                                                     Work Unit / Evidence
                                                         |
                                                         v
                                                     64 KiB v2.1 Revision
                                                         |
                                      16 KiB 报告期投影与冻结 selection
                                                         |
                                                         v
                                                Report MCP + v2 Skill
                                                         |
                                                         v
                                                write_report_result
```

关键隔离点：

- 上传成功不依赖任何 Digest job；
- v2 只处理已经完成的内容投影；
- usage/metering 与 v2 使用不同 job type；
- v1/v2 使用不同 job type；
- 报告只有在显式选择 `digest_v2` 时才读取 v2；
- 任一 v2 失败不修改已上传对象、Token 事件或 v1 Revision。

## 4. 架构合理性 Review

### 4.1 当前方案合理的部分

| 设计 | 判断 | 原因 |
| --- | --- | --- |
| 服务端确定性 Digest | 合理 | Agent 不再重复读取和总结大量原文 |
| v1/v2 显式版本 | 合理 | 历史 selection 和旧报告不漂移 |
| v2 独立 package | 本期合理 | 降低修改已上线 v1 的回归面 |
| v2 独立 job type | 必须 | 防止 Worker 跨版本 claim |
| 复用 Revision 表并带 version | 合理 | 数据模型已有版本维度，不需新结果表 |
| Reducer Registry | 有条件合理 | 只有结构化优先、fixture 驱动、unknown 降级时安全 |
| 默认关闭、单并发 | 必须 | 同进程/同数据库条件下控制资源风险 |
| 14.159 corpus 决定 parser 范围 | 合理 | 避免照搬 RTK 或针对单一样本硬编码 |

### 4.2 已修正的不合理设计

| 原设计问题 | 风险 | 修正 |
| --- | --- | --- |
| v1/v2 共用 `build_content_slice_digest` | Worker 抢错任务、错误版本处理 | 新增 `build_content_slice_digest_v2` |
| 为复用直接重构 v1 | 容量修复链路回归 | v2 独立包，暂时容忍少量重复 |
| 直接引入 RTK | 客户端、Hook、命令执行和发布面扩大 | 只借鉴算法思想，Go 独立实现 |
| 固定使用 migration 019 | 与现有 019 冲突 | 发布时选择真实下一个编号 |
| 不认识就回退原文 | 泄密和容量回归 | 输出有界 `unknown` |
| 专用 parser 由主观清单决定 | 低收益、高维护、过拟合 | 由 14.159 corpus + Gold 决定 |

### 4.3 仍然存在的架构债务

1. v2、content、usage、metering 仍在同一 API 进程，当前是逻辑隔离，不是进程
   或资源池硬隔离；
2. v1/v2 独立包会有少量重复代码，长期可能产生规则漂移；
3. 文本工具输出格式会随版本变化，Reducer Registry 需要 fixture 和 unknown
   率持续维护；
4. Revision 共表虽然简单，但生产大量回填仍会增加数据库 IO；
5. 结果质量最终仍受 Report Skill 和模型表达影响，Digest 只能保证证据结构，
   不能保证每次自然语言完全相同。

这些债务不需要本期通过大重构解决。只有混合负载证明单进程隔离不足时，下一
阶段才考虑把 Digest Worker 拆成独立进程/部署单元；本期提前拆服务会明显提高
发布和运维风险。

## 5. 模块影响

### 5.1 直接修改

| 模块 | 改动级别 | 主要改动 |
| --- | --- | --- |
| `api/internal/sessiondigestv2` | 新增/中等 | Work Unit、Reducer、Evidence、State、Budget |
| `api/internal/sessionsync` | 很小 | 新增 v2 job type 常量和约束 |
| `api/internal/reportsource` | 中等 | `digest_v2` attach、冻结 payload、mode 校验 |
| `api/service` | 中等 | 新的不可变 Report Skill |
| `api/handler`、`api/main.go` | 很小 | v2 配置、默认关闭 Worker |
| 数据库 migration | 小 | read mode、job type check、v2 独立唯一约束 |
| 离线 evaluator | 新增 | 14.159 inventory/replay/grade |

### 5.2 不做代码修改，但必须回归

| 模块 | 为什么要测 |
| --- | --- |
| `api/internal/sessiondigest` v1 | 确认 v2 未改变 v1 job 和结果 |
| Session Prepare/Chunk/Finalize | 确认上传协议、状态和时延不变 |
| content projection | v2 的上游，需确认无任务饥饿 |
| `api/internal/usage` / metering | 与 v2 共用进程和数据库 |
| Token analytics / report source total | 确认统计结果和口径不变 |
| Aida daemon/CLI | 确认无需升级且请求完全一致 |
| v1 Report Skill/MCP | 回滚路径必须保持可用 |

### 5.3 明确无产品改动

- Web 页面和用户操作入口；
- Aida 安装命令和最低客户端版本；
- Session 原始保留策略；
- 上传对象格式、cursor、hash、activity slices；
- Token 字段和成本统计规则；
- 报告写回 API 结构；
- 用户选择 Session 的现有产品流程。

用户可见的唯一变化应是：显式切换到 v2 后，日报从“聊天内容概述”变成“完成
结果、验证、决定和未完成项”的结果导向表达。

## 6. 对 Token 统计的影响

### 6.1 预期结论

不会改变 Token 数值或统计口径。

原因：

- Token/成本由原始上传对象进入 usage/metering pipeline；
- Digest 只消费内容投影，不写 usage/metering 表；
- 从 Digest 中排除 Token 遥测只是“不放入日报输入”，不是删除原始数据；
- `report-source-sessions.total_tokens` 继续查询现有 Token 汇总。

### 6.2 仍需防范的风险

共享 API 进程和 PostgreSQL 可能导致 usage job 变慢。表现应是“数据晚到”，
而不是“数值改变”。因此必须验证：

- 固定 Session 在开关 v2 前后的 Token 原始值和汇总逐项相同；
- summary/trends/rankings/sessions 查询相同；
- `report-source-sessions.total_tokens` 相同；
- usage/metering job age 没有持续积压；
- 关闭 v2 build 后延迟能够恢复。

任何 Token 数值差异都不是可接受误差，应直接阻断发布。

## 7. 对 Session 上传和 Aida 客户端的影响

### 7.1 预期结论

不修改上传功能，不要求升级 Aida 客户端。

保持不变：

- Prepare/Chunk/Finalize 接口；
- 分块策略和对象 hash；
- upload state/cursor；
- activity slices；
- token_usage 字段；
- MinIO/原始对象；
- Finalize 成功语义。

v2 Reconciler 在内容投影 ready 后异步发现任务，上传请求不创建、不等待也不
回滚 v2 job。

### 7.2 间接风险

同进程资源竞争可能提高上传 API P95，数据库回填也可能增加 IO。因此首期：

- v2 默认关闭；
- Worker 并发/batch 为 1；
- Reconciler 小批量；
- 生产回填限速；
- 监控 Prepare/Chunk/Finalize P95、错误率、数据库 CPU/IO、API RSS；
- 超门禁时立即关闭 v2 build，报告保持 v1。

如果实现过程中发现必须修改 daemon、上传 payload 或客户端版本，说明已超出
本期方案，必须停止并另立需求，不能顺带发布。

## 8. 风险登记

| 风险 | 可能性 | 影响 | 控制与停止条件 |
| --- | --- | --- | --- |
| v1/v2 Worker 跨版本 claim | 中（若共用 job）/低（修正后） | 高 | 独立 job type；集成测试发现即停止 |
| 专用 reducer 误判状态 | 中 | 高 | exit/结构化状态优先；格式不匹配为 unknown |
| 敏感信息进入 Digest | 低至中 | 高 | 先脱敏、allow-list、有界输出、安全 fixture |
| 原始 stdout 导致内存放大 | 中 | 高 | 流式归约、单并发、RSS 门禁、禁止 raw fallback |
| v2 拖慢上传 | 低至中 | 高 | 不进事务、batch 1、上传 P95 门禁、可关开关 |
| Token projection 延迟 | 低至中 | 中 | 监控 job age、限速回填、可关开关 |
| Token 数值变化 | 低 | 高 | 固定 fixture 逐项一致；任何差异阻断 |
| migration 冲突 | 中 | 高 | 以目标分支和生产最高版本选下一个编号 |
| 规则只适配 target Session | 中 | 中至高 | 14.159 全量 corpus、分层 Gold、20% holdout |
| v1/v2 Skill 或 mode 错配 | 中 | 高 | run 冻结、实际 trace 核验、未知 mode 失败 |
| v2/v1 重复代码长期漂移 | 中 | 中 | 本期接受；稳定后单独评估小范围公共抽象 |
| 回填增加数据库 IO | 中 | 中 | 限速、观察窗口、随时停 v2 build |

## 9. 本期 Go/No-Go 门禁

只有以下全部通过，才可从“14.157 测试启用”进入“生产启用 v2 build”：

- [ ] 14.159 Full Corpus 结构回放完成；
- [ ] Reducer 命中率、unknown 分布和 Top 未识别格式有记录；
- [ ] target Session 与 Human Gold 质量门禁通过；
- [x] v1/v2 Worker 不能 claim 对方 job；
- [x] 生产 v2 默认关闭，首期并发/batch 为 1；
- [x] Prepare/Chunk/Finalize 请求与状态机和基线一致；
- [ ] 固定负载上传 P95 增幅不超过 10%，无新增错误；
- [x] 固定 Token fixture 的数值链路和查询口径非回归；
- [ ] content/usage/metering job age 无持续回归；
- [ ] 大 Session 构建没有无界 RSS 增长；
- [x] v1 报告、v1 Revision 和快速回滚链路保留；
- [x] migration 使用 020，14.157 数据库已正常应用；
- [x] 测试环境真实 Agent trace 确认加载 Skill 1.0.10 和 `digest_v2`。

任一项未通过，本期继续使用 `digest_v1`。不得通过隐藏灰度、缩小原始样本、
关闭 Token 检查或放宽 raw fallback 绕过门禁。

## 10. 最终建议

当前保守方案已完成代码和测试环境落地，结论是“测试可继续、生产暂不切换”：

- Top 30 和目标样本证明容量、结果投影及真实 Agent 主链路成立；
- RTK 未成为运行时依赖，客户端、上传协议和 Token 统计代码路径未改变；
- 上传状态机和 Token 固定 fixture 已通过功能非回归；
- 全量 corpus、Human Gold、正式配对 A/B、上传 P95、队列年龄和 RSS 仍是生产
  前置。

这能优先解决“日报只总结聊天、没有总结结果”的真实问题，同时把容量修复、
Session 上传和 Token 统计的回归风险控制在较小范围内。
