-- Immutable, content-addressed snapshots of validated device topic mappings.
CREATE TABLE IF NOT EXISTS public.broker_mapping_snapshots (
    version varchar(80) PRIMARY KEY,
    rules jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
