-- S3: add the native device identity and lifecycle columns without guessing
-- legacy serial number, kind, activation source, name, or principal mappings.
-- S6 is responsible for reconciliation and the final NOT NULL tightening.

ALTER TABLE devices
    ALTER COLUMN device_template_id DROP NOT NULL,
    ALTER COLUMN sn DROP NOT NULL,
    ADD COLUMN device_number VARCHAR(128) COLLATE "C",
    ADD COLUMN name VARCHAR(128),
    ADD COLUMN device_kind VARCHAR(16),
    ADD COLUMN description VARCHAR(2000),
    ADD COLUMN labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN custom_fields JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN location JSONB,
    ADD COLUMN batch_number VARCHAR(128),
    ADD COLUMN administrative_state VARCHAR(16) NOT NULL DEFAULT 'enabled',
    ADD COLUMN activation_state VARCHAR(16),
    ADD COLUMN activation_time_source VARCHAR(24),
    ADD COLUMN created_by VARCHAR(128),
    ADD COLUMN updated_by VARCHAR(128),
    ADD COLUMN deleted_by VARCHAR(128),
    ADD COLUMN resource_version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE FUNCTION device_identity_labels_valid(value JSONB)
RETURNS BOOLEAN
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    entry RECORD;
    property_count BIGINT;
BEGIN
    IF value IS NULL OR jsonb_typeof(value) <> 'object' THEN
        RETURN FALSE;
    END IF;
    SELECT count(*) INTO property_count FROM jsonb_object_keys(value);
    IF property_count > 32 THEN
        RETURN FALSE;
    END IF;
    FOR entry IN SELECT key, val FROM jsonb_each(value) AS item(key, val) LOOP
        IF length(entry.key) NOT BETWEEN 1 AND 64
           OR entry.key !~ '^[A-Za-z0-9_.-]+$'
           OR jsonb_typeof(entry.val) <> 'string'
           OR length(entry.val #>> '{}') > 256 THEN
            RETURN FALSE;
        END IF;
    END LOOP;
    RETURN TRUE;
END;
$$;

CREATE FUNCTION device_identity_tags_valid(value JSONB)
RETURNS BOOLEAN
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    entry RECORD;
    item_count BIGINT;
    distinct_count BIGINT;
BEGIN
    IF value IS NULL OR jsonb_typeof(value) <> 'array' THEN
        RETURN FALSE;
    END IF;
    IF jsonb_array_length(value) > 100 THEN
        RETURN FALSE;
    END IF;
    FOR entry IN SELECT element FROM jsonb_array_elements(value) AS item(element) LOOP
        IF jsonb_typeof(entry.element) <> 'string'
           OR length(entry.element #>> '{}') NOT BETWEEN 1 AND 128 THEN
            RETURN FALSE;
        END IF;
    END LOOP;
    SELECT count(*), count(DISTINCT element)
      INTO item_count, distinct_count
      FROM jsonb_array_elements(value) AS item(element);
    RETURN item_count = distinct_count;
END;
$$;

CREATE FUNCTION device_identity_custom_fields_valid(value JSONB)
RETURNS BOOLEAN
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    entry RECORD;
    property_count BIGINT;
BEGIN
    IF value IS NULL OR jsonb_typeof(value) <> 'object' THEN
        RETURN FALSE;
    END IF;
    SELECT count(*) INTO property_count FROM jsonb_object_keys(value);
    IF property_count > 100 THEN
        RETURN FALSE;
    END IF;
    FOR entry IN SELECT val FROM jsonb_each(value) AS item(key, val) LOOP
        IF jsonb_typeof(entry.val) NOT IN ('string', 'number', 'boolean', 'null') THEN
            RETURN FALSE;
        END IF;
    END LOOP;
    RETURN TRUE;
END;
$$;

CREATE FUNCTION device_identity_location_valid(value JSONB)
RETURNS BOOLEAN
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    latitude_type TEXT;
    longitude_type TEXT;
    address_type TEXT;
    property_count BIGINT;
BEGIN
    IF value IS NULL OR jsonb_typeof(value) <> 'object' THEN
        RETURN FALSE;
    END IF;
    SELECT count(*) INTO property_count FROM jsonb_object_keys(value);
    IF property_count <> 3 OR NOT (value ?& ARRAY['latitude', 'longitude', 'address']) THEN
        RETURN FALSE;
    END IF;

    latitude_type := jsonb_typeof(value -> 'latitude');
    longitude_type := jsonb_typeof(value -> 'longitude');
    address_type := jsonb_typeof(value -> 'address');
    IF latitude_type NOT IN ('number', 'null') OR longitude_type NOT IN ('number', 'null')
       OR address_type NOT IN ('string', 'null') THEN
        RETURN FALSE;
    END IF;
    IF (latitude_type = 'null') <> (longitude_type = 'null') THEN
        RETURN FALSE;
    END IF;
    IF latitude_type = 'number'
       AND ((value -> 'latitude') < '-90'::jsonb OR (value -> 'latitude') > '90'::jsonb
            OR (value -> 'longitude') < '-180'::jsonb OR (value -> 'longitude') > '180'::jsonb) THEN
        RETURN FALSE;
    END IF;
    IF address_type = 'string' AND length(value ->> 'address') > 512 THEN
        RETURN FALSE;
    END IF;
    RETURN TRUE;
END;
$$;

ALTER TABLE devices
    ADD CONSTRAINT devices_device_number_valid
        CHECK (
            device_number IS NULL
            OR (
                length(device_number) BETWEEN 1 AND 128
                AND device_number = btrim(
                    device_number,
                    ' ' || chr(9) || chr(10) || chr(11) || chr(12) || chr(13) || chr(133) || chr(160)
                    || chr(5760) || chr(8192) || chr(8193) || chr(8194) || chr(8195) || chr(8196)
                    || chr(8197) || chr(8198) || chr(8199) || chr(8200) || chr(8201) || chr(8202)
                    || chr(8232) || chr(8233) || chr(8239) || chr(8287) || chr(12288)
                )
                AND device_number !~ '[[:cntrl:]]'
                AND device_number !~ ('[' || chr(128) || '-' || chr(159) || ']')
            )
        ),
    ADD CONSTRAINT devices_name_valid
        CHECK (name IS NULL OR length(name) BETWEEN 1 AND 128),
    ADD CONSTRAINT devices_device_kind_valid
        CHECK (device_kind IS NULL OR device_kind IN ('standalone', 'gateway', 'subdevice')),
    ADD CONSTRAINT devices_administrative_state_valid
        CHECK (administrative_state IN ('enabled', 'disabled')),
    ADD CONSTRAINT devices_activation_state_valid
        CHECK (activation_state IS NULL OR activation_state IN ('inactive', 'active')),
    ADD CONSTRAINT devices_activation_time_source_valid
        CHECK (
            (activation_state IS NULL AND activation_time_source IS NULL)
            OR (
                activation_state IS NOT NULL
                AND activation_time_source IS NOT NULL
                AND (
                    (activation_state = 'inactive' AND activation_time_source = 'not_activated' AND activated_at IS NULL)
                    OR (activation_state = 'active' AND activation_time_source IN ('runtime_confirmation', 'source_record') AND activated_at IS NOT NULL)
                    OR (activation_state = 'active' AND activation_time_source = 'source_unknown' AND activated_at IS NULL)
                )
            )
        ),
    ADD CONSTRAINT devices_created_by_nonempty
        CHECK (created_by IS NULL OR length(btrim(created_by)) > 0),
    ADD CONSTRAINT devices_updated_by_nonempty
        CHECK (updated_by IS NULL OR length(btrim(updated_by)) > 0),
    ADD CONSTRAINT devices_deleted_by_nonempty
        CHECK (deleted_by IS NULL OR length(btrim(deleted_by)) > 0),
    ADD CONSTRAINT devices_resource_version_range
        CHECK (resource_version BETWEEN 1 AND 9007199254740991),
    ADD CONSTRAINT devices_labels_valid
        CHECK (device_identity_labels_valid(labels)),
    ADD CONSTRAINT devices_tags_valid
        CHECK (device_identity_tags_valid(tags)),
    ADD CONSTRAINT devices_custom_fields_valid
        CHECK (device_identity_custom_fields_valid(custom_fields)),
    ADD CONSTRAINT devices_location_valid
        CHECK (location IS NULL OR device_identity_location_valid(location));

CREATE UNIQUE INDEX uq_devices_tenant_device_number
    ON devices (tenant_id, device_number);
CREATE INDEX idx_devices_live_tenant_created
    ON devices (tenant_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_devices_tenant_kind_created
    ON devices (tenant_id, device_kind, created_at DESC, id DESC);
CREATE INDEX idx_devices_tenant_product
    ON devices (tenant_id, product_id);
CREATE INDEX idx_devices_tenant_administrative_activation
    ON devices (tenant_id, administrative_state, activation_state);
CREATE INDEX idx_devices_tenant_batch
    ON devices (tenant_id, batch_number);

CREATE FUNCTION update_device_identity_updated_at()
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

CREATE TRIGGER trg_devices_updated_at
    BEFORE UPDATE ON devices
    FOR EACH ROW EXECUTE FUNCTION update_device_identity_updated_at();

COMMENT ON COLUMN devices.device_number IS
    'S3 nullable for offline reconciliation; S6 must validate/backfill and set NOT NULL. Legacy sn remains separate and is not a runtime fallback.';
COMMENT ON COLUMN devices.name IS
    'S3 nullable because legacy device names are not inferred; S6 reconciliation must backfill and set NOT NULL.';
COMMENT ON COLUMN devices.device_kind IS
    'S3 nullable because legacy gateway/subdevice kind is not inferred; S6 reconciliation must backfill and set NOT NULL.';
COMMENT ON COLUMN devices.activation_state IS
    'S3 nullable while legacy activated_at facts are reconciled; target state/source combinations are checked when supplied.';
COMMENT ON COLUMN devices.activation_time_source IS
    'S3 nullable because historical activation provenance is not inferred; S6 must reconcile before NOT NULL.';
COMMENT ON COLUMN devices.created_by IS
    'S3 nullable because legacy creator identity is not guessed; S6 reconciliation must backfill a trusted subject.';
COMMENT ON COLUMN devices.updated_by IS
    'S3 nullable because legacy updater identity is not guessed; S6 reconciliation must backfill a trusted subject.';
COMMENT ON COLUMN devices.deleted_by IS
    'S3 nullable for legacy tombstones; S6 must reconcile deleted actor provenance and enforce the E+D pair.';
COMMENT ON COLUMN devices.sn IS
    'Legacy serial number retained for offline reconciliation only; native device identity uses device_number.';
COMMENT ON COLUMN devices.device_template_id IS
    'Legacy template projection retained nullable for offline reconciliation; native template binding is owned separately and this column is not a runtime fallback.';
COMMENT ON COLUMN devices.voucher IS
    'Legacy credential material retained only for offline reconciliation; native device credentials use the dedicated credential owner.';
COMMENT ON COLUMN devices.is_online IS
    'Legacy connection projection retained only for offline reconciliation; runtime state has a separate owner.';
COMMENT ON COLUMN devices.activate_flag IS
    'Legacy activation projection retained only for offline reconciliation; native activation uses activation_state and activation_time_source.';
COMMENT ON COLUMN devices.parent_device_id IS
    'Legacy hierarchy projection retained only for offline reconciliation; native parent relationships use gateway membership.';
COMMENT ON COLUMN devices.topic_profile IS
    'Legacy topic projection retained only for offline reconciliation; native topic behavior is pinned by the access-template revision.';
