#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

if ! compat_bool "${SUPADUPA_COMPAT_UPGRADE_MATRIX:-}"; then
  if [[ -n "${SUPADUPA_TEST_REF:-}" ]]; then
    compat_init
    skip "upgrade_matrix.enabled" "set SUPADUPA_COMPAT_UPGRADE_MATRIX=true to run"
  else
    echo "SKIP upgrade_matrix.enabled - set SUPADUPA_COMPAT_UPGRADE_MATRIX=true to run"
  fi
  exit 0
fi

compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool curl
require_tool node

api_base="${SUPADUPA_API_URL%/}"
upgrade_timeout_seconds="${SUPADUPA_UPGRADE_REQUEST_TIMEOUT_SECONDS:-600}"
health_attempts="${SUPADUPA_UPGRADE_HEALTH_ATTEMPTS:-30}"
health_interval_seconds="${SUPADUPA_UPGRADE_HEALTH_INTERVAL_SECONDS:-2}"
health_path="${SUPADUPA_UPGRADE_HEALTH_PATH:-/auth/v1/health}"
verify_phases="${SUPADUPA_UPGRADE_VERIFY_PHASES:-02-rest-auth.sh 03-postgres.sh 04-db-fixture.sh 09-supabase-cli-db.sh 04-gen-types.sh 05-http-surfaces.sh 22-auth-deep.sh 23-storage-deep.sh 06-realtime.sh 24-realtime-deep.sh 25-functions-deep.sh 08-sdk-js.sh}"
failure_targets_raw="${SUPADUPA_UPGRADE_FAILURE_TARGETS:-${SUPADUPA_COMPAT_UPGRADE_FAILURE_TARGETS:-}}"
REALTIME_UPGRADE_PROBE_PID=""
REALTIME_UPGRADE_READY_FILE=""
REALTIME_UPGRADE_CHECK_FILE=""

cleanup_realtime_upgrade_probe() {
  if [[ -n "${REALTIME_UPGRADE_PROBE_PID:-}" ]] && kill -0 "$REALTIME_UPGRADE_PROBE_PID" >/dev/null 2>&1; then
    kill "$REALTIME_UPGRADE_PROBE_PID" >/dev/null 2>&1 || true
    wait "$REALTIME_UPGRADE_PROBE_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup_realtime_upgrade_probe EXIT

require_positive_int() {
  local name="$1"
  local value="$2"
  if [[ ! "$value" =~ ^[1-9][0-9]*$ ]]; then
    fail "upgrade_matrix.env.$name" "$name must be a positive integer"
  fi
}

require_positive_int SUPADUPA_UPGRADE_REQUEST_TIMEOUT_SECONDS "$upgrade_timeout_seconds"
require_positive_int SUPADUPA_UPGRADE_HEALTH_ATTEMPTS "$health_attempts"
require_positive_int SUPADUPA_UPGRADE_HEALTH_INTERVAL_SECONDS "$health_interval_seconds"

if [[ "$health_path" != /* ]]; then
  fail "upgrade_matrix.env.SUPADUPA_UPGRADE_HEALTH_PATH" "health path must start with /"
fi

sanitize_label() {
  local value="$1"
  value="${value//[^a-zA-Z0-9_.-]/_}"
  printf '%s' "$value"
}

url_path_escape() {
  node -e 'process.stdout.write(encodeURIComponent(process.argv[1]))' "$1"
}

upgrade_payload() {
  node -e '
const version = process.argv[1];
const backup_id = process.argv[2];
process.stdout.write(JSON.stringify({ version, backup_id }));
' "$1" "$2"
}

project_stack_version() {
  local file="$1"
  local version

  version="$(json_get_file_optional "$file" spec.stack_version)"
  if [[ -z "$version" ]]; then
    version="$(json_get_file_optional "$file" stack_version)"
  fi
  if [[ -z "$version" ]]; then
    version="$(json_get_file_optional "$file" project.spec.stack_version)"
  fi
  if [[ -z "$version" ]]; then
    version="$(json_get_file_optional "$file" project.stack_version)"
  fi
  printf '%s' "$version"
}

validate_stable_target() {
  local target="$1"
  if [[ ! "$target" =~ ^[0-9]+([.][0-9]+)+$ ]]; then
    fail "upgrade_matrix.target.$target" "target must be an explicit stable version such as 15.8.1.085"
  fi
}

parse_targets() {
  UPGRADE_TARGETS=()
  local raw_targets
  local raw_target
  local target

  if [[ -z "${SUPADUPA_UPGRADE_TARGETS:-}" ]]; then
    target="$(default_upgrade_target)"
    validate_stable_target "$target"
    UPGRADE_TARGETS+=("$target")
    pass "upgrade_matrix.targets" "default target: $target"
    return 0
  fi

  IFS=',' read -r -a raw_targets <<<"$SUPADUPA_UPGRADE_TARGETS"
  for raw_target in "${raw_targets[@]}"; do
    target="$(printf '%s' "$raw_target" | tr -d '[:space:]')"
    if [[ -z "$target" ]]; then
      fail "upgrade_matrix.targets" "SUPADUPA_UPGRADE_TARGETS contains an empty target"
    fi
    validate_stable_target "$target"
    UPGRADE_TARGETS+=("$target")
  done

  if [[ "${#UPGRADE_TARGETS[@]}" -eq 0 ]]; then
    fail "upgrade_matrix.targets" "SUPADUPA_UPGRADE_TARGETS must include at least one stable version"
  fi

  pass "upgrade_matrix.targets" "targets: ${UPGRADE_TARGETS[*]}"
}

default_upgrade_target() {
  local token
  local out="$ARTIFACT_DIR/upgrade-stack-releases.json"
  local err="$ARTIFACT_DIR/upgrade-stack-releases.stderr"
  local status
  local target

  token="$(read_secret_file "$ARTIFACT_DIR/token")"
  status="$(curl -sS \
    --connect-timeout 10 \
    --max-time 30 \
    -o "$out" \
    -w '%{http_code}' \
    -H "Authorization: Bearer $token" \
    "$api_base/v1/stack-releases" 2>"$err")"
  if [[ "$status" != 2* ]]; then
    fail "upgrade_matrix.targets" "failed to fetch stack releases for default target; HTTP $status"
  fi

  target="$(node -e '
const fs = require("fs");
const releases = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (!Array.isArray(releases) || releases.length === 0) throw new Error("no stack releases exposed");
const version = releases[0] && releases[0].version;
if (!version) throw new Error("first stack release has no version");
process.stdout.write(version);
' "$out" 2>"$ARTIFACT_DIR/upgrade-stack-releases-parse.stderr")" || {
    fail "upgrade_matrix.targets" "failed to parse default target; see upgrade-stack-releases-parse.stderr"
  }
  printf '%s' "$target"
}

parse_failure_targets() {
  UPGRADE_FAILURE_TARGETS=()
  local raw_targets
  local raw_target
  local target

  if [[ -z "${failure_targets_raw//[[:space:]]/}" ]]; then
    return 0
  fi

  IFS=',' read -r -a raw_targets <<<"$failure_targets_raw"
  for raw_target in "${raw_targets[@]}"; do
    target="$(printf '%s' "$raw_target" | tr -d '[:space:]')"
    if [[ -z "$target" ]]; then
      fail "upgrade_matrix.failure.targets" "SUPADUPA_UPGRADE_FAILURE_TARGETS contains an empty target"
    fi
    validate_stable_target "$target"
    UPGRADE_FAILURE_TARGETS+=("$target")
  done

  pass "upgrade_matrix.failure.targets" "targets: ${UPGRADE_FAILURE_TARGETS[*]}"
}

FETCHED_PROJECT_FILE=""
FETCHED_PROJECT_VERSION=""
fetch_project() {
  local label="$1"
  local out="$ARTIFACT_DIR/upgrade-$label-project.json"
  local err="$ARTIFACT_DIR/upgrade-$label-project.stderr"
  local version

  if ! supadupa_cli_authed projects get --ref "$SUPADUPA_TEST_REF" >"$out" 2>"$err"; then
    fail "upgrade_matrix.project.$label" "failed to fetch project; see $(basename "$err")"
  fi

  version="$(project_stack_version "$out")"
  if [[ -z "$version" ]]; then
    fail "upgrade_matrix.project.$label" "project response did not include stack_version"
  fi

  FETCHED_PROJECT_FILE="$out"
  FETCHED_PROJECT_VERSION="$version"
  pass "upgrade_matrix.project.$label" "stack_version=$version"
}

PROFILE_API_URL=""
fetch_cli_profile() {
  local label="$1"
  local out="$ARTIFACT_DIR/upgrade-$label-profile.json"
  local err="$ARTIFACT_DIR/upgrade-$label-profile.stderr"
  local api_url

  if ! supadupa_cli_authed projects cli-profile \
    --ref "$SUPADUPA_TEST_REF" \
    --format json >"$out" 2>"$err"; then
    fail "upgrade_matrix.profile.$label" "failed to fetch CLI profile; see $(basename "$err")"
  fi

  api_url="$(json_get_file_optional "$out" api_url)"
  if [[ -z "$api_url" ]]; then
    fail "upgrade_matrix.profile.$label" "profile did not include api_url"
  fi
  if [[ "$api_url" != https://* ]]; then
    fail "upgrade_matrix.profile.$label" "api_url is not HTTPS"
  fi

  PROFILE_API_URL="$api_url"
  pass "upgrade_matrix.profile.$label" "$api_url"
}

verify_project_api_health() {
  local label="$1"
  local body="$ARTIFACT_DIR/upgrade-$label-health.body"
  local err="$ARTIFACT_DIR/upgrade-$label-health.stderr"
  local api_url
  local status="000"
  local rc=1
  local attempt

  fetch_cli_profile "$label"
  api_url="${PROFILE_API_URL%/}$health_path"

  for ((attempt = 1; attempt <= health_attempts; attempt++)); do
    set +e
    status="$(curl -sS \
      --connect-timeout 10 \
      --max-time 15 \
      -o "$body" \
      -w '%{http_code}' \
      "$api_url" 2>"$err")"
    rc="$?"
    set -e

    case "$status" in
      2??)
        if [[ "$rc" -eq 0 ]]; then
          pass "upgrade_matrix.health.$label" "HTTP $status after attempt $attempt"
          return 0
        fi
        ;;
    esac

    if [[ "$attempt" -lt "$health_attempts" ]]; then
      sleep "$health_interval_seconds"
    fi
  done

  fail "upgrade_matrix.health.$label" "expected 2xx from $health_path; last HTTP $status"
}

run_upgrade_verification_phases() {
  local label="$1"
  local phase

  if [[ -z "${verify_phases//[[:space:]]/}" ]]; then
    skip "upgrade_matrix.verify.$label" "SUPADUPA_UPGRADE_VERIFY_PHASES is empty"
    return 0
  fi

  pass "upgrade_matrix.verify.$label" "running: $verify_phases"
  # shellcheck disable=SC2086
  for phase in $verify_phases; do
    case "$phase" in
      10-upgrade-matrix.sh|*/10-upgrade-matrix.sh)
        fail "upgrade_matrix.verify.$label" "verification phases must not include 10-upgrade-matrix.sh"
        ;;
    esac
    if [[ "$phase" == */* ]]; then
      "$phase"
    else
      "$SCRIPT_DIR/$phase"
    fi
  done
}

ensure_realtime_upgrade_probe_deps() {
  require_tool npm
  if [[ ! -d "$SCRIPT_DIR/node_modules/@supabase/supabase-js" ]]; then
    if npm --prefix "$SCRIPT_DIR" install --omit=dev --no-audit --no-fund --package-lock=false \
      >"$ARTIFACT_DIR/upgrade-realtime-sdk-install.out" 2>"$ARTIFACT_DIR/upgrade-realtime-sdk-install.stderr"; then
      pass "upgrade_matrix.realtime_continuity.sdk_install" "@supabase/supabase-js installed"
    else
      fail "upgrade_matrix.realtime_continuity.sdk_install" "npm install failed; see upgrade-realtime-sdk-install.stderr"
    fi
  fi
}

start_realtime_upgrade_probe() {
  local label="$1"
  local api_url
  local anon_key
  local ready_timeout="${SUPADUPA_UPGRADE_REALTIME_READY_TIMEOUT_SECONDS:-60}"
  local waited=0
  local safe_label

  if ! compat_bool "${SUPADUPA_UPGRADE_REALTIME_CONTINUITY_VALIDATE:-false}"; then
    return 0
  fi
  require_positive_int SUPADUPA_UPGRADE_REALTIME_READY_TIMEOUT_SECONDS "$ready_timeout"
  ensure_realtime_upgrade_probe_deps
  ensure_profile
  api_url="$(profile_value api_url)"
  anon_key="$(reveal_secret_value anon_key)"
  safe_label="$(sanitize_label "$label")"
  REALTIME_UPGRADE_READY_FILE="$ARTIFACT_DIR/upgrade-$safe_label-realtime-ready.json"
  REALTIME_UPGRADE_CHECK_FILE="$ARTIFACT_DIR/upgrade-$safe_label-realtime-check"
  rm -f "$REALTIME_UPGRADE_READY_FILE" "$REALTIME_UPGRADE_CHECK_FILE"

  SUPABASE_URL="$api_url" \
    SUPABASE_ANON_KEY="$anon_key" \
    SUPADUPA_COMPAT_RUN_ID="${SUPADUPA_COMPAT_RUN_ID:-upgrade-$safe_label-$$}" \
    SUPADUPA_REALTIME_READY_FILE="$REALTIME_UPGRADE_READY_FILE" \
    SUPADUPA_REALTIME_CHECK_FILE="$REALTIME_UPGRADE_CHECK_FILE" \
    node "$SCRIPT_DIR/realtime-upgrade-continuity-probe.mjs" \
    >"$ARTIFACT_DIR/upgrade-$safe_label-realtime-continuity.out" \
    2>"$ARTIFACT_DIR/upgrade-$safe_label-realtime-continuity.stderr" &
  REALTIME_UPGRADE_PROBE_PID="$!"

  while (( waited < ready_timeout )); do
    if [[ -s "$REALTIME_UPGRADE_READY_FILE" ]]; then
      pass "upgrade_matrix.realtime_continuity.ready.$safe_label" "probe subscribed before upgrade"
      return 0
    fi
    if ! kill -0 "$REALTIME_UPGRADE_PROBE_PID" >/dev/null 2>&1; then
      fail "upgrade_matrix.realtime_continuity.ready.$safe_label" "probe exited before ready; see upgrade-$safe_label-realtime-continuity.stderr"
    fi
    sleep 1
    waited=$((waited + 1))
  done

  fail "upgrade_matrix.realtime_continuity.ready.$safe_label" "probe did not subscribe within ${ready_timeout}s"
}

finish_realtime_upgrade_probe() {
  local label="$1"
  local finish_timeout="${SUPADUPA_UPGRADE_REALTIME_FINISH_TIMEOUT_SECONDS:-180}"
  local waited=0
  local safe_label
  local status

  if ! compat_bool "${SUPADUPA_UPGRADE_REALTIME_CONTINUITY_VALIDATE:-false}"; then
    return 0
  fi
  require_positive_int SUPADUPA_UPGRADE_REALTIME_FINISH_TIMEOUT_SECONDS "$finish_timeout"
  safe_label="$(sanitize_label "$label")"
  if [[ -z "${REALTIME_UPGRADE_PROBE_PID:-}" ]]; then
    fail "upgrade_matrix.realtime_continuity.$safe_label" "probe was not started"
  fi
  touch "$REALTIME_UPGRADE_CHECK_FILE"

  while (( waited < finish_timeout )); do
    if ! kill -0 "$REALTIME_UPGRADE_PROBE_PID" >/dev/null 2>&1; then
      set +e
      wait "$REALTIME_UPGRADE_PROBE_PID"
      status="$?"
      set -e
      REALTIME_UPGRADE_PROBE_PID=""
      if [[ "$status" -eq 0 ]]; then
        pass "upgrade_matrix.realtime_continuity.$safe_label" "active client resubscribed and received post-upgrade broadcast"
        return 0
      fi
      fail "upgrade_matrix.realtime_continuity.$safe_label" "probe failed; see upgrade-$safe_label-realtime-continuity.stderr"
    fi
    sleep 1
    waited=$((waited + 1))
  done

  fail "upgrade_matrix.realtime_continuity.$safe_label" "probe did not complete within ${finish_timeout}s"
}

BACKUP_ID=""
assert_backup_metadata() {
  local test_name="$1"
  local file="$2"
  local expected_kind="${3:-logical}"
  local expected_target_id="${4:-}"

  if ! node -e '
const fs = require("fs");
const parsed = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const backup = parsed.backup || parsed;
const expectedKind = process.argv[2];
const expectedTargetID = process.argv[3];
if (!backup.id) throw new Error("id missing");
if (backup.kind !== expectedKind) throw new Error(`kind=${backup.kind}; expected ${expectedKind}`);
if (backup.status !== "completed") throw new Error(`status=${backup.status}`);
if (!backup.started_at) throw new Error("started_at missing");
if (!backup.finished_at) throw new Error("finished_at missing");
if (Date.parse(backup.finished_at) < Date.parse(backup.started_at)) throw new Error("finished_at before started_at");
if (!backup.verified_at) throw new Error("verified_at missing");
if (!backup.size_bytes || Number(backup.size_bytes) <= 0) throw new Error("size_bytes missing");
if (!backup.checksum_sha256 || String(backup.checksum_sha256).length !== 64) throw new Error("checksum_sha256 missing");
if (!backup.location && !backup.remote_location) throw new Error("location and remote_location both missing");
if (expectedTargetID) {
  if (backup.storage_target_id !== expectedTargetID) throw new Error(`storage_target_id=${backup.storage_target_id}; expected ${expectedTargetID}`);
  if (!backup.remote_location) throw new Error("remote_location missing for expected target");
}
' "$file" "$expected_kind" "$expected_target_id"; then
    fail "$test_name" "backup metadata was incomplete or malformed"
  fi
}

trigger_pre_upgrade_backup() {
  local label="$1"
  local out="$ARTIFACT_DIR/upgrade-$label-backup.json"
  local err="$ARTIFACT_DIR/upgrade-$label-backup.stderr"
  local backup_id
  local backup_target_id
  local backup_remote_location
  local expected_target_id

  if ! supadupa_cli_authed backups trigger --ref "$SUPADUPA_TEST_REF" >"$out" 2>"$err"; then
    fail "upgrade_matrix.backup.$label" "backup trigger failed; see $(basename "$err")"
  fi

  backup_id="$(json_get_file_optional "$out" id)"
  if [[ -z "$backup_id" ]]; then
    fail "upgrade_matrix.backup.$label" "backup response did not include id"
  fi
  if [[ -s "$ARTIFACT_DIR/rustfs-target-kept-id" ]]; then
    expected_target_id="$(cat "$ARTIFACT_DIR/rustfs-target-kept-id")"
    assert_backup_metadata "upgrade_matrix.backup.$label" "$out" "logical" "$expected_target_id"
    backup_target_id="$(json_get_file_optional "$out" storage_target_id)"
    backup_remote_location="$(json_get_file_optional "$out" remote_location)"
    if [[ "$backup_target_id" != "$expected_target_id" ]]; then
      fail "upgrade_matrix.backup.$label" "backup storage_target_id=$backup_target_id; expected RustFS target $expected_target_id"
    fi
    if [[ -z "$backup_remote_location" ]]; then
      fail "upgrade_matrix.backup.$label" "backup did not include remote_location for RustFS target"
    fi
  else
    assert_backup_metadata "upgrade_matrix.backup.$label" "$out" "logical"
  fi

  BACKUP_ID="$backup_id"
  pass "upgrade_matrix.backup.$label" "backup_id=$backup_id"
}

verify_backup_list() {
  local label="$1"
  local backup_id="$2"
  local out="$ARTIFACT_DIR/upgrade-$label-backups.json"
  local err="$ARTIFACT_DIR/upgrade-$label-backups.stderr"

  if ! supadupa_cli_authed backups list --ref "$SUPADUPA_TEST_REF" >"$out" 2>"$err"; then
    fail "upgrade_matrix.backups.$label" "backup list failed; see $(basename "$err")"
  fi

  if ! node -e '
const fs = require("fs");
const backups = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const backupID = process.argv[2];
if (!Array.isArray(backups)) throw new Error("response is not an array");
const backup = backups.find((item) => item && item.id === backupID);
if (!backup) throw new Error(`backup ${backupID} missing`);
if (!backup.started_at) throw new Error("started_at missing from listed backup");
if (!backup.finished_at) throw new Error("finished_at missing from listed backup");
if (!backup.verified_at) throw new Error("verified_at missing from listed backup");
if (!backup.checksum_sha256 || backup.checksum_sha256.length !== 64) throw new Error("checksum missing from listed backup");
' "$out" "$backup_id"; then
    fail "upgrade_matrix.backups.$label" "backup $backup_id was missing or had incomplete listed metadata"
  fi

  pass "upgrade_matrix.backups.$label" "backup $backup_id listed"
}

upgrade_with_backup() {
  local target="$1"
  local label="$2"
  local backup_id="$3"
  local token
  local encoded_ref
  local payload
  local out="$ARTIFACT_DIR/upgrade-$label-response.json"
  local err="$ARTIFACT_DIR/upgrade-$label-response.stderr"
  local status="000"
  local rc=1
  local response_target
  local response_backup
  local response_project_version
  local response_previous
  local rollback_available

  token="$(read_secret_file "$ARTIFACT_DIR/token")"
  encoded_ref="$(url_path_escape "$SUPADUPA_TEST_REF")"
  payload="$(upgrade_payload "$target" "$backup_id")"

  set +e
  status="$(curl -sS \
    --connect-timeout 10 \
    --max-time "$upgrade_timeout_seconds" \
    -o "$out" \
    -w '%{http_code}' \
    -H "Authorization: Bearer $token" \
    -H "Content-Type: application/json" \
    -X POST "$api_base/v1/projects/$encoded_ref/upgrade" \
    --data "$payload" 2>"$err")"
  rc="$?"
  set -e

  if [[ "$rc" -ne 0 ]]; then
    fail "upgrade_matrix.upgrade.$label" "upgrade request failed; see $(basename "$err")"
  fi
  if [[ "$status" != "200" ]]; then
    fail "upgrade_matrix.upgrade.$label" "expected HTTP 200, got HTTP $status; see $(basename "$out")"
  fi

  response_target="$(json_get_file_optional "$out" target_version)"
  response_previous="$(json_get_file_optional "$out" previous_version)"
  response_backup="$(json_get_file_optional "$out" backup.id)"
  response_project_version="$(project_stack_version "$out")"
  rollback_available="$(json_get_file_optional "$out" rollback_available)"
  if [[ "$response_target" != "$target" ]]; then
    fail "upgrade_matrix.upgrade.$label" "response target_version=$response_target"
  fi
  if [[ -z "$response_previous" ]]; then
    fail "upgrade_matrix.upgrade.$label" "response previous_version was empty"
  fi
  if [[ "$response_backup" != "$backup_id" ]]; then
    fail "upgrade_matrix.upgrade.$label" "response backup.id=$response_backup; expected $backup_id"
  fi
  assert_backup_metadata "upgrade_matrix.upgrade.$label" "$out" "logical"
  if [[ "$rollback_available" != "true" ]]; then
    fail "upgrade_matrix.upgrade.$label" "expected rollback_available=true, got ${rollback_available:-empty}"
  fi
  if [[ "$response_project_version" != "$target" ]]; then
    fail "upgrade_matrix.upgrade.$label" "response project stack_version=$response_project_version"
  fi

  pass "upgrade_matrix.upgrade.$label" "target=$target backup_id=$backup_id"
}

upgrade_expected_failure_with_backup() {
  local target="$1"
  local label="$2"
  local backup_id="$3"
  local previous_version="$4"
  local token
  local encoded_ref
  local payload
  local out="$ARTIFACT_DIR/upgrade-failure-$label-response.json"
  local err="$ARTIFACT_DIR/upgrade-failure-$label-response.stderr"
  local status="000"
  local rc=1
  local response_error
  local response_target
  local response_previous
  local response_backup
  local rollback_attempted
  local rollback_available
  local restore_attempted
  local restore_error
  local restore_state

  token="$(read_secret_file "$ARTIFACT_DIR/token")"
  encoded_ref="$(url_path_escape "$SUPADUPA_TEST_REF")"
  payload="$(upgrade_payload "$target" "$backup_id")"

  set +e
  status="$(curl -sS \
    --connect-timeout 10 \
    --max-time "$upgrade_timeout_seconds" \
    -o "$out" \
    -w '%{http_code}' \
    -H "Authorization: Bearer $token" \
    -H "Content-Type: application/json" \
    -H "X-Supadupa-Compat-Inject-Upgrade-Failure: true" \
    -X POST "$api_base/v1/projects/$encoded_ref/upgrade" \
    --data "$payload" 2>"$err")"
  rc="$?"
  set -e

  if [[ "$rc" -ne 0 ]]; then
    fail "upgrade_matrix.failure.$label" "upgrade request failed; see $(basename "$err")"
  fi
  if [[ "$status" != "409" ]]; then
    fail "upgrade_matrix.failure.$label" "expected HTTP 409, got HTTP $status; see $(basename "$out")"
  fi

  response_error="$(json_get_file_optional "$out" error)"
  response_target="$(json_get_file_optional "$out" target_version)"
  response_previous="$(json_get_file_optional "$out" previous_version)"
  response_backup="$(json_get_file_optional "$out" backup.id)"
  rollback_available="$(json_get_file_optional "$out" rollback_available)"
  rollback_attempted="$(json_get_file_optional "$out" rollback_attempted)"
  restore_attempted="$(json_get_file_optional "$out" restore_attempted)"
  restore_state="$(json_get_file_optional "$out" restore_state)"
  restore_error="$(json_get_file_optional "$out" restore_error)"
  if [[ -z "$response_error" ]]; then
    fail "upgrade_matrix.failure.$label" "failure response did not include error"
  fi
  if [[ "$response_target" != "$target" ]]; then
    fail "upgrade_matrix.failure.$label" "response target_version=$response_target"
  fi
  if [[ "$response_previous" != "$previous_version" ]]; then
    fail "upgrade_matrix.failure.$label" "response previous_version=$response_previous; expected $previous_version"
  fi
  if [[ "$response_backup" != "$backup_id" ]]; then
    fail "upgrade_matrix.failure.$label" "response backup.id=$response_backup; expected $backup_id"
  fi
  assert_backup_metadata "upgrade_matrix.failure.$label" "$out" "logical"
  if [[ "$rollback_available" != "true" ]]; then
    fail "upgrade_matrix.failure.$label" "expected rollback_available=true, got ${rollback_available:-empty}"
  fi
  if [[ "$rollback_attempted" != "true" ]]; then
    fail "upgrade_matrix.failure.$label" "expected rollback_attempted=true, got ${rollback_attempted:-empty}"
  fi
  if compat_bool "${SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE:-false}"; then
    if [[ "$restore_attempted" != "true" ]]; then
      fail "upgrade_matrix.failure.$label" "expected restore_attempted=true with SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE=true, got ${restore_attempted:-empty}"
    fi
    if [[ -n "$restore_error" ]]; then
      fail "upgrade_matrix.failure.$label" "auto restore failed: $restore_error"
    fi
    if [[ "$restore_state" != "completed" ]]; then
      fail "upgrade_matrix.failure.$label" "expected auto restore_state=completed, got ${restore_state:-empty}"
    fi
  fi

  pass "upgrade_matrix.failure.$label" "rollback attempted with backup_id=$backup_id"
}

restore_failed_upgrade_backup_if_enabled() {
  local label="$1"
  local backup_id="$2"
  local out="$ARTIFACT_DIR/upgrade-failure-$label-restore.json"
  local err="$ARTIFACT_DIR/upgrade-failure-$label-restore.stderr"
  local restore_state

  if ! compat_bool "${SUPADUPA_UPGRADE_FAILURE_RESTORE_VALIDATE:-false}"; then
    skip "upgrade_matrix.failure_restore.$label" "set SUPADUPA_UPGRADE_FAILURE_RESTORE_VALIDATE=true to restore the pre-upgrade backup"
    return 0
  fi

  if ! compat_bool "${SUPADUPA_COMPAT_CREATE_PROJECT:-false}" &&
    ! compat_bool "${SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE:-false}"; then
    skip "upgrade_matrix.failure_restore.$label" "set SUPADUPA_COMPAT_CREATE_PROJECT=true or SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE=true"
    return 0
  fi

  if ! compat_bool "${SUPADUPA_COMPAT_ALLOW_DESTRUCTIVE_RESTORE:-false}"; then
    created_ref="$(cat "$ARTIFACT_DIR/created-project" 2>/dev/null || true)"
    if [[ "$created_ref" != "$SUPADUPA_TEST_REF" ]]; then
      skip "upgrade_matrix.failure_restore.$label" "restore validation only runs against a project created by this compat run"
      return 0
    fi
  fi

  if ! supadupa_cli_authed backups restore --ref "$SUPADUPA_TEST_REF" --backup-id "$backup_id" --confirmation "restore project $SUPADUPA_TEST_REF" >"$out" 2>"$err"; then
    fail "upgrade_matrix.failure_restore.$label" "restore command failed; see $(basename "$err")"
  fi
  restore_state="$(json_get_file_optional "$out" restore_state)"
  if [[ "$restore_state" != "completed" ]]; then
    fail "upgrade_matrix.failure_restore.$label" "expected completed restore, got ${restore_state:-empty}"
  fi
  pass "upgrade_matrix.failure_restore.$label" "backup_id=$backup_id"
}

ensure_token
if compat_bool "${SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP:-false}"; then
  compat_require_runtime_config_bool \
    "upgrade_matrix.server_guard.durable_upgrade_backup" \
    "upgrade.require_durable_backup" \
    "true"
fi
if compat_bool "${SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE:-false}"; then
  compat_require_runtime_config_bool \
    "upgrade_matrix.server_guard.failure_auto_restore" \
    "upgrade.failure_auto_restore" \
    "true"
fi
parse_targets
parse_failure_targets
verify_project_api_health "initial"
if ! compat_bool "${SUPADUPA_UPGRADE_SKIP_INITIAL_VERIFY_PHASES:-false}"; then
  run_upgrade_verification_phases "initial"
fi

for target in "${UPGRADE_TARGETS[@]}"; do
  label="$(sanitize_label "$target")"

  fetch_project "before-$label"
  if [[ "$FETCHED_PROJECT_VERSION" == "$target" ]]; then
    skip "upgrade_matrix.upgrade.$label" "target already installed"
    verify_project_api_health "already-$label"
    continue
  fi

  trigger_pre_upgrade_backup "$label"
  verify_backup_list "$label" "$BACKUP_ID"
  start_realtime_upgrade_probe "$label"
  upgrade_with_backup "$target" "$label" "$BACKUP_ID"
  fetch_project "after-$label"

  if [[ "$FETCHED_PROJECT_VERSION" != "$target" ]]; then
    fail "upgrade_matrix.project.after-$label" "expected stack_version=$target, got $FETCHED_PROJECT_VERSION"
  fi

  verify_project_api_health "after-$label"
  finish_realtime_upgrade_probe "$label"
  run_upgrade_verification_phases "after-$label"
done

for target in "${UPGRADE_FAILURE_TARGETS[@]}"; do
  label="$(sanitize_label "$target")"

  fetch_project "failure-before-$label"
  previous_version="$FETCHED_PROJECT_VERSION"
  if [[ "$previous_version" == "$target" ]]; then
    skip "upgrade_matrix.failure.$label" "target already installed"
    continue
  fi

  trigger_pre_upgrade_backup "failure-$label"
  verify_backup_list "failure-$label" "$BACKUP_ID"
  upgrade_expected_failure_with_backup "$target" "$label" "$BACKUP_ID" "$previous_version"
  fetch_project "failure-after-$label"
  if [[ "$FETCHED_PROJECT_VERSION" != "$previous_version" ]]; then
    fail "upgrade_matrix.failure_project.$label" "expected stack_version=$previous_version after failure, got $FETCHED_PROJECT_VERSION"
  fi
  verify_project_api_health "failure-after-$label"
  restore_failed_upgrade_backup_if_enabled "$label" "$BACKUP_ID"
done

pass "upgrade_matrix.complete" "validated ${#UPGRADE_TARGETS[@]} stable target(s)"
