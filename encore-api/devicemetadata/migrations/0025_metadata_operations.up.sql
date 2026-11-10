-- S3/S4 storage foundation for device-metadata asynchronous operations.
-- These tables record durable job state and file references only. They do not
-- implement a worker, scheduler, API endpoint, or secret storage.

CREATE FUNCTION metadata_operation_result_shape(value JSONB)
RETURNS BOOLEAN
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT CASE
        WHEN jsonb_typeof(value) <> 'object' THEN FALSE
        ELSE
            NOT EXISTS (
                SELECT 1
                FROM jsonb_object_keys(value) AS property(key)
                WHERE key NOT IN ('resourceIds', 'downloadId', 'checksum')
            )
            AND (
                NOT (value ? 'resourceIds')
                OR CASE
                    WHEN jsonb_typeof(value -> 'resourceIds') <> 'array' THEN FALSE
                    WHEN jsonb_array_length(value -> 'resourceIds') > 10000 THEN FALSE
                    ELSE NOT EXISTS (
                        SELECT 1
                        FROM jsonb_array_elements(value -> 'resourceIds') AS entry(value)
                        WHERE jsonb_typeof(entry.value) <> 'string'
                           OR (entry.value #>> '{}') !~ '^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$'
                    )
                END
            )
            AND (
                NOT (value ? 'downloadId')
                OR jsonb_typeof(value -> 'downloadId') = 'null'
                OR (
                    jsonb_typeof(value -> 'downloadId') = 'string'
                    AND (value ->> 'downloadId') ~ '^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$'
                )
            )
            AND (
                NOT (value ? 'checksum')
                OR jsonb_typeof(value -> 'checksum') = 'null'
                OR (jsonb_typeof(value -> 'checksum') = 'string' AND length(value ->> 'checksum') = 64)
            )
    END;
$$;

CREATE FUNCTION metadata_operation_problem_shape(value JSONB)
RETURNS BOOLEAN
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT CASE
        WHEN jsonb_typeof(value) <> 'object' THEN FALSE
        ELSE
            value ?& ARRAY['type', 'title', 'status', 'detail', 'instance', 'code', 'requestId']
            AND NOT EXISTS (
                SELECT 1
                FROM jsonb_object_keys(value) AS property(key)
                WHERE key NOT IN ('type', 'title', 'status', 'detail', 'instance', 'code', 'requestId', 'errors', 'conflicts')
            )
            AND jsonb_typeof(value -> 'type') = 'string'
            AND jsonb_typeof(value -> 'title') = 'string'
            AND length(value ->> 'title') BETWEEN 1 AND 256
            AND jsonb_typeof(value -> 'status') = 'number'
            AND (value ->> 'status') ~ '^[45][0-9]{2}$'
            AND jsonb_typeof(value -> 'detail') = 'string'
            AND length(value ->> 'detail') <= 2048
            AND jsonb_typeof(value -> 'instance') = 'string'
            AND jsonb_typeof(value -> 'code') = 'string'
            AND length(value ->> 'code') BETWEEN 1 AND 128
            AND jsonb_typeof(value -> 'requestId') = 'string'
            AND length(value ->> 'requestId') BETWEEN 1 AND 128
            AND (
                NOT (value ? 'errors')
                OR CASE
                    WHEN jsonb_typeof(value -> 'errors') <> 'array' THEN FALSE
                    WHEN jsonb_array_length(value -> 'errors') > 100 THEN FALSE
                    ELSE NOT EXISTS (
                        SELECT 1
                        FROM jsonb_array_elements(value -> 'errors') AS entry(value)
                        WHERE CASE
                            WHEN jsonb_typeof(entry.value) <> 'object' THEN TRUE
                            ELSE
                                NOT (entry.value ?& ARRAY['pointer', 'code', 'message'])
                                OR EXISTS (
                                    SELECT 1
                                    FROM jsonb_object_keys(entry.value) AS property(key)
                                    WHERE key NOT IN ('pointer', 'code', 'message')
                                )
                                OR jsonb_typeof(entry.value -> 'pointer') <> 'string'
                                OR length(entry.value ->> 'pointer') > 256
                                OR jsonb_typeof(entry.value -> 'code') <> 'string'
                                OR length(entry.value ->> 'code') NOT BETWEEN 1 AND 128
                                OR jsonb_typeof(entry.value -> 'message') <> 'string'
                                OR length(entry.value ->> 'message') > 512
                        END
                    )
                END
            )
            AND (
                NOT (value ? 'conflicts')
                OR CASE
                    WHEN jsonb_typeof(value -> 'conflicts') <> 'array' THEN FALSE
                    WHEN jsonb_array_length(value -> 'conflicts') > 100 THEN FALSE
                    ELSE NOT EXISTS (
                        SELECT 1
                        FROM jsonb_array_elements(value -> 'conflicts') AS entry(value)
                        WHERE CASE
                            WHEN jsonb_typeof(entry.value) <> 'object' THEN TRUE
                            ELSE
                                NOT (entry.value ?& ARRAY['kind', 'reasonCode', 'visibleResourceId'])
                                OR EXISTS (
                                    SELECT 1
                                    FROM jsonb_object_keys(entry.value) AS property(key)
                                    WHERE key NOT IN ('kind', 'reasonCode', 'visibleResourceId')
                                )
                                OR jsonb_typeof(entry.value -> 'kind') <> 'string'
                                OR length(entry.value ->> 'kind') NOT BETWEEN 1 AND 64
                                OR jsonb_typeof(entry.value -> 'reasonCode') <> 'string'
                                OR length(entry.value ->> 'reasonCode') NOT BETWEEN 1 AND 128
                                OR (
                                    jsonb_typeof(entry.value -> 'visibleResourceId') <> 'null'
                                    AND (
                                        jsonb_typeof(entry.value -> 'visibleResourceId') <> 'string'
                                        OR (entry.value ->> 'visibleResourceId') !~ '^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$'
                                    )
                                )
                        END
                    )
                END
            )
    END;
$$;

CREATE TABLE metadata_operations (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(128) NOT NULL,
    updated_by VARCHAR(128) NOT NULL,
    resource_version BIGINT NOT NULL DEFAULT 1,

    kind VARCHAR(40) NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'pending',
    originating_operation_name VARCHAR(128) NOT NULL,
    originating_subject_id VARCHAR(128) NOT NULL,
    resource_kind VARCHAR(48),
    resource_id UUID,
    authorized_selection JSONB NOT NULL DEFAULT '{}'::jsonb,
    request_digest CHAR(64) NOT NULL,
    input_artifact_id UUID,
    validation_digest CHAR(64),
    total_items INTEGER NOT NULL DEFAULT 0,
    completed_items INTEGER NOT NULL DEFAULT 0,
    failed_items INTEGER NOT NULL DEFAULT 0,
    checkpoint JSONB NOT NULL DEFAULT '{}'::jsonb,
    result JSONB,
    failure JSONB,
    lease_owner VARCHAR(128),
    lease_until TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,

    CONSTRAINT metadata_operations_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT metadata_operations_kind_valid CHECK (kind IN (
        'device_activation',
        'device_deletion',
        'device_renumber',
        'administrative_state',
        'template_binding',
        'gateway_binding',
        'credential_change',
        'batch_create',
        'batch_template_binding',
        'device_import',
        'device_export',
        'template_import',
        'template_export',
        'gateway_binding_batch'
    )),
    CONSTRAINT metadata_operations_state_valid
        CHECK (state IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT metadata_operations_originating_operation_nonempty
        CHECK (length(btrim(originating_operation_name)) BETWEEN 1 AND 128),
    CONSTRAINT metadata_operations_originating_subject_nonempty
        CHECK (length(btrim(originating_subject_id)) BETWEEN 1 AND 128),
    CONSTRAINT metadata_operations_actor_nonempty
        CHECK (length(btrim(created_by)) BETWEEN 1 AND 128 AND length(btrim(updated_by)) BETWEEN 1 AND 128),
    CONSTRAINT metadata_operations_resource_version_range
        CHECK (resource_version BETWEEN 1 AND 9007199254740991),
    CONSTRAINT metadata_operations_resource_pair
        CHECK ((resource_kind IS NULL) = (resource_id IS NULL)),
    CONSTRAINT metadata_operations_resource_kind_nonempty
        CHECK (resource_kind IS NULL OR length(btrim(resource_kind)) BETWEEN 1 AND 48),
    CONSTRAINT metadata_operations_authorized_selection_object
        CHECK (jsonb_typeof(authorized_selection) = 'object'),
    CONSTRAINT metadata_operations_request_digest_valid
        CHECK (request_digest::text ~ '^[0-9a-f]{64}$'),
    CONSTRAINT metadata_operations_validation_digest_valid
        CHECK (validation_digest IS NULL OR validation_digest::text ~ '^[0-9a-f]{64}$'),
    CONSTRAINT metadata_operations_progress_counts_valid
        CHECK (
            total_items >= 0
            AND completed_items >= 0
            AND completed_items <= total_items
            AND failed_items >= 0
            AND failed_items <= completed_items
        ),
    CONSTRAINT metadata_operations_checkpoint_object
        CHECK (jsonb_typeof(checkpoint) = 'object'),
    CONSTRAINT metadata_operations_result_object
        CHECK (result IS NULL OR metadata_operation_result_shape(result)),
    CONSTRAINT metadata_operations_failure_object
        CHECK (failure IS NULL OR metadata_operation_problem_shape(failure)),
    CONSTRAINT metadata_operations_failed_requires_failure
        CHECK (state <> 'failed' OR failure IS NOT NULL),
    CONSTRAINT metadata_operations_success_complete
        CHECK (state <> 'succeeded' OR (completed_items = total_items AND failed_items = 0)),
    CONSTRAINT metadata_operations_lease_pair
        CHECK ((lease_owner IS NULL) = (lease_until IS NULL)),
    CONSTRAINT metadata_operations_attempts_nonnegative
        CHECK (attempts >= 0),
    CONSTRAINT metadata_operations_finished_state_pair
        CHECK ((state IN ('succeeded', 'failed', 'cancelled')) = (finished_at IS NOT NULL))
);

CREATE TABLE metadata_operation_items (
    tenant_id UUID NOT NULL,
    operation_id UUID NOT NULL,
    item_key VARCHAR(128) NOT NULL,
    source_row INTEGER,
    input_digest CHAR(64) NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'pending',
    resource_id UUID,
    expected_resource_version BIGINT,
    target_generation BIGINT,
    desired_document JSONB,
    checkpoint JSONB NOT NULL DEFAULT '{}'::jsonb,
    owner_receipt JSONB,
    error JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT metadata_operation_items_pkey PRIMARY KEY (tenant_id, operation_id, item_key),
    CONSTRAINT metadata_operation_items_operation_fk
        FOREIGN KEY (tenant_id, operation_id)
        REFERENCES metadata_operations (tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT metadata_operation_items_key_nonempty
        CHECK (length(btrim(item_key)) BETWEEN 1 AND 128),
    CONSTRAINT metadata_operation_items_source_row_positive
        CHECK (source_row IS NULL OR source_row BETWEEN 1 AND 2147483647),
    CONSTRAINT metadata_operation_items_input_digest_valid
        CHECK (input_digest::text ~ '^[0-9a-f]{64}$'),
    CONSTRAINT metadata_operation_items_state_valid
        CHECK (state IN ('pending', 'succeeded', 'failed', 'skipped')),
    CONSTRAINT metadata_operation_items_resource_version_range
        CHECK (expected_resource_version IS NULL OR expected_resource_version BETWEEN 1 AND 9007199254740991),
    CONSTRAINT metadata_operation_items_target_generation_range
        CHECK (target_generation IS NULL OR target_generation BETWEEN 1 AND 9007199254740991),
    CONSTRAINT metadata_operation_items_desired_document_object
        CHECK (desired_document IS NULL OR jsonb_typeof(desired_document) = 'object'),
    CONSTRAINT metadata_operation_items_checkpoint_object
        CHECK (jsonb_typeof(checkpoint) = 'object'),
    CONSTRAINT metadata_operation_items_owner_receipt_object
        CHECK (owner_receipt IS NULL OR jsonb_typeof(owner_receipt) = 'object'),
    CONSTRAINT metadata_operation_items_error_object
        CHECK (error IS NULL OR metadata_operation_problem_shape(error)),
    CONSTRAINT metadata_operation_items_failed_requires_error
        CHECK (state <> 'failed' OR error IS NOT NULL)
);

CREATE TABLE metadata_artifacts (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(128) NOT NULL,

    operation_id UUID NOT NULL,
    purpose VARCHAR(32) NOT NULL,
    file_owner VARCHAR(64) NOT NULL,
    file_object_id VARCHAR(256) NOT NULL,
    file_name VARCHAR(256) NOT NULL,
    content_type VARCHAR(128) NOT NULL,
    size_bytes BIGINT NOT NULL,
    checksum CHAR(64) NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'staged',
    expires_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,

    CONSTRAINT metadata_artifacts_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT metadata_artifacts_purpose_valid
        CHECK (purpose IN ('import_source', 'validation_report', 'export', 'package', 'report')),
    CONSTRAINT metadata_artifacts_file_owner_nonempty
        CHECK (length(btrim(file_owner)) BETWEEN 1 AND 64),
    CONSTRAINT metadata_artifacts_object_id_nonempty
        CHECK (length(btrim(file_object_id)) BETWEEN 1 AND 256),
    CONSTRAINT metadata_artifacts_file_name_valid
        CHECK (
            length(btrim(file_name)) BETWEEN 1 AND 256
            AND position('/' IN file_name) = 0
            AND position(chr(92) IN file_name) = 0
        ),
    CONSTRAINT metadata_artifacts_content_type_nonempty
        CHECK (length(btrim(content_type)) BETWEEN 1 AND 128),
    CONSTRAINT metadata_artifacts_size_nonnegative CHECK (size_bytes >= 0),
    CONSTRAINT metadata_artifacts_checksum_valid
        CHECK (checksum::text ~ '^[0-9a-f]{64}$'),
    CONSTRAINT metadata_artifacts_state_valid
        CHECK (state IN ('staged', 'ready', 'expired', 'deleted')),
    CONSTRAINT metadata_artifacts_deleted_state_pair
        CHECK ((state = 'deleted') = (deleted_at IS NOT NULL)),
    CONSTRAINT metadata_artifacts_created_by_nonempty
        CHECK (length(btrim(created_by)) BETWEEN 1 AND 128),
    CONSTRAINT metadata_artifacts_operation_purpose_checksum_unique
        UNIQUE (tenant_id, operation_id, purpose, checksum)
);

-- The operation/artifact references are cyclic by contract. Both are deferred
-- so a single transaction can create a job, stage its file, then attach input.
ALTER TABLE metadata_artifacts
    ADD CONSTRAINT metadata_artifacts_operation_fk
    FOREIGN KEY (tenant_id, operation_id)
    REFERENCES metadata_operations (tenant_id, id)
    ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE metadata_operations
    ADD CONSTRAINT metadata_operations_input_artifact_fk
    FOREIGN KEY (tenant_id, input_artifact_id)
    REFERENCES metadata_artifacts (tenant_id, id)
    ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX idx_metadata_operations_tenant_created
    ON metadata_operations (tenant_id, created_at DESC, id DESC);
CREATE INDEX idx_metadata_operations_tenant_subject_created
    ON metadata_operations (tenant_id, originating_subject_id, created_at DESC, id DESC);
CREATE INDEX idx_metadata_operations_state_lease
    ON metadata_operations (state, lease_until);
CREATE INDEX idx_metadata_operations_tenant_resource
    ON metadata_operations (tenant_id, resource_id)
    WHERE resource_id IS NOT NULL;

CREATE INDEX idx_metadata_operation_items_tenant_operation_state_key
    ON metadata_operation_items (tenant_id, operation_id, state, item_key);

CREATE INDEX idx_metadata_artifacts_tenant_operation_purpose
    ON metadata_artifacts (tenant_id, operation_id, purpose);
CREATE INDEX idx_metadata_artifacts_state_expires
    ON metadata_artifacts (state, expires_at);

CREATE FUNCTION prevent_metadata_operation_terminal_state_regression()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.state IN ('succeeded', 'failed', 'cancelled')
       AND NEW.state IS DISTINCT FROM OLD.state THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'metadata operation terminal state is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_metadata_operations_terminal_state
    BEFORE UPDATE OF state ON metadata_operations
    FOR EACH ROW EXECUTE FUNCTION prevent_metadata_operation_terminal_state_regression();

CREATE FUNCTION prevent_metadata_operation_item_terminal_state_regression()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.state IN ('succeeded', 'failed', 'skipped')
       AND NEW.state IS DISTINCT FROM OLD.state THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'metadata operation item terminal state is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_metadata_operation_items_terminal_state
    BEFORE UPDATE OF state ON metadata_operation_items
    FOR EACH ROW EXECUTE FUNCTION prevent_metadata_operation_item_terminal_state_regression();

-- 0021 only attached this trigger to tables that existed at that migration.
CREATE TRIGGER trg_metadata_operations_tenant_immutable
    BEFORE UPDATE OF tenant_id ON metadata_operations
    FOR EACH ROW EXECUTE FUNCTION reject_tenant_id_reassignment();
CREATE TRIGGER trg_metadata_operation_items_tenant_immutable
    BEFORE UPDATE OF tenant_id ON metadata_operation_items
    FOR EACH ROW EXECUTE FUNCTION reject_tenant_id_reassignment();
CREATE TRIGGER trg_metadata_artifacts_tenant_immutable
    BEFORE UPDATE OF tenant_id ON metadata_artifacts
    FOR EACH ROW EXECUTE FUNCTION reject_tenant_id_reassignment();

CREATE FUNCTION protect_metadata_artifact_contents_and_lifecycle()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF ROW(
        NEW.id,
        NEW.tenant_id,
        NEW.created_at,
        NEW.created_by,
        NEW.operation_id,
        NEW.purpose,
        NEW.file_owner,
        NEW.file_object_id,
        NEW.file_name,
        NEW.content_type,
        NEW.size_bytes,
        NEW.checksum
    ) IS DISTINCT FROM ROW(
        OLD.id,
        OLD.tenant_id,
        OLD.created_at,
        OLD.created_by,
        OLD.operation_id,
        OLD.purpose,
        OLD.file_owner,
        OLD.file_object_id,
        OLD.file_name,
        OLD.content_type,
        OLD.size_bytes,
        OLD.checksum
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'metadata artifact content is immutable';
    END IF;

    IF OLD.state IN ('expired', 'deleted') AND NEW.state IN ('ready', 'staged') THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'metadata artifact lifecycle cannot move backward';
    END IF;
    RETURN NEW;
END;
$$;

-- Sort after trg_metadata_artifacts_tenant_immutable so the tenant-specific
-- guard reports the direct tenant reassignment invariant first.
CREATE TRIGGER trg_metadata_artifacts_z_content_immutable
    BEFORE UPDATE ON metadata_artifacts
    FOR EACH ROW EXECUTE FUNCTION protect_metadata_artifact_contents_and_lifecycle();

COMMENT ON TABLE metadata_operations IS
    'Durable metadata operation state and non-secret result shape only; worker execution and API acceptance are separate deliverables.';
COMMENT ON COLUMN metadata_operations.authorized_selection IS
    'Frozen authorized selection, not a reusable authorization token; reauthorize on execution and read.';
COMMENT ON COLUMN metadata_operations.checkpoint IS
    'Versioned, non-secret worker checkpoint; must not contain passwords, tokens, or signed URLs.';
COMMENT ON COLUMN metadata_operations.result IS
    'Non-secret Operation.result shape; large results are referenced through metadata_artifacts.';
COMMENT ON COLUMN metadata_operations.failure IS
    'Redacted Problem shape; failed operations require a value.';
COMMENT ON COLUMN metadata_operation_items.desired_document IS
    'Versioned recovery input only; contains no password, token, credential material, or signed URL.';
COMMENT ON COLUMN metadata_operation_items.owner_receipt IS
    'Formal external-owner receipt, not a mirror of current runtime state.';
COMMENT ON COLUMN metadata_artifacts.file_object_id IS
    'Opaque stable file-owner reference, never a signed URL or client filesystem path.';
