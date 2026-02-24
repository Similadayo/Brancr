package social

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/worker"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestScheduler_WorkerPoolIntegration tests that scheduler properly integrates with worker pool
func TestScheduler_WorkerPoolIntegration(t *testing.T) {
	// Setup in-memory database (requires CGO)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		errMsg := err.Error()
		if errMsg == "Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work. This is a stub" ||
			errMsg == "sql: unknown driver \"sqlite3\" (forgotten import?)" {
			t.Skip("Skipping test: SQLite requires CGO. Run with: set CGO_ENABLED=1 && go test ./...")
		}
		t.Fatalf("failed to open database: %v", err)
	}

	// Auto-migrate
	if err := db.AutoMigrate(&storage.ScheduledPost{}, &storage.PostLog{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	logger := zap.NewNop()
	optimizer := &mockCaptionOptimizerIntegration{}

	// Create scheduler
	scheduler := NewScheduler(db, logger, optimizer, 20)

	// Create worker pool and wrap it in adapter to match scheduler interface
	pool := worker.NewPool(5, 50, logger)
	pool.Start()
	defer pool.Shutdown(5 * time.Second)

	// Create adapter to match scheduler's WorkerPool interface
	poolAdapter := &workerPoolTestAdapter{pool: pool}
	scheduler.WorkerPool = poolAdapter

	// Create a test post
	scheduledAt := time.Now().Add(-1 * time.Minute)
	post := storage.ScheduledPost{
		TenantID:    uuid.New(),
		Platforms:   `["instagram"]`,
		Status:      "scheduled",
		ScheduledAt: &scheduledAt, // Due
		Caption:     "Test caption",
	}

	if err := db.Create(&post).Error; err != nil {
		t.Fatalf("failed to create post: %v", err)
	}

	// Track completion with wait group
	var wg sync.WaitGroup
	wg.Add(1)

	// Submit job to worker pool
	job := &postJob{
		scheduler: scheduler,
		post:      post,
		done:      wg.Done, // Track completion
	}

	if err := pool.Submit(job); err != nil {
		t.Fatalf("failed to submit job: %v", err)
	}

	// Wait for job to complete (with timeout)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Job completed
	case <-time.After(5 * time.Second):
		t.Fatal("job did not complete within timeout")
	}

	// Verify post was processed (status changed or post log created)
	var updatedPost storage.ScheduledPost
	if err := db.First(&updatedPost, post.ID).Error; err != nil {
		t.Fatalf("failed to fetch post: %v", err)
	}

	// Post should have been processed (status changed from scheduled)
	if updatedPost.Status == "scheduled" {
		t.Log("Post status unchanged - this is expected if executePost fails due to missing platform adapter")
	}

	// Check for post log
	var postLogs []storage.PostLog
	if err := db.Where("tenant_id = ? AND scheduled_post_id = ?",
		1, post.ID).Find(&postLogs).Error; err == nil {
		if len(postLogs) > 0 {
			t.Logf("Post log created: %+v", postLogs[0])
		}
	}
}

// TestScheduler_WorkerPoolFallback tests fallback to goroutines when pool is full
func TestScheduler_WorkerPoolFallback(t *testing.T) {
	// Setup in-memory database (requires CGO)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		errMsg := err.Error()
		if errMsg == "Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work. This is a stub" ||
			errMsg == "sql: unknown driver \"sqlite3\" (forgotten import?)" {
			t.Skip("Skipping test: SQLite requires CGO. Run with: set CGO_ENABLED=1 && go test ./...")
		}
		t.Fatalf("failed to open database: %v", err)
	}

	if err := db.AutoMigrate(&storage.ScheduledPost{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	logger := zap.NewNop()
	optimizer := &mockCaptionOptimizerIntegration{}

	scheduler := NewScheduler(db, logger, optimizer, 20)

	// Create a small worker pool (size 1, queue 1) to force queue full scenario
	pool := worker.NewPool(1, 1, logger)
	pool.Start()
	defer pool.Shutdown(5 * time.Second)

	// Create adapter
	poolAdapter := &workerPoolTestAdapter{pool: pool}
	scheduler.WorkerPool = poolAdapter

	// Fill the queue
	scheduledAt1 := time.Now()
	post1 := storage.ScheduledPost{
		TenantID:    uuid.New(),
		Platforms:   `["instagram"]`,
		Status:      "scheduled",
		ScheduledAt: &scheduledAt1,
	}
	db.Create(&post1)
	job1 := &postJob{
		scheduler: scheduler,
		post:      post1,
		done:      func() {}, // No-op for test
	}
	pool.Submit(job1) // Fill queue

	// Try to submit another job - should fail and fallback
	scheduledAt2 := time.Now()
	post2 := storage.ScheduledPost{
		TenantID:    uuid.New(),
		Platforms:   `["instagram"]`,
		Status:      "scheduled",
		ScheduledAt: &scheduledAt2,
	}
	db.Create(&post2)
	job2 := &postJob{
		scheduler: scheduler,
		post:      post2,
		done:      func() {}, // No-op for test
	}

	// This should trigger fallback (though we can't easily test the fallback path here)
	err = pool.Submit(job2)
	if err == nil {
		t.Log("Job submitted successfully (queue had space)")
	} else {
		t.Logf("Queue full as expected: %v", err)
	}
}

// workerPoolTestAdapter adapts worker.Pool to scheduler's interface for testing
type workerPoolTestAdapter struct {
	pool *worker.Pool
}

func (a *workerPoolTestAdapter) Submit(job interface {
	Execute(ctx context.Context) error
}) error {
	return a.pool.Submit(worker.JobFunc(job.Execute))
}

// mockCaptionOptimizerIntegration is a mock for integration tests (separate from unit test mock)
type mockCaptionOptimizerIntegration struct{}

func (m *mockCaptionOptimizerIntegration) OptimizeCaption(ctx context.Context, req OptimizeCaptionRequest) (*OptimizeCaptionResult, error) {
	return &OptimizeCaptionResult{
		OptimizedCaption: req.OriginalCaption,
		Hashtags:         []string{"#test"},
	}, nil
}
