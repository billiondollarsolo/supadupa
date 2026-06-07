#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool npm
require_tool node
ensure_token
ensure_profile

api_url="$(profile_value api_url)"
anon_key="$(reveal_secret_value anon_key)"
service_role="$(reveal_secret_value service_role)"
run_id=""
if [[ -s "$ARTIFACT_DIR/db-fixture-run-id" ]]; then
  run_id="$(cat "$ARTIFACT_DIR/db-fixture-run-id")"
fi

if [[ ! -d "$SCRIPT_DIR/node_modules/@supabase/supabase-js" ]]; then
  if npm --prefix "$SCRIPT_DIR" install --omit=dev --no-audit --no-fund --package-lock=false \
    >"$ARTIFACT_DIR/sdk-js-install.out" 2>"$ARTIFACT_DIR/sdk-js-install.stderr"; then
    pass "sdk.js.install" "@supabase/supabase-js installed"
  else
    fail "sdk.js.install" "npm install failed; see sdk-js-install.stderr"
  fi
fi

if SUPABASE_URL="$api_url" \
  SUPABASE_ANON_KEY="$anon_key" \
  SUPABASE_SERVICE_ROLE_KEY="$service_role" \
  SUPADUPA_TEST_REF="$SUPADUPA_TEST_REF" \
  SUPADUPA_COMPAT_RUN_ID="$run_id" \
  node "$SCRIPT_DIR/sdk-js-probe.mjs" \
  >"$ARTIFACT_DIR/sdk-js.out" 2>"$ARTIFACT_DIR/sdk-js.stderr"; then
  rows="$(json_get_file_optional "$ARTIFACT_DIR/sdk-js.out" rows)"
  auth_ok="$(json_get_file_optional "$ARTIFACT_DIR/sdk-js.out" auth.ok)"
  if [[ "$auth_ok" != "true" ]]; then
    fail "sdk.js.auth" "Supabase JS SDK auth probe did not report success"
  fi
  pass "sdk.js.select" "rows=$rows"
  pass "sdk.js.auth" "signInWithPassword and getUser passed"
else
  fail "sdk.js.select" "Supabase JS SDK probe failed; see sdk-js.stderr"
fi
