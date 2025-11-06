package interfaces

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/storage"
)

// BotManager manages tenant bot instances
type BotManager interface {
	GetBot(tenantID uint) (*tgbotapi.BotAPI, bool)
	GetOwnerID(tenantID uint) int64
	StartBotForTenant(tenant *storage.Tenant) error
	StopBot(tenantID uint)
	InitBots() error
	GenerateAIResponse(ctx context.Context, tenantID uint, userMsg string) (string, error)
	TrackOrCreateCustomer(ctx context.Context, tenantID uint, channel, platformID, displayName, username, profilePic string) (*storage.Customer, error)
}

