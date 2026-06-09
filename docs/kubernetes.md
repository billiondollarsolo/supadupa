# Kubernetes Deployment

Supadupa now has two Kubernetes pieces with separate responsibilities:

- Helm installs the platform: CRDs, RBAC, metadata database, control plane, admin UI, ingress, and optional operator.
- The operator watches Supadupa CRDs and owns ongoing reconciliation. The current operator patches `Project` CR status and conditions, renders deterministic per-project runtime ConfigMaps and Secrets, applies Deployments, Services, PVCs, and optional Ingresses for enabled image-backed services, observes managed Deployment availability and PVC bound state before reporting project `Ready=True`, and removes those resources when a project enters `destroying`. It also observes ProjectConfig, ProjectAuthHooks, ProjectBranchClone, ProjectReplica, and RetainedProjectResources CRs and patches validation/observed status for those auxiliary surfaces.

Do not use Helm releases for individual projects. Projects are represented as `Project` and related CRDs so the operator can reconcile status, drift, lifecycle commands, and future workload resources continuously.

## Render The Chart

The chart fails closed unless secrets are supplied or an existing Secret is referenced.

```bash
helm template supadupa charts/supadupa --include-crds \
  --set secrets.secretKey="$(openssl rand -hex 32)" \
  --set secrets.authSecret="$(openssl rand -hex 32)" \
  --set metaDb.password="$(openssl rand -hex 24)" \
  --set operator.enabled=true
```

## Install

```bash
helm upgrade --install supadupa charts/supadupa \
  --namespace supadupa \
  --create-namespace \
  --set secrets.secretKey="$(openssl rand -hex 32)" \
  --set secrets.authSecret="$(openssl rand -hex 32)" \
  --set metaDb.password="$(openssl rand -hex 24)" \
  --set operator.enabled=true
```

Set `ingress.enabled=true`, `ingress.apiHost`, and `ingress.adminHost` when the cluster has an ingress controller. The control plane defaults to the Kubernetes provisioner and sets `SUPADUPA_K8S_MANAGE_CRDS=false` because Helm installs CRDs from `charts/supadupa/crds`.

Set `operator.runtimeNamespaceOverride` when project runtime resources should be reconciled in a namespace different from the Helm release namespace. That namespace must already exist. The chart uses the same runtime namespace for control-plane `SUPADUPA_K8S_NAMESPACE`, operator `SUPADUPA_OPERATOR_NAMESPACE`, and runtime RBAC, so Project CR writes and operator watches stay aligned.

Set `operator.interval` to tune the polling reconcile loop. The value uses Go duration syntax, for example `5s`, `30s`, or `1m`.

The bundled metadata database is intended for small/self-contained installs. Its PostgreSQL container keeps privilege escalation disabled but allows the limited file ownership/mode capabilities required by the official `postgres` entrypoint during first initialization. For stricter production environments, set `metaDb.enabled=false` and provide `controlPlane.metaDsn` for an externally managed PostgreSQL database.

## Project Workloads

The operator renders workload resources only for enabled services that declare an image. A service can declare native workload fields such as `image`, `replicas`, `ports`, `volumes`, and `ingress`, or use compatible config keys such as `image`, `port`, `replicas`, `storageSize`, `storageMountPath`, and `ingressHost`.

Rendered Deployments disable service-account token automounting and apply runtime security defaults from the Project CR, including seccomp, privilege escalation, and dropped capabilities. Individual services can override `runAsNonRoot`, `allowPrivilegeEscalation`, and `dropCapabilities` when upstream image metadata or entrypoints require narrower service-specific handling. Service `command`, `args`, and `configFiles` entries are rendered into Deployments; config files are stored in the Project runtime ConfigMap and mounted read-only as single files with `subPath`. Generated service environment defaults use the operator-created Kubernetes Service names, for example `<project>-db`, `<project>-kong`, and `<project>-rest`. The Kubernetes provisioner uses this for Kong by mounting a `/home/kong/kong.yml` template, expanding secret-backed environment variables inside the pod, writing the final file to `KONG_DECLARATIVE_CONFIG` under `/tmp`, setting `KONG_PREFIX` under `/tmp`, and starting through Kong's image entrypoint. A paused Project keeps resources applied but scales image-backed workloads to zero replicas.

Project secret rotation is also a Project CR update. The provisioner writes managed secret values into `spec.environment` and reapplies the Project; the operator then reconciles the workload Secret named `<project>-environment`, which is the Secret referenced by rendered Deployments.

Destroy in apply mode is an operator handoff. The provisioner applies the Project with `spec.desiredState=destroying`, waits for the operator to report `status.phase=Terminating`, and then deletes rendered CRs. When the API is called with `retain_volumes=true`, rendered service volumes are marked retained before the destroying handoff so the operator preserves matching PVCs.

## Project Status

Use `kubectl get projects.platform.supadupa.dev -n <runtime-namespace>` for a quick phase view and `kubectl describe project <ref> -n <runtime-namespace>` or `kubectl get project <ref> -n <runtime-namespace> -o yaml` for full conditions.

Common `Project.status.phase` values:

- `RuntimeRendered`: the operator rendered project ConfigMaps, Secrets, and workload resources, but live workload availability has not been checked or no image-backed workloads exist.
- `RuntimePending`: resources are rendered, but one or more observed Deployments or PVCs are not available yet.
- `RuntimeReady`: image-backed workloads are rendered and observed Deployments/PVCs are ready or bound.
- `Paused`: desired state is `paused`; image-backed workloads remain defined but are scaled to zero replicas.
- `Terminating`: desired state is `destroying`; the operator has attempted cleanup and is reporting cleanup status before the CR is deleted.
- `Degraded`: the operator rejected the spec or failed to apply, prune, observe, or delete resources. Read `status.message` and condition reasons for the failing step.

Project conditions include `ResourcesRendered`, `WorkloadsRendered`, `WorkloadsAvailable`, and `Ready`. `Ready=True` means the operator's current reconciliation pass considers the project usable. `WorkloadsAvailable=False` includes a message such as an unavailable Deployment or unbound PVC.

Auxiliary CRDs currently report `Observed` with `Ready=False` and a `DataPlanePending` reason when their spec is valid. `Degraded` means the CR shape is invalid or status patching failed. These CRDs are intentionally observed-only until their data-plane reconciliation is implemented.

## Troubleshooting

Check operator logs first:

```bash
kubectl logs -n supadupa deploy/supadupa-operator
```

Confirm the control plane and operator are writing and watching the same runtime namespace:

```bash
kubectl get deploy -n supadupa supadupa-control-plane -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="SUPADUPA_K8S_NAMESPACE")].value}{"\n"}'
kubectl get deploy -n supadupa supadupa-operator -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="SUPADUPA_OPERATOR_NAMESPACE")].value}{"\n"}'
```

Inspect rendered runtime resources by project label:

```bash
kubectl get configmap,secret,deploy,svc,pvc,ingress -n <runtime-namespace> -l supadupa.dev/project=<ref>
```

If a project is stuck in `RuntimePending`, inspect the reported Deployment or PVC:

```bash
kubectl describe deploy -n <runtime-namespace> <ref>-<service>
kubectl describe pvc -n <runtime-namespace> <ref>-<service>-<volume>
kubectl logs -n <runtime-namespace> deploy/<ref>-<service>
```

If status updates fail, verify CRDs have `/status` subresources and operator RBAC can patch them:

```bash
kubectl auth can-i patch projects.platform.supadupa.dev/status -n <runtime-namespace> --as system:serviceaccount:supadupa:supadupa-operator
kubectl auth can-i patch projectconfigs.platform.supadupa.dev/status -n <runtime-namespace> --as system:serviceaccount:supadupa:supadupa-operator
```

Helm owns CRD installation from `charts/supadupa/crds`. Keep CRD changes in the chart and use `python3 scripts/check-kubernetes-crds.py` plus `scripts/check-helm-chart.sh` before upgrading a cluster.

## Kind Smoke

Run the live chart/operator smoke against a temporary Kind cluster:

```bash
KIND_BIN=kind KUBECTL_BIN=kubectl HELM_BIN=helm scripts/check-kubernetes-kind-smoke.sh
```

The script rebuilds local control-plane, admin, and operator images by default, loads them into Kind, installs the chart, waits for the platform rollouts, applies a two-service `Project`, and verifies operator create, dependency wait, pause, resume, destroy, and cleanup behavior. By default it creates a unique temporary Kind cluster name and deletes only that cluster during cleanup. Set `SUPADUPA_KIND_REBUILD_IMAGES=false` to reuse existing local images, or `SUPADUPA_KIND_KEEP_CLUSTER=true` to keep the cluster for debugging after a failure. If you set `SUPADUPA_KIND_CLUSTER` to an existing cluster name, the script refuses to replace it unless `SUPADUPA_KIND_DELETE_EXISTING_CLUSTER=true` is also set.

By default the smoke installs the chart with `projectIsolation.enabled=false` so the single-namespace assertions hold. Set `SUPADUPA_KIND_ISOLATION_SMOKE=true` to run the namespace-per-project path: the chart keeps isolation on, project workloads land in `supadupa-proj-<ref>`, and the smoke asserts the runtime namespace exists with PSA labels, the four NetworkPolicies, the `<ref>-quota` ResourceQuota, the `<ref>-limits` LimitRange, and a `<ref>-runtime` ServiceAccount with `automountServiceAccountToken: false`, then verifies the runtime namespace is deleted (and the control namespace survives) on destroy. These are existence checks that do not require a policy-enforcing CNI. To also exercise live cross-tenant network **denial**, install a policy-enforcing CNI (Calico/Cilium) on the Kind cluster and set `SUPADUPA_KIND_ISOLATION_CNI_ENFORCED=true`; under kindnet (the Kind default) NetworkPolicies are inert so the denial probe is intentionally skipped to avoid a false green.

Set `SUPADUPA_KIND_SUPABASE_CORE_SMOKE=true` to add an opt-in generated Supabase core data-plane check. That mode uses the Kubernetes provisioner renderer to create a second Project with real generated `db`, `kong`, `meta`, `auth`, and `rest` service specs, disables non-core services, waits for the generated DB startup bootstrap to create the required Supabase roles/schemas/grants, waits for the core Project to become `RuntimeReady`, verifies generated images and Kong config mounts, probes DB/Auth/REST/Kong reachability from inside the cluster, then destroys the Project and verifies cleanup. This mode uses upstream Supabase images and is intentionally not the default lightweight CI path. By default, `SUPADUPA_KIND_PRELOAD_CORE_IMAGES=true` preloads any locally cached upstream core-smoke images into the Kind node before workloads start, reducing Docker Hub rate-limit flakiness; set it to `false` to rely only on normal cluster pulls. When `busybox:1.37.0` is not cached locally but `alpine:3.22` is cached, the smoke tags Alpine inside the Kind node as a compatible probe-only fallback so the in-cluster network probe can run without pulling another image.

## Project Network Isolation (namespace-per-project, default on)

The Compose provisioner isolates every project on its own dedicated Docker network (`<ref>-edge`); only that project's edge-facing services and the shared edge-router join it, so projects cannot reach one another's containers. The Kubernetes path now mirrors this with **namespace-per-project isolation, which is the default** (`projectIsolation.enabled=true`).

When isolation is enabled, the operator owns the runtime-namespace lifecycle. For each project it:

- **Creates a per-project namespace** named `supadupa-proj-<ref>` (configurable prefix via `projectIsolation.runtimeNamespacePrefix`). The `Project` CR still lives in the control namespace where the provisioner writes it; only the runtime resources (ConfigMap/Secret/Deployments/Services/PVCs/Ingress/policies/quota/SA) move into the per-project namespace. The control plane stamps `spec.runtimeNamespace` onto each CR so the provisioner and operator agree.
- **Labels the namespace for Pod Security Admission** with `pod-security.kubernetes.io/{enforce,audit,warn}` at the levels in `projectIsolation.podSecurity`. The default `enforce` level is **`baseline`** (not `restricted`): the Supabase `db` container runs as root and re-adds capabilities, which `restricted` rejects. Raise to `restricted` only after verifying the generated db pod or running a root-tolerant database image.
- **Applies a default-deny `NetworkPolicy` set:**
  - `<name>-default-deny` — empty pod selector, `policyTypes: [Ingress, Egress]`, no rules (deny everything).
  - `<name>-allow-intra` — ingress from / egress to pods matching `supadupa.dev/project-ref=<ref>` (intra-project traffic).
  - `<name>-allow-ingress-controller` — ingress from the ingress controller namespace (`namespaceSelector` derived from `projectIsolation.networkPolicy.ingressControllerNamespace`, or an explicit `ingressControllerNamespaceSelector`) to the ingress-exposed pods.
  - `<name>-allow-egress` — egress to DNS (UDP/TCP 53 in `allowDNSNamespace`, default `kube-system`), to the project's own DB pod/port, and to any platform-wide `projectIsolation.networkPolicy.extraEgressCIDRs` / per-project `spec.runtimeNetwork.allowedEgressCidrs`+`externalEgressPorts` (the two are merged). Every external CIDR carries a mandatory `ipBlock.except` for `169.254.0.0/16` (link-local / cloud metadata) when that range falls inside it, and loopback CIDRs are dropped, so a careless broad CIDR (e.g. `0.0.0.0/0`) cannot re-open SSRF to `169.254.169.254`. This mirrors the Compose `internal`/`egress` split.
- **The PSA `audit` and `warn` levels default to `restricted` independently of `enforce`** (which defaults to `baseline`), so policy violations are surfaced via the API server even where they are not blocked. `enforce`/`audit`/`warn` and their `*Version` pins are all honored from `projectIsolation.podSecurity`.
- **Provisions a non-automounting runtime ServiceAccount** `<name>-runtime` (`automountServiceAccountToken: false`) and binds every project workload pod to it via `serviceAccountName`.
- **Applies an optional `ResourceQuota`** (`projectIsolation.resourceQuota`) and **`LimitRange`** (`projectIsolation.limitRange`) per namespace.
- **NetworkPolicy generation is toggleable** via `projectIsolation.networkPolicy.enabled`; when false the namespace, ServiceAccount, quota and limits are still applied but no NetworkPolicies are created.
- On destroy, deletes the runtime resources and then deletes the runtime namespace; the control namespace is never deleted (operator refuses). The operator also sets a `supadupa.dev/runtime-namespace` **finalizer** on live Project CRs, so a direct CR deletion (kubectl delete, GitOps prune, parent-namespace delete) still triggers the same teardown before the CR is garbage-collected — runtime namespaces are not orphaned.

**CNI prerequisite:** NetworkPolicy enforcement requires a policy-enforcing CNI such as **Calico or Cilium**. Under **kindnet** (the Kind default) and other flat CNIs the policies render and apply but do **not** block traffic — isolation is then namespace + RBAC + PSA + quota only, and cross-tenant network denial is *not* enforced. Treat a policy-enforcing CNI as a hard prerequisite for multi-tenant production.

**RBAC:** with isolation enabled the operator needs a cluster-scoped `ClusterRole`/`ClusterRoleBinding` (it creates/deletes namespaces and applies `networkpolicies`/`resourcequotas`/`limitranges`/`serviceaccounts` across namespaces). The chart emits this automatically. With `projectIsolation.enabled=false` it emits the legacy namespaced `Role`/`RoleBinding` instead.

**Opt-out / legacy mode:** set `projectIsolation.enabled=false` (chart) — which wires `SUPADUPA_K8S_ISOLATION=false` (control plane) and `SUPADUPA_OPERATOR_ISOLATION=false` (operator) — to restore the legacy single-namespace behavior (all projects reconciled into `SUPADUPA_K8S_NAMESPACE`, no namespaces/policies/quota created). This path is byte-for-byte unchanged from before the upgrade.

**Migration:** there is no in-place move of existing projects from the shared namespace into per-project namespaces (it would orphan PVCs). Existing single-namespace installs either stay on `projectIsolation.enabled=false`, or drain/recreate projects to migrate. Do not attempt automatic data migration.

**Operator runtime knobs:** `operator.health.port` (default `8081`) exposes `/healthz` (liveness) and `/readyz`. Readiness reflects *ongoing* reconcile health — it flips to ready after a successful reconcile and back to not-ready on a failed one (and reports not-ready on followers when leader election is enabled), rather than latching ready after the first success. `operator.leaderElection.enabled` (default false) gates Lease-based leader election against a `coordination.k8s.io/v1` Lease (`<release>-operator-leader`): only the lease holder reconciles, followers poll until the holder's lease expires. With leader election off the operator is pinned to a single replica (`replicas: 1`) regardless of `operator.replicaCount`, and the leases RBAC is not granted. `operator.metrics.enabled` (default false) binds a real `/metrics` endpoint (dependency-free Prometheus text format: `supadupa_operator_reconcile_total`, `..._reconcile_errors_total`, `..._last_reconcile_success`, and `..._leader` when leader election is on) on `SUPADUPA_OPERATOR_METRICS_ADDR` plus a ClusterIP Service; `operator.serviceMonitor.enabled` adds a Prometheus-Operator `ServiceMonitor`. `operator.podDisruptionBudget.enabled` adds a PDB (note: a single-replica operator with `minAvailable:1` blocks voluntary node drains — only enable it with leader election + `replicaCount>1`). `controlPlane.podDisruptionBudget.enabled` adds a PDB for the request-serving control plane (use when `controlPlane.replicaCount>1`).

The Compose model this mirrors is in `internal/provisioner/compose/compose.go` (`ensureEdgeNetwork`).

## Current Limits

- **Cross-project network isolation is implemented** (see "Project Network Isolation" above): namespace-per-project with default-deny `NetworkPolicy`, runtime ServiceAccount, PSA labels, and optional quota/limits, on by default. The remaining caveat is that network denial is only enforced under a policy-enforcing CNI (Calico/Cilium); under kindnet the policies are inert. Lease-based leader election and a real Prometheus `/metrics` endpoint are implemented but ship off by default (operator pinned to a single replica unless leader election is enabled).
- The operator reconciles generic image-backed service workloads from Project CRs, including Deployments, Services, PVCs, optional Ingresses, security defaults, commands/args, probes, writable-path mounts, ConfigMap-backed config-file mounts, dependency wait init containers, observed Deployment/PVC availability, pause scale-to-zero, stale-resource pruning, and destroy cleanup for non-retained resources. Auxiliary ProjectConfig, ProjectAuthHooks, ProjectBranchClone, ProjectReplica, and RetainedProjectResources CRDs currently receive observed/degraded status; deeper data-plane behavior for those CRDs remains future work.
- The Kind smoke validates Helm platform startup and a generic two-service Project workload lifecycle against a real Kubernetes API. It asserts command/args, deterministic service env ordering, ConfigMap-backed read-only file mounts, read-only root filesystem, writable emptyDir mounts, HTTP probes, runtime security defaults, Project environment to workload Secret sync, non-root dependency wait init containers, PVC binding, pause/resume/destroy behavior, and cleanup. The optional Supabase core mode now passes for generated `db`, `kong`, `meta`, `auth`, and `rest` service specs plus basic in-cluster reachability, with the generated DB startup bootstrap applying required roles, schemas, grants, optional extensions, and publication setup. The Kubernetes provisioner renders additional baseline operator-ready Supabase service specs, project-scoped Kubernetes service DNS in generated environment defaults, and a Kong startup path that expands a mounted declarative config template inside the pod, including Kubernetes service upstreams, key-auth consumers, ACLs, and request-transformer placeholders. Full Supabase Kubernetes data-plane parity still needs live-cluster validation of storage, realtime, functions, pooler, analytics, ingress behavior, storage classes, and full gateway auth/transform compatibility.
