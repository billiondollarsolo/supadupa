# Supadupa Security Review Remediation Plan

Date: 2026-06-13

Related reports:

- [Security review report](./report.md)
- [HTML report](./report.html)
- [Validation report](./validation-report.md)
- [Reviewed surfaces](./reviewed-surfaces.md)

## Purpose

This plan turns the repository-wide security review into an implementation backlog. It is intentionally written for review before code changes. The immediate goal is to fix the three reportable findings, handle local secret hygiene, and add guardrails so the same classes of issues are harder to reintroduce.

## Prioritization

| Priority | Area | Reasoning |
| --- | --- | --- |
| P0 | Local ignored runtime secrets | This workspace contains live-looking generated secrets, backups, and ACME material. It is not a tracked-code vulnerability, but it is an immediate operational hygiene issue if the values are real. |
| P1 | Project restore authorization | A project developer can trigger destructive restore actions. This is the highest product security finding because it can corrupt or roll back project data through normal API access. |
| P2 | Platform admin token revocation | Revoked or demoted platform admins can retain authority until token expiry. This weakens offboarding and incident response. |
| P2 | MFA seed storage | TOTP seeds are stored plaintext in normalized user rows. This weakens MFA if the database or backups are exposed. |
| P3 | Hardening and maintainability | Large files, brittle auth helper semantics, exposure defaults, backup target egress, and SSO readiness affect future security posture. |

## Implementation Strategy

Use small PRs in this order:

1. Operational cleanup and test-only guardrails.
2. Restore authorization fix.
3. Session revocation fix.
4. MFA seed encryption and migration.
5. Hardening backlog.
6. Maintainability splits.

Keep each PR scoped, with regression tests that fail before the fix and pass after the fix. Avoid mixing refactors into security fixes unless the refactor is required to make the fix safe.

## Task 0: Preserve Reports In Repo

Status: done for review artifacts.

### Files

- `docs/reviews/2026-06-13-security-review/report.md`
- `docs/reviews/2026-06-13-security-review/report.html`
- `docs/reviews/2026-06-13-security-review/validation-report.md`
- `docs/reviews/2026-06-13-security-review/reviewed-surfaces.md`
- `docs/reviews/2026-06-13-security-review/remediation-plan.md`

### Definition Of Done

- Reports are present in the repo under `docs/reviews/2026-06-13-security-review/`.
- The plan is reviewable before code changes.

## Task 1: Local Runtime Secret Hygiene

### Problem

Ignored local `runtime/` files contain live-looking generated secrets, backups, control-plane backup data, and ACME material. These files are not tracked by git, but they can still leak through local backups, logs, screenshots, support bundles, accidental copies, or future tooling.

### Scope

Do not commit secret values. Do not paste secrets into issues or PRs. Treat all values under local `runtime/` as potentially compromised until proven test-only.

### Proposed Work

- Inventory ignored runtime files by category without printing values:
  - project `.env` files
  - control-plane backups
  - project logical/physical backups
  - ACME account/certificate material
  - logs that may contain credentials
- Decide whether the environment is disposable.
- If disposable:
  - stop local services
  - remove `runtime/`
  - regenerate from clean setup
- If any values were real:
  - rotate project JWT secrets, database passwords, service-role keys, backup credentials, ACME account material, and any provider tokens
  - invalidate sessions and reissue credentials
  - document which values were rotated without recording the values themselves
- Add a small developer script or docs note for safe cleanup.

### Example Commands

Use these only after confirming local data can be destroyed:

```sh
docker compose -f deploy/compose.yaml down
rm -rf runtime
```

For non-disposable environments, replace deletion with a controlled archive and rotation process.

### Tests And Validation

- `git status --ignored --short runtime`
- `git ls-files runtime` should remain empty.
- Run existing setup smoke test or local startup flow after cleanup.
- Confirm no new tracked files contain secret markers:

```sh
git grep -nE '(JWT_SECRET|SERVICE_ROLE|PRIVATE KEY|acme|password=|SUPABASE_SERVICE_ROLE)' -- . ':!docs/reviews/2026-06-13-security-review/*.md'
```

### Definition Of Done

- Local secret-bearing runtime artifacts are removed or explicitly documented as test-only.
- Any real values found there are rotated.
- No secret values are added to the repo or plan.

## Task 2: Require Admin Authority For Destructive Project Restores

### Finding

Project developers can trigger destructive database restores.

Affected code:

- `internal/api/routes.go:286`
- `internal/api/routes.go:287`
- `internal/api/project_backup_recovery_handlers.go:83`
- `internal/api/project_backup_recovery_handlers.go:106`
- `internal/control/backup.go:1537`
- `internal/control/backup.go:1686`

### Reasoning

Restore operations are broader and more destructive than routine build or deploy actions. A developer can currently roll a project database backward or initiate PITR. Even though the backup id is project-scoped and backup integrity checks exist, the missing control is the caller's authority to perform destructive recovery.

### Policy Decision Needed

Choose one policy before implementation:

| Option | Description | Recommendation |
| --- | --- | --- |
| Project admin required | Only project admins can restore a project database. | Recommended default. |
| Org admin or project admin required | Org admins and project admins can restore. | Reasonable for org-managed environments. |
| Platform admin override | Platform admins can restore any project for break-glass operations. | Recommended as optional override with audit. |
| Developer allowed | Developers intentionally own restore authority. | Not recommended unless product role semantics explicitly say so. |

### Proposed Work

- Change `restoreBackupHandler` and `restorePITRBackupHandler` to require project admin authority.
- Add an explicit confirmation field to both restore request payloads.
- Use a confirmation phrase that binds to the project ref and restore type.
- Keep existing backup id, status, checksum, PITR, and project scoping checks.
- Add audit metadata that records:
  - actor id
  - project ref
  - restore type
  - backup id or recovery timestamp
  - confirmation present
- Update frontend and Terraform/CLI clients if they expose restore actions.
- Update docs to describe who can restore and what confirmation is required.

### Example API Shape

Logical restore request:

```json
{
  "backup_id": "backup-id",
  "confirmation": "restore project atlas-core"
}
```

PITR restore request:

```json
{
  "recovery_time_target_unix": "1781310000",
  "confirmation": "restore pitr project atlas-core"
}
```

Exact strings can be different, but tests should prove an omitted or wrong confirmation fails.

### Tests

Add or update API tests in `internal/api/server_test.go` or a smaller focused test file:

- Developer cannot call `POST /v1/projects/{ref}/restore`.
- Developer cannot call `POST /v1/projects/{ref}/database/backups/restore-pitr`.
- Project admin can call logical restore with correct confirmation.
- Project admin can call PITR restore with correct confirmation.
- Project admin gets 400 or 409 with missing confirmation.
- Project admin gets 400 or 409 with wrong confirmation.
- Project admin cannot restore another project by swapping `{ref}` or backup id.
- Audit event is emitted on accepted restore.

### Validation Commands

```sh
go test ./internal/api ./internal/control
go test ./...
```

### Definition Of Done

- Destructive restore endpoints no longer accept project developer authority.
- Missing or incorrect confirmation blocks both restore paths.
- Existing project scoping and backup integrity checks remain intact.
- Regression tests fail on the old behavior and pass on the new behavior.
- API docs and client behavior match the new authorization policy.

## Task 3: Add Immediate Revocation For Platform Admin Tokens

### Finding

Revoked platform admins retain admin authority until stateless token expiry.

Affected code:

- `internal/api/auth_handlers.go:137`
- `internal/api/auth_handlers.go:183`
- `internal/api/http_helpers.go:166`
- `internal/api/authz_helpers.go:96`
- `internal/api/user_handlers.go:20`
- `internal/api/user_handlers.go:51`
- `internal/api/user_handlers.go:80`

### Reasoning

Current tokens are HMAC-signed and expiring, but they carry role claims that remain valid after the underlying user changes. A demotion, deletion, password reset, or MFA reset should invalidate previously issued privileged sessions.

### Proposed Work

Implement a stateful revocation model while keeping signed tokens.

Recommended design:

- Add user fields:
  - `TokenVersion int64`, or
  - `SessionsValidAfter time.Time`
- Include the selected field in issued tokens.
- On token verification, continue checking signature and expiry.
- Before privileged actions, load the current user by token subject and verify:
  - user exists
  - user role matches required privilege
  - user is not disabled or deleted
  - token version matches or token issued-at is after `SessionsValidAfter`
- Bump token version or update `SessionsValidAfter` on:
  - role change
  - password change/reset
  - MFA enable/disable/reset
  - user deletion
  - explicit logout-all-sessions action if added
- Fail closed if the user lookup fails for platform-admin authorization.

### Helper Design

Prefer a helper that makes current-state checks explicit:

```go
func requireCurrentPlatformAdmin(w http.ResponseWriter, r *http.Request, store control.Store) (*control.User, bool)
```

Then update platform-admin handlers to use the current-state helper instead of only `requirePlatformAdmin(w, r)`.

### Migration Notes

- Existing tokens will not contain the new version or issued-at field.
- Decide compatibility:
  - reject old tokens after deployment, or
  - treat missing token version as invalid for platform-admin routes but valid for lower-risk self routes during a short transition.
- Document that deployment of this change may require admins to log in again.

### Tests

Add tests for:

- Old admin token fails after that user is demoted.
- Old admin token fails after that user is deleted.
- Old admin token fails after password reset if token invalidation includes password changes.
- Old admin token fails after MFA reset if token invalidation includes MFA changes.
- Newly issued admin token succeeds after re-login.
- Non-admin token still fails platform-admin routes.
- Missing claims fail closed.
- Auth-disabled test mode remains explicit and isolated to tests/dev.

### Validation Commands

```sh
go test ./internal/api ./internal/control
go test ./...
```

### Definition Of Done

- Platform-admin authorization uses current store state, not only embedded token role.
- Role changes and deletion invalidate old platform-admin authority immediately.
- Tests cover demotion and deletion reuse attempts.
- Token compatibility behavior is documented.

## Task 4: Encrypt MFA TOTP Seeds In Normalized Persistence

### Finding

Control-plane MFA TOTP seeds are stored plaintext in normalized users rows.

Affected code:

- `internal/control/store.go:2555`
- `internal/control/store.go:2567`
- `internal/control/store.go:2593`
- `internal/control/persistent_store.go:334`
- `internal/control/persistent_store.go:346`
- `internal/control/persistent_store.go:1991`
- `internal/control/persistent_store.go:1993`
- `internal/control/persistent_store.go:2506`

### Reasoning

TOTP seeds are equivalent to second-factor credentials. They should be encrypted at rest with application-controlled encryption, especially because this persistence layer already encrypts project secrets before normalized storage.

### Proposed Work

- Add encrypted columns or reuse existing columns with encrypted payload markers.
- Prefer explicit encrypted columns for clarity:
  - `mfa_secret_encrypted`
  - `mfa_pending_secret_encrypted`
- Encrypt before writing normalized users rows.
- Decrypt when loading normalized user state.
- Preserve support for empty/null values.
- Add migration logic for existing plaintext values:
  - detect plaintext legacy values
  - encrypt on next sync/load migration, or
  - run a one-time DB migration if the project has migration infrastructure for normalized control tables
- Consider forced MFA re-enrollment if prior backups may have exposed seeds.
- Ensure logs and errors never include decrypted seed values.

### Example Storage Behavior

Before:

```text
users.mfa_secret = JBSWY3DPEHPK3PXP
```

After:

```text
users.mfa_secret_encrypted = base64(version || nonce || ciphertext || tag)
```

The exact envelope should match the existing project secret encryption helper.

### Tests

Add persistence tests for:

- Begin MFA enrollment writes no raw pending seed to normalized users rows.
- Confirm MFA writes no raw confirmed seed to normalized users rows.
- Reload decrypts the seed and existing TOTP verification still works.
- Empty MFA fields stay empty/null.
- Legacy plaintext values migrate or are handled according to the chosen migration policy.
- Project secret encryption behavior remains unchanged.

### Validation Commands

```sh
go test ./internal/control
go test ./internal/api ./internal/control
go test ./...
```

Optional manual DB validation:

```sql
SELECT mfa_secret, mfa_pending_secret FROM users;
```

The query should not return raw TOTP seed values after migration. Adjust table/column names if encrypted columns replace the plaintext columns.

### Definition Of Done

- Normalized user rows no longer store raw TOTP seeds.
- Existing MFA login and replay-counter behavior still works.
- Migration behavior for existing installs is tested and documented.
- Operators are told whether MFA re-enrollment or rotation is recommended.

## Task 5: Fail Closed In Route-Local Authorization Helpers

### Problem

Some route-local helpers return success when request claims are absent. This relies on global `withAuth(AuthRequired=true)` wrapping protected routes. That is acceptable in the current main binary, but brittle for tests, alternate embedding, and future routes.

### Proposed Work

- Review helpers such as:
  - `requirePlatformAdmin`
  - `requireOrgRole`
  - `requireProjectRole`
- Make protected helpers fail closed when claims are absent.
- If test/dev no-auth behavior is needed, make it explicit through a separate test helper or server option.
- Add tests that protected handlers reject missing claims even if called directly.

### Tests

```sh
go test ./internal/api
```

### Definition Of Done

- Missing claims do not pass protected route-local authorization helpers by default.
- Existing auth-disabled test paths are explicit and documented.
- No production route depends on missing claims as authorization success.

## Task 6: Harden Backup Target Egress Controls

### Problem

Backup target endpoints are platform-admin-only, so no lower-privilege SSRF finding survived. Still, S3-compatible endpoint testing can reach arbitrary HTTP(S) destinations configured by an admin. This is acceptable for trusted operators but should be safer by default.

### Proposed Work

- Add warnings or policy checks for private, loopback, link-local, and metadata endpoint ranges.
- Consider an allowlist mode for production deployments.
- Resolve and validate redirect behavior if the S3 client follows redirects.
- Log endpoint host and policy decision without logging credentials.
- Add docs for safe S3-compatible target configuration.

### Tests

- Reject or warn on `127.0.0.1`, `localhost`, RFC1918, link-local, and metadata IP endpoints according to the selected policy.
- Confirm public S3-compatible endpoints still work.
- Confirm credentials are not logged on validation failure.

### Definition Of Done

- Operators get clear protection or warnings for risky backup target endpoints.
- The chosen behavior is configurable and documented.

## Task 7: Revisit Direct Database And Pooler Exposure Defaults

### Problem

Compose/edge profiles bind database and pooler entrypoints broadly while project routing code gates exposure separately. This can be safe when operators understand the network model, but broad binds increase blast radius when routing or firewall assumptions are wrong.

### Proposed Work

- Prefer loopback/default-private binding unless the operator explicitly opts in.
- Add setup prompts or flags such as `--db-public-bind`.
- Document required firewall rules for production.
- Add a startup warning when public DB/pooler binds are enabled.

### Tests

- Compose rendering defaults to loopback or private mode.
- Explicit public bind flag renders public exposure.
- Documentation examples match actual defaults.

### Definition Of Done

- Production defaults avoid accidental broad DB/pooler exposure.
- Public exposure requires explicit operator intent.

## Task 8: Clarify SSO Readiness And Real SAML Support

### Problem

Current SSO behavior is gated around a signed JSON adapter rather than full SAML protocol handling. The review did not find a reportable SSO bypass, but production SSO readiness should be explicit.

### Proposed Work

- Update docs and UI copy to clearly state what is supported today.
- If production SAML is in scope, plan a real SAML implementation or integrate a proven library.
- Add tests for assertion binding, audience, recipient, destination, ACS, replay, expiry, and issuer behavior.
- Keep adapter gate disabled by default for production unless explicitly intended.

### Definition Of Done

- Operators cannot mistake the adapter for complete SAML support.
- Any future SAML implementation has protocol-level tests.

## Task 9: Split Large Files Along Ownership Boundaries

### Problem

Large files increase review cost and make security-sensitive logic harder to audit.

Current high-return targets:

| File | Lines | Suggested Split |
| --- | ---: | --- |
| `internal/control/store.go` | 10703 | users/auth/MFA, org/project RBAC, secrets, backups, audit, platform defaults |
| `internal/api/server_test.go` | 7799 | auth tests, project tests, backup tests, secret tests, routing tests |
| `internal/mcp/server.go` | 4293 | transport, auth, tool registry, project tools, admin tools |
| `internal/provisioner/compose/compose.go` | 4265 | rendering, file writes, Docker command runner, edge functions, replicas |
| `internal/cli/cli.go` | 3403 | command groups by domain |
| `frontend/src/pages/project/database-panels.tsx` | 2003 | schemas, roles, extensions, cron, queues, webhooks |

### Proposed Work

- Do not start with a sweeping refactor.
- Split one file per PR.
- Preserve public APIs first, then move internals.
- Add package-level tests before moving code if coverage is weak.
- Prefer domain-oriented files over generic utility files.

### Tests

Run the relevant package tests for each split:

```sh
go test ./internal/control
go test ./internal/api
go test ./internal/mcp
go test ./internal/provisioner/compose
go test ./internal/cli
npm run check
```

### Definition Of Done

- Each split PR has no intended behavior change.
- Tests pass before and after the split.
- Security-sensitive functions are easier to locate by domain.

## Task 10: Add Security Regression Checks To CI

### Proposed Work

- Run backend package tests and frontend checks in CI.
- Add static checks for:
  - browser token storage markers
  - direct `dangerouslySetInnerHTML`
  - new route-local auth helpers that pass on missing claims
  - raw MFA seed persistence markers
  - restore endpoints allowing developer role
- Add dependency checks:
  - `npm audit --omit=dev`
  - `govulncheck ./...` when network access is available
- Store future security review reports under `docs/reviews/YYYY-MM-DD-security-review/`.

### Definition Of Done

- CI catches regressions for the three fixed findings.
- Dependency checks run in a reliable environment.
- Security review outputs have a standard location.

## Overall Validation Checklist

Before considering this plan complete:

- `go test ./...` passes.
- `npm run check` passes in `frontend`.
- `npm audit --omit=dev` passes or has documented accepted exceptions.
- `govulncheck ./...` passes or has documented network/tooling exceptions.
- Manual negative tests confirm:
  - developers cannot restore project databases
  - stale admin tokens fail after demotion/deletion
  - normalized user rows do not expose raw TOTP seeds
- Docs explain:
  - restore authorization policy
  - session revocation behavior
  - MFA seed migration/rotation guidance
  - safe handling of local runtime artifacts

## Review Questions

- Should project admins, org admins, or only platform admins be allowed to restore databases?
- Should token invalidation reject all pre-migration tokens immediately, or only for privileged routes?
- Should existing MFA users be forced to re-enroll after encrypted seed migration?
- Should production defaults make direct database/pooler exposure opt-in?
- Should backup target private-network egress be rejected by default or only warned?
