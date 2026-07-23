# Repository Guidelines

## Project Structure

This repository contains the Aida platform.

- `api/`: Go HTTP API, handlers in `api/handler`, services in `api/service`, config in `api/config`, migrations in `api/db/migrations`
- `daemon/`: Go `aida` CLI and Session upload client; it is not a production consumer service
- `web/`: Vite + React + TypeScript frontend
- `doc/`: product, rule, and validation documents
- `docker-compose.yml`: local integration stack

Do not assume old scaffold structure such as Next.js `app` router or separate frontend/backend template conventions. Check the actual code first.

## Build and Run

### Full local stack

```bash
docker compose up -d
```

### API only

```bash
docker compose up -d db minio
cd api && go run main.go
```

### Web only

```bash
cd web
pnpm install
pnpm dev
```

### CLI

```bash
cd daemon
go build -o aida .
./aida version
```

## Validation Commands

### Backend

```bash
cd api && go test ./...
cd daemon && go test ./...
```

The host has Go 1.26.3 installed at `~/sdk/go1.26.3` and wired into `PATH` via
`~/.bashrc` (`GOROOT`, `GOPATH`, `GOTOOLCHAIN=local` are all set). In a fresh
shell `go`, `gofmt`, `go test`, `go build`, and `go vet` work directly.

Prefer the host toolchain for Go test/build/format work. Do not spin up a
Docker container just to run `go test` — call `go` on the host. Only use
Docker for Go if you specifically need to reproduce the container build
environment; in that case never pull a new image or guess a patch-version
tag, run with `--pull=never`, and stop if no local Go image matches.

### Frontend

```bash
cd web && pnpm lint && pnpm typecheck && pnpm build
```

There are also workflow scripts in `web/package.json`, including `pnpm test`.

## Key Business Constraints

### Optimistic lock

Requirements and tasks use `version` optimistic locking.

When changing related code:

- requests must carry `base_version`
- final update must still rely on `WHERE id AND version`
- write conflicts must distinguish `404` from `409 EDIT_CONFLICT`
- all list/detail/dashboard queries that hydrate requirement/task data must include `version`

### Task done semantics

Any path that updates task status or progress must keep `done`, `progress`, and `completed_at` consistent. A successful write should increment `version` once.

### Frontend sync

Mutations on requirement/task/follow/dashboard pages must refresh every impacted query, not only the current component state.

### Validation policy

Current requirement/task field validation is intentionally loose. Treat it as safety validation, not heavy product gating.

## Coding Style

### Go

- use `gofmt`
- keep packages small and lowercase
- add focused `*_test.go` tests next to the package under change

### Frontend

- TypeScript with existing project patterns
- Ant Design components first, custom UI second
- keep edits scoped; avoid rewriting unrelated page structures
- prefer stable class names and page-local CSS for business page polish

## Migrations and Data

- migrations are under `api/db/migrations`
- API startup applies migrations automatically
- use forward migrations for schema fixes

Before an API release, compare the target database `schema_migrations` with the
current highest file in `api/db/migrations`. Do not treat a historical migration
number in documentation as the latest version.

## Release Notes

CLI packaging commands:

```bash
make release-test-dir
make release-prod-dir
```

Use the test package only for the fixed internal test distribution path. Do not reuse it for production.

## Production Deployment and System Report Assets

The production runtime services are `db`, `minio`, `api`, and `web`. There is
no production `consumer`; `daemon/` is packaged as the user-facing CLI.

Use immutable service image tags. Build and restart only the service in scope.
The checked-in single-port Compose template currently shares `IMAGE_TAG`
between API and Web, so a single-service release must not change that shared
value or run an unqualified `docker compose up -d`. Follow the service-specific
tag and container-ID checks in the deployment document.

The system report skill source is `api/service/daily_report_skill.go`. Skill
versions are immutable: publish a new version under the environment-specific
owner, verify it is uniquely resolvable in the public Registry, and only then
update Aida configuration. Never delete managed-agent database rows as a normal
refresh mechanism.

Production owner is `10086`; test owner is `100866`. The default report Agent
uses Aida's inline Report MCP with the current running user's token. Do not use
the system account token to read user data, and do not modify
`/home/intellif/dev/sandboxed-agent-platform` for Aida deployment work.

Canonical documents:

- `doc/Aida部署与运维基准.md`
- `doc/系统资源skill+mcp管理文档.md`

## Documentation

Business documents, validation cases, rollout notes, and deployment instructions are maintained under [doc/](/home/intellif/dev/project_manager/doc).

### Independent feature development

Before creating a branch or worktree for an independent feature or bug fix, read and follow `doc/Git-Worktree独立功能开发流程.md`. When `main` is occupied by other development, testing, or release preparation, perform the task in an isolated worktree. Do not reuse the shared runtime, database, ports, containers, or volumes, and do not merge, push, deploy, or clean up outside the authorization and checks defined by that document.

### Product and architecture document governance

For product, architecture, development, and test documents, use the templates under `doc/v2/文档模板/` and follow this order:

1. finalize product requirements;
2. verify current code and data facts;
3. propose the smallest architecture that satisfies the requirements;
4. validate uncertain decisions;
5. confirm architecture decisions;
6. split development work;
7. derive tests and acceptance from the requirements.

Every material statement must be classified as one of:

- **Product rule**: explicitly approved behavior or scope;
- **Verified code fact**: supported by a current source path, migration, test, or runtime evidence;
- **Necessary consequence**: unavoidable to satisfy a product rule, with the reasoning recorded;
- **Recommendation**: a candidate choice that is not yet approved.

Only product rules and necessary consequences that are both evidence-backed and confirmed may create implementation tasks. A code fact by itself does not create product scope or a development task. Recommendations and unverified architecture judgments must remain decision items; do not silently turn them into required tables, migrations, services, protocols, states, configuration, refactors, or future abstractions.

Each key architecture decision must record:

```text
对应需求 / 当前代码事实 / 待解决问题 / 候选方案 / 选择方案 / 验证证据 / 是否已确认
```

Each development task must trace to both a requirement section and a confirmed architecture decision. If either reference is missing, the task is not ready for implementation.

Default to the smallest change that closes the current product requirement on the current system. “Best practice” is only a candidate, not an implementation mandate. Logical responsibilities in a diagram do not require new packages, interfaces, processes, or services. Prefer one deep module with a small public interface and keep querying, pagination, deduplication, freezing, and validation details internal unless a proven boundary requires otherwise.

Before asking for full-document review, provide a one-page decision delta containing only behavior changes, code changes, new resources, unresolved decisions, and explicit non-goals. Do not rely on reviewers finding hidden scope expansion in long documents.

When requirements are already approved, architecture, development, and test documents must treat the requirements document as the sole product baseline. Earlier drafts and superseded split documents are not sources of truth. If implementation reveals a product-rule conflict, stop and amend the requirements first instead of resolving it inside the development plan.
