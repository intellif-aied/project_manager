# AIDA-BUG-20260722-012：OpenClaw 上传后不显示在 Token Session 列表

> 优先级：P0
>
> 状态：测试服已部署，待人工验收

## 问题

OpenClaw Session 上传成功后，`/api/v1/token-analytics/sessions` 不返回该 Session。当前查询以 Token rollup 为列表入口，而 OpenClaw 的 `usage_capability=unavailable`，没有 usage component 和 rollup，因此整行被排除。

## 期望行为

- 已上传的 OpenClaw Session 仍出现在 Token Session 列表；
- 返回 `usage_status=unavailable`；
- Token 和成本字段返回 `null`，前端显示“Token 暂不支持”，不伪造 `0 Token`；
- Summary、趋势和排名仍只统计真实 usage；
- 不新增数据表，不开发 OpenClaw Token 解析。

## 验收

1. 同一用户有一个已统计 Session 和一个 OpenClaw unavailable Session 时，列表返回两行；
2. OpenClaw 行的 Token/成本为 `null`，状态为 `unavailable`；
3. Summary 的 Session 数和 Token 总量不包含该行；
4. 日期、账号范围、搜索和分页仍生效；
5. 无数据库迁移。

## 测试服验证

- 2026-07-22 从 `main@ed1c148` 隔离工作树构建并部署 API 和 Web；
- 未使用或修改 `fea.0.0.2`；
- 真实账号新建查询快照后，第一页已返回 `openclaw-cc2547d867b424f73a6ce1691100aa90`；
- 该行为 `usage_status=unavailable`，Token 和成本字段均为 `null`；
- DB、MinIO 容器未重启，未新增 migration。
