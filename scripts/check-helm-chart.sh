#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
CHART="$ROOT/charts/supadupa"

if ! command -v helm >/dev/null 2>&1; then
  echo "helm is not installed; chart render checks cannot run" >&2
  exit 1
fi

helm lint "$CHART" \
  --set secrets.secretKey=test-secret-key-change-me-000000000000 \
  --set secrets.authSecret=test-auth-secret-change-me-000000000000 \
  --set metaDb.password=test-postgres-password-change-me

helm template supadupa "$CHART" --include-crds \
  --set secrets.secretKey=test-secret-key-change-me-000000000000 \
  --set secrets.authSecret=test-auth-secret-change-me-000000000000 \
  --set metaDb.password=test-postgres-password-change-me \
  --set operator.enabled=true \
  --set operator.interval=30s >/tmp/supadupa-helm-render.yaml

if grep -Eq 'image: "supadupa-(control-plane|admin|operator):latest"' /tmp/supadupa-helm-render.yaml; then
  echo "helm template rendered a Supadupa platform image with the drifting latest tag" >&2
  exit 1
fi

if ! grep -q 'name: SUPADUPA_OPERATOR_INTERVAL' /tmp/supadupa-helm-render.yaml || ! grep -q 'value: "30s"' /tmp/supadupa-helm-render.yaml; then
  echo "helm template did not render the configured operator interval" >&2
  exit 1
fi

first_meta_env="$(
  awk '
    /^kind: Deployment$/ { in_deployment = 1; in_control_plane = 0 }
    in_deployment && /^[[:space:]]*name: supadupa-control-plane$/ { in_control_plane = 1 }
    in_control_plane && /^[[:space:]]*- name: POSTGRES_PASSWORD$/ { print "POSTGRES_PASSWORD"; exit }
    in_control_plane && /^[[:space:]]*- name: SUPADUPA_META_DSN$/ { print "SUPADUPA_META_DSN"; exit }
  ' /tmp/supadupa-helm-render.yaml
)"
if [[ "$first_meta_env" != "POSTGRES_PASSWORD" ]]; then
  echo "helm template must define POSTGRES_PASSWORD before SUPADUPA_META_DSN so Kubernetes expands the DSN password" >&2
  exit 1
fi

helm template supadupa "$CHART" --include-crds \
  --namespace supadupa-system \
  --set secrets.existingSecret=supadupa-existing-secrets \
  --set operator.enabled=true \
  --set ingress.enabled=true >/dev/null

helm template supadupa "$CHART" --include-crds \
  --set metaDb.enabled=false \
  --set controlPlane.metaDsn=postgres://supadupa:password@postgres.example:5432/supadupa \
  --set secrets.secretKey=test-secret-key-change-me-000000000000 \
  --set secrets.authSecret=test-auth-secret-change-me-000000000000 \
  --set controlPlane.kubernetesNamespaceOverride=project-runtime >/dev/null

helm template supadupa "$CHART" --include-crds \
  --namespace supadupa-system \
  --set secrets.secretKey=test-secret-key-change-me-000000000000 \
  --set secrets.authSecret=test-auth-secret-change-me-000000000000 \
  --set metaDb.password=test-postgres-password-change-me \
  --set operator.enabled=true \
  --set projectIsolation.enabled=false \
  --set operator.runtimeNamespaceOverride=project-runtime >/tmp/supadupa-helm-runtime-namespace.yaml

if ! awk '
  /^[[:space:]]*- name: SUPADUPA_K8S_NAMESPACE$/ { in_k8s = 1; next }
  in_k8s && /^[[:space:]]*value: "project-runtime"$/ { found = 1; in_k8s = 0 }
  in_k8s && /^[[:space:]]*- name:/ { in_k8s = 0 }
  END { exit found ? 0 : 1 }
' /tmp/supadupa-helm-runtime-namespace.yaml; then
  echo "helm template did not align control-plane SUPADUPA_K8S_NAMESPACE with operator.runtimeNamespaceOverride" >&2
  exit 1
fi

if ! awk '
  /^[[:space:]]*- name: SUPADUPA_OPERATOR_NAMESPACE$/ { in_operator = 1; next }
  in_operator && /^[[:space:]]*value: "project-runtime"$/ { found = 1; in_operator = 0 }
  in_operator && /^[[:space:]]*- name:/ { in_operator = 0 }
  END { exit found ? 0 : 1 }
' /tmp/supadupa-helm-runtime-namespace.yaml; then
  echo "helm template did not render operator runtime namespace override" >&2
  exit 1
fi

if ! awk '
  /^kind: Role$/ { in_role = 1; next }
  in_role && /^[[:space:]]*namespace: "project-runtime"$/ { found = 1; in_role = 0 }
  in_role && /^---$/ { in_role = 0 }
  END { exit found ? 0 : 1 }
' /tmp/supadupa-helm-runtime-namespace.yaml; then
  echo "helm template did not bind runtime RBAC Role to operator.runtimeNamespaceOverride" >&2
  exit 1
fi

if helm lint "$CHART" \
  --set secrets.secretKey=test-secret-key-change-me-000000000000 \
  --set secrets.authSecret=test-auth-secret-change-me-000000000000 \
  --set metaDb.password=test-postgres-password-change-me \
  --set controlPlane.service.type=InvalidType >/dev/null 2>&1; then
  echo "helm values schema accepted an invalid controlPlane.service.type" >&2
  exit 1
fi

if helm lint "$CHART" \
  --set secrets.secretKey=test-secret-key-change-me-000000000000 \
  --set secrets.authSecret=test-auth-secret-change-me-000000000000 \
  --set metaDb.password=test-postgres-password-change-me \
  --set controlPlane.image.tag=latest >/dev/null 2>&1; then
  echo "helm values schema accepted a drifting latest image tag" >&2
  exit 1
fi

if helm lint "$CHART" \
  --set secrets.secretKey=test-secret-key-change-me-000000000000 \
  --set secrets.authSecret=test-auth-secret-change-me-000000000000 \
  --set metaDb.password=test-postgres-password-change-me \
  --set operator.resources.requests.cpu[0]=invalid >/dev/null 2>&1; then
  echo "helm values schema accepted an invalid operator.resources.requests.cpu shape" >&2
  exit 1
fi

if helm lint "$CHART" \
  --set secrets.secretKey=test-secret-key-change-me-000000000000 \
  --set secrets.authSecret=test-auth-secret-change-me-000000000000 \
  --set metaDb.password=test-postgres-password-change-me \
  --set operator.interval=soon >/dev/null 2>&1; then
  echo "helm values schema accepted an invalid operator.interval" >&2
  exit 1
fi

# --- Namespace-per-project isolation (projectIsolation.enabled=true, default) ---

helm template supadupa "$CHART" --include-crds \
  --namespace supadupa-system \
  --set secrets.secretKey=test-secret-key-change-me-000000000000 \
  --set secrets.authSecret=test-auth-secret-change-me-000000000000 \
  --set metaDb.password=test-postgres-password-change-me \
  --set operator.enabled=true >/tmp/supadupa-helm-isolation.yaml

if ! grep -q '^kind: ClusterRole$' /tmp/supadupa-helm-isolation.yaml; then
  echo "helm template did not render a ClusterRole when projectIsolation.enabled=true" >&2
  exit 1
fi

if ! grep -q '^kind: ClusterRoleBinding$' /tmp/supadupa-helm-isolation.yaml; then
  echo "helm template did not render a ClusterRoleBinding when projectIsolation.enabled=true" >&2
  exit 1
fi

for verb_resource in namespaces networkpolicies resourcequotas limitranges serviceaccounts; do
  if ! grep -q "$verb_resource" /tmp/supadupa-helm-isolation.yaml; then
    echo "helm template ClusterRole missing required resource: $verb_resource" >&2
    exit 1
  fi
done

if ! grep -q 'name: SUPADUPA_OPERATOR_ISOLATION' /tmp/supadupa-helm-isolation.yaml; then
  echo "helm template did not wire SUPADUPA_OPERATOR_ISOLATION into the operator" >&2
  exit 1
fi

if ! grep -q 'name: SUPADUPA_K8S_ISOLATION' /tmp/supadupa-helm-isolation.yaml; then
  echo "helm template did not wire SUPADUPA_K8S_ISOLATION into the control plane" >&2
  exit 1
fi

if ! grep -q 'path: /readyz' /tmp/supadupa-helm-isolation.yaml; then
  echo "helm template did not render the operator readiness probe" >&2
  exit 1
fi

# Legacy mode renders the namespaced Role, not a ClusterRole.
helm template supadupa "$CHART" --include-crds \
  --namespace supadupa-system \
  --set secrets.secretKey=test-secret-key-change-me-000000000000 \
  --set secrets.authSecret=test-auth-secret-change-me-000000000000 \
  --set metaDb.password=test-postgres-password-change-me \
  --set operator.enabled=true \
  --set projectIsolation.enabled=false >/tmp/supadupa-helm-legacy.yaml

if grep -q '^kind: ClusterRole$' /tmp/supadupa-helm-legacy.yaml; then
  echo "helm template rendered a ClusterRole when projectIsolation.enabled=false" >&2
  exit 1
fi

if ! grep -q '^kind: Role$' /tmp/supadupa-helm-legacy.yaml; then
  echo "helm template did not render the namespaced Role when projectIsolation.enabled=false" >&2
  exit 1
fi

# PodDisruptionBudget + metrics Service render when enabled.
helm template supadupa "$CHART" --include-crds \
  --namespace supadupa-system \
  --set secrets.secretKey=test-secret-key-change-me-000000000000 \
  --set secrets.authSecret=test-auth-secret-change-me-000000000000 \
  --set metaDb.password=test-postgres-password-change-me \
  --set operator.enabled=true \
  --set operator.metrics.enabled=true \
  --set operator.podDisruptionBudget.enabled=true >/tmp/supadupa-helm-extras.yaml

if ! grep -q '^kind: PodDisruptionBudget$' /tmp/supadupa-helm-extras.yaml; then
  echo "helm template did not render the operator PodDisruptionBudget when enabled" >&2
  exit 1
fi

if ! grep -q 'name: SUPADUPA_OPERATOR_METRICS_ADDR' /tmp/supadupa-helm-extras.yaml; then
  echo "helm template did not wire SUPADUPA_OPERATOR_METRICS_ADDR when metrics enabled" >&2
  exit 1
fi

# Schema rejects an invalid Pod Security Admission enforce level.
if helm lint "$CHART" \
  --set secrets.secretKey=test-secret-key-change-me-000000000000 \
  --set secrets.authSecret=test-auth-secret-change-me-000000000000 \
  --set metaDb.password=test-postgres-password-change-me \
  --set projectIsolation.podSecurity.enforce=bogus >/dev/null 2>&1; then
  echo "helm values schema accepted an invalid projectIsolation.podSecurity.enforce" >&2
  exit 1
fi
