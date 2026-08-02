# Report Brief 主标题驱动 Summary 测试发布

## 发布信息

| 字段 | 内容 |
| --- | --- |
| 日期 | 2026-08-01 |
| 环境 | Aida 测试服（192.168.14.157） |
| 源码基线 | `b40691e`（`main`）+ 本记录所列未提交改动 |
| 改动范围 | System `personal_daily` 的 Brief 主标题与最终 Summary 收口 |
| 生产环境 | 未涉及 |

## 发布清单

| 组件 | 是否发布 | 版本或结果 | 说明 |
| --- | --- | --- | --- |
| API | 是 | `sha256:ea53963a5f79661236cb1010f4d9ca7c8b85a1de187b272da95ed13966c9e287` | 只重建并替换 `api` |
| System Report Skill | 是 | `100866/aida-report@1.1.23` | SkillID `4327b653-fb60-489c-ab79-c25962cce96f`；Registry 正文 SHA256 `53e534b9540b809b9f490fa44bf8fad8f2ba7b79234a6eb566f4441db12654e0` |
| Report MCP | 否 | `aida-report-mcp@report-v1` | URL、鉴权与 Tool Interface 未变化 |
| Web / CLI | 否 | 未部署 | 无对应代码变更 |
| DB / MinIO | 否 | 保持运行 | 无 migration、未重启、未备份 |

## 主要行为

1. 带 Subject 的已接受 Brief 由服务端按 Workstream 顺序确定性生成 Summary，每项只使用对应 Title。
2. Agent 提交的 Summary 不再参与最终选材；Deliverables 继续用于工作详情。
3. System Skill 在 Pass 1 将 Title 写成完整读者主标题，Pass 2 只复制 Title，不再从 Deliverables 二次挑选概览内容。
4. 无工作、旧无 Subject Brief、降级写回、个人 Agent 与其他报告类型保持原行为。

## 验证结果

- `cd api && go test ./... -count=1`：通过。
- Skill 渲染后 90 行、10,789 字节，低于既有 130 行和 11,000 字节限制。
- `1.1.23` 从 `1.1.22` owner-qualified derive；Registry 回读正文与 API 生成文件逐字一致。
- 测试 API `/health` 返回 `{"status":"ok"}`，运行配置为 `100866/aida-report@1.1.23`。
- 真实 Run `38a02efc-e1a7-4d97-8a72-40a5df0db4d1`、Agent Session `449d9468-30b9-4725-8c00-de979c88fabe` 成功写回 Report `014475a3-1d4a-41ad-96ba-9bfdb550c3e8`。
- 真实 Run 的两条工作概览与已接受 Brief 的两个 Title 逐字一致；详细结果只出现在对应工作详情。
- Knowledge Map 回归用例确认“儿童睡前动画”支撑场景可留在详情，但不会进入工作概览。
- 生产异常 Agent Session `c84ed869-f17d-47cb-956f-766e3a9c192c` 的 8 个来源 Session 已只读还原并通过测试账号 305 正常上传；全部 Projection active、Slice ready。
- 8 Session 真实回归 Run `fd958b59-7c03-4d91-b819-4c64dc7304ff` succeeded，Report `0f145647-bff9-4f87-b01b-c0872e5e4f6b`；儿童睡前动画只在 `IF-Knowledge` 详情出现，概览未出现。
- 该回归使用完整 Session，测试 Context 798,500 字节；生产原 Run 只选了三个长 Session 的尾部 Slice，Context 230,960 字节，因此记录为事实覆盖回归，不宣称 Context 字节级一致。

## 回退

1. 将测试服 `MANAGED_AGENT_REPORT_SKILL_VERSION` 恢复为 `1.1.22`；
2. 恢复上一 API 镜像 `sha256:ef9b33feefd0f2f4dc4f39ef4e056d0da6a3178b667fe12edca0ccb976dde4fc`；
3. 只替换 API 并核对 `/health` 与默认 Agent SkillRef；Web、DB、MinIO 不处理。

## 仓库状态

- 本记录生成时改动尚未提交、未推送；
- 未切换或合并分支，未涉及生产发布。
