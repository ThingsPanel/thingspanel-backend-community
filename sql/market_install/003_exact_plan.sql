-- Add a precise representation for the existing v1 number-lexeme digest.
-- Historical plan/hash columns and their contents remain unchanged. Legacy
-- rows stay NULL until their original digest is verified by the grant owner.
ALTER TABLE market_install_authorizations
    ADD COLUMN plan_exact text,
    ADD COLUMN plan_encoding text,
    ADD CONSTRAINT market_install_authorizations_exact_plan_pair_check
        CHECK ((plan_exact IS NULL) = (plan_encoding IS NULL)),
    ADD CONSTRAINT market_install_authorizations_plan_encoding_check
        CHECK (plan_encoding IS NULL OR plan_encoding = 'json-number-lexeme-v1');
