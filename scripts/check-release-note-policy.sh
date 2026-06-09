#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
POLICY="$ROOT/docs/release-note-policy.md"

if [[ ! -f "$POLICY" ]]; then
  echo "missing docs/release-note-policy.md" >&2
  exit 1
fi

required_policy_terms=(
  "Authentication"
  "Persistence"
  "Terraform"
  "Deployment"
  "Required action"
  "Validation commands"
  "Fixed Looks Like"
)

for term in "${required_policy_terms[@]}"; do
  if ! grep -Fq "$term" "$POLICY"; then
    echo "release note policy missing required term: $term" >&2
    exit 1
  fi
done

if [[ "${SUPADUPA_RELEASE_NOTE_SKIP_DIFF:-false}" == "true" ]]; then
  echo "release note policy content check passed"
  exit 0
fi

cd "$ROOT"

if ! command -v git >/dev/null 2>&1 || ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "release note policy content check passed"
  exit 0
fi

base="${SUPADUPA_RELEASE_NOTE_BASE:-HEAD}"
if ! git rev-parse --verify "$base" >/dev/null 2>&1; then
  echo "release note policy content check passed; diff base '$base' is unavailable" >&2
  exit 0
fi

changed_files="$(
  {
    git diff --name-only "$base" --
    git ls-files --others --exclude-standard
  } | sort -u
)"

if [[ -z "$changed_files" ]]; then
  echo "release note policy check passed"
  exit 0
fi

sensitive_changed=false
while IFS= read -r path; do
  case "$path" in
    .env.example | \
    deploy/* | \
    charts/* | \
    migrations/* | \
    cmd/supadupa/* | \
    cmd/supadupa-operator/* | \
    internal/api/*auth* | \
    internal/api/*secret* | \
    internal/api/*sso* | \
    internal/api/scim* | \
    internal/control/*auth* | \
    internal/control/*sso* | \
    internal/control/*store* | \
    internal/control/persistent* | \
    internal/metadb/* | \
    internal/operator/* | \
    internal/provisioner/* | \
    internal/reconciler/* | \
    internal/scheduler/backups* | \
    internal/terraform/* | \
    scripts/setup-compose.sh | \
    scripts/setup-local-dns.sh | \
    scripts/compat/* | \
    go.mod | \
    go.sum | \
    frontend/package.json | \
    frontend/package-lock.json)
      sensitive_changed=true
      break
      ;;
  esac
done <<< "$changed_files"

if [[ "$sensitive_changed" != "true" ]]; then
  echo "release note policy check passed"
  exit 0
fi

operator_note_changed=false
while IFS= read -r path; do
  case "$path" in
    README.md | \
    docs/install.md | \
    docs/operations.md | \
    docs/security.md | \
    docs/kubernetes.md | \
    docs/dns-tls.md | \
    docs/upgrades.md | \
    docs/release-note-policy.md | \
    docs/project-review-remediation-plan.md | \
    scripts/compat/README.md)
      operator_note_changed=true
      break
      ;;
  esac
done <<< "$changed_files"

if [[ "$operator_note_changed" != "true" ]]; then
  cat >&2 <<'EOF'
sensitive operational files changed without an operator-note documentation update.

Add the release impact, required operator action, validation, and rollback/risk notes
to an appropriate docs file listed in docs/release-note-policy.md.
EOF
  exit 1
fi

echo "release note policy check passed"
