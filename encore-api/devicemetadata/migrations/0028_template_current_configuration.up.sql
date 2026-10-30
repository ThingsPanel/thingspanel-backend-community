-- User design correction: templates have one editable current configuration.
-- ThingModel snapshots remain versioned. Applied migrations 0023/0026 are
-- intentionally unchanged; no production migration is authorized here.
ALTER TABLE device_access_templates RENAME COLUMN draft TO configuration;
COMMENT ON COLUMN device_access_templates.configuration IS
    'Single current template configuration, saved directly; no draft/published pair. Nullable legacy rows require explicit offline conversion.';

-- Existing current configuration is the only authoritative editable value.
-- A legacy NULL remains pending explicit offline conversion; do not infer
-- current intent from MAX(published revision) after withdrawing publication.

ALTER TABLE device_access_bindings
    ADD COLUMN template_id UUID,
    ADD COLUMN model_id UUID,
    ADD COLUMN model_snapshot_id UUID;

-- Device model bindings retain their existing fixed ThingModel snapshot even
-- when the ordinary template is subsequently edited.
UPDATE device_access_bindings b
SET template_id=r.template_id, model_id=r.model_id, model_snapshot_id=r.model_snapshot_id
FROM device_access_template_revisions r
WHERE r.tenant_id=b.tenant_id AND r.id=b.template_revision_id;

ALTER TABLE device_access_bindings
    DROP CONSTRAINT device_access_bindings_configuration_exclusive,
    DROP CONSTRAINT device_access_bindings_unconfigured_parameters_empty,
    DROP CONSTRAINT device_access_bindings_template_revision_fk;
DROP TRIGGER trg_device_access_bindings_generation ON device_access_bindings;
ALTER TABLE device_access_bindings DROP COLUMN template_revision_id;

ALTER TABLE device_access_bindings
    ADD CONSTRAINT device_access_bindings_template_fk
        FOREIGN KEY (tenant_id, template_id)
        REFERENCES device_access_templates (tenant_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT device_access_bindings_model_fk
        FOREIGN KEY (tenant_id, model_id)
        REFERENCES thing_models (tenant_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT device_access_bindings_snapshot_fk
        FOREIGN KEY (tenant_id, model_id, model_snapshot_id)
        REFERENCES thing_model_snapshots (tenant_id, thing_model_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT device_access_bindings_model_reference_complete
        CHECK ((model_id IS NULL) = (model_snapshot_id IS NULL)),
    ADD CONSTRAINT device_access_bindings_configuration_exclusive
        CHECK (template_id IS NULL OR direct_access IS NULL),
    ADD CONSTRAINT device_access_bindings_unconfigured_parameters_empty
        CHECK (template_id IS NOT NULL OR direct_access IS NOT NULL OR device_parameters='{}'::jsonb);
CREATE INDEX idx_device_access_bindings_template
    ON device_access_bindings (tenant_id, template_id, device_id);

CREATE OR REPLACE FUNCTION enforce_device_access_binding_generation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.configuration_generation < OLD.configuration_generation THEN
        RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='device access binding generation cannot decrease';
    END IF;
    IF (NEW.template_id IS DISTINCT FROM OLD.template_id
        OR NEW.model_id IS DISTINCT FROM OLD.model_id
        OR NEW.model_snapshot_id IS DISTINCT FROM OLD.model_snapshot_id
        OR NEW.direct_access IS DISTINCT FROM OLD.direct_access
        OR NEW.device_parameters IS DISTINCT FROM OLD.device_parameters)
       AND NEW.configuration_generation <= OLD.configuration_generation THEN
        RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='device access configuration changes require a newer generation';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_device_access_bindings_generation
    BEFORE UPDATE ON device_access_bindings
    FOR EACH ROW EXECUTE FUNCTION enforce_device_access_binding_generation();

DROP TABLE device_access_template_revisions;
DROP FUNCTION validate_device_access_template_revision_snapshot();
DROP FUNCTION reject_device_access_template_revision_mutation();
COMMENT ON COLUMN device_access_bindings.template_id IS
    'Ordinary template identity; templates do not have published revisions.';
COMMENT ON COLUMN device_access_bindings.model_snapshot_id IS
    'Fixed published ThingModel snapshot; independent of template edits.';
