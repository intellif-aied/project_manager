# 第三轮真实链路与 Agent 验收

> 执行日期：2026-07-17  
> Digest 版本：`session-digest/v2.9.0`  
> 环境：14.159 冻结样本 -> 14.157 开发测试环境  
> 当前状态：10 个样本的隔离上传、服务端 Digest、真实 Agent 运行与日报回写均已完成

## 1. 上传隔离结论

本轮不调用 14.159 用户已安装的 Aida，不读取或修改共享 `~/.aida.yaml`、上传索引和常驻进程状态。

真实链路使用：

- 独立 worktree 源码构建的临时二进制 `/tmp/aida-stage3`；
- 独立 `HOME=/tmp/aida-stage3-home`；
- 隔离 Session 目录，仅放置 S01-S10 原文件的只读软链接；
- 显式注入 14.157 API、t01 Token，并关闭自动更新；
- 运行前通过 `/auth/me` 和 `aida status` 双重确认 uid 303 / t01；
- 测试二进制 SHA-256 为 `1c23a0903e5a7b743d3968bb2d2ac3528abef02b709200304536fc8b20c900e9`，与用户安装版 SHA-256 `a272cf5920243023be03e8351ba83ff6c4b476fee1ec844262f74f6709b5e2ee` 不同。

`aida upload --all` 仅发现冻结的 10 个样本，10/10 readiness 为 READY。由于 14.157 已保存相同 SHA 的完整原文，服务端按正常协议返回 `incremental=unchanged`、`chunks=0` 并复用原文；这验证了真实客户端扫描、身份、prepare 和 readiness 链路，没有直接写数据库，也没有伪造上传结果。

## 2. 14.157 Digest 构建结果

服务端已为 10 个 Session 的 11 个活动切片全部构建 `v2.9.0`，状态均为 `ready`。S10 跨日，因此对应两个独立切片。

| 样本 | Session ID | Slice key | Digest 字节 | Digest SHA-256 |
| --- | --- | --- | ---: | --- |
| S01 | `019f1d50-19f8-7253-a141-0c2ce417d6c0` | `83dc3dae-1297-457d-a6f5-af48661958a2` | 516,571 | `38e5adf99a0a024d10fed36bd33184895ceb98ffa9e09cb8f693850c6649fbe0` |
| S02 | `019f1d70-3d87-73f2-a849-03b45a15f5fe` | `b6c7a6ed-6bfe-404f-9109-49aa5179cbb8` | 485,727 | `88555595c2d8205d967b90162d4b8a42cde3bbd5f48b053897c0e76eb7ba5f1f` |
| S03 | `019f4bac-2026-7072-83f2-1a93ea32f2bd` | `fe943d51-db2a-46a9-a501-08d0b5e42836` | 204,465 | `6bbb6d6e37bbe6180982143af9279887d978413b30485cca2475c897dd064d13` |
| S04 | `019f68d0-21e2-7160-a7de-d319e1ad2622` | `df071136-3818-49d7-ac16-04180f6a989d` | 232,912 | `d3db99b0b0b4df1b1ad99b7de05ec67515a81ab1180b6d392037a8e0464a520e` |
| S05 | `019ecf3c-40c8-7f02-b09e-3c336f4363bb` | `6d549be2-a7a4-40d5-b48e-c82694dda358` | 42,014 | `458e76c69c40b14bf37dbe16afaf4a86b5eb6a1e6bd89c8509a12959f5f19aa3` |
| S06 | `019f6bc4-08b8-74a3-b1cb-72bf9b0c874f` | `c63a24e0-c6cb-4050-af4d-e2bbe4e16146` | 62,312 | `d1204fd6fdc0d25316ea7319ea2397f7727938920e1dfc4972f21169b5684662` |
| S07 | `019f44e7-45c6-7200-b5df-71daf81f9d33` | `b33e23f6-58f7-4f8c-beda-8c991d2f509e` | 956,551 | `87b55a063c41bc42d64447976d279d1fc8efc458cc94415d303881cff89c9452` |
| S08 | `019ece38-f7b7-7cf2-b209-d7eaf12e3c54` | `995213cb-3d16-4054-9905-77815cd99a14` | 217,870 | `1e256b7b301ff47aff258e4cb3ef8dc9a992414e28e749ecc640cee847df998c` |
| S09 | `019f4570-fd80-7ec1-ae1d-e1a469154d69` | `c2b85e61-87b3-4c24-b77c-c6103091955f` | 2,815 | `73fb175c2bdfbc100e412e4fd715d057b182b37d26077e685c29c696987c5090` |
| S10-A | `019f68ce-9a8a-7330-b1a6-6ac55fbe38f2` | `3f648a16-2d78-474b-9585-2ff0ad2336a4` | 22,404 | `4c9aaa749841dc0f58cc9f2f9dccc0434023bb7be6e435acd5f1b7972b25c437` |
| S10-B | `019f68ce-9a8a-7330-b1a6-6ac55fbe38f2` | `cdf09231-c5fa-4213-b414-af83c17b4728` | 71,135 | `00de363842d09910d016cd17fd95c83eea364fb0c53f0845663af100ded8927b` |

S01-S10 的服务端 Digest 均低于 1 MiB。`truncated=true` 表示确定性投影和噪音清理发生过，不表示报告事实被按 Top-K 删除。

## 3. 接口模拟真实用户操作

用户明确同意本轮直接调用接口模拟页面操作。测试仍使用 t01，并按前端背后的真实服务端流程执行，没有写数据库、伪造 MCP 结果或跳过日报回写：

```text
POST /api/v1/report-source-selections
  -> 服务端冻结所选 Slice，返回 selection_id、context_bytes 与 warning_required
POST /api/v1/ai-assets/report-agents/default/runs
  -> 创建真实 Agent run
GET  /api/v1/ai-assets/agent-runs/{run_id}
  -> 轮询运行状态
Agent -> MCP get_sessions
  -> 按 digest_v2 读取冻结 Digest
Agent -> MCP write_report_result
  -> 回写并持久化日报
```

固定运行条件：

- 默认 Report Agent：`aida-agent-djxxw3mpr922`，Agent Version `30`；
- 模型：`MiniMax-M2.5`；
- Agent 返回的 Skill 绑定：owner `100866`、slug `aida-report`、version `1.0.33`；
- Digest：`session-digest/v2.9.0`；
- MCP 读取模式：`digest_v2`；
- Redaction：`report-redaction/v1`；
- 10 次 selection 均为 `warning_required=false`。

Agent 查询接口返回了上述 Skill 绑定版本，但本次 run 响应没有提供可核验的 Skill SHA-256 与 Prompt 哈希，因此本文不把这两个字段写成“已验证固定值”。这不影响 Digest 读取和日报回写链路结论，但属于后续可观测性缺口。

### 3.1 运行结果

| 批次 | 日期 | 样本 | context bytes | Selection ID | Run ID | Report ID | 耗时 | 状态 |
| --- | --- | --- | ---: | --- | --- | --- | ---: | --- |
| B01 | 2026-07-06 | S01 | 126,324 | `2d45ac9f-1631-414c-b389-9e236ba39fe2` | `5a76583d-cff5-425c-b4b2-94503909ef03` | `998ab237-0da6-468c-bcd7-f06ba8b6cc93` | 86.127s | succeeded |
| B02 | 2026-07-15 | S02 | 76,311 | `4f8905d0-e9f1-4fc1-99f8-659a3fe5576b` | `d904c87f-cba4-4924-b033-4ce29911ffc9` | `43a8e61a-219c-43b1-9c79-fa7e6439dc29` | 70.714s | succeeded |
| B03 | 2026-07-11 | S03 | 26,177 | `6becce3e-340c-43a2-99a7-d667303a3021` | `473845d6-c6ff-4523-98e0-25942df003e1` | `b86508cb-00ed-49fd-8b15-f0f61d154161` | 27.810s | succeeded |
| B04 | 2026-07-16 | S04 | 52,755 | `0b34b63a-7f8c-4e38-b17a-760868c7de77` | `d9c0a50b-c4d9-4b68-b484-90d2fe536622` | `d156d48a-8afc-455d-87c9-b6ea963a6478` | 34.625s | succeeded |
| B05 | 2026-06-16 | S05 | 9,249 | `5929c634-035a-4307-a0ef-58a506f5c65d` | `6b6a6f6a-fe65-4d06-a226-b87bb24e74a8` | `d2c460f8-7dc5-404d-96ff-b9a826bb402d` | 47.359s | succeeded |
| B06 | 2026-07-17 | S06 | 13,515 | `5f21063c-6bb9-4e5a-8e8a-4bd13c710a42` | `db15925c-86b7-4d28-b288-a5350f5b4a4d` | `78c9409d-db01-46ab-8052-e1d97283cad6` | 24.355s | succeeded |
| B07 | 2026-07-10 | S07 | 54,942 | `97c23bbd-f78f-49d7-88f3-46cce7e60e80` | `934e4684-bd83-49d5-b4f6-4745bd211b9b` | `9558be35-8cdf-4cb9-a4fe-95e222a1d9de` | 27.810s | succeeded |
| B08 | 2026-06-22 | S08 | 27,440 | `a828a324-328d-478d-ba43-7c28d1b88c9d` | `dd4f96ed-f63c-4dd3-b77c-e98169627b4b` | `9d5e460e-1343-41f5-b661-ada73d08fa84` | 26.988s | succeeded |
| B09 | 2026-07-09 | S09 | 2,369 | `43d9b156-6eca-4d52-ac77-0ae5f649ff3e` | `94f533a8-e33c-48ef-8582-e4dd6879e852` | `af7d4b1b-a55e-4731-bf28-9844e11e6f98` | 20.798s | succeeded |
| B10 | 2026-07-16 | S10 两个 Slice | 20,195 | `d16818ea-439e-4b1e-95c7-155e4ea8eeea` | `cb348bf0-8522-4650-8254-dee492b03987` | `d156d48a-8afc-455d-87c9-b6ea963a6478` | 24.433s | succeeded |

10/10 次运行均观察到一次真实 Digest MCP 响应和一次 `write_report_result`，没有出现原来的分页循环、`token_limit_exceeded` 或 `infrastructure_failure`。

B04 与 B10 的报告日期均为 2026-07-16。产品按“用户 + report type + 日期”维护一份日报，因此两次成功写入复用了同一个 Report ID；后执行的 B04 覆盖了 B10 的持久化正文。这不是运行失败。B10 的实际 Agent 输出已从 run 结果保存到 [第三轮 Agent 日报原文](第三轮Agent日报原文.md)，本轮只能称为“10 次成功回写、9 个不同 Report ID”。

## 4. 跨样本日报校对

| 批次 | 日报效果 | 与 Digest 对照后的归因 |
| --- | --- | --- |
| B01 | 主线覆盖充分，包含 Dashboard、Token 权限、自动化、操作历史、分页、UI 与字段清理；但“弹窗自动关闭 bug”没有体现后续回退后的最终边界 | Digest 同时保留了早期结果和后续纠正；属于模型/Skill 未优先采用最终状态 |
| B02 | Session 切片、MCP、文档、测试与发布覆盖较全；概览状态数字与正文条目数量不一致 | Digest 没有丢失对应事实；属于模型自行计数和呈现不一致 |
| B03 | 插件、UI、安全和分析覆盖较好；把批量上报列为待跟进，但最终决策是维持单事件即时上报 | Digest 中存在最终决策；属于模型/Skill 的时序归并问题 |
| B04 | P0 修复、Aida 客户端、候选查询、Bug 与架构均有覆盖 | 内容完整，但暴露 Session ID、提交和主机等内部证据，属于写作层问题 |
| B05 | AutoRound 根因、修复边界和文档交付完整 | 包含哈希、绝对路径和行号等不适合默认日报的内部证据，属于写作层问题 |
| B06 | 准确保留“审计已交付，但产品仅部分完成”，并保留 Go/npm 验证失败 | 状态表达合理，验证了 `v2.9.0` 对 partial/unresolved 的保留能力 |
| B07 | HWM、Labgrid、用户映射与 Review 覆盖完整；日报写成“持续验证生产环境”，原文实际为 36 开发环境 | Digest 没有“生产环境”结论；属于模型用词错误 |
| B08 | 代码问答、MoE、PDF 翻译和 Skill 创建均恢复出来 | 暴露内部统计及跨日活动跨度，事实来自 Digest，但默认写作内部化 |
| B09 | 短 Session 的唯一实质结果被准确保留 | 默认日报仍输出私有远程地址和 commit hash，属于写作层问题 |
| B10 | SkillOpt 调研、推理配置、基础设施和 Aida logout 覆盖较完整 | 无明显 Digest 遗漏，技术证据偏多，属于写作层取舍 |

### 4.1 共性问题

跨样本可重复的问题集中在最终写作层：

1. B04、B05、B08、B09 等会暴露不必要的 Session ID、绝对路径、内网地址、commit、哈希和证据引用；
2. B01、B03 对同一主线的早期状态与最终纠正合并不稳定；
3. B01、B02 的状态数量由模型自行统计，可能与正文不一致；
4. B07 出现“开发环境”被写成“生产环境”的术语错误。

这些问题不能反向证明 Digest 缺少内容。B01、B03 的最终纠正已经存在于 Digest，B07 的“生产环境”并不存在于 Digest；继续为它们增加 Digest 语义裁剪会偏离“确定性去噪、尽可能完整保留”的产品边界。

## 5. 验收结论

- 隔离上传：通过；未使用用户本机已安装的 Aida 客户端。
- readiness：10/10 通过。
- `v2.9.0` Digest：10 个 Session、11 个切片全部 ready。
- Layer A 离线质量：通过；关键事实覆盖和完成度表达较 `v2.8.0` 明显恢复。
- Layer B 真实 Agent 链路：10/10 succeeded、10/10 调用 `write_report_result`，通过。
- Digest 产品判断：本轮没有发现需要继续增加语义删除或 Top-K 的共性证据，`v2.9.0` 候选方向合理。
- 日报产品判断：整体可读并覆盖主要工作，但默认 Skill/模型在内部证据、最终状态归并、数字一致性和术语准确性上仍有独立优化空间。
- 模块影响：本轮未修改上传协议、Token 统计、Session 原文、MCP 接口、用户自定义 Skill 或 Agent Prompt。

第三阶段的 10 样本真实链路已经完成。本结论仅用于开发测试验收，未执行生产发布；是否发布仍需单独决策。
