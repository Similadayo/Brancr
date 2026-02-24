package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// WebhookRetryManager handles retry logic for webhook processing
type WebhookRetryManager struct {
	Redis    *redis.Client
	Logger   *zap.Logger
	MaxRetries int
	BaseDelay time.Duration
}

// NewWebhookRetryManager creates a new retry manager
func NewWebhookRetryManager(rdb *redis.Client, logger *zap.Logger) *WebhookRetryManager {
	return &WebhookRetryManager{
		Redis:      rdb,
		Logger:     logger,
		MaxRetries: 3,
		BaseDelay:  time.Second,
	}
}

// WebhookEvent represents a webhook event to be retried
type WebhookEvent struct {
	Platform    string          `json:"platform"`
	EventID     string          `json:"event_id"`
	Payload     json.RawMessage `json:"payload"`
	Attempt     int             `json:"attempt"`
	LastError   string          `json:"last_error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// RetryKey returns the Redis key for a retry attempt
func (w *WebhookRetryManager) RetryKey(platform, eventID string, attempt int) string {
	return fmt.Sprintf("webhook:retry:%s:%s:%d", platform, eventID, attempt)
}

// DeadLetterKey returns the Redis key for dead-letter queue
func (w *WebhookRetryManager) DeadLetterKey(platform, eventID string) string {
	return fmt.Sprintf("webhook:dlq:%s:%s", platform, eventID)
}

// IdempotencyKey returns the Redis key for idempotency check
func (w *WebhookRetryManager) IdempotencyKey(platform, eventID string) string {
	return fmt.Sprintf("webhook:idempotency:%s:%s", platform, eventID)
}

// CheckIdempotency checks if an event has already been processed
func (w *WebhookRetryManager) CheckIdempotency(ctx context.Context, platform, eventID string) (bool, error) {
	if w.Redis == nil {
		return false, nil // No Redis, skip idempotency check
	}

	key := w.IdempotencyKey(platform, eventID)
	exists, err := w.Redis.Exists(ctx, key).Result()
	if err != nil {
		w.Logger.Warn("failed to check idempotency", zap.Error(err))
		return false, err
	}

	return exists > 0, nil
}

// MarkProcessed marks an event as processed (24 hour TTL)
func (w *WebhookRetryManager) MarkProcessed(ctx context.Context, platform, eventID string) error {
	if w.Redis == nil {
		return nil // No Redis, skip
	}

	key := w.IdempotencyKey(platform, eventID)
	return w.Redis.Set(ctx, key, "1", 24*time.Hour).Err()
}

// ScheduleRetry schedules a webhook event for retry with exponential backoff
func (w *WebhookRetryManager) ScheduleRetry(ctx context.Context, event *WebhookEvent, err error) error {
	if w.Redis == nil {
		w.Logger.Warn("Redis not available, cannot schedule retry", zap.String("platform", event.Platform))
		return fmt.Errorf("retry mechanism unavailable")
	}

	if event.Attempt >= w.MaxRetries {
		// Move to dead-letter queue
		return w.MoveToDeadLetter(ctx, event, err)
	}

	event.Attempt++
	if err != nil {
		event.LastError = err.Error()
	}

	// Calculate exponential backoff delay
	delay := w.BaseDelay * time.Duration(1<<uint(event.Attempt-1))
	if delay > 5*time.Minute {
		delay = 5 * time.Minute // Cap at 5 minutes
	}

	// Serialize event
	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Store in Redis with delay
	key := w.RetryKey(event.Platform, event.EventID, event.Attempt)
	if err := w.Redis.Set(ctx, key, eventData, delay+time.Hour).Err(); err != nil {
		return fmt.Errorf("failed to schedule retry: %w", err)
	}

	// Schedule retry using Redis delayed execution (using sorted set with score = timestamp)
	retryTime := time.Now().Add(delay)
	zsetKey := "webhook:retries:pending"
	score := float64(retryTime.Unix())
	member := fmt.Sprintf("%s:%s:%d", event.Platform, event.EventID, event.Attempt)

	if err := w.Redis.ZAdd(ctx, zsetKey, redis.Z{
		Score:  score,
		Member: member,
	}).Err(); err != nil {
		return fmt.Errorf("failed to add to retry queue: %w", err)
	}

	w.Logger.Info("webhook retry scheduled",
		zap.String("platform", event.Platform),
		zap.String("event_id", event.EventID),
		zap.Int("attempt", event.Attempt),
		zap.Duration("delay", delay))

	return nil
}

// MoveToDeadLetter moves a failed event to the dead-letter queue
func (w *WebhookRetryManager) MoveToDeadLetter(ctx context.Context, event *WebhookEvent, err error) error {
	if w.Redis == nil {
		w.Logger.Warn("Redis not available, cannot move to dead-letter queue")
		return fmt.Errorf("dead-letter queue unavailable")
	}

	if err != nil {
		event.LastError = err.Error()
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	key := w.DeadLetterKey(event.Platform, event.EventID)
	// Store in dead-letter queue with 7 day TTL
	if err := w.Redis.Set(ctx, key, eventData, 7*24*time.Hour).Err(); err != nil {
		return fmt.Errorf("failed to move to dead-letter queue: %w", err)
	}

	// Also add to a list for easy retrieval
	dlqListKey := "webhook:dlq:list"
	if err := w.Redis.LPush(ctx, dlqListKey, key).Err(); err != nil {
		w.Logger.Warn("failed to add to DLQ list", zap.Error(err))
	}

	w.Logger.Error("webhook moved to dead-letter queue",
		zap.String("platform", event.Platform),
		zap.String("event_id", event.EventID),
		zap.Int("attempts", event.Attempt),
		zap.Error(err))

	return nil
}

// GetPendingRetries returns events ready for retry
func (w *WebhookRetryManager) GetPendingRetries(ctx context.Context, limit int) ([]*WebhookEvent, error) {
	if w.Redis == nil {
		return nil, nil
	}

	now := time.Now().Unix()
	zsetKey := "webhook:retries:pending"

	// Get events with score <= now (ready for retry)
	members, err := w.Redis.ZRangeByScore(ctx, zsetKey, &redis.ZRangeBy{
		Min:   "0",
		Max:   fmt.Sprintf("%d", now),
		Count: int64(limit),
	}).Result()

	if err != nil {
		return nil, fmt.Errorf("failed to get pending retries: %w", err)
	}

	var events []*WebhookEvent
	for _, member := range members {
		// Parse member: platform:event_id:attempt
		parts := splitMember(member)
		if len(parts) != 3 {
			continue
		}

		key := w.RetryKey(parts[0], parts[1], parseInt(parts[2]))
		data, err := w.Redis.Get(ctx, key).Result()
		if err != nil {
			if err == redis.Nil {
				// Event expired, remove from sorted set
				w.Redis.ZRem(ctx, zsetKey, member)
			}
			continue
		}

		var event WebhookEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			w.Logger.Warn("failed to unmarshal retry event", zap.Error(err))
			continue
		}

		events = append(events, &event)

		// Remove from sorted set
		w.Redis.ZRem(ctx, zsetKey, member)
	}

	return events, nil
}

// Helper functions
func splitMember(member string) []string {
	return strings.Split(member, ":")
}

func parseInt(s string) int {
	val, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return val
}

