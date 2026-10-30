-- Shared synchronous request receipts for native metadata writes. Operation
-- and path are part of the key so one client key can safely span operations.
CREATE TABLE metadata_idempotency (
    tenant_id UUID NOT NULL,
    subject_id VARCHAR(128) NOT NULL CHECK (length(btrim(subject_id)) > 0),
    operation_name VARCHAR(128) NOT NULL CHECK (length(btrim(operation_name)) > 0),
    canonical_path VARCHAR(1024) NOT NULL CHECK (canonical_path LIKE '/%'),
    idempotency_key VARCHAR(128) NOT NULL
        CHECK (octet_length(idempotency_key) BETWEEN 8 AND 128)
        CHECK (idempotency_key ~ '^[A-Za-z0-9._:-]+$'),
    request_hash CHAR(64) NOT NULL CHECK (request_hash ~ '^[a-f0-9]{64}$'),
    state VARCHAR(16) NOT NULL DEFAULT 'in_progress'
        CHECK (state IN ('in_progress', 'completed', 'failed', 'expired')),
    http_status SMALLINT,
    response JSONB,
    response_hash CHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    replay_until TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, subject_id, operation_name, canonical_path, idempotency_key),
    CHECK (response_hash IS NULL OR response_hash ~ '^[a-f0-9]{64}$'),
    CHECK (
        (state = 'in_progress' AND http_status IS NULL AND response IS NULL)
        OR (state = 'completed' AND http_status BETWEEN 200 AND 299 AND response IS NOT NULL)
        OR (state = 'failed' AND http_status BETWEEN 400 AND 599)
        OR (state = 'expired' AND http_status = 409 AND response IS NULL)
    )
);

CREATE INDEX idx_metadata_idempotency_replay_until
    ON metadata_idempotency (replay_until);
CREATE INDEX idx_metadata_idempotency_in_progress
    ON metadata_idempotency (updated_at)
    WHERE state = 'in_progress';
