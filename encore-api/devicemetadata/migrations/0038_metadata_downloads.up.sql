-- Short-lived artifacts produced by metadata jobs (reading exports first).
-- The row is only reachable through GET /api/v2/metadata-downloads/{id}, which
-- re-checks tenant, the creating subject and the originating permission.

CREATE TABLE metadata_downloads (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    operation_id UUID NOT NULL,
    created_by VARCHAR(128) NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    content_type VARCHAR(128) NOT NULL,
    content BYTEA NOT NULL,
    checksum CHAR(64) NOT NULL,
    size_bytes BIGINT NOT NULL,
    row_count INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,

    CONSTRAINT metadata_downloads_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT metadata_downloads_operation_fk
        FOREIGN KEY (tenant_id, operation_id) REFERENCES metadata_operations (tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT metadata_downloads_checksum_hex CHECK (checksum::text ~ '^[0-9a-f]{64}$'),
    CONSTRAINT metadata_downloads_size_range CHECK (size_bytes BETWEEN 0 AND 67108864 AND octet_length(content) = size_bytes),
    CONSTRAINT metadata_downloads_rows_nonnegative CHECK (row_count >= 0),
    CONSTRAINT metadata_downloads_expiry_after_creation CHECK (expires_at > created_at),
    CONSTRAINT metadata_downloads_created_by_nonempty CHECK (length(btrim(created_by)) BETWEEN 1 AND 128),
    CONSTRAINT metadata_downloads_file_name_nonempty CHECK (length(btrim(file_name)) BETWEEN 1 AND 255)
);

CREATE INDEX idx_metadata_downloads_operation ON metadata_downloads (tenant_id, operation_id);
CREATE INDEX idx_metadata_downloads_expiry ON metadata_downloads (expires_at);

CREATE TRIGGER trg_tenant_id_immutable
    BEFORE UPDATE OF tenant_id ON metadata_downloads
    FOR EACH ROW EXECUTE FUNCTION reject_tenant_id_reassignment();
