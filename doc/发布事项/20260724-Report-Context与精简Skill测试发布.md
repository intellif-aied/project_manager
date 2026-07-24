# 20260724 Report Context 与精简 Skill 测试发布

## 1. 发布元数据

| 字段 | 内容 |
| --- | --- |
| 发布编号 | `20260724-04-report-context-concise-skill` |
| 目标环境 | 14.157 测试服 |
| Git 版本 | `main@815938c` |
| API 镜像 | `sha256:44347c49fab97e189d251cc8f84286c934063ec44ab330cf3a3c929ee1bcaf7e` |
| Report Skill | `100866/aida-report@1.0.50` |
| Skill ID | `75a1a329-c5b2-4d4a-b9b9-9e7f263763d0` |
| Registry SHA256 | `5e795b2bbc4fb4037cf35004007840f85ad00f976df38e6bbbd6e1012bd79ecd` |
| SKILL.md SHA256 | `0c06a74ee84e7ed9aa15c4f71cd8ab4c15f6dcaada6dbeeac2c2264205f21222` |
| MCP | `aida-report-mcp@report-v1`，未变更 |

## 2. 发布范围

- API：发布 Session+Digest Category 首末结果 Projection、历史冻结 Context 兼容、内部 `source_ref` 校验和 Git 轨迹剥离。
- Report Skill：从当前 `1.0.49` 派生不可变 `1.0.50`，正文从约 9,900 字符收敛到约 4,000 字符。
- Web、CLI、数据库迁移、Digest Job、Worker、模型、MCP 版本：不涉及。
- 生产环境：不涉及。

## 3. 自动化验证

- `cd api && go test ./...`：通过。
- `python3 -m py_compile scripts/test_default_report_assets.py`：通过。
- `git diff --check`：通过。
- Projection 覆盖：新来源归属、未知来源拒绝、混合新旧来源拒绝、历史无来源保持全部结果、同 Session 同类别首末保留、跨类别保留、未解决项保留、纯 Git 删除、混合业务结果剥离 Git 尾句。
- Registry 下载的 `SKILL.md` 与 API 生成文件逐字节一致。

## 4. 真实链路结果

### 4.1 仅收敛 Context

- Run：`643d185f-bf8b-4da1-85d8-62a29d6c0bc6`。
- Agent Session：`2618bf71-cc21-43be-9206-a6de97d8f565`。
- Context：22,596 bytes，85 条事实，事实正文 5,892 字符；MCP 工具正文 16,807 字符。
- 旧 Skill 正文 9,915 字符；模型输入 31,575 Token；10 分钟超时，未写回。
- 结论：372 条完整 Agent 回复造成的 Context 膨胀已消除，但旧 Skill 与固定模型长尾仍未闭环。

### 4.2 同 Context 加载精简 Skill

- Run：`31808223-26f5-403b-b4bf-87d995816467`。
- Agent Session：`f8e71b95-b062-4844-af07-70af1d154cf3`。
- 实际 Skill 正文：3,901 字符；Skill、Context、`write_report_result` 各调用一次，无重复询问。
- 写回调用时模型输入 30,420 Token；Agent 从 started 到 finished 约 401 秒。
- 结果：成功；Summary 290 字，正文 3,430 字，8 个动态工作主题。
- 发现：正文仍复述少量分支、提交号、worktree 和 HEAD。随后在 Projection 增加确定性 Git 尾句剥离；该增量只做自动化验证，不再追加模型调用。

## 5. 部署与回退

- 只重建并替换 API；DB、MinIO、Web 未重启。
- API 健康检查返回 `{"status":"ok"}`。
- `.env` 已切换 `MANAGED_AGENT_REPORT_SKILL_VERSION=1.0.50`，运行容器核对一致。
- API 回退镜像：`project_manager-api:rollback-before-concise-skill-20260724`。
- Skill 回退：将测试服配置恢复为 `1.0.49`，只重启 API；已发布的不可变 `1.0.50` 不删除、不覆盖。

## 6. 最终边界

- 测试服 Report Context 大小、单次读取、Skill 加载、真实写回和 Summary/Content 组合通过。
- Git 轨迹剥离增量通过自动化，后续普通测试 Run 持续观察，不为此单独消耗模型调用。
- 未推送远端，未部署生产。
