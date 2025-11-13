package social

import (
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

// SaveWhatsAppAccount persists or updates a tenant's WhatsApp social account and
// triggers the required Meta subscriptions.
func SaveWhatsAppAccount(db *gorm.DB, logger *zap.Logger, cfg *config.Config, tenantID uint, wabaID, phoneID, phoneNumber, token, status string) error {
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

	if token != "" {
		go subscribePhoneNumber(logger, phoneID, token)
	}
	if cfg != nil {
		go subscribeAppToWABA(logger, cfg, tenantID, wabaID)
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
