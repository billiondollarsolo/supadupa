#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"
compat_init

require_env SUPADUPA_TEST_REF
require_tool docker
require_tool node

ports_file="$ARTIFACT_DIR/public-exposure-docker-ports.tsv"
docker ps --format '{{.Names}}\t{{.Ports}}' >"$ports_file"

if node -e '
const fs = require("fs");
const ref = process.env.SUPADUPA_TEST_REF;
const file = process.argv[1];
const rows = fs.readFileSync(file, "utf8").trim().split(/\n/).filter(Boolean).map((line) => {
  const [name, ...rest] = line.split("\t");
  return { name, ports: rest.join("\t") };
});
const offenders = rows.filter((row) => row.name.startsWith(`${ref}-`) && row.ports.includes("->"));
if (offenders.length) {
  console.error(offenders.map((row) => `${row.name}\t${row.ports}`).join("\n"));
  process.exit(1);
}
' "$ports_file" >"$ARTIFACT_DIR/public-exposure-project.out" 2>"$ARTIFACT_DIR/public-exposure-project.stderr"; then
  pass "public_exposure.project_containers_not_published" "$SUPADUPA_TEST_REF containers have no host port mappings"
else
  fail "public_exposure.project_containers_not_published" "project containers expose host ports; see public-exposure-project.stderr"
fi

if node -e '
const fs = require("fs");
const file = process.argv[1];
const allowedPorts = new Set((process.env.SUPADUPA_PUBLIC_EDGE_PORTS || "80,443,5432,6543").split(",").map((value) => value.trim()).filter(Boolean));
const allowedName = new RegExp(process.env.SUPADUPA_PUBLIC_EDGE_CONTAINER_PATTERN || "(edge|router|traefik)");
const rows = fs.readFileSync(file, "utf8").trim().split(/\n/).filter(Boolean).map((line) => {
  const [name, ...rest] = line.split("\t");
  return { name, ports: rest.join("\t") };
});
const publicMappings = [];
const offenders = [];
for (const row of rows) {
  const mappings = row.ports.split(",").map((value) => value.trim()).filter((value) => value.includes("->"));
  for (const mapping of mappings) {
    const match = mapping.match(/^(?:0\.0\.0\.0|\[::\]|::):(\d+)->/);
    if (!match) continue;
    const port = match[1];
    publicMappings.push(`${row.name}:${port}`);
    if (!allowedPorts.has(port) || !allowedName.test(row.name)) {
      offenders.push(`${row.name}\t${mapping}`);
    }
  }
}
if (publicMappings.length === 0) {
  console.error("no public edge mappings found");
  process.exit(2);
}
if (offenders.length) {
  console.error(offenders.join("\n"));
  process.exit(1);
}
console.log(publicMappings.sort().join(", "));
' "$ports_file" >"$ARTIFACT_DIR/public-exposure-edge.out" 2>"$ARTIFACT_DIR/public-exposure-edge.stderr"; then
  edge_summary="$(cat "$ARTIFACT_DIR/public-exposure-edge.out")"
  pass "public_exposure.only_edge_public_ports" "$edge_summary"
else
  fail "public_exposure.only_edge_public_ports" "unexpected public host port mappings; see public-exposure-edge.stderr"
fi

if node -e '
const fs = require("fs");
const file = process.argv[1];
const rows = fs.readFileSync(file, "utf8").trim().split(/\n/).filter(Boolean).map((line) => {
  const [name, ...rest] = line.split("\t");
  return { name, ports: rest.join("\t") };
});
const loopbackMappings = [];
for (const row of rows) {
  const mappings = row.ports.split(",").map((value) => value.trim()).filter((value) => value.includes("->"));
  for (const mapping of mappings) {
    const match = mapping.match(/^127\.0\.0\.1:(\d+)->/);
    if (match) loopbackMappings.push(`${row.name}:${match[1]}`);
  }
}
console.log(loopbackMappings.sort().join(", ") || "none");
' "$ports_file" >"$ARTIFACT_DIR/public-exposure-loopback.out" 2>"$ARTIFACT_DIR/public-exposure-loopback.stderr"; then
  loopback_summary="$(cat "$ARTIFACT_DIR/public-exposure-loopback.out")"
  pass "public_exposure.loopback_only_support_ports" "$loopback_summary"
else
  fail "public_exposure.loopback_only_support_ports" "failed to inspect loopback mappings; see public-exposure-loopback.stderr"
fi
