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

run_id="${SUPADUPA_COMPAT_RUN_ID:-$(date -u +%Y%m%d%H%M%S)-$$}"
suffix="$(printf '%s' "$run_id" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9-' | cut -c1-40)"
function_name="compat-isolation-${suffix:-fn}"
function_source="$ARTIFACT_DIR/isolation-function.ts"
secondary_created=false
secondary_ref="${SUPADUPA_ISOLATION_REF:-}"

wait_for_isolation_project_removed() {
  local ref="$1"
  local deadline=$((SECONDS + ${SUPADUPA_COMPAT_DESTROY_TIMEOUT_SECONDS:-180}))
  local projects_root="${SUPADUPA_RUNTIME_PROJECTS_DIR:-$REPO_ROOT/runtime/projects}"
  local routes_dir="${SUPADUPA_RUNTIME_ROUTES_DIR:-$REPO_ROOT/runtime/routes}"
  local state_file="$ARTIFACT_DIR/isolation-secondary-project-cleanup-state.txt"

  while (( SECONDS < deadline )); do
    local pending=()

    if supadupa_cli_authed projects get --ref "$ref" >"$ARTIFACT_DIR/isolation-secondary-project-get-after-destroy.json" 2>"$ARTIFACT_DIR/isolation-secondary-project-get-after-destroy.stderr"; then
      pending+=("api-metadata")
    fi
    if [[ -e "$projects_root/$ref" ]]; then
      pending+=("project-dir")
    fi
    if [[ -e "$routes_dir/$ref.yaml" ]]; then
      pending+=("route-file")
    fi
    if command -v docker >/dev/null 2>&1 &&
      docker ps -a --format '{{.Names}}' | grep -F "$ref-" >"$ARTIFACT_DIR/isolation-secondary-containers-after-destroy.txt"; then
      pending+=("containers")
    fi

    if [[ "${#pending[@]}" -eq 0 ]]; then
      printf 'removed\n' >"$state_file"
      return 0
    fi

    printf 'pending: %s\n' "${pending[*]}" >"$state_file"
    sleep 5
  done

  return 1
}

cleanup_isolation() {
  if [[ -n "${function_name:-}" ]]; then
    if [[ -n "${secondary_ref:-}" && "$secondary_ref" != "$SUPADUPA_TEST_REF" ]]; then
      supadupa_cli_authed functions delete --ref "$secondary_ref" --name "$function_name" \
        >"$ARTIFACT_DIR/isolation-secondary-function-delete.out" 2>"$ARTIFACT_DIR/isolation-secondary-function-delete.stderr" || true
    fi
    supadupa_cli_authed functions delete --ref "$SUPADUPA_TEST_REF" --name "$function_name" \
      >"$ARTIFACT_DIR/isolation-primary-function-delete.out" 2>"$ARTIFACT_DIR/isolation-primary-function-delete.stderr" || true
  fi

  if [[ "${secondary_created:-false}" == "true" && -n "${secondary_ref:-}" ]]; then
    if supadupa_cli_authed projects destroy --ref "$secondary_ref" --yes \
      >"$ARTIFACT_DIR/isolation-secondary-project-destroy.out" 2>"$ARTIFACT_DIR/isolation-secondary-project-destroy.stderr"; then
      if wait_for_isolation_project_removed "$secondary_ref"; then
        pass "isolation.second_project_cleanup" "$secondary_ref destroyed and removed"
      else
        fail "isolation.second_project_cleanup" "destroy returned but $secondary_ref still has runtime/API artifacts; see isolation-secondary-project-cleanup-state.txt"
      fi
    else
      fail "isolation.second_project_cleanup" "failed to destroy $secondary_ref; see isolation-secondary-project-destroy.stderr"
    fi
  fi
}
trap cleanup_isolation EXIT

if [[ ! -d "$SCRIPT_DIR/node_modules/@supabase/supabase-js" || ! -d "$SCRIPT_DIR/node_modules/ws" ]]; then
  require_tool npm
  if npm --prefix "$SCRIPT_DIR" install --omit=dev --no-audit --no-fund --package-lock=false \
    >"$ARTIFACT_DIR/isolation-node-install.out" 2>"$ARTIFACT_DIR/isolation-node-install.stderr"; then
    pass "isolation.node_deps.install" "compat Node dependencies installed"
  else
    fail "isolation.node_deps.install" "npm install failed; see isolation-node-install.stderr"
  fi
fi

create_isolation_project() {
  require_env SUPADUPA_TEST_ORG_ID

  if [[ -z "$secondary_ref" ]]; then
    secondary_ref="compat-iso-${suffix:-$(date -u +%H%M%S)}"
    secondary_ref="$(printf '%s' "$secondary_ref" | cut -c1-55)"
  fi
  if [[ "$secondary_ref" == "$SUPADUPA_TEST_REF" ]]; then
    fail "isolation.second_project" "isolation project ref must differ from SUPADUPA_TEST_REF"
  fi

  local existing_json="$ARTIFACT_DIR/isolation-secondary-existing.json"
  local existing_err="$ARTIFACT_DIR/isolation-secondary-existing.stderr"
  if supadupa_cli_authed projects get --ref "$secondary_ref" >"$existing_json" 2>"$existing_err"; then
    fail "isolation.second_project" "isolation project $secondary_ref already exists"
  fi

  local create_args=(
    projects create
    --org-id "$SUPADUPA_TEST_ORG_ID"
    --ref "$secondary_ref"
    --name "${SUPADUPA_ISOLATION_NAME:-Compatibility Isolation $secondary_ref}"
  )
  if [[ -n "${SUPADUPA_APPS_DOMAIN:-}" ]]; then
    create_args+=(--domain "$SUPADUPA_APPS_DOMAIN")
  fi
  if [[ -n "${SUPADUPA_ISOLATION_STACK_VERSION:-${SUPADUPA_STACK_VERSION:-}}" ]]; then
    create_args+=(--stack-version "${SUPADUPA_ISOLATION_STACK_VERSION:-${SUPADUPA_STACK_VERSION:-}}")
  fi
  if [[ -n "${SUPADUPA_ISOLATION_STACK_PROFILE:-${SUPADUPA_STACK_PROFILE:-full}}" ]]; then
    create_args+=(--profile "${SUPADUPA_ISOLATION_STACK_PROFILE:-${SUPADUPA_STACK_PROFILE:-full}}")
  fi
  if [[ -n "${SUPADUPA_ISOLATION_CPU:-${SUPADUPA_CPU:-}}" ]]; then
    create_args+=(--cpu "${SUPADUPA_ISOLATION_CPU:-${SUPADUPA_CPU:-}}")
  fi
  if [[ -n "${SUPADUPA_ISOLATION_RAM_MB:-${SUPADUPA_RAM_MB:-}}" ]]; then
    create_args+=(--ram-mb "${SUPADUPA_ISOLATION_RAM_MB:-${SUPADUPA_RAM_MB:-}}")
  fi
  if [[ -n "${SUPADUPA_ISOLATION_DISK_GB:-${SUPADUPA_DISK_GB:-}}" ]]; then
    create_args+=(--disk-gb "${SUPADUPA_ISOLATION_DISK_GB:-${SUPADUPA_DISK_GB:-}}")
  fi
  if [[ -n "${SUPADUPA_ISOLATION_HOST_ID:-${SUPADUPA_HOST_ID:-}}" ]]; then
    create_args+=(--host-id "${SUPADUPA_ISOLATION_HOST_ID:-${SUPADUPA_HOST_ID:-}}")
  fi

  if supadupa_cli_authed "${create_args[@]}" >"$ARTIFACT_DIR/isolation-secondary-create.json" 2>"$ARTIFACT_DIR/isolation-secondary-create.stderr"; then
    secondary_created=true
    pass "isolation.second_project_create" "$secondary_ref"
  else
    fail "isolation.second_project_create" "project create failed; see isolation-secondary-create.stderr"
  fi

  local deadline=$((SECONDS + ${SUPADUPA_COMPAT_CREATE_TIMEOUT_SECONDS:-360}))
  local profile_file="$ARTIFACT_DIR/isolation-$secondary_ref-profile.json"
  local profile_err="$ARTIFACT_DIR/isolation-$secondary_ref-profile.stderr"
  while (( SECONDS < deadline )); do
    if supadupa_cli_authed projects cli-profile --ref "$secondary_ref" --format json >"$profile_file" 2>"$profile_err"; then
      local api_url
      api_url="$(json_get_file_optional "$profile_file" api_url)"
      if [[ -n "$api_url" ]] &&
        curl -fsS "$api_url/auth/v1/health" >"$ARTIFACT_DIR/isolation-secondary-auth-health.json" 2>"$ARTIFACT_DIR/isolation-secondary-auth-health.stderr"; then
        pass "isolation.second_project_public_ready" "$api_url"
        return 0
      fi
    fi
    sleep 5
  done

  fail "isolation.second_project_public_ready" "isolation project did not become publicly reachable before timeout"
}

if compat_bool "${SUPADUPA_COMPAT_CREATE_ISOLATION_PROJECT:-false}"; then
  create_isolation_project
elif [[ -z "$secondary_ref" ]]; then
  projects_file="$ARTIFACT_DIR/isolation-projects.json"
  projects_err="$ARTIFACT_DIR/isolation-projects.stderr"
  if supadupa_cli_authed projects list >"$projects_file" 2>"$projects_err"; then
    secondary_ref="$(node -e '
const fs = require("fs");
const projects = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const primary = process.argv[2];
const candidate = projects.find((project) => project.ref !== primary && (!project.status || project.status === "healthy" || project.status === "degraded"));
process.stdout.write(candidate ? candidate.ref : "");
' "$projects_file" "$SUPADUPA_TEST_REF")"
  fi
fi
if [[ -z "$secondary_ref" ]]; then
  skip "isolation.second_project" "no second project configured or discoverable"
  exit 0
fi
if [[ "$secondary_ref" == "$SUPADUPA_TEST_REF" ]]; then
  fail "isolation.second_project" "SUPADUPA_ISOLATION_REF must differ from SUPADUPA_TEST_REF"
fi

require_tool psql

cat >"$function_source" <<'TS'
Deno.serve(async (req: Request) => {
  return new Response(
    JSON.stringify({
      ok: true,
      deployed_ref: Deno.env.get("DEPLOYED_REF") ?? "",
      function_name: Deno.env.get("SUPABASE_FUNCTION_NAME") ?? "",
      function_version: Deno.env.get("SUPABASE_FUNCTION_VERSION") ?? "",
      verify_jwt: Deno.env.get("VERIFY_JWT") ?? "",
      has_authorization: req.headers.has("authorization"),
    }),
    { status: 200, headers: { "Content-Type": "application/json" } },
  );
});
TS

reveal_secret_value_for_ref() {
  local ref="$1"
  local kind="$2"
  local file="$ARTIFACT_DIR/$ref-$kind.value"
  local tmp="$ARTIFACT_DIR/$ref-$kind.tmp.json"
  local err="$ARTIFACT_DIR/$ref-$kind.stderr"
  local value

  if [[ ! -s "$file" ]]; then
    if ! supadupa_cli_authed secrets reveal --ref "$ref" --kind "$kind" >"$tmp" 2>"$err"; then
      rm -f "$tmp"
      fail "secret.$ref.$kind" "secret reveal failed; see $(basename "$err")"
    fi
    value="$(json_get_file "$tmp" value 2>/dev/null || true)"
    rm -f "$tmp"
    if [[ -z "$value" ]]; then
      fail "secret.$ref.$kind" "secret reveal response was empty"
    fi
    write_secret_file "$file" "$value"
  fi

  read_secret_file "$file"
}

deploy_isolation_function() {
  local ref="$1"
  local label="$2"
  local out="$ARTIFACT_DIR/isolation-$label-function-deploy.json"
  local err="$ARTIFACT_DIR/isolation-$label-function-deploy.stderr"

  if supadupa_cli_authed functions deploy \
    --ref "$ref" \
    --name "$function_name" \
    --entrypoint index.ts \
    --source-file "$function_source" \
    --verify-jwt=true \
    --secret "DEPLOYED_REF=$ref" \
    >"$out" 2>"$err"; then
    node - "$out" "$function_name" <<'NODE'
const fs = require("fs");
const body = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const name = process.argv[3];
if (body.name !== name) throw new Error(`name=${body.name}`);
if (body.verify_jwt !== true) throw new Error(`verify_jwt=${body.verify_jwt}`);
if (body.status !== "deployed") throw new Error(`status=${body.status}`);
if (!Number.isInteger(body.version) || body.version < 1) throw new Error(`version=${body.version}`);
NODE
    pass "isolation.function_deploy_$label" "$ref/$function_name"
  else
    fail "isolation.function_deploy_$label" "deploy failed; see $(basename "$err")"
  fi
}

request_isolation_function() {
  local test_name="$1"
  local api_url="$2"
  local key="$3"
  local expected_status="$4"
  local out="$ARTIFACT_DIR/$test_name.body"
  local headers="$ARTIFACT_DIR/$test_name.headers"
  local err="$ARTIFACT_DIR/$test_name.stderr"
  local status

  set +e
  status="$(curl -sS -D "$headers" -o "$out" -w '%{http_code}' \
    -H "apikey: $key" \
    -H "Authorization: Bearer $key" \
    "$api_url/functions/v1/$function_name" 2>"$err")"
  local rc="$?"
  set -e
  if [[ "$rc" -ne 0 ]]; then status="000"; fi
  if [[ "$status" != "$expected_status" ]]; then
    fail "$test_name" "expected HTTP $expected_status, got HTTP $status"
  fi
  pass "$test_name" "HTTP $status"
}

request_isolation_realtime() {
  local test_name="$1"
  local realtime_url="$2"
  local key="$3"
  local expect="$4"
  local out="$ARTIFACT_DIR/$test_name.out"
  local err="$ARTIFACT_DIR/$test_name.stderr"

  if SUPADUPA_REALTIME_URL="$realtime_url" \
    SUPADUPA_REALTIME_KEY="$key" \
    SUPADUPA_REALTIME_EXPECT="$expect" \
    node "$SCRIPT_DIR/realtime-probe.cjs" >"$out" 2>"$err"; then
    if [[ "$expect" == "accept" ]]; then
      pass "$test_name" "websocket opened"
    else
      local status
      status="$(tr -d '\r\n' <"$out")"
      pass "$test_name" "HTTP ${status:-403}"
    fi
  else
    fail "$test_name" "Realtime websocket probe failed; see $(basename "$err")"
  fi
}

primary_anon="$(reveal_secret_value anon_key)"
secondary_anon="$(reveal_secret_value_for_ref "$secondary_ref" anon_key)"
primary_db_password="$(reveal_secret_value db_password)"
primary_s3_access_key="$(reveal_secret_value s3_access_key)"
primary_s3_secret_key="$(reveal_secret_value s3_secret_key)"
primary_connect="$ARTIFACT_DIR/isolation-$SUPADUPA_TEST_REF-connect.json"
primary_connect_err="$ARTIFACT_DIR/isolation-$SUPADUPA_TEST_REF-connect.stderr"
secondary_connect="$ARTIFACT_DIR/isolation-$secondary_ref-connect.json"
secondary_connect_err="$ARTIFACT_DIR/isolation-$secondary_ref-connect.stderr"
secondary_profile="$ARTIFACT_DIR/isolation-$secondary_ref-profile.json"
secondary_profile_err="$ARTIFACT_DIR/isolation-$secondary_ref-profile.stderr"
token="$(read_secret_file "$ARTIFACT_DIR/token")"

if ! supadupa_cli --token "$token" projects connect \
  --ref "$SUPADUPA_TEST_REF" >"$primary_connect" 2>"$primary_connect_err"; then
  fail "isolation.primary_connect" "failed to fetch primary connect payload; see primary connect stderr"
fi
if ! supadupa_cli --token "$token" projects connect \
  --ref "$secondary_ref" >"$secondary_connect" 2>"$secondary_connect_err"; then
  fail "isolation.secondary_connect" "failed to fetch secondary connect payload; see secondary connect stderr"
fi
if ! supadupa_cli --token "$token" projects cli-profile \
  --ref "$secondary_ref" \
  --format json >"$secondary_profile" 2>"$secondary_profile_err"; then
  fail "isolation.second_project" "failed to fetch secondary project profile; see isolation profile stderr"
fi
pass "isolation.second_project" "$secondary_ref"

if node -e '
const fs = require("fs");
const primary = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
const secondary = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const primaryRef = process.argv[3];
const secondaryRef = process.argv[4];
const appsDomain = process.env.SUPADUPA_APPS_DOMAIN || "";

function assert(condition, message) {
  if (!condition) throw new Error(message);
}
function valueAt(object, path) {
  return path.split(".").reduce((current, key) => current && current[key], object);
}
function assertProject(payload, ref) {
  const publicDBHost = `db-${ref}.${appsDomain}`;
  const publicPoolerHost = `pooler-${ref}.${appsDomain}`;
  assert(String(payload.api_url || "").startsWith("https://"), `${ref} api_url is not HTTPS`);
  assert(String(payload.studio_url || "").startsWith("https://"), `${ref} studio_url is not HTTPS`);
  if (appsDomain) {
    assert(payload.api_url === `https://${ref}.${appsDomain}`, `${ref} api_url is not project-scoped: ${payload.api_url}`);
    assert(payload.studio_url === `https://studio-${ref}.${appsDomain}`, `${ref} studio_url is not project-scoped: ${payload.studio_url}`);
    for (const [path, host] of Object.entries({
      "postgres.public_direct": publicDBHost,
      "postgres.public_transaction": publicPoolerHost,
      "postgres.public_session": publicPoolerHost,
      "postgres_parts.public_direct.host": publicDBHost,
      "postgres_parts.public_transaction.host": publicPoolerHost,
      "postgres_parts.public_session.host": publicPoolerHost,
    })) {
      const value = valueAt(payload, path);
      assert(String(value || "").includes(host), `${ref} ${path} does not include ${host}: ${value}`);
    }
  }
  for (const [key, value] of Object.entries(payload.connection_snippets || {})) {
    if (key.includes("internal") || key.includes("local")) continue;
    assert(!/(localhost|127\.0\.0\.1|host\.docker\.internal|\.internal)(?::|\/|$)/.test(String(value)), `${ref} public snippet ${key} is not remote-safe: ${value}`);
  }
  for (const [kind, handle] of Object.entries(payload.secret_handles || {})) {
    assert(String(handle).includes(`/projects/${ref}/`), `${ref} secret handle ${kind} is not project-scoped: ${handle}`);
  }
}
assertProject(primary, primaryRef);
assertProject(secondary, secondaryRef);
for (const path of [
  "api_url",
  "studio_url",
  "postgres.public_direct",
  "postgres.public_transaction",
  "storage_s3_url",
]) {
  assert(valueAt(primary, path) !== valueAt(secondary, path), `${path} is shared across projects`);
}
' "$primary_connect" "$secondary_connect" "$SUPADUPA_TEST_REF" "$secondary_ref" >"$ARTIFACT_DIR/isolation-connect.out" 2>"$ARTIFACT_DIR/isolation-connect.stderr"; then
  pass "isolation.remote_connect_surfaces" "$SUPADUPA_TEST_REF and $secondary_ref have distinct public endpoints"
else
  fail "isolation.remote_connect_surfaces" "connect payload separation failed; see isolation-connect.stderr"
fi

routes_dir="${SUPADUPA_RUNTIME_ROUTES_DIR:-$REPO_ROOT/runtime/routes}"
if [[ -f "$routes_dir/$SUPADUPA_TEST_REF.yaml" && -f "$routes_dir/$secondary_ref.yaml" ]]; then
  if node -e '
const fs = require("fs");
const routesDir = process.argv[1];
const primaryRef = process.argv[2];
const secondaryRef = process.argv[3];
const appsDomain = process.env.SUPADUPA_APPS_DOMAIN || "";

function assert(condition, message) {
  if (!condition) throw new Error(message);
}
function checkRoute(ref, otherRef) {
  const body = fs.readFileSync(`${routesDir}/${ref}.yaml`, "utf8");
  if (appsDomain) {
    for (const expected of [
      `Host(\`${ref}.${appsDomain}\`)`,
      `Host(\`studio-${ref}.${appsDomain}\`)`,
      `HostSNI(\`db-${ref}.${appsDomain}\`)`,
      `HostSNI(\`pooler-${ref}.${appsDomain}\`)`,
    ]) {
      assert(body.includes(expected), `${ref} route file missing ${expected}`);
    }
    for (const unexpected of [
      `${otherRef}.${appsDomain}`,
      `studio-${otherRef}.${appsDomain}`,
      `db-${otherRef}.${appsDomain}`,
      `pooler-${otherRef}.${appsDomain}`,
    ]) {
      assert(!body.includes(unexpected), `${ref} route file references ${unexpected}`);
    }
  }
  assert(body.includes(`service: ${ref}-api`), `${ref} route missing own API service`);
  assert(body.includes(`service: ${ref}-studio`), `${ref} route missing own Studio service`);
  assert(!body.includes(`service: ${otherRef}-api`), `${ref} route references other API service`);
  assert(!body.includes(`service: ${otherRef}-studio`), `${ref} route references other Studio service`);
}
checkRoute(primaryRef, secondaryRef);
checkRoute(secondaryRef, primaryRef);
' "$routes_dir" "$SUPADUPA_TEST_REF" "$secondary_ref" >"$ARTIFACT_DIR/isolation-routes.out" 2>"$ARTIFACT_DIR/isolation-routes.stderr"; then
    pass "isolation.route_files_separate" "$SUPADUPA_TEST_REF and $secondary_ref route files are host/service separated"
  else
    fail "isolation.route_files_separate" "route file isolation failed; see isolation-routes.stderr"
  fi
else
  skip "isolation.route_files_separate" "runtime route files not available"
fi

if command -v docker >/dev/null 2>&1; then
  docker_ps_file="$ARTIFACT_DIR/isolation-docker-ps.tsv"
  docker_network_file="$ARTIFACT_DIR/isolation-docker-networks.jsonl"
  docker ps --format '{{.Names}}\t{{.Networks}}\t{{.Ports}}' >"$docker_ps_file"
  {
    docker network inspect "${SUPADUPA_TEST_REF}_internal" --format '{{json .}}'
    docker network inspect "${secondary_ref}_internal" --format '{{json .}}'
    docker network inspect supadupa-ingress --format '{{json .}}'
  } >"$docker_network_file" 2>"$ARTIFACT_DIR/isolation-docker-networks.stderr"

  if node -e '
const fs = require("fs");
const psFile = process.argv[1];
const networkFile = process.argv[2];
const primaryRef = process.argv[3];
const secondaryRef = process.argv[4];

function assert(condition, message) {
  if (!condition) throw new Error(message);
}
function projectPattern(ref) {
  return new RegExp(`(^|_)${ref}-`);
}
function projectOf(name) {
  if (projectPattern(primaryRef).test(name)) return primaryRef;
  if (projectPattern(secondaryRef).test(name)) return secondaryRef;
  return "";
}
const rows = fs.readFileSync(psFile, "utf8").trim().split(/\n/).filter(Boolean).map((line) => {
  const [name, networks = "", ports = ""] = line.split("\t");
  return { name, networks: networks.split(",").filter(Boolean), ports };
});
const projectRows = rows.filter((row) => projectOf(row.name));
assert(projectRows.some((row) => projectOf(row.name) === primaryRef), `${primaryRef} has no running containers`);
assert(projectRows.some((row) => projectOf(row.name) === secondaryRef), `${secondaryRef} has no running containers`);
for (const row of projectRows) {
  const ref = projectOf(row.name);
  const otherRef = ref === primaryRef ? secondaryRef : primaryRef;
  assert(row.networks.includes(`${ref}_internal`), `${row.name} is not attached to ${ref}_internal`);
  assert(!row.networks.includes(`${otherRef}_internal`), `${row.name} is attached to ${otherRef}_internal`);
  assert(!row.ports.includes("->"), `${row.name} has host port mappings: ${row.ports}`);
}

const networks = fs.readFileSync(networkFile, "utf8").trim().split(/\n/).filter(Boolean).map((line) => JSON.parse(line));
const byName = new Map(networks.map((network) => [network.Name, network]));
for (const ref of [primaryRef, secondaryRef]) {
  const otherRef = ref === primaryRef ? secondaryRef : primaryRef;
  const network = byName.get(`${ref}_internal`);
  assert(network, `${ref}_internal network missing`);
  const names = Object.values(network.Containers || {}).map((container) => container.Name);
  assert(names.some((name) => projectPattern(ref).test(name)), `${ref}_internal has no ${ref} containers`);
  assert(!names.some((name) => projectPattern(otherRef).test(name)), `${ref}_internal contains ${otherRef} containers`);
}
const ingress = byName.get("supadupa-ingress");
assert(ingress, "supadupa-ingress network missing");
const ingressNames = Object.values(ingress.Containers || {}).map((container) => container.Name);
for (const ref of [primaryRef, secondaryRef]) {
  const own = ingressNames.filter((name) => projectPattern(ref).test(name));
  const allowed = new Set([`${ref}-kong-1`, `${ref}-studio-1`, `${ref}-db-1`, `${ref}-pooler-1`]);
  assert(own.length > 0, `${ref} has no ingress-facing containers`);
  for (const name of own) {
    assert(allowed.has(name), `${name} is unexpectedly attached to supadupa-ingress`);
  }
}
' "$docker_ps_file" "$docker_network_file" "$SUPADUPA_TEST_REF" "$secondary_ref" >"$ARTIFACT_DIR/isolation-runtime.out" 2>"$ARTIFACT_DIR/isolation-runtime.stderr"; then
    pass "isolation.runtime_networks_separate" "$SUPADUPA_TEST_REF and $secondary_ref containers use separate internal networks"
  else
    fail "isolation.runtime_networks_separate" "runtime network isolation failed; see isolation-runtime.stderr"
  fi
else
  skip "isolation.runtime_networks_separate" "docker not available"
fi

secondary_api_url="$(json_get_file "$secondary_profile" api_url)"
primary_api_url="$(profile_value api_url)"
primary_realtime_url="$(profile_value_optional realtime_url)"
secondary_realtime_url="$(json_get_file_optional "$secondary_profile" realtime_url)"
secondary_db_url="$(json_get_file "$secondary_profile" public_database_url)"
secondary_s3_url="$(json_get_file "$secondary_profile" storage_s3_url)"
secondary_db_safe_url="$(url_without_password "$secondary_db_url")"

if [[ -z "$primary_realtime_url" || -z "$secondary_realtime_url" ]]; then
  skip "isolation.realtime_cross_project_rejected" "Realtime URL unavailable in one or both project profiles"
else
  request_isolation_realtime "isolation.realtime_primary_key_primary_project" "$primary_realtime_url" "$primary_anon" accept
  request_isolation_realtime "isolation.realtime_secondary_key_secondary_project" "$secondary_realtime_url" "$secondary_anon" accept
  request_isolation_realtime "isolation.realtime_secondary_key_primary_project_rejected" "$primary_realtime_url" "$secondary_anon" reject
  request_isolation_realtime "isolation.realtime_primary_key_secondary_project_rejected" "$secondary_realtime_url" "$primary_anon" reject
fi

deploy_isolation_function "$SUPADUPA_TEST_REF" primary
deploy_isolation_function "$secondary_ref" secondary
request_isolation_function "isolation.function_primary_key_primary_project" "$primary_api_url" "$primary_anon" 200
request_isolation_function "isolation.function_secondary_key_secondary_project" "$secondary_api_url" "$secondary_anon" 200
request_isolation_function "isolation.function_secondary_key_primary_project_rejected" "$primary_api_url" "$secondary_anon" 401
request_isolation_function "isolation.function_primary_key_secondary_project_rejected" "$secondary_api_url" "$primary_anon" 401
node - "$ARTIFACT_DIR/isolation.function_primary_key_primary_project.body" "$ARTIFACT_DIR/isolation.function_secondary_key_secondary_project.body" "$SUPADUPA_TEST_REF" "$secondary_ref" "$function_name" <<'NODE'
const fs = require("fs");
const primary = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const secondary = JSON.parse(fs.readFileSync(process.argv[3], "utf8"));
const primaryRef = process.argv[4];
const secondaryRef = process.argv[5];
const functionName = process.argv[6];
function assert(condition, message) {
  if (!condition) throw new Error(message);
}
assert(primary.ok === true, "primary function did not return ok");
assert(secondary.ok === true, "secondary function did not return ok");
assert(primary.deployed_ref === primaryRef, `primary deployed_ref=${primary.deployed_ref}`);
assert(secondary.deployed_ref === secondaryRef, `secondary deployed_ref=${secondary.deployed_ref}`);
assert(primary.function_name === functionName, `primary function_name=${primary.function_name}`);
assert(secondary.function_name === functionName, `secondary function_name=${secondary.function_name}`);
assert(primary.verify_jwt === "true", `primary verify_jwt=${primary.verify_jwt}`);
assert(secondary.verify_jwt === "true", `secondary verify_jwt=${secondary.verify_jwt}`);
NODE
pass "isolation.function_same_name_project_scoped" "$function_name resolves separately in both projects"

set +e
cross_rest_status="$(curl -sS -o "$ARTIFACT_DIR/isolation-cross-rest.body" -w '%{http_code}' \
  -H "apikey: $primary_anon" \
  "$secondary_api_url/rest/v1/" 2>"$ARTIFACT_DIR/isolation-cross-rest.stderr")"
cross_rest_rc="$?"
set -e
if [[ "$cross_rest_rc" -ne 0 ]]; then
  cross_rest_status="000"
fi
case "$cross_rest_status" in
  401|403) pass "isolation.anon_key_cross_project_rejected" "HTTP $cross_rest_status" ;;
  *) fail "isolation.anon_key_cross_project_rejected" "expected 401 or 403, got HTTP $cross_rest_status" ;;
esac

set +e
PGPASSWORD="$primary_db_password" psql "$secondary_db_safe_url" \
  -v ON_ERROR_STOP=1 \
  -Atqc "select current_database()" \
  >"$ARTIFACT_DIR/isolation-cross-db.out" 2>"$ARTIFACT_DIR/isolation-cross-db.stderr"
cross_db_rc="$?"
set -e
if [[ "$cross_db_rc" -eq 0 ]]; then
  fail "isolation.db_password_cross_project_rejected" "primary project DB password connected to secondary project"
fi
pass "isolation.db_password_cross_project_rejected" "psql rejected cross-project password"

if [[ -z "$primary_s3_access_key" || -z "$primary_s3_secret_key" || -z "$secondary_s3_url" ]]; then
  skip "isolation.s3_credentials_cross_project_rejected" "S3 credential material or secondary endpoint is unavailable"
elif SUPABASE_S3_ENDPOINT="$secondary_s3_url" \
  SUPABASE_S3_ACCESS_KEY="$primary_s3_access_key" \
  SUPABASE_S3_SECRET_KEY="$primary_s3_secret_key" \
  node "$SCRIPT_DIR/s3-reject-probe.mjs" \
  >"$ARTIFACT_DIR/isolation-cross-s3.out" 2>"$ARTIFACT_DIR/isolation-cross-s3.stderr"; then
  pass "isolation.s3_credentials_cross_project_rejected" "primary S3 credentials rejected by $secondary_ref endpoint"
else
  fail "isolation.s3_credentials_cross_project_rejected" "cross-project S3 credentials were accepted or probe failed; see isolation-cross-s3.stderr"
fi
