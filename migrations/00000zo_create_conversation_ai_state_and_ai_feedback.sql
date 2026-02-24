-- +migrate Up

-- Conversation AI state snapshot (for hysteresis + explainability)
CREATE TABLE IF NOT EXISTS conversation_ai_states (
  id BIGSERIAL PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  customer_id INTEGER NOT NULL,
  conversation_id INTEGER NOT NULL UNIQUE,

  state VARCHAR(30) NOT NULL DEFAULT 'discovery',
  mode VARCHAR(40) NOT NULL DEFAULT 'neutral_assistant',
  emotion_label VARCHAR(30) NOT NULL DEFAULT 'neutral',
  emotion_intensity DOUBLE PRECISION NOT NULL DEFAULT 0,
  calm_streak INTEGER NOT NULL DEFAULT 0,
  risk_level VARCHAR(20) NOT NULL DEFAULT 'low',
  risk_reasons JSONB NOT NULL DEFAULT '[]',
  decision_confidence DOUBLE PRECISION NOT NULL DEFAULT 0,

  last_escalation_reason VARCHAR(255) DEFAULT '',
  last_escalation_category VARCHAR(30) DEFAULT '',

  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now(),
  deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_conversation_ai_states_tenant ON conversation_ai_states(tenant_id);
CREATE INDEX IF NOT EXISTS idx_conversation_ai_states_customer ON conversation_ai_states(customer_id);
CREATE INDEX IF NOT EXISTS idx_conversation_ai_states_conversation ON conversation_ai_states(conversation_id);

-- AI feedback events for evaluation/learning loops
CREATE TABLE IF NOT EXISTS ai_feedback_events (
  id BIGSERIAL PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  customer_id INTEGER NOT NULL,
  conversation_id INTEGER NOT NULL,
  interaction_id INTEGER NOT NULL,

  type VARCHAR(50) NOT NULL,
  suggested_reply TEXT,
  final_reply TEXT,
  decision_state VARCHAR(30) DEFAULT '',
  decision_meta JSONB NOT NULL DEFAULT '{}',

  created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ai_feedback_events_tenant ON ai_feedback_events(tenant_id);
CREATE INDEX IF NOT EXISTS idx_ai_feedback_events_conversation ON ai_feedback_events(conversation_id);
CREATE INDEX IF NOT EXISTS idx_ai_feedback_events_interaction ON ai_feedback_events(interaction_id);
CREATE INDEX IF NOT EXISTS idx_ai_feedback_events_type ON ai_feedback_events(type);

-- +migrate Down

DROP INDEX IF EXISTS idx_ai_feedback_events_type;
DROP INDEX IF EXISTS idx_ai_feedback_events_interaction;
DROP INDEX IF EXISTS idx_ai_feedback_events_conversation;
DROP INDEX IF EXISTS idx_ai_feedback_events_tenant;
DROP TABLE IF EXISTS ai_feedback_events;

DROP INDEX IF EXISTS idx_conversation_ai_states_conversation;
DROP INDEX IF EXISTS idx_conversation_ai_states_customer;
DROP INDEX IF EXISTS idx_conversation_ai_states_tenant;
DROP TABLE IF EXISTS conversation_ai_states;


