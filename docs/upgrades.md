# Upgrades

Supadupa separates control-plane upgrades from project stack upgrades.

## Project Stack Releases

Stable project stack releases are exposed by:

```text
GET /v1/stack-releases
```

Project create and upgrade flows should use stable releases from that catalog. The UI and API should not depend on arbitrary moving tags for production-like upgrades.

## Project Upgrade Flow

For a project upgrade:

1. Choose a target stable stack release.
2. Verify the project is healthy enough to upgrade.
3. Create or verify a pre-upgrade backup.
4. Apply the new rendered project stack.
5. Reconcile services, routes, config, and secrets.
6. Validate project health.
7. Record upgrade and rollback metadata.

For production-like upgrades, require durable off-host backup artifacts before allowing the upgrade.

## Upgrade Guard Environment

```text
SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true
SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE=true
```

`SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true` blocks production-like upgrades unless durable backup conditions are met.

`SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE=true` enables automatic restore behavior where supported. This should be used only after failed-upgrade restore has been validated on disposable projects.

## Testing Older Versions

To validate upgrade support:

1. Create a disposable project on an older supported stable release.
2. Seed database, auth, storage, realtime, and functions fixtures.
3. Run backups.
4. Upgrade to the target stable release.
5. Re-run compatibility tests.
6. Validate Studio, public API, DB routes, pooler, storage, functions, and realtime.
7. Test rollback or restore behavior on a disposable project.

Only stable releases should be part of the supported upgrade matrix.

## Control-Plane Updates

Control-plane updates use the normal Compose rebuild/restart:

```bash
docker compose -f deploy/compose.yaml --profile edge up -d --build
```

The API runs meta database migrations on startup and reconciles persisted runtime artifacts.

## Before Updating The Control Plane

Recommended checklist:

1. Confirm admin/API health.
2. Confirm recent control-plane backup.
3. Confirm project backups for important projects.
4. Check current project runtime status.
5. Rebuild and restart.
6. Check API health.
7. Check project route health.
8. Check Advisor and Compliance for new findings.

## Operator Notes

### 0.2.0 — Security hardening and public edge redeploy

This release includes security, persistence, deployment, and validation changes
from the repository-wide remediation pass.

- **Externally observable behavior:** browser admin sessions are cookie-based and
  old admin bearer tokens are rejected after account/security state changes.
  Admins may need to log in again after deployment. Public edge installs can now
  request a wildcard certificate for the control-plane host suffix, while project
  routes request the apps wildcard when projects are created.
- **Who is affected:** existing Compose and Helm operators, especially installs
  with active admin sessions, apply-mode project provisioning, or public Traefik
  TLS.
- **Required action:** rebuild and restart the control plane/admin images, allow
  metadata migrations to run, keep the Cloudflare/Route53 DNS token available to
  Traefik for ACME renewal, and confirm admin/API health after startup.
- **Validation:** `go test ./...`, frontend build/check/audit, Compose setup and
  hardening checks, security regression checks, and a live HTTPS login/API smoke
  against the public admin/API hosts.
- **Rollback constraints:** migration checksums are now enforced. Do not edit
  previously applied migrations on a live install. If rolling back binaries,
  preserve the metadata database and verify admin login, route reconciliation,
  and project health before serving traffic.

### 0.1.0 — Docker proxy network rename

The base Compose definition previously named the internal Docker-API proxy
network `docker-proxy`, while the apply overlay named it `supadupa-docker-proxy`.
The merged stack therefore attached `docker-socket-proxy` (and the control plane)
to **two** redundant internal networks. Both names are now unified to
`supadupa-docker-proxy`.

- **Externally observable behavior:** none. The proxy keeps the same service DNS
  name (`docker-socket-proxy:2375`) and remains on an `internal: true` network;
  only the network's Compose key changed.
- **Who is affected:** existing Compose installs (base and apply mode) on their
  next `up`. Local dev and fresh installs are unaffected beyond the new name.
- **Required action:** none beyond a normal redeploy. On the next
  `docker compose ... up -d`, Compose creates `<project>_supadupa-docker-proxy`
  and the stale `<project>_docker-proxy` network is left orphaned; remove it with
  `docker network prune` (or `docker network rm <project>_docker-proxy`) once the
  stack is healthy. No restart ordering or backup is required.
- **Validation:** `scripts/check-compose-hardening.py` (asserts the proxy is the
  sole Docker-socket owner on exactly the `supadupa-docker-proxy` network) and
  `docker compose -f deploy/compose.yaml -f deploy/compose.apply.yaml config`.
- **Risk if skipped:** none — the orphaned network is harmless; pruning it is
  housekeeping only. There is no rollback constraint.

## Validation Commands

```bash
go test ./...
npm --prefix frontend run build
docker compose -f deploy/compose.yaml config
```

For live compatibility:

```bash
scripts/compat/run.sh
```
