#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool node
ensure_token

profile_json="$ARTIFACT_DIR/profile.json"
profile_err="$ARTIFACT_DIR/profile.stderr"
if supadupa_cli_authed projects cli-profile \
  --ref "$SUPADUPA_TEST_REF" \
  --format json >"$profile_json" 2>"$profile_err"; then
  pass "cli_profile.json" "profile saved"
else
  fail "cli_profile.json" "failed to fetch CLI profile; see profile.stderr"
fi

if node -e '
const fs = require("fs");
const profile = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const required = [
  "project_ref",
  "api_url",
  "studio_url",
  "rest_url",
  "auth_url",
  "graphql_url",
  "realtime_url",
  "functions_url",
  "storage_url",
  "storage_s3_url",
  "database_url",
  "pooler_transaction_url",
  "pooler_session_url",
  "env",
  "supabase_config_toml",
  "commands",
  "secret_handles",
  "compatibility_contracts",
];
for (const key of required) {
  if (profile[key] == null || profile[key] === "") throw new Error(`${key} is missing`);
}
if (!String(profile.api_url).startsWith("https://")) throw new Error("api_url is not HTTPS");
if (!String(profile.studio_url).startsWith("https://")) throw new Error("studio_url is not HTTPS");
if (!String((profile.commands || {}).supadupa_gen_types || "").includes(`projects gen-types --ref ${profile.project_ref}`)) {
  throw new Error("supadupa_gen_types command missing or wrong ref");
}
if (!String((profile.commands || {}).supadupa_env_reveal || "").includes(`projects env --ref ${profile.project_ref} --reveal-secrets --out .supadupa/supabase.env`)) {
  throw new Error("supadupa_env_reveal command missing or wrong ref");
}
if (!String((profile.commands || {}).supabase_db_push_env || "").includes(`supabase db push --db-url "$SUPABASE_DB_URL"`)) {
  throw new Error("supabase_db_push_env command missing");
}
if (!String((profile.compatibility_contracts || {}).typegen || "").includes("supadupa-cli projects gen-types")) {
  throw new Error("typegen compatibility contract missing");
}
const allowedLocalOrInternal = (path) =>
  path.includes("local_") ||
  path.endsWith(".supabase_local_env") ||
  path.includes("internal_") ||
  path.endsWith(".SUPADUPA_INTERNAL_DB_URL") ||
  path.includes(".psql_internal_") ||
  path.includes(".supabase_local_") ||
  path.endsWith(".supabase_typegen_tunnel");
const badPublicValues = [];
const walk = (value, path) => {
  if (typeof value === "string") {
    if (!allowedLocalOrInternal(path) && /(localhost|127\.0\.0\.1|host\.docker\.internal|\.internal)(?::|\/|$)/.test(value)) {
      badPublicValues.push(`${path}=${value}`);
    }
    return;
  }
  if (Array.isArray(value)) {
    value.forEach((item, index) => walk(item, `${path}[${index}]`));
    return;
  }
  if (value && typeof value === "object") {
    Object.entries(value).forEach(([key, item]) => walk(item, path ? `${path}.${key}` : key));
  }
};
walk({
  api_url: profile.api_url,
  studio_url: profile.studio_url,
  rest_url: profile.rest_url,
  auth_url: profile.auth_url,
  graphql_url: profile.graphql_url,
  realtime_url: profile.realtime_url,
  functions_url: profile.functions_url,
  storage_url: profile.storage_url,
  storage_s3_url: profile.storage_s3_url,
  database_url: profile.database_url,
  pooler_transaction_url: profile.pooler_transaction_url,
  pooler_session_url: profile.pooler_session_url,
  public_database_url: profile.public_database_url,
  public_pooler_url: profile.public_pooler_url,
  env: profile.env,
  commands: profile.commands,
}, "");
for (const [key, value] of Object.entries(profile.links || {})) {
  if (!key.endsWith("_local")) walk(value, `links.${key}`);
}
if (badPublicValues.length) throw new Error(`public profile contains local/internal values: ${badPublicValues.join("; ")}`);
' "$profile_json" >"$ARTIFACT_DIR/cli-profile-json.out" 2>"$ARTIFACT_DIR/cli-profile-json.stderr"; then
  pass "cli_profile.json_shape" "required fields present"
else
  fail "cli_profile.json_shape" "profile shape failed; see cli-profile-json.stderr"
fi
pass "cli_profile.typegen_command" "supadupa gen-types command exposed in profile"

connect_json="$ARTIFACT_DIR/connect.json"
connect_err="$ARTIFACT_DIR/connect.stderr"
if supadupa_cli_authed projects connect \
  --ref "$SUPADUPA_TEST_REF" >"$connect_json" 2>"$connect_err"; then
  pass "connect_payload.json" "connect payload saved"
else
  fail "connect_payload.json" "failed to fetch connect payload; see connect.stderr"
fi

if node -e '
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const ref = process.argv[2];
const appsDomain = process.env.SUPADUPA_APPS_DOMAIN || "";
const publicDBHost = `db-${ref}.${appsDomain}`;
const publicPoolerHost = `pooler-${ref}.${appsDomain}`;
const publicStorageHost = `storage-${ref}.${appsDomain}`;
const required = [
  "api_url",
  "studio_url",
  "rest_url",
  "auth_url",
  "graphql_url",
  "realtime_url",
  "functions_url",
  "storage_url",
  "storage_s3_url",
  "postgres",
  "postgres_parts",
  "connection_snippets",
  "secret_handles",
];
for (const key of required) {
  if (payload[key] == null || payload[key] === "") throw new Error(`${key} is missing`);
}
if (appsDomain) {
  const expectedHosts = {
    "postgres.public_direct": publicDBHost,
    "postgres.public_transaction": publicPoolerHost,
    "postgres.public_session": publicPoolerHost,
    "postgres_parts.public_direct.host": publicDBHost,
    "postgres_parts.public_transaction.host": publicPoolerHost,
    "postgres_parts.public_session.host": publicPoolerHost,
    "storage_s3_url": publicStorageHost,
  };
  for (const [path, host] of Object.entries(expectedHosts)) {
    const value = path.split(".").reduce((current, key) => current && current[key], payload);
    if (!String(value || "").includes(host)) throw new Error(`${path} does not include ${host}: ${value}`);
  }
}
const restDocs = String((payload.links || {}).rest_docs || "");
const graphqlExplorer = String((payload.links || {}).graphql_explorer || "");
if (!restDocs.includes(`/project/${ref}/api`)) {
  throw new Error(`rest_docs is not project-ref scoped: ${restDocs}`);
}
if (!graphqlExplorer.includes(`/project/${ref}/api?panel=graphql`)) {
  throw new Error(`graphql_explorer is not project-ref scoped: ${graphqlExplorer}`);
}
for (const [key, value] of Object.entries(payload.connection_snippets || {})) {
  if (key.includes("internal") || key.includes("local")) continue;
  if (/(localhost|127\.0\.0\.1|host\.docker\.internal|\.internal)(?::|\/|$)/.test(String(value))) {
    throw new Error(`public connection snippet ${key} is not remote-safe: ${value}`);
  }
}
for (const key of ["uri_direct", "uri_pool_transaction", "uri_pool_session", "psql_direct", "psql_pool_transaction", "psql_pool_session"]) {
  const value = String((payload.connection_snippets || {})[key] || "");
  if (!value) throw new Error(`${key} snippet missing`);
  if (!value.includes("?sslmode=require")) throw new Error(`${key} does not require TLS: ${value}`);
  if (appsDomain && !value.includes(appsDomain)) throw new Error(`${key} does not use the public apps domain: ${value}`);
}
' "$connect_json" "$SUPADUPA_TEST_REF" >"$ARTIFACT_DIR/connect-json.out" 2>"$ARTIFACT_DIR/connect-json.stderr"; then
  pass "connect_payload.remote_safe" "public snippets and parts are remote-safe"
else
  fail "connect_payload.remote_safe" "connect payload remote-safety check failed; see connect-json.stderr"
fi

env_file="$ARTIFACT_DIR/supabase.env"
env_err="$ARTIFACT_DIR/supabase-env.stderr"
if supadupa_cli_authed projects cli-profile \
  --ref "$SUPADUPA_TEST_REF" \
  --format env >"$env_file" 2>"$env_err"; then
  pass "cli_profile.env" "env export saved"
else
  fail "cli_profile.env" "env export failed; see supabase-env.stderr"
fi

if node -e '
const fs = require("fs");
const body = fs.readFileSync(process.argv[1], "utf8");
for (const key of ["SUPABASE_URL=", "SUPABASE_DB_URL=", "SUPABASE_ANON_KEY=", "SUPABASE_SERVICE_ROLE_KEY="]) {
  if (!body.includes(key)) throw new Error(`${key} missing`);
}
if (!body.includes("secret://projects/")) throw new Error("secret handles missing");
if (body.includes("eyJ")) throw new Error("env export leaked JWT-looking material");
' "$env_file" >"$ARTIFACT_DIR/cli-profile-env.out" 2>"$ARTIFACT_DIR/cli-profile-env.stderr"; then
  pass "cli_profile.env_handles" "handle-only env"
else
  fail "cli_profile.env_handles" "env export failed handle/leak check; see cli-profile-env.stderr"
fi

set +u
set -a
# shellcheck disable=SC1090
source "$env_file"
set +a
set -u
if [[ -n "${SUPABASE_URL:-}" && -n "${SUPABASE_DB_URL:-}" ]]; then
  pass "cli_profile.env_sourceable" "SUPABASE_URL and SUPABASE_DB_URL loaded"
else
  fail "cli_profile.env_sourceable" "env file did not source cleanly"
fi

toml_dir="$ARTIFACT_DIR/supabase"
toml_file="$toml_dir/config.toml"
toml_err="$ARTIFACT_DIR/supabase-toml.stderr"
mkdir -p "$toml_dir"
if supadupa_cli_authed projects cli-profile \
  --ref "$SUPADUPA_TEST_REF" \
  --format toml >"$toml_file" 2>"$toml_err"; then
  pass "cli_profile.toml" "TOML export saved"
else
  fail "cli_profile.toml" "TOML export failed; see supabase-toml.stderr"
fi

if node -e '
const fs = require("fs");
const body = fs.readFileSync(process.argv[1], "utf8");
if (!body.includes(`project_id = "${process.argv[2]}"`)) throw new Error("project_id mismatch");
if (!body.includes("[supadupa]")) throw new Error("[supadupa] missing");
if (!body.includes("[supadupa.secret_handles]")) throw new Error("[supadupa.secret_handles] missing");
' "$toml_file" "$SUPADUPA_TEST_REF" >"$ARTIFACT_DIR/cli-profile-toml.out" 2>"$ARTIFACT_DIR/cli-profile-toml.stderr"; then
  pass "cli_profile.toml_shape" "project metadata present"
else
  fail "cli_profile.toml_shape" "TOML export failed shape check; see cli-profile-toml.stderr"
fi

workspace="$ARTIFACT_DIR/workspace"
link_err="$ARTIFACT_DIR/project-link.stderr"
rm -rf "$workspace"
mkdir -p "$workspace"
if supadupa_cli_authed projects link \
  --ref "$SUPADUPA_TEST_REF" \
  --dir "$workspace" >"$ARTIFACT_DIR/project-link.stdout" 2>"$link_err"; then
  pass "cli_profile.link" "workspace linked"
else
  fail "cli_profile.link" "workspace link failed; see project-link.stderr"
fi

for file in project.json config.toml supabase.env; do
  if [[ ! -s "$workspace/.supadupa/$file" ]]; then
    fail "cli_profile.link.$file" "missing .supadupa/$file"
  fi
done
pass "cli_profile.link_files" ".supadupa project.json, config.toml, supabase.env present"

if [[ ! -s "$workspace/supabase/config.toml" ]]; then
  fail "cli_profile.link.supabase_config" "missing supabase/config.toml for official Supabase CLI workspace commands"
fi
pass "cli_profile.link.supabase_config" "supabase/config.toml present"

if node -e '
const fs = require("fs");
const binding = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const env = fs.readFileSync(process.argv[2], "utf8");
const supabaseConfig = fs.readFileSync(process.argv[4], "utf8");
if (binding.project_ref !== process.argv[3]) throw new Error("binding project_ref mismatch");
if (binding.secrets_revealed !== false) throw new Error("binding should be handle-only by default");
if (!String(binding.api_url).startsWith("https://")) throw new Error("binding api_url is not HTTPS");
if (!env.includes("secret://projects/")) throw new Error("linked env missing secret handles");
if (env.includes("eyJ")) throw new Error("linked env leaked JWT-looking material");
if (!supabaseConfig.includes(`project_id = "${process.argv[3]}"`)) throw new Error("supabase/config.toml project_id mismatch");
if (!supabaseConfig.includes("[supadupa]")) throw new Error("supabase/config.toml missing [supadupa] metadata");
const badBinding = [];
for (const [key, value] of Object.entries(binding)) {
  if (key.startsWith("local_") || key.startsWith("internal_")) continue;
  if (typeof value === "string" && /(localhost|127\.0\.0\.1|host\.docker\.internal|\.internal)(?::|\/|$)/.test(value)) {
    badBinding.push(`${key}=${value}`);
  }
}
for (const line of env.split(/\r?\n/)) {
  if (/^(SUPADUPA_LOCAL_|SUPABASE_LOCAL_|SUPADUPA_INTERNAL_)/.test(line)) continue;
  if (/(localhost|127\.0\.0\.1|host\.docker\.internal|\.internal)(?::|\/|$)/.test(line)) {
    badBinding.push(line);
  }
}
if (badBinding.length) throw new Error(`workspace binding contains local/internal values: ${badBinding.join("; ")}`);
' "$workspace/.supadupa/project.json" "$workspace/.supadupa/supabase.env" "$SUPADUPA_TEST_REF" "$workspace/supabase/config.toml" >"$ARTIFACT_DIR/project-link-check.out" 2>"$ARTIFACT_DIR/project-link-check.stderr"; then
  pass "cli_profile.link_handles" "workspace binding is handle-only"
else
  fail "cli_profile.link_handles" "workspace binding check failed; see project-link-check.stderr"
fi

revealed_env="$ARTIFACT_DIR/supabase.revealed.env"
revealed_err="$ARTIFACT_DIR/supabase-revealed-env.stderr"
if supadupa_cli_authed projects env \
  --ref "$SUPADUPA_TEST_REF" \
  --reveal-secrets \
  --out "$revealed_env" >"$ARTIFACT_DIR/supabase-revealed-env.stdout" 2>"$revealed_err"; then
  pass "cli_profile.env_reveal" "revealed env materialized"
else
  fail "cli_profile.env_reveal" "revealed env failed; see supabase-revealed-env.stderr"
fi

if node -e '
const fs = require("fs");
const body = fs.readFileSync(process.argv[1], "utf8");
if (body.includes("secret://projects/")) throw new Error("revealed env still contains secret handles");
if (body.includes("${DB_PASSWORD}") || body.includes("$DB_PASSWORD")) throw new Error("revealed env still contains DB password placeholder");
for (const key of ["SUPABASE_ANON_KEY=", "SUPABASE_SERVICE_ROLE_KEY=", "SUPABASE_DB_PASSWORD=", "SUPABASE_DB_URL="]) {
  if (!body.includes(key)) throw new Error(`${key} missing`);
}
' "$revealed_env" >"$ARTIFACT_DIR/supabase-revealed-env-check.out" 2>"$ARTIFACT_DIR/supabase-revealed-env-check.stderr"; then
  pass "cli_profile.env_reveal_materialized" "secrets and DB URLs materialized"
else
  fail "cli_profile.env_reveal_materialized" "revealed env check failed; see supabase-revealed-env-check.stderr"
fi
