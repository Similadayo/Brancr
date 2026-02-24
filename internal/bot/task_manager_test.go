package bot

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// migrateTaskManagerTestModels migrates the DB for task manager tests
func migrateTaskManagerTestModels(t *testing.T, db *gorm.DB) {
	err := db.AutoMigrate(
		&storage.TaskState{},
	)
	if err != nil {
		t.Fatalf("failed to migrate test models: %v", err)
	}
}

func TestConversationalTaskManager_StartTask(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)
	migrateTaskManagerTestModels(t, db)

	logger := zap.NewNop()
	manager := &TenantBotManager{DB: db} // Dummy manager
	tm := NewConversationalTaskManager(db, logger, nil, manager)

	tenantID := uuid.New()
	chatID := int64(12345)

	slots := GetSchedulePostSlots()

	task, err := tm.StartTask(context.Background(), tenantID, chatID, "schedule_post", slots, nil)
	if err != nil {
		t.Fatalf("StartTask failed: %v", err)
	}

	// Verify DB state
	var state storage.TaskState
	if err := db.First(&state, task.ID).Error; err != nil {
		t.Fatalf("failed to find task state: %v", err)
	}
	if state.Status != "active" {
		t.Errorf("expected status 'active', got %s", state.Status)
	}
	if state.TaskType != "schedule_post" {
		t.Errorf("expected task type 'schedule_post', got %s", state.TaskType)
	}
}

func TestConversationalTaskManager_SlotFilling(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)
	migrateTaskManagerTestModels(t, db)

	logger := zap.NewNop()
	tm := NewConversationalTaskManager(db, logger, nil, nil)
	tenantID := uuid.New()
	chatID := int64(12345)

	// Start task
	slots := []TaskSlot{
		{Name: "platform", Required: true},
		{Name: "time", Required: true},
	}
	task, _ := tm.StartTask(context.Background(), tenantID, chatID, "test_task", slots, nil)

	// Fill first slot
	err := tm.FillSlot(context.Background(), task.ID, "platform", "instagram")
	if err != nil {
		t.Fatalf("FillSlot failed: %v", err)
	}

	// Verify slot filled
	updatedTask, _ := tm.GetActiveTask(context.Background(), tenantID, chatID)
	filled := tm.GetFilledSlots(updatedTask)
	if filled["platform"] != "instagram" {
		t.Errorf("expected platform='instagram', got '%s'", filled["platform"])
	}

	// Check missing slots
	missing := tm.GetMissingSlots(updatedTask)
	if len(missing) != 1 {
		t.Errorf("expected 1 missing slot, got %d", len(missing))
	}
	if missing[0].Name != "time" {
		t.Errorf("expected missing slot 'time', got '%s'", missing[0].Name)
	}

	// Fill remaining slot
	tm.FillSlot(context.Background(), task.ID, "time", "tomorrow")
	updatedTask, _ = tm.GetActiveTask(context.Background(), tenantID, chatID)

	if !tm.IsTaskComplete(updatedTask) {
		t.Error("expected task to be complete")
	}
}

func TestConversationalTaskManager_Abandonment(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)
	migrateTaskManagerTestModels(t, db)

	logger := zap.NewNop()
	tm := NewConversationalTaskManager(db, logger, nil, nil)
	tenantID := uuid.New()
	chatID := int64(12345)

	task, _ := tm.StartTask(context.Background(), tenantID, chatID, "test_task", nil, nil)

	// Abandon task
	if err := tm.AbandonTask(context.Background(), task.ID); err != nil {
		t.Fatalf("AbandonTask failed: %v", err)
	}

	// Verify status
	var state storage.TaskState
	db.First(&state, task.ID)
	if state.Status != "abandoned" {
		t.Errorf("expected status 'abandoned', got %s", state.Status)
	}

	// Verify GetActiveTask returns nothing
	_, found := tm.GetActiveTask(context.Background(), tenantID, chatID)
	if found {
		t.Error("expected no active task found")
	}
}
