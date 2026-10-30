-- Templates select a published ThingModel version. Devices inherit that
-- selection through template_id, as in the existing device-config workflow.
-- Do not retain a second, per-device copy of the template's model selection.
CREATE FUNCTION resolve_current_template_model_selection()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    selected_model UUID;
    selected_snapshot UUID;
BEGIN
    -- Legacy rows without current configuration remain pending offline
    -- conversion. Never infer their current selection from a latest version.
    IF NEW.configuration IS NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.configuration->'model' = 'null'::jsonb THEN
        NEW.thing_model_id := NULL;
        NEW.thing_model_snapshot_id := NULL;
        RETURN NEW;
    END IF;
    IF jsonb_typeof(NEW.configuration->'model') IS DISTINCT FROM 'object'
       OR jsonb_typeof(NEW.configuration->'model'->'modelId') IS DISTINCT FROM 'string'
       OR jsonb_typeof(NEW.configuration->'model'->'publishedRevision') IS DISTINCT FROM 'number'
       OR (NEW.configuration->'model'->>'publishedRevision') !~ '^[1-9][0-9]*$' THEN
        RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='template requires a complete published ThingModel selection';
    END IF;
    selected_model := (NEW.configuration->'model'->>'modelId')::uuid;
    SELECT id INTO selected_snapshot
    FROM thing_model_snapshots
    WHERE tenant_id=NEW.tenant_id AND thing_model_id=selected_model
      AND version::text=NEW.configuration->'model'->>'publishedRevision';
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE='23503', MESSAGE='selected ThingModel version is not available in the template tenant';
    END IF;
    NEW.thing_model_id := selected_model;
    NEW.thing_model_snapshot_id := selected_snapshot;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_current_template_model_selection
    BEFORE INSERT OR UPDATE OF configuration, thing_model_id, thing_model_snapshot_id
    ON device_access_templates
    FOR EACH ROW EXECUTE FUNCTION resolve_current_template_model_selection();

-- Resolve only the exact version already selected in current configuration.
-- A missing/cross-tenant selection rejects this migration for explicit repair.
UPDATE device_access_templates SET configuration=configuration
WHERE configuration IS NOT NULL;

ALTER TABLE device_access_bindings
    DROP COLUMN model_id,
    DROP COLUMN model_snapshot_id;

CREATE OR REPLACE FUNCTION enforce_device_access_binding_generation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.configuration_generation < OLD.configuration_generation THEN
        RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='device access binding generation cannot decrease';
    END IF;
    IF (NEW.template_id IS DISTINCT FROM OLD.template_id
        OR NEW.direct_access IS DISTINCT FROM OLD.direct_access
        OR NEW.device_parameters IS DISTINCT FROM OLD.device_parameters)
       AND NEW.configuration_generation <= OLD.configuration_generation THEN
        RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='device access configuration changes require a newer generation';
    END IF;
    RETURN NEW;
END;
$$;
COMMENT ON COLUMN device_access_templates.thing_model_snapshot_id IS
    'Published ThingModel snapshot selected by the current template configuration; shared by its associated devices.';
