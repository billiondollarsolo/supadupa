import { Activity, Boxes, Command, Database, Gauge, KeyRound, RadioTower, RotateCcw, Shield, SlidersHorizontal, type LucideIcon } from "lucide-react";

export type ConfigArea = "general" | "database" | "auth" | "auth_providers" | "email_templates" | "storage" | "functions" | "realtime" | "pooler" | "network" | "smtp" | "ai";

// Shared shape for a single editable config field. `select` renders a dropdown
// constrained to `options`; everything else falls back to text/number/boolean.
export type ConfigField = {
  key: string;
  label: string;
  kind?: "text" | "number" | "boolean" | "textarea" | "select";
  options?: Array<{ value: string; label: string }>;
};
export type ProjectTab = "overview" | "connect" | "auth" | "database" | "storage" | "functions" | "realtime" | "logs" | "backups" | "config" | "activity";
export type ConnectSection = "overview" | "endpoints" | "keys" | "jwt" | "database" | "storage" | "links" | "cli" | "snippets" | "secrets";
export type AuthSection = "overview" | "runtime" | "providers" | "email" | "clients" | "hooks" | "access";
export type DatabaseSection =
  | "overview"
  | "config"
  | "pooler"
  | "branches"
  | "replicas"
  | "replication"
  | "analytics"
  | "extensions"
  | "cron"
  | "queues"
  | "webhooks"
  | "schemas"
  | "roles"
  | "ai";
export type ProjectSettingsSection = "overview" | "runtime" | "services" | "domains" | "network" | "operations" | "danger";
export type PlatformSettingsSection = "overview" | "defaults" | "features" | "db-ingress" | "backups" | "smtp" | "sso" | "scim" | "hosts";
export type OrganizationSection = "overview" | "members" | "teams" | "features" | "quotas" | "usage" | "billing";
export type SecuritySection = "overview" | "mfa" | "access" | "advisor" | "compliance";

export type ProjectSubnavItem<T extends string = string> = {
  id: T;
  label: string;
  description: string;
  // Optional cluster label for grouped sub-navs (e.g. Database). When present on
  // a tab's items, the sidebar renders small group headers between clusters.
  // Items without a group (or tabs whose items have no groups) render unchanged.
  group?: string;
};

export const configAreaLabels: Record<ConfigArea, string> = {
  general: "General",
  database: "Database",
  auth: "Auth",
  auth_providers: "Providers",
  email_templates: "Templates",
  storage: "Storage",
  functions: "Functions",
  realtime: "Realtime",
  pooler: "Pooler",
  network: "Network",
  smtp: "SMTP",
  ai: "AI",
};

const socialOAuthProviderFields = [
  ["apple", "Apple"],
  ["bitbucket", "Bitbucket"],
  ["discord", "Discord"],
  ["facebook", "Facebook"],
  ["gitlab", "GitLab"],
  ["kakao", "Kakao"],
  ["keycloak", "Keycloak"],
  ["linkedin_oidc", "LinkedIn OIDC"],
  ["notion", "Notion"],
  ["slack_oidc", "Slack OIDC"],
  ["spotify", "Spotify"],
  ["twitch", "Twitch"],
  ["twitter", "Twitter"],
  ["workos", "WorkOS"],
  ["zoom", "Zoom"],
].flatMap(([key, label]) => [
  { key: `oauth_${key}_enabled`, label: `${label} OAuth`, kind: "boolean" as const },
  { key: `oauth_${key}_client_id`, label: `${label} client ID` },
  { key: `oauth_${key}_client_secret_handle`, label: `${label} secret handle` },
  { key: `oauth_${key}_url`, label: `${label} provider URL` },
  { key: `oauth_${key}_redirect_uri`, label: `${label} redirect URI` },
  { key: `oauth_${key}_skip_nonce_check`, label: `${label} skip nonce`, kind: "boolean" as const },
]);

export const configSchemas: Record<ConfigArea, ConfigField[]> = {
  general: [
    {
      key: "environment",
      label: "Environment",
      kind: "select",
      options: [
        { value: "development", label: "Development" },
        { value: "production", label: "Production" },
      ],
    },
  ],
  database: [
    { key: "pg_graphql_enabled", label: "GraphQL API", kind: "boolean" },
    { key: "database_webhooks", label: "Webhooks", kind: "boolean" },
    { key: "pg_cron_enabled", label: "Cron", kind: "boolean" },
    { key: "pgmq_enabled", label: "Queues", kind: "boolean" },
    { key: "fdw_enabled", label: "FDW", kind: "boolean" },
    { key: "vault_enabled", label: "Vault", kind: "boolean" },
    { key: "pgvector_enabled", label: "pgvector", kind: "boolean" },
    { key: "supavisor_enabled", label: "Supavisor", kind: "boolean" },
    { key: "ssl_enforced", label: "DB SSL", kind: "boolean" },
    { key: "rls_enforce_system_tables", label: "Enforce RLS on system tables", kind: "boolean" },
    { key: "extension_toggle_ui", label: "Extension UI", kind: "boolean" },
    { key: "performance_advisor_mode", label: "Advisor mode" },
    { key: "orioledb_profile", label: "OrioleDB" },
  ],
  auth: [
    { key: "email_enabled", label: "Email login", kind: "boolean" },
    { key: "magic_link_enabled", label: "Magic links", kind: "boolean" },
    { key: "mfa_totp_enabled", label: "TOTP MFA", kind: "boolean" },
    { key: "mfa_totp_enroll_enabled", label: "TOTP enroll", kind: "boolean" },
    { key: "mfa_totp_verify_enabled", label: "TOTP verify", kind: "boolean" },
    { key: "mfa_phone_enabled", label: "Phone MFA", kind: "boolean" },
    { key: "mfa_phone_enroll_enabled", label: "Phone MFA enroll", kind: "boolean" },
    { key: "mfa_phone_verify_enabled", label: "Phone MFA verify", kind: "boolean" },
    { key: "mfa_phone_otp_length", label: "Phone MFA OTP length", kind: "number" },
    { key: "mfa_phone_max_frequency", label: "Phone MFA frequency" },
    { key: "captcha_provider", label: "Captcha provider" },
    { key: "captcha_site_key", label: "Captcha site key" },
    { key: "captcha_secret_handle", label: "Captcha secret handle" },
    { key: "jwt_key_mode", label: "JWT key mode" },
    { key: "site_url", label: "Site URL" },
    { key: "additional_redirects", label: "Redirect URLs" },
  ],
  auth_providers: [
    { key: "oauth_google_enabled", label: "Google OAuth", kind: "boolean" },
    { key: "oauth_google_client_id", label: "Google client ID" },
    { key: "oauth_google_client_secret_handle", label: "Google secret handle" },
    { key: "oauth_github_enabled", label: "GitHub OAuth", kind: "boolean" },
    { key: "oauth_github_client_id", label: "GitHub client ID" },
    { key: "oauth_github_client_secret_handle", label: "GitHub secret handle" },
    { key: "oauth_azure_enabled", label: "Azure OAuth", kind: "boolean" },
    { key: "oauth_azure_client_id", label: "Azure client ID" },
    { key: "oauth_azure_client_secret_handle", label: "Azure secret handle" },
    ...socialOAuthProviderFields,
    { key: "oauth_oidc_enabled", label: "Custom OIDC", kind: "boolean" },
    { key: "oauth_oidc_issuer_url", label: "OIDC issuer URL" },
    { key: "oauth_oidc_client_id", label: "OIDC client ID" },
    { key: "oauth_oidc_client_secret_handle", label: "OIDC secret handle" },
    { key: "oauth_oidc_scopes", label: "OIDC scopes" },
    { key: "phone_enabled", label: "Phone login", kind: "boolean" },
    { key: "sms_provider", label: "SMS provider" },
    { key: "sms_otp_exp", label: "SMS OTP expiry", kind: "number" },
    { key: "sms_otp_length", label: "SMS OTP length", kind: "number" },
    { key: "sms_max_frequency", label: "SMS max frequency" },
    { key: "sms_template", label: "SMS template", kind: "textarea" },
    { key: "sms_test_otp_handle", label: "SMS test OTP handle" },
    { key: "sms_test_otp_valid_until", label: "SMS test OTP valid until" },
    { key: "sms_twilio_account_sid", label: "Twilio account SID" },
    { key: "sms_twilio_auth_token_handle", label: "Twilio token handle" },
    { key: "sms_twilio_message_service_sid", label: "Twilio message SID" },
    { key: "sms_messagebird_originator", label: "MessageBird originator" },
    { key: "sms_messagebird_access_key_handle", label: "MessageBird key handle" },
    { key: "sms_textlocal_sender", label: "TextLocal sender" },
    { key: "sms_textlocal_api_key_handle", label: "TextLocal key handle" },
    { key: "sms_vonage_from", label: "Vonage from" },
    { key: "sms_vonage_api_key", label: "Vonage API key ID" },
    { key: "sms_vonage_api_secret_handle", label: "Vonage secret handle" },
    { key: "saml_enabled", label: "SAML SSO", kind: "boolean" },
    { key: "saml_metadata_url", label: "SAML metadata URL" },
    { key: "saml_entity_id", label: "SAML entity ID" },
    { key: "third_party_jwt_issuer", label: "External JWT issuer" },
    { key: "third_party_jwt_audience", label: "External JWT audience" },
    { key: "web3_ethereum_enabled", label: "Ethereum wallets", kind: "boolean" },
    { key: "web3_solana_enabled", label: "Solana wallets", kind: "boolean" },
  ],
  email_templates: [
    { key: "confirmation_subject", label: "Confirmation subject" },
    { key: "confirmation_body", label: "Confirmation body", kind: "textarea" },
    { key: "recovery_subject", label: "Recovery subject" },
    { key: "recovery_body", label: "Recovery body", kind: "textarea" },
    { key: "magic_link_subject", label: "Magic link subject" },
    { key: "magic_link_body", label: "Magic link body", kind: "textarea" },
    { key: "invite_subject", label: "Invite subject" },
    { key: "invite_body", label: "Invite body", kind: "textarea" },
    { key: "email_change_subject", label: "Email change subject" },
    { key: "email_change_body", label: "Email change body", kind: "textarea" },
    { key: "sms_otp_message", label: "SMS OTP message", kind: "textarea" },
    { key: "notification_password_changed_enabled", label: "Password changed notification", kind: "boolean" },
    { key: "notification_password_changed_subject", label: "Password changed subject" },
    { key: "notification_password_changed_body", label: "Password changed body", kind: "textarea" },
    { key: "notification_email_changed_enabled", label: "Email changed notification", kind: "boolean" },
    { key: "notification_email_changed_subject", label: "Email changed subject" },
    { key: "notification_email_changed_body", label: "Email changed body", kind: "textarea" },
    { key: "notification_phone_changed_enabled", label: "Phone changed notification", kind: "boolean" },
    { key: "notification_phone_changed_subject", label: "Phone changed subject" },
    { key: "notification_phone_changed_body", label: "Phone changed body", kind: "textarea" },
    { key: "notification_mfa_factor_enrolled_enabled", label: "MFA enrolled notification", kind: "boolean" },
    { key: "notification_mfa_factor_enrolled_subject", label: "MFA enrolled subject" },
    { key: "notification_mfa_factor_enrolled_body", label: "MFA enrolled body", kind: "textarea" },
    { key: "notification_mfa_factor_unenrolled_enabled", label: "MFA unenrolled notification", kind: "boolean" },
    { key: "notification_mfa_factor_unenrolled_subject", label: "MFA unenrolled subject" },
    { key: "notification_mfa_factor_unenrolled_body", label: "MFA unenrolled body", kind: "textarea" },
    { key: "notification_identity_linked_enabled", label: "Identity linked notification", kind: "boolean" },
    { key: "notification_identity_linked_subject", label: "Identity linked subject" },
    { key: "notification_identity_linked_body", label: "Identity linked body", kind: "textarea" },
    { key: "notification_identity_unlinked_enabled", label: "Identity unlinked notification", kind: "boolean" },
    { key: "notification_identity_unlinked_subject", label: "Identity unlinked subject" },
    { key: "notification_identity_unlinked_body", label: "Identity unlinked body", kind: "textarea" },
  ],
  storage: [
    { key: "file_size_limit_mb", label: "File limit MB", kind: "number" },
    { key: "image_transform_enabled", label: "Image transforms", kind: "boolean" },
    { key: "resumable_upload_enabled", label: "Resumable uploads", kind: "boolean" },
    { key: "s3_compat_enabled", label: "S3 compatibility", kind: "boolean" },
  ],
  functions: [
    { key: "runtime_enabled", label: "Runtime", kind: "boolean" },
    { key: "verify_jwt_by_default", label: "Verify JWT", kind: "boolean" },
    { key: "worker_timeout_ms", label: "Worker timeout ms", kind: "number" },
    { key: "import_map", label: "Import map" },
    { key: "deployment_policy", label: "Deploy policy" },
    { key: "secret_sync_enabled", label: "Secret sync", kind: "boolean" },
  ],
  realtime: [
    { key: "postgres_changes_enabled", label: "Postgres changes", kind: "boolean" },
    { key: "broadcast_enabled", label: "Broadcast", kind: "boolean" },
    { key: "presence_enabled", label: "Presence", kind: "boolean" },
    { key: "broadcast_replay", label: "Replay", kind: "boolean" },
    { key: "broadcast_from_database", label: "Broadcast from DB", kind: "boolean" },
  ],
  pooler: [
    { key: "dedicated_pooler_enabled", label: "Dedicated pooler", kind: "boolean" },
    { key: "dedicated_pooler_tier", label: "Dedicated tier" },
    { key: "pool_mode", label: "Pool mode" },
    { key: "default_pool_size", label: "Pool size", kind: "number" },
    { key: "max_client_connections", label: "Max clients", kind: "number" },
  ],
  network: [
    { key: "http_allowlist", label: "HTTP/Studio allowlist" },
    { key: "db_allowlist", label: "Database allowlist" },
    { key: "ssl_enforced", label: "SSL enforced", kind: "boolean" },
  ],
  smtp: [
    { key: "enabled", label: "Enabled", kind: "boolean" },
    { key: "host", label: "Host" },
    { key: "port", label: "Port", kind: "number" },
    { key: "sender_name", label: "Sender name" },
    { key: "sender_email", label: "Sender email" },
    { key: "username", label: "Username" },
    { key: "password_handle", label: "Password handle" },
    { key: "tls_mode", label: "TLS mode" },
  ],
  ai: [
    { key: "openai_enabled", label: "OpenAI", kind: "boolean" },
    { key: "openai_api_key_handle", label: "OpenAI key handle" },
    { key: "huggingface_enabled", label: "Hugging Face", kind: "boolean" },
    { key: "huggingface_api_key_handle", label: "Hugging Face key handle" },
    { key: "default_embedding_provider", label: "Default provider" },
    { key: "default_embedding_model", label: "Default model" },
    { key: "default_embedding_dimension", label: "Default dimension", kind: "number" },
    { key: "embedding_queue_enabled", label: "Embedding queue", kind: "boolean" },
    { key: "studio_assistant_enabled", label: "Studio assistant", kind: "boolean" },
    { key: "studio_assistant_provider", label: "Assistant provider" },
    { key: "studio_assistant_model", label: "Assistant model" },
    { key: "studio_assistant_key_handle", label: "Assistant key handle" },
  ],
};

// Additive: cross-links from a raw Runtime Config area to the friendly,
// guided feature tab that edits the same surface. Used to frame Runtime Config
// as the advanced/raw editor without removing either UI. Only areas with a
// dedicated first-class tab are listed; others fall back to "no guided tab".
export const configAreaGuidedTab: Partial<Record<ConfigArea, { tab: ProjectTab; label: string }>> = {
  database: { tab: "database", label: "Database" },
  auth: { tab: "auth", label: "Auth" },
  auth_providers: { tab: "auth", label: "Auth" },
  email_templates: { tab: "auth", label: "Auth" },
  smtp: { tab: "auth", label: "Auth" },
  storage: { tab: "storage", label: "Storage" },
  functions: { tab: "functions", label: "Functions" },
  realtime: { tab: "realtime", label: "Realtime" },
  pooler: { tab: "database", label: "Database" },
  ai: { tab: "database", label: "Database" },
};

export type ConfigFieldGroup = {
  id: string;
  label: string;
  fields: ConfigField[];
};

// Additive: derive scannable groups for a Runtime Config area so the flat list
// of every field (120+ for auth_providers) becomes collapsible clusters.
// auth_providers is grouped by provider/feature derived from the key prefix;
// every other area falls back to a single group, which is fine for short lists.
export function configFieldGroups(area: ConfigArea): ConfigFieldGroup[] {
  const schema = configSchemas[area];
  if (area !== "auth_providers") {
    return [{ id: area, label: configAreaLabels[area], fields: schema }];
  }
  const order: string[] = [];
  const groups = new Map<string, ConfigFieldGroup>();
  const push = (id: string, label: string, field: ConfigFieldGroup["fields"][number]) => {
    let group = groups.get(id);
    if (!group) {
      group = { id, label, fields: [] };
      groups.set(id, group);
      order.push(id);
    }
    group.fields.push(field);
  };
  for (const field of schema) {
    const key = field.key;
    if (key.startsWith("oauth_oidc_")) {
      push("oauth_oidc", "Custom OIDC", field);
    } else if (key.startsWith("oauth_")) {
      // oauth_<provider>_... — group per provider using its enabled-field label
      const provider = key.slice("oauth_".length).replace(/_(enabled|client_id|client_secret_handle|url|redirect_uri|skip_nonce_check)$/, "");
      const enabledLabel = schema.find((entry) => entry.key === `oauth_${provider}_enabled`)?.label;
      const label = enabledLabel ? enabledLabel.replace(/ OAuth$/, "") : provider;
      push(`oauth_${provider}`, `${label} OAuth`, field);
    } else if (key.startsWith("sms_") || key === "phone_enabled") {
      push("phone", "Phone / SMS", field);
    } else if (key.startsWith("saml_")) {
      push("saml", "SAML SSO", field);
    } else if (key.startsWith("third_party_jwt_")) {
      push("third_party_jwt", "External JWT", field);
    } else if (key.startsWith("web3_")) {
      push("web3", "Web3 wallets", field);
    } else {
      push("other", "Other", field);
    }
  }
  return order.map((id) => groups.get(id)!);
}

export const projectServiceLabels = [
  { key: "auth", label: "Auth" },
  { key: "rest", label: "REST API" },
  { key: "graphql", label: "GraphQL" },
  { key: "realtime", label: "Realtime" },
  { key: "storage", label: "Storage" },
  { key: "imgproxy", label: "Imgproxy" },
  { key: "functions", label: "Functions" },
  { key: "pooler", label: "Pooler" },
  { key: "studio", label: "Studio" },
  { key: "analytics", label: "Analytics" },
  { key: "vector", label: "Vector" },
];

export const projectTabs: Array<{ id: ProjectTab; label: string; suffix: string; icon: LucideIcon }> = [
  { id: "overview", label: "Overview", suffix: "", icon: Gauge },
  { id: "connect", label: "Connect", suffix: "connect", icon: KeyRound },
  { id: "auth", label: "Auth", suffix: "auth", icon: Shield },
  { id: "database", label: "Database", suffix: "database", icon: Database },
  { id: "storage", label: "Storage", suffix: "storage", icon: Boxes },
  { id: "functions", label: "Functions", suffix: "functions", icon: Command },
  { id: "realtime", label: "Realtime", suffix: "realtime", icon: RadioTower },
  { id: "logs", label: "Logs", suffix: "logs", icon: Activity },
  { id: "backups", label: "Backups", suffix: "backups", icon: RotateCcw },
  { id: "config", label: "Settings", suffix: "config", icon: SlidersHorizontal },
  { id: "activity", label: "Activity", suffix: "activity", icon: Activity },
];

export const connectSections: Array<ProjectSubnavItem<ConnectSection>> = [
  { id: "overview", label: "Overview", description: "Connection groups and quick actions." },
  { id: "endpoints", label: "Endpoints", description: "API, REST, Auth, GraphQL, Realtime, Functions, and Storage URLs." },
  { id: "keys", label: "API Keys", description: "Publishable, secret, anon, and service-role handles." },
  { id: "jwt", label: "JWT", description: "JWT secret and asymmetric signing keys." },
  { id: "database", label: "Database", description: "Direct Postgres, pooler strings, and broken-out fields." },
  { id: "storage", label: "Storage", description: "S3-compatible endpoint and storage credentials." },
  { id: "links", label: "Links", description: "Studio, REST docs, GraphQL explorer, logs, and service links." },
  { id: "cli", label: "CLI", description: "supadupa-cli and Supabase CLI compatibility profile." },
  { id: "snippets", label: "Snippets", description: "Connection and SDK initialization snippets." },
  { id: "secrets", label: "Secrets", description: "Secret handle inventory for project credentials." },
];

export const authSections: Array<ProjectSubnavItem<AuthSection>> = [
  { id: "overview", label: "Overview", description: "Auth posture and project access at a glance." },
  { id: "runtime", label: "Runtime", description: "Email login, MFA, captcha, redirects, and JWT mode." },
  { id: "providers", label: "Providers", description: "OAuth, OIDC, SAML, SMS, and wallet provider settings." },
  { id: "email", label: "Email", description: "SMTP delivery and transactional templates." },
  { id: "clients", label: "OAuth Clients", description: "Project-as-IdP clients and redirect URIs." },
  { id: "hooks", label: "Hooks", description: "Server-side auth customization hooks." },
  { id: "access", label: "Access", description: "Project-scoped teams, users, and role grants." },
];

export const databaseSections: Array<ProjectSubnavItem<DatabaseSection>> = [
  { id: "overview", label: "Overview", description: "Database posture and configured surfaces." },
  { id: "config", label: "Runtime", description: "GraphQL, webhooks, cron, queues, FDW, Vault, SSL.", group: "Connectivity" },
  { id: "pooler", label: "Pooler", description: "Supavisor modes and connection limits.", group: "Connectivity" },
  { id: "replicas", label: "Replicas", description: "Read replicas, routing, promotion, and failover.", group: "Connectivity" },
  { id: "branches", label: "Branches", description: "Preview branches and clone state.", group: "Connectivity" },
  { id: "replication", label: "Replication", description: "Logical publications and external destinations.", group: "Data movement" },
  { id: "analytics", label: "Analytics", description: "Iceberg analytics buckets.", group: "Data movement" },
  { id: "extensions", label: "Extensions", description: "Postgres extension toggles.", group: "Extensions & jobs" },
  { id: "cron", label: "Cron", description: "Scheduled database jobs.", group: "Extensions & jobs" },
  { id: "queues", label: "Queues", description: "pgmq queues and retention.", group: "Extensions & jobs" },
  { id: "webhooks", label: "Webhooks", description: "Database change webhooks.", group: "Extensions & jobs" },
  { id: "schemas", label: "Schemas", description: "Declarative schema versions.", group: "Schema & access" },
  { id: "roles", label: "Roles", description: "Database roles and schema grants.", group: "Schema & access" },
  { id: "ai", label: "Vector / AI", description: "Embeddings jobs and vector buckets.", group: "Vector / AI" },
];

export const projectSettingsSections: Array<ProjectSubnavItem<ProjectSettingsSection>> = [
  { id: "overview", label: "Overview", description: "Configuration posture and operational controls." },
  { id: "runtime", label: "Runtime Config", description: "Area-specific stack and service configuration." },
  { id: "services", label: "Services", description: "Enable or disable managed stack services." },
  { id: "domains", label: "Domains", description: "Custom ingress domains and certificates." },
  { id: "network", label: "Network", description: "Allowlists, TLS posture, and private connectivity." },
  { id: "operations", label: "Operations", description: "Runtime status and lifecycle actions." },
  { id: "danger", label: "Danger Zone", description: "Destructive project actions." },
];

export const platformSettingsSections: Array<ProjectSubnavItem<PlatformSettingsSection>> = [
  { id: "overview", label: "Overview", description: "Platform defaults and enterprise configuration summary." },
  { id: "defaults", label: "Defaults", description: "New project domain, version, profile, tier, and backup defaults." },
  { id: "features", label: "Feature Flags", description: "Local, Compose, and enterprise feature availability." },
  { id: "db-ingress", label: "Database Ingress", description: "Trusted client networks for direct Postgres and pooler access." },
  { id: "backups", label: "Backups", description: "S3-compatible backup targets for project and control-plane recovery." },
  { id: "smtp", label: "Platform SMTP", description: "Control-plane email delivery settings." },
  { id: "sso", label: "Platform SSO", description: "Global admin SAML SSO configuration." },
  { id: "scim", label: "SCIM", description: "Platform user and group provisioning status." },
];

export const organizationSections: Array<ProjectSubnavItem<OrganizationSection>> = [
  { id: "overview", label: "Overview", description: "Org scope, access posture, quota posture, and usage summary." },
  { id: "members", label: "Members", description: "Global org access and platform users." },
  { id: "teams", label: "Teams", description: "Project-scoped RBAC teams and membership." },
  { id: "features", label: "Features", description: "Org-specific rollout overrides inherited from platform defaults." },
  { id: "quotas", label: "Quotas", description: "Org-level project, CPU, memory, and disk limits." },
  { id: "usage", label: "Usage", description: "Metering, usage snapshots, and data-plane counters." },
  { id: "billing", label: "Billing", description: "Draft invoices from metering snapshots." },
];

export const securitySections: Array<ProjectSubnavItem<SecuritySection>> = [
  { id: "overview", label: "Overview", description: "Security posture across MFA, access review, advisor, and compliance." },
  { id: "mfa", label: "MFA", description: "Current platform account MFA enrollment." },
  { id: "access", label: "Access Review", description: "Org, team, and project-effective access." },
  { id: "advisor", label: "Advisor", description: "Fleet security and performance findings." },
  { id: "compliance", label: "Compliance", description: "SOC 2 and HIPAA control posture." },
];

export const projectSubnav: Partial<Record<ProjectTab, Array<ProjectSubnavItem>>> = {
  connect: connectSections,
  auth: authSections,
  database: databaseSections,
  config: projectSettingsSections,
};
