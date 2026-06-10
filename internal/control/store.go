package control

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// Bounds for optional exact-size overrides on project creation. These cap what
// an advanced user can request per dimension; a zero value falls back to the
// tier preset.
const (
	maxProjectCPU    = 64     // cores
	minProjectRAMMB  = 256    // MB
	maxProjectRAMMB  = 262144 // 256 GB
	maxProjectDiskGB = 16384  // 16 TB
)

var resourceTierReservations = map[ResourceTier]HostCapacity{
	ResourceTierSmall:  {CPU: 1, RAMMB: 2048, DiskGB: 20, Project: 1},
	ResourceTierMedium: {CPU: 2, RAMMB: 4096, DiskGB: 50, Project: 1},
	ResourceTierLarge:  {CPU: 4, RAMMB: 8192, DiskGB: 100, Project: 1},
}

type Store interface {
	CreateUser(ctx context.Context, req CreateUserRequest) (User, error)
	UpdateUser(ctx context.Context, id string, req UpdateUserRequest) (User, error)
	DeleteUser(ctx context.Context, id string) error
	ListUsers(ctx context.Context) ([]User, error)
	GetUserByID(ctx context.Context, id string) (User, error)
	AuthenticateUser(ctx context.Context, email string, password string) (User, error)
	RecordUserLogin(ctx context.Context, userID string) (time.Time, error)
	VerifyUserMFA(ctx context.Context, userID string, code string) (User, error)
	GetUserMFAStatus(ctx context.Context, userID string) (MFAStatus, error)
	BeginUserMFAEnrollment(ctx context.Context, userID string) (MFAEnrollment, error)
	ConfirmUserMFA(ctx context.Context, userID string, code string) (MFAStatus, error)
	DisableUserMFA(ctx context.Context, userID string, code string) (MFAStatus, error)
	HasUsers(ctx context.Context) bool
	CreateOrg(ctx context.Context, name string) (Org, error)
	UpdateOrg(ctx context.Context, id string, name string) (Org, error)
	DeleteOrg(ctx context.Context, id string) error
	ListOrgs(ctx context.Context) ([]Org, error)
	GetOrg(ctx context.Context, id string) (Org, error)
	GetOrgQuota(ctx context.Context, orgID string) (OrgQuota, error)
	UpdateOrgQuota(ctx context.Context, orgID string, input OrgQuotaInput) (OrgQuota, error)
	GetOrgFeatureFlags(ctx context.Context, orgID string) (OrgFeatureFlags, error)
	UpdateOrgFeatureFlags(ctx context.Context, orgID string, input OrgFeatureFlagsInput) (OrgFeatureFlags, error)
	GetOrgUsage(ctx context.Context, orgID string) (OrgUsage, error)
	ListOrgUsageSnapshots(ctx context.Context, orgID string, limit int) ([]UsageSnapshot, error)
	CreateOrgUsageSnapshot(ctx context.Context, orgID string) (UsageSnapshot, error)
	ListBillingInvoices(ctx context.Context, orgID string, limit int) ([]BillingInvoice, error)
	GetBillingInvoice(ctx context.Context, orgID string, invoiceID string) (BillingInvoice, error)
	CreateBillingInvoice(ctx context.Context, orgID string, input BillingInvoiceInput) (BillingInvoice, error)
	GetOrgAccessReview(ctx context.Context, orgID string) (OrgAccessReview, error)
	GetPlatformDefaults(ctx context.Context) (PlatformDefaults, error)
	UpdatePlatformDefaults(ctx context.Context, input PlatformDefaultsInput) (PlatformDefaults, error)
	GetPlatformSSOConfig(ctx context.Context) (PlatformSSOConfig, error)
	UpdatePlatformSSOConfig(ctx context.Context, input PlatformSSOConfigInput) (PlatformSSOConfig, error)
	ListOrgMembers(ctx context.Context, orgID string) ([]Membership, error)
	UpsertOrgMember(ctx context.Context, orgID string, input MembershipInput) (Membership, error)
	DeleteOrgMember(ctx context.Context, orgID string, email string) error
	ListOrgTeams(ctx context.Context, orgID string) ([]Team, error)
	CreateOrgTeam(ctx context.Context, orgID string, input TeamInput) (Team, error)
	DeleteOrgTeam(ctx context.Context, orgID string, slug string) error
	ListTeamMembers(ctx context.Context, orgID string, slug string) ([]TeamMember, error)
	UpsertTeamMember(ctx context.Context, orgID string, slug string, input TeamMemberInput) (TeamMember, error)
	DeleteTeamMember(ctx context.Context, orgID string, slug string, email string) error
	ListProjectAccess(ctx context.Context, ref string) ([]ProjectAccessGrant, error)
	UpsertProjectAccess(ctx context.Context, ref string, input ProjectAccessInput) (ProjectAccessGrant, error)
	DeleteProjectAccess(ctx context.Context, ref string, subjectType string, subjectID string) error
	ResolveProjectRole(ctx context.Context, ref string, email string) (string, error)
	CreateHost(ctx context.Context, req CreateHostRequest) (Host, error)
	ListHosts(ctx context.Context) ([]Host, error)
	GetHost(ctx context.Context, id string) (Host, error)
	DeleteHost(ctx context.Context, id string) error
	CreateProject(ctx context.Context, req CreateProjectRequest) (Project, error)
	CreateProjectBranch(ctx context.Context, sourceRef string, input ProjectBranchInput) (ProjectBranch, Project, error)
	ListProjectBranches(ctx context.Context, sourceRef string) ([]ProjectBranch, error)
	DeleteProjectBranch(ctx context.Context, sourceRef string, branchRef string) error
	CreateProjectReplica(ctx context.Context, ref string, input ProjectReplicaInput) (ProjectReplica, error)
	ListProjectReplicas(ctx context.Context, ref string) ([]ProjectReplica, error)
	UpdateProjectReplicaStatus(ctx context.Context, ref string, replicaID string, status string, message string) (ProjectReplica, error)
	DeleteProjectReplica(ctx context.Context, ref string, replicaID string) error
	GetProjectReplicaRouting(ctx context.Context, ref string) (ProjectReplicaRouting, error)
	PromoteProjectReplica(ctx context.Context, ref string, replicaID string, reason string) (ProjectReplica, error)
	FailoverProjectReplica(ctx context.Context, ref string, reason string) (ProjectReplica, error)
	ListProjects(ctx context.Context) ([]Project, error)
	ListProjectsByOrg(ctx context.Context, orgID string) ([]Project, error)
	GetProject(ctx context.Context, ref string) (Project, error)
	UpdateProjectStatus(ctx context.Context, ref string, status ProjectPhase, message string) (Project, error)
	UpdateProjectStackVersion(ctx context.Context, ref string, version string) (Project, error)
	UpdateProjectResourceTier(ctx context.Context, ref string, tier ResourceTier) (Project, error)
	GetProjectServices(ctx context.Context, ref string) (ProjectServices, error)
	UpdateProjectServices(ctx context.Context, ref string, input ProjectServicesInput) (Project, error)
	DeleteProject(ctx context.Context, ref string) error
	UpsertProjectRoutes(ctx context.Context, ref string, routes []ProjectRoute) ([]ProjectRoute, error)
	ListProjectRoutes(ctx context.Context, ref string) ([]ProjectRoute, error)
	DeleteProjectRoutes(ctx context.Context, ref string) error
	ListProjectDomains(ctx context.Context, ref string) ([]ProjectDomain, error)
	AddProjectDomain(ctx context.Context, ref string, input ProjectDomainInput) (ProjectDomain, error)
	UpdateProjectDomainCertStatus(ctx context.Context, ref string, fqdn string, status string) (ProjectDomain, error)
	UpdateProjectDomainCertificate(ctx context.Context, ref string, fqdn string, metadata ProjectDomainCertificateMetadata) (ProjectDomain, error)
	DeleteProjectDomain(ctx context.Context, ref string, fqdn string) error
	GetProjectConfig(ctx context.Context, ref string, area string) (ProjectConfig, error)
	ListProjectConfigs(ctx context.Context, ref string) ([]ProjectConfig, error)
	UpdateProjectConfig(ctx context.Context, ref string, area string, input ProjectConfigInput) (ProjectConfig, error)
	ListProjectAuthClients(ctx context.Context, ref string) ([]ProjectAuthClient, error)
	CreateProjectAuthClient(ctx context.Context, ref string, input ProjectAuthClientInput) (ProjectAuthClient, error)
	DeleteProjectAuthClient(ctx context.Context, ref string, clientID string) error
	ListProjectAuthHooks(ctx context.Context, ref string) ([]ProjectAuthHook, error)
	CreateProjectAuthHook(ctx context.Context, ref string, input ProjectAuthHookInput) (ProjectAuthHook, error)
	DeleteProjectAuthHook(ctx context.Context, ref string, hookType string) error
	ListProjectFunctions(ctx context.Context, ref string) ([]ProjectFunction, error)
	DeployProjectFunction(ctx context.Context, ref string, input ProjectFunctionInput) (ProjectFunction, error)
	DeleteProjectFunction(ctx context.Context, ref string, name string) error
	ListProjectFunctionRegions(ctx context.Context, ref string) ([]ProjectFunctionRegion, error)
	CreateProjectFunctionRegion(ctx context.Context, ref string, input ProjectFunctionRegionInput) (ProjectFunctionRegion, error)
	DeleteProjectFunctionRegion(ctx context.Context, ref string, id string) error
	ListProjectFunctionStorageMounts(ctx context.Context, ref string) ([]ProjectFunctionStorageMount, error)
	CreateProjectFunctionStorageMount(ctx context.Context, ref string, input ProjectFunctionStorageMountInput) (ProjectFunctionStorageMount, error)
	DeleteProjectFunctionStorageMount(ctx context.Context, ref string, id string) error
	ListProjectReplicationPipelines(ctx context.Context, ref string) ([]ProjectReplicationPipeline, error)
	CreateProjectReplicationPipeline(ctx context.Context, ref string, input ProjectReplicationPipelineInput) (ProjectReplicationPipeline, error)
	DeleteProjectReplicationPipeline(ctx context.Context, ref string, id string) error
	ListProjectEmbeddingJobs(ctx context.Context, ref string) ([]ProjectEmbeddingJob, error)
	CreateProjectEmbeddingJob(ctx context.Context, ref string, input ProjectEmbeddingJobInput) (ProjectEmbeddingJob, error)
	DeleteProjectEmbeddingJob(ctx context.Context, ref string, id string) error
	ListProjectDatabaseExtensions(ctx context.Context, ref string) ([]ProjectDatabaseExtension, error)
	UpdateProjectDatabaseExtension(ctx context.Context, ref string, name string, input ProjectDatabaseExtensionInput) (ProjectDatabaseExtension, error)
	ListProjectDatabaseCronJobs(ctx context.Context, ref string) ([]ProjectDatabaseCronJob, error)
	CreateProjectDatabaseCronJob(ctx context.Context, ref string, input ProjectDatabaseCronJobInput) (ProjectDatabaseCronJob, error)
	DeleteProjectDatabaseCronJob(ctx context.Context, ref string, name string) error
	ListProjectDatabaseQueues(ctx context.Context, ref string) ([]ProjectDatabaseQueue, error)
	CreateProjectDatabaseQueue(ctx context.Context, ref string, input ProjectDatabaseQueueInput) (ProjectDatabaseQueue, error)
	DeleteProjectDatabaseQueue(ctx context.Context, ref string, name string) error
	ListProjectDatabaseWebhooks(ctx context.Context, ref string) ([]ProjectDatabaseWebhook, error)
	CreateProjectDatabaseWebhook(ctx context.Context, ref string, input ProjectDatabaseWebhookInput) (ProjectDatabaseWebhook, error)
	DeleteProjectDatabaseWebhook(ctx context.Context, ref string, name string) error
	ListProjectDatabaseSchemas(ctx context.Context, ref string) ([]ProjectDatabaseSchema, error)
	CreateProjectDatabaseSchema(ctx context.Context, ref string, input ProjectDatabaseSchemaInput) (ProjectDatabaseSchema, error)
	DeleteProjectDatabaseSchema(ctx context.Context, ref string, name string, version string) error
	ListProjectDatabaseRoles(ctx context.Context, ref string) ([]ProjectDatabaseRole, error)
	CreateProjectDatabaseRole(ctx context.Context, ref string, input ProjectDatabaseRoleInput) (ProjectDatabaseRole, error)
	DeleteProjectDatabaseRole(ctx context.Context, ref string, name string) error
	ListProjectStorageBuckets(ctx context.Context, ref string) ([]ProjectStorageBucket, error)
	CreateProjectStorageBucket(ctx context.Context, ref string, input ProjectStorageBucketInput) (ProjectStorageBucket, error)
	DeleteProjectStorageBucket(ctx context.Context, ref string, name string) error
	ListProjectVectorBuckets(ctx context.Context, ref string) ([]ProjectVectorBucket, error)
	CreateProjectVectorBucket(ctx context.Context, ref string, input ProjectVectorBucketInput) (ProjectVectorBucket, error)
	DeleteProjectVectorBucket(ctx context.Context, ref string, name string) error
	ListProjectAnalyticsBuckets(ctx context.Context, ref string) ([]ProjectAnalyticsBucket, error)
	CreateProjectAnalyticsBucket(ctx context.Context, ref string, input ProjectAnalyticsBucketInput) (ProjectAnalyticsBucket, error)
	DeleteProjectAnalyticsBucket(ctx context.Context, ref string, name string) error
	GetProjectCDNPolicy(ctx context.Context, ref string) (ProjectCDNPolicy, error)
	UpdateProjectCDNPolicy(ctx context.Context, ref string, input ProjectCDNPolicyInput) (ProjectCDNPolicy, error)
	ListProjectCDNInvalidations(ctx context.Context, ref string) ([]CDNInvalidation, error)
	CreateProjectCDNInvalidation(ctx context.Context, ref string, input CDNInvalidationInput) (CDNInvalidation, error)
	CreateProjectCDNObjectEvent(ctx context.Context, ref string, input CDNObjectEventInput) (CDNInvalidation, error)
	ListProjectNetworkConnections(ctx context.Context, ref string) ([]ProjectNetworkConnection, error)
	CreateProjectNetworkConnection(ctx context.Context, ref string, input ProjectNetworkConnectionInput) (ProjectNetworkConnection, error)
	DeleteProjectNetworkConnection(ctx context.Context, ref string, id string) error
	ListProjectLogDrains(ctx context.Context, ref string) ([]LogDrain, error)
	CreateProjectLogDrain(ctx context.Context, ref string, input LogDrainInput) (LogDrain, error)
	UpdateProjectLogDrain(ctx context.Context, ref string, id string, input LogDrainInput) (LogDrain, error)
	DeleteProjectLogDrain(ctx context.Context, ref string, id string) error
	EnsureProjectSecrets(ctx context.Context, ref string) ([]ProjectSecret, error)
	ListProjectSecrets(ctx context.Context, ref string) ([]ProjectSecret, error)
	UpsertProjectSecret(ctx context.Context, ref string, kind string, input ProjectSecretInput) (ProjectSecret, error)
	DeleteProjectSecret(ctx context.Context, ref string, kind string) error
	RevealProjectSecret(ctx context.Context, ref string, kind string) (ProjectSecret, error)
	RotateProjectSecret(ctx context.Context, ref string, kind string) (ProjectSecret, error)
	CreateBackup(ctx context.Context, input BackupInput) (Backup, error)
	ListBackups(ctx context.Context, ref string) ([]Backup, error)
	GetBackup(ctx context.Context, ref string, backupID string) (Backup, error)
	CreatePlatformBackup(ctx context.Context, input PlatformBackupInput) (PlatformBackup, error)
	ListPlatformBackups(ctx context.Context) ([]PlatformBackup, error)
	GetPlatformBackup(ctx context.Context, backupID string) (PlatformBackup, error)
	ListBackupStorageTargets(ctx context.Context) ([]BackupStorageTarget, error)
	GetBackupStorageTarget(ctx context.Context, id string) (BackupStorageTarget, error)
	CreateBackupStorageTarget(ctx context.Context, input BackupStorageTargetInput) (BackupStorageTarget, error)
	UpdateBackupStorageTarget(ctx context.Context, id string, input BackupStorageTargetInput) (BackupStorageTarget, error)
	UpdateBackupStorageTargetTestResult(ctx context.Context, id string, testedAt time.Time, status string, message string) (BackupStorageTarget, error)
	DeleteBackupStorageTarget(ctx context.Context, id string) error
	GetBackupPolicy(ctx context.Context, ref string) (BackupPolicy, error)
	UpdateBackupPolicy(ctx context.Context, ref string, input BackupPolicyInput) (BackupPolicy, error)
	MarkBackupPolicyRun(ctx context.Context, ref string, runAt time.Time) (BackupPolicy, error)
	GetPITRPolicy(ctx context.Context, ref string) (PITRPolicy, error)
	UpdatePITRPolicy(ctx context.Context, ref string, input PITRPolicyInput) (PITRPolicy, error)
	CreateWALArchive(ctx context.Context, input WALArchiveInput) (WALArchive, error)
	ListWALArchives(ctx context.Context, ref string) ([]WALArchive, error)
	RecordProjectLog(ctx context.Context, input ProjectLogInput) (ProjectLog, error)
	ListProjectLogs(ctx context.Context, ref string, limit int) ([]ProjectLog, error)
	RecordAuditEvent(ctx context.Context, event AuditEventInput) (AuditEvent, error)
	ListAuditEvents(ctx context.Context, limit int) ([]AuditEvent, error)
	ListAuditEventsPage(ctx context.Context, query AuditEventQuery) (AuditEventPage, error)
	ListProjectAuditEvents(ctx context.Context, ref string, limit int) ([]AuditEvent, error)
	VerifyAuditLog(ctx context.Context) (AuditIntegrity, error)
	GetFleetMetrics(ctx context.Context) (FleetMetrics, error)
	GetProjectMetrics(ctx context.Context, ref string) (ProjectMetrics, error)
	RecordProjectTelemetry(ctx context.Context, ref string, input TelemetrySampleInput) (TelemetrySample, error)
	RecordNodeTelemetry(ctx context.Context, hostID string, input NodeTelemetrySampleInput) (NodeTelemetrySample, error)
}

type User struct {
	ID               string    `json:"id"`
	Email            string    `json:"email"`
	PasswordHash     string    `json:"-"`
	Role             string    `json:"role"`
	MFAEnabled       bool      `json:"mfa_enabled"`
	MFASecret        string    `json:"-"`
	MFAPendingSecret string    `json:"-"`
	MFAConfirmedAt   time.Time `json:"-"`
	MFAUpdatedAt     time.Time `json:"-"`
	MFALastCounter   int64     `json:"-"`
	CreatedAt        time.Time `json:"created_at"`
	// LastLoginAt is the most recent successful login (post-MFA). Nil until the
	// user has logged in at least once, so the API omits it rather than emitting
	// a zero date.
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

type CreateUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type UpdateUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type MFAStatus struct {
	UserID      string     `json:"user_id"`
	Email       string     `json:"email"`
	Enabled     bool       `json:"enabled"`
	Pending     bool       `json:"pending"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type MFAEnrollment struct {
	MFAStatus
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
}

type Org struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	FeatureFlagOverrides map[string]bool `json:"feature_flag_overrides"`
	FeatureFlags         map[string]bool `json:"feature_flags"`
	CreatedAt            time.Time       `json:"created_at"`
}

type OrgFeatureFlags struct {
	OrgID     string          `json:"org_id"`
	Defaults  map[string]bool `json:"defaults"`
	Overrides map[string]bool `json:"overrides"`
	Effective map[string]bool `json:"effective"`
}

type OrgFeatureFlagsInput struct {
	Overrides map[string]bool `json:"overrides"`
}

type OrgQuota struct {
	OrgID       string       `json:"org_id"`
	MaxProjects int          `json:"max_projects"`
	MaxCPU      int          `json:"max_cpu"`
	MaxRAMMB    int          `json:"max_ram_mb"`
	MaxDiskGB   int          `json:"max_disk_gb"`
	Used        HostCapacity `json:"used"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type OrgQuotaInput struct {
	MaxProjects int `json:"max_projects"`
	MaxCPU      int `json:"max_cpu"`
	MaxRAMMB    int `json:"max_ram_mb"`
	MaxDiskGB   int `json:"max_disk_gb"`
}

type OrgUsage struct {
	OrgID                 string         `json:"org_id"`
	Resources             HostCapacity   `json:"resources"`
	ProjectsByStatus      map[string]int `json:"projects_by_status"`
	ReadReplicas          int            `json:"read_replicas"`
	BackupCount           int            `json:"backup_count"`
	BackupStorageBytes    int64          `json:"backup_storage_bytes"`
	WALArchives           int            `json:"wal_archives"`
	WALArchiveBytes       int64          `json:"wal_archive_bytes"`
	ProjectLogEvents      int            `json:"project_log_events"`
	CustomDomains         int            `json:"custom_domains"`
	LogDrains             int            `json:"log_drains"`
	FunctionDeployments   int            `json:"function_deployments"`
	FunctionRegions       int            `json:"function_regions"`
	FunctionStorageMounts int            `json:"function_storage_mounts"`
	ReplicationPipelines  int            `json:"replication_pipelines"`
	EmbeddingJobs         int            `json:"embedding_jobs"`
	AuthClients           int            `json:"auth_clients"`
	AuthHooks             int            `json:"auth_hooks"`
	DatabaseExtensions    int            `json:"database_extensions"`
	DatabaseCronJobs      int            `json:"database_cron_jobs"`
	DatabaseQueues        int            `json:"database_queues"`
	DatabaseWebhooks      int            `json:"database_webhooks"`
	DatabaseSchemas       int            `json:"database_schemas"`
	DatabaseRoles         int            `json:"database_roles"`
	StorageBuckets        int            `json:"storage_buckets"`
	VectorBuckets         int            `json:"vector_buckets"`
	AnalyticsBuckets      int            `json:"analytics_buckets"`
	CDNEnabledProjects    int            `json:"cdn_enabled_projects"`
	CDNInvalidations      int            `json:"cdn_invalidations"`
	NetworkConnections    int            `json:"network_connections"`
	Secrets               int            `json:"secrets"`
	DBAllocatedBytes      int64          `json:"db_allocated_bytes"`
	StorageBytes          int64          `json:"storage_bytes"`
	EgressBytes           int64          `json:"egress_bytes"`
	FunctionInvocations   int64          `json:"function_invocations"`
	AuthMAUs              int            `json:"auth_maus"`
	SampledAt             time.Time      `json:"sampled_at"`
}

type UsageSnapshot struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Metrics   OrgUsage  `json:"metrics"`
	SampledAt time.Time `json:"sampled_at"`
}

type BillingLineItem struct {
	Key            string `json:"key"`
	Description    string `json:"description"`
	Quantity       int64  `json:"quantity"`
	Unit           string `json:"unit"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	AmountCents    int64  `json:"amount_cents"`
}

type BillingInvoice struct {
	ID              string            `json:"id"`
	OrgID           string            `json:"org_id"`
	UsageSnapshotID string            `json:"usage_snapshot_id"`
	Number          string            `json:"number"`
	Status          string            `json:"status"`
	Currency        string            `json:"currency"`
	PeriodStart     time.Time         `json:"period_start"`
	PeriodEnd       time.Time         `json:"period_end"`
	DueAt           time.Time         `json:"due_at"`
	SubtotalCents   int64             `json:"subtotal_cents"`
	TotalCents      int64             `json:"total_cents"`
	LineItems       []BillingLineItem `json:"line_items"`
	Metrics         OrgUsage          `json:"metrics"`
	CreatedAt       time.Time         `json:"created_at"`
}

type BillingInvoiceInput struct {
	UsageSnapshotID string `json:"usage_snapshot_id"`
	Currency        string `json:"currency"`
	Status          string `json:"status"`
	DueDays         int    `json:"due_days"`
}

type FleetMetrics struct {
	Orgs                  int                   `json:"orgs"`
	Users                 int                   `json:"users"`
	Hosts                 int                   `json:"hosts"`
	Projects              int                   `json:"projects"`
	ReadReplicas          int                   `json:"read_replicas"`
	ProjectsByStatus      map[string]int        `json:"projects_by_status"`
	HostCapacity          HostCapacity          `json:"host_capacity"`
	HostUsed              HostCapacity          `json:"host_used"`
	DatabaseIngress       DatabaseIngressStatus `json:"database_ingress"`
	NodeObserved          []NodeTelemetrySample `json:"node_observed"`
	Observed              TelemetryRollup       `json:"observed"`
	Routes                int                   `json:"routes"`
	CustomDomains         int                   `json:"custom_domains"`
	LogDrains             int                   `json:"log_drains"`
	FunctionDeployments   int                   `json:"function_deployments"`
	FunctionRegions       int                   `json:"function_regions"`
	FunctionStorageMounts int                   `json:"function_storage_mounts"`
	ReplicationPipelines  int                   `json:"replication_pipelines"`
	EmbeddingJobs         int                   `json:"embedding_jobs"`
	AuthClients           int                   `json:"auth_clients"`
	AuthHooks             int                   `json:"auth_hooks"`
	DatabaseExtensions    int                   `json:"database_extensions"`
	DatabaseCronJobs      int                   `json:"database_cron_jobs"`
	DatabaseQueues        int                   `json:"database_queues"`
	DatabaseWebhooks      int                   `json:"database_webhooks"`
	DatabaseSchemas       int                   `json:"database_schemas"`
	DatabaseRoles         int                   `json:"database_roles"`
	StorageBuckets        int                   `json:"storage_buckets"`
	VectorBuckets         int                   `json:"vector_buckets"`
	AnalyticsBuckets      int                   `json:"analytics_buckets"`
	CDNEnabledProjects    int                   `json:"cdn_enabled_projects"`
	CDNInvalidations      int                   `json:"cdn_invalidations"`
	NetworkConnections    int                   `json:"network_connections"`
	Backups               int                   `json:"backups"`
	BackupStorageBytes    int64                 `json:"backup_storage_bytes"`
	WALArchives           int                   `json:"wal_archives"`
	WALArchiveBytes       int64                 `json:"wal_archive_bytes"`
	ProjectLogEvents      int                   `json:"project_log_events"`
	AuditEvents           int                   `json:"audit_events"`
	AuditVerified         bool                  `json:"audit_verified"`
	SampledAt             time.Time             `json:"sampled_at"`
}

type DatabaseIngressStatus struct {
	Mode                string   `json:"mode"`
	Public              bool     `json:"public"`
	PostgresAddr        string   `json:"postgres_addr"`
	PoolerAddr          string   `json:"pooler_addr"`
	PostgresPublic      bool     `json:"postgres_public"`
	PoolerPublic        bool     `json:"pooler_public"`
	AllowlistConfigured bool     `json:"allowlist_configured"`
	AllowedCIDRs        []string `json:"allowed_cidrs"`
	Warnings            []string `json:"warnings"`
}

type ProjectMetrics struct {
	ProjectRef            string           `json:"project_ref"`
	OrgID                 string           `json:"org_id"`
	Status                ProjectPhase     `json:"status"`
	ResourceTier          ResourceTier     `json:"resource_tier"`
	Resources             HostCapacity     `json:"resources"`
	Observed              *TelemetrySample `json:"observed,omitempty"`
	ReadReplicas          int              `json:"read_replicas"`
	Routes                int              `json:"routes"`
	CustomDomains         int              `json:"custom_domains"`
	LogDrains             int              `json:"log_drains"`
	FunctionDeployments   int              `json:"function_deployments"`
	FunctionRegions       int              `json:"function_regions"`
	FunctionStorageMounts int              `json:"function_storage_mounts"`
	ReplicationPipelines  int              `json:"replication_pipelines"`
	EmbeddingJobs         int              `json:"embedding_jobs"`
	AuthClients           int              `json:"auth_clients"`
	AuthHooks             int              `json:"auth_hooks"`
	DatabaseExtensions    int              `json:"database_extensions"`
	DatabaseCronJobs      int              `json:"database_cron_jobs"`
	DatabaseQueues        int              `json:"database_queues"`
	DatabaseWebhooks      int              `json:"database_webhooks"`
	DatabaseSchemas       int              `json:"database_schemas"`
	DatabaseRoles         int              `json:"database_roles"`
	StorageBuckets        int              `json:"storage_buckets"`
	VectorBuckets         int              `json:"vector_buckets"`
	AnalyticsBuckets      int              `json:"analytics_buckets"`
	CDNEnabled            bool             `json:"cdn_enabled"`
	CDNInvalidations      int              `json:"cdn_invalidations"`
	NetworkConnections    int              `json:"network_connections"`
	Backups               int              `json:"backups"`
	BackupStorageBytes    int64            `json:"backup_storage_bytes"`
	WALArchives           int              `json:"wal_archives"`
	WALArchiveBytes       int64            `json:"wal_archive_bytes"`
	ProjectLogEvents      int              `json:"project_log_events"`
	ActivityEvents        int              `json:"activity_events"`
	Secrets               int              `json:"secrets"`
	DBAllocatedBytes      int64            `json:"db_allocated_bytes"`
	StorageBytes          int64            `json:"storage_bytes"`
	EgressBytes           int64            `json:"egress_bytes"`
	FunctionInvocations   int64            `json:"function_invocations"`
	AuthMAUs              int              `json:"auth_maus"`
	SampledAt             time.Time        `json:"sampled_at"`
}

type TelemetrySample struct {
	ProjectRef       string    `json:"project_ref"`
	Source           string    `json:"source"`
	CPUPercent       float64   `json:"cpu_percent"`
	MemoryBytes      int64     `json:"memory_bytes"`
	MemoryLimitBytes int64     `json:"memory_limit_bytes"`
	DiskUsedBytes    int64     `json:"disk_used_bytes"`
	DiskLimitBytes   int64     `json:"disk_limit_bytes"`
	NetworkRxBytes   int64     `json:"network_rx_bytes"`
	NetworkTxBytes   int64     `json:"network_tx_bytes"`
	SampledAt        time.Time `json:"sampled_at"`
}

type TelemetrySampleInput struct {
	Source           string
	CPUPercent       float64
	MemoryBytes      int64
	MemoryLimitBytes int64
	DiskUsedBytes    int64
	DiskLimitBytes   int64
	NetworkRxBytes   int64
	NetworkTxBytes   int64
	SampledAt        time.Time
}

type TelemetryRollup struct {
	ProjectsSampled   int       `json:"projects_sampled"`
	CPUPercent        float64   `json:"cpu_percent"`
	MemoryBytes       int64     `json:"memory_bytes"`
	MemoryLimitBytes  int64     `json:"memory_limit_bytes"`
	DiskUsedBytes     int64     `json:"disk_used_bytes"`
	DiskLimitBytes    int64     `json:"disk_limit_bytes"`
	NetworkRxBytes    int64     `json:"network_rx_bytes"`
	NetworkTxBytes    int64     `json:"network_tx_bytes"`
	LatestSampledAt   time.Time `json:"latest_sampled_at,omitempty"`
	OldestSampledAt   time.Time `json:"oldest_sampled_at,omitempty"`
	StaleProjects     int       `json:"stale_projects"`
	StaleAfterSeconds int       `json:"stale_after_seconds"`
}

type NodeTelemetrySample struct {
	HostID             string    `json:"host_id"`
	Source             string    `json:"source"`
	CPUPercent         float64   `json:"cpu_percent"`
	CPUUsedCores       float64   `json:"cpu_used_cores"`
	CPUCapacityCores   int       `json:"cpu_capacity_cores"`
	MemoryUsedBytes    int64     `json:"memory_used_bytes"`
	MemoryTotalBytes   int64     `json:"memory_total_bytes"`
	DiskUsedBytes      int64     `json:"disk_used_bytes"`
	DiskTotalBytes     int64     `json:"disk_total_bytes"`
	DiskAvailableBytes int64     `json:"disk_available_bytes"`
	NetworkSampled     bool      `json:"network_sampled"`
	NetworkRxBytes     int64     `json:"network_rx_bytes"`
	NetworkTxBytes     int64     `json:"network_tx_bytes"`
	SampledAt          time.Time `json:"sampled_at"`
}

type NodeTelemetrySampleInput struct {
	Source             string
	CPUPercent         float64
	CPUUsedCores       float64
	CPUCapacityCores   int
	MemoryUsedBytes    int64
	MemoryTotalBytes   int64
	DiskUsedBytes      int64
	DiskTotalBytes     int64
	DiskAvailableBytes int64
	NetworkSampled     bool
	NetworkRxBytes     int64
	NetworkTxBytes     int64
	SampledAt          time.Time
}

type Membership struct {
	UserID    string    `json:"user_id"`
	OrgID     string    `json:"org_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type MembershipInput struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type Team struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

type TeamInput struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type TeamMember struct {
	TeamID    string    `json:"team_id"`
	OrgID     string    `json:"org_id"`
	TeamSlug  string    `json:"team_slug"`
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type TeamMemberInput struct {
	Email string `json:"email"`
}

type ProjectAccessGrant struct {
	ID          string    `json:"id"`
	ProjectRef  string    `json:"project_ref"`
	OrgID       string    `json:"org_id"`
	SubjectType string    `json:"subject_type"`
	SubjectID   string    `json:"subject_id"`
	SubjectName string    `json:"subject_name"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}

type ProjectAccessInput struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Role        string `json:"role"`
}

type OrgAccessReview struct {
	OrgID       string                `json:"org_id"`
	Members     []Membership          `json:"members"`
	Teams       []TeamAccessReview    `json:"teams"`
	Projects    []ProjectAccessReview `json:"projects"`
	GeneratedAt time.Time             `json:"generated_at"`
}

type TeamAccessReview struct {
	Team    Team         `json:"team"`
	Members []TeamMember `json:"members"`
}

type ProjectAccessReview struct {
	ProjectRef  string                 `json:"project_ref"`
	ProjectName string                 `json:"project_name"`
	Grants      []ProjectAccessGrant   `json:"grants"`
	Effective   []EffectiveProjectRole `json:"effective"`
}

type EffectiveProjectRole struct {
	UserID  string   `json:"user_id"`
	Email   string   `json:"email"`
	Role    string   `json:"role"`
	Sources []string `json:"sources"`
}

type Project struct {
	ID            string         `json:"id"`
	Ref           string         `json:"ref"`
	OrgID         string         `json:"org_id"`
	Name          string         `json:"name"`
	Status        ProjectPhase   `json:"status"`
	Message       string         `json:"message,omitempty"`
	Spec          ProjectSpec    `json:"spec"`
	RuntimeStatus *ProjectStatus `json:"runtime_status,omitempty"`
	// DBIngressMode is the project's configured database exposure
	// ("private"/"allowlisted"/"public"). Transient: hydrated from the project's
	// network config when building API responses, not persisted on the row.
	DBIngressMode string    `json:"db_ingress_mode,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CreateProjectRequest struct {
	OrgID         string            `json:"-"`
	Ref           string            `json:"ref"`
	Name          string            `json:"name"`
	HostID        string            `json:"host_id"`
	Domain        string            `json:"domain"`
	StackVersion  string            `json:"stack_version"`
	Profile       StackProfile      `json:"profile"`
	ResourceTier  ResourceTier      `json:"resource_tier"`
	CPU           int               `json:"cpu,omitempty"`
	RAMMB         int               `json:"ram_mb,omitempty"`
	DiskGB        int               `json:"disk_gb,omitempty"`
	EnforceLimits bool              `json:"enforce_limits,omitempty"`
	Services      map[string]bool   `json:"services"`
	Environment   map[string]string `json:"environment"`
}

type ProjectServices struct {
	ProjectRef string          `json:"project_ref"`
	Services   map[string]bool `json:"services"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type ProjectServicesInput struct {
	Services map[string]bool `json:"services"`
}

type PlatformDefaults struct {
	Domain                      string          `json:"domain"`
	StackVersion                string          `json:"stack_version"`
	Profile                     StackProfile    `json:"profile"`
	ResourceTier                ResourceTier    `json:"resource_tier"`
	BackupSchedule              string          `json:"backup_schedule"`
	FeatureFlags                map[string]bool `json:"feature_flags"`
	DatabaseIngressAllowedCIDRs []string        `json:"database_ingress_allowed_cidrs"`
	SMTP                        PlatformSMTP    `json:"smtp"`
	UpdatedAt                   time.Time       `json:"updated_at"`
}

type PlatformDefaultsInput struct {
	Domain                      string          `json:"domain"`
	StackVersion                string          `json:"stack_version"`
	Profile                     StackProfile    `json:"profile"`
	ResourceTier                ResourceTier    `json:"resource_tier"`
	BackupSchedule              string          `json:"backup_schedule"`
	FeatureFlags                map[string]bool `json:"feature_flags"`
	DatabaseIngressAllowedCIDRs []string        `json:"database_ingress_allowed_cidrs"`
	SMTP                        PlatformSMTP    `json:"smtp"`
}

type PlatformSMTP struct {
	Enabled        bool   `json:"enabled"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	SenderName     string `json:"sender_name"`
	SenderEmail    string `json:"sender_email"`
	Username       string `json:"username"`
	PasswordHandle string `json:"password_handle"`
	TLSMode        string `json:"tls_mode"`
}

type PlatformSSOConfig struct {
	Enabled             bool      `json:"enabled"`
	Provider            string    `json:"provider"`
	IDPEntityID         string    `json:"idp_entity_id"`
	SSOURL              string    `json:"sso_url"`
	Certificate         string    `json:"certificate_pem"`
	ACSURL              string    `json:"acs_url"`
	MetadataURL         string    `json:"metadata_url"`
	EmailDomain         string    `json:"email_domain"`
	AutoProvision       bool      `json:"auto_provision"`
	DefaultRole         string    `json:"default_role"`
	SCIMEnabled         bool      `json:"scim_enabled"`
	SCIMTokenHash       string    `json:"-"`
	SCIMTokenConfigured bool      `json:"scim_token_configured"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type PlatformSSOConfigInput struct {
	Enabled       bool   `json:"enabled"`
	IDPEntityID   string `json:"idp_entity_id"`
	SSOURL        string `json:"sso_url"`
	Certificate   string `json:"certificate_pem"`
	ACSURL        string `json:"acs_url"`
	MetadataURL   string `json:"metadata_url"`
	EmailDomain   string `json:"email_domain"`
	AutoProvision bool   `json:"auto_provision"`
	DefaultRole   string `json:"default_role"`
	SCIMEnabled   bool   `json:"scim_enabled"`
	SCIMToken     string `json:"scim_token,omitempty"`
}

type PlatformSSOInitiation struct {
	Enabled     bool      `json:"enabled"`
	Provider    string    `json:"provider"`
	IDPEntityID string    `json:"idp_entity_id,omitempty"`
	LoginURL    string    `json:"login_url,omitempty"`
	ACSURL      string    `json:"acs_url,omitempty"`
	MetadataURL string    `json:"metadata_url,omitempty"`
	RequestedAt time.Time `json:"requested_at"`
}

type PlatformSSOAssertion struct {
	Issuer       string            `json:"issuer"`
	Audience     string            `json:"audience"`
	Email        string            `json:"email"`
	NameID       string            `json:"name_id"`
	Role         string            `json:"role,omitempty"`
	NotOnOrAfter time.Time         `json:"not_on_or_after"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	Signature    string            `json:"signature"`
}

type Host struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Address   string       `json:"address"`
	Capacity  HostCapacity `json:"capacity"`
	Used      HostCapacity `json:"used"`
	CreatedAt time.Time    `json:"created_at"`
}

type HostCapacity struct {
	CPU     int `json:"cpu"`
	RAMMB   int `json:"ram_mb"`
	DiskGB  int `json:"disk_gb"`
	Project int `json:"projects"`
}

type CreateHostRequest struct {
	Name     string       `json:"name"`
	Address  string       `json:"address"`
	Capacity HostCapacity `json:"capacity"`
}

type AuditEvent struct {
	ID           string            `json:"id"`
	ActorID      string            `json:"actor_id,omitempty"`
	ChainIndex   int               `json:"chain_index"`
	PreviousHash string            `json:"previous_hash"`
	Hash         string            `json:"hash"`
	Action       string            `json:"action"`
	Target       string            `json:"target"`
	Metadata     map[string]string `json:"metadata"`
	CreatedAt    time.Time         `json:"created_at"`
}

type AuditEventQuery struct {
	Limit   int
	Offset  int
	Action  string
	ActorID string
	Since   time.Time
	Until   time.Time
}

type AuditEventPage struct {
	Events []AuditEvent `json:"events"`
	Total  int          `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

type AuditEventInput struct {
	ActorID  string
	Action   string
	Target   string
	Metadata map[string]string
}

type AuditIntegrity struct {
	Verified  bool      `json:"verified"`
	Events    int       `json:"events"`
	HeadHash  string    `json:"head_hash"`
	BrokenAt  int       `json:"broken_at,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

type Backup struct {
	ID              string     `json:"id"`
	ProjectRef      string     `json:"project_ref"`
	Kind            string     `json:"kind"`
	Location        string     `json:"location"`
	RemoteLocation  string     `json:"remote_location,omitempty"`
	StorageTargetID string     `json:"storage_target_id,omitempty"`
	SizeBytes       int64      `json:"size_bytes"`
	ChecksumSHA256  string     `json:"checksum_sha256"`
	Status          string     `json:"status"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	VerifiedAt      *time.Time `json:"verified_at,omitempty"`
}

type BackupInput struct {
	ProjectRef      string
	Kind            string
	Location        string
	RemoteLocation  string
	StorageTargetID string
	SizeBytes       int64
	ChecksumSHA256  string
	Status          string
	StartedAt       time.Time
	FinishedAt      *time.Time
	VerifiedAt      *time.Time
}

type PlatformBackup struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	Location        string     `json:"location"`
	RemoteLocation  string     `json:"remote_location,omitempty"`
	StorageTargetID string     `json:"storage_target_id,omitempty"`
	SizeBytes       int64      `json:"size_bytes"`
	ChecksumSHA256  string     `json:"checksum_sha256"`
	Status          string     `json:"status"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	VerifiedAt      *time.Time `json:"verified_at,omitempty"`
}

type PlatformBackupInput struct {
	Kind            string
	Location        string
	RemoteLocation  string
	StorageTargetID string
	SizeBytes       int64
	ChecksumSHA256  string
	Status          string
	StartedAt       time.Time
	FinishedAt      *time.Time
	VerifiedAt      *time.Time
}

type BackupStorageTarget struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Type             string     `json:"type"`
	Endpoint         string     `json:"endpoint"`
	Region           string     `json:"region"`
	Bucket           string     `json:"bucket"`
	Prefix           string     `json:"prefix,omitempty"`
	AccessKeyID      string     `json:"access_key_id,omitempty"`
	SecretAccessKey  string     `json:"-"`
	SecretConfigured bool       `json:"secret_configured"`
	ForcePathStyle   bool       `json:"force_path_style"`
	Default          bool       `json:"default"`
	DurableOffHost   bool       `json:"durable_off_host"`
	RecoveryReady    bool       `json:"recovery_ready"`
	ReadinessStatus  string     `json:"readiness_status"`
	ReadinessMessage string     `json:"readiness_message,omitempty"`
	LastTestedAt     *time.Time `json:"last_tested_at,omitempty"`
	LastTestStatus   string     `json:"last_test_status,omitempty"`
	LastTestError    string     `json:"last_test_error,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type BackupStorageTargetInput struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	Prefix          string `json:"prefix"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	ForcePathStyle  bool   `json:"force_path_style"`
	Default         bool   `json:"default"`
}

type BackupPolicy struct {
	ProjectRef      string     `json:"project_ref"`
	Enabled         bool       `json:"enabled"`
	Schedule        string     `json:"schedule"`
	Kind            string     `json:"kind"`
	StorageTargetID string     `json:"storage_target_id,omitempty"`
	LastRunAt       *time.Time `json:"last_run_at,omitempty"`
	NextRunAt       *time.Time `json:"next_run_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type BackupPolicyInput struct {
	Enabled         bool   `json:"enabled"`
	Schedule        string `json:"schedule"`
	Kind            string `json:"kind"`
	StorageTargetID string `json:"storage_target_id"`
}

type PITRPolicy struct {
	ProjectRef    string     `json:"project_ref"`
	Enabled       bool       `json:"enabled"`
	ArchiveBucket string     `json:"archive_bucket"`
	RetentionDays int        `json:"retention_days"`
	LastArchiveAt *time.Time `json:"last_archive_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type PITRPolicyInput struct {
	Enabled       bool   `json:"enabled"`
	ArchiveBucket string `json:"archive_bucket"`
	RetentionDays int    `json:"retention_days"`
}

type WALArchive struct {
	ID              string     `json:"id"`
	ProjectRef      string     `json:"project_ref"`
	Segment         string     `json:"segment"`
	SegmentSource   string     `json:"segment_source"`
	Location        string     `json:"location"`
	RemoteLocation  string     `json:"remote_location"`
	StorageTargetID string     `json:"storage_target_id"`
	SizeBytes       int64      `json:"size_bytes"`
	ChecksumSHA256  string     `json:"checksum_sha256"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	VerifiedAt      *time.Time `json:"verified_at,omitempty"`
}

type WALArchiveInput struct {
	ProjectRef      string
	Segment         string
	SegmentSource   string
	Location        string
	RemoteLocation  string
	StorageTargetID string
	SizeBytes       int64
	ChecksumSHA256  string
	Status          string
	VerifiedAt      *time.Time
}

type ProjectRecoverabilityStatus struct {
	ProjectRef                string      `json:"project_ref"`
	Status                    string      `json:"status"`
	BackupPolicyEnabled       bool        `json:"backup_policy_enabled"`
	OffHostBackupConfigured   bool        `json:"off_host_backup_configured"`
	OffHostBackupVerified     bool        `json:"off_host_backup_verified"`
	LatestBackup              *Backup     `json:"latest_backup,omitempty"`
	LatestVerifiedBackup      *Backup     `json:"latest_verified_backup,omitempty"`
	PITREnabled               bool        `json:"pitr_enabled"`
	LatestWALArchive          *WALArchive `json:"latest_wal_archive,omitempty"`
	WALArchiveOffHostVerified bool        `json:"wal_archive_off_host_verified"`
	RecoveryWindowStart       *time.Time  `json:"recovery_window_start,omitempty"`
	RecoveryWindowEnd         *time.Time  `json:"recovery_window_end,omitempty"`
	PhysicalBackupAvailable   bool        `json:"physical_backup_available"`
	RestoreToTimeConfigured   bool        `json:"restore_to_time_configured"`
	RestoreToTimeAvailable    bool        `json:"restore_to_time_available"`
	RestoreToTimeUnavailable  string      `json:"restore_to_time_unavailable,omitempty"`
	Warnings                  []string    `json:"warnings"`
	Recommendations           []string    `json:"recommendations"`
}

type ProjectLog struct {
	ID         string            `json:"id"`
	ProjectRef string            `json:"project_ref"`
	Level      string            `json:"level"`
	Message    string            `json:"message"`
	Metadata   map[string]string `json:"metadata"`
	CreatedAt  time.Time         `json:"created_at"`
}

type ProjectLogInput struct {
	ProjectRef string
	Level      string
	Message    string
	Metadata   map[string]string
}

type ProjectRoute struct {
	ID           string    `json:"id"`
	ProjectRef   string    `json:"project_ref"`
	Name         string    `json:"name"`
	FQDN         string    `json:"fqdn"`
	PathPrefix   string    `json:"path_prefix,omitempty"`
	StripPrefix  string    `json:"strip_prefix,omitempty"`
	UpstreamURL  string    `json:"upstream_url"`
	TLS          bool      `json:"tls"`
	SSLEnforced  bool      `json:"ssl_enforced"`
	IPAllowlist  []string  `json:"ip_allowlist,omitempty"`
	SSORequired  bool      `json:"sso_required,omitempty"`
	CacheControl string    `json:"cache_control,omitempty"`
	SmartCDN     bool      `json:"smart_cdn,omitempty"`
	CertMode     string    `json:"cert_mode,omitempty"`
	CertFile     string    `json:"cert_file,omitempty"`
	KeyFile      string    `json:"key_file,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type ProjectDomain struct {
	ProjectRef      string     `json:"project_ref"`
	FQDN            string     `json:"fqdn"`
	CertStatus      string     `json:"cert_status"`
	CertMode        string     `json:"cert_mode"`
	CertFingerprint string     `json:"cert_fingerprint,omitempty"`
	CertNotAfter    *time.Time `json:"cert_not_after,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type ProjectDomainInput struct {
	FQDN string `json:"fqdn"`
}

type ProjectDomainCertificateInput struct {
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
}

type ProjectDomainCertificateMetadata struct {
	Status      string     `json:"status"`
	Mode        string     `json:"mode"`
	Fingerprint string     `json:"fingerprint,omitempty"`
	NotAfter    *time.Time `json:"not_after,omitempty"`
}

type ProjectConfig struct {
	ProjectRef string            `json:"project_ref"`
	Area       string            `json:"area"`
	Config     map[string]string `json:"config"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

type ProjectConfigInput struct {
	Config map[string]string `json:"config"`
}

type ProjectAuthClient struct {
	ID                 string    `json:"id"`
	ProjectRef         string    `json:"project_ref"`
	Name               string    `json:"name"`
	ClientID           string    `json:"client_id"`
	ClientSecretHandle string    `json:"client_secret_handle,omitempty"`
	RedirectURIs       []string  `json:"redirect_uris"`
	GrantTypes         []string  `json:"grant_types"`
	Scopes             []string  `json:"scopes"`
	Confidential       bool      `json:"confidential"`
	Status             string    `json:"status"`
	Message            string    `json:"message,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ProjectAuthClientInput struct {
	Name               string   `json:"name"`
	ClientID           string   `json:"client_id"`
	ClientSecretHandle string   `json:"client_secret_handle"`
	RedirectURIs       []string `json:"redirect_uris"`
	GrantTypes         []string `json:"grant_types"`
	Scopes             []string `json:"scopes"`
	Confidential       bool     `json:"confidential"`
}

type ProjectAuthHook struct {
	ID             string            `json:"id"`
	ProjectRef     string            `json:"project_ref"`
	HookType       string            `json:"hook_type"`
	Enabled        bool              `json:"enabled"`
	TargetURI      string            `json:"target_uri,omitempty"`
	EdgeFunction   string            `json:"edge_function,omitempty"`
	SecretHandle   string            `json:"secret_handle,omitempty"`
	Headers        map[string]string `json:"headers"`
	RuntimeSecret  string            `json:"-"`
	RuntimeHeaders map[string]string `json:"-"`
	TimeoutMS      int               `json:"timeout_ms"`
	RetryAttempts  int               `json:"retry_attempts"`
	Status         string            `json:"status"`
	Message        string            `json:"message,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type ProjectAuthHookInput struct {
	HookType      string            `json:"hook_type"`
	Enabled       bool              `json:"enabled"`
	TargetURI     string            `json:"target_uri"`
	EdgeFunction  string            `json:"edge_function"`
	SecretHandle  string            `json:"secret_handle"`
	Headers       map[string]string `json:"headers"`
	TimeoutMS     int               `json:"timeout_ms"`
	RetryAttempts int               `json:"retry_attempts"`
}

type ProjectFunction struct {
	ID          string            `json:"id"`
	ProjectRef  string            `json:"project_ref"`
	Name        string            `json:"name"`
	Version     int               `json:"version"`
	Entrypoint  string            `json:"entrypoint"`
	VerifyJWT   bool              `json:"verify_jwt"`
	Status      string            `json:"status"`
	SourceHash  string            `json:"source_hash"`
	SourceBytes int               `json:"source_bytes"`
	Secrets     map[string]string `json:"secrets"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type ProjectFunctionInput struct {
	Name       string            `json:"name"`
	Entrypoint string            `json:"entrypoint"`
	VerifyJWT  bool              `json:"verify_jwt"`
	Source     string            `json:"source"`
	Secrets    map[string]string `json:"secrets"`
}

type ProjectFunctionRegion struct {
	ID            string    `json:"id"`
	ProjectRef    string    `json:"project_ref"`
	FunctionName  string    `json:"function_name"`
	HostID        string    `json:"host_id,omitempty"`
	Region        string    `json:"region"`
	RoutingPolicy string    `json:"routing_policy"`
	InvocationURL string    `json:"invocation_url"`
	Status        string    `json:"status"`
	Message       string    `json:"message,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ProjectFunctionRegionInput struct {
	FunctionName  string `json:"function_name"`
	HostID        string `json:"host_id"`
	Region        string `json:"region"`
	RoutingPolicy string `json:"routing_policy"`
}

type ProjectFunctionStorageMount struct {
	ID           string    `json:"id"`
	ProjectRef   string    `json:"project_ref"`
	FunctionName string    `json:"function_name"`
	BucketName   string    `json:"bucket_name"`
	MountPath    string    `json:"mount_path"`
	ReadOnly     bool      `json:"read_only"`
	Prefix       string    `json:"prefix,omitempty"`
	EnvAlias     string    `json:"env_alias,omitempty"`
	Status       string    `json:"status"`
	Message      string    `json:"message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ProjectFunctionStorageMountInput struct {
	FunctionName string `json:"function_name"`
	BucketName   string `json:"bucket_name"`
	MountPath    string `json:"mount_path"`
	ReadOnly     bool   `json:"read_only"`
	Prefix       string `json:"prefix"`
	EnvAlias     string `json:"env_alias"`
}

type ProjectReplicationPipeline struct {
	ID               string            `json:"id"`
	ProjectRef       string            `json:"project_ref"`
	Name             string            `json:"name"`
	Type             string            `json:"type"`
	SourceSchema     string            `json:"source_schema"`
	SourceTable      string            `json:"source_table"`
	Destination      string            `json:"destination"`
	DestinationURI   string            `json:"destination_uri"`
	CredentialHandle string            `json:"credential_handle,omitempty"`
	Config           map[string]string `json:"config"`
	Status           string            `json:"status"`
	Message          string            `json:"message,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type ProjectReplicationPipelineInput struct {
	Name             string            `json:"name"`
	Type             string            `json:"type"`
	SourceSchema     string            `json:"source_schema"`
	SourceTable      string            `json:"source_table"`
	Destination      string            `json:"destination"`
	DestinationURI   string            `json:"destination_uri"`
	CredentialHandle string            `json:"credential_handle"`
	Config           map[string]string `json:"config"`
}

type ProjectEmbeddingJob struct {
	ID                string    `json:"id"`
	ProjectRef        string    `json:"project_ref"`
	Name              string    `json:"name"`
	SourceSchema      string    `json:"source_schema"`
	SourceTable       string    `json:"source_table"`
	SourceColumn      string    `json:"source_column"`
	PrimaryKeyColumn  string    `json:"primary_key_column"`
	DestinationTable  string    `json:"destination_table"`
	DestinationColumn string    `json:"destination_column"`
	Provider          string    `json:"provider"`
	Model             string    `json:"model"`
	Dimension         int       `json:"dimension"`
	Schedule          string    `json:"schedule"`
	BatchSize         int       `json:"batch_size"`
	Status            string    `json:"status"`
	Message           string    `json:"message,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type ProjectEmbeddingJobInput struct {
	Name              string `json:"name"`
	SourceSchema      string `json:"source_schema"`
	SourceTable       string `json:"source_table"`
	SourceColumn      string `json:"source_column"`
	PrimaryKeyColumn  string `json:"primary_key_column"`
	DestinationTable  string `json:"destination_table"`
	DestinationColumn string `json:"destination_column"`
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	Dimension         int    `json:"dimension"`
	Schedule          string `json:"schedule"`
	BatchSize         int    `json:"batch_size"`
}

type ProjectDatabaseExtension struct {
	ID         string    `json:"id"`
	ProjectRef string    `json:"project_ref"`
	Name       string    `json:"name"`
	Schema     string    `json:"schema"`
	Version    string    `json:"version,omitempty"`
	Enabled    bool      `json:"enabled"`
	Status     string    `json:"status"`
	Message    string    `json:"message,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ProjectDatabaseExtensionInput struct {
	Schema  string `json:"schema"`
	Version string `json:"version"`
	Enabled *bool  `json:"enabled,omitempty"`
}

type ProjectDatabaseCronJob struct {
	ID                string            `json:"id"`
	ProjectRef        string            `json:"project_ref"`
	Name              string            `json:"name"`
	Schedule          string            `json:"schedule"`
	Command           string            `json:"command"`
	Database          string            `json:"database"`
	Username          string            `json:"username"`
	Active            bool              `json:"active"`
	TimeoutSeconds    int               `json:"timeout_seconds"`
	MaxRuntimeSeconds int               `json:"max_runtime_seconds"`
	Metadata          map[string]string `json:"metadata"`
	Status            string            `json:"status"`
	Message           string            `json:"message,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type ProjectDatabaseCronJobInput struct {
	Name              string            `json:"name"`
	Schedule          string            `json:"schedule"`
	Command           string            `json:"command"`
	Database          string            `json:"database"`
	Username          string            `json:"username"`
	Active            bool              `json:"active"`
	TimeoutSeconds    int               `json:"timeout_seconds"`
	MaxRuntimeSeconds int               `json:"max_runtime_seconds"`
	Metadata          map[string]string `json:"metadata"`
}

type ProjectDatabaseQueue struct {
	ID                       string            `json:"id"`
	ProjectRef               string            `json:"project_ref"`
	Name                     string            `json:"name"`
	Schema                   string            `json:"schema"`
	RetentionMinutes         int               `json:"retention_minutes"`
	VisibilityTimeoutSeconds int               `json:"visibility_timeout_seconds"`
	MaxRetries               int               `json:"max_retries"`
	DeadLetterQueue          string            `json:"dead_letter_queue,omitempty"`
	Active                   bool              `json:"active"`
	Metadata                 map[string]string `json:"metadata"`
	Status                   string            `json:"status"`
	Message                  string            `json:"message,omitempty"`
	CreatedAt                time.Time         `json:"created_at"`
	UpdatedAt                time.Time         `json:"updated_at"`
}

type ProjectDatabaseQueueInput struct {
	Name                     string            `json:"name"`
	Schema                   string            `json:"schema"`
	RetentionMinutes         int               `json:"retention_minutes"`
	VisibilityTimeoutSeconds int               `json:"visibility_timeout_seconds"`
	MaxRetries               int               `json:"max_retries"`
	DeadLetterQueue          string            `json:"dead_letter_queue"`
	Active                   bool              `json:"active"`
	Metadata                 map[string]string `json:"metadata"`
}

type ProjectDatabaseWebhook struct {
	ID             string            `json:"id"`
	ProjectRef     string            `json:"project_ref"`
	Name           string            `json:"name"`
	Schema         string            `json:"schema"`
	Table          string            `json:"table"`
	Events         []string          `json:"events"`
	Endpoint       string            `json:"endpoint"`
	HTTPMethod     string            `json:"http_method"`
	Headers        map[string]string `json:"headers"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	RetryCount     int               `json:"retry_count"`
	Active         bool              `json:"active"`
	Metadata       map[string]string `json:"metadata"`
	Status         string            `json:"status"`
	Message        string            `json:"message,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type ProjectDatabaseWebhookInput struct {
	Name           string            `json:"name"`
	Schema         string            `json:"schema"`
	Table          string            `json:"table"`
	Events         []string          `json:"events"`
	Endpoint       string            `json:"endpoint"`
	HTTPMethod     string            `json:"http_method"`
	Headers        map[string]string `json:"headers"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	RetryCount     int               `json:"retry_count"`
	Active         bool              `json:"active"`
	Metadata       map[string]string `json:"metadata"`
}

type ProjectDatabaseSchema struct {
	ID         string            `json:"id"`
	ProjectRef string            `json:"project_ref"`
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	Schema     string            `json:"schema"`
	SQL        string            `json:"sql"`
	Checksum   string            `json:"checksum"`
	ApplyOrder int               `json:"apply_order"`
	Active     bool              `json:"active"`
	Metadata   map[string]string `json:"metadata"`
	Status     string            `json:"status"`
	Message    string            `json:"message,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

type ProjectDatabaseSchemaInput struct {
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	Schema     string            `json:"schema"`
	SQL        string            `json:"sql"`
	ApplyOrder int               `json:"apply_order"`
	Active     bool              `json:"active"`
	Metadata   map[string]string `json:"metadata"`
}

type ProjectDatabaseRole struct {
	ID                   string            `json:"id"`
	ProjectRef           string            `json:"project_ref"`
	Name                 string            `json:"name"`
	Login                bool              `json:"login"`
	Inherit              bool              `json:"inherit"`
	BypassRLS            bool              `json:"bypass_rls"`
	ConnectionLimit      int               `json:"connection_limit"`
	PasswordSecretHandle string            `json:"password_secret_handle,omitempty"`
	MemberOf             []string          `json:"member_of"`
	SchemaGrants         map[string]string `json:"schema_grants"`
	Metadata             map[string]string `json:"metadata"`
	Status               string            `json:"status"`
	Message              string            `json:"message,omitempty"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
}

type ProjectDatabaseRoleInput struct {
	Name                 string            `json:"name"`
	Login                bool              `json:"login"`
	Inherit              *bool             `json:"inherit,omitempty"`
	BypassRLS            bool              `json:"bypass_rls"`
	ConnectionLimit      int               `json:"connection_limit"`
	PasswordSecretHandle string            `json:"password_secret_handle"`
	MemberOf             []string          `json:"member_of"`
	SchemaGrants         map[string]string `json:"schema_grants"`
	Metadata             map[string]string `json:"metadata"`
}

type ProjectVectorBucket struct {
	ID             string            `json:"id"`
	ProjectRef     string            `json:"project_ref"`
	Name           string            `json:"name"`
	Dimension      int               `json:"dimension"`
	Distance       string            `json:"distance"`
	IndexMethod    string            `json:"index_method"`
	StorageBackend string            `json:"storage_backend"`
	StorageURI     string            `json:"storage_uri,omitempty"`
	Metadata       map[string]string `json:"metadata"`
	Status         string            `json:"status"`
	Message        string            `json:"message,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type ProjectVectorBucketInput struct {
	Name           string            `json:"name"`
	Dimension      int               `json:"dimension"`
	Distance       string            `json:"distance"`
	IndexMethod    string            `json:"index_method"`
	StorageBackend string            `json:"storage_backend"`
	StorageURI     string            `json:"storage_uri"`
	Metadata       map[string]string `json:"metadata"`
}

type ProjectAnalyticsBucket struct {
	ID                 string            `json:"id"`
	ProjectRef         string            `json:"project_ref"`
	Name               string            `json:"name"`
	StorageURI         string            `json:"storage_uri"`
	CatalogURI         string            `json:"catalog_uri,omitempty"`
	Warehouse          string            `json:"warehouse"`
	CredentialHandle   string            `json:"credential_handle,omitempty"`
	FormatVersion      int               `json:"format_version"`
	Partitioning       string            `json:"partitioning,omitempty"`
	RetentionDays      int               `json:"retention_days"`
	CompactionSchedule string            `json:"compaction_schedule"`
	Metadata           map[string]string `json:"metadata"`
	Status             string            `json:"status"`
	Message            string            `json:"message,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

type ProjectAnalyticsBucketInput struct {
	Name               string            `json:"name"`
	StorageURI         string            `json:"storage_uri"`
	CatalogURI         string            `json:"catalog_uri"`
	Warehouse          string            `json:"warehouse"`
	CredentialHandle   string            `json:"credential_handle"`
	FormatVersion      int               `json:"format_version"`
	Partitioning       string            `json:"partitioning"`
	RetentionDays      int               `json:"retention_days"`
	CompactionSchedule string            `json:"compaction_schedule"`
	Metadata           map[string]string `json:"metadata"`
}

type ProjectStorageBucket struct {
	ID                string            `json:"id"`
	ProjectRef        string            `json:"project_ref"`
	Name              string            `json:"name"`
	Public            bool              `json:"public"`
	FileSizeLimit     int64             `json:"file_size_limit"`
	AllowedMimeTypes  []string          `json:"allowed_mime_types"`
	CacheControl      string            `json:"cache_control"`
	AvifAutodetection bool              `json:"avif_autodetection"`
	Metadata          map[string]string `json:"metadata"`
	Status            string            `json:"status"`
	Message           string            `json:"message,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type ProjectStorageBucketInput struct {
	Name              string            `json:"name"`
	Public            bool              `json:"public"`
	FileSizeLimit     int64             `json:"file_size_limit"`
	AllowedMimeTypes  []string          `json:"allowed_mime_types"`
	CacheControl      string            `json:"cache_control"`
	AvifAutodetection bool              `json:"avif_autodetection"`
	Metadata          map[string]string `json:"metadata"`
}

type ProjectCDNPolicy struct {
	ProjectRef                  string    `json:"project_ref"`
	Enabled                     bool      `json:"enabled"`
	BrowserTTLSeconds           int       `json:"browser_ttl_seconds"`
	EdgeTTLSeconds              int       `json:"edge_ttl_seconds"`
	StaleWhileRevalidateSeconds int       `json:"stale_while_revalidate_seconds"`
	IncludedPaths               []string  `json:"included_paths"`
	ExcludedPaths               []string  `json:"excluded_paths"`
	SmartRevalidation           bool      `json:"smart_revalidation"`
	CacheControl                string    `json:"cache_control"`
	UpdatedAt                   time.Time `json:"updated_at"`
}

type ProjectCDNPolicyInput struct {
	Enabled                     bool     `json:"enabled"`
	BrowserTTLSeconds           int      `json:"browser_ttl_seconds"`
	EdgeTTLSeconds              int      `json:"edge_ttl_seconds"`
	StaleWhileRevalidateSeconds int      `json:"stale_while_revalidate_seconds"`
	IncludedPaths               []string `json:"included_paths"`
	ExcludedPaths               []string `json:"excluded_paths"`
	SmartRevalidation           bool     `json:"smart_revalidation"`
	CacheControl                string   `json:"cache_control"`
}

type CDNInvalidation struct {
	ID          string     `json:"id"`
	ProjectRef  string     `json:"project_ref"`
	Paths       []string   `json:"paths"`
	Source      string     `json:"source"`
	EventID     string     `json:"event_id,omitempty"`
	Status      string     `json:"status"`
	Message     string     `json:"message,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type CDNInvalidationInput struct {
	Paths []string `json:"paths"`
}

type CDNObjectEventInput struct {
	EventID    string `json:"event_id"`
	Bucket     string `json:"bucket"`
	ObjectPath string `json:"object_path"`
	EventType  string `json:"event_type"`
}

type ProjectNetworkConnection struct {
	ID         string            `json:"id"`
	ProjectRef string            `json:"project_ref"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Provider   string            `json:"provider"`
	Region     string            `json:"region,omitempty"`
	CIDRs      []string          `json:"cidrs"`
	EndpointID string            `json:"endpoint_id,omitempty"`
	Config     map[string]string `json:"config"`
	Status     string            `json:"status"`
	Message    string            `json:"message,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

type ProjectNetworkConnectionInput struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Provider   string            `json:"provider"`
	Region     string            `json:"region"`
	CIDRs      []string          `json:"cidrs"`
	EndpointID string            `json:"endpoint_id"`
	Config     map[string]string `json:"config"`
}

type ProjectBranch struct {
	ID               string     `json:"id"`
	SourceProjectRef string     `json:"source_project_ref"`
	ProjectRef       string     `json:"project_ref"`
	Name             string     `json:"name"`
	WithData         bool       `json:"with_data"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
}

type ProjectBranchInput struct {
	Ref      string `json:"ref"`
	Name     string `json:"name"`
	TTLHours int    `json:"ttl_hours"`
	WithData bool   `json:"with_data"`
}

type ProjectReplica struct {
	ID                    string       `json:"id"`
	ProjectRef            string       `json:"project_ref"`
	Name                  string       `json:"name"`
	HostID                string       `json:"host_id,omitempty"`
	Region                string       `json:"region,omitempty"`
	Tier                  ResourceTier `json:"tier"`
	Status                string       `json:"status"`
	Role                  string       `json:"role"`
	Message               string       `json:"message,omitempty"`
	ReadURI               string       `json:"read_uri"`
	PublicReadURI         string       `json:"public_read_uri,omitempty"`
	InternalReadURI       string       `json:"internal_read_uri,omitempty"`
	ReadWeight            int          `json:"read_weight"`
	FailoverPriority      int          `json:"failover_priority"`
	ReplicationLagBytes   int64        `json:"replication_lag_bytes"`
	ReplicationLagSeconds int          `json:"replication_lag_seconds"`
	PromotedAt            *time.Time   `json:"promoted_at,omitempty"`
	CreatedAt             time.Time    `json:"created_at"`
	UpdatedAt             time.Time    `json:"updated_at"`
}

type ProjectReplicaInput struct {
	Name             string       `json:"name"`
	HostID           string       `json:"host_id"`
	Region           string       `json:"region"`
	Tier             ResourceTier `json:"tier"`
	ReadWeight       int          `json:"read_weight"`
	FailoverPriority int          `json:"failover_priority"`
}

type ProjectReplicaRouteTarget struct {
	ReplicaID             string `json:"replica_id"`
	Name                  string `json:"name"`
	URI                   string `json:"uri"`
	Region                string `json:"region,omitempty"`
	Weight                int    `json:"weight"`
	FailoverPriority      int    `json:"failover_priority"`
	ReplicationLagBytes   int64  `json:"replication_lag_bytes"`
	ReplicationLagSeconds int    `json:"replication_lag_seconds"`
	Role                  string `json:"role"`
	Status                string `json:"status"`
}

type ProjectReplicaRouting struct {
	ProjectRef         string                      `json:"project_ref"`
	PrimaryURI         string                      `json:"primary_uri"`
	ReadStrategy       string                      `json:"read_strategy"`
	AutoFailover       bool                        `json:"auto_failover"`
	PrimaryReplicaID   string                      `json:"primary_replica_id,omitempty"`
	FailoverCandidate  *ProjectReplicaRouteTarget  `json:"failover_candidate,omitempty"`
	HealthyReadTargets []ProjectReplicaRouteTarget `json:"healthy_read_targets"`
	AllTargets         []ProjectReplicaRouteTarget `json:"all_targets"`
}

type LogDrain struct {
	ID         string            `json:"id"`
	ProjectRef string            `json:"project_ref"`
	Target     string            `json:"target"`
	Config     map[string]string `json:"config"`
	CreatedAt  time.Time         `json:"created_at"`
}

type LogDrainInput struct {
	Target string            `json:"target"`
	Config map[string]string `json:"config"`
}

type ProjectSecret struct {
	ID         string     `json:"id"`
	ProjectRef string     `json:"project_ref"`
	Kind       string     `json:"kind"`
	Value      string     `json:"-"`
	Masked     string     `json:"masked"`
	CreatedAt  time.Time  `json:"created_at"`
	RotatedAt  *time.Time `json:"rotated_at,omitempty"`
}

type ProjectSecretReveal struct {
	Kind      string     `json:"kind"`
	Value     string     `json:"value"`
	CreatedAt time.Time  `json:"created_at"`
	RotatedAt *time.Time `json:"rotated_at,omitempty"`
}

type ProjectSecretInput struct {
	Value string `json:"value"`
}

type JWTSigningKeyMaterial struct {
	KID        string `json:"kid"`
	Alg        string `json:"alg"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
	Status     string `json:"status"`
}

type JWTSigningKeySummary struct {
	Kind      string     `json:"kind"`
	KID       string     `json:"kid"`
	Alg       string     `json:"alg"`
	Status    string     `json:"status"`
	PublicKey string     `json:"public_key"`
	Handle    string     `json:"handle"`
	CreatedAt time.Time  `json:"created_at"`
	RotatedAt *time.Time `json:"rotated_at,omitempty"`
}

type RotateProjectSecretRequest struct {
	Kind string `json:"kind"`
}

type MemoryStore struct {
	mu                    sync.RWMutex
	platformDefaults      PlatformDefaults
	platformSSO           PlatformSSOConfig
	users                 map[string]User
	orgs                  map[string]Org
	orgQuotas             map[string]OrgQuota
	usageSnapshots        map[string][]UsageSnapshot
	billingInvoices       map[string][]BillingInvoice
	memberships           map[string]map[string]Membership
	teams                 map[string]map[string]Team
	teamMembers           map[string]map[string]TeamMember
	projectAccess         map[string][]ProjectAccessGrant
	hosts                 map[string]Host
	projects              map[string]Project
	routes                map[string][]ProjectRoute
	domains               map[string][]ProjectDomain
	configs               map[string]map[string]ProjectConfig
	authClients           map[string][]ProjectAuthClient
	authHooks             map[string][]ProjectAuthHook
	functions             map[string][]ProjectFunction
	functionRegions       map[string][]ProjectFunctionRegion
	functionStorageMounts map[string][]ProjectFunctionStorageMount
	replicationPipelines  map[string][]ProjectReplicationPipeline
	embeddingJobs         map[string][]ProjectEmbeddingJob
	databaseExtensions    map[string][]ProjectDatabaseExtension
	databaseCronJobs      map[string][]ProjectDatabaseCronJob
	databaseQueues        map[string][]ProjectDatabaseQueue
	databaseWebhooks      map[string][]ProjectDatabaseWebhook
	databaseSchemas       map[string][]ProjectDatabaseSchema
	databaseRoles         map[string][]ProjectDatabaseRole
	storageBuckets        map[string][]ProjectStorageBucket
	vectorBuckets         map[string][]ProjectVectorBucket
	analyticsBuckets      map[string][]ProjectAnalyticsBucket
	cdnPolicies           map[string]ProjectCDNPolicy
	cdnInvalidations      map[string][]CDNInvalidation
	networkConnections    map[string][]ProjectNetworkConnection
	branches              map[string][]ProjectBranch
	replicas              map[string][]ProjectReplica
	logDrains             map[string][]LogDrain
	secrets               map[string]map[string]ProjectSecret
	backupStorageTargets  map[string]BackupStorageTarget
	policies              map[string]BackupPolicy
	pitrPolicies          map[string]PITRPolicy
	backups               []Backup
	platformBackups       []PlatformBackup
	walArchives           []WALArchive
	projectLogs           []ProjectLog
	telemetry             map[string]TelemetrySample
	nodeTelemetry         map[string]NodeTelemetrySample
	auditEvents           []AuditEvent
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		platformDefaults:      defaultPlatformDefaults(),
		platformSSO:           defaultPlatformSSOConfig(),
		users:                 map[string]User{},
		orgs:                  map[string]Org{},
		orgQuotas:             map[string]OrgQuota{},
		usageSnapshots:        map[string][]UsageSnapshot{},
		billingInvoices:       map[string][]BillingInvoice{},
		memberships:           map[string]map[string]Membership{},
		teams:                 map[string]map[string]Team{},
		teamMembers:           map[string]map[string]TeamMember{},
		projectAccess:         map[string][]ProjectAccessGrant{},
		hosts:                 map[string]Host{},
		projects:              map[string]Project{},
		routes:                map[string][]ProjectRoute{},
		domains:               map[string][]ProjectDomain{},
		configs:               map[string]map[string]ProjectConfig{},
		authClients:           map[string][]ProjectAuthClient{},
		authHooks:             map[string][]ProjectAuthHook{},
		functions:             map[string][]ProjectFunction{},
		functionRegions:       map[string][]ProjectFunctionRegion{},
		functionStorageMounts: map[string][]ProjectFunctionStorageMount{},
		replicationPipelines:  map[string][]ProjectReplicationPipeline{},
		embeddingJobs:         map[string][]ProjectEmbeddingJob{},
		databaseExtensions:    map[string][]ProjectDatabaseExtension{},
		databaseCronJobs:      map[string][]ProjectDatabaseCronJob{},
		databaseQueues:        map[string][]ProjectDatabaseQueue{},
		databaseWebhooks:      map[string][]ProjectDatabaseWebhook{},
		databaseSchemas:       map[string][]ProjectDatabaseSchema{},
		databaseRoles:         map[string][]ProjectDatabaseRole{},
		storageBuckets:        map[string][]ProjectStorageBucket{},
		vectorBuckets:         map[string][]ProjectVectorBucket{},
		analyticsBuckets:      map[string][]ProjectAnalyticsBucket{},
		cdnPolicies:           map[string]ProjectCDNPolicy{},
		cdnInvalidations:      map[string][]CDNInvalidation{},
		networkConnections:    map[string][]ProjectNetworkConnection{},
		branches:              map[string][]ProjectBranch{},
		replicas:              map[string][]ProjectReplica{},
		logDrains:             map[string][]LogDrain{},
		secrets:               map[string]map[string]ProjectSecret{},
		backupStorageTargets:  map[string]BackupStorageTarget{},
		policies:              map[string]BackupPolicy{},
		pitrPolicies:          map[string]PITRPolicy{},
		platformBackups:       []PlatformBackup{},
		telemetry:             map[string]TelemetrySample{},
		nodeTelemetry:         map[string]NodeTelemetrySample{},
	}
}

func (s *MemoryStore) GetPlatformDefaults(ctx context.Context) (PlatformDefaults, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizedPlatformDefaults(s.platformDefaults), nil
}

func (s *MemoryStore) UpdatePlatformDefaults(ctx context.Context, input PlatformDefaultsInput) (PlatformDefaults, error) {
	defaults, err := normalizePlatformDefaults(input)
	if err != nil {
		return PlatformDefaults{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.platformDefaults = defaults
	return defaults, nil
}

func (s *MemoryStore) GetPlatformSSOConfig(ctx context.Context) (PlatformSSOConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizedPlatformSSOConfig(s.platformSSO), nil
}

func (s *MemoryStore) UpdatePlatformSSOConfig(ctx context.Context, input PlatformSSOConfigInput) (PlatformSSOConfig, error) {
	config, err := normalizePlatformSSOInput(input)
	if err != nil {
		return PlatformSSOConfig{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(input.SCIMToken) == "" {
		config.SCIMTokenHash = s.platformSSO.SCIMTokenHash
		config.SCIMTokenConfigured = config.SCIMTokenHash != ""
	}
	s.platformSSO = config
	return normalizedPlatformSSOConfig(config), nil
}

func (s *MemoryStore) CreateUser(ctx context.Context, req CreateUserRequest) (User, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		return User{}, fmt.Errorf("email is required")
	}
	if err := validateUserPassword(req.Password, true); err != nil {
		return User{}, err
	}
	role := req.Role
	if role == "" {
		role = "admin"
	}
	user := User{
		ID:           newID(),
		Email:        email,
		PasswordHash: hashPassword(req.Password),
		Role:         role,
		CreatedAt:    time.Now().UTC(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[email]; ok {
		return User{}, fmt.Errorf("%w: user %s already exists", ErrConflict, email)
	}
	s.users[email] = user
	return user, nil
}

func (s *MemoryStore) UpdateUser(ctx context.Context, id string, req UpdateUserRequest) (User, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return User{}, fmt.Errorf("user id is required")
	}
	nextEmail := strings.ToLower(strings.TrimSpace(req.Email))
	if nextEmail == "" {
		return User{}, fmt.Errorf("email is required")
	}
	nextRole := strings.TrimSpace(req.Role)
	if nextRole == "" {
		nextRole = "member"
	}
	if err := validateUserPassword(req.Password, false); err != nil {
		return User{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	var currentEmail string
	var user User
	for email, candidate := range s.users {
		if candidate.ID == id {
			currentEmail = email
			user = candidate
			break
		}
	}
	if currentEmail == "" {
		return User{}, fmt.Errorf("%w: user %s", ErrNotFound, id)
	}
	if currentEmail != nextEmail {
		if _, ok := s.users[nextEmail]; ok {
			return User{}, fmt.Errorf("%w: user %s already exists", ErrConflict, nextEmail)
		}
		delete(s.users, currentEmail)
		for orgID, members := range s.memberships {
			if member, ok := members[currentEmail]; ok {
				delete(members, currentEmail)
				member.Email = nextEmail
				members[nextEmail] = member
				s.memberships[orgID] = members
			}
		}
		for teamID, members := range s.teamMembers {
			if member, ok := members[currentEmail]; ok {
				delete(members, currentEmail)
				member.Email = nextEmail
				members[nextEmail] = member
				s.teamMembers[teamID] = members
			}
		}
		for ref, grants := range s.projectAccess {
			for index, grant := range grants {
				if grant.SubjectType == "user" && grant.SubjectID == user.ID {
					grants[index].SubjectName = nextEmail
				}
			}
			s.projectAccess[ref] = grants
		}
	}
	user.Email = nextEmail
	user.Role = nextRole
	if req.Password != "" {
		user.PasswordHash = hashPassword(req.Password)
	}
	s.users[nextEmail] = user
	return user, nil
}

func (s *MemoryStore) DeleteUser(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("user id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	var email string
	for candidateEmail, user := range s.users {
		if user.ID == id {
			email = candidateEmail
			break
		}
	}
	if email == "" {
		return fmt.Errorf("%w: user %s", ErrNotFound, id)
	}
	delete(s.users, email)
	for orgID, members := range s.memberships {
		delete(members, email)
		s.memberships[orgID] = members
	}
	for teamID, members := range s.teamMembers {
		delete(members, email)
		s.teamMembers[teamID] = members
	}
	for ref, grants := range s.projectAccess {
		filtered := grants[:0]
		for _, grant := range grants {
			if grant.SubjectType == "user" && grant.SubjectID == id {
				continue
			}
			filtered = append(filtered, grant)
		}
		s.projectAccess[ref] = append([]ProjectAccessGrant(nil), filtered...)
	}
	return nil
}

func (s *MemoryStore) ListUsers(ctx context.Context) ([]User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]User, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].Email < users[j].Email
	})
	return users, nil
}

func (s *MemoryStore) GetUserByID(ctx context.Context, id string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, user := range s.users {
		if user.ID == id {
			return user, nil
		}
	}
	return User{}, fmt.Errorf("%w: user %s", ErrNotFound, id)
}

func (s *MemoryStore) AuthenticateUser(ctx context.Context, email string, password string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	s.mu.RLock()
	user, ok := s.users[email]
	s.mu.RUnlock()
	verified, needsRehash := verifyPasswordWithRehash(password, user.PasswordHash)
	if !ok || !verified {
		return User{}, fmt.Errorf("%w: invalid credentials", ErrNotFound)
	}
	if needsRehash {
		s.mu.Lock()
		if current, ok := s.users[email]; ok && current.PasswordHash == user.PasswordHash {
			user.PasswordHash = hashPassword(password)
			current.PasswordHash = user.PasswordHash
			s.users[email] = current
		}
		s.mu.Unlock()
	}
	return user, nil
}

func (s *MemoryStore) RecordUserLogin(ctx context.Context, userID string) (time.Time, error) {
	at := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.userByIDLocked(userID)
	if !ok {
		return time.Time{}, fmt.Errorf("%w: user %s", ErrNotFound, userID)
	}
	user.LastLoginAt = &at
	s.users[user.Email] = user
	return at, nil
}

func (s *MemoryStore) VerifyUserMFA(ctx context.Context, userID string, code string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.userByIDLocked(userID)
	if !ok {
		return User{}, fmt.Errorf("%w: user %s", ErrNotFound, userID)
	}
	if !user.MFAEnabled || user.MFASecret == "" {
		return User{}, fmt.Errorf("mfa is not enabled")
	}
	counter, ok := VerifyTOTPCodeCounter(user.MFASecret, code, time.Now().UTC())
	if !ok {
		return User{}, fmt.Errorf("invalid mfa code")
	}
	if int64(counter) <= user.MFALastCounter {
		return User{}, fmt.Errorf("mfa code has already been used")
	}
	user.MFALastCounter = int64(counter)
	user.MFAUpdatedAt = time.Now().UTC()
	s.users[user.Email] = user
	return user, nil
}

func (s *MemoryStore) GetUserMFAStatus(ctx context.Context, userID string) (MFAStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.userByIDLocked(userID)
	if !ok {
		return MFAStatus{}, fmt.Errorf("%w: user %s", ErrNotFound, userID)
	}
	return mfaStatusForUser(user), nil
}

func (s *MemoryStore) BeginUserMFAEnrollment(ctx context.Context, userID string) (MFAEnrollment, error) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		return MFAEnrollment{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.userByIDLocked(userID)
	if !ok {
		return MFAEnrollment{}, fmt.Errorf("%w: user %s", ErrNotFound, userID)
	}
	user.MFAPendingSecret = secret
	user.MFAUpdatedAt = time.Now().UTC()
	s.users[user.Email] = user

	return MFAEnrollment{
		MFAStatus:  mfaStatusForUser(user),
		Secret:     secret,
		OTPAuthURL: TOTPAuthURL("supadupa", user.Email, secret),
	}, nil
}

func (s *MemoryStore) ConfirmUserMFA(ctx context.Context, userID string, code string) (MFAStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.userByIDLocked(userID)
	if !ok {
		return MFAStatus{}, fmt.Errorf("%w: user %s", ErrNotFound, userID)
	}
	if user.MFAPendingSecret == "" {
		return MFAStatus{}, fmt.Errorf("mfa enrollment is not pending")
	}
	counter, ok := VerifyTOTPCodeCounter(user.MFAPendingSecret, code, time.Now().UTC())
	if !ok {
		return MFAStatus{}, fmt.Errorf("invalid mfa code")
	}
	now := time.Now().UTC()
	user.MFASecret = user.MFAPendingSecret
	user.MFAPendingSecret = ""
	user.MFAEnabled = true
	user.MFAConfirmedAt = now
	user.MFAUpdatedAt = now
	user.MFALastCounter = int64(counter)
	s.users[user.Email] = user
	return mfaStatusForUser(user), nil
}

func (s *MemoryStore) DisableUserMFA(ctx context.Context, userID string, code string) (MFAStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.userByIDLocked(userID)
	if !ok {
		return MFAStatus{}, fmt.Errorf("%w: user %s", ErrNotFound, userID)
	}
	if !user.MFAEnabled {
		user.MFAPendingSecret = ""
		user.MFAUpdatedAt = time.Now().UTC()
		s.users[user.Email] = user
		return mfaStatusForUser(user), nil
	}
	counter, ok := VerifyTOTPCodeCounter(user.MFASecret, code, time.Now().UTC())
	if !ok {
		return MFAStatus{}, fmt.Errorf("invalid mfa code")
	}
	if int64(counter) <= user.MFALastCounter {
		return MFAStatus{}, fmt.Errorf("mfa code has already been used")
	}
	user.MFAEnabled = false
	user.MFASecret = ""
	user.MFAPendingSecret = ""
	user.MFAConfirmedAt = time.Time{}
	user.MFAUpdatedAt = time.Now().UTC()
	user.MFALastCounter = 0
	s.users[user.Email] = user
	return mfaStatusForUser(user), nil
}

func (s *MemoryStore) HasUsers(ctx context.Context) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users) > 0
}

func (s *MemoryStore) CreateOrg(ctx context.Context, name string) (Org, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Org{}, fmt.Errorf("name is required")
	}

	now := time.Now().UTC()
	org := Org{ID: newID(), Name: name, FeatureFlagOverrides: map[string]bool{}, CreatedAt: now}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.orgs[org.ID] = org
	return orgWithFeatureFlags(org, normalizedPlatformDefaults(s.platformDefaults)), nil
}

func (s *MemoryStore) UpdateOrg(ctx context.Context, id string, name string) (Org, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Org{}, fmt.Errorf("name is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	org, ok := s.orgs[id]
	if !ok {
		return Org{}, fmt.Errorf("%w: org %s", ErrNotFound, id)
	}
	org.Name = name
	s.orgs[id] = org
	return orgWithFeatureFlags(org, normalizedPlatformDefaults(s.platformDefaults)), nil
}

func (s *MemoryStore) DeleteOrg(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[id]; !ok {
		return fmt.Errorf("%w: org %s", ErrNotFound, id)
	}
	for _, project := range s.projects {
		if project.OrgID == id {
			return fmt.Errorf("%w: org %s still has projects", ErrConflict, id)
		}
	}
	for _, team := range s.teams[id] {
		delete(s.teamMembers, team.ID)
	}
	delete(s.orgs, id)
	delete(s.orgQuotas, id)
	delete(s.usageSnapshots, id)
	delete(s.billingInvoices, id)
	delete(s.memberships, id)
	delete(s.teams, id)
	return nil
}

func (s *MemoryStore) GetOrg(ctx context.Context, id string) (Org, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	org, ok := s.orgs[id]
	if !ok {
		return Org{}, fmt.Errorf("%w: org %s", ErrNotFound, id)
	}
	return orgWithFeatureFlags(org, normalizedPlatformDefaults(s.platformDefaults)), nil
}

func (s *MemoryStore) ListOrgs(ctx context.Context) ([]Org, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	orgs := make([]Org, 0, len(s.orgs))
	defaults := normalizedPlatformDefaults(s.platformDefaults)
	for _, org := range s.orgs {
		orgs = append(orgs, orgWithFeatureFlags(org, defaults))
	}
	sort.Slice(orgs, func(i, j int) bool {
		return orgs[i].CreatedAt.Before(orgs[j].CreatedAt)
	})
	return orgs, nil
}

func (s *MemoryStore) GetOrgFeatureFlags(ctx context.Context, orgID string) (OrgFeatureFlags, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	org, ok := s.orgs[orgID]
	if !ok {
		return OrgFeatureFlags{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	return orgFeatureFlags(orgID, org.FeatureFlagOverrides, normalizedPlatformDefaults(s.platformDefaults)), nil
}

func (s *MemoryStore) UpdateOrgFeatureFlags(ctx context.Context, orgID string, input OrgFeatureFlagsInput) (OrgFeatureFlags, error) {
	overrides, err := normalizeOrgFeatureOverrides(input.Overrides)
	if err != nil {
		return OrgFeatureFlags{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	org, ok := s.orgs[orgID]
	if !ok {
		return OrgFeatureFlags{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	org.FeatureFlagOverrides = overrides
	s.orgs[orgID] = org
	return orgFeatureFlags(orgID, overrides, normalizedPlatformDefaults(s.platformDefaults)), nil
}

func (s *MemoryStore) GetOrgQuota(ctx context.Context, orgID string) (OrgQuota, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return OrgQuota{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	return s.orgQuotaLocked(orgID), nil
}

func (s *MemoryStore) UpdateOrgQuota(ctx context.Context, orgID string, input OrgQuotaInput) (OrgQuota, error) {
	if input.MaxProjects < 0 || input.MaxCPU < 0 || input.MaxRAMMB < 0 || input.MaxDiskGB < 0 {
		return OrgQuota{}, fmt.Errorf("quota limits cannot be negative")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return OrgQuota{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	quota := OrgQuota{
		OrgID:       orgID,
		MaxProjects: input.MaxProjects,
		MaxCPU:      input.MaxCPU,
		MaxRAMMB:    input.MaxRAMMB,
		MaxDiskGB:   input.MaxDiskGB,
		UpdatedAt:   time.Now().UTC(),
	}
	s.orgQuotas[orgID] = quota
	return s.orgQuotaLocked(orgID), nil
}

func (s *MemoryStore) GetOrgUsage(ctx context.Context, orgID string) (OrgUsage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return OrgUsage{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	return s.orgMeteringLocked(orgID, time.Now().UTC()), nil
}

func (s *MemoryStore) ListOrgUsageSnapshots(ctx context.Context, orgID string, limit int) ([]UsageSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return nil, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	snapshots := cloneUsageSnapshots(s.usageSnapshots[orgID])
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].SampledAt.After(snapshots[j].SampledAt)
	})
	if len(snapshots) > limit {
		snapshots = snapshots[:limit]
	}
	return snapshots, nil
}

func (s *MemoryStore) CreateOrgUsageSnapshot(ctx context.Context, orgID string) (UsageSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return UsageSnapshot{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	sampledAt := time.Now().UTC()
	usage := s.orgMeteringLocked(orgID, sampledAt)
	snapshot := UsageSnapshot{
		ID:        newID(),
		OrgID:     orgID,
		Metrics:   cloneOrgUsage(usage),
		SampledAt: sampledAt,
	}
	s.usageSnapshots[orgID] = append(s.usageSnapshots[orgID], snapshot)
	return cloneUsageSnapshot(snapshot), nil
}

func (s *MemoryStore) ListBillingInvoices(ctx context.Context, orgID string, limit int) ([]BillingInvoice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return nil, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	invoices := cloneBillingInvoices(s.billingInvoices[orgID])
	sort.Slice(invoices, func(i, j int) bool {
		return invoices[i].CreatedAt.After(invoices[j].CreatedAt)
	})
	if len(invoices) > limit {
		invoices = invoices[:limit]
	}
	return invoices, nil
}

func (s *MemoryStore) GetBillingInvoice(ctx context.Context, orgID string, invoiceID string) (BillingInvoice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return BillingInvoice{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	for _, invoice := range s.billingInvoices[orgID] {
		if invoice.ID == invoiceID {
			return cloneBillingInvoice(invoice), nil
		}
	}
	return BillingInvoice{}, fmt.Errorf("%w: billing invoice %s", ErrNotFound, invoiceID)
}

func (s *MemoryStore) CreateBillingInvoice(ctx context.Context, orgID string, input BillingInvoiceInput) (BillingInvoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return BillingInvoice{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "draft"
	}
	if status != "draft" && status != "open" && status != "void" && status != "paid" {
		return BillingInvoice{}, fmt.Errorf("billing invoice status must be draft, open, void, or paid")
	}
	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if currency == "" {
		currency = "USD"
	}
	if len(currency) != 3 {
		return BillingInvoice{}, fmt.Errorf("billing invoice currency must be a three-letter code")
	}
	dueDays := input.DueDays
	if dueDays <= 0 {
		dueDays = 30
	}
	if dueDays > 365 {
		return BillingInvoice{}, fmt.Errorf("billing invoice due_days cannot exceed 365")
	}

	snapshot, ok := s.usageSnapshotForInvoiceLocked(orgID, input.UsageSnapshotID)
	if !ok {
		return BillingInvoice{}, fmt.Errorf("%w: usage snapshot %s", ErrNotFound, input.UsageSnapshotID)
	}
	if snapshot.ID == "" {
		sampledAt := time.Now().UTC()
		snapshot = UsageSnapshot{
			ID:        newID(),
			OrgID:     orgID,
			Metrics:   cloneOrgUsage(s.orgMeteringLocked(orgID, sampledAt)),
			SampledAt: sampledAt,
		}
		s.usageSnapshots[orgID] = append(s.usageSnapshots[orgID], snapshot)
	}

	lineItems := billingLineItemsForUsage(snapshot.Metrics)
	total := int64(0)
	for _, item := range lineItems {
		total += item.AmountCents
	}
	periodStart := time.Date(snapshot.SampledAt.Year(), snapshot.SampledAt.Month(), 1, 0, 0, 0, 0, time.UTC)
	now := time.Now().UTC()
	invoice := BillingInvoice{
		ID:              newID(),
		OrgID:           orgID,
		UsageSnapshotID: snapshot.ID,
		Number:          fmt.Sprintf("SDP-%s-%04d", snapshot.SampledAt.Format("200601"), len(s.billingInvoices[orgID])+1),
		Status:          status,
		Currency:        currency,
		PeriodStart:     periodStart,
		PeriodEnd:       snapshot.SampledAt,
		DueAt:           now.AddDate(0, 0, dueDays),
		SubtotalCents:   total,
		TotalCents:      total,
		LineItems:       lineItems,
		Metrics:         cloneOrgUsage(snapshot.Metrics),
		CreatedAt:       now,
	}
	s.billingInvoices[orgID] = append(s.billingInvoices[orgID], invoice)
	return cloneBillingInvoice(invoice), nil
}

func (s *MemoryStore) usageSnapshotForInvoiceLocked(orgID string, snapshotID string) (UsageSnapshot, bool) {
	snapshotID = strings.TrimSpace(snapshotID)
	snapshots := s.usageSnapshots[orgID]
	if snapshotID == "" {
		if len(snapshots) == 0 {
			return UsageSnapshot{}, true
		}
		latest := snapshots[0]
		for _, snapshot := range snapshots[1:] {
			if snapshot.SampledAt.After(latest.SampledAt) {
				latest = snapshot
			}
		}
		return cloneUsageSnapshot(latest), true
	}
	for _, snapshot := range snapshots {
		if snapshot.ID == snapshotID {
			return cloneUsageSnapshot(snapshot), true
		}
	}
	return UsageSnapshot{}, false
}

func (s *MemoryStore) orgMeteringLocked(orgID string, sampledAt time.Time) OrgUsage {
	projectRefs := map[string]struct{}{}
	usage := OrgUsage{
		OrgID:            orgID,
		ProjectsByStatus: map[string]int{},
		SampledAt:        sampledAt,
	}
	for _, project := range s.projects {
		if project.OrgID != orgID {
			continue
		}
		projectRefs[project.Ref] = struct{}{}
		usage.Resources = addHostCapacity(usage.Resources, resourceReservationForSpec(project.Spec))
		usage.ProjectsByStatus[string(project.Status)]++
		s.addRegisteredProjectChildOrgUsageLocked(project.Ref, &usage)
		usage.DatabaseExtensions += countEnabledDatabaseExtensions(project.Ref, s.databaseExtensions[project.Ref])
		if policy, ok := s.cdnPolicies[project.Ref]; ok && policy.Enabled {
			usage.CDNEnabledProjects++
		}
	}
	for ref, replicas := range s.replicas {
		project, ok := s.projects[ref]
		if !ok || project.OrgID != orgID {
			continue
		}
		usage.ReadReplicas += len(replicas)
		for _, replica := range replicas {
			usage.Resources = addHostCapacity(usage.Resources, replicaReservationForTier(replica.Tier))
		}
	}
	for _, backup := range s.backups {
		if _, ok := projectRefs[backup.ProjectRef]; !ok {
			continue
		}
		usage.BackupCount++
		usage.BackupStorageBytes += backup.SizeBytes
	}
	for _, archive := range s.walArchives {
		if _, ok := projectRefs[archive.ProjectRef]; !ok {
			continue
		}
		usage.WALArchives++
		usage.WALArchiveBytes += archive.SizeBytes
	}
	for _, log := range s.projectLogs {
		if _, ok := projectRefs[log.ProjectRef]; ok {
			usage.ProjectLogEvents++
		}
	}
	usage.DBAllocatedBytes = int64(usage.Resources.DiskGB) * 1024 * 1024 * 1024
	usage.StorageBytes = usage.BackupStorageBytes + usage.WALArchiveBytes
	return usage
}

func billingLineItemsForUsage(usage OrgUsage) []BillingLineItem {
	items := []BillingLineItem{
		billingLineItem("projects", "Dedicated Supabase projects", int64(usage.Resources.Project), "project", 2000),
		billingLineItem("cpu", "Allocated vCPU", int64(usage.Resources.CPU), "vCPU", 500),
		billingLineItem("ram", "Allocated RAM", int64(usage.Resources.RAMMB+1023)/1024, "GB", 100),
		billingLineItem("disk", "Allocated database disk", int64(usage.Resources.DiskGB), "GB", 10),
		billingLineItem("storage", "Object storage", bytesToBillableGiB(usage.StorageBytes), "GB", 2),
		billingLineItem("egress", "Network egress", bytesToBillableGiB(usage.EgressBytes), "GB", 9),
		billingLineItem("function_invocations", "Edge Function invocations", (usage.FunctionInvocations+99999)/100000, "100k calls", 20),
		billingLineItem("auth_maus", "Auth monthly active users", int64(usage.AuthMAUs), "MAU", 1),
	}
	out := make([]BillingLineItem, 0, len(items))
	for _, item := range items {
		if item.Quantity > 0 {
			out = append(out, item)
		}
	}
	return out
}

func billingLineItem(key string, description string, quantity int64, unit string, unitPriceCents int64) BillingLineItem {
	if quantity < 0 {
		quantity = 0
	}
	return BillingLineItem{
		Key:            key,
		Description:    description,
		Quantity:       quantity,
		Unit:           unit,
		UnitPriceCents: unitPriceCents,
		AmountCents:    quantity * unitPriceCents,
	}
}

func bytesToBillableGiB(value int64) int64 {
	if value <= 0 {
		return 0
	}
	const gib = int64(1024 * 1024 * 1024)
	return (value + gib - 1) / gib
}

func (s *MemoryStore) GetOrgAccessReview(ctx context.Context, orgID string) (OrgAccessReview, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return OrgAccessReview{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}

	members := make([]Membership, 0, len(s.memberships[orgID]))
	membersByEmail := map[string]Membership{}
	for _, member := range s.memberships[orgID] {
		members = append(members, member)
		membersByEmail[member.Email] = member
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].Email < members[j].Email
	})

	teams := make([]TeamAccessReview, 0, len(s.teams[orgID]))
	for _, team := range s.teams[orgID] {
		teamMembers := make([]TeamMember, 0, len(s.teamMembers[team.ID]))
		for _, member := range s.teamMembers[team.ID] {
			teamMembers = append(teamMembers, member)
		}
		sort.Slice(teamMembers, func(i, j int) bool {
			return teamMembers[i].Email < teamMembers[j].Email
		})
		teams = append(teams, TeamAccessReview{Team: team, Members: teamMembers})
	}
	sort.Slice(teams, func(i, j int) bool {
		return teams[i].Team.Name < teams[j].Team.Name
	})

	projects := make([]ProjectAccessReview, 0)
	for _, project := range s.projects {
		if project.OrgID != orgID {
			continue
		}
		effective := map[string]EffectiveProjectRole{}
		for _, member := range members {
			effective[member.Email] = EffectiveProjectRole{
				UserID:  member.UserID,
				Email:   member.Email,
				Role:    member.Role,
				Sources: []string{"org:" + member.Role},
			}
		}
		grants := append([]ProjectAccessGrant(nil), s.projectAccess[project.Ref]...)
		sort.Slice(grants, func(i, j int) bool {
			if grants[i].SubjectType == grants[j].SubjectType {
				return grants[i].SubjectName < grants[j].SubjectName
			}
			return grants[i].SubjectType < grants[j].SubjectType
		})
		for _, grant := range grants {
			switch grant.SubjectType {
			case "user":
				for _, member := range members {
					if member.UserID == grant.SubjectID {
						mergeEffectiveRole(effective, member.UserID, member.Email, grant.Role, "project:user:"+grant.Role)
						break
					}
				}
			case "team":
				for _, member := range s.teamMembers[grant.SubjectID] {
					if member.OrgID == orgID {
						mergeEffectiveRole(effective, member.UserID, member.Email, grant.Role, "project:team:"+grant.SubjectName+":"+grant.Role)
					}
				}
			}
		}
		effectiveRoles := make([]EffectiveProjectRole, 0, len(effective))
		for _, role := range effective {
			effectiveRoles = append(effectiveRoles, role)
		}
		sort.Slice(effectiveRoles, func(i, j int) bool {
			return effectiveRoles[i].Email < effectiveRoles[j].Email
		})
		projects = append(projects, ProjectAccessReview{
			ProjectRef:  project.Ref,
			ProjectName: project.Name,
			Grants:      grants,
			Effective:   effectiveRoles,
		})
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].ProjectRef < projects[j].ProjectRef
	})

	return OrgAccessReview{
		OrgID:       orgID,
		Members:     members,
		Teams:       teams,
		Projects:    projects,
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func (s *MemoryStore) ListOrgMembers(ctx context.Context, orgID string) ([]Membership, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return nil, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	members := make([]Membership, 0, len(s.memberships[orgID]))
	for _, member := range s.memberships[orgID] {
		members = append(members, member)
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].CreatedAt.Before(members[j].CreatedAt)
	})
	return members, nil
}

func (s *MemoryStore) UpsertOrgMember(ctx context.Context, orgID string, input MembershipInput) (Membership, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if email == "" {
		return Membership{}, fmt.Errorf("member email is required")
	}
	role, err := normalizeMembershipRole(input.Role)
	if err != nil {
		return Membership{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return Membership{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	user, ok := s.users[email]
	if !ok {
		user = User{
			ID:           newID(),
			Email:        email,
			PasswordHash: hashPassword(randomToken("invite", 24)),
			Role:         "member",
			CreatedAt:    time.Now().UTC(),
		}
		s.users[email] = user
	}
	if s.memberships[orgID] == nil {
		s.memberships[orgID] = map[string]Membership{}
	}
	member := s.memberships[orgID][email]
	if member.CreatedAt.IsZero() {
		member.CreatedAt = time.Now().UTC()
	}
	member.UserID = user.ID
	member.OrgID = orgID
	member.Email = email
	member.Role = role
	s.memberships[orgID][email] = member
	return member, nil
}

func (s *MemoryStore) DeleteOrgMember(ctx context.Context, orgID string, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return fmt.Errorf("member email is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	if _, ok := s.memberships[orgID][email]; !ok {
		return fmt.Errorf("%w: member %s for org %s", ErrNotFound, email, orgID)
	}
	userID := s.memberships[orgID][email].UserID
	delete(s.memberships[orgID], email)
	for teamID, members := range s.teamMembers {
		if member, ok := members[email]; ok && member.OrgID == orgID {
			delete(members, email)
			s.teamMembers[teamID] = members
		}
	}
	for ref, grants := range s.projectAccess {
		project, ok := s.projects[ref]
		if !ok || project.OrgID != orgID {
			continue
		}
		filtered := grants[:0]
		for _, grant := range grants {
			if grant.SubjectType == "user" && grant.SubjectID == userID {
				continue
			}
			filtered = append(filtered, grant)
		}
		s.projectAccess[ref] = append([]ProjectAccessGrant(nil), filtered...)
	}
	return nil
}

func (s *MemoryStore) ListOrgTeams(ctx context.Context, orgID string) ([]Team, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return nil, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	teams := make([]Team, 0, len(s.teams[orgID]))
	for _, team := range s.teams[orgID] {
		teams = append(teams, team)
	}
	sort.Slice(teams, func(i, j int) bool {
		return teams[i].Name < teams[j].Name
	})
	return teams, nil
}

func (s *MemoryStore) CreateOrgTeam(ctx context.Context, orgID string, input TeamInput) (Team, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Team{}, fmt.Errorf("team name is required")
	}
	slug := normalizeTeamSlug(input.Slug)
	if slug == "" {
		slug = normalizeTeamSlug(name)
	}
	if !teamSlugPattern.MatchString(slug) {
		return Team{}, fmt.Errorf("team slug must be 2-64 lowercase letters, numbers, or hyphens")
	}

	team := Team{
		ID:        newID(),
		OrgID:     orgID,
		Name:      name,
		Slug:      slug,
		CreatedAt: time.Now().UTC(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return Team{}, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}
	if s.teams[orgID] == nil {
		s.teams[orgID] = map[string]Team{}
	}
	if _, ok := s.teams[orgID][slug]; ok {
		return Team{}, fmt.Errorf("%w: team %s already exists", ErrConflict, slug)
	}
	s.teams[orgID][slug] = team
	return team, nil
}

func (s *MemoryStore) DeleteOrgTeam(ctx context.Context, orgID string, slug string) error {
	slug = normalizeTeamSlug(slug)
	s.mu.Lock()
	defer s.mu.Unlock()
	team, ok := s.teams[orgID][slug]
	if !ok {
		return fmt.Errorf("%w: team %s for org %s", ErrNotFound, slug, orgID)
	}
	delete(s.teams[orgID], slug)
	delete(s.teamMembers, team.ID)
	for ref, grants := range s.projectAccess {
		project, ok := s.projects[ref]
		if !ok || project.OrgID != orgID {
			continue
		}
		filtered := grants[:0]
		for _, grant := range grants {
			if grant.SubjectType == "team" && grant.SubjectID == team.ID {
				continue
			}
			filtered = append(filtered, grant)
		}
		s.projectAccess[ref] = append([]ProjectAccessGrant(nil), filtered...)
	}
	return nil
}

func (s *MemoryStore) ListTeamMembers(ctx context.Context, orgID string, slug string) ([]TeamMember, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	team, ok := s.teams[orgID][normalizeTeamSlug(slug)]
	if !ok {
		return nil, fmt.Errorf("%w: team %s for org %s", ErrNotFound, slug, orgID)
	}
	members := make([]TeamMember, 0, len(s.teamMembers[team.ID]))
	for _, member := range s.teamMembers[team.ID] {
		members = append(members, member)
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].Email < members[j].Email
	})
	return members, nil
}

func (s *MemoryStore) UpsertTeamMember(ctx context.Context, orgID string, slug string, input TeamMemberInput) (TeamMember, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if email == "" {
		return TeamMember{}, fmt.Errorf("team member email is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	team, ok := s.teams[orgID][normalizeTeamSlug(slug)]
	if !ok {
		return TeamMember{}, fmt.Errorf("%w: team %s for org %s", ErrNotFound, slug, orgID)
	}
	user, ok := s.users[email]
	if !ok {
		user = User{
			ID:           newID(),
			Email:        email,
			PasswordHash: hashPassword(randomToken("invite", 24)),
			Role:         "member",
			CreatedAt:    time.Now().UTC(),
		}
		s.users[email] = user
	}
	if s.memberships[orgID] == nil {
		s.memberships[orgID] = map[string]Membership{}
	}
	if _, ok := s.memberships[orgID][email]; !ok {
		s.memberships[orgID][email] = Membership{
			UserID:    user.ID,
			OrgID:     orgID,
			Email:     email,
			Role:      "viewer",
			CreatedAt: time.Now().UTC(),
		}
	}
	if s.teamMembers[team.ID] == nil {
		s.teamMembers[team.ID] = map[string]TeamMember{}
	}
	member := s.teamMembers[team.ID][email]
	if member.CreatedAt.IsZero() {
		member.CreatedAt = time.Now().UTC()
	}
	member.TeamID = team.ID
	member.OrgID = orgID
	member.TeamSlug = team.Slug
	member.UserID = user.ID
	member.Email = email
	s.teamMembers[team.ID][email] = member
	return member, nil
}

func (s *MemoryStore) DeleteTeamMember(ctx context.Context, orgID string, slug string, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return fmt.Errorf("team member email is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	team, ok := s.teams[orgID][normalizeTeamSlug(slug)]
	if !ok {
		return fmt.Errorf("%w: team %s for org %s", ErrNotFound, slug, orgID)
	}
	if _, ok := s.teamMembers[team.ID][email]; !ok {
		return fmt.Errorf("%w: team member %s", ErrNotFound, email)
	}
	delete(s.teamMembers[team.ID], email)
	return nil
}

func (s *MemoryStore) ListProjectAccess(ctx context.Context, ref string) ([]ProjectAccessGrant, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.projectAccess[ref], func(grants []ProjectAccessGrant) []ProjectAccessGrant {
		return append([]ProjectAccessGrant(nil), grants...)
	}, func(left, right ProjectAccessGrant) bool {
		if left.SubjectType == right.SubjectType {
			return left.SubjectName < right.SubjectName
		}
		return left.SubjectType < right.SubjectType
	}), nil
}

func (s *MemoryStore) UpsertProjectAccess(ctx context.Context, ref string, input ProjectAccessInput) (ProjectAccessGrant, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	role, err := normalizeMembershipRole(input.Role)
	if err != nil {
		return ProjectAccessGrant{}, err
	}
	subjectType := strings.ToLower(strings.TrimSpace(input.SubjectType))
	subjectID := strings.TrimSpace(input.SubjectID)
	if subjectID == "" {
		return ProjectAccessGrant{}, fmt.Errorf("subject id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectAccessGrant{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	subjectName := subjectID
	switch subjectType {
	case "user":
		email := strings.ToLower(subjectID)
		user, ok := s.users[email]
		if !ok {
			return ProjectAccessGrant{}, fmt.Errorf("%w: user %s", ErrNotFound, email)
		}
		member, ok := s.memberships[project.OrgID][email]
		if !ok {
			return ProjectAccessGrant{}, fmt.Errorf("%w: member %s for org %s", ErrNotFound, email, project.OrgID)
		}
		subjectID = user.ID
		subjectName = member.Email
	case "team":
		team, ok := s.teams[project.OrgID][normalizeTeamSlug(subjectID)]
		if !ok {
			return ProjectAccessGrant{}, fmt.Errorf("%w: team %s for org %s", ErrNotFound, subjectID, project.OrgID)
		}
		subjectID = team.ID
		subjectName = team.Name
	default:
		return ProjectAccessGrant{}, fmt.Errorf("subject type must be user or team")
	}
	grants := s.projectAccess[ref]
	for i, grant := range grants {
		if grant.SubjectType == subjectType && grant.SubjectID == subjectID {
			grant.Role = role
			grant.SubjectName = subjectName
			grants[i] = grant
			s.projectAccess[ref] = grants
			return grant, nil
		}
	}
	grant := ProjectAccessGrant{
		ID:          newID(),
		ProjectRef:  ref,
		OrgID:       project.OrgID,
		SubjectType: subjectType,
		SubjectID:   subjectID,
		SubjectName: subjectName,
		Role:        role,
		CreatedAt:   time.Now().UTC(),
	}
	s.projectAccess[ref] = append(grants, grant)
	return grant, nil
}

func (s *MemoryStore) DeleteProjectAccess(ctx context.Context, ref string, subjectType string, subjectID string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	subjectType = strings.ToLower(strings.TrimSpace(subjectType))
	subjectID = strings.TrimSpace(subjectID)
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	normalizedSubjectID, err := s.resolveAccessSubjectIDLocked(project.OrgID, subjectType, subjectID)
	if err != nil {
		return err
	}
	grants := s.projectAccess[ref]
	filtered := grants[:0]
	removed := false
	for _, grant := range grants {
		if grant.SubjectType == subjectType && grant.SubjectID == normalizedSubjectID {
			removed = true
			continue
		}
		filtered = append(filtered, grant)
	}
	if !removed {
		return fmt.Errorf("%w: project access grant", ErrNotFound)
	}
	s.projectAccess[ref] = append([]ProjectAccessGrant(nil), filtered...)
	return nil
}

func (s *MemoryStore) ResolveProjectRole(ctx context.Context, ref string, email string) (string, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	email = strings.ToLower(strings.TrimSpace(email))
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.projects[ref]
	if !ok {
		return "", fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	user, ok := s.users[email]
	if !ok {
		return "", fmt.Errorf("%w: user %s", ErrNotFound, email)
	}
	best := ""
	for _, grant := range s.projectAccess[ref] {
		if grant.SubjectType == "user" && grant.SubjectID == user.ID {
			best = higherRole(best, grant.Role)
			continue
		}
		if grant.SubjectType != "team" {
			continue
		}
		for _, member := range s.teamMembers[grant.SubjectID] {
			if member.OrgID == project.OrgID && member.Email == email {
				best = higherRole(best, grant.Role)
				break
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("%w: project access for %s", ErrNotFound, email)
	}
	return best, nil
}

func (s *MemoryStore) CreateHost(ctx context.Context, req CreateHostRequest) (Host, error) {
	name := strings.TrimSpace(req.Name)
	address := strings.TrimSpace(req.Address)
	if name == "" {
		return Host{}, fmt.Errorf("host name is required")
	}
	if address == "" {
		return Host{}, fmt.Errorf("host address is required")
	}
	if req.Capacity.CPU < 0 || req.Capacity.RAMMB < 0 || req.Capacity.DiskGB < 0 || req.Capacity.Project < 0 {
		return Host{}, fmt.Errorf("host capacity cannot be negative")
	}
	host := Host{
		ID:        newID(),
		Name:      name,
		Address:   address,
		Capacity:  req.Capacity,
		Used:      HostCapacity{},
		CreatedAt: time.Now().UTC(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts[host.ID] = host
	return host, nil
}

func (s *MemoryStore) ListHosts(ctx context.Context) ([]Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hosts := make([]Host, 0, len(s.hosts))
	for _, host := range s.hosts {
		hosts = append(hosts, host)
	}
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].CreatedAt.Before(hosts[j].CreatedAt)
	})
	return hosts, nil
}

func (s *MemoryStore) GetHost(ctx context.Context, id string) (Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	host, ok := s.hosts[id]
	if !ok {
		return Host{}, fmt.Errorf("%w: host %s", ErrNotFound, id)
	}
	return host, nil
}

func (s *MemoryStore) DeleteHost(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	host, ok := s.hosts[id]
	if !ok {
		return fmt.Errorf("%w: host %s", ErrNotFound, id)
	}
	if host.Used.CPU > 0 || host.Used.RAMMB > 0 || host.Used.DiskGB > 0 || host.Used.Project > 0 {
		return fmt.Errorf("%w: host %s still has reserved capacity", ErrConflict, id)
	}
	delete(s.hosts, id)
	return nil
}

func (s *MemoryStore) CreateProject(ctx context.Context, req CreateProjectRequest) (Project, error) {
	req = s.createProjectRequestWithDefaults(req)
	if err := validateCreateProject(req); err != nil {
		return Project{}, err
	}

	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[req.OrgID]; !ok {
		return Project{}, fmt.Errorf("%w: org %s", ErrNotFound, req.OrgID)
	}
	if _, ok := s.projects[req.Ref]; ok {
		return Project{}, fmt.Errorf("%w: project ref %s already exists", ErrConflict, req.Ref)
	}
	if err := s.validateGeneratedProjectHostReservationsLocked(req.Ref, req.Domain); err != nil {
		return Project{}, err
	}
	spec := req.toSpec()
	reservation := resourceReservationForSpec(spec)
	if err := s.validateOrgQuotaLocked(req.OrgID, reservation); err != nil {
		return Project{}, err
	}
	if req.HostID == "" {
		req.HostID = s.defaultHostForReservationLocked(reservation)
		spec.HostID = req.HostID
	}
	if req.HostID != "" {
		host, ok := s.hosts[req.HostID]
		if !ok {
			return Project{}, fmt.Errorf("%w: host %s", ErrNotFound, req.HostID)
		}
		if !hostHasCapacity(host, reservation) {
			return Project{}, fmt.Errorf("%w: host %s has insufficient capacity", ErrConflict, req.HostID)
		}
		host.Used = addHostCapacity(host.Used, reservation)
		s.hosts[host.ID] = host
	}
	project := Project{
		ID:        newID(),
		Ref:       req.Ref,
		OrgID:     req.OrgID,
		Name:      req.Name,
		Status:    ProjectProvisioning,
		Message:   "project accepted for provisioning",
		Spec:      spec,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.projects[project.Ref] = project
	if project.Spec.Profile == StackProfileOrioleDB {
		if s.configs[project.Ref] == nil {
			s.configs[project.Ref] = map[string]ProjectConfig{}
		}
		config := defaultProjectConfig(project.Ref, "database")
		config.Config["orioledb_profile"] = "preview"
		config.UpdatedAt = now
		s.configs[project.Ref]["database"] = config
	}
	s.secrets[project.Ref] = generateProjectSecrets(project.Ref)
	s.policies[project.Ref] = defaultBackupPolicyForSchedule(project.Ref, s.platformDefaults.BackupSchedule)
	s.pitrPolicies[project.Ref] = defaultPITRPolicy(project.Ref)
	return project, nil
}

func (s *MemoryStore) defaultHostForReservationLocked(reservation HostCapacity) string {
	type candidate struct {
		id       string
		projects int
		cpu      int
	}
	candidates := make([]candidate, 0, len(s.hosts))
	for _, host := range s.hosts {
		if hostHasCapacity(host, reservation) {
			candidates = append(candidates, candidate{id: host.ID, projects: host.Used.Project, cpu: host.Used.CPU})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].projects != candidates[j].projects {
			return candidates[i].projects < candidates[j].projects
		}
		if candidates[i].cpu != candidates[j].cpu {
			return candidates[i].cpu < candidates[j].cpu
		}
		return candidates[i].id < candidates[j].id
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].id
}

func (s *MemoryStore) createProjectRequestWithDefaults(req CreateProjectRequest) CreateProjectRequest {
	s.mu.RLock()
	defaults := normalizedPlatformDefaults(s.platformDefaults)
	s.mu.RUnlock()
	if strings.TrimSpace(req.Domain) == "" {
		req.Domain = defaults.Domain
	}
	if strings.TrimSpace(req.StackVersion) == "" {
		req.StackVersion = defaults.StackVersion
	}
	req.StackVersion = NormalizeStackReleaseVersion(req.StackVersion)
	if req.Profile == "" {
		req.Profile = defaults.Profile
	}
	if req.ResourceTier == "" {
		req.ResourceTier = defaults.ResourceTier
	}
	return req
}

func (s *MemoryStore) CreateProjectBranch(ctx context.Context, sourceRef string, input ProjectBranchInput) (ProjectBranch, Project, error) {
	sourceRef = strings.ToLower(strings.TrimSpace(sourceRef))
	branchRef := strings.ToLower(strings.TrimSpace(input.Ref))
	if !projectRefPattern.MatchString(branchRef) {
		return ProjectBranch{}, Project{}, fmt.Errorf("branch ref must be 3-55 lowercase letters, numbers, or hyphens, and cannot start or end with a hyphen")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = branchRef
	}
	if input.TTLHours < 0 {
		return ProjectBranch{}, Project{}, fmt.Errorf("ttl_hours cannot be negative")
	}

	now := time.Now().UTC()
	var expiresAt *time.Time
	if input.TTLHours > 0 {
		expires := now.Add(time.Duration(input.TTLHours) * time.Hour)
		expiresAt = &expires
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	source, ok := s.projects[sourceRef]
	if !ok {
		return ProjectBranch{}, Project{}, fmt.Errorf("%w: project %s", ErrNotFound, sourceRef)
	}
	if _, ok := s.projects[branchRef]; ok {
		return ProjectBranch{}, Project{}, fmt.Errorf("%w: project ref %s already exists", ErrConflict, branchRef)
	}

	environment := cloneStringMap(source.Spec.Environment)
	environment["SUPADUPA_BRANCH_SOURCE_REF"] = source.Ref
	req := CreateProjectRequest{
		OrgID:         source.OrgID,
		Ref:           branchRef,
		Name:          name,
		HostID:        source.Spec.HostID,
		Domain:        source.Spec.Domain,
		StackVersion:  source.Spec.StackVersion,
		Profile:       source.Spec.Profile,
		ResourceTier:  source.Spec.ResourceTier,
		CPU:           source.Spec.CPU,
		RAMMB:         source.Spec.RAMMB,
		DiskGB:        source.Spec.DiskGB,
		EnforceLimits: source.Spec.EnforceLimits,
		Services:      serviceEnabledMap(source.Spec.Services),
		Environment:   environment,
	}
	if err := validateCreateProject(req); err != nil {
		return ProjectBranch{}, Project{}, err
	}
	if err := s.validateGeneratedProjectHostReservationsLocked(req.Ref, req.Domain); err != nil {
		return ProjectBranch{}, Project{}, err
	}

	project := Project{
		ID:        newID(),
		Ref:       req.Ref,
		OrgID:     req.OrgID,
		Name:      req.Name,
		Status:    ProjectProvisioning,
		Message:   "branch accepted for provisioning",
		Spec:      req.toSpec(),
		CreatedAt: now,
		UpdatedAt: now,
	}
	reservation := resourceReservationForSpec(project.Spec)
	if err := s.validateOrgQuotaLocked(project.OrgID, reservation); err != nil {
		return ProjectBranch{}, Project{}, err
	}
	if project.Spec.HostID != "" {
		host, ok := s.hosts[project.Spec.HostID]
		if !ok {
			return ProjectBranch{}, Project{}, fmt.Errorf("%w: host %s", ErrNotFound, project.Spec.HostID)
		}
		if !hostHasCapacity(host, reservation) {
			return ProjectBranch{}, Project{}, fmt.Errorf("%w: host %s has insufficient capacity for %s tier", ErrConflict, project.Spec.HostID, project.Spec.ResourceTier)
		}
		host.Used = addHostCapacity(host.Used, reservation)
		s.hosts[host.ID] = host
	}

	branch := ProjectBranch{
		ID:               newID(),
		SourceProjectRef: source.Ref,
		ProjectRef:       project.Ref,
		Name:             name,
		WithData:         input.WithData,
		Status:           string(project.Status),
		CreatedAt:        now,
		ExpiresAt:        expiresAt,
	}
	s.projects[project.Ref] = project
	s.branches[source.Ref] = append(s.branches[source.Ref], branch)
	s.secrets[project.Ref] = generateProjectSecrets(project.Ref)
	s.policies[project.Ref] = defaultBackupPolicy(project.Ref)
	s.pitrPolicies[project.Ref] = defaultPITRPolicy(project.Ref)
	return branch, project, nil
}

func (s *MemoryStore) ListProjectBranches(ctx context.Context, sourceRef string) ([]ProjectBranch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sourceRef = strings.ToLower(strings.TrimSpace(sourceRef))
	if _, ok := s.projects[sourceRef]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, sourceRef)
	}
	branches := append([]ProjectBranch(nil), s.branches[sourceRef]...)
	for index := range branches {
		if project, ok := s.projects[branches[index].ProjectRef]; ok {
			branches[index].Status = string(project.Status)
		}
	}
	sort.Slice(branches, func(i, j int) bool {
		return branches[i].CreatedAt.Before(branches[j].CreatedAt)
	})
	if branches == nil {
		branches = []ProjectBranch{}
	}
	return branches, nil
}

func (s *MemoryStore) DeleteProjectBranch(ctx context.Context, sourceRef string, branchRef string) error {
	sourceRef = strings.ToLower(strings.TrimSpace(sourceRef))
	branchRef = strings.ToLower(strings.TrimSpace(branchRef))

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[sourceRef]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, sourceRef)
	}
	found := false
	for _, branch := range s.branches[sourceRef] {
		if branch.ProjectRef == branchRef {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: branch %s", ErrNotFound, branchRef)
	}
	return s.deleteProjectLocked(branchRef)
}

func (s *MemoryStore) CreateProjectReplica(ctx context.Context, ref string, input ProjectReplicaInput) (ProjectReplica, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name := strings.ToLower(strings.TrimSpace(input.Name))
	tier := input.Tier
	region := strings.TrimSpace(input.Region)
	hostID := strings.TrimSpace(input.HostID)
	readWeight := input.ReadWeight
	if readWeight <= 0 {
		readWeight = 100
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectReplica{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if tier == "" {
		tier = project.Spec.ResourceTier
	}
	if name == "" {
		name = fmt.Sprintf("replica-%d", len(s.replicas[ref])+1)
	}
	failoverPriority := input.FailoverPriority
	if failoverPriority <= 0 {
		failoverPriority = len(s.replicas[ref]) + 1
	}
	normalizedName, err := normalizeReplicaName(name)
	if err != nil {
		return ProjectReplica{}, err
	}
	if err := validateReplicaPublicDNSHost(ref, normalizedName, project.Spec.Domain); err != nil {
		return ProjectReplica{}, err
	}
	for _, replica := range s.replicas[ref] {
		if replica.Name == normalizedName {
			return ProjectReplica{}, fmt.Errorf("%w: replica %s for project %s already exists", ErrConflict, normalizedName, ref)
		}
	}
	reservation := replicaReservationForTier(tier)
	if err := s.validateOrgReplicaQuotaLocked(project.OrgID, reservation); err != nil {
		return ProjectReplica{}, err
	}
	if hostID != "" {
		host, ok := s.hosts[hostID]
		if !ok {
			return ProjectReplica{}, fmt.Errorf("%w: host %s", ErrNotFound, hostID)
		}
		if !hostHasCapacity(host, reservation) {
			return ProjectReplica{}, fmt.Errorf("%w: host %s has insufficient capacity for %s replica tier", ErrConflict, hostID, tier)
		}
		host.Used = addHostCapacity(host.Used, reservation)
		s.hosts[host.ID] = host
	}
	now := time.Now().UTC()
	internalReadURI := fmt.Sprintf("postgres://postgres:${DB_PASSWORD}@%s.%s.replica.internal:5432/postgres", normalizedName, ref)
	publicReadURI := postgresURIWithSSLMode(fmt.Sprintf("postgres://postgres:${DB_PASSWORD}@%s:5432/postgres", replicaDatabaseHost(ref, normalizedName, project.Spec.Domain)), "require")
	replica := ProjectReplica{
		ID:               newID(),
		ProjectRef:       ref,
		Name:             normalizedName,
		HostID:           hostID,
		Region:           region,
		Tier:             tier,
		Status:           "provisioning",
		Role:             "read",
		Message:          "replica accepted for provisioning",
		ReadURI:          publicReadURI,
		PublicReadURI:    publicReadURI,
		InternalReadURI:  internalReadURI,
		ReadWeight:       readWeight,
		FailoverPriority: failoverPriority,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	s.replicas[ref] = append(s.replicas[ref], replica)
	return replica, nil
}

func (s *MemoryStore) ListProjectReplicas(ctx context.Context, ref string) ([]ProjectReplica, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ref = strings.ToLower(strings.TrimSpace(ref))
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	replicas := append([]ProjectReplica(nil), s.replicas[ref]...)
	sort.Slice(replicas, func(i, j int) bool {
		return replicas[i].CreatedAt.Before(replicas[j].CreatedAt)
	})
	if replicas == nil {
		replicas = []ProjectReplica{}
	}
	return replicas, nil
}

func (s *MemoryStore) UpdateProjectReplicaStatus(ctx context.Context, ref string, replicaID string, status string, message string) (ProjectReplica, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	replicaID = strings.TrimSpace(replicaID)
	status = strings.ToLower(strings.TrimSpace(status))
	if replicaID == "" {
		return ProjectReplica{}, fmt.Errorf("replica id is required")
	}
	if status == "" {
		return ProjectReplica{}, fmt.Errorf("replica status is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	replicas := s.replicas[ref]
	for index, replica := range replicas {
		if replica.ID == replicaID {
			replica.Status = status
			if replica.Role == "" {
				replica.Role = "read"
			}
			if replica.ReadWeight <= 0 {
				replica.ReadWeight = 100
			}
			if replica.FailoverPriority <= 0 {
				replica.FailoverPriority = index + 1
			}
			replica.Message = strings.TrimSpace(message)
			replica.UpdatedAt = time.Now().UTC()
			replicas[index] = replica
			s.replicas[ref] = replicas
			return replica, nil
		}
	}
	return ProjectReplica{}, fmt.Errorf("%w: replica %s for project %s", ErrNotFound, replicaID, ref)
}

func (s *MemoryStore) DeleteProjectReplica(ctx context.Context, ref string, replicaID string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	replicaID = strings.TrimSpace(replicaID)
	if replicaID == "" {
		return fmt.Errorf("replica id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	replicas := s.replicas[ref]
	filtered := replicas[:0]
	removed := false
	var removedReplica ProjectReplica
	for _, replica := range replicas {
		if replica.ID != replicaID {
			filtered = append(filtered, replica)
			continue
		}
		if replica.Role == "primary" {
			return fmt.Errorf("%w: promoted primary replica %s cannot be deleted", ErrConflict, replicaID)
		}
		removed = true
		removedReplica = replica
	}
	if !removed {
		return fmt.Errorf("%w: replica %s for project %s", ErrNotFound, replicaID, ref)
	}
	if removedReplica.HostID != "" {
		if host, ok := s.hosts[removedReplica.HostID]; ok {
			host.Used = subtractHostCapacity(host.Used, replicaReservationForTier(removedReplica.Tier))
			s.hosts[host.ID] = host
		}
	}
	if len(filtered) == 0 {
		delete(s.replicas, ref)
	} else {
		s.replicas[ref] = append([]ProjectReplica(nil), filtered...)
	}
	return nil
}

func (s *MemoryStore) GetProjectReplicaRouting(ctx context.Context, ref string) (ProjectReplicaRouting, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ref = strings.ToLower(strings.TrimSpace(ref))
	project, ok := s.projects[ref]
	if !ok {
		return ProjectReplicaRouting{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return s.projectReplicaRoutingLocked(project, s.replicas[ref]), nil
}

func (s *MemoryStore) PromoteProjectReplica(ctx context.Context, ref string, replicaID string, reason string) (ProjectReplica, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	replicaID = strings.TrimSpace(replicaID)
	if replicaID == "" {
		return ProjectReplica{}, fmt.Errorf("replica id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectReplica{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	now := time.Now().UTC()
	replicas := s.replicas[ref]
	for index, replica := range replicas {
		if replica.ID != replicaID {
			continue
		}
		if replica.Status != "healthy" {
			return ProjectReplica{}, fmt.Errorf("%w: replica %s for project %s is not healthy", ErrConflict, replicaID, ref)
		}
		for otherIndex := range replicas {
			if replicas[otherIndex].Role == "primary" {
				replicas[otherIndex].Role = "read"
				replicas[otherIndex].UpdatedAt = now
			}
		}
		replica.Role = "primary"
		replica.Message = defaultReplicaMessage(strings.TrimSpace(reason), "replica promoted for failover")
		replica.PromotedAt = &now
		replica.UpdatedAt = now
		replicas[index] = replica
		project.Status = ProjectHealthy
		project.Message = "replica " + replica.Name + " promoted"
		project.UpdatedAt = now
		s.projects[ref] = project
		s.replicas[ref] = replicas
		return replica, nil
	}
	return ProjectReplica{}, fmt.Errorf("%w: replica %s for project %s", ErrNotFound, replicaID, ref)
}

func (s *MemoryStore) FailoverProjectReplica(ctx context.Context, ref string, reason string) (ProjectReplica, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectReplica{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	replicas := s.replicas[ref]
	candidateIndex := -1
	for index, replica := range replicas {
		if replica.Status != "healthy" {
			continue
		}
		if replica.Role == "primary" {
			continue
		}
		if candidateIndex == -1 || compareReplicaFailoverCandidate(replica, replicas[candidateIndex]) < 0 {
			candidateIndex = index
		}
	}
	if candidateIndex == -1 {
		return ProjectReplica{}, fmt.Errorf("%w: project %s has no healthy failover candidate", ErrConflict, ref)
	}
	now := time.Now().UTC()
	for index := range replicas {
		if replicas[index].Role == "primary" {
			replicas[index].Role = "read"
			replicas[index].UpdatedAt = now
		}
	}
	candidate := replicas[candidateIndex]
	candidate.Role = "primary"
	candidate.Message = defaultReplicaMessage(strings.TrimSpace(reason), "automatic failover candidate promoted")
	candidate.PromotedAt = &now
	candidate.UpdatedAt = now
	replicas[candidateIndex] = candidate
	project.Status = ProjectHealthy
	project.Message = "replica " + candidate.Name + " promoted"
	project.UpdatedAt = now
	s.projects[ref] = project
	s.replicas[ref] = replicas
	return candidate, nil
}

func (s *MemoryStore) ListProjectsByOrg(ctx context.Context, orgID string) ([]Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.orgs[orgID]; !ok {
		return nil, fmt.Errorf("%w: org %s", ErrNotFound, orgID)
	}

	projects := make([]Project, 0)
	for _, project := range s.projects {
		if project.OrgID == orgID {
			projects = append(projects, project)
		}
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].CreatedAt.Before(projects[j].CreatedAt)
	})
	return projects, nil
}

func (s *MemoryStore) ListProjects(ctx context.Context) ([]Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	projects := make([]Project, 0, len(s.projects))
	for _, project := range s.projects {
		projects = append(projects, project)
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].CreatedAt.Before(projects[j].CreatedAt)
	})
	return projects, nil
}

func (s *MemoryStore) GetProject(ctx context.Context, ref string) (Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.projects[ref]
	if !ok {
		return Project{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return project, nil
}

func (s *MemoryStore) UpdateProjectStatus(ctx context.Context, ref string, status ProjectPhase, message string) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return Project{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	project.Status = status
	project.Message = message
	project.UpdatedAt = time.Now().UTC()
	s.projects[ref] = project
	return project, nil
}

func (s *MemoryStore) UpdateProjectStackVersion(ctx context.Context, ref string, version string) (Project, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return Project{}, fmt.Errorf("stack version is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return Project{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	project.Spec.StackVersion = version
	project.Message = "stack version updated"
	project.UpdatedAt = time.Now().UTC()
	s.projects[ref] = project
	return project, nil
}

func (s *MemoryStore) UpdateProjectResourceTier(ctx context.Context, ref string, tier ResourceTier) (Project, error) {
	if tier == "" {
		return Project{}, fmt.Errorf("resource tier is required")
	}
	if _, ok := resourceTierReservations[tier]; !ok {
		return Project{}, fmt.Errorf("unsupported resource tier %q", tier)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return Project{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	oldReservation := resourceReservationForSpec(project.Spec)
	newReservation := resourceReservationForTier(tier)
	if err := s.validateProjectScaleQuotaLocked(project.OrgID, oldReservation, newReservation); err != nil {
		return Project{}, err
	}
	if project.Spec.HostID != "" {
		host, ok := s.hosts[project.Spec.HostID]
		if !ok {
			return Project{}, fmt.Errorf("%w: host %s", ErrNotFound, project.Spec.HostID)
		}
		nextUsed := addHostCapacity(subtractHostCapacity(host.Used, oldReservation), newReservation)
		if !capacityWithinLimit(nextUsed.CPU, host.Capacity.CPU) ||
			!capacityWithinLimit(nextUsed.RAMMB, host.Capacity.RAMMB) ||
			!capacityWithinLimit(nextUsed.DiskGB, host.Capacity.DiskGB) ||
			!capacityWithinLimit(nextUsed.Project, host.Capacity.Project) {
			return Project{}, fmt.Errorf("%w: host %s has insufficient capacity for %s tier", ErrConflict, project.Spec.HostID, tier)
		}
		host.Used = nextUsed
		s.hosts[host.ID] = host
	}
	project.Spec.ResourceTier = tier
	// Scaling by preset resets any exact per-dimension overrides so the new
	// tier's defaults take effect cleanly.
	project.Spec.CPU = 0
	project.Spec.RAMMB = 0
	project.Spec.DiskGB = 0
	project.Message = "resource tier updated"
	project.UpdatedAt = time.Now().UTC()
	s.projects[ref] = project
	return project, nil
}

func (s *MemoryStore) GetProjectServices(ctx context.Context, ref string) (ProjectServices, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectServices{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return ProjectServices{
		ProjectRef: project.Ref,
		Services:   ProjectServiceStates(project.Spec.Services),
		UpdatedAt:  project.UpdatedAt,
	}, nil
}

func (s *MemoryStore) UpdateProjectServices(ctx context.Context, ref string, input ProjectServicesInput) (Project, error) {
	services, err := normalizeProjectServices(input.Services)
	if err != nil {
		return Project{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return Project{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	project.Spec.Services = services
	project.Message = "enabled services updated"
	project.UpdatedAt = time.Now().UTC()
	s.projects[ref] = project
	return project, nil
}

func (s *MemoryStore) DeleteProject(ctx context.Context, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteProjectLocked(ref)
}

func (s *MemoryStore) deleteProjectLocked(ref string) error {
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	project := s.projects[ref]
	if project.Spec.HostID != "" {
		if host, ok := s.hosts[project.Spec.HostID]; ok {
			host.Used = subtractHostCapacity(host.Used, resourceReservationForSpec(project.Spec))
			s.hosts[host.ID] = host
		}
	}
	delete(s.projects, ref)
	delete(s.branches, ref)
	for _, replica := range s.replicas[ref] {
		if replica.HostID != "" {
			if host, ok := s.hosts[replica.HostID]; ok {
				host.Used = subtractHostCapacity(host.Used, replicaReservationForTier(replica.Tier))
				s.hosts[host.ID] = host
			}
		}
	}
	delete(s.replicas, ref)
	for sourceRef, branches := range s.branches {
		filtered := branches[:0]
		for _, branch := range branches {
			if branch.ProjectRef != ref {
				filtered = append(filtered, branch)
			}
		}
		if len(filtered) == 0 {
			delete(s.branches, sourceRef)
		} else {
			s.branches[sourceRef] = filtered
		}
	}
	s.cleanupRegisteredProjectChildrenLocked(ref)
	return nil
}

func (s *MemoryStore) UpsertProjectRoutes(ctx context.Context, ref string, routes []ProjectRoute) ([]ProjectRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	now := time.Now().UTC()
	normalized := make([]ProjectRoute, 0, len(routes))
	for _, route := range routes {
		if strings.TrimSpace(route.Name) == "" {
			return nil, fmt.Errorf("route name is required")
		}
		if strings.TrimSpace(route.FQDN) == "" {
			return nil, fmt.Errorf("route fqdn is required")
		}
		if strings.TrimSpace(route.UpstreamURL) == "" {
			return nil, fmt.Errorf("route upstream url is required")
		}
		if route.ID == "" {
			route.ID = newID()
		}
		route.ProjectRef = ref
		if route.CreatedAt.IsZero() {
			route.CreatedAt = now
		}
		route.IPAllowlist = append([]string(nil), route.IPAllowlist...)
		normalized = append(normalized, route)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Name < normalized[j].Name
	})
	s.routes[ref] = cloneProjectRoutes(normalized)
	return cloneProjectRoutes(normalized), nil
}

func (s *MemoryStore) ListProjectRoutes(ctx context.Context, ref string) ([]ProjectRoute, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneProjectRoutes(s.routes[ref]), nil
}

func (s *MemoryStore) DeleteProjectRoutes(ctx context.Context, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	delete(s.routes, ref)
	return nil
}

func (s *MemoryStore) ListProjectDomains(ctx context.Context, ref string) ([]ProjectDomain, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	domains := cloneProjectDomains(s.domains[ref])
	sort.Slice(domains, func(i, j int) bool {
		return domains[i].CreatedAt.Before(domains[j].CreatedAt)
	})
	return domains, nil
}

func (s *MemoryStore) AddProjectDomain(ctx context.Context, ref string, input ProjectDomainInput) (ProjectDomain, error) {
	fqdn, err := normalizeDomain(input.FQDN)
	if err != nil {
		return ProjectDomain{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectDomain{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if owner := s.reservedCustomDomainHostLocked(fqdn); owner != "" {
		return ProjectDomain{}, fmt.Errorf("%w: domain %s is reserved by %s", ErrConflict, fqdn, owner)
	}
	candidateRouteName := routeName(fqdn)
	for projectRef, domains := range s.domains {
		for _, existing := range domains {
			if existing.FQDN == fqdn {
				return ProjectDomain{}, fmt.Errorf("%w: domain %s already exists for project %s", ErrConflict, fqdn, projectRef)
			}
			if projectRef == ref && routeName(existing.FQDN) == candidateRouteName {
				return ProjectDomain{}, fmt.Errorf("%w: domain %s conflicts with route name for existing domain %s in project %s", ErrConflict, fqdn, existing.FQDN, ref)
			}
		}
	}
	now := time.Now().UTC()
	domain := ProjectDomain{
		ProjectRef: project.Ref,
		FQDN:       fqdn,
		CertStatus: "pending",
		CertMode:   "acme",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	s.domains[ref] = append(s.domains[ref], domain)
	return domain, nil
}

func (s *MemoryStore) reservedCustomDomainHostLocked(fqdn string) string {
	reserved := map[string]string{}
	for _, project := range s.projects {
		domain := strings.TrimSpace(project.Spec.Domain)
		addReservedDomainHost(reserved, domain, "project domain "+domain)
		addReservedDomainHost(reserved, projectHost(project.Ref, domain), "project API "+project.Ref)
		addReservedDomainHost(reserved, studioHost(project.Ref, domain), "project Studio "+project.Ref)
		addReservedDomainHost(reserved, storageHost(project.Ref, domain), "project storage "+project.Ref)
		addReservedDomainHost(reserved, databaseHost(project.Ref, domain), "project database "+project.Ref)
		addReservedDomainHost(reserved, poolerHost(project.Ref, domain), "project pooler "+project.Ref)
		for _, replica := range s.replicas[project.Ref] {
			addReservedDomainHost(reserved, replicaDatabaseHost(project.Ref, replica.Name, domain), "project replica "+project.Ref+"/"+replica.Name)
		}
		for _, host := range inferredPlatformHostsForProjectDomain(domain) {
			addReservedDomainHost(reserved, host, "platform host")
		}
	}
	for projectRef, domains := range s.domains {
		for _, domain := range domains {
			addReservedDomainHost(reserved, domain.FQDN, "project custom domain "+projectRef)
		}
	}
	addReservedDomainHost(reserved, os.Getenv("SUPADUPA_ADMIN_HOST"), "platform admin")
	addReservedDomainHost(reserved, os.Getenv("SUPADUPA_API_HOST"), "platform API")
	addReservedDomainHost(reserved, os.Getenv("SUPADUPA_ADMIN_URL"), "platform admin")
	addReservedDomainHost(reserved, os.Getenv("SUPADUPA_API_URL"), "platform API")
	return reserved[fqdn]
}

func (s *MemoryStore) validateGeneratedProjectHostReservationsLocked(ref string, domain string) error {
	domain = strings.Trim(strings.ToLower(strings.TrimSpace(domain)), ".")
	for label, host := range generatedProjectHosts(ref, domain) {
		if owner := s.reservedCustomDomainHostLocked(host); owner != "" {
			return fmt.Errorf("%w: generated %s host %s is reserved by %s", ErrConflict, label, host, owner)
		}
	}
	inferredPlatformHosts := map[string]struct{}{}
	for _, host := range inferredPlatformHostsForProjectDomain(domain) {
		normalized, ok := normalizedHostForDomainReservation(host)
		if ok {
			inferredPlatformHosts[normalized] = struct{}{}
		}
	}
	for label, host := range generatedProjectHosts(ref, domain) {
		normalized, ok := normalizedHostForDomainReservation(host)
		if !ok {
			continue
		}
		if _, reserved := inferredPlatformHosts[normalized]; reserved {
			return fmt.Errorf("%w: generated %s host %s is reserved by platform host topology for %s", ErrConflict, label, host, domain)
		}
	}
	return nil
}

func addReservedDomainHost(out map[string]string, value string, owner string) {
	host, ok := normalizedHostForDomainReservation(value)
	if !ok {
		return
	}
	if _, exists := out[host]; !exists {
		out[host] = owner
	}
}

func inferredPlatformHostsForProjectDomain(domain string) []string {
	domain = strings.Trim(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" {
		return nil
	}
	suffix := domain
	if strings.HasPrefix(domain, "apps.") {
		suffix = strings.TrimPrefix(domain, "apps.")
	}
	return []string{"admin." + suffix, "api." + suffix}
}

func normalizedHostForDomainReservation(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
		value = parsed.Hostname()
	} else {
		value = strings.Split(strings.TrimPrefix(strings.TrimPrefix(value, "https://"), "http://"), "/")[0]
		value = strings.Split(value, ":")[0]
	}
	host, err := normalizeDomain(value)
	if err != nil {
		return "", false
	}
	return host, true
}

func (s *MemoryStore) UpdateProjectDomainCertStatus(ctx context.Context, ref string, fqdn string, status string) (ProjectDomain, error) {
	normalized, err := normalizeDomain(fqdn)
	if err != nil {
		return ProjectDomain{}, err
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return ProjectDomain{}, fmt.Errorf("cert status is required")
	}
	switch status {
	case "pending", "issued", "failed", "uploaded":
	default:
		return ProjectDomain{}, fmt.Errorf("unsupported cert status %q", status)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDomain{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	domains := s.domains[ref]
	for index, domain := range domains {
		if domain.FQDN == normalized {
			domain.CertStatus = status
			domain.UpdatedAt = time.Now().UTC()
			domains[index] = domain
			s.domains[ref] = domains
			return domain, nil
		}
	}
	return ProjectDomain{}, fmt.Errorf("%w: domain %s for project %s", ErrNotFound, normalized, ref)
}

func (s *MemoryStore) UpdateProjectDomainCertificate(ctx context.Context, ref string, fqdn string, metadata ProjectDomainCertificateMetadata) (ProjectDomain, error) {
	normalized, err := normalizeDomain(fqdn)
	if err != nil {
		return ProjectDomain{}, err
	}
	status := strings.ToLower(strings.TrimSpace(metadata.Status))
	if status == "" {
		status = "uploaded"
	}
	switch status {
	case "pending", "issued", "failed", "uploaded":
	default:
		return ProjectDomain{}, fmt.Errorf("unsupported cert status %q", status)
	}
	mode := strings.ToLower(strings.TrimSpace(metadata.Mode))
	if mode == "" {
		mode = "byo"
	}
	switch mode {
	case "acme", "manual", "command", "byo":
	default:
		return ProjectDomain{}, fmt.Errorf("unsupported cert mode %q", mode)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDomain{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	domains := s.domains[ref]
	for index, domain := range domains {
		if domain.FQDN == normalized {
			domain.CertStatus = status
			domain.CertMode = mode
			domain.CertFingerprint = strings.TrimSpace(metadata.Fingerprint)
			domain.CertNotAfter = cloneTimePtr(metadata.NotAfter)
			domain.UpdatedAt = time.Now().UTC()
			domains[index] = domain
			s.domains[ref] = domains
			return domain, nil
		}
	}
	return ProjectDomain{}, fmt.Errorf("%w: domain %s for project %s", ErrNotFound, normalized, ref)
}

func (s *MemoryStore) DeleteProjectDomain(ctx context.Context, ref string, fqdn string) error {
	normalized, err := normalizeDomain(fqdn)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	domains := s.domains[ref]
	for index, domain := range domains {
		if domain.FQDN == normalized {
			s.domains[ref] = append(domains[:index], domains[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: domain %s for project %s", ErrNotFound, normalized, ref)
}

func (s *MemoryStore) GetProjectConfig(ctx context.Context, ref string, area string) (ProjectConfig, error) {
	normalizedArea, err := normalizeConfigArea(area)
	if err != nil {
		return ProjectConfig{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectConfig{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if config, ok := s.configs[ref][normalizedArea]; ok {
		return mergeProjectConfigWithDefaults(ref, normalizedArea, config), nil
	}
	return defaultProjectConfig(ref, normalizedArea), nil
}

func (s *MemoryStore) ListProjectConfigs(ctx context.Context, ref string) ([]ProjectConfig, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	configs := make([]ProjectConfig, 0, len(s.configs[ref]))
	for _, config := range s.configs[ref] {
		configs = append(configs, mergeProjectConfigWithDefaults(ref, config.Area, config))
	}
	sort.Slice(configs, func(i, j int) bool {
		return configs[i].Area < configs[j].Area
	})
	if configs == nil {
		configs = []ProjectConfig{}
	}
	return configs, nil
}

func (s *MemoryStore) UpdateProjectConfig(ctx context.Context, ref string, area string, input ProjectConfigInput) (ProjectConfig, error) {
	normalizedArea, err := normalizeConfigArea(area)
	if err != nil {
		return ProjectConfig{}, err
	}
	configMap, err := normalizeConfigValues(input.Config)
	if err != nil {
		return ProjectConfig{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectConfig{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if s.configs[ref] == nil {
		s.configs[ref] = map[string]ProjectConfig{}
	}
	base := defaultProjectConfig(ref, normalizedArea).Config
	if existing, ok := s.configs[ref][normalizedArea]; ok {
		for key, value := range existing.Config {
			base[key] = value
		}
	}
	for key, value := range configMap {
		base[key] = value
	}
	if normalizedArea == "general" {
		if err := validateGeneralConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "network" {
		if err := validateNetworkConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "auth" {
		if err := validateAuthConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "auth_providers" {
		if err := validateAuthProvidersConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "ai" {
		if err := validateAIConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "pooler" {
		if err := validatePoolerConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "functions" {
		if err := validateFunctionsConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	if normalizedArea == "smtp" {
		if err := validateSMTPConfig(base); err != nil {
			return ProjectConfig{}, err
		}
	}
	config := ProjectConfig{
		ProjectRef: ref,
		Area:       normalizedArea,
		Config:     base,
		UpdatedAt:  time.Now().UTC(),
	}
	s.configs[ref][normalizedArea] = cloneProjectConfig(config)
	return config, nil
}

func (s *MemoryStore) ListProjectAuthClients(ctx context.Context, ref string) ([]ProjectAuthClient, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.authClients[ref], cloneAuthClients, func(left, right ProjectAuthClient) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectAuthClient(ctx context.Context, ref string, input ProjectAuthClientInput) (ProjectAuthClient, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	client, err := normalizeProjectAuthClient(ref, input)
	if err != nil {
		return ProjectAuthClient{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectAuthClient{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.authClients[ref] {
		if existing.ClientID == client.ClientID {
			return ProjectAuthClient{}, fmt.Errorf("%w: auth client %s for project %s already exists", ErrConflict, client.ClientID, ref)
		}
	}
	s.authClients[ref] = append(s.authClients[ref], client)
	return cloneAuthClient(client), nil
}

func (s *MemoryStore) DeleteProjectAuthClient(ctx context.Context, ref string, clientID string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return fmt.Errorf("client_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	clients := s.authClients[ref]
	for index, client := range clients {
		if client.ClientID == clientID {
			s.authClients[ref] = append(clients[:index], clients[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: auth client %s for project %s", ErrNotFound, clientID, ref)
}

func (s *MemoryStore) ListProjectAuthHooks(ctx context.Context, ref string) ([]ProjectAuthHook, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.authHooks[ref], cloneAuthHooks, func(left, right ProjectAuthHook) bool {
		return left.HookType < right.HookType
	}), nil
}

func (s *MemoryStore) CreateProjectAuthHook(ctx context.Context, ref string, input ProjectAuthHookInput) (ProjectAuthHook, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	hook, err := normalizeProjectAuthHook(ref, input)
	if err != nil {
		return ProjectAuthHook{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectAuthHook{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for index, existing := range s.authHooks[ref] {
		if existing.HookType == hook.HookType {
			hook.ID = existing.ID
			hook.CreatedAt = existing.CreatedAt
			s.authHooks[ref][index] = hook
			return cloneAuthHook(hook), nil
		}
	}
	s.authHooks[ref] = append(s.authHooks[ref], hook)
	return cloneAuthHook(hook), nil
}

func (s *MemoryStore) DeleteProjectAuthHook(ctx context.Context, ref string, hookType string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	hookType = strings.ToLower(strings.TrimSpace(hookType))
	if hookType == "" {
		return fmt.Errorf("hook_type is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	hooks := s.authHooks[ref]
	for index, hook := range hooks {
		if hook.HookType == hookType {
			s.authHooks[ref] = append(hooks[:index], hooks[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: auth hook %s for project %s", ErrNotFound, hookType, ref)
}

func (s *MemoryStore) ListProjectFunctions(ctx context.Context, ref string) ([]ProjectFunction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.functions[ref], cloneProjectFunctions, func(left, right ProjectFunction) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) DeployProjectFunction(ctx context.Context, ref string, input ProjectFunctionInput) (ProjectFunction, error) {
	name, err := normalizeFunctionName(input.Name)
	if err != nil {
		return ProjectFunction{}, err
	}
	source := strings.TrimSpace(input.Source)
	if source == "" {
		return ProjectFunction{}, fmt.Errorf("function source is required")
	}
	entrypoint := strings.TrimSpace(input.Entrypoint)
	if entrypoint == "" {
		entrypoint = "index.ts"
	}
	entrypoint, err = normalizeFunctionEntrypoint(entrypoint)
	if err != nil {
		return ProjectFunction{}, err
	}
	secrets, err := normalizeFunctionSecretValues(input.Secrets)
	if err != nil {
		return ProjectFunction{}, err
	}

	now := time.Now().UTC()
	sum := sha256.Sum256([]byte(source))
	next := ProjectFunction{
		ID:          newID(),
		ProjectRef:  ref,
		Name:        name,
		Version:     1,
		Entrypoint:  entrypoint,
		VerifyJWT:   input.VerifyJWT,
		Status:      "deployed",
		SourceHash:  hex.EncodeToString(sum[:]),
		SourceBytes: len([]byte(source)),
		Secrets:     maskFunctionSecrets(secrets),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectFunction{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	functions := s.functions[ref]
	for index, existing := range functions {
		if existing.Name == name {
			next.ID = existing.ID
			next.Version = existing.Version + 1
			next.CreatedAt = existing.CreatedAt
			functions[index] = next
			s.functions[ref] = functions
			return cloneProjectFunction(next), nil
		}
	}
	s.functions[ref] = append(functions, next)
	return cloneProjectFunction(next), nil
}

func (s *MemoryStore) DeleteProjectFunction(ctx context.Context, ref string, name string) error {
	normalized, err := normalizeFunctionName(name)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	functions := s.functions[ref]
	for index, function := range functions {
		if function.Name == normalized {
			s.functions[ref] = append(functions[:index], functions[index+1:]...)
			s.functionRegions[ref] = removeFunctionRegions(s.functionRegions[ref], normalized)
			s.functionStorageMounts[ref] = removeFunctionStorageMounts(s.functionStorageMounts[ref], normalized, "")
			return nil
		}
	}
	return fmt.Errorf("%w: function %s for project %s", ErrNotFound, normalized, ref)
}

func (s *MemoryStore) ListProjectFunctionRegions(ctx context.Context, ref string) ([]ProjectFunctionRegion, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.functionRegions[ref], cloneProjectFunctionRegions, func(left, right ProjectFunctionRegion) bool {
		if left.FunctionName != right.FunctionName {
			return left.FunctionName < right.FunctionName
		}
		return left.Region < right.Region
	}), nil
}

func (s *MemoryStore) CreateProjectFunctionRegion(ctx context.Context, ref string, input ProjectFunctionRegionInput) (ProjectFunctionRegion, error) {
	region, err := normalizeProjectFunctionRegion(ref, input)
	if err != nil {
		return ProjectFunctionRegion{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectFunctionRegion{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if !functionExists(s.functions[ref], region.FunctionName) {
		return ProjectFunctionRegion{}, fmt.Errorf("%w: function %s for project %s", ErrNotFound, region.FunctionName, ref)
	}
	if region.HostID != "" {
		if _, ok := s.hosts[region.HostID]; !ok {
			return ProjectFunctionRegion{}, fmt.Errorf("%w: host %s", ErrNotFound, region.HostID)
		}
	}
	for index, existing := range s.functionRegions[ref] {
		if existing.FunctionName == region.FunctionName && existing.Region == region.Region {
			region.ID = existing.ID
			region.CreatedAt = existing.CreatedAt
			s.functionRegions[ref][index] = region
			return cloneProjectFunctionRegion(region), nil
		}
	}
	s.functionRegions[ref] = append(s.functionRegions[ref], region)
	return cloneProjectFunctionRegion(region), nil
}

func (s *MemoryStore) DeleteProjectFunctionRegion(ctx context.Context, ref string, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("function region id is required")
	}
	ref = strings.ToLower(strings.TrimSpace(ref))

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	regions := s.functionRegions[ref]
	for index, region := range regions {
		if region.ID == id {
			s.functionRegions[ref] = append(regions[:index], regions[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: function region %s for project %s", ErrNotFound, id, ref)
}

func (s *MemoryStore) ListProjectFunctionStorageMounts(ctx context.Context, ref string) ([]ProjectFunctionStorageMount, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.functionStorageMounts[ref], cloneProjectFunctionStorageMounts, func(left, right ProjectFunctionStorageMount) bool {
		if left.FunctionName != right.FunctionName {
			return left.FunctionName < right.FunctionName
		}
		return left.MountPath < right.MountPath
	}), nil
}

func (s *MemoryStore) CreateProjectFunctionStorageMount(ctx context.Context, ref string, input ProjectFunctionStorageMountInput) (ProjectFunctionStorageMount, error) {
	mount, err := normalizeProjectFunctionStorageMount(ref, input)
	if err != nil {
		return ProjectFunctionStorageMount{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectFunctionStorageMount{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if !functionExists(s.functions[ref], mount.FunctionName) {
		return ProjectFunctionStorageMount{}, fmt.Errorf("%w: function %s for project %s", ErrNotFound, mount.FunctionName, ref)
	}
	if !storageBucketExists(s.storageBuckets[ref], mount.BucketName) {
		return ProjectFunctionStorageMount{}, fmt.Errorf("%w: storage bucket %s for project %s", ErrNotFound, mount.BucketName, ref)
	}
	for _, existing := range s.functionStorageMounts[ref] {
		if existing.FunctionName == mount.FunctionName && existing.MountPath == mount.MountPath {
			return ProjectFunctionStorageMount{}, fmt.Errorf("%w: function storage mount %s for project %s", ErrConflict, mount.MountPath, ref)
		}
	}
	s.functionStorageMounts[ref] = append(s.functionStorageMounts[ref], mount)
	return cloneProjectFunctionStorageMount(mount), nil
}

func (s *MemoryStore) DeleteProjectFunctionStorageMount(ctx context.Context, ref string, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("function storage mount id is required")
	}
	ref = strings.ToLower(strings.TrimSpace(ref))

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	mounts := s.functionStorageMounts[ref]
	for index, mount := range mounts {
		if mount.ID == id {
			s.functionStorageMounts[ref] = append(mounts[:index], mounts[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: function storage mount %s for project %s", ErrNotFound, id, ref)
}

func (s *MemoryStore) ListProjectReplicationPipelines(ctx context.Context, ref string) ([]ProjectReplicationPipeline, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.replicationPipelines[ref], cloneReplicationPipelines, func(left, right ProjectReplicationPipeline) bool {
		return left.CreatedAt.Before(right.CreatedAt)
	}), nil
}

func (s *MemoryStore) CreateProjectReplicationPipeline(ctx context.Context, ref string, input ProjectReplicationPipelineInput) (ProjectReplicationPipeline, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	pipelineType, err := normalizeReplicationPipelineType(input.Type)
	if err != nil {
		return ProjectReplicationPipeline{}, err
	}
	destination, err := normalizeReplicationDestination(input.Destination)
	if err != nil {
		return ProjectReplicationPipeline{}, err
	}
	config, err := normalizeConfigValues(input.Config)
	if err != nil {
		return ProjectReplicationPipeline{}, err
	}
	sourceSchema := strings.ToLower(strings.TrimSpace(input.SourceSchema))
	if sourceSchema == "" {
		sourceSchema = "public"
	}
	sourceTable := strings.ToLower(strings.TrimSpace(input.SourceTable))
	if sourceTable == "" {
		return ProjectReplicationPipeline{}, fmt.Errorf("source_table is required")
	}
	if !identifierPattern.MatchString(sourceSchema) || !identifierPattern.MatchString(sourceTable) {
		return ProjectReplicationPipeline{}, fmt.Errorf("source schema and table must be simple Postgres identifiers")
	}
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if name == "" {
		name = sourceTable + "-" + pipelineType
	}
	if !refPattern.MatchString(name) {
		return ProjectReplicationPipeline{}, fmt.Errorf("pipeline name must be 3-64 lowercase letters, numbers, or dashes")
	}
	destinationURI := strings.TrimSpace(input.DestinationURI)
	if err := validateReplicationDestinationConfig(destination, destinationURI, config); err != nil {
		return ProjectReplicationPipeline{}, err
	}
	if err := validateReplicationSecretHandles(config); err != nil {
		return ProjectReplicationPipeline{}, err
	}
	credentialHandle := strings.TrimSpace(input.CredentialHandle)
	if credentialHandle != "" && !strings.HasPrefix(credentialHandle, "secret://") {
		return ProjectReplicationPipeline{}, fmt.Errorf("credential_handle must be a secret:// handle")
	}

	now := time.Now().UTC()
	pipeline := ProjectReplicationPipeline{
		ID:               newID(),
		ProjectRef:       ref,
		Name:             name,
		Type:             pipelineType,
		SourceSchema:     sourceSchema,
		SourceTable:      sourceTable,
		Destination:      destination,
		DestinationURI:   destinationURI,
		CredentialHandle: credentialHandle,
		Config:           config,
		Status:           "configured",
		Message:          "declarative replication pipeline recorded",
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectReplicationPipeline{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.replicationPipelines[ref] {
		if existing.Name == name {
			return ProjectReplicationPipeline{}, fmt.Errorf("%w: replication pipeline %s for project %s already exists", ErrConflict, name, ref)
		}
	}
	s.replicationPipelines[ref] = append(s.replicationPipelines[ref], pipeline)
	return cloneReplicationPipeline(pipeline), nil
}

func (s *MemoryStore) DeleteProjectReplicationPipeline(ctx context.Context, ref string, id string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("replication pipeline id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	pipelines := s.replicationPipelines[ref]
	for index, pipeline := range pipelines {
		if pipeline.ID == id {
			s.replicationPipelines[ref] = append(pipelines[:index], pipelines[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: replication pipeline %s for project %s", ErrNotFound, id, ref)
}

func (s *MemoryStore) ListProjectEmbeddingJobs(ctx context.Context, ref string) ([]ProjectEmbeddingJob, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.embeddingJobs[ref], cloneEmbeddingJobs, func(left, right ProjectEmbeddingJob) bool {
		return left.CreatedAt.Before(right.CreatedAt)
	}), nil
}

func (s *MemoryStore) CreateProjectEmbeddingJob(ctx context.Context, ref string, input ProjectEmbeddingJobInput) (ProjectEmbeddingJob, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	job, err := normalizeProjectEmbeddingJob(ref, input)
	if err != nil {
		return ProjectEmbeddingJob{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectEmbeddingJob{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.embeddingJobs[ref] {
		if existing.Name == job.Name {
			return ProjectEmbeddingJob{}, fmt.Errorf("%w: embedding job %s for project %s already exists", ErrConflict, job.Name, ref)
		}
	}
	s.embeddingJobs[ref] = append(s.embeddingJobs[ref], job)
	return job, nil
}

func (s *MemoryStore) DeleteProjectEmbeddingJob(ctx context.Context, ref string, id string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("embedding job id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	jobs := s.embeddingJobs[ref]
	for index, job := range jobs {
		if job.ID == id {
			s.embeddingJobs[ref] = append(jobs[:index], jobs[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: embedding job %s for project %s", ErrNotFound, id, ref)
}

func (s *MemoryStore) ListProjectDatabaseExtensions(ctx context.Context, ref string) ([]ProjectDatabaseExtension, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	extensions := mergedDatabaseExtensions(ref, s.databaseExtensions[ref])
	return cloneDatabaseExtensions(extensions), nil
}

func (s *MemoryStore) UpdateProjectDatabaseExtension(ctx context.Context, ref string, name string, input ProjectDatabaseExtensionInput) (ProjectDatabaseExtension, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	extension, err := normalizeProjectDatabaseExtension(ref, name, input)
	if err != nil {
		return ProjectDatabaseExtension{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDatabaseExtension{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	extensions := s.databaseExtensions[ref]
	for index, existing := range extensions {
		if existing.Name == extension.Name {
			extension.ID = existing.ID
			extension.CreatedAt = existing.CreatedAt
			extensions[index] = extension
			s.databaseExtensions[ref] = extensions
			return cloneDatabaseExtension(extension), nil
		}
	}
	s.databaseExtensions[ref] = append(s.databaseExtensions[ref], extension)
	return cloneDatabaseExtension(extension), nil
}

func (s *MemoryStore) ListProjectDatabaseCronJobs(ctx context.Context, ref string) ([]ProjectDatabaseCronJob, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.databaseCronJobs[ref], cloneDatabaseCronJobs, func(left, right ProjectDatabaseCronJob) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectDatabaseCronJob(ctx context.Context, ref string, input ProjectDatabaseCronJobInput) (ProjectDatabaseCronJob, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	job, err := normalizeProjectDatabaseCronJob(ref, input)
	if err != nil {
		return ProjectDatabaseCronJob{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDatabaseCronJob{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.databaseCronJobs[ref] {
		if existing.Name == job.Name {
			return ProjectDatabaseCronJob{}, fmt.Errorf("%w: database cron job %s for project %s already exists", ErrConflict, job.Name, ref)
		}
	}
	s.databaseCronJobs[ref] = append(s.databaseCronJobs[ref], job)
	return cloneDatabaseCronJob(job), nil
}

func (s *MemoryStore) DeleteProjectDatabaseCronJob(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if !refPattern.MatchString(name) {
		return fmt.Errorf("database cron job name must be 3-64 lowercase letters, numbers, or dashes")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	jobs := s.databaseCronJobs[ref]
	for index, job := range jobs {
		if job.Name == name {
			s.databaseCronJobs[ref] = append(jobs[:index], jobs[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: database cron job %s for project %s", ErrNotFound, name, ref)
}

func (s *MemoryStore) ListProjectDatabaseQueues(ctx context.Context, ref string) ([]ProjectDatabaseQueue, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.databaseQueues[ref], cloneDatabaseQueues, func(left, right ProjectDatabaseQueue) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectDatabaseQueue(ctx context.Context, ref string, input ProjectDatabaseQueueInput) (ProjectDatabaseQueue, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	queue, err := normalizeProjectDatabaseQueue(ref, input)
	if err != nil {
		return ProjectDatabaseQueue{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDatabaseQueue{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.databaseQueues[ref] {
		if existing.Name == queue.Name {
			return ProjectDatabaseQueue{}, fmt.Errorf("%w: database queue %s for project %s already exists", ErrConflict, queue.Name, ref)
		}
	}
	s.databaseQueues[ref] = append(s.databaseQueues[ref], queue)
	return cloneDatabaseQueue(queue), nil
}

func (s *MemoryStore) DeleteProjectDatabaseQueue(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if !refPattern.MatchString(name) {
		return fmt.Errorf("database queue name must be 3-64 lowercase letters, numbers, or dashes")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	queues := s.databaseQueues[ref]
	for index, queue := range queues {
		if queue.Name == name {
			s.databaseQueues[ref] = append(queues[:index], queues[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: database queue %s for project %s", ErrNotFound, name, ref)
}

func (s *MemoryStore) ListProjectDatabaseWebhooks(ctx context.Context, ref string) ([]ProjectDatabaseWebhook, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.databaseWebhooks[ref], cloneDatabaseWebhooks, func(left, right ProjectDatabaseWebhook) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectDatabaseWebhook(ctx context.Context, ref string, input ProjectDatabaseWebhookInput) (ProjectDatabaseWebhook, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	webhook, err := normalizeProjectDatabaseWebhook(ref, input)
	if err != nil {
		return ProjectDatabaseWebhook{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDatabaseWebhook{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.databaseWebhooks[ref] {
		if existing.Name == webhook.Name {
			return ProjectDatabaseWebhook{}, fmt.Errorf("%w: database webhook %s for project %s already exists", ErrConflict, webhook.Name, ref)
		}
	}
	s.databaseWebhooks[ref] = append(s.databaseWebhooks[ref], webhook)
	return cloneDatabaseWebhook(webhook), nil
}

func (s *MemoryStore) DeleteProjectDatabaseWebhook(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if !refPattern.MatchString(name) {
		return fmt.Errorf("database webhook name must be 3-64 lowercase letters, numbers, or dashes")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	webhooks := s.databaseWebhooks[ref]
	for index, webhook := range webhooks {
		if webhook.Name == name {
			s.databaseWebhooks[ref] = append(webhooks[:index], webhooks[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: database webhook %s for project %s", ErrNotFound, name, ref)
}

func (s *MemoryStore) ListProjectDatabaseSchemas(ctx context.Context, ref string) ([]ProjectDatabaseSchema, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.databaseSchemas[ref], cloneDatabaseSchemas, func(left, right ProjectDatabaseSchema) bool {
		if left.ApplyOrder == right.ApplyOrder {
			if left.Name == right.Name {
				return left.Version < right.Version
			}
			return left.Name < right.Name
		}
		return left.ApplyOrder < right.ApplyOrder
	}), nil
}

func (s *MemoryStore) CreateProjectDatabaseSchema(ctx context.Context, ref string, input ProjectDatabaseSchemaInput) (ProjectDatabaseSchema, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	schema, err := normalizeProjectDatabaseSchema(ref, input)
	if err != nil {
		return ProjectDatabaseSchema{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDatabaseSchema{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.databaseSchemas[ref] {
		if existing.Name == schema.Name && existing.Version == schema.Version {
			return ProjectDatabaseSchema{}, fmt.Errorf("%w: database schema %s@%s for project %s already exists", ErrConflict, schema.Name, schema.Version, ref)
		}
	}
	s.databaseSchemas[ref] = append(s.databaseSchemas[ref], schema)
	return cloneDatabaseSchema(schema), nil
}

func (s *MemoryStore) DeleteProjectDatabaseSchema(ctx context.Context, ref string, name string, version string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	version = strings.TrimSpace(version)
	if !refPattern.MatchString(name) {
		return fmt.Errorf("database schema name must be 3-64 lowercase letters, numbers, or dashes")
	}
	if version == "" {
		return fmt.Errorf("database schema version is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	schemas := s.databaseSchemas[ref]
	for index, schema := range schemas {
		if schema.Name == name && schema.Version == version {
			s.databaseSchemas[ref] = append(schemas[:index], schemas[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: database schema %s@%s for project %s", ErrNotFound, name, version, ref)
}

func (s *MemoryStore) ListProjectDatabaseRoles(ctx context.Context, ref string) ([]ProjectDatabaseRole, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.databaseRoles[ref], cloneDatabaseRoles, func(left, right ProjectDatabaseRole) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectDatabaseRole(ctx context.Context, ref string, input ProjectDatabaseRoleInput) (ProjectDatabaseRole, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	role, err := normalizeProjectDatabaseRole(ref, input)
	if err != nil {
		return ProjectDatabaseRole{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectDatabaseRole{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.databaseRoles[ref] {
		if existing.Name == role.Name {
			return ProjectDatabaseRole{}, fmt.Errorf("%w: database role %s for project %s already exists", ErrConflict, role.Name, ref)
		}
	}
	s.databaseRoles[ref] = append(s.databaseRoles[ref], role)
	return cloneDatabaseRole(role), nil
}

func (s *MemoryStore) DeleteProjectDatabaseRole(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if err := validateDatabaseIdentifier("database role name", name); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	roles := s.databaseRoles[ref]
	for index, role := range roles {
		if role.Name == name {
			s.databaseRoles[ref] = append(roles[:index], roles[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: database role %s for project %s", ErrNotFound, name, ref)
}

func (s *MemoryStore) ListProjectStorageBuckets(ctx context.Context, ref string) ([]ProjectStorageBucket, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.storageBuckets[ref], cloneStorageBuckets, func(left, right ProjectStorageBucket) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectStorageBucket(ctx context.Context, ref string, input ProjectStorageBucketInput) (ProjectStorageBucket, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	bucket, err := normalizeProjectStorageBucket(ref, input)
	if err != nil {
		return ProjectStorageBucket{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectStorageBucket{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.storageBuckets[ref] {
		if existing.Name == bucket.Name {
			return ProjectStorageBucket{}, fmt.Errorf("%w: storage bucket %s for project %s already exists", ErrConflict, bucket.Name, ref)
		}
	}
	s.storageBuckets[ref] = append(s.storageBuckets[ref], bucket)
	return cloneStorageBucket(bucket), nil
}

func (s *MemoryStore) DeleteProjectStorageBucket(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if !refPattern.MatchString(name) {
		return fmt.Errorf("storage bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	buckets := s.storageBuckets[ref]
	for index, bucket := range buckets {
		if bucket.Name == name {
			s.storageBuckets[ref] = append(buckets[:index], buckets[index+1:]...)
			s.functionStorageMounts[ref] = removeFunctionStorageMounts(s.functionStorageMounts[ref], "", name)
			return nil
		}
	}
	return fmt.Errorf("%w: storage bucket %s for project %s", ErrNotFound, name, ref)
}

func (s *MemoryStore) ListProjectVectorBuckets(ctx context.Context, ref string) ([]ProjectVectorBucket, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.vectorBuckets[ref], cloneVectorBuckets, func(left, right ProjectVectorBucket) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectVectorBucket(ctx context.Context, ref string, input ProjectVectorBucketInput) (ProjectVectorBucket, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	bucket, err := normalizeProjectVectorBucket(ref, input)
	if err != nil {
		return ProjectVectorBucket{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectVectorBucket{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.vectorBuckets[ref] {
		if existing.Name == bucket.Name {
			return ProjectVectorBucket{}, fmt.Errorf("%w: vector bucket %s for project %s already exists", ErrConflict, bucket.Name, ref)
		}
	}
	s.vectorBuckets[ref] = append(s.vectorBuckets[ref], bucket)
	return cloneVectorBucket(bucket), nil
}

func (s *MemoryStore) DeleteProjectVectorBucket(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if !refPattern.MatchString(name) {
		return fmt.Errorf("vector bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	buckets := s.vectorBuckets[ref]
	for index, bucket := range buckets {
		if bucket.Name == name {
			s.vectorBuckets[ref] = append(buckets[:index], buckets[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: vector bucket %s for project %s", ErrNotFound, name, ref)
}

func (s *MemoryStore) ListProjectAnalyticsBuckets(ctx context.Context, ref string) ([]ProjectAnalyticsBucket, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.analyticsBuckets[ref], cloneAnalyticsBuckets, func(left, right ProjectAnalyticsBucket) bool {
		return left.Name < right.Name
	}), nil
}

func (s *MemoryStore) CreateProjectAnalyticsBucket(ctx context.Context, ref string, input ProjectAnalyticsBucketInput) (ProjectAnalyticsBucket, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	bucket, err := normalizeProjectAnalyticsBucket(ref, input)
	if err != nil {
		return ProjectAnalyticsBucket{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectAnalyticsBucket{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.analyticsBuckets[ref] {
		if existing.Name == bucket.Name {
			return ProjectAnalyticsBucket{}, fmt.Errorf("%w: analytics bucket %s for project %s already exists", ErrConflict, bucket.Name, ref)
		}
	}
	s.analyticsBuckets[ref] = append(s.analyticsBuckets[ref], bucket)
	return cloneAnalyticsBucket(bucket), nil
}

func (s *MemoryStore) DeleteProjectAnalyticsBucket(ctx context.Context, ref string, name string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	name = strings.ToLower(strings.TrimSpace(name))
	if !refPattern.MatchString(name) {
		return fmt.Errorf("analytics bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	buckets := s.analyticsBuckets[ref]
	for index, bucket := range buckets {
		if bucket.Name == name {
			s.analyticsBuckets[ref] = append(buckets[:index], buckets[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: analytics bucket %s for project %s", ErrNotFound, name, ref)
}

func (s *MemoryStore) GetProjectCDNPolicy(ctx context.Context, ref string) (ProjectCDNPolicy, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectCDNPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if policy, ok := s.cdnPolicies[ref]; ok {
		return cloneProjectCDNPolicy(policy), nil
	}
	return defaultProjectCDNPolicy(ref), nil
}

func (s *MemoryStore) UpdateProjectCDNPolicy(ctx context.Context, ref string, input ProjectCDNPolicyInput) (ProjectCDNPolicy, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	policy, err := normalizeProjectCDNPolicy(ref, input)
	if err != nil {
		return ProjectCDNPolicy{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectCDNPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	s.cdnPolicies[ref] = cloneProjectCDNPolicy(policy)
	return cloneProjectCDNPolicy(policy), nil
}

func (s *MemoryStore) ListProjectCDNInvalidations(ctx context.Context, ref string) ([]CDNInvalidation, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.cdnInvalidations[ref], cloneCDNInvalidations, func(left, right CDNInvalidation) bool {
		return left.CreatedAt.After(right.CreatedAt)
	}), nil
}

func (s *MemoryStore) CreateProjectCDNInvalidation(ctx context.Context, ref string, input CDNInvalidationInput) (CDNInvalidation, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	paths, err := normalizeCDNPaths(input.Paths, false)
	if err != nil {
		return CDNInvalidation{}, err
	}
	return s.createProjectCDNInvalidationLocked(ref, paths, "manual", "", "edge cache invalidation recorded")
}

func (s *MemoryStore) CreateProjectCDNObjectEvent(ctx context.Context, ref string, input CDNObjectEventInput) (CDNInvalidation, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	event, err := normalizeCDNObjectEvent(input)
	if err != nil {
		return CDNInvalidation{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return CDNInvalidation{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	policy, ok := s.cdnPolicies[ref]
	if !ok {
		policy = defaultProjectCDNPolicy(ref)
	}
	if !policy.Enabled || !policy.SmartRevalidation {
		return CDNInvalidation{}, fmt.Errorf("%w: smart cdn revalidation is disabled for project %s", ErrNotFound, ref)
	}
	if event.Bucket != "" && !storageBucketExists(s.storageBuckets[ref], event.Bucket) {
		return CDNInvalidation{}, fmt.Errorf("%w: storage bucket %s for project %s", ErrNotFound, event.Bucket, ref)
	}
	path := storageObjectCDNPath(event.Bucket, event.ObjectPath)
	if !cdnPathIncluded(path, policy.IncludedPaths, policy.ExcludedPaths) {
		return CDNInvalidation{}, fmt.Errorf("object path %s is outside cdn policy scope", path)
	}
	return s.createProjectCDNInvalidationAlreadyLocked(ref, []string{path}, "storage_object_event", event.EventID, "smart cdn revalidation recorded for "+event.EventType)
}

func (s *MemoryStore) createProjectCDNInvalidationLocked(ref string, paths []string, source string, eventID string, message string) (CDNInvalidation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return CDNInvalidation{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return s.createProjectCDNInvalidationAlreadyLocked(ref, paths, source, eventID, message)
}

func (s *MemoryStore) createProjectCDNInvalidationAlreadyLocked(ref string, paths []string, source string, eventID string, message string) (CDNInvalidation, error) {
	now := time.Now().UTC()
	invalidation := CDNInvalidation{
		ID:          newID(),
		ProjectRef:  ref,
		Paths:       paths,
		Source:      source,
		EventID:     eventID,
		Status:      "completed",
		Message:     message,
		CreatedAt:   now,
		CompletedAt: &now,
	}
	s.cdnInvalidations[ref] = append(s.cdnInvalidations[ref], invalidation)
	return cloneCDNInvalidation(invalidation), nil
}

func (s *MemoryStore) ListProjectNetworkConnections(ctx context.Context, ref string) ([]ProjectNetworkConnection, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.networkConnections[ref], cloneNetworkConnections, func(left, right ProjectNetworkConnection) bool {
		return left.CreatedAt.Before(right.CreatedAt)
	}), nil
}

func (s *MemoryStore) CreateProjectNetworkConnection(ctx context.Context, ref string, input ProjectNetworkConnectionInput) (ProjectNetworkConnection, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	connection, err := normalizeProjectNetworkConnection(ref, input)
	if err != nil {
		return ProjectNetworkConnection{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectNetworkConnection{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, existing := range s.networkConnections[ref] {
		if existing.Name == connection.Name {
			return ProjectNetworkConnection{}, fmt.Errorf("%w: network connection %s for project %s already exists", ErrConflict, connection.Name, ref)
		}
	}
	s.networkConnections[ref] = append(s.networkConnections[ref], connection)
	return cloneNetworkConnection(connection), nil
}

func (s *MemoryStore) DeleteProjectNetworkConnection(ctx context.Context, ref string, id string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("network connection id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	connections := s.networkConnections[ref]
	for index, connection := range connections {
		if connection.ID == id {
			s.networkConnections[ref] = append(connections[:index], connections[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: network connection %s for project %s", ErrNotFound, id, ref)
}

func (s *MemoryStore) ListProjectLogDrains(ctx context.Context, ref string) ([]LogDrain, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return cloneAndSortProjectChildList(s.logDrains[ref], cloneLogDrains, func(left, right LogDrain) bool {
		return left.CreatedAt.Before(right.CreatedAt)
	}), nil
}

func (s *MemoryStore) CreateProjectLogDrain(ctx context.Context, ref string, input LogDrainInput) (LogDrain, error) {
	target, err := normalizeLogDrainTarget(input.Target)
	if err != nil {
		return LogDrain{}, err
	}
	config, err := normalizeConfigValues(input.Config)
	if err != nil {
		return LogDrain{}, err
	}
	if err := validateLogDrainConfig(target, config); err != nil {
		return LogDrain{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return LogDrain{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	drain := LogDrain{
		ID:         newID(),
		ProjectRef: ref,
		Target:     target,
		Config:     config,
		CreatedAt:  time.Now().UTC(),
	}
	s.logDrains[ref] = append(s.logDrains[ref], drain)
	return cloneLogDrain(drain), nil
}

func (s *MemoryStore) UpdateProjectLogDrain(ctx context.Context, ref string, id string, input LogDrainInput) (LogDrain, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return LogDrain{}, fmt.Errorf("log drain id is required")
	}
	target, err := normalizeLogDrainTarget(input.Target)
	if err != nil {
		return LogDrain{}, err
	}
	config, err := normalizeConfigValues(input.Config)
	if err != nil {
		return LogDrain{}, err
	}
	if err := validateLogDrainConfig(target, config); err != nil {
		return LogDrain{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return LogDrain{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	drains := s.logDrains[ref]
	for index, drain := range drains {
		if drain.ID == id {
			drain.Target = target
			drain.Config = config
			drains[index] = drain
			s.logDrains[ref] = drains
			return cloneLogDrain(drain), nil
		}
	}
	return LogDrain{}, fmt.Errorf("%w: log drain %s for project %s", ErrNotFound, id, ref)
}

func (s *MemoryStore) DeleteProjectLogDrain(ctx context.Context, ref string, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("log drain id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	drains := s.logDrains[ref]
	for index, drain := range drains {
		if drain.ID == id {
			s.logDrains[ref] = append(drains[:index], drains[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: log drain %s for project %s", ErrNotFound, id, ref)
}

func (s *MemoryStore) EnsureProjectSecrets(ctx context.Context, ref string) ([]ProjectSecret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if len(s.secrets[ref]) == 0 {
		s.secrets[ref] = generateProjectSecrets(ref)
	} else {
		ensureProjectSigningKeys(ref, s.secrets[ref])
		ensureSupabaseAPIKeys(ref, s.secrets[ref], time.Now().UTC())
	}
	return secretsToSlice(s.secrets[ref]), nil
}

func (s *MemoryStore) ListProjectSecrets(ctx context.Context, ref string) ([]ProjectSecret, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	return secretsToSlice(s.secrets[ref]), nil
}

func (s *MemoryStore) RevealProjectSecret(ctx context.Context, ref string, kind string) (ProjectSecret, error) {
	normalizedKind, err := normalizeProjectSecretKind(kind)
	if err != nil {
		return ProjectSecret{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectSecret{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	secret, ok := s.secrets[ref][normalizedKind]
	if !ok {
		return ProjectSecret{}, fmt.Errorf("%w: secret %s for project %s", ErrNotFound, normalizedKind, ref)
	}
	return secret, nil
}

func (s *MemoryStore) UpsertProjectSecret(ctx context.Context, ref string, kind string, input ProjectSecretInput) (ProjectSecret, error) {
	normalizedKind, err := normalizeCustomProjectSecretKind(kind)
	if err != nil {
		return ProjectSecret{}, err
	}
	value := strings.TrimSpace(input.Value)
	if value == "" {
		return ProjectSecret{}, fmt.Errorf("secret value is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectSecret{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if s.secrets[ref] == nil {
		s.secrets[ref] = map[string]ProjectSecret{}
	}
	now := time.Now().UTC()
	secret, ok := s.secrets[ref][normalizedKind]
	if !ok {
		secret = ProjectSecret{
			ID:         newID(),
			ProjectRef: ref,
			Kind:       normalizedKind,
			CreatedAt:  now,
		}
	} else {
		secret.RotatedAt = &now
	}
	secret.Value = value
	secret.Masked = maskSecret(value)
	s.secrets[ref][normalizedKind] = secret
	return secret, nil
}

func (s *MemoryStore) DeleteProjectSecret(ctx context.Context, ref string, kind string) error {
	normalizedKind, err := normalizeCustomProjectSecretKind(kind)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if _, ok := s.secrets[ref][normalizedKind]; !ok {
		return fmt.Errorf("%w: secret %s for project %s", ErrNotFound, normalizedKind, ref)
	}
	delete(s.secrets[ref], normalizedKind)
	return nil
}

func (s *MemoryStore) RotateProjectSecret(ctx context.Context, ref string, kind string) (ProjectSecret, error) {
	normalizedKind, err := normalizeManagedProjectSecretKind(kind)
	if err != nil {
		return ProjectSecret{}, err
	}
	if strings.HasPrefix(normalizedKind, "jwt_signing_key_previous_") {
		return ProjectSecret{}, fmt.Errorf("archived JWT signing keys cannot be rotated")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return ProjectSecret{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	secret, ok := s.secrets[ref][normalizedKind]
	if !ok {
		return ProjectSecret{}, fmt.Errorf("%w: secret %s for project %s", ErrNotFound, normalizedKind, ref)
	}
	if normalizedKind == "jwt_signing_key_current" {
		return s.rotateCurrentJWTSigningKeyLocked(ref, secret), nil
	}
	now := time.Now().UTC()
	value := randomSecretValue(ref, normalizedKind)
	switch normalizedKind {
	case "anon_key":
		ensureSupabaseAPIKeys(ref, s.secrets[ref], now)
		value = supabaseRoleJWT(ref, "anon", s.secrets[ref]["jwt_secret"].Value)
	case "service_role":
		ensureSupabaseAPIKeys(ref, s.secrets[ref], now)
		value = supabaseRoleJWT(ref, "service_role", s.secrets[ref]["jwt_secret"].Value)
	}
	secret.Value = value
	secret.Masked = maskSecret(value)
	secret.RotatedAt = &now
	s.secrets[ref][normalizedKind] = secret
	if normalizedKind == "jwt_secret" {
		ensureSupabaseAPIKeys(ref, s.secrets[ref], now)
	}
	return secret, nil
}

func (s *MemoryStore) rotateCurrentJWTSigningKeyLocked(ref string, current ProjectSecret) ProjectSecret {
	now := time.Now().UTC()
	archiveKind := fmt.Sprintf("jwt_signing_key_previous_%s", now.Format("20060102t150405z"))
	current.Kind = archiveKind
	current.Value = updateJWTSigningKeyMaterialStatus(current.Value, "previous")
	current.Masked = maskSecret(current.Value)
	current.RotatedAt = &now
	s.secrets[ref][archiveKind] = current

	next, ok := s.secrets[ref]["jwt_signing_key_next"]
	if !ok {
		next = newProjectSecret(ref, "jwt_signing_key_next", now)
	}
	next.Kind = "jwt_signing_key_current"
	next.Value = updateJWTSigningKeyMaterialStatus(next.Value, "current")
	next.Masked = maskSecret(next.Value)
	next.RotatedAt = &now
	s.secrets[ref]["jwt_signing_key_current"] = next
	s.secrets[ref]["jwt_signing_key_next"] = newProjectSecret(ref, "jwt_signing_key_next", now)
	return next
}

func (s *MemoryStore) CreateBackup(ctx context.Context, input BackupInput) (Backup, error) {
	if strings.TrimSpace(input.ProjectRef) == "" {
		return Backup{}, fmt.Errorf("project ref is required")
	}
	if strings.TrimSpace(input.Location) == "" {
		return Backup{}, fmt.Errorf("backup location is required")
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if kind == "" {
		kind = "logical"
	}
	status := input.Status
	if status == "" {
		status = "completed"
	}
	now := time.Now().UTC()
	startedAt := input.StartedAt
	if startedAt.IsZero() {
		startedAt = now
	}
	finishedAt := input.FinishedAt
	if finishedAt == nil && input.VerifiedAt != nil {
		verifiedAt := *input.VerifiedAt
		finishedAt = &verifiedAt
	}
	if finishedAt == nil && status == "completed" {
		completedAt := now
		finishedAt = &completedAt
	}
	backup := Backup{
		ID:              newID(),
		ProjectRef:      input.ProjectRef,
		Kind:            kind,
		Location:        input.Location,
		RemoteLocation:  strings.TrimSpace(input.RemoteLocation),
		StorageTargetID: strings.TrimSpace(input.StorageTargetID),
		SizeBytes:       input.SizeBytes,
		ChecksumSHA256:  strings.TrimSpace(input.ChecksumSHA256),
		Status:          status,
		StartedAt:       startedAt,
		FinishedAt:      finishedAt,
		CreatedAt:       now,
		VerifiedAt:      input.VerifiedAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[input.ProjectRef]; !ok {
		return Backup{}, fmt.Errorf("%w: project %s", ErrNotFound, input.ProjectRef)
	}
	if backup.StorageTargetID != "" {
		if _, ok := s.backupStorageTargets[backup.StorageTargetID]; !ok {
			return Backup{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, backup.StorageTargetID)
		}
	}
	s.backups = append(s.backups, backup)
	return backup, nil
}

func (s *MemoryStore) ListBackups(ctx context.Context, ref string) ([]Backup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	backups := make([]Backup, 0)
	for _, backup := range s.backups {
		if backup.ProjectRef == ref {
			backups = append(backups, backup)
		}
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})
	return backups, nil
}

func (s *MemoryStore) GetBackup(ctx context.Context, ref string, backupID string) (Backup, error) {
	backupID = strings.TrimSpace(backupID)
	if backupID == "" {
		return Backup{}, fmt.Errorf("backup id is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return Backup{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	for _, backup := range s.backups {
		if backup.ProjectRef == ref && backup.ID == backupID {
			return backup, nil
		}
	}
	return Backup{}, fmt.Errorf("%w: backup %s for project %s", ErrNotFound, backupID, ref)
}

func (s *MemoryStore) CreatePlatformBackup(ctx context.Context, input PlatformBackupInput) (PlatformBackup, error) {
	if strings.TrimSpace(input.Location) == "" {
		return PlatformBackup{}, fmt.Errorf("platform backup location is required")
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = "control-plane"
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "completed"
	}
	now := time.Now().UTC()
	startedAt := input.StartedAt
	if startedAt.IsZero() {
		startedAt = now
	}
	finishedAt := input.FinishedAt
	if finishedAt == nil && input.VerifiedAt != nil {
		verifiedAt := *input.VerifiedAt
		finishedAt = &verifiedAt
	}
	if finishedAt == nil && status == "completed" {
		completedAt := now
		finishedAt = &completedAt
	}
	backup := PlatformBackup{
		ID:              newID(),
		Kind:            kind,
		Location:        strings.TrimSpace(input.Location),
		RemoteLocation:  strings.TrimSpace(input.RemoteLocation),
		StorageTargetID: strings.TrimSpace(input.StorageTargetID),
		SizeBytes:       input.SizeBytes,
		ChecksumSHA256:  strings.TrimSpace(input.ChecksumSHA256),
		Status:          status,
		StartedAt:       startedAt,
		FinishedAt:      finishedAt,
		CreatedAt:       now,
		VerifiedAt:      input.VerifiedAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if backup.StorageTargetID != "" {
		if _, ok := s.backupStorageTargets[backup.StorageTargetID]; !ok {
			return PlatformBackup{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, backup.StorageTargetID)
		}
	}
	s.platformBackups = append(s.platformBackups, backup)
	return backup, nil
}

func (s *MemoryStore) ListPlatformBackups(ctx context.Context) ([]PlatformBackup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	backups := append([]PlatformBackup(nil), s.platformBackups...)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})
	return backups, nil
}

func (s *MemoryStore) GetPlatformBackup(ctx context.Context, backupID string) (PlatformBackup, error) {
	backupID = strings.TrimSpace(backupID)
	if backupID == "" {
		return PlatformBackup{}, fmt.Errorf("platform backup id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, backup := range s.platformBackups {
		if backup.ID == backupID {
			return backup, nil
		}
	}
	return PlatformBackup{}, fmt.Errorf("%w: platform backup %s", ErrNotFound, backupID)
}

func (s *MemoryStore) ListBackupStorageTargets(ctx context.Context) ([]BackupStorageTarget, error) {
	s.mu.RLock()
	rawTargets := make([]BackupStorageTarget, 0, len(s.backupStorageTargets))
	for _, target := range s.backupStorageTargets {
		rawTargets = append(rawTargets, target)
	}
	s.mu.RUnlock()

	targets := make([]BackupStorageTarget, 0, len(rawTargets))
	for _, target := range rawTargets {
		targets = append(targets, redactBackupStorageTarget(target))
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Default != targets[j].Default {
			return targets[i].Default
		}
		if targets[i].Name != targets[j].Name {
			return targets[i].Name < targets[j].Name
		}
		return targets[i].ID < targets[j].ID
	})
	return targets, nil
}

func (s *MemoryStore) GetBackupStorageTarget(ctx context.Context, id string) (BackupStorageTarget, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	target, ok := s.backupStorageTargets[id]
	if !ok {
		return BackupStorageTarget{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, id)
	}
	return target, nil
}

func (s *MemoryStore) CreateBackupStorageTarget(ctx context.Context, input BackupStorageTargetInput) (BackupStorageTarget, error) {
	target, err := normalizeBackupStorageTargetInput("", BackupStorageTarget{}, input, true)
	if err != nil {
		return BackupStorageTarget{}, err
	}
	now := time.Now().UTC()
	target.ID = newID()
	target.CreatedAt = now
	target.UpdatedAt = now

	s.mu.Lock()
	if target.Default {
		s.clearDefaultBackupStorageTargetLocked("")
	}
	s.backupStorageTargets[target.ID] = target
	s.mu.Unlock()
	return redactBackupStorageTarget(target), nil
}

func (s *MemoryStore) UpdateBackupStorageTarget(ctx context.Context, id string, input BackupStorageTargetInput) (BackupStorageTarget, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target id is required")
	}
	s.mu.Lock()
	existing, ok := s.backupStorageTargets[id]
	if !ok {
		s.mu.Unlock()
		return BackupStorageTarget{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, id)
	}
	target, err := normalizeBackupStorageTargetInput(id, existing, input, false)
	if err != nil {
		s.mu.Unlock()
		return BackupStorageTarget{}, err
	}
	if target.Default {
		s.clearDefaultBackupStorageTargetLocked(id)
	}
	s.backupStorageTargets[id] = target
	s.mu.Unlock()
	return redactBackupStorageTarget(target), nil
}

func (s *MemoryStore) UpdateBackupStorageTargetTestResult(ctx context.Context, id string, testedAt time.Time, status string, message string) (BackupStorageTarget, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target id is required")
	}
	status = strings.TrimSpace(status)
	if status != "passed" && status != "failed" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target test status is invalid")
	}
	message = strings.TrimSpace(message)
	if len(message) > 500 {
		message = message[:500]
	}
	if testedAt.IsZero() {
		testedAt = time.Now().UTC()
	}
	s.mu.Lock()
	target, ok := s.backupStorageTargets[id]
	if !ok {
		s.mu.Unlock()
		return BackupStorageTarget{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, id)
	}
	target.LastTestedAt = &testedAt
	target.LastTestStatus = status
	target.LastTestError = message
	target.UpdatedAt = time.Now().UTC()
	s.backupStorageTargets[id] = target
	s.mu.Unlock()
	return redactBackupStorageTarget(target), nil
}

func (s *MemoryStore) DeleteBackupStorageTarget(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("backup storage target id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.backupStorageTargets[id]; !ok {
		return fmt.Errorf("%w: backup storage target %s", ErrNotFound, id)
	}
	for ref, policy := range s.policies {
		if policy.StorageTargetID == id {
			policy.StorageTargetID = ""
			policy.UpdatedAt = time.Now().UTC()
			s.policies[ref] = policy
		}
	}
	delete(s.backupStorageTargets, id)
	return nil
}

func (s *MemoryStore) clearDefaultBackupStorageTargetLocked(exceptID string) {
	for id, target := range s.backupStorageTargets {
		if id == exceptID || !target.Default {
			continue
		}
		target.Default = false
		target.UpdatedAt = time.Now().UTC()
		s.backupStorageTargets[id] = target
	}
}

func (s *MemoryStore) GetBackupPolicy(ctx context.Context, ref string) (BackupPolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return BackupPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	policy, ok := s.policies[ref]
	if !ok {
		policy = defaultBackupPolicy(ref)
	}
	return policy, nil
}

func (s *MemoryStore) UpdateBackupPolicy(ctx context.Context, ref string, input BackupPolicyInput) (BackupPolicy, error) {
	schedule := strings.TrimSpace(input.Schedule)
	if schedule == "" {
		schedule = "daily"
	}
	if err := validateBackupSchedule(schedule); err != nil {
		return BackupPolicy{}, err
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if kind == "" {
		kind = "logical"
	}
	if kind != "logical" && kind != "physical" {
		return BackupPolicy{}, fmt.Errorf("unsupported backup kind %q", kind)
	}
	targetID := strings.TrimSpace(input.StorageTargetID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return BackupPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if targetID != "" {
		if _, ok := s.backupStorageTargets[targetID]; !ok {
			return BackupPolicy{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, targetID)
		}
	}
	now := time.Now().UTC()
	policy := s.policies[ref]
	policy.ProjectRef = ref
	policy.Enabled = input.Enabled
	policy.Schedule = schedule
	policy.Kind = kind
	policy.StorageTargetID = targetID
	policy.UpdatedAt = now
	if input.Enabled {
		next := nextBackupRun(now, schedule)
		policy.NextRunAt = &next
	} else {
		policy.NextRunAt = nil
	}
	s.policies[ref] = policy
	return policy, nil
}

func (s *MemoryStore) MarkBackupPolicyRun(ctx context.Context, ref string, runAt time.Time) (BackupPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return BackupPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	policy := s.policies[ref]
	if policy.ProjectRef == "" {
		policy = defaultBackupPolicy(ref)
	}
	runAt = runAt.UTC()
	policy.LastRunAt = &runAt
	next := nextBackupRun(runAt, policy.Schedule)
	policy.NextRunAt = &next
	policy.UpdatedAt = time.Now().UTC()
	s.policies[ref] = policy
	return policy, nil
}

func (s *MemoryStore) GetPITRPolicy(ctx context.Context, ref string) (PITRPolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return PITRPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	policy, ok := s.pitrPolicies[ref]
	if !ok {
		policy = defaultPITRPolicy(ref)
	}
	return policy, nil
}

func (s *MemoryStore) UpdatePITRPolicy(ctx context.Context, ref string, input PITRPolicyInput) (PITRPolicy, error) {
	bucket := strings.TrimSpace(input.ArchiveBucket)
	retentionDays := input.RetentionDays
	if retentionDays == 0 {
		retentionDays = 7
	}
	if retentionDays < 1 || retentionDays > 35 {
		return PITRPolicy{}, fmt.Errorf("retention_days must be between 1 and 35")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return PITRPolicy{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	if input.Enabled && bucket == "" {
		var ok bool
		bucket, ok = s.defaultWALArchiveBucketLocked(ref)
		if !ok {
			return PITRPolicy{}, fmt.Errorf("archive_bucket is required when PITR is enabled and no backup storage target is selected or configured as platform default")
		}
	}
	now := time.Now().UTC()
	policy := s.pitrPolicies[ref]
	policy.ProjectRef = ref
	policy.Enabled = input.Enabled
	policy.ArchiveBucket = bucket
	policy.RetentionDays = retentionDays
	policy.UpdatedAt = now
	s.pitrPolicies[ref] = policy
	return policy, nil
}

func (s *MemoryStore) defaultWALArchiveBucketLocked(ref string) (string, bool) {
	targetID := strings.TrimSpace(s.policies[ref].StorageTargetID)
	for _, target := range s.backupStorageTargets {
		if targetID != "" {
			if target.ID != targetID {
				continue
			}
		} else if !target.Default {
			continue
		}
		if !target.SecretConfigured {
			continue
		}
		if requireRecoveryReadyBackupTargets() && !backupStorageTargetIsReadyOffHost(target) {
			continue
		}
		return walArchiveBucketForTarget(target, ref), true
	}
	return "", false
}

func (s *MemoryStore) CreateWALArchive(ctx context.Context, input WALArchiveInput) (WALArchive, error) {
	if strings.TrimSpace(input.ProjectRef) == "" {
		return WALArchive{}, fmt.Errorf("project ref is required")
	}
	if strings.TrimSpace(input.Segment) == "" {
		return WALArchive{}, fmt.Errorf("wal segment is required")
	}
	if !isPostgresWALSegment(input.Segment) {
		return WALArchive{}, fmt.Errorf("wal segment must be a 24-character Postgres WAL filename")
	}
	if strings.TrimSpace(input.Location) == "" {
		return WALArchive{}, fmt.Errorf("wal archive location is required")
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "archived"
	}
	segmentSource := strings.TrimSpace(input.SegmentSource)
	if segmentSource == "" {
		segmentSource = "unknown"
	}
	archive := WALArchive{
		ID:              newID(),
		ProjectRef:      input.ProjectRef,
		Segment:         strings.ToUpper(strings.TrimSpace(input.Segment)),
		SegmentSource:   segmentSource,
		Location:        input.Location,
		RemoteLocation:  strings.TrimSpace(input.RemoteLocation),
		StorageTargetID: strings.TrimSpace(input.StorageTargetID),
		SizeBytes:       input.SizeBytes,
		ChecksumSHA256:  strings.TrimSpace(input.ChecksumSHA256),
		Status:          status,
		CreatedAt:       time.Now().UTC(),
		VerifiedAt:      input.VerifiedAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[input.ProjectRef]; !ok {
		return WALArchive{}, fmt.Errorf("%w: project %s", ErrNotFound, input.ProjectRef)
	}
	if archive.StorageTargetID != "" {
		if _, ok := s.backupStorageTargets[archive.StorageTargetID]; !ok {
			return WALArchive{}, fmt.Errorf("%w: backup storage target %s", ErrNotFound, archive.StorageTargetID)
		}
	}
	s.walArchives = append(s.walArchives, archive)
	policy := s.pitrPolicies[input.ProjectRef]
	if policy.ProjectRef == "" {
		policy = defaultPITRPolicy(input.ProjectRef)
	}
	archivedAt := archive.CreatedAt
	policy.LastArchiveAt = &archivedAt
	policy.UpdatedAt = time.Now().UTC()
	s.pitrPolicies[input.ProjectRef] = policy
	return archive, nil
}

func (s *MemoryStore) ListWALArchives(ctx context.Context, ref string) ([]WALArchive, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	archives := make([]WALArchive, 0)
	for _, archive := range s.walArchives {
		if archive.ProjectRef == ref {
			archives = append(archives, archive)
		}
	}
	sort.Slice(archives, func(i, j int) bool {
		return archives[i].CreatedAt.After(archives[j].CreatedAt)
	})
	return archives, nil
}

func (s *MemoryStore) RecordProjectLog(ctx context.Context, input ProjectLogInput) (ProjectLog, error) {
	if strings.TrimSpace(input.ProjectRef) == "" {
		return ProjectLog{}, fmt.Errorf("project ref is required")
	}
	level := input.Level
	if level == "" {
		level = "info"
	}
	logEntry := ProjectLog{
		ID:         newID(),
		ProjectRef: input.ProjectRef,
		Level:      level,
		Message:    input.Message,
		Metadata:   input.Metadata,
		CreatedAt:  time.Now().UTC(),
	}
	if logEntry.Metadata == nil {
		logEntry.Metadata = map[string]string{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[input.ProjectRef]; !ok {
		return ProjectLog{}, fmt.Errorf("%w: project %s", ErrNotFound, input.ProjectRef)
	}
	s.projectLogs = append(s.projectLogs, logEntry)
	return logEntry, nil
}

func (s *MemoryStore) ListProjectLogs(ctx context.Context, ref string, limit int) ([]ProjectLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	matched := make([]ProjectLog, 0)
	for _, logEntry := range s.projectLogs {
		if logEntry.ProjectRef == ref {
			matched = append(matched, logEntry)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (s *MemoryStore) RecordAuditEvent(ctx context.Context, input AuditEventInput) (AuditEvent, error) {
	if strings.TrimSpace(input.Action) == "" {
		return AuditEvent{}, fmt.Errorf("audit action is required")
	}
	if strings.TrimSpace(input.Target) == "" {
		return AuditEvent{}, fmt.Errorf("audit target is required")
	}
	event := AuditEvent{
		ActorID:   strings.TrimSpace(input.ActorID),
		Action:    strings.TrimSpace(input.Action),
		Target:    strings.TrimSpace(input.Target),
		Metadata:  cloneStringMap(input.Metadata),
		CreatedAt: time.Now().UTC(),
	}
	if event.Metadata == nil {
		event.Metadata = map[string]string{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	event.ID = newID()
	event.ChainIndex = len(s.auditEvents) + 1
	if len(s.auditEvents) > 0 {
		event.PreviousHash = s.auditEvents[len(s.auditEvents)-1].Hash
	}
	event.Hash = hashAuditEvent(event)
	s.auditEvents = append(s.auditEvents, event)
	return event, nil
}

func (s *MemoryStore) ListAuditEvents(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	start := len(s.auditEvents) - limit
	if start < 0 {
		start = 0
	}
	events := append([]AuditEvent(nil), s.auditEvents[start:]...)
	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt.After(events[j].CreatedAt)
	})
	return events, nil
}

// ListAuditEventsPage applies server-side filtering (action / actor / time
// window) and pagination over the full audit history, returning the matching
// slice plus the total match count. The full chain lives in memory, so this is
// an in-memory scan — no per-query SQL.
func (s *MemoryStore) ListAuditEventsPage(ctx context.Context, query AuditEventQuery) (AuditEventPage, error) {
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	action := strings.TrimSpace(query.Action)
	actor := strings.TrimSpace(query.ActorID)

	s.mu.RLock()
	defer s.mu.RUnlock()
	filtered := make([]AuditEvent, 0, len(s.auditEvents))
	for _, event := range s.auditEvents {
		if action != "" && event.Action != action {
			continue
		}
		if actor != "" && event.ActorID != actor {
			continue
		}
		if !query.Since.IsZero() && event.CreatedAt.Before(query.Since) {
			continue
		}
		if !query.Until.IsZero() && event.CreatedAt.After(query.Until) {
			continue
		}
		filtered = append(filtered, event)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	total := len(filtered)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return AuditEventPage{
		Events: append([]AuditEvent(nil), filtered[offset:end]...),
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (s *MemoryStore) ListProjectAuditEvents(ctx context.Context, ref string, limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	ref = strings.TrimSpace(ref)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.projects[ref]; !ok {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	target := "project:" + ref
	matched := make([]AuditEvent, 0)
	for _, event := range s.auditEvents {
		if event.Target == target || event.Metadata["project_ref"] == ref || event.Metadata["ref"] == ref {
			matched = append(matched, event)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (s *MemoryStore) VerifyAuditLog(ctx context.Context) (AuditIntegrity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	integrity := AuditIntegrity{
		Verified:  true,
		Events:    len(s.auditEvents),
		CheckedAt: time.Now().UTC(),
	}
	previousHash := ""
	for index, event := range s.auditEvents {
		expectedIndex := index + 1
		if event.ChainIndex != expectedIndex || event.PreviousHash != previousHash || event.Hash != hashAuditEvent(event) {
			integrity.Verified = false
			integrity.BrokenAt = expectedIndex
			return integrity, nil
		}
		previousHash = event.Hash
	}
	integrity.HeadHash = previousHash
	return integrity, nil
}

func (s *MemoryStore) RecordProjectTelemetry(ctx context.Context, ref string, input TelemetrySampleInput) (TelemetrySample, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "unknown"
	}
	if input.CPUPercent < 0 {
		return TelemetrySample{}, fmt.Errorf("cpu percent cannot be negative")
	}
	if input.MemoryBytes < 0 || input.MemoryLimitBytes < 0 || input.DiskUsedBytes < 0 || input.DiskLimitBytes < 0 || input.NetworkRxBytes < 0 || input.NetworkTxBytes < 0 {
		return TelemetrySample{}, fmt.Errorf("telemetry counters cannot be negative")
	}
	sampledAt := input.SampledAt.UTC()
	if sampledAt.IsZero() {
		sampledAt = time.Now().UTC()
	}
	sample := TelemetrySample{
		ProjectRef:       ref,
		Source:           source,
		CPUPercent:       input.CPUPercent,
		MemoryBytes:      input.MemoryBytes,
		MemoryLimitBytes: input.MemoryLimitBytes,
		DiskUsedBytes:    input.DiskUsedBytes,
		DiskLimitBytes:   input.DiskLimitBytes,
		NetworkRxBytes:   input.NetworkRxBytes,
		NetworkTxBytes:   input.NetworkTxBytes,
		SampledAt:        sampledAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[ref]; !ok {
		return TelemetrySample{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	s.telemetry[ref] = sample
	return sample, nil
}

func (s *MemoryStore) RecordNodeTelemetry(ctx context.Context, hostID string, input NodeTelemetrySampleInput) (NodeTelemetrySample, error) {
	hostID = strings.TrimSpace(hostID)
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "unknown"
	}
	if input.CPUPercent < 0 || input.CPUUsedCores < 0 {
		return NodeTelemetrySample{}, fmt.Errorf("node cpu usage cannot be negative")
	}
	if input.CPUCapacityCores < 0 || input.MemoryUsedBytes < 0 || input.MemoryTotalBytes < 0 || input.DiskUsedBytes < 0 || input.DiskTotalBytes < 0 || input.DiskAvailableBytes < 0 || input.NetworkRxBytes < 0 || input.NetworkTxBytes < 0 {
		return NodeTelemetrySample{}, fmt.Errorf("node telemetry counters cannot be negative")
	}
	sampledAt := input.SampledAt.UTC()
	if sampledAt.IsZero() {
		sampledAt = time.Now().UTC()
	}
	sample := NodeTelemetrySample{
		HostID:             hostID,
		Source:             source,
		CPUPercent:         input.CPUPercent,
		CPUUsedCores:       input.CPUUsedCores,
		CPUCapacityCores:   input.CPUCapacityCores,
		MemoryUsedBytes:    input.MemoryUsedBytes,
		MemoryTotalBytes:   input.MemoryTotalBytes,
		DiskUsedBytes:      input.DiskUsedBytes,
		DiskTotalBytes:     input.DiskTotalBytes,
		DiskAvailableBytes: input.DiskAvailableBytes,
		NetworkSampled:     input.NetworkSampled,
		NetworkRxBytes:     input.NetworkRxBytes,
		NetworkTxBytes:     input.NetworkTxBytes,
		SampledAt:          sampledAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.hosts[hostID]; !ok {
		return NodeTelemetrySample{}, fmt.Errorf("%w: host %s", ErrNotFound, hostID)
	}
	if s.nodeTelemetry == nil {
		s.nodeTelemetry = map[string]NodeTelemetrySample{}
	}
	s.nodeTelemetry[hostID] = sample
	return sample, nil
}

func (s *MemoryStore) GetFleetMetrics(ctx context.Context) (FleetMetrics, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	metrics := FleetMetrics{
		Orgs:             len(s.orgs),
		Users:            len(s.users),
		Hosts:            len(s.hosts),
		Projects:         len(s.projects),
		ProjectsByStatus: map[string]int{},
		NodeObserved:     []NodeTelemetrySample{},
		ProjectLogEvents: len(s.projectLogs),
		AuditEvents:      len(s.auditEvents),
		AuditVerified:    true,
		SampledAt:        time.Now().UTC(),
	}
	for _, project := range s.projects {
		metrics.ProjectsByStatus[string(project.Status)]++
	}
	for _, replicas := range s.replicas {
		metrics.ReadReplicas += len(replicas)
	}
	for _, host := range s.hosts {
		metrics.HostCapacity = addHostCapacity(metrics.HostCapacity, host.Capacity)
		metrics.HostUsed = addHostCapacity(metrics.HostUsed, host.Used)
		if sample, ok := s.nodeTelemetry[host.ID]; ok {
			metrics.NodeObserved = append(metrics.NodeObserved, sample)
		}
	}
	sort.Slice(metrics.NodeObserved, func(i, j int) bool {
		return metrics.NodeObserved[i].SampledAt.After(metrics.NodeObserved[j].SampledAt)
	})
	metrics.Observed = telemetryRollup(s.projects, s.telemetry, time.Now().UTC())
	s.addRegisteredProjectChildFleetMetricsLocked(&metrics)
	for ref := range s.projects {
		metrics.DatabaseExtensions += countEnabledDatabaseExtensions(ref, s.databaseExtensions[ref])
	}
	for ref, policy := range s.cdnPolicies {
		if _, ok := s.projects[ref]; ok && policy.Enabled {
			metrics.CDNEnabledProjects++
		}
	}
	for _, backup := range s.backups {
		metrics.Backups++
		metrics.BackupStorageBytes += backup.SizeBytes
	}
	for _, archive := range s.walArchives {
		metrics.WALArchives++
		metrics.WALArchiveBytes += archive.SizeBytes
	}
	previousHash := ""
	for index, event := range s.auditEvents {
		expectedIndex := index + 1
		if event.ChainIndex != expectedIndex || event.PreviousHash != previousHash || event.Hash != hashAuditEvent(event) {
			metrics.AuditVerified = false
			break
		}
		previousHash = event.Hash
	}
	return metrics, nil
}

func (s *MemoryStore) GetProjectMetrics(ctx context.Context, ref string) (ProjectMetrics, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.projects[ref]
	if !ok {
		return ProjectMetrics{}, fmt.Errorf("%w: project %s", ErrNotFound, ref)
	}
	metrics := ProjectMetrics{
		ProjectRef:   project.Ref,
		OrgID:        project.OrgID,
		Status:       project.Status,
		ResourceTier: project.Spec.ResourceTier,
		Resources:    resourceReservationForSpec(project.Spec),
		SampledAt:    time.Now().UTC(),
	}
	if sample, ok := s.telemetry[ref]; ok {
		metrics.Observed = &sample
	}
	s.addRegisteredProjectChildMetricsLocked(ref, &metrics)
	metrics.DatabaseExtensions = countEnabledDatabaseExtensions(ref, s.databaseExtensions[ref])
	if policy, ok := s.cdnPolicies[ref]; ok {
		metrics.CDNEnabled = policy.Enabled
	}
	for _, replica := range s.replicas[ref] {
		metrics.ReadReplicas++
		metrics.Resources = addHostCapacity(metrics.Resources, replicaReservationForTier(replica.Tier))
	}
	for _, backup := range s.backups {
		if backup.ProjectRef != ref {
			continue
		}
		metrics.Backups++
		metrics.BackupStorageBytes += backup.SizeBytes
	}
	for _, archive := range s.walArchives {
		if archive.ProjectRef != ref {
			continue
		}
		metrics.WALArchives++
		metrics.WALArchiveBytes += archive.SizeBytes
	}
	for _, logEntry := range s.projectLogs {
		if logEntry.ProjectRef == ref {
			metrics.ProjectLogEvents++
		}
	}
	target := "project:" + ref
	for _, event := range s.auditEvents {
		if event.Target == target || event.Metadata["project_ref"] == ref || event.Metadata["ref"] == ref {
			metrics.ActivityEvents++
		}
	}
	metrics.DBAllocatedBytes = int64(resourceReservationForSpec(project.Spec).DiskGB) * 1024 * 1024 * 1024
	metrics.StorageBytes = metrics.BackupStorageBytes + metrics.WALArchiveBytes
	return metrics, nil
}

func Audit(ctx context.Context, store Store, action string, target string, metadata map[string]string) {
	if store == nil {
		return
	}
	_, _ = store.RecordAuditEvent(ctx, AuditEventInput{
		Action:   action,
		Target:   target,
		Metadata: metadata,
	})
}

func LogProject(ctx context.Context, store Store, ref string, level string, message string, metadata map[string]string) {
	if store == nil || ref == "" {
		return
	}
	_, _ = store.RecordProjectLog(ctx, ProjectLogInput{
		ProjectRef: ref,
		Level:      level,
		Message:    message,
		Metadata:   metadata,
	})
}

func SecretRevealFor(secret ProjectSecret) ProjectSecretReveal {
	return ProjectSecretReveal{
		Kind:      secret.Kind,
		Value:     secret.Value,
		CreatedAt: secret.CreatedAt,
		RotatedAt: secret.RotatedAt,
	}
}

func validateCreateProject(req CreateProjectRequest) error {
	if req.OrgID == "" {
		return fmt.Errorf("org id is required")
	}
	if !projectRefPattern.MatchString(req.Ref) {
		return fmt.Errorf("ref must be 3-55 lowercase letters, numbers, or hyphens, and cannot start or end with a hyphen")
	}
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	domain, err := normalizeDomain(req.Domain)
	if err != nil {
		return fmt.Errorf("domain %w", err)
	}
	if err := validateGeneratedProjectFQDNs(req.Ref, domain); err != nil {
		return err
	}
	if strings.TrimSpace(req.StackVersion) == "" {
		return fmt.Errorf("stack version is required")
	}
	if err := validateSupportedStackVersion(req.StackVersion); err != nil {
		return err
	}
	if err := validateStackProfile(req.Profile); err != nil {
		return err
	}
	if err := validateResourceTier(req.ResourceTier); err != nil {
		return err
	}
	if err := validateResourceSizing(req.CPU, req.RAMMB, req.DiskGB); err != nil {
		return err
	}
	if _, err := normalizeProjectServices(req.Services); err != nil {
		return err
	}
	return nil
}

// validateResourceSizing bounds optional exact-size overrides. A zero value
// means "use the tier preset" and is always valid; non-zero values must fall
// within sane platform limits.
func validateResourceSizing(cpu, ramMB, diskGB int) error {
	if cpu < 0 || ramMB < 0 || diskGB < 0 {
		return fmt.Errorf("resource sizing cannot be negative")
	}
	if cpu > maxProjectCPU {
		return fmt.Errorf("cpu cannot exceed %d cores", maxProjectCPU)
	}
	if ramMB > 0 && ramMB < minProjectRAMMB {
		return fmt.Errorf("ram cannot be below %d MB", minProjectRAMMB)
	}
	if ramMB > maxProjectRAMMB {
		return fmt.Errorf("ram cannot exceed %d MB", maxProjectRAMMB)
	}
	if diskGB > maxProjectDiskGB {
		return fmt.Errorf("disk cannot exceed %d GB", maxProjectDiskGB)
	}
	return nil
}

func validateGeneratedProjectFQDNs(ref string, domain string) error {
	ref = strings.ToLower(strings.TrimSpace(ref))
	domain = strings.Trim(strings.ToLower(strings.TrimSpace(domain)), ".")
	for label, host := range generatedProjectHosts(ref, domain) {
		if len(host) > 253 {
			return fmt.Errorf("%s host %s exceeds the 253-character DNS name limit; shorten the project ref or apps domain", label, host)
		}
	}
	return nil
}

func generatedProjectHosts(ref string, domain string) map[string]string {
	ref = strings.ToLower(strings.TrimSpace(ref))
	domain = strings.Trim(strings.ToLower(strings.TrimSpace(domain)), ".")
	return map[string]string{
		"project API":      projectHost(ref, domain),
		"project Studio":   studioHost(ref, domain),
		"project Storage":  storageHost(ref, domain),
		"project database": databaseHost(ref, domain),
		"project pooler":   poolerHost(ref, domain),
	}
}

func AllowedProjectServices() []string {
	return append([]string(nil), allowedProjectServices...)
}

func DefaultProjectServiceStates() map[string]bool {
	out := map[string]bool{}
	for _, name := range allowedProjectServices {
		out[name] = true
	}
	return out
}

func ProjectServiceStates(input map[string]ServiceSpec) map[string]bool {
	out := DefaultProjectServiceStates()
	for key, spec := range input {
		normalized, ok := normalizeProjectServiceName(key)
		if !ok {
			continue
		}
		out[normalized] = spec.Enabled
	}
	return out
}

func normalizeProjectServices(input map[string]bool) (map[string]ServiceSpec, error) {
	states := DefaultProjectServiceStates()
	for key, enabled := range input {
		normalized, ok := normalizeProjectServiceName(key)
		if !ok {
			return nil, fmt.Errorf("unsupported project service %q", key)
		}
		states[normalized] = enabled
	}
	out := map[string]ServiceSpec{}
	for _, name := range allowedProjectServices {
		out[name] = ServiceSpec{Enabled: states[name]}
	}
	return out, nil
}

func normalizeProjectServiceName(input string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(input))
	switch normalized {
	case "edge-runtime", "edge_runtime":
		normalized = "functions"
	case "supavisor":
		normalized = "pooler"
	case "logflare":
		normalized = "analytics"
	}
	for _, allowed := range allowedProjectServices {
		if normalized == allowed {
			return normalized, true
		}
	}
	return "", false
}

func normalizeConfigArea(area string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(area))
	if normalized == "" {
		return "", fmt.Errorf("config area is required")
	}
	if _, ok := allowedConfigAreas[normalized]; !ok {
		return "", fmt.Errorf("unsupported config area %q", normalized)
	}
	return normalized, nil
}

func normalizeConfigValues(values map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range values {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			return nil, fmt.Errorf("config key is required")
		}
		if strings.ContainsAny(normalizedKey, " /\\") {
			return nil, fmt.Errorf("config key %q is invalid", key)
		}
		out[normalizedKey] = strings.TrimSpace(value)
	}
	return out, nil
}

func validateGeneralConfig(config map[string]string) error {
	switch strings.ToLower(strings.TrimSpace(config["environment"])) {
	case "", "development", "production":
		return nil
	default:
		return fmt.Errorf("environment must be development or production")
	}
}

func validateNetworkConfig(config map[string]string) error {
	for _, key := range []string{"http_allowlist", "db_allowlist"} {
		for _, entry := range splitAllowlist(config[key]) {
			if _, err := netip.ParsePrefix(entry); err == nil {
				continue
			}
			if _, err := netip.ParseAddr(entry); err == nil {
				continue
			}
			return fmt.Errorf("invalid %s entry %q", key, entry)
		}
	}
	switch strings.ToLower(strings.TrimSpace(config["db_ingress_mode"])) {
	case "", "private", "allowlisted", "public":
	default:
		return fmt.Errorf("db_ingress_mode must be private, allowlisted, or public")
	}
	if strings.EqualFold(strings.TrimSpace(config["db_ingress_mode"]), "allowlisted") && len(splitAllowlist(config["db_allowlist"])) == 0 {
		return fmt.Errorf("allowlisted database ingress requires at least one db_allowlist entry")
	}
	return nil
}

func validateAuthConfig(config map[string]string) error {
	if err := validateIntegerConfig(config, "mfa_phone_otp_length", 4, 10); err != nil {
		return err
	}
	if maxFrequency := strings.TrimSpace(config["mfa_phone_max_frequency"]); maxFrequency != "" {
		if _, err := time.ParseDuration(maxFrequency); err != nil {
			return fmt.Errorf("mfa_phone_max_frequency must be a duration")
		}
	}
	provider := strings.ToLower(strings.TrimSpace(config["captcha_provider"]))
	if provider != "" {
		switch provider {
		case "hcaptcha", "turnstile":
		default:
			return fmt.Errorf("unsupported captcha_provider %q", provider)
		}
	}
	secretHandle := strings.TrimSpace(config["captcha_secret_handle"])
	if secretHandle != "" && !strings.HasPrefix(secretHandle, "secret://") {
		return fmt.Errorf("captcha_secret_handle must be a secret:// handle")
	}
	return nil
}

func validateAuthProvidersConfig(config map[string]string) error {
	for key, value := range config {
		if strings.HasSuffix(key, "_secret_handle") || strings.HasSuffix(key, "_token_handle") || strings.HasSuffix(key, "_key_handle") || key == "sms_test_otp_handle" {
			if trimmed := strings.TrimSpace(value); trimmed != "" && !strings.HasPrefix(trimmed, "secret://") {
				return fmt.Errorf("%s must be a secret:// handle", key)
			}
		}
	}
	smsProvider := strings.ToLower(strings.TrimSpace(config["sms_provider"]))
	if smsProvider != "" {
		switch smsProvider {
		case "twilio", "twilio_verify", "messagebird", "textlocal", "vonage":
		default:
			return fmt.Errorf("unsupported sms_provider %q", smsProvider)
		}
	}
	if err := validateIntegerConfig(config, "sms_otp_exp", 1, 86400); err != nil {
		return err
	}
	if err := validateIntegerConfig(config, "sms_otp_length", 4, 10); err != nil {
		return err
	}
	if maxFrequency := strings.TrimSpace(config["sms_max_frequency"]); maxFrequency != "" {
		if _, err := time.ParseDuration(maxFrequency); err != nil {
			return fmt.Errorf("sms_max_frequency must be a duration")
		}
	}
	if validUntil := strings.TrimSpace(config["sms_test_otp_valid_until"]); validUntil != "" {
		if _, err := time.Parse(time.RFC3339, validUntil); err != nil {
			return fmt.Errorf("sms_test_otp_valid_until must be an RFC3339 timestamp")
		}
	}
	oidcIssuer := strings.TrimSpace(config["oauth_oidc_issuer_url"])
	if oidcIssuer != "" {
		parsed, err := url.Parse(oidcIssuer)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("oauth_oidc_issuer_url must be an https URL")
		}
	}
	return nil
}

func validateAIConfig(config map[string]string) error {
	for _, key := range []string{"openai_api_key_handle", "huggingface_api_key_handle", "studio_assistant_key_handle"} {
		value := strings.TrimSpace(config[key])
		if value != "" && !strings.HasPrefix(value, "secret://") {
			return fmt.Errorf("%s must be a secret:// handle", key)
		}
	}
	provider := strings.ToLower(strings.TrimSpace(config["default_embedding_provider"]))
	if provider == "" {
		provider = "openai"
	}
	if _, ok := allowedEmbeddingProviders[provider]; !ok {
		return fmt.Errorf("unsupported default embedding provider %q", provider)
	}
	assistantProvider := strings.ToLower(strings.TrimSpace(config["studio_assistant_provider"]))
	if assistantProvider == "" {
		assistantProvider = "openai"
	}
	if _, ok := allowedEmbeddingProviders[assistantProvider]; !ok {
		return fmt.Errorf("unsupported studio assistant provider %q", assistantProvider)
	}
	dimension := strings.TrimSpace(config["default_embedding_dimension"])
	if dimension != "" {
		value, err := strconv.Atoi(dimension)
		if err != nil || value < 1 || value > 65535 {
			return fmt.Errorf("default_embedding_dimension must be between 1 and 65535")
		}
	}
	return nil
}

func validatePoolerConfig(config map[string]string) error {
	mode := strings.ToLower(strings.TrimSpace(config["pool_mode"]))
	if mode == "" {
		mode = "transaction"
	}
	switch mode {
	case "transaction", "session", "both":
	default:
		return fmt.Errorf("unsupported pool_mode %q", mode)
	}
	tier := strings.ToLower(strings.TrimSpace(config["dedicated_pooler_tier"]))
	if tier == "" {
		tier = "small"
	}
	switch tier {
	case "small", "medium", "large":
	default:
		return fmt.Errorf("unsupported dedicated_pooler_tier %q", tier)
	}
	if err := validateIntegerConfig(config, "default_pool_size", 1, 10000); err != nil {
		return err
	}
	if err := validateIntegerConfig(config, "max_client_connections", 1, 100000); err != nil {
		return err
	}
	if err := validateFixedIntegerConfig(config, "transaction_port", 6543); err != nil {
		return err
	}
	if err := validateFixedIntegerConfig(config, "session_port", 5432); err != nil {
		return err
	}
	return nil
}

func validateFunctionsConfig(config map[string]string) error {
	if err := validateIntegerConfig(config, "worker_timeout_ms", 100, 300000); err != nil {
		return err
	}
	policy := strings.ToLower(strings.TrimSpace(config["deployment_policy"]))
	if policy == "" {
		return nil
	}
	switch policy {
	case "manual", "ci", "locked":
		return nil
	default:
		return fmt.Errorf("unsupported deployment_policy %q", policy)
	}
}

func validateSMTPConfig(config map[string]string) error {
	if err := validateIntegerConfig(config, "port", 1, 65535); err != nil {
		return err
	}
	passwordHandle := strings.TrimSpace(config["password_handle"])
	if passwordHandle != "" && !strings.HasPrefix(passwordHandle, "secret://") {
		return fmt.Errorf("password_handle must be a secret:// handle")
	}
	mode := strings.ToLower(strings.TrimSpace(config["tls_mode"]))
	if mode == "" {
		return nil
	}
	switch mode {
	case "starttls", "implicit", "none":
		return nil
	default:
		return fmt.Errorf("unsupported smtp tls_mode %q", mode)
	}
}

func validateIntegerConfig(config map[string]string, key string, min int, max int) error {
	raw := strings.TrimSpace(config[key])
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return fmt.Errorf("%s must be between %d and %d", key, min, max)
	}
	return nil
}

func validateFixedIntegerConfig(config map[string]string, key string, expected int) error {
	raw := strings.TrimSpace(config[key])
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value != expected {
		return fmt.Errorf("%s is fixed at %d for hosted-compatible public routing", key, expected)
	}
	return nil
}

func normalizeMembershipRole(role string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(role))
	if normalized == "" {
		normalized = "developer"
	}
	if _, ok := allowedMembershipRoles[normalized]; !ok {
		return "", fmt.Errorf("unsupported membership role %q", normalized)
	}
	return normalized, nil
}

func normalizeTeamSlug(input string) string {
	normalized := strings.ToLower(strings.TrimSpace(input))
	var builder strings.Builder
	lastHyphen := false
	for _, r := range normalized {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			builder.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			builder.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func (s *MemoryStore) resolveAccessSubjectIDLocked(orgID string, subjectType string, subjectID string) (string, error) {
	switch subjectType {
	case "user":
		email := strings.ToLower(strings.TrimSpace(subjectID))
		user, ok := s.users[email]
		if ok {
			return user.ID, nil
		}
		for _, user := range s.users {
			if user.ID == subjectID {
				return user.ID, nil
			}
		}
		return "", fmt.Errorf("%w: user %s", ErrNotFound, subjectID)
	case "team":
		if team, ok := s.teams[orgID][normalizeTeamSlug(subjectID)]; ok {
			return team.ID, nil
		}
		for _, team := range s.teams[orgID] {
			if team.ID == subjectID {
				return team.ID, nil
			}
		}
		return "", fmt.Errorf("%w: team %s for org %s", ErrNotFound, subjectID, orgID)
	default:
		return "", fmt.Errorf("subject type must be user or team")
	}
}

func higherRole(left string, right string) string {
	if membershipRoleRank(right) > membershipRoleRank(left) {
		return right
	}
	return left
}

func mergeEffectiveRole(roles map[string]EffectiveProjectRole, userID string, email string, role string, source string) {
	current := roles[email]
	if current.Email == "" {
		current = EffectiveProjectRole{UserID: userID, Email: email, Role: role}
	}
	current.Sources = append(current.Sources, source)
	current.Role = higherRole(current.Role, role)
	roles[email] = current
}

func membershipRoleRank(role string) int {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner":
		return 4
	case "admin":
		return 3
	case "developer":
		return 2
	case "viewer":
		return 1
	default:
		return 0
	}
}

func resourceReservationForTier(tier ResourceTier) HostCapacity {
	reservation, ok := resourceTierReservations[tier]
	if !ok {
		return resourceTierReservations[ResourceTierSmall]
	}
	return reservation
}

// resourceReservationForSpec starts from the project's tier preset and applies
// any exact per-dimension overrides (CPU cores / RAM MB / disk GB) carried on
// the spec. A zero override means "use the preset", so presets and exact sizing
// compose cleanly.
func resourceReservationForSpec(spec ProjectSpec) HostCapacity {
	reservation := resourceReservationForTier(spec.ResourceTier)
	if spec.CPU > 0 {
		reservation.CPU = spec.CPU
	}
	if spec.RAMMB > 0 {
		reservation.RAMMB = spec.RAMMB
	}
	if spec.DiskGB > 0 {
		reservation.DiskGB = spec.DiskGB
	}
	return reservation
}

// EffectiveResourceSizing resolves the CPU cores, RAM (MB), and disk (GB) for a
// spec by combining its tier preset with any exact per-dimension overrides. It
// is the single source of truth shared by capacity accounting and the
// provisioner's optional runtime-limit enforcement.
func EffectiveResourceSizing(spec ProjectSpec) (cpu int, ramMB int, diskGB int) {
	reservation := resourceReservationForSpec(spec)
	return reservation.CPU, reservation.RAMMB, reservation.DiskGB
}

func replicaReservationForTier(tier ResourceTier) HostCapacity {
	reservation := resourceReservationForTier(tier)
	reservation.Project = 0
	return reservation
}

func (s *MemoryStore) projectReplicaRoutingLocked(project Project, replicas []ProjectReplica) ProjectReplicaRouting {
	targets := make([]ProjectReplicaRouteTarget, 0, len(replicas))
	var candidate *ProjectReplicaRouteTarget
	primaryReplicaID := ""
	for index, replica := range replicas {
		replica = normalizedReplicaForRouting(replica, index)
		target := replicaRouteTarget(replica)
		targets = append(targets, target)
		if replica.Role == "primary" {
			primaryReplicaID = replica.ID
		}
		if replica.Status == "healthy" && replica.Role != "primary" {
			next := target
			if candidate == nil || compareReplicaRouteTarget(next, *candidate) < 0 {
				candidate = &next
			}
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		return compareReplicaRouteTarget(targets[i], targets[j]) < 0
	})
	healthyReadTargets := make([]ProjectReplicaRouteTarget, 0, len(targets))
	for _, target := range targets {
		if target.Status == "healthy" && target.Role != "primary" && target.Weight > 0 {
			healthyReadTargets = append(healthyReadTargets, target)
		}
	}
	return ProjectReplicaRouting{
		ProjectRef:         project.Ref,
		PrimaryURI:         projectPrimaryURI(project),
		ReadStrategy:       "weighted-healthy",
		AutoFailover:       true,
		PrimaryReplicaID:   primaryReplicaID,
		FailoverCandidate:  candidate,
		HealthyReadTargets: healthyReadTargets,
		AllTargets:         targets,
	}
}

func normalizedReplicaForRouting(replica ProjectReplica, index int) ProjectReplica {
	if replica.Role == "" {
		replica.Role = "read"
	}
	if replica.ReadWeight <= 0 {
		replica.ReadWeight = 100
	}
	if replica.FailoverPriority <= 0 {
		replica.FailoverPriority = index + 1
	}
	return replica
}

func replicaRouteTarget(replica ProjectReplica) ProjectReplicaRouteTarget {
	return ProjectReplicaRouteTarget{
		ReplicaID:             replica.ID,
		Name:                  replica.Name,
		URI:                   replica.ReadURI,
		Region:                replica.Region,
		Weight:                replica.ReadWeight,
		FailoverPriority:      replica.FailoverPriority,
		ReplicationLagBytes:   replica.ReplicationLagBytes,
		ReplicationLagSeconds: replica.ReplicationLagSeconds,
		Role:                  replica.Role,
		Status:                replica.Status,
	}
}

func compareReplicaFailoverCandidate(left ProjectReplica, right ProjectReplica) int {
	return compareReplicaRouteTarget(replicaRouteTarget(normalizedReplicaForRouting(left, 0)), replicaRouteTarget(normalizedReplicaForRouting(right, 0)))
}

func compareReplicaRouteTarget(left ProjectReplicaRouteTarget, right ProjectReplicaRouteTarget) int {
	if left.FailoverPriority != right.FailoverPriority {
		return left.FailoverPriority - right.FailoverPriority
	}
	if left.ReplicationLagSeconds != right.ReplicationLagSeconds {
		return left.ReplicationLagSeconds - right.ReplicationLagSeconds
	}
	if left.ReplicationLagBytes != right.ReplicationLagBytes {
		if left.ReplicationLagBytes < right.ReplicationLagBytes {
			return -1
		}
		return 1
	}
	return strings.Compare(left.Name, right.Name)
}

func projectPrimaryURI(project Project) string {
	return fmt.Sprintf("postgres://postgres:${DB_PASSWORD}@db.%s.internal:5432/postgres", project.Ref)
}

func defaultReplicaMessage(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func hostHasCapacity(host Host, reservation HostCapacity) bool {
	next := addHostCapacity(host.Used, reservation)
	return capacityWithinLimit(next.CPU, host.Capacity.CPU) &&
		capacityWithinLimit(next.RAMMB, host.Capacity.RAMMB) &&
		capacityWithinLimit(next.DiskGB, host.Capacity.DiskGB) &&
		capacityWithinLimit(next.Project, host.Capacity.Project)
}

func capacityWithinLimit(next int, limit int) bool {
	return limit <= 0 || next <= limit
}

func quotaWithinLimit(next int, limit int) bool {
	return limit <= 0 || next <= limit
}

func (s *MemoryStore) validateOrgQuotaLocked(orgID string, reservation HostCapacity) error {
	quota := s.orgQuotaLocked(orgID)
	next := addHostCapacity(quota.Used, reservation)
	if !quotaWithinLimit(next.Project, quota.MaxProjects) {
		return fmt.Errorf("%w: org %s project quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.CPU, quota.MaxCPU) {
		return fmt.Errorf("%w: org %s cpu quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.RAMMB, quota.MaxRAMMB) {
		return fmt.Errorf("%w: org %s ram quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.DiskGB, quota.MaxDiskGB) {
		return fmt.Errorf("%w: org %s disk quota exceeded", ErrConflict, orgID)
	}
	return nil
}

func (s *MemoryStore) validateOrgReplicaQuotaLocked(orgID string, reservation HostCapacity) error {
	quota := s.orgQuotaLocked(orgID)
	next := addHostCapacity(quota.Used, reservation)
	if !quotaWithinLimit(next.CPU, quota.MaxCPU) {
		return fmt.Errorf("%w: org %s cpu quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.RAMMB, quota.MaxRAMMB) {
		return fmt.Errorf("%w: org %s ram quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.DiskGB, quota.MaxDiskGB) {
		return fmt.Errorf("%w: org %s disk quota exceeded", ErrConflict, orgID)
	}
	return nil
}

func (s *MemoryStore) validateProjectScaleQuotaLocked(orgID string, oldReservation HostCapacity, newReservation HostCapacity) error {
	quota := s.orgQuotaLocked(orgID)
	next := addHostCapacity(subtractHostCapacity(quota.Used, oldReservation), newReservation)
	if !quotaWithinLimit(next.Project, quota.MaxProjects) {
		return fmt.Errorf("%w: org %s project quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.CPU, quota.MaxCPU) {
		return fmt.Errorf("%w: org %s cpu quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.RAMMB, quota.MaxRAMMB) {
		return fmt.Errorf("%w: org %s ram quota exceeded", ErrConflict, orgID)
	}
	if !quotaWithinLimit(next.DiskGB, quota.MaxDiskGB) {
		return fmt.Errorf("%w: org %s disk quota exceeded", ErrConflict, orgID)
	}
	return nil
}

func (s *MemoryStore) orgQuotaLocked(orgID string) OrgQuota {
	quota := s.orgQuotas[orgID]
	if quota.OrgID == "" {
		quota.OrgID = orgID
		quota.UpdatedAt = time.Now().UTC()
	}
	quota.Used = s.orgUsageLocked(orgID)
	return quota
}

func (s *MemoryStore) orgUsageLocked(orgID string) HostCapacity {
	usage := HostCapacity{}
	for _, project := range s.projects {
		if project.OrgID == orgID {
			usage = addHostCapacity(usage, resourceReservationForSpec(project.Spec))
		}
	}
	for ref, replicas := range s.replicas {
		project, ok := s.projects[ref]
		if !ok || project.OrgID != orgID {
			continue
		}
		for _, replica := range replicas {
			usage = addHostCapacity(usage, replicaReservationForTier(replica.Tier))
		}
	}
	return usage
}

func (s *MemoryStore) userByIDLocked(id string) (User, bool) {
	for _, user := range s.users {
		if user.ID == id {
			return user, true
		}
	}
	return User{}, false
}

func mfaStatusForUser(user User) MFAStatus {
	var confirmedAt *time.Time
	if !user.MFAConfirmedAt.IsZero() {
		confirmedAt = &user.MFAConfirmedAt
	}
	updatedAt := user.MFAUpdatedAt
	if updatedAt.IsZero() {
		updatedAt = user.CreatedAt
	}
	return MFAStatus{
		UserID:      user.ID,
		Email:       user.Email,
		Enabled:     user.MFAEnabled,
		Pending:     user.MFAPendingSecret != "",
		ConfirmedAt: confirmedAt,
		UpdatedAt:   updatedAt,
	}
}

func hashAuditEvent(event AuditEvent) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("%d\n", event.ChainIndex))
	builder.WriteString(event.PreviousHash)
	builder.WriteByte('\n')
	builder.WriteString(event.ID)
	builder.WriteByte('\n')
	builder.WriteString(event.ActorID)
	builder.WriteByte('\n')
	builder.WriteString(event.Action)
	builder.WriteByte('\n')
	builder.WriteString(event.Target)
	builder.WriteByte('\n')
	builder.WriteString(event.CreatedAt.UTC().Format(time.RFC3339Nano))
	builder.WriteByte('\n')
	keys := make([]string, 0, len(event.Metadata))
	for key := range event.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(event.Metadata[key])
		builder.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

func addHostCapacity(left HostCapacity, right HostCapacity) HostCapacity {
	return HostCapacity{
		CPU:     left.CPU + right.CPU,
		RAMMB:   left.RAMMB + right.RAMMB,
		DiskGB:  left.DiskGB + right.DiskGB,
		Project: left.Project + right.Project,
	}
}

func subtractHostCapacity(left HostCapacity, right HostCapacity) HostCapacity {
	return HostCapacity{
		CPU:     maxInt(0, left.CPU-right.CPU),
		RAMMB:   maxInt(0, left.RAMMB-right.RAMMB),
		DiskGB:  maxInt(0, left.DiskGB-right.DiskGB),
		Project: maxInt(0, left.Project-right.Project),
	}
}

func telemetryRollup(projects map[string]Project, samples map[string]TelemetrySample, now time.Time) TelemetryRollup {
	const staleAfter = 5 * time.Minute
	rollup := TelemetryRollup{StaleAfterSeconds: int(staleAfter.Seconds())}
	for ref := range projects {
		sample, ok := samples[ref]
		if !ok {
			continue
		}
		if rollup.LatestSampledAt.IsZero() || sample.SampledAt.After(rollup.LatestSampledAt) {
			rollup.LatestSampledAt = sample.SampledAt
		}
		if rollup.OldestSampledAt.IsZero() || sample.SampledAt.Before(rollup.OldestSampledAt) {
			rollup.OldestSampledAt = sample.SampledAt
		}
		if now.Sub(sample.SampledAt) > staleAfter {
			rollup.StaleProjects++
			continue
		}
		rollup.ProjectsSampled++
		rollup.CPUPercent += sample.CPUPercent
		rollup.MemoryBytes += sample.MemoryBytes
		rollup.MemoryLimitBytes += sample.MemoryLimitBytes
		rollup.DiskUsedBytes += sample.DiskUsedBytes
		rollup.DiskLimitBytes += sample.DiskLimitBytes
		rollup.NetworkRxBytes += sample.NetworkRxBytes
		rollup.NetworkTxBytes += sample.NetworkTxBytes
	}
	return rollup
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func normalizeProjectSecretKind(kind string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if normalized == "" {
		return "", fmt.Errorf("secret kind is required")
	}
	if strings.Contains(normalized, "/") || !secretKindPattern.MatchString(normalized) {
		return "", fmt.Errorf("secret kind %q is invalid", normalized)
	}
	return normalized, nil
}

func normalizeManagedProjectSecretKind(kind string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if normalized == "" {
		return "", fmt.Errorf("secret kind is required")
	}
	if _, ok := secretPrefixes[normalized]; !ok {
		if !strings.HasPrefix(normalized, "jwt_signing_key_previous_") {
			return "", fmt.Errorf("unsupported secret kind %q", normalized)
		}
	}
	return normalized, nil
}

func normalizeCustomProjectSecretKind(kind string) (string, error) {
	normalized, err := normalizeProjectSecretKind(kind)
	if err != nil {
		return "", err
	}
	if _, managed := secretPrefixes[normalized]; managed || strings.HasPrefix(normalized, "jwt_signing_key_") {
		return "", fmt.Errorf("secret kind %q is managed by the control plane", normalized)
	}
	return normalized, nil
}

func normalizeFunctionName(name string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return "", fmt.Errorf("function name is required")
	}
	if !refPattern.MatchString(normalized) {
		return "", fmt.Errorf("function name must be 3-64 lowercase letters, numbers, or dashes")
	}
	return normalized, nil
}

func normalizeProjectFunctionRegion(ref string, input ProjectFunctionRegionInput) (ProjectFunctionRegion, error) {
	functionName, err := normalizeFunctionName(input.FunctionName)
	if err != nil {
		return ProjectFunctionRegion{}, err
	}
	region := strings.ToLower(strings.TrimSpace(input.Region))
	if region == "" {
		region = "local"
	}
	if len(region) > 64 || strings.ContainsAny(region, " \r\n\t/\\") {
		return ProjectFunctionRegion{}, fmt.Errorf("region is invalid")
	}
	hostID := strings.TrimSpace(input.HostID)
	routingPolicy := strings.ToLower(strings.TrimSpace(input.RoutingPolicy))
	if routingPolicy == "" {
		routingPolicy = "nearest"
	}
	switch routingPolicy {
	case "nearest", "primary", "weighted":
	default:
		return ProjectFunctionRegion{}, fmt.Errorf("routing_policy must be nearest, primary, or weighted")
	}
	now := time.Now().UTC()
	projectRef := strings.ToLower(strings.TrimSpace(ref))
	return ProjectFunctionRegion{
		ID:            newID(),
		ProjectRef:    projectRef,
		FunctionName:  functionName,
		HostID:        hostID,
		Region:        region,
		RoutingPolicy: routingPolicy,
		InvocationURL: fmt.Sprintf("https://%s.%s.%s.functions.internal", functionName, region, projectRef),
		Status:        "configured",
		Message:       "regional invocation declaration recorded",
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func normalizeProjectFunctionStorageMount(ref string, input ProjectFunctionStorageMountInput) (ProjectFunctionStorageMount, error) {
	functionName, err := normalizeFunctionName(input.FunctionName)
	if err != nil {
		return ProjectFunctionStorageMount{}, err
	}
	bucketName := strings.ToLower(strings.TrimSpace(input.BucketName))
	if !refPattern.MatchString(bucketName) {
		return ProjectFunctionStorageMount{}, fmt.Errorf("bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}
	mountPath := strings.TrimSpace(strings.ReplaceAll(input.MountPath, "\\", "/"))
	if mountPath == "" {
		mountPath = "/mnt/" + bucketName
	}
	cleaned := path.Clean(mountPath)
	if !strings.HasPrefix(cleaned, "/mnt/") || cleaned == "/mnt" || strings.Contains(cleaned, "/../") {
		return ProjectFunctionStorageMount{}, fmt.Errorf("mount_path must be an absolute path under /mnt")
	}
	prefix := strings.TrimSpace(strings.ReplaceAll(input.Prefix, "\\", "/"))
	if prefix != "" {
		prefix = path.Clean(prefix)
		if prefix == "." {
			prefix = ""
		}
		if strings.HasPrefix(prefix, "../") || strings.HasPrefix(prefix, "/") || strings.Contains(prefix, "/../") {
			return ProjectFunctionStorageMount{}, fmt.Errorf("prefix must be relative to the bucket")
		}
	}
	envAlias := strings.ToUpper(strings.TrimSpace(input.EnvAlias))
	if envAlias == "" {
		envAlias = strings.ToUpper(strings.ReplaceAll(functionName+"_"+bucketName+"_MOUNT", "-", "_"))
	}
	if !envAliasPattern.MatchString(envAlias) {
		return ProjectFunctionStorageMount{}, fmt.Errorf("env_alias must be an uppercase environment variable name")
	}
	now := time.Now().UTC()
	return ProjectFunctionStorageMount{
		ID:           newID(),
		ProjectRef:   strings.ToLower(strings.TrimSpace(ref)),
		FunctionName: functionName,
		BucketName:   bucketName,
		MountPath:    cleaned,
		ReadOnly:     input.ReadOnly,
		Prefix:       prefix,
		EnvAlias:     envAlias,
		Status:       "configured",
		Message:      "function storage mount declaration recorded",
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func functionExists(functions []ProjectFunction, name string) bool {
	for _, function := range functions {
		if function.Name == name {
			return true
		}
	}
	return false
}

func storageBucketExists(buckets []ProjectStorageBucket, name string) bool {
	for _, bucket := range buckets {
		if bucket.Name == name {
			return true
		}
	}
	return false
}

func removeFunctionRegions(regions []ProjectFunctionRegion, functionName string) []ProjectFunctionRegion {
	out := regions[:0]
	for _, region := range regions {
		if functionName != "" && region.FunctionName == functionName {
			continue
		}
		out = append(out, region)
	}
	return out
}

func removeFunctionStorageMounts(mounts []ProjectFunctionStorageMount, functionName string, bucketName string) []ProjectFunctionStorageMount {
	out := mounts[:0]
	for _, mount := range mounts {
		if functionName != "" && mount.FunctionName == functionName {
			continue
		}
		if bucketName != "" && mount.BucketName == bucketName {
			continue
		}
		out = append(out, mount)
	}
	return out
}

func normalizeReplicaName(name string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return "", fmt.Errorf("replica name is required")
	}
	if !replicaNamePattern.MatchString(normalized) {
		return "", fmt.Errorf("replica name must be 3-64 lowercase letters, numbers, or dashes, and cannot start or end with a dash")
	}
	return normalized, nil
}

func validateReplicaPublicDNSHost(ref string, replicaName string, domain string) error {
	label := fmt.Sprintf("db-replica-%s-%s", routeName(replicaName), strings.ToLower(strings.TrimSpace(ref)))
	if len(label) <= 63 {
		host := replicaDatabaseHost(ref, replicaName, strings.Trim(strings.ToLower(strings.TrimSpace(domain)), "."))
		if len(host) > 253 {
			return fmt.Errorf("project replica host %s exceeds the 253-character DNS name limit; shorten the replica name, project ref, or apps domain", host)
		}
		return nil
	}
	maxReplicaNameLength := 63 - len("db-replica-") - 1 - len(strings.TrimSpace(ref))
	if maxReplicaNameLength < 3 {
		return fmt.Errorf("project ref %q is too long for public read-replica DNS labels; maximum ref length for replicas is 48 characters", ref)
	}
	return fmt.Errorf("replica name must be at most %d characters for project ref %q so public host %s stays within the 63-character DNS label limit", maxReplicaNameLength, ref, label)
}

func normalizeLogDrainTarget(target string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(target))
	if normalized == "" {
		return "", fmt.Errorf("log drain target is required")
	}
	if _, ok := allowedLogDrainTargets[normalized]; !ok {
		return "", fmt.Errorf("unsupported log drain target %q", normalized)
	}
	return normalized, nil
}

func normalizeReplicationPipelineType(pipelineType string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(pipelineType))
	if normalized == "" {
		normalized = "logical"
	}
	if _, ok := allowedReplicationPipelineTypes[normalized]; !ok {
		return "", fmt.Errorf("unsupported replication pipeline type %q", normalized)
	}
	return normalized, nil
}

func normalizeReplicationDestination(destination string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(destination))
	if normalized == "" {
		return "", fmt.Errorf("replication destination is required")
	}
	if _, ok := allowedReplicationDestinations[normalized]; !ok {
		return "", fmt.Errorf("unsupported replication destination %q", normalized)
	}
	return normalized, nil
}

func validateReplicationDestinationConfig(destination string, destinationURI string, config map[string]string) error {
	switch destination {
	case "postgres", "webhook":
		if strings.TrimSpace(destinationURI) == "" {
			return fmt.Errorf("replication destination %s requires destination_uri", destination)
		}
	case "s3", "iceberg":
		if strings.TrimSpace(config["bucket"]) == "" && strings.TrimSpace(destinationURI) == "" {
			return fmt.Errorf("replication destination %s requires bucket or destination_uri", destination)
		}
	case "bigquery":
		if strings.TrimSpace(config["dataset"]) == "" {
			return fmt.Errorf("replication destination bigquery requires dataset")
		}
	case "snowflake":
		if strings.TrimSpace(config["warehouse"]) == "" || strings.TrimSpace(config["database"]) == "" {
			return fmt.Errorf("replication destination snowflake requires warehouse and database")
		}
	case "redshift":
		if strings.TrimSpace(config["cluster"]) == "" && strings.TrimSpace(destinationURI) == "" {
			return fmt.Errorf("replication destination redshift requires cluster or destination_uri")
		}
	}
	return nil
}

func validateReplicationSecretHandles(config map[string]string) error {
	for key, value := range config {
		value = strings.TrimSpace(value)
		if value == "" || !isSensitiveProjectConfigKey(key) {
			continue
		}
		if !strings.HasPrefix(value, "secret://") {
			return fmt.Errorf("replication config %s must use a secret:// handle", key)
		}
	}
	return nil
}

func normalizeProjectEmbeddingJob(ref string, input ProjectEmbeddingJobInput) (ProjectEmbeddingJob, error) {
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	if provider == "" {
		provider = "openai"
	}
	if _, ok := allowedEmbeddingProviders[provider]; !ok {
		return ProjectEmbeddingJob{}, fmt.Errorf("unsupported embedding provider %q", provider)
	}
	sourceSchema := strings.ToLower(strings.TrimSpace(input.SourceSchema))
	if sourceSchema == "" {
		sourceSchema = "public"
	}
	sourceTable := strings.ToLower(strings.TrimSpace(input.SourceTable))
	sourceColumn := strings.ToLower(strings.TrimSpace(input.SourceColumn))
	primaryKeyColumn := strings.ToLower(strings.TrimSpace(input.PrimaryKeyColumn))
	if primaryKeyColumn == "" {
		primaryKeyColumn = "id"
	}
	destinationTable := strings.ToLower(strings.TrimSpace(input.DestinationTable))
	if destinationTable == "" {
		destinationTable = sourceTable + "_embeddings"
	}
	destinationColumn := strings.ToLower(strings.TrimSpace(input.DestinationColumn))
	if destinationColumn == "" {
		destinationColumn = "embedding"
	}
	for label, identifier := range map[string]string{
		"source_schema":      sourceSchema,
		"source_table":       sourceTable,
		"source_column":      sourceColumn,
		"primary_key_column": primaryKeyColumn,
		"destination_table":  destinationTable,
		"destination_column": destinationColumn,
	} {
		if !identifierPattern.MatchString(identifier) {
			return ProjectEmbeddingJob{}, fmt.Errorf("%s must be a simple Postgres identifier", label)
		}
	}
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if name == "" {
		name = sourceTable + "-" + sourceColumn + "-embeddings"
	}
	if !refPattern.MatchString(name) {
		return ProjectEmbeddingJob{}, fmt.Errorf("embedding job name must be 3-64 lowercase letters, numbers, or dashes")
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		if provider == "openai" {
			model = "text-embedding-3-small"
		} else {
			model = "default"
		}
	}
	dimension := input.Dimension
	if dimension == 0 {
		dimension = 1536
	}
	if dimension < 1 || dimension > 65535 {
		return ProjectEmbeddingJob{}, fmt.Errorf("dimension must be between 1 and 65535")
	}
	batchSize := input.BatchSize
	if batchSize == 0 {
		batchSize = 100
	}
	if batchSize < 1 || batchSize > 10000 {
		return ProjectEmbeddingJob{}, fmt.Errorf("batch_size must be between 1 and 10000")
	}
	schedule := strings.TrimSpace(input.Schedule)
	if schedule == "" {
		schedule = "manual"
	}
	if len(schedule) > 120 || strings.ContainsAny(schedule, "\r\n") {
		return ProjectEmbeddingJob{}, fmt.Errorf("schedule is invalid")
	}
	now := time.Now().UTC()
	return ProjectEmbeddingJob{
		ID:                newID(),
		ProjectRef:        ref,
		Name:              name,
		SourceSchema:      sourceSchema,
		SourceTable:       sourceTable,
		SourceColumn:      sourceColumn,
		PrimaryKeyColumn:  primaryKeyColumn,
		DestinationTable:  destinationTable,
		DestinationColumn: destinationColumn,
		Provider:          provider,
		Model:             model,
		Dimension:         dimension,
		Schedule:          schedule,
		BatchSize:         batchSize,
		Status:            "configured",
		Message:           "automatic embedding pipeline recorded",
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

func normalizeProjectVectorBucket(ref string, input ProjectVectorBucketInput) (ProjectVectorBucket, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectVectorBucket{}, fmt.Errorf("vector bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}
	dimension := input.Dimension
	if dimension == 0 {
		dimension = 1536
	}
	if dimension < 1 || dimension > 65535 {
		return ProjectVectorBucket{}, fmt.Errorf("dimension must be between 1 and 65535")
	}
	distance := strings.ToLower(strings.TrimSpace(input.Distance))
	if distance == "" {
		distance = "cosine"
	}
	if _, ok := allowedVectorBucketDistances[distance]; !ok {
		return ProjectVectorBucket{}, fmt.Errorf("unsupported vector distance %q", distance)
	}
	indexMethod := strings.ToLower(strings.TrimSpace(input.IndexMethod))
	if indexMethod == "" {
		indexMethod = "hnsw"
	}
	if _, ok := allowedVectorBucketIndexes[indexMethod]; !ok {
		return ProjectVectorBucket{}, fmt.Errorf("unsupported vector index method %q", indexMethod)
	}
	backend := strings.ToLower(strings.TrimSpace(input.StorageBackend))
	if backend == "" {
		backend = "postgres"
	}
	if _, ok := allowedVectorBucketBackends[backend]; !ok {
		return ProjectVectorBucket{}, fmt.Errorf("unsupported vector bucket backend %q", backend)
	}
	storageURI := strings.TrimSpace(input.StorageURI)
	if backend == "s3" && storageURI == "" {
		return ProjectVectorBucket{}, fmt.Errorf("s3 vector bucket requires storage_uri")
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectVectorBucket{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectVectorBucket{}, err
	}
	now := time.Now().UTC()
	return ProjectVectorBucket{
		ID:             newID(),
		ProjectRef:     ref,
		Name:           name,
		Dimension:      dimension,
		Distance:       distance,
		IndexMethod:    indexMethod,
		StorageBackend: backend,
		StorageURI:     storageURI,
		Metadata:       metadata,
		Status:         "configured",
		Message:        "vector bucket declaration recorded",
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func normalizeProjectAnalyticsBucket(ref string, input ProjectAnalyticsBucketInput) (ProjectAnalyticsBucket, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectAnalyticsBucket{}, fmt.Errorf("analytics bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}
	storageURI := strings.TrimSpace(input.StorageURI)
	if storageURI == "" {
		return ProjectAnalyticsBucket{}, fmt.Errorf("storage_uri is required")
	}
	if err := validateAnalyticsStorageURI(storageURI); err != nil {
		return ProjectAnalyticsBucket{}, err
	}
	catalogURI := strings.TrimSpace(input.CatalogURI)
	if catalogURI != "" {
		parsed, err := url.Parse(catalogURI)
		if err != nil || parsed.Scheme == "" {
			return ProjectAnalyticsBucket{}, fmt.Errorf("catalog_uri is invalid")
		}
	}
	warehouse := strings.TrimSpace(input.Warehouse)
	if warehouse == "" {
		warehouse = name
	}
	if strings.ContainsAny(warehouse, "\r\n\t") || len(warehouse) > 128 {
		return ProjectAnalyticsBucket{}, fmt.Errorf("warehouse is invalid")
	}
	credentialHandle := strings.TrimSpace(input.CredentialHandle)
	if credentialHandle != "" && !strings.HasPrefix(credentialHandle, "secret://") {
		return ProjectAnalyticsBucket{}, fmt.Errorf("credential_handle must be a secret:// handle")
	}
	formatVersion := input.FormatVersion
	if formatVersion == 0 {
		formatVersion = 2
	}
	if formatVersion != 1 && formatVersion != 2 {
		return ProjectAnalyticsBucket{}, fmt.Errorf("format_version must be 1 or 2")
	}
	partitioning := strings.TrimSpace(input.Partitioning)
	if strings.ContainsAny(partitioning, "\r\n") || len(partitioning) > 256 {
		return ProjectAnalyticsBucket{}, fmt.Errorf("partitioning is invalid")
	}
	retentionDays := input.RetentionDays
	if retentionDays < 0 || retentionDays > 3650 {
		return ProjectAnalyticsBucket{}, fmt.Errorf("retention_days must be between 0 and 3650")
	}
	compactionSchedule := strings.TrimSpace(input.CompactionSchedule)
	if compactionSchedule == "" {
		compactionSchedule = "manual"
	}
	if strings.ContainsAny(compactionSchedule, "\r\n\t") || len(compactionSchedule) > 128 {
		return ProjectAnalyticsBucket{}, fmt.Errorf("compaction_schedule is invalid")
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectAnalyticsBucket{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectAnalyticsBucket{}, err
	}
	now := time.Now().UTC()
	return ProjectAnalyticsBucket{
		ID:                 newID(),
		ProjectRef:         ref,
		Name:               name,
		StorageURI:         storageURI,
		CatalogURI:         catalogURI,
		Warehouse:          warehouse,
		CredentialHandle:   credentialHandle,
		FormatVersion:      formatVersion,
		Partitioning:       partitioning,
		RetentionDays:      retentionDays,
		CompactionSchedule: compactionSchedule,
		Metadata:           metadata,
		Status:             "configured",
		Message:            "Iceberg analytics bucket declaration recorded",
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func validateAnalyticsStorageURI(storageURI string) error {
	parsed, err := url.Parse(storageURI)
	if err != nil || parsed.Scheme == "" {
		return fmt.Errorf("storage_uri is invalid")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "s3", "gs", "az", "file":
		return nil
	default:
		return fmt.Errorf("storage_uri must use s3://, gs://, az://, or file://")
	}
}

func normalizeProjectAuthHook(ref string, input ProjectAuthHookInput) (ProjectAuthHook, error) {
	hookType := strings.ToLower(strings.TrimSpace(input.HookType))
	if hookType == "" {
		return ProjectAuthHook{}, fmt.Errorf("hook_type is required")
	}
	if _, ok := allowedAuthHookTypes[hookType]; !ok {
		return ProjectAuthHook{}, fmt.Errorf("unsupported auth hook_type %q", hookType)
	}
	targetURI := strings.TrimSpace(input.TargetURI)
	edgeFunction := strings.TrimSpace(input.EdgeFunction)
	if edgeFunction != "" {
		normalized, err := normalizeFunctionName(edgeFunction)
		if err != nil {
			return ProjectAuthHook{}, err
		}
		edgeFunction = normalized
	}
	if input.Enabled && targetURI == "" && edgeFunction == "" {
		return ProjectAuthHook{}, fmt.Errorf("enabled auth hooks require target_uri or edge_function")
	}
	if targetURI != "" {
		parsed, err := url.Parse(targetURI)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return ProjectAuthHook{}, fmt.Errorf("target_uri is invalid")
		}
		if parsed.Scheme != "https" && parsed.Scheme != "http" {
			return ProjectAuthHook{}, fmt.Errorf("target_uri must use http or https")
		}
		if len(targetURI) > 512 || strings.ContainsAny(targetURI, "\r\n\t ") {
			return ProjectAuthHook{}, fmt.Errorf("target_uri is invalid")
		}
	}
	secretHandle := strings.TrimSpace(input.SecretHandle)
	if secretHandle != "" && !strings.HasPrefix(secretHandle, "secret://") {
		return ProjectAuthHook{}, fmt.Errorf("secret_handle must use a secret:// handle")
	}
	headers, err := normalizeConfigValues(input.Headers)
	if err != nil {
		return ProjectAuthHook{}, err
	}
	for key, value := range headers {
		if value == "" {
			continue
		}
		if isSensitiveAuthHookHeaderKey(key) && !strings.HasPrefix(value, "secret://") {
			return ProjectAuthHook{}, fmt.Errorf("auth hook header %s must use a secret:// handle", key)
		}
		if len(value) > 512 || strings.ContainsAny(value, "\r\n") {
			return ProjectAuthHook{}, fmt.Errorf("auth hook header %s is invalid", key)
		}
	}
	timeoutMS := input.TimeoutMS
	if timeoutMS == 0 {
		timeoutMS = 5000
	}
	if timeoutMS < 100 || timeoutMS > 30000 {
		return ProjectAuthHook{}, fmt.Errorf("timeout_ms must be between 100 and 30000")
	}
	retryAttempts := input.RetryAttempts
	if retryAttempts < 0 || retryAttempts > 5 {
		return ProjectAuthHook{}, fmt.Errorf("retry_attempts must be between 0 and 5")
	}
	now := time.Now().UTC()
	status := "disabled"
	message := "Auth hook declaration recorded"
	if input.Enabled {
		status = "configured"
		message = "Auth hook declaration ready for runtime sync"
	}
	return ProjectAuthHook{
		ID:            newID(),
		ProjectRef:    ref,
		HookType:      hookType,
		Enabled:       input.Enabled,
		TargetURI:     targetURI,
		EdgeFunction:  edgeFunction,
		SecretHandle:  secretHandle,
		Headers:       headers,
		TimeoutMS:     timeoutMS,
		RetryAttempts: retryAttempts,
		Status:        status,
		Message:       message,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func isSensitiveAuthHookHeaderKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "proxy-authorization", "x-api-key", "x-auth-token":
		return true
	default:
		return isSensitiveProjectConfigKey(key)
	}
}

func normalizeProjectAuthClient(ref string, input ProjectAuthClientInput) (ProjectAuthClient, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return ProjectAuthClient{}, fmt.Errorf("auth client name is required")
	}
	if len(name) > 96 || strings.ContainsAny(name, "\r\n\t") {
		return ProjectAuthClient{}, fmt.Errorf("auth client name is invalid")
	}
	clientID := strings.TrimSpace(input.ClientID)
	if clientID == "" {
		clientID = "oauth_" + newID()
	}
	if len(clientID) > 128 || strings.ContainsAny(clientID, "\r\n\t ") {
		return ProjectAuthClient{}, fmt.Errorf("client_id is invalid")
	}
	secretHandle := strings.TrimSpace(input.ClientSecretHandle)
	if input.Confidential && secretHandle == "" {
		return ProjectAuthClient{}, fmt.Errorf("confidential auth clients require client_secret_handle")
	}
	if secretHandle != "" && !strings.HasPrefix(secretHandle, "secret://") {
		return ProjectAuthClient{}, fmt.Errorf("client_secret_handle must use a secret:// handle")
	}
	redirectURIs, err := normalizeOAuthRedirectURIs(input.RedirectURIs)
	if err != nil {
		return ProjectAuthClient{}, err
	}
	grantTypes, err := normalizeOAuthValues("grant_type", input.GrantTypes, allowedOAuthClientGrantTypes, []string{"authorization_code", "refresh_token"})
	if err != nil {
		return ProjectAuthClient{}, err
	}
	scopes, err := normalizeOAuthValues("scope", input.Scopes, allowedOAuthClientScopes, []string{"openid", "profile", "email"})
	if err != nil {
		return ProjectAuthClient{}, err
	}
	now := time.Now().UTC()
	return ProjectAuthClient{
		ID:                 newID(),
		ProjectRef:         ref,
		Name:               name,
		ClientID:           clientID,
		ClientSecretHandle: secretHandle,
		RedirectURIs:       redirectURIs,
		GrantTypes:         grantTypes,
		Scopes:             scopes,
		Confidential:       input.Confidential,
		Status:             "registered",
		Message:            "OAuth 2.1 client registration recorded",
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func normalizeOAuthRedirectURIs(values []string) ([]string, error) {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("redirect_uri %q is invalid", trimmed)
		}
		if parsed.Scheme != "https" && parsed.Scheme != "http" {
			return nil, fmt.Errorf("redirect_uri %q must use http or https", trimmed)
		}
		if len(trimmed) > 512 || strings.ContainsAny(trimmed, "\r\n\t ") {
			return nil, fmt.Errorf("redirect_uri %q is invalid", trimmed)
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one redirect_uri is required")
	}
	sort.Strings(out)
	return out, nil
}

func normalizeOAuthValues(label string, values []string, allowed map[string]struct{}, defaults []string) ([]string, error) {
	if len(values) == 0 {
		values = defaults
	}
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			normalized := strings.ToLower(strings.TrimSpace(part))
			if normalized == "" {
				continue
			}
			if _, ok := allowed[normalized]; !ok {
				return nil, fmt.Errorf("unsupported OAuth %s %q", label, normalized)
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	sort.Strings(out)
	return out, nil
}

func normalizeProjectDatabaseExtension(ref string, name string, input ProjectDatabaseExtensionInput) (ProjectDatabaseExtension, error) {
	normalizedName := strings.ToLower(strings.TrimSpace(name))
	if !extensionNamePattern.MatchString(normalizedName) {
		return ProjectDatabaseExtension{}, fmt.Errorf("database extension name must be a valid extension identifier")
	}
	base, ok := defaultDatabaseExtensions[normalizedName]
	if !ok {
		return ProjectDatabaseExtension{}, fmt.Errorf("unsupported database extension %q", normalizedName)
	}
	schema := strings.ToLower(strings.TrimSpace(input.Schema))
	if schema == "" {
		schema = base.Schema
	}
	if err := validateDatabaseIdentifier("extension schema", schema); err != nil {
		return ProjectDatabaseExtension{}, err
	}
	version := strings.TrimSpace(input.Version)
	if strings.ContainsAny(version, "\r\n\t ") || len(version) > 64 {
		return ProjectDatabaseExtension{}, fmt.Errorf("extension version is invalid")
	}
	enabled := base.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	status := "disabled"
	message := "database extension disabled"
	if enabled {
		status = "enabled"
		message = "database extension enabled"
	}
	now := time.Now().UTC()
	return ProjectDatabaseExtension{
		ID:         newID(),
		ProjectRef: ref,
		Name:       normalizedName,
		Schema:     schema,
		Version:    version,
		Enabled:    enabled,
		Status:     status,
		Message:    message,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func normalizeProjectDatabaseCronJob(ref string, input ProjectDatabaseCronJobInput) (ProjectDatabaseCronJob, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectDatabaseCronJob{}, fmt.Errorf("database cron job name must be 3-64 lowercase letters, numbers, or dashes")
	}
	schedule := strings.TrimSpace(input.Schedule)
	if err := validateCronSchedule(schedule); err != nil {
		return ProjectDatabaseCronJob{}, err
	}
	command := strings.TrimSpace(input.Command)
	if command == "" {
		return ProjectDatabaseCronJob{}, fmt.Errorf("database cron command is required")
	}
	if len(command) > 8192 || strings.ContainsRune(command, 0) {
		return ProjectDatabaseCronJob{}, fmt.Errorf("database cron command is invalid")
	}
	database := strings.ToLower(strings.TrimSpace(input.Database))
	if database == "" {
		database = "postgres"
	}
	if err := validateDatabaseIdentifier("cron database", database); err != nil {
		return ProjectDatabaseCronJob{}, err
	}
	username := strings.ToLower(strings.TrimSpace(input.Username))
	if username == "" {
		username = "postgres"
	}
	if err := validateDatabaseIdentifier("cron username", username); err != nil {
		return ProjectDatabaseCronJob{}, err
	}
	timeoutSeconds := input.TimeoutSeconds
	if timeoutSeconds == 0 {
		timeoutSeconds = 60
	}
	if timeoutSeconds < 1 || timeoutSeconds > 86400 {
		return ProjectDatabaseCronJob{}, fmt.Errorf("timeout_seconds must be between 1 and 86400")
	}
	maxRuntimeSeconds := input.MaxRuntimeSeconds
	if maxRuntimeSeconds == 0 {
		maxRuntimeSeconds = timeoutSeconds
	}
	if maxRuntimeSeconds < 1 || maxRuntimeSeconds > 86400 {
		return ProjectDatabaseCronJob{}, fmt.Errorf("max_runtime_seconds must be between 1 and 86400")
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectDatabaseCronJob{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectDatabaseCronJob{}, err
	}
	status := "paused"
	message := "pg_cron job declaration paused"
	if input.Active {
		status = "scheduled"
		message = "pg_cron job declaration ready for runtime sync"
	}
	now := time.Now().UTC()
	return ProjectDatabaseCronJob{
		ID:                newID(),
		ProjectRef:        ref,
		Name:              name,
		Schedule:          schedule,
		Command:           command,
		Database:          database,
		Username:          username,
		Active:            input.Active,
		TimeoutSeconds:    timeoutSeconds,
		MaxRuntimeSeconds: maxRuntimeSeconds,
		Metadata:          metadata,
		Status:            status,
		Message:           message,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

func validateCronSchedule(schedule string) error {
	if schedule == "" {
		return fmt.Errorf("cron schedule is required")
	}
	if len(schedule) > 128 || strings.ContainsAny(schedule, "\r\n\t") {
		return fmt.Errorf("cron schedule is invalid")
	}
	switch strings.ToLower(schedule) {
	case "@hourly", "@daily", "@weekly", "@monthly", "@yearly", "@annually", "@reboot":
		return nil
	}
	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return fmt.Errorf("cron schedule must be a five-field expression or supported @schedule")
	}
	for _, part := range parts {
		if len(part) > 32 || strings.ContainsAny(part, ";'\"`\\") {
			return fmt.Errorf("cron schedule field %q is invalid", part)
		}
	}
	return nil
}

func normalizeProjectDatabaseQueue(ref string, input ProjectDatabaseQueueInput) (ProjectDatabaseQueue, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectDatabaseQueue{}, fmt.Errorf("database queue name must be 3-64 lowercase letters, numbers, or dashes")
	}
	schema := strings.ToLower(strings.TrimSpace(input.Schema))
	if schema == "" {
		schema = "pgmq"
	}
	if err := validateDatabaseIdentifier("queue schema", schema); err != nil {
		return ProjectDatabaseQueue{}, err
	}
	retentionMinutes := input.RetentionMinutes
	if retentionMinutes == 0 {
		retentionMinutes = 1440
	}
	if retentionMinutes < 1 || retentionMinutes > 525600 {
		return ProjectDatabaseQueue{}, fmt.Errorf("retention_minutes must be between 1 and 525600")
	}
	visibilityTimeoutSeconds := input.VisibilityTimeoutSeconds
	if visibilityTimeoutSeconds == 0 {
		visibilityTimeoutSeconds = 30
	}
	if visibilityTimeoutSeconds < 1 || visibilityTimeoutSeconds > 86400 {
		return ProjectDatabaseQueue{}, fmt.Errorf("visibility_timeout_seconds must be between 1 and 86400")
	}
	maxRetries := input.MaxRetries
	if maxRetries == 0 {
		maxRetries = 5
	}
	if maxRetries < 0 || maxRetries > 1000 {
		return ProjectDatabaseQueue{}, fmt.Errorf("max_retries must be between 0 and 1000")
	}
	deadLetterQueue := strings.ToLower(strings.TrimSpace(input.DeadLetterQueue))
	if deadLetterQueue != "" {
		if !refPattern.MatchString(deadLetterQueue) {
			return ProjectDatabaseQueue{}, fmt.Errorf("dead_letter_queue must be 3-64 lowercase letters, numbers, or dashes")
		}
		if deadLetterQueue == name {
			return ProjectDatabaseQueue{}, fmt.Errorf("dead_letter_queue must be different from name")
		}
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectDatabaseQueue{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectDatabaseQueue{}, err
	}
	status := "paused"
	message := "pgmq queue declaration paused"
	if input.Active {
		status = "ready"
		message = "pgmq queue declaration ready for runtime sync"
	}
	now := time.Now().UTC()
	return ProjectDatabaseQueue{
		ID:                       newID(),
		ProjectRef:               ref,
		Name:                     name,
		Schema:                   schema,
		RetentionMinutes:         retentionMinutes,
		VisibilityTimeoutSeconds: visibilityTimeoutSeconds,
		MaxRetries:               maxRetries,
		DeadLetterQueue:          deadLetterQueue,
		Active:                   input.Active,
		Metadata:                 metadata,
		Status:                   status,
		Message:                  message,
		CreatedAt:                now,
		UpdatedAt:                now,
	}, nil
}

func normalizeProjectDatabaseWebhook(ref string, input ProjectDatabaseWebhookInput) (ProjectDatabaseWebhook, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectDatabaseWebhook{}, fmt.Errorf("database webhook name must be 3-64 lowercase letters, numbers, or dashes")
	}
	schema := strings.ToLower(strings.TrimSpace(input.Schema))
	if schema == "" {
		schema = "public"
	}
	if err := validateDatabaseIdentifier("webhook schema", schema); err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	table := strings.ToLower(strings.TrimSpace(input.Table))
	if err := validateDatabaseIdentifier("webhook table", table); err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	events, err := normalizeDatabaseWebhookEvents(input.Events)
	if err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	endpoint := strings.TrimSpace(input.Endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return ProjectDatabaseWebhook{}, fmt.Errorf("webhook endpoint must be an https URL")
	}
	if len(endpoint) > 2048 || strings.ContainsAny(endpoint, "\r\n\t") {
		return ProjectDatabaseWebhook{}, fmt.Errorf("webhook endpoint is invalid")
	}
	method := strings.ToUpper(strings.TrimSpace(input.HTTPMethod))
	if method == "" {
		method = "POST"
	}
	switch method {
	case "POST", "PUT", "PATCH":
	default:
		return ProjectDatabaseWebhook{}, fmt.Errorf("webhook http_method must be POST, PUT, or PATCH")
	}
	headers, err := normalizeConfigValues(input.Headers)
	if err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	if err := validateReplicationSecretHandles(headers); err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	timeoutSeconds := input.TimeoutSeconds
	if timeoutSeconds == 0 {
		timeoutSeconds = 10
	}
	if timeoutSeconds < 1 || timeoutSeconds > 300 {
		return ProjectDatabaseWebhook{}, fmt.Errorf("timeout_seconds must be between 1 and 300")
	}
	retryCount := input.RetryCount
	if retryCount == 0 {
		retryCount = 3
	}
	if retryCount < 0 || retryCount > 25 {
		return ProjectDatabaseWebhook{}, fmt.Errorf("retry_count must be between 0 and 25")
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	status := "paused"
	message := "database webhook declaration paused"
	if input.Active {
		status = "ready"
		message = "database webhook declaration ready for runtime sync"
	}
	now := time.Now().UTC()
	return ProjectDatabaseWebhook{
		ID:             newID(),
		ProjectRef:     ref,
		Name:           name,
		Schema:         schema,
		Table:          table,
		Events:         events,
		Endpoint:       endpoint,
		HTTPMethod:     method,
		Headers:        headers,
		TimeoutSeconds: timeoutSeconds,
		RetryCount:     retryCount,
		Active:         input.Active,
		Metadata:       metadata,
		Status:         status,
		Message:        message,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func normalizeDatabaseWebhookEvents(input []string) ([]string, error) {
	if len(input) == 0 {
		input = []string{"insert", "update", "delete"}
	}
	allowed := map[string]struct{}{"insert": {}, "update": {}, "delete": {}}
	seen := map[string]struct{}{}
	out := []string{}
	for _, event := range input {
		normalized := strings.ToLower(strings.TrimSpace(event))
		if _, ok := allowed[normalized]; !ok {
			return nil, fmt.Errorf("webhook event %q is not supported", event)
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("webhook events are required")
	}
	sort.Strings(out)
	return out, nil
}

func normalizeProjectDatabaseSchema(ref string, input ProjectDatabaseSchemaInput) (ProjectDatabaseSchema, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectDatabaseSchema{}, fmt.Errorf("database schema name must be 3-64 lowercase letters, numbers, or dashes")
	}
	version := strings.TrimSpace(input.Version)
	if version == "" {
		return ProjectDatabaseSchema{}, fmt.Errorf("database schema version is required")
	}
	if len(version) > 128 || strings.ContainsAny(version, "\r\n\t ") {
		return ProjectDatabaseSchema{}, fmt.Errorf("database schema version is invalid")
	}
	schemaName := strings.ToLower(strings.TrimSpace(input.Schema))
	if schemaName == "" {
		schemaName = "public"
	}
	if err := validateDatabaseIdentifier("database schema", schemaName); err != nil {
		return ProjectDatabaseSchema{}, err
	}
	sql := strings.TrimSpace(input.SQL)
	if sql == "" {
		return ProjectDatabaseSchema{}, fmt.Errorf("database schema sql is required")
	}
	if len(sql) > 262144 || strings.ContainsRune(sql, 0) {
		return ProjectDatabaseSchema{}, fmt.Errorf("database schema sql is invalid")
	}
	applyOrder := input.ApplyOrder
	if applyOrder < 0 || applyOrder > 1000000 {
		return ProjectDatabaseSchema{}, fmt.Errorf("apply_order must be between 0 and 1000000")
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectDatabaseSchema{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectDatabaseSchema{}, err
	}
	sum := sha256.Sum256([]byte(sql))
	status := "paused"
	message := "declarative schema migration paused"
	if input.Active {
		status = "pending"
		message = "declarative schema migration ready for runtime sync"
	}
	now := time.Now().UTC()
	return ProjectDatabaseSchema{
		ID:         newID(),
		ProjectRef: ref,
		Name:       name,
		Version:    version,
		Schema:     schemaName,
		SQL:        sql,
		Checksum:   hex.EncodeToString(sum[:]),
		ApplyOrder: applyOrder,
		Active:     input.Active,
		Metadata:   metadata,
		Status:     status,
		Message:    message,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func mergedDatabaseExtensions(ref string, overrides []ProjectDatabaseExtension) []ProjectDatabaseExtension {
	now := time.Now().UTC()
	byName := map[string]ProjectDatabaseExtension{}
	for _, name := range defaultDatabaseExtensionOrder {
		base := defaultDatabaseExtensions[name]
		base.ID = "default:" + name
		base.ProjectRef = ref
		base.CreatedAt = now
		base.UpdatedAt = now
		byName[name] = base
	}
	for _, override := range overrides {
		override.ProjectRef = ref
		if strings.TrimSpace(override.Status) == "" {
			if override.Enabled {
				override.Status = "enabled"
			} else {
				override.Status = "disabled"
			}
		}
		byName[override.Name] = override
	}
	extensions := make([]ProjectDatabaseExtension, 0, len(byName))
	seen := map[string]struct{}{}
	for _, name := range defaultDatabaseExtensionOrder {
		if extension, ok := byName[name]; ok {
			extensions = append(extensions, extension)
			seen[name] = struct{}{}
		}
	}
	for name, extension := range byName {
		if _, ok := seen[name]; ok {
			continue
		}
		extensions = append(extensions, extension)
	}
	sort.SliceStable(extensions, func(i, j int) bool {
		leftDefault := indexOfDefaultDatabaseExtension(extensions[i].Name)
		rightDefault := indexOfDefaultDatabaseExtension(extensions[j].Name)
		if leftDefault >= 0 && rightDefault >= 0 {
			return leftDefault < rightDefault
		}
		if leftDefault >= 0 {
			return true
		}
		if rightDefault >= 0 {
			return false
		}
		return extensions[i].Name < extensions[j].Name
	})
	return extensions
}

func indexOfDefaultDatabaseExtension(name string) int {
	for index, candidate := range defaultDatabaseExtensionOrder {
		if candidate == name {
			return index
		}
	}
	return -1
}

func countEnabledDatabaseExtensions(ref string, overrides []ProjectDatabaseExtension) int {
	count := 0
	for _, extension := range mergedDatabaseExtensions(ref, overrides) {
		if extension.Enabled {
			count++
		}
	}
	return count
}

func normalizeProjectDatabaseRole(ref string, input ProjectDatabaseRoleInput) (ProjectDatabaseRole, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if err := validateDatabaseIdentifier("database role name", name); err != nil {
		return ProjectDatabaseRole{}, err
	}
	if _, reserved := reservedDatabaseRoles[name]; reserved || strings.HasPrefix(name, "pg_") {
		return ProjectDatabaseRole{}, fmt.Errorf("database role %q is reserved by the upstream stack", name)
	}
	inherit := true
	if input.Inherit != nil {
		inherit = *input.Inherit
	}
	connectionLimit := input.ConnectionLimit
	if connectionLimit < -1 {
		return ProjectDatabaseRole{}, fmt.Errorf("connection_limit must be -1 or greater")
	}
	passwordHandle := strings.TrimSpace(input.PasswordSecretHandle)
	if input.Login && passwordHandle == "" {
		return ProjectDatabaseRole{}, fmt.Errorf("login database roles require password_secret_handle")
	}
	if passwordHandle != "" && !strings.HasPrefix(passwordHandle, "secret://") {
		return ProjectDatabaseRole{}, fmt.Errorf("password_secret_handle must use a secret:// handle")
	}
	memberOf, err := normalizeDatabaseRoleMembers(input.MemberOf, name)
	if err != nil {
		return ProjectDatabaseRole{}, err
	}
	schemaGrants, err := normalizeDatabaseSchemaGrants(input.SchemaGrants)
	if err != nil {
		return ProjectDatabaseRole{}, err
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectDatabaseRole{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectDatabaseRole{}, err
	}
	now := time.Now().UTC()
	return ProjectDatabaseRole{
		ID:                   newID(),
		ProjectRef:           ref,
		Name:                 name,
		Login:                input.Login,
		Inherit:              inherit,
		BypassRLS:            input.BypassRLS,
		ConnectionLimit:      connectionLimit,
		PasswordSecretHandle: passwordHandle,
		MemberOf:             memberOf,
		SchemaGrants:         schemaGrants,
		Metadata:             metadata,
		Status:               "configured",
		Message:              "database role declaration recorded",
		CreatedAt:            now,
		UpdatedAt:            now,
	}, nil
}

func validateDatabaseIdentifier(label string, value string) error {
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("%s must be a valid PostgreSQL identifier", label)
	}
	return nil
}

func normalizeDatabaseRoleMembers(values []string, roleName string) ([]string, error) {
	members := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		member := strings.ToLower(strings.TrimSpace(value))
		if member == "" {
			continue
		}
		if err := validateDatabaseIdentifier("member role", member); err != nil {
			return nil, err
		}
		if member == roleName {
			return nil, fmt.Errorf("database role cannot be member of itself")
		}
		if _, ok := seen[member]; ok {
			continue
		}
		seen[member] = struct{}{}
		members = append(members, member)
	}
	sort.Strings(members)
	return members, nil
}

func normalizeDatabaseSchemaGrants(values map[string]string) (map[string]string, error) {
	grants := map[string]string{}
	for schema, privilegeList := range values {
		normalizedSchema := strings.ToLower(strings.TrimSpace(schema))
		if normalizedSchema == "" {
			continue
		}
		if err := validateDatabaseIdentifier("schema grant name", normalizedSchema); err != nil {
			return nil, err
		}
		privileges := []string{}
		seen := map[string]struct{}{}
		for _, privilege := range strings.Split(privilegeList, ",") {
			normalizedPrivilege := strings.ToLower(strings.TrimSpace(privilege))
			if normalizedPrivilege == "" {
				continue
			}
			if _, ok := allowedDatabaseRolePrivileges[normalizedPrivilege]; !ok {
				return nil, fmt.Errorf("unsupported database role privilege %q", normalizedPrivilege)
			}
			if _, ok := seen[normalizedPrivilege]; ok {
				continue
			}
			seen[normalizedPrivilege] = struct{}{}
			privileges = append(privileges, normalizedPrivilege)
		}
		sort.Strings(privileges)
		if len(privileges) > 0 {
			grants[normalizedSchema] = strings.Join(privileges, ",")
		}
	}
	return grants, nil
}

func normalizeProjectStorageBucket(ref string, input ProjectStorageBucketInput) (ProjectStorageBucket, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if !refPattern.MatchString(name) {
		return ProjectStorageBucket{}, fmt.Errorf("storage bucket name must be 3-64 lowercase letters, numbers, or dashes")
	}
	fileSizeLimit := input.FileSizeLimit
	if fileSizeLimit < 0 {
		return ProjectStorageBucket{}, fmt.Errorf("file_size_limit cannot be negative")
	}
	if fileSizeLimit == 0 {
		fileSizeLimit = 50 * 1024 * 1024
	}
	mimeTypes := make([]string, 0, len(input.AllowedMimeTypes))
	seen := map[string]struct{}{}
	for _, value := range input.AllowedMimeTypes {
		mimeType := strings.ToLower(strings.TrimSpace(value))
		if mimeType == "" {
			continue
		}
		if len(mimeType) > 128 || strings.ContainsAny(mimeType, "\r\n\t ") || !strings.Contains(mimeType, "/") {
			return ProjectStorageBucket{}, fmt.Errorf("allowed_mime_types contains invalid value %q", value)
		}
		if _, ok := seen[mimeType]; ok {
			continue
		}
		seen[mimeType] = struct{}{}
		mimeTypes = append(mimeTypes, mimeType)
	}
	sort.Strings(mimeTypes)
	cacheControl := strings.TrimSpace(input.CacheControl)
	if cacheControl == "" {
		cacheControl = "3600"
	}
	if strings.ContainsAny(cacheControl, "\r\n") || len(cacheControl) > 128 {
		return ProjectStorageBucket{}, fmt.Errorf("cache_control is invalid")
	}
	metadata, err := normalizeConfigValues(input.Metadata)
	if err != nil {
		return ProjectStorageBucket{}, err
	}
	if err := validateReplicationSecretHandles(metadata); err != nil {
		return ProjectStorageBucket{}, err
	}
	now := time.Now().UTC()
	return ProjectStorageBucket{
		ID:                newID(),
		ProjectRef:        ref,
		Name:              name,
		Public:            input.Public,
		FileSizeLimit:     fileSizeLimit,
		AllowedMimeTypes:  mimeTypes,
		CacheControl:      cacheControl,
		AvifAutodetection: input.AvifAutodetection,
		Metadata:          metadata,
		Status:            "configured",
		Message:           "storage bucket declaration recorded",
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

func normalizeProjectNetworkConnection(ref string, input ProjectNetworkConnectionInput) (ProjectNetworkConnection, error) {
	name := strings.ToLower(strings.TrimSpace(input.Name))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(input.Provider)) + "-" + strings.ToLower(strings.TrimSpace(input.Type))
	}
	if !refPattern.MatchString(name) {
		return ProjectNetworkConnection{}, fmt.Errorf("network connection name must be 3-64 lowercase letters, numbers, or dashes")
	}
	connectionType := strings.ToLower(strings.TrimSpace(input.Type))
	if connectionType == "" {
		connectionType = "operator_network"
	}
	if _, ok := allowedNetworkConnectionTypes[connectionType]; !ok {
		return ProjectNetworkConnection{}, fmt.Errorf("unsupported network connection type %q", connectionType)
	}
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	if provider == "" {
		provider = "operator"
	}
	if _, ok := allowedNetworkConnectionProviders[provider]; !ok {
		return ProjectNetworkConnection{}, fmt.Errorf("unsupported network connection provider %q", provider)
	}
	cidrs, err := normalizeNetworkCIDRs(input.CIDRs)
	if err != nil {
		return ProjectNetworkConnection{}, err
	}
	config, err := normalizeConfigValues(input.Config)
	if err != nil {
		return ProjectNetworkConnection{}, err
	}
	if err := validateReplicationSecretHandles(config); err != nil {
		return ProjectNetworkConnection{}, err
	}
	region := strings.ToLower(strings.TrimSpace(input.Region))
	if len(region) > 64 || strings.ContainsAny(region, " \r\n\t") {
		return ProjectNetworkConnection{}, fmt.Errorf("region is invalid")
	}
	endpointID := strings.TrimSpace(input.EndpointID)
	if len(endpointID) > 160 || strings.ContainsAny(endpointID, "\r\n\t") {
		return ProjectNetworkConnection{}, fmt.Errorf("endpoint_id is invalid")
	}
	now := time.Now().UTC()
	return ProjectNetworkConnection{
		ID:         newID(),
		ProjectRef: ref,
		Name:       name,
		Type:       connectionType,
		Provider:   provider,
		Region:     region,
		CIDRs:      cidrs,
		EndpointID: endpointID,
		Config:     config,
		Status:     "requested",
		Message:    "private network connection declaration recorded",
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func normalizeNetworkCIDRs(input []string) ([]string, error) {
	out := make([]string, 0, len(input))
	seen := map[string]struct{}{}
	for _, value := range input {
		for _, part := range strings.Split(value, ",") {
			normalized := strings.TrimSpace(part)
			if normalized == "" {
				continue
			}
			if prefix, err := netip.ParsePrefix(normalized); err == nil {
				normalized = prefix.String()
			} else if addr, err := netip.ParseAddr(normalized); err == nil {
				normalized = addr.String()
			} else {
				return nil, fmt.Errorf("invalid network cidr %q", part)
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one cidr is required")
	}
	return out, nil
}

func normalizeOptionalNetworkCIDRs(input []string) ([]string, error) {
	out, err := normalizeNetworkCIDRs(input)
	if err != nil {
		if strings.Contains(err.Error(), "at least one cidr is required") {
			return []string{}, nil
		}
		return nil, err
	}
	return out, nil
}

func isSensitiveProjectConfigKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "api_key", "token", "secret", "password", "access_key", "secret_key", "access_token", "bearer_token", "authorization", "x-api-key":
		return true
	default:
		return false
	}
}

func validateLogDrainConfig(target string, config map[string]string) error {
	switch target {
	case "https", "loki", "sentry", "axiom":
		if strings.TrimSpace(config["url"]) == "" {
			return fmt.Errorf("log drain %s target requires url", target)
		}
	case "datadog":
		if strings.TrimSpace(config["api_key"]) == "" {
			return fmt.Errorf("log drain datadog target requires api_key")
		}
	case "s3":
		if strings.TrimSpace(config["bucket"]) == "" {
			return fmt.Errorf("log drain s3 target requires bucket")
		}
	}
	return nil
}

func defaultProjectCDNPolicy(ref string) ProjectCDNPolicy {
	return ProjectCDNPolicy{
		ProjectRef:                  ref,
		Enabled:                     false,
		BrowserTTLSeconds:           3600,
		EdgeTTLSeconds:              3600,
		StaleWhileRevalidateSeconds: 60,
		IncludedPaths:               []string{"/storage/v1/object/public/*"},
		ExcludedPaths:               []string{},
		SmartRevalidation:           false,
		CacheControl:                "public, max-age=3600, s-maxage=3600, stale-while-revalidate=60",
		UpdatedAt:                   time.Now().UTC(),
	}
}

func normalizeProjectCDNPolicy(ref string, input ProjectCDNPolicyInput) (ProjectCDNPolicy, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	browserTTL := input.BrowserTTLSeconds
	if browserTTL == 0 {
		browserTTL = 3600
	}
	edgeTTL := input.EdgeTTLSeconds
	if edgeTTL == 0 {
		edgeTTL = browserTTL
	}
	staleTTL := input.StaleWhileRevalidateSeconds
	if staleTTL == 0 {
		staleTTL = 60
	}
	for name, value := range map[string]int{
		"browser_ttl_seconds":            browserTTL,
		"edge_ttl_seconds":               edgeTTL,
		"stale_while_revalidate_seconds": staleTTL,
	} {
		if value < 0 || value > 31536000 {
			return ProjectCDNPolicy{}, fmt.Errorf("%s must be between 0 and 31536000", name)
		}
	}
	included, err := normalizeCDNPaths(input.IncludedPaths, true)
	if err != nil {
		return ProjectCDNPolicy{}, fmt.Errorf("included_paths %w", err)
	}
	if len(included) == 0 {
		included = []string{"/storage/v1/object/public/*"}
	}
	excluded, err := normalizeCDNPaths(input.ExcludedPaths, true)
	if err != nil {
		return ProjectCDNPolicy{}, fmt.Errorf("excluded_paths %w", err)
	}
	cacheControl := strings.TrimSpace(input.CacheControl)
	if cacheControl == "" {
		cacheControl = fmt.Sprintf("public, max-age=%d, s-maxage=%d", browserTTL, edgeTTL)
		if staleTTL > 0 {
			cacheControl += fmt.Sprintf(", stale-while-revalidate=%d", staleTTL)
		}
	}
	if err := validateCacheControl(cacheControl); err != nil {
		return ProjectCDNPolicy{}, err
	}
	return ProjectCDNPolicy{
		ProjectRef:                  ref,
		Enabled:                     input.Enabled,
		BrowserTTLSeconds:           browserTTL,
		EdgeTTLSeconds:              edgeTTL,
		StaleWhileRevalidateSeconds: staleTTL,
		IncludedPaths:               included,
		ExcludedPaths:               excluded,
		SmartRevalidation:           input.SmartRevalidation,
		CacheControl:                cacheControl,
		UpdatedAt:                   time.Now().UTC(),
	}, nil
}

func normalizeCDNPaths(paths []string, allowEmpty bool) ([]string, error) {
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		normalized := strings.TrimSpace(path)
		if normalized == "" {
			continue
		}
		if !strings.HasPrefix(normalized, "/") {
			return nil, fmt.Errorf("path %q must start with /", path)
		}
		if strings.ContainsAny(normalized, "\r\n\t") || strings.Contains(normalized, "..") {
			return nil, fmt.Errorf("path %q is invalid", path)
		}
		if len(normalized) > 256 {
			return nil, fmt.Errorf("path %q is too long", path)
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 && !allowEmpty {
		return nil, fmt.Errorf("at least one path is required")
	}
	return out, nil
}

func normalizeCDNObjectEvent(input CDNObjectEventInput) (CDNObjectEventInput, error) {
	eventID := strings.TrimSpace(input.EventID)
	if len(eventID) > 160 || strings.ContainsAny(eventID, "\r\n\t") {
		return CDNObjectEventInput{}, fmt.Errorf("event_id is invalid")
	}
	eventType := strings.ToLower(strings.TrimSpace(input.EventType))
	if eventType == "" {
		eventType = "object_changed"
	}
	switch eventType {
	case "object_created", "object_updated", "object_deleted", "object_changed":
	default:
		return CDNObjectEventInput{}, fmt.Errorf("unsupported storage event_type %q", eventType)
	}
	bucket := strings.ToLower(strings.TrimSpace(input.Bucket))
	if bucket != "" && !refPattern.MatchString(bucket) {
		return CDNObjectEventInput{}, fmt.Errorf("bucket must be 3-64 lowercase letters, numbers, or dashes")
	}
	objectPath := strings.Trim(strings.TrimSpace(input.ObjectPath), "/")
	if objectPath == "" {
		return CDNObjectEventInput{}, fmt.Errorf("object_path is required")
	}
	if strings.ContainsAny(objectPath, "\r\n\t") || strings.Contains(objectPath, "..") {
		return CDNObjectEventInput{}, fmt.Errorf("object_path is invalid")
	}
	if len(objectPath) > 512 {
		return CDNObjectEventInput{}, fmt.Errorf("object_path is too long")
	}
	return CDNObjectEventInput{
		EventID:    eventID,
		Bucket:     bucket,
		ObjectPath: objectPath,
		EventType:  eventType,
	}, nil
}

func storageObjectCDNPath(bucket string, objectPath string) string {
	objectPath = strings.Trim(strings.TrimSpace(objectPath), "/")
	if bucket == "" {
		return "/storage/v1/object/public/" + objectPath
	}
	return "/storage/v1/object/public/" + bucket + "/" + objectPath
}

func cdnPathIncluded(path string, included []string, excluded []string) bool {
	for _, pattern := range excluded {
		if cdnPathPatternMatches(pattern, path) {
			return false
		}
	}
	for _, pattern := range included {
		if cdnPathPatternMatches(pattern, path) {
			return true
		}
	}
	return false
}

func cdnPathPatternMatches(pattern string, path string) bool {
	if pattern == path {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(path, strings.TrimSuffix(pattern, "*"))
	}
	return false
}

func validateCacheControl(value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("cache_control cannot contain line breaks")
	}
	if len(value) > 256 {
		return fmt.Errorf("cache_control is too long")
	}
	lower := strings.ToLower(value)
	if strings.Contains(lower, "private") || strings.Contains(lower, "no-store") {
		return fmt.Errorf("cache_control must describe a public edge-cacheable response")
	}
	return nil
}

func defaultProjectConfig(ref string, area string) ProjectConfig {
	return ProjectConfig{
		ProjectRef: ref,
		Area:       area,
		Config:     cloneStringMap(allowedConfigAreas[area]),
		UpdatedAt:  time.Now().UTC(),
	}
}

func mergeProjectConfigWithDefaults(ref string, area string, config ProjectConfig) ProjectConfig {
	merged := defaultProjectConfig(ref, area)
	merged.UpdatedAt = config.UpdatedAt
	for key, value := range config.Config {
		merged.Config[key] = value
	}
	return merged
}

func cloneProjectConfig(config ProjectConfig) ProjectConfig {
	config.Config = cloneStringMap(config.Config)
	return config
}

func cloneAuthClients(clients []ProjectAuthClient) []ProjectAuthClient {
	out := append([]ProjectAuthClient(nil), clients...)
	for index := range out {
		out[index] = cloneAuthClient(out[index])
	}
	return out
}

func cloneAuthClient(client ProjectAuthClient) ProjectAuthClient {
	client.RedirectURIs = append([]string(nil), client.RedirectURIs...)
	client.GrantTypes = append([]string(nil), client.GrantTypes...)
	client.Scopes = append([]string(nil), client.Scopes...)
	return client
}

func cloneAuthHooks(hooks []ProjectAuthHook) []ProjectAuthHook {
	out := append([]ProjectAuthHook(nil), hooks...)
	for index := range out {
		out[index] = cloneAuthHook(out[index])
	}
	return out
}

func cloneAuthHook(hook ProjectAuthHook) ProjectAuthHook {
	hook.Headers = cloneStringMap(hook.Headers)
	hook.RuntimeHeaders = cloneStringMap(hook.RuntimeHeaders)
	return hook
}

func cloneProjectRoutes(routes []ProjectRoute) []ProjectRoute {
	out := append([]ProjectRoute(nil), routes...)
	for index := range out {
		out[index].IPAllowlist = append([]string(nil), out[index].IPAllowlist...)
	}
	return out
}

func cloneProjectCDNPolicy(policy ProjectCDNPolicy) ProjectCDNPolicy {
	policy.IncludedPaths = append([]string{}, policy.IncludedPaths...)
	policy.ExcludedPaths = append([]string{}, policy.ExcludedPaths...)
	return policy
}

func cloneCDNInvalidations(invalidations []CDNInvalidation) []CDNInvalidation {
	out := append([]CDNInvalidation(nil), invalidations...)
	for index := range out {
		out[index] = cloneCDNInvalidation(out[index])
	}
	return out
}

func cloneCDNInvalidation(invalidation CDNInvalidation) CDNInvalidation {
	invalidation.Paths = append([]string(nil), invalidation.Paths...)
	if invalidation.Source == "" {
		invalidation.Source = "manual"
	}
	if invalidation.CompletedAt != nil {
		completed := *invalidation.CompletedAt
		invalidation.CompletedAt = &completed
	}
	return invalidation
}

func cloneNetworkConnections(connections []ProjectNetworkConnection) []ProjectNetworkConnection {
	out := append([]ProjectNetworkConnection(nil), connections...)
	for index := range out {
		out[index] = cloneNetworkConnection(out[index])
	}
	return out
}

func cloneNetworkConnection(connection ProjectNetworkConnection) ProjectNetworkConnection {
	connection.CIDRs = append([]string(nil), connection.CIDRs...)
	connection.Config = cloneStringMap(connection.Config)
	return connection
}

func cloneUsageSnapshots(snapshots []UsageSnapshot) []UsageSnapshot {
	out := append([]UsageSnapshot(nil), snapshots...)
	for index := range out {
		out[index] = cloneUsageSnapshot(out[index])
	}
	return out
}

func cloneUsageSnapshot(snapshot UsageSnapshot) UsageSnapshot {
	snapshot.Metrics = cloneOrgUsage(snapshot.Metrics)
	return snapshot
}

func cloneBillingInvoices(invoices []BillingInvoice) []BillingInvoice {
	out := append([]BillingInvoice(nil), invoices...)
	for index := range out {
		out[index] = cloneBillingInvoice(out[index])
	}
	return out
}

func cloneBillingInvoice(invoice BillingInvoice) BillingInvoice {
	invoice.LineItems = append([]BillingLineItem(nil), invoice.LineItems...)
	invoice.Metrics = cloneOrgUsage(invoice.Metrics)
	return invoice
}

func cloneOrgUsage(usage OrgUsage) OrgUsage {
	usage.ProjectsByStatus = cloneIntMap(usage.ProjectsByStatus)
	return usage
}

func cloneIntMap(input map[string]int) map[string]int {
	out := map[string]int{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneProjectFunctions(functions []ProjectFunction) []ProjectFunction {
	out := append([]ProjectFunction(nil), functions...)
	for index := range out {
		out[index] = cloneProjectFunction(out[index])
	}
	return out
}

func cloneProjectFunction(function ProjectFunction) ProjectFunction {
	function.Secrets = cloneStringMap(function.Secrets)
	return function
}

func cloneProjectFunctionRegions(regions []ProjectFunctionRegion) []ProjectFunctionRegion {
	return append([]ProjectFunctionRegion(nil), regions...)
}

func cloneProjectFunctionRegion(region ProjectFunctionRegion) ProjectFunctionRegion {
	return region
}

func cloneProjectFunctionStorageMounts(mounts []ProjectFunctionStorageMount) []ProjectFunctionStorageMount {
	return append([]ProjectFunctionStorageMount(nil), mounts...)
}

func cloneProjectFunctionStorageMount(mount ProjectFunctionStorageMount) ProjectFunctionStorageMount {
	return mount
}

func cloneReplicationPipelines(pipelines []ProjectReplicationPipeline) []ProjectReplicationPipeline {
	out := append([]ProjectReplicationPipeline(nil), pipelines...)
	for index := range out {
		out[index] = cloneReplicationPipeline(out[index])
	}
	return out
}

func cloneReplicationPipeline(pipeline ProjectReplicationPipeline) ProjectReplicationPipeline {
	pipeline.Config = cloneStringMap(pipeline.Config)
	return pipeline
}

func cloneEmbeddingJobs(jobs []ProjectEmbeddingJob) []ProjectEmbeddingJob {
	return append([]ProjectEmbeddingJob(nil), jobs...)
}

func cloneDatabaseExtensions(extensions []ProjectDatabaseExtension) []ProjectDatabaseExtension {
	return append([]ProjectDatabaseExtension(nil), extensions...)
}

func cloneDatabaseExtension(extension ProjectDatabaseExtension) ProjectDatabaseExtension {
	return extension
}

func cloneDatabaseCronJobs(jobs []ProjectDatabaseCronJob) []ProjectDatabaseCronJob {
	out := append([]ProjectDatabaseCronJob(nil), jobs...)
	for index := range out {
		out[index] = cloneDatabaseCronJob(out[index])
	}
	return out
}

func cloneDatabaseCronJob(job ProjectDatabaseCronJob) ProjectDatabaseCronJob {
	job.Metadata = cloneStringMap(job.Metadata)
	return job
}

func cloneDatabaseQueues(queues []ProjectDatabaseQueue) []ProjectDatabaseQueue {
	out := append([]ProjectDatabaseQueue(nil), queues...)
	for index := range out {
		out[index] = cloneDatabaseQueue(out[index])
	}
	return out
}

func cloneDatabaseQueue(queue ProjectDatabaseQueue) ProjectDatabaseQueue {
	queue.Metadata = cloneStringMap(queue.Metadata)
	return queue
}

func cloneDatabaseWebhooks(webhooks []ProjectDatabaseWebhook) []ProjectDatabaseWebhook {
	out := append([]ProjectDatabaseWebhook(nil), webhooks...)
	for index := range out {
		out[index] = cloneDatabaseWebhook(out[index])
	}
	return out
}

func cloneDatabaseWebhook(webhook ProjectDatabaseWebhook) ProjectDatabaseWebhook {
	webhook.Events = append([]string(nil), webhook.Events...)
	webhook.Headers = cloneStringMap(webhook.Headers)
	webhook.Metadata = cloneStringMap(webhook.Metadata)
	return webhook
}

func cloneDatabaseSchemas(schemas []ProjectDatabaseSchema) []ProjectDatabaseSchema {
	out := append([]ProjectDatabaseSchema(nil), schemas...)
	for index := range out {
		out[index] = cloneDatabaseSchema(out[index])
	}
	return out
}

func cloneDatabaseSchema(schema ProjectDatabaseSchema) ProjectDatabaseSchema {
	schema.Metadata = cloneStringMap(schema.Metadata)
	return schema
}

func cloneDatabaseRoles(roles []ProjectDatabaseRole) []ProjectDatabaseRole {
	out := append([]ProjectDatabaseRole(nil), roles...)
	for index := range out {
		out[index] = cloneDatabaseRole(out[index])
	}
	return out
}

func cloneDatabaseRole(role ProjectDatabaseRole) ProjectDatabaseRole {
	role.MemberOf = append([]string(nil), role.MemberOf...)
	role.SchemaGrants = cloneStringMap(role.SchemaGrants)
	role.Metadata = cloneStringMap(role.Metadata)
	return role
}

func cloneStorageBuckets(buckets []ProjectStorageBucket) []ProjectStorageBucket {
	out := append([]ProjectStorageBucket(nil), buckets...)
	for index := range out {
		out[index] = cloneStorageBucket(out[index])
	}
	return out
}

func cloneStorageBucket(bucket ProjectStorageBucket) ProjectStorageBucket {
	bucket.AllowedMimeTypes = append([]string(nil), bucket.AllowedMimeTypes...)
	bucket.Metadata = cloneStringMap(bucket.Metadata)
	return bucket
}

func cloneVectorBuckets(buckets []ProjectVectorBucket) []ProjectVectorBucket {
	out := append([]ProjectVectorBucket(nil), buckets...)
	for index := range out {
		out[index] = cloneVectorBucket(out[index])
	}
	return out
}

func cloneVectorBucket(bucket ProjectVectorBucket) ProjectVectorBucket {
	bucket.Metadata = cloneStringMap(bucket.Metadata)
	return bucket
}

func cloneAnalyticsBuckets(buckets []ProjectAnalyticsBucket) []ProjectAnalyticsBucket {
	out := append([]ProjectAnalyticsBucket(nil), buckets...)
	for index := range out {
		out[index] = cloneAnalyticsBucket(out[index])
	}
	return out
}

func cloneAnalyticsBucket(bucket ProjectAnalyticsBucket) ProjectAnalyticsBucket {
	bucket.Metadata = cloneStringMap(bucket.Metadata)
	return bucket
}

func cloneLogDrains(drains []LogDrain) []LogDrain {
	out := append([]LogDrain(nil), drains...)
	for index := range out {
		out[index] = cloneLogDrain(out[index])
	}
	return out
}

func cloneAndSortProjectChildList[T any](input []T, clone func([]T) []T, less func(T, T) bool) []T {
	out := clone(input)
	sort.Slice(out, func(i, j int) bool {
		return less(out[i], out[j])
	})
	if out == nil {
		return []T{}
	}
	return out
}

func maskFunctionSecrets(secrets map[string]string) map[string]string {
	masked := map[string]string{}
	for key, value := range secrets {
		if strings.TrimSpace(value) == "" {
			continue
		}
		masked[key] = maskSecret(value)
	}
	return masked
}

func cloneStringMap(input map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneBoolMap(input map[string]bool) map[string]bool {
	out := map[string]bool{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func serviceEnabledMap(input map[string]ServiceSpec) map[string]bool {
	return ProjectServiceStates(input)
}

func cloneLogDrain(drain LogDrain) LogDrain {
	drain.Config = cloneStringMap(drain.Config)
	return drain
}

func normalizeDomain(input string) (string, error) {
	fqdn := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(input)), ".")
	if fqdn == "" {
		return "", fmt.Errorf("fqdn is required")
	}
	if strings.Contains(fqdn, "://") || strings.ContainsAny(fqdn, "/ \\") {
		return "", fmt.Errorf("fqdn must be a hostname")
	}
	if len(fqdn) > 253 || !domainPattern.MatchString(fqdn) {
		return "", fmt.Errorf("fqdn must be a valid hostname")
	}
	return fqdn, nil
}

func (req CreateProjectRequest) toSpec() ProjectSpec {
	services, err := normalizeProjectServices(req.Services)
	if err != nil {
		services, _ = normalizeProjectServices(nil)
	}

	return ProjectSpec{
		Ref:           req.Ref,
		OrgID:         req.OrgID,
		Name:          req.Name,
		HostID:        req.HostID,
		Domain:        strings.TrimSuffix(strings.ToLower(strings.TrimSpace(req.Domain)), "."),
		StackVersion:  strings.TrimSpace(req.StackVersion),
		Profile:       req.Profile,
		ResourceTier:  req.ResourceTier,
		CPU:           req.CPU,
		RAMMB:         req.RAMMB,
		DiskGB:        req.DiskGB,
		EnforceLimits: req.EnforceLimits,
		Services:      services,
		Environment:   req.Environment,
	}
}

func defaultPlatformDefaults() PlatformDefaults {
	now := time.Now().UTC()
	return PlatformDefaults{
		Domain:                      "supadupa.test",
		StackVersion:                "latest",
		Profile:                     StackProfileFull,
		ResourceTier:                ResourceTierSmall,
		BackupSchedule:              "daily",
		FeatureFlags:                cloneBoolMap(defaultPlatformFeatureFlags),
		DatabaseIngressAllowedCIDRs: []string{},
		SMTP:                        defaultPlatformSMTP(),
		UpdatedAt:                   now,
	}
}

func normalizedPlatformDefaults(defaults PlatformDefaults) PlatformDefaults {
	input := PlatformDefaultsInput{
		Domain:                      defaults.Domain,
		StackVersion:                defaults.StackVersion,
		Profile:                     defaults.Profile,
		ResourceTier:                defaults.ResourceTier,
		BackupSchedule:              defaults.BackupSchedule,
		FeatureFlags:                defaults.FeatureFlags,
		DatabaseIngressAllowedCIDRs: defaults.DatabaseIngressAllowedCIDRs,
		SMTP:                        defaults.SMTP,
	}
	normalized, err := normalizePlatformDefaults(input)
	if err != nil {
		return defaultPlatformDefaults()
	}
	if !defaults.UpdatedAt.IsZero() {
		normalized.UpdatedAt = defaults.UpdatedAt
	}
	return normalized
}

func normalizePlatformDefaults(input PlatformDefaultsInput) (PlatformDefaults, error) {
	domain := strings.TrimSpace(input.Domain)
	if domain == "" {
		domain = "supadupa.test"
	}
	normalizedDomain, err := normalizeDomain(domain)
	if err != nil {
		return PlatformDefaults{}, fmt.Errorf("domain %w", err)
	}
	if err := validateGeneratedProjectFQDNs(strings.Repeat("a", 55), normalizedDomain); err != nil {
		return PlatformDefaults{}, fmt.Errorf("domain %w", err)
	}
	stackVersion := strings.TrimSpace(input.StackVersion)
	if stackVersion == "" {
		stackVersion = "latest"
	}
	if err := validateSupportedStackVersion(stackVersion); err != nil {
		return PlatformDefaults{}, err
	}
	profile := input.Profile
	if profile == "" {
		profile = StackProfileFull
	}
	if err := validateStackProfile(profile); err != nil {
		return PlatformDefaults{}, err
	}
	tier := input.ResourceTier
	if tier == "" {
		tier = ResourceTierSmall
	}
	if err := validateResourceTier(tier); err != nil {
		return PlatformDefaults{}, err
	}
	schedule := strings.TrimSpace(input.BackupSchedule)
	if schedule == "" {
		schedule = "daily"
	}
	if err := validateBackupSchedule(schedule); err != nil {
		return PlatformDefaults{}, err
	}
	smtp, err := normalizePlatformSMTP(input.SMTP)
	if err != nil {
		return PlatformDefaults{}, err
	}
	featureFlags, err := normalizePlatformFeatureFlags(input.FeatureFlags)
	if err != nil {
		return PlatformDefaults{}, err
	}
	databaseIngressAllowedCIDRs, err := normalizeOptionalNetworkCIDRs(input.DatabaseIngressAllowedCIDRs)
	if err != nil {
		return PlatformDefaults{}, fmt.Errorf("database ingress allowlist: %w", err)
	}
	return PlatformDefaults{
		Domain:                      normalizedDomain,
		StackVersion:                stackVersion,
		Profile:                     profile,
		ResourceTier:                tier,
		BackupSchedule:              schedule,
		FeatureFlags:                featureFlags,
		DatabaseIngressAllowedCIDRs: databaseIngressAllowedCIDRs,
		SMTP:                        smtp,
		UpdatedAt:                   time.Now().UTC(),
	}, nil
}

func validateSupportedStackVersion(version string) error {
	normalized := NormalizeStackReleaseVersion(version)
	if _, ok := ResolveStackReleaseManifestFromEnv(nil, normalized); ok {
		return nil
	}
	return fmt.Errorf("unsupported stack version %q; supported stable versions: %s", version, strings.Join(SupportedStackReleaseVersionsFromEnv(nil), ", "))
}

func normalizePlatformFeatureFlags(input map[string]bool) (map[string]bool, error) {
	out := cloneBoolMap(defaultPlatformFeatureFlags)
	for key, value := range input {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "" {
			return nil, fmt.Errorf("feature flag key is required")
		}
		if _, ok := defaultPlatformFeatureFlags[normalized]; !ok {
			return nil, fmt.Errorf("unsupported feature flag %q", normalized)
		}
		out[normalized] = value
	}
	return out, nil
}

func normalizeOrgFeatureOverrides(input map[string]bool) (map[string]bool, error) {
	out := map[string]bool{}
	for key, value := range input {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "" {
			return nil, fmt.Errorf("feature flag key is required")
		}
		if _, ok := defaultPlatformFeatureFlags[normalized]; !ok {
			return nil, fmt.Errorf("unsupported feature flag %q", normalized)
		}
		out[normalized] = value
	}
	return out, nil
}

func orgFeatureFlags(orgID string, overrides map[string]bool, defaults PlatformDefaults) OrgFeatureFlags {
	defaultFlags := cloneBoolMap(defaults.FeatureFlags)
	effective := cloneBoolMap(defaultFlags)
	normalizedOverrides, err := normalizeOrgFeatureOverrides(overrides)
	if err != nil {
		normalizedOverrides = map[string]bool{}
	}
	for key, value := range normalizedOverrides {
		effective[key] = value
	}
	return OrgFeatureFlags{
		OrgID:     orgID,
		Defaults:  defaultFlags,
		Overrides: normalizedOverrides,
		Effective: effective,
	}
}

func orgWithFeatureFlags(org Org, defaults PlatformDefaults) Org {
	flags := orgFeatureFlags(org.ID, org.FeatureFlagOverrides, defaults)
	org.FeatureFlagOverrides = flags.Overrides
	org.FeatureFlags = flags.Effective
	return org
}

func defaultPlatformSMTP() PlatformSMTP {
	return PlatformSMTP{
		Port:    587,
		TLSMode: "starttls",
	}
}

func normalizePlatformSMTP(input PlatformSMTP) (PlatformSMTP, error) {
	smtp := PlatformSMTP{
		Enabled:        input.Enabled,
		Host:           strings.TrimSpace(input.Host),
		Port:           input.Port,
		SenderName:     strings.TrimSpace(input.SenderName),
		SenderEmail:    strings.TrimSpace(input.SenderEmail),
		Username:       strings.TrimSpace(input.Username),
		PasswordHandle: strings.TrimSpace(input.PasswordHandle),
		TLSMode:        strings.ToLower(strings.TrimSpace(input.TLSMode)),
	}
	if smtp.Port == 0 {
		smtp.Port = 587
	}
	if smtp.TLSMode == "" {
		smtp.TLSMode = "starttls"
	}
	config := map[string]string{
		"port":            strconv.Itoa(smtp.Port),
		"password_handle": smtp.PasswordHandle,
		"tls_mode":        smtp.TLSMode,
	}
	if err := validateSMTPConfig(config); err != nil {
		return PlatformSMTP{}, err
	}
	if smtp.Enabled && smtp.Host == "" {
		return PlatformSMTP{}, fmt.Errorf("smtp host is required when platform smtp is enabled")
	}
	return smtp, nil
}

func validateStackProfile(profile StackProfile) error {
	switch profile {
	case StackProfileFull, StackProfileEssential, StackProfileOrioleDB:
		return nil
	default:
		return fmt.Errorf("unsupported stack profile %q", profile)
	}
}

func validateResourceTier(tier ResourceTier) error {
	switch tier {
	case ResourceTierSmall, ResourceTierMedium, ResourceTierLarge:
		return nil
	default:
		return fmt.Errorf("unsupported resource tier %q", tier)
	}
}

func validateBackupSchedule(schedule string) error {
	if schedule != "daily" && schedule != "hourly" {
		return fmt.Errorf("unsupported backup schedule %q", schedule)
	}
	return nil
}

func newID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data[:])
}

func generateProjectSecrets(ref string) map[string]ProjectSecret {
	now := time.Now().UTC()
	secrets := map[string]ProjectSecret{}
	for kind := range secretPrefixes {
		if kind == "anon_key" || kind == "service_role" {
			continue
		}
		secrets[kind] = newProjectSecret(ref, kind, now)
	}
	ensureSupabaseAPIKeys(ref, secrets, now)
	return secrets
}

func ensureProjectSigningKeys(ref string, secrets map[string]ProjectSecret) {
	now := time.Now().UTC()
	for _, kind := range []string{"jwt_signing_key_current", "jwt_signing_key_next"} {
		if _, ok := secrets[kind]; !ok {
			secrets[kind] = newProjectSecret(ref, kind, now)
		}
	}
}

func ensureSupabaseAPIKeys(ref string, secrets map[string]ProjectSecret, now time.Time) {
	jwtSecret := strings.TrimSpace(secrets["jwt_secret"].Value)
	if jwtSecret == "" {
		secrets["jwt_secret"] = newProjectSecret(ref, "jwt_secret", now)
		jwtSecret = secrets["jwt_secret"].Value
	}
	for _, role := range []string{"anon", "service_role"} {
		kind := "anon_key"
		if role == "service_role" {
			kind = "service_role"
		}
		token := supabaseRoleJWT(ref, role, jwtSecret)
		secret, ok := secrets[kind]
		if !ok {
			secret = ProjectSecret{
				ID:         newID(),
				ProjectRef: ref,
				Kind:       kind,
				CreatedAt:  now,
			}
		}
		if !looksLikeJWT(secret.Value) || !verifySupabaseRoleJWT(secret.Value, role, jwtSecret) {
			secret.Value = token
			secret.Masked = maskSecret(token)
			secrets[kind] = secret
		}
	}
}

func newProjectSecret(ref string, kind string, now time.Time) ProjectSecret {
	value := randomSecretValue(ref, kind)
	return ProjectSecret{
		ID:         newID(),
		ProjectRef: ref,
		Kind:       kind,
		Value:      value,
		Masked:     maskSecret(value),
		CreatedAt:  now,
	}
}

func randomSecretValue(ref string, kind string) string {
	if strings.HasPrefix(kind, "jwt_signing_key_") {
		status := "previous"
		switch kind {
		case "jwt_signing_key_current":
			status = "current"
		case "jwt_signing_key_next":
			status = "next"
		}
		return randomJWTSigningKeyValue(ref, status)
	}
	if kind == "db_password" {
		return randomHex(secretByteLengths[kind])
	}
	return randomToken(secretPrefixes[kind], secretByteLengths[kind])
}

func supabaseRoleJWT(ref string, role string, jwtSecret string) string {
	now := time.Now().UTC().Unix()
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims := map[string]any{
		"iss":  "supabase",
		"ref":  ref,
		"role": role,
		"aud":  "authenticated",
		"iat":  now,
		"jti":  newID(),
		"exp":  int64(4102444800), // 2100-01-01T00:00:00Z, matching long-lived self-hosted API keys.
	}
	headerPayload, _ := json.Marshal(header)
	claimsPayload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(headerPayload) + "." + base64.RawURLEncoding.EncodeToString(claimsPayload)
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func looksLikeJWT(value string) bool {
	return len(strings.Split(strings.TrimSpace(value), ".")) == 3
}

func verifySupabaseRoleJWT(token string, role string, jwtSecret string) bool {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := mac.Sum(nil)
	actual, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(actual, expected) {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return false
	}
	return claims["role"] == role && claims["aud"] == "authenticated"
}

func randomJWTSigningKeyValue(ref string, status string) string {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return randomToken("jwk", 32)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return randomToken("jwk", 32)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return randomToken("jwk", 32)
	}
	material := JWTSigningKeyMaterial{
		KID:        ref + "-" + status + "-" + randomHex(4),
		Alg:        "EdDSA",
		PublicKey:  strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))),
		PrivateKey: strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))),
		Status:     status,
	}
	payload, err := json.Marshal(material)
	if err != nil {
		return randomToken("jwk", 32)
	}
	return string(payload)
}

func updateJWTSigningKeyMaterialStatus(value string, status string) string {
	material := JWTSigningKeyMaterial{}
	if err := json.Unmarshal([]byte(value), &material); err != nil {
		return value
	}
	material.Status = status
	payload, err := json.Marshal(material)
	if err != nil {
		return value
	}
	return string(payload)
}

func defaultBackupPolicy(ref string) BackupPolicy {
	return defaultBackupPolicyForSchedule(ref, "daily")
}

func defaultBackupPolicyForSchedule(ref string, schedule string) BackupPolicy {
	if err := validateBackupSchedule(schedule); err != nil {
		schedule = "daily"
	}
	now := time.Now().UTC()
	next := nextBackupRun(now, schedule)
	return BackupPolicy{
		ProjectRef: ref,
		Enabled:    true,
		Schedule:   schedule,
		Kind:       "logical",
		NextRunAt:  &next,
		UpdatedAt:  now,
	}
}

func normalizeBackupStorageTargetInput(id string, existing BackupStorageTarget, input BackupStorageTargetInput, creating bool) (BackupStorageTarget, error) {
	targetType := strings.ToLower(strings.TrimSpace(input.Type))
	if targetType == "" {
		targetType = "s3"
	}
	if targetType != "s3" {
		return BackupStorageTarget{}, fmt.Errorf("unsupported backup storage target type %q", targetType)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target name is required")
	}
	if len(name) > 120 || strings.ContainsAny(name, "\r\n\t") {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target name is invalid")
	}
	endpoint := strings.TrimSpace(input.Endpoint)
	if endpoint != "" {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return BackupStorageTarget{}, fmt.Errorf("backup storage target endpoint must be an absolute URL")
		}
		if parsed.User != nil {
			return BackupStorageTarget{}, fmt.Errorf("backup storage target endpoint must not include credentials")
		}
		if parsed.Scheme != "https" && parsed.Scheme != "http" {
			return BackupStorageTarget{}, fmt.Errorf("backup storage target endpoint scheme must be http or https")
		}
		endpoint = strings.TrimRight(endpoint, "/")
	}
	region := strings.TrimSpace(input.Region)
	if region == "" {
		region = "auto"
	}
	if len(region) > 80 || strings.ContainsAny(region, "\r\n\t /") {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target region is invalid")
	}
	bucket := strings.TrimSpace(input.Bucket)
	if bucket == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target bucket is required")
	}
	if len(bucket) > 255 || strings.ContainsAny(bucket, "\r\n\t/\\") {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target bucket is invalid")
	}
	prefix := strings.Trim(strings.TrimSpace(input.Prefix), "/")
	if strings.ContainsAny(prefix, "\r\n\t\\") || strings.Contains(prefix, "..") {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target prefix is invalid")
	}
	accessKeyID := strings.TrimSpace(input.AccessKeyID)
	secretAccessKey := strings.TrimSpace(input.SecretAccessKey)
	if accessKeyID == "" && !creating {
		accessKeyID = existing.AccessKeyID
	}
	if secretAccessKey == "" && !creating {
		secretAccessKey = existing.SecretAccessKey
	}
	if accessKeyID == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target access key id is required")
	}
	if secretAccessKey == "" {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target secret access key is required")
	}
	if strings.ContainsAny(accessKeyID, "\r\n\t") || strings.ContainsAny(secretAccessKey, "\r\n") {
		return BackupStorageTarget{}, fmt.Errorf("backup storage target credentials are invalid")
	}
	now := time.Now().UTC()
	createdAt := existing.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	target := BackupStorageTarget{
		ID:               id,
		Name:             name,
		Type:             targetType,
		Endpoint:         endpoint,
		Region:           region,
		Bucket:           bucket,
		Prefix:           prefix,
		AccessKeyID:      accessKeyID,
		SecretAccessKey:  secretAccessKey,
		SecretConfigured: secretAccessKey != "",
		ForcePathStyle:   input.ForcePathStyle,
		Default:          input.Default,
		LastTestedAt:     existing.LastTestedAt,
		LastTestStatus:   existing.LastTestStatus,
		LastTestError:    existing.LastTestError,
		CreatedAt:        createdAt,
		UpdatedAt:        now,
	}
	if !creating && backupStorageTargetConnectionChanged(existing, target) {
		target.LastTestedAt = nil
		target.LastTestStatus = ""
		target.LastTestError = ""
	}
	return target, nil
}

func redactBackupStorageTarget(target BackupStorageTarget) BackupStorageTarget {
	target.SecretConfigured = strings.TrimSpace(target.SecretAccessKey) != ""
	target.SecretAccessKey = ""
	target.DurableOffHost, target.RecoveryReady, target.ReadinessStatus, target.ReadinessMessage = backupStorageTargetReadiness(target)
	return target
}

func backupStorageTargetConnectionChanged(a BackupStorageTarget, b BackupStorageTarget) bool {
	return a.Endpoint != b.Endpoint ||
		a.Region != b.Region ||
		a.Bucket != b.Bucket ||
		a.Prefix != b.Prefix ||
		a.AccessKeyID != b.AccessKeyID ||
		a.SecretAccessKey != b.SecretAccessKey ||
		a.ForcePathStyle != b.ForcePathStyle
}

func defaultPITRPolicy(ref string) PITRPolicy {
	return PITRPolicy{
		ProjectRef:    ref,
		Enabled:       false,
		ArchiveBucket: "",
		RetentionDays: 7,
		UpdatedAt:     time.Now().UTC(),
	}
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func cloneProjectDomains(domains []ProjectDomain) []ProjectDomain {
	out := append([]ProjectDomain(nil), domains...)
	for index := range out {
		out[index].CertNotAfter = cloneTimePtr(out[index].CertNotAfter)
	}
	return out
}

func nextBackupRun(from time.Time, schedule string) time.Time {
	from = from.UTC()
	if schedule == "hourly" {
		return from.Add(time.Hour).Truncate(time.Hour)
	}
	next := time.Date(from.Year(), from.Month(), from.Day(), 2, 0, 0, 0, time.UTC)
	if !next.After(from) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func secretsToSlice(secrets map[string]ProjectSecret) []ProjectSecret {
	out := make([]ProjectSecret, 0, len(secrets))
	for _, secret := range secrets {
		out = append(out, secret)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Kind < out[j].Kind
	})
	return out
}

func randomToken(prefix string, bytes int) string {
	return prefix + "_" + randomHex(bytes)
}

func randomHex(bytes int) string {
	data := make([]byte, bytes)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data)
}

func maskSecret(value string) string {
	if len(value) <= 10 {
		return "********"
	}
	return value[:6] + strings.Repeat("*", 12) + value[len(value)-4:]
}
