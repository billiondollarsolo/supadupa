package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"supadupa2026/internal/control"
)

type restorePlatformBackupRequest struct {
	Confirm string `json:"confirm"`
}

type restorePlatformBackupResponse struct {
	Backup            control.PlatformBackup `json:"backup"`
	RestorePath       string                 `json:"restore_path"`
	RestoreState      string                 `json:"restore_state"`
	RuntimeReconciled int                    `json:"runtime_reconciled"`
	RuntimeDestroyed  int                    `json:"runtime_destroyed"`
	RuntimeErrors     []string               `json:"runtime_errors,omitempty"`
}

type platformRuntimeRestoreSummary struct {
	Reconciled int
	Destroyed  int
	Errors     []string
}

func listPlatformBackupsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
		backups, err := store.ListPlatformBackups(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, backups)
	}
}

func triggerPlatformBackupHandler(store control.Store) http.HandlerFunc {
	backupService := control.NewBackupService("")
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
		backup, err := backupService.TriggerPlatformBackup(r.Context(), store)
		if err != nil {
			control.Audit(r.Context(), store, "platform.backup_failed", "platform:control-plane", map[string]string{"error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		control.Audit(r.Context(), store, "platform.backup", "platform:control-plane", map[string]string{
			"backup_id":         backup.ID,
			"storage_target_id": backup.StorageTargetID,
			"remote_location":   backup.RemoteLocation,
		})
		writeJSON(w, http.StatusCreated, backup)
	}
}

func restorePlatformBackupHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	backupService := control.NewBackupService("")
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
		var payload restorePlatformBackupRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		if strings.TrimSpace(payload.Confirm) != "restore-control-plane" {
			writeError(w, http.StatusBadRequest, `confirm must be "restore-control-plane"`)
			return
		}
		beforeProjects, err := store.ListProjects(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		backup, restore, err := backupService.RestorePlatformBackup(r.Context(), store, r.PathValue("id"))
		if err != nil {
			control.Audit(r.Context(), store, "platform.restore_failed", "platform:control-plane", map[string]string{"backup_id": r.PathValue("id"), "error": err.Error()})
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		runtime := reconcilePlatformRestoreRuntime(r.Context(), store, provisioner, beforeProjects)
		state := restore.State
		if provisioner != nil && len(runtime.Errors) == 0 {
			state = "reconciled"
		}
		if len(runtime.Errors) > 0 {
			state = "metadata-restored-runtime-errors"
		}
		control.Audit(r.Context(), store, "platform.restore", "platform:control-plane", map[string]string{
			"backup_id":           backup.ID,
			"state":               state,
			"runtime_reconciled":  strconv.Itoa(runtime.Reconciled),
			"runtime_destroyed":   strconv.Itoa(runtime.Destroyed),
			"runtime_error_count": strconv.Itoa(len(runtime.Errors)),
		})
		writeJSON(w, http.StatusAccepted, restorePlatformBackupResponse{
			Backup:            backup,
			RestorePath:       restore.Path,
			RestoreState:      state,
			RuntimeReconciled: runtime.Reconciled,
			RuntimeDestroyed:  runtime.Destroyed,
			RuntimeErrors:     runtime.Errors,
		})
	}
}

func reconcilePlatformRestoreRuntime(ctx context.Context, store control.Store, provisioner control.Provisioner, beforeProjects []control.Project) platformRuntimeRestoreSummary {
	var summary platformRuntimeRestoreSummary
	if provisioner == nil {
		return summary
	}
	afterProjects, err := store.ListProjects(ctx)
	if err != nil {
		summary.Errors = append(summary.Errors, err.Error())
		return summary
	}
	beforeRefs := map[string]struct{}{}
	for _, project := range beforeProjects {
		beforeRefs[project.Ref] = struct{}{}
	}
	afterRefs := map[string]struct{}{}
	for _, project := range afterProjects {
		afterRefs[project.Ref] = struct{}{}
		if project.Status == control.ProjectDestroying {
			continue
		}
		if err := provisioner.Create(ctx, project.Spec); err != nil {
			message := fmt.Sprintf("reconcile restored project %s: %v", project.Ref, err)
			summary.Errors = append(summary.Errors, message)
			control.LogProject(ctx, store, project.Ref, "error", "Runtime restore reconcile failed", map[string]string{"error": err.Error()})
			control.Audit(ctx, store, "project.restore_reconcile_failed", "project:"+project.Ref, map[string]string{"error": err.Error()})
			continue
		}
		summary.Reconciled++
		control.LogProject(ctx, store, project.Ref, "info", "Runtime reconciled after control-plane restore", map[string]string{"provisioner": provisioner.Name()})
	}
	for ref := range beforeRefs {
		if _, ok := afterRefs[ref]; ok {
			continue
		}
		var err error
		if destroyer, ok := provisioner.(control.OptionedDestroyer); ok {
			err = destroyer.DestroyWithOptions(ctx, ref, control.DestroyOptions{RetainVolumes: true})
		} else {
			err = provisioner.Destroy(ctx, ref)
		}
		if err != nil {
			message := fmt.Sprintf("stop stale restored-away project %s: %v", ref, err)
			summary.Errors = append(summary.Errors, message)
			control.Audit(ctx, store, "project.restore_stale_destroy_failed", "project:"+ref, map[string]string{"error": err.Error()})
			continue
		}
		if err := cleanupProjectRuntimeArtifacts(ctx, ref); err != nil {
			message := fmt.Sprintf("cleanup stale restored-away project %s: %v", ref, err)
			summary.Errors = append(summary.Errors, message)
			control.Audit(ctx, store, "project.restore_stale_cleanup_failed", "project:"+ref, map[string]string{"error": err.Error()})
			continue
		}
		summary.Destroyed++
		control.Audit(ctx, store, "project.restore_stale_destroyed", "project:"+ref, map[string]string{"retain_volumes": "true"})
	}
	return summary
}

func cleanupProjectRuntimeArtifacts(ctx context.Context, ref string) error {
	if err := control.NewRoutingService("").RemoveProject(ref); err != nil {
		return fmt.Errorf("remove routes: %w", err)
	}
	if err := control.NewCertificateService().RemoveProject(ctx, ref); err != nil {
		return fmt.Errorf("remove certificates: %w", err)
	}
	return nil
}
