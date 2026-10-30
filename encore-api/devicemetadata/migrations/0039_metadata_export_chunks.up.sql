-- Working storage for exports that run across many worker ticks. Chunks are
-- assembled into one metadata_downloads artifact when the last slice finishes
-- and are removed with it (or when the operation fails).

CREATE TABLE metadata_export_chunks (
    tenant_id UUID NOT NULL,
    operation_id UUID NOT NULL,
    seq INTEGER NOT NULL,
    content BYTEA NOT NULL,
    row_count INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (tenant_id, operation_id, seq),
    CONSTRAINT metadata_export_chunks_operation_fk
        FOREIGN KEY (tenant_id, operation_id) REFERENCES metadata_operations (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT metadata_export_chunks_seq_nonnegative CHECK (seq >= 0),
    CONSTRAINT metadata_export_chunks_rows_nonnegative CHECK (row_count >= 0)
);

CREATE TRIGGER trg_tenant_id_immutable
    BEFORE UPDATE OF tenant_id ON metadata_export_chunks
    FOR EACH ROW EXECUTE FUNCTION reject_tenant_id_reassignment();
