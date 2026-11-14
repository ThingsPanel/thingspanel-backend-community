ALTER TABLE native_runtime_registrations ADD COLUMN IF NOT EXISTS deleting boolean NOT NULL DEFAULT false;
ALTER TABLE native_runtime_registrations ADD COLUMN IF NOT EXISTS lifecycle_generation bigint NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS native_runtime_lifecycle_events (
 event_id uuid PRIMARY KEY,
 device_id varchar(36) NOT NULL,
 request_sha256 char(64) NOT NULL,
 generation bigint NOT NULL,
 applied boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now()
);
