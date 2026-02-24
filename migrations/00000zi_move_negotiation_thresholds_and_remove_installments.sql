-- +migrate Up
ALTER TABLE tenant_policies
  ADD COLUMN IF NOT EXISTS negotiation_same_offer_required integer NOT NULL DEFAULT 3;

ALTER TABLE tenant_policies
  ADD COLUMN IF NOT EXISTS negotiation_persuasion_required integer NOT NULL DEFAULT 3;

ALTER TABLE tenant_policies
  ADD COLUMN IF NOT EXISTS negotiation_proximity_pct double precision NOT NULL DEFAULT 0.10;

-- Drop legacy installment columns
ALTER TABLE tenant_policies DROP COLUMN IF EXISTS allow_installments;
ALTER TABLE tenant_policies DROP COLUMN IF EXISTS installment_terms;

COMMENT ON COLUMN tenant_policies.negotiation_same_offer_required IS 'Number of repeated identical customer offers required to consider persistence';
COMMENT ON COLUMN tenant_policies.negotiation_persuasion_required IS 'Number of AI persuasion attempts required before persistence counts';
COMMENT ON COLUMN tenant_policies.negotiation_proximity_pct IS 'Proximity (fraction) used to compare offers';

-- +migrate Down
ALTER TABLE tenant_policies DROP COLUMN IF EXISTS negotiation_same_offer_required;
ALTER TABLE tenant_policies DROP COLUMN IF EXISTS negotiation_persuasion_required;
ALTER TABLE tenant_policies DROP COLUMN IF EXISTS negotiation_proximity_pct;

ALTER TABLE tenant_policies ADD COLUMN IF NOT EXISTS allow_installments boolean NOT NULL DEFAULT false;
ALTER TABLE tenant_policies ADD COLUMN IF NOT EXISTS installment_terms JSONB;