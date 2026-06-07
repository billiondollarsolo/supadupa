#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool psql
ensure_token
ensure_profile

api_url="$(profile_value api_url)"
public_db_url="$(profile_value_optional public_database_url)"
if [[ -z "$public_db_url" ]]; then
  fail "fixture.db.public_url" "profile did not include public_database_url"
fi

public_db_safe_url="$(url_without_password "$public_db_url")"
db_password="$(reveal_secret_value db_password)"
anon_key="$(reveal_secret_value anon_key)"
run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
printf '%s' "$run_id" >"$ARTIFACT_DIR/db-fixture-run-id"

if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
  -v ON_ERROR_STOP=1 \
  -v run_id="$run_id" \
  -v project_ref="$SUPADUPA_TEST_REF" \
  >"$ARTIFACT_DIR/db-fixture.out" 2>"$ARTIFACT_DIR/db-fixture.stderr" <<'SQL'
create table if not exists public.compat_runner_probe (
  id text primary key,
  project_ref text not null,
  created_at timestamptz not null default now()
);

alter table public.compat_runner_probe enable row level security;
grant select, insert, update, delete on public.compat_runner_probe to anon, authenticated, service_role;
drop policy if exists compat_runner_probe_select on public.compat_runner_probe;
create policy compat_runner_probe_select on public.compat_runner_probe for select using (true);

insert into public.compat_runner_probe (id, project_ref)
values (:'run_id', :'project_ref')
on conflict (id) do update
set project_ref = excluded.project_ref;

notify pgrst, 'reload schema';
SQL
then
  pass "fixture.db.seed" "compat_runner_probe row inserted"
else
  fail "fixture.db.seed" "psql fixture setup failed; see db-fixture.stderr"
fi

deadline=$((SECONDS + ${SUPADUPA_COMPAT_REST_SCHEMA_TIMEOUT_SECONDS:-90}))
while (( SECONDS < deadline )); do
  set +e
  rest_status="$(curl -sS -o "$ARTIFACT_DIR/db-fixture-rest.body" -w '%{http_code}' \
    -H "apikey: $anon_key" \
    "$api_url/rest/v1/compat_runner_probe?id=eq.$run_id&select=id,project_ref" \
    2>"$ARTIFACT_DIR/db-fixture-rest.stderr")"
  rest_rc="$?"
  set -e

  if [[ "$rest_rc" -eq 0 && "$rest_status" =~ ^2 ]] &&
    grep -q "\"id\":\"$run_id\"" "$ARTIFACT_DIR/db-fixture-rest.body"; then
    pass "fixture.rest.seeded_row" "HTTP $rest_status"
    exit 0
  fi
  sleep 3
done

fail "fixture.rest.seeded_row" "seeded row was not visible through PostgREST before timeout"
