CREATE TABLE custom_command_controls (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    thing_model_id UUID NOT NULL REFERENCES thing_models(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL,
    name VARCHAR(128) NOT NULL,
    description TEXT,
    content TEXT NOT NULL,
    enable_status VARCHAR(16) NOT NULL DEFAULT 'enable',
    control_type VARCHAR(32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_ccc_thing_model ON custom_command_controls(thing_model_id);
CREATE INDEX idx_ccc_tenant ON custom_command_controls(tenant_id);
