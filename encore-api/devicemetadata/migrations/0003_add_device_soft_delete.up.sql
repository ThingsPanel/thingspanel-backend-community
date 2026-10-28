-- Add soft delete support to devices table
ALTER TABLE devices ADD COLUMN deleted_at TIMESTAMPTZ;

-- Update devices table index to include soft-delete filtering
CREATE INDEX idx_d_tenant_active ON devices(tenant_id) WHERE deleted_at IS NULL;
