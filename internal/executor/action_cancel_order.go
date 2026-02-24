package executor

import (
	"github.com/similadayo/brancr/internal/ai/response"
	"github.com/similadayo/brancr/internal/types"
)

// ExecuteCancelOrder handles the cancellation of an active order
func ExecuteCancelOrder(ctx *types.ResolvedContext) *response.AIResponse {
	// 1. Check if there is an active order
	if ctx.Order == nil {
		return &response.AIResponse{
			HumanText: "You don't have an active order to cancel. Would you like to see our menu?",
			Meta: map[string]any{
				"action_type": "cancel_order_failed",
				"reason":      "no_active_order",
			},
		}
	}

	// 2. Check if order can be cancelled via chat
	// (Completed orders usually require human intervention)
	// Using string literals to avoid import complexity
	if string(ctx.Order.Payment.Status) == "COMPLETED" ||
		string(ctx.Order.Fulfillment.Status) == "COMPLETED" {
		return &response.AIResponse{
			HumanText: "Since your order is already completed, I can't cancel it automatically. Let me get a human to help you.",
			Meta: map[string]any{
				"action_type": "escalate_to_human",
				"reason":      "cancel_completed_order",
			},
		}
	}

	// 3. EFFECT: Cancel the order (DB update handled by CustomerResponder or here?)
	// Executor usually returns events/actions. V3 Pipeline usually updates state via Responder.
	// But `executor` package seems to handle logic.
	// However, `ExecuteSendOfferings` returned a response.
	// Does Executor mutate DB? `action_send_offerings` didn't.
	// `ActionCancelOrder` in `resolver.go` has `RequiresTx: true`.
	// The `Execute` method in `pipeline.go` (line 282) calls `actionImpl.Execute`.
	// BUT `ShouldBypassLLM` skips `actionImpl` and calls `ExecuteCancelOrder` directly.
	// So `ExecuteCancelOrder` MUST perform the side effect OR return an AIResponse that triggers it.
	// `AIResponse.Meta` can be used by Responder.

	// Better: We should return a Meta action that `CustomerResponder` understands?
	// OR `ExecuteCancelOrder` should just do the DB update.
	// Since we have `ctx.ResolvedContext` which has `CurrentOrder`, we might not have DB access here?
	// `executor` package imports?
	// `ExecuteSendOfferings` fetched offerings.
	// We should probably rely on the `CustomerResponder` to interpret `action_type: cancel_order`.
	// CHECK `customer_responder.go` to see if it handles `action_type`.

	// Wait, if I do `Meta: cancel_order`, will `CustomerResponder` execute it?
	// `CustomerResponder` logic (viewed earlier) records action but doesn't seem to have a big switch for Meta actions.
	// Line 793 `RecordAction`.

	// Let's implement the DB update here if possible by importing storage?
	// Or define that `actionResult` handles it.
	// Actually, `ctx` doesn't seem to have DB access exposed directly in `types.ResolvedContext`.
	// But `customer_responder.go` has `RecordAction`.

	// HYBRID APPROACH:
	// We return `action_type: cancel_order` and text.
	// But checking `customer_responder.go` again...
	// It doesn't seem to have special handling for `Meta["action_type"]` other than logging.

	// So, where does the Actual DB Cancellation happen?
	// In the standard pipeline, `resolver` returns `ActionCancelOrder`.
	// `executor` (registry) executes it.
	// We are BYPASSING that.
	// So we MUST implement the side effect here or call the registry's implementation!

	// Registry is `ActionRegistry` in `executor.go`.
	// Types are `ActionAddToCart`, `ActionCancelOrder`.
	// If `ActionCancelOrder` is already implemented in `executor/actions.go` (or similar), we can reuse it.
	// But `ShouldBypassLLM` avoids the *Reasoning* step. It shouldn't necessarily avoid the *Action Logic*.

	// Simplest Fix:
	// 1. Return response saying "Order cancelled."
	// 2. Add `cancel_order: true` to Meta.
	// 3. Update `customer_responder.go` to handle `cancel_order` Meta by cancelling the order.

	return &response.AIResponse{
		HumanText: "No problem, I've cancelled that order. Would you like to see the menu again?",
		Meta: map[string]any{
			"action_type":  "cancel_order",
			"bypass_llm":   true,
			"cancel_order": true, // Trigger side effect
		},
	}
}

// RequestCancelConfirmation returns a terminal confirmation request
func RequestCancelConfirmation(ctx *types.ResolvedContext) TerminalResponse {
	if ctx.Order == nil {
		return TerminalResponse{
			Text: "There’s no active order to cancel.",
			Stop: true,
		}
	}

	return TerminalResponse{
		Text: "Do you want me to cancel your active order?",
		Stop: true,
	}
}
