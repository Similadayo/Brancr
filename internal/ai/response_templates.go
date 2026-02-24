package ai

import (
	"fmt"
	"strings"
)

// ResponseTemplates provides hardcoded fallback responses for v2.0 pipeline.
// If AI fails, these templates ensure the system still responds meaningfully.
// This is FIRST-CLASS behavior, not an edge case.
type ResponseTemplates struct {
	BrandName string
	Currency  string
}

// NewResponseTemplates creates a new templates provider.
func NewResponseTemplates(brandName, currency string) *ResponseTemplates {
	if brandName == "" {
		brandName = "our store"
	}
	if currency == "" {
		currency = "NGN"
	}
	return &ResponseTemplates{
		BrandName: brandName,
		Currency:  currency,
	}
}

// === ACTION-BASED TEMPLATES ===
// These are used when the v2 ActionExecutor completes an action.

// OrderCreated returns template for when an order is created.
func (t *ResponseTemplates) OrderCreated(items string, total float64) string {
	return fmt.Sprintf("Got it! I've added %s to your order. Your total is %s %.2f.\n\nReady to checkout, or would you like to add more items?", items, t.Currency, total)
}

// OrderUpdated returns template for when items are added to existing order.
func (t *ResponseTemplates) OrderUpdated(items string, total float64) string {
	return fmt.Sprintf("Updated! Your order now includes %s. New total: %s %.2f.\n\nAnything else?", items, t.Currency, total)
}

// OrderLocked returns template for when checkout is initiated.
func (t *ResponseTemplates) OrderLocked(orderNumber string, total float64) string {
	return fmt.Sprintf("Order #%s confirmed! Total: %s %.2f.\n\nWould you like payment details now?", orderNumber, t.Currency, total)
}

// PaymentInstructions returns template with payment details.
// CRITICAL: Uses full account number, no masking.
func (t *ResponseTemplates) PaymentInstructions(info *PaymentInfo, orderNumber string) string {
	if info == nil {
		return "Please contact us directly for payment details."
	}

	lines := []string{
		fmt.Sprintf("Here are the payment details for order #%s:", orderNumber),
		"",
		fmt.Sprintf("Bank: %s", info.BankName),
		fmt.Sprintf("Account Name: %s", info.AccountName),
		fmt.Sprintf("Account Number: %s", info.AccountNumber), // FULL NUMBER
		fmt.Sprintf("Amount: %s %.2f", info.Currency, info.Amount),
	}

	if info.Reference != "" {
		lines = append(lines, fmt.Sprintf("Reference: %s", info.Reference))
	}

	lines = append(lines, "", "Please let me know once you've made the transfer!")

	return strings.Join(lines, "\n")
}

// PaymentConfirmed returns template for when payment is confirmed.
func (t *ResponseTemplates) PaymentConfirmed(orderNumber string) string {
	return fmt.Sprintf("Thank you! Payment received for order #%s. We'll get started on your order right away!", orderNumber)
}

// OrderCancelled returns template for when order is cancelled.
func (t *ResponseTemplates) OrderCancelled() string {
	return "Your order has been cancelled. Feel free to start a new order anytime!"
}

// === FALLBACK TEMPLATES ===
// These are used when AI fails or confidence is low.

// LowConfidence returns clarification request.
func (t *ResponseTemplates) LowConfidence(suggestedOptions []string) string {
	if len(suggestedOptions) == 0 {
		return "Just to make sure I understand - could you tell me a bit more about what you're looking for?"
	}
	return fmt.Sprintf("Just to be sure - are you trying to %s?", strings.Join(suggestedOptions, " or "))
}

// UnknownIntent returns helpful menu of options.
func (t *ResponseTemplates) UnknownIntent() string {
	return fmt.Sprintf("I can help you with:\n• Browse our products\n• Add items to your order\n• Check out and pay\n\nWhat would you like to do?")
}

// ConflictingState returns explanation of current state.
func (t *ResponseTemplates) ConflictingState(currentState OrderState, validActions []string) string {
	stateDesc := t.describeState(currentState)
	if len(validActions) == 0 {
		return fmt.Sprintf("You currently %s. How can I help?", stateDesc)
	}
	return fmt.Sprintf("You currently %s. Would you like to %s?", stateDesc, strings.Join(validActions, " or "))
}

// SystemError returns generic error message (no internals exposed).
func (t *ResponseTemplates) SystemError() string {
	return "Something went wrong on our end. Please try again in a moment, or contact us directly if this persists."
}

// === GREETING TEMPLATES ===

// Greeting returns a simple greeting.
func (t *ResponseTemplates) Greeting(customerName string) string {
	if customerName != "" {
		return fmt.Sprintf("Hi %s! Welcome to %s. How can I help you today?", customerName, t.BrandName)
	}
	return fmt.Sprintf("Hi! Welcome to %s. How can I help you today?", t.BrandName)
}

// Goodbye returns farewell message.
func (t *ResponseTemplates) Goodbye() string {
	return fmt.Sprintf("Thank you for visiting %s! Feel free to reach out anytime.", t.BrandName)
}

// === HELPER FUNCTIONS ===

func (t *ResponseTemplates) describeState(state OrderState) string {
	switch state {
	case OrderStateNone:
		return "don't have an active order"
	case OrderStateBrowsing:
		return "are browsing our products"
	case OrderStateCartActive:
		return "have items in your cart"
	case OrderStateCheckoutPending:
		return "have an order ready for checkout"
	case OrderStatePaymentSent:
		return "have a pending payment"
	case OrderStatePaid:
		return "have completed your payment"
	case OrderStateFulfilled:
		return "have a completed order"
	case OrderStateCancelled:
		return "cancelled your previous order"
	case OrderStateExpired:
		return "have an expired order"
	default:
		return "have an order in progress"
	}
}

// GetTemplateForAction returns the appropriate template based on action result.
func (t *ResponseTemplates) GetTemplateForAction(action V2ActionType, result *ActionResult) string {
	if result == nil || !result.Success {
		if result != nil && result.ErrorMessage != "" {
			// Use error as hint for conflicting state
			return t.ConflictingState(OrderStateNone, nil)
		}
		return t.SystemError()
	}

	switch action {
	case V2ActionCreateOrder, V2ActionUpdateOrder:
		if result.Order != nil {
			return t.OrderCreated("your items", result.Order.TotalAmount)
		}
		return t.OrderCreated("your items", 0)

	case V2ActionLockOrder:
		if result.Order != nil {
			return t.OrderLocked(result.Order.OrderNumber, result.Order.TotalAmount)
		}
		return t.OrderLocked("", 0)

	case V2ActionSendPayment:
		if result.PaymentInfo != nil && result.Order != nil {
			return t.PaymentInstructions(result.PaymentInfo, result.Order.OrderNumber)
		}
		return "Please let me get you the payment details..."

	case V2ActionConfirmPayment:
		if result.Order != nil {
			return t.PaymentConfirmed(result.Order.OrderNumber)
		}
		return t.PaymentConfirmed("")

	case V2ActionCancelOrder:
		return t.OrderCancelled()

	case V2ActionAskClarification:
		return t.LowConfidence(nil)

	case V2ActionRejectIntent:
		return t.ConflictingState(OrderStateNone, nil)

	default:
		return "" // No fallback needed, AI can respond
	}
}
