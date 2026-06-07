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

admin_host="${admin_host:-admin.$domain}"
api_host="${api_host:-api.$domain}"
apps_domain="${apps_domain:-apps.$domain}"

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
  if [[ -n "$refs" ]]; then
    IFS=',' read -r -a ref_array <<<"$refs"
    for ref in "${ref_array[@]}"; do
      ref="$(echo "$ref" | tr '[:upper:]' '[:lower:]' | xargs)"
      if [[ -z "$ref" ]]; then
        continue
      fi
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
