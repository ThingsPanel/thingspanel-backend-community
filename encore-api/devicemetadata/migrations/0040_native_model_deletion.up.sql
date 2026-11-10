-- Deleted catalog entries release their names; published snapshots stay immutable.
ALTER TABLE thing_models DROP CONSTRAINT thing_models_tenant_id_name_key;
CREATE UNIQUE INDEX thing_models_active_tenant_name
    ON thing_models (tenant_id, name) WHERE deleted_at IS NULL;
