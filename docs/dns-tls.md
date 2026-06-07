# DNS And TLS

Supadupa uses separate DNS patterns for the control plane and generated projects.

Recommended layout:

```text
admin.example.com       control-plane admin UI
api.example.com         control-plane management API
*.apps.example.com      generated project surfaces
```

For project `smoke`, generated routes look like:

```text
https://smoke.apps.example.com             project API
https://studio-smoke.apps.example.com      Studio
https://storage-smoke.apps.example.com     Storage S3
db-smoke.apps.example.com:5432             direct Postgres
pooler-smoke.apps.example.com:6543         transaction/session pooler
```

## DNS Records

For VPS mode, point these records to the server:

```text
admin.example.com       A/AAAA  <server-ip>
api.example.com         A/AAAA  <server-ip>
*.apps.example.com      A/AAAA  <server-ip>
```

You can also use a nested layout:

```text
admin.supadupa.example.com
api.supadupa.example.com
*.apps.supadupa.example.com
```

Run setup with explicit hosts:

```bash
export CLOUDFLARE_API_TOKEN='...'
scripts/setup-compose.sh \
  --mode vps \
  --admin-host admin.supadupa.example.com \
  --api-host api.supadupa.example.com \
  --apps-domain apps.supadupa.example.com \
  --email ops@example.com \
  --bootstrap-password 'change-this-password'
```

## Traefik Edge

The edge profile runs Traefik and publishes:

```text
80      HTTP redirect and ACME HTTP reachability
443     HTTPS control-plane and project HTTP surfaces
5432    TLS-routed direct Postgres
6543    TLS-routed pooler
```

Start with:

```bash
docker compose -f deploy/compose.yaml --profile edge up -d --build
```

Only Traefik publishes public ports. Project containers should remain unhosted on the public interface.

Traefik uses:

- Docker provider for labeled control-plane services.
- File provider for Supadupa-rendered per-project routes in `runtime/routes`.
- `exposedByDefault=false`, so unlabeled containers are not routed automatically.

## Let's Encrypt DNS-01

The Traefik DNS provider is selected with:

```text
SUPADUPA_ACME_DNS_PROVIDER=cloudflare
```

or:

```text
SUPADUPA_ACME_DNS_PROVIDER=route53
```

The cert resolver name is:

```text
SUPADUPA_TLS_CERT_RESOLVER=letsencrypt
```

Set `SUPADUPA_TLS_CERT_RESOLVER=` only for offline/local fake certificates.

## Cloudflare

Traefik uses Cloudflare DNS-01:

```text
CLOUDFLARE_API_TOKEN=<token>
SUPADUPA_ACME_EMAIL=ops@example.com
```

The token must be present in `.env` before starting the edge profile. Use a scoped Cloudflare token with DNS edit permission only for the required zone.

Check certificate issuance:

```bash
docker compose -f deploy/compose.yaml --profile edge logs -f edge-router
```

## Route53

Use Route53 DNS-01 with:

```text
SUPADUPA_ACME_DNS_PROVIDER=route53
AWS_ACCESS_KEY_ID=<access-key>
AWS_SECRET_ACCESS_KEY=<secret-key>
AWS_REGION=us-east-1
```

Optional values:

```text
AWS_SESSION_TOKEN=
AWS_PROFILE=
AWS_SHARED_CREDENTIALS_FILE=
AWS_HOSTED_ZONE_ID=
```

On AWS infrastructure, you can use an instance role instead of static access keys if Traefik can read the role credentials.

Run setup:

```bash
scripts/setup-compose.sh \
  --mode vps \
  --dns-provider route53 \
  --domain example.com \
  --email ops@example.com \
  --bootstrap-password 'change-this-password'
```

Start:

```bash
docker compose -f deploy/compose.yaml --profile edge up -d --build
```

## Offline Local DNS And Certificates

For non-public local routing, use offline mode:

```bash
scripts/setup-compose.sh --mode offline --bootstrap-password 'change-this-password'
scripts/setup-local-dns.sh --domain supadupa.test
docker compose -f deploy/compose.yaml --profile edge up -d --build
```

This generates:

```text
runtime/certs/local/supadupa-local-ca.crt
runtime/certs/local/supadupa-local.crt
runtime/certs/local/supadupa-local.key
runtime/routes/00-local-tls.yaml
runtime/local-dns/supadupa-dnsmasq.conf
runtime/local-dns/supadupa-hosts
```

The local server certificate covers:

```text
admin.supadupa.test
api.supadupa.test
apps.supadupa.test
*.apps.supadupa.test
```

Use dnsmasq for wildcard project DNS. `/etc/hosts` can only handle explicit project refs.

## Wildcard Certificates

For the MVP path, Traefik requests certificates as routes are used. You do not need to manually upload wildcard certificates.

If you do bring your own certificates, use the project custom domain/certificate UI. Supadupa stores certificate artifacts under:

```text
runtime/certs
```

Route artifacts are written under:

```text
runtime/routes
```

## Custom Domains

For a project custom domain:

1. Point the custom FQDN to the Supadupa edge host.
2. Add the custom domain in the project config UI.
3. Let managed TLS issue, or upload a BYO certificate/key pair.

Supadupa rejects domains that collide with platform hosts or generated project hosts.

## Generated Project Domain Defaults

`SUPADUPA_APPS_DOMAIN` seeds the default project domain on a fresh/default control plane. Example:

```text
SUPADUPA_APPS_DOMAIN=apps.example.com
```

If the Admin UI has already changed the domain, `SUPADUPA_APPS_DOMAIN` will not overwrite it. Set this for an explicit env override:

```text
SUPADUPA_PROJECT_DOMAIN=apps.example.com
```

## Validation

After startup:

```bash
curl -fsS https://api.example.com/v1/health
curl -I https://admin.example.com
curl -I https://smoke.apps.example.com
```

For direct DB routes, use the project `Connect` page for the exact URL and password.
