#!/usr/bin/env bash
set -euo pipefail
umask 077

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
  --bootstrap-password value    optional first admin password; prefer SUPADUPA_BOOTSTRAP_PASSWORD
  --db-loopback                 keep Postgres/pooler edge ports on 127.0.0.1 (default)
  --db-public-bind              bind Postgres/pooler edge ports to 0.0.0.0; pair with firewall rules
  --expose-db                   deprecated alias for --db-public-bind
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
db_public_bind=false
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
    --expose-db)
      # Deprecated compatibility alias for older install docs/scripts.
      db_public_bind=true
      shift
      ;;
    --db-public-bind)
      db_public_bind=true
      shift
      ;;
    --db-loopback)
      db_public_bind=false
      shift
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

validate_email() {
  local name="$1"
  local value="$2"
  reject_control_chars "$name" "$value"
  if [[ -z "$value" || "$value" == *" "* || "$value" != *@*.* ]]; then
    echo "$name must be an email address" >&2
    exit 2
  fi
}

validate_env_value() {
  reject_control_chars "$1" "${2:-}"
}

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
  http_addr="127.0.0.1:80"
  https_addr="127.0.0.1:443"
  postgres_addr="127.0.0.1:5432"
  pooler_addr="127.0.0.1:6543"
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
  # Keep raw database/pooler entrypoints loopback by default. Operators who need
  # external raw Postgres/pooler clients can opt in explicitly and should pair
  # that with host/provider firewall rules plus project allowlists.
  postgres_addr="127.0.0.1:5432"
  pooler_addr="127.0.0.1:6543"
  if [[ "$db_public_bind" == "true" ]]; then
    postgres_addr="0.0.0.0:5432"
    pooler_addr="0.0.0.0:6543"
  fi
fi

bootstrap_email="${bootstrap_email:-admin@example.test}"
bootstrap_password="${bootstrap_password:-${SUPADUPA_BOOTSTRAP_PASSWORD:-}}"
validate_hostname "SUPADUPA_ADMIN_HOST" "$admin_host"
validate_hostname "SUPADUPA_API_HOST" "$api_host"
validate_hostname "SUPADUPA_APPS_DOMAIN" "$apps_domain"
validate_email "SUPADUPA_ACME_EMAIL" "$email"
validate_email "SUPADUPA_BOOTSTRAP_EMAIL" "$bootstrap_email"
validate_env_value "SUPADUPA_BOOTSTRAP_PASSWORD" "$bootstrap_password"
for name in \
  CLOUDFLARE_API_TOKEN \
  AWS_ACCESS_KEY_ID \
  AWS_SECRET_ACCESS_KEY \
  AWS_SESSION_TOKEN \
  AWS_REGION \
  AWS_PROFILE \
  AWS_SHARED_CREDENTIALS_FILE \
  AWS_HOSTED_ZONE_ID; do
  validate_env_value "$name" "${!name:-}"
done
runtime_dir="$(pwd)/runtime"
host_uid="$(id -u)"
host_gid="$(id -g)"
control_plane_user="$host_uid:$host_gid"
if [[ "$host_uid" == "0" ]]; then
  control_plane_user="10001:10001"
fi
docker_gid="0"
if [[ -S /var/run/docker.sock ]]; then
  if detected_gid="$(stat -c '%g' /var/run/docker.sock 2>/dev/null || stat -f '%g' /var/run/docker.sock 2>/dev/null)"; then
    if [[ "$detected_gid" =~ ^[0-9]+$ ]]; then
      docker_gid="$detected_gid"
    fi
  fi
fi
docker_proxy_user="$control_plane_user"
if [[ "$(uname -s)" == "Darwin" ]]; then
  # Docker Desktop/Rancher Desktop expose a VM-owned socket inside containers;
  # the host socket group is not a reliable group_add value from macOS.
  docker_proxy_user="0:0"
fi
build_sha="$(git -C "$(dirname "$0")/.." rev-parse --short HEAD 2>/dev/null || echo unknown)"
secret_key="$(openssl rand -hex 32 2>/dev/null || od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
auth_secret="$(openssl rand -hex 32 2>/dev/null || od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"

mkdir -p "$runtime_dir"/{projects,routes,certs,backups}
if [[ "$host_uid" == "0" ]]; then
  chown -R 10001:10001 "$runtime_dir"
fi
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

generate_platform_routes() {
  local route_file="$runtime_dir/routes/00-platform.yaml"
  local api_wildcard="*.${api_host#*.}"
  local admin_wildcard="*.${admin_host#*.}"
  {
    cat <<EOF_PLATFORM_API
http:
  routers:
    supadupa-api:
      rule: Host(\`$api_host\`)
      entryPoints:
        - websecure
      service: supadupa-api
EOF_PLATFORM_API
    if [[ -n "$tls_cert_resolver" ]]; then
      cat <<EOF_PLATFORM_API_TLS
      tls:
        certResolver: $tls_cert_resolver
        domains:
          - main: "$api_wildcard"
EOF_PLATFORM_API_TLS
    else
      cat <<'EOF_PLATFORM_API_TLS'
      tls: {}
EOF_PLATFORM_API_TLS
    fi
    cat <<EOF_PLATFORM_ROUTES
    supadupa-api-http:
      rule: Host(\`$api_host\`)
      entryPoints:
        - web
      middlewares:
        - supadupa-api-https
      service: supadupa-api
    supadupa-admin:
      rule: Host(\`$admin_host\`)
      entryPoints:
        - websecure
      service: supadupa-admin
EOF_PLATFORM_ROUTES
    if [[ -n "$tls_cert_resolver" ]]; then
      cat <<EOF_PLATFORM_ADMIN_TLS
      tls:
        certResolver: $tls_cert_resolver
        domains:
          - main: "$admin_wildcard"
EOF_PLATFORM_ADMIN_TLS
    else
      cat <<'EOF_PLATFORM_ADMIN_TLS'
      tls: {}
EOF_PLATFORM_ADMIN_TLS
    fi
    cat <<EOF_PLATFORM_REST
    supadupa-admin-http:
      rule: Host(\`$admin_host\`)
      entryPoints:
        - web
      middlewares:
        - supadupa-admin-https
      service: supadupa-admin
  services:
    supadupa-api:
      loadBalancer:
        servers:
          - url: http://supadupavisor:8080
    supadupa-admin:
      loadBalancer:
        servers:
          - url: http://admin-ui:8080
  middlewares:
    supadupa-api-https:
      redirectScheme:
        scheme: https
    supadupa-admin-https:
      redirectScheme:
        scheme: https
EOF_PLATFORM_REST
  } >"$route_file"
}

if [[ "$mode" == "offline" ]]; then
  if ! command -v openssl >/dev/null 2>&1; then
    echo "offline mode requires openssl for local certificate generation" >&2
    exit 1
  fi
  generate_offline_tls
fi
generate_platform_routes

env_tmp="$(mktemp .env.XXXXXX)"
trap 'rm -f "$env_tmp"' EXIT
cat >"$env_tmp" <<EOF
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
SUPADUPA_META_DB_ADDR=127.0.0.1:15432
VITE_API_BASE_URL=$vite_api_base
SUPADUPA_CORS_ORIGINS=$cors_origins

SUPADUPA_SECRET_KEY=$secret_key
SUPADUPA_AUTH_SECRET=$auth_secret
SUPADUPA_BOOTSTRAP_EMAIL=$bootstrap_email
SUPADUPA_BOOTSTRAP_PASSWORD=$bootstrap_password

SUPADUPA_RUNTIME_HOST_DIR=$runtime_dir
SUPADUPA_RUNTIME_CONTAINER_DIR=/app/runtime
SUPADUPA_ROUTES_HOST_DIR=$runtime_dir/routes
SUPADUPA_CERTS_HOST_DIR=$runtime_dir/certs
SUPADUPA_PROJECT_HOST_ROOT=$runtime_dir/projects
SUPADUPA_CONTROL_PLANE_USER=$control_plane_user
SUPADUPA_DOCKER_PROXY_USER=$docker_proxy_user
SUPADUPA_DOCKER_GID=$docker_gid
SUPADUPA_BUILD_SHA=$build_sha

SUPADUPA_PROVISIONER=compose
SUPADUPA_COMPOSE_APPLY=true
SUPADUPA_PROJECT_DOCKER_LOGS=false

SUPADUPA_HTTP_ADDR=$http_addr
SUPADUPA_HTTPS_ADDR=$https_addr
SUPADUPA_POSTGRES_ADDR=$postgres_addr
SUPADUPA_POOLER_ADDR=$pooler_addr
SUPADUPA_DB_INGRESS_ALLOWED_CIDRS=
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
chmod 600 "$env_tmp"
mv "$env_tmp" .env
trap - EXIT

echo "Wrote .env"
echo "Created runtime directories under $runtime_dir"
echo "Ensured Docker network supadupa-ingress exists"
echo "Configured control-plane container user $control_plane_user and Docker proxy socket group $docker_gid"

# Warn (do not block) when the host has less RAM than a single full-profile
# project realistically needs (~4 GB) plus the control plane (~0.5 GB). The full
# profile includes Logflare/analytics (Elixir/BEAM), which is OOM-killed on
# small hosts and leaves the project "degraded" — the #1 first-run surprise.
mem_kb="$(awk '/^MemTotal:/{print $2}' /proc/meminfo 2>/dev/null || echo 0)"
mem_gb=$(( mem_kb / 1024 / 1024 ))
if [[ "$mem_kb" -gt 0 && "$mem_gb" -lt 4 ]]; then
  echo
  echo "Warning: this host has ~${mem_gb} GB RAM. A single full-profile project needs ~4 GB"
  echo "(Logflare/analytics alone wants ~1 GB) plus ~0.5 GB for the control plane. On less,"
  echo "the analytics container is OOM-killed and the project reports 'degraded'. Use a larger"
  echo "host, or create projects with analytics disabled. See docs/install.md (Resource Requirements)."
fi
echo "Project defaults will seed from SUPADUPA_APPS_DOMAIN on first startup"
if [[ "$mode" != "local" && "$mode" != "offline" ]]; then
  echo
  if [[ "$db_public_bind" != "true" ]]; then
    echo "Database/pooler edge ports bound to loopback ($postgres_addr / $pooler_addr); external DB clients cannot reach them."
    echo "Use --db-public-bind only when external raw Postgres/pooler clients are required."
  else
    echo "Note: Postgres/pooler edge ports publish on $postgres_addr / $pooler_addr."
    echo "Reachability is gated by Traefik — the platform database_external_access flag (default off) and each"
    echo "project's db_ingress_mode (default private) must both be enabled before any external client connects."
    echo "Set per-project db_allowlist (or SUPADUPA_DB_INGRESS_ALLOWED_CIDRS) to trusted client networks, and keep any"
    echo "host/provider firewall aligned. Use --db-loopback to bind these ports to 127.0.0.1 instead."
  fi
fi
if [[ "$mode" == "offline" ]]; then
  echo "Generated local TLS CA and certificate under $runtime_dir/certs/local"
fi
echo
if [[ "$mode" == "local" ]]; then
  cat <<'TXT'
Next:
  docker compose -f deploy/compose.yaml -f deploy/compose.apply.yaml up -d --build
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
     docker compose -f deploy/compose.yaml -f deploy/compose.apply.yaml --profile edge up -d --build
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
  2. Make sure ports 80 and 443 are reachable.
     Postgres/pooler publish on 5432 and 6543 (Traefik-gated, default-private per project); when you enable
     external DB access, also allow 5432/6543 at any host/provider firewall and restrict to trusted client IPs.
  3. Start:
     docker compose -f deploy/compose.yaml -f deploy/compose.apply.yaml --profile edge up -d --build
  4. Open:
     https://$admin_host
TXT
fi
