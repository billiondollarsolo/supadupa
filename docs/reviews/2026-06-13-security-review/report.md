# Security Review: supadupa

## Scope

- Scan mode: repository-wide security and engineering review for `/root/supadupa-all/supadupa`.
- Scan id: `ce8f05a_20260613T010755Z`.
- Primary reviewed surfaces: Go management API, control store, persistence, backup/restore, provisioning, Docker proxy, Kubernetes/operator rendering, Terraform client, CLI/MCP, React frontend, deployment files, and setup scripts.
- Threat model: generated during Phase 1 from local repository context and copied below.
- Artifact root: `/tmp/codex-security-scans/supadupa/ce8f05a_20260613T010755Z`.
- Coverage artifacts: `artifacts/02_discovery`, `artifacts/03_coverage`, `artifacts/04_reconciliation`, and `artifacts/05_findings`.
- Test status: scoped Go tests passed for reviewed packages; frontend static checks and Vitest passed; `npm audit --omit=dev` reported 0 vulnerabilities.
- Limitation: `govulncheck ./...` could not complete because `https://vuln.go.dev/index/modules.json.gz` returned HTTP 403.
- Worktree note: pre-existing local changes were present in `frontend/src/app.tsx` and `frontend/capture-screenshots.mjs`; this scan did not modify repository files.

### Scan Summary

| Field | Value |
| --- | --- |
| Reportable findings | 3 |
| Severity mix | 1 high, 2 medium |
| Confidence mix | 1 high, 2 medium |
| Coverage | 36 selected deep-review rows from 3945 ranked inventory rows, plus supporting files named in ledgers |
| Validation mode | Static source trace, scoped package tests, frontend static/tests, dependency audit |
| Local hygiene issue | Ignored `runtime/` files contain live-looking generated secrets/backups/cert material in this workspace |

## Threat Model

Supadupa is a self-hosted, multi-project Supabase control plane. Its primary product surfaces are the Go management API and admin control plane (`internal/api`, `internal/control`), Docker Compose provisioning path (`internal/provisioner/compose`, `deploy/compose.yaml`, `scripts/setup-compose.sh`), Kubernetes/operator rendering path (`internal/provisioner/kubernetes`, `internal/operator`, `charts/supadupa`), Terraform provider (`internal/terraform`), CLI and MCP binaries (`cmd/*`, `internal/cli`, `internal/mcp`), and React admin UI (`frontend/src`). The platform creates and manages isolated project stacks, public routes, database and pooler exposure, project secrets, SSO/SCIM configuration, backups, audit logs, and operational metadata.

Key assets include control-plane authentication secrets, session tokens, bootstrap credentials, SSO/SCIM tokens, MFA secrets, password hashes, project service-role keys, database passwords, S3 keys, backup credentials, Docker/Kubernetes privileges, meta database state, audit logs, backup/PITR metadata, platform defaults, organization membership, RBAC, and public edge routes.

Important trust boundaries include internet clients versus public admin/API/project routes, browser admin users versus bearer-token clients, authenticated users versus admin-only platform operations, organization/project members versus other tenants, the control plane versus managed project runtimes, the control plane versus Docker/Kubernetes APIs, and the control plane versus external IdPs, SCIM clients, DNS/TLS providers, S3-compatible backup targets, SMTP/SMS/OAuth providers, and observability endpoints.

Production-like assumptions used for severity calibration: `AuthRequired` is enabled, control-plane secrets are stable and non-development, TLS terminates correctly at the edge, bootstrap is not exposed after initialization, and operators are trusted to configure DNS, host firewall rules, backup targets, and database/pooler exposure intentionally. Project tenants may still be mutually untrusted in hosted-like usage.

Priority attacker stories were lower-privilege authenticated users crossing org/project boundaries, stale or forged authentication state, secret reveal/persistence mistakes, unsafe restore or provisioning actions, Docker proxy breakout, public routing/database exposure mistakes, and backup/SSRF-like egress through storage target configuration. Developer-only scripts and local smoke tooling were treated as lower priority unless they directly affected production runtime.

## Findings

| # | Severity | Confidence | Finding |
| --- | --- | --- | --- |
| 1 | high | medium | [Project developers can trigger destructive database restores](#1-project-developers-can-trigger-destructive-database-restores) |
| 2 | medium | high | [Revoked platform admins retain admin authority until stateless token expiry](#2-revoked-platform-admins-retain-admin-authority-until-stateless-token-expiry) |
| 3 | medium | medium | [Control-plane MFA TOTP seeds are stored plaintext in normalized users rows](#3-control-plane-mfa-totp-seeds-are-stored-plaintext-in-normalized-users-rows) |

### Confidence Scale

| Label | Meaning |
| --- | --- |
| high | Direct source, configuration, or runtime evidence supports the finding, with no material unresolved reachability or exploitability blocker. |
| medium | Source evidence supports a plausible issue, but runtime behavior, deployment configuration, role reachability, type constraints, or exploit reliability still need proof. |
| low | Weak or incomplete evidence; included only for explicit follow-up candidates. |

### [1] Project developers can trigger destructive database restores

| Field | Value |
| --- | --- |
| Severity | high |
| Confidence | medium |
| Confidence rationale | The HTTP routes, developer role gate, and restore command sinks are directly traced, but final severity depends on whether product policy intentionally grants developers destructive restore authority. |
| Category | Authorization bypass / destructive action authorization |
| CWE | CWE-862 Missing Authorization; CWE-863 Incorrect Authorization |
| Affected lines | `internal/api/routes.go:286`, `internal/api/routes.go:287`, `internal/api/project_backup_recovery_handlers.go:83`, `internal/api/project_backup_recovery_handlers.go:106`, `internal/control/backup.go:1537`, `internal/control/backup.go:1686` |

#### Summary

Logical and PITR restore endpoints are exposed as authenticated project routes, but both handlers require only `roleDeveloper`. Restore operations can roll back or overwrite project database state, so this grants a non-admin project member a destructive recovery action that is stronger than normal development access.

#### Validation

Static validation traced the route registration to the handler guard and then to the restore sinks. `POST /v1/projects/{ref}/restore` maps to `restoreBackupHandler`, and `POST /v1/projects/{ref}/database/backups/restore-pitr` maps to `restorePITRBackupHandler`. Both use `requireProjectRole(..., roleDeveloper)`. The backup service then executes the logical restore or PITR restore command when dry-run mode is not active. Scoped backend tests passed.

Existing checks scope backups to the project and validate backup status, checksum, and PITR recoverability. Those are good integrity controls, but they do not make the caller an admin or require explicit destructive confirmation.

#### Dataflow

Authenticated developer request -> `routes.go` restore route -> `restoreBackupHandler` or `restorePITRBackupHandler` -> `requireProjectRole(..., roleDeveloper)` -> request payload backup id or target timestamp -> `BackupService.RestoreBackup` or `BackupService.RestoreToTime` -> restore command execution.

#### Reachability

The attacker needs authenticated developer membership on the target project or organization. From the remote API, that developer can send the restore request for the project they can access. The impact is same-project, not cross-tenant by itself, but it crosses the administrative boundary between build/deploy participation and destructive database recovery.

#### Severity

Severity is high because a lower project role can trigger broad integrity and availability impact on production project data through a normal HTTP API. The impact is limited to projects where the attacker already has developer access, which keeps this below critical. Evidence that the product intentionally defines developers as restore operators would lower this to a policy/design decision; evidence that production projects routinely grant developer to non-admin contributors reinforces high severity.

#### Remediation

Require project admin, organization owner/admin, or platform admin for restore execution. Add an explicit destructive confirmation field for logical and PITR restores, similar to platform restore confirmation. Add tests that a project developer receives 403 for both restore endpoints while an admin succeeds. Audit all project routes where `roleDeveloper` gates irreversible lifecycle, data, or exposure changes.

### [2] Revoked platform admins retain admin authority until stateless token expiry

| Field | Value |
| --- | --- |
| Severity | medium |
| Confidence | high |
| Confidence rationale | Token issue, token verification, platform-admin authorization, and admin sinks are directly traced; the only material precondition is possession of a previously valid admin token. |
| Category | Stale session / privilege revocation bypass |
| CWE | CWE-613 Insufficient Session Expiration; CWE-863 Incorrect Authorization |
| Affected lines | `internal/api/auth_handlers.go:137`, `internal/api/auth_handlers.go:183`, `internal/api/http_helpers.go:166`, `internal/api/authz_helpers.go:96`, `internal/api/user_handlers.go:20`, `internal/api/user_handlers.go:51`, `internal/api/user_handlers.go:80` |

#### Summary

Bootstrap and login issue 24-hour tokens carrying the user's role. `withAuth` verifies only token signature and expiry, then `requirePlatformAdmin` accepts `claims.Role == "admin"` without checking the current user record. A platform admin who is demoted or deleted can continue using the old token against platform-admin routes until it expires.

#### Validation

Static validation traced token issuance at login/bootstrap, verification in middleware, and authorization in `requirePlatformAdmin`. `AuthService.Verify` is used as the closest control and installs token claims into the request context. No current-user lookup, token version, revocation list, or issued-after cutoff is checked before accepting the embedded role. Scoped API/control tests passed.

Counterevidence is that this requires prior admin authority. It is still reportable because demotion/deletion is intended to revoke admin authority, and the old token remains valid for up to 24 hours.

#### Dataflow

Admin login/bootstrap -> role-bearing token valid for 24 hours -> another admin demotes or deletes the user -> old bearer token or auth cookie reaches `withAuth` -> HMAC/expiry check succeeds -> `requirePlatformAdmin` trusts embedded `admin` role -> user create/update/delete and other platform-admin actions remain available.

#### Reachability

The attacker must be a former platform admin or have stolen a valid admin token before revocation. The API remains production-authenticated, but the stale token is still accepted as authenticated and authorized. Representative sinks are user create, update, and delete; other platform-admin-only handlers using the same helper are likely affected.

#### Severity

Severity is medium because this is not an initial admin escalation, but it materially weakens incident response, offboarding, and demotion. It can preserve control-plane privileges for the token TTL after access should have been revoked. Shorter token lifetime would reduce severity; adding immediate revocation checks would close the path.

#### Remediation

Add stateful token invalidation for platform-admin authorization: user existence, current role, disabled/deleted status, and a token version or `issued_after` timestamp. Recheck current user state before privileged platform routes, at least for platform-admin actions. Invalidate sessions on role change, password reset, MFA reset, and user deletion. Add regression tests that reuse an old admin token after demotion and deletion and expect 403 or 401.

### [3] Control-plane MFA TOTP seeds are stored plaintext in normalized users rows

| Field | Value |
| --- | --- |
| Severity | medium |
| Confidence | medium |
| Confidence rationale | The seed generation and normalized-table write/read path are directly traced, but exploitability depends on access to the database rows or backups and any deployment-level encryption controls. |
| Category | Sensitive secret stored without application-level encryption |
| CWE | CWE-312 Cleartext Storage of Sensitive Information; CWE-522 Insufficiently Protected Credentials |
| Affected lines | `internal/control/store.go:2555`, `internal/control/store.go:2567`, `internal/control/store.go:2593`, `internal/control/persistent_store.go:334`, `internal/control/persistent_store.go:346`, `internal/control/persistent_store.go:1991`, `internal/control/persistent_store.go:1993`, `internal/control/persistent_store.go:2506` |

#### Summary

MFA enrollment stores TOTP seeds in `User.MFAPendingSecret` and promotes them to `User.MFASecret` after confirmation. The persistent store writes both fields directly into normalized `users` columns. The same persistence layer encrypts project secrets before normalized storage, showing that application-level encryption is available but not applied to MFA seeds.

#### Validation

Static validation traced generation at `BeginUserMFAEnrollment`, assignment to pending/confirmed user fields, normalized `users` insert, and normalized reload. Project secrets are encrypted before insertion into the `secrets` table, which is a useful negative control. Scoped control tests passed.

Counterevidence is that MFA fields are omitted from JSON responses and the encrypted checkpoint protects another copy of state. Those controls do not protect normalized users rows or backups/exports that include them.

#### Dataflow

MFA enrollment -> TOTP seed generation -> `user.MFAPendingSecret` -> MFA confirmation -> `user.MFASecret` -> persistent checkpoint sync -> `users.mfa_secret` / `users.mfa_pending_secret` plaintext insert -> later reload directly into user state.

#### Reachability

This is not reachable by an ordinary HTTP user. The attacker needs read access to the normalized control-plane database, an exported SQL backup, or equivalent operational artifact. With the seed and the user's primary credential, the attacker can generate valid TOTP codes and bypass the intended MFA barrier.

#### Severity

Severity is medium because the issue weakens MFA for control-plane accounts, including admins, but exploitation requires database/backup access and usually another credential. Mandatory external database encryption or column encryption would reduce practical exposure; storing only encrypted seeds in application-controlled fields would close the code-level weakness.

#### Remediation

Encrypt MFA seeds before writing normalized user rows, using an authenticated encryption path equivalent to project secret encryption. Consider separating pending and confirmed seed encryption metadata and rotating/re-enrolling existing MFA seeds after migration. Keep JSON omission, and add persistence tests asserting the normalized database does not contain raw TOTP seeds.

## Reviewed Surfaces

| Surface | Risk Area | Outcome | Notes |
| --- | --- | --- | --- |
| `internal/api/project_backup_recovery_handlers.go` | Destructive project restore authorization | Reported | Developer-role users can trigger logical and PITR restore. |
| `internal/api/auth_handlers.go`, `internal/api/http_helpers.go`, `internal/api/authz_helpers.go`, `internal/api/user_handlers.go` | Authentication token revocation and platform-admin authorization | Reported | Role-bearing 24-hour token remains authoritative after demotion/deletion. |
| `internal/control/store.go`, `internal/control/persistent_store.go` | MFA seed persistence | Reported | Normalized users rows store TOTP seeds without application-level encryption. |
| `frontend/src/api.ts`, `frontend/src/lib/auth-session.ts`, `frontend/src/app.tsx`, `frontend/scripts/check-static.mjs` | Browser token handling, XSS sinks, URL construction | No issue found | No token storage or HTML sink found; static checks and frontend tests passed. |
| `cmd/supadupa-docker-proxy/main.go` | Docker daemon mediation | No issue found | Allowlist, label validation, privileged mode rejection, namespace checks, and bind-mount limits reviewed. |
| `internal/provisioner/compose/compose.go` | Compose generation, file writes, command execution | No issue found | Project refs validated; commands use argv arrays or quoted operator templates. |
| `internal/provisioner/kubernetes/kubernetes.go`, `internal/operator/isolation.go`, `charts/supadupa` | Kubernetes rendering, network policy, RBAC | No issue found | Rendering and network isolation reviewed; no reachable lower-privilege bypass found. |
| `internal/control/sso.go`, SSO/SCIM routes | SSO/SCIM assertion and token handling | No issue found | JSON adapter is gated; validation and replay controls reviewed. Production SAML support remains a readiness gap. |
| `internal/control/backup.go`, `internal/api/platform_backup_handlers.go` | Backup targets, platform restore, S3 endpoint testing | Rejected | Platform-admin-only backup target testing did not produce a lower-privilege SSRF finding; retain private-network warnings or allowlists as hardening. |
| `deploy/compose.yaml`, `scripts/setup-compose.sh` | Deployment defaults and port exposure | Needs follow-up | Direct Postgres/pooler exposure defaults deserve explicit production hardening, even though routing config gates project DB ingress. |
| `runtime/` ignored local files | Local secret hygiene | Needs follow-up | Ignored runtime/backups contain live-looking generated secrets, control-plane backup data, and ACME material in this workspace. These were not tracked by git, but should be purged and rotated if real. |
| Large implementation files | Maintainability and reviewability | Needs follow-up | `internal/control/store.go` is 10703 lines; several API, MCP, CLI, provisioning, and frontend files exceed 1000 lines. |

## Open Questions And Follow Up

- Confirm whether project developers are intended to run database restores. If not, change both restore endpoints to project admin or owner/admin and add regression tests.
- Decide the platform session revocation model. A token version or user `sessions_valid_after` timestamp would cover role change, password reset, MFA reset, and deletion.
- Migrate existing MFA seeds to encrypted storage and plan forced MFA re-enrollment if seed exposure through existing backups is plausible.
- Purge local ignored `runtime/` artifacts containing generated secrets, project backups, and ACME account material from this machine; rotate any values that were real.
- Split the largest implementation files along stable ownership boundaries. Highest-return candidates are `internal/control/store.go`, `internal/mcp/server.go`, `internal/provisioner/compose/compose.go`, `internal/cli/cli.go`, and `frontend/src/pages/project/database-panels.tsx`.
- Follow up on production hardening for direct database/pooler bind defaults, backup target private-network allowlists, and real SAML support versus the current signed JSON adapter gate.
