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

api_url="$(profile_value api_url)"
s3_url="$(profile_value storage_s3_url)"
service_role="$(reveal_secret_value service_role)"
s3_access_key="$(reveal_secret_value s3_access_key)"
s3_secret_key="$(reveal_secret_value s3_secret_key)"

if [[ -z "$s3_access_key" || -z "$s3_secret_key" ]]; then
  skip "storage.s3.credentials" "S3 secret material is not available"
  exit 0
fi

run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
bucket_suffix="$(printf '%s' "$run_id" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9-' | cut -c1-40)"
bucket_id="compat-s3-${bucket_suffix:-bucket}"
object_key="probe.txt"
object_body="supadupa s3 compat $run_id"$'\n'

cleanup_s3_bucket() {
  curl -sS -o "$ARTIFACT_DIR/storage-s3-clean-object.body" \
    -H "Content-Type: application/json" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X DELETE "$api_url/storage/v1/object/$bucket_id" \
    --data "{\"prefixes\":[\"$object_key\",\"copy-$object_key\"]}" \
    2>"$ARTIFACT_DIR/storage-s3-clean-object.stderr" || true
  curl -sS -o "$ARTIFACT_DIR/storage-s3-clean-bucket.body" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X DELETE "$api_url/storage/v1/bucket/$bucket_id" \
    2>"$ARTIFACT_DIR/storage-s3-clean-bucket.stderr" || true
}
trap cleanup_s3_bucket EXIT

create_body="$ARTIFACT_DIR/storage-s3-bucket-create.body"
create_err="$ARTIFACT_DIR/storage-s3-bucket-create.stderr"
set +e
create_status="$(curl -sS -o "$create_body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/bucket" \
  --data "{\"id\":\"$bucket_id\",\"name\":\"$bucket_id\",\"public\":false}" \
  2>"$create_err")"
create_rc="$?"
set -e
if [[ "$create_rc" -ne 0 ]]; then
  create_status="000"
fi
case "$create_status" in
  2??) pass "storage.s3.bucket_create" "HTTP $create_status bucket=$bucket_id" ;;
  *) fail "storage.s3.bucket_create" "expected 2xx, got HTTP $create_status" ;;
esac

if SUPABASE_S3_ENDPOINT="$s3_url" \
  SUPABASE_S3_ACCESS_KEY="$s3_access_key" \
  SUPABASE_S3_SECRET_KEY="$s3_secret_key" \
  SUPADUPA_S3_BUCKET="$bucket_id" \
  SUPADUPA_S3_OBJECT_KEY="$object_key" \
  SUPADUPA_S3_OBJECT_BODY="$object_body" \
  node "$SCRIPT_DIR/s3-compat-probe.mjs" \
  >"$ARTIFACT_DIR/storage-s3-probe.out" 2>"$ARTIFACT_DIR/storage-s3-probe.stderr"; then
  pass "storage.s3.client" "list, put, head, metadata, range, presigned get, copy, delete"
else
  fail "storage.s3.client" "S3 client probe failed; see storage-s3-probe.stderr"
fi
