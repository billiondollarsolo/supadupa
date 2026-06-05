CREATE TABLE database_cron_jobs (
    id TEXT PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    schedule TEXT NOT NULL,
    command TEXT NOT NULL,
    database_name TEXT NOT NULL,
    username TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    timeout_seconds INTEGER NOT NULL,
    max_runtime_seconds INTEGER NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL,
    message TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE(project_id, name)
);

CREATE INDEX database_cron_jobs_project_id_idx ON database_cron_jobs(project_id);
CREATE INDEX database_cron_jobs_status_idx ON database_cron_jobs(status);
