package types

import (
	"github.com/similadayo/brancr/internal/ai_v4/industry"
)

// StateController manages conversation mode transitions based on the canonical chat flow.
// Flow: DISCOVERY → SUPPORT → SALES → NEGOTIATION → ORDER_COLLECTION → PAYMENT_AWARENESS → ESCALATION

// DetermineNextMode determines the next conversation mode based on intent and current state.
// This implements the state machine transitions from the chat flow spec.
func DetermineNextMode(currentMode ConversationMode, intent Intent, signals *ContextSignals, cap industry.Capability) ConversationMode {
	// Fix 3 (Set 8): 🔒 HARD MODE LOCK AFTER ORDER COMMIT
	if signals != nil && signals.OrderCommitted {
		return ModePaymentAwareness
	}

	// If already escalated or in escalation mode, stay there (terminal state)
	if currentMode == ModeEscalation {
		return ModeEscalation
	}

	// Check for immediate escalation triggers
	// Fix 3 (Set 6): NEVER ESCALATE DURING ORDER COLLECTION (FINAL GUARD)
	if currentMode != ModeOrderCollection {
		if shouldEscalate(intent, signals) {
			return ModeEscalation
		}
	}

	// Fix 4 (Set 7): HARD RULE: Order committed → payment awareness
	if signals != nil && signals.OrderCommitted {
		return ModePaymentAwareness
	}

	// Default entry point
	if currentMode == "" {
		currentMode = ModeDiscovery
	}

	// Intent-based transitions
	switch intent {
	// Escalation intents → immediate escalation
	case IntentRefund, IntentHumanRequest, IntentComplaint:
		return ModeEscalation

	// Greeting/Unknown → stay in discovery or enter support
	case IntentGreeting, IntentUnknown:
		if currentMode == ModeDiscovery {
			return ModeSupport
		}
		return currentMode

	// Information intents → support mode
	case IntentInquiry, IntentOrderStatus:
		return ModeSupport

	// Price check → could be support or lead to sales
	case IntentPriceCheck:
		if currentMode == ModeNegotiation {
			return ModeNegotiation
		}
		return ModeSales

	// Purchase intent → sales or order collection
	case IntentPurchase:
		// Check capabilities!
		if cap.SupportsOrdering {
			if currentMode == ModeSales || currentMode == ModeNegotiation {
				return ModeOrderCollection
			}
			return ModeSales
		} else if cap.SupportsBooking {
			return ModeSupport // Booking is handled via support/inquiry flow for now
		}
		return ModeSupport

	// Negotiation intent → negotiation mode
	case IntentNegotiation:
		if cap.SupportsNegotiation {
			return ModeNegotiation
		}
		return currentMode // Ignore negotiation if not supported

	// Thank you / goodbye → could be completion
	case IntentThankYou, IntentGoodbye:
		if currentMode == ModeOrderCollection {
			return ModePaymentAwareness
		}
		return currentMode
	}

	// Mode-specific transitions when intent doesn't clearly indicate
	switch currentMode {
	case ModeDiscovery:
		return ModeSupport

	case ModeSupport:
		// Stay in support unless buying signals detected
		if signals != nil && signals.HasBuyingIntent {
			if cap.SupportsOrdering {
				return ModeSales
			}
		}
		return ModeSupport

	case ModeSales:
		// Stay in sales until clear order intent or negotiation
		return ModeSales

	case ModeNegotiation:
		// Stay until agreement or deadlock
		if signals != nil && signals.NegotiationDeadlock {
			return ModeEscalation
		}
		return ModeNegotiation

	case ModeOrderCollection:
		// Move to payment when order is confirmed
		if signals != nil && signals.OrderCommitted {
			return ModePaymentAwareness
		}
		return ModeOrderCollection

	case ModePaymentAwareness:
		// Fix 4 (Set 9): PAYMENT MODE SHOULD NOT AUTO-ESCALATE
		return ModePaymentAwareness
	}

	return currentMode
}

// shouldEscalate checks if immediate escalation is needed based on signals
func shouldEscalate(intent Intent, signals *ContextSignals) bool {
	// Intent-based escalation
	switch intent {
	case IntentRefund, IntentHumanRequest, IntentComplaint:
		return true
	}

	// Signal-based escalation
	if signals != nil {
		// Escalate on high frustration
		if signals.DetectedEmotion == EmotionAngry || signals.DetectedEmotion == EmotionFrustrated {
			return true
		}

		// Escalate on negotiation deadlock
		if signals.NegotiationDeadlock {
			return true
		}

		// Escalate on low confidence
		if signals.AIConfidenceLow {
			return true
		}
	}

	return false
}

// GetModeDisplayName returns a human-readable name for the mode
func GetModeDisplayName(mode ConversationMode) string {
	switch mode {
	case ModeDiscovery:
		return "Discovery"
	case ModeSupport:
		return "Support"
	case ModeSales:
		return "Sales"
	case ModeNegotiation:
		return "Negotiation"
	case ModeOrderCollection:
		return "Order Collection"
	case ModePaymentAwareness:
		return "Payment"
	case ModeEscalation:
		return "Escalated"
	default:
		return "Unknown"
	}
}

// IsTerminalMode returns true if the mode is terminal (AI should stop)
func IsTerminalMode(mode ConversationMode) bool {
	return mode == ModeEscalation
}

// CanTransitionTo checks if a transition from current to target mode is valid
func CanTransitionTo(current, target ConversationMode) bool {
	// Can always escalate
	if target == ModeEscalation {
		return true
	}

	// Cannot leave escalation
	if current == ModeEscalation {
		return false
	}

	// All other transitions are allowed for now
	// (Could add more specific rules here)
	return true
}
