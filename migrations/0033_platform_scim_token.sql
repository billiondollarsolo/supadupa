ALTER TABLE platform_sso
    ADD COLUMN IF NOT EXISTS scim_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS scim_token_hash TEXT;
