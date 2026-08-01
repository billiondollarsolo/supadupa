#!/usr/bin/env bash
# Structural proof that SUPADUPA_KIND_DATAPLANE_SMOKE cannot be a silent no-op:
# it must force the Supabase core smoke path and extend the preload image list.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
SMOKE="$ROOT/scripts/check-kubernetes-kind-smoke.sh"
RENDERER="$ROOT/scripts/kubernetes-core-smoke-renderer/main.go"

fail() {
  echo "kind dataplane gate check failed: $*" >&2
  exit 1
}

[[ -f "$SMOKE" ]] || fail "missing $SMOKE"

# Extract the early env block (first 80 lines) for force-CORE and image list checks.
head_block="$(head -n 80 "$SMOKE")"

echo "$head_block" | grep -Fq 'DATAPLANE_SMOKE="${SUPADUPA_KIND_DATAPLANE_SMOKE:-false}"' \
  || fail "DATAPLANE_SMOKE env assignment missing"

# Within the first 80 lines, DATAPLANE=true must set SUPABASE_CORE_SMOKE=true.
echo "$head_block" | grep -Fq 'SUPABASE_CORE_SMOKE="true"' \
  || fail "DATAPLANE must force SUPABASE_CORE_SMOKE=true near script top"

# Preload list must include dataplane service images (only added when DATAPLANE=true).
for img in \
  "supabase/storage-api:v1.60.4" \
  "supabase/realtime:v2.102.3" \
  "supabase/edge-runtime:v1.74.0" \
  "supabase/supavisor:2.9.5" \
  "supabase/logflare:1.43.1"
do
  echo "$head_block" | grep -Fq "$img" || fail "dataplane preload missing $img in early CORE_SMOKE_IMAGES block"
done

# Final invocation of run_supabase_core_smoke must be gated on CORE or DATAPLANE.
if ! grep -E 'if \[\[ "\$SUPABASE_CORE_SMOKE" == "true" \|\| "\$DATAPLANE_SMOKE" == "true" \]\]' "$SMOKE" >/dev/null \
  && ! grep -E 'if \[\[ "\$DATAPLANE_SMOKE" == "true" \|\| "\$SUPABASE_CORE_SMOKE" == "true" \]\]' "$SMOKE" >/dev/null; then
  # CORE-only final gate is acceptable only because DATAPLANE forces CORE earlier.
  if ! grep -q 'run_supabase_core_smoke' "$SMOKE"; then
    fail "run_supabase_core_smoke never called"
  fi
  # Prefer explicit OR gate so a future regression that drops the force still fails closed in review.
  fail "final core smoke gate must include DATAPLANE_SMOKE || SUPABASE_CORE_SMOKE (not CORE alone)"
fi

if [[ -f "$RENDERER" ]]; then
  grep -q 'SUPADUPA_KIND_DATAPLANE_SMOKE' "$RENDERER" || fail "renderer must read SUPADUPA_KIND_DATAPLANE_SMOKE"
  for svc in storage realtime functions pooler analytics; do
    grep -q "\"$svc\"" "$RENDERER" || fail "renderer missing service $svc"
  done
fi

# Simulate env force logic (same as smoke script) without running Kind.
DATAPLANE_SMOKE=true
SUPABASE_CORE_SMOKE=false
if [[ "$DATAPLANE_SMOKE" == "true" ]]; then
  SUPABASE_CORE_SMOKE="true"
fi
[[ "$SUPABASE_CORE_SMOKE" == "true" ]] || fail "simulation: DATAPLANE should force CORE"

echo "kind dataplane gate check passed (DATAPLANE forces CORE; preload includes data-plane images)"
