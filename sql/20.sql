-- Local dashboard template library.
-- Downloading a market bundle stores reusable templates here; concrete
-- ThingsVis dashboards are created later after the user selects real devices.

CREATE TABLE IF NOT EXISTS local_dashboard_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id VARCHAR(100) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    version VARCHAR(128) NOT NULL,
    source VARCHAR(20) NOT NULL DEFAULT 'MARKET',
    status VARCHAR(20) NOT NULL DEFAULT 'READY',
    bundle_key VARCHAR(63),
    bundle_version VARCHAR(128),
    dashboard_resource_key VARCHAR(100) NOT NULL,
    thumbnail TEXT NOT NULL DEFAULT '',
    snapshot JSONB NOT NULL,
    downloaded_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_local_dashboard_template_source
        CHECK (source IN ('MARKET', 'LOCAL')),
    CONSTRAINT ck_local_dashboard_template_status
        CHECK (status IN ('READY', 'DISABLED')),
    CONSTRAINT uq_local_dashboard_market_resource
        UNIQUE (tenant_id, bundle_key, bundle_version, dashboard_resource_key)
);

CREATE TABLE IF NOT EXISTS local_dashboard_template_bindings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_template_id UUID NOT NULL
        REFERENCES local_dashboard_templates(id) ON DELETE CASCADE,
    binding_key VARCHAR(100) NOT NULL,
    display_name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    required BOOLEAN NOT NULL DEFAULT TRUE,
    allow_many BOOLEAN NOT NULL DEFAULT FALSE,
    device_template_key VARCHAR(100) NOT NULL,
    local_device_template_id VARCHAR(100) NOT NULL,
    local_device_template_name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_local_dashboard_template_binding
        UNIQUE (dashboard_template_id, binding_key)
);

CREATE TABLE IF NOT EXISTS local_dashboard_template_instances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_template_id UUID NOT NULL
        REFERENCES local_dashboard_templates(id) ON DELETE RESTRICT,
    tenant_id VARCHAR(100) NOT NULL,
    dashboard_id VARCHAR(100) NOT NULL,
    project_id VARCHAR(100) NOT NULL DEFAULT '',
    name VARCHAR(255) NOT NULL,
    device_bindings JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_local_dashboard_instance_dashboard UNIQUE (dashboard_id)
);

CREATE INDEX IF NOT EXISTS idx_local_dashboard_templates_tenant
    ON local_dashboard_templates(tenant_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_local_dashboard_templates_bundle
    ON local_dashboard_templates(bundle_key, bundle_version);
CREATE INDEX IF NOT EXISTS idx_local_dashboard_bindings_template
    ON local_dashboard_template_bindings(dashboard_template_id);
CREATE INDEX IF NOT EXISTS idx_local_dashboard_bindings_device_template
    ON local_dashboard_template_bindings(local_device_template_id);
CREATE INDEX IF NOT EXISTS idx_local_dashboard_instances_template
    ON local_dashboard_template_instances(dashboard_template_id);
CREATE INDEX IF NOT EXISTS idx_local_dashboard_instances_tenant
    ON local_dashboard_template_instances(tenant_id, created_at DESC);

COMMENT ON TABLE local_dashboard_templates IS
    'Reusable dashboard definitions downloaded from the market or created locally';
COMMENT ON TABLE local_dashboard_template_bindings IS
    'Dashboard role placeholders mapped to installed local device templates';
COMMENT ON TABLE local_dashboard_template_instances IS
    'Concrete ThingsVis dashboards created from local dashboard templates';
