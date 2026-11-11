-- Existing MySQL plans already retain lexical numbers. Persist the same
-- explicit encoding marker and original-plan field as the PostgreSQL owner.
-- Do not backfill unknown operations or replace their original step digests.
ALTER TABLE market_install_operations
 ADD COLUMN plan_encoding VARBINARY(40) NULL,
 ADD COLUMN plan_exact LONGTEXT NULL,
 ADD CONSTRAINT market_install_plan_encoding CHECK (plan_encoding IN ('json-number-lexeme-v1')),
 ADD CONSTRAINT market_install_plan_exact_json CHECK (plan_exact IS NULL OR JSON_VALID(plan_exact));
