-- A31 shipped auto_migration_items without a switch, because every object it
-- planned was created enabled. Automations change that: scene_automations has an
-- enabled column, and a customer who turned a 联动 off expects it to arrive off.
--
-- Default true rather than false so the rows written by the previous release —
-- and every scene, which has no switch in the old schema at all — keep the
-- meaning they already had.

ALTER TABLE auto_migration_items
    ADD COLUMN IF NOT EXISTS enabled boolean NOT NULL DEFAULT true;
