-- +migrate Up
-- Add cost control fields to tenant_quota table
-- This enables dollar-based cost tracking and limits (in addition to usage-based quotas)
ALTER TABLE tenant_quotas
ADD COLUMN IF NOT EXISTS cost_limit DOUBLE PRECISION DEFAULT 0 NOT NULL,
ADD COLUMN IF NOT EXISTS cost_used DOUBLE PRECISION DEFAULT 0 NOT NULL;

-- Add comment explaining cost fields
COMMENT ON COLUMN tenant_quotas.cost_limit IS 'Cost limit in USD per reset period (0 = unlimited)';
COMMENT ON COLUMN tenant_quotas.cost_used IS 'Current cost used in USD for this reset period';

-- Create index for cost limit queries (for admin dashboards)
CREATE INDEX IF NOT EXISTS idx_tenant_quotas_cost_limit ON tenant_quotas(cost_limit) WHERE cost_limit > 0;

-- +migrate Down
ALTER TABLE tenant_quotas
DROP COLUMN IF EXISTS cost_limit,
DROP COLUMN IF EXISTS cost_used;
DROP INDEX IF EXISTS idx_tenant_quotas_cost_limit;

