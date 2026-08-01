package api

import (
	"fmt"
	"sort"
	"strings"

	"supadupa2026/internal/control"
)

func renderPrometheusMetrics(metrics control.FleetMetrics, projects []control.ProjectMetrics) string {
	var builder strings.Builder
	projectMetricFamilies := map[string]bool{}
	nodeMetricFamilies := map[string]bool{}
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
	writeNodeMetric := func(name string, help string, node control.NodeTelemetrySample, value any) {
		if !nodeMetricFamilies[name] {
			builder.WriteString(fmt.Sprintf("# HELP %s %s\n", name, help))
			builder.WriteString(fmt.Sprintf("# TYPE %s gauge\n", name))
			nodeMetricFamilies[name] = true
		}
		builder.WriteString(name)
		builder.WriteString(prometheusLabels(map[string]string{"host_id": node.HostID, "source": node.Source}))
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
	for _, node := range metrics.NodeObserved {
		writeNodeMetric("supadupa_node_cpu_percent", "Observed node CPU percent.", node, node.CPUPercent)
		writeNodeMetric("supadupa_node_cpu_used_cores", "Observed node CPU used cores.", node, node.CPUUsedCores)
		writeNodeMetric("supadupa_node_cpu_capacity_cores", "Observed node CPU capacity cores.", node, node.CPUCapacityCores)
		writeNodeMetric("supadupa_node_memory_used_bytes", "Observed node memory used bytes.", node, node.MemoryUsedBytes)
		writeNodeMetric("supadupa_node_memory_total_bytes", "Observed node memory total bytes.", node, node.MemoryTotalBytes)
		writeNodeMetric("supadupa_node_disk_used_bytes", "Observed node disk used bytes.", node, node.DiskUsedBytes)
		writeNodeMetric("supadupa_node_disk_total_bytes", "Observed node disk total bytes.", node, node.DiskTotalBytes)
		writeNodeMetric("supadupa_node_disk_available_bytes", "Observed node disk available bytes.", node, node.DiskAvailableBytes)
		writeNodeMetric("supadupa_node_network_sampled", "Whether node network counters were sampled, 1 for true and 0 for false.", node, boolMetric(node.NetworkSampled))
		writeNodeMetric("supadupa_node_network_rx_bytes", "Observed node network receive bytes.", node, node.NetworkRxBytes)
		writeNodeMetric("supadupa_node_network_tx_bytes", "Observed node network transmit bytes.", node, node.NetworkTxBytes)
		writeNodeMetric("supadupa_node_telemetry_sampled_at_unix", "Unix timestamp when node telemetry was sampled.", node, node.SampledAt.Unix())
	}
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
	writeMetric("supadupa_rollback_failures_total", "Total best-effort project-child rollback delete failures after apply-path errors.", RollbackFailureTotal())
	writeMetric("supadupa_legacy_password_hash_verifies_total", "Successful password verifications that used a legacy sha256$ hash since process start (B12 migration pressure).", control.LegacyPasswordHashVerifyCount())
	writeMetric("supadupa_legacy_scim_hash_verifies_total", "Successful SCIM token verifications that used a legacy unkeyed SHA-256 hash since process start (B12 migration pressure).", control.LegacySCIMHashVerifyCount())
	writeMetric("supadupa_legacy_mfa_plaintext_loads_total", "Normalized MFA seed loads that read legacy plaintext (no encryption envelope) since process start (B10 migration pressure).", control.LegacyMFAPlaintextLoadCount())
	writeMetric("supadupa_metrics_sampled_at_unix", "Unix timestamp when fleet metrics were sampled.", metrics.SampledAt.Unix())
	for _, project := range projects {
		writeProjectMetric("supadupa_project_resource_cpu", "Reserved CPU for a project.", project, nil, project.Resources.CPU)
		writeProjectMetric("supadupa_project_resource_ram_mb", "Reserved RAM for a project in MB.", project, nil, project.Resources.RAMMB)
		writeProjectMetric("supadupa_project_resource_disk_gb", "Reserved disk for a project in GB.", project, nil, project.Resources.DiskGB)
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
