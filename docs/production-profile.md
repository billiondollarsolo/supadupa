# Production Profile Checklist

Operator checklist for production-like Supadupa installs on the **Docker Compose** MVP path. Use this after a successful [Install](install.md) and before treating a fleet as recovery-ready or multi-tenant production.

Related: [Security](security.md), [Backups And Recovery](backups-recovery.md), [Upgrades](upgrades.md), [Operations](operations.md), [Known Issues](known-issues.md).

## 1. Strong secrets

Generate high-entropy values and keep them stable:

```text
SUPADUPA_SECRET_KEY
SUPADUPA_AUTH_SECRET
```

- Startup rejects missing values, known development placeholders, and short secrets unless `SUPADUPA_ALLOW_DEV_SECRETS=true` (local only).
- Do not rotate casually without understanding encrypted payload and session impact.
- Prefer an external key provider or protected vault key file for hardened installs (`SUPADUPA_KMS_*` / `SUPADUPA_VAULT_KEY_FILE` — see [Security](security.md)).
- Bootstrap admin credentials (`SUPADUPA_BOOTSTRAP_EMAIL` / `SUPADUPA_BOOTSTRAP_PASSWORD`) should be removed from `.env` after first login if policy requires it.
- Keep `.env` mode `0600`; never commit runtime secrets or unsanitized artifacts.

## 2. Recovery-ready off-host backup target

Before claiming production recovery:

1. Register a real **off-host** S3-compatible target (S3, R2, remote MinIO — not loopback/local-only).
2. Run the server-side target test until it passes.
3. Mark a default recovery-ready target, or bind each project backup policy to a recovery-ready target.
4. Confirm Advisor/Compliance no longer report local-only recovery posture for production projects.

See the hosted-grade recovery checklist in [Backups And Recovery](backups-recovery.md).

## 3. Fail-closed recovery and upgrade guards

With a tested durable target in place, enable:

```text
SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true
SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true
```

- `REQUIRE_RECOVERY_READY_TARGETS` rejects physical/WAL uploads against missing, untested, loopback, or local-only targets.
- `REQUIRE_DURABLE_UPGRADE_BACKUP` blocks production-like project upgrades unless a durable pre-upgrade backup exists on a recovery-ready target.
- Restart the control plane after changing these env vars. Confirm with `GET /v1/runtime-config` (admin-only, redacted).

Leaving both false is appropriate for local/dev drills only.

## 4. Production posture flag

Enable the platform feature flag **`production_posture`** (Settings → Features, or defaults API).

- When on, recovery-related Advisor findings use full production severity (backup-target guards, recovery-ready targets).
- When off, local/MVP installs avoid high-severity recovery noise.
- Default in code is `false`; production fleets should turn it **on** once recovery targets and guards above are real.

## 5. Database external access — fail closed

Keep **`database_external_access` false** unless external raw Postgres/pooler clients are an explicit requirement.

- Public HTTPS app routes (Auth, REST, GraphQL, Storage, Realtime, Functions) do **not** need this flag.
- External TCP DB/pooler (`5432` / `6543`) requires **both** the platform master switch and a per-project `db_ingress_mode` of `allowlisted` or `public`.
- Prefer loopback binds (`127.0.0.1`) for DB ports; use `--db-public-bind` / `0.0.0.0` only with host/provider firewall CIDRs and project allowlists.

## 6. Platform SSO JSON adapter — OFF

```text
# Leave unset or false in production
SUPADUPA_ENABLE_PLATFORM_SSO_JSON_ADAPTER=false
```

- Platform SSO is a **normalized JSON adapter**, not production SAML XML validation / XML DSig.
- Do not enable the adapter for production identity federation. Use password (and MFA) admin auth, or place a trusted IdP bridge in front that validates real SAML before any normalized assertion.
- Keep `platform_sso_scim` feature flag off until real SAML (or a permanent “unsupported”) decision lands. Details: [Security — Platform SSO](security.md#platform-sso).

## 7. Sizing (~4 GB per full-profile project)

- Budget **~4 GB RAM per full-profile project** plus ~0.5 GB for the control plane (API, admin UI, meta DB, Traefik, Docker proxy).
- Logflare/analytics on the `full` profile alone can approach ~1 GB and OOM on small hosts.
- On constrained hosts, use a leaner project profile or disable analytics; see [Install — Resource Requirements](install.md#resource-requirements) and [Known Issues](known-issues.md).

## 8. Compose apply mode and Docker proxy sensitivity

Apply mode lets the control plane create/update/destroy project stacks:

```text
# Requires deploy/compose.apply.yaml (and setup that sets these)
SUPADUPA_COMPOSE_APPLY=true
SUPADUPA_DOCKER_GID=<host docker.sock group id>
```

- The control plane **must not** mount `/var/run/docker.sock`; only `docker-socket-proxy` may.
- Control plane talks to Docker via `DOCKER_HOST=tcp://docker-socket-proxy:2375`.
- The proxy is a privileged trust boundary (API allowlist, project labels, no privileged/host-namespace creates). Treat proxy changes as security-sensitive.
- Validate apply-mode installs with `scripts/check-compose-apply-lifecycle-smoke.sh` and compose hardening checks before Terraform or automated lifecycle.

Details: [Security](security.md), [Install](install.md).

## 9. Stack versions — pin releases

- Use stable stack versions from `GET /v1/stack-releases` / the admin upgrade catalog.
- Do **not** use stack version `"latest"`.
- Prefer pinned platform images and digest-locked platform base images where documented.

## 10. Quick production gate summary

| Item | Production expectation |
|------|------------------------|
| Secrets | Strong, stable, no `ALLOW_DEV_SECRETS` |
| Backup target | Off-host, tested, recovery-ready |
| `SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS` | `true` |
| `SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP` | `true` |
| `production_posture` | enabled |
| `database_external_access` | `false` unless intentional |
| SSO JSON adapter | off |
| Host RAM | ~4 GB × projects + control plane |
| Apply mode | overlay + proxy only; no direct socket in control plane |
| Stack version | pinned stable release, never `latest` |

Until off-host PITR / durable failed-upgrade restore are proven for your target, treat recovery as **not hosted-grade** even when this checklist is otherwise green — see [Known Issues](known-issues.md) and [Backups And Recovery](backups-recovery.md).
