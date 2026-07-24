# AIDA-BUG-20260723-017：Agent 新 Session 疑似上下文污染或错误续接

> 优先级：P0  
> 状态：测试服已复现，已确认不属于 Aida 输入、Digest、Projection 或 MCP；作为 Agent 平台外部风险单独跟踪  
> 发现日期：2026-07-23  
> 范围：Agent Platform Session 隔离、Claude Code 执行上下文、Sandbox 初始化、会话恢复

## 1. 问题

测试服创建了全新的隔离 Agent Session，输入只包含 Aida 报告协议和一个 `run_id`。该 Session 的第一次工具调用却是：

```text
Read /home/liyijun/archexplor/modelbench/report/markdown.hpp
```

该绝对路径不存在于本次 Initial Message、Agent Instructions、绑定 Skill、Report Context 和本地工作目录。读取失败后，Agent 又声称当前 Aida 报告指令是附加在工具错误后的“注入文本”，并执行 `pwd && ls -la && git status`，始终没有加载 `aida-report` Skill，也没有调用 Report MCP。

这不是 Projection 内容问题。现有证据只能确认新 Session 接收了不属于本次请求的任务线索；根因可能是错误恢复历史会话、Sandbox/Claude Code 状态复用、上游模型请求上下文串接或其他平台隔离缺陷。在取得平台侧请求与恢复链路证据前，不把它武断归类为跨用户泄漏，但按潜在数据隔离事故处理。

## 2. 复现证据

```text
环境：14.157 测试服 + Agent Platform 192.168.18.107:3081
模型：GLM-5.2
Engine：claude-code
Run：89f2c5e1-2911-4015-ad31-72ce2087588e
Session：6786a587-1293-48e6-888b-f48f63834360
输入：只含 run_id 的当前报告启动协议
Context：S0210 Projection，10,911 bytes
首次工具：Read 外部绝对路径
后续工具：Bash 检查工作目录
Skill 调用：0
Report MCP 调用：0
写回：0
处置：人工取消，Run 失败
```

本机证据保存于非 Git 目录：

```text
.production-agent-analysis/replay-20260723/S0210-runid-only/
  report-context.json
  session.json
  session-raw.jsonl
  manifest.json
```

## 3. 风险

- Agent 可能执行与当前用户请求无关的文件读取、命令或外部调用；
- 报告内容可能混入其他任务事实，形成错误写回；
- 如果污染来源跨用户，可能构成数据隔离和隐私事故；
- Skill、MCP 权限和 Run 冻结均无法修复模型调用前已经错误的执行上下文；
- 仅凭 Session 最终成功/失败无法识别污染，必须检查首个动作和完整事件。

## 4. 固定处理口径

1. 本事件不进入 Aida Digest、Projection、Skill 或模型切换开发；Aida 不修改 Agent Platform、Sandbox 或模型网关规避问题；
2. Aida 创建 Session 的请求不包含 resume 参数，Agent 启动输入继续只传 `run_id`；
3. Aida 只对自身边界负责：冻结 Context、校验用户和 Run、校验 `context_hash`、校验 Selection 归属，并只允许 `agent_running` 阶段通过 MCP 写回；
4. Agent Platform 的 `completed`、普通文本输出或异常工具调用不能替代 `write_report_result`，也不能让 Aida Run 成功；
5. 本次异常回放不得用于评价 Projection 内容质量或模型能力，不阻塞当前 Aida 代码完成；生产发布仍按 Aida 自动化结果和人工报告验收单独决策；
6. 平台侧根因和隔离修复由 Agent Platform 负责，不能通过修改 Aida Skill 文案、切换模型或读取原始 Session 规避；
7. Aida 本期不新增 Agent 事件监控、首工具拦截或平台状态机适配。

## 5. 验收条件

1. 连续创建至少 100 个全新测试 Session，每个 Session 的 Engine session ID 和 Sandbox 初始化来源可审计；
2. 向前序 Session 注入唯一金丝雀路径和文本，后续全新 Session 的输入、事件、模型请求和输出均不得出现该金丝雀；
3. 并发创建不同测试用户的 Session，任一用户的唯一金丝雀均不得进入其他用户 Session；
4. 报告 Session 首个工具 100% 为绑定 Skill，随后才允许 Report MCP；
5. 服务重启、Sandbox 复用、Session cancel、timeout、retry 和 resumable 开关分别回归；
6. 平台侧提供根因、修复提交、部署版本和完整回归证据；
7. Aida Run 在污染、错误续接或首工具违规时进入明确失败，禁止写入报告。

## 6. 明确不做

- 不把外部路径加入 Skill 黑名单；
- 不通过增加“忽略历史任务”的提示词代替隔离修复；
- 不把这次失败归因于 Digest、Projection 字节数或 MCP；
- 不在根因未确认前宣称已经发生跨用户数据泄漏。
