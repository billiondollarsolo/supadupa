#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool curl
require_tool jq
ensure_token

api_base="${SUPADUPA_API_URL%/}"
token="$(read_secret_file "$ARTIFACT_DIR/token")"
telemetry_timeout_seconds="${SUPADUPA_COMPAT_TELEMETRY_TIMEOUT_SECONDS:-90}"
telemetry_poll_seconds="${SUPADUPA_COMPAT_TELEMETRY_POLL_SECONDS:-5}"

require_positive_int() {
  local name="$1"
  local value="$2"
  if [[ ! "$value" =~ ^[0-9]+$ ]] || [[ "$value" -le 0 ]]; then
    fail "metrics.env.$name" "$name must be a positive integer"
  fi
}

fetch_json() {
  local test_name="$1"
  local url="$2"
  local body="$3"
  local err="$4"
  local status
  local rc

  set +e
  status="$(curl -sS -o "$body" -w '%{http_code}' \
    -H "Authorization: Bearer $token" \
    "$url" 2>"$err")"
  rc="$?"
  set -e
  if [[ "$rc" -ne 0 ]]; then
    fail "$test_name" "request failed; see $(basename "$err")"
  fi
  case "$status" in
    2??) ;;
    *) fail "$test_name" "expected HTTP 2xx, got HTTP $status; see $(basename "$body")" ;;
  esac
}

require_positive_int SUPADUPA_COMPAT_TELEMETRY_TIMEOUT_SECONDS "$telemetry_timeout_seconds"
require_positive_int SUPADUPA_COMPAT_TELEMETRY_POLL_SECONDS "$telemetry_poll_seconds"

project_metrics="$ARTIFACT_DIR/project-metrics.json"
project_metrics_err="$ARTIFACT_DIR/project-metrics.stderr"
fetch_json "metrics.project.http" "$api_base/v1/projects/$SUPADUPA_TEST_REF/metrics" "$project_metrics" "$project_metrics_err"

if jq -e --arg ref "$SUPADUPA_TEST_REF" '.project_ref == $ref' "$project_metrics" >/dev/null; then
  pass "metrics.project.ref" "$SUPADUPA_TEST_REF"
else
  fail "metrics.project.ref" "project_ref mismatch"
fi

project_status="$(jq -r '.status // empty' "$project_metrics")"
case "$project_status" in
  healthy) pass "metrics.project.status" "$project_status" ;;
  *) fail "metrics.project.status" "expected healthy, got ${project_status:-empty}" ;;
esac

if jq -e '.resources.cpu > 0 and .resources.ram_mb > 0 and .resources.disk_gb > 0 and .resources.projects > 0' "$project_metrics" >/dev/null; then
  pass "metrics.project.resources" "$(jq -r '.resources | "\(.cpu) vCPU, \(.ram_mb) MB RAM, \(.disk_gb) GB disk"' "$project_metrics")"
else
  fail "metrics.project.resources" "project reservation metrics are missing or zero"
fi

if compat_bool "${SUPADUPA_COMPAT_REQUIRE_TELEMETRY:-true}"; then
  deadline=$((SECONDS + telemetry_timeout_seconds))
  while true; do
    if jq -e '.observed.cpu_percent >= 0 and .observed.memory_bytes >= 0 and .observed.memory_limit_bytes > 0 and (.observed.sampled_at | length > 0)' "$project_metrics" >/dev/null 2>&1; then
      pass "metrics.project.telemetry" "$(jq -r '.observed | "cpu=\(.cpu_percent)% memory=\(.memory_bytes)/\(.memory_limit_bytes)"' "$project_metrics")"
      break
    fi
    if [[ "$SECONDS" -ge "$deadline" ]]; then
      fail "metrics.project.telemetry" "no fresh project telemetry before ${telemetry_timeout_seconds}s timeout"
    fi
    sleep "$telemetry_poll_seconds"
    fetch_json "metrics.project.http" "$api_base/v1/projects/$SUPADUPA_TEST_REF/metrics" "$project_metrics" "$project_metrics_err"
  done
else
  skip "metrics.project.telemetry" "SUPADUPA_COMPAT_REQUIRE_TELEMETRY=false"
fi

fleet_metrics="$ARTIFACT_DIR/fleet-metrics.json"
fleet_metrics_err="$ARTIFACT_DIR/fleet-metrics.stderr"
fetch_json "metrics.fleet.http" "$api_base/v1/metrics" "$fleet_metrics" "$fleet_metrics_err"

if jq -e '.projects >= 1 and (.projects_by_status.healthy // 0) >= 1' "$fleet_metrics" >/dev/null; then
  pass "metrics.fleet.projects" "$(jq -r '"projects=\(.projects) healthy=\(.projects_by_status.healthy // 0)"' "$fleet_metrics")"
else
  fail "metrics.fleet.projects" "fleet project counts are missing selected healthy project"
fi

if jq -e '.hosts >= 1 and .host_capacity.cpu > 0 and .host_capacity.ram_mb > 0 and .host_capacity.disk_gb > 0' "$fleet_metrics" >/dev/null; then
  pass "metrics.fleet.capacity" "$(jq -r '.host_capacity | "\(.cpu) vCPU, \(.ram_mb) MB RAM, \(.disk_gb) GB disk"' "$fleet_metrics")"
else
  fail "metrics.fleet.capacity" "fleet host capacity is missing or zero"
fi

if jq -e '.host_used.cpu >= 1 and .host_used.ram_mb >= 1 and .host_used.disk_gb >= 1 and .host_used.projects >= 1' "$fleet_metrics" >/dev/null; then
  pass "metrics.fleet.reservations" "$(jq -r '.host_used | "\(.cpu) vCPU, \(.ram_mb) MB RAM, \(.disk_gb) GB disk, \(.projects) projects"' "$fleet_metrics")"
else
  fail "metrics.fleet.reservations" "fleet reserved resources are missing selected project"
fi

if compat_bool "${SUPADUPA_COMPAT_REQUIRE_TELEMETRY:-true}"; then
  if jq -e '.observed.projects_sampled >= 1 and .observed.cpu_percent >= 0 and .observed.memory_bytes >= 0 and .observed.memory_limit_bytes > 0' "$fleet_metrics" >/dev/null; then
    pass "metrics.fleet.telemetry" "$(jq -r '.observed | "sampled=\(.projects_sampled) stale=\(.stale_projects) cpu=\(.cpu_percent)% memory=\(.memory_bytes)/\(.memory_limit_bytes)"' "$fleet_metrics")"
  else
    fail "metrics.fleet.telemetry" "fleet observed telemetry is missing"
  fi
else
  skip "metrics.fleet.telemetry" "SUPADUPA_COMPAT_REQUIRE_TELEMETRY=false"
fi

if jq -e '.audit_verified == true' "$fleet_metrics" >/dev/null; then
  pass "metrics.fleet.audit" "audit_verified=true"
else
  fail "metrics.fleet.audit" "audit chain is not verified"
fi

prometheus_body="$ARTIFACT_DIR/prometheus-metrics.txt"
prometheus_err="$ARTIFACT_DIR/prometheus-metrics.stderr"
fetch_json "metrics.prometheus.http" "$api_base/metrics" "$prometheus_body" "$prometheus_err"

for metric_name in \
  supadupa_projects_total \
  supadupa_host_capacity_cpu \
  supadupa_host_used_cpu \
  supadupa_observed_projects \
  supadupa_audit_verified
do
  if grep -q "^$metric_name " "$prometheus_body"; then
    pass "metrics.prometheus.$metric_name" "present"
  else
    fail "metrics.prometheus.$metric_name" "missing from Prometheus output"
  fi
done
