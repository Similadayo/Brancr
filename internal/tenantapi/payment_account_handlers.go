package tenantapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/similadayo/brancr/internal/middleware"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
)

// =======================
// Payment Account Management
// =======================

// listPaymentAccounts lists all payment accounts for the tenant
func (h *Handler) listPaymentAccounts(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var accounts []storage.TenantPaymentAccount
	if err := h.DB.Where("tenant_id = ?", claims.TenantID).
		Order("is_default DESC, created_at DESC").
		Find(&accounts).Error; err != nil {
		h.Logger.Error("failed to fetch payment accounts", zap.Error(err))
		http.Error(w, "failed to fetch payment accounts", http.StatusInternalServerError)
		return
	}

	type accountResp struct {
		ID                  uint   `json:"id"`
		AccountType         string `json:"account_type"`
		BankName            string `json:"bank_name,omitempty"`
		AccountNumber       string `json:"account_number"` // Masked for security
		AccountName         string `json:"account_name"`
		MobileMoneyProvider string `json:"mobile_money_provider,omitempty"`
		PhoneNumber         string `json:"phone_number,omitempty"`
		IsDefault           bool   `json:"is_default"`
		IsActive            bool   `json:"is_active"`
		CreatedAt           string `json:"created_at"`
		UpdatedAt           string `json:"updated_at"`
	}

	resp := make([]accountResp, len(accounts))
	for i, acc := range accounts {
		// Decrypt account number for masking
		decryptedNumber, err := utils.DecryptToken(acc.AccountNumber)
		if err != nil {
			h.Logger.Warn("failed to decrypt account number for masking",
				zap.Error(err),
				zap.Uint("account_id", acc.ID))
			decryptedNumber = "" // Use empty string if decryption fails
		}
		// Mask account number - show only last 4 digits
		maskedNumber := maskAccountNumber(decryptedNumber)

		resp[i] = accountResp{
			ID:                  acc.ID,
			AccountType:         acc.AccountType,
			BankName:            acc.BankName,
			AccountNumber:       maskedNumber,
			AccountName:         acc.AccountName,
			MobileMoneyProvider: acc.MobileMoneyProvider,
			PhoneNumber:         acc.PhoneNumber,
			IsDefault:           acc.IsDefault,
			IsActive:            acc.IsActive,
			CreatedAt:           acc.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:           acc.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"payment_accounts": resp,
		"count":            len(resp),
	})
}

// createPaymentAccount creates a new payment account
func (h *Handler) createPaymentAccount(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		AccountType         string `json:"account_type"` // bank, mobile_money, cash
		BankName            string `json:"bank_name"`
		AccountNumber       string `json:"account_number"`
		AccountName         string `json:"account_name"`
		MobileMoneyProvider string `json:"mobile_money_provider"` // MTN, Airtel, etc.
		PhoneNumber         string `json:"phone_number"`
		IsDefault           bool   `json:"is_default"`
		IsActive            bool   `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate account type
	if req.AccountType != "bank" && req.AccountType != "mobile_money" && req.AccountType != "cash" {
		http.Error(w, "invalid account_type. Must be: bank, mobile_money, or cash", http.StatusBadRequest)
		return
	}

	// Validate required fields based on type
	if req.AccountType == "bank" {
		if req.BankName == "" || req.AccountNumber == "" || req.AccountName == "" {
			http.Error(w, "bank_name, account_number, and account_name are required for bank accounts", http.StatusBadRequest)
			return
		}
	} else if req.AccountType == "mobile_money" {
		if req.MobileMoneyProvider == "" || req.PhoneNumber == "" || req.AccountName == "" {
			http.Error(w, "mobile_money_provider, phone_number, and account_name are required for mobile money accounts", http.StatusBadRequest)
			return
		}
	}

	// If setting as default, unset other defaults
	if req.IsDefault {
		h.DB.Model(&storage.TenantPaymentAccount{}).
			Where("tenant_id = ? AND is_default = ?", claims.TenantID, true).
			Update("is_default", false)
	}

	// Encrypt account number before storing
	encryptedAccountNumber, err := utils.EncryptToken(req.AccountNumber)
	if err != nil {
		h.Logger.Error("failed to encrypt account number", zap.Error(err))
		http.Error(w, "failed to encrypt account number", http.StatusInternalServerError)
		return
	}

	account := storage.TenantPaymentAccount{
		TenantID:            claims.TenantID,
		AccountType:         req.AccountType,
		BankName:            req.BankName,
		AccountNumber:       encryptedAccountNumber, // Encrypted
		AccountName:         req.AccountName,
		MobileMoneyProvider: req.MobileMoneyProvider,
		PhoneNumber:         req.PhoneNumber,
		IsDefault:           req.IsDefault,
		IsActive:            req.IsActive,
	}

	if !req.IsActive {
		account.IsActive = true // Default to active
	}

	if err := h.DB.Create(&account).Error; err != nil {
		h.Logger.Error("failed to create payment account", zap.Error(err))
		http.Error(w, "failed to create payment account", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"payment_account": map[string]any{
			"id":           account.ID,
			"account_type": account.AccountType,
			"account_name": account.AccountName,
			"is_default":   account.IsDefault,
			"is_active":    account.IsActive,
		},
	})
}

// getPaymentAccount gets a single payment account
func (h *Handler) getPaymentAccount(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid payment account id", http.StatusBadRequest)
		return
	}

	var account storage.TenantPaymentAccount
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).
		First(&account).Error; err != nil {
		http.Error(w, "payment account not found", http.StatusNotFound)
		return
	}

	// Decrypt account number for masking
	decryptedNumber, err := utils.DecryptToken(account.AccountNumber)
	if err != nil {
		h.Logger.Warn("failed to decrypt account number for masking",
			zap.Error(err),
			zap.Uint("account_id", account.ID))
		decryptedNumber = "" // Use empty string if decryption fails
	}
	// Mask account number
	maskedNumber := maskAccountNumber(decryptedNumber)

	writeJSON(w, http.StatusOK, map[string]any{
		"payment_account": map[string]any{
			"id":                    account.ID,
			"account_type":          account.AccountType,
			"bank_name":             account.BankName,
			"account_number":        maskedNumber,
			"account_name":          account.AccountName,
			"mobile_money_provider": account.MobileMoneyProvider,
			"phone_number":          account.PhoneNumber,
			"is_default":            account.IsDefault,
			"is_active":             account.IsActive,
			"created_at":            account.CreatedAt.Format("2006-01-02T15:04:05Z"),
			"updated_at":            account.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		},
	})
}

// updatePaymentAccount updates a payment account
func (h *Handler) updatePaymentAccount(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid payment account id", http.StatusBadRequest)
		return
	}

	var account storage.TenantPaymentAccount
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).
		First(&account).Error; err != nil {
		http.Error(w, "payment account not found", http.StatusNotFound)
		return
	}

	var req struct {
		AccountType         *string `json:"account_type"`
		BankName            *string `json:"bank_name"`
		AccountNumber       *string `json:"account_number"`
		AccountName         *string `json:"account_name"`
		MobileMoneyProvider *string `json:"mobile_money_provider"`
		PhoneNumber         *string `json:"phone_number"`
		IsDefault           *bool   `json:"is_default"`
		IsActive            *bool   `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Update fields if provided
	if req.AccountType != nil {
		account.AccountType = *req.AccountType
	}
	if req.BankName != nil {
		account.BankName = *req.BankName
	}
	if req.AccountNumber != nil {
		// Encrypt account number before storing
		encryptedAccountNumber, err := utils.EncryptToken(*req.AccountNumber)
		if err != nil {
			h.Logger.Error("failed to encrypt account number", zap.Error(err))
			http.Error(w, "failed to encrypt account number", http.StatusInternalServerError)
			return
		}
		account.AccountNumber = encryptedAccountNumber
	}
	if req.AccountName != nil {
		account.AccountName = *req.AccountName
	}
	if req.MobileMoneyProvider != nil {
		account.MobileMoneyProvider = *req.MobileMoneyProvider
	}
	if req.PhoneNumber != nil {
		account.PhoneNumber = *req.PhoneNumber
	}
	if req.IsDefault != nil {
		// If setting as default, unset other defaults
		if *req.IsDefault {
			h.DB.Model(&storage.TenantPaymentAccount{}).
				Where("tenant_id = ? AND is_default = ? AND id != ?", claims.TenantID, true, id).
				Update("is_default", false)
		}
		account.IsDefault = *req.IsDefault
	}
	if req.IsActive != nil {
		account.IsActive = *req.IsActive
	}

	if err := h.DB.Save(&account).Error; err != nil {
		h.Logger.Error("failed to update payment account", zap.Error(err))
		http.Error(w, "failed to update payment account", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"payment_account": map[string]any{
			"id":           account.ID,
			"account_type": account.AccountType,
			"account_name": account.AccountName,
			"is_default":   account.IsDefault,
			"is_active":    account.IsActive,
		},
	})
}

// deletePaymentAccount deletes a payment account
func (h *Handler) deletePaymentAccount(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid payment account id", http.StatusBadRequest)
		return
	}

	var account storage.TenantPaymentAccount
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).
		First(&account).Error; err != nil {
		http.Error(w, "payment account not found", http.StatusNotFound)
		return
	}

	if err := h.DB.Delete(&account).Error; err != nil {
		h.Logger.Error("failed to delete payment account", zap.Error(err))
		http.Error(w, "failed to delete payment account", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "payment account deleted",
	})
}

// setDefaultPaymentAccount sets a payment account as default
func (h *Handler) setDefaultPaymentAccount(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.TenantFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid payment account id", http.StatusBadRequest)
		return
	}

	var account storage.TenantPaymentAccount
	if err := h.DB.Where("id = ? AND tenant_id = ?", id, claims.TenantID).
		First(&account).Error; err != nil {
		http.Error(w, "payment account not found", http.StatusNotFound)
		return
	}

	// Unset other defaults
	h.DB.Model(&storage.TenantPaymentAccount{}).
		Where("tenant_id = ? AND is_default = ? AND id != ?", claims.TenantID, true, id).
		Update("is_default", false)

	// Set this as default
	account.IsDefault = true
	if err := h.DB.Save(&account).Error; err != nil {
		h.Logger.Error("failed to set default payment account", zap.Error(err))
		http.Error(w, "failed to set default payment account", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "payment account set as default",
	})
}

// maskAccountNumber masks account number for security (shows only last 4 digits)
func maskAccountNumber(accountNumber string) string {
	if len(accountNumber) <= 4 {
		return "****"
	}
	// Show only last 4 digits, mask the rest
	masked := "****"
	for i := 0; i < len(accountNumber)-4; i++ {
		masked += "*"
	}
	return masked + accountNumber[len(accountNumber)-4:]
}
