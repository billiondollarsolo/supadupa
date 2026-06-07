ALTER TABLE org_quotas
    ADD COLUMN IF NOT EXISTS max_disk_iops INTEGER NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'org_quotas_max_disk_iops_check'
    ) THEN
        ALTER TABLE org_quotas
            ADD CONSTRAINT org_quotas_max_disk_iops_check CHECK (max_disk_iops >= 0);
    END IF;
END $$;
