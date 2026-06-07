ALTER TABLE backups
    ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS remote_location TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS storage_target_id TEXT NOT NULL DEFAULT '';

UPDATE backups
SET started_at = created_at
WHERE started_at IS NULL;

UPDATE backups
SET finished_at = COALESCE(verified_at, created_at)
WHERE finished_at IS NULL
  AND status = 'completed';

ALTER TABLE backups
    ALTER COLUMN started_at SET NOT NULL;

CREATE INDEX IF NOT EXISTS backups_storage_target_id_idx ON backups(storage_target_id);
