-- Shared append-only audit event schema required by the native metadata write
-- slices. Device-group creation writes its success row in the same transaction
-- as the group and idempotency receipt.
CREATE TABLE metadata_audit_events (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(128) NOT NULL,
    operation_id UUID,
    request_id VARCHAR(128) NOT NULL,
    operation_name VARCHAR(128) NOT NULL,
    resource_kind VARCHAR(64) NOT NULL,
    resource_id UUID,
    before_version BIGINT,
    after_version BIGINT,
    outcome VARCHAR(16) NOT NULL CHECK (outcome IN ('succeeded', 'denied', 'failed')),
    reason_code VARCHAR(128),
    redacted_changes JSONB NOT NULL DEFAULT '{}'::jsonb
        CHECK (octet_length(redacted_changes::text) <= 65536),
    UNIQUE (tenant_id, id)
);

CREATE INDEX idx_metadata_audit_tenant_created
    ON metadata_audit_events (tenant_id, created_at DESC, id DESC);
CREATE INDEX idx_metadata_audit_resource
    ON metadata_audit_events (tenant_id, resource_kind, resource_id, created_at);
CREATE INDEX idx_metadata_audit_operation
    ON metadata_audit_events (tenant_id, operation_id);
