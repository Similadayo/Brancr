package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestPool_Submit(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	pool := NewPool(2, 10, logger)
	pool.Start()
	defer pool.Shutdown(5 * time.Second)

	executed := make(chan bool, 1)

	job := JobFunc(func(ctx context.Context) error {
		executed <- true
		return nil
	})

	err := pool.Submit(job)
	if err != nil {
		t.Fatalf("Failed to submit job: %v", err)
	}

	select {
	case <-executed:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Job was not executed")
	}
}

func TestPool_QueueFull(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	pool := NewPool(1, 2, logger) // Small queue
	pool.Start()
	defer pool.Shutdown(5 * time.Second)

	// Fill queue with slow jobs
	slowJob := JobFunc(func(ctx context.Context) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	// Fill the queue
	pool.Submit(slowJob)
	pool.Submit(slowJob)

	// Next submit should fail
	err := pool.Submit(slowJob)
	if err != ErrQueueFull {
		t.Errorf("Expected ErrQueueFull, got: %v", err)
	}
}

func TestPool_JobError(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	pool := NewPool(1, 10, logger)
	pool.Start()
	defer pool.Shutdown(5 * time.Second)

	testErr := errors.New("test error")
	job := JobFunc(func(ctx context.Context) error {
		return testErr
	})

	err := pool.Submit(job)
	if err != nil {
		t.Fatalf("Failed to submit job: %v", err)
	}

	// Give job time to execute
	time.Sleep(100 * time.Millisecond)

	// Pool should still be functional
	stats := pool.Stats()
	if stats.ActiveJobs > 0 {
		t.Errorf("Expected no active jobs after error, got %d", stats.ActiveJobs)
	}
}

func TestPool_Stats(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	pool := NewPool(2, 10, logger)
	pool.Start()
	defer pool.Shutdown(5 * time.Second)

	stats := pool.Stats()
	if stats.Size != 2 {
		t.Errorf("Expected pool size 2, got %d", stats.Size)
	}

	if stats.QueueSize != 10 {
		t.Errorf("Expected queue size 10, got %d", stats.QueueSize)
	}
}

func TestPool_Shutdown(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	pool := NewPool(2, 10, logger)
	pool.Start()

	// Submit a job
	job := JobFunc(func(ctx context.Context) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	})
	pool.Submit(job)

	// Shutdown should complete
	err := pool.Shutdown(5 * time.Second)
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}
