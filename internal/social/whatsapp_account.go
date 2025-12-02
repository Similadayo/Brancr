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

// SyncPhoneNumbersFromMeta fetches all phone numbers from Brancr's WABA and updates the database pool
func SyncPhoneNumbersFromMeta(db *gorm.DB, logger *zap.Logger, cfg *config.Config) error {
	if cfg.BrancrWABAID == "" || cfg.BrancrWABAToken == "" {
		if logger != nil {
			logger.Info("phone number sync skipped: WABA ID or token not configured")
		}
		return nil
	}

	wabaID := cfg.BrancrWABAID

	// Try to get phone numbers directly from WABA ID
	// Request all fields we need including verification status
	endpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/phone_numbers?fields=id,display_phone_number,verified_name,quality_rating,code_verification_status&access_token=%s", wabaID, cfg.BrancrWABAToken)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// If we get an error saying it's a Business node, try to get WABAs from Business first
	if resp.StatusCode == 400 && strings.Contains(string(body), "Business") && strings.Contains(string(body), "phone_numbers") {
		if logger != nil {
			logger.Warn("BrancrWABAID appears to be a Business ID, attempting to get WABA from Business", zap.String("business_id", wabaID))
		}

		// Get owned WABAs from Business
		wabaEndpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/owned_whatsapp_business_accounts?access_token=%s", wabaID, cfg.BrancrWABAToken)
		wabaReq, err := http.NewRequest(http.MethodGet, wabaEndpoint, nil)
		if err != nil {
			return fmt.Errorf("failed to create WABA request: %w", err)
		}

		wabaResp, err := http.DefaultClient.Do(wabaReq)
		if err != nil {
			return fmt.Errorf("WABA request failed: %w", err)
		}
		defer wabaResp.Body.Close()

		wabaBody, _ := io.ReadAll(wabaResp.Body)
		if wabaResp.StatusCode >= 400 {
			if logger != nil {
				logger.Error("failed to get WABAs from Business", zap.Int("status", wabaResp.StatusCode), zap.ByteString("body", wabaBody))
			}
			return fmt.Errorf("failed to get WABAs from Business (status %d): %s. Please ensure BRANCR_WABA_ID is a WABA ID, not a Business ID", wabaResp.StatusCode, string(wabaBody))
		}

		var wabaResponse struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}

		if err := json.Unmarshal(wabaBody, &wabaResponse); err != nil {
			return fmt.Errorf("failed to parse WABA response: %w", err)
		}

		if len(wabaResponse.Data) == 0 {
			return fmt.Errorf("no WABAs found for Business ID %s. Please configure BRANCR_WABA_ID with an actual WABA ID", wabaID)
		}

		// Use the first WABA ID
		wabaID = wabaResponse.Data[0].ID
		if logger != nil {
			logger.Info("found WABA from Business", zap.String("waba_id", wabaID))
		}

		// Retry with the actual WABA ID
		// Request all fields we need including verification status
		endpoint = fmt.Sprintf("https://graph.facebook.com/v20.0/%s/phone_numbers?fields=id,display_phone_number,verified_name,quality_rating,code_verification_status&access_token=%s", wabaID, cfg.BrancrWABAToken)
		req, err = http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		body, _ = io.ReadAll(resp.Body)
	}

	if resp.StatusCode >= 400 {
		if logger != nil {
			logger.Error("failed to fetch phone numbers", zap.Int("status", resp.StatusCode), zap.ByteString("body", body), zap.String("endpoint", endpoint))
		}
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Data []struct {
			ID                     string `json:"id"`
			DisplayPhoneNumber     string `json:"display_phone_number"`
			VerifiedName           string `json:"verified_name"`
			QualityRating          string `json:"quality_rating"`
			CodeVerificationStatus string `json:"code_verification_status"` // PENDING, VERIFIED, etc.
		} `json:"data"`
		Paging struct {
			Next string `json:"next"`
		} `json:"paging"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if logger != nil {
		logger.Info("syncing phone numbers from Meta", zap.Int("count", len(response.Data)))
	}

	for _, phoneData := range response.Data {
		var phoneNum storage.WhatsAppPhoneNumber
		err := db.Where("phone_number_id = ?", phoneData.ID).First(&phoneNum).Error

		if err == gorm.ErrRecordNotFound {
			// Create new phone number
			phoneNum = storage.WhatsAppPhoneNumber{
				PhoneNumberID: phoneData.ID,
				PhoneNumber:   phoneData.DisplayPhoneNumber,
				WABAID:        wabaID, // Use the resolved WABA ID
				VerifiedName:  phoneData.VerifiedName,
				QualityRating: phoneData.QualityRating,
				Status:        "available",
			}
			if err := db.Create(&phoneNum).Error; err != nil {
				if logger != nil {
					logger.Error("failed to create phone number", zap.Error(err), zap.String("phone_id", phoneData.ID))
				}
				continue
			}
			if logger != nil {
				logger.Info("created phone number",
					zap.String("phone_id", phoneData.ID),
					zap.String("number", phoneData.DisplayPhoneNumber),
					zap.String("verification_status", phoneData.CodeVerificationStatus),
					zap.String("verified_name", phoneData.VerifiedName))
			}
		} else if err == nil {
			// Update verification status and quality rating (always update these from Meta)
			phoneNum.VerifiedName = phoneData.VerifiedName
			phoneNum.QualityRating = phoneData.QualityRating

			// Only update internal status if not assigned
			if phoneNum.TenantID == nil || *phoneNum.TenantID == 0 {
				phoneNum.Status = "available"
			}

			if err := db.Save(&phoneNum).Error; err != nil {
				if logger != nil {
					logger.Error("failed to update phone number", zap.Error(err), zap.String("phone_id", phoneData.ID))
				}
				continue
			}

			if logger != nil {
				logger.Debug("synced phone number verification status",
					zap.String("phone_id", phoneData.ID),
					zap.String("verification_status", phoneData.CodeVerificationStatus),
					zap.String("verified_name", phoneData.VerifiedName))
			}
		} else {
			if logger != nil {
				logger.Error("failed to query phone number", zap.Error(err), zap.String("phone_id", phoneData.ID))
			}
			continue
		}
	}

	if logger != nil {
		logger.Info("phone number sync completed", zap.Int("synced", len(response.Data)))
	}

	return nil
}

// RequestPhoneNumberFromMeta requests a new phone number to be added to Brancr's WABA
// This initiates the registration process - Meta will send a verification code
func RequestPhoneNumberFromMeta(logger *zap.Logger, cfg *config.Config, phoneNumber, displayName string) (string, error) {
	if cfg.BrancrWABAID == "" || cfg.BrancrWABAToken == "" {
		return "", fmt.Errorf("WABA ID or token not configured")
	}

	// Format phone number (remove spaces, ensure + prefix)
	phoneNumber = strings.TrimSpace(phoneNumber)
	if !strings.HasPrefix(phoneNumber, "+") {
		phoneNumber = "+" + phoneNumber
	}

	// Meta API endpoint to request phone number registration
	// Note: This endpoint may require special permissions
	endpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/phone_numbers", cfg.BrancrWABAID)

	payload := map[string]string{
		"phone_number": phoneNumber,
		"display_name": displayName,
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.URL.RawQuery = "access_token=" + cfg.BrancrWABAToken

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		if logger != nil {
			logger.Error("failed to request phone number", zap.Int("status", resp.StatusCode), zap.ByteString("body", body))
		}
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		ID                 string `json:"id"` // phone_number_id
		DisplayPhoneNumber string `json:"display_phone_number"`
		VerificationStatus string `json:"code_verification_status"` // PENDING, VERIFIED, etc.
		Success            bool   `json:"success"`
		Message            string `json:"message"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if logger != nil {
		logger.Info("phone number registration requested",
			zap.String("phone_number", phoneNumber),
			zap.String("phone_number_id", response.ID),
			zap.String("verification_status", response.VerificationStatus))
	}

	// Meta will send verification code to the phone number
	// Return the phone_number_id so admin can verify it later
	return response.ID, nil
}

// RegisterPhoneNumber registers a phone number with Meta (REQUIRED before verification)
// This must be called BEFORE verify_code to activate the number
func RegisterPhoneNumber(logger *zap.Logger, cfg *config.Config, phoneNumberID string) error {
	if cfg.BrancrWABAID == "" || cfg.BrancrWABAToken == "" {
		return fmt.Errorf("WABA ID or token not configured")
	}

	systemToken := cfg.MetaSystemUserToken
	if systemToken == "" {
		systemToken = cfg.BrancrWABAToken // Fallback to WABA token
	}
	if systemToken == "" {
		return fmt.Errorf("system user token not configured")
	}

	// Meta API endpoint to register phone number
	endpoint := fmt.Sprintf("https://graph.facebook.com/v19.0/%s/register", phoneNumberID)

	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+systemToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		if logger != nil {
			logger.Error("failed to register phone number", zap.Int("status", resp.StatusCode), zap.ByteString("body", body))
		}
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message,omitempty"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		if logger != nil {
			logger.Warn("failed to parse register response", zap.Error(err), zap.ByteString("body", body))
		}
		// Still consider it success if we got 200 OK
		if resp.StatusCode == 200 {
			if logger != nil {
				logger.Info("phone number registered successfully", zap.String("phone_number_id", phoneNumberID))
			}
			return nil
		}
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if logger != nil {
		logger.Info("phone number registered successfully",
			zap.String("phone_number_id", phoneNumberID),
			zap.Bool("success", response.Success))
	}

	return nil
}

// VerifyPhoneNumberWithCode completes phone number verification by submitting the code
// NOTE: Must call RegisterPhoneNumber FIRST before calling this
func VerifyPhoneNumberWithCode(logger *zap.Logger, cfg *config.Config, phoneNumberID, verificationCode string) error {
	if cfg.BrancrWABAID == "" || cfg.BrancrWABAToken == "" {
		return fmt.Errorf("WABA ID or token not configured")
	}

	systemToken := cfg.MetaSystemUserToken
	if systemToken == "" {
		systemToken = cfg.BrancrWABAToken // Fallback to WABA token
	}
	if systemToken == "" {
		return fmt.Errorf("system user token not configured")
	}

	// Meta API endpoint to verify phone number with code
	endpoint := fmt.Sprintf("https://graph.facebook.com/v19.0/%s/verify_code", phoneNumberID)

	payload := map[string]string{
		"code": verificationCode,
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+systemToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		if logger != nil {
			logger.Error("failed to verify phone number", zap.Int("status", resp.StatusCode), zap.ByteString("body", body))
		}
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	if logger != nil {
		logger.Info("phone number verified successfully", zap.String("phone_number_id", phoneNumberID))
	}

	return nil
}

// RequestPhoneNumberVerificationCode requests a verification code for an existing phone number
// NOTE: Phone numbers must be added MANUALLY in Meta Business Manager first
// This function requests a verification code for an already-added phone number
// Endpoint: POST /{Phone-Number-ID}/request_code
func RequestPhoneNumberVerificationCode(logger *zap.Logger, cfg *config.Config, phoneNumberID, codeMethod, locale string) error {
	if cfg.BrancrWABAID == "" || cfg.BrancrWABAToken == "" {
		return fmt.Errorf("WABA ID or token not configured")
	}

	if codeMethod == "" {
		codeMethod = "SMS" // Default to SMS
	}
	if locale == "" {
		locale = "en_US" // Default locale
	}

	// Meta API endpoint to request verification code for existing phone number
	// POST /{Phone-Number-ID}/request_code
	endpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/request_code", phoneNumberID)

	payload := map[string]string{
		"code_method": codeMethod, // SMS or VOICE
		"locale":      locale,     // e.g., "en_US"
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.URL.RawQuery = "access_token=" + cfg.BrancrWABAToken

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		if logger != nil {
			logger.Error("failed to request verification code", zap.Int("status", resp.StatusCode), zap.ByteString("body", body))
		}
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	if logger != nil {
		logger.Info("verification code requested",
			zap.String("phone_number_id", phoneNumberID),
			zap.String("method", codeMethod))
	}

	return nil
}

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
	
	// Mark as connected - Embedded Signup creates active accounts
	// Status might be "APPROVED", "CONNECTED", "LIVE", or empty - all mean account is ready
	account.Connected = true // Embedded Signup accounts are connected by default

	if err := db.Save(&account).Error; err != nil {
		return err
	}

	// Stage 2: Complete Solution Partner setup (SYNCHRONOUSLY for immediate activation)
	// IMPORTANT: Do these steps SYNCHRONOUSLY so account is ready immediately
	if cfg != nil && businessID != "" && wabaID != "" {
		// Step 1: Add system user to WABA FIRST (REQUIRED for all API calls)
		// This must be done synchronously so we can make API calls immediately
		addSystemUserToWABA(logger, cfg, wabaID, tenantID)

		// Step 2: Set up credit sharing IMMEDIATELY (SYNCHRONOUS)
		// This tells Meta that Brancr is the "Bill To Party" and handles all payments
		// When credit sharing is active, Meta knows not to collect payment method from tenants
		setupCreditSharingSync(logger, cfg, businessID, wabaID, tenantID)

		// Step 3: Set up billing (payment method) - Brancr's payment method
		// This configures the WABA to use Brancr's payment method automatically
		setupWhatsAppBillingSync(logger, cfg, wabaID, tenantID)

		// Step 4: Subscribe to webhooks SYNCHRONOUSLY (required for receiving messages)
		// Subscribe to WABA webhooks
		subscribeAppToWABASync(logger, cfg, tenantID, wabaID)
		
		// Subscribe to phone number webhooks (use system token, not tenant token)
		if sysToken := strings.TrimSpace(cfg.MetaSystemUserToken); sysToken != "" {
			subscribePhoneNumberSync(logger, phoneID, sysToken)
		}

		// Step 5: Verify phone number is ready (check status)
		// This ensures the account is actually active and can send/receive messages
		if phoneID != "" && cfg.MetaSystemUserToken != "" {
			verifyPhoneNumberReady(logger, phoneID, cfg.MetaSystemUserToken, tenantID, db)
		}

		// Step 6: Claim the tenant's business and WABA (async - non-blocking)
		// This establishes ownership relationship
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

// SubscribePhoneNumberToWebhooks subscribes a phone number to receive webhook events
// This is a public wrapper for the private subscribePhoneNumber function
func SubscribePhoneNumberToWebhooks(logger *zap.Logger, phoneID, accessToken string) {
	subscribePhoneNumber(logger, phoneID, accessToken)
}

// subscribePhoneNumberSync subscribes to phone number webhooks synchronously
func subscribePhoneNumberSync(logger *zap.Logger, phoneID, accessToken string) {
	phoneID = strings.TrimSpace(phoneID)
	accessToken = strings.TrimSpace(accessToken)
	if phoneID == "" || accessToken == "" {
		return
	}

	// Subscribe synchronously (don't use async retry for initial setup)
	endpoint := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/subscribed_apps", phoneID)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader("subscribed_fields=messages,statuses"))
	if err != nil {
		if logger != nil {
			logger.Warn("whatsapp phone subscription build failed", zap.String("phone_number_id", phoneID), zap.Error(err))
		}
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	q := make(url.Values)
	q.Set("access_token", accessToken)
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if logger != nil {
			logger.Warn("whatsapp phone subscription request failed",
				zap.String("phone_number_id", phoneID),
				zap.Error(err))
		}
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		if logger != nil {
			logger.Warn("whatsapp phone subscription returned non-200",
				zap.String("phone_number_id", phoneID),
				zap.Int("status", resp.StatusCode),
				zap.ByteString("body", body))
		}
		return
	}

	if logger != nil {
		logger.Info("✅ WhatsApp phone number subscribed (synchronous)",
			zap.String("phone_number_id", phoneID),
			zap.ByteString("response", body))
	}
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
	backoff := []time.Duration{
		5 * time.Second,
		15 * time.Second,
		30 * time.Second,
		2 * time.Minute,
		5 * time.Minute,
	}

	endpoint := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/subscribed_apps", phoneID)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader("subscribed_fields=messages,statuses"))
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

// CheckPhoneNumberWebhookSubscription checks if a phone number is subscribed to webhooks
func CheckPhoneNumberWebhookSubscription(logger *zap.Logger, phoneID, accessToken string) (bool, error) {
	if phoneID == "" || accessToken == "" {
		return false, fmt.Errorf("phone_number_id and access_token are required")
	}

	endpoint := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/subscribed_apps", phoneID)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	q := make(url.Values)
	q.Set("access_token", accessToken)
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		if logger != nil {
			logger.Warn("failed to check webhook subscription",
				zap.String("phone_number_id", phoneID),
				zap.Int("status", resp.StatusCode),
				zap.ByteString("body", body))
		}
		return false, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Data []struct {
			AppID            string   `json:"id"`
			SubscribedFields []string `json:"subscribed_fields"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		if logger != nil {
			logger.Warn("failed to parse subscription check response",
				zap.String("phone_number_id", phoneID),
				zap.Error(err),
				zap.ByteString("body", body))
		}
		return false, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if our app is subscribed and has messages field
	for _, app := range response.Data {
		for _, field := range app.SubscribedFields {
			if field == "messages" {
				if logger != nil {
					logger.Info("phone number is subscribed to webhooks",
						zap.String("phone_number_id", phoneID),
						zap.String("app_id", app.AppID))
				}
				return true, nil
			}
		}
	}

	if logger != nil {
		logger.Warn("phone number is not subscribed to messages webhook",
			zap.String("phone_number_id", phoneID),
			zap.Int("apps_count", len(response.Data)))
	}
	return false, nil
}

// CheckWABASubscription checks if the app is subscribed to WABA-level webhooks
func CheckWABASubscription(logger *zap.Logger, wabaID, accessToken string) (bool, []string, error) {
	if wabaID == "" || accessToken == "" {
		return false, nil, fmt.Errorf("waba_id and access_token are required")
	}

	endpoint := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/subscribed_apps", wabaID)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false, nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := make(url.Values)
	q.Set("access_token", accessToken)
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		if logger != nil {
			logger.Warn("failed to check WABA subscription",
				zap.String("waba_id", wabaID),
				zap.Int("status", resp.StatusCode),
				zap.ByteString("body", body))
		}
		return false, nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Data []struct {
			AppID            string   `json:"id"`
			SubscribedFields []string `json:"subscribed_fields"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		if logger != nil {
			logger.Warn("failed to parse WABA subscription check response",
				zap.String("waba_id", wabaID),
				zap.Error(err),
				zap.ByteString("body", body))
		}
		return false, nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if our app is subscribed and get subscribed fields
	for _, app := range response.Data {
		hasMessages := false
		for _, field := range app.SubscribedFields {
			if field == "messages" {
				hasMessages = true
			}
		}
		if hasMessages {
			if logger != nil {
				logger.Info("WABA is subscribed to webhooks",
					zap.String("waba_id", wabaID),
					zap.String("app_id", app.AppID),
					zap.Strings("subscribed_fields", app.SubscribedFields))
			}
			return true, app.SubscribedFields, nil
		}
	}

	if logger != nil {
		logger.Warn("WABA is not subscribed to messages webhook",
			zap.String("waba_id", wabaID),
			zap.Int("apps_count", len(response.Data)))
	}
	return false, nil, nil
}

// verifyPhoneNumberReady verifies that a phone number is ready to send/receive messages
// and updates the account status if needed
func verifyPhoneNumberReady(logger *zap.Logger, phoneID, accessToken string, tenantID uint, db *gorm.DB) {
	if phoneID == "" || accessToken == "" {
		return
	}

	// Get phone number status from Meta
	status, err := GetPhoneNumberStatus(logger, phoneID, accessToken)
	if err != nil {
		if logger != nil {
			logger.Warn("failed to verify phone number status",
				zap.Uint("tenant_id", tenantID),
				zap.String("phone_number_id", phoneID),
				zap.Error(err))
		}
		return
	}

	// Check if phone number is verified and ready
	codeVerificationStatus := ""
	if val, ok := status["code_verification_status"].(string); ok {
		codeVerificationStatus = strings.ToUpper(val)
	}

	phoneStatus := ""
	if val, ok := status["status"].(string); ok {
		phoneStatus = strings.ToUpper(val)
	}

	// Phone number is ready if:
	// - code_verification_status is "VERIFIED" (or empty for trial accounts)
	// - status is "CONNECTED" or "LIVE" (or empty)
	isReady := (codeVerificationStatus == "VERIFIED" || codeVerificationStatus == "" || codeVerificationStatus == "PENDING") &&
		(phoneStatus == "CONNECTED" || phoneStatus == "LIVE" || phoneStatus == "")

	if logger != nil {
		logger.Info("Phone number status check",
			zap.Uint("tenant_id", tenantID),
			zap.String("phone_number_id", phoneID),
			zap.String("code_verification_status", codeVerificationStatus),
			zap.String("status", phoneStatus),
			zap.Bool("is_ready", isReady))
	}

	// Update account if needed
	if !isReady && logger != nil {
		logger.Warn("Phone number not fully ready yet",
			zap.Uint("tenant_id", tenantID),
			zap.String("phone_number_id", phoneID),
			zap.String("note", "Phone number may need verification or may be in trial mode"))
	}
}

// GetPhoneNumberStatus gets detailed status of a phone number from Meta
func GetPhoneNumberStatus(logger *zap.Logger, phoneID, accessToken string) (map[string]interface{}, error) {
	if phoneID == "" || accessToken == "" {
		return nil, fmt.Errorf("phone_number_id and access_token are required")
	}

	endpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s?fields=id,display_phone_number,verified_name,quality_rating,code_verification_status,status&access_token=%s", phoneID, accessToken)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		if logger != nil {
			logger.Warn("failed to get phone number status",
				zap.String("phone_number_id", phoneID),
				zap.Int("status", resp.StatusCode),
				zap.ByteString("body", body))
		}
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var status map[string]interface{}
	if err := json.Unmarshal(body, &status); err != nil {
		if logger != nil {
			logger.Warn("failed to parse phone number status",
				zap.String("phone_number_id", phoneID),
				zap.Error(err),
				zap.ByteString("body", body))
		}
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return status, nil
}

// subscribeAppToWABASync subscribes to WABA webhooks synchronously (immediate, not async)
func subscribeAppToWABASync(logger *zap.Logger, cfg *config.Config, tenantID uint, wabaID string) {
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
	data.Set("subscribed_fields", "messages,statuses")

	endpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/subscribed_apps", wabaID)
	resp, err := http.PostForm(endpoint, data)
	if err != nil {
		if logger != nil {
			logger.Warn("whatsapp subscription request failed", zap.Uint("tenant_id", tenantID), zap.String("waba_id", wabaID), zap.Error(err))
		}
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// Check if already subscribed
		if strings.Contains(string(body), "already") || strings.Contains(string(body), "duplicate") {
			if logger != nil {
				logger.Info("WhatsApp WABA already subscribed", zap.Uint("tenant_id", tenantID), zap.String("waba_id", wabaID))
			}
			return
		}

		if logger != nil {
			logger.Warn("whatsapp subscription returned non-200",
				zap.Uint("tenant_id", tenantID),
				zap.String("waba_id", wabaID),
				zap.Int("status", resp.StatusCode),
				zap.ByteString("body", body))
		}
		return
	}

	if logger != nil {
		logger.Info("✅ WhatsApp WABA subscribed (synchronous)", zap.Uint("tenant_id", tenantID), zap.String("waba_id", wabaID))
	}
}

func subscribeAppToWABA(logger *zap.Logger, cfg *config.Config, tenantID uint, wabaID string) {
	subscribeAppToWABASync(logger, cfg, tenantID, wabaID)
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

	// Subscribe to phone number using system user token
	// System user token should be used for all API operations after system user is added to WABA
	if sysToken != "" {
		go subscribePhoneNumber(logger, phoneID, sysToken)
	} else if tenantToken != "" {
		// Fallback to tenant token if system token not available (shouldn't happen)
		logger.Warn("using tenant token for phone subscription (system token preferred)",
			zap.Uint("tenant_id", tenantID))
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

// addSystemUserToWABA adds Brancr's system user to the tenant's WABA.
// This is REQUIRED for the system user to make API calls to manage the WABA.
// According to Meta docs: "Employee-access system users need to be added to every WABA that is created"
// POST /{waba-id}/assigned_users
func addSystemUserToWABA(logger *zap.Logger, cfg *config.Config, wabaID string, tenantID uint) {
	if cfg == nil || strings.TrimSpace(wabaID) == "" {
		return
	}

	sysToken := strings.TrimSpace(cfg.MetaSystemUserToken)
	sysUserID := strings.TrimSpace(cfg.MetaSystemUserID)

	if sysToken == "" {
		if logger != nil {
			logger.Warn("system user addition skipped: missing system user token",
				zap.Uint("tenant_id", tenantID),
				zap.String("waba_id", wabaID))
		}
		return
	}

	if sysUserID == "" {
		if logger != nil {
			logger.Warn("system user addition skipped: missing system user ID",
				zap.Uint("tenant_id", tenantID),
				zap.String("waba_id", wabaID),
				zap.String("note", "Set META_SYSTEM_USER_ID environment variable"))
		}
		return
	}

	// Add system user to WABA with MANAGE task permissions
	// POST /{waba-id}/assigned_users
	body := map[string]interface{}{
		"user":  sysUserID,
		"tasks": []string{"MANAGE"}, // MANAGE allows full WABA management
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		if logger != nil {
			logger.Warn("system user addition failed: marshal error",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
		}
		return
	}

	endpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/assigned_users?access_token=%s", wabaID, sysToken)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		if logger != nil {
			logger.Warn("system user addition failed: request build error",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
		}
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if logger != nil {
			logger.Warn("system user addition failed: request error",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
		}
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// Check if user is already assigned (common case)
		if strings.Contains(string(respBody), "already assigned") || strings.Contains(string(respBody), "already exists") {
			if logger != nil {
				logger.Info("system user already added to WABA",
					zap.Uint("tenant_id", tenantID),
					zap.String("waba_id", wabaID))
			}
			return
		}

		if logger != nil {
			logger.Warn("system user addition failed: API error",
				zap.Uint("tenant_id", tenantID),
				zap.String("waba_id", wabaID),
				zap.Int("status", resp.StatusCode),
				zap.ByteString("body", respBody))
		}
		return
	}

	if logger != nil {
		logger.Info("✅ System user added to WABA successfully",
			zap.Uint("tenant_id", tenantID),
			zap.String("waba_id", wabaID),
			zap.String("system_user_id", sysUserID),
			zap.String("note", "System user can now make API calls to manage this WABA"))
	}
}

// setupCreditSharingSync sets up credit sharing synchronously (immediately, not async).
// This is called immediately after Embedded Signup callback to ensure Meta knows Brancr handles billing.
func setupCreditSharingSync(logger *zap.Logger, cfg *config.Config, tenantBusinessID, wabaID string, tenantID uint) {
	setupCreditSharing(logger, cfg, tenantBusinessID, wabaID, tenantID)
}

// setupCreditSharing sets up credit sharing for Solution Partner billing model.
// This allows Brancr to bill tenants instead of Meta billing them directly.
// According to Meta docs: "Solution Partner as the credit line owner is the 'Bill To Party'"
// IMPORTANT: This should be set up IMMEDIATELY AFTER Embedded Signup completes
// to tell Meta that Brancr handles billing, so tenants don't need to add payment method.
// POST /{extended_credit_id}/whatsapp_credit_sharing_and_attach
func setupCreditSharing(logger *zap.Logger, cfg *config.Config, tenantBusinessID, wabaID string, tenantID uint) {
	if cfg == nil || strings.TrimSpace(tenantBusinessID) == "" || strings.TrimSpace(wabaID) == "" {
		return
	}

	creditLineID := strings.TrimSpace(cfg.MetaExtendedCreditID)
	if creditLineID == "" {
		if logger != nil {
			logger.Info("credit sharing skipped: extended credit ID not configured",
				zap.Uint("tenant_id", tenantID),
				zap.String("waba_id", wabaID),
				zap.String("note", "Set META_EXTENDED_CREDIT_ID to enable Solution Partner billing. Tenants will be billed directly by Meta without this."))
		}
		return
	}

	sysToken := strings.TrimSpace(cfg.MetaSystemUserToken)
	if sysToken == "" {
		if logger != nil {
			logger.Warn("credit sharing skipped: missing system user token",
				zap.Uint("tenant_id", tenantID),
				zap.String("waba_id", wabaID))
		}
		return
	}

	// Set up credit sharing: share Brancr's credit line with tenant's business
	// POST /{extended_credit_id}/whatsapp_credit_sharing_and_attach
	body := map[string]interface{}{
		"receiving_business_id": tenantBusinessID,
		"waba_id":               wabaID,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		if logger != nil {
			logger.Warn("credit sharing setup failed: marshal error",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
		}
		return
	}

	endpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/whatsapp_credit_sharing_and_attach?access_token=%s", creditLineID, sysToken)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		if logger != nil {
			logger.Warn("credit sharing setup failed: request build error",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
		}
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if logger != nil {
			logger.Warn("credit sharing setup failed: request error",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
		}
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// Check if credit sharing already exists
		if strings.Contains(string(respBody), "already") || strings.Contains(string(respBody), "duplicate") {
			if logger != nil {
				logger.Info("credit sharing already configured for WABA",
					zap.Uint("tenant_id", tenantID),
					zap.String("waba_id", wabaID))
			}
			return
		}

		if logger != nil {
			logger.Warn("credit sharing setup failed: API error",
				zap.Uint("tenant_id", tenantID),
				zap.String("waba_id", wabaID),
				zap.String("tenant_business_id", tenantBusinessID),
				zap.String("credit_line_id", creditLineID),
				zap.Int("status", resp.StatusCode),
				zap.ByteString("body", respBody),
				zap.String("note", "This requires business_management permission and proper credit line setup. Tenant may be billed directly by Meta if this fails."))
		}
		return
	}

	var response struct {
		ID string `json:"id"` // Allocation config ID
	}

	if err := json.Unmarshal(respBody, &response); err == nil && response.ID != "" {
		if logger != nil {
			logger.Info("✅ Credit sharing configured successfully",
				zap.Uint("tenant_id", tenantID),
				zap.String("waba_id", wabaID),
				zap.String("allocation_config_id", response.ID),
				zap.String("note", "Brancr will now receive aggregated invoice from Meta. Tenant usage will be billed through Brancr."))
		}
	} else {
		if logger != nil {
			logger.Info("✅ Credit sharing configured successfully",
				zap.Uint("tenant_id", tenantID),
				zap.String("waba_id", wabaID),
				zap.String("note", "Brancr will now receive aggregated invoice from Meta. Tenant usage will be billed through Brancr."))
		}
	}
}

// GetExtendedCreditID fetches the Extended Credit Line ID for your Business Manager.
// This is useful for finding your credit line ID if you don't know it.
// Call this with your business ID and system user token.
// Returns the first extended credit ID found, or empty string if none found.
func GetExtendedCreditID(logger *zap.Logger, businessID, systemUserToken string) (string, error) {
	if businessID == "" || systemUserToken == "" {
		return "", fmt.Errorf("business_id and system_user_token are required")
	}

	endpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/extendedcredits?fields=id,legal_entity_name&access_token=%s", businessID, systemUserToken)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		if logger != nil {
			logger.Error("failed to fetch extended credits",
				zap.Int("status", resp.StatusCode),
				zap.ByteString("body", body))
		}
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Data []struct {
			ID             string `json:"id"`
			LegalEntityName string `json:"legal_entity_name"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if response.Error != nil {
		return "", fmt.Errorf("API error: %s (code %d)", response.Error.Message, response.Error.Code)
	}

	if len(response.Data) == 0 {
		if logger != nil {
			logger.Warn("no extended credits found for business",
				zap.String("business_id", businessID),
				zap.String("note", "You may need to create an Extended Credit account in Meta Business Manager > Billing"))
		}
		return "", fmt.Errorf("no extended credits found - create one in Meta Business Manager > Billing > Extended Credits")
	}

	creditID := response.Data[0].ID
	if logger != nil {
		logger.Info("found extended credit ID",
			zap.String("credit_id", creditID),
			zap.String("legal_entity_name", response.Data[0].LegalEntityName))
	}

	return creditID, nil
}

// GetBusinessIDFromWABA gets the Business ID that owns a WABA.
// This is useful when you only have WABA ID and need to find the Business ID.
func GetBusinessIDFromWABA(logger *zap.Logger, wabaID, accessToken string) (string, error) {
	if wabaID == "" || accessToken == "" {
		return "", fmt.Errorf("waba_id and access_token are required")
	}

	// Get WABA details including owner_business_info
	endpoint := fmt.Sprintf("https://graph.facebook.com/v20.0/%s?fields=owner_business_info&access_token=%s", wabaID, accessToken)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		if logger != nil {
			logger.Error("failed to fetch WABA business info",
				zap.Int("status", resp.StatusCode),
				zap.ByteString("body", body))
		}
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		OwnerBusinessInfo struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"owner_business_info"`
		Error *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if response.Error != nil {
		return "", fmt.Errorf("API error: %s (code %d)", response.Error.Message, response.Error.Code)
	}

	if response.OwnerBusinessInfo.ID == "" {
		return "", fmt.Errorf("business ID not found in WABA response")
	}

	if logger != nil {
		logger.Info("found business ID from WABA",
			zap.String("waba_id", wabaID),
			zap.String("business_id", response.OwnerBusinessInfo.ID),
			zap.String("business_name", response.OwnerBusinessInfo.Name))
	}

	return response.OwnerBusinessInfo.ID, nil
}
