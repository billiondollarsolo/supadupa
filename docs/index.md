# Supadupa User Docs

These docs cover the current MVP system: the Supadupa control plane, Docker Compose project runtime, Traefik edge routing, Cloudflare DNS-01 TLS, admin UI, CLI, backups, upgrades, and operating procedures.

Start here:

- [Install](install.md): local loopback and VPS install flows.
- [DNS And TLS](dns-tls.md): control-plane DNS, wildcard project DNS, Traefik, Let's Encrypt, and BYO certificates.
- [Offline Local Edge](offline-local.md): local wildcard-style routing with generated development certificates.
- [Projects](projects.md): create projects, connect clients, use Studio, custom domains, and CLI workflows.
- [Backups And Recovery](backups-recovery.md): S3-compatible targets, project backups, control-plane backups, WAL/PITR posture, and recovery gates.
- [Upgrades](upgrades.md): stable stack releases, project upgrades, upgrade backup guards, and control-plane updates.
- [Operations](operations.md): day-to-day commands, runtime layout, metrics, logs, Advisor, Compliance, and local development.
- [Security](security.md): secrets, access model, network exposure, Studio access, and production hardening.
- [Kubernetes](kubernetes.md): Helm chart, CRDs, operator scaffold, and current Kubernetes limits.
- [Operator Note Policy](release-note-policy.md): when security-sensitive or deployment-sensitive changes need explicit release/operator notes.
- [Troubleshooting](troubleshooting.md): common API, CORS, TLS, project runtime, backup, and CLI issues.

The older long-form README is preserved at [README-legacy.md](README-legacy.md).

## MVP Scope

Supadupa is currently MVP-ready for evaluation, internal dev, and production-like validation on a Linux VPS. The supported runtime path is Docker Compose. Kubernetes now has a Helm/operator scaffold, but Kubernetes is not yet the primary MVP runtime path.

Working at MVP level:

- Browser admin UI for organizations, users, projects, defaults, backups, logs, metrics, security, and operations.
- Multi-project Docker Compose provisioning with isolated project directories, networks, secrets, routes, and hostnames.
- Public project surfaces through Traefik: API, Studio, Storage S3, direct Postgres, transaction pooler, session pooler, Realtime, and Edge Functions.
- Supabase JS usage, official Supabase CLI DB workflows, and Supadupa CLI workflows.
- Studio access mediated by Supadupa control-plane login.
- Custom domains with route/certificate artifacts and BYO certificate upload.
- Logical backups, physical backup plumbing, WAL archive plumbing, control-plane backups, backup target management, and recovery posture reporting.
- Stable stack release catalog and project upgrade guardrails.
- Compatibility test suite under `scripts/compat`.

Not hosted-grade yet:

- Destructive PITR restore has not been fully proven against a real off-host S3/R2/remote-MinIO target in the live deployment.
- Failed-upgrade restore needs durable off-host backup artifact validation, not only local/dev artifacts.
- Official `supabase gen types --db-url` against the public DB route has an upstream Supabase CLI TLS/CA caveat. Use the Supadupa tunnel/wrapper workflow when needed.
- Real third-party provider propagation still needs proof for external CDN behavior, real SMS delivery, and true multi-region placement/failover.
- Kubernetes is not yet the primary MVP runtime path; the operator materializes generic project workloads, but full Supabase data-plane parity still needs live service validation.
- Compliance screens are operator evidence helpers, not certification claims.
