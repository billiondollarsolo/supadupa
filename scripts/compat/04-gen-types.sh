#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool docker
ensure_token
ensure_profile

types_out="$ARTIFACT_DIR/database.types.ts"
types_stdout="$ARTIFACT_DIR/gen-types.stdout"
types_stderr="$ARTIFACT_DIR/gen-types.stderr"

if supadupa_cli_authed projects gen-types \
  --ref "$SUPADUPA_TEST_REF" \
  --out "$types_out" >"$types_stdout" 2>"$types_stderr"; then
  if [[ ! -s "$types_out" ]]; then
    fail "cli.gen_types" "typegen succeeded but output file is empty"
  fi
	  if ! grep -q "export type Json" "$types_out"; then
	    fail "cli.gen_types" "generated file does not look like Supabase TypeScript types"
	  fi
	  expected_table="${SUPADUPA_COMPAT_TYPEGEN_EXPECT_TABLE:-compat_cli_probe}"
	  if [[ -n "$expected_table" ]]; then
	    if grep -q "${expected_table}:" "$types_out"; then
	      pass "cli.gen_types.table.$expected_table" "table present in generated types"
	    else
	      fail "cli.gen_types.table.$expected_table" "generated types did not include $expected_table"
	    fi
	  fi
	  line_count="$(wc -l <"$types_out" | tr -d ' ')"
	  pass "cli.gen_types" "generated database.types.ts ($line_count lines)"
else
  fail "cli.gen_types" "projects gen-types failed; see gen-types.stderr"
fi
