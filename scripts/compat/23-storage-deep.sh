#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_API_URL SUPADUPA_TEST_REF
require_tool curl
require_tool node
require_tool psql
ensure_token
ensure_profile

api_url="$(profile_value api_url)"
s3_url="$(profile_value storage_s3_url)"
control_token="$(read_secret_file "$ARTIFACT_DIR/token")"
public_db_url="$(profile_value_optional public_database_url)"
if [[ -z "$public_db_url" ]]; then
  fail "storage_deep.public_db_url" "profile did not include public_database_url"
fi
public_db_safe_url="$(url_without_password "$public_db_url")"
db_password="$(reveal_secret_value db_password)"
anon_key="$(reveal_secret_value anon_key)"
service_role="$(reveal_secret_value service_role)"
run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
bucket_suffix="$(printf '%s' "$run_id" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9-' | cut -c1-36)"
private_bucket="compat-deep-${bucket_suffix:-private}"
public_bucket="compat-pub-${bucket_suffix:-public}"
image_bucket="compat-img-${bucket_suffix:-image}"
cdn_bucket="compat-cdn-${bucket_suffix:-cdn}"
user_email="compat-storage-${bucket_suffix:-user}@example.test"
user_password="CompatStorage2026-${bucket_suffix:-password}!"
other_user_email="compat-storage-other-${bucket_suffix:-user}@example.test"
other_user_password="CompatStorageOther2026-${bucket_suffix:-password}!"
user_id=""
other_user_id=""
cdn_original_policy_file="$ARTIFACT_DIR/storage-deep-cdn-original.json"
cdn_policy_modified="false"

cleanup_storage_deep() {
  if [[ -n "${db_password:-}" && -n "${public_db_safe_url:-}" ]]; then
    PGPASSWORD="$db_password" psql "$public_db_safe_url" \
      -v ON_ERROR_STOP=1 \
      -q >"$ARTIFACT_DIR/storage-deep-policy-cleanup.out" 2>"$ARTIFACT_DIR/storage-deep-policy-cleanup.stderr" <<'SQL' || true
drop policy if exists compat_storage_deep_owner_select on storage.objects;
drop policy if exists compat_storage_deep_owner_insert on storage.objects;
SQL
  fi
  curl -sS -o "$ARTIFACT_DIR/storage-deep-private-objects-delete.body" \
    -H "Content-Type: application/json" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X DELETE "$api_url/storage/v1/object/$private_bucket" \
    --data '{"prefixes":["source.txt","copied.txt","moved.txt","signed.txt","tus.txt","user-owned.txt","other-owned.txt","s3-user-owned.txt","s3-other-owned.txt","cache.txt"]}' \
    2>"$ARTIFACT_DIR/storage-deep-private-objects-delete.stderr" || true
  curl -sS -o "$ARTIFACT_DIR/storage-deep-public-objects-delete.body" \
    -H "Content-Type: application/json" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X DELETE "$api_url/storage/v1/object/$public_bucket" \
    --data '{"prefixes":["public.txt"]}' \
    2>"$ARTIFACT_DIR/storage-deep-public-objects-delete.stderr" || true
  curl -sS -o "$ARTIFACT_DIR/storage-deep-image-objects-delete.body" \
    -H "Content-Type: application/json" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X DELETE "$api_url/storage/v1/object/$image_bucket" \
    --data '{"prefixes":["image.png"]}' \
    2>"$ARTIFACT_DIR/storage-deep-image-objects-delete.stderr" || true
  curl -sS -o "$ARTIFACT_DIR/storage-deep-private-bucket-delete.body" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X DELETE "$api_url/storage/v1/bucket/$private_bucket" \
    2>"$ARTIFACT_DIR/storage-deep-private-bucket-delete.stderr" || true
  curl -sS -o "$ARTIFACT_DIR/storage-deep-public-bucket-delete.body" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X DELETE "$api_url/storage/v1/bucket/$public_bucket" \
    2>"$ARTIFACT_DIR/storage-deep-public-bucket-delete.stderr" || true
  curl -sS -o "$ARTIFACT_DIR/storage-deep-image-bucket-delete.body" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X DELETE "$api_url/storage/v1/bucket/$image_bucket" \
    2>"$ARTIFACT_DIR/storage-deep-image-bucket-delete.stderr" || true
  curl -sS -o "$ARTIFACT_DIR/storage-deep-cdn-bucket-delete.body" \
    -H "Authorization: Bearer $control_token" \
    -X DELETE "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/storage/buckets/$cdn_bucket" \
    2>"$ARTIFACT_DIR/storage-deep-cdn-bucket-delete.stderr" || true
  if [[ "$cdn_policy_modified" == "true" && -f "$cdn_original_policy_file" ]]; then
    node - "$cdn_original_policy_file" "$ARTIFACT_DIR/storage-deep-cdn-restore-payload.json" <<'NODE' || true
const fs = require("fs");
const source = process.argv[2];
const target = process.argv[3];
const policy = JSON.parse(fs.readFileSync(source, "utf8"));
fs.writeFileSync(target, JSON.stringify({
  enabled: Boolean(policy.enabled),
  browser_ttl_seconds: Number(policy.browser_ttl_seconds || 0),
  edge_ttl_seconds: Number(policy.edge_ttl_seconds || 0),
  stale_while_revalidate_seconds: Number(policy.stale_while_revalidate_seconds || 0),
  included_paths: Array.isArray(policy.included_paths) ? policy.included_paths : [],
  excluded_paths: Array.isArray(policy.excluded_paths) ? policy.excluded_paths : [],
  smart_revalidation: Boolean(policy.smart_revalidation),
  cache_control: String(policy.cache_control || ""),
}));
NODE
    if [[ -s "$ARTIFACT_DIR/storage-deep-cdn-restore-payload.json" ]]; then
      curl -sS -o "$ARTIFACT_DIR/storage-deep-cdn-restore.body" \
        -H "Authorization: Bearer $control_token" \
        -H "Content-Type: application/json" \
        -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/cdn/policy" \
        --data-binary "@$ARTIFACT_DIR/storage-deep-cdn-restore-payload.json" \
        2>"$ARTIFACT_DIR/storage-deep-cdn-restore.stderr" || true
    fi
  fi
  if [[ -n "${user_id:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/storage-deep-user-delete.body" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/auth/v1/admin/users/$user_id" \
      2>"$ARTIFACT_DIR/storage-deep-user-delete.stderr" || true
  fi
  if [[ -n "${other_user_id:-}" ]]; then
    curl -sS -o "$ARTIFACT_DIR/storage-deep-other-user-delete.body" \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      -X DELETE "$api_url/auth/v1/admin/users/$other_user_id" \
      2>"$ARTIFACT_DIR/storage-deep-other-user-delete.stderr" || true
  fi
}
trap cleanup_storage_deep EXIT

create_bucket() {
  local name="$1"
  local is_public="$2"
  local allowed_mime_types="${3:-[\"text/plain\"]}"
  local out="$ARTIFACT_DIR/storage-deep-bucket-$name-create.body"
  local err="$ARTIFACT_DIR/storage-deep-bucket-$name-create.stderr"
  local status
  set +e
  status="$(curl -sS -o "$out" -w '%{http_code}' \
    -H "Content-Type: application/json" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X POST "$api_url/storage/v1/bucket" \
    --data "{\"id\":\"$name\",\"name\":\"$name\",\"public\":$is_public,\"file_size_limit\":10485760,\"allowed_mime_types\":$allowed_mime_types}" \
    2>"$err")"
  local rc="$?"
  set -e
  if [[ "$rc" -ne 0 ]]; then
    status="000"
  fi
  case "$status" in
    2??) pass "storage_deep.bucket_$name" "HTTP $status" ;;
    *) fail "storage_deep.bucket_$name" "expected bucket create 2xx, got HTTP $status" ;;
  esac
}

create_bucket "$private_bucket" false
create_bucket "$public_bucket" true
create_bucket "$image_bucket" true '["image/png"]'

source_body="$ARTIFACT_DIR/storage-deep-source.txt"
public_body="$ARTIFACT_DIR/storage-deep-public.txt"
cache_body="$ARTIFACT_DIR/storage-deep-cache.txt"
signed_body="$ARTIFACT_DIR/storage-deep-signed.txt"
tus_body="$ARTIFACT_DIR/storage-deep-tus.txt"
user_body="$ARTIFACT_DIR/storage-deep-user-owned.txt"
other_user_body="$ARTIFACT_DIR/storage-deep-other-owned.txt"
image_body="$ARTIFACT_DIR/storage-deep-image.png"
printf 'storage deep source %s\n' "$run_id" >"$source_body"
printf 'storage deep public %s\n' "$run_id" >"$public_body"
printf 'storage deep cache %s\n' "$run_id" >"$cache_body"
printf 'storage deep signed %s\n' "$run_id" >"$signed_body"
printf 'storage deep tus %s\n' "$run_id" >"$tus_body"
printf 'storage deep user %s\n' "$run_id" >"$user_body"
printf 'storage deep other user %s\n' "$run_id" >"$other_user_body"
if node - "$image_body" >"$ARTIFACT_DIR/storage-deep-image-generate.out" 2>"$ARTIFACT_DIR/storage-deep-image-generate.stderr" <<'NODE'
const fs = require("fs");
const zlib = require("zlib");
const out = process.argv[2];

function crc32(buf) {
  let crc = 0xffffffff;
  for (const byte of buf) {
    crc ^= byte;
    for (let i = 0; i < 8; i += 1) {
      crc = (crc >>> 1) ^ (0xedb88320 & -(crc & 1));
    }
  }
  return (crc ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
  const name = Buffer.from(type, "ascii");
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length, 0);
  const checksum = Buffer.alloc(4);
  checksum.writeUInt32BE(crc32(Buffer.concat([name, data])), 0);
  return Buffer.concat([length, name, data, checksum]);
}

const width = 16;
const height = 8;
const ihdr = Buffer.alloc(13);
ihdr.writeUInt32BE(width, 0);
ihdr.writeUInt32BE(height, 4);
ihdr[8] = 8;
ihdr[9] = 2;
const rows = [];
for (let y = 0; y < height; y += 1) {
  const row = Buffer.alloc(1 + width * 3);
  row[0] = 0;
  for (let x = 0; x < width; x += 1) {
    const offset = 1 + x * 3;
    row[offset] = (x * 16) & 0xff;
    row[offset + 1] = (y * 32) & 0xff;
    row[offset + 2] = 180;
  }
  rows.push(row);
}
const png = Buffer.concat([
  Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
  chunk("IHDR", ihdr),
  chunk("IDAT", zlib.deflateSync(Buffer.concat(rows))),
  chunk("IEND", Buffer.alloc(0)),
]);
fs.writeFileSync(out, png);
NODE
then
  pass "storage_deep.image_generate" "generated PNG fixture"
else
  fail "storage_deep.image_generate" "PNG fixture generation failed; see storage-deep-image-generate.stderr"
fi

set +e
upload_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-source-upload.body" -w '%{http_code}' \
  -H "Content-Type: text/plain" \
  -H "x-upsert: true" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/object/$private_bucket/source.txt" \
  --data-binary @"$source_body" \
  2>"$ARTIFACT_DIR/storage-deep-source-upload.stderr")"
upload_rc="$?"
set -e
if [[ "$upload_rc" -ne 0 ]]; then upload_status="000"; fi
case "$upload_status" in
  2??) pass "storage_deep.service_upload" "HTTP $upload_status" ;;
  *) fail "storage_deep.service_upload" "expected 2xx, got HTTP $upload_status" ;;
esac

set +e
cache_upload_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-cache-upload.body" -w '%{http_code}' \
  -H "Content-Type: text/plain" \
  -H "Cache-Control: public, max-age=60" \
  -H "x-upsert: true" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/object/$private_bucket/cache.txt" \
  --data-binary @"$cache_body" \
  2>"$ARTIFACT_DIR/storage-deep-cache-upload.stderr")"
cache_upload_rc="$?"
set -e
if [[ "$cache_upload_rc" -ne 0 ]]; then cache_upload_status="000"; fi
case "$cache_upload_status" in
  2??) pass "storage_deep.cache_control_upload" "HTTP $cache_upload_status" ;;
  *) fail "storage_deep.cache_control_upload" "expected 2xx, got HTTP $cache_upload_status" ;;
esac

set +e
list_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-list.body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/object/list/$private_bucket" \
  --data '{"prefix":"","limit":20,"offset":0,"sortBy":{"column":"name","order":"asc"},"search":"source"}' \
  2>"$ARTIFACT_DIR/storage-deep-list.stderr")"
list_rc="$?"
set -e
if [[ "$list_rc" -ne 0 ]]; then list_status="000"; fi
case "$list_status" in
  2??)
    if ! grep -q '"name":"source.txt"' "$ARTIFACT_DIR/storage-deep-list.body"; then
      fail "storage_deep.list_search" "list/search did not include source.txt"
    fi
    pass "storage_deep.list_search" "HTTP $list_status"
    ;;
  *) fail "storage_deep.list_search" "expected 2xx, got HTTP $list_status" ;;
esac

set +e
copy_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-copy.body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/object/copy" \
  --data "{\"bucketId\":\"$private_bucket\",\"sourceKey\":\"source.txt\",\"destinationKey\":\"copied.txt\"}" \
  2>"$ARTIFACT_DIR/storage-deep-copy.stderr")"
copy_rc="$?"
set -e
if [[ "$copy_rc" -ne 0 ]]; then copy_status="000"; fi
case "$copy_status" in
  2??) pass "storage_deep.copy" "HTTP $copy_status" ;;
  *) fail "storage_deep.copy" "expected 2xx, got HTTP $copy_status" ;;
esac

set +e
move_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-move.body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/object/move" \
  --data "{\"bucketId\":\"$private_bucket\",\"sourceKey\":\"copied.txt\",\"destinationKey\":\"moved.txt\"}" \
  2>"$ARTIFACT_DIR/storage-deep-move.stderr")"
move_rc="$?"
set -e
if [[ "$move_rc" -ne 0 ]]; then move_status="000"; fi
case "$move_status" in
  2??) pass "storage_deep.move" "HTTP $move_status" ;;
  *) fail "storage_deep.move" "expected 2xx, got HTTP $move_status" ;;
esac

set +e
moved_download_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-moved-download.body" -w '%{http_code}' \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  "$api_url/storage/v1/object/$private_bucket/moved.txt" \
  2>"$ARTIFACT_DIR/storage-deep-moved-download.stderr")"
moved_download_rc="$?"
set -e
if [[ "$moved_download_rc" -ne 0 ]]; then moved_download_status="000"; fi
case "$moved_download_status" in
  2??)
    if ! cmp -s "$source_body" "$ARTIFACT_DIR/storage-deep-moved-download.body"; then
      fail "storage_deep.move_download" "moved object body mismatch"
    fi
    pass "storage_deep.move_download" "HTTP $moved_download_status"
    ;;
  *) fail "storage_deep.move_download" "expected 2xx, got HTTP $moved_download_status" ;;
esac

set +e
signed_create_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-signed-upload-create.body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/object/upload/sign/$private_bucket/signed.txt" \
  --data '{"expiresIn":3600}' \
  2>"$ARTIFACT_DIR/storage-deep-signed-upload-create.stderr")"
signed_create_rc="$?"
set -e
if [[ "$signed_create_rc" -ne 0 ]]; then signed_create_status="000"; fi
case "$signed_create_status" in
  2??)
    signed_upload_url="$(node - "$ARTIFACT_DIR/storage-deep-signed-upload-create.body" "$api_url" <<'NODE'
const fs = require("fs");
const file = process.argv[2];
const apiURL = process.argv[3].replace(/\/$/, "");
const body = JSON.parse(fs.readFileSync(file, "utf8"));
const raw = body.url || body.signedURL || body.signedUrl || "";
if (!raw) process.exit(2);
if (/^https?:\/\//i.test(raw)) process.stdout.write(raw);
else if (raw.startsWith("/storage/v1/")) process.stdout.write(apiURL + raw);
else if (raw.startsWith("/")) process.stdout.write(apiURL + "/storage/v1" + raw);
else process.stdout.write(apiURL + "/storage/v1/" + raw);
NODE
)"
    if [[ -z "$signed_upload_url" ]]; then
      fail "storage_deep.signed_upload_create" "response did not include signed upload URL"
    fi
    pass "storage_deep.signed_upload_create" "HTTP $signed_create_status"
    ;;
  *) fail "storage_deep.signed_upload_create" "expected 2xx, got HTTP $signed_create_status" ;;
esac

set +e
signed_upload_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-signed-upload.body" -w '%{http_code}' \
  -H "Content-Type: text/plain" \
  -X PUT "$signed_upload_url" \
  --data-binary @"$signed_body" \
  2>"$ARTIFACT_DIR/storage-deep-signed-upload.stderr")"
signed_upload_rc="$?"
set -e
if [[ "$signed_upload_rc" -ne 0 ]]; then signed_upload_status="000"; fi
case "$signed_upload_status" in
  2??) pass "storage_deep.signed_upload" "HTTP $signed_upload_status" ;;
  *) fail "storage_deep.signed_upload" "expected 2xx, got HTTP $signed_upload_status" ;;
esac

set +e
signed_download_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-signed-download.body" -w '%{http_code}' \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  "$api_url/storage/v1/object/$private_bucket/signed.txt" \
  2>"$ARTIFACT_DIR/storage-deep-signed-download.stderr")"
signed_download_rc="$?"
set -e
if [[ "$signed_download_rc" -ne 0 ]]; then signed_download_status="000"; fi
case "$signed_download_status" in
  2??)
    if ! cmp -s "$signed_body" "$ARTIFACT_DIR/storage-deep-signed-download.body"; then
      fail "storage_deep.signed_upload_verify" "signed upload body mismatch"
    fi
    pass "storage_deep.signed_upload_verify" "HTTP $signed_download_status"
    ;;
  *) fail "storage_deep.signed_upload_verify" "expected 2xx, got HTTP $signed_download_status" ;;
esac

tus_metadata="$(node - "$private_bucket" "tus.txt" <<'NODE'
const encode = (value) => Buffer.from(value).toString("base64");
const entries = [
  ["bucketName", process.argv[2]],
  ["objectName", process.argv[3]],
  ["contentType", "text/plain"],
  ["cacheControl", "3600"],
];
process.stdout.write(entries.map(([key, value]) => `${key} ${encode(value)}`).join(","));
NODE
)"
tus_length="$(wc -c <"$tus_body" | tr -d ' ')"
set +e
tus_create_status="$(curl -sS -D "$ARTIFACT_DIR/storage-deep-tus-create.headers" -o "$ARTIFACT_DIR/storage-deep-tus-create.body" -w '%{http_code}' \
  -H "Tus-Resumable: 1.0.0" \
  -H "Upload-Length: $tus_length" \
  -H "Upload-Metadata: $tus_metadata" \
  -H "x-upsert: true" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/upload/resumable" \
  2>"$ARTIFACT_DIR/storage-deep-tus-create.stderr")"
tus_create_rc="$?"
set -e
if [[ "$tus_create_rc" -ne 0 ]]; then tus_create_status="000"; fi
case "$tus_create_status" in
  2??)
    tus_upload_url="$(awk 'tolower($1)=="location:"{print $2}' "$ARTIFACT_DIR/storage-deep-tus-create.headers" | tr -d '\r' | tail -1)"
    if [[ -z "$tus_upload_url" ]]; then
      fail "storage_deep.tus_create" "TUS create response did not include Location"
    fi
    if [[ "$tus_upload_url" != http* ]]; then
      tus_upload_url="${api_url%/}$tus_upload_url"
    fi
    pass "storage_deep.tus_create" "HTTP $tus_create_status"
    ;;
  *) fail "storage_deep.tus_create" "expected 2xx, got HTTP $tus_create_status" ;;
esac

set +e
tus_patch_status="$(curl -sS -D "$ARTIFACT_DIR/storage-deep-tus-patch.headers" -o "$ARTIFACT_DIR/storage-deep-tus-patch.body" -w '%{http_code}' \
  -H "Tus-Resumable: 1.0.0" \
  -H "Upload-Offset: 0" \
  -H "Content-Type: application/offset+octet-stream" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X PATCH "$tus_upload_url" \
  --data-binary @"$tus_body" \
  2>"$ARTIFACT_DIR/storage-deep-tus-patch.stderr")"
tus_patch_rc="$?"
set -e
if [[ "$tus_patch_rc" -ne 0 ]]; then tus_patch_status="000"; fi
case "$tus_patch_status" in
  2??) pass "storage_deep.tus_patch" "HTTP $tus_patch_status" ;;
  *) fail "storage_deep.tus_patch" "expected 2xx, got HTTP $tus_patch_status" ;;
esac

set +e
tus_download_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-tus-download.body" -w '%{http_code}' \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  "$api_url/storage/v1/object/$private_bucket/tus.txt" \
  2>"$ARTIFACT_DIR/storage-deep-tus-download.stderr")"
tus_download_rc="$?"
set -e
if [[ "$tus_download_rc" -ne 0 ]]; then tus_download_status="000"; fi
case "$tus_download_status" in
  2??)
    if ! cmp -s "$tus_body" "$ARTIFACT_DIR/storage-deep-tus-download.body"; then
      fail "storage_deep.tus_verify" "TUS object body mismatch"
    fi
    pass "storage_deep.tus_verify" "HTTP $tus_download_status"
    ;;
  *) fail "storage_deep.tus_verify" "expected 2xx, got HTTP $tus_download_status" ;;
esac

set +e
public_upload_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-public-upload.body" -w '%{http_code}' \
  -H "Content-Type: text/plain" \
  -H "x-upsert: true" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/object/$public_bucket/public.txt" \
  --data-binary @"$public_body" \
  2>"$ARTIFACT_DIR/storage-deep-public-upload.stderr")"
public_upload_rc="$?"
set -e
if [[ "$public_upload_rc" -ne 0 ]]; then public_upload_status="000"; fi
case "$public_upload_status" in
  2??) pass "storage_deep.public_upload" "HTTP $public_upload_status" ;;
  *) fail "storage_deep.public_upload" "expected 2xx, got HTTP $public_upload_status" ;;
esac

set +e
public_download_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-public-download.body" -w '%{http_code}' \
  "$api_url/storage/v1/object/public/$public_bucket/public.txt" \
  2>"$ARTIFACT_DIR/storage-deep-public-download.stderr")"
public_download_rc="$?"
set -e
if [[ "$public_download_rc" -ne 0 ]]; then public_download_status="000"; fi
case "$public_download_status" in
  2??)
    if ! cmp -s "$public_body" "$ARTIFACT_DIR/storage-deep-public-download.body"; then
      fail "storage_deep.public_download" "public object body mismatch"
    fi
    pass "storage_deep.public_download" "HTTP $public_download_status"
    ;;
  *) fail "storage_deep.public_download" "expected 2xx, got HTTP $public_download_status" ;;
esac

set +e
image_upload_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-image-upload.body" -w '%{http_code}' \
  -H "Content-Type: image/png" \
  -H "x-upsert: true" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/object/$image_bucket/image.png" \
  --data-binary @"$image_body" \
  2>"$ARTIFACT_DIR/storage-deep-image-upload.stderr")"
image_upload_rc="$?"
set -e
if [[ "$image_upload_rc" -ne 0 ]]; then image_upload_status="000"; fi
case "$image_upload_status" in
  2??) pass "storage_deep.image_upload" "HTTP $image_upload_status" ;;
  *) fail "storage_deep.image_upload" "expected 2xx, got HTTP $image_upload_status" ;;
esac

set +e
image_transform_status="$(curl -sS -D "$ARTIFACT_DIR/storage-deep-image-transform.headers" -o "$ARTIFACT_DIR/storage-deep-image-transform.body" -w '%{http_code}' \
  -H "Accept: image/png" \
  "$api_url/storage/v1/render/image/public/$image_bucket/image.png?width=4&height=4&resize=fill&quality=80" \
  2>"$ARTIFACT_DIR/storage-deep-image-transform.stderr")"
image_transform_rc="$?"
set -e
if [[ "$image_transform_rc" -ne 0 ]]; then image_transform_status="000"; fi
case "$image_transform_status" in
  2??)
    if ! awk 'BEGIN{ok=0} tolower($1)=="content-type:" && tolower($2) ~ /^image\// {ok=1} END{exit ok ? 0 : 1}' "$ARTIFACT_DIR/storage-deep-image-transform.headers"; then
      fail "storage_deep.image_transform_content_type" "transformed response was not image/*"
    fi
    if node - "$ARTIFACT_DIR/storage-deep-image-transform.body" 4 4 >"$ARTIFACT_DIR/storage-deep-image-transform-verify.out" 2>"$ARTIFACT_DIR/storage-deep-image-transform-verify.stderr" <<'NODE'
const fs = require("fs");
const file = process.argv[2];
const expectedWidth = Number(process.argv[3]);
const expectedHeight = Number(process.argv[4]);
const buf = fs.readFileSync(file);
const signature = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
if (buf.length < 33 || !buf.subarray(0, 8).equals(signature)) {
  throw new Error("response is not a PNG");
}
const chunkType = buf.subarray(12, 16).toString("ascii");
if (chunkType !== "IHDR") {
  throw new Error(`first chunk is ${chunkType}, expected IHDR`);
}
const width = buf.readUInt32BE(16);
const height = buf.readUInt32BE(20);
if (width !== expectedWidth || height !== expectedHeight) {
  throw new Error(`expected ${expectedWidth}x${expectedHeight}, got ${width}x${height}`);
}
console.log(JSON.stringify({ width, height }));
NODE
    then
      pass "storage_deep.image_transform" "public render resized image to 4x4"
    else
      fail "storage_deep.image_transform" "image transform verification failed; see storage-deep-image-transform-verify.stderr"
    fi
    ;;
  *) fail "storage_deep.image_transform" "expected public render 2xx, got HTTP $image_transform_status" ;;
esac

set +e
anon_private_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-anon-private-download.body" -w '%{http_code}' \
  -H "apikey: $anon_key" \
  "$api_url/storage/v1/object/$private_bucket/source.txt" \
  2>"$ARTIFACT_DIR/storage-deep-anon-private-download.stderr")"
anon_private_rc="$?"
set -e
if [[ "$anon_private_rc" -ne 0 ]]; then anon_private_status="000"; fi
case "$anon_private_status" in
  401|403|404|400) pass "storage_deep.private_anon_rejected" "HTTP $anon_private_status" ;;
  *) fail "storage_deep.private_anon_rejected" "expected private object rejection, got HTTP $anon_private_status" ;;
esac

user_create_body="$ARTIFACT_DIR/storage-deep-user-create.body"
set +e
user_create_status="$(curl -sS -o "$user_create_body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/auth/v1/admin/users" \
  --data "{\"email\":\"$user_email\",\"password\":\"$user_password\",\"email_confirm\":true}" \
  2>"$ARTIFACT_DIR/storage-deep-user-create.stderr")"
user_create_rc="$?"
set -e
if [[ "$user_create_rc" -ne 0 ]]; then user_create_status="000"; fi
case "$user_create_status" in
  2??)
    user_id="$(json_get_file "$user_create_body" id)"
    pass "storage_deep.user_create" "HTTP $user_create_status"
    ;;
  *) fail "storage_deep.user_create" "expected 2xx, got HTTP $user_create_status" ;;
esac

token_body="$ARTIFACT_DIR/storage-deep-user-token.body"
set +e
token_status="$(curl -sS -o "$token_body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $anon_key" \
  -X POST "$api_url/auth/v1/token?grant_type=password" \
  --data "{\"email\":\"$user_email\",\"password\":\"$user_password\"}" \
  2>"$ARTIFACT_DIR/storage-deep-user-token.stderr")"
token_rc="$?"
set -e
if [[ "$token_rc" -ne 0 ]]; then token_status="000"; fi
case "$token_status" in
  2??)
    user_access_token="$(json_get_file "$token_body" access_token)"
    pass "storage_deep.user_password_grant" "HTTP $token_status"
    ;;
  *) fail "storage_deep.user_password_grant" "expected 2xx, got HTTP $token_status" ;;
esac

other_user_create_body="$ARTIFACT_DIR/storage-deep-other-user-create.body"
set +e
other_user_create_status="$(curl -sS -o "$other_user_create_body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/auth/v1/admin/users" \
  --data "{\"email\":\"$other_user_email\",\"password\":\"$other_user_password\",\"email_confirm\":true}" \
  2>"$ARTIFACT_DIR/storage-deep-other-user-create.stderr")"
other_user_create_rc="$?"
set -e
if [[ "$other_user_create_rc" -ne 0 ]]; then other_user_create_status="000"; fi
case "$other_user_create_status" in
  2??)
    other_user_id="$(json_get_file "$other_user_create_body" id)"
    pass "storage_deep.other_user_create" "HTTP $other_user_create_status"
    ;;
  *) fail "storage_deep.other_user_create" "expected 2xx, got HTTP $other_user_create_status" ;;
esac

other_token_body="$ARTIFACT_DIR/storage-deep-other-user-token.body"
set +e
other_token_status="$(curl -sS -o "$other_token_body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $anon_key" \
  -X POST "$api_url/auth/v1/token?grant_type=password" \
  --data "{\"email\":\"$other_user_email\",\"password\":\"$other_user_password\"}" \
  2>"$ARTIFACT_DIR/storage-deep-other-user-token.stderr")"
other_token_rc="$?"
set -e
if [[ "$other_token_rc" -ne 0 ]]; then other_token_status="000"; fi
case "$other_token_status" in
  2??)
    other_user_access_token="$(json_get_file "$other_token_body" access_token)"
    pass "storage_deep.other_user_password_grant" "HTTP $other_token_status"
    ;;
  *) fail "storage_deep.other_user_password_grant" "expected 2xx, got HTTP $other_token_status" ;;
esac

if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
  -v ON_ERROR_STOP=1 \
  -v bucket="$private_bucket" \
  -q >"$ARTIFACT_DIR/storage-deep-policy-setup.out" 2>"$ARTIFACT_DIR/storage-deep-policy-setup.stderr" <<'SQL'
drop policy if exists compat_storage_deep_owner_select on storage.objects;
drop policy if exists compat_storage_deep_owner_insert on storage.objects;
create policy compat_storage_deep_owner_select
  on storage.objects
  for select
  to authenticated
  using (bucket_id = :'bucket' and owner = auth.uid());
create policy compat_storage_deep_owner_insert
  on storage.objects
  for insert
  to authenticated
  with check (bucket_id = :'bucket' and owner = auth.uid());
SQL
then
  pass "storage_deep.rls_policy_setup" "owner policies installed"
else
  fail "storage_deep.rls_policy_setup" "storage RLS policy setup failed; see storage-deep-policy-setup.stderr"
fi

set +e
user_upload_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-user-upload.body" -w '%{http_code}' \
  -H "Content-Type: text/plain" \
  -H "x-upsert: true" \
  -H "apikey: $anon_key" \
  -H "Authorization: Bearer $user_access_token" \
  -X POST "$api_url/storage/v1/object/$private_bucket/user-owned.txt" \
  --data-binary @"$user_body" \
  2>"$ARTIFACT_DIR/storage-deep-user-upload.stderr")"
user_upload_rc="$?"
set -e
if [[ "$user_upload_rc" -ne 0 ]]; then user_upload_status="000"; fi
case "$user_upload_status" in
  2??) pass "storage_deep.user_jwt_upload" "HTTP $user_upload_status" ;;
  *) fail "storage_deep.user_jwt_upload" "expected 2xx, got HTTP $user_upload_status" ;;
esac

set +e
user_download_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-user-download.body" -w '%{http_code}' \
  -H "apikey: $anon_key" \
  -H "Authorization: Bearer $user_access_token" \
  "$api_url/storage/v1/object/$private_bucket/user-owned.txt" \
  2>"$ARTIFACT_DIR/storage-deep-user-download.stderr")"
user_download_rc="$?"
set -e
if [[ "$user_download_rc" -ne 0 ]]; then user_download_status="000"; fi
case "$user_download_status" in
  2??)
    if ! cmp -s "$user_body" "$ARTIFACT_DIR/storage-deep-user-download.body"; then
      fail "storage_deep.user_jwt_download" "user-owned object body mismatch"
    fi
    pass "storage_deep.user_jwt_download" "HTTP $user_download_status"
    ;;
  *) fail "storage_deep.user_jwt_download" "expected 2xx, got HTTP $user_download_status" ;;
esac

if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
  -v ON_ERROR_STOP=1 \
  -v bucket="$private_bucket" \
  -v user_id="$user_id" \
  -Atq >"$ARTIFACT_DIR/storage-deep-owner-verify.out" 2>"$ARTIFACT_DIR/storage-deep-owner-verify.stderr" <<'SQL'
select count(*)
from storage.objects
where bucket_id = :'bucket'
  and name = 'user-owned.txt'
  and owner = :'user_id'::uuid
  and owner_id = :'user_id';
SQL
then
  owner_count="$(tr -d '\r\n' <"$ARTIFACT_DIR/storage-deep-owner-verify.out")"
  if [[ "$owner_count" != "1" ]]; then
    fail "storage_deep.user_jwt_owner" "expected user-owned object owner metadata, got $owner_count"
  fi
  pass "storage_deep.user_jwt_owner" "owner metadata matches auth.uid"
else
  fail "storage_deep.user_jwt_owner" "owner verification failed; see storage-deep-owner-verify.stderr"
fi

set +e
other_user_download_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-other-user-download-user-owned.body" -w '%{http_code}' \
  -H "apikey: $anon_key" \
  -H "Authorization: Bearer $other_user_access_token" \
  "$api_url/storage/v1/object/$private_bucket/user-owned.txt" \
  2>"$ARTIFACT_DIR/storage-deep-other-user-download-user-owned.stderr")"
other_user_download_rc="$?"
set -e
if [[ "$other_user_download_rc" -ne 0 ]]; then other_user_download_status="000"; fi
case "$other_user_download_status" in
  400|401|403|404) pass "storage_deep.rls_other_user_rejected" "HTTP $other_user_download_status" ;;
  *) fail "storage_deep.rls_other_user_rejected" "expected other user to be rejected from first user's object, got HTTP $other_user_download_status" ;;
esac

set +e
other_user_upload_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-other-user-upload.body" -w '%{http_code}' \
  -H "Content-Type: text/plain" \
  -H "x-upsert: true" \
  -H "apikey: $anon_key" \
  -H "Authorization: Bearer $other_user_access_token" \
  -X POST "$api_url/storage/v1/object/$private_bucket/other-owned.txt" \
  --data-binary @"$other_user_body" \
  2>"$ARTIFACT_DIR/storage-deep-other-user-upload.stderr")"
other_user_upload_rc="$?"
set -e
if [[ "$other_user_upload_rc" -ne 0 ]]; then other_user_upload_status="000"; fi
case "$other_user_upload_status" in
  2??) pass "storage_deep.other_user_jwt_upload" "HTTP $other_user_upload_status" ;;
  *) fail "storage_deep.other_user_jwt_upload" "expected other user upload 2xx, got HTTP $other_user_upload_status" ;;
esac

set +e
owner_download_other_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-user-download-other-owned.body" -w '%{http_code}' \
  -H "apikey: $anon_key" \
  -H "Authorization: Bearer $user_access_token" \
  "$api_url/storage/v1/object/$private_bucket/other-owned.txt" \
  2>"$ARTIFACT_DIR/storage-deep-user-download-other-owned.stderr")"
owner_download_other_rc="$?"
set -e
if [[ "$owner_download_other_rc" -ne 0 ]]; then owner_download_other_status="000"; fi
case "$owner_download_other_status" in
  400|401|403|404) pass "storage_deep.rls_owner_rejected_other_user_object" "HTTP $owner_download_other_status" ;;
  *) fail "storage_deep.rls_owner_rejected_other_user_object" "expected first user to be rejected from other user's object, got HTTP $owner_download_other_status" ;;
esac

set +e
other_user_self_download_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-other-user-self-download.body" -w '%{http_code}' \
  -H "apikey: $anon_key" \
  -H "Authorization: Bearer $other_user_access_token" \
  "$api_url/storage/v1/object/$private_bucket/other-owned.txt" \
  2>"$ARTIFACT_DIR/storage-deep-other-user-self-download.stderr")"
other_user_self_download_rc="$?"
set -e
if [[ "$other_user_self_download_rc" -ne 0 ]]; then other_user_self_download_status="000"; fi
case "$other_user_self_download_status" in
  2??)
    if ! cmp -s "$other_user_body" "$ARTIFACT_DIR/storage-deep-other-user-self-download.body"; then
      fail "storage_deep.other_user_jwt_download" "other user object body mismatch"
    fi
    pass "storage_deep.other_user_jwt_download" "HTTP $other_user_self_download_status"
    ;;
  *) fail "storage_deep.other_user_jwt_download" "expected other user self-download 2xx, got HTTP $other_user_self_download_status" ;;
esac

s3_user_object_key="s3-user-owned.txt"
s3_other_object_key="s3-other-owned.txt"
s3_user_body="storage deep s3 session user $run_id"$'\n'
s3_other_body="storage deep s3 session other user $run_id"$'\n'
if SUPABASE_S3_ENDPOINT="$s3_url" \
  SUPABASE_S3_ACCESS_KEY="$SUPADUPA_TEST_REF" \
  SUPABASE_S3_SECRET_KEY="$anon_key" \
  SUPABASE_S3_SESSION_TOKEN="$user_access_token" \
  SUPABASE_S3_OTHER_SESSION_TOKEN="$other_user_access_token" \
  SUPADUPA_S3_BUCKET="$private_bucket" \
  SUPADUPA_S3_USER_OBJECT_KEY="$s3_user_object_key" \
  SUPADUPA_S3_OTHER_OBJECT_KEY="$s3_other_object_key" \
  SUPADUPA_S3_USER_OBJECT_BODY="$s3_user_body" \
  SUPADUPA_S3_OTHER_OBJECT_BODY="$s3_other_body" \
  node "$SCRIPT_DIR/s3-session-token-probe.mjs" \
  >"$ARTIFACT_DIR/storage-deep-s3-session-token.out" 2>"$ARTIFACT_DIR/storage-deep-s3-session-token.stderr"; then
  pass "storage_deep.s3_session_token_rls" "user JWT S3 session token enforces owner RLS"
else
  fail "storage_deep.s3_session_token_rls" "S3 session-token RLS probe failed; see storage-deep-s3-session-token.stderr"
fi

if PGPASSWORD="$db_password" psql "$public_db_safe_url" \
  -v ON_ERROR_STOP=1 \
  -v bucket="$private_bucket" \
  -Atq >"$ARTIFACT_DIR/storage-deep-cache-verify.out" 2>"$ARTIFACT_DIR/storage-deep-cache-verify.stderr" <<'SQL'
select coalesce(metadata->>'cacheControl', '')
from storage.objects
where bucket_id = :'bucket'
  and name = 'cache.txt';
SQL
then
  cache_control="$(tr -d '\r\n' <"$ARTIFACT_DIR/storage-deep-cache-verify.out")"
  if [[ "$cache_control" != "public, max-age=60" ]]; then
    fail "storage_deep.cache_control_metadata" "expected cacheControl metadata, got $cache_control"
  fi
  pass "storage_deep.cache_control_metadata" "cacheControl persisted"
else
  fail "storage_deep.cache_control_metadata" "cache-control verification failed; see storage-deep-cache-verify.stderr"
fi

set +e
cdn_original_status="$(curl -sS -o "$cdn_original_policy_file" -w '%{http_code}' \
  -H "Authorization: Bearer $control_token" \
  "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/cdn/policy" \
  2>"$ARTIFACT_DIR/storage-deep-cdn-original.stderr")"
cdn_original_rc="$?"
set -e
if [[ "$cdn_original_rc" -ne 0 ]]; then cdn_original_status="000"; fi
case "$cdn_original_status" in
  2??) pass "storage_deep.cdn_policy_snapshot" "HTTP $cdn_original_status" ;;
  *) fail "storage_deep.cdn_policy_snapshot" "expected 2xx, got HTTP $cdn_original_status" ;;
esac

node - "$cdn_bucket" "$ARTIFACT_DIR/storage-deep-cdn-policy-payload.json" <<'NODE'
const fs = require("fs");
const bucket = process.argv[2];
const target = process.argv[3];
fs.writeFileSync(target, JSON.stringify({
  enabled: true,
  browser_ttl_seconds: 300,
  edge_ttl_seconds: 600,
  stale_while_revalidate_seconds: 30,
  included_paths: [`/storage/v1/object/public/${bucket}/*`],
  excluded_paths: [`/storage/v1/object/public/${bucket}/private/*`],
  smart_revalidation: true,
}));
NODE

set +e
cdn_update_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-cdn-policy-update.body" -w '%{http_code}' \
  -H "Authorization: Bearer $control_token" \
  -H "Content-Type: application/json" \
  -X PUT "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/cdn/policy" \
  --data-binary "@$ARTIFACT_DIR/storage-deep-cdn-policy-payload.json" \
  2>"$ARTIFACT_DIR/storage-deep-cdn-policy-update.stderr")"
cdn_update_rc="$?"
set -e
if [[ "$cdn_update_rc" -ne 0 ]]; then cdn_update_status="000"; fi
case "$cdn_update_status" in
  2??) cdn_policy_modified="true" ;;
  *) fail "storage_deep.cdn_policy_update" "expected 2xx, got HTTP $cdn_update_status" ;;
esac
if node - "$ARTIFACT_DIR/storage-deep-cdn-policy-update.body" "$cdn_bucket" <<'NODE'
const fs = require("fs");
const policy = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const bucket = process.argv[3];
if (policy.enabled !== true) throw new Error("enabled was not true");
if (policy.smart_revalidation !== true) throw new Error("smart_revalidation was not true");
if (policy.browser_ttl_seconds !== 300 || policy.edge_ttl_seconds !== 600 || policy.stale_while_revalidate_seconds !== 30) throw new Error("unexpected TTLs");
if (policy.cache_control !== "public, max-age=300, s-maxage=600, stale-while-revalidate=30") throw new Error(`unexpected cache_control ${policy.cache_control}`);
if (!Array.isArray(policy.included_paths) || !policy.included_paths.includes(`/storage/v1/object/public/${bucket}/*`)) throw new Error("included path missing");
NODE
then
  pass "storage_deep.cdn_policy_update" "cache policy persisted"
else
  fail "storage_deep.cdn_policy_update" "updated CDN policy did not match expected shape"
fi

set +e
cdn_routes_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-cdn-routes.body" -w '%{http_code}' \
  -H "Authorization: Bearer $control_token" \
  "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/routes" \
  2>"$ARTIFACT_DIR/storage-deep-cdn-routes.stderr")"
cdn_routes_rc="$?"
set -e
if [[ "$cdn_routes_rc" -ne 0 ]]; then cdn_routes_status="000"; fi
case "$cdn_routes_status" in
  2??) ;;
  *) fail "storage_deep.cdn_routes" "expected 2xx, got HTTP $cdn_routes_status" ;;
esac
if node - "$ARTIFACT_DIR/storage-deep-cdn-routes.body" <<'NODE'
const fs = require("fs");
const routes = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (!Array.isArray(routes)) throw new Error("routes response is not an array");
if (!routes.some((route) => route && route.cache_control === "public, max-age=300, s-maxage=600, stale-while-revalidate=30" && route.smart_cdn === true)) {
  throw new Error("no route reflected CDN cache headers");
}
NODE
then
  pass "storage_deep.cdn_routes" "route cache headers reflected"
else
  fail "storage_deep.cdn_routes" "routes did not expose CDN cache headers"
fi

node - "$cdn_bucket" "$ARTIFACT_DIR/storage-deep-cdn-invalidation-payload.json" <<'NODE'
const fs = require("fs");
const bucket = process.argv[2];
const target = process.argv[3];
fs.writeFileSync(target, JSON.stringify({
  paths: [`/storage/v1/object/public/${bucket}/public.txt`, `/storage/v1/object/public/${bucket}/*`],
}));
NODE
set +e
cdn_invalidation_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-cdn-invalidation.body" -w '%{http_code}' \
  -H "Authorization: Bearer $control_token" \
  -H "Content-Type: application/json" \
  -X POST "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/cdn/invalidations" \
  --data-binary "@$ARTIFACT_DIR/storage-deep-cdn-invalidation-payload.json" \
  2>"$ARTIFACT_DIR/storage-deep-cdn-invalidation.stderr")"
cdn_invalidation_rc="$?"
set -e
if [[ "$cdn_invalidation_rc" -ne 0 ]]; then cdn_invalidation_status="000"; fi
case "$cdn_invalidation_status" in
  201) ;;
  *) fail "storage_deep.cdn_manual_invalidation" "expected HTTP 201, got HTTP $cdn_invalidation_status" ;;
esac
if node - "$ARTIFACT_DIR/storage-deep-cdn-invalidation.body" "$cdn_bucket" <<'NODE'
const fs = require("fs");
const invalidation = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const bucket = process.argv[3];
if (invalidation.status !== "completed") throw new Error("invalidation not completed");
if (invalidation.source !== "manual") throw new Error(`unexpected source ${invalidation.source}`);
if (!Array.isArray(invalidation.paths) || !invalidation.paths.includes(`/storage/v1/object/public/${bucket}/public.txt`)) throw new Error("manual path missing");
if (!invalidation.completed_at) throw new Error("completed_at missing");
NODE
then
  pass "storage_deep.cdn_manual_invalidation" "manual invalidation completed"
else
  fail "storage_deep.cdn_manual_invalidation" "manual invalidation response did not match expected shape"
fi

set +e
cdn_bucket_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-cdn-bucket-create.body" -w '%{http_code}' \
  -H "Authorization: Bearer $control_token" \
  -H "Content-Type: application/json" \
  -X POST "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/storage/buckets" \
  --data "{\"name\":\"$cdn_bucket\",\"public\":true,\"file_size_limit\":1048576,\"cache_control\":\"public, max-age=300\"}" \
  2>"$ARTIFACT_DIR/storage-deep-cdn-bucket-create.stderr")"
cdn_bucket_rc="$?"
set -e
if [[ "$cdn_bucket_rc" -ne 0 ]]; then cdn_bucket_status="000"; fi
case "$cdn_bucket_status" in
  201) pass "storage_deep.cdn_management_bucket" "HTTP $cdn_bucket_status" ;;
  *) fail "storage_deep.cdn_management_bucket" "expected HTTP 201, got HTTP $cdn_bucket_status" ;;
esac

node - "$cdn_bucket" "$run_id" "$ARTIFACT_DIR/storage-deep-cdn-object-event-payload.json" <<'NODE'
const fs = require("fs");
const bucket = process.argv[2];
const runID = process.argv[3];
const target = process.argv[4];
fs.writeFileSync(target, JSON.stringify({
  event_id: `compat-storage-${runID}`,
  bucket,
  object_path: "public.txt",
  event_type: "object_updated",
}));
NODE
set +e
cdn_object_event_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-cdn-object-event.body" -w '%{http_code}' \
  -H "Authorization: Bearer $control_token" \
  -H "Content-Type: application/json" \
  -X POST "$SUPADUPA_API_URL/v1/projects/$SUPADUPA_TEST_REF/cdn/object-events" \
  --data-binary "@$ARTIFACT_DIR/storage-deep-cdn-object-event-payload.json" \
  2>"$ARTIFACT_DIR/storage-deep-cdn-object-event.stderr")"
cdn_object_event_rc="$?"
set -e
if [[ "$cdn_object_event_rc" -ne 0 ]]; then cdn_object_event_status="000"; fi
case "$cdn_object_event_status" in
  201) ;;
  *) fail "storage_deep.cdn_object_event" "expected HTTP 201, got HTTP $cdn_object_event_status" ;;
esac
if node - "$ARTIFACT_DIR/storage-deep-cdn-object-event.body" "$cdn_bucket" "$run_id" <<'NODE'
const fs = require("fs");
const invalidation = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const bucket = process.argv[3];
const runID = process.argv[4];
if (invalidation.status !== "completed") throw new Error("object-event invalidation not completed");
if (invalidation.source !== "storage_object_event") throw new Error(`unexpected source ${invalidation.source}`);
if (invalidation.event_id !== `compat-storage-${runID}`) throw new Error(`unexpected event_id ${invalidation.event_id}`);
if (!Array.isArray(invalidation.paths) || !invalidation.paths.includes(`/storage/v1/object/public/${bucket}/public.txt`)) throw new Error("object event path missing");
NODE
then
  pass "storage_deep.cdn_object_event" "smart revalidation completed"
else
  fail "storage_deep.cdn_object_event" "object-event invalidation response did not match expected shape"
fi

set +e
delete_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-delete.body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X DELETE "$api_url/storage/v1/object/$private_bucket" \
  --data '{"prefixes":["source.txt","moved.txt","signed.txt","tus.txt","cache.txt"]}' \
  2>"$ARTIFACT_DIR/storage-deep-delete.stderr")"
delete_rc="$?"
set -e
if [[ "$delete_rc" -ne 0 ]]; then delete_status="000"; fi
case "$delete_status" in
  2??) pass "storage_deep.delete_batch" "HTTP $delete_status" ;;
  *) fail "storage_deep.delete_batch" "expected 2xx, got HTTP $delete_status" ;;
esac

set +e
post_delete_list_status="$(curl -sS -o "$ARTIFACT_DIR/storage-deep-post-delete-list.body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X POST "$api_url/storage/v1/object/list/$private_bucket" \
  --data '{"prefix":"","limit":100,"offset":0,"sortBy":{"column":"name","order":"asc"}}' \
  2>"$ARTIFACT_DIR/storage-deep-post-delete-list.stderr")"
post_delete_list_rc="$?"
set -e
if [[ "$post_delete_list_rc" -ne 0 ]]; then post_delete_list_status="000"; fi
case "$post_delete_list_status" in
  2??)
    if grep -Eq '"name":"(source|moved|signed|tus|cache)[.]txt"' "$ARTIFACT_DIR/storage-deep-post-delete-list.body"; then
      fail "storage_deep.delete_batch_verify" "deleted objects still listed"
    fi
    pass "storage_deep.delete_batch_verify" "HTTP $post_delete_list_status"
    ;;
  *) fail "storage_deep.delete_batch_verify" "expected 2xx, got HTTP $post_delete_list_status" ;;
esac

pass "storage_deep.complete" "public/private/RLS cross-user isolation/copy/move/signed-upload/TUS/cache/image-transform/delete checks passed"
