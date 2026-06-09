package api

import (
	"net/http"
	"time"

	"supadupa2026/internal/control"
)

type projectStudioSessionResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

func createProjectHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if provisioner == nil {
			writeError(w, http.StatusServiceUnavailable, "provisioner is not configured")
			return
		}

		var payload control.CreateProjectRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
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
		// Best-effort: enforce RLS on internal tables if the project opts in. The
		// internal tables may not exist until Realtime finishes migrating, so this
		// is also re-applied whenever the database config is saved.
		if err := applyProjectSystemRLS(r.Context(), store, project); err != nil {
			control.LogProject(r.Context(), store, project.Ref, "warning", "System-table RLS enforcement skipped", map[string]string{"error": err.Error()})
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
			writeDecodeError(w, err)
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

func createProjectStudioSessionHandler(store control.Store, studioSessions *studioSessionStore) http.HandlerFunc {
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
		code, expiresAt, err := studioSessions.Create(claims, project.Ref, ttl)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, projectStudioSessionResponse{
			Code:      code,
			ExpiresAt: expiresAt,
		})
	}
}
