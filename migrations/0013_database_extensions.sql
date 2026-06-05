CREATE TABLE database_extensions (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    schema TEXT NOT NULL,
    version TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE(project_id, name)
);

CREATE INDEX database_extensions_project_id_idx ON database_extensions(project_id);
CREATE INDEX database_extensions_status_idx ON database_extensions(status);
