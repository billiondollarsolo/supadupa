CREATE TABLE org_quotas (
    org_id UUID PRIMARY KEY REFERENCES orgs(id) ON DELETE CASCADE,
    max_projects INTEGER NOT NULL DEFAULT 0,
    max_cpu INTEGER NOT NULL DEFAULT 0,
    max_ram_mb INTEGER NOT NULL DEFAULT 0,
    max_disk_gb INTEGER NOT NULL DEFAULT 0,
    max_disk_iops INTEGER NOT NULL DEFAULT 0,
    used JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (max_projects >= 0),
    CHECK (max_cpu >= 0),
    CHECK (max_ram_mb >= 0),
    CHECK (max_disk_gb >= 0),
    CHECK (max_disk_iops >= 0)
);

CREATE TABLE project_routes (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    fqdn TEXT NOT NULL,
    upstream_url TEXT NOT NULL,
    tls BOOLEAN NOT NULL DEFAULT true,
    ssl_enforced BOOLEAN NOT NULL DEFAULT true,
    ip_allowlist TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE INDEX project_routes_project_id_idx ON project_routes(project_id);
CREATE INDEX project_routes_fqdn_idx ON project_routes(fqdn);

CREATE TABLE project_branches (
    id UUID PRIMARY KEY,
    source_project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    branch_project_id UUID NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ
);

CREATE INDEX project_branches_source_project_id_idx ON project_branches(source_project_id);
CREATE INDEX project_branches_expires_at_idx ON project_branches(expires_at);

CREATE TABLE project_replicas (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    host_id UUID REFERENCES hosts(id),
    region TEXT,
    resource_tier TEXT NOT NULL,
    status TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'read',
    message TEXT,
    read_uri TEXT NOT NULL,
    read_weight INTEGER NOT NULL DEFAULT 100,
    failover_priority INTEGER NOT NULL DEFAULT 1,
    replication_lag_bytes BIGINT NOT NULL DEFAULT 0,
    replication_lag_seconds INTEGER NOT NULL DEFAULT 0,
    promoted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, name),
    CHECK (role IN ('read', 'primary')),
    CHECK (read_weight >= 0),
    CHECK (failover_priority > 0),
    CHECK (replication_lag_bytes >= 0),
    CHECK (replication_lag_seconds >= 0)
);

CREATE INDEX project_replicas_project_id_idx ON project_replicas(project_id);
CREATE INDEX project_replicas_host_id_idx ON project_replicas(host_id);
CREATE INDEX project_replicas_status_idx ON project_replicas(status);

ALTER TABLE backups
    ADD COLUMN status TEXT NOT NULL DEFAULT 'completed';

CREATE TABLE pitr_policies (
    project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT false,
    archive_bucket TEXT NOT NULL DEFAULT '',
    retention_days INTEGER NOT NULL DEFAULT 7,
    last_archive_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (retention_days BETWEEN 1 AND 35),
    CHECK ((enabled = false) OR (archive_bucket <> ''))
);

CREATE TABLE wal_archives (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    segment TEXT NOT NULL,
    location TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    verified_at TIMESTAMPTZ,
    UNIQUE (project_id, segment)
);

CREATE INDEX wal_archives_project_id_created_at_idx ON wal_archives(project_id, created_at DESC);
CREATE INDEX wal_archives_status_idx ON wal_archives(status);
