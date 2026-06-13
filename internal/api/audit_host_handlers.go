package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"supadupa2026/internal/control"
)

// listAuditEventsHandler returns a server-side filtered, paginated page of audit
// events: ?limit&offset&action&actor&since&until (since/until accept RFC3339 or
// a YYYY-MM-DD date). Filtering/paging happen over the full chain in the store,
// so search reaches all history rather than just a recent client-side window.
func listAuditEventsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
		q := r.URL.Query()
		query := control.AuditEventQuery{
			Limit:   atoiDefault(q.Get("limit"), 100),
			Offset:  atoiDefault(q.Get("offset"), 0),
			Action:  strings.TrimSpace(q.Get("action")),
			ActorID: strings.TrimSpace(q.Get("actor")),
			Since:   parseAuditTime(q.Get("since"), false),
			Until:   parseAuditTime(q.Get("until"), true),
		}
		page, err := store.ListAuditEventsPage(r.Context(), query)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	}
}

func atoiDefault(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	if parsed, err := strconv.Atoi(raw); err == nil {
		return parsed
	}
	return fallback
}

// parseAuditTime accepts RFC3339 or a bare YYYY-MM-DD; for a date-only "until"
// it rolls to the end of that day so the range is inclusive.
func parseAuditTime(raw string, endOfDay bool) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		if endOfDay {
			return parsed.Add(24*time.Hour - time.Nanosecond)
		}
		return parsed
	}
	return time.Time{}
}

func getAuditIntegrityHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePlatformAdmin(w, r, store) {
			return
		}
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
		if !requirePlatformAdmin(w, r, store) {
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
		if !requirePlatformAdmin(w, r, store) {
			return
		}
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
		if !requirePlatformAdmin(w, r, store) {
			return
		}
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
		if !requirePlatformAdmin(w, r, store) {
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
