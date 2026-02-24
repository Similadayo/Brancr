package social

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/similadayo/brancr/internal/types"
	"go.uber.org/zap"
)

// ReplySequencer handles intelligent delivery of AI replies
// This is a delivery concern, not an AI concern
// Enhanced with abort-on-interrupt per Blueprint Section 13
type ReplySequencer struct {
	logger *zap.Logger

	// Abort-on-interrupt tracking (Blueprint Section 13)
	// Key: conversationKey (tenantID:customerID:platform)
	activeSequences map[string]context.CancelFunc
	mu              sync.RWMutex
}

// NewReplySequencer creates a new reply sequencer
func NewReplySequencer(logger *zap.Logger) *ReplySequencer {
	return &ReplySequencer{
		logger:          logger,
		activeSequences: make(map[string]context.CancelFunc),
	}
}

// SendReply sends a structured reply using the sequencing engine
func (rs *ReplySequencer) SendReply(
	ctx context.Context,
	sendFunc func(ctx context.Context, msg string) error,
	reply types.StructuredReply,
) error {
	messages := reply.Messages

	// Rule 1: Smart Split - Break down long messages FIRST
	// This applies to ALL messages, whether single or multiple
	var splitMessages []string
	const maxBurstLength = 1000 // Characters - WhatsApp friendly (was 150, caused fragmentation)

	for _, msg := range messages {
		chunks := rs.SmartSplit(msg, maxBurstLength)
		splitMessages = append(splitMessages, chunks...)
	}
	messages = splitMessages

	// Rule 2: If single message (after split), send once
	if len(messages) == 1 {
		return sendFunc(ctx, messages[0])
	}

	// Rule 3: If multiple messages, send sequentially (with smart delays)
	// We removed the compression limit to allow "bursts" of any length
	if len(messages) >= 2 {
		rs.logger.Info("📨 Sending sequential reply",
			zap.Int("count", len(messages)),
			zap.String("mode", reply.ReplyMode))

		for i, msg := range messages {
			if err := sendFunc(ctx, msg); err != nil {
				rs.logger.Error("failed to send message in sequence",
					zap.Int("index", i+1),
					zap.Error(err))
			}

			// Add human-like delay between messages
			if i < len(messages)-1 {
				// Calculate delay based on length of NEXT message (simulating typing)
				nextMsgLen := len(messages[i+1])
				// Approx 50ms per character + base delay
				typingDelay := time.Duration(nextMsgLen*50)*time.Millisecond + 500*time.Millisecond

				// Cap at 3 seconds max to keep it snappy
				if typingDelay > 3*time.Second {
					typingDelay = 3 * time.Second
				}

				rs.logger.Debug("⏳ Simulating typing delay",
					zap.Duration("delay", typingDelay),
					zap.Int("next_msg_len", nextMsgLen))
				time.Sleep(typingDelay)
			}
		}
		return nil
	}

	return nil
}

// SmartSplit splits text into natural chunks based on length and punctuation
func (rs *ReplySequencer) SmartSplit(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= maxLen {
			chunks = append(chunks, remaining)
			break
		}

		// Find best cut point within maxLen
		// Priority: Newlines > Sentence End > Comma > Space
		cutPoint := -1

		// Look for cut point in the last 20% of the allowed window to avoid short chunks
		searchStart := maxLen / 2
		searchEnd := maxLen

		// 1. Newlines
		if idx := lastIndexAny(remaining[:searchEnd], "\n"); idx > searchStart {
			cutPoint = idx
		}

		// 2. Sentence End (., !, ?)
		if cutPoint == -1 {
			if idx := lastIndexAny(remaining[:searchEnd], ".!?"); idx > searchStart {
				cutPoint = idx + 1 // Include the punctuation
			}
		}

		// 3. Comma
		if cutPoint == -1 {
			if idx := lastIndexAny(remaining[:searchEnd], ","); idx > searchStart {
				cutPoint = idx + 1 // Include comma
			}
		}

		// 4. Space (fallback)
		if cutPoint == -1 {
			if idx := lastIndexAny(remaining[:searchEnd], " "); idx > searchStart {
				cutPoint = idx
			}
		}

		// 5. If absolutely no natural break, forced cut
		if cutPoint == -1 {
			cutPoint = maxLen
		}

		chunks = append(chunks, strings.TrimSpace(remaining[:cutPoint]))
		remaining = strings.TrimSpace(remaining[cutPoint:])
	}

	return chunks
}

// Helper for SmartSplit
func lastIndexAny(s, chars string) int {
	for i := len(s) - 1; i >= 0; i-- {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				return i
			}
		}
	}
	return -1
}

// getDelay returns human-like delay between messages
func (rs *ReplySequencer) getDelay(messageIndex int) time.Duration {
	switch messageIndex {
	case 1: // Second message
		return 350 * time.Millisecond // 300-500ms
	case 2: // Third message
		return 750 * time.Millisecond // 600-900ms
	default:
		return 1 * time.Second
	}
}

// ============================================================
// ABORT-ON-INTERRUPT (Blueprint Section 13)
// ============================================================

// SendReplyWithAbort sends a structured reply with abort capability
// If customer replies mid-sequence, remaining messages are aborted
func (rs *ReplySequencer) SendReplyWithAbort(
	ctx context.Context,
	conversationKey string, // Format: tenantID:customerID:platform
	sendFunc func(ctx context.Context, msg string) error,
	reply types.StructuredReply,
) error {
	messages := reply.Messages

	// Single message - no abort needed
	if len(messages) <= 1 {
		if len(messages) == 1 {
			return sendFunc(ctx, messages[0])
		}
		return nil
	}

	// Create cancellable context for this sequence
	seqCtx, cancel := context.WithCancel(ctx)

	// Register this sequence (cancel any existing for same conversation)
	rs.mu.Lock()
	if existingCancel, ok := rs.activeSequences[conversationKey]; ok {
		existingCancel() // Cancel previous sequence
		rs.logger.Info("🛑 Cancelled previous sequence for conversation",
			zap.String("key", conversationKey))
	}
	rs.activeSequences[conversationKey] = cancel
	rs.mu.Unlock()

	// Cleanup when done
	defer func() {
		rs.mu.Lock()
		delete(rs.activeSequences, conversationKey)
		rs.mu.Unlock()
	}()

	rs.logger.Info("📨 Starting abortable sequence",
		zap.String("key", conversationKey),
		zap.Int("count", len(messages)))

	// Send messages with abort check
	for i, msg := range messages {
		// Check if aborted before sending
		select {
		case <-seqCtx.Done():
			rs.logger.Info("🛑 Sequence aborted - customer replied mid-sequence",
				zap.String("key", conversationKey),
				zap.Int("sent", i),
				zap.Int("total", len(messages)))
			return nil // Not an error - graceful abort
		default:
		}

		// Send message
		if err := sendFunc(seqCtx, msg); err != nil {
			rs.logger.Error("failed to send message in sequence",
				zap.String("key", conversationKey),
				zap.Int("index", i+1),
				zap.Error(err))
			// Continue trying remaining messages
		}

		// Add delay before next message (with abort check)
		if i < len(messages)-1 {
			delay := rs.getDelay(i + 1)

			// Use timer with select to allow abort during delay
			timer := time.NewTimer(delay)
			select {
			case <-seqCtx.Done():
				timer.Stop()
				rs.logger.Info("🛑 Sequence aborted during delay",
					zap.String("key", conversationKey),
					zap.Int("sent", i+1),
					zap.Int("total", len(messages)))
				return nil
			case <-timer.C:
				// Delay completed, continue
			}
		}
	}

	rs.logger.Info("✅ Abortable sequence completed",
		zap.String("key", conversationKey),
		zap.Int("total", len(messages)))

	return nil
}

// AbortSequence cancels any active sequence for a conversation
// Should be called when a new customer message arrives
func (rs *ReplySequencer) AbortSequence(conversationKey string) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if cancel, ok := rs.activeSequences[conversationKey]; ok {
		cancel()
		delete(rs.activeSequences, conversationKey)
		rs.logger.Info("🛑 Aborted sequence for conversation",
			zap.String("key", conversationKey))
		return true
	}
	return false
}

// HasActiveSequence checks if there's an active sequence for a conversation
func (rs *ReplySequencer) HasActiveSequence(conversationKey string) bool {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	_, ok := rs.activeSequences[conversationKey]
	return ok
}
