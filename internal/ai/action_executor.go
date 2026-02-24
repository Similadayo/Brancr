package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ActionExecutor is Stage 4 of the v2.0 pipeline.
// It is WRITE-AUTHORITATIVE - all database mutations happen here.
// The executor receives a ResolvedAction from the Decision Resolver
// and performs the actual business operations.
type ActionExecutor struct {
	DB              *gorm.DB
	Logger          *zap.Logger
	PaymentProvider PaymentProvider
}

// NewActionExecutor creates a new action executor.
func NewActionExecutor(db *gorm.DB, logger *zap.Logger, paymentProvider PaymentProvider) *ActionExecutor {
	return &ActionExecutor{
		DB:              db,
		Logger:          logger,
		PaymentProvider: paymentProvider,
	}
}

// PaymentProvider interface for generating payment instructions.
// First implementation is hardcoded; future can integrate Paystack/Flutterwave.
type PaymentProvider interface {
	GeneratePaymentInstructions(ctx context.Context, tenantID uuid.UUID, order *storage.Order) (*PaymentInfo, error)
}

// PaymentInfo contains payment details to send to customer.
type PaymentInfo struct {
	BankName      string
	AccountName   string
	AccountNumber string // Full number, no masking
	Amount        float64
	Currency      string
	Reference     string
	ExpiresAt     *time.Time
	PaymentLink   string // Optional: for payment gateway integrations
}

// ActionResult is the output of the Action Executor.
type ActionResult struct {
	Success      bool
	Order        *storage.Order
	PaymentInfo  *PaymentInfo
	ErrorCode    string
	ErrorMessage string
	StateChanged bool // True if order state was modified
}

// Execute performs the action determined by the Decision Resolver.
// CRITICAL: This method must revalidate resolver assumptions before committing writes.
func (e *ActionExecutor) Execute(ctx context.Context, action *ResolvedAction, tenantID, customerID, conversationID uuid.UUID, platform string) (*ActionResult, error) {
	result := &ActionResult{Success: true}

	switch action.ActionType {
	case V2ActionNone:
		// No action needed
		return result, nil

	case V2ActionCreateOrder:
		return e.createOrder(ctx, action, tenantID, customerID, conversationID, platform)

	case V2ActionUpdateOrder:
		return e.updateOrder(ctx, action)

	case V2ActionLockOrder:
		return e.lockOrder(ctx, action)

	case V2ActionSendPayment:
		return e.sendPayment(ctx, action, tenantID)

	case V2ActionConfirmPayment:
		return e.confirmPayment(ctx, action)

	case V2ActionCancelOrder:
		return e.cancelOrder(ctx, action)

	case V2ActionAskClarification:
		// No DB action, just return success for response composer
		return result, nil

	case V2ActionRejectIntent:
		result.Success = false
		result.ErrorCode = "INVALID_STATE"
		result.ErrorMessage = action.FallbackReason
		return result, nil

	default:
		return result, nil
	}
}

// createOrder creates a new order in CART_ACTIVE state.
// Uses transaction to prevent race conditions.
func (e *ActionExecutor) createOrder(ctx context.Context, action *ResolvedAction, tenantID, customerID, conversationID uuid.UUID, platform string) (*ActionResult, error) {
	result := &ActionResult{Success: true}

	// CRITICAL: Revalidate that no active order exists (transaction-safe)
	err := e.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Double-check SSOT inside transaction
		var existingOrder storage.Order
		err := tx.Where("tenant_id = ? AND customer_id = ? AND platform = ?", tenantID, customerID, platform).
			Where("status NOT IN ?", []string{"fulfilled", "cancelled", "expired", "paid"}).
			First(&existingOrder).Error

		if err == nil {
			// Order already exists - reuse it (SSOT enforced)
			e.Logger.Info("⚠️ SSOT: Order already exists, reusing",
				zap.Stringer("existing_order_id", existingOrder.ID),
				zap.String("status", existingOrder.Status))
			result.Order = &existingOrder
			return nil
		}

		if err != gorm.ErrRecordNotFound {
			return err
		}

		// Create new order
		orderNumber := generateOrderNumber()
		paymentRef := generatePaymentReference()

		// Extract product info from validated data
		var items []storage.OrderItem
		var totalAmount float64

		if product, ok := action.ValidatedData["product"].(*storage.Product); ok {
			quantity := 1
			if q, ok := action.ValidatedData["quantity"].(int); ok {
				quantity = q
			}
			totalAmount = product.Price * float64(quantity)
			items = append(items, storage.OrderItem{
				Name:       product.Name,
				Quantity:   quantity,
				UnitPrice:  product.Price,
				TotalPrice: totalAmount,
			})
		}

		itemsJSON, _ := json.Marshal(items)

		newOrder := storage.Order{
			TenantID:         tenantID,
			CustomerID:       customerID,
			ConversationID:   conversationID,
			Platform:         platform,
			OrderNumber:      orderNumber,
			PaymentReference: paymentRef,
			Status:           "draft", // Maps to CART_ACTIVE
			TotalAmount:      totalAmount,
			Currency:         "NGN",
			Items:            string(itemsJSON),
			CreatedAt:        time.Now(),
		}

		if err := tx.Create(&newOrder).Error; err != nil {
			return err
		}

		result.Order = &newOrder
		result.StateChanged = true

		e.Logger.Info("📦 V2 Order created",
			zap.Stringer("order_id", newOrder.ID),
			zap.String("order_number", orderNumber),
			zap.Float64("total", totalAmount))

		return nil
	})

	if err != nil {
		result.Success = false
		result.ErrorCode = "CREATE_ORDER_FAILED"
		result.ErrorMessage = err.Error()
		return result, err
	}

	return result, nil
}

// updateOrder adds/removes items from an existing order.
func (e *ActionExecutor) updateOrder(ctx context.Context, action *ResolvedAction) (*ActionResult, error) {
	result := &ActionResult{Success: true}

	if action.ActiveOrder == nil {
		result.Success = false
		result.ErrorCode = "NO_ACTIVE_ORDER"
		result.ErrorMessage = "No order to update"
		return result, nil
	}

	// Parse existing items
	var items []storage.OrderItem
	if action.ActiveOrder.Items != "" {
		_ = json.Unmarshal([]byte(action.ActiveOrder.Items), &items)
	}

	// Add new item from validated data
	if product, ok := action.ValidatedData["product"].(*storage.Product); ok {
		quantity := 1
		if q, ok := action.ValidatedData["quantity"].(int); ok {
			quantity = q
		}

		// Check if item already exists
		found := false
		for i := range items {
			if items[i].Name == product.Name {
				items[i].Quantity += quantity
				items[i].TotalPrice = items[i].UnitPrice * float64(items[i].Quantity)
				found = true
				break
			}
		}

		if !found {
			items = append(items, storage.OrderItem{
				Name:       product.Name,
				Quantity:   quantity,
				UnitPrice:  product.Price,
				TotalPrice: product.Price * float64(quantity),
			})
		}
	}

	// Recalculate total
	var total float64
	for _, item := range items {
		total += item.TotalPrice
	}

	itemsJSON, _ := json.Marshal(items)

	// Update order
	if err := e.DB.WithContext(ctx).Model(action.ActiveOrder).Updates(map[string]interface{}{
		"items":        string(itemsJSON),
		"total_amount": total,
		"updated_at":   time.Now(),
	}).Error; err != nil {
		result.Success = false
		result.ErrorCode = "UPDATE_ORDER_FAILED"
		result.ErrorMessage = err.Error()
		return result, err
	}

	action.ActiveOrder.Items = string(itemsJSON)
	action.ActiveOrder.TotalAmount = total
	result.Order = action.ActiveOrder
	result.StateChanged = true

	e.Logger.Info("📦 V2 Order updated",
		zap.Stringer("order_id", action.ActiveOrder.ID),
		zap.Int("item_count", len(items)),
		zap.Float64("new_total", total))

	return result, nil
}

// lockOrder transitions order to CHECKOUT_PENDING.
func (e *ActionExecutor) lockOrder(ctx context.Context, action *ResolvedAction) (*ActionResult, error) {
	result := &ActionResult{Success: true}

	if action.ActiveOrder == nil {
		result.Success = false
		result.ErrorCode = "NO_ACTIVE_ORDER"
		result.ErrorMessage = "No order to lock"
		return result, nil
	}

	// Validate transition
	currentState := MapLegacyStatus(action.ActiveOrder.Status)
	if !currentState.CanTransitionTo(OrderStateCheckoutPending) {
		result.Success = false
		result.ErrorCode = "INVALID_TRANSITION"
		result.ErrorMessage = fmt.Sprintf("Cannot lock order in %s state", currentState)
		return result, nil
	}

	// Update status
	if err := e.DB.WithContext(ctx).Model(action.ActiveOrder).Updates(map[string]interface{}{
		"status":     "pending", // Maps to CHECKOUT_PENDING
		"updated_at": time.Now(),
	}).Error; err != nil {
		result.Success = false
		result.ErrorCode = "LOCK_ORDER_FAILED"
		result.ErrorMessage = err.Error()
		return result, err
	}

	action.ActiveOrder.Status = "pending"
	result.Order = action.ActiveOrder
	result.StateChanged = true

	e.Logger.Info("🔒 V2 Order locked",
		zap.Stringer("order_id", action.ActiveOrder.ID),
		zap.String("new_status", "pending"))

	return result, nil
}

// sendPayment generates and returns payment instructions.
func (e *ActionExecutor) sendPayment(ctx context.Context, action *ResolvedAction, tenantID uuid.UUID) (*ActionResult, error) {
	result := &ActionResult{Success: true}

	if action.ActiveOrder == nil {
		result.Success = false
		result.ErrorCode = "NO_ACTIVE_ORDER"
		result.ErrorMessage = "No order for payment"
		return result, nil
	}

	// Generate payment instructions
	if e.PaymentProvider != nil {
		paymentInfo, err := e.PaymentProvider.GeneratePaymentInstructions(ctx, tenantID, action.ActiveOrder)
		if err != nil {
			e.Logger.Warn("Payment provider error, using fallback", zap.Error(err))
		} else {
			result.PaymentInfo = paymentInfo
		}
	}

	// Update order status to awaiting_payment
	if err := e.DB.WithContext(ctx).Model(action.ActiveOrder).Updates(map[string]interface{}{
		"status":     "awaiting_payment", // Maps to PAYMENT_SENT
		"updated_at": time.Now(),
	}).Error; err != nil {
		result.Success = false
		result.ErrorCode = "SEND_PAYMENT_FAILED"
		result.ErrorMessage = err.Error()
		return result, err
	}

	action.ActiveOrder.Status = "awaiting_payment"
	result.Order = action.ActiveOrder
	result.StateChanged = true

	e.Logger.Info("💳 V2 Payment instructions sent",
		zap.Stringer("order_id", action.ActiveOrder.ID),
		zap.Float64("amount", action.ActiveOrder.TotalAmount))

	return result, nil
}

// confirmPayment marks order as PAID.
func (e *ActionExecutor) confirmPayment(ctx context.Context, action *ResolvedAction) (*ActionResult, error) {
	result := &ActionResult{Success: true}

	if action.ActiveOrder == nil {
		result.Success = false
		result.ErrorCode = "NO_ACTIVE_ORDER"
		result.ErrorMessage = "No order to confirm"
		return result, nil
	}

	// Safety guard: AI must NOT auto-mark paid without explicit tenant verification.
	tenantVerified, _ := action.ValidatedData["tenant_verified"].(bool)
	if !tenantVerified {
		result.Success = false
		result.ErrorCode = "TENANT_VERIFICATION_REQUIRED"
		result.ErrorMessage = "Payment requires tenant verification before marking paid"
		e.Logger.Warn("blocked_auto_paid_without_tenant_verification",
			zap.Stringer("order_id", action.ActiveOrder.ID))
		return result, nil
	}

	// Update status to paid
	if err := e.DB.WithContext(ctx).Model(action.ActiveOrder).Updates(map[string]interface{}{
		"status":     "paid",
		"updated_at": time.Now(),
	}).Error; err != nil {
		result.Success = false
		result.ErrorCode = "CONFIRM_PAYMENT_FAILED"
		result.ErrorMessage = err.Error()
		return result, err
	}

	action.ActiveOrder.Status = "paid"
	result.Order = action.ActiveOrder
	result.StateChanged = true

	e.Logger.Info("✅ V2 Payment confirmed",
		zap.Stringer("order_id", action.ActiveOrder.ID))

	return result, nil
}

// cancelOrder cancels the order.
func (e *ActionExecutor) cancelOrder(ctx context.Context, action *ResolvedAction) (*ActionResult, error) {
	result := &ActionResult{Success: true}

	if action.ActiveOrder == nil {
		result.Success = false
		result.ErrorCode = "NO_ACTIVE_ORDER"
		result.ErrorMessage = "No order to cancel"
		return result, nil
	}

	// Update status to cancelled
	if err := e.DB.WithContext(ctx).Model(action.ActiveOrder).Updates(map[string]interface{}{
		"status":     "cancelled",
		"updated_at": time.Now(),
	}).Error; err != nil {
		result.Success = false
		result.ErrorCode = "CANCEL_ORDER_FAILED"
		result.ErrorMessage = err.Error()
		return result, err
	}

	action.ActiveOrder.Status = "cancelled"
	result.Order = action.ActiveOrder
	result.StateChanged = true

	e.Logger.Info("❌ V2 Order cancelled",
		zap.Stringer("order_id", action.ActiveOrder.ID))

	return result, nil
}

// Helper functions

func generateOrderNumber() string {
	return fmt.Sprintf("ORD-%d", time.Now().UnixNano()%1000000)
}

func generatePaymentReference() string {
	return fmt.Sprintf("PAY-%d", time.Now().UnixNano()%1000000)
}

// HardcodedPaymentProvider is the initial implementation of PaymentProvider.
// Returns hardcoded bank account details from tenant settings.
type HardcodedPaymentProvider struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewHardcodedPaymentProvider creates a new hardcoded payment provider.
func NewHardcodedPaymentProvider(db *gorm.DB, logger *zap.Logger) *HardcodedPaymentProvider {
	return &HardcodedPaymentProvider{
		DB:     db,
		Logger: logger,
	}
}

// GeneratePaymentInstructions returns tenant's bank account details.
func (p *HardcodedPaymentProvider) GeneratePaymentInstructions(ctx context.Context, tenantID uuid.UUID, order *storage.Order) (*PaymentInfo, error) {
	// Fetch tenant's payment account
	var account storage.TenantPaymentAccount
	err := p.DB.WithContext(ctx).
		Where("tenant_id = ? AND is_active = ? AND is_default = ?", tenantID, true, true).
		First(&account).Error

	if err != nil {
		// Try any active account
		err = p.DB.WithContext(ctx).
			Where("tenant_id = ? AND is_active = ?", tenantID, true).
			First(&account).Error
		if err != nil {
			return nil, fmt.Errorf("no payment account configured for tenant")
		}
	}

	return &PaymentInfo{
		BankName:      account.BankName,
		AccountName:   account.AccountName,
		AccountNumber: account.AccountNumber, // Full number, no masking
		Amount:        order.TotalAmount,
		Currency:      order.Currency,
		Reference:     order.PaymentReference,
	}, nil
}
