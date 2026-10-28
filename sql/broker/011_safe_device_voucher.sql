-- Existing device vouchers are TEXT and may be empty, legacy or malformed.
-- Bad rows must deny their own authentication, never abort a global auth scan.
-- Keep this immutable function's semantics fixed; future changes need a new
-- function version and a rebuilt expression index.
CREATE OR REPLACE FUNCTION broker_safe_voucher_v1(value text) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE parsed json;
BEGIN
 parsed := value::json;
 IF json_typeof(parsed)<>'object' THEN RETURN NULL; END IF;
 IF parsed->>'username' IS NULL OR EXISTS(
  SELECT 1 FROM json_each(parsed) AS j WHERE j.key NOT IN ('username','password') OR json_typeof(j.value)<>'string'
 ) OR EXISTS(
  SELECT 1 FROM json_each(parsed) GROUP BY key HAVING count(*)>1
 ) THEN RETURN NULL; END IF;
 RETURN parsed::jsonb;
EXCEPTION WHEN data_exception THEN RETURN NULL;
END $$;
-- The fixed-size digest is only an index prefilter; authentication also checks
-- complete JSONB equality, so hash collisions cannot authorize another voucher.
-- The business schema is a separate installation prerequisite. Broker-only QA
-- schemas may not have devices yet; repeat this migration after core bootstrap.
DO $$ BEGIN
 IF to_regclass('devices') IS NOT NULL THEN
  CREATE INDEX IF NOT EXISTS broker_devices_voucher_lookup_v1 ON devices(md5(broker_safe_voucher_v1(voucher)::text));
 END IF;
END $$;
