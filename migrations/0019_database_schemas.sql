CREATE TABLE database_schemas (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    schema_name TEXT NOT NULL,
    sql TEXT NOT NULL,
    checksum TEXT NOT NULL,
    apply_order INTEGER NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE(project_id, name, version)
);

CREATE INDEX database_schemas_project_id_idx ON database_schemas(project_id);
CREATE INDEX database_schemas_status_idx ON database_schemas(status);
CREATE INDEX database_schemas_apply_order_idx ON database_schemas(project_id, apply_order);
