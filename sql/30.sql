-- Local installation request replay identity. Historical rows deliberately
-- retain an empty digest: their payload cannot be reconstructed safely.
ALTER TABLE market_bundle_installations
    ADD COLUMN IF NOT EXISTS request_digest VARCHAR(67) NOT NULL DEFAULT '';
