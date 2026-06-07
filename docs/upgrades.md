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
