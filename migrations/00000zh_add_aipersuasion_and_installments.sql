-- +migrate Up
ALTER TABLE sales_conversations
  ADD COLUMN IF NOT EXISTS ai_persuasion_count integer NOT NULL DEFAULT 0;

ALTER TABLE tenant_policies
  ADD COLUMN IF NOT EXISTS allow_installments boolean NOT NULL DEFAULT false;

ALTER TABLE tenant_policies
  ADD COLUMN IF NOT EXISTS installment_terms JSONB;

COMMENT ON COLUMN sales_conversations.ai_persuasion_count IS 'Number of persuasive messages sent by AI during this sales conversation';
COMMENT ON COLUMN tenant_policies.allow_installments IS 'Whether the tenant accepts installment payments';
COMMENT ON COLUMN tenant_policies.installment_terms IS 'JSON describing installment plans and fees';

-- +migrate Down
ALTER TABLE sales_conversations DROP COLUMN IF EXISTS ai_persuasion_count;
ALTER TABLE tenant_policies DROP COLUMN IF EXISTS allow_installments;
ALTER TABLE tenant_policies DROP COLUMN IF EXISTS installment_terms;
