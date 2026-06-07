#!/usr/bin/env bash

if [[ -n "${SUPADUPA_COMPAT_LIB_LOADED:-}" ]]; then
  return 0
fi
SUPADUPA_COMPAT_LIB_LOADED=1

COMPAT_SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${SUPADUPA_REPO_ROOT:-$(cd -- "$COMPAT_SCRIPT_DIR/../.." && pwd)}"
ARTIFACT_ROOT="${SUPADUPA_COMPAT_ARTIFACT_ROOT:-/tmp/supadupa-compat}"
ARTIFACT_DIR=""
RESULTS_FILE=""

compat_init() {
  if [[ -z "${SUPADUPA_TEST_REF:-}" ]]; then
    echo "SUPADUPA_TEST_REF is required" >&2
    exit 2
  fi
  if [[ ! "$SUPADUPA_TEST_REF" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
    echo "SUPADUPA_TEST_REF must be a lower-case project ref" >&2
    exit 2
  fi

  umask 077
  ARTIFACT_DIR="$ARTIFACT_ROOT/$SUPADUPA_TEST_REF"
  RESULTS_FILE="$ARTIFACT_DIR/results.jsonl"
  mkdir -p "$ARTIFACT_DIR"
  touch "$RESULTS_FILE"
}

json_string() {
  local value="${1:-}"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\n'/\\n}"
  value="${value//$'\r'/\\r}"
  value="${value//$'\t'/\\t}"
  printf '"%s"' "$value"
}

record_result() {
  local status="$1"
  local test_name="$2"
  local message="${3:-}"
  local ts
  ts="$(date -u +%FT%TZ)"

  printf '{"ts":%s,"status":%s,"test":%s,"message":%s}\n' \
    "$(json_string "$ts")" \
    "$(json_string "$status")" \
    "$(json_string "$test_name")" \
    "$(json_string "$message")" >>"$RESULTS_FILE"

  if [[ -n "$message" ]]; then
    printf '%s %s - %s\n' "$status" "$test_name" "$message"
  else
    printf '%s %s\n' "$status" "$test_name"
  fi
}

pass() {
  record_result "PASS" "$1" "${2:-}"
}

skip() {
  record_result "SKIP" "$1" "${2:-}"
}

fail() {
  record_result "FAIL" "$1" "${2:-}"
  exit 1
}

require_env() {
  local name
  for name in "$@"; do
    if [[ -z "${!name:-}" ]]; then
      fail "env.$name" "$name is required"
    fi
  done
}

require_tool() {
  local name="$1"
  if ! command -v "$name" >/dev/null 2>&1; then
    fail "tool.$name" "$name is required"
  fi
}

run_logged() {
  local test_name="$1"
  local log_name="$2"
  shift 2

  if (cd "$REPO_ROOT" && "$@") >"$ARTIFACT_DIR/$log_name.out" 2>"$ARTIFACT_DIR/$log_name.err"; then
    pass "$test_name" "logs: $log_name.out, $log_name.err"
  else
    fail "$test_name" "command failed; see $ARTIFACT_DIR/$log_name.err"
  fi
}

json_get_file() {
  local file="$1"
  local path="$2"

  if command -v jq >/dev/null 2>&1; then
    jq -er --arg path "$path" 'getpath($path | split(".")) // empty' "$file"
    return
  fi

  require_tool node
  node -e '
const fs = require("fs");
const file = process.argv[1];
const path = process.argv[2].split(".");
let value = JSON.parse(fs.readFileSync(file, "utf8"));
for (const key of path) {
  if (value == null || !(key in value)) process.exit(2);
  value = value[key];
}
if (value == null) process.exit(2);
if (typeof value === "object") {
  process.stdout.write(JSON.stringify(value));
} else {
  process.stdout.write(String(value));
}
' "$file" "$path"
}

json_get_file_optional() {
  json_get_file "$1" "$2" 2>/dev/null || true
}

supadupa_cli() {
  require_env SUPADUPA_API_URL

  if [[ -n "${SUPADUPA_CLI_BIN:-}" ]]; then
    "$SUPADUPA_CLI_BIN" --api "$SUPADUPA_API_URL" "$@"
    return
  fi

  (cd "$REPO_ROOT" && go run ./cmd/supadupa-cli --api "$SUPADUPA_API_URL" "$@")
}

write_secret_file() {
  local file="$1"
  local value="$2"
  umask 077
  printf '%s' "$value" >"$file"
  chmod 600 "$file"
}

read_secret_file() {
  local file="$1"
  if [[ ! -s "$file" ]]; then
    return 1
  fi
  cat "$file"
}

ensure_token() {
  local token_file="$ARTIFACT_DIR/token"
  local login_tmp="$ARTIFACT_DIR/login.tmp.json"
  local login_err="$ARTIFACT_DIR/login.stderr"
  local token

  if [[ -s "$token_file" ]]; then
    return 0
  fi

  require_env SUPADUPA_API_URL SUPADUPA_TEST_EMAIL SUPADUPA_TEST_PASSWORD

  if ! supadupa_cli login \
    --email "$SUPADUPA_TEST_EMAIL" \
    --password "$SUPADUPA_TEST_PASSWORD" >"$login_tmp" 2>"$login_err"; then
    rm -f "$login_tmp"
    fail "auth.login" "login failed; see $login_err"
  fi

  token="$(json_get_file "$login_tmp" token 2>/dev/null || true)"
  rm -f "$login_tmp"
  if [[ -z "$token" ]]; then
    fail "auth.login" "login response did not include a token"
  fi

  write_secret_file "$token_file" "$token"
  pass "auth.login" "token stored in restricted artifact"
}

supadupa_cli_authed() {
  local token
  ensure_token
  token="$(read_secret_file "$ARTIFACT_DIR/token")"
  supadupa_cli --token "$token" "$@"
}

ensure_profile() {
  local profile="$ARTIFACT_DIR/profile.json"
  local profile_err="$ARTIFACT_DIR/profile.stderr"

  if [[ -s "$profile" ]]; then
    return 0
  fi

  ensure_token
  if ! supadupa_cli_authed projects cli-profile \
    --ref "$SUPADUPA_TEST_REF" \
    --format json >"$profile" 2>"$profile_err"; then
    fail "project.cli_profile" "failed to fetch CLI profile; see $profile_err"
  fi
}

reveal_secret_value() {
  local kind="$1"
  local file="$ARTIFACT_DIR/$kind.value"
  local tmp="$ARTIFACT_DIR/$kind.tmp.json"
  local err="$ARTIFACT_DIR/$kind.stderr"
  local value

  if [[ ! -s "$file" ]]; then
    if ! supadupa_cli_authed secrets reveal \
      --ref "$SUPADUPA_TEST_REF" \
      --kind "$kind" >"$tmp" 2>"$err"; then
      rm -f "$tmp"
      fail "secret.$kind" "secret reveal failed; see $err"
    fi

    value="$(json_get_file "$tmp" value 2>/dev/null || true)"
    rm -f "$tmp"
    if [[ -z "$value" ]]; then
      fail "secret.$kind" "secret reveal response was empty"
    fi
    write_secret_file "$file" "$value"
  fi

  read_secret_file "$file"
}

profile_value() {
  ensure_profile
  json_get_file "$ARTIFACT_DIR/profile.json" "$1"
}

profile_value_optional() {
  ensure_profile
  json_get_file_optional "$ARTIFACT_DIR/profile.json" "$1"
}

compat_bool() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|on|ON)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

compat_fetch_runtime_config() {
  require_env SUPADUPA_API_URL
  require_tool curl
  ensure_token

  local out="${1:-$ARTIFACT_DIR/runtime-config.json}"
  local err="${2:-$ARTIFACT_DIR/runtime-config.stderr}"
  local token
  local status

  token="$(read_secret_file "$ARTIFACT_DIR/token")"
  status="$(curl -sS -o "$out" -w '%{http_code}' \
    -H "Authorization: Bearer $token" \
    "${SUPADUPA_API_URL%/}/v1/runtime-config" 2>"$err")"
  if [[ "$status" != 2* ]]; then
    fail "runtime_config.get" "expected 2xx, got HTTP $status; see $(basename "$err")"
  fi
}

compat_require_runtime_config_bool() {
  local test_name="$1"
  local path="$2"
  local expected="${3:-true}"
  local out="$ARTIFACT_DIR/runtime-config.json"
  local err="$ARTIFACT_DIR/runtime-config.stderr"
  local got

  compat_fetch_runtime_config "$out" "$err"
  got="$(json_get_file_optional "$out" "$path")"
  if [[ "$got" != "$expected" ]]; then
    fail "$test_name" "expected runtime config $path=$expected, got ${got:-empty}; see $(basename "$out")"
  fi
  pass "$test_name" "$path=$got"
}

remove_cached_project_material() {
  rm -f \
    "$ARTIFACT_DIR/created-project" \
    "$ARTIFACT_DIR/project.json" \
    "$ARTIFACT_DIR/profile.json" \
    "$ARTIFACT_DIR/anon_key.value" \
    "$ARTIFACT_DIR/service_role.value" \
    "$ARTIFACT_DIR/db_password.value"
}

url_without_password() {
  local input="$1"
  input="${input//'${DB_PASSWORD}'/}"
  input="${input//'\$DB_PASSWORD'/}"

  require_tool node
  node -e '
const input = process.argv[1];
const url = new URL(input);
url.password = "";
process.stdout.write(url.toString());
' "$input"
}

url_with_password() {
  local input="$1"
  local password="$2"

  require_tool node
  node -e '
const input = process.argv[1];
const password = process.argv[2];
const url = new URL(input);
const username = url.username || "postgres";
url.username = username;
url.password = password;
process.stdout.write(url.toString());
' "$input" "$password"
}

url_host() {
  require_tool node
  node -e '
const input = process.argv[1];
const url = new URL(input);
process.stdout.write(url.hostname);
' "$1"
}

is_public_host() {
  local host="$1"
  case "$host" in
    ""|localhost|127.*|::1|host.docker.internal|*.internal)
      return 1
      ;;
  esac
  return 0
}
