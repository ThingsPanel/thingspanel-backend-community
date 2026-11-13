CREATE TABLE IF NOT EXISTS native_runtime_registrations (
 device_id varchar(36) PRIMARY KEY,
 native_tenant_id uuid NOT NULL,
 tenant_id varchar(36) NOT NULL,
 deployment_id varchar(128) NOT NULL,
 credential_id uuid NOT NULL,
 voucher_sha256 char(64) NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
