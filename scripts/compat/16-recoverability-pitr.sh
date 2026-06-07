#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool curl
require_tool node
ensure_token

token="$(read_secret_file "$ARTIFACT_DIR/token")"
api_base="${SUPADUPA_API_URL%/}"
pitr_semantics_validate=false
pitr_semantics_public_db_safe_url=""
pitr_semantics_db_password=""
pitr_semantics_before_marker=""
pitr_semantics_after_marker=""
pitr_semantics_target_unix=""

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

wait_public_db_ready() {
  local test_name="$1"
  local deadline=$((SECONDS + 120))
  while (( SECONDS < deadline )); do
    if PGPASSWORD="$pitr_semantics_db_password" psql "$pitr_semantics_public_db_safe_url" \
      -v ON_ERROR_STOP=1 \
      -Atq >"$ARTIFACT_DIR/$test_name.out" 2>"$ARTIFACT_DIR/$test_name.stderr" <<'SQL'; then
select 1;
SQL
      pass "$test_name" "public database reachable"
      return 0
    fi
    sleep 3
  done
  fail "$test_name" "public database did not become reachable"
}

recoverability_json="$ARTIFACT_DIR/recoverability.json"
recoverability_err="$ARTIFACT_DIR/recoverability.stderr"
recoverability_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/recoverability" "" "$recoverability_json" "$recoverability_err")"
if [[ "$recoverability_status" != 2* ]]; then
  fail "recoverability.get" "expected 2xx, got HTTP $recoverability_status; see $(basename "$recoverability_err")"
fi

if ! node -e '
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const required = [
  "project_ref",
  "status",
  "backup_policy_enabled",
  "off_host_backup_configured",
  "off_host_backup_verified",
  "pitr_enabled",
  "physical_backup_available",
  "restore_to_time_configured",
  "restore_to_time_available",
  "warnings",
  "recommendations"
];
for (const key of required) {
  if (!(key in payload)) {
    throw new Error(`missing ${key}`);
  }
}
if (payload.project_ref !== process.argv[2]) throw new Error(`project_ref=${payload.project_ref}`);
if (!Array.isArray(payload.warnings)) throw new Error("warnings is not an array");
if (!Array.isArray(payload.recommendations)) throw new Error("recommendations is not an array");
' "$recoverability_json" "$SUPADUPA_TEST_REF"; then
  fail "recoverability.shape" "recoverability response is missing required fields"
fi
recoverability_state="$(json_get_file_optional "$recoverability_json" status)"
restore_available="$(node -e 'const fs=require("fs"); const payload=JSON.parse(fs.readFileSync(process.argv[1], "utf8")); process.stdout.write(String(Boolean(payload.restore_to_time_available)));' "$recoverability_json")"
pass "recoverability.get" "status=$recoverability_state restore_to_time_available=$restore_available"

backup_targets_json="$ARTIFACT_DIR/recoverability-backup-targets.json"
backup_targets_err="$ARTIFACT_DIR/recoverability-backup-targets.stderr"
backup_targets_status="$(api_json GET "/v1/backup-storage-targets" "" "$backup_targets_json" "$backup_targets_err")"
if [[ "$backup_targets_status" != 2* ]]; then
  fail "recoverability.backup_targets" "expected 2xx, got HTTP $backup_targets_status; see $(basename "$backup_targets_err")"
fi
if ! node -e '
const fs = require("fs");
const targets = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (!Array.isArray(targets)) throw new Error("backup targets response is not an array");
for (const target of targets) {
  for (const key of ["id", "name", "type", "bucket", "secret_configured", "default", "durable_off_host", "recovery_ready", "readiness_status"]) {
    if (!(key in target)) throw new Error(`target missing ${key}`);
  }
  if (typeof target.durable_off_host !== "boolean") throw new Error("target durable_off_host is not boolean");
  if (typeof target.recovery_ready !== "boolean") throw new Error("target recovery_ready is not boolean");
  if (typeof target.readiness_status !== "string") throw new Error("target readiness_status is not string");
  if ("secret_access_key" in target) throw new Error("target leaked secret_access_key");
}
' "$backup_targets_json"; then
  fail "recoverability.backup_targets" "backup target list shape was invalid"
fi
backup_target_count="$(node -e 'const fs=require("fs"); const targets=JSON.parse(fs.readFileSync(process.argv[1], "utf8")); process.stdout.write(String(targets.length));' "$backup_targets_json")"
pass "recoverability.backup_targets" "count=$backup_target_count"

if ! node -e '
const fs = require("fs");
const recoverability = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const targets = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const bools = [
  "backup_policy_enabled",
  "off_host_backup_configured",
  "off_host_backup_verified",
  "pitr_enabled",
  "wal_archive_off_host_verified",
  "physical_backup_available",
  "restore_to_time_configured",
  "restore_to_time_available",
];
for (const key of bools) {
  if (typeof recoverability[key] !== "boolean") throw new Error(`${key} is not boolean`);
}
const allowed = new Set(["unprotected", "scheduled-pending", "local-backup-only", "off-host-backup-ready", "restore-to-time-ready"]);
if (!allowed.has(recoverability.status)) throw new Error(`unknown status ${recoverability.status}`);
if (targets.length === 0) {
  if (recoverability.off_host_backup_configured) throw new Error("off_host_backup_configured true without any target");
  if (!recoverability.warnings.some((warning) => /S3-compatible backup target/i.test(warning))) throw new Error("missing no-target warning");
}
if (recoverability.status === "local-backup-only") {
  if (!recoverability.latest_verified_backup) throw new Error("local-only status without latest_verified_backup");
  if (recoverability.off_host_backup_verified) throw new Error("local-only status but off_host_backup_verified true");
  if (recoverability.restore_to_time_available) throw new Error("local-only status but restore_to_time_available true");
}
if (!recoverability.restore_to_time_available && !recoverability.restore_to_time_unavailable) {
  throw new Error("restore unavailable reason is missing");
}
if (recoverability.restore_to_time_available) {
  for (const key of ["pitr_enabled", "wal_archive_off_host_verified", "physical_backup_available", "restore_to_time_configured"]) {
    if (!recoverability[key]) throw new Error(`restore available but ${key} false`);
  }
  if (recoverability.restore_to_time_unavailable) throw new Error("restore unavailable reason set while available");
}
' "$recoverability_json" "$backup_targets_json"; then
  fail "recoverability.gates" "recoverability readiness gates were inconsistent"
fi
pass "recoverability.gates" "status=$recoverability_state targets=$backup_target_count"

artifact_backup_target_id="$(cat "$ARTIFACT_DIR/durable-backup-target-id" 2>/dev/null || true)"
target_to_test="$(node -e '
const fs = require("fs");
const preferred = process.env.SUPADUPA_COMPAT_BACKUP_TARGET_ID || process.argv[2] || "";
const targets = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
let target = null;
if (preferred) target = targets.find((item) => item.id === preferred);
if (!target) target = targets.find((item) => item.default && item.secret_configured);
if (!target) target = targets.find((item) => item.secret_configured);
if (target) process.stdout.write(target.id);
' "$backup_targets_json" "$artifact_backup_target_id")"
if [[ -n "${SUPADUPA_COMPAT_BACKUP_TARGET_ID:-}" ]]; then
  if [[ -z "$target_to_test" || "$target_to_test" != "$SUPADUPA_COMPAT_BACKUP_TARGET_ID" ]]; then
    fail "recoverability.backup_target_select" "requested backup target $SUPADUPA_COMPAT_BACKUP_TARGET_ID was not found"
  fi
  if ! node -e '
const fs = require("fs");
const targets = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const target = targets.find((item) => item.id === process.argv[2]);
if (!target) throw new Error("target missing");
if (!target.secret_configured) throw new Error("target exists but secret_configured=false");
' "$backup_targets_json" "$target_to_test" >"$ARTIFACT_DIR/recoverability-backup-target-select.out" 2>"$ARTIFACT_DIR/recoverability-backup-target-select.stderr"; then
    fail "recoverability.backup_target_select" "requested target is not usable; see recoverability-backup-target-select.stderr"
  fi
  pass "recoverability.backup_target_select" "target=$target_to_test"
elif [[ -n "$artifact_backup_target_id" ]]; then
  if [[ -z "$target_to_test" || "$target_to_test" != "$artifact_backup_target_id" ]]; then
    fail "recoverability.backup_target_select" "artifact backup target $artifact_backup_target_id was not found"
  fi
  pass "recoverability.backup_target_select" "target=$target_to_test"
fi
temporary_target_id=""
temporary_s3_pid=""
cleanup_temporary_target() {
  if [[ -n "$temporary_target_id" ]]; then
    local cleanup_out="$ARTIFACT_DIR/recoverability-temp-target-delete.json"
    local cleanup_err="$ARTIFACT_DIR/recoverability-temp-target-delete.stderr"
    api_json DELETE "/v1/backup-storage-targets/$temporary_target_id" "" "$cleanup_out" "$cleanup_err" >/dev/null || true
  fi
  if [[ -n "$temporary_s3_pid" ]]; then
    kill "$temporary_s3_pid" 2>/dev/null || true
    wait "$temporary_s3_pid" 2>/dev/null || true
  fi
}
if [[ -z "$target_to_test" ]] && compat_bool "${SUPADUPA_COMPAT_TEMP_BACKUP_TARGET:-true}"; then
  temp_s3_js="$ARTIFACT_DIR/recoverability-temp-s3.mjs"
  temp_s3_port="$ARTIFACT_DIR/recoverability-temp-s3.port"
  cat >"$temp_s3_js" <<'EOF'
import http from "node:http";

const objects = new Map();
const portFile = process.argv[2];

const server = http.createServer((request, response) => {
  const key = new URL(request.url || "/", "http://127.0.0.1").pathname;
  if (request.method === "PUT") {
    const chunks = [];
    request.on("data", (chunk) => chunks.push(chunk));
    request.on("end", () => {
      objects.set(key, Buffer.concat(chunks));
      response.writeHead(200);
      response.end();
    });
    return;
  }
  if (request.method === "GET") {
    if (!objects.has(key)) {
      response.writeHead(404);
      response.end();
      return;
    }
    response.writeHead(200);
    response.end(objects.get(key));
    return;
  }
  if (request.method === "DELETE") {
    objects.delete(key);
    response.writeHead(204);
    response.end();
    return;
  }
  response.writeHead(405);
  response.end();
});

server.listen(0, "127.0.0.1", () => {
  const address = server.address();
  process.stdout.write(String(address.port));
});
EOF
  node "$temp_s3_js" "$temp_s3_port" >"$temp_s3_port" 2>"$ARTIFACT_DIR/recoverability-temp-s3.stderr" &
  temporary_s3_pid="$!"
  trap cleanup_temporary_target EXIT
  for _ in $(seq 1 50); do
    if [[ -s "$temp_s3_port" ]]; then
      break
    fi
    sleep 0.1
  done
  if [[ ! -s "$temp_s3_port" ]]; then
    fail "recoverability.temp_backup_target" "temporary S3-compatible server did not start"
  fi
  temp_s3_endpoint="http://127.0.0.1:$(cat "$temp_s3_port")"
  temp_target_payload="$ARTIFACT_DIR/recoverability-temp-target-payload.json"
  node -e '
  const payload = {
  name: `compat-temp-${process.argv[2]}`,
  type: "s3",
  endpoint: process.argv[1],
  region: "auto",
  bucket: "supadupa-compat",
  prefix: `checks/${process.argv[2]}`,
  access_key_id: "compat-access",
  secret_access_key: "compat-secret",
  force_path_style: true,
  default: false,
};
process.stdout.write(JSON.stringify(payload));
' "$temp_s3_endpoint" "$SUPADUPA_TEST_REF" >"$temp_target_payload"
  temp_target_json="$ARTIFACT_DIR/recoverability-temp-target.json"
  temp_target_err="$ARTIFACT_DIR/recoverability-temp-target.stderr"
  temp_target_status="$(api_json POST "/v1/backup-storage-targets" "$(cat "$temp_target_payload")" "$temp_target_json" "$temp_target_err")"
  if [[ "$temp_target_status" != 2* ]]; then
    fail "recoverability.temp_backup_target" "expected 2xx create, got HTTP $temp_target_status; see $(basename "$temp_target_err")"
  fi
  if ! node -e '
const fs = require("fs");
const target = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (!target.id) throw new Error("missing id");
if (target.secret_access_key) throw new Error("secret_access_key leaked");
if (target.secret_configured !== true) throw new Error("secret_configured not true");
if (target.default) throw new Error("temporary target must not become platform default");
process.stdout.write(target.id);
' "$temp_target_json" >"$ARTIFACT_DIR/recoverability-temp-target.id" 2>"$ARTIFACT_DIR/recoverability-temp-target-check.stderr"; then
    fail "recoverability.temp_backup_target" "temporary target response was invalid"
  fi
  temporary_target_id="$(cat "$ARTIFACT_DIR/recoverability-temp-target.id")"
  target_to_test="$temporary_target_id"
  pass "recoverability.temp_backup_target" "created disposable S3-compatible target"
fi
if [[ -z "$target_to_test" ]]; then
  skip "recoverability.backup_target_test" "no configured backup target available to probe"
else
  target_test_json="$ARTIFACT_DIR/recoverability-backup-target-test.json"
  target_test_err="$ARTIFACT_DIR/recoverability-backup-target-test.stderr"
  target_test_status="$(api_json POST "/v1/backup-storage-targets/$target_to_test/test" "" "$target_test_json" "$target_test_err")"
  if [[ "$target_test_status" != 2* ]]; then
    fail "recoverability.backup_target_test" "expected 2xx, got HTTP $target_test_status; see $(basename "$target_test_err")"
  fi
  if ! node -e '
const fs = require("fs");
const target = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (target.id !== process.argv[2]) throw new Error(`target id=${target.id}`);
if (target.last_test_status !== "passed") throw new Error(`last_test_status=${target.last_test_status}`);
if (!target.last_tested_at) throw new Error("last_tested_at missing");
if ("secret_access_key" in target) throw new Error("target leaked secret_access_key");
' "$target_test_json" "$target_to_test"; then
    fail "recoverability.backup_target_test" "target test response was invalid"
  fi
  pass "recoverability.backup_target_test" "target=$target_to_test"
fi

if compat_bool "${SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE:-false}" && [[ -n "$target_to_test" ]]; then
  if ! node -e '
const fs = require("fs");
const dns = require("node:dns");
const os = require("node:os");
const net = require("node:net");
(async () => {
  const targets = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
  const targetID = process.argv[2] || "";
  const temporaryTargetID = process.argv[3] || "";
  const target = targets.find((item) => item.id === targetID);
  if (!target) throw new Error(`backup target ${targetID} was not found`);
  if (target.durable_off_host !== true) {
    throw new Error(`full PITR restore validation requires durable_off_host=true, got ${target.durable_off_host} (${target.readiness_status || "unknown"})`);
  }
  if (target.recovery_ready !== true) {
    throw new Error(`full PITR restore validation requires recovery_ready=true, got ${target.recovery_ready} (${target.readiness_message || target.readiness_status || "unknown"})`);
  }
  if (target.last_test_status !== "passed") {
    throw new Error(`full PITR restore validation requires a passed target test, got ${target.last_test_status || "missing"}`);
  }
  if (temporaryTargetID && targetID === temporaryTargetID) {
    throw new Error("full PITR restore validation requires a durable off-host target, not the disposable loopback target");
  }
  const endpoint = String(target.endpoint || "").trim();
  const host = endpoint ? new URL(endpoint).hostname.toLowerCase() : "";
  const localHosts = new Set(["localhost", "localhost.localdomain", "127.0.0.1", "::1", "0.0.0.0", "host.docker.internal", "host.containers.internal"]);
  const localIPs = new Set();
  for (const addrs of Object.values(os.networkInterfaces())) {
    for (const addr of addrs || []) {
      if (addr?.address) localIPs.add(addr.address.toLowerCase());
    }
  }
  function ipv4Private(ip) {
    const parts = ip.split(".").map((part) => Number(part));
    if (parts.length !== 4 || parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)) return false;
    return parts[0] === 10 ||
      (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31) ||
      (parts[0] === 192 && parts[1] === 168) ||
      (parts[0] === 169 && parts[1] === 254) ||
      parts[0] === 127 ||
      parts[0] === 0;
  }
  function ipv6Private(ip) {
    const value = ip.toLowerCase();
    return value === "::1" || value === "::" || value.startsWith("fc") || value.startsWith("fd") || value.startsWith("fe80:");
  }
  function rejectAddress(address) {
    const value = String(address || "").toLowerCase().replace(/^\[|\]$/g, "");
    return localIPs.has(value) || ipv4Private(value) || ipv6Private(value);
  }
  if (host && (localHosts.has(host) || rejectAddress(host))) {
    throw new Error(`full PITR restore validation requires a durable off-host target, got ${endpoint || "default AWS endpoint"}`);
  }
  if (host && net.isIP(host) === 0) {
    let addresses = [];
    try {
      addresses = (await dns.promises.lookup(host, { all: true, verbatim: true })).map((item) => item.address);
    } catch (_) {}
    for (const address of addresses) {
      if (rejectAddress(address)) {
        throw new Error(`full PITR restore validation requires a durable off-host target, ${endpoint} resolves to ${address}`);
      }
    }
  }
})().catch((error) => {
  console.error(error?.message || String(error));
  process.exit(1);
});
' "$backup_targets_json" "$target_to_test" "$temporary_target_id" >"$ARTIFACT_DIR/recoverability-full-pitr-target-precheck.out" 2>"$ARTIFACT_DIR/recoverability-full-pitr-target-precheck.stderr"; then
    fail "recoverability.full_pitr.target" "full restore validation requires a durable off-host backup target; see recoverability-full-pitr-target-precheck.stderr"
  fi
  pass "recoverability.full_pitr.target" "selected target is durable, recovery-ready, and not loopback"
fi

if [[ -n "$temporary_target_id" ]]; then
  temp_recoverability_json="$ARTIFACT_DIR/recoverability-temp-target-status.json"
  temp_recoverability_err="$ARTIFACT_DIR/recoverability-temp-target-status.stderr"
  temp_recoverability_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/recoverability" "" "$temp_recoverability_json" "$temp_recoverability_err")"
  if [[ "$temp_recoverability_status" != 2* ]]; then
    fail "recoverability.temp_backup_target_local_only" "expected 2xx, got HTTP $temp_recoverability_status"
  fi
  if ! node -e '
const fs = require("fs");
const status = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (status.off_host_backup_configured) throw new Error("loopback default target counted as off_host_backup_configured");
if (status.off_host_backup_verified) throw new Error("loopback target counted as off_host_backup_verified");
if (status.restore_to_time_available) throw new Error("loopback target made restore_to_time_available true");
if (!String(status.status || "").match(/^(local-backup-only|scheduled-pending|unprotected)$/)) throw new Error(`unexpected status ${status.status}`);
if (!Array.isArray(status.warnings) || !status.warnings.some((warning) => /local|loopback/i.test(warning))) {
  throw new Error(`missing local/loopback warning: ${JSON.stringify(status.warnings)}`);
}
' "$temp_recoverability_json" >"$ARTIFACT_DIR/recoverability-temp-target-status.out" 2>"$ARTIFACT_DIR/recoverability-temp-target-status-check.stderr"; then
    fail "recoverability.temp_backup_target_local_only" "loopback target incorrectly satisfied off-host gates"
  fi
  pass "recoverability.temp_backup_target_local_only" "loopback target did not satisfy off-host gates"

  created_ref="$(cat "$ARTIFACT_DIR/created-project" 2>/dev/null || true)"
  if compat_bool "${SUPADUPA_COMPAT_CREATE_PROJECT:-false}" && [[ "$created_ref" == "$SUPADUPA_TEST_REF" ]]; then
    temp_backup_json="$ARTIFACT_DIR/recoverability-temp-target-backup.json"
    temp_backup_err="$ARTIFACT_DIR/recoverability-temp-target-backup.stderr"
    temp_backup_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/backups" "" "$temp_backup_json" "$temp_backup_err")"
    if [[ "$temp_backup_status" != "201" ]]; then
      fail "recoverability.temp_backup_upload" "expected HTTP 201, got HTTP $temp_backup_status"
    fi
    if ! node -e '
const fs = require("fs");
const backup = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (backup.kind !== "logical") throw new Error(`kind=${backup.kind}`);
if (backup.status !== "completed") throw new Error(`status=${backup.status}`);
if (!backup.remote_location) throw new Error("remote_location missing");
if (backup.storage_target_id !== process.argv[2]) throw new Error(`storage_target_id=${backup.storage_target_id}`);
if (!backup.checksum_sha256 || backup.checksum_sha256.length !== 64) throw new Error("checksum missing");
if (!backup.verified_at) throw new Error("verified_at missing");
' "$temp_backup_json" "$temporary_target_id"; then
      fail "recoverability.temp_backup_upload" "backup uploaded to temporary target with malformed metadata"
    fi
    pass "recoverability.temp_backup_upload" "logical backup uploaded through disposable loopback target"
  else
    skip "recoverability.temp_backup_upload" "only runs against a project created by this compat run"
  fi
fi

hosted_backups_json="$ARTIFACT_DIR/recoverability-hosted-backups.json"
hosted_backups_err="$ARTIFACT_DIR/recoverability-hosted-backups.stderr"
hosted_backups_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/database/backups" "" "$hosted_backups_json" "$hosted_backups_err")"
if [[ "$hosted_backups_status" != 2* ]]; then
  fail "recoverability.hosted_backup_list" "expected 2xx, got HTTP $hosted_backups_status; see $(basename "$hosted_backups_err")"
fi
if ! node -e '
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (!Array.isArray(payload)) throw new Error("backup list is not an array");
for (const backup of payload) {
  for (const key of ["id", "project_ref", "kind", "size_bytes", "status", "created_at"]) {
    if (!(key in backup)) throw new Error(`backup missing ${key}`);
  }
  if (backup.project_ref !== process.argv[2]) throw new Error(`backup project_ref=${backup.project_ref}`);
  if (backup.status === "completed" && !backup.verified_at) throw new Error(`completed backup ${backup.id} missing verified_at`);
  if (backup.remote_location && !backup.storage_target_id) throw new Error(`remote backup ${backup.id} missing storage_target_id`);
}
' "$hosted_backups_json" "$SUPADUPA_TEST_REF"; then
  fail "recoverability.hosted_backup_list" "hosted-shaped backup list response is not an array"
fi
hosted_backup_count="$(node -e 'const fs=require("fs"); const payload=JSON.parse(fs.readFileSync(process.argv[1], "utf8")); process.stdout.write(String(payload.length));' "$hosted_backups_json")"
pass "recoverability.hosted_backup_list" "count=$hosted_backup_count"

if [[ "$restore_available" == "true" ]]; then
  skip "recoverability.restore_pitr_unavailable" "restore-to-time is already available"
  if ! compat_bool "${SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE:-false}"; then
    skip "recoverability.restore_pitr_available" "set SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE=true to run destructive restore-to-time validation"
  elif ! compat_bool "${SUPADUPA_COMPAT_CREATE_PROJECT:-false}" &&
    ! compat_bool "${SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE:-false}"; then
    skip "recoverability.restore_pitr_available" "set SUPADUPA_COMPAT_CREATE_PROJECT=true or SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE=true"
  else
    if ! compat_bool "${SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE:-false}"; then
      created_ref="$(cat "$ARTIFACT_DIR/created-project" 2>/dev/null || true)"
      if [[ "$created_ref" != "$SUPADUPA_TEST_REF" ]]; then
        skip "recoverability.restore_pitr_available" "restore validation only runs against a project created by this compat run"
      else
        run_pitr_restore_validate=true
      fi
    else
      run_pitr_restore_validate=true
    fi
    if compat_bool "${run_pitr_restore_validate:-false}"; then
      restore_target_unix="$(node -e '
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const value = payload.recovery_window_end || payload.latest_wal_archive?.created_at;
if (!value) throw new Error("no recovery target available");
const unix = Math.floor(Date.parse(value) / 1000);
if (!Number.isFinite(unix) || unix <= 0) throw new Error(`invalid recovery target ${value}`);
process.stdout.write(String(unix));
' "$recoverability_json" 2>"$ARTIFACT_DIR/recoverability-restore-pitr-target.stderr")" || {
        fail "recoverability.restore_pitr_available" "failed to derive recovery target; see recoverability-restore-pitr-target.stderr"
      }
      restore_pitr_available_json="$ARTIFACT_DIR/recoverability-restore-pitr-available.json"
      restore_pitr_available_err="$ARTIFACT_DIR/recoverability-restore-pitr-available.stderr"
      restore_pitr_available_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/database/backups/restore-pitr" "{\"recovery_time_target_unix\":\"$restore_target_unix\"}" "$restore_pitr_available_json" "$restore_pitr_available_err")"
      if [[ "$restore_pitr_available_status" != "201" ]]; then
        fail "recoverability.restore_pitr_available" "expected HTTP 201, got HTTP $restore_pitr_available_status"
      fi
      if ! node -e '
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const target = Number(process.argv[2]);
if (payload.restore_state !== "completed") throw new Error(`restore_state=${payload.restore_state}`);
if (!payload.restore_path) throw new Error("restore_path missing");
if (Number(payload.recovery_time_target_unix) !== target) throw new Error(`recovery_time_target_unix=${payload.recovery_time_target_unix}`);
' "$restore_pitr_available_json" "$restore_target_unix"; then
        fail "recoverability.restore_pitr_available" "PITR restore response was malformed"
      fi
      pass "recoverability.restore_pitr_available" "HTTP 201 target=$restore_target_unix"
    fi
  fi
else
  restore_pitr_json="$ARTIFACT_DIR/recoverability-restore-pitr.json"
  restore_pitr_err="$ARTIFACT_DIR/recoverability-restore-pitr.stderr"
  restore_pitr_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/database/backups/restore-pitr" '{"recovery_time_target_unix":"1735689600"}' "$restore_pitr_json" "$restore_pitr_err")"
  if [[ "$restore_pitr_status" != "409" ]]; then
    fail "recoverability.restore_pitr_unavailable" "expected HTTP 409, got HTTP $restore_pitr_status"
  fi
  if ! node -e '
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (!payload.error) throw new Error("missing error");
if (!payload.recoverability) throw new Error("missing recoverability");
if (payload.recoverability.restore_to_time_available !== false) throw new Error("restore_to_time_available was not false");
if (payload.recoverability.project_ref !== process.argv[2]) throw new Error(`project_ref=${payload.recoverability.project_ref}`);
if (!payload.recoverability.restore_to_time_unavailable) throw new Error("missing restore_to_time_unavailable");
' "$restore_pitr_json" "$SUPADUPA_TEST_REF"; then
    fail "recoverability.restore_pitr_unavailable" "409 response did not include recoverability"
  fi
  pass "recoverability.restore_pitr_unavailable" "HTTP 409 with recoverability"
fi

if ! compat_bool "${SUPADUPA_COMPAT_PHYSICAL_BACKUP_VALIDATE:-false}"; then
  skip "recoverability.physical_backup" "set SUPADUPA_COMPAT_PHYSICAL_BACKUP_VALIDATE=true to trigger a physical backup"
  exit 0
fi

if ! compat_bool "${SUPADUPA_COMPAT_CREATE_PROJECT:-false}" &&
  ! compat_bool "${SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE:-false}"; then
  skip "recoverability.physical_backup.guard" "set SUPADUPA_COMPAT_CREATE_PROJECT=true or SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE=true"
  exit 0
fi

if ! compat_bool "${SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE:-false}"; then
  created_ref="$(cat "$ARTIFACT_DIR/created-project" 2>/dev/null || true)"
  if [[ "$created_ref" != "$SUPADUPA_TEST_REF" ]]; then
    skip "recoverability.physical_backup.guard" "physical backup validation only runs against a project created by this compat run"
    exit 0
  fi
fi

policy_json="$ARTIFACT_DIR/recoverability-original-backup-policy.json"
policy_err="$ARTIFACT_DIR/recoverability-original-backup-policy.stderr"
policy_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/backups/policy" "" "$policy_json" "$policy_err")"
if [[ "$policy_status" != 2* ]]; then
  fail "recoverability.policy_get" "expected 2xx, got HTTP $policy_status"
fi

pitr_policy_json="$ARTIFACT_DIR/recoverability-original-pitr-policy.json"
pitr_policy_err="$ARTIFACT_DIR/recoverability-original-pitr-policy.stderr"
pitr_policy_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/pitr/policy" "" "$pitr_policy_json" "$pitr_policy_err")"
if [[ "$pitr_policy_status" != 2* && "$pitr_policy_status" != "403" ]]; then
  fail "recoverability.pitr_policy_get" "expected 2xx or feature-gated 403, got HTTP $pitr_policy_status"
fi

org_features_json="$ARTIFACT_DIR/recoverability-original-org-features.json"
if [[ -n "${SUPADUPA_TEST_ORG_ID:-}" ]]; then
  org_features_status="$(api_json GET "/v1/orgs/$SUPADUPA_TEST_ORG_ID/features" "" "$org_features_json" "$ARTIFACT_DIR/recoverability-original-org-features.stderr")"
  if [[ "$org_features_status" != 2* ]]; then
    rm -f "$org_features_json"
  fi
fi

restore_policy() {
  local restore_payload="$ARTIFACT_DIR/recoverability-restore-policy-payload.json"
  local restore_out="$ARTIFACT_DIR/recoverability-restore-policy.json"
  local restore_err="$ARTIFACT_DIR/recoverability-restore-policy.stderr"
  local restore_pitr_payload="$ARTIFACT_DIR/recoverability-restore-pitr-policy-payload.json"
  local restore_pitr_out="$ARTIFACT_DIR/recoverability-restore-pitr-policy.json"
  local restore_pitr_err="$ARTIFACT_DIR/recoverability-restore-pitr-policy.stderr"
  local restore_features_payload="$ARTIFACT_DIR/recoverability-restore-org-features-payload.json"
  local restore_features_out="$ARTIFACT_DIR/recoverability-restore-org-features.json"
  local restore_features_err="$ARTIFACT_DIR/recoverability-restore-org-features.stderr"
  if [[ -s "$policy_json" ]]; then
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
' "$policy_json" >"$restore_payload"
    api_json PUT "/v1/projects/$SUPADUPA_TEST_REF/backups/policy" "$(cat "$restore_payload")" "$restore_out" "$restore_err" >/dev/null || true
  fi
  if [[ "$pitr_policy_status" == 2* && -s "$pitr_policy_json" ]]; then
    node -e '
const fs = require("fs");
const policy = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const payload = {
  enabled: Boolean(policy.enabled),
  archive_bucket: policy.archive_bucket || "",
  retention_days: Number(policy.retention_days || 7),
};
process.stdout.write(JSON.stringify(payload));
' "$pitr_policy_json" >"$restore_pitr_payload"
    api_json PUT "/v1/projects/$SUPADUPA_TEST_REF/pitr/policy" "$(cat "$restore_pitr_payload")" "$restore_pitr_out" "$restore_pitr_err" >/dev/null || true
  fi
  if [[ -s "$org_features_json" && -n "${SUPADUPA_TEST_ORG_ID:-}" ]]; then
    node -e '
const fs = require("fs");
const features = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
process.stdout.write(JSON.stringify({ overrides: features.overrides || {} }));
' "$org_features_json" >"$restore_features_payload"
    api_json PUT "/v1/orgs/$SUPADUPA_TEST_ORG_ID/features" "$(cat "$restore_features_payload")" "$restore_features_out" "$restore_features_err" >/dev/null || true
  fi
}
trap 'restore_policy; cleanup_temporary_target' EXIT

if [[ -s "$org_features_json" ]] && compat_bool "${SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE:-false}"; then
  enable_pitr_features_payload="$ARTIFACT_DIR/recoverability-enable-pitr-feature-payload.json"
  node -e '
const fs = require("fs");
const features = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const overrides = { ...(features.overrides || {}), pitr: true };
process.stdout.write(JSON.stringify({ overrides }));
' "$org_features_json" >"$enable_pitr_features_payload"
  enable_pitr_features_status="$(api_json PUT "/v1/orgs/$SUPADUPA_TEST_ORG_ID/features" "$(cat "$enable_pitr_features_payload")" "$ARTIFACT_DIR/recoverability-enable-pitr-feature.json" "$ARTIFACT_DIR/recoverability-enable-pitr-feature.stderr")"
  if [[ "$enable_pitr_features_status" != 2* ]]; then
    fail "recoverability.pitr_feature" "expected 2xx, got HTTP $enable_pitr_features_status"
  fi
  pass "recoverability.pitr_feature" "temporarily enabled pitr for restore validation"
fi

if compat_bool "${SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE:-false}"; then
  require_tool psql
  ensure_profile
  pitr_semantics_public_db_url="$(profile_value_optional public_database_url)"
  if [[ -z "$pitr_semantics_public_db_url" ]]; then
    fail "recoverability.full_pitr.fixture_before" "profile did not include public_database_url"
  fi
  pitr_semantics_public_db_safe_url="$(url_without_password "$pitr_semantics_public_db_url")"
  pitr_semantics_db_password="$(reveal_secret_value db_password)"
  pitr_semantics_run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
  pitr_semantics_marker_suffix="$(printf '%s' "$pitr_semantics_run_id" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9-' | cut -c1-48)"
  pitr_semantics_before_marker="before-${pitr_semantics_marker_suffix:-pitr}"
  pitr_semantics_after_marker="after-${pitr_semantics_marker_suffix:-pitr}"
  if PGPASSWORD="$pitr_semantics_db_password" psql "$pitr_semantics_public_db_safe_url" \
    -v ON_ERROR_STOP=1 \
    -v before_marker="$pitr_semantics_before_marker" \
    -v after_marker="$pitr_semantics_after_marker" \
    -q >"$ARTIFACT_DIR/recoverability-full-pitr-fixture-before.out" 2>"$ARTIFACT_DIR/recoverability-full-pitr-fixture-before.stderr" <<'SQL'; then
create table if not exists public.compat_pitr_restore_semantics (
  marker text primary key,
  phase text not null,
  created_at timestamptz not null default now()
);
delete from public.compat_pitr_restore_semantics where marker in (:'before_marker', :'after_marker');
insert into public.compat_pitr_restore_semantics(marker, phase) values (:'before_marker', 'before-target');
SQL
    pitr_semantics_validate=true
    pass "recoverability.full_pitr.fixture_before" "inserted pre-target marker"
  else
    fail "recoverability.full_pitr.fixture_before" "failed to create pre-target fixture; see recoverability-full-pitr-fixture-before.stderr"
  fi
fi

physical_policy_payload="$ARTIFACT_DIR/recoverability-physical-policy-payload.json"
node -e '
const payload = {
  enabled: true,
  schedule: "daily",
  kind: "physical",
};
if (process.argv[1]) payload.storage_target_id = process.argv[1];
process.stdout.write(JSON.stringify(payload));
' "${target_to_test:-}" >"$physical_policy_payload"
physical_policy_json="$ARTIFACT_DIR/recoverability-physical-policy.json"
physical_policy_err="$ARTIFACT_DIR/recoverability-physical-policy.stderr"
physical_policy_status="$(api_json PUT "/v1/projects/$SUPADUPA_TEST_REF/backups/policy" "$(cat "$physical_policy_payload")" "$physical_policy_json" "$physical_policy_err")"
if [[ "$physical_policy_status" != 2* ]]; then
  fail "recoverability.physical_policy" "expected 2xx, got HTTP $physical_policy_status"
fi
pass "recoverability.physical_policy" "physical backup policy accepted"

physical_backup_json="$ARTIFACT_DIR/recoverability-physical-backup.json"
physical_backup_err="$ARTIFACT_DIR/recoverability-physical-backup.stderr"
physical_backup_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/backups" "" "$physical_backup_json" "$physical_backup_err")"
case "$physical_backup_status" in
  201)
    if ! node -e '
const fs = require("fs");
const backup = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const targetID = process.argv[2] || "";
if (backup.kind !== "physical") throw new Error(`kind=${backup.kind}`);
if (backup.status !== "completed") throw new Error(`status=${backup.status}`);
if (!backup.size_bytes || backup.size_bytes <= 0) throw new Error("size_bytes missing");
if (!backup.checksum_sha256 || backup.checksum_sha256.length !== 64) throw new Error("checksum missing");
if (!backup.verified_at) throw new Error("verified_at missing");
if (targetID) {
  if (!backup.remote_location) throw new Error("remote_location missing");
  if (backup.storage_target_id !== targetID) throw new Error(`storage_target_id=${backup.storage_target_id}`);
}
' "$physical_backup_json" "${target_to_test:-}"; then
      fail "recoverability.physical_backup" "physical backup response was malformed"
    fi
    pass "recoverability.physical_backup" "completed physical backup"
    ;;
  409)
    if grep -qi "physical backup command is not configured" "$physical_backup_json"; then
      pass "recoverability.physical_backup" "HTTP 409 without fake physical artifact"
    else
      fail "recoverability.physical_backup" "unexpected 409 response: $(cat "$physical_backup_json")"
    fi
    ;;
  *)
    fail "recoverability.physical_backup" "expected HTTP 201 or 409, got HTTP $physical_backup_status"
    ;;
esac

if ! compat_bool "${SUPADUPA_COMPAT_PITR_RESTORE_VALIDATE:-false}"; then
  exit 0
fi

if [[ "$physical_backup_status" != "201" ]]; then
  fail "recoverability.full_pitr.physical_backup" "physical backup must complete before PITR restore validation"
fi

pitr_policy_enable_json="$ARTIFACT_DIR/recoverability-enable-pitr-policy.json"
pitr_policy_enable_err="$ARTIFACT_DIR/recoverability-enable-pitr-policy.stderr"
pitr_policy_enable_status="$(api_json PUT "/v1/projects/$SUPADUPA_TEST_REF/pitr/policy" '{"enabled":true,"archive_bucket":"","retention_days":7}' "$pitr_policy_enable_json" "$pitr_policy_enable_err")"
if [[ "$pitr_policy_enable_status" != 2* ]]; then
  fail "recoverability.full_pitr.policy" "expected 2xx, got HTTP $pitr_policy_enable_status"
fi
pass "recoverability.full_pitr.policy" "PITR enabled for restore validation"

if [[ "$pitr_semantics_validate" == "true" ]]; then
  wait_public_db_ready "recoverability.full_pitr.fixture_db_ready_before_wal"
  pitr_semantics_target_unix="$(PGPASSWORD="$pitr_semantics_db_password" psql "$pitr_semantics_public_db_safe_url" \
    -v ON_ERROR_STOP=1 \
    -Atq 2>"$ARTIFACT_DIR/recoverability-full-pitr-target-time.stderr" <<'SQL'
select floor(extract(epoch from clock_timestamp()))::bigint;
SQL
)"
  if [[ -z "$pitr_semantics_target_unix" ]]; then
    fail "recoverability.full_pitr.fixture_after" "failed to capture restore target timestamp; see recoverability-full-pitr-target-time.stderr"
  fi
  sleep 2
  if PGPASSWORD="$pitr_semantics_db_password" psql "$pitr_semantics_public_db_safe_url" \
    -v ON_ERROR_STOP=1 \
    -v after_marker="$pitr_semantics_after_marker" \
    -q >"$ARTIFACT_DIR/recoverability-full-pitr-fixture-after.out" 2>"$ARTIFACT_DIR/recoverability-full-pitr-fixture-after.stderr" <<'SQL'; then
insert into public.compat_pitr_restore_semantics(marker, phase) values (:'after_marker', 'after-target');
SQL
    pass "recoverability.full_pitr.fixture_after" "inserted post-target marker"
  else
    fail "recoverability.full_pitr.fixture_after" "failed to insert post-target fixture; see recoverability-full-pitr-fixture-after.stderr"
  fi
fi

wal_json="$ARTIFACT_DIR/recoverability-wal-archive.json"
wal_err="$ARTIFACT_DIR/recoverability-wal-archive.stderr"
wal_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/pitr/wal" "" "$wal_json" "$wal_err")"
if [[ "$wal_status" != "201" ]]; then
  fail "recoverability.full_pitr.wal_archive" "expected HTTP 201, got HTTP $wal_status"
fi
if ! node -e '
const fs = require("fs");
const archive = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const targetID = process.argv[2] || "";
if (archive.status !== "archived") throw new Error(`status=${archive.status}`);
if (!archive.remote_location) throw new Error("remote_location missing");
if (!archive.storage_target_id) throw new Error("storage_target_id missing");
if (targetID && archive.storage_target_id !== targetID) throw new Error(`storage_target_id=${archive.storage_target_id}`);
if (!/^[0-9A-F]{24}$/.test(archive.segment || "")) throw new Error(`segment=${archive.segment}`);
if (archive.segment_source !== "postgres") throw new Error(`segment_source=${archive.segment_source}`);
if (!archive.size_bytes || archive.size_bytes <= 0) throw new Error("size_bytes missing");
if (!archive.checksum_sha256 || archive.checksum_sha256.length !== 64) throw new Error("checksum missing");
if (!archive.verified_at) throw new Error("verified_at missing");
' "$wal_json" "${target_to_test:-}"; then
  fail "recoverability.full_pitr.wal_archive" "WAL archive response was malformed"
fi
pass "recoverability.full_pitr.wal_archive" "WAL artifact archived"

ready_json="$ARTIFACT_DIR/recoverability-ready.json"
ready_err="$ARTIFACT_DIR/recoverability-ready.stderr"
ready_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/recoverability" "" "$ready_json" "$ready_err")"
if [[ "$ready_status" != 2* ]]; then
  fail "recoverability.full_pitr.ready" "expected 2xx, got HTTP $ready_status"
fi
if ! node -e '
const fs = require("fs");
const status = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (status.restore_to_time_available !== true) throw new Error(`restore_to_time_available=${status.restore_to_time_available}`);
for (const key of ["off_host_backup_configured", "off_host_backup_verified", "pitr_enabled", "wal_archive_off_host_verified", "physical_backup_available", "restore_to_time_configured"]) {
  if (status[key] !== true) throw new Error(`${key}=${status[key]}`);
}
if (!status.recovery_window_end) throw new Error("recovery_window_end missing");
' "$ready_json"; then
  fail "recoverability.full_pitr.ready" "restore-to-time readiness gates were not satisfied"
fi
pass "recoverability.full_pitr.ready" "restore-to-time gates satisfied"

if [[ -n "$pitr_semantics_target_unix" ]]; then
  restore_target_unix="$pitr_semantics_target_unix"
else
  restore_target_unix="$(node -e '
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const value = payload.recovery_window_end || payload.latest_wal_archive?.created_at;
if (!value) throw new Error("no recovery target available");
const unix = Math.floor(Date.parse(value) / 1000);
if (!Number.isFinite(unix) || unix <= 0) throw new Error(`invalid recovery target ${value}`);
process.stdout.write(String(unix));
' "$ready_json" 2>"$ARTIFACT_DIR/recoverability-full-pitr-target.stderr")" || {
    fail "recoverability.full_pitr.restore" "failed to derive recovery target; see recoverability-full-pitr-target.stderr"
  }
fi
restore_full_json="$ARTIFACT_DIR/recoverability-full-pitr-restore.json"
restore_full_err="$ARTIFACT_DIR/recoverability-full-pitr-restore.stderr"
restore_full_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/database/backups/restore-pitr" "{\"recovery_time_target_unix\":\"$restore_target_unix\"}" "$restore_full_json" "$restore_full_err")"
if [[ "$restore_full_status" != "201" ]]; then
  fail "recoverability.full_pitr.restore" "expected HTTP 201, got HTTP $restore_full_status"
fi
if ! node -e '
const fs = require("fs");
const payload = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const target = Number(process.argv[2]);
if (payload.restore_state !== "completed") throw new Error(`restore_state=${payload.restore_state}`);
if (!payload.restore_path) throw new Error("restore_path missing");
if (Number(payload.recovery_time_target_unix) !== target) throw new Error(`recovery_time_target_unix=${payload.recovery_time_target_unix}`);
' "$restore_full_json" "$restore_target_unix"; then
  fail "recoverability.full_pitr.restore" "PITR restore response was malformed"
fi
pass "recoverability.full_pitr.restore" "HTTP 201 target=$restore_target_unix"

if [[ "$pitr_semantics_validate" == "true" ]]; then
  wait_public_db_ready "recoverability.full_pitr.fixture_db_ready_after_restore"
  if ! pitr_semantics_counts="$(PGPASSWORD="$pitr_semantics_db_password" psql "$pitr_semantics_public_db_safe_url" \
    -v ON_ERROR_STOP=1 \
    -v before_marker="$pitr_semantics_before_marker" \
    -v after_marker="$pitr_semantics_after_marker" \
    -Atq 2>"$ARTIFACT_DIR/recoverability-full-pitr-fixture-assert.stderr" <<'SQL'
select
  count(*) filter (where marker = :'before_marker') || E'\t' ||
  count(*) filter (where marker = :'after_marker')
from public.compat_pitr_restore_semantics
where marker in (:'before_marker', :'after_marker');
SQL
  )"; then
    fail "recoverability.full_pitr.fixture_restore_semantics" "failed to query restored fixture; see recoverability-full-pitr-fixture-assert.stderr"
  fi
  before_count="${pitr_semantics_counts%%$'\t'*}"
  after_count="${pitr_semantics_counts#*$'\t'}"
  printf '%s\n' "$pitr_semantics_counts" >"$ARTIFACT_DIR/recoverability-full-pitr-fixture-assert.out"
  if [[ "$before_count" != "1" || "$after_count" != "0" ]]; then
    fail "recoverability.full_pitr.fixture_restore_semantics" "expected before marker present and after marker absent, got before=$before_count after=$after_count"
  fi
  if PGPASSWORD="$pitr_semantics_db_password" psql "$pitr_semantics_public_db_safe_url" \
    -v ON_ERROR_STOP=1 \
    -v before_marker="$pitr_semantics_before_marker" \
    -v after_marker="$pitr_semantics_after_marker" \
    -q >"$ARTIFACT_DIR/recoverability-full-pitr-fixture-cleanup.out" 2>"$ARTIFACT_DIR/recoverability-full-pitr-fixture-cleanup.stderr" <<'SQL'; then
delete from public.compat_pitr_restore_semantics where marker in (:'before_marker', :'after_marker');
SQL
    pass "recoverability.full_pitr.fixture_restore_semantics" "pre-target marker survived and post-target marker was rolled back"
  else
    fail "recoverability.full_pitr.fixture_restore_semantics" "restore semantics passed but fixture cleanup failed; see recoverability-full-pitr-fixture-cleanup.stderr"
  fi
fi
