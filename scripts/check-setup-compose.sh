#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

make_fake_docker() {
  local bin_dir="$1"
  cat >"$bin_dir/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "network" && "${2:-}" == "inspect" ]]; then
  exit 1
fi
if [[ "${1:-}" == "network" && "${2:-}" == "create" ]]; then
  exit 0
fi
echo "unexpected docker invocation: $*" >&2
exit 2
EOF
  chmod +x "$bin_dir/docker"
}

assert_env_mode() {
  local env_file="$1"
  local mode
  mode="$(stat -c '%a' "$env_file")"
  if [[ "$mode" != "600" ]]; then
    echo "expected $env_file mode 600, got $mode" >&2
    exit 1
  fi
}

run_expect_reject() {
  local name="$1"
  local expected="$2"
  shift 2

  local reject_dir="$work_dir/reject-$name"
  local stderr_file="$work_dir/reject-$name.stderr"
  mkdir -p "$reject_dir"
  set +e
  (
    cd "$reject_dir"
    PATH="$bin_dir:$PATH" "$@" >/dev/null 2>"$stderr_file"
  )
  local status=$?
  set -e
  if [[ "$status" -eq 0 ]]; then
    echo "$name accepted invalid input" >&2
    exit 1
  fi
  if [[ "$status" -ne 2 ]]; then
    echo "expected $name rejection exit 2, got $status" >&2
    cat "$stderr_file" >&2
    exit 1
  fi
  if [[ -f "$reject_dir/.env" ]]; then
    echo "$name wrote .env after rejecting invalid input" >&2
    exit 1
  fi
  if [[ -d "$reject_dir/runtime/local-dns" ]]; then
    echo "$name wrote local DNS files after rejecting invalid input" >&2
    exit 1
  fi
  if ! grep -q -- "$expected" "$stderr_file"; then
    echo "$name rejection message changed" >&2
    cat "$stderr_file" >&2
    exit 1
  fi
}

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT
bin_dir="$work_dir/bin"
mkdir -p "$bin_dir"
make_fake_docker "$bin_dir"

install_dir="$work_dir/install"
mkdir -p "$install_dir"
(
  cd "$install_dir"
  PATH="$bin_dir:$PATH" "$repo_root/scripts/setup-compose.sh" --mode local --force --bootstrap-password "local-test-password" >/dev/null
)
if [[ ! -f "$install_dir/.env" ]]; then
  echo "setup-compose did not create .env" >&2
  exit 1
fi
assert_env_mode "$install_dir/.env"
if ! grep -q '^SUPADUPA_BOOTSTRAP_PASSWORD=local-test-password$' "$install_dir/.env"; then
  echo "setup-compose did not write the expected bootstrap password" >&2
  exit 1
fi

vps_private_dir="$work_dir/vps-private"
mkdir -p "$vps_private_dir"
(
  cd "$vps_private_dir"
  PATH="$bin_dir:$PATH" "$repo_root/scripts/setup-compose.sh" --mode vps --domain example.com --force >/dev/null
)
if ! grep -q '^SUPADUPA_POSTGRES_ADDR=127.0.0.1:5432$' "$vps_private_dir/.env"; then
  echo "setup-compose vps mode did not keep Postgres loopback by default" >&2
  exit 1
fi
if ! grep -q '^SUPADUPA_POOLER_ADDR=127.0.0.1:6543$' "$vps_private_dir/.env"; then
  echo "setup-compose vps mode did not keep pooler loopback by default" >&2
  exit 1
fi
if ! grep -q '^SUPADUPA_DB_INGRESS_ALLOWED_CIDRS=$' "$vps_private_dir/.env"; then
  echo "setup-compose did not write database ingress allowlist metadata" >&2
  exit 1
fi

vps_exposed_dir="$work_dir/vps-exposed"
mkdir -p "$vps_exposed_dir"
(
  cd "$vps_exposed_dir"
  PATH="$bin_dir:$PATH" "$repo_root/scripts/setup-compose.sh" --mode vps --domain example.com --expose-db --force >"$work_dir/vps-exposed.out"
)
if ! grep -q '^SUPADUPA_POSTGRES_ADDR=0.0.0.0:5432$' "$vps_exposed_dir/.env"; then
  echo "setup-compose --expose-db did not publish Postgres" >&2
  exit 1
fi
if ! grep -q '^SUPADUPA_POOLER_ADDR=0.0.0.0:6543$' "$vps_exposed_dir/.env"; then
  echo "setup-compose --expose-db did not publish pooler" >&2
  exit 1
fi
if ! grep -q -- '--expose-db publishes raw Postgres and pooler ingress' "$work_dir/vps-exposed.out"; then
  echo "setup-compose --expose-db did not print database ingress warning" >&2
  exit 1
fi

run_expect_reject "compose-bootstrap-control" \
  'SUPADUPA_BOOTSTRAP_PASSWORD must not contain control characters' \
  "$repo_root/scripts/setup-compose.sh" --mode local --force --bootstrap-password $'bad\nvalue'

run_expect_reject "compose-domain-control" \
  'SUPADUPA_ADMIN_HOST must not contain control characters' \
  "$repo_root/scripts/setup-compose.sh" --mode vps --force --domain $'example.com\nSUPADUPA_AUTH_SECRET=bad'

run_expect_reject "compose-hostname-shape" \
  'SUPADUPA_ADMIN_HOST must contain only DNS hostname characters' \
  "$repo_root/scripts/setup-compose.sh" --mode local --force --admin-host 'bad_host.supadupa.test'

run_expect_reject "compose-email-shape" \
  'SUPADUPA_ACME_EMAIL must be an email address' \
  "$repo_root/scripts/setup-compose.sh" --mode local --force --email 'bad address'

run_expect_reject "compose-provider-token-control" \
  'CLOUDFLARE_API_TOKEN must not contain control characters' \
  env CLOUDFLARE_API_TOKEN=$'bad\nvalue' "$repo_root/scripts/setup-compose.sh" --mode local --force

dns_dir="$work_dir/local-dns"
mkdir -p "$dns_dir"
(
  cd "$dns_dir"
  "$repo_root/scripts/setup-local-dns.sh" --domain supadupa.test --refs alpha,BRAVO >/dev/null
)
if ! grep -q '^127.0.0.1 alpha.apps.supadupa.test$' "$dns_dir/runtime/local-dns/supadupa-hosts"; then
  echo "setup-local-dns did not write expected project host entry" >&2
  exit 1
fi
if ! grep -q '^127.0.0.1 bravo.apps.supadupa.test$' "$dns_dir/runtime/local-dns/supadupa-hosts"; then
  echo "setup-local-dns did not normalize uppercase refs" >&2
  exit 1
fi

run_expect_reject "local-dns-ref-control" \
  '--refs must not contain control characters' \
  "$repo_root/scripts/setup-local-dns.sh" --refs $'alpha\nbeta'

run_expect_reject "local-dns-ref-shape" \
  '--refs entries must be lowercase DNS labels' \
  "$repo_root/scripts/setup-local-dns.sh" --refs 'bad_ref'

run_expect_reject "local-dns-address-octet" \
  '--address octets must be between 0 and 255' \
  "$repo_root/scripts/setup-local-dns.sh" --address '999.0.0.1'

run_expect_reject "local-dns-hostname-shape" \
  '--admin-host must contain only DNS hostname characters' \
  "$repo_root/scripts/setup-local-dns.sh" --admin-host 'bad_host.supadupa.test'
