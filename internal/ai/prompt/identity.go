package prompt

import (
	"fmt"
	"strings"

	"github.com/similadayo/brancr/internal/domain"
)

// IdentityBlock generates the "IDENTITY" section of the system prompt.
// This tells the AI who it is representing.
func IdentityBlock(merchant domain.MerchantConfig) string {
	b := strings.Builder{}
	b.WriteString("IDENTITY:\n")

	// Name
	name := merchant.BusinessName
	if name == "" {
		name = "Customer Support"
	}
	b.WriteString(fmt.Sprintf("- Business Name: %s\n", name))

	// Industry
	if merchant.Industry != "" {
		b.WriteString(fmt.Sprintf("- Industry: %s\n", merchant.Industry))
	}

	// Description (Context)
	if merchant.Description != "" {
		// Clean up description if multiline
		desc := strings.ReplaceAll(merchant.Description, "\n", " ")
		b.WriteString(fmt.Sprintf("- About Us: %s\n", desc))
	}

	// Tone
	tone := merchant.TonePreset
	if tone == "" {
		tone = "Friendly and Professional"
	}
	b.WriteString(fmt.Sprintf("- Brand Tone: %s\n", tone))

	return b.String()
}
