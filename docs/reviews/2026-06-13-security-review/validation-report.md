# Validation Report

Scan id: ce8f05a_20260613T010755Z

## Method

Validation used static source tracing across the discovered entrypoints, root controls, and sinks, plus scoped test execution for the reviewed backend and frontend surfaces.

Commands run:

- `go test ./internal/api ./internal/control ./cmd/supadupa-docker-proxy ./internal/provisioner/compose ./internal/provisioner/kubernetes ./internal/operator ./internal/terraform ./internal/cli ./internal/mcp`
- `npm run check` in `frontend`
- `npm audit --omit=dev` in `frontend`
- `govulncheck ./...` attempted, but vulnerability feed fetch returned HTTP 403 from `https://vuln.go.dev/index/modules.json.gz`

## Rubric

| Criterion | Description |
| --- | --- |
| Realistic interface | The issue must be reachable through an HTTP API, persisted control-plane data path, CLI/client path, or deployment path in scope. |
| Attacker precondition | The attacker role must be lower than the protected action or must show a meaningful revocation/secret-control bypass. |
| Broken control | The closest authorization, token, encryption, path, network, or persistence control must be incomplete on the exact path. |
| Security sink or side effect | The path must reach privileged mutation, destructive restore, secret exposure, credential bypass, host/container action, or comparable impact. |
| Counterevidence | Existing checks, role gates, masking, encryption, tests, and deployment assumptions must not defeat the exact instance. |

## Candidate Closure Table

| Candidate | File:line | Family | Validation method | Closest control | Survives | Confidence |
| --- | --- | --- | --- | --- | --- | --- |
| CAND-PROJ-RESTORE-DEVELOPER-AUTHZ | `internal/api/project_backup_recovery_handlers.go:83` | Authorization for destructive restore | Static route-to-sink trace plus scoped backend tests | `requireProjectRole(..., roleDeveloper)` | yes | medium |
| CAND-PROJ-RESTORE-DEVELOPER-AUTHZ | `internal/api/project_backup_recovery_handlers.go:106` | Authorization for destructive restore | Static route-to-sink trace plus scoped backend tests | `requireProjectRole(..., roleDeveloper)` | yes | medium |
| CAND-AUTHZ-STALE-PLATFORM-ADMIN-TOKEN | `internal/api/authz_helpers.go:96` | Stale privileged token | Static token-issue/verify/authz trace plus scoped backend tests | `auth.Verify` signature/expiry and embedded role | yes | high |
| CAND-MFA-SEED-PLAINTEXT | `internal/control/persistent_store.go:1993` | Sensitive secret storage | Static persistence trace plus scoped control tests | Encrypted checkpoint exists, normalized users row is plaintext | yes | medium |

## Findings

### CAND-PROJ-RESTORE-DEVELOPER-AUTHZ

Developer-role users can call both restore endpoints:

- `internal/api/routes.go:286` registers `POST /v1/projects/{ref}/restore`
- `internal/api/routes.go:287` registers `POST /v1/projects/{ref}/database/backups/restore-pitr`
- `internal/api/project_backup_recovery_handlers.go:83` and `:106` require only `roleDeveloper`
- `internal/control/backup.go:1537` and `:1686` execute logical/PITR restore commands

The same shard observed that backup policy mutations are admin-only, which supports treating destructive restore as a stronger authority than normal developer operations. Counterevidence: developers are allowed to trigger backups and some lifecycle operations, and product policy could intentionally define developers as data-plane restore operators. Because that policy is not proven in code, the finding survives with medium confidence.

### CAND-AUTHZ-STALE-PLATFORM-ADMIN-TOKEN

Bootstrap and login issue 24-hour role-bearing tokens at `internal/api/auth_handlers.go:137` and `:183`. `internal/api/http_helpers.go:166` verifies signature and expiry, then installs claims. `internal/api/authz_helpers.go:96` accepts the embedded `admin` role without reloading current user state, token version, or revocation state. User create/update/delete in `internal/api/user_handlers.go:20`, `:51`, and `:80` are representative platform-admin sinks.

Counterevidence: the attacker must previously have platform-admin access, and this is not an initial privilege escalation from a normal user. It still survives because revocation/demotion is a security boundary and the old token remains authoritative for up to 24 hours.

### CAND-MFA-SEED-PLAINTEXT

MFA enrollment generates and stores TOTP seeds in memory at `internal/control/store.go:2555`, `:2567`, and `:2593`. The persistent store writes `user.MFASecret` and `user.MFAPendingSecret` directly into normalized `users` rows at `internal/control/persistent_store.go:1991` and `:1993`, and reloads them directly at `:334` and `:346`. Project secrets in the same persistence layer are encrypted before normalized storage at `internal/control/persistent_store.go:2506`, so application-level encryption is available but not applied to MFA seeds.

Counterevidence: the encrypted checkpoint protects another copy of state, and JSON responses omit MFA fields. Those controls do not protect the normalized users table or backups/exports containing it. Deployment-level disk or database encryption was not proven from the repository, so this survives with medium confidence.

## Negative Controls And Suppressions

- Frontend auth/client code: no browser token storage, no HTML sink, and static checks passed.
- Docker proxy: Docker API allowlist, Compose label validation, privileged container rejection, namespace/capability restrictions, and bind mount constraints were reviewed with no surviving breakout path.
- Compose/Kubernetes provisioning: command execution uses argument arrays or operator-controlled shell templates with shell-quoted substituted values; project refs and replica names are validated before file writes.
- SSO/SCIM: JSON adapter is gated; assertions check issuer/audience/domain/signature/expiry; SCIM tokens are hashed and constant-time verified.
- Backup target endpoint testing: platform-admin-only and not reachable by project developers except through existing random target ids; keep as a hardening item rather than a reportable SSRF finding.

## Test Results

- Backend scoped tests passed for reviewed packages.
- Frontend static checks and Vitest tests passed.
- Frontend production dependency audit reported 0 vulnerabilities.
- Go vulnerability check could not complete because the public vulnerability feed returned 403.
