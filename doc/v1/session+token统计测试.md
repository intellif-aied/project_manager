# Session + Token 统计测试方案

## 1. 测试目标

验证当前服务下所有角色账号在上传 session 后，Token 统计口径是否正常：

1. 个人统计：每个账号只能看到自己的 `scope=mine` 数据。
2. 组内统计：TL/PM 视角的 `scope=team` 数据是否符合角色权限。
3. 组统计：总监/admin 视角的 `/tokens?group_by=team` 是否按团队汇总。
4. Dashboard Token 卡片是否能基于 `/tokens/sessions` 和 `/tokens?group_by=team` 正常展示。

本方案不新增接口，不改数据库结构，不删除既有数据。测试数据使用唯一 `session_ref` 前缀，重复执行会 update 同一用户下同名 session，避免无限污染。

## 2. 当前可用接口

基础地址按当前开发环境：

```bash
BASE_URL="http://192.168.14.157:5176/api/v1"
```

使用接口：

1. `POST /auth/login`
2. `GET /users`
3. `POST /sessions/batch`
4. `GET /sessions`
5. `GET /tokens/sessions?from=&to=&scope=mine|team`
6. `GET /tokens?period=range&from=&to=&group_by=team|user|model`

上传接口要求 multipart：

1. form 字段 `metadata` 是 JSON。
2. JSON 结构为 `{ "sessions": [...] }`。
3. 可选上传文件字段名为 `file_${session_ref}`。
4. P0 统计验证只需要 metadata + token_usage，不必须上传 raw log 文件。

单条 session metadata 示例：

```json
{
  "session_ref": "token-p0-20260625-zhangsan-1",
  "agent_type": "codex",
  "started_at": "2026-06-25T09:00:00+08:00",
  "ended_at": "2026-06-25T09:30:00+08:00",
  "duration_secs": 1800,
  "model": "gpt-5-codex",
  "summary": "Token 统计测试：张三 session 1",
  "token_usage": {
    "input_tokens": 1000,
    "output_tokens": 200,
    "cache_creation_tokens": 0,
    "cache_read_tokens": 300,
    "total_tokens": 1500,
    "models": ["gpt-5-codex"]
  }
}
```

## 3. 测试账号

内置账号默认密码为 `123`。

| 角色 | employee_id | 姓名 | 团队 |
| --- | --- | --- | --- |
| admin | `admin` | 管理员 | 无 |
| director | `li_director` | 李总监 | 无 |
| pm | `chen_pm` | 陈PM | 无 |
| team_leader | `liu_tl` | 刘TL | AI工程 |
| team_leader | `zhao_tl` | 赵TL | 推理加速 |
| team_leader | `sun_tl` | 孙TL | 模型训练 |
| employee | `zhangsan` | 张三 | AI工程 |
| employee | `lisi` | 李四 | AI工程 |
| employee | `wangwu` | 王五 | AI工程 |
| employee | `zhaoliu` | 赵六 | AI工程 |
| employee | `qianqi` | 钱七 | AI工程 |
| employee | `sunba` | 孙八 | 推理加速 |
| employee | `zhoujiu` | 周九 | 推理加速 |
| employee | `wushi` | 吴十 | 模型训练 |

执行前建议用 admin token 调 `GET /users` 获取当前服务真实用户清单，避免本地数据库与 seed 不一致。

## 4. 推荐测试数据设计

使用固定日期范围，方便验证：

```bash
FROM="2026-06-25"
TO="2026-06-25"
SESSION_PREFIX="token-p0-20260625"
```

建议给每个业务账号上传 2 条 session。admin 可以不上传，也可以上传 1 条，用来验证 admin 个人数据和总监视角兼容。

### 4.1 可计算 token 分配

| employee_id | 团队 | session 数 | total_tokens |
| --- | --- | ---: | ---: |
| `zhangsan` | AI工程 | 2 | 3,000 |
| `lisi` | AI工程 | 2 | 4,000 |
| `wangwu` | AI工程 | 2 | 5,000 |
| `zhaoliu` | AI工程 | 2 | 6,000 |
| `qianqi` | AI工程 | 2 | 7,000 |
| `liu_tl` | AI工程 | 2 | 8,000 |
| `sunba` | 推理加速 | 2 | 9,000 |
| `zhoujiu` | 推理加速 | 2 | 10,000 |
| `zhao_tl` | 推理加速 | 2 | 11,000 |
| `wushi` | 模型训练 | 2 | 12,000 |
| `sun_tl` | 模型训练 | 2 | 13,000 |
| `chen_pm` | 无团队 | 2 | 14,000 |
| `li_director` | 无团队 | 2 | 15,000 |

预期团队汇总：

| group label | 成员 | total_tokens |
| --- | --- | ---: |
| AI工程 | 张三、李四、王五、赵六、钱七、刘TL | 33,000 |
| 推理加速 | 孙八、周九、赵TL | 30,000 |
| 模型训练 | 吴十、孙TL | 25,000 |
| 未分配团队 | 陈PM、李总监 | 29,000 |

全量合计：117,000。

说明：

1. PM 当前后端 token scope 与 director 类似；`/tokens/sessions?scope=team` 对 PM 如果没有 `team_id`，会看到全量。
2. TL 只看自己团队。
3. employee 只看自己。
4. director/admin 看全量。

## 5. 上传执行方案

### 5.1 登录所有账号

使用脚本循环登录：

```bash
login() {
  local employee_id="$1"
  curl -sS "$BASE_URL/auth/login" \
    -H "Content-Type: application/json" \
    --data "{\"employee_id\":\"${employee_id}\",\"password\":\"123\"}"
}
```

记录每个账号返回的 `token` 和 `user`。

### 5.2 受控模拟上传

推荐写一个临时 Node 脚本，例如 `scripts/simulate_session_token_stats.mjs`，流程：

1. 定义账号计划表。
2. 逐个账号调用 `/auth/login`。
3. 根据计划表生成 2 条 session metadata。
4. 使用 `FormData` 上传到 `/sessions/batch`。
5. 记录每个账号上传结果。
6. 上传后调用统计接口并断言预期值。

上传请求伪代码：

```js
const form = new FormData();
form.append("metadata", JSON.stringify({ sessions }));

await fetch(`${BASE_URL}/sessions/batch`, {
  method: "POST",
  headers: { Authorization: `Bearer ${token}` },
  body: form
});
```

session_ref 生成规则：

```text
${SESSION_PREFIX}-${employee_id}-1
${SESSION_PREFIX}-${employee_id}-2
```

重复执行时，同一用户同一 `session_ref` 会 update，不会新增重复 session。

## 6. 验证矩阵

### 6.1 个人账号验证

对每个账号调用：

```bash
GET /tokens/sessions?from=2026-06-25&to=2026-06-25&scope=mine
```

断言：

1. 返回 session 数等于该账号上传数。
2. `sum(total_tokens)` 等于该账号计划 token。
3. 返回数据里的 `user_id` 都等于当前账号。
4. Dashboard 个人 Token 卡片总量与接口聚合一致。

示例：

| employee_id | expected sessions | expected total |
| --- | ---: | ---: |
| `zhangsan` | 2 | 3,000 |
| `liu_tl` | 2 | 8,000 |
| `chen_pm` | 2 | 14,000 |
| `li_director` | 2 | 15,000 |

### 6.2 TL 组内统计验证

对 TL 调用：

```bash
GET /tokens/sessions?from=2026-06-25&to=2026-06-25&scope=team
```

断言：

| TL | 团队 | expected users | expected sessions | expected total |
| --- | --- | ---: | ---: | ---: |
| `liu_tl` | AI工程 | 6 | 12 | 33,000 |
| `zhao_tl` | 推理加速 | 3 | 6 | 30,000 |
| `sun_tl` | 模型训练 | 2 | 4 | 25,000 |

检查点：

1. 返回用户必须都属于 TL 所在团队。
2. 不应包含 PM、director、其它团队成员。
3. Dashboard TL Token 卡片应显示组内总量、session 数、distinct 上报人数。

### 6.3 PM 统计验证

对 PM 调用：

```bash
GET /tokens/sessions?from=2026-06-25&to=2026-06-25&scope=mine
GET /tokens/sessions?from=2026-06-25&to=2026-06-25&scope=team
```

当前后端口径：

1. `scope=mine`：只返回 PM 自己，预期 2 条、14,000。
2. `scope=team`：PM 无 `team_id` 时不会追加团队过滤，实际返回全量。

需要在报告中如实记录：

1. 如果产品期望 PM 看全量，这是正常。
2. 如果产品期望 PM 只看关联需求/负责范围，需要后续新增 PM scope 规则，当前接口不支持。

### 6.4 总监 / admin 组统计验证

对 director/admin 调用：

```bash
GET /tokens/sessions?from=2026-06-25&to=2026-06-25&scope=mine
GET /tokens/sessions?from=2026-06-25&to=2026-06-25&scope=team
GET /tokens?period=range&from=2026-06-25&to=2026-06-25&group_by=team
```

断言：

1. director `scope=mine`：只返回 `li_director` 自己，2 条、15,000。
2. director `scope=team`：后端无 scope 限制，返回全量，预期 26 条、117,000。
3. admin `scope=team`：同 director，返回全量。
4. `group_by=team` 返回四组：
   - AI工程：33,000
   - 推理加速：30,000
   - 模型训练：25,000
   - 未分配团队：29,000
5. `groups[].percent` 合计约 100%，允许浮点误差。

## 7. Dashboard 页面验证

登录不同账号后访问：

```text
http://192.168.14.157:5176/dashboard
```

验证：

1. 不再出现“原型角色”切换。
2. 页面模块由当前登录用户角色决定。
3. employee：
   - Token 卡片只展示个人数据。
   - 不应展示团队分组。
4. TL：
   - Token 卡片展示本组数据。
   - “我的 Token”展示 TL 自己的 `scope=mine` 数据。
5. PM：
   - Token 卡片展示当前后端 PM scope 结果。
   - 需要记录 PM 无 team_id 时是否展示全量。
6. director/admin：
   - Token 卡片展示全量。
   - 各组 Token 使用 `/tokens?group_by=team` 的真实返回。
   - 不展示各组 session_count / uploader_count / coverage，因为现有接口没有这些字段。

## 8. 推荐自动化脚本断言

脚本输出建议包含：

```text
[login] zhangsan ok role=employee team=AI工程
[upload] zhangsan sessions=2 total=3000 ok
[mine] zhangsan sessions=2 total=3000 ok
[team] liu_tl sessions=12 total=33000 users=6 ok
[team] zhao_tl sessions=6 total=30000 users=3 ok
[team] sun_tl sessions=4 total=25000 users=2 ok
[group] director AI工程=33000 推理加速=30000 模型训练=25000 未分配团队=29000 ok
```

失败时输出：

1. 请求 URL。
2. 当前账号。
3. 实际 session ids / user ids。
4. 实际 total 与 expected total。

## 9. 可选：使用本机真实 session list

如果要用本机真实 `.claude` / `.codex` session，而不是受控模拟 metadata：

1. 使用 daemon CLI 查看本机 sessions：

```bash
cd daemon
go build -o aida .
./aida sessions --all
```

2. 对每个账号登录并上传：

```bash
./aida login --server "$BASE_URL" --token "$USER_TOKEN"
./aida upload 1 2
```

注意：

1. 真实 session 的 token 数不容易预先计算，适合做“链路冒烟测试”，不适合做精确断言。
2. 每个账号上传同一批本机 session 时，由于 `session_ref + user_id` 唯一，可以分别归属到不同账号。
3. 如果真实 session 没有 token_usage，Token 统计不会出现预期 token 数。
4. 精确统计测试仍建议使用受控模拟 metadata。

## 10. 清理策略

本方案默认不删除测试数据。

如需清理，只删除本次前缀数据：

```sql
DELETE FROM sessions
WHERE session_ref LIKE 'token-p0-20260625-%';
```

由于 `token_usage.session_id` 设置了 `ON DELETE CASCADE`，删除 session 会自动删除对应 token_usage。

清理前必须确认环境不是生产库。

## 11. 验收标准

1. 所有账号都能登录。
2. 每个账号都能上传至少 2 条带 token_usage 的 session。
3. employee 的 `scope=mine` 只返回自己数据。
4. TL 的 `scope=team` 只返回本团队数据。
5. director/admin 的 `scope=team` 返回全量数据。
6. `/tokens?group_by=team` 返回真实团队分组，不报 UUID 类型错误。
7. Dashboard 不出现原型角色切换。
8. Dashboard Token 卡片不使用 mock 数据。
9. 组统计不展示接口不支持的 coverage、各组 session_count、各组 uploader_count。
10. 测试报告如实记录 PM 当前 scope 口径。
