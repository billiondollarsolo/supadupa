# Install

Supadupa supports two MVP install modes:

- Local loopback: control plane and admin UI on local ports, useful for evaluation and development.
- Offline local edge: local DNS plus generated development certificates, useful when not publicly online.
- VPS with public edge: control plane, admin UI, and project routes served through Traefik with Cloudflare DNS-01 certificates.

The install helper writes `.env`, creates runtime directories, and ensures the `supadupa-ingress` Docker network exists.

## Requirements

Local loopback:

- Linux or macOS with Docker.
- Docker Compose v2.
- Go 1.23+ if running binaries natively.
- Node 22+ if running the admin UI natively.

VPS:

- Linux VPS with a public IPv4 or IPv6 address.
- Docker and Docker Compose v2.
- DNS control for the domain.
- Cloudflare DNS API token for Let's Encrypt DNS-01.
- Inbound ports `80`, `443`, `5432`, and `6543` open.

## Local Loopback Install

Run:

```bash
scripts/setup-compose.sh --mode local --bootstrap-password 'change-this-password'
docker compose -f deploy/compose.yaml up -d --build
```

Open:

```text
http://localhost:3000
```

Login with the bootstrap email/password in `.env`. The default bootstrap email is `admin@example.test` unless `--bootstrap-email` was provided.

Stop:

```bash
docker compose -f deploy/compose.yaml down
```

Local loopback does not provide public project TLS routes. Use VPS mode for real DNS and certificates.

## Offline Local Edge Install

This mode is for running the edge stack without public DNS or Let's Encrypt. It generates a local CA and server certificate, disables the ACME resolver, and binds edge ports to loopback.

```bash
scripts/setup-compose.sh --mode offline --bootstrap-password 'change-this-password'
scripts/setup-local-dns.sh --domain supadupa.test
docker compose -f deploy/compose.yaml --profile edge up -d --build
```

Configure your OS resolver to use the generated dnsmasq config, or use explicit `/etc/hosts` entries for the projects you create:

```bash
scripts/setup-local-dns.sh --domain supadupa.test --refs smoke
```

Trust the generated local CA if you want browser trust:

```text
runtime/certs/local/supadupa-local-ca.crt
```

Open:

```text
https://admin.supadupa.test
```

See [Offline Local Edge](offline-local.md) for DNS details and limitations.

## VPS Install

Example domain:

```text
example.com
```

Create a Cloudflare API token scoped to the zone with DNS edit permission, then run:

```bash
export CLOUDFLARE_API_TOKEN='...'
scripts/setup-compose.sh \
  --mode vps \
  --domain example.com \
  --email ops@example.com \
  --bootstrap-email admin@example.com \
  --bootstrap-password 'change-this-password'
```

For Route53, use `--dns-provider route53` and provide AWS credentials or an instance role:

```bash
export AWS_ACCESS_KEY_ID='...'
export AWS_SECRET_ACCESS_KEY='...'
export AWS_REGION='us-east-1'
scripts/setup-compose.sh \
  --mode vps \
  --dns-provider route53 \
  --domain example.com \
  --email ops@example.com \
  --bootstrap-email admin@example.com \
  --bootstrap-password 'change-this-password'
```

Create DNS records pointing at the VPS:

```text
admin.example.com       A/AAAA  <server-ip>
api.example.com         A/AAAA  <server-ip>
*.apps.example.com      A/AAAA  <server-ip>
```

Start:

```bash
docker compose -f deploy/compose.yaml --profile edge up -d --build
```

Open:

```text
https://admin.example.com
```

## Setup Script Options

```text
--mode local|vps
--domain example.com
--admin-host host
--api-host host
--apps-domain domain
--email email
--bootstrap-email email
--bootstrap-password value
--force
```

For `--mode vps`, pass either `--domain` or all of `--admin-host`, `--api-host`, and `--apps-domain`.

## Generated Environment

The helper writes `.env` with:

- `SUPADUPA_ADMIN_HOST`: admin UI host.
- `SUPADUPA_API_HOST`: management API host.
- `SUPADUPA_APPS_DOMAIN`: wildcard project domain.
- `SUPADUPA_PROJECT_DOMAIN`: explicit override for project default domain when set.
- `SUPADUPA_TLS_CERT_RESOLVER`: Traefik cert resolver. Empty in offline mode.
- `SUPADUPA_ACME_DNS_PROVIDER`: `cloudflare` or `route53` for Let's Encrypt DNS-01.
- `VITE_API_BASE_URL`: API URL compiled into the admin UI.
- `SUPADUPA_CORS_ORIGINS`: origins allowed to call the API.
- `SUPADUPA_SECRET_KEY` and `SUPADUPA_AUTH_SECRET`: generated control-plane secrets.
- `SUPADUPA_RUNTIME_HOST_DIR`: host-side runtime directory.
- `SUPADUPA_COMPOSE_APPLY=true`: allows the control plane to apply project Compose stacks.
- `CLOUDFLARE_API_TOKEN`: used by Traefik DNS-01 when the provider is Cloudflare.
- `AWS_*`: used by Traefik DNS-01 when the provider is Route53.

On first startup, `SUPADUPA_APPS_DOMAIN` seeds the project default domain if the stored default is still `supadupa.test`. Set `SUPADUPA_PROJECT_DOMAIN` when the environment file should explicitly override the Admin UI default on restart.

## First Login Checklist

After login:

1. Open `Settings -> Defaults`.
2. Confirm project domain, stack version, stack profile, resource tier, and backup schedule.
3. Open `Settings -> Backups`.
4. Add and test an off-host S3/R2 target for production-like recovery.
5. Create an organization if one does not already exist.
6. Create a project.
7. Use the project `Connect` page for keys and URLs.
8. Open Studio from the project page.

## Updating An Existing Install

Pull or update the code, then rebuild:

```bash
docker compose -f deploy/compose.yaml --profile edge up -d --build
```

The control-plane API runs meta database migrations on startup and reconciles persisted project routes/runtime artifacts.
