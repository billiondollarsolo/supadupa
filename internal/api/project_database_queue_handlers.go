package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"supadupa2026/internal/control"
)

func listProjectDatabaseQueuesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		queues, err := store.ListProjectDatabaseQueues(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskDatabaseQueues(queues))
	}
}

func createProjectDatabaseQueueHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		var payload control.ProjectDatabaseQueueInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		queue, err := store.CreateProjectDatabaseQueue(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectDatabaseQueueCreate(r.Context(), project, queue); err != nil {
			logRollbackError(r.Context(), "delete project database queue after apply failure", store.DeleteProjectDatabaseQueue(r.Context(), ref, queue.Name))
			metadata := map[string]string{"name": queue.Name, "schema": queue.Schema, "active": fmt.Sprintf("%t", queue.Active), "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database queue apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_queue_create_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":   queue.Name,
			"schema": queue.Schema,
			"active": fmt.Sprintf("%t", queue.Active),
		}
		control.LogProject(r.Context(), store, ref, "info", "Database queue configured", metadata)
		control.Audit(r.Context(), store, "project.database_queue_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskDatabaseQueue(queue))
	}
}

func deleteProjectDatabaseQueueHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		name := r.PathValue("name")
		queue, ok, err := getCurrentProjectDatabaseQueue(r.Context(), store, ref, name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !ok {
			if err := store.DeleteProjectDatabaseQueue(r.Context(), ref, name); err != nil {
				writeStoreError(w, err)
			}
			return
		}
		if err := applyProjectDatabaseQueueDelete(r.Context(), project, queue); err != nil {
			metadata := map[string]string{"name": queue.Name, "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database queue delete apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_queue_delete_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProjectDatabaseQueue(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Database queue deleted", map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		control.Audit(r.Context(), store, "project.database_queue_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func getCurrentProjectDatabaseQueue(ctx context.Context, store control.Store, ref string, name string) (control.ProjectDatabaseQueue, bool, error) {
	queues, err := store.ListProjectDatabaseQueues(ctx, ref)
	if err != nil {
		return control.ProjectDatabaseQueue{}, false, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, queue := range queues {
		if queue.Name == normalized {
			return queue, true, nil
		}
	}
	return control.ProjectDatabaseQueue{}, false, nil
}

func applyProjectDatabaseQueueCreate(ctx context.Context, project control.Project, queue control.ProjectDatabaseQueue) error {
	if !databaseRuntimeApplyEnabled() || !queue.Active {
		return nil
	}
	sql := "CREATE SCHEMA IF NOT EXISTS " + quoteDatabaseIdentifier(queue.Schema) + ";\n" +
		"CREATE EXTENSION IF NOT EXISTS pgmq WITH SCHEMA " + quoteDatabaseIdentifier(queue.Schema) + ";\n"
	if strings.TrimSpace(queue.DeadLetterQueue) != "" {
		sql += "SELECT pgmq.create(" + quoteDatabaseLiteral(queue.DeadLetterQueue) + ");\n"
	}
	sql += "SELECT pgmq.create(" + quoteDatabaseLiteral(queue.Name) + ");\n"
	return execProjectDatabaseSQL(ctx, project, sql)
}

func applyProjectDatabaseQueueDelete(ctx context.Context, project control.Project, queue control.ProjectDatabaseQueue) error {
	if !databaseRuntimeApplyEnabled() || !queue.Active {
		return nil
	}
	sql := "SELECT pgmq.drop_queue(" + quoteDatabaseLiteral(queue.Name) + ");\n"
	if strings.TrimSpace(queue.DeadLetterQueue) != "" {
		sql += "SELECT pgmq.drop_queue(" + quoteDatabaseLiteral(queue.DeadLetterQueue) + ");\n"
	}
	return execProjectDatabaseSQL(ctx, project, sql)
}

func maskDatabaseQueues(queues []control.ProjectDatabaseQueue) []control.ProjectDatabaseQueue {
	out := make([]control.ProjectDatabaseQueue, len(queues))
	copy(out, queues)
	for index := range out {
		out[index] = maskDatabaseQueue(out[index])
	}
	return out
}

func maskDatabaseQueue(queue control.ProjectDatabaseQueue) control.ProjectDatabaseQueue {
	queue.Metadata = maskSensitiveStringMap(queue.Metadata, isSensitiveMetadataKey)
	return queue
}
