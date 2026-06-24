package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CommandRunner func(ctx context.Context, name string, args []string, env []string, stdout, stderr io.Writer) error

type Runner struct {
	HTTPClient    *http.Client
	CommandRunner CommandRunner
	Stdout        io.Writer
	Stderr        io.Writer
	Env           map[string]string
}

func (r Runner) Run(ctx context.Context, args []string) int {
	if r.Stdout == nil {
		r.Stdout = os.Stdout
	}
	if r.Stderr == nil {
		r.Stderr = os.Stderr
	}
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: r.httpTimeout()}
	}

	global := flag.NewFlagSet("supadupa-cli", flag.ContinueOnError)
	global.SetOutput(r.Stderr)
	apiURL := global.String("api", r.env("SUPADUPA_API_URL", "http://localhost:8080"), "Management API base URL")
	token := global.String("token", r.env("SUPADUPA_TOKEN", ""), "Bearer token")
	if err := global.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			r.printUsage()
			return 0
		}
		return 2
	}
	rest := global.Args()
	if len(rest) == 0 {
		r.printUsage()
		return 2
	}
	if rest[0] == "help" {
		r.printUsage()
		return 0
	}
	base, err := normalizeBaseURL(*apiURL)
	if err != nil {
		fmt.Fprintf(r.Stderr, "invalid --api: %v\n", err)
		return 2
	}
	c := apiClient{baseURL: base, token: *token, client: client}

	if err := r.dispatch(ctx, c, rest); err != nil {
		fmt.Fprintln(r.Stderr, err)
		return 1
	}
	return 0
}

func (r Runner) httpTimeout() time.Duration {
	value := strings.TrimSpace(r.env("SUPADUPA_CLI_HTTP_TIMEOUT", "10m"))
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		return 10 * time.Minute
	}
	return timeout
}

func (r Runner) dispatch(ctx context.Context, c apiClient, args []string) error {
	switch args[0] {
	case "bootstrap":
		return r.auth(ctx, c, "/v1/auth/bootstrap", args[1:])
	case "login":
		return r.auth(ctx, c, "/v1/auth/login", args[1:])
	case "mfa":
		return r.mfa(ctx, c, args[1:])
	case "orgs":
		return r.orgs(ctx, c, args[1:])
	case "members":
		return r.members(ctx, c, args[1:])
	case "teams":
		return r.teams(ctx, c, args[1:])
	case "users":
		return r.users(ctx, c, args[1:])
	case "scim":
		return r.scim(ctx, c, args[1:])
	case "provisioner":
		return r.provisioner(ctx, c, args[1:])
	case "hosts":
		return r.hosts(ctx, c, args[1:])
	case "quotas":
		return r.quotas(ctx, c, args[1:])
	case "usage":
		return r.usage(ctx, c, args[1:])
	case "billing":
		return r.billing(ctx, c, args[1:])
	case "settings":
		return r.settings(ctx, c, args[1:])
	case "backup-targets":
		return r.backupTargets(ctx, c, args[1:])
	case "projects":
		return r.projects(ctx, c, args[1:])
	case "config":
		return r.config(ctx, c, args[1:])
	case "services":
		return r.services(ctx, c, args[1:])
	case "domains":
		return r.domains(ctx, c, args[1:])
	case "routes":
		return r.routes(ctx, c, args[1:])
	case "log-drains":
		return r.logDrains(ctx, c, args[1:])
	case "secrets":
		return r.secrets(ctx, c, args[1:])
	case "backups":
		return r.backups(ctx, c, args[1:])
	case "pitr":
		return r.pitr(ctx, c, args[1:])
	case "branches":
		return r.branches(ctx, c, args[1:])
	case "replicas":
		return r.replicas(ctx, c, args[1:])
	case "functions":
		return r.functions(ctx, c, args[1:])
	case "auth-clients":
		return r.authClients(ctx, c, args[1:])
	case "auth-hooks":
		return r.authHooks(ctx, c, args[1:])
	case "replication":
		return r.replication(ctx, c, args[1:])
	case "embeddings":
		return r.embeddings(ctx, c, args[1:])
	case "database-extensions":
		return r.databaseExtensions(ctx, c, args[1:])
	case "database-cron":
		return r.databaseCron(ctx, c, args[1:])
	case "database-queues":
		return r.databaseQueues(ctx, c, args[1:])
	case "database-webhooks":
		return r.databaseWebhooks(ctx, c, args[1:])
	case "database-schemas":
		return r.databaseSchemas(ctx, c, args[1:])
	case "database-roles":
		return r.databaseRoles(ctx, c, args[1:])
	case "storage-buckets":
		return r.storageBuckets(ctx, c, args[1:])
	case "vector-buckets":
		return r.vectorBuckets(ctx, c, args[1:])
	case "analytics-buckets":
		return r.analyticsBuckets(ctx, c, args[1:])
	case "cdn":
		return r.cdn(ctx, c, args[1:])
	case "network":
		return r.network(ctx, c, args[1:])
	case "network-connections":
		return r.networkConnections(ctx, c, args[1:])
	case "metrics":
		return r.metrics(ctx, c, args[1:])
	case "advisor":
		return r.advisor(ctx, c, args[1:])
	case "compliance":
		return r.compliance(ctx, c, args[1:])
	case "audit":
		return r.audit(ctx, c, args[1:])
	case "logs":
		return r.logs(ctx, c, args[1:])
	case "access":
		return r.access(ctx, c, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (r Runner) settings(ctx context.Context, c apiClient, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("settings requires subcommand: defaults get, defaults set, sso get, sso set")
	}
	switch args[0] {
	case "defaults":
		switch args[1] {
		case "get":
			return r.printResponse(c.do(ctx, http.MethodGet, "/v1/settings/defaults", nil, false))
		case "set":
			fs := newFlagSet("settings defaults set", r.Stderr)
			domain := fs.String("domain", "", "Default base domain")
			stackVersion := fs.String("stack-version", "", "Default stack version")
			profile := fs.String("profile", "", "Default stack profile")
			backupSchedule := fs.String("backup-schedule", "", "Default backup schedule")
			smtpEnabled := fs.Bool("smtp-enabled", false, "Enable platform SMTP defaults")
			smtpHost := fs.String("smtp-host", "", "Platform SMTP host")
			smtpPort := fs.Int("smtp-port", 587, "Platform SMTP port")
			smtpSenderName := fs.String("smtp-sender-name", "", "Platform SMTP sender name")
			smtpSenderEmail := fs.String("smtp-sender-email", "", "Platform SMTP sender email")
			smtpUsername := fs.String("smtp-username", "", "Platform SMTP username")
			smtpPasswordHandle := fs.String("smtp-password-handle", "", "Platform SMTP password secret:// handle")
			smtpTLSMode := fs.String("smtp-tls-mode", "starttls", "Platform SMTP TLS mode: starttls, implicit, or none")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			payload := map[string]any{
				"domain":          *domain,
				"stack_version":   *stackVersion,
				"profile":         *profile,
				"backup_schedule": *backupSchedule,
				"smtp": map[string]any{
					"enabled":         *smtpEnabled,
					"host":            *smtpHost,
					"port":            *smtpPort,
					"sender_name":     *smtpSenderName,
					"sender_email":    *smtpSenderEmail,
					"username":        *smtpUsername,
					"password_handle": *smtpPasswordHandle,
					"tls_mode":        *smtpTLSMode,
				},
			}
			return r.printResponse(c.do(ctx, http.MethodPut, "/v1/settings/defaults", payload, false))
		default:
			return fmt.Errorf("unknown settings defaults subcommand %q", args[1])
		}
	case "sso":
		switch args[1] {
		case "get":
			return r.printResponse(c.do(ctx, http.MethodGet, "/v1/settings/sso", nil, false))
		case "set":
			fs := newFlagSet("settings sso set", r.Stderr)
			enabled := fs.Bool("enabled", false, "Enable platform SAML SSO")
			idpEntityID := fs.String("idp-entity-id", "", "SAML IdP entity ID")
			ssoURL := fs.String("sso-url", "", "SAML IdP login URL")
			certificatePEM := fs.String("certificate-pem", "", "SAML IdP signing certificate PEM")
			certificateFile := fs.String("certificate-file", "", "Path to SAML IdP signing certificate PEM")
			acsURL := fs.String("acs-url", "", "SAML assertion consumer service URL")
			metadataURL := fs.String("metadata-url", "", "SAML metadata URL")
			emailDomain := fs.String("email-domain", "", "Allowed SSO email domain")
			autoProvision := fs.Bool("auto-provision", false, "Create missing platform users from valid SAML assertions")
			defaultRole := fs.String("default-role", "developer", "Default role for auto-provisioned users")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			certificate := *certificatePEM
			if *certificateFile != "" {
				payload, err := os.ReadFile(*certificateFile)
				if err != nil {
					return err
				}
				certificate = string(payload)
			}
			payload := map[string]any{
				"enabled":         *enabled,
				"idp_entity_id":   *idpEntityID,
				"sso_url":         *ssoURL,
				"certificate_pem": certificate,
				"acs_url":         *acsURL,
				"metadata_url":    *metadataURL,
				"email_domain":    *emailDomain,
				"auto_provision":  *autoProvision,
				"default_role":    *defaultRole,
			}
			return r.printResponse(c.do(ctx, http.MethodPut, "/v1/settings/sso", payload, false))
		default:
			return fmt.Errorf("unknown settings sso subcommand %q", args[1])
		}
	default:
		return fmt.Errorf("unknown settings subcommand %q", args[0])
	}
}

type backupStorageTargetCLI struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	Endpoint         string `json:"endpoint"`
	Region           string `json:"region"`
	Bucket           string `json:"bucket"`
	Prefix           string `json:"prefix"`
	AccessKeyID      string `json:"access_key_id"`
	ForcePathStyle   bool   `json:"force_path_style"`
	Default          bool   `json:"default"`
	SecretConfigured bool   `json:"secret_configured"`
}

func (r Runner) backupTargets(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("backup-targets requires subcommand: list, create, update, test, delete")
	}
	switch args[0] {
	case "list":
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/backup-storage-targets", nil, false))
	case "create":
		input, err := r.parseBackupTargetInput("backup-targets create", args[1:], backupStorageTargetCLI{}, true)
		if err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/backup-storage-targets", input, false))
	case "update":
		targetID := strings.TrimSpace(flagArgValue(args[1:], "id"))
		if targetID == "" {
			return fmt.Errorf("--id is required")
		}
		existing, err := fetchBackupTargetForCLI(ctx, c, targetID)
		if err != nil {
			return err
		}
		input, err := r.parseBackupTargetInput("backup-targets update", args[1:], existing, false)
		if err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodPut, "/v1/backup-storage-targets/"+url.PathEscape(targetID), input, false))
	case "test":
		fs := newFlagSet("backup-targets test", r.Stderr)
		id := fs.String("id", "", "Backup storage target ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*id) == "" {
			return fmt.Errorf("--id is required")
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/backup-storage-targets/"+url.PathEscape(*id)+"/test", nil, false))
	case "delete":
		fs := newFlagSet("backup-targets delete", r.Stderr)
		id := fs.String("id", "", "Backup storage target ID")
		yes := fs.Bool("yes", false, "Confirm backup target deletion")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*id) == "" {
			return fmt.Errorf("--id is required")
		}
		if !*yes {
			return fmt.Errorf("backup-targets delete requires --yes")
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/backup-storage-targets/"+url.PathEscape(*id), nil, false))
	default:
		return fmt.Errorf("unknown backup-targets subcommand %q", args[0])
	}
}

func (r Runner) parseBackupTargetInput(command string, args []string, existing backupStorageTargetCLI, creating bool) (map[string]any, error) {
	fs := newFlagSet(command, r.Stderr)
	_ = fs.String("id", "", "Backup storage target ID")
	name := fs.String("name", existing.Name, "Backup storage target name")
	targetType := fs.String("type", valueOrDefault(existing.Type, "s3"), "Backup storage target type")
	endpoint := fs.String("endpoint", existing.Endpoint, "S3-compatible endpoint URL")
	region := fs.String("region", valueOrDefault(existing.Region, "auto"), "S3 region")
	bucket := fs.String("bucket", existing.Bucket, "S3 bucket")
	prefix := fs.String("prefix", existing.Prefix, "Object key prefix")
	accessKeyID := fs.String("access-key-id", existing.AccessKeyID, "S3 access key ID")
	secretAccessKey := fs.String("secret-access-key", "", "S3 secret access key; omitted on update preserves the stored secret")
	forcePathStyle := fs.Bool("force-path-style", existing.ForcePathStyle, "Use S3 path-style addressing")
	defaultTarget := fs.Bool("default", existing.Default, "Mark this as the platform default target")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if creating && strings.TrimSpace(*secretAccessKey) == "" {
		return nil, fmt.Errorf("--secret-access-key is required")
	}
	return map[string]any{
		"name":              *name,
		"type":              *targetType,
		"endpoint":          *endpoint,
		"region":            *region,
		"bucket":            *bucket,
		"prefix":            *prefix,
		"access_key_id":     *accessKeyID,
		"secret_access_key": *secretAccessKey,
		"force_path_style":  *forcePathStyle,
		"default":           *defaultTarget,
	}, nil
}

func fetchBackupTargetForCLI(ctx context.Context, c apiClient, id string) (backupStorageTargetCLI, error) {
	payload, status, err := c.do(ctx, http.MethodGet, "/v1/backup-storage-targets", nil, false)
	if err := apiResponseError(payload, status, err); err != nil {
		return backupStorageTargetCLI{}, err
	}
	var targets []backupStorageTargetCLI
	if err := json.Unmarshal(payload, &targets); err != nil {
		return backupStorageTargetCLI{}, err
	}
	for _, target := range targets {
		if target.ID == id {
			return target, nil
		}
	}
	return backupStorageTargetCLI{}, fmt.Errorf("backup storage target %s was not found", id)
}

func valueOrDefault(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func flagArgValue(args []string, name string) string {
	prefix := "--" + name + "="
	for i, arg := range args {
		if arg == "--"+name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}

func (r Runner) mfa(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("mfa requires subcommand: status, enroll, verify, disable")
	}
	switch args[0] {
	case "status":
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/account/mfa", nil, false))
	case "enroll":
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/account/mfa/enroll", nil, false))
	case "verify", "disable":
		fs := newFlagSet("mfa "+args[0], r.Stderr)
		code := fs.String("code", "", "Six-digit TOTP code")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*code) == "" {
			return fmt.Errorf("--code is required")
		}
		method := http.MethodPost
		path := "/v1/account/mfa/verify"
		if args[0] == "disable" {
			method = http.MethodDelete
			path = "/v1/account/mfa"
		}
		return r.printResponse(c.do(ctx, method, path, map[string]string{"code": strings.TrimSpace(*code)}, false))
	default:
		return fmt.Errorf("unknown mfa subcommand %q", args[0])
	}
}

func (r Runner) scim(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("scim requires subcommand: service-provider-config, users, create-user, replace-user, deprovision-user, delete-user, groups, create-group, delete-group")
	}
	switch args[0] {
	case "service-provider-config":
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/scim/v2/ServiceProviderConfig", nil, false))
	case "users":
		fs := newFlagSet("scim users", r.Stderr)
		id := fs.String("id", "", "SCIM user ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path := "/v1/scim/v2/Users"
		if strings.TrimSpace(*id) != "" {
			path += "/" + url.PathEscape(strings.TrimSpace(*id))
		}
		return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
	case "create-user", "replace-user":
		fs := newFlagSet("scim "+args[0], r.Stderr)
		id := fs.String("id", "", "SCIM user ID for replace-user")
		userName := fs.String("user-name", "", "SCIM userName")
		email := fs.String("email", "", "Primary email; defaults to user-name")
		displayName := fs.String("display-name", "", "Display name")
		password := fs.String("password", "", "Initial or replacement password")
		role := fs.String("role", "developer", "Platform role")
		active := fs.Bool("active", true, "Whether the SCIM user is active")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if args[0] == "replace-user" && strings.TrimSpace(*id) == "" {
			return fmt.Errorf("--id is required")
		}
		payload, err := scimUserPayload(*userName, *email, *displayName, *password, *role, *active)
		if err != nil {
			return err
		}
		path := "/v1/scim/v2/Users"
		method := http.MethodPost
		if args[0] == "replace-user" {
			method = http.MethodPut
			path += "/" + url.PathEscape(strings.TrimSpace(*id))
		}
		return r.printResponse(c.do(ctx, method, path, payload, false))
	case "deprovision-user", "delete-user":
		fs := newFlagSet("scim "+args[0], r.Stderr)
		id := fs.String("id", "", "SCIM user ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*id) == "" {
			return fmt.Errorf("--id is required")
		}
		path := "/v1/scim/v2/Users/" + url.PathEscape(strings.TrimSpace(*id))
		if args[0] == "delete-user" {
			return r.printResponse(c.do(ctx, http.MethodDelete, path, nil, false))
		}
		payload := map[string]any{
			"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
			"Operations": []map[string]any{{
				"op":    "replace",
				"path":  "active",
				"value": false,
			}},
		}
		return r.printResponse(c.do(ctx, http.MethodPatch, path, payload, false))
	case "groups":
		fs := newFlagSet("scim groups", r.Stderr)
		id := fs.String("id", "", "SCIM group ID")
		orgID := fs.String("org-id", "", "Filter SCIM groups by organization ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path := "/v1/scim/v2/Groups"
		if strings.TrimSpace(*id) != "" {
			path += "/" + url.PathEscape(strings.TrimSpace(*id))
		} else if strings.TrimSpace(*orgID) != "" {
			path += "?org_id=" + url.QueryEscape(strings.TrimSpace(*orgID))
		}
		return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
	case "create-group":
		fs := newFlagSet("scim create-group", r.Stderr)
		orgID := fs.String("org-id", "", "Organization ID")
		displayName := fs.String("display-name", "", "Group display name")
		slug := fs.String("slug", "", "Team slug")
		var members stringListFlag
		fs.Var(&members, "member", "Group member email or user ID; repeatable")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload, err := scimGroupPayload(*orgID, *displayName, *slug, members)
		if err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/scim/v2/Groups", payload, false))
	case "delete-group":
		fs := newFlagSet("scim delete-group", r.Stderr)
		id := fs.String("id", "", "SCIM group ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*id) == "" {
			return fmt.Errorf("--id is required")
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/scim/v2/Groups/"+url.PathEscape(strings.TrimSpace(*id)), nil, false))
	default:
		return fmt.Errorf("unknown scim subcommand %q", args[0])
	}
}

func (r Runner) provisioner(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("provisioner requires subcommand: status")
	}
	switch args[0] {
	case "status":
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/provisioner", nil, false))
	default:
		return fmt.Errorf("unknown provisioner subcommand %q", args[0])
	}
}

func scimUserPayload(userName, email, displayName, password, role string, active bool) (map[string]any, error) {
	userName = strings.TrimSpace(userName)
	email = strings.TrimSpace(email)
	if userName == "" {
		userName = email
	}
	if email == "" {
		email = userName
	}
	if userName == "" {
		return nil, fmt.Errorf("--user-name or --email is required")
	}
	return map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:User", "urn:supadupa:params:scim:schemas:extension:User"},
		"userName":    userName,
		"displayName": strings.TrimSpace(displayName),
		"active":      active,
		"emails":      []map[string]any{{"value": email, "primary": true}},
		"password":    strings.TrimSpace(password),
		"urn:supadupa:params:scim:schemas:extension:User": map[string]string{
			"role": strings.TrimSpace(role),
		},
	}, nil
}

func scimGroupPayload(orgID, displayName, slug string, members []string) (map[string]any, error) {
	orgID = strings.TrimSpace(orgID)
	displayName = strings.TrimSpace(displayName)
	slug = strings.TrimSpace(slug)
	if orgID == "" {
		return nil, fmt.Errorf("--org-id is required")
	}
	if displayName == "" {
		return nil, fmt.Errorf("--display-name is required")
	}
	groupMembers := make([]map[string]string, 0, len(members))
	for _, member := range members {
		member = strings.TrimSpace(member)
		if member != "" {
			groupMembers = append(groupMembers, map[string]string{"value": member, "display": member})
		}
	}
	return map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Group", "urn:supadupa:params:scim:schemas:extension:Group"},
		"externalId":  orgID,
		"displayName": displayName,
		"members":     groupMembers,
		"urn:supadupa:params:scim:schemas:extension:Group": map[string]string{
			"org_id": orgID,
			"slug":   slug,
		},
	}, nil
}

func (r Runner) auth(ctx context.Context, c apiClient, path string, args []string) error {
	fs := newFlagSet(path, r.Stderr)
	email := fs.String("email", "", "Email")
	password := fs.String("password", "", "Password")
	totp := fs.String("totp-code", "", "TOTP code")
	if err := fs.Parse(args); err != nil {
		return err
	}
	payload := map[string]string{"email": *email, "password": *password}
	if *totp != "" {
		payload["totp_code"] = *totp
	}
	return r.printResponse(c.do(ctx, http.MethodPost, path, payload, false))
}

func (r Runner) orgs(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("orgs requires subcommand: list, create, get, update, delete")
	}
	switch args[0] {
	case "list":
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/orgs", nil, false))
	case "create":
		fs := newFlagSet("orgs create", r.Stderr)
		name := fs.String("name", "", "Organization name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/orgs", map[string]string{"name": *name}, false))
	case "get", "update", "delete":
		fs := newFlagSet("orgs "+args[0], r.Stderr)
		orgID := fs.String("id", "", "Organization ID")
		name := fs.String("name", "", "Organization name")
		yes := fs.Bool("yes", false, "Confirm destructive action")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path := "/v1/orgs/" + url.PathEscape(*orgID)
		switch args[0] {
		case "get":
			return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
		case "update":
			return r.printResponse(c.do(ctx, http.MethodPut, path, map[string]string{"name": *name}, false))
		case "delete":
			if !*yes {
				return fmt.Errorf("orgs delete requires --yes")
			}
			return r.printResponse(c.do(ctx, http.MethodDelete, path, nil, false))
		}
	default:
		return fmt.Errorf("unknown orgs subcommand %q", args[0])
	}
	return nil
}

func (r Runner) members(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("members requires subcommand: list, upsert, delete")
	}
	fs := newFlagSet("members "+args[0], r.Stderr)
	orgID := fs.String("org-id", "", "Organization ID")
	email := fs.String("email", "", "Member email")
	role := fs.String("role", "developer", "Member role")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	path := "/v1/orgs/" + url.PathEscape(*orgID) + "/members"
	switch args[0] {
	case "list":
		return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
	case "upsert":
		return r.printResponse(c.do(ctx, http.MethodPost, path, map[string]string{"email": *email, "role": *role}, false))
	case "delete":
		return r.printResponse(c.do(ctx, http.MethodDelete, path+"/"+url.PathEscape(*email), nil, false))
	default:
		return fmt.Errorf("unknown members subcommand %q", args[0])
	}
}

func (r Runner) teams(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("teams requires subcommand: list, create, delete, members, add-member, delete-member")
	}
	fs := newFlagSet("teams "+args[0], r.Stderr)
	orgID := fs.String("org-id", "", "Organization ID")
	slug := fs.String("slug", "", "Team slug")
	name := fs.String("name", "", "Team name")
	email := fs.String("email", "", "Member email")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	basePath := "/v1/orgs/" + url.PathEscape(*orgID) + "/teams"
	switch args[0] {
	case "list":
		return r.printResponse(c.do(ctx, http.MethodGet, basePath, nil, false))
	case "create":
		return r.printResponse(c.do(ctx, http.MethodPost, basePath, map[string]string{"name": *name, "slug": *slug}, false))
	case "delete":
		return r.printResponse(c.do(ctx, http.MethodDelete, basePath+"/"+url.PathEscape(*slug), nil, false))
	case "members":
		return r.printResponse(c.do(ctx, http.MethodGet, basePath+"/"+url.PathEscape(*slug)+"/members", nil, false))
	case "add-member":
		return r.printResponse(c.do(ctx, http.MethodPost, basePath+"/"+url.PathEscape(*slug)+"/members", map[string]string{"email": *email}, false))
	case "delete-member":
		return r.printResponse(c.do(ctx, http.MethodDelete, basePath+"/"+url.PathEscape(*slug)+"/members/"+url.PathEscape(*email), nil, false))
	default:
		return fmt.Errorf("unknown teams subcommand %q", args[0])
	}
}

func (r Runner) users(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("users requires subcommand: list, create")
	}
	switch args[0] {
	case "list":
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/users", nil, false))
	case "create":
		fs := newFlagSet("users create", r.Stderr)
		email := fs.String("email", "", "User email")
		password := fs.String("password", "", "Initial password")
		role := fs.String("role", "admin", "Platform role")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]string{
			"email":    *email,
			"password": *password,
			"role":     *role,
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/users", payload, false))
	default:
		return fmt.Errorf("unknown users subcommand %q", args[0])
	}
}

func (r Runner) hosts(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("hosts requires subcommand: list, create, get, delete")
	}
	switch args[0] {
	case "list":
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/hosts", nil, false))
	case "create":
		fs := newFlagSet("hosts create", r.Stderr)
		name := fs.String("name", "", "Host name")
		address := fs.String("address", "", "Host address")
		cpu := fs.Int("cpu", 0, "CPU capacity")
		ramMB := fs.Int("ram-mb", 0, "RAM capacity in MB")
		diskGB := fs.Int("disk-gb", 0, "Disk capacity in GB")
		projects := fs.Int("projects", 0, "Project capacity")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"name":    *name,
			"address": *address,
			"capacity": map[string]int{
				"cpu":      *cpu,
				"ram_mb":   *ramMB,
				"disk_gb":  *diskGB,
				"projects": *projects,
			},
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/hosts", payload, false))
	case "get", "delete":
		fs := newFlagSet("hosts "+args[0], r.Stderr)
		id := fs.String("id", "", "Host ID")
		yes := fs.Bool("yes", false, "Confirm destructive action")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path := "/v1/hosts/" + url.PathEscape(*id)
		switch args[0] {
		case "get":
			return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
		case "delete":
			if !*yes {
				return fmt.Errorf("hosts delete requires --yes")
			}
			return r.printResponse(c.do(ctx, http.MethodDelete, path, nil, false))
		}
	default:
		return fmt.Errorf("unknown hosts subcommand %q", args[0])
	}
	return nil
}

func (r Runner) quotas(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("quotas requires subcommand: get, set")
	}
	fs := newFlagSet("quotas "+args[0], r.Stderr)
	orgID := fs.String("org-id", "", "Organization ID")
	maxProjects := fs.Int("max-projects", 0, "Max projects")
	maxCPU := fs.Int("max-cpu", 0, "Max CPU")
	maxRAMMB := fs.Int("max-ram-mb", 0, "Max RAM in MB")
	maxDiskGB := fs.Int("max-disk-gb", 0, "Max disk in GB")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	path := "/v1/orgs/" + url.PathEscape(*orgID) + "/quotas"
	switch args[0] {
	case "get":
		return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
	case "set":
		payload := map[string]int{
			"max_projects": *maxProjects,
			"max_cpu":      *maxCPU,
			"max_ram_mb":   *maxRAMMB,
			"max_disk_gb":  *maxDiskGB,
		}
		return r.printResponse(c.do(ctx, http.MethodPut, path, payload, false))
	default:
		return fmt.Errorf("unknown quotas subcommand %q", args[0])
	}
}

func (r Runner) usage(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage requires subcommand: current, snapshot, snapshots")
	}
	fs := newFlagSet("usage "+args[0], r.Stderr)
	orgID := fs.String("org-id", "", "Organization ID")
	limit := fs.Int("limit", 50, "Maximum snapshots to return")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(*orgID) == "" {
		return fmt.Errorf("--org-id is required")
	}
	path := "/v1/orgs/" + url.PathEscape(*orgID) + "/usage"
	switch args[0] {
	case "current":
		return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
	case "snapshot":
		return r.printResponse(c.do(ctx, http.MethodPost, path+"/snapshots", nil, false))
	case "snapshots":
		return r.printResponse(c.do(ctx, http.MethodGet, fmt.Sprintf("%s/snapshots?limit=%d", path, *limit), nil, false))
	default:
		return fmt.Errorf("unknown usage subcommand %q", args[0])
	}
}

func (r Runner) billing(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("billing requires subcommand: invoices, create-invoice, get-invoice")
	}
	fs := newFlagSet("billing "+args[0], r.Stderr)
	orgID := fs.String("org-id", "", "Organization ID")
	invoiceID := fs.String("invoice-id", "", "Invoice ID")
	usageSnapshotID := fs.String("usage-snapshot-id", "", "Usage snapshot ID")
	currency := fs.String("currency", "USD", "Three-letter invoice currency")
	status := fs.String("status", "draft", "Invoice status")
	dueDays := fs.Int("due-days", 30, "Days until invoice due")
	limit := fs.Int("limit", 50, "Maximum invoices to return")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(*orgID) == "" {
		return fmt.Errorf("--org-id is required")
	}
	path := "/v1/orgs/" + url.PathEscape(*orgID) + "/billing/invoices"
	switch args[0] {
	case "invoices":
		return r.printResponse(c.do(ctx, http.MethodGet, fmt.Sprintf("%s?limit=%d", path, *limit), nil, false))
	case "create-invoice":
		payload := map[string]any{
			"usage_snapshot_id": *usageSnapshotID,
			"currency":          *currency,
			"status":            *status,
			"due_days":          *dueDays,
		}
		return r.printResponse(c.do(ctx, http.MethodPost, path, payload, false))
	case "get-invoice":
		if strings.TrimSpace(*invoiceID) == "" {
			return fmt.Errorf("--invoice-id is required")
		}
		return r.printResponse(c.do(ctx, http.MethodGet, path+"/"+url.PathEscape(*invoiceID), nil, false))
	default:
		return fmt.Errorf("unknown billing subcommand %q", args[0])
	}
}

func (r Runner) projects(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("projects requires subcommand: list, create, get, connect, cli-profile, db-tunnel, gen-types, activity, pause, resume, restart, upgrade, scale, destroy")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("projects list", r.Stderr)
		orgID := fs.String("org-id", "", "Optional organization ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*orgID) == "" {
			return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects", nil, false))
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/orgs/"+url.PathEscape(*orgID)+"/projects", nil, false))
	case "create":
		fs := newFlagSet("projects create", r.Stderr)
		orgID := fs.String("org-id", "", "Organization ID")
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Project name")
		domain := fs.String("domain", "", "Base domain")
		stackVersion := fs.String("stack-version", "", "Stack version")
		hostID := fs.String("host-id", "", "Host ID")
		profile := fs.String("profile", "", "Stack profile")
		cpu := fs.Int("cpu", 0, "Exact CPU cores (0 = recommended size)")
		ramMB := fs.Int("ram-mb", 0, "Exact RAM in MB (0 = recommended size)")
		diskGB := fs.Int("disk-gb", 0, "Exact disk in GB (0 = recommended size)")
		enforceLimits := fs.Bool("enforce-limits", false, "Apply hard CPU/memory limits across enabled service containers")
		disableService := fs.String("disable-service", "", "Comma-separated services to disable (e.g. analytics,vector,imgproxy)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"ref":           *ref,
			"name":          *name,
			"domain":        *domain,
			"stack_version": *stackVersion,
			"host_id":       *hostID,
			"profile":       *profile,
		}
		if *cpu > 0 {
			payload["cpu"] = *cpu
		}
		if *ramMB > 0 {
			payload["ram_mb"] = *ramMB
		}
		if *diskGB > 0 {
			payload["disk_gb"] = *diskGB
		}
		if *enforceLimits {
			payload["enforce_limits"] = true
		}
		if strings.TrimSpace(*disableService) != "" {
			known := map[string]bool{"auth": true, "rest": true, "graphql": true, "realtime": true, "storage": true, "imgproxy": true, "functions": true, "pooler": true, "studio": true, "analytics": true, "vector": true}
			services := map[string]bool{}
			for name := range known {
				services[name] = true
			}
			for _, entry := range strings.Split(*disableService, ",") {
				name := strings.TrimSpace(entry)
				if name == "" {
					continue
				}
				if !known[name] {
					return fmt.Errorf("unknown service %q (valid: auth, rest, graphql, realtime, storage, imgproxy, functions, pooler, studio, analytics, vector)", name)
				}
				services[name] = false
			}
			payload["services"] = services
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/orgs/"+url.PathEscape(*orgID)+"/projects", payload, false))
	case "get", "connect", "cli-profile", "activity", "pause", "resume", "restart", "destroy":
		fs := newFlagSet("projects "+args[0], r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		format := fs.String("format", "json", "Output format for cli-profile: json, env, or toml")
		apiDomain := fs.String("api-domain", "", "Use a ready custom API domain as SUPABASE_URL for cli-profile env output")
		preferCustomDomain := fs.Bool("prefer-custom-domain", false, "Use the first ready custom API domain as SUPABASE_URL for cli-profile env output")
		yes := fs.Bool("yes", false, "Confirm destructive action")
		retainVolumes := fs.Bool("retain-volumes", false, "Retain data volumes on destroy")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path := "/v1/projects/" + url.PathEscape(*ref)
		method := http.MethodGet
		var body any
		switch args[0] {
		case "connect":
			path += "/connect"
		case "cli-profile":
			path += "/connect/cli"
			payload, status, err := c.do(ctx, http.MethodGet, path, nil, false)
			return r.printCLIProfile(payload, status, err, *format, customAPIURLOptions{Domain: *apiDomain, Prefer: *preferCustomDomain})
		case "activity":
			path += "/activity"
		case "pause", "resume", "restart":
			method = http.MethodPost
			path += "/" + args[0]
		case "destroy":
			if !*yes {
				return fmt.Errorf("projects destroy requires --yes")
			}
			method = http.MethodDelete
			if *retainVolumes {
				path += "?retain_volumes=true"
			}
		}
		return r.printResponse(c.do(ctx, method, path, body, false))
	case "link":
		return r.projectLink(ctx, c, args[1:])
	case "env":
		return r.projectEnv(ctx, c, args[1:])
	case "db-tunnel":
		return r.projectDBTunnel(ctx, c, args[1:])
	case "gen-types":
		return r.projectGenTypes(ctx, c, args[1:])
	case "upgrade":
		fs := newFlagSet("projects upgrade", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		version := fs.String("version", "", "Stack version")
		backupID := fs.String("backup-id", "", "Pre-upgrade backup ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]string{"version": *version}
		if strings.TrimSpace(*backupID) != "" {
			payload["backup_id"] = *backupID
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/upgrade", payload, false))
	case "scale":
		fs := newFlagSet("projects scale", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		cpu := fs.Int("cpu", 0, "CPU cores")
		ramMB := fs.Int("ram-mb", 0, "RAM in MB")
		diskGB := fs.Int("disk-gb", 0, "Disk in GB")
		enforceLimits := fs.Bool("enforce-limits", false, "Apply hard CPU/memory limits across enabled service containers")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/scale", map[string]any{"cpu": *cpu, "ram_mb": *ramMB, "disk_gb": *diskGB, "enforce_limits": *enforceLimits}, false))
	default:
		return fmt.Errorf("unknown projects subcommand %q", args[0])
	}
}

type cliProfileResponse struct {
	ProjectRef          string            `json:"project_ref"`
	ProjectName         string            `json:"project_name"`
	APIURL              string            `json:"api_url"`
	CustomAPIURLs       []string          `json:"custom_api_urls"`
	StudioURL           string            `json:"studio_url"`
	DatabaseURL         string            `json:"database_url"`
	InternalDatabaseURL string            `json:"internal_database_url"`
	PublicDatabaseURL   string            `json:"public_database_url"`
	Env                 map[string]string `json:"env"`
	SupabaseConfigTOML  string            `json:"supabase_config_toml"`
	SecretHandles       map[string]string `json:"secret_handles"`
}

type secretRevealResponse struct {
	Value string `json:"value"`
}

type dbTunnelReady struct {
	ProjectRef        string `json:"project_ref"`
	ListenAddr        string `json:"listen_addr"`
	RemoteHost        string `json:"remote_host"`
	DatabaseURL       string `json:"database_url"`
	DockerDatabaseURL string `json:"docker_database_url,omitempty"`
	Message           string `json:"message"`
}

func (r Runner) projectDBTunnel(ctx context.Context, c apiClient, args []string) error {
	fs := newFlagSet("projects db-tunnel", r.Stderr)
	ref := fs.String("ref", "", "Project ref")
	listen := fs.String("listen", "127.0.0.1:15432", "Local listen address")
	advertiseHost := fs.String("advertise-host", "", "Host to advertise in docker_database_url")
	readyFile := fs.String("ready-file", "", "Write tunnel metadata JSON after listening")
	once := fs.Bool("once", false, "Accept one connection and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	projectRef := strings.TrimSpace(*ref)
	if projectRef == "" {
		return fmt.Errorf("--ref is required")
	}
	profile, err := fetchCLIProfile(ctx, c, projectRef)
	if err != nil {
		return err
	}
	remoteURL := strings.TrimSpace(profile.PublicDatabaseURL)
	if remoteURL == "" {
		remoteURL = strings.TrimSpace(profile.DatabaseURL)
	}
	if remoteURL == "" {
		return fmt.Errorf("project profile did not include a database URL")
	}
	parseableRemoteURL := strings.ReplaceAll(remoteURL, "${DB_PASSWORD}", "db-password-placeholder")
	parseableRemoteURL = strings.ReplaceAll(parseableRemoteURL, "$DB_PASSWORD", "db-password-placeholder")
	remote, err := url.Parse(parseableRemoteURL)
	if err != nil {
		return fmt.Errorf("invalid project database URL: %w", err)
	}
	if remote.Hostname() == "" {
		return fmt.Errorf("project database URL did not include a host")
	}
	remotePort := remote.Port()
	if remotePort == "" {
		remotePort = "5432"
	}
	remoteAddr := net.JoinHostPort(remote.Hostname(), remotePort)
	listener, err := net.Listen("tcp", strings.TrimSpace(*listen))
	if err != nil {
		return err
	}
	defer listener.Close()

	localURL := tunnelDatabaseURL(remote, listener.Addr(), "")
	dockerURL := ""
	if host := strings.TrimSpace(*advertiseHost); host != "" {
		dockerURL = tunnelDatabaseURL(remote, listener.Addr(), host)
	}
	ready := dbTunnelReady{
		ProjectRef:        projectRef,
		ListenAddr:        listener.Addr().String(),
		RemoteHost:        remoteAddr,
		DatabaseURL:       localURL,
		DockerDatabaseURL: dockerURL,
		Message:           "local plaintext Postgres tunnel to public STARTTLS database route",
	}
	readyPayload, err := json.MarshalIndent(ready, "", "  ")
	if err != nil {
		return err
	}
	if path := strings.TrimSpace(*readyFile); path != "" {
		if err := os.WriteFile(path, append(readyPayload, '\n'), 0o600); err != nil {
			return err
		}
	}
	_, _ = r.Stdout.Write(append(readyPayload, '\n'))

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		if *once {
			return proxyPlainPostgresToSTARTTLS(ctx, conn, remoteAddr, remote.Hostname())
		}
		go func() {
			if err := proxyPlainPostgresToSTARTTLS(ctx, conn, remoteAddr, remote.Hostname()); err != nil {
				fmt.Fprintf(r.Stderr, "db tunnel connection failed: %v\n", err)
			}
		}()
	}
}

func tunnelDatabaseURL(remote *url.URL, addr net.Addr, advertisedHost string) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
		port = "5432"
	}
	if advertisedHost == "" {
		advertisedHost = host
	}
	if advertisedHost == "" || advertisedHost == "0.0.0.0" || advertisedHost == "::" {
		advertisedHost = "127.0.0.1"
	}
	username := remote.User.Username()
	if username == "" {
		username = "postgres"
	}
	path := remote.EscapedPath()
	if path == "" {
		path = "/postgres"
	}
	return fmt.Sprintf("postgres://%s:${DB_PASSWORD}@%s%s?sslmode=disable", username, net.JoinHostPort(advertisedHost, port), path)
}

func proxyPlainPostgresToSTARTTLS(ctx context.Context, client net.Conn, remoteAddr string, serverName string) error {
	defer client.Close()
	remote, err := net.Dial("tcp", remoteAddr)
	if err != nil {
		return err
	}
	defer remote.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = client.SetDeadline(deadline)
		_ = remote.SetDeadline(deadline)
	}
	if _, err := remote.Write([]byte{0, 0, 0, 8, 4, 210, 22, 47}); err != nil {
		return err
	}
	response := make([]byte, 1)
	if _, err := io.ReadFull(remote, response); err != nil {
		return err
	}
	if response[0] != 'S' {
		return fmt.Errorf("remote Postgres route rejected STARTTLS")
	}
	tlsRemote := tls.Client(remote, &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12})
	if err := tlsRemote.Handshake(); err != nil {
		return err
	}
	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(tlsRemote, client)
		_ = tlsRemote.CloseWrite()
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(client, tlsRemote)
		errCh <- err
	}()
	select {
	case err := <-errCh:
		if err != nil && !errorsIsClosedConn(err) {
			return err
		}
		return nil
	case <-ctx.Done():
		return nil
	}
}

func errorsIsClosedConn(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "use of closed network connection") || strings.Contains(text, "broken pipe") || strings.Contains(text, "connection reset")
}

func (r Runner) projectLink(ctx context.Context, c apiClient, args []string) error {
	fs := newFlagSet("projects link", r.Stderr)
	ref := fs.String("ref", "", "Project ref")
	dir := fs.String("dir", ".", "Workspace directory")
	revealSecrets := fs.Bool("reveal-secrets", false, "Write real secret values after audited reveal")
	apiDomain := fs.String("api-domain", "", "Use a ready custom API domain as SUPABASE_URL in the workspace env file")
	preferCustomDomain := fs.Bool("prefer-custom-domain", false, "Use the first ready custom API domain as SUPABASE_URL in the workspace env file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	projectRef := strings.TrimSpace(*ref)
	if projectRef == "" {
		return fmt.Errorf("--ref is required")
	}
	profile, err := fetchCLIProfile(ctx, c, projectRef)
	if err != nil {
		return err
	}
	env := profile.Env
	if *revealSecrets {
		env, err = materializedProjectEnv(ctx, c, projectRef, profile)
		if err != nil {
			return err
		}
	}
	env, err = envWithSelectedAPIURL(env, profile.CustomAPIURLs, customAPIURLOptions{Domain: *apiDomain, Prefer: *preferCustomDomain})
	if err != nil {
		return err
	}
	root := strings.TrimSpace(*dir)
	if root == "" {
		root = "."
	}
	bindingDir := filepath.Join(root, ".supadupa")
	if err := os.MkdirAll(bindingDir, 0o700); err != nil {
		return err
	}
	binding := map[string]any{
		"project_ref":      profile.ProjectRef,
		"project_name":     profile.ProjectName,
		"api_url":          profile.APIURL,
		"studio_url":       profile.StudioURL,
		"management_api":   c.baseURL,
		"linked_at":        time.Now().UTC().Format(time.RFC3339),
		"secrets_revealed": *revealSecrets,
	}
	if len(profile.CustomAPIURLs) > 0 {
		binding["custom_api_urls"] = profile.CustomAPIURLs
	}
	if selected := env["SUPABASE_URL"]; selected != "" && selected != profile.APIURL {
		binding["selected_api_url"] = selected
	}
	if binding["project_ref"] == "" {
		binding["project_ref"] = projectRef
	}
	payload, err := json.MarshalIndent(binding, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(filepath.Join(bindingDir, "project.json"), payload, 0o600); err != nil {
		return err
	}
	toml := profile.SupabaseConfigTOML
	if toml == "" || !strings.HasSuffix(toml, "\n") {
		toml += "\n"
	}
	if err := os.WriteFile(filepath.Join(bindingDir, "config.toml"), []byte(toml), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(bindingDir, "supabase.env"), []byte(renderEnvFile(env)), 0o600); err != nil {
		return err
	}
	supabaseConfigPath := filepath.Join(root, "supabase", "config.toml")
	supabaseConfigWritten, err := writeFileIfMissing(supabaseConfigPath, []byte(toml), 0o600)
	if err != nil {
		return err
	}
	if supabaseConfigWritten {
		binding["supabase_config_path"] = supabaseConfigPath
		binding["supabase_config_written"] = true
		updated, err := json.MarshalIndent(binding, "", "  ")
		if err != nil {
			return err
		}
		updated = append(updated, '\n')
		if err := os.WriteFile(filepath.Join(bindingDir, "project.json"), updated, 0o600); err != nil {
			return err
		}
	}
	fmt.Fprintf(r.Stdout, "linked %s in %s\n", projectRef, bindingDir)
	return nil
}

func (r Runner) projectEnv(ctx context.Context, c apiClient, args []string) error {
	fs := newFlagSet("projects env", r.Stderr)
	ref := fs.String("ref", "", "Project ref")
	out := fs.String("out", "", "Write env to file instead of stdout")
	revealSecrets := fs.Bool("reveal-secrets", false, "Print/write real secret values after audited reveal")
	apiDomain := fs.String("api-domain", "", "Use a ready custom API domain as SUPABASE_URL")
	preferCustomDomain := fs.Bool("prefer-custom-domain", false, "Use the first ready custom API domain as SUPABASE_URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	projectRef := strings.TrimSpace(*ref)
	if projectRef == "" {
		return fmt.Errorf("--ref is required")
	}
	profile, err := fetchCLIProfile(ctx, c, projectRef)
	if err != nil {
		return err
	}
	env := profile.Env
	if *revealSecrets {
		env, err = materializedProjectEnv(ctx, c, projectRef, profile)
		if err != nil {
			return err
		}
	}
	env, err = envWithSelectedAPIURL(env, profile.CustomAPIURLs, customAPIURLOptions{Domain: *apiDomain, Prefer: *preferCustomDomain})
	if err != nil {
		return err
	}
	payload := renderEnvFile(env)
	if strings.TrimSpace(*out) != "" {
		return writeFileWithParents(*out, []byte(payload), 0o600)
	}
	_, _ = fmt.Fprint(r.Stdout, payload)
	return nil
}

func (r Runner) projectGenTypes(ctx context.Context, c apiClient, args []string) error {
	fs := newFlagSet("projects gen-types", r.Stderr)
	ref := fs.String("ref", "", "Project ref")
	lang := fs.String("lang", "typescript", "Generated type language; currently typescript")
	out := fs.String("out", "", "Write generated types to this file instead of stdout")
	image := fs.String("image", r.env("SUPADUPA_POSTGRES_META_IMAGE", "public.ecr.aws/supabase/postgres-meta:v0.96.6"), "postgres-meta container image")
	network := fs.String("network", r.env("SUPADUPA_TYPEGEN_DOCKER_NETWORK", "host"), "Docker network for postgres-meta")
	if err := fs.Parse(args); err != nil {
		return err
	}
	projectRef := strings.TrimSpace(*ref)
	if projectRef == "" {
		return fmt.Errorf("--ref is required")
	}
	language := normalizeTypegenLanguage(*lang)
	if language == "" {
		return fmt.Errorf("unsupported gen-types language %q; supported: typescript", *lang)
	}
	imageName := strings.TrimSpace(*image)
	if imageName == "" {
		return fmt.Errorf("--image is required")
	}
	networkName := strings.TrimSpace(*network)
	if networkName == "" {
		return fmt.Errorf("--network is required")
	}

	profile, err := fetchCLIProfile(ctx, c, projectRef)
	if err != nil {
		return err
	}
	dbURLTemplate, err := preferredTypegenDBURL(profile)
	if err != nil {
		return err
	}
	dbURL := dbURLTemplate
	if dbURLHasPasswordPlaceholder(dbURLTemplate) {
		password, err := revealProjectSecret(ctx, c, projectRef, "db_password")
		if err != nil {
			return err
		}
		dbURL, err = materializeDBPassword(dbURLTemplate, password)
		if err != nil {
			return err
		}
	}

	var generated bytes.Buffer
	output := r.Stdout
	if strings.TrimSpace(*out) != "" {
		output = &generated
	}
	runner := r.CommandRunner
	if runner == nil {
		runner = defaultCommandRunner
	}
	dockerArgs := []string{
		"run", "--rm",
		"--network", networkName,
		"-e", "PG_META_GENERATE_TYPES",
		"-e", "PG_META_DB_URL",
		imageName,
		"node", "dist/server/server.js",
	}
	env := []string{
		"PG_META_GENERATE_TYPES=" + language,
		"PG_META_DB_URL=" + dbURL,
	}
	if err := runner(ctx, "docker", dockerArgs, env, output, r.Stderr); err != nil {
		return fmt.Errorf("postgres-meta type generation failed: %w", err)
	}
	if strings.TrimSpace(*out) != "" {
		if err := writeFileWithParents(*out, generated.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func writeFileWithParents(path string, payload []byte, perm os.FileMode) error {
	cleaned := filepath.Clean(path)
	if dir := filepath.Dir(cleaned); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return os.WriteFile(cleaned, payload, perm)
}

func writeFileIfMissing(path string, payload []byte, perm os.FileMode) (bool, error) {
	cleaned := filepath.Clean(path)
	if _, err := os.Stat(cleaned); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := writeFileWithParents(cleaned, payload, perm); err != nil {
		return false, err
	}
	return true, nil
}

func fetchCLIProfile(ctx context.Context, c apiClient, ref string) (cliProfileResponse, error) {
	payload, status, err := c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(ref)+"/connect/cli", nil, false)
	if err := apiResponseError(payload, status, err); err != nil {
		return cliProfileResponse{}, err
	}
	var profile cliProfileResponse
	if err := json.Unmarshal(payload, &profile); err != nil {
		return cliProfileResponse{}, err
	}
	return profile, nil
}

func revealProjectSecret(ctx context.Context, c apiClient, ref, kind string) (string, error) {
	path := "/v1/projects/" + url.PathEscape(ref) + "/secrets/" + url.PathEscape(kind) + "/reveal"
	payload, status, err := c.do(ctx, http.MethodGet, path, nil, false)
	if err := apiResponseError(payload, status, err); err != nil {
		return "", err
	}
	var secret secretRevealResponse
	if err := json.Unmarshal(payload, &secret); err != nil {
		return "", err
	}
	if secret.Value == "" {
		return "", fmt.Errorf("%s secret reveal returned an empty value", kind)
	}
	return secret.Value, nil
}

func materializedProjectEnv(ctx context.Context, c apiClient, ref string, profile cliProfileResponse) (map[string]string, error) {
	env := map[string]string{}
	for key, value := range profile.Env {
		env[key] = value
	}
	kindsByEnv := []struct {
		key  string
		kind string
	}{
		{key: "SUPABASE_ANON_KEY", kind: "anon_key"},
		{key: "SUPABASE_SERVICE_ROLE_KEY", kind: "service_role"},
		{key: "SUPABASE_DB_PASSWORD", kind: "db_password"},
		{key: "SUPABASE_S3_ACCESS_KEY", kind: "s3_access_key"},
		{key: "SUPABASE_S3_SECRET_KEY", kind: "s3_secret_key"},
	}
	revealed := map[string]string{}
	revealKind := func(kind string) (string, error) {
		if value, ok := revealed[kind]; ok {
			return value, nil
		}
		value, err := revealProjectSecret(ctx, c, ref, kind)
		if err != nil {
			return "", err
		}
		revealed[kind] = value
		return value, nil
	}
	for _, item := range kindsByEnv {
		value := strings.TrimSpace(env[item.key])
		if value == "" || !strings.HasPrefix(value, "secret://") {
			continue
		}
		secret, err := revealKind(item.kind)
		if err != nil {
			return nil, err
		}
		env[item.key] = secret
	}
	if dbPassword, ok := revealed["db_password"]; ok {
		for _, key := range []string{"SUPABASE_DB_URL", "DATABASE_URL", "SUPADUPA_INTERNAL_DB_URL"} {
			if value := env[key]; dbURLHasPasswordPlaceholder(value) {
				materialized, err := materializeDBPassword(value, dbPassword)
				if err != nil {
					return nil, err
				}
				env[key] = materialized
			}
		}
	}
	return env, nil
}

func preferredTypegenDBURL(profile cliProfileResponse) (string, error) {
	for _, candidate := range []string{
		profile.PublicDatabaseURL,
		profile.DatabaseURL,
		profile.Env["DATABASE_URL"],
		profile.Env["SUPABASE_DB_URL"],
		profile.InternalDatabaseURL,
	} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate), nil
		}
	}
	return "", fmt.Errorf("cli profile did not include a database URL")
}

func normalizeTypegenLanguage(input string) string {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "", "typescript", "ts":
		return "typescript"
	default:
		return ""
	}
}

func dbURLHasPasswordPlaceholder(input string) bool {
	return strings.Contains(input, "${DB_PASSWORD}") || strings.Contains(input, "$DB_PASSWORD")
}

func materializeDBPassword(input, password string) (string, error) {
	if !dbURLHasPasswordPlaceholder(input) {
		return input, nil
	}
	parseable := strings.ReplaceAll(input, "${DB_PASSWORD}", "db-password-placeholder")
	parseable = strings.ReplaceAll(parseable, "$DB_PASSWORD", "db-password-placeholder")
	parsed, err := url.Parse(parseable)
	if err == nil && parsed.User != nil {
		username := parsed.User.Username()
		if username != "" {
			parsed.User = url.UserPassword(username, password)
			return parsed.String(), nil
		}
	}
	if strings.Contains(input, "${DB_PASSWORD}") {
		return strings.ReplaceAll(input, "${DB_PASSWORD}", url.PathEscape(password)), nil
	}
	return strings.ReplaceAll(input, "$DB_PASSWORD", url.PathEscape(password)), nil
}

func apiResponseError(payload []byte, status int, err error) error {
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("api returned %d: %s", status, strings.TrimSpace(string(payload)))
	}
	return nil
}

func defaultCommandRunner(ctx context.Context, name string, args []string, env []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

type customAPIURLOptions struct {
	Domain string
	Prefer bool
}

func (r Runner) printCLIProfile(payload []byte, status int, err error, format string, apiOptions customAPIURLOptions) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" || format == "json" {
		return r.printResponse(payload, status, err)
	}
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("api returned %d: %s", status, strings.TrimSpace(string(payload)))
	}
	var profile cliProfileResponse
	if err := json.Unmarshal(payload, &profile); err != nil {
		return err
	}
	switch format {
	case "env":
		env, err := envWithSelectedAPIURL(profile.Env, profile.CustomAPIURLs, apiOptions)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprint(r.Stdout, renderEnvFile(env))
		return nil
	case "toml":
		_, _ = fmt.Fprint(r.Stdout, profile.SupabaseConfigTOML)
		if profile.SupabaseConfigTOML == "" || !strings.HasSuffix(profile.SupabaseConfigTOML, "\n") {
			fmt.Fprintln(r.Stdout)
		}
		return nil
	default:
		return fmt.Errorf("unsupported cli-profile format %q", format)
	}
}

func envWithSelectedAPIURL(env map[string]string, customAPIURLs []string, options customAPIURLOptions) (map[string]string, error) {
	out := make(map[string]string, len(env)+1)
	for key, value := range env {
		out[key] = value
	}
	selected, err := selectCustomAPIURL(customAPIURLs, options)
	if err != nil {
		return nil, err
	}
	if selected != "" {
		out["SUPABASE_URL"] = selected
		out["SUPADUPA_SELECTED_API_URL"] = selected
	}
	return out, nil
}

func selectCustomAPIURL(customAPIURLs []string, options customAPIURLOptions) (string, error) {
	domain := strings.TrimSpace(options.Domain)
	if domain == "" && !options.Prefer {
		return "", nil
	}
	ready := make([]string, 0, len(customAPIURLs))
	for _, apiURL := range customAPIURLs {
		normalized := normalizeCustomAPIURL(apiURL)
		if normalized != "" {
			ready = append(ready, normalized)
		}
	}
	if len(ready) == 0 {
		return "", fmt.Errorf("no ready custom API domains are available for this project")
	}
	if domain == "" {
		return ready[0], nil
	}
	want := normalizeCustomAPIURL(domain)
	for _, apiURL := range ready {
		if apiURL == want || hostFromURL(apiURL) == strings.Trim(strings.ToLower(domain), ".") {
			return apiURL, nil
		}
	}
	return "", fmt.Errorf("custom API domain %q is not ready for this project; available: %s", domain, strings.Join(ready, ", "))
}

func normalizeCustomAPIURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "https://" + strings.Trim(value, ".")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return ""
	}
	parsed.Scheme = "https"
	parsed.Host = strings.Trim(strings.ToLower(parsed.Host), ".")
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func hostFromURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func renderEnvFile(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&out, "%s=%s\n", key, shellEnvValue(env[key]))
	}
	return out.String()
}

func shellEnvValue(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (r Runner) access(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("access requires subcommand: list, grant, revoke, review")
	}
	fs := newFlagSet("access "+args[0], r.Stderr)
	ref := fs.String("ref", "", "Project ref")
	orgID := fs.String("org-id", "", "Organization ID")
	subjectType := fs.String("subject-type", "team", "Subject type: user or team")
	subjectID := fs.String("subject-id", "", "Subject ID, email, or team slug")
	role := fs.String("role", "viewer", "Project role")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	path := "/v1/projects/" + url.PathEscape(*ref) + "/access"
	switch args[0] {
	case "list":
		return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
	case "review":
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/orgs/"+url.PathEscape(*orgID)+"/access-review", nil, false))
	case "grant":
		payload := map[string]string{"subject_type": *subjectType, "subject_id": *subjectID, "role": *role}
		return r.printResponse(c.do(ctx, http.MethodPut, path, payload, false))
	case "revoke":
		return r.printResponse(c.do(ctx, http.MethodDelete, path+"/"+url.PathEscape(*subjectType)+"/"+url.PathEscape(*subjectID), nil, false))
	default:
		return fmt.Errorf("unknown access subcommand %q", args[0])
	}
}

func (r Runner) config(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("config requires subcommand: get, set")
	}
	fs := newFlagSet("config "+args[0], r.Stderr)
	ref := fs.String("ref", "", "Project ref")
	area := fs.String("area", "", "Config area")
	var values stringListFlag
	fs.Var(&values, "set", "Config key=value; repeatable or comma-separated")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	path := "/v1/projects/" + url.PathEscape(*ref) + "/config/" + url.PathEscape(*area)
	switch args[0] {
	case "get":
		return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
	case "set":
		return r.printResponse(c.do(ctx, http.MethodPut, path, map[string]any{"config": parseKeyValues(values)}, false))
	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

func (r Runner) services(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("services requires subcommand: list, set")
	}
	fs := newFlagSet("services "+args[0], r.Stderr)
	ref := fs.String("ref", "", "Project ref")
	var values stringListFlag
	fs.Var(&values, "service", "Service enabled state name=true|false; repeatable or comma-separated")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	path := "/v1/projects/" + url.PathEscape(*ref) + "/services"
	switch args[0] {
	case "list":
		return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
	case "set":
		services, err := parseBoolKeyValues(values)
		if err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodPut, path, map[string]any{"services": services}, false))
	default:
		return fmt.Errorf("unknown services subcommand %q", args[0])
	}
}

func (r Runner) domains(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("domains requires subcommand: list, add, delete, upload-certificate, reset-certificate")
	}
	fs := newFlagSet("domains "+args[0], r.Stderr)
	ref := fs.String("ref", "", "Project ref")
	fqdn := fs.String("fqdn", "", "Domain FQDN")
	certFile := fs.String("cert-file", "", "Certificate PEM file")
	keyFile := fs.String("key-file", "", "Private key PEM file")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	path := "/v1/projects/" + url.PathEscape(*ref) + "/domains"
	switch args[0] {
	case "list":
		return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
	case "add":
		return r.printResponse(c.do(ctx, http.MethodPost, path, map[string]string{"fqdn": *fqdn}, false))
	case "delete":
		return r.printResponse(c.do(ctx, http.MethodDelete, path+"/"+url.PathEscape(*fqdn), nil, false))
	case "upload-certificate":
		certificatePEM, err := os.ReadFile(*certFile)
		if err != nil {
			return err
		}
		privateKeyPEM, err := os.ReadFile(*keyFile)
		if err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodPut, path+"/"+url.PathEscape(*fqdn)+"/certificate", map[string]string{
			"certificate_pem": string(certificatePEM),
			"private_key_pem": string(privateKeyPEM),
		}, false))
	case "reset-certificate":
		return r.printResponse(c.do(ctx, http.MethodDelete, path+"/"+url.PathEscape(*fqdn)+"/certificate", nil, false))
	default:
		return fmt.Errorf("unknown domains subcommand %q", args[0])
	}
}

func (r Runner) routes(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("routes requires subcommand: list")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("routes list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/routes", nil, false))
	default:
		return fmt.Errorf("unknown routes subcommand %q", args[0])
	}
}

func (r Runner) logDrains(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("log-drains requires subcommand: list, create, delete")
	}
	fs := newFlagSet("log-drains "+args[0], r.Stderr)
	ref := fs.String("ref", "", "Project ref")
	id := fs.String("id", "", "Log drain ID")
	target := fs.String("target", "", "Log drain target")
	var values stringListFlag
	fs.Var(&values, "config", "Drain config key=value; repeatable or comma-separated")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	path := "/v1/projects/" + url.PathEscape(*ref) + "/log-drains"
	switch args[0] {
	case "list":
		return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
	case "create":
		payload := map[string]any{"target": *target, "config": parseKeyValues(values)}
		return r.printResponse(c.do(ctx, http.MethodPost, path, payload, false))
	case "delete":
		return r.printResponse(c.do(ctx, http.MethodDelete, path+"/"+url.PathEscape(*id), nil, false))
	default:
		return fmt.Errorf("unknown log-drains subcommand %q", args[0])
	}
}

func (r Runner) secrets(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("secrets requires subcommand: list, reveal, copy, rotate")
	}
	fs := newFlagSet("secrets "+args[0], r.Stderr)
	ref := fs.String("ref", "", "Project ref")
	kind := fs.String("kind", "", "Secret kind")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	path := "/v1/projects/" + url.PathEscape(*ref) + "/secrets"
	switch args[0] {
	case "list":
		return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
	case "reveal":
		return r.printResponse(c.do(ctx, http.MethodGet, path+"/"+url.PathEscape(*kind)+"/reveal", nil, false))
	case "copy":
		return r.printResponse(c.do(ctx, http.MethodPost, path+"/"+url.PathEscape(*kind)+"/copy", nil, false))
	case "rotate":
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/keys/rotate", map[string]string{"kind": *kind}, false))
	default:
		return fmt.Errorf("unknown secrets subcommand %q", args[0])
	}
}

func (r Runner) replicas(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("replicas requires subcommand: list, routing, create, promote, failover, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("replicas list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/replicas", nil, false))
	case "routing":
		fs := newFlagSet("replicas routing", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/replicas/routing", nil, false))
	case "create":
		fs := newFlagSet("replicas create", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Replica name")
		hostID := fs.String("host-id", "", "Host ID")
		region := fs.String("region", "", "Region label")
		tier := fs.String("tier", "small", "Resource tier")
		readWeight := fs.Int("read-weight", 100, "Read routing weight")
		failoverPriority := fs.Int("failover-priority", 1, "Failover priority, lower wins")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"name":              *name,
			"host_id":           *hostID,
			"region":            *region,
			"tier":              *tier,
			"read_weight":       *readWeight,
			"failover_priority": *failoverPriority,
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/replicas", payload, false))
	case "promote":
		fs := newFlagSet("replicas promote", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		id := fs.String("id", "", "Replica ID")
		reason := fs.String("reason", "manual promotion", "Promotion reason")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/replicas/"+url.PathEscape(*id)+"/promote", map[string]string{"reason": *reason}, false))
	case "failover":
		fs := newFlagSet("replicas failover", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		reason := fs.String("reason", "automatic failover", "Failover reason")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/replicas/failover", map[string]string{"reason": *reason}, false))
	case "delete":
		fs := newFlagSet("replicas delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		id := fs.String("id", "", "Replica ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/replicas/"+url.PathEscape(*id), nil, false))
	default:
		return fmt.Errorf("unknown replicas subcommand %q", args[0])
	}
}

func (r Runner) branches(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("branches requires subcommand: list, create, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("branches list", r.Stderr)
		ref := fs.String("ref", "", "Source project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/branches", nil, false))
	case "create":
		fs := newFlagSet("branches create", r.Stderr)
		ref := fs.String("ref", "", "Source project ref")
		branchRef := fs.String("branch-ref", "", "Branch project ref")
		name := fs.String("name", "", "Branch project name")
		ttlHours := fs.Int("ttl-hours", 0, "Optional branch TTL in hours")
		withData := fs.Bool("with-data", false, "Clone source project data into the branch")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"ref":       *branchRef,
			"name":      *name,
			"ttl_hours": *ttlHours,
			"with_data": *withData,
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/branches", payload, false))
	case "delete":
		fs := newFlagSet("branches delete", r.Stderr)
		ref := fs.String("ref", "", "Source project ref")
		branchRef := fs.String("branch-ref", "", "Branch project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/branches/"+url.PathEscape(*branchRef), nil, false))
	default:
		return fmt.Errorf("unknown branches subcommand %q", args[0])
	}
}

func (r Runner) backups(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("backups requires subcommand: list, trigger, restore, policy, set-policy")
	}
	fs := newFlagSet("backups "+args[0], r.Stderr)
	ref := fs.String("ref", "", "Project ref")
	switch args[0] {
	case "list", "trigger", "policy":
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path := "/v1/projects/" + url.PathEscape(*ref) + "/backups"
		method := http.MethodGet
		if args[0] == "trigger" {
			method = http.MethodPost
		}
		if args[0] == "policy" {
			path += "/policy"
		}
		return r.printResponse(c.do(ctx, method, path, nil, false))
	case "restore":
		backupID := fs.String("backup-id", "", "Backup ID")
		confirmation := fs.String("confirmation", "", "Exact restore confirmation, for example: restore project <ref>")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*confirmation) == "" {
			return fmt.Errorf("confirmation is required")
		}
		payload := map[string]string{"backup_id": *backupID, "confirmation": strings.TrimSpace(*confirmation)}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/restore", payload, false))
	case "set-policy":
		enabled := fs.Bool("enabled", false, "Enable scheduled backups")
		schedule := fs.String("schedule", "daily", "Backup schedule")
		kind := fs.String("kind", "logical", "Backup kind")
		storageTargetID := fs.String("storage-target-id", "", "Backup storage target ID; omit to use platform default")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"enabled":           *enabled,
			"schedule":          *schedule,
			"kind":              *kind,
			"storage_target_id": *storageTargetID,
		}
		path := "/v1/projects/" + url.PathEscape(*ref) + "/backups/policy"
		return r.printResponse(c.do(ctx, http.MethodPut, path, payload, false))
	default:
		return fmt.Errorf("unknown backups subcommand %q", args[0])
	}
}

func (r Runner) pitr(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("pitr requires subcommand: policy, set-policy, wal-list, archive")
	}
	fs := newFlagSet("pitr "+args[0], r.Stderr)
	ref := fs.String("ref", "", "Project ref")
	if args[0] == "set-policy" {
		enabled := fs.Bool("enabled", false, "Enable PITR")
		archiveBucket := fs.String("archive-bucket", "", "WAL archive bucket")
		retentionDays := fs.Int("retention-days", 7, "Retention window in days")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"enabled":        *enabled,
			"archive_bucket": *archiveBucket,
			"retention_days": *retentionDays,
		}
		return r.printResponse(c.do(ctx, http.MethodPut, "/v1/projects/"+url.PathEscape(*ref)+"/pitr/policy", payload, false))
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	path := "/v1/projects/" + url.PathEscape(*ref) + "/pitr"
	switch args[0] {
	case "policy":
		return r.printResponse(c.do(ctx, http.MethodGet, path+"/policy", nil, false))
	case "wal-list":
		return r.printResponse(c.do(ctx, http.MethodGet, path+"/wal", nil, false))
	case "archive":
		return r.printResponse(c.do(ctx, http.MethodPost, path+"/wal", nil, false))
	default:
		return fmt.Errorf("unknown pitr subcommand %q", args[0])
	}
}

func (r Runner) functions(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("functions requires subcommand: list, deploy, delete, regions, region, unregion, mounts, mount, unmount")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("functions list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/functions", nil, false))
	case "deploy":
		fs := newFlagSet("functions deploy", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Function name")
		entrypoint := fs.String("entrypoint", "index.ts", "Entrypoint")
		source := fs.String("source", "", "Source text")
		sourceFile := fs.String("source-file", "", "Path to function source file")
		var secrets stringListFlag
		fs.Var(&secrets, "secret", "Function secret as KEY=value; repeatable or comma-separated")
		verifyJWT := fs.Bool("verify-jwt", true, "Verify JWT")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		sourceText := *source
		if *sourceFile != "" {
			payload, err := os.ReadFile(filepath.Clean(*sourceFile))
			if err != nil {
				return err
			}
			sourceText = string(payload)
		}
		payload := map[string]any{
			"name":       *name,
			"entrypoint": *entrypoint,
			"verify_jwt": *verifyJWT,
			"source":     sourceText,
			"secrets":    parseKeyValues(secrets),
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/functions", payload, false))
	case "delete":
		fs := newFlagSet("functions delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Function name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/functions/"+url.PathEscape(*name), nil, false))
	case "regions":
		fs := newFlagSet("functions regions", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/functions/regions", nil, false))
	case "region":
		fs := newFlagSet("functions region", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		functionName := fs.String("function", "", "Function name")
		hostID := fs.String("host-id", "", "Host ID")
		region := fs.String("region", "local", "Region label")
		routingPolicy := fs.String("routing-policy", "nearest", "Routing policy: nearest, primary, or weighted")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"function_name":  *functionName,
			"host_id":        *hostID,
			"region":         *region,
			"routing_policy": *routingPolicy,
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/functions/regions", payload, false))
	case "unregion":
		fs := newFlagSet("functions unregion", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		id := fs.String("id", "", "Function region id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/functions/regions/"+url.PathEscape(*id), nil, false))
	case "mounts":
		fs := newFlagSet("functions mounts", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/functions/storage-mounts", nil, false))
	case "mount":
		fs := newFlagSet("functions mount", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		functionName := fs.String("function", "", "Function name")
		bucketName := fs.String("bucket", "", "Storage bucket name")
		mountPath := fs.String("mount-path", "", "Mount path under /mnt")
		prefix := fs.String("prefix", "", "Bucket prefix to mount")
		envAlias := fs.String("env-alias", "", "Environment variable alias for mount path")
		readOnly := fs.Bool("read-only", true, "Mount read-only")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"function_name": *functionName,
			"bucket_name":   *bucketName,
			"mount_path":    *mountPath,
			"read_only":     *readOnly,
			"prefix":        *prefix,
			"env_alias":     *envAlias,
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/functions/storage-mounts", payload, false))
	case "unmount":
		fs := newFlagSet("functions unmount", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		id := fs.String("id", "", "Storage mount id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/functions/storage-mounts/"+url.PathEscape(*id), nil, false))
	default:
		return fmt.Errorf("unknown functions subcommand %q", args[0])
	}
}

func (r Runner) authClients(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("auth-clients requires subcommand: list, create, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("auth-clients list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/auth/clients", nil, false))
	case "create":
		fs := newFlagSet("auth-clients create", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Client display name")
		clientID := fs.String("client-id", "", "OAuth client id; generated if omitted")
		secretHandle := fs.String("client-secret-handle", "", "Secret handle for confidential clients")
		confidential := fs.Bool("confidential", true, "Whether the client is confidential")
		var redirectURIs stringListFlag
		var grantTypes stringListFlag
		var scopes stringListFlag
		fs.Var(&redirectURIs, "redirect-uri", "Redirect URI; repeatable or comma-separated")
		fs.Var(&grantTypes, "grant-type", "OAuth grant type; repeatable or comma-separated")
		fs.Var(&scopes, "scope", "OAuth scope; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"name":                 *name,
			"client_id":            *clientID,
			"client_secret_handle": *secretHandle,
			"redirect_uris":        parseListValues(redirectURIs),
			"grant_types":          parseListValues(grantTypes),
			"scopes":               parseListValues(scopes),
			"confidential":         *confidential,
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/auth/clients", payload, false))
	case "delete":
		fs := newFlagSet("auth-clients delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		clientID := fs.String("client-id", "", "OAuth client id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/auth/clients/"+url.PathEscape(*clientID), nil, false))
	default:
		return fmt.Errorf("unknown auth-clients subcommand %q", args[0])
	}
}

func (r Runner) authHooks(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("auth-hooks requires subcommand: list, set, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("auth-hooks list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/auth/hooks", nil, false))
	case "set":
		fs := newFlagSet("auth-hooks set", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		hookType := fs.String("hook-type", "", "Auth hook type")
		enabled := fs.Bool("enabled", true, "Whether the hook is enabled")
		targetURI := fs.String("target-uri", "", "HTTPS hook endpoint")
		edgeFunction := fs.String("edge-function", "", "Edge Function target")
		secretHandle := fs.String("secret-handle", "", "Secret handle for hook signing or auth")
		timeoutMS := fs.Int("timeout-ms", 5000, "Hook timeout in milliseconds")
		retryAttempts := fs.Int("retry-attempts", 0, "Retry attempts")
		var headers stringListFlag
		fs.Var(&headers, "header", "Header as Key=value; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"hook_type":      *hookType,
			"enabled":        *enabled,
			"target_uri":     *targetURI,
			"edge_function":  *edgeFunction,
			"secret_handle":  *secretHandle,
			"headers":        parseKeyValues(headers),
			"timeout_ms":     *timeoutMS,
			"retry_attempts": *retryAttempts,
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/auth/hooks", payload, false))
	case "delete":
		fs := newFlagSet("auth-hooks delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		hookType := fs.String("hook-type", "", "Auth hook type")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/auth/hooks/"+url.PathEscape(*hookType), nil, false))
	default:
		return fmt.Errorf("unknown auth-hooks subcommand %q", args[0])
	}
}

func (r Runner) replication(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("replication requires subcommand: list, create, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("replication list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/replication", nil, false))
	case "create":
		fs := newFlagSet("replication create", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Pipeline name")
		pipelineType := fs.String("type", "logical", "Pipeline type: logical, etl, analytics_bucket")
		sourceSchema := fs.String("source-schema", "public", "Source schema")
		sourceTable := fs.String("source-table", "", "Source table")
		destination := fs.String("destination", "", "Destination type")
		destinationURI := fs.String("destination-uri", "", "Destination URI")
		credentialHandle := fs.String("credential-handle", "", "secret:// credential handle")
		var config stringListFlag
		fs.Var(&config, "config", "Destination config key=value; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"name":              *name,
			"type":              *pipelineType,
			"source_schema":     *sourceSchema,
			"source_table":      *sourceTable,
			"destination":       *destination,
			"destination_uri":   *destinationURI,
			"credential_handle": *credentialHandle,
			"config":            parseKeyValues(config),
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/replication", payload, false))
	case "delete":
		fs := newFlagSet("replication delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		id := fs.String("id", "", "Pipeline ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/replication/"+url.PathEscape(*id), nil, false))
	default:
		return fmt.Errorf("unknown replication subcommand %q", args[0])
	}
}

func (r Runner) embeddings(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("embeddings requires subcommand: list, create, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("embeddings list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/embeddings", nil, false))
	case "create":
		fs := newFlagSet("embeddings create", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Embedding job name")
		sourceSchema := fs.String("source-schema", "public", "Source schema")
		sourceTable := fs.String("source-table", "", "Source table")
		sourceColumn := fs.String("source-column", "", "Source text column")
		primaryKeyColumn := fs.String("primary-key-column", "id", "Primary key column")
		destinationTable := fs.String("destination-table", "", "Destination table")
		destinationColumn := fs.String("destination-column", "embedding", "Destination vector column")
		provider := fs.String("provider", "openai", "Embedding provider")
		model := fs.String("model", "text-embedding-3-small", "Embedding model")
		dimension := fs.Int("dimension", 1536, "Embedding dimension")
		schedule := fs.String("schedule", "manual", "Schedule or manual")
		batchSize := fs.Int("batch-size", 100, "Batch size")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"name":               *name,
			"source_schema":      *sourceSchema,
			"source_table":       *sourceTable,
			"source_column":      *sourceColumn,
			"primary_key_column": *primaryKeyColumn,
			"destination_table":  *destinationTable,
			"destination_column": *destinationColumn,
			"provider":           *provider,
			"model":              *model,
			"dimension":          *dimension,
			"schedule":           *schedule,
			"batch_size":         *batchSize,
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/embeddings", payload, false))
	case "delete":
		fs := newFlagSet("embeddings delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		id := fs.String("id", "", "Embedding job ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/embeddings/"+url.PathEscape(*id), nil, false))
	default:
		return fmt.Errorf("unknown embeddings subcommand %q", args[0])
	}
}

func (r Runner) vectorBuckets(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("vector-buckets requires subcommand: list, create, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("vector-buckets list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/vector-buckets", nil, false))
	case "create":
		fs := newFlagSet("vector-buckets create", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Vector bucket name")
		dimension := fs.Int("dimension", 1536, "Vector dimension")
		distance := fs.String("distance", "cosine", "Distance: cosine, l2, ip")
		indexMethod := fs.String("index-method", "hnsw", "Index method: none, hnsw, ivfflat")
		storageBackend := fs.String("storage-backend", "postgres", "Storage backend: postgres, s3")
		storageURI := fs.String("storage-uri", "", "Storage URI for S3-backed buckets")
		var metadata stringListFlag
		fs.Var(&metadata, "metadata", "Metadata key=value; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"name":            *name,
			"dimension":       *dimension,
			"distance":        *distance,
			"index_method":    *indexMethod,
			"storage_backend": *storageBackend,
			"storage_uri":     *storageURI,
			"metadata":        parseKeyValues(metadata),
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/vector-buckets", payload, false))
	case "delete":
		fs := newFlagSet("vector-buckets delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Vector bucket name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/vector-buckets/"+url.PathEscape(*name), nil, false))
	default:
		return fmt.Errorf("unknown vector-buckets subcommand %q", args[0])
	}
}

func (r Runner) analyticsBuckets(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("analytics-buckets requires subcommand: list, create, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("analytics-buckets list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/analytics-buckets", nil, false))
	case "create":
		fs := newFlagSet("analytics-buckets create", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Analytics bucket name")
		storageURI := fs.String("storage-uri", "", "Iceberg table storage URI")
		catalogURI := fs.String("catalog-uri", "", "Iceberg catalog URI")
		warehouse := fs.String("warehouse", "", "Iceberg warehouse")
		credentialHandle := fs.String("credential-handle", "", "secret:// handle for storage/catalog credentials")
		formatVersion := fs.Int("format-version", 2, "Iceberg format version: 1 or 2")
		partitioning := fs.String("partitioning", "", "Partition spec")
		retentionDays := fs.Int("retention-days", 0, "Retention period in days, 0 for indefinite")
		compactionSchedule := fs.String("compaction-schedule", "manual", "Compaction schedule")
		var metadata stringListFlag
		fs.Var(&metadata, "metadata", "Metadata key=value; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"name":                *name,
			"storage_uri":         *storageURI,
			"catalog_uri":         *catalogURI,
			"warehouse":           *warehouse,
			"credential_handle":   *credentialHandle,
			"format_version":      *formatVersion,
			"partitioning":        *partitioning,
			"retention_days":      *retentionDays,
			"compaction_schedule": *compactionSchedule,
			"metadata":            parseKeyValues(metadata),
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/analytics-buckets", payload, false))
	case "delete":
		fs := newFlagSet("analytics-buckets delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Analytics bucket name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/analytics-buckets/"+url.PathEscape(*name), nil, false))
	default:
		return fmt.Errorf("unknown analytics-buckets subcommand %q", args[0])
	}
}

func (r Runner) databaseRoles(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("database-roles requires subcommand: list, create, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("database-roles list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/database/roles", nil, false))
	case "create":
		fs := newFlagSet("database-roles create", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Database role name")
		login := fs.Bool("login", false, "Create as login role")
		noInherit := fs.Bool("no-inherit", false, "Disable role inheritance")
		bypassRLS := fs.Bool("bypass-rls", false, "Allow role to bypass RLS")
		connectionLimit := fs.Int("connection-limit", 0, "Connection limit; -1 for unlimited")
		passwordHandle := fs.String("password-secret-handle", "", "secret:// handle for login role password")
		var memberOf stringListFlag
		var grants stringListFlag
		var metadata stringListFlag
		fs.Var(&memberOf, "member-of", "Role membership; repeatable or comma-separated")
		fs.Var(&grants, "grant", "Schema grant schema=privileges; repeatable or comma-separated")
		fs.Var(&metadata, "metadata", "Metadata key=value; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		inherit := !*noInherit
		payload := map[string]any{
			"name":                   *name,
			"login":                  *login,
			"inherit":                inherit,
			"bypass_rls":             *bypassRLS,
			"connection_limit":       *connectionLimit,
			"password_secret_handle": *passwordHandle,
			"member_of":              splitListValues(memberOf),
			"schema_grants":          parseGrantValues(grants),
			"metadata":               parseKeyValues(metadata),
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/database/roles", payload, false))
	case "delete":
		fs := newFlagSet("database-roles delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Database role name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/database/roles/"+url.PathEscape(*name), nil, false))
	default:
		return fmt.Errorf("unknown database-roles subcommand %q", args[0])
	}
}

func (r Runner) databaseExtensions(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("database-extensions requires subcommand: list, set")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("database-extensions list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/database/extensions", nil, false))
	case "set":
		fs := newFlagSet("database-extensions set", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Extension name")
		schema := fs.String("schema", "", "Extension schema")
		version := fs.String("version", "", "Pinned extension version")
		enabled := fs.Bool("enabled", true, "Enable extension")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"schema":  *schema,
			"version": *version,
			"enabled": *enabled,
		}
		return r.printResponse(c.do(ctx, http.MethodPut, "/v1/projects/"+url.PathEscape(*ref)+"/database/extensions/"+url.PathEscape(*name), payload, false))
	default:
		return fmt.Errorf("unknown database-extensions subcommand %q", args[0])
	}
}

func (r Runner) databaseCron(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("database-cron requires subcommand: list, create, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("database-cron list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/database/cron-jobs", nil, false))
	case "create":
		fs := newFlagSet("database-cron create", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Cron job name")
		schedule := fs.String("schedule", "", "Cron schedule")
		command := fs.String("command", "", "SQL command")
		commandFile := fs.String("command-file", "", "Path to SQL command file")
		database := fs.String("database", "postgres", "Database name")
		username := fs.String("username", "postgres", "Database username")
		active := fs.Bool("active", true, "Whether the job is active")
		timeoutSeconds := fs.Int("timeout-seconds", 60, "Statement timeout seconds")
		maxRuntimeSeconds := fs.Int("max-runtime-seconds", 60, "Maximum runtime seconds")
		var metadata stringListFlag
		fs.Var(&metadata, "metadata", "Metadata key=value; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		commandText := *command
		if *commandFile != "" {
			payload, err := os.ReadFile(filepath.Clean(*commandFile))
			if err != nil {
				return err
			}
			commandText = string(payload)
		}
		payload := map[string]any{
			"name":                *name,
			"schedule":            *schedule,
			"command":             commandText,
			"database":            *database,
			"username":            *username,
			"active":              *active,
			"timeout_seconds":     *timeoutSeconds,
			"max_runtime_seconds": *maxRuntimeSeconds,
			"metadata":            parseKeyValues(metadata),
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/database/cron-jobs", payload, false))
	case "delete":
		fs := newFlagSet("database-cron delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Cron job name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/database/cron-jobs/"+url.PathEscape(*name), nil, false))
	default:
		return fmt.Errorf("unknown database-cron subcommand %q", args[0])
	}
}

func (r Runner) databaseQueues(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("database-queues requires subcommand: list, create, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("database-queues list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/database/queues", nil, false))
	case "create":
		fs := newFlagSet("database-queues create", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Queue name")
		schema := fs.String("schema", "pgmq", "Queue schema")
		retentionMinutes := fs.Int("retention-minutes", 1440, "Retention window in minutes")
		visibilityTimeoutSeconds := fs.Int("visibility-timeout-seconds", 30, "Visibility timeout seconds")
		maxRetries := fs.Int("max-retries", 5, "Maximum retry attempts before dead-letter handling")
		deadLetterQueue := fs.String("dead-letter-queue", "", "Dead-letter queue name")
		active := fs.Bool("active", true, "Whether the queue declaration is active")
		var metadata stringListFlag
		fs.Var(&metadata, "metadata", "Metadata key=value; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"name":                       *name,
			"schema":                     *schema,
			"retention_minutes":          *retentionMinutes,
			"visibility_timeout_seconds": *visibilityTimeoutSeconds,
			"max_retries":                *maxRetries,
			"dead_letter_queue":          *deadLetterQueue,
			"active":                     *active,
			"metadata":                   parseKeyValues(metadata),
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/database/queues", payload, false))
	case "delete":
		fs := newFlagSet("database-queues delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Queue name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/database/queues/"+url.PathEscape(*name), nil, false))
	default:
		return fmt.Errorf("unknown database-queues subcommand %q", args[0])
	}
}

func (r Runner) databaseWebhooks(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("database-webhooks requires subcommand: list, create, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("database-webhooks list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/database/webhooks", nil, false))
	case "create":
		fs := newFlagSet("database-webhooks create", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Webhook name")
		schema := fs.String("schema", "public", "Table schema")
		table := fs.String("table", "", "Table name")
		events := fs.String("events", "insert,update,delete", "Comma-separated events")
		endpoint := fs.String("endpoint", "", "HTTPS endpoint")
		method := fs.String("method", "POST", "HTTP method")
		timeoutSeconds := fs.Int("timeout-seconds", 10, "Request timeout seconds")
		retryCount := fs.Int("retry-count", 3, "Retry count")
		active := fs.Bool("active", true, "Whether the webhook declaration is active")
		var headers stringListFlag
		var metadata stringListFlag
		fs.Var(&headers, "header", "HTTP header key=value; repeatable or comma-separated")
		fs.Var(&metadata, "metadata", "Metadata key=value; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"name":            *name,
			"schema":          *schema,
			"table":           *table,
			"events":          parseListValues([]string{*events}),
			"endpoint":        *endpoint,
			"http_method":     *method,
			"headers":         parseKeyValues(headers),
			"timeout_seconds": *timeoutSeconds,
			"retry_count":     *retryCount,
			"active":          *active,
			"metadata":        parseKeyValues(metadata),
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/database/webhooks", payload, false))
	case "delete":
		fs := newFlagSet("database-webhooks delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Webhook name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/database/webhooks/"+url.PathEscape(*name), nil, false))
	default:
		return fmt.Errorf("unknown database-webhooks subcommand %q", args[0])
	}
}

func (r Runner) databaseSchemas(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("database-schemas requires subcommand: list, create, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("database-schemas list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/database/schemas", nil, false))
	case "create":
		fs := newFlagSet("database-schemas create", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Schema declaration name")
		version := fs.String("version", "", "Migration version")
		schema := fs.String("schema", "public", "Target database schema")
		sqlText := fs.String("sql", "", "SQL migration text")
		sqlFile := fs.String("sql-file", "", "Path to SQL migration file")
		applyOrder := fs.Int("apply-order", 0, "Apply order")
		active := fs.Bool("active", true, "Whether the schema migration is active")
		var metadata stringListFlag
		fs.Var(&metadata, "metadata", "Metadata key=value; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		sqlPayload := *sqlText
		if *sqlFile != "" {
			payload, err := os.ReadFile(filepath.Clean(*sqlFile))
			if err != nil {
				return err
			}
			sqlPayload = string(payload)
		}
		payload := map[string]any{
			"name":        *name,
			"version":     *version,
			"schema":      *schema,
			"sql":         sqlPayload,
			"apply_order": *applyOrder,
			"active":      *active,
			"metadata":    parseKeyValues(metadata),
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/database/schemas", payload, false))
	case "delete":
		fs := newFlagSet("database-schemas delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Schema declaration name")
		version := fs.String("version", "", "Migration version")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/database/schemas/"+url.PathEscape(*name)+"/"+url.PathEscape(*version), nil, false))
	default:
		return fmt.Errorf("unknown database-schemas subcommand %q", args[0])
	}
}

func (r Runner) storageBuckets(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("storage-buckets requires subcommand: list, create, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("storage-buckets list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/storage/buckets", nil, false))
	case "create":
		fs := newFlagSet("storage-buckets create", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Storage bucket name")
		public := fs.Bool("public", false, "Make bucket public")
		fileSizeLimit := fs.Int64("file-size-limit", 0, "File size limit in bytes; 0 uses platform default")
		cacheControl := fs.String("cache-control", "", "Cache-Control max age or directive")
		avif := fs.Bool("avif", false, "Enable AVIF autodetection")
		var mimeTypes stringListFlag
		var metadata stringListFlag
		fs.Var(&mimeTypes, "mime-type", "Allowed MIME type; repeatable or comma-separated")
		fs.Var(&metadata, "metadata", "Metadata key=value; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"name":               *name,
			"public":             *public,
			"file_size_limit":    *fileSizeLimit,
			"allowed_mime_types": splitListValues(mimeTypes),
			"cache_control":      *cacheControl,
			"avif_autodetection": *avif,
			"metadata":           parseKeyValues(metadata),
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/storage/buckets", payload, false))
	case "delete":
		fs := newFlagSet("storage-buckets delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Storage bucket name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/storage/buckets/"+url.PathEscape(*name), nil, false))
	default:
		return fmt.Errorf("unknown storage-buckets subcommand %q", args[0])
	}
}

func (r Runner) cdn(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("cdn requires subcommand: policy, set-policy, invalidations, invalidate, object-event")
	}
	switch args[0] {
	case "policy":
		fs := newFlagSet("cdn policy", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/cdn/policy", nil, false))
	case "set-policy":
		fs := newFlagSet("cdn set-policy", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		enabled := fs.Bool("enabled", false, "Enable CDN policy")
		browserTTL := fs.Int("browser-ttl", 3600, "Browser max-age in seconds")
		edgeTTL := fs.Int("edge-ttl", 3600, "Edge s-maxage in seconds")
		staleTTL := fs.Int("stale-while-revalidate", 60, "stale-while-revalidate seconds")
		smart := fs.Bool("smart", false, "Enable smart revalidation metadata")
		cacheControl := fs.String("cache-control", "", "Explicit Cache-Control header")
		var include stringListFlag
		var exclude stringListFlag
		fs.Var(&include, "include", "Included path pattern; repeatable or comma-separated")
		fs.Var(&exclude, "exclude", "Excluded path pattern; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"enabled":                        *enabled,
			"browser_ttl_seconds":            *browserTTL,
			"edge_ttl_seconds":               *edgeTTL,
			"stale_while_revalidate_seconds": *staleTTL,
			"included_paths":                 []string(include),
			"excluded_paths":                 []string(exclude),
			"smart_revalidation":             *smart,
			"cache_control":                  *cacheControl,
		}
		return r.printResponse(c.do(ctx, http.MethodPut, "/v1/projects/"+url.PathEscape(*ref)+"/cdn/policy", payload, false))
	case "invalidations":
		fs := newFlagSet("cdn invalidations", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/cdn/invalidations", nil, false))
	case "invalidate":
		fs := newFlagSet("cdn invalidate", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		var paths stringListFlag
		fs.Var(&paths, "path", "Path to invalidate; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/cdn/invalidations", map[string]any{"paths": []string(paths)}, false))
	case "object-event":
		fs := newFlagSet("cdn object-event", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		eventID := fs.String("event-id", "", "Storage event id")
		bucket := fs.String("bucket", "", "Storage bucket name")
		objectPath := fs.String("object-path", "", "Object path inside the bucket")
		eventType := fs.String("event-type", "object_changed", "Event type: object_created, object_updated, object_deleted, object_changed")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"event_id":    *eventID,
			"bucket":      *bucket,
			"object_path": *objectPath,
			"event_type":  *eventType,
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/cdn/object-events", payload, false))
	default:
		return fmt.Errorf("unknown cdn subcommand %q", args[0])
	}
}

func (r Runner) network(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("network requires subcommand: get")
	}
	switch args[0] {
	case "get":
		fs := newFlagSet("network get", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/network", nil, false))
	default:
		return fmt.Errorf("unknown network subcommand %q", args[0])
	}
}

func (r Runner) networkConnections(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("network-connections requires subcommand: list, create, delete")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("network-connections list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/network-connections", nil, false))
	case "create":
		fs := newFlagSet("network-connections create", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		name := fs.String("name", "", "Connection name")
		connectionType := fs.String("type", "operator_network", "Connection type: privatelink, vpc_peering, private_endpoint, wireguard, operator_network")
		provider := fs.String("provider", "operator", "Provider: aws, gcp, azure, custom, operator")
		region := fs.String("region", "", "Provider region")
		endpointID := fs.String("endpoint-id", "", "Provider endpoint or connection ID")
		var cidrs stringListFlag
		var config stringListFlag
		fs.Var(&cidrs, "cidr", "Allowed private CIDR/address; repeatable or comma-separated")
		fs.Var(&config, "config", "Connection config key=value; repeatable or comma-separated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload := map[string]any{
			"name":        *name,
			"type":        *connectionType,
			"provider":    *provider,
			"region":      *region,
			"cidrs":       []string(cidrs),
			"endpoint_id": *endpointID,
			"config":      parseKeyValues(config),
		}
		return r.printResponse(c.do(ctx, http.MethodPost, "/v1/projects/"+url.PathEscape(*ref)+"/network-connections", payload, false))
	case "delete":
		fs := newFlagSet("network-connections delete", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		id := fs.String("id", "", "Connection ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodDelete, "/v1/projects/"+url.PathEscape(*ref)+"/network-connections/"+url.PathEscape(*id), nil, false))
	default:
		return fmt.Errorf("unknown network-connections subcommand %q", args[0])
	}
}

func (r Runner) metrics(ctx context.Context, c apiClient, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "fleet":
			args = args[1:]
		case "project":
			args = args[1:]
		}
	}
	fs := newFlagSet("metrics", r.Stderr)
	prometheus := fs.Bool("prometheus", false, "Print Prometheus text format")
	ref := fs.String("ref", "", "Project ref for project-scoped metrics")
	history := fs.Bool("history", false, "Print retained project telemetry history for --ref")
	historyRange := fs.String("range", "6h", "Telemetry history range, e.g. 1h, 24h, 7d, 30d")
	historyStep := fs.String("step", "", "Telemetry history bucket step, e.g. 15s, 5m")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *prometheus {
		return r.printResponse(c.do(ctx, http.MethodGet, "/metrics", nil, true))
	}
	if strings.TrimSpace(*ref) != "" {
		if *history {
			query := url.Values{}
			if strings.TrimSpace(*historyRange) != "" {
				query.Set("range", strings.TrimSpace(*historyRange))
			}
			if strings.TrimSpace(*historyStep) != "" {
				query.Set("step", strings.TrimSpace(*historyStep))
			}
			path := "/v1/projects/" + url.PathEscape(*ref) + "/telemetry/history"
			if encoded := query.Encode(); encoded != "" {
				path += "?" + encoded
			}
			return r.printResponse(c.do(ctx, http.MethodGet, path, nil, false))
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/metrics", nil, false))
	}
	if *history {
		return fmt.Errorf("metrics --history requires --ref")
	}
	return r.printResponse(c.do(ctx, http.MethodGet, "/v1/metrics", nil, false))
}

func (r Runner) advisor(ctx context.Context, c apiClient, args []string) error {
	fs := newFlagSet("advisor", r.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return r.printResponse(c.do(ctx, http.MethodGet, "/v1/advisor", nil, false))
}

func (r Runner) compliance(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("compliance requires subcommand: report")
	}
	switch args[0] {
	case "report":
		fs := newFlagSet("compliance report", r.Stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/compliance/report", nil, false))
	default:
		return fmt.Errorf("unknown compliance subcommand %q", args[0])
	}
}

func (r Runner) audit(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("audit requires subcommand: list, integrity")
	}
	switch args[0] {
	case "list":
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/audit-events", nil, false))
	case "integrity":
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/audit-events/integrity", nil, false))
	default:
		return fmt.Errorf("unknown audit subcommand %q", args[0])
	}
}

func (r Runner) logs(ctx context.Context, c apiClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("logs requires subcommand: list, tail")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("logs list", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return r.printResponse(c.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(*ref)+"/logs", nil, false))
	case "tail":
		fs := newFlagSet("logs tail", r.Stderr)
		ref := fs.String("ref", "", "Project ref")
		once := fs.Bool("once", false, "Emit current logs and exit")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path := "/v1/projects/" + url.PathEscape(*ref) + "/logs/stream"
		if *once {
			path += "?follow=false"
		}
		return c.stream(ctx, path, r.Stdout)
	default:
		return fmt.Errorf("unknown logs subcommand %q", args[0])
	}
}

func (r Runner) printResponse(payload []byte, status int, err error) error {
	if err != nil {
		return err
	}
	if status == http.StatusNoContent {
		fmt.Fprintln(r.Stdout, "{}")
		return nil
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("api returned %d: %s", status, strings.TrimSpace(string(payload)))
	}
	if json.Valid(payload) {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, payload, "", "  "); err == nil {
			pretty.WriteByte('\n')
			_, _ = r.Stdout.Write(pretty.Bytes())
			return nil
		}
	}
	_, _ = r.Stdout.Write(payload)
	if len(payload) == 0 || payload[len(payload)-1] != '\n' {
		fmt.Fprintln(r.Stdout)
	}
	return nil
}

func (r Runner) printUsage() {
	fmt.Fprintln(r.Stderr, "usage: supadupa-cli [--api URL] [--token TOKEN] <command> [args]")
	fmt.Fprintln(r.Stderr, "commands: bootstrap, login, mfa, orgs, users, scim, provisioner, members, teams, access, hosts, quotas, usage, billing, settings, backup-targets, projects, config, services, domains, routes, log-drains, secrets, branches, replicas, backups, pitr, functions, auth-clients, auth-hooks, replication, embeddings, database-extensions, database-cron, database-queues, database-webhooks, database-schemas, database-roles, storage-buckets, vector-buckets, analytics-buckets, cdn, network, network-connections, metrics, advisor, compliance, audit, logs")
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

type apiClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func (c apiClient) do(ctx context.Context, method string, path string, body any, text bool) ([]byte, int, error) {
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
	if text {
		req.Header.Set("Accept", "text/plain")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	return payload, resp.StatusCode, err
}

func (c apiClient) stream(ctx context.Context, path string, output io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	_, err = io.Copy(output, resp.Body)
	return err
}

func newFlagSet(name string, output io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(output)
	return fs
}

func normalizeBaseURL(input string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(input), "/")
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("must include scheme and host")
	}
	return trimmed, nil
}

type stringListFlag []string

func (s *stringListFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringListFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func parseKeyValues(inputs []string) map[string]string {
	out := map[string]string{}
	for _, input := range inputs {
		for _, part := range splitKeyValueInput(input) {
			key, value, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key != "" {
				out[key] = value
			}
		}
	}
	return out
}

func splitKeyValueInput(input string) []string {
	key, value, ok := strings.Cut(input, "=")
	if ok && strings.TrimSpace(key) != "" {
		trimmedValue := strings.TrimSpace(value)
		if strings.HasPrefix(trimmedValue, "{") || strings.HasPrefix(trimmedValue, "[") {
			return []string{input}
		}
	}
	return strings.Split(input, ",")
}

func parseListValues(inputs []string) []string {
	out := []string{}
	for _, input := range inputs {
		for _, part := range strings.Split(input, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func parseGrantValues(inputs []string) map[string]string {
	out := map[string]string{}
	for _, input := range inputs {
		key, value, ok := strings.Cut(input, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return out
}

func splitListValues(inputs []string) []string {
	out := []string{}
	for _, input := range inputs {
		for _, part := range strings.Split(input, ",") {
			value := strings.TrimSpace(part)
			if value != "" {
				out = append(out, value)
			}
		}
	}
	return out
}

func parseBoolKeyValues(inputs []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, input := range inputs {
		for _, part := range strings.Split(input, ",") {
			key, value, ok := strings.Cut(part, "=")
			if !ok {
				return nil, fmt.Errorf("expected key=value, got %q", part)
			}
			key = strings.TrimSpace(key)
			value = strings.ToLower(strings.TrimSpace(value))
			if key == "" {
				return nil, fmt.Errorf("service name is required")
			}
			switch value {
			case "true", "1", "yes", "on", "enabled":
				out[key] = true
			case "false", "0", "no", "off", "disabled":
				out[key] = false
			default:
				return nil, fmt.Errorf("service %s requires boolean value", key)
			}
		}
	}
	return out, nil
}
