#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

phases=(
  "00-preflight.sh"
  "01-create-project.sh"
  "01-auth-project.sh"
  "30-runtime-config.sh"
  "02-cli-profile.sh"
  "15-tls.sh"
  "02-rest-auth.sh"
  "03-postgres.sh"
  "04-db-fixture.sh"
  "27-database-desired-state.sh"
  "28-provider-configs.sh"
  "09-supabase-cli-classification.sh"
  "09-supabase-cli-db.sh"
  "09-supabase-cli-typegen.sh"
  "09-supabase-cli-matrix.sh"
  "04-gen-types.sh"
  "05-function-fixture.sh"
  "05-http-surfaces.sh"
  "22-auth-deep.sh"
  "18-storage-s3.sh"
  "23-storage-deep.sh"
  "06-realtime.sh"
  "24-realtime-deep.sh"
  "25-functions-deep.sh"
  "26-replicas-deep.sh"
  "29-branches-deep.sh"
  "08-sdk-js.sh"
  "11-metrics.sh"
  "12-backup-restore.sh"
  "19-durable-backup-target.sh"
  "16-recoverability-pitr.sh"
  "20-rustfs-backup-target.sh"
  "21-custom-domains.sh"
  "07-isolation.sh"
  "14-public-exposure.sh"
  "13-security-boundaries.sh"
  "17-studio-auth.sh"
  "19-stack-releases.sh"
  "10-upgrade-matrix.sh"
)

cleanup_status=0
cleanup_on_exit() {
  local status="$?"

  if [[ -z "${_SUPADUPA_COMPAT_IN_CLEANUP:-}" ]] &&
    compat_bool "${SUPADUPA_COMPAT_CREATE_PROJECT:-false}" &&
    ! compat_bool "${SUPADUPA_COMPAT_KEEP_PROJECT:-false}" &&
    [[ -x "$SCRIPT_DIR/99-cleanup.sh" ]]; then
    _SUPADUPA_COMPAT_IN_CLEANUP=true "$SCRIPT_DIR/99-cleanup.sh" || cleanup_status="$?"
  fi

  if [[ "$status" -eq 0 && "$cleanup_status" -ne 0 ]]; then
    exit "$cleanup_status"
  fi
  exit "$status"
}
trap cleanup_on_exit EXIT

if [[ "$#" -gt 0 ]]; then
  phases=("$@")
fi

for phase in "${phases[@]}"; do
  case "$phase" in
    */*)
      phase_path="$phase"
      ;;
    *)
      phase_path="$SCRIPT_DIR/$phase"
      ;;
  esac

  if [[ ! -x "$phase_path" ]]; then
    fail "runner.$phase" "phase script is not executable: $phase_path"
  fi

  "$phase_path"
done

pass "runner.complete" "artifacts: $ARTIFACT_DIR"
