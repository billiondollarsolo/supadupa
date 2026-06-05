CREATE TABLE platform_defaults (
    id TEXT PRIMARY KEY,
    domain TEXT NOT NULL DEFAULT 'supadupa.test',
    stack_version TEXT NOT NULL DEFAULT 'latest',
    profile TEXT NOT NULL DEFAULT 'full',
    resource_tier TEXT NOT NULL DEFAULT 'small',
    backup_schedule TEXT NOT NULL DEFAULT 'daily',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (profile IN ('essential', 'full')),
    CHECK (resource_tier IN ('small', 'medium', 'large')),
    CHECK (backup_schedule IN ('daily', 'hourly'))
);

INSERT INTO platform_defaults (id)
VALUES ('default')
ON CONFLICT (id) DO NOTHING;
