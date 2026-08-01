# 选择性 Brief 与自动 Fact 归档测试发布

## 发布信息

| 字段 | 内容 |
| --- | --- |
| 日期 | 2026-07-31 |
| 环境 | Aida 测试服（192.168.14.157） |
| 源码基线 | `b63f0230b8e60fa5babf6e43c5ba51fa1506be7c`（`main`）+ 本记录所列未提交改动 |
| 改动范围 | 系统默认个人日报的项目归并、成果选择、未选 Fact 自动归档和 Brief JSON 修正反馈 |
| 生产环境 | 未涉及 |

## 发布清单

| 组件 | 是否发布 | 版本或结果 | 说明 |
| --- | --- | --- | --- |
| API | 是 | `sha256:ad293648687ab47fdb9551f12c6a5a35ccd23711b0615dabba03e7afed16c54a` | 只重建并替换 `api` |
| System Report Skill | 是 | `100866/aida-report@1.1.18` | SkillID `50ea4740-b7ef-4355-90c2-fa450f162766`，文件 SHA256 `07d70064e62280fcfcb320f5dd0d1238a8579ecb0c1c648d90d32654d4704651` |
| Report MCP | 否 | 继续使用 `aida-report-mcp@report-v1` | URL、鉴权和 tool interface 未变化 |
| Web / CLI | 否 | 未部署 | 无对应代码变更 |
| DB / MinIO | 否 | 保持运行 | 无 migration、未重启 |

## 实际改动

1. 正常 Report Brief 不再要求 Agent 枚举所有未选 Fact；服务端以内部原因 `not_selected` 确定性归档。
2. 存储 Brief 保留全部 Fact 归属，返回 Final Report 的 Accepted Brief 隐藏自动归档项；空日报仍要求逐条显式排除。
3. System Report Skill 先按命名项目归并，命名项目 Subject 不追加组件或活动后缀；每项目最多选择三个读者成果。
4. 非法 `brief_json` 返回标准 JSON 解析原因，不回显正文、不自动猜测修复。
5. Digest、Projection、Context schema、个人 Agent、其他报告类型、前端和数据库均未修改。

## 测试与验证

- `cd api && go test ./... -count=1`：通过。
- Report Brief、System Report Skill、Report Evaluation 和 MCP Handler 定向测试：通过。
- API `/health`：返回 `{"status":"ok"}`。
- 运行配置：`MANAGED_AGENT_REPORT_SKILL_OWNER=100866`，`MANAGED_AGENT_REPORT_SKILL_VERSION=1.1.18`。
- 公共 Registry：目标 Skill 唯一、未归档，回读 `SKILL.md` 与发布文件 SHA256 一致。
- 新 Run 实际加载 `aida-report@1.1.18` 与 `aida-report-mcp`。

## 真实流程回归

### 5-Session 样本

- 用户：310；日期：2026-07-28。
- Run：`237aec05-478e-4683-b781-676fe21b7bf1`。
- 结果：succeeded；Brief 修正 0 次，Result 修正 0 次。
- Brief：1 个 `aihub-frontend` 项目、3 个成果、20 个 `not_selected`。
- 验证：同项目的开发流程、规范校验和 UI 无障碍整改不再拆成多个 Workstream。

### 27-Session 样本

- 用户：305；日期：2026-07-28。
- Run：`64f25317-b2f0-43b4-ba05-0e9c1cf51439`。
- 结果：succeeded；Brief 修正 1 次，Result 修正 0 次。
- Brief：5 个彼此独立项目，每项目不超过 3 个成果，164 个 `not_selected`。
- 验证：长 Context 未发生 Fact 穷举错误或超时，概览保留项目名，最终正文未逐条展开次要 Fact。

## 中间不可变 Skill

测试过程中按 Registry 不可变规则发布了 `1.1.14` 至 `1.1.17` 用于逐轮同源对比；它们未覆盖、未删除，最终运行配置只引用 `1.1.18`。上一份发布前稳定版本仍为 `1.1.13`。

## 容器影响

- 最终 API 容器：`a0ece16217c15803ef621b998ac04664763ee5f5b840132becdde2c8d10c8d6e`。
- Web 自 2026-07-29 持续运行，DB 自 2026-07-22 持续运行，MinIO 自 2026-07-21 持续运行；本次均未重启。
- 未执行数据库备份，因为没有数据库结构或数据迁移。

## 回退

1. 将测试服 `MANAGED_AGENT_REPORT_SKILL_VERSION` 恢复为 `1.1.13`；
2. 将 API 恢复为发布前镜像 `sha256:b197e3f522b58b9a99eef9326253575eb480db11e6eb400a5514263052a4d568`；
3. 只重建或替换 API，检查 `/health`、默认 Agent SkillRef 和真实 Session 加载版本；
4. Web、DB、MinIO 无需处理。

## 仓库状态

- 本记录生成时改动尚未提交、未推送；
- 未合并分支，未涉及生产发布。
