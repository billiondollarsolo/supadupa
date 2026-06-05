CREATE TABLE network_connections (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    provider TEXT NOT NULL,
    region TEXT,
    cidrs TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    endpoint_id TEXT,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name),
    CHECK (type IN ('privatelink', 'vpc_peering', 'private_endpoint', 'wireguard', 'operator_network')),
    CHECK (provider IN ('aws', 'gcp', 'azure', 'custom', 'operator'))
);

CREATE INDEX network_connections_project_id_idx ON network_connections(project_id);
CREATE INDEX network_connections_status_idx ON network_connections(status);
CREATE INDEX network_connections_provider_idx ON network_connections(provider);
