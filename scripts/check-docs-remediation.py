#!/usr/bin/env python3
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]


CHECKS = {
    "install docs generated .env permissions": (
        "docs/install.md",
        [".env", "0600", "control-character"],
    ),
    "install docs public database gating": (
        "docs/install.md",
        ["--db-loopback", "SUPADUPA_POSTGRES_ADDR", "SUPADUPA_POOLER_ADDR", "SUPADUPA_DB_INGRESS_ALLOWED_CIDRS"],
    ),
    "install docs docker socket proxy": (
        "docs/install.md",
        ["docker-socket-proxy", "SUPADUPA_DOCKER_GID", "deploy/compose.apply.yaml"],
    ),
    "security docs session cookie": (
        "docs/security.md",
        ["HttpOnly", "supadupa_session", "SameSite=Lax", "localStorage"],
    ),
    "security docs sso role validation": (
        "docs/security.md",
        ["Platform SSO", "role binding", "SUPADUPA_ENABLE_PLATFORM_SSO_JSON_ADAPTER"],
    ),
    "security docs password hashing migration": (
        "docs/security.md",
        ["bcrypt-sha256", "sha256$", "rehashed"],
    ),
    "security docs csrf origin policy": (
        "docs/security.md",
        ["Cookie-authenticated", "allowed `Origin`", "bearer-token"],
    ),
    "operations docs migration checksum policy": (
        "docs/operations.md",
        ["schema_migrations", "SHA-256 checksum", "immutable"],
    ),
    "operations docs backup target persistence": (
        "docs/operations.md",
        ["BackupPolicy.StorageTargetID", "checkpoint restore", "SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS"],
    ),
    "operations docs vulnerability commands": (
        "docs/operations.md",
        ["govulncheck", "npm --prefix frontend audit", "security-audit-results"],
    ),
}


def main() -> int:
    failures = []
    for name, (relative_path, terms) in CHECKS.items():
        path = ROOT / relative_path
        if not path.exists():
            failures.append(f"{name}: missing {relative_path}")
            continue
        text = path.read_text(encoding="utf-8")
        for term in terms:
            if term not in text:
                failures.append(f"{name}: {relative_path} missing {term!r}")
    if failures:
        print("\n".join(failures), file=sys.stderr)
        return 1
    print("documentation remediation checks passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
