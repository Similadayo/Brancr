-- +migrate Up
ALTER TABLE sales_conversations
  ADD COLUMN IF NOT EXISTS same_offer_count integer NOT NULL DEFAULT 0;

ALTER TABLE sales_conversations
  ADD COLUMN IF NOT EXISTS last_offer_amount double precision NOT NULL DEFAULT 0;

ALTER TABLE sales_conversations
  ADD COLUMN IF NOT EXISTS ai_concession_count integer NOT NULL DEFAULT 0;

COMMENT ON COLUMN sales_conversations.same_offer_count IS 'Number of repeated identical offers from customer in this sales conversation';
COMMENT ON COLUMN sales_conversations.last_offer_amount IS 'Last numeric offer observed from customer in this sales conversation';
COMMENT ON COLUMN sales_conversations.ai_concession_count IS 'Number of AI concession counter-offers made in this conversation';

-- +migrate Down
ALTER TABLE sales_conversations DROP COLUMN IF EXISTS same_offer_count;
ALTER TABLE sales_conversations DROP COLUMN IF EXISTS last_offer_amount;
ALTER TABLE sales_conversations DROP COLUMN IF EXISTS ai_concession_count;
