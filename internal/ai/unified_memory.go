package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ============================================================
// UNIFIED MEMORY (Blueprint Section 15)
// Purpose: Give Support context to see Sales history
// Support must know: order history, negotiated prices, payment status
// ============================================================

// UnifiedMemory provides cross-role visibility into customer history
type UnifiedMemory struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// CustomerMemory contains unified customer context across sales and support
type CustomerMemory struct {
	// Customer identity
	CustomerID   uuid.UUID `json:"customer_id"`
	CustomerName string    `json:"customer_name,omitempty"`

	// Order history (visible to support)
	TotalOrders      int       `json:"total_orders"`
	LastOrderDate    time.Time `json:"last_order_date,omitempty"`
	LastOrderAmount  float64   `json:"last_order_amount,omitempty"`
	LastOrderProduct string    `json:"last_order_product,omitempty"`

	// Negotiation context (visible to support if active)
	HasActiveNegotiation bool    `json:"has_negotiation"`
	NegotiatedPrice      float64 `json:"negotiated_price,omitempty"`
	OriginalPrice        float64 `json:"original_price,omitempty"`
	DiscountGiven        float64 `json:"discount_given,omitempty"` // Percentage

	// Payment status (critical for support)
	HasPendingPayment bool      `json:"has_pending_payment"`
	LastPaymentDate   time.Time `json:"last_payment_date,omitempty"`
	LastPaymentAmount float64   `json:"last_payment_amount,omitempty"`
	PaymentStatus     string    `json:"payment_status,omitempty"` // pending, confirmed, failed

	// Support context
	OpenSupportIssues int    `json:"open_support_issues"`
	LastIssueType     string `json:"last_issue_type,omitempty"`
	EscalationHistory int    `json:"escalation_history"` // How many times escalated

	// Trust level
	TrustLevel       float64 `json:"trust_level"` // 0.0-1.0
	IsRepeatCustomer bool    `json:"is_repeat_customer"`
}

// NewUnifiedMemory creates a unified memory instance
func NewUnifiedMemory(db *gorm.DB, logger *zap.Logger) *UnifiedMemory {
	return &UnifiedMemory{
		DB:     db,
		Logger: logger,
	}
}

// GetCustomerMemory retrieves complete customer context for any role
// This enables support to see relevant sales context
func (um *UnifiedMemory) GetCustomerMemory(ctx context.Context, tenantID, customerID uuid.UUID) (*CustomerMemory, error) {
	memory := &CustomerMemory{
		CustomerID: customerID,
		TrustLevel: 0.5, // Default
	}

	// Get customer info
	var customer storage.Customer
	if err := um.DB.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, customerID).
		First(&customer).Error; err == nil {
		memory.CustomerName = customer.DisplayName
	}

	// Get order history
	var orderCount int64
	um.DB.WithContext(ctx).Model(&storage.Order{}).
		Where("tenant_id = ? AND customer_id = ?", tenantID, customerID).
		Count(&orderCount)
	memory.TotalOrders = int(orderCount)
	memory.IsRepeatCustomer = orderCount > 0

	// Get last order details
	var lastOrder storage.Order
	if err := um.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ?", tenantID, customerID).
		Order("created_at DESC").
		First(&lastOrder).Error; err == nil {
		memory.LastOrderDate = lastOrder.CreatedAt
		memory.LastOrderAmount = lastOrder.TotalAmount
		// Product name would need join or additional query
	}

	// Get active sales conversation (for negotiation context)
	var sc storage.SalesConversation
	if err := um.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ?", tenantID, customerID).
		Order("updated_at DESC").
		First(&sc).Error; err == nil {
		// Check for active negotiation
		memory.HasActiveNegotiation = sc.Stage == "negotiation"
		memory.NegotiatedPrice = sc.PriceDiscussed
		// Use LastAIOffer as a reference for original pricing context
		if sc.LastAIOffer > 0 {
			memory.OriginalPrice = sc.LastAIOffer
		}

		if memory.OriginalPrice > 0 && memory.NegotiatedPrice > 0 {
			memory.DiscountGiven = ((memory.OriginalPrice - memory.NegotiatedPrice) / memory.OriginalPrice) * 100
		}

		// Check for pending payment
		memory.HasPendingPayment = sc.PendingOrderStatus == "pending" || sc.Stage == "payment_instructions"
	}

	// Calculate trust level based on history
	memory.TrustLevel = um.calculateTrustLevel(memory)

	return memory, nil
}

// calculateTrustLevel determines customer trust based on history
func (um *UnifiedMemory) calculateTrustLevel(memory *CustomerMemory) float64 {
	trust := 0.5 // Base

	// Repeat customers get trust boost
	if memory.IsRepeatCustomer {
		trust += 0.2
	}

	// Multiple orders = higher trust
	if memory.TotalOrders >= 3 {
		trust += 0.2
	}

	// Pending payment = slight trust reduction
	if memory.HasPendingPayment {
		trust -= 0.1
	}

	// Cap at 0.0-1.0
	if trust < 0 {
		trust = 0
	}
	if trust > 1 {
		trust = 1
	}

	return trust
}

// GetSupportContext returns memory formatted for support role
func (um *UnifiedMemory) GetSupportContext(memory *CustomerMemory) string {
	if memory == nil {
		return ""
	}

	context := "CUSTOMER HISTORY:\n"

	if memory.IsRepeatCustomer {
		context += "- Repeat customer\n"
	}

	if memory.TotalOrders > 0 {
		context += "- Previous orders: " + string(rune(memory.TotalOrders+'0')) + "\n"
	}

	if memory.HasActiveNegotiation && memory.NegotiatedPrice > 0 {
		context += "- Active negotiation at price: " + formatPrice(memory.NegotiatedPrice) + "\n"
	}

	if memory.HasPendingPayment {
		context += "- ⚠️ HAS PENDING PAYMENT\n"
	}

	return context
}

// formatPrice formats price for display
func formatPrice(amount float64) string {
	if amount >= 1000000 {
		return fmt.Sprintf("%.1fM", amount/1000000)
	}
	if amount >= 1000 {
		return fmt.Sprintf("%.0fK", amount/1000)
	}
	return fmt.Sprintf("%.0f", amount)
}
