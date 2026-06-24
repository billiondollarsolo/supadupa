ALTER TABLE platform_defaults
    DROP CONSTRAINT IF EXISTS platform_defaults_resource_tier_check;

ALTER TABLE platform_defaults
    ADD CONSTRAINT platform_defaults_resource_tier_check
    CHECK (resource_tier IN ('small', 'medium', 'large', 'custom'));
