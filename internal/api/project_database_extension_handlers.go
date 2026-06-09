package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"supadupa2026/internal/control"
)

func listProjectDatabaseExtensionsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		extensions, err := store.ListProjectDatabaseExtensions(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, extensions)
	}
}

func updateProjectDatabaseExtensionHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		name := r.PathValue("name")
		var payload control.ProjectDatabaseExtensionInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		previous, previousOK, err := getCurrentProjectDatabaseExtension(r.Context(), store, ref, name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		extension, err := store.UpdateProjectDatabaseExtension(r.Context(), ref, name, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectDatabaseExtensionUpdate(r.Context(), project, extension); err != nil {
			if previousOK {
				_, _ = store.UpdateProjectDatabaseExtension(r.Context(), ref, previous.Name, control.ProjectDatabaseExtensionInput{
					Schema:  previous.Schema,
					Version: previous.Version,
					Enabled: &previous.Enabled,
				})
			}
			metadata := map[string]string{"name": extension.Name, "schema": extension.Schema, "enabled": fmt.Sprintf("%t", extension.Enabled), "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database extension apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_extension_update_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":    extension.Name,
			"schema":  extension.Schema,
			"enabled": fmt.Sprintf("%t", extension.Enabled),
		}
		control.LogProject(r.Context(), store, ref, "info", "Database extension updated", metadata)
		control.Audit(r.Context(), store, "project.database_extension_update", "project:"+ref, metadata)
		writeJSON(w, http.StatusOK, extension)
	}
}

func getCurrentProjectDatabaseExtension(ctx context.Context, store control.Store, ref string, name string) (control.ProjectDatabaseExtension, bool, error) {
	extensions, err := store.ListProjectDatabaseExtensions(ctx, ref)
	if err != nil {
		return control.ProjectDatabaseExtension{}, false, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, extension := range extensions {
		if extension.Name == normalized {
			return extension, true, nil
		}
	}
	return control.ProjectDatabaseExtension{}, false, nil
}

func applyProjectDatabaseExtensionUpdate(ctx context.Context, project control.Project, extension control.ProjectDatabaseExtension) error {
	if !databaseRuntimeApplyEnabled() {
		return nil
	}
	if extension.Enabled {
		sql := "CREATE SCHEMA IF NOT EXISTS " + quoteDatabaseIdentifier(extension.Schema) + ";\n" +
			"CREATE EXTENSION IF NOT EXISTS " + quoteDatabaseIdentifier(extension.Name) + " WITH SCHEMA " + quoteDatabaseIdentifier(extension.Schema)
		if strings.TrimSpace(extension.Version) != "" {
			sql += " VERSION " + quoteDatabaseLiteral(extension.Version)
		}
		sql += ";\n"
		return execProjectDatabaseSQL(ctx, project, sql)
	}
	return execProjectDatabaseSQL(ctx, project, "DROP EXTENSION IF EXISTS "+quoteDatabaseIdentifier(extension.Name)+";\n")
}
