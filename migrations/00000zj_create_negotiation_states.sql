-- +migrate Up
CREATE TABLE IF NOT EXISTS negotiation_states (
  id serial PRIMARY KEY,
  sales_conversation_id integer NOT NULL REFERENCES sales_conversations(id) ON DELETE CASCADE,
  product_id integer REFERENCES products(id) ON DELETE SET NULL,
  status varchar(50) NOT NULL DEFAULT 'idle',
  last_customer_offer double precision DEFAULT 0,
  same_offer_count integer NOT NULL DEFAULT 0,
  ai_concession_count integer NOT NULL DEFAULT 0,
  flexibility_attempted boolean NOT NULL DEFAULT false,
  lock_ai_price_changes boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_negstate_salesconv ON negotiation_states (sales_conversation_id);
CREATE INDEX IF NOT EXISTS idx_negstate_product ON negotiation_states (product_id);

-- +migrate Down
DROP TABLE IF EXISTS negotiation_states;
