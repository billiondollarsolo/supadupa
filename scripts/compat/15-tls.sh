#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool curl
require_tool node
require_tool openssl
ensure_token
ensure_profile

api_base="${SUPADUPA_API_URL%/}"
admin_url="${SUPADUPA_ADMIN_URL:-}"
if [[ -z "$admin_url" ]]; then
  admin_url="$(node -e '
const input = process.argv[1];
const url = new URL(input);
if (url.hostname.startsWith("api.")) url.hostname = `admin.${url.hostname.slice(4)}`;
process.stdout.write(url.toString().replace(/\/$/, ""));
' "$api_base")"
fi

https_urls=(
  "control_api|$api_base/v1/health"
  "control_admin|${admin_url%/}/"
  "project_api|$(profile_value api_url)/auth/v1/health"
  "project_studio|$(profile_value studio_url)/"
)

url_host_port() {
  local input="$1"
  local default_port="$2"
  node -e '
const input = process.argv[1];
const defaultPort = process.argv[2];
const url = new URL(input);
process.stdout.write(`${url.hostname}\t${url.port || defaultPort}`);
' "$input" "$default_port"
}

http_url_for_https() {
  local input="$1"
  node -e '
const url = new URL(process.argv[1]);
url.protocol = "http:";
url.port = "";
process.stdout.write(url.toString());
' "$input"
}

check_https_curl() {
  local label="$1"
  local url="$2"
  local body="$ARTIFACT_DIR/tls-$label-curl.body"
  local err="$ARTIFACT_DIR/tls-$label-curl.stderr"
  local status
  local rc

  set +e
  status="$(curl -sS --connect-timeout 10 --max-time 20 -o "$body" -w '%{http_code}' "$url" 2>"$err")"
  rc="$?"
  set -e
  if [[ "$rc" -ne 0 ]]; then
    fail "tls.$label.curl" "HTTPS client validation failed; see $(basename "$err")"
  fi
  case "$status" in
    2??|3??|4??)
      pass "tls.$label.curl" "HTTP $status"
      ;;
    *)
      fail "tls.$label.curl" "unexpected HTTP $status"
      ;;
  esac
}

check_https_openssl() {
  local label="$1"
  local url="$2"
  local host
  local port
  local tuple
  local out="$ARTIFACT_DIR/tls-$label-openssl.out"
  local err="$ARTIFACT_DIR/tls-$label-openssl.stderr"

  tuple="$(url_host_port "$url" 443)"
  host="${tuple%%$'\t'*}"
  port="${tuple##*$'\t'}"
  if timeout 20 openssl s_client \
    -connect "$host:$port" \
    -servername "$host" \
    -verify_return_error \
    -brief \
    </dev/null >"$out" 2>"$err"; then
    pass "tls.$label.openssl" "$host:$port"
  else
    fail "tls.$label.openssl" "certificate verification failed for $host:$port; see $(basename "$err")"
  fi
}

check_http_redirect() {
  local label="$1"
  local https_url="$2"
  local http_url
  local out="$ARTIFACT_DIR/tls-$label-http-redirect.headers"
  local err="$ARTIFACT_DIR/tls-$label-http-redirect.stderr"
  local status
  local redirect
  local rc

  http_url="$(http_url_for_https "$https_url")"
  set +e
  read -r status redirect < <(curl -sS -I --connect-timeout 10 --max-time 20 -o "$out" -w '%{http_code} %{redirect_url}' "$http_url" 2>"$err")
  rc="$?"
  set -e
  if [[ -z "$status" || "$status" == "000" ]]; then
    fail "tls.$label.http_redirect" "HTTP redirect probe failed with curl exit $rc; see $(basename "$err")"
  fi
  case "$status" in
    301|302|307|308)
      if [[ "$redirect" == https://* ]]; then
        pass "tls.$label.http_redirect" "HTTP $status -> $redirect"
      else
        fail "tls.$label.http_redirect" "HTTP $status did not redirect to HTTPS: ${redirect:-empty}"
      fi
      ;;
    *)
      fail "tls.$label.http_redirect" "expected HTTP redirect, got $status"
      ;;
  esac
}

for item in "${https_urls[@]}"; do
  label="${item%%|*}"
  url="${item#*|}"
  if [[ "$url" != https://* ]]; then
    fail "tls.$label.url" "URL is not HTTPS: $url"
  fi
  check_https_curl "$label" "$url"
  check_https_openssl "$label" "$url"
  check_http_redirect "$label" "$url"
done

check_postgres_tls() {
  local label="$1"
  local profile_key="$2"
  local url
  local safe_url
  local tuple
  local host
  local port
  local sslmode
  local out="$ARTIFACT_DIR/tls-$label-postgres.out"
  local err="$ARTIFACT_DIR/tls-$label-postgres.stderr"
  local deadline
  local poll_seconds
  local timeout_seconds

  url="$(profile_value_optional "$profile_key")"
  if [[ -z "$url" ]]; then
    fail "tls.$label.url" "profile did not include $profile_key"
  fi
  safe_url="$(url_without_password "$url")"
  tuple="$(url_host_port "$safe_url" 5432)"
  host="${tuple%%$'\t'*}"
  port="${tuple##*$'\t'}"
  sslmode="$(node -e 'const url = new URL(process.argv[1]); process.stdout.write(url.searchParams.get("sslmode") || "");' "$safe_url")"
  if [[ "$sslmode" != "require" ]]; then
    fail "tls.$label.sslmode" "expected sslmode=require, got ${sslmode:-empty}"
  fi
  timeout_seconds="${SUPADUPA_COMPAT_POOLER_TIMEOUT_SECONDS:-240}"
  poll_seconds="${SUPADUPA_COMPAT_POOLER_POLL_SECONDS:-3}"
  deadline=$((SECONDS + timeout_seconds))
  while true; do
    if timeout 20 openssl s_client \
      -starttls postgres \
      -connect "$host:$port" \
      -servername "$host" \
      -verify_return_error \
      -brief \
      </dev/null >"$out" 2>"$err"; then
      pass "tls.$label.postgres_starttls" "$host:$port sslmode=$sslmode"
      return
    fi
    if [[ "$SECONDS" -ge "$deadline" ]]; then
      fail "tls.$label.postgres_starttls" "Postgres STARTTLS verification failed before ${timeout_seconds}s timeout for $host:$port; see $(basename "$err")"
    fi
    sleep "$poll_seconds"
  done
}

check_postgres_tls "postgres_direct" "public_database_url"
check_postgres_tls "pooler_transaction" "pooler_transaction_url"
check_postgres_tls "pooler_session" "pooler_session_url"
