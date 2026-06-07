package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"supadupa2026/internal/control"
)

type contextKey string

const tokenClaimsKey contextKey = "token_claims"

const authCookieName = "supadupa_session"

type Config struct {
	Addr         string
	Logger       *slog.Logger
	Provisioner  control.Provisioner
	Store        control.Store
	Auth         *control.AuthService
	AuthRequired bool
	CORSOrigins  []string
}

func NewServer(cfg Config) *http.Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	store := cfg.Store
	if store == nil {
		store = control.NewMemoryStore()
	}
	auth := cfg.Auth
	if auth == nil {
		auth = control.NewAuthService(control.AuthSecretFromEnv(os.Getenv))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /v1/health", healthHandler)
	mux.HandleFunc("GET /metrics", prometheusMetricsHandler(store))
	mux.HandleFunc("GET /v1/metrics", getFleetMetricsHandler(store))
	mux.HandleFunc("GET /v1/runtime-config", getRuntimeConfigHandler(cfg.Provisioner))
	mux.HandleFunc("GET /v1/advisor", getAdvisorFindingsHandler(store))
	mux.HandleFunc("GET /v1/compliance/report", getComplianceReportHandler(store))
	mux.HandleFunc("GET /v1/auth/state", authStateHandler(store, cfg.AuthRequired))
	mux.HandleFunc("POST /v1/auth/bootstrap", bootstrapHandler(store, auth))
	mux.HandleFunc("POST /v1/auth/login", loginHandler(store, auth))
	mux.HandleFunc("POST /v1/auth/logout", logoutHandler())
	mux.HandleFunc("GET /v1/auth/studio/verify", studioForwardAuthHandler(store, auth))
	mux.HandleFunc("GET /v1/auth/sso/saml/start", startPlatformSSOHandler(store))
	mux.HandleFunc("POST /v1/auth/sso/saml/callback", platformSSOCallbackHandler(store, auth))
	mux.HandleFunc("GET /v1/account/mfa", getAccountMFAHandler(store))
	mux.HandleFunc("POST /v1/account/mfa/enroll", enrollAccountMFAHandler(store))
	mux.HandleFunc("POST /v1/account/mfa/verify", verifyAccountMFAHandler(store))
	mux.HandleFunc("DELETE /v1/account/mfa", disableAccountMFAHandler(store))
	mux.HandleFunc("GET /v1/users", listUsersHandler(store, auth))
	mux.HandleFunc("POST /v1/users", createUserHandler(store))
	mux.HandleFunc("GET /v1/scim/v2/ServiceProviderConfig", scimServiceProviderConfigHandler(store, auth))
	mux.HandleFunc("GET /v1/scim/v2/Users", listSCIMUsersHandler(store, auth))
	mux.HandleFunc("POST /v1/scim/v2/Users", createSCIMUserHandler(store, auth))
	mux.HandleFunc("GET /v1/scim/v2/Users/{id}", getSCIMUserHandler(store, auth))
	mux.HandleFunc("PUT /v1/scim/v2/Users/{id}", replaceSCIMUserHandler(store, auth))
	mux.HandleFunc("PATCH /v1/scim/v2/Users/{id}", patchSCIMUserHandler(store, auth))
	mux.HandleFunc("DELETE /v1/scim/v2/Users/{id}", deleteSCIMUserHandler(store, auth))
	mux.HandleFunc("GET /v1/scim/v2/Groups", listSCIMGroupsHandler(store, auth))
	mux.HandleFunc("POST /v1/scim/v2/Groups", createSCIMGroupHandler(store, auth))
	mux.HandleFunc("GET /v1/scim/v2/Groups/{id}", getSCIMGroupHandler(store, auth))
	mux.HandleFunc("DELETE /v1/scim/v2/Groups/{id}", deleteSCIMGroupHandler(store, auth))
	mux.HandleFunc("GET /v1/provisioner", provisionerHandler(cfg.Provisioner))
	mux.HandleFunc("GET /v1/stack-releases", listStackReleasesHandler())
	mux.HandleFunc("GET /v1/settings/defaults", getPlatformDefaultsHandler(store))
	mux.HandleFunc("PUT /v1/settings/defaults", updatePlatformDefaultsHandler(store))
	mux.HandleFunc("GET /v1/settings/sso", getPlatformSSOHandler(store))
	mux.HandleFunc("PUT /v1/settings/sso", updatePlatformSSOHandler(store))
	mux.HandleFunc("GET /v1/backup-storage-targets", listBackupStorageTargetsHandler(store))
	mux.HandleFunc("POST /v1/backup-storage-targets", createBackupStorageTargetHandler(store))
	mux.HandleFunc("PUT /v1/backup-storage-targets/{id}", updateBackupStorageTargetHandler(store))
	mux.HandleFunc("POST /v1/backup-storage-targets/{id}/test", testBackupStorageTargetHandler(store))
	mux.HandleFunc("DELETE /v1/backup-storage-targets/{id}", deleteBackupStorageTargetHandler(store))
	mux.HandleFunc("GET /v1/platform/backups", listPlatformBackupsHandler(store))
	mux.HandleFunc("POST /v1/platform/backups", triggerPlatformBackupHandler(store))
	mux.HandleFunc("POST /v1/platform/backups/{id}/restore", restorePlatformBackupHandler(store, cfg.Provisioner))
	mux.HandleFunc("GET /v1/audit-events", listAuditEventsHandler(store))
	mux.HandleFunc("GET /v1/audit-events/integrity", getAuditIntegrityHandler(store))
	mux.HandleFunc("GET /v1/hosts", listHostsHandler(store))
	mux.HandleFunc("POST /v1/hosts", createHostHandler(store))
	mux.HandleFunc("GET /v1/hosts/{id}", getHostHandler(store))
	mux.HandleFunc("DELETE /v1/hosts/{id}", deleteHostHandler(store))
	mux.HandleFunc("GET /v1/orgs", listOrgsHandler(store))
	mux.HandleFunc("POST /v1/orgs", createOrgHandler(store))
	mux.HandleFunc("GET /v1/orgs/{id}", getOrgHandler(store))
	mux.HandleFunc("PUT /v1/orgs/{id}", updateOrgHandler(store))
	mux.HandleFunc("DELETE /v1/orgs/{id}", deleteOrgHandler(store))
	mux.HandleFunc("GET /v1/projects", listProjectsHandler(store))
	mux.HandleFunc("GET /v1/orgs/{id}/features", getOrgFeatureFlagsHandler(store))
	mux.HandleFunc("PUT /v1/orgs/{id}/features", updateOrgFeatureFlagsHandler(store))
	mux.HandleFunc("GET /v1/orgs/{id}/quotas", getOrgQuotaHandler(store))
	mux.HandleFunc("PUT /v1/orgs/{id}/quotas", updateOrgQuotaHandler(store))
	mux.HandleFunc("GET /v1/orgs/{id}/usage", getOrgUsageHandler(store))
	mux.HandleFunc("GET /v1/orgs/{id}/usage/snapshots", listOrgUsageSnapshotsHandler(store))
	mux.HandleFunc("POST /v1/orgs/{id}/usage/snapshots", createOrgUsageSnapshotHandler(store))
	mux.HandleFunc("GET /v1/orgs/{id}/billing/invoices", listBillingInvoicesHandler(store))
	mux.HandleFunc("POST /v1/orgs/{id}/billing/invoices", createBillingInvoiceHandler(store))
	mux.HandleFunc("GET /v1/orgs/{id}/billing/invoices/{invoice_id}", getBillingInvoiceHandler(store))
	mux.HandleFunc("GET /v1/orgs/{id}/access-review", getOrgAccessReviewHandler(store))
	mux.HandleFunc("GET /v1/orgs/{id}/members", listOrgMembersHandler(store))
	mux.HandleFunc("POST /v1/orgs/{id}/members", upsertOrgMemberHandler(store))
	mux.HandleFunc("DELETE /v1/orgs/{id}/members/{email}", deleteOrgMemberHandler(store))
	mux.HandleFunc("GET /v1/orgs/{id}/teams", listOrgTeamsHandler(store))
	mux.HandleFunc("POST /v1/orgs/{id}/teams", createOrgTeamHandler(store))
	mux.HandleFunc("DELETE /v1/orgs/{id}/teams/{slug}", deleteOrgTeamHandler(store))
	mux.HandleFunc("GET /v1/orgs/{id}/teams/{slug}/members", listTeamMembersHandler(store))
	mux.HandleFunc("POST /v1/orgs/{id}/teams/{slug}/members", upsertTeamMemberHandler(store))
	mux.HandleFunc("DELETE /v1/orgs/{id}/teams/{slug}/members/{email}", deleteTeamMemberHandler(store))
	mux.HandleFunc("GET /v1/orgs/{id}/projects", listOrgProjectsHandler(store))
	mux.HandleFunc("POST /v1/orgs/{id}/projects", createProjectHandler(store, cfg.Provisioner))
	mux.HandleFunc("GET /v1/projects/{ref}", getProjectHandler(store, cfg.Provisioner))
	mux.HandleFunc("GET /v1/projects/{ref}/metrics", getProjectMetricsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/telemetry", recordProjectTelemetryHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/connect", getConnectHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/connect/cli", getCLIProfileHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/studio-session", createProjectStudioSessionHandler(store, auth))
	mux.HandleFunc("GET /v1/projects/{ref}/access", listProjectAccessHandler(store))
	mux.HandleFunc("PUT /v1/projects/{ref}/access", upsertProjectAccessHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/access/{subjectType}/{subjectID}", deleteProjectAccessHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/routes", listProjectRoutesHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/route-manifest", getProjectRouteManifestHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/domains", listProjectDomainsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/domains", addProjectDomainHandler(store))
	mux.HandleFunc("PUT /v1/projects/{ref}/domains/{fqdn}/certificate", uploadProjectDomainCertificateHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/domains/{fqdn}/certificate", resetProjectDomainCertificateHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/domains/{fqdn}", deleteProjectDomainHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/services", getProjectServicesHandler(store))
	mux.HandleFunc("PUT /v1/projects/{ref}/services", updateProjectServicesHandler(store, cfg.Provisioner))
	mux.HandleFunc("GET /v1/projects/{ref}/config/{area}", getProjectConfigHandler(store))
	mux.HandleFunc("PUT /v1/projects/{ref}/config/{area}", updateProjectConfigHandler(store, cfg.Provisioner))
	mux.HandleFunc("GET /v1/projects/{ref}/auth/clients", listProjectAuthClientsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/auth/clients", createProjectAuthClientHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/auth/clients/{client_id}", deleteProjectAuthClientHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/auth/hooks", listProjectAuthHooksHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/auth/hooks", createProjectAuthHookHandler(store, cfg.Provisioner))
	mux.HandleFunc("DELETE /v1/projects/{ref}/auth/hooks/{hook_type}", deleteProjectAuthHookHandler(store, cfg.Provisioner))
	mux.HandleFunc("GET /v1/projects/{ref}/branches", listProjectBranchesHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/branches", createProjectBranchHandler(store, cfg.Provisioner))
	mux.HandleFunc("DELETE /v1/projects/{ref}/branches/{branch_ref}", deleteProjectBranchHandler(store, cfg.Provisioner))
	mux.HandleFunc("GET /v1/projects/{ref}/replicas", listProjectReplicasHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/replicas", createProjectReplicaHandler(store, cfg.Provisioner))
	mux.HandleFunc("GET /v1/projects/{ref}/replicas/routing", getProjectReplicaRoutingHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/replicas/failover", failoverProjectReplicaHandler(store, cfg.Provisioner))
	mux.HandleFunc("POST /v1/projects/{ref}/replicas/{id}/promote", promoteProjectReplicaHandler(store, cfg.Provisioner))
	mux.HandleFunc("DELETE /v1/projects/{ref}/replicas/{id}", deleteProjectReplicaHandler(store, cfg.Provisioner))
	mux.HandleFunc("GET /v1/projects/{ref}/functions", listProjectFunctionsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/functions", deployProjectFunctionHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/functions/{name}", deleteProjectFunctionHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/functions/regions", listProjectFunctionRegionsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/functions/regions", createProjectFunctionRegionHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/functions/regions/{id}", deleteProjectFunctionRegionHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/functions/storage-mounts", listProjectFunctionStorageMountsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/functions/storage-mounts", createProjectFunctionStorageMountHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/functions/storage-mounts/{id}", deleteProjectFunctionStorageMountHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/replication", listProjectReplicationPipelinesHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/replication", createProjectReplicationPipelineHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/replication/{id}", deleteProjectReplicationPipelineHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/embeddings", listProjectEmbeddingJobsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/embeddings", createProjectEmbeddingJobHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/embeddings/{id}", deleteProjectEmbeddingJobHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/database/extensions", listProjectDatabaseExtensionsHandler(store))
	mux.HandleFunc("PUT /v1/projects/{ref}/database/extensions/{name}", updateProjectDatabaseExtensionHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/database/cron-jobs", listProjectDatabaseCronJobsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/database/cron-jobs", createProjectDatabaseCronJobHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/database/cron-jobs/{name}", deleteProjectDatabaseCronJobHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/database/queues", listProjectDatabaseQueuesHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/database/queues", createProjectDatabaseQueueHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/database/queues/{name}", deleteProjectDatabaseQueueHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/database/webhooks", listProjectDatabaseWebhooksHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/database/webhooks", createProjectDatabaseWebhookHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/database/webhooks/{name}", deleteProjectDatabaseWebhookHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/database/schemas", listProjectDatabaseSchemasHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/database/schemas", createProjectDatabaseSchemaHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/database/schemas/{name}/{version}", deleteProjectDatabaseSchemaHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/database/roles", listProjectDatabaseRolesHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/database/roles", createProjectDatabaseRoleHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/database/roles/{name}", deleteProjectDatabaseRoleHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/storage/buckets", listProjectStorageBucketsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/storage/buckets", createProjectStorageBucketHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/storage/buckets/{name}", deleteProjectStorageBucketHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/vector-buckets", listProjectVectorBucketsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/vector-buckets", createProjectVectorBucketHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/vector-buckets/{name}", deleteProjectVectorBucketHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/analytics-buckets", listProjectAnalyticsBucketsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/analytics-buckets", createProjectAnalyticsBucketHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/analytics-buckets/{name}", deleteProjectAnalyticsBucketHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/cdn/policy", getProjectCDNPolicyHandler(store))
	mux.HandleFunc("PUT /v1/projects/{ref}/cdn/policy", updateProjectCDNPolicyHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/cdn/invalidations", listProjectCDNInvalidationsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/cdn/invalidations", createProjectCDNInvalidationHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/cdn/object-events", createProjectCDNObjectEventHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/network", getProjectNetworkHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/network-connections", listProjectNetworkConnectionsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/network-connections", createProjectNetworkConnectionHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/network-connections/{id}", deleteProjectNetworkConnectionHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/log-drains", listProjectLogDrainsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/log-drains", createProjectLogDrainHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/log-drains/{id}", deleteProjectLogDrainHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/secrets", listProjectSecretsHandler(store))
	mux.HandleFunc("PUT /v1/projects/{ref}/secrets/{kind}", upsertProjectSecretHandler(store))
	mux.HandleFunc("DELETE /v1/projects/{ref}/secrets/{kind}", deleteProjectSecretHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/secrets/{kind}/reveal", revealProjectSecretHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/secrets/{kind}/copy", auditProjectSecretCopyHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/keys/rotate", rotateProjectSecretHandler(store, cfg.Provisioner))
	mux.HandleFunc("GET /v1/projects/{ref}/backups", listBackupsHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/database/backups", listBackupsHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/backups", triggerBackupHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/restore", restoreBackupHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/database/backups/restore-pitr", restorePITRBackupHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/recoverability", getProjectRecoverabilityHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/backups/policy", getBackupPolicyHandler(store))
	mux.HandleFunc("PUT /v1/projects/{ref}/backups/policy", updateBackupPolicyHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/pitr/policy", getPITRPolicyHandler(store))
	mux.HandleFunc("PUT /v1/projects/{ref}/pitr/policy", updatePITRPolicyHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/pitr/wal", listWALArchivesHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/pitr/wal", archiveWALHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/logs", listProjectLogsHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/logs/stream", streamProjectLogsHandler(store))
	mux.HandleFunc("GET /v1/projects/{ref}/activity", listProjectActivityHandler(store))
	mux.HandleFunc("POST /v1/projects/{ref}/pause", lifecycleHandler(store, cfg.Provisioner, control.ProjectPaused))
	mux.HandleFunc("POST /v1/projects/{ref}/resume", lifecycleHandler(store, cfg.Provisioner, control.ProjectHealthy))
	mux.HandleFunc("POST /v1/projects/{ref}/restart", restartHandler(store, cfg.Provisioner))
	mux.HandleFunc("POST /v1/projects/{ref}/upgrade", upgradeProjectHandler(store, cfg.Provisioner))
	mux.HandleFunc("POST /v1/projects/{ref}/scale", scaleProjectHandler(store, cfg.Provisioner))
	mux.HandleFunc("DELETE /v1/projects/{ref}", destroyProjectHandler(store, cfg.Provisioner))

	return &http.Server{
		Addr:    cfg.Addr,
		Handler: requestLogger(logger, withCORS(withAuth(cfg.AuthRequired, auth, mux), cfg.CORSOrigins)),
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func authStateHandler(store control.Store, authRequired bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"bootstrapped":  store.HasUsers(r.Context()),
			"auth_required": authRequired,
		}
		if config, err := store.GetPlatformSSOConfig(r.Context()); err == nil {
			response["sso_enabled"] = config.Enabled
			response["sso_provider"] = config.Provider
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func provisionerHandler(provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := "unconfigured"
		if provisioner != nil {
			name = provisioner.Name()
		}
		writeJSON(w, http.StatusOK, map[string]string{"provisioner": name})
	}
}

func getRuntimeConfigHandler(provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		name := "unconfigured"
		if provisioner != nil {
			name = provisioner.Name()
		}
		composeApply := envBoolValue(os.Getenv("SUPADUPA_COMPOSE_APPLY"))
		kubernetesApply := envBoolValue(os.Getenv("SUPADUPA_K8S_APPLY"))
		composeBackupDefaults := composeApply && !envFalseValue(os.Getenv("SUPADUPA_COMPOSE_BACKUP_DEFAULTS"))
		writeJSON(w, http.StatusOK, map[string]any{
			"provisioner": name,
			"apply": map[string]bool{
				"compose":            composeApply,
				"kubernetes":         kubernetesApply,
				"storage_data_plane": storageDataPlaneApplyEnabled(),
			},
			"backup": map[string]bool{
				"compose_defaults":           composeBackupDefaults,
				"logical_configured":         strings.TrimSpace(os.Getenv("SUPADUPA_LOGICAL_BACKUP_COMMAND")) != "" || composeBackupDefaults,
				"physical_configured":        strings.TrimSpace(os.Getenv("SUPADUPA_PHYSICAL_BACKUP_COMMAND")) != "" || composeBackupDefaults,
				"wal_archive_configured":     strings.TrimSpace(os.Getenv("SUPADUPA_WAL_ARCHIVE_COMMAND")) != "" || composeBackupDefaults,
				"logical_restore_configured": strings.TrimSpace(os.Getenv("SUPADUPA_LOGICAL_RESTORE_COMMAND")) != "" || composeBackupDefaults,
				"pitr_restore_configured":    strings.TrimSpace(os.Getenv("SUPADUPA_PITR_RESTORE_COMMAND")) != "" || composeBackupDefaults,
				"backup_dry_run":             envBoolValue(os.Getenv("SUPADUPA_BACKUP_DRY_RUN")),
				"restore_dry_run":            envBoolValue(os.Getenv("SUPADUPA_RESTORE_DRY_RUN")),
				"wal_archive_dry_run":        envBoolValue(os.Getenv("SUPADUPA_WAL_ARCHIVE_DRY_RUN")),
			},
			"recovery": map[string]bool{
				"require_recovery_ready_targets": envBoolValue(os.Getenv("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS")),
			},
			"upgrade": map[string]bool{
				"require_durable_backup": envBoolValue(os.Getenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP")),
				"failure_auto_restore":   envBoolValue(os.Getenv("SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE")),
			},
		})
	}
}

func listStackReleasesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		versions := control.SupportedStackReleaseVersionsFromEnv(os.Getenv)
		releases := make([]control.StackReleaseManifest, 0, len(versions))
		for _, version := range versions {
			if manifest, ok := control.ResolveStackReleaseManifestFromEnv(os.Getenv, version); ok {
				releases = append(releases, manifest)
			}
		}
		writeJSON(w, http.StatusOK, releases)
	}
}

func getFleetMetricsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		metrics, err := store.GetFleetMetrics(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, metrics)
	}
}

func getAdvisorFindingsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		findings, err := control.FleetAdvisorFindings(r.Context(), store)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, findings)
	}
}

func getComplianceReportHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		report, err := control.FleetComplianceReport(r.Context(), store)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, report)
	}
}

func prometheusMetricsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		metrics, err := store.GetFleetMetrics(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		projects, err := store.ListProjects(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		projectMetrics := make([]control.ProjectMetrics, 0, len(projects))
		for _, project := range projects {
			metrics, err := store.GetProjectMetrics(r.Context(), project.Ref)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			projectMetrics = append(projectMetrics, metrics)
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(renderPrometheusMetrics(metrics, projectMetrics)))
	}
}

func getPlatformDefaultsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		defaults, err := store.GetPlatformDefaults(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, defaults)
	}
}

func updatePlatformDefaultsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		var payload control.PlatformDefaultsInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		defaults, err := store.UpdatePlatformDefaults(r.Context(), payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "settings.defaults_update", "platform:defaults", map[string]string{
			"domain":          defaults.Domain,
			"stack_version":   defaults.StackVersion,
			"profile":         string(defaults.Profile),
			"resource_tier":   string(defaults.ResourceTier),
			"backup_schedule": defaults.BackupSchedule,
			"smtp_enabled":    strconv.FormatBool(defaults.SMTP.Enabled),
			"smtp_host":       defaults.SMTP.Host,
			"smtp_tls_mode":   defaults.SMTP.TLSMode,
		})
		writeJSON(w, http.StatusOK, defaults)
	}
}

func getPlatformSSOHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		config, err := store.GetPlatformSSOConfig(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, config)
	}
}

func updatePlatformSSOHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		var payload control.PlatformSSOConfigInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		config, err := store.UpdatePlatformSSOConfig(r.Context(), payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "settings.sso_update", "platform:sso", map[string]string{
			"enabled":               fmt.Sprintf("%t", config.Enabled),
			"provider":              config.Provider,
			"idp_entity":            config.IDPEntityID,
			"email_domain":          config.EmailDomain,
			"scim_enabled":          fmt.Sprintf("%t", config.SCIMEnabled),
			"scim_token_configured": fmt.Sprintf("%t", config.SCIMTokenConfigured),
		})
		writeJSON(w, http.StatusOK, config)
	}
}

func listBackupStorageTargetsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		targets, err := store.ListBackupStorageTargets(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, targets)
	}
}

func createBackupStorageTargetHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		var payload control.BackupStorageTargetInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		target, err := store.CreateBackupStorageTarget(r.Context(), payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "settings.backup_storage_target_create", "backup-storage-target:"+target.ID, backupStorageTargetAuditMetadata(target))
		writeJSON(w, http.StatusCreated, target)
	}
}

func updateBackupStorageTargetHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		var payload control.BackupStorageTargetInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		target, err := store.UpdateBackupStorageTarget(r.Context(), r.PathValue("id"), payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "settings.backup_storage_target_update", "backup-storage-target:"+target.ID, backupStorageTargetAuditMetadata(target))
		writeJSON(w, http.StatusOK, target)
	}
}

func testBackupStorageTargetHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		id := r.PathValue("id")
		target, err := store.GetBackupStorageTarget(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		testedAt := time.Now().UTC()
		status := "passed"
		message := ""
		if err := control.TestBackupStorageTarget(r.Context(), target); err != nil {
			status = "failed"
			message = err.Error()
		}
		updated, err := store.UpdateBackupStorageTargetTestResult(r.Context(), id, testedAt, status, message)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := backupStorageTargetAuditMetadata(updated)
		metadata["test_status"] = status
		if message != "" {
			metadata["test_error"] = message
		}
		control.Audit(r.Context(), store, "settings.backup_storage_target_test", "backup-storage-target:"+updated.ID, metadata)
		writeJSON(w, http.StatusOK, updated)
	}
}

func deleteBackupStorageTargetHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		id := r.PathValue("id")
		if err := store.DeleteBackupStorageTarget(r.Context(), id); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "settings.backup_storage_target_delete", "backup-storage-target:"+id, map[string]string{"id": id})
		w.WriteHeader(http.StatusNoContent)
	}
}

func backupStorageTargetAuditMetadata(target control.BackupStorageTarget) map[string]string {
	return map[string]string{
		"id":               target.ID,
		"name":             target.Name,
		"type":             target.Type,
		"endpoint":         target.Endpoint,
		"region":           target.Region,
		"bucket":           target.Bucket,
		"prefix":           target.Prefix,
		"default":          strconv.FormatBool(target.Default),
		"force_path_style": strconv.FormatBool(target.ForcePathStyle),
	}
}

func listPlatformBackupsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		backups, err := store.ListPlatformBackups(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, backups)
	}
}

func triggerPlatformBackupHandler(store control.Store) http.HandlerFunc {
	backupService := control.NewBackupService("")
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		backup, err := backupService.TriggerPlatformBackup(r.Context(), store)
		if err != nil {
			control.Audit(r.Context(), store, "platform.backup_failed", "platform:control-plane", map[string]string{"error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.Audit(r.Context(), store, "platform.backup", "platform:control-plane", map[string]string{
			"backup_id":         backup.ID,
			"storage_target_id": backup.StorageTargetID,
			"remote_location":   backup.RemoteLocation,
		})
		writeJSON(w, http.StatusCreated, backup)
	}
}

func restorePlatformBackupHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	backupService := control.NewBackupService("")
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		var payload restorePlatformBackupRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(payload.Confirm) != "restore-control-plane" {
			writeError(w, http.StatusBadRequest, `confirm must be "restore-control-plane"`)
			return
		}
		beforeProjects, err := store.ListProjects(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		backup, restore, err := backupService.RestorePlatformBackup(r.Context(), store, r.PathValue("id"))
		if err != nil {
			control.Audit(r.Context(), store, "platform.restore_failed", "platform:control-plane", map[string]string{"backup_id": r.PathValue("id"), "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		runtime := reconcilePlatformRestoreRuntime(r.Context(), store, provisioner, beforeProjects)
		state := restore.State
		if provisioner != nil && len(runtime.Errors) == 0 {
			state = "reconciled"
		}
		if len(runtime.Errors) > 0 {
			state = "metadata-restored-runtime-errors"
		}
		control.Audit(r.Context(), store, "platform.restore", "platform:control-plane", map[string]string{
			"backup_id":           backup.ID,
			"state":               state,
			"runtime_reconciled":  strconv.Itoa(runtime.Reconciled),
			"runtime_destroyed":   strconv.Itoa(runtime.Destroyed),
			"runtime_error_count": strconv.Itoa(len(runtime.Errors)),
		})
		writeJSON(w, http.StatusAccepted, restorePlatformBackupResponse{
			Backup:            backup,
			RestorePath:       restore.Path,
			RestoreState:      state,
			RuntimeReconciled: runtime.Reconciled,
			RuntimeDestroyed:  runtime.Destroyed,
			RuntimeErrors:     runtime.Errors,
		})
	}
}

type platformRuntimeRestoreSummary struct {
	Reconciled int
	Destroyed  int
	Errors     []string
}

func reconcilePlatformRestoreRuntime(ctx context.Context, store control.Store, provisioner control.Provisioner, beforeProjects []control.Project) platformRuntimeRestoreSummary {
	var summary platformRuntimeRestoreSummary
	if provisioner == nil {
		return summary
	}
	afterProjects, err := store.ListProjects(ctx)
	if err != nil {
		summary.Errors = append(summary.Errors, err.Error())
		return summary
	}
	beforeRefs := map[string]struct{}{}
	for _, project := range beforeProjects {
		beforeRefs[project.Ref] = struct{}{}
	}
	afterRefs := map[string]struct{}{}
	for _, project := range afterProjects {
		afterRefs[project.Ref] = struct{}{}
		if project.Status == control.ProjectDestroying {
			continue
		}
		if err := provisioner.Create(ctx, project.Spec); err != nil {
			message := fmt.Sprintf("reconcile restored project %s: %v", project.Ref, err)
			summary.Errors = append(summary.Errors, message)
			control.LogProject(ctx, store, project.Ref, "error", "Runtime restore reconcile failed", map[string]string{"error": err.Error()})
			control.Audit(ctx, store, "project.restore_reconcile_failed", "project:"+project.Ref, map[string]string{"error": err.Error()})
			continue
		}
		summary.Reconciled++
		control.LogProject(ctx, store, project.Ref, "info", "Runtime reconciled after control-plane restore", map[string]string{"provisioner": provisioner.Name()})
	}
	for ref := range beforeRefs {
		if _, ok := afterRefs[ref]; ok {
			continue
		}
		var err error
		if destroyer, ok := provisioner.(control.OptionedDestroyer); ok {
			err = destroyer.DestroyWithOptions(ctx, ref, control.DestroyOptions{RetainVolumes: true})
		} else {
			err = provisioner.Destroy(ctx, ref)
		}
		if err != nil {
			message := fmt.Sprintf("stop stale restored-away project %s: %v", ref, err)
			summary.Errors = append(summary.Errors, message)
			control.Audit(ctx, store, "project.restore_stale_destroy_failed", "project:"+ref, map[string]string{"error": err.Error()})
			continue
		}
		if err := cleanupProjectRuntimeArtifacts(ctx, ref); err != nil {
			message := fmt.Sprintf("cleanup stale restored-away project %s: %v", ref, err)
			summary.Errors = append(summary.Errors, message)
			control.Audit(ctx, store, "project.restore_stale_cleanup_failed", "project:"+ref, map[string]string{"error": err.Error()})
			continue
		}
		summary.Destroyed++
		control.Audit(ctx, store, "project.restore_stale_destroyed", "project:"+ref, map[string]string{"retain_volumes": "true"})
	}
	return summary
}

func cleanupProjectRuntimeArtifacts(ctx context.Context, ref string) error {
	if err := control.NewRoutingService("").RemoveProject(ref); err != nil {
		return fmt.Errorf("remove routes: %w", err)
	}
	if err := control.NewCertificateService().RemoveProject(ctx, ref); err != nil {
		return fmt.Errorf("remove certificates: %w", err)
	}
	return nil
}

type createOrgRequest struct {
	Name string `json:"name"`
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code"`
}

type authResponse struct {
	Token       string       `json:"token,omitempty"`
	MFARequired bool         `json:"mfa_required,omitempty"`
	User        control.User `json:"user"`
}

type platformSSOCallbackResponse struct {
	Token string       `json:"token"`
	User  control.User `json:"user"`
	SSO   string       `json:"sso"`
}

type mfaCodeRequest struct {
	Code string `json:"code"`
}

type upgradeProjectRequest struct {
	Version  string `json:"version"`
	BackupID string `json:"backup_id,omitempty"`
}

type upgradeProjectResponse struct {
	Project           control.Project `json:"project"`
	Backup            control.Backup  `json:"backup"`
	PreviousVersion   string          `json:"previous_version"`
	TargetVersion     string          `json:"target_version"`
	RollbackAvailable bool            `json:"rollback_available"`
}

type upgradeProjectFailureResponse struct {
	Error             string         `json:"error"`
	Backup            control.Backup `json:"backup"`
	PreviousVersion   string         `json:"previous_version"`
	TargetVersion     string         `json:"target_version"`
	RollbackAvailable bool           `json:"rollback_available"`
	RollbackAttempted bool           `json:"rollback_attempted"`
	RollbackError     string         `json:"rollback_error,omitempty"`
	RestoreAttempted  bool           `json:"restore_attempted,omitempty"`
	RestoreState      string         `json:"restore_state,omitempty"`
	RestoreError      string         `json:"restore_error,omitempty"`
}

type scaleProjectRequest struct {
	ResourceTier control.ResourceTier `json:"resource_tier"`
}

type restoreBackupRequest struct {
	BackupID string `json:"backup_id"`
}

type restoreBackupResponse struct {
	Backup       control.Backup `json:"backup"`
	RestorePath  string         `json:"restore_path"`
	RestoreState string         `json:"restore_state"`
}

type restorePITRBackupRequest struct {
	RecoveryTimeTargetUnix string `json:"recovery_time_target_unix"`
}

type restorePITRBackupResponse struct {
	ProjectRef             string    `json:"project_ref"`
	RecoveryTimeTargetUnix int64     `json:"recovery_time_target_unix"`
	RecoveryTimeTarget     time.Time `json:"recovery_time_target"`
	RestorePath            string    `json:"restore_path"`
	RestoreState           string    `json:"restore_state"`
}

type restorePITRBackupUnavailableResponse struct {
	Error          string                              `json:"error"`
	Recoverability control.ProjectRecoverabilityStatus `json:"recoverability"`
}

type restorePlatformBackupRequest struct {
	Confirm string `json:"confirm"`
}

type restorePlatformBackupResponse struct {
	Backup            control.PlatformBackup `json:"backup"`
	RestorePath       string                 `json:"restore_path"`
	RestoreState      string                 `json:"restore_state"`
	RuntimeReconciled int                    `json:"runtime_reconciled"`
	RuntimeDestroyed  int                    `json:"runtime_destroyed"`
	RuntimeErrors     []string               `json:"runtime_errors,omitempty"`
}

type projectStudioSessionResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type createBranchResponse struct {
	Branch  control.ProjectBranch `json:"branch"`
	Project control.Project       `json:"project"`
}

const (
	scimListResponseSchema = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimPatchOpSchema      = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	scimUserSchema         = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimGroupSchema        = "urn:ietf:params:scim:schemas:core:2.0:Group"
	scimUserExtension      = "urn:supadupa:params:scim:schemas:extension:User"
	scimGroupExtension     = "urn:supadupa:params:scim:schemas:extension:Group"
)

type scimListResponse struct {
	Schemas      []string `json:"schemas"`
	Total        int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    any      `json:"Resources"`
}

type scimMeta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created,omitempty"`
	Location     string    `json:"location,omitempty"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

type scimMember struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}

type scimUserExtensionResource struct {
	Role string `json:"role,omitempty"`
}

type scimGroupExtensionResource struct {
	OrgID string `json:"org_id,omitempty"`
	Slug  string `json:"slug,omitempty"`
}

type scimUserResource struct {
	Schemas     []string                  `json:"schemas"`
	ID          string                    `json:"id"`
	ExternalID  string                    `json:"externalId,omitempty"`
	UserName    string                    `json:"userName"`
	DisplayName string                    `json:"displayName,omitempty"`
	Active      bool                      `json:"active"`
	Emails      []scimEmail               `json:"emails,omitempty"`
	Meta        scimMeta                  `json:"meta"`
	Extension   scimUserExtensionResource `json:"urn:supadupa:params:scim:schemas:extension:User,omitempty"`
}

type scimUserRequest struct {
	Schemas     []string                  `json:"schemas"`
	ExternalID  string                    `json:"externalId"`
	UserName    string                    `json:"userName"`
	DisplayName string                    `json:"displayName"`
	Active      *bool                     `json:"active"`
	Emails      []scimEmail               `json:"emails"`
	Password    string                    `json:"password"`
	Extension   scimUserExtensionResource `json:"urn:supadupa:params:scim:schemas:extension:User"`
}

type scimGroupResource struct {
	Schemas     []string                   `json:"schemas"`
	ID          string                     `json:"id"`
	ExternalID  string                     `json:"externalId,omitempty"`
	DisplayName string                     `json:"displayName"`
	Members     []scimMember               `json:"members,omitempty"`
	Meta        scimMeta                   `json:"meta"`
	Extension   scimGroupExtensionResource `json:"urn:supadupa:params:scim:schemas:extension:Group,omitempty"`
}

type scimGroupRequest struct {
	Schemas     []string                   `json:"schemas"`
	ExternalID  string                     `json:"externalId"`
	DisplayName string                     `json:"displayName"`
	Members     []scimMember               `json:"members"`
	Extension   scimGroupExtensionResource `json:"urn:supadupa:params:scim:schemas:extension:Group"`
}

type scimPatchRequest struct {
	Schemas    []string        `json:"schemas"`
	Operations []scimOperation `json:"Operations"`
}

type scimOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

func bootstrapHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store.HasUsers(r.Context()) {
			writeError(w, http.StatusConflict, "admin user already exists")
			return
		}
		var payload authRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		user, err := store.CreateUser(r.Context(), control.CreateUserRequest{
			Email:    payload.Email,
			Password: payload.Password,
			Role:     "admin",
		})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		token, err := auth.Issue(user, 24*time.Hour)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setAuthCookie(w, r, token, 24*time.Hour)
		control.Audit(r.Context(), store, "user.bootstrap", "user:"+user.ID, map[string]string{"email": user.Email})
		writeJSON(w, http.StatusCreated, authResponse{Token: token, User: user})
	}
}

func loginHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload authRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		user, err := store.AuthenticateUser(r.Context(), payload.Email, payload.Password)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		if user.MFAEnabled {
			if !control.VerifyTOTPCode(user.MFASecret, payload.TOTPCode, time.Now().UTC()) {
				writeJSON(w, http.StatusAccepted, authResponse{MFARequired: true, User: user})
				return
			}
		}
		token, err := auth.Issue(user, 24*time.Hour)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setAuthCookie(w, r, token, 24*time.Hour)
		control.Audit(r.Context(), store, "user.login", "user:"+user.ID, map[string]string{"email": user.Email})
		writeJSON(w, http.StatusOK, authResponse{Token: token, User: user})
	}
}

func logoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clearAuthCookie(w, r)
		w.WriteHeader(http.StatusNoContent)
	}
}

func startPlatformSSOHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		config, err := store.GetPlatformSSOConfig(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		initiation := control.PlatformSSOInitiationForConfig(config)
		if !initiation.Enabled {
			writeError(w, http.StatusNotFound, "platform sso is disabled")
			return
		}
		writeJSON(w, http.StatusOK, initiation)
	}
}

func platformSSOCallbackHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		config, err := store.GetPlatformSSOConfig(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		var assertion control.PlatformSSOAssertion
		if err := decodeJSON(r, &assertion); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := control.ValidatePlatformSSOAssertion(config, assertion, time.Now().UTC()); err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		user, err := platformSSOUser(r.Context(), store, config, assertion)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		token, err := auth.Issue(user, 24*time.Hour)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setAuthCookie(w, r, token, 24*time.Hour)
		control.Audit(r.Context(), store, "user.sso_login", "user:"+user.ID, map[string]string{
			"email":   user.Email,
			"issuer":  assertion.Issuer,
			"name_id": assertion.NameID,
		})
		writeJSON(w, http.StatusOK, platformSSOCallbackResponse{Token: token, User: user, SSO: "saml"})
	}
}

func studioForwardAuthHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project, ok := projectFromForwardedStudioRequest(r, store)
		if !ok {
			writeError(w, http.StatusNotFound, "studio project route not found")
			return
		}
		var claims control.TokenClaims
		studioToken := studioTokenFromForwardedRequest(r)
		if studioToken != "" {
			var err error
			claims, err = auth.VerifyStudio(studioToken, project.Ref)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid supadupa studio session")
				return
			}
		} else {
			token := tokenFromRequest(r)
			if token == "" {
				writeError(w, http.StatusUnauthorized, "missing supadupa session")
				return
			}
			var err error
			claims, err = auth.Verify(token)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid supadupa session")
				return
			}
		}
		if !strings.EqualFold(claims.Role, "admin") {
			reqWithClaims := r.WithContext(context.WithValue(r.Context(), tokenClaimsKey, claims))
			if _, ok := requireProjectRole(w, reqWithClaims, store, project.Ref, roleViewer); !ok {
				return
			}
		}
		w.Header().Set("X-Supadupa-User", claims.Email)
		w.Header().Set("X-Supadupa-Project", project.Ref)
		w.WriteHeader(http.StatusNoContent)
	}
}

func studioTokenFromForwardedRequest(r *http.Request) string {
	for _, raw := range []string{r.URL.RawQuery, strings.TrimSpace(r.Header.Get("X-Forwarded-Uri"))} {
		if raw == "" {
			continue
		}
		query := raw
		if strings.Contains(raw, "?") {
			_, query, _ = strings.Cut(raw, "?")
		}
		values, err := url.ParseQuery(query)
		if err != nil {
			continue
		}
		if token := strings.TrimSpace(values.Get("supadupa_studio_token")); token != "" {
			return token
		}
		if token := strings.TrimSpace(values.Get("studio_token")); token != "" {
			return token
		}
	}
	return ""
}

func projectFromForwardedStudioRequest(r *http.Request, store control.Store) (control.Project, bool) {
	if ref := strings.TrimSpace(r.URL.Query().Get("project_ref")); ref != "" {
		project, err := store.GetProject(r.Context(), ref)
		return project, err == nil
	}
	host := forwardedHost(r)
	uri := r.Header.Get("X-Forwarded-Uri")
	projects, err := store.ListProjects(r.Context())
	if err != nil {
		return control.Project{}, false
	}
	for _, project := range projects {
		if strings.EqualFold(host, fmt.Sprintf("studio-%s.%s", project.Ref, project.Spec.Domain)) {
			return project, true
		}
		if strings.HasPrefix(uri, "/projects/"+project.Ref+"/studio") {
			return project, true
		}
	}
	return control.Project{}, false
}

func forwardedHost(r *http.Request) string {
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	return strings.ToLower(strings.Split(host, ":")[0])
}

func getAccountMFAHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := userIDFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing token claims")
			return
		}
		status, err := store.GetUserMFAStatus(r.Context(), userID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func enrollAccountMFAHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := userIDFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing token claims")
			return
		}
		enrollment, err := store.BeginUserMFAEnrollment(r.Context(), userID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "user.mfa_enroll", "user:"+userID, map[string]string{"email": enrollment.Email})
		writeJSON(w, http.StatusCreated, enrollment)
	}
}

func verifyAccountMFAHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := userIDFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing token claims")
			return
		}
		var payload mfaCodeRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		status, err := store.ConfirmUserMFA(r.Context(), userID, payload.Code)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "user.mfa_verify", "user:"+userID, map[string]string{"email": status.Email})
		writeJSON(w, http.StatusOK, status)
	}
}

func disableAccountMFAHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := userIDFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing token claims")
			return
		}
		var payload mfaCodeRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		status, err := store.DisableUserMFA(r.Context(), userID, payload.Code)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "user.mfa_disable", "user:"+userID, map[string]string{"email": status.Email})
		writeJSON(w, http.StatusOK, status)
	}
}

func createUserHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		var payload control.CreateUserRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		user, err := store.CreateUser(r.Context(), payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "user.create", "user:"+user.ID, map[string]string{"email": user.Email, "role": user.Role})
		writeJSON(w, http.StatusCreated, user)
	}
}

func listUsersHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		users, err := store.ListUsers(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, users)
	}
}

func scimServiceProviderConfigHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"schemas":        []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
			"patch":          map[string]bool{"supported": true},
			"bulk":           map[string]bool{"supported": false},
			"filter":         map[string]any{"supported": false},
			"changePassword": map[string]bool{"supported": false},
			"sort":           map[string]bool{"supported": false},
			"etag":           map[string]bool{"supported": false},
			"authenticationSchemes": []map[string]string{
				{"type": "oauthbearertoken", "name": "Bearer token"},
			},
		})
	}
}

func listSCIMUsersHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		users, err := store.ListUsers(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		resources := make([]scimUserResource, 0, len(users))
		for _, user := range users {
			resources = append(resources, scimUserFromControl(r, user))
		}
		writeJSON(w, http.StatusOK, scimList(resources))
	}
}

func createSCIMUserHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		var payload scimUserRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		email := scimEmailFromUserRequest(payload)
		if email == "" {
			writeError(w, http.StatusBadRequest, "SCIM userName or primary email is required")
			return
		}
		password := payload.Password
		if password == "" {
			password = fmt.Sprintf("scim-%d", time.Now().UTC().UnixNano())
		}
		role := strings.TrimSpace(payload.Extension.Role)
		if role == "" {
			role = "member"
		}
		user, err := store.CreateUser(r.Context(), control.CreateUserRequest{Email: email, Password: password, Role: role})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if payload.Active != nil && !*payload.Active {
			if err := store.DeleteUser(r.Context(), user.ID); err != nil {
				writeStoreError(w, err)
				return
			}
			control.Audit(r.Context(), store, "scim.user_deprovision", "user:"+user.ID, map[string]string{"email": user.Email})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		control.Audit(r.Context(), store, "scim.user_create", "user:"+user.ID, map[string]string{"email": user.Email, "role": user.Role})
		writeJSON(w, http.StatusCreated, scimUserFromControl(r, user))
	}
}

func getSCIMUserHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		user, err := store.GetUserByID(r.Context(), r.PathValue("id"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, scimUserFromControl(r, user))
	}
}

func replaceSCIMUserHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		var payload scimUserRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		id := r.PathValue("id")
		if payload.Active != nil && !*payload.Active {
			user, err := store.GetUserByID(r.Context(), id)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			if err := store.DeleteUser(r.Context(), id); err != nil {
				writeStoreError(w, err)
				return
			}
			control.Audit(r.Context(), store, "scim.user_deprovision", "user:"+id, map[string]string{"email": user.Email})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		email := scimEmailFromUserRequest(payload)
		if email == "" {
			writeError(w, http.StatusBadRequest, "SCIM userName or primary email is required")
			return
		}
		role := strings.TrimSpace(payload.Extension.Role)
		user, err := store.UpdateUser(r.Context(), id, control.UpdateUserRequest{Email: email, Password: payload.Password, Role: role})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "scim.user_replace", "user:"+user.ID, map[string]string{"email": user.Email, "role": user.Role})
		writeJSON(w, http.StatusOK, scimUserFromControl(r, user))
	}
}

func patchSCIMUserHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		id := r.PathValue("id")
		user, err := store.GetUserByID(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		var payload scimPatchRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		nextEmail := user.Email
		nextRole := user.Role
		changed := false
		for _, operation := range payload.Operations {
			if operation.Op != "" && !strings.EqualFold(operation.Op, "replace") {
				writeError(w, http.StatusBadRequest, "only SCIM replace operations are supported")
				return
			}
			path := strings.ToLower(strings.TrimSpace(operation.Path))
			if path == "active" && !scimBoolValue(operation.Value, true) {
				if err := store.DeleteUser(r.Context(), id); err != nil {
					writeStoreError(w, err)
					return
				}
				control.Audit(r.Context(), store, "scim.user_deprovision", "user:"+id, map[string]string{"email": user.Email})
				w.WriteHeader(http.StatusNoContent)
				return
			}
			switch path {
			case "username", "userName":
				email := strings.ToLower(strings.TrimSpace(scimStringValue(operation.Value)))
				if email == "" {
					writeError(w, http.StatusBadRequest, "SCIM userName value is required")
					return
				}
				nextEmail = email
				changed = true
			case "role", strings.ToLower(scimUserExtension + ".role"), strings.ToLower(scimUserExtension + ":role"):
				role := strings.TrimSpace(scimStringValue(operation.Value))
				if role == "" {
					writeError(w, http.StatusBadRequest, "SCIM role value is required")
					return
				}
				nextRole = role
				changed = true
			case "":
				email, role, hasChange := scimPatchObjectValues(operation.Value)
				if email != "" {
					nextEmail = email
				}
				if role != "" {
					nextRole = role
				}
				changed = changed || hasChange
			}
		}
		if changed {
			updated, err := store.UpdateUser(r.Context(), id, control.UpdateUserRequest{Email: nextEmail, Role: nextRole})
			if err != nil {
				writeStoreError(w, err)
				return
			}
			control.Audit(r.Context(), store, "scim.user_patch", "user:"+updated.ID, map[string]string{"email": updated.Email, "role": updated.Role})
			writeJSON(w, http.StatusOK, scimUserFromControl(r, updated))
			return
		}
		writeJSON(w, http.StatusOK, scimUserFromControl(r, user))
	}
}

func deleteSCIMUserHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		id := r.PathValue("id")
		user, err := store.GetUserByID(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := store.DeleteUser(r.Context(), id); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "scim.user_delete", "user:"+id, map[string]string{"email": user.Email})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listSCIMGroupsHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		orgs, err := store.ListOrgs(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		requestedOrgID := strings.TrimSpace(r.URL.Query().Get("org_id"))
		resources := []scimGroupResource{}
		for _, org := range orgs {
			if requestedOrgID != "" && org.ID != requestedOrgID {
				continue
			}
			teams, err := store.ListOrgTeams(r.Context(), org.ID)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			for _, team := range teams {
				resource, err := scimGroupFromControl(r, store, team)
				if err != nil {
					writeStoreError(w, err)
					return
				}
				resources = append(resources, resource)
			}
		}
		writeJSON(w, http.StatusOK, scimList(resources))
	}
}

func createSCIMGroupHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		var payload scimGroupRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		orgID := scimGroupOrgID(r, payload)
		if orgID == "" {
			writeError(w, http.StatusBadRequest, "SCIM group externalId or extension org_id is required")
			return
		}
		team, err := store.CreateOrgTeam(r.Context(), orgID, control.TeamInput{Name: payload.DisplayName, Slug: payload.Extension.Slug})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		for _, member := range payload.Members {
			email, err := scimMemberEmail(r.Context(), store, member)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			if _, err := store.UpsertTeamMember(r.Context(), orgID, team.Slug, control.TeamMemberInput{Email: email}); err != nil {
				writeStoreError(w, err)
				return
			}
		}
		resource, err := scimGroupFromControl(r, store, team)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "scim.group_create", "org:"+orgID, map[string]string{"team": team.Slug, "name": team.Name})
		writeJSON(w, http.StatusCreated, resource)
	}
}

func getSCIMGroupHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		team, err := findSCIMTeam(r.Context(), store, r.PathValue("id"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		resource, err := scimGroupFromControl(r, store, team)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resource)
	}
}

func deleteSCIMGroupHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		team, err := findSCIMTeam(r.Context(), store, r.PathValue("id"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := store.DeleteOrgTeam(r.Context(), team.OrgID, team.Slug); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "scim.group_delete", "org:"+team.OrgID, map[string]string{"team": team.Slug})
		w.WriteHeader(http.StatusNoContent)
	}
}

func createOrgHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload createOrgRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		org, err := store.CreateOrg(r.Context(), payload.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if claims, ok := claimsFromRequest(r); ok {
			member, err := store.UpsertOrgMember(r.Context(), org.ID, control.MembershipInput{Email: claims.Email, Role: "owner"})
			if err != nil {
				writeStoreError(w, err)
				return
			}
			control.Audit(r.Context(), store, "org.member_upsert", "org:"+org.ID, map[string]string{"email": member.Email, "role": member.Role})
		}
		control.Audit(r.Context(), store, "org.create", "org:"+org.ID, map[string]string{"name": org.Name})
		writeJSON(w, http.StatusCreated, org)
	}
}

func getOrgHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		org, err := store.GetOrg(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, org)
	}
}

func updateOrgHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		var payload createOrgRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		org, err := store.UpdateOrg(r.Context(), orgID, payload.Name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.update", "org:"+org.ID, map[string]string{"name": org.Name})
		writeJSON(w, http.StatusOK, org)
	}
}

func deleteOrgHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		if err := store.DeleteOrg(r.Context(), orgID); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.delete", "org:"+orgID, nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

func getOrgQuotaHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		quota, err := store.GetOrgQuota(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, quota)
	}
}

func updateOrgQuotaHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		var payload control.OrgQuotaInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		quota, err := store.UpdateOrgQuota(r.Context(), orgID, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.quota_update", "org:"+orgID, map[string]string{
			"max_projects":  fmt.Sprintf("%d", quota.MaxProjects),
			"max_cpu":       fmt.Sprintf("%d", quota.MaxCPU),
			"max_ram_mb":    fmt.Sprintf("%d", quota.MaxRAMMB),
			"max_disk_gb":   fmt.Sprintf("%d", quota.MaxDiskGB),
			"max_disk_iops": fmt.Sprintf("%d", quota.MaxDiskIOPS),
		})
		writeJSON(w, http.StatusOK, quota)
	}
}

func getOrgFeatureFlagsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		flags, err := store.GetOrgFeatureFlags(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, flags)
	}
}

func updateOrgFeatureFlagsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		var payload control.OrgFeatureFlagsInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		flags, err := store.UpdateOrgFeatureFlags(r.Context(), orgID, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.features_update", "org:"+orgID, map[string]string{
			"overrides": fmt.Sprintf("%d", len(flags.Overrides)),
			"enabled":   fmt.Sprintf("%d", countEnabledFlags(flags.Effective)),
		})
		writeJSON(w, http.StatusOK, flags)
	}
}

func getOrgUsageHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		usage, err := store.GetOrgUsage(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, usage)
	}
}

func listOrgUsageSnapshotsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 {
				writeError(w, http.StatusBadRequest, "limit must be a positive integer")
				return
			}
			limit = parsed
		}
		snapshots, err := store.ListOrgUsageSnapshots(r.Context(), orgID, limit)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, snapshots)
	}
}

func createOrgUsageSnapshotHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		if !requireOrgFeature(w, r, store, orgID, "usage_metering") {
			return
		}
		snapshot, err := store.CreateOrgUsageSnapshot(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.usage_snapshot_create", "org:"+orgID, map[string]string{"snapshot_id": snapshot.ID})
		writeJSON(w, http.StatusCreated, snapshot)
	}
}

func listBillingInvoicesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		if !requireOrgFeature(w, r, store, orgID, "billing") {
			return
		}
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 {
				writeError(w, http.StatusBadRequest, "limit must be a positive integer")
				return
			}
			limit = parsed
		}
		invoices, err := store.ListBillingInvoices(r.Context(), orgID, limit)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, invoices)
	}
}

func createBillingInvoiceHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		if !requireOrgFeature(w, r, store, orgID, "billing") {
			return
		}
		var payload control.BillingInvoiceInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		invoice, err := store.CreateBillingInvoice(r.Context(), orgID, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.billing_invoice_create", "org:"+orgID, map[string]string{
			"invoice_id": invoice.ID,
			"number":     invoice.Number,
			"status":     invoice.Status,
			"total":      fmt.Sprintf("%d", invoice.TotalCents),
		})
		writeJSON(w, http.StatusCreated, invoice)
	}
}

func getBillingInvoiceHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		if !requireOrgFeature(w, r, store, orgID, "billing") {
			return
		}
		invoice, err := store.GetBillingInvoice(r.Context(), orgID, r.PathValue("invoice_id"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, invoice)
	}
}

func getOrgAccessReviewHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		review, err := store.GetOrgAccessReview(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, review)
	}
}

func listOrgMembersHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		members, err := store.ListOrgMembers(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, members)
	}
}

func upsertOrgMemberHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		var payload control.MembershipInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		member, err := store.UpsertOrgMember(r.Context(), orgID, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.member_upsert", "org:"+orgID, map[string]string{"email": member.Email, "role": member.Role})
		writeJSON(w, http.StatusOK, member)
	}
}

func deleteOrgMemberHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		email := r.PathValue("email")
		if err := store.DeleteOrgMember(r.Context(), orgID, email); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.member_delete", "org:"+orgID, map[string]string{"email": strings.ToLower(strings.TrimSpace(email))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listOrgTeamsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		teams, err := store.ListOrgTeams(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, teams)
	}
}

func createOrgTeamHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		var payload control.TeamInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		team, err := store.CreateOrgTeam(r.Context(), orgID, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.team_create", "org:"+orgID, map[string]string{"team": team.Slug, "name": team.Name})
		writeJSON(w, http.StatusCreated, team)
	}
}

func deleteOrgTeamHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		slug := r.PathValue("slug")
		if err := store.DeleteOrgTeam(r.Context(), orgID, slug); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.team_delete", "org:"+orgID, map[string]string{"team": slug})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listTeamMembersHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		members, err := store.ListTeamMembers(r.Context(), orgID, r.PathValue("slug"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, members)
	}
}

func upsertTeamMemberHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		slug := r.PathValue("slug")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		var payload control.TeamMemberInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		member, err := store.UpsertTeamMember(r.Context(), orgID, slug, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.team_member_upsert", "org:"+orgID, map[string]string{"team": member.TeamSlug, "email": member.Email})
		writeJSON(w, http.StatusOK, member)
	}
}

func deleteTeamMemberHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		slug := r.PathValue("slug")
		if !requireOrgRole(w, r, store, orgID, roleAdmin) {
			return
		}
		email := r.PathValue("email")
		if err := store.DeleteTeamMember(r.Context(), orgID, slug, email); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "org.team_member_delete", "org:"+orgID, map[string]string{"team": slug, "email": strings.ToLower(strings.TrimSpace(email))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listAuditEventsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := store.ListAuditEvents(r.Context(), 100)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, events)
	}
}

func getAuditIntegrityHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		integrity, err := store.VerifyAuditLog(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, integrity)
	}
}

func createHostHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		var payload control.CreateHostRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		host, err := store.CreateHost(r.Context(), payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "host.create", "host:"+host.ID, map[string]string{"name": host.Name, "address": host.Address})
		writeJSON(w, http.StatusCreated, host)
	}
}

func listHostsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hosts, err := store.ListHosts(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, hosts)
	}
}

func getHostHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, err := store.GetHost(r.Context(), r.PathValue("id"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, host)
	}
}

func deleteHostHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		id := r.PathValue("id")
		if err := store.DeleteHost(r.Context(), id); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "host.delete", "host:"+id, nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

func listOrgsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgs, err := store.ListOrgs(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, orgs)
	}
}

func listOrgProjectsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("id")
		if !requireOrgRole(w, r, store, orgID, roleViewer) {
			return
		}
		projects, err := store.ListProjectsByOrg(r.Context(), orgID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, sanitizeProjectsForResponse(projects))
	}
}

func listProjectsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projects, err := projectsVisibleToRequest(r, store)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, sanitizeProjectsForResponse(projects))
	}
}

func createProjectHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if provisioner == nil {
			writeError(w, http.StatusServiceUnavailable, "provisioner is not configured")
			return
		}

		var payload control.CreateProjectRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		payload.OrgID = r.PathValue("id")
		if !requireOrgRole(w, r, store, payload.OrgID, roleAdmin) {
			return
		}

		project, err := store.CreateProject(r.Context(), payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		secrets, err := store.EnsureProjectSecrets(r.Context(), project.Ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}

		if err := provisioner.Create(r.Context(), control.ProjectSpecWithSecrets(project.Spec, secrets)); err != nil {
			project.Status = control.ProjectError
			project.Message = err.Error()
			_, _ = store.UpdateProjectStatus(r.Context(), project.Ref, control.ProjectError, err.Error())
			control.Audit(r.Context(), store, "project.provision_failed", "project:"+project.Ref, map[string]string{"error": err.Error()})
			writeJSON(w, http.StatusAccepted, sanitizeProjectForResponse(project))
			return
		}

		project.Status = control.ProjectHealthy
		project.Message = "project provisioned"
		_, _ = store.UpdateProjectStatus(r.Context(), project.Ref, control.ProjectHealthy, project.Message)
		routePath, err := reconcileProjectRoutes(r, store, project.Ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, project.Ref, "info", "Project secrets generated", map[string]string{"scope": "connect"})
		control.LogProject(r.Context(), store, project.Ref, "info", "Routes registered", map[string]string{"path": routePath})
		control.LogProject(r.Context(), store, project.Ref, "info", "Project provisioned", map[string]string{"provisioner": cfgProvisionerName(provisioner)})
		control.Audit(r.Context(), store, "project.create", "project:"+project.Ref, map[string]string{"org_id": project.OrgID, "host_id": project.Spec.HostID})
		writeJSON(w, http.StatusCreated, sanitizeProjectForResponse(project))
	}
}

func getProjectHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project, ok := requireProjectRole(w, r, store, r.PathValue("ref"), roleViewer)
		if !ok {
			return
		}
		if provisioner != nil {
			status, err := provisioner.Status(r.Context(), project.Ref)
			if err == nil {
				project.RuntimeStatus = &status
			}
		}
		writeJSON(w, http.StatusOK, sanitizeProjectForResponse(project))
	}
}

func getProjectMetricsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		metrics, err := store.GetProjectMetrics(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, metrics)
	}
}

func recordProjectTelemetryHandler(store control.Store) http.HandlerFunc {
	type payload struct {
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
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		var input payload
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sample, err := store.RecordProjectTelemetry(r.Context(), r.PathValue("ref"), control.TelemetrySampleInput{
			Source:           input.Source,
			CPUPercent:       input.CPUPercent,
			MemoryBytes:      input.MemoryBytes,
			MemoryLimitBytes: input.MemoryLimitBytes,
			DiskUsedBytes:    input.DiskUsedBytes,
			DiskLimitBytes:   input.DiskLimitBytes,
			NetworkRxBytes:   input.NetworkRxBytes,
			NetworkTxBytes:   input.NetworkTxBytes,
			SampledAt:        input.SampledAt,
		})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, sample)
	}
}

func getConnectHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project, ok := requireProjectRole(w, r, store, r.PathValue("ref"), roleViewer)
		if !ok {
			return
		}
		secrets, err := store.EnsureProjectSecrets(r.Context(), project.Ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		poolerConfig, err := store.GetProjectConfig(r.Context(), project.Ref, "pooler")
		if err != nil {
			writeStoreError(w, err)
			return
		}
		databaseConfig, err := store.GetProjectConfig(r.Context(), project.Ref, "database")
		if err != nil {
			writeStoreError(w, err)
			return
		}
		domains, err := store.ListProjectDomains(r.Context(), project.Ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, control.ConnectPayloadForProjectWithConfigsAndDomains(project, poolerConfig, databaseConfig, domains, secrets...))
	}
}

func getCLIProfileHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project, ok := requireProjectRole(w, r, store, r.PathValue("ref"), roleViewer)
		if !ok {
			return
		}
		secrets, err := store.EnsureProjectSecrets(r.Context(), project.Ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		poolerConfig, err := store.GetProjectConfig(r.Context(), project.Ref, "pooler")
		if err != nil {
			writeStoreError(w, err)
			return
		}
		databaseConfig, err := store.GetProjectConfig(r.Context(), project.Ref, "database")
		if err != nil {
			writeStoreError(w, err)
			return
		}
		domains, err := store.ListProjectDomains(r.Context(), project.Ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, control.ProjectCLIProfileForProjectWithConfigsAndDomains(project, poolerConfig, databaseConfig, domains, secrets...))
	}
}

func createProjectStudioSessionHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project, ok := requireProjectRole(w, r, store, r.PathValue("ref"), roleViewer)
		if !ok {
			return
		}
		claims, ok := claimsFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing token claims")
			return
		}
		ttl := 5 * time.Minute
		token, err := auth.IssueStudio(claims, project.Ref, ttl)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, projectStudioSessionResponse{
			Token:     token,
			ExpiresAt: time.Now().UTC().Add(ttl),
		})
	}
}

func listProjectAccessHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		grants, err := store.ListProjectAccess(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, grants)
	}
}

func upsertProjectAccessHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectAccessInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		grant, err := store.UpsertProjectAccess(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "project.access_upsert", "project:"+ref, map[string]string{"subject_type": grant.SubjectType, "subject_id": grant.SubjectID, "subject_name": grant.SubjectName, "role": grant.Role})
		writeJSON(w, http.StatusOK, grant)
	}
}

func deleteProjectAccessHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		subjectType := r.PathValue("subjectType")
		subjectID := r.PathValue("subjectID")
		if err := store.DeleteProjectAccess(r.Context(), ref, subjectType, subjectID); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "project.access_delete", "project:"+ref, map[string]string{"subject_type": subjectType, "subject_id": subjectID})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listProjectRoutesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		routes, err := store.ListProjectRoutes(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, routes)
	}
}

func getProjectRouteManifestHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleViewer)
		if !ok {
			return
		}
		routes, err := store.ListProjectRoutes(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		networkConfig, err := store.GetProjectConfig(r.Context(), ref, "network")
		if err != nil {
			writeStoreError(w, err)
			return
		}
		replicas, err := store.ListProjectReplicas(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, control.RouteManifestForProjectWithNetworkAndReplicas(project, routes, networkConfig, replicas))
	}
}

func listProjectDomainsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		domains, err := store.ListProjectDomains(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, domains)
	}
}

func addProjectDomainHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "custom_domains") {
			return
		}
		var payload control.ProjectDomainInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		domain, err := store.AddProjectDomain(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		cert, certErr := control.NewCertificateService().Provision(r.Context(), domain)
		if certErr != nil {
			updated, updateErr := store.UpdateProjectDomainCertificate(r.Context(), ref, domain.FQDN, control.ProjectDomainCertificateMetadata{Status: "failed", Mode: certModeFromState(cert.State)})
			if updateErr == nil {
				domain = updated
			}
			control.LogProject(r.Context(), store, ref, "error", "Custom domain certificate failed", map[string]string{"fqdn": domain.FQDN, "error": certErr.Error(), "cert_path": cert.Path})
			control.Audit(r.Context(), store, "project.domain_cert_failed", "project:"+ref, map[string]string{"fqdn": domain.FQDN, "error": certErr.Error(), "cert_path": cert.Path})
		} else {
			updated, updateErr := store.UpdateProjectDomainCertificate(r.Context(), ref, domain.FQDN, control.ProjectDomainCertificateMetadata{Status: cert.Status, Mode: certModeFromState(cert.State)})
			if updateErr != nil {
				writeStoreError(w, updateErr)
				return
			}
			domain = updated
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		certMetadata := map[string]string{"fqdn": domain.FQDN, "route_path": routePath, "cert_status": domain.CertStatus, "cert_path": cert.Path, "cert_state": cert.State}
		control.LogProject(r.Context(), store, ref, "info", "Custom domain added", certMetadata)
		control.Audit(r.Context(), store, "project.domain_create", "project:"+ref, certMetadata)
		writeJSON(w, http.StatusCreated, domain)
	}
}

func certModeFromState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "manual":
		return "manual"
	case "completed":
		return "command"
	case "byo":
		return "byo"
	default:
		return "acme"
	}
}

func deleteProjectDomainHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		fqdn := r.PathValue("fqdn")
		if err := control.NewCertificateService().Remove(r.Context(), ref, fqdn); err != nil && !errors.Is(err, os.ErrNotExist) {
			control.LogProject(r.Context(), store, ref, "error", "Custom domain certificate cleanup failed", map[string]string{"fqdn": fqdn, "error": err.Error()})
			control.Audit(r.Context(), store, "project.domain_cert_cleanup_failed", "project:"+ref, map[string]string{"fqdn": fqdn, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProjectDomain(r.Context(), ref, fqdn); err != nil {
			writeStoreError(w, err)
			return
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Custom domain removed", map[string]string{"fqdn": fqdn, "route_path": routePath})
		control.Audit(r.Context(), store, "project.domain_delete", "project:"+ref, map[string]string{"fqdn": fqdn})
		w.WriteHeader(http.StatusNoContent)
	}
}

func uploadProjectDomainCertificateHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "custom_domains") {
			return
		}
		domain, err := findProjectDomain(r.Context(), store, ref, r.PathValue("fqdn"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		var payload control.ProjectDomainCertificateInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cert, err := control.NewCertificateService().Upload(r.Context(), domain, payload.CertificatePEM, payload.PrivateKeyPEM)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		updated, err := store.UpdateProjectDomainCertificate(r.Context(), ref, domain.FQDN, control.ProjectDomainCertificateMetadata{
			Status:      cert.Status,
			Mode:        "byo",
			Fingerprint: cert.Fingerprint,
			NotAfter:    cert.NotAfter,
		})
		if err != nil {
			_ = control.NewCertificateService().Remove(r.Context(), ref, domain.FQDN)
			writeStoreError(w, err)
			return
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"fqdn":             updated.FQDN,
			"route_path":       routePath,
			"cert_status":      updated.CertStatus,
			"cert_mode":        updated.CertMode,
			"cert_fingerprint": updated.CertFingerprint,
		}
		control.LogProject(r.Context(), store, ref, "info", "Custom domain certificate uploaded", metadata)
		control.Audit(r.Context(), store, "project.domain_cert_upload", "project:"+ref, metadata)
		writeJSON(w, http.StatusOK, updated)
	}
}

func resetProjectDomainCertificateHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "custom_domains") {
			return
		}
		domain, err := findProjectDomain(r.Context(), store, ref, r.PathValue("fqdn"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		certService := control.NewCertificateService()
		if err := certService.Remove(r.Context(), ref, domain.FQDN); err != nil && !errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		cert, certErr := certService.Provision(r.Context(), domain)
		status := cert.Status
		mode := "acme"
		if cert.State == "manual" {
			mode = "manual"
		}
		if certErr != nil {
			status = "failed"
		}
		updated, err := store.UpdateProjectDomainCertificate(r.Context(), ref, domain.FQDN, control.ProjectDomainCertificateMetadata{
			Status: status,
			Mode:   mode,
		})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{"fqdn": updated.FQDN, "route_path": routePath, "cert_status": updated.CertStatus, "cert_mode": updated.CertMode, "cert_path": cert.Path, "cert_state": cert.State}
		if certErr != nil {
			metadata["error"] = certErr.Error()
			control.LogProject(r.Context(), store, ref, "error", "Custom domain certificate reset failed", metadata)
			control.Audit(r.Context(), store, "project.domain_cert_reset_failed", "project:"+ref, metadata)
		} else {
			control.LogProject(r.Context(), store, ref, "info", "Custom domain certificate reset", metadata)
			control.Audit(r.Context(), store, "project.domain_cert_reset", "project:"+ref, metadata)
		}
		writeJSON(w, http.StatusOK, updated)
	}
}

func findProjectDomain(ctx context.Context, store control.Store, ref string, fqdn string) (control.ProjectDomain, error) {
	needle := strings.Trim(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	domains, err := store.ListProjectDomains(ctx, ref)
	if err != nil {
		return control.ProjectDomain{}, err
	}
	for _, domain := range domains {
		if domain.FQDN == needle {
			return domain, nil
		}
	}
	return control.ProjectDomain{}, fmt.Errorf("%w: domain %s for project %s", control.ErrNotFound, fqdn, ref)
}

func getProjectConfigHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		config, err := store.GetProjectConfig(r.Context(), ref, r.PathValue("area"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, config)
	}
}

func getProjectServicesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		services, err := store.GetProjectServices(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, services)
	}
}

func updateProjectServicesHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectServicesInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		project, err := store.UpdateProjectServices(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := serviceAuditMetadata(project.Spec.Services)
		if syncer, ok := provisioner.(control.ServiceSyncer); ok {
			if err := syncer.SyncServices(r.Context(), ref, project.Spec); err != nil {
				control.LogProject(r.Context(), store, ref, "error", "Project services sync failed", map[string]string{"error": err.Error()})
				control.Audit(r.Context(), store, "project.services_sync_failed", "project:"+ref, map[string]string{"error": err.Error()})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			metadata["runtime_synced"] = "true"
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata["route_path"] = routePath
		project, err = store.UpdateProjectStatus(r.Context(), ref, control.ProjectHealthy, "enabled services updated")
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "Project services updated", metadata)
		control.Audit(r.Context(), store, "project.services_update", "project:"+ref, metadata)
		writeJSON(w, http.StatusOK, control.ProjectServices{ProjectRef: project.Ref, Services: control.ProjectServiceStates(project.Spec.Services), UpdatedAt: project.UpdatedAt})
	}
}

func updateProjectConfigHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		area := r.PathValue("area")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		previousConfig, err := store.GetProjectConfig(r.Context(), ref, area)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		var payload control.ProjectConfigInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		config, err := store.UpdateProjectConfig(r.Context(), ref, area, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{"area": config.Area}
		if config.Area == "network" {
			routePath, err := reconcileProjectRoutes(r, store, ref)
			if err != nil {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			metadata["route_path"] = routePath
		}
		if syncer, ok := provisioner.(control.ConfigSyncer); ok {
			runtimeConfig, err := materializeProjectConfigForRuntime(r.Context(), store, ref, config)
			if err != nil {
				rollbackProjectConfig(r.Context(), store, ref, previousConfig)
				control.LogProject(r.Context(), store, ref, "error", "Project config secret resolution failed", map[string]string{"area": config.Area, "error": err.Error()})
				control.Audit(r.Context(), store, "project.config_secret_resolution_failed", "project:"+ref, map[string]string{"area": config.Area, "error": err.Error()})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			if err := syncer.SyncConfig(r.Context(), ref, runtimeConfig); err != nil {
				rollbackProjectConfig(r.Context(), store, ref, previousConfig)
				control.LogProject(r.Context(), store, ref, "error", "Project config sync failed", map[string]string{"area": config.Area, "error": err.Error()})
				control.Audit(r.Context(), store, "project.config_sync_failed", "project:"+ref, map[string]string{"area": config.Area, "error": err.Error()})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			metadata["runtime_synced"] = "true"
		}
		control.LogProject(r.Context(), store, ref, "info", "Project config updated", map[string]string{"area": config.Area})
		control.Audit(r.Context(), store, "project.config_update", "project:"+ref, metadata)
		writeJSON(w, http.StatusOK, config)
	}
}

func rollbackProjectConfig(ctx context.Context, store control.Store, ref string, previous control.ProjectConfig) {
	_, _ = store.UpdateProjectConfig(ctx, ref, previous.Area, control.ProjectConfigInput{Config: previous.Config})
}

func materializeProjectConfigForRuntime(ctx context.Context, store control.Store, ref string, config control.ProjectConfig) (control.ProjectConfig, error) {
	runtimeConfig := config
	runtimeConfig.Config = cloneRuntimeConfigMap(config.Config)
	for _, key := range runtimeSecretHandleKeys(config.Area) {
		handle := strings.TrimSpace(config.Config[key])
		if handle == "" {
			continue
		}
		value, err := resolveProjectSecretHandleValue(ctx, store, ref, handle, key)
		if err != nil {
			return control.ProjectConfig{}, err
		}
		runtimeConfig.Config[runtimeResolvedSecretKey(key)] = value
	}
	return runtimeConfig, nil
}

func runtimeSecretHandleKeys(area string) []string {
	switch area {
	case "auth":
		return []string{"captcha_secret_handle"}
	case "auth_providers":
		keys := []string{
			"oauth_google_client_secret_handle",
			"oauth_github_client_secret_handle",
			"oauth_azure_client_secret_handle",
			"sms_test_otp_handle",
			"sms_twilio_auth_token_handle",
			"sms_messagebird_access_key_handle",
			"sms_textlocal_api_key_handle",
			"sms_vonage_api_secret_handle",
		}
		for _, provider := range []string{
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
		} {
			keys = append(keys, "oauth_"+provider+"_client_secret_handle")
		}
		sort.Strings(keys)
		return keys
	case "smtp":
		return []string{"password_handle"}
	default:
		return nil
	}
}

func runtimeResolvedSecretKey(handleKey string) string {
	return "__resolved_" + handleKey
}

func cloneRuntimeConfigMap(input map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func listProjectAuthClientsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		clients, err := store.ListProjectAuthClients(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskAuthClients(clients))
	}
}

func createProjectAuthClientHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectAuthClientInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		client, err := store.CreateProjectAuthClient(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{
			"client_id":    client.ClientID,
			"name":         client.Name,
			"confidential": fmt.Sprintf("%t", client.Confidential),
		}
		control.LogProject(r.Context(), store, ref, "info", "Auth client registered", metadata)
		control.Audit(r.Context(), store, "project.auth_client_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskAuthClient(client))
	}
}

func deleteProjectAuthClientHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		clientID := r.PathValue("client_id")
		if err := store.DeleteProjectAuthClient(r.Context(), ref, clientID); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Auth client deleted", map[string]string{"client_id": clientID})
		control.Audit(r.Context(), store, "project.auth_client_delete", "project:"+ref, map[string]string{"client_id": clientID})
		w.WriteHeader(http.StatusNoContent)
	}
}

func maskAuthClients(clients []control.ProjectAuthClient) []control.ProjectAuthClient {
	out := make([]control.ProjectAuthClient, len(clients))
	copy(out, clients)
	for index := range out {
		out[index] = maskAuthClient(out[index])
	}
	return out
}

func maskAuthClient(client control.ProjectAuthClient) control.ProjectAuthClient {
	if strings.TrimSpace(client.ClientSecretHandle) != "" {
		client.ClientSecretHandle = "********"
	}
	return client
}

func listProjectAuthHooksHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		hooks, err := store.ListProjectAuthHooks(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskAuthHooks(hooks))
	}
}

func createProjectAuthHookHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		previousHooks, err := store.ListProjectAuthHooks(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		var payload control.ProjectAuthHookInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		hook, err := store.CreateProjectAuthHook(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{
			"hook_type": hook.HookType,
			"enabled":   fmt.Sprintf("%t", hook.Enabled),
			"target":    authHookTargetForMetadata(hook),
		}
		if syncer, ok := provisioner.(control.AuthHookSyncer); ok {
			if err := syncProjectAuthHooks(r, store, syncer, ref); err != nil {
				restoreProjectAuthHooks(r.Context(), store, ref, previousHooks)
				control.LogProject(r.Context(), store, ref, "error", "Auth hooks sync failed", map[string]string{"hook_type": hook.HookType, "error": err.Error()})
				control.Audit(r.Context(), store, "project.auth_hooks_sync_failed", "project:"+ref, map[string]string{"hook_type": hook.HookType, "error": err.Error()})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			metadata["runtime_synced"] = "true"
		}
		control.LogProject(r.Context(), store, ref, "info", "Auth hook configured", metadata)
		control.Audit(r.Context(), store, "project.auth_hook_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskAuthHook(hook))
	}
}

func deleteProjectAuthHookHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		previousHooks, err := store.ListProjectAuthHooks(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		hookType := r.PathValue("hook_type")
		if err := store.DeleteProjectAuthHook(r.Context(), ref, hookType); err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{"hook_type": hookType}
		if syncer, ok := provisioner.(control.AuthHookSyncer); ok {
			if err := syncProjectAuthHooks(r, store, syncer, ref); err != nil {
				restoreProjectAuthHooks(r.Context(), store, ref, previousHooks)
				control.LogProject(r.Context(), store, ref, "error", "Auth hooks sync failed", map[string]string{"hook_type": hookType, "error": err.Error()})
				control.Audit(r.Context(), store, "project.auth_hooks_sync_failed", "project:"+ref, map[string]string{"hook_type": hookType, "error": err.Error()})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			metadata["runtime_synced"] = "true"
		}
		control.LogProject(r.Context(), store, ref, "warning", "Auth hook deleted", metadata)
		control.Audit(r.Context(), store, "project.auth_hook_delete", "project:"+ref, metadata)
		w.WriteHeader(http.StatusNoContent)
	}
}

func syncProjectAuthHooks(r *http.Request, store control.Store, syncer control.AuthHookSyncer, ref string) error {
	hooks, err := store.ListProjectAuthHooks(r.Context(), ref)
	if err != nil {
		return err
	}
	runtimeHooks, err := materializeProjectAuthHooksForRuntime(r.Context(), store, ref, hooks)
	if err != nil {
		return err
	}
	return syncer.SyncAuthHooks(r.Context(), ref, runtimeHooks)
}

func materializeProjectAuthHooksForRuntime(ctx context.Context, store control.Store, ref string, hooks []control.ProjectAuthHook) ([]control.ProjectAuthHook, error) {
	out := append([]control.ProjectAuthHook(nil), hooks...)
	for index := range out {
		out[index].Headers = cloneRuntimeConfigMap(out[index].Headers)
		out[index].RuntimeHeaders = cloneRuntimeConfigMap(out[index].Headers)
		if !out[index].Enabled {
			continue
		}
		if handle := strings.TrimSpace(out[index].SecretHandle); handle != "" {
			resolved, err := resolveProjectSecretHandleValue(ctx, store, ref, handle, "auth hook secret_handle")
			if err != nil {
				return nil, fmt.Errorf("auth hook secret_handle: %w", err)
			}
			out[index].RuntimeSecret = resolved
		}
		for key, value := range out[index].Headers {
			if !isSensitiveAuthHookHeaderKey(key) || strings.TrimSpace(value) == "" {
				continue
			}
			resolved, err := resolveProjectSecretHandleValue(ctx, store, ref, value, "auth hook header "+key)
			if err != nil {
				return nil, fmt.Errorf("auth hook header %s: %w", key, err)
			}
			out[index].RuntimeHeaders[key] = resolved
		}
	}
	return out, nil
}

func restoreProjectAuthHooks(ctx context.Context, store control.Store, ref string, hooks []control.ProjectAuthHook) {
	current, err := store.ListProjectAuthHooks(ctx, ref)
	if err == nil {
		for _, hook := range current {
			_ = store.DeleteProjectAuthHook(ctx, ref, hook.HookType)
		}
	}
	for _, hook := range hooks {
		_, _ = store.CreateProjectAuthHook(ctx, ref, authHookInputFromHook(hook))
	}
}

func authHookInputFromHook(hook control.ProjectAuthHook) control.ProjectAuthHookInput {
	return control.ProjectAuthHookInput{
		HookType:      hook.HookType,
		Enabled:       hook.Enabled,
		TargetURI:     hook.TargetURI,
		EdgeFunction:  hook.EdgeFunction,
		SecretHandle:  hook.SecretHandle,
		Headers:       cloneRuntimeConfigMap(hook.Headers),
		TimeoutMS:     hook.TimeoutMS,
		RetryAttempts: hook.RetryAttempts,
	}
}

func authHookTargetForMetadata(hook control.ProjectAuthHook) string {
	if hook.EdgeFunction != "" {
		return "edge:" + hook.EdgeFunction
	}
	return hook.TargetURI
}

func maskAuthHooks(hooks []control.ProjectAuthHook) []control.ProjectAuthHook {
	out := make([]control.ProjectAuthHook, len(hooks))
	copy(out, hooks)
	for index := range out {
		out[index] = maskAuthHook(out[index])
	}
	return out
}

func maskAuthHook(hook control.ProjectAuthHook) control.ProjectAuthHook {
	if strings.TrimSpace(hook.SecretHandle) != "" {
		hook.SecretHandle = "********"
	}
	masked := map[string]string{}
	for key, value := range hook.Headers {
		if isSensitiveAuthHookHeaderKey(key) && strings.TrimSpace(value) != "" {
			masked[key] = "********"
			continue
		}
		masked[key] = value
	}
	hook.Headers = masked
	return hook
}

func isSensitiveAuthHookHeaderKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "proxy-authorization", "x-api-key", "x-auth-token":
		return true
	default:
		return isSensitiveLogDrainConfigKey(key)
	}
}

func listProjectBranchesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		branches, err := store.ListProjectBranches(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, branches)
	}
}

func createProjectBranchHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sourceRef := r.PathValue("ref")
		sourceProject, ok := requireProjectRole(w, r, store, sourceRef, roleDeveloper)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, sourceProject, "preview_branches") {
			return
		}
		if provisioner == nil {
			writeError(w, http.StatusServiceUnavailable, "provisioner is not configured")
			return
		}
		var payload control.ProjectBranchInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		branch, project, err := store.CreateProjectBranch(r.Context(), sourceRef, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		secrets, err := store.EnsureProjectSecrets(r.Context(), project.Ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := provisioner.Create(r.Context(), control.ProjectSpecWithSecrets(project.Spec, secrets)); err != nil {
			project.Status = control.ProjectError
			project.Message = err.Error()
			branch.Status = string(control.ProjectError)
			_, _ = store.UpdateProjectStatus(r.Context(), project.Ref, control.ProjectError, err.Error())
			control.LogProject(r.Context(), store, sourceRef, "error", "Branch provision failed", map[string]string{"branch_ref": branch.ProjectRef, "error": err.Error()})
			control.Audit(r.Context(), store, "project.branch_failed", "project:"+sourceRef, map[string]string{"branch_ref": branch.ProjectRef, "error": err.Error()})
			writeJSON(w, http.StatusAccepted, createBranchResponse{Branch: branch, Project: sanitizeProjectForResponse(project)})
			return
		}
		cloneMetadata := map[string]string{
			"branch_ref": branch.ProjectRef,
			"with_data":  fmt.Sprintf("%t", branch.WithData),
		}
		if branch.WithData {
			control.LogProject(r.Context(), store, project.Ref, "info", "Branch data clone requested", map[string]string{"source_ref": sourceRef})
		} else {
			control.LogProject(r.Context(), store, project.Ref, "info", "Branch created without source data", map[string]string{"source_ref": sourceRef})
		}
		if branch.WithData {
			cloner, ok := provisioner.(control.BranchCloner)
			if !ok {
				project.Status = control.ProjectError
				project.Message = "branch data clone is not supported by this provisioner"
				branch.Status = string(control.ProjectError)
				_, _ = store.UpdateProjectStatus(r.Context(), project.Ref, control.ProjectError, project.Message)
				control.LogProject(r.Context(), store, sourceRef, "error", "Branch clone failed", map[string]string{"branch_ref": branch.ProjectRef, "error": project.Message})
				control.Audit(r.Context(), store, "project.branch_clone_failed", "project:"+sourceRef, map[string]string{"branch_ref": branch.ProjectRef, "error": project.Message})
				writeJSON(w, http.StatusAccepted, createBranchResponse{Branch: branch, Project: sanitizeProjectForResponse(project)})
				return
			}
			clone, err := cloner.CloneBranch(r.Context(), control.BranchCloneOptions{
				SourceRef: sourceRef,
				BranchRef: branch.ProjectRef,
				BranchID:  branch.ID,
				Name:      branch.Name,
				WithData:  branch.WithData,
				ExpiresAt: branch.ExpiresAt,
			})
			if err != nil {
				project.Status = control.ProjectError
				project.Message = err.Error()
				branch.Status = string(control.ProjectError)
				_, _ = store.UpdateProjectStatus(r.Context(), project.Ref, control.ProjectError, err.Error())
				control.LogProject(r.Context(), store, sourceRef, "error", "Branch clone failed", map[string]string{"branch_ref": branch.ProjectRef, "error": err.Error()})
				control.Audit(r.Context(), store, "project.branch_clone_failed", "project:"+sourceRef, map[string]string{"branch_ref": branch.ProjectRef, "error": err.Error()})
				writeJSON(w, http.StatusAccepted, createBranchResponse{Branch: branch, Project: sanitizeProjectForResponse(project)})
				return
			}
			cloneMetadata["clone_path"] = clone.Path
			cloneMetadata["clone_state"] = clone.State
			control.LogProject(r.Context(), store, project.Ref, "info", "Branch data clone prepared", map[string]string{"source_ref": sourceRef, "clone_path": clone.Path, "clone_state": clone.State})
		}

		project.Status = control.ProjectHealthy
		project.Message = "branch provisioned"
		branch.Status = string(project.Status)
		updated, err := store.UpdateProjectStatus(r.Context(), project.Ref, control.ProjectHealthy, project.Message)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		project = updated
		routePath, err := reconcileProjectRoutes(r, store, project.Ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, sourceRef, "info", "Branch created", map[string]string{"branch_ref": branch.ProjectRef})
		control.LogProject(r.Context(), store, project.Ref, "info", "Branch provisioned", map[string]string{"source_ref": sourceRef, "route_path": routePath})
		control.Audit(r.Context(), store, "project.branch_create", "project:"+sourceRef, cloneMetadata)
		writeJSON(w, http.StatusCreated, createBranchResponse{Branch: branch, Project: sanitizeProjectForResponse(project)})
	}
}

func deleteProjectBranchHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sourceRef := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, sourceRef, roleAdmin); !ok {
			return
		}
		if provisioner == nil {
			writeError(w, http.StatusServiceUnavailable, "provisioner is not configured")
			return
		}
		branchRef := strings.ToLower(strings.TrimSpace(r.PathValue("branch_ref")))
		branches, err := store.ListProjectBranches(r.Context(), sourceRef)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		found := false
		for _, branch := range branches {
			if branch.ProjectRef == branchRef {
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, fmt.Sprintf("branch %s not found", branchRef))
			return
		}
		if err := provisioner.Destroy(r.Context(), branchRef); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := control.NewRoutingService("").RemoveProject(branchRef); err != nil {
			control.LogProject(r.Context(), store, sourceRef, "error", "Branch route cleanup failed", map[string]string{"branch_ref": branchRef, "error": err.Error()})
			control.Audit(r.Context(), store, "project.branch_route_cleanup_failed", "project:"+sourceRef, map[string]string{"branch_ref": branchRef, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := control.NewCertificateService().RemoveProject(r.Context(), branchRef); err != nil {
			control.LogProject(r.Context(), store, sourceRef, "error", "Branch certificate cleanup failed", map[string]string{"branch_ref": branchRef, "error": err.Error()})
			control.Audit(r.Context(), store, "project.branch_certificate_cleanup_failed", "project:"+sourceRef, map[string]string{"branch_ref": branchRef, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProjectBranch(r.Context(), sourceRef, branchRef); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, sourceRef, "warning", "Branch deleted", map[string]string{"branch_ref": branchRef})
		control.Audit(r.Context(), store, "project.branch_delete", "project:"+sourceRef, map[string]string{"branch_ref": branchRef})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listProjectReplicasHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		replicas, err := store.ListProjectReplicas(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, replicas)
	}
}

func getProjectReplicaRoutingHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		routing, err := store.GetProjectReplicaRouting(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, routing)
	}
}

func promoteProjectReplicaHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		replicaID := r.PathValue("id")
		var payload struct {
			Reason string `json:"reason"`
		}
		if r.Body != nil {
			_ = decodeJSON(r, &payload)
		}
		replica, err := store.PromoteProjectReplica(r.Context(), ref, replicaID, payload.Reason)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := syncProjectReplicas(r, store, provisioner, ref); err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Read replica sync failed", map[string]string{"replica_id": replica.ID, "replica_name": replica.Name, "error": err.Error()})
			control.Audit(r.Context(), store, "project.replica_sync_failed", "project:"+ref, map[string]string{"replica_id": replica.ID, "replica_name": replica.Name, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Read replica promoted", map[string]string{"replica_id": replica.ID, "replica_name": replica.Name, "reason": replica.Message, "route_path": routePath})
		control.Audit(r.Context(), store, "project.replica_promote", "project:"+ref, map[string]string{"replica_id": replica.ID, "replica_name": replica.Name, "reason": replica.Message, "route_path": routePath})
		writeJSON(w, http.StatusOK, replica)
	}
}

func deleteProjectReplicaHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		replicaID := r.PathValue("id")
		if syncer, ok := provisioner.(control.ReplicaSyncer); ok {
			replicas, err := store.ListProjectReplicas(r.Context(), ref)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			found := false
			remaining := make([]control.ProjectReplica, 0, len(replicas))
			for _, replica := range replicas {
				if replica.ID == replicaID {
					found = true
					continue
				}
				remaining = append(remaining, replica)
			}
			if !found {
				writeStoreError(w, fmt.Errorf("%w: replica %s for project %s", control.ErrNotFound, replicaID, ref))
				return
			}
			if err := syncer.SyncReplicas(r.Context(), ref, remaining); err != nil {
				control.LogProject(r.Context(), store, ref, "error", "Read replica sync failed", map[string]string{"replica_id": replicaID, "error": err.Error()})
				control.Audit(r.Context(), store, "project.replica_sync_failed", "project:"+ref, map[string]string{"replica_id": replicaID, "error": err.Error()})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
		}
		if err := store.DeleteProjectReplica(r.Context(), ref, replicaID); err != nil {
			writeStoreError(w, err)
			return
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Read replica deleted", map[string]string{"replica_id": replicaID, "route_path": routePath})
		control.Audit(r.Context(), store, "project.replica_delete", "project:"+ref, map[string]string{"replica_id": replicaID, "route_path": routePath})
		w.WriteHeader(http.StatusNoContent)
	}
}

func failoverProjectReplicaHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload struct {
			Reason string `json:"reason"`
		}
		if r.Body != nil {
			_ = decodeJSON(r, &payload)
		}
		replica, err := store.FailoverProjectReplica(r.Context(), ref, payload.Reason)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := syncProjectReplicas(r, store, provisioner, ref); err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Read replica sync failed", map[string]string{"replica_id": replica.ID, "replica_name": replica.Name, "error": err.Error()})
			control.Audit(r.Context(), store, "project.replica_sync_failed", "project:"+ref, map[string]string{"replica_id": replica.ID, "replica_name": replica.Name, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Read replica failover completed", map[string]string{"replica_id": replica.ID, "replica_name": replica.Name, "reason": replica.Message, "route_path": routePath})
		control.Audit(r.Context(), store, "project.replica_failover", "project:"+ref, map[string]string{"replica_id": replica.ID, "replica_name": replica.Name, "reason": replica.Message, "route_path": routePath})
		writeJSON(w, http.StatusOK, replica)
	}
}

func createProjectReplicaHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "read_replicas") {
			return
		}
		if provisioner == nil {
			writeError(w, http.StatusServiceUnavailable, "provisioner is not configured")
			return
		}
		var payload control.ProjectReplicaInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		replica, err := store.CreateProjectReplica(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := syncCreatedProjectReplica(r, store, provisioner, ref, replica); err != nil {
			replica, _ = store.UpdateProjectReplicaStatus(r.Context(), ref, replica.ID, "error", err.Error())
			control.LogProject(r.Context(), store, ref, "error", "Read replica provision failed", map[string]string{"replica_id": replica.ID, "replica_name": replica.Name, "error": err.Error()})
			control.Audit(r.Context(), store, "project.replica_failed", "project:"+ref, map[string]string{"replica_id": replica.ID, "replica_name": replica.Name, "error": err.Error()})
			writeJSON(w, http.StatusAccepted, replica)
			return
		}
		replica, err = store.UpdateProjectReplicaStatus(r.Context(), ref, replica.ID, "healthy", "replica provisioned")
		if err != nil {
			writeStoreError(w, err)
			return
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "Read replica provisioned", map[string]string{"replica_id": replica.ID, "replica_name": replica.Name, "read_uri": replica.ReadURI, "route_path": routePath})
		control.Audit(r.Context(), store, "project.replica_create", "project:"+ref, map[string]string{"replica_id": replica.ID, "replica_name": replica.Name, "route_path": routePath})
		writeJSON(w, http.StatusCreated, replica)
	}
}

func syncCreatedProjectReplica(r *http.Request, store control.Store, provisioner control.Provisioner, ref string, replica control.ProjectReplica) error {
	if syncer, ok := provisioner.(control.ReplicaSyncer); ok {
		replicas, err := store.ListProjectReplicas(r.Context(), ref)
		if err != nil {
			return err
		}
		return syncer.SyncReplicas(r.Context(), ref, replicas)
	}
	opts := control.ReplicaOpts{
		ID:               replica.ID,
		Name:             replica.Name,
		HostID:           replica.HostID,
		Region:           replica.Region,
		Tier:             replica.Tier,
		ReadWeight:       replica.ReadWeight,
		FailoverPriority: replica.FailoverPriority,
	}
	return provisioner.AddReplica(r.Context(), ref, opts)
}

func syncProjectReplicas(r *http.Request, store control.Store, provisioner control.Provisioner, ref string) error {
	syncer, ok := provisioner.(control.ReplicaSyncer)
	if !ok {
		return nil
	}
	replicas, err := store.ListProjectReplicas(r.Context(), ref)
	if err != nil {
		return err
	}
	return syncer.SyncReplicas(r.Context(), ref, replicas)
}

func listProjectFunctionsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		functions, err := store.ListProjectFunctions(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, functions)
	}
}

func deployProjectFunctionHandler(store control.Store) http.HandlerFunc {
	functionService := control.NewFunctionDeploymentService()
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleDeveloper)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "edge_functions") {
			return
		}
		var payload control.ProjectFunctionInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		function, err := store.DeployProjectFunction(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		artifact, err := functionService.Deploy(r.Context(), function, payload)
		if err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Function artifact deploy failed", map[string]string{"name": function.Name, "version": fmt.Sprintf("%d", function.Version), "error": err.Error()})
			control.Audit(r.Context(), store, "project.function_deploy_failed", "project:"+ref, map[string]string{"name": function.Name, "version": fmt.Sprintf("%d", function.Version), "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "Function deployed", map[string]string{"name": function.Name, "version": fmt.Sprintf("%d", function.Version), "artifact_path": artifact.Directory})
		control.Audit(r.Context(), store, "project.function_deploy", "project:"+ref, map[string]string{"name": function.Name, "version": fmt.Sprintf("%d", function.Version), "artifact_path": artifact.Directory})
		writeJSON(w, http.StatusCreated, function)
	}
}

func deleteProjectFunctionHandler(store control.Store) http.HandlerFunc {
	functionService := control.NewFunctionDeploymentService()
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		name := r.PathValue("name")
		if _, ok := requireProjectRole(w, r, store, ref, roleDeveloper); !ok {
			return
		}
		if err := store.DeleteProjectFunction(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		if err := functionService.Delete(r.Context(), ref, name); err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Function artifact delete failed", map[string]string{"name": name, "error": err.Error()})
			control.Audit(r.Context(), store, "project.function_delete_failed", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name)), "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Function deleted", map[string]string{"name": name})
		control.Audit(r.Context(), store, "project.function_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listProjectFunctionRegionsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		regions, err := store.ListProjectFunctionRegions(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, regions)
	}
}

func createProjectFunctionRegionHandler(store control.Store) http.HandlerFunc {
	functionService := control.NewFunctionDeploymentService()
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleDeveloper)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "edge_functions") {
			return
		}
		var payload control.ProjectFunctionRegionInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		region, err := store.CreateProjectFunctionRegion(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		regions, err := store.ListProjectFunctionRegions(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := functionService.SyncRegionalInvocations(r.Context(), ref, regions); err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Function regional invocation sync failed", map[string]string{"region_id": region.ID, "error": err.Error()})
			control.Audit(r.Context(), store, "project.function_region_sync_failed", "project:"+ref, map[string]string{"region_id": region.ID, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{"region_id": region.ID, "function_name": region.FunctionName, "region": region.Region, "routing_policy": region.RoutingPolicy}
		if strings.TrimSpace(region.HostID) != "" {
			metadata["host_id"] = region.HostID
		}
		control.LogProject(r.Context(), store, ref, "info", "Function regional invocation configured", metadata)
		control.Audit(r.Context(), store, "project.function_region_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, region)
	}
}

func deleteProjectFunctionRegionHandler(store control.Store) http.HandlerFunc {
	functionService := control.NewFunctionDeploymentService()
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		id := r.PathValue("id")
		if _, ok := requireProjectRole(w, r, store, ref, roleDeveloper); !ok {
			return
		}
		if err := store.DeleteProjectFunctionRegion(r.Context(), ref, id); err != nil {
			writeStoreError(w, err)
			return
		}
		regions, err := store.ListProjectFunctionRegions(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := functionService.SyncRegionalInvocations(r.Context(), ref, regions); err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Function regional invocation sync failed", map[string]string{"region_id": id, "error": err.Error()})
			control.Audit(r.Context(), store, "project.function_region_sync_failed", "project:"+ref, map[string]string{"region_id": id, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Function regional invocation removed", map[string]string{"region_id": id})
		control.Audit(r.Context(), store, "project.function_region_delete", "project:"+ref, map[string]string{"region_id": id})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listProjectFunctionStorageMountsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		mounts, err := store.ListProjectFunctionStorageMounts(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, mounts)
	}
}

func createProjectFunctionStorageMountHandler(store control.Store) http.HandlerFunc {
	functionService := control.NewFunctionDeploymentService()
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleDeveloper)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "edge_functions") {
			return
		}
		var payload control.ProjectFunctionStorageMountInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		mount, err := store.CreateProjectFunctionStorageMount(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		mounts, err := store.ListProjectFunctionStorageMounts(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := functionService.SyncStorageMounts(r.Context(), ref, mounts); err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Function storage mount sync failed", map[string]string{"mount_id": mount.ID, "error": err.Error()})
			control.Audit(r.Context(), store, "project.function_storage_mount_sync_failed", "project:"+ref, map[string]string{"mount_id": mount.ID, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{"mount_id": mount.ID, "function_name": mount.FunctionName, "bucket_name": mount.BucketName, "mount_path": mount.MountPath}
		control.LogProject(r.Context(), store, ref, "info", "Function storage mount configured", metadata)
		control.Audit(r.Context(), store, "project.function_storage_mount_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, mount)
	}
}

func deleteProjectFunctionStorageMountHandler(store control.Store) http.HandlerFunc {
	functionService := control.NewFunctionDeploymentService()
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		id := r.PathValue("id")
		if _, ok := requireProjectRole(w, r, store, ref, roleDeveloper); !ok {
			return
		}
		if err := store.DeleteProjectFunctionStorageMount(r.Context(), ref, id); err != nil {
			writeStoreError(w, err)
			return
		}
		mounts, err := store.ListProjectFunctionStorageMounts(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := functionService.SyncStorageMounts(r.Context(), ref, mounts); err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Function storage mount sync failed", map[string]string{"mount_id": id, "error": err.Error()})
			control.Audit(r.Context(), store, "project.function_storage_mount_sync_failed", "project:"+ref, map[string]string{"mount_id": id, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Function storage mount removed", map[string]string{"mount_id": id})
		control.Audit(r.Context(), store, "project.function_storage_mount_delete", "project:"+ref, map[string]string{"mount_id": id})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listProjectReplicationPipelinesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		pipelines, err := store.ListProjectReplicationPipelines(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskReplicationPipelineConfigs(pipelines))
	}
}

func createProjectReplicationPipelineHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectReplicationPipelineInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		pipeline, err := store.CreateProjectReplicationPipeline(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "Replication pipeline configured", map[string]string{
			"pipeline_id": pipeline.ID,
			"name":        pipeline.Name,
			"type":        pipeline.Type,
			"destination": pipeline.Destination,
		})
		control.Audit(r.Context(), store, "project.replication_create", "project:"+ref, map[string]string{
			"pipeline_id": pipeline.ID,
			"name":        pipeline.Name,
			"type":        pipeline.Type,
			"destination": pipeline.Destination,
		})
		writeJSON(w, http.StatusCreated, maskReplicationPipelineConfig(pipeline))
	}
}

func deleteProjectReplicationPipelineHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		id := r.PathValue("id")
		if err := store.DeleteProjectReplicationPipeline(r.Context(), ref, id); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Replication pipeline deleted", map[string]string{"pipeline_id": id})
		control.Audit(r.Context(), store, "project.replication_delete", "project:"+ref, map[string]string{"pipeline_id": id})
		w.WriteHeader(http.StatusNoContent)
	}
}

func maskReplicationPipelineConfigs(pipelines []control.ProjectReplicationPipeline) []control.ProjectReplicationPipeline {
	out := make([]control.ProjectReplicationPipeline, len(pipelines))
	copy(out, pipelines)
	for index := range out {
		out[index] = maskReplicationPipelineConfig(out[index])
	}
	return out
}

func maskReplicationPipelineConfig(pipeline control.ProjectReplicationPipeline) control.ProjectReplicationPipeline {
	masked := map[string]string{}
	for key, value := range pipeline.Config {
		if isSensitiveLogDrainConfigKey(key) && strings.TrimSpace(value) != "" {
			masked[key] = "********"
			continue
		}
		masked[key] = value
	}
	pipeline.Config = masked
	return pipeline
}

func listProjectEmbeddingJobsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		jobs, err := store.ListProjectEmbeddingJobs(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, jobs)
	}
}

func createProjectEmbeddingJobHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectEmbeddingJobInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		job, err := store.CreateProjectEmbeddingJob(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{
			"job_id":      job.ID,
			"name":        job.Name,
			"source":      job.SourceSchema + "." + job.SourceTable + "." + job.SourceColumn,
			"provider":    job.Provider,
			"model":       job.Model,
			"destination": job.DestinationTable + "." + job.DestinationColumn,
		}
		control.LogProject(r.Context(), store, ref, "info", "Embedding job configured", metadata)
		control.Audit(r.Context(), store, "project.embedding_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, job)
	}
}

func deleteProjectEmbeddingJobHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		id := r.PathValue("id")
		if err := store.DeleteProjectEmbeddingJob(r.Context(), ref, id); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Embedding job deleted", map[string]string{"job_id": id})
		control.Audit(r.Context(), store, "project.embedding_delete", "project:"+ref, map[string]string{"job_id": id})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listProjectDatabaseExtensionsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		extensions, err := store.ListProjectDatabaseExtensions(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, extensions)
	}
}

func updateProjectDatabaseExtensionHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		name := r.PathValue("name")
		var payload control.ProjectDatabaseExtensionInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		previous, previousOK, err := getCurrentProjectDatabaseExtension(r.Context(), store, ref, name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		extension, err := store.UpdateProjectDatabaseExtension(r.Context(), ref, name, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectDatabaseExtensionUpdate(r.Context(), project, extension); err != nil {
			if previousOK {
				_, _ = store.UpdateProjectDatabaseExtension(r.Context(), ref, previous.Name, control.ProjectDatabaseExtensionInput{
					Schema:  previous.Schema,
					Version: previous.Version,
					Enabled: &previous.Enabled,
				})
			}
			metadata := map[string]string{"name": extension.Name, "schema": extension.Schema, "enabled": fmt.Sprintf("%t", extension.Enabled), "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database extension apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_extension_update_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":    extension.Name,
			"schema":  extension.Schema,
			"enabled": fmt.Sprintf("%t", extension.Enabled),
		}
		control.LogProject(r.Context(), store, ref, "info", "Database extension updated", metadata)
		control.Audit(r.Context(), store, "project.database_extension_update", "project:"+ref, metadata)
		writeJSON(w, http.StatusOK, extension)
	}
}

func getCurrentProjectDatabaseExtension(ctx context.Context, store control.Store, ref string, name string) (control.ProjectDatabaseExtension, bool, error) {
	extensions, err := store.ListProjectDatabaseExtensions(ctx, ref)
	if err != nil {
		return control.ProjectDatabaseExtension{}, false, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, extension := range extensions {
		if extension.Name == normalized {
			return extension, true, nil
		}
	}
	return control.ProjectDatabaseExtension{}, false, nil
}

func applyProjectDatabaseExtensionUpdate(ctx context.Context, project control.Project, extension control.ProjectDatabaseExtension) error {
	if !databaseRuntimeApplyEnabled() {
		return nil
	}
	if extension.Enabled {
		sql := "CREATE SCHEMA IF NOT EXISTS " + quoteDatabaseIdentifier(extension.Schema) + ";\n" +
			"CREATE EXTENSION IF NOT EXISTS " + quoteDatabaseIdentifier(extension.Name) + " WITH SCHEMA " + quoteDatabaseIdentifier(extension.Schema)
		if strings.TrimSpace(extension.Version) != "" {
			sql += " VERSION " + quoteDatabaseLiteral(extension.Version)
		}
		sql += ";\n"
		return execProjectDatabaseSQL(ctx, project, sql)
	}
	return execProjectDatabaseSQL(ctx, project, "DROP EXTENSION IF EXISTS "+quoteDatabaseIdentifier(extension.Name)+";\n")
}

func listProjectDatabaseCronJobsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		jobs, err := store.ListProjectDatabaseCronJobs(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskDatabaseCronJobs(jobs))
	}
}

func createProjectDatabaseCronJobHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		var payload control.ProjectDatabaseCronJobInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		job, err := store.CreateProjectDatabaseCronJob(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectDatabaseCronJobCreate(r.Context(), project, job); err != nil {
			_ = store.DeleteProjectDatabaseCronJob(r.Context(), ref, job.Name)
			metadata := map[string]string{"name": job.Name, "schedule": job.Schedule, "active": fmt.Sprintf("%t", job.Active), "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database cron job apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_cron_create_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":     job.Name,
			"schedule": job.Schedule,
			"active":   fmt.Sprintf("%t", job.Active),
		}
		control.LogProject(r.Context(), store, ref, "info", "Database cron job configured", metadata)
		control.Audit(r.Context(), store, "project.database_cron_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskDatabaseCronJob(job))
	}
}

func deleteProjectDatabaseCronJobHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		name := r.PathValue("name")
		job, ok, err := getCurrentProjectDatabaseCronJob(r.Context(), store, ref, name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !ok {
			if err := store.DeleteProjectDatabaseCronJob(r.Context(), ref, name); err != nil {
				writeStoreError(w, err)
			}
			return
		}
		if err := applyProjectDatabaseCronJobDelete(r.Context(), project, job); err != nil {
			metadata := map[string]string{"name": job.Name, "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database cron job delete apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_cron_delete_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProjectDatabaseCronJob(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Database cron job deleted", map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		control.Audit(r.Context(), store, "project.database_cron_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func getCurrentProjectDatabaseCronJob(ctx context.Context, store control.Store, ref string, name string) (control.ProjectDatabaseCronJob, bool, error) {
	jobs, err := store.ListProjectDatabaseCronJobs(ctx, ref)
	if err != nil {
		return control.ProjectDatabaseCronJob{}, false, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, job := range jobs {
		if job.Name == normalized {
			return job, true, nil
		}
	}
	return control.ProjectDatabaseCronJob{}, false, nil
}

func applyProjectDatabaseCronJobCreate(ctx context.Context, project control.Project, job control.ProjectDatabaseCronJob) error {
	if !databaseRuntimeApplyEnabled() || !job.Active {
		return nil
	}
	sql := "CREATE EXTENSION IF NOT EXISTS pg_cron WITH SCHEMA extensions;\n" +
		"SELECT cron.schedule_in_database(" +
		quoteDatabaseLiteral(job.Name) + ", " +
		quoteDatabaseLiteral(job.Schedule) + ", " +
		quoteDatabaseLiteral(job.Command) + ", " +
		quoteDatabaseLiteral(job.Database) + ", " +
		quoteDatabaseLiteral(job.Username) + ", true);\n"
	return execProjectDatabaseSQL(ctx, project, sql)
}

func applyProjectDatabaseCronJobDelete(ctx context.Context, project control.Project, job control.ProjectDatabaseCronJob) error {
	if !databaseRuntimeApplyEnabled() || !job.Active {
		return nil
	}
	return execProjectDatabaseSQL(ctx, project, "SELECT cron.unschedule(jobid) FROM cron.job WHERE jobname = "+quoteDatabaseLiteral(job.Name)+";\n")
}

func maskDatabaseCronJobs(jobs []control.ProjectDatabaseCronJob) []control.ProjectDatabaseCronJob {
	out := make([]control.ProjectDatabaseCronJob, len(jobs))
	copy(out, jobs)
	for index := range out {
		out[index] = maskDatabaseCronJob(out[index])
	}
	return out
}

func maskDatabaseCronJob(job control.ProjectDatabaseCronJob) control.ProjectDatabaseCronJob {
	masked := map[string]string{}
	for key, value := range job.Metadata {
		if isSensitiveLogDrainConfigKey(key) && strings.TrimSpace(value) != "" {
			masked[key] = "********"
			continue
		}
		masked[key] = value
	}
	job.Metadata = masked
	return job
}

func listProjectDatabaseQueuesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		queues, err := store.ListProjectDatabaseQueues(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskDatabaseQueues(queues))
	}
}

func createProjectDatabaseQueueHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		var payload control.ProjectDatabaseQueueInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		queue, err := store.CreateProjectDatabaseQueue(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectDatabaseQueueCreate(r.Context(), project, queue); err != nil {
			_ = store.DeleteProjectDatabaseQueue(r.Context(), ref, queue.Name)
			metadata := map[string]string{"name": queue.Name, "schema": queue.Schema, "active": fmt.Sprintf("%t", queue.Active), "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database queue apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_queue_create_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":   queue.Name,
			"schema": queue.Schema,
			"active": fmt.Sprintf("%t", queue.Active),
		}
		control.LogProject(r.Context(), store, ref, "info", "Database queue configured", metadata)
		control.Audit(r.Context(), store, "project.database_queue_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskDatabaseQueue(queue))
	}
}

func deleteProjectDatabaseQueueHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		name := r.PathValue("name")
		queue, ok, err := getCurrentProjectDatabaseQueue(r.Context(), store, ref, name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !ok {
			if err := store.DeleteProjectDatabaseQueue(r.Context(), ref, name); err != nil {
				writeStoreError(w, err)
			}
			return
		}
		if err := applyProjectDatabaseQueueDelete(r.Context(), project, queue); err != nil {
			metadata := map[string]string{"name": queue.Name, "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database queue delete apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_queue_delete_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProjectDatabaseQueue(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Database queue deleted", map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		control.Audit(r.Context(), store, "project.database_queue_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func getCurrentProjectDatabaseQueue(ctx context.Context, store control.Store, ref string, name string) (control.ProjectDatabaseQueue, bool, error) {
	queues, err := store.ListProjectDatabaseQueues(ctx, ref)
	if err != nil {
		return control.ProjectDatabaseQueue{}, false, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, queue := range queues {
		if queue.Name == normalized {
			return queue, true, nil
		}
	}
	return control.ProjectDatabaseQueue{}, false, nil
}

func applyProjectDatabaseQueueCreate(ctx context.Context, project control.Project, queue control.ProjectDatabaseQueue) error {
	if !databaseRuntimeApplyEnabled() || !queue.Active {
		return nil
	}
	sql := "CREATE SCHEMA IF NOT EXISTS " + quoteDatabaseIdentifier(queue.Schema) + ";\n" +
		"CREATE EXTENSION IF NOT EXISTS pgmq WITH SCHEMA " + quoteDatabaseIdentifier(queue.Schema) + ";\n"
	if strings.TrimSpace(queue.DeadLetterQueue) != "" {
		sql += "SELECT pgmq.create(" + quoteDatabaseLiteral(queue.DeadLetterQueue) + ");\n"
	}
	sql += "SELECT pgmq.create(" + quoteDatabaseLiteral(queue.Name) + ");\n"
	return execProjectDatabaseSQL(ctx, project, sql)
}

func applyProjectDatabaseQueueDelete(ctx context.Context, project control.Project, queue control.ProjectDatabaseQueue) error {
	if !databaseRuntimeApplyEnabled() || !queue.Active {
		return nil
	}
	sql := "SELECT pgmq.drop_queue(" + quoteDatabaseLiteral(queue.Name) + ");\n"
	if strings.TrimSpace(queue.DeadLetterQueue) != "" {
		sql += "SELECT pgmq.drop_queue(" + quoteDatabaseLiteral(queue.DeadLetterQueue) + ");\n"
	}
	return execProjectDatabaseSQL(ctx, project, sql)
}

func maskDatabaseQueues(queues []control.ProjectDatabaseQueue) []control.ProjectDatabaseQueue {
	out := make([]control.ProjectDatabaseQueue, len(queues))
	copy(out, queues)
	for index := range out {
		out[index] = maskDatabaseQueue(out[index])
	}
	return out
}

func maskDatabaseQueue(queue control.ProjectDatabaseQueue) control.ProjectDatabaseQueue {
	masked := map[string]string{}
	for key, value := range queue.Metadata {
		if isSensitiveLogDrainConfigKey(key) && strings.TrimSpace(value) != "" {
			masked[key] = "********"
			continue
		}
		masked[key] = value
	}
	queue.Metadata = masked
	return queue
}

func listProjectDatabaseWebhooksHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		webhooks, err := store.ListProjectDatabaseWebhooks(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskDatabaseWebhooks(webhooks))
	}
}

func createProjectDatabaseWebhookHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		var payload control.ProjectDatabaseWebhookInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		webhook, err := store.CreateProjectDatabaseWebhook(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectDatabaseWebhookCreate(r.Context(), store, project, webhook); err != nil {
			_ = store.DeleteProjectDatabaseWebhook(r.Context(), ref, webhook.Name)
			metadata := map[string]string{"name": webhook.Name, "table": webhook.Schema + "." + webhook.Table, "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database webhook apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_webhook_create_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":   webhook.Name,
			"table":  webhook.Schema + "." + webhook.Table,
			"active": fmt.Sprintf("%t", webhook.Active),
		}
		control.LogProject(r.Context(), store, ref, "info", "Database webhook configured", metadata)
		control.Audit(r.Context(), store, "project.database_webhook_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskDatabaseWebhook(webhook))
	}
}

func deleteProjectDatabaseWebhookHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		name := r.PathValue("name")
		webhook, ok, err := getCurrentProjectDatabaseWebhook(r.Context(), store, ref, name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !ok {
			if err := store.DeleteProjectDatabaseWebhook(r.Context(), ref, name); err != nil {
				writeStoreError(w, err)
			}
			return
		}
		if err := applyProjectDatabaseWebhookDelete(r.Context(), project, webhook); err != nil {
			metadata := map[string]string{"name": webhook.Name, "table": webhook.Schema + "." + webhook.Table, "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database webhook delete apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_webhook_delete_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProjectDatabaseWebhook(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Database webhook deleted", map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		control.Audit(r.Context(), store, "project.database_webhook_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func getCurrentProjectDatabaseWebhook(ctx context.Context, store control.Store, ref string, name string) (control.ProjectDatabaseWebhook, bool, error) {
	webhooks, err := store.ListProjectDatabaseWebhooks(ctx, ref)
	if err != nil {
		return control.ProjectDatabaseWebhook{}, false, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, webhook := range webhooks {
		if webhook.Name == normalized {
			return webhook, true, nil
		}
	}
	return control.ProjectDatabaseWebhook{}, false, nil
}

func applyProjectDatabaseWebhookCreate(ctx context.Context, store control.Store, project control.Project, webhook control.ProjectDatabaseWebhook) error {
	if !databaseRuntimeApplyEnabled() || !webhook.Active {
		return nil
	}
	if webhook.HTTPMethod != "POST" {
		return fmt.Errorf("active database webhooks currently require http_method POST for pg_net delivery")
	}
	headers, err := databaseWebhookRuntimeHeaders(ctx, store, project.Ref, webhook)
	if err != nil {
		return err
	}
	headersJSON, err := json.Marshal(headers)
	if err != nil {
		return err
	}
	functionName := databaseWebhookFunctionName(webhook)
	sql := "CREATE SCHEMA IF NOT EXISTS supadupa;\n" +
		"CREATE EXTENSION IF NOT EXISTS pg_net;\n" +
		"CREATE OR REPLACE FUNCTION supadupa." + quoteDatabaseIdentifier(functionName) + "()\n" +
		"RETURNS trigger\n" +
		"LANGUAGE plpgsql\n" +
		"SECURITY DEFINER\n" +
		"SET search_path = public, net, pg_temp\n" +
		"AS $function$\n" +
		"DECLARE\n" +
		"  request_payload jsonb;\n" +
		"  request_headers jsonb := " + quoteDatabaseLiteral(string(headersJSON)) + "::jsonb;\n" +
		"BEGIN\n" +
		"  request_payload := jsonb_build_object(\n" +
		"    'type', TG_OP,\n" +
		"    'table', TG_TABLE_NAME,\n" +
		"    'schema', TG_TABLE_SCHEMA,\n" +
		"    'record', CASE WHEN TG_OP = 'DELETE' THEN NULL ELSE to_jsonb(NEW) END,\n" +
		"    'old_record', CASE WHEN TG_OP IN ('UPDATE', 'DELETE') THEN to_jsonb(OLD) ELSE NULL END\n" +
		"  );\n" +
		"  PERFORM net.http_post(\n" +
		"    url := " + quoteDatabaseLiteral(webhook.Endpoint) + ",\n" +
		"    body := request_payload,\n" +
		"    headers := request_headers,\n" +
		"    timeout_milliseconds := " + strconv.Itoa(webhook.TimeoutSeconds*1000) + "\n" +
		"  );\n" +
		"  RETURN COALESCE(NEW, OLD);\n" +
		"END;\n" +
		"$function$;\n"
	for _, event := range webhook.Events {
		sql += "DROP TRIGGER IF EXISTS " + quoteDatabaseIdentifier(databaseWebhookTriggerName(webhook, event)) + " ON " + quoteDatabaseIdentifier(webhook.Schema) + "." + quoteDatabaseIdentifier(webhook.Table) + ";\n" +
			"CREATE TRIGGER " + quoteDatabaseIdentifier(databaseWebhookTriggerName(webhook, event)) + "\n" +
			"AFTER " + strings.ToUpper(event) + " ON " + quoteDatabaseIdentifier(webhook.Schema) + "." + quoteDatabaseIdentifier(webhook.Table) + "\n" +
			"FOR EACH ROW EXECUTE FUNCTION supadupa." + quoteDatabaseIdentifier(functionName) + "();\n"
	}
	return execProjectDatabaseSQL(ctx, project, sql)
}

func applyProjectDatabaseWebhookDelete(ctx context.Context, project control.Project, webhook control.ProjectDatabaseWebhook) error {
	if !databaseRuntimeApplyEnabled() || !webhook.Active {
		return nil
	}
	tableName := quoteDatabaseIdentifier(webhook.Schema) + "." + quoteDatabaseIdentifier(webhook.Table)
	sql := ""
	for _, event := range webhook.Events {
		sql += "DO $drop_webhook$\n" +
			"BEGIN\n" +
			"  IF to_regclass(" + quoteDatabaseLiteral(webhook.Schema+"."+webhook.Table) + ") IS NOT NULL THEN\n" +
			"    EXECUTE " + quoteDatabaseLiteral("DROP TRIGGER IF EXISTS "+quoteDatabaseIdentifier(databaseWebhookTriggerName(webhook, event))+" ON "+tableName) + ";\n" +
			"  END IF;\n" +
			"END\n" +
			"$drop_webhook$;\n"
	}
	sql += "DROP FUNCTION IF EXISTS supadupa." + quoteDatabaseIdentifier(databaseWebhookFunctionName(webhook)) + "();\n"
	return execProjectDatabaseSQL(ctx, project, sql)
}

func databaseWebhookRuntimeHeaders(ctx context.Context, store control.Store, ref string, webhook control.ProjectDatabaseWebhook) (map[string]string, error) {
	headers := map[string]string{"Content-Type": "application/json"}
	for key, value := range webhook.Headers {
		trimmed := strings.TrimSpace(value)
		if isSensitiveLogDrainConfigKey(key) && trimmed != "" {
			resolved, err := resolveProjectSecretHandleValue(ctx, store, ref, trimmed, "webhook header "+key)
			if err != nil {
				return nil, err
			}
			headers[key] = resolved
			continue
		}
		headers[key] = trimmed
	}
	return headers, nil
}

func databaseWebhookFunctionName(webhook control.ProjectDatabaseWebhook) string {
	return "webhook_" + strings.ReplaceAll(webhook.Name, "-", "_")
}

func databaseWebhookTriggerName(webhook control.ProjectDatabaseWebhook, event string) string {
	return "supadupa_webhook_" + strings.ReplaceAll(webhook.Name, "-", "_") + "_" + strings.ToLower(event)
}

func maskDatabaseWebhooks(webhooks []control.ProjectDatabaseWebhook) []control.ProjectDatabaseWebhook {
	out := make([]control.ProjectDatabaseWebhook, len(webhooks))
	copy(out, webhooks)
	for index := range out {
		out[index] = maskDatabaseWebhook(out[index])
	}
	return out
}

func maskDatabaseWebhook(webhook control.ProjectDatabaseWebhook) control.ProjectDatabaseWebhook {
	headers := map[string]string{}
	for key, value := range webhook.Headers {
		if isSensitiveLogDrainConfigKey(key) && strings.TrimSpace(value) != "" {
			headers[key] = "********"
			continue
		}
		headers[key] = value
	}
	metadata := map[string]string{}
	for key, value := range webhook.Metadata {
		if isSensitiveLogDrainConfigKey(key) && strings.TrimSpace(value) != "" {
			metadata[key] = "********"
			continue
		}
		metadata[key] = value
	}
	webhook.Headers = headers
	webhook.Metadata = metadata
	return webhook
}

func listProjectDatabaseSchemasHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		schemas, err := store.ListProjectDatabaseSchemas(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskDatabaseSchemas(schemas))
	}
}

func createProjectDatabaseSchemaHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		var payload control.ProjectDatabaseSchemaInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		schema, err := store.CreateProjectDatabaseSchema(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectDatabaseSchemaCreate(r.Context(), project, schema); err != nil {
			_ = store.DeleteProjectDatabaseSchema(r.Context(), ref, schema.Name, schema.Version)
			metadata := map[string]string{"name": schema.Name, "version": schema.Version, "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Declarative schema apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_schema_create_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":     schema.Name,
			"version":  schema.Version,
			"schema":   schema.Schema,
			"checksum": schema.Checksum,
			"active":   fmt.Sprintf("%t", schema.Active),
		}
		control.LogProject(r.Context(), store, ref, "info", "Declarative schema recorded", metadata)
		control.Audit(r.Context(), store, "project.database_schema_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskDatabaseSchema(schema))
	}
}

func applyProjectDatabaseSchemaCreate(ctx context.Context, project control.Project, schema control.ProjectDatabaseSchema) error {
	if !databaseRuntimeApplyEnabled() || !schema.Active {
		return nil
	}
	return execProjectDatabaseSQL(ctx, project, schema.SQL)
}

func databaseRuntimeApplyEnabled() bool {
	if value := strings.TrimSpace(os.Getenv("SUPADUPA_DATABASE_APPLY")); value != "" {
		return envBoolValue(value)
	}
	return envBoolValue(os.Getenv("SUPADUPA_COMPOSE_APPLY"))
}

func execProjectDatabaseSQL(ctx context.Context, project control.Project, sql string) error {
	if strings.TrimSpace(sql) == "" {
		return fmt.Errorf("database schema sql is required")
	}
	timeout := databaseApplyTimeout()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	projectDir := filepath.Join(envOrDefault("SUPADUPA_PROJECT_ROOT", "./runtime/projects"), project.Ref)
	composeFile := filepath.Join(projectDir, "compose.yaml")
	commandParts := strings.Fields(envOrDefault("SUPADUPA_COMPOSE_COMMAND", "docker compose"))
	if len(commandParts) == 0 {
		return fmt.Errorf("SUPADUPA_COMPOSE_COMMAND is empty")
	}
	args := append(commandParts[1:], "-p", project.Ref, "-f", composeFile, "exec", "-T", "db", "sh", "-c", `PGPASSWORD="$POSTGRES_PASSWORD" exec psql -v ON_ERROR_STOP=1 -U supabase_admin -d postgres`)
	cmd := exec.CommandContext(ctx, commandParts[0], args...)
	cmd.Dir = projectDir
	cmd.Stdin = strings.NewReader(sql)
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if len(detail) > 4096 {
			detail = detail[:4096]
		}
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("database SQL apply failed: %s", detail)
	}
	return nil
}

func quoteDatabaseIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteDatabaseLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func databaseApplyTimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv("SUPADUPA_DATABASE_APPLY_TIMEOUT"))
	if value == "" {
		return time.Minute
	}
	duration, err := time.ParseDuration(value)
	if err == nil {
		return duration
	}
	seconds, err := strconv.Atoi(value)
	if err == nil {
		return time.Duration(seconds) * time.Second
	}
	return time.Minute
}

func envOrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func deleteProjectDatabaseSchemaHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		name := r.PathValue("name")
		version := r.PathValue("version")
		if err := store.DeleteProjectDatabaseSchema(r.Context(), ref, name, version); err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{"name": strings.ToLower(strings.TrimSpace(name)), "version": strings.TrimSpace(version)}
		control.LogProject(r.Context(), store, ref, "warning", "Declarative schema deleted", metadata)
		control.Audit(r.Context(), store, "project.database_schema_delete", "project:"+ref, metadata)
		w.WriteHeader(http.StatusNoContent)
	}
}

func maskDatabaseSchemas(schemas []control.ProjectDatabaseSchema) []control.ProjectDatabaseSchema {
	out := make([]control.ProjectDatabaseSchema, len(schemas))
	copy(out, schemas)
	for index := range out {
		out[index] = maskDatabaseSchema(out[index])
	}
	return out
}

func maskDatabaseSchema(schema control.ProjectDatabaseSchema) control.ProjectDatabaseSchema {
	masked := map[string]string{}
	for key, value := range schema.Metadata {
		if isSensitiveLogDrainConfigKey(key) && strings.TrimSpace(value) != "" {
			masked[key] = "********"
			continue
		}
		masked[key] = value
	}
	schema.Metadata = masked
	return schema
}

func listProjectDatabaseRolesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		roles, err := store.ListProjectDatabaseRoles(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskDatabaseRoles(roles))
	}
}

func createProjectDatabaseRoleHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		var payload control.ProjectDatabaseRoleInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		role, err := store.CreateProjectDatabaseRole(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectDatabaseRoleCreate(r.Context(), store, project, role); err != nil {
			_ = store.DeleteProjectDatabaseRole(r.Context(), ref, role.Name)
			metadata := map[string]string{"name": role.Name, "login": fmt.Sprintf("%t", role.Login), "bypass_rls": fmt.Sprintf("%t", role.BypassRLS), "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database role apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_role_create_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":       role.Name,
			"login":      fmt.Sprintf("%t", role.Login),
			"bypass_rls": fmt.Sprintf("%t", role.BypassRLS),
		}
		control.LogProject(r.Context(), store, ref, "info", "Database role configured", metadata)
		control.Audit(r.Context(), store, "project.database_role_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskDatabaseRole(role))
	}
}

func deleteProjectDatabaseRoleHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		name := r.PathValue("name")
		role, ok, err := getCurrentProjectDatabaseRole(r.Context(), store, ref, name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !ok {
			if err := store.DeleteProjectDatabaseRole(r.Context(), ref, name); err != nil {
				writeStoreError(w, err)
			}
			return
		}
		if err := applyProjectDatabaseRoleDelete(r.Context(), project, role); err != nil {
			metadata := map[string]string{"name": role.Name, "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database role delete apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_role_delete_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProjectDatabaseRole(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Database role deleted", map[string]string{"name": name})
		control.Audit(r.Context(), store, "project.database_role_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func getCurrentProjectDatabaseRole(ctx context.Context, store control.Store, ref string, name string) (control.ProjectDatabaseRole, bool, error) {
	roles, err := store.ListProjectDatabaseRoles(ctx, ref)
	if err != nil {
		return control.ProjectDatabaseRole{}, false, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, role := range roles {
		if role.Name == normalized {
			return role, true, nil
		}
	}
	return control.ProjectDatabaseRole{}, false, nil
}

func applyProjectDatabaseRoleCreate(ctx context.Context, store control.Store, project control.Project, role control.ProjectDatabaseRole) error {
	if !databaseRuntimeApplyEnabled() {
		return nil
	}
	password := ""
	if role.Login {
		resolved, err := resolveProjectSecretHandle(ctx, store, project.Ref, role.PasswordSecretHandle)
		if err != nil {
			return err
		}
		password = resolved
	}
	sql := "DO $$ BEGIN\n" +
		"IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = " + quoteDatabaseLiteral(role.Name) + ") THEN\n" +
		"CREATE ROLE " + quoteDatabaseIdentifier(role.Name) + ";\n" +
		"END IF;\n" +
		"END $$;\n" +
		"ALTER ROLE " + quoteDatabaseIdentifier(role.Name)
	if role.Login {
		sql += " LOGIN PASSWORD " + quoteDatabaseLiteral(password)
	} else {
		sql += " NOLOGIN"
	}
	if role.Inherit {
		sql += " INHERIT"
	} else {
		sql += " NOINHERIT"
	}
	if role.BypassRLS {
		sql += " BYPASSRLS"
	} else {
		sql += " NOBYPASSRLS"
	}
	if role.ConnectionLimit != 0 {
		sql += " CONNECTION LIMIT " + strconv.Itoa(role.ConnectionLimit)
	}
	sql += ";\n"
	for _, member := range role.MemberOf {
		sql += "GRANT " + quoteDatabaseIdentifier(member) + " TO " + quoteDatabaseIdentifier(role.Name) + ";\n"
	}
	for schema, grants := range role.SchemaGrants {
		schemaPrivileges, tablePrivileges := databaseRoleGrantPrivileges(grants)
		if len(schemaPrivileges) > 0 {
			sql += "GRANT " + strings.Join(schemaPrivileges, ", ") + " ON SCHEMA " + quoteDatabaseIdentifier(schema) + " TO " + quoteDatabaseIdentifier(role.Name) + ";\n"
		}
		if len(tablePrivileges) > 0 {
			sql += "GRANT " + strings.Join(tablePrivileges, ", ") + " ON ALL TABLES IN SCHEMA " + quoteDatabaseIdentifier(schema) + " TO " + quoteDatabaseIdentifier(role.Name) + ";\n"
		}
	}
	return execProjectDatabaseSQL(ctx, project, sql)
}

func applyProjectDatabaseRoleDelete(ctx context.Context, project control.Project, role control.ProjectDatabaseRole) error {
	if !databaseRuntimeApplyEnabled() {
		return nil
	}
	sql := ""
	for schema, grants := range role.SchemaGrants {
		schemaPrivileges, tablePrivileges := databaseRoleGrantPrivileges(grants)
		if len(tablePrivileges) > 0 {
			sql += "REVOKE " + strings.Join(tablePrivileges, ", ") + " ON ALL TABLES IN SCHEMA " + quoteDatabaseIdentifier(schema) + " FROM " + quoteDatabaseIdentifier(role.Name) + ";\n"
		}
		if len(schemaPrivileges) > 0 {
			sql += "REVOKE " + strings.Join(schemaPrivileges, ", ") + " ON SCHEMA " + quoteDatabaseIdentifier(schema) + " FROM " + quoteDatabaseIdentifier(role.Name) + ";\n"
		}
	}
	for _, member := range role.MemberOf {
		sql += "REVOKE " + quoteDatabaseIdentifier(member) + " FROM " + quoteDatabaseIdentifier(role.Name) + ";\n"
	}
	sql += "DROP ROLE IF EXISTS " + quoteDatabaseIdentifier(role.Name) + ";\n"
	return execProjectDatabaseSQL(ctx, project, sql)
}

func databaseRoleGrantPrivileges(grants string) ([]string, []string) {
	schemaPrivileges := []string{}
	tablePrivileges := []string{}
	for _, grant := range strings.Split(grants, ",") {
		switch strings.ToLower(strings.TrimSpace(grant)) {
		case "usage":
			schemaPrivileges = append(schemaPrivileges, "USAGE")
		case "create":
			schemaPrivileges = append(schemaPrivileges, "CREATE")
		case "select":
			tablePrivileges = append(tablePrivileges, "SELECT")
		case "insert":
			tablePrivileges = append(tablePrivileges, "INSERT")
		case "update":
			tablePrivileges = append(tablePrivileges, "UPDATE")
		case "delete":
			tablePrivileges = append(tablePrivileges, "DELETE")
		case "all":
			schemaPrivileges = append(schemaPrivileges, "ALL PRIVILEGES")
			tablePrivileges = append(tablePrivileges, "ALL PRIVILEGES")
		}
	}
	return schemaPrivileges, tablePrivileges
}

func resolveProjectSecretHandle(ctx context.Context, store control.Store, ref string, handle string) (string, error) {
	return resolveProjectSecretHandleValue(ctx, store, ref, handle, "password_secret_handle")
}

func resolveProjectSecretHandleValue(ctx context.Context, store control.Store, ref string, handle string, label string) (string, error) {
	prefix := "secret://projects/" + ref + "/"
	if !strings.HasPrefix(handle, prefix) {
		return "", fmt.Errorf("%s must reference project %s", label, ref)
	}
	kind := strings.TrimSpace(strings.TrimPrefix(handle, prefix))
	if strings.Contains(kind, "/") || kind == "" {
		return "", fmt.Errorf("%s %s is not revealable by this control plane", label, handle)
	}
	secret, err := store.RevealProjectSecret(ctx, ref, kind)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(secret.Value) == "" {
		return "", fmt.Errorf("%s %s has no value", label, handle)
	}
	return secret.Value, nil
}

func maskDatabaseRoles(roles []control.ProjectDatabaseRole) []control.ProjectDatabaseRole {
	out := make([]control.ProjectDatabaseRole, len(roles))
	copy(out, roles)
	for index := range out {
		out[index] = maskDatabaseRole(out[index])
	}
	return out
}

func maskDatabaseRole(role control.ProjectDatabaseRole) control.ProjectDatabaseRole {
	if strings.TrimSpace(role.PasswordSecretHandle) != "" {
		role.PasswordSecretHandle = "********"
	}
	masked := map[string]string{}
	for key, value := range role.Metadata {
		if isSensitiveLogDrainConfigKey(key) && strings.TrimSpace(value) != "" {
			masked[key] = "********"
			continue
		}
		masked[key] = value
	}
	role.Metadata = masked
	return role
}

func listProjectStorageBucketsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		buckets, err := store.ListProjectStorageBuckets(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskStorageBuckets(buckets))
	}
}

func createProjectStorageBucketHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		var payload control.ProjectStorageBucketInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		bucket, err := store.CreateProjectStorageBucket(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectStorageBucketCreate(r.Context(), store, project, bucket); err != nil {
			_ = store.DeleteProjectStorageBucket(r.Context(), ref, bucket.Name)
			control.LogProject(r.Context(), store, ref, "error", "Storage bucket data-plane create failed", map[string]string{"name": bucket.Name, "error": err.Error()})
			control.Audit(r.Context(), store, "project.storage_bucket_create_failed", "project:"+ref, map[string]string{"name": bucket.Name, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":            bucket.Name,
			"public":          fmt.Sprintf("%t", bucket.Public),
			"file_size_limit": fmt.Sprintf("%d", bucket.FileSizeLimit),
		}
		control.LogProject(r.Context(), store, ref, "info", "Storage bucket configured", metadata)
		control.Audit(r.Context(), store, "project.storage_bucket_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskStorageBucket(bucket))
	}
}

func deleteProjectStorageBucketHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		name := r.PathValue("name")
		if err := applyProjectStorageBucketDelete(r.Context(), store, project, name); err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Storage bucket data-plane delete failed", map[string]string{"name": strings.ToLower(strings.TrimSpace(name)), "error": err.Error()})
			control.Audit(r.Context(), store, "project.storage_bucket_delete_failed", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name)), "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProjectStorageBucket(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Storage bucket deleted", map[string]string{"name": name})
		control.Audit(r.Context(), store, "project.storage_bucket_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func applyProjectStorageBucketCreate(ctx context.Context, store control.Store, project control.Project, bucket control.ProjectStorageBucket) error {
	if !storageDataPlaneApplyEnabled() {
		return nil
	}
	payload := map[string]any{
		"id":                 bucket.Name,
		"name":               bucket.Name,
		"public":             bucket.Public,
		"file_size_limit":    bucket.FileSizeLimit,
		"allowed_mime_types": bucket.AllowedMimeTypes,
		"avif_autodetection": bucket.AvifAutodetection,
	}
	if strings.TrimSpace(bucket.CacheControl) != "" {
		payload["cache_control"] = bucket.CacheControl
	}
	return projectStorageDataPlaneRequest(ctx, store, project, http.MethodPost, "/storage/v1/bucket", payload, false)
}

func applyProjectStorageBucketDelete(ctx context.Context, store control.Store, project control.Project, name string) error {
	if !storageDataPlaneApplyEnabled() {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return fmt.Errorf("storage bucket name is required")
	}
	return projectStorageDataPlaneRequest(ctx, store, project, http.MethodDelete, "/storage/v1/bucket/"+url.PathEscape(normalized), nil, true)
}

func storageDataPlaneApplyEnabled() bool {
	if value := strings.TrimSpace(os.Getenv("SUPADUPA_STORAGE_APPLY")); value != "" {
		return envBoolValue(value)
	}
	return envBoolValue(os.Getenv("SUPADUPA_COMPOSE_APPLY")) || envBoolValue(os.Getenv("SUPADUPA_K8S_APPLY"))
}

func envBoolValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func envFalseValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "n", "off":
		return true
	default:
		return false
	}
}

func projectStorageDataPlaneRequest(ctx context.Context, store control.Store, project control.Project, method string, path string, payload any, allowNotFound bool) error {
	serviceRole, err := projectServiceRoleKey(ctx, store, project.Ref)
	if err != nil {
		return err
	}
	var body io.Reader = http.NoBody
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, projectStorageDataPlaneBaseURL(project)+path, body)
	if err != nil {
		return err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("apikey", serviceRole)
	request.Header.Set("Authorization", "Bearer "+serviceRole)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("storage data-plane request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	detail, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if allowNotFound && storageDataPlaneNotFound(response.StatusCode, detail) {
		return nil
	}
	return fmt.Errorf("storage data-plane %s %s returned HTTP %d: %s", method, path, response.StatusCode, strings.TrimSpace(string(detail)))
}

func storageDataPlaneNotFound(status int, detail []byte) bool {
	if status == http.StatusNotFound {
		return true
	}
	if status != http.StatusBadRequest {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(detail, &payload); err != nil {
		return false
	}
	statusCode := strings.TrimSpace(fmt.Sprint(payload["statusCode"]))
	message := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["message"])))
	return statusCode == "404" || strings.Contains(message, "bucket not found")
}

func projectServiceRoleKey(ctx context.Context, store control.Store, ref string) (string, error) {
	secrets, err := store.EnsureProjectSecrets(ctx, ref)
	if err != nil {
		return "", err
	}
	for _, secret := range secrets {
		if secret.Kind == "service_role" && strings.TrimSpace(secret.Value) != "" {
			return secret.Value, nil
		}
	}
	return "", fmt.Errorf("project %s service_role key is not available", ref)
}

func projectStorageDataPlaneBaseURL(project control.Project) string {
	if configured := strings.TrimRight(strings.TrimSpace(os.Getenv("SUPADUPA_STORAGE_APPLY_BASE_URL")), "/"); configured != "" {
		configured = strings.ReplaceAll(configured, "{{ref}}", project.Ref)
		configured = strings.ReplaceAll(configured, "{{project_ref}}", project.Ref)
		configured = strings.ReplaceAll(configured, "{{domain}}", project.Spec.Domain)
		return configured
	}
	return fmt.Sprintf("https://%s.%s", project.Ref, project.Spec.Domain)
}

func maskStorageBuckets(buckets []control.ProjectStorageBucket) []control.ProjectStorageBucket {
	out := make([]control.ProjectStorageBucket, len(buckets))
	copy(out, buckets)
	for index := range out {
		out[index] = maskStorageBucket(out[index])
	}
	return out
}

func maskStorageBucket(bucket control.ProjectStorageBucket) control.ProjectStorageBucket {
	masked := map[string]string{}
	for key, value := range bucket.Metadata {
		if isSensitiveLogDrainConfigKey(key) && strings.TrimSpace(value) != "" {
			masked[key] = "********"
			continue
		}
		masked[key] = value
	}
	bucket.Metadata = masked
	return bucket
}

func listProjectVectorBucketsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		buckets, err := store.ListProjectVectorBuckets(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskVectorBuckets(buckets))
	}
}

func createProjectVectorBucketHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectVectorBucketInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		bucket, err := store.CreateProjectVectorBucket(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{
			"name":            bucket.Name,
			"dimension":       fmt.Sprintf("%d", bucket.Dimension),
			"distance":        bucket.Distance,
			"storage_backend": bucket.StorageBackend,
		}
		control.LogProject(r.Context(), store, ref, "info", "Vector bucket configured", metadata)
		control.Audit(r.Context(), store, "project.vector_bucket_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskVectorBucket(bucket))
	}
}

func deleteProjectVectorBucketHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		name := r.PathValue("name")
		if err := store.DeleteProjectVectorBucket(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Vector bucket deleted", map[string]string{"name": name})
		control.Audit(r.Context(), store, "project.vector_bucket_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func maskVectorBuckets(buckets []control.ProjectVectorBucket) []control.ProjectVectorBucket {
	out := make([]control.ProjectVectorBucket, len(buckets))
	copy(out, buckets)
	for index := range out {
		out[index] = maskVectorBucket(out[index])
	}
	return out
}

func maskVectorBucket(bucket control.ProjectVectorBucket) control.ProjectVectorBucket {
	masked := map[string]string{}
	for key, value := range bucket.Metadata {
		if isSensitiveLogDrainConfigKey(key) && strings.TrimSpace(value) != "" {
			masked[key] = "********"
			continue
		}
		masked[key] = value
	}
	bucket.Metadata = masked
	return bucket
}

func listProjectAnalyticsBucketsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		buckets, err := store.ListProjectAnalyticsBuckets(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskAnalyticsBuckets(buckets))
	}
}

func createProjectAnalyticsBucketHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectAnalyticsBucketInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		bucket, err := store.CreateProjectAnalyticsBucket(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{
			"name":           bucket.Name,
			"storage_uri":    bucket.StorageURI,
			"format_version": fmt.Sprintf("%d", bucket.FormatVersion),
			"warehouse":      bucket.Warehouse,
		}
		control.LogProject(r.Context(), store, ref, "info", "Analytics bucket configured", metadata)
		control.Audit(r.Context(), store, "project.analytics_bucket_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskAnalyticsBucket(bucket))
	}
}

func deleteProjectAnalyticsBucketHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		name := r.PathValue("name")
		if err := store.DeleteProjectAnalyticsBucket(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Analytics bucket deleted", map[string]string{"name": name})
		control.Audit(r.Context(), store, "project.analytics_bucket_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func maskAnalyticsBuckets(buckets []control.ProjectAnalyticsBucket) []control.ProjectAnalyticsBucket {
	out := make([]control.ProjectAnalyticsBucket, len(buckets))
	copy(out, buckets)
	for index := range out {
		out[index] = maskAnalyticsBucket(out[index])
	}
	return out
}

func maskAnalyticsBucket(bucket control.ProjectAnalyticsBucket) control.ProjectAnalyticsBucket {
	if strings.TrimSpace(bucket.CredentialHandle) != "" {
		bucket.CredentialHandle = "********"
	}
	masked := map[string]string{}
	for key, value := range bucket.Metadata {
		if isSensitiveLogDrainConfigKey(key) && strings.TrimSpace(value) != "" {
			masked[key] = "********"
			continue
		}
		masked[key] = value
	}
	bucket.Metadata = masked
	return bucket
}

func getProjectCDNPolicyHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		policy, err := store.GetProjectCDNPolicy(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, policy)
	}
}

func updateProjectCDNPolicyHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectCDNPolicyInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		policy, err := store.UpdateProjectCDNPolicy(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"enabled":    fmt.Sprintf("%t", policy.Enabled),
			"smart":      fmt.Sprintf("%t", policy.SmartRevalidation),
			"route_path": routePath,
		}
		control.LogProject(r.Context(), store, ref, "info", "CDN policy updated", metadata)
		control.Audit(r.Context(), store, "project.cdn_policy_update", "project:"+ref, metadata)
		writeJSON(w, http.StatusOK, policy)
	}
}

func listProjectCDNInvalidationsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		invalidations, err := store.ListProjectCDNInvalidations(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, invalidations)
	}
}

func createProjectCDNInvalidationHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.CDNInvalidationInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		invalidation, err := store.CreateProjectCDNInvalidation(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{
			"invalidation_id": invalidation.ID,
			"paths":           strings.Join(invalidation.Paths, ","),
			"status":          invalidation.Status,
		}
		control.LogProject(r.Context(), store, ref, "info", "CDN invalidation completed", metadata)
		control.Audit(r.Context(), store, "project.cdn_invalidate", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, invalidation)
	}
}

func createProjectCDNObjectEventHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.CDNObjectEventInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		invalidation, err := store.CreateProjectCDNObjectEvent(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{
			"invalidation_id": invalidation.ID,
			"event_id":        invalidation.EventID,
			"paths":           strings.Join(invalidation.Paths, ","),
			"source":          invalidation.Source,
			"status":          invalidation.Status,
		}
		control.LogProject(r.Context(), store, ref, "info", "Smart CDN object-change revalidation completed", metadata)
		control.Audit(r.Context(), store, "project.cdn_object_revalidate", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, invalidation)
	}
}

func listProjectNetworkConnectionsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		connections, err := store.ListProjectNetworkConnections(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskNetworkConnections(connections))
	}
}

func getProjectNetworkHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		config, err := store.GetProjectConfig(r.Context(), ref, "network")
		if err != nil {
			writeStoreError(w, err)
			return
		}
		connections, err := store.ListProjectNetworkConnections(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"project_ref":  ref,
			"config":       config,
			"connections":  maskNetworkConnections(connections),
			"allowlist":    strings.TrimSpace(config.Config["ip_allowlist"]),
			"ssl_enforced": strings.TrimSpace(config.Config["ssl_enforced"]),
		})
	}
}

func createProjectNetworkConnectionHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "network_restrictions") {
			return
		}
		var payload control.ProjectNetworkConnectionInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		connection, err := store.CreateProjectNetworkConnection(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{
			"connection_id": connection.ID,
			"name":          connection.Name,
			"type":          connection.Type,
			"provider":      connection.Provider,
			"cidrs":         strings.Join(connection.CIDRs, ","),
		}
		control.LogProject(r.Context(), store, ref, "info", "Private network connection requested", metadata)
		control.Audit(r.Context(), store, "project.network_connection_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskNetworkConnection(connection))
	}
}

func deleteProjectNetworkConnectionHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		id := r.PathValue("id")
		if err := store.DeleteProjectNetworkConnection(r.Context(), ref, id); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Private network connection removed", map[string]string{"connection_id": id})
		control.Audit(r.Context(), store, "project.network_connection_delete", "project:"+ref, map[string]string{"connection_id": id})
		w.WriteHeader(http.StatusNoContent)
	}
}

func maskNetworkConnections(connections []control.ProjectNetworkConnection) []control.ProjectNetworkConnection {
	out := make([]control.ProjectNetworkConnection, len(connections))
	copy(out, connections)
	for index := range out {
		out[index] = maskNetworkConnection(out[index])
	}
	return out
}

func maskNetworkConnection(connection control.ProjectNetworkConnection) control.ProjectNetworkConnection {
	masked := map[string]string{}
	for key, value := range connection.Config {
		if isSensitiveLogDrainConfigKey(key) && strings.TrimSpace(value) != "" {
			masked[key] = "********"
			continue
		}
		masked[key] = value
	}
	connection.Config = masked
	return connection
}

func listProjectLogDrainsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		drains, err := store.ListProjectLogDrains(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskLogDrainConfigs(drains))
	}
}

func createProjectLogDrainHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "log_drains") {
			return
		}
		var payload control.LogDrainInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		drain, err := store.CreateProjectLogDrain(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		artifact, err := control.NewLogDrainDeploymentService().Deploy(r.Context(), drain)
		if err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Log drain deployment failed", map[string]string{"target": drain.Target, "id": drain.ID, "error": err.Error()})
			control.Audit(r.Context(), store, "project.log_drain_deploy_failed", "project:"+ref, map[string]string{"target": drain.Target, "id": drain.ID, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "Log drain created", map[string]string{"target": drain.Target, "id": drain.ID, "artifact_path": artifact.Path})
		control.Audit(r.Context(), store, "project.log_drain_create", "project:"+ref, map[string]string{"target": drain.Target, "id": drain.ID, "artifact_path": artifact.Path})
		writeJSON(w, http.StatusCreated, maskLogDrainConfig(drain))
	}
}

func deleteProjectLogDrainHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		id := r.PathValue("id")
		if err := store.DeleteProjectLogDrain(r.Context(), ref, id); err != nil {
			writeStoreError(w, err)
			return
		}
		if err := control.NewLogDrainDeploymentService().Delete(r.Context(), ref, id); err != nil && !errors.Is(err, os.ErrNotExist) {
			control.LogProject(r.Context(), store, ref, "error", "Log drain artifact delete failed", map[string]string{"id": id, "error": err.Error()})
			control.Audit(r.Context(), store, "project.log_drain_delete_failed", "project:"+ref, map[string]string{"id": id, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Log drain deleted", map[string]string{"id": id})
		control.Audit(r.Context(), store, "project.log_drain_delete", "project:"+ref, map[string]string{"id": id})
		w.WriteHeader(http.StatusNoContent)
	}
}

func maskLogDrainConfigs(drains []control.LogDrain) []control.LogDrain {
	out := make([]control.LogDrain, len(drains))
	copy(out, drains)
	for index := range out {
		out[index] = maskLogDrainConfig(out[index])
	}
	return out
}

func maskLogDrainConfig(drain control.LogDrain) control.LogDrain {
	masked := map[string]string{}
	for key, value := range drain.Config {
		if isSensitiveLogDrainConfigKey(key) && strings.TrimSpace(value) != "" {
			masked[key] = "********"
			continue
		}
		masked[key] = value
	}
	drain.Config = masked
	return drain
}

func isSensitiveLogDrainConfigKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "api_key", "token", "secret", "password", "access_key", "secret_key", "access_token", "bearer_token", "authorization", "x-api-key":
		return true
	default:
		return false
	}
}

func listProjectSecretsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		secrets, err := store.ListProjectSecrets(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, secrets)
	}
}

func revealProjectSecretHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		kind := r.PathValue("kind")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		secret, err := store.RevealProjectSecret(r.Context(), ref, kind)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Secret revealed", map[string]string{"kind": kind})
		control.Audit(r.Context(), store, "project.secret_reveal", "project:"+ref, map[string]string{"kind": kind})
		writeJSON(w, http.StatusOK, control.SecretRevealFor(secret))
	}
}

func upsertProjectSecretHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		kind := r.PathValue("kind")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectSecretInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		secret, err := store.UpsertProjectSecret(r.Context(), ref, kind, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Secret updated", map[string]string{"kind": secret.Kind})
		control.Audit(r.Context(), store, "project.secret_upsert", "project:"+ref, map[string]string{"kind": secret.Kind})
		writeJSON(w, http.StatusOK, secret)
	}
}

func deleteProjectSecretHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		kind := r.PathValue("kind")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		if err := store.DeleteProjectSecret(r.Context(), ref, kind); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Secret deleted", map[string]string{"kind": strings.ToLower(strings.TrimSpace(kind))})
		control.Audit(r.Context(), store, "project.secret_delete", "project:"+ref, map[string]string{"kind": strings.ToLower(strings.TrimSpace(kind))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func auditProjectSecretCopyHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		kind := r.PathValue("kind")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		if _, err := store.RevealProjectSecret(r.Context(), ref, kind); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Secret copied", map[string]string{"kind": kind})
		control.Audit(r.Context(), store, "project.secret_copy", "project:"+ref, map[string]string{"kind": kind})
		w.WriteHeader(http.StatusNoContent)
	}
}

func rotateProjectSecretHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.RotateProjectSecretRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		secret, err := store.RotateProjectSecret(r.Context(), ref, payload.Kind)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if provisioner != nil {
			project, err := store.GetProject(r.Context(), ref)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			secrets, err := store.ListProjectSecrets(r.Context(), ref)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			spec := control.ProjectSpecWithSecrets(project.Spec, secrets)
			if err := provisioner.SyncSecrets(r.Context(), ref, spec); err != nil {
				_, _ = store.UpdateProjectStatus(r.Context(), ref, control.ProjectDegraded, err.Error())
				control.LogProject(r.Context(), store, ref, "error", "Secret sync failed", map[string]string{"kind": secret.Kind, "error": err.Error()})
				control.Audit(r.Context(), store, "project.secret_sync_failed", "project:"+ref, map[string]string{"kind": secret.Kind, "error": err.Error()})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
		}
		control.LogProject(r.Context(), store, ref, "warning", "Secret rotated", map[string]string{"kind": secret.Kind})
		control.Audit(r.Context(), store, "project.secret_rotate", "project:"+ref, map[string]string{"kind": secret.Kind})
		writeJSON(w, http.StatusOK, secret)
	}
}

func listBackupsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		backups, err := store.ListBackups(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, backups)
	}
}

func triggerBackupHandler(store control.Store) http.HandlerFunc {
	backupService := control.NewBackupService("")
	return func(w http.ResponseWriter, r *http.Request) {
		project, ok := requireProjectRole(w, r, store, r.PathValue("ref"), roleDeveloper)
		if !ok {
			return
		}
		policy, err := store.GetBackupPolicy(r.Context(), project.Ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		backup, err := backupService.TriggerBackupForKind(r.Context(), store, project, policy.Kind)
		if err != nil {
			control.LogProject(r.Context(), store, project.Ref, "error", "Backup failed", map[string]string{"error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, project.Ref, "info", strings.Title(backup.Kind)+" backup completed", map[string]string{"backup_id": backup.ID, "kind": backup.Kind})
		control.Audit(r.Context(), store, "project.backup", "project:"+project.Ref, map[string]string{"backup_id": backup.ID, "kind": backup.Kind})
		writeJSON(w, http.StatusCreated, backup)
	}
}

func restoreBackupHandler(store control.Store) http.HandlerFunc {
	backupService := control.NewBackupService("")
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleDeveloper); !ok {
			return
		}
		var payload restoreBackupRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		backup, restore, err := backupService.RestoreBackup(r.Context(), store, ref, payload.BackupID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Restore "+restore.State, map[string]string{"backup_id": backup.ID, "restore_path": restore.Path})
		control.Audit(r.Context(), store, "project.restore", "project:"+ref, map[string]string{"backup_id": backup.ID, "state": restore.State})
		writeJSON(w, http.StatusAccepted, restoreBackupResponse{Backup: backup, RestorePath: restore.Path, RestoreState: restore.State})
	}
}

func restorePITRBackupHandler(store control.Store) http.HandlerFunc {
	backupService := control.NewBackupService("")
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleDeveloper); !ok {
			return
		}
		var payload restorePITRBackupRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		targetUnix, err := strconv.ParseInt(strings.TrimSpace(payload.RecoveryTimeTargetUnix), 10, 64)
		if err != nil || targetUnix <= 0 {
			writeError(w, http.StatusBadRequest, "recovery_time_target_unix must be a Unix timestamp string")
			return
		}
		result, recoverability, err := backupService.RestoreToTime(r.Context(), store, ref, time.Unix(targetUnix, 0).UTC())
		if err != nil {
			if recoverability.ProjectRef != "" {
				writeJSON(w, http.StatusConflict, restorePITRBackupUnavailableResponse{Error: err.Error(), Recoverability: recoverability})
				return
			}
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "PITR restore "+result.State, map[string]string{"restore_path": result.Path, "recovery_time_target_unix": fmt.Sprintf("%d", result.RecoveryTimeTargetUnix)})
		control.Audit(r.Context(), store, "project.restore_pitr", "project:"+ref, map[string]string{"state": result.State, "recovery_time_target_unix": fmt.Sprintf("%d", result.RecoveryTimeTargetUnix)})
		writeJSON(w, http.StatusCreated, restorePITRBackupResponse{
			ProjectRef:             ref,
			RecoveryTimeTargetUnix: result.RecoveryTimeTargetUnix,
			RecoveryTimeTarget:     result.RecoveryTimeTarget,
			RestorePath:            result.Path,
			RestoreState:           result.State,
		})
	}
}

func getProjectRecoverabilityHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		status, err := control.ProjectRecoverability(r.Context(), store, ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func getBackupPolicyHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		policy, err := store.GetBackupPolicy(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, policy)
	}
}

func updateBackupPolicyHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.BackupPolicyInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		policy, err := store.UpdateBackupPolicy(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "Backup policy updated", map[string]string{"schedule": policy.Schedule, "storage_target_id": policy.StorageTargetID})
		control.Audit(r.Context(), store, "project.backup_policy_update", "project:"+ref, map[string]string{"enabled": fmt.Sprintf("%t", policy.Enabled), "schedule": policy.Schedule, "storage_target_id": policy.StorageTargetID})
		writeJSON(w, http.StatusOK, policy)
	}
}

func getPITRPolicyHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		policy, err := store.GetPITRPolicy(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, policy)
	}
}

func updatePITRPolicyHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "pitr") {
			return
		}
		var payload control.PITRPolicyInput
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		policy, err := store.UpdatePITRPolicy(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "PITR policy updated", map[string]string{"enabled": fmt.Sprintf("%t", policy.Enabled), "retention_days": fmt.Sprintf("%d", policy.RetentionDays)})
		control.Audit(r.Context(), store, "project.pitr_policy_update", "project:"+ref, map[string]string{"enabled": fmt.Sprintf("%t", policy.Enabled), "retention_days": fmt.Sprintf("%d", policy.RetentionDays)})
		writeJSON(w, http.StatusOK, policy)
	}
}

func listWALArchivesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		archives, err := store.ListWALArchives(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, archives)
	}
}

func archiveWALHandler(store control.Store) http.HandlerFunc {
	backupService := control.NewBackupService("")
	return func(w http.ResponseWriter, r *http.Request) {
		project, ok := requireProjectRole(w, r, store, r.PathValue("ref"), roleDeveloper)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "pitr") {
			return
		}
		archive, err := backupService.ArchiveWALSegment(r.Context(), store, project)
		if err != nil {
			control.LogProject(r.Context(), store, project.Ref, "error", "WAL archive failed", map[string]string{"error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, project.Ref, "info", "WAL segment archived", map[string]string{"wal_archive_id": archive.ID, "segment": archive.Segment})
		control.Audit(r.Context(), store, "project.wal_archive", "project:"+project.Ref, map[string]string{"wal_archive_id": archive.ID, "segment": archive.Segment})
		writeJSON(w, http.StatusCreated, archive)
	}
}

func listProjectLogsHandler(store control.Store) http.HandlerFunc {
	stream := streamProjectLogsHandler(store)
	return func(w http.ResponseWriter, r *http.Request) {
		if wantsProjectLogStream(r) {
			stream(w, r)
			return
		}
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		logs, err := store.ListProjectLogs(r.Context(), ref, 100)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, logs)
	}
}

func wantsProjectLogStream(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("stream")), "true") {
		return true
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
}

func streamProjectLogsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming is not supported")
			return
		}
		initial, err := store.ListProjectLogs(r.Context(), ref, 100)
		if err != nil {
			writeStoreError(w, err)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		emitted := map[string]struct{}{}
		writeProjectLogEvents(w, flusher, initial, emitted)
		if strings.EqualFold(r.URL.Query().Get("follow"), "false") {
			return
		}

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				logs, err := store.ListProjectLogs(r.Context(), ref, 100)
				if err != nil {
					_, _ = fmt.Fprintf(w, "event: error\ndata: %q\n\n", err.Error())
					flusher.Flush()
					return
				}
				writeProjectLogEvents(w, flusher, logs, emitted)
			case <-heartbeat.C:
				_, _ = fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}

func writeProjectLogEvents(w http.ResponseWriter, flusher http.Flusher, logs []control.ProjectLog, emitted map[string]struct{}) {
	for i := len(logs) - 1; i >= 0; i-- {
		logEntry := logs[i]
		if _, ok := emitted[logEntry.ID]; ok {
			continue
		}
		payload, err := json.Marshal(logEntry)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(w, "event: log\ndata: %s\n\n", payload)
		emitted[logEntry.ID] = struct{}{}
	}
	flusher.Flush()
}

func listProjectActivityHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		events, err := store.ListProjectAuditEvents(r.Context(), ref, 100)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, events)
	}
}

func lifecycleHandler(store control.Store, provisioner control.Provisioner, nextStatus control.ProjectPhase) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleDeveloper); !ok {
			return
		}
		if provisioner == nil {
			writeError(w, http.StatusServiceUnavailable, "provisioner is not configured")
			return
		}

		var err error
		switch nextStatus {
		case control.ProjectPaused:
			err = provisioner.Pause(r.Context(), ref)
		case control.ProjectHealthy:
			err = provisioner.Resume(r.Context(), ref)
		default:
			err = errors.New("unsupported lifecycle transition")
		}
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		project, err := store.UpdateProjectStatus(r.Context(), ref, nextStatus, string(nextStatus))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "Lifecycle transition completed", map[string]string{"status": string(nextStatus)})
		control.Audit(r.Context(), store, "project."+string(nextStatus), "project:"+ref, nil)
		writeJSON(w, http.StatusOK, sanitizeProjectForResponse(project))
	}
}

func restartHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleDeveloper); !ok {
			return
		}
		if provisioner == nil {
			writeError(w, http.StatusServiceUnavailable, "provisioner is not configured")
			return
		}
		if err := provisioner.Pause(r.Context(), ref); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := provisioner.Resume(r.Context(), ref); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		project, err := store.UpdateProjectStatus(r.Context(), ref, control.ProjectHealthy, "restarted")
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "Project restarted", nil)
		control.Audit(r.Context(), store, "project.restart", "project:"+ref, nil)
		writeJSON(w, http.StatusOK, sanitizeProjectForResponse(project))
	}
}

func upgradeProjectHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	backupService := control.NewBackupService("")
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		if provisioner == nil {
			writeError(w, http.StatusServiceUnavailable, "provisioner is not configured")
			return
		}
		var payload upgradeProjectRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		targetVersion := strings.TrimSpace(payload.Version)
		if err := validateUpgradeTarget(project.Spec.StackVersion, targetVersion); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		backup, err := preUpgradeBackup(r.Context(), store, backupService, project, payload.BackupID)
		if err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Pre-upgrade backup failed", map[string]string{"version": targetVersion, "error": err.Error()})
			control.Audit(r.Context(), store, "project.upgrade_backup_failed", "project:"+ref, map[string]string{"version": targetVersion, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if requireDurableUpgradeBackup() {
			if err := validateDurablePreUpgradeBackup(r.Context(), store, backup); err != nil {
				control.LogProject(r.Context(), store, ref, "error", "Pre-upgrade backup durability check failed", map[string]string{"version": targetVersion, "backup_id": backup.ID, "error": err.Error()})
				control.Audit(r.Context(), store, "project.upgrade_backup_failed", "project:"+ref, map[string]string{"version": targetVersion, "backup_id": backup.ID, "error": err.Error(), "durable_required": "true"})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
		}
		metadata := map[string]string{
			"previous_version": project.Spec.StackVersion,
			"target_version":   targetVersion,
			"backup_id":        backup.ID,
		}
		control.LogProject(r.Context(), store, ref, "info", "Pre-upgrade backup completed", metadata)
		control.Audit(r.Context(), store, "project.upgrade_backup", "project:"+ref, metadata)

		err = injectedUpgradeFailure(r, targetVersion)
		if err == nil {
			err = provisioner.Upgrade(r.Context(), ref, targetVersion)
		}
		if err != nil {
			rollbackErr := provisioner.Upgrade(r.Context(), ref, project.Spec.StackVersion)
			failedMetadata := make(map[string]string, len(metadata)+3)
			for key, value := range metadata {
				failedMetadata[key] = value
			}
			failedMetadata["error"] = err.Error()
			rollbackAttempted := true
			rollbackError := ""
			restoreAttempted := false
			restoreState := ""
			restoreError := ""
			if rollbackErr != nil {
				rollbackError = rollbackErr.Error()
				failedMetadata["rollback_error"] = rollbackErr.Error()
			} else {
				failedMetadata["rollback"] = "attempted"
				if autoRestoreFailedUpgradeBackup() {
					restoreAttempted = true
					_, restore, restoreErr := backupService.RestoreBackup(r.Context(), store, ref, backup.ID)
					if restoreErr != nil {
						restoreError = restoreErr.Error()
						failedMetadata["restore_error"] = restoreError
					} else {
						restoreState = restore.State
						failedMetadata["restore_state"] = restore.State
						if restore.State != "completed" {
							restoreError = fmt.Sprintf("logical restore returned state %q; configure SUPADUPA_LOGICAL_RESTORE_COMMAND or SUPADUPA_COMPOSE_APPLY=true for real failed-upgrade auto-restore", restore.State)
							failedMetadata["restore_error"] = restoreError
						} else {
							failedMetadata["restore"] = "attempted"
						}
					}
				}
			}
			control.LogProject(r.Context(), store, ref, "error", "Stack upgrade failed", failedMetadata)
			control.Audit(r.Context(), store, "project.upgrade_failed", "project:"+ref, failedMetadata)
			writeJSON(w, http.StatusConflict, upgradeProjectFailureResponse{
				Error:             err.Error(),
				Backup:            backup,
				PreviousVersion:   metadata["previous_version"],
				TargetVersion:     targetVersion,
				RollbackAvailable: true,
				RollbackAttempted: rollbackAttempted,
				RollbackError:     rollbackError,
				RestoreAttempted:  restoreAttempted,
				RestoreState:      restoreState,
				RestoreError:      restoreError,
			})
			return
		}
		project, err = store.UpdateProjectStackVersion(r.Context(), ref, targetVersion)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "Stack upgraded", metadata)
		control.Audit(r.Context(), store, "project.upgrade", "project:"+ref, metadata)
		writeJSON(w, http.StatusOK, upgradeProjectResponse{
			Project:           sanitizeProjectForResponse(project),
			Backup:            backup,
			PreviousVersion:   metadata["previous_version"],
			TargetVersion:     targetVersion,
			RollbackAvailable: true,
		})
	}
}

func injectedUpgradeFailure(r *http.Request, targetVersion string) error {
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Supadupa-Compat-Inject-Upgrade-Failure")), "true") {
		return nil
	}
	for _, target := range strings.FieldsFunc(os.Getenv("SUPADUPA_COMPAT_UPGRADE_FAILURE_TARGETS"), func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	}) {
		if strings.TrimSpace(target) == targetVersion {
			return fmt.Errorf("compat upgrade failure injection for %s", targetVersion)
		}
	}
	return nil
}

func preUpgradeBackup(ctx context.Context, store control.Store, backupService *control.BackupService, project control.Project, backupID string) (control.Backup, error) {
	backupID = strings.TrimSpace(backupID)
	if backupID != "" {
		backup, err := store.GetBackup(ctx, project.Ref, backupID)
		if err != nil {
			return control.Backup{}, err
		}
		if backup.Status != "completed" {
			return control.Backup{}, fmt.Errorf("backup %s is not completed", backup.ID)
		}
		if err := validatePreUpgradeBackupArtifact(ctx, store, backupService, project.Ref, &backup); err != nil {
			return control.Backup{}, err
		}
		return backup, nil
	}
	return backupService.TriggerLogicalBackup(ctx, store, project)
}

func validatePreUpgradeBackupArtifact(ctx context.Context, store control.Store, backupService *control.BackupService, ref string, backup *control.Backup) error {
	if backup.Kind != "logical" {
		return fmt.Errorf("backup %s kind %q cannot be used for stack upgrade", backup.ID, backup.Kind)
	}
	if backup.VerifiedAt == nil {
		return fmt.Errorf("backup %s has not been verified", backup.ID)
	}
	return backupService.EnsureLogicalBackupArtifact(ctx, store, ref, backup)
}

func requireDurableUpgradeBackup() bool {
	return envBoolValue(os.Getenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP"))
}

func autoRestoreFailedUpgradeBackup() bool {
	return envBoolValue(os.Getenv("SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE"))
}

func validateDurablePreUpgradeBackup(ctx context.Context, store control.Store, backup control.Backup) error {
	if strings.TrimSpace(backup.RemoteLocation) == "" || strings.TrimSpace(backup.StorageTargetID) == "" {
		return fmt.Errorf("pre-upgrade backup %s is local-only; configure a tested durable off-host backup target or disable SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP for local development", backup.ID)
	}
	targets, err := store.ListBackupStorageTargets(ctx)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.ID != backup.StorageTargetID {
			continue
		}
		if !target.RecoveryReady || !target.DurableOffHost {
			status := strings.TrimSpace(target.ReadinessStatus)
			if status == "" {
				status = "not-ready"
			}
			return fmt.Errorf("pre-upgrade backup %s target %s is not durable off-host ready: %s", backup.ID, backup.StorageTargetID, status)
		}
		return nil
	}
	return fmt.Errorf("pre-upgrade backup %s target %s is unavailable", backup.ID, backup.StorageTargetID)
}

func validateUpgradeTarget(currentVersion string, targetVersion string) error {
	if targetVersion == "" {
		return fmt.Errorf("version is required")
	}
	if strings.TrimSpace(currentVersion) == targetVersion {
		return fmt.Errorf("target version %s is already installed", targetVersion)
	}
	for _, version := range supportedUpgradeVersions() {
		if targetVersion == version {
			return nil
		}
	}
	return fmt.Errorf("unsupported stack version %q; supported stable versions: %s", targetVersion, strings.Join(supportedUpgradeVersions(), ", "))
}

func supportedUpgradeVersions() []string {
	return control.SupportedStackReleaseVersionsFromEnv(os.Getenv)
}

func scaleProjectHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		if provisioner == nil {
			writeError(w, http.StatusServiceUnavailable, "provisioner is not configured")
			return
		}
		var payload scaleProjectRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		project, err := store.UpdateProjectResourceTier(r.Context(), ref, payload.ResourceTier)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := provisioner.Scale(r.Context(), ref, payload.ResourceTier); err != nil {
			project, _ = store.UpdateProjectStatus(r.Context(), ref, control.ProjectError, err.Error())
			control.LogProject(r.Context(), store, ref, "error", "Resource tier scale failed", map[string]string{"resource_tier": string(payload.ResourceTier), "error": err.Error()})
			control.Audit(r.Context(), store, "project.scale_failed", "project:"+ref, map[string]string{"resource_tier": string(payload.ResourceTier), "error": err.Error()})
			writeJSON(w, http.StatusAccepted, sanitizeProjectForResponse(project))
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "Resource tier scaled", map[string]string{"resource_tier": string(payload.ResourceTier)})
		control.Audit(r.Context(), store, "project.scale", "project:"+ref, map[string]string{"resource_tier": string(payload.ResourceTier)})
		writeJSON(w, http.StatusOK, sanitizeProjectForResponse(project))
	}
}

func destroyProjectHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		if provisioner == nil {
			writeError(w, http.StatusServiceUnavailable, "provisioner is not configured")
			return
		}
		retainVolumes := parseBoolQuery(r, "retain_volumes")
		var err error
		if destroyer, ok := provisioner.(control.OptionedDestroyer); ok {
			err = destroyer.DestroyWithOptions(r.Context(), ref, control.DestroyOptions{RetainVolumes: retainVolumes})
		} else {
			err = provisioner.Destroy(r.Context(), ref)
		}
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := control.NewRoutingService("").RemoveProject(ref); err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Project route cleanup failed", map[string]string{"error": err.Error()})
			control.Audit(r.Context(), store, "project.route_cleanup_failed", "project:"+ref, map[string]string{"error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := control.NewCertificateService().RemoveProject(r.Context(), ref); err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Project certificate cleanup failed", map[string]string{"error": err.Error()})
			control.Audit(r.Context(), store, "project.certificate_cleanup_failed", "project:"+ref, map[string]string{"error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProject(r.Context(), ref); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "project.destroy", "project:"+ref, map[string]string{"retain_volumes": fmt.Sprintf("%t", retainVolumes)})
		w.WriteHeader(http.StatusNoContent)
	}
}

func parseBoolQuery(r *http.Request, key string) bool {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(key)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func withCORS(next http.Handler, configuredOrigins []string) http.Handler {
	allowedOrigins := allowedCORSOrigins(configuredOrigins)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowedCORSOrigins(configured []string) map[string]bool {
	origins := configured
	if len(origins) == 0 {
		origins = strings.Split(os.Getenv("SUPADUPA_CORS_ORIGINS"), ",")
	}
	out := make(map[string]bool)
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			out[origin] = true
		}
	}
	if len(out) == 0 {
		addDerivedCORSOrigin(out, os.Getenv("SUPADUPA_ADMIN_URL"))
		addDerivedCORSOrigin(out, os.Getenv("SUPADUPA_ADMIN_HOST"))
		if len(out) == 0 {
			for _, origin := range []string{
				"http://localhost:3000",
				"http://127.0.0.1:3000",
				"http://localhost:3001",
				"http://127.0.0.1:3001",
				"http://localhost:5173",
				"http://127.0.0.1:5173",
				"http://localhost:5174",
				"http://127.0.0.1:5174",
			} {
				out[origin] = true
			}
		}
	}
	return out
}

func addDerivedCORSOrigin(out map[string]bool, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		out[strings.TrimRight(value, "/")] = true
		return
	}
	out["https://"+strings.Trim(value, "/")] = true
}

func withAuth(required bool, auth *control.AuthService, next http.Handler) http.Handler {
	if !required {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		token := tokenFromRequest(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := auth.Verify(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tokenClaimsKey, claims)))
	})
}

func isPublicPath(path string) bool {
	return path == "/healthz" ||
		path == "/v1/health" ||
		path == "/v1/auth/state" ||
		path == "/v1/auth/bootstrap" ||
		path == "/v1/auth/login" ||
		path == "/v1/auth/logout" ||
		path == "/v1/auth/studio/verify" ||
		path == "/v1/auth/sso/saml/start" ||
		path == "/v1/auth/sso/saml/callback" ||
		strings.HasPrefix(path, "/v1/scim/v2/")
}

func tokenFromRequest(r *http.Request) string {
	header := r.Header.Get("Authorization")
	scheme, token, ok := strings.Cut(header, " ")
	if ok && strings.EqualFold(scheme, "Bearer") && token != "" {
		return token
	}
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func bearerTokenFromRequest(r *http.Request) string {
	header := r.Header.Get("Authorization")
	scheme, token, ok := strings.Cut(header, " ")
	if ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(token)
	}
	return ""
}

func setAuthCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	cookie := authCookie(r, token, int(ttl.Seconds()))
	http.SetCookie(w, &cookie)
}

func clearAuthCookie(w http.ResponseWriter, r *http.Request) {
	cookie := authCookie(r, "", -1)
	cookie.Expires = time.Unix(0, 0)
	http.SetCookie(w, &cookie)
}

func authCookie(r *http.Request, value string, maxAge int) http.Cookie {
	cookie := http.Cookie{
		Name:     authCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		MaxAge:   maxAge,
	}
	if domain := authCookieDomain(r); domain != "" {
		cookie.Domain = domain
	}
	return cookie
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func authCookieDomain(r *http.Request) string {
	if configured := strings.TrimSpace(os.Getenv("SUPADUPA_COOKIE_DOMAIN")); configured != "" {
		if strings.EqualFold(configured, "host-only") || strings.EqualFold(configured, "none") {
			return ""
		}
		return strings.TrimPrefix(configured, ".")
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, control.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, control.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func sanitizeProjectForResponse(project control.Project) control.Project {
	project.Spec.Environment = nil
	return project
}

func sanitizeProjectsForResponse(projects []control.Project) []control.Project {
	sanitized := make([]control.Project, len(projects))
	for index, project := range projects {
		sanitized[index] = sanitizeProjectForResponse(project)
	}
	return sanitized
}

type accessRole int

const (
	roleViewer accessRole = iota + 1
	roleDeveloper
	roleAdmin
	roleOwner
)

func roleName(role accessRole) string {
	switch role {
	case roleViewer:
		return "viewer"
	case roleDeveloper:
		return "developer"
	case roleAdmin:
		return "admin"
	case roleOwner:
		return "owner"
	default:
		return "unknown"
	}
}

func roleRank(role string) accessRole {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner":
		return roleOwner
	case "admin":
		return roleAdmin
	case "developer":
		return roleDeveloper
	case "viewer":
		return roleViewer
	default:
		return 0
	}
}

func countEnabledFlags(flags map[string]bool) int {
	count := 0
	for _, enabled := range flags {
		if enabled {
			count++
		}
	}
	return count
}

func requireOrgFeature(w http.ResponseWriter, r *http.Request, store control.Store, orgID string, flag string) bool {
	flags, err := store.GetOrgFeatureFlags(r.Context(), orgID)
	if err != nil {
		writeStoreError(w, err)
		return false
	}
	if flags.Effective[flag] {
		return true
	}
	writeError(w, http.StatusForbidden, "feature flag "+flag+" is disabled for org")
	return false
}

func requireProjectFeature(w http.ResponseWriter, r *http.Request, store control.Store, project control.Project, flag string) bool {
	return requireOrgFeature(w, r, store, project.OrgID, flag)
}

func requirePlatformAdmin(w http.ResponseWriter, r *http.Request) bool {
	claims, ok := claimsFromRequest(r)
	if !ok {
		return true
	}
	if strings.EqualFold(claims.Role, "admin") {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden: platform admin role required")
	return false
}

func requireSCIMOrPlatformAdmin(w http.ResponseWriter, r *http.Request, store control.Store, auth *control.AuthService) bool {
	if claims, ok := claimsFromRequest(r); ok && strings.EqualFold(claims.Role, "admin") {
		return true
	}
	token := tokenFromRequest(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return false
	}
	if token != "" && auth != nil {
		if claims, err := auth.Verify(token); err == nil && strings.EqualFold(claims.Role, "admin") {
			return true
		}
	}
	bearer := bearerTokenFromRequest(r)
	if bearer == "" {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return false
	}
	config, err := store.GetPlatformSSOConfig(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return false
	}
	if control.VerifyPlatformSCIMToken(config, bearer) {
		return true
	}
	if config.SCIMEnabled && config.SCIMTokenConfigured {
		writeError(w, http.StatusUnauthorized, "invalid SCIM bearer token")
		return false
	}
	writeError(w, http.StatusForbidden, "SCIM provisioning token is not configured")
	return false
}

func requireOrgRole(w http.ResponseWriter, r *http.Request, store control.Store, orgID string, minimum accessRole) bool {
	claims, ok := claimsFromRequest(r)
	if !ok {
		return true
	}
	role, err := orgRoleForEmail(r.Context(), store, orgID, claims.Email)
	if err != nil {
		writeStoreError(w, err)
		return false
	}
	if roleRank(role) >= minimum {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden: requires "+roleName(minimum)+" access to org")
	return false
}

func requireProjectRole(w http.ResponseWriter, r *http.Request, store control.Store, ref string, minimum accessRole) (control.Project, bool) {
	project, err := store.GetProject(r.Context(), ref)
	if err != nil {
		writeStoreError(w, err)
		return control.Project{}, false
	}
	claims, ok := claimsFromRequest(r)
	if !ok {
		return project, true
	}
	orgRole, err := orgRoleForEmail(r.Context(), store, project.OrgID, claims.Email)
	if err != nil {
		writeStoreError(w, err)
		return control.Project{}, false
	}
	if roleRank(orgRole) >= minimum {
		return project, true
	}
	projectRole, err := store.ResolveProjectRole(r.Context(), project.Ref, claims.Email)
	if err == nil && roleRank(projectRole) >= minimum {
		return project, true
	}
	if err != nil && !errors.Is(err, control.ErrNotFound) {
		writeStoreError(w, err)
		return control.Project{}, false
	}
	writeError(w, http.StatusForbidden, "forbidden: requires "+roleName(minimum)+" access to project")
	return control.Project{}, false
}

func projectsVisibleToRequest(r *http.Request, store control.Store) ([]control.Project, error) {
	projects, err := store.ListProjects(r.Context())
	if err != nil {
		return nil, err
	}
	claims, ok := claimsFromRequest(r)
	if !ok || strings.EqualFold(claims.Role, "admin") {
		return projects, nil
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	orgs, err := store.ListOrgs(r.Context())
	if err != nil {
		return nil, err
	}
	visibleOrgs := map[string]struct{}{}
	for _, org := range orgs {
		role, err := orgRoleForEmail(r.Context(), store, org.ID, email)
		if err != nil {
			return nil, err
		}
		if roleRank(role) >= roleViewer {
			visibleOrgs[org.ID] = struct{}{}
		}
	}
	visible := make([]control.Project, 0, len(projects))
	for _, project := range projects {
		if _, ok := visibleOrgs[project.OrgID]; ok {
			visible = append(visible, project)
			continue
		}
		role, err := store.ResolveProjectRole(r.Context(), project.Ref, email)
		if err != nil {
			if errors.Is(err, control.ErrNotFound) {
				continue
			}
			return nil, err
		}
		if roleRank(role) >= roleViewer {
			visible = append(visible, project)
		}
	}
	return visible, nil
}

func orgRoleForEmail(ctx context.Context, store control.Store, orgID string, email string) (string, error) {
	members, err := store.ListOrgMembers(ctx, orgID)
	if err != nil {
		return "", err
	}
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	for _, member := range members {
		if member.Email == normalizedEmail {
			return member.Role, nil
		}
	}
	return "", nil
}

func cfgProvisionerName(provisioner control.Provisioner) string {
	if provisioner == nil {
		return "unconfigured"
	}
	return provisioner.Name()
}

func serviceAuditMetadata(services map[string]control.ServiceSpec) map[string]string {
	metadata := map[string]string{}
	states := control.ProjectServiceStates(services)
	for _, service := range control.AllowedProjectServices() {
		metadata[service] = fmt.Sprintf("%t", states[service])
	}
	return metadata
}

func claimsFromRequest(r *http.Request) (control.TokenClaims, bool) {
	claims, ok := r.Context().Value(tokenClaimsKey).(control.TokenClaims)
	return claims, ok
}

func userIDFromRequest(r *http.Request) (string, bool) {
	claims, ok := claimsFromRequest(r)
	if !ok || claims.Subject == "" {
		return "", false
	}
	return claims.Subject, true
}

func platformSSOUser(ctx context.Context, store control.Store, config control.PlatformSSOConfig, assertion control.PlatformSSOAssertion) (control.User, error) {
	email := strings.ToLower(strings.TrimSpace(assertion.Email))
	users, err := store.ListUsers(ctx)
	if err != nil {
		return control.User{}, err
	}
	for _, user := range users {
		if strings.EqualFold(user.Email, email) {
			return user, nil
		}
	}
	if !config.AutoProvision {
		return control.User{}, fmt.Errorf("%w: platform user %s", control.ErrNotFound, email)
	}
	role := strings.ToLower(strings.TrimSpace(assertion.Role))
	if role == "" {
		role = config.DefaultRole
	}
	if role != "admin" && role != "developer" && role != "viewer" {
		return control.User{}, fmt.Errorf("saml assertion role must be admin, developer, or viewer")
	}
	password, err := randomSSOPassword()
	if err != nil {
		return control.User{}, err
	}
	return store.CreateUser(ctx, control.CreateUserRequest{Email: email, Password: password, Role: role})
}

func randomSSOPassword() (string, error) {
	var bytes [24]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return "sso-" + base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func renderPrometheusMetrics(metrics control.FleetMetrics, projects []control.ProjectMetrics) string {
	var builder strings.Builder
	projectMetricFamilies := map[string]bool{}
	writeMetric := func(name string, help string, value any) {
		builder.WriteString(fmt.Sprintf("# HELP %s %s\n", name, help))
		builder.WriteString(fmt.Sprintf("# TYPE %s gauge\n", name))
		builder.WriteString(fmt.Sprintf("%s %v\n", name, value))
	}
	writeProjectMetric := func(name string, help string, project control.ProjectMetrics, extraLabels map[string]string, value any) {
		if !projectMetricFamilies[name] {
			builder.WriteString(fmt.Sprintf("# HELP %s %s\n", name, help))
			builder.WriteString(fmt.Sprintf("# TYPE %s gauge\n", name))
			projectMetricFamilies[name] = true
		}
		labels := map[string]string{
			"project_ref":   project.ProjectRef,
			"org_id":        project.OrgID,
			"resource_tier": string(project.ResourceTier),
			"status":        string(project.Status),
		}
		for key, labelValue := range extraLabels {
			labels[key] = labelValue
		}
		builder.WriteString(name)
		builder.WriteString(prometheusLabels(labels))
		builder.WriteString(fmt.Sprintf(" %v\n", value))
	}
	writeMetric("supadupa_orgs_total", "Total organizations known to the control plane.", metrics.Orgs)
	writeMetric("supadupa_users_total", "Total platform users known to the control plane.", metrics.Users)
	writeMetric("supadupa_hosts_total", "Total hosts known to the control plane.", metrics.Hosts)
	writeMetric("supadupa_projects_total", "Total projects known to the control plane.", metrics.Projects)
	writeMetric("supadupa_read_replicas_total", "Total read replicas tracked.", metrics.ReadReplicas)
	builder.WriteString("# HELP supadupa_projects_by_status Projects by status.\n")
	builder.WriteString("# TYPE supadupa_projects_by_status gauge\n")
	for status, count := range metrics.ProjectsByStatus {
		builder.WriteString(fmt.Sprintf("supadupa_projects_by_status{status=%q} %d\n", status, count))
	}
	writeMetric("supadupa_host_capacity_cpu", "Total configured host CPU capacity.", metrics.HostCapacity.CPU)
	writeMetric("supadupa_host_used_cpu", "Total reserved host CPU.", metrics.HostUsed.CPU)
	writeMetric("supadupa_host_capacity_ram_mb", "Total configured host RAM capacity in MB.", metrics.HostCapacity.RAMMB)
	writeMetric("supadupa_host_used_ram_mb", "Total reserved host RAM in MB.", metrics.HostUsed.RAMMB)
	writeMetric("supadupa_host_capacity_disk_gb", "Total configured host disk capacity in GB.", metrics.HostCapacity.DiskGB)
	writeMetric("supadupa_host_used_disk_gb", "Total reserved host disk in GB.", metrics.HostUsed.DiskGB)
	writeMetric("supadupa_host_capacity_disk_iops", "Total configured host disk IOPS.", metrics.HostCapacity.DiskIOPS)
	writeMetric("supadupa_host_used_disk_iops", "Total reserved host disk IOPS.", metrics.HostUsed.DiskIOPS)
	writeMetric("supadupa_observed_projects", "Projects with a latest telemetry sample.", metrics.Observed.ProjectsSampled)
	writeMetric("supadupa_observed_stale_projects", "Projects whose latest telemetry sample is stale.", metrics.Observed.StaleProjects)
	writeMetric("supadupa_observed_cpu_percent", "Total observed CPU percent across latest project samples.", metrics.Observed.CPUPercent)
	writeMetric("supadupa_observed_memory_bytes", "Total observed memory bytes across latest project samples.", metrics.Observed.MemoryBytes)
	writeMetric("supadupa_observed_memory_limit_bytes", "Total observed memory limit bytes across latest project samples.", metrics.Observed.MemoryLimitBytes)
	writeMetric("supadupa_observed_disk_used_bytes", "Total observed disk usage bytes across latest project samples.", metrics.Observed.DiskUsedBytes)
	writeMetric("supadupa_observed_disk_limit_bytes", "Total observed disk limit bytes across latest project samples.", metrics.Observed.DiskLimitBytes)
	writeMetric("supadupa_routes_total", "Total ingress routes registered.", metrics.Routes)
	writeMetric("supadupa_custom_domains_total", "Total custom domains configured.", metrics.CustomDomains)
	writeMetric("supadupa_log_drains_total", "Total log drains configured.", metrics.LogDrains)
	writeMetric("supadupa_function_deployments_total", "Total Edge Function deployments tracked.", metrics.FunctionDeployments)
	writeMetric("supadupa_function_regions_total", "Total Edge Function regional invocation targets configured.", metrics.FunctionRegions)
	writeMetric("supadupa_function_storage_mounts_total", "Total Edge Function persistent storage mounts configured.", metrics.FunctionStorageMounts)
	writeMetric("supadupa_replication_pipelines_total", "Total replication and ETL pipelines configured.", metrics.ReplicationPipelines)
	writeMetric("supadupa_embedding_jobs_total", "Total automatic embedding jobs configured.", metrics.EmbeddingJobs)
	writeMetric("supadupa_database_extensions_enabled_total", "Total enabled database extensions across projects.", metrics.DatabaseExtensions)
	writeMetric("supadupa_auth_clients_total", "Total OAuth client registrations configured.", metrics.AuthClients)
	writeMetric("supadupa_auth_hooks_total", "Total Auth Hook declarations configured.", metrics.AuthHooks)
	writeMetric("supadupa_database_cron_jobs_total", "Total pg_cron job declarations configured.", metrics.DatabaseCronJobs)
	writeMetric("supadupa_database_queues_total", "Total pgmq queue declarations configured.", metrics.DatabaseQueues)
	writeMetric("supadupa_database_webhooks_total", "Total database webhook declarations configured.", metrics.DatabaseWebhooks)
	writeMetric("supadupa_database_schemas_total", "Total declarative database schema migrations recorded.", metrics.DatabaseSchemas)
	writeMetric("supadupa_database_roles_total", "Total database roles configured.", metrics.DatabaseRoles)
	writeMetric("supadupa_storage_buckets_total", "Total storage buckets configured.", metrics.StorageBuckets)
	writeMetric("supadupa_vector_buckets_total", "Total vector buckets configured.", metrics.VectorBuckets)
	writeMetric("supadupa_analytics_buckets_total", "Total Iceberg analytics buckets configured.", metrics.AnalyticsBuckets)
	writeMetric("supadupa_cdn_enabled_projects_total", "Total projects with CDN policy enabled.", metrics.CDNEnabledProjects)
	writeMetric("supadupa_cdn_invalidations_total", "Total CDN invalidations recorded.", metrics.CDNInvalidations)
	writeMetric("supadupa_network_connections_total", "Total private network connection declarations recorded.", metrics.NetworkConnections)
	writeMetric("supadupa_backups_total", "Total backups recorded.", metrics.Backups)
	writeMetric("supadupa_backup_storage_bytes", "Total backup storage bytes recorded.", metrics.BackupStorageBytes)
	writeMetric("supadupa_wal_archives_total", "Total WAL archives recorded.", metrics.WALArchives)
	writeMetric("supadupa_wal_archive_bytes", "Total WAL archive bytes recorded.", metrics.WALArchiveBytes)
	writeMetric("supadupa_project_log_events_total", "Total project log events recorded.", metrics.ProjectLogEvents)
	writeMetric("supadupa_audit_events_total", "Total audit events recorded.", metrics.AuditEvents)
	writeMetric("supadupa_audit_verified", "Whether the audit hash chain verifies, 1 for true and 0 for false.", boolMetric(metrics.AuditVerified))
	writeMetric("supadupa_metrics_sampled_at_unix", "Unix timestamp when fleet metrics were sampled.", metrics.SampledAt.Unix())
	for _, project := range projects {
		writeProjectMetric("supadupa_project_resource_cpu", "Reserved CPU for a project.", project, nil, project.Resources.CPU)
		writeProjectMetric("supadupa_project_resource_ram_mb", "Reserved RAM for a project in MB.", project, nil, project.Resources.RAMMB)
		writeProjectMetric("supadupa_project_resource_disk_gb", "Reserved disk for a project in GB.", project, nil, project.Resources.DiskGB)
		writeProjectMetric("supadupa_project_resource_disk_iops", "Reserved disk IOPS for a project.", project, nil, project.Resources.DiskIOPS)
		writeProjectMetric("supadupa_project_routes_total", "Ingress routes registered for a project.", project, nil, project.Routes)
		writeProjectMetric("supadupa_project_logs_total", "Project log events recorded for a project.", project, nil, project.ProjectLogEvents)
		writeProjectMetric("supadupa_project_activity_events_total", "Audit activity events associated with a project.", project, nil, project.ActivityEvents)
		writeProjectMetric("supadupa_project_backups_total", "Backups recorded for a project.", project, nil, project.Backups)
		writeProjectMetric("supadupa_project_backup_storage_bytes", "Backup storage bytes recorded for a project.", project, nil, project.BackupStorageBytes)
		writeProjectMetric("supadupa_project_wal_archives_total", "WAL archives recorded for a project.", project, nil, project.WALArchives)
		writeProjectMetric("supadupa_project_wal_archive_bytes", "WAL archive bytes recorded for a project.", project, nil, project.WALArchiveBytes)
		if project.Observed == nil {
			continue
		}
		observedLabels := map[string]string{"source": project.Observed.Source}
		writeProjectMetric("supadupa_project_observed_cpu_percent", "Observed project CPU percent from the latest telemetry sample.", project, observedLabels, project.Observed.CPUPercent)
		writeProjectMetric("supadupa_project_observed_memory_bytes", "Observed project memory bytes from the latest telemetry sample.", project, observedLabels, project.Observed.MemoryBytes)
		writeProjectMetric("supadupa_project_observed_memory_limit_bytes", "Observed project memory limit bytes from the latest telemetry sample.", project, observedLabels, project.Observed.MemoryLimitBytes)
		writeProjectMetric("supadupa_project_observed_disk_used_bytes", "Observed project disk usage bytes from the latest telemetry sample.", project, observedLabels, project.Observed.DiskUsedBytes)
		writeProjectMetric("supadupa_project_observed_disk_limit_bytes", "Observed project disk limit bytes from the latest telemetry sample.", project, observedLabels, project.Observed.DiskLimitBytes)
		writeProjectMetric("supadupa_project_observed_network_rx_bytes", "Observed project network receive bytes from the latest telemetry sample.", project, observedLabels, project.Observed.NetworkRxBytes)
		writeProjectMetric("supadupa_project_observed_network_tx_bytes", "Observed project network transmit bytes from the latest telemetry sample.", project, observedLabels, project.Observed.NetworkTxBytes)
		writeProjectMetric("supadupa_project_telemetry_sampled_at_unix", "Unix timestamp of the latest project telemetry sample.", project, observedLabels, project.Observed.SampledAt.Unix())
	}
	return builder.String()
}

func prometheusLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", key, prometheusLabelValue(labels[key])))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func prometheusLabelValue(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}

func boolMetric(value bool) int {
	if value {
		return 1
	}
	return 0
}

func scimList[T any](resources []T) scimListResponse {
	if resources == nil {
		resources = []T{}
	}
	return scimListResponse{
		Schemas:      []string{scimListResponseSchema},
		Total:        len(resources),
		StartIndex:   1,
		ItemsPerPage: len(resources),
		Resources:    resources,
	}
}

func scimUserFromControl(r *http.Request, user control.User) scimUserResource {
	return scimUserResource{
		Schemas:     []string{scimUserSchema, scimUserExtension},
		ID:          user.ID,
		ExternalID:  user.ID,
		UserName:    user.Email,
		DisplayName: user.Email,
		Active:      true,
		Emails:      []scimEmail{{Value: user.Email, Primary: true}},
		Meta: scimMeta{
			ResourceType: "User",
			Created:      user.CreatedAt,
			Location:     absoluteResourceLocation(r, "/v1/scim/v2/Users/"+user.ID),
		},
		Extension: scimUserExtensionResource{Role: user.Role},
	}
}

func scimGroupFromControl(r *http.Request, store control.Store, team control.Team) (scimGroupResource, error) {
	members, err := store.ListTeamMembers(r.Context(), team.OrgID, team.Slug)
	if err != nil {
		return scimGroupResource{}, err
	}
	scimMembers := make([]scimMember, 0, len(members))
	for _, member := range members {
		scimMembers = append(scimMembers, scimMember{Value: member.UserID, Display: member.Email})
	}
	return scimGroupResource{
		Schemas:     []string{scimGroupSchema, scimGroupExtension},
		ID:          team.ID,
		ExternalID:  team.OrgID,
		DisplayName: team.Name,
		Members:     scimMembers,
		Meta: scimMeta{
			ResourceType: "Group",
			Created:      team.CreatedAt,
			Location:     absoluteResourceLocation(r, "/v1/scim/v2/Groups/"+team.ID),
		},
		Extension: scimGroupExtensionResource{OrgID: team.OrgID, Slug: team.Slug},
	}, nil
}

func scimEmailFromUserRequest(payload scimUserRequest) string {
	for _, email := range payload.Emails {
		if email.Primary && strings.TrimSpace(email.Value) != "" {
			return strings.ToLower(strings.TrimSpace(email.Value))
		}
	}
	for _, email := range payload.Emails {
		if strings.TrimSpace(email.Value) != "" {
			return strings.ToLower(strings.TrimSpace(email.Value))
		}
	}
	return strings.ToLower(strings.TrimSpace(payload.UserName))
}

func scimGroupOrgID(r *http.Request, payload scimGroupRequest) string {
	if orgID := strings.TrimSpace(payload.Extension.OrgID); orgID != "" {
		return orgID
	}
	if orgID := strings.TrimSpace(payload.ExternalID); orgID != "" {
		return orgID
	}
	return strings.TrimSpace(r.URL.Query().Get("org_id"))
}

func scimMemberEmail(ctx context.Context, store control.Store, member scimMember) (string, error) {
	value := strings.TrimSpace(member.Value)
	if value != "" {
		user, err := store.GetUserByID(ctx, value)
		if err == nil {
			return user.Email, nil
		}
		if strings.Contains(value, "@") {
			return strings.ToLower(value), nil
		}
		return "", err
	}
	display := strings.TrimSpace(member.Display)
	if strings.Contains(display, "@") {
		return strings.ToLower(display), nil
	}
	return "", fmt.Errorf("SCIM group member value or email display is required")
}

func findSCIMTeam(ctx context.Context, store control.Store, id string) (control.Team, error) {
	id = strings.TrimSpace(id)
	orgs, err := store.ListOrgs(ctx)
	if err != nil {
		return control.Team{}, err
	}
	for _, org := range orgs {
		teams, err := store.ListOrgTeams(ctx, org.ID)
		if err != nil {
			return control.Team{}, err
		}
		for _, team := range teams {
			if team.ID == id || team.Slug == id {
				return team, nil
			}
		}
	}
	return control.Team{}, fmt.Errorf("%w: SCIM group %s", control.ErrNotFound, id)
}

func scimBoolValue(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true":
			return true
		case "false":
			return false
		default:
			return fallback
		}
	default:
		return fallback
	}
}

func scimStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func scimPatchObjectValues(value any) (string, string, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", "", false
	}
	var email string
	var role string
	changed := false
	for key, raw := range object {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "username":
			email = strings.ToLower(strings.TrimSpace(scimStringValue(raw)))
			changed = true
		case strings.ToLower(scimUserExtension):
			extension, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if rawRole, ok := extension["role"]; ok {
				role = strings.TrimSpace(scimStringValue(rawRole))
				changed = true
			}
		}
	}
	return email, role, changed
}

func absoluteResourceLocation(r *http.Request, path string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.Split(forwarded, ",")[0]
	}
	return scheme + "://" + r.Host + path
}

func reconcileProjectRoutes(r *http.Request, store control.Store, ref string) (string, error) {
	project, err := store.GetProject(r.Context(), ref)
	if err != nil {
		return "", err
	}
	domains, err := store.ListProjectDomains(r.Context(), ref)
	if err != nil {
		return "", err
	}
	networkConfig, err := store.GetProjectConfig(r.Context(), ref, "network")
	if err != nil {
		return "", err
	}
	cdnPolicy, err := store.GetProjectCDNPolicy(r.Context(), ref)
	if err != nil {
		return "", err
	}
	replicas, err := store.ListProjectReplicas(r.Context(), ref)
	if err != nil {
		return "", err
	}
	routes, err := store.UpsertProjectRoutes(r.Context(), ref, control.RoutesForProjectDomainsWithNetworkAndCDN(project, domains, networkConfig, cdnPolicy))
	if err != nil {
		return "", err
	}
	tcpRoutes := control.TCPRoutesForProjectWithNetworkAndReplicas(project, networkConfig, replicas)
	return control.NewRoutingService("").RenderProjectWithTCPRoutes(project, routes, tcpRoutes)
}
