# Aida 测试服部署流程

> 服务器：`ssh 157`
>
> 项目目录：`/home/intellif/dev/project_manager`

本文只规定如何把代码或 CLI 产物部署到测试服，以及如何确认部署目标已经更新。

## 1. 部署前检查

```bash
ssh 157
cd /home/intellif/dev/project_manager
git status --short --branch
git branch --show-current
git log -1 --oneline
docker compose ps
```

开始前必须明确本次涉及哪些组件：

- `api/` 变化：部署 API；
- `web/` 变化：部署 Web；
- `daemon/`、`install.sh`、`install.ps1` 或 `VERSION` 变化：发布 CLI；
- 没有变化的组件不部署、不重启。

工作区存在无关修改时停止，不覆盖、不回退、不带入本次构建。源码范围不明确时先询问用户。

## 2. 部署 API

仅在 API 发生变化时执行：

```bash
cd /home/intellif/dev/project_manager/api
go test ./...

cd /home/intellif/dev/project_manager
docker compose up -d --build --no-deps api
docker compose ps api
curl -fsS http://192.168.14.157:18090/health
```

完成条件：`api` 容器为运行状态，测试服 `/health` 返回成功。不得重启 `db`、`minio` 或 `web`。

## 3. 部署 Web

仅在 Web 发生变化时执行：

```bash
cd /home/intellif/dev/project_manager/web
export PATH=/home/intellif/.nvm/versions/node/v24.14.0/bin:$PATH
pnpm test
pnpm lint
pnpm typecheck
pnpm build

cd /home/intellif/dev/project_manager
docker compose up -d --build --no-deps web
docker compose ps web
curl -fsSI http://192.168.14.157:13000/
```

完成条件：`web` 容器为运行状态，测试 Web 地址返回成功。不得重启其他服务。

## 4. 发布 CLI

测试 CLI 分发地址固定为：

```text
http://192.168.14.157:9000/statics-live/aida
```

仅在 CLI 或安装脚本发生变化时执行。

### 4.1 构建

测试服 CLI 必须使用独立测试版本号，格式为：

```text
<正式候选版本>-test.<YYYYMMDD>.<当日序号>
```

例如正式候选版本为 `0.1.27` 时，首个测试包使用 `0.1.27-test.20260805.1`。重复测试只增加最后的序号，不修改根目录 `VERSION`，也不占用后续正式版本号。禁止用同一个版本号覆盖测试包，否则已安装该版本的客户端会跳过下载，无法通过正常安装流程取得新产物。

```bash
cd /home/intellif/dev/project_manager/daemon
go test ./... -count=1
go vet ./...

cd /home/intellif/dev/project_manager
TEST_CLI_VERSION=0.1.27-test.20260805.1
make VERSION="$TEST_CLI_VERSION" release-test-dir
cd aida-releases-test
sha256sum -c SHA256SUMS.txt
./aida-linux-amd64 version
```

确认：

- `aida-latest.txt` 和三个二进制内置版本均等于本次 `TEST_CLI_VERSION`；
- 根目录 `VERSION` 保持正式候选版本，不因测试轮次递增；
- 三个平台二进制、两个安装脚本、`SHA256SUMS.txt` 和 `aida-latest.txt` 齐全；
- 安装脚本使用测试 API 和测试 CLI 分发地址，不包含生产地址。

### 4.2 上传

使用测试服务器当前 MinIO 配置进行认证，不打印或记录凭据。上传到 `statics-live/aida`，顺序固定为：

```text
aida-linux-amd64
aida-darwin-arm64
aida-windows-amd64.exe
install.sh
install.ps1
SHA256SUMS.txt
```

上述文件上传后，先从测试 HTTP 地址重新下载并按新 `SHA256SUMS.txt` 校验。全部通过后，最后上传 `aida-latest.txt`。

### 4.3 部署后检查

```bash
curl -fsS http://192.168.14.157:9000/statics-live/aida/aida-latest.txt
```

再次从测试 HTTP 地址下载七个发布文件并执行：

```bash
sha256sum -c SHA256SUMS.txt
./aida-linux-amd64 version
```

完成条件：测试地址的版本文件等于目标版本，全部下载文件校验通过，下载的 Linux 二进制输出目标版本。

发布 CLI 不包含安装、替换或修改服务器上任何账号已经安装的 Aida。

## 5. 多组件部署顺序

同时涉及多个组件时，顺序为：

```text
API -> Web -> CLI
```

每一步达到本节规定的完成条件后再继续下一步。

## 6. 部署记录

每次部署在 `doc/发布事项/` 保存一份记录，只填写：

- 日期、环境、源码版本和改动范围；
- 实际部署的组件和版本；
- 执行的构建命令及结果；
- 容器状态、健康接口、CLI 版本文件和 SHA256 结果；
- 是否提交、推送以及是否涉及生产环境。

部署记录到此结束。
