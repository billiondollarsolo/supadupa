export const featureFlagGroups = [
  {
    label: "Org and access",
    flags: [
      ["single_org_mode", "Default org mode"],
      ["multi_org", "Multiple orgs"],
      ["team_rbac", "Team RBAC"],
      ["project_access_grants", "Project grants"],
      ["project_self_service", "Project self-service"],
      ["supabase_cli_compat", "Supabase CLI compat"],
    ],
  },
  {
    label: "Operations",
    flags: [
      ["service_toggles", "Service toggles"],
      ["custom_domains", "Custom domains"],
      ["network_restrictions", "Network restrictions"],
      ["log_drains", "Log drains"],
      ["pitr", "PITR"],
    ],
  },
  {
    label: "Enterprise",
    flags: [
      ["preview_branches", "Preview branches"],
      ["read_replicas", "Read replicas"],
      ["edge_functions", "Edge functions"],
      ["ai_integrations", "AI integrations"],
      ["usage_metering", "Usage metering"],
      ["billing", "Billing"],
      ["platform_sso_scim", "Platform SSO/SCIM"],
      ["kubernetes_operator", "Kubernetes operator"],
    ],
  },
] as const;
