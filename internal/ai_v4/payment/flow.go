package payment

import (
	"context"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/decision"
	"github.com/similadayo/brancr/internal/ai_v4/executor"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"go.uber.org/zap"
)

// PaymentFlow orchestrates payment-related actions and state transitions.
type PaymentFlow struct {
	executor *executor.ExecutorAdapter
	logger   *zap.Logger
}

// NewPaymentFlow creates a new Payment Flow wrapper.
func NewPaymentFlow(
	executor *executor.ExecutorAdapter,
	logger *zap.Logger,
) *PaymentFlow {
	return &PaymentFlow{
		executor: executor,
		logger:   logger,
	}
}

// Run executes the payment decision with rules and guards.
func (f *PaymentFlow) Run(
	ctx context.Context,
	dec *decision.Decision,
	snapshot *v4ctx.ContextSnapshot,
) (*executor.ExecutionResult, error) {

	// Working set of actions
	finalActions := []types.ActionType{}

	// Flags for manual handling
	isPendingPayment := false
	hasActiveOrder := snapshot.Operational.ActiveOrderID != nil

	// Pre-scan actions to handle Rules 1 & 2
	for _, a := range dec.AllowedActions {
		switch a {
		case types.ActionSendPaymentInstructions:
			// RULE 1: PAYMENT REQUEST ONCE
			// Map to ActionRequestPayment, but check if already sent
			status := "none"
			if snapshot.Operational.PaymentState != nil {
				status = snapshot.Operational.PaymentState.Status
			}
			if status == "requested" || status == "confirmed" || status == "proof_received" {
				f.logger.Debug("payment_flow_drop_duplicate_request", zap.String("status", status))
				continue
			}
			// Map to ActionRequestPayment for executor
			finalActions = append(finalActions, types.ActionRequestPayment)

		case types.ActionCreatePendingPayment:
			// RULE 2: PAYMENT PROOF HANDLING
			if hasActiveOrder {
				isPendingPayment = true
				// We don't pass this to executor as it's likely a state transition we handle via result
				// unless executor supports it. Assuming safe to drop from execution list and handle via result.
			}

		case types.ActionRequestPayment:
			// Same check as SendPaymentInstructions
			status := "none"
			if snapshot.Operational.PaymentState != nil {
				status = snapshot.Operational.PaymentState.Status
			}
			if status == "requested" || status == "confirmed" || status == "proof_received" {
				continue
			}
			finalActions = append(finalActions, a)

		default:
			finalActions = append(finalActions, a)
		}
	}

	// RULE 3: ORDER LOCK AFTER PAYMENT INTENT
	// If payment proof detected (either historically or just now)
	isProofReceived := isPendingPayment
	if snapshot.Operational.PaymentState != nil && snapshot.Operational.PaymentState.Status == "proof_received" {
		isProofReceived = true
	}

	if isProofReceived {
		// Filter out mutation actions
		filtered := []types.ActionType{}
		for _, a := range finalActions {
			if a == types.ActionAddToOrder || a == types.ActionUpdateOrder || a == types.ActionCancelOrder {
				f.logger.Debug("payment_flow_lock_dropped_mutation", zap.String("action", string(a)))
				continue
			}
			filtered = append(filtered, a)
		}
		finalActions = filtered
	}

	// Apply modified actions to decision (local scope)
	dec.AllowedActions = finalActions

	// EXECUTE
	result, err := f.executor.Execute(ctx, dec, snapshot)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = &executor.ExecutionResult{
			Messages: []string{},
		}
	}

	// RULE 4: ESCALATION PREPARATION & STATE MARKING
	if isPendingPayment {
		// Mark order as payment pending via explicit message signal
		result.Messages = append(result.Messages, "payment_proof_received")
	}

	return result, nil
}
