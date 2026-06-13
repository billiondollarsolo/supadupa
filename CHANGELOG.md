# Changelog

## 0.2.0 - 2026-06-13

This release hardens Supadupa's control plane, deployment path, and validation
suite after a repository-wide security and operational review.

### Security

- Moved browser admin sessions to HttpOnly cookies and added origin checks for
  cookie-authenticated mutations.
- Added platform admin token-version invalidation so privileged sessions stop
  working after user demotion, deletion, password, email, role, or MFA changes.
- Hardened the Docker apply path with a scoped Docker API proxy instead of a raw
  Docker socket mount in the control-plane container.
- Added security regression checks for stale admin tokens, sensitive values, and
  local runtime secret hygiene.

### Deployment

- Added VPS/edge deployment validation for public Traefik routing, Cloudflare
  DNS-01 certificates, and wildcard TLS domains.
- Platform TLS routes now request the control-plane wildcard certificate, such
  as `*.supadupa.brotechlabs.com`, instead of only exact admin/API host
  certificates.
- Project route rendering continues to request the apps wildcard certificate,
  such as `*.apps.supadupa.brotechlabs.com`, when project routes are created.
- Improved Compose setup validation, runtime artifact hygiene checks, and
  deployment documentation for local, offline, and VPS modes.

### Reliability

- Added metadata migration checksum validation and the `0043_user_token_version`
  migration for existing installs.
- Expanded backup, restore, PITR, upgrade, and compatibility validation coverage.
- Added route registration drift checks and split API route handlers into focused
  modules with regression coverage.

### Validation

- Validated Go tests, frontend static/tests/build, npm production audits,
  Compose config rendering, setup checks, security regression checks, and live
  public HTTPS login/API smoke tests.
- `govulncheck` could not be completed from the current host because
  `vuln.go.dev` and related Google Go endpoints return `403 Forbidden` to this
  environment. Run `govulncheck ./...` from CI or another network before a
  public release tag.
