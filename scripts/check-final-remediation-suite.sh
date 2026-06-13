#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

SKIP_SUMMARY=()

run() {
  echo "==> $*"
  "$@"
}

skip_gate() {
  local label="$1"
  local flag="$2"
  local reason_var="${flag}_REASON"
  local reason="${!reason_var:-}"
  if [[ -z "${reason//[[:space:]]/}" ]]; then
    echo "$flag=1 requires a non-empty $reason_var so skipped final gates have recorded evidence" >&2
    exit 1
  fi
  echo "==> skipping $label because $flag=1: $reason"
  SKIP_SUMMARY+=("$label: $reason")
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required for the final remediation suite" >&2
    exit 1
  fi
}

prepend_bin_dir() {
  local bin="$1"
  if [[ -n "$bin" ]]; then
    PATH="$(dirname "$bin"):$PATH"
    export PATH
  fi
}

run go test ./...
if [[ "${SUPADUPA_FINAL_SKIP_GOVULNCHECK:-}" == "1" ]]; then
  skip_gate "govulncheck" "SUPADUPA_FINAL_SKIP_GOVULNCHECK"
else
  run go run golang.org/x/vuln/cmd/govulncheck@latest ./...
fi

run npm --prefix frontend run build
run npm --prefix frontend run check
run npm --prefix frontend audit
if command -v chromium-browser >/dev/null 2>&1; then
  run env PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH="$(command -v chromium-browser)" npm --prefix frontend run browser-smoke
elif command -v chromium >/dev/null 2>&1; then
  run env PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH="$(command -v chromium)" npm --prefix frontend run browser-smoke
elif [[ "${SUPADUPA_FINAL_SKIP_BROWSER_SMOKE:-}" == "1" ]]; then
  skip_gate "browser smoke" "SUPADUPA_FINAL_SKIP_BROWSER_SMOKE"
else
  echo "chromium-browser or chromium is required for browser smoke; set SUPADUPA_FINAL_SKIP_BROWSER_SMOKE=1 with SUPADUPA_FINAL_SKIP_BROWSER_SMOKE_REASON only when recording an accepted local limitation" >&2
  exit 1
fi

run npm --prefix scripts/compat ci
run npm --prefix scripts/compat audit

run scripts/check-dockerignore.sh
run scripts/check-compose-hardening.py
run scripts/check-setup-compose.sh
run scripts/check-release-note-policy.sh
run python3 scripts/check-docs-remediation.py
run python3 scripts/check-security-regressions.py
run python3 scripts/check-kubernetes-crds.py
run jq empty charts/supadupa/values.schema.json

prepend_bin_dir "${HELM_BIN:-}"
require_command helm
run scripts/check-helm-chart.sh

run go test -race ./internal/scheduler
run go test ./cmd/supadupa-docker-proxy
run go test ./internal/operator ./cmd/supadupa-operator ./internal/provisioner/kubernetes

run bash -n \
  scripts/check-compose-apply-lifecycle-smoke.sh \
  scripts/check-compose-admin-ui-smoke.sh \
  scripts/check-compose-edge-routing-smoke.sh \
  scripts/check-compose-local-smoke.sh \
  scripts/check-dockerignore.sh \
  scripts/check-final-remediation-suite.sh \
  scripts/check-helm-chart.sh \
  scripts/check-kubernetes-kind-smoke.sh \
  scripts/check-release-note-policy.sh \
  scripts/check-setup-compose.sh \
  scripts/check-terraform-provider-smoke.sh \
  scripts/setup-compose.sh \
  scripts/setup-local-dns.sh \
  scripts/compat/*.sh
run python3 -m py_compile \
  scripts/check-compose-hardening.py \
  scripts/check-docs-remediation.py \
  scripts/check-security-regressions.py \
  scripts/check-kubernetes-crds.py

if [[ "${SUPADUPA_FINAL_SKIP_DOCKER_IMAGE_CHECKS:-}" == "1" ]]; then
  skip_gate "Docker image checks" "SUPADUPA_FINAL_SKIP_DOCKER_IMAGE_CHECKS"
else
  require_command docker
  run docker build -f deploy/Dockerfile.control-plane -t supadupa-control-plane:ci .
  run docker build -f deploy/Dockerfile.admin -t supadupa-admin:ci .
  run docker build -f deploy/Dockerfile.operator -t supadupa-operator:ci .
  run sh -c "docker run --rm --entrypoint id supadupa-control-plane:ci | grep -q 'uid=10001'"
  run sh -c "docker run --rm --entrypoint id supadupa-operator:ci | grep -q 'uid=10001'"
fi

prepend_bin_dir "${TERRAFORM_BIN:-}"
if [[ "${SUPADUPA_FINAL_SKIP_TERRAFORM_SMOKE:-}" == "1" ]]; then
  skip_gate "Terraform smoke" "SUPADUPA_FINAL_SKIP_TERRAFORM_SMOKE"
else
  require_command terraform
  run scripts/check-terraform-provider-smoke.sh
fi

if [[ "${SUPADUPA_FINAL_SKIP_COMPOSE_LIFECYCLE_SMOKE:-}" == "1" ]]; then
  skip_gate "Compose apply lifecycle smoke" "SUPADUPA_FINAL_SKIP_COMPOSE_LIFECYCLE_SMOKE"
else
  require_command docker
  require_command terraform
  run scripts/check-compose-apply-lifecycle-smoke.sh
fi

if [[ "${SUPADUPA_FINAL_SKIP_COMPOSE_LOCAL_SMOKE:-}" == "1" ]]; then
  skip_gate "local Compose setup smoke" "SUPADUPA_FINAL_SKIP_COMPOSE_LOCAL_SMOKE"
else
  require_command docker
  run scripts/check-compose-local-smoke.sh
fi

if [[ "${SUPADUPA_FINAL_SKIP_COMPOSE_EDGE_ROUTING_SMOKE:-}" == "1" ]]; then
  skip_gate "Compose edge routing smoke" "SUPADUPA_FINAL_SKIP_COMPOSE_EDGE_ROUTING_SMOKE"
else
  require_command docker
  run scripts/check-compose-edge-routing-smoke.sh
fi

if [[ "${SUPADUPA_FINAL_SKIP_COMPOSE_ADMIN_UI_SMOKE:-}" == "1" ]]; then
  skip_gate "Compose admin UI smoke" "SUPADUPA_FINAL_SKIP_COMPOSE_ADMIN_UI_SMOKE"
else
  require_command docker
  require_command npm
  run scripts/check-compose-admin-ui-smoke.sh
fi

prepend_bin_dir "${KIND_BIN:-}"
prepend_bin_dir "${KUBECTL_BIN:-}"
if [[ "${SUPADUPA_FINAL_SKIP_KIND_SMOKE:-}" == "1" ]]; then
  skip_gate "Kubernetes Kind smoke" "SUPADUPA_FINAL_SKIP_KIND_SMOKE"
else
  require_command docker
  require_command kind
  require_command kubectl
  require_command helm
  run scripts/check-kubernetes-kind-smoke.sh
fi

run git diff --check

if ((${#SKIP_SUMMARY[@]} > 0)); then
  echo "==> final remediation suite skip summary"
  printf ' - %s\n' "${SKIP_SUMMARY[@]}"
fi

echo "final remediation validation suite passed"
