# Report Brief 空参数边界修复生产发布

## 1. 发布元数据

| 项目 | 内容 |
| --- | --- |
| 发布编号 | `20260731-02-empty-report-brief-guard-prod` |
| 目标环境 | Aida 生产服，经 `192.168.14.157` 跳板进入 `192.168.14.182:/home/luoxian/aida` |
| 变更目标 | 阻止 `write_report_brief` 的空参数进入完整语义校验并消耗 Brief 纠错次数 |
| 源码提交 | `3971350`（`fix: reject empty report briefs before semantic validation`） |
| API 镜像 | `20260731-3971350-empty-brief-guard` |
| Registry digest | `sha256:3b0e3a8b61e3d814bd276f0620d3bf707b377a1eda6ce72cf760af0f0de45ce7` |
| 执行时间 | 2026-07-31 08:02—08:04 UTC（16:02—16:04 Asia/Shanghai） |
| 最终状态 | 已发布，生产边界回归通过 |

## 2. 完整范围清单

| 范围 | 本次状态 | 说明 |
| --- | --- | --- |
| API/后台 | 发布 | 只增加 Report Brief 空参数边界保护及回归测试 |
| Web | 仅核对 | 镜像和容器保持不变 |
| PostgreSQL migration | 不涉及 | 无 migration 文件和数据库结构变化 |
| 历史数据回填 | 不涉及 | 未执行回填 |
| 数据清理/删除 | 不涉及 | 未执行数据库写入、清理、删除、`DROP` 或 `TRUNCATE` |
| Report Skill | 不涉及 | 继续使用生产现有版本 |
| MCP/默认 Agent | 仅核对 | MCP 地址、工具定义、默认 Agent 和 SkillRef 均不变 |
| CLI/安装包 | 不涉及 | 无 CLI 变更 |
| MinIO/对象存储 | 不涉及 | 容器和对象均未操作 |
| Digest/报告链路 | 仅核对 | Context、Brief 语义规则和 Result 规则不变 |
| 文档/配置 | 发布 | 仅更新 API 镜像标签并记录本清单 |

## 3. 根因与修复

`{}` 是合法 JSON，Agent 平台没有根据 MCP Schema 的 `required` 声明阻止调用。API Handler 又把空参数兼容解析为空 Draft，导致它进入完整事实语义校验，产生大量 `fact_ref is not accounted for` 错误并消耗一次 Brief 纠错机会。

本次在 Handler 参数边界增加空 Draft 判断：

- `{}`、空白 `brief_json` 和字符串形式的空对象 `"{}"` 立即返回 `REPORT_BRIEF_INVALID: brief_json is required`；
- 不调用 `Accept` 或 `RejectInvalid`，不消耗语义纠错次数；
- 非空旧格式字段继续进入原有语义校验；
- malformed `brief_json` 仍按原机制计入纠错，事实完整性和质量规则未放宽。

涉及文件：

- `api/handler/report_mcp_brief.go`
- `api/handler/report_run_identity_test.go`

## 4. 测试证据

修复前最小回归稳定失败：

```text
go test ./handler -run TestEmptyBriefArgumentsStayAtArgumentBoundary -count=1 -v
--- FAIL: TestEmptyBriefArgumentsStayAtArgumentBoundary
error = "REPORT_BRIEF_INVALID: report brief is invalid", want brief_json requirement
```

修复后：

- 空参数、空白 `brief_json`、`"{}"` 和 malformed JSON 相关测试连续执行 2 次全部通过；
- 旧格式非空 Brief 兼容测试通过；
- `cd api && go test ./...` 全部通过；
- `git diff --check` 通过。

## 5. 生产部署

发布前：

- API 标签：`20260731-bb1614d-report-project-outcomes`
- API 镜像 digest：`sha256:943fa1950245efc501039d9b6afb14176d04a9c32211c605aeb844ca5f21ddc1`
- API 容器：`d5cddabbc4cb685eb53e3f89953721e0b775d90a9d9c1f042dbf8683c4458f41`

部署动作：

1. 构建并推送不可变 API 镜像；
2. 只把生产 `.env` 的 `API_IMAGE_TAG` 更新为新标签；
3. 执行 `docker compose pull api` 和 `docker compose up -d --no-deps api`；
4. 未替换 Web、DB、MinIO，未修改 Skill/MCP/Agent 配置。

本次按用户明确指令不新增 PostgreSQL 备份。原因是变更仅为无状态 API 参数边界保护，不涉及 migration、业务数据写入、回填或清理；回退只需恢复上一不可变 API 镜像。此前完整生产备份未改动。

## 6. 发布后验收

- 新 API 容器：`3ff12a879da4fea9688b504ec5246de558582a574de1f5c3c0755d6ecd483137`；
- 新 API 实际 RepoDigest 与 Registry 一致；
- 容器内 `/health` 返回 `{"status":"ok"}`，生产入口返回 HTTP 200；
- Web、DB、MinIO 容器 ID 与启动时间保持不变；
- 新 API 启动后的 `panic`、`fatal`、`REPORT_BRIEF_RETRY_EXHAUSTED` 和 HTTP 5xx 日志匹配数为 0；
- 使用生产 UID 198 的既有 Run 调用空参数，返回：

```text
REPORT_BRIEF_INVALID: brief_json is required; submit one non-empty serialized Report Brief JSON object
```

- 调用后该 Run 的 `brief_invalid_attempts` 仍为 `2`、`result_invalid_attempts` 仍为 `0`，确认空参数未进入语义校验、未增加纠错次数。

## 7. 回退方案

1. 将生产 `API_IMAGE_TAG` 恢复为 `20260731-bb1614d-report-project-outcomes`；
2. 仅执行 `docker compose pull api` 和 `docker compose up -d --no-deps api`；
3. 核对 `/health`、生产入口和报告 MCP；
4. Web、DB、MinIO、Skill、MCP 配置和数据库结构均无需处理。

## 8. 最终判定

```text
发布范围完整：是
代码与回归测试通过：是
生产边界验证通过：是
数据库备份：按用户明确指令不新增
阻断项：0
最终状态：已发布
```
