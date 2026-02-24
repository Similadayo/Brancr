package v3

import "strings"

// =============================================================================
// SOCIAL EXPECTATION - What the customer expects NEXT
// =============================================================================

// SocialExpectation represents what the customer anticipates the AI will do.
// This is NOT intent (what user said) — this is anticipation (what user wants next).
type SocialExpectation string

const (
	ExpectNone           SocialExpectation = "none"
	ExpectMenu           SocialExpectation = "expect_menu"
	ExpectConfirmation   SocialExpectation = "expect_confirmation"
	ExpectPaymentDetails SocialExpectation = "expect_payment_details"
	ExpectPriceResponse  SocialExpectation = "expect_price_response"
	ExpectAction         SocialExpectation = "expect_action"
	ExpectSilence        SocialExpectation = "expect_silence"
	ExpectReassurance    SocialExpectation = "expect_reassurance"
)

// deriveSocialExpectation infers what the customer expects next.
// CRITICAL: This is deterministic, not ML-based. Keeps behavior predictable.
func deriveSocialExpectation(ctx *ConversationContext, intent Intent) SocialExpectation {

	// SUPPORT MODE OVERRIDE: Never silence in customer service
	if ctx.ActiveRole == "customer_service" {
		// Support always expects conversation, not silence
		if intent.Type == IntentGreeting {
			return ExpectAction // Greeting needs response, not silence
		}
		return ExpectAction
	}

	// Explicit signals always win
	switch intent.Type {

	case IntentAddItem:
		return ExpectConfirmation

	case IntentCheckout, IntentConfirmPayment:
		return ExpectPaymentDetails

	case IntentNegotiation, IntentPriceInquiry:
		return ExpectPriceResponse

	case IntentGreeting:
		return ExpectAction // Changed: Greetings need responses, not menus

	case IntentHelp:
		return ExpectAction

	case IntentProductInquiry:
		return ExpectMenu
	}

	// Implicit signals (more subtle)

	// High commitment → customer expects action, not chatter
	if ctx.CommitmentGradient >= 0.7 {
		return ExpectAction
	}

	// Negative momentum → customer needs reassurance
	if ctx.UserSignals.Momentum < 0 {
		return ExpectReassurance
	}

	// After payment was requested → customer expects silence
	if ctx.LastSocialMove == MoveRequestedPayment {
		return ExpectSilence
	}

	// After item confirmed → customer likely expects silence or payment
	if ctx.LastSocialMove == MoveConfirmedItem && ctx.CommitmentGradient >= 0.5 {
		return ExpectSilence
	}

	return ExpectNone
}

// =============================================================================
// SUPPORT MODE GUARDS - Sales ≠ Support
// =============================================================================

// silenceAllowed returns false for support contexts where silence is wrong
func silenceAllowed(ctx *ConversationContext) bool {
	// NEVER silence in customer service mode
	if ctx.ActiveRole == "customer_service" {
		return false
	}

	// NEVER silence on greetings
	if ctx.ExpectedNextMove == ExpectAction {
		return false
	}

	return true
}

// handleGreetingOverride ensures greetings ALWAYS get conversational responses
// This runs BEFORE any other social logic
func handleGreetingOverride(ctx *ConversationContext, action Action) []string {
	if action.Type != ActionGreet {
		return nil
	}

	// Support tone if confusion exists
	if ctx.UserSignals.Momentum < 0 {
		return []string{
			"Good morning 🙂 How can I help?",
		}
	}

	// Check time for appropriate greeting
	return []string{
		"Good morning! How can I help today?",
	}
}

// =============================================================================
// EXPECTATION ENFORCEMENT - Make AI respond to what customer expects
// =============================================================================

// enforceExpectation adjusts messages based on customer expectations.
// IMPORTANT: This FILTERS LLM output, it does NOT replace it.
func enforceExpectation(messages []string, ctx *ConversationContext, action Action) []string {
	if len(messages) == 0 {
		return messages
	}

	// In support mode, never silence — let LLM response through
	if ctx.ActiveRole == "customer_service" {
		return messages
	}

	switch ctx.ExpectedNextMove {

	case ExpectSilence:
		// ONLY silence in sales mode after successful transaction
		if silenceAllowed(ctx) && ctx.CommitmentGradient > 0.8 {
			return trimToEssential(messages, 1) // Keep first message only
		}
		return messages

	case ExpectConfirmation:
		// Limit to 2 messages for confirmation flow
		return trimToEssential(messages, 2)

	case ExpectPaymentDetails:
		// Keep payment details concise
		return trimToEssential(messages, 2)

	case ExpectAction:
		// Brief action — keep LLM response but limit
		return trimToEssential(messages, 2)
	}

	return messages
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// containsConfirmation checks if any message contains confirmation language
func containsConfirmation(messages []string) bool {
	confirmPhrases := []string{"got it", "done", "added", "confirmed", "all set", "👍", "✅"}
	for _, m := range messages {
		lower := strings.ToLower(m)
		for _, phrase := range confirmPhrases {
			if strings.Contains(lower, phrase) {
				return true
			}
		}
	}
	return false
}

// buildMinimalConfirmation creates a minimal confirmation for an action
func buildMinimalConfirmation(action Action) string {
	switch action.Type {
	case ActionAddToCart:
		return "Got it 👍"
	case ActionRemoveFromCart:
		return "Removed 👍"
	case ActionConfirmOrder:
		return "Confirmed! ✅"
	default:
		return "All set 👍"
	}
}

// trimToEssential keeps only the first N messages
func trimToEssential(messages []string, max int) []string {
	if len(messages) <= max {
		return messages
	}
	return messages[:max]
}
