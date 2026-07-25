# 20260725 Report Projection 完整性修复测试发布

## 发布元数据

| 字段 | 内容 |
| --- | --- |
| 环境 | 14.157 测试服 |
| 分支 | `fix/report-context-readable-facts` |
| Git 基点 | `65054c0`，包含未提交的本轮代码与文档修改 |
| 组件 | API |
| API 二进制 SHA-256 | `fce874f54a60baa87b1d5b50604fdc7e4877d171721a4c80913871a6a2b3d56b` |
| 生产 | 未修改 |

## 改动范围

- 取消按状态和时间选择少量成果检查点，所有状态使用相同结构去噪规则；
- 修复扁平代码围栏导致围栏后业务正文丢失；
- 技术细节标题不再触发整段跳过；
- 纯 Git 流水删除，混合业务结果无法安全拆分时保留；
- 修复分析统计中的 `Git/Commit/Merge` 被误判为 Git 操作；
- 不修改 Digest、数据库、Web、CLI、Skill、MCP 接口、上传和 Token 统计。

## 部署过程与回退

Docker BuildKit 和原生 Docker builder 均在镜像构建阶段无子进程停滞，旧 API 始终健康。停止本次启动的两个构建客户端后，在 worktree 编译静态 API 二进制，通过 `docker cp` 替换测试 API 容器内 `/usr/local/bin/api` 并只重启该容器。DB、MinIO、Web 和其他容器未重启。

替换前二进制保存在测试服务器 `/tmp/aida-api-before-balanced-projection-v2`，旧镜像保留为 `project_manager-api:rollback-before-balanced-projection-20260725`。回退时将替换前二进制复制回 API 容器并只重启 API；也可使用旧镜像强制重建 API。部署后 `/health` 返回 `{"status":"ok"}`。

## 自动化与离线验收

- `go test ./...`：通过；
- `go vet ./...`：通过；
- `git diff --check`：通过；
- 15 个本机 Session：15/15 通过，304 条有效结果，5 条确定性噪声过滤；
- 31 份生产冻结 Context：31/31 通过，8,626 条有效结果完整映射；
- 完整 Context：16,518,069→1,401,694 bytes，缩小 91.51%，最大 208,787 bytes。

## 测试服真实 Run

| 用户 | Run | Context | Agent 结果 |
| --- | --- | ---: | --- |
| t02 / 304 | `ba855153-14fe-4ca6-bd79-2de014c112de` | 54,753 bytes | 成功读取一次 31,287 字符 MCP Context；第二次模型推理因 GLM-5.2 节点 prefill 容量不足返回 503 |
| t03 / 305 | `1d0ede08-8878-4887-87ca-f9da2f213147` | 54,861 bytes | Skill 加载后首次模型推理返回外部 500，未读取 Context |

两次 Run 均创建唯一外部 Session并正确进入 `failed`，没有重复 Skill、Context、写回或无限重试。模型成功率不作为本轮 Projection 验收条件。

使用上述两个 Run 的冻结 Digest 直接对照：每份 36 条 ResultStatement 全部进入 Projection，0 条整项过滤，0 条压缩超过 50%；正文分别为 49,405→46,004 bytes、49,442→46,041 bytes。测试中发现并修复 Git 分析统计误删，修复后已重新执行全部离线和自动化验收。

## 当前状态

- 测试服 API 已部署本轮最终二进制并健康；
- 未提交、未推送、未合并；
- 未部署生产。
