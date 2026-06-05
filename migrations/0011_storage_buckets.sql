CREATE TABLE storage_buckets (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    public BOOLEAN NOT NULL DEFAULT false,
    file_size_limit BIGINT NOT NULL DEFAULT 52428800,
    allowed_mime_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    cache_control TEXT NOT NULL DEFAULT '3600',
    avif_autodetection BOOLEAN NOT NULL DEFAULT false,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name),
    CHECK (file_size_limit >= 0)
);

CREATE INDEX storage_buckets_project_id_idx ON storage_buckets(project_id);
CREATE INDEX storage_buckets_status_idx ON storage_buckets(status);
