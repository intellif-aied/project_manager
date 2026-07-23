# AIDA-BUG-20260723-015：托管报告 Agent 仍接收旧架构范围参数

> 优先级：P0
> 状态：代码已完成，待测试服部署与真实 Run 验收
> 发现日期：2026-07-23
> 范围：报告 Run 提交、托管报告 Agent 输入、Report Skill、Report MCP 写回

## 1. 问题

当前报告 Run 已经以持久化 `ai_run` 为唯一生成请求，Report Context、Selection 和来源范围均由后端绑定到 Run。但是，后端提交托管报告 Agent 时，仍继续传递以下旧架构参数：

```text
report_type
period
target
report_source_selection_id
```

Report Skill 随后要求模型读取并在写回时复制其中部分参数。这使同一个 Run 的权威身份和范围同时存在于数据库与模型输入中，形成两套可能不一致的来源。

该行为是旧架构迁移未收尾，不是现有 Run 驱动流程的真实需求。

## 2. 已确认代码事实

当前代码存在以下行为：

- `api/handler/report_run_submitter.go` 从 `ai_run.input_ref_json` 读取报告类型、周期、目标和 Selection，并通过 `reportAgentStartPromptValues` 注入托管 Agent；
- `api/service/daily_report_skill.go` 要求 Agent 从运行输入读取 `report_type`、`period`、`target`、`run_id`，并在调用 `write_report_result` 时复制报告类型、周期和目标；
- `api/handler/managed_agent.go` 的托管报告提示词仍声明并展开上述旧参数；
- `get_report_context` 已经采用 `run_id` 定位冻结的 Report Context，说明 Agent 无需再携带范围参数读取报告事实。

## 3. 风险

- 模型可能遗漏、改写或错误复制报告类型、周期、目标或 Selection；
- Agent 输入参数可能与 `ai_run` 已冻结的范围不一致；
- Skill 被迫承担后端身份参数搬运职责，模型行为变成报告正确性的前置条件；
- 后续修改 Run 或 Context 契约时，旧提示词和旧参数可能继续漂移；
- 通过继续加强 Skill 提示无法消除双重权威来源，因此不能作为本问题的修复方案。

## 4. P0 固定修复口径

托管报告 Agent 的唯一报告业务身份输入是：

```text
run_id
```

确定性规则：

1. Agent 启动输入不再包含 `report_type`、`period`、`target` 和 `report_source_selection_id`；
2. MCP credential 绑定属于基础设施认证信息，不属于报告业务输入，保持现有安全注入方式；
3. Agent 使用 `run_id` 调用 `get_report_context`，报告类型、周期、目标、Selection、来源身份和完整 Context 均由后端根据 `ai_run` 解析；
4. Agent 调用 `write_report_result` 时只提交 `run_id`、报告正文和可选摘要，不再提交报告类型、周期、目标或 Selection；
5. Agent 调用 `write_report_failure` 时只提交 `run_id` 和错误信息；
6. 报告写回的归属、权限、范围及一致性全部以后端持久化的 `ai_run` 和已冻结 Report Context 为准；
7. Skill 删除读取、保存和复制旧范围参数的要求，不允许用提示词继续兼容双重参数来源；
8. 后端不得信任 Agent 提供的报告类型、周期、目标或 Selection 来决定读取范围或写回目标。

## 5. 本次修复范围

本 P0 必须同步收口以下位置：

- Report Run 向托管 Agent 提交的 Start Prompt Values 和 Initial Message；
- 托管报告 Agent 的系统提示词和参数模板；
- Aida Report Skill 的读取与写回协议；
- `write_report_result` 与 `write_report_failure` 的 Run 定位和后端取值逻辑；
- 相关 API、Service、MCP schema、单元测试和集成测试。

本次不改变：

- `ai_run` 的创建与持久化生命周期；
- Selection、Digest 和 Report Context 的后端冻结规则；
- Agent 使用 MCP credential 的认证方式；
- 前端创建 Run 和读取 Run 状态的现有流程；
- Codex、Claude Code 上传和 Token 统计链路。

## 6. 验收条件

以下条件必须全部满足：

1. 创建报告 Run 后，发往托管 Agent 的报告业务参数只有 `run_id`；
2. Start Prompt Values 和 Initial Message 中不再出现报告类型、周期、目标和 Selection；
3. `get_report_context` 仅凭 `run_id` 返回该 Run 已冻结的完整 Context；
4. `write_report_result` 仅凭 `run_id` 定位报告归属，并从后端 Run 数据取得报告类型、周期、目标和 Selection；
5. `write_report_failure` 不要求模型复制任何范围参数；
6. 修改 Agent 输入中的任意文本都不能改变该 Run 的报告范围和写回目标；
7. Skill 正文不再要求模型读取或复制旧范围参数；
8. 个人日报、个人周报、团队报告和部门报告的既有生成与写回回归通过；
9. Selection、Digest hash、Report Context hash、MCP 权限和报告写回完整性校验不被弱化；
10. Codex、Claude Code 上传、Token 统计和其他 Session 链路不受影响。

## 7. 当前修复结果

2026-07-23 已完成以下代码收口：

- Report Run Submitter 注入的系统报告身份参数只保留 `run_id`；
- 默认 Agent Instructions、Start Prompt Template 和动态运行消息删除报告类型、周期、目标和 Selection；
- Report Skill 改为只用 `run_id` 读取 Context，并只用 `run_id` 写回结果或失败；
- `write_report_result` schema 只要求 `run_id + content`，`summary` 保持可选；
- `write_report_failure` schema 只要求 `run_id + error_message`，`error_code` 保持可选；
- 写回处理器根据 `ai_run.input_ref_json` 解析报告类型、周期和目标，不再读取 Agent 复制的范围字段；
- 为兼容可能仍在运行的旧 Agent，请求中多余的旧范围字段会被忽略，不能改变写回目标；
- 已部署的旧默认 Agent 模板仍可被识别，并由现有资产修复逻辑更新为 `run_id` 单参数模板。

自动化验证：

```text
cd api && go test ./... -count=1
结果：全部通过
```

本次没有修改数据库、前端、Daemon、Session 上传、Digest 或 Token 统计代码。

## 8. 完成定义

本问题只有在代码修改、自动化测试和测试服真实报告 Run 验收全部通过后才能关闭。仅修改 Skill、仅隐藏提示词字段或要求模型正确复制参数，均不视为修复完成。
