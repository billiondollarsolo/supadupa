package control

import (
	"context"
	"time"
)

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
	UpdateProjectResources(ctx context.Context, ref string, input ProjectResourcesInput) (Project, error)
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
	GetProjectTelemetryHistory(ctx context.Context, ref string, query TelemetryHistoryQuery) (ProjectTelemetryHistory, error)
	RecordProjectTelemetry(ctx context.Context, ref string, input TelemetrySampleInput) (TelemetrySample, error)
	RecordNodeTelemetry(ctx context.Context, hostID string, input NodeTelemetrySampleInput) (NodeTelemetrySample, error)
}
