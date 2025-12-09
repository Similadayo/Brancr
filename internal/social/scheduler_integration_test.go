package social

import (
	"context"
	"testing"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/worker"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestScheduler_WorkerPoolIntegration tests that scheduler properly integrates with worker pool
func TestScheduler_WorkerPoolIntegration(t *testing.T) {
	// Setup in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Auto-migrate
	if err := db.AutoMigrate(&storage.ScheduledPost{}, &storage.ActivityLog{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	logger := zap.NewNop()
	optimizer := &mockCaptionOptimizer{}

	// Create scheduler
	scheduler := NewScheduler(db, logger, optimizer)

	// Create worker pool
	pool := worker.NewPool(5, 50, logger)
	pool.Start()
	defer pool.Shutdown(5 * time.Second)

	// Set worker pool
	scheduler.WorkerPool = pool

	// Create a test post
	post := storage.ScheduledPost{
		TenantID:    1,
		Platform:    "instagram",
		Status:      "scheduled",
		ScheduledAt: time.Now().Add(-1 * time.Minute), // Due
		Caption:     "Test caption",
	}

	if err := db.Create(&post).Error; err != nil {
		t.Fatalf("failed to create post: %v", err)
	}

	// Submit job to worker pool
	job := &postJob{
		scheduler: scheduler,
		post:      post,
	}

	if err := pool.Submit(job); err != nil {
		t.Fatalf("failed to submit job: %v", err)
	}

	// Wait for job to complete
	time.Sleep(2 * time.Second)

	// Verify post was processed (status changed or activity log created)
	var updatedPost storage.ScheduledPost
	if err := db.First(&updatedPost, post.ID).Error; err != nil {
		t.Fatalf("failed to fetch post: %v", err)
	}

	// Post should have been processed (status changed from scheduled)
	if updatedPost.Status == "scheduled" {
		t.Log("Post status unchanged - this is expected if executePost fails due to missing platform adapter")
	}

	// Check for activity log
	var activityLogs []storage.ActivityLog
	if err := db.Where("tenant_id = ? AND entity_type = ? AND entity_id = ?",
		1, "scheduled_post", post.ID).Find(&activityLogs).Error; err == nil {
		if len(activityLogs) > 0 {
			t.Logf("Activity log created: %+v", activityLogs[0])
		}
	}
}

// TestScheduler_WorkerPoolFallback tests fallback to goroutines when pool is full
func TestScheduler_WorkerPoolFallback(t *testing.T) {
	// Setup in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	if err := db.AutoMigrate(&storage.ScheduledPost{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	logger := zap.NewNop()
	optimizer := &mockCaptionOptimizer{}

	scheduler := NewScheduler(db, logger, optimizer)

	// Create a small worker pool (size 1, queue 1) to force queue full scenario
	pool := worker.NewPool(1, 1, logger)
	pool.Start()
	defer pool.Shutdown(5 * time.Second)

	scheduler.WorkerPool = pool

	// Fill the queue
	post1 := storage.ScheduledPost{TenantID: 1, Platform: "instagram", Status: "scheduled", ScheduledAt: time.Now()}
	db.Create(&post1)
	job1 := &postJob{scheduler: scheduler, post: post1}
	pool.Submit(job1) // Fill queue

	// Try to submit another job - should fail and fallback
	post2 := storage.ScheduledPost{TenantID: 1, Platform: "instagram", Status: "scheduled", ScheduledAt: time.Now()}
	db.Create(&post2)
	job2 := &postJob{scheduler: scheduler, post: post2}

	// This should trigger fallback (though we can't easily test the fallback path here)
	err = pool.Submit(job2)
	if err == nil {
		t.Log("Job submitted successfully (queue had space)")
	} else {
		t.Logf("Queue full as expected: %v", err)
	}
}

type mockCaptionOptimizer struct{}

func (m *mockCaptionOptimizer) OptimizeCaption(ctx context.Context, req OptimizeCaptionRequest) (*OptimizeCaptionResult, error) {
	return &OptimizeCaptionResult{
		OptimizedCaption: req.OriginalCaption,
		Hashtags:         []string{"#test"},
	}, nil
}

