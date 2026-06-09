#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'TXT'
usage: scripts/setup-local-dns.sh [options]

Generates local DNS helper files for offline Supadupa routing. By default this
writes files under runtime/local-dns and does not modify the OS.

Options:
  --domain domain              base domain, default: supadupa.test
  --admin-host host            admin host, default: admin.<domain>
  --api-host host              api host, default: api.<domain>
  --apps-domain domain         project wildcard domain, default: apps.<domain>
  --address ip                 target address, default: 127.0.0.1
  --refs ref,ref               project refs for hosts-file output
  --install-dnsmasq            copy dnsmasq config to /etc/dnsmasq.d
  --install-hosts              append project-specific entries to /etc/hosts
  -h, --help                   show this help

dnsmasq supports wildcard project DNS. /etc/hosts does not support wildcards, so
--install-hosts requires --refs and writes explicit hosts for those projects.
TXT
}

domain="supadupa.test"
admin_host=""
api_host=""
apps_domain=""
address="127.0.0.1"
refs=""
install_dnsmasq=false
install_hosts=false

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --domain)
      domain="${2:-}"
      shift 2
      ;;
    --admin-host)
      admin_host="${2:-}"
      shift 2
      ;;
    --api-host)
      api_host="${2:-}"
      shift 2
      ;;
    --apps-domain)
      apps_domain="${2:-}"
      shift 2
      ;;
    --address)
      address="${2:-}"
      shift 2
      ;;
    --refs)
      refs="${2:-}"
      shift 2
      ;;
    --install-dnsmasq)
      install_dnsmasq=true
      shift
      ;;
    --install-hosts)
      install_hosts=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage
      exit 2
      ;;
  esac
done

if [[ -z "$domain" ]]; then
  echo "--domain is required" >&2
  exit 2
fi

reject_control_chars() {
  local name="$1"
  local value="${2:-}"
  if [[ "$value" == *[[:cntrl:]]* ]]; then
    echo "$name must not contain control characters" >&2
    exit 2
  fi
}

validate_hostname() {
  local name="$1"
  local value="$2"
  reject_control_chars "$name" "$value"
  if [[ -z "$value" || ${#value} -gt 253 || "$value" != *.* ]]; then
    echo "$name must be a fully qualified hostname" >&2
    exit 2
  fi
  if [[ ! "$value" =~ ^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)+$ ]]; then
    echo "$name must contain only DNS hostname characters" >&2
    exit 2
  fi
}

validate_address() {
  local value="$1"
  reject_control_chars "--address" "$value"
  if [[ "$value" == "::1" ]]; then
    return
  fi
  if [[ ! "$value" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
    echo "--address must be an IPv4 address or ::1" >&2
    exit 2
  fi
  IFS='.' read -r -a octets <<<"$value"
  for octet in "${octets[@]}"; do
    if (( octet > 255 )); then
      echo "--address octets must be between 0 and 255" >&2
      exit 2
    fi
  done
}

validate_ref() {
  local ref="$1"
  reject_control_chars "--refs" "$ref"
  if [[ ! "$ref" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ ]]; then
    echo "--refs entries must be lowercase DNS labels" >&2
    exit 2
  fi
}

admin_host="${admin_host:-admin.$domain}"
api_host="${api_host:-api.$domain}"
apps_domain="${apps_domain:-apps.$domain}"

validate_hostname "--domain" "$domain"
validate_hostname "--admin-host" "$admin_host"
validate_hostname "--api-host" "$api_host"
validate_hostname "--apps-domain" "$apps_domain"
validate_address "$address"
reject_control_chars "--refs" "$refs"
ref_array=()
if [[ -n "$refs" ]]; then
  IFS=',' read -r -a raw_ref_array <<<"$refs"
  for ref in "${raw_ref_array[@]}"; do
    ref="$(printf '%s' "$ref" | tr '[:upper:]' '[:lower:]')"
    if [[ -z "$ref" ]]; then
      continue
    fi
    validate_ref "$ref"
    ref_array+=("$ref")
  done
fi

out_dir="runtime/local-dns"
mkdir -p "$out_dir"

dnsmasq_file="$out_dir/supadupa-dnsmasq.conf"
hosts_file="$out_dir/supadupa-hosts"

cat >"$dnsmasq_file" <<EOF
# Supadupa local wildcard DNS.
address=/$admin_host/$address
address=/$api_host/$address
address=/.$apps_domain/$address
EOF

{
  echo "# Supadupa local hosts entries."
  echo "$address $admin_host"
  echo "$address $api_host"
  if [[ "${#ref_array[@]}" -gt 0 ]]; then
    for ref in "${ref_array[@]}"; do
      echo "$address $ref.$apps_domain"
      echo "$address studio-$ref.$apps_domain"
      echo "$address storage-$ref.$apps_domain"
      echo "$address db-$ref.$apps_domain"
      echo "$address pooler-$ref.$apps_domain"
    done
  else
    echo "# Add --refs smoke,alpha to generate project-specific /etc/hosts entries."
  fi
} >"$hosts_file"

echo "Wrote $dnsmasq_file"
echo "Wrote $hosts_file"

if [[ "$install_dnsmasq" == "true" ]]; then
  if [[ "$(id -u)" -ne 0 ]]; then
    echo "--install-dnsmasq must run as root" >&2
    exit 1
  fi
  install -m 0644 "$dnsmasq_file" /etc/dnsmasq.d/supadupa.conf
  echo "Installed /etc/dnsmasq.d/supadupa.conf"
  if command -v systemctl >/dev/null 2>&1; then
    systemctl restart dnsmasq || true
  fi
fi

if [[ "$install_hosts" == "true" ]]; then
  if [[ -z "$refs" ]]; then
    echo "--install-hosts requires --refs because /etc/hosts cannot wildcard project domains" >&2
    exit 2
  fi
  if [[ "$(id -u)" -ne 0 ]]; then
    echo "--install-hosts must run as root" >&2
    exit 1
  fi
  {
    echo
    echo "# Supadupa local DNS entries"
    cat "$hosts_file"
  } >>/etc/hosts
  echo "Appended Supadupa entries to /etc/hosts"
fi

cat <<EOF

Wildcard local DNS:
  Use dnsmasq with $dnsmasq_file, then make your OS resolver use dnsmasq for $domain.

Project-specific fallback:
  Use $hosts_file for explicit project refs. This does not support new refs automatically.
EOF
