ALTER TABLE platform_defaults
    ADD COLUMN IF NOT EXISTS smtp_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS smtp_host TEXT,
    ADD COLUMN IF NOT EXISTS smtp_port INTEGER NOT NULL DEFAULT 587,
    ADD COLUMN IF NOT EXISTS smtp_sender_name TEXT,
    ADD COLUMN IF NOT EXISTS smtp_sender_email TEXT,
    ADD COLUMN IF NOT EXISTS smtp_username TEXT,
    ADD COLUMN IF NOT EXISTS smtp_password_handle TEXT,
    ADD COLUMN IF NOT EXISTS smtp_tls_mode TEXT NOT NULL DEFAULT 'starttls';

ALTER TABLE platform_defaults
    DROP CONSTRAINT IF EXISTS platform_defaults_profile_check;

ALTER TABLE platform_defaults
    ADD CONSTRAINT platform_defaults_profile_check
    CHECK (profile IN ('essential', 'full', 'orioledb'));

ALTER TABLE platform_defaults
    ADD CONSTRAINT platform_defaults_smtp_port_check
    CHECK (smtp_port BETWEEN 1 AND 65535);

ALTER TABLE platform_defaults
    ADD CONSTRAINT platform_defaults_smtp_tls_mode_check
    CHECK (smtp_tls_mode IN ('starttls', 'implicit', 'none'));

ALTER TABLE platform_defaults
    ADD CONSTRAINT platform_defaults_smtp_password_handle_check
    CHECK (smtp_password_handle IS NULL OR smtp_password_handle = '' OR smtp_password_handle LIKE 'secret://%');
