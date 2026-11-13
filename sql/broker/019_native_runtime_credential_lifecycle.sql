ALTER TABLE native_runtime_registrations ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;
CREATE TABLE IF NOT EXISTS native_runtime_credential_history (
 device_id varchar(36) NOT NULL,
 credential_id uuid NOT NULL,
 PRIMARY KEY(device_id,credential_id)
);
INSERT INTO native_runtime_credential_history(device_id,credential_id)
 SELECT device_id,credential_id FROM native_runtime_registrations ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS native_runtime_credential_events (
 event_id uuid PRIMARY KEY,
 device_id varchar(36) NOT NULL,
 request_sha256 char(64) NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
