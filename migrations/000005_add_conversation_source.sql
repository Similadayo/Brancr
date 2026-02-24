-- Add source field to conversations table for attribution tracking
-- +goose Up
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS source VARCHAR(50);
CREATE INDEX IF NOT EXISTS idx_conversations_source ON conversations(source);

-- +goose Down
DROP INDEX IF EXISTS idx_conversations_source;
ALTER TABLE conversations DROP COLUMN IF EXISTS source;
