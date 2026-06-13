# Security Remediation Implementation Status

Date: 2026-06-13

## Implemented

- Project logical and PITR restores now require project admin authority and exact project-bound confirmation text.
- Restore UI/API, CLI, MCP, and compatibility validation calls send confirmation text, and restore audit events record restore type plus confirmation presence.
- Platform admin authorization now checks current user state and token version before privileged actions.
- User role, password, email, MFA, and deletion paths invalidate stale platform-admin tokens through token-version changes.
- Normalized MFA TOTP seed persistence now encrypts confirmed and pending seeds, while legacy plaintext values remain readable for migration-on-next-sync behavior.
- Route-local authorization helpers fail closed without claims unless an explicit auth-disabled bypass context is present.
- Backup storage targets now surface network warnings for loopback, private, link-local, and metadata endpoints.
- Compose database and pooler edge binds now default to loopback; public binding requires `--db-public-bind` or explicit env values.
- Runtime secret hygiene and security regression scripts were added and wired into CI/final validation.
- Disposable local development `.env`, `runtime/`, backup, and log artifacts were removed after owner confirmation that this workspace state was development-only.
- New security-focused API tests were split into `internal/api/security_remediation_test.go` instead of expanding the monolithic API test file.

## Validation Run

Passed:

```sh
go test ./...
npm --prefix frontend run check
npm --prefix frontend run build
npm --prefix frontend audit --omit=dev
npm --prefix scripts/compat audit --omit=dev
python3 scripts/check-docs-remediation.py
python3 scripts/check-security-regressions.py
scripts/check-setup-compose.sh
scripts/check-runtime-secret-hygiene.sh --fail-on-present
scripts/check-runtime-secret-hygiene.sh --markdown-report docs/reviews/2026-06-13-security-review/local-runtime-artifact-inventory.md
scripts/check-dockerignore.sh
scripts/check-compose-hardening.py
```

Blocked by external service response:

```sh
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Result: `vuln.go.dev` returned HTTP 403 while fetching the vulnerability database.

## Remaining Review Points

- Local ignored runtime artifacts were removed as disposable development state. [local-runtime-artifact-inventory.md](./local-runtime-artifact-inventory.md) records the current zero-count inventory.
- The broad maintainability task was handled conservatively for this remediation pass by splitting the new security remediation API tests out of `server_test.go`; larger ownership-boundary splits should continue incrementally in separate low-risk PRs.
- Full production SAML XML support remains future work; current docs describe the JSON adapter gate and limitations.
