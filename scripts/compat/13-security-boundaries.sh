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

api_base="${SUPADUPA_API_URL%/}"
api_url="$(profile_value api_url)"
studio_url="$(profile_value studio_url)"
token="$(read_secret_file "$ARTIFACT_DIR/token")"
anon_key="$(reveal_secret_value anon_key)"
service_role="$(reveal_secret_value service_role)"
db_password="$(reveal_secret_value db_password)"

expect_rejected_status() {
  local test_name="$1"
  local status="$2"

  case "$status" in
    401|403)
      pass "$test_name" "HTTP $status"
      ;;
    *)
      fail "$test_name" "expected 401 or 403, got HTTP $status"
      ;;
  esac
}

profile_security_check() {
  local profile="$ARTIFACT_DIR/profile.json"
  if node -e '
const fs = require("fs");
const profile = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const requiredHandles = ["anon_key", "service_role", "db_password", "jwt_secret"];
for (const key of requiredHandles) {
  const value = profile.secret_handles?.[key];
  if (typeof value !== "string" || !value.startsWith("secret://")) {
    throw new Error(`secret_handles.${key} is not a secret:// handle`);
  }
}
const envKeys = ["SUPABASE_ANON_KEY", "SUPABASE_SERVICE_ROLE_KEY", "SUPABASE_DB_PASSWORD"];
for (const key of envKeys) {
  const value = profile.env?.[key];
  if (typeof value !== "string" || !value.startsWith("secret://")) {
    throw new Error(`env.${key} is not a secret:// handle`);
  }
}
for (const key of ["database_url", "public_database_url", "pooler_transaction_url", "pooler_session_url"]) {
  const value = profile[key] || "";
  if (value && !value.includes("${DB_PASSWORD}") && !value.includes("$DB_PASSWORD")) {
    throw new Error(`${key} does not preserve a DB password placeholder`);
  }
}
' "$profile" >"$ARTIFACT_DIR/security-profile.out" 2>"$ARTIFACT_DIR/security-profile.stderr"; then
    pass "security.profile_secret_handles" "CLI profile uses secret handles and DB password placeholders"
  else
    fail "security.profile_secret_handles" "profile leaked raw secret material or malformed handles; see security-profile.stderr"
  fi
}

profile_security_check

set +e
admin_project_status="$(curl -sS -o "$ARTIFACT_DIR/security-admin-token-project-api.body" -w '%{http_code}' \
  -H "apikey: $token" \
  -H "Authorization: Bearer $token" \
  "$api_url/rest/v1/" 2>"$ARTIFACT_DIR/security-admin-token-project-api.stderr")"
admin_project_rc="$?"
set -e
if [[ "$admin_project_rc" -ne 0 ]]; then
  admin_project_status="000"
fi
expect_rejected_status "security.admin_token_project_api_rejected" "$admin_project_status"

set +e
service_mgmt_status="$(curl -sS -o "$ARTIFACT_DIR/security-service-role-management.body" -w '%{http_code}' \
  -H "Authorization: Bearer $service_role" \
  "$api_base/v1/projects/$SUPADUPA_TEST_REF" 2>"$ARTIFACT_DIR/security-service-role-management.stderr")"
service_mgmt_rc="$?"
set -e
if [[ "$service_mgmt_rc" -ne 0 ]]; then
  service_mgmt_status="000"
fi
expect_rejected_status "security.service_role_management_api_rejected" "$service_mgmt_status"

set +e
anon_mgmt_status="$(curl -sS -o "$ARTIFACT_DIR/security-anon-management.body" -w '%{http_code}' \
  -H "Authorization: Bearer $anon_key" \
  "$api_base/v1/projects/$SUPADUPA_TEST_REF" 2>"$ARTIFACT_DIR/security-anon-management.stderr")"
anon_mgmt_rc="$?"
set -e
if [[ "$anon_mgmt_rc" -ne 0 ]]; then
  anon_mgmt_status="000"
fi
expect_rejected_status "security.anon_management_api_rejected" "$anon_mgmt_status"

set +e
noauth_scim_status="$(curl -sS -o "$ARTIFACT_DIR/security-scim-noauth.body" -w '%{http_code}' \
  "$api_base/v1/scim/v2/Users" 2>"$ARTIFACT_DIR/security-scim-noauth.stderr")"
noauth_scim_rc="$?"
set -e
if [[ "$noauth_scim_rc" -ne 0 ]]; then
  noauth_scim_status="000"
fi
expect_rejected_status "security.scim_requires_bearer_or_admin" "$noauth_scim_status"

set +e
service_scim_status="$(curl -sS -o "$ARTIFACT_DIR/security-service-role-scim.body" -w '%{http_code}' \
  -H "Authorization: Bearer $service_role" \
  "$api_base/v1/scim/v2/Users" 2>"$ARTIFACT_DIR/security-service-role-scim.stderr")"
service_scim_rc="$?"
set -e
if [[ "$service_scim_rc" -ne 0 ]]; then
  service_scim_status="000"
fi
expect_rejected_status "security.service_role_scim_rejected" "$service_scim_status"

if [[ -n "${SUPADUPA_COMPAT_SCIM_TOKEN:-}" ]]; then
  scim_body="$ARTIFACT_DIR/security-scim-token.body"
  scim_err="$ARTIFACT_DIR/security-scim-token.stderr"
  if curl -fsS \
    -H "Authorization: Bearer $SUPADUPA_COMPAT_SCIM_TOKEN" \
    "$api_base/v1/scim/v2/Users" \
    >"$scim_body" 2>"$scim_err"; then
    if grep -Fq "$SUPADUPA_COMPAT_SCIM_TOKEN" "$scim_body"; then
      fail "security.scim_token_redacted" "SCIM token appeared in SCIM response"
    fi
    pass "security.scim_bearer_token" "configured SCIM bearer token can list users"
  else
    fail "security.scim_bearer_token" "configured SCIM bearer token failed; see security-scim-token.stderr"
  fi
else
  skip "security.scim_bearer_token" "SUPADUPA_COMPAT_SCIM_TOKEN not configured"
fi

set +e
noauth_secret_status="$(curl -sS -o "$ARTIFACT_DIR/security-secret-reveal-noauth.body" -w '%{http_code}' \
  "$api_base/v1/projects/$SUPADUPA_TEST_REF/secrets/db_password/reveal" \
  2>"$ARTIFACT_DIR/security-secret-reveal-noauth.stderr")"
noauth_secret_rc="$?"
set -e
if [[ "$noauth_secret_rc" -ne 0 ]]; then
  noauth_secret_status="000"
fi
expect_rejected_status "security.secret_reveal_requires_auth" "$noauth_secret_status"

set +e
studio_status="$(curl -sS -o "$ARTIFACT_DIR/security-studio-noauth.body" -w '%{http_code}' \
  "$studio_url" 2>"$ARTIFACT_DIR/security-studio-noauth.stderr")"
studio_rc="$?"
set -e
if [[ "$studio_rc" -ne 0 ]]; then
  studio_status="000"
fi
expect_rejected_status "security.studio_requires_supadupa_auth" "$studio_status"

logs_body="$ARTIFACT_DIR/security-project-logs.body"
logs_err="$ARTIFACT_DIR/security-project-logs.stderr"
if curl -fsS \
  -H "Authorization: Bearer $token" \
  "$api_base/v1/projects/$SUPADUPA_TEST_REF/logs" \
  >"$logs_body" 2>"$logs_err"; then
  if ANON_KEY="$anon_key" SERVICE_ROLE="$service_role" DB_PASSWORD="$db_password" node -e '
const fs = require("fs");
const body = fs.readFileSync(process.argv[1], "utf8");
for (const [name, value] of Object.entries({
  anon_key: process.env.ANON_KEY,
  service_role: process.env.SERVICE_ROLE,
  db_password: process.env.DB_PASSWORD,
})) {
  if (value && body.includes(value)) {
    throw new Error(`${name} value appeared in project logs`);
  }
}
' "$logs_body" >"$ARTIFACT_DIR/security-logs.out" 2>"$ARTIFACT_DIR/security-logs.stderr"; then
    pass "security.revealed_secrets_not_in_logs" "project logs do not contain revealed secret values"
  else
    fail "security.revealed_secrets_not_in_logs" "secret material appeared in logs; see security-logs.stderr"
  fi
else
  fail "security.project_logs" "failed to fetch project logs; see security-project-logs.stderr"
fi
