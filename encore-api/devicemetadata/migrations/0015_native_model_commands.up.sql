-- Native ThingModel v2 state. Old rows intentionally remain unclassified:
-- canonical_model is NULL and both revision counters start at zero until an
-- explicit converter writes the v2 aggregate.
ALTER TABLE thing_models
    ADD COLUMN canonical_model JSONB,
    ADD COLUMN draft_revision BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN published_revision BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT thing_models_native_revisions_nonnegative
        CHECK (draft_revision >= 0 AND published_revision >= 0);

CREATE INDEX idx_thing_models_native_tenant_updated
    ON thing_models (tenant_id, updated_at DESC, id)
    WHERE canonical_model IS NOT NULL AND deleted_at IS NULL;

-- The shared snapshot table keeps v1 snapshots untouched. Only rows explicitly
-- written by the native repository have canonical_snapshot=true and exact
-- checksum input bytes; v2 never guesses a legacy snapshot's wire format.
ALTER TABLE thing_model_snapshots
    ADD COLUMN canonical_snapshot BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN canonical_content BYTEA,
    ADD COLUMN canonical_checksum CHAR(64);

ALTER TABLE thing_model_snapshots
    ADD CONSTRAINT thing_model_snapshots_native_payload_complete
        CHECK (
            (canonical_snapshot = FALSE AND canonical_content IS NULL AND canonical_checksum IS NULL)
            OR
            (canonical_snapshot = TRUE AND canonical_content IS NOT NULL AND canonical_checksum IS NOT NULL)
        );

CREATE INDEX idx_thing_model_snapshots_native_versions
    ON thing_model_snapshots (tenant_id, thing_model_id, version DESC)
    WHERE canonical_snapshot = TRUE;

CREATE TABLE thing_model_command_receipts (
    tenant_id UUID NOT NULL,
    subject_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    payload_hash CHAR(64) NOT NULL,
    result JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, subject_id, operation, idempotency_key),
    CHECK (octet_length(idempotency_key) BETWEEN 8 AND 128),
    CHECK (length(payload_hash) = 64)
);

CREATE INDEX idx_thing_model_receipts_expiry
    ON thing_model_command_receipts (expires_at);

-- Expired receipt rows remain as compact key tombstones: replay after the
-- 30-day window is rejected instead of silently executing again.
CREATE TABLE thing_model_outbox (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    model_id UUID NOT NULL REFERENCES thing_models(id) ON DELETE RESTRICT,
    operation TEXT NOT NULL,
    draft_revision BIGINT NOT NULL CHECK (draft_revision > 0),
    payload JSONB NOT NULL,
    state TEXT NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING','PROCESSING','DELIVERED','FAILED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT
);

CREATE INDEX idx_thing_model_outbox_pending
    ON thing_model_outbox (created_at, id)
    WHERE state IN ('PENDING','FAILED');
CREATE INDEX idx_thing_model_outbox_tenant_model
    ON thing_model_outbox (tenant_id, model_id, created_at DESC);
