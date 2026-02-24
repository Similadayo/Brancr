package decision

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/ai/guard"
	"github.com/similadayo/brancr/internal/ai/prompt"
	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/executor"
	"github.com/similadayo/brancr/internal/types"
)

// Generator is a function that generates text from system and user prompts.
// This decouples the decision package from the transport layer.
type Generator func(ctx context.Context, systemPrompt, userPrompt string) (string, error)

func Recommend(ctx context.Context, resolved *types.ResolvedContext, generate Generator) (string, error) {
	// 1. Get Available Options (Only in-stock/visible)
	// Using executor.FetchPrimaryOfferings (exported in Step 4)
	tenantID, err := uuid.Parse(resolved.Merchant.MerchantID)
	if err != nil {
		return "I'm having trouble accessing the menu right now.", nil
	}

	items, err := executor.FetchPrimaryOfferings(tenantID)
	if err != nil {
		return "I'm having trouble accessing the menu right now.", nil
	}

	// 2. Format
	system := prompt.DecisionSystemPrompt()
	user := formatItems(items)

	// 3. Call Generator (Closure)
	output, err := generate(ctx, system, user)
	if err != nil {
		return "I'm having a bit of trouble deciding right now. What kind of flavors do you usually like?", nil // Fallback
	}

	// 4. Validate (Short Output Guard)
	// Spec: "Most popular", "Customers love", "Best seller" banned.
	// We can use the existing guard + specific checks.
	if !guard.ValidateOutput(output) {
		return "Everything on the menu is great!", nil // Safe neutral fallback
	}

	return output, nil
}

func formatItems(items []domain.Offering) string {
	out := "AVAILABLE OPTIONS:\n"
	for _, i := range items {
		out += fmt.Sprintf("- %s — %s\n", i.Title, i.PriceText)
	}
	return out
}
