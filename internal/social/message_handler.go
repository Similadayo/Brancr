package social

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	v4types "github.com/similadayo/brancr/internal/ai_v4/types"
	"github.com/similadayo/brancr/internal/checkpoint"
	"github.com/similadayo/brancr/internal/negotiation"
	"github.com/similadayo/brancr/internal/observability"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// BotGetter interface for getting Telegram bot instances
type BotGetter interface {
	GetBot(tenantID uuid.UUID) (interface{}, bool) // Returns (*tgbotapi.BotAPI, bool) but using interface{} to avoid circular dependency
}

// MessageHandler processes incoming messages from social platforms
type MessageHandler struct {
	DB             *gorm.DB
	Logger         *zap.Logger
	IntentDetector *ai.IntentDetector
	Responder      interface {
		GenerateCustomerReply(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, message string, intent string, platform string, conversationID ...uuid.UUID) (string, error)
	}
	SystemToken        string           // Brancr's system user token for provider-owned WhatsApp model (legacy)
	RespondIO          *RespondIOClient // Respond.io API client for WhatsApp integration
	GupshupClient      *GupshupClient   // Gupshup Partner API client
	EscalationFunc     func(ctx context.Context, tenantID uuid.UUID, interactionID uuid.UUID, customerName, platform, message, suggestedReply, intent, tone string, confidence float64, reason string) error
	BotGetter          BotGetter            // Optional: for Telegram message sending
	NegotiationManager *negotiation.Manager // optional negotiation manager
	UsageService       interface {          // Optional: for usage tracking
		RecordAIReply(ctx context.Context, tenantID uuid.UUID, tokensUsed int, estimatedCost float64, modelName string) error
		RecordWhatsAppMessage(ctx context.Context, tenantID uuid.UUID) error
	}
	MessageBuffer   *MessageBuffer   // Optional: for message buffering to prevent spammy replies (legacy)
	MessageSplitter *MessageSplitter // Optional: for intelligent message splitting (legacy)

	// Production architecture components
	ProductionBuffer    *ProductionMessageBuffer    // Production-grade message buffer
	ProductionProcessor *ProductionMessageProcessor // Production message processor
	ConversationLock    *ConversationLock           // Prevents double replies
	ReplySequencer      *ReplySequencer             // Handles intelligent reply sequencing
	CostControlFallback *CostControlFallback        // Cost control fallback responses
	CheckpointService   *checkpoint.Service         // Experience Checkpoint Service
}

// IncomingMessage represents a message from a social platform
type IncomingMessage struct {
	Platform           string // instagram, facebook, tiktok, youtube, whatsapp
	TenantID           uuid.UUID
	CustomerID         string // Platform-specific customer ID
	CustomerName       string
	CustomerUsername   string
	CustomerProfilePic string // Profile picture URL
	MessageID          string // Platform-specific message ID
	Content            string
	MessageType        string // text, image, video, comment
	ThreadID           string // For DMs/threads
	Timestamp          time.Time
	Metadata           map[string]interface{} // Platform-specific data
}

func buildTimeoutFallbackMessage(customerMsg string, intent *ai.DetectedIntent) string {
	msgLower := strings.ToLower(strings.TrimSpace(customerMsg))
	if strings.Contains(msgLower, "variant") ||
		strings.Contains(msgLower, "storage") ||
		strings.Contains(msgLower, "gb") ||
		strings.Contains(msgLower, "color") ||
		strings.Contains(msgLower, "colour") {
		return "I can help with that. Please tell me the exact variant you want (for example 512GB and color), and I'll confirm availability."
	}

	intentName := ""
	if intent != nil {
		intentName = strings.ToLower(strings.TrimSpace(intent.Intent))
	}

	switch intentName {
	case "order", "purchase", "negotiation", "price", "inquiry", "question":
		return "I can help right away. Please resend your request in one line, and I'll give a direct answer."
	case "payment", "refund":
		return "I can help with payment updates. Please share your payment reference or what you want to confirm."
	default:
		return "I can help with this. Please resend your request in one line and I'll respond directly."
	}
}

// ProcessMessage handles an incoming message: detects intent, decides auto-reply or escalation
// Accepts interface{} to allow flexible usage, performs type assertion to IncomingMessage
func (h *MessageHandler) ProcessMessage(ctx context.Context, msg interface{}) error {
	// Type assert to IncomingMessage
	incomingMsg, ok := msg.(IncomingMessage)
	if !ok {
		return fmt.Errorf("invalid message type: expected IncomingMessage, got %T", msg)
	}

	return h.processMessageInternal(ctx, incomingMsg)
}

// processMessageInternal contains the actual message processing logic
func (h *MessageHandler) processMessageInternal(ctx context.Context, msg IncomingMessage) error {
	// PRODUCTION ARCHITECTURE (MAIN): Buffer first, process once, generate once, send smartly
	// Production architecture is the default and primary path
	if h.ProductionBuffer == nil || h.ProductionProcessor == nil {
		h.Logger.Error("❌ Production architecture not initialized - this should never happen",
			zap.Bool("production_buffer_nil", h.ProductionBuffer == nil),
			zap.Bool("production_processor_nil", h.ProductionProcessor == nil),
			zap.Stringer("tenant_id", msg.TenantID))
		// Fallback to legacy only if production is not available (should not happen in production)
		if h.MessageBuffer != nil {
			h.Logger.Warn("⚠️ Falling back to legacy buffer (production architecture not initialized)")
			// Continue to legacy buffer code below
		} else {
			// Last resort: process immediately without buffering
			return h.processSingleMessage(ctx, msg)
		}
	} else {
		// Step 1: Save individual message to DB immediately (before buffering)
		// This ensures messages appear in UI without [Message 1/2] prefixes
		if err := h.saveMessageOnly(ctx, msg); err != nil {
			h.Logger.Warn("failed to save individual message before buffering", zap.Error(err))
			// Continue anyway - message will be saved during processing
		}

		// Step 2: Add message to buffer (no processing yet)
		// Buffer will close when: deadline expires, max size reached, or explicit close
		onBufferClose := func(messages []IncomingMessage) error {
			// This is called when buffer closes - NOW we do all processing
			return h.ProductionProcessor.ProcessBufferClose(ctx, messages)
		}

		shouldClose, messages, costLimitReached := h.ProductionBuffer.AddMessage(msg, onBufferClose)

		if costLimitReached {
			// Cost limit reached - use fallback response
			return h.processCostLimitFallback(ctx, msg)
		}

		if shouldClose && len(messages) > 0 {
			// Buffer closed immediately (max size reached)
			return onBufferClose(messages)
		}

		// Message added to buffer, will be processed when buffer closes
		return nil
	}

	// Legacy buffering (FALLBACK ONLY - should not be used in production)
	if h.MessageBuffer != nil {
		// CRITICAL: Save individual message to DB immediately (before buffering)
		if err := h.saveMessageOnly(ctx, msg); err != nil {
			h.Logger.Warn("failed to save individual message before buffering", zap.Error(err))
		}

		processCallback := func(accumulated []IncomingMessage) error {
			return h.processAccumulatedMessages(ctx, accumulated)
		}

		shouldProcess, accumulated, _, costLimitReached := h.MessageBuffer.AddMessage(ctx, msg, processCallback)

		if costLimitReached {
			return h.processCostLimitFallback(ctx, msg)
		}

		if shouldProcess && len(accumulated) > 0 {
			return h.processAccumulatedMessages(ctx, accumulated)
		}

		return nil
	}

	// No buffering - process immediately
	return h.processSingleMessage(ctx, msg)
}

// processAccumulatedMessages processes multiple messages as a single conversation turn
// Combines messages into context and processes them together
// NOTE: ctx may be background context from timer, ensure it has proper timeout
func (h *MessageHandler) processAccumulatedMessages(ctx context.Context, messages []IncomingMessage) error {
	if len(messages) == 0 {
		h.Logger.Warn("processAccumulatedMessages called with empty messages")
		return nil
	}

	// CRITICAL: Each individual message should already be saved to DB when it first arrived
	// We only need to process them together for AI context, not save them again
	// Use the most recent message as the primary message (for metadata, customer, etc.)
	primaryMsg := messages[len(messages)-1]

	// Combine all messages into a single context string for AI processing
	// This is ONLY for AI context, not for saving to DB
	combinedContent := h.combineMessages(messages)

	h.Logger.Info("📦 Processing accumulated messages",
		zap.Int("count", len(messages)),
		zap.String("combined", combinedContent),
		zap.Stringer("tenant_id", primaryMsg.TenantID),
		zap.String("platform", primaryMsg.Platform))

	// CRITICAL: Always create a new context with timeout for async processing
	// The original ctx might be cancelled by the time the timer fires
	// Use 60 seconds to handle circuit breaker delays and retries
	processCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Check if original context is still valid (for logging)
	select {
	case <-ctx.Done():
		h.Logger.Warn("⚠️ Original context was cancelled, using new context for processing",
			zap.Stringer("tenant_id", primaryMsg.TenantID))
	default:
		// Context is still valid, but we'll use new one anyway for safety
	}

	// Process using the combined content for AI, but ensure we don't save duplicate interactions
	// Create a modified message with combined content for AI processing only
	combinedMsg := primaryMsg
	combinedMsg.Content = combinedContent

	// Mark in metadata that this is a combined message so we don't save it as a new interaction
	if combinedMsg.Metadata == nil {
		combinedMsg.Metadata = make(map[string]interface{})
	}
	combinedMsg.Metadata["is_combined_message"] = true
	combinedMsg.Metadata["original_message_count"] = len(messages)

	// Process as a single message (but it won't save a new interaction if already saved)
	err := h.processSingleMessage(processCtx, combinedMsg)

	// Record AI response for conversation turn lock
	if err == nil && h.MessageBuffer != nil {
		h.MessageBuffer.RecordAIResponse(primaryMsg.TenantID, primaryMsg.CustomerID, primaryMsg.Platform)
	} else if err != nil {
		h.Logger.Error("failed to process accumulated messages",
			zap.Error(err),
			zap.Int("message_count", len(messages)),
			zap.Stringer("tenant_id", primaryMsg.TenantID))
	}

	return err
}

// combineMessages combines multiple messages into a single context string
func (h *MessageHandler) combineMessages(messages []IncomingMessage) string {
	if len(messages) == 1 {
		return messages[0].Content
	}

	var parts []string
	for i, msg := range messages {
		// Add message with context indicator if multiple messages
		if len(messages) > 1 {
			parts = append(parts, fmt.Sprintf("[Message %d/%d] %s", i+1, len(messages), msg.Content))
		} else {
			parts = append(parts, msg.Content)
		}
	}

	return strings.Join(parts, "\n\n")
}

// saveMessageOnly saves a message to the database without processing AI reply
// This is used to save individual messages immediately when they arrive, before buffering
func (h *MessageHandler) saveMessageOnly(ctx context.Context, msg IncomingMessage) error {
	// CRITICAL: Serialize this operation per customer to prevent race conditions
	// When multiple webhooks arrive simultaneously (e.g. photo burst), we must ensure
	// we don't create duplicate conversations or customers.
	serializeFunc := func() error {
		// Track customer
		customer, err := h.trackCustomer(ctx, msg)
		if err != nil {
			return fmt.Errorf("failed to track customer: %w", err)
		}

		// Create or get conversation
		conversation, err := h.createOrGetConversation(ctx, msg, customer.ID)
		var conversationID uuid.UUID
		if err != nil {
			// Try fallback
			if fallbackConv, fallbackErr := h.createConversationFallback(ctx, msg, customer.ID); fallbackErr == nil {
				conversation = fallbackConv
				conversationID = conversation.ID
			} else {
				// Continue without conversation - will be linked later
				conversationID = uuid.Nil
			}
		} else {
			conversationID = conversation.ID
		}

		// Check if message already saved (deduplication)
		if msg.MessageID != "" {
			var existing storage.Interaction
			err := h.DB.Where("tenant_id = ? AND platform = ? AND platform_message_id = ? AND direction = ?",
				msg.TenantID, msg.Platform, msg.MessageID, "incoming").First(&existing).Error
			if err == nil {
				// Already saved - check if we need to link it to conversation (fix for lock interactions)
				if existing.ConversationID == uuid.Nil && conversationID != uuid.Nil {
					existing.ConversationID = conversationID
					if existing.CustomerID == uuid.Nil {
						existing.CustomerID = customer.ID
					}
					h.DB.Save(&existing)
					h.Logger.Info("🔗 Linked existing interaction to conversation",
						zap.Stringer("interaction_id", existing.ID),
						zap.Stringer("conversation_id", conversationID))
				}
				return nil
			}
		}

		// Detect intent (lightweight)
		intent, err := h.IntentDetector.DetectIntent(ctx, msg.TenantID, msg.Content, msg.Platform)
		if err != nil {
			intent = h.IntentDetector.FallbackDetection(msg.Content)
		}

		// Create interaction with original message content (no prefixes)
		interaction := storage.Interaction{
			TenantID:          msg.TenantID,
			CustomerID:        customer.ID,
			ConversationID:    conversationID,
			Platform:          msg.Platform,
			Direction:         "incoming",
			MessageType:       msg.MessageType,
			Content:           msg.Content, // Original content, no [Message 1/2] prefixes
			PlatformMessageID: msg.MessageID,
			DetectedIntent:    intent.Intent,
			DetectedTone:      intent.Tone,
			Confidence:        intent.Confidence,
			ResponseType:      "auto_reply",
			ResponseStatus:    "pending",
			Metadata:          h.metadataToJSON(msg.Metadata),
		}

		if err := h.DB.Create(&interaction).Error; err != nil {
			return fmt.Errorf("failed to save message: %w", err)
		}

		// Update conversation
		if conversation != nil && conversation.ID != uuid.Nil {
			conversation.LastMessageAt = msg.Timestamp
			conversation.Status = "active"
			h.DB.Save(&conversation)
		}

		return nil
	}

	// Use ConversationLock to serialize if available
	if h.ConversationLock != nil {
		return h.ConversationLock.Serialize(msg.TenantID, msg.CustomerID, msg.Platform, serializeFunc)
	}

	// Fallback if no lock available (should not happen in production)
	return serializeFunc()
}

// processCostLimitFallback handles cost limit reached scenario
// Uses soft fallback: asks for clarification rather than failing
func (h *MessageHandler) processCostLimitFallback(ctx context.Context, msg IncomingMessage) error {
	h.Logger.Info("💰 Cost limit reached, using fallback response",
		zap.Stringer("tenant_id", msg.TenantID))

	// Use cost control fallback if available
	if h.CostControlFallback != nil {
		fallbackReply := h.CostControlFallback.GenerateFallbackReply(ctx, msg)

		// Send fallback reply using sequencer
		if h.ReplySequencer != nil {
			sendFunc := func(ctx context.Context, msgText string) error {
				return h.sendReply(ctx, msg, msgText)
			}
			return h.ReplySequencer.SendReply(ctx, sendFunc, fallbackReply)
		}

		// Fallback: send first message only
		if len(fallbackReply.Messages) > 0 {
			return h.sendReply(ctx, msg, fallbackReply.Messages[0])
		}
	}

	// Legacy fallback: simple message
	fallbackReply := "Got it — just to be sure, are you asking about price or availability?"
	return h.sendReply(ctx, msg, fallbackReply)
}

// processSingleMessage processes a single message (original logic)
func (h *MessageHandler) processSingleMessage(ctx context.Context, msg IncomingMessage) error {
	// CRITICAL: Deduplication - Check if this message has already been processed
	// This prevents duplicate responses when both Instagram Login and Facebook Login accounts are connected
	// For Instagram, check across all tenants since the same message might resolve to different tenants
	// if both Instagram Login and Facebook Login accounts exist
	// NOTE: Exclude "processing" interactions (lock interactions) from duplicate check
	if msg.MessageID != "" {
		var existingInteraction storage.Interaction
		var err error

		if msg.Platform == "instagram" {
			// For Instagram: Check across all tenants to catch duplicates from different account types
			// Exclude "processing" interactions (lock interactions that haven't been fully processed yet)
			err = h.DB.Where("platform = ? AND platform_message_id = ? AND direction = ? AND response_type != ?",
				msg.Platform, msg.MessageID, "incoming", "processing").First(&existingInteraction).Error
		} else {
			// For other platforms: Check within the same tenant
			// Exclude "processing" interactions (lock interactions that haven't been fully processed yet)
			err = h.DB.Where("tenant_id = ? AND platform = ? AND platform_message_id = ? AND direction = ? AND response_type != ?",
				msg.TenantID, msg.Platform, msg.MessageID, "incoming", "processing").First(&existingInteraction).Error
		}

		if err == nil {
			// Message already processed - skip to prevent duplicate responses
			h.Logger.Info("⏭️ Skipping duplicate message - already processed",
				zap.Stringer("tenant_id", msg.TenantID),
				zap.Stringer("existing_tenant_id", existingInteraction.TenantID),
				zap.String("platform", msg.Platform),
				zap.String("message_id", msg.MessageID),
				zap.Stringer("existing_interaction_id", existingInteraction.ID))
			return nil // Return nil to indicate "successfully skipped" (not an error)
		}
		// If error is not "record not found", log it but continue processing
		// (err is guaranteed to be non-nil here since we returned if err == nil)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			h.Logger.Warn("⚠️ Error checking for duplicate message, continuing anyway",
				zap.Error(err),
				zap.String("message_id", msg.MessageID))
		}
	}

	// CRITICAL: Validate message content is not empty
	// Empty messages cause the AI to generate random responses
	if strings.TrimSpace(msg.Content) == "" {
		h.Logger.Error("❌ CRITICAL: Received message with empty content - skipping AI processing",
			zap.Stringer("tenant_id", msg.TenantID),
			zap.String("platform", msg.Platform),
			zap.String("customer_id", msg.CustomerID),
			zap.String("message_id", msg.MessageID),
			zap.String("message_type", msg.MessageType),
			zap.Any("metadata", msg.Metadata))
		// Still track customer and create interaction for record-keeping, but don't generate AI reply
		customer, err := h.trackCustomer(ctx, msg)
		if err == nil {
			// Create interaction with empty content for audit trail
			interaction := storage.Interaction{
				TenantID:          msg.TenantID,
				CustomerID:        customer.ID,
				Platform:          msg.Platform,
				Direction:         "incoming",
				MessageType:       msg.MessageType,
				Content:           "[EMPTY MESSAGE - Content extraction failed]",
				PlatformMessageID: msg.MessageID,
				DetectedIntent:    "unknown",
				ResponseType:      "skipped",
				ResponseStatus:    "failed",
				Metadata:          h.metadataToJSON(msg.Metadata),
			}
			h.DB.Create(&interaction)
		}
		return fmt.Errorf("message content is empty - cannot generate AI response")
	}

	// Step 0: Check if we have a lock interaction (created by webhook handler to prevent duplicates)
	var lockInteractionID uuid.UUID
	if lockIDStr, ok := msg.Metadata["lock_interaction_id"].(string); ok {
		if lockID, err := uuid.Parse(lockIDStr); err == nil {
			lockInteractionID = lockID
			h.Logger.Info("🔓 Found lock interaction, will update it",
				zap.Stringer("lock_interaction_id", lockInteractionID))
		}
	}

	// Step 1: Track or create customer
	customer, err := h.trackCustomer(ctx, msg)
	if err != nil {
		h.Logger.Error("failed to track customer", zap.Error(err))
		// If we have a lock interaction, delete it since processing failed
		if lockInteractionID != uuid.Nil {
			h.DB.Delete(&storage.Interaction{}, lockInteractionID)
		}
		return err
	}

	// Step 2: Detect intent and tone
	intent, err := h.IntentDetector.DetectIntent(ctx, msg.TenantID, msg.Content, msg.Platform)
	if err != nil {
		h.Logger.Error("intent detection failed", zap.Error(err))
		intent = h.IntentDetector.FallbackDetection(msg.Content)
	}

	// Step 2.5: Create or get conversation
	conversation, err := h.createOrGetConversation(ctx, msg, customer.ID)
	var conversationID uuid.UUID
	if err != nil {
		h.Logger.Error("failed to create/get conversation, retrying with fallback", zap.Error(err))
		// CRITICAL: Conversation is required for inbox to work - retry with fallback
		// Try to create conversation again with more lenient matching
		if fallbackConv, fallbackErr := h.createConversationFallback(ctx, msg, customer.ID); fallbackErr == nil {
			conversation = fallbackConv
			conversationID = conversation.ID
			h.Logger.Info("✅ Created conversation using fallback method",
				zap.Stringer("conversation_id", conversationID),
				zap.String("platform", msg.Platform))
		} else {
			h.Logger.Error("❌ CRITICAL: Failed to create conversation even with fallback - messages will not appear in inbox",
				zap.Error(fallbackErr),
				zap.Stringer("tenant_id", msg.TenantID),
				zap.Stringer("customer_id", customer.ID),
				zap.String("platform", msg.Platform))
			// Still continue - we'll try to link interaction later
		}
	} else {
		conversationID = conversation.ID
	}

	// Step 3: Log interaction (update lock interaction if exists, otherwise create new)
	// CRITICAL: Skip saving interaction if this is a combined message (from message buffering)
	// Individual messages should already be saved when they first arrive
	isCombinedMessage := false
	if isCombined, ok := msg.Metadata["is_combined_message"].(bool); ok && isCombined {
		isCombinedMessage = true
		h.Logger.Info("⏭️ Skipping interaction save for combined message - individual messages already saved",
			zap.Stringer("tenant_id", msg.TenantID),
			zap.String("platform", msg.Platform),
			zap.Int("message_count", func() int {
				if count, ok := msg.Metadata["original_message_count"].(int); ok {
					return count
				}
				return 0
			}()))
	}

	// Extract events for metadata (needed for both combined and individual messages)
	events := ai.ExtractEvents(msg.Content)

	// Blueprint Section 15: Media Classification
	// Classify incoming media (images) to detect receipts, complaints, or product queries
	if msg.MessageType == "image" {
		// Pass nil for state as it's not loaded yet - classifier handles nil state gracefully
		classification := ai.ClassifyMedia(ctx, msg.Content, msg.Content, nil)
		if classification.Type != ai.MediaTypeUnknown {
			// Add classification to metadata
			if msg.Metadata == nil {
				msg.Metadata = make(map[string]interface{})
			}
			msg.Metadata["media_type"] = string(classification.Type)
			msg.Metadata["media_confidence"] = classification.Confidence

			// If receipt detected, add event
			if classification.Type == ai.MediaTypePaymentReceipt {
				events = append(events, ai.Event{Type: ai.EventPaymentReceiptReceived})
			}

			h.Logger.Info("media_classified",
				zap.String("type", string(classification.Type)),
				zap.Float64("confidence", classification.Confidence))
		}
	}

	var interaction storage.Interaction
	if !isCombinedMessage {
		metadataMap := make(map[string]interface{})
		for k, v := range msg.Metadata {
			metadataMap[k] = v
		}
		ai.AttachEventsToMetadata(metadataMap, events)
		interactionMetadataJSON := h.metadataToJSON(metadataMap)

		if lockInteractionID != uuid.Nil {
			// Update the existing lock interaction with full details
			if err := h.DB.First(&interaction, "id = ?", lockInteractionID).Error; err == nil {
				interaction.CustomerID = customer.ID
				interaction.ConversationID = conversationID
				interaction.DetectedIntent = intent.Intent
				interaction.DetectedTone = intent.Tone
				interaction.Confidence = intent.Confidence
				interaction.ResponseType = "auto_reply"
				interaction.ResponseStatus = "pending"
				interaction.Metadata = interactionMetadataJSON

				if err := h.DB.Save(&interaction).Error; err != nil {
					h.Logger.Error("failed to update lock interaction", zap.Error(err))
				} else {
					h.Logger.Info("✅ Updated lock interaction with full details",
						zap.Stringer("interaction_id", interaction.ID))
				}
			} else {
				h.Logger.Warn("⚠️ Lock interaction not found, creating new one",
					zap.Stringer("lock_interaction_id", lockInteractionID),
					zap.Error(err))
				lockInteractionID = uuid.Nil // Reset to create new interaction
			}
		}

		// If no lock interaction, create new one
		if lockInteractionID == uuid.Nil {
			interaction = storage.Interaction{
				TenantID:          msg.TenantID,
				CustomerID:        customer.ID,
				ConversationID:    conversationID, // Link to conversation (0 if creation failed)
				Platform:          msg.Platform,
				Direction:         "incoming",
				MessageType:       msg.MessageType,
				Content:           msg.Content,
				PlatformMessageID: msg.MessageID,
				DetectedIntent:    intent.Intent,
				DetectedTone:      intent.Tone,
				Confidence:        intent.Confidence,
				ResponseType:      "auto_reply",
				ResponseStatus:    "pending",
				Metadata:          interactionMetadataJSON,
			}

			if err := h.DB.Create(&interaction).Error; err != nil {
				h.Logger.Error("failed to log interaction", zap.Error(err))
			}
		}
	} else {
		// For combined messages, we still need an interaction object for the rest of the flow
		// But we'll use the most recent individual message's interaction
		// Find the most recent interaction for this customer/platform
		var recentInteraction storage.Interaction
		if err := h.DB.Where("tenant_id = ? AND customer_id = ? AND platform = ? AND direction = ?",
			msg.TenantID, customer.ID, msg.Platform, "incoming").
			Order("created_at DESC").First(&recentInteraction).Error; err == nil {
			interaction = recentInteraction
			h.Logger.Info("📋 Using most recent interaction for combined message processing",
				zap.Stringer("interaction_id", interaction.ID))
		} else {
			// Fallback: create a minimal interaction just for processing
			interaction = storage.Interaction{
				TenantID:       msg.TenantID,
				CustomerID:     customer.ID,
				ConversationID: conversationID,
				Platform:       msg.Platform,
				Direction:      "incoming",
				Content:        msg.Content, // This will have [Message 1/2] but won't be saved
				ResponseType:   "auto_reply",
				ResponseStatus: "pending",
			}
			h.Logger.Warn("⚠️ No existing interaction found for combined message, using temporary one",
				zap.Stringer("tenant_id", msg.TenantID))
		}
	}

	if interaction.ConversationID == uuid.Nil {
		// CRITICAL: If interaction was created without conversation_id, try to link it now
		h.Logger.Warn("⚠️ Interaction created without conversation_id - attempting to link",
			zap.Stringer("interaction_id", interaction.ID),
			zap.Stringer("tenant_id", msg.TenantID),
			zap.Stringer("customer_id", customer.ID),
			zap.String("platform", msg.Platform))

		// Try to find or create conversation again
		if conv, convErr := h.createOrGetConversation(ctx, msg, customer.ID); convErr == nil && conv != nil {
			interaction.ConversationID = conv.ID
			if updateErr := h.DB.Model(&interaction).Update("conversation_id", conv.ID).Error; updateErr == nil {
				h.Logger.Info("✅ Linked interaction to conversation",
					zap.Stringer("interaction_id", interaction.ID),
					zap.Stringer("conversation_id", conv.ID))
				conversationID = conv.ID
				conversation = conv
			}
		}
	}

	// Update conversation last message time
	if conversation != nil && conversation.ID != uuid.Nil {
		conversation.LastMessageAt = msg.Timestamp
		conversation.Status = "active"
		h.DB.Save(&conversation)
	}

	// Create a dashboard notification for the new incoming message
	if interaction.ID != uuid.Nil && conversationID != uuid.Nil {
		h.createNotificationForIncomingMessage(msg, customer, conversationID, interaction.ID)
	}

	// Step 3.5: Check if this is a follow-up to an escalated conversation (aftermath tracking)
	// If customer responds within 24 hours of an escalated reply, notify tenant
	h.checkAndNotifyEscalationAftermath(ctx, msg, customer, interaction.ID, conversationID)

	// Step 3.75: Update persisted conversation state (SalesConversation) from deterministic events.
	// This makes follow-ups route correctly even when the follow-up message is vague (e.g. "any update?").
	var salesConv *storage.SalesConversation
	if conversationID != uuid.Nil {
		scManager := &ai.SalesConversationManager{DB: h.DB, Logger: h.Logger}
		if updated, scErr := scManager.ApplyDeterministicSignals(ctx, msg.TenantID, customer.ID, conversationID, intent.Intent, events); scErr != nil {
			h.Logger.Warn("failed to apply deterministic sales conversation signals", zap.Error(scErr), zap.Stringer("tenant_id", msg.TenantID), zap.Stringer("conversation_id", conversationID))
		} else {
			salesConv = updated
		}
	}

	// Step 4: Smart escalation decision - prefer (state, events) with legacy fallbacks
	// If customer's message includes an explicit monetary offer, record it and evaluate escalation eligibility through the NegotiationManager.
	offer := ai.DetectOfferInText(msg.Content)
	if offer > 0 && salesConv != nil && salesConv.PriceDiscussed > 0 {
		// Try to extract item-level negotiation metadata from sales conversation (if present)
		var items []map[string]any
		var minP, maxP float64
		var productID uuid.UUID = uuid.Nil
		var maxConcessions int = 2
		if salesConv.ItemsDiscussed != "" && salesConv.ItemsDiscussed != "[]" {
			_ = json.Unmarshal([]byte(salesConv.ItemsDiscussed), &items)
			if len(items) > 0 {
				if v, ok := items[0]["negotiation_min_price"].(float64); ok {
					minP = v
				}
				if v, ok := items[0]["negotiation_max_price"].(float64); ok {
					maxP = v
				}
				if v, ok := items[0]["product_id"].(string); ok {
					if pid, err := uuid.Parse(v); err == nil {
						productID = pid
					}
				}
				if v, ok := items[0]["max_concessions"].(float64); ok {
					maxConcessions = int(v)
				}
			}
		}

		// Record customer offer in negotiation state
		if h.NegotiationManager != nil {
			if _, err := h.NegotiationManager.RecordCustomerOffer(ctx, salesConv.ID, productID, offer); err != nil {
				h.Logger.Warn("failed to record customer offer", zap.Error(err))
			}
		}

		// Determine buying intent (boolean keywords)
		contentLower := strings.ToLower(msg.Content)
		buyingIntent := strings.Contains(contentLower, "cash") || strings.Contains(contentLower, "pay") || strings.Contains(contentLower, "delivery") || strings.Contains(contentLower, "i'll buy") || strings.Contains(contentLower, "i will buy") || strings.Contains(contentLower, "buy now") || strings.Contains(contentLower, "i'm buying") || strings.Contains(contentLower, "i can pay")

		// Determine proximity threshold and tenant policy
		var policy storage.TenantPolicy
		proximity := 0.10
		if err := h.DB.Where("tenant_id = ?", msg.TenantID).First(&policy).Error; err == nil {
			if policy.NegotiationProximityPct > 0 {
				proximity = policy.NegotiationProximityPct
			}
		}

		// Use NegotiationManager to check exact escalation eligibility when available
		if h.NegotiationManager != nil {
			eligible, reason, err := h.NegotiationManager.CheckEscalationEligibility(ctx, salesConv.ID, productID, offer, buyingIntent, proximity, maxConcessions, minP)
			if err != nil {
				h.Logger.Warn("error checking escalation eligibility", zap.Error(err))
			} else if eligible {
				// Create EscalationEvent and lock state, then call existing escalate path
				if _, err := h.NegotiationManager.ExecuteEscalation(ctx, msg.TenantID, salesConv.ID, productID, salesConv.PriceDiscussed, minP, []float64{offer}, nil, reason); err != nil {
					h.Logger.Error("failed to execute escalation", zap.Error(err))
				}
				return h.escalateToTenant(ctx, msg, customer, intent, interaction.ID, events, reason)
			}
		} else {
			// Fallback behavior when NegotiationManager is not configured: legacy proximity check
			if minP > 0 && maxP > 0 {
				if offer >= minP && offer <= maxP {
					h.Logger.Info("Offer within negotiable item range; escalating for approval", zap.Float64("offer", offer), zap.Float64("min", minP), zap.Float64("max", maxP))
					return h.escalateToTenant(ctx, msg, customer, intent, interaction.ID, events, "offer_in_range_for_approval")
				}
			} else {
				if offer >= salesConv.PriceDiscussed*(1.0-proximity) {
					h.Logger.Info("Offer within proximity threshold; escalating for approval", zap.Float64("offer", offer), zap.Float64("prod_price", salesConv.PriceDiscussed), zap.Float64("proximity", proximity))
					return h.escalateToTenant(ctx, msg, customer, intent, interaction.ID, events, "offer_in_range_for_approval")
				}
			}
		}
	}

	shouldEscalate, escalationReason := h.ShouldEscalateToHuman(ctx, msg, customer, intent, conversationID, events, salesConv)

	if shouldEscalate {
		h.Logger.Info("🚨 Escalation required",
			zap.String("reason", escalationReason),
			zap.String("intent", intent.Intent),
			zap.String("tone", intent.Tone),
			zap.Float64("confidence", intent.Confidence))
		observability.IncEscalation(escalationReason)
		return h.escalateToTenant(ctx, msg, customer, intent, interaction.ID, events, escalationReason)
	}

	// Send "typing..." indicator simulation before generating reply
	// This makes the AI response feel faster and more natural
	h.sendTypingIndicator(ctx, msg)

	// 🆕 FIX: Inject threadID into context for V4 adapter action execution
	// This is CRITICAL for Instagram where recipient ID (IGSID) differs from internal customer ID
	threadIDForCtx := msg.ThreadID
	if threadIDForCtx == "" {
		threadIDForCtx = msg.CustomerID // Fallback to customerID if threadID is empty
	}
	ctxWithIDs := v4types.WithThreadID(ctx, threadIDForCtx)
	ctxWithIDs = v4types.WithPlatformCustomerID(ctxWithIDs, msg.CustomerID)

	// Pass conversationID for sales conversation tracking.
	reply, err := h.Responder.GenerateCustomerReply(ctxWithIDs, msg.TenantID, customer.ID, msg.Content, intent.Intent, msg.Platform, conversationID)
	if err != nil {
		h.Logger.Error("failed to generate reply", zap.Error(err))
		errMsg := err.Error()

		// Check if this is a circuit breaker error - send friendly fallback instead of escalating
		if strings.Contains(strings.ToLower(errMsg), "circuit breaker") && strings.Contains(strings.ToLower(errMsg), "open") {
			h.Logger.Warn("⚠️ Circuit breaker is open, sending fallback response",
				zap.String("error", errMsg),
				zap.Stringer("tenant_id", msg.TenantID))

			// Send a friendly fallback message instead of escalating
			fallbackReply := "Thanks for your message! I'm experiencing a temporary delay. Please give me a moment and try again, or feel free to reach out directly."
			if err := h.sendReply(ctx, msg, fallbackReply); err != nil {
				h.Logger.Error("failed to send fallback reply", zap.Error(err))
				// If fallback send fails, escalate as last resort
				return h.escalateToTenant(ctx, msg, customer, intent, interaction.ID, events, "circuit_breaker_open_fallback_failed")
			}
			return nil // Successfully sent fallback
		}

		// Check if this is a negotiation escalation (format: "escalation_required:reason")
		if strings.HasPrefix(errMsg, "escalation_required:") {
			// Extract escalation reason from error message
			escalationReason := strings.TrimPrefix(errMsg, "escalation_required:")
			observability.IncEscalation(escalationReason)
			return h.escalateToTenant(ctx, msg, customer, intent, interaction.ID, events, escalationReason)
		}

		// Check for context deadline exceeded - send a processing message
		if strings.Contains(errMsg, "context deadline exceeded") || strings.Contains(errMsg, "timeout") {
			h.Logger.Warn("⚠️ Request timeout, sending processing message",
				zap.String("error", errMsg),
				zap.Stringer("tenant_id", msg.TenantID))

			observability.IncTimeoutFallback()
			fallbackReply := buildTimeoutFallbackMessage(msg.Content, intent)
			if err := h.sendReply(ctx, msg, fallbackReply); err != nil {
				h.Logger.Error("failed to send timeout fallback", zap.Error(err))
				observability.IncEscalation("timeout_fallback_failed")
				return h.escalateToTenant(ctx, msg, customer, intent, interaction.ID, events, "timeout_fallback_failed")
			}
			return nil // Successfully sent fallback
		}

		// If reply generation fails for other reasons, escalate as fallback
		return h.escalateToTenant(ctx, msg, customer, intent, interaction.ID, events, "reply_generation_failed")
	}

	// Sanitization moved to AFTER parsing to avoid breaking JSON structure

	// Attach staff-state snapshot into the interaction metadata for auditability/learning (best effort).
	// This is derived data, not raw transcripts.
	if conversationID != uuid.Nil {
		var st storage.ConversationAIState
		if err := h.DB.Where("tenant_id = ? AND conversation_id = ?", msg.TenantID, conversationID).First(&st).Error; err == nil {
			// Merge into existing metadata JSON.
			var meta map[string]interface{}
			_ = json.Unmarshal([]byte(interaction.Metadata), &meta)
			if meta == nil {
				meta = map[string]interface{}{}
			}
			meta["staff_state"] = map[string]interface{}{
				"state":                    st.State,
				"mode":                     st.Mode,
				"emotion_label":            st.EmotionLabel,
				"emotion_intensity":        st.EmotionIntensity,
				"calm_streak":              st.CalmStreak,
				"risk_level":               st.RiskLevel,
				"risk_reasons":             json.RawMessage(st.RiskReasons),
				"decision_confidence":      st.DecisionConfidence,
				"last_escalation_reason":   st.LastEscalationReason,
				"last_escalation_category": st.LastEscalationCategory,
			}
			merged, _ := json.Marshal(meta)
			_ = h.DB.Model(&interaction).Update("metadata", string(merged)).Error
			interaction.Metadata = string(merged)
		}
	}

	// Step 6.5: Extract image URLs if customer asked about items
	var imageURLs []string
	if h.Responder != nil {
		if rr, ok := h.Responder.(*ai.CustomerResponder); ok && rr.RetrievalService != nil {
			if imageGetter, ok := rr.RetrievalService.(interface {
				GetItemsWithImages(ctx context.Context, tenantID uuid.UUID, message string) ([]string, error)
			}); ok {
				urls, err := imageGetter.GetItemsWithImages(ctx, msg.TenantID, msg.Content)
				if err != nil {
					h.Logger.Warn("Failed to get images", zap.Error(err), zap.String("message", msg.Content))
				} else if len(urls) > 0 {
					imageURLs = urls
					h.Logger.Info("📸 Found images to send with reply",
						zap.Int("image_count", len(imageURLs)),
						zap.Strings("image_urls", imageURLs),
						zap.String("platform", msg.Platform),
						zap.String("customer_message", msg.Content))
				} else {
					h.Logger.Info("⚠️ No images found for customer request",
						zap.String("message", msg.Content),
						zap.Stringer("tenant_id", msg.TenantID))
				}
			} else {
				h.Logger.Warn("RetrievalService does not implement GetItemsWithImages")
			}
		} else {
			h.Logger.Warn("Responder or RetrievalService is nil, cannot get images")
		}
	}

	// Step 6.5.1: Safe Render - Prevent JSON Leaks (Fix 6)
	// We use a safe render function to ensure that even if the AI sends raw JSON,
	// the customer ONLY sees the text messages.
	textMessages, extractedImages, variant := h.safeRenderReply(reply)

	// Append extracted images to any images found by retrieval service
	if len(extractedImages) > 0 {
		imageURLs = append(imageURLs, extractedImages...)
	}

	// CRITICAL: Sanitize messages individually
	// This is the final safety gate that prevents internal phrases like "Summary:", "Manager approval", etc.
	// from leaking into customer-facing messages.
	safetyGate := ai.NewCustomerOutputSafetyGate()
	for i, msg := range textMessages {
		sanitized := safetyGate.SanitizeCustomerReply(msg)
		textMessages[i] = sanitized

		// Validate reply doesn't contain forbidden patterns (lint check)
		if err := safetyGate.ValidateCustomerReply(sanitized); err != nil {
			h.Logger.Warn("⚠️ Customer reply chunk contains forbidden patterns even after sanitization",
				zap.Error(err),
				zap.String("sanitized_chunk", sanitized))
		}
	}

	// Step 7: Send reply sequence to platform (Chunking logic)
	// Feature 4: Loop through messages and send sequentially with natural pacing
	for i, txt := range textMessages {
		// Attach images to the FIRST message in the sequence
		chunkImages := []string{}
		if i == 0 {
			chunkImages = imageURLs
		}

		// Add natural delay (typing indicator) for subsequent messages
		// First message delay is handled by earlier logic (Step 6)
		if i > 0 {
			// Simulate typing duration for 2nd/3rd messages
			// 2 seconds is a good default for "natural pacing"
			h.sendTypingIndicator(ctx, msg)
			time.Sleep(2 * time.Second)
		}

		if err := h.sendReplyWithImages(ctx, msg, txt, chunkImages); err != nil {
			h.Logger.Error("❌ Failed to send reply chunk",
				zap.Error(err),
				zap.Int("chunk_index", i),
				zap.String("platform", msg.Platform),
				zap.String("customer_id", msg.CustomerID))

			// If critical failure, mark as failed
			interaction.ResponseStatus = "failed"
			interaction.FinalReply = reply // Log usage
			h.DB.Save(&interaction)
			return nil
		}
	}

	interaction.ResponseStatus = "sent"
	interaction.FinalReply = reply
	h.DB.Save(&interaction)
	// Don't return error - we still want to log the interaction

	// Record AI response for conversation turn lock (REFINEMENT 2)
	if h.MessageBuffer != nil {
		h.MessageBuffer.RecordAIResponse(msg.TenantID, msg.CustomerID, msg.Platform)
	}

	// Step 8: Update incoming interaction
	interaction.ResponseType = "auto_reply"
	interaction.ResponseStatus = "sent"
	interaction.FinalReply = reply
	h.DB.Save(&interaction)

	// Checkpoint Trigger: First AI Reply
	if h.CheckpointService != nil && conversationID != uuid.Nil {
		// Check how many AI replies exist for this conversation
		var aiCount int64
		if err := h.DB.Model(&storage.Interaction{}).
			Where("conversation_id = ? AND response_type = ?", conversationID, "auto_reply").
			Count(&aiCount).Error; err == nil {

			// If count is 1, this is the first AI reply
			if aiCount == 1 {
				// Trigger "first_reply" checkpoint
				// Using background context to ensure it runs even if request cancels
				if err := h.CheckpointService.TriggerCheckpoint(context.Background(), msg.TenantID, "first_reply", interaction.ID); err != nil {
					h.Logger.Warn("failed to trigger first_reply checkpoint", zap.Error(err))
				}
			}
		}
	}

	// Step 8.1: Usage for AI reply is now handled within CustomerResponder (RecordAIReply)
	// This ensures we capture exact token usage and costs rather than estimating here.

	// Track WhatsApp message if platform is WhatsApp
	if msg.Platform == "whatsapp" && h.UsageService != nil {
		if err := h.UsageService.RecordWhatsAppMessage(ctx, msg.TenantID); err != nil {
			h.Logger.Warn("failed to record WhatsApp message usage", zap.Error(err))
		}
	}

	// Step 8.5: CRITICAL - Ensure conversation exists and is the SAME one used for incoming interaction
	// This is essential to prevent AI responses from appearing in a separate conversation
	if conversationID == uuid.Nil || conversation == nil {
		// Try to get/create conversation again - it might have been created in the meantime
		conv, convErr := h.createOrGetConversation(ctx, msg, customer.ID)
		if convErr == nil && conv != nil {
			conversationID = conv.ID
			conversation = conv
			h.Logger.Info("✅ Retrieved conversation for outgoing interaction",
				zap.Stringer("conversation_id", conversationID),
				zap.Stringer("tenant_id", msg.TenantID),
				zap.Stringer("customer_id", customer.ID))
		} else {
			h.Logger.Error("❌ CRITICAL: Conversation missing when creating outgoing interaction - outgoing message will not appear in conversation thread",
				zap.Stringer("tenant_id", msg.TenantID),
				zap.Stringer("customer_id", customer.ID),
				zap.String("platform", msg.Platform),
				zap.Error(convErr))
			// Still create outgoing interaction but log the error
		}
	}

	// CRITICAL: Verify we're using the same conversation as the incoming interaction
	// If the incoming interaction has a conversation_id, use that one
	if interaction.ConversationID != uuid.Nil && interaction.ConversationID != conversationID {
		h.Logger.Warn("⚠️ Conversation ID mismatch - using incoming interaction's conversation_id",
			zap.Stringer("incoming_conversation_id", interaction.ConversationID),
			zap.Stringer("outgoing_conversation_id", conversationID))
		conversationID = interaction.ConversationID
	}

	// Step 8.6: Create outgoing interaction record (so it appears in conversation history)
	// Also persist expected_amount to support linking later payment confirmations that contain no amount.
	metadataJSON := "{}"
	if variant != "" {
		metadataJSON = fmt.Sprintf(`{"variant":"%s"}`, variant)
	}
	{
		var latestSalesConv *storage.SalesConversation
		if conversationID != uuid.Nil {
			var sc storage.SalesConversation
			if err := h.DB.Where("tenant_id = ? AND customer_id = ? AND conversation_id = ?", msg.TenantID, customer.ID, conversationID).
				Order("updated_at DESC").
				First(&sc).Error; err == nil {
				latestSalesConv = &sc
			}
		}
		if latestSalesConv == nil {
			latestSalesConv = salesConv
		}
		if latestSalesConv != nil {
			// Important: negotiated prices can change. Only persist expected_amount when we are
			// actually at/near payment (or have a draft awaiting confirmation), not during early negotiation.
			stage := strings.ToLower(strings.TrimSpace(latestSalesConv.Stage))
			commitment := strings.ToLower(strings.TrimSpace(latestSalesConv.CommitmentLevel))
			shouldPersistExpected := false
			if strings.TrimSpace(latestSalesConv.PendingOrderDraft) != "" {
				shouldPersistExpected = true
			} else if stage == "payment_instructions" || stage == "commitment_confirmation" {
				shouldPersistExpected = true
			} else if commitment == "committed" || commitment == "payment_ready" {
				shouldPersistExpected = true
			}

			expectedAmount := 0.0
			expectedCurrency := "NGN"
			if shouldPersistExpected && strings.TrimSpace(latestSalesConv.PendingOrderDraft) != "" {
				var draft ai.OrderDraft
				if err := json.Unmarshal([]byte(latestSalesConv.PendingOrderDraft), &draft); err == nil {
					if draft.TotalAmount > 0 {
						expectedAmount = draft.TotalAmount
					}
					if strings.TrimSpace(draft.Currency) != "" {
						expectedCurrency = draft.Currency
					}
				}
			}
			if shouldPersistExpected && expectedAmount <= 0 && latestSalesConv.PriceDiscussed > 0 {
				expectedAmount = latestSalesConv.PriceDiscussed
			}
			if shouldPersistExpected && expectedAmount > 0 {
				m := map[string]interface{}{}
				ai.AttachExpectedAmountToMetadata(m, expectedAmount, expectedCurrency)
				if b, err := json.Marshal(m); err == nil {
					metadataJSON = string(b)
				}
			}
		}
	}
	outgoingInteraction := storage.Interaction{
		TenantID:       msg.TenantID,
		CustomerID:     customer.ID,
		ConversationID: conversationID,
		Platform:       msg.Platform,
		Direction:      "outgoing",
		MessageType:    "text",
		Content:        reply,
		ResponseType:   "auto_reply",
		ResponseStatus: "sent",
		FinalReply:     reply,
		DetectedIntent: intent.Intent,
		DetectedTone:   intent.Tone,
		Confidence:     intent.Confidence,
		Metadata:       metadataJSON, // Valid JSON object (empty object, not empty string)
	}
	if err := h.DB.Create(&outgoingInteraction).Error; err != nil {
		h.Logger.Warn("failed to create outgoing interaction for auto-reply", zap.Error(err))
		// Continue - not critical, but log it
	} else {
		h.Logger.Info("✅ Created outgoing interaction for AI auto-reply",
			zap.Stringer("interaction_id", outgoingInteraction.ID),
			zap.Stringer("conversation_id", conversationID),
			zap.Stringer("tenant_id", msg.TenantID),
			zap.Stringer("customer_id", customer.ID),
			zap.String("platform", msg.Platform),
			zap.String("message_preview", func() string {
				if len(reply) > 50 {
					return reply[:50] + "..."
				}
				return reply
			}()))

		// Update conversation's last message time after outgoing message is sent
		if conversationID != uuid.Nil {
			if conversation == nil || conversation.ID != conversationID {
				// Reload conversation if needed
				var conv storage.Conversation
				if err := h.DB.First(&conv, "id = ?", conversationID).Error; err == nil {
					conversation = &conv
				}
			}
			if conversation != nil && conversation.ID == conversationID {
				conversation.LastMessageAt = time.Now()
				conversation.Status = "active"
				if err := h.DB.Save(&conversation).Error; err != nil {
					h.Logger.Warn("failed to update conversation after outgoing message", zap.Error(err))
				}
			}
		} else {
			h.Logger.Warn("⚠️ Outgoing interaction created but conversationID is uuid.Nil - message may not appear in inbox",
				zap.Stringer("interaction_id", outgoingInteraction.ID),
				zap.Stringer("tenant_id", msg.TenantID),
				zap.Stringer("customer_id", customer.ID))
		}
	}

	// Step 9: Save to conversation memory
	h.saveToMemory(ctx, msg.TenantID, customer.ID, msg.Content, reply)

	h.Logger.Info("✅ Auto-replied to customer",
		zap.Stringer("tenant_id", msg.TenantID),
		zap.Stringer("customer_id", customer.ID),
		zap.String("platform", msg.Platform),
		zap.String("intent", intent.Intent),
	)

	return nil
}

// checkAndNotifyEscalationAftermath checks if customer responded after an escalated conversation
func (h *MessageHandler) checkAndNotifyEscalationAftermath(ctx context.Context, msg IncomingMessage, customer *storage.Customer, interactionID uuid.UUID, conversationID uuid.UUID) {
	// Check if there's a recent escalated interaction in this conversation (within last 24 hours)
	cutoffTime := time.Now().Add(-24 * time.Hour)

	var recentEscalatedInteraction storage.Interaction
	query := h.DB.Where("tenant_id = ? AND customer_id = ? AND platform = ? AND response_type = ? AND response_status = ? AND created_at > ?",
		msg.TenantID, customer.ID, msg.Platform, "manual", "sent", cutoffTime).
		Order("created_at DESC").First(&recentEscalatedInteraction)

	if query.Error != nil {
		// No recent escalated interaction - this is a new conversation
		return
	}

	// Found a recent escalated reply - this is a follow-up!
	// Log for now - we can enhance this later to send notifications
	h.Logger.Info("📬 Customer follow-up after escalated conversation",
		zap.Stringer("tenant_id", msg.TenantID),
		zap.Stringer("original_interaction_id", recentEscalatedInteraction.ID),
		zap.Stringer("new_interaction_id", interactionID),
		zap.String("customer_message", msg.Content))

	// TODO: Send notification to tenant via EscalationHandler about this follow-up
	// This will be handled after the main reply logic decides what to do
}

// createNotificationForIncomingMessage stores a dashboard notification for a new incoming message
func (h *MessageHandler) createNotificationForIncomingMessage(msg IncomingMessage, customer *storage.Customer, conversationID, interactionID uuid.UUID) {
	h.Logger.Debug("notification suppressed for incoming message",
		zap.Stringer("tenant_id", msg.TenantID),
		zap.Stringer("customer_id", customer.ID),
		zap.Stringer("conversation_id", conversationID),
		zap.Stringer("interaction_id", interactionID))
}

// trackCustomer ensures customer exists in database
func (h *MessageHandler) trackCustomer(ctx context.Context, msg IncomingMessage) (*storage.Customer, error) {
	var customer storage.Customer
	err := h.DB.Where("tenant_id = ? AND channel = ? AND platform_id = ?",
		msg.TenantID, msg.Platform, msg.CustomerID).First(&customer).Error

	if err == nil {
		// Update last message time
		customer.LastMessageAt = msg.Timestamp

		// Update customer name if we have new information
		// Always update when we have a name from the message (not just when empty)
		if msg.CustomerName != "" {
			if customer.DisplayName == "" || msg.CustomerName != customer.DisplayName {
				customer.DisplayName = msg.CustomerName
				h.Logger.Info("✅ Updated customer display name",
					zap.Stringer("customer_id", customer.ID),
					zap.String("old_name", customer.DisplayName),
					zap.String("new_name", msg.CustomerName))
			}
		} else if customer.DisplayName == "" {
			// Try to extract name from message content as fallback
			extractedName := extractNameFromMessageContent(msg.Content)
			if extractedName != "" {
				customer.DisplayName = extractedName
				h.Logger.Info("✅ Extracted customer name from message",
					zap.Stringer("customer_id", customer.ID),
					zap.String("extracted_name", extractedName))
			}
		}

		// Update username if we have a better one (always update if we have new username)
		if msg.CustomerUsername != "" {
			if customer.Username == "" || msg.CustomerUsername != customer.Username {
				customer.Username = msg.CustomerUsername
				h.Logger.Info("✅ Updated customer username",
					zap.Stringer("customer_id", customer.ID),
					zap.String("old_username", customer.Username),
					zap.String("new_username", msg.CustomerUsername))
			}
		}

		// Update profile picture if we have a new one (always update when available)
		if msg.CustomerProfilePic != "" {
			if customer.ProfilePicURL == "" || msg.CustomerProfilePic != customer.ProfilePicURL {
				customer.ProfilePicURL = msg.CustomerProfilePic
				h.Logger.Info("✅ Updated customer profile picture",
					zap.Stringer("customer_id", customer.ID),
					zap.String("profile_pic_url", msg.CustomerProfilePic))
			}
		}

		h.DB.Save(&customer)
		return &customer, nil
	}

	// Extract name from message if available (for cases like payment messages)
	extractedName := extractNameFromMessageContent(msg.Content)
	if extractedName != "" && msg.CustomerName == "" {
		msg.CustomerName = extractedName
	}

	// For Instagram: If we don't have a name, use a better fallback than "Instagram User"
	displayName := msg.CustomerName
	if displayName == "" && msg.Platform == "instagram" {
		// Try username first, then fallback to a more descriptive name
		if msg.CustomerUsername != "" {
			displayName = msg.CustomerUsername
		} else if len(msg.CustomerID) >= 8 {
			// Use a more descriptive fallback that can be updated later
			displayName = fmt.Sprintf("Instagram Customer (%s)", msg.CustomerID[:8]) // First 8 chars of ID
		} else {
			displayName = "Instagram Customer"
		}
	}

	// Create new customer
	customer = storage.Customer{
		TenantID:      msg.TenantID,
		Channel:       msg.Platform,
		PlatformID:    msg.CustomerID,
		DisplayName:   displayName,
		Username:      msg.CustomerUsername,
		ProfilePicURL: msg.CustomerProfilePic,
		LastMessageAt: msg.Timestamp,
	}

	if err := h.DB.Create(&customer).Error; err != nil {
		return nil, err
	}

	return &customer, nil
}

// createOrGetConversation creates or gets a conversation for the customer on this platform
func (h *MessageHandler) createOrGetConversation(ctx context.Context, msg IncomingMessage, customerID uuid.UUID) (*storage.Conversation, error) {
	// For Instagram: ALWAYS use CustomerID (senderID) as PlatformThreadID to ensure all messages from same customer are grouped
	// This is critical because Instagram doesn't have a consistent thread ID like other platforms
	var platformThreadID string
	if msg.Platform == "instagram" {
		// For Instagram, use CustomerID (which is the sender's IGSID) as the thread ID
		// This ensures all messages from the same customer are grouped together
		platformThreadID = msg.CustomerID
		h.Logger.Info("🔗 Using CustomerID as PlatformThreadID for Instagram",
			zap.String("customer_id", msg.CustomerID),
			zap.String("platform_thread_id", platformThreadID))
	} else {
		// For other platforms, use ThreadID if available, otherwise fall back to CustomerID
		platformThreadID = msg.ThreadID
		if platformThreadID == "" {
			platformThreadID = msg.CustomerID
		}
	}

	// First, try to find conversation by tenant + customer + platform (most reliable for Instagram)
	// This ensures we always group by customer, regardless of thread ID variations
	var conversation storage.Conversation
	err := h.DB.Where("tenant_id = ? AND customer_id = ? AND platform = ?",
		msg.TenantID, customerID, msg.Platform).
		Order("created_at DESC").First(&conversation).Error

	if err == nil {
		// Conversation exists, update it
		conversation.LastMessageAt = msg.Timestamp
		conversation.Status = "active"
		// Update platform_thread_id to ensure consistency
		if conversation.PlatformThreadID != platformThreadID {
			conversation.PlatformThreadID = platformThreadID
			h.Logger.Info("🔄 Updated PlatformThreadID for existing conversation",
				zap.Stringer("conversation_id", conversation.ID),
				zap.String("old_thread_id", conversation.PlatformThreadID),
				zap.String("new_thread_id", platformThreadID))
		}
		h.DB.Save(&conversation)
		return &conversation, nil
	}

	// Create new conversation
	conversation = storage.Conversation{
		TenantID:         msg.TenantID,
		CustomerID:       customerID,
		Platform:         msg.Platform,
		PlatformThreadID: platformThreadID,
		Status:           "active",
		LastMessageAt:    msg.Timestamp,
	}

	if err := h.DB.Create(&conversation).Error; err != nil {
		return nil, fmt.Errorf("failed to create conversation: %w", err)
	}

	h.Logger.Info("✅ Created new conversation",
		zap.Stringer("tenant_id", msg.TenantID),
		zap.Stringer("customer_id", customerID),
		zap.String("platform", msg.Platform),
		zap.String("thread_id", platformThreadID))

	return &conversation, nil
}

// createConversationFallback creates a conversation with more lenient matching (for cases where normal creation fails)
func (h *MessageHandler) createConversationFallback(ctx context.Context, msg IncomingMessage, customerID uuid.UUID) (*storage.Conversation, error) {
	// Try to find existing conversation by tenant + customer + platform (without thread_id matching)
	var existing storage.Conversation
	err := h.DB.Where("tenant_id = ? AND customer_id = ? AND platform = ?",
		msg.TenantID, customerID, msg.Platform).
		Order("created_at DESC").First(&existing).Error

	if err == nil {
		// Found existing conversation - update it
		existing.LastMessageAt = msg.Timestamp
		existing.Status = "active"
		// Update platform_thread_id if it's empty or different
		if existing.PlatformThreadID == "" || existing.PlatformThreadID != msg.ThreadID {
			existing.PlatformThreadID = msg.ThreadID
		}
		h.DB.Save(&existing)
		return &existing, nil
	}

	// Create new conversation with fallback thread ID
	platformThreadID := msg.ThreadID
	if platformThreadID == "" {
		platformThreadID = msg.CustomerID
	}
	if platformThreadID == "" {
		platformThreadID = fmt.Sprintf("%s_%d", msg.Platform, customerID)
	}

	conversation := storage.Conversation{
		TenantID:         msg.TenantID,
		CustomerID:       customerID,
		Platform:         msg.Platform,
		PlatformThreadID: platformThreadID,
		Status:           "active",
		LastMessageAt:    msg.Timestamp,
	}

	if err := h.DB.Create(&conversation).Error; err != nil {
		return nil, fmt.Errorf("failed to create conversation (fallback): %w", err)
	}

	return &conversation, nil
}

// ShouldEscalateToHuman determines if a message needs human intervention
// Returns (shouldEscalate, reason)
// This is the PROVEN escalation logic - exported so ProductionMessageProcessor can use it
func (h *MessageHandler) ShouldEscalateToHuman(ctx context.Context, msg IncomingMessage, customer *storage.Customer, intent *ai.DetectedIntent, conversationID uuid.UUID, events []ai.Event, salesConv *storage.SalesConversation) (bool, string) {
	messageLower := strings.ToLower(msg.Content)
	isMediaType := msg.MessageType == "audio" || msg.MessageType == "image" || msg.MessageType == "video" || msg.MessageType == "document" || msg.MessageType == "sticker"

	// If tenant or inbox is explicitly set to human mode, escalate immediately with reason
	// Guard: if no DB is attached (e.g., in simple unit tests), skip DB checks
	if h.DB != nil {
		// Check per-inbox/connected account ai_mode first
		var acc storage.SocialAccount
		if err := h.DB.Where("tenant_id = ? AND platform = ? AND connected = ?", msg.TenantID, msg.Platform, true).First(&acc).Error; err == nil {
			mode := strings.ToLower(strings.TrimSpace(acc.AiMode))
			if mode == "human" {
				return true, "inbox_mode_human"
			}
		}

		// Fallback to tenant-wide ai_human_mode
		var tenant storage.Tenant
		if err := h.DB.Select("id,ai_human_mode").First(&tenant, msg.TenantID).Error; err == nil {
			mode := strings.ToLower(strings.TrimSpace(tenant.AIHumanMode))
			if mode == "human" {
				return true, "tenant_mode_human"
			}
		}
	}

	// Event-driven fast path (backward compatible): use deterministic events when sufficient.
	if events == nil {
		events = ai.ExtractEvents(msg.Content)
	}
	if d := ai.DecideEscalationFromEvents(events); d.Handled {
		return d.ShouldEscalate, d.Reason
	}

	// NEW: Bypass escalation for normal order/conversation intents
	// This prevents "I want X" or "Confirm my order" from triggering deeper escalation checks
	// Let the AI (ResponseGenerator) handle these
	safeIntents := []string{"order", "purchase", "inquiry", "greeting", "price_check", "confirm_order", "order_status", "thank_you", "goodbye"}
	isSafeIntent := false
	for _, safe := range safeIntents {
		if intent != nil && strings.ToLower(intent.Intent) == safe {
			isSafeIntent = true
			break
		}
	}

	// If intent is clearly "order" or "purchase", DO NOT ESCALATE here
	// The AI is capable of handling order collection and confirmation
	if isSafeIntent && intent.Confidence > 0.4 {
		return false, ""
	}

	// NEW: Check for significant complaints based on intent
	if intent != nil && (intent.Intent == "complaint" || intent.Intent == "issue") && intent.Confidence > 0.7 {
		return true, "customer_complaint_requires_human"
	}

	// State-driven fallback: if we're already in a human-needed stage, keep routing to human
	// even when the follow-up message is vague and event extraction yields nothing.
	if salesConv != nil {
		stage := strings.ToLower(strings.TrimSpace(salesConv.Stage))
		switch stage {
		// case "needs_human":
		// 	 return true, "conversation_in_needs_human_stage"
		case "payment_confirmation":
			return true, "payment_confirmation_requires_tenant_verification"
		case "refund":
			return true, "refund_requires_human_judgment"
		}
	}

	// 2. Payment confirmation check (moved after intent bypass)
	isPaymentConfirmation := h.isPaymentConfirmation(msg.Content)
	if isPaymentConfirmation && intent.Confidence > 0.35 {
		return true, "payment_confirmation_requires_tenant_verification"
	}

	// 3. Payment mode cues (Pod check)
	paymentModePOD := strings.Contains(messageLower, "pay on delivery") || strings.Contains(messageLower, "cash on delivery") || strings.Contains(messageLower, "pod")
	paymentModePrepay := strings.Contains(messageLower, "bank transfer") || strings.Contains(messageLower, "transfer") || strings.Contains(messageLower, "send account") || strings.Contains(messageLower, "account number")

	// Pay-on-delivery: escalate only when it's a selection/commitment, not a question.
	if paymentModePOD {
		isInquiry := strings.Contains(messageLower, "do you") || strings.Contains(messageLower, "can i") || strings.Contains(messageLower, "can we") ||
			strings.Contains(messageLower, "is it") || strings.Contains(messageLower, "is there") || strings.Contains(messageLower, "available") ||
			strings.Contains(messageLower, "possible") || strings.Contains(messageLower, "how") || strings.Contains(messageLower, "?")
		if !isInquiry {
			return true, "payment_mode_confirmation_needed"
		}
		return false, ""
	}

	// Do not escalate when customer asks for account/bank details; handle via auto-reply flow.
	if paymentModePrepay {
		return false, ""
	}

	// 4. Simple ordering / option selections should be handled by the AI (no escalation).
	trimmed := strings.TrimSpace(messageLower)
	isBareOption := trimmed == "1" || trimmed == "2" || trimmed == "3" || trimmed == "4" || trimmed == "5"
	orderCue := strings.Contains(messageLower, "i'll take") || strings.Contains(messageLower, "i want one") || strings.Contains(messageLower, "add one") || strings.Contains(messageLower, "go for") || strings.Contains(messageLower, "option ")
	if intent.Intent == "order" || orderCue || isBareOption {
		return false, ""
	}

	// 2. Check conversation history for repeated frustration
	if conversationID != uuid.Nil {
		var recentInteractions []storage.Interaction
		h.DB.Where("conversation_id = ? AND direction = ? AND created_at > ?",
			conversationID, "incoming", time.Now().Add(-24*time.Hour)).
			Order("created_at DESC").
			Limit(5).
			Find(&recentInteractions)

		// Count negative/urgent tones in recent messages
		negativeCount := 0
		for _, inter := range recentInteractions {
			if inter.DetectedTone == "negative" || inter.DetectedTone == "urgent" {
				negativeCount++
			}
		}

		// If 3+ negative messages in last 24 hours, escalate
		if negativeCount >= 3 {
			return true, fmt.Sprintf("repeated_frustration_%d_negative_messages", negativeCount)
		}

		// Persistent negotiation: use sales conversation counters when available; fallback to a conservative scanning approach.
		if salesConv != nil {
			// Do NOT escalate negotiation automatically; only escalate when persistence and persuasion rules are met
			if salesConv.SameOfferCount >= 3 && salesConv.AIPersuasionCount >= 3 {
				// require that the last offer is close to the discussed price (within 10%) and the customer expresses buying intent
				if salesConv.PriceDiscussed > 0 {
					proximity := math.Abs(salesConv.LastOfferAmount-salesConv.PriceDiscussed) / salesConv.PriceDiscussed
					if proximity <= 0.10 {
						// check for buying intent cues in the current message
						if strings.Contains(messageLower, "cash") || strings.Contains(messageLower, "pay") || strings.Contains(messageLower, "now") || strings.Contains(messageLower, "last") || strings.Contains(messageLower, "final") {
							return true, "persistent_negotiation_persistent_offer"
						}
					}
				}
			}
		} else {
			// conservative fallback: require more evidence before escalation
			offerCount := 0
			for _, inter := range recentInteractions {
				if ai.DetectOfferInText(inter.Content) > 0 {
					offerCount++
				}
			}
			if offerCount >= 5 {
				return true, fmt.Sprintf("persistent_negotiation_%d_offers", offerCount)
			}
		}

		// Check for prior *real* escalations using EscalationEvent or NegotiationState (alerts must NOT count as escalations)
		// Only consider escalation records created/updated in the last 7 days for this sales conversation
		if salesConv != nil {
			var evt []storage.EscalationEvent
			if err := h.DB.Where("sales_conversation_id = ? AND created_at > ?", salesConv.ID, time.Now().Add(-7*24*time.Hour)).Limit(1).Find(&evt).Error; err == nil && len(evt) > 0 {
				// Customer had a prior real escalation event — re-escalate only for clearly urgent signals
				if intent.Tone == "negative" || intent.Tone == "urgent" {
					return true, "follow_up_after_previous_escalation"
				}
			}

			// Also check persisted negotiation state (if any) for escalated status
			var ns []storage.NegotiationState
			if err := h.DB.Where("sales_conversation_id = ? AND status = ? AND updated_at > ?", salesConv.ID, "escalated", time.Now().Add(-7*24*time.Hour)).Limit(1).Find(&ns).Error; err == nil && len(ns) > 0 {
				if intent.Tone == "negative" || intent.Tone == "urgent" {
					return true, "follow_up_after_previous_escalation"
				}
			}
		}
		// If we don't have a sales conversation, DO NOT use generic alerts or interaction flags as escalation evidence
		// Alerts are informational only and should never flip escalation state.
	}

	// 3. High confidence + Critical intent + Negative/Urgent tone → Escalate
	// Only escalate if we're VERY confident it's a real issue that needs human judgment
	// This allows AI to handle routine questions like "What's your refund policy?"
	// Critical intents that often require human judgment even when AI can respond:
	// Keep this narrow to avoid premature escalations. Refund demands and payment confirmations are handled earlier by events.
	isCriticalIntent := intent.Intent == "delivery_issue"
	isNegativeTone := intent.Tone == "negative" || intent.Tone == "urgent"

	if isCriticalIntent && intent.Confidence > 0.85 && isNegativeTone {
		return true, fmt.Sprintf("high_confidence_critical_intent_%s_%s", intent.Intent, intent.Tone)
	}

	// 4. Payment-related message handling - context-aware
	// Customer asking to PAY (provide account): Can auto-reply with stored details
	// Customer CONFIRMS they paid: Must escalate for tenant to verify & send receipt
	// Customer asking WHEN TO PAY: Can auto-reply with POD info if applicable

	isPaymentInquiry := strings.Contains(messageLower, "how to pay") ||
		strings.Contains(messageLower, "payment method") ||
		strings.Contains(messageLower, "where to send") ||
		strings.Contains(messageLower, "account number") ||
		strings.Contains(messageLower, "bank details") ||
		strings.Contains(messageLower, "payment details") ||
		strings.Contains(messageLower, "account to pay") ||
		strings.Contains(messageLower, "can i get the account")

	isInvoiceRequest := strings.Contains(messageLower, "invoice") ||
		strings.Contains(messageLower, "receipt")

	// Payment inquiry (asking for account details) - can be auto-replied with stored details
	// Check this FIRST before other escalation rules to avoid being overridden
	if (isPaymentInquiry || intent.Intent == "account") && !isPaymentConfirmation {
		return false, ""
	}

	// Escalate if customer confirms they paid - handled early above.

	// Invoice/receipt requests are informational by default.
	// Payment confirmations escalate earlier; otherwise the bot can explain that receipts are issued after verification.
	if isInvoiceRequest {
		return false, ""
	}

	// 5. Refund/complaint policy:
	// - Refund DEMANDS escalate (handled by deterministic events earlier: EventRefundDemand).
	// - Refund POLICY questions do NOT auto-escalate.
	// - Complaints go to AI Support-first; escalate only if threats/abuse or repeated frustration signals.
	// Therefore, do NOT auto-escalate on intent alone here.

	// 6. Very low confidence - can't understand the message (escalate to human)
	// For media messages (audio/image/video/document/sticker), avoid knee-jerk escalations unless payment-related
	// Increased threshold from 0.25 to 0.20 and require unknown/unrecognized intent to reduce false escalations
	if intent.Confidence < 0.20 {
		if isMediaType && !(intent.Intent == "payment" || isPaymentConfirmation || isInvoiceRequest) {
			return false, ""
		}
		// Only escalate if intent is truly unknown/unrecognized, not just low confidence on a valid intent
		if intent.Intent == "unknown" || intent.Intent == "unrecognized" {
			// CRITICAL FIX: Only escalate if the tone is NEGATIVE or URGENT
			// Don't escalate neutral/positive unknown messages (e.g., "That's cool!", "Okay", "lol")
			// Let the AI General Chat handle those.
			if intent.Tone == "negative" || intent.Tone == "urgent" || intent.Tone == "confusion" {
				return true, fmt.Sprintf("very_low_confidence_%.2f_unknown_intent_negative_tone", intent.Confidence)
			}
			return false, ""
		}
		// For other intents with low confidence, let AI try to handle it
		return false, ""
	}

	// 7. Intent detector explicitly says it requires escalation
	// But only if confidence is reasonable (not a false positive). Never escalate for negotiation/price_objection here.
	// Also explicitly handle "escalation" intent.
	if intent.Intent == "escalation" && intent.Confidence > 0.65 {
		return true, "explicit_escalation_intent"
	}

	// REFINEMENT: Do NOT escalate for greetings, feedback, or general inquiries even if flagged, unless urgent/negative
	isSafeIntent = isSafeIntent || intent.Intent == "greeting" || intent.Intent == "feedback" || intent.Intent == "general_inquiry"

	// Only follow "RequiresEscalation" flag if confidence is HIGH (0.8+) and it's not a safe intent
	if intent.RequiresEscalation && intent.Confidence > 0.8 && !isSafeIntent && intent.Intent != "negotiation" && intent.Intent != "price_objection" {
		return true, "intent_detector_flagged_escalation"
	}

	// 8. Complex/ambiguous messages that need human judgment
	// Check message length and complexity
	wordCount := len(strings.Fields(msg.Content))
	if wordCount > 100 && intent.Confidence < 0.5 {
		// Very long message with low confidence - likely complex issue
		return true, "complex_message_low_confidence"
	}

	// Default: don't escalate, let AI handle it
	return false, ""
}

// escalateToTenant sends message to tenant's Telegram control center
func (h *MessageHandler) escalateToTenant(ctx context.Context, msg IncomingMessage, customer *storage.Customer, intent *ai.DetectedIntent, interactionID uuid.UUID, events []ai.Event, reason string) error {
	// Load interaction first
	var interaction storage.Interaction
	if err := h.DB.First(&interaction, interactionID).Error; err != nil {
		return fmt.Errorf("interaction not found: %w", err)
	}

	// Generate suggested reply
	// Get conversationID for sales tracking
	var conversationID uuid.UUID
	conversation, _ := h.createOrGetConversation(ctx, msg, customer.ID)
	if conversation != nil {
		conversationID = conversation.ID
		}
		suggestedReply := ""
		if events == nil {
			events = ai.ExtractEvents(msg.Content)
		}
		isPaymentConfirmation := ai.HasEvent(events, ai.EventPaymentConfirmation) || h.isPaymentConfirmation(msg.Content)
		if isPaymentConfirmation {
			// Never let product-catalog context leak into payment confirmations.
			hasMedia := strings.TrimSpace(strings.ToLower(msg.MessageType)) != "" && strings.ToLower(strings.TrimSpace(msg.MessageType)) != "text"
			lower := strings.ToLower(msg.Content)
			switch {
			case hasMedia || strings.Contains(lower, "receipt") || strings.Contains(lower, "proof") || strings.Contains(lower, "screenshot") || strings.Contains(lower, "evidence"):
				suggestedReply = "Thanks — I’ve received your payment proof. I’m confirming it now and will update you shortly."
			default:
				suggestedReply = "Thanks — please share your payment proof/receipt (screenshot) so I can confirm and update you."
			}
		} else {
			suggestedReply, _ = h.Responder.GenerateCustomerReply(ctx, msg.TenantID, customer.ID, msg.Content, intent.Intent, msg.Platform, conversationID)
		}

	// Send escalation via escalation handler (include reason)
	if h.EscalationFunc != nil {
		if err := h.EscalationFunc(ctx, msg.TenantID, interactionID, customer.DisplayName, msg.Platform, msg.Content, suggestedReply, intent.Intent, intent.Tone, intent.Confidence, reason); err != nil {
			h.Logger.Error("failed to escalate", zap.Error(err))
			return err
		}
	}

	// Update interaction
	interaction.ResponseType = "escalated"
	interaction.ResponseStatus = "pending"
	interaction.SuggestedReply = suggestedReply
	interaction.EscalationReason = reason
	now := time.Now()
	interaction.EscalatedAt = &now
	h.DB.Save(&interaction)

		// If this is a payment confirmation, create pending payment record immediately
		if isPaymentConfirmation {
			h.createPendingPayment(ctx, msg.TenantID, customer.ID, &interaction, msg.Content)
		}

	h.Logger.Info("📤 Escalated to tenant",
		zap.Stringer("tenant_id", msg.TenantID),
		zap.Stringer("interaction_id", interactionID),
		zap.String("intent", intent.Intent),
	)

	return nil
}

// sendReplyWithImages sends a reply with optional images to the customer on the platform
func (h *MessageHandler) sendReplyWithImages(ctx context.Context, msg IncomingMessage, reply string, imageURLs []string) error {
	// First send the text reply (with intelligent splitting if enabled)
	if err := h.sendReply(ctx, msg, reply); err != nil {
		return err
	}

	// Then send images if available
	if len(imageURLs) > 0 {
		// For Instagram, send all images in one request (up to 10)
		// For other platforms, send individually (up to 5)
		if msg.Platform == "instagram" {
			// Limit to 10 images for Instagram (API limit)
			maxImages := 10
			if len(imageURLs) > maxImages {
				imageURLs = imageURLs[:maxImages]
			}

			var acc storage.SocialAccount
			if err := h.DB.Where("tenant_id = ? AND platform = ? AND connected = ?",
				msg.TenantID, msg.Platform, true).First(&acc).Error; err == nil {
				recipientID := msg.ThreadID
				if recipientID == "" || recipientID == acc.ExternalID || recipientID == acc.PageID {
					recipientID = msg.CustomerID
				}
				h.Logger.Info("📤 Sending Instagram images",
					zap.Int("image_count", len(imageURLs)),
					zap.String("recipient_id", recipientID),
					zap.Strings("image_urls", imageURLs))
				if err := h.sendInstagramImageReply(ctx, &acc, recipientID, imageURLs...); err != nil {
					h.Logger.Error("❌ Failed to send Instagram images",
						zap.Error(err),
						zap.Int("image_count", len(imageURLs)),
						zap.String("recipient_id", recipientID))
				} else {
					h.Logger.Info("✅ Successfully sent Instagram images",
						zap.Int("image_count", len(imageURLs)))
				}
			} else {
				h.Logger.Warn("⚠️ No connected Instagram account found for sending images",
					zap.Stringer("tenant_id", msg.TenantID),
					zap.String("platform", msg.Platform),
					zap.Error(err))
			}
		} else {
			// For other platforms, send images individually
			maxImages := 5
			if len(imageURLs) > maxImages {
				imageURLs = imageURLs[:maxImages]
			}

			for _, imageURL := range imageURLs {
				if err := h.sendImageReply(ctx, msg, imageURL); err != nil {
					h.Logger.Warn("Failed to send image",
						zap.String("image_url", imageURL),
						zap.Error(err))
					// Continue with other images even if one fails
				}
			}
		}
	}

	return nil
}

// sendImageReply sends an image to the customer on the platform
func (h *MessageHandler) sendImageReply(ctx context.Context, msg IncomingMessage, imageURL string) error {
	// Handle Telegram differently - it doesn't use social accounts
	if msg.Platform == "telegram" {
		return h.sendTelegramImageReply(ctx, msg, imageURL)
	}

	// Get social account
	var acc storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ? AND connected = ?",
		msg.TenantID, msg.Platform, true).First(&acc).Error; err != nil {
		// Enhanced diagnostic logging for debugging connection issues
		h.Logger.Warn("🔴 Failed to find connected social account for image reply",
			zap.Error(err),
			zap.Stringer("tenant_id", msg.TenantID),
			zap.String("platform", msg.Platform))
		return fmt.Errorf("no connected %s account", msg.Platform)
	}

	// Platform-specific image sending
	switch msg.Platform {
	case "instagram":
		recipientID := msg.ThreadID
		if recipientID == "" || recipientID == acc.ExternalID || recipientID == acc.PageID {
			recipientID = msg.CustomerID
		}
		return h.sendInstagramImageReply(ctx, &acc, recipientID, imageURL)
	case "facebook":
		return h.sendFacebookImageReply(ctx, &acc, msg.ThreadID, imageURL)
	case "whatsapp":
		return h.sendWhatsAppImageReply(ctx, &acc, msg.CustomerID, imageURL)
	default:
		return fmt.Errorf("image sending not implemented for %s", msg.Platform)
	}
}

// sendTelegramImageReply sends an image via Telegram bot
func (h *MessageHandler) sendTelegramImageReply(ctx context.Context, msg IncomingMessage, imageURL string) error {
	if h.BotGetter == nil {
		return fmt.Errorf("telegram bot not available")
	}

	botInterface, ok := h.BotGetter.GetBot(msg.TenantID)
	if !ok {
		return fmt.Errorf("telegram bot not found for tenant %d", msg.TenantID)
	}

	bot, ok := botInterface.(*tgbotapi.BotAPI)
	if !ok {
		return fmt.Errorf("invalid bot type")
	}

	chatID, err := strconv.ParseInt(msg.ThreadID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid telegram chat ID: %w", err)
	}

	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(imageURL))
	if _, err := bot.Send(photo); err != nil {
		return fmt.Errorf("failed to send telegram image: %w", err)
	}

	return nil
}

// sendInstagramImageReply sends one or more images via Instagram Messaging API
// Supports single image (attachment) or multiple images (attachments array, up to 10)
func (h *MessageHandler) sendInstagramImageReply(ctx context.Context, acc *storage.SocialAccount, threadID string, imageURLs ...string) error {
	if len(imageURLs) == 0 {
		return fmt.Errorf("no image URLs provided")
	}

	// Ensure access token is valid
	if acc.LoginMethod == "instagram_login" {
		if err := h.ensureInstagramLoginToken(ctx, acc); err != nil {
			h.Logger.Warn("⚠️ Failed to refresh Instagram Login token, attempting to use existing token", zap.Error(err))
		}
	}

	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}

	if decryptedToken == "" {
		return fmt.Errorf("Instagram access token is empty")
	}

	igAccountID := acc.ExternalID
	if igAccountID == "" {
		igAccountID = acc.PageID
	}

	var apiURL string
	if acc.LoginMethod == "instagram_login" {
		apiURL = fmt.Sprintf("https://graph.instagram.com/v24.0/%s/messages", igAccountID)
	} else {
		apiURL = fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", igAccountID)
	}

	// Limit to 10 images max
	if len(imageURLs) > 10 {
		imageURLs = imageURLs[:10]
	}

	var payload map[string]interface{}

	if len(imageURLs) == 1 {
		// Single image - use attachment (singular, not attachments array)
		// According to Instagram API docs: for single image, use "attachment" (singular)
		payload = map[string]interface{}{
			"recipient": map[string]string{
				"id": threadID,
			},
			"message": map[string]interface{}{
				"attachment": map[string]interface{}{
					"type": "image",
					"payload": map[string]string{
						"url": imageURLs[0],
					},
				},
			},
		}
		h.Logger.Info("📤 Preparing single image payload",
			zap.String("image_url", imageURLs[0]),
			zap.String("recipient_id", threadID))
	} else {
		// Multiple images - use attachments array
		attachments := make([]map[string]interface{}, len(imageURLs))
		for i, url := range imageURLs {
			attachments[i] = map[string]interface{}{
				"type": "image",
				"payload": map[string]string{
					"url": url,
				},
			}
		}
		payload = map[string]interface{}{
			"recipient": map[string]string{
				"id": threadID,
			},
			"message": map[string]interface{}{
				"attachments": attachments,
			},
		}
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if acc.LoginMethod == "instagram_login" {
		req.Header.Set("Authorization", "Bearer "+decryptedToken)
	} else {
		apiURL = fmt.Sprintf("%s?access_token=%s", apiURL, url.QueryEscape(decryptedToken))
		req.URL, _ = url.Parse(apiURL)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// Check for multi-image beta feature error
		var apiError struct {
			Error struct {
				Message   string `json:"message"`
				Type      string `json:"type"`
				Code      int    `json:"code"`
				SubCode   int    `json:"error_subcode"`
				FbtraceID string `json:"fbtrace_id"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &apiError); err == nil {
			if apiError.Error.SubCode == 2534068 {
				// Multi-image feature not available yet - fallback to sending images one by one
				h.Logger.Warn("⚠️ Multi-image feature not available, sending images individually",
					zap.Int("image_count", len(imageURLs)))
				for _, url := range imageURLs {
					if err := h.sendInstagramImageReply(ctx, acc, threadID, url); err != nil {
						h.Logger.Warn("Failed to send individual image", zap.String("url", url), zap.Error(err))
					}
					// Small delay between images
					time.Sleep(500 * time.Millisecond)
				}
				return nil
			}
		}
		return fmt.Errorf("Instagram API error %d: %s", resp.StatusCode, string(body))
	}

	h.Logger.Info("✅ Instagram image(s) sent successfully",
		zap.String("recipient_id", threadID),
		zap.Int("image_count", len(imageURLs)))

	return nil
}

// sendFacebookImageReply sends an image via Facebook Messenger API
func (h *MessageHandler) sendFacebookImageReply(ctx context.Context, acc *storage.SocialAccount, threadID, imageURL string) error {
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}

	apiURL := fmt.Sprintf("https://graph.facebook.com/v18.0/me/messages?access_token=%s", url.QueryEscape(decryptedToken))
	payload := map[string]interface{}{
		"recipient": map[string]string{
			"id": threadID,
		},
		"message": map[string]interface{}{
			"attachment": map[string]interface{}{
				"type": "image",
				"payload": map[string]string{
					"url": imageURL,
				},
			},
		},
	}

	jsonData, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(jsonData)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Facebook API error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// sendWhatsAppImageReply sends an image via WhatsApp Business API
func (h *MessageHandler) sendWhatsAppImageReply(ctx context.Context, acc *storage.SocialAccount, customerID, imageURL string) error {
	if h.GupshupClient == nil {
		return fmt.Errorf("gupshup client not configured")
	}

	appID := acc.PageID
	if appID == "" {
		appID = acc.ExternalID
	}

	// For now, send as text with image URL (Gupshup image API can be added later)
	_, err := h.GupshupClient.SendSessionMessage(appID, customerID, imageURL)
	return err
}

// sendReply sends a reply to the customer on the platform
// Supports intelligent message splitting when MessageSplitter is configured
func (h *MessageHandler) sendReply(ctx context.Context, msg IncomingMessage, reply string) error {
	// Check if message splitting is enabled
	if h.MessageSplitter != nil {
		decision := h.MessageSplitter.ShouldSplitMessage(reply)

		if decision.ShouldSplit && len(decision.Parts) > 1 {
			h.Logger.Info("📨 Splitting reply into sequence",
				zap.Int("parts", len(decision.Parts)),
				zap.String("reason", decision.Reason),
				zap.Stringer("tenant_id", msg.TenantID))

			// Send messages in sequence with human-like delays
			delays := h.MessageSplitter.GetSendDelays(len(decision.Parts))
			for i, part := range decision.Parts {
				// Wait for delay (except first message)
				if i > 0 {
					time.Sleep(delays[i])
				}

				// Send this part
				if err := h.sendReplySingle(ctx, msg, part); err != nil {
					h.Logger.Error("failed to send message part in sequence",
						zap.Int("part", i+1),
						zap.Int("total", len(decision.Parts)),
						zap.Error(err))
					// Continue sending remaining parts even if one fails
				}
			}

			return nil
		}
	}

	// Default: send as single message
	return h.sendReplySingle(ctx, msg, reply)
}

// sendReplySingle sends a single message part (internal helper)
func (h *MessageHandler) sendReplySingle(ctx context.Context, msg IncomingMessage, reply string) error {
	// Handle Telegram differently - it doesn't use social accounts
	if msg.Platform == "telegram" {
		return h.sendTelegramReply(ctx, msg, reply)
	}

	// Get social account with all fields (especially ExternalID)
	var acc storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND platform = ? AND connected = ?",
		msg.TenantID, msg.Platform, true).First(&acc).Error; err != nil {
		// Enhanced diagnostic logging for debugging connection issues
		h.Logger.Warn("🔴 Failed to find connected social account",
			zap.Error(err),
			zap.Stringer("tenant_id", msg.TenantID),
			zap.String("platform", msg.Platform),
			zap.String("note", "Checking what accounts exist for this tenant..."))

		// Check if any account exists for this tenant (regardless of connected status)
		var anyAccount storage.SocialAccount
		if dbErr := h.DB.Where("tenant_id = ? AND platform = ?", msg.TenantID, msg.Platform).First(&anyAccount).Error; dbErr == nil {
			h.Logger.Warn("🔍 Found account but not connected",
				zap.Stringer("tenant_id", msg.TenantID),
				zap.String("platform", msg.Platform),
				zap.Bool("connected", anyAccount.Connected),
				zap.String("mode", anyAccount.Mode),
				zap.String("page_id", anyAccount.PageID),
				zap.String("external_id", anyAccount.ExternalID),
				zap.Stringer("account_id", anyAccount.ID))

			// For WhatsApp Gupshup Partner, if account exists but not connected,
			// check if we have a live Gupshup app and auto-connect
			if msg.Platform == "whatsapp" && anyAccount.Mode == "gupshup_partner" {
				var gupshupApp storage.WhatsAppApp
				// Accept 'live', 'connected', or apps with active subscription
				if appErr := h.DB.Where("tenant_id = ? AND mode = ? AND (status IN (?, ?) OR subscription_active = ?)",
					msg.TenantID, "gupshup_partner", "live", "connected", true).First(&gupshupApp).Error; appErr == nil {
					// Gupshup app is ready! Auto-mark account as connected
					anyAccount.Connected = true
					if gupshupApp.PhoneNumber != "" {
						anyAccount.Username = gupshupApp.PhoneNumber
					}
					if gupshupApp.PhoneNumberID != "" {
						anyAccount.ExternalID = gupshupApp.PhoneNumberID
						anyAccount.PageID = gupshupApp.PhoneNumberID
					}
					if saveErr := h.DB.Save(&anyAccount).Error; saveErr == nil {
						h.Logger.Info("✅ Auto-connected WhatsApp account (Gupshup app is live)",
							zap.Stringer("tenant_id", msg.TenantID),
							zap.String("gupshup_app_id", gupshupApp.GupshupAppID))
						acc = anyAccount // Use this account
					} else {
						h.Logger.Error("Failed to auto-connect WhatsApp account", zap.Error(saveErr))
						return fmt.Errorf("no connected %s account", msg.Platform)
					}
				} else {
					return fmt.Errorf("no connected %s account", msg.Platform)
				}
			} else {
				return fmt.Errorf("no connected %s account", msg.Platform)
			}
		} else {
			h.Logger.Warn("🔍 No account found at all for this tenant and platform",
				zap.Stringer("tenant_id", msg.TenantID),
				zap.String("platform", msg.Platform),
				zap.Error(dbErr))

			// FALLBACK for WhatsApp Gupshup Partner: If no SocialAccount exists at all,
			// but we have a live Gupshup WhatsApp app, create the SocialAccount on the fly
			if msg.Platform == "whatsapp" && h.GupshupClient != nil {
				var gupshupApp storage.WhatsAppApp
				// Accept 'live', 'connected', or apps with active subscription
				if appErr := h.DB.Where("tenant_id = ? AND mode = ? AND (status IN (?, ?) OR subscription_active = ?)",
					msg.TenantID, "gupshup_partner", "live", "connected", true).First(&gupshupApp).Error; appErr == nil {
					h.Logger.Info("🔧 Found ready Gupshup app but no SocialAccount - creating one now",
						zap.Stringer("tenant_id", msg.TenantID),
						zap.String("gupshup_app_id", gupshupApp.GupshupAppID),
						zap.String("phone_number", gupshupApp.PhoneNumber))

					// Create SocialAccount on the fly
					newAccount := storage.SocialAccount{
						TenantID:   msg.TenantID,
						Platform:   "whatsapp",
						Mode:       "gupshup_partner",
						Username:   gupshupApp.PhoneNumber,
						PageID:     gupshupApp.PhoneNumberID,
						ExternalID: gupshupApp.PhoneNumberID,
						Connected:  true,
						Metadata:   "{}", // Valid empty JSON for JSONB column
					}
					if createErr := h.DB.Create(&newAccount).Error; createErr == nil {
						h.Logger.Info("✅ Created SocialAccount for existing Gupshup WhatsApp app",
							zap.Stringer("tenant_id", msg.TenantID),
							zap.Stringer("account_id", newAccount.ID))
						acc = newAccount // Use this newly created account
					} else {
						h.Logger.Error("Failed to create SocialAccount for Gupshup app",
							zap.Stringer("tenant_id", msg.TenantID),
							zap.Error(createErr))
						return fmt.Errorf("no connected %s account", msg.Platform)
					}
				} else {
					h.Logger.Warn("🔍 No Gupshup app found either - truly no WhatsApp connection",
						zap.Stringer("tenant_id", msg.TenantID),
						zap.Error(appErr))
					return fmt.Errorf("no connected %s account", msg.Platform)
				}
			} else {
				return fmt.Errorf("no connected %s account", msg.Platform)
			}
		}
	}

	// Log account details for debugging
	h.Logger.Info("📋 Loading social account for reply",
		zap.String("platform", msg.Platform),
		zap.Stringer("tenant_id", msg.TenantID),
		zap.String("external_id", acc.ExternalID),
		zap.String("page_id", acc.PageID))

	// Platform-specific reply sending
	// This will be implemented per platform
	switch msg.Platform {
	case "instagram":
		// For Instagram, use ThreadID (which should be the customer's IGSID)
		// If ThreadID is empty or matches account ID, fallback to CustomerID
		recipientID := msg.ThreadID
		if recipientID == "" || recipientID == acc.ExternalID || recipientID == acc.PageID {
			h.Logger.Warn("⚠️ ThreadID is empty or matches account ID, using CustomerID as fallback",
				zap.String("thread_id", msg.ThreadID),
				zap.String("customer_id", msg.CustomerID),
				zap.String("account_external_id", acc.ExternalID),
				zap.String("account_page_id", acc.PageID))
			recipientID = msg.CustomerID
		}

		// CRITICAL: Final validation - ensure recipient ID is not the account ID
		if recipientID == "" {
			return fmt.Errorf("CRITICAL: Cannot send Instagram message - both ThreadID and CustomerID are empty or invalid")
		}
		if recipientID == acc.ExternalID || recipientID == acc.PageID {
			return fmt.Errorf("CRITICAL: Cannot send Instagram message - recipient ID (%s) matches account ID. This is likely an echo message that should have been filtered earlier.", recipientID)
		}

		// 🆕 SANITIZE RECIPIENT ID: Instagram API requires numeric-only ID
		sanitizedRecipientID := ""
		for _, r := range recipientID {
			if r >= '0' && r <= '9' {
				sanitizedRecipientID += string(r)
			}
		}

		if sanitizedRecipientID == "" {
			h.Logger.Error("❌ Instagram recipient ID has no numeric chars",
				zap.String("original", recipientID))
			return fmt.Errorf("invalid Instagram recipient ID: %s", recipientID)
		}

		if sanitizedRecipientID != recipientID {
			h.Logger.Warn("⚠️ Sanitized Instagram recipient ID",
				zap.String("original", recipientID),
				zap.String("sanitized", sanitizedRecipientID))
		}

		h.Logger.Info("📨 Sending Instagram reply",
			zap.String("recipient_id", sanitizedRecipientID),
			zap.String("thread_id", msg.ThreadID),
			zap.String("customer_id", msg.CustomerID))

		return h.sendInstagramReply(ctx, &acc, sanitizedRecipientID, reply)
	case "facebook":
		return h.sendFacebookReply(ctx, &acc, msg.ThreadID, reply)
	case "whatsapp":
		return h.sendWhatsAppReply(ctx, &acc, msg.CustomerID, reply)
	default:
		return fmt.Errorf("reply sending not implemented for %s", msg.Platform)
	}
}

// sendTelegramReply sends a reply via Telegram bot
func (h *MessageHandler) sendTelegramReply(ctx context.Context, msg IncomingMessage, reply string) error {
	if h.BotGetter == nil {
		return fmt.Errorf("bot getter not configured for Telegram replies")
	}

	// Get bot instance
	botInterface, ok := h.BotGetter.GetBot(msg.TenantID)
	if !ok {
		return fmt.Errorf("telegram bot not found for tenant %d", msg.TenantID)
	}

	// Type assert to get the actual bot
	bot, ok := botInterface.(*tgbotapi.BotAPI)
	if !ok {
		return fmt.Errorf("invalid bot type for tenant %d", msg.TenantID)
	}

	// Parse chat ID from thread ID (which is the Telegram chat ID)
	chatID, err := strconv.ParseInt(msg.ThreadID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid telegram chat ID: %w", err)
	}

	// Send message via Telegram bot
	telegramMsg := tgbotapi.NewMessage(chatID, reply)
	telegramMsg.ParseMode = "Markdown"

	if _, err := bot.Send(telegramMsg); err != nil {
		h.Logger.Error("failed to send telegram reply",
			zap.Stringer("tenant_id", msg.TenantID),
			zap.Int64("chat_id", chatID),
			zap.Error(err))
		return fmt.Errorf("failed to send telegram message: %w", err)
	}

	h.Logger.Info("✅ Telegram message sent successfully",
		zap.Stringer("tenant_id", msg.TenantID),
		zap.Int64("chat_id", chatID),
		zap.String("message_preview", func() string {
			if len(reply) > 50 {
				return reply[:50] + "..."
			}
			return reply
		}()))

	return nil
}

// sendInstagramReply sends a reply via Instagram Messaging API
// According to Meta's official documentation: https://developers.facebook.com/docs/messenger-platform/instagram/feature-reference/send-message
func (h *MessageHandler) sendInstagramReply(ctx context.Context, acc *storage.SocialAccount, threadID, reply string) error {
	// Ensure access token is valid and refreshed if needed (for Instagram Login)
	if acc.LoginMethod == "instagram_login" {
		if err := h.ensureInstagramLoginToken(ctx, acc); err != nil {
			h.Logger.Warn("⚠️ Failed to refresh Instagram Login token, attempting to use existing token",
				zap.Error(err),
				zap.Stringer("tenant_id", acc.TenantID))
			// Continue with existing token - might still work
		}
	}

	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		// Try using as-is if decryption fails
		decryptedToken = acc.AccessToken
	}

	if decryptedToken == "" {
		return fmt.Errorf("Instagram access token is empty")
	}

	// Get Instagram Business Account ID (from ExternalID or PageID)
	igAccountID := acc.ExternalID
	if igAccountID == "" {
		igAccountID = acc.PageID
	}
	if igAccountID == "" {
		// Auto-fetch Instagram Business Account ID if missing
		h.Logger.Warn("⚠️ Instagram Business Account ID missing, attempting to auto-fetch",
			zap.Stringer("tenant_id", acc.TenantID),
			zap.String("platform", acc.Platform))

		fetchedID, err := h.fetchAndSaveInstagramID(ctx, acc)
		if err != nil {
			h.Logger.Error("❌ Failed to auto-fetch Instagram Business Account ID",
				zap.Error(err),
				zap.Stringer("tenant_id", acc.TenantID))
			return fmt.Errorf("Instagram Business Account ID not found. To fix this:\n1. Type 'verify instagram' in your Telegram bot to reconnect\n2. Or check your Meta Developer Console → ensure Instagram Business Account is linked to your Facebook Page\n3. Make sure you granted 'instagram_manage_messages' permission during OAuth. Error: %v", err)
		}
		igAccountID = fetchedID
		h.Logger.Info("✅ Auto-fetched and saved Instagram Business Account ID",
			zap.String("instagram_id", igAccountID),
			zap.Stringer("tenant_id", acc.TenantID))
	}

	// Determine API base URL and token requirements based on login method
	var apiURL string
	if acc.LoginMethod == "instagram_login" {
		// Instagram Login: Use graph.instagram.com/v24.0 with Instagram User access token
		// According to: https://developers.facebook.com/docs/instagram-platform/instagram-api-with-instagram-login/messaging-api
		// Endpoint: POST /<IG_ID>/messages or /me/messages
		apiURL = fmt.Sprintf("https://graph.instagram.com/v24.0/%s/messages", igAccountID)
		h.Logger.Info("📱 Using Instagram Login messaging API",
			zap.String("instagram_account_id", igAccountID),
			zap.String("login_method", "instagram_login"),
			zap.String("api_url", apiURL))
	} else {
		// Facebook Login: Verify token type - Instagram messaging REQUIRES a Page Access Token
		tokenType, pageID, err := h.verifyTokenType(ctx, decryptedToken)
		if err != nil {
			h.Logger.Warn("⚠️ Could not verify token type", zap.Error(err))
		} else {
			h.Logger.Info("🔑 Token type verified",
				zap.String("token_type", tokenType),
				zap.String("page_id", pageID))

			if tokenType != "PAGE" {
				return fmt.Errorf("Instagram messaging requires a Page Access Token, but you're using a %s token. Please generate a Page Access Token from Meta Developer Console → Tools → Graph API Explorer → Select your Page → Generate Token with 'instagram_manage_messages' and 'pages_messaging' permissions", tokenType)
			}
		}

		// Instagram Messaging API endpoint according to Meta's official documentation
		// Endpoint: https://graph.facebook.com/v18.0/{instagram-business-account-id}/messages
		// This requires a Page Access Token with instagram_manage_messages permission
		apiURL = fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", igAccountID)
	}

	// CRITICAL: Validate that we're not trying to send to ourselves
	// threadID should be the customer's IGSID, not the Instagram account ID
	if threadID == igAccountID {
		return fmt.Errorf("CRITICAL: Cannot send message - recipient ID (%s) matches Instagram account ID. This indicates the ThreadID was incorrectly set to the account ID instead of the customer's IGSID. Please check webhook processing.", threadID)
	}

	if threadID == "" {
		return fmt.Errorf("CRITICAL: ThreadID is empty - cannot determine recipient for Instagram message")
	}

	h.Logger.Info("📤 Attempting to send Instagram DM",
		zap.String("instagram_account_id", igAccountID),
		zap.String("recipient_id", threadID),
		zap.String("login_method", acc.LoginMethod),
		zap.String("message_preview", func() string {
			if len(reply) > 50 {
				return reply[:50] + "..."
			}
			return reply
		}()))

	// Instagram messaging payload format according to Meta's Send Message API
	// For Instagram Login: https://developers.facebook.com/docs/instagram-platform/instagram-api-with-instagram-login/messaging-api
	// For Facebook Login: https://developers.facebook.com/docs/messenger-platform/instagram/feature-reference/send-message
	// recipient.id must be the Instagram user's IGSID (Instagram-scoped ID) for Instagram Login, or PSID for Facebook Login
	payload := map[string]interface{}{
		"recipient": map[string]string{
			"id": threadID, // IGSID (Instagram-scoped ID) for Instagram Login, PSID for Facebook Login
		},
		"message": map[string]interface{}{
			"text": reply,
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// For Instagram Login, use Bearer token in Authorization header (as per documentation)
	// For Facebook Login, use access_token query parameter (backward compatible)
	var finalURL string
	if acc.LoginMethod == "instagram_login" {
		// Instagram Login: Use Bearer token in header
		finalURL = apiURL
	} else {
		// Facebook Login: Add access token as query parameter (Meta API standard)
		finalURL = fmt.Sprintf("%s?access_token=%s", apiURL, url.QueryEscape(decryptedToken))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", finalURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if acc.LoginMethod == "instagram_login" {
		req.Header.Set("Authorization", "Bearer "+decryptedToken)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// Parse error response to provide helpful guidance
		var apiError struct {
			Error struct {
				Message   string `json:"message"`
				Type      string `json:"type"`
				Code      int    `json:"code"`
				SubCode   int    `json:"error_subcode"`
				FbtraceID string `json:"fbtrace_id"`
			} `json:"error"`
		}
		json.Unmarshal(body, &apiError)

		errorMsg := fmt.Sprintf("Instagram API error %d: %s", resp.StatusCode, string(body))

		// Enhanced monitoring for Instagram rate limits
		isRateLimit := resp.StatusCode == 403 ||
			strings.Contains(strings.ToLower(apiError.Error.Message), "rate limit") ||
			strings.Contains(strings.ToLower(apiError.Error.Message), "request limit reached") ||
			apiError.Error.Code == 4 || apiError.Error.Code == 17

		if isRateLimit {
			h.Logger.Error("🚨 INSTAGRAM RATE LIMIT HIT - MONITORING ALERT",
				zap.Int("error_code", apiError.Error.Code),
				zap.Int("status_code", resp.StatusCode),
				zap.String("error_type", apiError.Error.Type),
				zap.String("error_message", apiError.Error.Message),
				zap.String("fbtrace_id", apiError.Error.FbtraceID),
				zap.String("alert_type", "instagram_rate_limit"),
				zap.String("action_required", "Reduce message sending frequency, wait before retrying"),
				zap.Stringer("tenant_id", acc.TenantID),
				zap.String("platform", "instagram"))
		}

		// Provide specific guidance for common errors based on Meta's error codes
		if apiError.Error.Code == 3 {
			h.Logger.Error("❌ INSTAGRAM MESSAGING CAPABILITY MISSING",
				zap.Int("error_code", apiError.Error.Code),
				zap.String("error_type", apiError.Error.Type),
				zap.String("error_message", apiError.Error.Message),
				zap.String("fbtrace_id", apiError.Error.FbtraceID),
				zap.String("action_required", "Enable Instagram Messaging capability in Meta Developer Console"))

			errorMsg += "\n\n❌ INSTAGRAM MESSAGING CAPABILITY MISSING:\n"
			errorMsg += "Your Meta app doesn't have the 'Instagram Messaging' capability enabled.\n"
			errorMsg += "This is REQUIRED even for test/development mode.\n\n"
			errorMsg += "🔧 IMMEDIATE ACTION REQUIRED:\n"
			errorMsg += "1. Go to: https://developers.facebook.com/apps/{YOUR_APP_ID}/dashboard\n"
			errorMsg += "2. Click 'Add Product' → Find 'Instagram Messaging' → Click 'Set Up'\n"
			errorMsg += "3. Configure webhook URL: https://your-domain.com/api/webhooks/meta\n"
			errorMsg += "4. Set verify token (must match META_WEBHOOK_VERIFY_TOKEN)\n"
			errorMsg += "5. Subscribe to 'messages' field\n"
			errorMsg += "6. Go to App Review → Permissions and Features\n"
			errorMsg += "7. Submit 'instagram_manage_messages' for App Review\n"
			errorMsg += "8. Wait for approval (1-7 days typically)\n\n"
			errorMsg += "⚠️ IMPORTANT: Instagram Messaging requires App Review approval\n"
			errorMsg += "   even for development/testing. Facebook Messenger works without\n"
			errorMsg += "   approval, but Instagram does not."
		} else if apiError.Error.Code == 190 {
			errorMsg += "\n\n⚠️ Invalid Access Token:\n"
			errorMsg += "Your Page Access Token may be expired or invalid.\n"
			errorMsg += "Please generate a new Page Access Token with 'instagram_manage_messages' permission.\n"
		} else if apiError.Error.Code == 10 {
			errorMsg += "\n\n⚠️ Permission Denied:\n"
			errorMsg += "Your app doesn't have the 'instagram_manage_messages' permission.\n"
			errorMsg += "Please request this permission in App Review and wait for approval.\n"
		} else if apiError.Error.Code == 200 {
			errorMsg += "\n\n⚠️ Invalid Recipient ID:\n"
			errorMsg += "The recipient ID (PSID) is invalid or the user has blocked your Instagram account.\n"
			errorMsg += "Ensure you're using the PSID from the webhook event (sender.id).\n"
		} else if apiError.Error.Code == 368 {
			errorMsg += "\n\n⚠️ Temporarily Blocked:\n"
			errorMsg += "You've been temporarily blocked from sending messages.\n"
			errorMsg += "This can happen if you send too many messages or violate Meta's messaging policies.\n"
			errorMsg += "Wait before trying again.\n"
		}

		h.Logger.Error("Instagram DM API error",
			zap.Int("status", resp.StatusCode),
			zap.Int("error_code", apiError.Error.Code),
			zap.Int("error_subcode", apiError.Error.SubCode),
			zap.String("error_type", apiError.Error.Type),
			zap.String("error_message", apiError.Error.Message),
			zap.String("fbtrace_id", apiError.Error.FbtraceID),
			zap.String("response", string(body)))

		return errors.New(errorMsg)
	}

	// Parse success response
	var response struct {
		RecipientID string `json:"recipient_id"`
		MessageID   string `json:"message_id"`
	}
	if err := json.Unmarshal(body, &response); err == nil {
		h.Logger.Info("✅ Instagram DM sent successfully",
			zap.String("recipient_id", response.RecipientID),
			zap.String("message_id", response.MessageID),
			zap.String("message_preview", func() string {
				if len(reply) > 50 {
					return reply[:50] + "..."
				}
				return reply
			}()))
	} else {
		h.Logger.Info("✅ Instagram DM sent",
			zap.String("recipient_id", threadID),
			zap.String("message_preview", func() string {
				if len(reply) > 50 {
					return reply[:50] + "..."
				}
				return reply
			}()))
	}

	return nil
}

// sendFacebookReply sends a reply via Facebook Messenger API
// According to Meta's official documentation: https://developers.facebook.com/docs/messenger-platform/reference/send-api
func (h *MessageHandler) sendFacebookReply(ctx context.Context, acc *storage.SocialAccount, threadID, reply string) error {
	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		// Try using as-is if decryption fails
		decryptedToken = acc.AccessToken
	}

	if decryptedToken == "" {
		return fmt.Errorf("Facebook access token is empty")
	}

	// Get Page ID - prefer ExternalID (which should be the Page ID for Facebook)
	pageID := acc.ExternalID
	if pageID == "" {
		pageID = acc.PageID
	}

	// Facebook Messenger API endpoint
	// According to Meta docs: Use {page-id}/messages for better reliability
	// Fallback to /me/messages if Page ID is not available
	var apiURL string
	if pageID != "" {
		// Use explicit Page ID endpoint (recommended by Meta)
		apiURL = fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages", pageID)
	} else {
		// Fallback to /me/messages (requires Page Access Token)
		apiURL = "https://graph.facebook.com/v18.0/me/messages"
		h.Logger.Warn("⚠️ Using /me/messages endpoint - Page ID not found. Consider setting ExternalID or PageID for better reliability")
	}

	h.Logger.Info("📤 Attempting to send Facebook Messenger message",
		zap.String("page_id", pageID),
		zap.String("recipient_id", threadID),
		zap.String("endpoint", apiURL),
		zap.String("message_preview", func() string {
			if len(reply) > 50 {
				return reply[:50] + "..."
			}
			return reply
		}()))

	// Facebook Messenger payload format according to Meta's Send API documentation
	// https://developers.facebook.com/docs/messenger-platform/reference/send-api#request
	payload := map[string]interface{}{
		"recipient": map[string]string{
			"id": threadID, // PSID (Page-Scoped ID) of the recipient
		},
		"message": map[string]interface{}{
			"text": reply,
		},
		"messaging_type": "RESPONSE", // Required: RESPONSE for replying within 24-hour window
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Add access token as query parameter (Meta API standard)
	apiURLWithToken := fmt.Sprintf("%s?access_token=%s", apiURL, url.QueryEscape(decryptedToken))

	req, err := http.NewRequestWithContext(ctx, "POST", apiURLWithToken, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// Parse error response to provide helpful guidance
		var apiError struct {
			Error struct {
				Message   string `json:"message"`
				Type      string `json:"type"`
				Code      int    `json:"code"`
				SubCode   int    `json:"error_subcode"`
				FbtraceID string `json:"fbtrace_id"`
			} `json:"error"`
		}
		json.Unmarshal(body, &apiError)

		errorMsg := fmt.Sprintf("Facebook Messenger API error %d: %s", resp.StatusCode, string(body))

		// Provide specific guidance for common errors
		if apiError.Error.Code == 190 {
			errorMsg += "\n\n⚠️ Invalid Access Token:\n"
			errorMsg += "Your Page Access Token may be expired or invalid.\n"
			errorMsg += "Please generate a new Page Access Token from Meta Developer Console.\n"
		} else if apiError.Error.Code == 10 {
			errorMsg += "\n\n⚠️ Permission Denied:\n"
			errorMsg += "Your app doesn't have the 'pages_messaging' permission.\n"
			errorMsg += "Please request this permission in App Review.\n"
		} else if apiError.Error.Code == 200 {
			errorMsg += "\n\n⚠️ Invalid Recipient ID:\n"
			errorMsg += "The recipient ID (PSID) is invalid or the user has blocked your page.\n"
		}

		h.Logger.Error("Facebook Messenger API error",
			zap.Int("status", resp.StatusCode),
			zap.Int("error_code", apiError.Error.Code),
			zap.Int("error_subcode", apiError.Error.SubCode),
			zap.String("error_type", apiError.Error.Type),
			zap.String("error_message", apiError.Error.Message),
			zap.String("fbtrace_id", apiError.Error.FbtraceID),
			zap.String("response", string(body)))

		return errors.New(errorMsg)
	}

	// Parse success response
	var response struct {
		RecipientID string `json:"recipient_id"`
		MessageID   string `json:"message_id"`
	}
	if err := json.Unmarshal(body, &response); err == nil {
		h.Logger.Info("✅ Facebook Messenger message sent successfully",
			zap.String("recipient_id", response.RecipientID),
			zap.String("message_id", response.MessageID),
			zap.String("message_preview", func() string {
				if len(reply) > 50 {
					return reply[:50] + "..."
				}
				return reply
			}()))
	} else {
		h.Logger.Info("✅ Facebook Messenger message sent",
			zap.String("recipient_id", threadID),
			zap.String("message_preview", func() string {
				if len(reply) > 50 {
					return reply[:50] + "..."
				}
				return reply
			}()))
	}

	return nil
}

// sendWhatsAppReply sends a reply via WhatsApp Business API using Gupshup only
func (h *MessageHandler) sendWhatsAppReply(ctx context.Context, acc *storage.SocialAccount, customerID, reply string) error {
	// Only use Gupshup - check if tenant has a Gupshup app
	if h.GupshupClient == nil {
		return fmt.Errorf("gupshup client not configured")
	}

	// Get app ID from WhatsAppApp table (check by tenant ID and mode)
	var app storage.WhatsAppApp
	if err := h.DB.Where("tenant_id = ? AND mode = ?", acc.TenantID, "gupshup_partner").First(&app).Error; err != nil {
		return fmt.Errorf("gupshup app not found for tenant: %w", err)
	}

	if app.GupshupAppID == "" {
		return fmt.Errorf("gupshup app ID is empty for tenant")
	}

	// Accept 'live' or 'connected' status, or apps with active subscription
	isReady := app.Status == "live" || app.Status == "connected" || app.SubscriptionActive
	if !isReady {
		return fmt.Errorf("gupshup app is not ready (status: %s, subscription_active: %v)", app.Status, app.SubscriptionActive)
	}

	// ✅ Check 24-hour session window for free-form messages
	// WhatsApp Business Platform allows free-form (session) messages within 24 hours
	// of the customer's last message. After 24h, must use pre-approved templates.
	// The 24-hour window resets every time the customer sends a message.
	var customer storage.Customer
	within24Hours := false
	if err := h.DB.Where("tenant_id = ? AND platform_id = ? AND channel = ?", acc.TenantID, customerID, "whatsapp").First(&customer).Error; err == nil {
		if !customer.LastMessageAt.IsZero() {
			timeSinceLastMessage := time.Since(customer.LastMessageAt)
			within24Hours = timeSinceLastMessage < 24*time.Hour
			h.Logger.Info("📅 Checking 24-hour session window",
				zap.String("customer_id", customerID),
				zap.Duration("time_since_last_message", timeSinceLastMessage),
				zap.Bool("within_24_hours", within24Hours),
				zap.String("customer_last_message_at", customer.LastMessageAt.Format(time.RFC3339)),
				zap.String("note", "Free-form window opens when user sends message. Resets with each user message. After 24h, must use templates."))
		} else {
			// No previous message from customer - can't use session messages
			// Must use template to initiate conversation
			h.Logger.Info("📅 No previous customer message - must use template to initiate",
				zap.String("customer_id", customerID),
				zap.String("note", "First message to customer requires template. Session messages only work after customer initiates."))
		}
	} else {
		// Customer not found - likely first interaction, must use template
		h.Logger.Info("📅 Customer not found in DB - must use template to initiate",
			zap.String("customer_id", customerID),
			zap.String("note", "First message requires template. Session messages only work after customer initiates conversation."))
	}

	// If within 24 hours, try to use session message (no template) first
	// Note: Gupshup Partner API might still require templates, but we'll try
	if within24Hours {
		h.Logger.Info("💬 Within 24-hour window - attempting session message (no template)",
			zap.String("customer_id", customerID),
			zap.String("app_id", app.GupshupAppID))

		// Try sending without template (session message)
		// If Gupshup doesn't support this, it will fail and we'll fall back to template
		messageID, err := h.GupshupClient.SendSessionMessage(app.GupshupAppID, customerID, reply)
		if err == nil {
			h.Logger.Info("✅ Session message sent successfully (no template needed)",
				zap.String("customer_id", customerID),
				zap.String("message_id", messageID))

			// Update interaction with message ID
			var interaction storage.Interaction
			if err := h.DB.Where("tenant_id = ? AND platform = ? AND direction = ? AND response_status = ?",
				acc.TenantID, "whatsapp", "outgoing", "pending").
				Order("created_at DESC").First(&interaction).Error; err == nil {
				interaction.PlatformMessageID = messageID
				interaction.ResponseStatus = "sent"
				h.DB.Save(&interaction)
			}
			return nil
		}

		// Session message failed - fall back to template
		h.Logger.Warn("⚠️ Session message failed, falling back to template",
			zap.String("customer_id", customerID),
			zap.Error(err),
			zap.String("note", "Gupshup Partner API may require templates for all messages"))
	}

	// Outside 24-hour window OR session message failed - use template
	// Check if we have a default template ID for AI replies
	// If not, try to create one automatically
	if app.DefaultTemplateID == "" {
		h.Logger.Info("🎨 No AI template found, creating one automatically",
			zap.String("app_id", app.GupshupAppID))

		templateID, err := h.GupshupClient.CreateAITemplate(app.GupshupAppID)
		if err != nil {
			h.Logger.Warn("⚠️ Failed to create AI template automatically (will try Meta Library fallback)",
				zap.String("app_id", app.GupshupAppID),
				zap.Error(err))
		} else {
			// Store the template ID in database
			app.DefaultTemplateID = templateID
			if err := h.DB.Save(&app).Error; err != nil {
				h.Logger.Warn("failed to save template ID to database",
					zap.String("app_id", app.GupshupAppID),
					zap.String("template_id", templateID),
					zap.Error(err))
			} else {
				h.Logger.Info("✅ AI template created and stored",
					zap.String("app_id", app.GupshupAppID),
					zap.String("template_id", templateID))
			}
		}
	}

	h.Logger.Info("📤 Sending WhatsApp message via Gupshup",
		zap.String("customer_id", customerID),
		zap.String("app_id", app.GupshupAppID),
		zap.String("template_id", app.DefaultTemplateID),
		zap.String("message_preview", func() string {
			if len(reply) > 50 {
				return reply[:50] + "..."
			}
			return reply
		}()))

	// Pass the default template ID if available (for AI replies)
	var templateID string
	if app.DefaultTemplateID != "" {
		templateID = app.DefaultTemplateID
	}
	messageID, err := h.GupshupClient.SendMessage(app.GupshupAppID, customerID, reply, templateID)
	if err != nil {
		h.Logger.Error("❌ Gupshup send failed",
			zap.String("customer_id", customerID),
			zap.String("app_id", app.GupshupAppID),
			zap.Error(err))
		return fmt.Errorf("failed to send message via Gupshup: %w", err)
	}

	h.Logger.Info("✅ WhatsApp message sent via Gupshup",
		zap.String("customer_id", customerID),
		zap.String("message_id", messageID),
		zap.String("app_id", app.GupshupAppID))

	// Update the most recent outgoing interaction with message ID
	var interaction storage.Interaction
	if err := h.DB.Where("tenant_id = ? AND platform = ? AND direction = ? AND response_status = ?",
		acc.TenantID, "whatsapp", "outgoing", "pending").
		Order("created_at DESC").First(&interaction).Error; err == nil {
		interaction.PlatformMessageID = messageID
		interaction.ResponseStatus = "sent"
		// Store Gupshup message ID in metadata
		var metadata map[string]interface{}
		if interaction.Metadata != "" {
			json.Unmarshal([]byte(interaction.Metadata), &metadata)
		}
		if metadata == nil {
			metadata = make(map[string]interface{})
		}
		metadata["gupshup_message_id"] = messageID
		metadataJSON, _ := json.Marshal(metadata)
		interaction.Metadata = string(metadataJSON)
		h.DB.Save(&interaction)
	}

	return nil
}

// sendWhatsAppReplyMeta sends a reply via Meta Direct WhatsApp Business API (fallback)
// This is the legacy implementation using Meta's direct API
func (h *MessageHandler) sendWhatsAppReplyMeta(ctx context.Context, acc *storage.SocialAccount, customerID, reply string) error {
	// Get phone_number_id (this is the tenant's assigned number)
	phoneNumberID := acc.PageID
	if phoneNumberID == "" {
		phoneNumberID = acc.ExternalID
	}
	if phoneNumberID == "" {
		return fmt.Errorf("phone_number_id not found for WhatsApp account")
	}

	// Use system user token for all WhatsApp API calls (Solution Partner model)
	// According to Meta docs: System User tokens are long-lived and should be used for API operations
	// The callback token is a user token (short-lived) and should not be used for API calls
	var accessToken string
	if acc.Mode == "provider_owned" || acc.Mode == "oauth2" {
		// Use system user token for provider-owned and Embedded Signup (oauth2) models
		// System user is added to WABA during onboarding, so we can use system token
		if h.SystemToken == "" {
			return fmt.Errorf("system token not configured for WhatsApp API calls")
		}
		accessToken = h.SystemToken
	} else {
		// Legacy mode: use tenant's token (fallback for other modes)
		if acc.AccessToken != "" {
			decrypted, err := utils.DecryptToken(acc.AccessToken)
			if err != nil {
				decrypted = acc.AccessToken
			}
			accessToken = decrypted
		} else {
			return fmt.Errorf("WhatsApp access token not found for tenant")
		}
	}

	if accessToken == "" {
		return fmt.Errorf("WhatsApp access token not found")
	}

	// WhatsApp Business API endpoint
	apiURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s/messages?access_token=%s", phoneNumberID, accessToken)

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                customerID, // WhatsApp phone number (e.g., "1234567890")
		"type":              "text",
		"text": map[string]string{
			"body": reply,
		},
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Parse response to check for errors (Meta sometimes returns 200 with errors in body)
	var apiResponse struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		Error *struct {
			Message   string `json:"message"`
			Type      string `json:"type"`
			Code      int    `json:"code"`
			ErrorData struct {
				MessagingProduct string `json:"messaging_product"`
				Details          string `json:"details"`
			} `json:"error_data"`
		} `json:"error"`
	}

	responseStr := string(body)
	if err := json.Unmarshal(body, &apiResponse); err == nil {
		if apiResponse.Error != nil {
			errorMsg := fmt.Sprintf("WhatsApp API error: %s (code: %d, type: %s)",
				apiResponse.Error.Message, apiResponse.Error.Code, apiResponse.Error.Type)

			// Specific error code handling
			if apiResponse.Error.Code == 131047 {
				errorMsg += "\n\n⚠️ 24-hour messaging window expired. Customer must initiate conversation first, or use an approved message template."
			} else if apiResponse.Error.Code == 131026 {
				errorMsg += "\n\n⚠️ Invalid recipient phone number. Ensure number is in E.164 format (+[country code][number])."
			} else if apiResponse.Error.Code == 131031 {
				errorMsg += "\n\n⚠️ Phone number not registered on WhatsApp or user has blocked your number."
			}

			h.Logger.Error("WhatsApp API error in response",
				zap.String("phone_number_id", phoneNumberID),
				zap.String("to", customerID),
				zap.Int("error_code", apiResponse.Error.Code),
				zap.String("error_type", apiResponse.Error.Type),
				zap.String("error_message", apiResponse.Error.Message),
				zap.String("response", responseStr))
			return errors.New(errorMsg)
		}
	}

	if resp.StatusCode >= 400 {
		h.Logger.Error("WhatsApp API error",
			zap.Int("status", resp.StatusCode),
			zap.String("response", responseStr),
			zap.String("phone_number_id", phoneNumberID))
		return fmt.Errorf("WhatsApp API error %d: %s", resp.StatusCode, responseStr)
	}

	// Log message ID for tracking delivery status
	messageID := ""
	if len(apiResponse.Messages) > 0 {
		messageID = apiResponse.Messages[0].ID
	}

	h.Logger.Info("✅ WhatsApp message sent via Meta Direct API",
		zap.String("phone_number_id", phoneNumberID),
		zap.String("to", customerID),
		zap.String("message_id", messageID),
		zap.String("message_preview", truncateString(reply, 50)),
		zap.String("note", "Check webhook for delivery status (sent/delivered/read)"))

	return nil
}

// saveToMemory saves conversation to memory for learning
func (h *MessageHandler) saveToMemory(ctx context.Context, tenantID, customerID uuid.UUID, message, reply string) {
	// Save customer conversation if the responder supports memory
	if h.Responder == nil {
		return
	}
	if rr, ok := h.Responder.(*ai.CustomerResponder); ok && rr.Memory != nil {
		rr.Memory.SaveCustomerConversation(ctx, tenantID, customerID, message, reply)
	}
}

// metadataToJSON converts metadata map to JSON string
func (h *MessageHandler) metadataToJSON(meta map[string]interface{}) string {
	if meta == nil {
		return "{}"
	}
	data, _ := json.Marshal(meta)
	return string(data)
}

// fetchAndSaveInstagramID fetches the Instagram Business Account ID and saves it to the database
func (h *MessageHandler) fetchAndSaveInstagramID(ctx context.Context, acc *storage.SocialAccount) (string, error) {
	// Decrypt token
	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// Step 1: Get Facebook pages
	pagesURL := fmt.Sprintf("https://graph.facebook.com/v18.0/me/accounts?access_token=%s", decryptedToken)
	resp, err := client.Get(pagesURL)
	if err != nil {
		return "", fmt.Errorf("failed to query pages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to fetch pages (status %d): %s", resp.StatusCode, string(body))
	}

	var pages struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
		return "", fmt.Errorf("failed to parse pages: %w", err)
	}

	if len(pages.Data) == 0 {
		return "", fmt.Errorf("no pages found")
	}

	pageID := pages.Data[0].ID

	// Step 2: Get Instagram Business Account linked to the page
	igURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=instagram_business_account{id}&access_token=%s", pageID, decryptedToken)
	igResp, err := client.Get(igURL)
	if err != nil {
		return "", fmt.Errorf("failed to get Instagram account: %w", err)
	}
	defer igResp.Body.Close()

	if igResp.StatusCode != 200 {
		body, _ := io.ReadAll(igResp.Body)
		return "", fmt.Errorf("failed to fetch Instagram account (status %d): %s", igResp.StatusCode, string(body))
	}

	var pageData struct {
		InstagramBusinessAccount struct {
			ID string `json:"id"`
		} `json:"instagram_business_account"`
	}
	if err := json.NewDecoder(igResp.Body).Decode(&pageData); err != nil {
		return "", fmt.Errorf("failed to parse Instagram account: %w", err)
	}

	if pageData.InstagramBusinessAccount.ID == "" {
		return "", fmt.Errorf("no Instagram Business Account linked to this page")
	}

	igID := pageData.InstagramBusinessAccount.ID

	// Save to database
	acc.ExternalID = igID
	acc.PageID = igID
	if err := h.DB.Save(acc).Error; err != nil {
		h.Logger.Error("⚠️ Failed to save Instagram Business Account ID to database",
			zap.Error(err),
			zap.String("instagram_id", igID))
		// Return the ID anyway - we can still use it for this request
	}

	return igID, nil
}

// verifyTokenType checks if the token is a Page Access Token or User Access Token
// Returns: tokenType ("PAGE" or "USER"), pageID, error
func (h *MessageHandler) verifyTokenType(ctx context.Context, token string) (string, string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Try to get /me/accounts - this works for both User and Page tokens
	// But for Page tokens, it returns the page info
	pagesURL := fmt.Sprintf("https://graph.facebook.com/v18.0/me/accounts?access_token=%s", token)
	resp, err := client.Get(pagesURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		// If /me/accounts fails, try /me to see if it's a Page token
		meURL := fmt.Sprintf("https://graph.facebook.com/v18.0/me?access_token=%s", token)
		meResp, err := client.Get(meURL)
		if err != nil {
			return "", "", fmt.Errorf("failed to verify token: %w", err)
		}
		defer meResp.Body.Close()

		if meResp.StatusCode == 200 {
			// This might be a Page token - try to get page info directly
			var pageInfo struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			meBody, _ := io.ReadAll(meResp.Body)
			if err := json.Unmarshal(meBody, &pageInfo); err == nil && pageInfo.ID != "" {
				// Check if this is actually a page by trying to get page details
				pageDetailURL := fmt.Sprintf("https://graph.facebook.com/v18.0/%s?fields=id,name,category&access_token=%s", pageInfo.ID, token)
				pageResp, err := client.Get(pageDetailURL)
				if err == nil {
					defer pageResp.Body.Close()
					if pageResp.StatusCode == 200 {
						return "PAGE", pageInfo.ID, nil
					}
				}
			}
		}

		return "", "", fmt.Errorf("token verification failed (status %d): %s", resp.StatusCode, string(body))
	}

	var pages struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(strings.NewReader(string(body))).Decode(&pages); err != nil {
		return "", "", err
	}

	if len(pages.Data) == 0 {
		// No pages found - this is likely a User token without page access
		return "USER", "", nil
	}

	// If we get pages, it's a User token that has page access
	// But for Instagram messaging, we need the actual Page Access Token
	// So we return USER here to indicate they need to generate a Page token
	return "USER", pages.Data[0].ID, nil
}

// ensureInstagramLoginToken refreshes Instagram Login tokens if they're about to expire
func (h *MessageHandler) ensureInstagramLoginToken(ctx context.Context, acc *storage.SocialAccount) error {
	if acc.LoginMethod != "instagram_login" {
		return nil
	}

	if acc.ExpiresAt == nil {
		return nil
	}

	timeUntilExpiry := time.Until(*acc.ExpiresAt)
	if timeUntilExpiry > 7*24*time.Hour {
		return nil
	}

	h.Logger.Info("🔄 Refreshing Instagram Login access token",
		zap.Stringer("tenant_id", acc.TenantID),
		zap.Stringer("account_id", acc.ID),
		zap.Duration("time_until_expiry", timeUntilExpiry))

	decryptedToken, err := utils.DecryptToken(acc.AccessToken)
	if err != nil {
		decryptedToken = acc.AccessToken
	}

	newToken, expiresIn, err := refreshInstagramLoginToken(ctx, decryptedToken)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "expired") ||
			strings.Contains(strings.ToLower(err.Error()), "invalid") {
			updates := map[string]interface{}{"connected": false}
			if updateErr := h.DB.WithContext(ctx).
				Model(&storage.SocialAccount{}).
				Where("id = ?", acc.ID).
				Updates(updates).Error; updateErr != nil {
				h.Logger.Warn("failed to mark Instagram account as disconnected", zap.Error(updateErr))
			} else {
				acc.Connected = false
				h.Logger.Warn("🔴 Instagram Login account marked as disconnected",
					zap.Stringer("tenant_id", acc.TenantID),
					zap.Stringer("account_id", acc.ID))
			}
		}
		return fmt.Errorf("instagram login token refresh failed: %w", err)
	}

	encryptedToken, err := utils.EncryptToken(newToken)
	if err != nil {
		return fmt.Errorf("encrypt access token: %w", err)
	}

	expiry := time.Now().Add(time.Duration(expiresIn) * time.Second)
	updates := map[string]interface{}{
		"access_token": encryptedToken,
		"expires_at":   &expiry,
	}

	if err := h.DB.WithContext(ctx).
		Model(&storage.SocialAccount{}).
		Where("id = ?", acc.ID).
		Updates(updates).Error; err != nil {
		h.Logger.Warn("failed to persist refreshed Instagram Login token", zap.Error(err))
		return fmt.Errorf("failed to save refreshed token: %w", err)
	}

	acc.AccessToken = newToken
	acc.ExpiresAt = &expiry

	h.Logger.Info("✅ Instagram Login token refreshed successfully",
		zap.Stringer("tenant_id", acc.TenantID),
		zap.Stringer("account_id", acc.ID))

	return nil
}

// shouldSendSticker determines if a sticker should be sent based on intent and message content
func (h *MessageHandler) shouldSendSticker(intent *ai.DetectedIntent, message string) bool {
	messageLower := strings.ToLower(message)

	// Check for positive tone
	if intent.Tone == "positive" && intent.Confidence > 0.6 {
		return true
	}

	// Check for appreciation keywords
	appreciationKeywords := []string{
		"thank", "thanks", "thank you", "appreciate", "grateful",
		"love it", "love this", "amazing", "great", "perfect",
		"wonderful", "excellent", "fantastic", "awesome", "brilliant",
		"best", "favorite", "favourite", "happy", "pleased",
		"delighted", "satisfied", "impressed",
	}

	for _, keyword := range appreciationKeywords {
		if strings.Contains(messageLower, keyword) {
			return true
		}
	}

	return false
}

// findMentionedPost searches for Instagram posts that match what the customer mentioned
func (h *MessageHandler) findMentionedPost(ctx context.Context, tenantID uuid.UUID, message string) string {
	messageLower := strings.ToLower(message)

	// Check if customer is talking about a post
	mentionsPost := strings.Contains(messageLower, "post") ||
		strings.Contains(messageLower, "photo you posted") ||
		strings.Contains(messageLower, "picture you posted") ||
		strings.Contains(messageLower, "video you posted") ||
		strings.Contains(messageLower, "that post") ||
		strings.Contains(messageLower, "your post") ||
		strings.Contains(messageLower, "the post") ||
		strings.Contains(messageLower, "reel") ||
		strings.Contains(messageLower, "story")

	if !mentionsPost {
		return ""
	}

	// Search for recent Instagram posts from this tenant
	// Look in PostAnalytics table for published posts with PlatformPostID
	var postAnalytics []storage.PostAnalytics
	if err := h.DB.Where("tenant_id = ? AND platform = ? AND platform_post_id != ?",
		tenantID, "instagram", "").
		Order("posted_at DESC").
		Limit(20).
		Find(&postAnalytics).Error; err != nil {
		h.Logger.Warn("Failed to search for posts", zap.Error(err))
		return ""
	}

	if len(postAnalytics) == 0 {
		// Fallback: search ScheduledPost table
		var posts []storage.ScheduledPost
		if err := h.DB.Where("tenant_id = ? AND platforms LIKE ? AND status = ?",
			tenantID, "%instagram%", "posted").
			Order("created_at DESC").
			Limit(20).
			Find(&posts).Error; err == nil && len(posts) > 0 {
			// Try to get PlatformPostID from PostAnalytics for these posts
			for _, post := range posts {
				var analytics storage.PostAnalytics
				if err := h.DB.Where("tenant_id = ? AND scheduled_post_id = ? AND platform = ? AND platform_post_id != ?",
					tenantID, post.ID, "instagram", "").
					First(&analytics).Error; err == nil {
					postAnalytics = append(postAnalytics, analytics)
				}
			}
		}
	}

	if len(postAnalytics) == 0 {
		return ""
	}

	// If customer mentions "recent", "latest", "newest", return the most recent post
	if strings.Contains(messageLower, "recent") || strings.Contains(messageLower, "latest") ||
		strings.Contains(messageLower, "newest") || strings.Contains(messageLower, "last") {
		if len(postAnalytics) > 0 && postAnalytics[0].PlatformPostID != "" {
			return postAnalytics[0].PlatformPostID
		}
	}

	// Try to match post by caption keywords
	// Get ScheduledPost captions for matching
	var scheduledPostIDs []uuid.UUID
	for _, analytics := range postAnalytics {
		scheduledPostIDs = append(scheduledPostIDs, analytics.ScheduledPostID)
	}

	var posts []storage.ScheduledPost
	if len(scheduledPostIDs) > 0 {
		h.DB.Where("id IN ?", scheduledPostIDs).Find(&posts)
	}

	// Extract keywords from message (remove common words)
	messageWords := strings.Fields(messageLower)
	keywords := []string{}
	for _, word := range messageWords {
		// Skip common words
		if word != "the" && word != "a" && word != "an" && word != "is" &&
			word != "are" && word != "was" && word != "were" && word != "post" &&
			word != "photo" && word != "picture" && word != "video" && word != "you" &&
			word != "your" && word != "that" && word != "this" && word != "about" &&
			word != "with" && word != "from" && len(word) > 2 {
			keywords = append(keywords, word)
		}
	}

	// Search for post with matching keywords in caption
	for _, analytics := range postAnalytics {
		if analytics.PlatformPostID == "" {
			continue
		}

		// Find corresponding ScheduledPost for caption
		var postCaption string
		for _, post := range posts {
			if post.ID == analytics.ScheduledPostID {
				postCaption = strings.ToLower(post.Caption)
				break
			}
		}

		if postCaption == "" {
			continue
		}

		matches := 0
		for _, keyword := range keywords {
			if strings.Contains(postCaption, keyword) {
				matches++
			}
		}
		// If at least 2 keywords match, or 1 keyword matches and it's a significant word, return this post
		if matches >= 2 || (matches >= 1 && len(keywords) == 1) {
			return analytics.PlatformPostID
		}
	}

	// If no specific match, return the most recent post
	if len(postAnalytics) > 0 && postAnalytics[0].PlatformPostID != "" {
		return postAnalytics[0].PlatformPostID
	}

	return ""
}

// SendWhatsAppReply sends a WhatsApp reply to a customer (used by workflows)
func (h *MessageHandler) SendWhatsAppReply(ctx context.Context, tenantID uuid.UUID, customerPlatformID, message string) error {
	// Load social account for tenant
	var acc storage.SocialAccount
	if err := h.DB.Where("tenant_id = ? AND channel = ?", tenantID, "whatsapp").First(&acc).Error; err != nil {
		return fmt.Errorf("whatsapp account not found for tenant: %w", err)
	}

	return h.sendWhatsAppReply(ctx, &acc, customerPlatformID, message)
}

// isPaymentConfirmation checks if message is a payment confirmation
func (h *MessageHandler) isPaymentConfirmation(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "paid") ||
		strings.Contains(lower, "sent payment") ||
		strings.Contains(lower, "transferred") ||
		strings.Contains(lower, "payment made") ||
		strings.Contains(lower, "made the payment") ||
		strings.Contains(lower, "made payment") ||
		strings.Contains(lower, "payment done") ||
		strings.Contains(lower, "i have sent") ||
		strings.Contains(lower, "i sent") ||
		strings.Contains(lower, "already paid")
}

// createPendingPayment creates a pending payment record when customer claims payment
func (h *MessageHandler) createPendingPayment(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, interaction *storage.Interaction, message string) {
	if interaction == nil {
		return
	}

	// Extract amount from message if possible (simple regex)
	amount := h.extractAmountFromMessage(message)
	if amount <= 0 {
		amount = h.extractAmountFromMessage(interaction.Content)
	}

	// Prefer attaching this claim to the most recent unambiguous pending order
	var order storage.Order
	orderFound := false
	if interaction.ConversationID != uuid.Nil {
		if err := h.DB.WithContext(ctx).
			Where("tenant_id = ? AND customer_id = ? AND conversation_id = ? AND status = ?", tenantID, customerID, interaction.ConversationID, "pending").
			Order("created_at DESC").
			First(&order).Error; err == nil {
			orderFound = true
		}
	}
	if !orderFound {
		if err := h.DB.WithContext(ctx).
			Where("tenant_id = ? AND customer_id = ? AND platform = ? AND status = ?", tenantID, customerID, interaction.Platform, "pending").
			Where("created_at > ?", time.Now().Add(-7*24*time.Hour)).
			Order("created_at DESC").
			First(&order).Error; err == nil {
			orderFound = true
		}
	}
	if !orderFound {
		h.Logger.Warn("payment claim received but no pending order found to attach", zap.Stringer("tenant_id", tenantID), zap.Stringer("customer_id", customerID), zap.String("platform", interaction.Platform))
		return
	}

	now := time.Now()
	// Upsert payment row for this order
	var payment storage.Payment
	if err := h.DB.WithContext(ctx).
		Where("tenant_id = ? AND order_id = ?", tenantID, order.ID).
		First(&payment).Error; err == nil {
		payment.ClaimedByCustomer = true
		payment.ClaimedAt = &now
		if payment.Status == "" {
			payment.Status = "pending"
		}
		if payment.VerificationStatus == "" {
			payment.VerificationStatus = "unverified"
		}
		if payment.Amount <= 0 && amount > 0 {
			payment.Amount = amount
		}
		if payment.Amount <= 0 {
			payment.Amount = order.TotalAmount
		}
		if payment.Currency == "" {
			payment.Currency = order.Currency
			if payment.Currency == "" {
				payment.Currency = "NGN"
			}
		}
		if payment.PaymentMethod == "" {
			payment.PaymentMethod = "bank_transfer"
		}
		_ = h.DB.WithContext(ctx).Save(&payment).Error
		h.Logger.Info("💰 Payment marked claimed (existing)", zap.Stringer("payment_id", payment.ID), zap.Stringer("order_id", order.ID), zap.Stringer("tenant_id", tenantID))
		return
	}

	payment = storage.Payment{
		TenantID:           tenantID,
		OrderID:            order.ID,
		CustomerID:         customerID,
		PaymentReference:   order.PaymentReference,
		Amount:             amount,
		Currency:           order.Currency,
		PaymentMethod:      "bank_transfer",
		Status:             "pending",
		VerificationStatus: "unverified",
		ClaimedByCustomer:  true,
		ClaimedAt:          &now,
		Metadata:           fmt.Sprintf(`{"interaction_id":"%s","customer_message":"%s"}`, interaction.ID.String(), strings.ReplaceAll(message, `"`, `\"`)),
		CreatedAt:          now,
	}
	if payment.Amount <= 0 {
		payment.Amount = order.TotalAmount
	}
	if payment.Currency == "" {
		payment.Currency = "NGN"
	}
	if payment.PortalToken == "" {
		payment.PortalToken = utils.GeneratePortalToken()
	}
	if payment.ReceiptID == "" {
		payment.ReceiptID = utils.GenerateReceiptID()
	}
	if err := h.DB.WithContext(ctx).Create(&payment).Error; err != nil {
		h.Logger.Error("failed to create pending payment",
			zap.Error(err),
			zap.Stringer("tenant_id", tenantID),
			zap.Stringer("customer_id", customerID),
			zap.Stringer("order_id", order.ID))
		return
	}

	h.Logger.Info("💰 Pending payment created (order-linked)",
		zap.Stringer("payment_id", payment.ID),
		zap.Stringer("order_id", order.ID),
		zap.String("payment_ref", payment.PaymentReference),
		zap.Float64("amount", payment.Amount),
		zap.Stringer("tenant_id", tenantID))

	// TODO: Trigger payment.created webhook notification
	// This requires WebhookService to be added to MessageHandler
	// webhookService.NotifyPaymentCreated(ctx, &payment)
}

// extractAmountFromMessage tries to extract payment amount from customer message
func (h *MessageHandler) extractAmountFromMessage(message string) float64 {
	// Remove commas and look for numbers
	message = strings.ReplaceAll(message, ",", "")

	// Common patterns: "20000", "20,000", "20000 naira", "NGN 20000"
	// Simple extraction: find first number that looks like an amount
	words := strings.Fields(message)
	for _, word := range words {
		// Remove currency symbols
		word = strings.TrimPrefix(word, "NGN")
		word = strings.TrimPrefix(word, "₦")
		word = strings.TrimSpace(word)

		// Try to parse as float
		if amount, err := strconv.ParseFloat(word, 64); err == nil && amount > 0 && amount < 100000000 {
			return amount
		}
	}

	return 0 // Unknown amount
}

// SendFollowUp sends a system-initiated follow-up message to a customer
// Feature 8: Follow-Up Scheduler usage
func (h *MessageHandler) SendFollowUp(ctx context.Context, tenantID, customerID uuid.UUID, content string) error {
	// 1. Fetch customer to get platform and channel (phone/id)
	var customer storage.Customer
	if err := h.DB.Where("id = ? AND tenant_id = ?", customerID, tenantID).First(&customer).Error; err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}

	// 2. Fetch tenant to get credentials
	var tenant storage.Tenant
	if err := h.DB.Where("id = ?", tenantID).First(&tenant).Error; err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	// 3. Construct synthetic incoming message
	msg := IncomingMessage{
		TenantID:     tenantID,
		Platform:     customer.Channel,    // "whatsapp", "telegram"
		CustomerID:   customer.PlatformID, // phone number or ID
		CustomerName: customer.DisplayName,
		Content:      content,
		MessageType:  "text",
		Timestamp:    time.Now(),
	}

	h.Logger.Info("📤 Sending follow-up message",
		zap.String("customer", customer.DisplayName),
		zap.String("platform", customer.Channel))

	return h.sendReplyWithImages(ctx, msg, content, nil)
}

// safeRenderReply parses a potential JSON reply and returns clean text segments
// This ensures that internal JSON structs never leak to the customer
func (h *MessageHandler) safeRenderReply(rawReply string) (messages []string, images []string, variant string) {
	trimmed := strings.TrimSpace(rawReply)

	// If not JSON-like, return raw text
	if !strings.HasPrefix(trimmed, "{") {
		return []string{rawReply}, nil, ""
	}

	// Attempt parsing
	var structured struct {
		Messages  []string `json:"messages"`
		MediaURLs []string `json:"media_urls"`
		Variant   string   `json:"variant"`
	}

	// If unmarshal fails, it might be partial JSON or hallucinations.
	// We MUST sanitize or fallback safely.
	if err := json.Unmarshal([]byte(rawReply), &structured); err != nil {
		h.Logger.Warn("⚠️ Failed to parse JSON reply, returning raw text as fallback (potential leak risk)",
			zap.Error(err), zap.String("reply", rawReply))
		// Fallback: Return raw text but log warning.
		// In a stricter system, we might return a generic error message to user.
		return []string{rawReply}, nil, ""
	}

	// Return parsed content
	if len(structured.Messages) == 0 {
		return []string{rawReply}, nil, "" // Fallback if valid JSON but empty content
	}

	return structured.Messages, structured.MediaURLs, structured.Variant
}
