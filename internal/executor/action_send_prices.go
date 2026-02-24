package executor

import (
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/conversation"
	"github.com/similadayo/brancr/internal/types"
)

func SendPrices(ctx *types.ResolvedContext) TerminalResponse {
	// 1. Fetch
	tenantID, _ := uuid.Parse(ctx.Merchant.MerchantID)
	offerings, err := FetchPrimaryOfferings(tenantID)
	if err != nil {
		return TerminalResponse{
			Text: "I'm having trouble pulling the price list right now.",
			Stop: true,
		}
	}

	// 2. Format
	// Using same formatter as menu for now, as it includes prices.
	// Spec says "prices only" but our FormatOfferings does "Item - Price".
	prices := conversation.FormatOfferings(offerings)

	// 3. Return Terminal
	return TerminalResponse{
		Text: prices,
		Stop: true,
	}
}
