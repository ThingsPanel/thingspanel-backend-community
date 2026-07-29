-- Migration: Create backend installation tables for Market Solution Bundle v1
-- This tracks local installation state and resource mappings

-- Table: market_bundle_installations (local side, complements market-side record)
-- Tracks installation state machine: DOWNLOADED -> VERIFIED -> MODELS_INSTALLED -> DASHBOARDS_CREATED -> WAITING_FOR_BINDINGS/COMPLETED
CREATE TABLE IF NOT EXISTS market_bundle_installations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key VARCHAR(255) NOT NULL,
    bundle_key VARCHAR(63) NOT NULL,
    bundle_version VARCHAR(128) NOT NULL,
    tenant_id VARCHAR(100) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'DOWNLOADING',
    -- State machine: DOWNLOADING, DOWNLOADED, VERIFIED, MODELS_INSTALLED, DASHBOARDS_CREATED, WAITING_FOR_BINDINGS, COMPLETED, FAILED, COMPENSATION_REQUIRED
    error_code VARCHAR(50),
    error_message TEXT,
    warnings JSONB DEFAULT '[]',
    -- Progress tracking
    downloaded_at TIMESTAMP WITH TIME ZONE,
    verified_at TIMESTAMP WITH TIME ZONE,
    models_installed_at TIMESTAMP WITH TIME ZONE,
    dashboards_created_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_installation_idempotency_tenant UNIQUE (idempotency_key, tenant_id)
);

-- Table: market_resource_mappings (maps market resource keys to local IDs)
-- Enables idempotent re-installation and rollback
CREATE TABLE IF NOT EXISTS market_resource_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL REFERENCES market_bundle_installations(id) ON DELETE CASCADE,
    tenant_id VARCHAR(100) NOT NULL,
    resource_type VARCHAR(30) NOT NULL, -- 'device_template', 'dashboard', 'device_config'
    market_resource_key VARCHAR(100) NOT NULL, -- resourceKey from bundle manifest
    market_version VARCHAR(128) NOT NULL,
    local_id VARCHAR(100) NOT NULL, -- Local ID after installation
    local_name VARCHAR(255), -- For display purposes
    status VARCHAR(20) NOT NULL DEFAULT 'active', -- active, superseded, deleted
    metadata JSONB, -- Extra info like binding status
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_resource_mapping UNIQUE (installation_id, resource_type, market_resource_key)
);

-- Table: market_bundle_binding_status (tracks device binding status for dashboards)
CREATE TABLE IF NOT EXISTS market_bundle_binding_status (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL REFERENCES market_bundle_installations(id) ON DELETE CASCADE,
    binding_key VARCHAR(100) NOT NULL, -- bindingKey from dashboard deviceBindings
    device_template_key VARCHAR(100) NOT NULL,
    required BOOLEAN NOT NULL DEFAULT true,
    local_device_id VARCHAR(100), -- Bound local device ID (null if not yet bound)
    bound_at TIMESTAMP WITH TIME ZONE,
    status VARCHAR(20) NOT NULL DEFAULT 'pending', -- pending, bound, unbound, failed
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_binding_key UNIQUE (installation_id, binding_key)
);

-- Table: market_installation_audit (audit trail for installations)
CREATE TABLE IF NOT EXISTS market_installation_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL,
    tenant_id VARCHAR(100) NOT NULL,
    action VARCHAR(50) NOT NULL, -- state_change, resource_created, resource_deleted, compensation, retry
    prev_state VARCHAR(30),
    new_state VARCHAR(30),
    resource_type VARCHAR(30),
    resource_key VARCHAR(100),
    local_id VARCHAR(100),
    details JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_installations_tenant ON market_bundle_installations(tenant_id);
CREATE INDEX IF NOT EXISTS idx_installations_status ON market_bundle_installations(status);
CREATE INDEX IF NOT EXISTS idx_installations_bundle ON market_bundle_installations(bundle_key, bundle_version);
CREATE INDEX IF NOT EXISTS idx_installations_created ON market_bundle_installations(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_mappings_tenant ON market_resource_mappings(tenant_id);
CREATE INDEX IF NOT EXISTS idx_mappings_local_id ON market_resource_mappings(local_id);
CREATE INDEX IF NOT EXISTS idx_mappings_resource_type ON market_resource_mappings(resource_type);

CREATE INDEX IF NOT EXISTS idx_bindings_installation ON market_bundle_binding_status(installation_id);
CREATE INDEX IF NOT EXISTS idx_bindings_device ON market_bundle_binding_status(local_device_id);

CREATE INDEX IF NOT EXISTS idx_audit_installation ON market_installation_audit(installation_id);
CREATE INDEX IF NOT EXISTS idx_audit_tenant ON market_installation_audit(tenant_id);
CREATE INDEX IF NOT EXISTS idx_audit_created ON market_installation_audit(created_at DESC);

-- Comments
COMMENT ON TABLE market_bundle_installations IS 'Local installation state machine for market bundles';
COMMENT ON TABLE market_resource_mappings IS 'Maps market resource keys to local IDs after installation';
COMMENT ON TABLE market_bundle_binding_status IS 'Tracks device binding status for dashboard resources';
COMMENT ON TABLE market_installation_audit IS 'Audit trail for installation state changes and resource operations';
