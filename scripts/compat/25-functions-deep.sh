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
anon_key="$(reveal_secret_value anon_key)"
service_role="$(reveal_secret_value service_role)"
run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
suffix="$(printf '%s' "$run_id" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9-' | cut -c1-40)"
jwt_function="compat-jwt-${suffix:-jwt}"
public_function="compat-public-${suffix:-public}"
import_function="compat-import-${suffix:-import}"
mount_bucket="compat-fn-mount-${suffix:-mount}"
mount_object_path="public/mounted.txt"
mount_object_content="function storage mount content $run_id"
mount_nested_path="public/nested/alpha.txt"
mount_nested_content="function storage mount nested alpha $run_id"
mount_deep_path="public/nested/deeper/beta.txt"
mount_deep_content="function storage mount nested beta $run_id"
mount_outside_path="private/hidden.txt"
mount_outside_content="function storage mount outside prefix $run_id"
secret_value="compat-secret-${suffix:-secret}"
redeploy_secret_value="compat-secret-v2-${suffix:-secret}"
source_file="$ARTIFACT_DIR/functions-deep-source.ts"
redeploy_source_file="$ARTIFACT_DIR/functions-deep-redeploy-source.ts"
import_source_file="$ARTIFACT_DIR/functions-deep-import-source.ts"
original_import_map_file="$ARTIFACT_DIR/functions-deep-original-import-map"
original_worker_timeout_file="$ARTIFACT_DIR/functions-deep-original-worker-timeout"
region_id_file="$ARTIFACT_DIR/functions-deep-region-id"
mount_id_file="$ARTIFACT_DIR/functions-deep-mount-id"

cleanup_functions_deep() {
  if [[ -f "$region_id_file" ]]; then
    supadupa_cli_authed functions unregion --ref "$SUPADUPA_TEST_REF" --id "$(cat "$region_id_file")" \
      >"$ARTIFACT_DIR/functions-deep-region-cleanup.out" 2>"$ARTIFACT_DIR/functions-deep-region-cleanup.stderr" || true
  fi
  if [[ -f "$mount_id_file" ]]; then
    supadupa_cli_authed functions unmount --ref "$SUPADUPA_TEST_REF" --id "$(cat "$mount_id_file")" \
      >"$ARTIFACT_DIR/functions-deep-mount-cleanup.out" 2>"$ARTIFACT_DIR/functions-deep-mount-cleanup.stderr" || true
  fi
  curl -sS -o "$ARTIFACT_DIR/functions-deep-mount-object-cleanup.body" \
    -H "Content-Type: application/json" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X DELETE "$api_url/storage/v1/object/$mount_bucket" \
    --data "{\"prefixes\":[\"$mount_object_path\",\"$mount_nested_path\",\"$mount_deep_path\",\"$mount_outside_path\"]}" \
    2>"$ARTIFACT_DIR/functions-deep-mount-object-cleanup.stderr" || true
  supadupa_cli_authed storage-buckets delete --ref "$SUPADUPA_TEST_REF" --name "$mount_bucket" \
    >"$ARTIFACT_DIR/functions-deep-mount-bucket-cleanup.out" 2>"$ARTIFACT_DIR/functions-deep-mount-bucket-cleanup.stderr" || true
  if [[ -f "$original_import_map_file" || -f "$original_worker_timeout_file" ]]; then
    original_import_map="$(cat "$original_import_map_file" 2>/dev/null || true)"
    original_worker_timeout="$(cat "$original_worker_timeout_file" 2>/dev/null || printf '60000')"
    supadupa_cli_authed config set --ref "$SUPADUPA_TEST_REF" --area functions --set "import_map=$original_import_map" --set "worker_timeout_ms=$original_worker_timeout" \
      >"$ARTIFACT_DIR/functions-deep-import-map-restore.out" 2>"$ARTIFACT_DIR/functions-deep-import-map-restore.stderr" || true
  fi
  supadupa_cli_authed functions delete --ref "$SUPADUPA_TEST_REF" --name "$jwt_function" \
    >"$ARTIFACT_DIR/functions-deep-jwt-delete.out" 2>"$ARTIFACT_DIR/functions-deep-jwt-delete.stderr" || true
  supadupa_cli_authed functions delete --ref "$SUPADUPA_TEST_REF" --name "$public_function" \
    >"$ARTIFACT_DIR/functions-deep-public-delete.out" 2>"$ARTIFACT_DIR/functions-deep-public-delete.stderr" || true
  supadupa_cli_authed functions delete --ref "$SUPADUPA_TEST_REF" --name "$import_function" \
    >"$ARTIFACT_DIR/functions-deep-import-delete.out" 2>"$ARTIFACT_DIR/functions-deep-import-delete.stderr" || true
}
trap cleanup_functions_deep EXIT

project_function_dir() {
  local root="${SUPADUPA_PROJECT_ROOT:-$REPO_ROOT/runtime/projects}"
  printf '%s/%s/functions/%s' "$root" "$SUPADUPA_TEST_REF" "$1"
}

wait_storage_surface() {
  local attempts="${1:-30}"
  local delay_seconds="${2:-2}"
  local attempt
  local status
  for ((attempt = 1; attempt <= attempts; attempt++)); do
    set +e
    status="$(curl -sS -o "$ARTIFACT_DIR/functions-deep-storage-ready.body" -w '%{http_code}' \
      -H "apikey: $service_role" \
      -H "Authorization: Bearer $service_role" \
      "$api_url/storage/v1/bucket" 2>"$ARTIFACT_DIR/functions-deep-storage-ready.stderr")"
    local rc="$?"
    set -e
    if [[ "$rc" -eq 0 && "$status" == 2* ]]; then
      pass "functions_deep.storage_ready" "HTTP $status after attempt $attempt"
      return 0
    fi
    sleep "$delay_seconds"
  done
  fail "functions_deep.storage_ready" "expected Storage bucket list 2xx, got HTTP ${status:-000}"
}

upload_mount_object() {
  local object_path="$1"
  local object_content="$2"
  local label="$3"
  local status
  local rc
  set +e
  status="$(curl -sS -o "$ARTIFACT_DIR/functions-deep-mount-object-$label-upload.body" -w '%{http_code}' \
    -H "Content-Type: text/plain" \
    -H "x-upsert: true" \
    -H "apikey: $service_role" \
    -H "Authorization: Bearer $service_role" \
    -X POST "$api_url/storage/v1/object/$mount_bucket/$object_path" \
    --data-binary "$object_content" \
    2>"$ARTIFACT_DIR/functions-deep-mount-object-$label-upload.stderr")"
  rc="$?"
  set -e
  if [[ "$rc" -ne 0 ]]; then status="000"; fi
  case "$status" in
    2??) pass "functions_deep.storage_mount_object_upload_$label" "HTTP $status" ;;
    *) fail "functions_deep.storage_mount_object_upload_$label" "expected 2xx, got HTTP $status" ;;
  esac
}

cat >"$source_file" <<'TS'
Deno.serve(async (req: Request) => {
  const url = new URL(req.url);
  if (url.searchParams.get("throw") === "1") {
    throw new Error("compat function throw");
  }
  const sleepMs = Number(url.searchParams.get("sleep_ms") ?? "0");
  if (Number.isInteger(sleepMs) && sleepMs > 0) {
    await new Promise((resolve) => setTimeout(resolve, sleepMs));
  }
  const body = await req.text();
  if (url.searchParams.get("mode") === "mount") {
    const mount = Deno.env.get("COMPAT_FN_MOUNT") ?? "";
    let mountedText = "";
    try {
      mountedText = mount ? await Deno.readTextFile(`${mount}/mounted.txt`) : "";
    } catch (error) {
      try {
        mountedText = mount ? Deno.readTextFileSync(`${mount}/mounted.txt`) : "";
      } catch (syncError) {
        return new Response(
          JSON.stringify({
            ok: false,
            mount,
            env_alias_present: mount !== "",
            read_error: String(error),
            sync_read_error: String(syncError),
          }),
          { status: 500, headers: { "Content-Type": "application/json" } },
        );
      }
    }
    return new Response(
      JSON.stringify({
        ok: true,
        mount,
        mounted_text: mountedText,
        env_alias_present: mount !== "",
      }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    );
  }
  if (url.searchParams.get("mode") === "mount-scale") {
    const mount = Deno.env.get("COMPAT_FN_MOUNT") ?? "";
    const paths = ["mounted.txt", "nested/alpha.txt", "nested/deeper/beta.txt"];
    const files: Record<string, string> = {};
    const errors: Record<string, string> = {};
    for (const path of paths) {
      try {
        files[path] = mount ? await Deno.readTextFile(`${mount}/${path}`) : "";
      } catch (error) {
        errors[path] = String(error);
      }
    }
    let outside_prefix_visible = false;
    let outside_prefix_error = "";
    try {
      await Deno.readTextFile(`${mount}/../private/hidden.txt`);
      outside_prefix_visible = true;
    } catch (error) {
      outside_prefix_error = String(error);
    }
    return new Response(
      JSON.stringify({
        ok: Object.keys(errors).length === 0 && !outside_prefix_visible,
        mount,
        files,
        errors,
        outside_prefix_visible,
        outside_prefix_error,
        env_alias_present: mount !== "",
      }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    );
  }
  if (url.searchParams.get("mode") === "mount-write") {
    const mount = Deno.env.get("COMPAT_FN_MOUNT") ?? "";
    const writeText = url.searchParams.get("value") ?? "compat mount write";
    if (mount === "") {
      return new Response(
        JSON.stringify({ ok: false, mount, env_alias_present: false }),
        { status: 500, headers: { "Content-Type": "application/json" } },
      );
    }
    try {
      await Deno.writeTextFile(`${mount}/mounted.txt`, writeText);
      const mountedText = await Deno.readTextFile(`${mount}/mounted.txt`);
      return new Response(
        JSON.stringify({
          ok: false,
          mount,
          write_allowed: true,
          mounted_text: mountedText,
          env_alias_present: true,
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    } catch (error) {
      const mountedText = await Deno.readTextFile(`${mount}/mounted.txt`);
      return new Response(
        JSON.stringify({
          ok: true,
          mount,
          write_allowed: false,
          mounted_text: mountedText,
          write_error: String(error),
          env_alias_present: true,
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }
  }
  return new Response(
    JSON.stringify({
      ok: true,
      method: req.method,
      path: url.pathname,
      search: url.search,
      body,
      header: req.headers.get("x-compat-header") ?? "",
      has_authorization: req.headers.has("authorization"),
      function_name: Deno.env.get("SUPABASE_FUNCTION_NAME") ?? "",
      function_version: Deno.env.get("SUPABASE_FUNCTION_VERSION") ?? "",
      sb_region: Deno.env.get("SB_REGION") ?? "",
      verify_jwt: Deno.env.get("VERIFY_JWT") ?? "",
      secret_present: Deno.env.get("COMPAT_SECRET") === "compat-secret-placeholder",
    }),
    {
      status: 200,
      headers: {
        "Content-Type": "application/json",
        "X-Compat-Function": Deno.env.get("SUPABASE_FUNCTION_NAME") ?? "",
      },
    },
  );
});
TS
node - "$source_file" "$secret_value" <<'NODE'
const fs = require("fs");
const file = process.argv[2];
const secret = process.argv[3];
fs.writeFileSync(file, fs.readFileSync(file, "utf8").replace("compat-secret-placeholder", secret));
NODE

deploy_function() {
  local name="$1"
  local verify_jwt="$2"
  local label="${3:-$name}"
  local deploy_source="${4:-$source_file}"
  local deploy_secret="${5:-$secret_value}"
  local out="$ARTIFACT_DIR/functions-deep-$label-deploy.json"
  local err="$ARTIFACT_DIR/functions-deep-$label-deploy.stderr"
  if supadupa_cli_authed functions deploy \
    --ref "$SUPADUPA_TEST_REF" \
    --name "$name" \
    --entrypoint index.ts \
    --source-file "$deploy_source" \
    --verify-jwt="$verify_jwt" \
    --secret "COMPAT_SECRET=$deploy_secret" \
    >"$out" 2>"$err"; then
    node - "$out" "$name" "$verify_jwt" "$deploy_secret" <<'NODE'
const fs = require("fs");
const fn = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const name = process.argv[3];
const verify = process.argv[4] === "true";
const secret = process.argv[5];
if (fn.name !== name) throw new Error(`name=${fn.name}`);
if (fn.verify_jwt !== verify) throw new Error(`verify_jwt=${fn.verify_jwt}`);
if (fn.status !== "deployed") throw new Error(`status=${fn.status}`);
if (!Number.isInteger(fn.version) || fn.version < 1) throw new Error(`version=${fn.version}`);
if (!fn.source_hash || fn.source_bytes <= 0) throw new Error("missing source metadata");
if (JSON.stringify(fn).includes(secret)) throw new Error("secret leaked in deploy response");
NODE
    pass "functions_deep.deploy_$label" "name=$name verify_jwt=$verify_jwt"
  else
    fail "functions_deep.deploy_$label" "deploy failed; see $(basename "$err")"
  fi
}

deploy_function "$jwt_function" true
deploy_function "$public_function" false

functions_list="$ARTIFACT_DIR/functions-deep-list.json"
if supadupa_cli_authed functions list --ref "$SUPADUPA_TEST_REF" >"$functions_list" 2>"$ARTIFACT_DIR/functions-deep-list.stderr"; then
  node - "$functions_list" "$jwt_function" "$public_function" "$secret_value" <<'NODE'
const fs = require("fs");
const list = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const jwtName = process.argv[3];
const publicName = process.argv[4];
const secret = process.argv[5];
for (const name of [jwtName, publicName]) {
  const fn = list.find((item) => item.name === name);
  if (!fn) throw new Error(`missing ${name}`);
  if (JSON.stringify(fn).includes(secret)) throw new Error(`secret leaked for ${name}`);
}
NODE
  pass "functions_deep.list_metadata" "temporary functions listed without secret values"
else
  fail "functions_deep.list_metadata" "function list failed"
fi

functions_config_file="$ARTIFACT_DIR/functions-deep-original-config.json"
if supadupa_cli_authed config get --ref "$SUPADUPA_TEST_REF" --area functions >"$functions_config_file" 2>"$ARTIFACT_DIR/functions-deep-original-config.stderr"; then
  json_get_file_optional "$functions_config_file" "config.import_map" >"$original_import_map_file"
  original_worker_timeout="$(json_get_file_optional "$functions_config_file" "config.worker_timeout_ms")"
  printf '%s' "${original_worker_timeout:-60000}" >"$original_worker_timeout_file"
else
  fail "functions_deep.config_read" "failed to fetch functions config; see functions-deep-original-config.stderr"
fi

wait_storage_surface

mount_bucket_file="$ARTIFACT_DIR/functions-deep-mount-bucket-create.json"
if supadupa_cli_authed storage-buckets create \
  --ref "$SUPADUPA_TEST_REF" \
  --name "$mount_bucket" \
  --file-size-limit 1048576 \
  --metadata "compat=functions-deep" \
  >"$mount_bucket_file" 2>"$ARTIFACT_DIR/functions-deep-mount-bucket-create.stderr"; then
  node - "$mount_bucket_file" "$mount_bucket" <<'NODE'
const fs = require("fs");
const bucket = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (bucket.name !== process.argv[3]) throw new Error(`name=${bucket.name}`);
if (bucket.public !== false) throw new Error(`public=${bucket.public}`);
if (bucket.status !== "configured") throw new Error(`status=${bucket.status}`);
NODE
  pass "functions_deep.storage_mount_bucket" "$mount_bucket"
else
  fail "functions_deep.storage_mount_bucket" "bucket create failed; see functions-deep-mount-bucket-create.stderr"
fi

upload_mount_object "$mount_object_path" "$mount_object_content" root
upload_mount_object "$mount_nested_path" "$mount_nested_content" nested
upload_mount_object "$mount_deep_path" "$mount_deep_content" deep
upload_mount_object "$mount_outside_path" "$mount_outside_content" outside_prefix

region_file="$ARTIFACT_DIR/functions-deep-region-create.json"
if supadupa_cli_authed functions region \
  --ref "$SUPADUPA_TEST_REF" \
  --function "$public_function" \
  --region us-east-1 \
  --routing-policy nearest \
  >"$region_file" 2>"$ARTIFACT_DIR/functions-deep-region-create.stderr"; then
  node - "$region_file" "$public_function" >"$region_id_file" <<'NODE'
const fs = require("fs");
const region = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const functionName = process.argv[3];
if (!region.id) throw new Error("missing region id");
if (region.function_name !== functionName) throw new Error(`function_name=${region.function_name}`);
if (region.region !== "us-east-1") throw new Error(`region=${region.region}`);
if (region.routing_policy !== "nearest") throw new Error(`routing_policy=${region.routing_policy}`);
if (!String(region.invocation_url || "").includes(`${functionName}.us-east-1.`)) throw new Error(`invocation_url=${region.invocation_url}`);
if (region.status !== "configured") throw new Error(`status=${region.status}`);
process.stdout.write(region.id);
NODE
  pass "functions_deep.region_create" "us-east-1 nearest"
else
  fail "functions_deep.region_create" "region create failed; see functions-deep-region-create.stderr"
fi

regions_list_file="$ARTIFACT_DIR/functions-deep-regions-list.json"
if supadupa_cli_authed functions regions --ref "$SUPADUPA_TEST_REF" >"$regions_list_file" 2>"$ARTIFACT_DIR/functions-deep-regions-list.stderr"; then
  node - "$regions_list_file" "$(cat "$region_id_file")" "$public_function" <<'NODE'
const fs = require("fs");
const regions = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const id = process.argv[3];
const functionName = process.argv[4];
const region = regions.find((item) => item.id === id);
if (!region) throw new Error(`missing region ${id}`);
if (region.function_name !== functionName || region.region !== "us-east-1") throw new Error(JSON.stringify(region));
NODE
  pass "functions_deep.region_list" "configured region listed"
else
  fail "functions_deep.region_list" "region list failed; see functions-deep-regions-list.stderr"
fi

validate_region_invocation() {
  local label="$1"
  local url_suffix="$2"
  local header_value="${3:-}"
  local body_file="$ARTIFACT_DIR/functions-deep-region-$label.body"
  local header_file="$ARTIFACT_DIR/functions-deep-region-$label.headers"
  local err_file="$ARTIFACT_DIR/functions-deep-region-$label.stderr"
  local status
  local args=(-sS -o "$body_file" -D "$header_file" -w '%{http_code}' -H "apikey: $anon_key")
  if [[ -n "$header_value" ]]; then
    args+=(-H "x-region: $header_value")
  fi
  status="$(curl "${args[@]}" "$api_url/functions/v1/$public_function$url_suffix" 2>"$err_file")"
  case "$status" in
    2??)
      if node - "$body_file" "$header_file" "us-east-1" <<'NODE'
const fs = require("fs");
const body = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const headers = fs.readFileSync(process.argv[3], "utf8");
const expected = process.argv[4];
if (body.sb_region !== expected) throw new Error(`sb_region=${body.sb_region}`);
if (!new RegExp(`^x-sb-edge-region:\\s*${expected}\\s*$`, "im").test(headers)) {
  throw new Error(`missing x-sb-edge-region=${expected}`);
}
NODE
      then
        pass "functions_deep.region_invoke_$label" "SB_REGION and x-sb-edge-region=$header_value$url_suffix"
      else
        fail "functions_deep.region_invoke_$label" "regional invocation metadata mismatch"
      fi
      ;;
    *) fail "functions_deep.region_invoke_$label" "expected 2xx, got HTTP $status; see $(basename "$err_file")" ;;
  esac
}

validate_region_invocation "header" "" "us-east-1"
validate_region_invocation "query" "?forceFunctionRegion=us-east-1"

mount_file="$ARTIFACT_DIR/functions-deep-mount-create.json"
if supadupa_cli_authed functions mount \
  --ref "$SUPADUPA_TEST_REF" \
  --function "$public_function" \
  --bucket "$mount_bucket" \
  --mount-path "/mnt/$mount_bucket" \
  --prefix public \
  --env-alias COMPAT_FN_MOUNT \
  --read-only=true \
  >"$mount_file" 2>"$ARTIFACT_DIR/functions-deep-mount-create.stderr"; then
  node - "$mount_file" "$public_function" "$mount_bucket" >"$mount_id_file" <<'NODE'
const fs = require("fs");
const mount = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const functionName = process.argv[3];
const bucketName = process.argv[4];
if (!mount.id) throw new Error("missing mount id");
if (mount.function_name !== functionName) throw new Error(`function_name=${mount.function_name}`);
if (mount.bucket_name !== bucketName) throw new Error(`bucket_name=${mount.bucket_name}`);
if (mount.mount_path !== `/mnt/${bucketName}`) throw new Error(`mount_path=${mount.mount_path}`);
if (mount.read_only !== true) throw new Error(`read_only=${mount.read_only}`);
if (mount.prefix !== "public") throw new Error(`prefix=${mount.prefix}`);
if (mount.env_alias !== "COMPAT_FN_MOUNT") throw new Error(`env_alias=${mount.env_alias}`);
if (mount.status !== "configured") throw new Error(`status=${mount.status}`);
process.stdout.write(mount.id);
NODE
  pass "functions_deep.storage_mount_create" "/mnt/$mount_bucket"
else
  fail "functions_deep.storage_mount_create" "storage mount create failed; see functions-deep-mount-create.stderr"
fi

mounts_list_file="$ARTIFACT_DIR/functions-deep-mounts-list.json"
if supadupa_cli_authed functions mounts --ref "$SUPADUPA_TEST_REF" >"$mounts_list_file" 2>"$ARTIFACT_DIR/functions-deep-mounts-list.stderr"; then
  node - "$mounts_list_file" "$(cat "$mount_id_file")" "$public_function" "$mount_bucket" <<'NODE'
const fs = require("fs");
const mounts = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const id = process.argv[3];
const functionName = process.argv[4];
const bucketName = process.argv[5];
const mount = mounts.find((item) => item.id === id);
if (!mount) throw new Error(`missing mount ${id}`);
if (mount.function_name !== functionName || mount.bucket_name !== bucketName) throw new Error(JSON.stringify(mount));
NODE
  pass "functions_deep.storage_mount_list" "configured mount listed"
else
  fail "functions_deep.storage_mount_list" "storage mount list failed; see functions-deep-mounts-list.stderr"
fi

function_dir="$(project_function_dir "$public_function")"
if [[ -d "$function_dir" ]]; then
  node - "$function_dir/regions.json" "$function_dir/storage-mounts.json" "$public_function" "$mount_bucket" <<'NODE'
const fs = require("fs");
const regions = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const mounts = JSON.parse(fs.readFileSync(process.argv[3], "utf8"));
const functionName = process.argv[4];
const bucketName = process.argv[5];
if (!regions.some((region) => region.function_name === functionName && region.region === "us-east-1")) {
  throw new Error(`region manifest mismatch: ${JSON.stringify(regions)}`);
}
if (!mounts.some((mount) => mount.function_name === functionName && mount.bucket_name === bucketName && mount.mount_path === `/mnt/${bucketName}`)) {
  throw new Error(`mount manifest mismatch: ${JSON.stringify(mounts)}`);
}
NODE
  pass "functions_deep.region_mount_manifests" "desired-state artifacts written"
else
  skip "functions_deep.region_mount_manifests" "local project function dir unavailable"
fi

mount_read_ok=false
for attempt in $(seq 1 30); do
  set +e
  mount_read_status="$(curl -sS -o "$ARTIFACT_DIR/functions_deep.storage_mount_runtime_read.body" -w '%{http_code}' \
    -X GET "$api_url/functions/v1/$public_function?mode=mount" \
    2>"$ARTIFACT_DIR/functions_deep.storage_mount_runtime_read.stderr")"
  mount_read_rc="$?"
  set -e
  if [[ "$mount_read_rc" -eq 0 && "$mount_read_status" == "200" ]]; then
    if node - "$ARTIFACT_DIR/functions_deep.storage_mount_runtime_read.body" "$mount_object_content" <<'NODE'
const fs = require("fs");
const body = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (typeof body.mount !== "string" || body.mount === "") throw new Error(`mount=${body.mount}`);
if (body.mounted_text !== process.argv[3]) throw new Error(`mounted_text=${body.mounted_text}`);
if (body.env_alias_present !== true) throw new Error(`env_alias_present=${body.env_alias_present}`);
NODE
    then
      pass "functions_deep.storage_mount_runtime_read" "HTTP $mount_read_status after attempt $attempt"
      mount_read_ok=true
      break
    fi
  fi
  sleep 2
done
if [[ "$mount_read_ok" != "true" ]]; then
  fail "functions_deep.storage_mount_runtime_read" "function could not read mounted object; see functions_deep.storage_mount_runtime_read.body"
fi

mount_scale_ok=false
for attempt in $(seq 1 30); do
  set +e
  mount_scale_status="$(curl -sS -o "$ARTIFACT_DIR/functions_deep.storage_mount_scale.body" -w '%{http_code}' \
    -X GET "$api_url/functions/v1/$public_function?mode=mount-scale" \
    2>"$ARTIFACT_DIR/functions_deep.storage_mount_scale.stderr")"
  mount_scale_rc="$?"
  set -e
  if [[ "$mount_scale_rc" -eq 0 && "$mount_scale_status" == "200" ]]; then
    if node - "$ARTIFACT_DIR/functions_deep.storage_mount_scale.body" "$mount_object_content" "$mount_nested_content" "$mount_deep_content" <<'NODE'
const fs = require("fs");
const body = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const expected = {
  "mounted.txt": process.argv[3],
  "nested/alpha.txt": process.argv[4],
  "nested/deeper/beta.txt": process.argv[5],
};
if (typeof body.mount !== "string" || body.mount === "") throw new Error(`mount=${body.mount}`);
if (body.env_alias_present !== true) throw new Error(`env_alias_present=${body.env_alias_present}`);
if (body.outside_prefix_visible !== false) throw new Error("outside-prefix object was visible through mount");
for (const [path, content] of Object.entries(expected)) {
  if (body.files?.[path] !== content) {
    throw new Error(`${path}=${body.files?.[path]}`);
  }
}
if (Object.keys(body.errors || {}).length > 0) throw new Error(`read errors: ${JSON.stringify(body.errors)}`);
NODE
    then
      pass "functions_deep.storage_mount_scale" "HTTP $mount_scale_status after attempt $attempt"
      mount_scale_ok=true
      break
    fi
  fi
  sleep 2
done
if [[ "$mount_scale_ok" != "true" ]]; then
  fail "functions_deep.storage_mount_scale" "function could not read nested mounted objects or leaked outside-prefix object; see functions_deep.storage_mount_scale.body"
fi

mount_write_ok=false
for attempt in $(seq 1 30); do
  set +e
  mount_write_status="$(curl -sS -o "$ARTIFACT_DIR/functions_deep.storage_mount_read_only.body" -w '%{http_code}' \
    -X GET "$api_url/functions/v1/$public_function?mode=mount-write&value=blocked-$run_id" \
    2>"$ARTIFACT_DIR/functions_deep.storage_mount_read_only.stderr")"
  mount_write_rc="$?"
  set -e
  if [[ "$mount_write_rc" -eq 0 && "$mount_write_status" == "200" ]]; then
    if node - "$ARTIFACT_DIR/functions_deep.storage_mount_read_only.body" "$mount_object_content" <<'NODE'
const fs = require("fs");
const body = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (typeof body.mount !== "string" || body.mount === "") throw new Error(`mount=${body.mount}`);
if (body.write_allowed !== false) throw new Error(`write_allowed=${body.write_allowed}`);
if (body.mounted_text !== process.argv[3]) throw new Error(`mounted_text=${body.mounted_text}`);
if (body.env_alias_present !== true) throw new Error(`env_alias_present=${body.env_alias_present}`);
if (!String(body.write_error || "").match(/PermissionDenied|permission denied|Read-only|readonly|AccessDenied|NotFound:.*writefile/i)) {
  throw new Error(`unexpected write_error=${body.write_error}`);
}
NODE
    then
      pass "functions_deep.storage_mount_read_only" "HTTP $mount_write_status after attempt $attempt"
      mount_write_ok=true
      break
    fi
  fi
  sleep 2
done
if [[ "$mount_write_ok" != "true" ]]; then
  fail "functions_deep.storage_mount_read_only" "read-only mount accepted a write or changed content; see functions_deep.storage_mount_read_only.body"
fi

set +e
mount_verify_status="$(curl -sS -o "$ARTIFACT_DIR/functions-deep-mount-object-verify.body" -w '%{http_code}' \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  "$api_url/storage/v1/object/$mount_bucket/$mount_object_path" \
  2>"$ARTIFACT_DIR/functions-deep-mount-object-verify.stderr")"
mount_verify_rc="$?"
set -e
if [[ "$mount_verify_rc" -ne 0 ]]; then mount_verify_status="000"; fi
case "$mount_verify_status" in
  2??)
    if [[ "$(cat "$ARTIFACT_DIR/functions-deep-mount-object-verify.body")" == "$mount_object_content" ]]; then
      pass "functions_deep.storage_mount_origin_unchanged" "HTTP $mount_verify_status"
    else
      fail "functions_deep.storage_mount_origin_unchanged" "mounted write mutated Storage object"
    fi
    ;;
  *) fail "functions_deep.storage_mount_origin_unchanged" "expected 2xx, got HTTP $mount_verify_status" ;;
esac

if supadupa_cli_authed functions unregion --ref "$SUPADUPA_TEST_REF" --id "$(cat "$region_id_file")" \
  >"$ARTIFACT_DIR/functions-deep-region-delete.out" 2>"$ARTIFACT_DIR/functions-deep-region-delete.stderr"; then
  rm -f "$region_id_file"
  pass "functions_deep.region_delete" "configured region removed"
else
  fail "functions_deep.region_delete" "region delete failed; see functions-deep-region-delete.stderr"
fi

if supadupa_cli_authed functions unmount --ref "$SUPADUPA_TEST_REF" --id "$(cat "$mount_id_file")" \
  >"$ARTIFACT_DIR/functions-deep-mount-delete.out" 2>"$ARTIFACT_DIR/functions-deep-mount-delete.stderr"; then
  rm -f "$mount_id_file"
  pass "functions_deep.storage_mount_delete" "configured mount removed"
else
  fail "functions_deep.storage_mount_delete" "storage mount delete failed; see functions-deep-mount-delete.stderr"
fi

if [[ -d "$function_dir" ]]; then
  if [[ -e "$function_dir/regions.json" || -e "$function_dir/storage-mounts.json" ]]; then
    fail "functions_deep.region_mount_manifest_cleanup" "stale desired-state artifact remained"
  fi
  pass "functions_deep.region_mount_manifest_cleanup" "desired-state artifacts removed"
fi

set +e
mount_delete_status="$(curl -sS -o "$ARTIFACT_DIR/functions-deep-mount-object-delete.body" -w '%{http_code}' \
  -H "Content-Type: application/json" \
  -H "apikey: $service_role" \
  -H "Authorization: Bearer $service_role" \
  -X DELETE "$api_url/storage/v1/object/$mount_bucket" \
  --data "{\"prefixes\":[\"$mount_object_path\",\"$mount_nested_path\",\"$mount_deep_path\",\"$mount_outside_path\"]}" \
  2>"$ARTIFACT_DIR/functions-deep-mount-object-delete.stderr")"
mount_delete_rc="$?"
set -e
if [[ "$mount_delete_rc" -ne 0 ]]; then mount_delete_status="000"; fi
case "$mount_delete_status" in
  2??) pass "functions_deep.storage_mount_object_delete" "HTTP $mount_delete_status" ;;
  *) fail "functions_deep.storage_mount_object_delete" "expected 2xx, got HTTP $mount_delete_status" ;;
esac

if supadupa_cli_authed storage-buckets delete --ref "$SUPADUPA_TEST_REF" --name "$mount_bucket" \
  >"$ARTIFACT_DIR/functions-deep-mount-bucket-delete.out" 2>"$ARTIFACT_DIR/functions-deep-mount-bucket-delete.stderr"; then
  pass "functions_deep.storage_mount_bucket_delete" "$mount_bucket"
else
  fail "functions_deep.storage_mount_bucket_delete" "bucket delete failed; see functions-deep-mount-bucket-delete.stderr"
fi

request_function() {
  local test_name="$1"
  local name="$2"
  local method="$3"
  local auth_mode="$4"
  local path_suffix="$5"
  local body="$6"
  local expected_status="$7"
  local out="$ARTIFACT_DIR/$test_name.body"
  local headers="$ARTIFACT_DIR/$test_name.headers"
  local err="$ARTIFACT_DIR/$test_name.stderr"
  local status
  local args=(-sS -D "$headers" -o "$out" -w '%{http_code}' -X "$method" -H "x-compat-header: $test_name")
  case "$auth_mode" in
    anon)
      args+=(-H "apikey: $anon_key" -H "Authorization: Bearer $anon_key")
      ;;
    apikey)
      args+=(-H "apikey: $anon_key")
      ;;
    none)
      ;;
    *)
      fail "$test_name" "unknown auth mode $auth_mode"
      ;;
  esac
  if [[ "$body" != "__NO_BODY__" ]]; then
    args+=(-H "Content-Type: text/plain" --data-binary "$body")
  fi
  set +e
  status="$(curl "${args[@]}" "$api_url/functions/v1/$name$path_suffix" 2>"$err")"
  local rc="$?"
  set -e
  if [[ "$rc" -ne 0 ]]; then status="000"; fi
  if [[ "$status" != "$expected_status" ]]; then
    fail "$test_name" "expected HTTP $expected_status, got HTTP $status"
  fi
  pass "$test_name" "HTTP $status"
}

request_function_with_retries() {
  local test_name="$1"
  local name="$2"
  local method="$3"
  local auth_mode="$4"
  local path_suffix="$5"
  local body="$6"
  local expected_status="$7"
  local attempts="${8:-30}"
  local delay_seconds="${9:-2}"
  local attempt
  local out="$ARTIFACT_DIR/$test_name.body"
  local headers="$ARTIFACT_DIR/$test_name.headers"
  local err="$ARTIFACT_DIR/$test_name.stderr"
  local status
  local args

  for ((attempt = 1; attempt <= attempts; attempt++)); do
    args=(-sS -D "$headers" -o "$out" -w '%{http_code}' -X "$method" -H "x-compat-header: $test_name")
    case "$auth_mode" in
      anon)
        args+=(-H "apikey: $anon_key" -H "Authorization: Bearer $anon_key")
        ;;
      apikey)
        args+=(-H "apikey: $anon_key")
        ;;
      none)
        ;;
      *)
        fail "$test_name" "unknown auth mode $auth_mode"
        ;;
    esac
    if [[ "$body" != "__NO_BODY__" ]]; then
      args+=(-H "Content-Type: text/plain" --data-binary "$body")
    fi
    set +e
    status="$(curl "${args[@]}" "$api_url/functions/v1/$name$path_suffix" 2>"$err")"
    local rc="$?"
    set -e
    if [[ "$rc" -ne 0 ]]; then status="000"; fi
    if [[ "$status" == "$expected_status" ]]; then
      pass "$test_name" "HTTP $status after attempt $attempt"
      return 0
    fi
    sleep "$delay_seconds"
  done
  fail "$test_name" "expected HTTP $expected_status, got HTTP $status"
}

validate_redeploy_runtime_body() {
  local test_name="$1"
  local body_file="$2"
  local expected_header="$3"
  node - "$body_file" "$jwt_function" "$run_id" "$expected_header" <<'NODE'
const fs = require("fs");
const body = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const name = process.argv[3];
const runId = process.argv[4];
if (body.redeployed !== true) throw new Error("redeployed marker missing");
if (body.method !== "PATCH") throw new Error(`method=${body.method}`);
if (!(body.path.endsWith(`/functions/v1/${name}`) || body.path.endsWith(`/${name}`))) throw new Error(`path=${body.path}`);
if (body.search !== "?mode=redeploy") throw new Error(`search=${body.search}`);
if (body.body !== `redeploy body ${runId}`) throw new Error(`body=${body.body}`);
if (body.header !== process.argv[5]) throw new Error(`header=${body.header}`);
if (body.has_authorization !== false) throw new Error("unexpected authorization");
if (body.function_name !== name) throw new Error(`function_name=${body.function_name}`);
if (body.function_version !== "2") throw new Error(`function_version=${body.function_version}`);
if (body.verify_jwt !== "false") throw new Error(`verify_jwt=${body.verify_jwt}`);
if (body.secret_present !== true) throw new Error("redeploy secret not injected");
NODE
  pass "$test_name" "same-name redeploy updated runtime source/env/auth"
}

restart_edge_runtime_if_available() {
  if ! command -v docker >/dev/null 2>&1; then
    skip "functions_deep.restart_runtime" "docker not available"
    return 1
  fi
  local container="${SUPADUPA_FUNCTIONS_EDGE_CONTAINER:-$SUPADUPA_TEST_REF-edge-runtime-1}"
  if ! docker inspect "$container" >/dev/null 2>&1; then
    skip "functions_deep.restart_runtime" "edge runtime container $container not found"
    return 1
  fi
  if docker restart "$container" >"$ARTIFACT_DIR/functions-deep-edge-restart.out" 2>"$ARTIFACT_DIR/functions-deep-edge-restart.stderr"; then
    pass "functions_deep.restart_runtime" "$container"
    return 0
  fi
  fail "functions_deep.restart_runtime" "docker restart failed; see functions-deep-edge-restart.stderr"
}

if supadupa_cli_authed config set --ref "$SUPADUPA_TEST_REF" --area functions --set "worker_timeout_ms=1000" \
  >"$ARTIFACT_DIR/functions-deep-timeout-config-set.json" 2>"$ARTIFACT_DIR/functions-deep-timeout-config-set.stderr"; then
  pass "functions_deep.timeout_config" "worker_timeout_ms=1000"
else
  fail "functions_deep.timeout_config" "failed to set timeout config; see functions-deep-timeout-config-set.stderr"
fi

request_function_with_retries "functions_deep.timeout_504" "$public_function" GET none "?sleep_ms=2500" "__NO_BODY__" 504 12 1
node - "$ARTIFACT_DIR/functions_deep.timeout_504.body" <<'NODE'
const fs = require("fs");
const body = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (body.msg !== "function timed out") throw new Error(`msg=${body.msg}`);
if (body.timeout_ms !== 1000) throw new Error(`timeout_ms=${body.timeout_ms}`);
NODE
pass "functions_deep.timeout_response_shape" "slow function returned hosted-style timeout response"

if supadupa_cli_authed config set --ref "$SUPADUPA_TEST_REF" --area functions --set "worker_timeout_ms=$(cat "$original_worker_timeout_file")" \
  >"$ARTIFACT_DIR/functions-deep-timeout-config-restore.json" 2>"$ARTIFACT_DIR/functions-deep-timeout-config-restore.stderr"; then
  pass "functions_deep.timeout_config_restore" "worker_timeout_ms=$(cat "$original_worker_timeout_file")"
else
  fail "functions_deep.timeout_config_restore" "failed to restore timeout config; see functions-deep-timeout-config-restore.stderr"
fi

request_function_with_retries "functions_deep.jwt_without_auth_rejected" "$jwt_function" GET none "" "__NO_BODY__" 401 20 1
request_function_with_retries "functions_deep.jwt_apikey_only_rejected" "$jwt_function" GET apikey "" "__NO_BODY__" 401 20 1
request_function_with_retries "functions_deep.jwt_authorized_post" "$jwt_function" POST anon "?mode=jwt" "jwt body $run_id" 200 20 1
node - "$ARTIFACT_DIR/functions_deep.jwt_authorized_post.body" "$jwt_function" "$run_id" <<'NODE'
const fs = require("fs");
const body = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const name = process.argv[3];
const runId = process.argv[4];
if (body.method !== "POST") throw new Error(`method=${body.method}`);
if (!(body.path.endsWith(`/functions/v1/${name}`) || body.path.endsWith(`/${name}`))) throw new Error(`path=${body.path}`);
if (body.search !== "?mode=jwt") throw new Error(`search=${body.search}`);
if (body.body !== `jwt body ${runId}`) throw new Error(`body=${body.body}`);
if (body.header !== "functions_deep.jwt_authorized_post") throw new Error(`header=${body.header}`);
if (body.has_authorization !== true) throw new Error("authorization missing");
if (body.function_name !== name) throw new Error(`function_name=${body.function_name}`);
if (body.verify_jwt !== "true") throw new Error(`verify_jwt=${body.verify_jwt}`);
if (body.secret_present !== true) throw new Error("secret not injected");
NODE
pass "functions_deep.jwt_request_shape" "method/body/header/env verified"

cat >"$redeploy_source_file" <<'TS'
Deno.serve(async (req: Request) => {
  const url = new URL(req.url);
  const body = await req.text();
  return new Response(
    JSON.stringify({
      ok: true,
      redeployed: true,
      method: req.method,
      path: url.pathname,
      search: url.search,
      body,
      header: req.headers.get("x-compat-header") ?? "",
      has_authorization: req.headers.has("authorization"),
      function_name: Deno.env.get("SUPABASE_FUNCTION_NAME") ?? "",
      function_version: Deno.env.get("SUPABASE_FUNCTION_VERSION") ?? "",
      verify_jwt: Deno.env.get("VERIFY_JWT") ?? "",
      secret_present: Deno.env.get("COMPAT_SECRET") === "compat-secret-v2-placeholder",
    }),
    {
      status: 200,
      headers: { "Content-Type": "application/json" },
    },
  );
});
TS
node - "$redeploy_source_file" "$redeploy_secret_value" <<'NODE'
const fs = require("fs");
const file = process.argv[2];
const secret = process.argv[3];
fs.writeFileSync(file, fs.readFileSync(file, "utf8").replace("compat-secret-v2-placeholder", secret));
NODE

deploy_function "$jwt_function" false "$jwt_function-redeploy" "$redeploy_source_file" "$redeploy_secret_value"
node - "$ARTIFACT_DIR/functions-deep-$jwt_function-redeploy-deploy.json" "$jwt_function" "$redeploy_secret_value" <<'NODE'
const fs = require("fs");
const fn = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const name = process.argv[3];
const secret = process.argv[4];
if (fn.name !== name) throw new Error(`name=${fn.name}`);
if (fn.version !== 2) throw new Error(`version=${fn.version}`);
if (fn.verify_jwt !== false) throw new Error(`verify_jwt=${fn.verify_jwt}`);
if (JSON.stringify(fn).includes(secret)) throw new Error("redeploy secret leaked");
NODE
pass "functions_deep.redeploy_metadata" "version incremented and verify_jwt updated"

request_function "functions_deep.redeploy_without_auth" "$jwt_function" PATCH none "?mode=redeploy" "redeploy body $run_id" 200
validate_redeploy_runtime_body "functions_deep.redeploy_runtime" "$ARTIFACT_DIR/functions_deep.redeploy_without_auth.body" "functions_deep.redeploy_without_auth"

if restart_edge_runtime_if_available; then
  request_function_with_retries "functions_deep.redeploy_after_runtime_restart" "$jwt_function" PATCH none "?mode=redeploy" "redeploy body $run_id" 200
  validate_redeploy_runtime_body "functions_deep.restart_persistence" "$ARTIFACT_DIR/functions_deep.redeploy_after_runtime_restart.body" "functions_deep.redeploy_after_runtime_restart"
fi

import_message="import-map-ok-${suffix:-import}"
import_map="$(node - "$import_message" <<'NODE'
const message = process.argv[2];
const moduleSource = `export const message = ${JSON.stringify(message)};`;
const dataURL = "data:application/typescript," + encodeURIComponent(moduleSource);
process.stdout.write(JSON.stringify({ imports: { "compat:message": dataURL } }));
NODE
)"
if supadupa_cli_authed config set --ref "$SUPADUPA_TEST_REF" --area functions --set "import_map=$import_map" \
  >"$ARTIFACT_DIR/functions-deep-import-map-set.json" 2>"$ARTIFACT_DIR/functions-deep-import-map-set.stderr"; then
  pass "functions_deep.import_map_config" "inline import map applied"
else
  fail "functions_deep.import_map_config" "failed to set import map; see functions-deep-import-map-set.stderr"
fi

cat >"$import_source_file" <<'TS'
import { message } from "compat:message";

Deno.serve(() => {
  return Response.json({
    ok: true,
    import_map: true,
    message,
    function_name: Deno.env.get("SUPABASE_FUNCTION_NAME") ?? "",
    function_version: Deno.env.get("SUPABASE_FUNCTION_VERSION") ?? "",
  });
});
TS
deploy_function "$import_function" false "$import_function" "$import_source_file" "$secret_value"
request_function_with_retries "functions_deep.import_map_runtime" "$import_function" GET none "" "__NO_BODY__" 200
node - "$ARTIFACT_DIR/functions_deep.import_map_runtime.body" "$import_function" "$import_message" <<'NODE'
const fs = require("fs");
const body = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const name = process.argv[3];
const message = process.argv[4];
if (body.ok !== true) throw new Error("ok missing");
if (body.import_map !== true) throw new Error("import_map marker missing");
if (body.message !== message) throw new Error(`message=${body.message}`);
if (body.function_name !== name) throw new Error(`function_name=${body.function_name}`);
if (body.function_version !== "1") throw new Error(`function_version=${body.function_version}`);
NODE
pass "functions_deep.import_map_resolution" "alias resolved through project import map"

request_function "functions_deep.public_without_auth" "$public_function" PUT none "?mode=public" "public body $run_id" 200
node - "$ARTIFACT_DIR/functions_deep.public_without_auth.body" "$public_function" "$run_id" <<'NODE'
const fs = require("fs");
const body = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const name = process.argv[3];
const runId = process.argv[4];
if (body.method !== "PUT") throw new Error(`method=${body.method}`);
if (!(body.path.endsWith(`/functions/v1/${name}`) || body.path.endsWith(`/${name}`))) throw new Error(`path=${body.path}`);
if (body.search !== "?mode=public") throw new Error(`search=${body.search}`);
if (body.body !== `public body ${runId}`) throw new Error(`body=${body.body}`);
if (body.header !== "functions_deep.public_without_auth") throw new Error(`header=${body.header}`);
if (body.has_authorization !== false) throw new Error("unexpected authorization");
if (body.function_name !== name) throw new Error(`function_name=${body.function_name}`);
if (body.verify_jwt !== "false") throw new Error(`verify_jwt=${body.verify_jwt}`);
if (body.secret_present !== true) throw new Error("secret not injected");
NODE
pass "functions_deep.public_request_shape" "no-JWT method/body/header/env verified"

request_function "functions_deep.options_allowed" "$jwt_function" OPTIONS none "" "__NO_BODY__" 200
request_function "functions_deep.throw_returns_500" "$public_function" GET none "?throw=1" "__NO_BODY__" 500

if supadupa_cli_authed functions delete --ref "$SUPADUPA_TEST_REF" --name "$public_function" \
  >"$ARTIFACT_DIR/functions-deep-public-delete-now.out" 2>"$ARTIFACT_DIR/functions-deep-public-delete-now.stderr"; then
  pass "functions_deep.delete_public" "$public_function"
else
  fail "functions_deep.delete_public" "delete failed"
fi

request_function "functions_deep.deleted_public_404" "$public_function" GET none "" "__NO_BODY__" 404
pass "functions_deep.complete" "deploy/list/JWT/no-JWT/request-shape/error/delete checks passed"
