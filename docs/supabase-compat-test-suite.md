# Supabase Compatibility Test Suite

This document defines the end-to-end test suite for proving that a Supadupa-provisioned project behaves like a Supabase project for developer workflows, SDKs, the official Supabase CLI where applicable, Studio, and the core data-plane services.

The goal is not only to smoke-test one route. The goal is to prove:

- Supadupa can provision an isolated project.
- Supadupa exposes correct connection metadata, secrets, routes, and Studio access.
- The official Supabase SDKs work against the project URLs.
- The official Supabase CLI works for workflows that can target a Supabase-compatible data plane.
- Supadupa's own CLI fills the management-plane gaps that the official Supabase CLI cannot perform without Supabase Cloud APIs.
- Every enabled Supabase data-plane surface is reachable, correctly authenticated, and isolated.

## Compatibility Contract

Supadupa has two API surfaces with different responsibilities.

| Surface | Owner | Expected compatibility |
|---------|-------|------------------------|
| Data plane | Upstream Supabase stack, provisioned per project | Supabase-compatible HTTP APIs, Postgres, pooler, Storage, Auth, Realtime, GraphQL, Edge Runtime, Studio |
| Management plane | Supadupa | Supadupa API/CLI for project lifecycle, secrets, RBAC, backups, metrics, routes, domains, config, branches, replicas, audit |
| Official Supabase CLI | Upstream Supabase CLI | Supported for commands that can use `--db-url`, `SUPABASE_URL`, service keys, anon keys, or local files |
| Supabase Cloud API workflows | Supabase hosted platform | Not compatible unless Supadupa implements an equivalent API or a Supadupa wrapper |

The test suite must mark each official CLI command as one of:

- `pass`: works through Supadupa today.
- `pass-with-db-route`: works after a reachable DB URL or tunnel is provided.
- `supadupa-wrapper`: use `supadupa-cli` because the command is management-plane behavior.
- `not-applicable`: depends on Supabase hosted cloud APIs or hosted-only infrastructure.
- `fail`: should work but does not.

## Required Test Environments

Run the suite in three environments because each catches different failures.

| Environment | Purpose |
|-------------|---------|
| Local native dev | Go API and frontend run natively; meta DB, edge router, and project stacks run in containers |
| Public DNS Compose host | Real wildcard DNS, Traefik, TLS, Studio SSO, public project URLs |
| Internal runtime network | Tests that intentionally run inside the Docker network where internal DB and pooler hostnames resolve |

The public DNS environment should use:

```txt
admin.<control-domain>
api.<control-domain>
*.<apps-domain>
```

Example:

```txt
admin.supadupa.brotechlabs.com
api.supadupa.brotechlabs.com
*.apps.supadupa.brotechlabs.com
```

## Required Tools

Install these on the test runner:

```bash
docker --version
docker compose version
go version
node --version
npm --version
curl --version
openssl version
psql --version
supabase --version
```

Useful optional tools:

```bash
jq --version
websocat --version
deno --version
```

If `jq` or `websocat` is missing, the test runner can use Node scripts instead.

## Test Project

Use a disposable project ref for each full run:

```bash
export SUPADUPA_API_URL="https://api.<control-domain>"
export SUPADUPA_ADMIN_URL="https://admin.<control-domain>"
export SUPADUPA_APPS_DOMAIN="apps.<control-domain>"
export SUPADUPA_TEST_REF="compat-$(date +%s)"
export SUPADUPA_TEST_NAME="Compatibility Test"
```

Project refs are public DNS labels. They must be 3-55 lowercase letters, numbers, or hyphens, cannot start or end with a hyphen, and must keep generated labels such as `storage-<ref>`, `db-<ref>`, `pooler-<ref>`, and `studio-<ref>` at or below 63 characters. The apps domain plus generated project hosts must also stay within the 253-character DNS name limit. Read-replica names are also DNS labels and cannot start or end with a hyphen; read-replica hosts add both a replica name and the ref in `db-replica-<replica>-<ref>`, so replica creation must reject name/ref combinations whose generated host label would exceed 63 characters or whose full generated FQDN would exceed 253 characters.

Generated project hosts must not claim platform or custom-domain hosts. For example, `ref=admin` with `domain=example.com` would generate `admin.example.com` and must be rejected because that host belongs to the control plane topology; creating a new project whose generated API host matches an existing custom domain must also be rejected.

For the current deployed example:

```bash
export SUPADUPA_API_URL="https://api.supadupa.brotechlabs.com"
export SUPADUPA_ADMIN_URL="https://admin.supadupa.brotechlabs.com"
export SUPADUPA_APPS_DOMAIN="apps.supadupa.brotechlabs.com"
export SUPADUPA_TEST_REF="compat-$(date +%s)"
```

Never write raw secrets to the repo. Store generated env files under a temporary directory such as `/tmp/supadupa-compat/<ref>`.

## Suite Layout

The preferred implementation is a single scriptable suite:

```txt
scripts/compat/
  run.sh
  lib.sh
  00-preflight.sh
  01-create-project.sh
  01-auth-project.sh
  02-cli-profile.sh
  02-rest-auth.sh
  03-postgres.sh
  04-db-fixture.sh
  09-supabase-cli-classification.sh
  09-supabase-cli-db.sh
  09-supabase-cli-typegen.sh
  09-supabase-cli-matrix.sh
  04-gen-types.sh
  05-function-fixture.sh
  05-http-surfaces.sh
  18-storage-s3.sh
  06-realtime.sh
  22-auth-deep.sh
  23-storage-deep.sh
  24-realtime-deep.sh
  25-functions-deep.sh
  26-replicas-deep.sh
  29-branches-deep.sh
  08-sdk-js.sh
  11-metrics.sh
  12-backup-restore.sh
  16-recoverability-pitr.sh
  07-isolation.sh
  14-public-exposure.sh
  13-security-boundaries.sh
  17-studio-auth.sh
  10-upgrade-matrix.sh
  99-cleanup.sh
  future/
    migration-sdk.sh
    observability.sh
    security.sh
  fixtures/
    functions/hello/index.ts
    sql/001_schema.sql
    sql/002_rls.sql
    clients/sdk-js.mjs
    clients/realtime.mjs
```

Each test file should:

- fail fast on hard failures;
- print a short `PASS`, `FAIL`, or `SKIP` line per assertion;
- write machine-readable results to `/tmp/supadupa-compat/<ref>/results.jsonl`;
- avoid printing raw secrets;
- clean up the disposable project unless `SUPADUPA_COMPAT_KEEP_PROJECT=true`.

The implemented runner supports both existing-project and disposable-project modes:

```bash
export SUPADUPA_COMPAT_CREATE_PROJECT=true
export SUPADUPA_TEST_ORG_ID="<org-id>"
export SUPADUPA_STACK_VERSION="<older-stable-version>"
export SUPADUPA_COMPAT_UPGRADE_MATRIX=true
scripts/compat/run.sh
```

When upgrade matrix mode is enabled, the selected project is intentionally mutated. The phase triggers a pre-upgrade backup, verifies restore-critical metadata, upgrades with that backup ID, verifies public health, and reruns REST, direct Postgres, DB fixture, official Supabase CLI migration push, type generation, HTTP surfaces, Functions, Realtime, and Supabase JS SDK checks after each target version. If `SUPADUPA_UPGRADE_TARGETS` is not set, the matrix fetches `/v1/stack-releases` and uses the newest exposed stable release as the target. Production-grade upgrade validation should start the control plane with `SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true` and provide a tested durable off-host target so local-only pre-upgrade backups cannot pass.

Official Supabase CLI validation runs the configured `SUPADUPA_SUPABASE_CLI`, defaulting to `npx -y supabase@latest`. The runner probes official `supabase gen types typescript --db-url`; a successful public-DB run proves the upstream BYO-domain TLS caveat is fixed, while a certificate/TLS failure is recorded as a known upstream caveat and followed by an official CLI typegen retry through `supadupa-cli projects db-tunnel`. Set `SUPADUPA_SUPABASE_CLI_MATRIX="latest 2.105.0"` to rerun classification, DB workflows, official typegen probing, tunnel fallback, and Supadupa wrapper typegen across latest and pinned CLI versions under isolated artifacts.

## Phase 0: Repo and Runtime Preflight

### 0.1 Unit and Build Tests

Commands:

```bash
go test ./...
npm --prefix frontend run build
```

Expected:

- Go tests pass.
- Frontend builds.
- No generated runtime files are required for unit tests.

### 0.2 Platform Health

Commands:

```bash
curl -fsS "$SUPADUPA_API_URL/v1/health"
curl -fsSI "$SUPADUPA_ADMIN_URL/"
docker ps --format '{{.Names}}\t{{.Status}}'
```

Expected:

- Management API returns status `ok`.
- Admin UI returns HTTP 200.
- Edge router and meta DB containers are healthy or running.

### 0.3 Wildcard DNS and TLS

Commands:

```bash
dig +short "probe-$SUPADUPA_TEST_REF.$SUPADUPA_APPS_DOMAIN"
openssl s_client -connect "probe-$SUPADUPA_TEST_REF.$SUPADUPA_APPS_DOMAIN:443" \
  -servername "probe-$SUPADUPA_TEST_REF.$SUPADUPA_APPS_DOMAIN" </dev/null
```

Expected:

- DNS resolves to the public host.
- TLS certificate is valid for the wildcard apps domain.
- A route may return 404 before project creation, but TLS should terminate.

## Phase 1: Control Plane Project Lifecycle

### 1.1 Login

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" login \
  --email "$SUPADUPA_TEST_EMAIL" \
  --password "$SUPADUPA_TEST_PASSWORD"
```

Expected:

- CLI returns a bearer token or writes one to the configured session path.
- Token can call authenticated endpoints.

### 1.2 Create Project

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects create \
  --org-id "$SUPADUPA_TEST_ORG_ID" \
  --ref "$SUPADUPA_TEST_REF" \
  --name "$SUPADUPA_TEST_NAME" \
  --domain "$SUPADUPA_APPS_DOMAIN" \
  --profile full
```

Expected:

- Project is created in the meta DB.
- Project status eventually reaches `healthy`.
- Runtime containers are created in an isolated Compose project.
- Traefik route file is rendered for API, Studio, and dedicated Storage S3.
- Project receives a host assignment and resource reservation.

### 1.3 Status Convergence

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects get \
  --ref "$SUPADUPA_TEST_REF"
```

Expected:

- `runtime_phase=healthy`.
- API URL is `https://<ref>.<apps-domain>`.
- Studio URL is `https://studio-<ref>.<apps-domain>`.
- No `localhost` URLs appear in public project links.
- Drift is empty or contains only known transient startup checks.

## Phase 2: CLI Profile

### 2.0 Admin Connect Payload

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects connect \
  --ref "$SUPADUPA_TEST_REF" > "/tmp/supadupa-compat/$SUPADUPA_TEST_REF/connect.json"
```

Expected:

- Public API, Studio, REST, Auth, GraphQL, Realtime, Functions, Storage, and Storage S3 URLs use HTTPS project domains.
- Storage REST stays under `https://<ref>.<apps-domain>/storage/v1`; Storage S3 uses `https://storage-<ref>.<apps-domain>/storage/v1/s3`.
- Default connection snippets are remote-first and use `db-<ref>.<apps-domain>` and `pooler-<ref>.<apps-domain>` with `sslmode=require`.
- Public Postgres parts include `public_direct`, `public_transaction`, and `public_session` with public hosts and TLS-required metadata.
- When a project has custom domains, `custom_domains` lists their certificate metadata and `custom_api_urls` lists only domains with an issued or uploaded certificate.
- Internal Docker-network URLs may exist only under explicit `internal` keys.
- Public snippets, links, commands, and profile fields do not leak `localhost`, `host.docker.internal`, or `.internal` values.

### 2.1 JSON Profile

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects cli-profile \
  --ref "$SUPADUPA_TEST_REF" \
  --format json > "/tmp/supadupa-compat/$SUPADUPA_TEST_REF/profile.json"
```

Expected JSON fields:

```txt
project_ref
api_url
studio_url
rest_url
auth_url
graphql_url
realtime_url
functions_url
storage_url
storage_s3_url
custom_domains
custom_api_urls
database_url
pooler_transaction_url
pooler_session_url
env
supabase_config_toml
commands
secret_handles
compatibility_contracts
```

Expected:

- HTTP URLs use public HTTPS project domains.
- In public DNS deployments, default DB URLs use public TLS hosts: `db-<ref>.<apps-domain>` for direct Postgres and `pooler-<ref>.<apps-domain>` for pooler connections.
- Internal DB URLs may exist only in explicit internal fields for runtime-network diagnostics.
- `commands.supadupa_gen_types` exposes the supported BYO-domain type generation command for the project.
- `compatibility_contracts.typegen` explains that Supadupa type generation wraps postgres-meta when upstream `supabase gen types --db-url` rewrites DB TLS settings.
- Ready custom API domains appear in `custom_api_urls`, profile env as `SUPADUPA_CUSTOM_API_URL(S)`, and TOML as `custom_api_urls`, while canonical `api_url` and default `SUPABASE_URL` remain the generated `https://<ref>.<apps-domain>` URL.
- Secret values are not exposed in the profile.
- Secret handles use `secret://projects/<ref>/...`.

### 2.2 Env Export

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects cli-profile \
  --ref "$SUPADUPA_TEST_REF" \
  --format env > "/tmp/supadupa-compat/$SUPADUPA_TEST_REF/supabase.env"
```

Expected:

- File contains `SUPABASE_URL`.
- File contains `SUPABASE_DB_URL`.
- File contains secret handles for sensitive values, not raw secrets.
- Shell quoting survives `source` without syntax errors.
- `--api-domain <fqdn>` and `--prefer-custom-domain` can emit a ready custom domain as `SUPABASE_URL` and record `SUPADUPA_SELECTED_API_URL`.

### 2.3 TOML Export

Commands:

```bash
mkdir -p "/tmp/supadupa-compat/$SUPADUPA_TEST_REF/supabase"
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects cli-profile \
  --ref "$SUPADUPA_TEST_REF" \
  --format toml > "/tmp/supadupa-compat/$SUPADUPA_TEST_REF/supabase/config.toml"
```

Expected:

- `project_id` equals the project ref.
- `[supadupa]` metadata exists.
- Ready custom API domains appear as `custom_api_urls` when configured.
- `[supadupa.secret_handles]` exists.
- TOML can be parsed by a standard TOML parser.

### 2.4 Secret Reveal

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" secrets reveal \
  --ref "$SUPADUPA_TEST_REF" \
  --kind db_password
```

Expected:

- Authorized users can reveal secrets.
- Unauthorized users cannot reveal secrets.
- Secret reveal is audited.
- The CLI and UI never reveal secrets automatically as part of normal profile export.

### 2.5 Workspace Binding

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects link \
  --ref "$SUPADUPA_TEST_REF" \
  --dir "/tmp/supadupa-compat/$SUPADUPA_TEST_REF/workspace"

supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects env \
  --ref "$SUPADUPA_TEST_REF" \
  --out "/tmp/supadupa-compat/$SUPADUPA_TEST_REF/workspace/.supadupa/supabase.env"
```

Expected:

- `.supadupa/project.json` records the project ref, API URL, Studio URL, Management API URL, and link time.
- `.supadupa/config.toml` contains the Supabase-compatible CLI TOML profile.
- `.supadupa/supabase.env` contains handle-only env by default.
- `projects link --api-domain <fqdn>` and `projects env --prefer-custom-domain` can materialize a ready custom domain as `SUPABASE_URL`; `.supadupa/project.json` records `custom_api_urls` and `selected_api_url` when a custom domain is selected.
- `supabase/config.toml` is created when missing so official Supabase CLI database commands can run from the linked workspace without a separate profile export; an existing `supabase/config.toml` must not be overwritten.
- `projects env --reveal-secrets` performs audited reveals for known secret handles and materializes DB URL password placeholders, but this mode is opt-in and not used by normal profile export.

### 2.6 Terraform Connection Data Source

Expected:

- `data "supadupa_project_connect"` fetches `/v1/projects/<ref>/connect` for existing projects.
- The data source exposes generated API, Studio, REST, Auth, GraphQL, Realtime, Functions, Storage, Storage S3, public direct Postgres, transaction pooler, and session pooler URLs.
- The data source exposes ready `custom_api_urls` and secret handles without revealing raw API keys or database passwords.
- Two project refs must return distinct generated and custom endpoint sets.

## Phase 3: Official Supabase CLI Compatibility

The official Supabase CLI has two classes of commands:

- Data-plane commands that can target a database URL or local project files.
- Hosted-platform commands that assume Supabase Cloud management APIs.

The suite must prove the former and classify the latter.

### 3.1 CLI Version Capture

Commands:

```bash
supabase --version
supabase --help
```

Expected:

- Version is recorded in results.
- Help output is saved to the artifact directory so compatibility can be re-audited when the CLI changes.

### 3.2 Database URL Reachability Gate

Before running DB CLI tests, classify DB access mode:

| Mode | Description | Expected |
|------|-------------|----------|
| `internal` | DB URL uses `db.<ref>.internal` | Official CLI DB tests must run from inside project/runtime network |
| `tunnel` | Supadupa opens a local DB tunnel | Official CLI DB tests run from developer machine against `127.0.0.1:<port>` |
| `public-tcp` | Supadupa exposes TLS TCP Postgres/pooler route | Official CLI DB tests run from developer machine against public DB hostname |

Validated production-style deployments use `public-tcp` through Traefik for hosted-style remote access. Dry-run or local-only deployments may still use `internal`.

Expected:

- If no reachable DB URL is available, mark DB CLI tests `SKIP: db route required`.
- Do not expose public Postgres by default just to make the test pass.

### 3.3 `supabase db pull`

Commands:

```bash
export DB_PASSWORD="<revealed-db-password>"
export SUPABASE_DB_URL="postgres://postgres:${DB_PASSWORD}@<reachable-db-host>:5432/postgres?sslmode=require"
supabase db pull --db-url "$SUPABASE_DB_URL"
```

Expected:

- Command connects to the project database.
- Generated migration reflects the project schema.
- No schema from another project appears.

### 3.4 `supabase db push`

Fixture SQL:

```sql
create schema if not exists compat;
create table if not exists compat.cli_probe (
  id uuid primary key default gen_random_uuid(),
  inserted_at timestamptz not null default now(),
  note text not null
);
```

Commands:

```bash
supabase db push --db-url "$SUPABASE_DB_URL"
psql "$SUPABASE_DB_URL" -c "select count(*) from compat.cli_probe;"
```

Expected:

- Migration applies.
- Table is visible through Postgres.
- Re-running is idempotent or fails only on intentionally non-idempotent fixture SQL.

### 3.5 Type Generation

Commands:

```bash
supabase gen types typescript --db-url "$SUPABASE_DB_URL" > database.types.ts
```

Supadupa wrapper command:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" \
  projects gen-types --ref "$PROJECT_REF" --out database.types.supadupa.ts
```

Expected:

- Type output includes `compat.cli_probe`.
- Command is classified based on the installed CLI help. If the installed CLI no longer supports `--db-url`, mark as `supadupa-wrapper-needed`.
- If the official CLI fails because of upstream CA/DB URL rewriting behavior, `09-supabase-cli-typegen.sh` must record `supabase_cli.typegen.official_upstream_tls_caveat`, then prove `supabase_cli.typegen.official_tunnel` through `supadupa-cli projects db-tunnel`; `projects gen-types` must still pass through postgres-meta and produce equivalent project types.
- `09-supabase-cli-matrix.sh` can repeat the official typegen probe across latest and pinned CLI versions via `SUPADUPA_SUPABASE_CLI_MATRIX`.
- The wrapper must not place the secret-bearing DB URL in Docker process arguments.

### 3.6 Hosted Management Commands

Classify these commands instead of expecting pass-by-default:

| Command family | Expected classification |
|----------------|-------------------------|
| `supabase projects *` | `supadupa-wrapper` |
| `supabase link` | `not-applicable` or `supadupa-wrapper` |
| `supabase branches *` | `supadupa-wrapper` |
| `supabase secrets *` | `supadupa-wrapper` unless targeting local function env only |
| `supabase functions deploy` | `supadupa-wrapper` until Supadupa implements equivalent deploy target |
| `supabase db * --db-url` | `pass` or `pass-with-db-route` |

Expected:

- The suite records this matrix clearly so failures are not confused with missing data-plane compatibility.

## Phase 4: Supabase HTTP API and SDK Tests

Use the generated profile and revealed anon/service keys.

Required env:

```bash
export SUPABASE_URL="https://$SUPADUPA_TEST_REF.$SUPADUPA_APPS_DOMAIN"
export SUPABASE_ANON_KEY="<revealed-anon-key>"
export SUPABASE_SERVICE_ROLE_KEY="<revealed-service-role-key>"
```

### 4.1 Gateway Health

Commands:

```bash
curl -fsS "$SUPABASE_URL/rest/v1/" \
  -H "apikey: $SUPABASE_ANON_KEY"
curl -fsS "$SUPABASE_URL/auth/v1/health"
```

Expected:

- Kong routes REST and Auth.
- No backend service is reachable directly through an unintended public port.

### 4.2 JavaScript SDK

Fixture:

```js
import { createClient } from "@supabase/supabase-js";

const supabase = createClient(process.env.SUPABASE_URL, process.env.SUPABASE_ANON_KEY);
const { data, error } = await supabase.from("compat_public_probe").select("*").limit(1);
if (error) throw error;
console.log(JSON.stringify(data));
```

Expected:

- SDK initializes.
- REST queries work.
- Errors are normal Supabase client errors, not route/TLS/CORS failures.

### 4.3 CORS

Commands:

```bash
curl -fsSI "$SUPABASE_URL/rest/v1/" \
  -H "Origin: $SUPADUPA_ADMIN_URL" \
  -H "apikey: $SUPABASE_ANON_KEY"
```

Expected:

- Browser-compatible CORS headers are returned where Supabase services require them.
- Admin origin is not accidentally trusted for privileged project APIs without keys.

## Phase 5: Auth

### 5.1 Signup and Login

Commands:

```bash
curl -fsS "$SUPABASE_URL/auth/v1/signup" \
  -H "apikey: $SUPABASE_ANON_KEY" \
  -H "Content-Type: application/json" \
  -d '{"email":"compat-user@example.test","password":"CorrectHorseBatteryStaple123!"}'

curl -fsS "$SUPABASE_URL/auth/v1/token?grant_type=password" \
  -H "apikey: $SUPABASE_ANON_KEY" \
  -H "Content-Type: application/json" \
  -d '{"email":"compat-user@example.test","password":"CorrectHorseBatteryStaple123!"}'
```

Expected:

- Signup succeeds or returns the expected email-confirmation behavior based on project config.
- Password grant returns access and refresh tokens when confirmation is disabled or user is confirmed.
- JWT verifies against project signing material.

### 5.2 User Endpoint

Commands:

```bash
curl -fsS "$SUPABASE_URL/auth/v1/user" \
  -H "apikey: $SUPABASE_ANON_KEY" \
  -H "Authorization: Bearer $USER_ACCESS_TOKEN"
```

Expected:

- Returns the authenticated user.
- Invalid token is rejected.

### 5.3 Service Role Admin

Commands:

```bash
curl -fsS "$SUPABASE_URL/auth/v1/admin/users" \
  -H "apikey: $SUPABASE_SERVICE_ROLE_KEY" \
  -H "Authorization: Bearer $SUPABASE_SERVICE_ROLE_KEY"
```

Expected:

- Service role can access admin endpoints.
- Anon key cannot access admin endpoints.

### 5.4 SMS Provider Runtime Config

Runner phase: `scripts/compat/22-auth-deep.sh`.

Expected:

- MFA remains optional and is intentionally skipped unless `SUPADUPA_COMPAT_AUTH_MFA_VALIDATE=true`.
- SMS provider config persists, validates, resolves secret handles, and renders to the project GoTrue runtime for OTP expiration, OTP length, max send frequency, SMS template, and test OTP values.
- Twilio, Twilio Verify, MessageBird, TextLocal, and Vonage provider config paths are represented without storing raw secrets in management-plane responses.
- Real third-party SMS delivery validation is opt-in with `SUPADUPA_COMPAT_AUTH_REAL_SMS_VALIDATE=true`; the runner stores provider credentials as temporary Supadupa secret handles, configures the requested provider, sends a real OTP to `SUPADUPA_COMPAT_SMS_PHONE`, and verifies it from `SUPADUPA_COMPAT_SMS_OTP_COMMAND` or `SUPADUPA_COMPAT_SMS_OTP_FILE`.
- Remaining hosted-parity coverage requires provider-credential CI runs for real SMS delivery plus Teams/Enterprise Auth Hook validation where the upstream GoTrue runtime supports those hooks.

## Phase 6: Database, REST, and RLS

### 6.1 Schema Setup

Use Postgres direct, pooler transaction, or a Supadupa DB tunnel.

Fixture SQL:

```sql
create table if not exists public.compat_public_probe (
  id uuid primary key default gen_random_uuid(),
  owner_id uuid,
  note text not null,
  inserted_at timestamptz not null default now()
);

alter table public.compat_public_probe enable row level security;

drop policy if exists compat_select_own on public.compat_public_probe;
create policy compat_select_own
  on public.compat_public_probe
  for select
  using (owner_id = auth.uid());

drop policy if exists compat_insert_own on public.compat_public_probe;
create policy compat_insert_own
  on public.compat_public_probe
  for insert
  with check (owner_id = auth.uid());
```

Expected:

- Schema applies.
- RLS is enabled.
- Toggling a database extension through Supadupa Management API `/v1/projects/<ref>/database/extensions/<name>` executes live `CREATE EXTENSION` or `DROP EXTENSION` DDL when runtime apply mode is enabled.
- Failed extension DDL returns conflict and rolls back the visible extension state to the previous value.
- Creating an active cron job through Supadupa Management API `/v1/projects/<ref>/database/cron-jobs` schedules it in live pg_cron, and deleting it removes the live `cron.job` row.
- Creating an active queue through Supadupa Management API `/v1/projects/<ref>/database/queues` creates live pgmq queue tables, including the dead-letter queue when configured, and deleting it drops those live queues.
- Creating an active declarative schema through Supadupa Management API `/v1/projects/<ref>/database/schemas` executes the SQL against the same live project Postgres when runtime apply mode is enabled.
- Failed live SQL through the Management API returns conflict and rolls back the schema metadata record.
- Creating a database role through Supadupa Management API `/v1/projects/<ref>/database/roles` executes live Postgres DDL for role flags, memberships, schema grants, and table grants when runtime apply mode is enabled.
- Login role passwords must resolve from revealable `secret://projects/<ref>/<kind>` handles; cross-project, nested, or missing handles return conflict and roll back metadata.
- Deleting a database role through the Management API drops the live role before removing metadata, so dependent-object failures remain visible instead of silently orphaning live state.

### 6.2 REST Anonymous Access

Commands:

```bash
curl -i "$SUPABASE_URL/rest/v1/compat_public_probe?select=*" \
  -H "apikey: $SUPABASE_ANON_KEY"
```

Expected:

- Request succeeds.
- No rows are returned unless policy permits them.

### 6.3 REST Authenticated Access

Commands:

```bash
curl -fsS "$SUPABASE_URL/rest/v1/compat_public_probe" \
  -H "apikey: $SUPABASE_ANON_KEY" \
  -H "Authorization: Bearer $USER_ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"owner_id":"<user-id>","note":"rls probe"}'
```

Expected:

- Authenticated user can insert its own row.
- Different user cannot select or update that row.
- Service role bypasses RLS.

### 6.4 Pooler

Commands:

```bash
psql "$SUPABASE_DB_URL" -c "select current_database();"
psql "$SUPABASE_POOLER_TRANSACTION_URL" -c "select current_database();"
psql "$SUPABASE_POOLER_SESSION_URL" -c "select current_database();"
```

Expected:

- Direct and pooled connections succeed in the configured DB access mode.
- Pooler user format matches the connection profile.

## Phase 7: GraphQL

Commands:

```bash
curl -fsS "$SUPABASE_URL/graphql/v1" \
  -H "apikey: $SUPABASE_ANON_KEY" \
  -H "Authorization: Bearer $USER_ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"query":"query { compat_public_probeCollection { edges { node { id note } } } }"}'
```

Expected:

- GraphQL endpoint responds.
- Schema includes the test table after cache refresh.
- RLS behavior matches REST for anon/auth/service role.

## Phase 8: Storage

### 8.1 Bucket CRUD

Commands:

```bash
curl -fsS "$SUPABASE_URL/storage/v1/bucket" \
  -H "Authorization: Bearer $SUPABASE_SERVICE_ROLE_KEY" \
  -H "Content-Type: application/json" \
  -d '{"id":"compat-public","name":"compat-public","public":true}'
```

Expected:

- Bucket is created.
- Duplicate create returns expected Supabase Storage error.
- Creating and deleting a bucket through Supadupa Management API `/v1/projects/<ref>/storage/buckets` applies to the same live Storage service when runtime apply mode is enabled; a failed live create must roll back the Management API metadata record.

### 8.2 Object Upload and Download

Commands:

```bash
printf 'hello from supadupa compat\n' > /tmp/supadupa-compat-object.txt

curl -fsS "$SUPABASE_URL/storage/v1/object/compat-public/probe.txt" \
  -H "Authorization: Bearer $SUPABASE_SERVICE_ROLE_KEY" \
  -H "Content-Type: text/plain" \
  --data-binary @/tmp/supadupa-compat-object.txt

curl -fsS "$SUPABASE_URL/storage/v1/object/public/compat-public/probe.txt"
```

Expected:

- Upload succeeds.
- Public download returns the uploaded bytes.
- Private bucket access requires valid token.

### 8.3 Signed URL

Commands:

```bash
curl -fsS "$SUPABASE_URL/storage/v1/object/sign/compat-public/probe.txt" \
  -H "Authorization: Bearer $SUPABASE_SERVICE_ROLE_KEY" \
  -H "Content-Type: application/json" \
  -d '{"expiresIn":3600}'
```

Expected:

- Signed URL is returned.
- Signed URL downloads object.

### 8.4 S3 Compatibility

Runner phase: `scripts/compat/18-storage-s3.sh`.

Expected:

- If S3 keys are implemented and revealable, test SigV4 list-buckets, put-object with metadata, head-object metadata, get-object, byte-range get-object, presigned get-object, list-objects, copy-object, and delete-object through `storage_s3_url` on `storage-<ref>.<apps-domain>`.
- If S3 keys are still secret handles only, mark `SKIP: S3 secret material not available`.

### 8.5 Owner RLS Isolation

Expected:

- A private bucket with `storage.objects` policies scoped to `owner = auth.uid()` lets an authenticated user upload and read its own object.
- A second authenticated user cannot read the first user's object; accept the Storage runtime's normal hidden-denial statuses such as `400`, `401`, `403`, or `404`.
- The second user can upload and read its own object in the same bucket.
- The first user cannot read the second user's object.

## Phase 9: Realtime

### 9.1 WebSocket Connect

Commands:

```bash
websocat "wss://$SUPADUPA_TEST_REF.$SUPADUPA_APPS_DOMAIN/realtime/v1/websocket?apikey=$SUPABASE_ANON_KEY&vsn=1.0.0"
```

Expected:

- WebSocket upgrade succeeds through Traefik and Kong.
- Invalid apikey fails.

### 9.2 Broadcast

Expected:

- Subscribe to a private or public channel.
- Send broadcast event.
- Receive event on another client.
- Authorization behavior matches Realtime config.

### 9.3 Postgres Changes

Expected:

- Enable publication for the test table if needed.
- Subscribe to insert events.
- Insert a row through REST or SQL.
- Receive one Realtime event with correct payload.

### 9.4 Private Channel User Isolation

Expected:

- A policy on `realtime.messages` can allow `compat-user-<auth.uid()>` topics for authenticated users.
- User A can subscribe to `compat-user-<user-a-id>` with `private: true`.
- User B cannot subscribe to User A's private topic.
- User B can subscribe to `compat-user-<user-b-id>`.
- Anonymous clients cannot subscribe to either user's private topic.

## Phase 10: Edge Functions

### 10.1 Supadupa Function Deploy

Fixture:

```ts
Deno.serve(async (req) => {
  const body = {
    ok: true,
    method: req.method,
    path: new URL(req.url).pathname,
    env: Deno.env.get("COMPAT_FUNCTION_SECRET") ? "present" : "missing",
  };
  return new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json" },
  });
});
```

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" functions deploy \
  --ref "$SUPADUPA_TEST_REF" \
  --name hello \
  --entrypoint fixtures/functions/hello/index.ts
```

Expected:

- Function artifact is written under the project runtime functions directory.
- Edge Runtime sees the function without exposing local host paths publicly.
- Function appears in Supadupa API/UI.

### 10.2 Function Invoke

Commands:

```bash
curl -fsS "$SUPABASE_URL/functions/v1/hello" \
  -H "Authorization: Bearer $SUPABASE_ANON_KEY"
```

Expected:

- Function returns JSON.

### 10.3 Function Storage Mounts

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" storage-buckets create \
  --ref "$SUPADUPA_TEST_REF" \
  --name compat-fn-mount

supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" functions mount \
  --ref "$SUPADUPA_TEST_REF" \
  --function hello \
  --bucket compat-fn-mount \
  --mount-path /mnt/compat-fn-mount \
  --prefix public \
  --env-alias COMPAT_FN_MOUNT \
  --read-only=true
```

Expected:

- Function can read the mounted object through the environment alias.

### 10.4 Regional Invocation

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" functions region \
  --ref "$SUPADUPA_TEST_REF" \
  --function hello \
  --region us-east-1 \
  --routing-policy nearest

curl -fsS "$SUPABASE_URL/functions/v1/hello" \
  -H "apikey: $SUPABASE_ANON_KEY" \
  -H "x-region: us-east-1"

curl -fsS "$SUPABASE_URL/functions/v1/hello?forceFunctionRegion=us-east-1" \
  -H "apikey: $SUPABASE_ANON_KEY"
```

Expected:

- Region declaration appears in the Management API list and runtime desired-state artifact.
- Function sees `SB_REGION=us-east-1`.
- Function response includes `x-sb-edge-region: us-east-1`.
- Region deletion removes the desired-state artifact.
- Function can read multiple nested objects materialized from the mounted prefix.
- Objects outside the configured mount prefix are not visible through the mount.
- Function cannot write through a read-only mount.
- A failed mount write does not mutate the origin Storage object.
- Route goes through project Kong and edge runtime.
- Auth-required and no-verify-JWT modes behave according to function config.

### 10.3 Function Secrets

Expected:

- Set function secret through Supadupa.
- Redeploy or restart as required by runtime behavior.
- Invoke function and confirm secret is present.
- Secret value is not exposed in function metadata or logs.

### 10.4 Official Supabase Function Deploy Classification

Commands:

```bash
supabase functions new compat-official
supabase functions deploy compat-official
```

Expected:

- Unless Supadupa has implemented a compatible hosted deployment API, classify as `supadupa-wrapper`.
- Do not call this a data-plane failure.

## Phase 11: Studio

### 11.1 Public Studio Route

Commands:

```bash
curl -fsSI "https://studio-$SUPADUPA_TEST_REF.$SUPADUPA_APPS_DOMAIN/"
```

Expected:

- Route is reachable.
- TLS is valid.
- Studio is protected by Supadupa SSO/forward-auth.
- Unauthenticated requests are redirected or rejected.

### 11.2 Authenticated Studio

Use Playwright or browser automation.

Expected:

- Supadupa admin login grants Studio access.
- Studio can load project metadata.
- Studio API calls are routed to the correct project.
- Studio REST docs and GraphQL explorer links use `/project/<ref>/...`, not the self-hosted `default` alias, so many separately hosted project Studios are unambiguous.
- No link points to `localhost`.
- No Studio route bypasses Supadupa auth.
- Custom domains cannot claim platform hosts or generated project hosts such as `admin.<control-domain>`, `api.<control-domain>`, `<ref>.<apps-domain>`, `studio-<ref>.<apps-domain>`, `storage-<ref>.<apps-domain>`, `db-<ref>.<apps-domain>`, `pooler-<ref>.<apps-domain>`, branch-generated hosts, or `db-replica-<replica>-<ref>.<apps-domain>` replica hosts.
- After a custom domain has an issued or uploaded certificate, `/connect`, `/connect/cli`, the Supadupa CLI profile, and the Admin UI Connect page expose it as a ready custom API URL while keeping `https://<ref>.<apps-domain>` as the canonical generated `api_url`.

### 11.3 Admin Project Page Coexistence

Expected:

- Supadupa project admin page remains the control-plane surface.
- Studio remains the per-project data-plane workspace.
- Actions that belong to Supadupa, such as lifecycle, capacity, backups, RBAC, domains, and audit, are not duplicated as authoritative Studio actions.

## Phase 12: Observability

### 12.1 Dashboard Metrics

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" metrics fleet
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" metrics project --ref "$SUPADUPA_TEST_REF"
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" metrics --prometheus
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" usage current --org-id "$SUPADUPA_TEST_ORG_ID"
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" usage snapshots --org-id "$SUPADUPA_TEST_ORG_ID" --limit 5
```

Expected:

- Host capacity is reported.
- Project reservations and fresh telemetry are reported.
- Fleet reservations, host capacity, fresh telemetry, audit status, and Prometheus metrics are reported.
- Dashboard shows separate live usage and reserved capacity sections with minimal project rows.

### 12.2 Project Logs

Expected:

- API/UI can fetch project logs.
- Function invocation appears in logs.
- Auth/REST/Storage failures produce useful service logs.
- Secret values are redacted.

### 12.3 Audit

Expected audited events:

- Project create.
- Secret reveal.
- Function deploy.
- Config update.
- Studio access verification if tracked.
- Project destroy.

### 12.4 Provider-Backed Service Configs

Runner phase: `scripts/compat/28-provider-configs.sh`.

Expected:

- Log drains can be declared through the Management API/CLI when `log_drains` is enabled, and generated drain config is cleaned up on delete.
- OAuth auth clients can be registered and deleted; confidential clients reject raw secret handles and return masked `client_secret_handle` values.
- Replication pipelines can be declared and deleted; raw sensitive destination config is rejected and sensitive config is masked on create/list responses.
- Embedding jobs can be declared and deleted.
- S3-backed vector buckets can be declared and deleted; raw sensitive metadata is rejected and sensitive metadata is masked on create/list responses.
- Private network connection declarations can be created and deleted when `network_restrictions` is enabled; raw sensitive config is rejected and sensitive config is masked.
- Project metrics counters include the created declarations before cleanup.
- Any temporary org feature overrides are restored, and post-delete lists confirm the compat-created resources are gone.

## Phase 13: Lifecycle and Isolation

### 13.1 Restart

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects restart \
  --ref "$SUPADUPA_TEST_REF"
```

Expected:

- Project returns to healthy.
- Data persists.
- Routes remain valid.

### 13.2 Pause and Resume

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects pause \
  --ref "$SUPADUPA_TEST_REF"

supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects resume \
  --ref "$SUPADUPA_TEST_REF"
```

Expected:

- Pause stops project services or marks them unavailable according to provisioner behavior.
- Resume restores the same URLs.
- Data persists.

### 13.3 Preview Branches

Runner phase: `scripts/compat/29-branches-deep.sh`.

Expected:

- Branch validation is opt-in with `SUPADUPA_COMPAT_BRANCH_VALIDATE=true` because it creates and deletes a temporary branch project.
- Existing source projects require `SUPADUPA_COMPAT_ALLOW_BRANCH_ON_EXISTING=true`; disposable projects created by the same compatibility run are allowed by default.
- If `SUPADUPA_TEST_ORG_ID` is set, the phase snapshots org feature overrides, temporarily enables `preview_branches`, and restores the original overrides during cleanup.
- Branch create defaults to `with_data=false` and returns a data-less branch without leaking runtime environment values.
- Branch list includes the created branch and reports `with_data=false`.
- Branch CLI profile and Connect payload expose remote-safe API, Studio, Storage S3, direct Postgres, and pooler endpoints for the branch ref, with no localhost or `.internal` leakage in public fields.
- Route manifest includes branch API, Studio, Storage S3, direct Postgres, and pooler routes under the public apps domain.
- Public branch Auth health becomes reachable through the branch API host.
- Branch delete removes the branch metadata and cleans up branch runtime/routes.

### 13.4 Stable Version Upgrade

Run this for each supported older stable version in the release matrix.

Commands:

```bash
export SOURCE_STACK_VERSION="<older-stable-version>"
export TARGET_STACK_VERSION="<current-stable-version>"

supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects create \
  --org-id "$SUPADUPA_TEST_ORG_ID" \
  --ref "$SUPADUPA_TEST_REF-upgrade" \
  --name "Compatibility Upgrade" \
  --domain "$SUPADUPA_APPS_DOMAIN" \
  --stack-version "$SOURCE_STACK_VERSION"

# Run REST, DB, SDK, Storage, Realtime, and Functions checks before upgrade.

supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects upgrade \
  --ref "$SUPADUPA_TEST_REF-upgrade" \
  --version "$TARGET_STACK_VERSION" \
  --backup-id "$PRE_UPGRADE_BACKUP_ID"

# Run the same checks after upgrade.
```

Automated matrix runner:

```bash
export SUPADUPA_COMPAT_CREATE_PROJECT=true
export SUPADUPA_STACK_VERSION="<older-stable-version>"
export SUPADUPA_COMPAT_UPGRADE_MATRIX=true
unset SUPADUPA_UPGRADE_TARGETS
scripts/compat/run.sh
```

Run one disposable project per supported older stable source version. Leaving `SUPADUPA_UPGRADE_TARGETS` empty proves the source version can upgrade to the newest stable release currently exposed by `/v1/stack-releases`; setting `SUPADUPA_UPGRADE_TARGETS` should be reserved for explicit target-version regression tests.
Set `SUPADUPA_UPGRADE_REALTIME_CONTINUITY_VALIDATE=true` on a disposable upgrade run to keep an active public Realtime client connected through the upgrade window.

Expected:

- Upgrade targets outside the supported stable allowlist are rejected before backup or provisioner calls.
- Each supported stable target resolves to a full stack release manifest covering Postgres, Kong, Studio, postgres-meta, GoTrue, PostgREST, Realtime, Storage, Edge Runtime, Supavisor, Logflare, Vector, and imgproxy image tags.
- Configured supported versions without a built-in or explicit release manifest are not exposed as upgrade targets.
- A completed pre-upgrade backup is recorded before the provisioner applies the target stack.
- Pre-upgrade backup responses and list responses include restore-critical metadata: `started_at`, `finished_at`, `verified_at`, positive `size_bytes`, 64-character `checksum_sha256`, and a local `location` or S3-compatible `remote_location`.
- A caller-supplied pre-upgrade backup must be logical, completed, verified, readable or rehydratable from its recorded S3-compatible target, size-matched, and checksum-matched before the provisioner is called.
- When `SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true`, the pre-upgrade backup must include `remote_location` and `storage_target_id`, and the target must be `recovery_ready=true` with `durable_off_host=true`; local-only or untested targets return HTTP 409 before the provisioner is called.
- The response includes previous version, target version, backup metadata, and rollback availability.
- Data written before upgrade is still visible through REST and Postgres after upgrade.
- Service toggles remain stable across Compose upgrade rerenders.
- Public API, Studio, direct Postgres, pooler, and project routes remain valid.
- When Realtime continuity validation is enabled, a client subscribed before the upgrade must observe reconnect/resubscribe behavior and receive a post-upgrade broadcast through the public project API host after health returns.
- If an intentionally failed upgrade fixture is used, failure injection must be explicitly enabled by the operator and by the request header.
- Failed upgrade responses include the error, previous version, target version, backup metadata, rollback availability, rollback attempt state, and any rollback error.
- When `SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE=true`, a failed upgrade with a successful stack-version rollback also runs the logical restore command against the pre-upgrade backup and reports `restore_attempted`, `restore_state`, and `restore_error`; this mode is destructive and must stay guarded to disposable projects unless explicitly allowed. `restore_state` must be `completed` to count as successful auto-recovery; a dry-run restore is reported through `restore_error`.
- After an intentionally failed upgrade, the provisioner attempts to roll back to the previous stack version, the project remains on the previous version, public health still passes, and the pre-upgrade backup remains restorable.
- When `SUPADUPA_UPGRADE_FAILURE_RESTORE_VALIDATE=true`, the runner restores the same pre-upgrade backup after the rollback check. This restore remains destructive and must stay guarded to disposable projects unless `SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE=true`.

### 13.4 Platform Backup Restore

Commands:

```bash
curl -fsS -X POST "$SUPADUPA_API_URL/v1/platform/backups" \
  -H "Authorization: Bearer $SUPADUPA_TOKEN"

curl -fsS -X POST "$SUPADUPA_API_URL/v1/platform/backups/$PLATFORM_BACKUP_ID/restore" \
  -H "Authorization: Bearer $SUPADUPA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"confirm":"restore-control-plane"}'
```

Expected:

- Platform backups are listed and restorable from Settings > Backups.
- API restore requires exact `restore-control-plane` confirmation.
- Restore rejects incomplete, wrong-kind, size-mismatched, or checksum-mismatched artifacts.
- If the local artifact is missing and the backup has a recorded S3-compatible target, restore rehydrates the artifact before validation.
- Restore imports the encrypted control-plane checkpoint and normalized metadata atomically only after validation succeeds.
- Restore preserves the source platform backup record after import and after process restart.
- Restore asks the active provisioner to reconcile restored projects and stop projects removed by the checkpoint while retaining volumes.
- API restore returns `restore_state=reconciled` when provisioner reconciliation succeeds, or `metadata-restored-runtime-errors` with `runtime_errors` when operator follow-up is required.
- Platform backup records must include `started_at`, `finished_at`, `verified_at`, `size_bytes`, `checksum_sha256`, and either a local `location` or S3-compatible `remote_location`. A platform backup uploaded to S3-compatible storage must keep its target available until restore validation has completed.

### 13.4a S3-Compatible Target Validation

Commands:

```bash
curl -fsS -X POST "$SUPADUPA_API_URL/v1/backup-storage-targets/$BACKUP_TARGET_ID/test" \
  -H "Authorization: Bearer $SUPADUPA_TOKEN"
```

Expected:

- When `SUPADUPA_BACKUP_TARGET_*` or `SUPADUPA_BACKUP_S3_*` env vars are present at startup, the control plane creates or updates the named target, marks it default, preserves an existing secret if the update omits it, and does not duplicate or audit-churn the target on later restarts when it already matches.
- When `SUPADUPA_BACKUP_TARGET_AUTO_TEST=true` or `SUPADUPA_BACKUP_S3_AUTO_TEST=true`, startup immediately runs the same server-side target probe used by the API, records `last_test_*`, and audits `backup_storage_target.bootstrap_env_test`; failed probes do not crash the control plane but leave the target non-recovery-ready.
- `supadupa-cli backup-targets list|create|update|test|delete` exercises the same Management API endpoints, redacts secrets in responses, and can assign project backup policies with `backups set-policy --storage-target-id`.
- Terraform `supadupa_backup_storage_target` can manage the same target shape, keeps `secret_access_key` sensitive/write-only, exposes `durable_off_host`, `recovery_ready`, `readiness_status`, and last test fields, and `supadupa_project_backup_policy.storage_target_id` can bind a project to the target for logical or physical backup artifacts.
- Target test writes a small object under `<prefix>/_supadupa-checks/...`, reads it back, and deletes it best-effort.
- The API returns a redacted target with `last_tested_at`, `last_test_status`, `durable_off_host`, `recovery_ready`, `readiness_status`, and `readiness_message`.
- Failed probes record `last_test_status=failed` and `last_test_error` without leaking credentials.
- Settings > Backups exposes the same test action, last-result state, and recovery-readiness state.
- `19-durable-backup-target.sh` can create or update a named operator-supplied S3/R2/remote-MinIO target from compatibility environment variables, optionally create the bucket, run the server-side probe, require `durable_off_host=true`, `recovery_ready=true`, and `last_test_status=passed`, and write the selected target ID for the recoverability/PITR phase.
- If no durable target is configured, the compatibility harness may create a disposable local S3-compatible probe target, validate the same server-side test path, and delete it afterward; this proves target plumbing but does not satisfy hosted-grade off-host durability.
- Local or same-machine endpoints such as `localhost`, `127.0.0.1`, `::1`, `0.0.0.0`, `host.docker.internal`, and IPs assigned to local network interfaces must return `durable_off_host=false`, `recovery_ready=false`, `readiness_status=local-or-loopback`, and must not satisfy `off_host_backup_configured`, `off_host_backup_verified`, WAL off-host verification, physical backup availability, or restore-to-time readiness.
- When `SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true`, physical backup uploads and WAL archive uploads require a selected/default backup target, and that target must be tested, durable off-host, and `recovery_ready=true`; automatic PITR archive-bucket derivation may only use a selected/default recovery-ready target. Local RustFS/temp targets and missing-target local artifacts are allowed for dev drills only when this production guard is off.
- The RustFS compatibility phase proves real SigV4 target create/test behavior. Against compat-created disposable projects it also proves project logical backup upload, physical backup upload when a physical backup command is available, WAL artifact upload, and that those loopback artifacts still do not satisfy off-host recoverability. Control-plane backup upload through RustFS must keep the target/container available after the run so the platform backup remains restorable.
- A persistent local RustFS target (`SUPADUPA_COMPAT_RUSTFS_KEEP_TARGET=true`) is allowed for dev restore drills and for checking Settings > Backups target visibility. It is still a loopback target and must not satisfy hosted-grade off-host readiness.

### 13.5 PITR Scheduling

Commands:

```bash
curl -fsS -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/pitr/policy" \
  -H "Authorization: Bearer $SUPADUPA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"enabled":true,"archive_bucket":"s3://compat-pitr/'"$SUPADUPA_TEST_REF"'","retention_days":7}'

curl -fsS "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/pitr/wal" \
  -H "Authorization: Bearer $SUPADUPA_TOKEN"
```

Expected:

- Enabling PITR requires an archive bucket and valid retention window.
- With `SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true`, enabling PITR without an explicit `archive_bucket` may derive the hosted-style WAL bucket only from a selected/default recovery-ready target; otherwise the request must fail with the same archive-bucket requirement.
- In applied Compose mode, WAL archive uses the default WAL-switch command, writes the WAL artifact, records the real 24-character Postgres WAL filename, and returns `segment_source=postgres`. Outside applied Compose mode, or when `SUPADUPA_COMPOSE_BACKUP_DEFAULTS=false`, WAL archive requires `SUPADUPA_WAL_ARCHIVE_COMMAND`; without it, the request fails and does not create a fake archive that could satisfy PITR readiness.
- Healthy and degraded projects with PITR enabled are archived automatically by the control-plane scheduler.
- The scheduler honors `SUPADUPA_WAL_ARCHIVE_INTERVAL` and does not create a WAL artifact on every tick.
- Scheduled WAL archive success and failure events are written to project logs and audit events.
- Full hosted-grade PITR validation requires a durable off-host S3-compatible target plus destructive disposable-project restore validation with `SUPADUPA_COMPAT_PHYSICAL_BACKUP_VALIDATE=true` and `SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE=true`; loopback RustFS/temp targets prove object-storage plumbing but must not satisfy off-host readiness. The hosted-grade destructive phase must require the selected target to report `durable_off_host=true`, `recovery_ready=true`, and `last_test_status=passed`, reject local/private target endpoints before restore, then prove restore semantics with a SQL fixture where data inserted before the recovery timestamp remains and data inserted after the recovery timestamp is absent after restore.

### 13.6 Recoverability Status

Commands:

```bash
curl -fsS -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/backups/policy" \
  -H "Authorization: Bearer $SUPADUPA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"enabled":true,"schedule":"daily","kind":"physical"}'

curl -fsS -X POST "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/backups" \
  -H "Authorization: Bearer $SUPADUPA_TOKEN"

curl -fsS "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/recoverability" \
  -H "Authorization: Bearer $SUPADUPA_TOKEN"

curl -fsS "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/database/backups" \
  -H "Authorization: Bearer $SUPADUPA_TOKEN"

curl -fsS -X POST "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/database/backups/restore-pitr" \
  -H "Authorization: Bearer $SUPADUPA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"recovery_time_target_unix":"1735689600","confirmation":"restore pitr project '"$SUPADUPA_TEST_REF"'"}'
```

Expected:

- Recoverability status reports backup policy state, off-host target configuration, latest backup, latest verified backup, PITR state, latest WAL archive, recovery window, physical backup availability, PITR restore command configuration, restore-to-time availability, warnings, and recommendations.
- A project with only local logical backups reports `off_host_backup_configured=false`, `off_host_backup_verified=false`, and `restore_to_time_available=false`.
- A project with a verified S3-compatible backup target reports off-host backup readiness only after a verified backup artifact records a remote location.
- In applied Compose mode, a `physical` backup policy uses the default `pg_basebackup` command and creates a verified `kind=physical` artifact.
- Outside applied Compose mode, or when `SUPADUPA_COMPOSE_BACKUP_DEFAULTS=false`, a `physical` backup policy requires `SUPADUPA_PHYSICAL_BACKUP_COMMAND`; without it, backup trigger returns HTTP 409 and does not create a fake physical artifact.
- With a Compose default or `SUPADUPA_PHYSICAL_BACKUP_COMMAND` plus an S3-compatible target configured, manual and scheduled physical backup triggers create verified `kind=physical` artifacts with `remote_location`.
- With a RustFS target in a disposable run, the suite must assert that logical backup, physical backup when configured, and real Compose/default or explicitly configured WAL archive metadata include `remote_location`, `storage_target_id`, `size_bytes`, `checksum_sha256`, `verified_at`, a real 24-character WAL `segment`, and `segment_source=postgres`.
- PITR does not report restore-to-time readiness until a verified off-host physical base backup, verified off-host WAL archive with `segment_source=postgres`, and configured PITR restore command are all available. In applied Compose mode, the default destructive restore command counts as configured; outside applied Compose mode, `SUPADUPA_PITR_RESTORE_COMMAND` must be set.
- The hosted-shaped restore route returns HTTP 409 with an embedded `recoverability` object until every restore-to-time gate is satisfied.
- The restore route rejects missing, size-mismatched, or checksum-mismatched physical backup and WAL archive artifacts before running the restore command.
- With a verified off-host physical backup artifact, an off-host WAL archive range from the selected base backup through the requested recovery target, and `SUPADUPA_PITR_RESTORE_COMMAND` configured, the restore route validates and rehydrates every selected WAL artifact, returns HTTP 201, writes a restore transcript, logs the restore, and records `project.restore_pitr`.
- The destructive compatibility path must validate recovered data, not only the API response: it inserts a pre-target row, captures the recovery target, inserts a post-target row, archives WAL, restores to the captured target, and verifies the pre-target row exists while the post-target row does not.
- The Compose default PITR restore path must stop the project stack, restore the `db-data` volume from the physical artifact, copy the selected WAL range, write recovery target settings, restart the stack, and produce a restore transcript.
- PITR restore command templates must expose both the latest WAL artifact and the selected WAL range, including `{{wal_path_args}}`, `{{wal_paths}}`, `{{wal_segments}}`, and `{{wal_archive_ids}}`.
- The Admin UI Backups page shows the same posture so local-only backups are visually distinct from hosted-grade recovery.

### 13.7 Project Isolation

Create or discover a second project and repeat a subset of remote connection, REST/Auth, and DB tests. On clean installs where no second project exists, the suite must be able to create a disposable peer project with `SUPADUPA_COMPAT_CREATE_ISOLATION_PROJECT=true`, wait for its public Auth health endpoint, run the isolation checks, and destroy the peer on exit.

Expected:

- Project A and Project B expose distinct public API, Studio, direct Postgres, transaction pooler, session pooler, and Storage S3 endpoints, with S3 on `storage-<ref>.<apps-domain>`.
- Public connection snippets for both projects use `db-<ref>.<apps-domain>` and `pooler-<ref>.<apps-domain>` with `sslmode=require`.
- Public connection snippets and profile fields for either project do not leak `localhost`, `host.docker.internal`, or `.internal` values.
- Generated read-replica hosts must fit the single-label wildcard DNS topology and the full DNS name limit; replica names must be DNS-safe labels, and creating a replica whose `db-replica-<replica>-<ref>` label exceeds 63 characters or whose full FQDN exceeds 253 characters must return a validation error instead of creating unroutable metadata.
- Secret handles are scoped to their own project ref.
- Route files for Project A do not reference Project B hosts or services, and vice versa.
- Project A keys do not work against Project B.
- Project A DB cannot see Project B schema/data.
- Project A S3 protocol credentials do not work against Project B's Storage S3 endpoint.
- User JWT S3 session-token credentials use the project ref as the S3 access key, the anon key as the S3 secret key, and the user JWT as `x-amz-security-token`; Storage RLS must allow the owner to write/read their own object and reject cross-user reads.
- Project A Studio route cannot access Project B.
- Runtime Docker networks and volumes are isolated.
- When the suite creates the second project, cleanup must prove the peer is removed from the Management API, its route file and project directory are gone, and no peer containers remain.

### 13.8 Destroy

Commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" projects destroy \
  --ref "$SUPADUPA_TEST_REF" \
  --yes
```

Expected:

- Runtime containers stop and are removed.
- Route files are removed.
- Default destroy removes volumes unless explicitly retained.
- Project URL no longer routes to a live stack.
- Audit records destroy.

## Phase 14: Security Regression Tests

### 14.1 Public Port Exposure

Runner phase: `scripts/compat/14-public-exposure.sh`.

Commands:

```bash
docker ps --format '{{.Names}}\t{{.Ports}}'
```

Expected:

- Only intended platform ports are published.
- Per-project Postgres, Auth, REST, Storage, Realtime, Edge Runtime, and Studio are not directly published to the host.
- Public access goes through Traefik and Kong.

### 14.2 TLS

Runner phase: `scripts/compat/15-tls.sh`.

Expected:

- Control-plane admin and API certificates are valid.
- Project wildcard certificate is valid.
- Studio wildcard certificate is valid.
- HTTP redirects to HTTPS where configured.
- Public direct Postgres and pooler URLs require `sslmode=require` and validate through PostgreSQL STARTTLS.

### 14.3 Auth Boundaries

Runner phase: `scripts/compat/13-security-boundaries.sh`.

Expected:

- Supadupa admin token cannot be used as a project anon/service key.
- Project service role key cannot call Supadupa Management API.
- Project anon/service keys cannot call Platform SCIM.
- Platform SCIM rejects unauthenticated requests and accepts the configured SCIM bearer token when `SUPADUPA_COMPAT_SCIM_TOKEN` is provided.
- Studio requires Supadupa admin authentication.
- Project APIs require Supabase keys/tokens as expected.

### 14.4 Secret Handling

Expected:

- CLI profile emits handles, not raw secrets.
- Secret reveal requires authorization.
- Revealed secrets do not appear in logs.
- Function env and project env files have restrictive permissions where feasible.

## Phase 15: Current Known Gates

These are not test failures unless the product has committed to the behavior.

| Gate | Current implication | Preferred resolution |
|------|---------------------|----------------------|
| Official Supabase CLI typegen rewrites DB TLS settings | Exact upstream `supabase gen types --db-url` can fail against BYO Let's Encrypt DB hosts | Use `supadupa-cli projects gen-types` and track/upstream a Supabase CLI fix |
| Official `supabase functions deploy` expects hosted management APIs | Should be classified as `supadupa-wrapper` | Keep `supadupa-cli functions deploy`, or implement a compatible deploy API |
| Secret handles are not raw env values | SDK/CLI tests need authorized reveal step | Use `supadupa-cli projects env --reveal-secrets` for trusted test/dev env materialization |
| Hosted Supabase project linking is cloud-specific | `supabase link` is not pass-by-default | Use `supadupa-cli projects link` for Supadupa workspace binding and config export |

## Acceptance Criteria

A release is Supabase-compatible enough for the MVP when:

- Project create to healthy passes in public DNS Compose.
- API, REST, Auth, GraphQL, Realtime, Storage, Edge Functions, and Studio all pass.
- Supabase JS SDK can query, insert, authenticate, upload, subscribe, and invoke functions.
- Official Supabase CLI DB workflows pass in at least one supported DB access mode.
- Official Supabase CLI hosted-management commands are explicitly classified and documented.
- Supadupa CLI covers lifecycle, profile export, secrets, config, function deploy, metrics, and destroy.
- No project service is exposed publicly except through the intended edge routes.
- All secrets stay masked unless explicitly revealed by an authorized user.
- Recoverability reports local-only, off-host, physical/PITR, and restore-to-time readiness accurately.
- Project isolation is proven with at least two projects.

## Minimal Smoke Subset

For fast validation after code changes, run:

```txt
0.1 Unit and Build Tests
0.2 Platform Health
1.2 Create Project
1.3 Status Convergence
2.1 JSON Profile
2.2 Env Export
2.3 TOML Export
3.2 Database URL Reachability Gate
4.1 Gateway Health
5.1 Signup and Login
6.2 REST Anonymous Access
7 GraphQL
8.2 Object Upload and Download
9.1 WebSocket Connect
10.2 Function Invoke
11.1 Public Studio Route
13.6 Recoverability Status
13.8 Destroy
```

## Full Nightly Suite

The full nightly suite should run every phase in order against a fresh project, then create a second project for isolation tests. Preserve artifacts for failed runs:

```txt
/tmp/supadupa-compat/<ref>/
  profile.json
  supabase.env
  supabase/config.toml
  results.jsonl
  curl/
  logs/
  screenshots/
  docker/
```

The nightly result should include:

- Supadupa git commit.
- Supadupa stack image versions.
- Official Supabase CLI version.
- Docker and Docker Compose versions.
- Apps/control domains.
- DB access mode.
- Pass/fail/skip counts by phase.
- Known gates hit during the run.

The repository workflow `.github/workflows/compat.yml` provides the CI entrypoint:

- Push and pull request runs execute local Go tests, the frontend build, and compatibility script syntax checks.
- Scheduled and manual runs execute `scripts/compat/run.sh` when live Supadupa secrets are configured.
- Manual runs can inspect an existing project or create a disposable project when `create_project=true` and `SUPADUPA_COMPAT_ORG_ID` is available.
- Manual and scheduled runs can enable the durable hosted-grade recovery profile with `durable_backup_target=true`, `physical_backup_validate=true`, `pitr_restore_validate=true`, and an off-host S3/R2/remote-MinIO target supplied through `SUPADUPA_COMPAT_DURABLE_S3_*` repository secrets. Destructive PITR validation is CI-gated to disposable project runs.
- Live artifacts are uploaded from `$SUPADUPA_COMPAT_ARTIFACT_ROOT/$SUPADUPA_TEST_REF`, excluding control tokens, revealed secret material, materialized secret env files, token/refresh bodies, and secret payloads.
