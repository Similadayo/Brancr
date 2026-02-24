package social

import (
	"context"

	"github.com/similadayo/brancr/internal/storage"
)

type SocialConnector interface {
	Name() string
	PostMedia(ctx context.Context, account *storage.SocialAccount, asset *storage.MediaAsset, caption string) error
	HealthCheck(ctx context.Context, account *storage.SocialAccount) error
}

var Connectors = map[string]SocialConnector{}
