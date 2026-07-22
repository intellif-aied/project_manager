# GC-S09：Git Remote 只读可达性检查

> 标注状态：`reviewed`，仅作历史校准样本，不进入 Digest 修改计划

> 样本类型：短 Session、单轮用户问题、只读查询、无文件修改、外部可达性验证先失败后成功

> 用途：校准 Atomic Work Unit 边界、镜像用户消息去重、重要查询/验证证据召回和 Agent claim 支持等级

> 封板校正：`git remote`、`git status`、`git ls-remote` 属于本任务的执行事实，但通常不是日报成果证据。此前按四条命令计算 `25%` 证据召回率、按两次远端检查计算 `0%` 关键验证召回率，不能用于评价 Digest 的日报质量，也不构成扩充命令白名单的依据。

## 1. 样本身份

| 字段 | 值 |
| --- | --- |
| Session ID | `019f4570-fd80-7ec1-ae1d-e1a469154d69` |
| 本机只读路径 | `~/.codex/sessions/2026/07/09/rollout-2026-07-09T05-54-20-019f4570-fd80-7ec1-ae1d-e1a469154d69.jsonl` |
| 原文 SHA-256 | `1c3c66c0af2102887b7476abf7498bb95610f6591ca4ee9afe1e09e760e5f80e` |
| 原文大小 | `148,806` bytes |
| JSONL 行/事件数 | `30` |
| 非法事件 | `0` |
| 物理尾部 | 以换行结尾，无残行 |
| 当前 Digest 版本 | `session-digest/v2.9.0` |

文档不保留样本中的内网 remote、完整 commit hash 和完整 `ls-remote` 输出；这些值不影响本 Case 的结构化期望。

## 2. 逐段读取记录

### 段 A：传输与系统上下文

| 行 | cursor | 类型 | 处理 |
| ---: | --- | --- | --- |
| 1 | `[0,22181)` | `session_meta` | 元数据，不建 Work Unit |
| 2 | `[22181,22414)` | `event_msg.task_started` | 控制事件，忽略 |
| 3 | `[22414,57238)` | developer message | 系统注入，忽略 |
| 4 | `[57238,92598)` | injected user message | 注入的工作区/指令上下文，忽略 |
| 5 | `[92598,127833)` | `world_state` | 运行状态，忽略 |
| 6 | `[127833,130903)` | `turn_context` | 运行上下文，忽略 |

### 段 B：有效用户目标和镜像事件

| 行 | cursor | 类型 | 内容/处理 |
| ---: | --- | --- | --- |
| 7 | `[130903,131190)` | `response_item.message(role=user)` | 有效目标：询问当前 Git remote 是否可访问；创建 WU-01 |
| 8 | `[131190,131386)` | `event_msg.user_message` | 行 7 的镜像消息；不创建第二个 Unit |
| 9 | `[131386,134354)` | reasoning | 忽略 |
| 10-11 | `[134354,135102)` | Agent commentary 双写 | 保留时序用途，不作最终 claim |

### 段 C：remote 配置和本地分支查询

| 行 | cursor | 类型 | 客观事实 |
| ---: | --- | --- | --- |
| 12,14 | `[135102,136573)` | `git remote -v` call/result | exit `0`；读取到已配置 remote，不证明网络可达 |
| 13,15 | `[135578,136963)` | `git status --short --branch` call/result | exit `0`；确认本地分支跟踪状态，不证明网络可达 |
| 16 | `[136963,137673)` | token_count | 忽略 |

### 段 D：第一次真实可达性检查

| 行 | cursor | 类型 | 客观事实 |
| ---: | --- | --- | --- |
| 17 | `[137673,139341)` | reasoning | 忽略 |
| 18-19 | `[139341,140139)` | Agent commentary 双写 | 声明将做只读连通性检查；不是验证结果 |
| 20,21 | `[140139,141227)` | `git ls-remote --heads origin` call/result | exit `128`；沙箱禁止 socket，本 attempt 失败；不能由此判定远端不可达 |
| 22 | `[141227,141938)` | token_count | 忽略 |
| 23 | `[141938,143838)` | reasoning | 忽略 |

### 段 E：授权后重试和 final answer

| 行 | cursor | 类型 | 客观事实 |
| ---: | --- | --- | --- |
| 24,25 | `[143838,145632)` | 授权后 `git ls-remote --heads origin` call/result | exit `0`；返回远端 refs，已证明当时可以只读访问 remote |
| 26 | `[145632,146344)` | token_count | 忽略 |
| 27-28 | `[146344,147512)` | final answer 双写 | Agent claim：可访问 remote；应由行 24-25 的成功证据支持 |
| 29 | `[147512,148224)` | token_count | 忽略 |
| 30 | `[148224,148806)` | task_complete | final answer 镜像；不重复生成 claim |

## 3. Golden Atomic Work Unit

```yaml
case_id: GC-S09
annotation_status: reviewed
work_unit:
  id: WU-01
  source_event:
    line: 7
    cursor: [130903, 131190]
    event_type: response_item.message
  goal: 询问当前 Git remote 是否可访问
  mirrored_events:
    - line: 8
      cursor: [131190, 131386]
facts:
  changes: []
  command_attempts:
    - kind: repository_configuration_read
      call_line: 12
      result_line: 14
      status: passed
      exit_code: 0
      supports_goal: partial
    - kind: repository_local_status_read
      call_line: 13
      result_line: 15
      status: passed
      exit_code: 0
      supports_goal: no
    - kind: repository_remote_reachability
      call_line: 20
      result_line: 21
      status: failed
      exit_code: 128
      failure_scope: sandbox_network_restriction
      supports_goal: inconclusive
    - kind: repository_remote_reachability
      call_line: 24
      result_line: 25
      status: passed
      exit_code: 0
      supports_goal: yes
  agent_claims:
    - source_lines: [27, 28, 30]
      canonical_count: 1
      state: complete
      support: supported_by_remote_read_check
  user_feedback: []
objective_state:
  change_state: no_change_evidence
  validation_state: mixed
  final_validation_state: passed
  agent_claim_state: complete
  user_feedback_state: no_feedback
  evidence_level: validation_evidence
  business_completion: not_labeled_by_digest_golden
unpaired_calls: []
unpaired_results: []
```

`validation_state=mixed` 保留首次环境限制失败和第二次授权后成功；`final_validation_state=passed` 表示最后外部只读验证成功。这两个维度不应被一个 `completed` 替代。

## 4. 当前 Digest v2.9 回放结果

### 4.1 实际输出摘要

| 字段 | 当前输出 |
| --- | --- |
| Work Unit | `1` |
| goal | 正确 |
| status | `unknown` |
| evidence grade | `C` |
| result statement | `1`，来源为 `agent_claim` |
| agent claim support | `unsupported` |
| evidence | 只有 `git status` exit `0`，kind=`repository` |
| changes | `0` |
| validations | `0` |
| unresolved | `0` |
| verified result | `0` |

### 4.2 结构对比结论

**已正确：**

- 系统注入未建 Work Unit；
- 行 7/8 镜像用户消息只建了一个 Unit；
- 无文件修改，没有伪造 change；
- final answer 双写与 task_complete 被合并为一个 claim；
- 没有把只读查询直接判成 `completed`。

**结构差异（不直接等于日报质量缺陷）：**

1. `api/internal/sessiondigestv2/reducer.go:57` 的 `classifyCommand` 不识别 `git remote -v`，因此 remote 配置事实未进 Digest；
2. 同一函数不识别 `git ls-remote`，因此第一次 exit `128` 和第二次 exit `0` 均未进 Digest；
3. 两次直接支持用户问题的 remote check 都没有进入 Digest，但普通执行命令不应自动计入日报证据召回分母；
4. 因为这些执行事实没有进入 Digest，final claim 被标记为 `unsupported`；这是一种保守输出，不等同于日报错误；
5. result statement 保留了内部 remote 和 commit 细节。这些不是虚假事实，但属于 Report Agent 可见投影的内部细节风险，不应与证据召回问题混为一类。

现有 `api/internal/sessiondigestv2/extractor_test.go:188` 覆盖“没有工具证据时保留 final answer 为 `agent_claim`”。本 Case 的实际输出符合该保守契约，不能据此要求 Digest 扩充 remote 命令分类。

## 5. 首个 Case 指标

| 指标 | 结果 | 说明 |
| --- | ---: | --- |
| Work Unit 边界精确率 | `1/1 = 100%` | 没有把注入或镜像消息建成额外 Unit |
| Work Unit 边界召回率 | `1/1 = 100%` | 真实用户目标已保留 |
| 命令结构保留率 | `1/4 = 25%` | 只描述四次执行中有几次进入 Digest，不作为日报质量指标 |
| remote check 结构保留率 | `0/2 = 0%` | 只描述两次调用均未进入 Digest，不作为日报证据召回率 |
| 输出 evidence 事实精确率 | `1/1 = 100%` | 已输出的 `git status` 成功事实正确，但对主目标支持弱 |
| 事实错误数 | `0` | 未把失败写成成功；主要问题是遗漏 |
| 原文体积 | `148,806` bytes |  |
| Digest 体积 | `2,815` bytes |  |
| 噪声压缩率 | `98.108%` | Digest/raw = `1.892%` |
| outcome coverage | `1/1`，complete | 只说明 Work Unit 成果条目已表示，不证明证据完整 |
| 幂等性（当前离线输出） | 通过 | 连续两次评测输出 SHA-256 均为 `673adbfbc026bd9b0d585fe715619b36e562480255299d4332c4ef045deba394` |
| 单次离线容量 | `0.02s`，峰值 RSS `10,292 KiB` | 短样本冒烟基线，不代表大 Session 门槛 |
| 跨批次一致性 | 待执行 | 需要使用真实 Slice absolute cursor 的 cut-point harness，不用重置 cursor 的两个独立文件假装端到端结果 |

## 6. 本 Case 的最终结论

本 Case 没有证明 Digest 存在需要修复的日报证据缺口：

- Work Unit 边界和镜像去重正确；
- 没有把只读操作误判为文件修改或业务完成；
- 无法形成业务完成事实时保守输出 `unknown`；
- 未保留 `git remote`、`git ls-remote` 不应直接算作日报证据遗漏；
- 已保留的 `git status` 对多数日报价值有限，但单个样本不足以授权新增过滤规则；
- 命令是否值得写入日报，应由 Report Agent/Skill 结合用户目标判断，Digest 不继续细化语义相关性分类。

因此不修改 `classifyCommand`，不继续为本 Case 寻找同类样本。本 Case 只保留为边界、去重、保守状态和幂等性的正向校准记录。

## 7. 复核记录

- 已逐行建立 30 个事件的 cursor/type 索引；
- 已二次回查用户消息、4 个 call/result、final answer 和 task_complete；
- 已用当前 `main` 构建的 `session-digest-eval` 回放原文；
- 已连续回放两次并比较完整离线输出 hash；
- 尚未将该 Case 冻结为自动化测试 fixture，尚未修改 Digest 代码。
