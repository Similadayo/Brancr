-- +migrate Up

ALTER TABLE conversation_ai_states
ADD COLUMN IF NOT EXISTS hostility_streak INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN conversation_ai_states.hostility_streak IS 'Consecutive hostile/frustrated turns used for behavioral escalation thresholds';

-- +migrate Down

ALTER TABLE conversation_ai_states
DROP COLUMN IF EXISTS hostility_streak;


