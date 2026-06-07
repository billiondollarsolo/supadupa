#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool node
require_tool npx
ensure_profile

read -r -a supabase_cmd <<<"${SUPADUPA_SUPABASE_CLI:-npx -y supabase@latest}"
work_dir="$ARTIFACT_DIR/supabase-cli-classification"
rm -rf "$work_dir"
mkdir -p "$work_dir"

if (cd "$work_dir" && "${supabase_cmd[@]}" --version) \
  >"$ARTIFACT_DIR/supabase-cli-classification-version.out" 2>"$ARTIFACT_DIR/supabase-cli-classification-version.stderr"; then
  version_text="$(tr -d '\r\n' <"$ARTIFACT_DIR/supabase-cli-classification-version.out")"
  pass "supabase_cli_classification.version" "$version_text"
else
  fail "supabase_cli_classification.version" "failed to run Supabase CLI; see supabase-cli-classification-version.stderr"
fi

if (cd "$work_dir" && "${supabase_cmd[@]}" --help) \
  >"$ARTIFACT_DIR/supabase-cli-help.out" 2>"$ARTIFACT_DIR/supabase-cli-help.stderr"; then
  pass "supabase_cli_classification.help" "saved supabase-cli-help.out"
else
  fail "supabase_cli_classification.help" "failed to capture Supabase CLI help; see supabase-cli-help.stderr"
fi

for family in db projects branches secrets functions link; do
  if (cd "$work_dir" && "${supabase_cmd[@]}" "$family" --help) \
    >"$ARTIFACT_DIR/supabase-cli-help-$family.out" 2>"$ARTIFACT_DIR/supabase-cli-help-$family.stderr"; then
    pass "supabase_cli_classification.help.$family" "saved supabase-cli-help-$family.out"
  else
    skip "supabase_cli_classification.help.$family" "help unavailable; see supabase-cli-help-$family.stderr"
  fi
done

public_db_url="$(profile_value_optional public_database_url)"
db_mode="unavailable"
db_classification="skip-db-route-required"
if [[ -n "$public_db_url" ]]; then
  public_db_safe_url="$(url_without_password "$public_db_url")"
  public_db_host="$(url_host "$public_db_safe_url")"
  if is_public_host "$public_db_host"; then
    db_mode="public-tcp"
    db_classification="pass-with-db-route"
  elif [[ "$public_db_host" == 127.* || "$public_db_host" == "localhost" ]]; then
    db_mode="tunnel"
    db_classification="pass-with-local-tunnel"
  else
    db_mode="internal"
    db_classification="inside-runtime-network-only"
  fi
fi

matrix_json="$ARTIFACT_DIR/supabase-cli-classification.json"
node -e '
const fs = require("fs");
const out = process.argv[1];
const projectRef = process.argv[2];
const dbMode = process.argv[3];
const dbClassification = process.argv[4];
const matrix = {
  project_ref: projectRef,
  db_access_mode: dbMode,
  command_families: {
    "supabase db * --db-url": dbClassification,
    "supabase projects *": "supadupa-wrapper",
    "supabase link": "supadupa-wrapper",
    "supabase branches *": "supadupa-wrapper",
    "supabase secrets *": "supadupa-wrapper",
    "supabase functions deploy": "supadupa-wrapper"
  },
  notes: {
    data_plane: "Official Supabase CLI database workflows may run when a reachable db-url is available.",
    management_plane: "Supabase Cloud management commands are replaced by supadupa-cli Management API commands."
  }
};
fs.writeFileSync(out, JSON.stringify(matrix, null, 2) + "\n", {mode: 0o600});
' "$matrix_json" "$SUPADUPA_TEST_REF" "$db_mode" "$db_classification"

if node -e '
const fs = require("fs");
const matrix = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const required = [
  "supabase db * --db-url",
  "supabase projects *",
  "supabase link",
  "supabase branches *",
  "supabase secrets *",
  "supabase functions deploy",
];
for (const key of required) {
  if (!matrix.command_families[key]) throw new Error(`${key} missing`);
}
if (!["public-tcp", "tunnel", "internal", "unavailable"].includes(matrix.db_access_mode)) {
  throw new Error(`unknown db_access_mode ${matrix.db_access_mode}`);
}
' "$matrix_json" >"$ARTIFACT_DIR/supabase-cli-classification-check.out" 2>"$ARTIFACT_DIR/supabase-cli-classification-check.stderr"; then
  pass "supabase_cli_classification.matrix" "db_access_mode=$db_mode"
else
  fail "supabase_cli_classification.matrix" "classification matrix failed; see supabase-cli-classification-check.stderr"
fi
