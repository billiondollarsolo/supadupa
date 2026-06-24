package control

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	controlStateCheckpointID       = "default"
	normalizedMetaSyncAdvisoryLock = int64(787403015222885991)
	encryptedStringPrefix          = "supadupa:v1:encrypted-string:"
)

type PersistentStore struct {
	*MemoryStore
	db         *sql.DB
	encryption persistentPayloadCipher
	saveMu     sync.Mutex
}

type memoryStoreSnapshot struct {
	PlatformDefaults      PlatformDefaults
	PlatformSSO           PlatformSSOConfig
	Users                 map[string]User
	Orgs                  map[string]Org
	OrgQuotas             map[string]OrgQuota
	UsageSnapshots        map[string][]UsageSnapshot
	BillingInvoices       map[string][]BillingInvoice
	Memberships           map[string]map[string]Membership
	Teams                 map[string]map[string]Team
	TeamMembers           map[string]map[string]TeamMember
	ProjectAccess         map[string][]ProjectAccessGrant
	Hosts                 map[string]Host
	Projects              map[string]Project
	Routes                map[string][]ProjectRoute
	Domains               map[string][]ProjectDomain
	Configs               map[string]map[string]ProjectConfig
	AuthClients           map[string][]ProjectAuthClient
	AuthHooks             map[string][]ProjectAuthHook
	Functions             map[string][]ProjectFunction
	FunctionRegions       map[string][]ProjectFunctionRegion
	FunctionStorageMounts map[string][]ProjectFunctionStorageMount
	ReplicationPipelines  map[string][]ProjectReplicationPipeline
	EmbeddingJobs         map[string][]ProjectEmbeddingJob
	DatabaseExtensions    map[string][]ProjectDatabaseExtension
	DatabaseCronJobs      map[string][]ProjectDatabaseCronJob
	DatabaseQueues        map[string][]ProjectDatabaseQueue
	DatabaseWebhooks      map[string][]ProjectDatabaseWebhook
	DatabaseSchemas       map[string][]ProjectDatabaseSchema
	DatabaseRoles         map[string][]ProjectDatabaseRole
	StorageBuckets        map[string][]ProjectStorageBucket
	VectorBuckets         map[string][]ProjectVectorBucket
	AnalyticsBuckets      map[string][]ProjectAnalyticsBucket
	CDNPolicies           map[string]ProjectCDNPolicy
	CDNInvalidations      map[string][]CDNInvalidation
	NetworkConnections    map[string][]ProjectNetworkConnection
	Branches              map[string][]ProjectBranch
	Replicas              map[string][]ProjectReplica
	LogDrains             map[string][]LogDrain
	Secrets               map[string]map[string]ProjectSecret
	BackupStorageTargets  map[string]BackupStorageTarget
	Policies              map[string]BackupPolicy
	PITRPolicies          map[string]PITRPolicy
	Backups               []Backup
	PlatformBackups       []PlatformBackup
	WALArchives           []WALArchive
	ProjectLogs           []ProjectLog
	Telemetry             map[string]TelemetrySample
	TelemetryHistory      map[string][]TelemetryHistorySample
	NodeTelemetry         map[string]NodeTelemetrySample
	AuditEvents           []AuditEvent
}

func emptySnapshot() memoryStoreSnapshot {
	return memoryStoreSnapshot{
		PlatformDefaults:      defaultPlatformDefaults(),
		PlatformSSO:           defaultPlatformSSOConfig(),
		Users:                 map[string]User{},
		Orgs:                  map[string]Org{},
		OrgQuotas:             map[string]OrgQuota{},
		UsageSnapshots:        map[string][]UsageSnapshot{},
		BillingInvoices:       map[string][]BillingInvoice{},
		Memberships:           map[string]map[string]Membership{},
		Teams:                 map[string]map[string]Team{},
		TeamMembers:           map[string]map[string]TeamMember{},
		ProjectAccess:         map[string][]ProjectAccessGrant{},
		Hosts:                 map[string]Host{},
		Projects:              map[string]Project{},
		Routes:                map[string][]ProjectRoute{},
		Domains:               map[string][]ProjectDomain{},
		Configs:               map[string]map[string]ProjectConfig{},
		AuthClients:           map[string][]ProjectAuthClient{},
		AuthHooks:             map[string][]ProjectAuthHook{},
		Functions:             map[string][]ProjectFunction{},
		FunctionRegions:       map[string][]ProjectFunctionRegion{},
		FunctionStorageMounts: map[string][]ProjectFunctionStorageMount{},
		ReplicationPipelines:  map[string][]ProjectReplicationPipeline{},
		EmbeddingJobs:         map[string][]ProjectEmbeddingJob{},
		DatabaseExtensions:    map[string][]ProjectDatabaseExtension{},
		DatabaseCronJobs:      map[string][]ProjectDatabaseCronJob{},
		DatabaseQueues:        map[string][]ProjectDatabaseQueue{},
		DatabaseWebhooks:      map[string][]ProjectDatabaseWebhook{},
		DatabaseSchemas:       map[string][]ProjectDatabaseSchema{},
		DatabaseRoles:         map[string][]ProjectDatabaseRole{},
		StorageBuckets:        map[string][]ProjectStorageBucket{},
		VectorBuckets:         map[string][]ProjectVectorBucket{},
		AnalyticsBuckets:      map[string][]ProjectAnalyticsBucket{},
		CDNPolicies:           map[string]ProjectCDNPolicy{},
		CDNInvalidations:      map[string][]CDNInvalidation{},
		NetworkConnections:    map[string][]ProjectNetworkConnection{},
		Branches:              map[string][]ProjectBranch{},
		Replicas:              map[string][]ProjectReplica{},
		LogDrains:             map[string][]LogDrain{},
		Secrets:               map[string]map[string]ProjectSecret{},
		BackupStorageTargets:  map[string]BackupStorageTarget{},
		Policies:              map[string]BackupPolicy{},
		PITRPolicies:          map[string]PITRPolicy{},
		Backups:               []Backup{},
		PlatformBackups:       []PlatformBackup{},
		WALArchives:           []WALArchive{},
		ProjectLogs:           []ProjectLog{},
		Telemetry:             map[string]TelemetrySample{},
		TelemetryHistory:      map[string][]TelemetryHistorySample{},
		NodeTelemetry:         map[string]NodeTelemetrySample{},
		AuditEvents:           []AuditEvent{},
	}
}

func NewPersistentStore(ctx context.Context, db *sql.DB) (*PersistentStore, error) {
	encryption, err := DefaultPersistentEncryption()
	if err != nil {
		return nil, err
	}
	return NewPersistentStoreWithEncryption(ctx, db, encryption)
}

func NewPersistentStoreWithEncryption(ctx context.Context, db *sql.DB, encryption persistentPayloadCipher) (*PersistentStore, error) {
	if encryption == nil {
		return nil, fmt.Errorf("persistent encryption provider is required")
	}
	store := &PersistentStore{MemoryStore: NewMemoryStore(), db: db, encryption: encryption}
	if err := store.load(ctx); err != nil {
		return nil, err
	}
	if err := store.save(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *PersistentStore) load(ctx context.Context) error {
	var payload []byte
	err := s.db.QueryRowContext(ctx, `SELECT state FROM control_state_checkpoints WHERE id = $1`, controlStateCheckpointID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		snapshot, err := s.loadNormalizedSnapshot(ctx)
		if err != nil {
			return err
		}
		s.applySnapshot(snapshot)
		return nil
	}
	if err != nil {
		return fmt.Errorf("load control state checkpoint: %w", err)
	}
	payload, err = s.encryption.Decrypt(payload)
	if err != nil {
		return fmt.Errorf("decrypt control state checkpoint: %w", err)
	}
	var snapshot memoryStoreSnapshot
	if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&snapshot); err != nil {
		return fmt.Errorf("decode control state checkpoint: %w", err)
	}

	s.applySnapshot(snapshot)
	return nil
}

func (s *PersistentStore) applySnapshot(snapshot memoryStoreSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applySnapshotLocked(snapshot)
}

func (s *PersistentStore) applySnapshotLocked(snapshot memoryStoreSnapshot) {
	s.platformDefaults = normalizedPlatformDefaults(snapshot.PlatformDefaults)
	s.platformSSO = normalizedPlatformSSOConfig(snapshot.PlatformSSO)
	s.users = nonNilMap(snapshot.Users)
	s.orgs = nonNilMap(snapshot.Orgs)
	s.orgQuotas = nonNilMap(snapshot.OrgQuotas)
	s.usageSnapshots = nonNilSliceMap(snapshot.UsageSnapshots)
	s.billingInvoices = nonNilSliceMap(snapshot.BillingInvoices)
	s.memberships = nonNilNestedMap(snapshot.Memberships)
	s.teams = nonNilNestedMap(snapshot.Teams)
	s.teamMembers = nonNilNestedMap(snapshot.TeamMembers)
	s.projectAccess = nonNilSliceMap(snapshot.ProjectAccess)
	s.hosts = nonNilMap(snapshot.Hosts)
	s.projects = nonNilMap(snapshot.Projects)
	s.routes = nonNilSliceMap(snapshot.Routes)
	s.domains = nonNilSliceMap(snapshot.Domains)
	s.configs = nonNilNestedMap(snapshot.Configs)
	s.authClients = nonNilSliceMap(snapshot.AuthClients)
	s.authHooks = nonNilSliceMap(snapshot.AuthHooks)
	s.functions = nonNilSliceMap(snapshot.Functions)
	s.functionRegions = nonNilSliceMap(snapshot.FunctionRegions)
	s.functionStorageMounts = nonNilSliceMap(snapshot.FunctionStorageMounts)
	s.replicationPipelines = nonNilSliceMap(snapshot.ReplicationPipelines)
	s.embeddingJobs = nonNilSliceMap(snapshot.EmbeddingJobs)
	s.databaseExtensions = nonNilSliceMap(snapshot.DatabaseExtensions)
	s.databaseCronJobs = nonNilSliceMap(snapshot.DatabaseCronJobs)
	s.databaseQueues = nonNilSliceMap(snapshot.DatabaseQueues)
	s.databaseWebhooks = nonNilSliceMap(snapshot.DatabaseWebhooks)
	s.databaseSchemas = nonNilSliceMap(snapshot.DatabaseSchemas)
	s.databaseRoles = nonNilSliceMap(snapshot.DatabaseRoles)
	s.storageBuckets = nonNilSliceMap(snapshot.StorageBuckets)
	s.vectorBuckets = nonNilSliceMap(snapshot.VectorBuckets)
	s.analyticsBuckets = nonNilSliceMap(snapshot.AnalyticsBuckets)
	s.cdnPolicies = nonNilMap(snapshot.CDNPolicies)
	s.cdnInvalidations = nonNilSliceMap(snapshot.CDNInvalidations)
	s.networkConnections = nonNilSliceMap(snapshot.NetworkConnections)
	s.branches = nonNilSliceMap(snapshot.Branches)
	s.replicas = nonNilSliceMap(snapshot.Replicas)
	s.logDrains = nonNilSliceMap(snapshot.LogDrains)
	s.secrets = nonNilNestedMap(snapshot.Secrets)
	s.backupStorageTargets = nonNilMap(snapshot.BackupStorageTargets)
	s.policies = nonNilMap(snapshot.Policies)
	s.pitrPolicies = nonNilMap(snapshot.PITRPolicies)
	s.backups = append([]Backup(nil), snapshot.Backups...)
	s.platformBackups = append([]PlatformBackup(nil), snapshot.PlatformBackups...)
	s.walArchives = append([]WALArchive(nil), snapshot.WALArchives...)
	s.projectLogs = append([]ProjectLog(nil), snapshot.ProjectLogs...)
	s.telemetry = nonNilMap(snapshot.Telemetry)
	s.telemetryHistory = nonNilSliceMap(snapshot.TelemetryHistory)
	s.nodeTelemetry = nonNilMap(snapshot.NodeTelemetry)
	s.auditEvents = append([]AuditEvent(nil), snapshot.AuditEvents...)
}

func (s *PersistentStore) loadNormalizedSnapshot(ctx context.Context) (memoryStoreSnapshot, error) {
	snapshot := emptySnapshot()
	projectRefs := map[string]string{}

	rows, err := s.db.QueryContext(ctx, `SELECT domain, stack_version, profile, resource_tier, backup_schedule, COALESCE(feature_flags, '{}'::jsonb), COALESCE(array_to_json(database_ingress_allowed_cidrs), '[]'::json), smtp_enabled, smtp_host, smtp_port, smtp_sender_name, smtp_sender_email, smtp_username, smtp_password_handle, smtp_tls_mode, updated_at FROM platform_defaults WHERE id = $1`, controlStateCheckpointID)
	if err != nil {
		return snapshot, fmt.Errorf("load normalized platform defaults: %w", err)
	}
	for rows.Next() {
		var defaults PlatformDefaults
		var profile, tier string
		var featureFlagsPayload, databaseIngressAllowedCIDRsPayload []byte
		var smtpHost, smtpSenderName, smtpSenderEmail, smtpUsername, smtpPasswordHandle, smtpTLSMode sql.NullString
		if err := rows.Scan(&defaults.Domain, &defaults.StackVersion, &profile, &tier, &defaults.BackupSchedule, &featureFlagsPayload, &databaseIngressAllowedCIDRsPayload, &defaults.SMTP.Enabled, &smtpHost, &defaults.SMTP.Port, &smtpSenderName, &smtpSenderEmail, &smtpUsername, &smtpPasswordHandle, &smtpTLSMode, &defaults.UpdatedAt); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("scan normalized platform defaults: %w", err)
		}
		if len(featureFlagsPayload) > 0 {
			if err := json.Unmarshal(featureFlagsPayload, &defaults.FeatureFlags); err != nil {
				rows.Close()
				return snapshot, fmt.Errorf("decode normalized platform feature flags: %w", err)
			}
		}
		if len(databaseIngressAllowedCIDRsPayload) > 0 {
			if err := json.Unmarshal(databaseIngressAllowedCIDRsPayload, &defaults.DatabaseIngressAllowedCIDRs); err != nil {
				rows.Close()
				return snapshot, fmt.Errorf("decode normalized database ingress allowlist: %w", err)
			}
		}
		defaults.Profile = StackProfile(profile)
		defaults.ResourceTier = ResourceTier(tier)
		defaults.SMTP.Host = smtpHost.String
		defaults.SMTP.SenderName = smtpSenderName.String
		defaults.SMTP.SenderEmail = smtpSenderEmail.String
		defaults.SMTP.Username = smtpUsername.String
		defaults.SMTP.PasswordHandle = smtpPasswordHandle.String
		defaults.SMTP.TLSMode = smtpTLSMode.String
		snapshot.PlatformDefaults = normalizedPlatformDefaults(defaults)
	}
	if err := rows.Close(); err != nil {
		return snapshot, fmt.Errorf("close normalized platform defaults: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate normalized platform defaults: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT enabled, provider, idp_entity_id, sso_url, certificate_pem, acs_url, metadata_url, email_domain, auto_provision, default_role, COALESCE(scim_enabled, false), COALESCE(scim_token_hash, ''), updated_at FROM platform_sso WHERE id = $1`, controlStateCheckpointID)
	if err != nil {
		return snapshot, fmt.Errorf("load normalized platform sso: %w", err)
	}
	for rows.Next() {
		var config PlatformSSOConfig
		var certificate, metadataURL, emailDomain sql.NullString
		if err := rows.Scan(&config.Enabled, &config.Provider, &config.IDPEntityID, &config.SSOURL, &certificate, &config.ACSURL, &metadataURL, &emailDomain, &config.AutoProvision, &config.DefaultRole, &config.SCIMEnabled, &config.SCIMTokenHash, &config.UpdatedAt); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("scan normalized platform sso: %w", err)
		}
		config.Certificate = certificate.String
		config.MetadataURL = metadataURL.String
		config.EmailDomain = emailDomain.String
		snapshot.PlatformSSO = normalizedPlatformSSOConfig(config)
	}
	if err := rows.Close(); err != nil {
		return snapshot, fmt.Errorf("close normalized platform sso: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate normalized platform sso: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT id, name, COALESCE(feature_flags, '{}'::jsonb), created_at FROM orgs`)
	if err != nil {
		return snapshot, fmt.Errorf("load normalized orgs: %w", err)
	}
	for rows.Next() {
		var org Org
		var featureFlagsPayload []byte
		if err := rows.Scan(&org.ID, &org.Name, &featureFlagsPayload, &org.CreatedAt); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("scan normalized org: %w", err)
		}
		if err := json.Unmarshal(featureFlagsPayload, &org.FeatureFlagOverrides); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("decode org feature flags: %w", err)
		}
		snapshot.Orgs[org.ID] = org
	}
	if err := rows.Close(); err != nil {
		return snapshot, fmt.Errorf("close normalized orgs: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate normalized orgs: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT id, email, password_hash, role, mfa_enabled, mfa_secret, mfa_pending_secret, mfa_confirmed_at, mfa_updated_at, COALESCE(mfa_last_accepted_counter, 0), COALESCE(token_version, 1), created_at, last_login_at FROM users`)
	if err != nil {
		return snapshot, fmt.Errorf("load normalized users: %w", err)
	}
	for rows.Next() {
		var user User
		var mfaSecret, pendingSecret sql.NullString
		var confirmedAt, updatedAt, lastLoginAt sql.NullTime
		if err := rows.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.MFAEnabled, &mfaSecret, &pendingSecret, &confirmedAt, &updatedAt, &user.MFALastCounter, &user.TokenVersion, &user.CreatedAt, &lastLoginAt); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("scan normalized user: %w", err)
		}
		user.MFASecret, err = s.decryptOptionalString(mfaSecret)
		if err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("decrypt normalized user mfa secret: %w", err)
		}
		user.MFAPendingSecret, err = s.decryptOptionalString(pendingSecret)
		if err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("decrypt normalized user pending mfa secret: %w", err)
		}
		if user.TokenVersion < 1 {
			user.TokenVersion = 1
		}
		if confirmedAt.Valid {
			user.MFAConfirmedAt = confirmedAt.Time
		}
		if updatedAt.Valid {
			user.MFAUpdatedAt = updatedAt.Time
		}
		user.LastLoginAt = timePtr(lastLoginAt)
		snapshot.Users[user.Email] = user
	}
	if err := rows.Close(); err != nil {
		return snapshot, fmt.Errorf("close normalized users: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate normalized users: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT id, name, address, capacity, used, created_at FROM hosts`)
	if err != nil {
		return snapshot, fmt.Errorf("load normalized hosts: %w", err)
	}
	for rows.Next() {
		var host Host
		var capacityPayload, usedPayload []byte
		if err := rows.Scan(&host.ID, &host.Name, &host.Address, &capacityPayload, &usedPayload, &host.CreatedAt); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("scan normalized host: %w", err)
		}
		if err := decodeJSON(capacityPayload, &host.Capacity); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("decode host capacity %s: %w", host.ID, err)
		}
		if err := decodeJSON(usedPayload, &host.Used); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("decode host usage %s: %w", host.ID, err)
		}
		snapshot.Hosts[host.ID] = host
	}
	if err := rows.Close(); err != nil {
		return snapshot, fmt.Errorf("close normalized hosts: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate normalized hosts: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT p.id, p.ref, p.org_id, p.name, p.host_id, p.status, p.stack_version, p.profile, p.resource_tier, p.created_at, p.updated_at, COALESCE(ps.desired_state, '{}'::jsonb)
FROM projects p
LEFT JOIN project_specs ps ON ps.project_id = p.id`)
	if err != nil {
		return snapshot, fmt.Errorf("load normalized projects: %w", err)
	}
	for rows.Next() {
		var project Project
		var hostID sql.NullString
		var status, profile, tier string
		var specPayload []byte
		if err := rows.Scan(&project.ID, &project.Ref, &project.OrgID, &project.Name, &hostID, &status, &project.Spec.StackVersion, &profile, &tier, &project.CreatedAt, &project.UpdatedAt, &specPayload); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("scan normalized project: %w", err)
		}
		project.Status = ProjectPhase(status)
		project.Spec = ProjectSpec{
			Ref:          project.Ref,
			OrgID:        project.OrgID,
			Name:         project.Name,
			HostID:       hostID.String,
			StackVersion: project.Spec.StackVersion,
			Profile:      StackProfile(profile),
			ResourceTier: ResourceTier(tier),
		}
		if len(specPayload) > 0 && string(specPayload) != "{}" {
			if err := decodeJSON(specPayload, &project.Spec); err != nil {
				rows.Close()
				return snapshot, fmt.Errorf("decode project spec %s: %w", project.Ref, err)
			}
		}
		snapshot.Projects[project.Ref] = project
		projectRefs[project.ID] = project.Ref
	}
	if err := rows.Close(); err != nil {
		return snapshot, fmt.Errorf("close normalized projects: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate normalized projects: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT org_id, max_projects, max_cpu, max_ram_mb, max_disk_gb, used, updated_at FROM org_quotas`)
	if err != nil {
		return snapshot, fmt.Errorf("load normalized org quotas: %w", err)
	}
	for rows.Next() {
		var quota OrgQuota
		var usedPayload []byte
		if err := rows.Scan(&quota.OrgID, &quota.MaxProjects, &quota.MaxCPU, &quota.MaxRAMMB, &quota.MaxDiskGB, &usedPayload, &quota.UpdatedAt); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("scan normalized org quota: %w", err)
		}
		if err := decodeJSON(usedPayload, &quota.Used); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("decode org quota usage %s: %w", quota.OrgID, err)
		}
		snapshot.OrgQuotas[quota.OrgID] = quota
	}
	if err := rows.Close(); err != nil {
		return snapshot, fmt.Errorf("close normalized org quotas: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate normalized org quotas: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT id, org_id, metrics, sampled_at FROM usage_snapshots`)
	if err != nil {
		return snapshot, fmt.Errorf("load normalized usage snapshots: %w", err)
	}
	for rows.Next() {
		var usageSnapshot UsageSnapshot
		var metricsPayload []byte
		if err := rows.Scan(&usageSnapshot.ID, &usageSnapshot.OrgID, &metricsPayload, &usageSnapshot.SampledAt); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("scan normalized usage snapshot: %w", err)
		}
		if err := decodeJSON(metricsPayload, &usageSnapshot.Metrics); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("decode usage snapshot %s: %w", usageSnapshot.ID, err)
		}
		snapshot.UsageSnapshots[usageSnapshot.OrgID] = append(snapshot.UsageSnapshots[usageSnapshot.OrgID], usageSnapshot)
	}
	if err := rows.Close(); err != nil {
		return snapshot, fmt.Errorf("close normalized usage snapshots: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate normalized usage snapshots: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT id, org_id, usage_snapshot_id, number, status, currency, period_start, period_end, due_at, subtotal_cents, total_cents, line_items, metrics, created_at FROM billing_invoices`)
	if err != nil {
		return snapshot, fmt.Errorf("load normalized billing invoices: %w", err)
	}
	for rows.Next() {
		var invoice BillingInvoice
		var lineItemsPayload []byte
		var metricsPayload []byte
		if err := rows.Scan(&invoice.ID, &invoice.OrgID, &invoice.UsageSnapshotID, &invoice.Number, &invoice.Status, &invoice.Currency, &invoice.PeriodStart, &invoice.PeriodEnd, &invoice.DueAt, &invoice.SubtotalCents, &invoice.TotalCents, &lineItemsPayload, &metricsPayload, &invoice.CreatedAt); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("scan normalized billing invoice: %w", err)
		}
		if err := decodeJSON(lineItemsPayload, &invoice.LineItems); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("decode billing invoice line items %s: %w", invoice.ID, err)
		}
		if err := decodeJSON(metricsPayload, &invoice.Metrics); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("decode billing invoice metrics %s: %w", invoice.ID, err)
		}
		snapshot.BillingInvoices[invoice.OrgID] = append(snapshot.BillingInvoices[invoice.OrgID], invoice)
	}
	if err := rows.Close(); err != nil {
		return snapshot, fmt.Errorf("close normalized billing invoices: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate normalized billing invoices: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT m.user_id, m.org_id, u.email, m.role, m.created_at
FROM memberships m
JOIN users u ON u.id = m.user_id`)
	if err != nil {
		return snapshot, fmt.Errorf("load normalized memberships: %w", err)
	}
	for rows.Next() {
		var member Membership
		if err := rows.Scan(&member.UserID, &member.OrgID, &member.Email, &member.Role, &member.CreatedAt); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("scan normalized membership: %w", err)
		}
		if snapshot.Memberships[member.OrgID] == nil {
			snapshot.Memberships[member.OrgID] = map[string]Membership{}
		}
		snapshot.Memberships[member.OrgID][member.Email] = member
	}
	if err := rows.Close(); err != nil {
		return snapshot, fmt.Errorf("close normalized memberships: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate normalized memberships: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT id, org_id, name, slug, created_at FROM teams`)
	if err != nil {
		return snapshot, fmt.Errorf("load normalized teams: %w", err)
	}
	for rows.Next() {
		var team Team
		if err := rows.Scan(&team.ID, &team.OrgID, &team.Name, &team.Slug, &team.CreatedAt); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("scan normalized team: %w", err)
		}
		if snapshot.Teams[team.OrgID] == nil {
			snapshot.Teams[team.OrgID] = map[string]Team{}
		}
		snapshot.Teams[team.OrgID][team.Slug] = team
	}
	if err := rows.Close(); err != nil {
		return snapshot, fmt.Errorf("close normalized teams: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate normalized teams: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT tm.team_id, t.org_id, t.slug, tm.user_id, u.email, tm.created_at
FROM team_members tm
JOIN teams t ON t.id = tm.team_id
JOIN users u ON u.id = tm.user_id`)
	if err != nil {
		return snapshot, fmt.Errorf("load normalized team members: %w", err)
	}
	for rows.Next() {
		var member TeamMember
		if err := rows.Scan(&member.TeamID, &member.OrgID, &member.TeamSlug, &member.UserID, &member.Email, &member.CreatedAt); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("scan normalized team member: %w", err)
		}
		if snapshot.TeamMembers[member.TeamID] == nil {
			snapshot.TeamMembers[member.TeamID] = map[string]TeamMember{}
		}
		snapshot.TeamMembers[member.TeamID][member.Email] = member
	}
	if err := rows.Close(); err != nil {
		return snapshot, fmt.Errorf("close normalized team members: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("iterate normalized team members: %w", err)
	}

	if err := s.loadNormalizedProjectChildren(ctx, snapshot, projectRefs); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func (s *PersistentStore) loadNormalizedProjectChildren(ctx context.Context, snapshot memoryStoreSnapshot, projectRefs map[string]string) error {
	rows, err := s.db.QueryContext(ctx, `
SELECT project_id, id, name, fqdn, upstream_url, tls, ssl_enforced, COALESCE(array_to_json(ip_allowlist), '[]'::json), COALESCE(cache_control, ''), smart_cdn, created_at
FROM project_routes`)
	if err != nil {
		return fmt.Errorf("load normalized project routes: %w", err)
	}
	for rows.Next() {
		var projectID string
		var route ProjectRoute
		var allowlistPayload []byte
		if err := rows.Scan(&projectID, &route.ID, &route.Name, &route.FQDN, &route.UpstreamURL, &route.TLS, &route.SSLEnforced, &allowlistPayload, &route.CacheControl, &route.SmartCDN, &route.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized project route: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		route.ProjectRef = ref
		if err := decodeJSON(allowlistPayload, &route.IPAllowlist); err != nil {
			rows.Close()
			return fmt.Errorf("decode route allowlist %s/%s: %w", ref, route.Name, err)
		}
		snapshot.Routes[ref] = append(snapshot.Routes[ref], route)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized project routes: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized project routes: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, org_id, subject_type, subject_id, subject_name, role, created_at
FROM project_access_grants`)
	if err != nil {
		return fmt.Errorf("load normalized project access grants: %w", err)
	}
	for rows.Next() {
		var projectID string
		var grant ProjectAccessGrant
		if err := rows.Scan(&projectID, &grant.ID, &grant.OrgID, &grant.SubjectType, &grant.SubjectID, &grant.SubjectName, &grant.Role, &grant.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized project access grant: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		grant.ProjectRef = ref
		snapshot.ProjectAccess[ref] = append(snapshot.ProjectAccess[ref], grant)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized project access grants: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized project access grants: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT project_id, area, config, updated_at FROM project_configs`)
	if err != nil {
		return fmt.Errorf("load normalized project configs: %w", err)
	}
	for rows.Next() {
		var projectID string
		var config ProjectConfig
		var payload []byte
		if err := rows.Scan(&projectID, &config.Area, &payload, &config.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized project config: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		config.ProjectRef = ref
		if err := decodeJSON(payload, &config.Config); err != nil {
			rows.Close()
			return fmt.Errorf("decode project config %s/%s: %w", ref, config.Area, err)
		}
		if snapshot.Configs[ref] == nil {
			snapshot.Configs[ref] = map[string]ProjectConfig{}
		}
		snapshot.Configs[ref][config.Area] = config
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized project configs: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized project configs: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, client_id, client_secret_handle, redirect_uris, grant_types, scopes, confidential, status, message, created_at, updated_at
FROM auth_clients`)
	if err != nil {
		return fmt.Errorf("load normalized auth clients: %w", err)
	}
	for rows.Next() {
		var projectID string
		var client ProjectAuthClient
		var secretHandle sql.NullString
		var redirectURIsPayload []byte
		var grantTypesPayload []byte
		var scopesPayload []byte
		var message sql.NullString
		if err := rows.Scan(&projectID, &client.ID, &client.Name, &client.ClientID, &secretHandle, &redirectURIsPayload, &grantTypesPayload, &scopesPayload, &client.Confidential, &client.Status, &message, &client.CreatedAt, &client.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized auth client: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		client.ProjectRef = ref
		client.ClientSecretHandle = secretHandle.String
		client.Message = message.String
		if err := decodeJSON(redirectURIsPayload, &client.RedirectURIs); err != nil {
			rows.Close()
			return fmt.Errorf("decode auth client redirect uris %s/%s: %w", ref, client.ClientID, err)
		}
		if err := decodeJSON(grantTypesPayload, &client.GrantTypes); err != nil {
			rows.Close()
			return fmt.Errorf("decode auth client grant types %s/%s: %w", ref, client.ClientID, err)
		}
		if err := decodeJSON(scopesPayload, &client.Scopes); err != nil {
			rows.Close()
			return fmt.Errorf("decode auth client scopes %s/%s: %w", ref, client.ClientID, err)
		}
		snapshot.AuthClients[ref] = append(snapshot.AuthClients[ref], client)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized auth clients: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized auth clients: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, hook_type, enabled, target_uri, edge_function, secret_handle, headers, timeout_ms, retry_attempts, status, message, created_at, updated_at
FROM auth_hooks`)
	if err != nil {
		return fmt.Errorf("load normalized auth hooks: %w", err)
	}
	for rows.Next() {
		var projectID string
		var hook ProjectAuthHook
		var targetURI sql.NullString
		var edgeFunction sql.NullString
		var secretHandle sql.NullString
		var headersPayload []byte
		var message sql.NullString
		if err := rows.Scan(&projectID, &hook.ID, &hook.HookType, &hook.Enabled, &targetURI, &edgeFunction, &secretHandle, &headersPayload, &hook.TimeoutMS, &hook.RetryAttempts, &hook.Status, &message, &hook.CreatedAt, &hook.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized auth hook: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		hook.ProjectRef = ref
		hook.TargetURI = targetURI.String
		hook.EdgeFunction = edgeFunction.String
		hook.SecretHandle = secretHandle.String
		hook.Message = message.String
		if err := decodeJSON(headersPayload, &hook.Headers); err != nil {
			rows.Close()
			return fmt.Errorf("decode auth hook headers %s/%s: %w", ref, hook.HookType, err)
		}
		snapshot.AuthHooks[ref] = append(snapshot.AuthHooks[ref], hook)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized auth hooks: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized auth hooks: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, version, entrypoint, verify_jwt, status, source_hash, source_bytes, secrets, created_at, updated_at
FROM edge_functions`)
	if err != nil {
		return fmt.Errorf("load normalized edge functions: %w", err)
	}
	for rows.Next() {
		var projectID string
		var fn ProjectFunction
		var secretsPayload []byte
		if err := rows.Scan(&projectID, &fn.ID, &fn.Name, &fn.Version, &fn.Entrypoint, &fn.VerifyJWT, &fn.Status, &fn.SourceHash, &fn.SourceBytes, &secretsPayload, &fn.CreatedAt, &fn.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized edge function: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		fn.ProjectRef = ref
		if err := decodeJSON(secretsPayload, &fn.Secrets); err != nil {
			rows.Close()
			return fmt.Errorf("decode edge function secrets %s/%s: %w", ref, fn.Name, err)
		}
		snapshot.Functions[ref] = append(snapshot.Functions[ref], fn)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized edge functions: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized edge functions: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, function_name, host_id, region, routing_policy, invocation_url, status, message, created_at, updated_at
FROM function_regions`)
	if err != nil {
		return fmt.Errorf("load normalized function regions: %w", err)
	}
	for rows.Next() {
		var projectID string
		var region ProjectFunctionRegion
		var hostID sql.NullString
		var message sql.NullString
		if err := rows.Scan(&projectID, &region.ID, &region.FunctionName, &hostID, &region.Region, &region.RoutingPolicy, &region.InvocationURL, &region.Status, &message, &region.CreatedAt, &region.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized function region: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		region.ProjectRef = ref
		region.HostID = hostID.String
		region.Message = message.String
		snapshot.FunctionRegions[ref] = append(snapshot.FunctionRegions[ref], region)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized function regions: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized function regions: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, function_name, bucket_name, mount_path, read_only, prefix, env_alias, status, message, created_at, updated_at
FROM function_storage_mounts`)
	if err != nil {
		return fmt.Errorf("load normalized function storage mounts: %w", err)
	}
	for rows.Next() {
		var projectID string
		var mount ProjectFunctionStorageMount
		var prefix sql.NullString
		var envAlias sql.NullString
		var message sql.NullString
		if err := rows.Scan(&projectID, &mount.ID, &mount.FunctionName, &mount.BucketName, &mount.MountPath, &mount.ReadOnly, &prefix, &envAlias, &mount.Status, &message, &mount.CreatedAt, &mount.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized function storage mount: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		mount.ProjectRef = ref
		mount.Prefix = prefix.String
		mount.EnvAlias = envAlias.String
		mount.Message = message.String
		snapshot.FunctionStorageMounts[ref] = append(snapshot.FunctionStorageMounts[ref], mount)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized function storage mounts: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized function storage mounts: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, type, source_schema, source_table, destination, destination_uri, credential_handle, config, status, message, created_at, updated_at
FROM replication_pipelines`)
	if err != nil {
		return fmt.Errorf("load normalized replication pipelines: %w", err)
	}
	for rows.Next() {
		var projectID string
		var pipeline ProjectReplicationPipeline
		var credentialHandle sql.NullString
		var message sql.NullString
		var configPayload []byte
		if err := rows.Scan(&projectID, &pipeline.ID, &pipeline.Name, &pipeline.Type, &pipeline.SourceSchema, &pipeline.SourceTable, &pipeline.Destination, &pipeline.DestinationURI, &credentialHandle, &configPayload, &pipeline.Status, &message, &pipeline.CreatedAt, &pipeline.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized replication pipeline: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		pipeline.ProjectRef = ref
		pipeline.CredentialHandle = credentialHandle.String
		pipeline.Message = message.String
		if err := decodeJSON(configPayload, &pipeline.Config); err != nil {
			rows.Close()
			return fmt.Errorf("decode replication pipeline config %s/%s: %w", ref, pipeline.ID, err)
		}
		snapshot.ReplicationPipelines[ref] = append(snapshot.ReplicationPipelines[ref], pipeline)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized replication pipelines: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized replication pipelines: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, source_schema, source_table, source_column, primary_key_column, destination_table, destination_column, provider, model, dimension, schedule, batch_size, status, message, created_at, updated_at
FROM embedding_jobs`)
	if err != nil {
		return fmt.Errorf("load normalized embedding jobs: %w", err)
	}
	for rows.Next() {
		var projectID string
		var job ProjectEmbeddingJob
		var message sql.NullString
		if err := rows.Scan(&projectID, &job.ID, &job.Name, &job.SourceSchema, &job.SourceTable, &job.SourceColumn, &job.PrimaryKeyColumn, &job.DestinationTable, &job.DestinationColumn, &job.Provider, &job.Model, &job.Dimension, &job.Schedule, &job.BatchSize, &job.Status, &message, &job.CreatedAt, &job.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized embedding job: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		job.ProjectRef = ref
		job.Message = message.String
		snapshot.EmbeddingJobs[ref] = append(snapshot.EmbeddingJobs[ref], job)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized embedding jobs: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized embedding jobs: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, schema, version, enabled, status, message, created_at, updated_at
FROM database_extensions`)
	if err != nil {
		return fmt.Errorf("load normalized database extensions: %w", err)
	}
	for rows.Next() {
		var projectID string
		var extension ProjectDatabaseExtension
		var version sql.NullString
		var message sql.NullString
		if err := rows.Scan(&projectID, &extension.ID, &extension.Name, &extension.Schema, &version, &extension.Enabled, &extension.Status, &message, &extension.CreatedAt, &extension.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized database extension: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		extension.ProjectRef = ref
		extension.Version = version.String
		extension.Message = message.String
		snapshot.DatabaseExtensions[ref] = append(snapshot.DatabaseExtensions[ref], extension)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized database extensions: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized database extensions: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, schedule, command, database_name, username, active, timeout_seconds, max_runtime_seconds, metadata, status, message, created_at, updated_at
FROM database_cron_jobs`)
	if err != nil {
		return fmt.Errorf("load normalized database cron jobs: %w", err)
	}
	for rows.Next() {
		var projectID string
		var job ProjectDatabaseCronJob
		var metadataPayload []byte
		var message sql.NullString
		if err := rows.Scan(&projectID, &job.ID, &job.Name, &job.Schedule, &job.Command, &job.Database, &job.Username, &job.Active, &job.TimeoutSeconds, &job.MaxRuntimeSeconds, &metadataPayload, &job.Status, &message, &job.CreatedAt, &job.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized database cron job: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		job.ProjectRef = ref
		job.Message = message.String
		if err := decodeJSON(metadataPayload, &job.Metadata); err != nil {
			rows.Close()
			return fmt.Errorf("decode database cron job metadata %s/%s: %w", ref, job.Name, err)
		}
		snapshot.DatabaseCronJobs[ref] = append(snapshot.DatabaseCronJobs[ref], job)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized database cron jobs: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized database cron jobs: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, schema, retention_minutes, visibility_timeout_seconds, max_retries, dead_letter_queue, active, metadata, status, message, created_at, updated_at
FROM database_queues`)
	if err != nil {
		return fmt.Errorf("load normalized database queues: %w", err)
	}
	for rows.Next() {
		var projectID string
		var queue ProjectDatabaseQueue
		var deadLetterQueue sql.NullString
		var metadataPayload []byte
		var message sql.NullString
		if err := rows.Scan(&projectID, &queue.ID, &queue.Name, &queue.Schema, &queue.RetentionMinutes, &queue.VisibilityTimeoutSeconds, &queue.MaxRetries, &deadLetterQueue, &queue.Active, &metadataPayload, &queue.Status, &message, &queue.CreatedAt, &queue.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized database queue: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		queue.ProjectRef = ref
		queue.DeadLetterQueue = deadLetterQueue.String
		queue.Message = message.String
		if err := decodeJSON(metadataPayload, &queue.Metadata); err != nil {
			rows.Close()
			return fmt.Errorf("decode database queue metadata %s/%s: %w", ref, queue.Name, err)
		}
		snapshot.DatabaseQueues[ref] = append(snapshot.DatabaseQueues[ref], queue)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized database queues: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized database queues: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, schema, table_name, events, endpoint, http_method, headers, timeout_seconds, retry_count, active, metadata, status, message, created_at, updated_at
FROM database_webhooks`)
	if err != nil {
		return fmt.Errorf("load normalized database webhooks: %w", err)
	}
	for rows.Next() {
		var projectID string
		var webhook ProjectDatabaseWebhook
		var eventsPayload []byte
		var headersPayload []byte
		var metadataPayload []byte
		var message sql.NullString
		if err := rows.Scan(&projectID, &webhook.ID, &webhook.Name, &webhook.Schema, &webhook.Table, &eventsPayload, &webhook.Endpoint, &webhook.HTTPMethod, &headersPayload, &webhook.TimeoutSeconds, &webhook.RetryCount, &webhook.Active, &metadataPayload, &webhook.Status, &message, &webhook.CreatedAt, &webhook.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized database webhook: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		webhook.ProjectRef = ref
		webhook.Message = message.String
		if err := decodeJSON(eventsPayload, &webhook.Events); err != nil {
			rows.Close()
			return fmt.Errorf("decode database webhook events %s/%s: %w", ref, webhook.Name, err)
		}
		if err := decodeJSON(headersPayload, &webhook.Headers); err != nil {
			rows.Close()
			return fmt.Errorf("decode database webhook headers %s/%s: %w", ref, webhook.Name, err)
		}
		if err := decodeJSON(metadataPayload, &webhook.Metadata); err != nil {
			rows.Close()
			return fmt.Errorf("decode database webhook metadata %s/%s: %w", ref, webhook.Name, err)
		}
		snapshot.DatabaseWebhooks[ref] = append(snapshot.DatabaseWebhooks[ref], webhook)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized database webhooks: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized database webhooks: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, version, schema_name, sql, checksum, apply_order, active, metadata, status, message, created_at, updated_at
FROM database_schemas`)
	if err != nil {
		return fmt.Errorf("load normalized database schemas: %w", err)
	}
	for rows.Next() {
		var projectID string
		var schema ProjectDatabaseSchema
		var metadataPayload []byte
		var message sql.NullString
		if err := rows.Scan(&projectID, &schema.ID, &schema.Name, &schema.Version, &schema.Schema, &schema.SQL, &schema.Checksum, &schema.ApplyOrder, &schema.Active, &metadataPayload, &schema.Status, &message, &schema.CreatedAt, &schema.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized database schema: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		schema.ProjectRef = ref
		schema.Message = message.String
		if err := decodeJSON(metadataPayload, &schema.Metadata); err != nil {
			rows.Close()
			return fmt.Errorf("decode database schema metadata %s/%s@%s: %w", ref, schema.Name, schema.Version, err)
		}
		snapshot.DatabaseSchemas[ref] = append(snapshot.DatabaseSchemas[ref], schema)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized database schemas: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized database schemas: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, login, inherit, bypass_rls, connection_limit, password_secret_handle, member_of, schema_grants, metadata, status, message, created_at, updated_at
FROM database_roles`)
	if err != nil {
		return fmt.Errorf("load normalized database roles: %w", err)
	}
	for rows.Next() {
		var projectID string
		var role ProjectDatabaseRole
		var passwordHandle sql.NullString
		var memberOfPayload []byte
		var schemaGrantsPayload []byte
		var metadataPayload []byte
		var message sql.NullString
		if err := rows.Scan(&projectID, &role.ID, &role.Name, &role.Login, &role.Inherit, &role.BypassRLS, &role.ConnectionLimit, &passwordHandle, &memberOfPayload, &schemaGrantsPayload, &metadataPayload, &role.Status, &message, &role.CreatedAt, &role.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized database role: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		role.ProjectRef = ref
		role.PasswordSecretHandle = passwordHandle.String
		role.Message = message.String
		if err := decodeJSON(memberOfPayload, &role.MemberOf); err != nil {
			rows.Close()
			return fmt.Errorf("decode database role members %s/%s: %w", ref, role.Name, err)
		}
		if err := decodeJSON(schemaGrantsPayload, &role.SchemaGrants); err != nil {
			rows.Close()
			return fmt.Errorf("decode database role schema grants %s/%s: %w", ref, role.Name, err)
		}
		if err := decodeJSON(metadataPayload, &role.Metadata); err != nil {
			rows.Close()
			return fmt.Errorf("decode database role metadata %s/%s: %w", ref, role.Name, err)
		}
		snapshot.DatabaseRoles[ref] = append(snapshot.DatabaseRoles[ref], role)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized database roles: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized database roles: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, public, file_size_limit, allowed_mime_types, cache_control, avif_autodetection, metadata, status, message, created_at, updated_at
FROM storage_buckets`)
	if err != nil {
		return fmt.Errorf("load normalized storage buckets: %w", err)
	}
	for rows.Next() {
		var projectID string
		var bucket ProjectStorageBucket
		var mimeTypesPayload []byte
		var metadataPayload []byte
		var message sql.NullString
		if err := rows.Scan(&projectID, &bucket.ID, &bucket.Name, &bucket.Public, &bucket.FileSizeLimit, &mimeTypesPayload, &bucket.CacheControl, &bucket.AvifAutodetection, &metadataPayload, &bucket.Status, &message, &bucket.CreatedAt, &bucket.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized storage bucket: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		bucket.ProjectRef = ref
		bucket.Message = message.String
		if err := decodeJSON(mimeTypesPayload, &bucket.AllowedMimeTypes); err != nil {
			rows.Close()
			return fmt.Errorf("decode storage bucket mime types %s/%s: %w", ref, bucket.Name, err)
		}
		if err := decodeJSON(metadataPayload, &bucket.Metadata); err != nil {
			rows.Close()
			return fmt.Errorf("decode storage bucket metadata %s/%s: %w", ref, bucket.Name, err)
		}
		snapshot.StorageBuckets[ref] = append(snapshot.StorageBuckets[ref], bucket)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized storage buckets: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized storage buckets: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, dimension, distance, index_method, storage_backend, storage_uri, metadata, status, message, created_at, updated_at
FROM vector_buckets`)
	if err != nil {
		return fmt.Errorf("load normalized vector buckets: %w", err)
	}
	for rows.Next() {
		var projectID string
		var bucket ProjectVectorBucket
		var storageURI sql.NullString
		var message sql.NullString
		var metadataPayload []byte
		if err := rows.Scan(&projectID, &bucket.ID, &bucket.Name, &bucket.Dimension, &bucket.Distance, &bucket.IndexMethod, &bucket.StorageBackend, &storageURI, &metadataPayload, &bucket.Status, &message, &bucket.CreatedAt, &bucket.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized vector bucket: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		bucket.ProjectRef = ref
		bucket.StorageURI = storageURI.String
		bucket.Message = message.String
		if err := decodeJSON(metadataPayload, &bucket.Metadata); err != nil {
			rows.Close()
			return fmt.Errorf("decode vector bucket metadata %s/%s: %w", ref, bucket.Name, err)
		}
		snapshot.VectorBuckets[ref] = append(snapshot.VectorBuckets[ref], bucket)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized vector buckets: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized vector buckets: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, storage_uri, catalog_uri, warehouse, credential_handle, format_version, partitioning, retention_days, compaction_schedule, metadata, status, message, created_at, updated_at
FROM analytics_buckets`)
	if err != nil {
		return fmt.Errorf("load normalized analytics buckets: %w", err)
	}
	for rows.Next() {
		var projectID string
		var bucket ProjectAnalyticsBucket
		var catalogURI sql.NullString
		var credentialHandle sql.NullString
		var partitioning sql.NullString
		var message sql.NullString
		var metadataPayload []byte
		if err := rows.Scan(&projectID, &bucket.ID, &bucket.Name, &bucket.StorageURI, &catalogURI, &bucket.Warehouse, &credentialHandle, &bucket.FormatVersion, &partitioning, &bucket.RetentionDays, &bucket.CompactionSchedule, &metadataPayload, &bucket.Status, &message, &bucket.CreatedAt, &bucket.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized analytics bucket: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		bucket.ProjectRef = ref
		bucket.CatalogURI = catalogURI.String
		bucket.CredentialHandle = credentialHandle.String
		bucket.Partitioning = partitioning.String
		bucket.Message = message.String
		if err := decodeJSON(metadataPayload, &bucket.Metadata); err != nil {
			rows.Close()
			return fmt.Errorf("decode analytics bucket metadata %s/%s: %w", ref, bucket.Name, err)
		}
		snapshot.AnalyticsBuckets[ref] = append(snapshot.AnalyticsBuckets[ref], bucket)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized analytics buckets: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized analytics buckets: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, enabled, browser_ttl_seconds, edge_ttl_seconds, stale_while_revalidate_seconds, COALESCE(array_to_json(included_paths), '[]'::json), COALESCE(array_to_json(excluded_paths), '[]'::json), smart_revalidation, cache_control, updated_at
FROM cdn_policies`)
	if err != nil {
		return fmt.Errorf("load normalized cdn policies: %w", err)
	}
	for rows.Next() {
		var projectID string
		var policy ProjectCDNPolicy
		var includedPayload []byte
		var excludedPayload []byte
		if err := rows.Scan(&projectID, &policy.Enabled, &policy.BrowserTTLSeconds, &policy.EdgeTTLSeconds, &policy.StaleWhileRevalidateSeconds, &includedPayload, &excludedPayload, &policy.SmartRevalidation, &policy.CacheControl, &policy.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized cdn policy: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		policy.ProjectRef = ref
		if err := decodeJSON(includedPayload, &policy.IncludedPaths); err != nil {
			rows.Close()
			return fmt.Errorf("decode cdn included paths %s: %w", ref, err)
		}
		if err := decodeJSON(excludedPayload, &policy.ExcludedPaths); err != nil {
			rows.Close()
			return fmt.Errorf("decode cdn excluded paths %s: %w", ref, err)
		}
		snapshot.CDNPolicies[ref] = policy
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized cdn policies: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized cdn policies: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, COALESCE(array_to_json(paths), '[]'::json), source, event_id, status, message, created_at, completed_at
FROM cdn_invalidations`)
	if err != nil {
		return fmt.Errorf("load normalized cdn invalidations: %w", err)
	}
	for rows.Next() {
		var projectID string
		var invalidation CDNInvalidation
		var pathsPayload []byte
		var eventID sql.NullString
		var message sql.NullString
		var completedAt sql.NullTime
		if err := rows.Scan(&projectID, &invalidation.ID, &pathsPayload, &invalidation.Source, &eventID, &invalidation.Status, &message, &invalidation.CreatedAt, &completedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized cdn invalidation: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		invalidation.ProjectRef = ref
		invalidation.EventID = eventID.String
		invalidation.Message = message.String
		if completedAt.Valid {
			invalidation.CompletedAt = &completedAt.Time
		}
		if err := decodeJSON(pathsPayload, &invalidation.Paths); err != nil {
			rows.Close()
			return fmt.Errorf("decode cdn invalidation paths %s/%s: %w", ref, invalidation.ID, err)
		}
		snapshot.CDNInvalidations[ref] = append(snapshot.CDNInvalidations[ref], invalidation)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized cdn invalidations: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized cdn invalidations: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT project_id, id, name, type, provider, region, COALESCE(array_to_json(cidrs), '[]'::json), endpoint_id, config, status, message, created_at, updated_at
FROM network_connections`)
	if err != nil {
		return fmt.Errorf("load normalized network connections: %w", err)
	}
	for rows.Next() {
		var projectID string
		var connection ProjectNetworkConnection
		var region sql.NullString
		var endpointID sql.NullString
		var message sql.NullString
		var cidrsPayload []byte
		var configPayload []byte
		if err := rows.Scan(&projectID, &connection.ID, &connection.Name, &connection.Type, &connection.Provider, &region, &cidrsPayload, &endpointID, &configPayload, &connection.Status, &message, &connection.CreatedAt, &connection.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized network connection: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		connection.ProjectRef = ref
		connection.Region = region.String
		connection.EndpointID = endpointID.String
		connection.Message = message.String
		if err := decodeJSON(cidrsPayload, &connection.CIDRs); err != nil {
			rows.Close()
			return fmt.Errorf("decode network connection cidrs %s/%s: %w", ref, connection.ID, err)
		}
		if err := decodeJSON(configPayload, &connection.Config); err != nil {
			rows.Close()
			return fmt.Errorf("decode network connection config %s/%s: %w", ref, connection.ID, err)
		}
		snapshot.NetworkConnections[ref] = append(snapshot.NetworkConnections[ref], connection)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized network connections: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized network connections: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT project_id, fqdn, cert_status, cert_mode, cert_fingerprint, cert_not_after, created_at, updated_at FROM domains`)
	if err != nil {
		return fmt.Errorf("load normalized domains: %w", err)
	}
	for rows.Next() {
		var projectID string
		var domain ProjectDomain
		var fingerprint sql.NullString
		var notAfter sql.NullTime
		if err := rows.Scan(&projectID, &domain.FQDN, &domain.CertStatus, &domain.CertMode, &fingerprint, &notAfter, &domain.CreatedAt, &domain.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized domain: %w", err)
		}
		domain.CertFingerprint = fingerprint.String
		if notAfter.Valid {
			value := notAfter.Time.UTC()
			domain.CertNotAfter = &value
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		domain.ProjectRef = ref
		snapshot.Domains[ref] = append(snapshot.Domains[ref], domain)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized domains: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized domains: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT id, project_id, target, config, created_at FROM log_drains`)
	if err != nil {
		return fmt.Errorf("load normalized log drains: %w", err)
	}
	for rows.Next() {
		var projectID sql.NullString
		var drain LogDrain
		var configPayload []byte
		if err := rows.Scan(&drain.ID, &projectID, &drain.Target, &configPayload, &drain.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized log drain: %w", err)
		}
		if !projectID.Valid {
			continue
		}
		ref, ok := projectRefs[projectID.String]
		if !ok {
			continue
		}
		drain.ProjectRef = ref
		if err := decodeJSON(configPayload, &drain.Config); err != nil {
			rows.Close()
			return fmt.Errorf("decode log drain config %s/%s: %w", ref, drain.ID, err)
		}
		snapshot.LogDrains[ref] = append(snapshot.LogDrains[ref], drain)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized log drains: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized log drains: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT project_id, id, kind, encrypted_value, created_at, rotated_at FROM secrets`)
	if err != nil {
		return fmt.Errorf("load normalized secrets: %w", err)
	}
	for rows.Next() {
		var projectID string
		var secret ProjectSecret
		var value []byte
		var rotatedAt sql.NullTime
		if err := rows.Scan(&projectID, &secret.ID, &secret.Kind, &value, &secret.CreatedAt, &rotatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized secret: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		secret.ProjectRef = ref
		decryptedValue, err := s.encryption.Decrypt(value)
		if err != nil {
			rows.Close()
			return fmt.Errorf("decrypt secret %s/%s: %w", ref, secret.Kind, err)
		}
		secret.Value = string(decryptedValue)
		secret.Masked = maskSecret(secret.Value)
		secret.RotatedAt = timePtr(rotatedAt)
		if snapshot.Secrets[ref] == nil {
			snapshot.Secrets[ref] = map[string]ProjectSecret{}
		}
		snapshot.Secrets[ref][secret.Kind] = secret
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized secrets: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized secrets: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `
SELECT pb.id, source.ref, branch.ref, pb.name, pb.with_data, pb.status, pb.created_at, pb.expires_at
FROM project_branches pb
JOIN projects source ON source.id = pb.source_project_id
JOIN projects branch ON branch.id = pb.branch_project_id`)
	if err != nil {
		return fmt.Errorf("load normalized project branches: %w", err)
	}
	for rows.Next() {
		var branch ProjectBranch
		var expiresAt sql.NullTime
		if err := rows.Scan(&branch.ID, &branch.SourceProjectRef, &branch.ProjectRef, &branch.Name, &branch.WithData, &branch.Status, &branch.CreatedAt, &expiresAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized project branch: %w", err)
		}
		branch.ExpiresAt = timePtr(expiresAt)
		snapshot.Branches[branch.SourceProjectRef] = append(snapshot.Branches[branch.SourceProjectRef], branch)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized project branches: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized project branches: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT project_id, id, name, host_id, region, resource_tier, status, role, message, read_uri, read_weight, failover_priority, replication_lag_bytes, replication_lag_seconds, promoted_at, created_at, updated_at FROM project_replicas`)
	if err != nil {
		return fmt.Errorf("load normalized project replicas: %w", err)
	}
	for rows.Next() {
		var projectID string
		var replica ProjectReplica
		var hostID, region, message sql.NullString
		var promotedAt sql.NullTime
		var tier string
		if err := rows.Scan(&projectID, &replica.ID, &replica.Name, &hostID, &region, &tier, &replica.Status, &replica.Role, &message, &replica.ReadURI, &replica.ReadWeight, &replica.FailoverPriority, &replica.ReplicationLagBytes, &replica.ReplicationLagSeconds, &promotedAt, &replica.CreatedAt, &replica.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized project replica: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		replica.ProjectRef = ref
		replica.HostID = hostID.String
		replica.Region = region.String
		replica.Tier = ResourceTier(tier)
		replica.Message = message.String
		replica.PromotedAt = timePtr(promotedAt)
		snapshot.Replicas[ref] = append(snapshot.Replicas[ref], replica)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized project replicas: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized project replicas: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT project_id, enabled, schedule, kind, storage_target_id, last_run_at, next_run_at, updated_at FROM backup_policies`)
	if err != nil {
		return fmt.Errorf("load normalized backup policies: %w", err)
	}
	for rows.Next() {
		var projectID string
		var policy BackupPolicy
		var storageTargetID sql.NullString
		var lastRunAt, nextRunAt sql.NullTime
		if err := rows.Scan(&projectID, &policy.Enabled, &policy.Schedule, &policy.Kind, &storageTargetID, &lastRunAt, &nextRunAt, &policy.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized backup policy: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		policy.ProjectRef = ref
		policy.StorageTargetID = storageTargetID.String
		policy.LastRunAt = timePtr(lastRunAt)
		policy.NextRunAt = timePtr(nextRunAt)
		snapshot.Policies[ref] = policy
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized backup policies: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized backup policies: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT project_id, enabled, archive_bucket, retention_days, last_archive_at, updated_at FROM pitr_policies`)
	if err != nil {
		return fmt.Errorf("load normalized pitr policies: %w", err)
	}
	for rows.Next() {
		var projectID string
		var policy PITRPolicy
		var lastArchiveAt sql.NullTime
		if err := rows.Scan(&projectID, &policy.Enabled, &policy.ArchiveBucket, &policy.RetentionDays, &lastArchiveAt, &policy.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized pitr policy: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		policy.ProjectRef = ref
		policy.LastArchiveAt = timePtr(lastArchiveAt)
		snapshot.PITRPolicies[ref] = policy
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized pitr policies: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized pitr policies: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT project_id, id, kind, location, remote_location, storage_target_id, size_bytes, checksum_sha256, status, started_at, finished_at, created_at, verified_at FROM backups`)
	if err != nil {
		return fmt.Errorf("load normalized backups: %w", err)
	}
	for rows.Next() {
		var projectID string
		var backup Backup
		var startedAt, finishedAt, verifiedAt sql.NullTime
		if err := rows.Scan(&projectID, &backup.ID, &backup.Kind, &backup.Location, &backup.RemoteLocation, &backup.StorageTargetID, &backup.SizeBytes, &backup.ChecksumSHA256, &backup.Status, &startedAt, &finishedAt, &backup.CreatedAt, &verifiedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized backup: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		backup.ProjectRef = ref
		if startedAt.Valid {
			backup.StartedAt = startedAt.Time
		} else {
			backup.StartedAt = backup.CreatedAt
		}
		backup.FinishedAt = timePtr(finishedAt)
		backup.VerifiedAt = timePtr(verifiedAt)
		snapshot.Backups = append(snapshot.Backups, backup)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized backups: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized backups: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT project_id, id, segment, segment_source, location, remote_location, storage_target_id, size_bytes, checksum_sha256, status, created_at, verified_at FROM wal_archives`)
	if err != nil {
		return fmt.Errorf("load normalized wal archives: %w", err)
	}
	for rows.Next() {
		var projectID string
		var archive WALArchive
		var verifiedAt sql.NullTime
		if err := rows.Scan(&projectID, &archive.ID, &archive.Segment, &archive.SegmentSource, &archive.Location, &archive.RemoteLocation, &archive.StorageTargetID, &archive.SizeBytes, &archive.ChecksumSHA256, &archive.Status, &archive.CreatedAt, &verifiedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized wal archive: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		archive.ProjectRef = ref
		archive.VerifiedAt = timePtr(verifiedAt)
		snapshot.WALArchives = append(snapshot.WALArchives, archive)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized wal archives: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized wal archives: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT project_id, id, level, message, metadata, created_at FROM project_logs`)
	if err != nil {
		return fmt.Errorf("load normalized project logs: %w", err)
	}
	for rows.Next() {
		var projectID string
		var logEntry ProjectLog
		var metadataPayload []byte
		if err := rows.Scan(&projectID, &logEntry.ID, &logEntry.Level, &logEntry.Message, &metadataPayload, &logEntry.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized project log: %w", err)
		}
		ref, ok := projectRefs[projectID]
		if !ok {
			continue
		}
		logEntry.ProjectRef = ref
		if err := decodeJSON(metadataPayload, &logEntry.Metadata); err != nil {
			rows.Close()
			return fmt.Errorf("decode project log metadata %s: %w", logEntry.ID, err)
		}
		snapshot.ProjectLogs = append(snapshot.ProjectLogs, logEntry)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized project logs: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized project logs: %w", err)
	}

	rows, err = s.db.QueryContext(ctx, `SELECT id, actor_id, chain_index, previous_hash, hash, action, target, metadata, created_at FROM audit_events`)
	if err != nil {
		return fmt.Errorf("load normalized audit events: %w", err)
	}
	for rows.Next() {
		var event AuditEvent
		var actorID sql.NullString
		var metadataPayload []byte
		if err := rows.Scan(&event.ID, &actorID, &event.ChainIndex, &event.PreviousHash, &event.Hash, &event.Action, &event.Target, &metadataPayload, &event.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan normalized audit event: %w", err)
		}
		event.ActorID = actorID.String
		if err := decodeJSON(metadataPayload, &event.Metadata); err != nil {
			rows.Close()
			return fmt.Errorf("decode audit metadata %s: %w", event.ID, err)
		}
		snapshot.AuditEvents = append(snapshot.AuditEvents, event)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close normalized audit events: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate normalized audit events: %w", err)
	}

	return nil
}

func (s *PersistentStore) save(ctx context.Context) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	snapshot := s.snapshot()
	payload, err := s.encodeSnapshot(snapshot)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO control_state_checkpoints (id, state, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET state = EXCLUDED.state, updated_at = EXCLUDED.updated_at`,
		controlStateCheckpointID, payload, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("save control state checkpoint: %w", err)
	}
	if err := s.syncNormalizedTables(ctx, snapshot); err != nil {
		return err
	}
	return nil
}

func (s *PersistentStore) snapshot() memoryStoreSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return memoryStoreSnapshot{
		PlatformDefaults:      normalizedPlatformDefaults(s.platformDefaults),
		PlatformSSO:           normalizedPlatformSSOConfig(s.platformSSO),
		Users:                 cloneMap(s.users),
		Orgs:                  cloneMap(s.orgs),
		OrgQuotas:             cloneMap(s.orgQuotas),
		UsageSnapshots:        cloneUsageSnapshotMap(s.usageSnapshots),
		BillingInvoices:       cloneBillingInvoiceMap(s.billingInvoices),
		Memberships:           cloneNestedMap(s.memberships),
		Teams:                 cloneNestedMap(s.teams),
		TeamMembers:           cloneNestedMap(s.teamMembers),
		ProjectAccess:         cloneSliceMap(s.projectAccess),
		Hosts:                 cloneMap(s.hosts),
		Projects:              cloneMap(s.projects),
		Routes:                cloneSliceMap(s.routes),
		Domains:               cloneSliceMap(s.domains),
		Configs:               cloneNestedMap(s.configs),
		AuthClients:           cloneAuthClientMap(s.authClients),
		AuthHooks:             cloneAuthHookMap(s.authHooks),
		Functions:             cloneSliceMap(s.functions),
		FunctionRegions:       cloneSliceMap(s.functionRegions),
		FunctionStorageMounts: cloneSliceMap(s.functionStorageMounts),
		ReplicationPipelines:  cloneSliceMap(s.replicationPipelines),
		EmbeddingJobs:         cloneSliceMap(s.embeddingJobs),
		DatabaseExtensions:    cloneDatabaseExtensionMap(s.databaseExtensions),
		DatabaseCronJobs:      cloneDatabaseCronJobMap(s.databaseCronJobs),
		DatabaseQueues:        cloneDatabaseQueueMap(s.databaseQueues),
		DatabaseWebhooks:      cloneDatabaseWebhookMap(s.databaseWebhooks),
		DatabaseSchemas:       cloneDatabaseSchemaMap(s.databaseSchemas),
		DatabaseRoles:         cloneDatabaseRoleMap(s.databaseRoles),
		StorageBuckets:        cloneStorageBucketMap(s.storageBuckets),
		VectorBuckets:         cloneSliceMap(s.vectorBuckets),
		AnalyticsBuckets:      cloneSliceMap(s.analyticsBuckets),
		CDNPolicies:           cloneMap(s.cdnPolicies),
		CDNInvalidations:      cloneSliceMap(s.cdnInvalidations),
		NetworkConnections:    cloneSliceMap(s.networkConnections),
		Branches:              cloneSliceMap(s.branches),
		Replicas:              cloneSliceMap(s.replicas),
		LogDrains:             cloneSliceMap(s.logDrains),
		Secrets:               cloneNestedMap(s.secrets),
		BackupStorageTargets:  cloneMap(s.backupStorageTargets),
		Policies:              cloneMap(s.policies),
		PITRPolicies:          cloneMap(s.pitrPolicies),
		Backups:               append([]Backup(nil), s.backups...),
		PlatformBackups:       append([]PlatformBackup(nil), s.platformBackups...),
		WALArchives:           append([]WALArchive(nil), s.walArchives...),
		ProjectLogs:           append([]ProjectLog(nil), s.projectLogs...),
		Telemetry:             cloneMap(s.telemetry),
		TelemetryHistory:      cloneSliceMap(s.telemetryHistory),
		NodeTelemetry:         cloneMap(s.nodeTelemetry),
		AuditEvents:           append([]AuditEvent(nil), s.auditEvents...),
	}
}

func (s *PersistentStore) encodeSnapshot(snapshot memoryStoreSnapshot) ([]byte, error) {
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(snapshot); err != nil {
		return nil, fmt.Errorf("encode control state checkpoint: %w", err)
	}
	payload, err := s.encryption.Encrypt(buffer.Bytes())
	if err != nil {
		return nil, fmt.Errorf("encrypt control state checkpoint: %w", err)
	}
	return payload, nil
}

func (s *PersistentStore) ExportControlPlaneCheckpoint(ctx context.Context) ([]byte, error) {
	var payload []byte
	err := s.db.QueryRowContext(ctx, `SELECT state FROM control_state_checkpoints WHERE id = $1`, controlStateCheckpointID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		if err := s.save(ctx); err != nil {
			return nil, err
		}
		err = s.db.QueryRowContext(ctx, `SELECT state FROM control_state_checkpoints WHERE id = $1`, controlStateCheckpointID).Scan(&payload)
	}
	if err != nil {
		return nil, fmt.Errorf("export control-plane checkpoint: %w", err)
	}
	return append([]byte(nil), payload...), nil
}

func (s *PersistentStore) ImportControlPlaneCheckpoint(ctx context.Context, payload []byte, preservedPlatformBackups ...PlatformBackup) error {
	if len(payload) == 0 {
		return fmt.Errorf("control-plane checkpoint payload is empty")
	}
	plaintext, err := s.encryption.Decrypt(payload)
	if err != nil {
		return fmt.Errorf("decrypt control-plane checkpoint: %w", err)
	}
	var snapshot memoryStoreSnapshot
	if err := gob.NewDecoder(bytes.NewReader(plaintext)).Decode(&snapshot); err != nil {
		return fmt.Errorf("decode control-plane checkpoint: %w", err)
	}
	preservePlatformBackups(&snapshot, preservedPlatformBackups)
	checkpointPayload, err := s.encodeSnapshot(snapshot)
	if err != nil {
		return err
	}

	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin control-plane checkpoint import: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
INSERT INTO control_state_checkpoints (id, state, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET state = EXCLUDED.state, updated_at = EXCLUDED.updated_at`,
		controlStateCheckpointID, checkpointPayload, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("import control-plane checkpoint: %w", err)
	}
	if err := s.syncNormalizedTablesTx(ctx, tx, snapshot); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit control-plane checkpoint import: %w", err)
	}
	s.applySnapshotLocked(snapshot)
	return nil
}

func preservePlatformBackups(snapshot *memoryStoreSnapshot, backups []PlatformBackup) {
	if snapshot == nil {
		return
	}
	for _, backup := range backups {
		if strings.TrimSpace(backup.ID) == "" {
			continue
		}
		exists := false
		for _, existing := range snapshot.PlatformBackups {
			if existing.ID == backup.ID {
				exists = true
				break
			}
		}
		if !exists {
			snapshot.PlatformBackups = append(snapshot.PlatformBackups, backup)
		}
	}
}

func (s *PersistentStore) syncNormalizedTables(ctx context.Context, snapshot memoryStoreSnapshot) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin normalized meta sync: %w", err)
	}
	defer tx.Rollback()

	if err := s.syncNormalizedTablesTx(ctx, tx, snapshot); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit normalized meta sync: %w", err)
	}
	return nil
}

func (s *PersistentStore) syncNormalizedTablesTx(ctx context.Context, tx *sql.Tx, snapshot memoryStoreSnapshot) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, normalizedMetaSyncAdvisoryLock); err != nil {
		return fmt.Errorf("lock normalized meta sync: %w", err)
	}

	deleteStatements := []string{
		`DELETE FROM audit_events`,
		`DELETE FROM billing_invoices`,
		`DELETE FROM usage_snapshots`,
		`DELETE FROM wal_archives`,
		`DELETE FROM backups`,
		`DELETE FROM project_logs`,
	}
	deleteStatements = append(deleteStatements, projectChildNormalizedDeleteStatements()...)
	deleteStatements = append(deleteStatements,
		`DELETE FROM project_branches`,
		`DELETE FROM project_replicas`,
		`DELETE FROM team_members`,
		`DELETE FROM teams`,
		`DELETE FROM project_specs`,
		`DELETE FROM memberships`,
		`DELETE FROM org_quotas`,
		`DELETE FROM projects`,
		`DELETE FROM hosts`,
		`DELETE FROM users`,
		`DELETE FROM orgs`,
		`DELETE FROM platform_sso`,
		`DELETE FROM platform_defaults`,
	)
	for _, statement := range deleteStatements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("clear normalized table: %w", err)
		}
	}

	for _, org := range snapshot.Orgs {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO orgs (id, name, feature_flags, created_at)
VALUES ($1, $2, $3, $4)`, org.ID, org.Name, mustJSON(org.FeatureFlagOverrides), org.CreatedAt); err != nil {
			return fmt.Errorf("sync org %s: %w", org.ID, err)
		}
	}
	defaults := normalizedPlatformDefaults(snapshot.PlatformDefaults)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO platform_defaults (id, domain, stack_version, profile, resource_tier, backup_schedule, feature_flags, database_ingress_allowed_cidrs, smtp_enabled, smtp_host, smtp_port, smtp_sender_name, smtp_sender_email, smtp_username, smtp_password_handle, smtp_tls_mode, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, ARRAY[]::TEXT[]), $9, NULLIF($10, ''), $11, NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''), NULLIF($15, ''), $16, $17)`,
		controlStateCheckpointID,
		defaults.Domain,
		defaults.StackVersion,
		string(defaults.Profile),
		string(defaults.ResourceTier),
		defaults.BackupSchedule,
		mustJSON(defaults.FeatureFlags),
		nonNilStringSlice(defaults.DatabaseIngressAllowedCIDRs),
		defaults.SMTP.Enabled,
		defaults.SMTP.Host,
		defaults.SMTP.Port,
		defaults.SMTP.SenderName,
		defaults.SMTP.SenderEmail,
		defaults.SMTP.Username,
		defaults.SMTP.PasswordHandle,
		defaults.SMTP.TLSMode,
		defaults.UpdatedAt); err != nil {
		return fmt.Errorf("sync platform defaults: %w", err)
	}
	sso := normalizedPlatformSSOConfig(snapshot.PlatformSSO)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO platform_sso (id, enabled, provider, idp_entity_id, sso_url, certificate_pem, acs_url, metadata_url, email_domain, auto_provision, default_role, scim_enabled, scim_token_hash, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		controlStateCheckpointID, sso.Enabled, sso.Provider, sso.IDPEntityID, sso.SSOURL, nullString(sso.Certificate), sso.ACSURL, nullString(sso.MetadataURL), nullString(sso.EmailDomain), sso.AutoProvision, sso.DefaultRole, sso.SCIMEnabled, nullString(sso.SCIMTokenHash), sso.UpdatedAt); err != nil {
		return fmt.Errorf("sync platform sso: %w", err)
	}
	for _, user := range snapshot.Users {
		encryptedMFASecret, err := s.encryptOptionalString(user.MFASecret)
		if err != nil {
			return fmt.Errorf("encrypt user %s mfa secret: %w", user.ID, err)
		}
		encryptedPendingMFASecret, err := s.encryptOptionalString(user.MFAPendingSecret)
		if err != nil {
			return fmt.Errorf("encrypt user %s pending mfa secret: %w", user.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, role, mfa_enabled, mfa_secret, mfa_pending_secret, mfa_confirmed_at, mfa_updated_at, mfa_last_accepted_counter, token_version, created_at, last_login_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			user.ID, user.Email, user.PasswordHash, user.Role, user.MFAEnabled, encryptedMFASecret, encryptedPendingMFASecret, nullTime(user.MFAConfirmedAt), nullTime(user.MFAUpdatedAt), user.MFALastCounter, nextTokenVersion(user.TokenVersion-1), user.CreatedAt, nullTimePtr(user.LastLoginAt)); err != nil {
			return fmt.Errorf("sync user %s: %w", user.ID, err)
		}
	}
	for orgID, memberships := range snapshot.Memberships {
		for _, member := range memberships {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO memberships (user_id, org_id, role, created_at)
VALUES ($1, $2, $3, $4)`, member.UserID, orgID, member.Role, member.CreatedAt); err != nil {
				return fmt.Errorf("sync membership %s/%s: %w", member.UserID, orgID, err)
			}
		}
	}
	for orgID, teams := range snapshot.Teams {
		for _, team := range teams {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO teams (id, org_id, name, slug, created_at)
VALUES ($1, $2, $3, $4, $5)`, team.ID, orgID, team.Name, team.Slug, team.CreatedAt); err != nil {
				return fmt.Errorf("sync team %s/%s: %w", orgID, team.Slug, err)
			}
		}
	}
	for teamID, members := range snapshot.TeamMembers {
		for _, member := range members {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO team_members (team_id, user_id, created_at)
VALUES ($1, $2, $3)`, teamID, member.UserID, member.CreatedAt); err != nil {
				return fmt.Errorf("sync team member %s/%s: %w", teamID, member.UserID, err)
			}
		}
	}
	for orgID, quota := range snapshot.OrgQuotas {
		used, err := jsonBytes(quota.Used)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO org_quotas (org_id, max_projects, max_cpu, max_ram_mb, max_disk_gb, used, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`, orgID, quota.MaxProjects, quota.MaxCPU, quota.MaxRAMMB, quota.MaxDiskGB, used, quota.UpdatedAt); err != nil {
			return fmt.Errorf("sync org quota %s: %w", orgID, err)
		}
	}
	for orgID, snapshots := range snapshot.UsageSnapshots {
		for _, usageSnapshot := range snapshots {
			metrics, err := jsonBytes(usageSnapshot.Metrics)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO usage_snapshots (id, org_id, metrics, sampled_at)
VALUES ($1, $2, $3, $4)`, usageSnapshot.ID, orgID, metrics, usageSnapshot.SampledAt); err != nil {
				return fmt.Errorf("sync usage snapshot %s: %w", usageSnapshot.ID, err)
			}
		}
	}
	for orgID, invoices := range snapshot.BillingInvoices {
		for _, invoice := range invoices {
			lineItems, err := jsonBytes(invoice.LineItems)
			if err != nil {
				return err
			}
			metrics, err := jsonBytes(invoice.Metrics)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO billing_invoices (id, org_id, usage_snapshot_id, number, status, currency, period_start, period_end, due_at, subtotal_cents, total_cents, line_items, metrics, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`, invoice.ID, orgID, invoice.UsageSnapshotID, invoice.Number, invoice.Status, invoice.Currency, invoice.PeriodStart, invoice.PeriodEnd, invoice.DueAt, invoice.SubtotalCents, invoice.TotalCents, lineItems, metrics, invoice.CreatedAt); err != nil {
				return fmt.Errorf("sync billing invoice %s: %w", invoice.ID, err)
			}
		}
	}
	for _, host := range snapshot.Hosts {
		capacity, err := jsonBytes(host.Capacity)
		if err != nil {
			return err
		}
		used, err := jsonBytes(host.Used)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO hosts (id, name, address, capacity, used, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`, host.ID, host.Name, host.Address, capacity, used, host.CreatedAt); err != nil {
			return fmt.Errorf("sync host %s: %w", host.ID, err)
		}
	}

	projectIDs := map[string]string{}
	for _, project := range snapshot.Projects {
		projectIDs[project.Ref] = project.ID
		if _, err := tx.ExecContext(ctx, `
INSERT INTO projects (id, ref, org_id, name, host_id, status, stack_version, profile, resource_tier, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			project.ID, project.Ref, project.OrgID, project.Name, nullString(project.Spec.HostID), string(project.Status), project.Spec.StackVersion, string(project.Spec.Profile), string(project.Spec.ResourceTier), project.CreatedAt, project.UpdatedAt); err != nil {
			return fmt.Errorf("sync project %s: %w", project.Ref, err)
		}
		spec, err := jsonBytes(project.Spec)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO project_specs (project_id, desired_state, updated_at)
VALUES ($1, $2, $3)`, project.ID, spec, project.UpdatedAt); err != nil {
			return fmt.Errorf("sync project spec %s: %w", project.Ref, err)
		}
	}

	for ref, grants := range snapshot.ProjectAccess {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, grant := range grants {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO project_access_grants (id, project_id, org_id, subject_type, subject_id, subject_name, role, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, grant.ID, projectID, grant.OrgID, grant.SubjectType, grant.SubjectID, grant.SubjectName, grant.Role, grant.CreatedAt); err != nil {
				return fmt.Errorf("sync project access grant %s/%s/%s: %w", ref, grant.SubjectType, grant.SubjectID, err)
			}
		}
	}

	for ref, routes := range snapshot.Routes {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, route := range routes {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO project_routes (id, project_id, name, fqdn, upstream_url, tls, ssl_enforced, ip_allowlist, cache_control, smart_cdn, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, route.ID, projectID, route.Name, route.FQDN, route.UpstreamURL, route.TLS, route.SSLEnforced, nonNilStringSlice(route.IPAllowlist), nullString(route.CacheControl), route.SmartCDN, route.CreatedAt); err != nil {
				return fmt.Errorf("sync route %s/%s: %w", ref, route.Name, err)
			}
		}
	}
	for ref, configs := range snapshot.Configs {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, config := range configs {
			payload, err := jsonBytes(config.Config)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO project_configs (project_id, area, config, updated_at)
VALUES ($1, $2, $3, $4)`, projectID, config.Area, payload, config.UpdatedAt); err != nil {
				return fmt.Errorf("sync project config %s/%s: %w", ref, config.Area, err)
			}
		}
	}
	for ref, clients := range snapshot.AuthClients {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, client := range clients {
			redirectURIs, err := jsonBytes(client.RedirectURIs)
			if err != nil {
				return err
			}
			grantTypes, err := jsonBytes(client.GrantTypes)
			if err != nil {
				return err
			}
			scopes, err := jsonBytes(client.Scopes)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_clients (id, project_id, name, client_id, client_secret_handle, redirect_uris, grant_types, scopes, confidential, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`, client.ID, projectID, client.Name, client.ClientID, nullString(client.ClientSecretHandle), redirectURIs, grantTypes, scopes, client.Confidential, client.Status, nullString(client.Message), client.CreatedAt, client.UpdatedAt); err != nil {
				return fmt.Errorf("sync auth client %s/%s: %w", ref, client.ClientID, err)
			}
		}
	}
	for ref, hooks := range snapshot.AuthHooks {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, hook := range hooks {
			headers, err := jsonBytes(hook.Headers)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_hooks (id, project_id, hook_type, enabled, target_uri, edge_function, secret_handle, headers, timeout_ms, retry_attempts, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`, hook.ID, projectID, hook.HookType, hook.Enabled, nullString(hook.TargetURI), nullString(hook.EdgeFunction), nullString(hook.SecretHandle), headers, hook.TimeoutMS, hook.RetryAttempts, hook.Status, nullString(hook.Message), hook.CreatedAt, hook.UpdatedAt); err != nil {
				return fmt.Errorf("sync auth hook %s/%s: %w", ref, hook.HookType, err)
			}
		}
	}
	for ref, functions := range snapshot.Functions {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, function := range functions {
			secrets, err := jsonBytes(function.Secrets)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO edge_functions (id, project_id, name, version, entrypoint, verify_jwt, status, source_hash, source_bytes, secrets, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`, function.ID, projectID, function.Name, function.Version, function.Entrypoint, function.VerifyJWT, function.Status, function.SourceHash, function.SourceBytes, secrets, function.CreatedAt, function.UpdatedAt); err != nil {
				return fmt.Errorf("sync function %s/%s: %w", ref, function.Name, err)
			}
		}
	}
	for ref, regions := range snapshot.FunctionRegions {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, region := range regions {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO function_regions (id, project_id, function_name, host_id, region, routing_policy, invocation_url, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, region.ID, projectID, region.FunctionName, nullString(region.HostID), region.Region, region.RoutingPolicy, region.InvocationURL, region.Status, nullString(region.Message), region.CreatedAt, region.UpdatedAt); err != nil {
				return fmt.Errorf("sync function region %s/%s/%s: %w", ref, region.FunctionName, region.Region, err)
			}
		}
	}
	for ref, mounts := range snapshot.FunctionStorageMounts {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, mount := range mounts {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO function_storage_mounts (id, project_id, function_name, bucket_name, mount_path, read_only, prefix, env_alias, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`, mount.ID, projectID, mount.FunctionName, mount.BucketName, mount.MountPath, mount.ReadOnly, nullString(mount.Prefix), nullString(mount.EnvAlias), mount.Status, nullString(mount.Message), mount.CreatedAt, mount.UpdatedAt); err != nil {
				return fmt.Errorf("sync function storage mount %s/%s: %w", ref, mount.ID, err)
			}
		}
	}
	for ref, pipelines := range snapshot.ReplicationPipelines {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, pipeline := range pipelines {
			config, err := jsonBytes(pipeline.Config)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO replication_pipelines (id, project_id, name, type, source_schema, source_table, destination, destination_uri, credential_handle, config, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`, pipeline.ID, projectID, pipeline.Name, pipeline.Type, pipeline.SourceSchema, pipeline.SourceTable, pipeline.Destination, pipeline.DestinationURI, nullString(pipeline.CredentialHandle), config, pipeline.Status, nullString(pipeline.Message), pipeline.CreatedAt, pipeline.UpdatedAt); err != nil {
				return fmt.Errorf("sync replication pipeline %s/%s: %w", ref, pipeline.ID, err)
			}
		}
	}
	for ref, jobs := range snapshot.EmbeddingJobs {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, job := range jobs {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO embedding_jobs (id, project_id, name, source_schema, source_table, source_column, primary_key_column, destination_table, destination_column, provider, model, dimension, schedule, batch_size, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`, job.ID, projectID, job.Name, job.SourceSchema, job.SourceTable, job.SourceColumn, job.PrimaryKeyColumn, job.DestinationTable, job.DestinationColumn, job.Provider, job.Model, job.Dimension, job.Schedule, job.BatchSize, job.Status, nullString(job.Message), job.CreatedAt, job.UpdatedAt); err != nil {
				return fmt.Errorf("sync embedding job %s/%s: %w", ref, job.ID, err)
			}
		}
	}
	for ref, extensions := range snapshot.DatabaseExtensions {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, extension := range extensions {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO database_extensions (id, project_id, name, schema, version, enabled, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, extension.ID, projectID, extension.Name, extension.Schema, nullString(extension.Version), extension.Enabled, extension.Status, nullString(extension.Message), extension.CreatedAt, extension.UpdatedAt); err != nil {
				return fmt.Errorf("sync database extension %s/%s: %w", ref, extension.Name, err)
			}
		}
	}
	for ref, jobs := range snapshot.DatabaseCronJobs {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, job := range jobs {
			metadata, err := jsonBytes(job.Metadata)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO database_cron_jobs (id, project_id, name, schedule, command, database_name, username, active, timeout_seconds, max_runtime_seconds, metadata, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`, job.ID, projectID, job.Name, job.Schedule, job.Command, job.Database, job.Username, job.Active, job.TimeoutSeconds, job.MaxRuntimeSeconds, metadata, job.Status, nullString(job.Message), job.CreatedAt, job.UpdatedAt); err != nil {
				return fmt.Errorf("sync database cron job %s/%s: %w", ref, job.Name, err)
			}
		}
	}
	for ref, queues := range snapshot.DatabaseQueues {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, queue := range queues {
			metadata, err := jsonBytes(queue.Metadata)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO database_queues (id, project_id, name, schema, retention_minutes, visibility_timeout_seconds, max_retries, dead_letter_queue, active, metadata, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`, queue.ID, projectID, queue.Name, queue.Schema, queue.RetentionMinutes, queue.VisibilityTimeoutSeconds, queue.MaxRetries, nullString(queue.DeadLetterQueue), queue.Active, metadata, queue.Status, nullString(queue.Message), queue.CreatedAt, queue.UpdatedAt); err != nil {
				return fmt.Errorf("sync database queue %s/%s: %w", ref, queue.Name, err)
			}
		}
	}
	for ref, webhooks := range snapshot.DatabaseWebhooks {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, webhook := range webhooks {
			events, err := jsonBytes(webhook.Events)
			if err != nil {
				return err
			}
			headers, err := jsonBytes(webhook.Headers)
			if err != nil {
				return err
			}
			metadata, err := jsonBytes(webhook.Metadata)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO database_webhooks (id, project_id, name, schema, table_name, events, endpoint, http_method, headers, timeout_seconds, retry_count, active, metadata, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`, webhook.ID, projectID, webhook.Name, webhook.Schema, webhook.Table, events, webhook.Endpoint, webhook.HTTPMethod, headers, webhook.TimeoutSeconds, webhook.RetryCount, webhook.Active, metadata, webhook.Status, nullString(webhook.Message), webhook.CreatedAt, webhook.UpdatedAt); err != nil {
				return fmt.Errorf("sync database webhook %s/%s: %w", ref, webhook.Name, err)
			}
		}
	}
	for ref, schemas := range snapshot.DatabaseSchemas {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, schema := range schemas {
			metadata, err := jsonBytes(schema.Metadata)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO database_schemas (id, project_id, name, version, schema_name, sql, checksum, apply_order, active, metadata, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`, schema.ID, projectID, schema.Name, schema.Version, schema.Schema, schema.SQL, schema.Checksum, schema.ApplyOrder, schema.Active, metadata, schema.Status, nullString(schema.Message), schema.CreatedAt, schema.UpdatedAt); err != nil {
				return fmt.Errorf("sync database schema %s/%s@%s: %w", ref, schema.Name, schema.Version, err)
			}
		}
	}
	for ref, buckets := range snapshot.StorageBuckets {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, bucket := range buckets {
			mimeTypes, err := jsonBytes(bucket.AllowedMimeTypes)
			if err != nil {
				return err
			}
			metadata, err := jsonBytes(bucket.Metadata)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO storage_buckets (id, project_id, name, public, file_size_limit, allowed_mime_types, cache_control, avif_autodetection, metadata, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`, bucket.ID, projectID, bucket.Name, bucket.Public, bucket.FileSizeLimit, mimeTypes, bucket.CacheControl, bucket.AvifAutodetection, metadata, bucket.Status, nullString(bucket.Message), bucket.CreatedAt, bucket.UpdatedAt); err != nil {
				return fmt.Errorf("sync storage bucket %s/%s: %w", ref, bucket.Name, err)
			}
		}
	}
	for ref, roles := range snapshot.DatabaseRoles {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, role := range roles {
			memberOf, err := jsonBytes(role.MemberOf)
			if err != nil {
				return err
			}
			schemaGrants, err := jsonBytes(role.SchemaGrants)
			if err != nil {
				return err
			}
			metadata, err := jsonBytes(role.Metadata)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO database_roles (id, project_id, name, login, inherit, bypass_rls, connection_limit, password_secret_handle, member_of, schema_grants, metadata, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`, role.ID, projectID, role.Name, role.Login, role.Inherit, role.BypassRLS, role.ConnectionLimit, nullString(role.PasswordSecretHandle), memberOf, schemaGrants, metadata, role.Status, nullString(role.Message), role.CreatedAt, role.UpdatedAt); err != nil {
				return fmt.Errorf("sync database role %s/%s: %w", ref, role.Name, err)
			}
		}
	}
	for ref, buckets := range snapshot.VectorBuckets {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, bucket := range buckets {
			metadata, err := jsonBytes(bucket.Metadata)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO vector_buckets (id, project_id, name, dimension, distance, index_method, storage_backend, storage_uri, metadata, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`, bucket.ID, projectID, bucket.Name, bucket.Dimension, bucket.Distance, bucket.IndexMethod, bucket.StorageBackend, nullString(bucket.StorageURI), metadata, bucket.Status, nullString(bucket.Message), bucket.CreatedAt, bucket.UpdatedAt); err != nil {
				return fmt.Errorf("sync vector bucket %s/%s: %w", ref, bucket.Name, err)
			}
		}
	}
	for ref, buckets := range snapshot.AnalyticsBuckets {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, bucket := range buckets {
			metadata, err := jsonBytes(bucket.Metadata)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO analytics_buckets (id, project_id, name, storage_uri, catalog_uri, warehouse, credential_handle, format_version, partitioning, retention_days, compaction_schedule, metadata, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`, bucket.ID, projectID, bucket.Name, bucket.StorageURI, nullString(bucket.CatalogURI), bucket.Warehouse, nullString(bucket.CredentialHandle), bucket.FormatVersion, nullString(bucket.Partitioning), bucket.RetentionDays, bucket.CompactionSchedule, metadata, bucket.Status, nullString(bucket.Message), bucket.CreatedAt, bucket.UpdatedAt); err != nil {
				return fmt.Errorf("sync analytics bucket %s/%s: %w", ref, bucket.Name, err)
			}
		}
	}
	for ref, policy := range snapshot.CDNPolicies {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO cdn_policies (project_id, enabled, browser_ttl_seconds, edge_ttl_seconds, stale_while_revalidate_seconds, included_paths, excluded_paths, smart_revalidation, cache_control, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, projectID, policy.Enabled, policy.BrowserTTLSeconds, policy.EdgeTTLSeconds, policy.StaleWhileRevalidateSeconds, nonNilStringSlice(policy.IncludedPaths), nonNilStringSlice(policy.ExcludedPaths), policy.SmartRevalidation, policy.CacheControl, policy.UpdatedAt); err != nil {
			return fmt.Errorf("sync cdn policy %s: %w", ref, err)
		}
	}
	for ref, invalidations := range snapshot.CDNInvalidations {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, invalidation := range invalidations {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO cdn_invalidations (id, project_id, paths, source, event_id, status, message, created_at, completed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, invalidation.ID, projectID, nonNilStringSlice(invalidation.Paths), invalidation.Source, nullString(invalidation.EventID), invalidation.Status, nullString(invalidation.Message), invalidation.CreatedAt, invalidation.CompletedAt); err != nil {
				return fmt.Errorf("sync cdn invalidation %s/%s: %w", ref, invalidation.ID, err)
			}
		}
	}
	for ref, connections := range snapshot.NetworkConnections {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, connection := range connections {
			config, err := jsonBytes(connection.Config)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO network_connections (id, project_id, name, type, provider, region, cidrs, endpoint_id, config, status, message, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`, connection.ID, projectID, connection.Name, connection.Type, connection.Provider, nullString(connection.Region), nonNilStringSlice(connection.CIDRs), nullString(connection.EndpointID), config, connection.Status, nullString(connection.Message), connection.CreatedAt, connection.UpdatedAt); err != nil {
				return fmt.Errorf("sync network connection %s/%s: %w", ref, connection.ID, err)
			}
		}
	}
	for ref, domains := range snapshot.Domains {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, domain := range domains {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO domains (project_id, fqdn, cert_status, cert_mode, cert_fingerprint, cert_not_after, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, projectID, domain.FQDN, domain.CertStatus, domain.CertMode, nullString(domain.CertFingerprint), nullTimePtr(domain.CertNotAfter), domain.CreatedAt, domain.UpdatedAt); err != nil {
				return fmt.Errorf("sync domain %s/%s: %w", ref, domain.FQDN, err)
			}
		}
	}
	for ref, drains := range snapshot.LogDrains {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, drain := range drains {
			config, err := jsonBytes(drain.Config)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO log_drains (id, project_id, target, config, created_at)
VALUES ($1, $2, $3, $4, $5)`, drain.ID, projectID, drain.Target, config, drain.CreatedAt); err != nil {
				return fmt.Errorf("sync log drain %s/%s: %w", ref, drain.ID, err)
			}
		}
	}
	for ref, secrets := range snapshot.Secrets {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, secret := range secrets {
			encryptedValue, err := s.encryption.Encrypt([]byte(secret.Value))
			if err != nil {
				return fmt.Errorf("encrypt secret %s/%s: %w", ref, secret.Kind, err)
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO secrets (id, project_id, kind, encrypted_value, created_at, rotated_at)
VALUES ($1, $2, $3, $4, $5, $6)`, secret.ID, projectID, secret.Kind, encryptedValue, secret.CreatedAt, secret.RotatedAt); err != nil {
				return fmt.Errorf("sync secret %s/%s: %w", ref, secret.Kind, err)
			}
		}
	}
	for ref, branches := range snapshot.Branches {
		sourceID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, branch := range branches {
			branchProjectID, ok := projectIDs[branch.ProjectRef]
			if !ok {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO project_branches (id, source_project_id, branch_project_id, name, with_data, status, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, branch.ID, sourceID, branchProjectID, branch.Name, branch.WithData, branch.Status, branch.CreatedAt, branch.ExpiresAt); err != nil {
				return fmt.Errorf("sync branch %s/%s: %w", ref, branch.ProjectRef, err)
			}
		}
	}
	for ref, replicas := range snapshot.Replicas {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		for _, replica := range replicas {
			if replica.Role == "" {
				replica.Role = "read"
			}
			if replica.ReadWeight <= 0 {
				replica.ReadWeight = 100
			}
			if replica.FailoverPriority <= 0 {
				replica.FailoverPriority = 1
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO project_replicas (id, project_id, name, host_id, region, resource_tier, status, role, message, read_uri, read_weight, failover_priority, replication_lag_bytes, replication_lag_seconds, promoted_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`, replica.ID, projectID, replica.Name, nullString(replica.HostID), nullString(replica.Region), string(replica.Tier), replica.Status, replica.Role, nullString(replica.Message), replica.ReadURI, replica.ReadWeight, replica.FailoverPriority, replica.ReplicationLagBytes, replica.ReplicationLagSeconds, replica.PromotedAt, replica.CreatedAt, replica.UpdatedAt); err != nil {
				return fmt.Errorf("sync replica %s/%s: %w", ref, replica.Name, err)
			}
		}
	}
	for ref, policy := range snapshot.Policies {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO backup_policies (project_id, enabled, schedule, kind, storage_target_id, last_run_at, next_run_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, projectID, policy.Enabled, policy.Schedule, policy.Kind, policy.StorageTargetID, policy.LastRunAt, policy.NextRunAt, policy.UpdatedAt); err != nil {
			return fmt.Errorf("sync backup policy %s: %w", ref, err)
		}
	}
	for ref, policy := range snapshot.PITRPolicies {
		projectID, ok := projectIDs[ref]
		if !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO pitr_policies (project_id, enabled, archive_bucket, retention_days, last_archive_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)`, projectID, policy.Enabled, policy.ArchiveBucket, policy.RetentionDays, policy.LastArchiveAt, policy.UpdatedAt); err != nil {
			return fmt.Errorf("sync pitr policy %s: %w", ref, err)
		}
	}
	for _, backup := range snapshot.Backups {
		projectID, ok := projectIDs[backup.ProjectRef]
		if !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO backups (id, project_id, kind, location, remote_location, storage_target_id, size_bytes, checksum_sha256, status, started_at, finished_at, created_at, verified_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`, backup.ID, projectID, backup.Kind, backup.Location, backup.RemoteLocation, backup.StorageTargetID, backup.SizeBytes, backup.ChecksumSHA256, backup.Status, backup.StartedAt, backup.FinishedAt, backup.CreatedAt, backup.VerifiedAt); err != nil {
			return fmt.Errorf("sync backup %s: %w", backup.ID, err)
		}
	}
	for _, archive := range snapshot.WALArchives {
		projectID, ok := projectIDs[archive.ProjectRef]
		if !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO wal_archives (id, project_id, segment, segment_source, location, remote_location, storage_target_id, size_bytes, checksum_sha256, status, created_at, verified_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`, archive.ID, projectID, archive.Segment, archive.SegmentSource, archive.Location, archive.RemoteLocation, archive.StorageTargetID, archive.SizeBytes, archive.ChecksumSHA256, archive.Status, archive.CreatedAt, archive.VerifiedAt); err != nil {
			return fmt.Errorf("sync wal archive %s: %w", archive.ID, err)
		}
	}
	for _, logEntry := range snapshot.ProjectLogs {
		projectID, ok := projectIDs[logEntry.ProjectRef]
		if !ok {
			continue
		}
		metadata, err := jsonBytes(logEntry.Metadata)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO project_logs (id, project_id, level, message, metadata, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`, logEntry.ID, projectID, logEntry.Level, logEntry.Message, metadata, logEntry.CreatedAt); err != nil {
			return fmt.Errorf("sync project log %s: %w", logEntry.ID, err)
		}
	}
	for _, event := range snapshot.AuditEvents {
		metadata, err := jsonBytes(event.Metadata)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO audit_events (id, actor_id, chain_index, previous_hash, hash, action, target, metadata, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, event.ID, nullString(event.ActorID), event.ChainIndex, event.PreviousHash, event.Hash, event.Action, event.Target, metadata, event.CreatedAt); err != nil {
			return fmt.Errorf("sync audit event %s: %w", event.ID, err)
		}
	}

	return nil
}

func jsonBytes(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal normalized meta value: %w", err)
	}
	return payload, nil
}

func decodeJSON(payload []byte, target any) error {
	if len(payload) == 0 {
		return nil
	}
	return json.Unmarshal(payload, target)
}

func (s *PersistentStore) encryptOptionalString(value string) (any, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	payload, err := s.encryption.Encrypt([]byte(value))
	if err != nil {
		return nil, err
	}
	return encryptedStringPrefix + base64.RawStdEncoding.EncodeToString(payload), nil
}

func (s *PersistentStore) decryptOptionalString(value sql.NullString) (string, error) {
	if !value.Valid || value.String == "" {
		return "", nil
	}
	if !strings.HasPrefix(value.String, encryptedStringPrefix) {
		return value.String, nil
	}
	encoded := strings.TrimPrefix(value.String, encryptedStringPrefix)
	payload, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	plaintext, err := s.encryption.Decrypt(payload)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nonNilStringSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nullTimePtr(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC()
}

func timePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

func (s *PersistentStore) checkpoint(ctx context.Context, err error) error {
	if err != nil {
		return err
	}
	return s.save(ctx)
}

func (s *PersistentStore) CreateUser(ctx context.Context, req CreateUserRequest) (User, error) {
	user, err := s.MemoryStore.CreateUser(ctx, req)
	return user, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateUser(ctx context.Context, id string, req UpdateUserRequest) (User, error) {
	user, err := s.MemoryStore.UpdateUser(ctx, id, req)
	return user, s.checkpoint(ctx, err)
}

func (s *PersistentStore) AuthenticateUser(ctx context.Context, email string, password string) (User, error) {
	user, err := s.MemoryStore.AuthenticateUser(ctx, email, password)
	return user, s.checkpoint(ctx, err)
}

func (s *PersistentStore) RecordUserLogin(ctx context.Context, userID string) (time.Time, error) {
	at, err := s.MemoryStore.RecordUserLogin(ctx, userID)
	return at, s.checkpoint(ctx, err)
}

func (s *PersistentStore) VerifyUserMFA(ctx context.Context, userID string, code string) (User, error) {
	user, err := s.MemoryStore.VerifyUserMFA(ctx, userID, code)
	return user, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteUser(ctx context.Context, id string) error {
	err := s.MemoryStore.DeleteUser(ctx, id)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) BeginUserMFAEnrollment(ctx context.Context, userID string) (MFAEnrollment, error) {
	enrollment, err := s.MemoryStore.BeginUserMFAEnrollment(ctx, userID)
	return enrollment, s.checkpoint(ctx, err)
}

func (s *PersistentStore) ConfirmUserMFA(ctx context.Context, userID string, code string) (MFAStatus, error) {
	status, err := s.MemoryStore.ConfirmUserMFA(ctx, userID, code)
	return status, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DisableUserMFA(ctx context.Context, userID string, code string) (MFAStatus, error) {
	status, err := s.MemoryStore.DisableUserMFA(ctx, userID, code)
	return status, s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateOrg(ctx context.Context, name string) (Org, error) {
	org, err := s.MemoryStore.CreateOrg(ctx, name)
	return org, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateOrg(ctx context.Context, id string, name string) (Org, error) {
	org, err := s.MemoryStore.UpdateOrg(ctx, id, name)
	return org, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteOrg(ctx context.Context, id string) error {
	err := s.MemoryStore.DeleteOrg(ctx, id)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateOrgQuota(ctx context.Context, orgID string, input OrgQuotaInput) (OrgQuota, error) {
	quota, err := s.MemoryStore.UpdateOrgQuota(ctx, orgID, input)
	return quota, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateOrgFeatureFlags(ctx context.Context, orgID string, input OrgFeatureFlagsInput) (OrgFeatureFlags, error) {
	flags, err := s.MemoryStore.UpdateOrgFeatureFlags(ctx, orgID, input)
	return flags, s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateOrgUsageSnapshot(ctx context.Context, orgID string) (UsageSnapshot, error) {
	snapshot, err := s.MemoryStore.CreateOrgUsageSnapshot(ctx, orgID)
	return snapshot, s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateBillingInvoice(ctx context.Context, orgID string, input BillingInvoiceInput) (BillingInvoice, error) {
	invoice, err := s.MemoryStore.CreateBillingInvoice(ctx, orgID, input)
	return invoice, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdatePlatformDefaults(ctx context.Context, input PlatformDefaultsInput) (PlatformDefaults, error) {
	defaults, err := s.MemoryStore.UpdatePlatformDefaults(ctx, input)
	return defaults, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdatePlatformSSOConfig(ctx context.Context, input PlatformSSOConfigInput) (PlatformSSOConfig, error) {
	config, err := s.MemoryStore.UpdatePlatformSSOConfig(ctx, input)
	return config, s.checkpoint(ctx, err)
}

func mustJSON(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(payload)
}

func (s *PersistentStore) UpsertOrgMember(ctx context.Context, orgID string, input MembershipInput) (Membership, error) {
	member, err := s.MemoryStore.UpsertOrgMember(ctx, orgID, input)
	return member, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteOrgMember(ctx context.Context, orgID string, email string) error {
	err := s.MemoryStore.DeleteOrgMember(ctx, orgID, email)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateOrgTeam(ctx context.Context, orgID string, input TeamInput) (Team, error) {
	team, err := s.MemoryStore.CreateOrgTeam(ctx, orgID, input)
	return team, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteOrgTeam(ctx context.Context, orgID string, slug string) error {
	err := s.MemoryStore.DeleteOrgTeam(ctx, orgID, slug)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpsertTeamMember(ctx context.Context, orgID string, slug string, input TeamMemberInput) (TeamMember, error) {
	member, err := s.MemoryStore.UpsertTeamMember(ctx, orgID, slug, input)
	return member, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteTeamMember(ctx context.Context, orgID string, slug string, email string) error {
	err := s.MemoryStore.DeleteTeamMember(ctx, orgID, slug, email)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpsertProjectAccess(ctx context.Context, ref string, input ProjectAccessInput) (ProjectAccessGrant, error) {
	grant, err := s.MemoryStore.UpsertProjectAccess(ctx, ref, input)
	return grant, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectAccess(ctx context.Context, ref string, subjectType string, subjectID string) error {
	err := s.MemoryStore.DeleteProjectAccess(ctx, ref, subjectType, subjectID)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateHost(ctx context.Context, req CreateHostRequest) (Host, error) {
	host, err := s.MemoryStore.CreateHost(ctx, req)
	return host, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteHost(ctx context.Context, id string) error {
	err := s.MemoryStore.DeleteHost(ctx, id)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProject(ctx context.Context, req CreateProjectRequest) (Project, error) {
	project, err := s.MemoryStore.CreateProject(ctx, req)
	return project, s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectBranch(ctx context.Context, sourceRef string, input ProjectBranchInput) (ProjectBranch, Project, error) {
	branch, project, err := s.MemoryStore.CreateProjectBranch(ctx, sourceRef, input)
	return branch, project, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectBranch(ctx context.Context, sourceRef string, branchRef string) error {
	err := s.MemoryStore.DeleteProjectBranch(ctx, sourceRef, branchRef)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectReplica(ctx context.Context, ref string, input ProjectReplicaInput) (ProjectReplica, error) {
	replica, err := s.MemoryStore.CreateProjectReplica(ctx, ref, input)
	return replica, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateProjectReplicaStatus(ctx context.Context, ref string, replicaID string, status string, message string) (ProjectReplica, error) {
	replica, err := s.MemoryStore.UpdateProjectReplicaStatus(ctx, ref, replicaID, status, message)
	return replica, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectReplica(ctx context.Context, ref string, replicaID string) error {
	err := s.MemoryStore.DeleteProjectReplica(ctx, ref, replicaID)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) PromoteProjectReplica(ctx context.Context, ref string, replicaID string, reason string) (ProjectReplica, error) {
	replica, err := s.MemoryStore.PromoteProjectReplica(ctx, ref, replicaID, reason)
	return replica, s.checkpoint(ctx, err)
}

func (s *PersistentStore) FailoverProjectReplica(ctx context.Context, ref string, reason string) (ProjectReplica, error) {
	replica, err := s.MemoryStore.FailoverProjectReplica(ctx, ref, reason)
	return replica, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateProjectStatus(ctx context.Context, ref string, status ProjectPhase, message string) (Project, error) {
	project, err := s.MemoryStore.UpdateProjectStatus(ctx, ref, status, message)
	return project, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateProjectStackVersion(ctx context.Context, ref string, version string) (Project, error) {
	project, err := s.MemoryStore.UpdateProjectStackVersion(ctx, ref, version)
	return project, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateProjectResourceTier(ctx context.Context, ref string, tier ResourceTier) (Project, error) {
	project, err := s.MemoryStore.UpdateProjectResourceTier(ctx, ref, tier)
	return project, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateProjectResources(ctx context.Context, ref string, input ProjectResourcesInput) (Project, error) {
	project, err := s.MemoryStore.UpdateProjectResources(ctx, ref, input)
	return project, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateProjectServices(ctx context.Context, ref string, input ProjectServicesInput) (Project, error) {
	project, err := s.MemoryStore.UpdateProjectServices(ctx, ref, input)
	return project, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProject(ctx context.Context, ref string) error {
	err := s.MemoryStore.DeleteProject(ctx, ref)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpsertProjectRoutes(ctx context.Context, ref string, routes []ProjectRoute) ([]ProjectRoute, error) {
	stored, err := s.MemoryStore.UpsertProjectRoutes(ctx, ref, routes)
	return stored, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectRoutes(ctx context.Context, ref string) error {
	err := s.MemoryStore.DeleteProjectRoutes(ctx, ref)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) AddProjectDomain(ctx context.Context, ref string, input ProjectDomainInput) (ProjectDomain, error) {
	domain, err := s.MemoryStore.AddProjectDomain(ctx, ref, input)
	return domain, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateProjectDomainCertStatus(ctx context.Context, ref string, fqdn string, status string) (ProjectDomain, error) {
	domain, err := s.MemoryStore.UpdateProjectDomainCertStatus(ctx, ref, fqdn, status)
	return domain, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateProjectDomainCertificate(ctx context.Context, ref string, fqdn string, metadata ProjectDomainCertificateMetadata) (ProjectDomain, error) {
	domain, err := s.MemoryStore.UpdateProjectDomainCertificate(ctx, ref, fqdn, metadata)
	return domain, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectDomain(ctx context.Context, ref string, fqdn string) error {
	err := s.MemoryStore.DeleteProjectDomain(ctx, ref, fqdn)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateProjectConfig(ctx context.Context, ref string, area string, input ProjectConfigInput) (ProjectConfig, error) {
	config, err := s.MemoryStore.UpdateProjectConfig(ctx, ref, area, input)
	return config, s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectAuthClient(ctx context.Context, ref string, input ProjectAuthClientInput) (ProjectAuthClient, error) {
	client, err := s.MemoryStore.CreateProjectAuthClient(ctx, ref, input)
	return client, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectAuthClient(ctx context.Context, ref string, clientID string) error {
	err := s.MemoryStore.DeleteProjectAuthClient(ctx, ref, clientID)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectAuthHook(ctx context.Context, ref string, input ProjectAuthHookInput) (ProjectAuthHook, error) {
	hook, err := s.MemoryStore.CreateProjectAuthHook(ctx, ref, input)
	return hook, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectAuthHook(ctx context.Context, ref string, hookType string) error {
	err := s.MemoryStore.DeleteProjectAuthHook(ctx, ref, hookType)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeployProjectFunction(ctx context.Context, ref string, input ProjectFunctionInput) (ProjectFunction, error) {
	function, err := s.MemoryStore.DeployProjectFunction(ctx, ref, input)
	return function, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectFunction(ctx context.Context, ref string, name string) error {
	err := s.MemoryStore.DeleteProjectFunction(ctx, ref, name)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectFunctionRegion(ctx context.Context, ref string, input ProjectFunctionRegionInput) (ProjectFunctionRegion, error) {
	region, err := s.MemoryStore.CreateProjectFunctionRegion(ctx, ref, input)
	return region, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectFunctionRegion(ctx context.Context, ref string, id string) error {
	err := s.MemoryStore.DeleteProjectFunctionRegion(ctx, ref, id)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectFunctionStorageMount(ctx context.Context, ref string, input ProjectFunctionStorageMountInput) (ProjectFunctionStorageMount, error) {
	mount, err := s.MemoryStore.CreateProjectFunctionStorageMount(ctx, ref, input)
	return mount, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectFunctionStorageMount(ctx context.Context, ref string, id string) error {
	err := s.MemoryStore.DeleteProjectFunctionStorageMount(ctx, ref, id)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectReplicationPipeline(ctx context.Context, ref string, input ProjectReplicationPipelineInput) (ProjectReplicationPipeline, error) {
	pipeline, err := s.MemoryStore.CreateProjectReplicationPipeline(ctx, ref, input)
	return pipeline, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectReplicationPipeline(ctx context.Context, ref string, id string) error {
	err := s.MemoryStore.DeleteProjectReplicationPipeline(ctx, ref, id)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectEmbeddingJob(ctx context.Context, ref string, input ProjectEmbeddingJobInput) (ProjectEmbeddingJob, error) {
	job, err := s.MemoryStore.CreateProjectEmbeddingJob(ctx, ref, input)
	return job, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectEmbeddingJob(ctx context.Context, ref string, id string) error {
	err := s.MemoryStore.DeleteProjectEmbeddingJob(ctx, ref, id)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateProjectDatabaseExtension(ctx context.Context, ref string, name string, input ProjectDatabaseExtensionInput) (ProjectDatabaseExtension, error) {
	extension, err := s.MemoryStore.UpdateProjectDatabaseExtension(ctx, ref, name, input)
	return extension, s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectDatabaseCronJob(ctx context.Context, ref string, input ProjectDatabaseCronJobInput) (ProjectDatabaseCronJob, error) {
	job, err := s.MemoryStore.CreateProjectDatabaseCronJob(ctx, ref, input)
	return job, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectDatabaseCronJob(ctx context.Context, ref string, name string) error {
	err := s.MemoryStore.DeleteProjectDatabaseCronJob(ctx, ref, name)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectDatabaseQueue(ctx context.Context, ref string, input ProjectDatabaseQueueInput) (ProjectDatabaseQueue, error) {
	queue, err := s.MemoryStore.CreateProjectDatabaseQueue(ctx, ref, input)
	return queue, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectDatabaseQueue(ctx context.Context, ref string, name string) error {
	err := s.MemoryStore.DeleteProjectDatabaseQueue(ctx, ref, name)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectDatabaseWebhook(ctx context.Context, ref string, input ProjectDatabaseWebhookInput) (ProjectDatabaseWebhook, error) {
	webhook, err := s.MemoryStore.CreateProjectDatabaseWebhook(ctx, ref, input)
	return webhook, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectDatabaseWebhook(ctx context.Context, ref string, name string) error {
	err := s.MemoryStore.DeleteProjectDatabaseWebhook(ctx, ref, name)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectDatabaseSchema(ctx context.Context, ref string, input ProjectDatabaseSchemaInput) (ProjectDatabaseSchema, error) {
	schema, err := s.MemoryStore.CreateProjectDatabaseSchema(ctx, ref, input)
	return schema, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectDatabaseSchema(ctx context.Context, ref string, name string, version string) error {
	err := s.MemoryStore.DeleteProjectDatabaseSchema(ctx, ref, name, version)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectDatabaseRole(ctx context.Context, ref string, input ProjectDatabaseRoleInput) (ProjectDatabaseRole, error) {
	role, err := s.MemoryStore.CreateProjectDatabaseRole(ctx, ref, input)
	return role, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectDatabaseRole(ctx context.Context, ref string, name string) error {
	err := s.MemoryStore.DeleteProjectDatabaseRole(ctx, ref, name)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectStorageBucket(ctx context.Context, ref string, input ProjectStorageBucketInput) (ProjectStorageBucket, error) {
	bucket, err := s.MemoryStore.CreateProjectStorageBucket(ctx, ref, input)
	return bucket, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectStorageBucket(ctx context.Context, ref string, name string) error {
	err := s.MemoryStore.DeleteProjectStorageBucket(ctx, ref, name)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectVectorBucket(ctx context.Context, ref string, input ProjectVectorBucketInput) (ProjectVectorBucket, error) {
	bucket, err := s.MemoryStore.CreateProjectVectorBucket(ctx, ref, input)
	return bucket, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectVectorBucket(ctx context.Context, ref string, name string) error {
	err := s.MemoryStore.DeleteProjectVectorBucket(ctx, ref, name)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectAnalyticsBucket(ctx context.Context, ref string, input ProjectAnalyticsBucketInput) (ProjectAnalyticsBucket, error) {
	bucket, err := s.MemoryStore.CreateProjectAnalyticsBucket(ctx, ref, input)
	return bucket, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectAnalyticsBucket(ctx context.Context, ref string, name string) error {
	err := s.MemoryStore.DeleteProjectAnalyticsBucket(ctx, ref, name)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateProjectCDNPolicy(ctx context.Context, ref string, input ProjectCDNPolicyInput) (ProjectCDNPolicy, error) {
	policy, err := s.MemoryStore.UpdateProjectCDNPolicy(ctx, ref, input)
	return policy, s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectCDNInvalidation(ctx context.Context, ref string, input CDNInvalidationInput) (CDNInvalidation, error) {
	invalidation, err := s.MemoryStore.CreateProjectCDNInvalidation(ctx, ref, input)
	return invalidation, s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectCDNObjectEvent(ctx context.Context, ref string, input CDNObjectEventInput) (CDNInvalidation, error) {
	invalidation, err := s.MemoryStore.CreateProjectCDNObjectEvent(ctx, ref, input)
	return invalidation, s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectNetworkConnection(ctx context.Context, ref string, input ProjectNetworkConnectionInput) (ProjectNetworkConnection, error) {
	connection, err := s.MemoryStore.CreateProjectNetworkConnection(ctx, ref, input)
	return connection, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectNetworkConnection(ctx context.Context, ref string, id string) error {
	err := s.MemoryStore.DeleteProjectNetworkConnection(ctx, ref, id)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateProjectLogDrain(ctx context.Context, ref string, input LogDrainInput) (LogDrain, error) {
	drain, err := s.MemoryStore.CreateProjectLogDrain(ctx, ref, input)
	return drain, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateProjectLogDrain(ctx context.Context, ref string, id string, input LogDrainInput) (LogDrain, error) {
	drain, err := s.MemoryStore.UpdateProjectLogDrain(ctx, ref, id, input)
	return drain, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectLogDrain(ctx context.Context, ref string, id string) error {
	err := s.MemoryStore.DeleteProjectLogDrain(ctx, ref, id)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) EnsureProjectSecrets(ctx context.Context, ref string) ([]ProjectSecret, error) {
	secrets, err := s.MemoryStore.EnsureProjectSecrets(ctx, ref)
	return secrets, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpsertProjectSecret(ctx context.Context, ref string, kind string, input ProjectSecretInput) (ProjectSecret, error) {
	secret, err := s.MemoryStore.UpsertProjectSecret(ctx, ref, kind, input)
	return secret, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteProjectSecret(ctx context.Context, ref string, kind string) error {
	err := s.MemoryStore.DeleteProjectSecret(ctx, ref, kind)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) RotateProjectSecret(ctx context.Context, ref string, kind string) (ProjectSecret, error) {
	secret, err := s.MemoryStore.RotateProjectSecret(ctx, ref, kind)
	return secret, s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateBackup(ctx context.Context, input BackupInput) (Backup, error) {
	backup, err := s.MemoryStore.CreateBackup(ctx, input)
	return backup, s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreatePlatformBackup(ctx context.Context, input PlatformBackupInput) (PlatformBackup, error) {
	backup, err := s.MemoryStore.CreatePlatformBackup(ctx, input)
	return backup, s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateBackupStorageTarget(ctx context.Context, input BackupStorageTargetInput) (BackupStorageTarget, error) {
	target, err := s.MemoryStore.CreateBackupStorageTarget(ctx, input)
	return target, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateBackupStorageTarget(ctx context.Context, id string, input BackupStorageTargetInput) (BackupStorageTarget, error) {
	target, err := s.MemoryStore.UpdateBackupStorageTarget(ctx, id, input)
	return target, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateBackupStorageTargetTestResult(ctx context.Context, id string, testedAt time.Time, status string, message string) (BackupStorageTarget, error) {
	target, err := s.MemoryStore.UpdateBackupStorageTargetTestResult(ctx, id, testedAt, status, message)
	return target, s.checkpoint(ctx, err)
}

func (s *PersistentStore) DeleteBackupStorageTarget(ctx context.Context, id string) error {
	err := s.MemoryStore.DeleteBackupStorageTarget(ctx, id)
	return s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdateBackupPolicy(ctx context.Context, ref string, input BackupPolicyInput) (BackupPolicy, error) {
	policy, err := s.MemoryStore.UpdateBackupPolicy(ctx, ref, input)
	return policy, s.checkpoint(ctx, err)
}

func (s *PersistentStore) MarkBackupPolicyRun(ctx context.Context, ref string, runAt time.Time) (BackupPolicy, error) {
	policy, err := s.MemoryStore.MarkBackupPolicyRun(ctx, ref, runAt)
	return policy, s.checkpoint(ctx, err)
}

func (s *PersistentStore) UpdatePITRPolicy(ctx context.Context, ref string, input PITRPolicyInput) (PITRPolicy, error) {
	policy, err := s.MemoryStore.UpdatePITRPolicy(ctx, ref, input)
	return policy, s.checkpoint(ctx, err)
}

func (s *PersistentStore) CreateWALArchive(ctx context.Context, input WALArchiveInput) (WALArchive, error) {
	archive, err := s.MemoryStore.CreateWALArchive(ctx, input)
	return archive, s.checkpoint(ctx, err)
}

func (s *PersistentStore) RecordProjectLog(ctx context.Context, input ProjectLogInput) (ProjectLog, error) {
	logEntry, err := s.MemoryStore.RecordProjectLog(ctx, input)
	return logEntry, s.checkpoint(ctx, err)
}

func (s *PersistentStore) RecordProjectTelemetry(ctx context.Context, ref string, input TelemetrySampleInput) (TelemetrySample, error) {
	sample, err := s.MemoryStore.RecordProjectTelemetry(ctx, ref, input)
	return sample, s.checkpoint(ctx, err)
}

func (s *PersistentStore) RecordNodeTelemetry(ctx context.Context, hostID string, input NodeTelemetrySampleInput) (NodeTelemetrySample, error) {
	sample, err := s.MemoryStore.RecordNodeTelemetry(ctx, hostID, input)
	return sample, s.checkpoint(ctx, err)
}

func (s *PersistentStore) RecordAuditEvent(ctx context.Context, input AuditEventInput) (AuditEvent, error) {
	event, err := s.MemoryStore.RecordAuditEvent(ctx, input)
	return event, s.checkpoint(ctx, err)
}

func cloneMap[T any](input map[string]T) map[string]T {
	out := map[string]T{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneNestedMap[T any](input map[string]map[string]T) map[string]map[string]T {
	out := map[string]map[string]T{}
	for key, values := range input {
		out[key] = cloneMap(values)
	}
	return out
}

func cloneSliceMap[T any](input map[string][]T) map[string][]T {
	out := map[string][]T{}
	for key, values := range input {
		out[key] = append([]T(nil), values...)
	}
	return out
}

func cloneUsageSnapshotMap(input map[string][]UsageSnapshot) map[string][]UsageSnapshot {
	out := map[string][]UsageSnapshot{}
	for key, snapshots := range input {
		out[key] = cloneUsageSnapshots(snapshots)
	}
	return out
}

func cloneBillingInvoiceMap(input map[string][]BillingInvoice) map[string][]BillingInvoice {
	out := map[string][]BillingInvoice{}
	for key, invoices := range input {
		out[key] = cloneBillingInvoices(invoices)
	}
	return out
}

func cloneStorageBucketMap(input map[string][]ProjectStorageBucket) map[string][]ProjectStorageBucket {
	out := map[string][]ProjectStorageBucket{}
	for key, buckets := range input {
		out[key] = cloneStorageBuckets(buckets)
	}
	return out
}

func cloneAuthClientMap(input map[string][]ProjectAuthClient) map[string][]ProjectAuthClient {
	out := map[string][]ProjectAuthClient{}
	for key, clients := range input {
		out[key] = cloneAuthClients(clients)
	}
	return out
}

func cloneAuthHookMap(input map[string][]ProjectAuthHook) map[string][]ProjectAuthHook {
	out := map[string][]ProjectAuthHook{}
	for key, hooks := range input {
		out[key] = cloneAuthHooks(hooks)
	}
	return out
}

func cloneDatabaseExtensionMap(input map[string][]ProjectDatabaseExtension) map[string][]ProjectDatabaseExtension {
	out := map[string][]ProjectDatabaseExtension{}
	for key, extensions := range input {
		out[key] = cloneDatabaseExtensions(extensions)
	}
	return out
}

func cloneDatabaseCronJobMap(input map[string][]ProjectDatabaseCronJob) map[string][]ProjectDatabaseCronJob {
	out := map[string][]ProjectDatabaseCronJob{}
	for key, jobs := range input {
		out[key] = cloneDatabaseCronJobs(jobs)
	}
	return out
}

func cloneDatabaseQueueMap(input map[string][]ProjectDatabaseQueue) map[string][]ProjectDatabaseQueue {
	out := map[string][]ProjectDatabaseQueue{}
	for key, queues := range input {
		out[key] = cloneDatabaseQueues(queues)
	}
	return out
}

func cloneDatabaseWebhookMap(input map[string][]ProjectDatabaseWebhook) map[string][]ProjectDatabaseWebhook {
	out := map[string][]ProjectDatabaseWebhook{}
	for key, webhooks := range input {
		out[key] = cloneDatabaseWebhooks(webhooks)
	}
	return out
}

func cloneDatabaseSchemaMap(input map[string][]ProjectDatabaseSchema) map[string][]ProjectDatabaseSchema {
	out := map[string][]ProjectDatabaseSchema{}
	for key, schemas := range input {
		out[key] = cloneDatabaseSchemas(schemas)
	}
	return out
}

func cloneDatabaseRoleMap(input map[string][]ProjectDatabaseRole) map[string][]ProjectDatabaseRole {
	out := map[string][]ProjectDatabaseRole{}
	for key, roles := range input {
		out[key] = cloneDatabaseRoles(roles)
	}
	return out
}

func nonNilMap[T any](input map[string]T) map[string]T {
	if input == nil {
		return map[string]T{}
	}
	return input
}

func nonNilNestedMap[T any](input map[string]map[string]T) map[string]map[string]T {
	if input == nil {
		return map[string]map[string]T{}
	}
	for key, values := range input {
		if values == nil {
			input[key] = map[string]T{}
		}
	}
	return input
}

func nonNilSliceMap[T any](input map[string][]T) map[string][]T {
	if input == nil {
		return map[string][]T{}
	}
	return input
}
