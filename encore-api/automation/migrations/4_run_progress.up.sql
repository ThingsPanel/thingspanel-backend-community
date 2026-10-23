-- A09: durable run progress and an execution lease.
--
-- A run used to be resumable only through a wait timer. Everything else lived in
-- the process: if the process died mid-program the run stayed "running" forever
-- and no worker ever picked it up again. Two columns fix that:
--
--   cursor_pc    the program counter that has *started*. It is written before a
--                step runs, so a crash inside a step resumes at that step and the
--                replay is deduplicated by the stable step/action identity.
--   lease_*      who is currently walking the program. A crashed worker leaves an
--                expired lease instead of an invisible lock, which is what makes
--                "resume after crash" decidable instead of a guess.

ALTER TABLE auto_runs ADD COLUMN IF NOT EXISTS cursor_pc integer NOT NULL DEFAULT 0;
ALTER TABLE auto_runs ADD COLUMN IF NOT EXISTS lease_owner text;
ALTER TABLE auto_runs ADD COLUMN IF NOT EXISTS lease_until timestamptz;
-- resume_count makes "this run was interrupted and picked up again" observable
-- instead of something an operator has to infer from timestamps.
ALTER TABLE auto_runs ADD COLUMN IF NOT EXISTS resume_count integer NOT NULL DEFAULT 0;

-- The resume scan: unfinished runs whose lease is gone.
CREATE INDEX IF NOT EXISTS auto_runs_resumable
    ON auto_runs (started_at)
    WHERE status IN ('accepted', 'running');

-- The expiry scan: unfinished runs whose budget is spent.
CREATE INDEX IF NOT EXISTS auto_runs_overdue
    ON auto_runs (deadline)
    WHERE status IN ('accepted', 'running', 'queued');

-- auto_targets.status was doing double duty: "was this device selected" and "what
-- did the dispatch return". The two cannot share a column, because the first is a
-- frozen snapshot and the second is overwritten as soon as the action runs. Once
-- it was overwritten, a resumed step saw zero selected targets and reported
-- "skipped" — a real action silently turned into a no-op. The selection now lives
-- in its own column and is never touched after the snapshot is written.
ALTER TABLE auto_targets ADD COLUMN IF NOT EXISTS selected boolean NOT NULL DEFAULT false;
UPDATE auto_targets SET selected = true WHERE status = 'selected' AND NOT selected;
