-- Tenant ownership is assigned from trusted context when a business row is
-- created. Moving a row across tenants would bypass resource authorization and
-- invalidate tenant-scoped references, so reject actual changes at the DB edge.
CREATE OR REPLACE FUNCTION reject_tenant_id_reassignment()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'tenant_id is immutable';
    END IF;
    RETURN NEW;
END;
$$;

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
    LOOP
        EXECUTE format(
            'CREATE TRIGGER trg_tenant_id_immutable BEFORE UPDATE OF tenant_id ON %I.%I FOR EACH ROW EXECUTE FUNCTION reject_tenant_id_reassignment()',
            tenant_table.nspname,
            tenant_table.relname
        );
    END LOOP;
END;
$$;
