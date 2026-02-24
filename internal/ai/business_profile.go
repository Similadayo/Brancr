package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// BusinessProfileSessions tracks per-chat onboarding states
var BusinessProfileSessions sync.Map // chatID → map[string]string

// ✅ StartBusinessProfileOnboarding starts natural business info setup
func StartBusinessProfileOnboarding(
	ctx context.Context,
	db *gorm.DB,
	bot *tgbotapi.BotAPI,
	tenantID uuid.UUID,
	chatID int64,
	client *AIClient,
	logger *zap.Logger,
) {
	step := map[string]string{"step": "niche"}
	BusinessProfileSessions.Store(chatID, step)

	bot.Send(tgbotapi.NewMessage(chatID, "What's your business niche or industry? (e.g. bakery, fashion brand, tech agency)"))
}

// ✅ ContinueBusinessProfileOnboarding continues the conversation dynamically
func ContinueBusinessProfileOnboarding(
	ctx context.Context,
	db *gorm.DB,
	bot *tgbotapi.BotAPI,
	tenantID uuid.UUID,
	chatID int64,
	text string,
	client *AIClient,
	logger *zap.Logger,
) {
	raw, _ := BusinessProfileSessions.Load(chatID)
	state, _ := raw.(map[string]string)
	if state == nil {
		StartBusinessProfileOnboarding(ctx, db, bot, tenantID, chatID, client, logger)
		return
	}

	step := state["step"]
	text = strings.TrimSpace(text)

	switch step {

	// 🧩 Step 1: Niche
	case "niche":
		state["niche"] = text
		state["step"] = "description"
		BusinessProfileSessions.Store(chatID, state)
		reply := fmt.Sprintf("Cool, %s. Tell me what your business does and who it serves.", text)
		bot.Send(tgbotapi.NewMessage(chatID, reply))
		return

	// 🧩 Step 2: Description
	case "description":
		state["description"] = text
		state["step"] = "tone"
		BusinessProfileSessions.Store(chatID, state)
		bot.Send(tgbotapi.NewMessage(chatID, "Got it. How would you describe your brand's communication style? (e.g. friendly, confident, calm, professional)"))
		return

	// 🧩 Step 3: Tone
	case "tone":
		state["tone"] = text
		BusinessProfileSessions.Store(chatID, state)

		// ✅ Save to DB (create or update)
		var profile storage.BusinessProfile
		if err := db.First(&profile, "tenant_id = ?", tenantID).Error; err == gorm.ErrRecordNotFound {
			profile = storage.BusinessProfile{
				TenantID:    tenantID,
				Niche:       state["niche"],
				Description: state["description"],
				Tone:        state["tone"],
			}
			if err := db.Create(&profile).Error; err != nil {
				logger.Error("failed to create business profile", zap.Error(err))
				bot.Send(tgbotapi.NewMessage(chatID, "Something went wrong saving that. Try again later."))
				return
			}
		} else {
			profile.Niche = state["niche"]
			profile.Description = state["description"]
			profile.Tone = state["tone"]
			db.Save(&profile)
		}

		// 🧠 Delete session
		BusinessProfileSessions.Delete(chatID)

		// 🎉 Confirmation
		finalMsg := fmt.Sprintf("Done! Here's what I know:\n\nNiche: %s\nTone: %s\nAbout: %s\n\nI'm ready now. Try saying hi!", state["niche"], state["tone"], state["description"])
		bot.Send(tgbotapi.NewMessage(chatID, finalMsg))
		return
	}
}
