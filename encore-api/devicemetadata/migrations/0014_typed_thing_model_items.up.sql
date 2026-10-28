-- Preserve existing metadata while adding the typed command/event contract.
ALTER TABLE thing_model_items
    ALTER COLUMN value_type DROP NOT NULL,
    ADD COLUMN parameters JSONB;
