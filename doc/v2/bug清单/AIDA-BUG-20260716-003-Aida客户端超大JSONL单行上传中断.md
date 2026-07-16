# AIDA-BUG-20260716-003：Aida 客户端超大 JSONL 单行导致 Session 上传中断

> 优先级：P0  
> 状态：代码已修复、待发布
> 发现时间：2026-07-16  
> 发现环境：测试服 `192.168.14.157`，测试账号 `t01`，Aida `0.1.4`  
> 主要责任边界：Aida 客户端分块与上传前校验  
> 次要责任边界：服务端 staging 生命周期与 Session 状态一致性  
> 本文记录真实测试结论和修复验收要求；修复代码已完成，但尚未发布生产。

## 1. 问题结论

当前 Aida 客户端能够分块上传总体积很大的 Session，但不能上传“任意合法的本地 Session”。只要某一条 JSONL 记录超过客户端 4MB chunk 上限，整个 Session 上传就会在客户端中断。

该问题与 Session 总文件大小无直接关系：

- 68.8MB Session 可以成功上传，因为其中每条完整 JSONL 记录均能装入单个 chunk；
- 38.5MB 和 67.9MB Session 上传失败，因为各自存在一条约 4.33MB、4.36MB 的 JSONL 记录；
- 失败发生在服务端 Digest 构建之前，因此服务端 Digest 无法缓解或修复该问题。

准确归因是：**4MB 限制来自 Aida 客户端；服务端当前允许最大 64MB 的未压缩 chunk。**

## 2. 真实复现证据

使用本机真实 Codex Session，通过 Aida `0.1.4` 登录测试账号 `t01`，一次选择 5 份此前未上传的 Session。

### 2.1 上传结果

| Session | 本地文件大小 | 结果 | 说明 |
| --- | ---: | --- | --- |
| `019f406e-7c3a-7751-9dbc-668ec422dc35` | 68,785,001 B | 成功 | 32 个 chunk，Digest ready |
| `019f4f7a-6fd2-7d72-b624-63b96bc910cd` | 26,763,397 B | 成功 | 27 个 chunk，Digest ready |
| `019eca80-d89d-76c0-8222-4939c889bd06` | 25,965,714 B | 成功 | 39 个 chunk，Digest ready |
| `019f44e7-45c6-7200-b5df-71daf81f9d33` | 38,474,491 B | 失败 | 第 14,456 行为 4,325,340 B |
| `019ece38-f7b7-7cf2-b209-d7eaf12e3c54` | 67,889,475 B | 失败 | 第 1,623 行为 4,363,901 B |

客户端错误：

```text
session JSONL line cannot fit in a chunk:
line=14456 bytes=4325340 limit=4194304

session JSONL line cannot fit in a chunk:
line=1623 bytes=4363901 limit=4194304
```

### 2.2 服务端残留状态

两个失败 Session 均已在服务端留下记录：

- `sessions.content_status = available`；
- 没有 active generation；
- 存在未完成的 staging generation；
- 分别残留 29 个和 6 个已接收 chunk；
- 没有 `session_content_slices`；
- 没有 ready Digest；
- 不会成为有效的 Report Source。

因此失败不仅影响上传，还会形成“Session 看起来 available，但实际上不能用于报告”的不一致状态。

## 3. 根因分析

### 3.1 P0：客户端 chunk 上限与单行上限不一致

`daemon/session_sync_client.go` 当前定义：

```go
defaultSyncChunkBytes   = 4 << 20
defaultSyncMaxLineBytes = 8 << 20
```

`daemon/session_chunking.go` 按完整 JSONL 行分块，并执行：

```go
if len(line) > limits.MaxChunkBytes {
    return false, fmt.Errorf(
        "%w: line=%d bytes=%d limit=%d",
        errSessionChunkTooLarge,
        lineNumber,
        len(line),
        limits.MaxChunkBytes,
    )
}
```

结果是：

- 客户端允许读取最大 8MB 的单行；
- 但每个上传 chunk 只能是 4MB；
- 4MB～8MB 的完整 JSONL 记录可以被读取，却必然无法上传；
- 当前协议按完整 JSONL 事件维护 line/cursor，客户端不能直接把一条 JSON 对象任意截成两个独立 chunk。

### 3.2 服务端没有造成 4MB 限制

`api/handler/session_sync.go` 的服务端未压缩 chunk 上限为：

```go
maxSessionSyncUncompressedChunk = 64 << 20
```

本次两条失败记录均小于 64MB。如果客户端将其作为单独 chunk 上传，服务端容量限制不会拒绝。

因此不能通过修改 Digest 预算、MCP 分页或 Report Agent Skill 修复本问题。

### 3.3 P0：客户端在完整预检前创建 staging

当前顺序为：

1. 客户端调用 `/session-syncs/prepare`；
2. 服务端创建 Session、source 和 staging generation；
3. 客户端从头流式读取并逐 chunk 上传；
4. 读取到超大单行后才发现无法分块；
5. 客户端直接返回错误，没有 finalize，也没有 abort。

这导致错误发生前的 chunk 已经永久进入 staging。

### 3.4 P0：服务端状态与 active generation 不一致

未完成上传没有 active generation、内容切片和 Digest，但 Session 仍显示 `available`。状态字段未能准确表达：

- staging 上传中；
- staging 上传失败；
- active 内容可用；
- Session 元数据存在但内容不可用于下游。

## 4. 影响范围

容易产生超大单行的真实内容包括：

- MCP 或工具一次返回的大结果；
- 图片、截图或 base64 数据；
- 压缩历史和嵌套会话结果；
- 大型补丁、日志、测试结果或序列化对象；
- 单次 Agent tool result 中包含的大文本。

业务影响：

1. 用户无法上传包含此类记录的完整 Session；
2. Session 内容、Token、成本和报告来源可能不完整；
3. 服务端 Digest 根本没有机会运行；
4. 用户重复上传仍会在同一行失败；
5. staging 和部分对象持续占用数据库及对象存储；
6. 页面可能把不可用 Session 错误展示为 available；
7. 若没有明确错误展示，用户会误认为上传成功但日报缺少内容。

## 5. 修复要求

### 5.1 客户端 P0 修复

1. 在调用 `/session-syncs/prepare` 之前完成本地可上传性预检；
2. chunk 上限必须至少覆盖客户端允许的完整单行上限，并与服务端能力对齐；
3. 4MB～8MB 的完整 JSONL 行必须能够作为单独 chunk 上传；
4. 不得把单个 JSONL 事件任意拆成两个服务器无法独立解析的 JSONL chunk；
5. 超过服务端最大能力的单行必须在任何远端写入前失败，并给出明确的行号、大小和上限；
6. 上传中途发生不可恢复错误时，客户端必须调用明确的 abort/cleanup 流程；
7. 本地 upload state 只能记录服务端已确认的 checkpoint，不得把失败 generation 当成成功来源。

推荐实现优先级：

- 第一阶段：将 chunk 与单行上限统一到不小于 8MB，并增加 prepare 前完整预检；
- 第二阶段：由服务端 capability 或版本契约下发允许的 chunk/line 上限，避免客户端硬编码漂移；
- 第三阶段：如需要支持超过服务端单 chunk 上限的单事件，再设计带 framing/reassembly 的协议，不能直接切断 JSONL。

### 5.2 服务端 P0 修复

1. 提供幂等的 staging abort 接口，或在上传失败/超时后自动回收；
2. 未 finalize 的 generation 不得进入 active；
3. 没有 active generation 时，Session 不得显示内容 `available`；
4. staging 必须有明确 TTL、失败状态和可观测错误码；
5. abort/过期清理必须删除或失效关联 chunk、处理任务和对象存储对象；
6. 相同 Session 重试时能够安全续传或重建，不产生多份悬空 staging。

## 6. 测试方案

### 6.1 客户端边界测试

至少覆盖完整 JSONL 单行：

| 用例 | 期望 |
| --- | --- |
| 4MB - 1B | 上传成功 |
| 4MB + 1B | 上传成功，不再触发现有错误 |
| 8MB - 1B | 上传成功 |
| 客户端/服务端协商上限 - 1B | 上传成功 |
| 协商上限 + 1B | prepare 前清晰拒绝，服务端零写入 |
| 超大行位于文件中部 | 不留下 staging 或部分 Session 可用状态 |
| 总文件大于 300MB、每行均合法 | 分块上传并 finalize 成功 |
| 失败后重新上传 | 可恢复或干净重建，不重复、不污染 |

### 6.2 服务端状态测试

必须断言：

- prepare 后未 finalize 的 source 不会被报告来源读取；
- abort 后 staging、chunk、任务和对象状态一致；
- TTL 清理可回收客户端断线留下的 staging；
- `content_status=available` 必须有 active generation 和 active projection；
- 失败 Session 不产生 `session_content_slices` 和 ready Digest；
- 重试成功后只保留一份有效 active generation。

### 6.3 真实回归

修复后使用本次两份真实失败 Session 重测：

1. Aida CLI 上传成功；
2. generation finalize；
3. 内容投影和 Digest 均 ready；
4. Report Source 可以选择对应切片；
5. 真实 Report Agent 使用 `digest_v1`；
6. `get_sessions` 单次返回、`has_more=false`；
7. `write_report_result` 成功并真实回写。

## 7. 验收标准

满足以下条件才能关闭本 Bug：

1. 本次 4,325,340 B 和 4,363,901 B 的真实单行均可成功上传；
2. 总文件大于 300MB 且单行合法时可以完成增量上传；
3. 客户端不再存在“允许读取 8MB、只能上传 4MB”的矛盾；
4. 任何不可恢复的上传失败都不会留下错误的 `available` Session；
5. 不存在无期限 staging、悬空 chunk 或错误 ready 状态；
6. 失败后重试具有幂等性；
7. 上传成功后 Digest 和真实 Agent 日报链路通过；
8. 客户端、API 和状态清理测试全部通过。

## 8. 与 Session Digest 修复的关系

两者解决不同阶段的问题：

```text
本地 Session
  -> Aida 客户端分块上传        本 Bug 发生位置
  -> 服务端 finalize/内容投影
  -> Session Digest 构建
  -> Report MCP 单页返回
  -> Agent 生成并回写日报
```

Session Digest 已验证能够让 340.7MB 报告来源在约 35 秒内完成真实 Agent 生成，但前提是 Session 已经成功上传并完成投影。本 Bug 不修复，部分真实大 Session 会被挡在 Digest 之前。
