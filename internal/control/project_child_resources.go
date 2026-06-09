package control

type projectChildResource struct {
	name              string
	inventory         projectChildResourceInventory
	cleanup           func(*MemoryStore, string)
	addOrgUsage       func(*MemoryStore, string, *OrgUsage)
	addFleetMetrics   func(*MemoryStore, *FleetMetrics)
	addProjectMetrics func(*MemoryStore, string, *ProjectMetrics)
}

type projectChildResourceInventory struct {
	memoryField       string
	snapshotField     string
	normalizedTable   string
	apiRoutePrefix    string
	cliCommand        string
	mcpTool           string
	terraformResource string
	omittedSurfaces   map[projectChildResourceSurface]string
}

func withProjectChildInventory(resource projectChildResource, inventory projectChildResourceInventory) projectChildResource {
	resource.inventory = inventory
	return resource
}

type projectChildResourceSurface string

const (
	projectChildSurfaceMemory    projectChildResourceSurface = "memory"
	projectChildSurfaceSnapshot  projectChildResourceSurface = "snapshot"
	projectChildSurfaceTable     projectChildResourceSurface = "normalized_table"
	projectChildSurfaceAPI       projectChildResourceSurface = "api"
	projectChildSurfaceCLI       projectChildResourceSurface = "cli"
	projectChildSurfaceMCP       projectChildResourceSurface = "mcp"
	projectChildSurfaceTerraform projectChildResourceSurface = "terraform"
)

func projectChildSliceResource[T any](
	name string,
	values func(*MemoryStore) map[string][]T,
	addOrgUsage func(*OrgUsage, int),
	addFleetMetrics func(*FleetMetrics, int),
	addProjectMetrics func(*ProjectMetrics, int),
) projectChildResource {
	resource := projectChildResource{
		name: name,
		cleanup: func(s *MemoryStore, ref string) {
			delete(values(s), ref)
		},
	}
	if addOrgUsage != nil {
		resource.addOrgUsage = func(s *MemoryStore, ref string, usage *OrgUsage) {
			addOrgUsage(usage, len(values(s)[ref]))
		}
	}
	if addFleetMetrics != nil {
		resource.addFleetMetrics = func(s *MemoryStore, metrics *FleetMetrics) {
			for _, entries := range values(s) {
				addFleetMetrics(metrics, len(entries))
			}
		}
	}
	if addProjectMetrics != nil {
		resource.addProjectMetrics = func(s *MemoryStore, ref string, metrics *ProjectMetrics) {
			addProjectMetrics(metrics, len(values(s)[ref]))
		}
	}
	return resource
}

func projectChildNestedMapResource[T any](
	name string,
	values func(*MemoryStore) map[string]map[string]T,
	addOrgUsage func(*OrgUsage, int),
	addProjectMetrics func(*ProjectMetrics, int),
) projectChildResource {
	resource := projectChildResource{
		name: name,
		cleanup: func(s *MemoryStore, ref string) {
			delete(values(s), ref)
		},
	}
	if addOrgUsage != nil {
		resource.addOrgUsage = func(s *MemoryStore, ref string, usage *OrgUsage) {
			addOrgUsage(usage, len(values(s)[ref]))
		}
	}
	if addProjectMetrics != nil {
		resource.addProjectMetrics = func(s *MemoryStore, ref string, metrics *ProjectMetrics) {
			addProjectMetrics(metrics, len(values(s)[ref]))
		}
	}
	return resource
}

func projectChildMapResource[T any](name string, values func(*MemoryStore) map[string]T) projectChildResource {
	return projectChildResource{
		name: name,
		cleanup: func(s *MemoryStore, ref string) {
			delete(values(s), ref)
		},
	}
}

var projectChildResourceRegistry = []projectChildResource{
	withProjectChildInventory(
		projectChildSliceResource("routes", func(s *MemoryStore) map[string][]ProjectRoute { return s.routes },
			nil,
			func(metrics *FleetMetrics, count int) { metrics.Routes += count },
			func(metrics *ProjectMetrics, count int) { metrics.Routes = count },
		),
		projectChildResourceInventory{
			memoryField:     "routes",
			snapshotField:   "Routes",
			normalizedTable: "project_routes",
			apiRoutePrefix:  "/v1/projects/{ref}/routes",
			cliCommand:      "routes",
			mcpTool:         "supadupa_list_project_routes",
			omittedSurfaces: map[projectChildResourceSurface]string{
				projectChildSurfaceTerraform: "routes are generated runtime routing state, not a declarative Terraform resource",
			},
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("domains", func(s *MemoryStore) map[string][]ProjectDomain { return s.domains },
			func(usage *OrgUsage, count int) { usage.CustomDomains += count },
			func(metrics *FleetMetrics, count int) { metrics.CustomDomains += count },
			func(metrics *ProjectMetrics, count int) { metrics.CustomDomains = count },
		),
		projectChildResourceInventory{
			memoryField:       "domains",
			snapshotField:     "Domains",
			normalizedTable:   "domains",
			apiRoutePrefix:    "/v1/projects/{ref}/domains",
			cliCommand:        "domains",
			mcpTool:           "supadupa_list_project_domains",
			terraformResource: "NewProjectDomainResource",
		},
	),
	withProjectChildInventory(
		projectChildMapResource("configs", func(s *MemoryStore) map[string]map[string]ProjectConfig { return s.configs }),
		projectChildResourceInventory{
			memoryField:       "configs",
			snapshotField:     "Configs",
			normalizedTable:   "project_configs",
			apiRoutePrefix:    "/v1/projects/{ref}/config/{area}",
			cliCommand:        "config",
			mcpTool:           "supadupa_get_project_config",
			terraformResource: "NewProjectConfigResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("auth_clients", func(s *MemoryStore) map[string][]ProjectAuthClient { return s.authClients },
			func(usage *OrgUsage, count int) { usage.AuthClients += count },
			func(metrics *FleetMetrics, count int) { metrics.AuthClients += count },
			func(metrics *ProjectMetrics, count int) { metrics.AuthClients = count },
		),
		projectChildResourceInventory{
			memoryField:       "authClients",
			snapshotField:     "AuthClients",
			normalizedTable:   "auth_clients",
			apiRoutePrefix:    "/v1/projects/{ref}/auth/clients",
			cliCommand:        "auth-clients",
			mcpTool:           "supadupa_list_project_auth_clients",
			terraformResource: "NewProjectAuthClientResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("auth_hooks", func(s *MemoryStore) map[string][]ProjectAuthHook { return s.authHooks },
			func(usage *OrgUsage, count int) { usage.AuthHooks += count },
			func(metrics *FleetMetrics, count int) { metrics.AuthHooks += count },
			func(metrics *ProjectMetrics, count int) { metrics.AuthHooks = count },
		),
		projectChildResourceInventory{
			memoryField:       "authHooks",
			snapshotField:     "AuthHooks",
			normalizedTable:   "auth_hooks",
			apiRoutePrefix:    "/v1/projects/{ref}/auth/hooks",
			cliCommand:        "auth-hooks",
			mcpTool:           "supadupa_list_project_auth_hooks",
			terraformResource: "NewProjectAuthHookResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("functions", func(s *MemoryStore) map[string][]ProjectFunction { return s.functions },
			func(usage *OrgUsage, count int) { usage.FunctionDeployments += count },
			func(metrics *FleetMetrics, count int) { metrics.FunctionDeployments += count },
			func(metrics *ProjectMetrics, count int) { metrics.FunctionDeployments = count },
		),
		projectChildResourceInventory{
			memoryField:       "functions",
			snapshotField:     "Functions",
			normalizedTable:   "edge_functions",
			apiRoutePrefix:    "/v1/projects/{ref}/functions",
			cliCommand:        "functions",
			mcpTool:           "supadupa_list_project_functions",
			terraformResource: "NewProjectFunctionResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("function_regions", func(s *MemoryStore) map[string][]ProjectFunctionRegion { return s.functionRegions },
			func(usage *OrgUsage, count int) { usage.FunctionRegions += count },
			func(metrics *FleetMetrics, count int) { metrics.FunctionRegions += count },
			func(metrics *ProjectMetrics, count int) { metrics.FunctionRegions = count },
		),
		projectChildResourceInventory{
			memoryField:       "functionRegions",
			snapshotField:     "FunctionRegions",
			normalizedTable:   "function_regions",
			apiRoutePrefix:    "/v1/projects/{ref}/functions/regions",
			cliCommand:        "functions",
			mcpTool:           "supadupa_list_project_function_regions",
			terraformResource: "NewProjectFunctionRegionResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("function_storage_mounts", func(s *MemoryStore) map[string][]ProjectFunctionStorageMount { return s.functionStorageMounts },
			func(usage *OrgUsage, count int) { usage.FunctionStorageMounts += count },
			func(metrics *FleetMetrics, count int) { metrics.FunctionStorageMounts += count },
			func(metrics *ProjectMetrics, count int) { metrics.FunctionStorageMounts = count },
		),
		projectChildResourceInventory{
			memoryField:       "functionStorageMounts",
			snapshotField:     "FunctionStorageMounts",
			normalizedTable:   "function_storage_mounts",
			apiRoutePrefix:    "/v1/projects/{ref}/functions/storage-mounts",
			cliCommand:        "functions",
			mcpTool:           "supadupa_list_project_function_storage_mounts",
			terraformResource: "NewProjectFunctionStorageMountResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("replication_pipelines", func(s *MemoryStore) map[string][]ProjectReplicationPipeline { return s.replicationPipelines },
			func(usage *OrgUsage, count int) { usage.ReplicationPipelines += count },
			func(metrics *FleetMetrics, count int) { metrics.ReplicationPipelines += count },
			func(metrics *ProjectMetrics, count int) { metrics.ReplicationPipelines = count },
		),
		projectChildResourceInventory{
			memoryField:       "replicationPipelines",
			snapshotField:     "ReplicationPipelines",
			normalizedTable:   "replication_pipelines",
			apiRoutePrefix:    "/v1/projects/{ref}/replication",
			cliCommand:        "replication",
			mcpTool:           "supadupa_list_project_replication_pipelines",
			terraformResource: "NewProjectReplicationPipelineResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("embedding_jobs", func(s *MemoryStore) map[string][]ProjectEmbeddingJob { return s.embeddingJobs },
			func(usage *OrgUsage, count int) { usage.EmbeddingJobs += count },
			func(metrics *FleetMetrics, count int) { metrics.EmbeddingJobs += count },
			func(metrics *ProjectMetrics, count int) { metrics.EmbeddingJobs = count },
		),
		projectChildResourceInventory{
			memoryField:       "embeddingJobs",
			snapshotField:     "EmbeddingJobs",
			normalizedTable:   "embedding_jobs",
			apiRoutePrefix:    "/v1/projects/{ref}/embeddings",
			cliCommand:        "embeddings",
			mcpTool:           "supadupa_list_project_embedding_jobs",
			terraformResource: "NewProjectEmbeddingJobResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("database_extensions", func(s *MemoryStore) map[string][]ProjectDatabaseExtension { return s.databaseExtensions },
			nil,
			nil,
			nil,
		),
		projectChildResourceInventory{
			memoryField:       "databaseExtensions",
			snapshotField:     "DatabaseExtensions",
			normalizedTable:   "database_extensions",
			apiRoutePrefix:    "/v1/projects/{ref}/database/extensions",
			cliCommand:        "database-extensions",
			mcpTool:           "supadupa_list_project_database_extensions",
			terraformResource: "NewProjectDatabaseExtensionResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("database_cron_jobs", func(s *MemoryStore) map[string][]ProjectDatabaseCronJob { return s.databaseCronJobs },
			func(usage *OrgUsage, count int) { usage.DatabaseCronJobs += count },
			func(metrics *FleetMetrics, count int) { metrics.DatabaseCronJobs += count },
			func(metrics *ProjectMetrics, count int) { metrics.DatabaseCronJobs = count },
		),
		projectChildResourceInventory{
			memoryField:       "databaseCronJobs",
			snapshotField:     "DatabaseCronJobs",
			normalizedTable:   "database_cron_jobs",
			apiRoutePrefix:    "/v1/projects/{ref}/database/cron-jobs",
			cliCommand:        "database-cron",
			mcpTool:           "supadupa_list_project_database_cron_jobs",
			terraformResource: "NewProjectDatabaseCronJobResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("database_queues", func(s *MemoryStore) map[string][]ProjectDatabaseQueue { return s.databaseQueues },
			func(usage *OrgUsage, count int) { usage.DatabaseQueues += count },
			func(metrics *FleetMetrics, count int) { metrics.DatabaseQueues += count },
			func(metrics *ProjectMetrics, count int) { metrics.DatabaseQueues = count },
		),
		projectChildResourceInventory{
			memoryField:       "databaseQueues",
			snapshotField:     "DatabaseQueues",
			normalizedTable:   "database_queues",
			apiRoutePrefix:    "/v1/projects/{ref}/database/queues",
			cliCommand:        "database-queues",
			mcpTool:           "supadupa_list_project_database_queues",
			terraformResource: "NewProjectDatabaseQueueResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("database_webhooks", func(s *MemoryStore) map[string][]ProjectDatabaseWebhook { return s.databaseWebhooks },
			func(usage *OrgUsage, count int) { usage.DatabaseWebhooks += count },
			func(metrics *FleetMetrics, count int) { metrics.DatabaseWebhooks += count },
			func(metrics *ProjectMetrics, count int) { metrics.DatabaseWebhooks = count },
		),
		projectChildResourceInventory{
			memoryField:       "databaseWebhooks",
			snapshotField:     "DatabaseWebhooks",
			normalizedTable:   "database_webhooks",
			apiRoutePrefix:    "/v1/projects/{ref}/database/webhooks",
			cliCommand:        "database-webhooks",
			mcpTool:           "supadupa_list_project_database_webhooks",
			terraformResource: "NewProjectDatabaseWebhookResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("database_schemas", func(s *MemoryStore) map[string][]ProjectDatabaseSchema { return s.databaseSchemas },
			func(usage *OrgUsage, count int) { usage.DatabaseSchemas += count },
			func(metrics *FleetMetrics, count int) { metrics.DatabaseSchemas += count },
			func(metrics *ProjectMetrics, count int) { metrics.DatabaseSchemas = count },
		),
		projectChildResourceInventory{
			memoryField:       "databaseSchemas",
			snapshotField:     "DatabaseSchemas",
			normalizedTable:   "database_schemas",
			apiRoutePrefix:    "/v1/projects/{ref}/database/schemas",
			cliCommand:        "database-schemas",
			mcpTool:           "supadupa_list_project_database_schemas",
			terraformResource: "NewProjectDatabaseSchemaResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("database_roles", func(s *MemoryStore) map[string][]ProjectDatabaseRole { return s.databaseRoles },
			func(usage *OrgUsage, count int) { usage.DatabaseRoles += count },
			func(metrics *FleetMetrics, count int) { metrics.DatabaseRoles += count },
			func(metrics *ProjectMetrics, count int) { metrics.DatabaseRoles = count },
		),
		projectChildResourceInventory{
			memoryField:       "databaseRoles",
			snapshotField:     "DatabaseRoles",
			normalizedTable:   "database_roles",
			apiRoutePrefix:    "/v1/projects/{ref}/database/roles",
			cliCommand:        "database-roles",
			mcpTool:           "supadupa_list_project_database_roles",
			terraformResource: "NewProjectDatabaseRoleResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("storage_buckets", func(s *MemoryStore) map[string][]ProjectStorageBucket { return s.storageBuckets },
			func(usage *OrgUsage, count int) { usage.StorageBuckets += count },
			func(metrics *FleetMetrics, count int) { metrics.StorageBuckets += count },
			func(metrics *ProjectMetrics, count int) { metrics.StorageBuckets = count },
		),
		projectChildResourceInventory{
			memoryField:       "storageBuckets",
			snapshotField:     "StorageBuckets",
			normalizedTable:   "storage_buckets",
			apiRoutePrefix:    "/v1/projects/{ref}/storage/buckets",
			cliCommand:        "storage-buckets",
			mcpTool:           "supadupa_list_project_storage_buckets",
			terraformResource: "NewProjectStorageBucketResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("vector_buckets", func(s *MemoryStore) map[string][]ProjectVectorBucket { return s.vectorBuckets },
			func(usage *OrgUsage, count int) { usage.VectorBuckets += count },
			func(metrics *FleetMetrics, count int) { metrics.VectorBuckets += count },
			func(metrics *ProjectMetrics, count int) { metrics.VectorBuckets = count },
		),
		projectChildResourceInventory{
			memoryField:       "vectorBuckets",
			snapshotField:     "VectorBuckets",
			normalizedTable:   "vector_buckets",
			apiRoutePrefix:    "/v1/projects/{ref}/vector-buckets",
			cliCommand:        "vector-buckets",
			mcpTool:           "supadupa_list_project_vector_buckets",
			terraformResource: "NewProjectVectorBucketResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("analytics_buckets", func(s *MemoryStore) map[string][]ProjectAnalyticsBucket { return s.analyticsBuckets },
			func(usage *OrgUsage, count int) { usage.AnalyticsBuckets += count },
			func(metrics *FleetMetrics, count int) { metrics.AnalyticsBuckets += count },
			func(metrics *ProjectMetrics, count int) { metrics.AnalyticsBuckets = count },
		),
		projectChildResourceInventory{
			memoryField:       "analyticsBuckets",
			snapshotField:     "AnalyticsBuckets",
			normalizedTable:   "analytics_buckets",
			apiRoutePrefix:    "/v1/projects/{ref}/analytics-buckets",
			cliCommand:        "analytics-buckets",
			mcpTool:           "supadupa_list_project_analytics_buckets",
			terraformResource: "NewProjectAnalyticsBucketResource",
		},
	),
	withProjectChildInventory(
		projectChildMapResource("cdn_policies", func(s *MemoryStore) map[string]ProjectCDNPolicy { return s.cdnPolicies }),
		projectChildResourceInventory{
			memoryField:       "cdnPolicies",
			snapshotField:     "CDNPolicies",
			normalizedTable:   "cdn_policies",
			apiRoutePrefix:    "/v1/projects/{ref}/cdn/policy",
			cliCommand:        "cdn",
			mcpTool:           "supadupa_get_project_cdn_policy",
			terraformResource: "NewProjectCDNPolicyResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("cdn_invalidations", func(s *MemoryStore) map[string][]CDNInvalidation { return s.cdnInvalidations },
			func(usage *OrgUsage, count int) { usage.CDNInvalidations += count },
			func(metrics *FleetMetrics, count int) { metrics.CDNInvalidations += count },
			func(metrics *ProjectMetrics, count int) { metrics.CDNInvalidations = count },
		),
		projectChildResourceInventory{
			memoryField:     "cdnInvalidations",
			snapshotField:   "CDNInvalidations",
			normalizedTable: "cdn_invalidations",
			apiRoutePrefix:  "/v1/projects/{ref}/cdn/invalidations",
			cliCommand:      "cdn",
			mcpTool:         "supadupa_list_project_cdn_invalidations",
			omittedSurfaces: map[projectChildResourceSurface]string{
				projectChildSurfaceTerraform: "invalidation events are imperative operations, not desired-state Terraform resources",
			},
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("network_connections", func(s *MemoryStore) map[string][]ProjectNetworkConnection { return s.networkConnections },
			func(usage *OrgUsage, count int) { usage.NetworkConnections += count },
			func(metrics *FleetMetrics, count int) { metrics.NetworkConnections += count },
			func(metrics *ProjectMetrics, count int) { metrics.NetworkConnections = count },
		),
		projectChildResourceInventory{
			memoryField:       "networkConnections",
			snapshotField:     "NetworkConnections",
			normalizedTable:   "network_connections",
			apiRoutePrefix:    "/v1/projects/{ref}/network-connections",
			cliCommand:        "network-connections",
			mcpTool:           "supadupa_list_project_network_connections",
			terraformResource: "NewProjectNetworkConnectionResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("log_drains", func(s *MemoryStore) map[string][]LogDrain { return s.logDrains },
			func(usage *OrgUsage, count int) { usage.LogDrains += count },
			func(metrics *FleetMetrics, count int) { metrics.LogDrains += count },
			func(metrics *ProjectMetrics, count int) { metrics.LogDrains = count },
		),
		projectChildResourceInventory{
			memoryField:       "logDrains",
			snapshotField:     "LogDrains",
			normalizedTable:   "log_drains",
			apiRoutePrefix:    "/v1/projects/{ref}/log-drains",
			cliCommand:        "log-drains",
			mcpTool:           "supadupa_list_project_log_drains",
			terraformResource: "NewProjectLogDrainResource",
		},
	),
	withProjectChildInventory(
		projectChildNestedMapResource("secrets", func(s *MemoryStore) map[string]map[string]ProjectSecret { return s.secrets },
			func(usage *OrgUsage, count int) { usage.Secrets += count },
			func(metrics *ProjectMetrics, count int) { metrics.Secrets = count },
		),
		projectChildResourceInventory{
			memoryField:     "secrets",
			snapshotField:   "Secrets",
			normalizedTable: "secrets",
			apiRoutePrefix:  "/v1/projects/{ref}/secrets",
			cliCommand:      "secrets",
			mcpTool:         "supadupa_list_project_secrets",
			omittedSurfaces: map[projectChildResourceSurface]string{
				projectChildSurfaceTerraform: "secret reveal/copy/rotate surfaces are intentionally not Terraform-managed",
			},
		},
	),
	withProjectChildInventory(
		projectChildMapResource("backup_policies", func(s *MemoryStore) map[string]BackupPolicy { return s.policies }),
		projectChildResourceInventory{
			memoryField:       "policies",
			snapshotField:     "Policies",
			normalizedTable:   "backup_policies",
			apiRoutePrefix:    "/v1/projects/{ref}/backups/policy",
			cliCommand:        "backups",
			mcpTool:           "supadupa_get_project_backup_policy",
			terraformResource: "NewProjectBackupPolicyResource",
		},
	),
	withProjectChildInventory(
		projectChildMapResource("pitr_policies", func(s *MemoryStore) map[string]PITRPolicy { return s.pitrPolicies }),
		projectChildResourceInventory{
			memoryField:       "pitrPolicies",
			snapshotField:     "PITRPolicies",
			normalizedTable:   "pitr_policies",
			apiRoutePrefix:    "/v1/projects/{ref}/pitr/policy",
			cliCommand:        "pitr",
			mcpTool:           "supadupa_get_project_pitr_policy",
			terraformResource: "NewProjectPITRPolicyResource",
		},
	),
	withProjectChildInventory(
		projectChildSliceResource("project_access", func(s *MemoryStore) map[string][]ProjectAccessGrant { return s.projectAccess },
			nil,
			nil,
			nil,
		),
		projectChildResourceInventory{
			memoryField:       "projectAccess",
			snapshotField:     "ProjectAccess",
			normalizedTable:   "project_access_grants",
			apiRoutePrefix:    "/v1/projects/{ref}/access",
			cliCommand:        "access",
			mcpTool:           "supadupa_list_project_access",
			terraformResource: "NewProjectAccessGrantResource",
		},
	),
	withProjectChildInventory(
		projectChildMapResource("telemetry", func(s *MemoryStore) map[string]TelemetrySample { return s.telemetry }),
		projectChildResourceInventory{
			memoryField:    "telemetry",
			snapshotField:  "Telemetry",
			apiRoutePrefix: "/v1/projects/{ref}/telemetry",
			omittedSurfaces: map[projectChildResourceSurface]string{
				projectChildSurfaceTable:     "telemetry samples are checkpointed and treated as transient scheduler observations rather than normalized durable configuration",
				projectChildSurfaceCLI:       "telemetry is posted by collectors and surfaced through metrics commands",
				projectChildSurfaceMCP:       "telemetry is exposed through project metrics tools rather than a telemetry mutation tool",
				projectChildSurfaceTerraform: "telemetry is observed runtime state, not Terraform-managed desired state",
			},
		},
	),
}

func (s *MemoryStore) cleanupRegisteredProjectChildrenLocked(ref string) {
	for _, resource := range projectChildResourceRegistry {
		if resource.cleanup != nil {
			resource.cleanup(s, ref)
		}
	}
}

func (s *MemoryStore) addRegisteredProjectChildOrgUsageLocked(ref string, usage *OrgUsage) {
	for _, resource := range projectChildResourceRegistry {
		if resource.addOrgUsage != nil {
			resource.addOrgUsage(s, ref, usage)
		}
	}
}

func (s *MemoryStore) addRegisteredProjectChildFleetMetricsLocked(metrics *FleetMetrics) {
	for _, resource := range projectChildResourceRegistry {
		if resource.addFleetMetrics != nil {
			resource.addFleetMetrics(s, metrics)
		}
	}
}

func (s *MemoryStore) addRegisteredProjectChildMetricsLocked(ref string, metrics *ProjectMetrics) {
	for _, resource := range projectChildResourceRegistry {
		if resource.addProjectMetrics != nil {
			resource.addProjectMetrics(s, ref, metrics)
		}
	}
}

func projectChildNormalizedDeleteStatements() []string {
	tables := projectChildNormalizedTables()
	statements := make([]string, 0, len(tables))
	for _, table := range tables {
		statements = append(statements, "DELETE FROM "+table)
	}
	return statements
}

func projectChildNormalizedTables() []string {
	tables := make([]string, 0, len(projectChildResourceRegistry))
	for _, resource := range projectChildResourceRegistry {
		table := resource.inventory.normalizedTable
		if table != "" {
			tables = append(tables, table)
		}
	}
	return tables
}
