package api

import (
	"net/http"

	"supadupa2026/internal/control"
)

func listAuditEventsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := store.ListAuditEvents(r.Context(), 100)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, events)
	}
}

func getAuditIntegrityHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		integrity, err := store.VerifyAuditLog(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, integrity)
	}
}

func createHostHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		var payload control.CreateHostRequest
		if err := decodeJSON(r, &payload); err != nil {
			writeDecodeError(w, err)
			return
		}
		host, err := store.CreateHost(r.Context(), payload)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "host.create", "host:"+host.ID, map[string]string{"name": host.Name, "address": host.Address})
		writeJSON(w, http.StatusCreated, host)
	}
}

func listHostsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hosts, err := store.ListHosts(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, hosts)
	}
}

func getHostHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, err := store.GetHost(r.Context(), r.PathValue("id"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, host)
	}
}

func deleteHostHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r) {
			return
		}
		id := r.PathValue("id")
		if err := store.DeleteHost(r.Context(), id); err != nil {
			writeStoreError(w, err)
			return
		}
		control.Audit(r.Context(), store, "host.delete", "host:"+id, nil)
		w.WriteHeader(http.StatusNoContent)
	}
}
