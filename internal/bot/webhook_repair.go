package bot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

func (m *TenantBotManager) EnsureWebhook(logger *zap.Logger, bot *tgbotapi.BotAPI, webhookURL string) bool {
	for attempt := 1; attempt <= 5; attempt++ {
		// Step 1: Set webhook
		cfg, _ := tgbotapi.NewWebhook(webhookURL)
		_, err := bot.Request(cfg)
		if err != nil {
			logger.Warn("setWebhook failed", zap.Int("attempt", attempt), zap.Error(err))
			time.Sleep(time.Duration(attempt*2) * time.Second)
			continue
		}

		// Step 2: Confirm registration
		resp, err := http.Get(fmt.Sprintf("https://api.telegram.org/bot%s/getWebhookInfo", bot.Token))
		if err != nil {
			logger.Warn("getWebhookInfo failed", zap.Int("attempt", attempt), zap.Error(err))
			time.Sleep(time.Duration(attempt*2) * time.Second)
			continue
		}
		defer resp.Body.Close()

		var result struct {
			OK     bool `json:"ok"`
			Result struct {
				URL string `json:"url"`
			} `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			logger.Warn("decode webhookInfo failed", zap.Error(err))
			continue
		}

		if result.OK && result.Result.URL == webhookURL {
			logger.Info("✅ Webhook confirmed by Telegram", zap.String("url", webhookURL))
			return true
		}

		logger.Warn("Webhook not yet active", zap.String("url", webhookURL))
		time.Sleep(time.Duration(attempt*2) * time.Second)
	}

	logger.Error("❌ Webhook setup failed after retries", zap.String("url", webhookURL))
	return false
}
