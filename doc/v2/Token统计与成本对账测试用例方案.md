# Token 统计与成本对账测试用例方案

## 1. 测试目标

验证本地 Aida CLI 上传 Session 后，Token、Session 切片、个人统计、管理范围统计和成本之间能够稳定对账，重点覆盖“同一个 Session 分多天持续增长、每天重复上传、上传失败重试、价格快照”场景。

测试分为两类：

1. 固定 JSONL 测试数据，用于精确验证解析公式和边界值。
2. 本机真实 Session + 四个测试角色，用于验证真实 CLI、上传 cursor、后台处理和权限范围。

## 2. 统一对账口径

Token 总量必须满足：

```text
total_tokens = input_tokens + cache_read_tokens + cache_write_tokens + output_tokens
```

每次增量上传必须满足：

```text
本次累计值 = 上次累计值 + 本次新增 Usage
```

重复上传未变化的文件时：

```text
新增 Chunk = 0
Token 累计值不变
成本累计值不变
```

成本以 Usage Component 生成时的价格和汇率快照为准，未计价 Token 不得按 0 元计入已计价成本。

## 3. 固定数据用例

| 编号 | 场景 | 预期 |
|---|---|---|
| F01 | 单条完整 JSONL 事件 | Token 字段与人工计算一致 |
| F02 | 输入、缓存读取、缓存写入、输出同时存在 | 总量等于四类 Token 之和 |
| F03 | 跨两天 Session | 每日切片按活动时间归属，不按 StartedAt 整体归属 |
| F04 | 同一 Session 追加第二天事件 | 第二次只产生新增 Chunk 和新增 Usage |
| F05 | 文件没有变化重复上传 | `unchanged`，新增 Chunk 为 0 |
| F06 | 文件最后一行不完整 | 暂不推进未完成行 cursor，后续补全后可继续上传 |
| F07 | Token counter 重置 | 不产生负数，重置后的增量按新段计算 |
| F08 | 多模型 Session | 按模型拆分 Usage，合计仍等于 Session 总量 |
| F09 | 重复事件/重复 Chunk | 不重复计入 Usage |
| F10 | 上传请求超时后重试 | ACK 成功的 Chunk 不重复计费 |
| F11 | 文件被删除或移动 | 列表清理缓存，不影响其他 Session |
| F12 | 索引文件损坏 | 自动重建，不影响上传 cursor |
| F13 | Session 持续增长 | 只读取并上传新增内容 |
| F14 | 本地时区跨日 | 服务端活动日期与约定时区一致 |
| F15 | 未知模型 | 标记未计价，不把成本伪装成 0 |

## 4. 四角色真实流程

| 步骤 | 工程师 t05 | PM t01 | TL t03 | 总监 t02 |
|---|---|---|---|---|
| 登录 | 验证身份 | 验证身份 | 验证身份 | 验证身份 |
| 上传 | 随机本机 Session | 随机本机 Session | 随机本机 Session | 随机本机 Session |
| 重复上传 | unchanged | unchanged | unchanged | unchanged |
| 个人统计 | Token、成本、明细 | Token、成本、明细 | Token、成本、明细 | Token、成本、明细 |
| 管理统计 | 无权限 | 无权限 | 小组范围 | 部门范围 |
| 权限边界 | 不得查看管理数据 | 不得查看管理数据 | 不得越过负责小组 | 不得越过负责部门 |

## 5. 真实测试记录要求

每个角色记录：

- CLI 版本、API 地址和账号角色。
- Session ref、文件大小、修改时间、模型和本地总 Token。
- 上传结果：`uploaded`、`unchanged`、Chunk 数量、是否 pending tail。
- 后台任务完成状态，必须 `pending=0`。
- 个人统计的 Token、成本、pricing status、未计价 Token。
- TL 小组或总监部门的 Token、成本、人员排名和 Session 明细。
- 明细合计与汇总值是否一致。

## 6. 验收标准

1. 四个角色均能真实登录并上传本机 Session。
2. 重复上传不增加 Token 和成本。
3. 后台处理完成后，个人明细合计等于个人汇总。
4. TL 管理统计只包含负责小组范围。
5. 总监管理统计只包含负责部门范围。
6. Token 统计和成本统计的 `freshness` 为 `ready`，pending 为 0。
7. 未知模型或未计价数据必须显式标记。
8. 任意失败用例都要记录原始 Session、cursor、Chunk 和服务端 Usage 证据，不以页面截图作为唯一依据。
