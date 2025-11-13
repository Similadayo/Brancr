package social

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

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

	endpoint := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/subscribed_apps?access_token=%s", phoneID, tenantToken)

	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		if logger != nil {
			logger.Warn("whatsapp phone subscription build failed", zap.String("phone_number_id", phoneID), zap.Error(err))
		}
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if logger != nil {
			logger.Warn("whatsapp phone subscription request failed", zap.String("phone_number_id", phoneID), zap.Error(err))
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
		logger.Info("WhatsApp phone number subscribed",
			zap.String("phone_number_id", phoneID),
			zap.ByteString("response", body))
	}
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
