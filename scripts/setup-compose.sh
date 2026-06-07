#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'TXT'
usage: scripts/setup-compose.sh [options]

Generates a Supadupa .env for Docker Compose, creates runtime directories, and
creates the Traefik ingress network.

Options:
  --mode local|offline|vps      local HTTP, offline local TLS, or public VPS install (default: local)
  --domain example.com          base domain for VPS defaults
  --admin-host host             admin hostname
  --api-host host               management API hostname
  --apps-domain domain          wildcard project domain
  --dns-provider provider       acme DNS provider: cloudflare or route53 (default: cloudflare)
  --email email                 Let's Encrypt email
  --bootstrap-email email       optional first admin email
  --bootstrap-password value    optional first admin password
  --force                       overwrite existing .env
  -h, --help                    show this help

For VPS TLS, export CLOUDFLARE_API_TOKEN before running. The token is written to
.env because Traefik needs it for Let's Encrypt DNS-01. Use a scoped token with
Zone:DNS:Edit for the zone only. For Route53, export AWS_ACCESS_KEY_ID,
AWS_SECRET_ACCESS_KEY, and AWS_REGION, or rely on an instance role.
TXT
}

mode="local"
domain=""
admin_host=""
api_host=""
apps_domain=""
dns_provider=""
email=""
bootstrap_email=""
bootstrap_password=""
force=false

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --mode)
      mode="${2:-}"
      shift 2
      ;;
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
    --dns-provider)
      dns_provider="${2:-}"
      shift 2
      ;;
    --email)
      email="${2:-}"
      shift 2
      ;;
    --bootstrap-email)
      bootstrap_email="${2:-}"
      shift 2
      ;;
    --bootstrap-password)
      bootstrap_password="${2:-}"
      shift 2
      ;;
    --force)
      force=true
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

if [[ "$mode" != "local" && "$mode" != "offline" && "$mode" != "vps" ]]; then
  echo "--mode must be local, offline, or vps" >&2
  exit 2
fi

if [[ -n "$dns_provider" && "$dns_provider" != "cloudflare" && "$dns_provider" != "route53" ]]; then
  echo "--dns-provider must be cloudflare or route53" >&2
  exit 2
fi

if [[ -f .env && "$force" != "true" ]]; then
  echo ".env already exists; rerun with --force to overwrite" >&2
  exit 1
fi

if [[ "$mode" == "vps" && -z "$domain" && ( -z "$admin_host" || -z "$api_host" || -z "$apps_domain" ) ]]; then
  echo "$mode mode requires --domain or explicit --admin-host, --api-host, and --apps-domain" >&2
  exit 2
fi

if [[ -n "$domain" ]]; then
  admin_host="${admin_host:-admin.$domain}"
  api_host="${api_host:-api.$domain}"
  apps_domain="${apps_domain:-apps.$domain}"
fi

if [[ "$mode" == "local" ]]; then
  admin_host="${admin_host:-admin.supadupa.test}"
  api_host="${api_host:-api.supadupa.test}"
  apps_domain="${apps_domain:-apps.supadupa.test}"
  email="${email:-admin@example.com}"
  admin_addr="127.0.0.1:3000"
  api_addr="127.0.0.1:8080"
  vite_api_base="http://localhost:8080"
  cors_origins="http://localhost:3000,http://127.0.0.1:3000"
  tls_cert_resolver="letsencrypt"
  acme_dns_provider="${dns_provider:-cloudflare}"
  http_addr="0.0.0.0:80"
  https_addr="0.0.0.0:443"
  postgres_addr="0.0.0.0:5432"
  pooler_addr="0.0.0.0:6543"
elif [[ "$mode" == "offline" ]]; then
  admin_host="${admin_host:-admin.supadupa.test}"
  api_host="${api_host:-api.supadupa.test}"
  apps_domain="${apps_domain:-apps.supadupa.test}"
  email="${email:-admin@example.com}"
  admin_addr="127.0.0.1:3000"
  api_addr="127.0.0.1:8080"
  vite_api_base="https://$api_host"
  cors_origins="https://$admin_host"
  tls_cert_resolver=""
  acme_dns_provider="${dns_provider:-cloudflare}"
  http_addr="127.0.0.1:80"
  https_addr="127.0.0.1:443"
  postgres_addr="127.0.0.1:5432"
  pooler_addr="127.0.0.1:6543"
else
  if [[ -n "$domain" ]]; then
    email="${email:-admin@$domain}"
  else
    email="${email:-admin@example.com}"
  fi
  admin_addr="127.0.0.1:3000"
  api_addr="127.0.0.1:8080"
  vite_api_base="https://$api_host"
  cors_origins="https://$admin_host"
  tls_cert_resolver="letsencrypt"
  acme_dns_provider="${dns_provider:-cloudflare}"
  http_addr="0.0.0.0:80"
  https_addr="0.0.0.0:443"
  postgres_addr="0.0.0.0:5432"
  pooler_addr="0.0.0.0:6543"
fi

bootstrap_email="${bootstrap_email:-admin@example.test}"
bootstrap_password="${bootstrap_password:-}"
runtime_dir="$(pwd)/runtime"
secret_key="$(openssl rand -hex 32 2>/dev/null || od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
auth_secret="$(openssl rand -hex 32 2>/dev/null || od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"

mkdir -p "$runtime_dir"/{projects,routes,certs,backups}
docker network inspect supadupa-ingress >/dev/null 2>&1 || docker network create supadupa-ingress >/dev/null

generate_offline_tls() {
  local cert_dir="$runtime_dir/certs/local"
  local route_file="$runtime_dir/routes/00-local-tls.yaml"
  mkdir -p "$cert_dir" "$runtime_dir/routes"
  if [[ ! -f "$cert_dir/supadupa-local-ca.key" ]]; then
    openssl genrsa -out "$cert_dir/supadupa-local-ca.key" 4096 >/dev/null 2>&1
    openssl req -x509 -new -nodes -key "$cert_dir/supadupa-local-ca.key" -sha256 -days 3650 \
      -subj "/CN=Supadupa Local CA" \
      -out "$cert_dir/supadupa-local-ca.crt" >/dev/null 2>&1
  fi
  cat >"$cert_dir/supadupa-local.ext" <<EOF_CERT_EXT
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage=digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=@alt_names

[alt_names]
DNS.1=$admin_host
DNS.2=$api_host
DNS.3=$apps_domain
DNS.4=*.$apps_domain
EOF_CERT_EXT
  openssl genrsa -out "$cert_dir/supadupa-local.key" 2048 >/dev/null 2>&1
  openssl req -new -key "$cert_dir/supadupa-local.key" \
    -subj "/CN=$admin_host" \
    -out "$cert_dir/supadupa-local.csr" >/dev/null 2>&1
  openssl x509 -req -in "$cert_dir/supadupa-local.csr" \
    -CA "$cert_dir/supadupa-local-ca.crt" \
    -CAkey "$cert_dir/supadupa-local-ca.key" \
    -CAcreateserial \
    -out "$cert_dir/supadupa-local.crt" \
    -days 825 \
    -sha256 \
    -extfile "$cert_dir/supadupa-local.ext" >/dev/null 2>&1
  chmod 600 "$cert_dir"/*.key
  cat >"$route_file" <<EOF_LOCAL_TLS
tls:
  stores:
    default:
      defaultCertificate:
        certFile: /certs/local/supadupa-local.crt
        keyFile: /certs/local/supadupa-local.key
EOF_LOCAL_TLS
}

if [[ "$mode" == "offline" ]]; then
  if ! command -v openssl >/dev/null 2>&1; then
    echo "offline mode requires openssl for local certificate generation" >&2
    exit 1
  fi
  generate_offline_tls
fi

cat >.env <<EOF
SUPADUPA_INSTALL_MODE=$mode
SUPADUPA_ADMIN_HOST=$admin_host
SUPADUPA_API_HOST=$api_host
SUPADUPA_APPS_DOMAIN=$apps_domain
SUPADUPA_PROJECT_DOMAIN=
SUPADUPA_ACME_EMAIL=$email
SUPADUPA_TLS_CERT_RESOLVER=$tls_cert_resolver
SUPADUPA_ACME_DNS_PROVIDER=$acme_dns_provider
SUPADUPA_ACME_DNS_DELAY_BEFORE_CHECK=10

SUPADUPA_ADMIN_ADDR=$admin_addr
SUPADUPA_API_ADDR=$api_addr
VITE_API_BASE_URL=$vite_api_base
SUPADUPA_CORS_ORIGINS=$cors_origins

SUPADUPA_SECRET_KEY=$secret_key
SUPADUPA_AUTH_SECRET=$auth_secret
SUPADUPA_BOOTSTRAP_EMAIL=$bootstrap_email
SUPADUPA_BOOTSTRAP_PASSWORD=$bootstrap_password

SUPADUPA_RUNTIME_HOST_DIR=$runtime_dir
SUPADUPA_RUNTIME_CONTAINER_DIR=$runtime_dir
SUPADUPA_ROUTES_HOST_DIR=$runtime_dir/routes
SUPADUPA_CERTS_HOST_DIR=$runtime_dir/certs
SUPADUPA_PROJECT_ROOT=$runtime_dir/projects
SUPADUPA_BACKUP_ROOT=$runtime_dir/backups

SUPADUPA_PROVISIONER=compose
SUPADUPA_COMPOSE_APPLY=true

SUPADUPA_HTTP_ADDR=$http_addr
SUPADUPA_HTTPS_ADDR=$https_addr
SUPADUPA_POSTGRES_ADDR=$postgres_addr
SUPADUPA_POOLER_ADDR=$pooler_addr
CLOUDFLARE_API_TOKEN=${CLOUDFLARE_API_TOKEN:-}
AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID:-}
AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY:-}
AWS_SESSION_TOKEN=${AWS_SESSION_TOKEN:-}
AWS_REGION=${AWS_REGION:-}
AWS_PROFILE=${AWS_PROFILE:-}
AWS_SHARED_CREDENTIALS_FILE=${AWS_SHARED_CREDENTIALS_FILE:-}
AWS_HOSTED_ZONE_ID=${AWS_HOSTED_ZONE_ID:-}

SUPADUPA_BACKUP_TARGET_NAME=
SUPADUPA_BACKUP_TARGET_ENDPOINT=
SUPADUPA_BACKUP_TARGET_REGION=auto
SUPADUPA_BACKUP_TARGET_BUCKET=
SUPADUPA_BACKUP_TARGET_PREFIX=supadupa
SUPADUPA_BACKUP_TARGET_ACCESS_KEY_ID=
SUPADUPA_BACKUP_TARGET_SECRET_ACCESS_KEY=
SUPADUPA_BACKUP_TARGET_FORCE_PATH_STYLE=false
SUPADUPA_BACKUP_TARGET_AUTO_TEST=false

SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS=false
SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP=false
EOF

echo "Wrote .env"
echo "Created runtime directories under $runtime_dir"
echo "Ensured Docker network supadupa-ingress exists"
echo "Project defaults will seed from SUPADUPA_APPS_DOMAIN on first startup"
if [[ "$mode" == "offline" ]]; then
  echo "Generated local TLS CA and certificate under $runtime_dir/certs/local"
fi
echo
if [[ "$mode" == "local" ]]; then
  cat <<'TXT'
Next:
  docker compose -f deploy/compose.yaml up -d --build
  open http://localhost:3000

For project URLs with TLS, rerun setup in --mode vps and start with --profile edge.
TXT
elif [[ "$mode" == "offline" ]]; then
  cat <<TXT
Next:
  1. Configure local DNS for:
     $admin_host -> 127.0.0.1
     $api_host -> 127.0.0.1
     *.$apps_domain -> 127.0.0.1
     See: scripts/setup-local-dns.sh --domain ${domain:-supadupa.test}
  2. Trust the local CA if you want browser trust:
     $runtime_dir/certs/local/supadupa-local-ca.crt
  3. Start:
     docker compose -f deploy/compose.yaml --profile edge up -d --build
  4. Open:
     https://$admin_host
TXT
else
  cat <<TXT
Next:
  1. Create DNS records:
     $admin_host -> this server
     $api_host -> this server
     *.$apps_domain -> this server
  2. Make sure ports 80, 443, 5432, and 6543 are reachable.
  3. Start:
     docker compose -f deploy/compose.yaml --profile edge up -d --build
  4. Open:
     https://$admin_host
TXT
fi
