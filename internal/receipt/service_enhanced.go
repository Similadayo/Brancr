package receipt

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
)

// GeneratePortalTokenAndURLEnhanced generates a portal token with expiration
func (s *ReceiptService) GeneratePortalTokenAndURLEnhanced(ctx context.Context, orderID uuid.UUID, tenantID uuid.UUID, publicURL string, expiryDays int) (string, string, error) {
	// Check if payment exists for this order
	var payment storage.Payment
	if err := s.DB.Where("order_id = ? AND tenant_id = ?", orderID, tenantID).First(&payment).Error; err != nil {
		// If no payment exists, create a placeholder payment record for portal access
		var order storage.Order
		if err := s.DB.Where("id = ? AND tenant_id = ?", orderID, tenantID).First(&order).Error; err != nil {
			return "", "", fmt.Errorf("order not found: %w", err)
		}

		// Generate portal token
		portalToken := utils.GeneratePortalToken()

		// Set token expiration (default 90 days if not specified)
		if expiryDays == 0 {
			expiryDays = 90
		}
		expiresAt := time.Now().AddDate(0, 0, expiryDays)

		// Create payment record with portal token
		payment = storage.Payment{
			TenantID:         tenantID,
			OrderID:          orderID,
			CustomerID:       order.CustomerID,
			PaymentReference: order.PaymentReference,
			Amount:           order.TotalAmount,
			Currency:         order.Currency,
			Status:           "pending",
			PortalToken:      portalToken,
			TokenExpiresAt:   &expiresAt,
			Metadata:         "{}",
			CreatedAt:        time.Now(),
		}

		if err := s.DB.Create(&payment).Error; err != nil {
			return "", "", fmt.Errorf("failed to create payment record: %w", err)
		}
	} else if payment.PortalToken == "" {
		// Generate portal token if not exists
		portalToken := utils.GeneratePortalToken()

		if expiryDays == 0 {
			expiryDays = 90
		}
		expiresAt := time.Now().AddDate(0, 0, expiryDays)

		payment.PortalToken = portalToken
		payment.TokenExpiresAt = &expiresAt
		if err := s.DB.Save(&payment).Error; err != nil {
			return "", "", fmt.Errorf("failed to save portal token: %w", err)
		}
	} else if payment.TokenExpiresAt != nil && time.Now().After(*payment.TokenExpiresAt) {
		// Token expired, generate new one
		portalToken := utils.GeneratePortalToken()

		if expiryDays == 0 {
			expiryDays = 90
		}
		expiresAt := time.Now().AddDate(0, 0, expiryDays)

		payment.PortalToken = portalToken
		payment.TokenExpiresAt = &expiresAt
		if err := s.DB.Save(&payment).Error; err != nil {
			return "", "", fmt.Errorf("failed to regenerate expired token: %w", err)
		}
	}

	// Generate portal URL (ensure no double slash and include API prefix)
	base := strings.TrimRight(publicURL, "/")
	portalURL := fmt.Sprintf("%s/api/portal/order?token=%s", base, payment.PortalToken)
	payment.PortalURL = portalURL

	if err := s.DB.Save(&payment).Error; err != nil {
		return "", "", fmt.Errorf("failed to save portal URL: %w", err)
	}

	return payment.PortalToken, portalURL, nil
}
