-- Add ai_human_mode to tenants
ALTER TABLE tenants
  ADD COLUMN ai_human_mode VARCHAR(20) NOT NULL DEFAULT 'ai';

-- Backfill existing rows (should be unnecessary due to default)
UPDATE tenants SET ai_human_mode = 'ai' WHERE ai_human_mode IS NULL;
