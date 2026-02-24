package social

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ConversationLock prevents double replies and overlapping sequences
// Core principle: One customer turn = one AI turn
type ConversationLock struct {
	locks  map[string]*lockState // Key: tenantID:customerID:platform
	mu     sync.RWMutex
	logger *zap.Logger
}

type lockState struct {
	locked   bool
	lockedAt time.Time
	mu       sync.Mutex
}

// NewConversationLock creates a new conversation lock manager
func NewConversationLock(logger *zap.Logger) *ConversationLock {
	return &ConversationLock{
		locks:  make(map[string]*lockState),
		logger: logger,
	}
}

// getLockKey generates a unique key for a conversation
func (cl *ConversationLock) getLockKey(tenantID uuid.UUID, customerID string, platform string) string {
	return fmt.Sprintf("%s:%s:%s", tenantID.String(), customerID, platform)
}

// Lock locks a conversation to prevent AI from replying again
// Returns true if lock was acquired, false if already locked
func (cl *ConversationLock) Lock(tenantID uuid.UUID, customerID string, platform string) bool {
	key := cl.getLockKey(tenantID, customerID, platform)

	cl.mu.Lock()
	state, exists := cl.locks[key]
	if !exists {
		state = &lockState{}
		cl.locks[key] = state
	}
	cl.mu.Unlock()

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.locked {
		cl.logger.Debug("Conversation already locked, skipping reply",
			zap.String("key", key),
			zap.Duration("locked_for", time.Since(state.lockedAt)))
		return false
	}

	state.locked = true
	state.lockedAt = time.Now()

	cl.logger.Info("🔒 Conversation locked",
		zap.String("key", key),
		zap.Stringer("tenant_id", tenantID))

	return true
}

// Unlock unlocks a conversation after AI reply is complete
func (cl *ConversationLock) Unlock(tenantID uuid.UUID, customerID string, platform string) {
	key := cl.getLockKey(tenantID, customerID, platform)

	cl.mu.RLock()
	state, exists := cl.locks[key]
	cl.mu.RUnlock()

	if !exists {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.locked {
		state.locked = false
		cl.logger.Info("🔓 Conversation unlocked",
			zap.String("key", key),
			zap.Stringer("tenant_id", tenantID),
			zap.Duration("was_locked_for", time.Since(state.lockedAt)))
	}
}

// IsLocked checks if a conversation is currently locked
func (cl *ConversationLock) IsLocked(tenantID uuid.UUID, customerID string, platform string) bool {
	key := cl.getLockKey(tenantID, customerID, platform)

	cl.mu.RLock()
	state, exists := cl.locks[key]
	cl.mu.RUnlock()

	if !exists {
		return false
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	return state.locked
}

// Cleanup removes old locks (call periodically)
func (cl *ConversationLock) Cleanup() {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	now := time.Now()
	for key, state := range cl.locks {
		state.mu.Lock()
		// Remove locks older than 5 minutes (shouldn't happen, but safety)
		if !state.locked || now.Sub(state.lockedAt) > 5*time.Minute {
			if !state.locked {
				delete(cl.locks, key)
			} else {
				// Force unlock stale locks
				state.locked = false
				cl.logger.Warn("🔓 Force unlocked stale conversation lock",
					zap.String("key", key),
					zap.Duration("age", now.Sub(state.lockedAt)))
			}
		}
		state.mu.Unlock()
	}
}

// Serialize executes a function while holding a lock for the specific customer.
// This ensures atomic operations (like creating a conversation) for the same customer
// are serialized, preventing race conditions when multiple webhooks arrive simultaneously.
func (cl *ConversationLock) Serialize(tenantID uuid.UUID, customerID string, platform string, fn func() error) error {
	key := cl.getLockKey(tenantID, customerID, platform)

	// Get or create lock state
	cl.mu.Lock()
	state, exists := cl.locks[key]
	if !exists {
		state = &lockState{}
		cl.locks[key] = state
	}
	cl.mu.Unlock()

	// Acquire per-customer lock (blocking)
	state.mu.Lock()
	defer state.mu.Unlock()

	// Execute function
	return fn()
}
