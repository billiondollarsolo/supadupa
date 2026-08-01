package control

import (
	"errors"
	"regexp"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

var refPattern = regexp.MustCompile(`^[a-z0-9-]{3,64}$`)
var projectRefPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,53}[a-z0-9])$`)
var replicaNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,62}[a-z0-9])$`)
var domainPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
var teamSlugPattern = regexp.MustCompile(`^[a-z0-9-]{2,64}$`)
var identifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
var extensionNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
var secretKindPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
var envAliasPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,62}$`)

var allowedConfigAreas = map[string]map[string]string{
	"general": {
		// Declared production-intent for the project. The security advisor holds
		// "production" projects to full severity and keeps "development" projects
		// quiet (posture gaps reported as info), so a greenfield fleet isn't a
		// wall of red. Defaults to development so projects opt in to enforcement.
		"environment": "development",
	},
	"database": {
		"pg_graphql_enabled":       "true",
		"database_webhooks":        "true",
		"pg_cron_enabled":          "true",
		"pgmq_enabled":             "true",
		"fdw_enabled":              "true",
		"vault_enabled":            "true",
		"pgvector_enabled":         "true",
		"supavisor_enabled":        "true",
		"ssl_enforced":             "true",
		"extension_toggle_ui":      "false",
		"performance_advisor_mode": "studio",
		"orioledb_profile":         "off",
		// When on, the control plane enables RLS on platform-created internal
		// tables (e.g. Realtime's Oban job tables) that otherwise land in the
		// API-exposed public schema. Secure by default; user tables are left to
		// the user. Applied on project create and whenever this config is saved.
		"rls_enforce_system_tables": "true",
	},
	"auth": {
		"email_enabled":            "true",
		"magic_link_enabled":       "true",
		"mfa_totp_enabled":         "true",
		"mfa_totp_enroll_enabled":  "true",
		"mfa_totp_verify_enabled":  "true",
		"mfa_phone_enabled":        "false",
		"mfa_phone_enroll_enabled": "false",
		"mfa_phone_verify_enabled": "false",
		"mfa_phone_otp_length":     "6",
		"mfa_phone_max_frequency":  "10s",
		"captcha_provider":         "",
		"captcha_site_key":         "",
		"captcha_secret_handle":    "",
		"jwt_key_mode":             "shared-secret",
		"site_url":                 "",
		"additional_redirects":     "",
	},
	"auth_providers": {
		"oauth_google_enabled":              "false",
		"oauth_google_client_id":            "",
		"oauth_google_client_secret_handle": "",
		"oauth_github_enabled":              "false",
		"oauth_github_client_id":            "",
		"oauth_github_client_secret_handle": "",
		"oauth_azure_enabled":               "false",
		"oauth_azure_client_id":             "",
		"oauth_azure_client_secret_handle":  "",
		"oauth_oidc_enabled":                "false",
		"oauth_oidc_issuer_url":             "",
		"oauth_oidc_client_id":              "",
		"oauth_oidc_client_secret_handle":   "",
		"oauth_oidc_scopes":                 "openid email profile",
		"phone_enabled":                     "false",
		"sms_provider":                      "",
		"sms_otp_exp":                       "60",
		"sms_otp_length":                    "6",
		"sms_max_frequency":                 "60s",
		"sms_template":                      "",
		"sms_test_otp_handle":               "",
		"sms_test_otp_valid_until":          "",
		"sms_twilio_account_sid":            "",
		"sms_twilio_auth_token_handle":      "",
		"sms_twilio_message_service_sid":    "",
		"sms_messagebird_originator":        "",
		"sms_messagebird_access_key_handle": "",
		"sms_textlocal_sender":              "",
		"sms_textlocal_api_key_handle":      "",
		"sms_vonage_from":                   "",
		"sms_vonage_api_key":                "",
		"sms_vonage_api_secret_handle":      "",
		"saml_enabled":                      "false",
		"saml_metadata_url":                 "",
		"saml_entity_id":                    "",
		"third_party_jwt_issuer":            "",
		"third_party_jwt_audience":          "",
		"web3_ethereum_enabled":             "false",
		"web3_solana_enabled":               "false",
	},
	"email_templates": {
		"confirmation_subject":                       "",
		"confirmation_body":                          "",
		"recovery_subject":                           "",
		"recovery_body":                              "",
		"magic_link_subject":                         "",
		"magic_link_body":                            "",
		"invite_subject":                             "",
		"invite_body":                                "",
		"email_change_subject":                       "",
		"email_change_body":                          "",
		"sms_otp_message":                            "",
		"notification_password_changed_enabled":      "false",
		"notification_password_changed_subject":      "",
		"notification_password_changed_body":         "",
		"notification_email_changed_enabled":         "false",
		"notification_email_changed_subject":         "",
		"notification_email_changed_body":            "",
		"notification_phone_changed_enabled":         "false",
		"notification_phone_changed_subject":         "",
		"notification_phone_changed_body":            "",
		"notification_mfa_factor_enrolled_enabled":   "false",
		"notification_mfa_factor_enrolled_subject":   "",
		"notification_mfa_factor_enrolled_body":      "",
		"notification_mfa_factor_unenrolled_enabled": "false",
		"notification_mfa_factor_unenrolled_subject": "",
		"notification_mfa_factor_unenrolled_body":    "",
		"notification_identity_linked_enabled":       "false",
		"notification_identity_linked_subject":       "",
		"notification_identity_linked_body":          "",
		"notification_identity_unlinked_enabled":     "false",
		"notification_identity_unlinked_subject":     "",
		"notification_identity_unlinked_body":        "",
	},
	"storage": {
		"file_size_limit_mb":       "50",
		"image_transform_enabled":  "true",
		"resumable_upload_enabled": "true",
		"s3_compat_enabled":        "true",
	},
	"functions": {
		"runtime_enabled":       "true",
		"verify_jwt_by_default": "true",
		"worker_timeout_ms":     "60000",
		"import_map":            "",
		"deployment_policy":     "manual",
		"secret_sync_enabled":   "true",
	},
	"realtime": {
		"postgres_changes_enabled": "true",
		"broadcast_enabled":        "true",
		"presence_enabled":         "true",
		"broadcast_replay":         "false",
		"broadcast_from_database":  "false",
	},
	"pooler": {
		"dedicated_pooler_enabled": "false",
		"dedicated_pooler_tier":    "small",
		"pool_mode":                "transaction",
		"default_pool_size":        "20",
		"max_client_connections":   "200",
		"transaction_port":         "6543",
		"session_port":             "5432",
	},
	"network": {
		// Two independent allowlists, both default-open (empty = allow all):
		// http_allowlist gates HTTP/Studio/API edge routes; db_allowlist gates the
		// public database/pooler ports. Restricting one never affects the other.
		"http_allowlist": "",
		"db_allowlist":   "",
		"ssl_enforced":   "true",
		// Per-project database exposure: "private" | "allowlisted" | "public".
		// Defaults to private so every project stays off until explicitly opened.
		"db_ingress_mode": "private",
	},
	"smtp": {
		"host":            "",
		"port":            "587",
		"sender_name":     "",
		"sender_email":    "",
		"username":        "",
		"password_handle": "",
		"tls_mode":        "starttls",
		"enabled":         "false",
	},
	"ai": {
		"openai_enabled":              "false",
		"openai_api_key_handle":       "",
		"huggingface_enabled":         "false",
		"huggingface_api_key_handle":  "",
		"default_embedding_provider":  "openai",
		"default_embedding_model":     "text-embedding-3-small",
		"default_embedding_dimension": "1536",
		"embedding_queue_enabled":     "true",
		"studio_assistant_enabled":    "false",
		"studio_assistant_provider":   "openai",
		"studio_assistant_model":      "default",
		"studio_assistant_key_handle": "",
	},
}

var socialOAuthProviderIDs = []string{
	"apple",
	"bitbucket",
	"discord",
	"facebook",
	"figma",
	"gitlab",
	"kakao",
	"keycloak",
	"linkedin_oidc",
	"notion",
	"snapchat",
	"slack_oidc",
	"spotify",
	"twitch",
	"twitter",
	"workos",
	"zoom",
}

func init() {
	authProviders := allowedConfigAreas["auth_providers"]
	for _, provider := range socialOAuthProviderIDs {
		prefix := "oauth_" + provider
		authProviders[prefix+"_enabled"] = "false"
		authProviders[prefix+"_client_id"] = ""
		authProviders[prefix+"_client_secret_handle"] = ""
		authProviders[prefix+"_url"] = ""
		authProviders[prefix+"_redirect_uri"] = ""
		authProviders[prefix+"_skip_nonce_check"] = "false"
	}
}

var secretPrefixes = map[string]string{
	"jwt_secret":              "jwt",
	"jwt_signing_key_current": "jwkcur",
	"jwt_signing_key_next":    "jwknxt",
	"publishable_key":         "pub",
	"secret_key":              "sec",
	"anon_key":                "anon",
	"service_role":            "svc",
	"db_password":             "db",
	"s3_access_key":           "s3ak",
	"s3_secret_key":           "s3sk",
}

var secretByteLengths = map[string]int{
	"jwt_secret":              32,
	"jwt_signing_key_current": 32,
	"jwt_signing_key_next":    32,
	"publishable_key":         24,
	"secret_key":              32,
	"anon_key":                32,
	"service_role":            32,
	"db_password":             24,
	"s3_access_key":           16,
	"s3_secret_key":           32,
}

var allowedLogDrainTargets = map[string]struct{}{
	"https":   {},
	"loki":    {},
	"datadog": {},
	"sentry":  {},
	"axiom":   {},
	"s3":      {},
}

var allowedReplicationPipelineTypes = map[string]struct{}{
	"logical":          {},
	"etl":              {},
	"analytics_bucket": {},
}

var allowedReplicationDestinations = map[string]struct{}{
	"postgres":  {},
	"webhook":   {},
	"s3":        {},
	"iceberg":   {},
	"bigquery":  {},
	"snowflake": {},
	"redshift":  {},
}

var allowedEmbeddingProviders = map[string]struct{}{
	"openai":      {},
	"huggingface": {},
	"local":       {},
}

var allowedVectorBucketBackends = map[string]struct{}{
	"postgres": {},
	"s3":       {},
}

var allowedVectorBucketDistances = map[string]struct{}{
	"cosine": {},
	"l2":     {},
	"ip":     {},
}

var allowedVectorBucketIndexes = map[string]struct{}{
	"none":    {},
	"hnsw":    {},
	"ivfflat": {},
}

var reservedDatabaseRoles = map[string]struct{}{
	"anon":                       {},
	"authenticated":              {},
	"authenticator":              {},
	"dashboard_user":             {},
	"pgbouncer":                  {},
	"postgres":                   {},
	"service_role":               {},
	"supabase_admin":             {},
	"supabase_auth_admin":        {},
	"supabase_functions_admin":   {},
	"supabase_read_only_user":    {},
	"supabase_replication_admin": {},
	"supabase_storage_admin":     {},
}

var allowedDatabaseRolePrivileges = map[string]struct{}{
	"usage":  {},
	"create": {},
	"select": {},
	"insert": {},
	"update": {},
	"delete": {},
	"all":    {},
}

var allowedOAuthClientGrantTypes = map[string]struct{}{
	"authorization_code": {},
	"refresh_token":      {},
	"client_credentials": {},
}

var allowedOAuthClientScopes = map[string]struct{}{
	"openid":         {},
	"profile":        {},
	"email":          {},
	"phone":          {},
	"offline_access": {},
}

var allowedAuthHookTypes = map[string]struct{}{
	"before_user_created":           {},
	"custom_access_token":           {},
	"send_sms":                      {},
	"send_email":                    {},
	"mfa_verification_attempt":      {},
	"password_verification_attempt": {},
}

var defaultDatabaseExtensionOrder = []string{
	"pgcrypto",
	"uuid-ossp",
	"pg_graphql",
	"pg_stat_statements",
	"pg_cron",
	"pgmq",
	"vector",
	"supabase_vault",
}

var defaultDatabaseExtensions = map[string]ProjectDatabaseExtension{
	"pgcrypto":           {Name: "pgcrypto", Schema: "extensions", Enabled: true, Status: "enabled", Message: "enabled by Compose init SQL"},
	"uuid-ossp":          {Name: "uuid-ossp", Schema: "extensions", Enabled: true, Status: "enabled", Message: "enabled by Compose init SQL"},
	"pg_graphql":         {Name: "pg_graphql", Schema: "graphql", Enabled: true, Status: "enabled", Message: "enabled by Compose init SQL"},
	"pg_stat_statements": {Name: "pg_stat_statements", Schema: "extensions", Enabled: true, Status: "enabled", Message: "enabled by Compose init SQL"},
	"pg_cron":            {Name: "pg_cron", Schema: "extensions", Enabled: true, Status: "enabled", Message: "enabled by Compose init SQL"},
	"pgmq":               {Name: "pgmq", Schema: "extensions", Enabled: true, Status: "enabled", Message: "enabled by Compose init SQL"},
	"vector":             {Name: "vector", Schema: "extensions", Enabled: true, Status: "enabled", Message: "enabled by Compose init SQL"},
	"supabase_vault":     {Name: "supabase_vault", Schema: "vault", Enabled: true, Status: "enabled", Message: "enabled by Compose init SQL"},
}

var allowedNetworkConnectionTypes = map[string]struct{}{
	"privatelink":      {},
	"vpc_peering":      {},
	"private_endpoint": {},
	"wireguard":        {},
	"operator_network": {},
}

var allowedNetworkConnectionProviders = map[string]struct{}{
	"aws":      {},
	"gcp":      {},
	"azure":    {},
	"custom":   {},
	"operator": {},
}

var allowedProjectServices = []string{
	"auth",
	"rest",
	"graphql",
	"realtime",
	"storage",
	"imgproxy",
	"functions",
	"pooler",
	"studio",
	"analytics",
	"vector",
}

var allowedMembershipRoles = map[string]struct{}{
	"owner":     {},
	"admin":     {},
	"developer": {},
	"viewer":    {},
}

var defaultPlatformFeatureFlags = map[string]bool{
	"single_org_mode":       true,
	"multi_org":             false,
	"resource_quotas":       false,
	"team_rbac":             true,
	"project_access_grants": true,
	"project_self_service":  true,
	"service_toggles":       true,
	"supabase_cli_compat":   true,
	"custom_domains":        false,
	"network_restrictions":  false,
	"log_drains":            false,
	"pitr":                  false,
	"preview_branches":      false,
	"read_replicas":         false,
	"edge_functions":        true,
	"ai_integrations":       false,
	"usage_metering":        false,
	"billing":               false,
	"platform_sso_scim":     false,
	"kubernetes_operator":   false,
	// Master switch: publish project databases through the edge router. Off by
	// default so nothing is externally reachable until an operator enables it.
	"database_external_access": false,
	// When on, the security advisor holds platform-wide recovery posture
	// (backup-target guards and recovery-ready targets) to full severity. Off by
	// default so a local/MVP deploy isn't a wall of high-severity findings.
	"production_posture": false,
}

// Bounds for exact project sizing. A zero value falls back to the recommended
// size for the selected profile and service set.
const (
	maxProjectCPU                  = 64     // cores
	minProjectRAMMB                = 256    // MB
	maxProjectRAMMB                = 262144 // 256 GB
	maxProjectDiskGB               = 16384  // 16 TB
	recommendedCPUHeadroomPercent  = 20
	recommendedRAMHeadroomPercent  = 25
	recommendedDiskHeadroomPercent = 20
	recommendedRAMRoundingStepMB   = 256
	recommendedDiskRoundingStepGB  = 5
	telemetryHistoryRetention      = 30 * 24 * time.Hour
	telemetryHistoryRawRetention   = 24 * time.Hour
	telemetryHistoryRollupStep     = 5 * time.Minute
	telemetryHistoryMaxPoints      = 1000
	telemetryMaxFutureSkew         = 5 * time.Minute
)

// Replica and legacy preset reservations. Main project create/resize flows store
// exact custom sizing; replica creation still uses tiered placement choices.
var resourceTierReservations = map[ResourceTier]HostCapacity{
	ResourceTierSmall:  {CPU: 2, RAMMB: 4096, DiskGB: 40, Project: 1},
	ResourceTierMedium: {CPU: 4, RAMMB: 8192, DiskGB: 80, Project: 1},
	ResourceTierLarge:  {CPU: 8, RAMMB: 16384, DiskGB: 160, Project: 1},
}
