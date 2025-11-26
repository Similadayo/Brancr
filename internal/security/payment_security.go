package security

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// PaymentSecurityHandler handles all security measures for payment-related messages
type PaymentSecurityHandler struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// PaymentAuditLog tracks all access to payment escalations
type PaymentAuditLog struct {
	gorm.Model
	TenantID      uint      `gorm:"index"`
	InteractionID uint      `gorm:"index"`
	Action        string    `gorm:"size:50;index"` // viewed, approved, edited, marked_fixed, ignored
	AccessedBy    int64     `gorm:"index"`         // Telegram ID of tenant who accessed
	IPAddress     string    `gorm:"size:50"`
	UserAgent     string    `gorm:"type:text"`
	Timestamp     time.Time `gorm:"index"`
	Success       bool      `gorm:"default:true"`
	Reason        string    `gorm:"type:text"` // Failure reason if Success = false
}

// EncryptPaymentMessage encrypts sensitive payment information in message content
func (h *PaymentSecurityHandler) EncryptPaymentMessage(message string) (string, error) {
	// Extract and encrypt payment amounts
	encrypted := h.maskPaymentAmounts(message)
	
	// Encrypt account numbers if present
	encrypted = h.maskAccountNumbers(encrypted)
	
	// Full encryption of the entire message for storage
	encryptedMessage, err := utils.EncryptToken(encrypted)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt payment message: %w", err)
	}
	
	return encryptedMessage, nil
}

// DecryptPaymentMessage decrypts payment message for authorized viewing
func (h *PaymentSecurityHandler) DecryptPaymentMessage(encryptedMessage string) (string, error) {
	decrypted, err := utils.DecryptToken(encryptedMessage)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt payment message: %w", err)
	}
	
	return decrypted, nil
}

// maskPaymentAmounts masks payment amounts in logs but preserves for actual message
func (h *PaymentSecurityHandler) maskPaymentAmounts(message string) string {
	// Pattern: currency symbols followed by numbers
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(\$|€|£|₦|naira|dollar|euro|pound)\s*(\d{1,3}(?:[,\s]\d{3})*(?:\.\d{2})?)`),
		regexp.MustCompile(`(?i)(\d{1,3}(?:[,\s]\d{3})*(?:\.\d{2})?)\s*(naira|dollars|euros|pounds|usd|ngn|gbp|eur)`),
	}
	
	masked := message
	for _, pattern := range patterns {
		masked = pattern.ReplaceAllStringFunc(masked, func(match string) string {
			// Keep original for actual message, but can be masked in logs
			return match
		})
	}
	
	return masked
}

// maskAccountNumbers masks account numbers in logs
func (h *PaymentSecurityHandler) maskAccountNumbers(message string) string {
	// Pattern for account numbers (varies by format)
	// Nigerian account: 10 digits
	accountPattern := regexp.MustCompile(`\b\d{10}\b`)
	
	masked := accountPattern.ReplaceAllStringFunc(message, func(match string) string {
		if len(match) == 10 {
			// Mask all but last 4 digits
			return "******" + match[6:]
		}
		return match
	})
	
	return masked
}

// LogPaymentAccess logs all access to payment escalations for audit
func (h *PaymentSecurityHandler) LogPaymentAccess(ctx context.Context, tenantID uint, interactionID uint, action string, accessedBy int64, success bool, reason string) error {
	auditLog := PaymentAuditLog{
		TenantID:      tenantID,
		InteractionID: interactionID,
		Action:        action,
		AccessedBy:    accessedBy,
		Timestamp:     time.Now(),
		Success:       success,
		Reason:        reason,
	}
	
	if err := h.DB.Create(&auditLog).Error; err != nil {
		h.Logger.Error("failed to log payment access",
			zap.Uint("tenant_id", tenantID),
			zap.Uint("interaction_id", interactionID),
			zap.Error(err))
		return err
	}
	
	h.Logger.Info("🔒 Payment access logged",
		zap.Uint("tenant_id", tenantID),
		zap.Uint("interaction_id", interactionID),
		zap.String("action", action),
		zap.Bool("success", success))
	
	return nil
}

// VerifyPaymentAccess verifies that the tenant has authorization to access payment escalation
func (h *PaymentSecurityHandler) VerifyPaymentAccess(tenantID uint, interactionID uint, accessedBy int64) (bool, error) {
	// Load interaction
	var interaction storage.Interaction
	if err := h.DB.First(&interaction, interactionID).Error; err != nil {
		return false, fmt.Errorf("interaction not found: %w", err)
	}
	
	// Verify it belongs to the tenant
	if interaction.TenantID != tenantID {
		return false, fmt.Errorf("interaction does not belong to tenant")
	}
	
	// Load tenant to verify owner
	var tenant storage.Tenant
	if err := h.DB.First(&tenant, tenantID).Error; err != nil {
		return false, fmt.Errorf("tenant not found: %w", err)
	}
	
	// Verify accessed by tenant owner
	if tenant.OwnerTelegramID != accessedBy {
		return false, fmt.Errorf("unauthorized access: only tenant owner can access payment escalations")
	}
	
	// Verify tenant status is active
	if tenant.Status != "active" {
		return false, fmt.Errorf("tenant account is not active")
	}
	
	return true, nil
}

// MaskForLogging masks sensitive payment information for safe logging
func (h *PaymentSecurityHandler) MaskForLogging(message string) string {
	masked := message
	
	// Mask payment amounts
	amountPattern := regexp.MustCompile(`(?i)(\$|€|£|₦|naira|dollar|euro|pound)\s*(\d{1,3}(?:[,\s]\d{3})*(?:\.\d{2})?)`)
	masked = amountPattern.ReplaceAllStringFunc(masked, func(match string) string {
		return "[AMOUNT_MASKED]"
	})
	
	// Mask account numbers (10 digits)
	accountPattern := regexp.MustCompile(`\b\d{10}\b`)
	masked = accountPattern.ReplaceAllString(masked, "[ACCOUNT_MASKED]")
	
	// Mask email addresses (but keep domain)
	emailPattern := regexp.MustCompile(`\b([a-zA-Z0-9._%+-]+)@([a-zA-Z0-9.-]+\.[a-zA-Z]{2,})\b`)
	masked = emailPattern.ReplaceAllString(masked, "[EMAIL_MASKED]@$2")
	
	// Mask phone numbers
	phonePattern := regexp.MustCompile(`\b(?:\+?\d{1,3}[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`)
	masked = phonePattern.ReplaceAllString(masked, "[PHONE_MASKED]")
	
	return masked
}

// ValidatePaymentIntent ensures the message is actually about payment
func (h *PaymentSecurityHandler) ValidatePaymentIntent(message string) bool {
	lower := strings.ToLower(message)
	
	paymentKeywords := []string{
		"paid", "payment", "naira", "dollar", "euro", "pound",
		"transaction", "transfer", "deposit", "amount", "money",
		"account number", "account name", "bank", "wire",
	}
	
	for _, keyword := range paymentKeywords {
		if strings.Contains(lower, keyword) {
			// Additional validation: should contain amount or account info
			hasAmount := regexp.MustCompile(`\d+`).MatchString(message)
			if hasAmount {
				return true
			}
		}
	}
	
	return false
}

// GetPaymentAccessHistory retrieves access history for a payment escalation
func (h *PaymentSecurityHandler) GetPaymentAccessHistory(interactionID uint) ([]PaymentAuditLog, error) {
	var logs []PaymentAuditLog
	if err := h.DB.Where("interaction_id = ?", interactionID).
		Order("created_at DESC").
		Find(&logs).Error; err != nil {
		return nil, err
	}
	
	return logs, nil
}

// CheckRateLimit checks if tenant is accessing too many payment escalations too quickly
func (h *PaymentSecurityHandler) CheckRateLimit(tenantID uint, accessedBy int64) (bool, error) {
	// Check access in last 5 minutes
	cutoff := time.Now().Add(-5 * time.Minute)
	
	var count int64
	if err := h.DB.Model(&PaymentAuditLog{}).
		Where("tenant_id = ? AND accessed_by = ? AND created_at > ?", tenantID, accessedBy, cutoff).
		Count(&count).Error; err != nil {
		return false, err
	}
	
	// Limit: 20 payment escalations per 5 minutes
	if count >= 20 {
		return false, fmt.Errorf("rate limit exceeded: too many payment escalation accesses")
	}
	
	return true, nil
}

