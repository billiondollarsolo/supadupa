#!/usr/bin/env python3
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]


def read(relative: str) -> str:
    return (ROOT / relative).read_text(encoding="utf-8")


def fail_if_contains(failures: list[str], relative: str, marker: str, reason: str) -> None:
    if marker in read(relative):
        failures.append(f"{relative}: forbidden marker {marker!r}: {reason}")


def function_body(source: str, name: str) -> str:
    match = re.search(rf"func\s+{re.escape(name)}\b", source)
    if not match:
        return ""
    brace = source.find("{", match.end())
    if brace == -1:
        return ""
    depth = 0
    for index in range(brace, len(source)):
        if source[index] == "{":
            depth += 1
        elif source[index] == "}":
            depth -= 1
            if depth == 0:
                return source[brace : index + 1]
    return source[brace:]


def main() -> int:
    failures: list[str] = []

    restore_handlers = read("internal/api/project_backup_recovery_handlers.go")
    for handler in ["restoreBackupHandler", "restorePITRBackupHandler"]:
        body = function_body(restore_handlers, handler)
        if not body:
            failures.append(f"missing restore handler {handler}")
        elif "roleDeveloper" in body:
            failures.append(f"{handler} must not authorize destructive restores with roleDeveloper")
    for marker in [
        "Confirmation string",
        "restoreConfirmationMatches",
        "confirmation must be",
    ]:
        if marker not in restore_handlers:
            failures.append(f"restore handlers missing confirmation guard marker {marker!r}")

    cli_source = read("internal/cli/cli.go")
    if '"confirmation"' not in cli_source or "confirmation is required" not in cli_source:
        failures.append("CLI backup restore must expose and send explicit confirmation")
    mcp_source = read("internal/mcp/server.go")
    if '"confirmation"' not in mcp_source or '"required": []string{"ref", "backup_id", "confirmation"}' not in mcp_source:
        failures.append("MCP project restore tool must require explicit confirmation")

    authz_helpers = read("internal/api/authz_helpers.go")
    for pattern in [
        r"if\s+!ok\s+\{\s*return\s+true",
        r"if\s+!ok\s+\{\s*return\s+project,\s*true",
    ]:
        if re.search(pattern, authz_helpers):
            failures.append(f"route-local auth helper still appears to allow missing claims: {pattern}")
    for marker in [
        "requestAllowsAuthBypass",
        "requireCurrentPlatformAdminClaims",
        "stale bearer token",
    ]:
        if marker not in authz_helpers:
            failures.append(f"authz helpers missing current-state/fail-closed marker {marker!r}")

    auth_source = read("internal/control/auth.go")
    store_source = read("internal/control/store.go")
    if "TokenVersion" not in auth_source or "TokenVersion" not in store_source:
        failures.append("token version must remain part of issued tokens and stored users")

    persistent_store = read("internal/control/persistent_store.go")
    fail_if_contains(failures, "internal/control/persistent_store.go", "nullString(user.MFASecret)", "raw MFA seed persistence")
    fail_if_contains(failures, "internal/control/persistent_store.go", "nullString(user.MFAPendingSecret)", "raw pending MFA seed persistence")
    for marker in [
        "encryptedStringPrefix",
        "encryptOptionalString",
        "decryptOptionalString",
    ]:
        if marker not in persistent_store:
            failures.append(f"persistent store missing MFA encryption marker {marker!r}")

    setup = read("scripts/setup-compose.sh")
    compose = read("deploy/compose.yaml")
    if "--db-public-bind" not in setup:
        failures.append("setup-compose must keep an explicit public DB bind flag")
    if "${SUPADUPA_POSTGRES_ADDR:-127.0.0.1:5432}" not in compose:
        failures.append("compose default Postgres edge bind must stay loopback")
    if "${SUPADUPA_POOLER_ADDR:-127.0.0.1:6543}" not in compose:
        failures.append("compose default pooler edge bind must stay loopback")

    if failures:
        print("\n".join(failures), file=sys.stderr)
        return 1
    print("security regression checks passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
