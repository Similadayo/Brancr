package circuitbreaker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// State represents the circuit breaker state
type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

// Config configures a circuit breaker
type Config struct {
	FailureThreshold   int           // Number of failures before opening
	SuccessThreshold   int           // Number of successes in half-open to close
	Timeout            time.Duration // How long to stay open before half-open
	MaxConcurrentCalls int           // Max concurrent calls in half-open state
}

// DefaultConfig returns default circuit breaker config
func DefaultConfig() Config {
	return Config{
		FailureThreshold:   5,
		SuccessThreshold:   2,
		Timeout:            30 * time.Second,
		MaxConcurrentCalls: 3,
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	name      string
	config    Config
	state     State
	mu        sync.RWMutex
	failures  int
	successes int
	lastFail  time.Time
	rdb       *redis.Client
	logger    *zap.Logger
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(name string, config Config, rdb *redis.Client, logger *zap.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		name:   name,
		config: config,
		state:  StateClosed,
		rdb:    rdb,
		logger: logger,
	}
}

// Execute executes a function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	cb.mu.Lock()
	state := cb.state
	cb.mu.Unlock()

	// Check if circuit is open
	if state == StateOpen {
		cb.mu.Lock()
		// Check if timeout has passed
		if time.Since(cb.lastFail) > cb.config.Timeout {
			cb.state = StateHalfOpen
			cb.successes = 0
			state = StateHalfOpen
			if cb.logger != nil {
				cb.logger.Info("circuit breaker transitioning to half-open",
					zap.String("name", cb.name))
			}
		} else {
			cb.mu.Unlock()
			return fmt.Errorf("circuit breaker %s is open", cb.name)
		}
		cb.mu.Unlock()
	}

	// Execute the function
	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failures++
		cb.lastFail = time.Now()

		if cb.state == StateHalfOpen || cb.failures >= cb.config.FailureThreshold {
			cb.state = StateOpen
			if cb.logger != nil {
				cb.logger.Warn("circuit breaker opened",
					zap.String("name", cb.name),
					zap.Int("failures", cb.failures),
					zap.Error(err))
			}
		}
		return err
	}

	// Success
	cb.failures = 0
	if cb.state == StateHalfOpen {
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.state = StateClosed
			if cb.logger != nil {
				cb.logger.Info("circuit breaker closed",
					zap.String("name", cb.name))
			}
		}
	}

	return nil
}

// State returns the current circuit breaker state
func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// IsOpen returns true if the circuit breaker is open
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.State() == StateOpen
}

var ErrCircuitOpen = errors.New("circuit breaker is open")
