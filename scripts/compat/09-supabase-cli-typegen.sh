#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool npx
ensure_profile

public_db_url="$(profile_value_optional public_database_url)"
if [[ -z "$public_db_url" ]]; then
  fail "supabase_cli.typegen.public_url" "profile did not include public_database_url"
fi

public_db_safe_url="$(url_without_password "$public_db_url")"
db_password="$(reveal_secret_value db_password)"
db_url="$(url_with_password "$public_db_safe_url" "$db_password")"
read -r -a supabase_cmd <<<"${SUPADUPA_SUPABASE_CLI:-npx -y supabase@latest}"

work_dir="$ARTIFACT_DIR/supabase-cli-typegen"
rm -rf "$work_dir"
mkdir -p "$work_dir/supabase"
cat >"$work_dir/supabase/config.toml" <<EOF
project_id = "$SUPADUPA_TEST_REF"
EOF

if (cd "$work_dir" && "${supabase_cmd[@]}" --version) \
  >"$ARTIFACT_DIR/supabase-cli-typegen-version.out" 2>"$ARTIFACT_DIR/supabase-cli-typegen-version.stderr"; then
  version_text="$(tr -d '\r\n' <"$ARTIFACT_DIR/supabase-cli-typegen-version.out")"
  pass "supabase_cli.typegen.version" "$version_text"
else
  fail "supabase_cli.typegen.version" "failed to run Supabase CLI; see supabase-cli-typegen-version.stderr"
fi

types_out="$ARTIFACT_DIR/supabase-cli-official-database.types.ts"
tunnel_pid=""
cleanup_typegen_tunnel() {
  if [[ -n "${tunnel_pid:-}" ]]; then
    kill "$tunnel_pid" >/dev/null 2>&1 || true
    wait "$tunnel_pid" >/dev/null 2>&1 || true
  fi
}
trap cleanup_typegen_tunnel EXIT

if (cd "$work_dir" && "${supabase_cmd[@]}" gen types typescript --db-url "$db_url") \
  >"$types_out" 2>"$ARTIFACT_DIR/supabase-cli-official-gen-types.stderr"; then
  if [[ ! -s "$types_out" ]]; then
    fail "supabase_cli.typegen.official" "official typegen succeeded but output was empty"
  fi
  if ! grep -q "export type Json" "$types_out"; then
    fail "supabase_cli.typegen.official" "official typegen output does not look like Supabase TypeScript types"
  fi
  line_count="$(wc -l <"$types_out" | tr -d ' ')"
  pass "supabase_cli.typegen.official" "official gen types succeeded ($line_count lines)"
else
  if grep -Eiq 'certificate|TLS|SSL|CA|x509|self[- ]signed|unable to get local issuer|unknown authority|does not match' "$ARTIFACT_DIR/supabase-cli-official-gen-types.stderr"; then
    pass "supabase_cli.typegen.official_upstream_tls_caveat" "known BYO-domain TLS failure; see supabase-cli-official-gen-types.stderr"
    if compat_bool "${SUPADUPA_SUPABASE_CLI_TYPEGEN_TUNNEL:-true}"; then
      if ! command -v docker >/dev/null 2>&1; then
        skip "supabase_cli.typegen.official_tunnel" "docker is required to discover host gateway for official CLI container"
        exit 0
      fi
      docker_gateway="$(docker network inspect bridge -f '{{(index .IPAM.Config 0).Gateway}}' 2>"$ARTIFACT_DIR/supabase-cli-typegen-docker-gateway.stderr" || true)"
      if [[ -z "$docker_gateway" ]]; then
        skip "supabase_cli.typegen.official_tunnel" "could not discover Docker bridge gateway"
        exit 0
      fi
      tunnel_ready="$ARTIFACT_DIR/supabase-cli-typegen-tunnel.json"
      rm -f "$tunnel_ready"
      supadupa_cli_authed projects db-tunnel \
        --ref "$SUPADUPA_TEST_REF" \
        --listen "0.0.0.0:0" \
        --advertise-host "$docker_gateway" \
        --ready-file "$tunnel_ready" \
        >"$ARTIFACT_DIR/supabase-cli-typegen-tunnel.out" \
        2>"$ARTIFACT_DIR/supabase-cli-typegen-tunnel.stderr" &
      tunnel_pid="$!"
      deadline=$((SECONDS + 30))
      while (( SECONDS < deadline )); do
        if [[ -s "$tunnel_ready" ]]; then
          break
        fi
        if ! kill -0 "$tunnel_pid" >/dev/null 2>&1; then
          fail "supabase_cli.typegen.official_tunnel.start" "db tunnel exited early; see supabase-cli-typegen-tunnel.stderr"
        fi
        sleep 1
      done
      if [[ ! -s "$tunnel_ready" ]]; then
        fail "supabase_cli.typegen.official_tunnel.start" "db tunnel did not become ready"
      fi
      tunnel_url="$(json_get_file "$tunnel_ready" docker_database_url)"
      if [[ -z "$tunnel_url" ]]; then
        fail "supabase_cli.typegen.official_tunnel.url" "db tunnel did not report docker_database_url"
      fi
      tunnel_db_url="$(url_with_password "$tunnel_url" "$db_password")"
      tunnel_types_out="$ARTIFACT_DIR/supabase-cli-official-tunnel-database.types.ts"
      if (cd "$work_dir" && "${supabase_cmd[@]}" gen types typescript --db-url "$tunnel_db_url") \
        >"$tunnel_types_out" 2>"$ARTIFACT_DIR/supabase-cli-official-tunnel-gen-types.stderr"; then
        if [[ ! -s "$tunnel_types_out" ]]; then
          fail "supabase_cli.typegen.official_tunnel" "official tunnel typegen succeeded but output was empty"
        fi
        if ! grep -q "export type Json" "$tunnel_types_out"; then
          fail "supabase_cli.typegen.official_tunnel" "official tunnel typegen output does not look like Supabase TypeScript types"
        fi
        line_count="$(wc -l <"$tunnel_types_out" | tr -d ' ')"
        pass "supabase_cli.typegen.official_tunnel" "official gen types succeeded through Supadupa DB tunnel ($line_count lines)"
      else
        fail "supabase_cli.typegen.official_tunnel" "official typegen failed through Supadupa DB tunnel; see supabase-cli-official-tunnel-gen-types.stderr"
      fi
    fi
  else
    fail "supabase_cli.typegen.official" "unexpected official typegen failure; see supabase-cli-official-gen-types.stderr"
  fi
fi
