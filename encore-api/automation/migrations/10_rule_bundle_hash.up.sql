-- A21 shipped bundle_hash by editing migration 9 after that migration had
-- already been applied somewhere. Encore records the version number, not a
-- file hash, so those databases kept reporting "version 9 applied" while the
-- column did not exist: every rule read then failed with
-- `column "bundle_hash" does not exist`, which looks like a code bug and is
-- actually a migration bug.
--
-- Editing an applied migration is the one thing a migration system cannot
-- recover from — the database has already been told the change happened, and
-- nothing in the tool will ever look again. The fix is a new migration.
--
-- A fresh database gets this as a no-op (IF NOT EXISTS), so both paths converge.
ALTER TABLE auto_rules ADD COLUMN IF NOT EXISTS bundle_hash text;

-- Same reason, same shape: the import history table also gained a column after
-- it was first created in a running installation.
ALTER TABLE auto_bundle_imports ADD COLUMN IF NOT EXISTS report jsonb;
