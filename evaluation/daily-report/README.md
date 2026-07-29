# 日报生成方案评测开发工具

该工具只在测试服顺序执行 2～3 个 Generation Variant，并导出冻结的 Evaluation Bundle。它不连接生产数据库、不调用 Reviewer，也不决定发布。

## 当前能力

- 校验 Dataset 与执行计划；
- 对相同 Case 顺序运行 2～3 个 Variant；
- 强制核对 Source Identity、Variant Hash 和 Pipeline Profile；
- 导出 Source Evidence、Digest、Context、Brief（存在时）、Generated Draft、运行指标和 Artifact Hash；
- 失败、超时 Run 也保留创建时的 Variant Manifest；
- Token 与成本尚未形成可靠的 Run 级归因时明确输出 `not_available`，不估算。
- 校验 Bundle Hash 和 Case/Variant 覆盖，计算 Brief 事实守恒等确定性指标；
- 生成匿名评审输入，并在 AI/Gold Review 完成后聚合 Scorecard 和三态结论。

## 使用

```bash
cd api
go run ./cmd/daily-report-eval validate \
  --plan ../evaluation/daily-report/examples/plan.example.json

export AIDA_EVAL_TOKEN='<测试账号 token>'
go run ./cmd/daily-report-eval run \
  --plan ../evaluation/daily-report/examples/plan.json \
  --base-url http://127.0.0.1:13000 \
  --environment test \
  --database-url "$DATABASE_URL" \
  --output ../evaluation/daily-report/bundles/<evaluation-id>
```

`--output` 必须是不存在的新目录。Token 只从指定环境变量读取，不写入 Plan 或 Bundle。具体固定 Case 尚未确定，示例 UUID 不可直接运行。

```bash
go run ./cmd/daily-report-eval verify --bundle <bundle> --output <workspace>/verification.json
go run ./cmd/daily-report-eval prepare-review --bundle <bundle> --output <workspace>
go run ./cmd/daily-report-eval aggregate --bundle <bundle> \
  --ai-reviews <workspace>/case-results.jsonl \
  --gold-reviews <workspace>/gold-reviews.jsonl \
  --output <workspace>/evaluation-result
```

## Bundle

Bundle 遵循 `schemas/bundle-manifest.schema.json`，目录按 `cases/<case-id>/runs/<run-id>` 保存。某阶段不存在时不创建空 Artifact；Manifest 中的阶段定义用于判断 `not_applicable`。
