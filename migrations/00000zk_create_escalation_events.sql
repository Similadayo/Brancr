-- +migrate Up
CREATE TABLE IF NOT EXISTS escalation_events (
  id serial PRIMARY KEY,
  tenant_id integer NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  sales_conversation_id integer NOT NULL REFERENCES sales_conversations(id) ON DELETE CASCADE,
  product_id integer REFERENCES products(id) ON DELETE SET NULL,
  list_price double precision NOT NULL DEFAULT 0,
  min_price double precision NOT NULL DEFAULT 0,
  customer_offers JSONB NOT NULL DEFAULT '[]'::jsonb,
  ai_offers JSONB NOT NULL DEFAULT '[]'::jsonb,
  reason varchar(255),
  status varchar(50) NOT NULL DEFAULT 'pending',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_escalation_tenant ON escalation_events (tenant_id);
CREATE INDEX IF NOT EXISTS idx_escalation_salesconv ON escalation_events (sales_conversation_id);

-- +migrate Down
DROP TABLE IF EXISTS escalation_events;
