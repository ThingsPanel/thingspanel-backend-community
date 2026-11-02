-- Device groups are tenant-scoped organizational metadata. Membership never
-- grants device permissions and a device may belong to more than one group.
ALTER TABLE devices
    ADD CONSTRAINT devices_tenant_id_id_unique UNIQUE (tenant_id, id);

CREATE TABLE device_groups (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    parent_id UUID,
    name VARCHAR(128) NOT NULL CHECK (length(btrim(name)) > 0),
    description TEXT,
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
    resource_version BIGINT NOT NULL DEFAULT 1 CHECK (resource_version > 0),
    created_by VARCHAR(128) NOT NULL,
    updated_by VARCHAR(128) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    UNIQUE (tenant_id, id),
    CONSTRAINT device_groups_parent_tenant_fk
        FOREIGN KEY (tenant_id, parent_id)
        REFERENCES device_groups (tenant_id, id)
        ON DELETE RESTRICT
);

CREATE UNIQUE INDEX uq_device_groups_root_name
    ON device_groups (tenant_id, name)
    WHERE parent_id IS NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX uq_device_groups_tenant_parent_name
    ON device_groups (tenant_id, parent_id, name)
    WHERE parent_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX idx_device_groups_parent_order
    ON device_groups (tenant_id, parent_id, sort_order, id)
    WHERE deleted_at IS NULL;

CREATE TABLE device_group_members (
    tenant_id UUID NOT NULL,
    group_id UUID NOT NULL,
    device_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(128) NOT NULL,
    PRIMARY KEY (tenant_id, group_id, device_id),
    FOREIGN KEY (tenant_id, group_id)
        REFERENCES device_groups (tenant_id, id)
        ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, device_id)
        REFERENCES devices (tenant_id, id)
        ON DELETE RESTRICT
);
CREATE INDEX idx_device_group_members_device
    ON device_group_members (tenant_id, device_id, group_id);
