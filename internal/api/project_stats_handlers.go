package api

import (
	"net/http"
	"strconv"
	"strings"

	"supadupa2026/internal/control"
)

// projectStatsResponse carries live, queried-on-demand stats for a project:
// actual database size + table/connection counts, and storage bucket/object/
// byte usage. Distinct from /metrics (which is resource-reservation based).
type projectStatsResponse struct {
	Available    bool  `json:"available"`
	DBSizeBytes  int64 `json:"db_size_bytes"`
	TableCount   int64 `json:"table_count"`
	Connections  int64 `json:"connections"`
	Buckets      int64 `json:"buckets"`
	Objects      int64 `json:"objects"`
	StorageBytes int64 `json:"storage_bytes"`
}

// projectStatsSQL gathers DB + storage stats in a single read-only round trip.
// storage.* lookups are guarded by to_regclass so the query is safe on stacks
// without the storage schema.
const projectStatsSQL = `select pg_database_size(current_database()), (select count(*) from information_schema.tables where table_schema='public' and table_type='BASE TABLE'), (select count(*) from pg_stat_activity where datname=current_database()), case when to_regclass('storage.buckets') is null then 0 else (select count(*) from storage.buckets) end, case when to_regclass('storage.objects') is null then 0 else (select count(*) from storage.objects) end, case when to_regclass('storage.objects') is null then 0 else coalesce((select sum((metadata->>'size')::bigint) from storage.objects),0) end;`

func getProjectStatsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project, ok := requireProjectRole(w, r, store, r.PathValue("ref"), roleViewer)
		if !ok {
			return
		}
		// Available stays false (zeros) when the DB can't be probed — e.g. the
		// project is paused/stopped or apply mode is off. The UI degrades to "—".
		stats := projectStatsResponse{}
		if databaseRuntimeApplyEnabled() {
			if out, err := queryProjectDatabaseSQL(r.Context(), project, projectStatsSQL); err == nil {
				parts := strings.Split(out, "|")
				if len(parts) == 6 {
					stats.DBSizeBytes = parseStatInt(parts[0])
					stats.TableCount = parseStatInt(parts[1])
					stats.Connections = parseStatInt(parts[2])
					stats.Buckets = parseStatInt(parts[3])
					stats.Objects = parseStatInt(parts[4])
					stats.StorageBytes = parseStatInt(parts[5])
					stats.Available = true
				}
			}
		}
		writeJSON(w, http.StatusOK, stats)
	}
}

func parseStatInt(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}
