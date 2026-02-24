-- +migrate Up
CREATE TABLE IF NOT EXISTS drafts (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_id BIGINT NULL,
    key TEXT NOT NULL,
    content JSONB NULL,
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_drafts_tenant_key ON drafts(tenant_id, key);

-- +migrate Down
DROP INDEX IF EXISTS idx_drafts_tenant_key;
DROP TABLE IF EXISTS drafts;
