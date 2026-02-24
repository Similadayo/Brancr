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
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
)

// SendInstagramAudio sends an audio message via Instagram Messaging API
func (h *MessageHandler) SendInstagramAudio(ctx context.Context, acc *storage.SocialAccount, threadID, audioURL string) error {
	return h.sendInstagramMedia(ctx, acc, threadID, "audio", audioURL, "")
}

// SendInstagramVideo sends a video message via Instagram Messaging API
func (h *MessageHandler) SendInstagramVideo(ctx context.Context, acc *storage.SocialAccount, threadID, videoURL string) error {
	return h.sendInstagramMedia(ctx, acc, threadID, "video", videoURL, "")
}

// sendInstagramMedia sends audio or video via Instagram Messaging API
func (h *MessageHandler) sendInstagramMedia(ctx context.Context, acc *storage.SocialAccount, threadID, mediaType, mediaURL, caption string) error {
	// Ensure access token is valid
	if acc.LoginMethod == "instagram_login" {
		if err := h.ensureInstagramLoginToken(ctx, acc); err != nil {
			h.Logger.Warn("⚠️ Failed to refresh Instagram Login token", zap.Error(err))
		}
	}

	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}

	if decryptedToken == "" {
		return fmt.Errorf("Instagram access token is empty")
	}

	igAccountID := acc.ExternalID
	if igAccountID == "" {
		igAccountID = acc.PageID
	}

	var apiURL string
	if acc.LoginMethod == "instagram_login" {
		apiURL = fmt.Sprintf("https://graph.instagram.com/v24.0/%s/messages", igAccountID)
	} else {
		apiURL = fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", igAccountID)
	}

	payload := map[string]interface{}{
		"recipient": map[string]string{
			"id": threadID,
		},
		"message": map[string]interface{}{
			"attachment": map[string]interface{}{
				"type": mediaType,
				"payload": map[string]string{
					"url": mediaURL,
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if acc.LoginMethod == "instagram_login" {
		req.Header.Set("Authorization", "Bearer "+decryptedToken)
	} else {
		apiURL = fmt.Sprintf("%s?access_token=%s", apiURL, url.QueryEscape(decryptedToken))
		req.URL, _ = url.Parse(apiURL)
	}

	client := &http.Client{Timeout: 30 * time.Second} // Longer timeout for media
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
	}

	h.Logger.Info("✅ Instagram media sent successfully",
		zap.String("type", mediaType),
		zap.String("recipient_id", threadID))

	return nil
}

// SendInstagramSticker sends a heart sticker via Instagram Messaging API
func (h *MessageHandler) SendInstagramSticker(ctx context.Context, acc *storage.SocialAccount, threadID string) error {
	// Ensure access token is valid
	if acc.LoginMethod == "instagram_login" {
		if err := h.ensureInstagramLoginToken(ctx, acc); err != nil {
			h.Logger.Warn("⚠️ Failed to refresh Instagram Login token", zap.Error(err))
		}
	}

	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}

	if decryptedToken == "" {
		return fmt.Errorf("Instagram access token is empty")
	}

	igAccountID := acc.ExternalID
	if igAccountID == "" {
		igAccountID = acc.PageID
	}

	var apiURL string
	if acc.LoginMethod == "instagram_login" {
		apiURL = fmt.Sprintf("https://graph.instagram.com/v24.0/%s/messages", igAccountID)
	} else {
		apiURL = fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", igAccountID)
	}

	payload := map[string]interface{}{
		"recipient": map[string]string{
			"id": threadID,
		},
		"message": map[string]interface{}{
			"attachment": map[string]interface{}{
				"type": "like_heart",
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if acc.LoginMethod == "instagram_login" {
		req.Header.Set("Authorization", "Bearer "+decryptedToken)
	} else {
		apiURL = fmt.Sprintf("%s?access_token=%s", apiURL, url.QueryEscape(decryptedToken))
		req.URL, _ = url.Parse(apiURL)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
	}

	h.Logger.Info("✅ Instagram sticker sent successfully",
		zap.String("recipient_id", threadID))

	return nil
}

// SendInstagramPublishedPost sends a published Instagram post via Instagram Messaging API
// postID should be the Meta ID for the post (the app user must own the post)
func (h *MessageHandler) SendInstagramPublishedPost(ctx context.Context, acc *storage.SocialAccount, threadID, postID string) error {
	// Ensure access token is valid
	if acc.LoginMethod == "instagram_login" {
		if err := h.ensureInstagramLoginToken(ctx, acc); err != nil {
			h.Logger.Warn("⚠️ Failed to refresh Instagram Login token", zap.Error(err))
		}
	}

	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}

	if decryptedToken == "" {
		return fmt.Errorf("Instagram access token is empty")
	}

	igAccountID := acc.ExternalID
	if igAccountID == "" {
		igAccountID = acc.PageID
	}

	var apiURL string
	if acc.LoginMethod == "instagram_login" {
		apiURL = fmt.Sprintf("https://graph.instagram.com/v24.0/%s/messages", igAccountID)
	} else {
		apiURL = fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", igAccountID)
	}

	payload := map[string]interface{}{
		"recipient": map[string]string{
			"id": threadID,
		},
		"message": map[string]interface{}{
			"attachment": map[string]interface{}{
				"type": "MEDIA_SHARE",
				"payload": map[string]string{
					"id": postID,
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if acc.LoginMethod == "instagram_login" {
		req.Header.Set("Authorization", "Bearer "+decryptedToken)
	} else {
		apiURL = fmt.Sprintf("%s?access_token=%s", apiURL, url.QueryEscape(decryptedToken))
		req.URL, _ = url.Parse(apiURL)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
	}

	h.Logger.Info("✅ Instagram published post sent successfully",
		zap.String("recipient_id", threadID),
		zap.String("post_id", postID))

	return nil
}
