package executor

import (
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai/response"
	"github.com/similadayo/brancr/internal/conversation"
	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/types"
)

// WARNING:
// Recommendations must be calm, grounded, and optional.
// Never upsell, invent popularity, or push decisions.
// Violations break trust and human realism.

func ExecuteRecommend(ctx *types.ResolvedContext) *response.AIResponse {
	// 1. Fetch Offerings
	// Guard 3: Use LastShownOfferings (Single Source of Truth)
	var offerings []domain.Offering

	if len(ctx.Conversation.LastShownOfferings) > 0 {
		offerings = ctx.Conversation.LastShownOfferings
	} else {
		// Fallback: Fetch fresh if no history (e.g. first interaction)
		tenantID, err := uuid.Parse(ctx.Merchant.MerchantID)
		if err != nil {
			return &response.AIResponse{HumanText: "I'm having trouble loading the suggestions right now."}
		}
		offerings, err = FetchPrimaryOfferings(tenantID)
		if err != nil {
			return &response.AIResponse{HumanText: "I can't access the list at the moment."}
		}
	}

	// 2. Select Candidates
	recs := SelectRecommendations(offerings, ctx)

	// 3. Format Recommendation
	text := conversation.FormatRecommendation(recs)

	// 4. Update State
	ctx.Conversation.HasRecommended = true
	ctx.Conversation.LastAIMessageAt = time.Now()

	// 5. Optional Follow-up (Check instructions)
	// Step 3.7: "Happy to explain the difference..."
	// "Allowed after recommendation"
	// We'll append it unconditionally or based on logic?
	// Prompt Test 2: "Optional explanatory follow-up".
	// Let's add a simple one if not truncated or specific condition?
	// "We can also tailor it to what you need." is good given "Standard" selector.

	text += "\n\nHappy to explain the difference if that helps."

	return &response.AIResponse{
		HumanText: text,
		Meta: map[string]any{
			"action_type": "recommend",
			"bypass_llm":  true,
		},
	}
}
