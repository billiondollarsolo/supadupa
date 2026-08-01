#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
KIND_BIN="${KIND_BIN:-kind}"
KUBECTL_BIN="${KUBECTL_BIN:-kubectl}"
HELM_BIN="${HELM_BIN:-helm}"
CLUSTER="${SUPADUPA_KIND_CLUSTER:-supadupa-smoke-$(date +%s)-$$}"
NAMESPACE="${SUPADUPA_KIND_NAMESPACE:-supadupa}"
PROJECT="${SUPADUPA_KIND_PROJECT:-kind-smoke}"
CORE_PROJECT="${SUPADUPA_KIND_SUPABASE_CORE_PROJECT:-kind-core}"
KEEP_CLUSTER="${SUPADUPA_KIND_KEEP_CLUSTER:-false}"
REBUILD_IMAGES="${SUPADUPA_KIND_REBUILD_IMAGES:-true}"
SUPABASE_CORE_SMOKE="${SUPADUPA_KIND_SUPABASE_CORE_SMOKE:-false}"
# Opt-in: enable storage/realtime/functions/pooler/analytics on the core smoke Project
# and wait for those Deployments. Default false so CI stays lightweight.
DATAPLANE_SMOKE="${SUPADUPA_KIND_DATAPLANE_SMOKE:-false}"
PRELOAD_CORE_IMAGES="${SUPADUPA_KIND_PRELOAD_CORE_IMAGES:-true}"
DELETE_EXISTING_CLUSTER="${SUPADUPA_KIND_DELETE_EXISTING_CLUSTER:-false}"
# Namespace-per-project isolation smoke. Default false because kindnet does NOT
# enforce NetworkPolicy: the cross-tenant denial assertion is a false-green
# without a policy-enforcing CNI (Calico/Cilium). When false, the lightweight
# path installs the chart with projectIsolation.enabled=false so every existing
# single-namespace assertion below keeps holding. When true, the chart keeps
# isolation on (its default), workloads land in supadupa-proj-<ref> namespaces,
# and the operator-extras assertions / cross-tenant denial probe run.
ISOLATION_SMOKE="${SUPADUPA_KIND_ISOLATION_SMOKE:-false}"
CLUSTER_CREATED=false

# RUNTIME_NS is the namespace project workloads land in. In isolation mode the
# operator creates supadupa-proj-<ref>; in legacy mode it is the control NS.
RUNTIME_NS_PREFIX="supadupa-proj-"
if [[ "$ISOLATION_SMOKE" == "true" ]]; then
  PROJECT_RUNTIME_NS="${RUNTIME_NS_PREFIX}${PROJECT}"
else
  PROJECT_RUNTIME_NS="$NAMESPACE"
fi

CONTROL_IMAGE="${SUPADUPA_CONTROL_PLANE_IMAGE:-supadupa-control-plane:ci}"
ADMIN_IMAGE="${SUPADUPA_ADMIN_IMAGE:-supadupa-admin:ci}"
OPERATOR_IMAGE="${SUPADUPA_OPERATOR_IMAGE:-supadupa-operator:ci}"
CORE_SMOKE_IMAGES=(
  "postgres:16-alpine"
  "supabase/postgres:15.8.1.060"
  "supabase/gotrue:v2.189.0"
  "postgrest/postgrest:v14.12"
  "supabase/postgres-meta:v0.96.6"
  "kong/kong:3.9.1"
  "busybox:1.37.0"
)

require_tool() {
  local bin="$1"
  local name="$2"
  if ! command -v "$bin" >/dev/null 2>&1; then
    echo "$name is required; set ${name^^}_BIN or install $name" >&2
    exit 1
  fi
}

cleanup() {
  local exit_code=$?
  if [[ "$exit_code" -ne 0 ]]; then
    echo "Kubernetes smoke failed; dumping namespace diagnostics" >&2
    "$KUBECTL_BIN" -n "$NAMESPACE" get all,pvc -o wide >&2 || true
    "$KUBECTL_BIN" -n "$NAMESPACE" get project "$PROJECT" -o yaml >&2 || true
    "$KUBECTL_BIN" -n "$NAMESPACE" describe pods >&2 || true
    "$KUBECTL_BIN" -n "$NAMESPACE" get events --sort-by=.lastTimestamp >&2 || true
    "$KUBECTL_BIN" -n "$NAMESPACE" logs deployment/supadupa-operator --tail=100 >&2 || true
    "$KUBECTL_BIN" -n "$NAMESPACE" logs deployment/supadupa-control-plane --tail=100 >&2 || true
    "$KUBECTL_BIN" -n "$NAMESPACE" logs statefulset/supadupa-meta-db --tail=100 >&2 || true
    if [[ "$ISOLATION_SMOKE" == "true" && "$PROJECT_RUNTIME_NS" != "$NAMESPACE" ]]; then
      "$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get all,pvc,networkpolicy,resourcequota,limitrange,serviceaccount -o wide >&2 || true
      "$KUBECTL_BIN" get namespace "$PROJECT_RUNTIME_NS" -o yaml >&2 || true
    fi
  fi
  if [[ "$KEEP_CLUSTER" == "true" ]]; then
    echo "SUPADUPA_KIND_KEEP_CLUSTER=true; leaving kind cluster $CLUSTER in place" >&2
    return
  fi
  if [[ "$CLUSTER_CREATED" == "true" ]]; then
    "$KIND_BIN" delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
  fi
}

retry_until() {
  local description="$1"
  local attempts="${2:-60}"
  local delay="${3:-2}"
  shift 3
  for _ in $(seq 1 "$attempts"); do
    if "$@"; then
      return 0
    fi
    sleep "$delay"
  done
  echo "timed out waiting for $description" >&2
  return 1
}

image_exists() {
  docker image inspect "$1" >/dev/null 2>&1
}

ensure_image() {
  local image="$1"
  local dockerfile="$2"
  if [[ "$REBUILD_IMAGES" != "true" ]] && image_exists "$image"; then
    return
  fi
  docker build -f "$ROOT/$dockerfile" -t "$image" "$ROOT"
}

preload_cached_image_to_kind() {
  local image="$1"
  local node="${CLUSTER}-control-plane"
  if ! image_exists "$image"; then
    if [[ "$image" == "busybox:1.37.0" ]] && image_exists "alpine:3.22"; then
      echo "kind core smoke image $image is not cached locally; using cached alpine:3.22 as a compatible smoke-only fallback"
      docker save "alpine:3.22" | docker exec --privileged -i "$node" \
        ctr --namespace=k8s.io images import --platform linux/amd64 --snapshotter=overlayfs - >/dev/null
      docker exec --privileged "$node" \
        ctr --namespace=k8s.io images tag --force docker.io/library/alpine:3.22 docker.io/library/busybox:1.37.0 >/dev/null
      return
    fi
    echo "kind core smoke image $image is not cached locally; Kubernetes will pull it if needed"
    return
  fi
  echo "Preloading cached core smoke image $image into kind node $node"
  if ! docker save "$image" | docker exec --privileged -i "$node" \
    ctr --namespace=k8s.io images import --platform linux/amd64 --snapshotter=overlayfs - >/dev/null; then
    echo "warning: could not preload $image into kind; Kubernetes will pull it if needed" >&2
  fi
}

preload_core_smoke_images() {
  if [[ "$SUPABASE_CORE_SMOKE" != "true" || "$PRELOAD_CORE_IMAGES" != "true" ]]; then
    return
  fi
  for image in "${CORE_SMOKE_IMAGES[@]}"; do
    preload_cached_image_to_kind "$image"
  done
}

project_phase_is() {
  local project="$1"
  local expected="$1"
  if [[ "$#" -eq 2 ]]; then
    expected="$2"
  else
    project="$PROJECT"
  fi
  [[ "$("$KUBECTL_BIN" -n "$NAMESPACE" get project "$project" -o jsonpath='{.status.phase}' 2>/dev/null || true)" == "$expected" ]]
}

deployment_replicas_is() {
  local project="$1"
  local expected="$1"
  if [[ "$#" -eq 2 ]]; then
    expected="$2"
  else
    project="$PROJECT"
  fi
  [[ "$("$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get deployment "$project-web" -o jsonpath='{.spec.replicas}' 2>/dev/null || true)" == "$expected" ]]
}

secret_key_is() {
  local key="$1"
  local expected="$2"
  local encoded
  encoded="$("$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get secret "$PROJECT-environment" -o "jsonpath={.data.$key}" 2>/dev/null || true)"
  if [[ -z "$encoded" ]]; then
    return 1
  fi
  [[ "$(printf '%s' "$encoded" | base64 -d)" == "$expected" ]]
}

assert_kube_json() {
  local description="$1"
  local kind="$2"
  local name="$3"
  local check="$4"
  if ! "$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get "$kind" "$name" -o json | python3 -c '
import json
import sys

obj = json.load(sys.stdin)
exec(sys.argv[1], {"obj": obj})
' "$check"; then
    echo "Kubernetes assertion failed: $description" >&2
    return 1
  fi
}

resource_absent() {
  "$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get "$1" "$2" >/dev/null 2>&1 && return 1
  return 0
}

resource_present() {
  "$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get "$1" "$2" >/dev/null 2>&1
}

verify_supabase_core_database() {
  retry_until "Supabase core db deployment exists" 90 2 resource_present deployment "$CORE_PROJECT-db"
  "$KUBECTL_BIN" -n "$NAMESPACE" rollout status "deployment/$CORE_PROJECT-db" --timeout=300s
  retry_until "Supabase core db bootstrap objects" 90 2 supabase_core_database_bootstrap_ready
}

supabase_core_database_bootstrap_ready() {
  "$KUBECTL_BIN" -n "$NAMESPACE" exec -i "deployment/$CORE_PROJECT-db" -- \
    psql -v ON_ERROR_STOP=1 -U postgres -d postgres <<'SQL'
DO $$
DECLARE
  missing text;
BEGIN
  SELECT name INTO missing
  FROM (VALUES
    ('role anon', EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon')),
    ('role authenticated', EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated')),
    ('role service_role', EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'service_role')),
    ('role authenticator', EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticator' AND rolcanlogin)),
    ('role supabase_auth_admin', EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'supabase_auth_admin' AND rolcanlogin)),
    ('role supabase_storage_admin', EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'supabase_storage_admin' AND rolcanlogin)),
    ('schema auth', EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'auth')),
    ('schema storage', EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'storage')),
    ('schema graphql_public', EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'graphql_public'))
  ) AS required(name, present)
  WHERE NOT present
  LIMIT 1;

  IF missing IS NOT NULL THEN
    RAISE EXCEPTION 'missing Kubernetes DB bootstrap object: %', missing;
  END IF;
END
$$;
SQL
}

run_supabase_core_smoke() {
  local render_root
  local manifest
  local core_services=(db kong meta auth rest)
  local dataplane_services=(storage realtime functions pooler analytics)
  local all_services=("${core_services[@]}")
  render_root="$(mktemp -d)"
  # Propagate dataplane flag into the Go renderer (opt-in; default keeps CI light).
  manifest="$(SUPADUPA_KIND_DATAPLANE_SMOKE="$DATAPLANE_SMOKE" go run "$ROOT/scripts/kubernetes-core-smoke-renderer" "$render_root" "$NAMESPACE" "$CORE_PROJECT" "apps.example.test")"
  "$KUBECTL_BIN" -n "$NAMESPACE" apply -f "$manifest"

  verify_supabase_core_database
  retry_until "Supabase core Project status RuntimeReady" 150 4 project_phase_is "$CORE_PROJECT" RuntimeReady
  if [[ "$DATAPLANE_SMOKE" == "true" ]]; then
    all_services+=("${dataplane_services[@]}")
  fi
  for service in "${all_services[@]}"; do
    "$KUBECTL_BIN" -n "$NAMESPACE" rollout status "deployment/$CORE_PROJECT-$service" --timeout=300s
    "$KUBECTL_BIN" -n "$NAMESPACE" get service "$CORE_PROJECT-$service" >/dev/null
  done
  "$KUBECTL_BIN" -n "$NAMESPACE" get pvc "$CORE_PROJECT-db-data" >/dev/null
  if [[ "$DATAPLANE_SMOKE" == "true" ]]; then
    "$KUBECTL_BIN" -n "$NAMESPACE" get pvc "$CORE_PROJECT-storage-data" >/dev/null
  fi

  if [[ "$DATAPLANE_SMOKE" == "true" ]]; then
    assert_kube_json "Supabase dataplane Project uses provisioner-rendered real images" project "$CORE_PROJECT" '
services = obj["spec"]["services"]
assert services["db"]["image"].startswith("supabase/postgres:"), services["db"]["image"]
assert services["kong"]["image"].startswith("kong/kong:"), services["kong"]["image"]
assert services["auth"]["image"].startswith("supabase/gotrue:"), services["auth"]["image"]
assert services["rest"]["image"].startswith("postgrest/postgrest:"), services["rest"]["image"]
for name, repo in (
    ("storage", "supabase/storage-api:"),
    ("realtime", "supabase/realtime:"),
    ("functions", "supabase/edge-runtime:"),
    ("pooler", "supabase/supavisor:"),
    ("analytics", "supabase/logflare:"),
):
    svc = services[name]
    assert svc["enabled"] is True, (name, svc)
    assert svc["image"].startswith(repo), (name, svc["image"])
    assert "@sha256:" in svc["image"], (name, svc["image"])
assert any(p.get("port") == 5000 for p in services["storage"].get("ports") or []), services["storage"]
assert any(p.get("port") == 4000 for p in services["realtime"].get("ports") or []), services["realtime"]
assert any(p.get("port") == 9000 for p in services["functions"].get("ports") or []), services["functions"]
assert any(p.get("name") == "transaction" and p.get("port") == 6543 for p in services["pooler"].get("ports") or []), services["pooler"]
assert any(p.get("port") == 4000 for p in services["analytics"].get("ports") or []), services["analytics"]
'
  else
    assert_kube_json "Supabase core Project uses provisioner-rendered real images" project "$CORE_PROJECT" '
services = obj["spec"]["services"]
assert services["db"]["image"].startswith("supabase/postgres:"), services["db"]["image"]
assert services["kong"]["image"].startswith("kong/kong:"), services["kong"]["image"]
assert services["auth"]["image"].startswith("supabase/gotrue:"), services["auth"]["image"]
assert services["rest"]["image"].startswith("postgrest/postgrest:"), services["rest"]["image"]
assert services["storage"]["enabled"] is False, services["storage"]
assert services["realtime"]["enabled"] is False, services["realtime"]
'
  fi

  assert_kube_json "Supabase core Kong Deployment mounts generated declarative config" deployment "$CORE_PROJECT-kong" '
pod = obj["spec"]["template"]["spec"]
container = pod["containers"][0]
mounts = {mount["name"]: mount for mount in container["volumeMounts"]}
assert mounts["config-kong-yml"]["mountPath"] == "/home/kong/kong.yml", mounts
assert mounts["config-kong-yml"]["readOnly"] is True, mounts
assert container["command"] == ["/bin/sh", "-ec"], container.get("command")
assert "kong docker-start" in container["args"][0], container.get("args")
'

  "$KUBECTL_BIN" -n "$NAMESPACE" run "$CORE_PROJECT-probe" \
    --rm \
    --attach \
    --restart=Never \
    --image=busybox:1.37.0 \
    --command -- sh -ec "
      nc -z $CORE_PROJECT-db 5432
      nc -z $CORE_PROJECT-rest 3000
      wget -qO- http://$CORE_PROJECT-auth:9999/health >/dev/null
      wget -qO- http://$CORE_PROJECT-kong:8000/auth/v1/health >/dev/null
    "

  "$KUBECTL_BIN" -n "$NAMESPACE" patch project "$CORE_PROJECT" --type merge -p '{"spec":{"desiredState":"destroying"}}'
  retry_until "Supabase core Project status Terminating" 90 2 project_phase_is "$CORE_PROJECT" Terminating
  for service in "${all_services[@]}"; do
    retry_until "destroyed Supabase core $service deployment" 90 2 resource_absent deployment "$CORE_PROJECT-$service"
    retry_until "destroyed Supabase core $service service" 90 2 resource_absent service "$CORE_PROJECT-$service"
  done
  retry_until "destroyed Supabase core db pvc" 90 2 resource_absent pvc "$CORE_PROJECT-db-data"
  "$KUBECTL_BIN" -n "$NAMESPACE" delete project "$CORE_PROJECT" --ignore-not-found
  rm -rf "$render_root"
}

require_tool "$KIND_BIN" kind
require_tool "$KUBECTL_BIN" kubectl
require_tool "$HELM_BIN" helm
require_tool docker docker
require_tool python3 python3
require_tool base64 base64

trap cleanup EXIT

if "$KIND_BIN" get clusters | grep -qx "$CLUSTER"; then
  if [[ "$DELETE_EXISTING_CLUSTER" == "true" ]]; then
    "$KIND_BIN" delete cluster --name "$CLUSTER" >/dev/null
  else
    echo "kind cluster $CLUSTER already exists; choose a different SUPADUPA_KIND_CLUSTER or set SUPADUPA_KIND_DELETE_EXISTING_CLUSTER=true to replace it" >&2
    exit 1
  fi
fi
"$KIND_BIN" create cluster --name "$CLUSTER" --wait 120s
CLUSTER_CREATED=true

ensure_image "$CONTROL_IMAGE" "deploy/Dockerfile.control-plane"
ensure_image "$ADMIN_IMAGE" "deploy/Dockerfile.admin"
ensure_image "$OPERATOR_IMAGE" "deploy/Dockerfile.operator"

"$KIND_BIN" load docker-image --name "$CLUSTER" "$CONTROL_IMAGE"
"$KIND_BIN" load docker-image --name "$CLUSTER" "$ADMIN_IMAGE"
"$KIND_BIN" load docker-image --name "$CLUSTER" "$OPERATOR_IMAGE"
preload_core_smoke_images

"$HELM_BIN" upgrade --install supadupa "$ROOT/charts/supadupa" \
  --namespace "$NAMESPACE" \
  --create-namespace \
  --set secrets.secretKey=test-secret-key-change-me-000000000000 \
  --set secrets.authSecret=test-auth-secret-change-me-000000000000 \
  --set metaDb.password=test-postgres-password-change-me \
  --set global.imagePullPolicy=IfNotPresent \
  --set controlPlane.image.repository="${CONTROL_IMAGE%:*}" \
  --set controlPlane.image.tag="${CONTROL_IMAGE##*:}" \
  --set admin.image.repository="${ADMIN_IMAGE%:*}" \
  --set admin.image.tag="${ADMIN_IMAGE##*:}" \
  --set operator.enabled=true \
  --set operator.interval=2s \
  --set operator.image.repository="${OPERATOR_IMAGE%:*}" \
  --set operator.image.tag="${OPERATOR_IMAGE##*:}" \
  --set projectIsolation.enabled="$ISOLATION_SMOKE"

"$KUBECTL_BIN" -n "$NAMESPACE" rollout status statefulset/supadupa-meta-db --timeout=180s
"$KUBECTL_BIN" -n "$NAMESPACE" rollout status deployment/supadupa-control-plane --timeout=180s
"$KUBECTL_BIN" -n "$NAMESPACE" rollout status deployment/supadupa-admin --timeout=180s
"$KUBECTL_BIN" -n "$NAMESPACE" rollout status deployment/supadupa-operator --timeout=180s

cat <<EOF | "$KUBECTL_BIN" -n "$NAMESPACE" apply -f -
apiVersion: platform.supadupa.dev/v1alpha1
kind: Project
metadata:
  name: $PROJECT
spec:
  ref: $PROJECT
  desiredState: running
  domain: apps.example.test
  environment:
    KIND_SMOKE_SECRET: present
  runtimeSecurityDefaults:
    seccompProfile: RuntimeDefault
    allowPrivilegeEscalation: false
    dropCapabilities:
      - ALL
  services:
    db:
      enabled: true
      image: $ADMIN_IMAGE
      replicas: 1
      ports:
        - name: http
          port: 8080
          targetPort: 8080
      readinessProbe:
        type: http
        path: /healthz
        port: 8080
      livenessProbe:
        type: http
        path: /healthz
        port: 8080
    web:
      enabled: true
      image: $ADMIN_IMAGE
      command:
        - /bin/sh
        - -ec
      args:
        - |
          test "\$(cat /etc/nginx/kind-smoke.conf)" = "kind-smoke-config"
          exec nginx -g 'daemon off;'
      replicas: 1
      dependsOn:
        - service: db
          port: 8080
      ports:
        - name: http
          port: 8080
          targetPort: 8080
      env:
        Z_LAST: z
        A_FIRST: a
      readinessProbe:
        type: http
        path: /healthz
        port: 8080
      livenessProbe:
        type: http
        path: /healthz
        port: 8080
      readOnlyRootFilesystem: true
      volumes:
        - name: data
          mountPath: /smoke-data
          size: 10Mi
      configFiles:
        - name: smoke-config
          mountPath: /etc/nginx/kind-smoke.conf
          content: |
            kind-smoke-config
      writablePaths:
        - name: tmp
          mountPath: /tmp
        - name: run
          mountPath: /run
        - name: nginx-cache
          mountPath: /var/cache/nginx
EOF

retry_until "Project status RuntimeReady" 90 2 project_phase_is "$PROJECT" RuntimeReady
"$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" rollout status "deployment/$PROJECT-db" --timeout=180s
"$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" rollout status "deployment/$PROJECT-web" --timeout=180s
"$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get configmap "$PROJECT-runtime" >/dev/null
"$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get secret "$PROJECT-environment" >/dev/null
"$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get service "$PROJECT-db" >/dev/null
"$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get service "$PROJECT-web" >/dev/null
"$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get pvc "$PROJECT-web-data" >/dev/null

assert_kube_json "Project runtime ConfigMap includes service config file content" configmap "$PROJECT-runtime" '
assert obj["data"]["service-web-smoke-config"] == "kind-smoke-config\n", obj["data"]
'

assert_kube_json "web Deployment renders advanced operator workload fields" deployment "$PROJECT-web" '
pod = obj["spec"]["template"]["spec"]
assert pod["automountServiceAccountToken"] is False, pod
assert pod["securityContext"]["seccompProfile"]["type"] == "RuntimeDefault", pod["securityContext"]
container = pod["containers"][0]
assert container["command"] == ["/bin/sh", "-ec"], container.get("command")
assert "cat /etc/nginx/kind-smoke.conf" in container["args"][0], container.get("args")
env_names = [entry["name"] for entry in container.get("env", [])]
assert env_names == ["A_FIRST", "Z_LAST"], env_names
security = container["securityContext"]
assert security["allowPrivilegeEscalation"] is False, security
assert security["readOnlyRootFilesystem"] is True, security
assert security["capabilities"]["drop"] == ["ALL"], security
readiness_http = container["readinessProbe"]["httpGet"]
liveness_http = container["livenessProbe"]["httpGet"]
assert readiness_http["path"] == "/healthz" and readiness_http["port"] == 8080, container["readinessProbe"]
assert liveness_http["path"] == "/healthz" and liveness_http["port"] == 8080, container["livenessProbe"]
mounts = {mount["name"]: mount for mount in container["volumeMounts"]}
assert mounts["config-smoke-config"]["mountPath"] == "/etc/nginx/kind-smoke.conf", mounts
assert mounts["config-smoke-config"]["subPath"] == "service-web-smoke-config", mounts
assert mounts["config-smoke-config"]["readOnly"] is True, mounts
assert mounts["writable-tmp"]["mountPath"] == "/tmp", mounts
assert mounts["writable-run"]["mountPath"] == "/run", mounts
assert mounts["writable-nginx-cache"]["mountPath"] == "/var/cache/nginx", mounts
volumes = {volume["name"]: volume for volume in pod["volumes"]}
config_items = volumes["config-smoke-config"]["configMap"]["items"]
assert config_items == [{"key": "service-web-smoke-config", "path": "service-web-smoke-config"}], config_items
assert volumes["writable-tmp"]["emptyDir"] == {}, volumes
assert volumes["writable-run"]["emptyDir"] == {}, volumes
assert volumes["writable-nginx-cache"]["emptyDir"] == {}, volumes
init = pod["initContainers"][0]
assert init["name"] == "wait-db", init
assert init["command"] == ["sh", "-c", "until nc -z '"$PROJECT"'-db 8080; do sleep 2; done"], init["command"]
assert init["securityContext"]["readOnlyRootFilesystem"] is True, init["securityContext"]
assert init["securityContext"]["allowPrivilegeEscalation"] is False, init["securityContext"]
assert init["securityContext"]["runAsNonRoot"] is True, init["securityContext"]
assert init["securityContext"]["runAsUser"] == 65534, init["securityContext"]
assert init["securityContext"]["runAsGroup"] == 65534, init["securityContext"]
'

if [[ "$ISOLATION_SMOKE" == "true" ]]; then
  echo "isolation smoke: asserting per-project namespace, PSA labels, NetworkPolicies, ResourceQuota, runtime SA"

  # The runtime namespace exists and carries Pod Security Admission labels.
  "$KUBECTL_BIN" get namespace "$PROJECT_RUNTIME_NS" >/dev/null
  assert_psa() {
    local key="$1"
    local value
    value="$("$KUBECTL_BIN" get namespace "$PROJECT_RUNTIME_NS" -o "jsonpath={.metadata.labels.pod-security\\.kubernetes\\.io/$key}" 2>/dev/null || true)"
    [[ -n "$value" ]]
  }
  for psa in enforce audit warn; do
    if ! assert_psa "$psa"; then
      echo "isolation smoke: namespace $PROJECT_RUNTIME_NS missing PSA label pod-security.kubernetes.io/$psa" >&2
      exit 1
    fi
  done

  # Default-deny + allow policies, runtime SA (automount disabled), quota/limits.
  "$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get networkpolicy "$PROJECT-default-deny" >/dev/null
  "$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get networkpolicy "$PROJECT-allow-intra" >/dev/null
  "$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get networkpolicy "$PROJECT-allow-ingress-controller" >/dev/null
  "$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get networkpolicy "$PROJECT-allow-egress" >/dev/null
  "$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get resourcequota "$PROJECT-quota" >/dev/null
  "$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" get limitrange "$PROJECT-limits" >/dev/null

  assert_kube_json "default-deny NetworkPolicy denies all ingress + egress" \
    networkpolicy "$PROJECT-default-deny" '
ingress = obj["spec"].get("ingress", [])
egress = obj["spec"].get("egress", [])
assert obj["spec"].get("podSelector", {}) == {}, obj["spec"].get("podSelector")
assert sorted(obj["spec"]["policyTypes"]) == ["Egress", "Ingress"], obj["spec"]["policyTypes"]
assert ingress == [], ingress
assert egress == [], egress
'

  runtime_sa_namespace="$PROJECT_RUNTIME_NS"
  if ! "$KUBECTL_BIN" -n "$runtime_sa_namespace" get serviceaccount "$PROJECT-runtime" -o json | python3 -c '
import json, sys
obj = json.load(sys.stdin)
assert obj.get("automountServiceAccountToken") is False, obj.get("automountServiceAccountToken")
'; then
    echo "isolation smoke: runtime ServiceAccount $PROJECT-runtime must set automountServiceAccountToken=false" >&2
    exit 1
  fi

  # Cross-tenant network denial requires a policy-enforcing CNI. Only run the
  # live denial probe when explicitly told the cluster has one; otherwise the
  # existence checks above are the cheap, CNI-independent guarantee.
  if [[ "${SUPADUPA_KIND_ISOLATION_CNI_ENFORCED:-false}" == "true" ]]; then
    echo "isolation smoke: probing intra-namespace reachability + cross-namespace denial"
    # Intra-namespace: the web pod must reach the project's own db service.
    "$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" run "$PROJECT-intra-probe" \
      --rm -i --restart=Never --image="$ADMIN_IMAGE" --command -- \
      sh -ec "nc -z -w 5 $PROJECT-db 8080"
    # Cross-namespace: a pod in the control namespace must NOT reach the
    # project's db service (default-deny ingress should refuse/time out).
    if "$KUBECTL_BIN" -n "$NAMESPACE" run "$PROJECT-deny-probe" \
      --rm -i --restart=Never --image="$ADMIN_IMAGE" --command -- \
      sh -ec "nc -z -w 5 $PROJECT-db.$PROJECT_RUNTIME_NS.svc 8080"; then
      echo "isolation smoke: cross-namespace probe reached $PROJECT-db; default-deny NetworkPolicy not enforced" >&2
      exit 1
    fi
  else
    echo "isolation smoke: SUPADUPA_KIND_ISOLATION_CNI_ENFORCED!=true; skipping live cross-tenant denial probe (kindnet does not enforce NetworkPolicy)"
  fi
fi

"$KUBECTL_BIN" -n "$NAMESPACE" patch project "$PROJECT" --type merge -p '{"spec":{"environment":{"KIND_SMOKE_SECRET":"rotated","KIND_SMOKE_ROTATED":"yes"}}}'
retry_until "operator-updated workload Secret from Project environment" 90 2 secret_key_is KIND_SMOKE_SECRET rotated
retry_until "operator-added workload Secret key from Project environment" 90 2 secret_key_is KIND_SMOKE_ROTATED yes

"$KUBECTL_BIN" -n "$NAMESPACE" patch project "$PROJECT" --type merge -p '{"spec":{"desiredState":"paused"}}'
retry_until "Project status Paused" 90 2 project_phase_is "$PROJECT" Paused
retry_until "paused workload replicas=0" 90 2 deployment_replicas_is "$PROJECT" 0

"$KUBECTL_BIN" -n "$NAMESPACE" patch project "$PROJECT" --type merge -p '{"spec":{"desiredState":"running"}}'
retry_until "Project status RuntimeReady after resume" 90 2 project_phase_is "$PROJECT" RuntimeReady
retry_until "resumed workload replicas=1" 90 2 deployment_replicas_is "$PROJECT" 1
"$KUBECTL_BIN" -n "$PROJECT_RUNTIME_NS" rollout status "deployment/$PROJECT-web" --timeout=180s

"$KUBECTL_BIN" -n "$NAMESPACE" patch project "$PROJECT" --type merge -p '{"spec":{"desiredState":"destroying"}}'
retry_until "Project status Terminating" 90 2 project_phase_is "$PROJECT" Terminating
retry_until "destroyed db deployment" 90 2 resource_absent deployment "$PROJECT-db"
retry_until "destroyed deployment" 90 2 resource_absent deployment "$PROJECT-web"
retry_until "destroyed db service" 90 2 resource_absent service "$PROJECT-db"
retry_until "destroyed service" 90 2 resource_absent service "$PROJECT-web"
retry_until "destroyed pvc" 90 2 resource_absent pvc "$PROJECT-web-data"
retry_until "destroyed configmap" 90 2 resource_absent configmap "$PROJECT-runtime"
retry_until "destroyed secret" 90 2 resource_absent secret "$PROJECT-environment"

if [[ "$ISOLATION_SMOKE" == "true" ]]; then
  runtime_ns_absent() {
    ! "$KUBECTL_BIN" get namespace "$PROJECT_RUNTIME_NS" >/dev/null 2>&1
  }
  retry_until "destroyed runtime namespace" 120 2 runtime_ns_absent
  # The control namespace must survive project teardown.
  "$KUBECTL_BIN" get namespace "$NAMESPACE" >/dev/null
fi

"$KUBECTL_BIN" -n "$NAMESPACE" delete project "$PROJECT" --ignore-not-found

if [[ "$SUPABASE_CORE_SMOKE" == "true" ]]; then
  run_supabase_core_smoke
fi

echo "Kubernetes Kind smoke passed for Helm chart and operator Project reconciliation"
