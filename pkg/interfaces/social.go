package interfaces

import (
	"context"

	"github.com/similadayo/brancr/internal/storage"
)

// Scheduler handles scheduled post execution
type Scheduler interface {
	Run(ctx context.Context, pollInterval interface{}, maxAttempts int)
}

// PlatformConnector handles posting to social media platforms
type PlatformConnector interface {
	PostMedia(ctx context.Context, account *storage.SocialAccount, asset *storage.MediaAsset, caption string) error
	VerifyToken(ctx context.Context, token string) error
}

// MessageHandler processes incoming messages from social platforms
type MessageHandler interface {
	HandleMessage(ctx context.Context, tenantID uint, platform, senderID, message string) error
}

// WebhookHandler handles webhook events from social platforms
type WebhookHandler interface {
	HandleWebhook(ctx context.Context, payload []byte) error
	VerifyWebhook(token, challenge string) string
}
