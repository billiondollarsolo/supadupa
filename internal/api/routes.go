package api

import (
	"net/http"

	"supadupa2026/internal/control"
)

type routeRegistrationConfig struct {
	store               control.Store
	auth                *control.AuthService
	provisioner         control.Provisioner
	authRequired        bool
	authLimiter         *authAttemptLimiter
	secretAccessLimiter *fixedWindowLimiter
	mfaAccessLimiter    *fixedWindowLimiter
	ssoCallbackLimiter  *fixedWindowLimiter
	studioSessions      *studioSessionStore
	ssoAssertions       *ssoAssertionReplayCache
}

type routeRegistry struct {
	mux *http.ServeMux
	routeRegistrationConfig
}

func registerAPIRoutes(mux *http.ServeMux, cfg routeRegistrationConfig) {
	routes := routeRegistry{mux: mux, routeRegistrationConfig: cfg}
	routes.registerCoreRoutes()
	routes.registerAuthRoutes()
	routes.registerAccountRoutes()
	routes.registerUserRoutes()
	routes.registerSCIMRoutes()
	routes.registerPlatformRoutes()
	routes.registerOrgRoutes()
	routes.registerProjectRoutes()
}

func (routes routeRegistry) registerCoreRoutes() {
	routes.mux.HandleFunc("GET /healthz", healthHandler)
	routes.mux.HandleFunc("GET /v1/health", healthHandler)
	routes.mux.HandleFunc("GET /metrics", prometheusMetricsHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/metrics", getFleetMetricsHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/runtime-config", getRuntimeConfigHandler(routes.provisioner))
	routes.mux.HandleFunc("GET /v1/advisor", getAdvisorFindingsHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/compliance/report", getComplianceReportHandler(routes.store))
}

func (routes routeRegistry) registerAuthRoutes() {
	routes.mux.HandleFunc("GET /v1/auth/state", authStateHandler(routes.store, routes.auth, routes.authRequired))
	routes.mux.HandleFunc("POST /v1/auth/bootstrap", bootstrapHandler(routes.store, routes.auth))
	routes.mux.HandleFunc("POST /v1/auth/login", loginHandler(routes.store, routes.auth, routes.authLimiter))
	routes.mux.HandleFunc("POST /v1/auth/logout", logoutHandler())
	routes.mux.HandleFunc("GET /v1/auth/studio/verify", studioForwardAuthHandler(routes.store, routes.auth, routes.studioSessions))
	routes.mux.HandleFunc("GET /v1/auth/sso/saml/start", startPlatformSSOHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/auth/sso/saml/callback", platformSSOCallbackHandler(routes.store, routes.auth, routes.ssoCallbackLimiter, routes.ssoAssertions))
}

func (routes routeRegistry) registerAccountRoutes() {
	routes.mux.HandleFunc("GET /v1/account/mfa", getAccountMFAHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/account/mfa/enroll", enrollAccountMFAHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/account/mfa/verify", verifyAccountMFAHandler(routes.store, routes.mfaAccessLimiter))
	routes.mux.HandleFunc("DELETE /v1/account/mfa", disableAccountMFAHandler(routes.store, routes.mfaAccessLimiter))
	routes.mux.HandleFunc("POST /v1/account/password", changeAccountPasswordHandler(routes.store))
}

func (routes routeRegistry) registerUserRoutes() {
	routes.mux.HandleFunc("GET /v1/users", listUsersHandler(routes.store, routes.auth))
	routes.mux.HandleFunc("POST /v1/users", createUserHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/users/{id}", updateUserHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/users/{id}", deleteUserHandler(routes.store))
}

func (routes routeRegistry) registerSCIMRoutes() {
	routes.mux.HandleFunc("GET /v1/scim/v2/ServiceProviderConfig", scimServiceProviderConfigHandler(routes.store, routes.auth))
	routes.mux.HandleFunc("GET /v1/scim/v2/Users", listSCIMUsersHandler(routes.store, routes.auth))
	routes.mux.HandleFunc("POST /v1/scim/v2/Users", createSCIMUserHandler(routes.store, routes.auth))
	routes.mux.HandleFunc("GET /v1/scim/v2/Users/{id}", getSCIMUserHandler(routes.store, routes.auth))
	routes.mux.HandleFunc("PUT /v1/scim/v2/Users/{id}", replaceSCIMUserHandler(routes.store, routes.auth))
	routes.mux.HandleFunc("PATCH /v1/scim/v2/Users/{id}", patchSCIMUserHandler(routes.store, routes.auth))
	routes.mux.HandleFunc("DELETE /v1/scim/v2/Users/{id}", deleteSCIMUserHandler(routes.store, routes.auth))
	routes.mux.HandleFunc("GET /v1/scim/v2/Groups", listSCIMGroupsHandler(routes.store, routes.auth))
	routes.mux.HandleFunc("POST /v1/scim/v2/Groups", createSCIMGroupHandler(routes.store, routes.auth))
	routes.mux.HandleFunc("GET /v1/scim/v2/Groups/{id}", getSCIMGroupHandler(routes.store, routes.auth))
	routes.mux.HandleFunc("DELETE /v1/scim/v2/Groups/{id}", deleteSCIMGroupHandler(routes.store, routes.auth))
}

func (routes routeRegistry) registerPlatformRoutes() {
	routes.mux.HandleFunc("GET /v1/provisioner", provisionerHandler(routes.provisioner))
	routes.mux.HandleFunc("GET /v1/stack-releases", listStackReleasesHandler())
	routes.mux.HandleFunc("GET /v1/settings/defaults", getPlatformDefaultsHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/settings/defaults", updatePlatformDefaultsHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/settings/sso", getPlatformSSOHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/settings/sso", updatePlatformSSOHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/backup-storage-targets", listBackupStorageTargetsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/backup-storage-targets", createBackupStorageTargetHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/backup-storage-targets/{id}", updateBackupStorageTargetHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/backup-storage-targets/{id}/test", testBackupStorageTargetHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/backup-storage-targets/{id}", deleteBackupStorageTargetHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/platform/backups", listPlatformBackupsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/platform/backups", triggerPlatformBackupHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/platform/backups/{id}/restore", restorePlatformBackupHandler(routes.store, routes.provisioner))
	routes.mux.HandleFunc("GET /v1/audit-events", listAuditEventsHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/audit-events/integrity", getAuditIntegrityHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/hosts", listHostsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/hosts", createHostHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/hosts/{id}", getHostHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/hosts/{id}", deleteHostHandler(routes.store))
}

func (routes routeRegistry) registerOrgRoutes() {
	routes.mux.HandleFunc("GET /v1/orgs", listOrgsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/orgs", createOrgHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/orgs/{id}", getOrgHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/orgs/{id}", updateOrgHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/orgs/{id}", deleteOrgHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects", listProjectsHandler(routes.store, routes.provisioner))
	routes.mux.HandleFunc("GET /v1/orgs/{id}/features", getOrgFeatureFlagsHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/orgs/{id}/features", updateOrgFeatureFlagsHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/orgs/{id}/quotas", getOrgQuotaHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/orgs/{id}/quotas", updateOrgQuotaHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/orgs/{id}/usage", getOrgUsageHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/orgs/{id}/usage/snapshots", listOrgUsageSnapshotsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/orgs/{id}/usage/snapshots", createOrgUsageSnapshotHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/orgs/{id}/billing/invoices", listBillingInvoicesHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/orgs/{id}/billing/invoices", createBillingInvoiceHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/orgs/{id}/billing/invoices/{invoice_id}", getBillingInvoiceHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/orgs/{id}/access-review", getOrgAccessReviewHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/orgs/{id}/members", listOrgMembersHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/orgs/{id}/members", upsertOrgMemberHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/orgs/{id}/members/{email}", deleteOrgMemberHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/orgs/{id}/teams", listOrgTeamsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/orgs/{id}/teams", createOrgTeamHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/orgs/{id}/teams/{slug}", deleteOrgTeamHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/orgs/{id}/teams/{slug}/members", listTeamMembersHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/orgs/{id}/teams/{slug}/members", upsertTeamMemberHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/orgs/{id}/teams/{slug}/members/{email}", deleteTeamMemberHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/orgs/{id}/projects", listOrgProjectsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/orgs/{id}/projects", createProjectHandler(routes.store, routes.provisioner))
}

func (routes routeRegistry) registerProjectRoutes() {
	routes.registerProjectOverviewRoutes()
	routes.registerProjectAccessAndRoutingRoutes()
	routes.registerProjectConfigRoutes()
	routes.registerProjectAuthRoutes()
	routes.registerProjectBranchAndReplicaRoutes()
	routes.registerProjectFunctionRoutes()
	routes.registerProjectDataRoutes()
	routes.registerProjectStorageAndEdgeRoutes()
	routes.registerProjectSecretRoutes()
	routes.registerProjectBackupAndOpsRoutes()
}

func (routes routeRegistry) registerProjectOverviewRoutes() {
	routes.mux.HandleFunc("GET /v1/projects/{ref}", getProjectHandler(routes.store, routes.provisioner))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/metrics", getProjectMetricsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/telemetry", recordProjectTelemetryHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/connect", getConnectHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/connect/cli", getCLIProfileHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/studio-session", createProjectStudioSessionHandler(routes.store, routes.studioSessions))
}

func (routes routeRegistry) registerProjectAccessAndRoutingRoutes() {
	routes.mux.HandleFunc("GET /v1/projects/{ref}/access", listProjectAccessHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/projects/{ref}/access", upsertProjectAccessHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/access/{subjectType}/{subjectID}", deleteProjectAccessHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/routes", listProjectRoutesHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/route-manifest", getProjectRouteManifestHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/domains", listProjectDomainsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/domains", addProjectDomainHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/projects/{ref}/domains/{fqdn}/certificate", uploadProjectDomainCertificateHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/domains/{fqdn}/certificate", resetProjectDomainCertificateHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/domains/{fqdn}", deleteProjectDomainHandler(routes.store))
}

func (routes routeRegistry) registerProjectConfigRoutes() {
	routes.mux.HandleFunc("GET /v1/projects/{ref}/services", getProjectServicesHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/projects/{ref}/services", updateProjectServicesHandler(routes.store, routes.provisioner))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/config/{area}", getProjectConfigHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/projects/{ref}/config/{area}", updateProjectConfigHandler(routes.store, routes.provisioner))
}

func (routes routeRegistry) registerProjectAuthRoutes() {
	routes.mux.HandleFunc("GET /v1/projects/{ref}/auth/clients", listProjectAuthClientsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/auth/clients", createProjectAuthClientHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/auth/clients/{client_id}", deleteProjectAuthClientHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/auth/hooks", listProjectAuthHooksHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/auth/hooks", createProjectAuthHookHandler(routes.store, routes.provisioner))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/auth/hooks/{hook_type}", deleteProjectAuthHookHandler(routes.store, routes.provisioner))
}

func (routes routeRegistry) registerProjectBranchAndReplicaRoutes() {
	routes.mux.HandleFunc("GET /v1/projects/{ref}/branches", listProjectBranchesHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/branches", createProjectBranchHandler(routes.store, routes.provisioner))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/branches/{branch_ref}", deleteProjectBranchHandler(routes.store, routes.provisioner))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/replicas", listProjectReplicasHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/replicas", createProjectReplicaHandler(routes.store, routes.provisioner))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/replicas/routing", getProjectReplicaRoutingHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/replicas/failover", failoverProjectReplicaHandler(routes.store, routes.provisioner))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/replicas/{id}/promote", promoteProjectReplicaHandler(routes.store, routes.provisioner))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/replicas/{id}", deleteProjectReplicaHandler(routes.store, routes.provisioner))
}

func (routes routeRegistry) registerProjectFunctionRoutes() {
	routes.mux.HandleFunc("GET /v1/projects/{ref}/functions", listProjectFunctionsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/functions", deployProjectFunctionHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/functions/{name}", deleteProjectFunctionHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/functions/regions", listProjectFunctionRegionsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/functions/regions", createProjectFunctionRegionHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/functions/regions/{id}", deleteProjectFunctionRegionHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/functions/storage-mounts", listProjectFunctionStorageMountsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/functions/storage-mounts", createProjectFunctionStorageMountHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/functions/storage-mounts/{id}", deleteProjectFunctionStorageMountHandler(routes.store))
}

func (routes routeRegistry) registerProjectDataRoutes() {
	routes.mux.HandleFunc("GET /v1/projects/{ref}/replication", listProjectReplicationPipelinesHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/replication", createProjectReplicationPipelineHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/replication/{id}", deleteProjectReplicationPipelineHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/embeddings", listProjectEmbeddingJobsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/embeddings", createProjectEmbeddingJobHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/embeddings/{id}", deleteProjectEmbeddingJobHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/database/extensions", listProjectDatabaseExtensionsHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/projects/{ref}/database/extensions/{name}", updateProjectDatabaseExtensionHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/database/cron-jobs", listProjectDatabaseCronJobsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/database/cron-jobs", createProjectDatabaseCronJobHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/database/cron-jobs/{name}", deleteProjectDatabaseCronJobHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/database/queues", listProjectDatabaseQueuesHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/database/queues", createProjectDatabaseQueueHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/database/queues/{name}", deleteProjectDatabaseQueueHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/database/webhooks", listProjectDatabaseWebhooksHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/database/webhooks", createProjectDatabaseWebhookHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/database/webhooks/{name}", deleteProjectDatabaseWebhookHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/database/schemas", listProjectDatabaseSchemasHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/database/schemas", createProjectDatabaseSchemaHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/database/schemas/{name}/{version}", deleteProjectDatabaseSchemaHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/database/roles", listProjectDatabaseRolesHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/database/roles", createProjectDatabaseRoleHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/database/roles/{name}", deleteProjectDatabaseRoleHandler(routes.store))
}

func (routes routeRegistry) registerProjectStorageAndEdgeRoutes() {
	routes.mux.HandleFunc("GET /v1/projects/{ref}/storage/buckets", listProjectStorageBucketsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/storage/buckets", createProjectStorageBucketHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/storage/buckets/{name}", deleteProjectStorageBucketHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/vector-buckets", listProjectVectorBucketsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/vector-buckets", createProjectVectorBucketHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/vector-buckets/{name}", deleteProjectVectorBucketHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/analytics-buckets", listProjectAnalyticsBucketsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/analytics-buckets", createProjectAnalyticsBucketHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/analytics-buckets/{name}", deleteProjectAnalyticsBucketHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/cdn/policy", getProjectCDNPolicyHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/projects/{ref}/cdn/policy", updateProjectCDNPolicyHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/cdn/invalidations", listProjectCDNInvalidationsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/cdn/invalidations", createProjectCDNInvalidationHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/cdn/object-events", createProjectCDNObjectEventHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/network", getProjectNetworkHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/network-connections", listProjectNetworkConnectionsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/network-connections", createProjectNetworkConnectionHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/network-connections/{id}", deleteProjectNetworkConnectionHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/log-drains", listProjectLogDrainsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/log-drains", createProjectLogDrainHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/projects/{ref}/log-drains/{id}", updateProjectLogDrainHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/log-drains/{id}", deleteProjectLogDrainHandler(routes.store))
}

func (routes routeRegistry) registerProjectSecretRoutes() {
	routes.mux.HandleFunc("GET /v1/projects/{ref}/secrets", listProjectSecretsHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/projects/{ref}/secrets/{kind}", upsertProjectSecretHandler(routes.store))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}/secrets/{kind}", deleteProjectSecretHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/secrets/{kind}/reveal", revealProjectSecretHandler(routes.store, routes.secretAccessLimiter))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/secrets/{kind}/copy", auditProjectSecretCopyHandler(routes.store, routes.secretAccessLimiter))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/keys/rotate", rotateProjectSecretHandler(routes.store, routes.provisioner))
}

func (routes routeRegistry) registerProjectBackupAndOpsRoutes() {
	routes.mux.HandleFunc("GET /v1/projects/{ref}/backups", listBackupsHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/database/backups", listBackupsHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/backups", triggerBackupHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/restore", restoreBackupHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/database/backups/restore-pitr", restorePITRBackupHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/recoverability", getProjectRecoverabilityHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/backups/policy", getBackupPolicyHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/projects/{ref}/backups/policy", updateBackupPolicyHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/pitr/policy", getPITRPolicyHandler(routes.store))
	routes.mux.HandleFunc("PUT /v1/projects/{ref}/pitr/policy", updatePITRPolicyHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/pitr/wal", listWALArchivesHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/pitr/wal", archiveWALHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/logs", listProjectLogsHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/logs/stream", streamProjectLogsHandler(routes.store))
	routes.mux.HandleFunc("GET /v1/projects/{ref}/activity", listProjectActivityHandler(routes.store))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/pause", lifecycleHandler(routes.store, routes.provisioner, control.ProjectPaused))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/resume", lifecycleHandler(routes.store, routes.provisioner, control.ProjectHealthy))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/restart", restartHandler(routes.store, routes.provisioner))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/upgrade", upgradeProjectHandler(routes.store, routes.provisioner))
	routes.mux.HandleFunc("POST /v1/projects/{ref}/scale", scaleProjectHandler(routes.store, routes.provisioner))
	routes.mux.HandleFunc("DELETE /v1/projects/{ref}", destroyProjectHandler(routes.store, routes.provisioner))
}
