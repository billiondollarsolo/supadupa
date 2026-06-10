<p align="center">
  <img src="supadupa-logo-wide.png" alt="Supadupa" width="440" />
</p>

<p align="center">
  Self-hosted, multi-project Supabase control plane — run an isolated Supabase stack per project on your own infrastructure.
</p>

<p align="center">
  <a href="#quick-start-local-loopback">Quick start</a> ·
  <a href="#how-routing-works">Routing</a> ·
  <a href="#backups-and-recovery">Backups</a> ·
  <a href="#security-notes">Security</a> ·
  <a href="docs/README-legacy.md">Full reference</a>
</p>

---

Supadupa lets an operator run many isolated Supabase projects on their own infrastructure, expose each project through its own public API, Studio, Postgres, pooler, Storage, Realtime, and Functions routes, and manage those projects from a browser admin UI, CLI, Terraform provider, and Management API.

This repo is currently in MVP shape. The Docker Compose backend is the supported runtime for bringing projects up on a Linux host or VPS. Kubernetes support exists as a renderer/operator contract, but it is not the primary MVP install path yet.

## MVP Status

This is a very early release. There will be many rough edges, missing features, and potential instability. There will also likely be breaking changes to the API, CLI, and runtime behavior as we iterate quickly toward a more stable v1.0.

Supadupa is good enough for MVP evaluation and internal dev environments where the operator understands the remaining hosted-grade gaps.

Working at MVP level:

- Admin UI for organizations, users, projects, routes, settings, backups, logs, metrics, security, and operations.
- Multi-project Compose provisioning with isolated project directories, networks, secrets, routes, and public hostnames.
- Public project surfaces through Traefik: API, Studio, Storage S3, direct Postgres, transaction pooler, session pooler, Realtime, and Edge Functions.
- Project Connect page and CLI profile for remote clients.
- Supabase JS, official Supabase CLI DB workflows, and Supadupa CLI workflows.
- Studio access through Supadupa control-plane login.
- Custom domains with generated route/cert artifacts and BYO certificate upload.
- Logical backups, physical backup plumbing, WAL archive plumbing, control-plane backups, backup target management, and recoverability reporting.
- Stable stack release catalog and project upgrade guardrails.
- Fleet dashboard, Advisor, Compliance, audit log, and metrics.
- Compatibility test suite under [scripts/compat](scripts/compat).

Not hosted-grade yet:

- No completed destructive PITR proof against a real off-host S3/R2/remote-MinIO target in the live deployment.
- Failed-upgrade restore needs to be proven with durable off-host backup artifacts, not only local/dev artifacts.
- Official `supabase gen types --db-url` against the public DB route still has an upstream Supabase CLI TLS/CA caveat. Supadupa provides a tunnel/wrapper workaround.
- Real third-party provider propagation still needs proof for external CDN behavior, real SMS delivery, and true multi-region placement/failover.
- Kubernetes is not the MVP runtime path.
- Compliance screens are operator evidence helpers, not a certification claim.

## How Routing Works

Use two DNS zones/patterns:

- Control plane: `admin.example.com`, `api.example.com`
- Projects: `*.apps.example.com`

For project `smoke`, Supadupa generates routes like:

- API: `https://smoke.apps.example.com`
- Studio: `https://studio-smoke.apps.example.com`
- Storage S3: `https://storage-smoke.apps.example.com`
- Direct Postgres: `db-smoke.apps.example.com:5432`
- Pooler: `pooler-smoke.apps.example.com:6543`

Only Traefik publishes public HTTP/TLS/Postgres ports. Project containers are not host-published.

## Requirements

For local loopback evaluation:

- Linux or macOS with Docker
- Docker Compose v2
- Go 1.25+ if running binaries natively
- Node `^20.19.0` or `>=22.12.0`

For a real VPS install:

- Linux VPS
- Public IPv4/IPv6 address
- Docker and Docker Compose v2
- DNS control for your domain
- Cloudflare DNS API token for Let's Encrypt DNS-01
- Open inbound ports `80` and `443`; open `5432` and `6543` only when intentionally exposing public DB/pooler routes.

You only need Docker + Docker Compose v2 to run the platform — Go and Node are required only if you build/run the binaries or frontend natively (the Compose path builds them in containers).

Plan for **~4 GB RAM per `full`-profile project** plus ~0.5 GB for the control plane: each project runs its own full Supabase stack, and on under ~4 GB the Logflare/analytics container is OOM-killed and the project shows `degraded`. See [Resource Requirements](docs/install.md#resource-requirements) for per-profile sizing and how to run leaner.

## Get the Code

```bash
git clone https://github.com/billiondollarsolo/supadupa.git
cd supadupa
```

All commands below are run from the repository root.

## Quick Start: Local Loopback

This starts the control plane, meta database, and admin UI on local ports. It does not give you public project TLS routes.

```bash
# from the repo root (see "Get the Code" above)
export SUPADUPA_BOOTSTRAP_PASSWORD='change-this-password'
scripts/setup-compose.sh --mode local
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.apply.yaml up -d --build
```

Open:

```text
http://localhost:3000
```

Login with the bootstrap email/password written in `.env`. The default bootstrap email is `admin@example.test` unless you pass `--bootstrap-email`.

To stop:

```bash
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.apply.yaml down
```

## Quick Start: VPS With DNS And TLS

Example domain:

```text
example.com
```

Create a Cloudflare API token with DNS edit permission for that zone, then run:

```bash
export CLOUDFLARE_API_TOKEN='...'
export SUPADUPA_BOOTSTRAP_PASSWORD='change-this-password'
scripts/setup-compose.sh \
  --mode vps \
  --domain example.com \
  --email ops@example.com \
  --bootstrap-email admin@example.com
```

Postgres and pooler edge ports (`5432`/`6543`) publish on `0.0.0.0` by default but stay unreachable until you enable external DB access (the `database_external_access` flag plus a per-project `db_ingress_mode`) — the host bind isn't the gate, Traefik is. Pass `--db-loopback` to bind them to `127.0.0.1` instead.

Create DNS records pointing at the VPS:

```text
admin.example.com       A/AAAA  <server-ip>
api.example.com         A/AAAA  <server-ip>
*.apps.example.com      A/AAAA  <server-ip>
```

Start the public edge stack:

```bash
docker compose --env-file .env -f deploy/compose.yaml -f deploy/compose.apply.yaml --profile edge up -d --build
```

Open:

```text
https://admin.example.com
```

Traefik uses Cloudflare DNS-01 to issue certificates. You do not need a separate wildcard certificate for the MVP path.

## First Setup In The Admin UI

After login:

1. Go to `Settings -> Defaults`.
2. Confirm the project domain. On a fresh/default install, startup seeds it from `SUPADUPA_APPS_DOMAIN`, for example `apps.example.com`.
3. Confirm the default stack version, profile, tier, and backup schedule.
4. Go to `Settings -> Backups`.
5. For production-like recovery, add a real off-host S3/R2 target and test it.
6. Create an organization if one does not exist.
7. Create a project.
8. Open the project `Connect` page for API keys, DB strings, pooler strings, and CLI profile.
9. Open project Studio from the project page.

`SUPADUPA_APPS_DOMAIN` seeds only a fresh/default project domain so the Admin UI can remain the normal operator control. Set `SUPADUPA_PROJECT_DOMAIN` if the environment file should explicitly override the Admin UI on every restart.

## Creating A Project

From the UI (`Projects` -> add project), the wizard has three steps:

1. **Identity** — project name, ref, and base domain.
2. **Org & placement** — organization and host (or the default local runtime).
3. **Stack** — stack version, profile (database engine + starting service set), size (a small/medium/large preset or exact CPU/RAM/disk), per-service toggles, and optional database container resource-limit enforcement.

Project creation is asynchronous: the request returns immediately (HTTP `202 Accepted`, status `provisioning`) and the runtime is brought up in the background, transitioning `provisioning -> starting -> healthy`. Poll `GET /v1/projects/{ref}` (or watch the UI) until the status is `healthy`.

From the CLI:

```bash
go run ./cmd/supadupa-cli --api https://api.example.com login \
  --email admin@example.com \
  --password 'change-this-password'

go run ./cmd/supadupa-cli --api https://api.example.com projects create \
  --org-id <org-id> \
  --ref smoke \
  --name "Smoke"
```

## Connecting To A Project

Use the project `Connect` page. It returns the canonical project-specific URLs.

Typical values:

```text
SUPABASE_URL=https://smoke.apps.example.com
SUPABASE_ANON_KEY=<anon key>
SUPABASE_SERVICE_ROLE_KEY=<service role key>
DATABASE_URL=postgres://postgres:<password>@db-smoke.apps.example.com:5432/postgres?sslmode=require
POOLER_TRANSACTION_URL=postgres://postgres.<ref>:<password>@pooler-smoke.apps.example.com:6543/postgres?sslmode=require
```

The official Supabase CLI DB commands can run against the public DB URL. If public typegen hits the known Supabase CLI TLS caveat, use Supadupa's tunnel/wrapper flow:

```bash
go run ./cmd/supadupa-cli projects db-tunnel --ref smoke
go run ./cmd/supadupa-cli projects gen-types --ref smoke
```

## Backups And Recovery

MVP behavior:

- Logical project backups are available.
- Physical backup and WAL archive plumbing exists.
- Control-plane backups are available from `Settings -> Backups`.
- S3-compatible backup targets can be added and tested.
- Local RustFS can prove S3 plumbing, but does not count as off-host recovery.

Hosted-grade recovery requires:

1. A real off-host S3/R2/remote-MinIO backup target.
2. Successful server-side target test.
3. A recovery-ready default target or explicit target binding per project policy.
4. `SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true`.
5. `SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true`.
6. Physical backup validation.
7. WAL archive validation.
8. Destructive disposable PITR restore validation.
9. Failed-upgrade restore validation.

Until those are complete, Advisor and Compliance intentionally show action-needed recovery findings.

## Operating Supadupa

Common commands:

```bash
docker compose -f deploy/compose.yaml ps
docker compose -f deploy/compose.yaml logs -f supadupavisor
docker compose -f deploy/compose.yaml --profile edge logs -f edge-router
docker compose -f deploy/compose.yaml -f deploy/compose.apply.yaml --profile edge up -d --build
```

Runtime data lives under `runtime/` by default:

```text
runtime/projects     rendered per-project Compose stacks
runtime/routes       Traefik dynamic routes
runtime/certs        uploaded/generated cert artifacts
runtime/backups      project and platform backup artifacts
```

Do not delete `runtime/` unless you intentionally want to remove rendered state and local artifacts.

## Upgrades

Project stack upgrades are limited to stable release manifests exposed by:

```text
GET /v1/stack-releases
```

The upgrade API creates or verifies a pre-upgrade logical backup and records rollback metadata. For production-like upgrades, enable durable upgrade backup enforcement and use an off-host target.

Control-plane updates use normal Compose rebuild/restart:

```bash
docker compose -f deploy/compose.yaml -f deploy/compose.apply.yaml --profile edge up -d --build
```

The API runs meta DB migrations on startup and reconciles persisted project runtime artifacts.

## Custom Domains And Certificates

Generated project domains use the wildcard apps domain. For custom domains:

1. Point the custom FQDN at the Supadupa edge host.
2. Add the custom domain on the project config page.
3. Let Traefik/ACME handle managed TLS, or upload a BYO certificate/key pair.

Supadupa rejects custom domains that collide with platform hosts or generated project hosts.

## Local Development

Run backend/API natively while meta DB and project runtime use containers:

```bash
docker compose -f deploy/compose.yaml up -d meta-db
export SUPADUPA_META_DSN='postgres://supadupa:supadupa@localhost:15432/supadupa_meta?sslmode=disable'
export SUPADUPA_SECRET_KEY="$(openssl rand -hex 32)"
export SUPADUPA_AUTH_SECRET="$(openssl rand -hex 32)"
export SUPADUPA_COMPOSE_APPLY=true
export SUPADUPA_PROJECT_ROOT="$PWD/runtime/projects"
go run ./cmd/supadupa api
```

Run the admin UI:

```bash
npm --prefix frontend install
npm --prefix frontend run dev -- --host 127.0.0.1 --port 3000
```

Run tests:

```bash
go test ./...
npm --prefix frontend run build
bash -n scripts/compat/*.sh
```

## Compatibility Tests

The compatibility suite lives in [scripts/compat](scripts/compat). It can validate project isolation, public endpoints, official Supabase CLI DB workflows, Storage, Realtime, Functions, Auth, backups, upgrades, and recovery posture.

Basic live run:

```bash
export SUPADUPA_API_URL=https://api.example.com
export SUPADUPA_TEST_EMAIL=admin@example.com
export SUPADUPA_TEST_PASSWORD='change-this-password'
export SUPADUPA_TEST_REF=smoke
scripts/compat/run.sh
```

Durable recovery validation requires real off-host S3/R2 credentials. See [scripts/compat/README.md](scripts/compat/README.md).

## Security Notes

- Use strong `SUPADUPA_SECRET_KEY` and `SUPADUPA_AUTH_SECRET`.
- Remove bootstrap credentials after first admin creation if your operational policy requires it.
- Keep Cloudflare and S3/R2 tokens scoped to the minimum permissions.
- Do not expose the Traefik dashboard publicly.
- Keep project containers off host-published ports; use Traefik routes.
- Keep DB-facing edge ports loopback unless public Postgres/pooler access is an explicit requirement.
- Treat service-role keys, DB passwords, SCIM tokens, and backup target secrets as production secrets.

## Troubleshooting

Admin cannot reach API:

- Check `VITE_API_BASE_URL`, `SUPADUPA_API_HOST`, and `SUPADUPA_CORS_ORIGINS`.
- Run `curl https://api.example.com/v1/health`.

TLS certificate does not issue:

- Confirm `CLOUDFLARE_API_TOKEN` is present in `.env`.
- Confirm DNS records point to the server.
- Check edge logs: `docker compose -f deploy/compose.yaml --profile edge logs -f edge-router`.

Project is degraded:

- Open the project runtime panel.
- Check `docker compose -p <ref> -f runtime/projects/<ref>/compose.yaml ps`.
- Check `docker compose -p <ref> -f runtime/projects/<ref>/compose.yaml logs`.

Advisor says recovery is not production ready:

- Add and test an off-host S3/R2 target.
- Restart with `SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true`.
- Restart with `SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true`.
- Run a disposable recovery compatibility profile.

## Repository Layout

```text
cmd/supadupa                 control-plane binary
cmd/supadupa-cli             CLI client
cmd/supadupa-mcp             MCP server
cmd/terraform-provider-*     Terraform/OpenTofu provider
frontend                     admin UI
internal/api                 Management API
internal/control             domain services and store contracts
internal/provisioner/compose Docker Compose project renderer/applier
internal/provisioner/kubernetes Kubernetes renderer
deploy                       Dockerfiles and Compose stack
migrations                   control-plane meta DB migrations
scripts/compat               compatibility suite
docs                         product, validation, and archived docs
```
