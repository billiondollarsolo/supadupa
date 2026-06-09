ALTER TABLE backup_policies
    ADD COLUMN IF NOT EXISTS storage_target_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS backup_policies_storage_target_id_idx ON backup_policies(storage_target_id);
