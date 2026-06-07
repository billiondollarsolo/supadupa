# Security

Supadupa's MVP security model relies on the control plane owning project lifecycle, Traefik owning public ingress, and project containers staying off host-published public ports.

## Secrets

Required control-plane secrets:

```text
SUPADUPA_SECRET_KEY
SUPADUPA_AUTH_SECRET
```

Generate strong values and keep them stable. Do not rotate them casually without understanding encrypted payload/session impact.

Bootstrap credentials:

```text
SUPADUPA_BOOTSTRAP_EMAIL
SUPADUPA_BOOTSTRAP_PASSWORD
```

These create the first admin only when no users exist. Remove them from `.env` after initial setup if your operational policy requires it.

## Persistent Secret Encryption

The control plane supports local persistent encryption and optional external mechanisms through environment such as:

```text
SUPADUPA_KMS_PROVIDER
SUPADUPA_KMS_COMMAND
SUPADUPA_VAULT_KEY_FILE
```

For MVP installs, keep `SUPADUPA_SECRET_KEY` protected and stable. For production hardening, use an external key provider or a protected vault key file.

## Network Exposure

Public ports should be:

```text
80
443
5432
6543
```

Traefik publishes those ports. Project containers should not be directly host-published. Public access flows through generated routes and TLS.

Control-plane containers publish loopback ports by default in VPS mode:

```text
SUPADUPA_ADMIN_ADDR=127.0.0.1:3000
SUPADUPA_API_ADDR=127.0.0.1:8080
```

Traefik serves the public admin/API hosts.

## Admin And API Access

The admin UI calls the management API through `VITE_API_BASE_URL`. CORS is controlled by:

```text
SUPADUPA_CORS_ORIGINS
```

For VPS mode this should include the public admin origin, for example:

```text
SUPADUPA_CORS_ORIGINS=https://admin.example.com
```

## Studio Access

Studio is exposed per project, for example:

```text
https://studio-smoke.apps.example.com
```

Studio access is mediated by Supadupa control-plane login. The Supadupa project admin page and Studio serve different purposes and can coexist:

- Supadupa controls lifecycle, backups, upgrades, routes, access, logs, and operational health.
- Studio controls Supabase project internals such as database, auth, storage, SQL editor, and table editor.

## Backup Credentials

S3/R2 credentials should be scoped to the backup bucket/prefix where possible. Treat these as production secrets:

- Access key ID.
- Secret access key.
- Bucket name and endpoint where sensitive.
- Backup artifacts.

## Cloudflare Credentials

`CLOUDFLARE_API_TOKEN` is written to `.env` for Traefik DNS-01. Use a scoped token with DNS edit permission only for the required zone.

## Traefik Dashboard

Do not expose the Traefik dashboard publicly. The Compose file defaults to dashboard disabled/insecure false. If you enable it for debugging, bind it to loopback or protect it.

## Project Keys

Treat these as secrets:

- Service role key.
- Database password.
- SCIM tokens.
- Webhook secrets.
- Auth provider client secrets.
- Function secrets.

Anon keys are public by design, but policies and row-level security must still be configured correctly in the project.

## Production Hardening Checklist

1. Strong stable control-plane secrets.
2. Bootstrap credentials removed or rotated after first admin.
3. Cloudflare token scoped to one zone.
4. S3/R2 token scoped to backup bucket/prefix.
5. Traefik dashboard disabled or protected.
6. Project containers not publicly host-published.
7. Off-host backup target tested.
8. Durable upgrade backup enforcement enabled.
9. Audit log reviewed.
10. Compatibility tests run against a disposable project.
