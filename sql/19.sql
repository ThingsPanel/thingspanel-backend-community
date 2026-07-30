-- Market Solution Bundle installation state and resource mappings.
-- This migration is intentionally idempotent so environments that applied the
-- original orphan migration manually can safely upgrade to database version 19.

CREATE TABLE IF NOT EXISTS market_bundle_installations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key VARCHAR(255) NOT NULL,
    bundle_key VARCHAR(63) NOT NULL,
    bundle_version VARCHAR(128) NOT NULL,
    tenant_id VARCHAR(100) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'DOWNLOADING',
    error_code VARCHAR(50),
    error_message TEXT,
    warnings JSONB DEFAULT '[]',
    downloaded_at TIMESTAMP WITH TIME ZONE,
    verified_at TIMESTAMP WITH TIME ZONE,
    models_installed_at TIMESTAMP WITH TIME ZONE,
    dashboards_created_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_installation_idempotency_tenant UNIQUE (idempotency_key, tenant_id)
);

CREATE TABLE IF NOT EXISTS market_resource_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL REFERENCES market_bundle_installations(id) ON DELETE CASCADE,
    tenant_id VARCHAR(100) NOT NULL,
    resource_type VARCHAR(30) NOT NULL,
    market_resource_key VARCHAR(100) NOT NULL,
    market_version VARCHAR(128) NOT NULL,
    local_id VARCHAR(100) NOT NULL,
    local_name VARCHAR(255),
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_resource_mapping UNIQUE (installation_id, resource_type, market_resource_key)
);

CREATE TABLE IF NOT EXISTS market_bundle_binding_status (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL REFERENCES market_bundle_installations(id) ON DELETE CASCADE,
    binding_key VARCHAR(100) NOT NULL,
    device_template_key VARCHAR(100) NOT NULL,
    required BOOLEAN NOT NULL DEFAULT true,
    local_device_id VARCHAR(100),
    bound_at TIMESTAMP WITH TIME ZONE,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_binding_key UNIQUE (installation_id, binding_key)
);

CREATE TABLE IF NOT EXISTS market_installation_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL,
    tenant_id VARCHAR(100) NOT NULL,
    action VARCHAR(50) NOT NULL,
    prev_state VARCHAR(30),
    new_state VARCHAR(30),
    resource_type VARCHAR(30),
    resource_key VARCHAR(100),
    local_id VARCHAR(100),
    details JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_installations_tenant
    ON market_bundle_installations(tenant_id);
CREATE INDEX IF NOT EXISTS idx_installations_status
    ON market_bundle_installations(status);
CREATE INDEX IF NOT EXISTS idx_installations_bundle
    ON market_bundle_installations(bundle_key, bundle_version);
CREATE INDEX IF NOT EXISTS idx_installations_created
    ON market_bundle_installations(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_mappings_tenant
    ON market_resource_mappings(tenant_id);
CREATE INDEX IF NOT EXISTS idx_mappings_local_id
    ON market_resource_mappings(local_id);
CREATE INDEX IF NOT EXISTS idx_mappings_resource_type
    ON market_resource_mappings(resource_type);

CREATE INDEX IF NOT EXISTS idx_bindings_installation
    ON market_bundle_binding_status(installation_id);
CREATE INDEX IF NOT EXISTS idx_bindings_device
    ON market_bundle_binding_status(local_device_id);

CREATE INDEX IF NOT EXISTS idx_audit_installation
    ON market_installation_audit(installation_id);
CREATE INDEX IF NOT EXISTS idx_audit_tenant
    ON market_installation_audit(tenant_id);
CREATE INDEX IF NOT EXISTS idx_audit_created
    ON market_installation_audit(created_at DESC);

COMMENT ON TABLE market_bundle_installations IS
    'Local installation state machine for market bundles';
COMMENT ON TABLE market_resource_mappings IS
    'Maps market resource keys to local IDs after installation';
COMMENT ON TABLE market_bundle_binding_status IS
    'Tracks device binding status for dashboard resources';
COMMENT ON TABLE market_installation_audit IS
    'Audit trail for installation state changes and resource operations';
