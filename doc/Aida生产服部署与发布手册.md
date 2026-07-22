# Aida 生产服部署与发布手册

> 本文只适用于生产环境，描述生产部署和部署后运行检查。产品使用与功能测试另行管理。任何测试发布、测试包或测试凭据不得进入本流程。

## 1. 生产架构与目录

生产部署目录为 `/home/luoxian/aida`，固定服务为 `db`、`minio`、`api`、`web`。`daemon/` 只产出用户侧 CLI。

- API、PostgreSQL、MinIO 不直接暴露公网；
- `db`、`minio` volume 不因发布重建；
- API 与 Web 使用各自不可变镜像标签，禁止使用 `latest`；
- 已有生产 Compose、Nginx 和 `.env` 必须先比较再修改，不直接用仓库模板覆盖；
- 密码、JWT 和服务 Token 只存生产 `.env` 或密钥系统。

## 2. 配置契约

首次部署使用仓库中的单端口 Compose 和 Nginx 模板；已有生产环境必须先比较差异，禁止直接覆盖生产配置。

生产 Compose 必须向 API 透传以下配置组：

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

密码、JWT 和服务 Token 只能保存在服务器 `.env` 或密钥系统。普通升级不得改变数据库、MinIO 或 JWT 凭证。

Report Skill/MCP 的 slug、MCP 协议版本、凭据槽、默认 Agent 文案，以及 Digest 的读取模式、算法版本、脱敏版本、容量预算和 Worker 参数由 API 镜像固化。需要调整时通过代码、测试和新 API 镜像发布，不在生产临时拼装运行组合。

## 3. 首次部署

1. 安装 Docker Engine、Compose v2，并配置镜像仓库访问；
2. 创建生产 `.env`，填写本节全部必需配置；
3. 比较并确认 Compose 和 Nginx 配置；
4. 启动 `db`、`minio`、`api`、`web`；
5. 确认 API 完成尚未应用的正向 migration；
6. 检查 `/health`、数据库、MinIO、模型目录和 Managed Agent Platform；
7. 发布并核对生产 Report Skill、MCP 和默认 Agent。

首次部署不得使用测试 Skill owner、测试 API 地址或测试 CLI 包。

## 4. 完整发布门禁

每次生产发布必须复制 [`完整生产发布清单模板`](发布事项/完整生产发布清单模板.md)，逐项声明：API、Web、migration、历史数据、清理、Report Skill、MCP/默认 Agent、CLI、MinIO、Digest/报告链路和文档配置是“发布、仅核对、不涉及或阻断”。

标准顺序：

1. 冻结源码提交和发布范围；
2. 记录当前容器、镜像、配置、migration、Skill/MCP 和 CLI 分发；
3. 完整备份 PostgreSQL、配置和回退镜像；
4. 构建并推送不可变 API/Web 镜像；
5. 先更新 API 并执行正向 migration；
6. 验证 Digest、队列和报告读取；
7. 发布或修复 Skill/MCP/默认 Agent；
8. 更新 Web；
9. 构建并发布生产 CLI，最后切换生产版本发现文件；
10. 完成服务状态、接口、数据和发布产物检查，填写最终发布判定。

任一阻断项未关闭时禁止生产发布。

## 5. 数据库与服务规则

- migration 只允许正向执行，已应用文件不得改名、改内容或复用编号；
- migration 必须先在空库和生产数据副本验证；
- 生产失败优先 forward fix，不反向删除已落地字段或约束；
- API-only/Web-only 只能替换目标服务，禁止共享镜像标签导致另一服务被更新；
- 禁止 `docker compose down -v`、清卷、无记录清库、`DROP` 或 `TRUNCATE`。

## 6. Report Skill、MCP、Agent 与 Digest

生产 Report Skill 使用生产 owner 下唯一、不可变、可解析的 `aida-report` 版本。新版本通过 owner-qualified derive 接口发布，正文使用 multipart 字段 `file:SKILL.md`，并核对返回 owner、slug、version、sha256 与 public registry 正文 hash。

禁止：

- 用普通 `POST /api/skill` 代替 owner-qualified derive；
- 覆盖同版本或直接删除平台数据库记录；
- 在 `.env` 临时拼装与 API 镜像不一致的 MCP、Skill 或 Digest 协议；
- 使用管理员 Token 批量改写所有用户的默认 Agent。

发布新 Skill 版本的顺序固定为：

1. 从当前 API 生成本次 Skill Markdown；
2. 使用 owner-qualified derive 接口发布；
3. 以当前生产版本为 `base_version`，使用新的不可变 `version`；
4. 查询 public registry，确认 owner、slug、version 和正文哈希；
5. 更新 API 的 `MANAGED_AGENT_REPORT_SKILL_VERSION`；
6. 重启 API 并核对默认 Agent 的 SkillRef。

owner-qualified derive 使用 `multipart/form-data`，正文必须通过字段名 `file:SKILL.md` 上传：

```bash
skill_file=/tmp/aida-report-SKILL.md
managed_agent_url=<MANAGED_AGENT_URL>
managed_agent_token=<MANAGED_AGENT_TOKEN>

curl -fsS -X POST \
  -H "Authorization: Bearer ${managed_agent_token}" \
  --form-string 'base_version=<当前生产版本>' \
  --form-string 'version=<新不可变版本>' \
  --form-string 'name=Aida Report Skill' \
  --form-string 'description=<本次 Skill 说明>' \
  -F "file:SKILL.md=@${skill_file};filename=SKILL.md" \
  "${managed_agent_url}/api/skill/10086/aida-report/derive"
```

接口成功后核对 owner、slug、version 和 sha256，并从 public registry 读取正文重新计算 SHA256。禁止用普通 `POST /api/skill` 代替 owner-qualified derive，禁止覆盖同版本或直接删除平台数据库记录。

MCP URL、认证头、凭据槽和工具集合必须与当前 API 镜像契约一致。改变 MCP URL 或协议时，提升 MCP 版本并发布新 API 镜像，不修改已发布版本。

Digest 固定规则：

- 新报告使用 `digest_v2`，不通过生产环境变量临时切换读取模式；
- Digest 算法版本、脱敏版本、容量预算和 Worker 参数随 API 镜像发布；
- Digest 未 ready 时不得让新报告进入不可恢复的半完成状态；
- Digest 异常时停止新报告并恢复上一 API 镜像或发布 forward fix。

## 7. 生产 CLI 发布

CLI 版本取根目录 `VERSION`，必须使用生产 target：

```bash
make release-prod-dir \
  AIDA_RELEASE_URL=<生产静态文件地址> \
  AIDA_API_URL=<生产公网 API 地址> \
  AIDA_INTERNAL_API_URL=<生产内网 API 地址>
```

禁止把 `release-test-dir` 产物复制到生产。生产包必须包含三个平台二进制、两个安装脚本、`SHA256SUMS.txt` 和 `aida-latest.txt`。

上传顺序固定为：二进制 → 安装脚本 → 校验清单 → 从生产 URL 下载复验 → 最后切换版本发现文件 → 再次完整下载校验。

版本发现文件切换后，已升级客户端不会因降低版本号自动回退；需要更高版本 forward fix 或明确的人工重装方案。

## 8. 生产部署后检查

- `db`、`minio`、`api`、`web` 容器和 `/health` 正常；
- migration、数据库数据量和关键约束与发布记录一致；
- API/Web 实际容器使用记录的不可变镜像 digest；
- Skill 在 public registry 可解析，默认 Agent owner/slug/version 正确；
- MCP 使用当前用户 Token 可调用；
- Digest ready/failed、上传 abort、API 5xx、队列和慢查询无异常；
- CLI 从生产地址下载后 hash、版本、内外网路由和安装脚本地址正确；
- 观察窗口结束后发布记录明确标为“已发布”“未发布/阻断”或“已回滚”。

## 9. 回退与止损

- Web 恢复上一不可变镜像；
- API 在无新数据兼容风险时评估恢复，否则 forward fix；
- migration 保留已执行结构，不反向 DROP；
- Skill/MCP 保留历史不可变版本，恢复 API 配置引用；
- CLI 在切换版本发现文件前可停止，切换后按生产发布记录执行 forward fix 或人工重装；
- 任何数据清理必须单独备份、审计和验收，不随普通发布隐式执行。

出现生产持续 5xx、数据库锁异常、队列 dead、对象 hash 不一致、数据对账不平、旧 Claude/Codex 主链路回归或无法确认恢复边界时立即停止。

## 10. 文档维护

- 本文只维护长期生产部署规则和配置契约；
- `doc/发布事项/` 保存每次生产发布的版本、镜像、migration、备份路径和执行结果；
- `doc/v2/bug清单/` 保存问题、根因和修复状态；
- 普通版本号、镜像标签、备份目录和单次执行结果不得回写本文。
