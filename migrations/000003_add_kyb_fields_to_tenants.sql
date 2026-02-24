-- Up
ALTER TABLE tenants
  ADD COLUMN IF NOT EXISTS kyb_status VARCHAR(50) DEFAULT 'not_submitted',
  ADD COLUMN IF NOT EXISTS kyb_data JSONB DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS kyb_submitted_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS kyb_verified_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS kyb_verified_by_admin_id BIGINT NULL;

-- Down
-- ALTER TABLE tenants
--   DROP COLUMN IF EXISTS kyb_verified_by_admin_id,
--   DROP COLUMN IF EXISTS kyb_verified_at,
--   DROP COLUMN IF EXISTS kyb_submitted_at,
--   DROP COLUMN IF EXISTS kyb_data,
--   DROP COLUMN IF EXISTS kyb_status;
