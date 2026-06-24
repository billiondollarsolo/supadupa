package api

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"supadupa2026/internal/control"
)

func scimServiceProviderConfigHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"schemas":        []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
			"patch":          map[string]bool{"supported": true},
			"bulk":           map[string]bool{"supported": false},
			"filter":         map[string]any{"supported": false},
			"changePassword": map[string]bool{"supported": false},
			"sort":           map[string]bool{"supported": false},
			"etag":           map[string]bool{"supported": false},
			"authenticationSchemes": []map[string]string{
				{"type": "oauthbearertoken", "name": "Bearer token"},
			},
		})
	}
}

func listSCIMUsersHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		users, err := store.ListUsers(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		resources := make([]scimUserResource, 0, len(users))
		for _, user := range users {
			resources = append(resources, scimUserFromControl(r, user))
		}
		writeJSON(w, http.StatusOK, scimList(resources))
	}
}

func createSCIMUserHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		var payload scimUserRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		email := scimEmailFromUserRequest(payload)
		if email == "" {
			writeError(w, http.StatusBadRequest, "SCIM userName or primary email is required")
			return
		}
		password := payload.Password
		if password == "" {
			var err error
			password, err = generatedSCIMPassword()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "SCIM password generation failed")
				return
			}
		}
		role := strings.TrimSpace(payload.Extension.Role)
		if role == "" {
			role = "member"
		}
		user, err := store.CreateUser(r.Context(), control.CreateUserRequest{Email: email, Password: password, Role: role})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if payload.Active != nil && !*payload.Active {
			if err := store.DeleteUser(r.Context(), user.ID); err != nil {
				writeStoreError(w, err)
				return
			}
			control.Audit(r.Context(), store, "scim.user_deprovision", "user:"+user.ID, map[string]string{"email": user.Email})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		control.Audit(r.Context(), store, "scim.user_create", "user:"+user.ID, map[string]string{"email": user.Email, "role": user.Role})
		writeJSON(w, http.StatusCreated, scimUserFromControl(r, user))
	}
}

func generatedSCIMPassword() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate SCIM password: %w", err)
	}
	return "scim_" + base64.RawURLEncoding.EncodeToString(data), nil
}

func getSCIMUserHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		user, err := store.GetUserByID(r.Context(), r.PathValue("id"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, scimUserFromControl(r, user))
	}
}

func replaceSCIMUserHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		var payload scimUserRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		id := r.PathValue("id")
		if payload.Active != nil && !*payload.Active {
			user, err := store.GetUserByID(r.Context(), id)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			if err := store.DeleteUser(r.Context(), id); err != nil {
				writeStoreError(w, err)
				return
			}
			control.Audit(r.Context(), store, "scim.user_deprovision", "user:"+id, map[string]string{"email": user.Email})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		email := scimEmailFromUserRequest(payload)
		if email == "" {
			writeError(w, http.StatusBadRequest, "SCIM userName or primary email is required")
			return
		}
		role := strings.TrimSpace(payload.Extension.Role)
		user, err := store.UpdateUser(r.Context(), id, control.UpdateUserRequest{Email: email, Password: payload.Password, Role: role})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "scim.user_replace", "user:"+user.ID, map[string]string{"email": user.Email, "role": user.Role})
		writeJSON(w, http.StatusOK, scimUserFromControl(r, user))
	}
}

func patchSCIMUserHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		id := r.PathValue("id")
		user, err := store.GetUserByID(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		var payload scimPatchRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		nextEmail := user.Email
		nextRole := user.Role
		changed := false
		for _, operation := range payload.Operations {
			if operation.Op != "" && !strings.EqualFold(operation.Op, "replace") {
				writeError(w, http.StatusBadRequest, "only SCIM replace operations are supported")
				return
			}
			path := strings.ToLower(strings.TrimSpace(operation.Path))
			if path == "active" && !scimBoolValue(operation.Value, true) {
				if err := store.DeleteUser(r.Context(), id); err != nil {
					writeStoreError(w, err)
					return
				}
				control.Audit(r.Context(), store, "scim.user_deprovision", "user:"+id, map[string]string{"email": user.Email})
				w.WriteHeader(http.StatusNoContent)
				return
			}
			switch path {
			case "username", "userName":
				email := strings.ToLower(strings.TrimSpace(scimStringValue(operation.Value)))
				if email == "" {
					writeError(w, http.StatusBadRequest, "SCIM userName value is required")
					return
				}
				nextEmail = email
				changed = true
			case "role", strings.ToLower(scimUserExtension + ".role"), strings.ToLower(scimUserExtension + ":role"):
				role := strings.TrimSpace(scimStringValue(operation.Value))
				if role == "" {
					writeError(w, http.StatusBadRequest, "SCIM role value is required")
					return
				}
				nextRole = role
				changed = true
			case "":
				email, role, hasChange := scimPatchObjectValues(operation.Value)
				if email != "" {
					nextEmail = email
				}
				if role != "" {
					nextRole = role
				}
				changed = changed || hasChange
			}
		}
		if changed {
			updated, err := store.UpdateUser(r.Context(), id, control.UpdateUserRequest{Email: nextEmail, Role: nextRole})
			if err != nil {
				writeStoreError(w, err)
				return
			}
			control.Audit(r.Context(), store, "scim.user_patch", "user:"+updated.ID, map[string]string{"email": updated.Email, "role": updated.Role})
			writeJSON(w, http.StatusOK, scimUserFromControl(r, updated))
			return
		}
		writeJSON(w, http.StatusOK, scimUserFromControl(r, user))
	}
}

func deleteSCIMUserHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		id := r.PathValue("id")
		user, err := store.GetUserByID(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := store.DeleteUser(r.Context(), id); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "scim.user_delete", "user:"+id, map[string]string{"email": user.Email})
		w.WriteHeader(http.StatusNoContent)
	}
}

func listSCIMGroupsHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		orgs, err := store.ListOrgs(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		requestedOrgID := strings.TrimSpace(r.URL.Query().Get("org_id"))
		resources := []scimGroupResource{}
		for _, org := range orgs {
			if requestedOrgID != "" && org.ID != requestedOrgID {
				continue
			}
			teams, err := store.ListOrgTeams(r.Context(), org.ID)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			for _, team := range teams {
				resource, err := scimGroupFromControl(r, store, team)
				if err != nil {
					writeStoreError(w, err)
					return
				}
				resources = append(resources, resource)
			}
		}
		writeJSON(w, http.StatusOK, scimList(resources))
	}
}

func createSCIMGroupHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		var payload scimGroupRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		orgID := scimGroupOrgID(r, payload)
		if orgID == "" {
			writeError(w, http.StatusBadRequest, "SCIM group externalId or extension org_id is required")
			return
		}
		team, err := store.CreateOrgTeam(r.Context(), orgID, control.TeamInput{Name: payload.DisplayName, Slug: payload.Extension.Slug})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		for _, member := range payload.Members {
			email, err := scimMemberEmail(r.Context(), store, member)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			if _, err := store.UpsertTeamMember(r.Context(), orgID, team.Slug, control.TeamMemberInput{Email: email}); err != nil {
				writeStoreError(w, err)
				return
			}
		}
		resource, err := scimGroupFromControl(r, store, team)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "scim.group_create", "org:"+orgID, map[string]string{"team": team.Slug, "name": team.Name})
		writeJSON(w, http.StatusCreated, resource)
	}
}

func getSCIMGroupHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		team, err := findSCIMTeam(r.Context(), store, r.PathValue("id"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		resource, err := scimGroupFromControl(r, store, team)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resource)
	}
}

func deleteSCIMGroupHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		team, err := findSCIMTeam(r.Context(), store, r.PathValue("id"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if err := store.DeleteOrgTeam(r.Context(), team.OrgID, team.Slug); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "scim.group_delete", "org:"+team.OrgID, map[string]string{"team": team.Slug})
		w.WriteHeader(http.StatusNoContent)
	}
}
