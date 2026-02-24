package social

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	v4types "github.com/similadayo/brancr/internal/ai_v4/types"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/types"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ProductionMessageProcessor implements the production-grade message processing flow
// Core principle: Buffer first, process once, generate once, send smartly
type ProductionMessageProcessor struct {
	DB             *gorm.DB
	Logger         *zap.Logger
	IntentDetector *ai.IntentDetector
	Responder      interface {
		GenerateCustomerReply(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, message string, intent string, platform string, conversationID ...uuid.UUID) (string, error)
	}
	ConversationLock  *ConversationLock
	ReplySequencer    *ReplySequencer
	EscalationService *EscalationService // Production escalation service (optional, for future use)
	EscalationFunc    func(ctx context.Context, tenantID uuid.UUID, interactionID uuid.UUID, customerName, platform, message, suggestedReply, intent, tone string, confidence float64, reason string) error
	// Old escalation check function (from MessageHandler.shouldEscalateToHuman)
	// This is the proven escalation logic that works correctly
	ShouldEscalateFunc func(ctx context.Context, msg IncomingMessage, customer *storage.Customer, intent *ai.DetectedIntent, conversationID uuid.UUID, events []ai.Event, salesConv *storage.SalesConversation) (bool, string)
	NegotiationManager interface{} // For pricing approval escalation checks
	sendReplyFunc      func(ctx context.Context, msg IncomingMessage, reply string) error
}

func legacyPreIntentPathEnabled() bool {
	return strings.ToLower(strings.TrimSpace(os.Getenv("AI_V4_LEGACY_PRE_INTENT"))) == "true"
}

func pythonTextPassthroughEnabled() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("AI_V4_PYTHON_TEXT_PASSTHROUGH")))
	if raw == "" {
		return true
	}
	return raw == "true" || raw == "1" || raw == "yes"
}

// NewProductionMessageProcessor creates a new production message processor
func NewProductionMessageProcessor(
	db *gorm.DB,
	logger *zap.Logger,
	intentDetector *ai.IntentDetector,
	responder interface {
		GenerateCustomerReply(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, message string, intent string, platform string, conversationID ...uuid.UUID) (string, error)
	},
	conversationLock *ConversationLock,
	replySequencer *ReplySequencer,
	escalationService *EscalationService,
	escalationFunc func(ctx context.Context, tenantID uuid.UUID, interactionID uuid.UUID, customerName, platform, message, suggestedReply, intent, tone string, confidence float64, reason string) error,
	shouldEscalateFunc func(ctx context.Context, msg IncomingMessage, customer *storage.Customer, intent *ai.DetectedIntent, conversationID uuid.UUID, events []ai.Event, salesConv *storage.SalesConversation) (bool, string),
	negotiationManager interface{},
	sendReplyFunc func(ctx context.Context, msg IncomingMessage, reply string) error,
) *ProductionMessageProcessor {
	return &ProductionMessageProcessor{
		DB:                 db,
		Logger:             logger,
		IntentDetector:     intentDetector,
		Responder:          responder,
		ConversationLock:   conversationLock,
		ReplySequencer:     replySequencer,
		EscalationService:  escalationService,
		EscalationFunc:     escalationFunc,
		ShouldEscalateFunc: shouldEscalateFunc,
		NegotiationManager: negotiationManager,
		sendReplyFunc:      sendReplyFunc,
	}
}

// SetSendReplyFunc sets the send reply function (for wiring after MessageHandler is created)
func (p *ProductionMessageProcessor) SetSendReplyFunc(fn func(ctx context.Context, msg IncomingMessage, reply string) error) {
	p.sendReplyFunc = fn
}

// ProcessBufferClose is called when a buffer closes
// This is where ALL processing happens: intent detection, AI generation, escalation
func (p *ProductionMessageProcessor) ProcessBufferClose(ctx context.Context, messages []IncomingMessage) error {
	if len(messages) == 0 {
		return nil
	}

	// Use the most recent message for metadata
	primaryMsg := messages[len(messages)-1]

	// Step 1: Combine messages into single text
	combinedText := p.combineMessages(messages)

	// Log individual messages for debugging sequence issues
	individualMsgs := make([]string, len(messages))
	for i, m := range messages {
		individualMsgs[i] = m.Content
	}

	p.Logger.Info("📦 Processing buffer close",
		zap.Int("message_count", len(messages)),
		zap.String("combined_text", combinedText),
		zap.Int("combined_length", len(combinedText)),
		zap.Strings("individual_messages", individualMsgs),
		zap.Stringer("tenant_id", primaryMsg.TenantID),
		zap.String("platform", primaryMsg.Platform))

	// Step 2: Check conversation lock (prevent double replies)

	if p.ConversationLock != nil && p.ConversationLock.IsLocked(primaryMsg.TenantID, primaryMsg.CustomerID, primaryMsg.Platform) {
		p.Logger.Info("🔒 Conversation is locked, skipping reply",
			zap.Stringer("tenant_id", primaryMsg.TenantID),
			zap.String("platform", primaryMsg.Platform))
		return nil // Not an error, just skip
	}

	// Step 3: Track customer and create conversation
	customer, err := p.trackCustomer(ctx, primaryMsg)
	if err != nil {
		return fmt.Errorf("failed to track customer: %w", err)
	}

	conversation, err := p.createOrGetConversation(ctx, primaryMsg, customer.ID)
	var conversationID uuid.UUID
	if err != nil {
		p.Logger.Warn("failed to create/get conversation", zap.Error(err))
		// Continue anyway
	} else {
		conversationID = conversation.ID
	}

	legacyPreIntent := legacyPreIntentPathEnabled()
	intent := &ai.DetectedIntent{
		Intent:     "python_authority",
		Tone:       "neutral",
		Confidence: 1.0,
		Reasoning:  "pre_intent_bypassed_v4_authority",
	}
	var events []ai.Event

	// Step 4: Legacy pre-intent path (disabled by default).
	if legacyPreIntent {
		if p.IntentDetector != nil {
			detectedIntent, detectErr := p.IntentDetector.DetectIntent(ctx, primaryMsg.TenantID, combinedText, primaryMsg.Platform)
			if detectErr != nil {
				p.Logger.Error("intent detection failed", zap.Error(detectErr))
				detectedIntent = p.IntentDetector.FallbackDetection(combinedText)
			}
			if detectedIntent != nil {
				intent = detectedIntent
			}
		}

		p.Logger.Info("🎯 Intent detected",
			zap.String("intent", intent.Intent),
			zap.Float64("confidence", intent.Confidence),
			zap.Stringer("tenant_id", primaryMsg.TenantID))

		// Step 4.5: Extract events for escalation and sales conversation tracking
		events = ai.ExtractEvents(combinedText)
	} else {
		p.Logger.Info("v4_authority_pre_intent_bypassed",
			zap.Stringer("tenant_id", primaryMsg.TenantID),
			zap.String("platform", primaryMsg.Platform))
	}

	// Step 4.6: Update conversation state (last message time, status)
	if conversation != nil && conversation.ID != uuid.Nil {
		conversation.LastMessageAt = primaryMsg.Timestamp
		conversation.Status = "active"
		p.DB.Save(&conversation)
	}

	// Step 4.7: Update persisted conversation state (SalesConversation) from deterministic events.
	// This makes follow-ups route correctly even when the follow-up message is vague (e.g. "any update?").
	var salesConv *storage.SalesConversation
	if legacyPreIntent && conversationID != uuid.Nil {
		scManager := &ai.SalesConversationManager{DB: p.DB, Logger: p.Logger}
		if updated, scErr := scManager.ApplyDeterministicSignals(ctx, primaryMsg.TenantID, customer.ID, conversationID, intent.Intent, events); scErr != nil {
			p.Logger.Warn("failed to apply deterministic sales conversation signals", zap.Error(scErr), zap.Stringer("tenant_id", primaryMsg.TenantID), zap.Stringer("conversation_id", conversationID))
		} else {
			salesConv = updated
		}
	}

	// If sales conversation not found via ApplyDeterministicSignals, try direct lookup
	if legacyPreIntent && salesConv == nil && conversationID != uuid.Nil {
		var sc storage.SalesConversation
		if err := p.DB.Where("tenant_id = ? AND customer_id = ? AND conversation_id = ?",
			primaryMsg.TenantID, customer.ID, conversationID).
			Order("updated_at DESC").
			First(&sc).Error; err == nil {
			salesConv = &sc
		}
	}

	// Step 4.8: Evaluate escalation using PROVEN old logic (shouldEscalateToHuman)
	// This handles negotiation/offers first, then general escalation checks
	// Escalation happens AFTER intent detection, BEFORE AI generation

	var escalationDecision *EscalationDecision
	var escalationReason string

	if legacyPreIntent {
		// First: Check for pricing approval (negotiation/offers) - same as old logic
		offer := ai.DetectOfferInText(combinedText)
		if offer > 0 && salesConv != nil && salesConv.PriceDiscussed > 0 {
			// Extract negotiation metadata
			var items []map[string]interface{}
			var minP float64
			var productID uuid.UUID
			var maxConcessions int = 2
			if salesConv.ItemsDiscussed != "" && salesConv.ItemsDiscussed != "[]" {
				_ = json.Unmarshal([]byte(salesConv.ItemsDiscussed), &items)
				if len(items) > 0 {
					if v, ok := items[0]["negotiation_min_price"].(float64); ok {
						minP = v
					}
					if v, ok := items[0]["product_id"].(string); ok {
						if id, parseErr := uuid.Parse(v); parseErr == nil {
							productID = id
						}
					}
					if v, ok := items[0]["max_concessions"].(float64); ok {
						maxConcessions = int(v)
					}
				}
			}

			// Record customer offer
			if p.NegotiationManager != nil {
				type NegotiationManager interface {
					RecordCustomerOffer(ctx context.Context, salesConvID uuid.UUID, productID uuid.UUID, offer float64) (*storage.NegotiationState, error)
					CheckEscalationEligibility(ctx context.Context, salesConvID uuid.UUID, productID uuid.UUID, offer float64, buyingIntent bool, proximity float64, maxConcessions int, minPrice float64) (bool, string, error)
					ExecuteEscalation(ctx context.Context, tenantID uuid.UUID, salesConvID uuid.UUID, productID uuid.UUID, discussedPrice float64, minPrice float64, offers []float64, metadata map[string]interface{}, reason string) (*storage.EscalationEvent, error)
				}
				if nm, ok := p.NegotiationManager.(NegotiationManager); ok {
					if _, recErr := nm.RecordCustomerOffer(ctx, salesConv.ID, productID, offer); recErr != nil {
						p.Logger.Warn("failed to record customer offer", zap.Error(recErr))
					}

					// Check buying intent
					contentLower := strings.ToLower(combinedText)
					buyingIntent := strings.Contains(contentLower, "cash") || strings.Contains(contentLower, "pay") || strings.Contains(contentLower, "delivery") || strings.Contains(contentLower, "i'll buy") || strings.Contains(contentLower, "i will buy") || strings.Contains(contentLower, "buy now") || strings.Contains(contentLower, "i'm buying") || strings.Contains(contentLower, "i can pay")

					// Get proximity threshold
					var policy storage.TenantPolicy
					proximity := 0.10
					if polErr := p.DB.Where("tenant_id = ?", primaryMsg.TenantID).First(&policy).Error; polErr == nil {
						if policy.NegotiationProximityPct > 0 {
							proximity = policy.NegotiationProximityPct
						}
					}

					// Check escalation eligibility
					eligible, reason, eligErr := nm.CheckEscalationEligibility(ctx, salesConv.ID, productID, offer, buyingIntent, proximity, maxConcessions, minP)
					if eligErr != nil {
						p.Logger.Warn("error checking escalation eligibility", zap.Error(eligErr))
					} else if eligible {
						// Execute escalation
						if _, execErr := nm.ExecuteEscalation(ctx, primaryMsg.TenantID, salesConv.ID, productID, salesConv.PriceDiscussed, minP, []float64{offer}, nil, reason); execErr != nil {
							p.Logger.Error("failed to execute escalation", zap.Error(execErr))
						} else {
							escalationDecision = &EscalationDecision{
								ShouldEscalate: true,
								Type:           EscalationTypePricingApproval,
								Reason:         reason,
								PauseAI:        true,
								DisableAI:      false,
							}
							escalationReason = reason
						}
					}
				}
			}
		}

		// Second: Use old proven escalation logic (shouldEscalateToHuman) if pricing escalation didn't trigger
		if escalationDecision == nil && p.ShouldEscalateFunc != nil {
			shouldEscalate, reason := p.ShouldEscalateFunc(ctx, primaryMsg, customer, intent, conversationID, events, salesConv)

			// 🆕 FIX: Override legacy escalation for greetings, casual messages, AND V4 negotiation
			// The legacy function is too aggressive and overrides V4's smart negotiation logic
			if shouldEscalate && intent != nil {
				ignoredIntents := map[string]bool{
					"greeting":          true,
					"casual":            true,
					"negotiation":       true,
					"negotiation_offer": true,
					"price_objection":   true,
				}
				if ignoredIntents[intent.Intent] {
					p.Logger.Info("🛡️ Blocking legacy escalation for V4 intent",
						zap.String("original_reason", reason),
						zap.String("intent", intent.Intent))
					shouldEscalate = false
				}
			}

			if shouldEscalate {
				// Determine escalation type based on reason
				escalationType := EscalationTypeHumanTakeover
				if strings.Contains(reason, "payment_mode") || strings.Contains(reason, "approval") || strings.Contains(reason, "negotiation") {
					escalationType = EscalationTypePricingApproval
				}

				escalationDecision = &EscalationDecision{
					ShouldEscalate: true,
					Type:           escalationType,
					Reason:         reason,
					PauseAI:        escalationType == EscalationTypePricingApproval,
					DisableAI:      escalationType == EscalationTypeHumanTakeover,
				}
				escalationReason = reason
			}
		}
	}

	// Step 5: Get or create interaction (for audit trail)
	// CRITICAL: Individual messages were already saved by saveMessageOnly() before buffering
	// If we have multiple messages (buffer was used), find the most recent interaction instead of creating a duplicate
	var interaction storage.Interaction
	if len(messages) > 1 {
		// Multiple messages = buffer was used, individual messages already saved
		// Find the most recent interaction from the last message to use for tracking
		if primaryMsg.MessageID != "" {
			if err := p.DB.Where("tenant_id = ? AND platform = ? AND platform_message_id = ? AND direction = ?",
				primaryMsg.TenantID, primaryMsg.Platform, primaryMsg.MessageID, "incoming").
				Order("created_at DESC").
				First(&interaction).Error; err != nil {
				p.Logger.Warn("⚠️ Could not find existing interaction for combined message (individual messages should have been saved)",
					zap.Error(err),
					zap.String("platform_message_id", primaryMsg.MessageID),
					zap.Int("message_count", len(messages)))
				// Create minimal interaction for tracking (fallback only)
				interaction = storage.Interaction{
					TenantID:       primaryMsg.TenantID,
					CustomerID:     customer.ID,
					ConversationID: conversationID,
					Platform:       primaryMsg.Platform,
					Direction:      "incoming",
					MessageType:    primaryMsg.MessageType,
					Content:        combinedText, // Combined text for reference only
					DetectedIntent: intent.Intent,
					DetectedTone:   intent.Tone,
					Confidence:     intent.Confidence,
					ResponseType:   "auto_reply",
					ResponseStatus: "pending",
				}
				if err := p.DB.Create(&interaction).Error; err != nil {
					p.Logger.Error("failed to create fallback interaction", zap.Error(err))
				}
			} else {
				// Found existing interaction - use it (this is the correct path)
				p.Logger.Debug("✅ Using existing interaction for combined message (no duplicate created)",
					zap.Stringer("interaction_id", interaction.ID),
					zap.Int("message_count", len(messages)))
			}
		} else {
			// No message ID - try to find by customer and timestamp
			cutoffTime := time.Now().Add(-5 * time.Minute)
			if err := p.DB.Where("tenant_id = ? AND customer_id = ? AND platform = ? AND direction = ? AND created_at > ?",
				primaryMsg.TenantID, customer.ID, primaryMsg.Platform, "incoming", cutoffTime).
				Order("created_at DESC").
				First(&interaction).Error; err != nil {
				p.Logger.Warn("⚠️ Could not find existing interaction for combined message",
					zap.Error(err),
					zap.Int("message_count", len(messages)))
			} else {
				p.Logger.Debug("✅ Using existing interaction for combined message",
					zap.Stringer("interaction_id", interaction.ID))
			}
		}
	} else {
		// Single message - should have been saved already, try to find it
		if primaryMsg.MessageID != "" {
			if err := p.DB.Where("tenant_id = ? AND platform = ? AND platform_message_id = ? AND direction = ?",
				primaryMsg.TenantID, primaryMsg.Platform, primaryMsg.MessageID, "incoming").
				Order("created_at DESC").
				First(&interaction).Error; err != nil {
				// Not found - create it (shouldn't happen, but fallback)
				interaction = storage.Interaction{
					TenantID:          primaryMsg.TenantID,
					CustomerID:        customer.ID,
					ConversationID:    conversationID,
					Platform:          primaryMsg.Platform,
					Direction:         "incoming",
					MessageType:       primaryMsg.MessageType,
					Content:           combinedText,
					PlatformMessageID: primaryMsg.MessageID,
					DetectedIntent:    intent.Intent,
					DetectedTone:      intent.Tone,
					Confidence:        intent.Confidence,
					ResponseType:      "auto_reply",
					ResponseStatus:    "pending",
					Metadata:          p.metadataToJSON(primaryMsg.Metadata),
				}
				if err := p.DB.Create(&interaction).Error; err != nil {
					p.Logger.Error("failed to create interaction", zap.Error(err))
				}
			}
		} else {
			// No message ID - create interaction (fallback)
			interaction = storage.Interaction{
				TenantID:       primaryMsg.TenantID,
				CustomerID:     customer.ID,
				ConversationID: conversationID,
				Platform:       primaryMsg.Platform,
				Direction:      "incoming",
				MessageType:    primaryMsg.MessageType,
				Content:        combinedText,
				DetectedIntent: intent.Intent,
				DetectedTone:   intent.Tone,
				Confidence:     intent.Confidence,
				ResponseType:   "auto_reply",
				ResponseStatus: "pending",
				Metadata:       p.metadataToJSON(primaryMsg.Metadata),
			}
			if err := p.DB.Create(&interaction).Error; err != nil {
				p.Logger.Error("failed to create interaction", zap.Error(err))
			}
		}
	}

	// Step 5.5: Create dashboard notification for incoming message
	if interaction.ID != uuid.Nil && conversationID != uuid.Nil {
		p.createNotificationForIncomingMessage(primaryMsg, customer, conversationID, interaction.ID)
	}

	// Step 5.6: Check escalation aftermath will be done AFTER AI generation
	// (moved to after AI generation to use detected intent and check if AI succeeded)

	// Step 6: Handle escalation if needed (using old proven logic)
	if escalationDecision != nil && escalationDecision.ShouldEscalate {
		// Use old escalation path: generate AI reply as suggestion, notify tenant, don't send hardcoded message to customer
		// The old logic generates an AI reply as a "suggested reply" for the tenant, not a hardcoded message
		return p.handleEscalationOldWay(ctx, primaryMsg, customer, intent, interaction, escalationReason, events)
	}

	// Step 7: Lock conversation BEFORE AI generation
	if p.ConversationLock != nil {
		if !p.ConversationLock.Lock(primaryMsg.TenantID, primaryMsg.CustomerID, primaryMsg.Platform) {
			p.Logger.Warn("failed to acquire conversation lock, skipping reply")
			return nil
		}
		defer func() {
			// Unlock after reply is sent (or on error)
			if p.ConversationLock != nil {
				p.ConversationLock.Unlock(primaryMsg.TenantID, primaryMsg.CustomerID, primaryMsg.Platform)
			}
		}()
	}

	// Step 8: AI generation (SINGLE CALL)
	// Use structured reply method if available, otherwise fall back to string parsing
	var structuredReply types.StructuredReply

	// 🆕 FIX: Inject threadID and platformCustomerID into context for V4 adapter
	// This is CRITICAL for Instagram where recipient ID (IGSID) differs from internal customer ID
	threadIDForCtx := primaryMsg.ThreadID
	if threadIDForCtx == "" {
		threadIDForCtx = primaryMsg.CustomerID // Fallback to customerID if threadID is empty
	}
	ctxWithIDs := v4types.WithThreadID(ctx, threadIDForCtx)
	ctxWithIDs = v4types.WithPlatformCustomerID(ctxWithIDs, primaryMsg.CustomerID)

	p.Logger.Info("🔗 Injecting platform IDs into context",
		zap.String("thread_id", threadIDForCtx),
		zap.String("customer_id", primaryMsg.CustomerID),
		zap.String("platform", primaryMsg.Platform))

	// Try to use structured reply method (if responder supports it)
	if structuredResponder, ok := p.Responder.(interface {
		GenerateCustomerReplyStructured(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, message string, intent string, platform string, conversationID ...uuid.UUID) (*types.StructuredReply, error)
	}); ok {
		structuredReplyPtr, structErr := structuredResponder.GenerateCustomerReplyStructured(
			ctxWithIDs, // Use context with threadID
			primaryMsg.TenantID,
			customer.ID,
			combinedText,
			intent.Intent,
			primaryMsg.Platform,
			conversationID,
		)
		if structErr != nil {
			// Handle error below
			return p.handleAIError(ctx, primaryMsg, customer, interaction, intent, combinedText, structErr)
		}
		if structuredReplyPtr != nil {
			structuredReply = *structuredReplyPtr
		}
	} else {
		// Fallback: use string method and parse
		aiReply, replyErr := p.Responder.GenerateCustomerReply(
			ctxWithIDs, // Use context with threadID
			primaryMsg.TenantID,
			customer.ID,
			combinedText,
			intent.Intent,
			primaryMsg.Platform,
			conversationID,
		)
		if replyErr != nil {
			// Handle error below
			return p.handleAIError(ctx, primaryMsg, customer, interaction, intent, combinedText, replyErr)
		}
		if pythonTextPassthroughEnabled() {
			structuredReply = types.StructuredReply{
				ReplyMode:  "single",
				Messages:   []string{aiReply},
				Confidence: "high",
				Escalation: nil,
			}
		} else {
			structuredReply = p.parseReplyToStructured(aiReply)
		}
	}

	// Step 9: Send reply using sequencer (delivery layer)
	p.Logger.Info("📤 Preparing to send structured reply",
		zap.String("reply_mode", structuredReply.ReplyMode),
		zap.Int("message_count", len(structuredReply.Messages)),
		zap.Int("input_message_count", len(messages)),
		zap.String("combined_input_length", fmt.Sprintf("%d chars", len(combinedText))),
		zap.String("preview", func() string {
			if len(structuredReply.Messages) > 0 && len(structuredReply.Messages[0]) > 100 {
				return structuredReply.Messages[0][:100] + "..."
			}
			if len(structuredReply.Messages) > 0 {
				return structuredReply.Messages[0]
			}
			return ""
		}()))

	if err := p.sendStructuredReply(ctx, primaryMsg, structuredReply); err != nil {
		if strings.Contains(err.Error(), "no connected whatsapp account") {
			p.Logger.Warn("failed to send structured reply (configuration issue)", zap.Error(err))
		} else {
			p.Logger.Error("failed to send structured reply", zap.Error(err))
		}
		return err
	}

	// Step 10: Update incoming interaction status
	interaction.ResponseStatus = "sent"
	interaction.ResponseType = "auto_reply"
	if len(structuredReply.Messages) > 0 {
		// Store the first message as FinalReply (or combine all if multiple)
		if len(structuredReply.Messages) == 1 {
			interaction.FinalReply = structuredReply.Messages[0]
		} else {
			// Combine all messages for FinalReply
			interaction.FinalReply = strings.Join(structuredReply.Messages, "\n\n")
		}
	}
	p.DB.Save(&interaction)

	// Step 11: Outgoing interactions are created by sendReplyFunc (MessageHandler.SendReply)
	// which is called by ReplySequencer for each message in the sequence
	// MessageHandler.SendReply already creates outgoing interactions (see reply_sender.go)
	// So we don't need to create them here - that would cause duplicates

	// Step 12: Track usage for billing (AI reply sent)
	// Note: UsageService is accessed via MessageHandler, so we'll track it there
	// For now, we rely on the old flow's usage tracking, but we should add it here too
	// This is handled by the sendReplyFunc which calls MessageHandler.SendReply

	// Step 13: Update conversation's last message time
	if conversationID != uuid.Nil {
		var conv storage.Conversation
		if err := p.DB.First(&conv, "id = ?", conversationID).Error; err == nil {
			conv.LastMessageAt = time.Now()
			conv.Status = "active"
			if err := p.DB.Save(&conv).Error; err != nil {
				p.Logger.Warn("failed to update conversation after outgoing message", zap.Error(err))
			}
		}
	}

	// Step 14: Conversation history is saved by the active responder path.
	// This preserves continuity across buffer batches.
	// The conversation is saved with combinedText (buffered messages) and the full AI reply.
	// This allows the AI to see its previous replies when processing the next buffer batch.

	// Combine all AI reply messages for logging
	fullAIReply := ""
	if len(structuredReply.Messages) > 0 {
		fullAIReply = strings.Join(structuredReply.Messages, "\n\n")
	}

	p.Logger.Info("✅ Successfully processed buffer and sent reply",
		zap.Stringer("tenant_id", primaryMsg.TenantID),
		zap.Stringer("customer_id", customer.ID),
		zap.Stringer("conversation_id", conversationID),
		zap.String("platform", primaryMsg.Platform),
		zap.Int("message_count", len(messages)),
		zap.Int("reply_messages", len(structuredReply.Messages)),
		zap.String("reply_mode", structuredReply.ReplyMode),
		zap.String("combined_text_preview", func() string {
			if len(combinedText) > 50 {
				return combinedText[:50] + "..."
			}
			return combinedText
		}()),
		zap.String("ai_reply_preview", func() string {
			if len(fullAIReply) > 50 {
				return fullAIReply[:50] + "..."
			}
			return fullAIReply
		}()),
		zap.String("note", "Conversation context saved for continuity"))

	// Step 15: Check escalation aftermath (customer follow-up after escalated conversation)
	// Only escalates if intent confidence is very low OR AI generation failed
	// Since AI generation succeeded here, we pass aiReplyGenerated=true
	if legacyPreIntent && interaction.ID != uuid.Nil && conversationID != uuid.Nil {
		p.checkAndNotifyEscalationAftermath(ctx, primaryMsg, customer, interaction.ID, conversationID, intent, true)
	}

	return nil
}

// combineMessages combines multiple messages into a single text
func (p *ProductionMessageProcessor) combineMessages(messages []IncomingMessage) string {
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

// sendStructuredReply sends a structured reply using the sequencer
// Applies the same sanitization as the old architecture (message_handler.go)
func (p *ProductionMessageProcessor) sendStructuredReply(ctx context.Context, msg IncomingMessage, reply types.StructuredReply) error {
	if p.sendReplyFunc == nil {
		return fmt.Errorf("send reply function is not configured")
	}

	// Python authority pass-through: send messages exactly as returned without Go text mutation.
	if pythonTextPassthroughEnabled() {
		p.Logger.Info("python_text_passthrough_send",
			zap.Int("message_count", len(reply.Messages)),
			zap.String("reply_mode", reply.ReplyMode))
		sent := 0
		for _, msgText := range reply.Messages {
			if msgText == "" {
				continue
			}
			if err := p.sendReplyFunc(ctx, msg, msgText); err != nil {
				return err
			}
			sent++
		}
		if sent == 0 {
			return fmt.Errorf("no messages in structured reply")
		}
		return nil
	}

	// CRITICAL: Sanitize each message using CustomerOutputSafetyGate (same as old architecture)
	// This removes internal phrases like "Summary:", "Manager approval", "escalation", etc.
	safetyGate := ai.NewCustomerOutputSafetyGate()
	sanitizedMessages := make([]string, len(reply.Messages))
	for i, msgText := range reply.Messages {
		sanitized := safetyGate.SanitizeCustomerReply(msgText)

		// Validate for forbidden patterns (lint check)
		if err := safetyGate.ValidateCustomerReply(sanitized); err != nil {
			p.Logger.Warn("⚠️ Customer reply contains forbidden patterns even after sanitization",
				zap.Error(err),
				zap.Int("message_index", i+1),
				zap.String("original_preview", func() string {
					if len(msgText) > 50 {
						return msgText[:50] + "..."
					}
					return msgText
				}()))
			// Continue anyway - sanitization already attempted to clean it
		}

		sanitizedMessages[i] = sanitized
	}

	// Create sanitized structured reply
	sanitizedReply := types.StructuredReply{
		ReplyMode:  reply.ReplyMode,
		Messages:   sanitizedMessages,
		Confidence: reply.Confidence,
		Escalation: reply.Escalation,
	}

	if p.ReplySequencer != nil {
		// Adapt sendReplyFunc signature to match ReplySequencer's expected signature
		sendFunc := func(ctx context.Context, replyText string) error {
			return p.sendReplyFunc(ctx, msg, replyText)
		}

		return p.ReplySequencer.SendReply(ctx, sendFunc, sanitizedReply)
	}

	// Fallback: send first message only
	if len(sanitizedReply.Messages) > 0 {
		return p.sendReplyFunc(ctx, msg, sanitizedReply.Messages[0])
	}

	return fmt.Errorf("no messages in structured reply")
}

// parseReplyToStructured converts a plain text reply to structured format
// Used as fallback when structured method is not available
func (p *ProductionMessageProcessor) parseReplyToStructured(reply string) types.StructuredReply {
	reply = strings.TrimSpace(reply)

	// Simple heuristic: if reply has paragraph breaks, split it
	parts := strings.Split(reply, "\n\n")

	// Filter empty parts
	var validParts []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			validParts = append(validParts, part)
		}
	}

	if len(validParts) <= 1 {
		return types.StructuredReply{
			ReplyMode:  "single",
			Messages:   []string{reply},
			Confidence: "high",
			Escalation: nil,
		}
	}

	// Limit to 3 messages max
	if len(validParts) > 3 {
		// Compress: combine first N-1, keep last
		firstMsg := strings.Join(validParts[:len(validParts)-1], " ")
		lastMsg := validParts[len(validParts)-1]
		validParts = []string{firstMsg, lastMsg}
	}

	return types.StructuredReply{
		ReplyMode:  "sequence",
		Messages:   validParts,
		Confidence: "high",
		Escalation: nil,
	}
}

// isRetryableDBError checks if the error is a transient database connection error
// that might succeed on retry (e.g., connection refused, connection reset, timeout)
func (p *ProductionMessageProcessor) isRetryableDBError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	retryPatterns := []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"dial tcp",
		"i/o timeout",
		"no connection could be made",
		"broken pipe",
		"network is unreachable",
		"connection pool exhausted",
		"too many connections",
		"server closed the connection unexpectedly",
		"could not connect to server",
	}
	for _, pattern := range retryPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}

// trackCustomer tracks or creates a customer (helper method) with retry logic
func (p *ProductionMessageProcessor) trackCustomer(ctx context.Context, msg IncomingMessage) (*storage.Customer, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 500ms, 1s, 2s
			backoff := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
			if backoff > 2*time.Second {
				backoff = 2 * time.Second
			}
			p.Logger.Info("🔄 Retrying trackCustomer after DB error",
				zap.Int("attempt", attempt+1),
				zap.Duration("backoff", backoff),
				zap.Error(lastErr))
			time.Sleep(backoff)
		}

		var customer storage.Customer

		// Try to find existing customer
		err := p.DB.Where("tenant_id = ? AND channel = ? AND platform_id = ?",
			msg.TenantID, msg.Platform, msg.CustomerID).First(&customer).Error

		if err == nil {
			return &customer, nil
		}

		if err == gorm.ErrRecordNotFound {
			// Create new customer
			customer = storage.Customer{
				ID:            uuid.New(),
				TenantID:      msg.TenantID,
				Channel:       msg.Platform,
				PlatformID:    msg.CustomerID,
				DisplayName:   msg.CustomerName,
				Username:      msg.CustomerUsername,
				ProfilePicURL: msg.CustomerProfilePic,
				LastMessageAt: msg.Timestamp,
			}

			if createErr := p.DB.Create(&customer).Error; createErr != nil {
				if p.isRetryableDBError(createErr) {
					lastErr = createErr
					continue
				}
				return nil, createErr
			}

			return &customer, nil
		}

		// Connection error - retry
		if p.isRetryableDBError(err) {
			lastErr = err
			continue
		}

		// Non-retryable error
		return nil, err
	}

	// All retries exhausted
	p.Logger.Error("❌ trackCustomer failed after all retries",
		zap.Int("max_retries", maxRetries),
		zap.Error(lastErr))
	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// createOrGetConversation creates or gets a conversation (helper method)
func (p *ProductionMessageProcessor) createOrGetConversation(ctx context.Context, msg IncomingMessage, customerID uuid.UUID) (*storage.Conversation, error) {
	const maxRetries = 3
	var lastErr error

	threadID := msg.ThreadID
	if threadID == "" {
		threadID = msg.CustomerID
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 500ms, 1s, 2s
			backoff := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
			if backoff > 2*time.Second {
				backoff = 2 * time.Second
			}
			p.Logger.Info("🔄 Retrying createOrGetConversation after DB error",
				zap.Int("attempt", attempt+1),
				zap.Duration("backoff", backoff),
				zap.Error(lastErr))
			time.Sleep(backoff)
		}

		var conversation storage.Conversation

		err := p.DB.Where("tenant_id = ? AND customer_id = ? AND platform = ?",
			msg.TenantID, customerID, msg.Platform).First(&conversation).Error

		if err == nil {
			// Update last message time
			conversation.LastMessageAt = msg.Timestamp
			conversation.Status = "active"
			if saveErr := p.DB.Save(&conversation).Error; saveErr != nil {
				if p.isRetryableDBError(saveErr) {
					lastErr = saveErr
					continue
				}
				// Non-retryable save error - return conversation anyway
				p.Logger.Warn("failed to update conversation timestamp", zap.Error(saveErr))
			}
			return &conversation, nil
		}

		if err == gorm.ErrRecordNotFound {
			// Create new conversation
			conversation = storage.Conversation{
				ID:               uuid.New(),
				TenantID:         msg.TenantID,
				CustomerID:       customerID,
				Platform:         msg.Platform,
				PlatformThreadID: threadID,
				Status:           "active",
				LastMessageAt:    msg.Timestamp,
			}

			if createErr := p.DB.Create(&conversation).Error; createErr != nil {
				if p.isRetryableDBError(createErr) {
					lastErr = createErr
					continue
				}
				return nil, createErr
			}

			return &conversation, nil
		}

		// Connection error - retry
		if p.isRetryableDBError(err) {
			lastErr = err
			continue
		}

		// Non-retryable error
		return nil, err
	}

	// All retries exhausted
	p.Logger.Error("❌ createOrGetConversation failed after all retries",
		zap.Int("max_retries", maxRetries),
		zap.Error(lastErr))
	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// metadataToJSON converts metadata map to JSON string
func (p *ProductionMessageProcessor) metadataToJSON(metadata map[string]interface{}) string {
	if len(metadata) == 0 {
		return "{}"
	}

	jsonBytes, err := json.Marshal(metadata)
	if err != nil {
		return "{}"
	}

	return string(jsonBytes)
}

// createNotificationForIncomingMessage stores a dashboard notification for a new incoming message
func (p *ProductionMessageProcessor) createNotificationForIncomingMessage(msg IncomingMessage, customer *storage.Customer, conversationID, interactionID uuid.UUID) {
	p.Logger.Debug("notification suppressed for incoming message",
		zap.Stringer("tenant_id", msg.TenantID),
		zap.Stringer("customer_id", customer.ID),
		zap.Stringer("conversation_id", conversationID),
		zap.Stringer("interaction_id", interactionID))
	// Note: Actual notification creation is handled by dashboard service
	// This is a placeholder for logging/audit purposes
}

// checkAndNotifyEscalationAftermath checks if customer responded after an escalated conversation
// Only escalates if intent confidence is very low OR AI generation failed
func (p *ProductionMessageProcessor) checkAndNotifyEscalationAftermath(ctx context.Context, msg IncomingMessage, customer *storage.Customer, interactionID uuid.UUID, conversationID uuid.UUID, intent *ai.DetectedIntent, aiReplyGenerated bool) {
	// Check if there's a recent escalated interaction in this conversation (within last 24 hours)
	cutoffTime := time.Now().Add(-24 * time.Hour)

	var recentEscalatedInteraction storage.Interaction
	query := p.DB.Where("tenant_id = ? AND customer_id = ? AND platform = ? AND response_type = ? AND response_status = ? AND created_at > ?",
		msg.TenantID, customer.ID, msg.Platform, "manual", "sent", cutoffTime).
		Order("created_at DESC").First(&recentEscalatedInteraction)

	if query.Error != nil {
		// No recent escalated interaction - this is a new conversation
		return
	}

	// Found a recent escalated reply - this is a follow-up!
	p.Logger.Info("📬 Customer follow-up after escalated conversation",
		zap.Stringer("tenant_id", msg.TenantID),
		zap.Stringer("original_interaction_id", recentEscalatedInteraction.ID),
		zap.Stringer("new_interaction_id", interactionID),
		zap.String("customer_message", msg.Content),
		zap.Bool("ai_reply_generated", aiReplyGenerated),
		zap.Float64("intent_confidence", func() float64 {
			if intent != nil {
				return intent.Confidence
			}
			return 0
		}()))

	// Check if message is benign (acknowledgments, thanks, greetings)
	isBenign := false
	lowerMsg := strings.ToLower(strings.TrimSpace(msg.Content))
	if len(lowerMsg) < 20 {
		if strings.Contains(lowerMsg, "ok") || strings.Contains(lowerMsg, "thanks") ||
			strings.Contains(lowerMsg, "thank you") || strings.Contains(lowerMsg, "cool") ||
			strings.Contains(lowerMsg, "k") || strings.Contains(lowerMsg, "👍") {
			isBenign = true
		}
	}

	// Also check if it's just a greeting
	if lowerMsg == "hi" || lowerMsg == "hello" || lowerMsg == "hey" {
		isBenign = true
	}

	// Only escalate if:
	// 1. Intent confidence is very low (< 0.3) AND message isn't benign, OR
	// 2. AI generation failed (aiReplyGenerated == false)
	shouldEscalate := false
	var suggestedReply string
	var escalationReason string

	if !aiReplyGenerated {
		// AI generation failed - escalate
		shouldEscalate = true
		escalationReason = "customer_follow_up_after_escalation_ai_failed"
		// Generate a fallback suggested reply
		suggestedReply = "Thanks for your message! Let me help you with that."
		p.Logger.Info("Escalating follow-up: AI generation failed", zap.String("customer_message", msg.Content))
	} else if intent != nil && intent.Confidence < 0.3 && !isBenign {
		// Intent confidence is very low and message isn't benign - escalate
		shouldEscalate = true
		escalationReason = "customer_follow_up_after_escalation_low_confidence"
		// Try to generate a suggested reply using the detected intent
		if p.Responder != nil {
			// Generate a simple suggested reply
			suggestedReply = fmt.Sprintf("I understand you're asking about: %s. Let me help you with that.", msg.Content)
		}
		p.Logger.Info("Escalating follow-up: Very low intent confidence",
			zap.String("customer_message", msg.Content),
			zap.Float64("confidence", intent.Confidence))
	} else {
		// AI handled it successfully or message is benign - no escalation needed
		p.Logger.Info("Skipping escalation: AI handled follow-up successfully or message is benign",
			zap.String("content", msg.Content),
			zap.Bool("is_benign", isBenign),
			zap.Float64("intent_confidence", func() float64 {
				if intent != nil {
					return intent.Confidence
				}
				return 0
			}()))
	}

	if shouldEscalate && p.EscalationFunc != nil {
		intentStr := ""
		toneStr := ""
		confidence := 0.0
		if intent != nil {
			intentStr = intent.Intent
			toneStr = intent.Tone
			confidence = intent.Confidence
		}
		_ = p.EscalationFunc(ctx, msg.TenantID, interactionID, customer.DisplayName, msg.Platform, msg.Content, suggestedReply, intentStr, toneStr, confidence, escalationReason)
	}
}

// handleAIError handles AI generation errors with appropriate fallbacks
func (p *ProductionMessageProcessor) handleAIError(
	ctx context.Context,
	primaryMsg IncomingMessage,
	customer *storage.Customer,
	interaction storage.Interaction,
	intent *ai.DetectedIntent,
	combinedText string,
	err error,
) error {
	p.Logger.Error("failed to generate AI reply", zap.Error(err))

	// Check for circuit breaker or timeout errors
	errMsg := err.Error()
	if strings.Contains(strings.ToLower(errMsg), "circuit breaker") && strings.Contains(strings.ToLower(errMsg), "open") {
		fallbackReply := types.StructuredReply{
			ReplyMode:  "single",
			Messages:   []string{"Thanks for your message! I'm experiencing a temporary delay. Please give me a moment and try again."},
			Confidence: "low",
			Escalation: nil,
		}
		return p.sendStructuredReply(ctx, primaryMsg, fallbackReply)
	}

	if strings.Contains(errMsg, "context deadline exceeded") || strings.Contains(errMsg, "timeout") {
		fallbackReply := types.StructuredReply{
			ReplyMode:  "single",
			Messages:   []string{buildTimeoutFallbackMessage(combinedText, intent)},
			Confidence: "low",
			Escalation: nil,
		}
		return p.sendStructuredReply(ctx, primaryMsg, fallbackReply)
	}

	// Escalate on other errors
	if p.EscalationFunc != nil {
		// Generate a fallback suggested reply based on intent
		fallbackReply := "Thanks for your message! Let me look into this for you."
		if intent != nil && intent.Intent != "" {
			switch intent.Intent {
			case "payment", "refund":
				fallbackReply = "Thanks for confirming! Let me verify this payment for you."
			case "question", "inquiry":
				fallbackReply = "Great question! Let me get back to you with the details."
			case "complaint", "issue":
				fallbackReply = "I understand your concern. Let me help resolve this for you."
			case "order", "purchase":
				fallbackReply = "Thanks for your interest! Let me help you with your order."
			}
		}
		_ = p.EscalationFunc(ctx, primaryMsg.TenantID, interaction.ID, customer.DisplayName, primaryMsg.Platform, combinedText, fallbackReply, intent.Intent, intent.Tone, intent.Confidence, "ai_generation_failed")
	}

	// Check escalation aftermath (AI generation failed, so aiReplyGenerated=false)
	if interaction.ID != uuid.Nil {
		var conv storage.Conversation
		if err := p.DB.First(&conv, "id = ?", interaction.ConversationID).Error; err == nil {
			p.checkAndNotifyEscalationAftermath(ctx, primaryMsg, customer, interaction.ID, conv.ID, intent, false)
		}
	}

	return err
}

// handleEscalationOldWay handles escalation using the old proven logic
// This matches the behavior of MessageHandler.escalateToTenant:
// - Generates AI reply as "suggested reply" for tenant
// - Notifies tenant via EscalationFunc (Telegram)
// - Does NOT send any message to customer (tenant handles it)
// - Updates interaction status to "escalated"
func (p *ProductionMessageProcessor) handleEscalationOldWay(
	ctx context.Context,
	msg IncomingMessage,
	customer *storage.Customer,
	intent *ai.DetectedIntent,
	interaction storage.Interaction,
	reason string,
	events []ai.Event,
) error {
	p.Logger.Info("🚨 Escalation triggered (standard logic)",
		zap.String("reason", reason),
		zap.Stringer("tenant_id", msg.TenantID),
		zap.Stringer("interaction_id", interaction.ID))

	// Generate suggested reply using AI (same as old escalateToTenant)
	// This gives the tenant an AI-generated suggestion, but we don't send it to customer
	var conversationID uuid.UUID
	if interaction.ConversationID != uuid.Nil {
		conversationID = interaction.ConversationID
	}

	suggestedReply, err := p.Responder.GenerateCustomerReply(
		ctx,
		msg.TenantID,
		customer.ID,
		msg.Content,
		intent.Intent,
		msg.Platform,
		conversationID,
	)
	if err != nil {
		p.Logger.Warn("failed to generate suggested reply for escalation", zap.Error(err))
		// Generate a fallback suggested reply based on intent
		switch intent.Intent {
		case "payment", "refund":
			suggestedReply = "Thanks for confirming! Let me verify this payment for you."
		case "question", "inquiry":
			suggestedReply = "Great question! Let me get back to you with the details."
		case "complaint", "issue":
			suggestedReply = "I understand your concern. Let me help resolve this for you."
		case "order", "purchase":
			suggestedReply = "Thanks for your interest! Let me help you with your order."
		default:
			suggestedReply = "Thanks for your message! Let me look into this for you."
		}
	}

	// Payment confirmations should never use catalog-context suggestions.
	// Keep this deterministic and aligned with the message content.
	if events == nil {
		events = ai.ExtractEvents(msg.Content)
	}
	isPaymentConfirmation := ai.HasEvent(events, ai.EventPaymentConfirmation) || p.isPaymentConfirmation(msg.Content)
	if isPaymentConfirmation {
		hasMedia := strings.TrimSpace(strings.ToLower(msg.MessageType)) != "" && strings.ToLower(strings.TrimSpace(msg.MessageType)) != "text"
		lower := strings.ToLower(msg.Content)
		if hasMedia || strings.Contains(lower, "receipt") || strings.Contains(lower, "proof") || strings.Contains(lower, "screenshot") || strings.Contains(lower, "evidence") {
			suggestedReply = "Thanks — I’ve received your payment proof. I’m confirming it now and will update you shortly."
		} else {
			suggestedReply = "Thanks — please share your payment proof/receipt (screenshot) so I can confirm and update you."
		}
	}

	// Send escalation notification to tenant via EscalationFunc (Telegram)
	if p.EscalationFunc != nil {
		if err := p.EscalationFunc(
			ctx,
			msg.TenantID,
			interaction.ID,
			customer.DisplayName,
			msg.Platform,
			msg.Content,
			suggestedReply,
			intent.Intent,
			intent.Tone,
			intent.Confidence,
			reason,
		); err != nil {
			p.Logger.Error("failed to escalate to tenant", zap.Error(err))
			return err
		}
	}

	// Update interaction status (same as old escalateToTenant)
	interaction.ResponseType = "escalated"
	interaction.ResponseStatus = "pending" // Not "sent" - tenant needs to respond
	interaction.SuggestedReply = suggestedReply
	interaction.EscalationReason = reason
	now := time.Now()
	interaction.EscalatedAt = &now
	if err := p.DB.Save(&interaction).Error; err != nil {
		p.Logger.Warn("failed to update interaction status", zap.Error(err))
	}

	// Handle payment confirmation events (same as old logic)
	if isPaymentConfirmation {
		// Create pending payment record
		p.createPendingPayment(ctx, msg.TenantID, customer.ID, &interaction, msg.Content)
	}

	p.Logger.Info("📤 Escalated to tenant (old logic)",
		zap.Stringer("tenant_id", msg.TenantID),
		zap.Stringer("interaction_id", interaction.ID),
		zap.String("intent", intent.Intent),
		zap.String("reason", reason))

	// IMPORTANT: Do NOT send any message to customer
	// The old logic doesn't send anything - tenant handles the response
	return nil
}

// isPaymentConfirmation checks if message confirms payment (helper for escalation)
func (p *ProductionMessageProcessor) isPaymentConfirmation(content string) bool {
	contentLower := strings.ToLower(content)
	confirmationPhrases := []string{
		"i have paid", "i paid", "payment sent", "i sent", "i transferred",
		"payment done", "paid already", "already paid", "payment made",
		"money sent", "funds transferred", "payment completed",
	}
	for _, phrase := range confirmationPhrases {
		if strings.Contains(contentLower, phrase) {
			return true
		}
	}
	return false
}

// createPendingPayment creates a pending payment record (helper for escalation)
func (p *ProductionMessageProcessor) createPendingPayment(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, interaction *storage.Interaction, content string) {
	// This is a simplified version - the full implementation is in MessageHandler
	// For now, just log it
	p.Logger.Info("💰 Payment confirmation detected, would create pending payment record",
		zap.Stringer("tenant_id", tenantID),
		zap.Stringer("customer_id", customerID),
		zap.Stringer("interaction_id", interaction.ID))
	// TODO: Implement full pending payment creation if needed
}
