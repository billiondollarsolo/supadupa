# Supadupa Master Improvement & Gap Closure Plan

**Status:** Active living backlog — Wave 0 complete + in-repo IDs implemented; external labs blocked with remaining commands  
**Date:** 2026-08-01  
**Repo baseline:** `main` @ `b4ef456` (*release: 0.3.0 security refresh*) — synced with `origin/main`  
**Audience:** Human operators + AI implementation agents  
**Product stance:** Early `0.3.x`; Docker Compose is the primary runtime; not hosted-grade v1.

---

## 0. How to use this document

This plan is the **single consolidated backlog** of incomplete work, gaps, debt, and improvements discovered from:

| Source | Role |
|--------|------|
| `README.md` Release Status + Known limitations | Product-facing gaps |
| `docs/known-issues.md`, `docs/index.md` | Ops / MVP limits |
| `docs/security.md`, `docs/backups-recovery.md`, `docs/operations.md`, `docs/kubernetes.md` | Domain-specific remaining work |
| `docs/project-review-remediation-plan.md` | Prior remediation; remaining open items |
| `docs/PRD.md` §5–§8, §13–§15 | Roadmap / feature catalog / deliberate divergences |
| `docs/supabase-compat-validation-2026-06-06.md` + `docs/supabase-compat-test-suite.md` | PARTIAL / SKIP / opt-in validation matrix |
| `docs/reviews/2026-06-13-security-review/` | Security remediation remainder |
| Code inventory (file sizes, untested packages, error swallows, DRY) | Structural debt |
| `go list -m -u`, `frontend/package.json` | Dependency hygiene |
| GitHub | No open issues/PRs at inventory time — **all backlog lives here** |

### Agent rules when executing

1. **Do not invent product features** not already present as APIs, flags, docs, or PRD rows. Prefer closing *proof/gaps* over new greenfield surfaces.
2. **Preserve intentional design choices** unless this plan explicitly overturns them:
   - Broad silent CRUD mega-frameworks were intentionally avoided for auth/audit/rollback clarity.
   - Compliance screens are evidence helpers, never certification claims.
   - Compose remains primary until K8s parity is proven.
3. **Every PR must** keep `go test ./...`, frontend `check`/`build`, and relevant domain guards green; security-sensitive changes need operator notes per `docs/release-note-policy.md`.
4. **Prefer small vertical slices** with validation over multi-week mega-PRs.
5. **Update this plan’s status table** (or a repo copy under `docs/master-improvement-plan.md`) as items complete.

### Priority model

| Priority | Meaning |
|----------|---------|
| **P0** | Data loss, security exploit path, or false “production ready” signal if ignored |
| **P1** | Blocks trustworthy production-like ops or release confidence |
| **P2** | Material feature completeness, hardening, maintainability |
| **P3** | Polish, density, optional enterprise, long-horizon architecture |

### Severity of effort tags

- **S** = hours / small PR  
- **M** = days / multi-file  
- **L** = multi-week / multi-PR  
- **XL** = multi-month program  

---

## 1. Executive summary — what “done” means at three horizons

### H1 — Hosted-grade Compose credibility (next product milestone)

Operators can run multi-project Compose with:

1. Durable off-host recovery **proven** (physical backup + WAL + destructive PITR + failed-upgrade restore).
2. Production defaults that fail closed (`REQUIRE_RECOVERY_READY_*`, durable upgrade backups).
3. No production use of the JSON SSO adapter; SAML path either real or explicitly “not supported.”
4. Clear sizing, limit-enforcement, and edge operational footguns documented + mitigated where cheap.
5. Security/release gates (govulncheck, audit, key smokes) always green on public tags.

### H2 — Enterprise Compose completeness

Feature-flagged surfaces (PITR, replicas, branches, SSO/SCIM, log drains, metering) are **validated**, not only present. Real third-party SMS/CDN where claimed. Registry/DRY debt reduced so new resources are not N-file hand-wires.

### H3 — Kubernetes first-class

Full Supabase data-plane parity on K8s (storage, realtime, functions, pooler, analytics, ingress, Kong auth/transform), auxiliary CRD data-plane reconciliation, multi-tenant isolation under policy-enforcing CNI, telemetry collector, HA operator defaults.

### Deliberate permanent non-goals (PRD §15)

Do **not** treat these as “gaps to close” unless product direction changes:

- Matching hosted Supabase’s AWS multi-region global CDN identity
- SOC 2 / HIPAA *certification* as a product deliverable (controls yes; cert no)
- Managed SLA / support
- Mandatory commercial billing processor

---

## 2. Workstream map (read this first)

```text
WS-A  Recovery & durability proof          [P0]
WS-B  Security hardening & SSO             [P0–P1]
WS-C  Compose operations & resilience      [P1–P2]
WS-D  Kubernetes maturity                  [P1–P2]
WS-E  Data-plane parity & provider truth   [P2]
WS-F  Auth / identity (platform + project) [P1–P2]
WS-G  Admin UI / UX / a11y                 [P2–P3]
WS-H  CLI / MCP / Terraform completeness   [P2]
WS-I  Observability, advisor, compliance   [P2]
WS-J  Multi-region / HA / density          [P1–P3]
WS-K  Code quality, DRY, modularization    [P1–P2]
WS-L  Testing, CI, release gates           [P1–P2]
WS-M  Dependencies & supply chain          [P2]
WS-N  Documentation accuracy & drift       [P2]
```

Sequencing recommendation: **A → B → L (gate) → C → F → E → K → G/H → D → J → M/N** with parallel safe work (K modularization, N docs) anytime.

---

## 3. Inventory tables (exhaustive)

### 3.1 WS-A — Recovery & durability

| ID | Item | Pri | Effort | Evidence | Done looks like | Validation |
|----|------|-----|--------|----------|-----------------|------------|
| A1 | Off-host destructive PITR not proven E2E | P0 | L | README, known-issues, backups-recovery, compat PARTIAL | Real S3/R2/remote MinIO: physical base + WAL range + restore timestamp + SQL marker semantics | `SUPADUPA_COMPAT_DURABLE_BACKUP_TARGET=true` + `PHYSICAL_BACKUP` + `PITR_RESTORE_VALIDATE` with public credentials profile; artifact retention |
| A2 | Failed-upgrade restore needs durable artifacts | P0 | M | README, index, backups-recovery | Failed upgrade → rollback → restore from recovery-ready off-host backup | Upgrade failure inject + `UPGRADE_FAILURE_RESTORE_VALIDATE` + durable target |
| A3 | Default posture often `local-backup-only` | P0 | M | compat validation | Prod install path enables recovery-ready target + physical + WAL | Advisor findings + `production_posture` |
| A4 | Loopback RustFS must never greenwash recovery | P1 | S | backups-recovery, rustfs phase | Keep guards; expand tests if new target types | `20-rustfs-backup-target.sh` + readiness fields |
| A5 | Recovery enforcement flags default off | P1 | S | backups-recovery | Documented prod profile sets both true; optional setup flag | Install docs + smoke with flags |
| A6 | Physical / PITR / durable target validation opt-in only | P1 | M | compat suite flags | Hosted-grade CI job (scheduled/dispatch) runs full profile | GitHub Actions workflow_dispatch matrix |
| A7 | Non-apply / K8s PITR needs external commands | P2 | L | README-legacy commands | Substrate defaults for K8s restore/physical/WAL | Kind or live K8s recovery drill |
| A8 | Control-plane backup ≠ project data plane | P2 | S | backups-recovery | Runbook pairing platform + project + WAL | Docs only or checklist script |
| A9 | Auto-restore on upgrade failure opt-in/destructive | P2 | M | `UPGRADE_FAILURE_AUTO_RESTORE` | Documented; only `restore_state=completed` counts | Disposable project test |
| A10 | Logical restore intentionally skips internal schemas | P2 | S | validation notes | Document when to use PITR vs logical | Docs |

**A1 implementation outline (agent):**

1. Provision real off-host bucket credentials in CI secrets (R2 recommended).
2. Wire scheduled workflow job `compat-hosted-grade` with env block from `docs/supabase-compat-validation-2026-06-06.md` durable profile.
3. Assert: `durable_off_host=true`, `recovery_ready=true`, physical artifact verified, WAL range verified, PITR restore keeps pre-timestamp row / drops post-timestamp row.
4. Store redacted evidence markdown under `docs/reviews/<date>/recovery-proof.md`.
5. Fail release tags if job red unless explicitly waived.

---

### 3.2 WS-B — Security hardening & SSO

| ID | Item | Pri | Effort | Evidence | Done | Validation |
|----|------|-----|--------|----------|------|------------|
| B1 | Platform SSO is JSON adapter, not SAML XML | P0 | XL | security.md, known-issues, security review | Real SAMLResponse parse + XML DSig; or permanently “unsupported in prod” with hard fail | Unit tests for signature attacks; opt-in compat; docs |
| B2 | JSON adapter opt-in flag only | P1 | S | `SUPADUPA_ENABLE_PLATFORM_SSO_JSON_ADAPTER` | Remains false by default; startup warn if enabled | Startup test |
| B3 | Project stack images not digest-locked | P1 | M | security.md | Pin digests in stack release manifests or lockfile | Render tests; upgrade matrix |
| B4 | Compose apply = sensitive Docker access | P1 | L | security.md, remediation plan | Separate apply worker host/VM path documented + optional deploy | Threat model doc + optional compose overlay |
| B5 | Docker proxy is privileged boundary | P1 | M | security.md, docker-proxy tests | Continue allowlist hardening; periodic review of new Docker API surfaces | Expand proxy tests for new endpoints |
| B6 | `randomHex` returns `"change-me"` on CSPRNG failure | P0 | S | compose.go (code scan) | Fail closed with error; never weak secret | Unit test forced failure |
| B7 | Project Vector docker.sock opt-in risk | P2 | S | security.md | Keep default off; prefer platform collector | Docs + regression that default omit mount |
| B8 | DB/pooler public dual-gate remain fail-closed | P2 | S | security.md | No regression of defaults | public-exposure + install smoke |
| B9 | KMS/Vault production path maturity | P2 | L | PRD §14, security.md | Documented external KMS default for prod; rotation runbook | Encryption tests with KMS command provider |
| B10 | MFA seed legacy plaintext migration | P3 | S | security.md | Migration complete path; re-enroll guidance | Persistent store tests |
| B11 | Traefik dashboard footgun | P3 | S | security.md | Defaults stay disabled | Compose hardening check |
| B12 | Legacy password / SCIM hash migration completeness | P2 | M | security.md | Metrics/counts for remaining legacy hashes; force rehash campaign tools | Auth tests |
| B13 | Swallowed rollback/status errors (audit trail gaps) | P1 | M | API handlers, lifecycle | Log + metric on rollback delete failure; don’t ignore status updates on critical paths | Handler tests |
| B14 | Stack manifest resolve errors ignored in places | P1 | S | compose/k8s `_ = ResolveStackRelease...` | Hard fail in apply paths | Unit tests |

**B6 fix sketch:**

```go
// return (hex, error); callers abort provision on error
// never return "change-me"
```

---

### 3.3 WS-C — Compose operations & resilience

| ID | Item | Pri | Effort | Evidence | Done | Validation |
|----|------|-----|--------|----------|------|------------|
| C1 | ~4GB+/full project; analytics OOM | P1 | M | known-issues, install | Stronger preflight; lean defaults messaging; optional auto-disable analytics on low RAM | setup-compose warning tests |
| C2 | Sizes are reservations unless enforce-limits | P1 | S | known-issues | UX already partially done in 0.3.0; clarify telemetry >100% | UI copy + docs |
| C3 | No true project-wide aggregate cgroup | P1 | L | known-issues | Document host accounting; explore systemd/cgroup parent optional | Ops doc |
| C4 | Edge-router-only recreate → ~30s 502 | P2 | M | known-issues | Faster reattach or dual edge / pre-join networks | Chaos script |
| C5 | Resource pressure thrash re-apply → 502 | P2 | M | known-issues | Status check hysteresis / backoff under load | Loadtest notes |
| C6 | `00-platform.yaml` delete footgun | P3 | S | known-issues | Startup rewrite if missing | Unit/smoke |
| C7 | Docker Hub rate limits | P2 | M | remediation plan | CI registry auth/cache | CI config |
| C8 | Apply mode vs render-only clarity | P2 | S | install, security | Install path always states apply status | Docs |
| C9 | Scale-to-zero density | P2 | L | PRD risks | Documented pause behavior; optional auto-pause policy | Lifecycle tests |

---

### 3.4 WS-D — Kubernetes maturity

| ID | Item | Pri | Effort | Evidence | Done | Validation |
|----|------|-----|--------|----------|------|------------|
| D1 | K8s not primary runtime | P1 | XL | index, README | Equal live validation vs Compose for declared GA features | Full parity checklist |
| D2 | Full Supabase K8s data plane incomplete | P1 | XL | kubernetes.md limits | Live storage, realtime, functions, pooler, analytics, ingress, Kong auth/transform | Extended Kind smoke + e2e |
| D3 | Auxiliary CRDs observed-only (`DataPlanePending`) | P1 | L | kubernetes.md, operator | Real reconcile for ProjectConfig, AuthHooks, BranchClone, Replica, Retained | Operator unit + Kind |
| D4 | NetworkPolicy needs policy CNI | P1 | M | kubernetes.md | Document required CNI; optional Kind Calico job | `KIND_ISOLATION_CNI_ENFORCED` |
| D5 | No in-place shared-ns → per-project-ns migration | P2 | L | kubernetes.md | Drain/recreate tool or documented freeze | Runbook |
| D6 | Leader election / metrics default off | P2 | M | kubernetes.md | Prod values profile enables HA operator | Helm template tests |
| D7 | PDB + single replica footgun | P2 | S | kubernetes.md | Guard values schema | Helm check |
| D8 | Bundled meta-DB not strict prod | P2 | S | kubernetes.md | Recommend external Postgres | Docs |
| D9 | Kind core smoke opt-in | P2 | M | scripts | Promote to release gate when pulls stable | CI |
| D10 | Per-service security hardening beyond core | P2 | L | remediation | Per-image writable paths / user / seccomp matrix | Operator tests |
| D11 | K8s project telemetry collector missing | P2 | L | operations.md | Metrics-server/cAdvisor path or external | History API non-empty |
| D12 | Doc drift: known-issues isolation wording | P2 | S | known-issues vs kubernetes.md | Align: isolation implemented; CNI caveat remains | Docs PR |
| D13 | Multigres adoption decision | P3 | L | PRD §14 | ADR: adopt / hybrid / reject | Architecture doc |

---

### 3.5 WS-E — Data-plane parity & provider truth

| ID | Item | Pri | Effort | Evidence | Done | Validation |
|----|------|-----|--------|----------|------|------------|
| E1 | Deep Auth PARTIAL (MFA, real SMS, advanced hooks) | P2 | L | compat validation | Matrix green with opt-ins documented | `22-auth-deep.sh` + real SMS |
| E2 | Deep Storage PARTIAL (external CDN, S3 edges) | P2 | L | validation | Real CDN propagation proof | `23-storage-deep` + CDN provider |
| E3 | Deep Realtime PARTIAL (policy edges, multi-region) | P2 | L | validation | Broader policy matrix | `24-realtime-deep` |
| E4 | Functions geo infra PARTIAL | P2 | XL | validation | True multi-region placement | Multi-host lab |
| E5 | Provider declarations PARTIAL propagation | P2 | L | validation | Distinguish desired-state vs live provider effect in UI/API | Contract tests |
| E6 | Network connections = declarations only | P2 | L | README-legacy | External reconciler or honest “declaration only” UX | Docs + UI badge |
| E7 | Analytics/Iceberg desired-state only | P2 | XL | README-legacy | Real Iceberg provisioning or demote claim | Feature flag honesty |
| E8 | Embeddings / vector buckets declaration+hints | P2 | L | README-legacy | Live pipeline or clear “config only” | Compat |
| E9 | Branch data clone needs command | P2 | M | README-legacy | Default apply-mode dump/restore clone | Branch deep script |
| E10 | Feature flags default off for enterprise surfaces | P2 | S | `defaultPlatformFeatureFlags` | Production profile presets | Settings docs |
| E11 | Official CLI typegen TLS caveat | P2 | S | README | Tunnel default documented; keep wrapper | `09-supabase-cli-*` |
| E12 | OrioleDB preview | P3 | L | README-legacy | Support or hide | Stack catalog |
| E13 | CDN not global hosted edge | P3 | — | PRD §15 | Keep as deliberate divergence | Docs only |

**Honesty principle:** If a resource only writes desired-state artifacts, API/UI must not imply external provider success. Prefer `status: declared` vs `status: applied`.

---

### 3.6 WS-F — Auth / identity

| ID | Item | Pri | Effort | Evidence | Done | Validation |
|----|------|-----|--------|----------|------|------------|
| F1 | Real platform SAML (see B1) | P0 | XL | security | Enterprise IdP login | IdP lab |
| F2 | Platform SCIM completeness / default-off | P2 | M | store flags, scim handlers | SCIM lifecycle tested when enabled | SCIM tests + compat |
| F3 | Project end-user SAML (PRD P2) | P2 | L | PRD §8.2 | Per-project IdP config wired to GoTrue | Auth deep |
| F4 | Platform MFA exists; project MFA deep opt-in | P2 | M | compat | Include MFA in deep Auth release profile | `AUTH_MFA_VALIDATE` |
| F5 | Phone MFA / captcha / Web3 (PRD) | P3 | L | PRD | Config UI + validation where stack supports | Manual/compat |
| F6 | JWT asymmetric key rotation maturity | P2 | M | PRD / connect surface | Rotation UI + zero-downtime dual-key window | API tests |
| F7 | Email templates editor UX | P2 | M | PRD | Template editor if claimed | UI + apply |

---

### 3.7 WS-G — Admin UI / UX / a11y

| ID | Item | Pri | Effort | Evidence | Done | Validation |
|----|------|-----|--------|----------|------|------------|
| G1 | Feature-flag empty states clarity | P2 | M | feature-flags.ts, panels | Never look “broken” when disabled | Vitest / visual |
| G2 | Large god-files: app.tsx, database-panels, settings | P2 | L | line counts | Split by domain panels | Build + smoke |
| G3 | No React error boundary | P2 | S | main.tsx | Root boundary + friendly fallback | Unit |
| G4 | Thin client-side form validation | P2 | M | pages | Shared validators for email/ref/CIDR | Vitest |
| G5 | a11y incomplete (labels, lint) | P2 | M | sparse aria | eslint-plugin-jsx-a11y or axe in CI | a11y CI |
| G6 | Only 3 frontend unit tests | P1 | L | frontend/src | Cover routes, forms, connect masking, flags | Coverage floor |
| G7 | Playwright coverage narrow | P2 | M | e2e | Critical path: create project, connect reveal, backup policy | e2e |
| G8 | Studio multi-project open question | P3 | XL | PRD | ADR: keep deep-link only | ADR |
| G9 | Billing empty until flag | P3 | S | org panels | Clear optional commercialization copy | UI |
| G10 | Theme/responsive polish | P3 | M | CSS | Mobile nav, high-contrast | Manual |
| G11 | Telemetry charts a11y | P3 | S | charts | Summaries for screen readers | Manual |

---

### 3.8 WS-H — CLI / MCP / Terraform

| ID | Item | Pri | Effort | Evidence | Done | Validation |
|----|------|-----|--------|----------|------|------------|
| H1 | Child-resource N-surface hand-wiring | P2 | XL | remediation, registry | Codegen or templates for API/CLI/MCP/TF stubs | Drift guards stay |
| H2 | MCP tools not generated | P2 | L | remediation | Generate from inventory metadata | MCP tests |
| H3 | Terraform ~40 resources, few dedicated tests | P2 | L | terraform/* | Helper-based generation + acceptance suite | TF smoke + per-resource |
| H4 | CLI/MCP/api payload shape duplication | P2 | L | cli.go, mcp/server.go | Shared OpenAPI or generated clients | Contract tests |
| H5 | Secret reveal remains opt-in audited | P2 | S | CLI docs | Keep safe default; improve docs | Compat |
| H6 | Partial inventory surfaces (routes, CDN invalidations, secrets, telemetry) | P3 | M | project_child_resources | Full or explicit permanent omissions | Registry tests |
| H7 | cmd entrypoints untested | P3 | S | cmd/* | Smoke that main wires correctly | Tiny tests |

---

### 3.9 WS-I — Observability / advisor / compliance

| ID | Item | Pri | Effort | Evidence | Done | Validation |
|----|------|-----|--------|----------|------|------------|
| I1 | Compliance never claims certification | P1 | S | operations, README | Copy audit; UI disclaimer | Docs check |
| I2 | Advisor is control-plane metadata only | P2 | L | README-legacy | Add substrate live checks (container health, disk) | Advisor tests |
| I3 | MVP metrics not long-retention | P2 | L | operations | Prometheus/OTel export path | Metrics scrape test |
| I4 | Telemetry 30d in checkpoint — privacy | P2 | M | operations | Export/purge policy + access review notes | Docs + API |
| I5 | production_posture flag default false | P2 | S | store flags | Prod profile enables | Install |
| I6 | Operator Prometheus metrics off by default | P2 | S | kubernetes | Prod values enable | Helm |
| I7 | No structured tracing (OTel) for control plane | P3 | L | — | Optional OTel spans on API/reconcile | Manual |

---

### 3.10 WS-J — Multi-region / HA / density

| ID | Item | Pri | Effort | Evidence | Done | Validation |
|----|------|-----|--------|----------|------|------------|
| J1 | Multi-region operator-provided only | P1 | L | PRD §15 | Host/region model docs + placement APIs honesty | Docs + multi-host lab |
| J2 | True multi-region Functions/Realtime unproven | P1 | XL | validation PARTIAL | Lab evidence | Continuity tests |
| J3 | HA / replica failover on K8s incomplete | P1 | XL | PRD P3, operator pending | Replica data-plane + promote/failover | Kind/e2e |
| J4 | Zero-downtime upgrades Phase 3 | P2 | XL | PRD | Rolling story for K8s; Compose maintenance windows documented | Upgrade matrix + realtime continuity |
| J5 | Hard isolation cost 2–4GB+/project | P2 | M | known-issues | Density targets + lean profiles | Install |
| J6 | PrivateLink/VPC automation | P2 | XL | PRD | Declarations + external reconciler | Integration |
| J7 | Multi-host control plane HA | P3 | XL | — | Leader election for control plane services | Chaos |

---

### 3.11 WS-K — Code quality, DRY, modularization

#### Monster files (split without behavior change)

| File | ~Lines | Split strategy |
|------|--------|----------------|
| `internal/control/store.go` | 11,463 | types.go, store_iface.go, memory_*.go by domain, clones package |
| `internal/api/server_test.go` | 7,975 | auth_test, org_test, project_*_test, backup_test, fakes/testutil |
| `internal/provisioner/compose/compose.go` | 4,285 | render, status, kong, env, replicas, networks |
| `internal/control/persistent_store.go` | 3,417 | checkpoint, normalized load, normalized sync |
| `internal/cli/cli.go` | 3,435 | command groups |
| `internal/mcp/server.go` | 4,393 | tools by domain |
| `internal/provisioner/kubernetes/kubernetes.go` | 2,685 | crd render, apply, env |
| `frontend/src/app.tsx` | 1,511 | shell, palette, nav, account |
| `frontend/src/pages/project/database-panels.tsx` | 2,003 | one file per resource panel |

#### DRY patterns

| Pattern | Location | Preferred remediation |
|---------|----------|----------------------|
| Project-child CRUD handlers | `project_database_*_handlers.go` etc. | Small generics/helpers with **explicit** apply/mask/audit hooks — not silent framework |
| Terraform resource boilerplate | `internal/terraform/*_resource.go` | Codegen from schema metadata |
| Clone helpers explosion | store + persistent_store + compose | Generics / shared clone util |
| Frontend resource panels | database-panels | Shared `ResourceCrudPanel` component with typed props |
| CLI/MCP path duplication | cli + mcp | Generate from OpenAPI or inventory |

#### Untested / thin packages

| Path | Priority |
|------|----------|
| `internal/provisioner/dbbootstrap` | P1 |
| `internal/control/{compliance,sso,traffic,recovery_posture,persistent_encryption}` | P1–P2 |
| `internal/env` | P3 |
| Most Terraform resources | P2 |

#### Other quality

| Item | Pri | Notes |
|------|-----|-------|
| Platform version dual source `0.3.0` | P2 | Add check that `version.go` == `frontend/package.json` |
| Magic stack versions in smokes | P2 | Derive from `DefaultStackReleaseVersion` / catalog |
| No golangci-lint / staticcheck in CI | P2 | Add |
| No coverage reporting | P2 | Optional thresholds after split |
| `go test -race` not default CI | P1 | Add for `api,control,scheduler,operator,compose` |
| No annotated TODOs in code | — | Debt is structural; use this plan as the backlog |

**K-store modularization acceptance:**

1. `go test ./internal/control ./internal/api` green  
2. Registry inventory guards still pass  
3. No public API break for `control.Store`  
4. Diff is mostly moves  

---

### 3.12 WS-L — Testing, CI, release gates

| ID | Item | Pri | Effort | Done | Validation |
|----|------|-----|--------|------|------------|
| L1 | Hosted-grade recovery CI job | P0 | M | Scheduled + dispatch with secrets | Evidence artifact |
| L2 | `go test -race` critical packages in CI | P1 | S | Workflow step | CI |
| L3 | govulncheck never silently skipped on tags | P1 | S | Tag workflow fails on vuln | CI |
| L4 | Compose apply-lifecycle smoke not every PR | P2 | M | Optional heavy job / nightly | Scripts |
| L5 | Terraform smoke not in CI | P2 | S | Add job | Script |
| L6 | Kind core + isolation CNI jobs | P2 | M | Nightly | Scripts |
| L7 | Upgrade matrix + realtime continuity release gate | P2 | M | On release branches | Compat |
| L8 | Frontend coverage floor | P2 | M | Vitest coverage | CI |
| L9 | Single workflow file complexity | P3 | M | Split lint/security/compat | CI maintainability |
| L10 | Final remediation suite skip discipline | P2 | S | Require reasons; publish summary | Script |

**Release gate matrix (proposed):**

| Gate | PR | Nightly | Tag/Release |
|------|----|---------|-------------|
| `go test ./...` | ✓ | ✓ | ✓ |
| `go test -race` critical | ✓ | ✓ | ✓ |
| govulncheck | ✓ | ✓ | ✓ required |
| frontend check/build/audit | ✓ | ✓ | ✓ |
| compose hardening + dockerignore | ✓ | ✓ | ✓ |
| Helm + CRD + Kind basic | ✓ | ✓ | ✓ |
| Kind core Supabase | | ✓ | ✓ |
| Compose local/edge/admin smoke | | ✓ | ✓ |
| TF provider smoke | | ✓ | ✓ |
| Compat core disposable | | ✓ | ✓ |
| Hosted-grade recovery | | optional | ✓ if claiming recovery |
| Upgrade matrix | | optional | ✓ for stack bumps |

---

### 3.13 WS-M — Dependencies & supply chain

| ID | Item | Pri | Effort | Notes |
|----|------|-----|--------|-------|
| M1 | Bump Go indirects / aws-sdk-go-v2 minors | P2 | S | `go get -u` careful; govulncheck |
| M2 | Frontend major track: React 19, TS 6, Zustand 5, lucide 1 | P2 | L | Separate PR; full UI smoke |
| M3 | Keep Radix/TanStack/recharts current on minor | P2 | S | Regular cadence |
| M4 | Project image digest pins (see B3) | P1 | M | Supply chain |
| M5 | Helm/Kind/kubectl pin update cadence | P3 | S | Document quarterly |
| M6 | No ESLint/Prettier today | P3 | M | Optional; a11y plugin if ESLint |
| M7 | Docker Hub auth in CI | P2 | S | Rate limits |
| M8 | `go 1.25` / toolchain `1.26.4` bootstrap docs | P3 | S | CONTRIBUTING |

---

### 3.14 WS-N — Documentation accuracy & drift

| ID | Item | Pri | Effort | Notes |
|----|------|-----|--------|-------|
| N1 | known-issues K8s isolation sentence stale | P2 | S | Align with kubernetes.md CNI caveat |
| N2 | README-legacy auth/stack `latest` examples | P2 | M | Banner + scrub dangerous examples |
| N3 | PRD vs reality (many “P3” items already partially built) | P2 | M | PRD status refresh or “as-built” appendix |
| N4 | Publish this plan into repo `docs/master-improvement-plan.md` | P2 | S | Single living backlog |
| N5 | Operator production profile checklist | P1 | M | One page: flags, targets, secrets, sizing |
| N6 | Security review remaining SAML note | P2 | S | Link B1 |
| N7 | Feature flag catalog for operators | P2 | M | Generate from `defaultPlatformFeatureFlags` |

---

## 4. Feature flags — current defaults (enterprise surfaces off)

From `internal/control/store.go` `defaultPlatformFeatureFlags` (inventory time):

| Flag | Default | Plan action |
|------|---------|-------------|
| `pitr` | false | Enable in prod profile after A1 |
| `preview_branches` | false | Enable after branch deep + clone command |
| `read_replicas` | false | Enable after replica deep + promote |
| `custom_domains` | false | Safe to enable when TLS ready |
| `network_restrictions` | false | Enable with honesty on declaration scope |
| `log_drains` | false | Enable after drain e2e |
| `ai_integrations` | false | Enable when provider path real |
| `usage_metering` / `billing` | false | Optional commercialization |
| `platform_sso_scim` | false | Keep off until B1 |
| `kubernetes_operator` | false | Enable when D2 progress |
| `multi_org` | false (`single_org_mode` true) | Multi-tenant ops profile |
| `database_external_access` | false | Keep fail-closed |
| `production_posture` | false | Enable in prod profile |

---

## 5. Suggested multi-PR program (executable DAG)

### Wave 0 — Correctness hotfixes (1–3 days)

1. **PR:** B6 fail-closed `randomHex` + B14 stack resolve errors  
2. **PR:** B13 log rollback failures on critical create paths  
3. **PR:** N1/N2 doc drift fixes (isolation + legacy banner)  
4. **PR:** L2 race tests on critical packages  

**Validate:** `go test ./...`, compose unit tests, security regressions.

### Wave 1 — Recovery credibility (1–3 weeks)

1. A5/A3 production profile docs + setup flags  
2. A1/A2 durable recovery CI job + evidence  
3. A4/A6 keep loopback honest; promote hosted-grade schedule  
4. N5 production checklist  

**Validate:** Hosted-grade compat profile; release note for recovery posture.

### Wave 2 — Security & identity honesty (2–6 weeks)

1. B1 decision ADR: implement SAML **or** remove prod SSO claims  
2. B3 digest pins for project images  
3. B4 apply-worker architecture design + optional deploy  
4. F2/F4 SCIM/MFA validation in security phase  

### Wave 3 — Maintainability (ongoing parallel)

1. K: split `server_test.go` (mechanical)  
2. K: split `store.go` types first  
3. H3 Terraform codegen spike  
4. G3/G6 frontend error boundary + tests  
5. L5 TF smoke in CI  

### Wave 4 — Data-plane honesty & depth

1. E5/E6/E7 status model for declared vs applied  
2. E1–E3 deepen Auth/Storage/Realtime matrices  
3. E9 branch clone default command  
4. C1 sizing preflight improvements  

### Wave 5 — Kubernetes program (multi-month)

1. D12 docs align  
2. D4 CNI documentation + enforced smoke  
3. D2 expand core → storage → realtime → functions → pooler → analytics  
4. D3 auxiliary CRD data plane  
5. D6/D11 HA + telemetry  
6. Only then flip product messaging to dual primary  

### Wave 6 — Enterprise / HA horizon

1. J* multi-region lab  
2. I3 long-retention observability  
3. M2 frontend major upgrades  
4. D13 Multigres ADR  

---

## 6. Standard validation cookbook (agents must run relevant subset)

### Always (any code change)

```bash
go test ./...
npm --prefix frontend run check
npm --prefix frontend run build
python3 scripts/check-docs-remediation.py
git diff --check
```

### Security-sensitive

```bash
python3 scripts/check-security-regressions.py
scripts/check-runtime-secret-hygiene.sh --fail-on-present  # sterile trees
scripts/check-release-note-policy.sh
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

### Compose / apply

```bash
scripts/check-compose-hardening.py
scripts/check-setup-compose.sh
# with Docker:
scripts/check-compose-local-smoke.sh
scripts/check-compose-apply-lifecycle-smoke.sh
scripts/check-compose-edge-routing-smoke.sh
scripts/check-compose-admin-ui-smoke.sh
```

### Kubernetes

```bash
python3 scripts/check-kubernetes-crds.py
scripts/check-helm-chart.sh
# with Kind:
scripts/check-kubernetes-kind-smoke.sh
SUPADUPA_KIND_SUPABASE_CORE_SMOKE=true scripts/check-kubernetes-kind-smoke.sh
SUPADUPA_KIND_ISOLATION_SMOKE=true scripts/check-kubernetes-kind-smoke.sh
```

### Hosted-grade recovery (needs secrets)

```bash
# See durable profile in docs/supabase-compat-validation-2026-06-06.md
export SUPADUPA_COMPAT_CREATE_PROJECT=true
export SUPADUPA_COMPAT_DURABLE_BACKUP_TARGET=true
# ... durable S3 env ...
export SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true
export SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true
export SUPADUPA_COMPAT_PHYSICAL_BACKUP_VALIDATE=true
export SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE=true
scripts/compat/run.sh
```

### Full local harness

```bash
# Prefer unskipped; document any SUPADUPA_FINAL_SKIP_* with reason
scripts/check-final-remediation-suite.sh
```

---

## 7. Discussion prompts (human decisions before big spends)

These are **product decisions**, not free agent choices:

1. **SSO:** Implement real SAML (XL) vs permanently document “use IdP bridge / not supported”?  
2. **Recovery claim:** When can marketing/docs say “PITR ready”? Only after A1 green?  
3. **K8s timeline:** Invest now or freeze messaging at “alpha scaffold”?  
4. **Codegen:** Accept generated TF/CLI/MCP surfaces, or keep hand-wired explicitness?  
5. **Density:** Lean default profile (no analytics) for small hosts?  
6. **Multigres:** Evaluate as HA/PITR foundation or stay first-party?  
7. **Frontend majors:** React 19 now or after UI test floor exists?  
8. **Repo living doc:** Commit this plan as `docs/master-improvement-plan.md`?

---

## 8. What is already strong (do not re-litigate)

Do **not** spend cycles re-implementing:

- Cookie-session admin auth, origin checks, password hashing upgrade path  
- Docker socket proxy + compose hardening + apply lifecycle smoke  
- Project isolation on Compose networks  
- Registry/inventory drift guards for project-child resources  
- API handler extraction from monolithic `server.go`  
- Kind/Helm platform smoke + core Supabase path (db/kong/meta/auth/rest)  
- Extensive compat suite for core data plane (PASS on many surfaces)  
- Sensitive masking helpers, release-note policy, govulncheck in CI  
- 0.3.0 exact sizing + telemetry history + secret reveal UX  

Debt remaining is **proof, honesty, modularization, and enterprise depth** — not “no security work has been done.”

---

## 9. Tracking template (copy per PR)

```markdown
### Plan item IDs: A1, B6, ...
### Summary
### Non-goals
### Risk / rollback
### Validation commands run
### Operator notes required? (Y/N + link)
### Follow-ups left open
```

---

## 10. Size of backlog (honest)

| Bucket | Approx item count |
|--------|-------------------|
| Explicit inventory rows (A–N) | ~120+ discrete items |
| PRD catalog rows still partial vs hosted parity | dozens (many partially built) |
| Structural debt files | ~10 mega-files |
| Opt-in validation gaps | ~15 major compat flags |

This is intentionally **huge**. Execution should be wave-based; agents must pick **one ID or small ID set** per PR, not the whole plan.

---

## 11. Recommended first human+agent session after approval

1. Decide discussion prompts §7.1–7.4.  
2. Land Wave 0 hotfixes (B6, B14, N1, L2).  
3. Create GitHub project board columns: P0 / P1 / P2 / P3 mapped to IDs.  
4. Commit living copy to `docs/master-improvement-plan.md`.  
5. Schedule hosted-grade recovery proof (A1) as next milestone.

---

## Appendix A — Largest code surfaces (maintainability heat map)

| Path | Lines |
|------|------:|
| `internal/control/store.go` | 11463 |
| `internal/api/server_test.go` | 7975 |
| `internal/provisioner/compose/compose.go` | 4285 |
| `internal/mcp/server.go` | 4393 |
| `internal/cli/cli.go` | 3435 |
| `internal/control/persistent_store.go` | 3417 |
| `internal/provisioner/kubernetes/kubernetes.go` | 2685 |
| `frontend/src/pages/project/database-panels.tsx` | 2003 |
| `internal/control/backup.go` | ~1961 |
| `internal/terraform/client.go` | 1824 |
| `frontend/src/app.tsx` | 1511 |
| `internal/operator/operator.go` | 1425 |
| `frontend/src/types.ts` | 1372 |
| `frontend/src/api.ts` | 1330 |

## Appendix B — Compat PARTIAL areas (quick)

- Recovery parity (durable off-host)  
- Deep Auth (MFA default skip; real SMS opt-in)  
- Deep Storage (external CDN)  
- Deep Realtime (multi-region)  
- Functions geo placement  
- Provider-backed declaration propagation  

## Appendix C — Sources & baseline commit

- Baseline: `b4ef456` release 0.3.0 security refresh  
- Remote: `github.com/billiondollarsolo/supadupa`  
- Open GitHub issues/PRs at inventory: **none** — this document *is* the backlog  

---

*End of master plan. For execution, start at Wave 0 and never expand scope mid-PR without updating IDs.*

---

---

## Implementation status (branch `feat/master-improvement-plan-implementation`)

**Status date:** 2026-08-01 (post-skeptic completion pass)  
**Orchestration trail:** multi-subagent partitions under implementer scratch `subagents/` (docs-ci, stack-resolve, frontend×2, b13, ci-h7, docs-b10) + orchestrator for B1/B6/C6/status.

Legend: **done** | **partial** | **blocked** | **deferred**

### External / lab blockers — remaining validation commands (AC5)

| ID | Status | Reason | Exact remaining command |
|----|--------|--------|-------------------------|
| A1 | blocked | No durable S3/R2 credentials in env | `export SUPADUPA_COMPAT_CREATE_PROJECT=true SUPADUPA_COMPAT_DURABLE_BACKUP_TARGET=true SUPADUPA_COMPAT_DURABLE_S3_ENDPOINT=… SUPADUPA_COMPAT_DURABLE_S3_REGION=auto SUPADUPA_COMPAT_DURABLE_S3_BUCKET=… SUPADUPA_COMPAT_DURABLE_S3_ACCESS_KEY_ID=… SUPADUPA_COMPAT_DURABLE_S3_SECRET_ACCESS_KEY=… SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=true SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=true SUPADUPA_COMPAT_PHYSICAL_BACKUP_VALIDATE=true SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE=true && scripts/compat/run.sh` |
| A2 | blocked | Same as A1 | Same durable env + `SUPADUPA_COMPAT_UPGRADE_FAILURE_TARGETS=<stable> SUPADUPA_UPGRADE_FAILURE_RESTORE_VALIDATE=true` (and optional `SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE=true` on disposable only) via workflow_dispatch or `scripts/compat/run.sh` |
| A6 | blocked | Needs durable secrets for hosted-grade CI job | GitHub Actions `compat.yml` workflow_dispatch with `durable_backup_target=true physical_backup_validate=true pitr_restore_validate=true create_project=true` after repo secrets are set |
| L1 | blocked | Hosted-grade recovery CI needs secrets | Same as A6; evidence under Actions artifacts + `docs/reviews/<date>/recovery-proof.md` |
| B1 (real SAML product) | deferred | Hard-fail unsupported path **done**; full XML/DSig IdP productization not built | Implement real SAML parser then: unit tests for signature transforms + `SUPADUPA_ENABLE_PLATFORM_SSO_JSON_ADAPTER` removed; IdP lab: `go test ./internal/control ./internal/api -run SSO` and live ACS against a test IdP |
| E1 | blocked | Real SMS / MFA deep need credentials | `SUPADUPA_COMPAT_AUTH_MFA_VALIDATE=true scripts/compat/22-auth-deep.sh` and/or `SUPADUPA_COMPAT_AUTH_REAL_SMS_VALIDATE=true SUPADUPA_COMPAT_SMS_PHONE=… SUPADUPA_COMPAT_SMS_OTP_COMMAND=… scripts/compat/run.sh` |
| E2 | blocked | External CDN provider | Configure real CDN credentials then `scripts/compat/23-storage-deep.sh` with CDN propagation asserts |
| E3 | blocked | Multi-region Realtime | Multi-host lab + `scripts/compat/24-realtime-deep.sh` with region matrix |
| E4 | blocked | Geo Functions placement | Multi-host lab + functions deep with true multi-region placement |
| E7–E9 E12 | deferred | Iceberg/embeddings/clone/orioledb depth | Feature-specific: branch clone `SUPADUPA_BRANCH_CLONE_COMMAND=…` + `SUPADUPA_COMPAT_BRANCH_VALIDATE=true scripts/compat/29-branches-deep.sh` |
| J1–J7 | blocked | Multi-host / HA lab not available | Provision ≥2 hosts, register capacity, place projects per region; run `scripts/compat/26-replicas-deep.sh` with promote/failover flags and document failover RTO |
| D1–D3 D5–D11 D13 | deferred | K8s program multi-month | Expand Kind: `SUPADUPA_KIND_SUPABASE_CORE_SMOKE=true SUPADUPA_KIND_ISOLATION_SMOKE=true scripts/check-kubernetes-kind-smoke.sh` then add storage/realtime/functions phases |

### WS-A Recovery

| ID | Status | Notes |
|----|--------|-------|
| A1 | blocked | See external table |
| A2 | blocked | See external table |
| A3 | done | `docs/production-profile.md` recovery-ready posture |
| A4 | done | Loopback honesty retained + documented |
| A5 | done | Production profile sets REQUIRE_* flags |
| A6 | blocked | See external table |
| A7 | deferred | K8s PITR substrate defaults | Remaining: implement `SUPADUPA_PITR_RESTORE_COMMAND` / physical/WAL defaults for k8s provisioner; validate with Kind + disposable project restore. |
| A8 | done | backups-recovery control-plane vs project section |
| A9 | done | Opt-in destructive auto-restore documented/guarded (no product claim change) |
| A10 | done | Logical vs PITR schema note |

### WS-B Security

| ID | Status | Notes |
|----|--------|-------|
| B1 | done | Production hard-fail: JSON adapter requires `SUPADUPA_ALLOW_DEV_SECRETS`; else startup error (`enforcePlatformSSOJSONAdapterPolicy`). Real SAML productization remains deferred (external table). |
| B2 | done | Adapter default off; warn only under allow-dev |
| B3 | done | Built-in releases pin multi-arch digests; compose/k8s use ImageRef; unit + compose render tests |
| B4 | deferred | Separate apply worker host/VM | Remaining: add deploy overlay + docs for isolated apply worker; validate with `scripts/check-compose-apply-lifecycle-smoke.sh` against remote DOCKER_HOST. |
| B5 | done | Existing proxy tests + allowlist retained as current boundary |
| B6 | done | Fail-closed randomHex compose+store + unit tests |
| B7 | done | Vector docker.sock default off documented |
| B8 | done | Dual-gate DB ingress fail-closed |
| B9 | deferred | External KMS as default prod path | Remaining: production-profile default `SUPADUPA_KMS_PROVIDER`; `go test ./internal/control -run Encryption`. |
| B10 | done | Legacy MFA plaintext load counter + metric + re-enroll docs |
| B11 | done | Traefik dashboard defaults disabled |
| B12 | done | Legacy password/SCIM verify counters + metrics + docs |
| B13 | done | logRollbackError on schema + auth-hook restore + cron/queue/webhook/role/storage; `supadupa_rollback_failures_total` metric |
| B14 | done | Stack resolve fail-closed |

### WS-C Compose ops

| ID | Status | Notes |
|----|--------|-------|
| C1 | done | Documented sizing / analytics OOM (known-issues + production-profile) |
| C2 | done | Reservations vs enforce-limits documented |
| C3 | done | Host capacity accounting documented in operations.md + known-issues (Compose has no project-wide aggregate cgroup; enforce-limits + host sizing guidance) |
| C4 | deferred | Edge-router recreate 502 window | Remaining: dual edge or pre-join networks; chaos: recreate only edge-router and measure attach time <5s. |
| C5 | deferred | Status thrash under resource pressure | Remaining: add reconcile backoff hysteresis; loadtest assert no compose re-apply storm. |
| C6 | done | `control.EnsurePlatformRouteFile` + startup rewrite + tests; known-issues note |
| C7 | deferred | Docker Hub rate limits | Remaining: add CI registry login secrets + `docker login` before image builds in compat.yml. |
| C8 | done | Apply mode honesty documented |
| C9 | deferred | Auto scale-to-zero policy | Remaining: optional idle auto-pause scheduler; lifecycle tests for pause after idle. |

### WS-D Kubernetes

| ID | Status | Notes |
|----|--------|-------|
| D1–D3 D5–D6 D9–D11 D13 | deferred | See external table |
| D4 | done | CNI caveat documented known-issues + kubernetes |
| D7 | done | Helm PDB fails closed when replicaCount≤1; schema if/then |
| D8 | done | External meta-DB recommendation in kubernetes.md |
| D12 | done | Isolation wording aligned |

### WS-E Data-plane

| ID | Status | Notes |
|----|--------|-------|
| E1–E4 E7–E9 E12 | blocked/deferred | External table |
| E5–E6 | done | Honesty via production-profile + feature-flags (declaration surfaces) |
| E10 | done | Full 22-flag catalog |
| E11 | done | Typegen caveat documented |
| E13 | done | Deliberate CDN divergence |

### WS-F Auth

| ID | Status | Notes |
|----|--------|-------|
| F1 | done | Same as B1 hard-fail unsupported prod path |
| F2–F7 | deferred | SCIM/MFA deep / project SAML | Remaining: IdP/SMS lab; `SUPADUPA_COMPAT_AUTH_MFA_VALIDATE=true` + SCIM token configured security phase. |

### WS-G UI

| ID | Status | Notes |
|----|--------|-------|
| G1 | done | Billing/PITR empty states mention feature flags |
| G2 | deferred | Split app.tsx / database-panels | Remaining: mechanical file splits; `npm run check && npm run build`. |
| G3 | done | ErrorBoundary |
| G4 | done | email/ref/CIDR validators wired into login, create project, database ingress |
| G5 | done | eslint + eslint-plugin-jsx-a11y (`npm run a11y`) wired in CI local-checks |
| G6 | done | Expanded vitest (validators + boundary) |
| G7–G11 | deferred | Broader Playwright + Studio | Remaining: expand `frontend/tests/e2e`; `npm run browser-smoke`. |

### WS-H Integrations

| ID | Status | Notes |
|----|--------|-------|
| H1–H4 H6 | deferred | TF/CLI/MCP codegen | Remaining: inventory-driven codegen spike; `go test ./internal/terraform ./internal/cli ./internal/mcp` + provider smoke. |
| H5 | done | Reveal opt-in audited |
| H7 | done | cmd cli/mcp/terraform-provider smoke tests |

### WS-I Observability

| ID | Status | Notes |
|----|--------|-------|
| I1 | done | Compliance certification disclaimer |
| I2–I4 I6 I7 | deferred | Advisor depth / OTel / long retention | Remaining: substrate health checks + optional OTel exporter; scrape `/metrics` after enable. |
| I5 | done | production_posture in flags + profile |

### WS-J Multi-region

| ID | Status | Notes |
|----|--------|-------|
| J1–J7 | blocked | External table |

### WS-K Quality

| ID | Status | Notes |
|----|--------|-------|
| Mega-file splits | deferred | store.go / server_test.go splits | Remaining: mechanical package splits; `go test ./internal/control ./internal/api`. |
| Dual version + race + randomHex + stack resolve | done | |

### WS-L CI

| ID | Status | Notes |
|----|--------|-------|
| L1 | blocked | External table |
| L2 | done | race critical packages |
| L3 | done | `govulncheck-required` job on tags v* / release/** |
| L4 | deferred | PR compose apply smokes | Remaining: optional heavy job running `scripts/check-compose-local-smoke.sh` on schedule. |
| L5 | done | terraform-provider-smoke job (schedule/dispatch/tags) |
| L6 | deferred | Kind core+isolation nightly | Remaining: schedule `SUPADUPA_KIND_SUPABASE_CORE_SMOKE=true SUPADUPA_KIND_ISOLATION_SMOKE=true scripts/check-kubernetes-kind-smoke.sh`. |
| L7 | deferred | Upgrade matrix release gate | Remaining: release workflow with `SUPADUPA_COMPAT_UPGRADE_MATRIX=true scripts/compat/run.sh`. |
| L8 | done | `npm run coverage` with vitest v8 thresholds (lib/components floors) in CI |
| L9 | deferred | Split CI workflows | Remaining: extract lint/security/compat workflows from monoworkflow. |
| L10 | done | skip reason self-check in final suite |

### WS-M Dependencies

| ID | Status | Notes |
|----|--------|-------|
| M1 | done | x/crypto + aws-sdk-go-v2 minor bumps |
| M2–M3 M5–M8 | deferred | Frontend major upgrades | Remaining: React 19 / TS 6 / Zustand 5 PR; `npm run check && npm run build && npm run a11y && npm run browser-smoke`. |
| M4 | done | Project stack image digests via B3 stack release digests |

### WS-N Docs

| ID | Status | Notes |
|----|--------|-------|
| N1 N2 N4 N5 N6 N7 | done | |
| N3 | done | PRD as-built appendix |

### Wave completion

| Wave | Status |
|------|--------|
| Wave 0 | **done** including B13 complete (log+metric on critical create rollbacks) |
| Wave 1 | **partial** — docs/profile done; A1/A2/L1 blocked on secrets |
| Wave 2 | **done** for honesty path (B1 hard-fail + B2); real SAML product deferred |
| Waves 3–6 | deferred structural / lab programs |

