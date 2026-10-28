-- Preserve a source tuple's immutable native target after an explicit route
-- rollback. Legacy mode sends future events through the old sender while the
-- bound target and projection history remain available for audit/replay.
DO $$
DECLARE
    previous_checks text[];
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass(current_schema() || '.notification_source_group_routes')
          AND conname = 'notification_source_group_routes_engine_state_check'
    ) THEN
        RETURN;
    END IF;

    SELECT array_agg(conname)
      INTO previous_checks
      FROM pg_constraint
     WHERE conrelid = to_regclass(current_schema() || '.notification_source_group_routes')
       AND contype = 'c'
       AND pg_get_constraintdef(oid) LIKE '%notification_group_id%'
       AND pg_get_constraintdef(oid) LIKE '%bound_notification_group_id%'
       AND pg_get_constraintdef(oid) LIKE '%group_revision%'
       AND pg_get_constraintdef(oid) LIKE '%legacy%'
       AND pg_get_constraintdef(oid) LIKE '%encore%';

    IF coalesce(array_length(previous_checks, 1), 0) <> 1 THEN
        RAISE EXCEPTION 'expected exactly one prior source route state constraint';
    END IF;

    EXECUTE format(
        'ALTER TABLE public.notification_source_group_routes DROP CONSTRAINT %I',
        previous_checks[1]
    );

    ALTER TABLE public.notification_source_group_routes
        ADD CONSTRAINT notification_source_group_routes_engine_state_check CHECK (
            (engine = 'legacy' AND notification_group_id IS NULL AND group_revision = 0)
            OR
            (engine = 'encore' AND notification_group_id IS NOT NULL
                AND bound_notification_group_id = notification_group_id AND group_revision > 0)
        );
END
$$;
