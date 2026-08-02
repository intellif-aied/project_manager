# Project Memory 概览优先与来源权重测试服发布

- 日期：2026-08-02
- 环境：AIDA 测试服
- 源码基线：`main` / `b40691e`，工作区包含尚未提交的报告生成改动
- 改动范围：Project Memory 影子解析来源优先级与权重

## 部署组件

- API：`sha256:7bf1849cdd555c217cb07567799ff6f1943c7218cc9a2f847f9809c7ecf3cbdb`
- Web：未部署
- CLI：未发布

## 规则调整

- 工作概览或人工顶层事项是用户最终选择的主要工作；工作详情不再建立项目；
- 完全人工日报无需 Summary，读取概览或顶层编号事项，权重 `1.00`；
- AI 生成后人工修改稿读取最终概览，权重 `0.95`；
- AI 原样保存且存在 Brief 时读取结构化 `workstreams[].subject`，权重 `0.75`；
- 旧 AI 稿没有 Brief 时回退概览，权重降为 `0.55`；
- Generated Draft 继续排除。

## 数据变更

- 测试服执行迁移 `035_report_project_memory_source_weight.sql`；
- 项目、别名和出现记录增加 `source_type/source_weight`；
- 权重只参与候选项目排序，不进入 Agent Context，也不改变当天事实边界；
- 未涉及生产数据库。

## 验证

- `cd api && go test ./...`：全部通过；
- `git diff --check`：通过；
- 测试服迁移版本 `34/35` 已应用；
- 测试服 API 健康接口返回 `{"status":"ok"}`。

真实冻结 Context 回放：

- `project-memory-shadow/v4`：13 Facts，13 个有候选，6 个由 Brief Subject `AgentENV` 高置信度匹配；
- `project-memory-shadow/v5`：250 Facts，35 个低置信度候选，0 个强制匹配；无 Brief 的旧 AI 概览均降权为 `0.55`；
- 当前仍为影子模式，回放结果不会改变日报。

## 状态

- 已部署测试服 API；
- 未提交、未推送；
- 未发布生产。
