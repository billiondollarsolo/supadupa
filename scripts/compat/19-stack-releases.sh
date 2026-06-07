#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

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

releases_json="$ARTIFACT_DIR/stack-releases.json"
releases_err="$ARTIFACT_DIR/stack-releases.stderr"
releases_status="$(api_json GET "/v1/stack-releases" "" "$releases_json" "$releases_err")"
if [[ "$releases_status" != 2* ]]; then
  fail "stack_releases.list" "expected 2xx, got HTTP $releases_status; see $(basename "$releases_err")"
fi

release_count="$(node -e '
const fs = require("fs");
const releases = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (!Array.isArray(releases)) throw new Error("stack releases response is not an array");
if (releases.length === 0) throw new Error("no stack releases exposed");
const required = ["version", "postgres", "kong", "studio", "postgres_meta", "auth", "rest", "realtime", "storage", "imgproxy", "edge_runtime", "pooler", "analytics", "vector"];
const seen = new Set();
for (const release of releases) {
  for (const key of required) {
    if (!release[key] || typeof release[key] !== "string") throw new Error(`${release.version || "unknown"} missing ${key}`);
    if (/\\s/.test(release[key])) throw new Error(`${release.version} ${key} contains whitespace`);
  }
  if (seen.has(release.version)) throw new Error(`duplicate version ${release.version}`);
  seen.add(release.version);
}
process.stdout.write(String(releases.length));
' "$releases_json")"
pass "stack_releases.list" "count=$release_count"

if [[ -z "${SUPADUPA_SUPPORTED_STACK_VERSIONS:-}" ]]; then
  if node -e '
const fs = require("fs");
const releases = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (releases.length < 3) throw new Error(`expected at least 3 built-in stable releases, got ${releases.length}`);
' "$releases_json" >"$ARTIFACT_DIR/stack-releases-minimum.out" 2>"$ARTIFACT_DIR/stack-releases-minimum.stderr"; then
    pass "stack_releases.minimum_catalog" "built-in catalog covers a few stable releases"
  else
    fail "stack_releases.minimum_catalog" "built-in catalog is too narrow; see stack-releases-minimum.stderr"
  fi
else
  skip "stack_releases.minimum_catalog" "SUPADUPA_SUPPORTED_STACK_VERSIONS filters the release catalog"
fi

project_json="$ARTIFACT_DIR/project.json"
project_err="$ARTIFACT_DIR/project.stderr"
if [[ ! -s "$project_json" ]]; then
  if ! supadupa_cli_authed projects get --ref "$SUPADUPA_TEST_REF" >"$project_json" 2>"$project_err"; then
    fail "stack_releases.project" "failed to fetch project; see $(basename "$project_err")"
  fi
fi

project_version="$(json_get_file_optional "$project_json" spec.stack_version)"
if [[ -z "$project_version" ]]; then
  fail "stack_releases.project" "project response did not include spec.stack_version"
fi
if [[ "$project_version" == "latest" ]]; then
  pass "stack_releases.project" "stack_version=latest resolves through platform default"
else
  if ! node -e '
const fs = require("fs");
const releases = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const version = process.argv[2];
if (!releases.some((release) => release.version === version)) throw new Error(`project stack version ${version} is not in release catalog`);
' "$releases_json" "$project_version"; then
    fail "stack_releases.project" "project stack_version=$project_version is not exposed in /v1/stack-releases"
  fi
  pass "stack_releases.project" "stack_version=$project_version"
fi

unsupported_json="$ARTIFACT_DIR/stack-releases-unsupported-upgrade.json"
unsupported_err="$ARTIFACT_DIR/stack-releases-unsupported-upgrade.stderr"
unsupported_version="supadupa-compat-unsupported-$(date -u +%Y%m%d%H%M%S)"
unsupported_status="$(api_json POST "/v1/projects/$SUPADUPA_TEST_REF/upgrade" "{\"version\":\"$unsupported_version\"}" "$unsupported_json" "$unsupported_err")"
if [[ "$unsupported_status" != "400" ]]; then
  fail "stack_releases.unsupported_upgrade_rejected" "expected HTTP 400, got HTTP $unsupported_status"
fi
if ! grep -qi "unsupported stack version" "$unsupported_json"; then
  fail "stack_releases.unsupported_upgrade_rejected" "response did not explain unsupported stack version"
fi
pass "stack_releases.unsupported_upgrade_rejected" "HTTP 400"

orgs_json="$ARTIFACT_DIR/stack-releases-orgs.json"
orgs_err="$ARTIFACT_DIR/stack-releases-orgs.stderr"
org_id="${SUPADUPA_COMPAT_ORG_ID:-}"
if [[ -z "$org_id" ]]; then
  orgs_status="$(api_json GET "/v1/orgs" "" "$orgs_json" "$orgs_err")"
  if [[ "$orgs_status" == 2* ]]; then
    org_id="$(node -e '
const fs = require("fs");
const orgs = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
if (Array.isArray(orgs) && orgs[0] && orgs[0].id) process.stdout.write(orgs[0].id);
' "$orgs_json")"
  fi
fi

if [[ -z "$org_id" ]]; then
  skip "stack_releases.unsupported_create_rejected" "no org id available"
else
  create_json="$ARTIFACT_DIR/stack-releases-unsupported-create.json"
  create_err="$ARTIFACT_DIR/stack-releases-unsupported-create.stderr"
  create_ref="compat-bad-stack-$(date -u +%H%M%S)"
  create_status="$(api_json POST "/v1/orgs/$org_id/projects" "{\"ref\":\"$create_ref\",\"name\":\"Unsupported Stack\",\"domain\":\"apps.supadupa.invalid\",\"stack_version\":\"$unsupported_version\",\"profile\":\"full\",\"resource_tier\":\"small\"}" "$create_json" "$create_err")"
  if [[ "$create_status" != "400" ]]; then
    fail "stack_releases.unsupported_create_rejected" "expected HTTP 400, got HTTP $create_status"
  fi
  if ! grep -qi "unsupported stack version" "$create_json"; then
    fail "stack_releases.unsupported_create_rejected" "response did not explain unsupported stack version"
  fi
  pass "stack_releases.unsupported_create_rejected" "HTTP 400"
fi
