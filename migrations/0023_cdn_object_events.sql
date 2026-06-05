ALTER TABLE cdn_invalidations
    ADD COLUMN source TEXT NOT NULL DEFAULT 'manual',
    ADD COLUMN event_id TEXT;

CREATE INDEX cdn_invalidations_source_idx ON cdn_invalidations(source);
CREATE INDEX cdn_invalidations_event_id_idx ON cdn_invalidations(event_id);
