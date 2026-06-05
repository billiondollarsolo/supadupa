CREATE TABLE database_webhooks (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    schema TEXT NOT NULL,
    table_name TEXT NOT NULL,
    events JSONB NOT NULL DEFAULT '[]'::jsonb,
    endpoint TEXT NOT NULL,
    http_method TEXT NOT NULL,
    headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    timeout_seconds INTEGER NOT NULL,
    retry_count INTEGER NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE(project_id, name)
);

CREATE INDEX database_webhooks_project_id_idx ON database_webhooks(project_id);
CREATE INDEX database_webhooks_status_idx ON database_webhooks(status);
