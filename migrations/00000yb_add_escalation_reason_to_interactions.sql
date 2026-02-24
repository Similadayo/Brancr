-- +migrate Up
ALTER TABLE interactions
ADD COLUMN escalation_reason VARCHAR(255);

-- +migrate Down
ALTER TABLE interactions
DROP COLUMN IF EXISTS escalation_reason;
