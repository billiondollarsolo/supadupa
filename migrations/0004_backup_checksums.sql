ALTER TABLE backups
    ADD COLUMN checksum_sha256 TEXT NOT NULL DEFAULT '';

ALTER TABLE wal_archives
    ADD COLUMN checksum_sha256 TEXT NOT NULL DEFAULT '';
