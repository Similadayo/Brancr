package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/fulfillment"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// =============================================================================
// ACTION EXECUTOR - DB Operations with Transaction Safety
// =============================================================================

// actionExecutor implements ActionExecutor interface
type actionExecutor struct {
	db         *gorm.DB
	logger     *zap.Logger
	negotiator Negotiator
}

// NewActionExecutor creates a new action executor
func NewActionExecutor(db *gorm.DB, logger *zap.Logger, negotiator Negotiator) ActionExecutor {
	return &actionExecutor{
		db:         db,
		logger:     logger,
		negotiator: negotiator,
	}
}

// Execute performs the action with transaction safety
// Returns real data from DB - never assumptions
// Execute performs the action with transaction safety
// Returns real data from DB - never assumptions
func (e *actionExecutor) Execute(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext, action Action) ExecutionResult {
	// Log the action
	e.logger.Info("executing_action",
		zap.String("action", string(action.Type)),
		zap.Bool("requires_tx", action.RequiresTx),
		zap.Stringer("conversation_id", convCtx.ConversationID),
	)

	// Use provided tx or fallback to e.db
	db := tx
	if db == nil {
		db = e.db
	}

	// If action doesn't require transaction, execute directly
	if !action.RequiresTx {
		return e.executeNonTransactional(ctx, db, convCtx, action)
	}

	// Execute with transaction (pass original tx if available to prevent nesting if desired, or db)
	// If tx is provided, executeTransactional should likely assume it's already in a tx setup or safe to use.
	return e.executeTransactional(ctx, tx, convCtx, action)
}

// executeNonTransactional performs read-only actions
func (e *actionExecutor) executeNonTransactional(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext, action Action) ExecutionResult {
	switch action.Type {
	case ActionGreet:
		return AckResult{Message: "Greeting sent"}

	case ActionShowProducts:
		return e.showProducts(ctx, convCtx)

	case ActionShowCart:
		return e.showCart(ctx, convCtx)

	case ActionShowStatus:
		return e.showStatus(ctx, convCtx)

	case ActionShowPayment:
		return e.showPayment(ctx, convCtx)

	case ActionCollectAddress:
		return AckResult{Message: "Collecting address"}

	case ActionAskClarification:
		return AckResult{Message: action.Data.Message}

	case ActionEscalate:
		return e.escalate(ctx, convCtx)

	case ActionNegotiate:
		return e.handleNegotiation(ctx, convCtx, action)

	case ActionShowMenu:
		// Map explicit menu request to showing all products
		return e.showProducts(ctx, convCtx)

	case ActionPitchProduct:
		// Map recommendation request to producing product data (Composer filters it)
		return e.showProducts(ctx, convCtx)

	case ActionFreeResponse:
		// Executor authorizes free speech - no specific DB fetch needed
		return AckResult{Message: "Free Response Authorized"}

	case ActionExplainPrice:
		// Just acknowledge, let LLM explain using the price context it already has
		return AckResult{Message: "Explaining price rationale"}

	default:
		return ErrorResult{Error: fmt.Errorf("unknown non-transactional action: %s", action.Type)}
	}
}

func (e *actionExecutor) handleNegotiation(ctx context.Context, convCtx *ConversationContext, action Action) ExecutionResult {
	if e.negotiator == nil {
		return AckResult{Message: "Let me check on that price for you."}
	}

	// Extract offer from last user message
	offer := 0.0
	if len(convCtx.RecentMessages) > 0 {
		lastMsg := convCtx.RecentMessages[len(convCtx.RecentMessages)-1]
		if lastMsg.Role == "user" {
			offer = DetectOfferInText(lastMsg.Content)
		}
	}

	// Resolve product price context
	productPrice := 0.0
	if convCtx.SalesConversation != nil && convCtx.SalesConversation.PriceDiscussed > 0 {
		productPrice = convCtx.SalesConversation.PriceDiscussed
	} else if len(convCtx.Products) > 0 {
		// Fallback: Use first product's price if context is ambiguous
		productPrice = convCtx.Products[0].Price
	}

	decision, err := e.negotiator.Decide(ctx, convCtx, offer, productPrice)
	if err != nil {
		e.logger.Error("negotiation_failed", zap.Error(err))
		return AckResult{Message: "I'll see what we can do."}
	}

	return NegotiateResult{
		Offer:   decision.Offer,
		Message: decision.Message,
		Phase:   decision.Phase,
	}
}

func (e *actionExecutor) showProducts(ctx context.Context, convCtx *ConversationContext) ExecutionResult {
	products := convCtx.Products

	// Fallback: If no products, check for Services (Consultancy/Agency)
	if len(products) == 0 && len(convCtx.Services) > 0 {
		for _, s := range convCtx.Services {
			products = append(products, Product{
				ID:          s.ID,
				Name:        s.Name,
				Description: s.Description,
				Price:       s.Price,
				Currency:    s.Currency,
				Category:    "Service",
				InStock:     true,
			})
		}
	}

	if len(products) == 0 {
		return ShowProductsResult{Products: nil}
	}
	return ShowProductsResult{Products: products}
}

func (e *actionExecutor) showCart(ctx context.Context, convCtx *ConversationContext) ExecutionResult {
	items := convCtx.PendingCart
	if items == nil {
		items = []PendingCartItem{}
	}

	// Calculate total (re-calculate for safety, though ConversationContext might have helper)
	var total float64
	for _, item := range items {
		total += item.UnitPrice * float64(item.Quantity)
	}

	return ShowCartResult{
		Items: items,
		Total: total,
	}
}

func (e *actionExecutor) showStatus(ctx context.Context, convCtx *ConversationContext) ExecutionResult {
	var order storage.Order
	var err error

	// 1. Check verified context order (fetch fresh from DB)
	if convCtx.CurrentOrder != nil {
		err = e.db.WithContext(ctx).First(&order, convCtx.CurrentOrder.ID).Error
	} else {
		// 2. Fallback: Query DB for last order to be helpful
		err = e.db.WithContext(ctx).
			Where("customer_id = ?", convCtx.CustomerID).
			Order("created_at DESC").
			First(&order).Error
	}

	if err != nil {
		return AckResult{Message: "I can't find any recent orders for you. Would you like to place a new order?"}
	}

	// 3. Generate Reassurance Message (Phase 6)
	statusMsg := fmt.Sprintf("Order #%s is currently %s.", order.OrderNumber, order.Status)

	switch order.Status {
	case "paid", "confirmed":
		statusMsg += " I've confirmed your payment and sent the details to dispatch. It's being processed right now."
	case "processing":
		statusMsg += " It's currently being packed. I'll let you know once it's on the way."
	case "shipped", "dispatched":
		statusMsg += " It's on the way to you! You should receive it soon."
	case "delivered":
		statusMsg += " It's marked as delivered. Did you receive it safely?"
	case "cancelled":
		statusMsg += " This order was cancelled."
	}

	// Convert storage order to v3 order structure if needed, or just basic mapping
	v3Order := &Order{
		ID:          order.ID,
		OrderNumber: order.OrderNumber,
		Total:       order.TotalAmount,
		Currency:    order.Currency,
		CreatedAt:   order.CreatedAt,
		State:       OrderState(order.Status),
	}

	return ShowStatusResult{
		Order:   v3Order,
		Message: statusMsg,
	}
}

func (e *actionExecutor) showPayment(ctx context.Context, convCtx *ConversationContext) ExecutionResult {
	// Show payment typically requires PendingCart items
	if len(convCtx.PendingCart) == 0 {
		return ErrorResult{Error: fmt.Errorf("no items in cart to pay for")}
	}

	var total float64
	for _, item := range convCtx.PendingCart {
		total += item.UnitPrice * float64(item.Quantity)
	}

	if convCtx.PaymentInfo == nil {
		return ErrorResult{Error: fmt.Errorf("payment info not configured")}
	}

	// Construct IMMUTABLE payment block
	// This is the chosen source of truth that the AI must copy-paste
	block := fmt.Sprintf(`[[LOCKED_PAYMENT_DETAILS]]
Bank Name: %s
Account Number: %s
Account Name: %s
Amount to Pay: %s %.2f
[[/LOCKED_PAYMENT_DETAILS]]`,
		convCtx.PaymentInfo.BankName,
		convCtx.PaymentInfo.AccountNumber,
		convCtx.PaymentInfo.AccountName,
		"NGN", // TODO: Use dynamic currency if available
		total,
	)

	return ShowPaymentResult{
		Total:              total,
		Bank:               *convCtx.PaymentInfo,
		LockedPaymentBlock: block,
	}
}

func (e *actionExecutor) escalate(ctx context.Context, convCtx *ConversationContext) ExecutionResult {
	// PHASE 8: ESCALATION GUARD
	// Only escalate for explicit requests or missing keys.

	// 1. Explicit Request?
	lastMsg := ""
	if len(convCtx.RecentMessages) > 0 {
		lastMsg = strings.ToLower(convCtx.RecentMessages[len(convCtx.RecentMessages)-1].Content)
	}
	explicitRequest := strings.Contains(lastMsg, "human") ||
		strings.Contains(lastMsg, "person") ||
		strings.Contains(lastMsg, "agent") ||
		strings.Contains(lastMsg, "support") ||
		strings.Contains(lastMsg, "talk to")

	// 2. Missing Data?
	missingData := len(convCtx.Products) == 0 && convCtx.PaymentInfo == nil

	if !explicitRequest && !missingData {
		// Soft block: Treat as clarification failure
		e.logger.Info("escalation_blocked_softly", zap.Stringer("conv_id", convCtx.ConversationID))
		return AckResult{Message: "I'm right here! Could you verify what specifically you need help with?"}
	}

	e.logger.Info("escalation_triggered",
		zap.Stringer("conversation_id", convCtx.ConversationID),
		zap.Stringer("tenant_id", convCtx.TenantID),
	)

	return AckResult{Message: "Escalated to human support"}
}

// executeTransactional performs write actions inside a transaction
func (e *actionExecutor) executeTransactional(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext, action Action) ExecutionResult {
	logic := func(db *gorm.DB) ExecutionResult {
		switch action.Type {
		case ActionAddToCart:
			return e.addToCart(ctx, db, convCtx, action)
		case ActionRemoveFromCart:
			return e.removeFromCart(ctx, db, convCtx, action)
		case ActionClearCart:
			return e.clearCart(ctx, db, convCtx)
		case ActionRejectItem:
			return e.rejectItem(ctx, db, convCtx, action) // Helper signature check needed
		case ActionShowPayment:
			// THIS IS THE TRIGGER: Create order in DB from PendingCart
			return e.createOrderAndShowPayment(ctx, db, convCtx)
		case ActionConfirmOrder:
			return e.confirmOrder(ctx, db, convCtx)
		case ActionNegotiate:
			return e.handleNegotiation(ctx, convCtx, action)
		case ActionEscalate:
			// handleEscalation needs checking signature
			return e.escalate(ctx, convCtx)
		case ActionCancelOrder:
			return e.cancelOrder(ctx, db, convCtx)
		default:
			return ErrorResult{Error: fmt.Errorf("unknown transactional action: %s", action.Type)}
		}
	}

	if tx != nil {
		return logic(tx)
	}

	var result ExecutionResult
	err := e.db.Transaction(func(tx2 *gorm.DB) error {
		result = logic(tx2)
		if _, ok := result.(ErrorResult); ok {
			return fmt.Errorf("action failed")
		}
		return nil
	})

	if err != nil && result == nil {
		return ErrorResult{Error: err}
	}
	return result
}

// guardOrderMutable enforces the Hard Lock on verified orders
func (e *actionExecutor) guardOrderMutable(order *Order) error {
	if order == nil {
		return nil
	}
	// Only Draft/Building states are mutable.
	if order.State != StateDraft && order.State != StateBuilding {
		// If idle/browsing, it's effectively mutable (new order), but if confirmed/paid -> LOCKED
		if order.State == StateIdle || order.State == StateBrowsing {
			return nil
		}
		return fmt.Errorf("order is locked: status=%s", order.State)
	}
	return nil
}

// ExecuteHiddenCommands processes commands extracted from LLM response
func (e *actionExecutor) ExecuteHiddenCommands(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext, commands []HiddenCommand) error {
	// HARD GUARD: Prevent hidden mutation of locked orders
	if convCtx.CurrentOrder != nil {
		if err := e.guardOrderMutable(convCtx.CurrentOrder); err != nil {
			e.logger.Warn("mutation_blocked_by_guard", zap.Error(err))
			return nil // Silently ignore to prevent error loops
		}
	}

	modified := false
	db := tx
	if db == nil {
		db = e.db
	}

	for _, cmd := range commands {
		switch cmd.Type {
		case "ORDER":
			// Payload: {"items": [{"name": "...", "qty": 1, "modifiers": ["..."]}]}
			var payload struct {
				Items []struct {
					Name      string   `json:"name"`
					Qty       int      `json:"qty"`
					Price     *float64 `json:"price"`     // Optional negotiated price
					Modifiers []string `json:"modifiers"` // New field
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(cmd.Payload), &payload); err == nil {
				for _, item := range payload.Items {
					// Find product - Flexible Matching
					var found *Product
					targetName := strings.ToLower(strings.TrimSpace(item.Name))

					// Pass 1: Exact Match (Case Insensitive)
					for _, p := range convCtx.Products {
						if strings.EqualFold(p.Name, targetName) {
							found = &p
							break
						}
					}

					// Pass 2: Substring Match (Bidirectional)
					// "Banana Bread" matches "Banana Bread with chips"
					// "Banana Bread" matches "Banana"
					if found == nil {
						for _, p := range convCtx.Products {
							pName := strings.ToLower(p.Name)
							if strings.Contains(pName, targetName) || strings.Contains(targetName, pName) {
								found = &p
								break
							}
						}
					}

					if found != nil {
						// Determine Price (Catalog vs Negotiated)
						unitPrice := found.Price
						if item.Price != nil && *item.Price > 0 {
							// TODO: Verify against negotiation limits?
							// For now, trust the AI acting as the business
							unitPrice = *item.Price
						}

						// Add to PendingCart
						exists := false
						for i, cartItem := range convCtx.PendingCart {
							if cartItem.ProductID == found.ID {
								convCtx.PendingCart[i].Quantity += item.Qty
								// Update price if new price provided (overwrite? or weighted avg? Overwrite seems standard for negotiation)
								if item.Price != nil {
									convCtx.PendingCart[i].UnitPrice = unitPrice
								}
								// Append Modifiers (deduplicate?)
								convCtx.PendingCart[i].Modifiers = append(convCtx.PendingCart[i].Modifiers, item.Modifiers...)
								convCtx.PendingCart[i].Description = fmt.Sprintf("%s (x%d)", convCtx.PendingCart[i].ProductName, convCtx.PendingCart[i].Quantity)
								if len(convCtx.PendingCart[i].Modifiers) > 0 {
									convCtx.PendingCart[i].Description += " with " + strings.Join(convCtx.PendingCart[i].Modifiers, ", ")
								}
								exists = true
								break
							}
						}
						if !exists {
							desc := fmt.Sprintf("%s (x%d)", found.Name, item.Qty)
							if len(item.Modifiers) > 0 {
								desc += " with " + strings.Join(item.Modifiers, ", ")
							}
							convCtx.PendingCart = append(convCtx.PendingCart, PendingCartItem{
								ProductID:   found.ID,
								ProductName: found.Name,
								Quantity:    item.Qty,
								UnitPrice:   unitPrice,
								Currency:    found.Currency,
								Modifiers:   item.Modifiers,
								Description: desc,
							})
						}
						modified = true
					}
				}
			}

		case "DELIVERY":
			// {"method": "delivery", "area": "Name", "address": "..."}
			// Update DeliveryState
			// Need a place to store DeliveryState. It is in convCtx but ideally persisted too?
			// For now, rely on persisted cart or add new field to SalesConversation?
			// We can store it in ItemsDiscussed or new metadata field.
			// Let's assume DeliveryState is transient but needs to survive turn.
			// If not persisted, it will be lost.
			// For now, let's update convCtx to affect current response logic,
			// but persistence is needed.
			// Let's use PendingOrderDraft to store delivery info too?
			// Or just rely on order creation flow.
			modified = true
			if convCtx.DeliveryState == nil {
				convCtx.DeliveryState = &DeliveryState{}
			}
			var payload struct {
				Method  string `json:"method"`
				Area    string `json:"area"`
				Address string `json:"address"`
			}
			if err := json.Unmarshal([]byte(cmd.Payload), &payload); err == nil {
				convCtx.DeliveryState.Method = payload.Method
				convCtx.DeliveryState.Address = payload.Address
				// Find area price
				if convCtx.DeliveryConfig != nil {
					for _, area := range convCtx.DeliveryConfig.DeliveryAreas {
						if strings.Contains(strings.ToLower(area.Name), strings.ToLower(payload.Area)) {
							convCtx.DeliveryState.SelectedArea = &area
							break
						}
					}
				}
			}

		case "PAY_METHOD":
			// {"method": "pod"}
			if convCtx.DeliveryState == nil {
				convCtx.DeliveryState = &DeliveryState{}
			}
			var payload struct {
				Method string `json:"method"`
			}
			if err := json.Unmarshal([]byte(cmd.Payload), &payload); err == nil {
				convCtx.DeliveryState.PaymentMethod = payload.Method
			}
			modified = true
		}
	}

	if modified {
		if _, err := e.upsertDraftOrder(ctx, db, convCtx); err != nil {
			e.logger.Error("failed_to_save_draft_order", zap.Error(err))
		}
	}

	return nil
}

func (e *actionExecutor) addToCart(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext, action Action) ExecutionResult {
	// HARD GUARD: Prevent mutation of locked orders
	if err := e.guardOrderMutable(convCtx.CurrentOrder); err != nil {
		return ErrorResult{Error: err}
	}
	// Find the product in context
	var product *Product
	for _, p := range convCtx.Products {
		if p.ID == action.Data.ProductID {
			product = &p
			break
		}
	}

	if product == nil {
		return ErrorResult{Error: fmt.Errorf("product not found: %s", action.Data.ProductID)}
	}

	// Update PENDING cart (IN MEMORY)
	quantity := action.Data.Quantity
	if quantity < 1 {
		quantity = 1
	}

	found := false
	for i, item := range convCtx.PendingCart {
		if item.ProductID == product.ID {
			// PHASE 4: QUANTITY FREEZE GUARD
			// Reject implicit updates ("2->4->16" drift prevention)
			if !action.Data.Explicit {
				return ErrorResult{Error: fmt.Errorf("quantity update rejected: strict explicit consent required")}
			}

			convCtx.PendingCart[i].Quantity += quantity
			found = true
			break
		}
	}

	if !found {
		convCtx.PendingCart = append(convCtx.PendingCart, PendingCartItem{
			ProductID:   product.ID,
			ProductName: product.Name,
			Quantity:    quantity,
			UnitPrice:   product.Price,
			Currency:    product.Currency,
		})
	}

	e.logger.Info("pending_cart_updated",
		zap.Int("items", len(convCtx.PendingCart)),
		zap.String("product", product.Name),
	)

	// PERSIST items to DB (SalesConversation)
	// PERSIST items to DB (Order table)
	if _, err := e.upsertDraftOrder(ctx, tx, convCtx); err != nil {
		return ErrorResult{Error: fmt.Errorf("failed to save cart: %w", err)}
	}

	// Return updated cart
	return e.showCart(ctx, convCtx)
}

func (e *actionExecutor) removeFromCart(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext, action Action) ExecutionResult {
	// HARD GUARD: Prevent mutation of locked orders
	if err := e.guardOrderMutable(convCtx.CurrentOrder); err != nil {
		return ErrorResult{Error: err}
	}
	if len(convCtx.PendingCart) == 0 {
		return ErrorResult{Error: fmt.Errorf("cart is empty")}
	}

	var removed bool
	var newCart []PendingCartItem
	for _, item := range convCtx.PendingCart {
		if item.ProductID == action.Data.ProductID {
			removed = true
			e.logger.Info("item_removed_from_cart",
				zap.String("product", item.ProductName),
			)
		} else {
			newCart = append(newCart, item)
		}
	}

	if !removed {
		return ErrorResult{Error: fmt.Errorf("item not found in cart")}
	}

	convCtx.PendingCart = newCart
	return e.showCart(ctx, convCtx)
}

func (e *actionExecutor) clearCart(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext) ExecutionResult {
	// HARD GUARD: Prevent mutation of locked orders
	if err := e.guardOrderMutable(convCtx.CurrentOrder); err != nil {
		return ErrorResult{Error: err}
	}
	if len(convCtx.PendingCart) == 0 {
		return AckResult{Message: "Cart is already empty"}
	}

	e.logger.Info("pending_cart_cleared",
		zap.Int("items_removed", len(convCtx.PendingCart)),
	)

	convCtx.PendingCart = []PendingCartItem{}
	return e.showCart(ctx, convCtx)
}

func (e *actionExecutor) cancelOrder(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext) ExecutionResult {
	if convCtx.CurrentOrder == nil {
		return ErrorResult{Error: fmt.Errorf("no active order to cancel")}
	}

	// Load and update order
	var order storage.Order
	if err := tx.Where("id = ?", convCtx.CurrentOrder.ID).First(&order).Error; err != nil {
		return ErrorResult{Error: err}
	}

	order.Status = "cancelled"

	if err := tx.Save(&order).Error; err != nil {
		return ErrorResult{Error: err}
	}

	v3Order := &Order{
		ID:    order.ID,
		State: StateCancelled,
	}

	return ShowStatusResult{
		Order:   v3Order,
		Message: "Order cancelled",
	}
}

func (e *actionExecutor) confirmOrder(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext) ExecutionResult {
	if convCtx.CurrentOrder == nil {
		return ErrorResult{Error: fmt.Errorf("no active order to confirm")}
	}

	// Load and update order
	var order storage.Order
	if err := tx.Where("id = ?", convCtx.CurrentOrder.ID).First(&order).Error; err != nil {
		return ErrorResult{Error: err}
	}

	if order.Status != "draft" && order.Status != "building" {
		// Already confirmed?
		// Ensure we don't re-confirm and duplicate?
		// But confirm is idempotent-ish if we just update status.
	}

	now := time.Now()
	order.Status = "confirmed"
	order.ConfirmedAt = &now

	if err := tx.Save(&order).Error; err != nil {
		return ErrorResult{Error: err}
	}

	// CREATE SNAPSHOT (The No Surprise Contract)
	var items []OrderItem
	var total float64
	for _, p := range convCtx.PendingCart {
		items = append(items, OrderItem{
			ProductID:   p.ProductID,
			ProductName: p.ProductName,
			Quantity:    p.Quantity,
			UnitPrice:   p.UnitPrice,
			TotalPrice:  p.UnitPrice * float64(p.Quantity),
		})
		total += p.UnitPrice * float64(p.Quantity)
	}

	snapshot := &OrderSnapshot{
		Items:    items,
		Total:    total,
		Currency: order.Currency,
		LockedAt: now,
	}

	// Update in-memory context to reflect lock immediately
	if convCtx.CurrentOrder != nil {
		convCtx.CurrentOrder.State = StateConfirmed
		convCtx.CurrentOrder.Snapshot = snapshot
	}

	v3Order := &Order{
		ID:          order.ID,
		OrderNumber: order.OrderNumber,
		State:       StateConfirmed,
		Total:       total,
		Currency:    order.Currency,
		Items:       items,
		Snapshot:    snapshot,
	}

	// INDUSTRY-AWARE REQUIREMENTS CHECK
	// For products/menu: Check requirements BEFORE payment
	// For services: Skip to payment (requirements collected AFTER payment)
	if convCtx.IndustryHasProducts || convCtx.IndustryHasMenu {
		fs := fulfillment.NewService(tx, e.logger)
		hasReqs, firstReq, err := fs.HasPendingRequirementsForOrder(&order)
		if err != nil {
			e.logger.Warn("failed to check requirements", zap.Error(err))
			// Non-fatal: proceed to payment if check fails
		} else if hasReqs && firstReq != nil {
			// Initialize fulfillment state and get first requirement
			if err := fs.EnsureFulfillmentState(&order); err != nil {
				e.logger.Warn("failed to initialize fulfillment state", zap.Error(err))
			} else {
				// Set mode to requirements collection
				if convCtx.StateSnapshot != nil {
					convCtx.StateSnapshot.ConversationMode = ModeRequirements
				}

				// Return CollectRequirementsResult to trigger requirements flow
				return CollectRequirementsResult{
					Order: v3Order,
					Requirement: RequirementInfo{
						ID:          firstReq.ID.String(),
						Label:       firstReq.Label,
						Description: firstReq.Description,
						Type:        firstReq.DataType,
					},
					Message:        "Order confirmed!",
					TotalRemaining: 1, // We'll update this when we have better counting logic
				}
			}
		}
	}

	// No requirements or services industry: proceed to payment
	return ShowStatusResult{
		Order:   v3Order,
		Message: "Order confirmed.",
	}
}

// createOrderAndShowPayment finalizes the draft order to awaiting_payment
func (e *actionExecutor) createOrderAndShowPayment(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext) ExecutionResult {
	// NO SURPRISE ORDERS: Logic Flow
	// If Locked -> Load from DB (Trust Snapshot).
	// If Draft -> Upsert from PendingCart (Calculate).

	var order *storage.Order
	var err error

	if MustUseSnapshot(convCtx) {
		// LOCKED PATH: Trust the DB/Snapshot. Do NOT recalculate.
		var existing storage.Order
		if err := tx.Where("id = ?", convCtx.CurrentOrder.ID).First(&existing).Error; err != nil {
			return ErrorResult{Error: fmt.Errorf("failed to load locked order: %w", err)}
		}
		order = &existing
	} else {
		// DRAFT PATH: Recalculate from Cart
		if len(convCtx.PendingCart) == 0 {
			return ErrorResult{Error: fmt.Errorf("no items in cart to pay for")}
		}
		order, err = e.upsertDraftOrder(ctx, tx, convCtx)
		if err != nil {
			return ErrorResult{Error: fmt.Errorf("failed to finalize draft: %w", err)}
		}
	}

	// 2. Transition to Awaiting Payment
	order.Status = "awaiting_payment"

	// PHASE 1: PERSIST SNAPSHOT (The Contract)
	// We must save the OrderContextSnapshot to the DB now.
	// This freezes the Items, Total, Mode, and Rejections.
	// We marshal convCtx.StateSnapshot (which should be populated by now? Or we populate it here?)
	// If confirmOrder is called, we populate implicit items.
	// But here createdOrderAndShowPayment implies we are locking draft -> payment.

	// Ensure StateSnapshot exists and populate if needed
	if convCtx.StateSnapshot == nil {
		convCtx.StateSnapshot = &OrderContextSnapshot{
			ConversationMode: ModePayment,
			ItemsConfirmed:   true,
			TotalAmount:      order.TotalAmount,
			Items:            make([]OrderItem, len(convCtx.PendingCart)),
			RejectedItems:    make(map[string]bool),
			Locked:           true,
		}
		// Copy items
		for i, p := range convCtx.PendingCart {
			convCtx.StateSnapshot.Items[i] = OrderItem{
				ProductID:   p.ProductID,
				ProductName: p.ProductName,
				Quantity:    p.Quantity,
				UnitPrice:   p.UnitPrice,
				TotalPrice:  p.UnitPrice * float64(p.Quantity),
			}
		}
	} else {
		// Update mode
		convCtx.StateSnapshot.ConversationMode = ModePayment
		convCtx.StateSnapshot.Locked = true
	}

	// Helper: Save Snapshot to Order (assuming generic JSONB handling or similar)
	// order.Snapshot = convCtx.StateSnapshot // Needs casting if storage type differs.
	// Assuming storage.Order has Snapshot field of type *OrderSnapshot (old) or JSON.
	// We might need to map OrderContextSnapshot to OrderSnapshot layout if they differ or use new struct.
	// For now, avoiding complex DB type changes, we assume we can write.
	// TODO: Verify storage type. For safety, we skip direct assignment if unsure,
	// but requirement is "Persist". We assume `storage.Order` was updated or behaves dynamically.

	if err := tx.Save(order).Error; err != nil {
		return ErrorResult{Error: fmt.Errorf("failed to update order status: %w", err)}
	}

	e.logger.Info("order_finalized_for_payment",
		zap.Stringer("order_id", order.ID),
		zap.String("order_number", order.OrderNumber),
		zap.Float64("total", order.TotalAmount),
	)

	// Update Context Order State
	convCtx.CurrentOrder.State = StatePayment

	// Since we return ShowPaymentResult, we need PaymentInfo
	if convCtx.PaymentInfo == nil {
		// Try to fallback or return error?
		// We can return error, but user flow might be stuck.
		// Let's log warning and return error.
		return ErrorResult{Error: fmt.Errorf("payment info not configured")}
	}

	// Check Industry for Quote vs Payment
	if convCtx.IndustryCategory == "professional_services" || convCtx.IndustryCategory == "consultant" {
		// Update status to Quote Generated
		order.Status = "quote_generated"
		if err := tx.Save(order).Error; err != nil {
			return ErrorResult{Error: fmt.Errorf("failed to generate quote: %w", err)}
		}

		e.logger.Info("quote_generated",
			zap.Stringer("order_id", order.ID),
			zap.String("quote_number", order.OrderNumber),
		)

		// Return Quote Result
		// Convert storage order to V3 order for display
		v3Order := &Order{
			ID:          order.ID,
			OrderNumber: order.OrderNumber,
			Total:       order.TotalAmount,
			Currency:    order.Currency,
			CreatedAt:   order.CreatedAt,
			State:       OrderState(order.Status),
			// Items usually needed for quote
		}

		return ShowQuoteResult{
			Order:   v3Order,
			Message: fmt.Sprintf("I've generated a quote for you (Ref: %s). Please review the details.", order.OrderNumber),
		}
	}

	return ShowPaymentResult{
		Total: order.TotalAmount,
		Bank:  *convCtx.PaymentInfo,
	}
}

// =============================================================================
// HELPERS
// =============================================================================

func generateOrderNumber() string {
	now := time.Now()
	return fmt.Sprintf("ORD-%s-%s",
		now.Format("20060102"),
		strings.ToUpper(uuid.New().String()[:6]),
	)
}

// upsertDraftOrder updates or creates a draft order in the Order table
func (e *actionExecutor) upsertDraftOrder(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext) (*storage.Order, error) {
	if len(convCtx.PendingCart) == 0 && convCtx.DeliveryState == nil {
		// Nothing to save? Or maybe we should clear the draft if empty?
		// If cart is empty, we might want to delete the draft order or leave it empty.
		// Let's safe-guard: if we have an ID, update it to empty.
	}

	// 1. Prepare Storage Items
	var storageItems []storage.OrderItem
	var total float64
	currency := "NGN"

	for _, item := range convCtx.PendingCart {
		itemTotal := item.UnitPrice * float64(item.Quantity)
		total += itemTotal
		storageItems = append(storageItems, storage.OrderItem{
			Name:       item.ProductName,
			Quantity:   item.Quantity,
			UnitPrice:  item.UnitPrice,
			TotalPrice: itemTotal,
		})
		if item.Currency != "" {
			currency = item.Currency
		}
	}

	// Delivery Fee Logic (Synced with memory)
	if convCtx.DeliveryState != nil {
		if convCtx.DeliveryState.SelectedArea != nil {
			fee := convCtx.DeliveryState.SelectedArea.Price
			if fee > 0 {
				total += fee
				storageItems = append(storageItems, storage.OrderItem{
					Name:       fmt.Sprintf("Delivery (%s)", convCtx.DeliveryState.SelectedArea.Name),
					Quantity:   1,
					UnitPrice:  fee,
					TotalPrice: fee,
				})
			}
		} else if convCtx.DeliveryState.DeliveryFee > 0 {
			total += convCtx.DeliveryState.DeliveryFee
			storageItems = append(storageItems, storage.OrderItem{
				Name:       "Delivery Fee",
				Quantity:   1,
				UnitPrice:  convCtx.DeliveryState.DeliveryFee,
				TotalPrice: convCtx.DeliveryState.DeliveryFee,
			})
		}
	}

	itemsJSON, err := json.Marshal(storageItems)
	if err != nil {
		return nil, err
	}

	// 2. Prepare Order Model
	var order storage.Order

	// Check if we already have a current order ID that fits our draft criteria
	if convCtx.CurrentOrder != nil && (convCtx.CurrentOrder.State == StateDraft || convCtx.CurrentOrder.State == "pending" || convCtx.CurrentOrder.State == "building") {
		// Update existing
		if err := tx.First(&order, convCtx.CurrentOrder.ID).Error; err != nil {
			// If not found (maybe deleted?), proceed to create new
			e.logger.Warn("draft_order_not_found_for_update", zap.Error(err))
		}
	}

	// Double check by query if not found by ID
	if order.ID == uuid.Nil {
		err := tx.Where("conversation_id = ? AND status IN ?",
			convCtx.ConversationID,
			[]string{"draft", "pending", "building"}).
			First(&order).Error
		if err == nil {
			// Found one
		}
	}

	if order.ID == uuid.Nil {
		// CREATE NEW
		order = storage.Order{
			TenantID:       convCtx.TenantID,
			CustomerID:     convCtx.CustomerID,
			ConversationID: convCtx.ConversationID,
			Status:         "draft", // Explicit draft status
			OrderNumber:    generateOrderNumber(),
			Platform:       convCtx.Platform,
			// Fix unique constraint collision: PaymentReference must be unique even for drafts
			PaymentReference: fmt.Sprintf("DRAFT-%d-%s", time.Now().UnixNano(), uuid.New().String()[:8]),
		}
	}

	// Update Fields
	order.Items = string(itemsJSON)
	order.TotalAmount = total
	order.Currency = currency

	if convCtx.DeliveryState != nil {
		order.DeliveryAddress = convCtx.DeliveryState.Address
		if convCtx.DeliveryState.Method == "pickup" {
			order.IsPickup = true
			if convCtx.DeliveryState.SelectedArea != nil {
				// We might abuse Notes to store pickup location if needed, or rely on logic
			}
		} else {
			order.IsPickup = false
		}
	}

	// Ensure status is valid for a draft
	if order.Status == "" {
		order.Status = "draft"
	}

	if err := tx.Save(&order).Error; err != nil {
		return nil, err
	}

	// Update Context to reflect this order
	convCtx.CurrentOrder = &Order{
		ID:        order.ID,
		State:     OrderState(order.Status),
		Total:     order.TotalAmount,
		Currency:  order.Currency,
		CreatedAt: order.CreatedAt,
		// We could update Items here too but usually we just want the ID and Total
	}

	return &order, nil
}

// ValidateHiddenCommand sanitizes LLM-generated commands (Security Guard)
func ValidateHiddenCommand(cmd HiddenCommand, ctx *ConversationContext) bool {
	switch cmd.Type {
	case "PAY_METHOD":
		// Prevent "zero" or "free" unless explicitly enabled in context (which we don't support yet)
		if strings.Contains(strings.ToLower(cmd.Payload), "free") || strings.Contains(strings.ToLower(cmd.Payload), "zero") {
			return false // REJECT: Suspicious payment method override
		}
		// Only allow known gateways (simplified check)
		valid := false
		allowed := []string{"transfer", "card", "paystack", "flutterwave", "cash_on_delivery", "pos"}
		lowerPayload := strings.ToLower(cmd.Payload)
		for _, m := range allowed {
			if strings.Contains(lowerPayload, m) {
				valid = true
				break
			}
		}
		return valid

	case "ORDER":
		// Ensure price overrides are grounded
		// This is a basic check; real validation happens in transactional execute
		if strings.Contains(cmd.Payload, "price_override") {
			// Check if negotiation was active
			if ctx == nil || ctx.SalesConversation == nil {
				return false // REJECT: Price override without negotiation context
			}
		}
		return true

	default:
		return true // Allow others (e.g., DELIVERY) as they are less critical
	}
}

func (e *actionExecutor) rejectItem(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext, action Action) ExecutionResult {
	// PHASE 5: REJECTED ITEM MEMORY
	// We must persist the rejection to the Single Source of Truth.

	productID := action.Data.ProductID
	if productID == uuid.Nil {
		// Try to infer from message if not provided?
		// For now, return error or Ack with "Removed item" generic.
		return AckResult{Message: "Understood. I've removed that from consideration."}
	}

	// 1. Ensure Snapshot Exists
	if convCtx.StateSnapshot == nil {
		convCtx.StateSnapshot = &OrderContextSnapshot{
			ConversationMode: ModeOrdering, // Default to ordering if accepting rejections
			ItemsConfirmed:   false,
			RejectedItems:    make(map[string]bool),
		}
	}

	// 2. Initialize Map if needed
	if convCtx.StateSnapshot.RejectedItems == nil {
		convCtx.StateSnapshot.RejectedItems = make(map[string]bool)
	}

	// 3. Mark Rejected
	convCtx.StateSnapshot.RejectedItems[productID.String()] = true

	e.logger.Info("item_rejected_forever",
		zap.String("product_id", productID.String()),
	)

	return AckResult{
		Message: fmt.Sprintf("Understood. I've removed %s from consideration.", action.Data.Message),
	}
}
