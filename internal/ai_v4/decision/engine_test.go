package decision

import (
	"context"
	"testing"

	"github.com/google/uuid"
	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/emotion"
	"github.com/similadayo/brancr/internal/ai_v4/memory"
	"github.com/similadayo/brancr/internal/ai_v4/role"
	"github.com/similadayo/brancr/internal/ai_v4/signals"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func createBaseInput() *DecisionInput {
	return &DecisionInput{
		Snapshot: &v4ctx.ContextSnapshot{
			Conversation: &memory.ConversationMemory{Mode: types.ModeDiscovery},
			Operational:  &memory.OperationalMemory{},
		},
		Facts:   &signals.SemanticFacts{},
		Intent:  &types.IntentResult{Primary: types.IntentInquiry, Confidence: 0.8},
		Emotion: &emotion.EmotionState{},
		Role:    &role.RoleContract{TenantID: uuid.New()},
		Capabilities: role.Capabilities{
			OrderingEnabled: true,
			SalesEnabled:    true,
		},
	}
}

func TestEngine_Decide_HardEscalation(t *testing.T) {
	engine := NewEngine(zap.NewNop())
	ctx := context.Background()

	input := createBaseInput()
	input.Intent.Primary = types.IntentComplaint

	decision, err := engine.Decide(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, decision.Escalation)
	assert.Equal(t, "customer_complaint", decision.Escalation.Reason)
	assert.True(t, decision.FreezeAI)
	assert.Equal(t, types.ModeEscalation, decision.NextMode)
}

func TestEngine_Decide_PaymentEscalation(t *testing.T) {
	engine := NewEngine(zap.NewNop())
	ctx := context.Background()

	input := createBaseInput()
	input.Facts.MentionsPayment = true
	input.Role.MustEscalateOnPayment = true // Force escalation

	decision, err := engine.Decide(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, decision.Escalation)
	assert.Equal(t, "active_payment_requires_human", decision.Escalation.Reason)
}

func TestEngine_Decide_CapabilitiesRestriction(t *testing.T) {
	engine := NewEngine(zap.NewNop())
	ctx := context.Background()

	input := createBaseInput()
	input.Capabilities.OrderingEnabled = false // Disable ordering
	input.Intent.Primary = types.IntentPurchase

	decision, err := engine.Decide(ctx, input)

	assert.NoError(t, err)
	assert.Nil(t, decision.Escalation)

	// Should contain blocked actions
	assert.Contains(t, decision.BlockedActions, types.ActionInitializeOrder)
	// Should NOT contain allowed ordering actions
	assert.NotContains(t, decision.AllowedActions, types.ActionInitializeOrder)
}

func TestEngine_Decide_OrderLocking(t *testing.T) {
	engine := NewEngine(zap.NewNop())
	ctx := context.Background()

	input := createBaseInput()
	uid := uuid.New()
	input.Snapshot.Operational.ActiveOrderID = &uid // Active Order
	input.Intent.Primary = types.IntentGreeting     // Unrelated intent

	decision, err := engine.Decide(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, types.ModeOrderCollection, decision.NextMode) // Forced mode
	assert.Equal(t, "order_flow_locked", decision.Reason)
	assert.False(t, decision.FreezeOrders) // Allowed to mutate order
}

func TestEngine_Decide_Failsafe(t *testing.T) {
	engine := NewEngine(zap.NewNop())
	ctx := context.Background()

	input := createBaseInput()
	input.Intent.Confidence = 0.3
	input.Emotion.IsConfused = true

	decision, err := engine.Decide(ctx, input)

	assert.NoError(t, err)
	assert.True(t, decision.FreezeOrders) // Safety freeze
	assert.Empty(t, decision.AllowedActions)
}

func TestEngine_Decide_PolicyEnforcement(t *testing.T) {
	engine := NewEngine(zap.NewNop())
	ctx := context.Background()

	input := createBaseInput()
	input.Role.ForbiddenTopics = []string{"politics"}
	input.Snapshot.Message = "Let's talk about politics"
	input.Capabilities.OrderingEnabled = true

	decision, err := engine.Decide(ctx, input)

	assert.NoError(t, err)
	// Should be blocked
	assert.Empty(t, decision.AllowedActions)
	assert.Equal(t, "blocked_forbidden_topic", decision.Reason)
}
