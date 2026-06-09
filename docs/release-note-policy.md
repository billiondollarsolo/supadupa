# Operator Note Policy

Supadupa changes that can affect a running installation must include an operator note. The goal is to make operational impact explicit before a release, not to create long changelog text for every small patch.

## Scope

An operator note is required for any change that touches one or more of these areas:

- Authentication, authorization, browser sessions, SSO, MFA, SCIM, service keys, API tokens, or project secret handling.
- Secret generation, encryption, KMS/Vault integration, secret rotation, masking, audit output, or bootstrap credentials.
- Persistence, migrations, checkpoint backup/restore, normalized metadata tables, destructive cleanup, PITR, or durable backup behavior.
- Terraform provider schema, imports, resource identifiers, computed/defaulted fields, lifecycle behavior, replace plans, or API compatibility.
- Deployment defaults for Docker Compose, Helm, Kubernetes CRDs, the operator, Dockerfiles, runtime images, network exposure, ports, ingress, TLS, or storage.
- Runtime compatibility scripts, stack release manifests, generated Supabase service configuration, CLI behavior, or public API routes.

## Note Contents

Each operator note should state:

- What changed in externally observable behavior.
- Who is affected: new installs, upgrades, local dev, Compose apply mode, Helm/operator users, Terraform users, or project runtime users.
- Required action, including environment variables, config values, migration order, restart requirements, backup requirements, or rollback limits.
- Validation commands or smoke tests operators should run after applying the change.
- Security or data-loss risk if the action is skipped.
- Any accepted exception if the change is intentionally invisible to operators.

## Documentation Targets

Put the operator note where the impacted operator will naturally look:

- `docs/install.md` for install-time defaults and bootstrap behavior.
- `docs/operations.md` for day-to-day operation, validation, logs, and runtime lifecycle.
- `docs/security.md` for secrets, auth, access control, exposure, and host-administrative access.
- `docs/kubernetes.md` for Helm, CRDs, operator behavior, runtime namespaces, service DNS, and Kubernetes limits.
- `docs/dns-tls.md` for public routing, certificates, and edge exposure.
- `docs/upgrades.md` for migration order, backup gates, stack release changes, and rollback constraints.
- `scripts/compat/README.md` for compatibility-suite behavior and new operator smoke tests.
- `docs/project-review-remediation-plan.md` for remediation tracking while this project review remains active.

## Example

```text
Operator note:
Compose apply mode now routes Docker API access through the internal docker-socket-proxy service instead of mounting the host socket into the control plane. Existing apply-mode installs must include deploy/compose.apply.yaml, set SUPADUPA_COMPOSE_APPLY=true, and restart the platform stack. Validate with scripts/check-compose-apply-lifecycle-smoke.sh before using Terraform project lifecycle operations. Skipping the proxy keeps older broad Docker socket exposure in place.
```

## Fixed Looks Like

- A reviewer can find the operational impact for every security-sensitive or deployment-sensitive change without reverse-engineering the diff.
- A release can explain required operator action, validation, and rollback constraints before the change reaches production.
- `scripts/check-release-note-policy.sh` fails when sensitive paths change without a corresponding docs/operator-note update.
