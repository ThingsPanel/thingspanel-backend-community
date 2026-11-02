-- Snapshot for this pre-31 feature baseline; merged develop tests prefer sql/31.sql.
-- Permanent device identity reservations. The transaction-level table lock
-- waits for in-flight device mutations and blocks new ones until backfill and
-- database writer fencing become visible together. External side effects and
-- pre-cutover deleted IDs remain outside this database-local proof.
LOCK TABLE devices IN SHARE ROW EXCLUSIVE MODE;
CREATE TABLE IF NOT EXISTS device_id_reservations (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    state VARCHAR(16) NOT NULL CONSTRAINT device_id_reservations_state_check CHECK (state IN ('active', 'tombstoned')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ,
    CONSTRAINT device_id_reservations_lifecycle_check CHECK ((state = 'active' AND deleted_at IS NULL) OR
           (state = 'tombstoned' AND deleted_at IS NOT NULL))
);
INSERT INTO device_id_reservations (id, tenant_id, state, created_at)
SELECT id, tenant_id, 'active', COALESCE(created_at, CURRENT_TIMESTAMP)
FROM devices
ON CONFLICT (id) DO NOTHING;
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM devices d
        LEFT JOIN device_id_reservations r ON r.id = d.id
        WHERE r.id IS NULL OR r.tenant_id <> d.tenant_id OR r.state <> 'active'
    ) THEN
        RAISE EXCEPTION 'device identity reservation conflicts with existing devices';
    END IF;
END $$;

CREATE OR REPLACE FUNCTION enforce_device_identity_fence()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path FROM CURRENT
AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.id IS DISTINCT FROM OLD.id OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
            RAISE EXCEPTION 'device identity and tenant are immutable'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'INSERT' THEN
        PERFORM 1 FROM device_id_reservations
        WHERE id = NEW.id AND tenant_id = NEW.tenant_id AND state = 'active'
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'device INSERT requires an active tenant identity reservation'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        PERFORM 1 FROM device_id_reservations
        WHERE id = OLD.id AND tenant_id = OLD.tenant_id AND state = 'tombstoned'
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'device DELETE requires a tenant identity tombstone'
                USING ERRCODE = '23514';
        END IF;
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'unsupported device identity operation';
END $$;
-- Pin resolution to the component's schema; explicitly put temporary objects
-- last so a temporary same-name table cannot satisfy the reservation check.
DO $$
BEGIN
    EXECUTE format('ALTER FUNCTION %I.enforce_device_identity_fence() SET search_path TO %I, pg_temp', current_schema(), current_schema());
END $$;
DROP TRIGGER IF EXISTS devices_identity_fence ON devices;
CREATE TRIGGER devices_identity_fence
BEFORE INSERT OR DELETE OR UPDATE OF id, tenant_id ON devices
FOR EACH ROW EXECUTE FUNCTION enforce_device_identity_fence();
