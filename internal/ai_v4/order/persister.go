package order

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	v4ctx "github.com/similadayo/brancr/internal/ai_v4/context"
	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

func isLikelyPhoneNumber(s string) bool {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return false
	}
	if strings.HasPrefix(raw, "+") {
		raw = raw[1:]
	}
	raw = strings.ReplaceAll(raw, " ", "")
	raw = strings.ReplaceAll(raw, "-", "")

	if raw == "" {
		return false
	}
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return len(raw) >= 8 && len(raw) <= 15
}

// SaveOrder persists an order to the database
func SaveOrder(
	ctx context.Context,
	db *gorm.DB,
	tenantID uuid.UUID,
	customerID uuid.UUID,
	conversationID uuid.UUID,
	platform string,
	orderSnapshot *v4ctx.OrderSnapshot,
) (*storage.Order, error) {
	// Resolve canonical customer from conversation when available.
	var conv storage.Conversation
	if err := db.WithContext(ctx).Where("id = ?", conversationID).First(&conv).Error; err == nil && conv.CustomerID != uuid.Nil {
		customerID = conv.CustomerID
	}

	customerName := ""
	customerPhone := ""
	customerEmail := ""

	var customer storage.Customer
	if err := db.WithContext(ctx).Where("id = ?", customerID).First(&customer).Error; err == nil {
		// Only auto-fill recipient name for WhatsApp. Other platforms often have handles / non-canonical names,
		// and we prefer collecting recipient details explicitly from the customer.
		if strings.EqualFold(strings.TrimSpace(platform), "whatsapp") {
			customerName = customer.DisplayName
		}
		// Only treat platform IDs as phone numbers on WhatsApp.
		// Instagram/Facebook/Telegram platform IDs are NOT phone numbers and should not auto-fill fulfillment.
		if strings.EqualFold(strings.TrimSpace(platform), "whatsapp") && isLikelyPhoneNumber(customer.PlatformID) {
			customerPhone = customer.PlatformID
		}
	}

	// Generate unique identifiers
	orderNumber := generateOrderNumber()
	paymentRef := generatePaymentReference(db, tenantID)

	// Convert items to JSON
	itemsJSON, err := json.Marshal(orderSnapshot.Items)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal items: %w", err)
	}

	// Calculate total amount (order + delivery fee)
	totalAmount := float64(orderSnapshot.TotalPrice)
	if orderSnapshot.DeliveryFee > 0 {
		totalAmount += float64(orderSnapshot.DeliveryFee)
	}

	order := &storage.Order{
		TenantID:         tenantID,
		CustomerID:       customerID,
		ConversationID:   conversationID,
		OrderNumber:      orderNumber,
		PaymentReference: paymentRef,
		Status:           "pending", // Awaiting payment
		TotalAmount:      totalAmount,
		Currency:         orderSnapshot.Currency,
		Items:            string(itemsJSON),
		CustomerName:     customerName,
		CustomerPhone:    customerPhone,
		CustomerEmail:    customerEmail,
		Platform:         platform,
		DeliveryAddress:  orderSnapshot.DeliveryAddress,
		IsPickup:         orderSnapshot.DeliveryMethod == "pickup",
		CreatedAt:        time.Now(),
	}

	if err := db.WithContext(ctx).Create(order).Error; err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	return order, nil
}

// UpdateOrder updates an existing order with new details
func UpdateOrder(
	db *gorm.DB,
	orderID uuid.UUID,
	updates map[string]interface{},
) error {
	return db.Model(&storage.Order{}).
		Where("id = ?", orderID).
		Updates(updates).Error
}

// GetOrderByConversation retrieves an active order for a conversation
func GetOrderByConversation(
	db *gorm.DB,
	conversationID uuid.UUID,
) (*storage.Order, error) {
	var order storage.Order
	err := db.Where("conversation_id = ? AND status IN (?)",
		conversationID,
		[]string{"pending", "confirmed"}).
		First(&order).Error

	if err != nil {
		return nil, err
	}

	return &order, nil
}

// generateOrderNumber creates a unique order number
// Format: ORD-20260122-ABC123
func generateOrderNumber() string {
	timestamp := time.Now().Format("20060102")
	random := strings.ToUpper(uuid.New().String()[:6])
	return fmt.Sprintf("ORD-%s-%s", timestamp, random)
}

// generatePaymentReference creates a unique payment reference
// Format: COMP-YYMMDD-ABC234
func generatePaymentReference(db *gorm.DB, tenantID uuid.UUID) string {
	prefix := tenantReferencePrefix(db, tenantID)
	datePart := time.Now().Format("060102")
	randomPart := generateFriendlyCode(6)
	return fmt.Sprintf("%s-%s-%s", prefix, datePart, randomPart)
}

func tenantReferencePrefix(db *gorm.DB, tenantID uuid.UUID) string {
	const fallback = "TEN"
	if db == nil {
		return fallback
	}

	var profile storage.BusinessProfile
	if err := db.Where("tenant_id = ?", tenantID).First(&profile).Error; err == nil {
		if abbr := abbreviateCompanyName(profile.Name); abbr != "" {
			return abbr
		}
	}

	// Fallback to a short tenant-derived stable prefix.
	tenantShort := strings.ToUpper(strings.ReplaceAll(tenantID.String(), "-", ""))
	if len(tenantShort) >= 3 {
		return tenantShort[:3]
	}
	return fallback
}

func abbreviateCompanyName(name string) string {
	raw := strings.TrimSpace(name)
	if raw == "" {
		return ""
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	stopWords := map[string]bool{
		"AND": true, "THE": true, "OF": true, "LTD": true, "LIMITED": true,
		"LLC": true, "INC": true, "CO": true, "COMPANY": true, "NIG": true,
	}

	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		u := strings.ToUpper(strings.TrimSpace(p))
		if u == "" || stopWords[u] {
			continue
		}
		filtered = append(filtered, u)
	}
	if len(filtered) == 0 {
		return ""
	}

	// Multi-word: initials up to 4 chars (simple and readable).
	if len(filtered) >= 2 {
		var b strings.Builder
		for i := 0; i < len(filtered) && b.Len() < 4; i++ {
			b.WriteByte(filtered[i][0])
		}
		if b.Len() >= 2 {
			return b.String()
		}
	}

	// Single-word: first 3-4 chars.
	word := filtered[0]
	if len(word) >= 4 {
		return word[:4]
	}
	if len(word) >= 3 {
		return word[:3]
	}
	return word
}

func generateFriendlyCode(n int) string {
	if n <= 0 {
		return ""
	}

	// Exclude easily-confused chars: 0,O,1,I,L
	const allowed = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
	raw := strings.ToUpper(strings.ReplaceAll(uuid.New().String(), "-", ""))
	var out strings.Builder
	for _, ch := range raw {
		if strings.ContainsRune(allowed, ch) {
			out.WriteRune(ch)
			if out.Len() >= n {
				return out.String()
			}
		}
	}

	// Deterministic fallback should never be needed, but keeps function total.
	for out.Len() < n {
		out.WriteByte(allowed[time.Now().Nanosecond()%len(allowed)])
	}
	return out.String()
}
