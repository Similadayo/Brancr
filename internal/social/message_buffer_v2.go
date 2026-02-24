package social

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Adaptive Windowing Constants (Blueprint Section 2)
// Base window is extended for incomplete messages, capped at max
const (
	BaseWindowDuration    = 2 * time.Second  // Initial wait time
	FragmentExtension     = 3 * time.Second  // Extension for incomplete messages
	MaxWindowDuration     = 10 * time.Second // Maximum wait cap
	MaxFragmentExtensions = 3                // Max times we extend for fragments
)

// Hard flush signal patterns - triggers immediate processing
var (
	phoneNumberRegex = regexp.MustCompile(`(?:\+?234|0)[789]\d{9}`)
	addressKeywords  = []string{
		"street", "road", "avenue", "close", "estate", "lekki", "ikeja", "vi",
		"ikoyi", "ajah", "yaba", "surulere", "lagos", "abuja", "ph", "port harcourt",
	}
	commitmentPhrases = []string{
		"confirm", "that's all", "thats all", "ready", "send payment", "i'm ready",
		"im ready", "proceed", "let's go", "lets go", "done", "okay proceed",
	}
)

// ProductionMessageBuffer implements production-grade message buffering
// Core principle: Buffer first, process once, generate once, send smartly
// Enhanced with adaptive windowing per Blueprint Section 2
type ProductionMessageBuffer struct {
	buffers map[string]*bufferState // Key: tenantID:customerID:platform
	mu      sync.RWMutex
	logger  *zap.Logger

	// Configuration
	BufferWindow     time.Duration // Base window (2s default, adaptive)
	MaxBufferSize    int           // Max messages before forcing close (5)
	MaxAICallsPerMin int           // Max AI calls per conversation per minute (cost control)
	maxAICallsMu     sync.RWMutex  // Protects MaxAICallsPerMin updates
}

// bufferState represents a single conversation buffer
type bufferState struct {
	messages     []IncomingMessage
	bufferOpen   bool
	deadline     time.Time
	processTimer *time.Timer
	mu           sync.Mutex

	// Cost control tracking
	aiCallCount  int       // AI calls in current minute window
	aiCallWindow time.Time // Start of current minute window

	// Adaptive windowing tracking (Blueprint Section 2)
	fragmentExtensionsUsed int  // How many times we extended for fragments
	lastMessageFragment    bool // Was last message incomplete?
}

// NewProductionMessageBuffer creates a new production-grade message buffer
func NewProductionMessageBuffer(logger *zap.Logger, maxAICallsPerMin int) *ProductionMessageBuffer {
	if maxAICallsPerMin <= 0 {
		maxAICallsPerMin = 3 // Default fallback
	}
	return &ProductionMessageBuffer{
		buffers:          make(map[string]*bufferState),
		logger:           logger,
		BufferWindow:     BaseWindowDuration, // Use adaptive base (2s)
		MaxBufferSize:    5,                  // Max 5 messages
		MaxAICallsPerMin: maxAICallsPerMin,   // Max AI calls per conversation per minute (cost control)
	}
}

// getBufferKey generates a unique key for a conversation
func (mb *ProductionMessageBuffer) getBufferKey(tenantID uuid.UUID, customerID string, platform string) string {
	return fmt.Sprintf("%s:%s:%s", tenantID.String(), customerID, platform)
}

// getOrCreateBuffer gets or creates a buffer for a conversation
func (mb *ProductionMessageBuffer) getOrCreateBuffer(key string) *bufferState {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	buf, exists := mb.buffers[key]
	if !exists {
		buf = &bufferState{
			messages:   make([]IncomingMessage, 0, mb.MaxBufferSize),
			bufferOpen: false,
		}
		mb.buffers[key] = buf
	}
	return buf
}

// AddMessage adds a message to the buffer
// Returns: (shouldClose, messages, costLimitReached) - shouldClose=true means buffer should be processed now
// costLimitReached=true means AI call limit exceeded, use fallback response
func (mb *ProductionMessageBuffer) AddMessage(
	msg IncomingMessage,
	onBufferClose func([]IncomingMessage) error,
) (bool, []IncomingMessage, bool) {
	key := mb.getBufferKey(msg.TenantID, msg.CustomerID, msg.Platform)
	buf := mb.getOrCreateBuffer(key)

	buf.mu.Lock()
	defer buf.mu.Unlock()

	// Check cost control (Max AI calls per conversation per minute)
	// This prevents runaway AI usage and cost explosions
	now := time.Now()
	if now.Sub(buf.aiCallWindow) > time.Minute {
		// Reset counter for new minute window
		buf.aiCallCount = 0
		buf.aiCallWindow = now
	}

	// Get current limit (thread-safe, supports dynamic updates from admin dashboard)
	maxCalls := mb.getMaxAICallsPerMin()
	if buf.aiCallCount >= maxCalls {
		mb.logger.Warn("⚠️ AI call limit reached for conversation, using fallback",
			zap.String("key", key),
			zap.Int("calls", buf.aiCallCount),
			zap.Int("limit", maxCalls),
			zap.Stringer("tenant_id", msg.TenantID))
		return false, nil, true // costLimitReached = true
	}

	// Add message to buffer
	buf.messages = append(buf.messages, msg)
	buf.bufferOpen = true

	// Rule 1: If buffer reaches max size, close immediately
	if len(buf.messages) >= mb.MaxBufferSize {
		mb.logger.Info("📦 Buffer reached max size, closing immediately",
			zap.String("key", key),
			zap.Int("count", len(buf.messages)))

		// Cancel timer if exists
		if buf.processTimer != nil {
			buf.processTimer.Stop()
			buf.processTimer = nil
		}

		// Extract messages and close buffer
		messages := make([]IncomingMessage, len(buf.messages))
		copy(messages, buf.messages)
		buf.messages = buf.messages[:0]
		buf.bufferOpen = false

		// Increment AI call count (will be processed)
		buf.aiCallCount++
		if buf.aiCallWindow.IsZero() {
			buf.aiCallWindow = time.Now()
		}

		return true, messages, false
	}

	// BLUEPRINT RULE: Hard Flush Detection (Section 2 - Hard Flush Signals)
	// Check if message contains signals that should trigger immediate processing
	if mb.isHardFlushSignal(msg.Content) && len(buf.messages) >= 1 {
		mb.logger.Info("⚡ Hard flush signal detected, closing buffer immediately",
			zap.String("key", key),
			zap.Int("count", len(buf.messages)),
			zap.String("signal_in", msg.Content[:min(50, len(msg.Content))]))

		// Cancel timer if exists
		if buf.processTimer != nil {
			buf.processTimer.Stop()
			buf.processTimer = nil
		}

		// Extract messages and close buffer
		messages := make([]IncomingMessage, len(buf.messages))
		copy(messages, buf.messages)
		buf.messages = buf.messages[:0]
		buf.bufferOpen = false
		buf.fragmentExtensionsUsed = 0
		buf.lastMessageFragment = false

		// Increment AI call count (will be processed)
		buf.aiCallCount++
		if buf.aiCallWindow.IsZero() {
			buf.aiCallWindow = time.Now()
		}

		return true, messages, false
	}

	// BLUEPRINT RULE: Adaptive Windowing (Section 2)
	// Calculate adaptive window based on message completeness
	adaptiveWindow := mb.getAdaptiveWindow(buf, msg)
	newDeadline := now.Add(adaptiveWindow)

	// Track fragment state for next message
	buf.lastMessageFragment = mb.isMessageFragment(msg.Content)

	mb.logger.Info("📥 Message added to buffer",
		zap.String("key", key),
		zap.Int("buffer_size", len(buf.messages)),
		zap.Int("ai_calls_this_min", buf.aiCallCount),
		zap.Int("ai_calls_limit", maxCalls),
		zap.Duration("buffer_window", mb.BufferWindow),
		zap.Time("deadline", newDeadline),
		zap.Stringer("tenant_id", msg.TenantID),
		zap.String("message_preview", func() string {
			if len(msg.Content) > 50 {
				return msg.Content[:50] + "..."
			}
			return msg.Content
		}()))

	// Cancel existing timer if any
	if buf.processTimer != nil {
		buf.processTimer.Stop()
	}

	// Set new timer with adaptive window
	buf.deadline = newDeadline
	buf.processTimer = time.AfterFunc(adaptiveWindow, func() {
		buf.mu.Lock()
		defer buf.mu.Unlock()

		// Check if buffer is still open and has messages
		if !buf.bufferOpen || len(buf.messages) == 0 {
			return
		}

		mb.logger.Info("⏰ Buffer deadline expired, closing buffer",
			zap.String("key", key),
			zap.Int("count", len(buf.messages)))

		// Extract messages and close buffer
		messages := make([]IncomingMessage, len(buf.messages))
		copy(messages, buf.messages)
		buf.messages = buf.messages[:0]
		buf.bufferOpen = false
		buf.processTimer = nil

		// Increment AI call count (will be processed)
		buf.aiCallCount++
		if buf.aiCallWindow.IsZero() {
			buf.aiCallWindow = time.Now()
		}

		// Call callback to process accumulated messages (in goroutine to not block timer)
		if onBufferClose != nil {
			go func() {
				if err := onBufferClose(messages); err != nil {
					if strings.Contains(err.Error(), "no connected whatsapp account") {
						mb.logger.Warn("failed to process buffer on close (configuration issue)",
							zap.Error(err),
							zap.String("key", key),
							zap.Int("message_count", len(messages)))
					} else {
						mb.logger.Error("failed to process buffer on close",
							zap.Error(err),
							zap.String("key", key),
							zap.Int("message_count", len(messages)))
					}
				}
			}()
		}
	})

	// Don't process immediately - wait for buffer to close
	return false, nil, false
}

// CloseBuffer manually closes a buffer (for testing or explicit close)
func (mb *ProductionMessageBuffer) CloseBuffer(tenantID uuid.UUID, customerID string, platform string) []IncomingMessage {
	key := mb.getBufferKey(tenantID, customerID, platform)

	mb.mu.RLock()
	buf, exists := mb.buffers[key]
	mb.mu.RUnlock()

	if !exists {
		return nil
	}

	buf.mu.Lock()
	defer buf.mu.Unlock()

	if !buf.bufferOpen || len(buf.messages) == 0 {
		return nil
	}

	// Cancel timer if exists
	if buf.processTimer != nil {
		buf.processTimer.Stop()
		buf.processTimer = nil
	}

	// Extract messages and close buffer
	messages := make([]IncomingMessage, len(buf.messages))
	copy(messages, buf.messages)
	buf.messages = buf.messages[:0]
	buf.bufferOpen = false

	return messages
}

// CombineMessages combines multiple messages into a single text for AI processing
func (mb *ProductionMessageBuffer) CombineMessages(messages []IncomingMessage) string {
	if len(messages) == 0 {
		return ""
	}
	if len(messages) == 1 {
		return messages[0].Content
	}

	var parts []string
	for _, msg := range messages {
		parts = append(parts, strings.TrimSpace(msg.Content))
	}

	return strings.Join(parts, "\n")
}

// Cleanup removes old/inactive buffers
func (mb *ProductionMessageBuffer) Cleanup() {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	now := time.Now()
	for key, buf := range mb.buffers {
		buf.mu.Lock()
		// Remove if empty and deadline passed
		if len(buf.messages) == 0 && (!buf.bufferOpen || now.After(buf.deadline)) {
			if buf.processTimer != nil {
				buf.processTimer.Stop()
			}
			delete(mb.buffers, key)
			mb.logger.Debug("🧹 Cleaned up inactive buffer", zap.String("key", key))
		}
		buf.mu.Unlock()
	}
}

// UpdateMaxAICallsPerMin updates the cost control limit dynamically
// This allows admin dashboard updates to take effect immediately
func (mb *ProductionMessageBuffer) UpdateMaxAICallsPerMin(newLimit int) {
	if newLimit < 1 || newLimit > 10 {
		mb.logger.Warn("invalid MaxAICallsPerMin value, ignoring",
			zap.Int("value", newLimit),
			zap.Int("min", 1),
			zap.Int("max", 10))
		return
	}

	mb.maxAICallsMu.Lock()
	mb.MaxAICallsPerMin = newLimit
	mb.maxAICallsMu.Unlock()

	mb.logger.Info("💰 Cost control limit updated",
		zap.Int("max_ai_calls_per_min", newLimit))
}

// getMaxAICallsPerMin returns the current limit (thread-safe)
func (mb *ProductionMessageBuffer) getMaxAICallsPerMin() int {
	mb.maxAICallsMu.RLock()
	defer mb.maxAICallsMu.RUnlock()
	return mb.MaxAICallsPerMin
}

// ============================================================
// ADAPTIVE WINDOWING METHODS (Blueprint Section 2)
// ============================================================

// isHardFlushSignal detects signals that should trigger immediate buffer processing
// These are high-confidence signals that the customer is done sending messages
func (mb *ProductionMessageBuffer) isHardFlushSignal(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}

	lower := strings.ToLower(content)

	// Signal 1: Question mark (customer asking a question)
	if strings.Contains(content, "?") {
		return true
	}

	// Signal 2: Phone number detected (providing contact info)
	if phoneNumberRegex.MatchString(content) {
		return true
	}

	// Signal 3: Address keywords (providing delivery info)
	for _, keyword := range addressKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}

	// Signal 4: Commitment phrases (ready to proceed)
	for _, phrase := range commitmentPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}

	return false
}

// isMessageFragment detects if a message is incomplete/fragmentary
// Fragments typically: short, no punctuation, single words/phrases
func (mb *ProductionMessageBuffer) isMessageFragment(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}

	// Very short messages are likely fragments
	if len(content) < 15 {
		return true
	}

	// Messages without ending punctuation are likely incomplete
	if !strings.HasSuffix(content, ".") &&
		!strings.HasSuffix(content, "!") &&
		!strings.HasSuffix(content, "?") &&
		!strings.HasSuffix(content, ",") {
		return true
	}

	// Single word messages are fragments
	words := strings.Fields(content)
	if len(words) <= 2 {
		return true
	}

	return false
}

// getAdaptiveWindow calculates the appropriate buffer window based on message context
// Implements Blueprint Section 2: Adaptive Message Windowing
func (mb *ProductionMessageBuffer) getAdaptiveWindow(buf *bufferState, msg IncomingMessage) time.Duration {
	// Start with base window
	window := BaseWindowDuration

	// Extend if previous message was a fragment (customer likely still typing)
	if buf.lastMessageFragment && buf.fragmentExtensionsUsed < MaxFragmentExtensions {
		window = window + FragmentExtension
		buf.fragmentExtensionsUsed++

		mb.logger.Debug("🔄 Extended window for fragment",
			zap.Duration("new_window", window),
			zap.Int("extensions_used", buf.fragmentExtensionsUsed))
	}

	// Extend if current message is also a fragment
	if mb.isMessageFragment(msg.Content) && buf.fragmentExtensionsUsed < MaxFragmentExtensions {
		window = window + FragmentExtension
		buf.fragmentExtensionsUsed++
	}

	// Cap at maximum window
	if window > MaxWindowDuration {
		window = MaxWindowDuration
	}

	return window
}
