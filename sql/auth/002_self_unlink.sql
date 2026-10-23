-- Retain identity ownership after voluntary unlink. Administrative disable must
-- never be bypassed through the self-service rebind path.
ALTER TABLE auth_external_bindings ADD COLUMN IF NOT EXISTS self_unlinked boolean NOT NULL DEFAULT false;
