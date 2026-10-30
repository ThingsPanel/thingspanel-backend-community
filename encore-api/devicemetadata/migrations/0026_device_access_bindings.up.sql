-- DM-DB-008: store only the device's desired configuration and confirmed
-- runtime identity reference. Runtime applied state remains owned elsewhere.

CREATE FUNCTION device_access_binding_parameters_valid(value JSONB)
RETURNS BOOLEAN
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    property_count BIGINT;
BEGIN
    IF value IS NULL OR jsonb_typeof(value) <> 'object' THEN
        RETURN FALSE;
    END IF;
    SELECT count(*) INTO property_count FROM jsonb_object_keys(value);
    RETURN property_count <= 100 AND octet_length(value::text) <= 65536;
END;
$$;

CREATE TABLE device_access_bindings (
    tenant_id UUID NOT NULL,
    device_id UUID NOT NULL,
    template_revision_id UUID,
    direct_access JSONB,
    device_parameters JSONB NOT NULL DEFAULT '{}'::jsonb,
    configuration_generation BIGINT NOT NULL DEFAULT 1,
    pending_operation_id UUID,
    runtime_provider_id UUID,
    service_instance_id VARCHAR(256),
    runtime_namespace VARCHAR(128),
    runtime_external_id VARCHAR(256),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by VARCHAR(128) NOT NULL,

    CONSTRAINT device_access_bindings_pk
        PRIMARY KEY (tenant_id, device_id),
    CONSTRAINT device_access_bindings_device_fk
        FOREIGN KEY (tenant_id, device_id)
        REFERENCES devices (tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT device_access_bindings_template_revision_fk
        FOREIGN KEY (tenant_id, template_revision_id)
        REFERENCES device_access_template_revisions (tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT device_access_bindings_pending_operation_fk
        FOREIGN KEY (tenant_id, pending_operation_id)
        REFERENCES metadata_operations (tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT device_access_bindings_configuration_exclusive
        CHECK (template_revision_id IS NULL OR direct_access IS NULL),
    CONSTRAINT device_access_bindings_direct_access_object
        CHECK (direct_access IS NULL OR jsonb_typeof(direct_access) = 'object'),
    CONSTRAINT device_access_bindings_device_parameters_valid
        CHECK (device_access_binding_parameters_valid(device_parameters)),
    CONSTRAINT device_access_bindings_unconfigured_parameters_empty
        CHECK (
            template_revision_id IS NOT NULL
            OR direct_access IS NOT NULL
            OR device_parameters = '{}'::jsonb
        ),
    CONSTRAINT device_access_bindings_generation_range
        CHECK (configuration_generation BETWEEN 1 AND 9007199254740991),
    CONSTRAINT device_access_bindings_runtime_identity_complete
        CHECK (
            (
                num_nonnulls(runtime_provider_id, runtime_namespace, runtime_external_id) = 0
                AND service_instance_id IS NULL
            )
            OR num_nonnulls(runtime_provider_id, runtime_namespace, runtime_external_id) = 3
        ),
    CONSTRAINT device_access_bindings_runtime_provider_nonzero
        CHECK (
            runtime_provider_id IS NULL
            OR runtime_provider_id <> '00000000-0000-0000-0000-000000000000'::uuid
        ),
    CONSTRAINT device_access_bindings_service_instance_nonempty
        CHECK (service_instance_id IS NULL OR length(btrim(service_instance_id)) > 0),
    CONSTRAINT device_access_bindings_runtime_namespace_nonempty
        CHECK (runtime_namespace IS NULL OR length(btrim(runtime_namespace)) > 0),
    CONSTRAINT device_access_bindings_runtime_external_id_nonempty
        CHECK (runtime_external_id IS NULL OR length(btrim(runtime_external_id)) > 0),
    CONSTRAINT device_access_bindings_updated_by_nonempty
        CHECK (length(btrim(updated_by)) > 0)
);

CREATE UNIQUE INDEX uq_device_access_bindings_runtime_instance_identity
    ON device_access_bindings (
        runtime_provider_id,
        service_instance_id,
        runtime_namespace,
        runtime_external_id
    )
    WHERE service_instance_id IS NOT NULL AND runtime_external_id IS NOT NULL;

CREATE UNIQUE INDEX uq_device_access_bindings_runtime_no_instance_identity
    ON device_access_bindings (
        runtime_provider_id,
        runtime_namespace,
        runtime_external_id
    )
    WHERE service_instance_id IS NULL AND runtime_external_id IS NOT NULL;

CREATE INDEX idx_device_access_bindings_template_revision
    ON device_access_bindings (tenant_id, template_revision_id, device_id);
CREATE INDEX idx_device_access_bindings_runtime_instance
    ON device_access_bindings (tenant_id, runtime_provider_id, service_instance_id, device_id);
CREATE INDEX idx_device_access_bindings_pending_operation
    ON device_access_bindings (tenant_id, pending_operation_id)
    WHERE pending_operation_id IS NOT NULL;

CREATE FUNCTION reject_device_access_binding_identity_reassignment()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.device_id IS DISTINCT FROM OLD.device_id THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'device access binding identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_device_access_bindings_identity_immutable
    BEFORE UPDATE OF tenant_id, device_id ON device_access_bindings
    FOR EACH ROW EXECUTE FUNCTION reject_device_access_binding_identity_reassignment();

CREATE FUNCTION update_device_access_bindings_updated_at()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF (to_jsonb(NEW) - 'updated_at') IS DISTINCT FROM (to_jsonb(OLD) - 'updated_at')
       OR NEW.updated_at IS DISTINCT FROM OLD.updated_at THEN
        NEW.updated_at := NOW();
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_device_access_bindings_updated_at
    BEFORE UPDATE ON device_access_bindings
    FOR EACH ROW EXECUTE FUNCTION update_device_access_bindings_updated_at();

CREATE FUNCTION enforce_device_access_binding_generation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.configuration_generation < OLD.configuration_generation THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'device access binding generation cannot decrease';
    END IF;

    IF (NEW.template_revision_id IS DISTINCT FROM OLD.template_revision_id
        OR NEW.direct_access IS DISTINCT FROM OLD.direct_access
        OR NEW.device_parameters IS DISTINCT FROM OLD.device_parameters)
       AND NEW.configuration_generation <= OLD.configuration_generation THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'device access configuration changes require a newer generation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_device_access_bindings_generation
    BEFORE UPDATE ON device_access_bindings
    FOR EACH ROW EXECUTE FUNCTION enforce_device_access_binding_generation();

COMMENT ON TABLE device_access_bindings IS
    'Desired device access and confirmed runtime identity only; applied runtime state is owned by the runtime service.';
COMMENT ON COLUMN device_access_bindings.direct_access IS
    'S3 direct configuration JSON object; full provider and access schema validation belongs to the authorized service/provider boundary.';
COMMENT ON COLUMN device_access_bindings.device_parameters IS
    'Per-device parameters, at most 100 properties and 64 KiB JSONB text; provider-specific schema validation is external.';
COMMENT ON COLUMN device_access_bindings.runtime_provider_id IS
    'Confirmed stable provider identity reference; not a runtime status cache.';
