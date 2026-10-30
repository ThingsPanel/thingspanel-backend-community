-- Extend the existing receipt owner for secret-bearing writes. Never put a
-- secret response or an unkeyed digest of a supplied password in JSONB.
ALTER TABLE metadata_idempotency
    ADD COLUMN request_key_id VARCHAR(128),
    ADD COLUMN credential_id UUID,
    ADD COLUMN response_key_id VARCHAR(128),
    ADD COLUMN response_cipher_suite VARCHAR(32),
    ADD COLUMN response_nonce BYTEA,
    ADD COLUMN response_ciphertext BYTEA,
    ADD CONSTRAINT metadata_receipt_credential_fk FOREIGN KEY (tenant_id, credential_id)
        REFERENCES device_credentials (tenant_id, id) ON DELETE RESTRICT;

-- 0019 used an unnamed response-state CHECK. Resolve it by its definition
-- rather than relying on PostgreSQL's generated constraint suffix.
DO $$
DECLARE constraint_name TEXT;
BEGIN
    FOR constraint_name IN
        SELECT conname FROM pg_constraint
        WHERE conrelid = 'metadata_idempotency'::regclass AND contype = 'c'
          AND pg_get_constraintdef(oid) LIKE '%state%'
          AND pg_get_constraintdef(oid) LIKE '%http_status%'
          AND pg_get_constraintdef(oid) LIKE '%response%'
    LOOP
        EXECUTE format('ALTER TABLE metadata_idempotency DROP CONSTRAINT %I', constraint_name);
    END LOOP;
END;
$$;

ALTER TABLE metadata_idempotency
    ADD CONSTRAINT metadata_receipt_state_response_valid CHECK (
        (state = 'in_progress' AND http_status IS NULL AND response IS NULL AND response_ciphertext IS NULL)
        OR (state = 'completed' AND http_status BETWEEN 200 AND 299
            AND ((response IS NOT NULL AND response_ciphertext IS NULL)
                OR (response IS NULL AND response_ciphertext IS NOT NULL)))
        OR (state = 'failed' AND http_status BETWEEN 400 AND 599)
        OR (state = 'expired' AND http_status = 409 AND response IS NULL AND response_ciphertext IS NULL)
    ),
    ADD CONSTRAINT metadata_receipt_secret_shape_valid CHECK (
        (request_key_id IS NULL AND credential_id IS NULL
            AND response_key_id IS NULL AND response_cipher_suite IS NULL
            AND response_nonce IS NULL AND response_ciphertext IS NULL)
        OR (request_key_id IS NOT NULL AND length(request_key_id) BETWEEN 1 AND 128
            AND credential_id IS NOT NULL AND response IS NULL AND response_hash IS NULL
            AND ((state = 'completed' AND response_key_id IS NOT NULL
                    AND length(response_key_id) BETWEEN 1 AND 128
                    AND response_cipher_suite = 'AES-256-GCM'
                    AND response_nonce IS NOT NULL AND octet_length(response_nonce) = 12
                    AND response_ciphertext IS NOT NULL AND octet_length(response_ciphertext) BETWEEN 17 AND 262160)
                OR (state = 'expired' AND response_key_id IS NULL AND response_cipher_suite IS NULL
                    AND response_nonce IS NULL AND response_ciphertext IS NULL)))
    );

COMMENT ON COLUMN metadata_idempotency.request_key_id IS
    'Secret request HMAC key reference. request_hash contains a keyed digest, never an unkeyed password hash.';
COMMENT ON COLUMN metadata_idempotency.response_ciphertext IS
    'AEAD-encrypted full successful secret response; AAD binds tenant, actor, operation, path, request key and credential. Expiry clears ciphertext but keeps the tombstone.';
