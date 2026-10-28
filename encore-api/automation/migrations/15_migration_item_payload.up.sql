-- A31 follow-up: the two columns that let an alarm be adopted.
--
-- 14_migrations.up.sql creates auto_migration_items with episode_id and payload
-- already in it. This file exists because a development database may have run an
-- earlier revision of 14 (before those columns were added), and Encore will not
-- re-run a migration that is already recorded. Without this, a dev database would
-- start fine and then fail on the first migration item with
-- "column episode_id does not exist" — the kind of error that only shows up on
-- the machine of whoever tries the feature first.
--
-- ADD COLUMN IF NOT EXISTS makes this a no-op on a fresh database, so the two
-- files are safe to run in either order.

ALTER TABLE auto_migration_items ADD COLUMN IF NOT EXISTS episode_id text;
ALTER TABLE auto_migration_items ADD COLUMN IF NOT EXISTS payload jsonb;

-- The apply loop and the rollback loop both filter by status; the existing
-- (migration_id, status, legacy_id) index already covers that. This one covers
-- "everything one run produced, newest first" for the report endpoint.
CREATE INDEX IF NOT EXISTS auto_migration_items_kind
    ON auto_migration_items (migration_id, kind, status);
