package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"supadupa2026/internal/control"
)

type topTable struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Rows      int64  `json:"rows"`
}

type bucketStat struct {
	Name    string `json:"name"`
	Public  bool   `json:"public"`
	Objects int64  `json:"objects"`
	Bytes   int64  `json:"bytes"`
}

type connectionStats struct {
	Total     int64 `json:"total"`
	Active    int64 `json:"active"`
	Idle      int64 `json:"idle"`
	IdleInTxn int64 `json:"idle_in_txn"`
	Max       int64 `json:"max"`
}

// projectStatsResponse is the live, queried-on-demand stats for a project.
type projectStatsResponse struct {
	Available bool `json:"available"`
	// Database
	DBSizeBytes         int64           `json:"db_size_bytes"`
	TableCount          int64           `json:"table_count"`
	Connections         connectionStats `json:"connections"`
	CacheHitRatio       float64         `json:"cache_hit_ratio"`
	TxnsPerSec          float64         `json:"txns_per_sec"`
	TuplesWrittenPerSec float64         `json:"tuples_written_per_sec"`
	Deadlocks           int64           `json:"deadlocks"`
	TempBytes           int64           `json:"temp_bytes"`
	TopTables           []topTable      `json:"top_tables"`
	// Storage
	Buckets         int64        `json:"buckets"`
	Objects         int64        `json:"objects"`
	StorageBytes    int64        `json:"storage_bytes"`
	BucketBreakdown []bucketStat `json:"bucket_breakdown"`
}

// rawStats mirrors the json_build_object the probe returns.
type rawStats struct {
	DBSizeBytes  int64        `json:"db_size_bytes"`
	TableCount   int64        `json:"table_count"`
	ConnTotal    int64        `json:"conn_total"`
	ConnActive   int64        `json:"conn_active"`
	ConnIdle     int64        `json:"conn_idle"`
	ConnIdleTxn  int64        `json:"conn_idle_in_txn"`
	ConnMax      int64        `json:"conn_max"`
	XactCommit   int64        `json:"xact_commit"`
	XactRollback int64        `json:"xact_rollback"`
	BlksHit      int64        `json:"blks_hit"`
	BlksRead     int64        `json:"blks_read"`
	TupInserted  int64        `json:"tup_inserted"`
	TupUpdated   int64        `json:"tup_updated"`
	TupDeleted   int64        `json:"tup_deleted"`
	Deadlocks    int64        `json:"deadlocks"`
	TempBytes    int64        `json:"temp_bytes"`
	TopTables    []topTable   `json:"top_tables"`
	StorageObjs  int64        `json:"storage_objects"`
	StorageBytes int64        `json:"storage_bytes"`
	BucketCount  int64        `json:"bucket_count"`
	Buckets      []bucketStat `json:"buckets"`
}

const projectStatsSQL = `select json_build_object(
 'db_size_bytes', pg_database_size(current_database()),
 'table_count', (select count(*) from information_schema.tables where table_schema='public' and table_type='BASE TABLE'),
 'conn_total', (select count(*) from pg_stat_activity where datname=current_database()),
 'conn_active', (select count(*) from pg_stat_activity where datname=current_database() and state='active'),
 'conn_idle', (select count(*) from pg_stat_activity where datname=current_database() and state='idle'),
 'conn_idle_in_txn', (select count(*) from pg_stat_activity where datname=current_database() and state='idle in transaction'),
 'conn_max', (select setting::int from pg_settings where name='max_connections'),
 'xact_commit', coalesce((select xact_commit from pg_stat_database where datname=current_database()),0),
 'xact_rollback', coalesce((select xact_rollback from pg_stat_database where datname=current_database()),0),
 'blks_hit', coalesce((select blks_hit from pg_stat_database where datname=current_database()),0),
 'blks_read', coalesce((select blks_read from pg_stat_database where datname=current_database()),0),
 'tup_inserted', coalesce((select tup_inserted from pg_stat_database where datname=current_database()),0),
 'tup_updated', coalesce((select tup_updated from pg_stat_database where datname=current_database()),0),
 'tup_deleted', coalesce((select tup_deleted from pg_stat_database where datname=current_database()),0),
 'deadlocks', coalesce((select deadlocks from pg_stat_database where datname=current_database()),0),
 'temp_bytes', coalesce((select temp_bytes from pg_stat_database where datname=current_database()),0),
 'top_tables', coalesce((select json_agg(t) from (select relname as name, pg_total_relation_size(relid) as size_bytes, n_live_tup as rows from pg_stat_user_tables order by pg_total_relation_size(relid) desc limit 6) t),'[]'::json),
 'storage_objects', case when to_regclass('storage.objects') is null then 0 else (select count(*) from storage.objects) end,
 'storage_bytes', case when to_regclass('storage.objects') is null then 0 else coalesce((select sum((metadata->>'size')::bigint) from storage.objects),0) end,
 'bucket_count', case when to_regclass('storage.buckets') is null then 0 else (select count(*) from storage.buckets) end,
 'buckets', case when to_regclass('storage.buckets') is null then '[]'::json else coalesce((select json_agg(b) from (select bk.name, bk.public, (select count(*) from storage.objects o where o.bucket_id=bk.id) as objects, coalesce((select sum((o.metadata->>'size')::bigint) from storage.objects o where o.bucket_id=bk.id),0) as bytes from storage.buckets bk order by bk.name) b),'[]'::json) end
);`

// dbCounterSample caches cumulative counters per project so successive /stats
// calls (the overview polls every ~30s) can derive txn/tuple rates without a
// separate background collector.
type dbCounterSample struct {
	at     time.Time
	xact   int64
	tuples int64
}

var dbStatsCache = struct {
	mu sync.Mutex
	m  map[string]dbCounterSample
}{m: map[string]dbCounterSample{}}

func getProjectStatsHandler(store control.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		project, ok := requireProjectRole(w, r, store, r.PathValue("ref"), roleViewer)
		if !ok {
			return
		}
		resp := projectStatsResponse{TopTables: []topTable{}, BucketBreakdown: []bucketStat{}}
		if databaseRuntimeApplyEnabled() {
			if out, err := queryProjectDatabaseSQL(r.Context(), project, projectStatsSQL); err == nil {
				var raw rawStats
				if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(out)), &raw); jsonErr == nil {
					resp.Available = true
					resp.DBSizeBytes = raw.DBSizeBytes
					resp.TableCount = raw.TableCount
					resp.Connections = connectionStats{Total: raw.ConnTotal, Active: raw.ConnActive, Idle: raw.ConnIdle, IdleInTxn: raw.ConnIdleTxn, Max: raw.ConnMax}
					if blks := raw.BlksHit + raw.BlksRead; blks > 0 {
						resp.CacheHitRatio = float64(raw.BlksHit) / float64(blks)
					}
					resp.Deadlocks = raw.Deadlocks
					resp.TempBytes = raw.TempBytes
					if raw.TopTables != nil {
						resp.TopTables = raw.TopTables
					}
					resp.Buckets = raw.BucketCount
					resp.Objects = raw.StorageObjs
					resp.StorageBytes = raw.StorageBytes
					if raw.Buckets != nil {
						resp.BucketBreakdown = raw.Buckets
					}
					// Rates from the previous sample for this project.
					now := time.Now()
					xact := raw.XactCommit + raw.XactRollback
					tuples := raw.TupInserted + raw.TupUpdated + raw.TupDeleted
					dbStatsCache.mu.Lock()
					if prev, ok := dbStatsCache.m[project.Ref]; ok {
						if dt := now.Sub(prev.at).Seconds(); dt > 0 {
							if d := xact - prev.xact; d > 0 {
								resp.TxnsPerSec = float64(d) / dt
							}
							if d := tuples - prev.tuples; d > 0 {
								resp.TuplesWrittenPerSec = float64(d) / dt
							}
						}
					}
					dbStatsCache.m[project.Ref] = dbCounterSample{at: now, xact: xact, tuples: tuples}
					dbStatsCache.mu.Unlock()
				}
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
