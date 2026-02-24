-- Add ai_mode to social_accounts for per-inbox AI/human mode
ALTER TABLE social_accounts
  ADD COLUMN ai_mode VARCHAR(20);

-- No backfill - tenants can set per-inbox mode or inherit tenant setting
