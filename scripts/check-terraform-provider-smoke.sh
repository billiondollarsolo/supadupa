#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
TERRAFORM_BIN="${TERRAFORM_BIN:-terraform}"

if ! command -v "$TERRAFORM_BIN" >/dev/null 2>&1; then
  echo "terraform is not installed; set TERRAFORM_BIN to a Terraform binary" >&2
  exit 1
fi

tmp="$(mktemp -d)"
server_pid=""

cleanup() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" >/dev/null 2>&1 || true
    wait "$server_pid" >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

plugin_dir="$tmp/plugins"
work_dir="$tmp/work"
server_dir="$tmp/server"
mkdir -p "$plugin_dir" "$work_dir" "$server_dir"

go build -o "$plugin_dir/terraform-provider-supadupa" "$ROOT/cmd/terraform-provider-supadupa"

cat >"$server_dir/fake_management_api.py" <<'PY'
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

project = None
domains = []


class Handler(BaseHTTPRequestHandler):
    def _write(self, status, payload=None):
        self.send_response(status)
        if payload is not None:
            data = json.dumps(payload).encode("utf-8")
            self.send_header("content-type", "application/json")
            self.send_header("content-length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            return
        self.send_header("content-length", "0")
        self.end_headers()

    def _log_request(self):
        with open(os.environ["SMOKE_LOG"], "a", encoding="utf-8") as handle:
            handle.write(f"{self.command} {self.path}\n")

    def _require_auth(self):
        if self.headers.get("Authorization") != "Bearer smoke-token":
            self._write(401, {"error": "missing or invalid bearer token"})
            return False
        return True

    def _read_json(self):
        length = int(self.headers.get("content-length") or "0")
        if length <= 0:
            return {}
        return json.loads(self.rfile.read(length))

    def do_POST(self):
        global project, domains
        self._log_request()
        if not self._require_auth():
            return
        if self.path == "/v1/orgs/org_1/projects":
            got = self._read_json()
            expected = {
                "ref": "alpha",
                "name": "Alpha",
                "domain": "apps.example.test",
                "stack_version": "2026.06.01",
                "profile": "full",
                "resource_tier": "medium",
            }
            for key, value in expected.items():
                if got.get(key) != value:
                    self._write(400, {"error": f"unexpected {key}: {got.get(key)!r}"})
                    return
            project = {
                "id": "proj_1",
                "org_id": "org_1",
                "ref": "alpha",
                "name": "Alpha",
                "status": "creating",
                "spec": {
                    "domain": "apps.example.test",
                    "stack_version": "2026.06.01",
                    "profile": "full",
                    "resource_tier": "medium",
                },
            }
            self._write(201, project)
            return
        if self.path == "/v1/projects/alpha/domains":
            got = self._read_json()
            if got.get("fqdn") != "api.alpha.example.test":
                self._write(400, {"error": f"unexpected fqdn: {got.get('fqdn')!r}"})
                return
            domain = {
                "id": "domain_1",
                "project_ref": "alpha",
                "fqdn": "api.alpha.example.test",
                "cert_status": "issued",
                "cert_mode": "managed",
            }
            domains = [domain]
            self._write(201, domain)
            return
        self._write(404, {"error": "not found"})

    def do_GET(self):
        self._log_request()
        if not self._require_auth():
            return
        if self.path == "/v1/projects/alpha":
            if project is None:
                self._write(404, {"error": "not found"})
                return
            healthy = dict(project)
            healthy["status"] = "healthy"
            self._write(200, healthy)
            return
        if self.path == "/v1/projects/alpha/domains":
            self._write(200, domains)
            return
        self._write(404, {"error": "not found"})

    def do_DELETE(self):
        global project, domains
        self._log_request()
        if not self._require_auth():
            return
        if self.path == "/v1/projects/alpha":
            project = None
            self._write(204)
            return
        if self.path == "/v1/projects/alpha/domains/api.alpha.example.test":
            domains = []
            self._write(204)
            return
        self._write(404, {"error": "not found"})

    def log_message(self, fmt, *args):
        return


server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
with open(os.environ["SMOKE_URL"], "w", encoding="utf-8") as handle:
    handle.write(f"http://127.0.0.1:{server.server_address[1]}\n")
server.serve_forever()
PY

export SMOKE_URL="$server_dir/url"
export SMOKE_LOG="$server_dir/requests.log"
: >"$SMOKE_LOG"
python3 "$server_dir/fake_management_api.py" &
server_pid="$!"

for _ in {1..100}; do
  if [[ -s "$SMOKE_URL" ]]; then
    break
  fi
  sleep 0.05
done
if [[ ! -s "$SMOKE_URL" ]]; then
  echo "fake Management API did not start" >&2
  exit 1
fi
api_url="$(<"$SMOKE_URL")"

cat >"$tmp/terraformrc" <<EOF
provider_installation {
  dev_overrides {
    "supadupa/supadupa" = "$plugin_dir"
  }
  direct {}
}
EOF

cat >"$work_dir/main.tf" <<EOF
terraform {
  required_providers {
    supadupa = {
      source = "supadupa/supadupa"
    }
  }
}

provider "supadupa" {
  api_url = "$api_url"
  token   = "smoke-token"
}

resource "supadupa_project" "alpha" {
  org_id        = "org_1"
  ref           = "alpha"
  name          = "Alpha"
  domain        = "apps.example.test"
  stack_version = "2026.06.01"
  profile       = "full"
  resource_tier = "medium"
}

resource "supadupa_project_domain" "api" {
  ref  = supadupa_project.alpha.ref
  fqdn = "api.alpha.example.test"
}
EOF

export TF_CLI_CONFIG_FILE="$tmp/terraformrc"
export CHECKPOINT_DISABLE=1
export TF_IN_AUTOMATION=1

"$TERRAFORM_BIN" -chdir="$work_dir" apply -auto-approve -input=false

set +e
"$TERRAFORM_BIN" -chdir="$work_dir" plan -detailed-exitcode -input=false
plan_status="$?"
set -e
if [[ "$plan_status" -ne 0 ]]; then
  echo "terraform no-op plan returned $plan_status; expected 0" >&2
  exit 1
fi

"$TERRAFORM_BIN" -chdir="$work_dir" destroy -auto-approve -input=false

for expected in \
  "POST /v1/orgs/org_1/projects" \
  "GET /v1/projects/alpha" \
  "POST /v1/projects/alpha/domains" \
  "GET /v1/projects/alpha/domains" \
  "DELETE /v1/projects/alpha/domains/api.alpha.example.test" \
  "DELETE /v1/projects/alpha"; do
  if ! grep -Fqx "$expected" "$SMOKE_LOG"; then
    echo "expected fake Management API request missing: $expected" >&2
    echo "seen requests:" >&2
    cat "$SMOKE_LOG" >&2
    exit 1
  fi
done
