package social

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TikTokWebhookHandler handles incoming webhook events from TikTok
type TikTokWebhookHandler struct {
	DB            *gorm.DB
	Logger        *zap.Logger
	WebhookSecret string // Secret for verifying webhook signatures
}

// NewTikTokWebhookHandler creates a new webhook handler
func NewTikTokWebhookHandler(db *gorm.DB, logger *zap.Logger, webhookSecret string) *TikTokWebhookHandler {
	return &TikTokWebhookHandler{
		DB:            db,
		Logger:        logger,
		WebhookSecret: webhookSecret,
	}
}

// WebhookEvent represents a TikTok webhook event
type WebhookEvent struct {
	Event     string                 `json:"event"`     // video.status_update, comment.created, etc.
	Timestamp int64                  `json:"timestamp"` // Unix timestamp
	Data      map[string]interface{} `json:"data"`      // Event-specific data
	VideoID   string                 `json:"video_id,omitempty"`
	PublishID string                 `json:"publish_id,omitempty"`
	CommentID string                 `json:"comment_id,omitempty"`
	TenantID  uint                   `json:"-"` // Extracted from account lookup
	AccountID uint                   `json:"-"` // Social account ID
}

// HandleWebhook processes incoming TikTok webhook events
func (h *TikTokWebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.Logger.Error("failed to read webhook body", zap.Error(err))
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// SECURITY: Webhook secret is REQUIRED - reject if not configured
	if h.WebhookSecret == "" {
		h.Logger.Error("webhook secret not configured - rejecting request for security",
			zap.String("remote_addr", r.RemoteAddr))
		http.Error(w, "Webhook endpoint disabled - secret not configured", http.StatusServiceUnavailable)
		return
	}

	// Verify webhook signature (REQUIRED for security)
	signature := r.Header.Get("X-TikTok-Signature")
	if signature == "" {
		signature = r.Header.Get("X-Signature") // Alternative header name
	}

	if signature == "" {
		h.Logger.Warn("missing webhook signature header",
			zap.String("remote_addr", r.RemoteAddr),
			zap.String("body_length", fmt.Sprintf("%d", len(body))))
		http.Error(w, "Missing signature header", http.StatusUnauthorized)
		return
	}

	if !h.verifySignature(body, signature) {
		h.Logger.Warn("invalid webhook signature",
			zap.String("signature", signature),
			zap.String("remote_addr", r.RemoteAddr),
			zap.String("body_length", fmt.Sprintf("%d", len(body))))
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// Parse webhook event
	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.Logger.Error("failed to parse webhook event", zap.Error(err))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Extract event data
	if event.Data == nil {
		// Try parsing as nested structure
		var nested struct {
			Event     string                 `json:"event"`
			Timestamp int64                  `json:"timestamp"`
			Data      map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(body, &nested); err == nil {
			event.Event = nested.Event
			event.Timestamp = nested.Timestamp
			event.Data = nested.Data
		}
	}

	// Extract video_id, publish_id, comment_id from data if not at top level
	if event.VideoID == "" && event.Data != nil {
		if vid, ok := event.Data["video_id"].(string); ok {
			event.VideoID = vid
		}
	}
	if event.PublishID == "" && event.Data != nil {
		if pid, ok := event.Data["publish_id"].(string); ok {
			event.PublishID = pid
		}
	}
	if event.CommentID == "" && event.Data != nil {
		if cid, ok := event.Data["comment_id"].(string); ok {
			event.CommentID = cid
		}
	}

	h.Logger.Info("📥 TikTok webhook event received",
		zap.String("event", event.Event),
		zap.String("video_id", event.VideoID),
		zap.String("publish_id", event.PublishID),
		zap.String("comment_id", event.CommentID),
		zap.Int64("timestamp", event.Timestamp))

	// Process event
	ctx := r.Context()
	if err := h.processEvent(ctx, &event); err != nil {
		h.Logger.Error("failed to process webhook event", zap.Error(err))
		http.Error(w, "Processing failed", http.StatusInternalServerError)
		return
	}

	// Respond with success
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// verifySignature verifies the webhook signature
func (h *TikTokWebhookHandler) verifySignature(body []byte, signature string) bool {
	if h.WebhookSecret == "" || signature == "" {
		return false
	}

	// TikTok uses HMAC-SHA256 for webhook signatures
	mac := hmac.New(sha256.New, []byte(h.WebhookSecret))
	mac.Write(body)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	// Compare signatures (constant-time comparison)
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

// processEvent processes a webhook event
func (h *TikTokWebhookHandler) processEvent(ctx context.Context, event *WebhookEvent) error {
	switch event.Event {
	case "video.status_update", "video.status", "status_update":
		return h.handleVideoStatusUpdate(ctx, event)
	case "comment.created", "comment_created":
		return h.handleCommentCreated(ctx, event)
	case "comment.replied", "comment_replied":
		return h.handleCommentReplied(ctx, event)
	case "video.published", "video_published":
		return h.handleVideoPublished(ctx, event)
	default:
		h.Logger.Debug("unhandled webhook event type", zap.String("event", event.Event))
		return nil // Don't fail on unknown events
	}
}

// handleVideoStatusUpdate handles video status update events
func (h *TikTokWebhookHandler) handleVideoStatusUpdate(ctx context.Context, event *WebhookEvent) error {
	if event.PublishID == "" {
		return fmt.Errorf("publish_id missing in video status update")
	}

	// Find the asset by publish_id (stored in MediaAsset metadata)
	var asset storage.MediaAsset
	if err := h.DB.WithContext(ctx).
		Where("metadata LIKE ?", fmt.Sprintf("%%tiktok_publish_id\":\"%s%%", event.PublishID)).
		Or("metadata LIKE ?", fmt.Sprintf("%%\"tiktok_publish_id\":\"%s\"%%", event.PublishID)).
		First(&asset).Error; err != nil {
		// If not found, log and continue (video might have been posted outside our system)
		h.Logger.Debug("video not found for publish_id", zap.String("publish_id", event.PublishID))
		return nil
	}

	// Extract status from event data
	status := "PROCESSING"
	if event.Data != nil {
		if s, ok := event.Data["status"].(string); ok {
			status = s
		}
	}

	// Update asset metadata with status
	if asset.Metadata == "" {
		asset.Metadata = "{}"
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(asset.Metadata), &metadata); err != nil {
		metadata = make(map[string]interface{})
	}

	metadata["tiktok_status"] = status
	metadata["tiktok_status_updated_at"] = time.Now().UTC().Format(time.RFC3339)
	if event.VideoID != "" {
		metadata["tiktok_video_id"] = event.VideoID
	}

	metadataJSON, _ := json.Marshal(metadata)
	asset.Metadata = string(metadataJSON)

	if err := h.DB.WithContext(ctx).Save(&asset).Error; err != nil {
		return fmt.Errorf("failed to update asset status: %w", err)
	}

	h.Logger.Info("✅ Video status updated via webhook",
		zap.Uint("asset_id", asset.ID),
		zap.String("publish_id", event.PublishID),
		zap.String("status", status),
		zap.String("video_id", event.VideoID))

	return nil
}

// handleCommentCreated handles new comment events
func (h *TikTokWebhookHandler) handleCommentCreated(ctx context.Context, event *WebhookEvent) error {
	if event.VideoID == "" || event.CommentID == "" {
		return fmt.Errorf("video_id or comment_id missing in comment event")
	}

	// Find the social account by video_id
	var asset storage.MediaAsset
	if err := h.DB.WithContext(ctx).
		Where("metadata LIKE ?", fmt.Sprintf("%%tiktok_video_id\":\"%s%%", event.VideoID)).
		First(&asset).Error; err != nil {
		h.Logger.Debug("video not found for comment", zap.String("video_id", event.VideoID))
		return nil
	}

	// Log comment event (could be stored in a comments table if needed)
	h.Logger.Info("💬 New comment received via webhook",
		zap.Uint("asset_id", asset.ID),
		zap.String("video_id", event.VideoID),
		zap.String("comment_id", event.CommentID))

	// TODO: Store comment in database if needed
	// TODO: Send notification to tenant

	return nil
}

// handleCommentReplied handles comment reply events
func (h *TikTokWebhookHandler) handleCommentReplied(ctx context.Context, event *WebhookEvent) error {
	h.Logger.Info("💬 Comment reply received via webhook",
		zap.String("comment_id", event.CommentID))

	// TODO: Store reply in database if needed
	// TODO: Send notification to tenant

	return nil
}

// handleVideoPublished handles video published events
func (h *TikTokWebhookHandler) handleVideoPublished(ctx context.Context, event *WebhookEvent) error {
	if event.VideoID == "" {
		return fmt.Errorf("video_id missing in video published event")
	}

	// Find the social account by video_id
	var asset storage.MediaAsset
	if err := h.DB.WithContext(ctx).
		Where("metadata LIKE ?", fmt.Sprintf("%%tiktok_video_id\":\"%s%%", event.VideoID)).
		First(&asset).Error; err != nil {
		h.Logger.Debug("video not found for published event", zap.String("video_id", event.VideoID))
		return nil
	}

	h.Logger.Info("✅ Video published via webhook",
		zap.Uint("asset_id", asset.ID),
		zap.String("video_id", event.VideoID))

	// Update status to PUBLISHED
	return h.handleVideoStatusUpdate(ctx, &WebhookEvent{
		Event:     "video.status_update",
		VideoID:   event.VideoID,
		PublishID: event.PublishID,
		Data: map[string]interface{}{
			"status": "PUBLISHED",
		},
	})
}
