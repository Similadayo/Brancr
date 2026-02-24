package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ConversationWorkflow manages customer-tenant conversations across platforms
type ConversationWorkflow struct {
	DB                *gorm.DB
	Logger            *zap.Logger
	BotManager        interface{} // TenantBotManager interface
	AIClient          *ai.AIClient
	Memory            *ai.MemoryService
	EscalationHandler interface{}   // EscalationHandler interface
	MessageSender     MessageSender // Interface for sending messages to customers
}

// ConversationContext holds the full context of an active conversation
type ConversationContext struct {
	ConversationID uuid.UUID
	TenantID       uuid.UUID
	CustomerID     uuid.UUID
	Platform       string
	ThreadID       string
	Status         ConversationStatus
	LastMessageAt  time.Time
	UnreadCount    int
	Priority       ConversationPriority
	AssignedTo     int64 // Telegram ID of assigned agent
	Tags           []string
	Metadata       map[string]interface{}
}

type ConversationStatus string

const (
	ConversationStatusActive    ConversationStatus = "active"
	ConversationStatusWaiting   ConversationStatus = "waiting" // Waiting for customer response
	ConversationStatusResolved  ConversationStatus = "resolved"
	ConversationStatusEscalated ConversationStatus = "escalated"
	ConversationStatusArchived  ConversationStatus = "archived"
)

type ConversationPriority string

const (
	PriorityLow    ConversationPriority = "low"
	PriorityNormal ConversationPriority = "normal"
	PriorityHigh   ConversationPriority = "high"
	PriorityUrgent ConversationPriority = "urgent"
)

// HandleCustomerMessage processes incoming customer messages
func (w *ConversationWorkflow) HandleCustomerMessage(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, platform string, message string, messageID string, threadID string) error {
	// Step 1: Load or create conversation
	conversation, err := w.getOrCreateConversation(ctx, tenantID, customerID, platform, threadID)
	if err != nil {
		return fmt.Errorf("failed to get/create conversation: %w", err)
	}

	// Step 2: Load customer
	var customer storage.Customer
	if err := w.DB.First(&customer, customerID).Error; err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}

	// Step 3: Create interaction record
	interaction := storage.Interaction{
		TenantID:          tenantID,
		CustomerID:        customerID,
		ConversationID:    conversation.ID,
		Platform:          platform,
		Direction:         "incoming",
		MessageType:       "text",
		Content:           message,
		PlatformMessageID: messageID,
		ResponseType:      "pending",
		ResponseStatus:    "pending",
		CreatedAt:         time.Now(),
	}

	// Step 4: AI Analysis - Detect intent, emotion, and decide action
	intent, emotion, decision, err := w.analyzeMessage(ctx, tenantID, customerID, message, platform)
	if err != nil {
		w.Logger.Warn("AI analysis failed, using fallback", zap.Error(err))
		// Fallback to default analysis
		intent = &ai.IntentResult{Intent: "unknown", Confidence: 0.5}
		emotion = &ai.EmotionResult{Label: "neutral", Confidence: 0.5}
		decision = &ai.PolicyDecision{Action: "escalate", ShouldHuman: true}
	}

	// Update interaction with analysis
	interaction.DetectedIntent = intent.Intent
	interaction.DetectedTone = emotion.Label
	interaction.Confidence = intent.Confidence

	// Step 4.5: Deterministic events + persisted conversation state.
	events := ai.ExtractEvents(message)
	metadataMap := map[string]interface{}{}
	ai.AttachEventsToMetadata(metadataMap, events)
	if b, mErr := json.Marshal(metadataMap); mErr == nil {
		interaction.Metadata = string(b)
	}
	if conversation.ID != uuid.Nil {
		scManager := &ai.SalesConversationManager{DB: w.DB, Logger: w.Logger}
		if _, scErr := scManager.ApplyDeterministicSignals(ctx, tenantID, customerID, conversation.ID, intent.Intent, events); scErr != nil {
			w.Logger.Warn("failed to apply deterministic sales conversation signals", zap.Error(scErr), zap.Stringer("tenant_id", tenantID), zap.Stringer("conversation_id", conversation.ID))
		}
	}

	// If events are sufficient to decide escalation, prefer them over probabilistic policy.
	if d := ai.DecideEscalationFromEvents(events); d.Handled {
		if d.ShouldEscalate {
			decision.Action = "escalate"
			decision.ShouldHuman = true
		} else {
			decision.Action = "auto_reply"
			decision.ShouldHuman = false
		}
	}

	// Step 5: Determine response action based on policy decision
	switch decision.Action {
	case "auto_reply":
		return w.handleAutoReply(ctx, tenantID, customerID, conversation, interaction, message, intent, emotion)
	case "escalate":
		return w.handleEscalation(ctx, tenantID, customerID, conversation, interaction, message, intent, emotion)
	case "ask_clarify":
		return w.handleClarification(ctx, tenantID, customerID, conversation, interaction, message)
	default:
		// Default to escalation for unknown actions
		return w.handleEscalation(ctx, tenantID, customerID, conversation, interaction, message, intent, emotion)
	}
}

// analyzeMessage performs AI analysis on incoming message
func (w *ConversationWorkflow) analyzeMessage(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, message string, platform string) (*ai.IntentResult, *ai.EmotionResult, *ai.PolicyDecision, error) {
	// Create classifier for intent detection
	classifier := ai.NewClassifier()
	intent, err := classifier.Classify(ctx, tenantID, message)
	if err != nil {
		w.Logger.Warn("intent classification failed", zap.Error(err))
		intent = &ai.IntentResult{Intent: "unknown", Confidence: 0.3}
	}

	// Analyze emotion
	emotionAnalyzer := ai.NewEmotionAnalyzer()
	emotion, err := emotionAnalyzer.Analyze(ctx, tenantID, message)
	if err != nil {
		w.Logger.Warn("emotion analysis failed", zap.Error(err))
		emotion = &ai.EmotionResult{Label: "neutral", Confidence: 0.5}
	}

	// Evaluate policy decision
	policyEngine := ai.NewPolicyEngine()
	decision, err := policyEngine.Evaluate(ctx, tenantID, intent, emotion)
	if err != nil {
		w.Logger.Warn("policy evaluation failed", zap.Error(err))
		decision = &ai.PolicyDecision{Action: "escalate", ShouldHuman: true, Confidence: 0.5}
	}

	return intent, emotion, decision, nil
}

// handleAutoReply generates and sends an automatic reply
func (w *ConversationWorkflow) handleAutoReply(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, conversation *storage.Conversation, interaction storage.Interaction, message string, intent *ai.IntentResult, emotion *ai.EmotionResult) error {
	// Get persona for responder
	var persona storage.Persona
	if err := w.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona = storage.Persona{BotName: "Assistant", Tone: "friendly"}
	}

	// Create responder
	responder := ai.NewAIResponder(w.DB, persona)

	// Generate AI reply
	reply, err := responder.GenerateTenantAssistantReplyForCustomer(ctx, tenantID, customerID, message)
	if err != nil {
		w.Logger.Error("failed to generate reply", zap.Error(err))
		// Fallback to escalation if AI fails
		return w.handleEscalation(ctx, tenantID, customerID, conversation, interaction, message, intent, emotion)
	}

	// Save suggested reply to interaction
	interaction.SuggestedReply = reply
	interaction.ResponseType = "auto_reply"
	interaction.ResponseStatus = "approved" // Auto-approved for auto-replies

	// Save interaction first
	if err := w.DB.Create(&interaction).Error; err != nil {
		return fmt.Errorf("failed to save interaction: %w", err)
	}

	// Send reply to customer (this would call the social media API)
	// For now, we'll just save it
	interaction.FinalReply = reply
	interaction.ResponseStatus = "sent"
	if err := w.DB.Save(&interaction).Error; err != nil {
		w.Logger.Warn("failed to update interaction status", zap.Error(err))
	}

	// Update conversation
	conversation.LastMessageAt = time.Now()
	conversation.Status = string(ConversationStatusWaiting) // Waiting for customer response
	if err := w.DB.Save(&conversation).Error; err != nil {
		w.Logger.Warn("failed to update conversation", zap.Error(err))
	}

	// Save to memory for future context
	if w.Memory != nil {
		w.Memory.SaveCustomerConversation(ctx, tenantID, customerID, message, reply)
	}

	w.Logger.Info("auto-reply sent",
		zap.Stringer("tenant_id", tenantID),
		zap.Stringer("customer_id", customerID),
		zap.String("platform", conversation.Platform),
	)

	return nil
}

var paymentMadePhrases = []string{
	"payment made",
	"payment done",
	"paid already",
	"i have paid",
	"i've paid",
	"i paid",
	"i have made payment",
	"i've made payment",
	"transfer done",
	"transfer made",
	"i have transferred",
	"i've transferred",
	"i transferred",
	"sent the money",
	"sent payment",
	"sent the payment",
}

func customerClaimsPayment(message string) bool {
	m := strings.ToLower(strings.TrimSpace(message))
	if m == "" {
		return false
	}
	for _, p := range paymentMadePhrases {
		if strings.Contains(m, p) {
			return true
		}
	}
	return false
}

// handleEscalation escalates the message to tenant
func (w *ConversationWorkflow) handleEscalation(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, conversation *storage.Conversation, interaction storage.Interaction, message string, intent *ai.IntentResult, emotion *ai.EmotionResult) error {
	// Get persona for responder
	var persona storage.Persona
	if err := w.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona = storage.Persona{BotName: "Assistant", Tone: "friendly"}
	}

	// Create responder
	responder := ai.NewAIResponder(w.DB, persona)

	// Generate suggested reply for escalation
	var suggestedReply string
	if customerClaimsPayment(message) {
		suggestedReply = "Thanks for the update! We're currently verifying your payment and will confirm shortly."
	} else {
		var err error
		suggestedReply, err = responder.GenerateTenantAssistantReplyForCustomer(ctx, tenantID, customerID, message)
		if err != nil {
			w.Logger.Warn("failed to generate suggested reply, generating contextual fallback", zap.Error(err))
			// Use contextual fallback generator if available
			if responder.Client != nil {
				fallbackGen := ai.NewContextualFallbackGenerator(
					responder.Client,
					w.DB,
					w.Logger,
					responder.Memory,
				)
				fallbackReply, fallbackErr := fallbackGen.GenerateEscalationAcknowledgment(ctx, tenantID, customerID, message)
				if fallbackErr == nil && fallbackReply != "" {
					suggestedReply = fallbackReply
				} else {
					suggestedReply = "Got it! Someone from our team will get back to you shortly."
				}
			} else {
				suggestedReply = "Got it! Someone from our team will get back to you shortly."
			}
		}
	}

	// Save interaction with escalation status
	interaction.SuggestedReply = suggestedReply
	interaction.ResponseType = "escalated"
	interaction.ResponseStatus = "pending"
	if err := w.DB.Create(&interaction).Error; err != nil {
		return fmt.Errorf("failed to save interaction: %w", err)
	}

	// Update conversation status
	conversation.Status = string(ConversationStatusEscalated)
	conversation.LastMessageAt = time.Now()
	if err := w.DB.Save(&conversation).Error; err != nil {
		w.Logger.Warn("failed to update conversation status", zap.Error(err))
	}

	// Load customer for escalation message
	var customer storage.Customer
	if err := w.DB.First(&customer, customerID).Error; err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}

	customerName := customer.DisplayName
	if customerName == "" {
		customerName = customer.Username
	}
	if customerName == "" {
		shortID := customer.PlatformID
		if len(shortID) > 10 {
			shortID = shortID[:10]
		}
		customerName = "Customer (" + shortID + ")"
	}

	// Send escalation to tenant via Telegram bot
	// This would call the EscalationHandler.SendEscalation method
	// For now, we'll log it
	w.Logger.Info("escalation triggered",
		zap.Stringer("tenant_id", tenantID),
		zap.Stringer("customer_id", customerID),
		zap.Stringer("interaction_id", interaction.ID),
		zap.String("intent", intent.Intent),
		zap.String("platform", conversation.Platform),
	)

	// TODO: Actually send escalation notification to tenant's Telegram bot
	// This would be: escalationHandler.SendEscalation(ctx, tenantID, interaction.ID, ...)

	return nil
}

// handleClarification asks customer for clarification
func (w *ConversationWorkflow) handleClarification(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, conversation *storage.Conversation, interaction storage.Interaction, message string) error {
	// Get persona for responder
	var persona storage.Persona
	if err := w.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona = storage.Persona{BotName: "Assistant", Tone: "friendly"}
	}

	// Create responder
	responder := ai.NewAIResponder(w.DB, persona)

	// Generate clarification question
	clarification, err := responder.GenerateTenantAssistantReplyForCustomer(ctx, tenantID, customerID, message)
	if err != nil {
		clarification = "Could you provide a bit more detail so I can help you better?"
	}

	// Save interaction
	interaction.SuggestedReply = clarification
	interaction.ResponseType = "auto_reply"
	interaction.ResponseStatus = "approved"
	if err := w.DB.Create(&interaction).Error; err != nil {
		return fmt.Errorf("failed to save interaction: %w", err)
	}

	// Update interaction after sending
	interaction.FinalReply = clarification
	interaction.ResponseStatus = "sent"
	if err := w.DB.Save(&interaction).Error; err != nil {
		w.Logger.Warn("failed to update interaction", zap.Error(err))
	}

	// Update conversation
	conversation.LastMessageAt = time.Now()
	conversation.Status = string(ConversationStatusWaiting)
	if err := w.DB.Save(&conversation).Error; err != nil {
		w.Logger.Warn("failed to update conversation", zap.Error(err))
	}

	return nil
}

// HandleTenantReply processes tenant's reply to a customer message
func (w *ConversationWorkflow) HandleTenantReply(ctx context.Context, tenantID uuid.UUID, conversationID uuid.UUID, reply string, interactionID uuid.UUID) error {
	// Load conversation
	var conversation storage.Conversation
	if err := w.DB.First(&conversation, conversationID).Error; err != nil {
		return fmt.Errorf("conversation not found: %w", err)
	}

	if conversation.TenantID != tenantID {
		return fmt.Errorf("unauthorized: conversation belongs to different tenant")
	}

	// Load interaction if provided
	var interaction storage.Interaction
	if interactionID != uuid.Nil {
		if err := w.DB.First(&interaction, interactionID).Error; err != nil {
			return fmt.Errorf("interaction not found: %w", err)
		}
	} else {
		// Find the most recent incoming interaction for this conversation
		if err := w.DB.Where("conversation_id = ? AND direction = ? AND response_status = ?", conversationID, "incoming", "pending").
			Order("created_at DESC").
			First(&interaction).Error; err != nil {
			return fmt.Errorf("no pending interaction found: %w", err)
		}
	}

	// Create outgoing interaction
	outgoingInteraction := storage.Interaction{
		TenantID:       tenantID,
		CustomerID:     interaction.CustomerID,
		ConversationID: conversationID,
		Platform:       conversation.Platform,
		Direction:      "outgoing",
		MessageType:    "text",
		Content:        reply,
		ResponseType:   "manual",
		ResponseStatus: "sent",
		FinalReply:     reply,
		CreatedAt:      time.Now(),
	}

	if err := w.DB.Create(&outgoingInteraction).Error; err != nil {
		return fmt.Errorf("failed to create outgoing interaction: %w", err)
	}

	// Update original interaction
	interaction.FinalReply = reply
	interaction.ResponseType = "manual"
	interaction.ResponseStatus = "sent"
	interaction.EscalatedAt = &time.Time{}
	*interaction.EscalatedAt = time.Now()
	if err := w.DB.Save(&interaction).Error; err != nil {
		w.Logger.Warn("failed to update interaction", zap.Error(err))
	}

	// Feedback loop: record tenant edit when they modify the AI suggested reply.
	_ = ai.RecordTenantEditFeedback(ctx, w.DB, interaction.ID, reply)

	// Update conversation
	conversation.Status = string(ConversationStatusWaiting)
	conversation.LastMessageAt = time.Now()
	if err := w.DB.Save(&conversation).Error; err != nil {
		w.Logger.Warn("failed to update conversation", zap.Error(err))
	}

	// Save to memory
	if w.Memory != nil {
		w.Memory.SaveCustomerConversation(ctx, tenantID, interaction.CustomerID, interaction.Content, reply)
	}

	// CRITICAL: Sanitize customer reply to remove internal language
	// Even tenant-manual replies may accidentally contain internal phrases if copy-pasted
	safetyGate := ai.NewCustomerOutputSafetyGate()
	reply = safetyGate.SanitizeCustomerReply(reply)

	// Actually send reply to customer via social media API
	if w.MessageSender != nil {
		// Load customer for platform ID
		var customer storage.Customer
		if err := w.DB.First(&customer, interaction.CustomerID).Error; err != nil {
			w.Logger.Error("failed to load customer for sending reply", zap.Error(err))
			return fmt.Errorf("customer not found: %w", err)
		}

		threadID := conversation.PlatformThreadID
		if threadID == "" {
			threadID = customer.PlatformID
		}

		switch conversation.Platform {
		case "whatsapp":
			if err := w.MessageSender.SendWhatsAppReply(ctx, tenantID, customer.PlatformID, reply); err != nil {
				w.Logger.Error("failed to send reply to customer",
					zap.Error(err),
					zap.Stringer("conversation_id", conversationID),
					zap.String("customer_platform_id", customer.PlatformID))
				return fmt.Errorf("failed to send reply: %w", err)
			}
		case "instagram", "facebook":
			if err := w.MessageSender.SendReply(ctx, tenantID, conversation.Platform, threadID, customer.PlatformID, reply); err != nil {
				w.Logger.Error("failed to send reply to customer",
					zap.Error(err),
					zap.Stringer("conversation_id", conversationID),
					zap.String("customer_platform_id", customer.PlatformID),
					zap.String("thread_id", threadID))
				return fmt.Errorf("failed to send reply: %w", err)
			}
		default:
			w.Logger.Warn("reply sending not implemented for platform",
				zap.String("platform", conversation.Platform),
				zap.Stringer("conversation_id", conversationID))
		}
	}

	w.Logger.Info("tenant reply sent",
		zap.Stringer("tenant_id", tenantID),
		zap.Stringer("conversation_id", conversationID),
		zap.Stringer("customer_id", interaction.CustomerID),
	)

	return nil
}

// getOrCreateConversation gets existing conversation or creates new one
func (w *ConversationWorkflow) getOrCreateConversation(ctx context.Context, tenantID uuid.UUID, customerID uuid.UUID, platform string, threadID string) (*storage.Conversation, error) {
	// Try to find existing conversation
	var conversation storage.Conversation
	err := w.DB.Where("tenant_id = ? AND customer_id = ? AND platform = ? AND platform_thread_id = ?", tenantID, customerID, platform, threadID).
		First(&conversation).Error

	if err == nil {
		// Conversation exists, update last message time
		conversation.LastMessageAt = time.Now()
		if conversation.Status == string(ConversationStatusResolved) || conversation.Status == string(ConversationStatusArchived) {
			conversation.Status = string(ConversationStatusActive) // Reactivate resolved conversations
		}
		if err := w.DB.Save(&conversation).Error; err != nil {
			w.Logger.Warn("failed to update conversation", zap.Error(err))
		}
		return &conversation, nil
	}

	// Create new conversation
	conversation = storage.Conversation{
		TenantID:         tenantID,
		CustomerID:       customerID,
		Platform:         platform,
		PlatformThreadID: threadID,
		Status:           string(ConversationStatusActive),
		LastMessageAt:    time.Now(),
	}

	if err := w.DB.Create(&conversation).Error; err != nil {
		return nil, fmt.Errorf("failed to create conversation: %w", err)
	}

	return &conversation, nil
}

// GetConversations retrieves all conversations for a tenant with filtering
func (w *ConversationWorkflow) GetConversations(ctx context.Context, tenantID uuid.UUID, status string, platform string, limit int, offset int) ([]storage.Conversation, error) {
	query := w.DB.Where("tenant_id = ?", tenantID)

	if status != "" {
		query = query.Where("status = ?", status)
	}

	if platform != "" {
		query = query.Where("platform = ?", platform)
	}

	var conversations []storage.Conversation
	if err := query.Order("last_message_at DESC").Limit(limit).Offset(offset).Find(&conversations).Error; err != nil {
		return nil, fmt.Errorf("failed to load conversations: %w", err)
	}

	return conversations, nil
}

// GetConversationWithMessages loads a conversation with all its interactions
func (w *ConversationWorkflow) GetConversationWithMessages(ctx context.Context, tenantID uuid.UUID, conversationID uuid.UUID) (*ConversationContext, []storage.Interaction, error) {
	// Load conversation
	var conversation storage.Conversation
	if err := w.DB.First(&conversation, conversationID).Error; err != nil {
		return nil, nil, fmt.Errorf("conversation not found: %w", err)
	}

	if conversation.TenantID != tenantID {
		return nil, nil, fmt.Errorf("unauthorized: conversation belongs to different tenant")
	}

	// Load all interactions
	var interactions []storage.Interaction
	if err := w.DB.Where("conversation_id = ?", conversationID).
		Order("created_at ASC").
		Find(&interactions).Error; err != nil {
		return nil, nil, fmt.Errorf("failed to load interactions: %w", err)
	}

	// Build context
	context := &ConversationContext{
		ConversationID: conversation.ID,
		TenantID:       conversation.TenantID,
		CustomerID:     conversation.CustomerID,
		Platform:       conversation.Platform,
		ThreadID:       conversation.PlatformThreadID,
		Status:         ConversationStatus(conversation.Status),
		LastMessageAt:  conversation.LastMessageAt,
	}

	// Count unread messages (incoming messages without tenant response)
	var unreadCount int64
	w.DB.Model(&storage.Interaction{}).
		Where("conversation_id = ? AND direction = ? AND response_status = ?", conversationID, "incoming", "pending").
		Count(&unreadCount)
	context.UnreadCount = int(unreadCount)

	return context, interactions, nil
}

// MarkConversationResolved marks a conversation as resolved
func (w *ConversationWorkflow) MarkConversationResolved(ctx context.Context, tenantID uuid.UUID, conversationID uuid.UUID) error {
	var conversation storage.Conversation
	if err := w.DB.First(&conversation, conversationID).Error; err != nil {
		return fmt.Errorf("conversation not found: %w", err)
	}

	if conversation.TenantID != tenantID {
		return fmt.Errorf("unauthorized")
	}

	conversation.Status = string(ConversationStatusResolved)
	return w.DB.Save(&conversation).Error
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
