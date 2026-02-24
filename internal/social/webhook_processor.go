package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
)

// WebhookProcessor wraps webhook processing with idempotency, retry, and error handling
type WebhookProcessor struct {
	RetryManager *utils.WebhookRetryManager
	Logger       *zap.Logger
}

// NewWebhookProcessor creates a new webhook processor
func NewWebhookProcessor(rdb *redis.Client, logger *zap.Logger) *WebhookProcessor {
	return &WebhookProcessor{
		RetryManager: utils.NewWebhookRetryManager(rdb, logger),
		Logger:       logger,
	}
}

// ProcessWebhook processes a webhook event with idempotency and retry support
func (wp *WebhookProcessor) ProcessWebhook(
	ctx context.Context,
	platform string,
	eventID string,
	payload json.RawMessage,
	handler func(ctx context.Context, payload json.RawMessage) error,
) error {
	// Check idempotency
	alreadyProcessed, err := wp.RetryManager.CheckIdempotency(ctx, platform, eventID)
	if err != nil {
		wp.Logger.Warn("failed to check idempotency, proceeding anyway",
			zap.String("platform", platform),
			zap.String("event_id", eventID),
			zap.Error(err))
	} else if alreadyProcessed {
		wp.Logger.Info("duplicate webhook event, skipping",
			zap.String("platform", platform),
			zap.String("event_id", eventID))
		return nil // Already processed
	}

	// Process webhook
	err = handler(ctx, payload)
	if err != nil {
		// Schedule retry
		event := &utils.WebhookEvent{
			Platform:  platform,
			EventID:   eventID,
			Payload:   payload,
			Attempt:   0,
			CreatedAt: time.Now(),
		}

		if retryErr := wp.RetryManager.ScheduleRetry(ctx, event, err); retryErr != nil {
			wp.Logger.Error("failed to schedule retry",
				zap.String("platform", platform),
				zap.String("event_id", eventID),
				zap.Error(retryErr))
		}

		return err
	}

	// Mark as processed
	if err := wp.RetryManager.MarkProcessed(ctx, platform, eventID); err != nil {
		wp.Logger.Warn("failed to mark event as processed",
			zap.String("platform", platform),
			zap.String("event_id", eventID),
			zap.Error(err))
	}

	return nil
}

// HandleWebhookWithRetry wraps HTTP webhook handler with retry and error sanitization
func (wp *WebhookProcessor) HandleWebhookWithRetry(
	w http.ResponseWriter,
	r *http.Request,
	platform string,
	extractEventID func(r *http.Request, body []byte) (string, error),
	handler func(ctx context.Context, body []byte) error,
) {
	ctx := context.Background()

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		wp.Logger.Error("failed to read webhook body", zap.Error(err))
		sanitized := utils.SanitizeError(err)
		http.Error(w, sanitized, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Extract event ID
	eventID, err := extractEventID(r, body)
	if err != nil {
		// If we can't extract event ID, generate one from request
		eventID = fmt.Sprintf("%s-%d", r.RemoteAddr, time.Now().UnixNano())
		wp.Logger.Warn("could not extract event ID, using generated ID",
			zap.String("event_id", eventID),
			zap.Error(err))
	}

	// Process with retry support
	err = wp.ProcessWebhook(ctx, platform, eventID, body, func(ctx context.Context, payload json.RawMessage) error {
		return handler(ctx, payload)
	})

	if err != nil {
		wp.Logger.Error("webhook processing failed",
			zap.String("platform", platform),
			zap.String("event_id", eventID),
			zap.Error(err))

		// Return sanitized error
		sanitized := utils.SanitizeError(err)
		http.Error(w, sanitized, http.StatusInternalServerError)
		return
	}

	// Success
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// ProcessRetryEvent processes a retry event
func (wp *WebhookProcessor) ProcessRetryEvent(
	ctx context.Context,
	event *utils.WebhookEvent,
	handler func(ctx context.Context, payload json.RawMessage) error,
) error {
	wp.Logger.Info("processing retry event",
		zap.String("platform", event.Platform),
		zap.String("event_id", event.EventID),
		zap.Int("attempt", event.Attempt))

	err := handler(ctx, event.Payload)
	if err != nil {
		// Schedule another retry
		if retryErr := wp.RetryManager.ScheduleRetry(ctx, event, err); retryErr != nil {
			wp.Logger.Error("failed to schedule retry",
				zap.String("platform", event.Platform),
				zap.String("event_id", event.EventID),
				zap.Error(retryErr))
		}
		return err
	}

	// Success - mark as processed
	if err := wp.RetryManager.MarkProcessed(ctx, event.Platform, event.EventID); err != nil {
		wp.Logger.Warn("failed to mark retry event as processed",
			zap.String("platform", event.Platform),
			zap.String("event_id", event.EventID),
			zap.Error(err))
	}

	return nil
}

