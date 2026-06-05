# supadupa

Self-hosted, enterprise-grade Supabase platform control plane.

This repository is being built from `docs/PRD.md` draft v0.2. The MVP target is a Docker Compose-backed supadupavisor control plane with a full admin UI, while keeping orchestration behind a provisioner interface so Kubernetes can become a backend swap later.

## Current Milestone

MVP control plane with selected Phase 2/3 surfaces:

- Go Management API with orgs, users, org/team/project RBAC, hosts, CPU/RAM/disk/IOPS quotas, usage snapshots, billing invoices, platform defaults, projects, lifecycle, config, audit log, metrics, fleet advisor, and project logs.
- Provisioner abstraction with Docker Compose rendering and Kubernetes CRD rendering backends.
- Admin UI for fleet dashboard, project creation, Connect, lifecycle, config, secrets, domains, network policy, backups, PITR, functions, log drains, branches, read replicas, metering, and billing invoices.
- CLI as a thin client over the Management API.
- Meta DB migrations for control-plane resources including project routes, functions, branches, replicas, backups, PITR/WAL archives, domains, log drains, quotas, usage snapshots, billing invoices, and immutable audit events.

## Layout

- `cmd/supadupa/` - supadupavisor control-plane binary entrypoint.
- `cmd/supadupa-cli/` - thin CLI over the Management API.
- `cmd/supadupa-mcp/` - MCP stdio server for AI tools, backed by the Management API.
- `cmd/terraform-provider-supadupa/` - Terraform/OpenTofu provider over the Management API.
- `internal/api/` - HTTP Management API server.
- `internal/cli/` - CLI command/request plumbing.
- `internal/mcp/` - MCP JSON-RPC transport and tool bridge.
- `internal/terraform/` - Terraform provider client, resources, and data sources.
- `internal/control/` - orchestration contracts and domain types.
- `internal/provisioner/compose/` - Docker Compose provisioner implementation.
- `internal/provisioner/kubernetes/` - Kubernetes CRD and custom-resource renderer.
- `migrations/` - meta database SQL migrations.
- `docs/` - product and architecture documentation.

## Development

```bash
go test ./...
go run ./cmd/supadupa
```

The service listens on `:8080` by default. Override with `SUPADUPA_ADDR`. Browser clients are allowed from common local admin origins (`localhost`/`127.0.0.1` on `3000`, `3001`, `5173`, and `5174`) by default; set `SUPADUPA_CORS_ORIGINS` to a comma-separated list of admin UI origins for deployed or custom-port environments. Set `SUPADUPA_AUTH_SECRET` to a stable high-entropy value for bearer-token signing; when it is omitted, supadupa falls back to `SUPADUPA_SECRET_KEY` before using the local-development signing key.

## Docker Compose MVP

The MVP stack can run the meta DB, supadupavisor Management API, admin UI, and Traefik edge router from `deploy/compose.yaml`:

```bash
docker network create supadupa-ingress
mkdir -p runtime/{projects,routes,certs,backups}
docker compose -f deploy/compose.yaml --profile edge up -d --build
```

The admin UI is available at `http://localhost:3000` and the Management API at `http://localhost:8080`. Override these bindings with `SUPADUPA_ADMIN_ADDR` and `SUPADUPA_API_ADDR`; when changing the browser-facing admin origin, also set `SUPADUPA_CORS_ORIGINS` for the control plane.

### Linux node with real DNS

For realistic project, Studio, and wildcard routing tests, run the Compose MVP on a Linux host with DNS pointed at that host instead of using a local port proxy. Create DNS records like:

- `admin.example.test` -> Linux node
- `api.example.test` -> Linux node
- `*.projects.example.test` -> Linux node
- `studio.*.projects.example.test` -> Linux node, if your DNS provider supports nested wildcards

Then set the browser-facing origins before starting Compose:

```bash
export SUPADUPA_ADMIN_HOST=admin.example.test
export SUPADUPA_API_HOST=api.example.test
export SUPADUPA_ACME_EMAIL=ops@example.test
export SUPADUPA_RUNTIME_HOST_DIR="$PWD/runtime"
export SUPADUPA_RUNTIME_CONTAINER_DIR="$PWD/runtime"
export SUPADUPA_COMPOSE_APPLY=true

docker network create supadupa-ingress
mkdir -p runtime/{projects,routes,certs,backups}
docker compose -f deploy/compose.yaml --profile edge up -d --build
```

The edge proxy uses Traefik's Docker provider for the platform admin/API services and the file provider for per-project routes rendered by the control plane. `SUPADUPA_ADMIN_HOST` and `SUPADUPA_API_HOST` control the platform hostnames; Compose uses them to set Traefik labels, the admin UI's built-in API base URL, and the default CORS origin. `SUPADUPA_ACME_EMAIL` controls the Let's Encrypt account contact for automatic TLS. Set `VITE_API_BASE_URL` or `SUPADUPA_CORS_ORIGINS` only when those need to differ. The project domain is managed from platform defaults in the admin UI/API.

In Settings, set the default project domain to your wildcard project domain, for example `projects.example.test`. New project Connect payloads will use canonical DNS-backed URLs such as `https://<ref>.projects.example.test` and `https://studio.<ref>.projects.example.test`.

For local development, set `SUPADUPA_BOOTSTRAP_EMAIL` and `SUPADUPA_BOOTSTRAP_PASSWORD` to seed the first admin if no users exist in the meta DB:

- Email: `admin@supadupa.local`
- Password: `supadupa2026`

Override those with `SUPADUPA_BOOTSTRAP_EMAIL` and `SUPADUPA_BOOTSTRAP_PASSWORD`. The bootstrap is skipped once any user exists. Normal deployments do not have a baked-in default login; first-run bootstrap is manual unless those env vars are explicitly set.

By default the containerized Compose provisioner renders project stacks but does not apply them (`SUPADUPA_COMPOSE_APPLY=false`). To let supadupavisor run `docker compose` against the host Docker daemon, set an absolute runtime path and mount it at the same path inside the container so host-side bind mounts resolve correctly:

```bash
export SUPADUPA_RUNTIME_HOST_DIR="$PWD/runtime"
export SUPADUPA_RUNTIME_CONTAINER_DIR="$PWD/runtime"
export SUPADUPA_COMPOSE_APPLY=true
docker compose -f deploy/compose.yaml --profile edge up -d --build
```

The PRD calls for a dedicated control-plane meta Postgres. For local development:

```bash
docker compose -f deploy/compose.yaml up -d meta-db
export SUPADUPA_META_DSN=postgres://supadupa:supadupa@localhost:15432/supadupa_meta?sslmode=disable
go run ./cmd/supadupa
```

When `SUPADUPA_META_DSN` is set, supadupa connects to the meta DB, applies every file in `migrations/`, stores an encrypted control-plane checkpoint, and mirrors control-plane state into normalized SQL tables. On startup the checkpoint is preferred, but the store can reconstruct from the normalized tables if the checkpoint row is missing.

Persistent checkpoint payloads and normalized project secret values support three encryption providers:

- Default local AES-GCM: set `SUPADUPA_SECRET_KEY` to a stable high-entropy value before production use. If unset, a deterministic local-development key is used.
- Vault-style file key: set `SUPADUPA_KMS_PROVIDER=vault-file` and `SUPADUPA_VAULT_KEY_FILE=/path/to/key-material`. The file contents are used as key material for envelope encryption.
- External command KMS: set `SUPADUPA_KMS_PROVIDER=command` and `SUPADUPA_KMS_COMMAND=/path/to/wrapper`. The wrapper receives bytes on stdin, gets `SUPADUPA_KMS_OPERATION=encrypt` or `decrypt`, and must write the transformed bytes to stdout.

Encrypted payloads carry a provider prefix, so existing local AES-GCM checkpoints continue to decrypt after switching to a Vault-file or command KMS provider as long as the old `SUPADUPA_SECRET_KEY` is still available.

The edge proxy is Traefik reading dynamic route files rendered by the control plane. It shares the external `supadupa-ingress` Docker network with per-project Kong containers:

```bash
docker network create supadupa-ingress
mkdir -p runtime/routes
docker compose -f deploy/compose.yaml --profile edge up -d edge-router
```

By default, route files are written to `./runtime/routes` and mounted into Traefik. Override the control-plane output path with `SUPADUPA_ROUTES_ROOT` and the host mount with `SUPADUPA_ROUTES_HOST_DIR` if you use a different directory. Set `SUPADUPA_ACME_EMAIL` before production use; Traefik stores ACME state in the `supadupa-letsencrypt` volume.

The Traefik dashboard and insecure API are disabled by default. For temporary local diagnostics on the loopback-bound dashboard port, set both `SUPADUPA_TRAEFIK_DASHBOARD_ENABLED=true` and `SUPADUPA_TRAEFIK_DASHBOARD_INSECURE=true`; do not expose that port publicly.

Custom domain creates also prepare certificate state under `./runtime/certs/<ref>`. Without an operator command, supadupa writes a manual JSON plan and leaves `cert_status=pending` while Traefik handles ACME from the rendered TLS router. Configure `SUPADUPA_CERT_COMMAND` to automate issuance; stdout/stderr becomes a certificate transcript and successful runs mark the domain `issued`. Template variables include `{{ref}}`, `{{project_ref}}`, `{{fqdn}}`, `{{cert_path}}`, and `{{cert_root}}`.

Project destroy removes the rendered project stack, Traefik route artifact, and the project's certificate artifact directory before deleting control-plane metadata. Route or certificate cleanup failures return a conflict and are recorded in project logs and the immutable audit chain. When `retain_volumes=true`, Compose records retained Docker volumes under the provisioner root `_retained` directory; Kubernetes records retained PVC intent as `RetainedProjectResources` under `runtime/kubernetes/_retained`.

Select the orchestration substrate with `SUPADUPA_PROVISIONER`:

```bash
SUPADUPA_PROVISIONER=compose go run ./cmd/supadupa
SUPADUPA_PROVISIONER=kubernetes SUPADUPA_K8S_ROOT=./runtime/kubernetes go run ./cmd/supadupa
```

The active substrate is exposed through `/v1/provisioner`, `supadupa-cli provisioner status`, MCP `supadupa_get_provisioner`, and the admin Settings screen.

The Kubernetes provisioner renders CRD definitions under `runtime/kubernetes/_crds` plus `Project`, `ProjectConfig`, `ProjectAuthHooks`, `ProjectBranchClone`, and `ProjectReplica` custom resources under `runtime/kubernetes` by default. Project config updates become one `configs/<area>.yaml` manifest per config area so the operator can reconcile auth, storage, functions, realtime, pooler, network, SMTP, AI, and database settings without changing the Management API. Destroy deletes every rendered project manifest in apply mode before removing local artifacts. Set `SUPADUPA_K8S_APPLY=true` to apply rendered CRDs and resources with `kubectl`; otherwise it runs in render-only mode for local development.

The Compose provisioner renders one isolated project directory per project. Each directory includes `.env`, `compose.yaml`, Kong and Vector config, a `log-drains` fragment directory, and `00-supadupa-init.sql`, which bootstraps the per-project Postgres schemas, baseline Supabase roles, and stack-class extensions such as `pg_graphql`, `pg_cron`, `pgmq`, `vector`, and `supabase_vault`.

Pause and resume are persisted in the rendered desired state, not just the meta DB. Compose stores `SUPADUPA_DESIRED_STATE` in the project `.env`; Kubernetes stores `spec.desiredState` in the rendered `Project` custom resource. The reconciler reads that state so paused projects stay paused across status checks, restarts, and service-toggle re-renders.

Project config updates for auth, auth providers, email templates, storage, functions, realtime, pooler, database markers, and SMTP are synchronized into each Compose project's `.env` when they map to upstream runtime settings. Network config updates re-render Traefik route policy. Config values that do not have a direct stack runtime setting remain control-plane metadata for UI/API parity. Provider client secrets, captcha secrets, SMS tokens, and SMTP passwords are represented as secret handles in config; raw secret material should stay behind audited secret reveal/copy flows.

TOTP and phone MFA are managed in project config area `auth`. `mfa_totp_enabled` and `mfa_phone_enabled` remain shorthands for enabling both enrollment and verification, while `mfa_totp_enroll_enabled`, `mfa_totp_verify_enabled`, `mfa_phone_enroll_enabled`, `mfa_phone_verify_enabled`, `mfa_phone_otp_length`, and `mfa_phone_max_frequency` map to the upstream GoTrue MFA runtime settings.

Phone login and phone MFA SMS config supports GoTrue-compatible providers `twilio`, `twilio_verify`, `messagebird`, `textlocal`, and `vonage`. Provider credential values use `secret://` handles for tokens, access keys, and API secrets; Compose writes the matching `GOTRUE_SMS_*` runtime settings.

Social OAuth config supports the hosted-style named provider surface through project config area `auth_providers`: Apple, Bitbucket, Discord, Facebook, GitLab, GitHub, Google, Kakao, Keycloak, LinkedIn OIDC, Notion, Slack OIDC, Spotify, Twitch, Twitter, WorkOS, Zoom, Azure, and custom OIDC metadata. Provider client secrets use `secret://` handles; Compose writes matching `GOTRUE_EXTERNAL_*` runtime settings for named GoTrue providers and `SUPADUPA_AUTH_OIDC_*` metadata for custom OIDC.

Email template config supports both auth transaction templates and notification templates. Notification keys include `notification_password_changed_*`, `notification_email_changed_*`, `notification_phone_changed_*`, `notification_mfa_factor_enrolled_*`, `notification_mfa_factor_unenrolled_*`, `notification_identity_linked_*`, and `notification_identity_unlinked_*`, with `enabled`, `subject`, and `body` variants. Compose writes notification enablement to GoTrue `GOTRUE_MAILER_NOTIFICATIONS_*_ENABLED` settings and notification bodies to `GOTRUE_MAILER_TEMPLATES_*_NOTIFICATION`, while keeping subjects as supadupa metadata for UI parity.

Dedicated pooler options are managed through project config area `pooler`. Compose records the requested dedicated pooler tier, pool mode, pool sizes, client limits, and transaction/session ports as `SUPADUPA_POOLER_*` environment metadata. The Connect payload uses configured pooler ports when returning transaction and session pooled Postgres strings and includes a `pooler` section with the dedicated pooler mode, tier, pool size, client limit, and port metadata for the Admin UI.

Database SSL enforcement is managed through project config area `database` with `ssl_enforced`. The Connect payload reflects that desired state in direct and pooled Postgres URIs with `sslmode=require` when enforced, or `sslmode=prefer` when operators explicitly allow optional DB SSL.

OAuth 2.1 project clients are registered through `/v1/projects/<ref>/auth/clients`. The control plane validates redirect URIs, supported grants and scopes, masks confidential client secret handles in API/UI responses, and records create/delete events in project logs, metering, and the immutable audit chain.

Auth Hooks are declared through `/v1/projects/<ref>/auth/hooks`. Hook types such as `custom_access_token`, `before_user_created`, `send_sms`, and `send_email` can target an HTTPS endpoint or a project Edge Function. Compose synchronizes enabled hooks into GoTrue `GOTRUE_HOOK_*_ENABLED` and `GOTRUE_HOOK_*_URI` settings, maps Edge Function targets to the project's `/functions/v1/<name>` URL, and writes the complete desired state to each project `auth-hooks.json` artifact. Kubernetes writes the same hook set to an `auth-hooks.yaml` `ProjectAuthHooks` manifest for the operator to reconcile atomically. Secret handles and sensitive headers are masked in API/UI responses and retained as `SUPADUPA_AUTH_HOOK_*_SECRET_HANDLE` metadata until a secret resolver can inject Standard Webhooks secrets into GoTrue.

Enabled service toggles are managed through `/v1/projects/<ref>/services`. Compose records `SUPADUPA_SERVICE_*_ENABLED` in `.env`, re-renders `compose.yaml` and Kong routes, and removes disabled services on the next apply. Kubernetes re-renders the `Project` CRD service map.

Every project now receives both the legacy shared JWT secret and asymmetric Ed25519 JWT signing keys. The Connect payload exposes current, next, and archived signing key summaries with public key material; private key JSON stays behind the audited secret reveal/copy endpoints. Rotating `jwt_signing_key_current` archives the old current key, promotes `jwt_signing_key_next`, and generates a fresh next key.

Platform SCIM v2 provisioning is available under `/v1/scim/v2` for auth-required deployments. Users map to platform users, and groups map to org teams; group create uses `externalId` or the `urn:supadupa:params:scim:schemas:extension:Group.org_id` extension to select the target org. SCIM user deprovisioning through `DELETE /Users/{id}` or `PATCH active:false` removes the platform user, org memberships, team memberships, and user-level project grants, then records the action in the immutable audit chain.

Platform SAML SSO for supadupa operators is configured through `/v1/settings/sso` and surfaced on the admin login screen with a "Continue with SSO" action. The public `/v1/auth/sso/saml/start` endpoint returns IdP login metadata, and `/v1/auth/sso/saml/callback` accepts a normalized SAML assertion signed by the configured IdP certificate before issuing the same platform bearer token as password login. The callback enforces issuer, audience/ACS URL, expiry, optional email-domain restriction, and auto-provisioning policy.

Platform SCIM provisioning is exposed through `/v1/scim/v2/ServiceProviderConfig`, `/v1/scim/v2/Users`, and `/v1/scim/v2/Groups`. The admin Settings screen shows the service-provider capabilities plus provisioned user/group summaries so operators can verify IdP provisioning state without leaving the dashboard.

Platform account MFA is managed through `/v1/account/mfa` for the currently authenticated operator. The API, admin UI, CLI, and MCP server expose TOTP status, enrollment, verification, and disable flows; login returns an MFA challenge without a bearer token until a valid TOTP code is supplied.

Edge Function deploys write runtime artifacts under `./runtime/projects/<ref>/functions/<name>` by default: the source entrypoint, a per-function `.env`, and deployment metadata. The Compose data plane bind-mounts that `functions` directory into Edge Runtime at `/home/deno/functions`.

Regional invocation targets for Edge Functions are declared through `/v1/projects/<ref>/functions/regions`. Each declaration links a deployed function to a region label, optional host, routing policy, and generated invocation URL. Compose writes the declarations to each function's `regions.json` artifact as desired state; Kubernetes or another multi-region substrate can reconcile those records into colocated function runtime placement without changing API, CLI, or UI contracts.

Persistent storage mounts for Edge Functions are declared through `/v1/projects/<ref>/functions/storage-mounts`. Each declaration links a deployed function to a project storage bucket, mount path under `/mnt`, optional bucket prefix, read-only/read-write mode, and environment alias. Compose writes the current declarations to each function's `storage-mounts.json` artifact so a runtime sidecar or future substrate reconciler can mount the requested S3-backed bucket without changing API, CLI, or UI contracts.

Read replicas are provisioned through `/v1/projects/<ref>/replicas`. Each replica records resource tier, region, read-routing weight, failover priority, lag metadata, and role. `/v1/projects/<ref>/replicas/routing` returns the weighted healthy read target set and next failover candidate; `/promote` and `/failover` update the authoritative control-plane routing state and emit logs/audit events. Compose writes replica routing metadata to each replica `.env`, and Kubernetes writes it to the `ProjectReplica` CRD for substrate reconcilers to enact streaming replication, read routing, and promotion.

Replication pipelines are managed through `/v1/projects/<ref>/replication`. They declaratively record logical replication, ETL, and analytics-bucket exports for destinations such as Postgres, webhooks, S3, Iceberg, BigQuery, Snowflake, and Redshift. Destination credentials should be referenced with `secret://` handles; sensitive config keys are masked in API/UI responses. Pipeline creates and deletes are metered, logged, and sealed in the immutable audit chain.

AI provider defaults are managed through project config area `ai`. Provider credentials must be referenced with `secret://` handles. Automatic embedding jobs are declared through `/v1/projects/<ref>/embeddings`; vector buckets are declared through `/v1/projects/<ref>/vector-buckets`. The Compose MVP records these as control-plane declarations and runtime env hints for queues/functions to consume; pgvector itself is still provided by the upstream Postgres stack.

Analytics Buckets are declared through `/v1/projects/<ref>/analytics-buckets`. Each declaration records an Apache Iceberg storage URI, optional REST/catalog URI, warehouse name, credential handle, format version, partitioning, retention, compaction schedule, and masked metadata. Compose records these as control-plane desired state for a lakehouse/ETL reconciler to provision Iceberg tables without changing API, CLI, or UI contracts.

Studio AI Assistant configuration is also managed through project config area `ai`. Set `studio_assistant_enabled`, `studio_assistant_provider`, `studio_assistant_model`, and `studio_assistant_key_handle` to provide the LLM API key handle that upstream Studio/assistant integrations need; Compose records these as `SUPADUPA_STUDIO_AI_ASSISTANT_*` runtime metadata.

Database extensions are listed and toggled through `/v1/projects/<ref>/database/extensions`. The API starts from the upstream Compose init catalog (`pgcrypto`, `uuid-ossp`, `pg_graphql`, `pg_stat_statements`, `pg_cron`, `pgmq`, `vector`, and `supabase_vault`) and records per-project overrides for enabled state, schema, and pinned version. Compose treats these as desired-state declarations for the extension toggle UI; a live DDL worker can reconcile the same records into `CREATE EXTENSION` / `DROP EXTENSION` operations.

Database cron jobs are declared through `/v1/projects/<ref>/database/cron-jobs`. The control plane records pg_cron schedules, SQL commands, database/user targets, active state, runtime limits, masked metadata, project logs, audit events, and usage metrics. Compose treats these as desired-state declarations; a database worker can reconcile them into `cron.schedule` calls without changing API, CLI, or UI contracts.

Database queues are declared through `/v1/projects/<ref>/database/queues`. The control plane records pgmq queue names, schema, retention window, visibility timeout, retry/dead-letter policy, active state, masked metadata, project logs, audit events, and usage metrics. Compose treats these as desired-state declarations; a database worker can reconcile them into `pgmq.create` and related queue policy calls without changing API, CLI, or UI contracts.

Database webhooks are declared through `/v1/projects/<ref>/database/webhooks`. The control plane records trigger schema/table, insert/update/delete events, HTTPS delivery endpoint, method, headers, timeout/retry policy, active state, masked metadata, project logs, audit events, and usage metrics. Compose treats these as desired-state declarations for the upstream database webhook stack feature; a database worker can reconcile them into trigger and HTTP delivery definitions without changing API, CLI, or UI contracts.

Declarative database schemas are recorded through `/v1/projects/<ref>/database/schemas`. The control plane stores migration name, version, target schema, SQL text, SHA-256 checksum, apply order, active state, masked metadata, project logs, audit events, and usage metrics. Compose treats these as desired-state migration records; a database worker or CLI migration runner can reconcile them into live DDL without changing API, CLI, or UI contracts.

Database roles are declared through `/v1/projects/<ref>/database/roles`. The control plane records role flags, connection limits, schema grants, memberships, masked password secret handles, project logs, audit events, and usage metrics. Reserved upstream roles such as `anon`, `authenticated`, `service_role`, and `supabase_admin` cannot be replaced through this surface. Compose records the desired state; a reconciler/worker can apply the same declarations as live Postgres DDL without changing API, CLI, or UI contracts.

Storage buckets are declared through `/v1/projects/<ref>/storage/buckets`. The control plane records public/private policy, file size limits, allowed MIME types, cache policy, AVIF autodetection, masked metadata, project logs, audit events, and usage metrics. Compose treats these as bucket declarations for the upstream Storage API/Studio surface; an operator worker can reconcile the same records into live bucket DDL or Storage API calls.

CDN policy is managed through `/v1/projects/<ref>/cdn/policy`, with invalidations recorded through `/v1/projects/<ref>/cdn/invalidations`. Smart CDN object-change revalidation is exposed through `/v1/projects/<ref>/cdn/object-events`: storage webhooks or operators post bucket/object events, and the control plane records a scoped invalidation when CDN and smart revalidation are enabled. For Compose MVP deployments this renders cache-control and Smart CDN metadata into Traefik headers middleware on API/custom-domain routes, records explicit and object-event invalidation requests, and meters CDN-enabled projects plus invalidation counts. A real global cache worker can consume the same policy and invalidation ledger in later substrates.

Private network connectivity is declared through `/v1/projects/<ref>/network-connections`. The control plane records PrivateLink, VPC peering, private endpoints, WireGuard, and operator-network requests with provider metadata, CIDR policy, endpoint IDs, masked config, metrics, project logs, and audit events. Compose treats these as operator-facing declarations; Kubernetes or external network automation can reconcile the same records into real cloud/provider connectivity. Sensitive config values must be stored as `secret://` handles.

Branch creates provision a full isolated branch project and then prepare a clone artifact. Compose writes the artifact under `./runtime/projects/<branch-ref>/branch-clone`; without a live clone command, supadupa writes an explicit dry-run `clone-plan.sql`. Configure `SUPADUPA_BRANCH_CLONE_COMMAND` to run a real source-to-branch dump/restore; stdout/stderr becomes the clone transcript. Template variables include `{{source_ref}}`, `{{branch_ref}}`, `{{branch_id}}`, `{{branch_name}}`, `{{expires_at}}`, `{{source_dir}}`, `{{branch_dir}}`, and `{{clone_path}}`. Kubernetes writes a `branch-clone.yaml` `ProjectBranchClone` manifest beside the branch `Project` resource so an operator can reconcile source-to-branch data copy in-cluster.

Log drain creates write Vector sink fragments under `./runtime/projects/<ref>/log-drains/<drain-id>.toml` by default. Compose mounts that directory into Vector at `/etc/vector/log-drains` alongside the base per-project `vector.yml`.

Logical backups are written under `./runtime/backups` by default. Without a live data-plane dump command, supadupa writes an explicit dry-run `.sql` artifact for local development. Every backup records size, SHA-256 checksum, and `verified_at` after the artifact is readable. Scheduled backup successes and failures are both recorded in project logs and the immutable audit chain. Configure `SUPADUPA_LOGICAL_BACKUP_COMMAND` to run a real dump command; stdout becomes the backup artifact. The control-plane scheduler checks due backup policies every minute by default; set `SUPADUPA_BACKUP_SCHEDULER_TICK` to a Go duration such as `30s` or `5m` to tune that loop. Template variables are shell-quoted and include `{{ref}}`, `{{project_ref}}`, `{{project_id}}`, `{{stack_version}}`, `{{project_dir}}`, `{{compose_file}}`, `{{backup_root}}`, and `{{backup_path}}`.

Example for a running Compose project:

```bash
export SUPADUPA_LOGICAL_BACKUP_COMMAND='docker compose -p {{ref}} -f {{compose_file}} exec -T db pg_dump -U postgres postgres'
```

Restores default to an explicit dry-run `.sql` restore plan. Configure `SUPADUPA_LOGICAL_RESTORE_COMMAND` to run a real restore command; stdout/stderr becomes the restore transcript. Template variables include `{{ref}}`, `{{project_ref}}`, `{{project_dir}}`, `{{compose_file}}`, `{{backup_root}}`, `{{backup_id}}`, `{{backup_kind}}`, `{{backup_path}}`, `{{restore_root}}`, and `{{restore_path}}`.

WAL archives default to explicit dry-run `.wal` artifacts. Each archive records size, SHA-256 checksum, and `verified_at` after the artifact is readable. Configure `SUPADUPA_WAL_ARCHIVE_COMMAND` to run WAL-G, pgBackRest, or an operator wrapper; stdout becomes the WAL artifact. Template variables include `{{ref}}`, `{{project_ref}}`, `{{project_id}}`, `{{domain}}`, `{{project_dir}}`, `{{compose_file}}`, `{{backup_root}}`, `{{segment}}`, `{{archive_bucket}}`, `{{retention_days}}`, `{{wal_root}}`, and `{{wal_path}}`.

Fleet Security & Performance Advisor findings are exposed through `/v1/advisor` and the admin UI Security section. The current evaluator flags project health drift, disabled backups/PITR, open ingress, missing database SSL enforcement, projects not enrolled in fleet advisor mode, missing log drains, and public storage buckets. Findings are derived from control-plane desired state and operational metadata, so future substrate-specific checks can extend the same endpoint without changing CLI or UI contracts.

Billing invoices are generated through `/v1/orgs/<org-id>/billing/invoices` from durable usage snapshots. Invoices are operator-owned draft/open/paid/void artifacts with line items and cent-denominated totals derived from metering counters; supadupa does not integrate with external payment processors unless an operator commercializes the deployment.

SOC 2 / HIPAA control evidence is exposed through `/v1/compliance/report` and the admin UI Security section. The report maps existing platform evidence into controls for immutable audit logging, admin MFA, backup/PITR coverage, DB SSL, ingress allowlists, log retention, secret rotation, and operator-owned DPA/BAA certification posture. It does not certify a deployment; it gives operators structured evidence and action gaps for their own compliance program.

## CLI

The CLI is intentionally a thin client over the versioned Management API. It reads `SUPADUPA_API_URL` and `SUPADUPA_TOKEN`, with `--api` and `--token` available as command-line overrides.

```bash
go run ./cmd/supadupa-cli --api http://localhost:8080 bootstrap \
  --email admin@example.com \
  --password super-secure

export SUPADUPA_TOKEN=<token-from-bootstrap-or-login>

go run ./cmd/supadupa-cli users create \
  --email operator@example.com \
  --password initial-secret \
  --role admin
go run ./cmd/supadupa-cli users list
go run ./cmd/supadupa-cli mfa status
go run ./cmd/supadupa-cli mfa enroll
go run ./cmd/supadupa-cli mfa verify --code 123456
go run ./cmd/supadupa-cli mfa disable --code 123456
go run ./cmd/supadupa-cli provisioner status
go run ./cmd/supadupa-cli scim service-provider-config
go run ./cmd/supadupa-cli scim users
go run ./cmd/supadupa-cli scim create-user \
  --user-name dev@example.com \
  --display-name "Dev User" \
  --role developer
go run ./cmd/supadupa-cli scim groups --org-id <org-id>
go run ./cmd/supadupa-cli scim create-group \
  --org-id <org-id> \
  --display-name Developers \
  --slug developers \
  --member dev@example.com
go run ./cmd/supadupa-cli orgs create --name Platform
go run ./cmd/supadupa-cli orgs get --id <org-id>
go run ./cmd/supadupa-cli orgs update --id <org-id> --name "Platform Engineering"
go run ./cmd/supadupa-cli members upsert \
  --org-id <org-id> \
  --email dev@example.com \
  --role developer
go run ./cmd/supadupa-cli teams create \
  --org-id <org-id> \
  --name Developers \
  --slug developers
go run ./cmd/supadupa-cli teams add-member \
  --org-id <org-id> \
  --slug developers \
  --email dev@example.com
go run ./cmd/supadupa-cli quotas set \
  --org-id <org-id> \
  --max-projects 10 \
  --max-cpu 32 \
  --max-ram-mb 131072 \
  --max-disk-gb 2000 \
  --max-disk-iops 48000
go run ./cmd/supadupa-cli usage current --org-id <org-id>
go run ./cmd/supadupa-cli usage snapshot --org-id <org-id>
go run ./cmd/supadupa-cli usage snapshots --org-id <org-id> --limit 10
go run ./cmd/supadupa-cli billing create-invoice \
  --org-id <org-id> \
  --usage-snapshot-id <snapshot-id> \
  --currency USD \
  --status draft \
  --due-days 30
go run ./cmd/supadupa-cli billing invoices --org-id <org-id> --limit 10
go run ./cmd/supadupa-cli billing get-invoice --org-id <org-id> --invoice-id <invoice-id>
go run ./cmd/supadupa-cli settings defaults set \
  --domain supadupa.test \
  --stack-version latest \
  --profile full \
  --tier small \
  --backup-schedule daily \
  --smtp-enabled \
  --smtp-host smtp.example.com \
  --smtp-port 587 \
  --smtp-sender-email noreply@example.com \
  --smtp-password-handle secret://platform/smtp-password \
  --smtp-tls-mode starttls
go run ./cmd/supadupa-cli settings defaults get
go run ./cmd/supadupa-cli hosts create \
  --name host-a \
  --address 10.0.0.10 \
  --cpu 16 \
  --ram-mb 65536 \
  --disk-gb 1000 \
  --disk-iops 48000 \
  --projects 20
go run ./cmd/supadupa-cli hosts list
go run ./cmd/supadupa-cli hosts get --id <host-id>
go run ./cmd/supadupa-cli hosts delete --id <host-id> --yes
go run ./cmd/supadupa-cli projects create \
  --org-id <org-id> \
  --ref alpha \
  --name Alpha

Omitted project create flags for domain, stack version, profile, tier, and backup schedule inherit the platform defaults. Supported stack profiles are `full`, `essential`, and `orioledb`; `orioledb` provisions the full stack with OrioleDB preview metadata and seeds `database.orioledb_profile=preview`.

Platform defaults also include operator SMTP settings for platform mail and integrations. The SMTP password is stored as a `secret://` handle only; raw SMTP passwords are rejected by the API and should stay behind the same audited secret-management path used by project secrets.

go run ./cmd/supadupa-cli projects create \
  --org-id <org-id> \
  --ref alpha-oriole \
  --name "Alpha Oriole" \
  --profile orioledb

go run ./cmd/supadupa-cli projects list
go run ./cmd/supadupa-cli projects connect --ref alpha
go run ./cmd/supadupa-cli projects cli-profile --ref alpha --format env
go run ./cmd/supadupa-cli projects cli-profile --ref alpha --format toml
go run ./cmd/supadupa-cli access grant \
  --ref alpha \
  --subject-type team \
  --subject-id developers \
  --role developer
go run ./cmd/supadupa-cli access list --ref alpha
go run ./cmd/supadupa-cli access review --org-id <org-id>
go run ./cmd/supadupa-cli config set \
  --ref alpha \
  --area database \
  --set extension_toggle_ui=true \
  --set performance_advisor_mode=fleet
go run ./cmd/supadupa-cli config set \
  --ref alpha \
  --area realtime \
  --set broadcast_replay=true \
  --set broadcast_from_database=true
go run ./cmd/supadupa-cli config set \
  --ref alpha \
  --area pooler \
  --set dedicated_pooler_enabled=true \
  --set dedicated_pooler_tier=medium \
  --set pool_mode=both \
  --set default_pool_size=50 \
  --set max_client_connections=500
go run ./cmd/supadupa-cli config set \
  --ref alpha \
  --area auth \
  --set site_url=https://app.example.com \
  --set additional_redirects=https://app.example.com/auth/callback \
  --set mfa_totp_enabled=true \
  --set mfa_totp_enroll_enabled=true \
  --set mfa_totp_verify_enabled=true \
  --set mfa_phone_enabled=true \
  --set mfa_phone_enroll_enabled=true \
  --set mfa_phone_verify_enabled=true \
  --set mfa_phone_otp_length=8 \
  --set mfa_phone_max_frequency=20s \
  --set captcha_provider=turnstile \
  --set captcha_site_key=<site-key> \
  --set captcha_secret_handle=secret://projects/alpha/captcha-secret
go run ./cmd/supadupa-cli config set \
  --ref alpha \
  --area auth_providers \
  --set oauth_google_enabled=true \
  --set oauth_google_client_id=<client-id> \
  --set oauth_google_client_secret_handle=secret://projects/alpha/google_oauth_secret \
  --set oauth_discord_enabled=true \
  --set oauth_discord_client_id=<discord-client-id> \
  --set oauth_discord_client_secret_handle=secret://projects/alpha/discord_secret \
  --set oauth_gitlab_enabled=true \
  --set oauth_gitlab_url=https://gitlab.example.com \
  --set oauth_oidc_enabled=true \
  --set oauth_oidc_issuer_url=https://issuer.example.com \
  --set oauth_oidc_client_id=<oidc-client-id> \
  --set oauth_oidc_client_secret_handle=secret://projects/alpha/oidc_secret \
  --set sms_provider=messagebird \
  --set sms_messagebird_originator=Supadupa \
  --set sms_messagebird_access_key_handle=secret://projects/alpha/messagebird_key \
  --set saml_enabled=true \
  --set saml_metadata_url=https://idp.example.com/metadata
go run ./cmd/supadupa-cli config set \
  --ref alpha \
  --area email_templates \
  --set confirmation_subject="Confirm your account" \
  --set magic_link_subject="Your magic link" \
  --set notification_password_changed_enabled=true \
  --set notification_password_changed_subject="Password changed" \
  --set notification_identity_linked_enabled=true \
  --set notification_identity_linked_body="Identity linked"
go run ./cmd/supadupa-cli config set \
  --ref alpha \
  --area smtp \
  --set enabled=true \
  --set host=smtp.example.com \
  --set port=587 \
  --set sender_name=Supadupa \
  --set sender_email=noreply@example.com \
  --set username=apikey \
  --set password_handle=secret://projects/alpha/smtp-password \
  --set tls_mode=starttls
go run ./cmd/supadupa-cli services list --ref alpha
go run ./cmd/supadupa-cli services set \
  --ref alpha \
  --service storage=false \
  --service functions=true
go run ./cmd/supadupa-cli domains add \
  --ref alpha \
  --fqdn api.example.com
go run ./cmd/supadupa-cli routes list --ref alpha
go run ./cmd/supadupa-cli log-drains create \
  --ref alpha \
  --target https \
  --config url=https://logs.example.com/ingest
go run ./cmd/supadupa-cli secrets list --ref alpha
go run ./cmd/supadupa-cli secrets rotate --ref alpha --kind jwt_signing_key_current
go run ./cmd/supadupa-cli secrets copy --ref alpha --kind service_role
go run ./cmd/supadupa-cli secrets rotate --ref alpha --kind service_role
go run ./cmd/supadupa-cli projects scale --ref alpha --tier large
go run ./cmd/supadupa-cli backups set-policy \
  --ref alpha \
  --enabled \
  --schedule daily \
  --kind logical
go run ./cmd/supadupa-cli backups trigger --ref alpha
go run ./cmd/supadupa-cli backups restore --ref alpha --backup-id <backup-id>
go run ./cmd/supadupa-cli branches create \
  --ref alpha \
  --branch-ref alpha-preview \
  --name "Alpha Preview" \
  --ttl-hours 24
go run ./cmd/supadupa-cli branches list --ref alpha
go run ./cmd/supadupa-cli branches delete --ref alpha --branch-ref alpha-preview
go run ./cmd/supadupa-cli replicas create \
  --ref alpha \
  --name east \
  --region us-east \
  --tier small \
  --read-weight 100 \
  --failover-priority 1
go run ./cmd/supadupa-cli replicas list --ref alpha
go run ./cmd/supadupa-cli replicas routing --ref alpha
go run ./cmd/supadupa-cli replicas promote --ref alpha --id <replica-id> --reason "planned maintenance"
go run ./cmd/supadupa-cli replicas failover --ref alpha --reason "primary degraded"
go run ./cmd/supadupa-cli replicas delete --ref alpha --id <replica-id>
go run ./cmd/supadupa-cli pitr set-policy \
  --ref alpha \
  --enabled \
  --archive-bucket s3://supadupa-pitr/alpha \
  --retention-days 14
go run ./cmd/supadupa-cli pitr archive --ref alpha
go run ./cmd/supadupa-cli pitr wal-list --ref alpha
go run ./cmd/supadupa-cli functions deploy \
  --ref alpha \
  --name hello \
  --source-file ./functions/hello/index.ts \
  --secret API_KEY=value
go run ./cmd/supadupa-cli functions region \
  --ref alpha \
  --function hello \
  --host-id <host-id> \
  --region us-east-1 \
  --routing-policy nearest
go run ./cmd/supadupa-cli functions regions --ref alpha
go run ./cmd/supadupa-cli functions mount \
  --ref alpha \
  --function hello \
  --bucket assets \
  --mount-path /mnt/assets \
  --prefix public \
  --env-alias ASSETS_MOUNT
go run ./cmd/supadupa-cli functions mounts --ref alpha
go run ./cmd/supadupa-cli auth-clients create \
  --ref alpha \
  --name dashboard \
  --client-id dashboard_app \
  --client-secret-handle secret://projects/alpha/auth/dashboard \
  --redirect-uri https://app.example.com/auth/callback \
  --grant-type authorization_code,refresh_token \
  --scope openid,email,profile
go run ./cmd/supadupa-cli auth-clients list --ref alpha
go run ./cmd/supadupa-cli auth-hooks set \
  --ref alpha \
  --hook-type custom_access_token \
  --target-uri https://hooks.example.com/auth/token \
  --secret-handle secret://projects/alpha/auth/hook \
  --header authorization=secret://projects/alpha/auth/hook-header \
  --timeout-ms 7000 \
  --retry-attempts 2
go run ./cmd/supadupa-cli auth-hooks list --ref alpha
go run ./cmd/supadupa-cli replication create \
  --ref alpha \
  --name orders-etl \
  --type etl \
  --source-schema public \
  --source-table orders \
  --destination s3 \
  --credential-handle secret://projects/alpha/etl-destination \
  --config bucket=analytics-lake,prefix=orders/
go run ./cmd/supadupa-cli replication list --ref alpha
go run ./cmd/supadupa-cli replication delete --ref alpha --id <pipeline-id>
go run ./cmd/supadupa-cli database-extensions list --ref alpha
go run ./cmd/supadupa-cli database-extensions set \
  --ref alpha \
  --name pg_cron \
  --schema extensions \
  --version 1.6 \
  --enabled=false
go run ./cmd/supadupa-cli database-cron create \
  --ref alpha \
  --name refresh-rollups \
  --schedule "*/15 * * * *" \
  --command "select analytics.refresh_rollups();" \
  --timeout-seconds 90 \
  --max-runtime-seconds 120
go run ./cmd/supadupa-cli database-queues create \
  --ref alpha \
  --name events \
  --schema pgmq \
  --retention-minutes 10080 \
  --visibility-timeout-seconds 45 \
  --max-retries 7 \
  --dead-letter-queue events-dlq
go run ./cmd/supadupa-cli database-webhooks create \
  --ref alpha \
  --name orders-events \
  --schema public \
  --table orders \
  --events insert,update \
  --endpoint https://hooks.example.com/orders \
  --header Authorization=secret://projects/alpha/webhooks/orders-token
go run ./cmd/supadupa-cli database-schemas create \
  --ref alpha \
  --name app-schema \
  --version 20260605_001 \
  --schema public \
  --sql-file ./migrations/20260605_001_app_schema.sql \
  --apply-order 10
go run ./cmd/supadupa-cli database-roles create \
  --ref alpha \
  --name app_writer \
  --login \
  --connection-limit 25 \
  --password-secret-handle secret://projects/alpha/db/app-writer \
  --member-of authenticated \
  --grant public=usage,select,insert,update \
  --metadata purpose=application-writes
go run ./cmd/supadupa-cli database-roles list --ref alpha
go run ./cmd/supadupa-cli storage-buckets create \
  --ref alpha \
  --name assets \
  --public \
  --file-size-limit 52428800 \
  --mime-type image/png,image/jpeg,image/webp \
  --cache-control 3600 \
  --avif \
  --metadata purpose=public-assets
go run ./cmd/supadupa-cli storage-buckets list --ref alpha
go run ./cmd/supadupa-cli config set \
  --ref alpha \
  --area ai \
  --set openai_enabled=true \
  --set openai_api_key_handle=secret://projects/alpha/openai \
  --set default_embedding_model=text-embedding-3-small \
  --set studio_assistant_enabled=true \
  --set studio_assistant_key_handle=secret://projects/alpha/studio-ai
go run ./cmd/supadupa-cli embeddings create \
  --ref alpha \
  --name docs-embeddings \
  --source-table documents \
  --source-column body \
  --destination-table document_embeddings \
  --provider openai \
  --model text-embedding-3-small
go run ./cmd/supadupa-cli vector-buckets create \
  --ref alpha \
  --name documents \
  --dimension 1536 \
  --distance cosine \
  --index-method hnsw \
  --storage-backend postgres \
  --metadata purpose=semantic-search
go run ./cmd/supadupa-cli analytics-buckets create \
  --ref alpha \
  --name events \
  --storage-uri s3://lakehouse/events \
  --catalog-uri http://iceberg-rest:8181 \
  --warehouse analytics \
  --credential-handle secret://projects/alpha/iceberg \
  --partitioning "days(created_at)" \
  --retention-days 365
go run ./cmd/supadupa-cli embeddings list --ref alpha
go run ./cmd/supadupa-cli vector-buckets list --ref alpha
go run ./cmd/supadupa-cli analytics-buckets list --ref alpha
go run ./cmd/supadupa-cli cdn set-policy \
  --ref alpha \
  --enabled \
  --browser-ttl 300 \
  --edge-ttl 600 \
  --stale-while-revalidate 30 \
  --smart \
  --include /storage/v1/object/public/*
go run ./cmd/supadupa-cli cdn policy --ref alpha
go run ./cmd/supadupa-cli cdn invalidate --ref alpha --path /storage/v1/object/public/*
go run ./cmd/supadupa-cli cdn object-event --ref alpha --event-id evt-1 --bucket assets --object-path avatars/user.png --event-type object_updated
go run ./cmd/supadupa-cli cdn invalidations --ref alpha
go run ./cmd/supadupa-cli network get --ref alpha
go run ./cmd/supadupa-cli network-connections create \
  --ref alpha \
  --name aws-prod \
  --type privatelink \
  --provider aws \
  --region us-east-1 \
  --cidr 10.0.0.0/16 \
  --endpoint-id vpce-123 \
  --config account_id=123456789012 \
  --config token=secret://projects/alpha/private-link-token
go run ./cmd/supadupa-cli network-connections list --ref alpha
go run ./cmd/supadupa-cli logs list --ref alpha
go run ./cmd/supadupa-cli logs tail --ref alpha
go run ./cmd/supadupa-cli projects activity --ref alpha
go run ./cmd/supadupa-cli audit list
go run ./cmd/supadupa-cli audit integrity
go run ./cmd/supadupa-cli metrics --ref alpha
go run ./cmd/supadupa-cli metrics --prometheus
go run ./cmd/supadupa-cli advisor
go run ./cmd/supadupa-cli compliance report
go run ./cmd/supadupa-cli settings sso get
go run ./cmd/supadupa-cli settings sso set --enabled --idp-entity-id https://idp.example.com/saml --sso-url https://idp.example.com/login --certificate-file ./idp.pem --acs-url https://supadupa.example.com/v1/auth/sso/saml/callback --email-domain example.com --auto-provision --default-role developer
go run ./cmd/supadupa-cli projects destroy --ref alpha --yes
go run ./cmd/supadupa-cli projects destroy --ref alpha --yes --retain-volumes
go run ./cmd/supadupa-cli orgs delete --id <org-id> --yes
```

## MCP server

The MCP server is also a thin client over the versioned Management API. It speaks JSON-RPC over stdio and exposes tools for org/project discovery, platform defaults including SMTP, host capacity targets, org usage snapshots, billing invoices, project detail, Connect payload retrieval, audited secrets/reveal/copy/rotation, fleet/project metrics, fleet advisor, compliance evidence, immutable audit log verification, project config, database extensions/cron/queues/webhooks/schemas/roles, activity, logs, backups/restore/PITR, branches, replicas, routing/failover, domains, routes, log drains, network connections, storage/vector/analytics buckets, Edge Functions, lifecycle actions including upgrade/scale/destroy, and backup triggering.

```json
{
  "mcpServers": {
    "supadupa": {
      "command": "go",
      "args": ["run", "./cmd/supadupa-mcp"],
      "env": {
        "SUPADUPA_API_URL": "http://localhost:8080",
        "SUPADUPA_TOKEN": "<management-api-token>"
      }
    }
  }
}
```

Common tools include `supadupa_list_users`, `supadupa_create_user`, `supadupa_get_provisioner`, `supadupa_get_account_mfa`, `supadupa_enroll_account_mfa`, `supadupa_verify_account_mfa`, `supadupa_disable_account_mfa`, `supadupa_get_scim_service_provider_config`, `supadupa_list_scim_users`, `supadupa_get_scim_user`, `supadupa_create_scim_user`, `supadupa_replace_scim_user`, `supadupa_deprovision_scim_user`, `supadupa_delete_scim_user`, `supadupa_list_scim_groups`, `supadupa_get_scim_group`, `supadupa_create_scim_group`, `supadupa_delete_scim_group`, `supadupa_list_orgs`, `supadupa_create_org`, `supadupa_get_org`, `supadupa_update_org`, `supadupa_delete_org`, `supadupa_list_hosts`, `supadupa_create_host`, `supadupa_get_host`, `supadupa_delete_host`, `supadupa_get_fleet_metrics`, `supadupa_get_advisor_findings`, `supadupa_get_compliance_report`, `supadupa_list_audit_events`, `supadupa_get_audit_integrity`, `supadupa_get_platform_sso`, `supadupa_set_platform_sso`, `supadupa_get_org_usage`, `supadupa_get_org_quota`, `supadupa_set_org_quota`, `supadupa_list_org_members`, `supadupa_upsert_org_member`, `supadupa_list_org_teams`, `supadupa_create_org_team`, `supadupa_list_org_team_members`, `supadupa_add_org_team_member`, `supadupa_get_org_access_review`, `supadupa_list_org_usage_snapshots`, `supadupa_create_org_usage_snapshot`, `supadupa_list_billing_invoices`, `supadupa_create_billing_invoice`, `supadupa_get_billing_invoice`, `supadupa_list_projects`, `supadupa_list_org_projects`, `supadupa_create_project`, `supadupa_get_project`, `supadupa_project_connect`, `supadupa_list_project_access`, `supadupa_grant_project_access`, `supadupa_revoke_project_access`, `supadupa_list_project_secrets`, `supadupa_reveal_project_secret`, `supadupa_record_project_secret_copy`, `supadupa_rotate_project_secret`, `supadupa_get_project_metrics`, `supadupa_get_project_config`, `supadupa_get_project_services`, `supadupa_set_project_services`, `supadupa_list_project_database_extensions`, `supadupa_set_project_database_extension`, `supadupa_list_project_database_cron_jobs`, `supadupa_create_project_database_cron_job`, `supadupa_list_project_database_queues`, `supadupa_create_project_database_queue`, `supadupa_list_project_database_webhooks`, `supadupa_create_project_database_webhook`, `supadupa_list_project_database_schemas`, `supadupa_create_project_database_schema`, `supadupa_list_project_database_roles`, `supadupa_create_project_database_role`, `supadupa_list_project_auth_clients`, `supadupa_create_project_auth_client`, `supadupa_list_project_auth_hooks`, `supadupa_set_project_auth_hook`, `supadupa_list_project_logs`, `supadupa_list_project_backups`, `supadupa_set_project_backup_policy`, `supadupa_restore_project_backup`, `supadupa_set_project_pitr_policy`, `supadupa_list_project_wal_archives`, `supadupa_archive_project_wal`, `supadupa_list_project_branches`, `supadupa_create_project_branch`, `supadupa_delete_project_branch`, `supadupa_list_project_replicas`, `supadupa_create_project_replica`, `supadupa_get_project_replica_routing`, `supadupa_promote_project_replica`, `supadupa_delete_project_replica`, `supadupa_failover_project_replica`, `supadupa_list_project_domains`, `supadupa_add_project_domain`, `supadupa_list_project_log_drains`, `supadupa_create_project_log_drain`, `supadupa_get_project_network`, `supadupa_list_project_network_connections`, `supadupa_create_project_network_connection`, `supadupa_get_project_cdn_policy`, `supadupa_set_project_cdn_policy`, `supadupa_list_project_cdn_invalidations`, `supadupa_create_project_cdn_invalidation`, `supadupa_create_project_cdn_object_event`, `supadupa_list_project_storage_buckets`, `supadupa_create_project_storage_bucket`, `supadupa_list_project_vector_buckets`, `supadupa_create_project_vector_bucket`, `supadupa_list_project_analytics_buckets`, `supadupa_create_project_analytics_bucket`, `supadupa_list_project_replication_pipelines`, `supadupa_create_project_replication_pipeline`, `supadupa_list_project_embedding_jobs`, `supadupa_create_project_embedding_job`, `supadupa_list_project_functions`, `supadupa_deploy_project_function`, `supadupa_list_project_function_regions`, `supadupa_create_project_function_region`, `supadupa_list_project_function_storage_mounts`, `supadupa_create_project_function_storage_mount`, `supadupa_pause_project`, `supadupa_resume_project`, `supadupa_restart_project`, and `supadupa_trigger_backup`.

Project configuration tools include both `supadupa_get_project_config` and `supadupa_set_project_config`, so MCP clients can read and update the same `/v1/projects/{ref}/config/{area}` surface used by the CLI, Terraform provider, and Admin UI.

Project network tools include `supadupa_get_project_network` for the effective network policy plus `supadupa_list_project_network_connections`, `supadupa_create_project_network_connection`, and `supadupa_delete_project_network_connection` for private connectivity declarations.

## Terraform provider

The Terraform provider is a thin IaC client over the same Management API. It supports platform defaults, platform SSO, host capacity targets, org lifecycle, org lookup, org quotas, org members, org teams, org team members, project access grants, project lifecycle, project branches, project read replicas, project backup policies, project PITR policies, project config areas, project OAuth clients, project Auth Hooks, project database extension toggles, project database cron jobs, project database queues, project database webhooks, project declarative database schemas, project database roles, project automatic embedding jobs, Edge Function deployments, Edge Function regional invocation targets, Edge Function storage mounts, project custom domains, project log drains, project private network connections, project storage buckets, project vector buckets, project analytics buckets, project replication pipelines, and project CDN policies. Project and host fields are replace-on-change until the Management API exposes partial updates. Host deletion is refused while projects or replicas reserve capacity on the host; org deletion is refused while projects still belong to the org, matching normal Terraform destroy ordering. Platform defaults, platform SSO, org quota, backup policy, PITR policy, project config, database extension, and CDN policy resources reset to server defaults when destroyed because those records are default-backed control-plane records.

```bash
go build -o ~/.terraform.d/plugins/registry.terraform.io/supadupa/supadupa/0.1.0/darwin_arm64/terraform-provider-supadupa_v0.1.0 ./cmd/terraform-provider-supadupa
```

```hcl
terraform {
  required_providers {
    supadupa = {
      source  = "supadupa/supadupa"
      version = "0.1.0"
    }
  }
}

provider "supadupa" {
  api_url = "http://localhost:8080"
  token   = var.supadupa_token
}

resource "supadupa_platform_defaults" "defaults" {
  domain               = "supadupa.test"
  stack_version        = "latest"
  profile              = "full"
  resource_tier        = "small"
  backup_schedule      = "daily"
  smtp_enabled         = true
  smtp_host            = "smtp.example.com"
  smtp_port            = 587
  smtp_sender_email    = "noreply@example.com"
  smtp_password_handle = "secret://platform/smtp-password"
  smtp_tls_mode        = "starttls"
}

resource "supadupa_platform_sso" "saml" {
  enabled         = true
  idp_entity_id   = "https://idp.example.com/saml"
  sso_url         = "https://idp.example.com/login"
  certificate_pem = var.saml_certificate_pem
  acs_url         = "https://supadupa.example.com/v1/auth/sso/saml/callback"
  metadata_url    = "https://idp.example.com/metadata"
  email_domain    = "example.com"
  auto_provision  = true
  default_role    = "developer"
}

resource "supadupa_host" "east_1a" {
  name                 = "east-1a"
  address              = "10.0.0.12"
  capacity_cpu         = 16
  capacity_ram_mb      = 65536
  capacity_disk_gb     = 1000
  capacity_disk_iops   = 48000
  capacity_projects    = 20
}

resource "supadupa_org" "platform" {
  name = "Platform"
}

resource "supadupa_org_quota" "platform" {
  org_id        = supadupa_org.platform.id
  max_projects  = 25
  max_cpu       = 64
  max_ram_mb    = 131072
  max_disk_gb   = 4096
  max_disk_iops = 24000
}

resource "supadupa_org_member" "developer" {
  org_id = supadupa_org.platform.id
  email  = "dev@example.com"
  role   = "developer"
}

resource "supadupa_org_team" "platform_engineering" {
  org_id = supadupa_org.platform.id
  name   = "Platform Engineering"
  slug   = "platform-engineering"
}

resource "supadupa_org_team_member" "developer_platform" {
  org_id    = supadupa_org.platform.id
  team_slug = supadupa_org_team.platform_engineering.slug
  email     = supadupa_org_member.developer.email
}

resource "supadupa_project" "alpha" {
  org_id        = supadupa_org.platform.id
  ref           = "alpha"
  name          = "Alpha"
  host_id       = supadupa_host.east_1a.id
  domain        = "supadupa.test"
  profile       = "full"
  resource_tier = "small"
}

resource "supadupa_project_access_grant" "platform_engineering_alpha" {
  ref          = supadupa_project.alpha.ref
  subject_type = "team"
  subject_id   = supadupa_org_team.platform_engineering.slug
  role         = "admin"
}

resource "supadupa_project_branch" "alpha_preview" {
  source_ref = supadupa_project.alpha.ref
  ref        = "alpha-preview"
  name       = "Alpha Preview"
  ttl_hours  = 24
}

resource "supadupa_project_replica" "alpha_east" {
  ref               = supadupa_project.alpha.ref
  name              = "east"
  host_id           = supadupa_host.east_1a.id
  region            = "us-east"
  tier              = "small"
  read_weight       = 100
  failover_priority = 1
}

resource "supadupa_project_backup_policy" "alpha" {
  ref      = supadupa_project.alpha.ref
  enabled  = true
  schedule = "daily"
  kind     = "logical"
}

resource "supadupa_project_pitr_policy" "alpha" {
  ref            = supadupa_project.alpha.ref
  enabled        = true
  archive_bucket = "s3://supadupa-pitr/alpha"
  retention_days = 14
}

resource "supadupa_project_config" "alpha_auth_providers" {
  ref  = supadupa_project.alpha.ref
  area = "auth_providers"

  config = {
    oauth_google_enabled              = "true"
    oauth_google_client_id            = var.google_client_id
    oauth_google_client_secret_handle = "secret://projects/alpha/google_oauth_secret"
    oauth_discord_enabled             = "true"
    oauth_discord_client_id           = var.discord_client_id
    oauth_discord_client_secret_handle = "secret://projects/alpha/discord_secret"
    oauth_gitlab_enabled              = "true"
    oauth_gitlab_url                  = "https://gitlab.example.com"
    oauth_oidc_enabled                = "true"
    oauth_oidc_issuer_url             = "https://issuer.example.com"
    oauth_oidc_client_id              = var.oidc_client_id
    oauth_oidc_client_secret_handle   = "secret://projects/alpha/oidc_secret"
    sms_provider                      = "messagebird"
    sms_messagebird_originator        = "Supadupa"
    sms_messagebird_access_key_handle = "secret://projects/alpha/messagebird_key"
    saml_enabled                      = "true"
    saml_metadata_url                 = "https://idp.example.com/metadata"
  }
}

resource "supadupa_project_config" "alpha_smtp" {
  ref  = supadupa_project.alpha.ref
  area = "smtp"

  config = {
    enabled         = "true"
    host            = "smtp.example.com"
    port            = "587"
    sender_name     = "Supadupa"
    sender_email    = "noreply@example.com"
    username        = "apikey"
    password_handle = "secret://projects/alpha/smtp-password"
    tls_mode        = "starttls"
  }
}

resource "supadupa_project_auth_client" "alpha_dashboard" {
  ref                  = supadupa_project.alpha.ref
  name                 = "Dashboard App"
  client_id            = "dashboard_app"
  client_secret_handle = "secret://projects/alpha/auth/dashboard_app"
  redirect_uris        = ["https://app.example.com/auth/callback"]
  grant_types          = ["authorization_code", "refresh_token"]
  scopes               = ["openid", "email", "profile"]
  confidential         = true
}

resource "supadupa_project_auth_hook" "alpha_custom_token" {
  ref            = supadupa_project.alpha.ref
  hook_type      = "custom_access_token"
  enabled        = true
  target_uri     = "https://hooks.example.com/custom-token"
  secret_handle  = "secret://projects/alpha/auth/custom_token_hook"
  timeout_ms     = 3000
  retry_attempts = 2

  headers = {
    Authorization = "secret://projects/alpha/auth/custom_token_header"
  }
}

resource "supadupa_project_database_cron_job" "alpha_refresh_rollups" {
  ref                 = supadupa_project.alpha.ref
  name                = "refresh-rollups"
  schedule            = "*/15 * * * *"
  command             = "select analytics.refresh_rollups();"
  database            = "postgres"
  username            = "postgres"
  active              = true
  timeout_seconds     = 90
  max_runtime_seconds = 120

  metadata = {
    owner    = "analytics"
    password = "secret://projects/alpha/db/cron-password"
  }
}

resource "supadupa_project_database_queue" "alpha_events" {
  ref                        = supadupa_project.alpha.ref
  name                       = "events"
  schema                     = "pgmq"
  retention_minutes          = 10080
  visibility_timeout_seconds = 45
  max_retries                = 7
  dead_letter_queue          = "events-dlq"
  active                     = true

  metadata = {
    owner = "backend"
    token = "secret://projects/alpha/db/pgmq-token"
  }
}

resource "supadupa_project_database_webhook" "alpha_order_events" {
  ref             = supadupa_project.alpha.ref
  name            = "orders-events"
  schema          = "public"
  table           = "orders"
  events          = ["insert", "update"]
  endpoint        = "https://hooks.example.com/orders"
  http_method     = "POST"
  timeout_seconds = 15
  retry_count     = 5
  active          = true

  headers = {
    Authorization = "secret://projects/alpha/webhooks/orders-token"
    X-Source      = "supadupa"
  }

  metadata = {
    owner = "backend"
    token = "secret://projects/alpha/webhooks/meta-token"
  }
}

resource "supadupa_project_database_schema" "alpha_app_schema" {
  ref         = supadupa_project.alpha.ref
  name        = "app-schema"
  version     = "20260605_001"
  schema      = "public"
  sql         = "create table public.accounts(id uuid primary key);"
  apply_order = 10
  active      = true

  metadata = {
    owner = "backend"
    token = "secret://projects/alpha/db/schema-token"
  }
}

resource "supadupa_project_database_role" "alpha_app_writer" {
  ref                    = supadupa_project.alpha.ref
  name                   = "app_writer"
  login                  = true
  inherit                = false
  bypass_rls             = true
  connection_limit       = 25
  password_secret_handle = "secret://projects/alpha/db/app-writer"
  member_of              = ["authenticated"]

  schema_grants = {
    public = "usage,select,insert"
  }

  metadata = {
    purpose = "app"
    api_key = "secret://projects/alpha/db-role-api"
  }
}

resource "supadupa_project_database_extension" "alpha_pg_cron" {
  ref     = supadupa_project.alpha.ref
  name    = "pg_cron"
  schema  = "extensions"
  version = "1.6"
  enabled = true
}

resource "supadupa_project_embedding_job" "alpha_docs_embeddings" {
  ref                = supadupa_project.alpha.ref
  name               = "docs-embeddings"
  source_schema      = "public"
  source_table       = "documents"
  source_column      = "body"
  primary_key_column = "id"
  destination_table  = "document_embeddings"
  destination_column = "embedding"
  provider           = "openai"
  model              = "text-embedding-3-small"
  dimension          = 1536
  schedule           = "manual"
  batch_size         = 100
}

resource "supadupa_project_function" "alpha_hello" {
  ref        = supadupa_project.alpha.ref
  name       = "hello-api"
  entrypoint = "index.ts"
  verify_jwt = true
  source     = "Deno.serve(() => new Response('ok'))"

  secrets = {
    API_KEY = "secret://projects/alpha/functions/hello-api-key"
  }
}

resource "supadupa_project_function_region" "alpha_hello_us_east" {
  ref            = supadupa_project.alpha.ref
  function_name  = supadupa_project_function.alpha_hello.name
  host_id        = "host-1"
  region         = "us-east-1"
  routing_policy = "primary"
}

resource "supadupa_project_function_storage_mount" "alpha_hello_assets" {
  ref           = supadupa_project.alpha.ref
  function_name = supadupa_project_function.alpha_hello.name
  bucket_name   = "assets"
  mount_path    = "/mnt/assets"
  read_only     = true
  prefix        = "public"
  env_alias     = "ASSETS_MOUNT"
}

resource "supadupa_project_domain" "alpha_api" {
  ref  = supadupa_project.alpha.ref
  fqdn = "api.example.com"
}

resource "supadupa_project_log_drain" "alpha_https_logs" {
  ref    = supadupa_project.alpha.ref
  target = "https"

  config = {
    url   = "https://logs.example.com/ingest"
    token = "secret://projects/alpha/log_drain_token"
  }
}

resource "supadupa_project_storage_bucket" "alpha_assets" {
  ref                 = supadupa_project.alpha.ref
  name                = "assets"
  public              = true
  file_size_limit     = 52428800
  allowed_mime_types  = ["image/jpeg", "image/png", "image/webp"]
  cache_control       = "3600"
  avif_autodetection  = true

  metadata = {
    purpose = "public-assets"
  }
}

resource "supadupa_project_vector_bucket" "alpha_documents" {
  ref             = supadupa_project.alpha.ref
  name            = "documents"
  dimension       = 1536
  distance        = "cosine"
  index_method    = "hnsw"
  storage_backend = "s3"
  storage_uri     = "s3://vectors/documents"

  metadata = {
    purpose    = "semantic-search"
    access_key = "secret://projects/alpha/vector_s3_access_key"
  }
}

resource "supadupa_project_analytics_bucket" "alpha_events" {
  ref                 = supadupa_project.alpha.ref
  name                = "events"
  storage_uri         = "s3://lakehouse/events"
  catalog_uri         = "http://iceberg-rest:8181"
  warehouse           = "analytics"
  credential_handle   = "secret://projects/alpha/iceberg"
  format_version      = 2
  partitioning        = "days(created_at)"
  retention_days      = 365
  compaction_schedule = "0 2 * * *"

  metadata = {
    purpose    = "warehouse"
    access_key = "secret://projects/alpha/lakehouse_s3_access_key"
  }
}

resource "supadupa_project_replication_pipeline" "alpha_orders_etl" {
  ref               = supadupa_project.alpha.ref
  name              = "orders-etl"
  type              = "etl"
  source_schema     = "public"
  source_table      = "orders"
  destination       = "s3"
  destination_uri   = "s3://lakehouse/orders"
  credential_handle = "secret://projects/alpha/orders_etl"

  config = {
    bucket     = "lakehouse"
    prefix     = "orders/"
    access_key = "secret://projects/alpha/lakehouse_s3_access_key"
  }
}

resource "supadupa_project_cdn_policy" "alpha_storage" {
  ref                             = supadupa_project.alpha.ref
  enabled                         = true
  browser_ttl_seconds             = 300
  edge_ttl_seconds                = 600
  stale_while_revalidate_seconds  = 30
  included_paths                  = ["/storage/v1/object/public/*"]
  excluded_paths                  = ["/storage/v1/object/private/*"]
  smart_revalidation              = true
  cache_control                   = "public, max-age=300, s-maxage=600, stale-while-revalidate=30"
}

resource "supadupa_project_network_connection" "alpha_privatelink" {
  ref         = supadupa_project.alpha.ref
  name        = "aws-prod"
  type        = "privatelink"
  provider    = "aws"
  region      = "us-east-1"
  cidrs       = ["10.0.0.0/16", "203.0.113.10"]
  endpoint_id = "vpce-123"

  config = {
    account_id = "123456789012"
    token      = "secret://projects/alpha/private_link_token"
  }
}
```
