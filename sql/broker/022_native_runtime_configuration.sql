CREATE TABLE IF NOT EXISTS native_runtime_configurations (
 device_id varchar(36) PRIMARY KEY REFERENCES native_runtime_registrations(device_id),
 generation bigint NOT NULL CHECK (generation >= 1),
 content_digest char(64) NOT NULL,
 device_config_id varchar(36) NOT NULL,
 pending boolean NOT NULL DEFAULT false,
 applied_at timestamptz NOT NULL DEFAULT now()
);

-- Keep every historical projection protected, not only the device's latest one.
CREATE TABLE IF NOT EXISTS native_runtime_managed_configurations (
 device_config_id varchar(36) PRIMARY KEY,
 device_id varchar(36) NOT NULL REFERENCES native_runtime_registrations(device_id),
 generation bigint NOT NULL CHECK (generation >= 1),
 content_digest char(64) NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(device_id,generation)
);
CREATE OR REPLACE FUNCTION protect_native_runtime_configuration() RETURNS trigger AS $$
DECLARE old_id text; new_id text; locked_id text;
BEGIN
 IF TG_OP <> 'INSERT' THEN
  IF TG_TABLE_NAME='device_configs' THEN old_id:=OLD.id::text; ELSE old_id:=OLD.device_config_id::text; END IF;
 END IF;
 IF TG_OP <> 'DELETE' THEN
  IF TG_TABLE_NAME='device_configs' THEN new_id:=NEW.id::text; ELSE new_id:=NEW.device_config_id::text; END IF;
 END IF;
 -- Serialize registration with legacy writes which started before the managed
 -- record became visible, including INSERT waiting on a foreign key.
 FOR locked_id IN SELECT DISTINCT value FROM unnest(ARRAY[old_id,new_id]) AS ids(value) WHERE value IS NOT NULL ORDER BY value LOOP
  PERFORM pg_advisory_xact_lock(hashtextextended(locked_id,72311032));
 END LOOP;
 IF EXISTS(SELECT 1 FROM native_runtime_managed_configurations WHERE device_config_id=old_id OR device_config_id=new_id) THEN
  RAISE EXCEPTION 'native runtime configuration is immutable' USING ERRCODE='55000';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- Some isolated broker installations do not own script tables. Attach only to
-- local existing processing tables; community initialization creates all three.
DO $$
DECLARE table_name text;
BEGIN
 FOREACH table_name IN ARRAY ARRAY['device_configs','data_scripts','device_topic_mappings'] LOOP
  IF to_regclass(table_name) IS NOT NULL THEN
   EXECUTE format('DROP TRIGGER IF EXISTS native_runtime_configuration_immutable ON %I',table_name);
   EXECUTE format('CREATE TRIGGER native_runtime_configuration_immutable BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION protect_native_runtime_configuration()',table_name);
  END IF;
 END LOOP;
END;
$$;
