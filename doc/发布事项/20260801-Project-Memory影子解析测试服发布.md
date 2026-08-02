# Project Memory 影子解析测试服发布

- 日期：2026-08-01
- 环境：AIDA 测试服
- 源码基线：`main` / `b40691e`，工作区包含本轮尚未提交的报告生成改动
- 改动范围：系统默认个人日报 Project Memory 第一阶段影子解析

## 部署组件

- API：`sha256:c8af4f08e37a1b723ccb2315b4f1f178aa8c626ed1b7f24aebbd1edc6857866d`
- Web：未部署
- CLI：未发布

## 数据变更

- 测试服执行迁移 `034_report_project_memory_shadow.sql`；
- 新增项目、别名、出现记录、同步状态和影子解析快照表；
- 影子结果不进入 Report Agent Context，不改变日报生成结果；
- 未涉及生产数据库。

## 验证

- `cd api && go test ./...`：全部通过；
- `git diff --check`：通过；
- `docker compose up -d --build --no-deps api`：成功；
- `docker compose ps api`：API 运行；
- `GET http://192.168.14.157:18090/health`：`{"status":"ok"}`；
- `schema_migrations`：版本 `34` 已应用。

使用测试服 UID 305 的历史最终日报与三个真实冻结 Report Context 回放影子解析：

- 首轮发现旧解析会把“工作概览”整句成果当成项目名；
- 调整为优先读取“工作详情”的稳定三级标题，概览与手写列表只作回退；
- 解析算法版本参与来源指纹，规则变化会自动重建项目记忆；
- `project-memory-shadow/v3` 的一份真实 Context 含 77 个 Facts，召回 45 个低置信度候选，0 个被强制匹配；说明影子观测已生效，同时保持“证据不足不归并”。

## 状态

- 已部署测试服 API；
- 未提交、未推送；
- 未发布生产；
- 当前只用于采集候选质量，尚未启用 Project Memory 影响日报生成。
