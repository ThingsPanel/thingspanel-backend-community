-- The platformconfig owner adopts the existing public dictionary tables.
-- Historical 1.sql/4.sql and their sys_version ledger remain unchanged.
-- No data is copied, reseeded, truncated, or silently reshaped by this step.
-- This file runs only through the existing owner migration transaction/locks.
CREATE FUNCTION public.platformconfig_dictionary_schema_ready()
RETURNS boolean
LANGUAGE sql
STABLE
SET search_path = pg_catalog, public
AS $dictionary_schema$
WITH expected_columns(table_name,column_name,data_type,character_length,nullable,datetime_precision) AS (
  VALUES
    ('sys_dict','id','character varying',36,'NO',0),
    ('sys_dict','dict_code','character varying',36,'NO',0),
    ('sys_dict','dict_value','character varying',255,'NO',0),
    ('sys_dict','created_at','timestamp with time zone',0,'NO',6),
    ('sys_dict','remark','character varying',255,'YES',0),
    ('sys_dict_language','id','character varying',36,'NO',0),
    ('sys_dict_language','dict_id','character varying',36,'NO',0),
    ('sys_dict_language','language_code','character varying',36,'NO',0),
    ('sys_dict_language','translation','character varying',255,'NO',0)
), expected_constraints(table_name,constraint_name,kind,key_columns,reference_table,reference_columns) AS (
  VALUES
    ('sys_dict','sys_dict_pkey','p',ARRAY['id']::text[],NULL::text,NULL::text[]),
    ('sys_dict','sys_dict_dict_code_dict_value_key','u',ARRAY['dict_code','dict_value']::text[],NULL::text,NULL::text[]),
    ('sys_dict_language','sys_dict_language_pkey','p',ARRAY['id']::text[],NULL::text,NULL::text[]),
    ('sys_dict_language','sys_dict_language_dict_id_language_code_key','u',ARRAY['dict_id','language_code']::text[],NULL::text,NULL::text[]),
    ('sys_dict_language','sys_dict_language_dict_id_fkey','f',ARRAY['dict_id']::text[],'sys_dict',ARRAY['id']::text[])
)
SELECT COALESCE(
  (SELECT count(*) = 2 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
    WHERE n.nspname='public' AND c.relname IN ('sys_dict','sys_dict_language') AND c.relkind='r' AND c.relpersistence='p' AND NOT c.relrowsecurity AND NOT c.relforcerowsecurity)
  AND NOT EXISTS (
    SELECT 1 FROM expected_columns expected
    LEFT JOIN information_schema.columns actual ON actual.table_schema='public' AND actual.table_name=expected.table_name AND actual.column_name=expected.column_name
    WHERE actual.column_name IS NULL OR actual.data_type<>expected.data_type
      OR COALESCE(actual.character_maximum_length,0)<>expected.character_length
      OR actual.is_nullable<>expected.nullable OR COALESCE(actual.datetime_precision,0)<>expected.datetime_precision
      OR actual.column_default IS NOT NULL OR actual.is_identity<>'NO' OR actual.is_generated<>'NEVER'
  )
  AND NOT EXISTS (
    SELECT 1 FROM information_schema.columns actual
    LEFT JOIN expected_columns expected ON expected.table_name=actual.table_name AND expected.column_name=actual.column_name
    WHERE actual.table_schema='public' AND actual.table_name IN ('sys_dict','sys_dict_language') AND expected.column_name IS NULL
  )
  AND NOT EXISTS (
    SELECT 1 FROM expected_constraints expected
    LEFT JOIN pg_catalog.pg_constraint actual ON actual.conrelid=pg_catalog.to_regclass('public.'||expected.table_name) AND actual.conname=expected.constraint_name
    WHERE actual.oid IS NULL OR actual.contype::text<>expected.kind OR NOT actual.convalidated OR actual.condeferrable
      OR ARRAY(SELECT attribute.attname::text FROM unnest(actual.conkey) WITH ORDINALITY AS key(attnum,position)
        JOIN pg_catalog.pg_attribute attribute ON attribute.attrelid=actual.conrelid AND attribute.attnum=key.attnum ORDER BY key.position)<>expected.key_columns
      OR (expected.kind IN ('p','u') AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_index idx WHERE idx.indexrelid=actual.conindid AND idx.indisunique AND idx.indisvalid AND idx.indisready))
      OR (expected.kind='f' AND (
        actual.confrelid<>pg_catalog.to_regclass('public.'||expected.reference_table) OR actual.confdeltype<>'c' OR actual.confupdtype<>'a' OR actual.confmatchtype<>'s'
        OR ARRAY(SELECT attribute.attname::text FROM unnest(actual.confkey) WITH ORDINALITY AS key(attnum,position)
          JOIN pg_catalog.pg_attribute attribute ON attribute.attrelid=actual.confrelid AND attribute.attnum=key.attnum ORDER BY key.position)<>expected.reference_columns
      ))
  ),false);
$dictionary_schema$;

DO $dictionary_adoption$
BEGIN
  IF NOT public.platformconfig_dictionary_schema_ready() THEN
    RAISE EXCEPTION 'platformconfig legacy dictionary schema is missing or incompatible';
  END IF;
END;
$dictionary_adoption$;
