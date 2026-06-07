#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool node
require_tool npx
require_tool psql
ensure_token
ensure_profile

public_db_url="$(profile_value_optional public_database_url)"
if [[ -z "$public_db_url" ]]; then
  fail "supabase_cli.db.public_url" "profile did not include public_database_url"
fi

public_db_safe_url="$(url_without_password "$public_db_url")"
public_db_host="$(url_host "$public_db_safe_url")"
if ! is_public_host "$public_db_host"; then
  fail "supabase_cli.db.public_url" "public database host is not public"
fi
pass "supabase_cli.db.public_url" "$public_db_host"

db_password="$(reveal_secret_value db_password)"
db_url="$(url_with_password "$public_db_safe_url" "$db_password")"
anon_key="$(reveal_secret_value anon_key)"
api_url="$(profile_value api_url)"
run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
migration_version="$(date -u +%Y%m%d%H%M%S)"
repair_version="$(date -u -d '+1 minute' +%Y%m%d%H%M%S)"
work_dir="$ARTIFACT_DIR/supabase-cli-db"
migrations_dir="$work_dir/supabase/migrations"
rm -rf "$work_dir"
mkdir -p "$migrations_dir"
printf '%s' "$run_id" >"$ARTIFACT_DIR/supabase-cli-db-run-id"

cleanup_repair_probe() {
  if [[ -n "${db_password:-}" && -n "${public_db_safe_url:-}" && -n "${repair_version:-}" ]]; then
    PGPASSWORD="$db_password" psql "$public_db_safe_url" \
      -v ON_ERROR_STOP=1 \
      -v version="$repair_version" \
      -q >"$ARTIFACT_DIR/supabase-cli-migration-repair-cleanup.out" 2>"$ARTIFACT_DIR/supabase-cli-migration-repair-cleanup.stderr" <<'SQL' || true
delete from supabase_migrations.schema_migrations
where version = :'version';
SQL
  fi
}
trap cleanup_repair_probe EXIT

cat >"$work_dir/supabase/config.toml" <<EOF
project_id = "$SUPADUPA_TEST_REF"
EOF

remote_versions="$ARTIFACT_DIR/supabase-cli-remote-migrations.txt"
if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
  -v ON_ERROR_STOP=1 \
  -Atqc "select version from supabase_migrations.schema_migrations order by version" \
  >"$remote_versions" 2>"$ARTIFACT_DIR/supabase-cli-remote-migrations.stderr"; then
  while IFS= read -r version; do
    if [[ "$version" =~ ^[0-9]+$ ]]; then
      printf -- '-- remote migration placeholder for %s\n' "$version" \
        >"$migrations_dir/${version}_remote_history_placeholder.sql"
    fi
  done <"$remote_versions"
else
  : >"$remote_versions"
fi

cat >"$migrations_dir/${migration_version}_supadupa_compat_cli_probe.sql" <<'SQL'
create schema if not exists compat;

create table if not exists compat.cli_probe (
  id uuid primary key default gen_random_uuid(),
  inserted_at timestamptz not null default now(),
  note text not null
);

create table if not exists public.compat_cli_probe (
  id text primary key,
  project_ref text not null,
  created_at timestamptz not null default now()
);

alter table public.compat_cli_probe enable row level security;
grant select on public.compat_cli_probe to anon, authenticated, service_role;
drop policy if exists compat_cli_probe_select on public.compat_cli_probe;
create policy compat_cli_probe_select on public.compat_cli_probe for select using (true);
SQL

cat >>"$migrations_dir/${migration_version}_supadupa_compat_cli_probe.sql" <<SQL
insert into compat.cli_probe (note)
values ('supadupa compat cli migration $run_id');

insert into public.compat_cli_probe (id, project_ref)
values ('$run_id', '$SUPADUPA_TEST_REF')
on conflict (id) do update
set project_ref = excluded.project_ref;

notify pgrst, 'reload schema';
SQL

read -r -a supabase_cmd <<<"${SUPADUPA_SUPABASE_CLI:-npx -y supabase@latest}"

if (cd "$work_dir" && "${supabase_cmd[@]}" --version) \
  >"$ARTIFACT_DIR/supabase-cli-version.out" 2>"$ARTIFACT_DIR/supabase-cli-version.stderr"; then
  version_text="$(tr -d '\r\n' <"$ARTIFACT_DIR/supabase-cli-version.out")"
  pass "supabase_cli.version" "$version_text"
else
  fail "supabase_cli.version" "failed to run Supabase CLI; see supabase-cli-version.stderr"
fi

if (cd "$work_dir" && "${supabase_cmd[@]}" db push --db-url "$db_url" --yes) \
  >"$ARTIFACT_DIR/supabase-cli-db-push.out" 2>"$ARTIFACT_DIR/supabase-cli-db-push.stderr"; then
  pass "supabase_cli.db_push" "migration $migration_version applied"
else
  fail "supabase_cli.db_push" "supabase db push failed; see supabase-cli-db-push.stderr"
fi

if (cd "$work_dir" && "${supabase_cmd[@]}" db push --db-url "$db_url" --yes) \
  >"$ARTIFACT_DIR/supabase-cli-db-push-rerun.out" 2>"$ARTIFACT_DIR/supabase-cli-db-push-rerun.stderr"; then
  pass "supabase_cli.db_push_noop_rerun" "migration history stable"
else
  fail "supabase_cli.db_push_noop_rerun" "supabase db push rerun failed; see supabase-cli-db-push-rerun.stderr"
fi

if (cd "$work_dir" && "${supabase_cmd[@]}" migration list --db-url "$db_url") \
  >"$ARTIFACT_DIR/supabase-cli-migration-list.out" 2>"$ARTIFACT_DIR/supabase-cli-migration-list.stderr"; then
  if ! grep -q "$migration_version" "$ARTIFACT_DIR/supabase-cli-migration-list.out"; then
    fail "supabase_cli.migration_list" "migration list did not include $migration_version"
  fi
  pass "supabase_cli.migration_list" "remote migration history listed"
else
  fail "supabase_cli.migration_list" "supabase migration list failed; see supabase-cli-migration-list.stderr"
fi

if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
  -v ON_ERROR_STOP=1 \
  -v run_id="$run_id" \
  -Atq \
  >"$ARTIFACT_DIR/supabase-cli-db-push-verify.out" 2>"$ARTIFACT_DIR/supabase-cli-db-push-verify.stderr" <<'SQL'
select count(*) from compat.cli_probe
where note = 'supadupa compat cli migration ' || :'run_id';
SQL
then
  count="$(tr -d '\r\n' <"$ARTIFACT_DIR/supabase-cli-db-push-verify.out")"
  if [[ "$count" != "1" ]]; then
    fail "supabase_cli.db_push_verify" "expected one compat.cli_probe row, got $count"
  fi
  pass "supabase_cli.db_push_verify" "compat.cli_probe row found"
else
  fail "supabase_cli.db_push_verify" "psql verification failed; see supabase-cli-db-push-verify.stderr"
fi

pull_before="$(find "$migrations_dir" -maxdepth 1 -type f | wc -l | tr -d ' ')"
if (cd "$work_dir" && "${supabase_cmd[@]}" db pull supadupa_compat_pull \
  --db-url "$db_url" \
  --schema public,compat \
  --yes) >"$ARTIFACT_DIR/supabase-cli-db-pull.out" 2>"$ARTIFACT_DIR/supabase-cli-db-pull.stderr"; then
  pull_after="$(find "$migrations_dir" -maxdepth 1 -type f | wc -l | tr -d ' ')"
  if [[ "$pull_after" -le "$pull_before" ]]; then
    fail "supabase_cli.db_pull" "db pull did not create a migration file"
  fi
  if ! grep -R -qE 'compat_cli_probe|cli_probe' "$migrations_dir"; then
    fail "supabase_cli.db_pull" "pulled migration did not include compat schema/table"
  fi
  pass "supabase_cli.db_pull" "remote schema pulled"
else
  fail "supabase_cli.db_pull" "supabase db pull failed; see supabase-cli-db-pull.stderr"
fi

diff_attempts="${SUPADUPA_COMPAT_CLI_DB_DIFF_ATTEMPTS:-2}"
if ! [[ "$diff_attempts" =~ ^[0-9]+$ ]] || [[ "$diff_attempts" -lt 1 ]]; then
  diff_attempts=1
fi
diff_ok=false
diff_engine="pg-schema"
for diff_attempt in $(seq 1 "$diff_attempts"); do
  if (cd "$work_dir" && "${supabase_cmd[@]}" db diff \
    --db-url "$db_url" \
    --schema public,compat \
    --use-pg-schema) >"$ARTIFACT_DIR/supabase-cli-db-diff.out" 2>"$ARTIFACT_DIR/supabase-cli-db-diff.stderr"; then
    diff_ok=true
    break
  fi
  cp "$ARTIFACT_DIR/supabase-cli-db-diff.out" "$ARTIFACT_DIR/supabase-cli-db-diff.attempt-$diff_attempt.out" || true
  cp "$ARTIFACT_DIR/supabase-cli-db-diff.stderr" "$ARTIFACT_DIR/supabase-cli-db-diff.attempt-$diff_attempt.stderr" || true
  if [[ "$diff_attempt" -lt "$diff_attempts" ]]; then
    sleep "${SUPADUPA_COMPAT_CLI_DB_DIFF_RETRY_SECONDS:-5}"
  fi
done
if [[ "$diff_ok" != "true" ]] && compat_bool "${SUPADUPA_COMPAT_CLI_DB_DIFF_MIGRA_FALLBACK:-true}"; then
  if (cd "$work_dir" && "${supabase_cmd[@]}" db diff \
    --db-url "$db_url" \
    --schema public,compat \
    --use-migra) >"$ARTIFACT_DIR/supabase-cli-db-diff-migra.out" 2>"$ARTIFACT_DIR/supabase-cli-db-diff-migra.stderr"; then
    cp "$ARTIFACT_DIR/supabase-cli-db-diff-migra.out" "$ARTIFACT_DIR/supabase-cli-db-diff.out"
    cp "$ARTIFACT_DIR/supabase-cli-db-diff-migra.stderr" "$ARTIFACT_DIR/supabase-cli-db-diff.stderr"
    diff_engine="migra"
    diff_ok=true
  fi
fi
if [[ "$diff_ok" == "true" ]]; then
  if [[ "$diff_engine" == "pg-schema" ]] && ! grep -qE 'compat_cli_probe|cli_probe' "$ARTIFACT_DIR/supabase-cli-db-diff.out"; then
    fail "supabase_cli.db_diff" "db diff output did not include compat schema/table"
  fi
  if [[ "$diff_engine" == "migra" ]] && ! grep -q "Finished supabase db diff" "$ARTIFACT_DIR/supabase-cli-db-diff.stderr"; then
    fail "supabase_cli.db_diff" "migra fallback did not report a completed db diff"
  fi
  pass "supabase_cli.db_diff" "remote schema diff generated via $diff_engine"
else
  fail "supabase_cli.db_diff" "supabase db diff failed; see supabase-cli-db-diff.stderr"
fi

printf -- '-- synthetic migration repair probe\n' >"$migrations_dir/${repair_version}_supadupa_repair_probe.sql"
if (cd "$work_dir" && "${supabase_cmd[@]}" migration repair "$repair_version" --status applied --db-url "$db_url") \
  >"$ARTIFACT_DIR/supabase-cli-migration-repair-applied.out" 2>"$ARTIFACT_DIR/supabase-cli-migration-repair-applied.stderr"; then
  :
else
  fail "supabase_cli.migration_repair_applied" "supabase migration repair --status applied failed; see supabase-cli-migration-repair-applied.stderr"
fi
if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
  -v ON_ERROR_STOP=1 \
  -v version="$repair_version" \
  -Atq \
  >"$ARTIFACT_DIR/supabase-cli-migration-repair-applied-verify.out" 2>"$ARTIFACT_DIR/supabase-cli-migration-repair-applied-verify.stderr" <<'SQL'
select count(*) from supabase_migrations.schema_migrations
where version = :'version';
SQL
then
  repaired_count="$(tr -d '\r\n' <"$ARTIFACT_DIR/supabase-cli-migration-repair-applied-verify.out")"
  if [[ "$repaired_count" != "1" ]]; then
    fail "supabase_cli.migration_repair_applied" "expected synthetic migration in history, got $repaired_count"
  fi
  pass "supabase_cli.migration_repair_applied" "synthetic migration marked applied"
else
  fail "supabase_cli.migration_repair_applied" "repair applied verification failed; see supabase-cli-migration-repair-applied-verify.stderr"
fi

if (cd "$work_dir" && "${supabase_cmd[@]}" migration repair "$repair_version" --status reverted --db-url "$db_url") \
  >"$ARTIFACT_DIR/supabase-cli-migration-repair-reverted.out" 2>"$ARTIFACT_DIR/supabase-cli-migration-repair-reverted.stderr"; then
  :
else
  fail "supabase_cli.migration_repair_reverted" "supabase migration repair --status reverted failed; see supabase-cli-migration-repair-reverted.stderr"
fi
if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
  -v ON_ERROR_STOP=1 \
  -v version="$repair_version" \
  -Atq \
  >"$ARTIFACT_DIR/supabase-cli-migration-repair-reverted-verify.out" 2>"$ARTIFACT_DIR/supabase-cli-migration-repair-reverted-verify.stderr" <<'SQL'
select count(*) from supabase_migrations.schema_migrations
where version = :'version';
SQL
then
  repaired_count="$(tr -d '\r\n' <"$ARTIFACT_DIR/supabase-cli-migration-repair-reverted-verify.out")"
  if [[ "$repaired_count" != "0" ]]; then
    fail "supabase_cli.migration_repair_reverted" "expected synthetic migration removed from history, got $repaired_count"
  fi
  pass "supabase_cli.migration_repair_reverted" "synthetic migration marked reverted"
else
  fail "supabase_cli.migration_repair_reverted" "repair reverted verification failed; see supabase-cli-migration-repair-reverted-verify.stderr"
fi

deadline=$((SECONDS + ${SUPADUPA_COMPAT_REST_SCHEMA_TIMEOUT_SECONDS:-90}))
while (( SECONDS < deadline )); do
  set +e
  rest_status="$(curl -sS -o "$ARTIFACT_DIR/supabase-cli-rest.body" -w '%{http_code}' \
    -H "apikey: $anon_key" \
    "$api_url/rest/v1/compat_cli_probe?id=eq.$run_id&select=id,project_ref" \
    2>"$ARTIFACT_DIR/supabase-cli-rest.stderr")"
  rest_rc="$?"
  set -e

  if [[ "$rest_rc" -eq 0 && "$rest_status" =~ ^2 ]] &&
    grep -q "\"id\":\"$run_id\"" "$ARTIFACT_DIR/supabase-cli-rest.body"; then
    pass "supabase_cli.rest_visible" "HTTP $rest_status"
    exit 0
  fi
  sleep 3
done

fail "supabase_cli.rest_visible" "CLI migration row was not visible through PostgREST before timeout"
