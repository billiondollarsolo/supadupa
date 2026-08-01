package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"supadupa2026/internal/control"
)

func listProjectDatabaseSchemasHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		schemas, err := store.ListProjectDatabaseSchemas(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskDatabaseSchemas(schemas))
	}
}

func createProjectDatabaseSchemaHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		var payload control.ProjectDatabaseSchemaInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		schema, err := store.CreateProjectDatabaseSchema(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectDatabaseSchemaCreate(r.Context(), project, schema); err != nil {
			logRollbackError(r.Context(), "delete project database schema after apply failure", store.DeleteProjectDatabaseSchema(r.Context(), ref, schema.Name, schema.Version))
			metadata := map[string]string{"name": schema.Name, "version": schema.Version, "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Declarative schema apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_schema_create_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":     schema.Name,
			"version":  schema.Version,
			"schema":   schema.Schema,
			"checksum": schema.Checksum,
			"active":   fmt.Sprintf("%t", schema.Active),
		}
		control.LogProject(r.Context(), store, ref, "info", "Declarative schema recorded", metadata)
		control.Audit(r.Context(), store, "project.database_schema_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskDatabaseSchema(schema))
	}
}

func applyProjectDatabaseSchemaCreate(ctx context.Context, project control.Project, schema control.ProjectDatabaseSchema) error {
	if !databaseRuntimeApplyEnabled() || !schema.Active {
		return nil
	}
	return execProjectDatabaseSQL(ctx, project, schema.SQL)
}

func deleteProjectDatabaseSchemaHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleAdmin); !ok {
			return
		}
		name := r.PathValue("name")
		version := r.PathValue("version")
		if err := store.DeleteProjectDatabaseSchema(r.Context(), ref, name, version); err != nil {
			writeStoreError(w, err)
			return
		}
		metadata := map[string]string{"name": strings.ToLower(strings.TrimSpace(name)), "version": strings.TrimSpace(version)}
		control.LogProject(r.Context(), store, ref, "warning", "Declarative schema deleted", metadata)
		control.Audit(r.Context(), store, "project.database_schema_delete", "project:"+ref, metadata)
		w.WriteHeader(http.StatusNoContent)
	}
}

func maskDatabaseSchemas(schemas []control.ProjectDatabaseSchema) []control.ProjectDatabaseSchema {
	out := make([]control.ProjectDatabaseSchema, len(schemas))
	copy(out, schemas)
	for index := range out {
		out[index] = maskDatabaseSchema(out[index])
	}
	return out
}

func maskDatabaseSchema(schema control.ProjectDatabaseSchema) control.ProjectDatabaseSchema {
	schema.Metadata = maskSensitiveStringMap(schema.Metadata, isSensitiveMetadataKey)
	return schema
}
