CREATE TABLE embedding_jobs (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    source_schema TEXT NOT NULL,
    source_table TEXT NOT NULL,
    source_column TEXT NOT NULL,
    primary_key_column TEXT NOT NULL,
    destination_table TEXT NOT NULL,
    destination_column TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    dimension INTEGER NOT NULL,
    schedule TEXT NOT NULL,
    batch_size INTEGER NOT NULL,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name),
    CHECK (provider IN ('openai', 'huggingface', 'local')),
    CHECK (dimension BETWEEN 1 AND 65535),
    CHECK (batch_size BETWEEN 1 AND 10000)
);

CREATE INDEX embedding_jobs_project_id_idx ON embedding_jobs(project_id);
CREATE INDEX embedding_jobs_status_idx ON embedding_jobs(status);

CREATE TABLE vector_buckets (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    dimension INTEGER NOT NULL,
    distance TEXT NOT NULL,
    index_method TEXT NOT NULL,
    storage_backend TEXT NOT NULL,
    storage_uri TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name),
    CHECK (dimension BETWEEN 1 AND 65535),
    CHECK (distance IN ('cosine', 'l2', 'ip')),
    CHECK (index_method IN ('none', 'hnsw', 'ivfflat')),
    CHECK (storage_backend IN ('postgres', 's3'))
);

CREATE INDEX vector_buckets_project_id_idx ON vector_buckets(project_id);
CREATE INDEX vector_buckets_status_idx ON vector_buckets(status);
