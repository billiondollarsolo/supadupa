#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
ensure_token
ensure_profile

api_url="$(profile_value api_url)"
anon_key="$(reveal_secret_value anon_key)"
pass "secret.anon_key" "anon key stored in restricted artifact"

set +e
no_key_status="$(curl -sS -o "$ARTIFACT_DIR/rest-no-key.body" -w '%{http_code}' \
  "$api_url/rest/v1/" 2>"$ARTIFACT_DIR/rest-no-key.stderr")"
no_key_rc="$?"
set -e
if [[ "$no_key_rc" -ne 0 ]]; then
  no_key_status="000"
fi

case "$no_key_status" in
  401|403)
    pass "rest.no_key_rejected" "HTTP $no_key_status"
    ;;
  *)
    fail "rest.no_key_rejected" "expected 401 or 403, got HTTP $no_key_status"
    ;;
esac

set +e
anon_status="$(curl -sS -o "$ARTIFACT_DIR/rest-anon.body" -w '%{http_code}' \
  -H "apikey: $anon_key" \
  "$api_url/rest/v1/" 2>"$ARTIFACT_DIR/rest-anon.stderr")"
anon_rc="$?"
set -e
if [[ "$anon_rc" -ne 0 ]]; then
  anon_status="000"
fi

case "$anon_status" in
  2??)
    pass "rest.anon_key_accepted" "HTTP $anon_status"
    ;;
  *)
    fail "rest.anon_key_accepted" "expected 2xx, got HTTP $anon_status"
    ;;
esac
