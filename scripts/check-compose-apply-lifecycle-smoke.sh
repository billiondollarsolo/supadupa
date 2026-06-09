#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
TERRAFORM_BIN="${TERRAFORM_BIN:-terraform}"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required" >&2
    exit 1
  fi
}

free_port() {
  python3 - <<'PY'
import socket
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
}

json_get() {
  python3 - "$1" "$2" <<'PY'
import json
import sys

path = sys.argv[2].split(".")
with open(sys.argv[1], encoding="utf-8") as handle:
    value = json.load(handle)
for part in path:
    if isinstance(value, list):
        value = value[int(part)]
    else:
        value = value[part]
if isinstance(value, (dict, list)):
    print(json.dumps(value))
else:
    print(value)
PY
}

api() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local output="$4"
  local status
  local args=(-sS -o "$output" -w "%{http_code}" -X "$method" -H "Authorization: Bearer $token")
  if [[ -n "$body" ]]; then
    args+=(-H "Content-Type: application/json" --data-binary "$body")
  fi
  status="$(curl "${args[@]}" "$api_url$path")"
  printf '%s' "$status"
}

require_status() {
  local actual="$1"
  local expected="$2"
  local body_file="$3"
  local context="$4"
  if [[ "$actual" != "$expected" ]]; then
    echo "$context failed: expected HTTP $expected, got $actual" >&2
    cat "$body_file" >&2 || true
    exit 1
  fi
}

wait_http_ok() {
  local url="$1"
  local deadline=$((SECONDS + 180))
  until curl -fsS "$url" >/dev/null 2>&1; do
    if (( SECONDS >= deadline )); then
      echo "timed out waiting for $url" >&2
      exit 1
    fi
    sleep 2
  done
}

compose_rows_json() {
  local compose_file="$1"
  local project="$2"
  docker compose -p "$project" -f "$compose_file" ps -a --format json | python3 -c 'import json,sys
raw=sys.stdin.read().strip()
if not raw:
    print("[]")
elif raw.startswith("["):
    print(raw)
else:
    print(json.dumps([json.loads(line) for line in raw.splitlines() if line.strip()]))'
}

assert_compose_running() {
  local compose_file="$1"
  local project="$2"
  local rows_file="$tmp/${project}-ps.json"
  local deadline=$((SECONDS + 240))
  while true; do
    compose_rows_json "$compose_file" "$project" >"$rows_file"
    if python3 - "$rows_file" <<'PY'
import json
import sys
rows = json.load(open(sys.argv[1], encoding="utf-8"))
required = {"db", "kong", "meta"}
seen = {row.get("Service"): row for row in rows}
if not required.issubset(seen):
    raise SystemExit(1)
for service in required:
    row = seen[service]
    state = str(row.get("State", "")).lower()
    if "running" not in state:
        raise SystemExit(1)
PY
    then
      return
    fi
    if (( SECONDS >= deadline )); then
      echo "timed out waiting for required project services to run" >&2
      cat "$rows_file" >&2
      exit 1
    fi
    sleep 3
  done
}

assert_compose_stopped() {
  local compose_file="$1"
  local project="$2"
  local rows_file="$tmp/${project}-stopped-ps.json"
  compose_rows_json "$compose_file" "$project" >"$rows_file"
  python3 - "$rows_file" <<'PY'
import json
import sys
rows = json.load(open(sys.argv[1], encoding="utf-8"))
if not rows:
    raise SystemExit("expected stopped project containers, got none")
running = [row for row in rows if "running" in str(row.get("State", "")).lower()]
if running:
    raise SystemExit(f"expected no running containers after pause, got {running!r}")
PY
}

assert_file_contains() {
  local path="$1"
  local needle="$2"
  if ! grep -Fq -- "$needle" "$path"; then
    echo "expected $path to contain $needle" >&2
    sed -n '1,220p' "$path" >&2 || true
    exit 1
  fi
}

assert_compose_service_absent() {
  local project="$1"
  local service="$2"
  local matches
  matches="$(docker ps -a -q --filter "label=com.docker.compose.project=$project" --filter "label=com.docker.compose.service=$service")"
  if [[ -n "$matches" ]]; then
    echo "expected no $service containers after cleanup" >&2
    docker ps -a --filter "label=com.docker.compose.project=$project" --filter "label=com.docker.compose.service=$service" >&2
    exit 1
  fi
}

assert_compose_service_running() {
  local project="$1"
  local service="$2"
  local matches
  matches="$(docker ps -q --filter "label=com.docker.compose.project=$project" --filter "label=com.docker.compose.service=$service" --filter "status=running")"
  if [[ -z "$matches" ]]; then
    echo "expected $service container to be running" >&2
    docker ps -a --filter "label=com.docker.compose.project=$project" --filter "label=com.docker.compose.service=$service" >&2
    exit 1
  fi
}

assert_project_status() {
  local ref="$1"
  local expected="$2"
  local context="$3"
  local body="$tmp/${ref}-${context//[^a-zA-Z0-9]/-}.json"
  local status
  status="$(api GET "/v1/projects/$ref" "" "$body")"
  require_status "$status" "200" "$body" "$context"
  if [[ "$(json_get "$body" status)" != "$expected" ]]; then
    echo "$context did not report $expected project status" >&2
    cat "$body" >&2
    exit 1
  fi
}

require_command docker
require_command curl
require_command python3
require_command "$TERRAFORM_BIN"

docker compose version >/dev/null
if [[ ! -S /var/run/docker.sock ]]; then
  echo "/var/run/docker.sock is required for apply-mode smoke" >&2
  exit 1
fi

suffix="$(python3 - <<'PY'
import secrets
print(secrets.token_hex(3))
PY
)"
platform_project="supadupa-life-$suffix"
ref="life$suffix"
tmp="$(mktemp -d)"
runtime="$tmp/runtime"
plugin_dir="$tmp/terraform-plugins"
terraform_dir="$tmp/terraform"
mkdir -p "$runtime/projects" "$runtime/routes" "$runtime/certs" "$runtime/backups"
mkdir -p "$plugin_dir" "$terraform_dir"
chown -R 10001:10001 "$runtime"

api_port="$(free_port)"
admin_port="$(free_port)"
meta_port="$(free_port)"
api_url="http://127.0.0.1:$api_port"
token=""
ingress_created="false"

cleanup() {
  set +e
  if [[ -n "$token" ]]; then
    curl -fsS -X DELETE -H "Authorization: Bearer $token" "$api_url/v1/projects/$ref" >/dev/null 2>&1 || true
  fi
  if [[ -f "$runtime/projects/$ref/compose.yaml" ]]; then
    docker compose -p "$ref" -f "$runtime/projects/$ref/compose.yaml" down -v --remove-orphans >/dev/null 2>&1 || true
  fi
  docker compose -p "$platform_project" -f "$ROOT/deploy/compose.yaml" -f "$ROOT/deploy/compose.apply.yaml" down -v --remove-orphans >/dev/null 2>&1 || true
  if [[ "$ingress_created" == "true" ]]; then
    docker network rm supadupa-ingress >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

if ! docker network inspect supadupa-ingress >/dev/null 2>&1; then
  docker network create supadupa-ingress >/dev/null
  ingress_created="true"
fi

export SUPADUPA_SECRET_KEY="smoke-secret-key-$suffix-000000000000000000000000"
export SUPADUPA_AUTH_SECRET="smoke-auth-secret-$suffix-000000000000000000000000"
export SUPADUPA_ALLOW_DEV_SECRETS=""
export SUPADUPA_CONTROL_PLANE_USER="10001:10001"
export SUPADUPA_DOCKER_GID
SUPADUPA_DOCKER_GID="$(stat -c '%g' /var/run/docker.sock)"
export SUPADUPA_API_ADDR="127.0.0.1:$api_port"
export SUPADUPA_ADMIN_ADDR="127.0.0.1:$admin_port"
export SUPADUPA_META_DB_ADDR="127.0.0.1:$meta_port"
export SUPADUPA_RUNTIME_HOST_DIR="$runtime"
export SUPADUPA_RUNTIME_CONTAINER_DIR="$runtime"
export SUPADUPA_ROUTES_HOST_DIR="$runtime/routes"
export SUPADUPA_CERTS_HOST_DIR="$runtime/certs"
export SUPADUPA_PROJECT_DOMAIN="apps.example.test"
export SUPADUPA_APPS_DOMAIN="apps.example.test"
export SUPADUPA_DEFAULT_PROFILE="essential"
export SUPADUPA_DEFAULT_RESOURCE_TIER="small"
export SUPADUPA_DEFAULT_STACK_VERSION="15.8.1.060"
export SUPADUPA_REPLICA_READY_TIMEOUT_SECONDS="${SUPADUPA_REPLICA_READY_TIMEOUT_SECONDS:-240}"

docker compose -p "$platform_project" -f "$ROOT/deploy/compose.yaml" -f "$ROOT/deploy/compose.apply.yaml" up -d --build
wait_http_ok "$api_url/healthz"

body="$tmp/bootstrap.json"
bootstrap_status="$(curl -sS -o "$body" -w "%{http_code}" -X POST -H "Content-Type: application/json" "$api_url/v1/auth/bootstrap" --data-binary "{\"email\":\"owner-$suffix@example.test\",\"password\":\"smoke-password-$suffix-000000000000\"}")"
require_status "$bootstrap_status" "201" "$body" "bootstrap admin"
token="$(json_get "$body" token)"
if [[ -z "$token" ]]; then
  echo "bootstrap response did not include API token" >&2
  cat "$body" >&2
  exit 1
fi

body="$tmp/org.json"
status="$(api POST /v1/orgs '{"name":"Smoke"}' "$body")"
require_status "$status" "201" "$body" "create org"
org_id="$(json_get "$body" id)"

body="$tmp/org-features.json"
status="$(api PUT "/v1/orgs/$org_id/features" '{"overrides":{"read_replicas":true}}' "$body")"
require_status "$status" "200" "$body" "enable read replica feature"
if [[ "$(json_get "$body" effective.read_replicas)" != "True" && "$(json_get "$body" effective.read_replicas)" != "true" ]]; then
  echo "org feature response did not enable read_replicas" >&2
  cat "$body" >&2
  exit 1
fi

go build -o "$plugin_dir/terraform-provider-supadupa" "$ROOT/cmd/terraform-provider-supadupa"
cat >"$tmp/terraformrc" <<EOF
provider_installation {
  dev_overrides {
    "supadupa/supadupa" = "$plugin_dir"
  }
  direct {}
}
EOF

cat >"$terraform_dir/main.tf" <<EOF
terraform {
  required_providers {
    supadupa = {
      source = "supadupa/supadupa"
    }
  }
}

provider "supadupa" {
  api_url = "$api_url"
  token   = "$token"
}

resource "supadupa_project" "lifecycle" {
  org_id        = "$org_id"
  ref           = "$ref"
  name          = "Lifecycle Smoke"
  domain        = "apps.example.test"
  stack_version = "15.8.1.060"
  profile       = "essential"
  resource_tier = "small"
}
EOF

export TF_CLI_CONFIG_FILE="$tmp/terraformrc"
export CHECKPOINT_DISABLE=1
export TF_IN_AUTOMATION=1

"$TERRAFORM_BIN" -chdir="$terraform_dir" apply -auto-approve -input=false

compose_file="$runtime/projects/$ref/compose.yaml"
if [[ ! -f "$compose_file" ]]; then
  echo "expected project compose file at $compose_file" >&2
  exit 1
fi
assert_compose_running "$compose_file" "$ref"

set +e
"$TERRAFORM_BIN" -chdir="$terraform_dir" plan -detailed-exitcode -input=false
plan_status="$?"
set -e
if [[ "$plan_status" -ne 0 ]]; then
  echo "terraform no-op plan returned $plan_status; expected 0" >&2
  exit 1
fi

body="$tmp/pause.json"
status="$(api POST "/v1/projects/$ref/pause" "" "$body")"
require_status "$status" "200" "$body" "pause project"
if [[ "$(json_get "$body" status)" != "paused" ]]; then
  echo "pause response did not report paused status" >&2
  cat "$body" >&2
  exit 1
fi
assert_compose_stopped "$compose_file" "$ref"

body="$tmp/resume.json"
status="$(api POST "/v1/projects/$ref/resume" "" "$body")"
require_status "$status" "200" "$body" "resume project"
assert_compose_running "$compose_file" "$ref"

body="$tmp/restart.json"
status="$(api POST "/v1/projects/$ref/restart" "" "$body")"
require_status "$status" "200" "$body" "restart project"
assert_compose_running "$compose_file" "$ref"

body="$tmp/scale.json"
status="$(api POST "/v1/projects/$ref/scale" '{"resource_tier":"medium"}' "$body")"
require_status "$status" "200" "$body" "scale project"
if [[ "$(json_get "$body" spec.resource_tier)" != "medium" ]]; then
  echo "scale response did not report medium resource tier" >&2
  cat "$body" >&2
  exit 1
fi
if [[ ! -f "$runtime/projects/$ref/scale.yaml" ]]; then
  echo "expected scale manifest to be written" >&2
  exit 1
fi
assert_compose_running "$compose_file" "$ref"

body="$tmp/upgrade.json"
status="$(api POST "/v1/projects/$ref/upgrade" '{"version":"15.8.1.085"}' "$body")"
require_status "$status" "200" "$body" "upgrade project"
if [[ "$(json_get "$body" previous_version)" != "15.8.1.060" || "$(json_get "$body" target_version)" != "15.8.1.085" || "$(json_get "$body" project.spec.stack_version)" != "15.8.1.085" ]]; then
  echo "upgrade response did not report expected versions" >&2
  cat "$body" >&2
  exit 1
fi
if [[ -z "$(json_get "$body" backup.id)" ]]; then
  echo "upgrade response did not include a pre-upgrade backup id" >&2
  cat "$body" >&2
  exit 1
fi
assert_file_contains "$compose_file" "supabase/postgres:15.8.1.085"
assert_file_contains "$runtime/projects/$ref/.env" "STACK_VERSION=15.8.1.085"
assert_compose_running "$compose_file" "$ref"

body="$tmp/replica.json"
status="$(api POST "/v1/projects/$ref/replicas" '{"name":"east","region":"us-east","tier":"small","read_weight":70,"failover_priority":1}' "$body")"
require_status "$status" "201" "$body" "create read replica"
replica_id="$(json_get "$body" id)"
if [[ "$(json_get "$body" status)" != "healthy" ]]; then
  echo "replica response did not report healthy status" >&2
  cat "$body" >&2
  exit 1
fi
replica_compose_file="$runtime/projects/$ref/replicas/compose.yaml"
replica_env_file="$runtime/projects/$ref/replicas/$replica_id.env"
if [[ ! -f "$replica_compose_file" || ! -f "$replica_env_file" ]]; then
  echo "expected replica compose and env files to be written" >&2
  find "$runtime/projects/$ref/replicas" -maxdepth 1 -type f -print >&2 || true
  exit 1
fi
assert_file_contains "$replica_compose_file" "db-replica-east:"
assert_file_contains "$replica_compose_file" 'image: supabase/postgres:${STACK_VERSION}'
assert_file_contains "$replica_env_file" "REPLICA_REGION=us-east"
assert_file_contains "$replica_env_file" "REPLICA_READ_WEIGHT=70"
assert_file_contains "$runtime/projects/$ref/pg_hba.conf" "host replication supabase_replication_admin 0.0.0.0/0 scram-sha-256"
assert_compose_service_running "$ref" "db-replica-east"
sleep 35
assert_project_status "$ref" "healthy" "replica reconciled project status"

body="$tmp/replica-delete.json"
status="$(api DELETE "/v1/projects/$ref/replicas/$replica_id" "" "$body")"
require_status "$status" "204" "$body" "delete read replica"
if [[ -f "$replica_compose_file" || -f "$replica_env_file" ]]; then
  echo "replica files remain after delete" >&2
  find "$runtime/projects/$ref/replicas" -maxdepth 1 -type f -print >&2 || true
  exit 1
fi
assert_compose_service_absent "$ref" "db-replica-east"
assert_compose_running "$compose_file" "$ref"

"$TERRAFORM_BIN" -chdir="$terraform_dir" destroy -auto-approve -input=false

if [[ -d "$runtime/projects/$ref" ]]; then
  echo "project directory still exists after destroy" >&2
  exit 1
fi
if [[ -n "$(docker ps -a -q --filter "label=com.docker.compose.project=$ref")" ]]; then
  echo "project containers remain after destroy" >&2
  docker ps -a --filter "label=com.docker.compose.project=$ref" >&2
  exit 1
fi
if [[ -n "$(docker volume ls -q --filter "label=com.docker.compose.project=$ref")" ]]; then
  echo "project volumes remain after destroy" >&2
  docker volume ls --filter "label=com.docker.compose.project=$ref" >&2
  exit 1
fi
