#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

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
elif isinstance(value, bool):
    print("true" if value else "false")
else:
    print(value)
PY
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

api() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local output="$4"
  local args=(-sS -o "$output" -w "%{http_code}" -X "$method" -H "Authorization: Bearer $token")
  if [[ -n "$body" ]]; then
    args+=(-H "Content-Type: application/json" --data-binary "$body")
  fi
  curl "${args[@]}" "$api_url$path"
}

platform_compose() {
  docker compose -p "$platform_project" \
    --env-file "$install_dir/.env" \
    -f "$ROOT/deploy/compose.yaml" \
    -f "$ROOT/deploy/compose.apply.yaml" \
    "$@"
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
    if "running" not in str(seen[service].get("State", "")).lower():
        raise SystemExit(1)
PY
    then
      return
    fi
    if (( SECONDS >= deadline )); then
      echo "timed out waiting for required admin UI smoke project services to run" >&2
      cat "$rows_file" >&2
      exit 1
    fi
    sleep 3
  done
}

require_command docker
require_command curl
require_command npm
require_command python3

docker compose version >/dev/null
if [[ ! -S /var/run/docker.sock ]]; then
  echo "/var/run/docker.sock is required for admin UI Compose smoke" >&2
  exit 1
fi

suffix="$(python3 - <<'PY'
import secrets
print(secrets.token_hex(3))
PY
)"
platform_project="supadupa-adminui-$suffix"
ref="ui$suffix"
tmp="$(mktemp -d)"
install_dir="$tmp/install"
mkdir -p "$install_dir"

api_port="$(free_port)"
admin_port="$(free_port)"
meta_port="$(free_port)"
api_url="http://127.0.0.1:$api_port"
admin_url="http://127.0.0.1:$admin_port"
email="admin-ui-$suffix@example.test"
password="admin-ui-password-$suffix-000000000000"
token=""
ingress_created="false"

cleanup() {
  set +e
  if [[ -n "$token" ]]; then
    curl -fsS -X DELETE -H "Authorization: Bearer $token" "$api_url/v1/projects/$ref" >/dev/null 2>&1 || true
  fi
  if [[ -f "$install_dir/runtime/projects/$ref/compose.yaml" ]]; then
    docker compose -p "$ref" -f "$install_dir/runtime/projects/$ref/compose.yaml" down -v --remove-orphans >/dev/null 2>&1 || true
  fi
  docker compose -p "$platform_project" \
    --env-file "$install_dir/.env" \
    -f "$ROOT/deploy/compose.yaml" \
    -f "$ROOT/deploy/compose.apply.yaml" \
    down -v --remove-orphans >/dev/null 2>&1 || true
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

(
  cd "$install_dir"
  "$ROOT/scripts/setup-compose.sh" \
    --mode local \
    --force \
    --bootstrap-email "$email" \
    --bootstrap-password "$password" >/dev/null
)

apps_domain="$(awk -F= '$1 == "SUPADUPA_APPS_DOMAIN" { print $2; exit }' "$install_dir/.env")"
if [[ -z "$apps_domain" ]]; then
  echo "setup-compose did not write SUPADUPA_APPS_DOMAIN" >&2
  exit 1
fi

export SUPADUPA_API_ADDR="127.0.0.1:$api_port"
export SUPADUPA_ADMIN_ADDR="127.0.0.1:$admin_port"
export SUPADUPA_META_DB_ADDR="127.0.0.1:$meta_port"
export VITE_API_BASE_URL="$api_url"
export SUPADUPA_CORS_ORIGINS="$admin_url,http://localhost:$admin_port"
export SUPADUPA_DEFAULT_PROFILE="essential"
export SUPADUPA_DEFAULT_RESOURCE_TIER="custom"
export SUPADUPA_DEFAULT_STACK_VERSION="15.8.1.060"

platform_compose up -d --build
wait_http_ok "$api_url/healthz"
wait_http_ok "$admin_url/"

login_body="$tmp/token-login.json"
status="$(curl -sS -o "$login_body" -w "%{http_code}" \
  -X POST \
  -H "Content-Type: application/json" \
  --data-binary "{\"email\":\"$email\",\"password\":\"$password\"}" \
  "$api_url/v1/auth/login")"
require_status "$status" "200" "$login_body" "token login"
token="$(json_get "$login_body" token)"
if [[ -z "$token" ]]; then
  echo "login response did not include token" >&2
  cat "$login_body" >&2
  exit 1
fi

body="$tmp/org.json"
status="$(api POST /v1/orgs '{"name":"Admin UI Smoke"}' "$body")"
require_status "$status" "201" "$body" "create org"
org_id="$(json_get "$body" id)"

body="$tmp/project.json"
status="$(api POST "/v1/orgs/$org_id/projects" "{\"ref\":\"$ref\",\"name\":\"Admin UI Smoke Project\"}" "$body")"
require_status "$status" "201" "$body" "create admin UI smoke project"
if [[ "$(json_get "$body" status)" != "healthy" ]]; then
  echo "admin UI smoke project did not report healthy status" >&2
  cat "$body" >&2
  exit 1
fi

compose_file="$install_dir/runtime/projects/$ref/compose.yaml"
assert_compose_running "$compose_file" "$ref"

connect_body="$tmp/connect.json"
status="$(api GET "/v1/projects/$ref/connect" "" "$connect_body")"
require_status "$status" "200" "$connect_body" "project connect payload"
project_api_url="https://$ref.$apps_domain"
project_studio_url="https://studio-$ref.$apps_domain"
project_storage_s3_url="https://storage-$ref.$apps_domain/storage/v1/s3"
for pair in \
  "api_url:$project_api_url" \
  "studio_url:$project_studio_url" \
  "storage_s3_url:$project_storage_s3_url"; do
  key="${pair%%:*}"
  expected="${pair#*:}"
  if [[ "$(json_get "$connect_body" "$key")" != "$expected" ]]; then
    echo "connect payload $key did not match expected UI smoke URL $expected" >&2
    cat "$connect_body" >&2
    exit 1
  fi
done

(
  cd "$ROOT/frontend"
  SUPADUPA_LIVE_ADMIN_URL="$admin_url" \
  SUPADUPA_LIVE_EMAIL="$email" \
  SUPADUPA_LIVE_PASSWORD="$password" \
  SUPADUPA_LIVE_PROJECT_REF="$ref" \
  SUPADUPA_LIVE_PROJECT_API_URL="$project_api_url" \
  SUPADUPA_LIVE_PROJECT_STUDIO_URL="$project_studio_url" \
  SUPADUPA_LIVE_PROJECT_STORAGE_S3_URL="$project_storage_s3_url" \
    npm exec -- playwright test --config playwright.live.config.ts tests/e2e/live-compose-admin.spec.ts
)

body="$tmp/delete.json"
status="$(api DELETE "/v1/projects/$ref" "" "$body")"
require_status "$status" "204" "$body" "delete admin UI smoke project"
if [[ -d "$install_dir/runtime/projects/$ref" ]]; then
  echo "admin UI smoke project directory remains after delete" >&2
  exit 1
fi
if [[ -n "$(docker ps -a -q --filter "label=com.docker.compose.project=$ref")" ]]; then
  echo "admin UI smoke project containers remain after delete" >&2
  docker ps -a --filter "label=com.docker.compose.project=$ref" >&2
  exit 1
fi
if [[ -n "$(docker network ls -q --filter "label=com.docker.compose.project=$ref")" ]]; then
  echo "admin UI smoke project networks remain after delete" >&2
  docker network ls --filter "label=com.docker.compose.project=$ref" >&2
  exit 1
fi
token=""

echo "Docker Compose admin UI smoke passed"
