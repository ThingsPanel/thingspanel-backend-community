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

-- Menu changes verified from the community database (2026-07-21 to 2026-08-04).
-- No menu records from sql/1.sql through sql/18.sql are absent from the database.
INSERT INTO public.sys_ui_elements
    (id, parent_id, element_code, element_type, orders, param1, param2, param3, authority, description, created_at, remark, multilingual, route_path)
VALUES
    ('f9ee1a58-d3c1-e5e6-1c12-d142442b7ece', '95e2a961-382b-f4a6-87b3-1898123c95bc', 'visualization_thingsvis-template', 3, 1, '/visualization/thingsvis-template', 'clarity:plugin-line', '0', '["TENANT_ADMIN","SYS_ADMIN"]'::json, '看板模板', '2026-07-31 11:50:15.995616+08', '', 'route.visualization-thingsvis-template', ''),
    ('8a2aab63-13a3-81eb-4b41-8067c133ccf2', '0', 'resource-hub', 1, 116, '/resource-hub', 'icon-park-outline:data-server', '0', '["TENANT_ADMIN","SYS_ADMIN"]'::json, '资源中心', '2026-07-31 16:56:02.283589+08', '', 'route.resource-hub', ''),
    ('c645e229-76a5-c4f5-8dd1-9d2c49f744ad', '8a2aab63-13a3-81eb-4b41-8067c133ccf2', 'resource-hub_device', 3, 2, '/resource-hub/device-template', 'mdi:monitor-dashboard', '0', '["SYS_ADMIN","TENANT_ADMIN"]'::json, '设备模板', '2026-07-31 17:42:40.329918+08', '', 'route.resource-hub_device', ''),
    ('33a98c3f-b2fb-0d0c-bd2b-13785d77bc12', '8a2aab63-13a3-81eb-4b41-8067c133ccf2', 'resource-hub_dashboard', 3, 1, '/resource-hub/dashboard-template', 'icon-park-outline:workbench', '0', '["SYS_ADMIN","TENANT_ADMIN"]'::json, '看板模板', '2026-07-31 17:44:11.409574+08', '', 'route.resource-hub_dashboard', '')
ON CONFLICT (id) DO NOTHING;
