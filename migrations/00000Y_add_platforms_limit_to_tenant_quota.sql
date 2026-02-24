-- +migrate Up
ALTER TABLE tenant_quotas ADD COLUMN platforms_limit INTEGER NOT NULL DEFAULT 5;

-- +migrate Down
ALTER TABLE tenant_quotas DROP COLUMN platforms_limit;
