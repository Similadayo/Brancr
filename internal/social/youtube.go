package social

import (
	"context"
	"fmt"

	"github.com/similadayo/brancr/internal/storage"
)

type YouTubeConnector struct{}

func (y *YouTubeConnector) Name() string { return "youtube" }

func (y *YouTubeConnector) PostMedia(ctx context.Context, acc *storage.SocialAccount, asset *storage.MediaAsset, caption string) error {
	fmt.Printf("[YouTube] Uploading video with caption: %s\n", caption)
	return nil
}

func (y *YouTubeConnector) HealthCheck(ctx context.Context, acc *storage.SocialAccount) error {
	return nil
}

func init() {
	Connectors["youtube"] = &YouTubeConnector{}
}
