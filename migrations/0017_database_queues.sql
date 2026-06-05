CREATE TABLE database_queues (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    schema TEXT NOT NULL,
    retention_minutes INTEGER NOT NULL,
    visibility_timeout_seconds INTEGER NOT NULL,
    max_retries INTEGER NOT NULL,
    dead_letter_queue TEXT,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE(project_id, name)
);

CREATE INDEX database_queues_project_id_idx ON database_queues(project_id);
CREATE INDEX database_queues_status_idx ON database_queues(status);
