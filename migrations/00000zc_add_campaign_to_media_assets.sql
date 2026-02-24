-- +migrate Up
ALTER TABLE media_assets
  ADD COLUMN IF NOT EXISTS campaign VARCHAR(255);

CREATE INDEX IF NOT EXISTS idx_media_assets_campaign ON media_assets(campaign);

-- +migrate Down
DROP INDEX IF EXISTS idx_media_assets_campaign;
ALTER TABLE media_assets DROP COLUMN IF EXISTS campaign;
