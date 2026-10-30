-- S3/S4: the existing Encore template table becomes the only access-template
-- owner. Legacy columns are deliberately retained for the later offline
-- conversion and reconciliation window.
ALTER TABLE device_templates RENAME TO device_access_templates;

ALTER TABLE device_access_templates
    DROP CONSTRAINT device_templates_tenant_id_name_key,
    ALTER COLUMN thing_model_id DROP NOT NULL,
    ALTER COLUMN thing_model_snapshot_id DROP NOT NULL;

ALTER TABLE device_access_templates
    ADD COLUMN resource_version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN description VARCHAR(2000),
    ADD COLUMN image_asset_id UUID,
    ADD COLUMN administrative_state VARCHAR(16) NOT NULL DEFAULT 'enabled',
    ADD COLUMN draft JSONB,
    ADD COLUMN created_by VARCHAR(128),
    ADD COLUMN updated_by VARCHAR(128),
    ADD COLUMN deleted_by VARCHAR(128),
    ADD CONSTRAINT device_access_templates_resource_version_range
        CHECK (resource_version BETWEEN 1 AND 9007199254740991),
    ADD CONSTRAINT device_access_templates_administrative_state_valid
        CHECK (administrative_state IN ('enabled', 'disabled'));

-- Historical ARCHIVED means administratively disabled. Other legacy status
-- values remain in the untouched status column and are not guessed here.
UPDATE device_access_templates
SET administrative_state = 'disabled'
WHERE status = 'ARCHIVED';

CREATE UNIQUE INDEX uq_device_access_templates_live_tenant_name
    ON device_access_templates (tenant_id, name)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_device_access_templates_live_created
    ON device_access_templates (tenant_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_device_access_templates_tenant_brand_model
    ON device_access_templates (tenant_id, brand, model_number);

COMMENT ON COLUMN device_access_templates.draft IS
    'Nullable only during S3/S5 offline migration; S6 must backfill a validated TemplateDraft and set NOT NULL.';
COMMENT ON COLUMN device_access_templates.created_by IS
    'Nullable only during S3/S5 offline migration; S6 must backfill a trusted actor and set NOT NULL.';
COMMENT ON COLUMN device_access_templates.updated_by IS
    'Nullable only during S3/S5 offline migration; S6 must backfill a trusted actor and set NOT NULL.';
COMMENT ON COLUMN device_access_templates.deleted_by IS
    'May remain NULL for live rows; S6 must backfill a trusted actor for tombstones and enforce that invariant for deleted rows.';

CREATE TABLE device_access_template_revisions (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    template_id UUID NOT NULL,
    revision INTEGER NOT NULL,
    device_kind VARCHAR(16) NOT NULL,
    model_id UUID,
    model_snapshot_id UUID,
    model_published_revision BIGINT,
    model_semantic_checksum CHAR(64),
    model_resources_checksum CHAR(64),
    access_configuration JSONB NOT NULL,
    topic_mappings JSONB NOT NULL DEFAULT '[]'::jsonb,
    processing_scripts JSONB NOT NULL DEFAULT '[]'::jsonb,
    custom_field_definitions JSONB NOT NULL DEFAULT '[]'::jsonb,
    content_digest CHAR(64) NOT NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_by VARCHAR(128) NOT NULL,

    CONSTRAINT device_access_template_revisions_tenant_id_unique
        UNIQUE (tenant_id, id),
    CONSTRAINT device_access_template_revisions_tenant_template_id_unique
        UNIQUE (tenant_id, template_id, id),
    CONSTRAINT device_access_template_revisions_tenant_template_revision_unique
        UNIQUE (tenant_id, template_id, revision),
    CONSTRAINT device_access_template_revisions_revision_positive
        CHECK (revision > 0),
    CONSTRAINT device_access_template_revisions_device_kind_valid
        CHECK (device_kind IN ('standalone', 'gateway', 'subdevice')),
    CONSTRAINT device_access_template_revisions_model_reference_complete
        CHECK (
            num_nonnulls(
                model_id,
                model_snapshot_id,
                model_published_revision,
                model_semantic_checksum,
                model_resources_checksum
            ) = 0
            OR (
                num_nonnulls(
                    model_id,
                    model_snapshot_id,
                    model_published_revision,
                    model_semantic_checksum,
                    model_resources_checksum
                ) = 5
                AND model_published_revision BETWEEN 1 AND 9007199254740991
                AND model_semantic_checksum::text ~ '^[0-9a-f]{64}$'
                AND model_resources_checksum::text ~ '^[0-9a-f]{64}$'
            )
        ),
    CONSTRAINT device_access_template_revisions_content_digest_valid
        CHECK (content_digest::text ~ '^[0-9a-f]{64}$'),
    CONSTRAINT device_access_template_revisions_configuration_object
        CHECK (jsonb_typeof(access_configuration) = 'object'),
    CONSTRAINT device_access_template_revisions_topic_mappings_array
        CHECK (jsonb_typeof(topic_mappings) = 'array'),
    CONSTRAINT device_access_template_revisions_processing_scripts_array
        CHECK (jsonb_typeof(processing_scripts) = 'array'),
    CONSTRAINT device_access_template_revisions_custom_fields_array
        CHECK (jsonb_typeof(custom_field_definitions) = 'array'),
    CONSTRAINT device_access_template_revisions_published_by_nonempty
        CHECK (length(btrim(published_by)) > 0),
    CONSTRAINT device_access_template_revisions_template_fk
        FOREIGN KEY (tenant_id, template_id)
        REFERENCES device_access_templates (tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT device_access_template_revisions_model_fk
        FOREIGN KEY (tenant_id, model_id)
        REFERENCES thing_models (tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT device_access_template_revisions_snapshot_fk
        FOREIGN KEY (tenant_id, model_id, model_snapshot_id)
        REFERENCES thing_model_snapshots (tenant_id, thing_model_id, id)
        ON DELETE RESTRICT
);

CREATE INDEX idx_device_access_template_revisions_tenant_template_revision_desc
    ON device_access_template_revisions (tenant_id, template_id, revision DESC);
CREATE INDEX idx_device_access_template_revisions_tenant_snapshot
    ON device_access_template_revisions (tenant_id, model_snapshot_id);

CREATE FUNCTION validate_device_access_template_revision_snapshot()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    snapshot_version INTEGER;
    snapshot_canonical BOOLEAN;
    snapshot_canonical_content BYTEA;
    snapshot_canonical_checksum CHAR(64);
    snapshot_resources_content JSONB;
    snapshot_resources_revision BIGINT;
    snapshot_resources_checksum CHAR(64);
BEGIN
    IF num_nonnulls(
        NEW.model_id,
        NEW.model_snapshot_id,
        NEW.model_published_revision,
        NEW.model_semantic_checksum,
        NEW.model_resources_checksum
    ) = 0 THEN
        RETURN NEW;
    END IF;

    IF num_nonnulls(
        NEW.model_id,
        NEW.model_snapshot_id,
        NEW.model_published_revision,
        NEW.model_semantic_checksum,
        NEW.model_resources_checksum
    ) <> 5 THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'device access template revision model reference is incomplete';
    END IF;

    SELECT s.version,
           s.canonical_snapshot,
           s.canonical_content,
           s.canonical_checksum,
           s.resources_content,
           s.resources_revision,
           s.resources_checksum
      INTO snapshot_version,
           snapshot_canonical,
           snapshot_canonical_content,
           snapshot_canonical_checksum,
           snapshot_resources_content,
           snapshot_resources_revision,
           snapshot_resources_checksum
      FROM thing_model_snapshots AS s
      JOIN thing_models AS m
        ON m.tenant_id = s.tenant_id
       AND m.id = s.thing_model_id
     WHERE s.tenant_id = NEW.tenant_id
       AND s.thing_model_id = NEW.model_id
       AND s.id = NEW.model_snapshot_id
       AND m.deleted_at IS NULL
       AND m.status <> 'ARCHIVED'
       AND m.canonical_model IS NOT NULL
     FOR SHARE OF m;

    IF NOT FOUND
       OR snapshot_canonical IS DISTINCT FROM TRUE
       OR snapshot_canonical_content IS NULL
       OR octet_length(snapshot_canonical_content) = 0
       OR snapshot_canonical_checksum IS NULL
       OR snapshot_resources_content IS NULL
       OR jsonb_typeof(snapshot_resources_content) IS DISTINCT FROM 'object'
       OR snapshot_resources_revision IS NULL
       OR snapshot_resources_revision < 0
       OR snapshot_resources_checksum IS NULL THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'device access template revision requires a complete canonical model snapshot';
    END IF;

    IF snapshot_version::BIGINT IS DISTINCT FROM NEW.model_published_revision
       OR snapshot_canonical_checksum IS DISTINCT FROM NEW.model_semantic_checksum
       OR snapshot_resources_checksum IS DISTINCT FROM NEW.model_resources_checksum THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'device access template revision model snapshot metadata does not match';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_device_access_template_revision_snapshot_valid
    BEFORE INSERT ON device_access_template_revisions
    FOR EACH ROW EXECUTE FUNCTION validate_device_access_template_revision_snapshot();

CREATE FUNCTION reject_device_access_template_revision_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION USING
        ERRCODE = '23514',
        MESSAGE = 'device access template revisions are immutable';
END;
$$;

CREATE TRIGGER trg_device_access_template_revision_no_update
    BEFORE UPDATE ON device_access_template_revisions
    FOR EACH ROW EXECUTE FUNCTION reject_device_access_template_revision_mutation();
CREATE TRIGGER trg_device_access_template_revision_no_delete
    BEFORE DELETE ON device_access_template_revisions
    FOR EACH ROW EXECUTE FUNCTION reject_device_access_template_revision_mutation();
