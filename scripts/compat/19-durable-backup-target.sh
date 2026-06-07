#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

if ! compat_bool "${SUPADUPA_COMPAT_DURABLE_BACKUP_TARGET:-false}"; then
  rm -f "$ARTIFACT_DIR/durable-backup-target-id"
  skip "durable_backup_target.enabled" "set SUPADUPA_COMPAT_DURABLE_BACKUP_TARGET=true to run"
  exit 0
fi

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool curl
require_tool node
ensure_token

token="$(read_secret_file "$ARTIFACT_DIR/token")"
api_base="${SUPADUPA_API_URL%/}"

if compat_bool "${SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS:-false}"; then
  compat_require_runtime_config_bool \
    "durable_backup_target.server_guard.recovery_ready_targets" \
    "recovery.require_recovery_ready_targets" \
    "true"
fi

target_name="${SUPADUPA_COMPAT_DURABLE_BACKUP_TARGET_NAME:-compat-durable-$SUPADUPA_TEST_REF}"
endpoint="${SUPADUPA_COMPAT_DURABLE_S3_ENDPOINT:-${SUPADUPA_BACKUP_TARGET_ENDPOINT:-${SUPADUPA_BACKUP_S3_ENDPOINT:-}}}"
region="${SUPADUPA_COMPAT_DURABLE_S3_REGION:-${SUPADUPA_BACKUP_TARGET_REGION:-${SUPADUPA_BACKUP_S3_REGION:-auto}}}"
bucket="${SUPADUPA_COMPAT_DURABLE_S3_BUCKET:-${SUPADUPA_BACKUP_TARGET_BUCKET:-${SUPADUPA_BACKUP_S3_BUCKET:-}}}"
prefix="${SUPADUPA_COMPAT_DURABLE_S3_PREFIX:-${SUPADUPA_BACKUP_TARGET_PREFIX:-${SUPADUPA_BACKUP_S3_PREFIX:-compat/$SUPADUPA_TEST_REF}}}"
access_key_id="${SUPADUPA_COMPAT_DURABLE_S3_ACCESS_KEY_ID:-${SUPADUPA_BACKUP_TARGET_ACCESS_KEY_ID:-${SUPADUPA_BACKUP_S3_ACCESS_KEY_ID:-}}}"
secret_access_key="${SUPADUPA_COMPAT_DURABLE_S3_SECRET_ACCESS_KEY:-${SUPADUPA_BACKUP_TARGET_SECRET_ACCESS_KEY:-${SUPADUPA_BACKUP_S3_SECRET_ACCESS_KEY:-}}}"
force_path_style=false
default_target=true
if compat_bool "${SUPADUPA_COMPAT_DURABLE_S3_FORCE_PATH_STYLE:-${SUPADUPA_BACKUP_TARGET_FORCE_PATH_STYLE:-false}}"; then
  force_path_style=true
fi
if ! compat_bool "${SUPADUPA_COMPAT_DURABLE_BACKUP_TARGET_DEFAULT:-true}"; then
  default_target=false
fi

if [[ -z "$endpoint" || -z "$bucket" || -z "$access_key_id" || -z "$secret_access_key" ]]; then
  fail "durable_backup_target.env" "durable S3 endpoint, bucket, access key, and secret key are required"
fi

api_json() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local out="$4"
  local err="$5"
  local status

  if [[ -n "$body" ]]; then
    status="$(curl -sS -o "$out" -w '%{http_code}' \
      -X "$method" \
      -H "Authorization: Bearer $token" \
      -H "Content-Type: application/json" \
      -d "$body" \
      "$api_base$path" 2>"$err")"
  else
    status="$(curl -sS -o "$out" -w '%{http_code}' \
      -X "$method" \
      -H "Authorization: Bearer $token" \
      "$api_base$path" 2>"$err")"
  fi
  printf '%s' "$status"
}

if compat_bool "${SUPADUPA_COMPAT_DURABLE_S3_CREATE_BUCKET:-false}"; then
  if SUPABASE_S3_ENDPOINT="$endpoint" \
    SUPABASE_S3_REGION="$region" \
    SUPABASE_S3_ACCESS_KEY="$access_key_id" \
    SUPABASE_S3_SECRET_KEY="$secret_access_key" \
    SUPADUPA_S3_BUCKET="$bucket" \
    SUPADUPA_S3_ACTION="create" \
    node "$SCRIPT_DIR/s3-bucket-admin.mjs" >"$ARTIFACT_DIR/durable-backup-target-bucket-create.out" 2>"$ARTIFACT_DIR/durable-backup-target-bucket-create.stderr"; then
    pass "durable_backup_target.bucket_create" "$bucket"
  else
    fail "durable_backup_target.bucket_create" "failed to create/list bucket; see durable-backup-target-bucket-create.stderr"
  fi
fi

targets_json="$ARTIFACT_DIR/durable-backup-targets-before.json"
targets_err="$ARTIFACT_DIR/durable-backup-targets-before.stderr"
targets_status="$(api_json GET "/v1/backup-storage-targets" "" "$targets_json" "$targets_err")"
if [[ "$targets_status" != 2* ]]; then
  fail "durable_backup_target.list" "expected 2xx, got HTTP $targets_status"
fi

existing_id="$(node -e '
const fs = require("fs");
const targets = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const name = process.argv[2];
const match = Array.isArray(targets) ? targets.find((target) => target && target.name === name) : null;
if (match?.id) process.stdout.write(match.id);
' "$targets_json" "$target_name")"

payload_file="$(mktemp "${TMPDIR:-/tmp}/supadupa-durable-target-payload.XXXXXX.json")"
trap 'rm -f "$payload_file"' EXIT
node -e '
const payload = {
  name: process.argv[1],
  type: "s3",
  endpoint: process.argv[2],
  region: process.argv[3],
  bucket: process.argv[4],
  prefix: process.argv[5],
  access_key_id: process.argv[6],
  secret_access_key: process.argv[7],
  force_path_style: process.argv[8] === "true",
  default: process.argv[9] === "true",
};
process.stdout.write(JSON.stringify(payload));
' "$target_name" "$endpoint" "$region" "$bucket" "$prefix" "$access_key_id" "$secret_access_key" "$force_path_style" "$default_target" >"$payload_file"

target_json="$ARTIFACT_DIR/durable-backup-target.json"
target_err="$ARTIFACT_DIR/durable-backup-target.stderr"
if [[ -n "$existing_id" ]]; then
  target_status="$(api_json PUT "/v1/backup-storage-targets/$existing_id" "$(cat "$payload_file")" "$target_json" "$target_err")"
  target_action="update"
else
  target_status="$(api_json POST "/v1/backup-storage-targets" "$(cat "$payload_file")" "$target_json" "$target_err")"
  target_action="create"
fi
if [[ "$target_status" != 2* ]]; then
  fail "durable_backup_target.$target_action" "expected 2xx, got HTTP $target_status; see durable-backup-target.stderr"
fi
target_id="$(json_get_file "$target_json" id)"
if [[ -z "$target_id" ]]; then
  fail "durable_backup_target.$target_action" "target response did not include id"
fi
if node -e 'const fs=require("fs"); const target=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); process.exit("secret_access_key" in target ? 0 : 1);' "$target_json"; then
  fail "durable_backup_target.$target_action" "target response leaked secret_access_key"
fi
printf '%s' "$target_id" >"$ARTIFACT_DIR/durable-backup-target-id"
pass "durable_backup_target.$target_action" "$target_id"

test_json="$ARTIFACT_DIR/durable-backup-target-test.json"
test_err="$ARTIFACT_DIR/durable-backup-target-test.stderr"
test_status="$(api_json POST "/v1/backup-storage-targets/$target_id/test" "" "$test_json" "$test_err")"
if [[ "$test_status" != 2* ]]; then
  fail "durable_backup_target.test" "expected 2xx, got HTTP $test_status; see durable-backup-target-test.stderr"
fi
if ! node -e '
const fs = require("fs");
const target = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (target.id !== process.argv[2]) throw new Error(`id=${target.id}`);
if ("secret_access_key" in target) throw new Error("secret_access_key leaked");
if (target.last_test_status !== "passed") throw new Error(`last_test_status=${target.last_test_status}`);
if (!target.last_tested_at) throw new Error("last_tested_at missing");
if (target.durable_off_host !== true) throw new Error(`durable_off_host=${target.durable_off_host} (${target.readiness_status || "unknown"})`);
if (target.recovery_ready !== true) throw new Error(`recovery_ready=${target.recovery_ready} (${target.readiness_message || target.readiness_status || "unknown"})`);
if (target.readiness_status !== "off-host-ready") throw new Error(`readiness_status=${target.readiness_status}`);
' "$test_json" "$target_id" >"$ARTIFACT_DIR/durable-backup-target-test-check.out" 2>"$ARTIFACT_DIR/durable-backup-target-test-check.stderr"; then
  fail "durable_backup_target.ready" "target did not pass hosted-grade readiness; see durable-backup-target-test-check.stderr"
fi
pass "durable_backup_target.ready" "target=$target_id endpoint=$endpoint bucket=$bucket"
