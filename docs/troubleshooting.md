# Troubleshooting

This guide covers common Supadupa MVP install and runtime issues.

## Admin UI Cannot Reach API

Symptoms:

- Admin shows API unreachable.
- Browser console shows CORS errors.
- Requests to `/v1/health` return 200 but browser blocks the response.

Check:

```text
VITE_API_BASE_URL
SUPADUPA_API_HOST
SUPADUPA_CORS_ORIGINS
```

For VPS mode:

```text
VITE_API_BASE_URL=https://api.example.com
SUPADUPA_CORS_ORIGINS=https://admin.example.com
```

Validate:

```bash
curl -i https://api.example.com/v1/health
docker compose -f deploy/compose.yaml logs -f supadupavisor
```

If the admin UI was built with the wrong `VITE_API_BASE_URL`, rebuild:

```bash
docker compose -f deploy/compose.yaml --profile edge up -d --build admin-ui
```

## Vite Blocks The Admin Host In Dev

Symptom:

```text
Blocked request. This host is not allowed.
```

Set:

```bash
export SUPADUPA_ADMIN_HOST=admin.example.com
```

Then restart the Vite dev server.

## TLS Certificate Does Not Issue

Check:

```text
CLOUDFLARE_API_TOKEN
SUPADUPA_ACME_EMAIL
SUPADUPA_ADMIN_HOST
SUPADUPA_API_HOST
SUPADUPA_APPS_DOMAIN
```

Confirm DNS points at the VPS:

```bash
dig admin.example.com
dig api.example.com
dig smoke.apps.example.com
```

Check Traefik:

```bash
docker compose -f deploy/compose.yaml --profile edge logs -f edge-router
```

## Project Is Degraded

Open the project runtime panel in the UI, then check the rendered stack:

```bash
docker compose -p <ref> -f runtime/projects/<ref>/compose.yaml ps
docker compose -p <ref> -f runtime/projects/<ref>/compose.yaml logs
```

Check route manifest:

```bash
cat runtime/routes/<ref>.yaml
```

Common causes:

- Project services still starting.
- Route manifest drift.
- Missing or invalid project config.
- Docker network not available.
- Host capacity not registered.
- Backup/recovery guard blocks an operation.

## Studio Bad Gateway

Check the Studio route:

```bash
curl -I https://studio-<ref>.apps.example.com
```

Then check the project stack:

```bash
docker compose -p <ref> -f runtime/projects/<ref>/compose.yaml ps
docker compose -p <ref> -f runtime/projects/<ref>/compose.yaml logs studio
```

If the route points to localhost in the UI, rebuild/restart the admin/API so runtime links use public route data.

## Project URLs Use The Wrong Domain

Check `Settings -> Defaults`. On fresh/default installs, startup seeds the domain from:

```text
SUPADUPA_APPS_DOMAIN
```

If an existing UI-configured default should be overwritten by env on restart, set:

```text
SUPADUPA_PROJECT_DOMAIN=apps.example.com
```

Then restart the API/container.

## Backup Target Does Not Appear

If using env bootstrap, at least one backup target env var must be set. For a full S3 target:

```text
SUPADUPA_BACKUP_TARGET_NAME=R2
SUPADUPA_BACKUP_TARGET_ENDPOINT=https://...
SUPADUPA_BACKUP_TARGET_REGION=auto
SUPADUPA_BACKUP_TARGET_BUCKET=supadupa-backups
SUPADUPA_BACKUP_TARGET_ACCESS_KEY_ID=...
SUPADUPA_BACKUP_TARGET_SECRET_ACCESS_KEY=...
```

Restart the API and check:

```bash
docker compose -f deploy/compose.yaml logs -f supadupavisor
```

You can also add targets manually in `Settings -> Backups`.

## Advisor Says Recovery Is Not Ready

Expected until all production recovery proof exists.

Actions:

1. Add an off-host S3/R2/remote-MinIO target.
2. Test the target.
3. Run project backups.
4. Validate WAL/physical backup posture.
5. Run disposable restore tests.
6. Enable recovery guards only after validation.

## Official Supabase CLI Typegen Fails On Public DB TLS

Use Supadupa's tunnel/wrapper flow:

```bash
go run ./cmd/supadupa-cli projects db-tunnel --ref <ref>
go run ./cmd/supadupa-cli projects gen-types --ref <ref>
```

Other official Supabase CLI DB workflows should use the DB URL from the project `Connect` page.

## Docker Network Missing

The setup helper creates:

```text
supadupa-ingress
```

Create it manually if needed:

```bash
docker network create supadupa-ingress
```

Then restart:

```bash
docker compose -f deploy/compose.yaml --profile edge up -d
```

## Compose Config Validation

Render the current Compose config:

```bash
docker compose -f deploy/compose.yaml config
```

Render with example env:

```bash
docker compose --env-file .env.example -f deploy/compose.yaml config
```
