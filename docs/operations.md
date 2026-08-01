# Operations

This document covers day-to-day operation of the current Docker Compose MVP runtime.

## Common Commands

Show services:

```bash
docker compose -f deploy/compose.yaml ps
```

Control-plane logs:

```bash
docker compose -f deploy/compose.yaml logs -f supadupavisor
```

Traefik edge logs:

```bash
docker compose -f deploy/compose.yaml --profile edge logs -f edge-router
```

Rebuild/restart:

```bash
docker compose -f deploy/compose.yaml --profile edge up -d --build
```

Stop:

```bash
docker compose -f deploy/compose.yaml --profile edge down
```

## Runtime Layout

Default runtime paths:

```text
runtime/projects     rendered per-project Compose stacks
runtime/routes       Traefik dynamic routes
runtime/certs        uploaded/generated certificate artifacts
runtime/backups      project and platform backup artifacts
```

Do not delete `runtime/` unless you intentionally want to remove rendered state and local artifacts.

## Project Runtime

Each project has a rendered Compose stack under:

```text
runtime/projects/<ref>
```

Inspect a project:

```bash
docker compose -p <ref> -f runtime/projects/<ref>/compose.yaml ps
docker compose -p <ref> -f runtime/projects/<ref>/compose.yaml logs
```

The control plane should own project lifecycle operations. Manual Compose operations are useful for debugging, but normal create/update/delete should go through Supadupa.

## Metadata Migrations

The control plane runs metadata database migrations on startup. Applied migrations are recorded with version, name, SHA-256 checksum, and timestamp in `schema_migrations`.

Operational rules:

- Treat released migration files as immutable. Add a new migration instead of editing an existing one.
- Startup fails if an already applied migration version has different SQL bytes than the checksum stored in the metadata database.
- Legacy rows without checksums are backfilled during the first run after upgrade.
- Run `go test ./internal/metadb ./internal/control` after migration or persistent-store changes.

If a checksum failure appears during upgrade, stop the rollout and inspect the migration file history before changing the database. Do not manually rewrite `schema_migrations` unless you have proven the database schema and migration bytes are intentionally equivalent.

## Backup Target Persistence

Backup storage targets and project backup policies are persisted in normalized metadata tables and checkpoint snapshots. `BackupPolicy.StorageTargetID` is part of the persisted policy state, so a project policy remains bound to its selected S3/R2/remote-MinIO target after control-plane restart, checkpoint restore, or normalized-table reconstruction.

Production recovery guidance:

- Configure and test an off-host target before relying on logical backups, physical backups, WAL archives, or upgrade safety backups.
- Enable `SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true` when recovery-critical uploads must reject missing, untested, loopback, or local-only targets.
- Use `SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true` for production upgrade workflows that must have a durable pre-upgrade backup.
- Validate target persistence with `go test ./internal/control` and, when a disposable metadata database is available, `SUPADUPA_TEST_META_DSN=... go test ./internal/control -run TestPersistentStoreRestoresProjectChildFieldsFromNormalizedTables`.

## Dashboard

The dashboard is intended as an at-a-glance operational view:

- Control-plane API reachability.
- Host/resource pool status.
- Project count and runtime status.
- System usage and capacity where available.
- Advisor and Compliance summaries.

Static requested resources and live usage are different. Requested resources describe what a project reserves or intends to use; live usage describes what the host/project is consuming now.



## Host capacity accounting (no project-wide cgroup)

Tier and exact CPU/RAM/disk values are **reservations** for placement, quota, and dashboards. Docker Compose does **not** provide a true project-wide aggregate cgroup that caps the sum of all service containers under one project hard limit.

What operators should do:

1. Size the host for control plane **plus** the sum of concurrent project reservations (see install resource requirements).
2. Enable **Enforce limits** on projects that must not burst: Supadupa then writes per-container Compose `deploy.resources.limits` (or Kubernetes requests/limits) by distributing the project budget across enabled services.
3. Treat telemetry over 100% of reservation as a signal that the project is bursting into free host capacity when limits are off — not as a Compose bug.
4. For stricter multi-tenant isolation than per-container limits, run projects on dedicated hosts/VMs or use Kubernetes with ResourceQuota per project namespace (`projectIsolation` + quota in the Helm chart).

This is plan item **C3** (host accounting documentation). True host-level aggregate cgroups remain optional future work outside Compose.


## Metrics And Logs

The current MVP includes lightweight metrics/log surfaces in the admin UI. For production-grade observability, plan to add external log and metrics storage such as Prometheus/Grafana, OpenTelemetry, or another long-retention backend.

Project telemetry history is retained inside the control-plane checkpoint for 30 days. Raw samples are kept for 24 hours, then compacted into five-minute rollups. Project viewers can read this history through the UI, API, CLI, and MCP tools, so treat CPU, memory, disk, and network counters as retained operational metadata when planning backups, exports, and access reviews. The built-in project telemetry collector is currently implemented for Docker Compose deployments; Kubernetes deployments need an external metrics path or a future Kubernetes collector for project history to populate.

Useful places in the UI:

- Dashboard.
- Project overview.
- Project logs.
- Project runtime panel.
- Hosts.
- Advisor.
- Compliance.
- Audit.

## Advisor And Compliance

Advisor reports actionable operational findings. Compliance reports evidence posture and control status. These screens are helpers, not certification claims.

Common findings:

- API surface unreachable.
- Project runtime degraded.
- No host capacity registered.
- Recovery target not production-ready.
- Durable upgrade backup enforcement disabled.

## Local Development

Run only the meta database in Docker:

```bash
export SUPADUPA_META_DB_ADDR=127.0.0.1:15432
docker compose -f deploy/compose.yaml up -d meta-db
```

Run the API natively:

```bash
export SUPADUPA_META_DSN='postgres://supadupa:supadupa@localhost:15432/supadupa_meta?sslmode=disable'
export SUPADUPA_COMPOSE_APPLY=true
go run ./cmd/supadupa api
```

Run the admin UI natively:

```bash
npm --prefix frontend install
npm --prefix frontend run dev -- --host 127.0.0.1 --port 3000
```

If using a public admin host during development, set `SUPADUPA_ADMIN_HOST` so Vite allows the host.

## Validation

Local validation:

```bash
go test ./...
npm --prefix frontend run build
npm --prefix frontend run check
npm --prefix frontend audit --audit-level=moderate
npm --prefix scripts/compat ci
npm --prefix scripts/compat audit --audit-level=moderate
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
bash -n scripts/compat/*.sh
docker compose -f deploy/compose.yaml config
```

CI writes `govulncheck` plus frontend and compat `npm audit --json` output under `artifacts/security` and uploads them as the `security-audit-results` artifact when present.

Live validation:

```bash
export SUPADUPA_API_URL=https://api.example.com
export SUPADUPA_TEST_EMAIL=admin@example.com
export SUPADUPA_TEST_PASSWORD='change-this-password'
export SUPADUPA_TEST_REF=smoke
scripts/compat/run.sh
```
