-- Native model catalog and presentation resources are versioned separately
-- from the semantic ThingModel aggregate.
CREATE TABLE thing_model_resources (
    tenant_id UUID NOT NULL,
    thing_model_id UUID NOT NULL REFERENCES thing_models(id) ON DELETE CASCADE,
    revision BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0),
    profile JSONB NOT NULL DEFAULT '{"author":"","tags":[],"cover":"","version":"","brand":"","modelNumber":""}'::jsonb,
    web_chart_config JSONB,
    app_chart_config JSONB,
    updated_by UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, thing_model_id)
);

ALTER TABLE thing_model_snapshots
    ADD COLUMN resources_content JSONB,
    ADD COLUMN resources_revision BIGINT,
    ADD COLUMN resources_checksum CHAR(64),
    ADD CONSTRAINT thing_model_snapshots_native_resources_complete
        CHECK (
            resources_content IS NULL AND resources_revision IS NULL AND resources_checksum IS NULL
            OR
            resources_content IS NOT NULL
            AND resources_revision IS NOT NULL AND resources_revision >= 0
            AND resources_checksum IS NOT NULL AND length(resources_checksum) = 64
        );
