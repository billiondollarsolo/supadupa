package api

import (
	"fmt"
	"net/http"
	"strings"

	"supadupa2026/internal/control"
)

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
			writeDecodeError(w, err)
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
			writeDecodeError(w, err)
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
			writeDecodeError(w, err)
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
			"project_ref":    ref,
			"config":         config,
			"connections":    maskNetworkConnections(connections),
			"http_allowlist": strings.TrimSpace(config.Config["http_allowlist"]),
			"db_allowlist":   strings.TrimSpace(config.Config["db_allowlist"]),
			"ssl_enforced":   strings.TrimSpace(config.Config["ssl_enforced"]),
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
			writeDecodeError(w, err)
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
	connection.Config = maskSensitiveStringMap(connection.Config, isSensitiveMetadataKey)
	return connection
}
