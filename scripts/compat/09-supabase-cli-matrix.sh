#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool npx
require_tool node

matrix="${SUPADUPA_SUPABASE_CLI_MATRIX:-}"
if [[ -z "$matrix" ]]; then
  skip "supabase_cli_matrix.config" "set SUPADUPA_SUPABASE_CLI_MATRIX='latest 2.105.0' to validate multiple official CLI versions"
  exit 0
fi

base_artifact_dir="$ARTIFACT_DIR"
base_results_file="$RESULTS_FILE"

for version in $matrix; do
  case "$version" in
    latest)
      cli_spec="npx -y supabase@latest"
      ;;
    supabase@*|npx\ *)
      cli_spec="$version"
      ;;
    *)
      cli_spec="npx -y supabase@$version"
      ;;
  esac

  safe_version="$(printf '%s' "$version" | tr -c 'A-Za-z0-9._-' '_')"
  export SUPADUPA_SUPABASE_CLI="$cli_spec"
  export SUPADUPA_COMPAT_ARTIFACT_ROOT="$base_artifact_dir/supabase-cli-matrix/$safe_version"

  for phase in \
    "$SCRIPT_DIR/09-supabase-cli-classification.sh" \
    "$SCRIPT_DIR/09-supabase-cli-db.sh" \
    "$SCRIPT_DIR/09-supabase-cli-typegen.sh" \
    "$SCRIPT_DIR/04-gen-types.sh"; do
    "$phase"
  done

  matrix_result_dir="$SUPADUPA_COMPAT_ARTIFACT_ROOT/$SUPADUPA_TEST_REF"
  if [[ -s "$matrix_result_dir/results.jsonl" ]]; then
    while IFS= read -r line; do
      node -e '
const payload = JSON.parse(process.argv[1]);
payload.test = `supabase_cli_matrix.${process.argv[2]}.${payload.test}`;
process.stdout.write(JSON.stringify(payload) + "\n");
' "$line" "$safe_version" >>"$base_results_file"
    done <"$matrix_result_dir/results.jsonl"
  fi
  pass "supabase_cli_matrix.$safe_version" "$cli_spec"
done

ARTIFACT_DIR="$base_artifact_dir"
RESULTS_FILE="$base_results_file"
pass "supabase_cli_matrix.complete" "versions: $matrix"
