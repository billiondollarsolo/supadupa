#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool node
require_tool psql
ensure_token
ensure_profile

public_db_url="$(profile_value_optional public_database_url)"
if [[ -z "$public_db_url" ]]; then
  fail "database_desired.public_db_url" "profile did not include public_database_url"
fi
public_db_safe_url="$(url_without_password "$public_db_url")"
db_password="$(reveal_secret_value db_password)"

run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
suffix="$(printf '%s' "$run_id" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9' | cut -c1-24)"
if [[ ${#suffix} -lt 8 ]]; then
  suffix="${suffix}$(date -u +%H%M%S)"
fi
api_name="compatds${suffix}"
cron_name="$api_name"
queue_name="$api_name"
dead_letter_queue="${api_name}dlq"
webhook_name="$api_name"
schema_decl_name="$api_name"
schema_decl_version="v${suffix}"
role_name="compatds_${suffix}"
fixture_table="compatds_fixture_${suffix}"
schema_table="compatds_schema_${suffix}"

psql_project() {
  PGPASSWORD="$db_password" psql "$public_db_safe_url" -v ON_ERROR_STOP=1 "$@"
}

cleanup_database_desired_state() {
  supadupa_cli_authed database-webhooks delete --ref "$SUPADUPA_TEST_REF" --name "$webhook_name" \
    >"$ARTIFACT_DIR/database-desired-webhook-cleanup.out" 2>"$ARTIFACT_DIR/database-desired-webhook-cleanup.stderr" || true
  supadupa_cli_authed database-roles delete --ref "$SUPADUPA_TEST_REF" --name "$role_name" \
    >"$ARTIFACT_DIR/database-desired-role-cleanup.out" 2>"$ARTIFACT_DIR/database-desired-role-cleanup.stderr" || true
  supadupa_cli_authed database-queues delete --ref "$SUPADUPA_TEST_REF" --name "$queue_name" \
    >"$ARTIFACT_DIR/database-desired-queue-cleanup.out" 2>"$ARTIFACT_DIR/database-desired-queue-cleanup.stderr" || true
  supadupa_cli_authed database-cron delete --ref "$SUPADUPA_TEST_REF" --name "$cron_name" \
    >"$ARTIFACT_DIR/database-desired-cron-cleanup.out" 2>"$ARTIFACT_DIR/database-desired-cron-cleanup.stderr" || true
  supadupa_cli_authed database-schemas delete --ref "$SUPADUPA_TEST_REF" --name "$schema_decl_name" --version "$schema_decl_version" \
    >"$ARTIFACT_DIR/database-desired-schema-cleanup.out" 2>"$ARTIFACT_DIR/database-desired-schema-cleanup.stderr" || true
  psql_project -v fixture_table="$fixture_table" -v schema_table="$schema_table" -v role_name="$role_name" -v queue_name="$queue_name" -v dead_letter_queue="$dead_letter_queue" \
    -q >"$ARTIFACT_DIR/database-desired-sql-cleanup.out" 2>"$ARTIFACT_DIR/database-desired-sql-cleanup.stderr" <<'SQL' || true
select format('drop table if exists public.%I cascade', :'fixture_table')\gexec
select format('drop table if exists public.%I cascade', :'schema_table')\gexec
select cron.unschedule(jobid) from cron.job where jobname = :'queue_name';
select format('drop role if exists %I', :'role_name')\gexec
SQL
}
trap cleanup_database_desired_state EXIT

psql_project -v fixture_table="$fixture_table" -q >"$ARTIFACT_DIR/database-desired-fixture.out" 2>"$ARTIFACT_DIR/database-desired-fixture.stderr" <<'SQL'
create table if not exists public.:"fixture_table" (
  id bigserial primary key,
  body text not null,
  created_at timestamptz default now()
);
SQL
pass "database_desired.fixture" "$fixture_table"

extensions_file="$ARTIFACT_DIR/database-desired-extensions.json"
if supadupa_cli_authed database-extensions list --ref "$SUPADUPA_TEST_REF" >"$extensions_file" 2>"$ARTIFACT_DIR/database-desired-extensions.stderr"; then
  node - "$extensions_file" <<'NODE'
const fs = require("fs");
const extensions = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
for (const name of ["pg_cron", "pgmq", "pg_net", "pg_graphql", "vector"]) {
  if (!extensions.some((extension) => extension.name === name || (name === "pg_net" && extension.name === "pg_net"))) {
    if (name === "pg_net") continue;
    throw new Error(`missing extension ${name}`);
  }
}
NODE
  live_extension_count="$(psql_project -Atqc "select count(*) from pg_extension where extname in ('pg_cron','pgmq','pg_graphql','vector')")"
  if [[ "$live_extension_count" -lt 4 ]]; then
    fail "database_desired.extensions_live" "expected core extensions installed, got $live_extension_count"
  fi
  pass "database_desired.extensions_live" "core extensions installed"
else
  fail "database_desired.extensions_list" "database extension list failed"
fi

cron_file="$ARTIFACT_DIR/database-desired-cron-create.json"
if supadupa_cli_authed database-cron create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "$cron_name" \
  --schedule "*/5 * * * *" \
  --command "select 1;" \
  --database postgres \
  --username postgres \
  --timeout-seconds 30 \
  --max-runtime-seconds 30 \
  --metadata "compat=database-desired" \
  >"$cron_file" 2>"$ARTIFACT_DIR/database-desired-cron-create.stderr"; then
  node - "$cron_file" "$cron_name" <<'NODE'
const fs = require("fs");
const job = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (job.name !== process.argv[3]) throw new Error(`name=${job.name}`);
if (job.status !== "scheduled") throw new Error(`status=${job.status}`);
if (job.active !== true) throw new Error(`active=${job.active}`);
NODE
  cron_count="$(psql_project -Atqc "select count(*) from cron.job where jobname = '$cron_name'")"
  if [[ "$cron_count" != "1" ]]; then
    fail "database_desired.cron_live" "expected live cron.job row, got $cron_count"
  fi
  pass "database_desired.cron_live" "$cron_name scheduled"
else
  fail "database_desired.cron_create" "cron create failed; see database-desired-cron-create.stderr"
fi

queue_file="$ARTIFACT_DIR/database-desired-queue-create.json"
if supadupa_cli_authed database-queues create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "$queue_name" \
  --schema pgmq \
  --retention-minutes 1440 \
  --visibility-timeout-seconds 30 \
  --max-retries 5 \
  --dead-letter-queue "$dead_letter_queue" \
  --metadata "compat=database-desired" \
  >"$queue_file" 2>"$ARTIFACT_DIR/database-desired-queue-create.stderr"; then
  node - "$queue_file" "$queue_name" "$dead_letter_queue" <<'NODE'
const fs = require("fs");
const queue = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (queue.name !== process.argv[3]) throw new Error(`name=${queue.name}`);
if (queue.dead_letter_queue !== process.argv[4]) throw new Error(`dead_letter_queue=${queue.dead_letter_queue}`);
if (queue.status !== "ready") throw new Error(`status=${queue.status}`);
NODE
  queue_count="$(psql_project -Atqc "select count(*) from pg_class where relnamespace = 'pgmq'::regnamespace and relname in ('q_${queue_name}', 'q_${dead_letter_queue}')")"
  if [[ "$queue_count" != "2" ]]; then
    fail "database_desired.queue_live" "expected live queue and dead-letter tables, got $queue_count"
  fi
  pass "database_desired.queue_live" "$queue_name and $dead_letter_queue created"
else
  fail "database_desired.queue_create" "queue create failed; see database-desired-queue-create.stderr"
fi

webhook_file="$ARTIFACT_DIR/database-desired-webhook-create.json"
if supadupa_cli_authed database-webhooks create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "$webhook_name" \
  --schema public \
  --table "$fixture_table" \
  --events insert,update \
  --endpoint "https://example.com/supadupa-compat-webhook" \
  --method POST \
  --timeout-seconds 5 \
  --retry-count 1 \
  --header "X-Supadupa-Compat=database-desired" \
  --metadata "compat=database-desired" \
  >"$webhook_file" 2>"$ARTIFACT_DIR/database-desired-webhook-create.stderr"; then
  node - "$webhook_file" "$webhook_name" "$fixture_table" <<'NODE'
const fs = require("fs");
const webhook = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (webhook.name !== process.argv[3]) throw new Error(`name=${webhook.name}`);
if (webhook.table !== process.argv[4]) throw new Error(`table=${webhook.table}`);
if (webhook.status !== "ready") throw new Error(`status=${webhook.status}`);
NODE
  trigger_count="$(psql_project -Atqc "select count(*) from pg_trigger where tgname in ('supadupa_webhook_${webhook_name}_insert','supadupa_webhook_${webhook_name}_update')")"
  if [[ "$trigger_count" != "2" ]]; then
    fail "database_desired.webhook_live" "expected live insert/update triggers, got $trigger_count"
  fi
  pass "database_desired.webhook_live" "$webhook_name triggers created"
else
  fail "database_desired.webhook_create" "webhook create failed; see database-desired-webhook-create.stderr"
fi

schema_sql_file="$ARTIFACT_DIR/database-desired-schema.sql"
cat >"$schema_sql_file" <<SQL
create table if not exists public.$schema_table (
  id bigserial primary key,
  run_id text not null,
  created_at timestamptz default now()
);
insert into public.$schema_table (run_id) values ('$run_id');
SQL

schema_file="$ARTIFACT_DIR/database-desired-schema-create.json"
if supadupa_cli_authed database-schemas create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "$schema_decl_name" \
  --version "$schema_decl_version" \
  --schema public \
  --sql-file "$schema_sql_file" \
  --apply-order 10 \
  --metadata "compat=database-desired" \
  >"$schema_file" 2>"$ARTIFACT_DIR/database-desired-schema-create.stderr"; then
  node - "$schema_file" "$schema_decl_name" "$schema_decl_version" <<'NODE'
const fs = require("fs");
const schema = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (schema.name !== process.argv[3]) throw new Error(`name=${schema.name}`);
if (schema.version !== process.argv[4]) throw new Error(`version=${schema.version}`);
if (!schema.checksum) throw new Error("missing checksum");
NODE
  schema_row_count="$(psql_project -Atqc "select count(*) from public.$schema_table where run_id = '$run_id'")"
  if [[ "$schema_row_count" != "1" ]]; then
    fail "database_desired.schema_live" "expected live schema SQL row, got $schema_row_count"
  fi
  pass "database_desired.schema_live" "$schema_table applied"
else
  fail "database_desired.schema_create" "schema create failed; see database-desired-schema-create.stderr"
fi

role_file="$ARTIFACT_DIR/database-desired-role-create.json"
if supadupa_cli_authed database-roles create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "$role_name" \
  --member-of authenticated \
  --grant public=usage,select \
  --metadata "compat=database-desired" \
  >"$role_file" 2>"$ARTIFACT_DIR/database-desired-role-create.stderr"; then
  node - "$role_file" "$role_name" <<'NODE'
const fs = require("fs");
const role = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (role.name !== process.argv[3]) throw new Error(`name=${role.name}`);
if (role.status !== "configured") throw new Error(`status=${role.status}`);
if (role.login !== false) throw new Error(`login=${role.login}`);
NODE
  role_exists="$(psql_project -Atqc "select count(*) from pg_roles where rolname = '$role_name'")"
  role_member="$(psql_project -Atqc "select count(*) from pg_auth_members m join pg_roles r on r.oid=m.roleid join pg_roles u on u.oid=m.member where r.rolname='authenticated' and u.rolname='$role_name'")"
  if [[ "$role_exists" != "1" || "$role_member" != "1" ]]; then
    fail "database_desired.role_live" "expected live role and authenticated membership, got role=$role_exists membership=$role_member"
  fi
  pass "database_desired.role_live" "$role_name created and granted"
else
  fail "database_desired.role_create" "role create failed; see database-desired-role-create.stderr"
fi

if supadupa_cli_authed database-webhooks delete --ref "$SUPADUPA_TEST_REF" --name "$webhook_name" \
  >"$ARTIFACT_DIR/database-desired-webhook-delete.out" 2>"$ARTIFACT_DIR/database-desired-webhook-delete.stderr"; then
  trigger_count_after="$(psql_project -Atqc "select count(*) from pg_trigger where tgname in ('supadupa_webhook_${webhook_name}_insert','supadupa_webhook_${webhook_name}_update')")"
  if [[ "$trigger_count_after" != "0" ]]; then
    fail "database_desired.webhook_delete_live" "expected webhook triggers removed, got $trigger_count_after"
  fi
  pass "database_desired.webhook_delete_live" "$webhook_name removed"
else
  fail "database_desired.webhook_delete" "webhook delete failed; see database-desired-webhook-delete.stderr"
fi

if supadupa_cli_authed database-roles delete --ref "$SUPADUPA_TEST_REF" --name "$role_name" \
  >"$ARTIFACT_DIR/database-desired-role-delete.out" 2>"$ARTIFACT_DIR/database-desired-role-delete.stderr"; then
  role_exists_after="$(psql_project -Atqc "select count(*) from pg_roles where rolname = '$role_name'")"
  if [[ "$role_exists_after" != "0" ]]; then
    fail "database_desired.role_delete_live" "expected role removed, got $role_exists_after"
  fi
  pass "database_desired.role_delete_live" "$role_name removed"
else
  fail "database_desired.role_delete" "role delete failed; see database-desired-role-delete.stderr"
fi

if supadupa_cli_authed database-queues delete --ref "$SUPADUPA_TEST_REF" --name "$queue_name" \
  >"$ARTIFACT_DIR/database-desired-queue-delete.out" 2>"$ARTIFACT_DIR/database-desired-queue-delete.stderr"; then
  queue_count_after="$(psql_project -Atqc "select count(*) from pg_class where relnamespace = 'pgmq'::regnamespace and relname in ('q_${queue_name}', 'q_${dead_letter_queue}')")"
  if [[ "$queue_count_after" != "0" ]]; then
    fail "database_desired.queue_delete_live" "expected queue tables removed, got $queue_count_after"
  fi
  pass "database_desired.queue_delete_live" "$queue_name removed"
else
  fail "database_desired.queue_delete" "queue delete failed; see database-desired-queue-delete.stderr"
fi

if supadupa_cli_authed database-cron delete --ref "$SUPADUPA_TEST_REF" --name "$cron_name" \
  >"$ARTIFACT_DIR/database-desired-cron-delete.out" 2>"$ARTIFACT_DIR/database-desired-cron-delete.stderr"; then
  cron_count_after="$(psql_project -Atqc "select count(*) from cron.job where jobname = '$cron_name'")"
  if [[ "$cron_count_after" != "0" ]]; then
    fail "database_desired.cron_delete_live" "expected cron.job removed, got $cron_count_after"
  fi
  pass "database_desired.cron_delete_live" "$cron_name removed"
else
  fail "database_desired.cron_delete" "cron delete failed; see database-desired-cron-delete.stderr"
fi

if supadupa_cli_authed database-schemas delete --ref "$SUPADUPA_TEST_REF" --name "$schema_decl_name" --version "$schema_decl_version" \
  >"$ARTIFACT_DIR/database-desired-schema-delete.out" 2>"$ARTIFACT_DIR/database-desired-schema-delete.stderr"; then
  pass "database_desired.schema_metadata_delete" "$schema_decl_name/$schema_decl_version removed"
else
  fail "database_desired.schema_delete" "schema metadata delete failed; see database-desired-schema-delete.stderr"
fi

psql_project -v fixture_table="$fixture_table" -v schema_table="$schema_table" -q >"$ARTIFACT_DIR/database-desired-final-sql-cleanup.out" 2>"$ARTIFACT_DIR/database-desired-final-sql-cleanup.stderr" <<'SQL'
select format('drop table if exists public.%I cascade', :'fixture_table')\gexec
select format('drop table if exists public.%I cascade', :'schema_table')\gexec
SQL
pass "database_desired.cleanup" "temporary SQL objects removed"

pass "database_desired.complete" "extensions, cron, pgmq, webhook triggers, schema SQL, and role DDL verified"
