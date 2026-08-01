#!/usr/bin/env bash
# Fail if internal/control/version.go Version does not match frontend/package.json "version".
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_GO="$ROOT/internal/control/version.go"
PACKAGE_JSON="$ROOT/frontend/package.json"

if [[ ! -f "$VERSION_GO" ]]; then
  echo "missing $VERSION_GO" >&2
  exit 1
fi
if [[ ! -f "$PACKAGE_JSON" ]]; then
  echo "missing $PACKAGE_JSON" >&2
  exit 1
fi

go_version="$(
  sed -nE 's/^const Version = "([^"]+)"/\1/p' "$VERSION_GO" | head -n1
)"
if [[ -z "$go_version" ]]; then
  echo "could not parse const Version from internal/control/version.go" >&2
  exit 1
fi

frontend_version="$(
  # Prefer node when available (handles JSON correctly); fall back to sed for bare CI.
  if command -v node >/dev/null 2>&1; then
    node -e 'const p=require(process.argv[1]); if(!p.version) process.exit(2); process.stdout.write(String(p.version));' "$PACKAGE_JSON"
  else
    sed -nE 's/^[[:space:]]*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' "$PACKAGE_JSON" | head -n1
  fi
)"
if [[ -z "$frontend_version" ]]; then
  echo "could not parse version from frontend/package.json" >&2
  exit 1
fi

if [[ "$go_version" != "$frontend_version" ]]; then
  echo "platform version mismatch: internal/control/version.go Version=${go_version@Q} != frontend/package.json version=${frontend_version@Q}" >&2
  echo "Keep both in sync (plan dual-source platform version check)." >&2
  exit 1
fi

echo "platform version check passed: ${go_version}"
