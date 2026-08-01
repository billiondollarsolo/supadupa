# Feature Flags Catalog

Platform and org feature flags gate advanced surfaces. Defaults favor a safe single-org Compose MVP.

Source of truth for platform defaults: `defaultPlatformFeatureFlags` in `internal/control/store.go`.

## Platform flags (typical defaults)

| Flag | Default | Effect when enabled |
|------|---------|---------------------|
| `pitr` | false | WAL/PITR UI and recoverability APIs for projects |
| `preview_branches` | false | Preview branch create/list surfaces |
| `read_replicas` | false | Read replica lifecycle and routing |
| `custom_domains` | false | Custom domain and certificate management |
| `network_restrictions` | false | Per-project network connection declarations |
| `log_drains` | false | Log drain configuration |
| `ai_integrations` | false | Embeddings / AI provider config surfaces |
| `usage_metering` | false | Usage snapshots and metering |
| `billing` | false | Draft invoices (no payment processor) |
| `platform_sso_scim` | false | Platform SSO/SCIM admin surfaces |
| `kubernetes_operator` | false | Kubernetes operator product surfaces |
| `multi_org` | false | Multi-organization mode (`single_org_mode` inverse) |
| `database_external_access` | false | Master switch for raw DB/pooler TCP ingress |
| `production_posture` | false | Stronger advisor severity for recovery gaps |

## Production profile

For production-like installs, see [production-profile.md](production-profile.md). Enable recovery-related flags only after an off-host target is tested and durable.

## Related

- [Master improvement plan](master-improvement-plan.md) (E10, N7)
- [Security](security.md) (SSO JSON adapter gate)
