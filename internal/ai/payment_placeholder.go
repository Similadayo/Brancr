package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"gorm.io/gorm"
)

// AccountPlaceholder is used in prompts to prevent LLM from masking account numbers
// The LLM receives this placeholder, and we replace it with real number after generation
const (
	AccountNumberPlaceholder1 = "{{BANK_ACCOUNT_1}}"
	AccountNumberPlaceholder2 = "{{BANK_ACCOUNT_2}}"
	AccountNumberPlaceholder3 = "{{BANK_ACCOUNT_3}}"
)

// PaymentPlaceholderReplacer replaces account placeholders with real numbers after LLM response
// This prevents LLM from masking sensitive data like account numbers
type PaymentPlaceholderReplacer struct {
	DB *gorm.DB
}

// NewPaymentPlaceholderReplacer creates a new replacer
func NewPaymentPlaceholderReplacer(db *gorm.DB) *PaymentPlaceholderReplacer {
	return &PaymentPlaceholderReplacer{DB: db}
}

// ReplaceAccountPlaceholders replaces all account placeholders with real decrypted account numbers
// This should be called AFTER the LLM generates a response
func (r *PaymentPlaceholderReplacer) ReplaceAccountPlaceholders(ctx context.Context, tenantID uuid.UUID, response string) string {
	if !strings.Contains(response, "{{BANK_ACCOUNT_") {
		return response // No placeholders to replace
	}

	// Get tenant's payment accounts
	var accounts []storage.TenantPaymentAccount
	if err := r.DB.Where("tenant_id = ? AND is_active = ?", tenantID, true).
		Order("is_default DESC").
		Find(&accounts).Error; err != nil || len(accounts) == 0 {
		// Can't get accounts - leave placeholders as-is (will be visible to customer as unfilled)
		return response
	}

	result := response

	// Replace placeholders with actual account numbers
	for i, account := range accounts {
		if account.AccountType == "bank" {
			decrypted, err := utils.DecryptToken(account.AccountNumber)
			if err != nil {
				continue // Skip if decryption fails
			}

			placeholder := fmt.Sprintf("{{BANK_ACCOUNT_%d}}", i+1)
			result = strings.ReplaceAll(result, placeholder, decrypted)
		}
	}

	// Also replace generic placeholder if used
	if strings.Contains(result, "{{ACCOUNT_NUMBER}}") && len(accounts) > 0 {
		for _, account := range accounts {
			if account.AccountType == "bank" && account.IsDefault {
				decrypted, err := utils.DecryptToken(account.AccountNumber)
				if err == nil {
					result = strings.ReplaceAll(result, "{{ACCOUNT_NUMBER}}", decrypted)
				}
				break
			}
		}
	}

	return result
}

// GetAccountsWithPlaceholders returns payment info with placeholders for LLM context
// The actual numbers are replaced AFTER LLM generates response
func (r *PaymentPlaceholderReplacer) GetAccountsWithPlaceholders(ctx context.Context, tenantID uuid.UUID) string {
	var accounts []storage.TenantPaymentAccount
	if err := r.DB.Where("tenant_id = ? AND is_active = ?", tenantID, true).
		Order("is_default DESC").
		Find(&accounts).Error; err != nil || len(accounts) == 0 {
		return ""
	}

	var info strings.Builder
	info.WriteString("\nPAYMENT ACCOUNTS:\n")

	for i, account := range accounts {
		if account.AccountType == "bank" {
			info.WriteString(fmt.Sprintf("- Bank: %s\n", account.BankName))
			info.WriteString(fmt.Sprintf("  Account Name: %s\n", account.AccountName))
			// Use placeholder instead of real number - prevents LLM from masking
			info.WriteString(fmt.Sprintf("  Account Number: {{BANK_ACCOUNT_%d}}\n", i+1))
			if account.IsDefault {
				info.WriteString("  (Primary payment method)\n")
			}
		} else if account.AccountType == "mobile_money" {
			info.WriteString(fmt.Sprintf("- %s Mobile Money\n", account.MobileMoneyProvider))
			info.WriteString(fmt.Sprintf("  Account Name: %s\n", account.AccountName))
			info.WriteString(fmt.Sprintf("  Phone Number: %s\n", account.PhoneNumber))
			if account.IsDefault {
				info.WriteString("  (Primary payment method)\n")
			}
		}
	}

	info.WriteString("\nIMPORTANT: When sharing account numbers, use the EXACT placeholder format shown above (e.g., {{BANK_ACCOUNT_1}}). These will be replaced with real numbers.\n")

	return info.String()
}
