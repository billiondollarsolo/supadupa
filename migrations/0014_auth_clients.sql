CREATE TABLE auth_clients (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    client_id TEXT NOT NULL,
    client_secret_handle TEXT,
    redirect_uris JSONB NOT NULL DEFAULT '[]'::jsonb,
    grant_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    confidential BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE(project_id, client_id)
);

CREATE INDEX auth_clients_project_id_idx ON auth_clients(project_id);
CREATE INDEX auth_clients_status_idx ON auth_clients(status);
