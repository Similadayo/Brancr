-- +migrate Up
ALTER TABLE scheduled_posts
  ADD COLUMN IF NOT EXISTS enhance_caption BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN scheduled_posts.enhance_caption IS 'If true, enhance existing caption with AI (Mode 3); if false, use caption as-is (Mode 2)';

-- +migrate Down
ALTER TABLE scheduled_posts DROP COLUMN IF EXISTS enhance_caption;
