#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool curl
require_tool node
require_tool psql
ensure_token
ensure_profile

if [[ ! -d "$SCRIPT_DIR/node_modules/@supabase/supabase-js" ]]; then
  if (cd "$SCRIPT_DIR" && npm install) >"$ARTIFACT_DIR/auth-deep-sdk-install.out" 2>"$ARTIFACT_DIR/auth-deep-sdk-install.stderr"; then
    pass "auth_deep.sdk.install" "@supabase/supabase-js installed"
  else
    fail "auth_deep.sdk.install" "npm install failed; see auth-deep-sdk-install.stderr"
  fi
fi

api_url="$(profile_value api_url)"
api_host="$(node -e 'process.stdout.write(new URL(process.argv[1]).host)' "$api_url")"
public_db_url="$(profile_value_optional public_database_url)"
if [[ -z "$public_db_url" ]]; then
  fail "auth_deep.public_db_url" "profile did not include public_database_url"
fi
public_db_safe_url="$(url_without_password "$public_db_url")"
db_password="$(reveal_secret_value db_password)"
anon_key="$(reveal_secret_value anon_key)"
service_role="$(reveal_secret_value service_role)"
run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
auth_suffix="$(printf '%s' "$run_id" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9-' | cut -c1-48)"
login_email="compat-auth-deep-${auth_suffix:-auth}@example.test"
auth_password="CompatAuth2026-${auth_suffix:-password}!"
user_id=""
user_access_token=""
refreshed_access_token=""
refresh_token=""
refreshed_refresh_token=""
smtp_recovery_user_id=""
smtp_signup_user_id=""
smtp_magic_user_id=""
smtp_original_config_file="$ARTIFACT_DIR/auth-deep-smtp-original.json"
email_templates_original_config_file="$ARTIFACT_DIR/auth-deep-email-templates-original.json"
auth_providers_original_config_file="$ARTIFACT_DIR/auth-deep-auth-providers-original.json"
sms_runtime_original_config_file="$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers-original.json"
smtp_mailpit_container=""
auth_hook_user_id=""
before_user_created_allowed_user_id=""
auth_hook_function=""
auth_hook_created="false"
before_user_created_hook_created="false"
signed_hook_function=""
signed_hook_created="false"
signed_hook_secret_kind=""
signed_hook_user_id=""
send_email_hook_function=""
send_email_hook_created="false"
send_email_hook_user_id=""
send_sms_hook_function=""
send_sms_hook_created="false"
send_sms_hook_user_id=""
send_sms_auth_providers_modified="false"
sms_runtime_auth_providers_modified="false"
sms_runtime_test_otp_secret_kind=""
sms_runtime_provider_secret_kind=""
real_sms_auth_providers_modified="false"
real_sms_original_config_file="$ARTIFACT_DIR/auth-deep-real-sms-auth-providers-original.json"
real_sms_secret_kind=""
real_sms_user_id=""

cleanup_auth_deep() {
  if [[ -n "${smtp_recovery_user_id:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-smtp-recovery-user-delete.body" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/auth/v1/admin/users/$smtp_recovery_user_id" \
      2>"$ARTIFACT_DIR/auth-deep-smtp-recovery-user-delete.stderr" || true
  fi
  if [[ -n "${smtp_signup_user_id:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-smtp-signup-user-delete.body" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/auth/v1/admin/users/$smtp_signup_user_id" \
      2>"$ARTIFACT_DIR/auth-deep-smtp-signup-user-delete.stderr" || true
  fi
  if [[ -n "${smtp_magic_user_id:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-smtp-magic-user-delete.body" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/auth/v1/admin/users/$smtp_magic_user_id" \
      2>"$ARTIFACT_DIR/auth-deep-smtp-magic-user-delete.stderr" || true
  fi
  if [[ -n "${auth_hook_user_id:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-hook-user-delete.body" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/auth/v1/admin/users/$auth_hook_user_id" \
      2>"$ARTIFACT_DIR/auth-deep-hook-user-delete.stderr" || true
  fi
  if [[ -n "${signed_hook_user_id:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-signed-hook-user-delete.body" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/auth/v1/admin/users/$signed_hook_user_id" \
      2>"$ARTIFACT_DIR/auth-deep-signed-hook-user-delete.stderr" || true
  fi
  if [[ -n "${before_user_created_allowed_user_id:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-before-user-created-allowed-user-delete.body" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/auth/v1/admin/users/$before_user_created_allowed_user_id" \
      2>"$ARTIFACT_DIR/auth-deep-before-user-created-allowed-user-delete.stderr" || true
  fi
  if [[ -n "${send_email_hook_user_id:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-send-email-user-delete.body" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/auth/v1/admin/users/$send_email_hook_user_id" \
      2>"$ARTIFACT_DIR/auth-deep-send-email-user-delete.stderr" || true
  fi
  if [[ -n "${send_sms_hook_user_id:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-send-sms-user-delete.body" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/auth/v1/admin/users/$send_sms_hook_user_id" \
      2>"$ARTIFACT_DIR/auth-deep-send-sms-user-delete.stderr" || true
  fi
  if [[ -n "${real_sms_user_id:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-real-sms-user-delete.body" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/auth/v1/admin/users/$real_sms_user_id" \
      2>"$ARTIFACT_DIR/auth-deep-real-sms-user-delete.stderr" || true
  fi
  if [[ "$real_sms_auth_providers_modified" == "true" && -f "$real_sms_original_config_file" ]]; then
    node - "$real_sms_original_config_file" "$ARTIFACT_DIR/auth-deep-real-sms-auth-providers-restore-payload.json" <<'NODE' || true
const fs = require("fs");
const source = process.argv[2];
const target = process.argv[3];
const payload = JSON.parse(fs.readFileSync(source, "utf8"));
fs.writeFileSync(target, JSON.stringify({ config: payload.config ?? {} }));
NODE
    if [[ -s "$ARTIFACT_DIR/auth-deep-real-sms-auth-providers-restore-payload.json" ]]; then
      curl -sS -o "$ARTIFACT_DIR/auth-deep-real-sms-auth-providers-restore.body" \
        -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
        -H "Content-Type: application/json" \
        -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/auth_providers" \
        --data-binary "@$ARTIFACT_DIR/auth-deep-real-sms-auth-providers-restore-payload.json" \
        2>"$ARTIFACT_DIR/auth-deep-real-sms-auth-providers-restore.stderr" || true
    fi
  fi
  if [[ -n "${real_sms_secret_kind:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-real-sms-secret-delete.body" \
      -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
      -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/secrets/$real_sms_secret_kind" \
      2>"$ARTIFACT_DIR/auth-deep-real-sms-secret-delete.stderr" || true
  fi
  if [[ "$sms_runtime_auth_providers_modified" == "true" && -f "$sms_runtime_original_config_file" ]]; then
    node - "$sms_runtime_original_config_file" "$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers-restore-payload.json" <<'NODE' || true
const fs = require("fs");
const source = process.argv[2];
const target = process.argv[3];
const payload = JSON.parse(fs.readFileSync(source, "utf8"));
fs.writeFileSync(target, JSON.stringify({ config: payload.config ?? {} }));
NODE
    if [[ -s "$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers-restore-payload.json" ]]; then
      curl -sS -o "$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers-restore.body" \
        -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
        -H "Content-Type: application/json" \
        -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/auth_providers" \
        --data-binary "@$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers-restore-payload.json" \
        2>"$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers-restore.stderr" || true
    fi
  fi
  if [[ -n "${sms_runtime_test_otp_secret_kind:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-sms-runtime-test-otp-secret-delete.body" \
      -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
      -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/secrets/$sms_runtime_test_otp_secret_kind" \
      2>"$ARTIFACT_DIR/auth-deep-sms-runtime-test-otp-secret-delete.stderr" || true
  fi
  if [[ -n "${sms_runtime_provider_secret_kind:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-sms-runtime-provider-secret-delete.body" \
      -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
      -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/secrets/$sms_runtime_provider_secret_kind" \
      2>"$ARTIFACT_DIR/auth-deep-sms-runtime-provider-secret-delete.stderr" || true
  fi
  if [[ -n "${user_id:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-user-delete.body" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/auth/v1/admin/users/$user_id" \
      2>"$ARTIFACT_DIR/auth-deep-user-delete.stderr" || true
  fi
  if [[ "$auth_hook_created" == "true" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-hook-delete.body" \
      -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
      -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks/custom_access_token" \
      2>"$ARTIFACT_DIR/auth-deep-hook-delete.stderr" || true
  fi
  if [[ "$before_user_created_hook_created" == "true" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-before-user-created-hook-delete.body" \
      -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
      -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks/before_user_created" \
      2>"$ARTIFACT_DIR/auth-deep-before-user-created-hook-delete.stderr" || true
  fi
  if [[ "$signed_hook_created" == "true" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-signed-hook-delete.body" \
      -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
      -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks/custom_access_token" \
      2>"$ARTIFACT_DIR/auth-deep-signed-hook-delete.stderr" || true
  fi
  if [[ -n "${signed_hook_function:-}" ]]; then
    supadupa_cli_authed functions delete --ref "$SUPADUPA_TEST_REF" --name "$signed_hook_function" \
      >"$ARTIFACT_DIR/auth-deep-signed-hook-function-delete.out" 2>"$ARTIFACT_DIR/auth-deep-signed-hook-function-delete.stderr" || true
  fi
  if [[ -n "${signed_hook_secret_kind:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-signed-hook-secret-delete.body" \
      -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
      -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/secrets/$signed_hook_secret_kind" \
      2>"$ARTIFACT_DIR/auth-deep-signed-hook-secret-delete.stderr" || true
  fi
  if [[ "$send_email_hook_created" == "true" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-send-email-hook-delete.body" \
      -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
      -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks/send_email" \
      2>"$ARTIFACT_DIR/auth-deep-send-email-hook-delete.stderr" || true
  fi
  if [[ "$send_sms_hook_created" == "true" ]]; then
    curl -sS -o "$ARTIFACT_DIR/auth-deep-send-sms-hook-delete.body" \
      -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
      -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks/send_sms" \
      2>"$ARTIFACT_DIR/auth-deep-send-sms-hook-delete.stderr" || true
  fi
  if [[ -n "${send_email_hook_function:-}" ]]; then
    supadupa_cli_authed functions delete --ref "$SUPADUPA_TEST_REF" --name "$send_email_hook_function" \
      >"$ARTIFACT_DIR/auth-deep-send-email-hook-function-delete.out" 2>"$ARTIFACT_DIR/auth-deep-send-email-hook-function-delete.stderr" || true
  fi
  if [[ -n "${send_sms_hook_function:-}" ]]; then
    supadupa_cli_authed functions delete --ref "$SUPADUPA_TEST_REF" --name "$send_sms_hook_function" \
      >"$ARTIFACT_DIR/auth-deep-send-sms-hook-function-delete.out" 2>"$ARTIFACT_DIR/auth-deep-send-sms-hook-function-delete.stderr" || true
  fi
  if [[ -n "${auth_hook_function:-}" ]]; then
    supadupa_cli_authed functions delete --ref "$SUPADUPA_TEST_REF" --name "$auth_hook_function" \
      >"$ARTIFACT_DIR/auth-deep-hook-function-delete.out" 2>"$ARTIFACT_DIR/auth-deep-hook-function-delete.stderr" || true
  fi
  if [[ -n "${db_password:-}" && -n "${public_db_safe_url:-}" ]]; then
    PGPASSWORD="$db_password" psql "$public_db_safe_url" \
      -v ON_ERROR_STOP=1 \
      -q >"$ARTIFACT_DIR/auth-deep-rls-cleanup.out" 2>"$ARTIFACT_DIR/auth-deep-rls-cleanup.stderr" <<'SQL' || true
drop table if exists public.compat_auth_rls;
drop table if exists public.compat_auth_hook_events;
drop table if exists public.compat_auth_sms_hook_events;
SQL
  fi
  if [[ "$send_sms_auth_providers_modified" == "true" && -f "$auth_providers_original_config_file" ]]; then
    node - "$auth_providers_original_config_file" "$ARTIFACT_DIR/auth-deep-auth-providers-restore-payload.json" <<'NODE' || true
const fs = require("fs");
const source = process.argv[2];
const target = process.argv[3];
const payload = JSON.parse(fs.readFileSync(source, "utf8"));
fs.writeFileSync(target, JSON.stringify({ config: payload.config ?? {} }));
NODE
    if [[ -s "$ARTIFACT_DIR/auth-deep-auth-providers-restore-payload.json" ]]; then
      curl -sS -o "$ARTIFACT_DIR/auth-deep-auth-providers-restore.body" \
        -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
        -H "Content-Type: application/json" \
        -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/auth_providers" \
        --data-binary "@$ARTIFACT_DIR/auth-deep-auth-providers-restore-payload.json" \
        2>"$ARTIFACT_DIR/auth-deep-auth-providers-restore.stderr" || true
    fi
  fi
  if [[ -f "$smtp_original_config_file" ]]; then
    node - "$smtp_original_config_file" "$ARTIFACT_DIR/auth-deep-smtp-restore-payload.json" <<'NODE' || true
const fs = require("fs");
const source = process.argv[2];
const target = process.argv[3];
const payload = JSON.parse(fs.readFileSync(source, "utf8"));
fs.writeFileSync(target, JSON.stringify({ config: payload.config ?? {} }));
NODE
    if [[ -s "$ARTIFACT_DIR/auth-deep-smtp-restore-payload.json" ]]; then
      curl -sS -o "$ARTIFACT_DIR/auth-deep-smtp-restore.body" \
        -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
        -H "Content-Type: application/json" \
        -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/smtp" \
        --data-binary "@$ARTIFACT_DIR/auth-deep-smtp-restore-payload.json" \
        2>"$ARTIFACT_DIR/auth-deep-smtp-restore.stderr" || true
    fi
  fi
  if [[ -f "$email_templates_original_config_file" ]]; then
    node - "$email_templates_original_config_file" "$ARTIFACT_DIR/auth-deep-email-templates-restore-payload.json" <<'NODE' || true
const fs = require("fs");
const source = process.argv[2];
const target = process.argv[3];
const payload = JSON.parse(fs.readFileSync(source, "utf8"));
fs.writeFileSync(target, JSON.stringify({ config: payload.config ?? {} }));
NODE
    if [[ -s "$ARTIFACT_DIR/auth-deep-email-templates-restore-payload.json" ]]; then
      curl -sS -o "$ARTIFACT_DIR/auth-deep-email-templates-restore.body" \
        -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token" 2>/dev/null || true)" \
        -H "Content-Type: application/json" \
        -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/email_templates" \
        --data-binary "@$ARTIFACT_DIR/auth-deep-email-templates-restore-payload.json" \
        2>"$ARTIFACT_DIR/auth-deep-email-templates-restore.stderr" || true
    fi
  fi
  if [[ -n "${smtp_mailpit_container:-}" ]]; then
    docker rm -f "$smtp_mailpit_container" >/dev/null 2>&1 || true
  fi
}
trap cleanup_auth_deep EXIT

wait_auth_ready() {
  local test_name="$1"
  local deadline=$((SECONDS + 90))
  local health_status
  while (( SECONDS < deadline )); do
    health_status="$(curl -sS -o "$ARTIFACT_DIR/$test_name.body" -w '%{http_code}' "$api_url/auth/v1/health" 2>"$ARTIFACT_DIR/$test_name.stderr" || printf '000')"
    if [[ "$health_status" =~ ^2 ]]; then
      pass "$test_name" "HTTP $health_status"
      return 0
    fi
    sleep 3
  done
  fail "$test_name" "auth health did not recover"
}

project_runtime_network() {
  local auth_container="${SUPADUPA_TEST_REF}-auth-1"
  if ! docker inspect "$auth_container" >/dev/null 2>&1; then
    return 1
  fi
  docker inspect "$auth_container" --format '{{range $name,$net := .NetworkSettings.Networks}}{{println $name}}{{end}}' | head -1
}

mailpit_message_for() {
  local container="$1"
  local email="$2"
  local subject="$3"
  local out="$4"
  local deadline=$((SECONDS + 60))
  local messages_file="$ARTIFACT_DIR/auth-deep-mailpit-messages.json"
  local message_id

  while (( SECONDS < deadline )); do
    if docker exec "$container" wget -qO- http://127.0.0.1:8025/api/v1/messages >"$messages_file" 2>"$ARTIFACT_DIR/auth-deep-mailpit-messages.stderr"; then
      message_id="$(node -e '
const fs = require("fs");
const email = process.argv[1];
const subject = process.argv[2];
const payload = JSON.parse(fs.readFileSync(0, "utf8"));
const messages = payload.messages || [];
for (const message of messages) {
  const recipients = message.To || [];
  if (message.Subject === subject && recipients.some((to) => to.Address === email)) {
    process.stdout.write(message.ID);
    process.exit(0);
  }
}
process.exit(2);
' "$email" "$subject" <"$messages_file" 2>/dev/null || true)"
      if [[ -n "$message_id" ]]; then
        docker exec "$container" wget -qO- "http://127.0.0.1:8025/api/v1/message/$message_id" >"$out" 2>"$out.stderr"
        return 0
      fi
    fi
    sleep 2
  done
  return 1
}

auth_response_user_id() {
  node - "$1" <<'NODE'
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
process.stdout.write(payload.user?.id || payload.id || "");
NODE
}

mail_otp_code() {
  node - "$1" <<'NODE'
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const text = String(payload.Text || payload.HTML || "");
const match = text.match(/code:\s*([0-9]+)/i);
if (!match) process.exit(2);
process.stdout.write(match[1]);
NODE
}

validate_mail_links_project_host() {
  node - "$1" "$2" "$3" <<'NODE'
const fs = require("fs");
const file = process.argv[2];
const expectedHost = process.argv[3];
const expectedType = process.argv[4];
const payload = JSON.parse(fs.readFileSync(file, "utf8"));
const text = String(payload.Text || "") + "\n" + String(payload.HTML || "");
if (!text.includes(`https://${expectedHost}/verify?`)) throw new Error(`missing project verify host ${expectedHost}`);
if (!text.includes(`type=${expectedType}`) && !text.includes(`type=${expectedType.replaceAll("&", "&amp;")}`)) {
  throw new Error(`missing type=${expectedType}`);
}
NODE
}

run_auth_hook_checks() {
  local mode="${SUPADUPA_COMPAT_AUTH_HOOKS:-auto}"
  if [[ "$mode" == "false" || "$mode" == "0" || "$mode" == "off" ]]; then
    skip "auth_deep.hooks" "SUPADUPA_COMPAT_AUTH_HOOKS disabled"
    return 0
  fi

  curl -sS -o "$ARTIFACT_DIR/auth-deep-hooks-list-before.json" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks" \
    2>"$ARTIFACT_DIR/auth-deep-hooks-list-before.stderr"
  if node - "$ARTIFACT_DIR/auth-deep-hooks-list-before.json" <<'NODE' >/dev/null 2>&1; then
const fs = require("fs");
const hooks = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (hooks.some((hook) => hook.hook_type === "custom_access_token" || hook.hook_type === "before_user_created")) process.exit(0);
process.exit(1);
NODE
    if [[ "$mode" == "true" || "$mode" == "1" || "$mode" == "on" ]]; then
      fail "auth_deep.hooks.existing" "custom_access_token or before_user_created hook already exists; refusing to overwrite"
    fi
    skip "auth_deep.hooks" "custom_access_token or before_user_created hook already exists; refusing to overwrite"
    return 0
  fi

  auth_hook_function="compat-auth-hook-${suffix:-hook}"
  local hook_source="$ARTIFACT_DIR/auth-deep-hook-function.ts"
  cat >"$hook_source" <<'TS'
Deno.serve(async (req: Request) => {
  const payload = await req.json().catch(() => ({}));
  const hookName = payload?.metadata?.name ?? "";
  const email = String(payload?.user?.email ?? "");
  if (hookName === "before-user-created" && email.endsWith("@blocked.example.test")) {
    return new Response(JSON.stringify({
      error: {
        http_code: 400,
        message: "blocked by supadupa compat before-user-created hook",
      },
    }), {
      headers: { "Content-Type": "application/json" },
    });
  }
  if (hookName === "before-user-created") {
    return new Response("{}", {
      headers: { "Content-Type": "application/json" },
    });
  }
  const claims = payload?.claims ?? {};
  claims.supadupa_hook = "auth-deep";
  return new Response(JSON.stringify({ claims }), {
    headers: { "Content-Type": "application/json" },
  });
});
TS
  if supadupa_cli_authed functions deploy \
    --ref "$SUPADUPA_TEST_REF" \
    --name "$auth_hook_function" \
    --entrypoint index.ts \
    --source-file "$hook_source" \
    --verify-jwt=false \
    >"$ARTIFACT_DIR/auth-deep-hook-function-deploy.json" 2>"$ARTIFACT_DIR/auth-deep-hook-function-deploy.stderr"; then
    pass "auth_deep.hook_function_deploy" "$auth_hook_function"
  else
    fail "auth_deep.hook_function_deploy" "deploy failed; see auth-deep-hook-function-deploy.stderr"
  fi

  cat >"$ARTIFACT_DIR/auth-deep-hook-payload.json" <<'JSON'
{"hook_type":"custom_access_token","enabled":true,"edge_function":"__FUNCTION__","timeout_ms":5000,"retry_attempts":0}
JSON
  node - "$ARTIFACT_DIR/auth-deep-hook-payload.json" "$auth_hook_function" <<'NODE'
const fs = require("fs");
const path = process.argv[2];
const functionName = process.argv[3];
const payload = JSON.parse(fs.readFileSync(path, "utf8"));
payload.edge_function = functionName;
fs.writeFileSync(path, JSON.stringify(payload));
NODE
  local hook_status
  hook_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-hook-create.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X POST "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-hook-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-hook-create.stderr" || printf '000')"
  case "$hook_status" in
    2??)
      auth_hook_created="true"
      pass "auth_deep.hook_config" "HTTP $hook_status"
      ;;
    *) fail "auth_deep.hook_config" "expected 2xx, got HTTP $hook_status" ;;
  esac
  wait_auth_ready "auth_deep.hook_auth_ready"

  local hook_email="compat-auth-hook-${auth_suffix:-auth}@example.test"
  local hook_password="CompatHook2026-${auth_suffix:-password}!"
  local hook_create_status
  hook_create_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-hook-user-create.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X POST "$api_url/auth/v1/admin/users" \
    --data "{\"email\":\"$hook_email\",\"password\":\"$hook_password\",\"email_confirm\":true}" \
    2>"$ARTIFACT_DIR/auth-deep-hook-user-create.stderr" || printf '000')"
  case "$hook_create_status" in
    2??)
      auth_hook_user_id="$(json_get_file "$ARTIFACT_DIR/auth-deep-hook-user-create.body" id)"
      pass "auth_deep.hook_user_create" "HTTP $hook_create_status"
      ;;
    *) fail "auth_deep.hook_user_create" "expected 2xx, got HTTP $hook_create_status" ;;
  esac
  local token_status
  token_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-hook-token.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/token?grant_type=password" \
    --data "{\"email\":\"$hook_email\",\"password\":\"$hook_password\"}" \
    2>"$ARTIFACT_DIR/auth-deep-hook-token.stderr" || printf '000')"
  case "$token_status" in
    2??)
      node - "$ARTIFACT_DIR/auth-deep-hook-token.body" <<'NODE'
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const token = payload.access_token || "";
const claims = JSON.parse(Buffer.from(token.split(".")[1], "base64url").toString("utf8"));
if (claims.supadupa_hook !== "auth-deep") {
  throw new Error(`expected supadupa_hook claim, got ${claims.supadupa_hook}`);
}
NODE
      pass "auth_deep.hook_custom_access_token" "custom claim present in issued JWT"
      ;;
    *) fail "auth_deep.hook_custom_access_token" "expected 2xx token response, got HTTP $token_status" ;;
  esac
  local refresh_hook_token
  refresh_hook_token="$(json_get_file "$ARTIFACT_DIR/auth-deep-hook-token.body" refresh_token)"
  if [[ -n "$refresh_hook_token" ]]; then
    local refresh_hook_status
    refresh_hook_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-hook-refresh.body" -w '%{http_code}' \
      -H "Content-Type: application/json" \
      -H "apikey: $anon_key" \
      -X POST "$api_url/auth/v1/token?grant_type=refresh_token" \
      --data "{\"refresh_token\":\"$refresh_hook_token\"}" \
      2>"$ARTIFACT_DIR/auth-deep-hook-refresh.stderr" || printf '000')"
    case "$refresh_hook_status" in
      2??)
        node - "$ARTIFACT_DIR/auth-deep-hook-refresh.body" <<'NODE'
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const token = payload.access_token || "";
const claims = JSON.parse(Buffer.from(token.split(".")[1], "base64url").toString("utf8"));
if (claims.supadupa_hook !== "auth-deep") {
  throw new Error(`expected refreshed supadupa_hook claim, got ${claims.supadupa_hook}`);
}
NODE
        pass "auth_deep.hook_token_refresh" "custom claim present after refresh"
        ;;
      *) fail "auth_deep.hook_token_refresh" "expected 2xx refresh response, got HTTP $refresh_hook_status" ;;
    esac
  else
    fail "auth_deep.hook_token_refresh" "hook token response did not include refresh_token"
  fi
  local delete_status
  delete_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-hook-delete.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks/custom_access_token" \
    2>"$ARTIFACT_DIR/auth-deep-hook-delete.stderr" || printf '000')"
  case "$delete_status" in
    2??)
      auth_hook_created="false"
      pass "auth_deep.hook_delete" "HTTP $delete_status"
      ;;
    *) fail "auth_deep.hook_delete" "expected 2xx, got HTTP $delete_status" ;;
  esac
  wait_auth_ready "auth_deep.hook_delete_auth_ready"

  run_signed_custom_access_token_hook_check

  cat >"$ARTIFACT_DIR/auth-deep-before-user-created-payload.json" <<'JSON'
{"hook_type":"before_user_created","enabled":true,"edge_function":"__FUNCTION__","timeout_ms":5000,"retry_attempts":0}
JSON
  node - "$ARTIFACT_DIR/auth-deep-before-user-created-payload.json" "$auth_hook_function" <<'NODE'
const fs = require("fs");
const path = process.argv[2];
const functionName = process.argv[3];
const payload = JSON.parse(fs.readFileSync(path, "utf8"));
payload.edge_function = functionName;
fs.writeFileSync(path, JSON.stringify(payload));
NODE
  local before_hook_status
  before_hook_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-before-user-created-hook-create.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X POST "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-before-user-created-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-before-user-created-hook-create.stderr" || printf '000')"
  case "$before_hook_status" in
    2??)
      before_user_created_hook_created="true"
      pass "auth_deep.before_user_created_hook_config" "HTTP $before_hook_status"
      ;;
    *) fail "auth_deep.before_user_created_hook_config" "expected 2xx, got HTTP $before_hook_status" ;;
  esac
  wait_auth_ready "auth_deep.before_user_created_auth_ready"

  local blocked_email="compat-auth-blocked-${auth_suffix:-auth}@blocked.example.test"
  local blocked_password="CompatBlocked2026-${auth_suffix:-password}!"
  local blocked_status
  blocked_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-before-user-created-blocked-signup.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/signup" \
    --data "{\"email\":\"$blocked_email\",\"password\":\"$blocked_password\"}" \
    2>"$ARTIFACT_DIR/auth-deep-before-user-created-blocked-signup.stderr" || printf '000')"
  case "$blocked_status" in
    4??)
      if ! grep -qi "blocked by supadupa compat before-user-created hook" "$ARTIFACT_DIR/auth-deep-before-user-created-blocked-signup.body"; then
        fail "auth_deep.before_user_created_reject" "rejection response did not include hook message"
      fi
      pass "auth_deep.before_user_created_reject" "HTTP $blocked_status"
      ;;
    *) fail "auth_deep.before_user_created_reject" "expected 4xx signup rejection, got HTTP $blocked_status" ;;
  esac
  local blocked_count
  blocked_count="$(PGPASSWORD="$db_password" psql "$public_db_safe_url" \
    -v ON_ERROR_STOP=1 \
    -v email="$blocked_email" \
    -Atq 2>"$ARTIFACT_DIR/auth-deep-before-user-created-blocked-count.stderr" <<'SQL'
select count(*) from auth.users where email = :'email';
SQL
)"
  printf '%s\n' "$blocked_count" >"$ARTIFACT_DIR/auth-deep-before-user-created-blocked-count.out"
  if [[ "$blocked_count" != "0" ]]; then
    fail "auth_deep.before_user_created_no_user" "blocked signup inserted $blocked_count auth.users rows"
  fi
  pass "auth_deep.before_user_created_no_user" "blocked signup did not create auth.users row"

  local allowed_email="compat-auth-allowed-${auth_suffix:-auth}@example.test"
  local allowed_password="CompatAllowed2026-${auth_suffix:-password}!"
  local allowed_status
  allowed_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-before-user-created-allowed-signup.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/signup" \
    --data "{\"email\":\"$allowed_email\",\"password\":\"$allowed_password\"}" \
    2>"$ARTIFACT_DIR/auth-deep-before-user-created-allowed-signup.stderr" || printf '000')"
  case "$allowed_status" in
    2??)
      before_user_created_allowed_user_id="$(auth_response_user_id "$ARTIFACT_DIR/auth-deep-before-user-created-allowed-signup.body")"
      if [[ -z "$before_user_created_allowed_user_id" ]]; then
        fail "auth_deep.before_user_created_allow" "allowed signup response did not include user id"
      fi
      pass "auth_deep.before_user_created_allow" "HTTP $allowed_status"
      ;;
    *) fail "auth_deep.before_user_created_allow" "expected 2xx signup allow response, got HTTP $allowed_status" ;;
  esac

  local before_delete_status
  before_delete_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-before-user-created-hook-delete.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks/before_user_created" \
    2>"$ARTIFACT_DIR/auth-deep-before-user-created-hook-delete.stderr" || printf '000')"
  case "$before_delete_status" in
    2??)
      before_user_created_hook_created="false"
      pass "auth_deep.before_user_created_hook_delete" "HTTP $before_delete_status"
      ;;
    *) fail "auth_deep.before_user_created_hook_delete" "expected 2xx, got HTTP $before_delete_status" ;;
  esac
  wait_auth_ready "auth_deep.before_user_created_hook_delete_auth_ready"
  run_send_sms_hook_check
  supadupa_cli_authed functions delete --ref "$SUPADUPA_TEST_REF" --name "$auth_hook_function" \
    >"$ARTIFACT_DIR/auth-deep-hook-function-delete.out" 2>"$ARTIFACT_DIR/auth-deep-hook-function-delete.stderr" || true
  auth_hook_function=""
}

run_signed_custom_access_token_hook_check() {
  local secret_material="supadupa-auth-hook-${run_id}"
  local secret_base64
  local secret_value
  secret_base64="$(printf '%s' "$secret_material" | base64 | tr -d '\n')"
  secret_value="v1,whsec_$secret_base64"
  signed_hook_secret_kind="auth-hook-whsec-${auth_suffix:-auth}"
  signed_hook_secret_kind="$(printf '%s' "$signed_hook_secret_kind" | cut -c1-63)"

  cat >"$ARTIFACT_DIR/auth-deep-signed-hook-secret-payload.json" <<JSON
{"value":"$secret_value"}
JSON
  local secret_status
  secret_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-signed-hook-secret.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/secrets/$signed_hook_secret_kind" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-signed-hook-secret-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-signed-hook-secret.stderr" || printf '000')"
  case "$secret_status" in
    2??) pass "auth_deep.signed_hook_secret" "temporary Standard Webhooks secret handle created" ;;
    *) fail "auth_deep.signed_hook_secret" "expected 2xx, got HTTP $secret_status" ;;
  esac

  signed_hook_function="compat-signed-auth-hook-${auth_suffix:-hook}"
  local signed_hook_source="$ARTIFACT_DIR/auth-deep-signed-hook-function.ts"
  cat >"$signed_hook_source" <<'TS'
function base64ToBytes(value: string): Uint8Array {
  const binary = atob(value);
  const out = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) out[index] = binary.charCodeAt(index);
  return out;
}

function bytesToBase64(value: ArrayBuffer): string {
  let binary = "";
  const bytes = new Uint8Array(value);
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

function signatures(header: string): string[] {
  const values: string[] = [];
  for (const part of header.split(/\s+/)) {
    if (!part) continue;
    const [version, signature] = part.split(",", 2);
    if (version === "v1" && signature) values.push(signature);
  }
  return values;
}

Deno.serve(async (req: Request) => {
  const body = await req.text();
  const secret = Deno.env.get("AUTH_HOOK_SECRET_BASE64") ?? "";
  const webhookId = req.headers.get("webhook-id") ?? "";
  const webhookTimestamp = req.headers.get("webhook-timestamp") ?? "";
  const webhookSignature = req.headers.get("webhook-signature") ?? "";
  if (!secret || !webhookId || !webhookTimestamp || !webhookSignature) {
    return new Response(JSON.stringify({
      error: { http_code: 401, message: "missing Standard Webhooks signature headers" },
    }), { status: 401, headers: { "Content-Type": "application/json" } });
  }

  const signedContent = `${webhookId}.${webhookTimestamp}.${body}`;
  const key = await crypto.subtle.importKey(
    "raw",
    base64ToBytes(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const expected = bytesToBase64(await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(signedContent)));
  if (!signatures(webhookSignature).some((signature) => signature === expected)) {
    return new Response(JSON.stringify({
      error: { http_code: 401, message: "invalid Standard Webhooks signature" },
    }), { status: 401, headers: { "Content-Type": "application/json" } });
  }

  const payload = JSON.parse(body);
  const claims = payload?.claims ?? {};
  claims.supadupa_signed_hook = "verified";
  return new Response(JSON.stringify({ claims }), {
    headers: { "Content-Type": "application/json" },
  });
});
TS
  if supadupa_cli_authed functions deploy \
    --ref "$SUPADUPA_TEST_REF" \
    --name "$signed_hook_function" \
    --entrypoint index.ts \
    --source-file "$signed_hook_source" \
    --secret "AUTH_HOOK_SECRET_BASE64=$secret_base64" \
    --verify-jwt=false \
    >"$ARTIFACT_DIR/auth-deep-signed-hook-function-deploy.json" 2>"$ARTIFACT_DIR/auth-deep-signed-hook-function-deploy.stderr"; then
    pass "auth_deep.signed_hook_function_deploy" "$signed_hook_function"
  else
    fail "auth_deep.signed_hook_function_deploy" "deploy failed; see auth-deep-signed-hook-function-deploy.stderr"
  fi

  cat >"$ARTIFACT_DIR/auth-deep-signed-hook-payload.json" <<JSON
{"hook_type":"custom_access_token","enabled":true,"edge_function":"$signed_hook_function","secret_handle":"secret://projects/$SUPADUPA_TEST_REF/$signed_hook_secret_kind","timeout_ms":30000,"retry_attempts":0}
JSON
  local signed_hook_status
  signed_hook_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-signed-hook-create.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X POST "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-signed-hook-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-signed-hook-create.stderr" || printf '000')"
  case "$signed_hook_status" in
    2??)
      signed_hook_created="true"
      pass "auth_deep.signed_hook_config" "HTTP $signed_hook_status"
      ;;
    *) fail "auth_deep.signed_hook_config" "expected 2xx, got HTTP $signed_hook_status" ;;
  esac
  wait_auth_ready "auth_deep.signed_hook_auth_ready"

  local signed_email="compat-auth-signed-hook-${auth_suffix:-auth}@example.test"
  local signed_password="CompatSignedHook2026-${auth_suffix:-password}!"
  local signed_create_status
  signed_create_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-signed-hook-user-create.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X POST "$api_url/auth/v1/admin/users" \
    --data "{\"email\":\"$signed_email\",\"password\":\"$signed_password\",\"email_confirm\":true}" \
    2>"$ARTIFACT_DIR/auth-deep-signed-hook-user-create.stderr" || printf '000')"
  case "$signed_create_status" in
    2??)
      signed_hook_user_id="$(json_get_file "$ARTIFACT_DIR/auth-deep-signed-hook-user-create.body" id)"
      pass "auth_deep.signed_hook_user_create" "HTTP $signed_create_status"
      ;;
    *) fail "auth_deep.signed_hook_user_create" "expected 2xx, got HTTP $signed_create_status" ;;
  esac

  local signed_token_status
  signed_token_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-signed-hook-token.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/token?grant_type=password" \
    --data "{\"email\":\"$signed_email\",\"password\":\"$signed_password\"}" \
    2>"$ARTIFACT_DIR/auth-deep-signed-hook-token.stderr" || printf '000')"
  case "$signed_token_status" in
    2??)
      node - "$ARTIFACT_DIR/auth-deep-signed-hook-token.body" <<'NODE'
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const token = payload.access_token || "";
const claims = JSON.parse(Buffer.from(token.split(".")[1], "base64url").toString("utf8"));
if (claims.supadupa_signed_hook !== "verified") {
  throw new Error(`expected signed hook claim, got ${claims.supadupa_signed_hook}`);
}
NODE
      pass "auth_deep.signed_hook_standard_webhooks" "signature verified and custom claim present"
      ;;
    *) fail "auth_deep.signed_hook_standard_webhooks" "expected 2xx token response, got HTTP $signed_token_status" ;;
  esac

  local signed_delete_status
  signed_delete_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-signed-hook-delete.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks/custom_access_token" \
    2>"$ARTIFACT_DIR/auth-deep-signed-hook-delete.stderr" || printf '000')"
  case "$signed_delete_status" in
    2??)
      signed_hook_created="false"
      pass "auth_deep.signed_hook_delete" "HTTP $signed_delete_status"
      ;;
    *) fail "auth_deep.signed_hook_delete" "expected 2xx, got HTTP $signed_delete_status" ;;
  esac
  wait_auth_ready "auth_deep.signed_hook_delete_auth_ready"

  supadupa_cli_authed functions delete --ref "$SUPADUPA_TEST_REF" --name "$signed_hook_function" \
    >"$ARTIFACT_DIR/auth-deep-signed-hook-function-delete.out" 2>"$ARTIFACT_DIR/auth-deep-signed-hook-function-delete.stderr" || true
  signed_hook_function=""
  curl -sS -o "$ARTIFACT_DIR/auth-deep-signed-hook-secret-delete.body" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/secrets/$signed_hook_secret_kind" \
    2>"$ARTIFACT_DIR/auth-deep-signed-hook-secret-delete.stderr" || true
  signed_hook_secret_kind=""
}

run_sms_provider_runtime_config_check() {
  curl -sS -o "$sms_runtime_original_config_file" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/auth_providers" \
    2>"$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers-original.stderr"

  sms_runtime_test_otp_secret_kind="compat-sms-test-otp-${auth_suffix:-auth}"
  sms_runtime_test_otp_secret_kind="$(printf '%s' "$sms_runtime_test_otp_secret_kind" | cut -c1-63)"
  sms_runtime_provider_secret_kind="compat-sms-provider-${auth_suffix:-auth}"
  sms_runtime_provider_secret_kind="$(printf '%s' "$sms_runtime_provider_secret_kind" | cut -c1-63)"
  local sms_test_otp_value="+15555550123:654321"
  local sms_provider_secret_value="compat-sms-provider-secret-${auth_suffix:-auth}"
  local sms_test_otp_handle="secret://projects/$SUPADUPA_TEST_REF/$sms_runtime_test_otp_secret_kind"
  local sms_provider_secret_handle="secret://projects/$SUPADUPA_TEST_REF/$sms_runtime_provider_secret_kind"

  cat >"$ARTIFACT_DIR/auth-deep-sms-runtime-test-otp-secret-payload.json" <<JSON
{"value":"$sms_test_otp_value"}
JSON
  local test_otp_secret_status
  test_otp_secret_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-sms-runtime-test-otp-secret.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/secrets/$sms_runtime_test_otp_secret_kind" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-sms-runtime-test-otp-secret-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-sms-runtime-test-otp-secret.stderr" || printf '000')"
  case "$test_otp_secret_status" in
    2??) pass "auth_deep.sms_runtime_test_otp_secret" "temporary SMS test OTP secret handle created" ;;
    *) fail "auth_deep.sms_runtime_test_otp_secret" "expected 2xx, got HTTP $test_otp_secret_status" ;;
  esac

  cat >"$ARTIFACT_DIR/auth-deep-sms-runtime-provider-secret-payload.json" <<JSON
{"value":"$sms_provider_secret_value"}
JSON
  local provider_secret_status
  provider_secret_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-sms-runtime-provider-secret.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/secrets/$sms_runtime_provider_secret_kind" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-sms-runtime-provider-secret-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-sms-runtime-provider-secret.stderr" || printf '000')"
  case "$provider_secret_status" in
    2??) pass "auth_deep.sms_runtime_provider_secret" "temporary SMS provider secret handle created" ;;
    *) fail "auth_deep.sms_runtime_provider_secret" "expected 2xx, got HTTP $provider_secret_status" ;;
  esac

  local invalid_name invalid_payload invalid_status
  while IFS=$'\t' read -r invalid_name invalid_payload; do
    invalid_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-sms-runtime-invalid-$invalid_name.body" -w '%{http_code}' \
      -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
      -H "Content-Type: application/json" \
      -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/auth_providers" \
      --data "$invalid_payload" \
      2>"$ARTIFACT_DIR/auth-deep-sms-runtime-invalid-$invalid_name.stderr" || printf '000')"
    case "$invalid_status" in
      4??) pass "auth_deep.sms_runtime_invalid_$invalid_name" "rejected HTTP $invalid_status" ;;
      *) fail "auth_deep.sms_runtime_invalid_$invalid_name" "expected 4xx, got HTTP $invalid_status" ;;
    esac
  done <<'CASES'
otp_length	{"config":{"sms_otp_length":"3"}}
max_frequency	{"config":{"sms_max_frequency":"often"}}
test_otp_handle	{"config":{"sms_test_otp_handle":"raw-test-otp"}}
test_otp_valid_until	{"config":{"sms_test_otp_valid_until":"tomorrow"}}
CASES

  node - "$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers-payload.json" "$sms_test_otp_handle" "$sms_provider_secret_handle" <<'NODE'
const fs = require("fs");
const target = process.argv[2];
const smsTestOTPHandle = process.argv[3];
const providerSecretHandle = process.argv[4];
fs.writeFileSync(target, JSON.stringify({
  config: {
    phone_enabled: "true",
    sms_provider: "textlocal",
    sms_otp_exp: "90",
    sms_otp_length: "8",
    sms_max_frequency: "45s",
    sms_template: "Code: {{ .Code }}",
    sms_test_otp_handle: smsTestOTPHandle,
    sms_test_otp_valid_until: "2026-12-31T23:59:59Z",
    sms_twilio_account_sid: "twilio-account",
    sms_twilio_auth_token_handle: providerSecretHandle,
    sms_twilio_message_service_sid: "twilio-message-service",
    sms_messagebird_originator: "Supadupa",
    sms_messagebird_access_key_handle: providerSecretHandle,
    sms_textlocal_sender: "Supadupa",
    sms_textlocal_api_key_handle: providerSecretHandle,
    sms_vonage_from: "Supadupa",
    sms_vonage_api_key: "vonage-key",
    sms_vonage_api_secret_handle: providerSecretHandle,
  },
}));
NODE

  local auth_providers_status
  auth_providers_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/auth_providers" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers.stderr" || printf '000')"
  case "$auth_providers_status" in
    2??)
      sms_runtime_auth_providers_modified="true"
      pass "auth_deep.sms_runtime_config_update" "HTTP $auth_providers_status"
      ;;
    *) fail "auth_deep.sms_runtime_config_update" "expected 2xx, got HTTP $auth_providers_status" ;;
  esac

  node - "$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers.body" "$sms_test_otp_handle" "$sms_provider_secret_handle" "$sms_test_otp_value" "$sms_provider_secret_value" <<'NODE'
const fs = require("fs");
const body = fs.readFileSync(process.argv[2], "utf8");
const payload = JSON.parse(body);
const config = payload.config ?? payload;
const expected = {
  phone_enabled: "true",
  sms_provider: "textlocal",
  sms_otp_exp: "90",
  sms_otp_length: "8",
  sms_max_frequency: "45s",
  sms_template: "Code: {{ .Code }}",
  sms_test_otp_handle: process.argv[3],
  sms_test_otp_valid_until: "2026-12-31T23:59:59Z",
  sms_textlocal_api_key_handle: process.argv[4],
};
for (const [key, value] of Object.entries(expected)) {
  if (config[key] !== value) throw new Error(`${key}=${config[key]}`);
}
if (body.includes(process.argv[5]) || body.includes(process.argv[6])) {
  throw new Error("management response leaked SMS secret material");
}
NODE
  pass "auth_deep.sms_runtime_config_response" "SMS provider config persisted with secret handles only"
  wait_auth_ready "auth_deep.sms_runtime_auth_ready"

  local projects_root="${SUPADUPA_PROJECT_ROOT:-${SUPADUPA_RUNTIME_PROJECTS_DIR:-$REPO_ROOT/runtime/projects}}"
  local runtime_env="$projects_root/$SUPADUPA_TEST_REF/.env"
  if [[ -f "$runtime_env" ]]; then
    node - "$runtime_env" "$sms_test_otp_value" "$sms_test_otp_handle" "$sms_provider_secret_value" <<'NODE'
const fs = require("fs");
const envPath = process.argv[2];
const expectedTestOTP = process.argv[3];
const expectedTestOTPHandle = process.argv[4];
const expectedProviderSecret = process.argv[5];
const env = Object.fromEntries(fs.readFileSync(envPath, "utf8")
  .split(/\r?\n/)
  .filter((line) => line && !line.startsWith("#") && line.includes("="))
  .map((line) => {
    const index = line.indexOf("=");
    return [line.slice(0, index), line.slice(index + 1)];
  }));
const expected = {
  GOTRUE_SMS_PROVIDER: "textlocal",
  GOTRUE_SMS_OTP_EXP: "90",
  GOTRUE_SMS_OTP_LENGTH: "8",
  GOTRUE_SMS_MAX_FREQUENCY: "45s",
  GOTRUE_SMS_TEMPLATE: "Code: {{ .Code }}",
  GOTRUE_SMS_TEST_OTP: expectedTestOTP,
  SUPADUPA_SMS_TEST_OTP_HANDLE: expectedTestOTPHandle,
  GOTRUE_SMS_TEST_OTP_VALID_UNTIL: "2026-12-31T23:59:59Z",
  GOTRUE_SMS_TEXTLOCAL_API_KEY: expectedProviderSecret,
};
for (const [key, value] of Object.entries(expected)) {
  if (env[key] !== value) throw new Error(`${key}=${env[key]}`);
}
NODE
    pass "auth_deep.sms_runtime_env_render" "GoTrue SMS runtime env rendered"
  else
    skip "auth_deep.sms_runtime_env_render" "local runtime .env not present"
  fi

  node - "$sms_runtime_original_config_file" "$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers-restore-payload.json" <<'NODE'
const fs = require("fs");
const source = process.argv[2];
const target = process.argv[3];
const payload = JSON.parse(fs.readFileSync(source, "utf8"));
fs.writeFileSync(target, JSON.stringify({ config: payload.config ?? {} }));
NODE
  local restore_status
  restore_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers-restore.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/auth_providers" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers-restore-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-sms-runtime-auth-providers-restore.stderr" || printf '000')"
  case "$restore_status" in
    2??)
      sms_runtime_auth_providers_modified="false"
      pass "auth_deep.sms_runtime_config_restore" "original auth provider config restored"
      ;;
    *) fail "auth_deep.sms_runtime_config_restore" "expected 2xx, got HTTP $restore_status" ;;
  esac
  wait_auth_ready "auth_deep.sms_runtime_config_restore_ready"

  curl -sS -o "$ARTIFACT_DIR/auth-deep-sms-runtime-test-otp-secret-delete.body" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/secrets/$sms_runtime_test_otp_secret_kind" \
    2>"$ARTIFACT_DIR/auth-deep-sms-runtime-test-otp-secret-delete.stderr" || true
  sms_runtime_test_otp_secret_kind=""
  curl -sS -o "$ARTIFACT_DIR/auth-deep-sms-runtime-provider-secret-delete.body" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/secrets/$sms_runtime_provider_secret_kind" \
    2>"$ARTIFACT_DIR/auth-deep-sms-runtime-provider-secret-delete.stderr" || true
  sms_runtime_provider_secret_kind=""
}

run_real_sms_provider_delivery_check() {
  if ! compat_bool "${SUPADUPA_COMPAT_AUTH_REAL_SMS_VALIDATE:-false}"; then
    skip "auth_deep.real_sms_provider" "set SUPADUPA_COMPAT_AUTH_REAL_SMS_VALIDATE=true to run real SMS provider validation"
    return 0
  fi

  local provider
  provider="$(printf '%s' "${SUPADUPA_COMPAT_SMS_PROVIDER:-}" | tr '[:upper:]' '[:lower:]')"
  if [[ -z "$provider" ]]; then
    fail "auth_deep.real_sms_provider.env" "SUPADUPA_COMPAT_SMS_PROVIDER is required"
  fi
  case "$provider" in
    twilio|twilio_verify|messagebird|textlocal|vonage) ;;
    *) fail "auth_deep.real_sms_provider.env" "unsupported SUPADUPA_COMPAT_SMS_PROVIDER=$provider" ;;
  esac
  local phone="${SUPADUPA_COMPAT_SMS_PHONE:-}"
  if [[ -z "$phone" ]]; then
    fail "auth_deep.real_sms_provider.env" "SUPADUPA_COMPAT_SMS_PHONE is required"
  fi

  curl -sS -o "$real_sms_original_config_file" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/auth_providers" \
    2>"$ARTIFACT_DIR/auth-deep-real-sms-auth-providers-original.stderr"

  real_sms_secret_kind="compat-real-sms-${auth_suffix:-auth}"
  real_sms_secret_kind="$(printf '%s' "$real_sms_secret_kind" | cut -c1-63)"
  local secret_value=""
  local payload_args=("$provider" "$phone" "secret://projects/$SUPADUPA_TEST_REF/$real_sms_secret_kind")

  case "$provider" in
    twilio|twilio_verify)
      if [[ -z "${SUPADUPA_COMPAT_SMS_TWILIO_ACCOUNT_SID:-}" || -z "${SUPADUPA_COMPAT_SMS_TWILIO_AUTH_TOKEN:-}" ]]; then
        fail "auth_deep.real_sms_provider.env" "Twilio validation requires SUPADUPA_COMPAT_SMS_TWILIO_ACCOUNT_SID and SUPADUPA_COMPAT_SMS_TWILIO_AUTH_TOKEN"
      fi
      secret_value="$SUPADUPA_COMPAT_SMS_TWILIO_AUTH_TOKEN"
      payload_args+=("$SUPADUPA_COMPAT_SMS_TWILIO_ACCOUNT_SID" "${SUPADUPA_COMPAT_SMS_TWILIO_MESSAGE_SERVICE_SID:-}")
      ;;
    messagebird)
      if [[ -z "${SUPADUPA_COMPAT_SMS_MESSAGEBIRD_ORIGINATOR:-}" || -z "${SUPADUPA_COMPAT_SMS_MESSAGEBIRD_ACCESS_KEY:-}" ]]; then
        fail "auth_deep.real_sms_provider.env" "MessageBird validation requires SUPADUPA_COMPAT_SMS_MESSAGEBIRD_ORIGINATOR and SUPADUPA_COMPAT_SMS_MESSAGEBIRD_ACCESS_KEY"
      fi
      secret_value="$SUPADUPA_COMPAT_SMS_MESSAGEBIRD_ACCESS_KEY"
      payload_args+=("$SUPADUPA_COMPAT_SMS_MESSAGEBIRD_ORIGINATOR")
      ;;
    textlocal)
      if [[ -z "${SUPADUPA_COMPAT_SMS_TEXTLOCAL_SENDER:-}" || -z "${SUPADUPA_COMPAT_SMS_TEXTLOCAL_API_KEY:-}" ]]; then
        fail "auth_deep.real_sms_provider.env" "TextLocal validation requires SUPADUPA_COMPAT_SMS_TEXTLOCAL_SENDER and SUPADUPA_COMPAT_SMS_TEXTLOCAL_API_KEY"
      fi
      secret_value="$SUPADUPA_COMPAT_SMS_TEXTLOCAL_API_KEY"
      payload_args+=("$SUPADUPA_COMPAT_SMS_TEXTLOCAL_SENDER")
      ;;
    vonage)
      if [[ -z "${SUPADUPA_COMPAT_SMS_VONAGE_FROM:-}" || -z "${SUPADUPA_COMPAT_SMS_VONAGE_API_KEY:-}" || -z "${SUPADUPA_COMPAT_SMS_VONAGE_API_SECRET:-}" ]]; then
        fail "auth_deep.real_sms_provider.env" "Vonage validation requires SUPADUPA_COMPAT_SMS_VONAGE_FROM, SUPADUPA_COMPAT_SMS_VONAGE_API_KEY, and SUPADUPA_COMPAT_SMS_VONAGE_API_SECRET"
      fi
      secret_value="$SUPADUPA_COMPAT_SMS_VONAGE_API_SECRET"
      payload_args+=("$SUPADUPA_COMPAT_SMS_VONAGE_FROM" "$SUPADUPA_COMPAT_SMS_VONAGE_API_KEY")
      ;;
  esac

  REAL_SMS_SECRET_VALUE="$secret_value" node - "$ARTIFACT_DIR/auth-deep-real-sms-secret-payload.json" <<'NODE'
const fs = require("fs");
fs.writeFileSync(process.argv[2], JSON.stringify({ value: process.env.REAL_SMS_SECRET_VALUE ?? "" }));
NODE
  local secret_status
  secret_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-real-sms-secret.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/secrets/$real_sms_secret_kind" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-real-sms-secret-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-real-sms-secret.stderr" || printf '000')"
  case "$secret_status" in
    2??) pass "auth_deep.real_sms_secret" "temporary SMS provider secret handle created" ;;
    *) fail "auth_deep.real_sms_secret" "expected 2xx, got HTTP $secret_status" ;;
  esac

  node - "$ARTIFACT_DIR/auth-deep-real-sms-auth-providers-payload.json" "${payload_args[@]}" <<'NODE'
const fs = require("fs");
const [target, provider, phone, secretHandle, first, second] = process.argv.slice(2);
const config = {
  phone_enabled: "true",
  sms_provider: provider,
  sms_otp_exp: process.env.SUPADUPA_COMPAT_SMS_OTP_EXP || "300",
  sms_otp_length: process.env.SUPADUPA_COMPAT_SMS_OTP_LENGTH || "6",
  sms_max_frequency: process.env.SUPADUPA_COMPAT_SMS_MAX_FREQUENCY || "60s",
  sms_template: process.env.SUPADUPA_COMPAT_SMS_TEMPLATE || "Your code is {{ .Code }}",
};
switch (provider) {
  case "twilio":
  case "twilio_verify":
    config.sms_twilio_account_sid = first;
    config.sms_twilio_auth_token_handle = secretHandle;
    config.sms_twilio_message_service_sid = second || "";
    break;
  case "messagebird":
    config.sms_messagebird_originator = first;
    config.sms_messagebird_access_key_handle = secretHandle;
    break;
  case "textlocal":
    config.sms_textlocal_sender = first;
    config.sms_textlocal_api_key_handle = secretHandle;
    break;
  case "vonage":
    config.sms_vonage_from = first;
    config.sms_vonage_api_key = second;
    config.sms_vonage_api_secret_handle = secretHandle;
    break;
}
fs.writeFileSync(target, JSON.stringify({ config }));
NODE

  local config_status
  config_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-real-sms-auth-providers.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/auth_providers" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-real-sms-auth-providers-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-real-sms-auth-providers.stderr" || printf '000')"
  case "$config_status" in
    2??)
      real_sms_auth_providers_modified="true"
      pass "auth_deep.real_sms_config" "$provider configured through secret handle"
      ;;
    *) fail "auth_deep.real_sms_config" "expected 2xx, got HTTP $config_status" ;;
  esac
  wait_auth_ready "auth_deep.real_sms_auth_ready"

  local request_status
  request_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-real-sms-otp.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/otp" \
    --data "{\"phone\":\"$phone\",\"create_user\":true}" \
    2>"$ARTIFACT_DIR/auth-deep-real-sms-otp.stderr" || printf '000')"
  case "$request_status" in
    2??) pass "auth_deep.real_sms_otp_request" "OTP requested through $provider" ;;
    *) fail "auth_deep.real_sms_otp_request" "expected 2xx, got HTTP $request_status: $(cat "$ARTIFACT_DIR/auth-deep-real-sms-otp.body")" ;;
  esac

  if compat_bool "${SUPADUPA_COMPAT_SMS_REQUEST_ONLY:-false}"; then
    skip "auth_deep.real_sms_verify" "SUPADUPA_COMPAT_SMS_REQUEST_ONLY=true"
  else
    local otp=""
    if [[ -n "${SUPADUPA_COMPAT_SMS_OTP_COMMAND:-}" ]]; then
      otp="$(bash -c "$SUPADUPA_COMPAT_SMS_OTP_COMMAND" 2>"$ARTIFACT_DIR/auth-deep-real-sms-otp-command.stderr" | tr -cd '0-9' | tail -c "${SUPADUPA_COMPAT_SMS_OTP_LENGTH:-6}")"
    elif [[ -n "${SUPADUPA_COMPAT_SMS_OTP_FILE:-}" ]]; then
      local deadline=$((SECONDS + ${SUPADUPA_COMPAT_SMS_OTP_TIMEOUT_SECONDS:-180}))
      while (( SECONDS < deadline )); do
        if [[ -s "$SUPADUPA_COMPAT_SMS_OTP_FILE" ]]; then
          otp="$(tr -cd '0-9' <"$SUPADUPA_COMPAT_SMS_OTP_FILE" | tail -c "${SUPADUPA_COMPAT_SMS_OTP_LENGTH:-6}")"
          break
        fi
        sleep 3
      done
    else
      fail "auth_deep.real_sms_verify.env" "set SUPADUPA_COMPAT_SMS_OTP_COMMAND, SUPADUPA_COMPAT_SMS_OTP_FILE, or SUPADUPA_COMPAT_SMS_REQUEST_ONLY=true"
    fi
    if [[ -z "$otp" ]]; then
      fail "auth_deep.real_sms_verify.otp" "no OTP was produced"
    fi
    printf '%s\n' "$otp" >"$ARTIFACT_DIR/auth-deep-real-sms-otp.out"
    local verify_status
    verify_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-real-sms-verify.body" -w '%{http_code}' \
      -H "Content-Type: application/json" \
      -H "apikey: $anon_key" \
      -X POST "$api_url/auth/v1/verify" \
      --data "{\"type\":\"sms\",\"phone\":\"$phone\",\"token\":\"$otp\"}" \
      2>"$ARTIFACT_DIR/auth-deep-real-sms-verify.stderr" || printf '000')"
    case "$verify_status" in
      2??)
        if [[ "$(json_get_file "$ARTIFACT_DIR/auth-deep-real-sms-verify.body" user.phone)" != "$phone" ]]; then
          fail "auth_deep.real_sms_verify" "verified session phone mismatch"
        fi
        real_sms_user_id="$(auth_response_user_id "$ARTIFACT_DIR/auth-deep-real-sms-verify.body")"
        pass "auth_deep.real_sms_verify" "real provider OTP verified"
        ;;
      *) fail "auth_deep.real_sms_verify" "expected 2xx, got HTTP $verify_status: $(cat "$ARTIFACT_DIR/auth-deep-real-sms-verify.body")" ;;
    esac
  fi

  node - "$real_sms_original_config_file" "$ARTIFACT_DIR/auth-deep-real-sms-auth-providers-restore-payload.json" <<'NODE'
const fs = require("fs");
const source = process.argv[2];
const target = process.argv[3];
const payload = JSON.parse(fs.readFileSync(source, "utf8"));
fs.writeFileSync(target, JSON.stringify({ config: payload.config ?? {} }));
NODE
  local restore_status
  restore_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-real-sms-auth-providers-restore.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/auth_providers" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-real-sms-auth-providers-restore-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-real-sms-auth-providers-restore.stderr" || printf '000')"
  case "$restore_status" in
    2??)
      real_sms_auth_providers_modified="false"
      pass "auth_deep.real_sms_restore" "original auth provider config restored"
      ;;
    *) fail "auth_deep.real_sms_restore" "expected 2xx, got HTTP $restore_status" ;;
  esac
  wait_auth_ready "auth_deep.real_sms_restore_ready"

  curl -sS -o "$ARTIFACT_DIR/auth-deep-real-sms-secret-delete.body" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/secrets/$real_sms_secret_kind" \
    2>"$ARTIFACT_DIR/auth-deep-real-sms-secret-delete.stderr" || true
  real_sms_secret_kind=""
}

run_send_sms_hook_check() {
  local mode="${SUPADUPA_COMPAT_AUTH_HOOKS:-auto}"
  if [[ "$mode" == "false" || "$mode" == "0" || "$mode" == "off" ]]; then
    skip "auth_deep.send_sms_hook" "SUPADUPA_COMPAT_AUTH_HOOKS disabled"
    return 0
  fi

  curl -sS -o "$ARTIFACT_DIR/auth-deep-send-sms-hooks-list-before.json" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks" \
    2>"$ARTIFACT_DIR/auth-deep-send-sms-hooks-list-before.stderr"
  if node - "$ARTIFACT_DIR/auth-deep-send-sms-hooks-list-before.json" <<'NODE' >/dev/null 2>&1; then
const fs = require("fs");
const hooks = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (hooks.some((hook) => hook.hook_type === "send_sms")) process.exit(0);
process.exit(1);
NODE
    if [[ "$mode" == "true" || "$mode" == "1" || "$mode" == "on" ]]; then
      fail "auth_deep.send_sms_hook.existing" "send_sms hook already exists; refusing to overwrite"
    fi
    skip "auth_deep.send_sms_hook" "send_sms hook already exists; refusing to overwrite"
    return 0
  fi

  curl -sS -o "$auth_providers_original_config_file" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/auth_providers" \
    2>"$ARTIFACT_DIR/auth-deep-auth-providers-original.stderr"

  if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
    -v ON_ERROR_STOP=1 \
    -q >"$ARTIFACT_DIR/auth-deep-send-sms-events-setup.out" 2>"$ARTIFACT_DIR/auth-deep-send-sms-events-setup.stderr" <<'SQL'
drop table if exists public.compat_auth_sms_hook_events;
create table public.compat_auth_sms_hook_events (
  id bigserial primary key,
  run_id text not null,
  hook_name text not null,
  event_phone text not null,
  otp text,
  payload jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);
grant insert, select, delete on public.compat_auth_sms_hook_events to service_role;
grant usage, select on sequence public.compat_auth_sms_hook_events_id_seq to service_role;
notify pgrst, 'reload schema';
SQL
  then
    pass "auth_deep.send_sms_hook_events_setup" "event table created"
  else
    fail "auth_deep.send_sms_hook_events_setup" "event table setup failed; see auth-deep-send-sms-events-setup.stderr"
  fi

  local schema_deadline=$((SECONDS + ${SUPADUPA_COMPAT_REST_SCHEMA_TIMEOUT_SECONDS:-90}))
  while (( SECONDS < schema_deadline )); do
    local schema_status
    schema_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-send-sms-events-schema.body" -w '%{http_code}' \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      "$api_url/rest/v1/compat_auth_sms_hook_events?select=id&limit=1" \
      2>"$ARTIFACT_DIR/auth-deep-send-sms-events-schema.stderr" || printf '000')"
    if [[ "$schema_status" =~ ^2 ]]; then
      pass "auth_deep.send_sms_hook_events_schema" "PostgREST table visible"
      break
    fi
    sleep 3
  done
  if (( SECONDS >= schema_deadline )); then
    fail "auth_deep.send_sms_hook_events_schema" "event table was not visible through PostgREST before timeout"
  fi

  send_sms_hook_function="compat-send-sms-hook-${auth_suffix:-hook}"
  local send_sms_source="$ARTIFACT_DIR/auth-deep-send-sms-hook-function.ts"
  cat >"$send_sms_source" <<'TS'
Deno.serve(async (req: Request) => {
  const payload = await req.json().catch(() => ({}));
  const metadataName = String(payload?.metadata?.name ?? "");
  const smsData = payload?.sms ?? payload?.sms_data ?? {};
  const hookName = metadataName || "send-sms";
  const eventPhone = String(payload?.user?.phone ?? payload?.phone ?? "");
  const otp = String(smsData?.otp ?? smsData?.token ?? "");
  const supabaseUrl = Deno.env.get("SUPABASE_URL") ?? "";
  const serviceKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY") ?? Deno.env.get("SUPABASE_SERVICE_KEY") ?? "";
  const runId = Deno.env.get("SUPADUPA_COMPAT_RUN_ID") ?? "";

  if (!supabaseUrl || !serviceKey || !runId) {
    return new Response(JSON.stringify({
      error: { http_code: 500, message: "send-sms hook missing Supabase runtime env" },
    }), { status: 500, headers: { "Content-Type": "application/json" } });
  }
  if (!eventPhone || !otp) {
    return new Response(JSON.stringify({
      error: { http_code: 500, message: "send-sms hook missing phone or otp" },
    }), { status: 500, headers: { "Content-Type": "application/json" } });
  }

  const response = await fetch(`${supabaseUrl}/rest/v1/compat_auth_sms_hook_events`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "apikey": serviceKey,
      "Authorization": `Bearer ${serviceKey}`,
    },
    body: JSON.stringify({
      run_id: runId,
      hook_name: hookName,
      event_phone: eventPhone,
      otp,
      payload,
    }),
  });

  if (!response.ok) {
    return new Response(JSON.stringify({
      error: { http_code: 500, message: `event insert failed: ${response.status}` },
    }), { status: 500, headers: { "Content-Type": "application/json" } });
  }

  return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
});
TS
  if supadupa_cli_authed functions deploy \
    --ref "$SUPADUPA_TEST_REF" \
    --name "$send_sms_hook_function" \
    --entrypoint index.ts \
    --source-file "$send_sms_source" \
    --secret "SUPADUPA_COMPAT_RUN_ID=$run_id" \
    --verify-jwt=false \
    >"$ARTIFACT_DIR/auth-deep-send-sms-hook-function-deploy.json" 2>"$ARTIFACT_DIR/auth-deep-send-sms-hook-function-deploy.stderr"; then
    pass "auth_deep.send_sms_hook_function_deploy" "$send_sms_hook_function"
  else
    fail "auth_deep.send_sms_hook_function_deploy" "deploy failed; see auth-deep-send-sms-hook-function-deploy.stderr"
  fi

  cat >"$ARTIFACT_DIR/auth-deep-send-sms-auth-providers-payload.json" <<'JSON'
{"config":{"phone_enabled":"true","sms_provider":""}}
JSON
  local auth_providers_status
  auth_providers_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-send-sms-auth-providers.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/auth_providers" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-send-sms-auth-providers-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-send-sms-auth-providers.stderr" || printf '000')"
  case "$auth_providers_status" in
    2??)
      send_sms_auth_providers_modified="true"
      pass "auth_deep.send_sms_phone_auth_config" "phone auth enabled for hook check"
      ;;
    *) fail "auth_deep.send_sms_phone_auth_config" "expected 2xx, got HTTP $auth_providers_status" ;;
  esac
  wait_auth_ready "auth_deep.send_sms_phone_auth_ready"

  cat >"$ARTIFACT_DIR/auth-deep-send-sms-hook-payload.json" <<'JSON'
{"hook_type":"send_sms","enabled":true,"edge_function":"__FUNCTION__","timeout_ms":5000,"retry_attempts":0}
JSON
  node - "$ARTIFACT_DIR/auth-deep-send-sms-hook-payload.json" "$send_sms_hook_function" <<'NODE'
const fs = require("fs");
const path = process.argv[2];
const functionName = process.argv[3];
const payload = JSON.parse(fs.readFileSync(path, "utf8"));
payload.edge_function = functionName;
fs.writeFileSync(path, JSON.stringify(payload));
NODE
  local send_sms_hook_status
  send_sms_hook_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-send-sms-hook-create.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X POST "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-send-sms-hook-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-send-sms-hook-create.stderr" || printf '000')"
  case "$send_sms_hook_status" in
    2??)
      send_sms_hook_created="true"
      pass "auth_deep.send_sms_hook_config" "HTTP $send_sms_hook_status"
      ;;
    *) fail "auth_deep.send_sms_hook_config" "expected 2xx, got HTTP $send_sms_hook_status" ;;
  esac
  wait_auth_ready "auth_deep.send_sms_hook_auth_ready"

  local send_phone="+1555$(printf '%010d' "$((RANDOM * RANDOM % 10000000000))")"
  local sms_status
  sms_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-send-sms-otp.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/otp" \
    --data "{\"phone\":\"$send_phone\",\"create_user\":true}" \
    2>"$ARTIFACT_DIR/auth-deep-send-sms-otp.stderr" || printf '000')"
  case "$sms_status" in
    2??) pass "auth_deep.send_sms_hook_otp_request" "HTTP $sms_status" ;;
    *) fail "auth_deep.send_sms_hook_otp_request" "expected 2xx, got HTTP $sms_status: $(cat "$ARTIFACT_DIR/auth-deep-send-sms-otp.body")" ;;
  esac

  local sms_deadline=$((SECONDS + 60))
  local sms_event=""
  local sms_otp=""
  local sms_verified_phone=""
  while (( SECONDS < sms_deadline )); do
    sms_event="$(PGPASSWORD="$db_password" psql "$public_db_safe_url" \
      -v ON_ERROR_STOP=1 \
      -v run_id="$run_id" \
      -Atq 2>"$ARTIFACT_DIR/auth-deep-send-sms-event-query.stderr" <<'SQL' || true
select event_phone || E'\t' || otp
from public.compat_auth_sms_hook_events
where run_id = :'run_id'
order by id desc
limit 1;
SQL
)"
    if [[ -n "$sms_event" ]]; then
      break
    fi
    sleep 2
  done
  sms_verified_phone="${sms_event%%$'\t'*}"
  sms_otp="${sms_event#*$'\t'}"
  printf '%s\n' "$sms_otp" >"$ARTIFACT_DIR/auth-deep-send-sms-otp.out"
  printf '%s\n' "$sms_verified_phone" >"$ARTIFACT_DIR/auth-deep-send-sms-phone.out"
  if [[ -z "$sms_otp" || -z "$sms_verified_phone" || "$sms_event" != *$'\t'* ]]; then
    fail "auth_deep.send_sms_hook_event" "send_sms hook did not record an event"
  fi
  pass "auth_deep.send_sms_hook_event" "hook recorded phone OTP event for $sms_verified_phone"

  local sms_verify_status
  sms_verify_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-send-sms-verify.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/verify" \
    --data "{\"type\":\"sms\",\"phone\":\"$sms_verified_phone\",\"token\":\"$sms_otp\"}" \
    2>"$ARTIFACT_DIR/auth-deep-send-sms-verify.stderr" || printf '000')"
  case "$sms_verify_status" in
    2??)
      if [[ "$(json_get_file "$ARTIFACT_DIR/auth-deep-send-sms-verify.body" user.phone)" != "$sms_verified_phone" ]]; then
        fail "auth_deep.send_sms_hook_verify" "verified session phone mismatch"
      fi
      send_sms_hook_user_id="$(json_get_file "$ARTIFACT_DIR/auth-deep-send-sms-verify.body" user.id)"
      pass "auth_deep.send_sms_hook_verify" "hook-delivered phone OTP verified"
      ;;
    *) fail "auth_deep.send_sms_hook_verify" "expected 2xx, got HTTP $sms_verify_status: $(cat "$ARTIFACT_DIR/auth-deep-send-sms-verify.body")" ;;
  esac

  local send_sms_delete_status
  send_sms_delete_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-send-sms-hook-delete.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks/send_sms" \
    2>"$ARTIFACT_DIR/auth-deep-send-sms-hook-delete.stderr" || printf '000')"
  case "$send_sms_delete_status" in
    2??|204)
      send_sms_hook_created="false"
      pass "auth_deep.send_sms_hook_delete" "HTTP $send_sms_delete_status"
      ;;
    *) fail "auth_deep.send_sms_hook_delete" "expected 2xx/204, got HTTP $send_sms_delete_status" ;;
  esac
  wait_auth_ready "auth_deep.send_sms_hook_delete_auth_ready"
  supadupa_cli_authed functions delete --ref "$SUPADUPA_TEST_REF" --name "$send_sms_hook_function" \
    >"$ARTIFACT_DIR/auth-deep-send-sms-hook-function-delete.out" 2>"$ARTIFACT_DIR/auth-deep-send-sms-hook-function-delete.stderr" || true
  send_sms_hook_function=""

  node - "$auth_providers_original_config_file" "$ARTIFACT_DIR/auth-deep-auth-providers-restore-payload.json" <<'NODE'
const fs = require("fs");
const source = process.argv[2];
const target = process.argv[3];
const payload = JSON.parse(fs.readFileSync(source, "utf8"));
fs.writeFileSync(target, JSON.stringify({ config: payload.config ?? {} }));
NODE
  local restore_status
  restore_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-auth-providers-restore.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/auth_providers" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-auth-providers-restore-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-auth-providers-restore.stderr" || printf '000')"
  case "$restore_status" in
    2??)
      send_sms_auth_providers_modified="false"
      pass "auth_deep.send_sms_auth_providers_restore" "original auth provider config restored"
      ;;
    *) fail "auth_deep.send_sms_auth_providers_restore" "expected 2xx, got HTTP $restore_status" ;;
  esac
  wait_auth_ready "auth_deep.send_sms_auth_providers_restore_ready"
}

run_send_email_hook_check() {
  local mode="${SUPADUPA_COMPAT_AUTH_HOOKS:-auto}"
  if [[ "$mode" == "false" || "$mode" == "0" || "$mode" == "off" ]]; then
    skip "auth_deep.send_email_hook" "SUPADUPA_COMPAT_AUTH_HOOKS disabled"
    return 0
  fi

  curl -sS -o "$ARTIFACT_DIR/auth-deep-send-email-hooks-list-before.json" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks" \
    2>"$ARTIFACT_DIR/auth-deep-send-email-hooks-list-before.stderr"
  if node - "$ARTIFACT_DIR/auth-deep-send-email-hooks-list-before.json" <<'NODE' >/dev/null 2>&1; then
const fs = require("fs");
const hooks = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (hooks.some((hook) => hook.hook_type === "send_email")) process.exit(0);
process.exit(1);
NODE
    if [[ "$mode" == "true" || "$mode" == "1" || "$mode" == "on" ]]; then
      fail "auth_deep.send_email_hook.existing" "send_email hook already exists; refusing to overwrite"
    fi
    skip "auth_deep.send_email_hook" "send_email hook already exists; refusing to overwrite"
    return 0
  fi

  if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
    -v ON_ERROR_STOP=1 \
    -q >"$ARTIFACT_DIR/auth-deep-send-email-events-setup.out" 2>"$ARTIFACT_DIR/auth-deep-send-email-events-setup.stderr" <<'SQL'
drop table if exists public.compat_auth_hook_events;
create table public.compat_auth_hook_events (
  id bigserial primary key,
  run_id text not null,
  hook_name text not null,
  event_email text not null,
  action_type text,
  token text,
  token_hash text,
  payload jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);
grant insert, select, delete on public.compat_auth_hook_events to service_role;
grant usage, select on sequence public.compat_auth_hook_events_id_seq to service_role;
notify pgrst, 'reload schema';
SQL
  then
    pass "auth_deep.send_email_hook_events_setup" "event table created"
  else
    fail "auth_deep.send_email_hook_events_setup" "event table setup failed; see auth-deep-send-email-events-setup.stderr"
  fi

  local schema_deadline=$((SECONDS + ${SUPADUPA_COMPAT_REST_SCHEMA_TIMEOUT_SECONDS:-90}))
  while (( SECONDS < schema_deadline )); do
    local schema_status
    schema_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-send-email-events-schema.body" -w '%{http_code}' \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      "$api_url/rest/v1/compat_auth_hook_events?select=id&limit=1" \
      2>"$ARTIFACT_DIR/auth-deep-send-email-events-schema.stderr" || printf '000')"
    if [[ "$schema_status" =~ ^2 ]]; then
      pass "auth_deep.send_email_hook_events_schema" "PostgREST table visible"
      break
    fi
    sleep 3
  done
  if (( SECONDS >= schema_deadline )); then
    fail "auth_deep.send_email_hook_events_schema" "event table was not visible through PostgREST before timeout"
  fi

  send_email_hook_function="compat-send-email-hook-${auth_suffix:-hook}"
  local send_hook_source="$ARTIFACT_DIR/auth-deep-send-email-hook-function.ts"
  cat >"$send_hook_source" <<'TS'
Deno.serve(async (req: Request) => {
  const payload = await req.json().catch(() => ({}));
  const metadataName = String(payload?.metadata?.name ?? "");
  const emailData = payload?.email_data ?? payload?.email ?? {};
  const hookName = metadataName || "send-email";
  const eventEmail = String(payload?.user?.email ?? payload?.email ?? "");
  const actionType = String(emailData?.email_action_type ?? emailData?.action_type ?? "");
  const token = String(emailData?.token ?? "");
  const tokenHash = String(emailData?.token_hash ?? "");
  const supabaseUrl = Deno.env.get("SUPABASE_URL") ?? "";
  const serviceKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY") ?? Deno.env.get("SUPABASE_SERVICE_KEY") ?? "";
  const runId = Deno.env.get("SUPADUPA_COMPAT_RUN_ID") ?? "";

  if (!supabaseUrl || !serviceKey || !runId) {
    return new Response(JSON.stringify({
      error: { http_code: 500, message: "send-email hook missing Supabase runtime env" },
    }), { status: 500, headers: { "Content-Type": "application/json" } });
  }

  const response = await fetch(`${supabaseUrl}/rest/v1/compat_auth_hook_events`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "apikey": serviceKey,
      "Authorization": `Bearer ${serviceKey}`,
    },
    body: JSON.stringify({
      run_id: runId,
      hook_name: hookName,
      event_email: eventEmail,
      action_type: actionType,
      token,
      token_hash: tokenHash,
      payload,
    }),
  });

  if (!response.ok) {
    return new Response(JSON.stringify({
      error: { http_code: 500, message: `event insert failed: ${response.status}` },
    }), { status: 500, headers: { "Content-Type": "application/json" } });
  }

  return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
});
TS
  if supadupa_cli_authed functions deploy \
    --ref "$SUPADUPA_TEST_REF" \
    --name "$send_email_hook_function" \
    --entrypoint index.ts \
    --source-file "$send_hook_source" \
    --secret "SUPADUPA_COMPAT_RUN_ID=$run_id" \
    --verify-jwt=false \
    >"$ARTIFACT_DIR/auth-deep-send-email-hook-function-deploy.json" 2>"$ARTIFACT_DIR/auth-deep-send-email-hook-function-deploy.stderr"; then
    pass "auth_deep.send_email_hook_function_deploy" "$send_email_hook_function"
  else
    fail "auth_deep.send_email_hook_function_deploy" "deploy failed; see auth-deep-send-email-hook-function-deploy.stderr"
  fi

  cat >"$ARTIFACT_DIR/auth-deep-send-email-hook-payload.json" <<'JSON'
{"hook_type":"send_email","enabled":true,"edge_function":"__FUNCTION__","timeout_ms":5000,"retry_attempts":0}
JSON
  node - "$ARTIFACT_DIR/auth-deep-send-email-hook-payload.json" "$send_email_hook_function" <<'NODE'
const fs = require("fs");
const path = process.argv[2];
const functionName = process.argv[3];
const payload = JSON.parse(fs.readFileSync(path, "utf8"));
payload.edge_function = functionName;
fs.writeFileSync(path, JSON.stringify(payload));
NODE
  local send_hook_status
  send_hook_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-send-email-hook-create.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X POST "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-send-email-hook-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-send-email-hook-create.stderr" || printf '000')"
  case "$send_hook_status" in
    2??)
      send_email_hook_created="true"
      pass "auth_deep.send_email_hook_config" "HTTP $send_hook_status"
      ;;
    *) fail "auth_deep.send_email_hook_config" "expected 2xx, got HTTP $send_hook_status" ;;
  esac
  wait_auth_ready "auth_deep.send_email_hook_auth_ready"

  local send_email="compat-auth-send-email-${auth_suffix:-auth}@example.test"
  local send_status
  send_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-send-email-otp.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/otp" \
    --data "{\"email\":\"$send_email\",\"create_user\":true}" \
    2>"$ARTIFACT_DIR/auth-deep-send-email-otp.stderr" || printf '000')"
  case "$send_status" in
    2??) pass "auth_deep.send_email_hook_otp_request" "HTTP $send_status" ;;
    *) fail "auth_deep.send_email_hook_otp_request" "expected 2xx, got HTTP $send_status" ;;
  esac

  local event_deadline=$((SECONDS + 60))
  local send_token=""
  while (( SECONDS < event_deadline )); do
    send_token="$(PGPASSWORD="$db_password" psql "$public_db_safe_url" \
      -v ON_ERROR_STOP=1 \
      -v run_id="$run_id" \
      -v email="$send_email" \
      -Atq 2>"$ARTIFACT_DIR/auth-deep-send-email-event-query.stderr" <<'SQL' || true
select token
from public.compat_auth_hook_events
where run_id = :'run_id'
  and event_email = :'email'
  and coalesce(action_type, '') in ('signup', 'magiclink', 'email/signup', '')
order by id desc
limit 1;
SQL
)"
    if [[ -n "$send_token" ]]; then
      break
    fi
    sleep 2
  done
  printf '%s\n' "$send_token" >"$ARTIFACT_DIR/auth-deep-send-email-token.out"
  if [[ -z "$send_token" ]]; then
    fail "auth_deep.send_email_hook_event" "send_email hook did not record an event"
  fi
  pass "auth_deep.send_email_hook_event" "hook recorded signup OTP event"

  if mailpit_message_for "$smtp_mailpit_container" "$send_email" "Confirm Your Email" "$ARTIFACT_DIR/auth-deep-send-email-mailpit-message.json"; then
    fail "auth_deep.send_email_hook_replaces_smtp" "SMTP captured an email even though send_email hook was enabled"
  fi
  pass "auth_deep.send_email_hook_replaces_smtp" "SMTP fallback was not used"

  local send_verify_status
  send_verify_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-send-email-verify.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/verify" \
    --data "{\"type\":\"signup\",\"email\":\"$send_email\",\"token\":\"$send_token\"}" \
    2>"$ARTIFACT_DIR/auth-deep-send-email-verify.stderr" || printf '000')"
  case "$send_verify_status" in
    2??)
      if [[ "$(json_get_file "$ARTIFACT_DIR/auth-deep-send-email-verify.body" user.email)" != "$send_email" ]]; then
        fail "auth_deep.send_email_hook_verify" "verified session email mismatch"
      fi
      send_email_hook_user_id="$(json_get_file "$ARTIFACT_DIR/auth-deep-send-email-verify.body" user.id)"
      pass "auth_deep.send_email_hook_verify" "hook-delivered OTP verified"
      ;;
    *) fail "auth_deep.send_email_hook_verify" "expected 2xx, got HTTP $send_verify_status" ;;
  esac

  local send_delete_status
  send_delete_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-send-email-hook-delete.body" -w '%{http_code}' \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/auth/hooks/send_email" \
    2>"$ARTIFACT_DIR/auth-deep-send-email-hook-delete.stderr" || printf '000')"
  case "$send_delete_status" in
    2??|204)
      send_email_hook_created="false"
      pass "auth_deep.send_email_hook_delete" "HTTP $send_delete_status"
      ;;
    *) fail "auth_deep.send_email_hook_delete" "expected 2xx/204, got HTTP $send_delete_status" ;;
  esac
  wait_auth_ready "auth_deep.send_email_hook_delete_auth_ready"
  supadupa_cli_authed functions delete --ref "$SUPADUPA_TEST_REF" --name "$send_email_hook_function" \
    >"$ARTIFACT_DIR/auth-deep-send-email-hook-function-delete.out" 2>"$ARTIFACT_DIR/auth-deep-send-email-hook-function-delete.stderr" || true
  send_email_hook_function=""
}

run_auth_smtp_checks() {
  local mode="${SUPADUPA_COMPAT_AUTH_SMTP:-auto}"
  if [[ "$mode" == "false" || "$mode" == "0" || "$mode" == "off" ]]; then
    skip "auth_deep.smtp" "SUPADUPA_COMPAT_AUTH_SMTP disabled"
    return 0
  fi
  if ! command -v docker >/dev/null 2>&1; then
    if [[ "$mode" == "true" || "$mode" == "1" || "$mode" == "on" ]]; then
      fail "auth_deep.smtp.docker" "docker is required when SUPADUPA_COMPAT_AUTH_SMTP is enabled"
    fi
    skip "auth_deep.smtp" "docker unavailable; cannot attach local SMTP capture"
    return 0
  fi
  local auth_container="${SUPADUPA_TEST_REF}-auth-1"
  if ! docker inspect "$auth_container" >/dev/null 2>&1; then
    if [[ "$mode" == "true" || "$mode" == "1" || "$mode" == "on" ]]; then
      fail "auth_deep.smtp.auth_container" "$auth_container not found"
    fi
    skip "auth_deep.smtp" "$auth_container not visible; remote-only runner cannot attach SMTP capture"
    return 0
  fi
  local network
  network="$(docker inspect "$auth_container" --format '{{range $name,$net := .NetworkSettings.Networks}}{{println $name}}{{end}}' | head -1)"
  if [[ -z "$network" ]]; then
    fail "auth_deep.smtp.network" "could not determine auth container network"
  fi

  smtp_mailpit_container="supadupa-compat-mailpit-${SUPADUPA_TEST_REF}-${suffix:-smtp}"
  smtp_mailpit_container="$(printf '%s' "$smtp_mailpit_container" | tr -cd 'a-zA-Z0-9_.-' | cut -c1-63)"
  docker rm -f "$smtp_mailpit_container" >/dev/null 2>&1 || true
  if docker run -d \
    --name "$smtp_mailpit_container" \
    --network "$network" \
    --network-alias compat-mailpit \
    "${SUPADUPA_COMPAT_MAILPIT_IMAGE:-axllent/mailpit:latest}" \
    >"$ARTIFACT_DIR/auth-deep-mailpit-container.out" 2>"$ARTIFACT_DIR/auth-deep-mailpit-container.stderr"; then
    pass "auth_deep.smtp_capture_start" "$smtp_mailpit_container on $network"
  else
    fail "auth_deep.smtp_capture_start" "failed to start Mailpit; see auth-deep-mailpit-container.stderr"
  fi

  curl -sS -o "$smtp_original_config_file" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/smtp" \
    2>"$ARTIFACT_DIR/auth-deep-smtp-original.stderr"
  curl -sS -o "$email_templates_original_config_file" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/email_templates" \
    2>"$ARTIFACT_DIR/auth-deep-email-templates-original.stderr"

  cat >"$ARTIFACT_DIR/auth-deep-smtp-payload.json" <<JSON
{"config":{"enabled":"true","host":"compat-mailpit","port":"1025","sender_name":"Supadupa Compat","sender_email":"auth@supadupa.test","username":"","password_handle":"","tls_mode":"none"}}
JSON
  if curl -sS -o "$ARTIFACT_DIR/auth-deep-smtp-config.body" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/smtp" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-smtp-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-smtp-config.stderr"; then
    pass "auth_deep.smtp_config" "project SMTP routed to capture container"
  else
    fail "auth_deep.smtp_config" "failed to configure SMTP; see auth-deep-smtp-config.stderr"
  fi
  cat >"$ARTIFACT_DIR/auth-deep-email-templates-payload.json" <<'JSON'
{"config":{"magic_link_subject":"Your Magic Link","magic_link_body":"Magic link: {{ .ConfirmationURL }}\ncode: {{ .Token }}"}}
JSON
  if curl -sS -o "$ARTIFACT_DIR/auth-deep-email-templates-config.body" \
    -H "Authorization: Bearer $(read_secret_file "$ARTIFACT_DIR/token")" \
    -H "Content-Type: application/json" \
    -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/config/email_templates" \
    --data-binary "@$ARTIFACT_DIR/auth-deep-email-templates-payload.json" \
    2>"$ARTIFACT_DIR/auth-deep-email-templates-config.stderr"; then
    pass "auth_deep.email_templates_config" "magic-link template configured for capture"
  else
    fail "auth_deep.email_templates_config" "failed to configure email templates; see auth-deep-email-templates-config.stderr"
  fi
  local health_deadline=$((SECONDS + 90))
  local health_status
  while (( SECONDS < health_deadline )); do
    health_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-smtp-health.body" -w '%{http_code}' "$api_url/auth/v1/health" 2>"$ARTIFACT_DIR/auth-deep-smtp-health.stderr" || printf '000')"
    if [[ "$health_status" =~ ^2 ]]; then
      pass "auth_deep.smtp_auth_ready" "HTTP $health_status"
      break
    fi
    sleep 3
  done
  if (( SECONDS >= health_deadline )); then
    fail "auth_deep.smtp_auth_ready" "auth health did not recover after SMTP config"
  fi

  local recovery_email="compat-auth-recovery-${auth_suffix:-auth}@example.test"
  local recovery_password="CompatRecovery2026-${auth_suffix:-password}!"
  local magic_email="compat-auth-magic-${auth_suffix:-auth}@example.test"
  local magic_password="CompatMagic2026-${auth_suffix:-password}!"
  local signup_email="compat-auth-signup-${auth_suffix:-auth}@example.test"
  local create_body="$ARTIFACT_DIR/auth-deep-smtp-recovery-create.body"
  local create_status
  create_status="$(curl -sS -o "$create_body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X POST "$api_url/auth/v1/admin/users" \
    --data "{\"email\":\"$recovery_email\",\"password\":\"$recovery_password\",\"email_confirm\":true}" \
    2>"$ARTIFACT_DIR/auth-deep-smtp-recovery-create.stderr" || printf '000')"
  case "$create_status" in
    2??)
      smtp_recovery_user_id="$(auth_response_user_id "$create_body")"
      pass "auth_deep.smtp_recovery_user" "HTTP $create_status"
      ;;
    *) fail "auth_deep.smtp_recovery_user" "expected 2xx, got HTTP $create_status" ;;
  esac

  local recover_status
  recover_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-smtp-recover.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/recover" \
    --data "{\"email\":\"$recovery_email\"}" \
    2>"$ARTIFACT_DIR/auth-deep-smtp-recover.stderr" || printf '000')"
  case "$recover_status" in
    2??) pass "auth_deep.smtp_recover_request" "HTTP $recover_status" ;;
    *) fail "auth_deep.smtp_recover_request" "expected 2xx, got HTTP $recover_status" ;;
  esac
  if mailpit_message_for "$smtp_mailpit_container" "$recovery_email" "Reset Your Password" "$ARTIFACT_DIR/auth-deep-smtp-recovery-message.json"; then
    validate_mail_links_project_host "$ARTIFACT_DIR/auth-deep-smtp-recovery-message.json" "$api_host" "recovery"
    pass "auth_deep.smtp_recovery_email" "captured reset email on project host"
  else
    fail "auth_deep.smtp_recovery_email" "reset email was not captured"
  fi
  local recovery_code
  recovery_code="$(mail_otp_code "$ARTIFACT_DIR/auth-deep-smtp-recovery-message.json")"
  local recovery_verify_status
  recovery_verify_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-smtp-recovery-verify.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/verify" \
    --data "{\"type\":\"recovery\",\"email\":\"$recovery_email\",\"token\":\"$recovery_code\"}" \
    2>"$ARTIFACT_DIR/auth-deep-smtp-recovery-verify.stderr" || printf '000')"
  case "$recovery_verify_status" in
    2??)
      if [[ "$(json_get_file "$ARTIFACT_DIR/auth-deep-smtp-recovery-verify.body" user.email)" != "$recovery_email" ]]; then
        fail "auth_deep.smtp_recovery_verify" "verified session email mismatch"
      fi
      pass "auth_deep.smtp_recovery_verify" "HTTP $recovery_verify_status"
      ;;
    *) fail "auth_deep.smtp_recovery_verify" "expected 2xx, got HTTP $recovery_verify_status" ;;
  esac

  local magic_create_body="$ARTIFACT_DIR/auth-deep-smtp-magic-create.body"
  local magic_create_status
  magic_create_status="$(curl -sS -o "$magic_create_body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X POST "$api_url/auth/v1/admin/users" \
    --data "{\"email\":\"$magic_email\",\"password\":\"$magic_password\",\"email_confirm\":true}" \
    2>"$ARTIFACT_DIR/auth-deep-smtp-magic-create.stderr" || printf '000')"
  case "$magic_create_status" in
    2??)
      smtp_magic_user_id="$(auth_response_user_id "$magic_create_body")"
      pass "auth_deep.smtp_magic_user" "HTTP $magic_create_status"
      ;;
    *) fail "auth_deep.smtp_magic_user" "expected 2xx, got HTTP $magic_create_status" ;;
  esac

  local magic_status
  magic_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-smtp-magic-otp.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/otp" \
    --data "{\"email\":\"$magic_email\",\"create_user\":false}" \
    2>"$ARTIFACT_DIR/auth-deep-smtp-magic-otp.stderr" || printf '000')"
  case "$magic_status" in
    2??) pass "auth_deep.smtp_magic_otp_request" "HTTP $magic_status" ;;
    *) fail "auth_deep.smtp_magic_otp_request" "expected 2xx, got HTTP $magic_status" ;;
  esac
  if mailpit_message_for "$smtp_mailpit_container" "$magic_email" "Your Magic Link" "$ARTIFACT_DIR/auth-deep-smtp-magic-message.json"; then
    validate_mail_links_project_host "$ARTIFACT_DIR/auth-deep-smtp-magic-message.json" "$api_host" "magiclink"
    pass "auth_deep.smtp_magic_email" "captured magic link email on project host"
  else
    fail "auth_deep.smtp_magic_email" "magic link email was not captured"
  fi
  local magic_code
  magic_code="$(mail_otp_code "$ARTIFACT_DIR/auth-deep-smtp-magic-message.json")"
  local magic_verify_status
  magic_verify_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-smtp-magic-verify.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/verify" \
    --data "{\"type\":\"magiclink\",\"email\":\"$magic_email\",\"token\":\"$magic_code\"}" \
    2>"$ARTIFACT_DIR/auth-deep-smtp-magic-verify.stderr" || printf '000')"
  case "$magic_verify_status" in
    2??)
      if [[ "$(json_get_file "$ARTIFACT_DIR/auth-deep-smtp-magic-verify.body" user.email)" != "$magic_email" ]]; then
        fail "auth_deep.smtp_magic_verify" "verified session email mismatch"
      fi
      pass "auth_deep.smtp_magic_verify" "HTTP $magic_verify_status"
      ;;
    *) fail "auth_deep.smtp_magic_verify" "expected 2xx, got HTTP $magic_verify_status" ;;
  esac

  local otp_status
  otp_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-smtp-signup-otp.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/otp" \
    --data "{\"email\":\"$signup_email\",\"create_user\":true}" \
    2>"$ARTIFACT_DIR/auth-deep-smtp-signup-otp.stderr" || printf '000')"
  case "$otp_status" in
    2??) pass "auth_deep.smtp_signup_otp_request" "HTTP $otp_status" ;;
    *) fail "auth_deep.smtp_signup_otp_request" "expected 2xx, got HTTP $otp_status" ;;
  esac
  if mailpit_message_for "$smtp_mailpit_container" "$signup_email" "Confirm Your Email" "$ARTIFACT_DIR/auth-deep-smtp-signup-message.json"; then
    validate_mail_links_project_host "$ARTIFACT_DIR/auth-deep-smtp-signup-message.json" "$api_host" "signup"
    pass "auth_deep.smtp_signup_email" "captured signup email on project host"
  else
    fail "auth_deep.smtp_signup_email" "signup email was not captured"
  fi
  local signup_code
  signup_code="$(mail_otp_code "$ARTIFACT_DIR/auth-deep-smtp-signup-message.json")"
  local signup_verify_status
  signup_verify_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-smtp-signup-verify.body" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $anon_key" \
    -X POST "$api_url/auth/v1/verify" \
    --data "{\"type\":\"signup\",\"email\":\"$signup_email\",\"token\":\"$signup_code\"}" \
    2>"$ARTIFACT_DIR/auth-deep-smtp-signup-verify.stderr" || printf '000')"
  case "$signup_verify_status" in
    2??)
      if [[ "$(json_get_file "$ARTIFACT_DIR/auth-deep-smtp-signup-verify.body" user.email)" != "$signup_email" ]]; then
        fail "auth_deep.smtp_signup_verify" "verified session email mismatch"
      fi
      smtp_signup_user_id="$(json_get_file "$ARTIFACT_DIR/auth-deep-smtp-signup-verify.body" user.id)"
      pass "auth_deep.smtp_signup_verify" "HTTP $signup_verify_status"
    ;;
    *) fail "auth_deep.smtp_signup_verify" "expected 2xx, got HTTP $signup_verify_status" ;;
  esac

  run_send_email_hook_check
}

admin_create_body="$ARTIFACT_DIR/auth-deep-admin-create-user.body"
admin_create_err="$ARTIFACT_DIR/auth-deep-admin-create-user.stderr"
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
    user_id="$(json_get_file "$admin_create_body" id)"
    pass "auth_deep.admin_create_confirmed_user" "HTTP $admin_create_status"
    ;;
  *) fail "auth_deep.admin_create_confirmed_user" "expected 2xx, got HTTP $admin_create_status" ;;
esac

token_body="$ARTIFACT_DIR/auth-deep-token.body"
token_err="$ARTIFACT_DIR/auth-deep-token.stderr"
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
    refresh_token="$(json_get_file "$token_body" refresh_token)"
    if [[ -z "$user_access_token" || -z "$refresh_token" ]]; then
      fail "auth_deep.password_grant" "token response did not include access and refresh tokens"
    fi
    pass "auth_deep.password_grant" "HTTP $token_status"
    ;;
  *) fail "auth_deep.password_grant" "expected 2xx, got HTTP $token_status" ;;
esac

node - "$user_access_token" "$user_id" >"$ARTIFACT_DIR/auth-deep-jwt-claims.out" 2>"$ARTIFACT_DIR/auth-deep-jwt-claims.stderr" <<'NODE'
const token = process.argv[2];
const expectedSub = process.argv[3];
const payload = JSON.parse(Buffer.from(token.split(".")[1], "base64url").toString("utf8"));
if (payload.sub !== expectedSub) throw new Error(`sub=${payload.sub}`);
if (payload.role !== "authenticated") throw new Error(`role=${payload.role}`);
if (payload.aud !== "authenticated") throw new Error(`aud=${payload.aud}`);
process.stdout.write(JSON.stringify({ sub: payload.sub, role: payload.role, aud: payload.aud }));
NODE
pass "auth_deep.jwt_claims" "hosted-compatible sub, role, and aud present"

if compat_bool "${SUPADUPA_COMPAT_AUTH_MFA_VALIDATE:-false}"; then
  if SUPABASE_URL="$api_url" \
    SUPABASE_ANON_KEY="$anon_key" \
    SUPABASE_SERVICE_ROLE_KEY="$service_role" \
    SUPADUPA_COMPAT_RUN_ID="$run_id" \
    node "$SCRIPT_DIR/auth-mfa-totp-probe.mjs" >"$ARTIFACT_DIR/auth-deep-mfa-totp.out" 2>"$ARTIFACT_DIR/auth-deep-mfa-totp.stderr"; then
    pass "auth_deep.mfa_totp" "enroll/challenge/verify/listFactors reached aal2"
  else
    fail "auth_deep.mfa_totp" "MFA TOTP probe failed; see auth-deep-mfa-totp.stderr"
  fi
else
  skip "auth_deep.mfa_totp" "set SUPADUPA_COMPAT_AUTH_MFA_VALIDATE=true to run MFA validation"
fi

run_sms_provider_runtime_config_check
run_real_sms_provider_delivery_check
run_auth_hook_checks

refresh_body="$ARTIFACT_DIR/auth-deep-refresh.body"
refresh_err="$ARTIFACT_DIR/auth-deep-refresh.stderr"
set +e
refresh_status="$(curl -sS -o "$refresh_body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $anon_key" \
  -X POST "$api_url/auth/v1/token?grant_type=refresh_token" \
  --data "{\"refresh_token\":\"$refresh_token\"}" \
  2>"$refresh_err")"
refresh_rc="$?"
set -e
if [[ "$refresh_rc" -ne 0 ]]; then
  refresh_status="000"
fi
case "$refresh_status" in
  2??)
    refreshed_access_token="$(json_get_file "$refresh_body" access_token)"
    refreshed_refresh_token="$(json_get_file "$refresh_body" refresh_token)"
    if [[ -z "$refreshed_access_token" || -z "$refreshed_refresh_token" || "$refreshed_refresh_token" == "$refresh_token" ]]; then
      fail "auth_deep.refresh_token" "refresh response did not rotate tokens"
    fi
    pass "auth_deep.refresh_token" "HTTP $refresh_status"
    ;;
  *) fail "auth_deep.refresh_token" "expected 2xx, got HTTP $refresh_status" ;;
esac

if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
  -v ON_ERROR_STOP=1 \
  -v user_id="$user_id" \
  -v other_id="00000000-0000-0000-0000-000000000000" \
  -q >"$ARTIFACT_DIR/auth-deep-rls-setup.out" 2>"$ARTIFACT_DIR/auth-deep-rls-setup.stderr" <<'SQL'
drop table if exists public.compat_auth_rls;
create table public.compat_auth_rls (
  id text primary key,
  owner_id uuid not null,
  note text not null
);

alter table public.compat_auth_rls enable row level security;
grant select on public.compat_auth_rls to anon, authenticated;
create policy compat_auth_rls_owner_select
  on public.compat_auth_rls
  for select
  using (auth.uid() = owner_id);

insert into public.compat_auth_rls (id, owner_id, note)
values
  ('own-row', :'user_id'::uuid, 'visible to owner'),
  ('other-row', :'other_id'::uuid, 'hidden from owner');

notify pgrst, 'reload schema';
SQL
then
  pass "auth_deep.rls_setup" "compat_auth_rls table created"
else
  fail "auth_deep.rls_setup" "RLS setup failed; see auth-deep-rls-setup.stderr"
fi

deadline=$((SECONDS + ${SUPADUPA_COMPAT_REST_SCHEMA_TIMEOUT_SECONDS:-90}))
while (( SECONDS < deadline )); do
  set +e
  rls_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-rls-user.body" -w '%{http_code}' \
    -H "apikey: $anon_key" \
    -H "Authorization: Bearer $refreshed_access_token" \
    "$api_url/rest/v1/compat_auth_rls?select=id,note&order=id.asc" \
    2>"$ARTIFACT_DIR/auth-deep-rls-user.stderr")"
  rls_rc="$?"
  set -e
  if [[ "$rls_rc" -eq 0 && "$rls_status" =~ ^2 ]] &&
    grep -q '"id":"own-row"' "$ARTIFACT_DIR/auth-deep-rls-user.body" &&
    ! grep -q '"id":"other-row"' "$ARTIFACT_DIR/auth-deep-rls-user.body"; then
    pass "auth_deep.rls_authenticated_claims" "HTTP $rls_status owner row only"
    break
  fi
  sleep 3
done
if (( SECONDS >= deadline )); then
  fail "auth_deep.rls_authenticated_claims" "owner row not visible through RLS before timeout"
fi

set +e
anon_rls_status="$(curl -sS -o "$ARTIFACT_DIR/auth-deep-rls-anon.body" -w '%{http_code}' \
  -H "apikey: $anon_key" \
  "$api_url/rest/v1/compat_auth_rls?select=id,note&order=id.asc" \
  2>"$ARTIFACT_DIR/auth-deep-rls-anon.stderr")"
anon_rls_rc="$?"
set -e
if [[ "$anon_rls_rc" -ne 0 ]]; then
  anon_rls_status="000"
fi
case "$anon_rls_status" in
  2??)
    if [[ "$(cat "$ARTIFACT_DIR/auth-deep-rls-anon.body")" != "[]" ]]; then
      fail "auth_deep.rls_anon_rejected" "expected anon to see no rows"
    fi
    pass "auth_deep.rls_anon_rejected" "anon sees no rows"
    ;;
  *) fail "auth_deep.rls_anon_rejected" "expected 2xx empty result, got HTTP $anon_rls_status" ;;
esac

logout_body="$ARTIFACT_DIR/auth-deep-logout.body"
logout_err="$ARTIFACT_DIR/auth-deep-logout.stderr"
set +e
logout_status="$(curl -sS -o "$logout_body" -w '%{http_code}' \
  -H "apikey: $anon_key" \
  -H "Authorization: Bearer $refreshed_access_token" \
  -X POST "$api_url/auth/v1/logout" \
  2>"$logout_err")"
logout_rc="$?"
set -e
if [[ "$logout_rc" -ne 0 ]]; then
  logout_status="000"
fi
case "$logout_status" in
  2??) pass "auth_deep.logout" "HTTP $logout_status" ;;
  *) fail "auth_deep.logout" "expected 2xx, got HTTP $logout_status" ;;
esac

refresh_after_logout_body="$ARTIFACT_DIR/auth-deep-refresh-after-logout.body"
refresh_after_logout_err="$ARTIFACT_DIR/auth-deep-refresh-after-logout.stderr"
set +e
refresh_after_logout_status="$(curl -sS -o "$refresh_after_logout_body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $anon_key" \
  -X POST "$api_url/auth/v1/token?grant_type=refresh_token" \
  --data "{\"refresh_token\":\"$refreshed_refresh_token\"}" \
  2>"$refresh_after_logout_err")"
refresh_after_logout_rc="$?"
set -e
if [[ "$refresh_after_logout_rc" -ne 0 ]]; then
  refresh_after_logout_status="000"
fi
case "$refresh_after_logout_status" in
  400|401|403)
    if grep -Eqi 'refresh_token_not_found|invalid refresh token|token' "$refresh_after_logout_body"; then
      pass "auth_deep.logout_revokes_refresh" "HTTP $refresh_after_logout_status"
    else
      fail "auth_deep.logout_revokes_refresh" "unexpected rejection body: $(cat "$refresh_after_logout_body")"
    fi
    ;;
  *) fail "auth_deep.logout_revokes_refresh" "expected refresh rejection after logout, got HTTP $refresh_after_logout_status" ;;
esac

run_auth_smtp_checks
