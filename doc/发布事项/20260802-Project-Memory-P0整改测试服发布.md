# Project Memory P0 整改测试服发布

> 日期：2026-08-02  
> 环境：AIDA 测试服  
> 源码基线：`main@98ea65f` 加本次未提交 P0 工作区改动

## 改动范围

- Project Memory 不再把当日工作概览原句写入 alias；
- 无效项目名只降级对应决策，不拒绝整批 proposal；
- Report Context 的 Project Memory Hint 移除历史正文；
- 默认系统日报不再混用旧 Continuity Context；
- Project Memory Hint 使用结构化锚点、唯一 workstream subject 和最大主线数；
- 最近有效但未命中的项目只作为可忽略的 `candidate_only` 名称候选；
- 重复短 alias 同时属于多个项目时保持未匹配；
- Project Memory Skill 补充父项目预归并、安全完整/短 alias 契约；
- Report Skill 明确区分锚点 Hint 与无锚点候选；
- Brief JSON 仅缺少尾部容器闭合符时执行有界修复，业务校验不放宽；
- 个人自定义 Agent 的既有 Continuity Context 行为不变；
- 补充方案文档和单元测试。

## 实际部署

- 组件：API；
- 测试服地址：`http://192.168.14.157:18090`；
- 镜像：`project_manager-api`；
- 未部署 Web、CLI、DB、MinIO；
- 未执行数据库迁移或数据备份。

## 构建与验证

- `go test ./internal/reportmemory ./internal/reportcontext ./internal/reportrun`：通过；
- `go test ./...`：通过；
- `docker compose up -d --build --no-deps api`：成功；
- `docker compose ps api`：API 容器运行正常；
- `GET /health`：返回 `{"status":"ok"}`；
- 冻结样本 `case-016`：Project Memory 命中，Context 无 Continuity/Recent Context，日报成功；
- 冻结样本 `case-001`：无 Project Memory 命中，Context 无历史回退，日报成功。
- `case-016` 结构化锚点契约重复 3 次：3/3 均收敛为 1 条 Symphony 主线，最终日报全部成功；
- JSON 尾部闭合修复：缺失容器闭合符可恢复，内容错误、字符串截断和括号错配仍拒绝。
- 刘乐 7 月 31 日 8 个冻结 Session 切片：整改前 Project Memory 0 命中；整改后 `使用手册` 命中 20 个 Fact 锚点，连续 3 次均生成 1 条“芯片验证平台”主线；
- 测试系统资产：`aida-report@1.1.28`、`aida-project-memory@project-memory-v4`；仅测试 Agent 使用；`1.1.28` 精简契约烟测成功。

## 版本状态

- 当前修改尚未提交、尚未推送；
- 未涉及生产环境；
- 完整 A/B/C 重复盲评尚未执行，本记录不代表生产发布结论。
