-- Add web/app chart config columns to thing_models table.
-- These store model-level chart layout configs (ThingsVis JSON), parallel to the
-- old /device/template API's web_chart_config and app_chart_config fields.
ALTER TABLE thing_models ADD COLUMN IF NOT EXISTS web_chart_config JSONB;
ALTER TABLE thing_models ADD COLUMN IF NOT EXISTS app_chart_config JSONB;
