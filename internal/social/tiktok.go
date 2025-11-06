package social

import (
	"context"
	"fmt"

	"github.com/similadayo/brancr/internal/storage"
)

type TikTokConnector struct{}

func (t *TikTokConnector) Name() string { return "tiktok" }

func (t *TikTokConnector) PostMedia(ctx context.Context, acc *storage.SocialAccount, asset *storage.MediaAsset, caption string) error {
	fmt.Printf("[TikTok] Uploading video... Caption=%s URLs=%s\n", caption, asset.URLs)
	return nil
}

func (t *TikTokConnector) HealthCheck(ctx context.Context, acc *storage.SocialAccount) error {
	return nil
}

func init() {
	Connectors["tiktok"] = &TikTokConnector{}
}
