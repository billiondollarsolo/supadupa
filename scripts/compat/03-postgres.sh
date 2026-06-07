#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool psql
ensure_token
ensure_profile

public_db_url="$(profile_value_optional public_database_url)"
if [[ -z "$public_db_url" ]]; then
  fail "postgres.public_url" "profile did not include public_database_url"
fi

public_db_safe_url="$(url_without_password "$public_db_url")"
public_db_host="$(url_host "$public_db_safe_url")"
if ! is_public_host "$public_db_host"; then
  fail "postgres.public_url" "public database host is not public"
fi
pass "postgres.public_url" "$public_db_host"

db_password="$(reveal_secret_value db_password)"
pass "secret.db_password" "database password stored in restricted artifact"

if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
  -v ON_ERROR_STOP=1 \
  -Atqc "select current_database() || '|' || current_user || '|' || inet_server_port()" \
  >"$ARTIFACT_DIR/postgres.out" 2>"$ARTIFACT_DIR/postgres.stderr"; then
  postgres_probe="$(tr -d '\r' <"$ARTIFACT_DIR/postgres.out")"
  case "$postgres_probe" in
    postgres\|*\|5432)
      pass "postgres.public_connect" "$postgres_probe"
      ;;
    *)
      fail "postgres.public_connect" "unexpected probe output"
      ;;
  esac
else
  fail "postgres.public_connect" "psql failed; see postgres.stderr"
fi

check_pooler_url() {
  local name="$1"
  local profile_key="$2"
  local expected_port="$3"
  local raw_url
  local safe_url
  local host
  local port
  local out
  local err
  local probe
  local deadline
  local poll_seconds
  local timeout_seconds
  local rc

  raw_url="$(profile_value_optional "$profile_key")"
  if [[ -z "$raw_url" ]]; then
    fail "postgres.$name.url" "profile did not include $profile_key"
  fi
  safe_url="$(url_without_password "$raw_url")"
  host="$(url_host "$safe_url")"
  if ! is_public_host "$host"; then
    fail "postgres.$name.url" "$profile_key host is not public"
  fi
  port="$(node -e 'const url = new URL(process.argv[1]); process.stdout.write(url.port);' "$safe_url")"
  if [[ "$port" != "$expected_port" ]]; then
    fail "postgres.$name.url" "$profile_key expected port $expected_port, got ${port:-empty}"
  fi
  pass "postgres.$name.url" "$host:$port"

  out="$ARTIFACT_DIR/postgres-$name.out"
  err="$ARTIFACT_DIR/postgres-$name.stderr"
  timeout_seconds="${SUPADUPA_COMPAT_POOLER_TIMEOUT_SECONDS:-240}"
  poll_seconds="${SUPADUPA_COMPAT_POOLER_POLL_SECONDS:-3}"
  deadline=$((SECONDS + timeout_seconds))
  while true; do
    set +e
    PGPASSWORD="$db_password" psql "$safe_url" \
      -v ON_ERROR_STOP=1 \
      -Atqc "select current_database() || '|' || current_user || '|' || inet_server_port()" \
      >"$out" 2>"$err"
    rc="$?"
    set -e
    if [[ "$rc" -eq 0 ]]; then
      probe="$(tr -d '\r' <"$out")"
      case "$probe" in
        postgres\|*\|*)
          pass "postgres.$name.connect" "$probe"
          return
          ;;
        *)
          fail "postgres.$name.connect" "unexpected probe output"
          ;;
      esac
    fi
    if [[ "$SECONDS" -ge "$deadline" ]]; then
      fail "postgres.$name.connect" "psql failed before ${timeout_seconds}s timeout; see $(basename "$err")"
    fi
    sleep "$poll_seconds"
  done
}

check_pooler_url "pooler_transaction" "pooler_transaction_url" "6543"
check_pooler_url "pooler_session" "pooler_session_url" "5432"
