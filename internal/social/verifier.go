package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

	// Step 2.5️⃣ — Subscribe Page to app for Instagram message webhooks
	// CRITICAL: Without this subscription, Instagram DMs won't trigger webhooks
	fmt.Printf("📨 Attempting to subscribe Page %s to receive Instagram message webhooks...\n", pageID)
	subscribeErr := SubscribePageToInstagramMessages(ctx, client, pageID, acc.AccessToken)
	if subscribeErr != nil {
		// Log warning but don't fail verification - subscription might already exist
		fmt.Printf("⚠️ Failed to subscribe Page to Instagram messages (may already be subscribed): %v\n", subscribeErr)
		fmt.Printf("⚠️ Instagram DMs may not work without Page subscription. Please check Meta Dashboard webhook configuration.\n")
	} else {
		fmt.Printf("✅ Page %s successfully subscribed to receive Instagram message webhooks\n", pageID)
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
		
		msg := fmt.Sprintf("✅ Instagram connected as @%s (verified)", username)
		if subscribeErr == nil {
			msg += "\n📨 Page subscribed to receive Instagram message webhooks"
		}
		return msg, nil

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
		if acc.AccessToken != "" {
			if decrypted, err := utils.DecryptToken(acc.AccessToken); err == nil {
				acc.AccessToken = decrypted
			}
		}

		if acc.PageID == "" || acc.AccessToken == "" {
			return "", fmt.Errorf("WhatsApp configuration incomplete — reconnect the account")
		}

		endpoint := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=id,display_phone_number,verified_name,quality_rating,status&access_token=%s",
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
			Status             string `json:"status"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return "", fmt.Errorf("invalid WhatsApp response: %w", err)
		}

		if payload.ID != "" {
			acc.ExternalID = payload.ID
			acc.PageID = payload.ID
		}
		if payload.VerifiedName != "" {
			acc.Username = payload.VerifiedName
		} else if payload.DisplayPhoneNumber != "" {
			acc.Username = payload.DisplayPhoneNumber
		}

		persistSocialAccount(acc, acc.AccessToken, db)

		name := payload.VerifiedName
		if name == "" {
			name = payload.DisplayPhoneNumber
		}
		if name == "" {
			name = acc.Username
		}

		msg := fmt.Sprintf("✅ WhatsApp Business number verified: %s", name)
		if payload.QualityRating != "" {
			msg += fmt.Sprintf("\nQuality rating: %s", payload.QualityRating)
		}
		if payload.Status != "" {
			msg += fmt.Sprintf("\nStatus: %s", payload.Status)
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
	
	// Decrypt token if it's encrypted
	rawToken := strings.TrimSpace(acc.AccessToken)
	if rawToken == "" {
		return "", fmt.Errorf("access token missing")
	}
	
	// Try to decrypt the token
	// Encrypted tokens are base64-encoded, so if base64 decode fails, it's probably plain text
	token := rawToken
	decrypted, err := utils.DecryptToken(rawToken)
	if err != nil {
		// Decryption failed - check error type
		errStr := err.Error()
		
		// If it's a base64 decode error, token is probably plain text already
		if strings.Contains(errStr, "illegal base64") || 
		   strings.Contains(errStr, "cannot decode") ||
		   strings.Contains(errStr, "invalid base64") {
			// Token is plain text, use as-is
			token = rawToken
		} else if strings.Contains(errStr, "TOKEN_SECRET_KEY") {
			// Missing or invalid encryption key - tokens might be encrypted but we can't decrypt
			// Return error indicating reconnection is needed
			return "", fmt.Errorf("encryption key missing or invalid; please reconnect your account to refresh the token")
		} else {
			// Other decryption error - token might be encrypted but corrupted or wrong key
			// Try using as-is (might be plain text despite looking encrypted)
			token = rawToken
		}
	} else {
		// Decryption succeeded, use decrypted token
		token = decrypted
	}
	
	if token == "" {
		return "", fmt.Errorf("access token is empty after decryption")
	}
	
	// Validate token format - Facebook access tokens typically start with letters/numbers
	// and don't contain special characters that would indicate it's still encrypted
	if len(token) < 10 {
		return "", fmt.Errorf("access token appears to be invalid (too short)")
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

		// CRITICAL: Subscribe the connected Facebook Page to receive Instagram message webhooks
		// We need to find the connected Page first
		pageID, pageSubscribeErr := findAndSubscribeInstagramPage(ctx, client, payload.ID, token)
		if pageSubscribeErr != nil {
			// Log warning but don't fail verification - subscription might already exist
			fmt.Printf("⚠️ Failed to subscribe Page for Instagram messages (may already be subscribed): %v\n", pageSubscribeErr)
		}

		persistSocialAccount(acc, token, db)
		label := payload.Username
		if label == "" {
			label = payload.Name
		}
		
		msg := fmt.Sprintf("✅ Instagram Business Account verified: @%s", label)
		if pageID != "" && pageSubscribeErr == nil {
			msg += fmt.Sprintf("\n📨 Page (ID: %s) subscribed to receive Instagram message webhooks", pageID)
		}
		return msg, nil

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

// SubscribePageToInstagramMessages subscribes a Facebook Page to receive Instagram message webhooks
// This is CRITICAL: Without this subscription, Instagram DMs won't trigger webhooks
// According to Meta documentation: POST /{page-id}/subscribed_apps?subscribed_fields=messages&access_token={page-access-token}
func SubscribePageToInstagramMessages(ctx context.Context, client *http.Client, pageID, accessToken string) error {
	// Decrypt token if needed
	token := accessToken
	if decrypted, err := utils.DecryptToken(accessToken); err == nil {
		token = decrypted
	}

	endpoint := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/subscribed_apps", pageID)
	
	// Use form-encoded POST as per Meta documentation
	formData := url.Values{}
	formData.Set("subscribed_fields", "messages") // Required field for Instagram message webhooks
	
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(formData.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create subscription request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	// Add access token as query parameter (Meta API standard)
	q := req.URL.Query()
	q.Set("access_token", token)
	req.URL.RawQuery = q.Encode()
	
	fmt.Printf("📤 POST %s?access_token=***\n", endpoint)
	fmt.Printf("📤 Body: subscribed_fields=messages\n")
	
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("❌ Subscription request failed: %v\n", err)
		return fmt.Errorf("subscription request failed: %w", err)
	}
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("📥 Response: Status %d, Body: %s\n", resp.StatusCode, string(body))
	
	// Check if subscription succeeded (200 OK) or already exists (may return different status)
	if resp.StatusCode == http.StatusOK {
		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err == nil {
			if success, ok := result["success"].(bool); ok && success {
				return nil // Successfully subscribed
			}
		}
		// If we got 200 OK, assume it worked even if response format is unexpected
		return nil
	}
	
	// Check for common non-fatal errors (subscription might already exist)
	var errorResp struct {
		Error struct {
			Message   string `json:"message"`
			Type      string `json:"type"`
			Code      int    `json:"code"`
			ErrorSubCode int `json:"error_subcode"`
		} `json:"error"`
	}
	
	if err := json.Unmarshal(body, &errorResp); err == nil {
		// Some errors are acceptable (e.g., already subscribed, permission denied but webhook still works)
		errorMsg := errorResp.Error.Message
		if strings.Contains(strings.ToLower(errorMsg), "already subscribed") ||
			strings.Contains(strings.ToLower(errorMsg), "already exists") ||
			(errorResp.Error.Code == 200 && errorResp.Error.ErrorSubCode == 0) {
			// Already subscribed or similar non-fatal condition
			return nil
		}
		return fmt.Errorf("Meta API error: %s (code: %d)", errorMsg, errorResp.Error.Code)
	}
	
	// If we can't parse the error, return the raw response
	if resp.StatusCode < 500 {
		// Client error (4xx) - might be non-fatal
		return fmt.Errorf("subscription failed with status %d: %s", resp.StatusCode, string(body))
	}
	
	// Server error (5xx) - retry might help
	return fmt.Errorf("Meta API server error %d: %s", resp.StatusCode, string(body))
}

// findAndSubscribeInstagramPage finds the Facebook Page connected to an Instagram Business Account
// and subscribes it to receive Instagram message webhooks
func findAndSubscribeInstagramPage(ctx context.Context, client *http.Client, igBusinessAccountID, accessToken string) (string, error) {
	// Step 1: Get the connected Facebook Page ID from the Instagram Business Account
	// The Instagram Business Account has a connected_page field that links to the Facebook Page
	pageURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=connected_page&access_token=%s", igBusinessAccountID, accessToken)
	pageResp, err := client.Get(pageURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch connected page: %w", err)
	}
	defer pageResp.Body.Close()
	
	pageBody, _ := io.ReadAll(pageResp.Body)
	if pageResp.StatusCode >= 400 {
		// If we can't find the page, try alternative approach: query pages that have this Instagram account
		// Fallback: Try to find pages that have this Instagram account linked
		fallbackURL := fmt.Sprintf("https://graph.facebook.com/v18.0/me/accounts?fields=id,instagram_business_account&access_token=%s", accessToken)
		fallbackResp, fallbackErr := client.Get(fallbackURL)
		if fallbackErr == nil {
			defer fallbackResp.Body.Close()
			fallbackBody, _ := io.ReadAll(fallbackResp.Body)
			if fallbackResp.StatusCode == 200 {
				var pagesData struct {
					Data []struct {
						ID                       string `json:"id"`
						InstagramBusinessAccount struct {
							ID string `json:"id"`
						} `json:"instagram_business_account"`
					} `json:"data"`
				}
				if json.Unmarshal(fallbackBody, &pagesData) == nil {
					for _, page := range pagesData.Data {
						if page.InstagramBusinessAccount.ID == igBusinessAccountID {
							// Found the page! Subscribe it
							subscribeErr := SubscribePageToInstagramMessages(ctx, client, page.ID, accessToken)
							return page.ID, subscribeErr
						}
					}
				}
			}
		}
		return "", fmt.Errorf("failed to find connected Facebook Page: %s", string(pageBody))
	}
	
	var pageData struct {
		ConnectedPage struct {
			ID string `json:"id"`
		} `json:"connected_page"`
	}
	
	if err := json.Unmarshal(pageBody, &pageData); err != nil {
		return "", fmt.Errorf("failed to parse connected page response: %w", err)
	}
	
	if pageData.ConnectedPage.ID == "" {
		return "", fmt.Errorf("no connected Facebook Page found for Instagram Business Account")
	}
	
	pageID := pageData.ConnectedPage.ID
	
		// Step 2: Subscribe the Page to receive Instagram message webhooks
		subscribeErr := SubscribePageToInstagramMessages(ctx, client, pageID, accessToken)
	return pageID, subscribeErr
}
