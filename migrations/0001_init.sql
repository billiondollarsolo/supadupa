CREATE TABLE orgs (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'admin',
    mfa_enabled BOOLEAN NOT NULL DEFAULT false,
    mfa_secret TEXT,
    mfa_pending_secret TEXT,
    mfa_confirmed_at TIMESTAMPTZ,
    mfa_updated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE memberships (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, org_id)
);

CREATE TABLE hosts (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    address TEXT NOT NULL,
    capacity JSONB NOT NULL DEFAULT '{}'::jsonb,
    used JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE projects (
    id UUID PRIMARY KEY,
    ref TEXT NOT NULL UNIQUE,
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    host_id UUID REFERENCES hosts(id),
    status TEXT NOT NULL,
    stack_version TEXT NOT NULL,
    profile TEXT NOT NULL,
    resource_tier TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (status IN ('provisioning', 'healthy', 'degraded', 'paused', 'error', 'destroying'))
);

CREATE INDEX projects_org_id_idx ON projects(org_id);
CREATE INDEX projects_status_idx ON projects(status);

CREATE TABLE project_specs (
    project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    desired_state JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_configs (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    area TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, area)
);

CREATE TABLE edge_functions (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    version INTEGER NOT NULL,
    entrypoint TEXT NOT NULL,
    verify_jwt BOOLEAN NOT NULL DEFAULT true,
    status TEXT NOT NULL,
    source_hash TEXT NOT NULL,
    source_bytes INTEGER NOT NULL DEFAULT 0,
    secrets JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE INDEX edge_functions_project_id_idx ON edge_functions(project_id);

CREATE TABLE secrets (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    encrypted_value BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at TIMESTAMPTZ,
    UNIQUE (project_id, kind, created_at)
);

CREATE INDEX secrets_project_id_idx ON secrets(project_id);
CREATE UNIQUE INDEX secrets_project_id_kind_idx ON secrets(project_id, kind);

CREATE TABLE backups (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    location TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    verified_at TIMESTAMPTZ
);

CREATE INDEX backups_project_id_created_at_idx ON backups(project_id, created_at DESC);

CREATE TABLE backup_policies (
    project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT true,
    schedule TEXT NOT NULL DEFAULT 'daily',
    kind TEXT NOT NULL DEFAULT 'logical',
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_logs (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    level TEXT NOT NULL,
    message TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX project_logs_project_id_created_at_idx ON project_logs(project_id, created_at DESC);

CREATE TABLE domains (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    fqdn TEXT NOT NULL,
    cert_status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, fqdn)
);

CREATE TABLE log_drains (
    id UUID PRIMARY KEY,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    target TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX log_drains_project_id_idx ON log_drains(project_id);

CREATE TABLE usage_snapshots (
    id UUID PRIMARY KEY,
    org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    metrics JSONB NOT NULL DEFAULT '{}'::jsonb,
    sampled_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX usage_snapshots_org_id_sampled_at_idx ON usage_snapshots(org_id, sampled_at DESC);

CREATE TABLE audit_events (
    id UUID PRIMARY KEY,
    actor_id UUID REFERENCES users(id),
    chain_index BIGINT NOT NULL,
    previous_hash TEXT NOT NULL DEFAULT '',
    hash TEXT NOT NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX audit_events_chain_index_idx ON audit_events(chain_index);
CREATE INDEX audit_events_created_at_idx ON audit_events(created_at DESC);
CREATE INDEX audit_events_target_idx ON audit_events(target);
