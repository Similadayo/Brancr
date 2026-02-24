package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestCircuitBreaker_Execute_Success(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cb := NewCircuitBreaker("test", DefaultConfig(), nil, logger)

	err := cb.Execute(context.Background(), func() error {
		return nil
	})

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if cb.State() != StateClosed {
		t.Errorf("Expected state Closed, got: %v", cb.State())
	}
}

func TestCircuitBreaker_Execute_Failure(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	config.FailureThreshold = 2
	cb := NewCircuitBreaker("test", config, nil, logger)

	testErr := errors.New("test error")

	// First failure
	cb.Execute(context.Background(), func() error {
		return testErr
	})

	if cb.State() != StateClosed {
		t.Errorf("Expected state Closed after first failure, got: %v", cb.State())
	}

	// Second failure should open circuit
	cb.Execute(context.Background(), func() error {
		return testErr
	})

	if cb.State() != StateOpen {
		t.Errorf("Expected state Open after threshold, got: %v", cb.State())
	}
}

func TestCircuitBreaker_OpenState(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	config.FailureThreshold = 1
	config.Timeout = 100 * time.Millisecond
	cb := NewCircuitBreaker("test", config, nil, logger)

	// Open circuit
	cb.Execute(context.Background(), func() error {
		return errors.New("fail")
	})

	if !cb.IsOpen() {
		t.Error("Expected circuit to be open")
	}

	// Should reject immediately
	err := cb.Execute(context.Background(), func() error {
		return nil
	})

	if err == nil {
		t.Error("Expected error when circuit is open")
	}
}

func TestCircuitBreaker_HalfOpen_Recovery(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	config := DefaultConfig()
	config.FailureThreshold = 1
	config.SuccessThreshold = 1
	config.Timeout = 50 * time.Millisecond
	cb := NewCircuitBreaker("test", config, nil, logger)

	// Open circuit
	cb.Execute(context.Background(), func() error {
		return errors.New("fail")
	})

	// Wait for timeout (need to check state after timeout)
	time.Sleep(150 * time.Millisecond)

	// Try to execute - this should trigger transition to half-open
	cb.Execute(context.Background(), func() error {
		return nil
	})

	// Should be half-open or closed now (depending on timing)
	state := cb.State()
	if state != StateHalfOpen && state != StateClosed {
		t.Errorf("Expected state HalfOpen or Closed, got: %v", state)
	}

	// Success should close circuit
	cb.Execute(context.Background(), func() error {
		return nil
	})

	if cb.State() != StateClosed {
		t.Errorf("Expected state Closed after success, got: %v", cb.State())
	}
}
