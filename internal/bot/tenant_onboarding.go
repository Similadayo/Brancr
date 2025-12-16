package bot

import (
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
)

// CheckAndStartOnboarding checks if tenant needs onboarding and starts wizard if needed
func (m *TenantBotManager) CheckAndStartOnboarding(bot *tgbotapi.BotAPI, tenantID uint, chatID int64) {
	var tenant storage.Tenant
	if err := m.DB.First(&tenant, tenantID).Error; err != nil {
		return
	}

	// First, check if industry is selected (if IndustrySelectionHandler is available)
	if m.IndustrySelectionHandler != nil {
		m.IndustrySelectionHandler.CheckAndPromptIndustrySelection(bot, tenantID, chatID)
		// If industry not selected, return early (industry selection takes priority)
		if tenant.IndustryID == 0 {
			return
		}
	}

	// Check if tenant has completed initial setup
	hasSocialAccount := false
	var accounts []storage.SocialAccount
	m.DB.Where("tenant_id = ? AND connected = ?", tenantID, true).Find(&accounts)
	if len(accounts) > 0 {
		hasSocialAccount = true
	}

	var posts []storage.ScheduledPost
	postCount := m.DB.Where("tenant_id = ?", tenantID).Find(&posts).RowsAffected

	// Determine onboarding status
	stepsCompleted := 0
	totalSteps := 3

	if tenant.OnboardingComplete {
		stepsCompleted++
	}
	if hasSocialAccount {
		stepsCompleted++
	}
	if postCount > 0 {
		stepsCompleted++
	}

	// Show onboarding wizard if not all steps are done
	if stepsCompleted < totalSteps {
		m.showOnboardingWizard(bot, tenantID, chatID, stepsCompleted, totalSteps, hasSocialAccount)
	}
}

// showOnboardingWizard displays the onboarding wizard with progress
func (m *TenantBotManager) showOnboardingWizard(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, completed, total int, hasSocialAccount bool) {
	progressBar := ""
	for i := 0; i < total; i++ {
		if i < completed {
			progressBar += "✅"
		} else {
			progressBar += "⚪"
		}
	}

	welcomeMsg := fmt.Sprintf(
		"👋 *Welcome to Brancr!*\n\n"+
			"Let's get you set up in 3 quick steps:\n\n"+
			"%s Progress: %d/%d\n\n",
		progressBar, completed, total)

	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	// Step 1: Complete Profile
	if !hasSocialAccount {
		welcomeMsg += "1️⃣ *Connect Social Accounts*\n" +
			"   Link Instagram, Facebook, or WhatsApp\n" +
			"   to start posting and messaging.\n\n"
		keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("🔗 Connect Accounts", "menu:connect"),
		})
	} else {
		welcomeMsg += "✅ *Social Accounts Connected*\n\n"
	}

	// Step 2: Create First Post
	welcomeMsg += "2️⃣ *Create Your First Post*\n" +
		"   Upload media and schedule it to test\n" +
		"   your workflow.\n\n"
	keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("📸 Upload & Post", "onboarding:create_post"),
	})

	// Step 3: View Example
	welcomeMsg += "3️⃣ *View Examples*\n" +
		"   See how other businesses use Brancr\n" +
		"   for inspiration.\n\n"
	keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("💡 Show Examples", "onboarding:examples"),
		tgbotapi.NewInlineKeyboardButtonData("❌ Skip Wizard", "onboarding:skip"),
	})

	msg := tgbotapi.NewMessage(chatID, welcomeMsg)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	bot.Send(msg)
}

// showSetupChecklist displays a progress checklist
func (m *TenantBotManager) showSetupChecklist(bot *tgbotapi.BotAPI, tenantID uint, chatID int64) {
	var tenant storage.Tenant
	m.DB.First(&tenant, tenantID)

	var accounts []storage.SocialAccount
	m.DB.Where("tenant_id = ? AND connected = ?", tenantID, true).Find(&accounts)

	var posts []storage.ScheduledPost
	postCount := m.DB.Where("tenant_id = ?", tenantID).Find(&posts).RowsAffected

	checklist := "📋 *Setup Checklist*\n\n"

	// Profile complete
	if tenant.OnboardingComplete {
		checklist += "✅ Profile completed\n"
	} else {
		checklist += "⚪ Complete your profile\n"
	}

	// Social accounts
	if len(accounts) > 0 {
		checklist += fmt.Sprintf("✅ Connected %d social account(s)\n", len(accounts))
		for _, acc := range accounts {
			checklist += fmt.Sprintf("   • %s\n", strings.Title(acc.Platform))
		}
	} else {
		checklist += "⚪ Connect a social account\n"
	}

	// First post
	if postCount > 0 {
		checklist += fmt.Sprintf("✅ Created %d post(s)\n", postCount)
	} else {
		checklist += "⚪ Create your first post\n"
	}

	checklist += "\n💡 _Type 'menu' anytime for quick actions._"

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔗 Connect Accounts", "menu:connect"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📸 Upload Media", "onboarding:upload"),
			tgbotapi.NewInlineKeyboardButtonData("💡 Examples", "onboarding:examples"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, checklist)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

// showExamplePosts displays example posts to inspire new tenants
func (m *TenantBotManager) showExamplePosts(bot *tgbotapi.BotAPI, chatID int64) {
	examples := `💡 *Example Post Ideas*

Here are some ideas to get you started:

*1. Product Showcase*
📸 Carousel with product photos
💬 Caption highlighting key features
⏰ Post during peak engagement hours

*2. Behind the Scenes*
📸 Single image or video
💬 Share your story or process
⏰ Build authentic connection

*3. Customer Spotlight*
📸 User-generated content
💬 Thank customers and tag them
⏰ Encourage more engagement

*4. Educational Content*
📸 Infographic or carousel
💬 Share tips or tutorials
⏰ Position as industry expert

*5. Limited Offers*
📸 Eye-catching image
💬 Clear call-to-action
⏰ Create urgency with deadline

🎯 *Pro Tips:*
• Post consistently (3-5x per week)
• Use AI captions for optimization
• Schedule posts during your audience's active hours
• Engage with comments quickly

Ready to create your first post? Upload a photo or video!`

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📸 Upload Now", "onboarding:upload"),
			tgbotapi.NewInlineKeyboardButtonData("📋 View Checklist", "onboarding:checklist"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Back to Menu", "menu:main"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, examples)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}
