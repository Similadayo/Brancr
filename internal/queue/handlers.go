package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// WebhookPayload represents a webhook processing job payload
type WebhookPayload struct {
	Platform  string          `json:"platform"` // meta, gupshup, respondio
	TenantID  uint            `json:"tenant_id,omitempty"`
	EventID   string          `json:"event_id"`
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
}

// PostExecutionPayload represents a post execution job payload
type PostExecutionPayload struct {
	PostID   uint `json:"post_id"`
	TenantID uint `json:"tenant_id"`
}

// MediaUploadPayload represents a media upload job payload
type MediaUploadPayload struct {
	AssetID  uint   `json:"asset_id"`
	TenantID uint   `json:"tenant_id"`
	Platform string `json:"platform"`
}

// AIGenerateReplyPayload represents an AI reply generation job payload
type AIGenerateReplyPayload struct {
	TenantID      uint   `json:"tenant_id"`
	Platform      string `json:"platform"`
	ThreadID      string `json:"thread_id"`
	CustomerID    string `json:"customer_id"`
	Message       string `json:"message"`
	InteractionID uint   `json:"interaction_id,omitempty"`
}

// RegisterWebhookHandlers registers webhook processing handlers
func RegisterWebhookHandlers(queue *JobQueue, db *gorm.DB, logger *zap.Logger, handlers map[string]func(context.Context, *WebhookPayload) error) {
	rdb := queue.GetRedisClient()
	for platform, handler := range handlers {
		queue.RegisterHandler(JobType(fmt.Sprintf("webhook:%s", platform)), asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			var payload WebhookPayload
			if err := json.Unmarshal(task.Payload(), &payload); err != nil {
				logger.Error("failed to unmarshal webhook payload", zap.Error(err))
				return err
			}

			// Check idempotency
			idempotencyKey := fmt.Sprintf("webhook:idempotency:%s:%s", payload.Platform, payload.EventID)
			if rdb != nil {
				exists, _ := rdb.Exists(ctx, idempotencyKey).Result()
				if exists > 0 {
					logger.Info("duplicate webhook event, skipping",
						zap.String("platform", payload.Platform),
						zap.String("event_id", payload.EventID))
					return nil // Already processed
				}
				// Mark as processed (24 hour TTL)
				rdb.Set(ctx, idempotencyKey, "1", 24*time.Hour)
			}

			return handler(ctx, &payload)
		}))
	}
}

// RegisterPostExecutionHandler registers post execution handler
func RegisterPostExecutionHandler(queue *JobQueue, db *gorm.DB, logger *zap.Logger, handler func(context.Context, *PostExecutionPayload) error) {
	queue.RegisterHandler(JobTypePostExecution, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		var payload PostExecutionPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			logger.Error("failed to unmarshal post execution payload", zap.Error(err))
			return err
		}
		return handler(ctx, &payload)
	}))
}

// RegisterMediaUploadHandler registers media upload handler
func RegisterMediaUploadHandler(queue *JobQueue, db *gorm.DB, logger *zap.Logger, handler func(context.Context, *MediaUploadPayload) error) {
	queue.RegisterHandler(JobTypeMediaUpload, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		var payload MediaUploadPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			logger.Error("failed to unmarshal media upload payload", zap.Error(err))
			return err
		}
		return handler(ctx, &payload)
	}))
}

// RegisterAIGenerateReplyHandler registers AI reply generation handler
func RegisterAIGenerateReplyHandler(queue *JobQueue, db *gorm.DB, logger *zap.Logger, handler func(context.Context, *AIGenerateReplyPayload) error) {
	queue.RegisterHandler(JobTypeAIGenerateReply, asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		var payload AIGenerateReplyPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			logger.Error("failed to unmarshal AI reply payload", zap.Error(err))
			return err
		}
		return handler(ctx, &payload)
	}))
}
