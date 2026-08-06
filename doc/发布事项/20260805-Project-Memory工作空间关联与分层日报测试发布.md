# Project Memory 工作空间关联与分层日报测试发布

> 日期：2026-08-05
> 环境：AIDA 测试服（192.168.14.157）
> 源码基线：`main@ea4697d` 加本记录所列未提交改动

## 发布范围

| 组件 | 结果 | 说明 |
| --- | --- | --- |
| API | 已部署 | 镜像 `sha256:cdbc983efd71e95683a515fd7fcabee5f552782028f762fba9540bc9a90ea8a2` |
| DB | 已迁移 | migration 038；新增 Fact 来源、Project↔Workspace Link 及证据表 |
| System Report Skill | 已发布并切换 | `100866/aida-report@1.1.31`，Skill ID `861a567f-7ca0-4be7-b0e0-78039718947d`，正文 SHA256 `35c162803bdefe641a158448d3a46167eb0c4f32f4a3f5822db2026ce6c4aa56` |
| Project Memory Skill / Agent | 已更新 | `aida-project-memory@project-memory-v5`，Agent managed v2，模型 `deepseek-v4-flash` |
| Report / Project Memory MCP | 沿用 | MCP URL 与协议版本不变 |
| Web | 未涉及 | 未构建、未重启 |
| CLI | 沿用 Phase 1 测试包 | 本次未重复发布 |
| 生产环境 | 未涉及 | 未灰度、未发布、未写入 |

## 变更行为

- 服务端保存 Brief Fact 到 Session/Workspace 的私有证据链，Project Memory 决策完成后确定性写入 Project↔Workspace 弱关联。
- 系统个人日报最多读取 3 个与当天 Fact Workspace 相交的历史项目候选；无锚点最近项目不再进入 Context。
- 候选只辅助命名和归并，不提供历史成果，也不覆盖当天 Facts。
- 已接受 Brief 由服务端渲染为一份正文：简单事项保持一级列表，多成果事项使用一级项目加二级成果。
- Project Memory 夜间窗口新增可配置项，默认仍为北京时间 02:00～06:00；测试结束已恢复默认。

## 验证

- `cd api && go test ./...`：通过。
- `cd daemon && go test ./... -count=1 && go vet ./...`：通过。
- migration 037、038 均已应用，API `/health` 成功。
- Fact Source 与 Workspace Evidence 写入：A 组 Run `6b9d6c3c-e215-489d-901b-a29caa717a3f` 保存 26 个 Fact、8 个 Session 来源、3 个 Workspace。
- Project Memory：2026-07-30 Job 成功，Snapshot `efc68072-42d9-4502-bf17-fb8653e6b346`；5 个主题均取得 Workspace Ref。
- 同源 A/B：无 Hint Run `6b9d6c3c-e215-489d-901b-a29caa717a3f`，3 Hint Run `dd21c173-5050-41ee-b5cc-dd716b053b2c` 与重复 Run `721e2887-a4de-4fdd-a462-26c34d3fa2d8` 均成功。
- 无匹配降级：Run `ad63cff9-8bae-4a30-9abd-95beb662ed6d` 无 Hint，成功生成分层正文。
- 最终测试服配置：Report Skill `1.1.31`、Project Memory Skill `project-memory-v5`、Nightly 02:00～06:00。

## 回退

1. 将测试服 Report Skill 恢复为 `1.1.30`、Project Memory Skill 恢复为 `project-memory-v4`；
2. 恢复上一 API 镜像并只替换 API；
3. migration 038 新表可保留，不影响旧代码读取；不需要删除测试数据；
4. Web、DB 容器、MinIO 与 CLI 不处理。

## 仓库状态

- 当前仍为 `main`；本次改动尚未提交、尚未推送。
- 未执行生产灰度或生产发布。
