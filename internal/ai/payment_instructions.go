package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/similadayo/brancr/internal/storage"
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
func (g *PaymentInstructionGenerator) GeneratePaymentInstructions(ctx context.Context, tenantID uint, amount float64, currency string) (string, error) {
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
			instructions.WriteString(fmt.Sprintf("Account Number: %s\n", account.AccountNumber))
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

	instructions.WriteString("\nAfter payment, please send proof of payment (screenshot or transaction reference) for confirmation.")

	return instructions.String(), nil
}

// GeneratePaymentReference generates a unique payment reference code
func (g *PaymentInstructionGenerator) GeneratePaymentReference(tenantID uint) string {
	// Format: BRNCR-{TENANT_ID}-{DATE}-{RANDOM}
	// Example: BRNCR-123-20241204-ABC123
	return fmt.Sprintf("BRNCR-%d-%s", tenantID, generateRandomString(6))
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

