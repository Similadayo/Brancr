package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/industry"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// IndustrySelectionHandler handles industry selection for tenants
type IndustrySelectionHandler struct {
	DB              *gorm.DB
	IndustryService *industry.Service
	Logger          *zap.Logger
}

// NewIndustrySelectionHandler creates a new industry selection handler
func NewIndustrySelectionHandler(db *gorm.DB, industryService *industry.Service, logger *zap.Logger) *IndustrySelectionHandler {
	return &IndustrySelectionHandler{
		DB:              db,
		IndustryService: industryService,
		Logger:          logger,
	}
}

// CheckAndPromptIndustrySelection checks if tenant has selected industry and prompts if not
func (h *IndustrySelectionHandler) CheckAndPromptIndustrySelection(bot *tgbotapi.BotAPI, tenantID uint, chatID int64) {
	// Get tenant
	var tenant storage.Tenant
	if err := h.DB.First(&tenant, tenantID).Error; err != nil {
		return
	}

	// If industry already selected, return
	if tenant.IndustryID != 0 {
		return
	}

	// Show industry selection
	h.ShowIndustrySelection(bot, tenantID, chatID)
}

// ShowIndustrySelection displays industry options for tenant to select
func (h *IndustrySelectionHandler) ShowIndustrySelection(bot *tgbotapi.BotAPI, tenantID uint, chatID int64) {
	ctx := context.Background()

	// Get all industries
	industries, err := h.IndustryService.GetAllIndustries(ctx)
	if err != nil {
		h.Logger.Error("failed to get industries", zap.Error(err))
		return
	}

	msg := `🏭 *Select Your Industry*

This helps me understand your business better and provide more relevant assistance.

Choose your industry:`

	var keyboardRows [][]tgbotapi.InlineKeyboardButton

	// Create buttons for each industry (max 2 per row)
	for i := 0; i < len(industries); i += 2 {
		var row []tgbotapi.InlineKeyboardButton

		// First button in row
		btn1 := tgbotapi.NewInlineKeyboardButtonData(
			industries[i].Name,
			fmt.Sprintf("industry:select:%d", industries[i].ID),
		)
		row = append(row, btn1)

		// Second button in row (if exists)
		if i+1 < len(industries) {
			btn2 := tgbotapi.NewInlineKeyboardButtonData(
				industries[i+1].Name,
				fmt.Sprintf("industry:select:%d", industries[i+1].ID),
			)
			row = append(row, btn2)
		}

		keyboardRows = append(keyboardRows, row)
	}

	// Add skip button (defaults to General)
	keyboardRows = append(keyboardRows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("⏭️ Skip (General)", "industry:select:0"),
	})

	message := tgbotapi.NewMessage(chatID, msg)
	message.ParseMode = "Markdown"
	message.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboardRows...)
	bot.Send(message)
}

// HandleIndustrySelectionCallback handles industry selection from inline button
func (h *IndustrySelectionHandler) HandleIndustrySelectionCallback(bot *tgbotapi.BotAPI, tenantID uint, chatID int64, industryIDStr string) {
	ctx := context.Background()

	industryID, err := strconv.ParseUint(industryIDStr, 10, 32)
	if err != nil {
		h.Logger.Error("invalid industry ID", zap.String("id", industryIDStr), zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Invalid selection. Please try again."))
		return
	}

	// If 0, set to General industry
	if industryID == 0 {
		generalIndustry, err := h.IndustryService.GetAllIndustries(ctx)
		if err == nil && len(generalIndustry) > 0 {
			// Find General industry
			for _, ind := range generalIndustry {
				if strings.ToLower(ind.Name) == "general" {
					industryID = uint64(ind.ID)
					break
				}
			}
		}
	}

	// Set tenant industry
	if err := h.IndustryService.SetTenantIndustry(ctx, tenantID, uint(industryID)); err != nil {
		h.Logger.Error("failed to set tenant industry", zap.Error(err))
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Failed to save industry selection. Please try again."))
		return
	}

	// Get industry name for confirmation
	selectedIndustry, err := h.IndustryService.GetIndustryByID(ctx, uint(industryID))
	industryName := "General"
	if err == nil {
		industryName = selectedIndustry.Name
	}

	// Show confirmation and next steps
	capabilities, err := h.IndustryService.GetIndustryCapabilities(ctx, tenantID)
	nextSteps := ""
	if err == nil {
		if capabilities.HasProducts {
			nextSteps += "\n\n📦 You can now add products to your catalog!"
		}
		if capabilities.HasMenu {
			nextSteps += "\n\n🍽️ You can now add menu items!"
		}
		if capabilities.HasServices {
			nextSteps += "\n\n💼 You can now add services!"
		}
	}

	confirmMsg := fmt.Sprintf(
		"✅ Industry set to: *%s*%s\n\n"+
			"I'll now tailor my responses to your industry. Type 'menu' to see what I can do!",
		industryName, nextSteps)

	bot.Send(tgbotapi.NewMessage(chatID, confirmMsg))
	bot.Send(tgbotapi.NewMessage(chatID, "💡 _Tip: You can change your industry anytime by typing 'change industry'._"))
}
