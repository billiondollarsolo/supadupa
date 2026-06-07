#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool npm
require_tool node
require_tool psql
ensure_token
ensure_profile

api_url="$(profile_value api_url)"
public_db_url="$(profile_value_optional public_database_url)"
if [[ -z "$public_db_url" ]]; then
  fail "realtime_deep.public_db_url" "profile did not include public_database_url"
fi
public_db_safe_url="$(url_without_password "$public_db_url")"
db_password="$(reveal_secret_value db_password)"
anon_key="$(reveal_secret_value anon_key)"
service_role="$(reveal_secret_value service_role)"

if [[ ! -d "$SCRIPT_DIR/node_modules/@supabase/supabase-js" ]]; then
  if npm --prefix "$SCRIPT_DIR" install --omit=dev --no-audit --no-fund --package-lock=false \
    >"$ARTIFACT_DIR/realtime-deep-sdk-install.out" 2>"$ARTIFACT_DIR/realtime-deep-sdk-install.stderr"; then
    pass "realtime_deep.sdk.install" "@supabase/supabase-js installed"
  else
    fail "realtime_deep.sdk.install" "npm install failed; see realtime-deep-sdk-install.stderr"
  fi
fi

run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
suffix="$(printf '%s' "$run_id" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9_' | cut -c1-36)"
table_name="compat_realtime_${suffix:-deep}"
trigger_function="compat_realtime_${suffix:-deep}_broadcast"
messages_policy="compat_realtime_${suffix:-deep}_messages_select"
user_messages_policy="compat_realtime_${suffix:-deep}_user_messages_select"
db_broadcast_channel="compat-db-${suffix:-deep}"
db_broadcast_topic="$db_broadcast_channel"

cleanup_realtime_deep() {
  if [[ -n "${db_password:-}" && -n "${public_db_safe_url:-}" && -n "${table_name:-}" ]]; then
    PGPASSWORD="$db_password" psql "$public_db_safe_url" \
      -v ON_ERROR_STOP=1 \
      -v table="$table_name" \
      -v func="$trigger_function" \
      -v policy="$messages_policy" \
      -v user_policy="$user_messages_policy" \
      -q >"$ARTIFACT_DIR/realtime-deep-cleanup.out" 2>"$ARTIFACT_DIR/realtime-deep-cleanup.stderr" <<'SQL' || true
drop policy if exists :"user_policy" on realtime.messages;
drop policy if exists :"policy" on realtime.messages;
drop table if exists public.:"table" cascade;
drop function if exists public.:"func"() cascade;
SQL
  fi
}
trap cleanup_realtime_deep EXIT

if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
  -v ON_ERROR_STOP=1 \
  -v table="$table_name" \
  -v func="$trigger_function" \
  -v policy="$messages_policy" \
  -v user_policy="$user_messages_policy" \
  -v topic="$db_broadcast_topic" \
  -q >"$ARTIFACT_DIR/realtime-deep-setup.out" 2>"$ARTIFACT_DIR/realtime-deep-setup.stderr" <<'SQL'
drop policy if exists :"user_policy" on realtime.messages;
drop policy if exists :"policy" on realtime.messages;
drop function if exists public.:"func"() cascade;
drop table if exists public.:"table" cascade;
create table public.:"table" (
  id bigserial primary key,
  run_id text not null,
  body text not null,
  created_at timestamptz default now()
);
alter table public.:"table" enable row level security;
create policy compat_realtime_select on public.:"table" for select to anon, authenticated using (true);
create policy compat_realtime_insert on public.:"table" for insert to anon, authenticated with check (true);
create policy :"policy" on realtime.messages
  for select to authenticated
  using (realtime.topic() = :'topic');
create policy :"user_policy" on realtime.messages
  for select to authenticated
  using (realtime.topic() = ('compat-user-' || auth.uid()::text));
do $$
begin
  if not exists (select 1 from pg_publication where pubname = 'supabase_realtime') then
    raise exception 'supabase_realtime publication is missing';
  end if;
end
$$;
create function public.:"func"()
returns trigger
language plpgsql
security definer
set search_path = public, realtime
as $$
begin
  perform realtime.broadcast_changes(TG_ARGV[0], 'compat-db-broadcast', TG_OP, TG_TABLE_NAME, TG_TABLE_SCHEMA, NEW, OLD);
  return NEW;
end;
$$;
select format('create trigger %I after insert on public.%I for each row execute function public.%I(%L)', :'func', :'table', :'func', :'topic')\gexec
select format('alter publication supabase_realtime add table public.%I', :'table')\gexec
select format($fmt$
do $$
begin
  if not exists (
    select 1
    from pg_publication_tables
    where pubname = 'supabase_realtime'
      and schemaname = 'public'
      and tablename = %L
  ) then
    raise exception 'table %% is not in supabase_realtime publication', %L;
  end if;
end
$$;
$fmt$, :'table', :'table')\gexec
SQL
then
  pass "realtime_deep.fixture" "table $table_name published"
else
  fail "realtime_deep.fixture" "fixture setup failed; see realtime-deep-setup.stderr"
fi

if SUPABASE_URL="$api_url" \
  SUPABASE_ANON_KEY="$anon_key" \
  SUPABASE_SERVICE_ROLE_KEY="$service_role" \
  SUPADUPA_TEST_REF="$SUPADUPA_TEST_REF" \
  SUPADUPA_COMPAT_RUN_ID="$run_id" \
  SUPADUPA_REALTIME_TABLE="$table_name" \
  SUPADUPA_REALTIME_DB_BROADCAST_CHANNEL="$db_broadcast_channel" \
  SUPADUPA_REALTIME_DB_BROADCAST_TOPIC="$db_broadcast_topic" \
  node "$SCRIPT_DIR/realtime-deep-probe.mjs" >"$ARTIFACT_DIR/realtime-deep.out" 2>"$ARTIFACT_DIR/realtime-deep.stderr"; then
  pass "realtime_deep.presence_postgres_changes_db_broadcast" "presence, Postgres Changes, private DB broadcast, replay, and same-project private-channel isolation delivered"
else
  fail "realtime_deep.presence_postgres_changes" "deep realtime probe failed; see realtime-deep.stderr"
fi

run_realtime_reconnect_check() {
  local mode="${SUPADUPA_COMPAT_REALTIME_RECONNECT:-auto}"
  if [[ "$mode" == "false" || "$mode" == "0" || "$mode" == "off" ]]; then
    skip "realtime_deep.reconnect" "SUPADUPA_COMPAT_REALTIME_RECONNECT disabled"
    return 0
  fi
  if ! command -v docker >/dev/null 2>&1; then
    if [[ "$mode" == "true" || "$mode" == "1" || "$mode" == "on" ]]; then
      fail "realtime_deep.reconnect.docker" "docker is required when SUPADUPA_COMPAT_REALTIME_RECONNECT is enabled"
    fi
    skip "realtime_deep.reconnect" "docker unavailable; cannot restart Realtime container"
    return 0
  fi
  local container="${SUPADUPA_REALTIME_CONTAINER:-$SUPADUPA_TEST_REF-realtime-1}"
  if ! docker inspect "$container" >/dev/null 2>&1; then
    if [[ "$mode" == "true" || "$mode" == "1" || "$mode" == "on" ]]; then
      fail "realtime_deep.reconnect.container" "Realtime container $container not found"
    fi
    skip "realtime_deep.reconnect" "Realtime container $container not visible"
    return 0
  fi
  if SUPABASE_URL="$api_url" \
    SUPABASE_ANON_KEY="$anon_key" \
    SUPADUPA_COMPAT_RUN_ID="$run_id" \
    SUPADUPA_REALTIME_RESTART_CONTAINER="$container" \
    node "$SCRIPT_DIR/realtime-reconnect-probe.mjs" >"$ARTIFACT_DIR/realtime-reconnect.out" 2>"$ARTIFACT_DIR/realtime-reconnect.stderr"; then
    pass "realtime_deep.reconnect_after_restart" "client resubscribed and received broadcast after $container restart"
  else
    fail "realtime_deep.reconnect_after_restart" "reconnect probe failed; see realtime-reconnect.stderr"
  fi
}

run_realtime_reconnect_check
