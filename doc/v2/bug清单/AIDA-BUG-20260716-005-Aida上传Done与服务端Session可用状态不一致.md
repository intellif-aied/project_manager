# AIDA-BUG-20260716-005：Aida 客户端 Done 与服务端 Session 可用状态不一致

> 优先级：P0  
> 状态：修复中；已实现 readiness 与客户端状态语义，待 14.157 真实验收
> 发现时间：2026-07-16  
> 发现环境：客户端 `192.168.14.159`，生产 `113.100.143.91:9180`  
> 影响范围：Aida Session 上传、Token 投影、日报来源选择  
> 当前原则：先修复 Done/readiness 主问题；checkpoint 身份隔离仍作为后续兼容性修复，不在本次 P0 客户端补丁中扩大。

## 1. 问题结论

Aida 客户端在 Session 仍可能有后续内容、服务端内容 projection 尚未 ready 时，终端已经显示：

```text
Done. ... processed.
```

但同一 Session 在服务端实际状态仍为：

```text
content_status = uploading
```

结果是 Token 统计页面已经能看到 Token，但日报来源候选接口返回空列表，用户无法选择该 Session 生成日报。

这是 P0 状态语义和用户反馈错误：客户端把“本次上传请求已提交/前缀已确认”错误展示成“Session 上传完成并可用于日报”。

## 2. 真实取证

生产账号 uid `14` 的 Session：

```text
session_id: 41f1a07f-1b98-4a7d-98c7-44e37f2ad9a8
session_ref: 019f4575-4fe7-72a1-86d8-b6a4c719a73e
```

生产普通 Session 接口返回：

```text
content_status: uploading
```

日报来源接口在无日期和有日期条件下均返回：

```json
{"items":[],"page":1,"page_size":5,"total":0}
```

但生产 Token Analytics 返回：

```text
total_tokens: 456325672
data_freshness: ready
pending_source_count: 0
activity_from: 2026-07-09
activity_to: 2026-07-16
```

并且 Token Analytics Session 列表能返回同一 `session_ref`。

客户端 `192.168.14.159` 的本地证据：

| 项目 | 结果 |
| --- | --- |
| Aida 版本 | 0.1.5 |
| 本地 Session 文件当前大小 | 47,933,973 B |
| 最后确认上传游标 | 47,632,200 B |
| 最后上传时间 | 2026-07-16 08:25:58Z |
| 本地 Session 结束时间 | 2026-07-16 08:38:40Z |
| 当前账号配置 | uid 299，约 08:31 切换 |

这表明客户端显示 Done 时，Session 仍在继续写入，上传的是中间前缀快照；之后没有使用 uid `14` 完成最终补传。

## 3. 根因分析

客户端上传流程在 chunk 上传和 finalize HTTP 成功后，将结果状态 `created/updated` 计入成功，并打印 `Done`。服务端 finalize 只负责：

1. 校验 chunk 连续性；
2. 将 generation 切换为 active；
3. 创建内容 projection、usage 和 metrics 的异步处理任务；
4. 立即返回，不等待内容 projection 达到 `available/ready`。

因此存在三个不同完成状态，但客户端未区分：

```text
本次前缀上传成功
服务端 finalize 接受
Session 内容 projection ready
```

当前 Token/usage worker 可能先完成，所以 Token Analytics 已有数据；内容 projection 或 Session 状态仍未 ready，导致 Report Source 过滤掉该 Session。

此外，本地 `upload-state.json` 的 checkpoint 主要按 Session/source 标识保存，账号切换后存在复用其他账号 checkpoint 的风险，必须纳入修复审计。

## 4. 业务影响

1. 用户看到客户端 Done，以为上传成功；
2. Token 页面显示已有 Token，进一步强化“上传成功”的错觉；
3. 日报来源列表没有该 Session，用户无法生成日报；
4. 用户可能反复上传或切换账号，产生重复 generation、悬挂 staging 或错误归属；
5. 服务端失败/处理中没有回传到客户端，导致 P0 级可观测性和状态语义错误。

## 5. 修复要求

### 5.1 客户端

1. `Done` 只能表示服务端确认 Session 已达到可用状态，不能只依据 finalize HTTP 200；
2. finalize 成功后轮询 Session readiness，至少确认 `content_status=available`、active projection ready；
3. Session 仍在生成或存在 `pendingTail` 时，显示“已上传当前快照，等待 Session 结束”，不能显示最终 Done；
4. 轮询超时、projection failed 或状态不一致时必须显示失败/处理中，并给出可重试提示；
5. 本地 upload checkpoint 必须绑定账号身份、服务端地址和 Session/source，切换账号不得复用其他账号状态；
6. 账号切换时清晰提示未完成上传，并阻止静默归属到新账号。

### 5.2 服务端

1. finalize 响应必须明确区分 `accepted/processing/ready/failed`，不能只返回 active；
2. 提供按 Session/source 查询 readiness 的稳定接口；
3. 内容 projection 失败必须将 Session 标记为明确失败状态，并记录错误原因；
4. Token/usage ready 但 content projection 未 ready 时，状态接口必须返回不一致信息，不能让客户端误判成功；
5. 对长时间 `uploading` 的 Session 建立超时、告警和可恢复机制。

## 6. 验收要求

- 上传仍在生成的 Session：客户端不能显示最终 Done；
- Session 结束后再次上传：只有 content projection ready 后才显示 Done；
- projection 失败：客户端显示失败，不得显示 Done；
- Token 已 ready、内容未 ready：前端明确显示处理中，Report Source 不得静默显示为“没有 Session”；
- 账号切换后：checkpoint 不得串用，不能产生跨账号 generation；
- 完整上传成功后：Token Analytics、普通 Session、Report Source 三处状态一致；
- 对超大 Session、pending tail、网络中断、finalize 成功但 projection 延迟分别回归。

## 7. 关闭条件

只有客户端成功提示与服务端 `available/ready` 一致，处理中和失败状态可见，Token/Session/Report Source 三条链路状态一致，并完成账号切换和异常重试回归后，才能关闭本 P0。

## 8. 当前实现进度

已实现：

1. API 增加 generation readiness 只读接口；
2. 客户端 finalize 后轮询服务端权威状态；
3. 只有 `ready_for_reports=true` 才计入 READY/Done；
4. projection 延迟、失败和 pending tail 分别展示 PROCESSING、FAILED、CURRENT；
5. legacy 接口只显示已接受/处理中，不再误报 ready。

待完成：

1. 14.157 真实 Session 上传和报告来源验收；
2. 账号、服务端维度的 checkpoint 身份隔离；
3. Token Analytics、普通 Session、Report Source 三条链路一致性回归。
