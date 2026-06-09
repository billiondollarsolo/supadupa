package api

import (
	"fmt"
	"net/http"
	"strings"

	"supadupa2026/internal/control"
)

type createBranchResponse struct {
	Branch  control.ProjectBranch `json:"branch"`
	Project control.Project       `json:"project"`
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
			writeDecodeError(w, err)
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
			writeDecodeError(w, err)
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
