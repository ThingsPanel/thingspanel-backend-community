-- DM-DB-008: native gateway hierarchy edges. Legacy parent_device_id remains
-- separate for offline reconciliation and is never read as a fallback.

CREATE TABLE gateway_memberships (
    tenant_id UUID NOT NULL,
    child_device_id UUID NOT NULL,
    gateway_device_id UUID NOT NULL,
    subdevice_address VARCHAR(128) COLLATE "C" NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by VARCHAR(128) NOT NULL,

    CONSTRAINT gateway_memberships_pk
        PRIMARY KEY (tenant_id, child_device_id),
    CONSTRAINT gateway_memberships_child_fk
        FOREIGN KEY (tenant_id, child_device_id)
        REFERENCES devices (tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT gateway_memberships_gateway_fk
        FOREIGN KEY (tenant_id, gateway_device_id)
        REFERENCES devices (tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT gateway_memberships_not_self
        CHECK (gateway_device_id <> child_device_id),
    CONSTRAINT gateway_memberships_address_valid
        CHECK (
            length(subdevice_address) BETWEEN 1 AND 128
            AND subdevice_address = btrim(
                subdevice_address,
                ' ' || chr(9) || chr(10) || chr(11) || chr(12) || chr(13) || chr(133) || chr(160)
                || chr(5760) || chr(8192) || chr(8193) || chr(8194) || chr(8195) || chr(8196)
                || chr(8197) || chr(8198) || chr(8199) || chr(8200) || chr(8201) || chr(8202)
                || chr(8232) || chr(8233) || chr(8239) || chr(8287) || chr(12288)
            )
            AND subdevice_address !~ '[[:cntrl:]]'
            AND subdevice_address !~ ('[' || chr(128) || '-' || chr(159) || ']')
        ),
    CONSTRAINT gateway_memberships_updated_by_nonempty
        CHECK (length(btrim(updated_by)) > 0),
    CONSTRAINT gateway_memberships_parent_address_unique
        UNIQUE (tenant_id, gateway_device_id, subdevice_address)
);

CREATE INDEX idx_gateway_memberships_gateway_child
    ON gateway_memberships (tenant_id, gateway_device_id, child_device_id);

CREATE FUNCTION reject_gateway_membership_identity_reassignment()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.child_device_id IS DISTINCT FROM OLD.child_device_id THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'gateway membership identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_gateway_memberships_identity_immutable
    BEFORE UPDATE OF tenant_id, child_device_id ON gateway_memberships
    FOR EACH ROW EXECUTE FUNCTION reject_gateway_membership_identity_reassignment();

CREATE FUNCTION update_gateway_memberships_updated_at()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF (to_jsonb(NEW) - 'updated_at') IS DISTINCT FROM (to_jsonb(OLD) - 'updated_at')
       OR NEW.updated_at IS DISTINCT FROM OLD.updated_at THEN
        NEW.updated_at := NOW();
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_gateway_memberships_updated_at
    BEFORE UPDATE ON gateway_memberships
    FOR EACH ROW EXECUTE FUNCTION update_gateway_memberships_updated_at();

CREATE FUNCTION validate_gateway_membership_tree()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_tenant UUID;
    target_child UUID;
    target_gateway UUID;
    locked_device RECORD;
    locked_count INTEGER := 0;
    child_kind VARCHAR(32);
    gateway_kind VARCHAR(32);
    child_deleted_at TIMESTAMPTZ;
    gateway_deleted_at TIMESTAMPTZ;
    ancestor_depth INTEGER;
    ancestor_cycle BOOLEAN;
    ancestor_contains_child BOOLEAN;
    subtree_depth INTEGER;
    subtree_cycle BOOLEAN;
BEGIN
    IF TG_OP = 'DELETE' THEN
        target_tenant := OLD.tenant_id;
        target_child := OLD.child_device_id;
        target_gateway := OLD.gateway_device_id;
    ELSE
        target_tenant := NEW.tenant_id;
        target_child := NEW.child_device_id;
        target_gateway := NEW.gateway_device_id;
    END IF;

    -- Serialize every edge mutation in one tenant before inspecting the tree.
    PERFORM pg_advisory_xact_lock(
        hashtextextended('device-gateway-tree:' || target_tenant::text, 72311028)
    );

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;

    IF target_child = target_gateway THEN
        RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'gateway membership cannot be self-referential';
    END IF;

    -- Lock both endpoint devices in stable ID order, preventing their live/kind
    -- state from changing between validation and the edge write.
    FOR locked_device IN
        SELECT id, device_kind, deleted_at
        FROM devices
        WHERE tenant_id = target_tenant
          AND id IN (target_child, target_gateway)
        ORDER BY id
        FOR SHARE
    LOOP
        locked_count := locked_count + 1;
        IF locked_device.id = target_child THEN
            child_kind := locked_device.device_kind;
            child_deleted_at := locked_device.deleted_at;
        ELSE
            gateway_kind := locked_device.device_kind;
            gateway_deleted_at := locked_device.deleted_at;
        END IF;
    END LOOP;

    -- Let the composite foreign keys return the ordinary FK violation when an
    -- endpoint is missing or belongs to another tenant.
    IF locked_count <> 2 THEN
        RETURN NEW;
    END IF;

    IF gateway_deleted_at IS NOT NULL OR gateway_kind IS DISTINCT FROM 'gateway' THEN
        RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'membership parent must be a live gateway';
    END IF;
    IF child_deleted_at IS NOT NULL OR child_kind IS NULL OR child_kind NOT IN ('gateway', 'subdevice') THEN
        RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'membership child must be a live gateway or subdevice';
    END IF;

    WITH RECURSIVE ancestors(device_id, depth, path, cycle) AS (
        SELECT target_gateway, 1, ARRAY[target_gateway]::UUID[], FALSE
        UNION ALL
        SELECT membership.gateway_device_id,
               ancestors.depth + 1,
               ancestors.path || membership.gateway_device_id,
               membership.gateway_device_id = ANY(ancestors.path)
        FROM ancestors
        JOIN gateway_memberships AS membership
          ON membership.tenant_id = target_tenant
         AND membership.child_device_id = ancestors.device_id
        WHERE NOT ancestors.cycle AND ancestors.depth < 34
    )
    SELECT COALESCE(MAX(depth), 0), COALESCE(bool_or(cycle), FALSE),
           COALESCE(bool_or(target_child = ANY(path)), FALSE)
      INTO ancestor_depth, ancestor_cycle, ancestor_contains_child
    FROM ancestors;

    WITH RECURSIVE descendants(device_id, depth, path, cycle) AS (
        SELECT target_child, 1, ARRAY[target_child]::UUID[], FALSE
        UNION ALL
        SELECT membership.child_device_id,
               descendants.depth + 1,
               descendants.path || membership.child_device_id,
               membership.child_device_id = ANY(descendants.path)
        FROM descendants
        JOIN gateway_memberships AS membership
          ON membership.tenant_id = target_tenant
         AND membership.gateway_device_id = descendants.device_id
        WHERE NOT descendants.cycle AND descendants.depth < 34
    )
    SELECT COALESCE(MAX(depth), 0), COALESCE(bool_or(cycle), FALSE)
      INTO subtree_depth, subtree_cycle
    FROM descendants;

    IF ancestor_cycle OR subtree_cycle OR ancestor_depth >= 34 OR subtree_depth >= 34 THEN
        RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'gateway hierarchy contains a cycle or exceeds depth 32';
    END IF;
    IF ancestor_contains_child THEN
        RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'gateway hierarchy cannot contain a cycle';
    END IF;
    IF ancestor_depth + subtree_depth > 32 THEN
        RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'gateway hierarchy depth cannot exceed 32';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_gateway_memberships_validate_tree
    BEFORE INSERT OR UPDATE OR DELETE ON gateway_memberships
    FOR EACH ROW EXECUTE FUNCTION validate_gateway_membership_tree();

COMMENT ON TABLE gateway_memberships IS
    'Native tenant-scoped gateway hierarchy edges; legacy devices.parent_device_id is not a runtime fallback.';
COMMENT ON COLUMN gateway_memberships.subdevice_address IS
    'Tenant-local child address under the gateway; C collation preserves exact address identity.';

CREATE FUNCTION enforce_gateway_memberships_after_device_state_change()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    current_kind VARCHAR(32);
    current_deleted_at TIMESTAMPTZ;
BEGIN
    IF NEW.device_kind IS NOT DISTINCT FROM OLD.device_kind
       AND NEW.deleted_at IS NOT DISTINCT FROM OLD.deleted_at THEN
        RETURN NULL;
    END IF;

    PERFORM pg_advisory_xact_lock(
        hashtextextended('device-gateway-tree:' || NEW.tenant_id::text, 72311028)
    );

    -- This deferred constraint trigger checks the final row and final edge set
    -- at transaction end. Callers can unbind before a same-transaction state change.
    SELECT device_kind, deleted_at
      INTO current_kind, current_deleted_at
    FROM devices
    WHERE tenant_id = NEW.tenant_id AND id = NEW.id;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM gateway_memberships AS membership
        WHERE membership.tenant_id = NEW.tenant_id
          AND (
              (membership.gateway_device_id = NEW.id
               AND (current_deleted_at IS NOT NULL OR current_kind IS DISTINCT FROM 'gateway'))
              OR
              (membership.child_device_id = NEW.id
               AND (current_deleted_at IS NOT NULL OR current_kind IS NULL OR current_kind NOT IN ('gateway', 'subdevice')))
          )
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'device state change would invalidate a gateway membership';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER trg_devices_gateway_membership_state
    AFTER UPDATE ON devices
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION enforce_gateway_memberships_after_device_state_change();
