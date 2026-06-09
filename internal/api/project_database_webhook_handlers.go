package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"supadupa2026/internal/control"
)

func listProjectDatabaseWebhooksHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		webhooks, err := store.ListProjectDatabaseWebhooks(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskDatabaseWebhooks(webhooks))
	}
}

func createProjectDatabaseWebhookHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		var payload control.ProjectDatabaseWebhookInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		webhook, err := store.CreateProjectDatabaseWebhook(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectDatabaseWebhookCreate(r.Context(), store, project, webhook); err != nil {
			_ = store.DeleteProjectDatabaseWebhook(r.Context(), ref, webhook.Name)
			metadata := map[string]string{"name": webhook.Name, "table": webhook.Schema + "." + webhook.Table, "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database webhook apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_webhook_create_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":   webhook.Name,
			"table":  webhook.Schema + "." + webhook.Table,
			"active": fmt.Sprintf("%t", webhook.Active),
		}
		control.LogProject(r.Context(), store, ref, "info", "Database webhook configured", metadata)
		control.Audit(r.Context(), store, "project.database_webhook_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskDatabaseWebhook(webhook))
	}
}

func deleteProjectDatabaseWebhookHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		name := r.PathValue("name")
		webhook, ok, err := getCurrentProjectDatabaseWebhook(r.Context(), store, ref, name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !ok {
			if err := store.DeleteProjectDatabaseWebhook(r.Context(), ref, name); err != nil {
				writeStoreError(w, err)
			}
			return
		}
		if err := applyProjectDatabaseWebhookDelete(r.Context(), project, webhook); err != nil {
			metadata := map[string]string{"name": webhook.Name, "table": webhook.Schema + "." + webhook.Table, "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database webhook delete apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_webhook_delete_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProjectDatabaseWebhook(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Database webhook deleted", map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		control.Audit(r.Context(), store, "project.database_webhook_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func getCurrentProjectDatabaseWebhook(ctx context.Context, store control.Store, ref string, name string) (control.ProjectDatabaseWebhook, bool, error) {
	webhooks, err := store.ListProjectDatabaseWebhooks(ctx, ref)
	if err != nil {
		return control.ProjectDatabaseWebhook{}, false, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, webhook := range webhooks {
		if webhook.Name == normalized {
			return webhook, true, nil
		}
	}
	return control.ProjectDatabaseWebhook{}, false, nil
}

func applyProjectDatabaseWebhookCreate(ctx context.Context, store control.Store, project control.Project, webhook control.ProjectDatabaseWebhook) error {
	if !databaseRuntimeApplyEnabled() || !webhook.Active {
		return nil
	}
	if webhook.HTTPMethod != "POST" {
		return fmt.Errorf("active database webhooks currently require http_method POST for pg_net delivery")
	}
	headers, err := databaseWebhookRuntimeHeaders(ctx, store, project.Ref, webhook)
	if err != nil {
		return err
	}
	headersJSON, err := json.Marshal(headers)
	if err != nil {
		return err
	}
	functionName := databaseWebhookFunctionName(webhook)
	sql := "CREATE SCHEMA IF NOT EXISTS supadupa;\n" +
		"CREATE EXTENSION IF NOT EXISTS pg_net;\n" +
		"CREATE OR REPLACE FUNCTION supadupa." + quoteDatabaseIdentifier(functionName) + "()\n" +
		"RETURNS trigger\n" +
		"LANGUAGE plpgsql\n" +
		"SECURITY DEFINER\n" +
		"SET search_path = public, net, pg_temp\n" +
		"AS $function$\n" +
		"DECLARE\n" +
		"  request_payload jsonb;\n" +
		"  request_headers jsonb := " + quoteDatabaseLiteral(string(headersJSON)) + "::jsonb;\n" +
		"BEGIN\n" +
		"  request_payload := jsonb_build_object(\n" +
		"    'type', TG_OP,\n" +
		"    'table', TG_TABLE_NAME,\n" +
		"    'schema', TG_TABLE_SCHEMA,\n" +
		"    'record', CASE WHEN TG_OP = 'DELETE' THEN NULL ELSE to_jsonb(NEW) END,\n" +
		"    'old_record', CASE WHEN TG_OP IN ('UPDATE', 'DELETE') THEN to_jsonb(OLD) ELSE NULL END\n" +
		"  );\n" +
		"  PERFORM net.http_post(\n" +
		"    url := " + quoteDatabaseLiteral(webhook.Endpoint) + ",\n" +
		"    body := request_payload,\n" +
		"    headers := request_headers,\n" +
		"    timeout_milliseconds := " + strconv.Itoa(webhook.TimeoutSeconds*1000) + "\n" +
		"  );\n" +
		"  RETURN COALESCE(NEW, OLD);\n" +
		"END;\n" +
		"$function$;\n"
	for _, event := range webhook.Events {
		sql += "DROP TRIGGER IF EXISTS " + quoteDatabaseIdentifier(databaseWebhookTriggerName(webhook, event)) + " ON " + quoteDatabaseIdentifier(webhook.Schema) + "." + quoteDatabaseIdentifier(webhook.Table) + ";\n" +
			"CREATE TRIGGER " + quoteDatabaseIdentifier(databaseWebhookTriggerName(webhook, event)) + "\n" +
			"AFTER " + strings.ToUpper(event) + " ON " + quoteDatabaseIdentifier(webhook.Schema) + "." + quoteDatabaseIdentifier(webhook.Table) + "\n" +
			"FOR EACH ROW EXECUTE FUNCTION supadupa." + quoteDatabaseIdentifier(functionName) + "();\n"
	}
	return execProjectDatabaseSQL(ctx, project, sql)
}

func applyProjectDatabaseWebhookDelete(ctx context.Context, project control.Project, webhook control.ProjectDatabaseWebhook) error {
	if !databaseRuntimeApplyEnabled() || !webhook.Active {
		return nil
	}
	tableName := quoteDatabaseIdentifier(webhook.Schema) + "." + quoteDatabaseIdentifier(webhook.Table)
	sql := ""
	for _, event := range webhook.Events {
		sql += "DO $drop_webhook$\n" +
			"BEGIN\n" +
			"  IF to_regclass(" + quoteDatabaseLiteral(webhook.Schema+"."+webhook.Table) + ") IS NOT NULL THEN\n" +
			"    EXECUTE " + quoteDatabaseLiteral("DROP TRIGGER IF EXISTS "+quoteDatabaseIdentifier(databaseWebhookTriggerName(webhook, event))+" ON "+tableName) + ";\n" +
			"  END IF;\n" +
			"END\n" +
			"$drop_webhook$;\n"
	}
	sql += "DROP FUNCTION IF EXISTS supadupa." + quoteDatabaseIdentifier(databaseWebhookFunctionName(webhook)) + "();\n"
	return execProjectDatabaseSQL(ctx, project, sql)
}

func databaseWebhookRuntimeHeaders(ctx context.Context, store control.Store, ref string, webhook control.ProjectDatabaseWebhook) (map[string]string, error) {
	headers := map[string]string{"Content-Type": "application/json"}
	for key, value := range webhook.Headers {
		trimmed := strings.TrimSpace(value)
		if isSensitiveMetadataKey(key) && trimmed != "" {
			resolved, err := resolveProjectSecretHandleValue(ctx, store, ref, trimmed, "webhook header "+key)
			if err != nil {
				return nil, err
			}
			headers[key] = resolved
			continue
		}
		headers[key] = trimmed
	}
	return headers, nil
}

func databaseWebhookFunctionName(webhook control.ProjectDatabaseWebhook) string {
	return "webhook_" + strings.ReplaceAll(webhook.Name, "-", "_")
}

func databaseWebhookTriggerName(webhook control.ProjectDatabaseWebhook, event string) string {
	return "supadupa_webhook_" + strings.ReplaceAll(webhook.Name, "-", "_") + "_" + strings.ToLower(event)
}

func maskDatabaseWebhooks(webhooks []control.ProjectDatabaseWebhook) []control.ProjectDatabaseWebhook {
	out := make([]control.ProjectDatabaseWebhook, len(webhooks))
	copy(out, webhooks)
	for index := range out {
		out[index] = maskDatabaseWebhook(out[index])
	}
	return out
}

func maskDatabaseWebhook(webhook control.ProjectDatabaseWebhook) control.ProjectDatabaseWebhook {
	webhook.Headers = maskSensitiveStringMap(webhook.Headers, isSensitiveMetadataKey)
	webhook.Metadata = maskSensitiveStringMap(webhook.Metadata, isSensitiveMetadataKey)
	return webhook
}
