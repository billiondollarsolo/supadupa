#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF SUPADUPA_TEST_EMAIL SUPADUPA_TEST_PASSWORD
require_tool curl
require_tool node
ensure_profile

api_base="${SUPADUPA_API_URL%/}"
studio_url="$(profile_value studio_url)"
cookie_jar="$ARTIFACT_DIR/studio-auth-cookies.txt"
login_body="$ARTIFACT_DIR/studio-auth-login.body"
login_headers="$ARTIFACT_DIR/studio-auth-login.headers"
login_err="$ARTIFACT_DIR/studio-auth-login.stderr"

set +e
login_status="$(curl -sS -c "$cookie_jar" -D "$login_headers" -o "$login_body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -X POST "$api_base/v1/auth/login" \
  --data "{\"email\":\"$SUPADUPA_TEST_EMAIL\",\"password\":\"$SUPADUPA_TEST_PASSWORD\"}" \
  2>"$login_err")"
login_rc="$?"
set -e
if [[ "$login_rc" -ne 0 ]]; then
  login_status="000"
fi
case "$login_status" in
  2??) ;;
  *) fail "studio_auth.login_cookie" "expected 2xx, got HTTP $login_status" ;;
esac
if ! grep -q 'supadupa_session' "$cookie_jar"; then
  fail "studio_auth.login_cookie" "login did not set supadupa_session cookie"
fi
pass "studio_auth.login_cookie" "HTTP $login_status"

noauth_body="$ARTIFACT_DIR/studio-auth-noauth.body"
noauth_err="$ARTIFACT_DIR/studio-auth-noauth.stderr"
set +e
noauth_status="$(curl -sS -o "$noauth_body" -w '%{http_code}' "$studio_url" 2>"$noauth_err")"
noauth_rc="$?"
set -e
if [[ "$noauth_rc" -ne 0 ]]; then
  noauth_status="000"
fi
case "$noauth_status" in
  401|403) pass "studio_auth.noauth_rejected" "HTTP $noauth_status" ;;
  *) fail "studio_auth.noauth_rejected" "expected 401 or 403, got HTTP $noauth_status" ;;
esac

control_token="$(read_secret_file "$ARTIFACT_DIR/token")"
studio_session_body="$ARTIFACT_DIR/studio-auth-session.body"
studio_session_err="$ARTIFACT_DIR/studio-auth-session.stderr"
studio_session_status="$(curl -sS -o "$studio_session_body" -w '%{http_code}' \
  -H "Authorization: Bearer $control_token" \
  "$api_base/v1/projects/$SUPADUPA_TEST_REF/studio-session" \
  2>"$studio_session_err")"
case "$studio_session_status" in
  2??) ;;
  *) fail "studio_auth.session_token" "expected 2xx, got HTTP $studio_session_status" ;;
esac
studio_token="$(json_get_file "$studio_session_body" token)"
studio_separator="?"
case "$studio_url" in
  *\?*) studio_separator="&" ;;
esac
studio_load_url="$studio_url${studio_separator}supadupa_studio_token=$studio_token"

studio_body="$ARTIFACT_DIR/studio-auth.body"
studio_headers="$ARTIFACT_DIR/studio-auth.headers"
studio_err="$ARTIFACT_DIR/studio-auth.stderr"
set +e
studio_status="$(curl -sSL -D "$studio_headers" -o "$studio_body" -w '%{http_code}' "$studio_load_url" 2>"$studio_err")"
studio_rc="$?"
set -e
if [[ "$studio_rc" -ne 0 ]]; then
  studio_status="000"
fi
case "$studio_status" in
  2??) ;;
  *) fail "studio_auth.authenticated_load" "expected 2xx, got HTTP $studio_status" ;;
esac
if ! grep -qi '<title[^>]*>Supabase</title>\|__NEXT_DATA__\|/_next/' "$studio_body"; then
  fail "studio_auth.authenticated_load" "authenticated response did not look like Studio HTML"
fi
pass "studio_auth.authenticated_load" "HTTP $studio_status"

if grep -Eqi 'localhost|127\.0\.0\.1|host\.docker\.internal|\.internal(:|/|")' "$studio_body"; then
  fail "studio_auth.no_localhost_links" "authenticated Studio HTML contains local/internal host references"
fi
pass "studio_auth.no_localhost_links" "Studio HTML has no obvious local/internal host references"

connect_json="$ARTIFACT_DIR/studio-auth-connect.json"
connect_err="$ARTIFACT_DIR/studio-auth-connect.stderr"
if ! supadupa_cli_authed projects connect --ref "$SUPADUPA_TEST_REF" >"$connect_json" 2>"$connect_err"; then
  fail "studio_auth.connect_payload" "failed to fetch connect payload; see studio-auth-connect.stderr"
fi

rest_docs_url="$(json_get_file "$connect_json" links.rest_docs)"
graphql_explorer_url="$(json_get_file "$connect_json" links.graphql_explorer)"
if [[ "$rest_docs_url" != *"/project/$SUPADUPA_TEST_REF/api"* ]]; then
  fail "studio_auth.rest_docs_project_ref" "REST docs link is not project-ref scoped: $rest_docs_url"
fi
if [[ "$graphql_explorer_url" != *"/project/$SUPADUPA_TEST_REF/api?panel=graphql"* ]]; then
  fail "studio_auth.graphql_project_ref" "GraphQL explorer link is not project-ref scoped: $graphql_explorer_url"
fi
pass "studio_auth.project_ref_links" "Studio docs links use project ref"

for link_name in rest_docs graphql_explorer; do
  link_url="$(json_get_file "$connect_json" "links.$link_name")"
  separator="?"
  case "$link_url" in
    *\?*) separator="&" ;;
  esac
  link_body="$ARTIFACT_DIR/studio-auth-$link_name.body"
  link_err="$ARTIFACT_DIR/studio-auth-$link_name.stderr"
  link_status="$(curl -sSL -o "$link_body" -w '%{http_code}' \
    "$link_url${separator}supadupa_studio_token=$studio_token" \
    2>"$link_err")"
  case "$link_status" in
    2??) ;;
    *) fail "studio_auth.$link_name" "expected 2xx, got HTTP $link_status" ;;
  esac
  if ! grep -qi '<title[^>]*>Supabase</title>\|__NEXT_DATA__\|/_next/' "$link_body"; then
    fail "studio_auth.$link_name" "response did not look like Studio HTML"
  fi
  pass "studio_auth.$link_name" "project-ref link loaded"
done
