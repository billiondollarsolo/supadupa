#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool curl
require_tool node

runtime_json="$ARTIFACT_DIR/runtime-config.json"
runtime_err="$ARTIFACT_DIR/runtime-config.stderr"
compat_fetch_runtime_config "$runtime_json" "$runtime_err"

if ! node -e '
const fs = require("fs");
const config = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
function bool(path) {
  let value = config;
  for (const key of path.split(".")) value = value?.[key];
  if (typeof value !== "boolean") throw new Error(`${path} is not boolean`);
  return value;
}
if (typeof config.provisioner !== "string" || config.provisioner.length === 0) {
  throw new Error("provisioner missing");
}
for (const path of [
  "apply.compose",
  "apply.kubernetes",
  "apply.storage_data_plane",
  "backup.compose_defaults",
  "backup.logical_configured",
  "backup.physical_configured",
  "backup.wal_archive_configured",
  "backup.logical_restore_configured",
  "backup.pitr_restore_configured",
  "backup.backup_dry_run",
  "backup.restore_dry_run",
  "backup.wal_archive_dry_run",
  "recovery.require_recovery_ready_targets",
  "upgrade.require_durable_backup",
  "upgrade.failure_auto_restore",
]) {
  bool(path);
}
const rendered = JSON.stringify(config);
for (const forbidden of ["SUPADUPA_", "_COMMAND", "SECRET", "ACCESS_KEY", "PASSWORD"]) {
  if (rendered.toUpperCase().includes(forbidden)) {
    throw new Error(`runtime config leaked ${forbidden}`);
  }
}
' "$runtime_json" >"$ARTIFACT_DIR/runtime-config-check.out" 2>"$ARTIFACT_DIR/runtime-config-check.stderr"; then
  fail "runtime_config.redacted_shape" "runtime config shape/redaction check failed; see runtime-config-check.stderr"
fi

runtime_summary="$(node -e '
const fs = require("fs");
const config = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
process.stdout.write([
  `provisioner=${config.provisioner}`,
  `compose_defaults=${config.backup.compose_defaults}`,
  `recovery_guard=${config.recovery.require_recovery_ready_targets}`,
  `upgrade_guard=${config.upgrade.require_durable_backup}`,
].join(" "));
' "$runtime_json")"
pass "runtime_config.redacted_shape" "$runtime_summary"
