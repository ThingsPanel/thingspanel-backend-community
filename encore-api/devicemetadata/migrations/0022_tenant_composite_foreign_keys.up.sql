-- Strengthen same-database relationships so an ID from another tenant cannot
-- satisfy a reference. Existing ON DELETE behavior is preserved.

ALTER TABLE thing_models
    ADD CONSTRAINT thing_models_tenant_id_id_unique UNIQUE (tenant_id, id);

ALTER TABLE thing_model_snapshots
    ADD CONSTRAINT thing_model_snapshots_tenant_model_id_unique UNIQUE (tenant_id, thing_model_id, id);

ALTER TABLE device_templates
    ADD CONSTRAINT device_templates_tenant_id_id_unique UNIQUE (tenant_id, id);

ALTER TABLE products
    ADD CONSTRAINT products_tenant_id_id_unique UNIQUE (tenant_id, id);

-- Keep historical single-column delete actions while making tenant/model part
-- of the relation key.
ALTER TABLE thing_model_items
    DROP CONSTRAINT thing_model_items_thing_model_id_fkey,
    ADD CONSTRAINT thing_model_items_tenant_model_fk
        FOREIGN KEY (tenant_id, thing_model_id)
        REFERENCES thing_models (tenant_id, id)
        ON DELETE CASCADE;

ALTER TABLE thing_model_snapshots
    DROP CONSTRAINT thing_model_snapshots_thing_model_id_fkey,
    ADD CONSTRAINT thing_model_snapshots_tenant_model_fk
        FOREIGN KEY (tenant_id, thing_model_id)
        REFERENCES thing_models (tenant_id, id)
        ON DELETE NO ACTION;

ALTER TABLE device_templates
    DROP CONSTRAINT device_templates_thing_model_id_fkey,
    DROP CONSTRAINT device_templates_thing_model_snapshot_id_fkey,
    ADD CONSTRAINT device_templates_tenant_model_fk
        FOREIGN KEY (tenant_id, thing_model_id)
        REFERENCES thing_models (tenant_id, id)
        ON DELETE NO ACTION,
    ADD CONSTRAINT device_templates_tenant_model_snapshot_fk
        FOREIGN KEY (tenant_id, thing_model_id, thing_model_snapshot_id)
        REFERENCES thing_model_snapshots (tenant_id, thing_model_id, id)
        ON DELETE NO ACTION;

ALTER TABLE products
    DROP CONSTRAINT products_device_template_id_fkey,
    ADD CONSTRAINT products_tenant_template_fk
        FOREIGN KEY (tenant_id, device_template_id)
        REFERENCES device_templates (tenant_id, id)
        ON DELETE NO ACTION;

ALTER TABLE devices
    DROP CONSTRAINT devices_device_template_id_fkey,
    DROP CONSTRAINT devices_product_id_fkey,
    ADD CONSTRAINT devices_tenant_template_fk
        FOREIGN KEY (tenant_id, device_template_id)
        REFERENCES device_templates (tenant_id, id)
        ON DELETE NO ACTION,
    ADD CONSTRAINT devices_tenant_product_fk
        FOREIGN KEY (tenant_id, product_id)
        REFERENCES products (tenant_id, id)
        MATCH SIMPLE
        ON DELETE NO ACTION;

ALTER TABLE custom_command_controls
    DROP CONSTRAINT custom_command_controls_thing_model_id_fkey,
    ADD CONSTRAINT custom_command_controls_tenant_model_fk
        FOREIGN KEY (tenant_id, thing_model_id)
        REFERENCES thing_models (tenant_id, id)
        ON DELETE CASCADE;

ALTER TABLE thing_model_outbox
    DROP CONSTRAINT thing_model_outbox_model_id_fkey,
    ADD CONSTRAINT thing_model_outbox_tenant_model_fk
        FOREIGN KEY (tenant_id, model_id)
        REFERENCES thing_models (tenant_id, id)
        ON DELETE RESTRICT;

ALTER TABLE thing_model_resources
    DROP CONSTRAINT thing_model_resources_thing_model_id_fkey,
    ADD CONSTRAINT thing_model_resources_tenant_model_fk
        FOREIGN KEY (tenant_id, thing_model_id)
        REFERENCES thing_models (tenant_id, id)
        ON DELETE CASCADE;

-- The active snapshot pointer must refer to a snapshot from this same tenant
-- and model. NULL remains a valid draft/unpublished state (MATCH SIMPLE).
ALTER TABLE thing_models
    ADD CONSTRAINT thing_models_current_snapshot_tenant_model_fk
        FOREIGN KEY (tenant_id, id, current_snapshot_id)
        REFERENCES thing_model_snapshots (tenant_id, thing_model_id, id)
        MATCH SIMPLE
        ON DELETE NO ACTION;
