-- +migrate Up
CREATE TABLE IF NOT EXISTS negotiation_histories (
  id BIGSERIAL PRIMARY KEY,
  sales_conversation_id INTEGER,
  type VARCHAR(50),
  customer_offer DOUBLE PRECISION,
  counter_offer DOUBLE PRECISION,
  final_agreement DOUBLE PRECISION,
  status VARCHAR(50),
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_negotiation_history_sales_conv ON negotiation_histories(sales_conversation_id);

-- +migrate Down
DROP TABLE IF EXISTS negotiation_histories;
