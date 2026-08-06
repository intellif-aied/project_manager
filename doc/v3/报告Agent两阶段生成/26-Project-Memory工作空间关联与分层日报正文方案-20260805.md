# Project Memory 工作空间关联与分层日报正文方案

> 日期：2026-08-05
> 状态：Phase 1～4 已开发并完成测试服真实回归；尚未提交、未发布生产

## 1. 目标

Project Memory 不再只从历史日报的语义主题中猜测连续项目。它应先沉淀 Session 内稳定的工作空间身份，再把历史中可靠的项目名称、别名与该身份关联，供后续日报作为可选上下文。

日报正文保持一份，不恢复“工作概览 / 工作详情”的重复结构；但恢复项目下的具体工作推进，使用一级项目、二级成果的自然层级。

## 2. 分层日报格式

```md
1. AI Coding 提效支撑
   - Qwen3-4B：推进 130 万数据训练，并记录 step=9000 的接受长度观察。
   - GLM5.2-FP8：推进 20 万条样本数据生成，整理当前数据来源与生成进度。
2. 另一项独立工作
   - 完成当天实际推进的事项。
```

- 一级编号是项目或稳定工作主线；仅在当天事实或高相关 Project Memory 候选支持时使用项目名。
- 二级无序列表是同一主线内一至三个 Deliverable，不再压缩成一个标题，也不重复成另一段“工作详情”。
- 简单工作可只写一级编号的一句话，不强行补二级项。
- 子项只描述用户处理、推进或明确定位的工作；不得根据 Session 推导测试通过、验收通过、发布上线、生产可用等外部状态。
- Git、命令、构建、测试、部署等仍只用于关联和理解，除非用户明确把它们作为工作结果表达。

## 3. 工作空间身份

上传与 Digest/Projection 阶段按事件时间段而不是整段 Session 建立 `workspace_identity`：

1. Git remote + 仓库根目录可用时，生成稳定 `codebase_key`；
2. Git 不可用时，使用规范化 CWD 生成仅用户内可比的 `workspace_key`；
3. 同一 Session 中切换仓库或工作目录时，切为新的身份时间段；
4. 当天切片未包含 Git 操作时，可继承同一身份时间段此前已采集的 Git 线索；
5. 原始路径、remote、分支和命令不进入 Report Context，只保存内部哈希与匹配强度。

刘乐当天 10 个切片中 9 个位于同一 CWD；黄咏驰当天 3 个切片均位于同一 CWD。两类情况都应形成强工作空间关联，即使本次切片没有 `git_commits`。

## 4. Project Memory 的关联规则

Project Memory 新增内部的“项目候选 ↔ 工作空间身份”关联：

```text
workspace_identity
  -> 历史可靠项目名：芯片验证平台
  -> 可靠别名：版本流、测试执行模块、用例筛选
  -> 来源：人工最终日报 / 当天明确表述
  -> 命中强度：同一 codebase_key 或 workspace_key
```

- 人工日报或人工修改稿的明确父级项目名优先作为命名来源。
- 旧格式中非编号的父级项目标题也必须被提取；例如“芯片验证平台开发工作：”不能只保留其下的“批量测试执行”。
- 未修改 AI 日报仍优先使用结构化 Brief Subject，不能把当天成果句、进度或测试结论写入项目记忆。
- 模型名、任务名、数据任务等可作为关联别名，但单独命中时只是中等线索；与同一工作空间、连续 Session 或多个别名共同命中时才提升为高相关候选。
- 历史进度、产量、测试结果、发布日期和下一步计划不进入候选提示，避免旧状态泄漏到当天日报。

## 5. Report Context 的使用方式

服务端先用当天切片的 `workspace_identity` 匹配 Project Memory，再向 Report Agent 提供少量候选：

```text
高相关项目候选：AI Coding 提效支撑
匹配依据：当前工作与历史 Qwen3-4B 训练 / GLM5.2-FP8 数据生成线索，以及同一工作空间身份一致。
```

- 候选是辅助理解，不是当天事实，也不产生 `fact_ref`。
- 当前 Facts 明确冲突、出现独立目标或没有语义支持时，Agent 必须忽略候选。
- 首期只做用户内关联；跨用户仅在未来存在明确团队项目映射或同一 Git codebase 的可靠命名证据时再设计，不能靠相似词自动合并。

## 6. 影响范围与验收

后续实施涉及 Session/Projection 的工作空间身份提取、Project Memory 输入与解析、Report Context 候选匹配、系统日报第二阶段写作及其测试。不会修改个人 Agent、Report MCP、数据库中既有日报正文或周报/团队报告。

验收至少覆盖：

1. 同一 Session 前段有 Git 信息、后段无 Git 信息时，后段仍能匹配正确工作空间；
2. 同一 Session 切换目录后，不继承前一工作空间的项目候选；
3. 刘乐可因历史“芯片验证平台”与同一工作空间获得高相关候选；
4. 黄咏驰的人工父级标题可进入 Memory，避免退化为“批量测试执行”这一子能力名称；
5. AI Coding 提效支撑示例中，后续 Qwen3-4B / GLM5.2-FP8 连续工作可获得该项目候选，但不带入历史进度；
6. 日报只有一份分层正文，不出现概览/详情重复，也不生成测试通过、验收通过等外部状态结论。

## 7. Phase 1 实施边界（2026-08-05）

本阶段只落地 Workspace Identity 的确定性采集与影子证据，不把候选注入 Report Agent：

- AIDA 客户端在 CWD 属于 Git 仓库时读取 `remote.origin.url`，先在本机去除凭证并规范化，再上传不可逆的 SHA-256 `repository_key`；原始 remote 不离开客户端。
- 服务端兼容旧客户端；没有 `repository_key` 时仍可按用户内规范化 CWD 建立身份。
- 数据结构采用 `Workspace + 多个 Identity Key + Evidence`，同一工作空间可同时挂 Git 与 CWD 身份，避免被拆成两个项目。
- 影子证据只基于日报已经冻结的 Source Selection 写入，不修改 Project Memory 候选、Report Context 或日报正文；影子写入失败也不阻断原有夜间 Memory 流程。
- 当前 Session 协议只有整段 CWD 和 repository key，尚不能准确表达长 Session 内的目录切换。因此本阶段只形成所选 Slice 对应 Session 的工作空间观察；事件级 Workspace Segment 留到协议具备时间线元数据后实施，不能把本阶段描述为已完成切段。

Phase 1 验证通过后，下一阶段才开放只读候选检索，并以刘乐、黄咏驰等真实样本确认命中与误导率；分层日报正文属于独立改动，不与影子采集合并上线。

### 7.1 测试服验证结果

- API 已部署至测试服，migration 037 正常应用，健康检查通过。
- 新 CLI 使用隔离 HOME 上传真实 Codex Session，服务端 Prepare 阶段成功保存脱敏后的 `repository_key`；本机已安装 AIDA 未被修改。
- 选取用户 305 的一份真实日报 Source Selection 进行影子物化：40 条 Source Item 归并为 13 个 Workspace，说明相同 CWD 没有被按切片重复建项。
- 同一报告第二次物化新增 Evidence 为 0，幂等通过。
- 清理测试服 MinIO 中 5,155 个数据库未引用的孤儿 Session chunk 后，测试 CLI `0.1.27-test.20260805.2` 已完成三平台发布与回下载校验。
- 使用隔离 HOME 完整上传真实 Codex Session 成功：Session 状态为 `available`、Generation 为 `active`，CWD 与 Git Repository Key 均已保存。
- 基于该 Session 生成真实测试日报后，影子物化得到 1 个 Workspace、2 类 Identity Key（`cwd`、`git_repository`）和 2 类 Evidence，证明 Git 与 CWD 被绑定到同一 Workspace。

## 8. Phase 2：Workspace 与 Project Memory 关联

已增加服务端内部证据链：

```text
Report Brief Deliverable
  -> fact_ref
  -> frozen Session ref
  -> Workspace Evidence
  -> Project Memory 决策
  -> Project ↔ Workspace 弱关联
```

- Fact 对 Session 的来源引用只写入 `report_run_fact_sources`，不进入 Report Context JSON。
- Project Memory Agent 继续只负责 `link_existing / create_new / unresolved` 语义判断；Workspace 关联由服务端根据已接受 Brief 的 `fact_refs` 确定性写入。
- `report_project_workspace_links` 保存项目与 Workspace 的累计弱关联；`report_project_workspace_link_evidence` 保留报告、主题、置信度和来源权重，便于审计和重算。
- 同一 Session 被多个 Source Selection 复用时，读取同一 Session 已有 Workspace Evidence；不要求 Evidence 必须由当前 Selection 重复创建。
- Workspace 可关联多个 Project，以兼容 monorepo、通用工作目录和同一代码库内多条业务主线；不存在唯一归属约束。

## 9. Phase 3：只读弱提示

- 只为当天 Fact 所属 Workspace 召回历史 Project，最多 3 个。
- 取消没有 Fact/Workspace 锚点的“最近项目”候选，避免无关历史名称进入 Context。
- Workspace 命中为 `candidate_only` 弱参考；Workspace 与当天名称语义同时命中时才提升匹配基础。
- Context 明确要求当前 Facts 优先，项目名称冲突、目标切换或无法确认时忽略 Hint；历史成果、进度、日期和状态不得进入当天日报。
- 运行开关位于 `BuildRequest.EnableWorkspaceMemory`，系统个人日报开启，其他报告与个人 Agent 不受影响。

## 10. Phase 4：单正文分层渲染

已接受 Brief 由服务端确定性渲染，不再让 Pass 2 重新选材：

- 一个 Workstream 只有一个 Deliverable 时保持 `1. 完整成果` 的紧凑形式；
- 同一 Workstream 有多个 Deliverable 时输出 `1. Subject` 加二级 `- Result`；
- `summary` 继续保存 Workstream Title 列表，供内部快照和后续汇总使用，但用户日报正文只显示一份；
- 不恢复“工作概览 / 工作详情”，不新增标题区块；个人 Agent 和其他报告类型保持原流程。

## 11. 测试服真实验收

### 11.1 资源与环境

- migration 037、038 已应用；API 镜像 `sha256:cdbc983efd71e95683a515fd7fcabee5f552782028f762fba9540bc9a90ea8a2` 健康。
- System Report Skill：`100866/aida-report@1.1.31`。
- Project Memory Skill：`100866/aida-project-memory@project-memory-v5`，Resolver `project-memory-resolver/v5`。
- 测试时临时把 Nightly 窗口设为全天以触发指定 Job；验收后已恢复北京时间 02:00～06:00。

### 11.2 同源 A/B

使用测试用户 305 的 2026-07-31 完整生产样本切片：

| 组别 | Run | Memory Hint | 结果 |
| --- | --- | --- | --- |
| A：无 Workspace 历史关联 | `6b9d6c3c-e215-489d-901b-a29caa717a3f` | 0 | 成功；5 个一级主线，多个成果使用二级列表 |
| B1：写入 2026-07-30 Project Memory 后 | `dd21c173-5050-41ee-b5cc-dd716b053b2c` | 3 | 成功；归并为离线安全网关、IF-Knowledge、安全拦截三条主线 |
| B2：同 Context 重复 | `721e2887-a4de-4fdd-a462-26c34d3fa2d8` | 3 | 成功；主线与 B1 一致，表达存在正常模型差异 |

两个 B 组 Context 均为 95,416 字节，Hint 只包含项目名、别名、相关 Fact 引用和弱参考说明；没有历史进度、日期或成果句。Project Memory 2026-07-30 Job 成功产生 Snapshot `efc68072-42d9-4502-bf17-fb8653e6b346`，当前测试用户形成 8 条 Project↔Workspace Link，覆盖 4 个 Workspace。

### 11.3 无匹配降级

Run `ad63cff9-8bae-4a30-9abd-95beb662ed6d` 使用独立 `exe.dev` Workspace：Context 不包含 Project Memory Hint，仍成功生成一条项目加三条成果的分层正文。证明没有历史匹配时会自然退回当天 Facts，不阻断生成。

### 11.4 已知边界

- 现有客户端只提供整段 Session CWD / repository key，尚不支持同一 Session 内按时间切换 Workspace；本期不声称解决该场景。
- Workspace 是弱关联，同一 Workspace 可召回多个项目；Report Agent 仍需根据当天 Facts 选择或忽略。
- 分层正文恢复了信息量，但 Deliverable 文字长度仍由 Brief 质量决定；本期不增加会导致日报失败的新硬校验。

## 12. Phase 5：当天 Workspace 边界进入 Report Context

2026-08-06 的生产样本同时包含 AIDA 与 Knowledge Map 两个 Session。服务端已识别为两个 Workspace，但原 Context 只提供打平后的 Facts，Report Agent 因两边都出现 MCP，将 Knowledge Map 的授权工作错误命名为“AIDA 文档访问授权”。

本阶段新增匿名 `workspace_context`：

- 只提供 `workspace-001` 等当前 Run 内短引用及对应 `fact_refs`，不暴露 CWD、Git remote、数据库 Workspace ID 或哈希；
- 不同 `workspace_ref` 的 Facts 默认分开，相同技术词（例如 MCP）不能作为跨 Workspace 合并依据；
- 当天 Facts 明确指向同一业务项目，或一个带当天语义锚点的 Project Memory Hint 覆盖两组兼容 Facts 时，仍允许跨 Workspace 归并；
- 只有一个有效 Workspace 时不增加该 Context，避免无意义 Token 开销；加载失败时保持日报可生成，不把质量提示升级成生成阻断。

测试服使用同一组两个真实 Session 连续回归：

- Run `4a5d4ff0-0fb7-4431-87d3-a26c8d473789` 验证两个匿名 Workspace 能稳定隔离 AIDA 与 Knowledge Map，原始路径和持久化身份未进入 Context；
- 修复 Source Selection 复用问题：Workspace Evidence 会复用首次选择时保存的记录，读取时必须按 Session、Projection Revision 与 Slice Cursor 关联，不能错误要求 Evidence 属于当前 Selection；回归测试已覆盖；
- Report Skill 最终测试版本为 `100866/aida-report@1.1.40`，同一 Workspace 默认先形成一个 Workstream；只有当天 Facts 明确出现不同项目名时才拆分，未命名模块归入组内已有的平台或项目父级；
- Run `a6f6f539-80b6-42fb-93b9-e2df36baf65e` 证明跨 Workspace 隔离有效，但 MiniMax 仍把同一 AIDA Workspace 拆成“日报页面”和“客户端同步”两个主项；
- Run `71fc148a-c744-4dfe-af9d-ea304788f30b` 在更明确的 Skill 下仍被 MiniMax 拆成四个模块，并生成过多细节。说明继续堆叠提示词不能稳定解决父级命名；不能因此增加按 CWD 强制合并或更严格的失败型校验，以免误伤同一 Workspace 内真实存在多个项目的场景。

当前阶段可确认的是 Workspace 边界和 Evidence 复用机制有效；稳定父级项目名仍应优先依赖 Project Memory 的当天语义锚点。没有 Memory Hint 时，Workspace 只能用于防止跨项目误合并，不能被当成唯一项目归属。
