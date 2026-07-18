# 第五阶段：Report Context V1

> 建立日期：2026-07-18
> 开发分支：`feat/report-context-v1`
> 状态：V1 个人报告纵向链路开发中，未发布

## 1. 目标

V1 只解决一个问题：不再让默认 Report Agent 依靠超长 Skill 自行选择和编排多个读取 MCP，而是由服务端按 Report Run 稳定准备一次完整报告上下文。

```text
前端选择来源
  → API 创建并冻结 Report Run
  → Context Builder 准备统一上下文
  → 服务端校验完整性并保存一条 Context 记录
  → Agent 通过 run_id 读取 Context
  → Agent 使用 Skill 生成报告
  → 复用现有 MCP 写回
```

## 2. V1 范围

V1 第一批只覆盖存在冻结 selection 的 `personal_daily/personal_weekly`。团队和部门报告继续走现有工具链，不在本批次重构。

本批只新增：

1. 一个确定性的 `Report Context Builder`；
2. 一张轻量表 `report_run_contexts`；
3. 一个读取工具 `get_report_context(run_id)`；
4. 默认 Agent 的读取流程切换；
5. 对应的完整性、权限、十样本和容量测试。

继续复用：

- 现有 Report Run；
- 现有 Session selection snapshot；
- 现有 Session Digest；
- 现有 `write_report_result/write_report_failure`；
- 现有 Run 失败状态；
- 现有报告查询和前端展示。

## 3. 明确不做

- 不开发 `get_report_evidence`；
- 不建设对象存储和 Context Artifact 平台；
- 不建设通用 confidence 评分；
- 不建设所有来源的统一版本系统；
- 不在本期开放定制 Agent；
- 不删除现有低层 MCP 工具；
- 不修改 Digest、Session 上传和 Token 统计；
- 不做 Top-K、服务端语义总结和正文审查。

这些内容不是 V1 的隐含待办。只有 V1 真实运行证明存在必要性后，才能另立方案。

## 4. Agent 边界

平台只决定数据边界、权限、来源和完整性。Agent 继续负责：

- 跨 Session 合并同一工作；
- 判断完成、进行中、失败和阻塞；
- 处理任务与 Session 事实冲突；
- 保留所有彼此不同的实质结果；
- 使用默认或后续用户 Skill 组织最终表达。

## 5. 文档

- [需求与产品边界](01-需求与产品边界.md)
- [V1 架构与数据流程](02-架构与数据流程.md)
- [V1 数据模型与 MCP 契约](03-数据模型与MCP契约.md)
- [实施、测试与回退](04-实施测试与迁移方案.md)
- [三轮设计 Review](05-风险与三轮设计Review.md)
- [开发实现记录](06-开发实现记录.md)
