package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"supadupa2026/internal/control"
)

func sanitizeProjectForResponse(project control.Project) control.Project {
	project.Spec.Environment = nil
	return project
}

func sanitizeProjectsForResponse(projects []control.Project) []control.Project {
	sanitized := make([]control.Project, len(projects))
	for index, project := range projects {
		sanitized[index] = sanitizeProjectForResponse(project)
	}
	return sanitized
}

type accessRole int

const (
	roleViewer accessRole = iota + 1
	roleDeveloper
	roleAdmin
	roleOwner
)

func roleName(role accessRole) string {
	switch role {
	case roleViewer:
		return "viewer"
	case roleDeveloper:
		return "developer"
	case roleAdmin:
		return "admin"
	case roleOwner:
		return "owner"
	default:
		return "unknown"
	}
}

func roleRank(role string) accessRole {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner":
		return roleOwner
	case "admin":
		return roleAdmin
	case "developer":
		return roleDeveloper
	case "viewer":
		return roleViewer
	default:
		return 0
	}
}

func countEnabledFlags(flags map[string]bool) int {
	count := 0
	for _, enabled := range flags {
		if enabled {
			count++
		}
	}
	return count
}

func requireOrgFeature(w http.ResponseWriter, r *http.Request, store control.Store, orgID string, flag string) bool {
	flags, err := store.GetOrgFeatureFlags(r.Context(), orgID)
	if err != nil {
		writeStoreError(w, err)
		return false
	}
	if flags.Effective[flag] {
		return true
	}
	writeError(w, http.StatusForbidden, "feature flag "+flag+" is disabled for org")
	return false
}

func requireProjectFeature(w http.ResponseWriter, r *http.Request, store control.Store, project control.Project, flag string) bool {
	return requireOrgFeature(w, r, store, project.OrgID, flag)
}

func requirePlatformAdmin(w http.ResponseWriter, r *http.Request) bool {
	claims, ok := claimsFromRequest(r)
	if !ok {
		return true
	}
	if strings.EqualFold(claims.Role, "admin") {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden: platform admin role required")
	return false
}

func requireSCIMOrPlatformAdmin(w http.ResponseWriter, r *http.Request, store control.Store, auth *control.AuthService) bool {
	if claims, ok := claimsFromRequest(r); ok && strings.EqualFold(claims.Role, "admin") {
		return true
	}
	token := tokenFromRequest(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return false
	}
	if token != "" && auth != nil {
		if claims, err := auth.Verify(token); err == nil && strings.EqualFold(claims.Role, "admin") {
			return true
		}
	}
	bearer := bearerTokenFromRequest(r)
	if bearer == "" {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return false
	}
	config, err := store.GetPlatformSSOConfig(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return false
	}
	if control.VerifyPlatformSCIMToken(config, bearer) {
		if control.PlatformSCIMTokenNeedsRehash(config, bearer) {
			_, _ = store.UpdatePlatformSSOConfig(r.Context(), control.PlatformSSOConfigInput{
				Enabled:       config.Enabled,
				IDPEntityID:   config.IDPEntityID,
				SSOURL:        config.SSOURL,
				Certificate:   config.Certificate,
				ACSURL:        config.ACSURL,
				MetadataURL:   config.MetadataURL,
				EmailDomain:   config.EmailDomain,
				AutoProvision: config.AutoProvision,
				DefaultRole:   config.DefaultRole,
				SCIMEnabled:   config.SCIMEnabled,
				SCIMToken:     bearer,
			})
		}
		return true
	}
	if config.SCIMEnabled && config.SCIMTokenConfigured {
		writeError(w, http.StatusUnauthorized, "invalid SCIM bearer token")
		return false
	}
	writeError(w, http.StatusForbidden, "SCIM provisioning token is not configured")
	return false
}

func requireOrgRole(w http.ResponseWriter, r *http.Request, store control.Store, orgID string, minimum accessRole) bool {
	claims, ok := claimsFromRequest(r)
	if !ok {
		return true
	}
	role, err := orgRoleForEmail(r.Context(), store, orgID, claims.Email)
	if err != nil {
		writeStoreError(w, err)
		return false
	}
	if roleRank(role) >= minimum {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden: requires "+roleName(minimum)+" access to org")
	return false
}

func requireProjectRole(w http.ResponseWriter, r *http.Request, store control.Store, ref string, minimum accessRole) (control.Project, bool) {
	project, err := store.GetProject(r.Context(), ref)
	if err != nil {
		writeStoreError(w, err)
		return control.Project{}, false
	}
	claims, ok := claimsFromRequest(r)
	if !ok {
		return project, true
	}
	orgRole, err := orgRoleForEmail(r.Context(), store, project.OrgID, claims.Email)
	if err != nil {
		writeStoreError(w, err)
		return control.Project{}, false
	}
	if roleRank(orgRole) >= minimum {
		return project, true
	}
	projectRole, err := store.ResolveProjectRole(r.Context(), project.Ref, claims.Email)
	if err == nil && roleRank(projectRole) >= minimum {
		return project, true
	}
	if err != nil && !errors.Is(err, control.ErrNotFound) {
		writeStoreError(w, err)
		return control.Project{}, false
	}
	writeError(w, http.StatusForbidden, "forbidden: requires "+roleName(minimum)+" access to project")
	return control.Project{}, false
}

func projectsVisibleToRequest(r *http.Request, store control.Store) ([]control.Project, error) {
	projects, err := store.ListProjects(r.Context())
	if err != nil {
		return nil, err
	}
	claims, ok := claimsFromRequest(r)
	if !ok || strings.EqualFold(claims.Role, "admin") {
		return projects, nil
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	orgs, err := store.ListOrgs(r.Context())
	if err != nil {
		return nil, err
	}
	visibleOrgs := map[string]struct{}{}
	for _, org := range orgs {
		role, err := orgRoleForEmail(r.Context(), store, org.ID, email)
		if err != nil {
			return nil, err
		}
		if roleRank(role) >= roleViewer {
			visibleOrgs[org.ID] = struct{}{}
		}
	}
	visible := make([]control.Project, 0, len(projects))
	for _, project := range projects {
		if _, ok := visibleOrgs[project.OrgID]; ok {
			visible = append(visible, project)
			continue
		}
		role, err := store.ResolveProjectRole(r.Context(), project.Ref, email)
		if err != nil {
			if errors.Is(err, control.ErrNotFound) {
				continue
			}
			return nil, err
		}
		if roleRank(role) >= roleViewer {
			visible = append(visible, project)
		}
	}
	return visible, nil
}

func orgRoleForEmail(ctx context.Context, store control.Store, orgID string, email string) (string, error) {
	members, err := store.ListOrgMembers(ctx, orgID)
	if err != nil {
		return "", err
	}
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	for _, member := range members {
		if member.Email == normalizedEmail {
			return member.Role, nil
		}
	}
	return "", nil
}

func claimsFromRequest(r *http.Request) (control.TokenClaims, bool) {
	claims, ok := r.Context().Value(tokenClaimsKey).(control.TokenClaims)
	return claims, ok
}

func userIDFromRequest(r *http.Request) (string, bool) {
	claims, ok := claimsFromRequest(r)
	if !ok || claims.Subject == "" {
		return "", false
	}
	return claims.Subject, true
}
