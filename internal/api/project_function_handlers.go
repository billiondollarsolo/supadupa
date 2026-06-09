package api

import (
	"fmt"
	"net/http"
	"strings"

	"supadupa2026/internal/control"
)

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
			writeDecodeError(w, err)
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
			writeDecodeError(w, err)
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
			writeDecodeError(w, err)
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
