#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

if [[ -z "${SUPADUPA_COMPAT_CUSTOM_DOMAIN_FQDN:-}" ]]; then
  skip "custom_domain.enabled" "set SUPADUPA_COMPAT_CUSTOM_DOMAIN_FQDN to run"
  exit 0
fi

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool curl
require_tool jq
require_tool node
require_tool openssl
if [[ ! -d "$SCRIPT_DIR/node_modules/ws" ]]; then
  require_tool npm
fi
ensure_token
ensure_profile

token="$(read_secret_file "$ARTIFACT_DIR/token")"
api_base="${SUPADUPA_API_URL%/}"
project_ref="$SUPADUPA_TEST_REF"
fqdn="${SUPADUPA_COMPAT_CUSTOM_DOMAIN_FQDN%.}"
tmp_dir="$(mktemp -d)"
created_domain=false
anon_key="$(reveal_secret_value anon_key)"
service_role="$(reveal_secret_value service_role)"

api_json() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local out="$4"
  local err="$5"
  local status

  if [[ -n "$body" ]]; then
    status="$(curl -sS -o "$out" -w '%{http_code}' \
      -X "$method" \
      -H "Authorization: Bearer $token" \
      -H "Content-Type: application/json" \
      -d "$body" \
      "$api_base$path" 2>"$err")"
  else
    status="$(curl -sS -o "$out" -w '%{http_code}' \
      -X "$method" \
      -H "Authorization: Bearer $token" \
      "$api_base$path" 2>"$err")"
  fi
  printf '%s' "$status"
}

cleanup_custom_domain() {
  rm -rf "$tmp_dir"
  if [[ "$created_domain" == "true" ]]; then
    api_json DELETE "/v1/projects/$project_ref/domains/$fqdn" "" "$ARTIFACT_DIR/custom-domain-delete-cleanup.json" "$ARTIFACT_DIR/custom-domain-delete-cleanup.stderr" >/dev/null || true
  fi
}
trap cleanup_custom_domain EXIT

project_file="$ARTIFACT_DIR/custom-domain-project.json"
project_status="$(api_json GET "/v1/projects/$project_ref" "" "$project_file" "$ARTIFACT_DIR/custom-domain-project.stderr")"
if [[ "$project_status" != 2* ]]; then
  fail "custom_domain.project" "expected project lookup 2xx, got HTTP $project_status"
fi
org_id="$(jq -er '.org_id' "$project_file")"

if compat_bool "${SUPADUPA_COMPAT_ENABLE_CUSTOM_DOMAINS:-false}"; then
  features_status="$(api_json PUT "/v1/orgs/$org_id/features" '{"overrides":{"custom_domains":true}}' "$ARTIFACT_DIR/custom-domain-features.json" "$ARTIFACT_DIR/custom-domain-features.stderr")"
  if [[ "$features_status" != 2* ]]; then
    fail "custom_domain.feature_enable" "expected feature enable 2xx, got HTTP $features_status"
  fi
  pass "custom_domain.feature_enable" "custom_domains enabled for org"
fi

project_domain="$(jq -er '.spec.domain' "$project_file")"
for reserved_host in \
  "$project_ref.$project_domain" \
  "studio-$project_ref.$project_domain" \
  "storage-$project_ref.$project_domain" \
  "db-$project_ref.$project_domain" \
  "pooler-$project_ref.$project_domain"; do
  reserved_name="$(printf '%s' "$reserved_host" | tr -c 'A-Za-z0-9._-' '-')"
  reserved_status="$(api_json POST "/v1/projects/$project_ref/domains" "{\"fqdn\":\"$reserved_host\"}" "$ARTIFACT_DIR/custom-domain-reserved-$reserved_name.json" "$ARTIFACT_DIR/custom-domain-reserved-$reserved_name.stderr")"
  if [[ "$reserved_status" != "409" ]]; then
    fail "custom_domain.reserved_host.$reserved_name" "expected generated host conflict, got HTTP $reserved_status"
  fi
  pass "custom_domain.reserved_host.$reserved_name" "generated host rejected"
done

create_status="$(api_json POST "/v1/projects/$project_ref/domains" "{\"fqdn\":\"$fqdn\"}" "$ARTIFACT_DIR/custom-domain-create.json" "$ARTIFACT_DIR/custom-domain-create.stderr")"
if [[ "$create_status" != "201" ]]; then
  fail "custom_domain.create" "expected create 201, got HTTP $create_status"
fi
created_domain=true
if [[ "$(jq -er '.cert_mode' "$ARTIFACT_DIR/custom-domain-create.json")" != "manual" ]]; then
  fail "custom_domain.create" "expected initial cert_mode=manual"
fi
pass "custom_domain.create" "created $fqdn"

openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 2 \
  -subj "/CN=$fqdn" \
  -addext "subjectAltName=DNS:$fqdn" \
  -keyout "$tmp_dir/domain.key" \
  -out "$tmp_dir/domain.crt" >"$ARTIFACT_DIR/custom-domain-openssl.out" 2>"$ARTIFACT_DIR/custom-domain-openssl.stderr"

upload_payload="$(jq -n --rawfile cert "$tmp_dir/domain.crt" --rawfile key "$tmp_dir/domain.key" '{certificate_pem:$cert, private_key_pem:$key}')"
upload_status="$(api_json PUT "/v1/projects/$project_ref/domains/$fqdn/certificate" "$upload_payload" "$ARTIFACT_DIR/custom-domain-upload.json" "$ARTIFACT_DIR/custom-domain-upload.stderr")"
if [[ "$upload_status" != 2* ]]; then
  fail "custom_domain.byo_upload" "expected upload 2xx, got HTTP $upload_status"
fi
if [[ "$(jq -er '.cert_status' "$ARTIFACT_DIR/custom-domain-upload.json")" != "uploaded" || "$(jq -er '.cert_mode' "$ARTIFACT_DIR/custom-domain-upload.json")" != "byo" ]]; then
  fail "custom_domain.byo_upload" "expected uploaded/byo metadata"
fi
if grep -q "PRIVATE KEY" "$ARTIFACT_DIR/custom-domain-upload.json"; then
  fail "custom_domain.byo_upload" "private key leaked in API response"
fi
pass "custom_domain.byo_upload" "uploaded BYO certificate without key leak"

generated_api_url="$(profile_value api_url)"
connect_status="$(api_json GET "/v1/projects/$project_ref/connect" "" "$ARTIFACT_DIR/custom-domain-connect.json" "$ARTIFACT_DIR/custom-domain-connect.stderr")"
if [[ "$connect_status" != 2* ]]; then
  fail "custom_domain.connect_surface" "expected connect payload 2xx, got HTTP $connect_status"
fi
if ! jq -e --arg url "https://$fqdn" '(.custom_api_urls // []) | index($url)' "$ARTIFACT_DIR/custom-domain-connect.json" >/dev/null; then
  fail "custom_domain.connect_surface" "custom domain missing from connect custom_api_urls"
fi
if ! jq -e --arg fqdn "$fqdn" '(.custom_domains // []) | any(.fqdn == $fqdn and (.cert_status == "uploaded" or .cert_status == "issued"))' "$ARTIFACT_DIR/custom-domain-connect.json" >/dev/null; then
  fail "custom_domain.connect_surface" "custom domain metadata missing from connect payload"
fi
if [[ "$(jq -er '.api_url' "$ARTIFACT_DIR/custom-domain-connect.json")" != "$generated_api_url" ]]; then
  fail "custom_domain.connect_surface" "generated api_url should remain canonical"
fi
pass "custom_domain.connect_surface" "custom API URL discoverable from connect payload"

cli_status="$(api_json GET "/v1/projects/$project_ref/connect/cli" "" "$ARTIFACT_DIR/custom-domain-cli-profile.json" "$ARTIFACT_DIR/custom-domain-cli-profile.stderr")"
if [[ "$cli_status" != 2* ]]; then
  fail "custom_domain.cli_profile_surface" "expected CLI profile 2xx, got HTTP $cli_status"
fi
if ! jq -e --arg url "https://$fqdn" '(.custom_api_urls // []) | index($url)' "$ARTIFACT_DIR/custom-domain-cli-profile.json" >/dev/null; then
  fail "custom_domain.cli_profile_surface" "custom domain missing from CLI profile custom_api_urls"
fi
if [[ "$(jq -er '.env.SUPADUPA_CUSTOM_API_URL' "$ARTIFACT_DIR/custom-domain-cli-profile.json")" != "https://$fqdn" ]]; then
  fail "custom_domain.cli_profile_surface" "SUPADUPA_CUSTOM_API_URL missing from CLI profile env"
fi
if ! jq -e --arg url "https://$fqdn" '.supabase_config_toml | contains("custom_api_urls = [\"" + $url + "\"]")' "$ARTIFACT_DIR/custom-domain-cli-profile.json" >/dev/null; then
  fail "custom_domain.cli_profile_surface" "custom_api_urls missing from Supabase config TOML"
fi
pass "custom_domain.cli_profile_surface" "custom API URL discoverable from CLI profile"

if supadupa_cli_authed projects cli-profile --ref "$project_ref" --format env --api-domain "$fqdn" \
  >"$ARTIFACT_DIR/custom-domain-cli-env.out" 2>"$ARTIFACT_DIR/custom-domain-cli-env.stderr"; then
  if ! grep -q "SUPABASE_URL='https://$fqdn'" "$ARTIFACT_DIR/custom-domain-cli-env.out" || ! grep -q "SUPADUPA_SELECTED_API_URL='https://$fqdn'" "$ARTIFACT_DIR/custom-domain-cli-env.out"; then
    fail "custom_domain.cli_env_select" "selected custom SUPABASE_URL missing from CLI env output"
  fi
  pass "custom_domain.cli_env_select" "cli-profile env can select custom API URL"
else
  fail "custom_domain.cli_env_select" "supadupa-cli cli-profile env failed; see custom-domain-cli-env.stderr"
fi

if supadupa_cli_authed projects cli-profile --ref "$project_ref" --format toml \
  >"$ARTIFACT_DIR/custom-domain-cli-toml.out" 2>"$ARTIFACT_DIR/custom-domain-cli-toml.stderr"; then
  if ! grep -Fq "custom_api_urls = [\"https://$fqdn\"]" "$ARTIFACT_DIR/custom-domain-cli-toml.out"; then
    fail "custom_domain.cli_toml" "custom_api_urls missing from CLI TOML output"
  fi
  pass "custom_domain.cli_toml" "cli-profile TOML includes custom API URLs"
else
  fail "custom_domain.cli_toml" "supadupa-cli cli-profile toml failed; see custom-domain-cli-toml.stderr"
fi

if supadupa_cli_authed projects env --ref "$project_ref" --prefer-custom-domain \
  >"$ARTIFACT_DIR/custom-domain-project-env.out" 2>"$ARTIFACT_DIR/custom-domain-project-env.stderr"; then
  if ! grep -q "SUPABASE_URL='https://$fqdn'" "$ARTIFACT_DIR/custom-domain-project-env.out"; then
    fail "custom_domain.project_env_select" "projects env did not select custom SUPABASE_URL"
  fi
  pass "custom_domain.project_env_select" "projects env can prefer custom API URL"
else
  fail "custom_domain.project_env_select" "supadupa-cli projects env failed; see custom-domain-project-env.stderr"
fi

link_workspace="$tmp_dir/custom-domain-workspace"
if supadupa_cli_authed projects link --ref "$project_ref" --dir "$link_workspace" --api-domain "$fqdn" \
  >"$ARTIFACT_DIR/custom-domain-link.out" 2>"$ARTIFACT_DIR/custom-domain-link.stderr"; then
  if ! grep -q "SUPABASE_URL='https://$fqdn'" "$link_workspace/.supadupa/supabase.env"; then
    fail "custom_domain.project_link_select" "linked supabase.env did not select custom SUPABASE_URL"
  fi
  if ! jq -e --arg url "https://$fqdn" '.selected_api_url == $url and (((.custom_api_urls // []) | index($url)) != null)' "$link_workspace/.supadupa/project.json" >/dev/null; then
    fail "custom_domain.project_link_select" "linked project.json missing selected/custom API URL metadata"
  fi
  pass "custom_domain.project_link_select" "projects link can select custom API URL"
else
  fail "custom_domain.project_link_select" "supadupa-cli projects link failed; see custom-domain-link.stderr"
fi

generated_status="$(curl -sS -o "$ARTIFACT_DIR/custom-domain-generated-rest.body" -w '%{http_code}' "$generated_api_url/rest/v1/" 2>"$ARTIFACT_DIR/custom-domain-generated-rest.stderr" || true)"
case "$generated_status" in
  401|403|404|2??) pass "custom_domain.generated_host" "generated host remains reachable: HTTP $generated_status" ;;
  *) fail "custom_domain.generated_host" "unexpected generated host HTTP $generated_status" ;;
esac

if [[ -n "${SUPADUPA_COMPAT_CUSTOM_DOMAIN_RESOLVE_IP:-}" ]]; then
  resolve_args=(--resolve "$fqdn:443:$SUPADUPA_COMPAT_CUSTOM_DOMAIN_RESOLVE_IP")

  custom_auth_status="$(curl -k -sS -o "$ARTIFACT_DIR/custom-domain-auth-health.body" -w '%{http_code}' \
    "${resolve_args[@]}" \
    "https://$fqdn/auth/v1/health" 2>"$ARTIFACT_DIR/custom-domain-auth-health.stderr" || true)"
  case "$custom_auth_status" in
    2??) pass "custom_domain.auth_health" "HTTP $custom_auth_status" ;;
    *) fail "custom_domain.auth_health" "expected 2xx, got HTTP $custom_auth_status" ;;
  esac

  custom_rest_status="$(curl -k -sS -o "$ARTIFACT_DIR/custom-domain-rest.body" -w '%{http_code}' \
    "${resolve_args[@]}" \
    -H "apikey: $anon_key" \
    "https://$fqdn/rest/v1/" 2>"$ARTIFACT_DIR/custom-domain-rest.stderr" || true)"
  case "$custom_rest_status" in
    2??|404) pass "custom_domain.rest" "HTTP $custom_rest_status" ;;
    *) fail "custom_domain.rest" "unexpected REST HTTP $custom_rest_status" ;;
  esac

  custom_storage_status="$(curl -k -sS -o "$ARTIFACT_DIR/custom-domain-storage.body" -w '%{http_code}' \
    "${resolve_args[@]}" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    "https://$fqdn/storage/v1/bucket" 2>"$ARTIFACT_DIR/custom-domain-storage.stderr" || true)"
  case "$custom_storage_status" in
    2??) pass "custom_domain.storage" "HTTP $custom_storage_status" ;;
    *) fail "custom_domain.storage" "expected Storage bucket list 2xx, got HTTP $custom_storage_status" ;;
  esac

  if [[ ! -d "$SCRIPT_DIR/node_modules/ws" ]]; then
    if npm --prefix "$SCRIPT_DIR" install --omit=dev --no-audit --no-fund --package-lock=false \
      >"$ARTIFACT_DIR/custom-domain-realtime-sdk-install.out" 2>"$ARTIFACT_DIR/custom-domain-realtime-sdk-install.stderr"; then
      pass "custom_domain.realtime_sdk.install" "ws installed"
    else
      fail "custom_domain.realtime_sdk.install" "npm install failed; see custom-domain-realtime-sdk-install.stderr"
    fi
  fi

  if SUPADUPA_REALTIME_URL="https://$fqdn/realtime/v1" \
    SUPADUPA_REALTIME_KEY="$anon_key" \
    SUPADUPA_REALTIME_EXPECT="accept" \
    SUPADUPA_REALTIME_INSECURE_SKIP_VERIFY=true \
    SUPADUPA_REALTIME_RESOLVE_IP="$SUPADUPA_COMPAT_CUSTOM_DOMAIN_RESOLVE_IP" \
    node "$SCRIPT_DIR/realtime-probe.cjs" >"$ARTIFACT_DIR/custom-domain-realtime.out" 2>"$ARTIFACT_DIR/custom-domain-realtime.stderr"; then
    pass "custom_domain.realtime" "websocket opened"
  else
    fail "custom_domain.realtime" "custom-domain websocket failed; see custom-domain-realtime.stderr"
  fi

  function_name="${SUPADUPA_COMPAT_FUNCTION_NAME:-hello}"
  custom_function_status="$(curl -k -sS -o "$ARTIFACT_DIR/custom-domain-function-$function_name.body" -w '%{http_code}' \
    "${resolve_args[@]}" \
    -H "apikey: $anon_key" \
    -H "Authorization: Bearer $anon_key" \
    "https://$fqdn/functions/v1/$function_name" 2>"$ARTIFACT_DIR/custom-domain-function-$function_name.stderr" || true)"
  case "$custom_function_status" in
    2??) pass "custom_domain.functions.$function_name" "HTTP $custom_function_status" ;;
    *) fail "custom_domain.functions.$function_name" "expected function 2xx, got HTTP $custom_function_status" ;;
  esac
else
  skip "custom_domain.auth_health" "set SUPADUPA_COMPAT_CUSTOM_DOMAIN_RESOLVE_IP for custom-host routing checks"
  skip "custom_domain.rest" "set SUPADUPA_COMPAT_CUSTOM_DOMAIN_RESOLVE_IP for custom-host routing checks"
  skip "custom_domain.storage" "set SUPADUPA_COMPAT_CUSTOM_DOMAIN_RESOLVE_IP for custom-host routing checks"
  skip "custom_domain.realtime" "set SUPADUPA_COMPAT_CUSTOM_DOMAIN_RESOLVE_IP for custom-host routing checks"
  skip "custom_domain.functions" "set SUPADUPA_COMPAT_CUSTOM_DOMAIN_RESOLVE_IP for custom-host routing checks"
fi

reset_status="$(api_json DELETE "/v1/projects/$project_ref/domains/$fqdn/certificate" "" "$ARTIFACT_DIR/custom-domain-reset.json" "$ARTIFACT_DIR/custom-domain-reset.stderr")"
if [[ "$reset_status" != 2* || "$(jq -er '.cert_mode' "$ARTIFACT_DIR/custom-domain-reset.json")" != "manual" ]]; then
  fail "custom_domain.reset" "expected reset to manual, got HTTP $reset_status"
fi
pass "custom_domain.reset" "reset to managed/manual certificate state"

delete_status="$(api_json DELETE "/v1/projects/$project_ref/domains/$fqdn" "" "$ARTIFACT_DIR/custom-domain-delete.json" "$ARTIFACT_DIR/custom-domain-delete.stderr")"
if [[ "$delete_status" != "204" ]]; then
  fail "custom_domain.delete" "expected delete 204, got HTTP $delete_status"
fi
created_domain=false
pass "custom_domain.delete" "deleted $fqdn"
