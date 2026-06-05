ALTER TABLE org_quotas
    ADD COLUMN IF NOT EXISTS max_disk_iops INTEGER NOT NULL DEFAULT 0,
    ADD CONSTRAINT org_quotas_max_disk_iops_check CHECK (max_disk_iops >= 0);
