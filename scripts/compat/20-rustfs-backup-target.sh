#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

if ! compat_bool "${SUPADUPA_COMPAT_RUSTFS_BACKUP_TARGET:-false}"; then
  skip "rustfs.enabled" "set SUPADUPA_COMPAT_RUSTFS_BACKUP_TARGET=true to run"
  exit 0
fi

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool curl
require_tool docker
require_tool node
ensure_token

token="$(read_secret_file "$ARTIFACT_DIR/token")"
api_base="${SUPADUPA_API_URL%/}"
image="${SUPADUPA_COMPAT_RUSTFS_IMAGE:-rustfs/rustfs:latest}"
access_key="${SUPADUPA_COMPAT_RUSTFS_ACCESS_KEY:-rustfsadmin}"
secret_key="${SUPADUPA_COMPAT_RUSTFS_SECRET_KEY:-rustfsadmin-secret}"
container_name="supadupa-compat-rustfs-${SUPADUPA_TEST_REF}-$$"
bucket="supadupa-compat-${SUPADUPA_TEST_REF}-$$"
target_id=""
keep_target=false
if compat_bool "${SUPADUPA_COMPAT_RUSTFS_KEEP_TARGET:-false}"; then
  keep_target=true
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

assert_backup_payload() {
  local test_name="$1"
  local file="$2"
  local expected_kind="$3"
  local expected_target_id="$4"

  if ! node -e '
const fs = require("fs");
const backup = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const expectedKind = process.argv[2];
const expectedTargetID = process.argv[3];
if (!backup.id) throw new Error("id missing");
if (backup.kind !== expectedKind) throw new Error(`kind=${backup.kind}; expected ${expectedKind}`);
if (backup.status !== "completed") throw new Error(`status=${backup.status}`);
if (backup.storage_target_id !== expectedTargetID) throw new Error(`storage_target_id=${backup.storage_target_id}; expected ${expectedTargetID}`);
if (!backup.remote_location) throw new Error("remote_location missing");
if (!backup.size_bytes || Number(backup.size_bytes) <= 0) throw new Error("size_bytes missing");
if (!backup.checksum_sha256 || String(backup.checksum_sha256).length !== 64) throw new Error("checksum missing");
if (!backup.verified_at) throw new Error("verified_at missing");
if (!backup.started_at) throw new Error("started_at missing");
if (!backup.finished_at) throw new Error("finished_at missing");
if (Date.parse(backup.finished_at) < Date.parse(backup.started_at)) throw new Error("finished_at before started_at");
' "$file" "$expected_kind" "$expected_target_id"; then
    fail "$test_name" "backup metadata was malformed"
  fi
}

assert_target_listed() {
  local label="$1"
  local expected_id="$2"
  local out="$ARTIFACT_DIR/rustfs-targets-$label.json"
  local err="$ARTIFACT_DIR/rustfs-targets-$label.stderr"
  local status

  status="$(api_json GET "/v1/backup-storage-targets" "" "$out" "$err")"
  if [[ "$status" != 2* ]]; then
    fail "rustfs.target_list.$label" "expected 2xx, got HTTP $status"
  fi
  if ! node -e '
const fs = require("fs");
const targets = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const expectedID = process.argv[2];
if (!Array.isArray(targets)) throw new Error("response is not an array");
const target = targets.find((item) => item && item.id === expectedID);
if (!target) throw new Error(`target ${expectedID} missing`);
if ("secret_access_key" in target) throw new Error("secret_access_key leaked");
if (!target.endpoint || !target.bucket || !target.region) throw new Error("connection metadata missing");
if (target.durable_off_host !== false) throw new Error("loopback RustFS target should not be durable_off_host");
if (target.recovery_ready !== false) throw new Error("loopback RustFS target should not be recovery_ready");
if (target.readiness_status !== "local-or-loopback") throw new Error(`unexpected readiness_status ${target.readiness_status}`);
' "$out" "$expected_id"; then
    fail "rustfs.target_list.$label" "target was missing from list or leaked secrets"
  fi
  pass "rustfs.target_list.$label" "$expected_id"
}

cleanup_rustfs() {
  if [[ "$keep_target" == "true" ]]; then
    return 0
  fi
  if [[ -n "$target_id" ]]; then
    api_json DELETE "/v1/backup-storage-targets/$target_id" "" "$ARTIFACT_DIR/rustfs-target-delete.json" "$ARTIFACT_DIR/rustfs-target-delete.stderr" >/dev/null || true
  fi
  if [[ -s "$ARTIFACT_DIR/rustfs-endpoint" ]]; then
    endpoint="$(cat "$ARTIFACT_DIR/rustfs-endpoint")"
    SUPABASE_S3_ENDPOINT="$endpoint" \
      SUPABASE_S3_ACCESS_KEY="$access_key" \
      SUPABASE_S3_SECRET_KEY="$secret_key" \
      SUPADUPA_S3_BUCKET="$bucket" \
      SUPADUPA_S3_ACTION="delete" \
      node "$SCRIPT_DIR/s3-bucket-admin.mjs" >"$ARTIFACT_DIR/rustfs-bucket-delete.out" 2>"$ARTIFACT_DIR/rustfs-bucket-delete.stderr" || true
  fi
  docker rm -f "$container_name" >"$ARTIFACT_DIR/rustfs-container-rm.out" 2>"$ARTIFACT_DIR/rustfs-container-rm.stderr" || true
}
trap cleanup_rustfs EXIT

if docker run -d --rm \
  --name "$container_name" \
  -p 127.0.0.1::9000 \
  -e RUSTFS_ACCESS_KEY="$access_key" \
  -e RUSTFS_SECRET_KEY="$secret_key" \
  "$image" \
  /data >"$ARTIFACT_DIR/rustfs-container.id" 2>"$ARTIFACT_DIR/rustfs-container.stderr"; then
  pass "rustfs.container_start" "$image"
else
  fail "rustfs.container_start" "failed to start RustFS; see rustfs-container.stderr"
fi

rustfs_port=""
for _ in $(seq 1 60); do
  rustfs_port="$(docker port "$container_name" 9000/tcp 2>/dev/null | sed -n 's/.*://p' | head -n 1)"
  if [[ -n "$rustfs_port" ]]; then
    endpoint="http://127.0.0.1:$rustfs_port"
    if curl -sS -o "$ARTIFACT_DIR/rustfs-root.body" -w '%{http_code}' "$endpoint" >"$ARTIFACT_DIR/rustfs-root.status" 2>"$ARTIFACT_DIR/rustfs-root.stderr"; then
      status="$(cat "$ARTIFACT_DIR/rustfs-root.status")"
      case "$status" in
        200|301|302|400|403) break ;;
      esac
    fi
  fi
  sleep 1
done
if [[ -z "$rustfs_port" ]]; then
  fail "rustfs.container_ready" "RustFS port was not published"
fi
endpoint="http://127.0.0.1:$rustfs_port"
printf '%s' "$endpoint" >"$ARTIFACT_DIR/rustfs-endpoint"
pass "rustfs.container_ready" "$endpoint"

if SUPABASE_S3_ENDPOINT="$endpoint" \
  SUPABASE_S3_ACCESS_KEY="$access_key" \
  SUPABASE_S3_SECRET_KEY="$secret_key" \
  SUPADUPA_S3_BUCKET="$bucket" \
  SUPADUPA_S3_ACTION="create" \
  node "$SCRIPT_DIR/s3-bucket-admin.mjs" >"$ARTIFACT_DIR/rustfs-bucket-create.out" 2>"$ARTIFACT_DIR/rustfs-bucket-create.stderr"; then
  pass "rustfs.bucket_create" "$bucket"
else
  fail "rustfs.bucket_create" "failed to create RustFS bucket; see rustfs-bucket-create.stderr"
fi

target_payload="$ARTIFACT_DIR/rustfs-target-payload.json"
node -e '
const payload = {
  name: `compat-rustfs-${process.argv[2]}`,
  type: "s3",
  endpoint: process.argv[1],
  region: "us-east-1",
  bucket: process.argv[3],
  prefix: `rustfs/${process.argv[2]}`,
  access_key_id: process.argv[4],
  secret_access_key: process.argv[5],
  force_path_style: true,
  default: true,
};
process.stdout.write(JSON.stringify(payload));
' "$endpoint" "$SUPADUPA_TEST_REF" "$bucket" "$access_key" "$secret_key" >"$target_payload"

target_json="$ARTIFACT_DIR/rustfs-target.json"
target_err="$ARTIFACT_DIR/rustfs-target.stderr"
target_status="$(api_json POST "/v1/backup-storage-targets" "$(cat "$target_payload")" "$target_json" "$target_err")"
if [[ "$target_status" != 2* ]]; then
  fail "rustfs.target_create" "expected 2xx, got HTTP $target_status; see rustfs-target.stderr"
fi
target_id="$(json_get_file "$target_json" id)"
if [[ -z "$target_id" ]] || jq -e 'has("secret_access_key")' "$target_json" >/dev/null 2>&1; then
  fail "rustfs.target_create" "target response missing id or leaked secret"
fi
printf '%s' "$target_id" >"$ARTIFACT_DIR/rustfs-target-id"
printf '%s' "$container_name" >"$ARTIFACT_DIR/rustfs-container-name"
printf '%s' "$bucket" >"$ARTIFACT_DIR/rustfs-bucket"
pass "rustfs.target_create" "$target_id"
assert_target_listed "after_create" "$target_id"

target_test_json="$ARTIFACT_DIR/rustfs-target-test.json"
target_test_err="$ARTIFACT_DIR/rustfs-target-test.stderr"
target_test_status="$(api_json POST "/v1/backup-storage-targets/$target_id/test" "" "$target_test_json" "$target_test_err")"
if [[ "$target_test_status" != 2* ]]; then
  fail "rustfs.target_test" "expected 2xx, got HTTP $target_test_status; see rustfs-target-test.stderr"
fi
if ! node -e '
const fs = require("fs");
const target = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (target.last_test_status !== "passed") throw new Error(`last_test_status=${target.last_test_status}`);
if (!target.last_tested_at) throw new Error("last_tested_at missing");
if ("secret_access_key" in target) throw new Error("secret_access_key leaked");
' "$target_test_json"; then
  fail "rustfs.target_test" "target test response was malformed"
fi
pass "rustfs.target_test" "server-side S3 probe passed"
assert_target_listed "after_test" "$target_id"

recoverability_json="$ARTIFACT_DIR/rustfs-recoverability.json"
recoverability_err="$ARTIFACT_DIR/rustfs-recoverability.stderr"
recoverability_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/recoverability" "" "$recoverability_json" "$recoverability_err")"
if [[ "$recoverability_status" != 2* ]]; then
  fail "rustfs.local_target_recoverability" "expected 2xx, got HTTP $recoverability_status"
fi
if ! node -e '
const fs = require("fs");
const status = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (status.off_host_backup_configured) throw new Error("loopback RustFS counted as off_host_backup_configured");
if (status.restore_to_time_available) throw new Error("loopback RustFS made restore_to_time_available true");
' "$recoverability_json"; then
  fail "rustfs.local_target_recoverability" "loopback RustFS incorrectly satisfied off-host gates"
fi
pass "rustfs.local_target_recoverability" "loopback RustFS did not satisfy off-host gates"

created_ref="$(cat "$ARTIFACT_DIR/created-project" 2>/dev/null || true)"
run_project_artifact_checks=false
if compat_bool "${SUPADUPA_COMPAT_CREATE_PROJECT:-false}" && [[ "$created_ref" == "$SUPADUPA_TEST_REF" ]]; then
  run_project_artifact_checks=true
fi
strict_recovery_ready_targets=false
if compat_bool "${SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS:-false}"; then
  strict_recovery_ready_targets=true
fi

restore_project_policies() {
  if [[ "$run_project_artifact_checks" != "true" ]]; then
    return 0
  fi
  if [[ -s "$ARTIFACT_DIR/rustfs-original-backup-policy.json" ]]; then
    node -e '
const fs = require("fs");
const policy = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const payload = {
  enabled: Boolean(policy.enabled),
  schedule: policy.schedule || "daily",
  kind: policy.kind || "logical",
};
if (policy.storage_target_id) payload.storage_target_id = policy.storage_target_id;
process.stdout.write(JSON.stringify(payload));
' "$ARTIFACT_DIR/rustfs-original-backup-policy.json" >"$ARTIFACT_DIR/rustfs-restore-backup-policy-payload.json" 2>"$ARTIFACT_DIR/rustfs-restore-backup-policy-payload.stderr" &&
      api_json PUT "/v1/projects/$SUPADUPA_TEST_REF/backups/policy" "$(cat "$ARTIFACT_DIR/rustfs-restore-backup-policy-payload.json")" "$ARTIFACT_DIR/rustfs-restore-backup-policy.json" "$ARTIFACT_DIR/rustfs-restore-backup-policy.stderr" >/dev/null || true
  fi
  if [[ -s "$ARTIFACT_DIR/rustfs-original-pitr-policy.json" ]]; then
    node -e '
const fs = require("fs");
const policy = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const payload = {
  enabled: Boolean(policy.enabled),
  archive_bucket: policy.archive_bucket || "",
  retention_days: Number(policy.retention_days || 7),
};
process.stdout.write(JSON.stringify(payload));
' "$ARTIFACT_DIR/rustfs-original-pitr-policy.json" >"$ARTIFACT_DIR/rustfs-restore-pitr-policy-payload.json" 2>"$ARTIFACT_DIR/rustfs-restore-pitr-policy-payload.stderr" &&
      api_json PUT "/v1/projects/$SUPADUPA_TEST_REF/pitr/policy" "$(cat "$ARTIFACT_DIR/rustfs-restore-pitr-policy-payload.json")" "$ARTIFACT_DIR/rustfs-restore-pitr-policy.json" "$ARTIFACT_DIR/rustfs-restore-pitr-policy.stderr" >/dev/null || true
  fi
  if [[ -s "$ARTIFACT_DIR/rustfs-original-org-features.json" && -n "${SUPADUPA_TEST_ORG_ID:-}" ]]; then
    node -e '
const fs = require("fs");
const features = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
process.stdout.write(JSON.stringify({ overrides: features.overrides || {} }));
' "$ARTIFACT_DIR/rustfs-original-org-features.json" >"$ARTIFACT_DIR/rustfs-restore-org-features-payload.json" 2>"$ARTIFACT_DIR/rustfs-restore-org-features-payload.stderr" &&
      api_json PUT "/v1/orgs/$SUPADUPA_TEST_ORG_ID/features" "$(cat "$ARTIFACT_DIR/rustfs-restore-org-features-payload.json")" "$ARTIFACT_DIR/rustfs-restore-org-features.json" "$ARTIFACT_DIR/rustfs-restore-org-features.stderr" >/dev/null || true
  fi
}

if [[ "$run_project_artifact_checks" == "true" ]]; then
  api_json GET "/v1/projects/$SUPADUPA_TEST_REF/backups/policy" "" "$ARTIFACT_DIR/rustfs-original-backup-policy.json" "$ARTIFACT_DIR/rustfs-original-backup-policy.stderr" >/dev/null || true
  api_json GET "/v1/projects/$SUPADUPA_TEST_REF/pitr/policy" "" "$ARTIFACT_DIR/rustfs-original-pitr-policy.json" "$ARTIFACT_DIR/rustfs-original-pitr-policy.stderr" >/dev/null || true
  if [[ -n "${SUPADUPA_TEST_ORG_ID:-}" ]]; then
    org_features_status="$(api_json GET "/v1/orgs/$SUPADUPA_TEST_ORG_ID/features" "" "$ARTIFACT_DIR/rustfs-original-org-features.json" "$ARTIFACT_DIR/rustfs-original-org-features.stderr")"
    if [[ "$org_features_status" == 2* ]]; then
      if ! node -e '
const fs = require("fs");
const features = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const overrides = { ...(features.overrides || {}), pitr: true };
process.stdout.write(JSON.stringify({ overrides }));
' "$ARTIFACT_DIR/rustfs-original-org-features.json" >"$ARTIFACT_DIR/rustfs-enable-pitr-feature-payload.json" 2>"$ARTIFACT_DIR/rustfs-enable-pitr-feature-payload.stderr"; then
        fail "rustfs.pitr_feature" "failed to build temporary feature override payload"
      fi
      pitr_feature_json="$ARTIFACT_DIR/rustfs-enable-pitr-feature.json"
      pitr_feature_err="$ARTIFACT_DIR/rustfs-enable-pitr-feature.stderr"
      pitr_feature_status="$(api_json PUT "/v1/orgs/$SUPADUPA_TEST_ORG_ID/features" "$(cat "$ARTIFACT_DIR/rustfs-enable-pitr-feature-payload.json")" "$pitr_feature_json" "$pitr_feature_err")"
      if [[ "$pitr_feature_status" != 2* ]]; then
        fail "rustfs.pitr_feature" "expected 2xx, got HTTP $pitr_feature_status"
      fi
      pass "rustfs.pitr_feature" "temporarily enabled pitr for disposable project"
    else
      skip "rustfs.pitr_feature" "SUPADUPA_TEST_ORG_ID features unavailable; WAL upload may be skipped"
    fi
  else
    skip "rustfs.pitr_feature" "SUPADUPA_TEST_ORG_ID is not set"
  fi
  trap 'restore_project_policies; cleanup_rustfs' EXIT
fi

if [[ "$run_project_artifact_checks" == "true" ]]; then
  logical_policy_payload="$ARTIFACT_DIR/rustfs-logical-policy-payload.json"
  node -e '
const payload = {
  enabled: true,
  schedule: "daily",
  kind: "logical",
  storage_target_id: process.argv[1],
};
process.stdout.write(JSON.stringify(payload));
' "$target_id" >"$logical_policy_payload"
  logical_policy_json="$ARTIFACT_DIR/rustfs-logical-policy.json"
  logical_policy_err="$ARTIFACT_DIR/rustfs-logical-policy.stderr"
  logical_policy_status="$(api_json PUT "/v1/projects/$SUPADUPA_TEST_REF/backups/policy" "$(cat "$logical_policy_payload")" "$logical_policy_json" "$logical_policy_err")"
  if [[ "$logical_policy_status" != 2* ]]; then
    fail "rustfs.logical_policy" "expected 2xx, got HTTP $logical_policy_status"
  fi
  pass "rustfs.logical_policy" "project policy uses RustFS target for logical backups"

  backup_json="$ARTIFACT_DIR/rustfs-project-backup.json"
  backup_err="$ARTIFACT_DIR/rustfs-project-backup.stderr"
  backup_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/backups" "" "$backup_json" "$backup_err")"
  if [[ "$backup_status" != "201" ]]; then
    fail "rustfs.project_backup_upload" "expected HTTP 201, got HTTP $backup_status"
  fi
  assert_backup_payload "rustfs.project_backup_upload" "$backup_json" "logical" "$target_id"
  if ! node -e '
const fs = require("fs");
const backup = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (backup.status !== "completed") throw new Error(`status=${backup.status}`);
if (!backup.remote_location) throw new Error("remote_location missing");
if (backup.storage_target_id !== process.argv[2]) throw new Error(`storage_target_id=${backup.storage_target_id}`);
if (!backup.checksum_sha256 || backup.checksum_sha256.length !== 64) throw new Error("checksum missing");
' "$backup_json" "$target_id"; then
    fail "rustfs.project_backup_upload" "project backup metadata was malformed"
  fi
  pass "rustfs.project_backup_upload" "logical backup uploaded through RustFS"
else
  skip "rustfs.project_backup_upload" "only runs against a project created by this compat run"
fi

if [[ "$run_project_artifact_checks" == "true" ]]; then
  physical_policy_payload="$ARTIFACT_DIR/rustfs-physical-policy-payload.json"
  node -e '
const payload = {
  enabled: true,
  schedule: "daily",
  kind: "physical",
  storage_target_id: process.argv[1],
};
process.stdout.write(JSON.stringify(payload));
' "$target_id" >"$physical_policy_payload"

  physical_policy_json="$ARTIFACT_DIR/rustfs-physical-policy.json"
  physical_policy_err="$ARTIFACT_DIR/rustfs-physical-policy.stderr"
  physical_policy_status="$(api_json PUT "/v1/projects/$SUPADUPA_TEST_REF/backups/policy" "$(cat "$physical_policy_payload")" "$physical_policy_json" "$physical_policy_err")"
  if [[ "$physical_policy_status" != 2* ]]; then
    fail "rustfs.physical_policy" "expected 2xx, got HTTP $physical_policy_status"
  fi
  pass "rustfs.physical_policy" "project policy uses RustFS target for physical backups"

  physical_backup_json="$ARTIFACT_DIR/rustfs-physical-backup.json"
  physical_backup_err="$ARTIFACT_DIR/rustfs-physical-backup.stderr"
  physical_backup_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/backups" "" "$physical_backup_json" "$physical_backup_err")"
  case "$physical_backup_status" in
    201)
      if [[ "$strict_recovery_ready_targets" == "true" ]]; then
        fail "rustfs.physical_backup_upload" "strict recovery-ready target guard allowed loopback RustFS upload"
      fi
      assert_backup_payload "rustfs.physical_backup_upload" "$physical_backup_json" "physical" "$target_id"
      if ! node -e '
const fs = require("fs");
const backup = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (backup.kind !== "physical") throw new Error(`kind=${backup.kind}`);
if (backup.status !== "completed") throw new Error(`status=${backup.status}`);
if (!backup.remote_location) throw new Error("remote_location missing");
if (backup.storage_target_id !== process.argv[2]) throw new Error(`storage_target_id=${backup.storage_target_id}`);
if (!backup.size_bytes || backup.size_bytes <= 0) throw new Error("size_bytes missing");
if (!backup.checksum_sha256 || backup.checksum_sha256.length !== 64) throw new Error("checksum missing");
if (!backup.verified_at) throw new Error("verified_at missing");
' "$physical_backup_json" "$target_id"; then
        fail "rustfs.physical_backup_upload" "physical backup metadata was malformed"
      fi
      pass "rustfs.physical_backup_upload" "physical backup uploaded through RustFS"
      ;;
    409)
      if [[ "$strict_recovery_ready_targets" == "true" ]] && grep -qi "not recovery-ready" "$physical_backup_json"; then
        pass "rustfs.physical_backup_upload" "strict guard rejected loopback RustFS physical upload"
      elif grep -qi "physical backup command is not configured" "$physical_backup_json" &&
        ! compat_bool "${SUPADUPA_COMPAT_RUSTFS_REQUIRE_PHYSICAL:-false}"; then
        skip "rustfs.physical_backup_upload" "physical backup command is not configured"
      else
        fail "rustfs.physical_backup_upload" "expected HTTP 201, got HTTP $physical_backup_status: $(cat "$physical_backup_json")"
      fi
      ;;
    *)
      fail "rustfs.physical_backup_upload" "expected HTTP 201, got HTTP $physical_backup_status"
      ;;
  esac

  pitr_policy_json="$ARTIFACT_DIR/rustfs-pitr-policy.json"
  pitr_policy_err="$ARTIFACT_DIR/rustfs-pitr-policy.stderr"
  pitr_policy_status="$(api_json PUT "/v1/projects/$SUPADUPA_TEST_REF/pitr/policy" '{"enabled":true,"archive_bucket":"","retention_days":7}' "$pitr_policy_json" "$pitr_policy_err")"
  if [[ "$pitr_policy_status" == "403" ]] && grep -qi "feature flag pitr is disabled" "$pitr_policy_json" &&
    ! compat_bool "${SUPADUPA_COMPAT_RUSTFS_REQUIRE_WAL:-false}"; then
    skip "rustfs.pitr_policy" "pitr feature flag is disabled for org"
    skip "rustfs.wal_archive_upload" "pitr feature flag is disabled for org"
  elif [[ "$pitr_policy_status" != 2* ]]; then
    if [[ "$strict_recovery_ready_targets" == "true" ]] && grep -qi "archive_bucket is required" "$pitr_policy_json"; then
      pass "rustfs.pitr_policy" "strict guard rejected PITR bucket derivation from loopback RustFS target"

      strict_pitr_payload="$ARTIFACT_DIR/rustfs-pitr-policy-explicit-payload.json"
      node -e '
const payload = {
  enabled: true,
  archive_bucket: `s3://${process.argv[1]}/rustfs/${process.argv[2]}/projects/${process.argv[2]}/wal`,
  retention_days: 7,
};
process.stdout.write(JSON.stringify(payload));
' "$bucket" "$SUPADUPA_TEST_REF" >"$strict_pitr_payload"
      strict_pitr_json="$ARTIFACT_DIR/rustfs-pitr-policy-explicit.json"
      strict_pitr_err="$ARTIFACT_DIR/rustfs-pitr-policy-explicit.stderr"
      strict_pitr_status="$(api_json PUT "/v1/projects/$SUPADUPA_TEST_REF/pitr/policy" "$(cat "$strict_pitr_payload")" "$strict_pitr_json" "$strict_pitr_err")"
      if [[ "$strict_pitr_status" != 2* ]]; then
        fail "rustfs.pitr_policy_explicit" "expected 2xx with explicit archive bucket, got HTTP $strict_pitr_status"
      fi
      pass "rustfs.pitr_policy_explicit" "explicit WAL bucket accepted for WAL guard check"

      wal_json="$ARTIFACT_DIR/rustfs-wal-archive.json"
      wal_err="$ARTIFACT_DIR/rustfs-wal-archive.stderr"
      wal_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/pitr/wal" "" "$wal_json" "$wal_err")"
      if [[ "$wal_status" == "409" ]] && grep -qi "not recovery-ready" "$wal_json"; then
        pass "rustfs.wal_archive_upload" "strict guard rejected loopback RustFS WAL upload"
      else
        fail "rustfs.wal_archive_upload" "strict guard should reject loopback RustFS WAL upload, got HTTP $wal_status"
      fi
    else
      fail "rustfs.pitr_policy" "expected 2xx, got HTTP $pitr_policy_status"
    fi
  else
    if [[ "$strict_recovery_ready_targets" == "true" ]]; then
      fail "rustfs.pitr_policy" "strict recovery-ready target guard allowed PITR bucket derivation from loopback RustFS target"
    fi
    if ! node -e '
const fs = require("fs");
const policy = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (policy.enabled !== true) throw new Error("PITR not enabled");
if (!String(policy.archive_bucket || "").includes(process.argv[2])) throw new Error(`archive_bucket=${policy.archive_bucket}`);
' "$pitr_policy_json" "$SUPADUPA_TEST_REF"; then
      fail "rustfs.pitr_policy" "PITR policy did not derive a project WAL bucket"
    fi
    pass "rustfs.pitr_policy" "WAL bucket derived from RustFS target"

    wal_json="$ARTIFACT_DIR/rustfs-wal-archive.json"
    wal_err="$ARTIFACT_DIR/rustfs-wal-archive.stderr"
    wal_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/pitr/wal" "" "$wal_json" "$wal_err")"
    if [[ "$wal_status" != "201" ]]; then
      fail "rustfs.wal_archive_upload" "expected HTTP 201, got HTTP $wal_status"
    fi
    if ! node -e '
const fs = require("fs");
const archive = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (archive.status !== "archived") throw new Error(`status=${archive.status}`);
if (!/^[0-9A-F]{24}$/.test(archive.segment || "")) throw new Error(`segment=${archive.segment}`);
if (archive.segment_source !== "postgres") throw new Error(`segment_source=${archive.segment_source}`);
if (!archive.remote_location) throw new Error("remote_location missing");
if (archive.storage_target_id !== process.argv[2]) throw new Error(`storage_target_id=${archive.storage_target_id}`);
if (!archive.size_bytes || archive.size_bytes <= 0) throw new Error("size_bytes missing");
if (!archive.checksum_sha256 || archive.checksum_sha256.length !== 64) throw new Error("checksum missing");
if (!archive.verified_at) throw new Error("verified_at missing");
' "$wal_json" "$target_id"; then
      fail "rustfs.wal_archive_upload" "WAL archive metadata was malformed"
    fi
    pass "rustfs.wal_archive_upload" "WAL artifact uploaded through RustFS"
  fi

  final_recoverability_json="$ARTIFACT_DIR/rustfs-final-recoverability.json"
  final_recoverability_err="$ARTIFACT_DIR/rustfs-final-recoverability.stderr"
  final_recoverability_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/recoverability" "" "$final_recoverability_json" "$final_recoverability_err")"
  if [[ "$final_recoverability_status" != 2* ]]; then
    fail "rustfs.final_recoverability" "expected 2xx, got HTTP $final_recoverability_status"
  fi
  if ! node -e '
const fs = require("fs");
const status = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (status.off_host_backup_configured) throw new Error("loopback target counted as off_host_backup_configured after physical/WAL artifacts");
if (status.off_host_backup_verified) throw new Error("loopback target counted as off_host_backup_verified after physical/WAL artifacts");
if (status.wal_archive_off_host_verified) throw new Error("loopback target counted as off-host WAL after upload");
if (status.restore_to_time_available) throw new Error("loopback target made restore_to_time_available true");
' "$final_recoverability_json"; then
    fail "rustfs.final_recoverability" "loopback RustFS artifacts incorrectly satisfied off-host gates"
  fi
  pass "rustfs.final_recoverability" "loopback artifacts still do not satisfy off-host gates"
else
  skip "rustfs.physical_backup_upload" "only runs against a project created by this compat run"
  skip "rustfs.wal_archive_upload" "only runs against a project created by this compat run"
fi

if [[ "$keep_target" == "true" ]] || compat_bool "${SUPADUPA_COMPAT_RUSTFS_PLATFORM_BACKUP:-false}"; then
  if [[ "$keep_target" != "true" ]]; then
    fail "rustfs.platform_backup_upload" "platform backup validation requires SUPADUPA_COMPAT_RUSTFS_KEEP_TARGET=true so the control-plane backup remains restorable"
  fi
  platform_backup_json="$ARTIFACT_DIR/rustfs-platform-backup.json"
  platform_backup_err="$ARTIFACT_DIR/rustfs-platform-backup.stderr"
  platform_backup_status="$(api_json POST "/v1/platform/backups" "" "$platform_backup_json" "$platform_backup_err")"
  if [[ "$platform_backup_status" != "201" ]]; then
    fail "rustfs.platform_backup_upload" "expected HTTP 201, got HTTP $platform_backup_status"
  fi
  assert_backup_payload "rustfs.platform_backup_upload" "$platform_backup_json" "control-plane" "$target_id"
  if ! node -e '
const fs = require("fs");
const backup = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (backup.kind !== "control-plane") throw new Error(`kind=${backup.kind}`);
if (backup.status !== "completed") throw new Error(`status=${backup.status}`);
if (!backup.remote_location) throw new Error("remote_location missing");
if (backup.storage_target_id !== process.argv[2]) throw new Error(`storage_target_id=${backup.storage_target_id}`);
if (!backup.size_bytes || backup.size_bytes <= 0) throw new Error("size_bytes missing");
if (!backup.checksum_sha256 || backup.checksum_sha256.length !== 64) throw new Error("checksum missing");
if (!backup.verified_at) throw new Error("verified_at missing");
' "$platform_backup_json" "$target_id"; then
    fail "rustfs.platform_backup_upload" "platform backup metadata was malformed"
  fi
  pass "rustfs.platform_backup_upload" "control-plane backup uploaded through RustFS"
else
  skip "rustfs.platform_backup_upload" "set SUPADUPA_COMPAT_RUSTFS_KEEP_TARGET=true and SUPADUPA_COMPAT_RUSTFS_PLATFORM_BACKUP=true to validate persistent control-plane backup upload"
fi

if [[ "$keep_target" == "true" ]]; then
  printf '%s' "$target_id" >"$ARTIFACT_DIR/rustfs-target-kept-id"
  pass "rustfs.keep_target" "target/container kept for later compat phases"
fi
