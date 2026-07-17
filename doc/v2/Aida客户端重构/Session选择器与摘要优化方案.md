# Session 选择器与摘要优化方案

> 文档状态：方案已确认，尚未开发
> 更新时间：2026-07-17
> 适用范围：Aida CLI 的 Claude Code、Codex Session 列表
> 版本策略：不提前增加版本号，随下一次正式客户端发布交付

## 1. 背景

当前 Session 选择器存在三个信息层级问题：

1. `sub-agent(n)` 对普通用户没有决策价值；用户选择根 Session 后，sub-agent 本来就会自动跟随上传。
2. 项目目录占用大量横向空间，但用户主要依据任务内容和活动时间选择 Session。
3. 摘要直接取第一条用户消息，Claude Code 和 Codex 注入的 IDE、环境、权限、Skill、Plugin 等结构化上下文会被误认为真实需求。

典型错误摘要包括：

```text
<ide_opened_file>The user opened the file c:\\...
ide_opened_file>The user opened the file c:\\...
<environment_context>...
<skills_instructions>...
```

## 2. 产品目标

- 默认列表只展示真正帮助用户判断 Session 的信息。
- 固定使用两行布局，同时展示完整 Session ID。
- 展示首条有效需求和最近一条有效需求。
- 清理客户端注入内容，但不误删用户真实讨论的 HTML、XML、日志或代码。
- 保持 Session 分组、搜索、选择和上传协议不变。

## 3. 列表布局

推荐布局：

```text
> [ ] codex  07-16 14:43:05  修复 Session 上传后的 Token 重复统计
      019f69ee-4e81-7c91-9732-eeb9836a9879  最近：重新上传 1066 数据并复查
```

展示规则：

- 第一行：选择状态、客户端、最后活动时间、首条有效需求。
- 第二行：完整 Session ID、最近一条有效需求。
- 首条和最近摘要相同时，第二行只展示 Session ID。
- 不展示目录和 `sub-agent(n)`。
- 目录、sub-agent Session ID、摘要、模型和 agent path 继续作为隐藏搜索字段。
- 根 Session 被选择后，上传成员仍包含全部 sub-agent。
- JSON 模式保留现有字段，并以可选字段新增 `recent_summary`，避免破坏脚本兼容。

Bubble Tea 的可见条目数必须按每条两行重新计算，不能继续使用当前一条 Session 占一行的高度公式。

## 4. 摘要数据模型

`SessionInfo` 增加以下本地字段：

```text
Summary              首条有效需求
RecentSummary        最近一条有效需求
SummaryAt            首条有效需求时间
RecentSummaryAt      最近有效需求时间
SummaryStatus        ok / empty / parse_error
SummarySource        provider 与事件来源
```

服务端上传协议保持兼容：

- `summary` 继续上传首条有效需求；
- `recent_summary` 首期只用于本地列表，不要求 API 和数据库增加字段；
- CWD 和父子关系继续上传，只从默认终端列表隐藏。

根 Session 的首摘要优先来自根 Session。最近摘要从根 Session 和其 sub-agent 的有效候选中按时间选择最新一条。

## 5. 统一提取流水线

Claude Code 与 Codex 不能继续各自直接取第一条消息。两类 Provider 都把用户文本交给统一的摘要提取器：

```text
读取用户事件
-> 解码内容载体
-> 识别结构化注入
-> 剥离或丢弃注入块
-> 判断是否为有效用户需求
-> 提取首条与最近候选
-> Unicode 安全截断
```

### 5.1 三类处理结果

**整条丢弃**：消息只有 IDE 操作、系统说明、权限、工具结果、sub-agent 通知或客户端运行上下文。

**剥离后保留**：消息前部是结构化上下文，后部仍包含用户真实需求。只删除上下文块。

**原样保留**：用户在真实需求中讨论 HTML、XML、标签解析或粘贴代码。不能因为文本包含 `<...>` 就删除。

### 5.2 Codex 重点识别

- `environment_context`
- `permissions instructions`
- `collaboration_mode`
- `skills_instructions`
- `apps_instructions`
- `plugins_instructions`
- `multi_agent_mode`
- Memory、AGENTS 指令和工具生成通知

### 5.3 Claude Code 重点识别

- `ide_opened_file`
- `ide_selection`
- `system-reminder`
- `command-message`
- `command-args`
- `local-command-caveat`
- `local-command-stdout`
- `task-notification`

### 5.4 容错要求

- 支持完整、嵌套和连续多个上下文块。
- 支持缺少 `<`、缺少闭合标签、内容被截断等不完整格式。
- 支持 Windows 路径、引号和反斜杠。
- 对序列化为 JSON 内容数组的文本先结构化解码，再参与分类。
- 规则只在消息开头、独立块或满足客户端固定签名时生效，避免误删正文中的标签。
- 清理器必须幂等，同一消息执行两次得到相同结果。

## 6. 有效摘要规则

- 首摘要取第一条有效用户消息的首个有效句子。
- 最近摘要取最后一条有信息量的有效用户消息。
- `继续`、`可以`、`好的`、`开始吧`、`确认`等短确认语不作为最近摘要。
- 首尾相同只保留一份。
- 找不到有效内容时显示“暂无摘要”，不得回退到原始注入文本。
- 截断按 Unicode rune 处理，不能按字节切断中文或其他多字节字符。
- 摘要清理同时作用于终端显示和上传的 `summary`，避免脏摘要进入服务端。

## 7. 本地索引

当前 `session-index.json` 版本为 v2，已经缓存旧摘要。实现本方案时必须升级到 v3：

- v2 索引不再复用；
- 首次运行重新扫描并生成首摘要、最近摘要和时间；
- 后续仍按文件大小和修改时间复用缓存；
- 索引升级只影响本地摘要缓存，不改变上传 cursor 和 `upload-state.json`。

## 8. 代码边界

建议新增独立的纯逻辑文件 `daemon/session_summary.go`，由以下位置调用：

- `daemon/device_client.go`：Claude Code 用户消息候选；
- `daemon/codex_scan.go`：Codex 用户消息候选；
- `daemon/session_group.go`：根 Session 与 sub-agent 最近摘要聚合；
- `daemon/session_index.go`：索引 v3；
- `daemon/session_tui.go`：两行布局；
- `daemon/session_pagination.go`：非 TTY 和 JSON 兼容输出。

不得修改：

- Prepare、Chunk、Finalize 协议；
- prefix checkpoint 和增量 cursor；
- Session 成组选择及上传成员；
- API、数据库、Usage Parser、Token Analytics 和 Pricing。

## 9. 测试方案

### 9.1 摘要单元测试

- 两类 Provider 的正常中文、英文和多轮需求。
- 每种已知注入标签的完整、截断、缺失尖括号和嵌套形式。
- 连续多个注入块后跟真实需求。
- 用户主动讨论 `<ide_opened_file>` 或 HTML/XML 的误杀保护。
- 只有注入、没有真实用户内容。
- 短确认语过滤和首尾去重。
- 超长中文、emoji、Windows 路径和非法 UTF-8 边界。
- 清理器幂等测试。

### 9.2 展示测试

- 80、120、168 列终端宽度。
- 每条 Session 固定两行，翻页和滚动不越界。
- 完整 Session ID 可见且可搜索。
- 无最近摘要时布局不跳动。
- TTY、非 TTY 和 JSON 输出均保持可用。

### 9.3 行为回归

- 选择根 Session 后 sub-agent 仍全部上传。
- 同一 Session 再次上传且无新增内容时 `chunks=0`。
- 上传前后 cursor、切片字节和 Token 完全一致。
- 索引 v2 升级 v3 后只重算摘要，不触发全量重传。

### 9.4 真实样本验收

- 使用包含 Claude Code 和 Codex 注入内容的脱敏真实 Session 回放。
- 列表中不得出现已知结构化标签或系统说明。
- 随机抽查首摘要和最近摘要与原始真实用户输入一致。
- 在 Windows 上验证 IDE 路径、引号和终端宽度。

## 10. 验收标准

1. 默认列表不再显示目录和 `sub-agent(n)`。
2. 每条 Session 显示完整 Session ID。
3. 首摘要和最近摘要都不包含客户端结构化注入。
4. 用户真实的代码和标签讨论不被误删。
5. 搜索仍能通过目录、根/子 Session ID 和摘要命中。
6. Session 分组、增量上传、切片和 Token 结果无变化。
7. v2 缓存不会让旧错误摘要继续显示。
