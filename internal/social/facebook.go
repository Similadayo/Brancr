package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/storage"
)

type FacebookConnector struct{}

func (f *FacebookConnector) Name() string { return "facebook" }

func (f *FacebookConnector) PostMedia(ctx context.Context, acc *storage.SocialAccount, asset *storage.MediaAsset, caption string) error {
	// Validate access token
	if acc.AccessToken == "" || strings.TrimSpace(acc.AccessToken) == "" {
		return fmt.Errorf("Facebook access token is empty or invalid")
	}

	// Parse media URLs
	var urls []string
	if err := json.Unmarshal([]byte(asset.URLs), &urls); err != nil || len(urls) == 0 {
		return fmt.Errorf("no media URLs provided")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resolvedType := inferMediaType(asset.Type, urls)
	hasVideo := false
	for _, u := range urls {
		if isVideoURL(u) {
			hasVideo = true
			break
		}
	}

	// Handle carousel (multiple media items)
	if len(urls) > 1 {
		return f.postCarousel(ctx, client, acc, urls, caption)
	}

	// Single media item
	if resolvedType == "video" || hasVideo {
		return f.postVideo(ctx, client, acc, urls[0], caption)
	}

	// Single image: Use photos endpoint
	apiURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/photos", acc.PageID)
	if acc.PageID == "" {
		apiURL = "https://graph.facebook.com/v18.0/me/photos"
	}

	payload := map[string]string{
		"url":          urls[0],
		"caption":      caption,
		"access_token": acc.AccessToken,
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Facebook API error %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("✅ [Facebook] Posted successfully to PageID=%s\n", acc.PageID)
	return nil
}

// postCarousel posts multiple images as a Facebook carousel/album
func (f *FacebookConnector) postCarousel(ctx context.Context, client *http.Client, acc *storage.SocialAccount, urls []string, caption string) error {
	// Step 1: Upload each media to get media IDs
	var mediaIDs []string
	uploadURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/photos", acc.PageID)
	if acc.PageID == "" {
		uploadURL = "https://graph.facebook.com/v18.0/me/photos"
	}

	for _, url := range urls {
		var mediaID string
		var err error

		if isVideoURL(url) {
			mediaID, err = f.uploadCarouselVideo(ctx, client, acc, url)
		} else {
			mediaID, err = f.uploadCarouselPhoto(ctx, client, acc, uploadURL, url)
		}
		if err != nil {
			return err
		}

		mediaIDs = append(mediaIDs, mediaID)
	}

	// Step 2: Create post with attached_media (carousel)
	feedURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/feed", acc.PageID)
	if acc.PageID == "" {
		feedURL = "https://graph.facebook.com/v18.0/me/feed"
	}

	// Facebook requires form-urlencoded data for attached_media
	formData := url.Values{}
	formData.Set("message", caption)
	formData.Set("access_token", acc.AccessToken)

	for idx, mediaID := range mediaIDs {
		formData.Set(fmt.Sprintf("attached_media[%d]media_fbid", idx), mediaID)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", feedURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create feed request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("feed request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Facebook feed error %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("✅ [Facebook] Posted carousel (%d images) successfully to PageID=%s\n", len(urls), acc.PageID)
	return nil
}

func (f *FacebookConnector) uploadCarouselPhoto(ctx context.Context, client *http.Client, acc *storage.SocialAccount, uploadURL, mediaURL string) (string, error) {
	payload := map[string]string{
		"url":          mediaURL,
		"published":    "false",
		"access_token": acc.AccessToken,
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", uploadURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return "", fmt.Errorf("failed to create upload request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Facebook upload error %d: %s", resp.StatusCode, string(body))
	}

	var uploadResult map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&uploadResult); err != nil {
		return "", fmt.Errorf("failed to parse upload response: %w", err)
	}

	photoID, ok := uploadResult["id"].(string)
	if !ok {
		return "", fmt.Errorf("no photo ID returned: %v", uploadResult)
	}

	return photoID, nil
}

func (f *FacebookConnector) uploadCarouselVideo(ctx context.Context, client *http.Client, acc *storage.SocialAccount, mediaURL string) (string, error) {
	uploadURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/videos", acc.PageID)
	if acc.PageID == "" {
		uploadURL = "https://graph.facebook.com/v18.0/me/videos"
	}

	form := url.Values{}
	form.Set("file_url", mediaURL)
	form.Set("published", "false")
	form.Set("access_token", acc.AccessToken)

	req, err := http.NewRequestWithContext(ctx, "POST", uploadURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create video upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("video upload request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Facebook video upload error %d: %s", resp.StatusCode, string(body))
	}

	var uploadResult map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&uploadResult); err != nil {
		return "", fmt.Errorf("failed to parse video upload response: %w", err)
	}

	videoID, ok := uploadResult["id"].(string)
	if !ok {
		return "", fmt.Errorf("no video ID returned: %v", uploadResult)
	}

	return videoID, nil
}

func (f *FacebookConnector) postVideo(ctx context.Context, client *http.Client, acc *storage.SocialAccount, videoURL, caption string) error {
	apiURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/videos", acc.PageID)
	if acc.PageID == "" {
		apiURL = "https://graph.facebook.com/v18.0/me/videos"
	}

	form := url.Values{}
	form.Set("file_url", videoURL)
	if strings.TrimSpace(caption) != "" {
		form.Set("description", caption)
	}
	form.Set("access_token", acc.AccessToken)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create video request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("video request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Facebook video API error %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("✅ [Facebook] Posted video successfully to PageID=%s\n", acc.PageID)
	return nil
}

func (f *FacebookConnector) HealthCheck(ctx context.Context, acc *storage.SocialAccount) error {
	// Simple test request to Graph API me?fields=id
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://graph.facebook.com/me?fields=id", nil)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(acc.AccessToken))
	_, err := http.DefaultClient.Do(req)
	return err
}

func init() {
	Connectors["facebook"] = &FacebookConnector{}
}
