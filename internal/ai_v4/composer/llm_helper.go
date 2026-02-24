package composer

import (
	"context"
	"fmt"

	"github.com/similadayo/brancr/internal/ai_v4/types"
)

// renderWithLLM calls the V4 Python service to rewrite the deterministic reply.
func (c *DefaultComposer) renderWithLLM(ctx context.Context, resolved *types.ResolvedContext, intent *types.IntentResult, baseReply string, mustInclude []string) (string, error) {
	// Build specific context summaries
	cartSummary := "" // Implement cart summary logic
	// Fix 7: Strict Memory Safety - only show order context if definitely active
	if resolved.Memory.ActiveOrderID != nil && resolved.Signals != nil && resolved.Signals.HasActiveOrder {
		cartSummary = "User has an active order"
	}

	// Fix 4: HARD GATE PRODUCT CONTEXT REMOVED
	// Always provide product context. The Python AI (v4.py) prompt controls when to show it.
	productsCtx := ""
	if len(resolved.Offerings) > 0 {
		productsCtx = formatOfferings(resolved.Offerings)
	}

	// Build LLM request
	llmReq := types.GenerateRequest{
		Message:             resolved.Message,
		BusinessName:        resolved.Business.Name,
		BusinessDescription: resolved.Business.Description,
		Industry:            resolved.Business.Industry,
		BusinessTone:        resolved.Business.Tone,
		CustomerName:        resolved.Customer.Name,
		Platform:            string(resolved.Platform),
		ConversationState:   "browsing", // Simplified for now
		ConversationMode:    string(resolved.Memory.Mode),
		Intent:              string(intent.Primary),
		CartSummary:         cartSummary, // Use the calculated summary
		OrderSummary:        "",          // TODO: Add order summary
		ProductsContext:     productsCtx, // gated context
		BaseReply:           baseReply,
		MustInclude:         mustInclude,
	}

	// Call LLM
	resp, err := c.llm.GenerateWithContext(ctx, llmReq)
	if err != nil {
		return "", err
	}

	return resp.Reply, nil
}

// formatOfferings converts offerings to a context string for LLM
func formatOfferings(offerings []types.Offering) string {
	if len(offerings) == 0 {
		return ""
	}

	var sb string
	for _, o := range offerings {
		price := "Price on Request"
		if o.Price > 0 {
			price = fmt.Sprintf("%.2f %s", o.Price, o.Currency)
		}

		status := ""
		if !o.IsAvailable {
			status = " (Unavailable)"
		}

		sb += fmt.Sprintf("- %s (%s): %s%s\n", o.Name, o.Type, price, status)
	}
	return sb
}
