# Security

Supadupa's MVP security model relies on the control plane owning project lifecycle, Traefik owning public ingress, and project containers staying off host-published public ports.

## Secrets

Required control-plane secrets:

```text
SUPADUPA_SECRET_KEY
SUPADUPA_AUTH_SECRET
```

Generate strong values and keep them stable. Startup rejects missing values, known development placeholders, and short values unless `SUPADUPA_ALLOW_DEV_SECRETS=true` is explicitly set for local development. Do not rotate these secrets casually without understanding encrypted payload/session impact.

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

Default public ports should be:

```text
80
443
```

Traefik publishes those ports. Project containers should not be directly host-published. Public access flows through generated routes and TLS.

## Project Network Isolation

Under the Compose provisioner each project runs on its own dedicated Docker network (`<ref>-edge`) plus a project-private `internal` network. Only that project's edge-facing services and the single shared edge-router join `<ref>-edge`, so one project can never reach another project's containers (including its Postgres). The edge-router is the only component bridging projects, and it routes by per-project DNS aliases (`<ref>-kong`, `<ref>-db`). The control plane creates/attaches/removes these networks over the docker-socket-proxy, which only permits the named edge-router container to attach to project-labeled networks.

The Kubernetes provisioner does **not** yet enforce equivalent isolation — all projects share one namespace with no `NetworkPolicy`. Treat the Kubernetes path as single-tenant until namespace-per-project + default-deny `NetworkPolicy` lands; see "Project Network Isolation" in `docs/kubernetes.md`.

Direct Postgres and pooler edge routes use `5432` and `6543`, but Compose setup keeps those bindings on loopback unless `--expose-db` or explicit `SUPADUPA_POSTGRES_ADDR` / `SUPADUPA_POOLER_ADDR` values are used. Private database ingress does not block normal app access through public HTTPS Auth, REST, GraphQL, Storage, Realtime, or Functions routes.

When raw database ingress is public, treat it as a separate production exposure decision. Restrict `5432` and `6543` to trusted client networks with host firewall or provider firewall rules, and configure the same CIDRs in `Settings -> Database Ingress`. Saving that UI setting persists the allowlist, rewrites existing project route manifests, and lets Traefik's file provider reload TCP `ipAllowList` middleware without a container restart.

Control-plane containers publish loopback ports by default in VPS mode:

```text
SUPADUPA_ADMIN_ADDR=127.0.0.1:3000
SUPADUPA_API_ADDR=127.0.0.1:8080
SUPADUPA_META_DB_ADDR=127.0.0.1:15432
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

Browser admin auth uses an HttpOnly `supadupa_session` cookie with SameSite=Lax; Secure is set for HTTPS or proxied requests. Cookie-authenticated mutating requests require an allowed `Origin`; bearer-token API clients are not required to send browser origins.

## Passwords And Session Credentials

New platform user passwords are stored as `bcrypt-sha256$...` hashes: the submitted password is SHA-256 prehashed before bcrypt so long admin passwords are handled consistently by bcrypt's input limit. Legacy `sha256$...` password hashes continue to verify during migration and are rehashed to the new format after successful login or explicit password update. User creation and password update reject short passwords, common development placeholders, leading/trailing whitespace, and control characters.

Browser sessions are cookie based. The admin UI should not store reusable bearer tokens in `localStorage` or `sessionStorage`; the static frontend checks guard that behavior.

## Platform SSO

The current platform SSO endpoint is a compatibility/development adapter, not a complete SAML implementation. It accepts a normalized signed JSON assertion and validates issuer, audience, email domain, expiry, role binding, and the configured certificate signature over that normalized payload. It does not parse a real `SAMLResponse` XML document or validate XML signature transforms.

Enabling platform SSO now requires:

```text
SUPADUPA_ENABLE_PLATFORM_SSO_JSON_ADAPTER=true
```

Use that flag only for local development or controlled compatibility tests. Production deployments should keep it false until platform SSO is backed by real SAML XML parsing and signature validation, or place this adapter behind a trusted IdP bridge that performs that validation before forwarding the normalized assertion.

## Studio Access

Studio is exposed per project, for example:

```text
https://studio-smoke.apps.example.com
```

Studio access is mediated by Supadupa control-plane login. The Admin UI requests short-lived one-time Studio codes; reusable admin bearer tokens are not placed in Studio URLs. The Supadupa project admin page and Studio serve different purposes and can coexist:

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

## Container Runtime Privileges

Platform Dockerfile base images and fixed platform Compose images are pinned by digest for reproducibility. Generated project stack images use stable release-manifest tags rather than `latest`; they are not yet digest-locked. The control-plane image runs as a non-root user by default, and `setup-compose.sh` writes `SUPADUPA_CONTROL_PLANE_USER` plus `SUPADUPA_DOCKER_GID` so runtime bind mounts stay writable and the apply-mode Docker proxy socket group is explicit.

The base compose file does not mount `/var/run/docker.sock`. Compose apply mode is an explicit opt-in through `deploy/compose.apply.yaml`; the control plane talks to an internal `docker-socket-proxy` service through `DOCKER_HOST=tcp://docker-socket-proxy:2375`, and only that proxy service mounts the host Docker socket. The proxy is intended to allow Compose lifecycle routes, blocks known administrative surfaces such as Swarm, plugins, secrets, system prune, image build, image history, image search, and image import, rejects privileged or host-namespace container-create payloads, filters project object list responses, requires Docker event streams to carry exactly one non-platform project label, requires image-list requests to carry shaped reference filters with tag-only wildcards, rejects malformed image inspect and pull references or tags, and permits only the configured shared ingress network needed for project routing while still requiring project-labeled containers on network mutations. Static tests cover the allowlist, dangerous create payloads, scoped image/event requests, malformed image references, and broad wildcard rejection, and `scripts/check-compose-apply-lifecycle-smoke.sh` validates create, pause, resume, restart, scale, destroy, and cleanup through the live proxy. Treat the proxy as sensitive host-administrative access; for stricter production isolation, move Compose apply into a separate worker host or VM.

Generated project stacks also keep the Docker socket disabled by default. `SUPADUPA_PROJECT_DOCKER_LOGS=true` restores the legacy per-project Vector `docker_logs` source and mounts `/var/run/docker.sock` into the project Vector container. Use that only as a compatibility bridge for Docker-log-based drains; prefer a platform-side collector or socket proxy for production log collection.

## Project Keys

Treat these as secrets:

- Service role key.
- Database password.
- SCIM tokens. New SCIM tokens must be at least 24 characters and are stored as versioned HMAC-SHA256 hashes keyed by the control-plane auth secret; legacy SHA-256 hashes continue to verify during migration.
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
11. Sanitized compatibility artifacts reviewed before upload or sharing.
12. Public DB/pooler ports exposed only when required and firewall-controlled.
