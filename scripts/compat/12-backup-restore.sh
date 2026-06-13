#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool psql
require_tool curl
ensure_token
ensure_profile

if ! compat_bool "${SUPADUPA_COMPAT_RESTORE_VALIDATE:-false}"; then
  skip "restore.enabled" "SUPADUPA_COMPAT_RESTORE_VALIDATE is not true"
  exit 0
fi

if ! compat_bool "${SUPADUPA_COMPAT_CREATE_PROJECT:-false}" &&
  ! compat_bool "${SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE:-false}"; then
  skip "restore.guard" "set SUPADUPA_COMPAT_CREATE_PROJECT=true or SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE=true"
  exit 0
fi

if ! compat_bool "${SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE:-false}"; then
  created_ref="$(cat "$ARTIFACT_DIR/created-project" 2>/dev/null || true)"
  if [[ "$created_ref" != "$SUPADUPA_TEST_REF" ]]; then
    skip "restore.guard" "restore validation only runs against a project created by this compat run"
    exit 0
  fi
fi

api_url="$(profile_value api_url)"
public_db_url="$(profile_value_optional public_database_url)"
if [[ -z "$public_db_url" ]]; then
  fail "restore.public_url" "profile did not include public_database_url"
fi

public_db_safe_url="$(url_without_password "$public_db_url")"
db_password="$(reveal_secret_value db_password)"
anon_key="$(reveal_secret_value anon_key)"
run_id="${SUPADUPA_COMPAT_RUN_ID:-restore-$(date -u +%Y%m%d%H%M%S)-$$}"
printf '%s' "$run_id" >"$ARTIFACT_DIR/restore-run-id"

psql_restore_probe() {
  local test_name="$1"
  local sql_file="$2"
  local out="$ARTIFACT_DIR/$test_name.out"
  local err="$ARTIFACT_DIR/$test_name.stderr"
  if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
    -v ON_ERROR_STOP=1 \
    -v run_id="$run_id" \
    -v project_ref="$SUPADUPA_TEST_REF" \
    -Atq \
    >"$out" 2>"$err" <"$sql_file"; then
    return 0
  fi
  fail "$test_name" "psql failed; see $(basename "$err")"
}

seed_sql="$ARTIFACT_DIR/restore-seed.sql"
cat >"$seed_sql" <<'SQL'
create table if not exists public.compat_restore_probe (
  id text primary key,
  project_ref text not null,
  phase text not null,
  created_at timestamptz not null default now()
);

alter table public.compat_restore_probe enable row level security;
grant select, insert, update, delete on public.compat_restore_probe to anon, authenticated, service_role;
drop policy if exists compat_restore_probe_select on public.compat_restore_probe;
create policy compat_restore_probe_select on public.compat_restore_probe for select using (true);

insert into public.compat_restore_probe (id, project_ref, phase)
values (:'run_id', :'project_ref', 'before_backup')
on conflict (id) do update
set project_ref = excluded.project_ref,
    phase = excluded.phase;

delete from public.compat_restore_probe where id = :'run_id' || '-after';
notify pgrst, 'reload schema';
select phase from public.compat_restore_probe where id = :'run_id';
SQL

psql_restore_probe "restore.seed" "$seed_sql"
if [[ "$(tr -d '\r\n' <"$ARTIFACT_DIR/restore.seed.out")" != "before_backup" ]]; then
  fail "restore.seed" "expected before_backup marker"
fi
pass "restore.seed" "before_backup marker inserted"

backup_json="$ARTIFACT_DIR/restore-backup.json"
backup_err="$ARTIFACT_DIR/restore-backup.stderr"
if ! supadupa_cli_authed backups trigger --ref "$SUPADUPA_TEST_REF" >"$backup_json" 2>"$backup_err"; then
  fail "restore.backup" "backup trigger failed; see $(basename "$backup_err")"
fi
backup_id="$(json_get_file_optional "$backup_json" id)"
backup_ref="$(json_get_file_optional "$backup_json" project_ref)"
backup_kind="$(json_get_file_optional "$backup_json" kind)"
backup_status="$(json_get_file_optional "$backup_json" status)"
backup_size="$(json_get_file_optional "$backup_json" size_bytes)"
backup_checksum="$(json_get_file_optional "$backup_json" checksum_sha256)"
backup_verified_at="$(json_get_file_optional "$backup_json" verified_at)"
backup_started_at="$(json_get_file_optional "$backup_json" started_at)"
backup_finished_at="$(json_get_file_optional "$backup_json" finished_at)"
backup_location="$(json_get_file_optional "$backup_json" location)"
backup_remote_location="$(json_get_file_optional "$backup_json" remote_location)"
if [[ -z "$backup_id" ]]; then
  fail "restore.backup" "backup response did not include id"
fi
if [[ "$backup_ref" != "$SUPADUPA_TEST_REF" ]]; then
  fail "restore.backup" "expected project_ref=$SUPADUPA_TEST_REF, got ${backup_ref:-empty}"
fi
if [[ "$backup_kind" != "logical" ]]; then
  fail "restore.backup" "expected logical backup, got ${backup_kind:-empty}"
fi
if [[ "$backup_status" != "completed" ]]; then
  fail "restore.backup" "expected completed backup, got ${backup_status:-empty}"
fi
case "$backup_size" in
  ''|0) fail "restore.backup" "backup size is empty or zero" ;;
esac
if [[ -z "$backup_checksum" || "${#backup_checksum}" -ne 64 ]]; then
  fail "restore.backup" "backup checksum was missing or malformed"
fi
if [[ -z "$backup_verified_at" ]]; then
  fail "restore.backup" "backup verified_at was empty"
fi
if [[ -z "$backup_started_at" || -z "$backup_finished_at" ]]; then
  fail "restore.backup" "backup started_at or finished_at was empty"
fi
if ! node -e '
const started = Date.parse(process.argv[1]);
const finished = Date.parse(process.argv[2]);
const verified = Date.parse(process.argv[3]);
if (!Number.isFinite(started) || !Number.isFinite(finished) || !Number.isFinite(verified)) throw new Error("invalid timestamps");
if (finished < started) throw new Error("finished_at before started_at");
if (verified < started) throw new Error("verified_at before started_at");
' "$backup_started_at" "$backup_finished_at" "$backup_verified_at"; then
  fail "restore.backup" "backup timestamps were invalid"
fi
if [[ -z "$backup_location" && -z "$backup_remote_location" ]]; then
  fail "restore.backup" "backup had neither local nor remote location"
fi
pass "restore.backup" "backup_id=$backup_id size=$backup_size started=$backup_started_at finished=$backup_finished_at"

backup_list_json="$ARTIFACT_DIR/restore-backup-list.json"
backup_list_err="$ARTIFACT_DIR/restore-backup-list.stderr"
if ! supadupa_cli_authed backups list --ref "$SUPADUPA_TEST_REF" >"$backup_list_json" 2>"$backup_list_err"; then
  fail "restore.backup_list" "backup list failed; see $(basename "$backup_list_err")"
fi
if ! node -e '
const fs = require("fs");
const backups = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const backupID = process.argv[2];
if (!Array.isArray(backups)) throw new Error("response is not an array");
const backup = backups.find((item) => item && item.id === backupID);
if (!backup) throw new Error(`backup ${backupID} missing`);
for (const field of ["started_at", "finished_at", "verified_at", "checksum_sha256", "size_bytes"]) {
  if (!backup[field]) throw new Error(`${field} missing`);
}
' "$backup_list_json" "$backup_id"; then
  fail "restore.backup_list" "backup $backup_id missing from list or missing restore-critical metadata"
fi
pass "restore.backup_list" "backup $backup_id listed with restore metadata"

preflight_restore_json="$ARTIFACT_DIR/restore-preflight-response.json"
preflight_restore_err="$ARTIFACT_DIR/restore-preflight-response.stderr"
if ! supadupa_cli_authed backups restore --ref "$SUPADUPA_TEST_REF" --backup-id "$backup_id" --confirmation "restore project $SUPADUPA_TEST_REF" >"$preflight_restore_json" 2>"$preflight_restore_err"; then
  fail "restore.preflight" "preflight restore command failed; see $(basename "$preflight_restore_err")"
fi
preflight_restore_state="$(json_get_file_optional "$preflight_restore_json" restore_state)"
if [[ "$preflight_restore_state" != "completed" ]]; then
  fail "restore.preflight" "expected completed preflight restore, got ${preflight_restore_state:-empty}"
fi
pass "restore.preflight" "live restore command is configured"

mutate_sql="$ARTIFACT_DIR/restore-mutate.sql"
cat >"$mutate_sql" <<'SQL'
update public.compat_restore_probe
set phase = 'after_backup'
where id = :'run_id';

insert into public.compat_restore_probe (id, project_ref, phase)
values (:'run_id' || '-after', :'project_ref', 'after_backup_only')
on conflict (id) do update
set project_ref = excluded.project_ref,
    phase = excluded.phase;

select phase || '|' || (
  select count(*)::text from public.compat_restore_probe where id = :'run_id' || '-after'
)
from public.compat_restore_probe
where id = :'run_id';
SQL

psql_restore_probe "restore.mutate" "$mutate_sql"
if [[ "$(tr -d '\r\n' <"$ARTIFACT_DIR/restore.mutate.out")" != "after_backup|1" ]]; then
  fail "restore.mutate" "expected after_backup mutation and extra row"
fi
pass "restore.mutate" "post-backup mutation written"

restore_json="$ARTIFACT_DIR/restore-response.json"
restore_err="$ARTIFACT_DIR/restore-response.stderr"
if ! supadupa_cli_authed backups restore --ref "$SUPADUPA_TEST_REF" --backup-id "$backup_id" --confirmation "restore project $SUPADUPA_TEST_REF" >"$restore_json" 2>"$restore_err"; then
  fail "restore.run" "restore command failed; see $(basename "$restore_err")"
fi
restore_state="$(json_get_file_optional "$restore_json" restore_state)"
restore_path="$(json_get_file_optional "$restore_json" restore_path)"
if [[ "$restore_state" != "completed" ]]; then
  fail "restore.run" "expected completed restore, got ${restore_state:-empty}"
fi
if [[ -z "$restore_path" ]]; then
  fail "restore.run" "restore_path was empty"
fi
pass "restore.run" "state=$restore_state path=$restore_path"

verify_sql="$ARTIFACT_DIR/restore-verify.sql"
cat >"$verify_sql" <<'SQL'
notify pgrst, 'reload schema';
select phase || '|' || (
  select count(*)::text from public.compat_restore_probe where id = :'run_id' || '-after'
)
from public.compat_restore_probe
where id = :'run_id';
SQL

psql_restore_probe "restore.verify" "$verify_sql"
if [[ "$(tr -d '\r\n' <"$ARTIFACT_DIR/restore.verify.out")" != "before_backup|0" ]]; then
  fail "restore.verify" "restore did not revert post-backup mutation"
fi
pass "restore.verify" "post-backup mutation reverted"

deadline=$((SECONDS + ${SUPADUPA_COMPAT_REST_SCHEMA_TIMEOUT_SECONDS:-90}))
while (( SECONDS < deadline )); do
  set +e
  rest_status="$(curl -sS -o "$ARTIFACT_DIR/restore-rest.body" -w '%{http_code}' \
    -H "apikey: $anon_key" \
    "$api_url/rest/v1/compat_restore_probe?id=eq.$run_id&select=id,phase" \
    2>"$ARTIFACT_DIR/restore-rest.stderr")"
  rest_rc="$?"
  set -e

  if [[ "$rest_rc" -eq 0 && "$rest_status" =~ ^2 ]] &&
    grep -q "\"phase\":\"before_backup\"" "$ARTIFACT_DIR/restore-rest.body"; then
    pass "restore.rest_visible" "HTTP $rest_status"
    exit 0
  fi
  sleep 3
done

fail "restore.rest_visible" "restored marker was not visible through PostgREST before timeout"
