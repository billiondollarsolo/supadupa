# Backups And Recovery

Supadupa supports project backups, control-plane backups, S3-compatible backup targets, WAL archive plumbing, and recovery posture reporting. The MVP has backup and validation plumbing, but hosted-grade recovery requires off-host proof.

## Control-plane vs project recovery

- **Control-plane backups** capture Supadupa metadata (orgs, projects, policies, encrypted secrets checkpoint). They do **not** replace project database dumps or physical Postgres base backups.
- **Project logical backups** capture application data suitable for logical restore. They intentionally avoid some Supabase internal operational schemas (Realtime, GraphQL, extensions, analytics internals) because restoring those can conflict with a live stack-owned data plane.
- **Full-cluster recovery** (including stack internals and point-in-time rollback) requires **physical base backup + WAL archive + PITR restore**, with a durable off-host target and `SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true` in production.

Always treat platform backup, project logical backup, and PITR posture as complementary operator runbook steps.

## Backup Targets

Configure backup targets in:

```text
Settings -> Backups
```

Supported MVP target type:

```text
S3-compatible storage
```

Examples include Cloudflare R2, AWS S3, MinIO, and RustFS. Local RustFS is useful for development validation, but does not count as off-host recovery.

Environment bootstrap variables:

```text
SUPADUPA_BACKUP_TARGET_NAME=
SUPADUPA_BACKUP_TARGET_ENDPOINT=
SUPADUPA_BACKUP_TARGET_REGION=auto
SUPADUPA_BACKUP_TARGET_BUCKET=
SUPADUPA_BACKUP_TARGET_PREFIX=supadupa
SUPADUPA_BACKUP_TARGET_ACCESS_KEY_ID=
SUPADUPA_BACKUP_TARGET_SECRET_ACCESS_KEY=
SUPADUPA_BACKUP_TARGET_FORCE_PATH_STYLE=false
SUPADUPA_BACKUP_TARGET_AUTO_TEST=false
```

Generated blank/default placeholders are ignored on startup. Supadupa creates or updates the named default target only after meaningful backup target values are supplied, such as a target name, bucket, endpoint, credentials, non-default region/prefix, or `SUPADUPA_BACKUP_TARGET_FORCE_PATH_STYLE=true`. Set `SUPADUPA_BACKUP_TARGET_AUTO_TEST=true` to test a configured target during startup.

## Project Backups

Project backups are managed under:

```text
Projects -> <project> -> Backups
```

Use this page to:

- Run a logical backup.
- View started and finished timestamps.
- See status and error details.
- Review target and artifact details.
- Check recovery posture for the project.

## Control-Plane Backups

Control-plane backup actions are under:

```text
Settings -> Backups
```

Control-plane backups protect Supadupa management state, not the full project data plane by themselves. Project data requires project backups and WAL/PITR validation.

## Recovery Gates

Production-like recovery enforcement is controlled by:

```text
SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true
SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true
```

Use these only after a tested off-host target exists. Leaving them false allows local/dev workflows while Advisor and Compliance continue to report recovery gaps.

## Hosted-Grade Recovery Checklist

Hosted-grade recovery is not considered proven until all of these pass:

1. A real off-host S3/R2/remote-MinIO target exists.
2. Server-side backup target test passes.
3. A recovery-ready default target exists, or each project policy binds to a recovery-ready target.
4. `SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true`.
5. `SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true`.
6. Physical backup validation passes.
7. WAL archive validation passes.
8. Disposable destructive PITR restore validation passes.
9. Failed-upgrade restore validation passes using durable off-host artifacts.

Until that proof exists, Advisor and Compliance should show action-needed recovery findings.

## Upgrade Backups

Project upgrade flows create or verify a pre-upgrade logical backup and record rollback metadata. For production-like upgrades, enforce durable upgrade backup requirements and store artifacts off-host.

## RustFS For Development

RustFS can be used as a local S3-compatible target while developing backup flows. It is useful for:

- Creating backup targets.
- Testing credentials.
- Proving object writes.
- Exercising UI/API paths.

It is not sufficient for production recovery because it can fail with the same host as the project runtime.

## Compatibility Validation

The compatibility suite can exercise backup and recovery posture:

```bash
export SUPADUPA_API_URL=https://api.example.com
export SUPADUPA_TEST_EMAIL=admin@example.com
export SUPADUPA_TEST_PASSWORD='change-this-password'
export SUPADUPA_TEST_REF=smoke
scripts/compat/run.sh
```

Durable destructive recovery tests require disposable projects and real off-host S3/R2 credentials.
