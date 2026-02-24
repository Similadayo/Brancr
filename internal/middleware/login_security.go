package middleware

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// LoginSecurityConfig defines lockout settings
type LoginSecurityConfig struct {
	MaxFailedAttempts int           // Max failures before lockout (default: 5)
	LockoutDuration   time.Duration // How long to lock out (default: 15 minutes)
	AttemptWindow     time.Duration // Window to track attempts (default: 15 minutes)
}

// DefaultLoginSecurityConfig returns sensible defaults
func DefaultLoginSecurityConfig() LoginSecurityConfig {
	return LoginSecurityConfig{
		MaxFailedAttempts: 5,
		LockoutDuration:   15 * time.Minute,
		AttemptWindow:     15 * time.Minute,
	}
}

// LoginSecurity tracks failed login attempts to prevent brute-force attacks
type LoginSecurity struct {
	rdb    *redis.Client
	logger *zap.Logger
	config LoginSecurityConfig
	// In-memory fallback when Redis is unavailable
	memStore sync.Map
}

type memoryAttempt struct {
	count     int
	lockedAt  *time.Time
	expiresAt time.Time
	mu        sync.Mutex
}

// NewLoginSecurity creates a new login security tracker
func NewLoginSecurity(rdb *redis.Client, logger *zap.Logger, config LoginSecurityConfig) *LoginSecurity {
	return &LoginSecurity{
		rdb:    rdb,
		logger: logger,
		config: config,
	}
}

// IsLocked checks if an identifier (email or IP) is currently locked out
func (ls *LoginSecurity) IsLocked(ctx context.Context, identifier string) (bool, time.Duration) {
	key := fmt.Sprintf("lockout:%s", identifier)

	if ls.rdb != nil {
		ttl, err := ls.rdb.TTL(ctx, key).Result()
		if err == nil && ttl > 0 {
			return true, ttl
		}
		return false, 0
	}

	// Memory fallback
	if val, ok := ls.memStore.Load(key); ok {
		attempt := val.(*memoryAttempt)
		attempt.mu.Lock()
		defer attempt.mu.Unlock()
		if attempt.lockedAt != nil && time.Since(*attempt.lockedAt) < ls.config.LockoutDuration {
			remaining := ls.config.LockoutDuration - time.Since(*attempt.lockedAt)
			return true, remaining
		}
	}
	return false, 0
}

// RecordFailedAttempt records a failed login and returns true if now locked out
func (ls *LoginSecurity) RecordFailedAttempt(ctx context.Context, identifier string) (locked bool, attemptsRemaining int) {
	attemptKey := fmt.Sprintf("login_attempts:%s", identifier)
	lockoutKey := fmt.Sprintf("lockout:%s", identifier)

	if ls.rdb != nil {
		// Increment attempt counter
		count, err := ls.rdb.Incr(ctx, attemptKey).Result()
		if err != nil {
			ls.logger.Warn("failed to record login attempt", zap.Error(err))
			return false, ls.config.MaxFailedAttempts
		}

		// Set expiry on first attempt
		if count == 1 {
			ls.rdb.Expire(ctx, attemptKey, ls.config.AttemptWindow)
		}

		remaining := ls.config.MaxFailedAttempts - int(count)
		if count >= int64(ls.config.MaxFailedAttempts) {
			// Lock the account
			ls.rdb.Set(ctx, lockoutKey, "1", ls.config.LockoutDuration)
			ls.rdb.Del(ctx, attemptKey) // Reset attempts
			ls.logger.Warn("account locked due to failed attempts",
				zap.String("identifier", identifier),
				zap.Int64("attempts", count),
				zap.Duration("lockout_duration", ls.config.LockoutDuration))
			return true, 0
		}
		return false, remaining
	}

	// Memory fallback
	val, _ := ls.memStore.LoadOrStore(attemptKey, &memoryAttempt{count: 0, expiresAt: time.Now().Add(ls.config.AttemptWindow)})
	attempt := val.(*memoryAttempt)
	attempt.mu.Lock()
	defer attempt.mu.Unlock()

	if time.Now().After(attempt.expiresAt) {
		attempt.count = 0
		attempt.expiresAt = time.Now().Add(ls.config.AttemptWindow)
	}

	attempt.count++
	remaining := ls.config.MaxFailedAttempts - attempt.count

	if attempt.count >= ls.config.MaxFailedAttempts {
		now := time.Now()
		lockAttempt := &memoryAttempt{lockedAt: &now}
		ls.memStore.Store(lockoutKey, lockAttempt)
		ls.memStore.Delete(attemptKey)
		ls.logger.Warn("account locked due to failed attempts (in-memory)",
			zap.String("identifier", identifier),
			zap.Int("attempts", attempt.count))
		return true, 0
	}
	return false, remaining
}

// ClearAttempts clears failed attempts after successful login
func (ls *LoginSecurity) ClearAttempts(ctx context.Context, identifier string) {
	attemptKey := fmt.Sprintf("login_attempts:%s", identifier)

	if ls.rdb != nil {
		ls.rdb.Del(ctx, attemptKey)
	} else {
		ls.memStore.Delete(attemptKey)
	}
}

// GetRemainingAttempts returns how many login attempts are left before lockout
func (ls *LoginSecurity) GetRemainingAttempts(ctx context.Context, identifier string) int {
	attemptKey := fmt.Sprintf("login_attempts:%s", identifier)

	if ls.rdb != nil {
		count, err := ls.rdb.Get(ctx, attemptKey).Int()
		if err != nil {
			return ls.config.MaxFailedAttempts
		}
		remaining := ls.config.MaxFailedAttempts - count
		if remaining < 0 {
			return 0
		}
		return remaining
	}

	// Memory fallback
	if val, ok := ls.memStore.Load(attemptKey); ok {
		attempt := val.(*memoryAttempt)
		attempt.mu.Lock()
		defer attempt.mu.Unlock()
		remaining := ls.config.MaxFailedAttempts - attempt.count
		if remaining < 0 {
			return 0
		}
		return remaining
	}
	return ls.config.MaxFailedAttempts
}
