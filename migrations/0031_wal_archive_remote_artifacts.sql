ALTER TABLE wal_archives
    ADD COLUMN remote_location TEXT NOT NULL DEFAULT '',
    ADD COLUMN storage_target_id TEXT NOT NULL DEFAULT '';

CREATE INDEX wal_archives_storage_target_id_idx ON wal_archives(storage_target_id);
