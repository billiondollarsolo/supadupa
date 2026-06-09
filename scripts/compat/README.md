# Supadupa Compatibility Runner

This directory contains the first-pass automated checks from `docs/supabase-compat-test-suite.md`.
By default the runner inspects an existing project. It can also create and clean up a disposable project when `SUPADUPA_COMPAT_CREATE_PROJECT=true`.

## Required Environment

```bash
export SUPADUPA_API_URL="https://api.<control-domain>"
export SUPADUPA_TEST_EMAIL="admin@example.test"
export SUPADUPA_TEST_PASSWORD="..."
export SUPADUPA_TEST_REF="smoke"
```

The project ref must already exist and be reachable through the public routes. When create mode is enabled, the ref must not already exist.

## Optional Environment

```bash
export SUPADUPA_COMPAT_ARTIFACT_ROOT="/tmp/supadupa-compat"
export SUPADUPA_CLI_BIN="/path/to/supadupa-cli"
export SUPADUPA_REPO_ROOT="/root/supadupa"
export SUPADUPA_COMPAT_FUNCTION_NAME="hello"
export SUPADUPA_ISOLATION_REF="another-project-ref"
export SUPADUPA_ADMIN_URL="https://admin.<control-domain>"
```

Create-mode and upgrade-matrix options:

```bash
export SUPADUPA_COMPAT_CREATE_PROJECT="true"
export SUPADUPA_TEST_ORG_ID="<org-id>"
export SUPADUPA_APPS_DOMAIN="apps.<control-domain>"
export SUPADUPA_STACK_VERSION="15.8.1.060"
export SUPADUPA_COMPAT_KEEP_PROJECT="false"

export SUPADUPA_COMPAT_UPGRADE_MATRIX="true"
export SUPADUPA_UPGRADE_TARGETS="15.8.1.085"
export SUPADUPA_COMPAT_RESTORE_VALIDATE="true"
```

Create-mode writes a `created-project` marker after the project create API succeeds. `run.sh` destroys that project on exit unless `SUPADUPA_COMPAT_KEEP_PROJECT=true`. Use `SUPADUPA_COMPAT_RETAIN_VOLUMES=true` only when you intentionally want Docker volumes left behind for forensics.

The restore phase is destructive. It only runs when `SUPADUPA_COMPAT_RESTORE_VALIDATE=true`, and it only mutates a project created by the same compatibility run unless `SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE=true` is explicitly set.

PITR restore-to-time validation is also destructive. It only runs when `SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE=true`, `SUPADUPA_COMPAT_PHYSICAL_BACKUP_VALIDATE=true`, and the selected project was created by the same compatibility run unless `SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE=true` is explicitly set. Full hosted-grade proof requires a configured durable off-host backup target; disposable loopback targets are allowed for probe coverage but are rejected before full PITR restore validation. The hosted-grade path also rejects local/private target endpoints and validates restore semantics by inserting a pre-target SQL marker, inserting a post-target marker after the recovery timestamp, running restore-to-time, and asserting that only the pre-target marker remains.

Read-replica validation is opt-in because it allocates and deletes a live replica:

```bash
export SUPADUPA_COMPAT_REPLICA_VALIDATE=true
export SUPADUPA_COMPAT_ENABLE_REPLICA_FEATURE=true
```

By default this phase only runs against a project created by the same compatibility run. Set `SUPADUPA_COMPAT_ALLOW_REPLICA_ON_EXISTING=true` only when you intentionally want to create and delete a replica on an existing project. When `SUPADUPA_COMPAT_ENABLE_REPLICA_FEATURE=true` and `SUPADUPA_TEST_ORG_ID` is set, the phase snapshots org feature overrides, temporarily enables `read_replicas`, and restores the original overrides on cleanup. The phase validates create/list/routing/route-manifest/delete, including the public `db-replica-<replica>-<ref>.<apps-domain>:5432` URI and Traefik upstream alias. Compose replica recovery waits up to `SUPADUPA_REPLICA_READY_TIMEOUT_SECONDS`, defaulting to 240 seconds. Set `SUPADUPA_COMPAT_REPLICA_PROMOTE_VALIDATE=true` and/or `SUPADUPA_COMPAT_REPLICA_FAILOVER_VALIDATE=true` only on disposable projects to validate destructive promotion and failover. When both are enabled, the phase creates a second replica, promotes the first, fails over to the second, and relies on project cleanup to remove promoted replica resources.

Preview branch validation is opt-in because it creates and deletes a temporary branch project:

```bash
export SUPADUPA_COMPAT_BRANCH_VALIDATE=true
```

By default this phase only runs against a project created by the same compatibility run. Set `SUPADUPA_COMPAT_ALLOW_BRANCH_ON_EXISTING=true` only when you intentionally want to create and delete a branch project on an existing source project. When `SUPADUPA_TEST_ORG_ID` is set, the phase snapshots org feature overrides, temporarily enables `preview_branches`, and restores the original overrides on cleanup. The phase validates data-less branch create/list/connect/route-manifest/public Auth health/delete, including branch API, Studio, Storage S3, direct Postgres, and pooler hostnames.

Two-project remote isolation normally uses `SUPADUPA_ISOLATION_REF` or auto-discovers another existing project. To prove multi-project isolation on a clean install, let the isolation phase create and destroy its own peer project:

```bash
export SUPADUPA_COMPAT_CREATE_ISOLATION_PROJECT=true
export SUPADUPA_TEST_ORG_ID="<org-id>"
```

The disposable peer uses the same domain, stack version, profile, tier, and host settings as the primary compat run unless the `SUPADUPA_ISOLATION_*` overrides are set. The phase waits for the peer's public Auth health, validates distinct public API/Studio/Storage S3/direct Postgres/pooler surfaces, route files, Docker networks, Realtime key rejection, same-name Edge Function project scoping, DB password isolation, and S3 credential isolation, then destroys the peer project on exit.

Durable S3-compatible backup target validation is opt-in and uses an operator-provided off-host target such as Cloudflare R2, AWS S3, or remote MinIO:

```bash
export SUPADUPA_COMPAT_DURABLE_BACKUP_TARGET=true
export SUPADUPA_COMPAT_DURABLE_S3_ENDPOINT="https://<account>.r2.cloudflarestorage.com"
export SUPADUPA_COMPAT_DURABLE_S3_REGION="auto"
export SUPADUPA_COMPAT_DURABLE_S3_BUCKET="supadupa-compat"
export SUPADUPA_COMPAT_DURABLE_S3_ACCESS_KEY_ID="<access-key>"
export SUPADUPA_COMPAT_DURABLE_S3_SECRET_ACCESS_KEY="<secret-key>"
export SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true
```

The durable phase creates or updates a named Management API backup target, tests it server-side, requires `durable_off_host=true`, `recovery_ready=true`, and `last_test_status=passed`, then writes the target ID into the shared artifact directory. `SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true` should also be set on the API process for hosted-grade deployments so physical backup uploads, WAL archive uploads, and derived PITR archive buckets reject missing, untested, or loopback targets. When the runner has that flag enabled, `19-durable-backup-target.sh` also calls `GET /v1/runtime-config` and fails unless the remote API process reports `recovery.require_recovery_ready_targets=true`. `16-recoverability-pitr.sh` prefers that artifact target for physical backup and PITR validation. Set `SUPADUPA_COMPAT_DURABLE_S3_CREATE_BUCKET=true` only when the credentials are allowed to create the bucket, and set `SUPADUPA_COMPAT_DURABLE_S3_FORCE_PATH_STYLE=true` for providers that require path-style requests.

RustFS backup target validation is opt-in because it pulls and starts `rustfs/rustfs` locally:

```bash
export SUPADUPA_COMPAT_RUSTFS_BACKUP_TARGET=true
export SUPADUPA_COMPAT_RUSTFS_IMAGE="rustfs/rustfs:1.0.0-beta.2"
```

The RustFS phase starts a loopback-only container, creates a bucket with SigV4, creates and tests a temporary Supadupa backup target, verifies loopback RustFS does not satisfy off-host recoverability gates, and deletes the target/container on exit. Project logical backup, physical backup, and WAL artifact upload through RustFS only run for compat-created disposable projects. When strict recovery-ready targets are enabled, the RustFS phase expects physical/WAL uploads and PITR bucket derivation to be rejected while logical upload plumbing can still run. When `SUPADUPA_TEST_ORG_ID` is set, the phase snapshots org feature overrides, temporarily enables `pitr` for WAL upload validation, and restores the original overrides on cleanup. Set `SUPADUPA_COMPAT_RUSTFS_REQUIRE_PHYSICAL=true` or `SUPADUPA_COMPAT_RUSTFS_REQUIRE_WAL=true` when the run must fail instead of skipping those deep checks. Control-plane backup upload through RustFS is skipped unless both `SUPADUPA_COMPAT_RUSTFS_KEEP_TARGET=true` and `SUPADUPA_COMPAT_RUSTFS_PLATFORM_BACKUP=true` are set, because platform backup records must remain restorable after the phase ends.

For a persistent local dev target, run RustFS with `SUPADUPA_COMPAT_RUSTFS_KEEP_TARGET=true`. This keeps the container and backup target registered after the phase so platform backups created during the run still point at readable S3-compatible storage. It is useful for local restore drills, but because the endpoint is loopback it must still fail the off-host recoverability gates.

If `SUPADUPA_CLI_BIN` is not set, the scripts run the local CLI with:

```bash
go run ./cmd/supadupa-cli
```

`projects gen-types` also honors the CLI's existing optional environment:

```bash
export SUPADUPA_POSTGRES_META_IMAGE="public.ecr.aws/supabase/postgres-meta:v0.96.6"
export SUPADUPA_TYPEGEN_DOCKER_NETWORK="host"
```

The official Supabase CLI migration phase uses this command by default:

```bash
export SUPADUPA_SUPABASE_CLI="npx -y supabase@latest"
```

The default runner also probes official `supabase gen types typescript --db-url`. If upstream still rejects the BYO-domain TLS chain, the phase records `supabase_cli.typegen.official_upstream_tls_caveat` instead of failing the whole suite; if upstream fixes the behavior, the same phase records a normal pass with generated type output.

To track latest plus pinned stable CLI versions in one pass, set:

```bash
export SUPADUPA_SUPABASE_CLI_MATRIX="latest 2.105.0"
```

The matrix runs CLI classification, official DB workflows, official typegen probing, and Supadupa wrapper typegen for each version under isolated artifacts.

## Usage

Run the full first-pass suite:

```bash
scripts/compat/run.sh
```

Run one phase:

```bash
scripts/compat/run.sh 03-postgres.sh
```

Run a present non-default phase directly:

```bash
scripts/compat/23-storage-deep.sh
```

Run a disposable project through an older stable release and upgrade to the current supported stable:

```bash
export SUPADUPA_TEST_REF="compat-$(date +%H%M%S)"
export SUPADUPA_COMPAT_CREATE_PROJECT=true
export SUPADUPA_STACK_VERSION="15.8.1.049"
export SUPADUPA_COMPAT_UPGRADE_MATRIX=true
scripts/compat/run.sh
```

The upgrade matrix mutates the selected project. It triggers a pre-upgrade backup for each target, verifies restore-critical metadata (`started_at`, `finished_at`, `verified_at`, `size_bytes`, `checksum_sha256`, and local or remote location), upgrades with that backup ID, verifies rollback availability in the response, verifies public health, then reruns the configured compatibility phases. By default the post-upgrade phase includes deep Auth, Storage, Realtime, and Edge Functions checks, not only shallow HTTP smoke checks. If `SUPADUPA_UPGRADE_TARGETS` is empty, the matrix fetches `/v1/stack-releases` and uses the newest exposed stable release as the target. Override `SUPADUPA_UPGRADE_VERIFY_PHASES` to change the post-upgrade checks, or set `SUPADUPA_UPGRADE_SKIP_INITIAL_VERIFY_PHASES=true` to avoid duplicating the initial checks when running through `run.sh`. Set `SUPADUPA_UPGRADE_REALTIME_CONTINUITY_VALIDATE=true` to keep an active public Realtime client subscribed through the upgrade and require it to resubscribe and receive a post-upgrade broadcast after health returns. For hosted-grade upgrade drills, start the control plane with `SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true`; the API will reject upgrades whose selected pre-upgrade backup is local-only or on an untested/non-durable target. When the runner has `SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true`, the matrix first checks `GET /v1/runtime-config` and requires the remote API process to report `upgrade.require_durable_backup=true`. When `SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE=true`, it similarly requires `upgrade.failure_auto_restore=true`.

To prove every built-in older stable release can move to the newest exposed stable, run one disposable project per source version. For example:

```bash
for source in 15.8.1.049 15.8.1.054 15.8.1.060; do
  export SUPADUPA_TEST_REF="compat-up-${source//./-}-$(date +%H%M%S)"
  export SUPADUPA_COMPAT_CREATE_PROJECT=true
  export SUPADUPA_STACK_VERSION="$source"
  export SUPADUPA_COMPAT_UPGRADE_MATRIX=true
  unset SUPADUPA_UPGRADE_TARGETS
  scripts/compat/run.sh
done
```

Public Postgres and pooler probes use `SUPADUPA_COMPAT_POOLER_TIMEOUT_SECONDS`, defaulting to `240`, because cold disposable projects can expose API health before Supavisor is ready on every public listener.

Failed-upgrade recovery validation is opt-in. Start the control plane with `SUPADUPA_COMPAT_UPGRADE_FAILURE_TARGETS=<stable-version>`, then run the matrix with `SUPADUPA_UPGRADE_FAILURE_TARGETS=<stable-version>` or the same `SUPADUPA_COMPAT_UPGRADE_FAILURE_TARGETS` exported in the runner environment. The runner sends the guarded compat header, expects HTTP 409, verifies the response includes previous/target versions, backup metadata, `rollback_available`, `rollback_attempted`, and any rollback error, confirms the project stayed on the previous version, and verifies public health. Set `SUPADUPA_UPGRADE_FAILURE_RESTORE_VALIDATE=true` to additionally restore the pre-upgrade backup; that restore is guarded to projects created by the same compat run unless `SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE=true` is set.

Realtime upgrade continuity validation is opt-in because it is timing-sensitive during image pulls and container replacement. Run it on disposable projects with `SUPADUPA_UPGRADE_REALTIME_CONTINUITY_VALIDATE=true`. The matrix starts `realtime-upgrade-continuity-probe.mjs` before `/v1/projects/<ref>/upgrade`, waits for health after the upgrade, then signals the probe to assert reconnect/resubscribe behavior and post-upgrade broadcast delivery. The probe waits for a post-upgrade subscribed socket before sending the validation broadcast, so a transient reconnect does not force Supabase JS `send()` into REST fallback. Tune `SUPADUPA_UPGRADE_REALTIME_READY_TIMEOUT_SECONDS`, `SUPADUPA_UPGRADE_REALTIME_FINISH_TIMEOUT_SECONDS`, and `SUPADUPA_REALTIME_UPGRADE_TIMEOUT_MS` for slow upgrades.

Artifacts are written to:

```bash
/tmp/supadupa-compat/$SUPADUPA_TEST_REF
```

The runner appends machine-readable results to:

```bash
/tmp/supadupa-compat/$SUPADUPA_TEST_REF/results.jsonl
```

Secrets such as bearer tokens, anon keys, and database passwords are stored only as restricted files in the artifact directory. They are not printed in status output.

## GitHub Actions

`.github/workflows/compat.yml` runs local checks on push and pull request:

```txt
go test ./...
npm --prefix frontend run build
bash -n scripts/compat/*.sh
```

The same workflow runs the live compatibility suite on schedule or by manual dispatch when these repository secrets are configured:

```txt
SUPADUPA_COMPAT_API_URL
SUPADUPA_COMPAT_EMAIL
SUPADUPA_COMPAT_PASSWORD
```

Optional live-run secrets:

```txt
SUPADUPA_COMPAT_TEST_REF
SUPADUPA_COMPAT_ORG_ID
SUPADUPA_COMPAT_APPS_DOMAIN
```

Durable hosted-grade recovery runs require repository secrets for an off-host S3-compatible target:

```txt
SUPADUPA_COMPAT_DURABLE_S3_ENDPOINT
SUPADUPA_COMPAT_DURABLE_S3_REGION
SUPADUPA_COMPAT_DURABLE_S3_BUCKET
SUPADUPA_COMPAT_DURABLE_S3_ACCESS_KEY_ID
SUPADUPA_COMPAT_DURABLE_S3_SECRET_ACCESS_KEY
```

Optional durable profile repository variables:

```txt
SUPADUPA_COMPAT_CREATE_PROJECT=true
SUPADUPA_COMPAT_DURABLE_BACKUP_TARGET=true
SUPADUPA_COMPAT_PHYSICAL_BACKUP_VALIDATE=true
SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE=true
SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true
SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true
SUPADUPA_COMPAT_DURABLE_S3_CREATE_BUCKET=false
SUPADUPA_COMPAT_DURABLE_S3_FORCE_PATH_STYLE=false
SUPADUPA_COMPAT_DURABLE_BACKUP_TARGET_NAME=compat-durable
SUPADUPA_COMPAT_DURABLE_S3_PREFIX=compat/nightly
SUPADUPA_STACK_VERSION=15.8.1.049
SUPADUPA_COMPAT_UPGRADE_MATRIX=true
SUPADUPA_UPGRADE_FAILURE_TARGETS=15.8.1.085
SUPADUPA_UPGRADE_FAILURE_RESTORE_VALIDATE=true
SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE=false
```

Manual dispatch can inspect an existing project or create a disposable project. Disposable create requires `SUPADUPA_COMPAT_ORG_ID`; the workflow sets `SUPADUPA_COMPAT_KEEP_PROJECT=false` so cleanup runs on exit. Destructive PITR validation, failed-upgrade backup restore validation, and failed-upgrade auto-restore validation are only allowed when the workflow is creating a disposable project. Failed-upgrade targets also require `upgrade_matrix=true`; the target API process must have matching guarded failure injection enabled with `SUPADUPA_COMPAT_UPGRADE_FAILURE_TARGETS`. Set `upgrade_failure_auto_restore=true` only when the API process is also started with `SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE=true`, otherwise the matrix will fail because the API correctly did not run a destructive auto-restore. The live job runs `scripts/compat/sanitize-artifacts.sh` and uploads only the sanitized copy plus `_artifact-manifest.tsv`; the sanitizer recursively skips control tokens, revealed secret values, materialized secret env files, password/credential/API-key artifacts, token/refresh/session/cookie/login response bodies, OTP/magic-link/recovery artifacts, and secret payloads.
For upgrade coverage, set `create_project=true`, `upgrade_matrix=true`, and `source_stack_version=15.8.1.049`; leave `upgrade_targets` empty to validate the oldest built-in stable release against the newest exposed release catalog entry.

## Included Checks

`scripts/compat/run.sh` defines the default phase order. The list below also documents additional phase scripts that are present for direct or opt-in use.

When passing explicit phase arguments, use phase filenames from this directory. Slash-containing external phase paths are rejected unless `SUPADUPA_COMPAT_ALLOW_EXTERNAL_PHASES=true` is set for an intentional local run.

- `00-preflight.sh`: required tool checks, `go test ./...`, frontend build, and management API health.
- `01-create-project.sh`: optional disposable project create when `SUPADUPA_COMPAT_CREATE_PROJECT=true`.
- `01-auth-project.sh`: Supadupa login, project inspection, CLI profile export, public HTTPS API URL, and public direct Postgres URL metadata.
- `30-runtime-config.sh`: admin-only runtime-config capture, server guard posture, and redacted response shape validation.
- `02-cli-profile.sh`: JSON/env/TOML profile validation, public URL leakage sweep, workspace binding, handle-only env export, and opt-in audited env materialization.
- `15-tls.sh`: TLS and HTTPS redirect validation for control API/admin, project API/Studio, public direct Postgres, and transaction/session pooler routes.
- `02-rest-auth.sh`: REST rejects missing API keys and accepts the project anon key.
- `03-postgres.sh`: public direct Postgres plus transaction/session Supavisor pooler connections from the CLI profile URLs.
- `04-db-fixture.sh`: creates a small RLS-enabled table and verifies it through PostgREST.
- `27-database-desired-state.sh`: validates live database desired-state apply/delete paths through the Management API: core extension presence, pg_cron scheduling and unscheduling, pgmq queue plus dead-letter queue create/drop, pg_net webhook trigger create/drop, declarative schema SQL execution, database role DDL/grants, and cleanup of temporary SQL objects.
- `28-provider-configs.sh`: validates provider-backed service declarations through the Management API and CLI: log drains, OAuth auth clients, replication pipelines, embedding jobs, vector buckets, and private network connections. It checks raw sensitive config rejection, response/list masking, project metrics counters, temporary feature override restoration, and cleanup.
- `09-supabase-cli-classification.sh`: captures official Supabase CLI version/help output and records the supported data-plane versus Supadupa-wrapper management command matrix.
- `09-supabase-cli-db.sh`: runs official `supabase db push` against the public project DB route, reruns it as a no-op, runs `supabase migration list`, `supabase db pull`, `supabase db diff`, verifies synthetic `supabase migration repair` applied/reverted state, and verifies the migrated row through Postgres and PostgREST.
- `09-supabase-cli-typegen.sh`: probes official `supabase gen types typescript --db-url` against the public DB route, recording either an upstream-fix pass or the known BYO-domain TLS caveat; when the caveat is present, it starts `supadupa-cli projects db-tunnel` and proves official typegen through the local tunnel.
- `09-supabase-cli-matrix.sh`: optional multi-version official CLI validation when `SUPADUPA_SUPABASE_CLI_MATRIX` is set; runs classification, DB workflow, official typegen probe, and Supadupa wrapper typegen per version.
- `04-gen-types.sh`: `supadupa-cli projects gen-types` generates Supabase TypeScript types and verifies the compatibility table appears in the output.
- `05-function-fixture.sh`: optionally deploys the `hello` Edge Function fixture for disposable projects or when `SUPADUPA_COMPAT_DEPLOY_FIXTURES=true`.
- `05-http-surfaces.sh`: Auth health/admin, CORS preflight, signup confirmation behavior, confirmed password login, `/auth/v1/user`, GraphQL, Storage bucket create/object upload/download, private object rejection, signed URL download, and Edge Functions through the public API host.
- `22-auth-deep.sh`: creates a confirmed project user, verifies password grant, hosted-compatible JWT claims, SMS provider runtime config validation, temporary custom access token Auth Hook claim injection on sign-in and refresh, Standard Webhooks signed custom access token Auth Hook secret-handle delivery, before-user-created Auth Hook rejection/no-user-create and allow paths, temporary send-SMS Auth Hook phone OTP delivery and verification, refresh-token rotation, logout refresh revocation, and PostgREST RLS propagation through `auth.uid()`. MFA TOTP enroll/challenge/verify/listFactors is opt-in with `SUPADUPA_COMPAT_AUTH_MFA_VALIDATE=true`. Real third-party SMS delivery is opt-in with `SUPADUPA_COMPAT_AUTH_REAL_SMS_VALIDATE=true`, `SUPADUPA_COMPAT_SMS_PROVIDER`, provider credentials, `SUPADUPA_COMPAT_SMS_PHONE`, and either `SUPADUPA_COMPAT_SMS_OTP_COMMAND` or `SUPADUPA_COMPAT_SMS_OTP_FILE` to supply the received OTP; set `SUPADUPA_COMPAT_SMS_REQUEST_ONLY=true` only to prove provider request acceptance without OTP verification. When Docker can see the project auth container, it also starts a temporary Mailpit SMTP capture container on the project network, applies project SMTP and email-template config, verifies recovery, magic-link, and signup OTP email delivery on the public project Auth host, verifies OTP sessions, validates that a temporary send-email Auth Hook replaces SMTP and receives a usable signup OTP payload, restores SMTP/template config, and removes test users/container.
- `18-storage-s3.sh`: creates a temporary bucket through Storage REST, then uses project S3 credentials and SigV4 requests through `storage_s3_url` on `storage-<ref>.<apps-domain>` to list buckets, put an object with metadata, HEAD it, GET it, range-GET it, fetch it through a presigned URL, list objects, copy it, and delete the objects.
- `23-storage-deep.sh`: validates deeper Storage REST/S3 parity in the default runner: public bucket reads, private anonymous rejection, object list/search, copy/move, signed upload URLs, TUS resumable upload, hosted-style image transformation, user-JWT owner/RLS behavior, user-JWT S3 session-token owner/RLS behavior, cross-user private bucket RLS rejection, cache-control metadata, CDN policy route cache headers, manual CDN invalidation, Smart CDN object-event revalidation, policy restore, and batch delete.
- `06-realtime.sh`: Realtime websocket rejects invalid and missing keys, accepts the project anon key, and delivers a Supabase JS broadcast.
- `24-realtime-deep.sh`: validates deeper Realtime parity with presence sync, Postgres Changes delivery from a temporary RLS-enabled table published to `supabase_realtime`, authenticated private channels, same-project per-user private-channel isolation, database-triggered broadcasts through `realtime.broadcast_changes`, broadcast replay, and client resubscribe/broadcast delivery after a Realtime container restart when Docker can see the project runtime.
- `25-functions-deep.sh`: deploys temporary JWT-required and no-JWT Edge Functions, then validates management metadata/listing, region declaration/list/delete desired-state artifacts, hosted-style regional invocation through `x-region` and `forceFunctionRegion` with `SB_REGION` and `x-sb-edge-region`, storage-mount declaration/list/delete plus mounted object runtime reads, nested multi-object storage mount materialization, mount prefix isolation, read-only mount write rejection, origin Storage object immutability, configurable worker timeout and 504 response, same-name redeploy/update, runtime-restart persistence when Docker is available, inline import-map config and runtime resolution, secret redaction and env injection, auth rejection, method/body/header/query propagation, OPTIONS handling, thrown-error 500 behavior, delete, and post-delete 404 behavior through the public functions route.
- `26-replicas-deep.sh`: optional read-replica create/list/routing/route-manifest/delete validation when `SUPADUPA_COMPAT_REPLICA_VALIDATE=true`, guarded to compat-created projects unless `SUPADUPA_COMPAT_ALLOW_REPLICA_ON_EXISTING=true`; it verifies hosted-shaped public read URIs, internal-only read URIs, read-routing metadata, failover candidate selection, generated TCP route removal after delete, and opt-in disposable-project promote/failover.
- `29-branches-deep.sh`: optional preview branch create/list/connect/route-manifest/public-health/delete validation when `SUPADUPA_COMPAT_BRANCH_VALIDATE=true`, guarded to compat-created projects unless `SUPADUPA_COMPAT_ALLOW_BRANCH_ON_EXISTING=true`; it verifies data-less branch defaults and isolated public API, Studio, Storage S3, direct Postgres, and pooler routes.
- `08-sdk-js.sh`: runs `@supabase/supabase-js` against the public API URL and project anon key, including REST select plus password sign-in and `getUser`.
- `11-metrics.sh`: validates project metrics, fleet host capacity/reservations, fresh telemetry, audit verification, and Prometheus metric export.
- `12-backup-restore.sh`: optional destructive backup/restore validation for a disposable project, including restore-critical metadata checks, backup-list persistence, SQL rollback verification, restore path reporting, and REST visibility.
- `19-durable-backup-target.sh`: optional operator-supplied S3/R2/remote-MinIO backup target validation when `SUPADUPA_COMPAT_DURABLE_BACKUP_TARGET=true`; creates or updates a named target, optionally creates the bucket, runs the server-side probe, requires hosted-grade `durable_off_host`/`recovery_ready` readiness, and writes `durable-backup-target-id` for the recoverability/PITR phase.
- `16-recoverability-pitr.sh`: recoverability shape, local-only/off-host/PITR readiness gate consistency, backup target list redaction and readiness fields, configured backup target probe or disposable Node-based local S3-compatible target probe, loopback-target false-positive guard, guarded logical backup upload through the disposable target for compat-created projects, hosted-shaped backup list shape, real Postgres WAL `segment` plus `segment_source=postgres` checks, restore-to-time 409 response checks, optional physical backup validation when `SUPADUPA_COMPAT_PHYSICAL_BACKUP_VALIDATE=true`, and destructive restore-to-time success plus SQL rollback semantics validation when `SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE=true`.
- `20-rustfs-backup-target.sh`: optional RustFS-backed S3 target validation when `SUPADUPA_COMPAT_RUSTFS_BACKUP_TARGET=true`, including signed bucket create, backup target create/test/list, explicit `durable_off_host=false`/`recovery_ready=false` loopback readiness, loopback recoverability rejection, guarded disposable-project logical/physical backup upload with restore metadata, guarded WAL archive upload with real Postgres segment metadata, optional persistent control-plane backup upload, and cleanup.
- `21-custom-domains.sh`: optional custom-domain validation when `SUPADUPA_COMPAT_CUSTOM_DOMAIN_FQDN` is set, including reserved generated-host rejection for API, Studio, Storage S3, direct DB, and pooler hosts, create/delete, BYO certificate upload/reset, generated-host health, Connect/CLI-profile custom API URL discoverability, real `supadupa-cli` env/TOML/link custom-domain selection, and optional custom-host Auth/REST/Storage/Realtime/Functions routing through `curl --resolve` plus websocket DNS override.
- `07-isolation.sh`: two-project remote surface and secret isolation check, including project-scoped same-name Edge Functions plus cross-project anon key, Realtime JWT, Function JWT, DB password, and S3 protocol credential rejection. It uses `SUPADUPA_ISOLATION_REF` when set, or auto-discovers another project when the API has one.
- `14-public-exposure.sh`: non-destructive Docker port exposure check proving project containers are not host-published and only edge/router containers publish public ports.
- `13-security-boundaries.sh`: non-destructive cross-plane security regression checks for control-plane token misuse, project key misuse, SCIM bearer isolation, Studio auth, secret reveal auth, CLI profile secret handles, and log redaction. Set `SUPADUPA_COMPAT_SCIM_TOKEN` to validate a configured SCIM provisioning token live.
- `17-studio-auth.sh`: captures a Supadupa session cookie, proves unauthenticated Studio is rejected, authenticated Studio loads, and the returned Studio HTML has no obvious local/internal host links.
- `19-stack-releases.sh`: validates that exposed stack releases are complete service manifests, the inspected project uses a catalog version, and unsupported create/upgrade targets are rejected before provisioning or upgrade work.
- `10-upgrade-matrix.sh`: optional stable upgrade matrix when `SUPADUPA_COMPAT_UPGRADE_MATRIX=true`, including deep post-upgrade Auth/Storage/Realtime/Functions verification, opt-in active Realtime continuity through upgrades, opt-in failed-upgrade rollback, and pre-upgrade-backup restore validation.
- `99-cleanup.sh`: automatic disposable project cleanup from the `run.sh` exit trap.

## Support Utilities

- `lib.sh`: shared runner setup, result recording, auth/profile helpers, secret reveal helpers, and CLI wrappers.
- `realtime-probe.cjs`: low-level websocket accept/reject probe for Realtime, including optional custom DNS/TLS override support.
- `realtime-broadcast-probe.mjs`: Supabase JS Realtime broadcast probe.
- `realtime-deep-probe.mjs`: Supabase JS Realtime presence, Postgres Changes, private database broadcast, and replay probe. Postgres Changes inserts are retried while the channel is subscribed because Realtime can report channel subscription before the backing CDC subscription row is fully materialized on fresh or just-upgraded runtimes.
- `realtime-reconnect-probe.mjs`: Supabase JS Realtime reconnect probe that restarts a visible project Realtime container, waits for the same channel to resubscribe, and verifies post-restart broadcast delivery.
- `sdk-js-probe.mjs`: Supabase JS REST/Auth smoke probe.
- `s3-compat-probe.mjs`: SigV4 S3 list/put/get/list-objects/delete probe.
- `s3-bucket-admin.mjs`: SigV4 bucket administration helper used by RustFS validation.
- `s3-reject-probe.mjs`: cross-project S3 credential rejection probe.
- `fixtures/functions/hello/index.ts`: Edge Function fixture used by `05-function-fixture.sh`.
