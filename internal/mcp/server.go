package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	maxMCPMessageBytes    = 1024 * 1024
	maxMCPHeaderLineBytes = 64 * 1024
	maxManagementAPIBytes = 10 * 1024 * 1024
)

type Runner struct {
	HTTPClient *http.Client
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Env        map[string]string
}

func (r Runner) Run(ctx context.Context, args []string) int {
	if r.Stdin == nil {
		r.Stdin = os.Stdin
	}
	if r.Stdout == nil {
		r.Stdout = os.Stdout
	}
	if r.Stderr == nil {
		r.Stderr = os.Stderr
	}
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	flags := flag.NewFlagSet("supadupa-mcp", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	apiURL := flags.String("api", r.env("SUPADUPA_API_URL", "http://localhost:8080"), "Management API base URL")
	token := flags.String("token", r.env("SUPADUPA_TOKEN", ""), "Bearer token")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	base, err := normalizeBaseURL(*apiURL)
	if err != nil {
		fmt.Fprintf(r.Stderr, "invalid --api: %v\n", err)
		return 2
	}
	server := server{api: apiClient{baseURL: base, token: *token, client: client}}
	if err := server.serve(ctx, r.Stdin, r.Stdout); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(r.Stderr, err)
		return 1
	}
	return 0
}

func (r Runner) env(key string, fallback string) string {
	if r.Env != nil {
		if value, ok := r.Env[key]; ok {
			return value
		}
	}
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

type server struct {
	api apiClient
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type refArgs struct {
	Ref string `json:"ref"`
}

type projectConfigArgs struct {
	Ref    string            `json:"ref"`
	Area   string            `json:"area"`
	Config map[string]string `json:"config"`
}

type userCreateArgs struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type mfaCodeArgs struct {
	Code string `json:"code"`
}

type scimArgs struct {
	ID          string   `json:"id"`
	OrgID       string   `json:"org_id"`
	UserName    string   `json:"user_name"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Password    string   `json:"password"`
	Role        string   `json:"role"`
	Active      *bool    `json:"active"`
	Slug        string   `json:"slug"`
	Members     []string `json:"members"`
}

type orgCreateArgs struct {
	Name string `json:"name"`
}

type orgUpdateArgs struct {
	OrgID string `json:"org_id"`
	Name  string `json:"name"`
}

type projectCreateArgs struct {
	OrgID        string            `json:"org_id"`
	Ref          string            `json:"ref"`
	Name         string            `json:"name"`
	HostID       string            `json:"host_id"`
	Domain       string            `json:"domain"`
	StackVersion string            `json:"stack_version"`
	Profile      string            `json:"profile"`
	ResourceTier string            `json:"resource_tier"`
	Services     map[string]bool   `json:"services"`
	Environment  map[string]string `json:"environment"`
}

type projectServicesArgs struct {
	Ref      string          `json:"ref"`
	Services map[string]bool `json:"services"`
}

type orgMemberArgs struct {
	OrgID string `json:"org_id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type orgTeamArgs struct {
	OrgID string `json:"org_id"`
	Slug  string `json:"slug"`
	Name  string `json:"name"`
}

type orgTeamMemberArgs struct {
	OrgID string `json:"org_id"`
	Slug  string `json:"slug"`
	Email string `json:"email"`
}

type orgQuotaArgs struct {
	OrgID       string `json:"org_id"`
	MaxProjects int    `json:"max_projects"`
	MaxCPU      int    `json:"max_cpu"`
	MaxRAMMB    int    `json:"max_ram_mb"`
	MaxDiskGB   int    `json:"max_disk_gb"`
}

type hostArgs struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Address          string `json:"address"`
	CapacityCPU      int    `json:"capacity_cpu"`
	CapacityRAMMB    int    `json:"capacity_ram_mb"`
	CapacityDiskGB   int    `json:"capacity_disk_gb"`
	CapacityProjects int    `json:"capacity_projects"`
}

type platformDefaultsArgs struct {
	Domain             string `json:"domain"`
	StackVersion       string `json:"stack_version"`
	Profile            string `json:"profile"`
	ResourceTier       string `json:"resource_tier"`
	BackupSchedule     string `json:"backup_schedule"`
	SMTPEnabled        bool   `json:"smtp_enabled"`
	SMTPHost           string `json:"smtp_host"`
	SMTPPort           int    `json:"smtp_port"`
	SMTPSenderName     string `json:"smtp_sender_name"`
	SMTPSenderEmail    string `json:"smtp_sender_email"`
	SMTPUsername       string `json:"smtp_username"`
	SMTPPasswordHandle string `json:"smtp_password_handle"`
	SMTPTLSMode        string `json:"smtp_tls_mode"`
}

type platformSSOArgs struct {
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

type projectAccessArgs struct {
	Ref         string `json:"ref"`
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Role        string `json:"role"`
}

type projectBranchArgs struct {
	Ref       string `json:"ref"`
	BranchRef string `json:"branch_ref"`
	Name      string `json:"name"`
	TTLHours  int    `json:"ttl_hours"`
	WithData  bool   `json:"with_data"`
}

type projectReplicaArgs struct {
	Ref              string `json:"ref"`
	Name             string `json:"name"`
	HostID           string `json:"host_id"`
	Region           string `json:"region"`
	Tier             string `json:"tier"`
	ReadWeight       int    `json:"read_weight"`
	FailoverPriority int    `json:"failover_priority"`
}

type projectReplicaActionArgs struct {
	Ref    string `json:"ref"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type projectLifecycleArgs struct {
	Ref           string `json:"ref"`
	Version       string `json:"version"`
	Tier          string `json:"tier"`
	RetainVolumes bool   `json:"retain_volumes"`
}

type projectBackupRestoreArgs struct {
	Ref      string `json:"ref"`
	BackupID string `json:"backup_id"`
}

type projectBackupPolicyArgs struct {
	Ref      string `json:"ref"`
	Enabled  bool   `json:"enabled"`
	Schedule string `json:"schedule"`
	Kind     string `json:"kind"`
}

type projectPITRPolicyArgs struct {
	Ref           string `json:"ref"`
	Enabled       bool   `json:"enabled"`
	ArchiveBucket string `json:"archive_bucket"`
	RetentionDays int    `json:"retention_days"`
}

type projectSecretArgs struct {
	Ref  string `json:"ref"`
	Kind string `json:"kind"`
}

type projectDomainArgs struct {
	Ref  string `json:"ref"`
	FQDN string `json:"fqdn"`
}

type projectLogDrainArgs struct {
	Ref    string            `json:"ref"`
	Target string            `json:"target"`
	Config map[string]string `json:"config"`
}

type projectNetworkConnectionArgs struct {
	Ref        string            `json:"ref"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Provider   string            `json:"provider"`
	Region     string            `json:"region"`
	CIDRs      []string          `json:"cidrs"`
	EndpointID string            `json:"endpoint_id"`
	Config     map[string]string `json:"config"`
}

type projectCDNPolicyArgs struct {
	Ref                         string   `json:"ref"`
	Enabled                     bool     `json:"enabled"`
	BrowserTTLSeconds           int      `json:"browser_ttl_seconds"`
	EdgeTTLSeconds              int      `json:"edge_ttl_seconds"`
	StaleWhileRevalidateSeconds int      `json:"stale_while_revalidate_seconds"`
	IncludedPaths               []string `json:"included_paths"`
	ExcludedPaths               []string `json:"excluded_paths"`
	SmartRevalidation           bool     `json:"smart_revalidation"`
	CacheControl                string   `json:"cache_control"`
}

type projectCDNInvalidationArgs struct {
	Ref   string   `json:"ref"`
	Paths []string `json:"paths"`
}

type projectCDNObjectEventArgs struct {
	Ref        string `json:"ref"`
	EventID    string `json:"event_id"`
	Bucket     string `json:"bucket"`
	ObjectPath string `json:"object_path"`
	EventType  string `json:"event_type"`
}

type projectDatabaseExtensionArgs struct {
	Ref     string `json:"ref"`
	Name    string `json:"name"`
	Schema  string `json:"schema"`
	Version string `json:"version"`
	Enabled *bool  `json:"enabled"`
}

type projectDatabaseCronJobArgs struct {
	Ref               string            `json:"ref"`
	Name              string            `json:"name"`
	Schedule          string            `json:"schedule"`
	Command           string            `json:"command"`
	Database          string            `json:"database"`
	Username          string            `json:"username"`
	Active            *bool             `json:"active"`
	TimeoutSeconds    int               `json:"timeout_seconds"`
	MaxRuntimeSeconds int               `json:"max_runtime_seconds"`
	Metadata          map[string]string `json:"metadata"`
}

type projectDatabaseQueueArgs struct {
	Ref                      string            `json:"ref"`
	Name                     string            `json:"name"`
	Schema                   string            `json:"schema"`
	RetentionMinutes         int               `json:"retention_minutes"`
	VisibilityTimeoutSeconds int               `json:"visibility_timeout_seconds"`
	MaxRetries               int               `json:"max_retries"`
	DeadLetterQueue          string            `json:"dead_letter_queue"`
	Active                   *bool             `json:"active"`
	Metadata                 map[string]string `json:"metadata"`
}

type projectDatabaseWebhookArgs struct {
	Ref            string            `json:"ref"`
	Name           string            `json:"name"`
	Schema         string            `json:"schema"`
	Table          string            `json:"table"`
	Events         []string          `json:"events"`
	Endpoint       string            `json:"endpoint"`
	HTTPMethod     string            `json:"http_method"`
	Headers        map[string]string `json:"headers"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	RetryCount     int               `json:"retry_count"`
	Active         *bool             `json:"active"`
	Metadata       map[string]string `json:"metadata"`
}

type projectDatabaseSchemaArgs struct {
	Ref        string            `json:"ref"`
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	Schema     string            `json:"schema"`
	SQL        string            `json:"sql"`
	ApplyOrder int               `json:"apply_order"`
	Active     *bool             `json:"active"`
	Metadata   map[string]string `json:"metadata"`
}

type projectDatabaseSchemaKeyArgs struct {
	Ref     string `json:"ref"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type projectDatabaseRoleArgs struct {
	Ref                  string            `json:"ref"`
	Name                 string            `json:"name"`
	Login                bool              `json:"login"`
	Inherit              *bool             `json:"inherit"`
	BypassRLS            bool              `json:"bypass_rls"`
	ConnectionLimit      int               `json:"connection_limit"`
	PasswordSecretHandle string            `json:"password_secret_handle"`
	MemberOf             []string          `json:"member_of"`
	SchemaGrants         map[string]string `json:"schema_grants"`
	Metadata             map[string]string `json:"metadata"`
}

type projectAuthClientArgs struct {
	Ref                string   `json:"ref"`
	Name               string   `json:"name"`
	ClientID           string   `json:"client_id"`
	ClientSecretHandle string   `json:"client_secret_handle"`
	RedirectURIs       []string `json:"redirect_uris"`
	GrantTypes         []string `json:"grant_types"`
	Scopes             []string `json:"scopes"`
	Confidential       *bool    `json:"confidential"`
}

type projectAuthClientIDArgs struct {
	Ref      string `json:"ref"`
	ClientID string `json:"client_id"`
}

type projectAuthHookArgs struct {
	Ref           string            `json:"ref"`
	HookType      string            `json:"hook_type"`
	Enabled       *bool             `json:"enabled"`
	TargetURI     string            `json:"target_uri"`
	EdgeFunction  string            `json:"edge_function"`
	SecretHandle  string            `json:"secret_handle"`
	Headers       map[string]string `json:"headers"`
	TimeoutMS     int               `json:"timeout_ms"`
	RetryAttempts int               `json:"retry_attempts"`
}

type projectAuthHookTypeArgs struct {
	Ref      string `json:"ref"`
	HookType string `json:"hook_type"`
}

type projectStorageBucketArgs struct {
	Ref               string            `json:"ref"`
	Name              string            `json:"name"`
	Public            bool              `json:"public"`
	FileSizeLimit     int64             `json:"file_size_limit"`
	AllowedMimeTypes  []string          `json:"allowed_mime_types"`
	CacheControl      string            `json:"cache_control"`
	AvifAutodetection bool              `json:"avif_autodetection"`
	Metadata          map[string]string `json:"metadata"`
}

type projectVectorBucketArgs struct {
	Ref            string            `json:"ref"`
	Name           string            `json:"name"`
	Dimension      int               `json:"dimension"`
	Distance       string            `json:"distance"`
	IndexMethod    string            `json:"index_method"`
	StorageBackend string            `json:"storage_backend"`
	StorageURI     string            `json:"storage_uri"`
	Metadata       map[string]string `json:"metadata"`
}

type projectAnalyticsBucketArgs struct {
	Ref                string            `json:"ref"`
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

type projectFunctionArgs struct {
	Ref        string            `json:"ref"`
	Name       string            `json:"name"`
	Entrypoint string            `json:"entrypoint"`
	VerifyJWT  *bool             `json:"verify_jwt"`
	Source     string            `json:"source"`
	Secrets    map[string]string `json:"secrets"`
}

type projectFunctionNameArgs struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
}

type projectFunctionRegionArgs struct {
	Ref           string `json:"ref"`
	FunctionName  string `json:"function_name"`
	HostID        string `json:"host_id"`
	Region        string `json:"region"`
	RoutingPolicy string `json:"routing_policy"`
}

type projectFunctionRecordArgs struct {
	Ref string `json:"ref"`
	ID  string `json:"id"`
}

type projectFunctionStorageMountArgs struct {
	Ref          string `json:"ref"`
	FunctionName string `json:"function_name"`
	BucketName   string `json:"bucket_name"`
	MountPath    string `json:"mount_path"`
	ReadOnly     *bool  `json:"read_only"`
	Prefix       string `json:"prefix"`
	EnvAlias     string `json:"env_alias"`
}

type projectReplicationPipelineArgs struct {
	Ref              string            `json:"ref"`
	Name             string            `json:"name"`
	Type             string            `json:"type"`
	SourceSchema     string            `json:"source_schema"`
	SourceTable      string            `json:"source_table"`
	Destination      string            `json:"destination"`
	DestinationURI   string            `json:"destination_uri"`
	CredentialHandle string            `json:"credential_handle"`
	Config           map[string]string `json:"config"`
}

type projectEmbeddingJobArgs struct {
	Ref               string `json:"ref"`
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

type orgArgs struct {
	OrgID string `json:"org_id"`
	Limit int    `json:"limit"`
}

type billingInvoiceArgs struct {
	OrgID           string `json:"org_id"`
	InvoiceID       string `json:"invoice_id"`
	UsageSnapshotID string `json:"usage_snapshot_id"`
	Currency        string `json:"currency"`
	Status          string `json:"status"`
	DueDays         int    `json:"due_days"`
}

func (s server) serve(ctx context.Context, input io.Reader, output io.Writer) error {
	reader := bufio.NewReader(input)
	for {
		payload, err := readMessage(reader)
		if err != nil {
			return err
		}
		var request rpcRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			if err := writeMessage(output, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: err.Error()}}); err != nil {
				return err
			}
			continue
		}
		if len(request.ID) == 0 {
			continue
		}
		response := s.handle(ctx, request)
		if err := writeMessage(output, response); err != nil {
			return err
		}
	}
}

func (s server) handle(ctx context.Context, request rpcRequest) rpcResponse {
	response := rpcResponse{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "initialize":
		response.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo":      map[string]string{"name": "supadupa-mcp", "version": "0.1.0"},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		}
	case "tools/list":
		response.Result = map[string]any{"tools": tools()}
	case "tools/call":
		result, err := s.callTool(ctx, request.Params)
		if err != nil {
			response.Error = &rpcError{Code: -32000, Message: err.Error()}
			return response
		}
		response.Result = result
	default:
		response.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	return response
}

func (s server) callTool(ctx context.Context, payload json.RawMessage) (any, error) {
	var params toolCallParams
	if err := json.Unmarshal(payload, &params); err != nil {
		return nil, err
	}
	var method, path string
	var body any
	switch params.Name {
	case "supadupa_list_users":
		method, path = http.MethodGet, "/v1/users"
	case "supadupa_create_user":
		args, err := decodeUserCreateArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/users"
		body = map[string]string{"email": args.Email, "password": args.Password, "role": args.Role}
	case "supadupa_get_provisioner":
		method, path = http.MethodGet, "/v1/provisioner"
	case "supadupa_get_account_mfa":
		method, path = http.MethodGet, "/v1/account/mfa"
	case "supadupa_enroll_account_mfa":
		method, path = http.MethodPost, "/v1/account/mfa/enroll"
	case "supadupa_verify_account_mfa":
		args, err := decodeMFACodeArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/account/mfa/verify"
		body = map[string]string{"code": args.Code}
	case "supadupa_disable_account_mfa":
		args, err := decodeMFACodeArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/account/mfa"
		body = map[string]string{"code": args.Code}
	case "supadupa_get_scim_service_provider_config":
		method, path = http.MethodGet, "/v1/scim/v2/ServiceProviderConfig"
	case "supadupa_list_scim_users":
		method, path = http.MethodGet, "/v1/scim/v2/Users"
	case "supadupa_get_scim_user":
		args, err := decodeSCIMArgs(params.Arguments, true)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/scim/v2/Users/"+url.PathEscape(args.ID)
	case "supadupa_create_scim_user":
		args, err := decodeSCIMArgs(params.Arguments, false)
		if err != nil {
			return nil, err
		}
		userPayload, err := scimUserPayload(args)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/scim/v2/Users"
		body = userPayload
	case "supadupa_replace_scim_user":
		args, err := decodeSCIMArgs(params.Arguments, true)
		if err != nil {
			return nil, err
		}
		userPayload, err := scimUserPayload(args)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPut, "/v1/scim/v2/Users/"+url.PathEscape(args.ID)
		body = userPayload
	case "supadupa_deprovision_scim_user":
		args, err := decodeSCIMArgs(params.Arguments, true)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPatch, "/v1/scim/v2/Users/"+url.PathEscape(args.ID)
		body = scimDeprovisionPayload()
	case "supadupa_delete_scim_user":
		args, err := decodeSCIMArgs(params.Arguments, true)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/scim/v2/Users/"+url.PathEscape(args.ID)
	case "supadupa_list_scim_groups":
		args, err := decodeSCIMArgs(params.Arguments, false)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/scim/v2/Groups"
		if args.OrgID != "" {
			path += "?org_id=" + url.QueryEscape(args.OrgID)
		}
	case "supadupa_get_scim_group":
		args, err := decodeSCIMArgs(params.Arguments, true)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/scim/v2/Groups/"+url.PathEscape(args.ID)
	case "supadupa_create_scim_group":
		args, err := decodeSCIMArgs(params.Arguments, false)
		if err != nil {
			return nil, err
		}
		groupPayload, err := scimGroupPayload(args)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/scim/v2/Groups"
		body = groupPayload
	case "supadupa_delete_scim_group":
		args, err := decodeSCIMArgs(params.Arguments, true)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/scim/v2/Groups/"+url.PathEscape(args.ID)
	case "supadupa_list_orgs":
		method, path = http.MethodGet, "/v1/orgs"
	case "supadupa_create_org":
		args, err := decodeOrgCreateArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/orgs"
		body = map[string]string{"name": args.Name}
	case "supadupa_get_org":
		args, err := decodeOrgArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/orgs/"+url.PathEscape(args.OrgID)
	case "supadupa_update_org":
		args, err := decodeOrgUpdateArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPut, "/v1/orgs/"+url.PathEscape(args.OrgID)
		body = map[string]string{"name": args.Name}
	case "supadupa_delete_org":
		args, err := decodeOrgArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/orgs/"+url.PathEscape(args.OrgID)
	case "supadupa_list_hosts":
		method, path = http.MethodGet, "/v1/hosts"
	case "supadupa_create_host":
		args, err := decodeHostArgs(params.Arguments, true)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/hosts"
		body = map[string]any{
			"name":    args.Name,
			"address": args.Address,
			"capacity": map[string]int{
				"cpu":      args.CapacityCPU,
				"ram_mb":   args.CapacityRAMMB,
				"disk_gb":  args.CapacityDiskGB,
				"projects": args.CapacityProjects,
			},
		}
	case "supadupa_get_host":
		args, err := decodeHostArgs(params.Arguments, false)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/hosts/"+url.PathEscape(args.ID)
	case "supadupa_delete_host":
		args, err := decodeHostArgs(params.Arguments, false)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/hosts/"+url.PathEscape(args.ID)
	case "supadupa_get_fleet_metrics":
		method, path = http.MethodGet, "/v1/metrics"
	case "supadupa_get_advisor_findings":
		method, path = http.MethodGet, "/v1/advisor"
	case "supadupa_get_compliance_report":
		method, path = http.MethodGet, "/v1/compliance/report"
	case "supadupa_list_audit_events":
		method, path = http.MethodGet, "/v1/audit-events"
	case "supadupa_get_audit_integrity":
		method, path = http.MethodGet, "/v1/audit-events/integrity"
	case "supadupa_get_platform_defaults":
		method, path = http.MethodGet, "/v1/settings/defaults"
	case "supadupa_set_platform_defaults":
		args, err := decodePlatformDefaultsArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPut, "/v1/settings/defaults"
		body = map[string]any{
			"domain":          args.Domain,
			"stack_version":   args.StackVersion,
			"profile":         args.Profile,
			"resource_tier":   args.ResourceTier,
			"backup_schedule": args.BackupSchedule,
			"smtp": map[string]any{
				"enabled":         args.SMTPEnabled,
				"host":            args.SMTPHost,
				"port":            args.SMTPPort,
				"sender_name":     args.SMTPSenderName,
				"sender_email":    args.SMTPSenderEmail,
				"username":        args.SMTPUsername,
				"password_handle": args.SMTPPasswordHandle,
				"tls_mode":        args.SMTPTLSMode,
			},
		}
	case "supadupa_get_platform_sso":
		method, path = http.MethodGet, "/v1/settings/sso"
	case "supadupa_set_platform_sso":
		args, err := decodePlatformSSOArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPut, "/v1/settings/sso"
		body = map[string]any{
			"enabled":         args.Enabled,
			"idp_entity_id":   args.IDPEntityID,
			"sso_url":         args.SSOURL,
			"certificate_pem": args.Certificate,
			"acs_url":         args.ACSURL,
			"metadata_url":    args.MetadataURL,
			"email_domain":    args.EmailDomain,
			"auto_provision":  args.AutoProvision,
			"default_role":    args.DefaultRole,
		}
	case "supadupa_get_org_usage":
		args, err := decodeOrgArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/usage"
	case "supadupa_get_org_quota":
		args, err := decodeOrgArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/quotas"
	case "supadupa_set_org_quota":
		args, err := decodeOrgQuotaArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPut, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/quotas"
		body = map[string]int{
			"max_projects": args.MaxProjects,
			"max_cpu":      args.MaxCPU,
			"max_ram_mb":   args.MaxRAMMB,
			"max_disk_gb":  args.MaxDiskGB,
		}
	case "supadupa_list_org_usage_snapshots":
		args, err := decodeOrgArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/usage/snapshots?limit="+strconv.Itoa(limitOrDefault(args.Limit))
	case "supadupa_create_org_usage_snapshot":
		args, err := decodeOrgArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/usage/snapshots"
	case "supadupa_list_billing_invoices":
		args, err := decodeOrgArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/billing/invoices?limit="+strconv.Itoa(limitOrDefault(args.Limit))
	case "supadupa_create_billing_invoice":
		args, err := decodeBillingInvoiceArgs(params.Arguments, false)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/billing/invoices"
		body = map[string]any{
			"usage_snapshot_id": args.UsageSnapshotID,
			"currency":          args.Currency,
			"status":            args.Status,
			"due_days":          args.DueDays,
		}
	case "supadupa_get_billing_invoice":
		args, err := decodeBillingInvoiceArgs(params.Arguments, true)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/billing/invoices/"+url.PathEscape(args.InvoiceID)
	case "supadupa_list_org_members":
		args, err := decodeOrgArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/members"
	case "supadupa_upsert_org_member":
		args, err := decodeOrgMemberArgs(params.Arguments, true)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/members"
		body = map[string]string{"email": args.Email, "role": args.Role}
	case "supadupa_delete_org_member":
		args, err := decodeOrgMemberArgs(params.Arguments, false)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/members/"+url.PathEscape(args.Email)
	case "supadupa_list_org_teams":
		args, err := decodeOrgArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/teams"
	case "supadupa_create_org_team":
		args, err := decodeOrgTeamArgs(params.Arguments, true)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/teams"
		body = map[string]string{"name": args.Name, "slug": args.Slug}
	case "supadupa_delete_org_team":
		args, err := decodeOrgTeamArgs(params.Arguments, false)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/teams/"+url.PathEscape(args.Slug)
	case "supadupa_list_org_team_members":
		args, err := decodeOrgTeamMemberArgs(params.Arguments, false)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/teams/"+url.PathEscape(args.Slug)+"/members"
	case "supadupa_add_org_team_member":
		args, err := decodeOrgTeamMemberArgs(params.Arguments, true)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/teams/"+url.PathEscape(args.Slug)+"/members"
		body = map[string]string{"email": args.Email}
	case "supadupa_delete_org_team_member":
		args, err := decodeOrgTeamMemberArgs(params.Arguments, true)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/teams/"+url.PathEscape(args.Slug)+"/members/"+url.PathEscape(args.Email)
	case "supadupa_list_projects":
		method, path = http.MethodGet, "/v1/projects"
	case "supadupa_list_org_projects":
		args, err := decodeOrgArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/projects"
	case "supadupa_create_project":
		args, err := decodeProjectCreateArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/projects"
		projectBody := map[string]any{
			"ref":  args.Ref,
			"name": args.Name,
		}
		if args.HostID != "" {
			projectBody["host_id"] = args.HostID
		}
		if args.Domain != "" {
			projectBody["domain"] = args.Domain
		}
		if args.StackVersion != "" {
			projectBody["stack_version"] = args.StackVersion
		}
		if args.Profile != "" {
			projectBody["profile"] = args.Profile
		}
		if args.ResourceTier != "" {
			projectBody["resource_tier"] = args.ResourceTier
		}
		if len(args.Services) > 0 {
			projectBody["services"] = args.Services
		}
		if len(args.Environment) > 0 {
			projectBody["environment"] = args.Environment
		}
		body = projectBody
	case "supadupa_get_project":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)
	case "supadupa_project_connect":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/connect"
	case "supadupa_get_project_metrics":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/metrics"
	case "supadupa_get_project_config":
		args, err := decodeProjectConfigArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(args.Ref)+"/config/"+url.PathEscape(args.Area)
	case "supadupa_set_project_config":
		args, err := decodeProjectConfigWriteArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPut, "/v1/projects/"+url.PathEscape(args.Ref)+"/config/"+url.PathEscape(args.Area)
		body = map[string]any{"config": args.Config}
	case "supadupa_get_project_services":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/services"
	case "supadupa_set_project_services":
		args, err := decodeProjectServicesArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPut, "/v1/projects/"+url.PathEscape(args.Ref)+"/services"
		body = map[string]any{"services": args.Services}
	case "supadupa_list_project_database_extensions":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/database/extensions"
	case "supadupa_set_project_database_extension":
		args, err := decodeProjectDatabaseExtensionArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPut, "/v1/projects/"+url.PathEscape(args.Ref)+"/database/extensions/"+url.PathEscape(args.Name)
		body = map[string]any{"schema": args.Schema, "version": args.Version, "enabled": defaultBool(args.Enabled, true)}
	case "supadupa_list_project_database_cron_jobs":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/database/cron-jobs"
	case "supadupa_create_project_database_cron_job":
		args, err := decodeProjectDatabaseCronJobArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/database/cron-jobs"
		body = map[string]any{
			"name":                args.Name,
			"schedule":            args.Schedule,
			"command":             args.Command,
			"database":            args.Database,
			"username":            args.Username,
			"active":              defaultBool(args.Active, true),
			"timeout_seconds":     args.TimeoutSeconds,
			"max_runtime_seconds": args.MaxRuntimeSeconds,
			"metadata":            args.Metadata,
		}
	case "supadupa_delete_project_database_cron_job":
		args, err := decodeProjectFunctionNameArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/database/cron-jobs/"+url.PathEscape(args.Name)
	case "supadupa_list_project_database_queues":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/database/queues"
	case "supadupa_create_project_database_queue":
		args, err := decodeProjectDatabaseQueueArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/database/queues"
		body = map[string]any{
			"name":                       args.Name,
			"schema":                     args.Schema,
			"retention_minutes":          args.RetentionMinutes,
			"visibility_timeout_seconds": args.VisibilityTimeoutSeconds,
			"max_retries":                args.MaxRetries,
			"dead_letter_queue":          args.DeadLetterQueue,
			"active":                     defaultBool(args.Active, true),
			"metadata":                   args.Metadata,
		}
	case "supadupa_delete_project_database_queue":
		args, err := decodeProjectFunctionNameArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/database/queues/"+url.PathEscape(args.Name)
	case "supadupa_list_project_database_webhooks":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/database/webhooks"
	case "supadupa_create_project_database_webhook":
		args, err := decodeProjectDatabaseWebhookArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/database/webhooks"
		body = map[string]any{
			"name":            args.Name,
			"schema":          args.Schema,
			"table":           args.Table,
			"events":          args.Events,
			"endpoint":        args.Endpoint,
			"http_method":     args.HTTPMethod,
			"headers":         args.Headers,
			"timeout_seconds": args.TimeoutSeconds,
			"retry_count":     args.RetryCount,
			"active":          defaultBool(args.Active, true),
			"metadata":        args.Metadata,
		}
	case "supadupa_delete_project_database_webhook":
		args, err := decodeProjectFunctionNameArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/database/webhooks/"+url.PathEscape(args.Name)
	case "supadupa_list_project_database_schemas":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/database/schemas"
	case "supadupa_create_project_database_schema":
		args, err := decodeProjectDatabaseSchemaArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/database/schemas"
		body = map[string]any{
			"name":        args.Name,
			"version":     args.Version,
			"schema":      args.Schema,
			"sql":         args.SQL,
			"apply_order": args.ApplyOrder,
			"active":      defaultBool(args.Active, true),
			"metadata":    args.Metadata,
		}
	case "supadupa_delete_project_database_schema":
		args, err := decodeProjectDatabaseSchemaKeyArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/database/schemas/"+url.PathEscape(args.Name)+"/"+url.PathEscape(args.Version)
	case "supadupa_list_project_database_roles":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/database/roles"
	case "supadupa_create_project_database_role":
		args, err := decodeProjectDatabaseRoleArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/database/roles"
		body = map[string]any{
			"name":                   args.Name,
			"login":                  args.Login,
			"inherit":                defaultBool(args.Inherit, true),
			"bypass_rls":             args.BypassRLS,
			"connection_limit":       args.ConnectionLimit,
			"password_secret_handle": args.PasswordSecretHandle,
			"member_of":              args.MemberOf,
			"schema_grants":          args.SchemaGrants,
			"metadata":               args.Metadata,
		}
	case "supadupa_delete_project_database_role":
		args, err := decodeProjectFunctionNameArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/database/roles/"+url.PathEscape(args.Name)
	case "supadupa_list_project_auth_clients":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/auth/clients"
	case "supadupa_create_project_auth_client":
		args, err := decodeProjectAuthClientArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/auth/clients"
		body = map[string]any{
			"name":                 args.Name,
			"client_id":            args.ClientID,
			"client_secret_handle": args.ClientSecretHandle,
			"redirect_uris":        args.RedirectURIs,
			"grant_types":          args.GrantTypes,
			"scopes":               args.Scopes,
			"confidential":         *args.Confidential,
		}
	case "supadupa_delete_project_auth_client":
		args, err := decodeProjectAuthClientIDArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/auth/clients/"+url.PathEscape(args.ClientID)
	case "supadupa_list_project_auth_hooks":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/auth/hooks"
	case "supadupa_set_project_auth_hook":
		args, err := decodeProjectAuthHookArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/auth/hooks"
		body = map[string]any{
			"hook_type":      args.HookType,
			"enabled":        *args.Enabled,
			"target_uri":     args.TargetURI,
			"edge_function":  args.EdgeFunction,
			"secret_handle":  args.SecretHandle,
			"headers":        args.Headers,
			"timeout_ms":     args.TimeoutMS,
			"retry_attempts": args.RetryAttempts,
		}
	case "supadupa_delete_project_auth_hook":
		args, err := decodeProjectAuthHookTypeArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/auth/hooks/"+url.PathEscape(args.HookType)
	case "supadupa_list_project_activity":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/activity"
	case "supadupa_list_project_access":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/access"
	case "supadupa_grant_project_access":
		args, err := decodeProjectAccessArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPut, "/v1/projects/"+url.PathEscape(args.Ref)+"/access"
		body = map[string]string{"subject_type": args.SubjectType, "subject_id": args.SubjectID, "role": args.Role}
	case "supadupa_revoke_project_access":
		args, err := decodeProjectAccessArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/access/"+url.PathEscape(args.SubjectType)+"/"+url.PathEscape(args.SubjectID)
	case "supadupa_get_org_access_review":
		args, err := decodeOrgArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/orgs/"+url.PathEscape(args.OrgID)+"/access-review"
	case "supadupa_list_project_logs":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/logs"
	case "supadupa_list_project_backups":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/backups"
	case "supadupa_list_project_secrets":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/secrets"
	case "supadupa_reveal_project_secret":
		args, err := decodeProjectSecretArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(args.Ref)+"/secrets/"+url.PathEscape(args.Kind)+"/reveal"
	case "supadupa_record_project_secret_copy":
		args, err := decodeProjectSecretArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/secrets/"+url.PathEscape(args.Kind)+"/copy"
	case "supadupa_rotate_project_secret":
		args, err := decodeProjectSecretArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/keys/rotate"
		body = map[string]string{"kind": args.Kind}
	case "supadupa_get_project_backup_policy":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/backups/policy"
	case "supadupa_set_project_backup_policy":
		args, err := decodeProjectBackupPolicyArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPut, "/v1/projects/"+url.PathEscape(args.Ref)+"/backups/policy"
		body = map[string]any{"enabled": args.Enabled, "schedule": args.Schedule, "kind": args.Kind}
	case "supadupa_restore_project_backup":
		args, err := decodeProjectBackupRestoreArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/restore"
		body = map[string]string{"backup_id": args.BackupID}
	case "supadupa_get_project_pitr_policy":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/pitr/policy"
	case "supadupa_set_project_pitr_policy":
		args, err := decodeProjectPITRPolicyArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPut, "/v1/projects/"+url.PathEscape(args.Ref)+"/pitr/policy"
		body = map[string]any{"enabled": args.Enabled, "archive_bucket": args.ArchiveBucket, "retention_days": args.RetentionDays}
	case "supadupa_list_project_wal_archives":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/pitr/wal"
	case "supadupa_archive_project_wal":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(ref)+"/pitr/wal"
	case "supadupa_list_project_branches":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/branches"
	case "supadupa_create_project_branch":
		args, err := decodeProjectBranchArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/branches"
		body = map[string]any{"ref": args.BranchRef, "name": args.Name, "ttl_hours": args.TTLHours, "with_data": args.WithData}
	case "supadupa_delete_project_branch":
		args, err := decodeProjectBranchArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/branches/"+url.PathEscape(args.BranchRef)
	case "supadupa_list_project_replicas":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/replicas"
	case "supadupa_create_project_replica":
		args, err := decodeProjectReplicaArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/replicas"
		body = map[string]any{
			"name":              args.Name,
			"host_id":           args.HostID,
			"region":            args.Region,
			"tier":              args.Tier,
			"read_weight":       args.ReadWeight,
			"failover_priority": args.FailoverPriority,
		}
	case "supadupa_get_project_replica_routing":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/replicas/routing"
	case "supadupa_promote_project_replica":
		args, err := decodeProjectReplicaActionArgs(params.Arguments, true, "manual promotion")
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/replicas/"+url.PathEscape(args.ID)+"/promote"
		body = map[string]string{"reason": args.Reason}
	case "supadupa_delete_project_replica":
		args, err := decodeProjectReplicaActionArgs(params.Arguments, true, "")
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/replicas/"+url.PathEscape(args.ID)
	case "supadupa_failover_project_replica":
		args, err := decodeProjectReplicaActionArgs(params.Arguments, false, "automatic failover")
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/replicas/failover"
		body = map[string]string{"reason": args.Reason}
	case "supadupa_list_project_domains":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/domains"
	case "supadupa_add_project_domain":
		args, err := decodeProjectDomainArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/domains"
		body = map[string]string{"fqdn": args.FQDN}
	case "supadupa_delete_project_domain":
		args, err := decodeProjectDomainArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/domains/"+url.PathEscape(args.FQDN)
	case "supadupa_list_project_routes":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/routes"
	case "supadupa_list_project_log_drains":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/log-drains"
	case "supadupa_create_project_log_drain":
		args, err := decodeProjectLogDrainArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/log-drains"
		body = map[string]any{"target": args.Target, "config": args.Config}
	case "supadupa_delete_project_log_drain":
		args, err := decodeProjectFunctionRecordArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/log-drains/"+url.PathEscape(args.ID)
	case "supadupa_get_project_network":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/network"
	case "supadupa_list_project_network_connections":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/network-connections"
	case "supadupa_create_project_network_connection":
		args, err := decodeProjectNetworkConnectionArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/network-connections"
		body = map[string]any{
			"name":        args.Name,
			"type":        args.Type,
			"provider":    args.Provider,
			"region":      args.Region,
			"cidrs":       args.CIDRs,
			"endpoint_id": args.EndpointID,
			"config":      args.Config,
		}
	case "supadupa_delete_project_network_connection":
		args, err := decodeProjectFunctionRecordArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/network-connections/"+url.PathEscape(args.ID)
	case "supadupa_get_project_cdn_policy":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/cdn/policy"
	case "supadupa_set_project_cdn_policy":
		args, err := decodeProjectCDNPolicyArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPut, "/v1/projects/"+url.PathEscape(args.Ref)+"/cdn/policy"
		body = map[string]any{
			"enabled":                        args.Enabled,
			"browser_ttl_seconds":            args.BrowserTTLSeconds,
			"edge_ttl_seconds":               args.EdgeTTLSeconds,
			"stale_while_revalidate_seconds": args.StaleWhileRevalidateSeconds,
			"included_paths":                 args.IncludedPaths,
			"excluded_paths":                 args.ExcludedPaths,
			"smart_revalidation":             args.SmartRevalidation,
			"cache_control":                  args.CacheControl,
		}
	case "supadupa_list_project_cdn_invalidations":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/cdn/invalidations"
	case "supadupa_create_project_cdn_invalidation":
		args, err := decodeProjectCDNInvalidationArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/cdn/invalidations"
		body = map[string]any{"paths": args.Paths}
	case "supadupa_create_project_cdn_object_event":
		args, err := decodeProjectCDNObjectEventArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/cdn/object-events"
		body = map[string]any{
			"event_id":    args.EventID,
			"bucket":      args.Bucket,
			"object_path": args.ObjectPath,
			"event_type":  args.EventType,
		}
	case "supadupa_list_project_storage_buckets":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/storage/buckets"
	case "supadupa_create_project_storage_bucket":
		args, err := decodeProjectStorageBucketArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/storage/buckets"
		body = map[string]any{
			"name":               args.Name,
			"public":             args.Public,
			"file_size_limit":    args.FileSizeLimit,
			"allowed_mime_types": args.AllowedMimeTypes,
			"cache_control":      args.CacheControl,
			"avif_autodetection": args.AvifAutodetection,
			"metadata":           args.Metadata,
		}
	case "supadupa_delete_project_storage_bucket":
		args, err := decodeProjectFunctionNameArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/storage/buckets/"+url.PathEscape(args.Name)
	case "supadupa_list_project_vector_buckets":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/vector-buckets"
	case "supadupa_create_project_vector_bucket":
		args, err := decodeProjectVectorBucketArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/vector-buckets"
		body = map[string]any{
			"name":            args.Name,
			"dimension":       args.Dimension,
			"distance":        args.Distance,
			"index_method":    args.IndexMethod,
			"storage_backend": args.StorageBackend,
			"storage_uri":     args.StorageURI,
			"metadata":        args.Metadata,
		}
	case "supadupa_delete_project_vector_bucket":
		args, err := decodeProjectFunctionNameArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/vector-buckets/"+url.PathEscape(args.Name)
	case "supadupa_list_project_analytics_buckets":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/analytics-buckets"
	case "supadupa_create_project_analytics_bucket":
		args, err := decodeProjectAnalyticsBucketArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/analytics-buckets"
		body = map[string]any{
			"name":                args.Name,
			"storage_uri":         args.StorageURI,
			"catalog_uri":         args.CatalogURI,
			"warehouse":           args.Warehouse,
			"credential_handle":   args.CredentialHandle,
			"format_version":      args.FormatVersion,
			"partitioning":        args.Partitioning,
			"retention_days":      args.RetentionDays,
			"compaction_schedule": args.CompactionSchedule,
			"metadata":            args.Metadata,
		}
	case "supadupa_delete_project_analytics_bucket":
		args, err := decodeProjectFunctionNameArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/analytics-buckets/"+url.PathEscape(args.Name)
	case "supadupa_list_project_replication_pipelines":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/replication"
	case "supadupa_create_project_replication_pipeline":
		args, err := decodeProjectReplicationPipelineArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/replication"
		body = map[string]any{
			"name":              args.Name,
			"type":              args.Type,
			"source_schema":     args.SourceSchema,
			"source_table":      args.SourceTable,
			"destination":       args.Destination,
			"destination_uri":   args.DestinationURI,
			"credential_handle": args.CredentialHandle,
			"config":            args.Config,
		}
	case "supadupa_delete_project_replication_pipeline":
		args, err := decodeProjectFunctionRecordArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/replication/"+url.PathEscape(args.ID)
	case "supadupa_list_project_embedding_jobs":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/embeddings"
	case "supadupa_create_project_embedding_job":
		args, err := decodeProjectEmbeddingJobArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/embeddings"
		body = map[string]any{
			"name":               args.Name,
			"source_schema":      args.SourceSchema,
			"source_table":       args.SourceTable,
			"source_column":      args.SourceColumn,
			"primary_key_column": args.PrimaryKeyColumn,
			"destination_table":  args.DestinationTable,
			"destination_column": args.DestinationColumn,
			"provider":           args.Provider,
			"model":              args.Model,
			"dimension":          args.Dimension,
			"schedule":           args.Schedule,
			"batch_size":         args.BatchSize,
		}
	case "supadupa_delete_project_embedding_job":
		args, err := decodeProjectFunctionRecordArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/embeddings/"+url.PathEscape(args.ID)
	case "supadupa_list_project_functions":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/functions"
	case "supadupa_deploy_project_function":
		args, err := decodeProjectFunctionArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/functions"
		body = map[string]any{
			"name":       args.Name,
			"entrypoint": args.Entrypoint,
			"verify_jwt": *args.VerifyJWT,
			"source":     args.Source,
			"secrets":    args.Secrets,
		}
	case "supadupa_delete_project_function":
		args, err := decodeProjectFunctionNameArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/functions/"+url.PathEscape(args.Name)
	case "supadupa_list_project_function_regions":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/functions/regions"
	case "supadupa_create_project_function_region":
		args, err := decodeProjectFunctionRegionArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/functions/regions"
		body = map[string]string{
			"function_name":  args.FunctionName,
			"host_id":        args.HostID,
			"region":         args.Region,
			"routing_policy": args.RoutingPolicy,
		}
	case "supadupa_delete_project_function_region":
		args, err := decodeProjectFunctionRecordArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/functions/regions/"+url.PathEscape(args.ID)
	case "supadupa_list_project_function_storage_mounts":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/functions/storage-mounts"
	case "supadupa_create_project_function_storage_mount":
		args, err := decodeProjectFunctionStorageMountArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/functions/storage-mounts"
		body = map[string]any{
			"function_name": args.FunctionName,
			"bucket_name":   args.BucketName,
			"mount_path":    args.MountPath,
			"read_only":     *args.ReadOnly,
			"prefix":        args.Prefix,
			"env_alias":     args.EnvAlias,
		}
	case "supadupa_delete_project_function_storage_mount":
		args, err := decodeProjectFunctionRecordArgs(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)+"/functions/storage-mounts/"+url.PathEscape(args.ID)
	case "supadupa_pause_project":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(ref)+"/pause"
	case "supadupa_resume_project":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(ref)+"/resume"
	case "supadupa_restart_project":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(ref)+"/restart"
	case "supadupa_upgrade_project":
		args, err := decodeProjectLifecycleArgs(params.Arguments, "upgrade")
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/upgrade"
		body = map[string]string{"version": args.Version}
	case "supadupa_scale_project":
		args, err := decodeProjectLifecycleArgs(params.Arguments, "scale")
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(args.Ref)+"/scale"
		body = map[string]string{"resource_tier": args.Tier}
	case "supadupa_destroy_project":
		args, err := decodeProjectLifecycleArgs(params.Arguments, "destroy")
		if err != nil {
			return nil, err
		}
		method, path = http.MethodDelete, "/v1/projects/"+url.PathEscape(args.Ref)
		if args.RetainVolumes {
			path += "?retain_volumes=true"
		}
	case "supadupa_trigger_backup":
		ref, err := decodeRef(params.Arguments)
		if err != nil {
			return nil, err
		}
		method, path = http.MethodPost, "/v1/projects/"+url.PathEscape(ref)+"/backups"
	default:
		return nil, fmt.Errorf("unknown tool %q", params.Name)
	}
	payload, status, err := s.api.do(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("management API returned %d: %s", status, strings.TrimSpace(string(payload)))
	}
	return toolJSONResult(payload), nil
}

func tools() []map[string]any {
	refSchema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"ref": map[string]string{"type": "string", "description": "Project ref"}},
		"required":   []string{"ref"},
	}
	configSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":  map[string]string{"type": "string", "description": "Project ref"},
			"area": map[string]string{"type": "string", "description": "Config area such as auth, storage, functions, realtime, pooler, network, smtp, or ai"},
		},
		"required": []string{"ref", "area"},
	}
	configWriteSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":  map[string]string{"type": "string", "description": "Project ref"},
			"area": map[string]string{"type": "string", "description": "Config area such as auth, storage, functions, realtime, pooler, network, smtp, or ai"},
			"config": map[string]any{
				"type":                 "object",
				"description":          "Desired config values for the selected project config area",
				"additionalProperties": map[string]string{"type": "string"},
			},
		},
		"required": []string{"ref", "area", "config"},
	}
	servicesSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref": map[string]string{"type": "string", "description": "Project ref"},
			"services": map[string]any{
				"type":                 "object",
				"description":          "Desired project service enablement map",
				"additionalProperties": map[string]string{"type": "boolean"},
			},
		},
		"required": []string{"ref", "services"},
	}
	branchSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":        map[string]string{"type": "string", "description": "Source project ref"},
			"branch_ref": map[string]string{"type": "string", "description": "New branch project ref"},
			"name":       map[string]string{"type": "string", "description": "Branch project display name"},
			"ttl_hours":  map[string]string{"type": "integer", "description": "Optional branch TTL in hours"},
			"with_data":  map[string]string{"type": "boolean", "description": "Clone source project data into the branch"},
		},
		"required": []string{"ref", "branch_ref"},
	}
	replicaSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":               map[string]string{"type": "string", "description": "Project ref"},
			"name":              map[string]string{"type": "string", "description": "Replica name"},
			"host_id":           map[string]string{"type": "string", "description": "Target host ID"},
			"region":            map[string]string{"type": "string", "description": "Region label"},
			"tier":              map[string]string{"type": "string", "description": "Resource tier"},
			"read_weight":       map[string]string{"type": "integer", "description": "Read routing weight"},
			"failover_priority": map[string]string{"type": "integer", "description": "Failover priority; lower wins"},
		},
		"required": []string{"ref"},
	}
	replicaActionSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":    map[string]string{"type": "string", "description": "Project ref"},
			"id":     map[string]string{"type": "string", "description": "Replica ID"},
			"reason": map[string]string{"type": "string", "description": "Action reason"},
		},
		"required": []string{"ref", "id"},
	}
	replicaFailoverSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":    map[string]string{"type": "string", "description": "Project ref"},
			"reason": map[string]string{"type": "string", "description": "Failover reason"},
		},
		"required": []string{"ref"},
	}
	projectUpgradeSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":     map[string]string{"type": "string", "description": "Project ref"},
			"version": map[string]string{"type": "string", "description": "Target stack version"},
		},
		"required": []string{"ref", "version"},
	}
	projectScaleSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":  map[string]string{"type": "string", "description": "Project ref"},
			"tier": map[string]string{"type": "string", "description": "Target resource tier: small, medium, or large"},
		},
		"required": []string{"ref", "tier"},
	}
	projectDestroySchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":            map[string]string{"type": "string", "description": "Project ref"},
			"retain_volumes": map[string]string{"type": "boolean", "description": "Retain data volumes/PVCs while deleting control-plane metadata"},
		},
		"required": []string{"ref"},
	}
	backupRestoreSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":       map[string]string{"type": "string", "description": "Project ref"},
			"backup_id": map[string]string{"type": "string", "description": "Backup ID to restore"},
		},
		"required": []string{"ref", "backup_id"},
	}
	backupPolicySchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":      map[string]string{"type": "string", "description": "Project ref"},
			"enabled":  map[string]string{"type": "boolean", "description": "Whether scheduled backups are enabled"},
			"schedule": map[string]string{"type": "string", "description": "Backup schedule such as daily or cron expression"},
			"kind":     map[string]string{"type": "string", "description": "Backup kind, usually logical or physical"},
		},
		"required": []string{"ref"},
	}
	pitrPolicySchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":            map[string]string{"type": "string", "description": "Project ref"},
			"enabled":        map[string]string{"type": "boolean", "description": "Whether PITR is enabled"},
			"archive_bucket": map[string]string{"type": "string", "description": "WAL archive bucket URI"},
			"retention_days": map[string]string{"type": "integer", "description": "WAL retention window in days"},
		},
		"required": []string{"ref"},
	}
	secretSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":  map[string]string{"type": "string", "description": "Project ref"},
			"kind": map[string]string{"type": "string", "description": "Secret kind such as service_role, anon, jwt_secret, jwt_signing_key_current, or s3_secret"},
		},
		"required": []string{"ref", "kind"},
	}
	domainSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":  map[string]string{"type": "string", "description": "Project ref"},
			"fqdn": map[string]string{"type": "string", "description": "Custom domain FQDN"},
		},
		"required": []string{"ref", "fqdn"},
	}
	logDrainSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":    map[string]string{"type": "string", "description": "Project ref"},
			"target": map[string]string{"type": "string", "description": "Drain target such as https, loki, datadog, axiom, sentry, s3, or fleet"},
			"config": map[string]string{"type": "object", "description": "Drain config. Sensitive values should use secret:// handles"},
		},
		"required": []string{"ref", "target"},
	}
	networkConnectionSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":         map[string]string{"type": "string", "description": "Project ref"},
			"name":        map[string]string{"type": "string", "description": "Connection name"},
			"type":        map[string]string{"type": "string", "description": "privatelink, vpc_peering, private_endpoint, wireguard, or operator_network"},
			"provider":    map[string]string{"type": "string", "description": "aws, gcp, azure, custom, or operator"},
			"region":      map[string]string{"type": "string", "description": "Provider region"},
			"cidrs":       map[string]string{"type": "array", "description": "Allowed private CIDR/address list"},
			"endpoint_id": map[string]string{"type": "string", "description": "Provider endpoint or connection ID"},
			"config":      map[string]string{"type": "object", "description": "Connection config. Sensitive values should use secret:// handles"},
		},
		"required": []string{"ref", "name"},
	}
	databaseExtensionSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":     map[string]string{"type": "string", "description": "Project ref"},
			"name":    map[string]string{"type": "string", "description": "Postgres extension name"},
			"schema":  map[string]string{"type": "string", "description": "Extension schema"},
			"version": map[string]string{"type": "string", "description": "Pinned extension version"},
			"enabled": map[string]string{"type": "boolean", "description": "Whether the extension is enabled"},
		},
		"required": []string{"ref", "name"},
	}
	databaseCronJobSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":                 map[string]string{"type": "string", "description": "Project ref"},
			"name":                map[string]string{"type": "string", "description": "Cron job name"},
			"schedule":            map[string]string{"type": "string", "description": "Five-field cron schedule"},
			"command":             map[string]string{"type": "string", "description": "SQL command to run"},
			"database":            map[string]string{"type": "string", "description": "Target database name"},
			"username":            map[string]string{"type": "string", "description": "Database username"},
			"active":              map[string]string{"type": "boolean", "description": "Whether the job declaration is active"},
			"timeout_seconds":     map[string]string{"type": "integer", "description": "Statement timeout seconds"},
			"max_runtime_seconds": map[string]string{"type": "integer", "description": "Maximum runtime seconds"},
			"metadata":            map[string]string{"type": "object", "description": "Cron metadata. Sensitive values should use secret:// handles"},
		},
		"required": []string{"ref", "name", "schedule", "command"},
	}
	databaseQueueSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":                        map[string]string{"type": "string", "description": "Project ref"},
			"name":                       map[string]string{"type": "string", "description": "Queue name"},
			"schema":                     map[string]string{"type": "string", "description": "Queue schema"},
			"retention_minutes":          map[string]string{"type": "integer", "description": "Retention window in minutes"},
			"visibility_timeout_seconds": map[string]string{"type": "integer", "description": "Visibility timeout seconds"},
			"max_retries":                map[string]string{"type": "integer", "description": "Maximum retry attempts before dead-letter handling"},
			"dead_letter_queue":          map[string]string{"type": "string", "description": "Dead-letter queue name"},
			"active":                     map[string]string{"type": "boolean", "description": "Whether the queue declaration is active"},
			"metadata":                   map[string]string{"type": "object", "description": "Queue metadata. Sensitive values should use secret:// handles"},
		},
		"required": []string{"ref", "name"},
	}
	databaseWebhookSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":             map[string]string{"type": "string", "description": "Project ref"},
			"name":            map[string]string{"type": "string", "description": "Webhook name"},
			"schema":          map[string]string{"type": "string", "description": "Table schema"},
			"table":           map[string]string{"type": "string", "description": "Table name"},
			"events":          map[string]string{"type": "array", "description": "insert, update, and/or delete"},
			"endpoint":        map[string]string{"type": "string", "description": "HTTPS delivery endpoint"},
			"http_method":     map[string]string{"type": "string", "description": "HTTP method"},
			"headers":         map[string]string{"type": "object", "description": "Outbound headers. Sensitive values should use secret:// handles"},
			"timeout_seconds": map[string]string{"type": "integer", "description": "Request timeout seconds"},
			"retry_count":     map[string]string{"type": "integer", "description": "Retry count"},
			"active":          map[string]string{"type": "boolean", "description": "Whether the webhook declaration is active"},
			"metadata":        map[string]string{"type": "object", "description": "Webhook metadata. Sensitive values should use secret:// handles"},
		},
		"required": []string{"ref", "name", "table", "endpoint"},
	}
	databaseSchemaSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":         map[string]string{"type": "string", "description": "Project ref"},
			"name":        map[string]string{"type": "string", "description": "Schema declaration name"},
			"version":     map[string]string{"type": "string", "description": "Migration version"},
			"schema":      map[string]string{"type": "string", "description": "Target database schema"},
			"sql":         map[string]string{"type": "string", "description": "SQL migration text"},
			"apply_order": map[string]string{"type": "integer", "description": "Apply order"},
			"active":      map[string]string{"type": "boolean", "description": "Whether the schema migration is active"},
			"metadata":    map[string]string{"type": "object", "description": "Schema metadata. Sensitive values should use secret:// handles"},
		},
		"required": []string{"ref", "name", "version", "sql"},
	}
	databaseSchemaKeySchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":     map[string]string{"type": "string", "description": "Project ref"},
			"name":    map[string]string{"type": "string", "description": "Schema declaration name"},
			"version": map[string]string{"type": "string", "description": "Migration version"},
		},
		"required": []string{"ref", "name", "version"},
	}
	databaseRoleSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":                    map[string]string{"type": "string", "description": "Project ref"},
			"name":                   map[string]string{"type": "string", "description": "Database role name"},
			"login":                  map[string]string{"type": "boolean", "description": "Whether this is a login role"},
			"inherit":                map[string]string{"type": "boolean", "description": "Whether the role inherits privileges"},
			"bypass_rls":             map[string]string{"type": "boolean", "description": "Whether the role can bypass RLS"},
			"connection_limit":       map[string]string{"type": "integer", "description": "Connection limit; -1 for unlimited"},
			"password_secret_handle": map[string]string{"type": "string", "description": "secret:// handle for login role password"},
			"member_of":              map[string]string{"type": "array", "description": "Role memberships"},
			"schema_grants":          map[string]string{"type": "object", "description": "Schema grant map, for example public=usage,select"},
			"metadata":               map[string]string{"type": "object", "description": "Role metadata. Sensitive values should use secret:// handles"},
		},
		"required": []string{"ref", "name"},
	}
	authClientSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":                  map[string]string{"type": "string", "description": "Project ref"},
			"name":                 map[string]string{"type": "string", "description": "OAuth client display name"},
			"client_id":            map[string]string{"type": "string", "description": "OAuth client ID. Generated if omitted"},
			"client_secret_handle": map[string]string{"type": "string", "description": "secret:// handle for confidential clients"},
			"redirect_uris":        map[string]string{"type": "array", "description": "Allowed redirect URIs"},
			"grant_types":          map[string]string{"type": "array", "description": "OAuth grant types"},
			"scopes":               map[string]string{"type": "array", "description": "OAuth scopes"},
			"confidential":         map[string]string{"type": "boolean", "description": "Whether the client is confidential"},
		},
		"required": []string{"ref", "name"},
	}
	authClientIDSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":       map[string]string{"type": "string", "description": "Project ref"},
			"client_id": map[string]string{"type": "string", "description": "OAuth client ID"},
		},
		"required": []string{"ref", "client_id"},
	}
	authHookSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":            map[string]string{"type": "string", "description": "Project ref"},
			"hook_type":      map[string]string{"type": "string", "description": "Auth Hook type"},
			"enabled":        map[string]string{"type": "boolean", "description": "Whether the hook is enabled"},
			"target_uri":     map[string]string{"type": "string", "description": "HTTPS hook endpoint"},
			"edge_function":  map[string]string{"type": "string", "description": "Project Edge Function target"},
			"secret_handle":  map[string]string{"type": "string", "description": "secret:// handle for hook signing or auth"},
			"headers":        map[string]string{"type": "object", "description": "Outbound headers. Sensitive values should use secret:// handles"},
			"timeout_ms":     map[string]string{"type": "integer", "description": "Hook timeout in milliseconds"},
			"retry_attempts": map[string]string{"type": "integer", "description": "Retry attempts"},
		},
		"required": []string{"ref", "hook_type"},
	}
	authHookTypeSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":       map[string]string{"type": "string", "description": "Project ref"},
			"hook_type": map[string]string{"type": "string", "description": "Auth Hook type"},
		},
		"required": []string{"ref", "hook_type"},
	}
	storageBucketSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":                map[string]string{"type": "string", "description": "Project ref"},
			"name":               map[string]string{"type": "string", "description": "Storage bucket name"},
			"public":             map[string]string{"type": "boolean", "description": "Whether the bucket is publicly readable"},
			"file_size_limit":    map[string]string{"type": "integer", "description": "Maximum object size in bytes"},
			"allowed_mime_types": map[string]string{"type": "array", "description": "Allowed MIME types. Empty allows all"},
			"cache_control":      map[string]string{"type": "string", "description": "Default Cache-Control directive"},
			"avif_autodetection": map[string]string{"type": "boolean", "description": "Whether AVIF autodetection is enabled"},
			"metadata":           map[string]string{"type": "object", "description": "Bucket metadata. Sensitive values should use secret:// handles"},
		},
		"required": []string{"ref", "name"},
	}
	vectorBucketSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":             map[string]string{"type": "string", "description": "Project ref"},
			"name":            map[string]string{"type": "string", "description": "Vector bucket name"},
			"dimension":       map[string]string{"type": "integer", "description": "Vector dimension"},
			"distance":        map[string]string{"type": "string", "description": "cosine, l2, or ip"},
			"index_method":    map[string]string{"type": "string", "description": "none, hnsw, or ivfflat"},
			"storage_backend": map[string]string{"type": "string", "description": "postgres or s3"},
			"storage_uri":     map[string]string{"type": "string", "description": "Storage URI for S3-backed buckets"},
			"metadata":        map[string]string{"type": "object", "description": "Vector bucket metadata"},
		},
		"required": []string{"ref", "name"},
	}
	analyticsBucketSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":                 map[string]string{"type": "string", "description": "Project ref"},
			"name":                map[string]string{"type": "string", "description": "Analytics bucket name"},
			"storage_uri":         map[string]string{"type": "string", "description": "Iceberg table storage URI"},
			"catalog_uri":         map[string]string{"type": "string", "description": "Iceberg catalog URI"},
			"warehouse":           map[string]string{"type": "string", "description": "Iceberg warehouse name"},
			"credential_handle":   map[string]string{"type": "string", "description": "secret:// handle for storage/catalog credentials"},
			"format_version":      map[string]string{"type": "integer", "description": "Iceberg format version"},
			"partitioning":        map[string]string{"type": "string", "description": "Partition spec"},
			"retention_days":      map[string]string{"type": "integer", "description": "Retention period in days, 0 for indefinite"},
			"compaction_schedule": map[string]string{"type": "string", "description": "Compaction schedule"},
			"metadata":            map[string]string{"type": "object", "description": "Analytics bucket metadata"},
		},
		"required": []string{"ref", "name", "storage_uri"},
	}
	cdnPolicySchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":                            map[string]string{"type": "string", "description": "Project ref"},
			"enabled":                        map[string]string{"type": "boolean", "description": "Whether the CDN policy is enabled"},
			"browser_ttl_seconds":            map[string]string{"type": "integer", "description": "Browser max-age in seconds"},
			"edge_ttl_seconds":               map[string]string{"type": "integer", "description": "Edge s-maxage in seconds"},
			"stale_while_revalidate_seconds": map[string]string{"type": "integer", "description": "stale-while-revalidate seconds"},
			"included_paths":                 map[string]string{"type": "array", "description": "Path patterns included in CDN caching"},
			"excluded_paths":                 map[string]string{"type": "array", "description": "Path patterns excluded from CDN caching"},
			"smart_revalidation":             map[string]string{"type": "boolean", "description": "Whether storage object events trigger smart revalidation"},
			"cache_control":                  map[string]string{"type": "string", "description": "Explicit Cache-Control header"},
		},
		"required": []string{"ref"},
	}
	cdnInvalidationSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":   map[string]string{"type": "string", "description": "Project ref"},
			"paths": map[string]string{"type": "array", "description": "Object or route paths to invalidate"},
		},
		"required": []string{"ref", "paths"},
	}
	cdnObjectEventSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":         map[string]string{"type": "string", "description": "Project ref"},
			"event_id":    map[string]string{"type": "string", "description": "Storage event id"},
			"bucket":      map[string]string{"type": "string", "description": "Storage bucket name"},
			"object_path": map[string]string{"type": "string", "description": "Object path inside the bucket"},
			"event_type":  map[string]string{"type": "string", "description": "object_created, object_updated, object_deleted, or object_changed"},
		},
		"required": []string{"ref", "event_id", "bucket", "object_path", "event_type"},
	}
	functionDeploySchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":        map[string]string{"type": "string", "description": "Project ref"},
			"name":       map[string]string{"type": "string", "description": "Edge Function name"},
			"entrypoint": map[string]string{"type": "string", "description": "Function source entrypoint"},
			"verify_jwt": map[string]string{"type": "boolean", "description": "Whether invocations require JWT verification"},
			"source":     map[string]string{"type": "string", "description": "Function source text"},
			"secrets":    map[string]string{"type": "object", "description": "Function secret environment values"},
		},
		"required": []string{"ref", "name", "source"},
	}
	functionNameSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":  map[string]string{"type": "string", "description": "Project ref"},
			"name": map[string]string{"type": "string", "description": "Edge Function name"},
		},
		"required": []string{"ref", "name"},
	}
	functionRegionSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":            map[string]string{"type": "string", "description": "Project ref"},
			"function_name":  map[string]string{"type": "string", "description": "Deployed Edge Function name"},
			"host_id":        map[string]string{"type": "string", "description": "Optional host ID"},
			"region":         map[string]string{"type": "string", "description": "Region label"},
			"routing_policy": map[string]string{"type": "string", "description": "nearest, primary, or weighted"},
		},
		"required": []string{"ref", "function_name"},
	}
	functionRecordSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref": map[string]string{"type": "string", "description": "Project ref"},
			"id":  map[string]string{"type": "string", "description": "Function region or storage mount ID"},
		},
		"required": []string{"ref", "id"},
	}
	functionMountSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":           map[string]string{"type": "string", "description": "Project ref"},
			"function_name": map[string]string{"type": "string", "description": "Deployed Edge Function name"},
			"bucket_name":   map[string]string{"type": "string", "description": "Project Storage bucket name"},
			"mount_path":    map[string]string{"type": "string", "description": "Absolute path under /mnt"},
			"read_only":     map[string]string{"type": "boolean", "description": "Whether the mount is read-only"},
			"prefix":        map[string]string{"type": "string", "description": "Optional bucket prefix"},
			"env_alias":     map[string]string{"type": "string", "description": "Environment variable alias for the mount"},
		},
		"required": []string{"ref", "function_name", "bucket_name"},
	}
	replicationSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":               map[string]string{"type": "string", "description": "Project ref"},
			"name":              map[string]string{"type": "string", "description": "Replication pipeline name"},
			"type":              map[string]string{"type": "string", "description": "logical, etl, or analytics_bucket"},
			"source_schema":     map[string]string{"type": "string", "description": "Source schema"},
			"source_table":      map[string]string{"type": "string", "description": "Source table"},
			"destination":       map[string]string{"type": "string", "description": "Destination type"},
			"destination_uri":   map[string]string{"type": "string", "description": "Destination URI"},
			"credential_handle": map[string]string{"type": "string", "description": "secret:// credential handle"},
			"config":            map[string]string{"type": "object", "description": "Destination config"},
		},
		"required": []string{"ref", "name", "source_table", "destination"},
	}
	embeddingSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":                map[string]string{"type": "string", "description": "Project ref"},
			"name":               map[string]string{"type": "string", "description": "Embedding job name"},
			"source_schema":      map[string]string{"type": "string", "description": "Source schema"},
			"source_table":       map[string]string{"type": "string", "description": "Source table"},
			"source_column":      map[string]string{"type": "string", "description": "Source text column"},
			"primary_key_column": map[string]string{"type": "string", "description": "Primary key column"},
			"destination_table":  map[string]string{"type": "string", "description": "Destination table"},
			"destination_column": map[string]string{"type": "string", "description": "Destination vector column"},
			"provider":           map[string]string{"type": "string", "description": "Embedding provider"},
			"model":              map[string]string{"type": "string", "description": "Embedding model"},
			"dimension":          map[string]string{"type": "integer", "description": "Embedding dimension"},
			"schedule":           map[string]string{"type": "string", "description": "Schedule or manual"},
			"batch_size":         map[string]string{"type": "integer", "description": "Batch size"},
		},
		"required": []string{"ref", "source_table", "source_column"},
	}
	noArgs := map[string]string{"type": "object"}
	userCreateSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"email":    map[string]string{"type": "string", "description": "Platform user email"},
			"password": map[string]string{"type": "string", "description": "Initial platform user password"},
			"role":     map[string]string{"type": "string", "description": "Platform role: admin, developer, or viewer"},
		},
		"required": []string{"email", "password"},
	}
	mfaCodeSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code": map[string]string{"type": "string", "description": "Six-digit TOTP code"},
		},
		"required": []string{"code"},
	}
	scimListGroupsSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"org_id": map[string]string{"type": "string", "description": "Optional organization ID filter"},
		},
	}
	scimIDSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]string{"type": "string", "description": "SCIM resource ID"},
		},
		"required": []string{"id"},
	}
	scimUserWriteSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":           map[string]string{"type": "string", "description": "SCIM user ID for replace/deprovision/delete operations"},
			"user_name":    map[string]string{"type": "string", "description": "SCIM userName"},
			"email":        map[string]string{"type": "string", "description": "Primary email; defaults to user_name"},
			"display_name": map[string]string{"type": "string", "description": "Display name"},
			"password":     map[string]string{"type": "string", "description": "Initial or replacement password"},
			"role":         map[string]string{"type": "string", "description": "Platform role"},
			"active":       map[string]string{"type": "boolean", "description": "Whether the SCIM user is active"},
		},
	}
	scimGroupWriteSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":           map[string]string{"type": "string", "description": "SCIM group ID for delete operations"},
			"org_id":       map[string]string{"type": "string", "description": "Organization ID"},
			"display_name": map[string]string{"type": "string", "description": "Group display name"},
			"slug":         map[string]string{"type": "string", "description": "Team slug"},
			"members":      map[string]any{"type": "array", "items": map[string]string{"type": "string"}, "description": "Member emails or user IDs"},
		},
	}
	orgCreateSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]string{"type": "string", "description": "Organization display name"},
		},
		"required": []string{"name"},
	}
	orgUpdateSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"org_id": map[string]string{"type": "string", "description": "Organization ID"},
			"name":   map[string]string{"type": "string", "description": "Organization display name"},
		},
		"required": []string{"org_id", "name"},
	}
	orgSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"org_id": map[string]string{"type": "string", "description": "Organization ID"},
			"limit":  map[string]string{"type": "integer", "description": "Maximum records to return"},
		},
		"required": []string{"org_id"},
	}
	billingInvoiceCreateSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"org_id":            map[string]string{"type": "string", "description": "Organization ID"},
			"usage_snapshot_id": map[string]string{"type": "string", "description": "Optional usage snapshot ID; latest snapshot is used when omitted"},
			"currency":          map[string]string{"type": "string", "description": "Invoice currency; defaults to USD"},
			"status":            map[string]string{"type": "string", "description": "Invoice status; defaults to draft"},
			"due_days":          map[string]string{"type": "integer", "description": "Days until due; defaults to 30"},
		},
		"required": []string{"org_id"},
	}
	billingInvoiceGetSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"org_id":     map[string]string{"type": "string", "description": "Organization ID"},
			"invoice_id": map[string]string{"type": "string", "description": "Billing invoice ID"},
		},
		"required": []string{"org_id", "invoice_id"},
	}
	orgMemberSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"org_id": map[string]string{"type": "string", "description": "Organization ID"},
			"email":  map[string]string{"type": "string", "description": "Member email"},
			"role":   map[string]string{"type": "string", "description": "Org role such as viewer, developer, or admin"},
		},
		"required": []string{"org_id", "email"},
	}
	orgTeamSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"org_id": map[string]string{"type": "string", "description": "Organization ID"},
			"slug":   map[string]string{"type": "string", "description": "Team slug"},
			"name":   map[string]string{"type": "string", "description": "Team display name"},
		},
		"required": []string{"org_id", "slug"},
	}
	orgTeamCreateSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"org_id": map[string]string{"type": "string", "description": "Organization ID"},
			"slug":   map[string]string{"type": "string", "description": "Team slug"},
			"name":   map[string]string{"type": "string", "description": "Team display name"},
		},
		"required": []string{"org_id", "name"},
	}
	orgTeamMemberSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"org_id": map[string]string{"type": "string", "description": "Organization ID"},
			"slug":   map[string]string{"type": "string", "description": "Team slug"},
			"email":  map[string]string{"type": "string", "description": "Member email"},
		},
		"required": []string{"org_id", "slug", "email"},
	}
	orgTeamMemberListSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"org_id": map[string]string{"type": "string", "description": "Organization ID"},
			"slug":   map[string]string{"type": "string", "description": "Team slug"},
		},
		"required": []string{"org_id", "slug"},
	}
	orgQuotaSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"org_id":       map[string]string{"type": "string", "description": "Organization ID"},
			"max_projects": map[string]string{"type": "integer", "description": "Maximum projects"},
			"max_cpu":      map[string]string{"type": "integer", "description": "Maximum CPU"},
			"max_ram_mb":   map[string]string{"type": "integer", "description": "Maximum RAM in MB"},
			"max_disk_gb":  map[string]string{"type": "integer", "description": "Maximum disk in GB"},
		},
		"required": []string{"org_id"},
	}
	hostIDSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]string{"type": "string", "description": "Host ID"},
		},
		"required": []string{"id"},
	}
	hostCreateSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":              map[string]string{"type": "string", "description": "Host display name"},
			"address":           map[string]string{"type": "string", "description": "Host address"},
			"capacity_cpu":      map[string]string{"type": "integer", "description": "CPU capacity"},
			"capacity_ram_mb":   map[string]string{"type": "integer", "description": "RAM capacity in MB"},
			"capacity_disk_gb":  map[string]string{"type": "integer", "description": "Disk capacity in GB"},
			"capacity_projects": map[string]string{"type": "integer", "description": "Project slot capacity"},
		},
		"required": []string{"name", "address"},
	}
	projectCreateSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"org_id":        map[string]string{"type": "string", "description": "Organization ID"},
			"ref":           map[string]string{"type": "string", "description": "Project ref"},
			"name":          map[string]string{"type": "string", "description": "Project display name"},
			"host_id":       map[string]string{"type": "string", "description": "Target host ID"},
			"domain":        map[string]string{"type": "string", "description": "Base domain for project routing"},
			"stack_version": map[string]string{"type": "string", "description": "Upstream Supabase stack version"},
			"profile":       map[string]string{"type": "string", "description": "Stack profile: full, essential, or orioledb"},
			"resource_tier": map[string]string{"type": "string", "description": "Resource tier: small, medium, or large"},
			"services": map[string]any{
				"type":                 "object",
				"description":          "Per-service enablement map",
				"additionalProperties": map[string]string{"type": "boolean"},
			},
			"environment": map[string]any{
				"type":                 "object",
				"description":          "Project environment overrides",
				"additionalProperties": map[string]string{"type": "string"},
			},
		},
		"required": []string{"org_id", "ref", "name"},
	}
	platformDefaultsSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"domain":               map[string]string{"type": "string", "description": "Default base domain for new projects"},
			"stack_version":        map[string]string{"type": "string", "description": "Default upstream Supabase stack version"},
			"profile":              map[string]string{"type": "string", "description": "Default stack profile: full, essential, or orioledb"},
			"resource_tier":        map[string]string{"type": "string", "description": "Default resource tier: small, medium, or large"},
			"backup_schedule":      map[string]string{"type": "string", "description": "Default backup schedule: daily or hourly"},
			"smtp_enabled":         map[string]string{"type": "boolean", "description": "Whether platform SMTP defaults are enabled"},
			"smtp_host":            map[string]string{"type": "string", "description": "Platform SMTP host"},
			"smtp_port":            map[string]string{"type": "integer", "description": "Platform SMTP port"},
			"smtp_sender_name":     map[string]string{"type": "string", "description": "Platform SMTP sender display name"},
			"smtp_sender_email":    map[string]string{"type": "string", "description": "Platform SMTP sender email"},
			"smtp_username":        map[string]string{"type": "string", "description": "Platform SMTP username"},
			"smtp_password_handle": map[string]string{"type": "string", "description": "secret:// handle for the platform SMTP password"},
			"smtp_tls_mode":        map[string]string{"type": "string", "description": "Platform SMTP TLS mode: starttls, implicit, or none"},
		},
	}
	platformSSOSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"enabled":         map[string]string{"type": "boolean", "description": "Whether platform SAML SSO is enabled"},
			"idp_entity_id":   map[string]string{"type": "string", "description": "SAML IdP entity ID"},
			"sso_url":         map[string]string{"type": "string", "description": "SAML IdP login URL"},
			"certificate_pem": map[string]string{"type": "string", "description": "SAML IdP signing certificate PEM"},
			"acs_url":         map[string]string{"type": "string", "description": "Assertion consumer service URL"},
			"metadata_url":    map[string]string{"type": "string", "description": "Optional SAML metadata URL"},
			"email_domain":    map[string]string{"type": "string", "description": "Optional allowed email domain"},
			"auto_provision":  map[string]string{"type": "boolean", "description": "Whether valid SAML assertions may create users"},
			"default_role":    map[string]string{"type": "string", "description": "Default role for auto-provisioned users"},
		},
	}
	projectAccessSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref":          map[string]string{"type": "string", "description": "Project ref"},
			"subject_type": map[string]string{"type": "string", "description": "user or team"},
			"subject_id":   map[string]string{"type": "string", "description": "User email/ID or team slug"},
			"role":         map[string]string{"type": "string", "description": "Project role such as viewer, developer, or admin"},
		},
		"required": []string{"ref", "subject_id"},
	}
	return []map[string]any{
		{"name": "supadupa_list_users", "description": "List platform users.", "inputSchema": noArgs},
		{"name": "supadupa_create_user", "description": "Create a platform user.", "inputSchema": userCreateSchema},
		{"name": "supadupa_get_provisioner", "description": "Get the active orchestration provisioner backend.", "inputSchema": noArgs},
		{"name": "supadupa_get_account_mfa", "description": "Get MFA status for the authenticated platform account.", "inputSchema": noArgs},
		{"name": "supadupa_enroll_account_mfa", "description": "Start TOTP MFA enrollment for the authenticated platform account.", "inputSchema": noArgs},
		{"name": "supadupa_verify_account_mfa", "description": "Verify a TOTP code to enable MFA for the authenticated platform account.", "inputSchema": mfaCodeSchema},
		{"name": "supadupa_disable_account_mfa", "description": "Disable MFA for the authenticated platform account with a valid TOTP code.", "inputSchema": mfaCodeSchema},
		{"name": "supadupa_get_scim_service_provider_config", "description": "Get SCIM v2 service provider capabilities.", "inputSchema": noArgs},
		{"name": "supadupa_list_scim_users", "description": "List platform users in SCIM v2 format.", "inputSchema": noArgs},
		{"name": "supadupa_get_scim_user", "description": "Get a platform user in SCIM v2 format.", "inputSchema": scimIDSchema},
		{"name": "supadupa_create_scim_user", "description": "Provision a platform user through SCIM v2.", "inputSchema": scimUserWriteSchema},
		{"name": "supadupa_replace_scim_user", "description": "Replace a platform user through SCIM v2.", "inputSchema": scimUserWriteSchema},
		{"name": "supadupa_deprovision_scim_user", "description": "Deprovision a platform user through SCIM active=false.", "inputSchema": scimIDSchema},
		{"name": "supadupa_delete_scim_user", "description": "Delete a platform user through SCIM v2.", "inputSchema": scimIDSchema},
		{"name": "supadupa_list_scim_groups", "description": "List organization teams in SCIM v2 group format.", "inputSchema": scimListGroupsSchema},
		{"name": "supadupa_get_scim_group", "description": "Get an organization team in SCIM v2 group format.", "inputSchema": scimIDSchema},
		{"name": "supadupa_create_scim_group", "description": "Provision an organization team through SCIM v2.", "inputSchema": scimGroupWriteSchema},
		{"name": "supadupa_delete_scim_group", "description": "Delete an organization team through SCIM v2.", "inputSchema": scimIDSchema},
		{"name": "supadupa_list_orgs", "description": "List supadupa organizations.", "inputSchema": noArgs},
		{"name": "supadupa_create_org", "description": "Create a supadupa organization.", "inputSchema": orgCreateSchema},
		{"name": "supadupa_get_org", "description": "Get a supadupa organization by ID.", "inputSchema": orgSchema},
		{"name": "supadupa_update_org", "description": "Update a supadupa organization.", "inputSchema": orgUpdateSchema},
		{"name": "supadupa_delete_org", "description": "Delete an empty supadupa organization.", "inputSchema": orgSchema},
		{"name": "supadupa_list_hosts", "description": "List control-plane host capacity targets.", "inputSchema": noArgs},
		{"name": "supadupa_create_host", "description": "Create a control-plane host capacity target.", "inputSchema": hostCreateSchema},
		{"name": "supadupa_get_host", "description": "Get a control-plane host by ID.", "inputSchema": hostIDSchema},
		{"name": "supadupa_delete_host", "description": "Delete an unused control-plane host.", "inputSchema": hostIDSchema},
		{"name": "supadupa_get_fleet_metrics", "description": "Get fleet-wide supadupa metrics.", "inputSchema": noArgs},
		{"name": "supadupa_get_advisor_findings", "description": "Get fleet Security & Performance Advisor findings.", "inputSchema": noArgs},
		{"name": "supadupa_get_compliance_report", "description": "Get SOC 2 / HIPAA control evidence report.", "inputSchema": noArgs},
		{"name": "supadupa_list_audit_events", "description": "List immutable control-plane audit events.", "inputSchema": noArgs},
		{"name": "supadupa_get_audit_integrity", "description": "Verify the immutable audit hash chain.", "inputSchema": noArgs},
		{"name": "supadupa_get_platform_defaults", "description": "Get platform defaults for new projects, platform SMTP, and integrations.", "inputSchema": noArgs},
		{"name": "supadupa_set_platform_defaults", "description": "Update platform defaults for new projects and platform SMTP.", "inputSchema": platformDefaultsSchema},
		{"name": "supadupa_get_platform_sso", "description": "Get platform SAML SSO settings.", "inputSchema": noArgs},
		{"name": "supadupa_set_platform_sso", "description": "Update platform SAML SSO settings.", "inputSchema": platformSSOSchema},
		{"name": "supadupa_get_org_usage", "description": "Get current org metering counters.", "inputSchema": orgSchema},
		{"name": "supadupa_get_org_quota", "description": "Get org quota limits.", "inputSchema": orgSchema},
		{"name": "supadupa_set_org_quota", "description": "Update org quota limits.", "inputSchema": orgQuotaSchema},
		{"name": "supadupa_list_org_usage_snapshots", "description": "List durable org usage snapshots.", "inputSchema": orgSchema},
		{"name": "supadupa_create_org_usage_snapshot", "description": "Capture a durable org usage snapshot for billing and audit.", "inputSchema": orgSchema},
		{"name": "supadupa_list_billing_invoices", "description": "List generated billing invoices for an org.", "inputSchema": orgSchema},
		{"name": "supadupa_create_billing_invoice", "description": "Create a generated billing invoice for an org.", "inputSchema": billingInvoiceCreateSchema},
		{"name": "supadupa_get_billing_invoice", "description": "Get a generated billing invoice by ID.", "inputSchema": billingInvoiceGetSchema},
		{"name": "supadupa_list_org_members", "description": "List organization members.", "inputSchema": orgSchema},
		{"name": "supadupa_upsert_org_member", "description": "Add or update an organization member role.", "inputSchema": orgMemberSchema},
		{"name": "supadupa_delete_org_member", "description": "Remove an organization member.", "inputSchema": orgMemberSchema},
		{"name": "supadupa_list_org_teams", "description": "List organization teams.", "inputSchema": orgSchema},
		{"name": "supadupa_create_org_team", "description": "Create an organization team.", "inputSchema": orgTeamCreateSchema},
		{"name": "supadupa_delete_org_team", "description": "Delete an organization team.", "inputSchema": orgTeamSchema},
		{"name": "supadupa_list_org_team_members", "description": "List organization team members.", "inputSchema": orgTeamMemberListSchema},
		{"name": "supadupa_add_org_team_member", "description": "Add a member to an organization team.", "inputSchema": orgTeamMemberSchema},
		{"name": "supadupa_delete_org_team_member", "description": "Remove a member from an organization team.", "inputSchema": orgTeamMemberSchema},
		{"name": "supadupa_get_org_access_review", "description": "Get organization access review across projects.", "inputSchema": orgSchema},
		{"name": "supadupa_list_projects", "description": "List all visible supadupa projects.", "inputSchema": noArgs},
		{"name": "supadupa_list_org_projects", "description": "List projects in one organization.", "inputSchema": orgSchema},
		{"name": "supadupa_create_project", "description": "Provision a new isolated Supabase project in an organization.", "inputSchema": projectCreateSchema},
		{"name": "supadupa_get_project", "description": "Get a project by ref.", "inputSchema": refSchema},
		{"name": "supadupa_project_connect", "description": "Get the full project Connect payload.", "inputSchema": refSchema},
		{"name": "supadupa_get_project_metrics", "description": "Get per-project metrics.", "inputSchema": refSchema},
		{"name": "supadupa_get_project_config", "description": "Get one project config area.", "inputSchema": configSchema},
		{"name": "supadupa_set_project_config", "description": "Update one project config area.", "inputSchema": configWriteSchema},
		{"name": "supadupa_get_project_services", "description": "Get project enabled-service state.", "inputSchema": refSchema},
		{"name": "supadupa_set_project_services", "description": "Update project enabled-service state.", "inputSchema": servicesSchema},
		{"name": "supadupa_list_project_database_extensions", "description": "List project Postgres extension declarations.", "inputSchema": refSchema},
		{"name": "supadupa_set_project_database_extension", "description": "Enable, disable, or pin a project Postgres extension.", "inputSchema": databaseExtensionSchema},
		{"name": "supadupa_list_project_database_cron_jobs", "description": "List project pg_cron job declarations.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_database_cron_job", "description": "Create a project pg_cron job declaration.", "inputSchema": databaseCronJobSchema},
		{"name": "supadupa_delete_project_database_cron_job", "description": "Delete a project pg_cron job declaration.", "inputSchema": functionNameSchema},
		{"name": "supadupa_list_project_database_queues", "description": "List project pgmq queue declarations.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_database_queue", "description": "Create a project pgmq queue declaration.", "inputSchema": databaseQueueSchema},
		{"name": "supadupa_delete_project_database_queue", "description": "Delete a project pgmq queue declaration.", "inputSchema": functionNameSchema},
		{"name": "supadupa_list_project_database_webhooks", "description": "List project database webhook declarations.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_database_webhook", "description": "Create a project database webhook declaration.", "inputSchema": databaseWebhookSchema},
		{"name": "supadupa_delete_project_database_webhook", "description": "Delete a project database webhook declaration.", "inputSchema": functionNameSchema},
		{"name": "supadupa_list_project_database_schemas", "description": "List project declarative database schema migrations.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_database_schema", "description": "Create a project declarative database schema migration.", "inputSchema": databaseSchemaSchema},
		{"name": "supadupa_delete_project_database_schema", "description": "Delete a project declarative database schema migration.", "inputSchema": databaseSchemaKeySchema},
		{"name": "supadupa_list_project_database_roles", "description": "List project database role declarations.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_database_role", "description": "Create a project database role declaration.", "inputSchema": databaseRoleSchema},
		{"name": "supadupa_delete_project_database_role", "description": "Delete a project database role declaration.", "inputSchema": functionNameSchema},
		{"name": "supadupa_list_project_auth_clients", "description": "List project OAuth clients.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_auth_client", "description": "Create or update a project OAuth client.", "inputSchema": authClientSchema},
		{"name": "supadupa_delete_project_auth_client", "description": "Delete a project OAuth client.", "inputSchema": authClientIDSchema},
		{"name": "supadupa_list_project_auth_hooks", "description": "List project Auth Hook declarations.", "inputSchema": refSchema},
		{"name": "supadupa_set_project_auth_hook", "description": "Create or update a project Auth Hook declaration.", "inputSchema": authHookSchema},
		{"name": "supadupa_delete_project_auth_hook", "description": "Delete a project Auth Hook declaration.", "inputSchema": authHookTypeSchema},
		{"name": "supadupa_list_project_activity", "description": "List control-plane activity for a project.", "inputSchema": refSchema},
		{"name": "supadupa_list_project_access", "description": "List project access grants.", "inputSchema": refSchema},
		{"name": "supadupa_grant_project_access", "description": "Grant a user or team access to a project.", "inputSchema": projectAccessSchema},
		{"name": "supadupa_revoke_project_access", "description": "Revoke user or team access from a project.", "inputSchema": projectAccessSchema},
		{"name": "supadupa_list_project_logs", "description": "List project logs.", "inputSchema": refSchema},
		{"name": "supadupa_list_project_backups", "description": "List project backups.", "inputSchema": refSchema},
		{"name": "supadupa_list_project_secrets", "description": "List masked project secret metadata.", "inputSchema": refSchema},
		{"name": "supadupa_reveal_project_secret", "description": "Reveal a project secret through the audited reveal endpoint.", "inputSchema": secretSchema},
		{"name": "supadupa_record_project_secret_copy", "description": "Record an audited project secret copy action without returning the secret value.", "inputSchema": secretSchema},
		{"name": "supadupa_rotate_project_secret", "description": "Rotate a project secret or signing key.", "inputSchema": secretSchema},
		{"name": "supadupa_get_project_backup_policy", "description": "Get project backup policy.", "inputSchema": refSchema},
		{"name": "supadupa_set_project_backup_policy", "description": "Update project scheduled backup policy.", "inputSchema": backupPolicySchema},
		{"name": "supadupa_restore_project_backup", "description": "Restore a project backup.", "inputSchema": backupRestoreSchema},
		{"name": "supadupa_get_project_pitr_policy", "description": "Get project PITR policy.", "inputSchema": refSchema},
		{"name": "supadupa_set_project_pitr_policy", "description": "Update project PITR policy.", "inputSchema": pitrPolicySchema},
		{"name": "supadupa_list_project_wal_archives", "description": "List project PITR WAL archives.", "inputSchema": refSchema},
		{"name": "supadupa_archive_project_wal", "description": "Archive a project WAL segment for PITR.", "inputSchema": refSchema},
		{"name": "supadupa_list_project_branches", "description": "List project preview branches.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_branch", "description": "Create a preview branch project.", "inputSchema": branchSchema},
		{"name": "supadupa_delete_project_branch", "description": "Delete a preview branch project.", "inputSchema": branchSchema},
		{"name": "supadupa_list_project_replicas", "description": "List project read replicas.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_replica", "description": "Create a project read replica declaration.", "inputSchema": replicaSchema},
		{"name": "supadupa_get_project_replica_routing", "description": "Get project read routing and failover state.", "inputSchema": refSchema},
		{"name": "supadupa_promote_project_replica", "description": "Promote a project read replica.", "inputSchema": replicaActionSchema},
		{"name": "supadupa_delete_project_replica", "description": "Delete a project read replica.", "inputSchema": replicaActionSchema},
		{"name": "supadupa_failover_project_replica", "description": "Fail over to the best healthy project read replica.", "inputSchema": replicaFailoverSchema},
		{"name": "supadupa_list_project_domains", "description": "List project custom domains.", "inputSchema": refSchema},
		{"name": "supadupa_add_project_domain", "description": "Add a project custom domain.", "inputSchema": domainSchema},
		{"name": "supadupa_delete_project_domain", "description": "Delete a project custom domain.", "inputSchema": domainSchema},
		{"name": "supadupa_list_project_routes", "description": "List project ingress routes.", "inputSchema": refSchema},
		{"name": "supadupa_list_project_log_drains", "description": "List project log drains.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_log_drain", "description": "Create a project log drain.", "inputSchema": logDrainSchema},
		{"name": "supadupa_delete_project_log_drain", "description": "Delete a project log drain.", "inputSchema": functionRecordSchema},
		{"name": "supadupa_get_project_network", "description": "Get a project effective network policy, including IP allowlist and private connectivity declarations.", "inputSchema": refSchema},
		{"name": "supadupa_list_project_network_connections", "description": "List project private network connectivity declarations.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_network_connection", "description": "Create a project private network connectivity declaration.", "inputSchema": networkConnectionSchema},
		{"name": "supadupa_delete_project_network_connection", "description": "Delete a project private network connectivity declaration.", "inputSchema": functionRecordSchema},
		{"name": "supadupa_get_project_cdn_policy", "description": "Get a project CDN policy.", "inputSchema": refSchema},
		{"name": "supadupa_set_project_cdn_policy", "description": "Create or update a project CDN policy.", "inputSchema": cdnPolicySchema},
		{"name": "supadupa_list_project_cdn_invalidations", "description": "List project CDN invalidation jobs.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_cdn_invalidation", "description": "Create a project CDN invalidation job.", "inputSchema": cdnInvalidationSchema},
		{"name": "supadupa_create_project_cdn_object_event", "description": "Record a storage object event for Smart CDN revalidation.", "inputSchema": cdnObjectEventSchema},
		{"name": "supadupa_list_project_storage_buckets", "description": "List project Storage bucket declarations.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_storage_bucket", "description": "Create a project Storage bucket declaration.", "inputSchema": storageBucketSchema},
		{"name": "supadupa_delete_project_storage_bucket", "description": "Delete a project Storage bucket declaration.", "inputSchema": functionNameSchema},
		{"name": "supadupa_list_project_vector_buckets", "description": "List project Vector bucket declarations.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_vector_bucket", "description": "Create a project Vector bucket declaration.", "inputSchema": vectorBucketSchema},
		{"name": "supadupa_delete_project_vector_bucket", "description": "Delete a project Vector bucket declaration.", "inputSchema": functionNameSchema},
		{"name": "supadupa_list_project_analytics_buckets", "description": "List project Iceberg Analytics Bucket declarations.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_analytics_bucket", "description": "Create a project Iceberg Analytics Bucket declaration.", "inputSchema": analyticsBucketSchema},
		{"name": "supadupa_delete_project_analytics_bucket", "description": "Delete a project Iceberg Analytics Bucket declaration.", "inputSchema": functionNameSchema},
		{"name": "supadupa_list_project_replication_pipelines", "description": "List logical replication, ETL, and analytics export pipelines.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_replication_pipeline", "description": "Create a project replication or ETL pipeline declaration.", "inputSchema": replicationSchema},
		{"name": "supadupa_delete_project_replication_pipeline", "description": "Delete a project replication or ETL pipeline declaration.", "inputSchema": functionRecordSchema},
		{"name": "supadupa_list_project_embedding_jobs", "description": "List automatic embedding job declarations.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_embedding_job", "description": "Create an automatic embedding job declaration.", "inputSchema": embeddingSchema},
		{"name": "supadupa_delete_project_embedding_job", "description": "Delete an automatic embedding job declaration.", "inputSchema": functionRecordSchema},
		{"name": "supadupa_list_project_functions", "description": "List deployed Edge Functions for a project.", "inputSchema": refSchema},
		{"name": "supadupa_deploy_project_function", "description": "Deploy or redeploy a project Edge Function.", "inputSchema": functionDeploySchema},
		{"name": "supadupa_delete_project_function", "description": "Delete a project Edge Function.", "inputSchema": functionNameSchema},
		{"name": "supadupa_list_project_function_regions", "description": "List Edge Function regional invocation declarations.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_function_region", "description": "Create an Edge Function regional invocation declaration.", "inputSchema": functionRegionSchema},
		{"name": "supadupa_delete_project_function_region", "description": "Delete an Edge Function regional invocation declaration.", "inputSchema": functionRecordSchema},
		{"name": "supadupa_list_project_function_storage_mounts", "description": "List Edge Function persistent storage mount declarations.", "inputSchema": refSchema},
		{"name": "supadupa_create_project_function_storage_mount", "description": "Create an Edge Function persistent storage mount declaration.", "inputSchema": functionMountSchema},
		{"name": "supadupa_delete_project_function_storage_mount", "description": "Delete an Edge Function persistent storage mount declaration.", "inputSchema": functionRecordSchema},
		{"name": "supadupa_pause_project", "description": "Pause a project.", "inputSchema": refSchema},
		{"name": "supadupa_resume_project", "description": "Resume a project.", "inputSchema": refSchema},
		{"name": "supadupa_restart_project", "description": "Restart a project.", "inputSchema": refSchema},
		{"name": "supadupa_upgrade_project", "description": "Upgrade a project's Supabase stack version.", "inputSchema": projectUpgradeSchema},
		{"name": "supadupa_scale_project", "description": "Resize a project's resource tier.", "inputSchema": projectScaleSchema},
		{"name": "supadupa_destroy_project", "description": "Destroy a project, optionally retaining data volumes.", "inputSchema": projectDestroySchema},
		{"name": "supadupa_trigger_backup", "description": "Trigger a logical backup for a project.", "inputSchema": refSchema},
	}
}

func decodeRef(payload json.RawMessage) (string, error) {
	var args refArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return "", err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	if args.Ref == "" {
		return "", fmt.Errorf("ref is required")
	}
	return args.Ref, nil
}

func decodeUserCreateArgs(payload json.RawMessage) (userCreateArgs, error) {
	var args userCreateArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return userCreateArgs{}, err
	}
	args.Email = strings.ToLower(strings.TrimSpace(args.Email))
	args.Password = strings.TrimSpace(args.Password)
	args.Role = strings.TrimSpace(args.Role)
	if args.Role == "" {
		args.Role = "developer"
	}
	if args.Email == "" {
		return userCreateArgs{}, fmt.Errorf("email is required")
	}
	if args.Password == "" {
		return userCreateArgs{}, fmt.Errorf("password is required")
	}
	return args, nil
}

func decodeMFACodeArgs(payload json.RawMessage) (mfaCodeArgs, error) {
	var args mfaCodeArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return mfaCodeArgs{}, err
	}
	args.Code = strings.TrimSpace(args.Code)
	if args.Code == "" {
		return mfaCodeArgs{}, fmt.Errorf("code is required")
	}
	return args, nil
}

func decodeSCIMArgs(payload json.RawMessage, requireID bool) (scimArgs, error) {
	var args scimArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return scimArgs{}, err
	}
	args.ID = strings.TrimSpace(args.ID)
	args.OrgID = strings.TrimSpace(args.OrgID)
	args.UserName = strings.TrimSpace(args.UserName)
	args.Email = strings.TrimSpace(args.Email)
	args.DisplayName = strings.TrimSpace(args.DisplayName)
	args.Password = strings.TrimSpace(args.Password)
	args.Role = strings.TrimSpace(args.Role)
	args.Slug = strings.TrimSpace(args.Slug)
	args.Members = cleanStringList(args.Members)
	if requireID && args.ID == "" {
		return scimArgs{}, fmt.Errorf("id is required")
	}
	return args, nil
}

func scimUserPayload(args scimArgs) (map[string]any, error) {
	userName := args.UserName
	email := args.Email
	if userName == "" {
		userName = email
	}
	if email == "" {
		email = userName
	}
	if userName == "" {
		return nil, fmt.Errorf("user_name or email is required")
	}
	active := true
	if args.Active != nil {
		active = *args.Active
	}
	return map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:User", "urn:supadupa:params:scim:schemas:extension:User"},
		"userName":    userName,
		"displayName": args.DisplayName,
		"active":      active,
		"emails":      []map[string]any{{"value": email, "primary": true}},
		"password":    args.Password,
		"urn:supadupa:params:scim:schemas:extension:User": map[string]string{
			"role": args.Role,
		},
	}, nil
}

func scimGroupPayload(args scimArgs) (map[string]any, error) {
	if args.OrgID == "" {
		return nil, fmt.Errorf("org_id is required")
	}
	if args.DisplayName == "" {
		return nil, fmt.Errorf("display_name is required")
	}
	members := make([]map[string]string, 0, len(args.Members))
	for _, member := range args.Members {
		members = append(members, map[string]string{"value": member, "display": member})
	}
	return map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Group", "urn:supadupa:params:scim:schemas:extension:Group"},
		"externalId":  args.OrgID,
		"displayName": args.DisplayName,
		"members":     members,
		"urn:supadupa:params:scim:schemas:extension:Group": map[string]string{
			"org_id": args.OrgID,
			"slug":   args.Slug,
		},
	}, nil
}

func scimDeprovisionPayload() map[string]any {
	return map[string]any{
		"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": []map[string]any{{
			"op":    "replace",
			"path":  "active",
			"value": false,
		}},
	}
}

func decodeOrgArgs(payload json.RawMessage) (orgArgs, error) {
	var args orgArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return orgArgs{}, err
	}
	args.OrgID = strings.TrimSpace(args.OrgID)
	if args.OrgID == "" {
		return orgArgs{}, fmt.Errorf("org_id is required")
	}
	return args, nil
}

func decodeBillingInvoiceArgs(payload json.RawMessage, requireInvoiceID bool) (billingInvoiceArgs, error) {
	var args billingInvoiceArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return billingInvoiceArgs{}, err
	}
	args.OrgID = strings.TrimSpace(args.OrgID)
	args.InvoiceID = strings.TrimSpace(args.InvoiceID)
	args.UsageSnapshotID = strings.TrimSpace(args.UsageSnapshotID)
	args.Currency = strings.ToUpper(strings.TrimSpace(args.Currency))
	args.Status = strings.TrimSpace(args.Status)
	if args.Currency == "" {
		args.Currency = "USD"
	}
	if args.Status == "" {
		args.Status = "draft"
	}
	if args.DueDays == 0 {
		args.DueDays = 30
	}
	if args.OrgID == "" {
		return billingInvoiceArgs{}, fmt.Errorf("org_id is required")
	}
	if requireInvoiceID && args.InvoiceID == "" {
		return billingInvoiceArgs{}, fmt.Errorf("invoice_id is required")
	}
	return args, nil
}

func decodeOrgCreateArgs(payload json.RawMessage) (orgCreateArgs, error) {
	var args orgCreateArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return orgCreateArgs{}, err
	}
	args.Name = strings.TrimSpace(args.Name)
	if args.Name == "" {
		return orgCreateArgs{}, fmt.Errorf("name is required")
	}
	return args, nil
}

func decodeOrgUpdateArgs(payload json.RawMessage) (orgUpdateArgs, error) {
	var args orgUpdateArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return orgUpdateArgs{}, err
	}
	args.OrgID = strings.TrimSpace(args.OrgID)
	args.Name = strings.TrimSpace(args.Name)
	if args.OrgID == "" {
		return orgUpdateArgs{}, fmt.Errorf("org_id is required")
	}
	if args.Name == "" {
		return orgUpdateArgs{}, fmt.Errorf("name is required")
	}
	return args, nil
}

func decodeOrgMemberArgs(payload json.RawMessage, requireRole bool) (orgMemberArgs, error) {
	var args orgMemberArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return orgMemberArgs{}, err
	}
	args.OrgID = strings.TrimSpace(args.OrgID)
	args.Email = strings.ToLower(strings.TrimSpace(args.Email))
	args.Role = strings.TrimSpace(args.Role)
	if args.OrgID == "" {
		return orgMemberArgs{}, fmt.Errorf("org_id is required")
	}
	if args.Email == "" {
		return orgMemberArgs{}, fmt.Errorf("email is required")
	}
	if requireRole && args.Role == "" {
		args.Role = "developer"
	}
	return args, nil
}

func decodeOrgTeamArgs(payload json.RawMessage, requireName bool) (orgTeamArgs, error) {
	var args orgTeamArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return orgTeamArgs{}, err
	}
	args.OrgID = strings.TrimSpace(args.OrgID)
	args.Slug = strings.TrimSpace(args.Slug)
	args.Name = strings.TrimSpace(args.Name)
	if args.OrgID == "" {
		return orgTeamArgs{}, fmt.Errorf("org_id is required")
	}
	if args.Slug == "" && !requireName {
		return orgTeamArgs{}, fmt.Errorf("slug is required")
	}
	if requireName && args.Name == "" {
		return orgTeamArgs{}, fmt.Errorf("name is required")
	}
	return args, nil
}

func decodeOrgTeamMemberArgs(payload json.RawMessage, requireEmail bool) (orgTeamMemberArgs, error) {
	var args orgTeamMemberArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return orgTeamMemberArgs{}, err
	}
	args.OrgID = strings.TrimSpace(args.OrgID)
	args.Slug = strings.TrimSpace(args.Slug)
	args.Email = strings.ToLower(strings.TrimSpace(args.Email))
	if args.OrgID == "" {
		return orgTeamMemberArgs{}, fmt.Errorf("org_id is required")
	}
	if args.Slug == "" {
		return orgTeamMemberArgs{}, fmt.Errorf("slug is required")
	}
	if requireEmail && args.Email == "" {
		return orgTeamMemberArgs{}, fmt.Errorf("email is required")
	}
	return args, nil
}

func decodeOrgQuotaArgs(payload json.RawMessage) (orgQuotaArgs, error) {
	var args orgQuotaArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return orgQuotaArgs{}, err
	}
	args.OrgID = strings.TrimSpace(args.OrgID)
	if args.OrgID == "" {
		return orgQuotaArgs{}, fmt.Errorf("org_id is required")
	}
	return args, nil
}

func decodeHostArgs(payload json.RawMessage, requireCreateFields bool) (hostArgs, error) {
	var args hostArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return hostArgs{}, err
	}
	args.ID = strings.TrimSpace(args.ID)
	args.Name = strings.TrimSpace(args.Name)
	args.Address = strings.TrimSpace(args.Address)
	if requireCreateFields {
		if args.Name == "" {
			return hostArgs{}, fmt.Errorf("name is required")
		}
		if args.Address == "" {
			return hostArgs{}, fmt.Errorf("address is required")
		}
		return args, nil
	}
	if args.ID == "" {
		return hostArgs{}, fmt.Errorf("id is required")
	}
	return args, nil
}

func decodePlatformDefaultsArgs(payload json.RawMessage) (platformDefaultsArgs, error) {
	var args platformDefaultsArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return platformDefaultsArgs{}, err
	}
	args.Domain = strings.TrimSpace(args.Domain)
	args.StackVersion = strings.TrimSpace(args.StackVersion)
	args.Profile = strings.TrimSpace(args.Profile)
	args.ResourceTier = strings.TrimSpace(args.ResourceTier)
	args.BackupSchedule = strings.TrimSpace(args.BackupSchedule)
	args.SMTPHost = strings.TrimSpace(args.SMTPHost)
	args.SMTPSenderName = strings.TrimSpace(args.SMTPSenderName)
	args.SMTPSenderEmail = strings.TrimSpace(args.SMTPSenderEmail)
	args.SMTPUsername = strings.TrimSpace(args.SMTPUsername)
	args.SMTPPasswordHandle = strings.TrimSpace(args.SMTPPasswordHandle)
	args.SMTPTLSMode = strings.ToLower(strings.TrimSpace(args.SMTPTLSMode))
	if args.Domain == "" {
		args.Domain = "supadupa.test"
	}
	if args.StackVersion == "" {
		args.StackVersion = "latest"
	}
	if args.Profile == "" {
		args.Profile = "full"
	}
	if args.ResourceTier == "" {
		args.ResourceTier = "small"
	}
	if args.BackupSchedule == "" {
		args.BackupSchedule = "daily"
	}
	if args.SMTPPort == 0 {
		args.SMTPPort = 587
	}
	if args.SMTPTLSMode == "" {
		args.SMTPTLSMode = "starttls"
	}
	return args, nil
}

func decodePlatformSSOArgs(payload json.RawMessage) (platformSSOArgs, error) {
	var args platformSSOArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return platformSSOArgs{}, err
	}
	args.IDPEntityID = strings.TrimSpace(args.IDPEntityID)
	args.SSOURL = strings.TrimSpace(args.SSOURL)
	args.Certificate = strings.TrimSpace(args.Certificate)
	args.ACSURL = strings.TrimSpace(args.ACSURL)
	args.MetadataURL = strings.TrimSpace(args.MetadataURL)
	args.EmailDomain = strings.ToLower(strings.TrimSpace(args.EmailDomain))
	args.DefaultRole = strings.TrimSpace(args.DefaultRole)
	if args.DefaultRole == "" {
		args.DefaultRole = "developer"
	}
	return args, nil
}

func decodeProjectCreateArgs(payload json.RawMessage) (projectCreateArgs, error) {
	var args projectCreateArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectCreateArgs{}, err
	}
	args.OrgID = strings.TrimSpace(args.OrgID)
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.HostID = strings.TrimSpace(args.HostID)
	args.Domain = strings.TrimSpace(args.Domain)
	args.StackVersion = strings.TrimSpace(args.StackVersion)
	args.Profile = strings.TrimSpace(args.Profile)
	args.ResourceTier = strings.TrimSpace(args.ResourceTier)
	for key, value := range args.Environment {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" {
			delete(args.Environment, key)
			continue
		}
		if trimmedKey != key {
			delete(args.Environment, key)
		}
		args.Environment[trimmedKey] = trimmedValue
	}
	if args.OrgID == "" {
		return projectCreateArgs{}, fmt.Errorf("org_id is required")
	}
	if args.Ref == "" {
		return projectCreateArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectCreateArgs{}, fmt.Errorf("name is required")
	}
	return args, nil
}

func decodeProjectAccessArgs(payload json.RawMessage) (projectAccessArgs, error) {
	var args projectAccessArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectAccessArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.SubjectType = strings.TrimSpace(args.SubjectType)
	args.SubjectID = strings.TrimSpace(args.SubjectID)
	args.Role = strings.TrimSpace(args.Role)
	if args.Ref == "" {
		return projectAccessArgs{}, fmt.Errorf("ref is required")
	}
	if args.SubjectType == "" {
		args.SubjectType = "team"
	}
	if args.SubjectID == "" {
		return projectAccessArgs{}, fmt.Errorf("subject_id is required")
	}
	if args.Role == "" {
		args.Role = "viewer"
	}
	return args, nil
}

func limitOrDefault(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func cleanStringList(values []string) []string {
	if values == nil {
		return []string{}
	}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}

func defaultBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func decodeProjectBranchArgs(payload json.RawMessage) (projectBranchArgs, error) {
	var args projectBranchArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectBranchArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.BranchRef = strings.TrimSpace(args.BranchRef)
	args.Name = strings.TrimSpace(args.Name)
	if args.Ref == "" {
		return projectBranchArgs{}, fmt.Errorf("ref is required")
	}
	if args.BranchRef == "" {
		return projectBranchArgs{}, fmt.Errorf("branch_ref is required")
	}
	return args, nil
}

func decodeProjectReplicaArgs(payload json.RawMessage) (projectReplicaArgs, error) {
	var args projectReplicaArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectReplicaArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.HostID = strings.TrimSpace(args.HostID)
	args.Region = strings.TrimSpace(args.Region)
	args.Tier = strings.TrimSpace(args.Tier)
	if args.Ref == "" {
		return projectReplicaArgs{}, fmt.Errorf("ref is required")
	}
	if args.Tier == "" {
		args.Tier = "small"
	}
	if args.ReadWeight == 0 {
		args.ReadWeight = 100
	}
	if args.FailoverPriority == 0 {
		args.FailoverPriority = 1
	}
	return args, nil
}

func decodeProjectReplicaActionArgs(payload json.RawMessage, requireID bool, fallbackReason string) (projectReplicaActionArgs, error) {
	var args projectReplicaActionArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectReplicaActionArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.ID = strings.TrimSpace(args.ID)
	args.Reason = strings.TrimSpace(args.Reason)
	if args.Ref == "" {
		return projectReplicaActionArgs{}, fmt.Errorf("ref is required")
	}
	if requireID && args.ID == "" {
		return projectReplicaActionArgs{}, fmt.Errorf("id is required")
	}
	if args.Reason == "" {
		args.Reason = fallbackReason
	}
	return args, nil
}

func decodeProjectLifecycleArgs(payload json.RawMessage, action string) (projectLifecycleArgs, error) {
	var args projectLifecycleArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectLifecycleArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Version = strings.TrimSpace(args.Version)
	args.Tier = strings.TrimSpace(args.Tier)
	if args.Ref == "" {
		return projectLifecycleArgs{}, fmt.Errorf("ref is required")
	}
	switch action {
	case "upgrade":
		if args.Version == "" {
			return projectLifecycleArgs{}, fmt.Errorf("version is required")
		}
	case "scale":
		if args.Tier == "" {
			return projectLifecycleArgs{}, fmt.Errorf("tier is required")
		}
	}
	return args, nil
}

func decodeProjectBackupRestoreArgs(payload json.RawMessage) (projectBackupRestoreArgs, error) {
	var args projectBackupRestoreArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectBackupRestoreArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.BackupID = strings.TrimSpace(args.BackupID)
	if args.Ref == "" {
		return projectBackupRestoreArgs{}, fmt.Errorf("ref is required")
	}
	if args.BackupID == "" {
		return projectBackupRestoreArgs{}, fmt.Errorf("backup_id is required")
	}
	return args, nil
}

func decodeProjectBackupPolicyArgs(payload json.RawMessage) (projectBackupPolicyArgs, error) {
	var args projectBackupPolicyArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectBackupPolicyArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Schedule = strings.TrimSpace(args.Schedule)
	args.Kind = strings.TrimSpace(args.Kind)
	if args.Ref == "" {
		return projectBackupPolicyArgs{}, fmt.Errorf("ref is required")
	}
	if args.Schedule == "" {
		args.Schedule = "daily"
	}
	if args.Kind == "" {
		args.Kind = "logical"
	}
	return args, nil
}

func decodeProjectPITRPolicyArgs(payload json.RawMessage) (projectPITRPolicyArgs, error) {
	var args projectPITRPolicyArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectPITRPolicyArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.ArchiveBucket = strings.TrimSpace(args.ArchiveBucket)
	if args.Ref == "" {
		return projectPITRPolicyArgs{}, fmt.Errorf("ref is required")
	}
	if args.RetentionDays == 0 {
		args.RetentionDays = 7
	}
	return args, nil
}

func decodeProjectSecretArgs(payload json.RawMessage) (projectSecretArgs, error) {
	var args projectSecretArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectSecretArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Kind = strings.TrimSpace(args.Kind)
	if args.Ref == "" {
		return projectSecretArgs{}, fmt.Errorf("ref is required")
	}
	if args.Kind == "" {
		return projectSecretArgs{}, fmt.Errorf("kind is required")
	}
	return args, nil
}

func decodeProjectConfigArgs(payload json.RawMessage) (projectConfigArgs, error) {
	var args projectConfigArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectConfigArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Area = strings.TrimSpace(args.Area)
	if args.Ref == "" {
		return projectConfigArgs{}, fmt.Errorf("ref is required")
	}
	if args.Area == "" {
		return projectConfigArgs{}, fmt.Errorf("area is required")
	}
	return args, nil
}

func decodeProjectConfigWriteArgs(payload json.RawMessage) (projectConfigArgs, error) {
	args, err := decodeProjectConfigArgs(payload)
	if err != nil {
		return projectConfigArgs{}, err
	}
	for key, value := range args.Config {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			delete(args.Config, key)
			continue
		}
		if trimmedKey != key {
			delete(args.Config, key)
		}
		args.Config[trimmedKey] = value
	}
	if len(args.Config) == 0 {
		return projectConfigArgs{}, fmt.Errorf("config is required")
	}
	return args, nil
}

func decodeProjectServicesArgs(payload json.RawMessage) (projectServicesArgs, error) {
	var args projectServicesArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectServicesArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	if args.Ref == "" {
		return projectServicesArgs{}, fmt.Errorf("ref is required")
	}
	for key, value := range args.Services {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			delete(args.Services, key)
			continue
		}
		if trimmedKey != key {
			delete(args.Services, key)
		}
		args.Services[trimmedKey] = value
	}
	if len(args.Services) == 0 {
		return projectServicesArgs{}, fmt.Errorf("services is required")
	}
	return args, nil
}

func decodeProjectDomainArgs(payload json.RawMessage) (projectDomainArgs, error) {
	var args projectDomainArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectDomainArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.FQDN = strings.Trim(strings.ToLower(strings.TrimSpace(args.FQDN)), ".")
	if args.Ref == "" {
		return projectDomainArgs{}, fmt.Errorf("ref is required")
	}
	if args.FQDN == "" {
		return projectDomainArgs{}, fmt.Errorf("fqdn is required")
	}
	return args, nil
}

func decodeProjectLogDrainArgs(payload json.RawMessage) (projectLogDrainArgs, error) {
	var args projectLogDrainArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectLogDrainArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Target = strings.TrimSpace(args.Target)
	if args.Ref == "" {
		return projectLogDrainArgs{}, fmt.Errorf("ref is required")
	}
	if args.Target == "" {
		return projectLogDrainArgs{}, fmt.Errorf("target is required")
	}
	if args.Config == nil {
		args.Config = map[string]string{}
	}
	return args, nil
}

func decodeProjectNetworkConnectionArgs(payload json.RawMessage) (projectNetworkConnectionArgs, error) {
	var args projectNetworkConnectionArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectNetworkConnectionArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.Type = strings.TrimSpace(args.Type)
	args.Provider = strings.TrimSpace(args.Provider)
	args.Region = strings.TrimSpace(args.Region)
	args.EndpointID = strings.TrimSpace(args.EndpointID)
	if args.Ref == "" {
		return projectNetworkConnectionArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectNetworkConnectionArgs{}, fmt.Errorf("name is required")
	}
	if args.Type == "" {
		args.Type = "operator_network"
	}
	if args.Provider == "" {
		args.Provider = "operator"
	}
	if args.CIDRs == nil {
		args.CIDRs = []string{}
	}
	if args.Config == nil {
		args.Config = map[string]string{}
	}
	return args, nil
}

func decodeProjectCDNPolicyArgs(payload json.RawMessage) (projectCDNPolicyArgs, error) {
	var args projectCDNPolicyArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectCDNPolicyArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.CacheControl = strings.TrimSpace(args.CacheControl)
	if args.Ref == "" {
		return projectCDNPolicyArgs{}, fmt.Errorf("ref is required")
	}
	if args.BrowserTTLSeconds == 0 {
		args.BrowserTTLSeconds = 3600
	}
	if args.EdgeTTLSeconds == 0 {
		args.EdgeTTLSeconds = 3600
	}
	if args.StaleWhileRevalidateSeconds == 0 {
		args.StaleWhileRevalidateSeconds = 60
	}
	args.IncludedPaths = cleanStringList(args.IncludedPaths)
	args.ExcludedPaths = cleanStringList(args.ExcludedPaths)
	return args, nil
}

func decodeProjectCDNInvalidationArgs(payload json.RawMessage) (projectCDNInvalidationArgs, error) {
	var args projectCDNInvalidationArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectCDNInvalidationArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	if args.Ref == "" {
		return projectCDNInvalidationArgs{}, fmt.Errorf("ref is required")
	}
	args.Paths = cleanStringList(args.Paths)
	if len(args.Paths) == 0 {
		return projectCDNInvalidationArgs{}, fmt.Errorf("paths is required")
	}
	return args, nil
}

func decodeProjectCDNObjectEventArgs(payload json.RawMessage) (projectCDNObjectEventArgs, error) {
	var args projectCDNObjectEventArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectCDNObjectEventArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.EventID = strings.TrimSpace(args.EventID)
	args.Bucket = strings.TrimSpace(args.Bucket)
	args.ObjectPath = strings.TrimSpace(args.ObjectPath)
	args.EventType = strings.TrimSpace(args.EventType)
	if args.Ref == "" {
		return projectCDNObjectEventArgs{}, fmt.Errorf("ref is required")
	}
	if args.EventID == "" {
		return projectCDNObjectEventArgs{}, fmt.Errorf("event_id is required")
	}
	if args.Bucket == "" {
		return projectCDNObjectEventArgs{}, fmt.Errorf("bucket is required")
	}
	if args.ObjectPath == "" {
		return projectCDNObjectEventArgs{}, fmt.Errorf("object_path is required")
	}
	if args.EventType == "" {
		return projectCDNObjectEventArgs{}, fmt.Errorf("event_type is required")
	}
	return args, nil
}

func decodeProjectDatabaseExtensionArgs(payload json.RawMessage) (projectDatabaseExtensionArgs, error) {
	var args projectDatabaseExtensionArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectDatabaseExtensionArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.Schema = strings.TrimSpace(args.Schema)
	args.Version = strings.TrimSpace(args.Version)
	if args.Ref == "" {
		return projectDatabaseExtensionArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectDatabaseExtensionArgs{}, fmt.Errorf("name is required")
	}
	return args, nil
}

func decodeProjectDatabaseCronJobArgs(payload json.RawMessage) (projectDatabaseCronJobArgs, error) {
	var args projectDatabaseCronJobArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectDatabaseCronJobArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.Schedule = strings.TrimSpace(args.Schedule)
	args.Command = strings.TrimSpace(args.Command)
	args.Database = strings.TrimSpace(args.Database)
	args.Username = strings.TrimSpace(args.Username)
	if args.Ref == "" {
		return projectDatabaseCronJobArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectDatabaseCronJobArgs{}, fmt.Errorf("name is required")
	}
	if args.Schedule == "" {
		return projectDatabaseCronJobArgs{}, fmt.Errorf("schedule is required")
	}
	if args.Command == "" {
		return projectDatabaseCronJobArgs{}, fmt.Errorf("command is required")
	}
	if args.Database == "" {
		args.Database = "postgres"
	}
	if args.Username == "" {
		args.Username = "postgres"
	}
	if args.TimeoutSeconds == 0 {
		args.TimeoutSeconds = 60
	}
	if args.MaxRuntimeSeconds == 0 {
		args.MaxRuntimeSeconds = 60
	}
	if args.Metadata == nil {
		args.Metadata = map[string]string{}
	}
	return args, nil
}

func decodeProjectDatabaseQueueArgs(payload json.RawMessage) (projectDatabaseQueueArgs, error) {
	var args projectDatabaseQueueArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectDatabaseQueueArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.Schema = strings.TrimSpace(args.Schema)
	args.DeadLetterQueue = strings.TrimSpace(args.DeadLetterQueue)
	if args.Ref == "" {
		return projectDatabaseQueueArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectDatabaseQueueArgs{}, fmt.Errorf("name is required")
	}
	if args.Schema == "" {
		args.Schema = "pgmq"
	}
	if args.RetentionMinutes == 0 {
		args.RetentionMinutes = 1440
	}
	if args.VisibilityTimeoutSeconds == 0 {
		args.VisibilityTimeoutSeconds = 30
	}
	if args.MaxRetries == 0 {
		args.MaxRetries = 5
	}
	if args.Metadata == nil {
		args.Metadata = map[string]string{}
	}
	return args, nil
}

func decodeProjectDatabaseWebhookArgs(payload json.RawMessage) (projectDatabaseWebhookArgs, error) {
	var args projectDatabaseWebhookArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectDatabaseWebhookArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.Schema = strings.TrimSpace(args.Schema)
	args.Table = strings.TrimSpace(args.Table)
	args.Endpoint = strings.TrimSpace(args.Endpoint)
	args.HTTPMethod = strings.ToUpper(strings.TrimSpace(args.HTTPMethod))
	if args.Ref == "" {
		return projectDatabaseWebhookArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectDatabaseWebhookArgs{}, fmt.Errorf("name is required")
	}
	if args.Table == "" {
		return projectDatabaseWebhookArgs{}, fmt.Errorf("table is required")
	}
	if args.Endpoint == "" {
		return projectDatabaseWebhookArgs{}, fmt.Errorf("endpoint is required")
	}
	if args.Schema == "" {
		args.Schema = "public"
	}
	args.Events = cleanStringList(args.Events)
	if len(args.Events) == 0 {
		args.Events = []string{"insert", "update", "delete"}
	}
	if args.HTTPMethod == "" {
		args.HTTPMethod = http.MethodPost
	}
	if args.TimeoutSeconds == 0 {
		args.TimeoutSeconds = 10
	}
	if args.RetryCount == 0 {
		args.RetryCount = 3
	}
	if args.Headers == nil {
		args.Headers = map[string]string{}
	}
	if args.Metadata == nil {
		args.Metadata = map[string]string{}
	}
	return args, nil
}

func decodeProjectDatabaseSchemaArgs(payload json.RawMessage) (projectDatabaseSchemaArgs, error) {
	var args projectDatabaseSchemaArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectDatabaseSchemaArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.Version = strings.TrimSpace(args.Version)
	args.Schema = strings.TrimSpace(args.Schema)
	args.SQL = strings.TrimSpace(args.SQL)
	if args.Ref == "" {
		return projectDatabaseSchemaArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectDatabaseSchemaArgs{}, fmt.Errorf("name is required")
	}
	if args.Version == "" {
		return projectDatabaseSchemaArgs{}, fmt.Errorf("version is required")
	}
	if args.SQL == "" {
		return projectDatabaseSchemaArgs{}, fmt.Errorf("sql is required")
	}
	if args.Schema == "" {
		args.Schema = "public"
	}
	if args.Metadata == nil {
		args.Metadata = map[string]string{}
	}
	return args, nil
}

func decodeProjectDatabaseSchemaKeyArgs(payload json.RawMessage) (projectDatabaseSchemaKeyArgs, error) {
	var args projectDatabaseSchemaKeyArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectDatabaseSchemaKeyArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.Version = strings.TrimSpace(args.Version)
	if args.Ref == "" {
		return projectDatabaseSchemaKeyArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectDatabaseSchemaKeyArgs{}, fmt.Errorf("name is required")
	}
	if args.Version == "" {
		return projectDatabaseSchemaKeyArgs{}, fmt.Errorf("version is required")
	}
	return args, nil
}

func decodeProjectDatabaseRoleArgs(payload json.RawMessage) (projectDatabaseRoleArgs, error) {
	var args projectDatabaseRoleArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectDatabaseRoleArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.PasswordSecretHandle = strings.TrimSpace(args.PasswordSecretHandle)
	if args.Ref == "" {
		return projectDatabaseRoleArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectDatabaseRoleArgs{}, fmt.Errorf("name is required")
	}
	args.MemberOf = cleanStringList(args.MemberOf)
	if args.SchemaGrants == nil {
		args.SchemaGrants = map[string]string{}
	}
	if args.Metadata == nil {
		args.Metadata = map[string]string{}
	}
	return args, nil
}

func decodeProjectAuthClientArgs(payload json.RawMessage) (projectAuthClientArgs, error) {
	var args projectAuthClientArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectAuthClientArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.ClientID = strings.TrimSpace(args.ClientID)
	args.ClientSecretHandle = strings.TrimSpace(args.ClientSecretHandle)
	if args.Ref == "" {
		return projectAuthClientArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectAuthClientArgs{}, fmt.Errorf("name is required")
	}
	if args.RedirectURIs == nil {
		args.RedirectURIs = []string{}
	}
	if args.GrantTypes == nil {
		args.GrantTypes = []string{}
	}
	if args.Scopes == nil {
		args.Scopes = []string{}
	}
	if args.Confidential == nil {
		defaultConfidential := true
		args.Confidential = &defaultConfidential
	}
	return args, nil
}

func decodeProjectAuthClientIDArgs(payload json.RawMessage) (projectAuthClientIDArgs, error) {
	var args projectAuthClientIDArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectAuthClientIDArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.ClientID = strings.TrimSpace(args.ClientID)
	if args.Ref == "" {
		return projectAuthClientIDArgs{}, fmt.Errorf("ref is required")
	}
	if args.ClientID == "" {
		return projectAuthClientIDArgs{}, fmt.Errorf("client_id is required")
	}
	return args, nil
}

func decodeProjectAuthHookArgs(payload json.RawMessage) (projectAuthHookArgs, error) {
	var args projectAuthHookArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectAuthHookArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.HookType = strings.TrimSpace(args.HookType)
	args.TargetURI = strings.TrimSpace(args.TargetURI)
	args.EdgeFunction = strings.TrimSpace(args.EdgeFunction)
	args.SecretHandle = strings.TrimSpace(args.SecretHandle)
	if args.Ref == "" {
		return projectAuthHookArgs{}, fmt.Errorf("ref is required")
	}
	if args.HookType == "" {
		return projectAuthHookArgs{}, fmt.Errorf("hook_type is required")
	}
	if args.Enabled == nil {
		defaultEnabled := true
		args.Enabled = &defaultEnabled
	}
	if args.Headers == nil {
		args.Headers = map[string]string{}
	}
	if args.TimeoutMS == 0 {
		args.TimeoutMS = 5000
	}
	return args, nil
}

func decodeProjectAuthHookTypeArgs(payload json.RawMessage) (projectAuthHookTypeArgs, error) {
	var args projectAuthHookTypeArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectAuthHookTypeArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.HookType = strings.TrimSpace(args.HookType)
	if args.Ref == "" {
		return projectAuthHookTypeArgs{}, fmt.Errorf("ref is required")
	}
	if args.HookType == "" {
		return projectAuthHookTypeArgs{}, fmt.Errorf("hook_type is required")
	}
	return args, nil
}

func decodeProjectStorageBucketArgs(payload json.RawMessage) (projectStorageBucketArgs, error) {
	var args projectStorageBucketArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectStorageBucketArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.CacheControl = strings.TrimSpace(args.CacheControl)
	if args.Ref == "" {
		return projectStorageBucketArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectStorageBucketArgs{}, fmt.Errorf("name is required")
	}
	if args.AllowedMimeTypes == nil {
		args.AllowedMimeTypes = []string{}
	}
	if args.Metadata == nil {
		args.Metadata = map[string]string{}
	}
	return args, nil
}

func decodeProjectVectorBucketArgs(payload json.RawMessage) (projectVectorBucketArgs, error) {
	var args projectVectorBucketArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectVectorBucketArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.Distance = strings.TrimSpace(args.Distance)
	args.IndexMethod = strings.TrimSpace(args.IndexMethod)
	args.StorageBackend = strings.TrimSpace(args.StorageBackend)
	args.StorageURI = strings.TrimSpace(args.StorageURI)
	if args.Ref == "" {
		return projectVectorBucketArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectVectorBucketArgs{}, fmt.Errorf("name is required")
	}
	if args.Dimension == 0 {
		args.Dimension = 1536
	}
	if args.Distance == "" {
		args.Distance = "cosine"
	}
	if args.IndexMethod == "" {
		args.IndexMethod = "hnsw"
	}
	if args.StorageBackend == "" {
		args.StorageBackend = "postgres"
	}
	if args.Metadata == nil {
		args.Metadata = map[string]string{}
	}
	return args, nil
}

func decodeProjectAnalyticsBucketArgs(payload json.RawMessage) (projectAnalyticsBucketArgs, error) {
	var args projectAnalyticsBucketArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectAnalyticsBucketArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.StorageURI = strings.TrimSpace(args.StorageURI)
	args.CatalogURI = strings.TrimSpace(args.CatalogURI)
	args.Warehouse = strings.TrimSpace(args.Warehouse)
	args.CredentialHandle = strings.TrimSpace(args.CredentialHandle)
	args.Partitioning = strings.TrimSpace(args.Partitioning)
	args.CompactionSchedule = strings.TrimSpace(args.CompactionSchedule)
	if args.Ref == "" {
		return projectAnalyticsBucketArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectAnalyticsBucketArgs{}, fmt.Errorf("name is required")
	}
	if args.StorageURI == "" {
		return projectAnalyticsBucketArgs{}, fmt.Errorf("storage_uri is required")
	}
	if args.FormatVersion == 0 {
		args.FormatVersion = 2
	}
	if args.CompactionSchedule == "" {
		args.CompactionSchedule = "manual"
	}
	if args.Metadata == nil {
		args.Metadata = map[string]string{}
	}
	return args, nil
}

func decodeProjectFunctionArgs(payload json.RawMessage) (projectFunctionArgs, error) {
	var args projectFunctionArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectFunctionArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.Entrypoint = strings.TrimSpace(args.Entrypoint)
	args.Source = strings.TrimSpace(args.Source)
	if args.Ref == "" {
		return projectFunctionArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectFunctionArgs{}, fmt.Errorf("name is required")
	}
	if args.Entrypoint == "" {
		args.Entrypoint = "index.ts"
	}
	if args.VerifyJWT == nil {
		defaultVerifyJWT := true
		args.VerifyJWT = &defaultVerifyJWT
	}
	if args.Source == "" {
		return projectFunctionArgs{}, fmt.Errorf("source is required")
	}
	if args.Secrets == nil {
		args.Secrets = map[string]string{}
	}
	return args, nil
}

func decodeProjectFunctionNameArgs(payload json.RawMessage) (projectFunctionNameArgs, error) {
	var args projectFunctionNameArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectFunctionNameArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	if args.Ref == "" {
		return projectFunctionNameArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectFunctionNameArgs{}, fmt.Errorf("name is required")
	}
	return args, nil
}

func decodeProjectFunctionRegionArgs(payload json.RawMessage) (projectFunctionRegionArgs, error) {
	var args projectFunctionRegionArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectFunctionRegionArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.FunctionName = strings.TrimSpace(args.FunctionName)
	args.HostID = strings.TrimSpace(args.HostID)
	args.Region = strings.TrimSpace(args.Region)
	args.RoutingPolicy = strings.TrimSpace(args.RoutingPolicy)
	if args.Ref == "" {
		return projectFunctionRegionArgs{}, fmt.Errorf("ref is required")
	}
	if args.FunctionName == "" {
		return projectFunctionRegionArgs{}, fmt.Errorf("function_name is required")
	}
	return args, nil
}

func decodeProjectFunctionRecordArgs(payload json.RawMessage) (projectFunctionRecordArgs, error) {
	var args projectFunctionRecordArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectFunctionRecordArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.ID = strings.TrimSpace(args.ID)
	if args.Ref == "" {
		return projectFunctionRecordArgs{}, fmt.Errorf("ref is required")
	}
	if args.ID == "" {
		return projectFunctionRecordArgs{}, fmt.Errorf("id is required")
	}
	return args, nil
}

func decodeProjectFunctionStorageMountArgs(payload json.RawMessage) (projectFunctionStorageMountArgs, error) {
	var args projectFunctionStorageMountArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectFunctionStorageMountArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.FunctionName = strings.TrimSpace(args.FunctionName)
	args.BucketName = strings.TrimSpace(args.BucketName)
	args.MountPath = strings.TrimSpace(args.MountPath)
	args.Prefix = strings.TrimSpace(args.Prefix)
	args.EnvAlias = strings.TrimSpace(args.EnvAlias)
	if args.Ref == "" {
		return projectFunctionStorageMountArgs{}, fmt.Errorf("ref is required")
	}
	if args.FunctionName == "" {
		return projectFunctionStorageMountArgs{}, fmt.Errorf("function_name is required")
	}
	if args.BucketName == "" {
		return projectFunctionStorageMountArgs{}, fmt.Errorf("bucket_name is required")
	}
	if args.ReadOnly == nil {
		defaultReadOnly := false
		args.ReadOnly = &defaultReadOnly
	}
	return args, nil
}

func decodeProjectReplicationPipelineArgs(payload json.RawMessage) (projectReplicationPipelineArgs, error) {
	var args projectReplicationPipelineArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectReplicationPipelineArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.Type = strings.TrimSpace(args.Type)
	args.SourceSchema = strings.TrimSpace(args.SourceSchema)
	args.SourceTable = strings.TrimSpace(args.SourceTable)
	args.Destination = strings.TrimSpace(args.Destination)
	args.DestinationURI = strings.TrimSpace(args.DestinationURI)
	args.CredentialHandle = strings.TrimSpace(args.CredentialHandle)
	if args.Ref == "" {
		return projectReplicationPipelineArgs{}, fmt.Errorf("ref is required")
	}
	if args.Name == "" {
		return projectReplicationPipelineArgs{}, fmt.Errorf("name is required")
	}
	if args.Type == "" {
		args.Type = "logical"
	}
	if args.SourceSchema == "" {
		args.SourceSchema = "public"
	}
	if args.SourceTable == "" {
		return projectReplicationPipelineArgs{}, fmt.Errorf("source_table is required")
	}
	if args.Destination == "" {
		return projectReplicationPipelineArgs{}, fmt.Errorf("destination is required")
	}
	if args.Config == nil {
		args.Config = map[string]string{}
	}
	return args, nil
}

func decodeProjectEmbeddingJobArgs(payload json.RawMessage) (projectEmbeddingJobArgs, error) {
	var args projectEmbeddingJobArgs
	if err := json.Unmarshal(payload, &args); err != nil {
		return projectEmbeddingJobArgs{}, err
	}
	args.Ref = strings.TrimSpace(args.Ref)
	args.Name = strings.TrimSpace(args.Name)
	args.SourceSchema = strings.TrimSpace(args.SourceSchema)
	args.SourceTable = strings.TrimSpace(args.SourceTable)
	args.SourceColumn = strings.TrimSpace(args.SourceColumn)
	args.PrimaryKeyColumn = strings.TrimSpace(args.PrimaryKeyColumn)
	args.DestinationTable = strings.TrimSpace(args.DestinationTable)
	args.DestinationColumn = strings.TrimSpace(args.DestinationColumn)
	args.Provider = strings.TrimSpace(args.Provider)
	args.Model = strings.TrimSpace(args.Model)
	args.Schedule = strings.TrimSpace(args.Schedule)
	if args.Ref == "" {
		return projectEmbeddingJobArgs{}, fmt.Errorf("ref is required")
	}
	if args.SourceSchema == "" {
		args.SourceSchema = "public"
	}
	if args.SourceTable == "" {
		return projectEmbeddingJobArgs{}, fmt.Errorf("source_table is required")
	}
	if args.SourceColumn == "" {
		return projectEmbeddingJobArgs{}, fmt.Errorf("source_column is required")
	}
	if args.PrimaryKeyColumn == "" {
		args.PrimaryKeyColumn = "id"
	}
	if args.DestinationColumn == "" {
		args.DestinationColumn = "embedding"
	}
	if args.Provider == "" {
		args.Provider = "openai"
	}
	if args.Model == "" {
		args.Model = "text-embedding-3-small"
	}
	if args.Dimension == 0 {
		args.Dimension = 1536
	}
	if args.Schedule == "" {
		args.Schedule = "manual"
	}
	if args.BatchSize == 0 {
		args.BatchSize = 100
	}
	return args, nil
}

func toolJSONResult(payload []byte) map[string]any {
	var parsed any
	if len(payload) > 0 && json.Unmarshal(payload, &parsed) == nil {
		return map[string]any{"content": []map[string]string{{"type": "text", "text": string(mustPrettyJSON(parsed))}}, "structuredContent": parsed}
	}
	return map[string]any{"content": []map[string]string{{"type": "text", "text": string(payload)}}}
}

func mustPrettyJSON(value any) []byte {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return []byte(fmt.Sprint(value))
	}
	return payload
}

type apiClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func (c apiClient) do(ctx context.Context, method string, path string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	limited := io.LimitReader(res.Body, maxManagementAPIBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, 0, err
	}
	if len(payload) > maxManagementAPIBytes {
		return nil, 0, fmt.Errorf("management API response exceeded %d bytes", maxManagementAPIBytes)
	}
	return payload, res.StatusCode, err
}

func normalizeBaseURL(input string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(input), "/")
	if trimmed == "" {
		return "", fmt.Errorf("base URL is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("must include scheme and host")
	}
	return trimmed, nil
}

func readMessage(reader *bufio.Reader) ([]byte, error) {
	line, err := readFirstMessageLine(reader)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.TrimSpace(line), "{") {
		return []byte(line), nil
	}
	contentLength := -1
	for {
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
			contentLength = parsed
		}
		line, err = readLimitedLine(reader, maxMCPHeaderLineBytes, "MCP header")
		if err != nil {
			return nil, err
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	if contentLength > maxMCPMessageBytes {
		return nil, fmt.Errorf("MCP message exceeded %d bytes", maxMCPMessageBytes)
	}
	payload := make([]byte, contentLength)
	_, err = io.ReadFull(reader, payload)
	return payload, err
}

func readFirstMessageLine(reader *bufio.Reader) (string, error) {
	var out []byte
	limit := maxMCPHeaderLineBytes
	label := "MCP header"
	sawNonSpace := false
	for {
		b, err := reader.ReadByte()
		if err != nil {
			if err == io.EOF && len(out) > 0 {
				return strings.TrimRight(string(out), "\r\n"), nil
			}
			return "", err
		}
		if b == '\n' {
			return strings.TrimRight(string(out), "\r\n"), nil
		}
		if !sawNonSpace && !isMCPLineSpace(b) {
			sawNonSpace = true
			if b == '{' {
				limit = maxMCPMessageBytes
				label = "MCP message"
			}
		}
		if len(out) >= limit {
			return "", fmt.Errorf("%s exceeded %d bytes", label, limit)
		}
		out = append(out, b)
	}
}

func readLimitedLine(reader *bufio.Reader, limit int, label string) (string, error) {
	var out []byte
	for {
		b, err := reader.ReadByte()
		if err != nil {
			if err == io.EOF && len(out) > 0 {
				return strings.TrimRight(string(out), "\r\n"), nil
			}
			return "", err
		}
		if b == '\n' {
			return strings.TrimRight(string(out), "\r\n"), nil
		}
		if len(out) >= limit {
			return "", fmt.Errorf("%s exceeded %d bytes", label, limit)
		}
		out = append(out, b)
	}
}

func isMCPLineSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r'
}

func writeMessage(output io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "Content-Length: %d\r\n\r\n%s", len(payload), payload)
	return err
}
