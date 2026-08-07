# Report Compiler 单次提案测试发布

> 日期：2026-08-06  
> 环境：AIDA 测试服（192.168.14.157）  
> 源码：`main@358005c` 加本记录所列未提交改动

## 部署范围

| 组件 | 结果 | 说明 |
| --- | --- | --- |
| API | 已部署 | 镜像 `sha256:75ea19f92b768d136a315abc9d641972046db1fe722e06dbf8c59f8b0f5c645a` |
| System Report Skill | 已发布并切换 | `100866/aida-report@1.1.42`，Skill ID `6cd6a887-9ee0-48e5-8ae6-7b5ae4cacdb6` |
| Report MCP | 接口升级 | managed personal daily 只暴露 Context、结构化 Result、Failure 三个工具 |
| Web / CLI / DB / MinIO | 未涉及 | 未部署、未重启、无新 migration、无备份 |

## 变更行为

- System personal daily 从 Brief 反复校验改为一次结构化 Draft 提交。
- 服务端完成 Draft 修复、Brief 保存、确定性渲染；质量问题不再让整份日报失败。
- Agent Session 完成但未写回时，状态同步器使用冻结 Context 自动补写。
- Personal Agent、周报、团队报告和部门报告保持原流程。

## 验证

- `cd api && go test ./...`：通过。
- `git diff --check`：通过。
- API 容器运行，`/health` 返回 `{"status":"ok"}`。
- Registry 回读 `SKILL.md` 7,895 字节，SHA256 为 `fdae9376120c301fb0ca6aa924a4efb3af02555442ae75e65e792ac2306d6350`，与 API 生成文件一致。
- 实际 Agent 已绑定 `100866/aida-report@1.1.42`；抽查 Session 中 `write_report_brief` 调用为 0。
- Project Association v2：12/12 Run 成功；8 accepted、3 repaired、1 fallback；项目关联门禁 5/12，通过结果不能作为生产质量批准。
- 结果位于 `/home/intellif/evaluation/project-association-regression-v2/` 下的 `candidate-1.1.42.json` 与 `replay-1.1.42/`。

## 回退

1. 将测试服 `MANAGED_AGENT_REPORT_SKILL_VERSION` 恢复为 `1.1.40`；
2. 恢复上一个 API 镜像并仅替换 API；
3. Web、DB、MinIO 和历史日报不处理。

## 仓库状态

- 本次代码与文档尚未提交、尚未推送；
- 当前仍为 `main`，未涉及生产环境。
