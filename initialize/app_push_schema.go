package initialize

import "gorm.io/gorm"

// Separate from legacy CID registrations: only session-bound devices participate
// in native member fanout. Existing registrations require a new App login.
func ApplyAppPushSchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		return tx.Exec(`
   SELECT pg_advisory_xact_lock(716238940);
   CREATE TABLE IF NOT EXISTS app_push_devices (
    id uuid PRIMARY KEY, tenant_id text NOT NULL, user_id text NOT NULL,
    app_id text NOT NULL, installation_id text NOT NULL, cid text NOT NULL,
    platform text NOT NULL, session_hash text NOT NULL, expires_at timestamptz NOT NULL,
    active boolean NOT NULL DEFAULT true, updated_at timestamptz NOT NULL DEFAULT now()
   );
   CREATE UNIQUE INDEX IF NOT EXISTS app_push_active_cid ON app_push_devices(app_id,cid) WHERE active;
   CREATE UNIQUE INDEX IF NOT EXISTS app_push_active_installation ON app_push_devices(app_id,installation_id) WHERE active;
   CREATE INDEX IF NOT EXISTS app_push_member ON app_push_devices(tenant_id,user_id) WHERE active;
  `).Error
	})
}
