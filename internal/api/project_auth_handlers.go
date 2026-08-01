package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"supadupa2026/internal/control"
)

func listProjectAuthClientsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		clients, err := store.ListProjectAuthClients(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskAuthClients(clients))
	}
}

func createProjectAuthClientHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		var payload control.ProjectAuthClientInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		client, err := store.CreateProjectAuthClient(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{
			"client_id":    client.ClientID,
			"name":         client.Name,
			"confidential": fmt.Sprintf("%t", client.Confidential),
		}
		control.LogProject(r.Context(), store, ref, "info", "Auth client registered", metadata)
		control.Audit(r.Context(), store, "project.auth_client_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskAuthClient(client))
	}
}

func deleteProjectAuthClientHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		clientID := r.PathValue("client_id")
		if err := store.DeleteProjectAuthClient(r.Context(), ref, clientID); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Auth client deleted", map[string]string{"client_id": clientID})
		control.Audit(r.Context(), store, "project.auth_client_delete", "project:"+ref, map[string]string{"client_id": clientID})
		w.WriteHeader(http.StatusNoContent)
	}
}

func maskAuthClients(clients []control.ProjectAuthClient) []control.ProjectAuthClient {
	out := make([]control.ProjectAuthClient, len(clients))
	copy(out, clients)
	for index := range out {
		out[index] = maskAuthClient(out[index])
	}
	return out
}

func maskAuthClient(client control.ProjectAuthClient) control.ProjectAuthClient {
	if strings.TrimSpace(client.ClientSecretHandle) != "" {
		client.ClientSecretHandle = maskedSensitiveValue
	}
	return client
}

func listProjectAuthHooksHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		hooks, err := store.ListProjectAuthHooks(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskAuthHooks(hooks))
	}
}

func createProjectAuthHookHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		previousHooks, err := store.ListProjectAuthHooks(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		var payload control.ProjectAuthHookInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		hook, err := store.CreateProjectAuthHook(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{
			"hook_type": hook.HookType,
			"enabled":   fmt.Sprintf("%t", hook.Enabled),
			"target":    authHookTargetForMetadata(hook),
		}
		if syncer, ok := provisioner.(control.AuthHookSyncer); ok {
			if err := syncProjectAuthHooks(r, store, syncer, ref); err != nil {
				restoreProjectAuthHooks(r.Context(), store, ref, previousHooks)
				control.LogProject(r.Context(), store, ref, "error", "Auth hooks sync failed", map[string]string{"hook_type": hook.HookType, "error": err.Error()})
				control.Audit(r.Context(), store, "project.auth_hooks_sync_failed", "project:"+ref, map[string]string{"hook_type": hook.HookType, "error": err.Error()})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			metadata["runtime_synced"] = "true"
		}
		control.LogProject(r.Context(), store, ref, "info", "Auth hook configured", metadata)
		control.Audit(r.Context(), store, "project.auth_hook_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskAuthHook(hook))
	}
}

func deleteProjectAuthHookHandler(store control.Store, provisioner control.Provisioner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		previousHooks, err := store.ListProjectAuthHooks(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		hookType := r.PathValue("hook_type")
		if err := store.DeleteProjectAuthHook(r.Context(), ref, hookType); err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{"hook_type": hookType}
		if syncer, ok := provisioner.(control.AuthHookSyncer); ok {
			if err := syncProjectAuthHooks(r, store, syncer, ref); err != nil {
				restoreProjectAuthHooks(r.Context(), store, ref, previousHooks)
				control.LogProject(r.Context(), store, ref, "error", "Auth hooks sync failed", map[string]string{"hook_type": hookType, "error": err.Error()})
				control.Audit(r.Context(), store, "project.auth_hooks_sync_failed", "project:"+ref, map[string]string{"hook_type": hookType, "error": err.Error()})
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			metadata["runtime_synced"] = "true"
		}
		control.LogProject(r.Context(), store, ref, "warning", "Auth hook deleted", metadata)
		control.Audit(r.Context(), store, "project.auth_hook_delete", "project:"+ref, metadata)
		w.WriteHeader(http.StatusNoContent)
	}
}

func syncProjectAuthHooks(r *http.Request, store control.Store, syncer control.AuthHookSyncer, ref string) error {
	hooks, err := store.ListProjectAuthHooks(r.Context(), ref)
	if err != nil {
		return err
	}
	runtimeHooks, err := materializeProjectAuthHooksForRuntime(r.Context(), store, ref, hooks)
	if err != nil {
		return err
	}
	pctx, cancel := detachedProvisionContext(r)
	defer cancel()
	return syncer.SyncAuthHooks(pctx, ref, runtimeHooks)
}

func materializeProjectAuthHooksForRuntime(ctx context.Context, store control.Store, ref string, hooks []control.ProjectAuthHook) ([]control.ProjectAuthHook, error) {
	out := append([]control.ProjectAuthHook(nil), hooks...)
	for index := range out {
		out[index].Headers = cloneRuntimeConfigMap(out[index].Headers)
		out[index].RuntimeHeaders = cloneRuntimeConfigMap(out[index].Headers)
		if !out[index].Enabled {
			continue
		}
		if handle := strings.TrimSpace(out[index].SecretHandle); handle != "" {
			resolved, err := resolveProjectSecretHandleValue(ctx, store, ref, handle, "auth hook secret_handle")
			if err != nil {
				return nil, fmt.Errorf("auth hook secret_handle: %w", err)
			}
			out[index].RuntimeSecret = resolved
		}
		for key, value := range out[index].Headers {
			if !isSensitiveAuthHookHeaderKey(key) || strings.TrimSpace(value) == "" {
				continue
			}
			resolved, err := resolveProjectSecretHandleValue(ctx, store, ref, value, "auth hook header "+key)
			if err != nil {
				return nil, fmt.Errorf("auth hook header %s: %w", key, err)
			}
			out[index].RuntimeHeaders[key] = resolved
		}
	}
	return out, nil
}

func restoreProjectAuthHooks(ctx context.Context, store control.Store, ref string, hooks []control.ProjectAuthHook) {
	current, err := store.ListProjectAuthHooks(ctx, ref)
	if err == nil {
		for _, hook := range current {
			logRollbackError(ctx, "delete project auth hook during restore after sync failure", store.DeleteProjectAuthHook(ctx, ref, hook.HookType))
		}
	}
	for _, hook := range hooks {
		if _, err := store.CreateProjectAuthHook(ctx, ref, authHookInputFromHook(hook)); err != nil {
			logRollbackError(ctx, "recreate project auth hook during restore after sync failure", err)
		}
	}
}

func authHookInputFromHook(hook control.ProjectAuthHook) control.ProjectAuthHookInput {
	return control.ProjectAuthHookInput{
		HookType:      hook.HookType,
		Enabled:       hook.Enabled,
		TargetURI:     hook.TargetURI,
		EdgeFunction:  hook.EdgeFunction,
		SecretHandle:  hook.SecretHandle,
		Headers:       cloneRuntimeConfigMap(hook.Headers),
		TimeoutMS:     hook.TimeoutMS,
		RetryAttempts: hook.RetryAttempts,
	}
}

func authHookTargetForMetadata(hook control.ProjectAuthHook) string {
	if hook.EdgeFunction != "" {
		return "edge:" + hook.EdgeFunction
	}
	return hook.TargetURI
}

func maskAuthHooks(hooks []control.ProjectAuthHook) []control.ProjectAuthHook {
	out := make([]control.ProjectAuthHook, len(hooks))
	copy(out, hooks)
	for index := range out {
		out[index] = maskAuthHook(out[index])
	}
	return out
}

func maskAuthHook(hook control.ProjectAuthHook) control.ProjectAuthHook {
	if strings.TrimSpace(hook.SecretHandle) != "" {
		hook.SecretHandle = maskedSensitiveValue
	}
	hook.Headers = maskSensitiveStringMap(hook.Headers, isSensitiveAuthHookHeaderKey)
	return hook
}

func isSensitiveAuthHookHeaderKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "proxy-authorization", "x-api-key", "x-auth-token":
		return true
	default:
		return isSensitiveMetadataKey(key)
	}
}
