-- Tenant-local device scope for human users.
-- Existing tenant users are backfilled with manage access to preserve their
-- current behavior; newly created users have no device access until an admin
-- assigns it through the user management API.
CREATE TABLE IF NOT EXISTS user_device_permissions (
    user_id VARCHAR(36) NOT NULL,
    device_id VARCHAR(36) NOT NULL,
    tenant_id VARCHAR(36) NOT NULL,
    access_level VARCHAR(16) NOT NULL CHECK (access_level IN ('read', 'manage')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, device_id),
    CONSTRAINT fk_user_device_permissions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_user_device_permissions_device FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_user_device_permissions_scope
    ON user_device_permissions (tenant_id, user_id, access_level, device_id);

INSERT INTO user_device_permissions (user_id, device_id, tenant_id, access_level)
SELECT u.id, d.id, u.tenant_id, 'manage'
FROM users u
JOIN devices d ON d.tenant_id = u.tenant_id
WHERE u.authority = 'TENANT_USER'
  AND u.tenant_id IS NOT NULL
  AND d.activate_flag = 'active'
ON CONFLICT (user_id, device_id) DO NOTHING;
