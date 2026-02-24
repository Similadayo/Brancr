package bot

import (
	"context"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
)

// handleRecurringTemplateCallback handles recurring template callback queries
func (h *TenantWebhookHandler) handleRecurringTemplateCallback(bot *tgbotapi.BotAPI, tenantID uuid.UUID, cq *tgbotapi.CallbackQuery) {
	bot.Request(tgbotapi.NewCallback(cq.ID, "Loading..."))

	data := cq.Data
	chatID := cq.Message.Chat.ID

	parts := strings.Split(data, ":")
	if len(parts) < 2 {
		return
	}

	action := parts[1]
	templateManager := NewRecurringTemplateManager(h.Manager.DB, h.Manager.Logger)
	ctx := context.Background()

	switch action {
	case "list":
		templateManager.SendTemplatesList(ctx, bot, tenantID, chatID)
	case "create":
		// Start template creation flow
		bot.Send(tgbotapi.NewMessage(chatID, "📝 Let's create a recurring post template!\n\nWhat should we call it? (e.g., 'Monday Motivation', 'Weekly Product Showcase')"))
		// Would need to store state for this flow
	case "toggle":
		// Toggle template active status
		if len(parts) >= 3 {
			templateID, err := uuid.Parse(parts[2])
			if err == nil {
				var template storage.RecurringTemplate
				if err := h.Manager.DB.First(&template, templateID).Error; err == nil && template.TenantID == tenantID {
					template.Active = !template.Active
					h.Manager.DB.Save(&template)
					status := "activated"
					if !template.Active {
						status = "paused"
					}
					bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Template '%s' %s", template.Name, status)))
				}
			}
		}
	case "delete":
		// Delete template
		if len(parts) >= 3 {
			templateID, err := uuid.Parse(parts[2])
			if err == nil {
				var template storage.RecurringTemplate
				if err := h.Manager.DB.First(&template, templateID).Error; err == nil && template.TenantID == tenantID {
					h.Manager.DB.Delete(&template)
					bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Deleted template '%s'", template.Name)))
					templateManager.SendTemplatesList(ctx, bot, tenantID, chatID)
				}
			}
		}
	}
}

// handleRecurringTemplateCommand handles text commands for recurring templates
func (h *TenantWebhookHandler) handleRecurringTemplateCommand(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, arg string) {
	templateManager := NewRecurringTemplateManager(h.Manager.DB, h.Manager.Logger)
	ctx := context.Background()

	switch strings.ToLower(arg) {
	case "list", "":
		templateManager.SendTemplatesList(ctx, bot, tenantID, chatID)
	case "create":
		bot.Send(tgbotapi.NewMessage(chatID, "📝 Let's create a recurring post template!\n\nWhat should we call it? (e.g., 'Monday Motivation', 'Weekly Product Showcase')"))
	default:
		templateManager.SendTemplatesList(ctx, bot, tenantID, chatID)
	}
}
