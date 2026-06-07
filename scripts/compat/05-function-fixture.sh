#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

if ! compat_bool "${SUPADUPA_COMPAT_CREATE_PROJECT:-false}" &&
  ! compat_bool "${SUPADUPA_COMPAT_DEPLOY_FIXTURES:-false}"; then
  skip "fixture.function.deploy" "fixture deployment is disabled for existing projects"
  exit 0
fi

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
ensure_token
ensure_profile

function_name="${SUPADUPA_COMPAT_FUNCTION_NAME:-hello}"
source_file="${SUPADUPA_COMPAT_FUNCTION_SOURCE:-$SCRIPT_DIR/fixtures/functions/hello/index.ts}"
if [[ ! -f "$source_file" ]]; then
  fail "fixture.function.source" "missing function source: $source_file"
fi

if supadupa_cli_authed functions deploy \
  --ref "$SUPADUPA_TEST_REF" \
  --name "$function_name" \
  --entrypoint index.ts \
  --source-file "$source_file" \
  --verify-jwt=true \
  >"$ARTIFACT_DIR/function-deploy.json" 2>"$ARTIFACT_DIR/function-deploy.stderr"; then
  pass "fixture.function.deploy" "$function_name"
else
  fail "fixture.function.deploy" "function deploy failed; see function-deploy.stderr"
fi

api_url="$(profile_value api_url)"
anon_key="$(reveal_secret_value anon_key)"
deadline=$((SECONDS + ${SUPADUPA_COMPAT_FUNCTION_TIMEOUT_SECONDS:-90}))
while (( SECONDS < deadline )); do
  set +e
  function_status="$(curl -sS -o "$ARTIFACT_DIR/function-fixture.body" -w '%{http_code}' \
    -H "apikey: $anon_key" \
    -H "Authorization: Bearer $anon_key" \
    "$api_url/functions/v1/$function_name" 2>"$ARTIFACT_DIR/function-fixture.stderr")"
  function_rc="$?"
  set -e

  if [[ "$function_rc" -eq 0 && "$function_status" =~ ^2 ]]; then
    pass "fixture.function.ready" "HTTP $function_status"
    exit 0
  fi
  sleep 3
done

fail "fixture.function.ready" "function did not become reachable before timeout"
