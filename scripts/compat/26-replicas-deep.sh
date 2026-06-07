#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

if ! compat_bool "${SUPADUPA_COMPAT_REPLICA_VALIDATE:-false}"; then
  skip "replicas_deep.enabled" "set SUPADUPA_COMPAT_REPLICA_VALIDATE=true to run"
  exit 0
fi

created_ref="$(cat "$ARTIFACT_DIR/created-project" 2>/dev/null || true)"
if [[ "$created_ref" != "$SUPADUPA_TEST_REF" ]] &&
  ! compat_bool "${SUPADUPA_COMPAT_ALLOW_REPLICA_ON_EXISTING:-false}"; then
  skip "replicas_deep.guard" "set SUPADUPA_COMPAT_CREATE_PROJECT=true or SUPADUPA_COMPAT_ALLOW_REPLICA_ON_EXISTING=true"
  exit 0
fi

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool curl
require_tool node
ensure_token

token="$(read_secret_file "$ARTIFACT_DIR/token")"
api_base="${SUPADUPA_API_URL%/}"

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

replica_id=""
secondary_replica_id=""
original_features_json="$ARTIFACT_DIR/replicas-deep-original-org-features.json"

cleanup_replicas_deep() {
  if [[ -n "${secondary_replica_id:-}" ]]; then
    api_json DELETE "/v1/projects/$SUPADUPA_TEST_REF/replicas/$secondary_replica_id" "" \
      "$ARTIFACT_DIR/replicas-deep-delete-secondary-cleanup.json" \
      "$ARTIFACT_DIR/replicas-deep-delete-secondary-cleanup.stderr" >/dev/null || true
  fi
  if [[ -n "${replica_id:-}" ]]; then
    api_json DELETE "/v1/projects/$SUPADUPA_TEST_REF/replicas/$replica_id" "" \
      "$ARTIFACT_DIR/replicas-deep-delete-cleanup.json" \
      "$ARTIFACT_DIR/replicas-deep-delete-cleanup.stderr" >/dev/null || true
  fi
  if [[ -s "$original_features_json" && -n "${SUPADUPA_TEST_ORG_ID:-}" ]]; then
    node -e '
const fs = require("fs");
const features = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
process.stdout.write(JSON.stringify({ overrides: features.overrides || {} }));
' "$original_features_json" >"$ARTIFACT_DIR/replicas-deep-restore-features-payload.json" 2>"$ARTIFACT_DIR/replicas-deep-restore-features-payload.stderr" &&
      api_json PUT "/v1/orgs/$SUPADUPA_TEST_ORG_ID/features" "$(cat "$ARTIFACT_DIR/replicas-deep-restore-features-payload.json")" \
        "$ARTIFACT_DIR/replicas-deep-restore-features.json" \
        "$ARTIFACT_DIR/replicas-deep-restore-features.stderr" >/dev/null || true
  fi
}
trap cleanup_replicas_deep EXIT

create_secondary_replica() {
  local name="$1"
  local priority="$2"
  local payload="$ARTIFACT_DIR/replicas-deep-create-secondary-payload.json"
  local out="$ARTIFACT_DIR/replicas-deep-create-secondary.json"
  local err="$ARTIFACT_DIR/replicas-deep-create-secondary.stderr"
  local status

  node -e '
const payload = {
  name: process.argv[1],
  region: process.env.SUPADUPA_COMPAT_REPLICA_REGION || "compat-local",
  tier: process.env.SUPADUPA_COMPAT_REPLICA_TIER || "small",
  read_weight: Number(process.env.SUPADUPA_COMPAT_REPLICA_READ_WEIGHT || 100),
  failover_priority: Number(process.argv[2] || 2),
};
if (process.env.SUPADUPA_COMPAT_REPLICA_HOST_ID) payload.host_id = process.env.SUPADUPA_COMPAT_REPLICA_HOST_ID;
process.stdout.write(JSON.stringify(payload));
' "$name" "$priority" >"$payload"

  status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/replicas" "$(cat "$payload")" "$out" "$err")"
  case "$status" in
    201) ;;
    202) fail "replicas_deep.secondary_create" "replica accepted but provisioning failed or is incomplete; see $(basename "$out")" ;;
    *) fail "replicas_deep.secondary_create" "expected HTTP 201, got HTTP $status; see $(basename "$err")" ;;
  esac
  secondary_replica_id="$(json_get_file "$out" id 2>/dev/null || true)"
  if [[ -z "$secondary_replica_id" ]]; then
    fail "replicas_deep.secondary_create" "secondary replica response missing id"
  fi
  pass "replicas_deep.secondary_create" "replica_id=$secondary_replica_id"
}

verify_primary_replica() {
  local label="$1"
  local expected_id="$2"
  local routing_out="$ARTIFACT_DIR/replicas-deep-$label-routing.json"
  local routing_err="$ARTIFACT_DIR/replicas-deep-$label-routing.stderr"
  local status
  status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/replicas/routing" "" "$routing_out" "$routing_err")"
  if [[ "$status" != 2* ]]; then
    fail "replicas_deep.$label.routing" "expected 2xx, got HTTP $status"
  fi
  node -e '
const fs = require("fs");
const routing = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const expected = process.argv[2];
if (routing.primary_replica_id !== expected) {
  throw new Error(`expected primary_replica_id ${expected}, got ${routing.primary_replica_id || "empty"}`);
}
const primary = (routing.all_targets || []).find((target) => target.replica_id === expected);
if (!primary) throw new Error(`primary target ${expected} missing`);
if (primary.role !== "primary") throw new Error(`expected primary role, got ${primary.role}`);
' "$routing_out" "$expected_id" >"$ARTIFACT_DIR/replicas-deep-$label-routing-check.out" 2>"$ARTIFACT_DIR/replicas-deep-$label-routing-check.stderr" ||
    fail "replicas_deep.$label.routing" "routing did not mark expected primary; see replicas-deep-$label-routing-check.stderr"
  pass "replicas_deep.$label.routing" "primary_replica_id=$expected_id"
}

if compat_bool "${SUPADUPA_COMPAT_ENABLE_REPLICA_FEATURE:-false}"; then
  if [[ -z "${SUPADUPA_TEST_ORG_ID:-}" ]]; then
    fail "replicas_deep.feature_enable" "SUPADUPA_TEST_ORG_ID is required to enable read_replicas temporarily"
  fi
  feature_status="$(api_json GET "/v1/orgs/$SUPADUPA_TEST_ORG_ID/features" "" "$original_features_json" "$ARTIFACT_DIR/replicas-deep-original-org-features.stderr")"
  if [[ "$feature_status" != 2* ]]; then
    fail "replicas_deep.feature_snapshot" "expected 2xx, got HTTP $feature_status"
  fi
  node -e '
const fs = require("fs");
const features = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const overrides = { ...(features.overrides || {}), read_replicas: true };
process.stdout.write(JSON.stringify({ overrides }));
' "$original_features_json" >"$ARTIFACT_DIR/replicas-deep-enable-feature-payload.json"
  enable_status="$(api_json PUT "/v1/orgs/$SUPADUPA_TEST_ORG_ID/features" "$(cat "$ARTIFACT_DIR/replicas-deep-enable-feature-payload.json")" \
    "$ARTIFACT_DIR/replicas-deep-enable-feature.json" \
    "$ARTIFACT_DIR/replicas-deep-enable-feature.stderr")"
  if [[ "$enable_status" != 2* ]]; then
    fail "replicas_deep.feature_enable" "expected 2xx, got HTTP $enable_status"
  fi
  pass "replicas_deep.feature_enable" "temporarily enabled read_replicas"
fi

run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
suffix="$(printf '%s' "$run_id" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9-' | cut -c1-36)"
replica_name="compat-rpl-${suffix:-run}"
replica_payload="$ARTIFACT_DIR/replicas-deep-create-payload.json"
node -e '
const payload = {
  name: process.argv[1],
  region: process.env.SUPADUPA_COMPAT_REPLICA_REGION || "compat-local",
  tier: process.env.SUPADUPA_COMPAT_REPLICA_TIER || "small",
  read_weight: Number(process.env.SUPADUPA_COMPAT_REPLICA_READ_WEIGHT || 100),
  failover_priority: Number(process.env.SUPADUPA_COMPAT_REPLICA_FAILOVER_PRIORITY || 1),
};
if (process.env.SUPADUPA_COMPAT_REPLICA_HOST_ID) payload.host_id = process.env.SUPADUPA_COMPAT_REPLICA_HOST_ID;
process.stdout.write(JSON.stringify(payload));
' "$replica_name" >"$replica_payload"

create_json="$ARTIFACT_DIR/replicas-deep-create.json"
create_err="$ARTIFACT_DIR/replicas-deep-create.stderr"
create_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/replicas" "$(cat "$replica_payload")" "$create_json" "$create_err")"
case "$create_status" in
  201) ;;
  202)
    fail "replicas_deep.create" "replica accepted but provisioning failed or is incomplete; see $(basename "$create_json")"
    ;;
  403)
    if grep -qi "feature flag read_replicas is disabled" "$create_json"; then
      if compat_bool "${SUPADUPA_COMPAT_REQUIRE_REPLICA_VALIDATE:-false}"; then
        fail "replicas_deep.create" "read_replicas feature flag is disabled"
      fi
      skip "replicas_deep.create" "read_replicas feature flag is disabled"
      exit 0
    fi
    fail "replicas_deep.create" "expected HTTP 201, got HTTP $create_status"
    ;;
  *)
    fail "replicas_deep.create" "expected HTTP 201, got HTTP $create_status; see $(basename "$create_err")"
    ;;
esac

replica_id="$(json_get_file "$create_json" id 2>/dev/null || true)"
public_read_uri="$(json_get_file "$create_json" public_read_uri 2>/dev/null || true)"
internal_read_uri="$(json_get_file "$create_json" internal_read_uri 2>/dev/null || true)"
read_uri="$(json_get_file "$create_json" read_uri 2>/dev/null || true)"
if [[ -z "$replica_id" || -z "$public_read_uri" || -z "$internal_read_uri" || -z "$read_uri" ]]; then
  fail "replicas_deep.create" "replica response missing id/read_uri/public_read_uri/internal_read_uri"
fi
if [[ "$read_uri" != "$public_read_uri" ]]; then
  fail "replicas_deep.public_uri" "read_uri must match public_read_uri"
fi
expected_host="db-replica-$replica_name-$SUPADUPA_TEST_REF.$(node -e '
const uri = process.argv[1];
const url = new URL(uri);
process.stdout.write(url.hostname.split(".").slice(1).join("."));
' "$public_read_uri")"
actual_host="$(url_host "$public_read_uri")"
if [[ "$actual_host" != "$expected_host" ]]; then
  fail "replicas_deep.public_uri" "expected host $expected_host, got $actual_host"
fi
if [[ "$public_read_uri" != *"sslmode=require"* ]]; then
  fail "replicas_deep.public_uri" "public URI must require TLS"
fi
if [[ "$internal_read_uri" != *".replica.internal:5432/"* ]]; then
  fail "replicas_deep.internal_uri" "internal URI must stay on replica.internal"
fi
pass "replicas_deep.create" "replica_id=$replica_id host=$actual_host"

list_json="$ARTIFACT_DIR/replicas-deep-list.json"
list_err="$ARTIFACT_DIR/replicas-deep-list.stderr"
list_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/replicas" "" "$list_json" "$list_err")"
if [[ "$list_status" != 2* ]]; then
  fail "replicas_deep.list" "expected 2xx, got HTTP $list_status"
fi
node -e '
const fs = require("fs");
const replicas = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const id = process.argv[2];
const replica = Array.isArray(replicas) && replicas.find((item) => item.id === id);
if (!replica) throw new Error(`replica ${id} not found in list`);
if (replica.status !== "healthy") throw new Error(`expected healthy status, got ${replica.status}`);
if (replica.role !== "read") throw new Error(`expected read role, got ${replica.role}`);
' "$list_json" "$replica_id" >"$ARTIFACT_DIR/replicas-deep-list-check.out" 2>"$ARTIFACT_DIR/replicas-deep-list-check.stderr" ||
  fail "replicas_deep.list" "replica missing or not healthy/read; see replicas-deep-list-check.stderr"
pass "replicas_deep.list" "healthy read replica visible"

routing_json="$ARTIFACT_DIR/replicas-deep-routing.json"
routing_err="$ARTIFACT_DIR/replicas-deep-routing.stderr"
routing_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/replicas/routing" "" "$routing_json" "$routing_err")"
if [[ "$routing_status" != 2* ]]; then
  fail "replicas_deep.routing" "expected 2xx, got HTTP $routing_status"
fi
node -e '
const fs = require("fs");
const routing = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const id = process.argv[2];
if (!Array.isArray(routing.healthy_read_targets)) throw new Error("healthy_read_targets is not an array");
if (!routing.healthy_read_targets.some((target) => target.replica_id === id && target.role === "read")) {
  throw new Error(`read target ${id} missing`);
}
if (!routing.failover_candidate || routing.failover_candidate.replica_id !== id) {
  throw new Error("created replica is not the failover candidate");
}
' "$routing_json" "$replica_id" >"$ARTIFACT_DIR/replicas-deep-routing-check.out" 2>"$ARTIFACT_DIR/replicas-deep-routing-check.stderr" ||
  fail "replicas_deep.routing" "routing response missing created replica; see replicas-deep-routing-check.stderr"
pass "replicas_deep.routing" "read target and failover candidate visible"

manifest_json="$ARTIFACT_DIR/replicas-deep-route-manifest.json"
manifest_err="$ARTIFACT_DIR/replicas-deep-route-manifest.stderr"
manifest_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/route-manifest" "" "$manifest_json" "$manifest_err")"
if [[ "$manifest_status" != 2* ]]; then
  fail "replicas_deep.route_manifest" "expected 2xx, got HTTP $manifest_status"
fi
expected_upstream="$SUPADUPA_TEST_REF-db-replica-$replica_name:5432"
node -e '
const fs = require("fs");
const manifest = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const host = process.argv[2];
const upstream = process.argv[3];
if (!Array.isArray(manifest.tcp_routes)) throw new Error("tcp_routes is not an array");
const route = manifest.tcp_routes.find((item) => item.name === "db-replica-" + process.argv[4]);
if (!route) throw new Error("replica TCP route missing");
if (route.fqdn !== host) throw new Error(`expected fqdn ${host}, got ${route.fqdn}`);
if (route.upstream_address !== upstream) throw new Error(`expected upstream ${upstream}, got ${route.upstream_address}`);
if (route.entrypoint !== "postgres" || route.public_port !== 5432 || route.tls !== true) {
  throw new Error("replica TCP route does not expose TLS Postgres on public 5432");
}
' "$manifest_json" "$actual_host" "$expected_upstream" "$replica_name" >"$ARTIFACT_DIR/replicas-deep-route-manifest-check.out" 2>"$ARTIFACT_DIR/replicas-deep-route-manifest-check.stderr" ||
  fail "replicas_deep.route_manifest" "replica TCP route mismatch; see replicas-deep-route-manifest-check.stderr"
pass "replicas_deep.route_manifest" "$actual_host -> $expected_upstream"

if compat_bool "${SUPADUPA_COMPAT_REPLICA_PROMOTE_VALIDATE:-false}" ||
  compat_bool "${SUPADUPA_COMPAT_REPLICA_FAILOVER_VALIDATE:-false}"; then
  if [[ "$created_ref" != "$SUPADUPA_TEST_REF" ]]; then
    fail "replicas_deep.promote_failover.guard" "promotion/failover validation requires a compat-created disposable project"
  fi

  if compat_bool "${SUPADUPA_COMPAT_REPLICA_PROMOTE_VALIDATE:-false}" &&
    compat_bool "${SUPADUPA_COMPAT_REPLICA_FAILOVER_VALIDATE:-false}"; then
    create_secondary_replica "${replica_name}-fo" 2
  fi

  if compat_bool "${SUPADUPA_COMPAT_REPLICA_PROMOTE_VALIDATE:-false}"; then
    promote_json="$ARTIFACT_DIR/replicas-deep-promote.json"
    promote_err="$ARTIFACT_DIR/replicas-deep-promote.stderr"
    promote_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/replicas/$replica_id/promote" '{"reason":"compat validation"}' "$promote_json" "$promote_err")"
    if [[ "$promote_status" != 2* ]]; then
      fail "replicas_deep.promote" "expected 2xx, got HTTP $promote_status"
    fi
    node -e '
const fs = require("fs");
const replica = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const expected = process.argv[2];
if (replica.id !== expected) throw new Error(`expected promoted id ${expected}, got ${replica.id}`);
if (replica.role !== "primary") throw new Error(`expected primary role, got ${replica.role}`);
if (!replica.promoted_at) throw new Error("promoted_at missing");
' "$promote_json" "$replica_id" >"$ARTIFACT_DIR/replicas-deep-promote-check.out" 2>"$ARTIFACT_DIR/replicas-deep-promote-check.stderr" ||
      fail "replicas_deep.promote" "promote response mismatch; see replicas-deep-promote-check.stderr"
    pass "replicas_deep.promote" "replica promoted"
    verify_primary_replica "promote" "$replica_id"
  fi

  if compat_bool "${SUPADUPA_COMPAT_REPLICA_FAILOVER_VALIDATE:-false}"; then
    expected_failover_id="$replica_id"
    if [[ -n "$secondary_replica_id" ]]; then
      expected_failover_id="$secondary_replica_id"
    fi
    failover_json="$ARTIFACT_DIR/replicas-deep-failover.json"
    failover_err="$ARTIFACT_DIR/replicas-deep-failover.stderr"
    failover_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/replicas/failover" '{"reason":"compat validation"}' "$failover_json" "$failover_err")"
    if [[ "$failover_status" != 2* ]]; then
      fail "replicas_deep.failover" "expected 2xx, got HTTP $failover_status"
    fi
    node -e '
const fs = require("fs");
const replica = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const expected = process.argv[2];
if (replica.id !== expected) throw new Error(`expected failover id ${expected}, got ${replica.id}`);
if (replica.role !== "primary") throw new Error(`expected primary role, got ${replica.role}`);
if (!replica.promoted_at) throw new Error("promoted_at missing");
' "$failover_json" "$expected_failover_id" >"$ARTIFACT_DIR/replicas-deep-failover-check.out" 2>"$ARTIFACT_DIR/replicas-deep-failover-check.stderr" ||
      fail "replicas_deep.failover" "failover response mismatch; see replicas-deep-failover-check.stderr"
    pass "replicas_deep.failover" "replica promoted by failover"
    verify_primary_replica "failover" "$expected_failover_id"
  fi

  skip "replicas_deep.delete" "promoted replicas are retained for disposable project cleanup"
  skip "replicas_deep.route_manifest_delete" "project cleanup removes promoted replica routes"
  exit 0
fi

delete_json="$ARTIFACT_DIR/replicas-deep-delete.json"
delete_err="$ARTIFACT_DIR/replicas-deep-delete.stderr"
delete_status="$(api_json DELETE "/v1/projects/$SUPADUPA_TEST_REF/replicas/$replica_id" "" "$delete_json" "$delete_err")"
if [[ "$delete_status" != "204" ]]; then
  fail "replicas_deep.delete" "expected HTTP 204, got HTTP $delete_status"
fi
replica_id=""
pass "replicas_deep.delete" "replica removed"

manifest_after_json="$ARTIFACT_DIR/replicas-deep-route-manifest-after-delete.json"
manifest_after_err="$ARTIFACT_DIR/replicas-deep-route-manifest-after-delete.stderr"
manifest_after_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/route-manifest" "" "$manifest_after_json" "$manifest_after_err")"
if [[ "$manifest_after_status" != 2* ]]; then
  fail "replicas_deep.route_manifest_delete" "expected 2xx, got HTTP $manifest_after_status"
fi
node -e '
const fs = require("fs");
const manifest = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const host = process.argv[2];
if ((manifest.tcp_routes || []).some((item) => item.fqdn === host)) {
  throw new Error(`replica TCP route still present for ${host}`);
}
' "$manifest_after_json" "$actual_host" >"$ARTIFACT_DIR/replicas-deep-route-manifest-after-delete-check.out" 2>"$ARTIFACT_DIR/replicas-deep-route-manifest-after-delete-check.stderr" ||
  fail "replicas_deep.route_manifest_delete" "replica TCP route still present after delete"
pass "replicas_deep.route_manifest_delete" "replica route removed"

skip "replicas_deep.promote_failover" "set SUPADUPA_COMPAT_REPLICA_PROMOTE_VALIDATE=true or SUPADUPA_COMPAT_REPLICA_FAILOVER_VALIDATE=true on a disposable project"
