ALTER TABLE platform_defaults
    ADD COLUMN IF NOT EXISTS database_ingress_allowed_cidrs TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];
