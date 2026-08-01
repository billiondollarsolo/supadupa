# supadupa - Product Requirements Document

|               |                                                                                                               |
|---------------|---------------------------------------------------------------------------------------------------------------|
| **Product**   | supadupa - self-hosted, enterprise-grade Supabase platform                                                    |
| **Status**    | Draft v0.2 (feature-complete parity edition)                                                                  |
| **Date**      | 2026-06-04                                                                                                    |
| **Owner**     | TBD                                                                                                           |
| **Target**    | MVP: Docker Compose + full Admin UI; Later: Kubernetes operator                                               |
| **Parity goal** | Functional parity with the entire hosted Supabase feature surface (79 catalog features across 8 product areas) |

## 1. Summary

supadupa is a self-hosted control plane that provisions, manages, and operates many isolated Supabase stacks ("projects") from a single admin interface, replacing hosted Supabase for teams that need full control over their data and infrastructure.

The Supabase data plane (Postgres + GoTrue/Auth, PostgREST, pg_graphql, Realtime, Storage, imgproxy, Kong, Studio, Supavisor, Edge Runtime, Logflare, Vector, and the Postgres extension ecosystem) is open source and is treated here as a dependency, not something we build. supadupa builds the control plane and platform layer that Supabase keeps proprietary.

The single most important framing for this document: of hosted Supabase's ~79 catalog features, roughly two-thirds are data-plane features already present in the upstream stack. For those, supadupa's job is to enable, configure, and surface them, not reimplement them. The remaining third are platform/control-plane features supadupa must build. Section 8 enumerates all 79 with that classification so there are no gaps.

Self-hosted Supabase only ever mimics a single project, and Studio cannot manage multiple organizations or projects. supadupa exists to close exactly that gap, with hard tenant isolation (one full dedicated stack per project) and the full enterprise feature set.

## 2. Goals and non-goals

### Goals

- Provision and operate fully isolated Supabase stacks on demand from an API and admin UI.
- Present many projects under an org -> project hierarchy through one dashboard.
- Achieve functional parity with the complete hosted Supabase feature surface (Section 8), with explicit, justified divergences listed in Section 15.
- Ship an MVP on Docker Compose, architected so the move to Kubernetes is a backend swap, not a rewrite.
- Hard isolation by default: one dedicated stack per project.

### Non-goals

- Forking or patching upstream Supabase service images. We orchestrate them.
- Operating a public multi-region SaaS. supadupa is operator-deployed.
- Rebuilding features that the upstream stack already provides. We enable them.
- Re-implementing Studio's data tooling. We deep-link to each project's own Studio.

## 3. Users and personas

| Persona | Needs |
|---------|-------|
| Platform operator | Deploy supadupa, manage hosts/capacity, upgrade stacks, run backups/restores, monitor the fleet, manage compliance. |
| Org admin | Create/manage projects, manage members and roles, set quotas, configure projects. |
| Developer | Use a project: connection details, keys, Studio, edge functions, logs; self-serve pause/restore/branch. |
| Security/compliance | Audit logs, access reviews, key rotation, network restrictions, data-residency assurances. |

## 4. Architecture overview

Four layers, with a pluggable orchestration substrate.

### 4.0 Naming and runtime terminology

- **supadupa** is the whole product/platform: the admin UI, Management API, provisioners, metadata database, edge routing, CLIs, MCP/Terraform integrations, and all managed project stacks.
- **supadupavisor** is the deployed control-plane service/container. It owns the Management API and reconciliation loop, persists desired state in the meta DB, and calls the active provisioner to converge project infrastructure. This name is intentionally distinct from upstream **Supavisor**, which remains the per-project Postgres pooler inside each Supabase data-plane stack.
- **Project stack** means one isolated Supabase data plane provisioned by supadupavisor. In Compose, a project stack is a rendered Compose project with its own containers, Docker network, volumes, env, secrets, and routes. In Kubernetes, the same logical project stack maps to a namespace or equivalent isolation boundary with project-scoped Pods, Services, Secrets, ConfigMaps, volumes, and Ingress/routes.
- **Provisioner** remains the code abstraction (`ComposeProvisioner`, later `KubernetesProvisioner`). supadupavisor invokes a provisioner; business logic must not call Docker or Kubernetes directly.

Suggested container/resource naming:

```txt
supadupa-supadupavisor-1
supadupa-meta-db-1
supadupa-admin-ui-1
supadupa-edge-router-1

supadupa-alpha-db-1
supadupa-alpha-kong-1
supadupa-alpha-auth-1
supadupa-alpha-supavisor-1
supadupa-alpha-studio-1
```

1. **Control plane (the brain).** A Go service exposing a REST Management API, backed by its own dedicated Postgres ("meta DB"). Stores orgs, projects, users, roles, quotas, secret references, and the declarative desired state of every project. Runs a reconciliation loop converging desired -> actual.
2. **Provisioner abstraction.** All orchestration goes through one interface so Compose (now) and Kubernetes (later) are interchangeable. No business logic ever shells out to `docker compose` directly.

   ```go
   type Provisioner interface {
       Create(ctx context.Context, spec ProjectSpec) error
       Destroy(ctx context.Context, ref string) error
       Status(ctx context.Context, ref string) (ProjectStatus, error)
       Upgrade(ctx context.Context, ref, version string) error
       Pause(ctx context.Context, ref string) error
       Resume(ctx context.Context, ref string) error
       // Phase 2+
       Scale(ctx context.Context, ref string, spec ProjectSpec) error
       AddReplica(ctx context.Context, ref string, opts ReplicaOpts) error // Phase 3
   }
   ```

   MVP: `ComposeProvisioner`; Phase 3: `KubernetesProvisioner` (operator + `Project` CRD)

3. **Data plane (per project).** One full, isolated Supabase stack per project: own Postgres volume, own JWT secret/signing keys, own derived keys, own Docker network. Rendered from a template; never hand-edited.
4. **Edge routing.** A single front reverse proxy (Traefik/Caddy) routes `{ref}.<domain>` to each project's Kong over a shared ingress network, with automatic TLS. Maps cleanly onto a Kubernetes Ingress + Service per namespace later. No per-service host ports.

### Locked decisions

- **Substrate:** Docker Compose (MVP) -> Kubernetes (Phase 3), behind the `Provisioner` abstraction.
- **Isolation:** Hard - full dedicated stack per project.
- **Control-plane language:** Go (operator ecosystem is Go-native; the Kubernetes reconciler is the same logic against the Kubernetes API).

## 5. Roadmap and phasing

| Phase | Name | Theme | Substrate |
|-------|------|-------|-----------|
| P1 | MVP | Provision + operate isolated projects; full admin UI; surface everything the stack already provides | Docker Compose |
| P2 | Compose-complete | Operationally credible enterprise on a single/few hosts; configuration parity | Docker Compose |
| P3 | K8s-enterprise | Scale, HA, multi-node, and the features that need real orchestration | Kubernetes operator |

Phase key used in Section 8: P1 = MVP, P2 = Compose-complete, P3 = K8s-enterprise.
Responsibility key: Stack = ships upstream, enable/surface only; Config = in stack, supadupa must expose configuration UI/API; Build = supadupa control-plane must implement.

## 6. MVP scope (Phase 1)

End-to-end project lifecycle with hard isolation, driven entirely by the control plane and surfaced in the admin UI. On provisioning, every Stack-class feature in Section 8 is immediately available to that project (REST, GraphQL, Auth, Realtime, Storage, Functions runtime, pgvector, cron, queues, FDW, Vault, extensions, Supavisor, full Studio via deep-link). MVP build work is the orchestration around them.

- Control plane: Management API + meta-DB schema; `ComposeProvisioner` (render template + per-project `.env`, `docker compose -p <ref> up -d`); reconciliation loop with drift detection; per-project secret + key generation; lifecycle (create/pause/resume/destroy).
- Routing: Traefik front proxy, wildcard DNS + auto TLS, dynamic registration.
- Admin UI (Section 7): auth, org/project list + detail, create-project wizard, full Connect surface (Section 9), Studio deep-link, log tail, fleet dashboard, daily backups.

Deferred past MVP: all P2/P3 items in Section 8 (PITR, config UIs, read replicas, HA, SSO/SCIM, branching, metering, etc.).

## 7. Admin UI specification

### 7.1 Stack

Vite 5+; React + TypeScript; Tailwind CSS v4 (CSS-first); Linear-inspired dark aesthetic; TanStack Query (server state) + Zustand (UI state); TanStack Router; Lucide icons.

### 7.2 Tailwind v4 setup

Tailwind v4 is CSS-first. Config lives in CSS via `@theme`, not `tailwind.config.js`. Use the official Vite plugin.

```ts
// vite.config.ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({ plugins: [react(), tailwindcss()] });
```

```css
/* app.css */
@import "tailwindcss";

@theme {
  /* design tokens from 7.3 as CSS custom properties */
}
```

### 7.3 Design system - Linear aesthetic

Restraint executed precisely: near-black surfaces, hairline borders, muted grayscale text with one saturated accent, tight typography, fast subtle motion. Density over whitespace; keyboard over mouse.

| Token | Value | Use |
|-------|-------|-----|
| `--color-bg` | `#08090a` | App background |
| `--color-surface` | `#0f1011` | Panels, cards |
| `--color-surface-2` | `#16181a` | Elevated/hover |
| `--color-border` | `#1f2123` | Hairline borders (1px) |
| `--color-border-strong` | `#2b2e31` | Focus/active |
| `--color-text` | `#f7f8f8` | Primary text |
| `--color-text-muted` | `#8a8f98` | Secondary |
| `--color-text-faint` | `#62666d` | Tertiary/labels |
| `--color-accent` | `#5e6ad2` | Primary action / brand |
| `--color-accent-hover` | `#6872e5` | Accent hover |
| `--color-success` | `#4cb782` | Healthy |
| `--color-warning` | `#f2c94c` | Degraded |
| `--color-danger` | `#eb5757` | Errors / destructive |

- **Type:** Inter / Inter Display, base 13-14px, line-height ~1.5. Scale (px): 11 (caps labels), 13 (body), 14 (UI), 16 (section), 20/24 (page). Slight negative tracking on large sizes; uppercase + tracked-out for tiny labels.
- **Geometry:** radius 6px (controls) / 8px (cards) / 10-12px (modals). 1px hairlines; elevation from borders + low-opacity shadow (`0 1px 2px rgba(0,0,0,.3)`).
- **Motion:** 120-160ms ease-out; one orchestrated staggered reveal on load. Nothing bouncy.
- **Spacing:** 4 / 8 / 12 / 16 / 24 / 32.
- **Signature:** Command palette (Cmd/Ctrl-K) first-class; keyboard shortcuts on all primary actions; dense tables with inline status pills; bottom-right toasts; typed confirmation for destructive actions.
- Dark-only for MVP; light theme is a P2 nicety.

### 7.4 Information architecture / screens

| Screen | Contents |
|--------|----------|
| Login | Platform auth (SSO/SCIM in P3). |
| Fleet dashboard | Project count, aggregate health, host CPU/RAM/disk, recent events. |
| Projects list | Table: name, ref, org, status pill, host/region, version, created; filter + Cmd-K. |
| Create project | Wizard: name -> org -> host -> stack profile (essential/full) -> resource tier -> review. |
| Project detail | Tabs: Connect (Section 9), Auth, Database, Storage, Functions, Realtime, Logs, Backups, Settings, Activity. Tabs light up as their features land per phase. |
| Organizations | Orgs, members, roles, quotas. |
| Settings | Platform config, hosts, defaults, SMTP, integrations. |
| Audit log | Control-plane action history. |
| Security | Network restrictions, key rotation, SSL enforcement, access reviews. |

## 8. Complete feature parity matrix

Every feature in the hosted Supabase catalog, classified by responsibility and assigned a phase. Nothing is omitted.

### 8.1 Database

| Feature | Responsibility | Phase | supadupa does |
|---------|----------------|-------|---------------|
| Postgres database (full per project) | Stack | P1 | Provision dedicated Postgres per project |
| Postgres Extensions | Stack + Config | P1 / P2 UI | Enable in stack; extension toggle UI in P2 |
| Postgres Roles & permissions | Stack + Config | P2 | Role management UI |
| Vault (secrets in Postgres) | Stack | P1 | Available on provision |
| Auto REST API (PostgREST) | Stack | P1 | Surface endpoint + keys |
| Auto GraphQL API (pg_graphql) | Stack | P1 | Surface endpoint |
| Database Webhooks | Stack | P1 | Available; UI surfacing P2 |
| Cron (pg_cron) | Stack | P1 | Available; UI in P2 |
| Queues (pgmq) | Stack | P1 | Available; UI in P2 |
| Foreign Data Wrappers | Stack | P1 | Available |
| Declarative Schemas | Stack/CLI | P2 | Wire into CLI/migrations |
| Database backups (daily) | Build | P1 | Scheduled logical backups per project |
| Point-in-Time Recovery (PITR) | Build | P2 | WAL archiving (pgBackRest/WAL-G) -> object store |
| Branching (preview envs) | Build | P3 | Clone project -> ephemeral branch project |
| Read replicas | Build | P3 | Streaming + WAL-shipping replicas, auto-failover, read routing |
| Supavisor (pooler) | Stack | P1 | Expose transaction + session pooled strings |
| Dedicated Poolers | Build/Config | P3 | Co-located dedicated pooler option |
| OrioleDB storage engine | Stack (opt) | P2 | Offer as stack profile option |
| Replication to external | Build/Config | P3 | Logical replication / publication management |
| Supabase ETL (analytics destinations) | Build | P3 | Managed replication pipelines |
| Analytics Buckets (Apache Iceberg) | Build | P3 | Iceberg-format analytics storage |
| Security & Performance Advisor | Stack (Studio) | P1 / P2 | Free via Studio; fleet-level advisor P2 |
| Visual Schema Designer | Stack (Studio) | P1 | Via Studio deep-link |
| Foreign Key Selector | Stack (Studio) | P1 | Via Studio |
| SQL Editor | Stack (Studio) | P1 | Via Studio |
| Policy Templates | Stack (Studio) | P1 | Via Studio |

### 8.2 Authentication

| Feature | Responsibility | Phase | supadupa does |
|---------|----------------|-------|---------------|
| Email login | Stack | P1 | Available |
| Magic links (passwordless) | Stack | P1 | Available |
| Phone logins (SMS) | Stack + Config | P2 | SMS-provider config UI |
| Social / OAuth providers | Stack + Config | P2 | Per-project provider config UI |
| SSO with SAML (project end-users) | Stack + Config | P2 | SAML IdP config per project |
| Third-Party Auth (trust external JWTs) | Stack + Config | P2 | Config UI |
| Web3 Auth (Ethereum/Solana wallets) | Stack | P2 | Enable + config |
| MFA - TOTP | Stack | P1 | Enabled by default upstream |
| MFA - Phone | Stack + Config | P2 | Needs SMS provider |
| Captcha protection | Stack + Config | P2 | hCaptcha/Turnstile config |
| Auth Hooks (serverless customization) | Stack | P2 | Wire to edge functions |
| RLS authorization | Stack | P1 | Available |
| RBAC (end-user roles) | Stack | P1 | RLS-based pattern |
| OAuth 2.1 Server (project as IdP) | Stack | P2 | Enable + client registration UI |
| JWT Signing Keys (asymmetric) | Build + Stack | P2 | Per-project asymmetric key management + rotation |
| Email Templates | Stack + Config | P2 | Template editor UI |
| Custom SMTP | Config (Build UI) | P2 | Per-project SMTP config |
| Server-side Auth helpers | Stack (SDK) | P1 | Compatibility only |
| User Impersonation | Stack (Studio) | P1 | Via Studio |

### 8.3 Storage

| Feature | Responsibility | Phase | supadupa does |
|---------|----------------|-------|---------------|
| File storage | Stack | P1 | Available; bucket UI P2 |
| Image transformations (imgproxy) | Stack | P1 | Available |
| Resumable uploads (TUS) | Stack | P1 | Available |
| S3 compatibility | Stack | P1 | Surface S3 endpoint + access keys |
| Content Delivery Network | Build | P2 | Caching layer at edge proxy |
| Smart CDN (auto revalidation) | Build | P3 | Cache invalidation on object change |
| Persistent Storage (S3 mounts for functions) | Build/Config | P3 | Mount buckets into edge runtime |

### 8.4 Realtime

| Feature | Responsibility | Phase | supadupa does |
|---------|----------------|-------|---------------|
| Broadcast | Stack | P1 | Available |
| Broadcast Authorization | Stack | P1 | Available (RLS on channels) |
| Broadcast Replay | Stack | P2 | Enable |
| Broadcast from Database | Stack | P2 | Enable |
| Postgres Changes | Stack | P1 | Available |
| Presence | Stack | P1 | Available |
| Presence Authorization | Stack | P1 | Available |

### 8.5 Edge Functions

| Feature | Responsibility | Phase | supadupa does |
|---------|----------------|-------|---------------|
| Deno Edge Functions (runtime) | Stack | P1 | Runtime present on provision |
| Function deploy pipeline + secrets + logs | Build | P2 | Deploy API/UI, per-function secrets, log view |
| Regional invocations | Build | P3 | Run near DB (multi-host/region) |

### 8.6 Platform / control plane

| Feature | Responsibility | Phase | supadupa does |
|---------|----------------|-------|---------------|
| Management API | Build | P1 | The control-plane API itself (expanded each phase) |
| CLI | Build | P2 | Thin client over Management API |
| Terraform provider | Build | P3 | IaC over Management API |
| MCP Server | Build | P3 | Expose control plane to AI tools |
| Custom domains | Build | P2 | Per-project domain + cert issuance |
| Network restrictions (IP allowlist) | Build | P2 | Per-project ingress allowlists |
| PrivateLink / VPC connectivity | Build | P3 | Private network connectivity |
| SSL enforcement | Build/Config | P1 / P2 | TLS at edge (P1); enforce DB SSL (P2) |
| Log Drains (Datadog, Loki, Sentry, Axiom, S3, HTTPS) | Build | P2 | Per-project/fleet log export |
| Logs & Analytics | Stack (Logflare) + Build | P1 / P2 | Tail (P1); full analytics (P2) |
| Reports & Metrics | Build | P2 | Prometheus/Grafana per-project + fleet |
| Compute tiers / exact sizing | Build | P3 | Presets + exact CPU/RAM/disk, optional container limits, tier resize |
| SOC 2 / HIPAA controls | Build/process | P3 | Control mapping, audit, DPA posture |
| SOC 2 / HIPAA compliance | Process | P3 | Operator certification path |

### 8.7 Studio (delivered via per-project deep-link)

| Feature | Responsibility | Phase | supadupa does |
|---------|----------------|-------|---------------|
| SQL Editor | Stack | P1 | Deep-link |
| Visual Schema Designer | Stack | P1 | Deep-link |
| Foreign Key Selector | Stack | P1 | Deep-link |
| Policy Templates | Stack | P1 | Deep-link |
| User Impersonation | Stack | P1 | Deep-link |
| Security & Performance Advisor | Stack | P1 | Deep-link |
| Supabase AI Assistant | Stack + Config | P2 | Needs LLM API key config |
| Reports & Metrics (in-Studio) | Stack | P1 | Deep-link |

### 8.8 Vector / AI

| Feature | Responsibility | Phase | supadupa does |
|---------|----------------|-------|---------------|
| Vector database (pgvector) | Stack | P1 | Extension available |
| Automatic Embeddings | Stack | P2 | Wire triggers + queues + functions |
| Vector Buckets (S3-backed) | Build | P3 | Vector bucket storage + similarity search |
| AI Integrations (OpenAI/Hugging Face) | Config | P2 | Provider key config |

### 8.9 Client libraries

| Feature | Responsibility | Phase | supadupa does |
|---------|----------------|-------|---------------|
| JS / Flutter / Python / Swift SDKs | Upstream | P1 | Compatibility only. SDKs work unchanged against each project's endpoints. |

### 8.10 Platform-account features

These apply to supadupa admins and are distinct from project end-user auth.

| Feature | Responsibility | Phase | Notes |
|---------|----------------|-------|-------|
| Org -> team -> project RBAC | Build | P2 | Platform-level roles |
| Platform SSO (SAML) + SCIM | Build | P3 | For supadupa operators/admins |
| Platform MFA | Build | P2 | TOTP for admin accounts |
| Immutable audit log | Build | P2 | Every control-plane action |
| Usage metering + quotas | Build | P2 | DB size, egress, MAUs, invocations, storage |
| Invoicing / billing | Build | P3 (optional) | Only if commercialized |

## 9. Project Connect surface

The project Connect tab is the single place an admin gets every credential, link, and action for a project. All secrets masked by default with reveal + copy, every access audited.

### 9.1 Connection details

- **API URL** - the project's `{ref}.<domain>` Kong endpoint
- **API keys** - publishable + secret keys, and legacy `anon` / `service_role` keys
- **JWT** - JWT secret and asymmetric JWT signing keys (current + rotation history)
- **Postgres - direct** (port 5432): full URI + broken-out host/port/db/user/password
- **Postgres - pooled (Supavisor)**: transaction-mode and session-mode strings
- **Storage** - S3-compatible endpoint + storage access key/secret
- **Realtime** endpoint URL
- **Edge Functions** base URL
- **GraphQL** endpoint
- Copy-ready snippets per format: URI, `psql`, and per-language SDK init (JS/Python/Flutter/Swift)

### 9.2 Links

Studio (deep-link), auto-generated REST API docs, GraphQL explorer, each service endpoint, project logs.

### 9.3 Actions

Pause / resume / restart / destroy; reveal / copy / rotate keys; custom domain; custom SMTP; enabled-services toggles + env/config; backups (list/trigger/restore); per-project activity/audit.

Phase split: connection details, links, Studio deep-link, pause/resume/destroy, reveal/copy keys -> P1. Key rotation, custom domain, custom SMTP, service toggles/config, restore -> P2.

## 10. Control-plane Management API sketch

REST, JSON, bearer auth, versioned under `/v1`. This is the parity target for hosted Supabase's Management API; the CLI, Terraform provider, and MCP server are all thin clients over it.

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/v1/orgs` | Create org |
| `GET` | `/v1/orgs/{id}/projects` | List projects |
| `POST` | `/v1/orgs/{id}/projects` | Provision project |
| `GET` | `/v1/projects/{ref}` | Project detail + status |
| `GET` | `/v1/projects/{ref}/connect` | Full connect payload (Section 9) |
| `POST` | `/v1/projects/{ref}/{pause|resume|restart}` | Lifecycle |
| `POST` | `/v1/projects/{ref}/upgrade` | Upgrade stack version |
| `DELETE` | `/v1/projects/{ref}` | Destroy (`?retain_volumes=`) |
| `POST` | `/v1/projects/{ref}/keys/rotate` | Rotate keys (P2) |
| `GET/PUT` | `/v1/projects/{ref}/config/{auth|storage|functions|...}` | Config (P2) |
| `POST` | `/v1/projects/{ref}/backups` / `/restore` | Backup / restore |
| `POST` | `/v1/projects/{ref}/replicas` | Read replica (P3) |
| `POST` | `/v1/projects/{ref}/branches` | Branch (P3) |
| `GET` | `/v1/projects/{ref}/logs` | Stream logs |
| `*` | `/v1/projects/{ref}/domains`, `/network`, `/log-drains` | Platform config (P2) |

## 11. Data model (meta DB)

| Entity | Key fields |
|--------|------------|
| org | id, name, created_at |
| user | id, email, password_hash, mfa_secret, created_at |
| membership | user_id, org_id, role |
| project | id, ref (unique), org_id, name, host_id, status, stack_version, profile, resource_tier, created_at |
| project_spec | project_id, desired_state (JSONB: enabled services, env, resources) |
| project_config | project_id, area (auth/storage/...), config (JSONB) |
| secret | project_id, kind (jwt/signing_key/anon/service/publishable/db/s3), encrypted_value, created_at, rotated_at |
| host | id, name, address, capacity (cpu/ram/disk), used |
| backup | id, project_id, kind (logical/wal), location, size, created_at, verified_at |
| domain | project_id, fqdn, cert_status |
| log_drain | project_id (nullable=fleet), target, config |
| audit_event | id, actor_id, action, target, metadata, created_at |

`project.status`: `provisioning`, `healthy`, `degraded`, `paused`, `error`, `destroying`.

## 12. Non-functional requirements

- **Isolation:** no shared data-plane state; one project's compromise/crash cannot affect another.
- **Reproducibility:** projects rendered from templates + spec; reconciliation is idempotent.
- **Portability:** the `Provisioner` package is the only orchestrator-specific code. The Kubernetes migration must not touch API, meta DB, UI, routing, or secret models.
- **Security:** secrets never logged or returned except via audited reveal endpoints; TLS everywhere; least-privilege DB roles; key rotation supported.
- **Performance (MVP):** provision-to-healthy < ~90s on a warm host; UI interactions < 100ms perceived.
- **Observability:** every control-plane action emits a structured event; every project exposes health.

## 13. Milestones and sequencing

| Milestone | Deliverable | Depends on |
|-----------|-------------|------------|
| M0 | Repo scaffold: control-plane skeleton, `Provisioner` interface, meta-DB migrations | - |
| M1 | `ComposeProvisioner` create/destroy an isolated stack from a spec | M0 |
| M2 | Routing: Traefik front proxy + wildcard TLS + dynamic registration | M1 |
| M3 | Management API + reconciliation loop + secret/key generation | M1 |
| M4 | Admin UI shell (Vite/Tailwind4/Linear) + auth + projects list/detail | M3 |
| M5 | Create-project wizard + Connect surface + lifecycle + Studio deep-link + log tail | M2, M4 |
| M6 | Daily backups + fleet dashboard -> MVP complete (all Stack features live) | M5 |
| M7-M9 | Phase 2: PITR, KMS/Vault, key rotation, all config UIs (auth/storage/functions), custom domains, SMTP, network restrictions, log drains, metrics, RBAC, audit, metering | M6 |
| M10+ | Phase 3: `KubernetesProvisioner`, HA, read replicas, branching, regional invocations, sharding (eval Multigres), ETL, Iceberg, PrivateLink, SSO/SCIM, Terraform, MCP | M7+ |

## 14. Risks and open questions

- **Resource cost of hard isolation** (~2-4 GB RAM/project). Mitigations: stack profiles + scale-to-zero. Open: target projects-per-host for MVP?
- **Upgrade downtime** - zero-downtime rolling upgrades are a Phase 3 problem. Open: acceptable maintenance-window policy for P1/P2?
- **Multigres** - Supabase open-sourced it as a Kubernetes operator (rolling upgrades, pgBackRest PITR, OTel); may absorb several P3 items (HA, PITR, sharding). Open: adopt as Phase 3 data-plane foundation?
- **Studio multi-project** - stock Studio is single-project; we deep-link. Open: build a custom unified data browser later?
- **Secret storage in MVP** - encrypted-in-meta-DB is a stopgap; Vault/KMS in P2. Open: acceptable for first deployments?

## 15. Deliberate divergences from hosted Supabase

"Parity" here means functional equivalence, not identical implementation. Known, intentional differences:

- **Infrastructure model.** Hosted Supabase runs on AWS with managed compute/disk tiers and 17 regions. supadupa "regions/tiers" map to operator-owned hosts and resource tiers; multi-region is operator-provided.
- **PrivateLink -> VPC/network equivalent.** Implemented against the operator's network, not AWS PrivateLink specifically.
- **Compliance (SOC 2 / HIPAA).** Hosted Supabase is certified. For supadupa these are an operator responsibility: we provide the technical controls (audit, encryption, access control, retention), but certification is the deploying org's to obtain.
- **Managed support / SLA.** Not applicable to self-hosted; the operator owns uptime.
- **CDN / Smart CDN.** Functional caching, but not Supabase's specific global edge network.
- **Billing.** Metering is built; invoicing only if the operator commercializes.

These are the only intended gaps. Everything in Section 8 is otherwise targeted for functional parity.

## Appendix A - Upstream stack components

Postgres, GoTrue (Auth), PostgREST, pg_graphql, Realtime, Storage API, imgproxy, Kong (gateway), Studio, Supavisor (pooler), Edge Runtime (Deno functions), Logflare (analytics), Vector (logs), pg_cron, pgmq, pgvector, Vault. Versions pinned per supadupa release; upstream `docker-compose.yml` is the reference for tested-together version sets.

## As-built vs PRD (0.3.x)

This PRD still reads as a phased roadmap. As of the 0.3.x control plane, many Phase 2/3 **API and admin surfaces** exist in code (feature-flagged): custom domains, network restrictions, log drains, PITR plumbing, preview branches, read replicas, usage metering, draft billing, platform SSO/SCIM adapters, Kubernetes Helm/operator scaffold, team RBAC, compliance helpers, and related project-child resources.

That does **not** mean hosted-grade proof is complete. Compose remains the primary MVP runtime; Kubernetes data-plane parity is incomplete; off-host durable PITR/upgrade-restore validation is not fully proven in-tree; platform SSO is a normalized-JSON adapter (not full SAML XML); billing has no payment processor; and several recovery/HA paths remain operator-proven rather than product-certified. Treat feature flags and API presence as “surface available,” not “production-certified.” See [feature-flags.md](feature-flags.md), [known-issues.md](known-issues.md), [production-profile.md](production-profile.md), and [master-improvement-plan.md](master-improvement-plan.md).
