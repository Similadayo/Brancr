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
	"sync"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type InstagramConnector struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// SetDBAndLogger sets the database and logger for the Instagram connector
// This allows the connector to refresh tokens when needed
func (i *InstagramConnector) SetDBAndLogger(db *gorm.DB, logger *zap.Logger) {
	i.DB = db
	i.Logger = logger
}

// getInstagramAPIBase returns the correct API base URL based on login method
// Instagram Login uses graph.instagram.com/v24.0, Facebook Login uses graph.facebook.com/v18.0
func (i *InstagramConnector) getInstagramAPIBase(acc *storage.SocialAccount) string {
	if acc != nil && acc.LoginMethod == "instagram_login" {
		return "https://graph.instagram.com/v24.0"
	}
	// Default to Facebook Graph API (backward compatible)
	return "https://graph.facebook.com/v18.0"
}

// getInstagramAccountIDForPosting gets the Instagram account ID for posting
// For Instagram Login: account ID is in ExternalID or PageID (it's the Instagram User ID)
// For Facebook Login: need to get from Facebook Page's instagram_business_account
func (i *InstagramConnector) getInstagramAccountIDForPosting(ctx context.Context, acc *storage.SocialAccount) (string, error) {
	// If using Instagram Login, the account ID is already stored
	if acc.LoginMethod == "instagram_login" {
		if acc.ExternalID != "" {
			return acc.ExternalID, nil
		}
		if acc.PageID != "" {
			return acc.PageID, nil
		}
		return "", fmt.Errorf("Instagram account ID not found for Instagram Login account")
	}

	// For Facebook Login, use existing logic (get from Facebook Page)
	return i.getInstagramAccountID(ctx, acc)
}

// PostResult holds the result of posting media to Instagram
type PostResult struct {
	PlatformPostID string // The Instagram media ID after successful posting
}

// Temporary storage for media IDs (creationID -> mediaID)
// Used to pass post IDs from connector to scheduler for analytics collection
var (
	mediaIDStore   = &sync.Map{}
	mediaIDStoreMu sync.RWMutex
)

// storeMediaID stores the media ID temporarily for retrieval by scheduler
// Uses a composite key: "tenantID:platform:timestamp" to avoid collisions
func storeMediaIDForTenant(tenantID uint, platform, mediaID string) {
	key := fmt.Sprintf("%d:%s:%d", tenantID, platform, time.Now().UnixNano())
	mediaIDStore.Store(key, mediaID)
	// Clean up after 1 hour
	go func() {
		time.Sleep(1 * time.Hour)
		mediaIDStore.Delete(key)
	}()
}

// GetLatestMediaID retrieves the most recent media ID for a tenant/platform
// This is a best-effort approach - returns the first match found
func GetLatestMediaID(tenantID uint, platform string) (string, bool) {
	prefix := fmt.Sprintf("%d:%s:", tenantID, platform)
	var latestKey string
	
	mediaIDStore.Range(func(key, value interface{}) bool {
		keyStr, ok := key.(string)
		if !ok {
			return true
		}
		if strings.HasPrefix(keyStr, prefix) {
			// Extract timestamp from key
			parts := strings.Split(keyStr, ":")
			if len(parts) >= 3 {
				// This is a simplified check - in practice, we'd parse the timestamp
				latestKey = keyStr
				return false // Found one, stop
			}
		}
		return true
	})
	
	if latestKey != "" {
		if val, ok := mediaIDStore.Load(latestKey); ok {
			if mediaID, ok := val.(string); ok {
				mediaIDStore.Delete(latestKey) // Remove after retrieval
				return mediaID, true
			}
		}
	}
	
	return "", false
}

// storeMediaID stores using creationID (for backward compatibility)
func storeMediaID(creationID, mediaID string) {
	mediaIDStore.Store(creationID, mediaID)
	go func() {
		time.Sleep(1 * time.Hour)
		mediaIDStore.Delete(creationID)
	}()
}

// getMediaID retrieves a stored media ID by creation ID
func getMediaID(creationID string) (string, bool) {
	if val, ok := mediaIDStore.Load(creationID); ok {
		if mediaID, ok := val.(string); ok {
			return mediaID, true
		}
	}
	return "", false
}

func (i *InstagramConnector) Name() string { return "instagram" }

func (i *InstagramConnector) PostMedia(ctx context.Context, acc *storage.SocialAccount, asset *storage.MediaAsset, caption string) error {
	return i.PostMediaWithResult(ctx, acc, asset, caption, nil)
}

// PostMediaWithResult posts media and optionally returns the platform post ID
func (i *InstagramConnector) PostMediaWithResult(ctx context.Context, acc *storage.SocialAccount, asset *storage.MediaAsset, caption string, result *PostResult) error {
	// Ensure access token is valid and refreshed if needed (for Instagram Login)
	if err := i.ensureAccessToken(ctx, acc); err != nil {
		return fmt.Errorf("failed to ensure valid access token: %w", err)
	}

	// Decrypt token if needed (ensureAccessToken may return early without decrypting)
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		// If decryption fails, try using as-is (might already be decrypted)
		decryptedToken = acc.AccessToken
	}
	acc.AccessToken = decryptedToken // Update account object with decrypted token

	// Validate access token
	if acc.AccessToken == "" || strings.TrimSpace(acc.AccessToken) == "" {
		return fmt.Errorf("Instagram access token is empty or invalid")
	}

	// Parse media URLs
	var urls []string
	if err := json.Unmarshal([]byte(asset.URLs), &urls); err != nil || len(urls) == 0 {
		return fmt.Errorf("no media URLs provided")
	}

	// Step 1: Get Instagram Business Account ID
	igAccountID, err := i.getInstagramAccountIDForPosting(ctx, acc)
	if err != nil {
		return fmt.Errorf("failed to get Instagram account ID: %w", err)
	}

	// Step 2: Handle the media type
	var creationID string
	resolvedType := inferMediaType(asset.Type, urls)

	switch {
	case len(urls) > 1:
		creationID, err = i.createCarousel(ctx, igAccountID, acc.AccessToken, urls, caption, acc)
		if err != nil {
			return fmt.Errorf("failed to create carousel: %w", err)
		}
	case resolvedType == "reel":
		creationID, err = i.createVideoMedia(ctx, igAccountID, acc.AccessToken, urls[0], caption, "REELS", acc)
		if err != nil {
			return fmt.Errorf("failed to create reel media: %w", err)
		}
	case resolvedType == "video":
		// Instagram deprecated plain VIDEO uploads; treat feed videos as reels shared to feed.
		creationID, err = i.createVideoMedia(ctx, igAccountID, acc.AccessToken, urls[0], caption, "REELS", acc)
		if err != nil {
			return fmt.Errorf("failed to create video media: %w", err)
		}
	default:
		creationID, err = i.createImageMedia(ctx, igAccountID, acc.AccessToken, urls[0], caption, acc)
		if err != nil {
			return err
		}
	}

	// Step 3: Wait for media to be ready (Instagram needs time to process)
	if err := i.waitForMediaReady(ctx, creationID, acc.AccessToken, acc); err != nil {
		return fmt.Errorf("media not ready: %w", err)
	}

	// Step 4: Publish the media
	apiBase := i.getInstagramAPIBase(acc)
	publishURL := fmt.Sprintf("%s/%s/media_publish", apiBase, igAccountID)
	publishPayload := map[string]string{
		"creation_id":  creationID,
		"access_token": acc.AccessToken,
	}

	publishData, _ := json.Marshal(publishPayload)
	publishReq, err := http.NewRequestWithContext(ctx, "POST", publishURL, strings.NewReader(string(publishData)))
	if err != nil {
		return fmt.Errorf("failed to create publish request: %w", err)
	}

	publishReq.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	publishResp, err := client.Do(publishReq)
	if err != nil {
		return fmt.Errorf("publish request failed: %w", err)
	}
	defer publishResp.Body.Close()

	// Read response body once - check for success ID regardless of status code
	// Instagram sometimes returns success data even with error status codes
	body, _ := io.ReadAll(publishResp.Body)
	
	var successResponse struct {
		ID string `json:"id"`
	}
	var mediaID string
	if err := json.Unmarshal(body, &successResponse); err == nil && successResponse.ID != "" {
		// Post succeeded - response contains media ID (even if status code was an error)
		mediaID = successResponse.ID
		fmt.Printf("✅ [Instagram] Posted successfully to account ID=%s (media_id: %s)\n", igAccountID, mediaID)
		// Store result if result pointer provided
		if result != nil {
			result.PlatformPostID = mediaID
		}
		return nil
	}

	if publishResp.StatusCode >= 400 {
		// Parse error response to provide better error messages
		var apiError struct {
			Error struct {
				Message      string `json:"message"`
				Type         string `json:"type"`
				Code         int    `json:"code"`
				ErrorSubcode int    `json:"error_subcode"`
				ErrorUserMsg string `json:"error_user_msg"`
			} `json:"error"`
		}
		
		if err := json.Unmarshal(body, &apiError); err == nil {
			// Handle specific error codes
			if apiError.Error.Code == 4 && apiError.Error.ErrorSubcode == 2207051 {
				// For rate limit errors, wait briefly and verify if post actually succeeded
				// Instagram sometimes returns rate limit errors even when the post succeeds
				time.Sleep(2 * time.Second) // Brief wait for Instagram to process
				if verified, err := i.verifyPostPublished(ctx, igAccountID, acc.AccessToken, creationID, acc); err == nil && verified {
					fmt.Printf("✅ [Instagram] Posted successfully to account ID=%s (verified after rate limit warning)\n", igAccountID)
					// Note: We can't retrieve the exact media ID here, but post succeeded
					return nil
				}
				return fmt.Errorf("Instagram request limit reached (403): Application request limit reached. This usually happens when posting too frequently. Please wait and try again later. Error: %s", apiError.Error.ErrorUserMsg)
			}
			if apiError.Error.Code == 190 {
				return fmt.Errorf("Instagram authentication error (403): Invalid access token. Please reconnect your Instagram account. Error: %s", apiError.Error.Message)
			}
			return fmt.Errorf("Instagram publish error %d: %s (Code: %d, Subcode: %d)", 
				publishResp.StatusCode, apiError.Error.Message, apiError.Error.Code, apiError.Error.ErrorSubcode)
		}
		
		return fmt.Errorf("Instagram publish error %d: %s", publishResp.StatusCode, string(body))
	}

	// This code path should not be reached if successResponse.ID was set above
	// But keeping it for safety
	fmt.Printf("✅ [Instagram] Posted successfully to account ID=%s\n", igAccountID)
	return nil
}

func (i *InstagramConnector) createImageMedia(ctx context.Context, igAccountID, accessToken, imageURL, caption string, acc *storage.SocialAccount) (string, error) {
	apiBase := i.getInstagramAPIBase(acc)
	createURL := fmt.Sprintf("%s/%s/media", apiBase, igAccountID)
	payload := map[string]string{
		"image_url":    imageURL,
		"caption":      caption,
		"access_token": accessToken,
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", createURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return "", fmt.Errorf("failed to create media request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
	}

	var createResult map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&createResult); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	creationID, ok := createResult["id"].(string)
	if !ok {
		return "", fmt.Errorf("no creation ID returned: %v", createResult)
	}

	return creationID, nil
}

// createCarousel creates an Instagram carousel post
// Instagram carousels require: 1) Create individual media containers, 2) Wait for each to be ready, 3) Create carousel container, 4) Publish
func (i *InstagramConnector) createCarousel(ctx context.Context, igAccountID, accessToken string, urls []string, caption string, acc *storage.SocialAccount) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	// Step 1: Create individual media containers for each image
	var mediaIDs []string
	apiBase := i.getInstagramAPIBase(acc)
	createURL := fmt.Sprintf("%s/%s/media", apiBase, igAccountID)

	for _, url := range urls {
		payload := map[string]string{
			"access_token": accessToken,
		}

		payload["is_carousel_item"] = "true"

		if isVideoURL(url) {
			// Instagram requires REELS media type for feed videos, even inside carousels.
			payload["media_type"] = "REELS"
			payload["video_url"] = url
			payload["thumb_offset"] = "0"
			payload["share_to_feed"] = "true"
		} else {
			payload["image_url"] = url
		}

		jsonData, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, "POST", createURL, strings.NewReader(string(jsonData)))
		if err != nil {
			return "", fmt.Errorf("failed to create media request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("API request failed: %w", err)
		}

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return "", fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
		}

		var createResult map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&createResult); err != nil {
			resp.Body.Close()
			return "", fmt.Errorf("failed to parse response: %w", err)
		}
		resp.Body.Close()

		mediaID, ok := createResult["id"].(string)
		if !ok {
			return "", fmt.Errorf("no media ID returned: %v", createResult)
		}

		// Wait for this media to be ready
		if err := i.waitForMediaReady(ctx, mediaID, accessToken, acc); err != nil {
			return "", fmt.Errorf("media not ready: %w", err)
		}

		mediaIDs = append(mediaIDs, mediaID)
	}

	// Step 2: Create carousel container with all media IDs
	carouselURL := fmt.Sprintf("%s/%s/media", apiBase, igAccountID)

	// Build children parameter (comma-separated media IDs)
	children := strings.Join(mediaIDs, ",")

	payload := map[string]string{
		"media_type":   "CAROUSEL",
		"children":     children,
		"caption":      caption,
		"access_token": accessToken,
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", carouselURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return "", fmt.Errorf("failed to create carousel request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("carousel API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Instagram carousel API error %d: %s", resp.StatusCode, string(body))
	}

	var carouselResult map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&carouselResult); err != nil {
		return "", fmt.Errorf("failed to parse carousel response: %w", err)
	}

	carouselID, ok := carouselResult["id"].(string)
	if !ok {
		return "", fmt.Errorf("no carousel ID returned: %v", carouselResult)
	}

	return carouselID, nil
}

func (i *InstagramConnector) createVideoMedia(ctx context.Context, igAccountID, accessToken, videoURL, caption, mediaType string, acc *storage.SocialAccount) (string, error) {
	client := &http.Client{Timeout: 45 * time.Second}
	apiBase := i.getInstagramAPIBase(acc)
	createURL := fmt.Sprintf("%s/%s/media", apiBase, igAccountID)

	payload := map[string]string{
		"video_url":    videoURL,
		"caption":      caption,
		"access_token": accessToken,
		"thumb_offset": "0",
	}

	switch mediaType {
	case "REELS":
		payload["media_type"] = "REELS"
		payload["share_to_feed"] = "true"
	default:
		payload["media_type"] = "VIDEO"
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", createURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return "", fmt.Errorf("failed to create video request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("video API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Instagram video API error %d: %s", resp.StatusCode, string(body))
	}

	var createResult map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&createResult); err != nil {
		return "", fmt.Errorf("failed to parse video response: %w", err)
	}

	creationID, ok := createResult["id"].(string)
	if !ok {
		return "", fmt.Errorf("no creation ID returned: %v", createResult)
	}

	return creationID, nil
}

// waitForMediaReady polls Instagram to check if the media container is ready for publishing
// Instagram requires media to be processed before publishing (status must be "FINISHED")
func (i *InstagramConnector) waitForMediaReady(ctx context.Context, creationID, accessToken string, acc *storage.SocialAccount) error {
		apiBase := i.getInstagramAPIBase(acc)
		statusURL := fmt.Sprintf("%s/%s?fields=status_code&access_token=%s", apiBase, creationID, accessToken)
	client := &http.Client{Timeout: 10 * time.Second}

	maxAttempts := 15 // 15 attempts * 2 seconds = 30 seconds max wait (reduced to avoid rate limits)
	attempt := 0

	for attempt < maxAttempts {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for media")
		default:
		}

		req, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create status check request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			// If context cancelled, return immediately
			if ctx.Err() != nil {
				return fmt.Errorf("context cancelled: %w", ctx.Err())
			}
			// Otherwise, wait and retry
			time.Sleep(2 * time.Second)
			attempt++
			continue
		}

		if resp.StatusCode >= 400 {
			resp.Body.Close()
			// If we get an error, wait a bit and retry
			time.Sleep(2 * time.Second)
			attempt++
			continue
		}

		var statusResult map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&statusResult); err != nil {
			resp.Body.Close()
			time.Sleep(2 * time.Second)
			attempt++
			continue
		}
		resp.Body.Close()

		// Check status_code field - Instagram returns it as string or number
		var statusCode string
		if codeStr, ok := statusResult["status_code"].(string); ok {
			statusCode = codeStr
		} else if codeNum, ok := statusResult["status_code"].(float64); ok {
			// Instagram sometimes returns numeric codes: 200 = FINISHED
			if codeNum == 200 {
				return nil // Media is ready!
			}
			statusCode = fmt.Sprintf("%.0f", codeNum)
		} else {
			// Status_code not present yet - media still initializing
			time.Sleep(2 * time.Second)
			attempt++
			continue
		}

		// Instagram status codes: "FINISHED" = ready, "IN_PROGRESS" = processing, "ERROR" = failed
		// Also check for numeric equivalents
		if statusCode == "FINISHED" || statusCode == "200" {
			return nil // Media is ready!
		}

		if statusCode == "ERROR" || statusCode == "400" {
			return fmt.Errorf("media processing failed (status: %s)", statusCode)
		}

		// Still processing (IN_PROGRESS or other)
		time.Sleep(2 * time.Second)
		attempt++
	}

	return fmt.Errorf("media processing timed out after %d seconds", maxAttempts*2)
}

// getInstagramAccountID retrieves the Instagram Business Account ID
// First tries to get it from PageID (if stored), otherwise fetches it dynamically
func (i *InstagramConnector) getInstagramAccountID(ctx context.Context, acc *storage.SocialAccount) (string, error) {
	// If PageID is already an Instagram account ID, use it
	// Otherwise, fetch it from the Facebook Page
	if acc.PageID != "" && !strings.Contains(acc.PageID, "_") {
		// Instagram Business Account IDs are numeric, Facebook Page IDs contain underscores
		// This is a simple heuristic - could be improved
		return acc.PageID, nil
	}

	// Fetch from Facebook Page
	client := &http.Client{Timeout: 10 * time.Second}

	// Step 1: Get pages if PageID not set or is a Facebook Page ID
	var pageID string
	if acc.PageID == "" {
		pagesURL := fmt.Sprintf("https://graph.facebook.com/v18.0/me/accounts?access_token=%s", acc.AccessToken)
		resp, err := client.Get(pagesURL)
		if err != nil {
			return "", fmt.Errorf("failed to query pages: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("could not fetch pages: %s", string(body))
		}

		var pages map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
			return "", fmt.Errorf("invalid pages response: %w", err)
		}

		data, ok := pages["data"].([]interface{})
		if !ok || len(data) == 0 {
			return "", fmt.Errorf("no connected Facebook pages found")
		}

		page := data[0].(map[string]interface{})
		pageID = fmt.Sprintf("%v", page["id"])
	} else {
		pageID = acc.PageID
	}

	// Step 2: Get Instagram Business Account from the page
	igURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=instagram_business_account&access_token=%s", pageID, acc.AccessToken)
	resp, err := client.Get(igURL)
	if err != nil {
		return "", fmt.Errorf("failed to get Instagram account: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Instagram account lookup failed: %s", string(body))
	}

	var pageData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&pageData); err != nil {
		return "", fmt.Errorf("invalid response: %w", err)
	}

	igAccount, ok := pageData["instagram_business_account"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("no linked Instagram Business Account found")
	}

	igID := fmt.Sprintf("%v", igAccount["id"])
	return igID, nil
}

// verifyPostPublished checks if a creation_id was actually published despite an error response
// This handles cases where Instagram's API returns an error but the post actually succeeded
func (i *InstagramConnector) verifyPostPublished(ctx context.Context, igAccountID, accessToken, creationID string, acc *storage.SocialAccount) (bool, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	apiBase := i.getInstagramAPIBase(acc)
	
	// Try to query the creation_id as if it were published media
	// Once published, Instagram converts creation_id to media_id which can be queried
	mediaURL := fmt.Sprintf("%s/%s?fields=id,timestamp&access_token=%s", apiBase, creationID, accessToken)
	resp, err := client.Get(mediaURL)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 200 {
		var result struct {
			ID        string `json:"id"`
			Timestamp string `json:"timestamp"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.ID != "" {
			// Creation ID can be queried as media - post was published successfully
			return true, nil
		}
	}
	
	// Alternative: Check account's recent media (limit to most recent 5 to avoid rate limits)
	// Look for a post created in the last few minutes
	recentMediaURL := fmt.Sprintf("%s/%s/media?fields=id,timestamp&limit=5&access_token=%s", apiBase, igAccountID, accessToken)
	resp2, err := client.Get(recentMediaURL)
	if err != nil {
		return false, nil // Don't treat verification failure as error
	}
	defer resp2.Body.Close()
	
	if resp2.StatusCode == 200 {
		var mediaList struct {
			Data []struct {
				ID        string `json:"id"`
				Timestamp string `json:"timestamp"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp2.Body).Decode(&mediaList); err == nil {
			// Check if any recent media matches our creation_id
			for _, media := range mediaList.Data {
				if media.ID == creationID {
					return true, nil
				}
			}
		}
	}
	
	return false, nil // Couldn't verify - assume it failed
}

// ensureAccessToken refreshes Instagram Login tokens if they're about to expire
func (i *InstagramConnector) ensureAccessToken(ctx context.Context, acc *storage.SocialAccount) error {
	if acc.LoginMethod != "instagram_login" {
		return nil
	}

	if acc.ExpiresAt == nil {
		return nil
	}

	timeUntilExpiry := time.Until(*acc.ExpiresAt)
	if timeUntilExpiry > 7*24*time.Hour {
		return nil
	}

	if i.DB == nil || i.Logger == nil {
		return nil
	}

	i.Logger.Info("🔄 Refreshing Instagram Login access token",
		zap.Uint("tenant_id", acc.TenantID),
		zap.Uint("account_id", acc.ID),
		zap.Duration("time_until_expiry", timeUntilExpiry))

	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}

	newToken, expiresIn, err := refreshInstagramLoginToken(ctx, decryptedToken)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "expired") ||
			strings.Contains(strings.ToLower(err.Error()), "invalid") {
			updates := map[string]interface{}{"connected": false}
			if updateErr := i.DB.WithContext(ctx).
				Model(&storage.SocialAccount{}).
				Where("id = ?", acc.ID).
				Updates(updates).Error; updateErr != nil {
				i.Logger.Warn("failed to mark Instagram account as disconnected", zap.Error(updateErr))
			} else {
				acc.Connected = false
				i.Logger.Warn("🔴 Instagram Login account marked as disconnected",
					zap.Uint("tenant_id", acc.TenantID),
					zap.Uint("account_id", acc.ID))
			}
		}
		return fmt.Errorf("instagram login token refresh failed: %w", err)
	}

	encryptedToken, err := utils.EncryptToken(newToken)
	if err != nil {
		return fmt.Errorf("encrypt access token: %w", err)
	}

	expiry := time.Now().Add(time.Duration(expiresIn) * time.Second)
	updates := map[string]interface{}{
		"access_token": encryptedToken,
		"expires_at":   &expiry,
	}

	if err := i.DB.WithContext(ctx).
		Model(&storage.SocialAccount{}).
		Where("id = ?", acc.ID).
		Updates(updates).Error; err != nil {
		i.Logger.Warn("failed to persist refreshed Instagram Login token", zap.Error(err))
		return fmt.Errorf("failed to save refreshed token: %w", err)
	}

	acc.AccessToken = newToken
	acc.ExpiresAt = &expiry

	i.Logger.Info("✅ Instagram Login token refreshed successfully",
		zap.Uint("tenant_id", acc.TenantID),
		zap.Uint("account_id", acc.ID))

	return nil
}

// refreshInstagramLoginToken refreshes an Instagram Login token
func refreshInstagramLoginToken(ctx context.Context, accessToken string) (newToken string, expiresIn int, err error) {
	appSecret := os.Getenv("INSTAGRAM_APP_SECRET")
	if appSecret == "" {
		return "", 0, fmt.Errorf("INSTAGRAM_APP_SECRET not configured")
	}

	endpoint := "https://graph.instagram.com/refresh_access_token"
	params := url.Values{}
	params.Set("grant_type", "ig_refresh_token")
	params.Set("client_secret", appSecret) // Required for token refresh
	params.Set("access_token", accessToken)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", 0, fmt.Errorf("failed to parse token response: %w", err)
	}

	return tokenResponse.AccessToken, tokenResponse.ExpiresIn, nil
}

func (i *InstagramConnector) HealthCheck(ctx context.Context, acc *storage.SocialAccount) error {
	if err := i.ensureAccessToken(ctx, acc); err != nil {
		return fmt.Errorf("token refresh failed: %w", err)
	}

	// Decrypt token if needed (ensureAccessToken may return early without decrypting)
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		// If decryption fails, try using as-is (might already be decrypted)
		decryptedToken = acc.AccessToken
	}
	acc.AccessToken = decryptedToken // Update account object with decrypted token

	if acc.AccessToken == "" {
		return fmt.Errorf("access token is empty")
	}

	igID, err := i.getInstagramAccountID(ctx, acc)
	if err != nil {
		return err
	}

	// Test by fetching account info
	apiBase := i.getInstagramAPIBase(acc)
	url := fmt.Sprintf("%s/%s?fields=id,username&access_token=%s", apiBase, igID, acc.AccessToken)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("Instagram account validation failed (status %d)", resp.StatusCode)
	}

	return nil
}

func init() {
	Connectors["instagram"] = &InstagramConnector{}
}
