package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"supadupa2026/internal/control"
)

func listProjectLogsHandler(store control.Store) http.HandlerFunc {
	stream := streamProjectLogsHandler(store)
	return func(w http.ResponseWriter, r *http.Request) {
		if wantsProjectLogStream(r) {
			stream(w, r)
			return
		}
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		logs, err := store.ListProjectLogs(r.Context(), ref, 100)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, logs)
	}
}

func wantsProjectLogStream(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("stream")), "true") {
		return true
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
}

func streamProjectLogsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming is not supported")
			return
		}
		initial, err := store.ListProjectLogs(r.Context(), ref, 100)
		if err != nil {
			writeStoreError(w, err)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		emitted := map[string]struct{}{}
		writeProjectLogEvents(w, flusher, initial, emitted)
		if strings.EqualFold(r.URL.Query().Get("follow"), "false") {
			return
		}

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				logs, err := store.ListProjectLogs(r.Context(), ref, 100)
				if err != nil {
					_, _ = fmt.Fprintf(w, "event: error\ndata: %q\n\n", err.Error())
					flusher.Flush()
					return
				}
				writeProjectLogEvents(w, flusher, logs, emitted)
			case <-heartbeat.C:
				_, _ = fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}

func writeProjectLogEvents(w http.ResponseWriter, flusher http.Flusher, logs []control.ProjectLog, emitted map[string]struct{}) {
	for i := len(logs) - 1; i >= 0; i-- {
		logEntry := logs[i]
		if _, ok := emitted[logEntry.ID]; ok {
			continue
		}
		payload, err := json.Marshal(logEntry)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(w, "event: log\ndata: %s\n\n", payload)
		emitted[logEntry.ID] = struct{}{}
	}
	flusher.Flush()
}

func listProjectActivityHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("ref")
		if _, ok := requireProjectRole(w, r, store, ref, roleViewer); !ok {
			return
		}
		events, err := store.ListProjectAuditEvents(r.Context(), ref, 100)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, events)
	}
}
