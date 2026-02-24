-- V3 AI Architecture: Database Constraints and Decision Logs
-- This migration adds:
-- 1. Unique constraint to prevent duplicate active orders per conversation
-- 2. decision_logs table for V3 observability

-- =============================================================================
-- 1. PREVENT DUPLICATE ACTIVE ORDERS PER CONVERSATION
-- =============================================================================
-- This ensures only ONE order in building/checkout/payment state per conversation
-- Prevents the bug where multiple orders are created for same conversation

CREATE UNIQUE INDEX IF NOT EXISTS one_active_order_per_conv 
ON orders(conversation_id) 
WHERE status IN ('draft', 'building', 'pending', 'checkout', 'awaiting_payment', 'payment');

-- =============================================================================
-- 2. DECISION LOGS TABLE FOR V3 OBSERVABILITY
-- =============================================================================
-- Tracks AI decision points for debugging and audit

CREATE TABLE IF NOT EXISTS decision_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    customer_id UUID,
    conversation_id UUID NOT NULL,
    
    -- Decision context
    stage VARCHAR(50) NOT NULL,              -- 'intent', 'resolve', 'execute', 'compose'
    input TEXT,                               -- What came in (message, intent, etc.)
    output TEXT,                              -- What was decided (intent type, action, etc.)
    
    -- Metadata
    confidence DECIMAL(3,2),                  -- Confidence score if applicable
    source VARCHAR(50),                       -- 'heuristic', 'llm', etc.
    duration_ms INT,                          -- Processing time
    success BOOLEAN DEFAULT true,             -- Whether action succeeded
    error_message TEXT,                       -- Error if any
    
    -- Additional context (JSON)
    metadata JSONB DEFAULT '{}',              -- Extra data (entities, emotion, etc.)
    
    created_at TIMESTAMP DEFAULT NOW()
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_decision_logs_tenant ON decision_logs(tenant_id);
CREATE INDEX IF NOT EXISTS idx_decision_logs_conversation ON decision_logs(conversation_id);
CREATE INDEX IF NOT EXISTS idx_decision_logs_stage ON decision_logs(stage);
CREATE INDEX IF NOT EXISTS idx_decision_logs_created ON decision_logs(created_at);

-- =============================================================================
-- 3. UNIQUE ITEM PER ORDER CONSTRAINT (optional, prevents duplicate items)
-- =============================================================================
-- This is commented out because items are stored as JSONB, not as separate rows
-- If you migrate to a normalized order_items table, uncomment this:
-- CREATE UNIQUE INDEX IF NOT EXISTS unique_item_per_order 
-- ON order_items(order_id, product_id);

COMMENT ON INDEX one_active_order_per_conv IS 'V3: Prevents duplicate active orders per conversation';
COMMENT ON TABLE decision_logs IS 'V3: AI decision audit trail for observability';
