package api

import (
	"net"
	"net/http"
	"os"
	"strings"
	"supadupa2026/internal/env"

	"supadupa2026/internal/control"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": control.Version})
}

func provisionerHandler(provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
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
		composeApply := env.BoolValue(os.Getenv("SUPADUPA_COMPOSE_APPLY"))
		kubernetesApply := env.BoolValue(os.Getenv("SUPADUPA_K8S_APPLY"))
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
				"backup_dry_run":             env.BoolValue(os.Getenv("SUPADUPA_BACKUP_DRY_RUN")),
				"restore_dry_run":            env.BoolValue(os.Getenv("SUPADUPA_RESTORE_DRY_RUN")),
				"wal_archive_dry_run":        env.BoolValue(os.Getenv("SUPADUPA_WAL_ARCHIVE_DRY_RUN")),
			},
			"recovery": map[string]bool{
				"require_recovery_ready_targets": env.BoolValue(os.Getenv("SUPADUPA_REQUIRE_RECOVERY_READY_TARGETS")),
			},
			"upgrade": map[string]bool{
				"require_durable_backup": env.BoolValue(os.Getenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP")),
				"failure_auto_restore":   env.BoolValue(os.Getenv("SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE")),
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
		defaults, err := store.GetPlatformDefaults(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metrics.DatabaseIngress = databaseIngressStatusFromEnvAndDefaults(os.Getenv, defaults)
		writeJSON(w, http.StatusOK, metrics)
	}
}

func databaseIngressStatusFromEnv(getenv func(string) string) control.DatabaseIngressStatus {
	return databaseIngressStatusFromEnvAndDefaults(getenv, control.PlatformDefaults{})
}

func databaseIngressStatusFromEnvAndDefaults(getenv func(string) string, defaults control.PlatformDefaults) control.DatabaseIngressStatus {
	postgresAddr := strings.TrimSpace(getenv("SUPADUPA_POSTGRES_ADDR"))
	if postgresAddr == "" {
		postgresAddr = "127.0.0.1:5432"
	}
	poolerAddr := strings.TrimSpace(getenv("SUPADUPA_POOLER_ADDR"))
	if poolerAddr == "" {
		poolerAddr = "127.0.0.1:6543"
	}
	allowedCIDRs := splitCIDRs(getenv("SUPADUPA_DB_INGRESS_ALLOWED_CIDRS"))
	if len(defaults.DatabaseIngressAllowedCIDRs) > 0 {
		allowedCIDRs = append([]string(nil), defaults.DatabaseIngressAllowedCIDRs...)
	}
	status := control.DatabaseIngressStatus{
		Mode:                "private",
		PostgresAddr:        postgresAddr,
		PoolerAddr:          poolerAddr,
		PostgresPublic:      bindAddressIsPublic(postgresAddr),
		PoolerPublic:        bindAddressIsPublic(poolerAddr),
		AllowlistConfigured: len(allowedCIDRs) > 0,
		AllowedCIDRs:        allowedCIDRs,
		Warnings:            []string{},
	}
	status.Public = status.PostgresPublic || status.PoolerPublic
	if status.Public {
		status.Mode = "public"
		// The host publishing these ports is the intended default; actual
		// reachability is gated PER PROJECT at the edge router (private by
		// default). So this is informational, not a fleet-wide allowlist alarm.
		status.Warnings = append(status.Warnings, "Database ports are published on the host. Each project stays private until you open it under Config → Network.")
	}
	return status
}

func splitCIDRs(raw string) []string {
	out := []string{}
	for _, value := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func bindAddressIsPublic(addr string) bool {
	host := strings.TrimSpace(addr)
	if parsedHost, _, err := net.SplitHostPort(addr); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return !ip.IsLoopback()
	}
	return host != "localhost"
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
