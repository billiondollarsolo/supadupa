#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_EMAIL SUPADUPA_TEST_PASSWORD SUPADUPA_TEST_REF
ensure_token

project_json="$ARTIFACT_DIR/project.json"
project_err="$ARTIFACT_DIR/project.stderr"
if supadupa_cli_authed projects get --ref "$SUPADUPA_TEST_REF" >"$project_json" 2>"$project_err"; then
  runtime_phase="$(json_get_file_optional "$project_json" runtime_phase)"
  if [[ -n "$runtime_phase" && "$runtime_phase" != "healthy" ]]; then
    fail "project.inspect" "runtime_phase=$runtime_phase"
  fi
  pass "project.inspect" "project metadata saved"
else
  fail "project.inspect" "failed to fetch project; see project.stderr"
fi

profile_json="$ARTIFACT_DIR/profile.json"
profile_err="$ARTIFACT_DIR/profile.stderr"
if supadupa_cli_authed projects cli-profile \
  --ref "$SUPADUPA_TEST_REF" \
  --format json >"$profile_json" 2>"$profile_err"; then
  pass "project.cli_profile" "CLI profile saved"
else
  fail "project.cli_profile" "failed to fetch CLI profile; see profile.stderr"
fi

api_url="$(json_get_file_optional "$profile_json" api_url)"
if [[ -z "$api_url" ]]; then
  fail "project.api_url" "profile did not include api_url"
fi
if [[ "$api_url" != https://* ]]; then
  fail "project.api_url" "api_url is not HTTPS"
fi
if [[ "$api_url" == *localhost* || "$api_url" == *127.0.0.1* ]]; then
  fail "project.api_url" "api_url is not public"
fi
pass "project.api_url" "$api_url"

public_db_url="$(json_get_file_optional "$profile_json" public_database_url)"
if [[ -z "$public_db_url" ]]; then
  fail "project.public_database_url" "profile did not include public_database_url"
fi

public_db_safe_url="$(url_without_password "$public_db_url")"
public_db_host="$(url_host "$public_db_safe_url")"
if ! is_public_host "$public_db_host"; then
  fail "project.public_database_url" "public database host is not public"
fi
pass "project.public_database_url" "$public_db_host"
