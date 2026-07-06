# Repository Guidelines

## Project Structure

This repository contains the Aida platform.

- `api/`: Go HTTP API, handlers in `api/handler`, services in `api/service`, config in `api/config`, migrations in `api/db/migrations`
- `daemon/`: Go CLI and report generator service
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

### Daemon / consumer

```bash
cd daemon
go build -o aida .
./aida serve
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

Important existing migrations:

- `005_user_auth.sql`
- `007_requirements_p0.sql`
- `016_requirement_task_versions.sql`

## Release Notes

CLI packaging commands:

```bash
make release-test-dir
make release-prod-dir
```

Use the test package only for the fixed internal test distribution path. Do not reuse it for production.

## Fast Aida Report Skill Refresh

The system report skill is generated from `api/service/daily_report_skill.go` and published to the managed agent platform as `aida-report@1.0.0`. Keep the version at `1.0.0` during development unless the user explicitly asks for a version change.

Do not edit `/home/intellif/dev/sandboxed-agent-platform` source for this. That repo is only a reference and runtime dependency.

Fast path after changing the skill source:

1. Build and push the current `project_manager` images from `192.168.14.157`.

```bash
ssh 157 'cd /home/intellif/dev/project_manager && set -e
REG=192.168.14.129:80/aied
TAG=$(date +%Y%m%d)-$(git rev-parse --short HEAD)-$(date +%H%M)
for svc in api web daemon; do
  case "$svc" in
    api) repo=ai-coding-console-api ;;
    web) repo=ai-coding-console-web ;;
    daemon) repo=ai-coding-console-consumer ;;
  esac
  docker build -t "$REG/$repo:$TAG" -t "$REG/$repo:latest" "./$svc"
  docker push "$REG/$repo:$TAG"
  docker push "$REG/$repo:latest"
done
echo "$TAG"'
```

2. Update production `/home/luoxian/aida/.env` to the new `IMAGE_TAG`, keep `MANAGED_AGENT_REPORT_SKILL_VERSION=1.0.0`, then restart production services.

```bash
ssh luoxian@192.168.14.182
cd /home/luoxian/aida
docker compose pull api web consumer
docker compose up -d api web consumer
docker compose ps
docker compose exec -T api printenv | grep MANAGED_AGENT_REPORT_SKILL_VERSION
curl -fsS http://localhost/health
```

3. Force the managed agent platform to rebuild the system skill from the newly deployed API code. The existing `ensureUserReportSkill` path does not overwrite an active `aida-report@1.0.0`; delete the old active `1.0.0` row and archive all other versions first.

```bash
ssh 157 'docker run --rm --network host -e PGPASSWORD="$SAP_PG_PASSWORD" postgres:16-alpine \
  psql -h 192.168.18.107 -p 15434 -U sap -d sap -v ON_ERROR_STOP=1 -c "
begin;
update skills set archived=true, updated_at=now()
where slug='\''aida-report'\'' and version <> '\''1.0.0'\'' and archived=false;
delete from skills
where slug='\''aida-report'\'' and version = '\''1.0.0'\'';
commit;"'
```

Use the known managed-agent Postgres password from the deployment environment as `SAP_PG_PASSWORD`; do not commit that secret.

4. Trigger rebuild with a valid production Aida JWT for the target owner, normally a test/admin user such as `t03`.

```bash
curl -fsS 'http://113.100.143.91:9180/api/v1/ai-assets/skills?scope=mine&include_system=true' \
  -H 'Accept: application/json' \
  -H "Authorization: Bearer $AIDA_JWT" \
  --insecure
```

5. Verify only the latest active skill remains.

```bash
ssh 157 'docker run --rm --network host -e PGPASSWORD="$SAP_PG_PASSWORD" postgres:16-alpine \
  psql -h 192.168.18.107 -p 15434 -U sap -d sap -c "
select version, archived, count(*)
from skills
where slug='\''aida-report'\''
group by version, archived
order by version, archived;
select count(*) as active_non_100
from skills
where slug='\''aida-report'\'' and archived=false and version <> '\''1.0.0'\'';
select count(*) as active_100
from skills
where slug='\''aida-report'\'' and archived=false and version = '\''1.0.0'\'';"'
```

Expected result: `active_non_100 = 0`, `active_100 = 1`. Also fetch `skill-md` and check the current rules exist before running report regression.

## Documentation

Business documents, validation cases, rollout notes, and deployment instructions are maintained under [doc/](/home/intellif/dev/project_manager/doc).
