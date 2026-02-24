-- +migrate Up
-- Create bot_sessions table to replace in-memory session maps for persistence and multi-instance support

-- PostSession table for post creation flow
CREATE TABLE IF NOT EXISTS post_sessions (
  id BIGSERIAL PRIMARY KEY,
  chat_id BIGINT NOT NULL UNIQUE,
  tenant_id INTEGER NOT NULL,
  step INTEGER NOT NULL DEFAULT 0,
  media_asset_id INTEGER DEFAULT 0,
  media_type VARCHAR(50),
  is_carousel BOOLEAN DEFAULT false,
  name VARCHAR(255),
  caption TEXT,
  use_ai_caption BOOLEAN DEFAULT false,
  caption_mode VARCHAR(50),
  platforms JSONB,
  schedule_input TEXT,
  batch_schedule BOOLEAN DEFAULT false,
  batch_spacing VARCHAR(100),
  original_inputs JSONB,
  tiktok_disable_duet BOOLEAN DEFAULT false,
  tiktok_disable_stitch BOOLEAN DEFAULT false,
  tiktok_disable_comment BOOLEAN DEFAULT false,
  tiktok_schedule_time TIMESTAMPTZ,
  tiktok_options_asked BOOLEAN DEFAULT false,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now(),
  deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_post_sessions_tenant ON post_sessions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_post_sessions_expires ON post_sessions(expires_at) WHERE expires_at IS NOT NULL;

-- BusinessSession table for business profile update flow
CREATE TABLE IF NOT EXISTS business_sessions (
  id BIGSERIAL PRIMARY KEY,
  chat_id BIGINT NOT NULL UNIQUE,
  tenant_id INTEGER NOT NULL,
  mode VARCHAR(50) NOT NULL,
  step INTEGER NOT NULL DEFAULT 1,
  menu_item JSONB,
  faq JSONB,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now(),
  deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_business_sessions_tenant ON business_sessions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_business_sessions_expires ON business_sessions(expires_at) WHERE expires_at IS NOT NULL;

-- EscalationResolutionSession table for escalation resolution flow
CREATE TABLE IF NOT EXISTS escalation_resolution_sessions (
  id BIGSERIAL PRIMARY KEY,
  chat_id BIGINT NOT NULL UNIQUE,
  tenant_id INTEGER NOT NULL,
  interaction_id INTEGER NOT NULL,
  customer_id INTEGER NOT NULL,
  platform VARCHAR(50) NOT NULL,
  started_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now(),
  deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_escalation_resolution_sessions_tenant ON escalation_resolution_sessions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_escalation_resolution_sessions_interaction ON escalation_resolution_sessions(interaction_id);
CREATE INDEX IF NOT EXISTS idx_escalation_resolution_sessions_expires ON escalation_resolution_sessions(expires_at) WHERE expires_at IS NOT NULL;

-- +migrate Down
DROP INDEX IF EXISTS idx_escalation_resolution_sessions_expires;
DROP INDEX IF EXISTS idx_escalation_resolution_sessions_interaction;
DROP INDEX IF EXISTS idx_escalation_resolution_sessions_tenant;
DROP TABLE IF EXISTS escalation_resolution_sessions;

DROP INDEX IF EXISTS idx_business_sessions_expires;
DROP INDEX IF EXISTS idx_business_sessions_tenant;
DROP TABLE IF EXISTS business_sessions;

DROP INDEX IF EXISTS idx_post_sessions_expires;
DROP INDEX IF EXISTS idx_post_sessions_tenant;
DROP TABLE IF EXISTS post_sessions;

