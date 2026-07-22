# AIDA-BUG-20260722-014：报告 Agent 误判 Digest 内容缺失

> 优先级：P0
> 状态：生产已止血，待回归关闭
> 环境：Aida 生产服 `192.168.14.182`、Managed Agent Platform `192.168.18.107:3081`
> 发现日期：2026-07-22

## 1. 事故现象

用户生成 2026-07-22 个人日报后，报告 Run、Agent Session 和报告写回均显示成功，但报告正文没有读取已经冻结的具体工作事实，只生成了“存在多次 Codex 会话、详细工作记录未能展开”的空泛内容，并建议用户查阅代码仓库或开发文档。

生产证据：

- Managed Agent Session：`ad719068-c3fb-47f1-b204-d005463cf487`；
- Aida Run：`2fa77462-0c66-4fad-a89d-08d2e0e71e7a`；
- 写回报告：`f1b44cfc-1717-4cd4-b268-0a71728e9cc1`；
- Agent Session 状态：`completed`；
- 报告写回状态：`saved`。

本事故属于“技术状态成功、业务内容错误”。不能以 Run completed 或报告 saved 判断报告内容正确。

## 2. 影响与严重性

- 用户得到没有实际工作事实的错误日报；
- 系统将错误报告标记为生成成功，用户无法从状态识别异常；
- 使用同一 Report Context V1、Digest V2 和旧 Report Skill 的大上下文个人日报存在同类风险；
- 错误报告可能覆盖用户对 AI 报告可信度的判断，因此定级为 P0。

本事故不代表以下链路失败：

- Digest 未生成或丢失；
- Report Context 未冻结；
- Report Run 等待、Agent 提交或写回失败；
- Codex、Claude Code 上传或 Token 统计异常。

## 3. 已确认事实

该 Run 的 Report Context 已完整返回：

- 实际 Context 约 `992381 bytes`；
- MCP 双层 JSON 工具结果约 `1580502 characters`；
- Digest `completeness=complete`；
- Digest `compaction=detailed`；
- Digest 硬限制为 `67108864 bytes`，本次没有触发内容限制；
- 选择范围包含 22 个 Session、37895 条源事件和 4006 条纳入事件。

报告期投影后的权威工作事实位于：

```text
sessions[].digest.report_period_summary.days[].highlights
```

以下字段在 `period_result_focused` 表达中按设计为空，只保留来源和覆盖元数据：

```text
sessions[].digest.items[].digest.work_units
sessions[].digest.items[].digest.daily_summaries
sessions[].digest.items[].digest.discussion_aggregates
```

因此，本事故不是 Digest 截断或 Context 内容不足。

## 4. 直接根因

生产 Report Skill 只要求 Agent 使用 `report_period_summary`，没有固定完整 JSON 路径，也没有说明 item 下的 `work_units=[]` 是报告期投影的正常结构。

Agent 在 MCP 大结果落盘后自行使用脚本探索 JSON，实际检查了：

```text
sessions[0].report_period_summary
sessions[].digest.items[].digest.work_units
```

但没有读取正确字段：

```text
sessions[0].digest.report_period_summary.days[].highlights
```

Agent 随后把 `work_units=[]`、`aggregated_work_unit_count` 和覆盖统计错误解释为“工作内容被截断”，并基于该错误判断生成报告。

## 5. 促成因素

### 5.1 Skill 数据契约不确定

后端代码明确知道合并结果的位置，但 Skill 只描述业务概念，没有将字段路径、字段优先级和禁止推断规则写成确定协议，给模型留下了自行解释空间。

### 5.2 大 Context 触发工具结果落盘

本次 MCP 结果超过 Claude Code 工具的直接展示阈值，完整内容被保存到文件。落盘不是数据丢失，但旧 Skill 没有规定落盘后必须解析完整 JSON 并提取固定路径，Agent 依赖 preview 和自行探索，放大了字段误判概率。

### 5.3 缺少真实大上下文 Agent 回归

既有测试验证了 Context、Digest hash、覆盖完整性和写回成功，没有覆盖以下真实链路：

```text
大 Report Context
  -> MCP 工具结果落盘
  -> Agent 解析完整文件
  -> 读取全部 report_period_summary highlights
  -> 生成包含具体工作事实的报告
```

### 5.4 成功判定只覆盖技术状态

`write_report_result` 成功后 Run 即完成，没有自动识别“内容被截断”“无法提取具体工作”“请查阅代码仓库”等明显错误输出。因此业务上无效的报告仍被标记为成功。

### 5.5 Skill 版本号未代表契约变化

生产 `10086/aida-report@1.0.15` 与测试 owner 下的 `100866/aida-report@1.0.45` 正文完全一致。仅提升或切换版本号不能修复本事故，发布验收必须比较实际正文和回归结果。

## 6. 生产止血

2026-07-22 已完成 Skill-only 生产修复：

1. 发布不可变 Skill `10086/aida-report@1.0.16`；
2. 固定个人日报事实路径为 `sessions[].digest.report_period_summary.days[].highlights`；
3. 明确要求读取请求周期内的全部 highlights；
4. 明确 `period_result_focused` 下 item 的空数组是正常投影结构；
5. 禁止根据 work_units、事件遗漏数和 truncated 统计判断报告事实缺失；
6. 明确大工具结果落盘后必须解析完整 JSON，不能使用 preview 判断事实缺失；
7. 将生产 `MANAGED_AGENT_REPORT_SKILL_VERSION` 从 `1.0.15` 切换为 `1.0.16`；
8. 仅滚动重建生产 API，未修改 API 镜像、Web、数据库、MCP、Digest 或客户端上传逻辑。

生产配置备份：

```text
/home/luoxian/aida/backups/skill-1.0.16-20260722T1155Z/.env
```

发布后生产 API 正常运行，容器实际配置为：

```text
MANAGED_AGENT_REPORT_SKILL_OWNER=10086
MANAGED_AGENT_REPORT_SKILL_VERSION=1.0.16
```

发布窗口观察到一条 Session finalize 返回 500，客户端随即完成 abort；后续 finalize 已成功，最近观察窗口没有持续 5xx。该现象需要在事故复盘中保留，但没有证据表明与 Skill 正文有关。

## 7. 回退方式

若新 Skill 导致新报告异常：

1. 恢复备份中的 `.env`，或将 `MANAGED_AGENT_REPORT_SKILL_VERSION` 恢复为 `1.0.15`；
2. 仅滚动重建生产 API；
3. 不删除 `1.0.16`，不修改历史 Skill 资产；
4. 不回退数据库、Web、MCP、Digest 或上传链路。

## 8. 关闭条件

事故只有同时满足以下条件才能关闭：

1. 使用生产 `1.0.16` 重新生成同类个人日报；
2. 新 Agent Session 的 SkillRef 精确为 `10086/aida-report@1.0.16`；
3. 大 Context 落盘场景下仍读取全部 `report_period_summary.days[].highlights`；
4. 报告包含来源中存在的具体项目、功能、缺陷、文档、决策、验证和未决事项；
5. 报告不出现“内容被截断”“无法获取具体工作”“请查阅代码仓库”等诊断或无来源建议；
6. Run、报告写回和原有 Codex、Claude Code 上传及 Token 统计回归正常；
7. 自动化回归纳入固定事故样本，不能只依赖人工验证。

## 9. 后续防复发项

按以下顺序处理，不扩大 Digest 的产品职责：

1. 建立 Report Context JSON 路径到 Skill 规则的显式契约；
2. 增加约 1 MB Context 和工具结果落盘的 Agent 端到端回归；
3. 验收报告必须包含具体来源事实，不能只检查 completed/saved；
4. 增加明显内部诊断文案的质量失败或告警；
5. Skill 发布时同时核对 owner、slug、version、正文 hash 和真实样本结果；
6. 保留本事故 Session、Run 和报告作为固定回归证据；
7. 复盘单次 API 滚动期间的上传请求处理，确认是否需要无损滚动或发布窗口保护。

## 10. 当前状态

```text
事故等级：P0
生产止血：已完成
代码修改：无
数据修复：未自动重写旧报告
待人工验收：使用新 Run 重新生成同类个人日报
待工程闭环：大 Context Agent 回归、内容质量门禁、发布窗口上传保护复盘
```

旧报告不会因 Skill 发布自动重写。用户重新生成后产生的新 Run 才会使用当前生产 Skill 并形成新的验收证据。
