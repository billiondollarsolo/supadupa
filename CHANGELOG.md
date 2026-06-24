# Changelog

## 0.3.0 - 2026-06-24

This release finishes the current security refresh and moves the local Compose
path toward a realistic operator workflow. It replaces user-facing size presets
with exact resource reservations, adds retained project telemetry history,
fixes secret reveal/rotation behavior, and updates the UI, API, CLI, MCP,
Terraform, Compose, Helm, operator, and smoke-script surfaces together.

### Project Sizing And Runtime Limits

- Replaced project create/resize size presets with exact CPU cores, RAM MB, and
  disk GB. Omitted values now resolve to the recommended reservation for the
  selected stack profile and enabled services.
- Split stack selection from sizing. Operators choose `full`, `essential`, or
  `orioledb`, then can toggle individual services before accepting or overriding
  the computed recommendation.
- Added startup minimums and recommended defaults with headroom: 20% CPU, 25%
  RAM, and 20% disk, with RAM and disk rounded upward.
- Added optional runtime limit enforcement. When enabled, Supadupa distributes
  the selected CPU/RAM budget across all enabled services and writes
  per-container Docker Compose limits.
- Kept "no limits" behavior explicit: projects still reserve resources for
  quota, placement, and dashboards, but containers can use available host
  capacity when hard limits are disabled.
- Enforced minimum sizing on create and resize so users cannot enable runtime
  limits below the computed startup floor.

### Telemetry And Capacity

- Added 30-day project telemetry history. Raw samples are retained for 24 hours,
  then compacted into five-minute rollups.
- Added stale/future-sample protections so retained telemetry does not show
  misleading latest data outside the supported window.
- Updated overview cards and telemetry graphs to compare CPU, memory, and disk
  usage against the project's allotted reservation instead of host totals alone.
- Added project telemetry history access through the Admin UI, Management API,
  CLI, and MCP server.
- Added CLI support for retained history through
  `metrics --ref <ref> --history --range <range> --step <step>`.

### Secrets And Security

- Fixed the Connect keys experience so secret handles such as
  `secret://projects/<ref>/anon_key` are not presented as actual keys.
- Added reveal/copy protections for sensitive project keys and connection
  values.
- Added project secret rotation and runtime synchronization coverage.
- Tightened session/auth checks, route authorization coverage, project-scoped
  access checks, runtime secret validation, and local deployment defaults.
- Added migration/default handling for custom resource sizing so fresh installs
  and existing metadata use the same project sizing model.

### Admin UI And Operations

- Updated project creation to show stack profiles, service selection, available
  host capacity, computed minimums, recommended defaults, exact CPU/RAM/disk
  inputs, limit enforcement, and explicit no-limit behavior.
- Updated resize to use the same exact CPU/RAM/disk model as create and to
  invalidate live metrics/history after a successful resize.
- Updated project telemetry charts with range controls, retained history, stale
  data warnings, and reservation-aware usage percentages.
- Updated project operations so forward upgrades show only newer stack releases;
  rollback metadata is no longer displayed as an upgrade target.
- Updated Connect, overview, settings, project config, and side-panel surfaces
  to match the new resource, secret, telemetry, and operations behavior.

### Automation And Integrations

- Added a real CLI `help` command and `--help`/`-h` handling.
- Updated CLI project create/scale to accept exact CPU/RAM/disk and
  `--enforce-limits`.
- Added MCP support for exact project sizing and
  `supadupa_get_project_telemetry_history`.
- Updated Terraform/OpenTofu provider surfaces for custom resource sizing and
  limit enforcement.
- Updated Compose rendering to emit per-service limits when enforcement is on
  and to keep limits out of generated Compose when enforcement is off.
- Updated Helm/operator schemas and Kubernetes manifests so the alpha
  Kubernetes path understands the same resource fields, even though Compose
  remains the supported runtime path.
- Updated local setup, smoke, and compatibility scripts so they use the custom
  sizing model and current project defaults.

### Validation

- Expanded API, control-plane, Compose, Kubernetes renderer, operator, CLI, MCP,
  Terraform, and frontend regression tests around resource sizing, telemetry
  history, secret rotation, authz, upgrade filtering, and integration drift.
- Re-ran frontend static checks/build, Go tests, shell syntax checks, and
  `git diff --check` before preparing the release.

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
