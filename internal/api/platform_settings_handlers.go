package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"supadupa2026/internal/control"
)

func getPlatformDefaultsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
		defaults, err := store.GetPlatformDefaults(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, defaults)
	}
}

func updatePlatformDefaultsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
		var payload control.PlatformDefaultsInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		defaults, err := store.UpdatePlatformDefaults(r.Context(), payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "settings.defaults_update", "platform:defaults", map[string]string{
			"domain":                         defaults.Domain,
			"stack_version":                  defaults.StackVersion,
			"profile":                        string(defaults.Profile),
			"resource_tier":                  string(defaults.ResourceTier),
			"backup_schedule":                defaults.BackupSchedule,
			"database_ingress_allowed_cidrs": fmt.Sprintf("%d", len(defaults.DatabaseIngressAllowedCIDRs)),
			"smtp_enabled":                   strconv.FormatBool(defaults.SMTP.Enabled),
			"smtp_host":                      defaults.SMTP.Host,
			"smtp_tls_mode":                  defaults.SMTP.TLSMode,
		})
		routePaths, err := reconcileAllProjectRoutes(r, store)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if len(routePaths) > 0 {
			control.Audit(r.Context(), store, "settings.database_ingress_routes_reconciled", "platform:defaults", map[string]string{
				"projects": fmt.Sprintf("%d", len(routePaths)),
			})
		}
		writeJSON(w, http.StatusOK, defaults)
	}
}

func getPlatformSSOHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
		config, err := store.GetPlatformSSOConfig(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, config)
	}
}

func updatePlatformSSOHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
		var payload control.PlatformSSOConfigInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		config, err := store.UpdatePlatformSSOConfig(r.Context(), payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "settings.sso_update", "platform:sso", map[string]string{
			"enabled":               fmt.Sprintf("%t", config.Enabled),
			"provider":              config.Provider,
			"idp_entity":            config.IDPEntityID,
			"email_domain":          config.EmailDomain,
			"scim_enabled":          fmt.Sprintf("%t", config.SCIMEnabled),
			"scim_token_configured": fmt.Sprintf("%t", config.SCIMTokenConfigured),
		})
		writeJSON(w, http.StatusOK, config)
	}
}

func listBackupStorageTargetsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
		targets, err := store.ListBackupStorageTargets(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, targets)
	}
}

func createBackupStorageTargetHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
		var payload control.BackupStorageTargetInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		target, err := store.CreateBackupStorageTarget(r.Context(), payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "settings.backup_storage_target_create", "backup-storage-target:"+target.ID, backupStorageTargetAuditMetadata(target))
		writeJSON(w, http.StatusCreated, target)
	}
}

func updateBackupStorageTargetHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
		var payload control.BackupStorageTargetInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		target, err := store.UpdateBackupStorageTarget(r.Context(), r.PathValue("id"), payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "settings.backup_storage_target_update", "backup-storage-target:"+target.ID, backupStorageTargetAuditMetadata(target))
		writeJSON(w, http.StatusOK, target)
	}
}

func testBackupStorageTargetHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
		id := r.PathValue("id")
		target, err := store.GetBackupStorageTarget(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		testedAt := time.Now().UTC()
		status := "passed"
		message := ""
		if err := control.TestBackupStorageTarget(r.Context(), target); err != nil {
			status = "failed"
			message = err.Error()
		}
		updated, err := store.UpdateBackupStorageTargetTestResult(r.Context(), id, testedAt, status, message)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := backupStorageTargetAuditMetadata(updated)
		metadata["test_status"] = status
		if message != "" {
			metadata["test_error"] = message
		}
		control.Audit(r.Context(), store, "settings.backup_storage_target_test", "backup-storage-target:"+updated.ID, metadata)
		writeJSON(w, http.StatusOK, updated)
	}
}

func deleteBackupStorageTargetHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
		id := r.PathValue("id")
		if err := store.DeleteBackupStorageTarget(r.Context(), id); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "settings.backup_storage_target_delete", "backup-storage-target:"+id, map[string]string{"id": id})
		w.WriteHeader(http.StatusNoContent)
	}
}

func backupStorageTargetAuditMetadata(target control.BackupStorageTarget) map[string]string {
	return map[string]string{
		"id":               target.ID,
		"name":             target.Name,
		"type":             target.Type,
		"endpoint":         target.Endpoint,
		"region":           target.Region,
		"bucket":           target.Bucket,
		"prefix":           target.Prefix,
		"default":          strconv.FormatBool(target.Default),
		"force_path_style": strconv.FormatBool(target.ForcePathStyle),
	}
}
