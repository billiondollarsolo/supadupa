CREATE TABLE auth_hooks (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    hook_type TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    target_uri TEXT,
    edge_function TEXT,
    secret_handle TEXT,
    headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    timeout_ms INTEGER NOT NULL,
    retry_attempts INTEGER NOT NULL,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE(project_id, hook_type)
);

CREATE INDEX auth_hooks_project_id_idx ON auth_hooks(project_id);
CREATE INDEX auth_hooks_status_idx ON auth_hooks(status);
