package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"supadupa2026/internal/env"

	"supadupa2026/internal/control"
)

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
			writeDecodeError(w, err)
			return
		}
		targetVersion := strings.TrimSpace(payload.Version)
		if err := validateUpgradeTarget(project.Spec.StackVersion, targetVersion); err != nil {
			writeDecodeError(w, err)
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
	return env.BoolValue(os.Getenv("SUPADUPA_REQUIRE_DURABLE_UPGRADE_BACKUP"))
}

func autoRestoreFailedUpgradeBackup() bool {
	return env.BoolValue(os.Getenv("SUPADUPA_UPGRADE_FAILURE_AUTO_RESTORE"))
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
			writeDecodeError(w, err)
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
