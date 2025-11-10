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
	"github.com/similadayo/brancr/internal/utils"
	"gorm.io/gorm"
)

// VerifySocialConnection validates a tenant's social account token manually (until OAuth2 is ready)
// If db is provided, it will also save the Instagram Business Account ID to ExternalID
func VerifySocialConnection(ctx context.Context, acc *storage.SocialAccount, db *gorm.DB) (string, error) {
	if strings.EqualFold(acc.Mode, "oauth2") && !strings.EqualFold(acc.Platform, "tiktok") {
		return verifyMetaOAuthConnection(ctx, acc, db)
	}

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

		persistSocialAccount(acc, acc.AccessToken, db)
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

				persistSocialAccount(acc, acc.AccessToken, db)
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
			persistSocialAccount(acc, acc.AccessToken, db)
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

		persistSocialAccount(acc, acc.AccessToken, db)
		return fmt.Sprintf("✅ Facebook connected as %s (user ID: %s)\n📄 Pages: %v", name, id, pageList), nil

	case "whatsapp":
		if acc.PageID == "" {
			return "", fmt.Errorf("WhatsApp phone_number_id missing — reconnect the account")
		}

		endpoint := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=id,display_phone_number,verified_name,quality_rating&access_token=%s",
			acc.PageID, acc.AccessToken)
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return "", fmt.Errorf("failed to create verification request: %w", err)
		}

		res, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to contact WhatsApp Graph API: %w", err)
		}
		defer res.Body.Close()

		body, _ := io.ReadAll(res.Body)
		if res.StatusCode >= 400 {
			return "", fmt.Errorf("WhatsApp verification failed (%d): %s", res.StatusCode, string(body))
		}

		var payload struct {
			ID                 string `json:"id"`
			DisplayPhoneNumber string `json:"display_phone_number"`
			VerifiedName       string `json:"verified_name"`
			QualityRating      string `json:"quality_rating"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return "", fmt.Errorf("invalid WhatsApp response: %w", err)
		}

		name := payload.VerifiedName
		if name == "" {
			name = payload.DisplayPhoneNumber
		}
		if name == "" {
			name = acc.Username
		}

		persistSocialAccount(acc, acc.AccessToken, db)

		msg := fmt.Sprintf("✅ WhatsApp Business number verified: %s", name)
		if payload.QualityRating != "" {
			msg += fmt.Sprintf("\nQuality rating: %s", payload.QualityRating)
		}
		return msg, nil

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
		persistSocialAccount(acc, acc.AccessToken, db)
		return fmt.Sprintf("✅ YouTube connected as '%s' (channel verified)", title), nil

	case "tiktok":
		appID := os.Getenv("TIKTOK_CLIENT_KEY")
		token := acc.AccessToken

		if appID == "" {
			return "", fmt.Errorf("TikTok app credentials not configured (TIKTOK_APP_ID missing)")
		}

		if token == "" {
			return "⚠️ TikTok manual token mode active — token not yet connected.\nYou’ll be able to post after verification.", nil
		}

		url := "https://open.tiktokapis.com/v2/user/info/?fields=display_name,username,open_id,profile_image_url"
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		res, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to contact TikTok API: %w", err)
		}
		defer res.Body.Close()

		body, _ := io.ReadAll(res.Body)
		if res.StatusCode == http.StatusUnauthorized {
			var payload struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal(body, &payload)
			if strings.EqualFold(payload.Error.Code, "scope_not_authorized") {
				persistSocialAccount(acc, acc.AccessToken, db)
				return "✅ TikTok connected (no profile info — please re-connect and approve basic profile access if you need it).", nil
			}
		}
		if res.StatusCode != http.StatusOK {
			return "", fmt.Errorf("TikTok verification failed: %s", string(body))
		}

		var tk map[string]any
		json.Unmarshal(body, &tk)
		dataRaw, ok := tk["data"].(map[string]any)
		if !ok {
			return "", fmt.Errorf("unexpected TikTok API response: missing data field")
		}

		user := dataRaw["display_name"]
		persistSocialAccount(acc, acc.AccessToken, db)
		return fmt.Sprintf("✅ TikTok connected as %v", user), nil

	default:
		return "", fmt.Errorf("unsupported platform: %s", acc.Platform)
	}
}

func verifyMetaOAuthConnection(ctx context.Context, acc *storage.SocialAccount, db *gorm.DB) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	token := strings.TrimSpace(acc.AccessToken)
	if token == "" {
		return "", fmt.Errorf("access token missing")
	}

	switch acc.Platform {
	case "facebook":
		if acc.PageID == "" {
			return "", fmt.Errorf("facebook page id missing; reconnect account")
		}

		url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=id,name,fan_count&access_token=%s", acc.PageID, token)
		resp, err := client.Get(url)
		if err != nil {
			return "", fmt.Errorf("failed to fetch facebook page info: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("facebook verification failed: %s", string(body))
		}

		var page struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			FanCount int    `json:"fan_count"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return "", fmt.Errorf("invalid facebook response: %w", err)
		}

		if page.ID != "" {
			acc.ExternalID = page.ID
		}
		if page.Name != "" {
			acc.Username = page.Name
		}

		persistSocialAccount(acc, token, db)
		return fmt.Sprintf("✅ Facebook Page verified: %s (ID: %s)", page.Name, page.ID), nil

	case "instagram":
		igID := acc.ExternalID
		if igID == "" {
			igID = acc.PageID
		}
		if igID == "" {
			return "", fmt.Errorf("instagram business account id missing; reconnect account")
		}

		url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=id,username,name&access_token=%s", igID, token)
		resp, err := client.Get(url)
		if err != nil {
			return "", fmt.Errorf("failed to fetch instagram account: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("instagram verification failed: %s", string(body))
		}

		var payload struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Name     string `json:"name"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return "", fmt.Errorf("invalid instagram response: %w", err)
		}

		acc.ExternalID = payload.ID
		acc.PageID = payload.ID
		acc.Username = payload.Username

		persistSocialAccount(acc, token, db)
		label := payload.Username
		if label == "" {
			label = payload.Name
		}
		return fmt.Sprintf("✅ Instagram Business Account verified: @%s", label), nil

	case "whatsapp":
		if acc.PageID == "" {
			return "", fmt.Errorf("whatsapp phone number id missing; reconnect account")
		}

		url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=id,display_phone_number,verified_name&access_token=%s", acc.PageID, token)
		resp, err := client.Get(url)
		if err != nil {
			return "", fmt.Errorf("failed to fetch whatsapp phone number: %w", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("whatsapp verification failed: %s", string(body))
		}

		var payload struct {
			ID                 string `json:"id"`
			DisplayPhoneNumber string `json:"display_phone_number"`
			VerifiedName       string `json:"verified_name"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return "", fmt.Errorf("invalid whatsapp response: %w", err)
		}

		if payload.ID != "" {
			acc.PageID = payload.ID
			acc.ExternalID = payload.ID
		}
		if payload.VerifiedName != "" {
			acc.Username = payload.VerifiedName
		} else if payload.DisplayPhoneNumber != "" {
			acc.Username = payload.DisplayPhoneNumber
		}

		persistSocialAccount(acc, token, db)
		return fmt.Sprintf("✅ WhatsApp Business number verified: %s", acc.Username), nil

	default:
		return "", fmt.Errorf("oauth verification not supported for %s", acc.Platform)
	}
}

func persistSocialAccount(acc *storage.SocialAccount, plainToken string, db *gorm.DB) {
	if db == nil {
		return
	}
	encrypted, err := utils.EncryptToken(plainToken)
	if err == nil {
		acc.AccessToken = encrypted
	}
	acc.Connected = true
	_ = db.Save(acc).Error
}
