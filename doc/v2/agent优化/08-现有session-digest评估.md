# `tmp/session-digest` 候选 Skill 评估

> 状态：评估完成
> 日期：2026-07-15
> 对象：`tmp/session-digest/SKILL.md`、`tmp/session-digest/digest.sh`
> 结论：适合作为规则原型和离线开发工具参考，不适合作为托管报告生产链路

## 1. 结论摘要

leader 提出的方向是正确的：必须在日报模型读取大量 Session 原文之前完成低成本规则摘要。候选 Skill 也验证了 Claude/Codex 格式识别、目标、文件、命令和结果提取可以不调用 LLM。

但当前托管报告不能直接“先运行这个 Skill”，主要原因不是脚本写法，而是运行边界不成立：报告 Agent 获得的是 Report MCP 返回的数据库投影事件，不是用户主机上的 JSONL 路径。若先调用 `get_sessions` 再运行脚本，原文已经进入模型上下文；若跳过 `get_sessions`，现有 `write_report_result` 又会因为来源未完整读取而拒绝写回。

因此本方案吸收其无 LLM、规则提取思路，但把实现位置前移到 Aida 服务端，并增加版本、脱敏、覆盖、总预算、生命周期和写回一致性契约。

## 2. 当前文件状态

- Skill 声明依赖 `bash + jq`，输入是一个 JSONL 文件或目录；
- Claude/Codex 通过 JSON 字段自动探测；
- 输出是 Markdown，每个 Session 包含标题、第一条目标、短路径文件、最多 12 条命令和最长 Agent 文本；
- 使用 `set -uo pipefail`，刻意不启用 `-e`，多数解析错误使用 `|| true` 吞掉；
- 整个 `tmp/` 被仓库 `.gitignore` 忽略，候选文件不是可发布、可追踪的正式 Skill 资产；
- 当前 Report Agent/Harness 没有用户 JSONL 挂载契约，也没有 jq 可用性契约。

## 3. 可复用点

1. 不调用 LLM，方向符合成本、确定性和可审计要求；
2. 对 Claude 与 Codex 分支提取，适合作为 Golden Fixture 的第一批规则来源；
3. 容忍 live JSONL 尾部半行，说明解析器需要处理不完整输入；
4. files、operations 去重和结果限长可作为 Go extractor 的启发；
5. “先摘要再交给下游模型”的边界判断正确，只是生产执行位置需要改变。

## 4. 生产阻断项

| 级别 | 问题 | 影响 |
| --- | --- | --- |
| P0 | Agent 没有原始 JSONL 路径，MCP 返回的是已投影事件 | Skill 无法在当前托管报告输入上直接运行 |
| P0 | 读取 MCP 后再摘要已经消耗输入 Token；跳过 MCP 又不满足来源读取完成校验 | 无法同时达成省 Token 与成功写回 |
| P0 | jq 不是 Agent Harness 保证依赖；jq 缺失或 schema 不匹配时错误被吞掉 | 可能以退出码 0 返回只有标题的“成功”摘要 |
| P0 | 没有整个响应的 byte budget；文件数量和 Session 文件数量不设上限 | 注释中的“约 1–1.5KB”不是可执行硬约束 |
| P0 | 原始命令会进入输出，没有 Bearer、Cookie、DSN、环境变量或私钥脱敏 | 可能把凭据交给模型或写入报告 |
| P1 | 只取第一条用户请求和最长 Agent 文本 | 多任务 Session 会漏后续目标，最长文本也未必是最终产出 |
| P1 | 文件路径只保留最后两级 | 同名文件可能碰撞，无法可靠追踪交付 |
| P1 | 不提供 source/included/omitted、截断和完整覆盖证据 | 无法证明所有显式来源均被表示 |
| P1 | 没有 digest/redaction version、hash、epoch 和 cursor 绑定 | 无法冻结、重放或随内容清理失效 |
| P1 | 输出文本未标记为不可信证据 | Session 中的 prompt injection 可能被下游 Agent 当作指令 |

## 5. 14.157 抽样证据

在 2026-07-15 对当前用户目录中的实际 Claude/Codex JSONL 做只读抽样。为避免泄漏，文件名和正文未写入文档，只记录输入/输出大小：

| 格式 | 输入 bytes | 输出 bytes | 输出行数 | 观察 |
| --- | ---: | ---: | ---: | --- |
| Codex | 150952 | 75 | 2 | 仅标题，关键字段为空 |
| Codex | 1014868 | 75 | 2 | 仅标题，schema 路径未命中 |
| Claude | 159801 | 34 | 2 | 仅标题，关键字段为空 |
| Claude | 9890608 | 47 | 2 | 仅标题，不能视为有效压缩 |

另用同一脚本模拟 jq 不可用：退出码仍为 0，输出 1 行、73 bytes 的标题，没有目标、产出、文件或校验。由此可见，“输出很小”不能单独作为成功标准，必须同时校验 coverage 和关键事实。

该抽样只说明当前 schema/错误处理风险，不用于估算最终服务端 Digest 的质量或压缩率。正式门禁以版本化 Golden Fixture 和同源真实 Agent A/B 为准。

## 6. 规则迁移关系

| 候选 Skill | 目标服务端契约 | 处理方式 |
| --- | --- | --- |
| 第一条目标 | `goals[]` | 改为保留所有有效用户任务，去除环境注入 |
| 最长 Agent 文本 | `outcomes[]` | 改为 final/complete/验证结论优先，不按长度选择 |
| 最后两级文件路径 | `files_changed[]` | 保留可区分的仓库相对路径 |
| 原始 Bash/function cmd | `validations[]` | 只保留规范化测试族、状态和脱敏短结果 |
| 无 blocker | `blockers[]` | 从最终未解决失败和明确阻塞提取 |
| 无覆盖信息 | `coverage` | 增加 source/included/omitted/truncated/representation |
| Markdown | 版本化 JSON | 便于 byte budget、schema 测试、hash 和不可信数据边界 |

## 7. 最终采用方式

- 不把 `tmp/session-digest` 加入默认报告 Agent 的 Skill 列表；
- 不要求 Agent 安装 jq、访问用户目录或接收 JSONL 路径；
- 不修改 `sandboxed-agent-platform`；
- 可将候选规则转换为脱敏的 Claude/Codex Fixture 和 extractor 单元测试；
- 生产实现采用 `03-数据契约与摘要规范.md` 定义的服务端版本化 Digest；
- 若未来需要个人开发机上的独立 CLI，可另行加固此脚本，但不得与托管报告的完整性和安全验收混为一谈。
