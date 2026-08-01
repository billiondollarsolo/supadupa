package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"supadupa2026/internal/control"
)

func listProjectDatabaseRolesHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		roles, err := store.ListProjectDatabaseRoles(r.Context(), ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, maskDatabaseRoles(roles))
	}
}

func createProjectDatabaseRoleHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		var payload control.ProjectDatabaseRoleInput
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		role, err := store.CreateProjectDatabaseRole(r.Context(), ref, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := applyProjectDatabaseRoleCreate(r.Context(), store, project, role); err != nil {
			logRollbackError(r.Context(), "delete project database role after apply failure", store.DeleteProjectDatabaseRole(r.Context(), ref, role.Name))
			metadata := map[string]string{"name": role.Name, "login": fmt.Sprintf("%t", role.Login), "bypass_rls": fmt.Sprintf("%t", role.BypassRLS), "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database role apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_role_create_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		metadata := map[string]string{
			"name":       role.Name,
			"login":      fmt.Sprintf("%t", role.Login),
			"bypass_rls": fmt.Sprintf("%t", role.BypassRLS),
		}
		control.LogProject(r.Context(), store, ref, "info", "Database role configured", metadata)
		control.Audit(r.Context(), store, "project.database_role_create", "project:"+ref, metadata)
		writeJSON(w, http.StatusCreated, maskDatabaseRole(role))
	}
}

func deleteProjectDatabaseRoleHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		project, ok := requireProjectRole(w, r, store, ref, roleAdmin)
		if !ok {
			return
		}
		name := r.PathValue("name")
		role, ok, err := getCurrentProjectDatabaseRole(r.Context(), store, ref, name)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !ok {
			if err := store.DeleteProjectDatabaseRole(r.Context(), ref, name); err != nil {
				writeStoreError(w, err)
			}
			return
		}
		if err := applyProjectDatabaseRoleDelete(r.Context(), project, role); err != nil {
			metadata := map[string]string{"name": role.Name, "error": err.Error()}
			control.LogProject(r.Context(), store, ref, "error", "Database role delete apply failed", metadata)
			control.Audit(r.Context(), store, "project.database_role_delete_failed", "project:"+ref, metadata)
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err := store.DeleteProjectDatabaseRole(r.Context(), ref, name); err != nil {
			writeStoreError(w, err)
			return
		}
		control.LogProject(r.Context(), store, ref, "warning", "Database role deleted", map[string]string{"name": name})
		control.Audit(r.Context(), store, "project.database_role_delete", "project:"+ref, map[string]string{"name": strings.ToLower(strings.TrimSpace(name))})
		w.WriteHeader(http.StatusNoContent)
	}
}

func getCurrentProjectDatabaseRole(ctx context.Context, store control.Store, ref string, name string) (control.ProjectDatabaseRole, bool, error) {
	roles, err := store.ListProjectDatabaseRoles(ctx, ref)
	if err != nil {
		return control.ProjectDatabaseRole{}, false, err
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, role := range roles {
		if role.Name == normalized {
			return role, true, nil
		}
	}
	return control.ProjectDatabaseRole{}, false, nil
}

func applyProjectDatabaseRoleCreate(ctx context.Context, store control.Store, project control.Project, role control.ProjectDatabaseRole) error {
	if !databaseRuntimeApplyEnabled() {
		return nil
	}
	password := ""
	if role.Login {
		resolved, err := resolveProjectSecretHandle(ctx, store, project.Ref, role.PasswordSecretHandle)
		if err != nil {
			return err
		}
		password = resolved
	}
	sql := "DO $$ BEGIN\n" +
		"IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = " + quoteDatabaseLiteral(role.Name) + ") THEN\n" +
		"CREATE ROLE " + quoteDatabaseIdentifier(role.Name) + ";\n" +
		"END IF;\n" +
		"END $$;\n" +
		"ALTER ROLE " + quoteDatabaseIdentifier(role.Name)
	if role.Login {
		sql += " LOGIN PASSWORD " + quoteDatabaseLiteral(password)
	} else {
		sql += " NOLOGIN"
	}
	if role.Inherit {
		sql += " INHERIT"
	} else {
		sql += " NOINHERIT"
	}
	if role.BypassRLS {
		sql += " BYPASSRLS"
	} else {
		sql += " NOBYPASSRLS"
	}
	if role.ConnectionLimit != 0 {
		sql += " CONNECTION LIMIT " + strconv.Itoa(role.ConnectionLimit)
	}
	sql += ";\n"
	for _, member := range role.MemberOf {
		sql += "GRANT " + quoteDatabaseIdentifier(member) + " TO " + quoteDatabaseIdentifier(role.Name) + ";\n"
	}
	for schema, grants := range role.SchemaGrants {
		schemaPrivileges, tablePrivileges := databaseRoleGrantPrivileges(grants)
		if len(schemaPrivileges) > 0 {
			sql += "GRANT " + strings.Join(schemaPrivileges, ", ") + " ON SCHEMA " + quoteDatabaseIdentifier(schema) + " TO " + quoteDatabaseIdentifier(role.Name) + ";\n"
		}
		if len(tablePrivileges) > 0 {
			sql += "GRANT " + strings.Join(tablePrivileges, ", ") + " ON ALL TABLES IN SCHEMA " + quoteDatabaseIdentifier(schema) + " TO " + quoteDatabaseIdentifier(role.Name) + ";\n"
		}
	}
	return execProjectDatabaseSQL(ctx, project, sql)
}

func applyProjectDatabaseRoleDelete(ctx context.Context, project control.Project, role control.ProjectDatabaseRole) error {
	if !databaseRuntimeApplyEnabled() {
		return nil
	}
	sql := ""
	for schema, grants := range role.SchemaGrants {
		schemaPrivileges, tablePrivileges := databaseRoleGrantPrivileges(grants)
		if len(tablePrivileges) > 0 {
			sql += "REVOKE " + strings.Join(tablePrivileges, ", ") + " ON ALL TABLES IN SCHEMA " + quoteDatabaseIdentifier(schema) + " FROM " + quoteDatabaseIdentifier(role.Name) + ";\n"
		}
		if len(schemaPrivileges) > 0 {
			sql += "REVOKE " + strings.Join(schemaPrivileges, ", ") + " ON SCHEMA " + quoteDatabaseIdentifier(schema) + " FROM " + quoteDatabaseIdentifier(role.Name) + ";\n"
		}
	}
	for _, member := range role.MemberOf {
		sql += "REVOKE " + quoteDatabaseIdentifier(member) + " FROM " + quoteDatabaseIdentifier(role.Name) + ";\n"
	}
	sql += "DROP ROLE IF EXISTS " + quoteDatabaseIdentifier(role.Name) + ";\n"
	return execProjectDatabaseSQL(ctx, project, sql)
}

func databaseRoleGrantPrivileges(grants string) ([]string, []string) {
	schemaPrivileges := []string{}
	tablePrivileges := []string{}
	for _, grant := range strings.Split(grants, ",") {
		switch strings.ToLower(strings.TrimSpace(grant)) {
		case "usage":
			schemaPrivileges = append(schemaPrivileges, "USAGE")
		case "create":
			schemaPrivileges = append(schemaPrivileges, "CREATE")
		case "select":
			tablePrivileges = append(tablePrivileges, "SELECT")
		case "insert":
			tablePrivileges = append(tablePrivileges, "INSERT")
		case "update":
			tablePrivileges = append(tablePrivileges, "UPDATE")
		case "delete":
			tablePrivileges = append(tablePrivileges, "DELETE")
		case "all":
			schemaPrivileges = append(schemaPrivileges, "ALL PRIVILEGES")
			tablePrivileges = append(tablePrivileges, "ALL PRIVILEGES")
		}
	}
	return schemaPrivileges, tablePrivileges
}

func resolveProjectSecretHandle(ctx context.Context, store control.Store, ref string, handle string) (string, error) {
	return resolveProjectSecretHandleValue(ctx, store, ref, handle, "password_secret_handle")
}

func resolveProjectSecretHandleValue(ctx context.Context, store control.Store, ref string, handle string, label string) (string, error) {
	prefix := "secret://projects/" + ref + "/"
	if !strings.HasPrefix(handle, prefix) {
		return "", fmt.Errorf("%s must reference project %s", label, ref)
	}
	kind := strings.TrimSpace(strings.TrimPrefix(handle, prefix))
	if strings.Contains(kind, "/") || kind == "" {
		return "", fmt.Errorf("%s %s is not revealable by this control plane", label, handle)
	}
	secret, err := store.RevealProjectSecret(ctx, ref, kind)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(secret.Value) == "" {
		return "", fmt.Errorf("%s %s has no value", label, handle)
	}
	return secret.Value, nil
}

func maskDatabaseRoles(roles []control.ProjectDatabaseRole) []control.ProjectDatabaseRole {
	out := make([]control.ProjectDatabaseRole, len(roles))
	copy(out, roles)
	for index := range out {
		out[index] = maskDatabaseRole(out[index])
	}
	return out
}

func maskDatabaseRole(role control.ProjectDatabaseRole) control.ProjectDatabaseRole {
	if strings.TrimSpace(role.PasswordSecretHandle) != "" {
		role.PasswordSecretHandle = maskedSensitiveValue
	}
	role.Metadata = maskSensitiveStringMap(role.Metadata, isSensitiveMetadataKey)
	return role
}
