-- Imported metadata retains the existing runtime owner; this is not a new access configuration.
CREATE TABLE imported_device_origins (
 tenant_id UUID NOT NULL,
 device_id UUID NOT NULL,
 source_owner TEXT NOT NULL CHECK (source_owner = 'community'),
 source_database TEXT NOT NULL CHECK (length(btrim(source_database)) BETWEEN 1 AND 128),
 source_deployment_id TEXT NOT NULL CHECK (length(btrim(source_deployment_id)) BETWEEN 1 AND 128),
 source_tenant_id TEXT NOT NULL CHECK (length(btrim(source_tenant_id)) BETWEEN 1 AND 128),
 source_device_id TEXT NOT NULL CHECK (source_device_id = device_id::text),
 source_template_id TEXT,
 source_template_name TEXT CHECK (source_template_name IS NULL OR length(source_template_name) <= 128),
 source_protocol TEXT CHECK (source_protocol IS NULL OR length(source_protocol) <= 64),
 source_access_way TEXT CHECK (source_access_way IS NULL OR length(source_access_way) <= 32),
 source_service_access_id TEXT CHECK (source_service_access_id IS NULL OR length(source_service_access_id) <= 256),
 source_row_sha256 CHAR(64) NOT NULL CHECK (source_row_sha256 ~ '^[a-f0-9]{64}$'),
 source_template_sha256 CHAR(64) CHECK (source_template_sha256 IS NULL OR source_template_sha256 ~ '^[a-f0-9]{64}$'),
 source_archive_sha256 CHAR(64) NOT NULL CHECK (source_archive_sha256 ~ '^[a-f0-9]{64}$'),
 imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 imported_by TEXT NOT NULL CHECK (length(btrim(imported_by)) BETWEEN 1 AND 128),
 PRIMARY KEY (tenant_id,device_id),
 FOREIGN KEY (tenant_id,device_id) REFERENCES devices(tenant_id,id) ON DELETE RESTRICT,
 UNIQUE (source_deployment_id,source_tenant_id,source_device_id),
 CHECK ((source_template_id IS NULL) = (source_template_sha256 IS NULL)),
 CHECK (source_template_id IS NULL OR source_template_id ~ '^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$')
);
CREATE FUNCTION reject_imported_device_origin_mutation() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='imported device origin is immutable';
END;
$$;
CREATE TRIGGER imported_device_origins_immutable BEFORE UPDATE OR DELETE ON imported_device_origins
 FOR EACH ROW EXECUTE FUNCTION reject_imported_device_origin_mutation();
