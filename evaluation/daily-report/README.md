# 日报生成方案评测开发工具

该工具只对显式开启评测能力的隔离测试实例执行 2～3 个 Generation Variant，并导出可复核的 Evaluation Bundle。CLI 不连接数据库，不调用 Reviewer，也不决定发布。

## 安全边界

- Aida 服务必须同时配置 `AIDA_ENVIRONMENT=test`、`AIDA_EVALUATION_ENABLED=true` 和唯一的 `AIDA_EVALUATION_INSTANCE_ID`；
- `AIDA_BUILD_REVISION` 必须是可识别的构建版本，不能是 `not_available`；
- CLI 在创建任何 Run 前向每个 Case × Variant Runtime 获取服务端 Attestation；任一 Runtime 未通过时整批停止；
- 生产默认不注册 Source Freeze 和 Artifact Adapter 路由；
- 每个 Run 的 Source Identity 必须与运行前冻结的 Source Evidence 完全一致；
- `required/optional` Baseline 引用必须落在 Case 的上海自然日；跨日引用只允许标记为 `exclude`；
- Plan 显式指定模型时，实际 Variant Manifest 必须使用同一模型，否则 Exporter 与 Verifier 都拒绝该 Bundle；
- 同一 `variant_version` 在全部 Case/重复中必须对应唯一实际 Manifest Hash，不同 Variant 也不能共享同一实际 Manifest，避免把重复运行伪装成 A/B；
- Token 只从 Plan 指定的环境变量读取，不写入 Plan 或 Bundle；多账号 Dataset 必须按 `case_id` 完整映射凭证环境变量。

## 当前能力

- 冻结 Digest 前的脱敏 Source Evidence，并生成文件 Hash；
- 校验 Dataset、Source、Evidence Baseline、Production Pattern 和执行计划；
- 对相同 Case 顺序运行 2～3 个可独立配置 Runtime 的 Variant；
- 导出 Digest、Context、Brief（存在时）、Generated Draft、运行指标、Runtime Attestation 和 Artifact Hash；
- 校验 Bundle Hash、完整 Case/Variant/Repetition 覆盖及 First Bad Stage；
- 生成匿名评审输入，并在 AI Review 与人工确认的 Gold Review 完成后聚合 Scorecard；Review 的 Evidence 引用必须能回指冻结 Source 或 Evidence Baseline；
- Production 手写日报只进入结构/篇幅 Pattern 比较，不作为事实 GT；
- Token 与成本没有可靠 Run 级归因时明确输出 `not_available`。

## 使用

先在隔离测试实例冻结每个真实用户日的 Source：

```bash
cd api
export AIDA_EVAL_SOURCE_TOKEN='<测试账号 token>'
go run ./cmd/daily-report-eval freeze-source \
  --base-url http://127.0.0.1:18090 \
  --token-env AIDA_EVAL_SOURCE_TOKEN \
  --report-date 2026-07-29 \
  --slice-key '<session-slice-uuid>' \
  --output ../evaluation/daily-report/datasets/<version>/sources/<case-id>.json
```

把命令输出的文件 SHA-256、Source Identity 和结构化 Evidence Baseline 写入 Dataset Manifest。随后校验：

```bash
go run ./cmd/daily-report-eval validate \
  --plan ../evaluation/daily-report/examples/plan.example.json
```

示例文件可通过校验，但其中 UUID、Agent 和 Runtime 仅用于说明格式，不可直接执行真实评测。

系统默认 Agent 可能由服务端固定模型并忽略单次 `model_id`。不要用请求标签猜测实际模型；若 Runtime 不支持覆盖，应省略 `model_id`，以 Variant Manifest 为准。工具会拒绝任何显式请求与实际模型不一致的 Run。

每个 Variant 的 Runtime 在 Plan 中独立声明。单账号 Dataset 可使用一个 `token_env`；多账号 Dataset 使用 `case_token_envs`，并且必须恰好覆盖全部 Case。Runner 会在创建第一个 Run 前确认每个 Case × Variant 的 Token 非空、可用于 Runtime Attestation；该预检不证明 Token 对 Source 的归属，错账号凭证会在该 Case 开始读取用户隔离 Source 时被拒绝，整批不会生成有效 Bundle：

```bash
export AIDA_EVAL_CASE_001_BASELINE_TOKEN='<case-001 在基线 Runtime 的测试账号 token>'
export AIDA_EVAL_CASE_001_CANDIDATE_TOKEN='<case-001 在候选 Runtime 的测试账号 token>'
go run ./cmd/daily-report-eval run \
  --plan ../evaluation/daily-report/examples/plan.json \
  --output ../evaluation/daily-report/bundles/<evaluation-id>
```

`--output` 必须是不存在的新目录。失败时保留不完整目录供排查，但没有 `manifest.json` 的目录不是有效 Bundle。

```bash
go run ./cmd/daily-report-eval verify --bundle <bundle> --output <workspace>/verification.json
go run ./cmd/daily-report-eval prepare-review --bundle <bundle> --output <workspace>
go run ./cmd/daily-report-eval aggregate --bundle <bundle> \
  --ai-reviews <workspace>/case-results.jsonl \
  --gold-reviews <workspace>/gold-reviews.jsonl \
  --output <workspace>/evaluation-result
```

## Bundle

Bundle 根目录保存 Dataset、执行计划及其引用的 Source/Pattern 文件；运行产物位于 `cases/<case-id>/runs/<run-id>`。某阶段不存在时不创建空 Artifact，Run Manifest 的实际阶段决定 `not_applicable` 和 First Bad Stage 的允许值。
