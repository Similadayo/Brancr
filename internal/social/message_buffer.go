package social

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// MessageBuffer manages buffering of sequential messages to prevent spammy AI replies
type MessageBuffer struct {
	buffers map[string]*conversationBuffer // Key: tenantID:customerID:platform
	mu      sync.RWMutex
	logger  *zap.Logger

	// Configuration
	BufferWindow       time.Duration // How long to wait for more messages (2-3s)
	MaxContextSize     int           // Max messages to accumulate (3-5)
	MaxAICallsPerMin   int           // Cost control: max AI calls per conversation per minute
	ClearIntentPhrases []string      // Phrases that indicate clear intent (override buffering)
}

// conversationBuffer holds buffered messages for a single conversation
type conversationBuffer struct {
	messages     []bufferedMessage
	lastAITime   time.Time // When AI last replied
	aiCallCount  int       // AI calls in current minute
	aiCallWindow time.Time // Start of current minute window
	mu           sync.Mutex
	processTimer *time.Timer
	processing   bool // Lock to prevent concurrent processing
}

// bufferedMessage represents a message waiting to be processed
type bufferedMessage struct {
	msg       IncomingMessage
	timestamp time.Time
}

// NewMessageBuffer creates a new message buffer manager
func NewMessageBuffer(logger *zap.Logger) *MessageBuffer {
	return &MessageBuffer{
		buffers:          make(map[string]*conversationBuffer),
		logger:           logger,
		BufferWindow:     2 * time.Second, // 2-3 seconds as recommended
		MaxContextSize:   5,               // Last 3-5 messages
		MaxAICallsPerMin: 3,               // Max 3 AI calls per conversation per minute
		ClearIntentPhrases: []string{
			// Greetings (process immediately to avoid delays)
			"hello", "hi", "hey", "good morning", "good afternoon", "good evening",
			// Price queries
			"how much", "how much is", "what's the price", "what is the price",
			"can you do", "i want to pay", "i'll pay", "i will pay",
			"send account", "account number", "payment details",
			// Availability
			"do you have", "is it available", "is this available",
			// Orders
			"i want to order", "i'll order", "i will order",
			// Time queries
			"when can", "when will", "how long",
			// Responses
			"yes", "ok", "sure", "alright", "fine",
			"no", "not interested", "maybe later",
		},
	}
}

// getBufferKey generates a unique key for a conversation
func (mb *MessageBuffer) getBufferKey(tenantID uuid.UUID, customerID string, platform string) string {
	return fmt.Sprintf("%s:%s:%s", tenantID.String(), customerID, platform)
}

// getOrCreateBuffer gets or creates a buffer for a conversation
func (mb *MessageBuffer) getOrCreateBuffer(key string) *conversationBuffer {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	buf, exists := mb.buffers[key]
	if !exists {
		buf = &conversationBuffer{
			messages: make([]bufferedMessage, 0, mb.MaxContextSize),
		}
		mb.buffers[key] = buf
	}
	return buf
}

// AddMessage adds a message to the buffer and returns whether to process immediately
// Returns: (shouldProcessNow, accumulatedMessages, clearIntent, costLimitReached)
// If shouldProcessNow is true, caller should process accumulatedMessages immediately
// If costLimitReached is true, caller should use fallback response
func (mb *MessageBuffer) AddMessage(ctx context.Context, msg IncomingMessage, processCallback func([]IncomingMessage) error) (bool, []IncomingMessage, bool, bool) {
	key := mb.getBufferKey(msg.TenantID, msg.CustomerID, msg.Platform)
	buf := mb.getOrCreateBuffer(key)

	buf.mu.Lock()
	defer buf.mu.Unlock()

	// Check conversation turn lock: Don't reply if AI just replied and user hasn't sent new message
	// This prevents double replies and self-contradictions (REFINEMENT 2)
	timeSinceAI := time.Since(buf.lastAITime)
	if timeSinceAI < 500*time.Millisecond && len(buf.messages) == 0 {
		// AI replied very recently (< 500ms) and this is the first message after
		// This is likely a race condition - wait a bit more
		buf.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
		buf.mu.Lock()
	}

	// Check cost control (REFINEMENT 3: Max AI calls per minute)
	now := time.Now()
	if now.Sub(buf.aiCallWindow) > time.Minute {
		// Reset counter for new minute
		buf.aiCallCount = 0
		buf.aiCallWindow = now
	}

	if buf.aiCallCount >= mb.MaxAICallsPerMin {
		mb.logger.Warn("⚠️ AI call limit reached for conversation, using fallback",
			zap.String("key", key),
			zap.Int("calls", buf.aiCallCount))
		return false, nil, false, true
	}

	// Add message to buffer
	buf.messages = append(buf.messages, bufferedMessage{
		msg:       msg,
		timestamp: time.Now(),
	})

	// Keep only last MaxContextSize messages
	if len(buf.messages) > mb.MaxContextSize {
		buf.messages = buf.messages[len(buf.messages)-mb.MaxContextSize:]
	}

	// Check for clear intent (REFINEMENT 1: Override buffering)
	// Check both the new message AND accumulated messages (for sequences like "I want" + "to buy" + "how much")
	clearIntent := mb.hasClearIntent(msg.Content)

	// Also check accumulated messages for intent (in case intent is split across messages)
	if !clearIntent && len(buf.messages) > 0 {
		// Combine all buffered messages to check for intent across the sequence
		combinedText := ""
		for _, bm := range buf.messages {
			combinedText += " " + bm.msg.Content
		}
		combinedText += " " + msg.Content
		clearIntent = mb.hasClearIntent(combinedText)

		if clearIntent {
			mb.logger.Info("🎯 Clear intent detected in accumulated sequence, processing immediately",
				zap.String("combined", strings.TrimSpace(combinedText)),
				zap.Int("message_count", len(buf.messages)+1),
				zap.Stringer("tenant_id", msg.TenantID))
		}
	}

	if clearIntent {
		mb.logger.Info("🎯 Clear intent detected, processing immediately",
			zap.String("message", msg.Content),
			zap.Int("buffered_count", len(buf.messages)),
			zap.Stringer("tenant_id", msg.TenantID))

		// Cancel any pending timer
		if buf.processTimer != nil {
			buf.processTimer.Stop()
			buf.processTimer = nil
		}

		// Extract accumulated messages (including the new one)
		accumulated := make([]IncomingMessage, len(buf.messages))
		for i, bm := range buf.messages {
			accumulated[i] = bm.msg
		}
		// Add the new message to accumulated
		accumulated = append(accumulated, msg)
		buf.messages = buf.messages[:0] // Clear buffer

		// Update AI call tracking
		buf.aiCallCount++
		return true, accumulated, true, false
	}

	// SAFETY: For very short/simple messages (like "Hello", "Hi"), process immediately
	// This prevents unnecessary delays for greetings and simple queries
	messageLower := strings.ToLower(strings.TrimSpace(msg.Content))
	simpleGreetings := []string{"hello", "hi", "hey", "good morning", "good afternoon", "good evening"}
	isSimpleGreeting := false
	for _, greeting := range simpleGreetings {
		if messageLower == greeting || strings.HasPrefix(messageLower, greeting+" ") {
			isSimpleGreeting = true
			break
		}
	}

	// If it's a simple greeting and first message, process immediately
	if isSimpleGreeting && len(buf.messages) == 1 {
		mb.logger.Info("👋 Simple greeting detected, processing immediately",
			zap.String("message", msg.Content),
			zap.Stringer("tenant_id", msg.TenantID))

		accumulated := make([]IncomingMessage, len(buf.messages))
		for i, bm := range buf.messages {
			accumulated[i] = bm.msg
		}
		buf.messages = buf.messages[:0] // Clear buffer
		buf.aiCallCount++
		return true, accumulated, false, false
	}

	// Cancel existing timer if any
	if buf.processTimer != nil {
		buf.processTimer.Stop()
		mb.logger.Debug("Cancelled existing buffer timer",
			zap.String("key", key),
			zap.Int("buffered_count", len(buf.messages)))
	}

	// Set timer to process after buffer window
	// CRITICAL: The callback uses the original context which may be cancelled by the time timer fires
	// We need to ensure processing happens even if original context is cancelled
	buf.processTimer = time.AfterFunc(mb.BufferWindow, func() {
		buf.mu.Lock()

		if len(buf.messages) == 0 || buf.processing {
			buf.mu.Unlock()
			return
		}

		buf.processing = true
		accumulated := make([]IncomingMessage, len(buf.messages))
		for i, bm := range buf.messages {
			accumulated[i] = bm.msg
		}
		buf.messages = buf.messages[:0] // Clear buffer
		buf.processTimer = nil
		buf.mu.Unlock() // Unlock before async processing

		// Update AI call tracking
		now := time.Now()
		buf.mu.Lock()
		if now.Sub(buf.aiCallWindow) > time.Minute {
			buf.aiCallCount = 0
			buf.aiCallWindow = now
		}
		buf.aiCallCount++
		buf.mu.Unlock()

		// Call the callback to process accumulated messages (outside lock, in goroutine)
		// CRITICAL: Execute in goroutine to prevent blocking the timer and causing context deadline
		if processCallback != nil && len(accumulated) > 0 {
			mb.logger.Info("⏰ Buffer window expired, processing accumulated messages",
				zap.Int("count", len(accumulated)),
				zap.String("key", key),
				zap.Stringer("tenant_id", accumulated[0].TenantID))

			// Execute callback in goroutine to prevent timer blocking
			go func() {
				if err := processCallback(accumulated); err != nil {
					mb.logger.Error("❌ Failed to process buffered messages",
						zap.Error(err),
						zap.Int("message_count", len(accumulated)),
						zap.String("key", key),
						zap.Stringer("tenant_id", accumulated[0].TenantID))
				} else {
					mb.logger.Info("✅ Successfully processed buffered messages",
						zap.Int("count", len(accumulated)),
						zap.String("key", key),
						zap.Stringer("tenant_id", accumulated[0].TenantID))
				}

				// Mark processing as complete after callback finishes
				buf.mu.Lock()
				buf.processing = false
				buf.mu.Unlock()
			}()
		} else {
			mb.logger.Warn("⚠️ Buffer timer fired but no callback or messages",
				zap.Bool("has_callback", processCallback != nil),
				zap.Int("message_count", len(accumulated)),
				zap.String("key", key))

			// Mark processing as complete if no callback
			buf.mu.Lock()
			buf.processing = false
			buf.mu.Unlock()
		}
	})

	// Don't process immediately - wait for buffer window or clear intent
	return false, nil, false, false
}

// ProcessBuffered processes accumulated messages and returns them
// This is called when buffer window expires or clear intent is detected
func (mb *MessageBuffer) ProcessBuffered(key string) []IncomingMessage {
	mb.mu.RLock()
	buf, exists := mb.buffers[key]
	mb.mu.RUnlock()

	if !exists {
		return nil
	}

	buf.mu.Lock()
	defer buf.mu.Unlock()

	if len(buf.messages) == 0 {
		return nil
	}

	accumulated := make([]IncomingMessage, len(buf.messages))
	for i, bm := range buf.messages {
		accumulated[i] = bm.msg
	}
	buf.messages = buf.messages[:0] // Clear buffer

	// Update AI call tracking
	now := time.Now()
	if now.Sub(buf.aiCallWindow) > time.Minute {
		buf.aiCallCount = 0
		buf.aiCallWindow = now
	}
	buf.aiCallCount++

	return accumulated
}

// RecordAIResponse records that AI has replied (for conversation turn lock)
func (mb *MessageBuffer) RecordAIResponse(tenantID uuid.UUID, customerID string, platform string) {
	key := mb.getBufferKey(tenantID, customerID, platform)

	mb.mu.RLock()
	buf, exists := mb.buffers[key]
	mb.mu.RUnlock()

	if !exists {
		return
	}

	buf.mu.Lock()
	buf.lastAITime = time.Now()
	buf.mu.Unlock()
}

// hasClearIntent checks if message contains clear intent phrases
func (mb *MessageBuffer) hasClearIntent(message string) bool {
	messageLower := strings.ToLower(strings.TrimSpace(message))

	for _, phrase := range mb.ClearIntentPhrases {
		if strings.Contains(messageLower, phrase) {
			return true
		}
	}

	return false
}

// Cleanup removes old buffers (call periodically)
func (mb *MessageBuffer) Cleanup() {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	now := time.Now()
	for key, buf := range mb.buffers {
		buf.mu.Lock()
		// Remove buffers that haven't been used in 5 minutes
		if len(buf.messages) == 0 && now.Sub(buf.lastAITime) > 5*time.Minute {
			delete(mb.buffers, key)
		}
		buf.mu.Unlock()
	}
}
