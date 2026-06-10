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
- Go 1.25+ if running binaries natively.
- Node `^20.19.0` or `>=22.12.0` if running the admin UI natively.

VPS:

- Linux VPS with a public IPv4 or IPv6 address.
- Docker and Docker Compose v2.
- DNS control for the domain.
- Cloudflare DNS API token for Let's Encrypt DNS-01.
- Inbound ports `80` and `443` open. Open `5432` and `6543` only when intentionally exposing public DB/pooler routes.

## Local Loopback Install

Run:

```bash
export SUPADUPA_BOOTSTRAP_PASSWORD='change-this-password'
scripts/setup-compose.sh --mode local
docker compose -f deploy/compose.yaml -f deploy/compose.apply.yaml up -d --build
```

Open:

```text
http://localhost:3000
```

Login with the bootstrap email/password in `.env`. The default bootstrap email is `admin@example.test` unless `--bootstrap-email` was provided.

Stop:

```bash
docker compose -f deploy/compose.yaml -f deploy/compose.apply.yaml down
```

Local loopback does not provide public project TLS routes. Use VPS mode for real DNS and certificates.

## Offline Local Edge Install

This mode is for running the edge stack without public DNS or Let's Encrypt. It generates a local CA and server certificate, disables the ACME resolver, and binds edge ports to loopback.

```bash
export SUPADUPA_BOOTSTRAP_PASSWORD='change-this-password'
scripts/setup-compose.sh --mode offline
scripts/setup-local-dns.sh --domain supadupa.test
docker compose -f deploy/compose.yaml -f deploy/compose.apply.yaml --profile edge up -d --build
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
export SUPADUPA_BOOTSTRAP_PASSWORD='change-this-password'
scripts/setup-compose.sh \
  --mode vps \
  --domain example.com \
  --email ops@example.com \
  --bootstrap-email admin@example.com
```

Add `--expose-db` only when this host should publish direct Postgres and pooler routes on `5432` and `6543`.

For Route53, use `--dns-provider route53` and provide AWS credentials or an instance role:

```bash
export AWS_ACCESS_KEY_ID='...'
export AWS_SECRET_ACCESS_KEY='...'
export AWS_REGION='us-east-1'
export SUPADUPA_BOOTSTRAP_PASSWORD='change-this-password'
scripts/setup-compose.sh \
  --mode vps \
  --dns-provider route53 \
  --domain example.com \
  --email ops@example.com \
  --bootstrap-email admin@example.com
```

Create DNS records pointing at the VPS:

```text
admin.example.com       A/AAAA  <server-ip>
api.example.com         A/AAAA  <server-ip>
*.apps.example.com      A/AAAA  <server-ip>
```

Start:

```bash
docker compose -f deploy/compose.yaml -f deploy/compose.apply.yaml --profile edge up -d --build
```

Open:

```text
https://admin.example.com
```

## Setup Script Options

```text
--mode local|offline|vps
--domain example.com
--admin-host host
--api-host host
--apps-domain domain
--email email
--bootstrap-email email
--bootstrap-password value
--expose-db
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
- `SUPADUPA_ENABLE_PLATFORM_SSO_JSON_ADAPTER`: absent defaults to `false`; set manually only for development or controlled compatibility use of the normalized JSON SSO adapter, not for production SAML XML validation.
- `SUPADUPA_META_DB_ADDR`: loopback metadata database bind address, defaulting to `127.0.0.1:15432`.
- `SUPADUPA_POSTGRES_ADDR` and `SUPADUPA_POOLER_ADDR`: loopback by default; set through `--expose-db` for intentional public DB/pooler exposure.
- `SUPADUPA_DB_INGRESS_ALLOWED_CIDRS`: optional comma-separated trusted client CIDRs used for database-ingress posture reporting and Traefik TCP allowlist middleware when raw DB/pooler ports are public.
- `SUPADUPA_RUNTIME_HOST_DIR`: host-side runtime directory mounted into the control plane.
- `SUPADUPA_RUNTIME_CONTAINER_DIR`: container-side runtime mount path where the control plane writes project files, routes, certs, and backups.
- `SUPADUPA_PROJECT_HOST_ROOT`: host-side project directory used in generated per-project Compose bind mounts because those sources are resolved by the host Docker daemon.
- `SUPADUPA_CONTROL_PLANE_USER`: numeric `uid:gid` used by the supadupavisor container; generated from the user running `setup-compose.sh` so runtime bind mounts stay writable without running the process as root.
- `SUPADUPA_DOCKER_GID`: host Docker socket group ID. The `docker-socket-proxy` service ships in the base compose and runs as root there; the apply overlay (`deploy/compose.apply.yaml`) overrides it to run unprivileged with this group ID added so it can reach the socket without root.
- `SUPADUPA_COMPOSE_APPLY=true`: allows the control plane to apply project Compose stacks through the internal Docker API proxy when started with `deploy/compose.apply.yaml`.
- `SUPADUPA_PROJECT_DOCKER_LOGS=false`: keeps generated project Vector services from mounting the host Docker socket; set true only for legacy Docker-log drain compatibility.
- `CLOUDFLARE_API_TOKEN`: used by Traefik DNS-01 when the provider is Cloudflare.
- `AWS_*`: used by Traefik DNS-01 when the provider is Route53.

The helper writes `.env` with mode `0600` and rejects newline/control-character input before writing secret-bearing files. Host and email options are validated before runtime directories or Docker networks are created.
For manual `.env` files, set `SUPADUPA_CONTROL_PLANE_USER` to a numeric user/group that can write `SUPADUPA_RUNTIME_HOST_DIR`; if `SUPADUPA_COMPOSE_APPLY=true`, use an absolute `SUPADUPA_RUNTIME_HOST_DIR`, keep `SUPADUPA_RUNTIME_CONTAINER_DIR` as a writable container path such as `/app/runtime`, set `SUPADUPA_PROJECT_HOST_ROOT` to the host-side projects directory, start with `-f deploy/compose.apply.yaml`, and set `SUPADUPA_DOCKER_GID` to `stat -c '%g' /var/run/docker.sock`. The control-plane container never mounts the Docker socket; only the isolated `docker-socket-proxy` service does (in the base compose file), and the control plane reaches it over `DOCKER_HOST=tcp://docker-socket-proxy:2375`.

On first startup, `SUPADUPA_APPS_DOMAIN` seeds the project default domain if the stored default is still `supadupa.test`. Set `SUPADUPA_PROJECT_DOMAIN` when the environment file should explicitly override the Admin UI default on restart.

## Database Ingress

Normal Supabase-style applications use the public HTTPS project routes for Auth, REST, GraphQL, Storage, Realtime, and Edge Functions. Those routes remain public when direct database ingress is private.

Raw Postgres clients, migration tools, ORMs, database GUIs, and pooler clients use separate TCP ingress on `5432` and `6543`. Compose keeps those ports on loopback by default. Use an SSH tunnel for private operator access, or run setup with `--expose-db` only when external database clients need direct access.

When `--expose-db` is used, restrict `5432` and `6543` to trusted client networks with host firewall/provider firewall rules. Configure the same trusted CIDRs in `Settings -> Database Ingress`; saving that setting rewrites existing project route manifests so Traefik's file provider reloads TCP `ipAllowList` middleware without restarting the platform. `SUPADUPA_DB_INGRESS_ALLOWED_CIDRS` remains an initial `.env` default for first boot and diagnostics.

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
docker compose -f deploy/compose.yaml -f deploy/compose.apply.yaml --profile edge up -d --build
```

The control-plane API runs meta database migrations on startup and reconciles persisted project routes/runtime artifacts.
