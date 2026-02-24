-- +migrate Up
CREATE TABLE IF NOT EXISTS escalations (
  id BIGSERIAL PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  user_id VARCHAR(255),
  platform VARCHAR(50),
  message TEXT,
  draft_reply TEXT,
  status VARCHAR(50),
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_escalations_tenant ON escalations(tenant_id);

-- +migrate Down
DROP TABLE IF EXISTS escalations;
