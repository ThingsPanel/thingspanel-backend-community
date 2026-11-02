-- Preserve the existing client status envelope without reconstructing request
-- metadata from opaque digests. Older operations keep explicitly empty labels.
ALTER TABLE market_install_operations ADD COLUMN bundle_key text NOT NULL DEFAULT '';
ALTER TABLE market_install_operations ADD COLUMN bundle_version text NOT NULL DEFAULT '';
