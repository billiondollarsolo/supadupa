# Supabase Compatibility Validation - 2026-06-06

## Result

**Overall status: PASS for the deployed Supabase runtime surfaces tested by the default runner, PARTIAL for hosted-grade recovery and deeper service parity. Official Supabase CLI public typegen still has an upstream BYO-domain TLS caveat, but official typegen now passes through the Supadupa DB tunnel fallback.**

### 2026-06-07 Update

The broad disposable compatibility run completed against the live control plane with MFA intentionally skipped:

```txt
artifact: /tmp/supadupa-compat-full-085328/compat-full-085328
result: 514 PASS, 4 SKIP
project: compat-full-085328
stack upgrade: 15.8.1.060 -> 15.8.1.085
```

That run validated create, RustFS S3 target plumbing, logical backup, physical backup upload to loopback RustFS, WAL archive upload to loopback RustFS, logical restore rollback, recoverability/PITR 409 gating, function fixture deployment, official Supabase CLI DB workflows, Supadupa typegen, REST/Auth, public direct Postgres, public transaction and session pooler, Storage deep, Realtime deep, Edge Functions deep, Supabase JS SDK select/auth, pre-upgrade backup, stack upgrade, and post-upgrade verification.

During the full run, the post-upgrade pooler initially exposed a real race: Supavisor could start before the `supabase_admin` password/bootstrap state was durable, exit with `FATAL 28P01`, and leave the project degraded until manually restarted after DB bootstrap. The code now makes this repeatable rather than manual:

- Compose create/upgrade stages pooler separately from the non-pooler stack.
- Pooler startup re-applies DB bootstrap, force-recreates pooler, and waits for a stable container.
- The generated pooler service has `restart: unless-stopped`.
- Unit tests assert the staged command order for create and upgrade.

The patched server was rebuilt and restarted, then a fresh focused disposable proof passed without manual recovery:

```txt
artifact: /tmp/supadupa-upgrade-poolerfix-094625/compat-poolfix-094625
result: PASS
stack upgrade: 15.8.1.060 -> 15.8.1.085
validated: public DB, transaction pooler, session pooler, REST auth, Supabase JS SDK select/auth
cleanup: compat-poolfix-094625 destroyed
```

Final local verification after the patch:

```txt
go test ./...
git diff --check
public API health/CORS for https://admin.supadupa.brotechlabs.com
```

The isolation runner now supports clean-install multi-project proof by creating and destroying its own peer project with `SUPADUPA_COMPAT_CREATE_ISOLATION_PROJECT=true`. Live validation passed:

```txt
artifact: /tmp/supadupa-isolation-peer-100449/smoke
primary: smoke
peer: compat-iso-20260607100451-3814928
validated: distinct public API/Studio/Storage S3/direct Postgres/pooler endpoints, separate route files, separate Docker networks, Realtime key isolation, same-name Edge Function project scoping, cross-project anon-key rejection, DB password rejection, S3 credential rejection
cleanup: peer destroyed; no peer containers, route files, project directory, or API metadata remained
```

Supadupa now provisions a project with hosted-style public API, Studio, direct Postgres, Supavisor pooler, REST, GraphQL, Auth, Storage, Realtime, Edge Functions, project metrics, fleet metrics, and gateway API-key enforcement. The default compatibility runner covers the core data-plane path plus deeper Auth, Storage, Realtime, and Edge Functions behavior. Additional opt-in and single-phase scripts cover custom domains, read-replica create/delete routing, and RustFS/S3 backup-target plumbing.

Backup and recovery are not yet equivalent to hosted Supabase on the live `smoke` project. The API and UI now expose the required gates, but `smoke` currently reports `local-backup-only`: no durable off-host S3-compatible target is configured, no off-host artifact is verified, PITR is disabled, no physical base backup is available, and restore-to-time is unavailable until an off-host target, physical backup path, WAL archive, and `SUPADUPA_PITR_RESTORE_COMMAND` are configured and validated.

The live API now exposes redacted runtime guard state through `GET /v1/runtime-config`. The 2026-06-07 live probe returned Compose apply mode and Compose backup defaults enabled, with logical, physical, WAL, logical-restore, and PITR restore commands configured through defaults. It also returned `recovery.require_recovery_ready_targets=false` and `upgrade.require_durable_backup=false`, so the current `smoke` deployment is accurately classified as dev/local recovery mode until a durable off-host target is added and the API is restarted with those guards enabled.

The GitHub compatibility workflow now exposes the same durable recovery profile as CI inputs and repository variables. With `SUPADUPA_COMPAT_DURABLE_S3_*` secrets configured, manual or scheduled runs can create a disposable project, register and server-test an off-host S3/R2 target, run physical backup validation, and run destructive PITR restore semantics. CI rejects destructive PITR validation unless `create_project=true`, and the durable target and upgrade phases now verify `GET /v1/runtime-config` so hosted-grade runner flags fail unless the remote API process was also started with the matching production guards. CI uploads only a sanitizer-produced artifact copy. `scripts/compat/sanitize-artifacts.sh` recursively skips control tokens, revealed secret values, materialized secret env files, password/credential/API-key artifacts, token/refresh/session/cookie/login bodies, OTP/magic-link/recovery artifacts, and secret payload files, then records kept/skipped paths in `_artifact-manifest.tsv`.

The exact official command below still fails against the public Let's Encrypt Postgres endpoint because Supabase CLI rewrites the DB URL and injects Supabase's hosted database CA bundle:

```txt
npx -y supabase@latest gen types typescript --db-url <public-db-url>
```

The database endpoint itself is valid: `psql`, `supabase db push`, direct `postgres-meta` type generation, Supadupa's wrapped type generation, and official Supabase CLI typegen through `supadupa-cli projects db-tunnel` all work. There is no secure Supadupa-side change that makes the exact public-DB upstream command pass for BYO-domain TLS; that still requires an upstream Supabase CLI change to preserve DB URL SSL parameters or only inject Supabase's hosted CA bundle for Supabase-owned DB hosts.

Validation artifacts are stored outside the repo at:

```txt
/tmp/supadupa-compat/compat-035843
/tmp/supadupa-compat/compat-up-051418
/tmp/supadupa-compat/compat-restore-065427
/tmp/supadupa-compat/compat-049-172146
/tmp/supadupa-compat/compat-rpl-021702
/tmp/supadupa-compat/compat-rpl-ha-022956
/tmp/supadupa-compat/compat-rtup-054020
```

The validation project is still running:

```txt
ref: compat-035843
domain: apps.supadupa.brotechlabs.com
created_at: 2026-06-06T03:58:43Z
status: healthy
```

## Environment

| Item | Evidence |
|------|----------|
| Docker | `Docker version 29.1.3` |
| Docker Compose | `Docker Compose version 2.40.3` |
| Go | `go version go1.26.0 linux/amd64` |
| Node | `v22.22.1` |
| npm | `9.2.0` |
| curl | `curl 8.18.0` |
| OpenSSL | `OpenSSL 3.5.5 27 Jan 2026` |
| jq | `jq-1.8.1` |
| psql | `psql (PostgreSQL) 18.4` |
| Official Supabase CLI | `2.105.0` via `npx -y supabase@latest` |

Primary references:

- Supabase CLI npx docs: <https://supabase.com/docs/guides/cli/getting-started?platform=npx&queryGroups=platform>
- Traefik TCP/Postgres STARTTLS routing docs: <https://doc.traefik.io/traefik/v3.3/routing/routers/>
- Supabase upstream Kong route convention: <https://raw.githubusercontent.com/supabase/supabase/master/docker/volumes/api/kong.yml>
- Supabase upstream Kong entrypoint: <https://raw.githubusercontent.com/supabase/supabase/master/docker/volumes/api/kong-entrypoint.sh>

## Summary Matrix

| Area | Status | Evidence |
|------|--------|----------|
| Go tests | PASS | `go test ./...` |
| Frontend build | PASS | `npm --prefix frontend run build` |
| Automated compatibility runner | PASS | `scripts/compat/run.sh` against `smoke` with `compat-035843` isolation; disposable create/upgrade run `compat-up-051418`; focused Auth deep rerun artifact `/tmp/supadupa-compat/smoke` includes MFA skipped, magic-link email capture, and magic-link verification pass |
| Project runtime | PASS | `compat-035843 healthy enabled services updated` |
| Control API health | PASS | `{"status":"ok"}` locally and via public API |
| Control API CORS | PASS | `https://admin.supadupa.brotechlabs.com` receives `Access-Control-Allow-Origin` |
| Startup runtime reconciliation | PASS | API startup rewrites routes and replays project secrets, service toggles, explicit runtime configs, Auth Hooks, and replicas through the provisioner so existing projects pick up generator fixes after control-plane upgrades; Functions service reconciliation force-recreates `edge-runtime` so dispatcher changes apply to existing projects; Kong is force-recreated after rerenders so gateway route/env changes are applied to existing projects |
| DNS/TLS | PASS | `15-tls.sh` validates Let's Encrypt wildcard certs, HTTPS redirects, direct Postgres STARTTLS, and transaction/session pooler STARTTLS |
| Studio SSO gate | PASS | Unauthenticated Studio returns 401; Supadupa session cookie loads Studio HTML through `studio-<ref>` with no obvious local/internal host links |
| REST gateway auth | PASS | No key returns 401; anon `apikey` returns 200 |
| GraphQL gateway auth | PASS | No key returns 401; anon `apikey` returns 200 |
| Auth | PASS | Health/admin endpoints pass, CORS preflight returns browser headers, signup returns confirmation-gated user, confirmed password grant returns a session, `/auth/v1/user` returns the signed-in user, refresh tokens rotate, logout revokes refresh, hosted-compatible JWT claims are present, custom access token Auth Hook adds claims to issued and refreshed JWTs, signed Standard Webhooks Auth Hook delivery works through secret handles, PostgREST RLS sees `auth.uid()` correctly, and local-runtime deep checks validate SMTP-backed recovery, magic-link, and signup OTP email delivery plus OTP verification through the public project Auth host; the same deep script now validates that send-SMS and send-email Auth Hooks replace built-in delivery and receive usable OTP payloads. TOTP MFA remains available as an opt-in compat phase. |
| Storage REST/S3 core | PASS | Default runner validates service-role `apikey` bucket list/create, object upload/download, private anonymous-read rejection, private signed URL download, and project S3 protocol credentials through `storage_s3_url` on `storage-<ref>.<apps-domain>`: SigV4 list buckets, put object with metadata, HEAD metadata, GET, byte-range GET, presigned GET, list objects, copy object, and delete. Live `smoke` passed `18-storage-s3.sh` through `https://storage-smoke.apps.supadupa.brotechlabs.com/storage/v1/s3`. |
| Storage deep script | PASS | `scripts/compat/23-storage-deep.sh` is now in the default runner and validates public bucket reads, list/search, copy/move, signed upload URLs, TUS resumable upload, delete cleanup, user-JWT owner/RLS upload/download, user-JWT S3 session-token owner/RLS upload/download with cross-user denial, owner metadata, cache-control metadata, hosted-style image transformations through `/storage/v1/render/image/public/...`, CDN policy route cache headers, manual CDN invalidation, and Smart CDN object-event revalidation with policy restore. Live `smoke` passed the deep suite after the dedicated S3 host route was reconciled. |
| Supabase JS SDK | PASS | Automated `08-sdk-js.sh` performs REST select plus password sign-in and `getUser` against `smoke` |
| Realtime | PASS | Valid websocket reaches 101; invalid/missing key returns 403; cross-project anon keys are rejected, SDK broadcast received, and presence sync, Postgres Changes, authenticated private channels, same-project per-user private-channel isolation, database-originated broadcast, broadcast replay, client reconnect after Realtime container restart, and active-client continuity through `15.8.1.049 -> 15.8.1.085` upgrade pass through the public API host |
| Edge Functions | PASS | `/functions/v1/hello` returns `{"ok":true,"path":"/hello"}`; default runner deploys temporary JWT-required and no-JWT functions, validates metadata/listing, secret redaction and env injection, JWT rejection/acceptance, method/body/header/query propagation, same-name redeploy/update, runtime-restart persistence, inline import-map config and runtime resolution, Supabase regional invocation semantics through `x-region` and `forceFunctionRegion` with `SB_REGION` plus `x-sb-edge-region`, read-only storage mount write rejection, origin Storage object immutability, OPTIONS handling, thrown-error 500 behavior, delete, and post-delete 404 behavior |
| Two-project isolation | PASS | `smoke` anon key, DB password, S3 protocol credentials, Realtime JWTs, and Edge Function JWTs rejected by `compat-035843`; same function name resolves to each project's own runtime |
| Cross-plane security | PASS | Admin token rejected by project API, project anon/service keys rejected by Management API and SCIM, unauthenticated SCIM is rejected, configured SCIM bearer validation is supported through `SUPADUPA_COMPAT_SCIM_TOKEN`, Studio requires Supadupa auth, secret reveal requires auth, CLI profile uses secret handles, and revealed values are absent from project logs |
| Public direct Postgres | PASS | `psql` direct returns `postgres|postgres` |
| Public pooler | PASS | transaction and session Supavisor listener URLs both return `postgres|postgres` |
| Read replicas | PASS | Disposable `compat-rpl-021702` validated create, healthy list, weighted routing/failover-candidate metadata, generated `db-replica-<replica>-<ref>.apps.supadupa.brotechlabs.com` TCP route, Traefik upstream alias, delete, route removal, feature-override restoration, and project cleanup. Disposable `compat-rpl-ha-022956` validated two-replica promotion and failover, primary routing metadata updates, feature-override restoration, and project cleanup with no replica containers or volumes left behind. |
| Official CLI DB workflows | PASS | Migration applied, no-op rerun succeeds, `migration list` shows remote history, remote schema pulls through `db pull`, `db diff` generates a remote diff, synthetic `migration repair` apply/revert is verified, and the row is visible through Postgres/PostgREST |
| Official CLI `gen types --db-url` | PASS VIA TUNNEL / PUBLIC TLS UPSTREAM | Public DB route still hits the upstream CLI CA handling caveat with `supabase@2.105.0`; `09-supabase-cli-typegen.sh` now starts `supadupa-cli projects db-tunnel` and official typegen succeeds through that local route |
| Supadupa CLI `projects gen-types` | PASS | Generated 4,753-line TypeScript file for `smoke` through public DB route and includes `compat_cli_probe` |
| Direct postgres-meta typegen | PASS | Generated 4,698-line TypeScript file |
| Metrics | PASS | `11-metrics.sh` validates project status/resources/telemetry, fleet host capacity/reservations/telemetry, audit, and Prometheus export |
| Provider-backed service configs | PASS | `28-provider-configs.sh` validates log drains, OAuth auth clients, replication pipelines, embedding jobs, vector buckets, and private network connections through live create/list/delete paths; raw sensitive config is rejected where required, responses/lists mask sensitive fields, metrics counters increment, temporary feature overrides are restored, and cleanup is verified. 2026-06-07 update: self-host OAuth provider defaults/rendering now include Figma and Snapchat, matching the current Supabase self-host provider list; live `smoke` config confirms `oauth_figma_*` and `oauth_snapchat_*` defaults are exposed. |
| Multi-project remote isolation | PASS | `07-isolation.sh` auto-discovered `compat-035843`, proved `smoke` and `compat-035843` have distinct public API/Studio/DB/pooler/S3 endpoints, separate route files and runtime networks, project-scoped same-name Edge Functions, and rejected cross-project anon key, Realtime JWT, DB password, S3 protocol credential, and Edge Function JWT use. The phase now also supports clean-install proof with `SUPADUPA_COMPAT_CREATE_ISOLATION_PROJECT=true`; live artifact `/tmp/supadupa-isolation-peer-100449/smoke` created peer `compat-iso-20260607100451-3814928`, passed the same remote separation checks against `smoke`, destroyed the peer, and verified no peer containers/routes/runtime metadata remained. |
| Audit | PASS | Fleet `audit_verified=true` |
| Backup defaults | PASS | Live `smoke` backup trigger produced a 534,734-byte PostgreSQL dump using the applied Compose default command |
| Backup target validation | PASS / LIVE NOT DURABLE | `POST /v1/backup-storage-targets/<id>/test` writes and reads a probe object through a configured S3-compatible target, records `last_test_*`, redacts credentials, and is exposed in Settings > Backups; target API/UI/Terraform responses now include `durable_off_host`, `recovery_ready`, `readiness_status`, and `readiness_message`, so a local target that passes the S3 probe is still shown as `local-or-loopback` and not recovery-ready; `supadupa_backup_storage_target` plus `supadupa_project_backup_policy.storage_target_id` make target and policy binding repeatable in IaC; `16-recoverability-pitr.sh` validates the target shape; `20-rustfs-backup-target.sh` starts RustFS, creates a bucket through SigV4, creates/tests a temporary target, and verifies loopback RustFS does not satisfy off-host gates; live `smoke` has the persistent local `dev-rustfs-local` target, but still has no durable off-host target configured |
| Physical backup policy | IMPLEMENTED / LIVE NOT CONFIGURED | Backup policies now accept `physical`; applied Compose defaults run `pg_basebackup`, manual and scheduled physical backups create verified physical artifacts, and S3-compatible targets upload them off-host; live `smoke` has no verified physical base backup |
| Restore defaults | PASS | Disposable `compat-restore-065427` validated backup, preflight restore, post-backup mutation, completed restore, SQL rollback verification, REST visibility, and cleanup |
| Platform restore | PASS | Control-plane backups restore through `POST /v1/platform/backups/<id>/restore` and Settings > Backups with exact confirmation, checksum/size validation, S3-compatible rehydration, atomic checkpoint import, source-backup preservation, and post-restore runtime reconciliation |
| Recoverability status | PASS / LIVE LOCAL-ONLY | API/UI distinguish local-only backups, verified off-host backups, target durability/readiness, PITR/WAL state, real Postgres WAL segment identity, PITR restore command configuration, and restore-to-time readiness; live `smoke` remains local-only until a durable S3-compatible target and physical base backup path are configured |
| Advisor/compliance recovery posture | PASS / LIVE ACTION NEEDED | Fleet Advisor now emits platform-level recoverability findings when the recovery-ready target guard, durable upgrade backup guard, or default recovery-ready target is missing. Compliance adds hosted-grade recovery guard and off-host target readiness controls, so local-only deployments show action-needed evidence instead of looking production-ready. |
| Hosted-shaped backup APIs | IMPLEMENTED / LIVE NOT READY | `GET /v1/projects/<ref>/database/backups` lists backups; `POST /v1/projects/<ref>/database/backups/restore-pitr` accepts `recovery_time_target_unix` plus `confirmation`, returns recoverability-rich 409 when not ready, selects only physical base backups at or before the recovery target, and returns 201 after running `SUPADUPA_PITR_RESTORE_COMMAND` through physical backup/WAL/window gates |
| Upgrade guardrails | PASS | Stable allowlist exposes complete manifests for `15.8.1.085`, `15.8.1.060`, `15.8.1.054`, and `15.8.1.049`; verified pre-upgrade backup IDs, optional `SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true` hard gate for tested durable off-host backup artifacts, service-toggle preservation, structured failed-upgrade recovery metadata, guarded compat failure injection, rollback attempt tests, live `15.8.1.060 -> 15.8.1.085`, `15.8.1.054 -> 15.8.1.085`, and `15.8.1.049 -> 15.8.1.085` matrices; final oldest-stable proof `compat-rtup-054020` includes active Realtime continuity plus deep post-upgrade Realtime verification |
| Public exposure | PASS | `14-public-exposure.sh` verifies project containers are not host-published, only edge/router publishes public 80/443/5432/6543, and support ports are loopback-only; outbound-capable services now join a private per-project non-internal `egress` network for hosted-style hooks/OAuth/SMTP/S3/log-drain/Edge Function egress without publishing container ports |

## Hosted-Grade Parity Gaps

The PASS result above proves the current public project runtime behaves like a Supabase data plane for the tested surfaces. It does not yet mean full hosted Supabase Enterprise parity.

| Area | Current status | Next validation required |
|------|----------------|--------------------------|
| CLI parity | PASS / PUBLIC TYPEGEN UPSTREAM CAVEAT | Official `db push`, no-op push, `db pull`, `db diff`, `migration list`, and synthetic `migration repair` apply/revert now pass through the public DB route. The default runner probes official `supabase gen types --db-url`; if the upstream BYO-domain TLS caveat appears, it proves official typegen through `supadupa-cli projects db-tunnel`. `SUPADUPA_SUPABASE_CLI_MATRIX` can repeat classification, DB workflows, public typegen probing, tunnel fallback, and Supadupa wrapper typegen across latest and pinned stable CLI versions. |
| Recovery parity | PARTIAL | Require durable off-host S3/R2/MinIO-compatible target, verified physical base backup, verified WAL archive range, destructive PITR restore with SQL rollback semantics, and failed-upgrade restore validation in a hosted-grade profile. Loopback RustFS remains plumbing coverage only. |
| Custom domains | PASS | BYO cert upload/reset, reserved-host rejection, explicit Traefik TLS routing, metadata, opt-in compat-script create/delete, generated-host health, Connect/CLI-profile custom API URL discoverability, real CLI env/TOML/link custom-domain selection, Terraform custom-domain endpoint attributes, MCP connect payload passthrough, and custom-host Auth/REST/Storage/Realtime/Functions reachability through Traefik are covered. |
| Deep Auth parity | PARTIAL | Refresh token rotation, logout/revocation, hosted-compatible JWT claims, JWT claim/RLS propagation, custom access token Auth Hook claim injection on sign-in and refresh, Standard Webhooks signed Auth Hook secret-handle delivery, before-user-created Auth Hook rejection/no-user-create and allow paths, SMTP config application, password recovery email delivery, magic-link email delivery and verification, signup OTP email delivery, hosted project-domain mail links, OTP verification, send-SMS Auth Hook phone OTP delivery, send-email Auth Hook SMTP replacement, and SMS provider runtime config for OTP expiration/length/max frequency/template/test OTP secret handles now pass when the validator can attach local runtime dependencies. MFA validation remains available through `SUPADUPA_COMPAT_AUTH_MFA_VALIDATE=true`, but is intentionally skipped in the current plan. Real third-party SMS provider delivery is now covered by an opt-in harness path using `SUPADUPA_COMPAT_AUTH_REAL_SMS_VALIDATE=true`, provider credentials stored as temporary secret handles, `SUPADUPA_COMPAT_SMS_PHONE`, and OTP verification from `SUPADUPA_COMPAT_SMS_OTP_COMMAND` or `SUPADUPA_COMPAT_SMS_OTP_FILE`; remaining validation gap is running that path with real provider credentials plus Teams/Enterprise hook validation where the underlying GoTrue runtime supports them. |
| Deep Storage parity | PARTIAL | Public bucket reads, user-JWT/RLS Storage REST access, user-JWT S3 session-token/RLS access, cross-user private bucket RLS denial, copy/move/list/search/delete variants, signed upload URLs, TUS resumable upload, cache-control metadata, hosted-style image transformations, CDN policy route cache headers, manual invalidation, Smart CDN object-event revalidation, expanded S3 protocol operations, and default post-upgrade Storage verification now have runner coverage. Remaining coverage: real external CDN/provider propagation behavior and S3 edge cases beyond HEAD/range/presign/copy/metadata. |
| Deep Realtime parity | PARTIAL | Postgres Changes, presence, cross-project key rejection, authenticated private channels, same-project per-user private-channel owner allow/cross-user deny/anonymous deny, database-triggered broadcasts through `realtime.broadcast_changes`, broadcast replay, client resubscribe and broadcast delivery after Realtime container restart, default post-upgrade Realtime verification, and opt-in active-client upgrade continuity now have runner coverage. Remaining coverage is broader authorization policy edge cases and multi-region Realtime placement beyond the single-host Compose profile. |
| Deep Functions parity | PASS / GEO INFRA PARTIAL | List/deploy/delete, same-name redeploy/update, runtime-restart persistence, JWT-required and no-JWT modes, configurable worker timeout with 504 response, method/body/header/query propagation, env secret injection, inline import-map runtime resolution, region declaration/list/delete desired-state artifacts, hosted-style regional invocation via `x-region` and `forceFunctionRegion`, `SB_REGION`, `x-sb-edge-region`, storage-mount declaration/list/delete, mounted object runtime reads, nested multi-object mount materialization, mount prefix isolation, read-only mount write rejection, origin Storage object immutability, error handling, post-delete 404 behavior, and default post-upgrade function invocation now have runner coverage. Remaining gap is true geographically distributed provider placement/failover beyond the single-host Compose profile. |
| Live desired-state resources | PASS / PROVIDER PROPAGATION PARTIAL | Database extensions, pg_cron jobs, pgmq queues plus dead-letter queues, pg_net database webhook triggers, declarative schema SQL, database roles/grants, storage buckets, GoTrue/SMS/SMTP config secret injection, Auth Hook Standard Webhooks secret injection, hosted-style data-less branch defaults, provider-backed declarations for log drains/auth clients/replication/embeddings/vector buckets/network connections, Docker-log-backed Vector log drain artifact generation, Compose read-replica standby overlay reconciliation, generated read-replica TCP route metadata, guarded read-replica create/list/routing/route-manifest/delete, guarded preview branch create/list/connect/route-manifest/public-health/delete validation, and disposable read-replica promote/failover now apply or record in Compose runtime mode. The default runner now verifies database desired-state resources through live Postgres and provider-backed declaration create/list/delete plus masking/metrics; remaining coverage is real third-party provider propagation beyond local declaration/artifact paths. |

Hosted-grade manual/CI validation should run with explicit durability and upgrade settings. Loopback RustFS is useful for S3 plumbing and local restore drills, but hosted-grade recovery still requires a durable off-host S3/R2/MinIO-compatible target.

Durable off-host recovery profile:

```bash
export SUPADUPA_COMPAT_CREATE_PROJECT=true
export SUPADUPA_COMPAT_DURABLE_BACKUP_TARGET=true
export SUPADUPA_COMPAT_DURABLE_S3_ENDPOINT="https://<account>.r2.cloudflarestorage.com"
export SUPADUPA_COMPAT_DURABLE_S3_REGION="auto"
export SUPADUPA_COMPAT_DURABLE_S3_BUCKET="supadupa-compat"
export SUPADUPA_COMPAT_DURABLE_S3_ACCESS_KEY_ID="<access-key>"
export SUPADUPA_COMPAT_DURABLE_S3_SECRET_ACCESS_KEY="<secret-key>"
export SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true
export SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true
export SUPADUPA_COMPAT_PHYSICAL_BACKUP_VALIDATE=true
export SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE=true
scripts/compat/run.sh
```

Disposable local RustFS plumbing profile:

```bash
export SUPADUPA_COMPAT_CREATE_PROJECT=true
export SUPADUPA_COMPAT_RUSTFS_BACKUP_TARGET=true
export SUPADUPA_COMPAT_PHYSICAL_BACKUP_VALIDATE=true
export SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE=true
scripts/compat/run.sh
```

Persistent local RustFS platform-backup drill:

```bash
export SUPADUPA_COMPAT_RUSTFS_BACKUP_TARGET=true
export SUPADUPA_COMPAT_RUSTFS_KEEP_TARGET=true
export SUPADUPA_COMPAT_RUSTFS_PLATFORM_BACKUP=true
scripts/compat/20-rustfs-backup-target.sh
```

Disposable read-replica routing profile:

```bash
export SUPADUPA_COMPAT_CREATE_PROJECT=true
export SUPADUPA_COMPAT_REPLICA_VALIDATE=true
export SUPADUPA_COMPAT_ENABLE_REPLICA_FEATURE=true
export SUPADUPA_COMPAT_REPLICA_PROMOTE_VALIDATE=true
export SUPADUPA_COMPAT_REPLICA_FAILOVER_VALIDATE=true
scripts/compat/run.sh
```

Stable upgrade and failed-upgrade restore profile:

```bash
export SUPADUPA_COMPAT_CREATE_PROJECT=true
export SUPADUPA_COMPAT_UPGRADE_MATRIX=true
export SUPADUPA_UPGRADE_FAILURE_RESTORE_VALIDATE=true
scripts/compat/run.sh
```

## Fixes Validated In This Run

1. API keys are now Supabase-compatible JWTs.
   - `anon_key` and `service_role` are HS256 JWTs signed by the project JWT secret.
   - Rotation also emits signed JWTs and includes a `jti` so rotated values change.

2. Public Postgres and pooler are routed like hosted Supabase.
   - `db-<ref>.<apps-domain>:5432` routes to project Postgres.
   - `pooler-<ref>.<apps-domain>:6543` routes to transaction pooler.
   - `pooler-<ref>.<apps-domain>:5432` routes to session pooler.
   - `db-replica-<replica>-<ref>.<apps-domain>:5432` routes to project read replicas when replicas are provisioned, preserving the same single-label wildcard certificate topology.
   - Traefik TCP routers use PostgreSQL ALPN so modern libpq works with `sslmode=require`.
   - Public Connect payload and CLI profile pooler URLs now always advertise the externally routed edge ports `6543` and `5432`; configurable pooler ports remain internal Supavisor settings and internal Docker-network URLs.

2a. Generated public project hosts are validated before metadata is accepted.
   - Project refs remain capped at 55 characters so `storage-<ref>`, `studio-<ref>`, `pooler-<ref>`, and `db-<ref>` fit under wildcard DNS.
   - Project creation now rejects apps domains that are valid by themselves but would make generated API, Studio, Storage, direct Postgres, or pooler FQDNs exceed the 253-character DNS name limit.
   - Platform default apps domains are validated against the maximum supported project ref length so future project creation cannot inherit an unroutable domain.
   - Project and branch creation now reject generated hosts that would claim platform hosts such as `admin.<control-domain>` / `api.<control-domain>` or an existing project custom domain.

3. Kong now enforces API keys.
   - REST and GraphQL no-key requests return 401.
   - `apikey` works for anon/service key flows.
   - Auth health remains open.
   - Storage receives the transformed Authorization header.

4. Realtime now matches the self-hosted Supabase tenant convention.
   - `SELF_HOST_TENANT_NAME=${PROJECT_REF}` seeds the project ref tenant.
   - Realtime has a `<ref>.supabase-realtime` network alias, matching upstream Supabase's tenant subdomain convention.
   - Kong routes websocket traffic to `/socket` with `protocol: ws`.

5. GraphQL, Storage, and Edge Functions are wired through the project API host.
   - GraphQL adds `Content-Profile: graphql_public`.
   - Storage includes upstream-compatible forwarded path and S3 protocol env.
   - Edge Runtime dispatches deployed functions by name.

6. Existing live projects were repaired.
   - `compat-035843` regenerated through the current provisioner and remains healthy.
   - `smoke` was regenerated, had legacy API keys rotated, now enforces gateway API keys, and now has TCP routes for `db-smoke.apps.supadupa.brotechlabs.com` and `pooler-smoke.apps.supadupa.brotechlabs.com`.

7. Supadupa CLI type generation now wraps postgres-meta safely.
   - `supadupa-cli projects gen-types --ref <ref> --out database.types.ts` fetches the project CLI profile, performs an audited reveal of `db_password`, passes the resolved DB URL to Docker through environment variables, and does not place the secret-bearing DB URL in Docker arguments.
   - Live validation generated `/tmp/supadupa-compat/live-typegen/database.types.ts` for `smoke`.
   - The CLI profile and Admin UI Connect page now expose `supadupa-cli projects gen-types --ref <ref> --out database.types.ts` directly, with a typegen compatibility note explaining the upstream BYO-domain TLS caveat.
   - `supadupa-cli projects link --ref <ref> --dir <workspace>` now provides the Supadupa replacement for hosted-cloud `supabase link` by writing `.supadupa/project.json`, `.supadupa/config.toml`, and handle-only `.supadupa/supabase.env`.
   - `supadupa-cli projects env --ref <ref> --reveal-secrets` can materialize real SDK/CLI env for trusted development and test workflows after audited reveals; normal profile/link output keeps secret handles.
   - The Admin UI `/connect` payload is now remote-first: default DB and pooler snippets use `db-<ref>.<apps-domain>` and `pooler-<ref>.<apps-domain>` with `sslmode=require`, while Docker-network values remain under explicit internal keys.
   - `scripts/compat/02-cli-profile.sh` now asserts `storage_s3_url` on `storage-<ref>.<apps-domain>`, validates remote-safe public Connect payload snippets and Postgres parts, and sweeps public profile/link outputs for localhost or `.internal` leakage while preserving explicitly local/internal diagnostic fields.

8. Route reconciliation now runs on startup and service updates.
   - Control-plane startup rewrites route files for every persisted project from the current store state.
   - Service updates also refresh route files, so existing projects pick up new HTTP/TCP route rendering without requiring a separate network config edit.
   - Replica create/promote/failover/delete now also re-render route files so `db-replica-<replica>-<ref>.<apps-domain>` TCP routes stay aligned with replica desired state.
   - Replica creation now validates the generated public hostname before writing metadata, rejecting name/ref/domain combinations where `db-replica-<replica>-<ref>` would exceed the 63-character DNS label limit or the full replica FQDN would exceed the 253-character DNS name limit.
   - `scripts/compat/26-replicas-deep.sh` now provides guarded read-replica create/list/routing/route-manifest/delete coverage for disposable projects, including public read URI shape, internal read URI isolation, failover-candidate metadata, Traefik upstream alias checks, and route removal after delete.
   - Live disposable runs `compat-rpl-021702` and `compat-rpl-ha-022956` passed the read-replica phase. Compose now reloads primary replication config through `supabase_admin`, waits up to `SUPADUPA_REPLICA_READY_TIMEOUT_SECONDS` for cold standby boot, includes `replicas/compose.yaml` during project destroy, and prunes project-labeled Docker volumes so overlay-defined replica containers and volumes are removed.
   - `scripts/compat/29-branches-deep.sh` now provides guarded preview branch create/list/connect/route-manifest/public Auth health/delete coverage. It proves data-less branch defaults, branch-specific remote API/Studio/Storage S3/direct Postgres/pooler routes, no public profile localhost or `.internal` leakage, temporary feature override restoration, and metadata cleanup.

9. Upgrade now has safety guardrails.
   - `/v1/projects/<ref>/upgrade` rejects targets outside `SUPADUPA_SUPPORTED_STACK_VERSIONS` or the default stable set.
   - Project creation and platform defaults also reject unsupported stack versions before provisioning, so a user cannot accidentally start an unsupported image pull.
   - Stable versions resolve through stack release manifests that map every Compose image tag, not only the Postgres image.
   - `SUPADUPA_STACK_RELEASES_JSON` can extend or override release manifests as a JSON object keyed by version or as a list of manifest objects.
   - Configured supported versions without a built-in or explicit complete manifest are not exposed as upgrade targets, preventing unsafe Postgres-only or mixed-service pseudo releases. Partial overrides are allowed only for built-in releases.
   - `/v1/stack-releases` exposes the currently supported manifests to the admin UI, and the project lifecycle panel uses those backend-provided versions instead of a hardcoded target.
   - The Create Project wizard and Settings > Defaults use the same release catalog instead of free-text stack version entry.
   - Upgrade creates or uses a completed logical backup before calling the provisioner.
   - Caller-supplied backup IDs must be verified, readable, size-matched, and checksum-matched.
   - Production operators can set `SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true`; with that guard enabled, the API rejects local-only pre-upgrade backups and backups on untested/non-durable S3-compatible targets before the provisioner is called.
   - If provisioner upgrade fails, the API attempts to roll the stack back to the previous version and returns structured recovery metadata: error, previous/target versions, backup metadata, rollback availability, rollback attempt state, and rollback error if rollback fails.
   - Failed-upgrade responses now keep `rollback_attempted=true` even when the rollback command fails, and return `rollback_error` separately so automation and operators can distinguish "not attempted" from "attempted but failed."
   - `SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE=true` makes a failed upgrade with a successful stack-version rollback also run logical restore from the pre-upgrade backup and report `restore_attempted`, `restore_state`, and `restore_error`. This remains opt-in because it is destructive, and only `restore_state=completed` counts as successful auto-recovery; dry-run restore state is surfaced as `restore_error`.
   - The compatibility runner can inject a failed upgrade only when both `SUPADUPA_COMPAT_UPGRADE_FAILURE_TARGETS` is set on the server and the request includes `X-Supadupa-Compat-Inject-Upgrade-Failure: true`.
   - Compose upgrade now preserves existing service toggles instead of defaulting disabled services back on.
   - Compose upgrade applies a targeted runtime DB bootstrap for repeatable compatibility objects such as `supabase_realtime` instead of replaying the full first-boot init SQL against existing databases.
   - The admin lifecycle panel consumes the backup-aware response, shows the pre-upgrade backup ID, rollback readiness, backup completion time, local-vs-remote artifact state, explains the durable-backup production guard, and refreshes backup/project/recoverability state.

10. Disposable compatibility create and upgrade validation now runs end to end.
   - The runner can create a disposable project, seed a PostgREST-visible DB fixture, run a Supabase JS SDK client probe, deploy an Edge Function fixture, run data-plane checks, upgrade through configured stable targets, rerun checks, and destroy the project.
   - The CLI now defaults to a 10-minute HTTP timeout, configurable with `SUPADUPA_CLI_HTTP_TIMEOUT`, so long create/upgrade calls do not cancel server-side Docker work during image pulls.

11. Control API CORS now derives the deployed admin origin.
   - When explicit `SUPADUPA_CORS_ORIGINS` is absent, the API includes `https://$SUPADUPA_ADMIN_HOST` or `SUPADUPA_ADMIN_URL` in the default allowed origins alongside local dev origins.
   - Platform SCIM is no longer tied only to browser/admin auth: `/v1/settings/sso` stores `scim_enabled` plus a hash of the write-only `scim_token`, SCIM responses only expose `scim_token_configured`, SCIM routes accept either a platform admin session or the configured SCIM bearer token, and project anon/service keys are explicitly rejected from the SCIM surface.
   - Live check returned `Access-Control-Allow-Origin: https://admin.supadupa.brotechlabs.com`.

12. Applied Compose backup defaults now run against the live data plane, and restore defaults are command-validated.
   - With `SUPADUPA_COMPOSE_APPLY=true`, unset `SUPADUPA_LOGICAL_BACKUP_COMMAND` now defaults to `docker compose ... pg_dump --clean --if-exists --quote-all-identifiers --schema=public --schema=auth --schema=storage --schema=supabase_migrations`.
   - The Compose logical default intentionally avoids Supabase internal operational schemas such as Realtime, GraphQL, extensions, and analytics because plain logical restore of those internals can conflict with live stack-owned objects; full-cluster recovery remains a separate PITR/physical backup path.
   - Unset `SUPADUPA_LOGICAL_RESTORE_COMMAND` defaults to reading the project DB password from the project `.env` and running `docker compose ... psql -v ON_ERROR_STOP=1 -U supabase_admin < {{backup_path}}`, because Supabase internal event triggers are owned by `supabase_admin`.
   - `SUPADUPA_COMPOSE_BACKUP_DEFAULTS=false`, `0`, or `off` keeps the previous dry-run behavior unless explicit commands are configured.
   - Unit coverage verifies both the real Compose default command selection, remote artifact rehydration, checksum rejection, and the opt-out dry-run path.
   - `scripts/compat/12-backup-restore.sh` validates live restore only against a compat-created project unless `SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE=true`; it also confirms restore is `completed` before applying post-backup mutations, requires backup run timestamps/checksum/size/verification metadata, verifies the backup remains listed, and requires a reported restore path.
   - Live `smoke` backup trigger created `runtime/backups/smoke/20260606T060406Z-logical.sql` with a real PostgreSQL dump header.

13. Control-plane restore automation is available for platform backups.
   - Platform backups can be restored with `POST /v1/platform/backups/<id>/restore` or from Settings > Backups.
   - Restore is guarded by the exact `restore-control-plane` confirmation.
   - Restore verifies the artifact size and checksum before atomically importing the encrypted checkpoint and normalized metadata.
   - If the local artifact is missing, restore can rehydrate it from the recorded S3-compatible backup target before validation.
   - The source platform backup record is preserved in the restored checkpoint so the recovery artifact remains visible after restore and process restart.
   - After metadata import, the API asks the provisioner to reconcile restored projects and stop projects removed by the checkpoint while retaining volumes; `restore_state` reports `reconciled` or `metadata-restored-runtime-errors`.

13a. S3-compatible backup targets can be validated before use.
   - `SUPADUPA_BACKUP_TARGET_*` and `SUPADUPA_BACKUP_S3_*` startup env vars can bootstrap the named target, mark it default, update it idempotently, and avoid browser entry for secret material during first install.
   - `SUPADUPA_BACKUP_TARGET_AUTO_TEST=true` or `SUPADUPA_BACKUP_S3_AUTO_TEST=true` makes startup immediately run the server-side S3 target probe, record `last_test_*`, and audit `backup_storage_target.bootstrap_env_test`, so first-install backup target readiness can be proven without a manual UI click.
   - `supadupa-cli backup-targets list|create|update|test|delete` exposes the same automation path, and `backups set-policy --storage-target-id` can bind projects to a target from CLI.
   - Terraform now exposes `supadupa_backup_storage_target` for S3-compatible targets and `supadupa_project_backup_policy.storage_target_id` for binding logical or physical project backup policies to that target. The target resource keeps `secret_access_key` sensitive/write-only and exposes recovery-readiness fields for IaC assertions.
   - `POST /v1/backup-storage-targets/<id>/test` uses the stored target configuration server-side, writes a small object under `<prefix>/_supadupa-checks/...`, reads it back, deletes it best-effort, records `last_tested_at`, `last_test_status`, and `last_test_error`, and returns a redacted target.
   - Target responses compute `durable_off_host`, `recovery_ready`, `readiness_status`, and `readiness_message` from the same gates used by recoverability. Settings > Backups exposes the same target test action and shows recovery readiness next to each target, so a local target can pass the S3 probe without appearing hosted-grade.
   - `GET /v1/runtime-config` returns redacted server-side operational booleans, including recovery and upgrade guard state, so CI can distinguish runner flags from API process configuration without exposing command strings or credentials.
   - Fleet Advisor and Compliance now consume the same recovery posture, adding high-level platform findings/controls when hosted-grade guards or recovery-ready off-host targets are missing.
   - Production operators can set `SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true`; with that guard enabled, physical backup uploads and WAL archive uploads require a selected/default backup target, and that target must be tested durable off-host. Default PITR archive-bucket derivation also requires a tested durable off-host target instead of accepting a missing, untested, or loopback S3-compatible endpoint.
   - This closes the operational gap where a platform admin could save invalid off-host backup credentials and only discover the failure during a scheduled or pre-upgrade backup.
   - Current no-credential compat fallback uses a small disposable Node HTTP S3-compatible shim to prove target probe behavior, project logical backup upload on disposable compat projects, backup target readiness shape, and loopback false-positive protection.
   - `20-rustfs-backup-target.sh` adds opt-in RustFS coverage: it pulls/runs `rustfs/rustfs`, creates a bucket with SigV4, creates, tests, and lists a backup target through the Management API, confirms loopback RustFS returns `durable_off_host=false`, `recovery_ready=false`, and `readiness_status=local-or-loopback`, and deletes the temporary target/container on exit. Project logical backup, physical backup, and WAL upload through RustFS are guarded to compat-created disposable projects and now assert restore-critical artifact metadata. Control-plane backup upload through RustFS is available only when the RustFS target is intentionally kept so the platform backup record remains restorable.
   - Persistent user-configured targets should appear in Settings > Backups. Temporary compat-created RustFS/Node targets are expected to appear only while their phase is running and are removed during cleanup.

14. Supavisor and Storage runtime reconciliation now backfills hosted-style defaults for existing projects.
   - Existing project `.env` files now receive `REQUEST_ALLOW_X_FORWARDED_PATH=true`, `GLOBAL_S3_BUCKET=<ref>`, and S3 protocol keys during service/config/secret/hook sync without rotating existing DB/JWT secrets.
   - Supavisor tenant render now uses direct tenant-user-record authentication for `postgres.<ref>` and replaces stale tenant metadata on pooler startup.
   - Startup runtime reconciliation now replays service toggles, explicit runtime configs, Auth Hooks, and replicas through the provisioner in addition to secrets/routes, so existing project artifacts are repaired from the current generator after a control-plane restart instead of requiring ad hoc per-project fixes.
   - Imgproxy now receives the project `storage-data` volume read-only plus `IMGPROXY_LOCAL_FILESYSTEM_ROOT=/`, allowing Storage's local backend image transformation URLs to resolve through the hosted-style public render endpoint.
   - Live `smoke` transaction and session pooler URLs pass through Traefik and Supavisor.
   - Live `smoke` Storage bucket create, object upload, and object download pass through the public API host.
   - Live `smoke` `scripts/compat/23-storage-deep.sh` passed image transformation validation: `/storage/v1/render/image/public/<bucket>/image.png?width=4&height=4&resize=fill&quality=80` returned a PNG resized to 4x4.

15. Project TLS validation is automated.
   - `scripts/compat/15-tls.sh` verifies control API/admin HTTPS, project API HTTPS, project Studio HTTPS, HTTP-to-HTTPS redirects, direct Postgres STARTTLS, and transaction/session pooler STARTTLS.
   - Validation found and fixed a Studio redirect ordering bug: the HTTP Studio route was applying Supadupa SSO before HTTPS redirect, returning 401 on plain HTTP. Redirect routers now apply only the HTTPS redirect middleware; HTTPS Studio keeps the SSO forward-auth middleware.
   - Connect payload Studio deep links now use `/project/<ref>/api` and `/project/<ref>/api?panel=graphql` instead of the self-hosted `default` alias. `scripts/compat/17-studio-auth.sh` uses the same scoped Studio session-token flow as the admin UI, verifies unauthenticated Studio is rejected, and proves the project-ref REST docs and GraphQL explorer links load remotely.
   - API and live custom-domain validation now cover generated Storage S3, branch, and read-replica hosts in addition to API, Studio, DB, and pooler hosts, so one project cannot claim another project's generated remote surface as a custom domain.
   - Connect payloads and CLI profiles now include `custom_domains` metadata plus `custom_api_urls` for domains whose cert status is `issued` or `uploaded`. Generated `api_url` remains `https://<ref>.<apps-domain>`; custom domains are exposed as alternate ready Supabase API URLs in the profile env/TOML, snippets, MCP-backed connect payload, Terraform `supadupa_project_connect` data source, Terraform custom-domain resource attributes, and Admin UI Connect page.
   - `supadupa-cli projects cli-profile --format env --api-domain <fqdn>`, `projects env --prefer-custom-domain`, and `projects link --api-domain <fqdn>` can intentionally emit a ready custom domain as `SUPABASE_URL` while recording `SUPADUPA_SELECTED_API_URL` / `selected_api_url`; `scripts/compat/21-custom-domains.sh` now validates those real CLI paths.

16. Enabled PITR policies now archive WAL automatically.
   - The control-plane backup scheduler calls `RunDueWALArchives` on every scheduler pass.
   - Healthy and degraded projects with PITR enabled archive immediately when no previous archive exists, then on `SUPADUPA_WAL_ARCHIVE_INTERVAL` cadence, defaulting to 5 minutes.
   - In applied Compose mode, unset `SUPADUPA_WAL_ARCHIVE_COMMAND` now defaults to a real WAL-switch command that records the Postgres WAL filename, writes the completed project WAL segment from the `db` container, and stores `segment_source=postgres`. Outside applied Compose mode, WAL archive requests require an explicit command unless `SUPADUPA_WAL_ARCHIVE_DRY_RUN=true` is deliberately set for tests.
   - This prevents fake dry-run WAL artifacts from satisfying off-host PITR readiness when a durable target is configured.
   - With `SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true`, enabling PITR without an explicit archive bucket derives the WAL bucket only from a selected/default recovery-ready target. Untested and loopback targets are skipped and the request keeps returning the archive-bucket requirement.
   - Scheduled WAL archive successes and failures are recorded in project logs and the immutable audit chain.
   - Manual WAL archive actions still use the same archive service and artifact validation path.

17. Project recoverability posture is explicit.
   - `GET /v1/projects/<ref>/recoverability` reports backup policy state, off-host target configuration, latest backup, latest verified backup, PITR state, latest WAL archive, WAL segment source, off-host WAL verification, recovery window, physical backup availability, PITR restore command configuration, and restore-to-time availability.
   - The Admin UI Backups page surfaces this status so local-only backups are not presented as hosted-grade recoverability.
   - Restore-to-time remains false until a verified off-host physical base backup, verified off-host WAL archive, and configured `SUPADUPA_PITR_RESTORE_COMMAND` exist.
   - WAL archives now record `remote_location`, `storage_target_id`, and `segment_source` when an S3-compatible backup target is selected or configured as platform default; local-only, dry-run, and legacy-streamed WAL history is visible but does not expose a hosted-grade recovery window.

18. Restore-to-time uses the hosted Supabase Management API shape.
   - `GET /v1/projects/<ref>/database/backups` lists project backups through the hosted-shaped backup route while the existing Supadupa `/backups` route remains available for first-party clients.
   - `POST /v1/projects/<ref>/database/backups/restore-pitr` accepts `{"recovery_time_target_unix":"<unix-seconds>","confirmation":"restore pitr project <ref>"}`.
   - The API returns a recoverability-rich 409 when PITR, physical base backups, WAL archives, a restore command, or the requested recovery window are missing.
   - When all gates are satisfied, the control plane rehydrates the selected physical backup plus the verified off-host WAL archive range from that base backup through the requested recovery target from the recorded S3-compatible target, validates readability, size, and checksum for each artifact, then runs `SUPADUPA_PITR_RESTORE_COMMAND`, returns HTTP 201, records a restore transcript, logs the action, and appends an immutable `project.restore_pitr` audit event.
   - `16-recoverability-pitr.sh` now treats a 201 response as insufficient for hosted-grade proof: the destructive phase inserts a pre-target SQL marker, captures the recovery timestamp, inserts a post-target marker, archives WAL, restores to the captured timestamp, and asserts that the pre-target marker remains while the post-target marker is gone.
   - In applied Compose mode, unset `SUPADUPA_PITR_RESTORE_COMMAND` now defaults to a destructive restore command that stops the project stack with the base `compose.yaml` plus `replicas/compose.yaml` when present, restores the `db-data` volume from the verified physical backup, copies the selected WAL range into `pg_wal`, writes recovery target settings, restarts the same compose-file set, and records a transcript. Unit coverage validates the volume restore mechanics, replica-overlay compose-file arguments, and tar-shaped physical backup artifact with a fake Docker binary.
   - PITR restore command templates receive both backwards-compatible latest-WAL placeholders (`{{wal_path}}`, `{{wal_segment}}`, `{{wal_archive_id}}`), compose-file placeholders (`{{compose_file_args}}`, `{{compose_files}}`), and range placeholders (`{{wal_path_args}}`, `{{wal_paths}}`, `{{wal_segments}}`, `{{wal_archive_ids}}`) so real replay tooling can consume every validated WAL artifact in the selected range and stop/start every Compose overlay needed for the project.
   - Restore-to-time now constrains the recovery window to a verified off-host physical base backup at or before the latest off-host WAL archive, and target restores select the newest verified off-host physical backup at or before `recovery_time_target_unix` instead of accidentally using a future base backup.
   - This aligns the Management API contract with hosted Supabase while still leaving live off-host physical backup configuration and a durable-target destructive WAL replay run to the remaining PITR work.

19. Physical backup policies are first-class.
   - `PUT /v1/projects/<ref>/backups/policy` now accepts `kind=physical` in addition to `logical`.
   - Manual and scheduled project backup triggers dispatch by policy kind.
   - In applied Compose mode, physical backups default to running `pg_basebackup` inside the project `db` container and packaging the generated base-backup files as a verified physical artifact.
   - Outside applied Compose mode, physical backups require `SUPADUPA_PHYSICAL_BACKUP_COMMAND`; no dry-run physical artifacts are created because fake physical backups would incorrectly satisfy PITR base-backup checks.
   - Verified physical artifacts upload through the same S3-compatible target/default-target path and are the base artifacts recoverability uses for restore-to-time readiness.

## Detailed Evidence

### HTTP and Gateway Auth

```txt
https://compat-035843.apps.supadupa.brotechlabs.com/auth/v1/health -> 200
GET /rest/v1/cli_probe without key -> 401 No API key found in request
GET /rest/v1/cli_probe with anon apikey -> 200 [{"id":1,"label":"from supabase cli"}]
POST /graphql/v1 without key -> 401 No API key found in request
POST /graphql/v1 with anon apikey -> 200 cli_probeCollection result
GET /auth/v1/admin/users with service apikey -> 200
GET /storage/v1/bucket with service apikey -> 200
GET /functions/v1/hello -> 200 {"ok":true,"path":"/hello"}
```

### Realtime

```txt
valid anon key websocket -> HTTP/1.1 101 Switching Protocols
invalid key websocket -> HTTP/1.1 403 Forbidden
missing key websocket -> HTTP/1.1 403 Forbidden
supabase-js broadcast -> realtime broadcast ok true compat-035843
private database broadcast -> compat-db-broadcast delivered on authenticated channel
broadcast replay -> replayed=true for prior database-originated broadcast
```

### Database and CLI

```txt
psql public direct -> postgres|postgres
psql public pooler transaction -> postgres|postgres
supabase db push --db-url <public-direct-url> --yes -> passed
supabase migration list --db-url <public-direct-url> -> passed
supabase db pull --db-url <public-direct-url> --schema public,compat --yes -> passed
supabase db diff --db-url <public-direct-url> --schema public,compat --use-pg-schema -> passed
supabase migration repair <synthetic-version> --status applied/reverted --db-url <public-direct-url> -> passed
supabase CLI classification -> version 2.105.0, db_access_mode=public-tcp, management commands=supadupa-wrapper
postgres-meta direct typegen -> 4698 lines
smoke psql public direct -> postgres|postgres|5432
supadupa-cli projects gen-types --ref smoke --out /tmp/supadupa-compat/live-typegen/database.types.ts -> 4643 lines
```

The official CLI typegen failure is isolated to CLI/postgres-meta CA handling:

```txt
supabase gen types typescript --db-url <public-direct-url>
error: unable to get local issuer certificate
```

Validated facts:

- OpenSSL verifies the public Postgres STARTTLS chain.
- `psql` works with `sslmode=require`.
- Direct `postgres-meta` works with the original DB URL.
- Supabase CLI `2.105.0` still fails for `gen types --db-url` against the public DB host with `unable to get local issuer certificate`.
- Supabase CLI `2.105.0` succeeds for `gen types --db-url` through `supadupa-cli projects db-tunnel`, producing 315 lines in the live `smoke` probe.

Supported Supadupa typegen commands:

```bash
supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" \
  projects gen-types --ref "$PROJECT_REF" --out database.types.ts

supadupa-cli --api "$SUPADUPA_API_URL" --token "$SUPADUPA_TOKEN" \
  projects db-tunnel --ref "$PROJECT_REF" --listen 127.0.0.1:15432

supabase gen types typescript \
  --db-url "postgres://postgres:${DB_PASSWORD}@127.0.0.1:15432/postgres?sslmode=disable"
```

Underlying postgres-meta form, useful for diagnosing upstream CLI behavior:

```bash
docker run --rm --network host \
  -e PG_META_GENERATE_TYPES=typescript \
  -e PG_META_DB_URL="$DB_URL" \
  public.ecr.aws/supabase/postgres-meta:v0.96.6 \
  node dist/server/server.js > database.types.ts
```

### Automated Runner

```txt
SUPADUPA_API_URL=https://api.supadupa.brotechlabs.com \
SUPADUPA_TEST_EMAIL=admin@example.test \
SUPADUPA_TEST_REF=smoke \
SUPADUPA_ISOLATION_REF=compat-035843 \
scripts/compat/run.sh
```

Result:

```txt
PASS preflight.go_test
PASS preflight.frontend_build
PASS preflight.api_health
PASS project.inspect
PASS project.cli_profile
PASS project.api_url - https://smoke.apps.supadupa.brotechlabs.com
PASS project.public_database_url - db-smoke.apps.supadupa.brotechlabs.com
PASS cli_profile.json - profile saved
PASS cli_profile.json_shape - required fields present
PASS cli_profile.typegen_command - supadupa gen-types command exposed in profile
PASS connect_payload.json - connect payload saved
PASS connect_payload.remote_safe - public snippets and parts are remote-safe
PASS isolation.second_project - compat-035843
PASS isolation.remote_connect_surfaces - smoke and compat-035843 have distinct public endpoints
PASS isolation.route_files_separate - smoke and compat-035843 route files are host/service separated
PASS isolation.runtime_networks_separate - smoke and compat-035843 containers use separate internal networks
PASS isolation.realtime_primary_key_primary_project - websocket opened
PASS isolation.realtime_secondary_key_secondary_project - websocket opened
PASS isolation.realtime_secondary_key_primary_project_rejected - HTTP 401
PASS isolation.realtime_primary_key_secondary_project_rejected - HTTP 401
PASS isolation.function_deploy_primary - smoke/compat-isolation-20260606213120-3624384
PASS isolation.function_deploy_secondary - compat-035843/compat-isolation-20260606213120-3624384
PASS isolation.function_primary_key_primary_project - HTTP 200
PASS isolation.function_secondary_key_secondary_project - HTTP 200
PASS isolation.function_secondary_key_primary_project_rejected - HTTP 401
PASS isolation.function_primary_key_secondary_project_rejected - HTTP 401
PASS isolation.function_same_name_project_scoped - compat-isolation-20260606213120-3624384 resolves separately in both projects
PASS isolation.anon_key_cross_project_rejected - HTTP 401
PASS isolation.db_password_cross_project_rejected - psql rejected cross-project password
PASS isolation.s3_credentials_cross_project_rejected - primary S3 credentials rejected by compat-035843 endpoint
PASS functions_deep.region_create - us-east-1 nearest
PASS functions_deep.region_list - configured region listed
PASS functions_deep.region_invoke_header - SB_REGION and x-sb-edge-region=us-east-1
PASS functions_deep.region_invoke_query - SB_REGION and x-sb-edge-region=?forceFunctionRegion=us-east-1
PASS functions_deep.restart_runtime - smoke-edge-runtime-1
PASS functions_deep.redeploy_after_runtime_restart - HTTP 200 after attempt 1
PASS functions_deep.restart_persistence - same-name redeploy updated runtime source/env/auth
PASS functions_deep.import_map_config - inline import map applied
PASS functions_deep.import_map_runtime - HTTP 200 after attempt 1
PASS functions_deep.import_map_resolution - alias resolved through project import map
PASS functions_deep.storage_mount_runtime_read - HTTP 200 after attempt 1
PASS functions_deep.storage_mount_scale - HTTP 200 after attempt 1
PASS functions_deep.storage_mount_read_only - HTTP 200 after attempt 1
PASS functions_deep.storage_mount_origin_unchanged - HTTP 200
PASS realtime_deep.fixture - table compat_realtime_rtauth20260607033607 published
PASS realtime_deep.presence_postgres_changes_db_broadcast - presence, Postgres Changes, private DB broadcast, replay, and same-project private-channel isolation delivered
PASS database_desired.extensions_live - core extensions installed
PASS database_desired.cron_live - compatds202606070342331282567 scheduled
PASS database_desired.queue_live - compatds202606070342331282567 and compatds202606070342331282567dlq created
PASS database_desired.webhook_live - compatds202606070342331282567 triggers created
PASS database_desired.schema_live - compatds_schema_202606070342331282567 applied
PASS database_desired.role_live - compatds_202606070342331282567 created and granted
PASS database_desired.webhook_delete_live - compatds202606070342331282567 removed
PASS database_desired.role_delete_live - compatds_202606070342331282567 removed
PASS database_desired.queue_delete_live - compatds202606070342331282567 removed
PASS database_desired.cron_delete_live - compatds202606070342331282567 removed
PASS database_desired.schema_metadata_delete - compatds202606070342331282567/v202606070342331282567 removed
PASS database_desired.cleanup - temporary SQL objects removed
PASS provider_configs.feature_enable - temporarily enabled log_drains and network_restrictions
PASS provider_configs.auth_client_raw_secret_rejected - raw confidential client secret rejected
PASS provider_configs.log_drain_create - id=eae101a82980d0c67fb77458b0a5cee7
PASS provider_configs.auth_client_create - compat-client-202606070402121355723 registered
PASS provider_configs.replication_raw_secret_rejected - raw sensitive replication config rejected
PASS provider_configs.replication_create - id=041e640b473799b7694ad8514e863eea
PASS provider_configs.embedding_create - id=ee1099094cb0a887620ee618db367b68
PASS provider_configs.vector_raw_secret_rejected - raw sensitive vector metadata rejected
PASS provider_configs.vector_create - compat-vector-202606070402121355723 configured
PASS provider_configs.network_raw_secret_rejected - raw sensitive network config rejected
PASS provider_configs.network_create - id=08494a5dade3f42acc72a7191c3c2922
PASS provider_configs.log_drain_list - created drain visible and masked
PASS provider_configs.auth_client_list - compat-client-202606070402121355723 visible and masked
PASS provider_configs.replication_list - created pipeline visible and masked
PASS provider_configs.embedding_list - created embedding job visible
PASS provider_configs.vector_list - created vector bucket visible and masked
PASS provider_configs.network_list - created network declaration visible and masked
PASS provider_configs.metrics - resource counters include configured provider declarations
PASS provider_configs.cleanup - provider-backed declarations removed
PASS tls.control_api.curl - HTTP 200
PASS tls.control_api.openssl - api.supadupa.brotechlabs.com:443
PASS tls.control_api.http_redirect - HTTP 308 -> https://api.supadupa.brotechlabs.com/v1/health
PASS tls.control_admin.curl - HTTP 200
PASS tls.control_admin.openssl - admin.supadupa.brotechlabs.com:443
PASS tls.control_admin.http_redirect - HTTP 308 -> https://admin.supadupa.brotechlabs.com/
PASS tls.project_api.curl - HTTP 200
PASS tls.project_api.openssl - smoke.apps.supadupa.brotechlabs.com:443
PASS tls.project_api.http_redirect - HTTP 308 -> https://smoke.apps.supadupa.brotechlabs.com/auth/v1/health
PASS tls.project_studio.curl - HTTP 401
PASS tls.project_studio.openssl - studio-smoke.apps.supadupa.brotechlabs.com:443
PASS tls.project_studio.http_redirect - HTTP 308 -> https://studio-smoke.apps.supadupa.brotechlabs.com/
PASS tls.postgres_direct.postgres_starttls - db-smoke.apps.supadupa.brotechlabs.com:5432 sslmode=require
PASS tls.pooler_transaction.postgres_starttls - pooler-smoke.apps.supadupa.brotechlabs.com:6543 sslmode=require
PASS tls.pooler_session.postgres_starttls - pooler-smoke.apps.supadupa.brotechlabs.com:5432 sslmode=require
PASS rest.no_key_rejected - HTTP 401
PASS rest.anon_key_accepted - HTTP 200
PASS postgres.public_connect - postgres|postgres|5432
PASS postgres.pooler_transaction.url - pooler-smoke.apps.supadupa.brotechlabs.com:6543
PASS postgres.pooler_transaction.connect - postgres|postgres|5432
PASS postgres.pooler_session.url - pooler-smoke.apps.supadupa.brotechlabs.com:5432
PASS postgres.pooler_session.connect - postgres|postgres|5432
PASS supabase_cli.db_push - migration applied
PASS supabase_cli.db_push_noop_rerun - migration history stable
PASS cli.gen_types.table.compat_cli_probe - table present in generated types
PASS cli.gen_types - generated database.types.ts (4753 lines)
PASS auth.health - HTTP 200
PASS auth.admin_no_key_rejected - HTTP 401
PASS auth.admin_service_role - HTTP 200
PASS cors.auth_token_preflight - HTTP 200
PASS cors.rest_preflight - HTTP 200
PASS auth.signup - HTTP 200
PASS auth.signup_password_grant_confirmation_gate - HTTP 400 email_not_confirmed
PASS auth.admin_create_confirmed_user - HTTP 200
PASS auth.password_grant - HTTP 200
PASS auth.user_session - HTTP 200
PASS graphql.no_key_rejected - HTTP 401
PASS graphql.anon_key_accepted - HTTP 200
PASS storage.service_role_bucket_list - HTTP 200
PASS storage.service_role_bucket_create - HTTP 200
PASS storage.service_role_object_upload - HTTP 200
PASS storage.service_role_object_download - HTTP 200
PASS storage.private_anon_rejected - HTTP 400 not_found
PASS storage.signed_url_create - HTTP 200
PASS storage.signed_url_download - HTTP 200
PASS storage.s3.bucket_create - HTTP 200 bucket=compat-s3-20260607041837-1403712
PASS storage.s3.client - list, put, head, metadata, range, presigned get, copy, delete
PASS storage_deep.cdn_policy_update - cache policy persisted
PASS storage_deep.cdn_routes - route cache headers reflected
PASS storage_deep.cdn_manual_invalidation - manual invalidation completed
PASS storage_deep.cdn_object_event - smart revalidation completed
PASS functions.hello - HTTP 200
PASS realtime.invalid_key_rejected - HTTP 401
PASS realtime.missing_key_rejected - HTTP 401
PASS realtime.anon_key_accepted - websocket opened
PASS realtime.broadcast - message delivered
PASS metrics.project.status - healthy
PASS metrics.project.resources - 1 vCPU, 2048 MB RAM, 20 GB disk, 3000 IOPS
PASS metrics.project.telemetry - cpu=6.97% memory=2710087663/32845762396
PASS metrics.fleet.projects - projects=2 healthy=2
PASS metrics.fleet.capacity - 16 vCPU, 31326 MB RAM, 600 GB disk, 48000 IOPS
PASS metrics.fleet.reservations - 2 vCPU, 4096 MB RAM, 40 GB disk, 6000 IOPS, 2 projects
PASS metrics.fleet.telemetry - sampled=2 stale=0 cpu=13.88% memory=5415026820/65691524792
PASS metrics.fleet.audit - audit_verified=true
PASS metrics.prometheus.supadupa_projects_total - present
PASS metrics.prometheus.supadupa_host_capacity_cpu - present
PASS metrics.prometheus.supadupa_host_used_cpu - present
PASS metrics.prometheus.supadupa_observed_projects - present
PASS metrics.prometheus.supadupa_audit_verified - present
PASS metrics.prometheus.supadupa_projects_total - present
PASS recoverability.get - status=local-backup-only restore_to_time_available=false
PASS recoverability.backup_targets - count=1
PASS recoverability.gates - status=local-backup-only targets=1
PASS recoverability.backup_target_test - target=8eacf182bd77065c2a885703e25cc0f7
PASS recoverability.hosted_backup_list - count=4
PASS recoverability.restore_pitr_unavailable - HTTP 409 with recoverability
SKIP recoverability.physical_backup - set SUPADUPA_COMPAT_PHYSICAL_BACKUP_VALIDATE=true to trigger a physical backup
PASS rustfs.container_start - rustfs/rustfs:latest
PASS rustfs.container_ready - http://127.0.0.1:32775
PASS rustfs.bucket_create - supadupa-compat-smoke-1386979
PASS rustfs.target_create - 8046349ed9f4862c269d061b51b7fb1b
PASS rustfs.target_list.after_create - 8046349ed9f4862c269d061b51b7fb1b
PASS rustfs.target_test - server-side S3 probe passed
PASS rustfs.target_list.after_test - 8046349ed9f4862c269d061b51b7fb1b
PASS rustfs.local_target_recoverability - loopback RustFS did not satisfy off-host gates
SKIP rustfs.project_backup_upload - only runs against a project created by this compat run
PASS rustfs.disposable.create_public_ready - https://compat-rustfs-175728.apps.supadupa.brotechlabs.com
PASS rustfs.disposable.target_test - server-side S3 probe passed
PASS rustfs.disposable.logical_policy - project policy uses RustFS target for logical backups
PASS rustfs.disposable.project_backup_upload - logical backup uploaded through RustFS
PASS rustfs.disposable.physical_policy - project policy uses RustFS target for physical backups
PASS rustfs.disposable.physical_backup_upload - physical backup uploaded through RustFS
PASS rustfs.disposable.pitr_feature - temporarily enabled pitr for disposable project
PASS rustfs.disposable.pitr_policy - WAL bucket derived from RustFS target
PASS rustfs.disposable.wal_archive_upload - real 16 MiB Compose WAL segment uploaded through RustFS with no dry-run markers
PASS rustfs.disposable.final_recoverability - loopback artifacts still do not satisfy off-host gates
PASS rustfs.disposable.cleanup - compat-rustfs-175728 destroyed
LIVE target dev-rustfs-local - durable_off_host=false recovery_ready=false readiness_status=local-or-loopback last_test_status=passed
PASS isolation.second_project - compat-035843
PASS isolation.realtime_primary_key_primary_project - websocket opened
PASS isolation.realtime_secondary_key_secondary_project - websocket opened
PASS isolation.realtime_secondary_key_primary_project_rejected - HTTP 401
PASS isolation.realtime_primary_key_secondary_project_rejected - HTTP 401
PASS isolation.function_primary_key_primary_project - HTTP 200
PASS isolation.function_secondary_key_secondary_project - HTTP 200
PASS isolation.function_secondary_key_primary_project_rejected - HTTP 401
PASS isolation.function_primary_key_secondary_project_rejected - HTTP 401
PASS isolation.function_same_name_project_scoped - compat-isolation-20260606213120-3624384 resolves separately in both projects
PASS isolation.anon_key_cross_project_rejected - HTTP 401
PASS isolation.db_password_cross_project_rejected - psql rejected cross-project password
PASS isolation.s3_credentials_cross_project_rejected - primary S3 credentials rejected by compat-035843 endpoint
PASS cli_profile.json_shape - required fields present
PASS cli_profile.env_handles - handle-only env
PASS cli_profile.env_sourceable - SUPABASE_URL and SUPABASE_DB_URL loaded
PASS cli_profile.toml_shape - project metadata present
PASS cli_profile.link_handles - workspace binding is handle-only
PASS cli_profile.env_reveal_materialized - secrets and DB URLs materialized
PASS security.profile_secret_handles - CLI profile uses secret handles and DB password placeholders
PASS security.admin_token_project_api_rejected - HTTP 401
PASS security.service_role_management_api_rejected - HTTP 401
PASS security.anon_management_api_rejected - HTTP 401
PASS security.scim_requires_bearer_or_admin - HTTP 401
PASS security.service_role_scim_rejected - HTTP 401
SKIP security.scim_bearer_token - SUPADUPA_COMPAT_SCIM_TOKEN not configured
PASS security.secret_reveal_requires_auth - HTTP 401
PASS security.studio_requires_supadupa_auth - HTTP 401
PASS studio_auth.login_cookie - HTTP 200
PASS studio_auth.noauth_rejected - HTTP 401
PASS studio_auth.authenticated_load - HTTP 200
PASS studio_auth.no_localhost_links - Studio HTML has no obvious local/internal host references
PASS stack_releases.list - count=4
PASS stack_releases.minimum_catalog - built-in catalog covers a few stable releases
PASS stack_releases.project - stack_version=15.8.1.085
PASS stack_releases.unsupported_upgrade_rejected - HTTP 400
PASS stack_releases.unsupported_create_rejected - HTTP 400
PASS security.revealed_secrets_not_in_logs - project logs do not contain revealed secret values
PASS public_exposure.project_containers_not_published - smoke containers have no host port mappings
PASS public_exposure.only_edge_public_ports - supadupa-edge-router-1:443, supadupa-edge-router-1:5432, supadupa-edge-router-1:6543, supadupa-edge-router-1:80
PASS public_exposure.loopback_only_support_ports - supadupa-edge-router-1:8088, supadupa-meta-db-1:15432
PASS runner.complete - artifacts: /tmp/supadupa-compat/smoke
```

### Startup Routes and Upgrade Guardrails

```txt
startup route reconciliation:
project_ref=smoke routes=2 path=/root/supadupa/runtime/routes/smoke.yaml
project_ref=compat-035843 routes=2 path=/root/supadupa/runtime/routes/compat-035843.yaml

live unsupported upgrade preflight:
POST /v1/projects/smoke/upgrade {"version":"nightly"} -> 400
unsupported stack version "nightly"; supported stable versions: 15.8.1.085, 15.8.1.060
current release catalog:
15.8.1.085
15.8.1.060
15.8.1.054
15.8.1.049
```

### Disposable Stable Upgrade Matrix

```txt
ref=compat-up-051418
source stack_version=15.8.1.060
target stack_version=15.8.1.085
backup_id=b1eb39a3ca0d5c592c316bc4d2f697ba
backup artifact=runtime/backups/compat-up-051418/20260606T051532Z-logical.sql
backup size=399628 bytes
```

Result:

```txt
PASS project.create
PASS project.create_public_ready - https://compat-up-051418.apps.supadupa.brotechlabs.com
PASS rest.no_key_rejected - HTTP 401
PASS rest.anon_key_accepted - HTTP 200
PASS postgres.public_connect - postgres|postgres|5432
PASS fixture.db.seed
PASS fixture.rest.seeded_row - HTTP 200
PASS cli.gen_types - generated database.types.ts (4661 lines)
PASS sdk.js.select - rows=1
PASS sdk.js.auth - signInWithPassword and getUser passed
PASS fixture.function.deploy - hello
PASS auth.health - HTTP 200
PASS graphql.anon_key_accepted - HTTP 200
PASS storage.service_role_bucket_list - HTTP 200
PASS functions.hello - HTTP 200
PASS realtime.anon_key_accepted - websocket opened
PASS upgrade_matrix.backup.15.8.1.085
PASS upgrade_matrix.upgrade.15.8.1.085
PASS upgrade_matrix.project.after-15.8.1.085 - stack_version=15.8.1.085
PASS upgrade_matrix.verify.after-15.8.1.085
PASS project.cleanup - compat-up-051418 destroyed
```

No `compat-up-051418` containers or project directory remained after cleanup.

Additional older-stable matrix:

```txt
ref=compat-up-054-154237
source stack_version=15.8.1.054
target stack_version=15.8.1.085
backup_id=3fadcc828c15cf08d6bf238cc566b3b7
```

Result:

```txt
PASS project.create_public_ready - https://compat-up-054-154237.apps.supadupa.brotechlabs.com
PASS tls.postgres_direct.postgres_starttls - db-compat-up-054-154237.apps.supadupa.brotechlabs.com:5432 sslmode=require
PASS tls.pooler_transaction.postgres_starttls - pooler-compat-up-054-154237.apps.supadupa.brotechlabs.com:6543 sslmode=require
PASS tls.pooler_session.postgres_starttls - pooler-compat-up-054-154237.apps.supadupa.brotechlabs.com:5432 sslmode=require
PASS supabase_cli.db_push
PASS supabase_cli.db_pull
PASS cli.gen_types - generated database.types.ts (5657 lines)
PASS functions.hello - HTTP 200
PASS storage.s3.client - list, put, get, list objects, delete
PASS realtime.broadcast - message delivered
PASS sdk.js.auth - signInWithPassword and getUser passed
PASS upgrade_matrix.backup.15.8.1.085 - backup_id=3fadcc828c15cf08d6bf238cc566b3b7
PASS upgrade_matrix.upgrade.15.8.1.085 - target=15.8.1.085
PASS upgrade_matrix.project.after-15.8.1.085 - stack_version=15.8.1.085
PASS upgrade_matrix.verify.after-15.8.1.085
PASS project.cleanup - compat-up-054-154237 destroyed
```

Oldest built-in stable matrix:

```txt
ref=compat-049-172146
source stack_version=15.8.1.049
target stack_version=15.8.1.085
backup_id=ce0cfe0a11891946a33e1d45962188ff
artifacts=/tmp/supadupa-compat/compat-049-172146
```

Result:

```txt
PASS project.create_public_ready - https://compat-049-172146.apps.supadupa.brotechlabs.com
PASS tls.postgres_direct.postgres_starttls - db-compat-049-172146.apps.supadupa.brotechlabs.com:5432 sslmode=require
PASS tls.pooler_transaction.postgres_starttls - pooler-compat-049-172146.apps.supadupa.brotechlabs.com:6543 sslmode=require
PASS tls.pooler_session.postgres_starttls - pooler-compat-049-172146.apps.supadupa.brotechlabs.com:5432 sslmode=require
PASS supabase_cli.db_push - migration applied
PASS supabase_cli.db_pull - remote schema pulled
PASS cli.gen_types - generated database.types.ts (5657 lines)
PASS storage.s3.client - list, put, get, list objects, delete
PASS realtime.broadcast - message delivered
PASS sdk.js.auth - signInWithPassword and getUser passed
PASS upgrade_matrix.backup.15.8.1.085 - backup_id=ce0cfe0a11891946a33e1d45962188ff
PASS upgrade_matrix.upgrade.15.8.1.085 - target=15.8.1.085 backup_id=ce0cfe0a11891946a33e1d45962188ff
PASS upgrade_matrix.project.after-15.8.1.085 - stack_version=15.8.1.085
PASS upgrade_matrix.verify.after-15.8.1.085
PASS project.cleanup - compat-049-172146 destroyed
```

The first `15.8.1.049` run exposed that the default pooler readiness timeout was too short for a cold oldest-stable project: direct Postgres passed quickly, but transaction pooler STARTTLS needed more than 60 seconds. `scripts/compat/03-postgres.sh` and `15-tls.sh` now default `SUPADUPA_COMPAT_POOLER_TIMEOUT_SECONDS` to 240 seconds to avoid false failures while still preserving bounded readiness checks.

Oldest stable upgrade with active Realtime continuity and post-upgrade deep Realtime:

```txt
ref=compat-rtup-054020
source stack_version=15.8.1.049
target stack_version=15.8.1.085
backup_id=a19ec4820e3b001cd4b2b5988eb132b9
artifacts=/tmp/supadupa-compat/compat-rtup-054020
```

Result:

```txt
PASS upgrade_matrix.realtime_continuity.15.8.1.085 - active client resubscribed and received post-upgrade broadcast
PASS realtime_deep.fixture - table compat_realtime_202606070540321863349 published
PASS realtime_deep.presence_postgres_changes_db_broadcast - presence, Postgres Changes, private DB broadcast, replay, and same-project private-channel isolation delivered
PASS realtime_deep.reconnect_after_restart - client resubscribed and received broadcast after compat-rtup-054020-realtime-1 restart
PASS upgrade_matrix.complete - validated 1 stable target(s)
PASS project.cleanup - compat-rtup-054020 destroyed
```

### Control API CORS

```txt
curl -sSI -H 'Origin: https://admin.supadupa.brotechlabs.com' \
  https://api.supadupa.brotechlabs.com/v1/health

HTTP/2 200
access-control-allow-origin: https://admin.supadupa.brotechlabs.com
access-control-allow-credentials: true
vary: Origin
```

## Metrics

Current project metrics:

```txt
project_ref=smoke
status=healthy
reserved=1 CPU, 2048 MB RAM, 20 GB disk, 3000 IOPS
observed_cpu_percent=6.97
observed_memory=2710087663/32845762396 bytes
```

Current fleet metrics:

```txt
projects=2
audit_verified=true
host_capacity=16 CPU, 31326 MB RAM, 600 GB disk, 48000 IOPS
host_used=2 CPU, 4096 MB RAM, 40 GB disk, 6000 IOPS
observed_projects_sampled=2
observed_stale_projects=0
```

## Remaining Work

1. Track or upstream a fix for official Supabase CLI typegen.
   - Cleanest upstream fix: preserve `sslmode=require` in `ToPostgresURL`, or only inject Supabase's hosted CA bundle for Supabase-owned DB hosts.
   - Supadupa now has product workarounds through `supadupa-cli projects gen-types` and `supadupa-cli projects db-tunnel` for official CLI typegen.
   - `09-supabase-cli-typegen.sh` now records the exact public official CLI behavior on every default run and proves the tunnel fallback; `09-supabase-cli-matrix.sh` can repeat that probe across `SUPADUPA_SUPABASE_CLI_MATRIX` versions such as `latest 2.105.0`.

2. Expand `scripts/compat` into the full automated suite from `docs/supabase-compat-test-suite.md` and keep it running in CI.
   - Default runner now covers preflight, optional project create/destroy, auth/project inspection, redacted runtime-config guard capture, TLS/redirect/Postgres STARTTLS validation, REST auth, public Postgres, DB fixture, live database desired-state apply/delete for extensions/pg_cron/pgmq/webhooks/schema SQL/roles, provider-backed declaration create/list/delete/masking/metrics for log drains/auth clients/replication/embeddings/vector buckets/network connections, official Supabase CLI version/help capture, CLI command classification, `db push`, `db pull`, `db diff`, migration list/repair, official CLI typegen caveat tracking, optional multi-version official CLI matrix, Supabase JS SDK, Supadupa typegen, Function fixture deploy, Auth admin, refresh/logout/RLS claims, GraphQL, Storage REST, Storage signed URLs, expanded Storage S3 protocol checks, Functions, Realtime, metrics assertions, destructive disposable backup/restore validation, recoverability/PITR readiness gates, backup target redaction/probe checks when configured, opt-in RustFS target validation, opt-in physical backup validation, auto-discovered or configured two-project remote isolation, non-destructive public exposure checks, non-destructive cross-plane security regression checks, stack release catalog completeness and unsupported-upgrade rejection, opt-in stable upgrade matrix with deep Auth/Storage/Realtime/Functions post-upgrade verification, opt-in active Realtime upgrade-continuity validation, and opt-in failed-upgrade rollback validation with guarded recovery-backup restore.
   - `21-custom-domains.sh` is in the default phase list but skips unless `SUPADUPA_COMPAT_CUSTOM_DOMAIN_FQDN` is set. `23-storage-deep.sh` is now in the default phase list for deeper Storage REST/RLS/TUS coverage.
   - The runner now includes `02-cli-profile.sh`, covering JSON/env/TOML profile shape, dedicated-host `storage_s3_url`, public URL leakage checks, workspace binding, handle-only env export, and opt-in audited env materialization.
   - `.github/workflows/compat.yml` now runs local checks on push/PR and live compatibility checks on schedule or manual dispatch when `SUPADUPA_COMPAT_*` secrets are configured.
   - Manual workflow dispatch now accepts `source_stack_version` and `upgrade_targets`; when `upgrade_targets` is empty, `10-upgrade-matrix.sh` fetches `/v1/stack-releases` and uses the newest exposed stable release as the target. This prevents an enabled upgrade-matrix workflow from failing only because `SUPADUPA_UPGRADE_TARGETS` was omitted.
   - Manual workflow dispatch and repository variables now also accept `upgrade_failure_targets`, `upgrade_failure_restore_validate`, and `upgrade_failure_auto_restore`. CI rejects destructive failed-upgrade restore or auto-restore validation unless `create_project=true`, and failed-upgrade targets require `upgrade_matrix=true`.
   - Next phase: configure repository secrets in the target GitHub repo, run the scheduled/manual workflow against a disposable project, and preserve artifacts from failed runs.

3. Keep stable release upgrade validation running continuously.
   - Current live matrix covers `15.8.1.060 -> 15.8.1.085`, `15.8.1.054 -> 15.8.1.085`, and `15.8.1.049 -> 15.8.1.085`.
   - Built-in complete release manifests now cover `15.8.1.085`, `15.8.1.060`, `15.8.1.054`, and `15.8.1.049`.
   - Full service-image release manifests are implemented in code, and unknown configured versions are rejected rather than treated as Postgres-only upgrades. Next validation work is to wire the manual/scheduled GitHub workflow to run this oldest-stable disposable matrix regularly and preserve artifacts from failed runs.

4. Implement and validate hosted-grade physical/PITR recovery semantics.
   - Current logical backups and scheduled WAL artifacts improve recoverability, but they are not a full physical base backup plus WAL replay restore-to-timestamp flow.
   - The hosted-compatible restore-to-time API shape, first-class physical backup policy, real Compose WAL archive default, default Compose PITR restore command, WAL-range artifact validation, SQL marker restore-semantics validation, and RustFS-backed SigV4 project logical backup, Compose `pg_basebackup` physical backup, and WAL upload checks now exist; configure a durable off-host target, then prove destructive restore-to-time against a disposable project.
   - The RustFS phase is intentionally loopback-only and therefore proves object-storage plumbing without satisfying off-host durability. Local interface IPs are rejected by recoverability, and the hosted-grade PITR compat precheck rejects local/private endpoints before destructive restore. `19-durable-backup-target.sh` now creates or updates an operator-supplied S3/R2/remote-MinIO target from credentials, requires the API to report it as `durable_off_host=true` and `recovery_ready=true`, and hands that target to the physical/PITR phase; the remaining work is to run that profile with real off-host credentials and preserve the destructive restore artifacts.
   - `SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true` is now the production guard for refusing physical/WAL uploads and derived PITR buckets unless a selected/default S3-compatible target exists, has passed validation, and is durable off-host. It should be enabled for hosted-grade deployments; local-only artifacts and RustFS loopback targets remain dev-only drill paths with the guard off.
   - S3-compatible target validation, env bootstrap, CLI automation, UI management, and Terraform IaC management are now available, but live `smoke` still has no durable off-host target configured.
   - The recoverability API/UI now exposes this as unavailable instead of implying PITR is fully ready.
   - Live `smoke` currently has no durable off-host S3-compatible backup target configured, so backups are local-only until an off-host target is added.

5. Decide whether to keep or destroy `compat-035843`.
   - It is intentionally still running so the current state can be inspected.
