# AI Coding Console 部署文档

## 1. 文档目的

本文档用于部署 Aida / AI Coding Console 的服务端环境，适用于：

- 人工按步骤执行部署
- agent 按文档编排自动化部署
- 日常升级、回滚、验证和 CLI 发布

目标是让部署过程可重复、可验证、可维护，而不是依赖临时口头说明。

---

## 2. 部署范围

当前系统包含 5 个运行组件：

1. `db`：PostgreSQL 16
2. `minio`：对象存储，用于原始日志与 CLI 安装包托管
3. `api`：后端 API
4. `web`：前端页面
5. `consumer`：日报 / 周报生成服务

其中：

- `api`、`web`、`consumer` 为业务服务
- `db`、`minio` 为基础依赖

---

## 3. 架构说明

部署后整体关系如下：

```text
用户浏览器 --> web --> api --> PostgreSQL
                      |--> MinIO
                      |--> consumer --> Claude CLI

开发机 / 员工机器 --> aida CLI --> 内网 API（可达时优先）
                                \-> 外网 API（自动回退）
```

关键说明：

- `web` 对外提供页面访问入口
- `api` 负责业务接口、鉴权、需求任务、Token、报表相关逻辑
- `consumer` 由 `api` 通过 `REPORT_GENERATOR_URL` 调用，用于生成日报 / 周报草稿
- `consumer` 需要复用宿主机上的 Claude 登录态，因此必须挂载宿主机 `~/.claude`

---

## 4. 前置条件

## 4.1 服务器要求

建议至少满足：

- Linux 服务器
- 已安装 Docker Engine
- 已安装 Docker Compose v2
- 具备 root 或可执行 Docker 管理操作的权限
- 服务器可访问镜像仓库 `192.168.14.129:80`

## 4.2 网络与端口规划

当前推荐部署为单公网入口：外网只开放服务器 `80` 端口，其余服务只在 Docker 内网互通。

以服务器公网入口 `113.100.143.91:9180` 为例：

| 访问对象 | 对外地址 | 说明 |
|---|---|---|
| Web | `http://113.100.143.91:9180/` | 浏览器页面入口 |
| 外网 API | `http://113.100.143.91:9180/api/v1` | 前端入口，也是 `aida` CLI 的稳定回退入口 |
| 公司内网 API | `http://192.168.14.182/api/v1` | `aida` CLI 在公司网络内优先使用，避免占用外网小带宽 |
| 健康检查 | `http://113.100.143.91:9180/health` | 由 Web Nginx 反代到 API |
| CLI 安装包 | `http://113.100.143.91:9180/statics-live/aida/` | 由 Web Nginx 反代到 MinIO |

内部端口仍然存在但不映射到公网：

| 服务 | 容器端口 | 宿主机端口 | 说明 |
|---|---:|---:|---|
| Web | 80 | 80 | 唯一公网入口 |
| API | 8080 | 不暴露 | 只允许 Web Nginx 和容器内服务访问 |
| PostgreSQL | 5432 | 不暴露 | 生产数据服务，不对外开放 |
| MinIO API | 9000 | 不暴露 | 通过 `/statics-live/` 下载静态包 |
| MinIO Console | 9001 | 不暴露 | 如需管理，建议 SSH 隧道临时访问 |
| Consumer | 8090 | 不暴露 | 仅 API 内部调用 |

## 4.3 Claude 登录前置条件

`consumer` 依赖 Claude CLI 生成日报 / 周报，因此部署完成后必须在宿主机执行一次 Claude 登录。

要求：

- 宿主机存在可用的 `~/.claude`
- `consumer` 通过 volume 挂载该目录到容器内 `/root/.claude`

如果未登录 Claude：

- 服务可以启动
- 但日报 / 周报生成会失败

---

## 5. 镜像信息

当前镜像仓库：

```text
192.168.14.129:80/aied
```

当前版本标签示例：

```text
20260626
```

对应镜像：

```text
192.168.14.129:80/aied/ai-coding-console-api:20260626
192.168.14.129:80/aied/ai-coding-console-web:20260626
192.168.14.129:80/aied/ai-coding-console-consumer:20260626
```

## 5.1 标签约定

每个业务镜像固定打两个标签：

- 日期标签 `YYYYMMDD`
- `latest`

建议：

- 部署文件一律使用日期标签
- `latest` 仅用于临时调试或人工检查

原因：

- 日期标签可精确回滚
- 避免 `latest` 漂移导致环境不一致

## 5.2 私库是 HTTP 仓库

当前私库 `192.168.14.129:80` 走 HTTP，而不是 HTTPS。

如果目标服务器 Docker 默认按 HTTPS 拉取，会报错：

```text
http: server gave HTTP response to HTTPS client
```

解决方式：

1. 修改 `/etc/docker/daemon.json`
2. 加入 insecure registry 配置
3. 重启 Docker

示例：

```json
{
  "insecure-registries": ["192.168.14.129:80"]
}
```

重启：

```bash
systemctl restart docker
```

验证：

```bash
docker info | grep -A5 "Insecure Registries"
```

如果目标机无法改 Docker 配置，可改用离线导入方案，见第 8 章。

---

## 6. 从源码构建并推送镜像

如果你不是直接使用现成镜像，而是要从当前仓库发布新版本，可在源码根目录执行：

```bash
REG=192.168.14.129:80/aied
TAG=$(date +%Y%m%d)

docker build -t $REG/ai-coding-console-api:$TAG      -t $REG/ai-coding-console-api:latest      ./api
docker build -t $REG/ai-coding-console-web:$TAG      -t $REG/ai-coding-console-web:latest      ./web
docker build -t $REG/ai-coding-console-consumer:$TAG -t $REG/ai-coding-console-consumer:latest ./daemon

for repo in ai-coding-console-api ai-coding-console-web ai-coding-console-consumer; do
  for t in $TAG latest; do
    docker push $REG/$repo:$t
  done
done
```

发布后需要同步更新部署文件中的镜像标签。

---

## 7. 标准部署流程

本章给出推荐的标准部署方式：单机 Docker Compose 部署。

## 7.1 目录准备

建议在目标服务器准备独立部署目录，例如：

```bash
mkdir -p /data/ai-coding-console
cd /data/ai-coding-console
```

建议在该目录下保存：

- `docker-compose.yml`
- `.env`（如需要）
- 运维记录、备份脚本、升级脚本

## 7.2 部署版 `docker-compose.yml`

仓库已提供单端口部署模板：

```text
deploy/docker-compose.single-port.yml
```

服务器部署时复制为部署目录里的 `docker-compose.yml`：

```bash
cp deploy/docker-compose.single-port.yml /data/ai-coding-console/docker-compose.yml
cp deploy/nginx.conf /data/ai-coding-console/nginx.conf
cd /data/ai-coding-console
```

建议在部署目录创建 `.env`，固定服务器地址、镜像标签和密码：

```bash
cat > .env <<'EOF'
PUBLIC_BASE_URL=http://113.100.143.91:9180
IMAGE_REGISTRY=192.168.14.129:80/aied
IMAGE_TAG=20260626

POSTGRES_PASSWORD=请替换为强密码
MINIO_ROOT_PASSWORD=请替换为强密码
JWT_SECRET=请替换为强随机字符串
CLAUDE_HOME=/home/intellif/.claude
AIDA_BOOTSTRAP_ADMIN_UIDS=1
TZ=Asia/Shanghai
EOF
```

这份模板只有 `web` 映射 `80:80`，`api`、`db`、`minio`、`consumer` 都不暴露宿主机端口。`nginx.conf` 会挂载到 Web 容器内，用于保留外网端口并统一反代 `/api/v1/`、`/health`、`/statics-live/`。

## 7.3 关键配置说明

### `JWT_SECRET`

- 必须修改
- 生产环境不要使用默认值
- 建议使用高强度随机字符串

### `CORS_ORIGIN`

- 单端口部署填写 `PUBLIC_BASE_URL`，例如 `http://113.100.143.91:9180`
- 如果使用域名，填写域名地址，例如 `http://aida.example.com`
- 如果多个来源，按后端支持格式配置

### `AIDA_PUBLIC_BASE_URL`

- 必须是外部可访问的 Web 根地址，例如 `http://113.100.143.91:9180`
- 报告 Agent 会用它生成 MCP 地址：`http://113.100.143.91:9180/api/v1/mcp/reports`

### `AIDA_BOOTSTRAP_ADMIN_UIDS`

- 填 AIHub 用户 ID，不是登录名
- 当前默认管理员 `admin` 的 AIHub 用户 ID 为 `1`，因此填写 `AIDA_BOOTSTRAP_ADMIN_UIDS=1`

### `MINIO_EXTERNAL_ENDPOINT`

- 单端口部署填写 `PUBLIC_BASE_URL`，例如 `http://113.100.143.91:9180`
- CLI 安装包通过 Web Nginx 的 `/statics-live/` 反代访问 MinIO，不再对外开放 `9000`

### `REPORT_GENERATOR_URL`

- 必须指向 `consumer`
- 通常使用容器内服务名：`http://consumer:8090`

### `~/.claude` 挂载

- 必须确认宿主机目录真实存在
- 必须确保部署用户在宿主机完成过 Claude 登录

## 7.4 启动服务

在部署目录执行：

```bash
docker compose pull
docker compose up -d
```

查看状态：

```bash
docker compose ps
```

查看日志：

```bash
docker compose logs -f api
docker compose logs -f web
docker compose logs -f consumer
docker compose logs -f db
docker compose logs -f minio
```

---

## 8. 离线导入部署

如果目标服务器不能直接从私库拉镜像，可在一台能访问私库的机器执行：

```bash
docker pull 192.168.14.129:80/aied/ai-coding-console-api:20260626
docker pull 192.168.14.129:80/aied/ai-coding-console-web:20260626
docker pull 192.168.14.129:80/aied/ai-coding-console-consumer:20260626

docker save \
  192.168.14.129:80/aied/ai-coding-console-api:20260626 \
  192.168.14.129:80/aied/ai-coding-console-web:20260626 \
  192.168.14.129:80/aied/ai-coding-console-consumer:20260626 \
  | gzip > ai-coding-console-images-20260626.tar.gz

scp ai-coding-console-images-20260626.tar.gz <用户>@<服务器IP>:/tmp/
```

在目标服务器导入：

```bash
gunzip -c /tmp/ai-coding-console-images-20260626.tar.gz | docker load
docker images | grep ai-coding-console
```

之后继续执行：

```bash
docker compose up -d
```

---

## 9. 首次部署后的初始化与验证

## 9.1 检查服务可用性

### 前端页面

```text
http://113.100.143.91:9180/
```

### API

```text
http://113.100.143.91:9180/health
http://113.100.143.91:9180/api/v1
```

### CLI 安装包

```text
http://113.100.143.91:9180/statics-live/aida/install.sh
```

单端口部署不对公网开放 MinIO Console。如果必须管理 MinIO，建议通过 SSH 隧道或临时内网访问处理，不要长期暴露 `9001`。

## 9.2 默认账号

当前数据库迁移中，内置账号密码已统一重置为：

```text
工号：admin
密码：123
```

说明：

- 旧文档中提到的 `Admin@123!` 已失效
- 当前镜像初始化后以迁移 `008_builtin_password_123.sql` 的结果为准

建议首次登录后立即执行：

1. 修改 `admin` 密码
2. 替换 `JWT_SECRET`
3. 评估是否保留默认内置用户

## 9.3 验证数据库迁移

进入数据库：

```bash
docker compose exec db psql -U aidashboard -d aidashboard
```

检查迁移记录：

```sql
select * from schema_migrations order by version;
```

重点确认包含：

- `005_user_auth.sql`
- `007_requirements_p0.sql`
- `016_requirement_task_versions.sql`

## 9.4 验证 Claude 报表能力

在宿主机确认 Claude 已登录后，检查 consumer 日志：

```bash
docker compose logs --tail=200 consumer
```

如果报表生成失败，优先检查：

- `/home/<部署用户>/.claude` 是否存在
- volume 挂载路径是否正确
- 宿主机是否完成 Claude 登录

---

## 10. 升级流程

推荐升级顺序如下：

1. 备份数据库
2. 更新镜像标签
3. 拉取新镜像
4. 重启服务
5. 验证核心能力

示例：

```bash
cd /data/ai-coding-console

# 1. 备份数据库
docker compose exec -T db pg_dump -U aidashboard aidashboard > backup_$(date +%Y%m%d_%H%M%S).sql

# 2. 修改 docker-compose.yml 中的镜像 tag

# 3. 拉取新镜像
docker compose pull

# 4. 重启服务
docker compose up -d

# 5. 验证
docker compose ps
docker compose logs --tail=100 api
docker compose logs --tail=100 web
docker compose logs --tail=100 consumer
```

升级后至少验证：

- 页面能正常打开
- admin 能登录
- 需求列表能正常加载
- Dashboard 能正常加载
- consumer 无明显启动错误

---

## 11. 回滚流程

如果新版本异常，按旧标签回滚：

1. 将 `docker-compose.yml` 中镜像 tag 改回上一版本
2. 拉取或导入旧镜像
3. 重新 `docker compose up -d`

示例：

```bash
docker compose pull
docker compose up -d
docker compose ps
```

注意：

- 如果新版本已经执行了不可兼容的数据迁移，单纯回滚镜像可能不够
- 因此升级前的数据库备份必须保留

---

## 12. CLI 安装包发布

`aida` CLI 的 Linux / macOS Apple Silicon / Windows 安装包可直接发布到本机 MinIO，无需额外静态文件服务器。

## 12.1 构建发布包

仓库根目录执行：

### 测试包

```bash
make release-test-dir
```

产物目录：

```text
./aida-releases-test/
```

该命令会生成：

- `install.sh`
- `install.ps1`
- `aida-linux-amd64`
- `aida-darwin-arm64`
- `aida-windows-amd64.exe`
- `aida-latest.txt`
- `SHA256SUMS.txt`

### 正式包

正式包必须传入最终对外地址：

```bash
make release-prod-dir \
  AIDA_RELEASE_URL=http://113.100.143.91:9180/statics-live/aida \
  AIDA_API_URL=http://113.100.143.91:9180/api/v1 \
  AIDA_INTERNAL_API_URL=http://192.168.14.182/api/v1
```

产物目录：

```text
./aida-releases-release/
```

说明：

- 测试包固定使用测试地址 `http://192.168.14.157:9000/statics-live/aida`
- 正式包必须传入实际服务器地址
- 安装脚本会把外网发布地址、外网 API 和内网 API 候选地址固化进去
- 外网 API 始终是稳定回退地址，不能被内网地址替换

## 12.2 上传到 MinIO

假设你把生成目录拷贝到了服务器 `/tmp/aida-releases`：

```bash
docker compose --profile tools run --rm -v /tmp/aida-releases:/data:ro mc '
  mc alias set local http://minio:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD"
  mc mb -p local/statics-live 2>/dev/null || true
  mc cp /data/aida-linux-amd64 /data/aida-darwin-arm64 /data/aida-windows-amd64.exe local/statics-live/aida/
  mc cp /data/install.sh /data/install.ps1 /data/SHA256SUMS.txt local/statics-live/aida/
  mc cp /data/aida-latest.txt local/statics-live/aida/aida-latest.txt
  mc anonymous set download local/statics-live/aida
'
```

说明：

- bucket 名为 `statics-live`
- 发布前缀为 `aida/`
- 命令会将该目录设置为匿名只读下载
- `aida-latest.txt` 必须最后上传，它是客户端开始拉取新版本的发布开关

## 12.3 CLI 安装命令

### Linux / macOS Apple Silicon

```bash
curl -fsSL http://113.100.143.91:9180/statics-live/aida/install.sh \
  | AIDA_TOKEN=<用户JWT> bash
```

`install.sh` 会按当前系统选择二进制：Linux x86_64 下载 `aida-linux-amd64`，macOS arm64 下载 `aida-darwin-arm64`。

### Windows

```powershell
$env:AIDA_TOKEN="<用户JWT>"; Invoke-RestMethod http://113.100.143.91:9180/statics-live/aida/install.ps1 | Invoke-Expression
```

如果不带 `AIDA_TOKEN`，安装后可手动登录：

```bash
aida login --server http://113.100.143.91:9180/api/v1 --token <jwt>
```

Windows 注意事项：

- 直接在当前 PowerShell 会话中执行 `Invoke-RestMethod ... | Invoke-Expression`
- 不要再套一层 `powershell -Command`
- 否则 PATH 刷新只会发生在子进程里

安装后的 `~/.aida.yaml`（Windows 为 `%USERPROFILE%\.aida.yaml`）包含：

```yaml
api_url: http://113.100.143.91:9180/api/v1
internal_api_url: http://192.168.14.182/api/v1
auto_route: true
release_url: http://113.100.143.91:9180/statics-live/aida
auto_update: true
```

`aida` 在真正上传前使用当前 Token 请求内网 `/auth/me`，1.2 秒内验证成功就走内网；连接失败、超时、鉴权失败或响应不是合法 JSON 时自动使用外网。`AIDA_API_URL` 或 `aida login --server` 的显式地址优先，不执行自动切换。可用 `aida status` 查看本次实际选择的入口。

## 12.4 CLI 自动更新

安装脚本只负责首次安装和从旧版本切换到支持自更新的版本。完成这一次升级后，用户不需要在每次发布时重新执行 `curl | bash`。

客户端行为：

1. 用户正常执行 `aida sessions`、`aida upload`、`aida status` 或 `aida login`；
2. 客户端每天最多检查一次外网 `aida-latest.txt`；
3. 发现更高版本后下载当前平台二进制；
4. 使用同一发布目录的 `SHA256SUMS.txt` 校验；
5. Linux/macOS 原子替换，Windows 在当前进程退出后完成替换；
6. 本次命令继续运行，下一次命令使用新版本。

人工立即检查可执行：

```bash
aida update
aida version
```

关闭自动更新可在配置中设置：

```yaml
auto_update: false
```

发布新 CLI 时必须同时上传三个平台二进制、`aida-latest.txt` 和完整的 `SHA256SUMS.txt`，最后再上传 `aida-latest.txt`。这样客户端不会在二进制尚未上传完成时读到新版本。严禁只替换二进制而不更新校验文件。

当前生产下载地址是 HTTP。SHA256 可以发现损坏或发布文件不一致，但不能抵御主动网络劫持；生产应尽快迁移到 HTTPS 或增加离线签名验证。

已有旧客户端尚无 `release_url` 和自动路由配置，需要在本功能首次发布时执行最后一次安装命令，或者由运维批量写入上述配置。此后正常发布不再要求用户重复安装。

## 12.5 单端口 Nginx 入口

`web` 镜像内置 Nginx 已配置：

- `/api/v1/` 反代到 `api:8080`
- `/health` 反代到 `api:8080`
- `/statics-live/` 反代到 `minio:9000/statics-live/`

因此服务器不需要额外维护一份宿主机 Nginx 配置；只要使用 `deploy/docker-compose.single-port.yml` 并对外开放 `80` 即可。

---

## 13. 运维建议

生产环境建议至少做以下加固：

1. 修改 `JWT_SECRET`
2. 修改 MinIO 默认账号密码
3. 修改 PostgreSQL 默认密码
4. 修改 `admin` 默认密码
5. 对外使用固定域名，而不是裸 IP
6. 对 Web / API / MinIO 增加反向代理和访问控制
7. 定期备份 PostgreSQL 与 MinIO 数据

---

## 14. 常见问题

## 14.1 拉镜像时报 HTTPS / HTTP 冲突

现象：

```text
http: server gave HTTP response to HTTPS client
```

原因：

- 私库是 HTTP
- Docker 默认按 HTTPS 访问

处理：

- 配置 `insecure-registries`
- 或使用离线导入方式

## 14.2 consumer 启动正常，但日报生成失败

优先检查：

1. 宿主机是否已经 Claude 登录
2. `/home/<部署用户>/.claude` 是否存在
3. volume 挂载路径是否写对
4. `REPORT_GENERATOR_URL` 是否指向 `http://consumer:8090`

## 14.3 页面能打开，但接口 401 或登录异常

优先检查：

1. `JWT_SECRET` 是否在 API 重启前后发生不一致
2. 浏览器访问地址是否与 `CORS_ORIGIN` 匹配
3. 是否误用了旧环境 token

## 14.4 CLI 安装成功，但命令找不到

### Linux / macOS Apple Silicon

- 重新执行 `source ~/.bashrc` 或对应 shell 的 rc 文件
- 确认 `~/.local/bin` 已加入 PATH

### Windows

- 在当前 PowerShell 会话刷新 PATH
- 确保不要用 `powershell -Command` 再嵌套执行安装命令

---

## 15. 交付检查清单

部署完成后，至少完成以下检查：

- `docker compose ps` 全部服务为运行状态
- Web 页面可访问
- admin 可登录
- 需求列表可正常加载
- Dashboard 可正常加载
- consumer 无明显报错
- Claude 报表生成可用
- CLI 安装包可下载
- `aida status` 在公司网络显示 `internal`，外部网络显示 `public`
- `aida update` 能正确校验并安装最新版本
- Linux / macOS Apple Silicon / Windows 安装命令至少验证一端

如果这份文档后续需要扩展，可以继续补充：

- HTTPS / 域名正式接入方案
- 备份恢复 SOP
- 多环境发布规范
- CI/CD 自动化发布流程
