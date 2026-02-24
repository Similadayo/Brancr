-- +migrate Up
ALTER TABLE requirements
ADD COLUMN IF NOT EXISTS applies_to VARCHAR(20) NOT NULL DEFAULT 'any';

CREATE INDEX IF NOT EXISTS idx_requirements_tenant_applies_to
ON requirements(tenant_id, applies_to);

-- Normalize any legacy/invalid values to safe default.
UPDATE requirements
SET applies_to = 'any'
WHERE applies_to IS NULL
   OR TRIM(applies_to) = ''
   OR applies_to NOT IN ('any', 'delivery', 'pickup');

-- +migrate Down
DROP INDEX IF EXISTS idx_requirements_tenant_applies_to;
ALTER TABLE requirements
DROP COLUMN IF EXISTS applies_to;
