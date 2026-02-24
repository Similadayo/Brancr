package bot

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ProactiveTriggers checks for opportunities to proactively help the user
type ProactiveTriggers struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewProactiveTriggers creates a new proactive triggers checker
func NewProactiveTriggers(db *gorm.DB, logger *zap.Logger) *ProactiveTriggers {
	return &ProactiveTriggers{
		DB:     db,
		Logger: logger,
	}
}

// TriggerResult represents something the bot should proactively mention
type TriggerResult struct {
	Type     string                 // "unscheduled_media", "abandoned_task"
	Message  string                 // The message to show
	Data     map[string]interface{} // Extra data for handling response
	Priority int                    // Higher = more important
}

// CheckTriggers runs all proactive checks and returns any triggers
func (pt *ProactiveTriggers) CheckTriggers(ctx context.Context, tenantID uuid.UUID, chatID int64) []TriggerResult {
	var triggers []TriggerResult

	// Check for abandoned tasks (higher priority)
	if trigger := pt.checkAbandonedTasks(ctx, tenantID, chatID); trigger != nil {
		triggers = append(triggers, *trigger)
	}

	// Check for unscheduled media
	if trigger := pt.checkUnscheduledMedia(ctx, tenantID); trigger != nil {
		triggers = append(triggers, *trigger)
	}

	// Check for unread/pending orders (NEW)
	if trigger := pt.checkUnreadOrders(ctx, tenantID); trigger != nil {
		triggers = append(triggers, *trigger)
	}

	// Sort by priority (highest first)
	// Simple bubble sort since we have few items
	for i := 0; i < len(triggers)-1; i++ {
		for j := i + 1; j < len(triggers); j++ {
			if triggers[j].Priority > triggers[i].Priority {
				triggers[i], triggers[j] = triggers[j], triggers[i]
			}
		}
	}

	return triggers
}

// checkUnscheduledMedia finds uploaded media that hasn't been scheduled
func (pt *ProactiveTriggers) checkUnscheduledMedia(ctx context.Context, tenantID uuid.UUID) *TriggerResult {
	// Look for media uploaded 1-24 hours ago that hasn't been scheduled
	oneHourAgo := time.Now().Add(-1 * time.Hour)
	oneDayAgo := time.Now().Add(-24 * time.Hour)

	var unscheduledAssets []storage.MediaAsset
	pt.DB.Where("tenant_id = ? AND status = ? AND created_at < ? AND created_at > ?",
		tenantID, "uploaded", oneHourAgo, oneDayAgo).
		Order("created_at DESC").
		Limit(5).
		Find(&unscheduledAssets)

	if len(unscheduledAssets) == 0 {
		return nil
	}

	// Filter out assets that already have scheduled posts
	var trueUnscheduled []storage.MediaAsset
	for _, asset := range unscheduledAssets {
		var count int64
		pt.DB.Model(&storage.ScheduledPost{}).
			Where("media_asset_id = ?", asset.ID).
			Count(&count)
		if count == 0 {
			trueUnscheduled = append(trueUnscheduled, asset)
		}
	}

	if len(trueUnscheduled) == 0 {
		return nil
	}

	pt.Logger.Info("📸 Found unscheduled media",
		zap.Stringer("tenant_id", tenantID),
		zap.Int("count", len(trueUnscheduled)))

	// Build friendly message
	var message string
	if len(trueUnscheduled) == 1 {
		message = "📸 By the way, you uploaded a photo earlier that isn't scheduled yet. Want me to help schedule it?"
	} else {
		message = fmt.Sprintf("📸 You have %d uploads from earlier that aren't scheduled yet. Want me to help with those?", len(trueUnscheduled))
	}

	// Collect asset IDs
	var assetIDs []uuid.UUID
	for _, a := range trueUnscheduled {
		assetIDs = append(assetIDs, a.ID)
	}

	return &TriggerResult{
		Type:     "unscheduled_media",
		Message:  message,
		Priority: 5,
		Data: map[string]interface{}{
			"asset_ids": assetIDs,
			"count":     len(trueUnscheduled),
		},
	}
}

// checkAbandonedTasks finds tasks that were started but not completed
func (pt *ProactiveTriggers) checkAbandonedTasks(ctx context.Context, tenantID uuid.UUID, chatID int64) *TriggerResult {
	// Look for active tasks that haven't been updated in 30+ minutes
	thirtyMinsAgo := time.Now().Add(-30 * time.Minute)
	twoDaysAgo := time.Now().Add(-48 * time.Hour)

	var abandonedTask storage.TaskState
	if err := pt.DB.Where("tenant_id = ? AND chat_id = ? AND status = ? AND updated_at < ? AND updated_at > ?",
		tenantID, chatID, "active", thirtyMinsAgo, twoDaysAgo).
		Order("updated_at DESC").
		First(&abandonedTask).Error; err != nil {
		return nil
	}

	pt.Logger.Info("🤔 Found abandoned task",
		zap.Stringer("tenant_id", tenantID),
		zap.Stringer("task_id", abandonedTask.ID),
		zap.String("task_type", abandonedTask.TaskType))

	// Human-readable task descriptions
	taskDescriptions := map[string]string{
		"schedule_post":   "scheduling a post",
		"connect_account": "connecting a social account",
		"update_business": "updating your business info",
		"add_product":     "adding a product",
		"add_offer":       "setting up an offer",
	}

	desc := taskDescriptions[abandonedTask.TaskType]
	if desc == "" {
		desc = "something"
	}

	return &TriggerResult{
		Type:     "abandoned_task",
		Message:  fmt.Sprintf("🤔 Looks like you were %s earlier but didn't finish. Want to pick up where you left off?", desc),
		Priority: 10, // Higher priority than unscheduled media
		Data: map[string]interface{}{
			"task_id":   abandonedTask.ID,
			"task_type": abandonedTask.TaskType,
		},
	}
}

// checkUnreadOrders finds pending orders that need attention
func (pt *ProactiveTriggers) checkUnreadOrders(ctx context.Context, tenantID uuid.UUID) *TriggerResult {
	var count int64
	// Check for orders created in last 24h that are still pending
	twentyFourHoursAgo := time.Now().Add(-24 * time.Hour)

	pt.DB.Model(&storage.Order{}).
		Where("tenant_id = ? AND status = ? AND created_at > ?", tenantID, "pending", twentyFourHoursAgo).
		Count(&count)

	if count == 0 {
		return nil
	}

	pt.Logger.Info("📦 Found unread orders",
		zap.Stringer("tenant_id", tenantID),
		zap.Int64("count", count))

	var message string
	if count == 1 {
		message = "📦 You have a new order waiting for confirmation! Want to view it?"
	} else {
		message = fmt.Sprintf("📦 You have %d new orders waiting for your attention.", count)
	}

	return &TriggerResult{
		Type:     "unread_orders",
		Message:  message,
		Priority: 15, // Highest priority - money is involved!
		Data: map[string]interface{}{
			"count": count,
		},
	}
}

// ShouldShowTrigger determines if we should show a trigger
// (prevents spamming the user with the same trigger)
func (pt *ProactiveTriggers) ShouldShowTrigger(ctx context.Context, tenantID uuid.UUID, chatID int64, triggerType string) bool {
	// Check if we've shown this trigger recently
	key := fmt.Sprintf("trigger_shown:%s:%s", tenantID, triggerType)
	var session storage.GenericSession

	// If session exists and hasn't expired, don't show
	if err := pt.DB.Where("key = ? AND expires_at > ?", key, time.Now()).First(&session).Error; err == nil {
		return false
	}

	// Set cooldown based on trigger type
	cooldown := 24 * time.Hour
	if triggerType == "abandoned_task" {
		cooldown = 4 * time.Hour // Remind more often for abandoned tasks
	} else if triggerType == "unread_orders" {
		cooldown = 1 * time.Hour // Remind hourly for orders
	}

	// Mark as shown (set cooldown)
	expiresAt := time.Now().Add(cooldown)
	session = storage.GenericSession{
		Key:       key,
		Data:      "shown",
		ExpiresAt: &expiresAt,
	}

	// Use Save to create or update
	if err := pt.DB.Save(&session).Error; err != nil {
		pt.Logger.Error("failed to save trigger cooldown", zap.Error(err))
		// Fail safe: show it anyway if DB fails, but log error
		return true
	}

	return true
}

// FormatProactiveMessage creates a friendly proactive message
// Only returns the most important trigger to avoid overwhelming the user
func (pt *ProactiveTriggers) FormatProactiveMessage(triggers []TriggerResult) string {
	if len(triggers) == 0 {
		return ""
	}
	// Return only the highest priority trigger
	return triggers[0].Message
}

// GetTriggerType returns the type of the highest priority trigger
func (pt *ProactiveTriggers) GetTriggerType(triggers []TriggerResult) string {
	if len(triggers) == 0 {
		return ""
	}
	return triggers[0].Type
}

// GetTriggerData returns the data of the highest priority trigger
func (pt *ProactiveTriggers) GetTriggerData(triggers []TriggerResult) map[string]interface{} {
	if len(triggers) == 0 {
		return nil
	}
	return triggers[0].Data
}
