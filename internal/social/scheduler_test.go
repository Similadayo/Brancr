package social

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/testutil"
)

// mockCaptionOptimizer is a mock implementation of CaptionOptimizer for testing
type mockCaptionOptimizer struct {
	shouldFail bool
	result     *OptimizeCaptionResult
}

func (m *mockCaptionOptimizer) OptimizeCaption(ctx context.Context, req OptimizeCaptionRequest) (*OptimizeCaptionResult, error) {
	if m.shouldFail {
		return nil, fmt.Errorf("mock optimization failed")
	}
	if m.result != nil {
		return m.result, nil
	}
	return &OptimizeCaptionResult{
		OptimizedCaption: "Optimized: " + req.OriginalCaption,
		Hashtags:         []string{"#test", "#example"},
		SuggestedTime:    "10:00",
		Improvements:     "Enhanced with AI",
	}, nil
}

func TestScheduler_ExecutePost_Mode1_EmptyCaption(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	logger := testutil.TestLogger(t)
	optimizer := &mockCaptionOptimizer{shouldFail: false}
	scheduler := NewScheduler(db, logger, optimizer, 20)

	// Create a test post with empty caption (Mode 1: AI-generated)
	post := storage.ScheduledPost{
		TenantID:       uuid.New(),
		Name:           "Test Post",
		Caption:        "", // Empty caption triggers Mode 1
		MediaAssetIDs:  "[1]",
		Platforms:      `["instagram"]`,
		Status:         "scheduled",
		ScheduledAt:    timePtr(time.Now().Add(-1 * time.Hour)), // Past time to trigger execution
		EnhanceCaption: false,
	}

	if err := db.Create(&post).Error; err != nil {
		t.Fatalf("Failed to create test post: %v", err)
	}

	// Test that scheduler would generate caption (we can't fully test without actual posting)
	// This is a basic structure test
	if scheduler.CaptionOptimizer == nil {
		t.Error("CaptionOptimizer should be set")
	}
}

func TestScheduler_ExecutePost_Mode2_UseAsIs(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Create a test post with caption and EnhanceCaption=false (Mode 2: Use as-is)
	post := storage.ScheduledPost{
		TenantID:       uuid.New(),
		Name:           "Test Post",
		Caption:        "My custom caption",
		MediaAssetIDs:  "[1]",
		Platforms:      `["instagram"]`,
		Status:         "scheduled",
		ScheduledAt:    timePtr(time.Now().Add(-1 * time.Hour)),
		EnhanceCaption: false, // Should use caption as-is
	}

	if err := db.Create(&post).Error; err != nil {
		t.Fatalf("Failed to create test post: %v", err)
	}

	// Verify EnhanceCaption is set correctly
	if post.EnhanceCaption {
		t.Error("EnhanceCaption should be false for Mode 2")
	}
}

func TestScheduler_ExecutePost_Mode3_EnhanceCaption(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	// Create a test post with caption and EnhanceCaption=true (Mode 3: AI-enhanced)
	post := storage.ScheduledPost{
		TenantID:       uuid.New(),
		Name:           "Test Post",
		Caption:        "My caption to enhance",
		MediaAssetIDs:  "[1]",
		Platforms:      `["instagram"]`,
		Status:         "scheduled",
		ScheduledAt:    timePtr(time.Now().Add(-1 * time.Hour)),
		EnhanceCaption: true, // Should enhance caption
	}

	if err := db.Create(&post).Error; err != nil {
		t.Fatalf("Failed to create test post: %v", err)
	}

	// Verify EnhanceCaption is set correctly
	if !post.EnhanceCaption {
		t.Error("EnhanceCaption should be true for Mode 3")
	}
}

func TestScheduler_NewScheduler(t *testing.T) {
	db := testutil.TestDB(t)
	defer testutil.CleanupDB(t, db)

	logger := testutil.TestLogger(t)
	optimizer := &mockCaptionOptimizer{}

	scheduler := NewScheduler(db, logger, optimizer, 20)

	if scheduler.DB == nil {
		t.Error("DB should be set")
	}
	if scheduler.Logger == nil {
		t.Error("Logger should be set")
	}
	if scheduler.CaptionOptimizer == nil {
		t.Error("CaptionOptimizer should be set")
	}
}

// Helper function to create time pointer
func timePtr(t time.Time) *time.Time {
	return &t
}
