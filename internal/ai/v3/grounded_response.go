package v3

import (
	"fmt"
	"strings"
)

// =============================================================================
// GROUNDED RESPONSE GENERATOR - Data-Driven, No Templates
// =============================================================================

// GenerateFromGroundedContext generates a response ONLY from DB data.
// No fallback templates. If data exists, it must speak.
func GenerateFromGroundedContext(g *GroundedContext, userMessage string) string {
	// Safety: if no grounded context, escalate
	if g == nil {
		return "I'm having trouble retrieving the business details right now. Please try again in a moment."
	}

	switch g.ConversationType {

	case ConversationGreeting:
		return greet(g)

	case ConversationSales:
		return listProducts(g)

	case ConversationAdvisory:
		return recommendProduct(g, userMessage)

	case ConversationSupport:
		return answerFromFAQsOrPolicy(g, userMessage)
	}

	// ResponseMode fallback
	switch g.ResponseMode {
	case GroundingClarify:
		if len(g.MissingData) > 0 {
			return g.MissingData[0].Prompt // Ask ONE specific question
		}
		return clarifyFromAvailableData(g)

	case GroundingEscalate:
		return "I've flagged this for a human team member to assist you."
	}

	// Direct mode but no conversation type match - use available data
	return directResponseFromData(g)
}

// =============================================================================
// CONVERSATION TYPE HANDLERS
// =============================================================================

// greet handles ConversationGreeting
func greet(g *GroundedContext) string {
	name := g.BusinessName
	if name == "" {
		name = "us"
	}

	// If we have products, include them in greeting
	if len(g.Products) > 0 {
		if len(g.Products) == 1 {
			return fmt.Sprintf("Hey! Welcome to %s. We've got %s. What are you in the mood for?",
				name, g.Products[0].Name)
		}
		first := g.Products[0].Name
		second := g.Products[1].Name
		return fmt.Sprintf("Hey! Welcome to %s. We've got %s, %s and more. What are you in the mood for?",
			name, first, second)
	}

	// If we have services
	if len(g.Services) > 0 {
		if len(g.Services) == 1 {
			return fmt.Sprintf("Hey! Welcome to %s. We offer %s. How can I help?",
				name, g.Services[0].Name)
		}
		first := g.Services[0].Name
		second := g.Services[1].Name
		return fmt.Sprintf("Hey! Welcome to %s. We offer %s, %s and more. How can I help?",
			name, first, second)
	}

	// Minimal greeting (no products/services yet)
	return fmt.Sprintf("Hey! Welcome to %s. How can I help you today?", name)
}

// listProducts handles ConversationSales
func listProducts(g *GroundedContext) string {
	if len(g.Products) == 0 && len(g.Services) == 0 {
		return "We don't have items listed yet, but I can help you once they're added."
	}

	var lines []string
	for _, p := range g.Products {
		line := fmt.Sprintf("• %s - %s %.0f", p.Name, p.Currency, p.Price)
		if p.Description != "" {
			line += fmt.Sprintf(" (%s)", truncateDesc(p.Description, 30))
		}
		lines = append(lines, line)
	}

	for _, s := range g.Services {
		line := fmt.Sprintf("• %s - %s %.0f", s.Name, s.Currency, s.Price)
		if s.Duration != "" {
			line += fmt.Sprintf(" (%s)", s.Duration)
		}
		lines = append(lines, line)
	}

	header := "Here's what we've got:"
	if g.BusinessName != "" {
		header = fmt.Sprintf("Here's what %s has:", g.BusinessName)
	}

	return header + "\n" + strings.Join(lines, "\n")
}

// recommendProduct handles ConversationAdvisory
func recommendProduct(g *GroundedContext, msg string) string {
	if len(g.Products) == 0 && len(g.Services) == 0 {
		return "We don't have items listed yet, but I can help you once they're added."
	}

	// Simple heuristic: pick the first product as recommendation
	// Future: ranking based on popularity, message keywords, etc.
	if len(g.Products) > 0 {
		p := g.Products[0]
		return fmt.Sprintf(
			"If you want something most people love, I'd suggest %s at %s %.0f.",
			p.Name, p.Currency, p.Price,
		)
	}

	if len(g.Services) > 0 {
		s := g.Services[0]
		return fmt.Sprintf(
			"Our most popular option is %s at %s %.0f.",
			s.Name, s.Currency, s.Price,
		)
	}

	return "Let me know what you're looking for and I'll help you pick."
}

// answerFromFAQsOrPolicy handles ConversationSupport
func answerFromFAQsOrPolicy(g *GroundedContext, msg string) string {
	lower := strings.ToLower(msg)

	// Try FAQ match first
	for _, faq := range g.FAQs {
		for _, kw := range faq.Keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return faq.Answer
			}
		}
		// Also check question similarity
		if strings.Contains(lower, strings.ToLower(faq.Question)) {
			return faq.Answer
		}
	}

	// Try policy match
	for _, policy := range g.Policies {
		if strings.Contains(lower, strings.ToLower(policy.Type)) {
			return policy.Content
		}
	}

	// No FAQ/Policy match - escalate
	return "I don't have the specific answer to that, but I've noted your question for the team."
}

// =============================================================================
// RESPONSE MODE HANDLERS
// =============================================================================

// clarifyFromAvailableData asks a specific question based on what's missing
func clarifyFromAvailableData(g *GroundedContext) string {
	// If we have products, ask about preference
	if len(g.Products) > 0 {
		if len(g.Products) == 1 {
			return fmt.Sprintf("Are you interested in %s?", g.Products[0].Name)
		}
		return fmt.Sprintf("We have %s and %s. Which one interests you?",
			g.Products[0].Name, g.Products[1].Name)
	}

	// If we have services
	if len(g.Services) > 0 {
		return fmt.Sprintf("We offer %s. Would you like more details?",
			g.Services[0].Name)
	}

	// Nothing specific to ask about
	return "What are you looking for today?"
}

// directResponseFromData creates a response from available data
func directResponseFromData(g *GroundedContext) string {
	// If products exist, show them
	if len(g.Products) > 0 {
		return listProducts(g)
	}

	// If services exist, show them
	if len(g.Services) > 0 {
		return listProducts(g) // reuse, it handles services too
	}

	// Payment info available
	if g.PaymentInfo != nil && g.PaymentInfo.BankName != "" {
		return fmt.Sprintf("You can pay via bank transfer to %s (%s %s).",
			g.PaymentInfo.BankName, g.PaymentInfo.AccountNumber, g.PaymentInfo.AccountName)
	}

	// Absolute fallback (no data at all)
	return "I'm here! How can I help you today?"
}

// =============================================================================
// HELPERS
// =============================================================================

func truncateDesc(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
