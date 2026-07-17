# 04 测试与人工选择 Session 验收

## 1. 测试原则

本轮必须同时验证：

1. 原始 Session 完整上传；
2. 确定性噪音被删除；
3. 所有用户可见语义块完整保留且没有截断；
4. 用户手动选择的 Session 与冻结快照完全一致；
5. 大上下文提示不阻止用户确认继续；
6. Agent 真实读取冻结上下文并写回日报；
7. 最终结果由用户人工判断是否合理。

禁止：

- 测试脚本自动选择 Session；
- 直接调用 `report-source-selections` 替用户勾选来源；
- 根据文件大小或预期结论自动挑样本；
- 手工构造日报或直接写数据库；
- 只跑 Digest 单元测试就宣称真实流程通过；
- 用 run `completed` 代替 `write_report_result` 成功证据。

## 2. 自动化测试

### 2.1 事件可见性

- Codex user 消息完整保留；
- Codex commentary 与 final 用户可见文本完整保留；
- Claude Code user/assistant text 完整保留；
- system、developer、reasoning、tool call、tool result、usage、图片块按原因删除；
- 未知事件不执行；若不能证明其不含用户可见语义，必须令 coverage 不完整并阻止冻结；
- 长消息大于 768/512 bytes 时内容和 Hash 保持完整；
- Markdown 列表和段落换行不被折叠成单行。

### 2.2 脱敏

- 私钥、Bearer/Basic Authorization、API Key、Token、密码、Cookie 和 URL 凭据被替换；
- 脱敏前后 block 数量不变；
- 普通产品名、Session 概念、Token 讨论和业务数字不被整段删除；
- 脱敏原因和计数可审计，但敏感原文不进入日志。

### 2.3 去重

- 同一消息的客户端重复持久化副本精确去重；
- 同一主题的不同时间状态不去重；
- 文本相似但结论不同不去重；
- 同一个 Session 上传两次产生两个用户可选择来源时，选择结果按用户勾选项保留，不做跨来源模糊合并。

### 2.4 覆盖与顺序

- `candidate = retained + allowed_omitted`；
- `semantic_blocks_truncated=0`；
- 每个 retained block 可通过 `block_ref/content_sha256` 映射来源事件；
- 同一来源顺序稳定；
- 多来源合并顺序稳定；
- 相同输入重复构建的 JSON 与 Hash 一致。

### 2.5 容量提醒

- 小于 1 MiB：不要求确认，直接创建 run；
- 等于 1 MiB：边界行为固定并有测试；
- 大于 1 MiB：返回 `warning_required=true`，不创建 run；
- 用户确认后：使用同一冻结快照创建 run，payload 未裁剪；
- 用户返回调整：不创建 run，允许重新手动选择；
- 旧的 16/64/128 KiB 预算不再截断 semantic blocks；
- 实际基础设施错误明确失败，不伪装为内容过大提示。

### 2.6 Agent 与写回

- Skill 使用 semantic blocks，不引用 v2.8 highlights 计数；
- Agent 不逐轮照抄聊天；
- 方案、决定、实现、发布和质量优化均可从上下文生成；
- 没有文件证据的明确产品决定不会自动消失；
- 正文不包含凭据、内部 ID、主机、路径、命令和纯验证章节；
- 同一主线允许合并，不再要求正文 bullet 数等于来源块数；
- `write_report_result` 成功后 run 才为 succeeded。

## 3. 真实样本准备

样本不在文档中固定为系统自动选择项。候选范围可以包括：

- 当前会话 `019f668c-2eb5-7c92-b08f-358ee84af865`；
- 用户后续指定的本机长 Session；
- 多 Session 组合；
- 清噪后超过 1 MiB 的超长组合。

测试人员可以负责：

- 核对本机文件路径、大小、行数和 SHA256；
- 使用正式 Aida 命令完整上传用户指定的 Session；
- 等待服务端 ready；
- 确认候选列表可见。

测试人员不得替用户完成页面勾选。

## 4. 真实流程与人工选择门

### 阶段 A：部署前

1. 确认 14.157 运行镜像、Digest 版本、Skill 版本和数据库迁移状态；
2. 确认共享工作区没有与本轮文件重叠的未提交修改；
3. 记录测试账号，但不使用生产账号；
4. 不清理其他用户、其他 Session 或其他会话的测试数据。

### 阶段 B：完整上传

1. 用户指定要上传的本机 Session；
2. 记录完整 JSONL 的大小、行数、SHA256；
3. 使用正式 `aida upload`，从 cursor 0 到 EOF；
4. readiness API 返回 ready；
5. 候选 Session API 能看到对应来源；
6. 不创建 selection，不创建 Agent run。

### 阶段 C：等待用户手动选择

测试人员向用户明确说明：

> 测试环境和候选 Session 已准备完成，请在 14.157 页面手动勾选本次希望用于日报的 Session，并点击生成。

此阶段必须暂停。只有用户完成页面操作后才继续。

记录：

- 用户实际选择的 Session 展示名称或 slice key；
- 选择数量；
- report type 和 period；
- 是否出现大上下文提示；
- 用户选择“返回调整”还是“继续生成”。

### 阶段 D：真实 Agent 跟踪

用户点击生成后：

1. 获取 selection id 和 run id；
2. 核对冻结来源与用户实际勾选完全一致；
3. 核对 context bytes、warning 状态和确认状态；
4. 确认 Agent 只调用一次完整 `get_sessions`；
5. 确认 Agent 没有请求 raw/full fallback；
6. 确认调用 `write_report_result`；
7. 从报告 API 读取真实持久化 Markdown；
8. 输出原文给用户，不先替用户改写。

### 阶段 E：用户人工校对

用户重点判断：

- 是否覆盖当天真正完成的方案、决定、实现、发布和质量工作；
- 是否仍然只是复述聊天；
- 是否遗漏长回复后半部分的重要结论；
- 是否把纯工具、测试和文件变更写成日报主体；
- 是否存在重复、错误当前状态或错误环境；
- 是否存在用户认为不应删除的内容；
- 多 Session 汇总是否符合日常使用视角。

## 5. 验收产物

每次真实验收必须保存：

- 测试环境和版本；
- 用户手动选择的来源清单；
- 原始文件大小、行数和 SHA256；
- raw 事件类型计数；
- semantic block 保留/删除原因统计；
- 清噪后上下文字节数和估算 Token；
- 是否触发 1 MiB 提示及用户选择；
- selection、run、Agent platform session、report id；
- Agent 工具调用与最终状态；
- 最终持久化 Markdown 原文；
- 用户人工结论和明确遗漏项。

产物不得包含 Authorization、密码、Cookie 或完整私钥。

## 6. 通过条件

同时满足以下条件才通过：

1. 自动化测试全部通过；
2. 用户指定 Session 完整上传；
3. 用户在页面手动选择，选择内容与冻结来源一致；
4. 所有候选语义块均保留或有允许的确定性删除原因；
5. 语义块没有字节截断；
6. 大于 1 MiB 时提示正确，用户确认后仍能继续；
7. Agent 和 MCP 写回完整执行；
8. 最终日报经用户人工确认没有关键内容遗漏；
9. 不影响 Aida 上传、Token 统计和其他用户 Session；
10. 未进行任何生产操作。

## 7. 停止条件

出现以下任一情况立即停止，不用“继续调提示词”掩盖：

- retained semantic block 无允许原因消失；
- 出现可能包含用户可见语义的未知事件却仍标记 coverage 完整；
- 用户可见消息被截断；
- 选择冻结与用户勾选不一致；
- 1 MiB 提示确认后仍被旧硬上限阻断；
- Agent 未调用 `get_sessions` 或未调用 `write_report_result`；
- run 显示成功但报告未真实写回；
- 需要修改 Aida 上传或 Token 统计才能继续；
- 共享工作区出现与本轮重叠且归属不明的修改。
