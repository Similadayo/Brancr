package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
)

type InstagramConnector struct{}

func (i *InstagramConnector) Name() string { return "instagram" }

func (i *InstagramConnector) PostMedia(ctx context.Context, acc *storage.SocialAccount, asset *storage.MediaAsset, caption string) error {
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
	igAccountID, err := i.getInstagramAccountID(ctx, acc)
	if err != nil {
		return fmt.Errorf("failed to get Instagram account ID: %w", err)
	}

	// Step 2: Handle the media type
	var creationID string
	resolvedType := inferMediaType(asset.Type, urls)

	switch {
	case len(urls) > 1:
		creationID, err = i.createCarousel(ctx, igAccountID, acc.AccessToken, urls, caption)
		if err != nil {
			return fmt.Errorf("failed to create carousel: %w", err)
		}
	case resolvedType == "reel":
		creationID, err = i.createVideoMedia(ctx, igAccountID, acc.AccessToken, urls[0], caption, "REELS")
		if err != nil {
			return fmt.Errorf("failed to create reel media: %w", err)
		}
	case resolvedType == "video":
		// Instagram deprecated plain VIDEO uploads; treat feed videos as reels shared to feed.
		creationID, err = i.createVideoMedia(ctx, igAccountID, acc.AccessToken, urls[0], caption, "REELS")
		if err != nil {
			return fmt.Errorf("failed to create video media: %w", err)
		}
	default:
		creationID, err = i.createImageMedia(ctx, igAccountID, acc.AccessToken, urls[0], caption)
		if err != nil {
			return err
		}
	}

	// Step 3: Wait for media to be ready (Instagram needs time to process)
	if err := i.waitForMediaReady(ctx, creationID, acc.AccessToken); err != nil {
		return fmt.Errorf("media not ready: %w", err)
	}

	// Step 4: Publish the media
	publishURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/media_publish", igAccountID)
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

	if publishResp.StatusCode >= 400 {
		body, _ := io.ReadAll(publishResp.Body)
		
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

	fmt.Printf("✅ [Instagram] Posted successfully to account ID=%s\n", igAccountID)
	return nil
}

func (i *InstagramConnector) createImageMedia(ctx context.Context, igAccountID, accessToken, imageURL, caption string) (string, error) {
	createURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/media", igAccountID)
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
func (i *InstagramConnector) createCarousel(ctx context.Context, igAccountID, accessToken string, urls []string, caption string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	// Step 1: Create individual media containers for each image
	var mediaIDs []string
	createURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/media", igAccountID)

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
		if err := i.waitForMediaReady(ctx, mediaID, accessToken); err != nil {
			return "", fmt.Errorf("media not ready: %w", err)
		}

		mediaIDs = append(mediaIDs, mediaID)
	}

	// Step 2: Create carousel container with all media IDs
	carouselURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/media", igAccountID)

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

func (i *InstagramConnector) createVideoMedia(ctx context.Context, igAccountID, accessToken, videoURL, caption, mediaType string) (string, error) {
	client := &http.Client{Timeout: 45 * time.Second}
	createURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/media", igAccountID)

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
func (i *InstagramConnector) waitForMediaReady(ctx context.Context, creationID, accessToken string) error {
	statusURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=status_code&access_token=%s", creationID, accessToken)
	client := &http.Client{Timeout: 10 * time.Second}

	maxAttempts := 30 // 30 attempts * 2 seconds = 60 seconds max wait
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

func (i *InstagramConnector) HealthCheck(ctx context.Context, acc *storage.SocialAccount) error {
	if acc.AccessToken == "" {
		return fmt.Errorf("access token is empty")
	}

	igID, err := i.getInstagramAccountID(ctx, acc)
	if err != nil {
		return err
	}

	// Test by fetching account info
	url := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=id,username&access_token=%s", igID, acc.AccessToken)
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
