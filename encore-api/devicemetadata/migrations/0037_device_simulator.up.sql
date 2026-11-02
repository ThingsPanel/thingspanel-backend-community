-- Controlled device simulator (DM-RT-019). The profile holds simulation-only
-- settings; sessions bind a run to one profile version for a limited time; runs
-- record how many messages Community accepted. No broker address, port or
-- credential is stored: Community publishes to its own configured broker.

CREATE TABLE device_simulator_profiles (
    tenant_id UUID NOT NULL,
    device_id UUID NOT NULL,
    resource_version BIGINT NOT NULL DEFAULT 1,
    kind VARCHAR(16) NOT NULL,
    payload JSONB NOT NULL,
    topic_purpose VARCHAR(16) NOT NULL,
    interval_seconds INTEGER NOT NULL,
    message_limit INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by VARCHAR(128) NOT NULL,

    PRIMARY KEY (tenant_id, device_id),
    CONSTRAINT device_simulator_profiles_device_fk
        FOREIGN KEY (tenant_id, device_id) REFERENCES devices (tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT device_simulator_profiles_version_range
        CHECK (resource_version BETWEEN 1 AND 9007199254740991),
    CONSTRAINT device_simulator_profiles_kind_valid
        CHECK (kind IN ('telemetry', 'attribute', 'event')),
    CONSTRAINT device_simulator_profiles_purpose_valid
        CHECK (topic_purpose IN ('telemetry', 'attribute', 'event')),
    CONSTRAINT device_simulator_profiles_payload_object
        CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT device_simulator_profiles_interval_range
        CHECK (interval_seconds BETWEEN 1 AND 3600),
    CONSTRAINT device_simulator_profiles_limit_range
        CHECK (message_limit BETWEEN 1 AND 1000),
    CONSTRAINT device_simulator_profiles_updated_by_nonempty
        CHECK (length(btrim(updated_by)) BETWEEN 1 AND 128)
);

CREATE TABLE device_simulation_sessions (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    device_id UUID NOT NULL,
    profile_version BIGINT NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'ready',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(128) NOT NULL,
    closed_at TIMESTAMPTZ,

    CONSTRAINT device_simulation_sessions_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT device_simulation_sessions_device_fk
        FOREIGN KEY (tenant_id, device_id) REFERENCES devices (tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT device_simulation_sessions_state_valid
        CHECK (state IN ('ready', 'closed')),
    CONSTRAINT device_simulation_sessions_closed_pair
        CHECK ((state = 'closed') = (closed_at IS NOT NULL)),
    CONSTRAINT device_simulation_sessions_version_range
        CHECK (profile_version BETWEEN 1 AND 9007199254740991),
    CONSTRAINT device_simulation_sessions_created_by_nonempty
        CHECK (length(btrim(created_by)) BETWEEN 1 AND 128)
);

CREATE INDEX idx_device_simulation_sessions_device
    ON device_simulation_sessions (tenant_id, device_id, created_at DESC);

CREATE TABLE device_simulation_runs (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    device_id UUID NOT NULL,
    session_id UUID NOT NULL,
    state VARCHAR(16) NOT NULL,
    sent INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    error JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(128) NOT NULL,

    CONSTRAINT device_simulation_runs_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT device_simulation_runs_session_fk
        FOREIGN KEY (tenant_id, session_id) REFERENCES device_simulation_sessions (tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT device_simulation_runs_device_fk
        FOREIGN KEY (tenant_id, device_id) REFERENCES devices (tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT device_simulation_runs_state_valid
        CHECK (state IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),
    CONSTRAINT device_simulation_runs_counts_nonnegative
        CHECK (sent >= 0 AND failed >= 0),
    CONSTRAINT device_simulation_runs_error_object
        CHECK (error IS NULL OR jsonb_typeof(error) = 'object'),
    CONSTRAINT device_simulation_runs_created_by_nonempty
        CHECK (length(btrim(created_by)) BETWEEN 1 AND 128)
);

CREATE INDEX idx_device_simulation_runs_device
    ON device_simulation_runs (tenant_id, device_id, created_at DESC);

COMMENT ON TABLE device_simulator_profiles IS
    'Simulation-only settings per device; never holds a broker address, port or credential.';

CREATE TRIGGER trg_tenant_id_immutable
    BEFORE UPDATE OF tenant_id ON device_simulator_profiles
    FOR EACH ROW EXECUTE FUNCTION reject_tenant_id_reassignment();
CREATE TRIGGER trg_tenant_id_immutable
    BEFORE UPDATE OF tenant_id ON device_simulation_sessions
    FOR EACH ROW EXECUTE FUNCTION reject_tenant_id_reassignment();
CREATE TRIGGER trg_tenant_id_immutable
    BEFORE UPDATE OF tenant_id ON device_simulation_runs
    FOR EACH ROW EXECUTE FUNCTION reject_tenant_id_reassignment();
