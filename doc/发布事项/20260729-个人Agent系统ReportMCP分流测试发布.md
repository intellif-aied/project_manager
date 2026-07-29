# 个人 Agent 与系统 Report MCP 分流测试发布

## 发布信息

| 字段 | 内容 |
| --- | --- |
| 日期 | 2026-07-29 |
| 环境 | Aida 测试服（192.168.14.157） |
| 源码版本 | `main@9f77148` 加当前未提交的个人 Agent/系统 Report MCP 分流改动 |
| 改动范围 | 个人 Agent 保留个人 Prompt、Skill 和 MCP；系统仅注入绑定 Report Run 的 Report MCP；个人流程绕过 Report Brief 门禁 |
| 数据库 | 无 migration、无数据变更、未备份 |
| 前端 | 未改动、未部署 |
| 生产环境 | 未涉及 |

## 部署组件

| 组件 | 镜像 | 结果 |
| --- | --- | --- |
| API | `sha256:2168d0f3c3c05cd438f93a089c1874143c5672506c7630fe46a8de24112ef756` | 已更新，容器运行正常 |

## 构建与检查

- `cd api && go test ./...`：通过。
- `docker compose up -d --build --no-deps api`：通过。
- 容器运行产物已确认包含 `personal-report` 和 `report_agent_source` 分流标识。
- 工作区另有 `api/cmd/team-session-reassign` 协作者改动；该独立命令不在 API Dockerfile 的构建及运行产物列表中，未回退、未提交。

## 发布后检查

- `docker compose ps api`：API 容器运行正常。
- `http://192.168.14.157:18090/health` 返回 `{"status":"ok"}`。
- 仅替换 API，未重启 db、minio、web，未发布 CLI。
- 本次未提交、未推送。

## 回滚

- 发布前 API 镜像已保留为 `project_manager-api:rollback-20260729-personal-agent-mcp`。
- 回退镜像 ID：`sha256:31bdf4adcd7699900a9c8d8085ca7f163848f1fcedb441d9dd62074acf2aa234`。
- 如需回退，将该镜像重新标记为 `project_manager-api:latest` 后执行 `docker compose up -d --no-deps --force-recreate api`。
