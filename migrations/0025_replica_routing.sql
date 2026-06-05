ALTER TABLE project_replicas
    ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'read',
    ADD COLUMN IF NOT EXISTS read_weight INTEGER NOT NULL DEFAULT 100,
    ADD COLUMN IF NOT EXISTS failover_priority INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS replication_lag_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS replication_lag_seconds INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS promoted_at TIMESTAMPTZ;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'project_replicas_role_check'
    ) THEN
        ALTER TABLE project_replicas
            ADD CONSTRAINT project_replicas_role_check CHECK (role IN ('read', 'primary'));
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'project_replicas_read_weight_check'
    ) THEN
        ALTER TABLE project_replicas
            ADD CONSTRAINT project_replicas_read_weight_check CHECK (read_weight >= 0);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'project_replicas_failover_priority_check'
    ) THEN
        ALTER TABLE project_replicas
            ADD CONSTRAINT project_replicas_failover_priority_check CHECK (failover_priority > 0);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'project_replicas_replication_lag_bytes_check'
    ) THEN
        ALTER TABLE project_replicas
            ADD CONSTRAINT project_replicas_replication_lag_bytes_check CHECK (replication_lag_bytes >= 0);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'project_replicas_replication_lag_seconds_check'
    ) THEN
        ALTER TABLE project_replicas
            ADD CONSTRAINT project_replicas_replication_lag_seconds_check CHECK (replication_lag_seconds >= 0);
    END IF;
END $$;
