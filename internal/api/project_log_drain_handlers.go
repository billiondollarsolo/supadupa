package api

import (
	"errors"
	"net/http"
	"os"

	"supadupa2026/internal/control"
)

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
			writeDecodeError(w, err)
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

func updateProjectLogDrainHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		if !requireProjectFeature(w, r, store, project, "log_drains") {
			return
		}
		id := r.PathValue("id")
		var payload control.LogDrainInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		drain, err := store.UpdateProjectLogDrain(r.Context(), ref, id, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		artifact, err := control.NewLogDrainDeploymentService().Deploy(r.Context(), drain)
		if err != nil {
			control.LogProject(r.Context(), store, ref, "error", "Log drain redeployment failed", map[string]string{"target": drain.Target, "id": drain.ID, "error": err.Error()})
			control.Audit(r.Context(), store, "project.log_drain_deploy_failed", "project:"+ref, map[string]string{"target": drain.Target, "id": drain.ID, "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "Log drain updated", map[string]string{"target": drain.Target, "id": drain.ID, "artifact_path": artifact.Path})
		control.Audit(r.Context(), store, "project.log_drain_update", "project:"+ref, map[string]string{"target": drain.Target, "id": drain.ID, "artifact_path": artifact.Path})
		writeJSON(w, http.StatusOK, maskLogDrainConfig(drain))
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
	drain.Config = maskSensitiveStringMap(drain.Config, isSensitiveMetadataKey)
	return drain
}
