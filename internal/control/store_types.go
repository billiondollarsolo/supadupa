package control

import (
	"time"
)

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
	TokenVersion     int64     `json:"-"`
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

type TelemetryHistoryQuery struct {
	From  time.Time
	To    time.Time
	Step  time.Duration
	Limit int
}

type ProjectTelemetryHistory struct {
	ProjectRef          string                         `json:"project_ref"`
	From                time.Time                      `json:"from"`
	To                  time.Time                      `json:"to"`
	StepSeconds         int                            `json:"step_seconds"`
	RetentionSeconds    int                            `json:"retention_seconds"`
	RawRetentionSeconds int                            `json:"raw_retention_seconds"`
	LatestSampledAt     time.Time                      `json:"latest_sampled_at,omitempty"`
	Points              []ProjectTelemetryHistoryPoint `json:"points"`
}

type ProjectTelemetryHistoryPoint struct {
	SampledAt                time.Time `json:"sampled_at"`
	Source                   string    `json:"source"`
	Samples                  int       `json:"samples"`
	CPUPercent               float64   `json:"cpu_percent"`
	CPUReservationPercent    float64   `json:"cpu_reservation_percent"`
	MemoryBytes              int64     `json:"memory_bytes"`
	MemoryLimitBytes         int64     `json:"memory_limit_bytes"`
	MemoryReservationPercent float64   `json:"memory_reservation_percent"`
	DiskUsedBytes            int64     `json:"disk_used_bytes"`
	DiskLimitBytes           int64     `json:"disk_limit_bytes"`
	DiskReservationPercent   float64   `json:"disk_reservation_percent"`
	NetworkRxBytes           int64     `json:"network_rx_bytes"`
	NetworkTxBytes           int64     `json:"network_tx_bytes"`
	ReservedCPU              int       `json:"reserved_cpu"`
	ReservedRAMMB            int       `json:"reserved_ram_mb"`
	ReservedDiskGB           int       `json:"reserved_disk_gb"`
}

type TelemetryHistorySample struct {
	ProjectRef               string
	Source                   string
	Samples                  int
	CPUPercent               float64
	CPUReservationPercent    float64
	MemoryBytes              int64
	MemoryLimitBytes         int64
	MemoryReservationPercent float64
	DiskUsedBytes            int64
	DiskLimitBytes           int64
	DiskReservationPercent   float64
	NetworkRxBytes           int64
	NetworkTxBytes           int64
	ReservedCPU              int
	ReservedRAMMB            int
	ReservedDiskGB           int
	SampledAt                time.Time
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

type ProjectResourcesInput struct {
	CPU           int  `json:"cpu"`
	RAMMB         int  `json:"ram_mb"`
	DiskGB        int  `json:"disk_gb"`
	EnforceLimits bool `json:"enforce_limits"`
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
	Warnings         []string   `json:"warnings,omitempty"`
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
