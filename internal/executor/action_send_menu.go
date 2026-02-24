package executor

import (
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/conversation"
	"github.com/similadayo/brancr/internal/types"
)

func SendMenu(ctx *types.ResolvedContext) TerminalResponse {
	// 1. Fetch
	tenantID, _ := uuid.Parse(ctx.Merchant.MerchantID)
	offerings, err := FetchPrimaryOfferings(tenantID)
	if err != nil {
		return TerminalResponse{
			Text: "I'm having trouble pulling the menu right now.",
			Stop: true,
		}
	}

	// 2. Format (Deterministic, No Narration)
	menu := conversation.FormatOfferings(offerings)

	// 3. Return Terminal
	return TerminalResponse{
		Text: menu,
		Stop: true,
	}
}
