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

## Dashboard

The dashboard is intended as an at-a-glance operational view:

- Control-plane API reachability.
- Host/resource pool status.
- Project count and runtime status.
- System usage and capacity where available.
- Advisor and Compliance summaries.

Static requested resources and live usage are different. Requested resources describe what a project reserves or intends to use; live usage describes what the host/project is consuming now.

## Metrics And Logs

The current MVP includes lightweight metrics/log surfaces in the admin UI. For production-grade observability, plan to add external log and metrics storage such as Prometheus/Grafana, OpenTelemetry, or another long-retention backend.

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
npm --prefix frontend run dev -- --host 0.0.0.0 --port 3000
```

If using a public admin host during development, set `SUPADUPA_ADMIN_HOST` so Vite allows the host.

## Validation

Local validation:

```bash
go test ./...
npm --prefix frontend run build
bash -n scripts/compat/*.sh
docker compose -f deploy/compose.yaml config
```

Live validation:

```bash
export SUPADUPA_API_URL=https://api.example.com
export SUPADUPA_TEST_EMAIL=admin@example.com
export SUPADUPA_TEST_PASSWORD='change-this-password'
export SUPADUPA_TEST_REF=smoke
scripts/compat/run.sh
```
