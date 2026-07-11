# 系统资源 Skill 与 MCP 管理文档

## 文档状态

- 方案状态：14.157 开发验证、生产 Skill 刷新、生产部署和真实模型回归均已完成。
- 最近复审日期：2026-07-11。
- 适用项目：`/home/intellif/dev/project_manager`。
- Agent 平台仅作为接口和实现约束参考，禁止修改 `/home/intellif/dev/sandboxed-agent-platform`。

### 2026-07-11 内容约束实施记录

- 开发系统 Skill：`100866/aida-report@1.0.0`。
- 当前开发 SkillID：`f571820d-1441-49c0-88d1-0a472b0b1a77`。
- 当前开发 SHA256：`83f7cd8c3c8534998a4eb1a9c98dc2e14fee2f5f63303275cf5497879b5e8cc9`。
- 生产 Skill：`10086/aida-report@1.0.0`，SkillID `37630dde-98a8-4ae5-ba5e-e7c63d77bfd3`，SHA256 `83f7cd8c3c8534998a4eb1a9c98dc2e14fee2f5f63303275cf5497879b5e8cc9`。
- Aida 注入权威 `calendar_context`，`write_report_result` 拒绝错误星期、内部 ID/编号和 UUID，并允许 Agent 使用相同 run_id 修正后重试。
- Report MCP 为部门 scope 返回稳定 `department_name`；当前产品没有部门实体名称时使用“部门”，禁止从 ID 或人员信息猜测。
- Skill 明确区分名册人数、报告提交覆盖和成员活动，禁止把“小组报告已提交”推导为“全员参与/全部在岗”。
- 24 个六类报告真实模型任务全部成功，内容约束 24/24 通过；补充部门日报/周报定向回归 2/2 通过。
- 生产最终版本：提交 `0be0f4e`，镜像标签 `20260711-0be0f4e-report-content`。
- 生产部门日报定向回归通过：名册 4 人、实际提交 3 人、缺失测试09 1 人，未出现“全员参与”、日报统计周报或“无个人日报”等矛盾表述。
- 验证结果见 [报告日期与身份约束修复开发测试报告-20260711.md](./报告日期与身份约束修复开发测试报告-20260711.md) 和 [部门报告覆盖语义定向回归-20260711.md](./部门报告覆盖语义定向回归-20260711.md)。
- 生产发布与问题闭环见 [生产报告发布回归最终报告-20260711.md](./生产报告发布回归最终报告-20260711.md)。

### 2026-07-10 实施记录

- 开发系统账号显示名：`aida-test`。
- 开发系统账号 AIHub username / Registry owner：`100866`。
- 开发系统 Skill：`100866/aida-report@1.0.0`。
- SkillID：`dca4fddb-68e9-4284-831f-a8e5e3a5180f`。
- SHA256：`271bee73dc110305fc8e3ec22e5347bc1e2a56ef29f4103db5f9db1e673806aa`。
- 14.157 API 容器配置：`MANAGED_AGENT_REPORT_SKILL_OWNER=100866`。
- 手动报告、定时报告 run-now、旧 Agent owner 修复均已完成真实模型验证。
- 生产账号显示名为 `aida-system`、Registry owner 为 `10086`；生产系统 Skill 尚未发布，本轮未部署生产。
- 专项结果见 [系统报告Skill集中管理开发测试报告-20260710.md](./系统报告Skill集中管理开发测试报告-20260710.md)。

## 背景

当前 Aida 开发环境和生产环境共用同一个 managed agent platform：

- managed agent platform：`http://192.168.18.107:3081`
- 开发 Aida：`http://192.168.14.157:5173`
- 生产 Aida：`http://113.100.143.91:9180`

因此，Agent 平台的 Skill Registry 是共享的。开发环境如果直接操作生产使用的系统 Skill，可能影响生产报告 Agent 运行。

当前 Aida 代码仍通过 `ensureUserReportSkill` 给普通用户查询或创建 `aida-report`。这会让同一份系统报告 Skill 在多个普通用户下重复存在，也是当前 Registry 中重复资产的主要来源。

当前默认报告 Agent 已经使用内联 MCP Server，而不是 Registry MCP Binding：

- MCP URL 由 Aida 环境配置提供。
- Credential Slot 为 `AIDA_REPORT_MCP_AUTH`。
- 运行时注入当前登录用户 Token。
- 默认 Agent 的 `MCPBindings` 为空。

所以本方案需要分别处理两类资源：

- 系统报告 Skill：由专用系统账号集中维护，普通用户只引用。
- 报告 MCP：继续由 Aida 按环境以内联方式注入，不作为系统账号名下的 Registry MCP 资产管理。

## 目标产品形态

系统默认报告能力必须满足：

- 开发和生产引用不同系统账号下的报告 Skill。
- 普通用户不需要创建、复制或维护系统报告 Skill。
- 默认报告 Agent 引用 Agent 平台已确认存在的系统 Skill。
- Aida 不从当前用户信息推导系统 Skill owner。
- MCP 使用当前运行用户身份访问数据，不使用系统账号 Token。
- 普通用户自定义 Skill、MCP 和 Agent 的现有能力不受影响。
- 配置或系统 Skill 异常时，在修改 Agent 前失败，禁止写入不可解析的 SkillRef。

## 系统账号规划

| 环境 | 显示名 | AIHub 登录 username / Registry owner | 用途 |
| --- | --- | --- | --- |
| 生产 | `aida-system` | `10086` | 维护生产默认报告 Skill |
| 开发 | `aida-test` | `100866` | 维护 14.157 开发环境默认报告 Skill |

规则：

- 生产环境只能引用 Registry owner `10086` 创建的系统报告 Skill。
- 14.157 开发环境只能引用 Registry owner `100866` 创建的系统报告 Skill。
- 普通用户账号不得自动创建系统默认 `aida-report`。
- 不再使用 `t01`、`t03`、`1066` 或真实员工账号作为系统 Skill owner。
- `aida-system` 和 `aida-test` 是 AIHub 显示名，不是可写入 SkillRef 的 owner。Agent 平台按 AIHub 登录 username 精确解析 owner，因此实际配置必须分别使用 `10086` 和 `100866`。
- `10086` 和 `100866` 必须保持稳定。修改登录 username 会导致已有显式 owner 引用失效。

## 系统 Skill 命名

首轮开发使用以下资源：

| 环境 | owner | slug | version |
| --- | --- | --- | --- |
| 生产 | `10086` | `aida-report` | `1.0.0` |
| 开发 | `100866` | `aida-report` | `1.0.0` |

生产引用示例：

```json
{
  "owner": "10086",
  "slug": "aida-report",
  "version": "1.0.0"
}
```

开发引用示例：

```json
{
  "owner": "100866",
  "slug": "aida-report",
  "version": "1.0.0"
}
```

Agent 平台把 Skill 版本视为不可变资产。本轮只发布当前最新内容的 `1.0.0`，不在 Aida 运行时实现同版本覆盖或自动更新。后续正式升级应发布新的补丁版本并更新环境配置；开发期如需直接重建同版本资产，必须使用独立维护流程，不能放进普通用户请求链路。

## Owner 解析原则

### 核心约束

环境配置中的 owner 只是查找系统 Skill 的约束，不是可以直接写入 Agent 的可信引用。

禁止以下流程：

```text
读取 MANAGED_AGENT_REPORT_SKILL_OWNER
-> 直接拼接 owner + slug + version
-> 创建或修复 Agent
```

必须采用以下流程：

```text
使用当前请求用户 Token 查询 Agent 平台公共 Skill Registry
-> 按配置 owner + slug + version 查找未归档资产
-> 校验只存在一个匹配项
-> 校验平台返回 owner 非空且与配置一致
-> 使用平台返回的 owner + slug + version 构造 SkillRef
-> 解析成功后才允许创建或修复 Agent
```

如果系统 Skill 不存在、已归档、owner 为空或 owner 不一致：

- 返回明确的系统资源配置错误。
- 不回退到当前 Aida 用户名或用户 ID。
- 不给当前用户创建 `aida-report`。
- 不修改现有 Agent。
- 不把错误引用写入 Agent 平台。

### Agent 平台约束

Agent 平台当前解析规则为：

- SkillRef 带 owner：根据 `users.username + slug + version` 精确解析。
- SkillRef 不带 owner：根据 Agent owner 解析该用户自己的 Skill。
- 解析失败：在创建 Session 前返回 `SKILL_NOT_FOUND`。

集中式系统 Skill 属于另一个账号，因此最终 SkillRef 必须带 owner。历史故障不是“显式 owner”能力本身错误，而是 Aida 使用本地用户信息猜出了错误 owner。

## 后端开发方案

### 1. 配置项

新增唯一必要配置：

```env
MANAGED_AGENT_REPORT_SKILL_OWNER=100866
```

生产配置为：

```env
MANAGED_AGENT_REPORT_SKILL_OWNER=10086
```

继续沿用：

```env
MANAGED_AGENT_REPORT_SKILL_SLUG=aida-report
MANAGED_AGENT_REPORT_SKILL_VERSION=1.0.0
MANAGED_AGENT_REPORT_MCP_SLUG=aida-report-mcp
MANAGED_AGENT_REPORT_MCP_URL=<当前环境的报告 MCP URL>
```

不增加 `MANAGED_AGENT_REPORT_MCP_OWNER`。

### 2. 系统 Skill 解析器

在 Aida 后端新增单一解析入口，例如：

```go
resolveSystemReportSkill(ctx, client) (model.ManagedSkillRef, error)
```

职责：

- 配置 owner 为空时直接返回系统资源配置错误。
- 调用 Agent 平台公共 Skill 列表接口。
- 精确筛选配置指定的 owner、slug 和 version。
- 排除归档资产。
- 使用 Agent 平台响应中的 owner、slug 和 version 返回 SkillRef。
- 对缺失、空 owner、配置不一致和重复结果返回可识别错误。

解析器使用当前请求用户 Token 查询公共 Registry，不保存或使用 `10086`、`100866` Token。

### 3. 默认 Agent 创建、修复和运行

以下路径统一先调用系统 Skill 解析器：

- 创建默认报告 Agent。
- 修复默认报告 Agent。
- 手动运行报告 Agent。
- 定时报告任务运行前的依赖检查。

手动报告运行和定时报告运行当前不是同一条调用链：手动运行进入 `StartReportAgentRun`，定时运行通过 `executeReportAgentScheduleRun` 直接创建 Session。因此实现时应抽取一个两条路径共用的“解析系统 Skill 并确认/修复报告 Agent”函数，不能假设修改手动运行入口后定时任务会自动生效。

处理顺序必须固定为：

```text
解析系统 Skill
-> 构造完整 Agent 请求
-> 创建或更新 Agent
-> 启动运行
```

禁止先 PUT 修复 Agent，再验证 Skill 是否存在。

默认报告 Agent 路径不再通过 `currentManagedOwner`、当前用户 ID 或 fallback 计算系统 Skill owner。历史 `reportSkillRefOwner(skill, fallbackOwner)` 不再承担系统 Skill owner 推导职责。

### 4. Skill 列表

`include_system=true` 时，Aida 后端执行：

```text
查询 scope=mine
+ 解析配置指定的公共系统 Skill
+ 过滤当前用户自己的重复 aida-report
+ 合并并去重
-> 返回给前端
```

前端不直接接收完整公共 Registry，只看到当前用户 Skill 和当前环境指定的系统报告 Skill。

`include_system=false` 时保持用户自己的 Skill 列表行为不变。

### 5. 删除旧的自动创建逻辑

系统模式启用后：

- 删除默认报告流程对 `ensureUserReportSkill` 的调用。
- 禁止请求链路自动执行 `CreateSkill(aida-report)`。
- 普通用户自定义 Skill 创建接口保持不变。
- 已有普通用户重复报告 Skill 作为测试脏数据单独清理，不增加长期兼容逻辑。

## MCP 管理规则

默认报告 Agent 的 MCP 保持当前内联配置：

```text
name: aida-report-mcp
url: 当前 Aida 环境的 /api/v1/mcp/reports
credential slot: AIDA_REPORT_MCP_AUTH
auth header: Authorization
auth scheme: Bearer
```

运行时规则：

- 手动运行使用当前登录用户 Token。
- 定时运行使用任务 owner 对应的用户 Token。
- 系统账号 Token 不得用于读取普通用户、团队或部门报告数据。
- 默认报告 Agent 不依赖 `10086` 或 `100866` 名下的 Registry MCP Binding。

普通用户自定义 MCP 和自定义 Agent 的 MCP Binding 能力保持不变。

## 开发实施顺序

1. 使用显示名为 `aida-test`、登录 username 为 `100866` 的账号在 Agent 平台发布开发系统 Skill `aida-report@1.0.0`。
2. 使用公共 Skill 列表确认平台返回 owner 为 `100866`，并记录 SkillID 和 SHA256 作为发布核对信息。
3. 增加 `MANAGED_AGENT_REPORT_SKILL_OWNER` 配置和系统 Skill 解析器。
4. 修改系统 Skill 列表合并逻辑。
5. 替换默认 Agent 创建、修复、手动运行和定时运行路径。
6. 增加历史 owner 故障的定向测试。
7. 在 14.157 配置 `MANAGED_AGENT_REPORT_SKILL_OWNER=100866` 并重新构建 API。
8. 清理或归档开发测试账号的旧默认报告 Agent 和重复报告 Skill。
9. 使用普通测试账号重新创建默认报告 Agent 并运行六类报告。
10. 14.157 验收通过后，才准备显示名为 `aida-system`、Registry owner 为 `10086` 的生产 Skill 和生产部署。

## 生产实施顺序

1. 使用显示名为 `aida-system`、登录 username 为 `10086` 的账号发布生产系统 Skill。
2. 在部署前用普通生产账号 Token 验证公共 Registry 可见且返回 owner 为 `10086`。
3. 生产配置 `MANAGED_AGENT_REPORT_SKILL_OWNER=10086`。
4. 部署包含解析器和失败不写入保护的新后端。
5. 创建或重建测试用默认报告 Agent，确认 SkillRef 来自平台返回值。
6. 先验证个人日报和个人周报，再按个人、小组、部门顺序验证六类报告。
7. 验收通过后再处理普通用户历史重复系统 Skill。

## 历史故障与回归要求

### 故障记录

Git 提交 `13af5c0` 引入过 owner 自动补全逻辑：当报告 SkillRef owner 为空时，用当前 Aida 用户名补充 owner。

该逻辑曾让默认报告 Agent 写入类似引用：

```json
{
  "owner": "001898",
  "slug": "aida-report",
  "version": "1.0.0"
}
```

但 Agent 平台中的实际 Skill 不能按该 owner 解析，运行时返回：

```text
SKILL_NOT_FOUND: skill "aida-report"@"1.0.0" is not available in your registry
```

Git 提交 `f1bd4be` 修复为优先使用 Agent 平台返回的 owner；平台已返回 SkillID 但 owner 为空时保持 owner 为空，并清理已有错误 owner。

### 必须增加的测试

可执行 Case 和通过门槛统一维护在 [报告任务测试用例.md](./报告任务测试用例.md) 的“8.17 系统报告 Skill 与历史 Owner Bug 定向 Case”，本节保留开发侧不可遗漏的测试范围。

- Aida 本地用户名与 Agent 平台用户名不同，系统 Skill 仍可正确解析和运行。
- 配置 owner 错误时返回配置错误，并断言没有发出 Agent Create/PUT 请求。
- 公共 Skill 返回 owner 为空时禁止 fallback。
- 系统 Skill 不存在时禁止创建普通用户副本。
- 系统 Skill 已归档时禁止创建或修复默认 Agent。
- 现有 Agent 带历史错误 owner 时，必须先解析系统 Skill，再修复为平台返回的 owner。
- 两个普通用户可以引用同一份系统 Skill，且各自 MCP 数据权限保持隔离。
- 普通用户没有自己的 `aida-report` 时仍可创建和运行默认报告 Agent。
- `include_system=true` 只返回一份当前环境系统报告 Skill。
- `include_system=false` 不注入系统 Skill。
- 手动运行和定时运行均使用当前运行用户身份访问报告 MCP。
- 系统账号 username 修改或配置漂移时明确失败，不污染 Agent 配置。

## 禁止事项

- 禁止修改 `/home/intellif/dev/sandboxed-agent-platform` 源码解决本方案问题。
- 禁止把环境配置中的 owner 未经平台解析直接写入 Agent。
- 禁止系统 Skill 解析失败后回退当前用户名、用户 ID 或空 owner。
- 禁止生产默认 Agent 引用 `100866` Skill。
- 禁止开发默认 Agent 引用 `10086` Skill。
- 禁止在普通用户请求链路中自动创建系统报告 Skill。
- 禁止默认报告 Agent 使用系统账号 Token 读取用户业务数据。
- 禁止为了默认报告 Agent 重新引入 Registry MCP Binding。
- 禁止在开发调试中归档或删除 `10086` 的生产系统 Skill。

## 可行性复审

### 已确认可行

- Agent 平台公共 Skill 列表能够返回全部用户的未归档 Skill。
- Agent 平台支持显式 owner 的跨用户 SkillRef。
- 默认报告 Agent 已支持内联 MCP Server，不需要系统 MCP Registry 资产。
- Aida 已有 Skill 列表代理、Agent 创建、修复、手动运行和定时运行入口，改动集中在 managed agent handler 和配置层。
- 定时报告路径能够使用任务 owner 对应的用户 Client 查询公共 Skill，但当前会绕过手动运行修复逻辑，开发时必须显式接入共用解析入口。
- 不需要修改数据库结构、前端 Agent payload 协议或 Agent 平台源码。

### 前置条件

- 开发和生产系统 Skill 必须先于 Aida 配置切换完成发布。
- Agent 平台公共列表必须返回系统 Skill 的真实 owner。
- 系统账号 username 必须保持稳定。
- 14.157 必须先完成真实模型和定时任务验证，不能直接首发生产。

### 主要风险及控制

| 风险 | 控制措施 |
| --- | --- |
| owner 配置拼错或系统账号改名 | 平台预解析失败后不写 Agent |
| 系统 Skill 未发布或被归档 | 创建、修复、运行前返回明确配置错误 |
| 再次从本地用户推导 owner | 删除系统路径 fallback，并增加历史故障测试 |
| 公共 Registry 中存在大量重复资产 | 后端只合并配置指定的精确系统 Skill |
| MCP 使用系统账号权限访问用户数据 | 保持内联 MCP 和当前运行用户 Token |
| 旧 Agent 引用普通用户 Skill | 开发期按测试脏数据清理，生产部署前受控重建 |

### 复审结论

方案在当前代码和 Agent 平台接口约束下可行。最终边界应固定为“系统账号集中维护报告 Skill，Aida 按环境内联注入报告 MCP”。

实施时最大的风险不是跨账号引用本身，而是未经平台确认就构造 owner。只要严格执行“先解析、后写入、失败不修改”，并完成历史 `SKILL_NOT_FOUND` 定向回归，该方案不需要侵入 Agent 平台，也不会改变普通用户自定义资产的产品形态。
