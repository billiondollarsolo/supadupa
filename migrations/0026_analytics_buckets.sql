CREATE TABLE analytics_buckets (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    storage_uri TEXT NOT NULL,
    catalog_uri TEXT,
    warehouse TEXT NOT NULL,
    credential_handle TEXT,
    format_version INTEGER NOT NULL DEFAULT 2,
    partitioning TEXT,
    retention_days INTEGER NOT NULL DEFAULT 0,
    compaction_schedule TEXT NOT NULL DEFAULT 'manual',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name),
    CHECK (format_version IN (1, 2)),
    CHECK (retention_days BETWEEN 0 AND 3650)
);

CREATE INDEX analytics_buckets_project_id_idx ON analytics_buckets(project_id);
CREATE INDEX analytics_buckets_status_idx ON analytics_buckets(status);
