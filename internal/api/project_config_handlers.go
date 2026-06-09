package api

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"supadupa2026/internal/control"
)

func getProjectConfigHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		config, err := store.GetProjectConfig(r.Context(), ref, r.PathValue("area"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, config)
	}
}

func getProjectServicesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		services, err := store.GetProjectServices(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, services)
	}
}

func updateProjectServicesHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectServicesInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		project, err := store.UpdateProjectServices(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := serviceAuditMetadata(project.Spec.Services)
		if syncer, ok := provisioner.(control.ServiceSyncer); ok {
			if err := syncer.SyncServices(r.Context(), ref, project.Spec); err != nil {
				control.LogProject(r.Context(), store, ref, "error", "Project services sync failed", map[string]string{"error": err.Error()})
				control.Audit(r.Context(), store, "project.services_sync_failed", "project:"+ref, map[string]string{"error": err.Error()})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			metadata["runtime_synced"] = "true"
		}
		routePath, err := reconcileProjectRoutes(r, store, ref)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata["route_path"] = routePath
		project, err = store.UpdateProjectStatus(r.Context(), ref, control.ProjectHealthy, "enabled services updated")
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "info", "Project services updated", metadata)
		control.Audit(r.Context(), store, "project.services_update", "project:"+ref, metadata)
		writeJSON(w, http.StatusOK, control.ProjectServices{ProjectRef: project.Ref, Services: control.ProjectServiceStates(project.Spec.Services), UpdatedAt: project.UpdatedAt})
	}
}

func updateProjectConfigHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		area := r.PathValue("area")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		previousConfig, err := store.GetProjectConfig(r.Context(), ref, area)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		var payload control.ProjectConfigInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		config, err := store.UpdateProjectConfig(r.Context(), ref, area, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{"area": config.Area}
		if config.Area == "general" {
			// General config (production-intent) is control-plane metadata only —
			// it changes how the advisor scores the project and carries no
			// container or edge state, so it skips both route reconcile and the
			// container SyncConfig below.
			control.LogProject(r.Context(), store, ref, "info", "Project config updated", metadata)
			control.Audit(r.Context(), store, "project.config_update", "project:"+ref, metadata)
			writeJSON(w, http.StatusOK, config)
			return
		}
		if config.Area == "network" {
			// Network config (exposure mode, IP allowlist, SSL) is enforced
			// entirely at the edge router via route files — it carries no
			// container state, so it reconciles routes and skips the container
			// SyncConfig below. This keeps exposure changes live and decoupled
			// from project-stack availability.
			routePath, err := reconcileProjectRoutes(r, store, ref)
			if err != nil {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			metadata["route_path"] = routePath
			control.LogProject(r.Context(), store, ref, "info", "Project config updated", metadata)
			control.Audit(r.Context(), store, "project.config_update", "project:"+ref, metadata)
			writeJSON(w, http.StatusOK, config)
			return
		}
		if syncer, ok := provisioner.(control.ConfigSyncer); ok {
			runtimeConfig, err := materializeProjectConfigForRuntime(r.Context(), store, ref, config)
			if err != nil {
				rollbackProjectConfig(r.Context(), store, ref, previousConfig)
				control.LogProject(r.Context(), store, ref, "error", "Project config secret resolution failed", map[string]string{"area": config.Area, "error": err.Error()})
				control.Audit(r.Context(), store, "project.config_secret_resolution_failed", "project:"+ref, map[string]string{"area": config.Area, "error": err.Error()})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			if err := syncer.SyncConfig(r.Context(), ref, runtimeConfig); err != nil {
				rollbackProjectConfig(r.Context(), store, ref, previousConfig)
				control.LogProject(r.Context(), store, ref, "error", "Project config sync failed", map[string]string{"area": config.Area, "error": err.Error()})
				control.Audit(r.Context(), store, "project.config_sync_failed", "project:"+ref, map[string]string{"area": config.Area, "error": err.Error()})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			metadata["runtime_synced"] = "true"
		}
		// Optional per-project hardening: when the database config opts in, enforce
		// RLS on platform-created internal tables. Best-effort — never fails the save.
		if config.Area == "database" {
			if err := applyProjectSystemRLS(r.Context(), store, project); err != nil {
				control.LogProject(r.Context(), store, ref, "warning", "System-table RLS enforcement skipped", map[string]string{"error": err.Error()})
			}
		}
		control.LogProject(r.Context(), store, ref, "info", "Project config updated", map[string]string{"area": config.Area})
		control.Audit(r.Context(), store, "project.config_update", "project:"+ref, metadata)
		writeJSON(w, http.StatusOK, config)
	}
}

func rollbackProjectConfig(ctx context.Context, store control.Store, ref string, previous control.ProjectConfig) {
	_, _ = store.UpdateProjectConfig(ctx, ref, previous.Area, control.ProjectConfigInput{Config: previous.Config})
}

func materializeProjectConfigForRuntime(ctx context.Context, store control.Store, ref string, config control.ProjectConfig) (control.ProjectConfig, error) {
	runtimeConfig := config
	runtimeConfig.Config = cloneRuntimeConfigMap(config.Config)
	for _, key := range runtimeSecretHandleKeys(config.Area) {
		handle := strings.TrimSpace(config.Config[key])
		if handle == "" {
			continue
		}
		value, err := resolveProjectSecretHandleValue(ctx, store, ref, handle, key)
		if err != nil {
			return control.ProjectConfig{}, err
		}
		runtimeConfig.Config[runtimeResolvedSecretKey(key)] = value
	}
	return runtimeConfig, nil
}

func runtimeSecretHandleKeys(area string) []string {
	switch area {
	case "auth":
		return []string{"captcha_secret_handle"}
	case "auth_providers":
		keys := []string{
			"oauth_google_client_secret_handle",
			"oauth_github_client_secret_handle",
			"oauth_azure_client_secret_handle",
			"sms_test_otp_handle",
			"sms_twilio_auth_token_handle",
			"sms_messagebird_access_key_handle",
			"sms_textlocal_api_key_handle",
			"sms_vonage_api_secret_handle",
		}
		for _, provider := range []string{
			"apple",
			"bitbucket",
			"discord",
			"facebook",
			"figma",
			"gitlab",
			"kakao",
			"keycloak",
			"linkedin_oidc",
			"notion",
			"snapchat",
			"slack_oidc",
			"spotify",
			"twitch",
			"twitter",
			"workos",
			"zoom",
		} {
			keys = append(keys, "oauth_"+provider+"_client_secret_handle")
		}
		sort.Strings(keys)
		return keys
	case "smtp":
		return []string{"password_handle"}
	default:
		return nil
	}
}

func runtimeResolvedSecretKey(handleKey string) string {
	return "__resolved_" + handleKey
}

func cloneRuntimeConfigMap(input map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		out[key] = value
	}
	return out
}
