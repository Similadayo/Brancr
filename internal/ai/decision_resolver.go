package ai

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// DecisionResolver is Stage 3 of the v2.0 pipeline.
// It is DECISION-AUTHORITATIVE but DATA-READ-ONLY.
// All business decisions are made here, but no writes occur.
type DecisionResolver struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewDecisionResolver creates a new decision resolver.
func NewDecisionResolver(db *gorm.DB, logger *zap.Logger) *DecisionResolver {
	return &DecisionResolver{
		DB:     db,
		Logger: logger,
	}
}

// ResolverInput is the input to the Decision Resolver.
type ResolverInput struct {
	TenantID       uuid.UUID
	CustomerID     uuid.UUID
	ConversationID uuid.UUID
	Platform       string
	Intent         CanonicalIntent
	Confidence     float64
	Entities       map[string]interface{} // UNTRUSTED - must be validated
}

// ResolvedAction is the output of the Decision Resolver.
type ResolvedAction struct {
	ActionType     V2ActionType
	ActiveOrder    *storage.Order
	CurrentState   OrderState
	TargetState    OrderState
	ValidatedData  map[string]interface{} // Validated entities
	ValidationErr  error                  // If action is invalid
	FallbackReason string                 // Why fallback was triggered
}

// Resolve determines what action to take based on intent and current state.
// This is the core decision logic - no AI, pure deterministic code.
func (r *DecisionResolver) Resolve(ctx context.Context, input ResolverInput) (*ResolvedAction, error) {
	result := &ResolvedAction{
		ActionType:    V2ActionNone,
		ValidatedData: make(map[string]interface{}),
	}

	// Step 1: Check confidence threshold
	if input.Confidence < ConfidenceThreshold {
		result.ActionType = V2ActionAskClarification
		result.FallbackReason = fmt.Sprintf("confidence %.2f below threshold %.2f", input.Confidence, ConfidenceThreshold)
		r.Logger.Info("🔍 Low confidence, asking clarification",
			zap.String("intent", string(input.Intent)),
			zap.Float64("confidence", input.Confidence))
		return result, nil
	}

	// Step 2: Validate intent is canonical
	if !IsValidIntent(string(input.Intent)) {
		result.ActionType = V2ActionAskClarification
		result.FallbackReason = "unknown intent"
		return result, nil
	}

	// Step 3: Fetch active order (SSOT lookup)
	activeOrder, currentState := r.getActiveOrder(ctx, input.TenantID, input.CustomerID, input.Platform)
	result.ActiveOrder = activeOrder
	result.CurrentState = currentState

	// Step 4: Validate entities if intent requires them
	if err := r.validateEntities(ctx, input, result); err != nil {
		result.ActionType = V2ActionAskClarification
		result.ValidationErr = err
		result.FallbackReason = err.Error()
		return result, nil
	}

	// Step 5: Determine action based on intent and state
	action, targetState, err := r.determineAction(input.Intent, currentState, activeOrder)
	if err != nil {
		result.ActionType = V2ActionRejectIntent
		result.ValidationErr = err
		result.FallbackReason = err.Error()
		return result, nil
	}

	result.ActionType = action
	result.TargetState = targetState

	r.Logger.Info("✅ Decision resolved",
		zap.String("intent", string(input.Intent)),
		zap.String("current_state", string(currentState)),
		zap.String("action", string(action)),
		zap.String("target_state", string(targetState)))

	return result, nil
}

// getActiveOrder fetches the single active order for this conversation.
// Implements SSOT: One active order per (tenant + customer + platform).
func (r *DecisionResolver) getActiveOrder(ctx context.Context, tenantID, customerID uuid.UUID, platform string) (*storage.Order, OrderState) {
	var order storage.Order

	// SSOT lookup - find active order (POSITIVE WHITELIST ONLY)
	// Only these statuses are considered "active" for v2:
	// - draft: Order being built
	// - pending: Awaiting confirmation
	// - building: Items being added (legacy compatibility)
	// - locked: Checkout in progress
	// - awaiting_payment: Payment instructions sent
	// CRITICAL: "confirmed", "paid", "fulfilled", "cancelled", "expired" are TERMINAL
	err := r.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ? AND platform = ?", tenantID, customerID, platform).
		Where("status IN ?", []string{"draft", "pending", "building", "locked", "awaiting_payment"}).
		Order("created_at DESC").
		First(&order).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, OrderStateNone
		}
		r.Logger.Warn("Error fetching active order", zap.Error(err))
		return nil, OrderStateNone
	}

	// Map legacy status to v2 state
	state := MapLegacyStatus(order.Status)

	// SAFETY ASSERTION: Terminal orders must NEVER be returned as active
	// If this panics, it means the SSOT whitelist query is broken
	if state == OrderStatePaid || state == OrderStateCancelled || state == OrderStateFulfilled || state == OrderStateExpired {
		r.Logger.Error("🚨 CRITICAL: Terminal order returned as active - SSOT query is broken!",
			zap.Stringer("order_id", order.ID),
			zap.String("status", order.Status),
			zap.String("state", string(state)))
		// Return nil to treat as no active order (graceful degradation)
		return nil, OrderStateNone
	}

	return &order, state
}

// validateEntities validates untrusted entities from AI against database.
func (r *DecisionResolver) validateEntities(ctx context.Context, input ResolverInput, result *ResolvedAction) error {
	// Validate product if specified
	if productName, ok := input.Entities["product_name"].(string); ok && productName != "" {
		product, err := r.findProduct(ctx, input.TenantID, productName)
		if err != nil {
			return fmt.Errorf("product not found: %s", productName)
		}
		result.ValidatedData["product"] = product
		result.ValidatedData["product_id"] = product.ID
		result.ValidatedData["product_name"] = product.Name
		result.ValidatedData["product_price"] = product.Price
	}

	// Validate quantity if specified
	if quantity, ok := input.Entities["quantity"].(float64); ok {
		if quantity <= 0 || quantity > 100 {
			return fmt.Errorf("invalid quantity: %.0f (must be 1-100)", quantity)
		}
		result.ValidatedData["quantity"] = int(quantity)
	} else {
		// Default quantity is 1
		result.ValidatedData["quantity"] = 1
	}

	return nil
}

// findProduct searches for a product by name (fuzzy match).
func (r *DecisionResolver) findProduct(ctx context.Context, tenantID uuid.UUID, name string) (*storage.Product, error) {
	var product storage.Product

	// Try exact match first
	err := r.DB.WithContext(ctx).
		Where("tenant_id = ? AND LOWER(name) = LOWER(?)", tenantID, name).
		First(&product).Error

	if err == nil {
		return &product, nil
	}

	// Try fuzzy match (contains)
	err = r.DB.WithContext(ctx).
		Where("tenant_id = ? AND LOWER(name) LIKE LOWER(?)", tenantID, "%"+name+"%").
		First(&product).Error

	if err != nil {
		return nil, err
	}

	return &product, nil
}

// determineAction decides the action and target state based on intent and current state.
func (r *DecisionResolver) determineAction(intent CanonicalIntent, currentState OrderState, activeOrder *storage.Order) (V2ActionType, OrderState, error) {
	switch intent {
	case IntentGreeting, IntentGoodbye, IntentClarify, IntentBrowseProducts, IntentAskProductInfo, IntentAskPrice, IntentViewCart, IntentTrackOrder:
		// Informational intents - no state change
		return V2ActionNone, currentState, nil

	case IntentAddToCart:
		if currentState == OrderStateNone || currentState == OrderStateBrowsing {
			// No order exists - create one
			return V2ActionCreateOrder, OrderStateCartActive, nil
		}
		if currentState == OrderStateCartActive {
			// Order exists and is editable - update it
			return V2ActionUpdateOrder, OrderStateCartActive, nil
		}
		// Order exists but is not editable
		return V2ActionRejectIntent, currentState, fmt.Errorf("cannot add items: order is in %s state", currentState)

	case IntentRemoveFromCart:
		if currentState != OrderStateCartActive {
			return V2ActionRejectIntent, currentState, fmt.Errorf("cannot remove items: no active cart")
		}
		return V2ActionUpdateOrder, OrderStateCartActive, nil

	case IntentCheckout:
		if currentState != OrderStateCartActive {
			if currentState == OrderStateNone {
				return V2ActionRejectIntent, currentState, fmt.Errorf("cannot checkout: no items in cart")
			}
			return V2ActionRejectIntent, currentState, fmt.Errorf("cannot checkout: order is in %s state", currentState)
		}
		return V2ActionLockOrder, OrderStateCheckoutPending, nil

	case IntentAskPaymentMethod:
		if currentState == OrderStateCheckoutPending {
			return V2ActionSendPayment, OrderStatePaymentSent, nil
		}
		if currentState == OrderStatePaymentSent {
			// Already sent - resend
			return V2ActionSendPayment, OrderStatePaymentSent, nil
		}
		if currentState == OrderStateCartActive {
			// Auto-checkout first, then send payment
			return V2ActionSendPayment, OrderStatePaymentSent, nil
		}
		return V2ActionRejectIntent, currentState, fmt.Errorf("cannot send payment: order is in %s state", currentState)

	case IntentConfirmPayment:
		if currentState != OrderStatePaymentSent {
			if currentState == OrderStateNone {
				return V2ActionRejectIntent, currentState, fmt.Errorf("no pending payment to confirm")
			}
			return V2ActionRejectIntent, currentState, fmt.Errorf("cannot confirm payment: order is in %s state", currentState)
		}
		return V2ActionConfirmPayment, OrderStatePaid, nil

	case IntentCancelOrder:
		if currentState == OrderStateNone {
			return V2ActionRejectIntent, currentState, fmt.Errorf("no order to cancel")
		}
		if currentState.IsTerminal() {
			return V2ActionRejectIntent, currentState, fmt.Errorf("cannot cancel: order already %s", currentState)
		}
		return V2ActionCancelOrder, OrderStateCancelled, nil

	case IntentAffirm:
		// Context-dependent - check what we're affirming
		if currentState == OrderStateCartActive {
			// Affirming checkout
			return V2ActionLockOrder, OrderStateCheckoutPending, nil
		}
		return V2ActionNone, currentState, nil

	case IntentReject:
		// Context-dependent - could be cancel or just "no"
		return V2ActionNone, currentState, nil

	case IntentUnknown:
		return V2ActionAskClarification, currentState, nil

	default:
		return V2ActionAskClarification, currentState, nil
	}
}
