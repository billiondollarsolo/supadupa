#!/usr/bin/env bash
set -euo pipefail

required_patterns=(
  ".env"
  ".env.*"
  ".npmrc"
  ".aws"
  ".ssh"
  ".terraform"
  "terraform.tfstate"
  "terraform.tfstate.*"
  "*.key"
  "*.pem"
  "*.crt"
  "*.csr"
  "runtime"
  "**/node_modules"
  "scripts/compat/artifacts"
)

for pattern in "${required_patterns[@]}"; do
  if ! grep -Fxq "$pattern" .dockerignore; then
    echo ".dockerignore missing required pattern: $pattern" >&2
    exit 1
  fi
done

echo ".dockerignore policy check passed"
