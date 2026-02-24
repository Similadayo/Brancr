package signals

import (
	"context"
	"testing"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/stretchr/testify/assert"
)

func TestFactExtractor_Heuristics(t *testing.T) {
	extractor := NewFactExtractor(nil) // No LLM
	ctx := context.Background()

	tests := []struct {
		msg      string
		expected SemanticFacts
	}{
		{
			msg: "I want to talk to a human",
			expected: SemanticFacts{
				MentionsHuman: true,
			},
		},
		{
			msg: "How much is this?",
			expected: SemanticFacts{
				MentionsPrice:   true,
				MentionsPayment: true, // "how much" triggers fuzzy payment/price
			},
		},
		{
			msg: "I want a refund",
			expected: SemanticFacts{
				MentionsRefund:    true,
				MentionsComplaint: true,
			},
		},
		{
			msg: "Yes, confirm order",
			expected: SemanticFacts{
				MentionsConfirmation: true,
			},
		},
		{
			msg: "I want 5 of those",
			expected: SemanticFacts{
				MentionsQuantity: true,
			},
		},
	}

	for _, tt := range tests {
		snapshot := &v4ctx.ContextSnapshot{
			Message: tt.msg,
		}
		facts, err := extractor.Extract(ctx, snapshot)
		assert.NoError(t, err)

		if tt.expected.MentionsHuman {
			assert.True(t, facts.MentionsHuman, "Expected MentionsHuman for: %s", tt.msg)
		}
		if tt.expected.MentionsRefund {
			assert.True(t, facts.MentionsRefund, "Expected MentionsRefund for: %s", tt.msg)
		}
		if tt.expected.MentionsConfirmation {
			assert.True(t, facts.MentionsConfirmation, "Expected MentionsConfirmation for: %s", tt.msg)
		}
		if tt.expected.MentionsQuantity {
			assert.True(t, facts.MentionsQuantity, "Expected MentionsQuantity for: %s", tt.msg)
		}
	}
}
