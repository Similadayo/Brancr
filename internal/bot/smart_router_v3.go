package bot

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
)

// ============================================================
// SMART ROUTER V3 - AI Intent Detection & Dynamic Home Screen
// ============================================================
// Design Principles:
// 1. No main menu by default - respond to what user sends
// 2. Dynamic "home screen" when user says "menu" or first message of day
// 3. AI intent detection for natural language commands
// 4. Context-aware routing based on session state
// ============================================================

// IntentType represents detected user intent
type IntentType string

const (
	IntentPost       IntentType = "post"
	IntentSchedule   IntentType = "schedule"
	IntentAnalytics  IntentType = "analytics"
	IntentConnect    IntentType = "connect"
	IntentHelp       IntentType = "help"
	IntentSettings   IntentType = "settings"
	IntentOrders     IntentType = "orders"
	IntentEscalation IntentType = "escalation"
	IntentMenu       IntentType = "menu"
	IntentUnknown    IntentType = "unknown"
)

// IntentPatterns for lightweight intent detection
var intentPatterns = map[IntentType][]string{
	IntentPost:       {"post", "share", "upload", "put", "publish"},
	IntentSchedule:   {"schedule", "scheduled", "calendar", "upcoming", "posts today", "what's scheduled"},
	IntentAnalytics:  {"analytics", "stats", "statistics", "how am i doing", "performance", "insights", "report"},
	IntentConnect:    {"connect", "link", "add account", "instagram", "facebook", "tiktok"},
	IntentHelp:       {"help", "how do i", "what can you", "guide"},
	IntentSettings:   {"settings", "configure", "change", "update hours", "update tone", "add faq"},
	IntentOrders:     {"orders", "order", "sales", "today's orders"},
	IntentEscalation: {"messages", "pending", "escalation", "waiting", "inbox", "unread"},
	IntentMenu:       {"menu", "home", "start", "hi", "hello", "hey"},
}

// DetectIntent uses lightweight pattern matching to detect user intent
func DetectIntent(text string) IntentType {
	lower := strings.ToLower(strings.TrimSpace(text))

	// Check for order status command first (special case)
	orderPattern := regexp.MustCompile(`order\s+#?\d+\s+(shipped|delivered|cancelled|processing|paid)`)
	if orderPattern.MatchString(lower) {
		return IntentOrders
	}

	for intent, patterns := range intentPatterns {
		for _, pattern := range patterns {
			if strings.Contains(lower, pattern) {
				return intent
			}
		}
	}

	return IntentUnknown
}

// ============================================================
// SMART HOME SCREEN
// ============================================================

// SmartHomeData holds dynamic content for home screen
type SmartHomeData struct {
	PendingEscalations int
	ScheduledToday     int
	OrdersToday        int
	UnreadCount        int
	HasActivity        bool
	Greeting           string
}

// GetSmartHomeData gathers current stats for the home screen
func (m *TenantBotManager) GetSmartHomeData(tenantID uuid.UUID) SmartHomeData {
	data := SmartHomeData{}

	// Get current time info
	now := time.Now()
	hour := now.Hour()

	// Set greeting based on time
	switch {
	case hour < 12:
		data.Greeting = "Good morning! ☀️"
	case hour < 17:
		data.Greeting = "Good afternoon! 👋"
	default:
		data.Greeting = "Good evening! 🌙"
	}

	// Count pending escalations
	var escalationCount int64
	m.DB.Model(&storage.PendingEscalation{}).Where("tenant_id = ?", tenantID).Count(&escalationCount)
	data.PendingEscalations = int(escalationCount)

	// Count scheduled posts for today
	today := time.Now().Truncate(24 * time.Hour)
	tomorrow := today.Add(24 * time.Hour)
	var scheduledCount int64
	m.DB.Model(&storage.ScheduledPost{}).
		Where("tenant_id = ? AND scheduled_at BETWEEN ? AND ? AND status = ?", tenantID, today, tomorrow, "pending").
		Count(&scheduledCount)
	data.ScheduledToday = int(scheduledCount)

	// Count orders today
	var orderCount int64
	m.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND created_at BETWEEN ? AND ?", tenantID, today, tomorrow).
		Count(&orderCount)
	data.OrdersToday = int(orderCount)

	data.HasActivity = data.PendingEscalations > 0 || data.ScheduledToday > 0 || data.OrdersToday > 0

	return data
}

// SendSmartHomeScreen sends a dynamic home screen based on current state
func (m *TenantBotManager) SendSmartHomeScreen(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	data := m.GetSmartHomeData(tenantID)

	var lines []string
	lines = append(lines, data.Greeting)
	lines = append(lines, "")

	// Only show what needs attention
	if data.PendingEscalations > 0 {
		lines = append(lines, fmt.Sprintf("📬 %d messages waiting", data.PendingEscalations))
	}
	if data.ScheduledToday > 0 {
		lines = append(lines, fmt.Sprintf("📸 %d posts today", data.ScheduledToday))
	}
	if data.OrdersToday > 0 {
		lines = append(lines, fmt.Sprintf("📦 %d orders today", data.OrdersToday))
	}

	if !data.HasActivity {
		lines = append(lines, "All caught up! 🎉")
	}

	lines = append(lines, "")
	lines = append(lines, "Send a photo to post, or tell me what you need.")

	msg := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))

	// Smart button selection - only show what's relevant (max 2)
	var buttons []tgbotapi.InlineKeyboardButton
	if data.PendingEscalations > 0 {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("📬 Messages", "v3home:messages"))
	}
	if data.ScheduledToday > 0 {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("📅 Calendar", "v3home:calendar"))
	}
	if len(buttons) == 0 {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("📊 Stats", "v3home:stats"))
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("⚙️ Settings", "v3home:settings"))
	}

	// Max 2 buttons
	if len(buttons) > 2 {
		buttons = buttons[:2]
	}

	if len(buttons) > 0 {
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(buttons...),
		)
	}

	bot.Send(msg)
}

// ============================================================
// SMART ROUTER - Main Entry Point
// ============================================================

// RouteMessageV3 intelligently routes text messages based on intent and context
// Returns true if message was handled, false if should fall through to other handlers
func (m *TenantBotManager) RouteMessageV3(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, text string) bool {
	ctx := context.Background()

	// Check for active posting session first
	if m.ContinuePostFlowV3(bot, chatID, text) {
		return true
	}

	// Check for order status command
	if m.HandleOrderStatusCommand(bot, tenantID, chatID, text) {
		return true
	}

	// Detect intent
	intent := DetectIntent(text)

	switch intent {
	case IntentMenu:
		m.SendSmartHomeScreen(bot, chatID, tenantID)
		return true

	case IntentSchedule:
		m.showCalendarPreview(bot, chatID)
		return true

	case IntentAnalytics:
		m.sendQuickAnalytics(bot, chatID, tenantID)
		return true

	case IntentConnect:
		m.sendConnectOptions(bot, chatID, tenantID)
		return true

	case IntentHelp:
		m.sendHelpV3(bot, chatID)
		return true

	case IntentEscalation:
		m.showPendingEscalations(bot, chatID, tenantID)
		return true

	case IntentOrders:
		m.HandleOrdersToday(bot, tenantID, chatID)
		return true

	case IntentSettings:
		m.sendSettingsV3(bot, chatID)
		return true

	case IntentPost:
		// User wants to post but didn't send media
		msg := tgbotapi.NewMessage(chatID, "Send me a photo or video to post! 📸")
		bot.Send(msg)
		return true

	case IntentUnknown:
		// Check for custom reply session (from payment ask)
		var interactionIDStr string
		if err := m.SessionManager.GetGenericSession(ctx, fmt.Sprintf("payask:%d", chatID), &interactionIDStr); err == nil {
			m.handleCustomPaymentReply(bot, tenantID, chatID, text, interactionIDStr)
			return true
		}

		// Check for custom escalation reply
		if err := m.SessionManager.GetGenericSession(ctx, fmt.Sprintf("customreply:%d", chatID), &interactionIDStr); err == nil {
			m.handleCustomEscalationReply(bot, tenantID, chatID, text, interactionIDStr)
			return true
		}

		return false // Let other handlers try
	}

	return false
}

// ============================================================
// RESPONSE HELPERS
// ============================================================

func (m *TenantBotManager) sendQuickAnalytics(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	// Get quick stats
	now := time.Now()
	weekAgo := now.AddDate(0, 0, -7)

	var postCount int64
	m.DB.Model(&storage.ScheduledPost{}).
		Where("tenant_id = ? AND created_at > ? AND status = ?", tenantID, weekAgo, "posted").
		Count(&postCount)

	var messageCount int64
	m.DB.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND created_at > ?", tenantID, weekAgo).
		Count(&messageCount)

	msg := fmt.Sprintf(
		"📊 This week:\n"+
			"• %d posts published\n"+
			"• %d messages handled\n\n"+
			"Keep it up! 🚀",
		postCount, messageCount,
	)

	bot.Send(tgbotapi.NewMessage(chatID, msg))
}

func (m *TenantBotManager) sendConnectOptions(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	// Get current connections
	var accounts []storage.SocialAccount
	m.DB.Where("tenant_id = ? AND connected = ?", tenantID, true).Find(&accounts)

	var lines []string
	lines = append(lines, "🔗 Connected accounts:")

	if len(accounts) == 0 {
		lines = append(lines, "None yet")
	} else {
		for _, acc := range accounts {
			lines = append(lines, fmt.Sprintf("• %s %s", getPlatformEmoji(acc.Platform), acc.Username))
		}
	}

	msg := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Add New", "v3home:connect"),
		),
	)
	bot.Send(msg)
}

func (m *TenantBotManager) sendHelpV3(bot *tgbotapi.BotAPI, chatID int64) {
	// Ultra-short help
	help := `📱 Quick guide:

• Send a photo → Schedule post
• "schedule" → See upcoming posts
• "stats" → Quick analytics
• "orders" → Today's orders
• "order 123 shipped" → Update status

Send me a photo to get started! 📸`

	bot.Send(tgbotapi.NewMessage(chatID, help))
}

func (m *TenantBotManager) sendSettingsV3(bot *tgbotapi.BotAPI, chatID int64) {
	settings := `⚙️ What to change?

• "update hours" → Business hours
• "add faq" → Add knowledge
• "change tone" → AI voice style

Or type what you'd like to update!`

	bot.Send(tgbotapi.NewMessage(chatID, settings))
}

func (m *TenantBotManager) showPendingEscalations(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	var escalations []storage.PendingEscalation
	m.DB.Where("tenant_id = ?", tenantID).Order("created_at DESC").Limit(5).Find(&escalations)

	if len(escalations) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "📬 No pending messages. All caught up! 🎉"))
		return
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("📬 %d messages waiting:\n", len(escalations)))

	for i, esc := range escalations {
		preview := esc.Message
		if len(preview) > 40 {
			preview = preview[:37] + "..."
		}
		lines = append(lines, fmt.Sprintf("%d. %s: \"%s\"", i+1, getPlatformEmoji(esc.Platform), preview))
	}

	msg := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Handle First", fmt.Sprintf("v3esc:first:%s", escalations[0].InteractionID)),
		),
	)
	bot.Send(msg)
}

// ============================================================
// CUSTOM REPLY HANDLERS
// ============================================================

func (m *TenantBotManager) handleCustomPaymentReply(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, text, interactionIDStr string) {
	ctx := context.Background()

	interactionID, err := uuid.Parse(interactionIDStr)
	if err != nil {
		return
	}

	var interaction storage.Interaction
	if err := m.DB.First(&interaction, interactionID).Error; err != nil {
		return
	}

	var customer storage.Customer
	m.DB.First(&customer, interaction.CustomerID)

	// Send custom message to customer
	m.sendToCustomer(ctx, tenantID, interaction.CustomerID, interaction.Platform, text)

	// Confirm and cleanup
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Sent to customer."))
	m.SessionManager.DeleteSession(ctx, fmt.Sprintf("payask:%d", chatID))
}

func (m *TenantBotManager) handleCustomEscalationReply(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, text, interactionIDStr string) {
	ctx := context.Background()

	interactionID, err := uuid.Parse(interactionIDStr)
	if err != nil {
		return
	}

	var pending storage.PendingEscalation
	if err := m.DB.Where("interaction_id = ?", interactionID).First(&pending).Error; err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Escalation expired."))
		return
	}

	var customer storage.Customer
	m.DB.First(&customer, pending.CustomerID)

	// Send custom reply
	if m.EscalationHandler != nil && m.EscalationHandler.MessageHandler != nil {
		m.EscalationHandler.MessageHandler.SendReply(ctx, tenantID, pending.Platform, "", customer.PlatformID, text)
	}

	// Cleanup
	bot.Send(tgbotapi.NewMessage(chatID, "✅ Sent!"))
	m.DB.Where("interaction_id = ?", interactionID).Delete(&storage.PendingEscalation{})
	m.SessionManager.DeleteSession(ctx, fmt.Sprintf("customreply:%d", chatID))
}

// ============================================================
// V3 HOME CALLBACK HANDLER
// ============================================================

func (m *TenantBotManager) HandleHomeV3Callback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, action string) {
	switch action {
	case "messages":
		m.showPendingEscalations(bot, chatID, tenantID)
	case "calendar":
		m.showCalendarPreview(bot, chatID)
	case "stats":
		m.sendQuickAnalytics(bot, chatID, tenantID)
	case "settings":
		m.sendSettingsV3(bot, chatID)
	case "connect":
		m.startSocialConnectFlow(bot, chatID, tenantID)
	}
}

func (m *TenantBotManager) startSocialConnectFlow(bot *tgbotapi.BotAPI, chatID int64, tenantID uuid.UUID) {
	msg := tgbotapi.NewMessage(chatID, "Which platform to connect?")

	// Use standardized keyboard
	formatter := &UXFormatter{}
	msg.ReplyMarkup = formatter.CreateConnectionKeyboard()

	bot.Send(msg)
}
