ALTER TABLE public.device_configs
    ADD COLUMN IF NOT EXISTS native_thing_model_id varchar(36),
    ADD COLUMN IF NOT EXISTS native_thing_model_snapshot_id varchar(36),
    ADD COLUMN IF NOT EXISTS native_thing_model_revision integer,
    ADD COLUMN IF NOT EXISTS native_binding_revision integer NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'device_configs_native_binding_fields_check'
          AND conrelid = 'public.device_configs'::regclass
    ) THEN
        ALTER TABLE public.device_configs
            ADD CONSTRAINT device_configs_native_binding_fields_check CHECK (
                (native_thing_model_id IS NULL
                    AND native_thing_model_snapshot_id IS NULL
                    AND native_thing_model_revision IS NULL)
                OR
                (native_thing_model_id IS NOT NULL
                    AND native_thing_model_snapshot_id IS NOT NULL
                    AND native_thing_model_revision IS NOT NULL
                    AND native_thing_model_revision > 0)
            );
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'device_configs_native_binding_revision_check'
          AND conrelid = 'public.device_configs'::regclass
    ) THEN
        ALTER TABLE public.device_configs
            ADD CONSTRAINT device_configs_native_binding_revision_check
            CHECK (native_binding_revision >= 0);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'device_configs_native_legacy_binding_exclusive_check'
          AND conrelid = 'public.device_configs'::regclass
    ) THEN
        ALTER TABLE public.device_configs
            ADD CONSTRAINT device_configs_native_legacy_binding_exclusive_check
            CHECK (native_thing_model_id IS NULL OR device_template_id IS NULL);
    END IF;
END $$;
