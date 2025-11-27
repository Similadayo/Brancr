package workflows

import (
	"context"
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/similadayo/brancr/internal/ai"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// WorkflowOrchestrator coordinates all workflow services and provides unified interface
type WorkflowOrchestrator struct {
	DB                  *gorm.DB
	Logger              *zap.Logger
	PostingWorkflow     *PostingWorkflow
	ConversationWorkflow *ConversationWorkflow
	EscalationWorkflow  *EscalationWorkflow
	DashboardWorkflow   *DashboardWorkflow
}

// NewWorkflowOrchestrator creates a new workflow orchestrator with all services initialized
func NewWorkflowOrchestrator(db *gorm.DB, logger *zap.Logger, aiClient *ai.AIClient, memory *ai.MemoryService, botManager interface{}) *WorkflowOrchestrator {
	orchestrator := &WorkflowOrchestrator{
		DB:     db,
		Logger: logger,
	}

	// Initialize posting workflow
	orchestrator.PostingWorkflow = &PostingWorkflow{
		DB:         db,
		Logger:     logger,
		BotManager: botManager,
	}

	// Initialize conversation workflow
	orchestrator.ConversationWorkflow = &ConversationWorkflow{
		DB:                db,
		Logger:            logger,
		BotManager:        botManager,
		AIClient:          aiClient,
		Memory:            memory,
	}

	// Initialize escalation workflow
	orchestrator.EscalationWorkflow = &EscalationWorkflow{
		DB:         db,
		Logger:     logger,
		BotManager: botManager,
	}

	// Initialize dashboard workflow
	orchestrator.DashboardWorkflow = &DashboardWorkflow{
		DB:     db,
		Logger: logger,
	}

	return orchestrator
}

// SetBotForPosting sets the bot instance for posting workflow
func (o *WorkflowOrchestrator) SetBotForPosting(bot *tgbotapi.BotAPI) {
	if o.PostingWorkflow != nil {
		o.PostingWorkflow.Bot = bot
	}
}

// ============================================================================
// POSTING WORKFLOW METHODS
// ============================================================================

// StartPostingFlow begins the posting workflow
func (o *WorkflowOrchestrator) StartPostingFlow(ctx context.Context, tenantID uint, chatID int64, mediaAssetID uint) error {
	if o.PostingWorkflow == nil {
		return fmt.Errorf("posting workflow not initialized")
	}
	return o.PostingWorkflow.StartPostingFlow(ctx, tenantID, chatID, mediaAssetID)
}

// HandlePostingInput processes user input in posting workflow
func (o *WorkflowOrchestrator) HandlePostingInput(ctx context.Context, tenantID uint, chatID int64, input string) error {
	if o.PostingWorkflow == nil {
		return fmt.Errorf("posting workflow not initialized")
	}
	return o.PostingWorkflow.HandleUserInput(ctx, tenantID, chatID, input)
}

// HandlePostingPlatformToggle toggles a platform in the selection
func (o *WorkflowOrchestrator) HandlePostingPlatformToggle(ctx context.Context, tenantID uint, chatID int64, platform string) error {
	if o.PostingWorkflow == nil {
		return fmt.Errorf("posting workflow not initialized")
	}
	return o.PostingWorkflow.TogglePlatform(ctx, tenantID, chatID, platform)
}

// HandlePostingPlatformConfirm confirms platform selection
func (o *WorkflowOrchestrator) HandlePostingPlatformConfirm(ctx context.Context, tenantID uint, chatID int64) error {
	if o.PostingWorkflow == nil {
		return fmt.Errorf("posting workflow not initialized")
	}
	return o.PostingWorkflow.ConfirmPlatforms(ctx, tenantID, chatID)
}

// ============================================================================
// CONVERSATION WORKFLOW METHODS
// ============================================================================

// HandleIncomingCustomerMessage processes incoming customer message through full workflow
func (o *WorkflowOrchestrator) HandleIncomingCustomerMessage(ctx context.Context, tenantID uint, customerID uint, platform string, message string, messageID string, threadID string) error {
	if o.ConversationWorkflow == nil {
		return fmt.Errorf("conversation workflow not initialized")
	}
	return o.ConversationWorkflow.HandleCustomerMessage(ctx, tenantID, customerID, platform, message, messageID, threadID)
}

// HandleTenantReply processes tenant's reply to customer
func (o *WorkflowOrchestrator) HandleTenantReply(ctx context.Context, tenantID uint, conversationID uint, reply string, interactionID uint) error {
	if o.ConversationWorkflow == nil {
		return fmt.Errorf("conversation workflow not initialized")
	}
	return o.ConversationWorkflow.HandleTenantReply(ctx, tenantID, conversationID, reply, interactionID)
}

// GetConversations retrieves conversations for dashboard
func (o *WorkflowOrchestrator) GetConversations(ctx context.Context, tenantID uint, status string, platform string, limit int, offset int) ([]storage.Conversation, error) {
	if o.ConversationWorkflow == nil {
		return nil, fmt.Errorf("conversation workflow not initialized")
	}
	return o.ConversationWorkflow.GetConversations(ctx, tenantID, status, platform, limit, offset)
}

// GetConversationWithMessages retrieves full conversation with all messages
func (o *WorkflowOrchestrator) GetConversationWithMessages(ctx context.Context, tenantID uint, conversationID uint) (*ConversationContext, []storage.Interaction, error) {
	if o.ConversationWorkflow == nil {
		return nil, nil, fmt.Errorf("conversation workflow not initialized")
	}
	return o.ConversationWorkflow.GetConversationWithMessages(ctx, tenantID, conversationID)
}

// ============================================================================
// ESCALATION WORKFLOW METHODS
// ============================================================================

// CreateEscalation creates a new escalation
func (o *WorkflowOrchestrator) CreateEscalation(ctx context.Context, interactionID uint, priority EscalationPriority) error {
	if o.EscalationWorkflow == nil {
		return fmt.Errorf("escalation workflow not initialized")
	}
	return o.EscalationWorkflow.CreateEscalation(ctx, interactionID, priority)
}

// HandleEscalationAction processes tenant action on escalation
func (o *WorkflowOrchestrator) HandleEscalationAction(ctx context.Context, tenantID uint, interactionID uint, action EscalationAction, params map[string]interface{}) error {
	if o.EscalationWorkflow == nil {
		return fmt.Errorf("escalation workflow not initialized")
	}
	return o.EscalationWorkflow.HandleEscalationAction(ctx, tenantID, interactionID, action, params)
}

// GetPendingEscalations retrieves pending escalations
func (o *WorkflowOrchestrator) GetPendingEscalations(ctx context.Context, tenantID uint, priority EscalationPriority, limit int) ([]storage.Interaction, error) {
	if o.EscalationWorkflow == nil {
		return nil, fmt.Errorf("escalation workflow not initialized")
	}
	return o.EscalationWorkflow.GetPendingEscalations(ctx, tenantID, priority, limit)
}

// GetEscalationStats retrieves escalation statistics
func (o *WorkflowOrchestrator) GetEscalationStats(ctx context.Context, tenantID uint, startDate, endDate time.Time) (map[string]interface{}, error) {
	if o.EscalationWorkflow == nil {
		return nil, fmt.Errorf("escalation workflow not initialized")
	}
	return o.EscalationWorkflow.GetEscalationStats(ctx, tenantID, startDate, endDate)
}

// ============================================================================
// DASHBOARD WORKFLOW METHODS
// ============================================================================

// GetConversationDashboard retrieves conversation dashboard data
func (o *WorkflowOrchestrator) GetConversationDashboard(ctx context.Context, tenantID uint, filters ConversationFilters, page int, perPage int) (*ConversationDashboardData, error) {
	if o.DashboardWorkflow == nil {
		return nil, fmt.Errorf("dashboard workflow not initialized")
	}
	return o.DashboardWorkflow.GetConversationDashboard(ctx, tenantID, filters, page, perPage)
}

// GetConversationDetail retrieves full conversation details
func (o *WorkflowOrchestrator) GetConversationDetail(ctx context.Context, tenantID uint, conversationID uint) (*ConversationDetailData, error) {
	if o.DashboardWorkflow == nil {
		return nil, fmt.Errorf("dashboard workflow not initialized")
	}
	return o.DashboardWorkflow.GetConversationDetail(ctx, tenantID, conversationID)
}

// SendReplyFromDashboard sends reply from dashboard
func (o *WorkflowOrchestrator) SendReplyFromDashboard(ctx context.Context, tenantID uint, conversationID uint, reply string, interactionID uint) error {
	if o.DashboardWorkflow == nil {
		return fmt.Errorf("dashboard workflow not initialized")
	}
	return o.DashboardWorkflow.SendReplyFromDashboard(ctx, tenantID, conversationID, reply, interactionID)
}

// GetEscalationDashboard retrieves escalation dashboard data
func (o *WorkflowOrchestrator) GetEscalationDashboard(ctx context.Context, tenantID uint, filters EscalationFilters, page int, perPage int) (*EscalationDashboardData, error) {
	if o.DashboardWorkflow == nil {
		return nil, fmt.Errorf("dashboard workflow not initialized")
	}
	return o.DashboardWorkflow.GetEscalationDashboard(ctx, tenantID, filters, page, perPage)
}

// ============================================================================
// UNIFIED WORKFLOW METHODS
// ============================================================================

// ProcessIncomingMessage is a unified entry point for processing any incoming message
func (o *WorkflowOrchestrator) ProcessIncomingMessage(ctx context.Context, tenantID uint, platform string, message string, messageID string, threadID string, customerID uint) error {
	// Step 1: Handle as customer message through conversation workflow
	err := o.HandleIncomingCustomerMessage(ctx, tenantID, customerID, platform, message, messageID, threadID)
	if err != nil {
		o.Logger.Error("failed to process incoming message", zap.Error(err))
		return fmt.Errorf("failed to process message: %w", err)
	}

	return nil
}

// GetWorkflowSummary provides a summary of all workflow states for a tenant
type WorkflowSummary struct {
	TenantID            uint
	ActivePostingFlows  int
	ActiveConversations int
	PendingEscalations  int
	UnreadMessages      int
}

// GetWorkflowSummary retrieves summary of all workflows for a tenant
func (o *WorkflowOrchestrator) GetWorkflowSummary(ctx context.Context, tenantID uint) (*WorkflowSummary, error) {
	summary := &WorkflowSummary{
		TenantID: tenantID,
	}

	// Count active conversations
	var activeConversations int64
	o.DB.Model(&storage.Conversation{}).
		Where("tenant_id = ? AND status = ?", tenantID, "active").
		Count(&activeConversations)
	summary.ActiveConversations = int(activeConversations)

	// Count pending escalations
	var pendingEscalations int64
	o.DB.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND response_type = ? AND response_status = ?", tenantID, "escalated", "pending").
		Count(&pendingEscalations)
	summary.PendingEscalations = int(pendingEscalations)

	// Count unread messages
	var unreadMessages int64
	o.DB.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND direction = ? AND response_status = ?", tenantID, "incoming", "pending").
		Count(&unreadMessages)
	summary.UnreadMessages = int(unreadMessages)

	return summary, nil
}

