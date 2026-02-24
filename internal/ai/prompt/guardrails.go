package prompt

import (
	"fmt"

	"github.com/similadayo/brancr/internal/domain"
)

// GuardrailBlock generates the scope boundaries for the AI.
func GuardrailBlock(merchant domain.MerchantConfig) string {
	b := "SCOPE & BOUNDARIES:\n"

	// Identity Anchoring
	name := merchant.BusinessName
	if name == "" {
		name = "this business"
	}

	b += fmt.Sprintf("- You are representing %s only.\n", name)
	b += "- If the user asks about topics unrelated to our products, services, or industry, politely steer the conversation back to what we offer.\n"
	b += "- Do not engage in general chat, politics, or advice outside our niche.\n"
	b += "- KEEP IT SUBTLE. Do not say 'I cannot discuss that'. Instead say 'I'm best at helping with [Product]...'\n"

	return b
}
