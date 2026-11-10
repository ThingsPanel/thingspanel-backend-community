CREATE TABLE provider_publication_events (
    id UUID PRIMARY KEY,
    provider_id UUID NOT NULL,
    version TEXT NOT NULL,
    actor_id VARCHAR(128) NOT NULL CHECK (octet_length(actor_id) BETWEEN 1 AND 128 AND btrim(actor_id) <> '' AND actor_id !~ '[[:cntrl:]]'),
    request_id VARCHAR(128) NOT NULL CHECK (octet_length(request_id) BETWEEN 1 AND 128 AND btrim(request_id) <> '' AND request_id !~ '[[:cntrl:]]'),
    schema_digest CHAR(64) NOT NULL CHECK (schema_digest ~ '^[0-9a-f]{64}$'),
    artifact_digest VARCHAR(71) NOT NULL CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (provider_id, version),
    FOREIGN KEY (provider_id, version) REFERENCES provider_releases(provider_id, version) ON DELETE RESTRICT
);

CREATE TRIGGER provider_publication_events_reject_update_delete
    BEFORE UPDATE OR DELETE ON provider_publication_events
    FOR EACH ROW EXECUTE FUNCTION reject_provider_release_mutation();

CREATE TRIGGER provider_publication_events_reject_truncate
    BEFORE TRUNCATE ON provider_publication_events
    FOR EACH STATEMENT EXECUTE FUNCTION reject_provider_release_mutation();
