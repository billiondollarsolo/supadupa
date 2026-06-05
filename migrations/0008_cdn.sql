ALTER TABLE project_routes
    ADD COLUMN cache_control TEXT,
    ADD COLUMN smart_cdn BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE cdn_policies (
    project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    browser_ttl_seconds INTEGER NOT NULL DEFAULT 3600,
    edge_ttl_seconds INTEGER NOT NULL DEFAULT 3600,
    stale_while_revalidate_seconds INTEGER NOT NULL DEFAULT 60,
    included_paths TEXT[] NOT NULL DEFAULT ARRAY['/storage/v1/object/public/*'],
    excluded_paths TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    smart_revalidation BOOLEAN NOT NULL DEFAULT FALSE,
    cache_control TEXT NOT NULL DEFAULT 'public, max-age=3600, s-maxage=3600, stale-while-revalidate=60',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (browser_ttl_seconds BETWEEN 0 AND 31536000),
    CHECK (edge_ttl_seconds BETWEEN 0 AND 31536000),
    CHECK (stale_while_revalidate_seconds BETWEEN 0 AND 31536000)
);

CREATE TABLE cdn_invalidations (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    paths TEXT[] NOT NULL,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX cdn_invalidations_project_id_idx ON cdn_invalidations(project_id);
CREATE INDEX cdn_invalidations_status_idx ON cdn_invalidations(status);
