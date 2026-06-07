ALTER TABLE wal_archives
    ADD COLUMN segment_source TEXT NOT NULL DEFAULT 'unknown';
