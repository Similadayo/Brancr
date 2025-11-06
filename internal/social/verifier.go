package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"gorm.io/gorm"
)

// VerifySocialConnection validates a tenant's social account token manually (until OAuth2 is ready)
// If db is provided, it will also save the Instagram Business Account ID to ExternalID
func VerifySocialConnection(ctx context.Context, acc *storage.SocialAccount, db *gorm.DB) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	switch acc.Platform {
	case "instagram":
		// Step 1️⃣ — Get Facebook pages linked to this token
		pagesURL := fmt.Sprintf("https://graph.facebook.com/v17.0/me/accounts?access_token=%s", acc.AccessToken)
		resp, err := client.Get(pagesURL)
		if err != nil {
			return "", fmt.Errorf("failed to query connected pages: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("could not fetch connected pages: %s", string(body))
		}

		var pages map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
			return "", fmt.Errorf("invalid response for Facebook pages: %w", err)
		}
		data, ok := pages["data"].([]any)
		if !ok || len(data) == 0 {
			return "", fmt.Errorf("no connected Facebook pages found for this token")
		}

		page := data[0].(map[string]any)
		pageID := fmt.Sprintf("%v", page["id"])

		// Step 2️⃣ — Fetch the Instagram business account linked to that page
		igReqURL := fmt.Sprintf("https://graph.facebook.com/v17.0/%s?fields=instagram_business_account&access_token=%s", pageID, acc.AccessToken)
		igRes, err := client.Get(igReqURL)
		if err != nil {
			return "", fmt.Errorf("failed to get instagram business link: %w", err)
		}
		defer igRes.Body.Close()

		var igData map[string]any
		json.NewDecoder(igRes.Body).Decode(&igData)
		igFieldRaw, ok := igData["instagram_business_account"].(map[string]any)
		if !ok {
			return "", fmt.Errorf("no linked Instagram Business Account found for this page")
		}

		igID := fmt.Sprintf("%v", igFieldRaw["id"])

		// Save Instagram Business Account ID to ExternalID for webhook routing
		if db != nil {
			acc.ExternalID = igID
			acc.PageID = igID // Also save to PageID for backward compatibility
			acc.Connected = true
			if err := db.Save(acc).Error; err != nil {
				// Log error but don't fail verification
				fmt.Printf("⚠️ Warning: Failed to save Instagram Business Account ID: %v\n", err)
			}
		}

		// Step 3️⃣ — Fetch actual Instagram profile info
		igInfoURL := fmt.Sprintf("https://graph.facebook.com/v17.0/%s?fields=id,name,username&access_token=%s", igID, acc.AccessToken)
		igInfoRes, err := client.Get(igInfoURL)
		if err != nil {
			return "", fmt.Errorf("failed to fetch instagram profile: %w", err)
		}
		defer igInfoRes.Body.Close()

		body, _ := io.ReadAll(igInfoRes.Body)
		if igInfoRes.StatusCode != 200 {
			return "", fmt.Errorf("invalid instagram info response: %s", string(body))
		}

		var igInfo map[string]any
		json.Unmarshal(body, &igInfo)
		username := fmt.Sprintf("%v", igInfo["username"])

		return fmt.Sprintf("✅ Instagram connected as @%s (verified)", username), nil

	case "facebook":
		// Step 1️⃣ — Basic profile check
		userURL := fmt.Sprintf("https://graph.facebook.com/v17.0/me?fields=id,name&access_token=%s", acc.AccessToken)
		userResp, err := client.Get(userURL)
		if err != nil {
			return "", fmt.Errorf("failed to contact Facebook Graph: %w", err)
		}
		defer userResp.Body.Close()

		body, _ := io.ReadAll(userResp.Body)

		// If it’s a Page token, /me will still return info, but we handle both types
		if userResp.StatusCode != 200 {
			// Try to detect if token is for a Page
			var errResp map[string]any
			json.Unmarshal(body, &errResp)
			msg := fmt.Sprintf("%v", errResp["error"])
			if strings.Contains(msg, "(Page)") {
				// Fallback: Fetch Page directly
				pageURL := fmt.Sprintf("https://graph.facebook.com/v17.0/%s?fields=id,name,fan_count&access_token=%s", acc.AccessToken, acc.AccessToken)
				pageResp, err := client.Get(pageURL)
				if err != nil {
					return "", fmt.Errorf("failed to query page info: %w", err)
				}
				defer pageResp.Body.Close()

				pb, _ := io.ReadAll(pageResp.Body)
				if pageResp.StatusCode != 200 {
					return "", fmt.Errorf("invalid page token: %s", string(pb))
				}

				var page map[string]any
				json.Unmarshal(pb, &page)
				name := fmt.Sprintf("%v", page["name"])
				id := fmt.Sprintf("%v", page["id"])

				return fmt.Sprintf("✅ Facebook Page connected as '%s' (Page ID: %s)", name, id), nil
			}

			return "", fmt.Errorf("invalid Facebook token: %s", string(body))
		}

		// Parse user data (User Token)
		var userData map[string]any
		json.Unmarshal(body, &userData)
		name := fmt.Sprintf("%v", userData["name"])
		id := fmt.Sprintf("%v", userData["id"])

		// Step 2️⃣ — Try to fetch connected pages (only for User Tokens)
		pagesURL := fmt.Sprintf("https://graph.facebook.com/v17.0/me/accounts?access_token=%s", acc.AccessToken)
		pagesResp, err := client.Get(pagesURL)
		if err != nil {
			return "", fmt.Errorf("failed to fetch connected pages: %w", err)
		}
		defer pagesResp.Body.Close()

		pBody, _ := io.ReadAll(pagesResp.Body)
		if pagesResp.StatusCode != 200 {
			// Silently fallback — maybe this is a direct user profile, not a business manager
			return fmt.Sprintf("✅ Facebook user connected as '%s' (User ID: %s)\nℹ️ No pages found or token may be user-only.", name, id), nil
		}

		var pages map[string]any
		json.Unmarshal(pBody, &pages)
		var pageList []string
		if data, ok := pages["data"].([]any); ok {
			for _, d := range data {
				m := d.(map[string]any)
				pageList = append(pageList, fmt.Sprintf("%v", m["name"]))
			}
		}

		if len(pageList) == 0 {
			return fmt.Sprintf("⚠️ Facebook user verified (%s) but no connected Pages found.", name), nil
		}

		return fmt.Sprintf("✅ Facebook connected as %s (user ID: %s)\n📄 Pages: %v", name, id, pageList), nil

	case "whatsapp":
		// Step 1️⃣ — Use Twilio Sandbox for testing
		sid := os.Getenv("TWILIO_ACCOUNT_SID")
		token := os.Getenv("TWILIO_AUTH_TOKEN")
		from := os.Getenv("TWILIO_WHATSAPP_FROM")

		if sid == "" || token == "" || from == "" {
			return "", fmt.Errorf("Twilio credentials missing; set TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN, TWILIO_WHATSAPP_FROM")
		}

		url := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s.json", sid)
		req, _ := http.NewRequest("GET", url, nil)
		req.SetBasicAuth(sid, token)

		res, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to contact Twilio: %w", err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(res.Body)
			return "", fmt.Errorf("invalid Twilio credentials: %s", string(body))
		}

		return "✅ WhatsApp (Twilio Sandbox) connected and verified", nil

	case "youtube":
		apiKey := os.Getenv("YOUTUBE_API_KEY")
		token := acc.AccessToken

		if apiKey == "" && token == "" {
			return "", fmt.Errorf("YouTube API key or access token not provided")
		}

		url := "https://www.googleapis.com/youtube/v3/channels?part=snippet&mine=true"
		req, _ := http.NewRequest("GET", url, nil)

		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		} else {
			q := req.URL.Query()
			q.Add("key", apiKey)
			req.URL.RawQuery = q.Encode()
		}

		res, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to contact YouTube API: %w", err)
		}
		defer res.Body.Close()

		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			return "", fmt.Errorf("YouTube verification failed: %s", string(body))
		}

		var yt map[string]any
		json.Unmarshal(body, &yt)
		items, ok := yt["items"].([]any)
		if !ok || len(items) == 0 {
			return "", fmt.Errorf("no YouTube channels found for this token")
		}

		snippet := items[0].(map[string]any)["snippet"].(map[string]any)
		title := fmt.Sprintf("%v", snippet["title"])
		return fmt.Sprintf("✅ YouTube connected as '%s' (channel verified)", title), nil

	case "tiktok":
		appID := os.Getenv("TIKTOK_APP_ID")
		token := acc.AccessToken

		if appID == "" {
			return "", fmt.Errorf("TikTok app credentials not configured (TIKTOK_APP_ID missing)")
		}

		if token == "" {
			return "⚠️ TikTok manual token mode active — token not yet connected.\nYou’ll be able to post after verification.", nil
		}

		url := "https://open.tiktokapis.com/v2/user/info/"
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		res, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to contact TikTok API: %w", err)
		}
		defer res.Body.Close()

		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 {
			return "", fmt.Errorf("TikTok verification failed: %s", string(body))
		}

		var tk map[string]any
		json.Unmarshal(body, &tk)
		dataRaw, ok := tk["data"].(map[string]any)
		if !ok {
			return "", fmt.Errorf("unexpected TikTok API response: missing data field")
		}

		user := dataRaw["display_name"]
		return fmt.Sprintf("✅ TikTok connected as %v", user), nil

	default:
		return "", fmt.Errorf("unsupported platform: %s", acc.Platform)
	}
}
