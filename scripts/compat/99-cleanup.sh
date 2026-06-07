#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

if ! compat_bool "${SUPADUPA_COMPAT_CREATE_PROJECT:-false}"; then
  skip "project.cleanup" "SUPADUPA_COMPAT_CREATE_PROJECT is not enabled"
  exit 0
fi

if compat_bool "${SUPADUPA_COMPAT_KEEP_PROJECT:-false}"; then
  skip "project.cleanup" "SUPADUPA_COMPAT_KEEP_PROJECT is enabled"
  exit 0
fi

if [[ ! -s "$ARTIFACT_DIR/created-project" ]]; then
  skip "project.cleanup" "no created-project marker found"
  exit 0
fi

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
ensure_token

destroy_args=(projects destroy --ref "$SUPADUPA_TEST_REF" --yes)
if compat_bool "${SUPADUPA_COMPAT_RETAIN_VOLUMES:-false}"; then
  destroy_args+=(--retain-volumes)
fi

if supadupa_cli_authed "${destroy_args[@]}" >"$ARTIFACT_DIR/cleanup-project.json" 2>"$ARTIFACT_DIR/cleanup-project.stderr"; then
  rm -f "$ARTIFACT_DIR/created-project"
  remove_cached_project_material
  pass "project.cleanup" "$SUPADUPA_TEST_REF destroyed"
else
  fail "project.cleanup" "project destroy failed; see cleanup-project.stderr"
fi
