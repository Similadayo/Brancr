-- +migrate Up
-- Add blocked and suspended fields to tenants table
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS blocked BOOLEAN DEFAULT false;
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS suspended BOOLEAN DEFAULT false;

-- Add tenant_id to log_entries table for tenant-scoped logging
ALTER TABLE log_entries ADD COLUMN IF NOT EXISTS tenant_id UUID;
CREATE INDEX IF NOT EXISTS idx_log_entries_tenant_id ON log_entries(tenant_id);

-- +migrate Down
-- Remove tenant_id from log_entries
DROP INDEX IF EXISTS idx_log_entries_tenant_id;
ALTER TABLE log_entries DROP COLUMN IF EXISTS tenant_id;

-- Remove blocked and suspended from tenants
ALTER TABLE tenants DROP COLUMN IF EXISTS suspended;
ALTER TABLE tenants DROP COLUMN IF EXISTS blocked;
