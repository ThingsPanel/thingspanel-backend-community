CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE thing_models (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    name VARCHAR(128) NOT NULL,
    description_i18n JSONB,
    category VARCHAR(64),
    status VARCHAR(16) NOT NULL DEFAULT 'DRAFT',
    current_snapshot_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID,
    deleted_at TIMESTAMPTZ,
    UNIQUE (tenant_id, name)
);

CREATE TABLE thing_model_items (
    id UUID PRIMARY KEY,
    thing_model_id UUID NOT NULL REFERENCES thing_models(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL,
    type VARCHAR(16) NOT NULL,
    identifier VARCHAR(64) NOT NULL,
    name_i18n JSONB NOT NULL,
    description_i18n JSONB,
    value_type JSONB NOT NULL,
    access JSONB NOT NULL,
    web_chart_config JSONB,
    app_chart_config JSONB,
    meta_items JSONB NOT NULL DEFAULT '[]'::jsonb,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (thing_model_id, identifier)
);
CREATE INDEX idx_tmi_thing_model ON thing_model_items(thing_model_id);
CREATE INDEX idx_tmi_tenant ON thing_model_items(tenant_id);
CREATE INDEX idx_tmi_type ON thing_model_items(thing_model_id, type);

CREATE TABLE thing_model_snapshots (
    id UUID PRIMARY KEY,
    thing_model_id UUID NOT NULL REFERENCES thing_models(id),
    tenant_id UUID NOT NULL,
    version INT NOT NULL,
    content JSONB NOT NULL,
    changelog TEXT,
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_by UUID NOT NULL,
    UNIQUE (thing_model_id, version)
);
CREATE OR REPLACE FUNCTION fn_snap_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'thing_model_snapshots is immutable';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_snap_no_update BEFORE UPDATE ON thing_model_snapshots
    FOR EACH ROW EXECUTE FUNCTION fn_snap_immutable();
CREATE TRIGGER trg_snap_no_delete BEFORE DELETE ON thing_model_snapshots
    FOR EACH ROW EXECUTE FUNCTION fn_snap_immutable();

CREATE TABLE device_templates (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    name VARCHAR(128) NOT NULL,
    brand VARCHAR(64),
    product_name VARCHAR(128),
    model_number VARCHAR(64),
    thing_model_id UUID NOT NULL REFERENCES thing_models(id),
    thing_model_snapshot_id UUID NOT NULL REFERENCES thing_model_snapshots(id),
    protocol_type VARCHAR(32),
    protocol_config JSONB,
    voucher_type VARCHAR(32),
    connection_type VARCHAR(8) DEFAULT 'DIRECT',
    auto_register BOOLEAN DEFAULT false,
    online_config JSONB,
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    extras JSONB NOT NULL DEFAULT '{}'::jsonb,
    description_i18n JSONB,
    image_url VARCHAR(1024),
    status VARCHAR(16) NOT NULL DEFAULT 'DRAFT',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    UNIQUE (tenant_id, name)
);
CREATE INDEX idx_dt_thing_model ON device_templates(thing_model_id);
CREATE INDEX idx_dt_tenant ON device_templates(tenant_id);
CREATE INDEX idx_dt_brand_model ON device_templates(brand, model_number);

CREATE TABLE products (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    name VARCHAR(128) NOT NULL,
    device_template_id UUID NOT NULL REFERENCES device_templates(id),
    product_key VARCHAR(64),
    sku VARCHAR(64),
    image_url VARCHAR(1024),
    ota_package_id UUID,
    description_i18n JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    UNIQUE (tenant_id, product_key)
);
CREATE INDEX idx_p_template ON products(device_template_id);

CREATE TABLE devices (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    device_template_id UUID NOT NULL REFERENCES device_templates(id),
    product_id UUID REFERENCES products(id),
    sn VARCHAR(128) NOT NULL,
    voucher VARCHAR(256),
    is_online SMALLINT NOT NULL DEFAULT 0,
    activate_flag VARCHAR(16) NOT NULL DEFAULT 'INACTIVE',
    parent_device_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    activated_at TIMESTAMPTZ,
    UNIQUE (tenant_id, sn)
);
CREATE INDEX idx_d_template ON devices(device_template_id);
CREATE INDEX idx_d_product ON devices(product_id);
CREATE INDEX idx_d_parent ON devices(parent_device_id);

CREATE TABLE meta_item_keys (
    id UUID PRIMARY KEY,
    tenant_id UUID,
    key VARCHAR(64) NOT NULL,
    label_i18n JSONB NOT NULL,
    scope VARCHAR(16) NOT NULL DEFAULT 'ALL',
    value_schema JSONB NOT NULL,
    is_builtin BOOLEAN NOT NULL DEFAULT false,
    description_i18n JSONB
);
CREATE UNIQUE INDEX uq_mik_tenant_key
    ON meta_item_keys (COALESCE(tenant_id, '00000000-0000-0000-0000-000000000000'), key);

CREATE TABLE thing_model_audit_log (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    actor_id UUID,
    action VARCHAR(64) NOT NULL,
    target_type VARCHAR(32) NOT NULL,
    target_id UUID NOT NULL,
    before JSONB,
    after JSONB,
    request_id VARCHAR(64),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_audit_tenant_time ON thing_model_audit_log(tenant_id, occurred_at DESC);
CREATE INDEX idx_audit_target ON thing_model_audit_log(target_type, target_id);
