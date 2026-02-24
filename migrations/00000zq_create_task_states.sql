-- Create task_states table for conversational task management
-- +migrate Up
CREATE TABLE IF NOT EXISTS task_states (
    id SERIAL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    chat_id BIGINT NOT NULL,
    task_type VARCHAR(50) NOT NULL,
    task_data TEXT,
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_states_tenant_chat ON task_states(tenant_id, chat_id);
CREATE INDEX IF NOT EXISTS idx_task_states_status ON task_states(status);
CREATE INDEX IF NOT EXISTS idx_task_states_updated ON task_states(updated_at);

-- +migrate Down
DROP TABLE IF EXISTS task_states;
