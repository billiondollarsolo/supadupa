#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

if ! compat_bool "${SUPADUPA_COMPAT_BRANCH_VALIDATE:-false}"; then
  skip "branches_deep.enabled" "set SUPADUPA_COMPAT_BRANCH_VALIDATE=true to run"
  exit 0
fi

created_ref="$(cat "$ARTIFACT_DIR/created-project" 2>/dev/null || true)"
if [[ "$created_ref" != "$SUPADUPA_TEST_REF" ]] &&
  ! compat_bool "${SUPADUPA_COMPAT_ALLOW_BRANCH_ON_EXISTING:-false}"; then
  skip "branches_deep.guard" "set SUPADUPA_COMPAT_CREATE_PROJECT=true or SUPADUPA_COMPAT_ALLOW_BRANCH_ON_EXISTING=true"
  exit 0
fi

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool curl
require_tool node
ensure_token

token="$(read_secret_file "$ARTIFACT_DIR/token")"
api_base="${SUPADUPA_API_URL%/}"
branch_ref="${SUPADUPA_COMPAT_BRANCH_REF:-compat-br-$(date -u +%H%M%S)}"
created_branch_ref="$branch_ref"
original_features_json="$ARTIFACT_DIR/branches-deep-original-org-features.json"
project_json="$ARTIFACT_DIR/branches-deep-source-project.json"

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

cleanup_branch() {
  if [[ -n "${branch_ref:-}" ]]; then
    api_json DELETE "/v1/projects/$SUPADUPA_TEST_REF/branches/$branch_ref" "" \
      "$ARTIFACT_DIR/branches-deep-delete-cleanup.json" \
      "$ARTIFACT_DIR/branches-deep-delete-cleanup.stderr" >/dev/null || true
  fi
  if [[ -s "$original_features_json" && -n "${org_id:-}" ]]; then
    node -e '
const fs = require("fs");
const features = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
process.stdout.write(JSON.stringify({ overrides: features.overrides || {} }));
' "$original_features_json" >"$ARTIFACT_DIR/branches-deep-restore-features-payload.json" 2>"$ARTIFACT_DIR/branches-deep-restore-features-payload.stderr" &&
      api_json PUT "/v1/orgs/$org_id/features" "$(cat "$ARTIFACT_DIR/branches-deep-restore-features-payload.json")" \
        "$ARTIFACT_DIR/branches-deep-restore-features.json" \
        "$ARTIFACT_DIR/branches-deep-restore-features.stderr" >/dev/null || true
  fi
}
trap cleanup_branch EXIT

project_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF" "" "$project_json" "$ARTIFACT_DIR/branches-deep-source-project.stderr")"
if [[ "$project_status" != "200" ]]; then
  fail "branches_deep.source_project" "expected HTTP 200, got HTTP $project_status"
fi
org_id="${SUPADUPA_TEST_ORG_ID:-$(json_get_file_optional "$project_json" org_id)}"
if [[ -n "$org_id" ]]; then
  feature_status="$(api_json GET "/v1/orgs/$org_id/features" "" "$original_features_json" "$ARTIFACT_DIR/branches-deep-original-org-features.stderr")"
  if [[ "$feature_status" == "200" ]]; then
    node -e '
const fs = require("fs");
const features = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const overrides = {...(features.overrides || {}), preview_branches: true};
process.stdout.write(JSON.stringify({overrides}));
' "$original_features_json" >"$ARTIFACT_DIR/branches-deep-enable-feature-payload.json" 2>"$ARTIFACT_DIR/branches-deep-enable-feature-payload.stderr"
    enable_status="$(api_json PUT "/v1/orgs/$org_id/features" "$(cat "$ARTIFACT_DIR/branches-deep-enable-feature-payload.json")" "$ARTIFACT_DIR/branches-deep-enable-feature.json" "$ARTIFACT_DIR/branches-deep-enable-feature.stderr")"
    if [[ "$enable_status" != 2* ]]; then
      fail "branches_deep.feature_enable" "expected 2xx, got HTTP $enable_status"
    fi
    pass "branches_deep.feature_enable" "preview_branches enabled for validation"
  fi
fi

create_payload="$ARTIFACT_DIR/branches-deep-create-payload.json"
node -e '
const fs = require("fs");
const branchRef = process.argv[1];
fs.writeFileSync(process.argv[2], JSON.stringify({
  ref: branchRef,
  name: "Compat Branch " + branchRef,
  ttl_hours: 2,
  with_data: false
}));
' "$branch_ref" "$create_payload"

create_json="$ARTIFACT_DIR/branches-deep-create.json"
create_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/branches" "$(cat "$create_payload")" "$create_json" "$ARTIFACT_DIR/branches-deep-create.stderr")"
if [[ "$create_status" != "201" ]]; then
  fail "branches_deep.create" "expected HTTP 201, got HTTP $create_status"
fi
if node -e '
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const sourceRef = process.argv[2];
const branchRef = process.argv[3];
if (payload.branch?.source_project_ref !== sourceRef) throw new Error("source_project_ref mismatch");
if (payload.branch?.project_ref !== branchRef) throw new Error("branch project_ref mismatch");
if (payload.branch?.with_data !== false) throw new Error("branch should default to data-less");
if (payload.project?.ref !== branchRef) throw new Error("project ref mismatch");
if (payload.project?.status !== "healthy") throw new Error(`project status ${payload.project?.status}`);
if (JSON.stringify(payload).includes("POSTGRES_PASSWORD")) throw new Error("response leaked runtime environment");
' "$create_json" "$SUPADUPA_TEST_REF" "$branch_ref" >"$ARTIFACT_DIR/branches-deep-create-check.out" 2>"$ARTIFACT_DIR/branches-deep-create-check.stderr"; then
  pass "branches_deep.create" "branch_ref=$branch_ref"
else
  fail "branches_deep.create" "create response malformed; see branches-deep-create-check.stderr"
fi

list_json="$ARTIFACT_DIR/branches-deep-list.json"
list_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/branches" "" "$list_json" "$ARTIFACT_DIR/branches-deep-list.stderr")"
if [[ "$list_status" != "200" ]]; then
  fail "branches_deep.list" "expected HTTP 200, got HTTP $list_status"
fi
if node -e '
const fs = require("fs");
const branches = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const branch = branches.find((item) => item.project_ref === process.argv[2]);
if (!branch) throw new Error("branch not listed");
if (branch.with_data !== false) throw new Error("branch list should report data-less");
' "$list_json" "$branch_ref" >"$ARTIFACT_DIR/branches-deep-list-check.out" 2>"$ARTIFACT_DIR/branches-deep-list-check.stderr"; then
  pass "branches_deep.list" "branch listed"
else
  fail "branches_deep.list" "branch list malformed; see branches-deep-list-check.stderr"
fi

connect_json="$ARTIFACT_DIR/branches-deep-connect.json"
connect_status="$(api_json GET "/v1/projects/$branch_ref/connect/cli" "" "$connect_json" "$ARTIFACT_DIR/branches-deep-connect.stderr")"
if [[ "$connect_status" != "200" ]]; then
  fail "branches_deep.connect" "expected HTTP 200, got HTTP $connect_status"
fi
if node -e '
const fs = require("fs");
const profile = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const ref = process.argv[2];
const domain = new URL(profile.api_url).hostname.split(".").slice(1).join(".");
const expectedAPI = `https://${ref}.${domain}`;
if (profile.api_url !== expectedAPI) throw new Error(`api_url=${profile.api_url}`);
if (!profile.database_url.includes(`db-${ref}.`)) throw new Error(`database_url=${profile.database_url}`);
if (!profile.pooler_transaction_url.includes(`pooler-${ref}.`)) throw new Error(`pooler_transaction_url=${profile.pooler_transaction_url}`);
if (profile.storage_s3_url !== `https://storage-${ref}.${domain}/storage/v1/s3`) throw new Error(`storage_s3_url=${profile.storage_s3_url}`);
const publicText = JSON.stringify({
  api_url: profile.api_url,
  studio_url: profile.studio_url,
  storage_s3_url: profile.storage_s3_url,
  database_url: profile.database_url,
  pooler_transaction_url: profile.pooler_transaction_url,
  pooler_session_url: profile.pooler_session_url,
  env: Object.fromEntries(Object.entries(profile.env || {}).filter(([key]) => !key.startsWith("SUPADUPA_INTERNAL_"))),
  commands: Object.fromEntries(Object.entries(profile.commands || {}).filter(([key]) => !key.includes("internal"))),
});
if (/localhost|127[.]0[.]0[.]1|host[.]docker[.]internal|[.]internal/.test(publicText)) {
  throw new Error("public branch profile leaked local/internal URL");
}
' "$connect_json" "$branch_ref" >"$ARTIFACT_DIR/branches-deep-connect-check.out" 2>"$ARTIFACT_DIR/branches-deep-connect-check.stderr"; then
  pass "branches_deep.connect" "remote-safe branch profile"
else
  fail "branches_deep.connect" "branch profile malformed; see branches-deep-connect-check.stderr"
fi

manifest_json="$ARTIFACT_DIR/branches-deep-route-manifest.json"
manifest_status="$(api_json GET "/v1/projects/$branch_ref/route-manifest" "" "$manifest_json" "$ARTIFACT_DIR/branches-deep-route-manifest.stderr")"
if [[ "$manifest_status" != "200" ]]; then
  fail "branches_deep.route_manifest" "expected HTTP 200, got HTTP $manifest_status"
fi
if node -e '
const fs = require("fs");
const manifest = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const ref = process.argv[2];
const http = new Set((manifest.http_routes || []).map((route) => route.fqdn));
const tcp = new Set((manifest.tcp_routes || []).map((route) => route.fqdn));
const base = [...http].find((fqdn) => fqdn.startsWith(`${ref}.`));
if (!base) throw new Error("base HTTP route missing");
const domain = base.split(".").slice(1).join(".");
for (const fqdn of [`${ref}.${domain}`, `studio-${ref}.${domain}`, `storage-${ref}.${domain}`]) {
  if (!http.has(fqdn)) throw new Error(`missing HTTP route ${fqdn}`);
}
for (const fqdn of [`db-${ref}.${domain}`, `pooler-${ref}.${domain}`]) {
  if (!tcp.has(fqdn)) throw new Error(`missing TCP route ${fqdn}`);
}
' "$manifest_json" "$branch_ref" >"$ARTIFACT_DIR/branches-deep-route-manifest-check.out" 2>"$ARTIFACT_DIR/branches-deep-route-manifest-check.stderr"; then
  pass "branches_deep.route_manifest" "branch routes are isolated and public"
else
  fail "branches_deep.route_manifest" "route manifest malformed; see branches-deep-route-manifest-check.stderr"
fi

health_url="$(json_get_file "$connect_json" api_url)/auth/v1/health"
deadline=$((SECONDS + ${SUPADUPA_BRANCH_READY_TIMEOUT_SECONDS:-180}))
while (( SECONDS < deadline )); do
  status="$(curl -ksS -o "$ARTIFACT_DIR/branches-deep-auth-health.body" -w '%{http_code}' "$health_url" 2>"$ARTIFACT_DIR/branches-deep-auth-health.stderr" || true)"
  if [[ "$status" =~ ^2 ]]; then
    pass "branches_deep.public_auth_health" "HTTP $status"
    break
  fi
  sleep 5
done
if ! [[ "${status:-}" =~ ^2 ]]; then
  fail "branches_deep.public_auth_health" "branch Auth health did not become reachable"
fi

delete_json="$ARTIFACT_DIR/branches-deep-delete.json"
delete_status="$(api_json DELETE "/v1/projects/$SUPADUPA_TEST_REF/branches/$branch_ref" "" "$delete_json" "$ARTIFACT_DIR/branches-deep-delete.stderr")"
if [[ "$delete_status" != "204" ]]; then
  fail "branches_deep.delete" "expected HTTP 204, got HTTP $delete_status"
fi
branch_ref=""
pass "branches_deep.delete" "branch removed"

list_after_json="$ARTIFACT_DIR/branches-deep-list-after-delete.json"
list_after_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/branches" "" "$list_after_json" "$ARTIFACT_DIR/branches-deep-list-after-delete.stderr")"
if [[ "$list_after_status" != "200" ]]; then
  fail "branches_deep.delete_verify" "expected HTTP 200, got HTTP $list_after_status"
fi
if node -e '
const fs = require("fs");
const branches = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (branches.some((item) => item.project_ref === process.argv[2])) throw new Error("branch still listed");
' "$list_after_json" "$created_branch_ref" >"$ARTIFACT_DIR/branches-deep-list-after-delete-check.out" 2>"$ARTIFACT_DIR/branches-deep-list-after-delete-check.stderr"; then
  pass "branches_deep.delete_verify" "branch metadata removed"
else
  fail "branches_deep.delete_verify" "branch metadata still present"
fi
