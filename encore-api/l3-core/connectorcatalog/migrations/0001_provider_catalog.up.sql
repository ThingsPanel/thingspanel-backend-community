CREATE TABLE provider_releases (
    provider_id UUID NOT NULL,
    version TEXT COLLATE "C" NOT NULL,
    name TEXT NOT NULL,
    protocols JSONB NOT NULL,
    device_kinds JSONB NOT NULL,
    schema_digest TEXT NOT NULL,
    artifact_digest TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    configuration_schema_raw BYTEA NOT NULL,
    device_parameter_schema_raw BYTEA NOT NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT provider_releases_pkey PRIMARY KEY (provider_id, version),
    CONSTRAINT provider_releases_version_length CHECK (char_length(version) BETWEEN 1 AND 128),
    CONSTRAINT provider_releases_name_length CHECK (char_length(name) BETWEEN 1 AND 128),
    CONSTRAINT provider_releases_protocols_array CHECK (
        jsonb_typeof(protocols) = 'array' AND jsonb_array_length(protocols) <= 128
    ),
    CONSTRAINT provider_releases_device_kinds_array CHECK (
        jsonb_typeof(device_kinds) = 'array' AND jsonb_array_length(device_kinds) <= 3
    ),
    CONSTRAINT provider_releases_schema_digest CHECK (schema_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT provider_releases_artifact_digest CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT provider_releases_manifest_digest CHECK (manifest_digest ~ '^[0-9a-f]{64}$'),
    CONSTRAINT provider_releases_configuration_schema_size CHECK (
        octet_length(configuration_schema_raw) BETWEEN 1 AND 1048576
    ),
    CONSTRAINT provider_releases_device_schema_size CHECK (
        octet_length(device_parameter_schema_raw) BETWEEN 1 AND 1048576
    )
);

CREATE FUNCTION reject_provider_release_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'provider releases are immutable'
        USING ERRCODE = '23514', CONSTRAINT = 'provider_releases_immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER provider_releases_reject_update_delete
    BEFORE UPDATE OR DELETE ON provider_releases
    FOR EACH ROW EXECUTE FUNCTION reject_provider_release_mutation();

CREATE TRIGGER provider_releases_reject_truncate
    BEFORE TRUNCATE ON provider_releases
    FOR EACH STATEMENT EXECUTE FUNCTION reject_provider_release_mutation();
