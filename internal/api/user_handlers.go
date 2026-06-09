package api

import (
	"fmt"
	"net/http"

	"supadupa2026/internal/control"
)

func createUserHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		var payload control.CreateUserRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		user, err := store.CreateUser(r.Context(), payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "user.create", "user:"+user.ID, map[string]string{"email": user.Email, "role": user.Role})
		writeJSON(w, http.StatusCreated, user)
	}
}

func updateUserHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		id := r.PathValue("id")
		current, err := store.GetUserByID(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		var payload control.UpdateUserRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		// Don't let an admin demote the last remaining admin (would lock everyone out).
		if current.Role == "admin" && payload.Role != "admin" && lastAdmin(r, store, current.ID) {
			writeError(w, http.StatusConflict, "cannot change the role of the last admin")
			return
		}
		user, err := store.UpdateUser(r.Context(), id, payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "user.update", "user:"+user.ID, map[string]string{"email": user.Email, "role": user.Role, "password_changed": fmt.Sprintf("%t", payload.Password != "")})
		writeJSON(w, http.StatusOK, user)
	}
}

func deleteUserHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		id := r.PathValue("id")
		if requesterID, ok := userIDFromRequest(r); ok && requesterID == id {
			writeError(w, http.StatusConflict, "you cannot delete your own account")
			return
		}
		target, err := store.GetUserByID(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if target.Role == "admin" && lastAdmin(r, store, id) {
			writeError(w, http.StatusConflict, "cannot delete the last admin")
			return
		}
		if err := store.DeleteUser(r.Context(), id); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "user.delete", "user:"+id, map[string]string{"email": target.Email})
		w.WriteHeader(http.StatusNoContent)
	}
}

// lastAdmin reports whether the given user is the only admin remaining.
func lastAdmin(r *http.Request, store control.Store, id string) bool {
	users, err := store.ListUsers(r.Context())
	if err != nil {
		return false
	}
	admins := 0
	for _, user := range users {
		if user.Role == "admin" {
			admins++
		}
	}
	return admins <= 1
}

func listUsersHandler(store control.Store, auth *control.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireSCIMOrPlatformAdmin(w, r, store, auth) {
			return
		}
		users, err := store.ListUsers(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, users)
	}
}
