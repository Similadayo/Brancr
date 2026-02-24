package social

import (
	"context"

	"github.com/similadayo/brancr/internal/types"
	"go.uber.org/zap"
)

// CostControlFallback provides fallback responses when AI call limits are exceeded
type CostControlFallback struct {
	logger *zap.Logger
}

// NewCostControlFallback creates a new cost control fallback handler
func NewCostControlFallback(logger *zap.Logger) *CostControlFallback {
	return &CostControlFallback{
		logger: logger,
	}
}

// GenerateFallbackReply generates a friendly clarification message when cost limits are reached
// This is a "soft fallback" - asks for clarification rather than failing
func (c *CostControlFallback) GenerateFallbackReply(ctx context.Context, msg IncomingMessage) types.StructuredReply {
	// Soft fallback: Ask for clarification
	// This prevents spam while still being helpful
	fallbackMessages := []string{
		"I'm getting a lot of messages from you. Could you help me understand what you need?",
		"Are you asking about:",
		"• Price or availability?",
		"• Placing an order?",
		"• Something else?",
		"Just let me know and I'll help you right away!",
	}
	
	// For very short messages, use a simpler fallback
	if len(msg.Content) < 20 {
		fallbackMessages = []string{
			"Got it! Could you tell me a bit more about what you're looking for?",
		}
	}
	
	c.logger.Info("💰 Using cost control fallback response",
		zap.Stringer("tenant_id", msg.TenantID),
		zap.String("platform", msg.Platform),
		zap.String("customer_id", msg.CustomerID))
	
	return types.StructuredReply{
		ReplyMode:  "sequence",
		Messages:   fallbackMessages,
		Confidence: "low",
		Escalation: nil,
	}
}

