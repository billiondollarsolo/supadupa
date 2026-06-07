#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

if ! compat_bool "${SUPADUPA_COMPAT_CREATE_PROJECT:-false}"; then
  skip "project.create" "SUPADUPA_COMPAT_CREATE_PROJECT is not enabled"
  exit 0
fi

require_env SUPADUPA_API_URL SUPADUPA_TEST_EMAIL SUPADUPA_TEST_PASSWORD SUPADUPA_TEST_REF SUPADUPA_TEST_ORG_ID
ensure_token
remove_cached_project_material

existing_json="$ARTIFACT_DIR/create-existing.json"
existing_err="$ARTIFACT_DIR/create-existing.stderr"
if supadupa_cli_authed projects get --ref "$SUPADUPA_TEST_REF" >"$existing_json" 2>"$existing_err"; then
  fail "project.create" "project $SUPADUPA_TEST_REF already exists"
fi

create_args=(
  projects create
  --org-id "$SUPADUPA_TEST_ORG_ID"
  --ref "$SUPADUPA_TEST_REF"
  --name "${SUPADUPA_TEST_NAME:-Compatibility Test $SUPADUPA_TEST_REF}"
)

if [[ -n "${SUPADUPA_APPS_DOMAIN:-}" ]]; then
  create_args+=(--domain "$SUPADUPA_APPS_DOMAIN")
fi
if [[ -n "${SUPADUPA_STACK_VERSION:-}" ]]; then
  create_args+=(--stack-version "$SUPADUPA_STACK_VERSION")
fi
if [[ -n "${SUPADUPA_STACK_PROFILE:-full}" ]]; then
  create_args+=(--profile "${SUPADUPA_STACK_PROFILE:-full}")
fi
if [[ -n "${SUPADUPA_RESOURCE_TIER:-small}" ]]; then
  create_args+=(--tier "${SUPADUPA_RESOURCE_TIER:-small}")
fi
if [[ -n "${SUPADUPA_HOST_ID:-}" ]]; then
  create_args+=(--host-id "$SUPADUPA_HOST_ID")
fi

if supadupa_cli_authed "${create_args[@]}" >"$ARTIFACT_DIR/create-project.json" 2>"$ARTIFACT_DIR/create-project.stderr"; then
  printf '%s\n' "$SUPADUPA_TEST_REF" >"$ARTIFACT_DIR/created-project"
  pass "project.create" "project create response saved"
else
  fail "project.create" "project create failed; see create-project.stderr"
fi

deadline=$((SECONDS + ${SUPADUPA_COMPAT_CREATE_TIMEOUT_SECONDS:-360}))
while (( SECONDS < deadline )); do
  rm -f "$ARTIFACT_DIR/profile.json"
  if supadupa_cli_authed projects cli-profile --ref "$SUPADUPA_TEST_REF" --format json >"$ARTIFACT_DIR/profile.json" 2>"$ARTIFACT_DIR/profile.stderr"; then
    api_url="$(json_get_file_optional "$ARTIFACT_DIR/profile.json" api_url)"
    if [[ -n "$api_url" ]]; then
      if curl -fsS "$api_url/auth/v1/health" >"$ARTIFACT_DIR/create-auth-health.json" 2>"$ARTIFACT_DIR/create-auth-health.stderr"; then
        pass "project.create_public_ready" "$api_url"
        exit 0
      fi
    fi
  fi
  sleep 5
done

fail "project.create_public_ready" "project did not become publicly reachable before timeout"
