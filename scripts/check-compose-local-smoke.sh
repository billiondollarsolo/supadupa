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

api_cookie() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local output="$4"
  local args=(-sS -b "$cookie_jar" -o "$output" -w "%{http_code}" -X "$method" -H "Origin: $admin_origin")
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
      echo "timed out waiting for required local project services to run" >&2
      cat "$rows_file" >&2
      exit 1
    fi
    sleep 3
  done
}

assert_local_project_ready() {
  local project="$1"
  local context="$2"
  local compose_file="$install_dir/runtime/projects/$project/compose.yaml"
  local route_file="$install_dir/runtime/routes/$project.yaml"
  local body="$tmp/$project-connect.json"
  local expected_host="$project.$apps_domain"
  local expected_studio_host="studio-$project.$apps_domain"
  local expected_storage_host="storage-$project.$apps_domain"
  local status
  if [[ ! -f "$compose_file" ]]; then
    echo "expected $context compose file at $compose_file" >&2
    exit 1
  fi
  assert_compose_running "$compose_file" "$project"
  status="$(api_cookie GET "/v1/projects/$project/connect" "" "$body")"
  require_status "$status" "200" "$body" "$context connect payload"
  if [[ "$(json_get "$body" secret_handles.db_password)" != "secret://projects/$project/db_password" ]]; then
    echo "$context connect payload did not include project-scoped secret handles" >&2
    cat "$body" >&2
    exit 1
  fi
  if [[ "$(json_get "$body" api_url)" != "https://$expected_host" ]]; then
    echo "$context connect payload API URL did not match the seeded apps domain" >&2
    cat "$body" >&2
    exit 1
  fi
  if [[ "$(json_get "$body" studio_url)" != "https://$expected_studio_host" ]]; then
    echo "$context connect payload Studio URL did not match the seeded apps domain" >&2
    cat "$body" >&2
    exit 1
  fi
  if [[ "$(json_get "$body" storage_s3_url)" != "https://$expected_storage_host/storage/v1/s3" ]]; then
    echo "$context connect payload S3 URL did not match the seeded apps domain" >&2
    cat "$body" >&2
    exit 1
  fi
  if [[ ! -f "$route_file" ]]; then
    echo "expected $context route manifest at $route_file" >&2
    exit 1
  fi
  if ! grep -Fq 'Host(`'"$expected_host"'`)' "$route_file"; then
    echo "$context route manifest did not contain the exact project host rule" >&2
    cat "$route_file" >&2
    exit 1
  fi
  if ! grep -Fq "$project-kong:8000" "$route_file"; then
    echo "$context route manifest did not point at the project Kong service" >&2
    cat "$route_file" >&2
    exit 1
  fi
}

create_local_project() {
  local project="$1"
  local name="$2"
  local body="$tmp/$project-create.json"
  local status
  status="$(api_cookie POST "/v1/orgs/$org_id/projects" "{\"ref\":\"$project\",\"name\":\"$name\"}" "$body")"
  require_status "$status" "201" "$body" "create $project through browser cookie"
  if [[ "$(json_get "$body" status)" != "healthy" ]]; then
    echo "created local project $project did not report healthy status" >&2
    cat "$body" >&2
    exit 1
  fi
  assert_local_project_ready "$project" "$project"
}

delete_local_project() {
  local project="$1"
  local body="$tmp/$project-delete.json"
  local status
  status="$(api_cookie DELETE "/v1/projects/$project" "" "$body")"
  require_status "$status" "204" "$body" "delete $project"
  if [[ -d "$install_dir/runtime/projects/$project" ]]; then
    echo "project directory still exists after deleting $project" >&2
    exit 1
  fi
  if [[ -f "$install_dir/runtime/routes/$project.yaml" ]]; then
    echo "route manifest still exists after deleting $project" >&2
    cat "$install_dir/runtime/routes/$project.yaml" >&2
    exit 1
  fi
  if [[ -n "$(docker ps -a -q --filter "label=com.docker.compose.project=$project")" ]]; then
    echo "project containers remain after deleting $project" >&2
    docker ps -a --filter "label=com.docker.compose.project=$project" >&2
    exit 1
  fi
  if [[ -n "$(docker network ls -q --filter "label=com.docker.compose.project=$project")" ]]; then
    echo "project networks remain after deleting $project" >&2
    docker network ls --filter "label=com.docker.compose.project=$project" >&2
    exit 1
  fi
}

require_command docker
require_command curl
require_command python3

docker compose version >/dev/null
if [[ ! -S /var/run/docker.sock ]]; then
  echo "/var/run/docker.sock is required for local Compose smoke" >&2
  exit 1
fi

suffix="$(python3 - <<'PY'
import secrets
print(secrets.token_hex(3))
PY
)"
platform_project="supadupa-local-$suffix"
ref="local$suffix"
second_ref="local${suffix}b"
tmp="$(mktemp -d)"
install_dir="$tmp/install"
mkdir -p "$install_dir"

api_port="$(free_port)"
admin_port="$(free_port)"
meta_port="$(free_port)"
api_url="http://127.0.0.1:$api_port"
admin_origin="http://127.0.0.1:$admin_port"
cookie_jar="$tmp/cookies.txt"
ingress_created="false"
token=""

cleanup() {
  set +e
  if [[ -n "$token" ]]; then
    for cleanup_ref in "$ref" "$second_ref"; do
      curl -fsS -X DELETE -H "Authorization: Bearer $token" "$api_url/v1/projects/$cleanup_ref" >/dev/null 2>&1 || true
    done
  fi
  for cleanup_ref in "$ref" "$second_ref"; do
    if [[ -f "$install_dir/runtime/projects/$cleanup_ref/compose.yaml" ]]; then
      docker compose -p "$cleanup_ref" -f "$install_dir/runtime/projects/$cleanup_ref/compose.yaml" down -v --remove-orphans >/dev/null 2>&1 || true
    fi
  done
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
    --bootstrap-email "owner-$suffix@example.test" \
    --bootstrap-password "local-smoke-password-$suffix-000000000000" >/dev/null
)

if [[ "$(stat -c '%a' "$install_dir/.env")" != "600" ]]; then
  echo "setup-compose wrote .env with unexpected permissions" >&2
  exit 1
fi
apps_domain="$(awk -F= '$1 == "SUPADUPA_APPS_DOMAIN" { print $2; exit }' "$install_dir/.env")"
if [[ -z "$apps_domain" ]]; then
  echo "setup-compose did not write SUPADUPA_APPS_DOMAIN" >&2
  exit 1
fi

export SUPADUPA_API_ADDR="127.0.0.1:$api_port"
export SUPADUPA_ADMIN_ADDR="127.0.0.1:$admin_port"
export SUPADUPA_META_DB_ADDR="127.0.0.1:$meta_port"
export VITE_API_BASE_URL="$api_url"
export SUPADUPA_CORS_ORIGINS="$admin_origin,http://localhost:$admin_port"
export SUPADUPA_DEFAULT_PROFILE="essential"
export SUPADUPA_DEFAULT_RESOURCE_TIER="custom"
export SUPADUPA_DEFAULT_STACK_VERSION="15.8.1.060"

platform_compose up -d --build

wait_http_ok "$api_url/healthz"

body="$tmp/auth-state.json"
status="$(curl -sS -o "$body" -w "%{http_code}" "$api_url/v1/auth/state")"
require_status "$status" "200" "$body" "auth state"
if [[ "$(json_get "$body" bootstrapped)" != "true" ]]; then
  echo "expected setup bootstrap admin to mark auth state bootstrapped" >&2
  cat "$body" >&2
  exit 1
fi

login_body="$tmp/browser-login.json"
login_headers="$tmp/browser-login.headers"
status="$(curl -sS -c "$cookie_jar" -D "$login_headers" -o "$login_body" -w "%{http_code}" \
  -X POST \
  -H "Content-Type: application/json" \
  -H "X-Supadupa-Browser: true" \
  --data-binary "{\"email\":\"owner-$suffix@example.test\",\"password\":\"local-smoke-password-$suffix-000000000000\"}" \
  "$api_url/v1/auth/login")"
require_status "$status" "200" "$login_body" "browser login"
if grep -q '"token"' "$login_body"; then
  echo "browser login response exposed a bearer token" >&2
  cat "$login_body" >&2
  exit 1
fi
if ! grep -qi 'set-cookie: supadupa_session=' "$login_headers"; then
  echo "browser login did not set supadupa_session cookie" >&2
  cat "$login_headers" >&2
  exit 1
fi

body="$tmp/cookie-state.json"
status="$(curl -sS -b "$cookie_jar" -o "$body" -w "%{http_code}" "$api_url/v1/auth/state")"
require_status "$status" "200" "$body" "cookie auth state"
if [[ "$(json_get "$body" user.email)" != "owner-$suffix@example.test" ]]; then
  echo "cookie auth state did not return the logged-in user" >&2
  cat "$body" >&2
  exit 1
fi

token_body="$tmp/token-login.json"
status="$(curl -sS -o "$token_body" -w "%{http_code}" \
  -X POST \
  -H "Content-Type: application/json" \
  --data-binary "{\"email\":\"owner-$suffix@example.test\",\"password\":\"local-smoke-password-$suffix-000000000000\"}" \
  "$api_url/v1/auth/login")"
require_status "$status" "200" "$token_body" "token login"
token="$(json_get "$token_body" token)"
if [[ -z "$token" ]]; then
  echo "non-browser login did not return token for cleanup fallback" >&2
  cat "$token_body" >&2
  exit 1
fi

body="$tmp/org.json"
status="$(api_cookie POST /v1/orgs '{"name":"Local Smoke"}' "$body")"
require_status "$status" "201" "$body" "create org through browser cookie"
org_id="$(json_get "$body" id)"

create_local_project "$ref" "Local Smoke Project One"
create_local_project "$second_ref" "Local Smoke Project Two"

body="$tmp/backup-target.json"
target_body="{\"name\":\"Local Smoke Target\",\"type\":\"s3\",\"endpoint\":\"http://127.0.0.1:1\",\"region\":\"auto\",\"bucket\":\"local-smoke-backups\",\"prefix\":\"smoke/$ref\",\"access_key_id\":\"local-access\",\"secret_access_key\":\"local-secret\",\"force_path_style\":true,\"default\":true}"
status="$(api_cookie POST /v1/backup-storage-targets "$target_body" "$body")"
require_status "$status" "201" "$body" "create backup storage target"
target_id="$(json_get "$body" id)"
if [[ -z "$target_id" || "$(json_get "$body" secret_configured)" != "true" ]]; then
  echo "backup storage target response did not include expected target metadata" >&2
  cat "$body" >&2
  exit 1
fi

body="$tmp/backup-policy.json"
policy_body="{\"enabled\":true,\"schedule\":\"hourly\",\"kind\":\"logical\",\"storage_target_id\":\"$target_id\"}"
status="$(api_cookie PUT "/v1/projects/$ref/backups/policy" "$policy_body" "$body")"
require_status "$status" "200" "$body" "bind backup policy to target"
if [[ "$(json_get "$body" storage_target_id)" != "$target_id" ]]; then
  echo "backup policy did not bind the created target" >&2
  cat "$body" >&2
  exit 1
fi

platform_compose restart supadupavisor >/dev/null
sleep 2
wait_http_ok "$api_url/healthz"

body="$tmp/backup-policy-after-restart.json"
status="$(api_cookie GET "/v1/projects/$ref/backups/policy" "" "$body")"
require_status "$status" "200" "$body" "backup policy after control-plane restart"
if [[ "$(json_get "$body" storage_target_id)" != "$target_id" ]]; then
  echo "backup policy target binding did not survive control-plane restart" >&2
  cat "$body" >&2
  exit 1
fi

body="$tmp/backup-targets-after-restart.json"
status="$(api_cookie GET /v1/backup-storage-targets "" "$body")"
require_status "$status" "200" "$body" "backup targets after control-plane restart"
if ! python3 - "$body" "$target_id" <<'PY'
import json
import sys

targets = json.load(open(sys.argv[1], encoding="utf-8"))
target_id = sys.argv[2]
for target in targets:
    if target.get("id") == target_id and target.get("bucket") == "local-smoke-backups" and target.get("secret_configured") is True:
        raise SystemExit(0)
raise SystemExit(1)
PY
then
  echo "created backup target did not survive control-plane restart" >&2
  cat "$body" >&2
  exit 1
fi

assert_local_project_ready "$ref" "$ref after control-plane restart"
assert_local_project_ready "$second_ref" "$second_ref after control-plane restart"

delete_local_project "$ref"
assert_local_project_ready "$second_ref" "$second_ref after deleting $ref"
delete_local_project "$second_ref"
token=""

echo "local Docker Compose setup smoke passed"
