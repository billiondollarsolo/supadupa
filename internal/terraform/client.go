package terraform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrNotFound = errors.New("not found")

type Client struct {
	baseURL string
	token   string
	client  *http.Client
}

type Org struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PlatformDefaults struct {
	Domain         string       `json:"domain"`
	StackVersion   string       `json:"stack_version"`
	Profile        string       `json:"profile"`
	ResourceTier   string       `json:"resource_tier"`
	BackupSchedule string       `json:"backup_schedule"`
	SMTP           PlatformSMTP `json:"smtp"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

type PlatformDefaultsInput struct {
	Domain         string       `json:"domain"`
	StackVersion   string       `json:"stack_version"`
	Profile        string       `json:"profile"`
	ResourceTier   string       `json:"resource_tier"`
	BackupSchedule string       `json:"backup_schedule"`
	SMTP           PlatformSMTP `json:"smtp"`
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
	Enabled       bool      `json:"enabled"`
	Provider      string    `json:"provider"`
	IDPEntityID   string    `json:"idp_entity_id"`
	SSOURL        string    `json:"sso_url"`
	Certificate   string    `json:"certificate_pem"`
	ACSURL        string    `json:"acs_url"`
	MetadataURL   string    `json:"metadata_url"`
	EmailDomain   string    `json:"email_domain"`
	AutoProvision bool      `json:"auto_provision"`
	DefaultRole   string    `json:"default_role"`
	UpdatedAt     time.Time `json:"updated_at"`
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
}

type HostCapacity struct {
	CPU      int `json:"cpu"`
	RAMMB    int `json:"ram_mb"`
	DiskGB   int `json:"disk_gb"`
	DiskIOPS int `json:"disk_iops"`
	Project  int `json:"projects"`
}

type Host struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Address   string       `json:"address"`
	Capacity  HostCapacity `json:"capacity"`
	Used      HostCapacity `json:"used"`
	CreatedAt time.Time    `json:"created_at"`
}

type HostInput struct {
	Name     string       `json:"name"`
	Address  string       `json:"address"`
	Capacity HostCapacity `json:"capacity"`
}

type OrgQuota struct {
	OrgID       string       `json:"org_id"`
	MaxProjects int          `json:"max_projects"`
	MaxCPU      int          `json:"max_cpu"`
	MaxRAMMB    int          `json:"max_ram_mb"`
	MaxDiskGB   int          `json:"max_disk_gb"`
	MaxDiskIOPS int          `json:"max_disk_iops"`
	Used        HostCapacity `json:"used"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type OrgQuotaInput struct {
	MaxProjects int `json:"max_projects"`
	MaxCPU      int `json:"max_cpu"`
	MaxRAMMB    int `json:"max_ram_mb"`
	MaxDiskGB   int `json:"max_disk_gb"`
	MaxDiskIOPS int `json:"max_disk_iops"`
}

type OrgMember struct {
	UserID    string    `json:"user_id"`
	OrgID     string    `json:"org_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type OrgMemberInput struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type OrgTeam struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

type OrgTeamInput struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type OrgTeamMember struct {
	TeamID    string    `json:"team_id"`
	OrgID     string    `json:"org_id"`
	TeamSlug  string    `json:"team_slug"`
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type OrgTeamMemberInput struct {
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

type ProjectAccessGrantInput struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Role        string `json:"role"`
}

type ProjectBackupPolicy struct {
	ProjectRef string     `json:"project_ref"`
	Enabled    bool       `json:"enabled"`
	Schedule   string     `json:"schedule"`
	Kind       string     `json:"kind"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
	NextRunAt  *time.Time `json:"next_run_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type ProjectBackupPolicyInput struct {
	Enabled  bool   `json:"enabled"`
	Schedule string `json:"schedule"`
	Kind     string `json:"kind"`
}

type ProjectPITRPolicy struct {
	ProjectRef    string     `json:"project_ref"`
	Enabled       bool       `json:"enabled"`
	ArchiveBucket string     `json:"archive_bucket"`
	RetentionDays int        `json:"retention_days"`
	LastArchiveAt *time.Time `json:"last_archive_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type ProjectPITRPolicyInput struct {
	Enabled       bool   `json:"enabled"`
	ArchiveBucket string `json:"archive_bucket"`
	RetentionDays int    `json:"retention_days"`
}

type ProjectBranch struct {
	ID               string     `json:"id"`
	SourceProjectRef string     `json:"source_project_ref"`
	ProjectRef       string     `json:"project_ref"`
	Name             string     `json:"name"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
}

type ProjectBranchInput struct {
	Ref      string `json:"ref"`
	Name     string `json:"name"`
	TTLHours int    `json:"ttl_hours"`
}

type projectBranchCreateResponseWire struct {
	Branch  ProjectBranch `json:"branch"`
	Project Project       `json:"project"`
}

type ProjectReplica struct {
	ID                    string     `json:"id"`
	ProjectRef            string     `json:"project_ref"`
	Name                  string     `json:"name"`
	HostID                string     `json:"host_id,omitempty"`
	Region                string     `json:"region,omitempty"`
	Tier                  string     `json:"tier"`
	Status                string     `json:"status"`
	Role                  string     `json:"role"`
	Message               string     `json:"message,omitempty"`
	ReadURI               string     `json:"read_uri"`
	ReadWeight            int        `json:"read_weight"`
	FailoverPriority      int        `json:"failover_priority"`
	ReplicationLagBytes   int64      `json:"replication_lag_bytes"`
	ReplicationLagSeconds int        `json:"replication_lag_seconds"`
	PromotedAt            *time.Time `json:"promoted_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type ProjectReplicaInput struct {
	Name             string `json:"name"`
	HostID           string `json:"host_id"`
	Region           string `json:"region"`
	Tier             string `json:"tier"`
	ReadWeight       int    `json:"read_weight"`
	FailoverPriority int    `json:"failover_priority"`
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

type ProjectReplicaActionInput struct {
	Reason string `json:"reason"`
}

type Project struct {
	ID     string      `json:"id"`
	Ref    string      `json:"ref"`
	OrgID  string      `json:"org_id"`
	Name   string      `json:"name"`
	Status string      `json:"status"`
	Spec   ProjectSpec `json:"spec"`
}

type ProjectSpec struct {
	HostID       string `json:"host_id"`
	Domain       string `json:"domain"`
	StackVersion string `json:"stack_version"`
	Profile      string `json:"profile"`
	ResourceTier string `json:"resource_tier"`
}

type CreateProjectRequest struct {
	Ref          string `json:"ref"`
	Name         string `json:"name"`
	HostID       string `json:"host_id,omitempty"`
	Domain       string `json:"domain,omitempty"`
	StackVersion string `json:"stack_version,omitempty"`
	Profile      string `json:"profile,omitempty"`
	ResourceTier string `json:"resource_tier,omitempty"`
}

type ProjectConfig struct {
	ProjectRef string            `json:"project_ref"`
	Area       string            `json:"area"`
	Config     map[string]string `json:"config"`
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
	ID            string            `json:"id"`
	ProjectRef    string            `json:"project_ref"`
	HookType      string            `json:"hook_type"`
	Enabled       bool              `json:"enabled"`
	TargetURI     string            `json:"target_uri,omitempty"`
	EdgeFunction  string            `json:"edge_function,omitempty"`
	SecretHandle  string            `json:"secret_handle,omitempty"`
	Headers       map[string]string `json:"headers"`
	TimeoutMS     int               `json:"timeout_ms"`
	RetryAttempts int               `json:"retry_attempts"`
	Status        string            `json:"status"`
	Message       string            `json:"message,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
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

type ProjectDomain struct {
	ProjectRef string `json:"project_ref"`
	FQDN       string `json:"fqdn"`
	CertStatus string `json:"cert_status"`
}

type ProjectDomainInput struct {
	FQDN string `json:"fqdn"`
}

type ProjectLogDrain struct {
	ID         string            `json:"id"`
	ProjectRef string            `json:"project_ref"`
	Target     string            `json:"target"`
	Config     map[string]string `json:"config"`
	CreatedAt  time.Time         `json:"created_at"`
}

type ProjectLogDrainInput struct {
	Target string            `json:"target"`
	Config map[string]string `json:"config"`
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

func NewClient(baseURL string, token string, httpClient *http.Client) (*Client, error) {
	normalized, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: normalized, token: token, client: httpClient}, nil
}

func (c *Client) ListOrgs(ctx context.Context) ([]Org, error) {
	var orgs []Org
	if err := c.do(ctx, http.MethodGet, "/v1/orgs", nil, &orgs); err != nil {
		return nil, err
	}
	return orgs, nil
}

func (c *Client) CreateOrg(ctx context.Context, name string) (Org, error) {
	var org Org
	if err := c.do(ctx, http.MethodPost, "/v1/orgs", map[string]string{"name": name}, &org); err != nil {
		return Org{}, err
	}
	return org, nil
}

func (c *Client) GetOrg(ctx context.Context, id string) (Org, error) {
	var org Org
	if err := c.do(ctx, http.MethodGet, "/v1/orgs/"+url.PathEscape(id), nil, &org); err != nil {
		return Org{}, err
	}
	return org, nil
}

func (c *Client) UpdateOrg(ctx context.Context, id string, name string) (Org, error) {
	var org Org
	if err := c.do(ctx, http.MethodPut, "/v1/orgs/"+url.PathEscape(id), map[string]string{"name": name}, &org); err != nil {
		return Org{}, err
	}
	return org, nil
}

func (c *Client) DeleteOrg(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/orgs/"+url.PathEscape(id), nil, nil)
}

func (c *Client) ListHosts(ctx context.Context) ([]Host, error) {
	var hosts []Host
	if err := c.do(ctx, http.MethodGet, "/v1/hosts", nil, &hosts); err != nil {
		return nil, err
	}
	return hosts, nil
}

func (c *Client) CreateHost(ctx context.Context, input HostInput) (Host, error) {
	var host Host
	if err := c.do(ctx, http.MethodPost, "/v1/hosts", input, &host); err != nil {
		return Host{}, err
	}
	return host, nil
}

func (c *Client) GetHost(ctx context.Context, id string) (Host, error) {
	var host Host
	if err := c.do(ctx, http.MethodGet, "/v1/hosts/"+url.PathEscape(id), nil, &host); err != nil {
		return Host{}, err
	}
	return host, nil
}

func (c *Client) DeleteHost(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/hosts/"+url.PathEscape(id), nil, nil)
}

func (c *Client) GetPlatformDefaults(ctx context.Context) (PlatformDefaults, error) {
	var defaults PlatformDefaults
	if err := c.do(ctx, http.MethodGet, "/v1/settings/defaults", nil, &defaults); err != nil {
		return PlatformDefaults{}, err
	}
	return defaults, nil
}

func (c *Client) UpdatePlatformDefaults(ctx context.Context, input PlatformDefaultsInput) (PlatformDefaults, error) {
	var defaults PlatformDefaults
	if err := c.do(ctx, http.MethodPut, "/v1/settings/defaults", input, &defaults); err != nil {
		return PlatformDefaults{}, err
	}
	return defaults, nil
}

func (c *Client) GetPlatformSSOConfig(ctx context.Context) (PlatformSSOConfig, error) {
	var config PlatformSSOConfig
	if err := c.do(ctx, http.MethodGet, "/v1/settings/sso", nil, &config); err != nil {
		return PlatformSSOConfig{}, err
	}
	return config, nil
}

func (c *Client) UpdatePlatformSSOConfig(ctx context.Context, input PlatformSSOConfigInput) (PlatformSSOConfig, error) {
	var config PlatformSSOConfig
	if err := c.do(ctx, http.MethodPut, "/v1/settings/sso", input, &config); err != nil {
		return PlatformSSOConfig{}, err
	}
	return config, nil
}

func (c *Client) GetOrgQuota(ctx context.Context, orgID string) (OrgQuota, error) {
	var quota OrgQuota
	path := "/v1/orgs/" + url.PathEscape(orgID) + "/quotas"
	if err := c.do(ctx, http.MethodGet, path, nil, &quota); err != nil {
		return OrgQuota{}, err
	}
	return quota, nil
}

func (c *Client) UpdateOrgQuota(ctx context.Context, orgID string, input OrgQuotaInput) (OrgQuota, error) {
	var quota OrgQuota
	path := "/v1/orgs/" + url.PathEscape(orgID) + "/quotas"
	if err := c.do(ctx, http.MethodPut, path, input, &quota); err != nil {
		return OrgQuota{}, err
	}
	return quota, nil
}

func (c *Client) ListOrgMembers(ctx context.Context, orgID string) ([]OrgMember, error) {
	var members []OrgMember
	path := "/v1/orgs/" + url.PathEscape(orgID) + "/members"
	if err := c.do(ctx, http.MethodGet, path, nil, &members); err != nil {
		return nil, err
	}
	return members, nil
}

func (c *Client) UpsertOrgMember(ctx context.Context, orgID string, input OrgMemberInput) (OrgMember, error) {
	var member OrgMember
	path := "/v1/orgs/" + url.PathEscape(orgID) + "/members"
	if err := c.do(ctx, http.MethodPost, path, input, &member); err != nil {
		return OrgMember{}, err
	}
	return member, nil
}

func (c *Client) DeleteOrgMember(ctx context.Context, orgID string, email string) error {
	path := "/v1/orgs/" + url.PathEscape(orgID) + "/members/" + url.PathEscape(email)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListOrgTeams(ctx context.Context, orgID string) ([]OrgTeam, error) {
	var teams []OrgTeam
	path := "/v1/orgs/" + url.PathEscape(orgID) + "/teams"
	if err := c.do(ctx, http.MethodGet, path, nil, &teams); err != nil {
		return nil, err
	}
	return teams, nil
}

func (c *Client) CreateOrgTeam(ctx context.Context, orgID string, input OrgTeamInput) (OrgTeam, error) {
	var team OrgTeam
	path := "/v1/orgs/" + url.PathEscape(orgID) + "/teams"
	if err := c.do(ctx, http.MethodPost, path, input, &team); err != nil {
		return OrgTeam{}, err
	}
	return team, nil
}

func (c *Client) DeleteOrgTeam(ctx context.Context, orgID string, slug string) error {
	path := "/v1/orgs/" + url.PathEscape(orgID) + "/teams/" + url.PathEscape(slug)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListOrgTeamMembers(ctx context.Context, orgID string, slug string) ([]OrgTeamMember, error) {
	var members []OrgTeamMember
	path := "/v1/orgs/" + url.PathEscape(orgID) + "/teams/" + url.PathEscape(slug) + "/members"
	if err := c.do(ctx, http.MethodGet, path, nil, &members); err != nil {
		return nil, err
	}
	return members, nil
}

func (c *Client) UpsertOrgTeamMember(ctx context.Context, orgID string, slug string, input OrgTeamMemberInput) (OrgTeamMember, error) {
	var member OrgTeamMember
	path := "/v1/orgs/" + url.PathEscape(orgID) + "/teams/" + url.PathEscape(slug) + "/members"
	if err := c.do(ctx, http.MethodPost, path, input, &member); err != nil {
		return OrgTeamMember{}, err
	}
	return member, nil
}

func (c *Client) DeleteOrgTeamMember(ctx context.Context, orgID string, slug string, email string) error {
	path := "/v1/orgs/" + url.PathEscape(orgID) + "/teams/" + url.PathEscape(slug) + "/members/" + url.PathEscape(email)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) CreateProject(ctx context.Context, orgID string, input CreateProjectRequest) (Project, error) {
	var project Project
	path := "/v1/orgs/" + url.PathEscape(orgID) + "/projects"
	if err := c.do(ctx, http.MethodPost, path, input, &project); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (c *Client) GetProject(ctx context.Context, ref string) (Project, error) {
	var project Project
	if err := c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(ref), nil, &project); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (c *Client) DeleteProject(ctx context.Context, ref string) error {
	return c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(ref), nil, nil)
}

func (c *Client) ListProjectAccess(ctx context.Context, ref string) ([]ProjectAccessGrant, error) {
	var grants []ProjectAccessGrant
	path := "/v1/projects/" + url.PathEscape(ref) + "/access"
	if err := c.do(ctx, http.MethodGet, path, nil, &grants); err != nil {
		return nil, err
	}
	return grants, nil
}

func (c *Client) UpsertProjectAccess(ctx context.Context, ref string, input ProjectAccessGrantInput) (ProjectAccessGrant, error) {
	var grant ProjectAccessGrant
	path := "/v1/projects/" + url.PathEscape(ref) + "/access"
	if err := c.do(ctx, http.MethodPut, path, input, &grant); err != nil {
		return ProjectAccessGrant{}, err
	}
	return grant, nil
}

func (c *Client) DeleteProjectAccess(ctx context.Context, ref string, subjectType string, subjectID string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/access/" + url.PathEscape(subjectType) + "/" + url.PathEscape(subjectID)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) GetProjectBackupPolicy(ctx context.Context, ref string) (ProjectBackupPolicy, error) {
	var policy ProjectBackupPolicy
	path := "/v1/projects/" + url.PathEscape(ref) + "/backups/policy"
	if err := c.do(ctx, http.MethodGet, path, nil, &policy); err != nil {
		return ProjectBackupPolicy{}, err
	}
	return policy, nil
}

func (c *Client) UpdateProjectBackupPolicy(ctx context.Context, ref string, input ProjectBackupPolicyInput) (ProjectBackupPolicy, error) {
	var policy ProjectBackupPolicy
	path := "/v1/projects/" + url.PathEscape(ref) + "/backups/policy"
	if err := c.do(ctx, http.MethodPut, path, input, &policy); err != nil {
		return ProjectBackupPolicy{}, err
	}
	return policy, nil
}

func (c *Client) GetProjectPITRPolicy(ctx context.Context, ref string) (ProjectPITRPolicy, error) {
	var policy ProjectPITRPolicy
	path := "/v1/projects/" + url.PathEscape(ref) + "/pitr/policy"
	if err := c.do(ctx, http.MethodGet, path, nil, &policy); err != nil {
		return ProjectPITRPolicy{}, err
	}
	return policy, nil
}

func (c *Client) UpdateProjectPITRPolicy(ctx context.Context, ref string, input ProjectPITRPolicyInput) (ProjectPITRPolicy, error) {
	var policy ProjectPITRPolicy
	path := "/v1/projects/" + url.PathEscape(ref) + "/pitr/policy"
	if err := c.do(ctx, http.MethodPut, path, input, &policy); err != nil {
		return ProjectPITRPolicy{}, err
	}
	return policy, nil
}

func (c *Client) ListProjectBranches(ctx context.Context, ref string) ([]ProjectBranch, error) {
	var branches []ProjectBranch
	path := "/v1/projects/" + url.PathEscape(ref) + "/branches"
	if err := c.do(ctx, http.MethodGet, path, nil, &branches); err != nil {
		return nil, err
	}
	return branches, nil
}

func (c *Client) CreateProjectBranch(ctx context.Context, sourceRef string, input ProjectBranchInput) (ProjectBranch, Project, error) {
	var wire projectBranchCreateResponseWire
	path := "/v1/projects/" + url.PathEscape(sourceRef) + "/branches"
	if err := c.do(ctx, http.MethodPost, path, input, &wire); err != nil {
		return ProjectBranch{}, Project{}, err
	}
	return wire.Branch, wire.Project, nil
}

func (c *Client) DeleteProjectBranch(ctx context.Context, sourceRef string, branchRef string) error {
	path := "/v1/projects/" + url.PathEscape(sourceRef) + "/branches/" + url.PathEscape(branchRef)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectReplicas(ctx context.Context, ref string) ([]ProjectReplica, error) {
	var replicas []ProjectReplica
	path := "/v1/projects/" + url.PathEscape(ref) + "/replicas"
	if err := c.do(ctx, http.MethodGet, path, nil, &replicas); err != nil {
		return nil, err
	}
	return replicas, nil
}

func (c *Client) CreateProjectReplica(ctx context.Context, ref string, input ProjectReplicaInput) (ProjectReplica, error) {
	var replica ProjectReplica
	path := "/v1/projects/" + url.PathEscape(ref) + "/replicas"
	if err := c.do(ctx, http.MethodPost, path, input, &replica); err != nil {
		return ProjectReplica{}, err
	}
	return replica, nil
}

func (c *Client) DeleteProjectReplica(ctx context.Context, ref string, id string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/replicas/" + url.PathEscape(id)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) GetProjectReplicaRouting(ctx context.Context, ref string) (ProjectReplicaRouting, error) {
	var routing ProjectReplicaRouting
	path := "/v1/projects/" + url.PathEscape(ref) + "/replicas/routing"
	if err := c.do(ctx, http.MethodGet, path, nil, &routing); err != nil {
		return ProjectReplicaRouting{}, err
	}
	return routing, nil
}

func (c *Client) PromoteProjectReplica(ctx context.Context, ref string, id string, reason string) (ProjectReplica, error) {
	var replica ProjectReplica
	path := "/v1/projects/" + url.PathEscape(ref) + "/replicas/" + url.PathEscape(id) + "/promote"
	if err := c.do(ctx, http.MethodPost, path, ProjectReplicaActionInput{Reason: reason}, &replica); err != nil {
		return ProjectReplica{}, err
	}
	return replica, nil
}

func (c *Client) FailoverProjectReplica(ctx context.Context, ref string, reason string) (ProjectReplica, error) {
	var replica ProjectReplica
	path := "/v1/projects/" + url.PathEscape(ref) + "/replicas/failover"
	if err := c.do(ctx, http.MethodPost, path, ProjectReplicaActionInput{Reason: reason}, &replica); err != nil {
		return ProjectReplica{}, err
	}
	return replica, nil
}

func (c *Client) GetProjectConfig(ctx context.Context, ref string, area string) (ProjectConfig, error) {
	var config ProjectConfig
	path := "/v1/projects/" + url.PathEscape(ref) + "/config/" + url.PathEscape(area)
	if err := c.do(ctx, http.MethodGet, path, nil, &config); err != nil {
		return ProjectConfig{}, err
	}
	return config, nil
}

func (c *Client) UpdateProjectConfig(ctx context.Context, ref string, area string, config map[string]string) (ProjectConfig, error) {
	var updated ProjectConfig
	path := "/v1/projects/" + url.PathEscape(ref) + "/config/" + url.PathEscape(area)
	if err := c.do(ctx, http.MethodPut, path, ProjectConfigInput{Config: config}, &updated); err != nil {
		return ProjectConfig{}, err
	}
	return updated, nil
}

func (c *Client) ListProjectAuthClients(ctx context.Context, ref string) ([]ProjectAuthClient, error) {
	var clients []ProjectAuthClient
	path := "/v1/projects/" + url.PathEscape(ref) + "/auth/clients"
	if err := c.do(ctx, http.MethodGet, path, nil, &clients); err != nil {
		return nil, err
	}
	return clients, nil
}

func (c *Client) CreateProjectAuthClient(ctx context.Context, ref string, input ProjectAuthClientInput) (ProjectAuthClient, error) {
	var client ProjectAuthClient
	path := "/v1/projects/" + url.PathEscape(ref) + "/auth/clients"
	if err := c.do(ctx, http.MethodPost, path, input, &client); err != nil {
		return ProjectAuthClient{}, err
	}
	return client, nil
}

func (c *Client) DeleteProjectAuthClient(ctx context.Context, ref string, clientID string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/auth/clients/" + url.PathEscape(clientID)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectAuthHooks(ctx context.Context, ref string) ([]ProjectAuthHook, error) {
	var hooks []ProjectAuthHook
	path := "/v1/projects/" + url.PathEscape(ref) + "/auth/hooks"
	if err := c.do(ctx, http.MethodGet, path, nil, &hooks); err != nil {
		return nil, err
	}
	return hooks, nil
}

func (c *Client) CreateProjectAuthHook(ctx context.Context, ref string, input ProjectAuthHookInput) (ProjectAuthHook, error) {
	var hook ProjectAuthHook
	path := "/v1/projects/" + url.PathEscape(ref) + "/auth/hooks"
	if err := c.do(ctx, http.MethodPost, path, input, &hook); err != nil {
		return ProjectAuthHook{}, err
	}
	return hook, nil
}

func (c *Client) DeleteProjectAuthHook(ctx context.Context, ref string, hookType string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/auth/hooks/" + url.PathEscape(hookType)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectDatabaseCronJobs(ctx context.Context, ref string) ([]ProjectDatabaseCronJob, error) {
	var jobs []ProjectDatabaseCronJob
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/cron-jobs"
	if err := c.do(ctx, http.MethodGet, path, nil, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (c *Client) CreateProjectDatabaseCronJob(ctx context.Context, ref string, input ProjectDatabaseCronJobInput) (ProjectDatabaseCronJob, error) {
	var job ProjectDatabaseCronJob
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/cron-jobs"
	if err := c.do(ctx, http.MethodPost, path, input, &job); err != nil {
		return ProjectDatabaseCronJob{}, err
	}
	return job, nil
}

func (c *Client) DeleteProjectDatabaseCronJob(ctx context.Context, ref string, name string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/cron-jobs/" + url.PathEscape(name)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectDatabaseQueues(ctx context.Context, ref string) ([]ProjectDatabaseQueue, error) {
	var queues []ProjectDatabaseQueue
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/queues"
	if err := c.do(ctx, http.MethodGet, path, nil, &queues); err != nil {
		return nil, err
	}
	return queues, nil
}

func (c *Client) CreateProjectDatabaseQueue(ctx context.Context, ref string, input ProjectDatabaseQueueInput) (ProjectDatabaseQueue, error) {
	var queue ProjectDatabaseQueue
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/queues"
	if err := c.do(ctx, http.MethodPost, path, input, &queue); err != nil {
		return ProjectDatabaseQueue{}, err
	}
	return queue, nil
}

func (c *Client) DeleteProjectDatabaseQueue(ctx context.Context, ref string, name string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/queues/" + url.PathEscape(name)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectDatabaseWebhooks(ctx context.Context, ref string) ([]ProjectDatabaseWebhook, error) {
	var webhooks []ProjectDatabaseWebhook
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/webhooks"
	if err := c.do(ctx, http.MethodGet, path, nil, &webhooks); err != nil {
		return nil, err
	}
	return webhooks, nil
}

func (c *Client) CreateProjectDatabaseWebhook(ctx context.Context, ref string, input ProjectDatabaseWebhookInput) (ProjectDatabaseWebhook, error) {
	var webhook ProjectDatabaseWebhook
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/webhooks"
	if err := c.do(ctx, http.MethodPost, path, input, &webhook); err != nil {
		return ProjectDatabaseWebhook{}, err
	}
	return webhook, nil
}

func (c *Client) DeleteProjectDatabaseWebhook(ctx context.Context, ref string, name string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/webhooks/" + url.PathEscape(name)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectDatabaseSchemas(ctx context.Context, ref string) ([]ProjectDatabaseSchema, error) {
	var schemas []ProjectDatabaseSchema
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/schemas"
	if err := c.do(ctx, http.MethodGet, path, nil, &schemas); err != nil {
		return nil, err
	}
	return schemas, nil
}

func (c *Client) CreateProjectDatabaseSchema(ctx context.Context, ref string, input ProjectDatabaseSchemaInput) (ProjectDatabaseSchema, error) {
	var schema ProjectDatabaseSchema
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/schemas"
	if err := c.do(ctx, http.MethodPost, path, input, &schema); err != nil {
		return ProjectDatabaseSchema{}, err
	}
	return schema, nil
}

func (c *Client) DeleteProjectDatabaseSchema(ctx context.Context, ref string, name string, version string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/schemas/" + url.PathEscape(name) + "/" + url.PathEscape(version)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectDatabaseRoles(ctx context.Context, ref string) ([]ProjectDatabaseRole, error) {
	var roles []ProjectDatabaseRole
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/roles"
	if err := c.do(ctx, http.MethodGet, path, nil, &roles); err != nil {
		return nil, err
	}
	return roles, nil
}

func (c *Client) CreateProjectDatabaseRole(ctx context.Context, ref string, input ProjectDatabaseRoleInput) (ProjectDatabaseRole, error) {
	var role ProjectDatabaseRole
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/roles"
	if err := c.do(ctx, http.MethodPost, path, input, &role); err != nil {
		return ProjectDatabaseRole{}, err
	}
	return role, nil
}

func (c *Client) DeleteProjectDatabaseRole(ctx context.Context, ref string, name string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/roles/" + url.PathEscape(name)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectDatabaseExtensions(ctx context.Context, ref string) ([]ProjectDatabaseExtension, error) {
	var extensions []ProjectDatabaseExtension
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/extensions"
	if err := c.do(ctx, http.MethodGet, path, nil, &extensions); err != nil {
		return nil, err
	}
	return extensions, nil
}

func (c *Client) UpdateProjectDatabaseExtension(ctx context.Context, ref string, name string, input ProjectDatabaseExtensionInput) (ProjectDatabaseExtension, error) {
	var extension ProjectDatabaseExtension
	path := "/v1/projects/" + url.PathEscape(ref) + "/database/extensions/" + url.PathEscape(name)
	if err := c.do(ctx, http.MethodPut, path, input, &extension); err != nil {
		return ProjectDatabaseExtension{}, err
	}
	return extension, nil
}

func (c *Client) ListProjectEmbeddingJobs(ctx context.Context, ref string) ([]ProjectEmbeddingJob, error) {
	var jobs []ProjectEmbeddingJob
	path := "/v1/projects/" + url.PathEscape(ref) + "/embeddings"
	if err := c.do(ctx, http.MethodGet, path, nil, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (c *Client) CreateProjectEmbeddingJob(ctx context.Context, ref string, input ProjectEmbeddingJobInput) (ProjectEmbeddingJob, error) {
	var job ProjectEmbeddingJob
	path := "/v1/projects/" + url.PathEscape(ref) + "/embeddings"
	if err := c.do(ctx, http.MethodPost, path, input, &job); err != nil {
		return ProjectEmbeddingJob{}, err
	}
	return job, nil
}

func (c *Client) DeleteProjectEmbeddingJob(ctx context.Context, ref string, id string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/embeddings/" + url.PathEscape(id)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectFunctions(ctx context.Context, ref string) ([]ProjectFunction, error) {
	var functions []ProjectFunction
	path := "/v1/projects/" + url.PathEscape(ref) + "/functions"
	if err := c.do(ctx, http.MethodGet, path, nil, &functions); err != nil {
		return nil, err
	}
	return functions, nil
}

func (c *Client) DeployProjectFunction(ctx context.Context, ref string, input ProjectFunctionInput) (ProjectFunction, error) {
	var function ProjectFunction
	path := "/v1/projects/" + url.PathEscape(ref) + "/functions"
	if err := c.do(ctx, http.MethodPost, path, input, &function); err != nil {
		return ProjectFunction{}, err
	}
	return function, nil
}

func (c *Client) DeleteProjectFunction(ctx context.Context, ref string, name string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/functions/" + url.PathEscape(name)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectFunctionRegions(ctx context.Context, ref string) ([]ProjectFunctionRegion, error) {
	var regions []ProjectFunctionRegion
	path := "/v1/projects/" + url.PathEscape(ref) + "/functions/regions"
	if err := c.do(ctx, http.MethodGet, path, nil, &regions); err != nil {
		return nil, err
	}
	return regions, nil
}

func (c *Client) CreateProjectFunctionRegion(ctx context.Context, ref string, input ProjectFunctionRegionInput) (ProjectFunctionRegion, error) {
	var region ProjectFunctionRegion
	path := "/v1/projects/" + url.PathEscape(ref) + "/functions/regions"
	if err := c.do(ctx, http.MethodPost, path, input, &region); err != nil {
		return ProjectFunctionRegion{}, err
	}
	return region, nil
}

func (c *Client) DeleteProjectFunctionRegion(ctx context.Context, ref string, id string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/functions/regions/" + url.PathEscape(id)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectFunctionStorageMounts(ctx context.Context, ref string) ([]ProjectFunctionStorageMount, error) {
	var mounts []ProjectFunctionStorageMount
	path := "/v1/projects/" + url.PathEscape(ref) + "/functions/storage-mounts"
	if err := c.do(ctx, http.MethodGet, path, nil, &mounts); err != nil {
		return nil, err
	}
	return mounts, nil
}

func (c *Client) CreateProjectFunctionStorageMount(ctx context.Context, ref string, input ProjectFunctionStorageMountInput) (ProjectFunctionStorageMount, error) {
	var mount ProjectFunctionStorageMount
	path := "/v1/projects/" + url.PathEscape(ref) + "/functions/storage-mounts"
	if err := c.do(ctx, http.MethodPost, path, input, &mount); err != nil {
		return ProjectFunctionStorageMount{}, err
	}
	return mount, nil
}

func (c *Client) DeleteProjectFunctionStorageMount(ctx context.Context, ref string, id string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/functions/storage-mounts/" + url.PathEscape(id)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectDomains(ctx context.Context, ref string) ([]ProjectDomain, error) {
	var domains []ProjectDomain
	path := "/v1/projects/" + url.PathEscape(ref) + "/domains"
	if err := c.do(ctx, http.MethodGet, path, nil, &domains); err != nil {
		return nil, err
	}
	return domains, nil
}

func (c *Client) AddProjectDomain(ctx context.Context, ref string, fqdn string) (ProjectDomain, error) {
	var domain ProjectDomain
	path := "/v1/projects/" + url.PathEscape(ref) + "/domains"
	if err := c.do(ctx, http.MethodPost, path, ProjectDomainInput{FQDN: fqdn}, &domain); err != nil {
		return ProjectDomain{}, err
	}
	return domain, nil
}

func (c *Client) DeleteProjectDomain(ctx context.Context, ref string, fqdn string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/domains/" + url.PathEscape(fqdn)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectLogDrains(ctx context.Context, ref string) ([]ProjectLogDrain, error) {
	var drains []ProjectLogDrain
	path := "/v1/projects/" + url.PathEscape(ref) + "/log-drains"
	if err := c.do(ctx, http.MethodGet, path, nil, &drains); err != nil {
		return nil, err
	}
	return drains, nil
}

func (c *Client) CreateProjectLogDrain(ctx context.Context, ref string, input ProjectLogDrainInput) (ProjectLogDrain, error) {
	var drain ProjectLogDrain
	path := "/v1/projects/" + url.PathEscape(ref) + "/log-drains"
	if err := c.do(ctx, http.MethodPost, path, input, &drain); err != nil {
		return ProjectLogDrain{}, err
	}
	return drain, nil
}

func (c *Client) DeleteProjectLogDrain(ctx context.Context, ref string, id string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/log-drains/" + url.PathEscape(id)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectNetworkConnections(ctx context.Context, ref string) ([]ProjectNetworkConnection, error) {
	var connections []ProjectNetworkConnection
	path := "/v1/projects/" + url.PathEscape(ref) + "/network-connections"
	if err := c.do(ctx, http.MethodGet, path, nil, &connections); err != nil {
		return nil, err
	}
	return connections, nil
}

func (c *Client) CreateProjectNetworkConnection(ctx context.Context, ref string, input ProjectNetworkConnectionInput) (ProjectNetworkConnection, error) {
	var connection ProjectNetworkConnection
	path := "/v1/projects/" + url.PathEscape(ref) + "/network-connections"
	if err := c.do(ctx, http.MethodPost, path, input, &connection); err != nil {
		return ProjectNetworkConnection{}, err
	}
	return connection, nil
}

func (c *Client) DeleteProjectNetworkConnection(ctx context.Context, ref string, id string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/network-connections/" + url.PathEscape(id)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectStorageBuckets(ctx context.Context, ref string) ([]ProjectStorageBucket, error) {
	var buckets []ProjectStorageBucket
	path := "/v1/projects/" + url.PathEscape(ref) + "/storage/buckets"
	if err := c.do(ctx, http.MethodGet, path, nil, &buckets); err != nil {
		return nil, err
	}
	return buckets, nil
}

func (c *Client) CreateProjectStorageBucket(ctx context.Context, ref string, input ProjectStorageBucketInput) (ProjectStorageBucket, error) {
	var bucket ProjectStorageBucket
	path := "/v1/projects/" + url.PathEscape(ref) + "/storage/buckets"
	if err := c.do(ctx, http.MethodPost, path, input, &bucket); err != nil {
		return ProjectStorageBucket{}, err
	}
	return bucket, nil
}

func (c *Client) DeleteProjectStorageBucket(ctx context.Context, ref string, name string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/storage/buckets/" + url.PathEscape(name)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectVectorBuckets(ctx context.Context, ref string) ([]ProjectVectorBucket, error) {
	var buckets []ProjectVectorBucket
	path := "/v1/projects/" + url.PathEscape(ref) + "/vector-buckets"
	if err := c.do(ctx, http.MethodGet, path, nil, &buckets); err != nil {
		return nil, err
	}
	return buckets, nil
}

func (c *Client) CreateProjectVectorBucket(ctx context.Context, ref string, input ProjectVectorBucketInput) (ProjectVectorBucket, error) {
	var bucket ProjectVectorBucket
	path := "/v1/projects/" + url.PathEscape(ref) + "/vector-buckets"
	if err := c.do(ctx, http.MethodPost, path, input, &bucket); err != nil {
		return ProjectVectorBucket{}, err
	}
	return bucket, nil
}

func (c *Client) DeleteProjectVectorBucket(ctx context.Context, ref string, name string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/vector-buckets/" + url.PathEscape(name)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectAnalyticsBuckets(ctx context.Context, ref string) ([]ProjectAnalyticsBucket, error) {
	var buckets []ProjectAnalyticsBucket
	path := "/v1/projects/" + url.PathEscape(ref) + "/analytics-buckets"
	if err := c.do(ctx, http.MethodGet, path, nil, &buckets); err != nil {
		return nil, err
	}
	return buckets, nil
}

func (c *Client) CreateProjectAnalyticsBucket(ctx context.Context, ref string, input ProjectAnalyticsBucketInput) (ProjectAnalyticsBucket, error) {
	var bucket ProjectAnalyticsBucket
	path := "/v1/projects/" + url.PathEscape(ref) + "/analytics-buckets"
	if err := c.do(ctx, http.MethodPost, path, input, &bucket); err != nil {
		return ProjectAnalyticsBucket{}, err
	}
	return bucket, nil
}

func (c *Client) DeleteProjectAnalyticsBucket(ctx context.Context, ref string, name string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/analytics-buckets/" + url.PathEscape(name)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListProjectReplicationPipelines(ctx context.Context, ref string) ([]ProjectReplicationPipeline, error) {
	var pipelines []ProjectReplicationPipeline
	path := "/v1/projects/" + url.PathEscape(ref) + "/replication"
	if err := c.do(ctx, http.MethodGet, path, nil, &pipelines); err != nil {
		return nil, err
	}
	return pipelines, nil
}

func (c *Client) CreateProjectReplicationPipeline(ctx context.Context, ref string, input ProjectReplicationPipelineInput) (ProjectReplicationPipeline, error) {
	var pipeline ProjectReplicationPipeline
	path := "/v1/projects/" + url.PathEscape(ref) + "/replication"
	if err := c.do(ctx, http.MethodPost, path, input, &pipeline); err != nil {
		return ProjectReplicationPipeline{}, err
	}
	return pipeline, nil
}

func (c *Client) DeleteProjectReplicationPipeline(ctx context.Context, ref string, id string) error {
	path := "/v1/projects/" + url.PathEscape(ref) + "/replication/" + url.PathEscape(id)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) GetProjectCDNPolicy(ctx context.Context, ref string) (ProjectCDNPolicy, error) {
	var policy ProjectCDNPolicy
	path := "/v1/projects/" + url.PathEscape(ref) + "/cdn/policy"
	if err := c.do(ctx, http.MethodGet, path, nil, &policy); err != nil {
		return ProjectCDNPolicy{}, err
	}
	return policy, nil
}

func (c *Client) UpdateProjectCDNPolicy(ctx context.Context, ref string, input ProjectCDNPolicyInput) (ProjectCDNPolicy, error) {
	var policy ProjectCDNPolicy
	path := "/v1/projects/" + url.PathEscape(ref) + "/cdn/policy"
	if err := c.do(ctx, http.MethodPut, path, input, &policy); err != nil {
		return ProjectCDNPolicy{}, err
	}
	return policy, nil
}

func (c *Client) do(ctx context.Context, method string, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("supadupa API %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func normalizeBaseURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "http://localhost:8080"
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("api_url must include scheme and host")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}
