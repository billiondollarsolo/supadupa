#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF

for tool in go npm curl node psql docker; do
  require_tool "$tool"
  pass "tool.$tool" "$(command -v "$tool")"
done

if docker compose version >"$ARTIFACT_DIR/docker-compose-version.out" 2>"$ARTIFACT_DIR/docker-compose-version.err"; then
  pass "tool.docker_compose" "docker compose is available"
else
  fail "tool.docker_compose" "docker compose version failed"
fi

run_logged "preflight.go_test" "go-test" go test ./...
run_logged "preflight.frontend_build" "frontend-build" npm --prefix frontend run build

if curl -fsS "$SUPADUPA_API_URL/v1/health" >"$ARTIFACT_DIR/api-health.json" 2>"$ARTIFACT_DIR/api-health.stderr"; then
  pass "preflight.api_health" "management API health response saved"
else
  fail "preflight.api_health" "management API health failed; see api-health.stderr"
fi
