package conversation

import (
	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/intent"
	"go.uber.org/zap"
)

// EvaluateTransactionState determines the next TransactionState based on:
// 1. Current State (from previous turn)
// 2. User Intent (what they just said)
// 3. Payment Status (signal from order)
//
// PHASE 2: This function is PURE, DETERMINISTIC, and AUTHORITATIVE.
// It enforces forward-only transitions with strict validation.
func EvaluateTransactionState(
	current domain.TransactionState,
	userIntent intent.IntentType,
	paymentStatus domain.PaymentStatus,
	logger *zap.Logger,
) domain.TransactionState {

	next := current

	switch current {
	// Browsing -> Committed
	// Triggered by order-commitment signals (not just keywords)
	case domain.TransactionBrowsing:
		if isCommitmentIntent(userIntent) {
			next = domain.TransactionCommitted
			logTransition(logger, current, next, "intent_commitment")
		}

	// Committed -> AwaitingPayment
	// Payment questions are treated as order-related by default
	case domain.TransactionCommitted:
		if isPaymentIntent(userIntent) {
			next = domain.TransactionAwaitingPayment
			logTransition(logger, current, next, "intent_payment_request")
		}

	// AwaitingPayment -> Paid
	// ONLY triggered by actual payment completion, never by language alone
	case domain.TransactionAwaitingPayment:
		if paymentStatus == domain.PaymentCompleted {
			next = domain.TransactionPaid
			logTransition(logger, current, next, "payment_confirmed")
		}

	// Paid: Terminal state
	// No transitions allowed (future: explicit reset command)
	case domain.TransactionPaid:
		// Remain in Paid state
		// Any regression attempts will be caught by ValidateTransactionState
	}

	// PHASE 2: Internal validation to prevent regressions
	// If proposed transition is invalid, clamp to current state
	if !ValidateTransactionState(current, next, logger) {
		if logger != nil {
			logger.Warn("transaction_state_regression_prevented",
				zap.String("current", string(current)),
				zap.String("proposed", string(next)),
				zap.String("intent", string(userIntent)),
			)
		}
		return current // Clamp to current state
	}

	return next
}

// isCommitmentIntent detects order-commitment signals
// PHASE 2: Expanded to include broader commitment intents
func isCommitmentIntent(i intent.IntentType) bool {
	return i == intent.IntentConfirmOrder ||
		i == intent.IntentPlaceOrder ||
		i == intent.IntentAcceptPrice ||
		i == intent.IntentOrderStart // New session with intent to order
}

// isPaymentIntent detects payment-related questions/requests
// PHASE 2: Payment questions are treated as order-related by default
func isPaymentIntent(i intent.IntentType) bool {
	return i == intent.IntentRequestPayment ||
		i == intent.IntentConfirmPayment
}

// IsPaymentIntent is the exported version for use in pipeline (Phase 6)
// Detects when customer is expressing payment intent
func IsPaymentIntent(i intent.IntentType) bool {
	return isPaymentIntent(i)
}

// ValidateTransactionState checks for illegal state transitions or regressions.
// It returns true if the transition is valid, false otherwise.
//
// INVARIANTS (Phase 5):
// 1. TransactionState >= Committed: Order context must never be scrubbed (Handled in Pipeline).
// 2. TransactionState may only move forward (Browsing -> Committed -> AwaitingPayment -> Paid).
// 3. Resets (Paid -> Browsing) must be explicit.
func ValidateTransactionState(prev, next domain.TransactionState, logger *zap.Logger) bool {
	// Allow same state
	if prev == next {
		return true
	}

	// Define state weight for simple comparison
	weight := func(s domain.TransactionState) int {
		switch s {
		case domain.TransactionBrowsing:
			return 0
		case domain.TransactionCommitted:
			return 1
		case domain.TransactionAwaitingPayment:
			return 2
		case domain.TransactionPaid:
			return 3
		default:
			return -1 // Unknown
		}
	}

	wPrev := weight(prev)
	wNext := weight(next)

	// Forward movement is always allowed
	if wNext > wPrev {
		return true
	}

	// Regressions (wNext < wPrev)
	// Only valid reset is Paid -> Browsing (or explicit new session, handled by caller reset)
	if prev == domain.TransactionPaid && next == domain.TransactionBrowsing {
		// Allowed reset after completion
		return true
	}

	// Any other regression is suspicious
	if logger != nil {
		logger.Warn("transaction_state_regression_detected",
			zap.String("from", string(prev)),
			zap.String("to", string(next)),
			zap.String("reason", "illegal_backward_transition"),
		)
	}

	return false
}

func logTransition(logger *zap.Logger, from, to domain.TransactionState, reason string) {
	if logger != nil && from != to {
		logger.Info("transaction_state_transition",
			zap.String("from", string(from)),
			zap.String("to", string(to)),
			zap.String("reason", reason),
		)
	}
}
