-- Additive A08 owner migration. Never rewrite old jsonb values or step hashes.
-- NULL encoding + NULL exact marks a legacy row; execution must validate
-- every historical step identity/digest before adopting its observable plan.
ALTER TABLE market_install_operations
 ADD COLUMN plan_encoding text,
 ADD COLUMN plan_exact text,
 ADD CONSTRAINT market_install_plan_encoding CHECK (plan_encoding IN ('json-number-lexeme-v1')),
 ADD CONSTRAINT market_install_plan_exact_json CHECK (plan_exact IS NULL OR jsonb_typeof(plan_exact::jsonb) = 'object');
