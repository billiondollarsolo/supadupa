#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool curl
require_tool node
ensure_token
ensure_profile

api_url="$(profile_value api_url)"
anon_key="$(reveal_secret_value anon_key)"
service_role="$(reveal_secret_value service_role)"
admin_origin="${SUPADUPA_ADMIN_URL:-}"
if [[ -z "$admin_origin" ]]; then
  admin_origin="$(node -e '
const api = new URL(process.argv[1]);
const host = api.hostname.replace(/^api[.]/, "admin.");
process.stdout.write(`${api.protocol}//${host}${api.port ? `:${api.port}` : ""}`);
' "$SUPADUPA_API_URL")"
fi
pass "secret.service_role" "service role key stored in restricted artifact"

auth_user_ids=()
storage_cleanup_bucket=""
storage_cleanup_object=""

cleanup_auth_users() {
  local user_id
  for user_id in "${auth_user_ids[@]:-}"; do
    [[ -z "$user_id" ]] && continue
    curl -sS -o "$ARTIFACT_DIR/auth-user-delete-$user_id.body" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/auth/v1/admin/users/$user_id" \
      2>"$ARTIFACT_DIR/auth-user-delete-$user_id.stderr" || true
  done
}

cleanup_storage_objects() {
  if [[ -z "${storage_cleanup_bucket:-}" || -z "${api_url:-}" || -z "${service_role:-}" ]]; then
    return 0
  fi
  if [[ -n "${storage_cleanup_object:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/storage-object-delete.body" \
      -H "Content-Type: application/json" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/storage/v1/object/$storage_cleanup_bucket" \
      --data "{\"prefixes\":[\"$storage_cleanup_object\"]}" \
      2>"$ARTIFACT_DIR/storage-object-delete.stderr" || true
  fi
  curl -sS -o "$ARTIFACT_DIR/storage-bucket-delete.body" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X DELETE "$api_url/storage/v1/bucket/$storage_cleanup_bucket" \
    2>"$ARTIFACT_DIR/storage-bucket-delete.stderr" || true
}

cleanup_all() {
  cleanup_storage_objects
  cleanup_auth_users
}
trap cleanup_all EXIT

expect_cors_preflight() {
  local test_name="$1"
  local url="$2"
  local method="$3"
  local headers="$4"
  local out="$ARTIFACT_DIR/$test_name.headers"
  local err="$ARTIFACT_DIR/$test_name.stderr"
  local status
  local rc

  set +e
  status="$(curl -sS -D "$out" -o /dev/null -w '%{http_code}' \
    -X OPTIONS "$url" \
    -H "Origin: $admin_origin" \
    -H "Access-Control-Request-Method: $method" \
    -H "Access-Control-Request-Headers: $headers" \
    2>"$err")"
  rc="$?"
  set -e
  if [[ "$rc" -ne 0 ]]; then
    status="000"
  fi
  if [[ "$status" != 2* ]]; then
    fail "$test_name" "expected 2xx preflight, got HTTP $status"
  fi
  if ! grep -qi '^access-control-allow-origin:' "$out"; then
    fail "$test_name" "missing Access-Control-Allow-Origin"
  fi
  pass "$test_name" "HTTP $status"
}

expect_cors_preflight "cors.auth_token_preflight" "$api_url/auth/v1/token?grant_type=password" "POST" "apikey,authorization,content-type"
expect_cors_preflight "cors.rest_preflight" "$api_url/rest/v1/" "GET" "apikey,authorization"

set +e
auth_health_status="$(curl -sS -o "$ARTIFACT_DIR/auth-health.body" -w '%{http_code}' \
  "$api_url/auth/v1/health" 2>"$ARTIFACT_DIR/auth-health.stderr")"
auth_health_rc="$?"
set -e
if [[ "$auth_health_rc" -ne 0 ]]; then
  auth_health_status="000"
fi
case "$auth_health_status" in
  2??) pass "auth.health" "HTTP $auth_health_status" ;;
  *) fail "auth.health" "expected 2xx, got HTTP $auth_health_status" ;;
esac

set +e
auth_admin_no_key_status="$(curl -sS -o "$ARTIFACT_DIR/auth-admin-no-key.body" -w '%{http_code}' \
  "$api_url/auth/v1/admin/users" 2>"$ARTIFACT_DIR/auth-admin-no-key.stderr")"
auth_admin_no_key_rc="$?"
set -e
if [[ "$auth_admin_no_key_rc" -ne 0 ]]; then
  auth_admin_no_key_status="000"
fi
case "$auth_admin_no_key_status" in
  401|403) pass "auth.admin_no_key_rejected" "HTTP $auth_admin_no_key_status" ;;
  *) fail "auth.admin_no_key_rejected" "expected 401 or 403, got HTTP $auth_admin_no_key_status" ;;
esac

set +e
auth_admin_status="$(curl -sS -o "$ARTIFACT_DIR/auth-admin.body" -w '%{http_code}' \
  -H "apikey: $service_role" \
  "$api_url/auth/v1/admin/users" 2>"$ARTIFACT_DIR/auth-admin.stderr")"
auth_admin_rc="$?"
set -e
if [[ "$auth_admin_rc" -ne 0 ]]; then
  auth_admin_status="000"
fi
case "$auth_admin_status" in
  2??) pass "auth.admin_service_role" "HTTP $auth_admin_status" ;;
  *) fail "auth.admin_service_role" "expected 2xx, got HTTP $auth_admin_status" ;;
esac

auth_run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
auth_suffix="$(printf '%s' "$auth_run_id" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9-' | cut -c1-48)"
signup_email="compat-signup-${auth_suffix:-signup}@example.test"
login_email="compat-login-${auth_suffix:-login}@example.test"
auth_password="CompatAuth2026-${auth_suffix:-password}!"

signup_body="$ARTIFACT_DIR/auth-signup.body"
signup_err="$ARTIFACT_DIR/auth-signup.stderr"
set +e
signup_status="$(curl -sS -o "$signup_body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $anon_key" \
  -X POST "$api_url/auth/v1/signup" \
  --data "{\"email\":\"$signup_email\",\"password\":\"$auth_password\"}" \
  2>"$signup_err")"
signup_rc="$?"
set -e
if [[ "$signup_rc" -ne 0 ]]; then
  signup_status="000"
fi
case "$signup_status" in
  2??)
    signup_user_id="$(json_get_file_optional "$signup_body" id)"
    if [[ -n "$signup_user_id" ]]; then
      auth_user_ids+=("$signup_user_id")
    fi
    pass "auth.signup" "HTTP $signup_status"
    ;;
  *) fail "auth.signup" "expected 2xx, got HTTP $signup_status" ;;
esac

signup_token_body="$ARTIFACT_DIR/auth-signup-token.body"
signup_token_err="$ARTIFACT_DIR/auth-signup-token.stderr"
set +e
signup_token_status="$(curl -sS -o "$signup_token_body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $anon_key" \
  -X POST "$api_url/auth/v1/token?grant_type=password" \
  --data "{\"email\":\"$signup_email\",\"password\":\"$auth_password\"}" \
  2>"$signup_token_err")"
signup_token_rc="$?"
set -e
if [[ "$signup_token_rc" -ne 0 ]]; then
  signup_token_status="000"
fi
case "$signup_token_status" in
  2??) pass "auth.signup_password_grant" "HTTP $signup_token_status" ;;
  400|401|403)
    if grep -qi 'email_not_confirmed' "$signup_token_body"; then
      pass "auth.signup_password_grant_confirmation_gate" "HTTP $signup_token_status email_not_confirmed"
    else
      fail "auth.signup_password_grant" "unexpected rejection HTTP $signup_token_status: $(cat "$signup_token_body")"
    fi
    ;;
  *) fail "auth.signup_password_grant" "expected 2xx or confirmation gate, got HTTP $signup_token_status" ;;
esac

admin_create_body="$ARTIFACT_DIR/auth-admin-create-user.body"
admin_create_err="$ARTIFACT_DIR/auth-admin-create-user.stderr"
set +e
admin_create_status="$(curl -sS -o "$admin_create_body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/auth/v1/admin/users" \
  --data "{\"email\":\"$login_email\",\"password\":\"$auth_password\",\"email_confirm\":true}" \
  2>"$admin_create_err")"
admin_create_rc="$?"
set -e
if [[ "$admin_create_rc" -ne 0 ]]; then
  admin_create_status="000"
fi
case "$admin_create_status" in
  2??)
    login_user_id="$(json_get_file "$admin_create_body" id)"
    auth_user_ids+=("$login_user_id")
    pass "auth.admin_create_confirmed_user" "HTTP $admin_create_status"
    ;;
  *) fail "auth.admin_create_confirmed_user" "expected 2xx, got HTTP $admin_create_status" ;;
esac

token_body="$ARTIFACT_DIR/auth-token.body"
token_err="$ARTIFACT_DIR/auth-token.stderr"
set +e
token_status="$(curl -sS -o "$token_body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $anon_key" \
  -X POST "$api_url/auth/v1/token?grant_type=password" \
  --data "{\"email\":\"$login_email\",\"password\":\"$auth_password\"}" \
  2>"$token_err")"
token_rc="$?"
set -e
if [[ "$token_rc" -ne 0 ]]; then
  token_status="000"
fi
case "$token_status" in
  2??)
    user_access_token="$(json_get_file "$token_body" access_token)"
    if [[ -z "$user_access_token" ]]; then
      fail "auth.password_grant" "token response did not include access_token"
    fi
    pass "auth.password_grant" "HTTP $token_status"
    ;;
  *) fail "auth.password_grant" "expected 2xx, got HTTP $token_status" ;;
esac

user_body="$ARTIFACT_DIR/auth-user.body"
user_err="$ARTIFACT_DIR/auth-user.stderr"
set +e
user_status="$(curl -sS -o "$user_body" -w '%{http_code}' \
  -H "apikey: $anon_key" \
  -H "Authorization: Bearer $user_access_token" \
  "$api_url/auth/v1/user" \
  2>"$user_err")"
user_rc="$?"
set -e
if [[ "$user_rc" -ne 0 ]]; then
  user_status="000"
fi
case "$user_status" in
  2??)
    user_email="$(json_get_file "$user_body" email)"
    if [[ "$user_email" != "$login_email" ]]; then
      fail "auth.user_session" "expected $login_email, got $user_email"
    fi
    pass "auth.user_session" "HTTP $user_status"
    ;;
  *) fail "auth.user_session" "expected 2xx, got HTTP $user_status" ;;
esac

graphql_payload='{"query":"query { __typename }"}'
set +e
graphql_no_key_status="$(curl -sS -o "$ARTIFACT_DIR/graphql-no-key.body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -X POST "$api_url/graphql/v1" \
  --data "$graphql_payload" 2>"$ARTIFACT_DIR/graphql-no-key.stderr")"
graphql_no_key_rc="$?"
set -e
if [[ "$graphql_no_key_rc" -ne 0 ]]; then
  graphql_no_key_status="000"
fi
case "$graphql_no_key_status" in
  401|403) pass "graphql.no_key_rejected" "HTTP $graphql_no_key_status" ;;
  *) fail "graphql.no_key_rejected" "expected 401 or 403, got HTTP $graphql_no_key_status" ;;
esac

set +e
graphql_status="$(curl -sS -o "$ARTIFACT_DIR/graphql-anon.body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $anon_key" \
  -X POST "$api_url/graphql/v1" \
  --data "$graphql_payload" 2>"$ARTIFACT_DIR/graphql-anon.stderr")"
graphql_rc="$?"
set -e
if [[ "$graphql_rc" -ne 0 ]]; then
  graphql_status="000"
fi
case "$graphql_status" in
  2??) pass "graphql.anon_key_accepted" "HTTP $graphql_status" ;;
  *) fail "graphql.anon_key_accepted" "expected 2xx, got HTTP $graphql_status" ;;
esac

set +e
storage_status="$(curl -sS -o "$ARTIFACT_DIR/storage-buckets.body" -w '%{http_code}' \
  -H "apikey: $service_role" \
  "$api_url/storage/v1/bucket" 2>"$ARTIFACT_DIR/storage-buckets.stderr")"
storage_rc="$?"
set -e
if [[ "$storage_rc" -ne 0 ]]; then
  storage_status="000"
fi
case "$storage_status" in
  2??) pass "storage.service_role_bucket_list" "HTTP $storage_status" ;;
  *) fail "storage.service_role_bucket_list" "expected 2xx, got HTTP $storage_status" ;;
esac

storage_run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
bucket_suffix="$(printf '%s' "$storage_run_id" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9-' | cut -c1-40)"
bucket_id="compat-${bucket_suffix:-bucket}"
object_path="compat-object.txt"
storage_cleanup_bucket="$bucket_id"
storage_cleanup_object="$object_path"
object_body="$ARTIFACT_DIR/storage-object.txt"
printf 'supadupa storage compat %s\n' "$storage_run_id" >"$object_body"

set +e
storage_create_status="$(curl -sS -o "$ARTIFACT_DIR/storage-bucket-create.body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/bucket" \
  --data "{\"id\":\"$bucket_id\",\"name\":\"$bucket_id\",\"public\":false}" \
  2>"$ARTIFACT_DIR/storage-bucket-create.stderr")"
storage_create_rc="$?"
set -e
if [[ "$storage_create_rc" -ne 0 ]]; then
  storage_create_status="000"
fi
case "$storage_create_status" in
  2??) pass "storage.service_role_bucket_create" "HTTP $storage_create_status bucket=$bucket_id" ;;
  *) fail "storage.service_role_bucket_create" "expected 2xx, got HTTP $storage_create_status" ;;
esac

set +e
storage_upload_status="$(curl -sS -o "$ARTIFACT_DIR/storage-object-upload.body" -w '%{http_code}' \
  -H "Content-Type: text/plain" \
  -H "x-upsert: true" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/object/$bucket_id/$object_path" \
  --data-binary @"$object_body" \
  2>"$ARTIFACT_DIR/storage-object-upload.stderr")"
storage_upload_rc="$?"
set -e
if [[ "$storage_upload_rc" -ne 0 ]]; then
  storage_upload_status="000"
fi
case "$storage_upload_status" in
  2??) pass "storage.service_role_object_upload" "HTTP $storage_upload_status" ;;
  *) fail "storage.service_role_object_upload" "expected 2xx, got HTTP $storage_upload_status" ;;
esac

set +e
storage_download_status="$(curl -sS -o "$ARTIFACT_DIR/storage-object-download.body" -w '%{http_code}' \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  "$api_url/storage/v1/object/$bucket_id/$object_path" \
  2>"$ARTIFACT_DIR/storage-object-download.stderr")"
storage_download_rc="$?"
set -e
if [[ "$storage_download_rc" -ne 0 ]]; then
  storage_download_status="000"
fi
case "$storage_download_status" in
  2??)
    if cmp -s "$object_body" "$ARTIFACT_DIR/storage-object-download.body"; then
      pass "storage.service_role_object_download" "HTTP $storage_download_status"
    else
      fail "storage.service_role_object_download" "downloaded object body mismatch"
    fi
    ;;
  *) fail "storage.service_role_object_download" "expected 2xx, got HTTP $storage_download_status" ;;
esac

set +e
storage_private_anon_status="$(curl -sS -o "$ARTIFACT_DIR/storage-private-anon-download.body" -w '%{http_code}' \
  -H "apikey: $anon_key" \
  "$api_url/storage/v1/object/$bucket_id/$object_path" \
  2>"$ARTIFACT_DIR/storage-private-anon-download.stderr")"
storage_private_anon_rc="$?"
set -e
if [[ "$storage_private_anon_rc" -ne 0 ]]; then
  storage_private_anon_status="000"
fi
case "$storage_private_anon_status" in
  401|403|404) pass "storage.private_anon_rejected" "HTTP $storage_private_anon_status" ;;
  400)
    if grep -Eqi '"not_found"|"Object not found"' "$ARTIFACT_DIR/storage-private-anon-download.body"; then
      pass "storage.private_anon_rejected" "HTTP $storage_private_anon_status not_found"
    else
      fail "storage.private_anon_rejected" "unexpected HTTP 400 response: $(cat "$ARTIFACT_DIR/storage-private-anon-download.body")"
    fi
    ;;
  *) fail "storage.private_anon_rejected" "expected 401, 403, or 404, got HTTP $storage_private_anon_status" ;;
esac

signed_url_body="$ARTIFACT_DIR/storage-signed-url.body"
signed_url_err="$ARTIFACT_DIR/storage-signed-url.stderr"
set +e
signed_url_status="$(curl -sS -o "$signed_url_body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/object/sign/$bucket_id/$object_path" \
  --data '{"expiresIn":3600}' \
  2>"$signed_url_err")"
signed_url_rc="$?"
set -e
if [[ "$signed_url_rc" -ne 0 ]]; then
  signed_url_status="000"
fi
case "$signed_url_status" in
  2??)
    signed_url="$(node - "$signed_url_body" "$api_url" <<'NODE'
const fs = require("fs");
const path = process.argv[2];
const apiURL = process.argv[3].replace(/\/$/, "");
const body = JSON.parse(fs.readFileSync(path, "utf8"));
const raw = body.signedURL || body.signedUrl || body.url || "";
if (!raw) process.exit(2);
if (/^https?:\/\//i.test(raw)) {
  process.stdout.write(raw);
} else if (raw.startsWith("/storage/v1/")) {
  process.stdout.write(apiURL + raw);
} else if (raw.startsWith("/")) {
  process.stdout.write(apiURL + "/storage/v1" + raw);
} else {
  process.stdout.write(apiURL + "/storage/v1/" + raw);
}
NODE
)"
    if [[ -z "$signed_url" ]]; then
      fail "storage.signed_url_create" "response did not include signedURL"
    fi
    pass "storage.signed_url_create" "HTTP $signed_url_status"
    ;;
  *) fail "storage.signed_url_create" "expected 2xx, got HTTP $signed_url_status" ;;
esac

set +e
signed_download_status="$(curl -sS -o "$ARTIFACT_DIR/storage-signed-url-download.body" -w '%{http_code}' \
  "$signed_url" \
  2>"$ARTIFACT_DIR/storage-signed-url-download.stderr")"
signed_download_rc="$?"
set -e
if [[ "$signed_download_rc" -ne 0 ]]; then
  signed_download_status="000"
fi
case "$signed_download_status" in
  2??)
    if cmp -s "$object_body" "$ARTIFACT_DIR/storage-signed-url-download.body"; then
      pass "storage.signed_url_download" "HTTP $signed_download_status"
    else
      fail "storage.signed_url_download" "downloaded object body mismatch"
    fi
    ;;
  *) fail "storage.signed_url_download" "expected 2xx, got HTTP $signed_download_status" ;;
esac

curl -sS -o "$ARTIFACT_DIR/storage-object-delete.body" \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X DELETE "$api_url/storage/v1/object/$bucket_id" \
  --data "{\"prefixes\":[\"$object_path\"]}" \
  2>"$ARTIFACT_DIR/storage-object-delete.stderr" || true
curl -sS -o "$ARTIFACT_DIR/storage-bucket-delete.body" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X DELETE "$api_url/storage/v1/bucket/$bucket_id" \
  2>"$ARTIFACT_DIR/storage-bucket-delete.stderr" || true
storage_cleanup_bucket=""
storage_cleanup_object=""

function_name="${SUPADUPA_COMPAT_FUNCTION_NAME:-hello}"
set +e
function_status="$(curl -sS -o "$ARTIFACT_DIR/function-$function_name.body" -w '%{http_code}' \
  -H "apikey: $anon_key" \
  -H "Authorization: Bearer $anon_key" \
  "$api_url/functions/v1/$function_name" 2>"$ARTIFACT_DIR/function-$function_name.stderr")"
function_rc="$?"
set -e
if [[ "$function_rc" -ne 0 ]]; then
  function_status="000"
fi
case "$function_status" in
  2??) pass "functions.$function_name" "HTTP $function_status" ;;
  *) fail "functions.$function_name" "expected 2xx, got HTTP $function_status" ;;
esac
