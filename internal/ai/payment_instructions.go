package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// PaymentInstructionGenerator generates payment instructions for customers
type PaymentInstructionGenerator struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewPaymentInstructionGenerator creates a new payment instruction generator
func NewPaymentInstructionGenerator(db *gorm.DB, logger *zap.Logger) *PaymentInstructionGenerator {
	return &PaymentInstructionGenerator{
		DB:     db,
		Logger: logger,
	}
}

// GeneratePaymentInstructions generates payment instructions based on tenant's payment accounts
// orderNumber and paymentReference are optional - if provided, they'll be included in instructions
// portalURL is optional - if provided, it'll be included as a link to track order
func (g *PaymentInstructionGenerator) GeneratePaymentInstructions(ctx context.Context, tenantID uuid.UUID, amount float64, currency string, orderNumber string, paymentReference string, portalURL string) (string, error) {
	// Get tenant's payment accounts
	var accounts []storage.TenantPaymentAccount
	if err := g.DB.Where("tenant_id = ? AND is_active = ?", tenantID, true).
		Order("is_default DESC").
		Find(&accounts).Error; err != nil {
		return "", fmt.Errorf("failed to fetch payment accounts: %w", err)
	}

	if len(accounts) == 0 {
		return "Please contact us directly for payment instructions.", nil
	}

	var instructions strings.Builder
	instructions.WriteString(fmt.Sprintf("Payment Details for %.2f %s:\n\n", amount, currency))

	for i, account := range accounts {
		if i > 0 {
			instructions.WriteString("\n")
		}

		switch account.AccountType {
		case "bank":
			instructions.WriteString(fmt.Sprintf("Bank Transfer:\n"))
			instructions.WriteString(fmt.Sprintf("Bank Name: %s\n", account.BankName))
			instructions.WriteString(fmt.Sprintf("Account Name: %s\n", account.AccountName))

			// CRITICAL: Use placeholder to prevent masking by sanitizers/LLM
			// Real numbers are injected by finalizeReply() at final send boundary
			instructions.WriteString(fmt.Sprintf("Account Number: {{BANK_ACCOUNT_%d}}\n", i+1))

			if account.IsDefault {
				instructions.WriteString("(Primary payment method)\n")
			}

		case "mobile_money":
			instructions.WriteString(fmt.Sprintf("%s Mobile Money:\n", account.MobileMoneyProvider))
			instructions.WriteString(fmt.Sprintf("Phone Number: %s\n", account.PhoneNumber))
			instructions.WriteString(fmt.Sprintf("Account Name: %s\n", account.AccountName))
			if account.IsDefault {
				instructions.WriteString("(Primary payment method)\n")
			}

		case "cash":
			instructions.WriteString("Cash Payment:\n")
			instructions.WriteString("Please arrange for cash payment directly with us.\n")
		}
	}

	// Add order details if provided
	if orderNumber != "" {
		instructions.WriteString(fmt.Sprintf("\n\nOrder Number: %s", orderNumber))
	}
	if paymentReference != "" {
		instructions.WriteString(fmt.Sprintf("\nPayment Reference: %s\nPlease include this reference when making payment.", paymentReference))
	}

	// Add portal link if provided
	if portalURL != "" {
		instructions.WriteString(fmt.Sprintf("\n\n📱 Track your order and download receipt: %s", portalURL))
	}

	// NOTE: Do NOT add hardcoded follow-up messages like "send proof of payment"
	// The AI should naturally and dynamically ask for confirmation in its own words

	return instructions.String(), nil
}

// GeneratePaymentReference generates a unique payment reference code prefixed by tenant name and item label
func (g *PaymentInstructionGenerator) GeneratePaymentReference(tenantName string, tenantID uuid.UUID, itemLabel string) string {
	return utils.GeneratePaymentReference(tenantName, tenantID, itemLabel)
}

// generateRandomString generates a random alphanumeric string
func generateRandomString(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[i%len(charset)]
	}
	return string(b)
}
