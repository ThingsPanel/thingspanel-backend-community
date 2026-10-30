CREATE TABLE file_assets (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    object_key VARCHAR(1024) NOT NULL CHECK (object_key <> '' AND btrim(object_key) <> ''),
    content_type VARCHAR(32) NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg', 'image/webp')),
    size_bytes BIGINT NOT NULL CHECK (size_bytes BETWEEN 1 AND 10485760),
    checksum CHAR(64) NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
    purpose VARCHAR(32) NOT NULL CHECK (purpose = 'template-image'),
    created_by VARCHAR(128) NOT NULL CHECK (btrim(created_by) <> '' AND created_by !~ '[[:cntrl:]]'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, id),
    UNIQUE (object_key)
);

CREATE FUNCTION file_assets_reject_update() RETURNS trigger AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
        RAISE EXCEPTION 'file asset identity is immutable' USING ERRCODE = '23514';
    END IF;
    IF NEW.object_key IS DISTINCT FROM OLD.object_key
       OR NEW.content_type IS DISTINCT FROM OLD.content_type
       OR NEW.size_bytes IS DISTINCT FROM OLD.size_bytes
       OR NEW.checksum IS DISTINCT FROM OLD.checksum
       OR NEW.purpose IS DISTINCT FROM OLD.purpose
       OR NEW.created_by IS DISTINCT FROM OLD.created_by
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'file asset content is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER file_assets_immutable_update
    BEFORE UPDATE ON file_assets
    FOR EACH ROW EXECUTE FUNCTION file_assets_reject_update();
