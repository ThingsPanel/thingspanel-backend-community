-- 0021 installed trg_tenant_id_immutable on every tenant-bearing table that
-- existed at that time. Tables introduced afterwards (device access bindings,
-- credentials, gateway memberships, operation/artifact storage) inherited the
-- tenant_id column but not the trigger, so tenant reassignment was silently
-- possible on those rows. Backfill the same guard without changing any table
-- definition, and skip tables that are already covered so this is re-runnable
-- on databases created from a later baseline.

DO $$
DECLARE
    tenant_table RECORD;
BEGIN
    FOR tenant_table IN
        SELECT n.nspname, c.relname
        FROM pg_catalog.pg_class AS c
        JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
        JOIN pg_catalog.pg_attribute AS a ON a.attrelid = c.oid
        WHERE n.nspname = current_schema()
          AND c.relkind IN ('r', 'p')
          AND a.attname = 'tenant_id'
          AND a.attnum > 0
          AND NOT a.attisdropped
          AND NOT EXISTS (
              SELECT 1
              FROM pg_catalog.pg_trigger AS tg
              WHERE tg.tgrelid = c.oid
                AND tg.tgname = 'trg_tenant_id_immutable'
                AND NOT tg.tgisinternal
          )
        ORDER BY c.relname
    LOOP
        EXECUTE format(
            'CREATE TRIGGER trg_tenant_id_immutable BEFORE UPDATE OF tenant_id ON %I.%I FOR EACH ROW EXECUTE FUNCTION reject_tenant_id_reassignment()',
            tenant_table.nspname,
            tenant_table.relname
        );
    END LOOP;
END;
$$;
