# Aida

Aida 是面向 AI 部门内部的需求与产出管理平台，覆盖从需求、任务、Session 上报到 Token 统计、日报/周报生成的完整链路。

当前仓库包含 4 个主要部分：

- `api/`：Go HTTP API，负责鉴权、需求/任务、Dashboard、报表、Token 统计等后端能力
- `daemon/`：Go CLI 与报表生成服务，共用一套代码
- `web/`：Vite + React 18 + Ant Design 6 前端
- `docker-compose.yml`：本地联调所需的 PostgreSQL、MinIO、API、Web、Consumer

## 1. 当前功能范围

- 需求管理：需求创建、编辑、关注、验收标准维护、任务拆解
- 任务管理：任务创建、分配、进度更新、关注、与 AC 关联
- 乐观锁：需求与任务编辑基于 `version` 做并发控制
- Session 上报：CLI 扫描本地 Claude/Codex 会话并上报
- Token 统计：个人、团队、部门多视角聚合
- 日报/周报：基于已上传 session 生成草稿并提交

## 2. 技术栈

### 后端

- Go `1.26.3`
- `chi v5`
- PostgreSQL `16`
- MinIO（可选，用于原始日志对象存储）

### 前端

- Vite `8`
- React `18`
- TypeScript `5`
- Ant Design `6`
- TanStack Query `5`
- React Router `7`

## 3. 快速启动

### 方式一：Docker Compose

```bash
docker compose up -d
docker compose ps
```

默认端口：

- Web: `http://localhost:3000`
- API: `http://localhost:8080`
- PostgreSQL: `localhost:5432`
- MinIO API: `http://localhost:9000`
- MinIO Console: `http://localhost:9001`

### 方式二：本地开发

先启动依赖：

```bash
docker compose up -d db minio
```

启动 API：

```bash
cd api
DATABASE_URL="postgres://aidashboard:devpassword@localhost:5432/aidashboard?sslmode=disable" \
JWT_SECRET="dev-jwt-secret" \
CORS_ORIGIN="http://localhost:5173" \
PORT="8080" \
go run main.go
```

启动 Web：

```bash
cd web
pnpm install
pnpm dev
```

默认本地开发地址：

- Web: `http://localhost:5173`
- API: `http://localhost:8080`

启动报表生成服务：

```bash
cd daemon
go build -o aida .
./aida serve
```

## 4. 常用命令

### 后端

```bash
cd api
go build ./...
go test ./...
```

### CLI / Consumer

```bash
cd daemon
go build -o aida .
go test ./...
```

### 前端

```bash
cd web
pnpm lint
pnpm typecheck
pnpm build
pnpm test
```

## 5. 配置说明

### API 环境变量

见 [api/config/config.go](/home/intellif/dev/project_manager/api/config/config.go)。

常用项：

- `DATABASE_URL`
- `JWT_SECRET`
- `CORS_ORIGIN`
- `PORT`
- `REPORT_GENERATOR_URL`
- `ENABLE_PUBLIC_REGISTER`
- `MINIO_ENDPOINT`
- `MINIO_ACCESS_KEY`
- `MINIO_SECRET_KEY`
- `MINIO_BUCKET`
- `MINIO_USE_SSL`
- `MINIO_EXTERNAL_ENDPOINT`

说明：

- `ENABLE_PUBLIC_REGISTER` 默认是 `false`
- MinIO 未配置时，系统仍可运行，只是原始日志对象存储相关能力不可用

### Web 运行时配置

见 [web/public/config.js](/home/intellif/dev/project_manager/web/public/config.js) 与 [docker-compose.yml](/home/intellif/dev/project_manager/docker-compose.yml)。

主要配置项：

- `AIHUB_RUNTIME_CONFIG_apiBaseUrl`
- `AIHUB_RUNTIME_CONFIG_authApiBaseUrl`
- `AIHUB_RUNTIME_CONFIG_userApiBaseUrl`
- `AIHUB_RUNTIME_CONFIG_appTitle`

## 6. 数据与迁移

- 所有数据库迁移在 `api/db/migrations/`
- API 启动时自动按编号执行迁移
- 不支持 down migration，修复通过新增 forward migration 完成

关键迁移：

- `005_user_auth.sql`：用户登录与密码
- `007_requirements_p0.sql`：需求/任务 P0 基础结构
- `016_requirement_task_versions.sql`：需求与任务 `version` 乐观锁字段

## 7. 业务约束

### 需求 / 任务乐观锁

- 编辑需求、任务时必须带 `base_version`
- 后端最终更新必须走 `WHERE id AND version`
- `RowsAffected=0` 时要区分：
  - 数据不存在：`404`
  - 版本冲突：`409 EDIT_CONFLICT`

### 进度与完成态

- `task.status=done` 时，`progress` 与 `completed_at` 逻辑必须一致
- Dashboard 推荐操作如果通过一次 `updateTask` 同时修改 `status + progress`，也必须复用相同规则

### 前端状态同步

- 需求、任务、关注、Dashboard 操作完成后，必须同步刷新相关 Query
- 不能只更新当前弹窗本地状态，必须覆盖列表、详情、Dashboard 等关联视图

## 8. CLI 发布

测试发布包：

```bash
make release-test-dir
```

正式发布包：

```bash
make release-prod-dir
```

补充说明：

- 测试发布地址固定为 `http://192.168.14.157:9000/statics-live/aida`
- 正式包不要复用测试包
- Windows 用户请直接在当前 PowerShell 会话中执行 `Invoke-RestMethod ... | Invoke-Expression`

详细发布文档见：
[doc/Aida部署与运维基准.md](/home/intellif/dev/project_manager/doc/Aida部署与运维基准.md)

## 9. 相关文档

业务方案、测试用例、字段规则、部署说明等文档统一维护在 [doc/](/home/intellif/dev/project_manager/doc) 目录。

## 10. 开发建议

- 前端改动提交前至少执行 `pnpm lint && pnpm typecheck && pnpm build`
- 后端接口或并发逻辑改动提交前至少执行 `go test ./...`
- 涉及需求/任务编辑时，优先检查 `version`、Query 同步、Dashboard 联动是否完整
