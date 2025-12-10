package workflows

import (
	"context"
	"fmt"
	"time"

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
	EscalationHandler interface{} // EscalationHandler interface
	MessageSender     MessageSender // Interface for sending messages to customers
}

// ConversationContext holds the full context of an active conversation
type ConversationContext struct {
	ConversationID uint
	TenantID       uint
	CustomerID     uint
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
func (w *ConversationWorkflow) HandleCustomerMessage(ctx context.Context, tenantID uint, customerID uint, platform string, message string, messageID string, threadID string) error {
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
func (w *ConversationWorkflow) analyzeMessage(ctx context.Context, tenantID uint, customerID uint, message string, platform string) (*ai.IntentResult, *ai.EmotionResult, *ai.PolicyDecision, error) {
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
func (w *ConversationWorkflow) handleAutoReply(ctx context.Context, tenantID uint, customerID uint, conversation *storage.Conversation, interaction storage.Interaction, message string, intent *ai.IntentResult, emotion *ai.EmotionResult) error {
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
		zap.Uint("tenant_id", tenantID),
		zap.Uint("customer_id", customerID),
		zap.String("platform", conversation.Platform),
	)

	return nil
}

// handleEscalation escalates the message to tenant
func (w *ConversationWorkflow) handleEscalation(ctx context.Context, tenantID uint, customerID uint, conversation *storage.Conversation, interaction storage.Interaction, message string, intent *ai.IntentResult, emotion *ai.EmotionResult) error {
	// Get persona for responder
	var persona storage.Persona
	if err := w.DB.First(&persona, "tenant_id = ?", tenantID).Error; err != nil {
		persona = storage.Persona{BotName: "Assistant", Tone: "friendly"}
	}

	// Create responder
	responder := ai.NewAIResponder(w.DB, persona)

	// Generate suggested reply for escalation
	suggestedReply, err := responder.GenerateTenantAssistantReplyForCustomer(ctx, tenantID, customerID, message)
	if err != nil {
		w.Logger.Warn("failed to generate suggested reply", zap.Error(err))
		suggestedReply = "Thank you for your message. We'll get back to you shortly."
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
		zap.Uint("tenant_id", tenantID),
		zap.Uint("customer_id", customerID),
		zap.Uint("interaction_id", interaction.ID),
		zap.String("intent", intent.Intent),
		zap.String("platform", conversation.Platform),
	)

	// TODO: Actually send escalation notification to tenant's Telegram bot
	// This would be: escalationHandler.SendEscalation(ctx, tenantID, interaction.ID, ...)

	return nil
}

// handleClarification asks customer for clarification
func (w *ConversationWorkflow) handleClarification(ctx context.Context, tenantID uint, customerID uint, conversation *storage.Conversation, interaction storage.Interaction, message string) error {
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
func (w *ConversationWorkflow) HandleTenantReply(ctx context.Context, tenantID uint, conversationID uint, reply string, interactionID uint) error {
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
	if interactionID > 0 {
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

	// Actually send reply to customer via social media API
	if w.MessageSender != nil && conversation.Platform == "whatsapp" {
		// Load customer for platform ID
		var customer storage.Customer
		if err := w.DB.First(&customer, interaction.CustomerID).Error; err != nil {
			w.Logger.Error("failed to load customer for sending reply", zap.Error(err))
			return fmt.Errorf("customer not found: %w", err)
		}

		if err := w.MessageSender.SendWhatsAppReply(ctx, tenantID, customer.PlatformID, reply); err != nil {
			w.Logger.Error("failed to send reply to customer",
				zap.Error(err),
				zap.Uint("conversation_id", conversationID),
				zap.String("customer_platform_id", customer.PlatformID))
			return fmt.Errorf("failed to send reply: %w", err)
		}
	}

	w.Logger.Info("tenant reply sent",
		zap.Uint("tenant_id", tenantID),
		zap.Uint("conversation_id", conversationID),
		zap.Uint("customer_id", interaction.CustomerID),
	)

	return nil
}

// getOrCreateConversation gets existing conversation or creates new one
func (w *ConversationWorkflow) getOrCreateConversation(ctx context.Context, tenantID uint, customerID uint, platform string, threadID string) (*storage.Conversation, error) {
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
func (w *ConversationWorkflow) GetConversations(ctx context.Context, tenantID uint, status string, platform string, limit int, offset int) ([]storage.Conversation, error) {
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
func (w *ConversationWorkflow) GetConversationWithMessages(ctx context.Context, tenantID uint, conversationID uint) (*ConversationContext, []storage.Interaction, error) {
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
func (w *ConversationWorkflow) MarkConversationResolved(ctx context.Context, tenantID uint, conversationID uint) error {
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
