-- +migrate Up
-- Add LastAIOffer and EscalationPending fields to sales_conversations table
-- This prevents price backtracking and enables escalation state locking

-- Add LastAIOffer column to track the best/lowest price AI has offered
-- This prevents the AI from offering a higher price than it has already stated
ALTER TABLE sales_conversations 
ADD COLUMN IF NOT EXISTS last_ai_offer double precision NOT NULL DEFAULT 0;

-- Add EscalationPending column to track if escalation has been triggered
-- When true, negotiation should pause and wait for human intervention
ALTER TABLE sales_conversations 
ADD COLUMN IF NOT EXISTS escalation_pending boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN sales_conversations.last_ai_offer IS 'Last/best price AI has offered - prevents backtracking (can only stay same or improve)';
COMMENT ON COLUMN sales_conversations.escalation_pending IS 'True if escalation mentioned - negotiation should pause and wait for human intervention';

-- +migrate Down
ALTER TABLE sales_conversations DROP COLUMN IF EXISTS last_ai_offer;
ALTER TABLE sales_conversations DROP COLUMN IF EXISTS escalation_pending;

