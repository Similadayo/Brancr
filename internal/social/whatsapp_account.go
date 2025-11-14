package social

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/config"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SaveWhatsAppAccount persists or updates a tenant's WhatsApp social account,
// claims the business and WABA, then triggers the required Meta subscriptions.
func SaveWhatsAppAccount(db *gorm.DB, logger *zap.Logger, cfg *config.Config, tenantID uint, businessID, wabaID, phoneID, phoneNumber, token, status string) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	var account storage.SocialAccount
	err := db.Where("tenant_id = ? AND platform = ?", tenantID, "whatsapp").First(&account).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			account = storage.SocialAccount{
				TenantID: tenantID,
				Platform: "whatsapp",
			}
		} else {
			return err
		}
	}

	encrypted, err := encryptToken(token)
	if err != nil {
		return err
	}

	if encrypted != "" {
		account.AccessToken = encrypted
	}

	account.PageID = phoneID
	account.ExternalID = phoneID
	if strings.TrimSpace(phoneNumber) != "" {
		account.Username = phoneNumber
	}
	account.Mode = "oauth2"
	account.Connected = strings.ToUpper(status) == "APPROVED" || status == ""

	if err := db.Save(&account).Error; err != nil {
		return err
	}

	// Stage 2: Set up billing first (may be required for ownership claims)
	// Then claim the tenant's business and WABA (2025 automated access flow)
	if cfg != nil && businessID != "" && wabaID != "" {
		// Set up billing first - this may be required for successful ownership claims
		// Meta often requires payment method to be configured before claiming ownership
		setupWhatsAppBillingSync(logger, cfg, wabaID, tenantID)

		// Then proceed with claiming (billing should be set up first)
		go claimTenantBusinessAndWABA(logger, cfg, businessID, wabaID, tenantID, phoneID, token)
	}

	return nil
}

func encryptToken(token string) (string, error) {
	if token == "" {
		return "", nil
	}
	enc, err := utils.EncryptToken(token)
	if err != nil {
		return token, nil
	}
	return enc, nil
}

func subscribePhoneNumber(logger *zap.Logger, phoneID, tenantToken string) {
	phoneID = strings.TrimSpace(phoneID)
	tenantToken = strings.TrimSpace(tenantToken)
	if phoneID == "" || tenantToken == "" {
		return
	}

	go attemptPhoneSubscription(logger, phoneID, tenantToken, 0)
}

func attemptPhoneSubscription(logger *zap.Logger, phoneID, tenantToken string, attempt int) {
	const maxAttempts = 5
	backoff := []time.Duration{
		5 * time.Second,
		15 * time.Second,
		30 * time.Second,
		2 * time.Minute,
		5 * time.Minute,
	}

	endpoint := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/subscribed_apps", phoneID)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader("subscribed_fields=messages"))
	if err != nil {
		if logger != nil {
			logger.Warn("whatsapp phone subscription build failed", zap.String("phone_number_id", phoneID), zap.Error(err))
		}
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	q := make(url.Values)
	q.Set("access_token", tenantToken)
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if logger != nil {
			logger.Warn("whatsapp phone subscription request failed",
				zap.String("phone_number_id", phoneID),
				zap.Int("attempt", attempt+1),
				zap.Error(err))
		}
		retryPhoneSubscription(logger, phoneID, tenantToken, attempt, backoff, err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		err = fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		if logger != nil {
			logger.Warn("whatsapp phone subscription returned non-200",
				zap.String("phone_number_id", phoneID),
				zap.Int("attempt", attempt+1),
				zap.Int("status", resp.StatusCode),
				zap.ByteString("body", body))
		}
		retryPhoneSubscription(logger, phoneID, tenantToken, attempt, backoff, err)
		return
	}

	if logger != nil {
		logger.Info("WhatsApp phone number subscribed",
			zap.String("phone_number_id", phoneID),
			zap.ByteString("response", body))
	}
}

func retryPhoneSubscription(logger *zap.Logger, phoneID, tenantToken string, attempt int, backoff []time.Duration, reason error) {
	const maxAttempts = 5
	if attempt >= maxAttempts-1 || attempt >= len(backoff) {
		if logger != nil {
			logger.Error("max retries reached for whatsapp phone subscription",
				zap.String("phone_number_id", phoneID),
				zap.Error(reason))
		}
		return
	}

	wait := backoff[attempt]
	if logger != nil {
		logger.Info("retrying whatsapp phone subscription",
			zap.String("phone_number_id", phoneID),
			zap.Int("next_attempt", attempt+2),
			zap.Duration("wait", wait))
	}

	time.AfterFunc(wait, func() {
		attemptPhoneSubscription(logger, phoneID, tenantToken, attempt+1)
	})
}

func subscribeAppToWABA(logger *zap.Logger, cfg *config.Config, tenantID uint, wabaID string) {
	if strings.TrimSpace(wabaID) == "" || cfg == nil {
		return
	}

	sysToken := strings.TrimSpace(cfg.MetaSystemUserToken)
	if sysToken == "" {
		if logger != nil {
			logger.Warn("whatsapp subscription skipped: missing system user token", zap.Uint("tenant_id", tenantID))
		}
		return
	}

	data := url.Values{}
	data.Set("access_token", sysToken)
	data.Set("subscribed_fields", "messages")

	endpoint := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/subscribed_apps", wabaID)
	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		if logger != nil {
			logger.Warn("whatsapp subscription request failed", zap.Uint("tenant_id", tenantID), zap.String("waba_id", wabaID), zap.Error(err))
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		if logger != nil {
			body, _ := io.ReadAll(resp.Body)
			logger.Warn("whatsapp subscription returned non-200",
				zap.Uint("tenant_id", tenantID),
				zap.String("waba_id", wabaID),
				zap.Int("status", resp.StatusCode),
				zap.ByteString("body", body))
		}
		return
	}

	if logger != nil {
		logger.Info("WhatsApp WABA subscribed", zap.Uint("tenant_id", tenantID), zap.String("waba_id", wabaID))
	}
}

// claimTenantBusinessAndWABA performs the 2025 automated access flow:
// 1. Claims the tenant's business as managed by our business
// 2. Claims the WABA as owned by our business
// 3. Subscribes to webhooks once ownership is established
func claimTenantBusinessAndWABA(logger *zap.Logger, cfg *config.Config, tenantBusinessID, wabaID string, tenantID uint, phoneID, tenantToken string) {
	sysToken := strings.TrimSpace(cfg.MetaSystemUserToken)
	sysBusinessID := strings.TrimSpace(cfg.MetaBusinessID)

	if sysToken == "" || sysBusinessID == "" {
		if logger != nil {
			logger.Warn("whatsapp claiming skipped: missing system token or business id",
				zap.Uint("tenant_id", tenantID),
				zap.String("sys_token_present", fmt.Sprintf("%v", sysToken != "")),
				zap.String("sys_business_id_present", fmt.Sprintf("%v", sysBusinessID != "")))
		}
		return
	}

	// Step 1: Claim business ownership (non-fatal - might already be claimed)
	// Note: Billing should be set up BEFORE claiming (done in SaveWhatsAppAccount)
	if err := claimBusinessOwnership(logger, sysBusinessID, tenantBusinessID, sysToken, tenantID); err != nil {
		if logger != nil {
			logger.Warn("whatsapp business claiming failed (continuing anyway)",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err),
				zap.String("note", "business might already be claimed, or billing setup may be required first"))
		}
		// Continue to WABA claiming - embedded signup might have already established business relationship
	}

	// Step 2: Claim WABA ownership (non-fatal - might already be claimed or owned)
	// Note: We use OUR business ID (sysBusinessID) to claim WABA as owned by Brancr, not tenant's business ID
	if err := claimWABAOwnership(logger, sysBusinessID, wabaID, sysToken, tenantID); err != nil {
		if logger != nil {
			logger.Warn("whatsapp WABA claiming failed (continuing anyway)",
				zap.Uint("tenant_id", tenantID),
				zap.String("our_business_id", sysBusinessID),
				zap.String("waba_id", wabaID),
				zap.Error(err),
				zap.String("note", "WABA might already be claimed, owned, or billing/payment method setup may be required"))
		}
		// Continue to subscriptions - embedded signup may have already established ownership
	} else {
		if logger != nil {
			logger.Info("WhatsApp WABA claimed successfully", zap.Uint("tenant_id", tenantID), zap.String("waba_id", wabaID))
		}
	}

	// Step 3: Proceed with subscriptions (they may work even if claiming failed)
	if logger != nil {
		logger.Info("Proceeding with WhatsApp subscriptions", zap.Uint("tenant_id", tenantID))
	}

	// Subscribe to WABA
	go subscribeAppToWABA(logger, cfg, tenantID, wabaID)

	// Subscribe to phone number
	if tenantToken != "" {
		go subscribePhoneNumber(logger, phoneID, tenantToken)
	}
}

// setupWhatsAppBillingSync sets up billing synchronously (before claiming ownership).
// This is called BEFORE claiming to ensure billing is configured first.
func setupWhatsAppBillingSync(logger *zap.Logger, cfg *config.Config, wabaID string, tenantID uint) {
	setupWhatsAppBilling(logger, cfg, wabaID, tenantID, true)
}

// setupWhatsAppBilling configures billing for the WABA to use Brancr's payment method.
// This allows Brancr to handle payments for tenants instead of Meta billing them directly.
// If sync is true, it runs synchronously. If false, it's non-blocking (for retries).
func setupWhatsAppBilling(logger *zap.Logger, cfg *config.Config, wabaID string, tenantID uint, sync bool) {
	if cfg == nil || strings.TrimSpace(wabaID) == "" {
		return
	}

	// This is critical - Meta may require payment method to be set up BEFORE claiming ownership
	// Without this, claiming ownership may fail with permission errors
	paymentMethodID := strings.TrimSpace(cfg.MetaPaymentMethodID)

	if paymentMethodID == "" {
		if logger != nil {
			logger.Info("whatsapp billing setup skipped: payment method not configured",
				zap.Uint("tenant_id", tenantID),
				zap.String("waba_id", wabaID),
				zap.String("note", "Tenant will need to add payment method manually, or Brancr can handle billing via Meta Payment Processing API"))
		}
		return
	}

	sysToken := strings.TrimSpace(cfg.MetaSystemUserToken)
	if sysToken == "" {
		if logger != nil {
			logger.Warn("whatsapp billing setup skipped: missing system user token",
				zap.Uint("tenant_id", tenantID),
				zap.String("waba_id", wabaID))
		}
		return
	}

	// Set payment method for WABA
	// POST /{waba_id}/payment_method
	body := map[string]string{
		"payment_method_id": paymentMethodID,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		if logger != nil {
			logger.Warn("whatsapp billing setup failed: marshal error",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
		}
		return
	}

	endpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/payment_method?access_token=%s", wabaID, sysToken)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		if logger != nil {
			logger.Warn("whatsapp billing setup failed: request build error",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
		}
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if logger != nil {
			logger.Warn("whatsapp billing setup failed: request error",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
		}
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		if logger != nil {
			logger.Warn("whatsapp billing setup failed: API error",
				zap.Uint("tenant_id", tenantID),
				zap.String("waba_id", wabaID),
				zap.Int("status", resp.StatusCode),
				zap.ByteString("body", respBody),
				zap.String("note", "This may require Meta Payment Processing API access. Tenant may need to add payment method manually."))
		}
		return
	}

	if logger != nil {
		logger.Info("WhatsApp billing configured successfully",
			zap.Uint("tenant_id", tenantID),
			zap.String("waba_id", wabaID),
			zap.String("note", "Brancr payment method is now used for this WABA"))
	}
}

// claimBusinessOwnership claims the tenant's business as managed by our business.
// POST {our_business_id}/managed_businesses
func claimBusinessOwnership(logger *zap.Logger, ourBusinessID, tenantBusinessID, sysToken string, tenantID uint) error {
	body := map[string]string{
		"existing_client_business_id": tenantBusinessID,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	endpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/managed_businesses?access_token=%s", ourBusinessID, sysToken)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	if logger != nil {
		logger.Info("WhatsApp business claimed",
			zap.Uint("tenant_id", tenantID),
			zap.String("tenant_business_id", tenantBusinessID))
	}
	return nil
}

// claimWABAOwnership claims the WABA as owned by our business.
// POST {our_business_id}/owned_whatsapp_business_accounts
// IMPORTANT: Use OUR business ID (sysBusinessID), not tenant's business ID
// This endpoint adds the WABA to the list of WABAs owned by our business
func claimWABAOwnership(logger *zap.Logger, ourBusinessID, wabaID, sysToken string, tenantID uint) error {
	body := map[string]string{
		"wa_business_account_id": wabaID,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	endpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/owned_whatsapp_business_accounts?access_token=%s", ourBusinessID, sysToken)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	if logger != nil {
		logger.Info("WhatsApp WABA claimed",
			zap.Uint("tenant_id", tenantID),
			zap.String("waba_id", wabaID))
	}
	return nil
}
