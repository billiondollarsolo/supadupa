CREATE TABLE replication_pipelines (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    source_schema TEXT NOT NULL,
    source_table TEXT NOT NULL,
    destination TEXT NOT NULL,
    destination_uri TEXT NOT NULL DEFAULT '',
    credential_handle TEXT,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name),
    CHECK (type IN ('logical', 'etl', 'analytics_bucket')),
    CHECK (destination IN ('postgres', 'webhook', 's3', 'iceberg', 'bigquery', 'snowflake', 'redshift'))
);

CREATE INDEX replication_pipelines_project_id_idx ON replication_pipelines(project_id);
CREATE INDEX replication_pipelines_destination_idx ON replication_pipelines(destination);
