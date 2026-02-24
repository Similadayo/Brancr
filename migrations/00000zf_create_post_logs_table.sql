-- +migrate Up
CREATE TABLE IF NOT EXISTS post_logs (
  id BIGSERIAL PRIMARY KEY,
  tenant_id INTEGER,
  scheduled_post_id INTEGER,
  platform VARCHAR(50),
  action VARCHAR(50),
  original_caption TEXT,
  optimized_caption TEXT,
  status VARCHAR(20),
  error_message TEXT,
  improvements TEXT,
  metadata JSONB,
  created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_post_logs_tenant ON post_logs(tenant_id);
CREATE INDEX IF NOT EXISTS idx_post_logs_scheduled_post ON post_logs(scheduled_post_id);

-- +migrate Down
DROP TABLE IF EXISTS post_logs;
