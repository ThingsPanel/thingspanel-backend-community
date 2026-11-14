CREATE TABLE IF NOT EXISTS native_runtime_credential_leases (
 connection_id varchar(80) PRIMARY KEY,
 device_id varchar(36) NOT NULL,
 tenant_id varchar(36) NOT NULL,
 broker_cell varchar(128) NOT NULL,
 broker_instance varchar(128) NOT NULL,
 voucher_sha256 char(64) NOT NULL,
 expires_at timestamptz NOT NULL,
 revoked_ack_at timestamptz
);
CREATE INDEX IF NOT EXISTS native_credential_leases_device_expiry ON native_runtime_credential_leases(device_id, expires_at);
