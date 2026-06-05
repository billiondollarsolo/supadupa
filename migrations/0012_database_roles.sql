CREATE TABLE database_roles (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    login BOOLEAN NOT NULL DEFAULT FALSE,
    inherit BOOLEAN NOT NULL DEFAULT TRUE,
    bypass_rls BOOLEAN NOT NULL DEFAULT FALSE,
    connection_limit INTEGER NOT NULL DEFAULT 0,
    password_secret_handle TEXT,
    member_of JSONB NOT NULL DEFAULT '[]'::jsonb,
    schema_grants JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE(project_id, name),
    CHECK (connection_limit >= -1)
);

CREATE INDEX database_roles_project_id_idx ON database_roles(project_id);
CREATE INDEX database_roles_status_idx ON database_roles(status);
