# V3 Report Brief 主标题驱动 Summary 方案

> 日期：2026-08-01
> 状态：已开发并完成测试服真实回归，尚未提交或发布生产
> 适用范围：System Report Flow 的默认个人日报 `personal_daily`

## 1. 问题

当前两阶段流程在两个位置执行编辑选择：

1. Pass 1 将 Context 归并为 Workstream、Title 和 Deliverables；
2. Pass 2 再次读取全部 Deliverables，自行选择 Summary 内容。

生产 Run `18d5c02c-4f33-4700-be80-4740840aa15b` 中，Knowledge Map Workstream 的 Title 已正确写为“推进 Knowledge Map 产品定义并落地配套技能”，但 Pass 2 又把用于验证 Skill 的“6 岁儿童睡前卡通动画”场景提升到 Summary。该内容不是幻觉，来自 Context 的历史 Fact；问题是支撑场景在第二次编辑选择中被错误提升。

继续增加“某类 Demo 不进 Summary”等提示词只能覆盖单个表现，无法消除双重选择产生的波动。

## 2. 目标与边界

### 2.1 目标

1. 每个 Workstream 只在 Brief 阶段决定一次 Summary 主成果；
2. Demo、测试案例、评测场景、指标和其他支撑事实可进入正文，但不会在 Final 阶段被重新提升到 Summary；
3. Summary 由服务端确定性渲染，顺序和内容可通过 Report Brief Module 的 Interface 直接测试；
4. 不因表达质量增加新的生成失败门槛。

### 2.2 不改

- 不过滤或改写 Report Context 事实；
- 不回退用户明确选择历史 Session 的行为；
- 不新增第二个 Agent、Skill、MCP 或模型调用；
- 不影响 Personal Report Agent、周报、团队/部门报告；
- 不增加数据库 migration；
- 不为儿童动画、Demo、截图等具体词语建立服务端 Case。

## 3. Module 职责

```text
Report Context
  └─ 完整事实，不决定展示层级
      ▼
Report Brief Module
  ├─ Subject：稳定工作对象
  ├─ Title：读者可见的主要成果标题，也是 Summary 唯一来源
  └─ Deliverables：正文成果及支撑信息
      ▼
Final Report
  ├─ Summary：服务端按 Workstream 顺序确定性渲染 Title
  └─ Content：Agent 根据 Deliverables 编写详情
```

`write_report_brief` 继续作为语义 seam。Report Brief Module 隐藏 Summary 生成规则，对调用方只增加一个从已接受 Brief 取得读者 Summary 的小 Interface。Final Report 不再承担编辑选择。

## 4. Interface 语义

### 4.1 Workstream Title

现有 JSON 字段不变，Title 的语义收紧为“完整、读者可见的 Summary 主标题”：

- 包含可识别 Subject；
- 只写一至两个主要成果；
- 不写 Demo、测试案例、验证场景、支撑指标或实现轨迹；
- 专有名词保持原样，其他文字使用自然中文；
- 不写建议、下一步或从缺失证据推导的状态。

Deliverables 仍可保留验证场景和支撑结果。例如“通过 6 岁儿童睡前卡通场景验证知识问题、寻源与证据指针机制”可以出现在 Knowledge Map 正文，但不进入 Title。

### 4.2 确定性 Summary

对于带 Subject 的系统个人日报 Brief：

```text
1. <workstreams[0].title>
2. <workstreams[1].title>
...
```

服务端忽略 Agent 提交的 Summary 文案并使用上述结果覆盖。Agent 在滚动兼容期仍可提交 Summary，但它不再参与最终内容选择。无可汇报工作继续使用固定文案；旧的无 Subject Brief、降级写回、Personal Report Flow 和其他报告类型保持原行为。

## 5. 实现方案

1. 在 `reportbrief.Stored` 增加纯计算的读者 Summary Interface，只处理带 Subject 的 Brief 和无工作 Brief；
2. `ValidateForWrite` 使用该确定性 Summary 做结构和安全校验，不再依赖 Agent 对 Deliverables 的二次概括；
3. `toolWriteReportResult` 在 Brief 校验通过后，用确定性 Summary 重新组装“工作概览 + 工作详情”；
4. System Report Skill 将 Pass 1 的 Title 定义为主成果标题；Pass 2 只按顺序复制 Title 并编写正文，不重新选择 Summary 内容；
5. 保持 `summary` 参数兼容，后续确认所有运行版本完成升级后再决定是否收窄 Tool Interface。

## 6. 测试

### 6.1 确定性测试

- Brief 含一个主标题和多个 Deliverables 时，Summary 只包含 Title；
- Deliverable 包含儿童动画、Demo 或验证场景时，正文保留、Summary 不出现；
- 多 Workstream 按 Brief 顺序编号；
- Agent 提交不同 Summary 时，最终存储仍使用 Brief Title；
- 无工作、旧无 Subject Brief和降级写回保持原行为；
- Personal Report Agent 与其他报告类型不变。

### 6.2 真实回归

1. 将生产问题 Run 的同源 Session 在测试服完整回放；
2. 验证 Knowledge Map Summary 只包含产品判断与 Skill 落地，儿童睡前卡通场景只在正文；
3. 回放现有 303、310、312、313、314 用户样本，人工检查 Title 是否足以承担概览；
4. 检查 Result 重试、降级率和报告保存成功率不回退。

### 6.3 本次已完成验证

- API 全量 Go 测试通过；新增回归用例直接模拟 Knowledge Map 的儿童睡前场景，确认场景保留在详情且不会进入概览；
- 测试服已切换 `100866/aida-report@1.1.23`，只重建 API，健康检查通过；
- 用户 305 默认 Agent 真实 Run `38a02efc-e1a7-4d97-8a72-40a5df0db4d1` 成功，实际加载 `1.1.23`；
- 该 Run 最终 Summary 与数据库中已接受 Brief 的两个 Title 逐字一致，详细 Deliverables 未被二次提升到概览；
- 首轮验证未复制生产数据，先用确定性用例覆盖问题形态；后续生产异常 Session 的只读还原与真实回归见下一节，生产环境始终未发生写入。

### 6.4 生产异常 Session 测试服回归

生产 Agent Session `c84ed869-f17d-47cb-956f-766e3a9c192c` 对应 Aida Run `18d5c02c-4f33-4700-be80-4740840aa15b`，实际使用 8 个 Session 切片。已从生产 MinIO 只读下载这些 Session 的原始上传分片，逐分片校验 SHA256 后按顺序还原，并通过测试账号 305 的正常 CLI 上传链路导入测试服。8 个 Session 均完成 Projection，游标与生产原始 Session 一致，测试 Slice 均为 `ready`。

测试服回归 Run `fd958b59-7c03-4d91-b819-4c64dc7304ff` 使用这 8 个测试 Slice 和 `aida-report@1.1.23`，最终成功生成 Report `0f145647-bff9-4f87-b01b-c0872e5e4f6b`：

- Brief 将儿童睡前卡通动画作为 `IF-Knowledge` 的第三条 Deliverable 保留；
- `IF-Knowledge` Title 为“完成 GPGPU AI Coding 全景调研与 Knowledge Map 方案收敛”；
- 最终工作概览由服务端逐字使用该 Title，没有儿童睡前场景；
- 工作详情仍保留该场景，验证了“完整证据留正文、主要成果进概览”的目标；
- Brief 前两次因 InfoAgent 结果包含操作轨迹被拒，第三次压缩后接受；Result 无校验失败，Run 正常成功。

本次还原的是 8 个完整原始 Session。生产原 Run 选择的是其中 3 个长 Session 的历史尾部 Slice，因此测试 Context 为 798,500 字节，生产 Context 为 230,960 字节，二者不是字节级相同；测试样本覆盖了生产异常事实且输入更大，不把它记录为完全相同的 Context 回放。

## 7. 风险与回退

| 风险 | 控制 |
| --- | --- |
| Title 太泛导致 Summary 信息不足 | 在 Brief 阶段统一 Title 语义，并用多用户样本评测；不在 Final 阶段补选 Deliverable |
| 旧 Skill 的 Title 未包含 Subject | 滚动期允许直接使用已有 Title；新 Skill 明确 Title 必须是完整读者标题 |
| Agent Summary 与最终 Summary 不一致 | 服务端以已接受 Brief 为准，Tool 返回和报告快照记录确定性结果 |
| 影响降级或个人流程 | 只对带 Subject 的 System personal_daily Brief 生效 |

回退只需恢复上一版 API 镜像和 System Report Skill；无数据结构变化，历史 Report、Context 和 Brief 均无需处理。
