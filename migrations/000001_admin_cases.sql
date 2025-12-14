-- +migrate Up
CREATE TABLE admin_cases (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  source TEXT NOT NULL,        -- support | escalation | billing | compliance | security
  status TEXT NOT NULL,        -- open | investigating | waiting | resolved | closed
  reason TEXT NOT NULL,
  created_by TEXT NOT NULL,    -- admin email
  assigned_to TEXT,            -- admin email
  created_at TIMESTAMPTZ DEFAULT now(),
  closed_at TIMESTAMPTZ
);

-- +migrate Down
DROP TABLE IF EXISTS admin_cases;
