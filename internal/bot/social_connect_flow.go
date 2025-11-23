package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/social"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
)

func sendPlain(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	bot.Send(msg)
}

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
	case "instagram":
		if m.sendMetaOAuthLink(bot, tenantID, chatID, platform, []string{"instagram"}) {
			return
		}
		sendPlain(bot, chatID, fmt.Sprintf("Meta OAuth is temporarily unavailable, so I'll fall back to manual setup. Please send a current %s page token from the Meta Graph API.", strings.Title(platform)))
		tenantsession := &PostSession{Step: 99, Name: platform}
		tenantSessions[chatID] = tenantsession

	case "facebook":
		if m.sendMetaOAuthLink(bot, tenantID, chatID, platform, []string{"facebook"}) {
			return
		}
		sendPlain(bot, chatID, "Meta OAuth is temporarily unavailable. Please paste the latest Facebook page access token to connect.")
		tenantsession := &PostSession{Step: 99, Name: platform}
		tenantSessions[chatID] = tenantsession

	case "tiktok":
		if m.sendTikTokOAuthLink(bot, tenantID, chatID) {
			return
		}
		sendPlain(bot, chatID, "I couldn’t generate the TikTok OAuth link just now. Please try again in a minute or contact support.")

	case "youtube":
		sendPlain(bot, chatID, "Please send your YouTube API key or OAuth access token to connect the channel.")
		tenantsession := &PostSession{Step: 99, Name: platform}
		tenantSessions[chatID] = tenantsession

	case "whatsapp":
		// New simplified WhatsApp connection via Respond.io/Gupshup
		// Direct tenants to the dashboard to connect via the new flow
		baseURL, err := utils.GetPublicURL()
		if err != nil {
			baseURL = "https://www.brancr.com"
		}
		baseURL = strings.TrimRight(baseURL, "/")
		
		connectURL := fmt.Sprintf("%s/app/integrations/whatsapp", baseURL)
		
		info := "📱 *Connect WhatsApp*\n\n" +
			"Brancr now uses *Respond.io* and *Gupshup* for WhatsApp integration.\n\n" +
			"To connect your WhatsApp number:\n" +
			"1️⃣ Open the dashboard link below\n" +
			"2️⃣ Enter your phone number\n" +
			"3️⃣ Follow the verification steps\n\n" +
			"*Note:* The connection process is handled through our dashboard for security."

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonURL("🔗 Open Dashboard", connectURL),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("⬅️ Back to menu", "menu:main"),
			),
		)

		msg := tgbotapi.NewMessage(chatID, info)
		msg.ParseMode = "Markdown"
		msg.DisableWebPagePreview = true
		msg.ReplyMarkup = keyboard
		bot.Send(msg)
		return

	default:
		sendPlain(bot, chatID, "I can connect Instagram, Facebook, WhatsApp, TikTok, and YouTube right now. Let me know which one you’d like to set up.")
		return
	}
}

func (m *TenantBotManager) sendMetaOAuthLink(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, platform string, platforms []string) bool {
	oauthURL, err := m.buildMetaOAuthURL(tenantID, platforms)
	if err != nil {
		if m.Logger != nil {
			m.Logger.Warn("meta oauth link unavailable", zap.Error(err))
		}
		sendPlain(bot, chatID, "I couldn’t produce a Meta OAuth link. Please confirm that Brancr is reachable on the public URL configured in the dashboard and try again.")
		return false
	}

	connectMsg := fmt.Sprintf(
		"Let’s connect your *%s* account.\n\n1️⃣ Open the secure link.\n2️⃣ Approve Brancr in the Meta consent screen.\n3️⃣ Come back and tap *Verify* so I can double-check the connection.",
		strings.Title(platform),
	)

	buttonLabel := fmt.Sprintf("Open %s OAuth", strings.Title(platform))
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL(buttonLabel, oauthURL),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✔️ Verify now", fmt.Sprintf("verify:%s", platform)),
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Back to menu", "menu:main"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, connectMsg)
	msg.ParseMode = "Markdown"
	msg.DisableWebPagePreview = true
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
	return true
}

func (m *TenantBotManager) buildMetaOAuthURL(tenantID uint, platforms []string) (string, error) {
	baseURL, err := utils.GetPublicURL()
	if err != nil {
		return "", err
	}

	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return "", fmt.Errorf("public URL not configured")
	}

	params := url.Values{}
	params.Set("tenant_id", fmt.Sprintf("%d", tenantID))

	if len(platforms) > 0 {
		params.Set("platforms", strings.Join(platforms, ","))
		params.Set("platform", platforms[0])
	}

	if m.Config != nil && m.Config.MetaSuccessURL != "" {
		params.Set("success_redirect", m.Config.MetaSuccessURL)
	}

	return fmt.Sprintf("%s/api/oauth/meta/start?%s", baseURL, params.Encode()), nil
}

func (m *TenantBotManager) sendTikTokOAuthLink(bot *tgbotapi.BotAPI, tenantID uint, chatID int64) bool {
	oauthURL, err := m.buildTikTokOAuthURL(tenantID)
	if err != nil {
		if m.Logger != nil {
			m.Logger.Warn("tiktok oauth link unavailable", zap.Error(err))
		}
		return false
	}

	text := "Connect TikTok so Brancr can schedule and publish videos on your behalf.\n\n1️⃣ Open the secure TikTok link.\n2️⃣ Approve Brancr in the consent screen.\n3️⃣ Return and tap *Verify TikTok* so I can confirm the connection."

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("Open TikTok OAuth", oauthURL),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✔️ Verify TikTok", "verify:tiktok"),
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Back to menu", "menu:main"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.DisableWebPagePreview = true
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
	return true
}

func (m *TenantBotManager) buildTikTokOAuthURL(tenantID uint) (string, error) {
	baseURL, err := utils.GetPublicURL()
	if err != nil {
		return "", err
	}

	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return "", fmt.Errorf("public URL not configured")
	}

	params := url.Values{}
	params.Set("tenant_id", fmt.Sprintf("%d", tenantID))
	if m.Config != nil && m.Config.MetaSuccessURL != "" {
		params.Set("success_redirect", m.Config.MetaSuccessURL)
	}

	return fmt.Sprintf("%s/api/oauth/tiktok/start?%s", baseURL, params.Encode()), nil
}

func (m *TenantBotManager) sendMetaEmbeddedSignup(bot *tgbotapi.BotAPI, tenantID uint, chatID int64) bool {
	baseURL, err := utils.GetPublicURL()
	if err != nil {
		return false
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return false
	}

	params := url.Values{}
	params.Set("tenant_id", fmt.Sprintf("%d", tenantID))
	if m.Config != nil && m.Config.MetaSuccessURL != "" {
		params.Set("success_redirect", m.Config.MetaSuccessURL)
	}

	startURL := fmt.Sprintf("%s/api/oauth/meta/whatsapp/start?%s", baseURL, params.Encode())

	info := "Meta handles WhatsApp onboarding in a separate window. Open the signup portal, finish the steps, then tap *Verify* so I can confirm the connection."

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("Open WhatsApp Signup", startURL),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✔️ Verify WhatsApp", "verify:whatsapp"),
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Back to menu", "menu:main"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, info)
	msg.ParseMode = "Markdown"
	msg.DisableWebPagePreview = true
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
	return true
}

// 🚀 ContinueSocialConnect
func (m *TenantBotManager) ContinueSocialConnect(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, text string) {
	sess, ok := tenantSessions[chatID]
	if !ok || sess.Step != 99 {
		return
	}

	platform := sess.Name
	ctx := context.Background()

	if platform == "instagram" || platform == "facebook" || platform == "whatsapp" {
		notice := fmt.Sprintf("The %s integration now uses Meta OAuth. Run 'connect %s' to generate a fresh secure link.", platform, platform)
		sendPlain(bot, chatID, notice)
		delete(tenantSessions, chatID)
		return
	}

	sendPlain(bot, chatID, "Verifying the token you sent. One moment...")

	acc, err := social.AuthorizeManualToken(ctx, platform, text)
	if err != nil {
		m.Logger.Warn("manual token validation failed", zap.String("platform", platform), zap.Error(err))
		sendPlain(bot, chatID, fmt.Sprintf("I couldn’t verify your %s token. Please confirm it’s still active, includes the required permissions, and hasn’t expired, then try again.", strings.Title(platform)))
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
