package api

import (
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
