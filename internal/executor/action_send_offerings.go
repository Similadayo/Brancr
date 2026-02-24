package executor

import (
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai/response"
	"github.com/similadayo/brancr/internal/conversation"
	"github.com/similadayo/brancr/internal/types"
)

// WARNING:
// ActionSendOfferings must NEVER call GPT or ask questions.
// It is a direct human-rep action.
// Violations reintroduce chatbot behavior.

func ExecuteSendOfferings(ctx *types.ResolvedContext) *response.AIResponse {

	tenantID, _ := uuid.Parse(ctx.Merchant.MerchantID)
	offerings, err := FetchPrimaryOfferings(tenantID)
	if err != nil {
		return &response.AIResponse{
			HumanText: "I’m having trouble pulling the full list right now, but I can still walk you through our main options.",
			Meta: map[string]any{
				"error": true,
			},
		}
	}

	text := conversation.FormatOfferings(offerings)

	// OPTIONAL soft follow-up
	followUp := conversation.OfferingsSoftFollowUp(ctx)

	if followUp != "" {
		text = text + "\n\n" + followUp
	}

	// Step 2.8 State Update (Mandatory)
	ctx.Conversation.HasShownOfferings = true
	ctx.Conversation.LastAIMessageAt = time.Now()

	return &response.AIResponse{
		HumanText: text,
		Meta: map[string]any{
			"action_type":          "send_offerings",
			"bypass_llm":           true,
			"last_shown_offerings": offerings, // Guard 3: Pass back for persistence
		},
	}
}

// fetchPrimaryOfferings is now in internal/executor/offerings_fetch.go
