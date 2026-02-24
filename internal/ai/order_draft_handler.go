package ai

import (
	"strings"
)

// ============================================================
// ORDER SILENT DRAFT HANDLER (Blueprint Section 7 & 9)
// Purpose: Handle silent order drafts and order modifications
// Extends existing OrderDraft in sales_conversation.go
// ============================================================

// OrderDraftHandler manages order draft operations
// Works with existing OrderDraft struct in sales_conversation.go
type OrderDraftHandler struct{}

// NewOrderDraftHandler creates a new handler
func NewOrderDraftHandler() *OrderDraftHandler {
	return &OrderDraftHandler{}
}

// OrderModificationResult contains the result of a modification
type OrderModificationResult struct {
	Success      bool    `json:"success"`
	Message      string  `json:"message"` // For AI to phrase (not hardcoded to customer)
	ItemsChanged int     `json:"items_changed"`
	NewTotal     float64 `json:"new_total,omitempty"`
}

// IsOrderModificationRequest detects if a message requests order modification
// Blueprint Section 9: Handle "actually, add X" during payment phase
func IsOrderModificationRequest(message string, stage string) bool {
	// Only valid during payment-related stages
	validStages := map[string]bool{
		"payment":                 true,
		"payment_handling":        true,
		"payment_instructions":    true,
		"order_confirmation":      true,
		"commitment_confirmation": true,
	}

	if !validStages[stage] {
		return false
	}

	messageLower := strings.ToLower(message)

	// Modification keywords
	modificationPhrases := []string{
		"actually", "wait", "also add", "add", "remove",
		"change", "cancel", "instead", "swap", "plus",
		"forgot", "include", "exclude", "more of", "less of",
		"can you add", "i want to add", "let me add",
		"take out", "take off", "no more", "actually i want",
		"wait i also", "oh and", "one more",
	}

	for _, phrase := range modificationPhrases {
		if strings.Contains(messageLower, phrase) {
			return true
		}
	}

	return false
}

// ShouldShowDraftToCustomer determines if AI should mention the "order"
// Blueprint: NEVER mention order/draft unless customer has committed
func ShouldShowDraftToCustomer(stage string, commitmentLevel string) bool {
	// Only allowed to mention order after commitment
	committedLevels := map[string]bool{
		"committed":     true,
		"payment_ready": true,
	}

	committedStages := map[string]bool{
		"payment":              true,
		"payment_handling":     true,
		"payment_instructions": true,
		"order_confirmation":   true,
	}

	return committedLevels[commitmentLevel] || committedStages[stage]
}

// GetDraftContextPrompt returns prompt guidance for draft handling
// This is internal context for AI, never shown to customer
func GetDraftContextPrompt(hasItems bool, stage string, commitmentLevel string) string {
	if !hasItems {
		return "" // No draft context needed
	}

	if !ShouldShowDraftToCustomer(stage, commitmentLevel) {
		return `INTERNAL ORDER CONTEXT (DO NOT MENTION TO CUSTOMER):
- Customer has discussed items but NOT committed
- DO NOT say "your order" or mention order details
- Focus on helping them decide, not closing
- Wait for commitment signals before proceeding to payment`
	}

	return `ORDER CONTEXT:
- Customer has committed and is ready to finalize
- You may now discuss order details
- Proceed with payment instructions when ready`
}
