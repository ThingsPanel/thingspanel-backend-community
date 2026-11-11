-- Source identities are issued by the notification owner from current platform
-- plugin grants. Existing operator-issued credentials cannot bypass that gate.
CREATE TABLE notification_managed_credentials (
    credential_id TEXT PRIMARY KEY REFERENCES notification_credentials(id) ON DELETE CASCADE,
    service_fingerprint CHAR(64) NOT NULL,
 registration_id TEXT NOT NULL REFERENCES notification_plugin_registrations(id),
 grant_version BIGINT NOT NULL,
 registration_version BIGINT NOT NULL
);
UPDATE notification_credentials SET revoked_at=NOW()
WHERE purpose IN ('source','projection') AND revoked_at IS NULL;
