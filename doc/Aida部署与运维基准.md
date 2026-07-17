# Aida 部署与运维基准

> 本文是 Aida 测试环境、生产环境和日常升级的长期基准。版本号、镜像标签、迁移区间、Skill 版本、测试账号、run ID 和备份路径不写入本文；这些内容记录在 `doc/v2/发布事项/` 下的单次发布记录中。

## 1. 适用范围

本文只规定：部署架构、配置契约、首次部署、标准升级、迁移、Skill/MCP/Agent 资源发布、CLI 发布、备份、回滚和固定验收。

部署架构或长期流程发生变化时更新本文；普通版本发布不得修改本文。

## 2. 生产架构

生产目录：`/home/luoxian/aida`。测试开发机：`ssh 157`，代码目录 `/home/intellif/dev/project_manager`。

生产服务固定为：

| 服务 | 职责 |
| --- | --- |
| `db` | PostgreSQL 16，持久化业务数据 |
| `minio` | Session、发布产物和 CLI 文件 |
| `api` | API、数据库迁移、报告、Digest 和后台处理 |
| `web` | Web SPA 与统一 Nginx 入口 |

`daemon/` 产出 Aida CLI，不作为生产 consumer 运行。

```text
浏览器 / Aida CLI -> web/Nginx:80
                       ├─ /api/v1/       -> api:8080
                       ├─ /health        -> api:8080
                       ├─ /statics-live/ -> minio:9000/statics-live/
                       └─ /              -> Web SPA

api -> PostgreSQL / MinIO / Managed Agent Platform
Report Agent -> /api/v1/mcp/reports
```

固定要求：

- API、PostgreSQL、MinIO 不直接暴露公网；
- `db`、`minio` volume 不因发布重建；
- Nginx 保持大文件上传和长请求超时配置；
- API 与 Web 分别使用不可变镜像标签，禁止生产使用 `latest`；
- `mc` 只通过 Compose `tools` profile 临时运行。

## 3. 部署文件和配置契约

首次部署使用仓库中的单端口 Compose 和 Nginx 模板；已有生产环境必须先比较差异，禁止直接覆盖生产配置。

生产 Compose 必须包含 `db`、`minio`、`api`、`web`，并向 API 透传以下配置组：

```env
PUBLIC_BASE_URL=<公网入口>
IMAGE_REGISTRY=<镜像仓库>
API_IMAGE_TAG=<本次 API 不可变标签>
WEB_IMAGE_TAG=<本次 Web 不可变标签>
TZ=Asia/Shanghai

AI_GATEWAY_MODELS_URL=<模型目录接口>
AIDA_CLAUDE_CACHE_WRITE_VARIANT=5m

MANAGED_AGENT_URL=<Managed Agent Platform 地址>
MANAGED_AGENT_TOKEN=<生产管理 Token>
MANAGED_AGENT_DEFAULT_ENGINE=claude-code
MANAGED_AGENT_DEFAULT_MODEL_ID=<生产默认模型>
MANAGED_AGENT_REPORT_SKILL_OWNER=10086
MANAGED_AGENT_REPORT_SKILL_VERSION=<当前生产不可变版本>
MANAGED_AGENT_REPORT_MCP_URL=<公网或内网 MCP 地址>
```

密码、JWT 和服务 Token 只能保存在服务器 `.env` 或密钥系统，不写入仓库、文档和日志。普通升级不得改变数据库、MinIO 或 JWT 凭证。

Report Skill/MCP 的 slug、MCP 协议版本、凭据槽、默认 Agent 文案、资产修复策略，以及 Digest 的读取模式、算法版本、脱敏版本、容量预算、Worker 开关和批量均由 API 镜像固化，不写入 `.env`。这些值属于代码兼容性契约；需要调整时通过代码评审、测试和新 API 镜像发布，不能在生产临时拼装一套运行组合。

生产 Compose 会从 `PUBLIC_BASE_URL` 派生 API 内部使用的 Aida 公网地址，不需要再维护第二个同义配置项。

## 4. 首次部署

1. 安装 Docker Engine、Compose v2，并配置镜像仓库访问。
2. 创建生产 `.env`，填入本节配置契约中的全部必需项。
3. 检查 Compose 服务和 Nginx 配置。
4. 启动 `db`、`minio`、`api`、`web`。
5. API 自动执行尚未应用的正向迁移。
6. 检查 `/health`、数据库连接、MinIO、模型目录和 Managed Agent Platform。
7. 发布并核对生产 Report Skill、MCP 和默认 Agent。

首次部署不得使用测试 Skill owner、测试 API 地址或测试 CLI 包。

## 5. 标准升级流程

单次发布的具体值填写在 `doc/v2/发布事项/YYYYMMDD-*.md`，本文只规定顺序：

1. 冻结本次范围内 API、Web 和 CLI 的源码提交，排除备份及临时文件。
2. 记录生产当前容器、配置和数据库状态。
3. 备份 PostgreSQL 并校验 SHA256。
4. 分别用不可变标签构建并推送本次范围内的 API/Web 镜像。
5. 先更新 API，执行正向迁移并检查健康状态。
6. 完成 Digest/后台数据准备后，确认所有新报告使用规定的读取模式。
7. 发布或修复 Report Skill、MCP 和默认 Agent 绑定。
8. 更新 Web。
9. 构建并发布 CLI，最后再切换 CLI 版本发现文件。
10. 执行固定验收并填写单次发布记录。

API-only 或 Web-only 发布只能更新并重建指定服务；`API_IMAGE_TAG` 与 `WEB_IMAGE_TAG` 必须独立维护，禁止使用共享标签导致另一服务被意外替换。

## 6. 数据库迁移规则

- 迁移文件位于 `api/db/migrations/`，按数字顺序执行；
- 生产只执行正向迁移，不执行 down migration；
- 已执行迁移不得改名、改内容或复用编号；
- 迁移必须先在空库和生产数据副本验证，再进入生产；
- 生产迁移失败时保留现场，优先发布 forward fix；
- 14.157 是开发测试环境，允许清库重建，但不能用测试库版本反推生产版本；
- 每次发布记录只填写本次 `from -> to`，本文不保存当前最高迁移号。

## 7. Report Skill、MCP 和默认 Agent

### 7.1 Skill 发布顺序

生产 Report Skill 必须是 public registry 中唯一、未归档、可解析的：

```text
10086/aida-report@<version>
```

发布新版本必须按以下顺序：

1. 从当前 API 生成本次 Skill Markdown；
2. 使用平台 owner-qualified derive 接口发布：

   ```text
   POST /api/skill/{owner}/aida-report/derive
   ```

3. 以现有生产版本为 `base_version`，使用新的不可变 `version`；
4. 查询 public registry，确认 owner、slug、version 唯一且正文哈希正确；
5. 再更新 API 的 `MANAGED_AGENT_REPORT_SKILL_VERSION`；
6. 重启 API；
7. 触发默认 Report Agent 创建或运行，确认 Agent 引用新 Skill。

禁止直接使用普通 `POST /api/skill` 代替 owner-qualified derive；普通接口可能把 Skill 发布到当前管理 Token 的 owner，导致生产 API 找不到配置的 `10086` Skill。禁止覆盖同版本或直接删除平台数据库记录。

### 7.2 MCP 发布

MCP 版本必须在 public registry 中唯一，URL、认证头、凭据槽和工具集合必须与 API 镜像契约一致。变更 MCP URL 或协议时，在代码中提升 MCP 版本并发布新 API 镜像，不修改已发布版本，也不通过 `.env` 覆盖协议版本。

### 7.3 Digest 规则

- `digest_v2` 是新报告的固定读取模式，不提供 rollout、canary、shadow 或回退到 `full` 的环境开关；
- Digest 算法版本、脱敏版本、容量预算和 Worker 参数随 API 镜像发布，测试与生产使用同一套代码策略；
- Digest 未 ready 时不得让新报告进入不可恢复的半完成状态；
- Digest 异常时停止新报告并回滚上一 API 镜像或 forward fix，禁止通过修改 `.env` 绕过容量保护。

## 8. Aida CLI 发布

CLI 版本取仓库根目录 `VERSION`。生产包必须同时固化公网回退地址、内网优先地址和发布地址：

```bash
make release-prod-dir \
  AIDA_RELEASE_URL=<生产静态文件地址> \
  AIDA_API_URL=<公网 API 地址> \
  AIDA_INTERNAL_API_URL=<内网 API 地址>
```

发布目录必须包含三个平台二进制、安装脚本、`SHA256SUMS.txt` 和版本发现文件。上传顺序固定为：

```text
二进制 -> 安装脚本 -> SHA256SUMS.txt -> 下载校验 -> 版本发现文件
```

版本发现文件是自动更新开关，必须最后上传。已安装旧客户端不能通过降低版本号自动回滚；需要发布更高修复版本或人工重装。

## 9. 备份、回滚和止损

发布前至少备份：

- PostgreSQL 完整备份；
- `.env`、Compose、Nginx 配置；
- 当前 API/Web 镜像标签和 digest；
- 当前 Skill/MCP 版本及哈希。

回滚规则：

- Web 可恢复上一镜像；
- API 优先 forward fix，不能反向删除已执行迁移；
- Digest 异常时恢复上一不可变 API 镜像，不临时改写读取模式或算法参数；
- Skill 保留历史版本，不覆盖、不删除；
- CLI 版本发现文件只能阻止后续升级，不能让已升级客户端自动降级。

禁止执行 `docker compose down -v`、清卷、清库、`DROP`、`TRUNCATE` 或无记录的业务数据删除。

## 10. 固定验收

每次发布都要确认：

- `db`、`minio`、`api`、`web` 均正常运行；
- `/health` 返回成功；
- 数据库迁移达到发布记录指定版本；
- API/Web 镜像标签均为各自记录的不可变标签且不是 `latest`；
- Report Skill 在 public registry 可解析，默认 Agent 引用正确 owner/slug/version；
- MCP 工具可调用，凭据槽和当前用户 Token 正常；
- Digest ready/failed、上传 abort、API 5xx 无异常积压；
- CLI 下载文件可校验，内网/公网路由符合配置；
- 发布后的实际结果记录到对应的单次发布文档。

## 11. 文档维护规则

- 本文只维护长期部署规则和配置契约；
- `doc/v2/发布事项/` 维护每次发布的版本值、命令、验收和回退记录；
- `doc/v2/bug清单/` 维护问题和修复方案；
- 任何新增生产组件、改变网络拓扑、改变迁移策略或改变 Skill/MCP 发布方式时，才修改本文；
- 版本升级、镜像更新、Skill 补丁和单次测试结果不得回写本文。
