# V1 风险与三轮设计 Review

> 当前状态：V1 设计评审，不代表代码已实现

## Review 1：是否仍然是 Agent

平台只固定身份、周期、selection、权限、来源和完整性。Agent 继续负责跨 Session 归并、完成度、冲突和表达，不由服务端生成日报正文。

结论：这是确定性运行外壳加 Agent 语义核心，不是模板工作流。

## Review 2：是否过度设计

V1 只包含：

- 一个 Context Builder；
- 一张 `report_run_contexts`；
- 一个 `get_report_context`；
- 默认 Agent 切换；
- 现有写回和失败流程复用。

明确删除或后置：

- Evidence 回查；
- 对象存储；
- confidence 系统；
- 来源版本平台；
- 定制 Agent 开放；
- 多表事实血缘；
- 旧 MCP 废弃。

结论：V1 已收敛为最小可验证纵向链路。

## Review 3：风险与停止条件

| 风险 | V1 控制 |
| --- | --- |
| Context Builder 遗漏事实 | 先离线对照 B01-B10，任何实质遗漏立即停止 |
| 服务端变成语义总结器 | 禁止 LLM、Top-K 和重要性排序 |
| JSONB 快照过大 | 只保存规范化事实，测量平均/P95/最大大小 |
| 权限被 run_id 绕过 | 每次读取和写回重新认证、校验 Run 归属 |
| Agent 能力被削弱 | Agent 保留归并、完成度、冲突和表达 |
| 新旧链路难回退 | 保留旧工具和旧 Agent Version |
| Skill 继续膨胀 | MCP 路由下沉后只做去重，不新增同义规则 |

停止条件：

1. Context 相比冻结 Digest 遗漏实质工作对象；
2. Context 构建需要引入第二个 LLM；
3. 必须同时建设 Evidence、对象存储或版本平台才能运行；
4. 新链路影响 Session 上传或 Token 统计口径；
5. 默认 Agent 无法仅凭一次 Context 读取完成十样本报告。

最终结论：先完成 V1 纵向链路和十样本验证，不设计尚无真实需求证据的 V2。
