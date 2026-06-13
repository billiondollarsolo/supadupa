# Reviewed Surfaces

| Surface | Risk Area | Outcome | Notes |
| --- | --- | --- | --- |
| `internal/api/project_backup_recovery_handlers.go` | Destructive project restore authorization | Reported | Developer-role users can trigger logical and PITR restore. |
| `internal/api/auth_handlers.go`, `internal/api/http_helpers.go`, `internal/api/authz_helpers.go`, `internal/api/user_handlers.go` | Authentication token revocation and platform-admin authorization | Reported | Role-bearing 24-hour token remains authoritative after demotion/deletion. |
| `internal/control/store.go`, `internal/control/persistent_store.go` | MFA seed persistence | Reported | Normalized users rows store TOTP seeds without application-level encryption. |
| `frontend/src/api.ts`, `frontend/src/lib/auth-session.ts`, `frontend/src/app.tsx`, `frontend/scripts/check-static.mjs` | Browser token handling, XSS sinks, URL construction | No issue found | No token storage or HTML sink found; static checks and frontend tests passed. |
| `cmd/supadupa-docker-proxy/main.go` | Docker daemon mediation | No issue found | Allowlist, label validation, privileged mode rejection, namespace checks, and bind-mount limits reviewed. |
| `internal/provisioner/compose/compose.go` | Compose generation, file writes, command execution | No issue found | Project refs validated; commands use argv arrays or quoted operator templates. |
| `internal/provisioner/kubernetes/kubernetes.go`, `internal/operator/isolation.go`, `charts/supadupa` | Kubernetes rendering, network policy, RBAC | No issue found | Rendering and network isolation reviewed; no reachable lower-privilege bypass found. |
| `internal/control/sso.go`, `internal/api/*sso*`, SCIM routes | SSO/SCIM assertion and token handling | No issue found | JSON adapter is gated; validation and replay controls reviewed. Production SAML support remains a readiness gap. |
| `internal/control/backup.go`, `internal/api/platform_backup_handlers.go` | Backup targets, platform restore, S3 endpoint testing | Rejected | Platform-admin-only backup target testing did not produce a lower-privilege SSRF finding; still recommended as hardening. |
| `deploy/compose.yaml`, `scripts/setup-compose.sh` | Deployment defaults and port exposure | Needs follow-up | Direct Postgres/pooler exposure defaults deserve explicit production hardening, even though routing config gates project DB ingress. |
| `runtime/` ignored local files | Local secret hygiene | Needs follow-up | Ignored runtime/backups contain live-looking generated secrets, control-plane backup data, and ACME material in this workspace. Not tracked in git, but should be purged/rotated. |
| Large implementation files | Maintainability and reviewability | Needs follow-up | `internal/control/store.go` is 10703 lines; several API, MCP, CLI, provisioning, and frontend files exceed 1000 lines. |
