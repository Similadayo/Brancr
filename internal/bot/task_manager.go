package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ConversationalTaskManager handles multi-turn task orchestration
// It tracks active tasks, manages slot-filling, and executes completed tasks
type ConversationalTaskManager struct {
	DB      *gorm.DB
	Logger  *zap.Logger
	Memory  *ai.MemoryService
	Manager *TenantBotManager
}

// TaskSlot represents a piece of information needed to complete a task
type TaskSlot struct {
	Name         string   `json:"name"`         // "platform", "schedule_time", "caption"
	Value        string   `json:"value"`        // filled value or empty
	Required     bool     `json:"required"`     // must be filled to complete task
	Prompt       string   `json:"prompt"`       // natural language prompt to ask for this slot
	Alternatives []string `json:"alternatives"` // suggested values for quick selection
}

// ActiveTask represents an ongoing conversational task
type ActiveTask struct {
	ID          uuid.UUID              `json:"id"`
	TaskType    string                 `json:"task_type"` // "schedule_post", "connect_account", etc.
	Slots       []TaskSlot             `json:"slots"`
	Context     map[string]interface{} `json:"context"` // extra data (media_asset_id, etc.)
	StartedAt   time.Time              `json:"started_at"`
	LastUpdated time.Time              `json:"last_updated"`
}

// NewConversationalTaskManager creates a new task manager
func NewConversationalTaskManager(db *gorm.DB, logger *zap.Logger, memory *ai.MemoryService, manager *TenantBotManager) *ConversationalTaskManager {
	return &ConversationalTaskManager{
		DB:      db,
		Logger:  logger,
		Memory:  memory,
		Manager: manager,
	}
}

// GetActiveTask retrieves the current active task for a chat
func (tm *ConversationalTaskManager) GetActiveTask(ctx context.Context, tenantID uuid.UUID, chatID int64) (*ActiveTask, bool) {
	var state storage.TaskState
	if err := tm.DB.Where("tenant_id = ? AND chat_id = ? AND status = ?", tenantID, chatID, "active").
		Order("updated_at DESC").
		First(&state).Error; err != nil {
		return nil, false
	}

	var task ActiveTask
	if err := json.Unmarshal([]byte(state.TaskData), &task); err != nil {
		tm.Logger.Warn("failed to unmarshal task data", zap.Error(err), zap.Stringer("task_id", state.ID))
		return nil, false
	}
	task.ID = state.ID
	return &task, true
}

// StartTask begins a new conversational task
func (tm *ConversationalTaskManager) StartTask(ctx context.Context, tenantID uuid.UUID, chatID int64, taskType string, slots []TaskSlot, taskContext map[string]interface{}) (*ActiveTask, error) {
	// Abandon any existing active task for this chat
	tm.DB.Model(&storage.TaskState{}).
		Where("tenant_id = ? AND chat_id = ? AND status = ?", tenantID, chatID, "active").
		Update("status", "abandoned")

	task := ActiveTask{
		TaskType:    taskType,
		Slots:       slots,
		Context:     taskContext,
		StartedAt:   time.Now(),
		LastUpdated: time.Now(),
	}

	taskJSON, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task: %w", err)
	}

	state := storage.TaskState{
		TenantID: tenantID,
		ChatID:   chatID,
		TaskType: taskType,
		TaskData: string(taskJSON),
		Status:   "active",
	}

	if err := tm.DB.Create(&state).Error; err != nil {
		return nil, fmt.Errorf("failed to create task state: %w", err)
	}

	task.ID = state.ID
	tm.Logger.Info("🎯 Started new conversational task",
		zap.Stringer("tenant_id", tenantID),
		zap.String("task_type", taskType),
		zap.Stringer("task_id", state.ID))

	return &task, nil
}

// FillSlot updates a slot value in the active task
func (tm *ConversationalTaskManager) FillSlot(ctx context.Context, taskID uuid.UUID, slotName, value string) error {
	var state storage.TaskState
	if err := tm.DB.First(&state, taskID).Error; err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	var task ActiveTask
	if err := json.Unmarshal([]byte(state.TaskData), &task); err != nil {
		return fmt.Errorf("failed to unmarshal task: %w", err)
	}

	// Find and fill the slot
	slotFound := false
	for i := range task.Slots {
		if task.Slots[i].Name == slotName {
			task.Slots[i].Value = value
			slotFound = true
			tm.Logger.Info("📝 Filled task slot",
				zap.Stringer("task_id", taskID),
				zap.String("slot", slotName),
				zap.String("value", value))
			break
		}
	}

	if !slotFound {
		return fmt.Errorf("slot %s not found in task", slotName)
	}

	task.LastUpdated = time.Now()
	taskJSON, _ := json.Marshal(task)

	return tm.DB.Model(&state).Updates(map[string]interface{}{
		"task_data":  string(taskJSON),
		"updated_at": time.Now(),
	}).Error
}

// FillSlotByIndex fills a slot by its position (for when user responds to current prompt)
func (tm *ConversationalTaskManager) FillNextMissingSlot(ctx context.Context, taskID uuid.UUID, value string) (string, error) {
	var state storage.TaskState
	if err := tm.DB.First(&state, taskID).Error; err != nil {
		return "", fmt.Errorf("task not found: %w", err)
	}

	var task ActiveTask
	if err := json.Unmarshal([]byte(state.TaskData), &task); err != nil {
		return "", fmt.Errorf("failed to unmarshal task: %w", err)
	}

	// Find the first missing required slot and fill it
	for i := range task.Slots {
		if task.Slots[i].Required && task.Slots[i].Value == "" {
			task.Slots[i].Value = value
			task.LastUpdated = time.Now()
			taskJSON, _ := json.Marshal(task)

			tm.DB.Model(&state).Updates(map[string]interface{}{
				"task_data":  string(taskJSON),
				"updated_at": time.Now(),
			})

			tm.Logger.Info("📝 Filled next missing slot",
				zap.Stringer("task_id", taskID),
				zap.String("slot", task.Slots[i].Name),
				zap.String("value", value))

			return task.Slots[i].Name, nil
		}
	}

	return "", fmt.Errorf("no missing slots to fill")
}

// GetMissingSlots returns slots that still need values
func (tm *ConversationalTaskManager) GetMissingSlots(task *ActiveTask) []TaskSlot {
	var missing []TaskSlot
	for _, slot := range task.Slots {
		if slot.Required && slot.Value == "" {
			missing = append(missing, slot)
		}
	}
	return missing
}

// GetFilledSlots returns all slots that have values
func (tm *ConversationalTaskManager) GetFilledSlots(task *ActiveTask) map[string]string {
	filled := make(map[string]string)
	for _, slot := range task.Slots {
		if slot.Value != "" {
			filled[slot.Name] = slot.Value
		}
	}
	return filled
}

// IsTaskComplete checks if all required slots are filled
func (tm *ConversationalTaskManager) IsTaskComplete(task *ActiveTask) bool {
	return len(tm.GetMissingSlots(task)) == 0
}

// CompleteTask marks a task as completed
func (tm *ConversationalTaskManager) CompleteTask(ctx context.Context, taskID uuid.UUID) error {
	tm.Logger.Info("✅ Task completed", zap.Stringer("task_id", taskID))
	return tm.DB.Model(&storage.TaskState{}).Where("id = ?", taskID).
		Updates(map[string]interface{}{
			"status":     "completed",
			"updated_at": time.Now(),
		}).Error
}

// AbandonTask marks a task as abandoned
func (tm *ConversationalTaskManager) AbandonTask(ctx context.Context, taskID uuid.UUID) error {
	tm.Logger.Info("⏹️ Task abandoned", zap.Stringer("task_id", taskID))
	return tm.DB.Model(&storage.TaskState{}).Where("id = ?", taskID).
		Updates(map[string]interface{}{
			"status":     "abandoned",
			"updated_at": time.Now(),
		}).Error
}

// UpdateTaskContext updates the context data of a task
func (tm *ConversationalTaskManager) UpdateTaskContext(ctx context.Context, taskID uuid.UUID, key string, value interface{}) error {
	var state storage.TaskState
	if err := tm.DB.First(&state, taskID).Error; err != nil {
		return err
	}

	var task ActiveTask
	json.Unmarshal([]byte(state.TaskData), &task)

	if task.Context == nil {
		task.Context = make(map[string]interface{})
	}
	task.Context[key] = value
	task.LastUpdated = time.Now()

	taskJSON, _ := json.Marshal(task)
	return tm.DB.Model(&state).Update("task_data", string(taskJSON)).Error
}

// GetNextPrompt returns the prompt for the next missing slot
func (tm *ConversationalTaskManager) GetNextPrompt(task *ActiveTask) (string, []string) {
	missing := tm.GetMissingSlots(task)
	if len(missing) == 0 {
		return "", nil
	}
	return missing[0].Prompt, missing[0].Alternatives
}

// BuildTaskSummary creates a human-readable summary of the task state
func (tm *ConversationalTaskManager) BuildTaskSummary(task *ActiveTask) string {
	var parts []string
	for _, slot := range task.Slots {
		if slot.Value != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", slot.Name, slot.Value))
		}
	}
	if len(parts) == 0 {
		return "No details yet"
	}
	return strings.Join(parts, ", ")
}

// CleanupOldTasks marks very old active tasks as abandoned (for maintenance)
func (tm *ConversationalTaskManager) CleanupOldTasks(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result := tm.DB.Model(&storage.TaskState{}).
		Where("status = ? AND updated_at < ?", "active", cutoff).
		Update("status", "abandoned")
	return result.RowsAffected, result.Error
}
