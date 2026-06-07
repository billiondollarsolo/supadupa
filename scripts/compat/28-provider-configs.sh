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
ensure_profile

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

json_array_has_name() {
  local file="$1"
  local name="$2"
  node - "$file" "$name" <<'NODE'
const fs = require("fs");
const values = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (!Array.isArray(values)) throw new Error("expected array");
if (!values.some((value) => value && value.name === process.argv[3])) {
  throw new Error(`missing ${process.argv[3]}`);
}
NODE
}

json_array_missing_name() {
  local file="$1"
  local name="$2"
  node - "$file" "$name" <<'NODE'
const fs = require("fs");
const values = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (!Array.isArray(values)) throw new Error("expected array");
if (values.some((value) => value && value.name === process.argv[3])) {
  throw new Error(`still present ${process.argv[3]}`);
}
NODE
}

json_id_to_file() {
  local file="$1"
  local path="$2"
  local out="$3"
  local value

  value="$(json_get_file "$file" "$path" 2>/dev/null || true)"
  if [[ -z "$value" ]]; then
    fail "provider_configs.id.$path" "missing $path in $file"
  fi
  printf '%s' "$value" >"$out"
}

assert_masked_and_not_leaked() {
  local test_name="$1"
  local file="$2"
  local leaked="$3"
  if grep -Fq "$leaked" "$file"; then
    fail "$test_name" "raw sensitive value leaked in $file"
  fi
  if ! grep -Fq '"********"' "$file"; then
    fail "$test_name" "masked sentinel missing in $file"
  fi
}

run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
suffix="$(printf '%s' "$run_id" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9' | cut -c1-24)"
if [[ ${#suffix} -lt 8 ]]; then
  suffix="${suffix}$(date -u +%H%M%S)"
fi

log_drain_id_file="$ARTIFACT_DIR/provider-configs-log-drain.id"
auth_client_id="compat-client-${suffix}"
replication_id_file="$ARTIFACT_DIR/provider-configs-replication.id"
embedding_id_file="$ARTIFACT_DIR/provider-configs-embedding.id"
vector_name="compat-vector-${suffix}"
network_id_file="$ARTIFACT_DIR/provider-configs-network.id"

original_features_json="$ARTIFACT_DIR/provider-configs-original-org-features.json"
org_id="${SUPADUPA_TEST_ORG_ID:-}"

cleanup_provider_configs() {
  if [[ -s "$network_id_file" ]]; then
    supadupa_cli_authed network-connections delete --ref "$SUPADUPA_TEST_REF" --id "$(cat "$network_id_file")" \
      >"$ARTIFACT_DIR/provider-configs-network-cleanup.out" 2>"$ARTIFACT_DIR/provider-configs-network-cleanup.stderr" || true
  fi
  supadupa_cli_authed vector-buckets delete --ref "$SUPADUPA_TEST_REF" --name "$vector_name" \
    >"$ARTIFACT_DIR/provider-configs-vector-cleanup.out" 2>"$ARTIFACT_DIR/provider-configs-vector-cleanup.stderr" || true
  if [[ -s "$embedding_id_file" ]]; then
    supadupa_cli_authed embeddings delete --ref "$SUPADUPA_TEST_REF" --id "$(cat "$embedding_id_file")" \
      >"$ARTIFACT_DIR/provider-configs-embedding-cleanup.out" 2>"$ARTIFACT_DIR/provider-configs-embedding-cleanup.stderr" || true
  fi
  if [[ -s "$replication_id_file" ]]; then
    supadupa_cli_authed replication delete --ref "$SUPADUPA_TEST_REF" --id "$(cat "$replication_id_file")" \
      >"$ARTIFACT_DIR/provider-configs-replication-cleanup.out" 2>"$ARTIFACT_DIR/provider-configs-replication-cleanup.stderr" || true
  fi
  supadupa_cli_authed auth-clients delete --ref "$SUPADUPA_TEST_REF" --client-id "$auth_client_id" \
    >"$ARTIFACT_DIR/provider-configs-auth-client-cleanup.out" 2>"$ARTIFACT_DIR/provider-configs-auth-client-cleanup.stderr" || true
  if [[ -s "$log_drain_id_file" ]]; then
    supadupa_cli_authed log-drains delete --ref "$SUPADUPA_TEST_REF" --id "$(cat "$log_drain_id_file")" \
      >"$ARTIFACT_DIR/provider-configs-log-drain-cleanup.out" 2>"$ARTIFACT_DIR/provider-configs-log-drain-cleanup.stderr" || true
  fi
  if [[ -s "$original_features_json" && -n "${org_id:-}" ]]; then
    node -e '
const fs = require("fs");
const features = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
process.stdout.write(JSON.stringify({ overrides: features.overrides || {} }));
' "$original_features_json" >"$ARTIFACT_DIR/provider-configs-restore-features-payload.json" 2>"$ARTIFACT_DIR/provider-configs-restore-features-payload.stderr" &&
      api_json PUT "/v1/orgs/$org_id/features" "$(cat "$ARTIFACT_DIR/provider-configs-restore-features-payload.json")" \
        "$ARTIFACT_DIR/provider-configs-restore-features.json" \
        "$ARTIFACT_DIR/provider-configs-restore-features.stderr" >/dev/null || true
  fi
}
trap cleanup_provider_configs EXIT

if [[ -z "$org_id" ]]; then
  project_json="$ARTIFACT_DIR/provider-configs-project.json"
  if supadupa_cli_authed projects inspect --ref "$SUPADUPA_TEST_REF" >"$project_json" 2>"$ARTIFACT_DIR/provider-configs-project.stderr"; then
    org_id="$(json_get_file_optional "$project_json" org_id)"
  fi
fi

if [[ -n "$org_id" ]]; then
  feature_status="$(api_json GET "/v1/orgs/$org_id/features" "" "$original_features_json" "$ARTIFACT_DIR/provider-configs-original-features.stderr")"
  if [[ "$feature_status" == 2* ]]; then
    node -e '
const fs = require("fs");
const features = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const overrides = { ...(features.overrides || {}), log_drains: true, network_restrictions: true };
process.stdout.write(JSON.stringify({ overrides }));
' "$original_features_json" >"$ARTIFACT_DIR/provider-configs-enable-features-payload.json"
    enable_status="$(api_json PUT "/v1/orgs/$org_id/features" "$(cat "$ARTIFACT_DIR/provider-configs-enable-features-payload.json")" \
      "$ARTIFACT_DIR/provider-configs-enable-features.json" \
      "$ARTIFACT_DIR/provider-configs-enable-features.stderr")"
    if [[ "$enable_status" != 2* ]]; then
      fail "provider_configs.feature_enable" "expected 2xx, got HTTP $enable_status"
    fi
    pass "provider_configs.feature_enable" "temporarily enabled log_drains and network_restrictions"
  else
    rm -f "$original_features_json"
    skip "provider_configs.feature_enable" "org feature snapshot unavailable; gated resources may be skipped"
  fi
else
  skip "provider_configs.feature_enable" "org id unavailable; gated resources may be skipped"
fi

bad_auth_out="$ARTIFACT_DIR/provider-configs-auth-client-bad.out"
bad_auth_err="$ARTIFACT_DIR/provider-configs-auth-client-bad.stderr"
if supadupa_cli_authed auth-clients create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "Compat Bad Client" \
  --client-id "compat-bad-${suffix}" \
  --client-secret-handle "raw-secret" \
  --redirect-uri "https://example.com/auth/callback" \
  >"$bad_auth_out" 2>"$bad_auth_err"; then
  fail "provider_configs.auth_client_raw_secret_rejected" "raw client secret handle was accepted"
fi
if ! grep -qi "secret://" "$bad_auth_out" "$bad_auth_err"; then
  fail "provider_configs.auth_client_raw_secret_rejected" "expected secret:// validation message"
fi
pass "provider_configs.auth_client_raw_secret_rejected" "raw confidential client secret rejected"

log_drain_create="$ARTIFACT_DIR/provider-configs-log-drain-create.json"
if supadupa_cli_authed log-drains create \
  --ref "$SUPADUPA_TEST_REF" \
  --target https \
  --config "url=https://logs.example.com/supadupa-compat/${suffix}" \
  --config "token=compat-log-token-${suffix}" \
  >"$log_drain_create" 2>"$ARTIFACT_DIR/provider-configs-log-drain-create.stderr"; then
  json_id_to_file "$log_drain_create" id "$log_drain_id_file"
  assert_masked_and_not_leaked "provider_configs.log_drain_mask_create" "$log_drain_create" "compat-log-token-${suffix}"
  pass "provider_configs.log_drain_create" "id=$(cat "$log_drain_id_file")"
else
  if grep -qi "feature flag log_drains is disabled" "$ARTIFACT_DIR/provider-configs-log-drain-create.stderr" "$log_drain_create" 2>/dev/null; then
    skip "provider_configs.log_drain_create" "log_drains feature flag is disabled"
  else
    fail "provider_configs.log_drain_create" "create failed; see provider-configs-log-drain-create.stderr"
  fi
fi

auth_create="$ARTIFACT_DIR/provider-configs-auth-client-create.json"
if supadupa_cli_authed auth-clients create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "Compat Client ${suffix}" \
  --client-id "$auth_client_id" \
  --client-secret-handle "secret://projects/$SUPADUPA_TEST_REF/auth-client-${suffix}" \
  --redirect-uri "https://example.com/auth/callback" \
  --grant-type authorization_code \
  --grant-type refresh_token \
  --scope openid \
  --scope email \
  --scope profile \
  >"$auth_create" 2>"$ARTIFACT_DIR/provider-configs-auth-client-create.stderr"; then
  assert_masked_and_not_leaked "provider_configs.auth_client_mask_create" "$auth_create" "secret://projects/$SUPADUPA_TEST_REF/auth-client-${suffix}"
  pass "provider_configs.auth_client_create" "$auth_client_id registered"
else
  fail "provider_configs.auth_client_create" "create failed; see provider-configs-auth-client-create.stderr"
fi

bad_repl_out="$ARTIFACT_DIR/provider-configs-replication-bad.out"
bad_repl_err="$ARTIFACT_DIR/provider-configs-replication-bad.stderr"
if supadupa_cli_authed replication create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "compat-repl-bad-${suffix}" \
  --type etl \
  --source-table "compat_source_${suffix}" \
  --destination s3 \
  --config "bucket=compat-bucket-${suffix}" \
  --config "access_key=raw" \
  >"$bad_repl_out" 2>"$bad_repl_err"; then
  fail "provider_configs.replication_raw_secret_rejected" "raw replication access_key was accepted"
fi
if ! grep -qi "secret://" "$bad_repl_out" "$bad_repl_err"; then
  fail "provider_configs.replication_raw_secret_rejected" "expected secret:// validation message"
fi
pass "provider_configs.replication_raw_secret_rejected" "raw sensitive replication config rejected"

replication_create="$ARTIFACT_DIR/provider-configs-replication-create.json"
if supadupa_cli_authed replication create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "compat-repl-${suffix}" \
  --type etl \
  --source-table "compat_source_${suffix}" \
  --destination s3 \
  --credential-handle "secret://projects/$SUPADUPA_TEST_REF/replication-credential-${suffix}" \
  --config "bucket=compat-bucket-${suffix}" \
  --config "access_key=secret://projects/$SUPADUPA_TEST_REF/replication-access-${suffix}" \
  >"$replication_create" 2>"$ARTIFACT_DIR/provider-configs-replication-create.stderr"; then
  json_id_to_file "$replication_create" id "$replication_id_file"
  assert_masked_and_not_leaked "provider_configs.replication_mask_create" "$replication_create" "secret://projects/$SUPADUPA_TEST_REF/replication-access-${suffix}"
  pass "provider_configs.replication_create" "id=$(cat "$replication_id_file")"
else
  fail "provider_configs.replication_create" "create failed; see provider-configs-replication-create.stderr"
fi

embedding_create="$ARTIFACT_DIR/provider-configs-embedding-create.json"
if supadupa_cli_authed embeddings create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "compat-embed-${suffix}" \
  --source-table "compat_docs_${suffix}" \
  --source-column body \
  --primary-key-column id \
  --destination-table "compat_embeddings_${suffix}" \
  --destination-column embedding \
  --provider openai \
  --model text-embedding-3-small \
  --dimension 1536 \
  --schedule manual \
  --batch-size 100 \
  >"$embedding_create" 2>"$ARTIFACT_DIR/provider-configs-embedding-create.stderr"; then
  json_id_to_file "$embedding_create" id "$embedding_id_file"
  pass "provider_configs.embedding_create" "id=$(cat "$embedding_id_file")"
else
  fail "provider_configs.embedding_create" "create failed; see provider-configs-embedding-create.stderr"
fi

bad_vector_out="$ARTIFACT_DIR/provider-configs-vector-bad.out"
bad_vector_err="$ARTIFACT_DIR/provider-configs-vector-bad.stderr"
if supadupa_cli_authed vector-buckets create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "compat-vector-bad-${suffix}" \
  --storage-backend s3 \
  --storage-uri "s3://vector-buckets/compat-vector-bad-${suffix}" \
  --metadata "access_key=raw" \
  >"$bad_vector_out" 2>"$bad_vector_err"; then
  fail "provider_configs.vector_raw_secret_rejected" "raw vector access_key was accepted"
fi
if ! grep -qi "secret://" "$bad_vector_out" "$bad_vector_err"; then
  fail "provider_configs.vector_raw_secret_rejected" "expected secret:// validation message"
fi
pass "provider_configs.vector_raw_secret_rejected" "raw sensitive vector metadata rejected"

vector_create="$ARTIFACT_DIR/provider-configs-vector-create.json"
if supadupa_cli_authed vector-buckets create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "$vector_name" \
  --dimension 1536 \
  --distance cosine \
  --index-method hnsw \
  --storage-backend s3 \
  --storage-uri "s3://vector-buckets/$vector_name" \
  --metadata "purpose=compat" \
  --metadata "access_key=secret://projects/$SUPADUPA_TEST_REF/vector-access-${suffix}" \
  >"$vector_create" 2>"$ARTIFACT_DIR/provider-configs-vector-create.stderr"; then
  assert_masked_and_not_leaked "provider_configs.vector_mask_create" "$vector_create" "secret://projects/$SUPADUPA_TEST_REF/vector-access-${suffix}"
  pass "provider_configs.vector_create" "$vector_name configured"
else
  fail "provider_configs.vector_create" "create failed; see provider-configs-vector-create.stderr"
fi

bad_network_out="$ARTIFACT_DIR/provider-configs-network-bad.out"
bad_network_err="$ARTIFACT_DIR/provider-configs-network-bad.stderr"
if supadupa_cli_authed network-connections create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "compat-net-bad-${suffix}" \
  --type privatelink \
  --provider aws \
  --region us-east-1 \
  --cidr 10.0.0.0/16 \
  --config "token=raw" \
  >"$bad_network_out" 2>"$bad_network_err"; then
  fail "provider_configs.network_raw_secret_rejected" "raw network token was accepted"
fi
if ! grep -qi "secret://" "$bad_network_out" "$bad_network_err"; then
  fail "provider_configs.network_raw_secret_rejected" "expected secret:// validation message"
fi
pass "provider_configs.network_raw_secret_rejected" "raw sensitive network config rejected"

network_create="$ARTIFACT_DIR/provider-configs-network-create.json"
if supadupa_cli_authed network-connections create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "compat-net-${suffix}" \
  --type privatelink \
  --provider aws \
  --region us-east-1 \
  --endpoint-id "vpce-compat-${suffix}" \
  --cidr 10.0.0.0/16 \
  --config account_id=123456789012 \
  --config "token=secret://projects/$SUPADUPA_TEST_REF/network-token-${suffix}" \
  >"$network_create" 2>"$ARTIFACT_DIR/provider-configs-network-create.stderr"; then
  json_id_to_file "$network_create" id "$network_id_file"
  assert_masked_and_not_leaked "provider_configs.network_mask_create" "$network_create" "secret://projects/$SUPADUPA_TEST_REF/network-token-${suffix}"
  pass "provider_configs.network_create" "id=$(cat "$network_id_file")"
else
  if grep -qi "feature flag network_restrictions is disabled" "$ARTIFACT_DIR/provider-configs-network-create.stderr" "$network_create" 2>/dev/null; then
    skip "provider_configs.network_create" "network_restrictions feature flag is disabled"
  else
    fail "provider_configs.network_create" "create failed; see provider-configs-network-create.stderr"
  fi
fi

if [[ -s "$log_drain_id_file" ]]; then
  log_drain_list="$ARTIFACT_DIR/provider-configs-log-drain-list.json"
  if supadupa_cli_authed log-drains list --ref "$SUPADUPA_TEST_REF" >"$log_drain_list" 2>"$ARTIFACT_DIR/provider-configs-log-drain-list.stderr"; then
    assert_masked_and_not_leaked "provider_configs.log_drain_mask_list" "$log_drain_list" "compat-log-token-${suffix}"
    pass "provider_configs.log_drain_list" "created drain visible and masked"
  else
    fail "provider_configs.log_drain_list" "list failed; see provider-configs-log-drain-list.stderr"
  fi
fi

auth_list="$ARTIFACT_DIR/provider-configs-auth-client-list.json"
if supadupa_cli_authed auth-clients list --ref "$SUPADUPA_TEST_REF" >"$auth_list" 2>"$ARTIFACT_DIR/provider-configs-auth-client-list.stderr"; then
  node - "$auth_list" "$auth_client_id" <<'NODE'
const fs = require("fs");
const values = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const client = values.find((value) => value.client_id === process.argv[3]);
if (!client) throw new Error("client missing");
if (client.client_secret_handle !== "********") throw new Error(`unmasked handle ${client.client_secret_handle}`);
NODE
  pass "provider_configs.auth_client_list" "$auth_client_id visible and masked"
else
  fail "provider_configs.auth_client_list" "list failed; see provider-configs-auth-client-list.stderr"
fi

replication_list="$ARTIFACT_DIR/provider-configs-replication-list.json"
if supadupa_cli_authed replication list --ref "$SUPADUPA_TEST_REF" >"$replication_list" 2>"$ARTIFACT_DIR/provider-configs-replication-list.stderr"; then
  json_array_has_name "$replication_list" "compat-repl-${suffix}"
  assert_masked_and_not_leaked "provider_configs.replication_mask_list" "$replication_list" "secret://projects/$SUPADUPA_TEST_REF/replication-access-${suffix}"
  pass "provider_configs.replication_list" "created pipeline visible and masked"
else
  fail "provider_configs.replication_list" "list failed; see provider-configs-replication-list.stderr"
fi

embedding_list="$ARTIFACT_DIR/provider-configs-embedding-list.json"
if supadupa_cli_authed embeddings list --ref "$SUPADUPA_TEST_REF" >"$embedding_list" 2>"$ARTIFACT_DIR/provider-configs-embedding-list.stderr"; then
  json_array_has_name "$embedding_list" "compat-embed-${suffix}"
  pass "provider_configs.embedding_list" "created embedding job visible"
else
  fail "provider_configs.embedding_list" "list failed; see provider-configs-embedding-list.stderr"
fi

vector_list="$ARTIFACT_DIR/provider-configs-vector-list.json"
if supadupa_cli_authed vector-buckets list --ref "$SUPADUPA_TEST_REF" >"$vector_list" 2>"$ARTIFACT_DIR/provider-configs-vector-list.stderr"; then
  json_array_has_name "$vector_list" "$vector_name"
  assert_masked_and_not_leaked "provider_configs.vector_mask_list" "$vector_list" "secret://projects/$SUPADUPA_TEST_REF/vector-access-${suffix}"
  pass "provider_configs.vector_list" "created vector bucket visible and masked"
else
  fail "provider_configs.vector_list" "list failed; see provider-configs-vector-list.stderr"
fi

if [[ -s "$network_id_file" ]]; then
  network_list="$ARTIFACT_DIR/provider-configs-network-list.json"
  if supadupa_cli_authed network-connections list --ref "$SUPADUPA_TEST_REF" >"$network_list" 2>"$ARTIFACT_DIR/provider-configs-network-list.stderr"; then
    json_array_has_name "$network_list" "compat-net-${suffix}"
    assert_masked_and_not_leaked "provider_configs.network_mask_list" "$network_list" "secret://projects/$SUPADUPA_TEST_REF/network-token-${suffix}"
    pass "provider_configs.network_list" "created network declaration visible and masked"
  else
    fail "provider_configs.network_list" "list failed; see provider-configs-network-list.stderr"
  fi
fi

metrics_file="$ARTIFACT_DIR/provider-configs-project-metrics.json"
metrics_status="$(api_json GET "/v1/projects/$SUPADUPA_TEST_REF/metrics" "" "$metrics_file" "$ARTIFACT_DIR/provider-configs-project-metrics.stderr")"
if [[ "$metrics_status" != 2* ]]; then
  fail "provider_configs.metrics" "expected 2xx, got HTTP $metrics_status"
fi
node - "$metrics_file" "$([[ -s "$log_drain_id_file" ]] && printf true || printf false)" "$([[ -s "$network_id_file" ]] && printf true || printf false)" <<'NODE'
const fs = require("fs");
const metrics = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const requireLogDrain = process.argv[3] === "true";
const requireNetwork = process.argv[4] === "true";
const checks = {
  auth_clients: true,
  replication_pipelines: true,
  embedding_jobs: true,
  vector_buckets: true,
  log_drains: requireLogDrain,
  network_connections: requireNetwork,
};
for (const [key, required] of Object.entries(checks)) {
  if (!required) continue;
  if (!Number.isFinite(metrics[key]) || metrics[key] < 1) {
    throw new Error(`${key}=${metrics[key]}`);
  }
}
NODE
pass "provider_configs.metrics" "resource counters include configured provider declarations"

cleanup_provider_configs
trap - EXIT

post_auth_list="$ARTIFACT_DIR/provider-configs-auth-client-post-delete.json"
supadupa_cli_authed auth-clients list --ref "$SUPADUPA_TEST_REF" >"$post_auth_list" 2>"$ARTIFACT_DIR/provider-configs-auth-client-post-delete.stderr" ||
  fail "provider_configs.cleanup.auth_clients" "post-delete list failed"
if grep -Fq "$auth_client_id" "$post_auth_list"; then
  fail "provider_configs.cleanup.auth_clients" "$auth_client_id remained after cleanup"
fi

post_replication_list="$ARTIFACT_DIR/provider-configs-replication-post-delete.json"
supadupa_cli_authed replication list --ref "$SUPADUPA_TEST_REF" >"$post_replication_list" 2>"$ARTIFACT_DIR/provider-configs-replication-post-delete.stderr" ||
  fail "provider_configs.cleanup.replication" "post-delete list failed"
json_array_missing_name "$post_replication_list" "compat-repl-${suffix}"

post_embedding_list="$ARTIFACT_DIR/provider-configs-embedding-post-delete.json"
supadupa_cli_authed embeddings list --ref "$SUPADUPA_TEST_REF" >"$post_embedding_list" 2>"$ARTIFACT_DIR/provider-configs-embedding-post-delete.stderr" ||
  fail "provider_configs.cleanup.embeddings" "post-delete list failed"
json_array_missing_name "$post_embedding_list" "compat-embed-${suffix}"

post_vector_list="$ARTIFACT_DIR/provider-configs-vector-post-delete.json"
supadupa_cli_authed vector-buckets list --ref "$SUPADUPA_TEST_REF" >"$post_vector_list" 2>"$ARTIFACT_DIR/provider-configs-vector-post-delete.stderr" ||
  fail "provider_configs.cleanup.vector_buckets" "post-delete list failed"
json_array_missing_name "$post_vector_list" "$vector_name"

if [[ -s "$log_drain_id_file" ]]; then
  post_log_drain_list="$ARTIFACT_DIR/provider-configs-log-drain-post-delete.json"
  supadupa_cli_authed log-drains list --ref "$SUPADUPA_TEST_REF" >"$post_log_drain_list" 2>"$ARTIFACT_DIR/provider-configs-log-drain-post-delete.stderr" ||
    fail "provider_configs.cleanup.log_drains" "post-delete list failed"
  if grep -Fq "$(cat "$log_drain_id_file")" "$post_log_drain_list"; then
    fail "provider_configs.cleanup.log_drains" "log drain remained after cleanup"
  fi
fi

if [[ -s "$network_id_file" ]]; then
  post_network_list="$ARTIFACT_DIR/provider-configs-network-post-delete.json"
  supadupa_cli_authed network-connections list --ref "$SUPADUPA_TEST_REF" >"$post_network_list" 2>"$ARTIFACT_DIR/provider-configs-network-post-delete.stderr" ||
    fail "provider_configs.cleanup.network_connections" "post-delete list failed"
  json_array_missing_name "$post_network_list" "compat-net-${suffix}"
fi

pass "provider_configs.cleanup" "provider-backed declarations removed"
