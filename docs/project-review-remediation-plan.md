# Project Review Remediation Plan

Date: 2026-06-08

This plan turns the multi-agent project review into an implementation roadmap. It covers security, correctness, simplification, dependency freshness, deployment hardening, validation, and regression prevention.

## Implementation Status, 2026-06-08

Launch-readiness stance: the initial MVP/Alpha launch should treat Docker Compose as the primary deployment path. The Compose base and apply-mode flows have the broadest live validation and are the right default for early OSS users. Kubernetes now has an installable Helm chart plus an operator path that passes Kind smoke testing, including the opt-in generated Supabase core services, but it remains alpha packaging until storage, realtime, functions, pooler, analytics, ingress, and full gateway compatibility are validated.

Current pass fixed the highest-risk auth/session, secret, persistence, migration, build-context, Compose hardening, frontend data-loading, provisioner artifact handling, scheduler duplication, and image runtime-hardening items. The main local validation suite now passes:

- `go test ./...`
- `npm --prefix frontend run build`
- `npm --prefix frontend run check`
- `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium-browser npm --prefix frontend run browser-smoke`
- `npm --prefix frontend exec vitest run src/lib/routes.test.ts --environment jsdom`
- `npm --prefix frontend audit`
- `npm --prefix scripts/compat ci && npm --prefix scripts/compat audit`
- `scripts/check-dockerignore.sh && scripts/check-compose-hardening.py && bash -n scripts/check-dockerignore.sh scripts/setup-compose.sh scripts/setup-local-dns.sh && git diff --check`
- `TERRAFORM_BIN=/tmp/supadupa-terraform-1.15.5/terraform scripts/check-compose-apply-lifecycle-smoke.sh` with Docker available after downloading Terraform v1.15.5 from HashiCorp releases and verifying the published SHA256SUMS entry; the script starts an isolated apply-mode platform stack with non-conflicting API/admin/meta-db ports, bootstraps an admin, creates an org, enables read replicas, builds the Terraform provider through dev overrides, applies a `supadupa_project` resource against the live local Management API so project creation reaches Docker Compose through the internal proxy, verifies required project containers run, verifies a Terraform no-op plan, exercises API pause/resume/restart/scale, upgrades the stack from `15.8.1.060` to `15.8.1.085` with a pre-upgrade backup assertion, creates a read replica, waits through a reconciler interval and verifies the project remains `healthy`, deletes the replica with manifest/env/container cleanup assertions, destroys the project through Terraform, and asserts project containers and volumes are removed.
- `scripts/check-setup-compose.sh` validates setup-compose writes `.env` with mode `600`, rejects env-file injection inputs before `.env` creation, verifies VPS database binds stay loopback unless `--expose-db` is supplied, and validates setup-local-dns output plus invalid ref/address/hostname rejection before DNS helper file creation.
- `python3 scripts/check-docs-remediation.py` verifies operator docs keep required install, security, migration, backup-target, and dependency-validation topics.
- `go test -race ./internal/scheduler`
- `go test ./cmd/supadupa-docker-proxy`
- `go test ./internal/terraform`
- `TERRAFORM_BIN=/tmp/supadupa-terraform-1.15.5/terraform scripts/check-terraform-provider-smoke.sh` after downloading Terraform v1.15.5 to `/tmp` and verifying the release checksum; the script builds the provider through Terraform dev overrides, applies a `supadupa_project` resource against a fake Management API, verifies a no-op plan, destroys the resource, and asserts POST/GET/DELETE lifecycle calls.
- `go test ./internal/operator ./cmd/supadupa-operator ./internal/provisioner/kubernetes ./cmd/supadupa-docker-proxy ./internal/api`
- `go test ./internal/operator ./cmd/supadupa-operator` after adding generic Project workload reconciliation for image-backed Kubernetes services.
- `go test -count=1 ./internal/provisioner/kubernetes`
- `go test ./internal/operator ./internal/provisioner/kubernetes` after adding Project service `configFiles`, ConfigMap-backed Deployment file mounts, and Kubernetes Kong declarative config rendering.
- `go test ./internal/provisioner/kubernetes` after making provisioner-generated Project CRDs structural and aligning Project identity/config-file schema with the Helm CRD.
- `go test ./internal/operator ./internal/provisioner/kubernetes` after adding Project service command/args rendering and moving Kubernetes Kong to a Compose-like startup path that expands a mounted declarative config template inside the pod.
- `go test ./internal/provisioner/kubernetes` after switching generated Kubernetes service environment defaults from Compose hostnames to operator-created service DNS names such as `<project>-db`, `<project>-kong`, and `<project>-rest`.
- `go test ./internal/operator ./internal/provisioner/kubernetes` after changing Kubernetes apply-mode destroy to hand off cleanup through `spec.desiredState=destroying`, wait for operator `Terminating` status before deleting the Project CR, and preserve `retain_volumes=true` PVCs even when existing PVCs were created before the retain label was present.
- `go test ./internal/provisioner/kubernetes` after changing Kubernetes secret sync to mutate and reapply Project `spec.environment`, so the operator-owned `<project>-environment` workload Secret receives rotated managed secrets instead of an unused standalone Secret manifest.
- `PATH=/tmp/linux-amd64:$PATH scripts/check-helm-chart.sh` after aligning Helm runtime namespace rendering so control-plane `SUPADUPA_K8S_NAMESPACE`, operator `SUPADUPA_OPERATOR_NAMESPACE`, and runtime RBAC all use `operator.runtimeNamespaceOverride` or the shared runtime namespace helper.
- `go test ./internal/mcp`
- `python3 scripts/check-kubernetes-crds.py`
- `jq empty charts/supadupa/values.schema.json`
- `PATH=/tmp/linux-amd64:$PATH scripts/check-helm-chart.sh` after downloading Helm v3.21.0 to `/tmp`; the script validates default, optional operator/ingress/existing-secret, external metadata database, and explicit runtime-namespace render modes.
- `KIND_BIN=/tmp/supadupa-k8s-tools/kind KUBECTL_BIN=/tmp/supadupa-k8s-tools/kubectl HELM_BIN=/tmp/linux-amd64/helm scripts/check-kubernetes-kind-smoke.sh` after downloading Kind v0.32.0 and kubectl v1.36.1 to `/tmp` and verifying release checksums; the script creates a temporary Kind cluster, rebuilds and loads control-plane/admin/operator images from the current worktree, installs the Helm chart, waits for metadata DB/control-plane/admin/operator rollouts, applies a two-service Project CR, verifies operator-rendered ConfigMap/Secret/Deployment/Service/PVC resources, verifies command/args, deterministic service env ordering, ConfigMap-backed read-only config-file mounts, read-only root filesystem, writable emptyDir mounts, HTTP probes, runtime security defaults, and non-root dependency wait init containers against live Kubernetes objects, verifies Project environment patches update the operator-owned workload Secret, waits for observed workload availability before `RuntimeReady`, exercises pause/resume/destroy, asserts non-retained workload cleanup, deletes the cluster, and `/tmp/supadupa-k8s-tools/kind get clusters` reports no clusters afterward. `SUPADUPA_KIND_SUPABASE_CORE_SMOKE=true` now additionally passes for generated `db`, `kong`, `meta`, `auth`, and `rest` services using the shared DB bootstrap renderer instead of a manual DB seed; the smoke preloads locally cached upstream images into the Kind node to reduce Docker Hub rate-limit flakiness and can use cached `alpine:3.22` as a smoke-only compatible fallback for the probe image when `busybox:1.37.0` is not cached.
- `scripts/check-compose-hardening.py` now performs a structured Compose JSON hardening assertion: base `deploy/compose.yaml` has no Docker socket mount, no Traefik Docker provider, and no Docker-label platform routing; `deploy/compose.apply.yaml` mounts the Docker socket only into the internal `docker-socket-proxy` service, keeps the proxy on an internal-only network, and points `supadupavisor` at `DOCKER_HOST=tcp://docker-socket-proxy:2375`.
- Generated local validation artifacts are now ignored in `.gitignore` for Python bytecode/cache output, Playwright reports/test results, and local compatibility/security artifacts, reducing accidental status noise during remediation validation.
- Docker proxy regression tests now exercise forwarded Compose lifecycle routes over a fake Unix-socket Docker API using real Supadupa project-ref Compose labels, prove forbidden create and exec-create payloads are blocked before reaching the socket, prove unlabeled/platform-labeled container and network mutations are blocked before upstream mutation, prove existing unlabeled network/volume object reads are stopped at preflight, prove missing read-only object probes are forwarded so Compose can create volumes/networks/containers, allow only the configured shared ingress network for read/connect/disconnect while still requiring project-labeled containers, cover transient inspect retries plus Compose `rename` during force-recreate, and prove `/containers/json`, `/networks`, and `/volumes` list responses are filtered to non-platform Supadupa project objects.
- Docker proxy image/event regression tests now prove unscoped or mixed-platform event filters, broad image wildcard filters, image imports, non-empty image-create bodies, URL-scheme/path-traversal pulls, wildcard pulls, malformed tags, and malformed image inspect paths are blocked before reaching the Docker socket, while valid tag-wildcard image-list filters and empty-body pull-style image creates remain compatible with Docker Compose.
- `scripts/check-final-remediation-suite.sh` provides a single local final-validation harness for the automated matrix. On 2026-06-08, the full unskipped local run was attempted with `PATH=/snap/bin:/tmp/linux-amd64:/tmp/supadupa-k8s-tools:$PATH HELM_BIN=/tmp/linux-amd64/helm TERRAFORM_BIN=/tmp/supadupa-terraform-1.15.5/terraform KIND_BIN=/tmp/supadupa-k8s-tools/kind KUBECTL_BIN=/tmp/supadupa-k8s-tools/kubectl SUPADUPA_KIND_SUPABASE_CORE_SMOKE=true scripts/check-final-remediation-suite.sh` and stopped only because `govulncheck` could not fetch vulnerability data from `vuln.go.dev`, which returned HTTP 403 for `GO-2025-3408`. The same harness then passed with `SUPADUPA_FINAL_SKIP_GOVULNCHECK=1` and a required skip reason documenting that external vulnerability database failure; it covered Go tests, frontend build/check/audit/browser smoke, compat install/audit, Docker/Compose/documentation/Kubernetes/Helm guards, Docker image builds and non-root checks, Terraform provider smoke, live apply-mode Compose lifecycle smoke, live Kind Helm/operator smoke, generated Supabase core Kubernetes smoke, and `git diff --check`. After the subsequent Kind preload hardening, focused Go/syntax/docs checks and `SUPADUPA_KIND_SUPABASE_CORE_SMOKE=true scripts/check-kubernetes-kind-smoke.sh` also pass. A skipped-live harness mode remains available through `SUPADUPA_FINAL_SKIP_*` flags for environments without Docker, Terraform, or Kind, but every skip flag now requires a matching non-empty `<flag>_REASON` environment variable and prints a final skip summary for validation evidence. `govulncheck` remains default-on; `SUPADUPA_FINAL_SKIP_GOVULNCHECK=1` exists only for documented external vulnerability database access failures such as `vuln.go.dev` returning HTTP 403.
- `scripts/setup-compose.sh --mode local --force` route-generation proof: writes `runtime/routes/00-platform.yaml` with file-provider API/admin routes.
- `scripts/check-release-note-policy.sh` validates that security-sensitive and deployment-sensitive changes are covered by the operator-note policy and have a matching docs update; `.github/workflows/compat.yml` runs it in the local-checks job.
- `.github/workflows/compat.yml` now runs Kubernetes CRD schema checks, Helm chart checks, checksum-verified Kind/kubectl installs, the live Kind chart/operator smoke, documentation remediation checks, broader shell syntax checks, and Python script compilation in CI.
- `go install golang.org/x/vuln/cmd/govulncheck@latest && $(go env GOPATH)/bin/govulncheck ./...` reports no vulnerabilities.
- `docker build -f deploy/Dockerfile.control-plane -t supadupa-control-plane:ci .`, `docker build -f deploy/Dockerfile.admin -t supadupa-admin:ci .`, `docker build -f deploy/Dockerfile.operator -t supadupa-operator:ci .`, and non-root `docker run --rm --entrypoint id ...` checks pass for control-plane and operator images.
- Isolated base Compose smoke with temporary `/tmp/supadupa-smoke` runtime and non-conflicting ports passes: `GET /healthz` returns `{"status":"ok"}`, `GET /v1/auth/state` returns expected unauthenticated bootstrap state, admin UI `/` serves the SPA, and the stack is removed with `docker compose down -v`.
- `scripts/check-compose-local-smoke.sh` now exercises the default setup-driven Docker Compose launch path in a temporary install directory: it runs `scripts/setup-compose.sh --mode local`, verifies generated `.env` mode `0600`, starts `deploy/compose.yaml + deploy/compose.apply.yaml` from the generated environment with non-conflicting ports, verifies env-bootstrapped admin state, performs browser-cookie login without a bearer token in the response, creates an org and two projects through the cookie-authenticated API with an allowed origin, verifies each project's runtime containers, exact seeded-domain Connect URLs, route host rule, and route upstream to its own Kong service, creates a backup storage target, binds the first project's backup policy to that target, restarts the control-plane service, verifies the target, policy binding, and both projects survive persistent-store reload, deletes the first project, verifies the second project still runs and routes independently, deletes the second project, removes the platform stack, and leaves the repository `.env` untouched.
- `scripts/check-compose-edge-routing-smoke.sh` now exercises the HTTPS edge path in a temporary offline-mode install: it runs `scripts/setup-compose.sh --mode offline`, starts `deploy/compose.yaml + deploy/compose.apply.yaml` with the `edge` profile on non-conflicting loopback ports, verifies the generated local CA and route host rule, creates a project, waits for real `db`, `kong`, `auth`, and `rest` containers, curls `https://<project>.<apps-domain>/auth/v1/health` through Traefik and Kong with the generated CA, verifies routed REST requests reach Kong auth, deletes the project, removes the platform stack, and leaves the repository `.env` untouched.
- `scripts/check-compose-admin-ui-smoke.sh` now exercises the Docker-served Admin UI against a live local Compose stack: it runs `scripts/setup-compose.sh --mode local`, starts `deploy/compose.yaml + deploy/compose.apply.yaml` with non-conflicting ports and an Admin UI build pointed at the live Management API, creates a project through the API, uses Playwright to log in through the real Admin UI, opens the project's Connect endpoints and links pages, verifies exact API, REST, Studio, and Storage S3 URLs render from the live Connect payload, clicks the Studio action, verifies the opened URL contains only a short-lived `supadupa_studio_code` rather than a reusable bearer token, asserts browser local/session storage do not contain auth tokens, deletes the project, removes the platform stack, and leaves the repository `.env` untouched.
- Isolated apply-mode Compose smoke with `deploy/compose.yaml + deploy/compose.apply.yaml`, a non-root control plane, internal Docker socket proxy, matching absolute host/container runtime path, and non-conflicting ports passes: browser-cookie bootstrap succeeds, org creation succeeds, Terraform-driven project creation reaches live Docker Compose, all expected project containers start, Terraform no-op plan succeeds, API-driven pause, resume, restart, and scale succeed, Terraform destroy removes the project, and disposable project/platform stacks, volumes, networks, and temp files are removed.
- `npm --prefix frontend outdated` now reports only major-version migrations outside the current compatibility track: React 19, React type packages 19, TypeScript 6, Zustand 5, and lucide-react 1.x.
- `npm --prefix scripts/compat outdated` reports no package updates.
- `GOPROXY=https://goproxy.cn,direct GOTOOLCHAIN=go1.26.4 go list -u -m all` reports current direct Go dependencies, with remaining update notices limited to indirect modules.
- `go env GOVERSION GOTOOLCHAIN GOPROXY` reports `go1.26.4`, `auto`, and the default Go proxy once the repository `toolchain go1.26.4` directive has resolved the patched toolchain.

Notable completed fixes:

- SSO role tampering is rejected, and the JSON SSO adapter requires an explicit opt-in flag.
- SSO assertions are rejected on replay within the same control-plane instance until their assertion expiry.
- Production startup rejects missing, known-placeholder, or too-short auth/encryption secrets.
- Browser auth uses secure cookie sessions instead of exposing bearer tokens to browser clients.
- Mutating API routes enforce origin checks for browser-cookie auth.
- Password storage moved to bcrypt over SHA-256 prehashing.
- SCIM bearer tokens are stored as keyed hashes and remain write-only.
- Login, SSO callback, MFA verify/disable, and project secret reveal/copy paths are throttled and audited without logging passwords, TOTP codes, SSO signatures, or secret values.
- MFA TOTP counters are persisted and replayed or lower counters are rejected across login, enrollment, disable, and persistent-store restore.
- Migration checksums are persisted and drift is detected.
- Backup policy storage targets persist across checkpoint restore.
- Setup-generated empty/default backup-target environment placeholders are ignored at startup, so the default local Compose install no longer fails closed unless an operator supplies meaningful backup target settings.
- `.dockerignore` blocks local runtime, env, keys, certs, Terraform state, and generated artifacts from Docker build contexts.
- `scripts/check-setup-compose.sh` now provides a focused setup regression harness for `.env` permissions, env-file injection rejection, VPS database bind defaults, local DNS helper output, and invalid ref/address/hostname rejection using a stubbed Docker CLI.
- Platform images are digest-pinned where compose uses fixed platform images, and generated project replica manifests no longer use `supabase/postgres:latest`.
- Optional compat helper containers use explicit default image tags (`rustfs/rustfs:1.0.0-beta.2` and `axllent/mailpit:v1.30.0`) instead of `:latest`, while retaining environment overrides.
- Base Compose no longer exposes the Docker socket to Traefik; platform routes are generated through the Traefik file provider.
- Compose apply mode no longer mounts the host Docker socket into `supadupavisor`; a first-party Docker API proxy is isolated on an internal proxy network, blocks unrelated administrative surfaces, rejects dangerous container-create and privileged exec-create payloads, accepts real project-ref Compose labels while rejecting platform/non-project labels, rejects malformed object path shapes, preflights container/exec/network/volume object access against Docker object labels, permits read-only 404 existence probes needed before Compose creates missing objects, allows labeled container `rename` during force-recreate, validates bind-mount host sources without blocking generated in-container targets, verifies network connect/disconnect requests against both the target network and target container labels, permits only the configured shared ingress network exception needed for project routing, filters container/network/volume list responses to non-platform Supadupa project objects, requires Docker event streams to include exactly one non-platform Compose project label filter, restricts image reads to reference-filtered image lists and Docker-reference-shaped image inspect reads, restricts image creation to pull-style `fromImage` requests with Docker-reference-shaped image/tag parameters, rejects image import/body/wildcard/scheme/path-traversal pulls, and is the only apply-mode service with the socket mount.
- API JSON requests and MCP frames now have bounded body/header limits with oversized-request regression tests; MCP bare JSON lines and header lines are byte-counted before allocation, and Management API responses read by MCP are capped.
- Per-project Vector Docker-log socket access is disabled by default and available only through `SUPADUPA_PROJECT_DOCKER_LOGS=true`.
- Generated Compose project services and replica manifests set `security_opt: no-new-privileges:true`.
- Kubernetes Project and ProjectReplica CRDs declare operator-facing `runtimeSecurityDefaults` with `seccompProfile: RuntimeDefault`, `allowPrivilegeEscalation: false`, and `dropCapabilities: [ALL]`, and the ProjectConfig, ProjectAuthHooks, ProjectBranchClone, and RetainedProjectResources CRDs now use structural spec schemas instead of preserving unknown fields.
- `charts/supadupa` now packages CRDs, least-needed operator RBAC, metadata database, control plane, admin UI, optional ingress, and optional operator deployment; the chart fails closed unless auth/encryption/meta-db secrets are supplied or an existing Secret is referenced. Control-plane runtime namespace selection defaults from the pod namespace unless explicitly overridden, Helm owns CRD installation, admin/meta database pods disable service-account token automount, PostgreSQL uses a subdirectory `PGDATA` under the mounted volume with explicit startup capabilities instead of broad privilege escalation, control-plane metadata DSN expansion is ordered after the password env var, operator polling interval is configurable through `operator.interval`, Supadupa platform image defaults use the chart app version instead of `latest`, the values schema rejects `latest` image tags, and desired-state CRD spec schemas are structural for Project, ProjectConfig, ProjectAuthHooks, ProjectBranchClone, ProjectReplica, and RetainedProjectResources. Control-plane Project CR writes, operator watches, and runtime RBAC all use the shared Helm runtime namespace helper so `operator.runtimeNamespaceOverride` cannot split control-plane writes from operator reconciliation. The Project CRD schema includes rendered identity fields (`orgId`, `displayName`, and `hostId`) plus service config-file mounts so structural pruning does not drop provisioner-authored fields.
- `cmd/supadupa-operator` and `internal/operator` provide an in-cluster operator scaffold that lists `Project` CRs, applies deterministic per-project runtime ConfigMaps and Secrets for `running`/`paused`, applies Deployments, Services, PVCs, and optional Ingresses for enabled image-backed services, prunes stale Deployments, Services, PVCs, and Ingresses by project label while preserving retained PVCs, observes managed Deployment availability and PVC bound state before reporting `Ready=True`, scales desired workloads to zero for `paused`, deletes current and stale non-retained workload resources for `destroying`, patches status/conditions through the `/status` subresource, reflects runtime security defaults, applies service command/args, readiness/liveness probes, read-only root filesystem flags, emptyDir writable-path mounts, deterministic container env ordering, and hardened dependency wait init containers, and reports unsupported desired states as degraded. Dependency wait init containers now set an explicit non-root nobody UID/GID so Pods with `runAsNonRoot` do not fail Kubernetes admission/runtime config checks when using the BusyBox wait image. The Kubernetes provisioner now hands apply-mode destroy to the operator by applying `spec.desiredState=destroying`, waiting for `status.phase=Terminating`, then deleting rendered CRs; `retain_volumes=true` marks rendered volumes retained before that handoff, and the operator preserves those desired retained PVCs even if the existing PVC was created before a retain label existed. The operator also lists ProjectConfig, ProjectAuthHooks, ProjectBranchClone, ProjectReplica, and RetainedProjectResources CRs and patches validation/observed status for those auxiliary CRD surfaces while deeper data-plane reconciliation remains explicit future work.
- Admin nginx image now gives the non-root `nginx` user ownership of `/run`, so the container can write its pid file and serve the SPA while still running as uid 101.
- Provisioner artifact writes now flow through a shared atomic writer that uses temp files, chmod, fsync, rename, and directory fsync; regression tests cover replacement permissions, missing-directory cleanup, and failed-rename cleanup. Kubernetes Project, ProjectConfig, ProjectAuthHooks, ProjectBranchClone, ProjectReplica, and RetainedProjectResources manifests are rendered from typed YAML objects, rendering tests parse them structurally and cover special keys plus quoted, newline, slash, colon, percent-bearing values, and service dependency metadata, and lifecycle/status updates for Project manifests mutate `spec.stackVersion`, `spec.desiredState`, `spec.resourceTier`, and `spec.environment` through parsed YAML nodes with drift tests for malformed/non-scalar fields.
- Scheduler/reconciler loops use a shared `PeriodicRunner` with a per-runner overlap guard; telemetry collection now uses bounded per-project concurrency with a regression test proving slow project collection overlaps.
- API route registration is split into grouped helpers in `internal/api/routes.go`; core/platform status, auth/session/SSO callback, user administration, account MFA, SCIM, org/billing/team, project overview/create/connect, project access/routing/domain, project config/services, project auth clients/hooks, project branch/replica lifecycle, project function/region/storage-mount, project replication/embedding, project database resources, project storage/vector/analytics buckets, project CDN/network, project log drains, project secrets, project backup/recovery, project log/activity, project lifecycle/ops, platform defaults, platform SSO settings, backup storage target, platform backup/restore, audit, and host handlers have been extracted from `internal/api/server.go`, and a route snapshot test guards critical routes, grouping order, duplicate registrations, and unwired route groups.
- A narrow project-child resource registry centralizes in-memory cleanup plus org/fleet/project metric counts for registered child maps, with a registry classification test that fails when new `MemoryStore` map fields are not registered or explicitly classified. The registry now also has inventory metadata for domains, configs, project access, auth clients/hooks, function resources, replication and embedding resources, database extension/cron/queue/webhook/schema/role resources, storage/vector/analytics buckets, CDN policy, backup/PITR policies, network connections, log drains, and typed partial-inventory metadata for routes, CDN invalidations, secrets, and telemetry. The drift guard now uses the registry's inventory as the source of truth, verifies complete-inventory resources have every required field, verifies partial-inventory resources declare present surfaces and explain omitted surfaces, and checks their MemoryStore fields, snapshot fields, snapshot field type parity, shape-appropriate checkpoint restore helpers, checkpoint clone helpers, normalized persistence tables inside the normalized-load helper, normalized inserts inside the normalized-sync helper, registry-generated delete-before-resync statements, API route prefixes, CLI commands, MCP tool dispatch/list registration, and Terraform provider resources remain wired.
- The project-child inventory guard now parses concrete API `HandleFunc` route registrations plus the top-level CLI dispatch switch and `printUsage()` command list, so stale comments or unrelated command text cannot satisfy API/CLI surface coverage. The MemoryStore classification guard also derives registered project-child fields from registry inventory instead of a parallel hard-coded resource-to-field map.
- Persistent-store restore coverage now proves auth client secret handles and OAuth arrays, auth hook secret handles and headers, database webhook events/headers/metadata, network connection CIDRs/config, replication pipeline credentials/config, database cron/queue/role structured and sensitive fields, and storage/vector/analytics bucket metadata survive checkpoint restoration, and a `SUPADUPA_TEST_META_DSN`-gated integration test exercises the same assertions after deleting the checkpoint row to force normalized-table reconstruction.
- Terraform resource and data-source provider configuration now uses a shared provider-data helper, Terraform resource imports share common one/two/three-part parser/state setters, resource list lookups now share a generic `findInList` helper, sensitive string/map mask preservation helpers are centralized, optional Terraform state string/time plus string list/map conversions are centralized with unit coverage, `supadupa_project` now has a framework resource-method lifecycle smoke that drives create/read/delete through a fake Management API plus live apply-mode coverage through `scripts/check-compose-apply-lifecycle-smoke.sh`, Terraform-facing resource attributes avoid reserved root names such as `provider`, API-defaulted project attributes are optional+computed, and `scripts/check-terraform-provider-smoke.sh` proves the built provider loads under Terraform CLI with dev overrides.
- Project frontend pages no longer fetch secrets or broad connect payloads on overview by default; route parsing is centralized with encoded-segment regression tests; modals and command palette inert background content and restore focus; a Playwright browser smoke covers first-admin bootstrap into the fleet dashboard with mocked Management API responses.

Remaining open or partially open items:

- The repository declares `toolchain go1.26.4`, and CI pins `actions/setup-go` to `1.26.4`. The patched toolchain is resolved in this environment now, but first-time local setup may still need a reachable Go proxy.
- Fresh unauthenticated Docker Hub pulls can still hit registry rate limits. Current image validation passed after the pinned base digests were available locally; CI/release environments should authenticate or use a registry cache if rate limits recur.
- Apply mode still depends on sensitive Docker daemon access, now through the internal proxy. The proxy filters container, network, and volume list responses, scopes Docker event streams to one non-platform Compose project label filter, requires image-list requests to include shaped reference filters with tag-only wildcards, restricts image reads to shaped inspect paths, blocks image imports while allowing pull-style `/images/create?fromImage=...` requests needed by Docker Compose, validates pull image/tag query parameters against Docker-reference-shaped values, and permits only the configured shared ingress network needed for project routing. Fixed state for stricter production environments is a separate apply worker host or VM. `scripts/check-compose-apply-lifecycle-smoke.sh` now passes with Terraform v1.15.5 and covers local Terraform create, Terraform no-op plan, API pause, API resume, API restart, API scale, API stack upgrade from `15.8.1.060` to `15.8.1.085` with pre-upgrade backup assertions, read-replica create/delete with runtime manifest/env/container cleanup assertions, a reconciler health assertion while the replica is active, Terraform destroy, and final container/volume cleanup. Compose project status now treats rendered replica overlay services as expected live services while still reporting stale replica containers as drift.
- Generated project runtime services have live create validation through apply-mode Compose, and Kubernetes Helm/operator validation now passes in Kind for platform startup, generic Project workload create, pause, resume, destroy, cleanup, and the opt-in generated Supabase core data-plane path. The Kind smoke validates live two-service dependency ordering, command/args, deterministic service env ordering, read-only ConfigMap file mounts, read-only root filesystem with writable emptyDirs, HTTP probes, runtime security defaults, service-specific security overrides, non-root dependency wait init containers, PVC binding, and Project status readiness gates. Kubernetes apply-mode destroy now uses the operator cleanup path instead of deleting the Project CR first, with unit coverage for the `retain_volumes=true` PVC preservation path. Broader `cap_drop`, `read_only`, seccomp/apparmor, user, and service-specific writable-path hardening still needs per-Supabase-service validation before enabling beyond the generated core path. The Kubernetes operator now reconciles Project CR status, project runtime ConfigMaps/Secrets, generic image-backed service Deployments, Services, PVCs, optional Ingresses, workload security contexts, service command/args, readiness/liveness probes, read-only root filesystem flags, emptyDir writable-path mounts, ConfigMap-backed read-only config-file mounts, dependency wait init containers, deterministic container env ordering, generated per-service env, observed Deployment/PVC availability before setting project `Ready=True`, and observed/degraded status for auxiliary ProjectConfig, ProjectAuthHooks, ProjectBranchClone, ProjectReplica, and RetainedProjectResources CRDs. The Kubernetes provisioner renders baseline operator-ready service images, ports, PVCs, ingress hints, probes, writable path hints, service dependency metadata, custom `env.*` overrides, Kubernetes service DNS names in generated environment defaults for postgres-meta, realtime, storage, functions, pooler, and analytics, Compose-aligned service environment keys for Kong, auth, db, imgproxy, and Studio from the stack release manifest, plus a Kong startup path that mounts a declarative config template, expands secret-backed environment variables inside the pod, writes the final file and `KONG_PREFIX` under `/tmp`, and includes Kubernetes service upstreams, key-auth consumers, ACLs, and request-transformer placeholders. The DB bootstrap renderer is shared with Compose; Kubernetes mounts that SQL into generated DB Pods, starts Postgres with pod-network listening enabled, waits for bootstrap-created roles/schemas/grants, and `SUPADUPA_KIND_SUPABASE_CORE_SMOKE=true scripts/check-kubernetes-kind-smoke.sh` now passes for generated `db`, `kong`, `meta`, `auth`, and `rest`, including generated-image assertions, Kong config mount assertions, in-cluster DB/Auth/REST/Kong reachability probes, and cleanup. Full Supabase Kubernetes data-plane parity still needs live validation of storage, realtime, functions, pooler, analytics, ingress behavior, storage-class behavior, and complete gateway auth/transform compatibility.
- Project-child resource handling has an in-memory cleanup/metrics registry plus broad complete and partial inventory metadata, and checkpoint snapshot/restore wiring is now registry-guarded for every declared memory/snapshot surface. Normalized DB delete-before-resync now uses the registry, and simple list clone/sort behavior is centralized for the common slice-backed child resources. Typed normalized DB load/insert SQL, API, CLI, Terraform, and generated clone/load behavior remain hand-wired per resource; MCP tool presence is now inventory-guarded but not generated. Branches and replicas remain explicit registry exceptions because deletion also removes branch projects, source-project references, and replica host-capacity reservations.
- API route registration, route handler groups, shared API helper families, sensitive-map masking, and project database resource handlers now live in focused helpers/files. Broad CRUD abstractions were intentionally avoided so endpoint-specific authorization, audit, rollback, masking, and runtime apply behavior stays explicit.
- Frontend modal/focus behavior is covered by static checks plus a Vitest/jsdom keyboard regression for initial focus, tab wrap, Escape close, inert background content, and focus restore. Project route encoding/decoding is covered by Vitest, auth-sensitive frontend files have a static guard against browser storage, and `browser-smoke` passed with system Chromium on this Ubuntu 26.04 environment because Playwright's bundled browser download does not officially support that host label and the CDN was inaccessible from this network location.
- Provisioner rendering is improved and guarded by `go test ./internal/provisioner/...`; Kubernetes Project, non-Project desired-state manifests, and generated CRD manifests are now rendered from typed YAML objects, Project lifecycle/status mutations use parsed YAML nodes, provisioner-rendered CRDs include `/status` subresources like the Helm chart CRDs, generated and chart CRDs now structurally validate Project, ProjectConfig, ProjectAuthHooks, ProjectBranchClone, ProjectReplica, and RetainedProjectResources spec shapes, Project and ProjectReplica CRDs structurally validate operator-facing runtime-security fields, Project CRDs structurally validate service command/args, config-file mounts, and rendered identity fields, the Helm values schema validates service types, images, service ports, security contexts, resource shapes, metadata database storage, ingress TLS, and operator settings, and rendered Project CRs include baseline operator-ready Supabase service workload fields including commands/args, probes, writable-path hardening hints, dependency-ordering metadata, ConfigMap-backed file mount metadata, project-scoped Kubernetes service DNS in generated env defaults, Kong declarative config template/startup behavior, and Compose-aligned service environment keys.

## Goals

- Remove known privilege-escalation and token-exposure paths.
- Make production deployments fail closed when required secrets or hardening settings are missing.
- Preserve backup and recovery configuration reliably across persistent-store reloads and migrations.
- Reduce duplicated resource handling so future features do not require editing many disconnected registries.
- Bring vulnerable or materially stale dependencies onto supported versions.
- Add tests that prove fixed behavior and prevent regressions.

## Non-Goals

- Full hosted-grade compliance certification.
- Complete rewrite of the API, CLI, Terraform provider, or provisioners.
- Kubernetes production hardening beyond the issues identified in review.
- Large visual redesign of the frontend.

## Priority Model

- P0: exploitable security or data-loss risk. Fix before production use.
- P1: high-impact hardening, correctness, or operational risk.
- P2: medium-risk maintainability, dependency, or UX/accessibility work.
- P3: lower-risk cleanup and long-term quality improvements.

## Phase 0: Baseline And Guardrails

### Tasks

- Capture current baseline:
  - `go test ./...`
  - `npm --prefix frontend run build`
  - `npm --prefix frontend audit --json`
  - `go list -m -u all`
  - Install or run `govulncheck ./...` in CI or a local tool container.
- Add a remediation tracking issue or project board with each section below as a milestone.
- Add a short release note policy: any fix that changes auth, secrets, persistence, migrations, Terraform behavior, or deployment defaults needs an explicit operator note.
- Guard the policy with `scripts/check-release-note-policy.sh`, so sensitive operational diffs require a corresponding docs/operator-note update.

### Current Status

- `.github/workflows/compat.yml` runs local checks on pull requests, scheduled runs, workflow dispatch, and pushes to `main` plus `release/**`.
- CI local checks run Go tests, the Go toolchain guard, `govulncheck`, frontend build/check/browser smoke/audit, compat audit, Dockerignore policy, release-note policy, Compose hardening checks, Kubernetes CRD/Helm checks, Docker image builds, Kind smoke, and script syntax checks.
- `govulncheck` plus frontend and compat `npm audit --json` outputs are written under `artifacts/security` and uploaded as the `security-audit-results` CI artifact when present.
- `scripts/check-release-note-policy.sh` is wired into CI and blocks security-sensitive or deployment-sensitive diffs unless matching operator-facing documentation is included.

### Fixed Looks Like

- Baseline commands are documented with current pass/fail status.
- Security-critical changes are tracked independently from broad cleanup.
- CI can run the same core checks developers run locally.
- Security-sensitive and deployment-sensitive diffs cannot pass the release-note check without an operator-facing docs update.

### Validation

- Confirm `git status --short` is clean before each remediation branch.
- CI stores `npm audit` and `govulncheck` results as the `security-audit-results` artifact when present.
- Run `scripts/check-release-note-policy.sh` on remediation and release branches.

## Phase 1: P0 Security Fixes

## 1.1 Fix SSO Role Escalation

### Problem

`PlatformSSOAssertionSignaturePayload` signs issuer, audience, email, NameID, and expiry, but not `Role`. `platformSSOUser` trusts `assertion.Role` during auto-provisioning. A user with a valid assertion can alter the role field to `admin`.

Relevant files:

- `internal/control/sso.go`
- `internal/api/server.go`
- `internal/control/sso_test.go` or `internal/api/server_test.go`

### Tasks

- Short-term fix:
  - Stop trusting `assertion.Role` unless it is part of the validated signature payload.
  - Prefer using `config.DefaultRole` for auto-provisioned users until a real signed-role path exists.
- Add role to `PlatformSSOAssertionSignaturePayload` only if this custom assertion format remains supported.
- Add a tamper test:
  - Generate a valid signed assertion with role `viewer`.
  - Change only `role` to `admin`.
  - Verify validation or provisioning rejects the escalation.
- Add a test for the expected valid path:
  - A signed assertion with an allowed role provisions the intended role, or default-role provisioning ignores unsigned role data.
- Longer-term:
  - Replace the custom JSON SSO callback with real SAML response parsing and XML signature validation, or document that the current format is a test/development adapter only.

### Current Status

- `PlatformSSOAssertionSignaturePayload` signs issuer, audience, normalized email, NameID, normalized role, and expiry, so role tampering invalidates the assertion signature.
- Enabling the normalized JSON SSO adapter requires `SUPADUPA_ENABLE_PLATFORM_SSO_JSON_ADAPTER=true`; without that explicit opt-in, SSO settings update is rejected.
- SSO callback responses set a browser session cookie and omit bearer tokens from the browser response body.
- Regression coverage in `TestPlatformSAMLSSOSettingsAndCallback` verifies adapter opt-in, successful viewer auto-provisioning, replay rejection, role-tampering rejection with `401`, email-domain rejection, and audit events.
- `docs/security.md` documents that the current endpoint is a development/compatibility JSON assertion adapter, not full SAML XML parsing/signature validation.

### Fixed Looks Like

- A caller cannot change role without invalidating the assertion.
- Auto-provisioning never creates an admin from unsigned client input.
- Tests fail if `Role` is removed from the signed payload or trusted without validation.

### Validation

- `go test ./internal/control ./internal/api`
- Focused SSO role/replay regression: `go test ./internal/api -run 'TestPlatformSAMLSSOSettingsAndCallback|TestPlatformSSOCallbackFailuresAreThrottledAndAudited'`
- Manual negative check with a tampered SSO assertion receives `401` or provisions only the configured default non-admin role.

### Regression Checks

- Add a named test such as `TestPlatformSSOAssertionRejectsUnsignedRoleTampering`.
- Keep role-validation tests near signature-payload tests, not only API endpoint tests.

## 1.2 Fail Closed On Control-Plane Secrets

### Problem

Production-like compose defaults allow known or empty secrets. Auth token signing and persistent encryption fall back to development values.

Relevant files:

- `deploy/compose.yaml`
- `scripts/setup-compose.sh`
- `cmd/supadupa/main.go`
- `internal/control/auth.go`
- `internal/control/persistent_encryption.go`

### Tasks

- Introduce an explicit dev/local mode flag, for example `SUPADUPA_ALLOW_DEV_SECRETS=true`.
- At startup, reject:
  - empty `SUPADUPA_SECRET_KEY`
  - empty `SUPADUPA_AUTH_SECRET` when auth is required
  - known placeholders such as `local-dev-secret-change-me`, `dev-only-change-me`, and `supadupa-local-development-secret-key`
  - secrets below a documented minimum length
- Change deployment compose defaults:
  - Use required variable syntax for production profiles where appropriate.
  - Keep local quick-start usable through `setup-compose.sh` generating strong secrets.
- Split auth and encryption secrets:
  - Do not silently use `SUPADUPA_SECRET_KEY` as `SUPADUPA_AUTH_SECRET` in production mode unless explicitly documented and accepted.
- Add startup tests:
  - fails with missing secrets in production mode
  - fails with known placeholders
  - passes in explicit dev mode
  - passes with generated strong secrets

### Current Status

- `deploy/compose.yaml` uses required variable syntax for `SUPADUPA_SECRET_KEY` and `SUPADUPA_AUTH_SECRET`; `scripts/setup-compose.sh` generates separate random 32-byte hex values for both.
- Startup calls `validateRuntimeSecretsFromEnv` before metadata DB setup; production mode rejects missing secrets, known placeholders, and values shorter than 32 characters unless `SUPADUPA_ALLOW_DEV_SECRETS=true` is explicitly set.
- Runtime secret validation errors name the offending setting but do not include the submitted secret value.
- `cmd/supadupa/main_test.go` covers missing secret rejection, known-placeholder rejection, short-secret rejection, strong-secret success, explicit dev override, and non-disclosure of rejected secret values.
- `.env.example`, `docs/install.md`, and `docs/security.md` document required stable secrets and the local-development escape hatch.

### Fixed Looks Like

- A production or VPS startup cannot accidentally use known secrets.
- Local setup still works because scripts generate random values.
- Logs report which required setting is missing without printing secret values.

### Validation

- `go test ./cmd/supadupa ./internal/control`
- Focused startup secret regression: `go test ./cmd/supadupa -run 'TestValidateRuntimeSecrets|TestBootstrapInitialAdmin'`
- Start local compose after running setup script.
- Attempt startup with placeholder secrets and confirm it exits early.
- `scripts/check-setup-compose.sh`

### Regression Checks

- CI test for placeholder rejection.
- Documentation in `docs/install.md` and `.env.example` warns that secrets are required and generated by setup.
- Setup regression test asserts `.env` is mode `600` and invalid control-character input exits before writing `.env`.

## Phase 2: P1 Data Integrity And Recovery Correctness

## 2.1 Persist Backup Policy Storage Target IDs

### Problem

`BackupPolicy.StorageTargetID` is validated and exposed, but normalized persistent storage omits it. This can drop off-host backup target binding after reload or normalized-state reconstruction.

Relevant files:

- `internal/control/store.go`
- `internal/control/persistent_store.go`
- `migrations/`
- `internal/control/persistent_store_test.go`
- CLI and Terraform backup policy paths if present

### Tasks

- Add a migration:
  - `ALTER TABLE backup_policies ADD COLUMN storage_target_id UUID REFERENCES backup_storage_targets(id) ON DELETE SET NULL;`
  - Preserve existing policies with null target IDs.
- Update normalized load SQL to select `storage_target_id`.
- Update normalized sync SQL to insert `storage_target_id`.
- Confirm snapshot structures include `StorageTargetID`.
- Add persistent-store tests:
  - create backup storage target
  - set project backup policy with that target
  - force checkpoint/sync/reload
  - assert target ID remains
- Add migration test:
  - upgraded schema has the same column as fresh schema
  - old DB with existing policies migrates without data loss

### Current Status

- `migrations/0037_backup_policy_storage_target.sql` adds `backup_policies.storage_target_id` with an index, and fresh schema/migration drift is guarded by `TestPersistentStoreInsertColumnsExistInMigrations`.
- Normalized persistent load and sync include `storage_target_id`, and checkpoint snapshot/restore already carries `BackupPolicy.StorageTargetID`.
- `TestPersistentStoreRestoresCheckpoint` creates a backup storage target, binds a physical project backup policy to it, restores the checkpoint, and asserts `StorageTargetID`, `Kind`, and `Schedule` remain intact.
- The project-child registry classifies `backup_policies` as a complete inventory resource, and the shared project-child field-preservation fixture now asserts backup policy storage-target fields across checkpoint restore and the optional normalized-table restore.
- API and Terraform paths expose and preserve `storage_target_id`; focused tests cover Management API policy updates and Terraform client/resource state for `supadupa_project_backup_policy`.

### Fixed Looks Like

- Backup policies keep their configured target through reloads.
- Backup policy API, CLI, Terraform, memory store, and persistent store agree on the field.
- Fresh and migrated schemas match for `backup_policies`.

### Validation

- `go test ./internal/control ./internal/metadb ./internal/api ./internal/cli ./internal/terraform`
- Focused backup policy target persistence/API/Terraform regression: `go test ./internal/control ./internal/api ./internal/terraform -run 'TestPersistentStoreRestoresCheckpoint|TestBackupStorageTargetsAndPolicies|TestProjectBackupPolicy|TestClientProjectBackupPolicy|TestProviderSchema|TestPersistentStoreInsertColumnsExistInMigrations'`
- Manual API check:
  - create target
  - update policy with `storage_target_id`
  - restart control plane
  - read policy and confirm target remains

### Regression Checks

- Add a schema consistency test that compares important normalized tables between fresh and migrated databases.
- Add field-preservation tests for future policy fields.

## 2.2 Add Migration Checksum And Drift Protection

### Problem

The migrator records only version and skips previously applied versions without verifying SQL checksum. Historical migrations appear to have drift risk.

Relevant files:

- `internal/metadb/migrate.go`
- `internal/metadb/migrate_test.go`
- `migrations/`

### Tasks

- Extend migration metadata table with `checksum`, `applied_at`, and optional `name`.
- Compute checksum over exact SQL bytes.
- On startup:
  - apply unapplied migrations
  - fail if an applied version has a different checksum
- Add tests:
  - first run stores checksum
  - second run with same SQL passes
  - modified applied SQL fails
  - fresh-vs-upgraded schema equivalence for key tables
- Document that historical migrations are immutable after release.

### Current Status

- `schema_migrations` now records `checksum`, `name`, and `applied_at`; the migrator computes SHA-256 over the exact SQL bytes loaded from disk.
- Startup applies unapplied migrations, backfills checksum/name for legacy rows that lack them, and fails if a previously applied migration version has a different checksum.
- Migration tests cover sorted loading, name derivation, first-run checksum/name recording, same-SQL reapply, checksum drift failure, legacy checksum backfill, and applied-row name backfill.
- `TestPersistentStoreInsertColumnsExistInMigrations` parses normalized `INSERT INTO ... (columns...)` statements from `persistent_store.go` and fails if any written column is absent from migration DDL, guarding key table drift such as missing policy or secret-state columns.

### Fixed Looks Like

- Applied migrations cannot be silently edited.
- Upgrade drift is detected before runtime logic touches inconsistent schema.

### Validation

- `go test ./internal/metadb`
- `go test ./internal/metadb ./internal/control`
- Run migration tests against a temporary database or test harness.

### Regression Checks

- CI runs migration tests.
- Review checklist includes "new schema changes require new migration, not editing old migrations."

## Phase 3: P1 Token And Secret Handling

## 3.1 Move Admin Sessions Away From LocalStorage

### Problem

The frontend stores admin bearer tokens in `localStorage`, making tokens available to XSS, browser extensions, and compromised dependencies.

Relevant files:

- `frontend/src/api.ts`
- `frontend/src/lib/routes.ts`
- `frontend/scripts/check-static.mjs`
- `frontend/src/lib/auth-session.ts`
- `frontend/src/pages/login.tsx`
- `internal/api/server.go`

### Tasks

- Prefer HttpOnly cookie sessions:
  - API already sets `supadupa_session`; make it the canonical session transport.
  - Stop returning or storing long-lived admin bearer tokens in frontend storage.
  - Use `/v1/auth/state` or `/v1/account` to hydrate current user.
- If bearer tokens are still needed for CLI/API:
  - keep CLI login response unchanged
  - make browser login response omit token or mark browser mode explicitly
- Update frontend:
  - remove `localStorage` token persistence
  - keep only in-memory state if needed for UI
  - use `credentials: "include"` consistently
  - await logout and clear in-memory state after server success or confirmed failure handling
- Strengthen cookie behavior:
  - `HttpOnly`
  - `Secure` in HTTPS/proxy deployments
  - `SameSite=Lax` or stricter where workable
  - host-only domain unless configured
- Add CSRF review:
  - session cookies plus mutating API routes need CSRF protection or strict CORS/origin checks.
  - Add origin/referrer validation for mutating browser requests, or CSRF tokens.

### Current Status

- Browser-mode bootstrap, login, and SSO callback responses set `supadupa_session` cookies and omit bearer tokens from response bodies. CLI/API clients still receive bearer tokens when they do not opt into browser mode.
- `frontend/src/api.ts` sends `X-Supadupa-Browser: true` and `credentials: "include"` for browser API calls, and `frontend/src/lib/auth-session.ts` hydrates session state from `/v1/auth/state` without storing bearer tokens.
- `internal/api/http_helpers.go` enforces HttpOnly, `SameSite=Lax`, host-only-by-default auth cookies, and `Secure` cookies when TLS or `X-Forwarded-Proto: https` is present. `SUPADUPA_COOKIE_DOMAIN` is explicit opt-in only.
- `internal/api/http_helpers.go` rejects mutating cookie-authenticated requests from disallowed origins and rejects mutating cookie-authenticated requests with no `Origin`; bearer-authenticated API/CLI mutations remain compatible.
- `frontend/scripts/check-static.mjs` rejects `supadupa_token` and `supadupa_studio_token` markers anywhere in frontend source, rejects `localStorage`/`sessionStorage` in auth-sensitive browser session files, and self-tests its API path interpolation guard.
- Regression evidence now includes `TestBrowserAuthResponsesOmitBearerToken`, `TestCORSRequiresOriginForCookieAuthenticatedMutation`, `TestAuthStateIncludesCookieAuthenticatedUser`, `TestAuthCookieDomainRequiresExplicitOptIn`, and the frontend static remediation check.

### Fixed Looks Like

- Browser devtools localStorage no longer contains admin session tokens.
- Admin UI remains logged in across reloads through an HttpOnly cookie.
- CLI/API token use remains possible for non-browser clients.
- Cross-site POSTs cannot use a cookie session to mutate state.

### Validation

- `npm --prefix frontend run build`
- `go test ./internal/api`
- Browser manual checks:
  - login works
  - reload keeps session
  - logout clears session
  - localStorage has no admin token
  - unauthorized API calls fail after logout

### Regression Checks

- Frontend test or lint check preventing `supadupa_token` localStorage usage.
- API test for cookie-based auth and logout.
- API test for CSRF/origin behavior on mutating routes.

## 3.2 Replace Studio Query Tokens

### Problem

Studio session tokens are appended as `supadupa_studio_token` query parameters, which can leak via history, logs, screenshots, and referrers.

Relevant files:

- `frontend/src/components/runtime-link.tsx`
- `internal/api/server.go`
- route/forward-auth code

### Tasks

- Replace query token with one of:
  - one-time short-TTL code in URL, exchanged server-side for a cookie
  - server-set HttpOnly Studio cookie scoped to the Studio host
  - `postMessage` handoff to a verified Studio origin after opening a neutral URL
- Enforce one-time use and short TTL for any URL-borne code.
- Ensure codes are scoped to:
  - project ref
  - user ID
  - audience `studio`
  - expiry
- Avoid logging codes.
- Add tests:
  - code works once
  - expired code fails
  - code for project A fails for project B
  - normal admin token is not accepted as Studio one-time code unless intended

### Current Status

- `frontend/src/components/runtime-link.tsx` no longer appends reusable `supadupa_studio_token` values. Studio links request `/v1/projects/{ref}/studio-session`, open a neutral tab, and add a short-lived `supadupa_studio_code` URL parameter only for the handoff.
- `internal/api/project_overview_handlers.go` issues Studio handoff codes scoped to the authenticated claims and project ref with a short TTL.
- `internal/api/auth_handlers.go` consumes Studio codes exactly once, requires the forwarded Studio request to match the target project by host or route evidence, sets a follow-up `supadupa_session` cookie for Studio, and rejects replayed or wrong-project codes.
- `frontend/scripts/check-static.mjs` fails the build if the old `supadupa_studio_token` marker returns to frontend source.
- Regression evidence now includes `TestStudioForwardAuthUsesSupadupaSessionCookie`, covering unauthenticated denial, authenticated cookie forwarding, host/project mismatch, one-time code success, replay rejection, and wrong-project rejection.

### Fixed Looks Like

- Studio URLs no longer contain bearer/session tokens.
- A captured URL code is useless after first exchange or expiry.
- Studio access remains project-scoped.

### Validation

- `go test ./internal/api`
- Manual Studio open from project page.
- Browser history contains no reusable token.

### Regression Checks

- Test `withStudioToken` removal or replacement behavior.
- Add server tests for code replay and project scoping.

## 3.3 Harden Password And SCIM Token Hashing

### Problem

Passwords and SCIM tokens use fast SHA-256 hashing.

Relevant files:

- `internal/control/auth.go`
- `internal/control/sso.go`
- persistent user and SSO config storage

### Tasks

- Adopt PHC-format Argon2id or bcrypt for passwords.
- Keep verification migration:
  - verify old `sha256$...`
  - rehash to new format on successful login or explicit user update
- Set password policy appropriate for admin users:
  - minimum length
  - reject common placeholders
  - optional breach-list check if practical
- For SCIM:
  - require high-entropy generated tokens
  - store HMAC-SHA256 with server-side secret or Argon2id hash
  - avoid raw token display after creation
- Add tests:
  - old hashes still authenticate
  - successful old-hash login upgrades hash
  - wrong password fails
  - SCIM token verification constant-time and rejects empty/disabled cases

### Current Status

- New password hashes use `bcrypt-sha256$...`, and legacy `sha256$...` password hashes still verify and are rehashed on successful login.
- User creation and password update share a central password policy: passwords are required on creation, optional on metadata-only update, must be at least 12 characters, must not have leading/trailing/control whitespace, and reject common placeholder values.
- Platform SCIM bearer tokens use a versioned HMAC-SHA256 hash keyed by the server auth secret, legacy SHA-256 hashes can be verified and detected for rehash, short SCIM token input is rejected, and raw tokens are not returned after configuration.
- Regression tests cover bcrypt hashing, wrong-password failure, legacy password rehash, weak/common password rejection, empty-password update preservation, SCIM HMAC format, legacy SCIM verification/rehash signaling, and short SCIM token rejection.

### Fixed Looks Like

- New password hashes are slow and self-describing.
- Existing users can still log in, then migrate.
- SCIM token storage is resistant to offline brute force for reasonable token entropy.

### Validation

- `go test ./internal/control ./internal/api`
- Focused auth hashing/policy regression: `go test ./internal/control ./internal/api -run 'TestHashPasswordUsesBcryptSHA256|TestAuthenticateUserRehashesLegacySHA256Password|TestUserPasswordPolicyRejectsWeakAndPlaceholderPasswords|TestUpdateUserPasswordPolicyRejectsWeakPasswordAndAllowsEmptyPassword|TestBootstrap|TestUser|TestSCIM'`
- Manual login for existing and newly created users.

### Regression Checks

- Test rejects accidental return to plain SHA-256 for new users.

## Phase 4: P1 Deployment Hardening

## 4.1 Harden `.env` Generation And Script Inputs

### Problem

`setup-compose.sh` writes secrets to `.env` without restricted permissions and writes raw values directly.

Relevant files:

- `scripts/setup-compose.sh`
- `scripts/setup-local-dns.sh`
- `.env.example`

### Tasks

- Add `umask 077` before writing secret-bearing files.
- Write `.env` to a temp file, `chmod 600`, then rename.
- Validate inputs:
  - hostnames/domains
  - IP addresses
  - emails
  - project refs
  - no newlines/control characters
- Prefer prompting or env vars for bootstrap password over command-line argument.
- Update docs to warn that `.env` contains secrets.

### Current Status

- `scripts/setup-compose.sh` starts with `umask 077`, writes `.env` through a temporary `.env.XXXXXX` file, applies mode `600`, then renames it into place.
- Setup-generated `.env` values are validated before writing: hostnames must be fully qualified DNS names, emails must have a basic address shape, bootstrap passwords and provider credential env vars reject control characters, and generated local/VPS database bind defaults stay loopback unless `--expose-db` is explicitly supplied for VPS mode.
- `scripts/setup-local-dns.sh` validates hostnames, IPv4/`::1` address input, and project refs before creating `runtime/local-dns` output; refs are normalized to lowercase after validation so invalid ref shapes cannot leave partial DNS helper files.
- `scripts/check-setup-compose.sh` now verifies `.env` mode `600`, control-character rejection without `.env` creation, invalid compose domain/hostname/email/provider-token rejection, VPS default/`--expose-db` database binds, valid local-DNS output, invalid local-DNS ref/address/hostname rejection, and no local-DNS output after invalid input.

### Fixed Looks Like

- Generated `.env` is mode `0600`.
- Invalid values cannot inject extra env-file or hosts-file lines.
- Scripts fail with clear messages on invalid input.

### Validation

- Run setup script in local/offline modes.
- `stat -c %a .env` returns `600`.
- Try malicious newline input and confirm rejection.
- `scripts/check-setup-compose.sh`

### Regression Checks

- `scripts/check-setup-compose.sh` covers setup-compose and setup-local-dns validation helpers with a stubbed Docker CLI.
- Compatibility scripts continue to run.

## 4.2 Reduce Docker Socket And Root Risk

### Problem

The API container has writable Docker socket access and runs as root.

Relevant files:

- `deploy/compose.yaml`
- `deploy/Dockerfile.control-plane`
- `internal/provisioner/compose`

### Tasks

- Decide target architecture:
  - separate provisioner worker with Docker access, API only calls worker
  - or Docker socket proxy allowing only required endpoints
- Make control-plane runtime non-root where feasible.
- Add compose security settings:
  - `cap_drop: ["ALL"]` where workable
  - `security_opt: ["no-new-privileges:true"]`
  - read-only root filesystem where workable
  - explicit writable mounts only for runtime dirs
- Make Docker socket mount read-only or proxy-mediated if direct access remains.
- Document remaining risk if compose provisioning still requires broad Docker control.

### Current Status

- Base Compose has no Docker socket mount and platform routing is generated through Traefik's file provider instead of Docker-label discovery.
- Apply-mode Compose mounts the host Docker socket only into the internal `docker-socket-proxy` service; `supadupavisor` talks to `DOCKER_HOST=tcp://docker-socket-proxy:2375`.
- The proxy blocks known administrative Docker API surfaces, filters container/network/volume list responses to non-platform Supadupa project objects, preflights object mutations against Compose project labels, rejects dangerous container-create and privileged exec-create payloads, scopes Docker events to exactly one non-platform Compose project label filter, requires image-list reference filters with tag-only wildcards, allows only Docker-reference-shaped image inspect reads, and allows only pull-style image create requests with Docker-reference-shaped `fromImage` and `tag` query values.
- `cmd/supadupa-docker-proxy` tests prove image imports, request bodies, broad wildcard image filters, wildcard pulls, URL-scheme pulls, malformed image inspect paths, malformed tags, mixed/platform event filters, unlabeled/platform-labeled object mutations, and unrelated list entries are blocked or filtered before broader Docker daemon access.
- Remaining risk: apply mode still grants a first-party service sensitive Docker daemon access. Stricter production isolation should run apply provisioning on a separate worker host or VM.

### Fixed Looks Like

- API compromise does not automatically imply unconstrained host Docker control.
- Runtime process is non-root unless a documented operation requires otherwise.

### Validation

- Compose local project creation still works.
- Provisioner operations still create/update/destroy project containers.
- Container user is non-root: `docker compose exec supadupavisor id`.
- `scripts/check-compose-apply-lifecycle-smoke.sh`

### Regression Checks

- Deployment docs state exact Docker privileges required.
- CI or smoke test asserts image has expected user if practical.
- Apply-mode lifecycle smoke asserts create, pause, resume, restart, scale, destroy, container cleanup, and volume cleanup through the internal Docker proxy.

## 4.3 Docker Build Context And Image Pinning

### Problem

Build context can include local secrets because `.dockerignore` is minimal. Images use mutable tags and run as root.

Relevant files:

- `.dockerignore`
- `deploy/Dockerfile.admin`
- `deploy/Dockerfile.control-plane`
- `deploy/compose.yaml`

### Tasks

- Extend `.dockerignore`:
  - `.env`
  - `.env.*`
  - `*.key`
  - `*.pem` if not intentionally copied
  - `runtime`
  - `tmp`
  - nested `node_modules`
  - compat artifacts
  - local backup artifacts
- Pin image digests for release builds or provide a lock file.
- Add non-root users in final images.
- Add nginx hardening:
  - `server_tokens off`
  - CSP suitable for the SPA
  - `frame-ancestors`
  - HSTS for TLS deployments
  - cache policy for static assets vs `index.html`

### Current Status

- `.dockerignore` blocks `.env`, environment variants, keys/certs, runtime state, backups, Terraform state, local temp outputs, frontend and compat `node_modules`, Playwright output, and generated artifacts from Docker build contexts.
- `scripts/check-dockerignore.sh` is wired into CI and asserts the required secret/runtime/build-output patterns stay present.
- Control-plane and operator images build with non-root final users, and CI verifies their runtime `id` output. The admin image runs nginx as uid 101 and owns `/run` so the pid file can be written without root.
- `deploy/nginx/admin.conf` disables server tokens and adds SPA-safe hardening headers and cache policy separation for static assets versus `index.html`.
- Compose platform image defaults use fixed version tags or digest-pinned references where the deployment uses platform images. The Helm chart defaults platform images from the chart app version and rejects `latest` tags through the values schema.
- Remaining operational risk: fresh image builds and live smokes can still hit unauthenticated registry rate limits. CI/release environments should authenticate or use a registry cache when rate limits recur.

### Fixed Looks Like

- Docker build context does not send local secrets.
- Release builds are reproducible or digest-pinned.
- Admin nginx includes basic hardening headers.

### Validation

- `docker build` for both images.
- Confirm `.env` is not in context using build logs or temporary test.
- Smoke-test admin UI.

### Regression Checks

- Add a check that `.dockerignore` contains required secret patterns.

## 4.4 Public Database Port Defaults

### Problem

Edge profile defaults bind Postgres and pooler ports to `0.0.0.0`, which can expose DB-facing ports unexpectedly.

Relevant files:

- `deploy/compose.yaml`
- `.env.example`
- `scripts/setup-compose.sh`
- docs routing/install pages

### Tasks

- Default Postgres and pooler binds to `127.0.0.1` for local/offline.
- Require explicit public opt-in for VPS mode.
- Document firewall, TLS, password, and operational prerequisites.
- Add setup-script confirmation for public DB exposure.

### Current Status

- `deploy/compose.yaml` defaults `SUPADUPA_POSTGRES_ADDR` and `SUPADUPA_POOLER_ADDR` to `127.0.0.1`.
- `.env.example` documents loopback database-facing edge bindings and warns to use `0.0.0.0` only for intentional public VPS exposure with firewall, TLS, and auth controls.
- `scripts/setup-compose.sh` generates loopback Postgres and pooler binds for local, offline, and VPS modes by default; VPS mode switches those two binds to `0.0.0.0` only when `--expose-db` is supplied.
- `scripts/check-setup-compose.sh` now verifies both VPS paths: default loopback binds and explicit `--expose-db` public binds.
- `docs/install.md`, `docs/dns-tls.md`, and `docs/security.md` document the public direct-DB exposure opt-in and operational risk.

### Fixed Looks Like

- Local edge startup does not publish DB ports publicly.
- VPS public DB exposure is an explicit operator choice.

### Validation

- Generated local `.env` uses loopback DB binds.
- Generated VPS `.env` behavior matches documented opt-in.
- `scripts/check-setup-compose.sh`

## Phase 5: P1/P2 API And MCP Resilience

## 5.1 Add Request Body Limits

### Problem

API and MCP paths previously could allocate unbounded memory from request bodies or content lengths.

Relevant files:

- `internal/api/server.go`
- `internal/mcp/server.go`

### Tasks

- Completed:
  - API JSON routes and MCP frames are bounded.
  - Oversized JSON/MCP paths return bounded error responses.
  - Regression tests cover the oversized-request paths.
- Keep default body limits explicit:
  - small JSON routes, for example 1 MiB
  - upload/source routes with explicit larger limits
  - MCP frame limit
  - upstream response read limit
- Wrap request bodies with `http.MaxBytesReader`.
- Return `413 Payload Too Large` for oversized requests.
- Add tests:
  - oversized JSON rejected
  - normal JSON accepted
  - oversized MCP `Content-Length` rejected before allocation
  - oversized bare newline-delimited MCP JSON rejected before reading the full stream
  - oversized MCP header lines rejected before reading the full stream
  - large upstream MCP response capped

### Fixed Looks Like

- No request path allocates arbitrary memory solely from client-provided length.
- Limits are documented and route-specific where needed.

### Validation

- `go test ./internal/api ./internal/mcp`
- Manual curl with oversized payload returns 413.

## 5.2 Add Login, MFA, And Secret-Reveal Throttling

### Problem

Login and TOTP verification have no throttling or replay tracking. Secret reveal/copy paths should also be treated as sensitive operations.

Relevant files:

- `internal/api/server.go`
- `internal/control/auth.go`
- store types if persistence is needed

### Tasks

- Add per-account and per-IP rate limits for:
  - password login
  - MFA verification
  - SSO callback failures
  - secret reveal
- Audit failed login/MFA attempts without logging passwords or codes.
- Consider last accepted TOTP counter storage to prevent replay within the valid window.
- Add tests for lockout, reset after success/time, and audit events.

### Current Status

- Password login uses per-IP and per-email failure buckets, returns `429` with `Retry-After` after repeated failed attempts, audits `user.login_failed` without passwords, and resets the limiter after successful authentication.
- Platform MFA verify/disable paths use sensitive-action throttles, audit failed code checks without submitted TOTP values, and reset after successful verification/disable.
- SSO SAML callback failures are throttled by IP plus assertion identity fields where available, successful callbacks reset those buckets, and assertion replay is rejected.
- Project secret reveal/copy paths are throttled by both account and IP, return `Retry-After`, and continue auditing sensitive access without exposing secret values.
- TOTP verification stores and persists the last accepted counter so a code cannot be replayed within the valid window.

### Fixed Looks Like

- Repeated guesses are throttled.
- Operators can see failed authentication attempts.
- Valid users are not permanently locked out by one failure burst.

### Validation

- `go test ./internal/api ./internal/control`
- Focused throttling/replay regression: `go test ./internal/api ./internal/control -run 'TestPlatformLoginFailuresAreThrottled|TestPlatformLoginThrottleFollowsAccount|TestPlatformMFAFailuresAreThrottled|TestPlatformMFADisableFailuresAreThrottled|TestPlatformSSOCallbackFailuresAreThrottled|TestProjectSecretRevealIsThrottled|TestProjectSecretRevealThrottleFollowsAccountAcrossIPs|TestProjectSecretCopyThrottleFollowsIPAcrossAccounts|TestTOTPCodeCannotBeReplayed|TestPersistentStoreRestoresCheckpoint'`
- Manual repeated failed login returns throttling response.

## Phase 6: Dependency Freshness And Supply Chain

## 6.1 Go Vulnerability Updates

### Problem

Review reported reachable vulnerabilities in indirect dependencies:

- `golang.org/x/net v0.48.0` to `v0.55.0`
- `google.golang.org/grpc v1.79.2` to at least `v1.79.3`, latest observed `v1.81.1`

Direct dependencies appeared current during review.

### Tasks

- Install or run `govulncheck` with a patched Go toolchain:
  - `GOTOOLCHAIN=go1.26.4 go run golang.org/x/vuln/cmd/govulncheck@latest ./...`
  - If the default Go proxy cannot fetch the toolchain in the current environment, use a working proxy such as `GOPROXY=https://goproxy.cn,direct`.
- Update vulnerable modules:
  - `GOTOOLCHAIN=go1.26.4 go get golang.org/x/crypto@v0.52.0 golang.org/x/net@v0.55.0 google.golang.org/grpc@v1.81.1`
  - run `go mod tidy`
- Re-run tests and vulnerability checks.
- Watch for Terraform plugin framework constraints before broad transitive upgrades.

### Current Status

- `golang.org/x/crypto`, `golang.org/x/net`, and `google.golang.org/grpc` are updated to the patched versions called out by the review.
- `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` reports no vulnerabilities.
- `.github/workflows/compat.yml` runs `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` in local checks for pull requests plus pushes to `main` and `release/**` branches.
- `go list -u -m all` still shows unrelated minor/patch updates, but not for the vulnerable modules identified in this phase.

### Fixed Looks Like

- `govulncheck ./...` reports no reachable vulnerabilities for these issues.
- `go test ./...` passes.

### Validation

- `go test ./...`
- `govulncheck ./...`
- `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`

### Regression Checks

- CI runs `govulncheck` on pull requests plus pushes to `main` and `release/**` branches.

## 6.2 Frontend Vite And Audit Fixes

### Problem

`npm audit` previously reported moderate Vite/esbuild issues. Vite is now upgraded to `^8.0.16` and `@vitejs/plugin-react` to `^6.0.2`.

Relevant files:

- `frontend/package.json`
- `frontend/package-lock.json`
- `frontend/vite.config.ts`

### Tasks

- Completed:
  - `vite` upgraded to `^8.0.16`
  - `@vitejs/plugin-react` upgraded to `^6.0.2`
  - Node engine requirement documented as `^20.19.0 || >=22.12.0`
- Run:
  - `npm --prefix frontend install`
  - `npm --prefix frontend run build`
  - `npm --prefix frontend audit`
- Check dev server behavior:
  - bind only to local interface unless explicitly configured
  - no exposed dev server in production docs
- Defer React 19/Zustand/lucide major migrations unless needed for Vite compatibility.

### Current Status

- `npm --prefix frontend audit --audit-level=moderate` reports no vulnerabilities, and `npm --prefix frontend run build` plus `npm --prefix frontend run check` pass.
- `npm --prefix frontend outdated` now reports only major-version migrations outside the current compatibility track: React 19, React type packages 19, TypeScript 6, Zustand 5, and lucide-react 1.x.
- Local development docs and browser-smoke config bind Vite to `127.0.0.1`; production deployment uses the built admin image rather than a Vite dev server.

### Fixed Looks Like

- `npm audit` has no Vite/esbuild finding.
- Production build passes.
- Dev server remains local-only by default.

### Validation

- `npm --prefix frontend run build`
- `npm --prefix frontend run check`
- `npm --prefix frontend audit`
- Manual admin UI smoke test.

## 6.3 Compat Package Lockfile

### Problem

`scripts/compat/package.json` has dependencies but no lockfile, reducing reproducibility and auditability.

### Tasks

- Add `scripts/compat/package-lock.json`.
- Update dependencies:
  - `@supabase/supabase-js`
  - `ws`
- Change install commands to use `npm ci` where possible.
- Run compat probes that use these dependencies.

### Current Status

- `scripts/compat/package-lock.json` is present, `@supabase/supabase-js` is updated to `^2.108.0`, and `ws` is updated to `^8.21.0`.
- `npm --prefix scripts/compat outdated` reports no package updates, and `npm ci` plus `npm audit` pass for the compat package.

### Fixed Looks Like

- Compat scripts install reproducibly.
- `npm --prefix scripts/compat audit` can run.

### Validation

- `npm --prefix scripts/compat ci`
- `npm --prefix scripts/compat audit`
- `scripts/check-dockerignore.sh`
- `scripts/check-compose-hardening.py`
- `scripts/check-release-note-policy.sh`
- `python3 scripts/check-kubernetes-crds.py`
- `PATH=/tmp/linux-amd64:$PATH scripts/check-helm-chart.sh` or CI-installed Helm v3.21.0
- `scripts/check-kubernetes-kind-smoke.sh` with Kind, kubectl, Helm, and Docker available
- `bash -n scripts/check-compose-apply-lifecycle-smoke.sh scripts/check-dockerignore.sh scripts/check-helm-chart.sh scripts/check-kubernetes-kind-smoke.sh scripts/check-release-note-policy.sh scripts/check-setup-compose.sh scripts/setup-compose.sh scripts/setup-local-dns.sh scripts/compat/*.sh`
- `python3 -m py_compile scripts/check-compose-hardening.py scripts/check-kubernetes-crds.py`
- `git diff --check`
- Run selected realtime/Supabase JS compatibility scripts.

## Phase 7: Simplification And Deduplication

## 7.1 Centralize Project-Child Resource Handling

### Problem

Project child resources are manually registered and handled across memory store fields, constructors, snapshots, normalized persistence, cleanup, usage, metrics, API, CLI, and Terraform. This drift likely caused the backup policy persistence bug.

Relevant files:

- `internal/control/store.go`
- `internal/control/persistent_store.go`
- `internal/api/server.go`
- `internal/cli/cli.go`
- `internal/terraform/*resource.go`

### Tasks

- Inventory all project-child resource types:
  - domains
  - routes
  - log drains
  - functions
  - replicas
  - auth hooks/clients
  - database extensions/roles/schemas/queues/webhooks/cron jobs
  - storage/vector/analytics buckets
  - secrets
  - policies
  - network connections
- Introduce a small typed registry for common behavior:
  - project ref
  - ID/key field
  - clone function
  - sort function
  - list/count/delete cleanup
  - metric name
- Start with low-risk resources such as log drains or analytics buckets.
- Do not attempt a giant all-resource refactor in one branch.
- Add field-preservation tests for resources that cross memory/persistent/API boundaries.

### Current Status

- `internal/control/project_child_resources.go` centralizes in-memory cleanup and org/fleet/project metrics for registered project-child maps.
- `internal/control/project_child_resources_test.go` classifies every `MemoryStore` map field as a registered project child, an explicit manual project-child exception, or non-project-scoped state.
- Domains, configs, project access grants, auth clients/hooks, functions, function regions, function storage mounts, replication pipelines, embedding jobs, database extensions, database cron jobs, database queues, database webhooks, database schemas, database roles, storage/vector/analytics buckets, CDN policy, backup/PITR policies, network connections, and log drains now carry complete registry inventory metadata for their MemoryStore field, snapshot field, normalized table, API route prefix, CLI command, MCP tool, and Terraform resource. Routes, CDN invalidations, secrets, and telemetry now carry typed partial inventory metadata for the surfaces they do have plus explicit omission reasons for missing surfaces. A drift guard uses those registry values as the source of truth and verifies those surfaces stay wired to the current source.
- `TestPersistentStoreRestoresCheckpoint` now includes field-preservation assertions for auth client secret handles, redirect URIs, grant types, and scopes; auth hook secret handles, headers, timeout, and retry settings; project configs, project domains and certificate metadata, project access grants, project functions, function regions, function storage mounts, replication pipeline credentials/config, embedding jobs, database extension overrides, database cron/queue/webhook/schema/role structured and sensitive fields, network connection CIDRs/config, storage/vector/analytics bucket metadata, backup/CDN/PITR policy values, and log drain config updates. `TestPersistentStoreRestoresProjectChildFieldsFromNormalizedTables` reuses the same assertions against a disposable Postgres DSN after forcing restore from normalized tables, including project config, domain, project access, backup/CDN/PITR policy, and log drain update/config preservation. `TestProjectChildFieldPreservationCoverageTracksInventory` now derives covered resource names from the shared fixture value types and requires every complete-inventory resource to be covered by field-preservation restore assertions.
- The project-child inventory drift guard now verifies normalized persistence deletes registered resource tables before full resync, in addition to load and insert coverage. It also compares the registry-derived normalized table set with the load/sync/delete table references and requires explicit exceptions for manual operational tables such as branches, replicas, backups, WAL archives, project logs, and audit events.
- Project-child list clone/sort behavior now uses a shared helper for simple slice-backed child resources, including project access grants, auth clients/hooks, functions, function regions/storage mounts, replication pipelines, embedding jobs, database cron/queue/webhook/schema/role resources, storage/vector/analytics buckets, CDN invalidations, network connections, and log drains; `TestProjectChildListsCloneSortAndReturnEmptySlices` proves representative list paths return non-nil empty slices, preserve their sort order, and clone nested metadata/config maps before returning API-facing values, and `TestProjectChildListMethodsUseSharedCloneSortHelper` guards those list methods against drifting back to bespoke `sort.Slice` logic.
- Remaining gap: the registry does not yet drive snapshot/load/sync, API/CLI/MCP/Terraform generation, specialized clone/list behavior across every child resource, or full fixture generation for every persistent resource type.

### Fixed Looks Like

- Adding a new child resource requires fewer disconnected edits.
- Metrics, cleanup, snapshot, and persistence cannot silently miss registered resources.
- Tests cover cross-store preservation.

### Validation

- `go test ./internal/control ./internal/api`
- Targeted deduplication guard: `go test ./internal/control`
- Project-child normalized delete drift guard: `go test ./internal/control`
- Project-child clone/sort list helper regression: `go test ./internal/control`
- Project-child clone/sort helper drift guard: `go test ./internal/control`
- Expanded checkpoint field-preservation regression: `go test ./internal/control`
- Registry-derived field-preservation fixture coverage and normalized log-drain restore regression: `go test ./internal/control`
- Diff review confirms no behavior changes for existing API responses.

## 7.2 Extract API Route Modules And Shared Handlers

### Problem

`internal/api/server.go` contains a very large route table and many inline handlers, making auth, body limits, audit, and validation behavior hard to keep consistent.

### Tasks

- Split routes by domain:
  - auth
  - settings
  - orgs/RBAC
  - projects lifecycle
  - project config
  - backup/recovery
  - database resources
  - storage/CDN
  - metrics/audit
- Add shared helpers:
  - `registerAuthRoutes`
  - `registerProjectRoutes`
  - `decodeLimitedJSON`
  - `auditMetadata`
  - `requireProjectAccess`
  - path segment helpers
- Keep public API paths unchanged.

### Fixed Looks Like

- No route behavior changes.
- New route groups have focused tests.
- Body limit and auth helpers are used consistently.

### Current Status

- `internal/api/routes.go` owns route registration and groups routes by platform, org, and project domains.
- `internal/api/auth_handlers.go` owns auth state, bootstrap, login, logout, SSO callback, Studio forward auth, replay cache, and Studio session code handling.
- `internal/api/core_handlers.go` owns health, provisioner/runtime-config, fleet metrics, advisor, compliance, and Prometheus handlers.
- `internal/api/user_handlers.go` owns user administration handlers.
- `internal/api/scim_handlers.go` owns SCIM handlers.
- `internal/api/org_handlers.go` owns org, quota, feature-flag, usage, billing, member, and team handlers.
- `internal/api/project_overview_handlers.go` owns project create/read, metrics, telemetry, connect, CLI profile, and Studio session handlers.
- `internal/api/project_access_routing_handlers.go` owns project access grants, route manifests, custom domains, and domain certificate handlers.
- `internal/api/project_config_handlers.go` owns project service and config handlers plus runtime config secret materialization helpers.
- `internal/api/project_auth_handlers.go` owns project auth client and auth hook handlers plus masking and runtime hook materialization helpers.
- `internal/api/project_branch_replica_handlers.go` owns project branch and read-replica lifecycle handlers plus replica sync helpers.
- `internal/api/project_function_handlers.go` owns project function, function region, and function storage mount handlers.
- `internal/api/project_data_handlers.go` owns project replication pipeline and embedding job handlers.
- `internal/api/project_database_extension_handlers.go`, `internal/api/project_database_cron_handlers.go`, `internal/api/project_database_queue_handlers.go`, `internal/api/project_database_webhook_handlers.go`, `internal/api/project_database_schema_handlers.go`, `internal/api/project_database_role_handlers.go`, and `internal/api/project_database_runtime_helpers.go` own project database extension, cron, queue, webhook, schema, role, database SQL apply, and database secret-handle resolution behavior.
- `internal/api/project_storage_handlers.go` owns project storage bucket, vector bucket, analytics bucket, storage data-plane apply, and storage/vector/analytics masking helpers.
- `internal/api/project_edge_network_handlers.go` owns project CDN policy, CDN invalidation, CDN object-event, network summary, and network connection handlers.
- `internal/api/project_log_drain_handlers.go` owns project log drain handlers plus the shared sensitive-config-key masking helper used by extracted handler groups.
- `internal/api/project_secret_handlers.go` owns project secret list/reveal/upsert/delete/copy/rotate handlers.
- `internal/api/project_backup_recovery_handlers.go` owns project backup, restore, PITR policy, recoverability, and WAL archive handlers plus restore DTOs.
- `internal/api/project_log_activity_handlers.go` owns project log listing, SSE log streaming, and project activity handlers.
- `internal/api/project_lifecycle_handlers.go` owns project pause/resume/restart/upgrade/scale/destroy handlers plus upgrade/scale DTOs and upgrade safety helpers.
- `internal/api/platform_settings_handlers.go` owns platform defaults, platform SSO configuration, and backup storage target handlers.
- `internal/api/audit_host_handlers.go` owns audit event and host handlers.
- `internal/api/platform_backup_handlers.go` owns platform backup and restore handlers.
- `internal/api/http_helpers.go` owns request logging, body limits, CORS, auth-cookie transport, bearer/cookie token extraction, auth middleware, JSON decoding, and API error writers.
- `internal/api/authz_helpers.go` owns role ranking, platform/org/project authorization gates, project response sanitization, request claims, and visible-project filtering.
- `internal/api/rate_limiters.go` owns login/MFA/SSO/secret-access limiter types plus sensitive action keying and sanitized audit failure reasons.
- `internal/api/sensitive_masking_helpers.go` owns shared sensitive-map masking and generic sensitive metadata key detection; domain wrappers preserve existing response shapes and stricter auth-hook header handling.
- `internal/api/scim_helpers.go`, `internal/api/metrics_helpers.go`, `internal/api/platform_sso_helpers.go`, and `internal/api/project_routing_helpers.go` own SCIM DTO conversion, Prometheus rendering, platform SSO auto-provisioning, and route reconciliation helpers.
- `internal/api/server.go` now contains only `Config` and `NewServer`, so Phase 7.2's original `server.go` extraction target is complete.
- Phase 7.2 follow-up simplification is complete for the planned route/module/helper extraction scope.

### Validation

- `go test ./internal/api`
- Targeted extraction validation: `go test ./internal/api`
- Broad extraction validation: `go test ./...`
- Whitespace validation: `git diff --check`
- Whitespace/regression guard: `git diff --check -- internal/api docs/project-review-remediation-plan.md`
- Sensitive masking helper regression: `go test ./internal/api`
- Database handler split regression: `go test ./internal/api`
- Optional route snapshot test listing registered paths.

## 7.3 Terraform Resource Update Semantics And Helpers

### Problem

Several Terraform resources implement update as delete-then-create. Common client/config/import scaffolding is repeated.

### Tasks

- Identify all delete-then-create update resources.
- For each:
  - implement API update endpoint, or
  - mark ForceNew/RequiresReplace for fields that cannot update safely.
- Extract common helpers:
  - provider client configuration
  - import ID parsing
  - list-and-find by key
  - masked sensitive map preservation
  - time/string setters
- Add Terraform provider tests for failed update behavior.

### Fixed Looks Like

- Terraform plans accurately show replacement when replacement is required.
- Failed update cannot silently destroy the remote object.
- Repeated scaffolding is reduced.

### Current Status

- Replace-only Terraform resources no longer perform silent delete-then-create updates; unsupported in-place updates report replacement diagnostics, and resources that cannot update in place use `RequiresReplace` plan modifiers or a resource-level `ModifyPlan` replacement hook.
- `internal/terraform/replace_plan_test.go` now includes a guard that instantiates replace-only resource schemas and fails if a configurable attribute lacks `RequiresReplace` and the resource has no replacement `ModifyPlan` hook.
- Terraform resource and data-source provider-data configuration now flows through a shared `clientFromProviderData` helper with tests for nil, wrong-type, and valid client cases, eliminating repeated Configure-method type-check/diagnostic scaffolding.
- Shared import helpers now cover one-part IDs, two-part IDs, and three-part IDs; trim all ID parts; reject incomplete IDs; and set Terraform import attributes with consistent diagnostics. Two-part helpers preserve existing slash-or-colon support, and the three-part helper preserves database schema colon support while keeping slash-only parsing for access grants and team members.
- A generic list lookup helper now centralizes the `ErrNotFound` return pattern across resource list searches, with unit coverage for success and not-found behavior.
- Sensitive string and map mask preservation now lives in `internal/terraform/sensitive_helpers.go` and is used by project function, auth client/hook, replication pipeline, analytics bucket, database role/webhook/schema/queue/cron, storage/vector bucket, network connection, log drain, and platform SSO state setters.
- Optional Terraform state string/time conversions and string list/map state encoders now live in `internal/terraform/state_value_helpers.go`; direct tests cover null-vs-empty string handling, timestamp formatting, list encoding, and masked sensitive-map preservation. Resource-specific state setters still map domain fields explicitly, but shared helpers now own Terraform framework string list/map encoding and error handling.
- Terraform-facing attributes that previously used the reserved root name `provider` are now renamed to domain-specific names (`sso_provider`, `network_provider`, and `embedding_provider`) while the Management API JSON payloads continue to use their existing `provider` fields. `internal/terraform/provider_schema_test.go` prevents reserved root schema names from reappearing, verifies API-defaulted `supadupa_project` attributes are optional+computed, and `scripts/check-terraform-provider-smoke.sh` exercises the built provider through Terraform CLI dev overrides.
- Terraform CLI smoke coverage now creates, refreshes through a no-op plan, and destroys `supadupa_project` plus representative child resource `supadupa_project_domain` against a fake Management API. The smoke asserts the expected project and domain API requests are observed.
- Remaining gap: per-resource state setter functions are still resource-specific where field mapping differs, and live Terraform acceptance against a deployed Management API still does not cover every Terraform resource.

### Validation

- `go test ./internal/terraform`
- `go test ./internal/terraform` after provider-data helper extraction.
- `go test ./internal/terraform` after import/list lookup helper extraction.
- `go test ./internal/terraform` after expanding import/list lookup helper usage across remaining simple two-part imports and resource list searches.
- `go test ./internal/terraform` after sensitive-helper extraction.
- `go test ./internal/terraform` after one-part and three-part import helper extraction.
- `go test ./internal/terraform` after optional state value helper extraction.
- `go test ./internal/terraform` after adding the provider reserved-name schema guard and CLI smoke script.
- `TERRAFORM_BIN=/tmp/supadupa-terraform-1.15.5/terraform scripts/check-terraform-provider-smoke.sh` after expanding the fake Management API smoke to cover `supadupa_project` and `supadupa_project_domain`.
- `go test ./...`
- `git diff --check`
- Live Terraform acceptance against a deployed Management API when an environment is available.

## 7.4 Provisioner Rendering And Atomic Writes

### Problem

Provisioners render and mutate YAML using string operations and write artifacts directly.

Relevant files:

- `internal/provisioner/compose/compose.go`
- `internal/provisioner/kubernetes/kubernetes.go`

### Tasks

- Centralize artifact rendering:
  - one function returns desired compose/Kubernetes artifacts
  - one writer handles temp file, chmod, fsync, rename, and directory fsync
- For Kubernetes:
  - render typed objects through YAML library
  - parse and update YAML nodes instead of line replacement
- Add tests for special characters, env values, and atomic write behavior.

### Current Status

- Shared writer exists in `internal/provisioner/artifact` and is used by compose and Kubernetes provisioner artifact writes.
- Atomic writer tests prove replacement content/mode, temp cleanup on missing-directory failure, and temp cleanup plus existing-path preservation on failed final rename.
- Kubernetes Project manifests now render from typed YAML objects, and render tests parse the Project CR structurally to cover quoted values, newlines, slashes, colons, percent signs, and YAML-significant map keys without depending on encoder formatting.
- Kubernetes ProjectConfig, ProjectAuthHooks, ProjectBranchClone, ProjectReplica, and RetainedProjectResources manifests now render from typed YAML objects, with tests decoding those manifests structurally instead of depending on YAML quote formatting. Kubernetes secret sync now mutates Project `spec.environment` and reapplies the Project CR, so the operator-owned `<project>-environment` Secret used by workloads receives rotated managed secrets instead of writing an unused standalone Secret manifest.
- Provisioner-generated CustomResourceDefinition manifests now render from typed YAML objects, and tests decode them structurally to assert CRD names, status subresources, structural ProjectConfig/ProjectAuthHooks/ProjectBranchClone/ProjectReplica/RetainedProjectResources spec schemas, and the ProjectReplica runtime-security schema.
- Kubernetes Project lifecycle/status helpers parse YAML nodes for `spec.stackVersion`, `spec.desiredState`, and `spec.resourceTier`; they reject malformed YAML, duplicate keys, non-map `spec`, and non-scalar target fields instead of silently missing or corrupting fields.
- Kubernetes `SyncServices` now parses the existing Project manifest and replaces only `spec.services`; regression coverage proves paused desired state, org/display/host identity fields, domain, stack version, profile, resource tier, environment, and domain/version-derived default service rendering are preserved.
- Kubernetes Project render tests assert baseline operator-ready workload fields for stack-release images, service ports, persistent volumes, ingress hosts, readiness/liveness probes, read-only root filesystem flags, writable path hints, default service dependencies, disabled-service dependency filtering, Compose-aligned service environment keys, and custom `env.*` overrides from the structured Project manifest.
- `charts/supadupa` packages the Kubernetes install surface, including CRDs with `/status` subresources, structural Project/ProjectConfig/ProjectAuthHooks/ProjectBranchClone/ProjectReplica/RetainedProjectResources spec schemas for operator-facing and desired-state fields, RBAC, meta DB, control plane, admin UI, optional ingress, optional operator deployment, runtime namespace selection, configurable operator polling interval, and operator permissions for project ConfigMaps, Secrets, Deployments, Services, PVCs, and Ingresses. Helm v3.21.0 `lint` and `template --include-crds` pass with explicit secret values, and `scripts/check-helm-chart.sh` now fails closed if Helm is unavailable plus asserts representative invalid values, operator interval rendering, shared runtime namespace rendering, and metadata DSN env ordering are rejected or rendered correctly.
- `internal/operator` and `cmd/supadupa-operator` provide the operator scaffold and unit tests for `Project` status reconciliation, image-backed workload apply/delete/prune paths, observed Deployment/PVC availability before project `Ready=True`, stale workload cleanup during destroy, retained-PVC preservation, runtime security default propagation, service readiness/liveness probe rendering, read-only root filesystem rendering, emptyDir writable-path mounts, deterministic container env ordering, dependency wait init containers, auxiliary CRD observed/degraded status, and paused scale-to-zero behavior.
- Live Kind validation now passes for Helm platform install, generic operator Project reconciliation, and the opt-in generated Supabase core service smoke for `db`, `kong`, `meta`, `auth`, and `rest`, including self-contained DB bootstrap verification, core service rollouts, DB/Auth/REST/Kong in-cluster probes, and operator cleanup. Apply-mode provisioner destroy now hands cleanup to the operator and has retained-PVC regression coverage. The Compose apply lifecycle smoke has been expanded and now passes with API stack upgrade, read-replica create/delete, active-replica reconciler health, and cleanup assertions. Remaining Kubernetes gap: full Supabase Kubernetes data-plane parity still needs live cluster validation for storage, realtime, functions, pooler, analytics, ingress behavior, and full gateway auth/transform compatibility.

### Fixed Looks Like

- Crashes cannot leave partially written route/config artifacts.
- YAML rendering handles escaping correctly.
- Create/sync/upgrade share render logic.

### Validation

- `go test ./internal/provisioner/...`
- `go test ./internal/operator ./cmd/supadupa-operator ./internal/provisioner/kubernetes`
- `python3 scripts/check-kubernetes-crds.py`
- `jq empty charts/supadupa/values.schema.json`
- `scripts/check-helm-chart.sh` with Helm installed
- Manual project create/sync/upgrade/replica smoke test.

## 7.5 Scheduler And Reconciler Reliability

### Problem

Ticker loops repeat logic and run serially; reconciler ignores some persistence errors.

Relevant files:

- `internal/reconciler/reconciler.go`
- `internal/scheduler/backups.go`
- `internal/scheduler/telemetry.go`

### Tasks

- Extract scheduler runner:
  - interval parsing
  - initial run behavior
  - cancellation
  - error logging policy
- Add bounded concurrency for independent per-project work.
- Aggregate and return persistence errors from reconciler status writes.
- Keep audit/log best-effort only if explicitly documented.

### Current Status

- Scheduler and reconciler loops share `PeriodicRunner`, including overlap-skip behavior covered by tests.
- Telemetry collection uses bounded per-project worker concurrency, with a blocking collector test proving independent eligible projects overlap.
- Reconciler status-write persistence failures are aggregated and returned, including failures after provisioner errors.

### Fixed Looks Like

- One slow project does not block all telemetry/reconciliation.
- Persistence failures are visible to callers and tests.

### Validation

- `go test ./internal/reconciler ./internal/scheduler`
- Tests with fake slow/failing projects.

## Phase 8: Frontend Quality And Performance

## 8.1 Gate Dashboard Queries By Route/Panel

### Problem

The dashboard eagerly fetches many project and platform datasets on every authenticated route.

Relevant files:

- `frontend/src/app.tsx`
- page and panel components
- dashboard context

### Tasks

- Move page-specific queries into route/page components.
- Gate active-project queries by selected tab/section.
- Keep only global essentials at app shell:
  - auth state
  - runtime config
  - org/project list if needed for nav
- Avoid fetching secrets metadata, logs, SCIM users, and audit data unless the user opens those views.

### Current Status

- `frontend/src/app.tsx` derives route flags from the current pathname and uses `enabled` gates on page-specific React Query calls.
- The app shell keeps global essentials loaded: auth state, org/project lists, runtime/provisioner status, and selected project detail only on project routes.
- Sensitive or expensive datasets are gated to the relevant surfaces: connect payloads only on the Connect tab; SCIM users/groups and platform SSO/settings only on Settings; audit events/integrity only on the Audit page; project logs/log drains only on Logs; backups/WAL/PITR only on Backups; and project secrets only through explicit side-panel/API actions rather than overview load.
- `npm --prefix frontend run check` and the browser smoke cover the route helper/static guard path; manual browser network comparison remains the strongest evidence for request-count reduction.

### Fixed Looks Like

- Initial authenticated load issues far fewer API requests.
- Sensitive operational data is not loaded for unrelated pages.
- Existing pages still show data when opened.

### Validation

- `npm --prefix frontend run build`
- Browser network panel before/after request count.
- Manual page navigation smoke test.

## 8.2 Encode Dynamic API Path Segments

### Problem

Many frontend API calls interpolate path parameters directly.

Relevant files:

- `frontend/src/api.ts`

### Tasks

- Add helper:
  - `const segment = (value: string) => encodeURIComponent(value);`
- Use it for every dynamic path segment:
  - project ref
  - org ID
  - route/domain FQDN
  - log drain ID
  - secret kind
  - resource names
- Add tests if frontend test framework exists, or add static review checklist.

### Current Status

- `frontend/src/api.ts` centralizes API path segment encoding with `segment()` and query encoding with `queryString()`.
- `frontend/scripts/check-static.mjs` scans `/v1` API template literals, including multi-line literals and `apiBase`-prefixed stream URLs, and fails if dynamic path/query values do not use `segment()` or `queryString()`.
- The static checker has a built-in self-test proving it catches one-line and multi-line raw `/v1` interpolation while allowing the expected `apiBase` prefix before the API path.
- `frontend/src/lib/routes.ts` centralizes UI project path generation and decoding through `projectPath`, `projectRefFromPathname`, and project subroute helpers; Vitest covers encoded refs and URL-significant section/item values.

### Fixed Looks Like

- No API path interpolation uses raw dynamic values.
- Existing normalized refs still work.

### Validation

- `npm --prefix frontend run build`
- `npm --prefix frontend exec vitest run src/lib/routes.test.ts --environment jsdom`
- `node frontend/scripts/check-static.mjs`
- Manual operations for IDs/names containing URL-significant characters where backend allows them.

## 8.3 Dialog Accessibility

### Problem

Custom dialogs lack focus trap, initial focus, focus restore, and complete ARIA labeling.

Relevant files:

- `frontend/src/components/modal.tsx`
- command palette in `frontend/src/app.tsx`

### Tasks

- Use native `<dialog>` or a small tested focus trap.
- Add:
  - `aria-labelledby`
  - optional `aria-describedby`
  - Escape close
  - focus restore
  - initial focus
  - inert background behavior
- Test destructive modals with keyboard only.

### Current Status

- `frontend/src/components/modal.tsx` uses role/ARIA labeling, initial focus, tab wrap, Escape close, inert background handling, and focus restore.
- `frontend/src/components/modal.test.tsx` covers initial focus, focus trap tab wrapping, Escape close, background inert state, and focus restoration under jsdom.
- The command palette uses the same focus helper path for modal-like behavior, so the static and Vitest checks cover the shared focus primitives.

### Fixed Looks Like

- Keyboard and screen-reader users can open, use, and close dialogs predictably.
- Focus returns to the triggering control.

### Validation

- `npm --prefix frontend run check`
- `npm --prefix frontend run build`
- `./node_modules/.bin/vitest run --environment jsdom src/lib/routes.test.ts src/components/modal.test.tsx` from `frontend/`
- Manual keyboard pass remains useful before release on real browsers.

## Phase 9: Documentation Updates

### Tasks

- Update install docs:
  - required secrets
  - generated `.env` permissions
  - public DB port opt-in
  - Docker socket risk
- Update security docs:
  - session cookie model
  - SSO role validation
  - password hashing migration
  - CSRF/origin policy
- Update operations docs:
  - migration checksum policy
  - backup target persistence
  - dependency/vulnerability check commands
- Update compatibility docs if token artifacts sanitization changes.

### Current Status

- `docs/install.md` documents required generated secrets, `.env` mode `0600`, setup input rejection, public direct-DB opt-in through `--expose-db`, loopback metadata/admin/API binds, and apply-mode Docker socket proxy requirements.
- `docs/security.md` documents stable control-plane secrets, browser HttpOnly cookie sessions, origin enforcement for cookie-authenticated mutations, SSO JSON adapter limitations and role binding, Studio one-time-code access, password hashing migration from legacy `sha256$...` to `bcrypt-sha256$...`, SCIM token hashing, Docker socket/proxy risk, and public DB/pooler exposure risk.
- `docs/operations.md` documents migration checksum immutability and failure behavior, backup target/policy persistence including `BackupPolicy.StorageTargetID`, recovery-target enforcement flags, and local vulnerability/audit validation commands including `govulncheck` and `npm audit`.
- `scripts/check-docs-remediation.py` guards the required Phase 9 topics across install, security, and operations docs, and `.github/workflows/compat.yml` runs it in local checks.

### Fixed Looks Like

- Operators can understand new required settings before upgrade.
- Security-sensitive behavior is documented in one place.
- Upgrade notes call out any breaking deployment changes.

### Validation

- `python3 scripts/check-docs-remediation.py`
- `python3 -m py_compile scripts/check-compose-hardening.py scripts/check-docs-remediation.py scripts/check-kubernetes-crds.py`
- `scripts/check-release-note-policy.sh`

## Phase 10: Final Validation Matrix

Run the full validation suite after all P0/P1 work, and after each major phase.

### Required Automated Checks

- `scripts/check-final-remediation-suite.sh` runs the automated matrix below as one reproducible local harness. Live infrastructure gates may be skipped only with an explicit `SUPADUPA_FINAL_SKIP_*` environment variable plus the matching non-empty `<flag>_REASON`; skipped gates are printed in a final skip summary that must be copied into validation evidence. `SUPADUPA_FINAL_SKIP_GOVULNCHECK=1` is reserved for documented external `vuln.go.dev` access failures.
- `go test ./...`
- `npm --prefix frontend run build`
- `npm --prefix frontend run check`
- `npm --prefix frontend audit`
- `govulncheck ./...`
- `npm --prefix scripts/compat ci`
- `npm --prefix scripts/compat audit`
- `scripts/check-dockerignore.sh`
- `scripts/check-compose-hardening.py`
- `scripts/check-setup-compose.sh`
- `scripts/check-release-note-policy.sh`
- `python3 scripts/check-docs-remediation.py`
- `python3 scripts/check-kubernetes-crds.py`
- `scripts/check-helm-chart.sh` with Helm installed
- `scripts/check-kubernetes-kind-smoke.sh` with Kind, kubectl, Helm, and Docker available
- `SUPADUPA_KIND_SUPABASE_CORE_SMOKE=true scripts/check-kubernetes-kind-smoke.sh` for the opt-in generated Supabase core Kubernetes data-plane smoke when upstream image pulls are acceptable
- `scripts/check-compose-local-smoke.sh` with Docker available
- `scripts/check-compose-edge-routing-smoke.sh` with Docker available
- `scripts/check-compose-admin-ui-smoke.sh` with Docker, npm, and a Chromium browser available
- `scripts/check-compose-apply-lifecycle-smoke.sh` with Docker and Terraform available
- `scripts/check-terraform-provider-smoke.sh` with Terraform available
- `go test ./internal/operator ./cmd/supadupa-operator ./internal/provisioner/kubernetes`
- `go test -race ./internal/scheduler`
- `go test ./cmd/supadupa-docker-proxy`
- Docker image builds for control-plane, admin, and operator images, plus non-root `id` checks for control-plane and operator.
- `bash -n` for shell validation scripts and `python3 -m py_compile` for Python validation scripts.
- `git diff --check`

### Required Manual Smoke Tests

- Local setup:
  - run `scripts/setup-compose.sh --mode local`
  - verify `.env` permissions
  - start compose
  - login
  - create org/project
  - open project connect page
- Auth:
  - login/logout/reload
  - no localStorage token
  - failed login throttling
  - MFA path if enabled
- SSO:
  - valid assertion path
  - tampered role assertion rejected
- Studio:
  - open Studio
  - no reusable token in URL
  - project-scoped access enforced
- Backups:
  - create backup target
  - bind policy to target
  - restart/reload persistent store
  - verify target remains
- Terraform:
  - plan update for resources formerly delete-then-create
  - confirm replacement or safe update behavior
- Deployment:
  - Docker images build
  - containers run non-root where intended
  - Docker socket access is constrained or documented
  - public DB ports require explicit opt-in
- Kubernetes:
  - Helm chart renders and rejects invalid values
  - CRD schemas remain structural for operator-facing fields
  - Kind smoke proves platform rollout plus Project create, dependency ordering, pause, resume, destroy, and cleanup

### Regression Suite Additions

- SSO role tamper test.
- Secret placeholder startup rejection tests.
- Backup policy target persistence reload test.
- Migration checksum drift test.
- Request body limit tests for API and MCP.
- Login/MFA throttling tests.
- Cookie-session auth tests.
- Studio one-time-code replay and scope tests.
- Terraform failed-update behavior tests.
- `.env` permission and input validation script tests.

## Suggested Implementation Order

1. Fix SSO role escalation.
2. Fail closed on production secrets and harden setup-generated `.env`.
3. Persist backup policy storage target IDs.
4. Move browser admin sessions off localStorage and add CSRF/origin protection.
5. Replace Studio query tokens.
6. Add request body limits and auth throttling.
7. Update vulnerable Go and frontend dependencies.
8. Harden Docker socket, image users, build context, public DB port defaults, and nginx headers.
9. Add migration checksum protection.
10. Refactor duplicated project-child resource handling.
11. Clean up Terraform update semantics and repeated scaffolding.
12. Gate frontend queries, encode path segments, and improve dialogs.
13. Complete docs and final validation matrix.

## Release And Rollout Notes

- Treat phases 1 through 4 as security releases.
- If changing session behavior, provide a compatibility window for CLI/API token flows.
- If adding required secrets, document upgrade steps before merging deployment changes.
- If adding migration checksum enforcement, ensure existing deployed databases can record baseline checksums safely.
- If changing public DB bind defaults, call it out as a behavior change in install and upgrade docs.

## Completion Criteria

The remediation effort is complete when:

- All P0 and P1 items are implemented, tested, and documented.
- `go test ./...`, frontend build, npm audits, and govulncheck pass or have documented accepted exceptions.
- Manual smoke tests pass for local compose setup, auth, Studio, backup policy persistence, and project lifecycle.
- New regression tests cover each previously identified critical/high issue.
- The codebase has a clear path to reduce project-child duplication without relying on memory of every registry location.
- Helm/operator completion specifically requires Helm lint/template checks, CRD schema checks, operator/provisioner unit tests, and the Kind smoke to pass.
