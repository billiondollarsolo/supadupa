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

realtime_url="$(profile_value_optional realtime_url)"
if [[ -z "$realtime_url" ]]; then
  fail "realtime.profile_url" "profile did not include realtime_url"
fi
case "$realtime_url" in
  https://*|http://*|wss://*|ws://*) pass "realtime.profile_url" "$realtime_url" ;;
  *) fail "realtime.profile_url" "unexpected realtime_url scheme" ;;
esac

anon_key="$(reveal_secret_value anon_key)"
pass "secret.realtime_anon_key" "anon key available from restricted artifact"

if [[ ! -d "$SCRIPT_DIR/node_modules/@supabase/supabase-js" ]]; then
  if npm --prefix "$SCRIPT_DIR" install --omit=dev --no-audit --no-fund --package-lock=false \
    >"$ARTIFACT_DIR/realtime-sdk-install.out" 2>"$ARTIFACT_DIR/realtime-sdk-install.stderr"; then
    pass "realtime.sdk.install" "@supabase/supabase-js installed"
  else
    fail "realtime.sdk.install" "npm install failed; see realtime-sdk-install.stderr"
  fi
fi

if SUPADUPA_REALTIME_URL="$realtime_url" \
  SUPADUPA_REALTIME_KEY="invalid" \
  SUPADUPA_REALTIME_EXPECT="reject" \
  node "$SCRIPT_DIR/realtime-probe.cjs" >"$ARTIFACT_DIR/realtime-invalid.out" 2>"$ARTIFACT_DIR/realtime-invalid.stderr"; then
  invalid_status="$(tr -d '\r\n' <"$ARTIFACT_DIR/realtime-invalid.out")"
  pass "realtime.invalid_key_rejected" "HTTP ${invalid_status:-403}"
else
  fail "realtime.invalid_key_rejected" "invalid key websocket was not rejected; see realtime-invalid.stderr"
fi

if SUPADUPA_REALTIME_URL="$realtime_url" \
  SUPADUPA_REALTIME_KEY="" \
  SUPADUPA_REALTIME_EXPECT="reject" \
  node "$SCRIPT_DIR/realtime-probe.cjs" >"$ARTIFACT_DIR/realtime-missing-key.out" 2>"$ARTIFACT_DIR/realtime-missing-key.stderr"; then
  missing_status="$(tr -d '\r\n' <"$ARTIFACT_DIR/realtime-missing-key.out")"
  pass "realtime.missing_key_rejected" "HTTP ${missing_status:-403}"
else
  fail "realtime.missing_key_rejected" "missing-key websocket was not rejected; see realtime-missing-key.stderr"
fi

if SUPADUPA_REALTIME_URL="$realtime_url" \
  SUPADUPA_REALTIME_KEY="$anon_key" \
  SUPADUPA_REALTIME_EXPECT="accept" \
  node "$SCRIPT_DIR/realtime-probe.cjs" >"$ARTIFACT_DIR/realtime-anon.out" 2>"$ARTIFACT_DIR/realtime-anon.stderr"; then
  pass "realtime.anon_key_accepted" "websocket opened"
else
  fail "realtime.anon_key_accepted" "anon websocket failed; see realtime-anon.stderr"
fi

run_id="${SUPADUPA_COMPAT_RUN_ID:-}"
if [[ -z "$run_id" && -s "$ARTIFACT_DIR/db-fixture-run-id" ]]; then
  run_id="$(cat "$ARTIFACT_DIR/db-fixture-run-id")"
fi
if [[ -z "$run_id" ]]; then
  run_id="$(date -u +%Y%m%d%H%M%S)-$$"
fi

api_url="$(profile_value api_url)"
if SUPABASE_URL="$api_url" \
  SUPABASE_ANON_KEY="$anon_key" \
  SUPADUPA_TEST_REF="$SUPADUPA_TEST_REF" \
  SUPADUPA_COMPAT_RUN_ID="$run_id" \
  node "$SCRIPT_DIR/realtime-broadcast-probe.mjs" >"$ARTIFACT_DIR/realtime-broadcast.out" 2>"$ARTIFACT_DIR/realtime-broadcast.stderr"; then
  pass "realtime.broadcast" "message delivered"
else
  fail "realtime.broadcast" "broadcast probe failed; see realtime-broadcast.stderr"
fi
