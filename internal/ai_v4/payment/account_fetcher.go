package payment

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"gorm.io/gorm"
)

// GetDefaultPaymentAccount fetches the tenant's default payment account
func GetDefaultPaymentAccount(db *gorm.DB, tenantID uuid.UUID) (*storage.TenantPaymentAccount, error) {
	var account storage.TenantPaymentAccount
	err := db.Where("tenant_id = ? AND is_default = ? AND is_active = ?",
		tenantID, true, true).
		First(&account).Error

	if err != nil {
		return nil, err
	}

	return &account, nil
}

// GetDecryptedAccountNumber decrypts the account number for display
func GetDecryptedAccountNumber(account *storage.TenantPaymentAccount) string {
	if account.AccountNumber == "" {
		return ""
	}

	// Try to decrypt (account number is stored encrypted)
	decrypted, err := utils.DecryptToken(account.AccountNumber)
	if err != nil {
		// If decryption fails, return last 4 chars only for safety
		if len(account.AccountNumber) > 4 {
			return "****" + account.AccountNumber[len(account.AccountNumber)-4:]
		}
		return "****"
	}

	return decrypted
}

// FormatPaymentInstructions generates formatted payment instructions
func FormatPaymentInstructions(
	account *storage.TenantPaymentAccount,
	paymentRef string,
	amount float64,
	currency string,
) string {
	// Decrypt account number before displaying
	accountNumber := GetDecryptedAccountNumber(account)

	return fmt.Sprintf(`Payment Details:

Bank: %s
Account Number: %s
Account Name: %s
Amount: %s%.2f
Reference: %s

Please use the reference when making payment.`,
		account.BankName,
		accountNumber,
		account.AccountName,
		currency,
		amount,
		paymentRef)
}
