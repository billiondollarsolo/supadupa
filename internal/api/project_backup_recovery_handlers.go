package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"supadupa2026/internal/control"
)

type restoreBackupRequest struct {
	BackupID     string `json:"backup_id"`
	Confirmation string `json:"confirmation"`
}

type restoreBackupResponse struct {
	Backup       control.Backup `json:"backup"`
	RestorePath  string         `json:"restore_path"`
	RestoreState string         `json:"restore_state"`
}

type restorePITRBackupRequest struct {
	RecoveryTimeTargetUnix string `json:"recovery_time_target_unix"`
	Confirmation           string `json:"confirmation"`
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
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload restoreBackupRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		if !restoreConfirmationMatches(ref, "logical", payload.Confirmation) {
			writeError(w, http.StatusBadRequest, `confirmation must be "restore project `+ref+`"`)
			return
		}
		backup, restore, err := backupService.RestoreBackup(r.Context(), store, ref, payload.BackupID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Restore "+restore.State, map[string]string{"backup_id": backup.ID, "restore_path": restore.Path})
		control.Audit(r.Context(), store, "project.restore", "project:"+ref, map[string]string{"backup_id": backup.ID, "state": restore.State, "restore_type": "logical", "confirmation": "present"})
		writeJSON(w, http.StatusAccepted, restoreBackupResponse{Backup: backup, RestorePath: restore.Path, RestoreState: restore.State})
	}
}

func restorePITRBackupHandler(store control.Store) http.HandlerFunc {
	backupService := control.NewBackupService("")
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload restorePITRBackupRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		if !restoreConfirmationMatches(ref, "pitr", payload.Confirmation) {
			writeError(w, http.StatusBadRequest, `confirmation must be "restore pitr project `+ref+`"`)
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
		control.Audit(r.Context(), store, "project.restore_pitr", "project:"+ref, map[string]string{"state": result.State, "restore_type": "pitr", "confirmation": "present", "recovery_time_target_unix": fmt.Sprintf("%d", result.RecoveryTimeTargetUnix)})
		writeJSON(w, http.StatusCreated, restorePITRBackupResponse{
			ProjectRef:             ref,
			RecoveryTimeTargetUnix: result.RecoveryTimeTargetUnix,
			RecoveryTimeTarget:     result.RecoveryTimeTarget,
			RestorePath:            result.Path,
			RestoreState:           result.State,
		})
	}
}

func restoreConfirmationMatches(ref string, restoreType string, confirmation string) bool {
	expected := "restore project " + ref
	if restoreType == "pitr" {
		expected = "restore pitr project " + ref
	}
	return strings.TrimSpace(confirmation) == expected
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
			writeDecodeError(w, err)
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
			writeDecodeError(w, err)
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
