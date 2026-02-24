package order

import (
	"context"
	"errors"

	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/ai_v4/decision"
	"github.com/similadayo/brancr/internal/ai_v4/executor"
	"github.com/similadayo/brancr/internal/ai_v4/types"
	"go.uber.org/zap"
)

// OrderFlow orchestrates the safe execution of order-related actions.
// It acts as a safety middleware before the mechanical executor.
type OrderFlow struct {
	executor *executor.ExecutorAdapter
	logger   *zap.Logger
}

// NewOrderFlow creates a new Order Flow wrapper.
func NewOrderFlow(
	executor *executor.ExecutorAdapter,
	logger *zap.Logger,
) *OrderFlow {
	return &OrderFlow{
		executor: executor,
		logger:   logger,
	}
}

// Run executes the decision with strict ordering and guards.
func (f *OrderFlow) Run(
	ctx context.Context,
	dec *decision.Decision,
	snapshot *v4ctx.ContextSnapshot,
) (*executor.ExecutionResult, error) {

	// Create a working set of allowed actions to filter/sort
	// We want to verify presense of actions first.
	actionSet := make(map[types.ActionType]bool)
	for _, a := range dec.AllowedActions {
		actionSet[a] = true
	}

	// RULE 1: SINGLE ACTIVE ORDER
	// If order is active, silently drop initialization
	if snapshot.Operational.ActiveOrderID != nil {
		if actionSet[types.ActionInitializeOrder] {
			delete(actionSet, types.ActionInitializeOrder)
			f.logger.Debug("order_flow_rule_dropped_init", zap.String("reason", "active_order_exists"))
		}
	}

	// RULE 2: ADD ITEMS ONLY WITH ACTIVE ORDER (Strict)
	// If no order active, creating items creates a phantom state.
	// HOWEVER: If InitializeOrder is ALSO present (and allowed), then it is valid.
	// So we check: ActiveOrder == nil AND InitializeOrder is NOT in the (filtered) set.
	if snapshot.Operational.ActiveOrderID == nil && !actionSet[types.ActionInitializeOrder] {
		// Logic check: If we have ActionAddToOrder or ActionUpdateOrder
		if actionSet[types.ActionAddToOrder] || actionSet[types.ActionUpdateOrder] {
			// This is a logic error in appropriate design, AI should have proposed Init first or Engine blocked it.
			// But here we enforce.
			return nil, errors.New("cannot add/update order without active order or initialization")
		}
	}

	// RULE 3: CONFIRM ONLY ONCE
	if snapshot.ActiveOrder != nil && snapshot.ActiveOrder.Status == "confirmed" {
		if actionSet[types.ActionConfirmOrder] {
			delete(actionSet, types.ActionConfirmOrder)
			f.logger.Debug("order_flow_rule_dropped_confirm", zap.String("reason", "already_confirmed"))
		}
	}

	// RULE 4: EXECUTION ORDER (Strict Re-Ordering)
	// We reconstruct AllowedActions in the enforced order.
	orderedActions := []types.ActionType{}

	// Priority 1: Initialize
	if actionSet[types.ActionInitializeOrder] {
		orderedActions = append(orderedActions, types.ActionInitializeOrder)
	}
	// Priority 2: Add
	if actionSet[types.ActionAddToOrder] {
		orderedActions = append(orderedActions, types.ActionAddToOrder)
	}
	// Priority 3: Update
	if actionSet[types.ActionUpdateOrder] {
		orderedActions = append(orderedActions, types.ActionUpdateOrder)
	}
	// Priority 4: Confirm
	if actionSet[types.ActionConfirmOrder] {
		orderedActions = append(orderedActions, types.ActionConfirmOrder)
	}

	// Add any remaining non-order actions (e.g. Payment, Cancel) at the end
	// or handled separately? The instruction said "Executes order-related actions".
	// But the ExecutorAdapter processes `dec.AllowedActions`.
	// If we overwrite `dec.AllowedActions`, we must ensure we don't drop other allowed actions (like RequestPayment).

	// Let's iterate original list to find others not in our priority list
	otherActions := []types.ActionType{}
	for _, a := range dec.AllowedActions {
		// If it's not one of the order actions we handled
		if a != types.ActionInitializeOrder &&
			a != types.ActionAddToOrder &&
			a != types.ActionUpdateOrder &&
			a != types.ActionConfirmOrder {
			// Check if it was in our actionSet (i.e. not dropped by other logic?
			// Only Rule 1 and 3 drop specific actions.
			// Rule 2 returns error.
			// So if it's in original, preserve it.
			otherActions = append(otherActions, a)
		}
	}

	// Combine: Ordered Order Actions + Others
	// Payment usually comes after Confirm.
	// We will append others after.
	finalActions := append(orderedActions, otherActions...)

	// Update Decision - mutation is acceptable for this local scope passed to adapter
	dec.AllowedActions = finalActions

	// EXECUTE
	return f.executor.Execute(ctx, dec, snapshot)
}
