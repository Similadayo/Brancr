package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
)

// 🚀 StartSocialConnectFlow
func (m *TenantBotManager) StartSocialConnectFlow(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, platform string) {
	if m.Logger == nil {
		l, _ := zap.NewDevelopment()
		m.Logger = l
	}

	platform = strings.TrimSpace(strings.ToLower(platform))
	if platform == "" {
		bot.Send(tgbotapi.NewMessage(chatID, "Please specify a platform: Instagram, Facebook, WhatsApp, TikTok, or YouTube."))
		return
	}

	switch platform {
	case "instagram", "facebook":
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🔗 Send your %s access token (from Meta Graph API).", strings.Title(platform))))
		tenantSessions[chatID] = &PostSession{Step: 99, Name: platform}

	case "tiktok":
		bot.Send(tgbotapi.NewMessage(chatID, "🎵 Send your TikTok Business Access Token."))
		tenantSessions[chatID] = &PostSession{Step: 99, Name: platform}

	case "youtube":
		bot.Send(tgbotapi.NewMessage(chatID, "📺 Send your YouTube API key or OAuth token."))
		tenantSessions[chatID] = &PostSession{Step: 99, Name: platform}

	case "whatsapp":
		// ✅ WhatsApp now uses Twilio sandbox verification — no user input needed
		bot.Send(tgbotapi.NewMessage(chatID, "📞 Connecting to WhatsApp (Twilio Sandbox Mode)... please wait."))

		ctx := context.Background()
		acc, err := social.AuthorizeManualToken(ctx, "whatsapp", "")
		if err != nil {
			m.Logger.Warn("whatsapp sandbox verification failed", zap.Error(err))
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to connect WhatsApp at this time. Please ensure Twilio is properly configured and try again later."))
			return
		}

		acc.TenantID = tenantID
		acc.Connected = true
		acc.Mode = "sandbox"

		if err := m.DB.Create(&acc).Error; err != nil {
			m.Logger.Error("failed to save WhatsApp sandbox account", zap.Error(err))
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to save your WhatsApp connection. Please try again later or contact support if the issue persists."))
			return
		}

		// Professional WhatsApp connection confirmation
		confirmMsg := "✅ *WhatsApp Account Connected*\n\n📱 Your WhatsApp account is now connected via Twilio Sandbox and ready to use.\n\nYou can now:\n• Send messages to WhatsApp\n• Receive customer messages\n• Manage WhatsApp conversations\n\nTo verify the connection, use: `verify whatsapp`"
		msg := tgbotapi.NewMessage(chatID, confirmMsg)
		msg.ParseMode = "Markdown"
		bot.Send(msg)
		return

	default:
		bot.Send(tgbotapi.NewMessage(chatID, "Unsupported platform."))
		return
	}
}

// 🚀 ContinueSocialConnect
func (m *TenantBotManager) ContinueSocialConnect(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, text string) {
	sess, ok := tenantSessions[chatID]
	if !ok || sess.Step != 99 {
		return
	}

	platform := sess.Name
	ctx := context.Background()

	bot.Send(tgbotapi.NewMessage(chatID, "🔍 Verifying your token... please wait."))

	acc, err := social.AuthorizeManualToken(ctx, platform, text)
	if err != nil {
		m.Logger.Warn("manual token validation failed", zap.String("platform", platform), zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Unable to verify your %s access token. Please ensure:\n\n• The token is valid and not expired\n• You have the correct permissions\n• The token format is correct\n\nPlease try again with a new token.", strings.Title(platform))))
		delete(tenantSessions, chatID)
		return
	}

	encrypted, _ := utils.EncryptToken(text)
	acc.AccessToken = encrypted
	acc.TenantID = tenantID
	acc.Connected = true
	acc.Mode = "manual"

	// For Instagram, fetch and save the Instagram Business Account ID (needed for webhook routing)
	if platform == "instagram" {
		igID, err := m.fetchInstagramBusinessAccountID(ctx, text)
		if err != nil {
			m.Logger.Warn("failed to fetch Instagram Business Account ID", zap.Error(err))
			// Continue anyway - might be saved later
		} else {
			acc.ExternalID = igID
			acc.PageID = igID // Also save to PageID for backward compatibility
			m.Logger.Info("✅ Instagram Business Account ID fetched",
				zap.String("instagram_id", igID),
				zap.Uint("tenant_id", tenantID))
		}
	}

	// For Facebook, fetch and save the Page ID
	if platform == "facebook" {
		pageID, err := m.fetchFacebookPageID(ctx, text)
		if err != nil {
			m.Logger.Warn("failed to fetch Facebook Page ID", zap.Error(err))
		} else {
			acc.ExternalID = pageID
			acc.PageID = pageID
			m.Logger.Info("✅ Facebook Page ID fetched",
				zap.String("page_id", pageID),
				zap.Uint("tenant_id", tenantID))
		}
	}

	if err := m.DB.Create(&acc).Error; err != nil {
		m.Logger.Error("failed to create social account", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Unable to save your account connection. Please try again later or contact support if the issue persists."))
		delete(tenantSessions, chatID)
		return
	}

	// Get account details for confirmation message
	var accountName string
	if acc.ExternalID != "" {
		accountName = acc.ExternalID
	} else if acc.PageID != "" {
		accountName = acc.PageID
	}

	// Professional connection confirmation message
	var confirmMsg string
	if accountName != "" {
		confirmMsg = fmt.Sprintf("✅ *%s Account Connected*\n\n📱 Your %s account is now connected and ready to use.\n\nAccount ID: `%s`\n\nYou can now:\n• Schedule posts to %s\n• Receive messages from %s\n• Manage your social media presence\n\nTo verify the connection, use: `verify %s`", 
			strings.Title(platform), strings.Title(platform), accountName, strings.Title(platform), strings.Title(platform), platform)
	} else {
		confirmMsg = fmt.Sprintf("✅ *%s Account Connected*\n\n📱 Your %s account is now connected and ready to use.\n\nYou can now:\n• Schedule posts to %s\n• Receive messages from %s\n• Manage your social media presence\n\nTo verify the connection, use: `verify %s`", 
			strings.Title(platform), strings.Title(platform), strings.Title(platform), strings.Title(platform), platform)
	}

	msg := tgbotapi.NewMessage(chatID, confirmMsg)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
	delete(tenantSessions, chatID)
}

// fetchInstagramBusinessAccountID fetches the Instagram Business Account ID from Meta Graph API
func (m *TenantBotManager) fetchInstagramBusinessAccountID(ctx context.Context, accessToken string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Step 1: Get Facebook pages
	var pageID string
	
	// Try /me/accounts first (works for User tokens)
	pagesURL := fmt.Sprintf("https://graph.facebook.com/v18.0/me/accounts?access_token=%s", accessToken)
	resp, err := client.Get(pagesURL)
	if err != nil {
		return "", fmt.Errorf("failed to query pages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		m.Logger.Warn("⚠️ /me/accounts failed, trying /me (might be Page token)",
			zap.Int("status", resp.StatusCode),
			zap.String("response_preview", truncateString(string(body), 200)))
		
		// If /me/accounts fails, try /me - might be a Page Access Token
		meURL := fmt.Sprintf("https://graph.facebook.com/v18.0/me?access_token=%s", accessToken)
		meResp, err := client.Get(meURL)
		if err != nil {
			m.Logger.Error("❌ Failed to verify token",
				zap.Error(err),
				zap.String("response", string(body)))
			return "", fmt.Errorf("failed to fetch pages (status %d): %s. Make sure your token has 'pages_show_list' permission and is not expired.", resp.StatusCode, string(body))
		}
		defer meResp.Body.Close()
		
		if meResp.StatusCode == 200 {
			var meData map[string]interface{}
			if err := json.NewDecoder(meResp.Body).Decode(&meData); err == nil {
				if id, ok := meData["id"].(string); ok && id != "" {
					pageID = id
					m.Logger.Info("✅ Detected Page Access Token", zap.String("page_id", pageID))
				} else {
					return "", fmt.Errorf("failed to get page ID from token response")
				}
			} else {
				return "", fmt.Errorf("failed to parse token response: %w", err)
			}
		} else {
			meBody, _ := io.ReadAll(meResp.Body)
			return "", fmt.Errorf("failed to verify token (status %d): %s", meResp.StatusCode, string(meBody))
		}
	} else {
		var pages struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
			return "", fmt.Errorf("failed to parse pages: %w", err)
		}

		if len(pages.Data) == 0 {
			return "", fmt.Errorf("no pages found")
		}

		pageID = pages.Data[0].ID
	}
	
	if pageID == "" {
		return "", fmt.Errorf("could not determine page ID from token")
	}

	// Step 2: Get Instagram Business Account linked to the page
	igURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=instagram_business_account{id}&access_token=%s", pageID, accessToken)
	igResp, err := client.Get(igURL)
	if err != nil {
		return "", fmt.Errorf("failed to get Instagram account: %w", err)
	}
	defer igResp.Body.Close()

	if igResp.StatusCode != 200 {
		body, _ := io.ReadAll(igResp.Body)
		m.Logger.Error("❌ Failed to fetch Instagram Business Account",
			zap.Int("status", igResp.StatusCode),
			zap.String("response", string(body)))
		return "", fmt.Errorf("failed to fetch Instagram account (status %d): %s", igResp.StatusCode, string(body))
	}

	var pageData struct {
		InstagramBusinessAccount struct {
			ID string `json:"id"`
		} `json:"instagram_business_account"`
	}
	if err := json.NewDecoder(igResp.Body).Decode(&pageData); err != nil {
		return "", fmt.Errorf("failed to parse Instagram account: %w", err)
	}

	if pageData.InstagramBusinessAccount.ID == "" {
		return "", fmt.Errorf("no Instagram Business Account linked to this page")
	}

	return pageData.InstagramBusinessAccount.ID, nil
}

// fetchFacebookPageID fetches the Facebook Page ID
func (m *TenantBotManager) fetchFacebookPageID(ctx context.Context, accessToken string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Get pages
	pagesURL := fmt.Sprintf("https://graph.facebook.com/v18.0/me/accounts?access_token=%s", accessToken)
	resp, err := client.Get(pagesURL)
	if err != nil {
		return "", fmt.Errorf("failed to query pages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to fetch pages (status %d)", resp.StatusCode)
	}

	var pages struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
		return "", fmt.Errorf("failed to parse pages: %w", err)
	}

	if len(pages.Data) == 0 {
		return "", fmt.Errorf("no pages found")
	}

	return pages.Data[0].ID, nil
}

// truncateString truncates a string to max length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
