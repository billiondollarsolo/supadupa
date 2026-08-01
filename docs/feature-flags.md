# Feature Flags Catalog

Platform and org feature flags gate advanced surfaces. Defaults favor a safe single-org Compose MVP.

**Source of truth** for platform defaults: `defaultPlatformFeatureFlags` in `internal/control/store.go`.

Org-level flags inherit platform defaults unless overridden. Enabling a flag only exposes product surfaces; it does not by itself prove production readiness (recovery targets, SSO, billing processors, etc.).

## Platform flags (all keys)

| Flag | Default | Effect when enabled |
|------|---------|---------------------|
| `single_org_mode` | **true** | Platform operates as a single-organization product surface (default MVP posture). |
| `multi_org` | false | Multi-organization mode (`single_org_mode` inverse / multi-tenant org surfaces). |
| `resource_quotas` | false | Org/project resource quota enforcement surfaces. |
| `team_rbac` | **true** | Team and role-based access control within orgs. |
| `project_access_grants` | **true** | Per-user project access grants. |
| `project_self_service` | **true** | Users can create/manage projects without full platform-admin escalation. |
| `service_toggles` | **true** | Per-project service enable/disable toggles. |
| `supabase_cli_compat` | **true** | Supabase CLI compatibility surfaces and routes. |
| `custom_domains` | false | Custom domain and certificate management. |
| `network_restrictions` | false | Per-project network connection declarations / ingress restrictions. |
| `log_drains` | false | Log drain configuration. |
| `pitr` | false | WAL/PITR UI and recoverability APIs for projects. |
| `preview_branches` | false | Preview branch create/list surfaces. |
| `read_replicas` | false | Read replica lifecycle and routing. |
| `edge_functions` | **true** | Edge Functions deploy and invoke surfaces. |
| `ai_integrations` | false | Embeddings / AI provider config surfaces. |
| `usage_metering` | false | Usage snapshots and metering. |
| `billing` | false | Draft invoices (no payment processor). |
| `platform_sso_scim` | false | Platform SSO/SCIM admin surfaces. |
| `kubernetes_operator` | false | Kubernetes operator product surfaces. |
| `database_external_access` | false | Master switch: publish project databases through the edge router. Off by default so nothing is externally reachable until an operator enables it. |
| `production_posture` | false | When on, the security advisor holds platform-wide recovery posture (backup-target guards and recovery-ready targets) to full severity. Off by default so a local/MVP deploy is not a wall of high-severity findings. |

## Defaults summary (copy-paste)

```text
single_org_mode true
multi_org false
resource_quotas false
team_rbac true
project_access_grants true
project_self_service true
service_toggles true
supabase_cli_compat true
custom_domains false
network_restrictions false
log_drains false
pitr false
preview_branches false
read_replicas false
edge_functions true
ai_integrations false
usage_metering false
billing false
platform_sso_scim false
kubernetes_operator false
database_external_access false
production_posture false
```

## Production profile

For production-like installs, see [production-profile.md](production-profile.md). Enable recovery-related flags (`pitr`, `production_posture`) only after an off-host target is tested and durable. Keep `database_external_access` false unless raw DB/pooler TCP is intentionally published and firewall-controlled. Keep `platform_sso_scim` off until real SAML/SCIM production posture is in place (see [security.md](security.md)).

## Related

- [Master improvement plan](master-improvement-plan.md) (E10, N7)
- [Security](security.md) (SSO JSON adapter gate)
- [Production profile](production-profile.md)
