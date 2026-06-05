CREATE TABLE platform_sso (
    id TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT false,
    provider TEXT NOT NULL DEFAULT 'saml',
    idp_entity_id TEXT NOT NULL DEFAULT '',
    sso_url TEXT NOT NULL DEFAULT '',
    certificate_pem TEXT,
    acs_url TEXT NOT NULL DEFAULT '',
    metadata_url TEXT,
    email_domain TEXT,
    auto_provision BOOLEAN NOT NULL DEFAULT false,
    default_role TEXT NOT NULL DEFAULT 'developer',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (provider IN ('saml')),
    CHECK (default_role IN ('admin', 'developer', 'viewer'))
);

INSERT INTO platform_sso (id)
VALUES ('default')
ON CONFLICT (id) DO NOTHING;
