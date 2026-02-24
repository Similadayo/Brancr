-- +migrate Up
-- Create pending_escalations table to replace in-memory map for persistence and multi-instance support
CREATE TABLE IF NOT EXISTS pending_escalations (
  id BIGSERIAL PRIMARY KEY,
  interaction_id INTEGER NOT NULL UNIQUE,
  tenant_id INTEGER NOT NULL,
  customer_id INTEGER NOT NULL,
  platform VARCHAR(50) NOT NULL,
  message TEXT NOT NULL,
  suggested_reply TEXT NOT NULL,
  chat_id BIGINT NOT NULL,
  intent VARCHAR(50),
  tone VARCHAR(50),
  confidence DOUBLE PRECISION,
  reason TEXT,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now(),
  deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_pending_escalations_tenant ON pending_escalations(tenant_id);
CREATE INDEX IF NOT EXISTS idx_pending_escalations_customer ON pending_escalations(customer_id);
CREATE INDEX IF NOT EXISTS idx_pending_escalations_chat ON pending_escalations(chat_id);
CREATE INDEX IF NOT EXISTS idx_pending_escalations_interaction ON pending_escalations(interaction_id);

-- +migrate Down
DROP INDEX IF EXISTS idx_pending_escalations_interaction;
DROP INDEX IF EXISTS idx_pending_escalations_chat;
DROP INDEX IF EXISTS idx_pending_escalations_customer;
DROP INDEX IF EXISTS idx_pending_escalations_tenant;
DROP TABLE IF EXISTS pending_escalations;

