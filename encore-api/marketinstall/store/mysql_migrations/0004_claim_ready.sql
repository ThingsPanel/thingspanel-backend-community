-- A08 queue ownership stays on the original operation row. This indexed,
-- deterministic projection avoids a filesort locking all eligible rows before
-- LIMIT 1 is applied. Paused/final operations and unknown leases are not ready.
ALTER TABLE market_install_operations
 ADD COLUMN claim_ready_at DATETIME(6) GENERATED ALWAYS AS
  (CASE state WHEN _binary'queued' THEN created_at WHEN _binary'running' THEN lease_until ELSE NULL END) STORED,
 ADD INDEX market_install_ready (claim_ready_at,id);
