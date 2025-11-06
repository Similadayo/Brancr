package social

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/storage"
)

// AuthorizeManualToken validates a manual token against its platform API
func AuthorizeManualToken(ctx context.Context, platform, token string) (*storage.SocialAccount, error) {
	if token == "" {
		// ✅ WhatsApp sandbox mode doesn't require a user token
		if platform == "whatsapp" {
			acc := &storage.SocialAccount{
				Platform:  "whatsapp",
				Mode:      "sandbox",
				Connected: true,
			}
			return acc, nil
		}
		return nil, fmt.Errorf("empty token")
	}

	var verifyURL string
	switch platform {
	case "instagram", "facebook":
		verifyURL = "https://graph.facebook.com/me?fields=id"
	case "whatsapp":
		verifyURL = "https://graph.facebook.com/v17.0/me"
	case "tiktok":
		verifyURL = "https://open.tiktokapis.com/v2/user/info/"
	case "youtube":
		verifyURL = "https://www.googleapis.com/oauth2/v3/tokeninfo"
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", verifyURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth check failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("token invalid or expired (status %d)", res.StatusCode)
	}

	acc := &storage.SocialAccount{
		Platform:    platform,
		AccessToken: token,
		Connected:   true,
		Mode:        "manual",
		ExpiresAt:   nil,
		CreatedAt:   time.Now(),
	}

	return acc, nil
}
