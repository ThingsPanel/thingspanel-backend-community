-- TEST ONLY: synthetic fixtures insert devices directly instead of going
-- through develop's DAL reservation CAS. Keep their identity rows in the same
-- seed transaction. Dedicated lifecycle tests additionally install actual SQL31.
CREATE TABLE IF NOT EXISTS device_id_reservations (
 id text PRIMARY KEY, tenant_id text NOT NULL,
 state varchar(16) NOT NULL CHECK(state IN ('active','tombstoned')),
 created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
 deleted_at timestamptz,
 CHECK((state='active' AND deleted_at IS NULL) OR (state='tombstoned' AND deleted_at IS NOT NULL))
);
INSERT INTO device_id_reservations(id,tenant_id,state)
 SELECT id,tenant_id,'active' FROM devices ON CONFLICT(id) DO NOTHING;
CREATE OR REPLACE FUNCTION broker_fixture_register_identity() RETURNS trigger
LANGUAGE plpgsql SET search_path FROM CURRENT AS $$
BEGIN
 INSERT INTO device_id_reservations(id,tenant_id,state)
 VALUES(NEW.id,NEW.tenant_id,'active') ON CONFLICT(id) DO NOTHING;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS broker_fixture_register_identity ON devices;
CREATE TRIGGER broker_fixture_register_identity AFTER INSERT ON devices
 FOR EACH ROW EXECUTE FUNCTION broker_fixture_register_identity();
