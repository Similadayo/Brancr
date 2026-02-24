package workflows

// This file demonstrates how to integrate the workflow system into your application
// These are example integration points that show how the workflows work together

/*
WORKFLOW SYSTEM OVERVIEW
========================

This workflow system provides professional, UX-friendly workflows for:

1. POSTING FLOW (Telegram Bot)
   - Step-by-step media upload and post creation
   - Platform selection with visual indicators
   - Caption generation and editing
   - Scheduling with natural language support
   - Review and confirmation before publishing

2. CONVERSATION WORKFLOW (Customer-Tenant Chat)
   - Automatic message routing and intent detection
   - AI-powered auto-replies
   - Context-aware conversation management
   - Escalation triggers for complex issues
   - Full conversation history tracking

3. ESCALATION WORKFLOW (Customer to Tenant)
   - Priority-based escalation system
   - Rich notification formatting
   - Quick action buttons (Approve, Edit, Custom Reply)
   - Response tracking and statistics
   - Dashboard integration

4. DASHBOARD WORKFLOWS
   - Conversation list with filters and search
   - Detailed conversation view with full history
   - Escalation management interface
   - Statistics and analytics
   - Real-time updates

INTEGRATION EXAMPLES
====================

Example 1: Initializing the Workflow Orchestrator

	import (
		"context"
		"github.com/similadayo/brancr/internal/workflows"
		"github.com/similadayo/brancr/internal/ai"
		"gorm.io/gorm"
	)

	// Initialize orchestrator
	orchestrator := workflows.NewWorkflowOrchestrator(
		db,              // *gorm.DB
		logger,          // *zap.Logger
		aiClient,        // *ai.AIClient
		memoryService,   // *ai.MemoryService
		botManager,      // TenantBotManager interface
	)

	// Set bot for posting workflow
	orchestrator.SetBotForPosting(telegramBot)


Example 2: Handling Incoming Customer Message

	// When a message comes from Instagram/Facebook/WhatsApp
	func handleIncomingMessage(tenantID uuid.UUID, platform string, message string, customerID uuid.UUID) {
		ctx := context.Background()

		// Process through unified workflow
		err := orchestrator.ProcessIncomingMessage(
			ctx,
			tenantID,
			platform,
			message,
			messageID,
			threadID,
			customerID,
		)

		// The workflow will:
		// 1. Create/get conversation
		// 2. Analyze intent and emotion
		// 3. Either auto-reply or escalate to tenant
		// 4. Send notifications via Telegram bot
	}


Example 3: Starting Post Creation Flow

	// In Telegram bot handler when user uploads media
	func handleMediaUpload(bot *tgbotapi.BotAPI, tenantID uuid.UUID, chatID int64, mediaAssetID uuid.UUID) {
		ctx := context.Background()

		// Start posting workflow
		err := orchestrator.StartPostingFlow(ctx, tenantID, chatID, mediaAssetID)

		// User will see guided prompts:
		// 1. "Would you like to create a post?"
		// 2. "Select platforms..."
		// 3. "Write caption or use AI..."
		// 4. "When to publish?"
		// 5. "Review and confirm"
	}


Example 4: Handling Tenant Reply to Escalation

	// In Telegram bot callback handler
	func handleEscalationApprove(tenantID uuid.UUID, interactionID uuid.UUID) {
		ctx := context.Background()

		params := map[string]interface{}{
			"reply": suggestedReply,
		}

		err := orchestrator.HandleEscalationAction(
			ctx,
			tenantID,
			interactionID,
			workflows.EscalationActionApprove,
			params,
		)

		// This will:
		// 1. Update interaction status
		// 2. Send reply to customer
		// 3. Update conversation
		// 4. Send confirmation to tenant
	}


Example 5: Dashboard Conversation List

	// In dashboard API handler
	func getConversationsHandler(w http.ResponseWriter, r *http.Request) {
		tenantID := getTenantIDFromContext(r)
		ctx := r.Context()

		page := parseInt(r.URL.Query().Get("page"), 1)
		perPage := parseInt(r.URL.Query().Get("per_page"), 20)

		filters := workflows.ConversationFilters{
			Status:   r.URL.Query().Get("status"),
			Platform: r.URL.Query().Get("platform"),
			Priority: r.URL.Query().Get("priority"),
		}

		data, err := orchestrator.GetConversationDashboard(
			ctx,
			tenantID,
			filters,
			page,
			perPage,
		)

		// Returns:
		// - List of conversations with preview
		// - Unread counts
		// - Status indicators
		// - Pagination info
	}


Example 6: Dashboard Conversation Detail

	// Load full conversation for dashboard view
	func getConversationDetail(tenantID uuid.UUID, conversationID uuid.UUID) {
		ctx := context.Background()

		detail, err := orchestrator.GetConversationDetail(
			ctx,
			tenantID,
			conversationID,
		)

		// Returns:
		// - Full conversation object
		// - Customer info
		// - All interactions/messages
		// - Suggested reply if pending
		// - Reply capability flag
	}


Example 7: Sending Reply from Dashboard

	// When tenant sends reply from dashboard UI
	func sendReplyFromDashboard(tenantID uuid.UUID, conversationID uuid.UUID, reply string) {
		ctx := context.Background()

		err := orchestrator.SendReplyFromDashboard(
			ctx,
			tenantID,
			conversationID,
			reply,
			0, // interactionID - 0 means respond to latest
		)

		// This will:
		// 1. Create outgoing interaction
		// 2. Update conversation status
		// 3. Send via social media API
		// 4. Update pending interactions
	}


Example 8: Getting Workflow Summary

	// For dashboard overview widget
	func getWorkflowSummary(tenantID uuid.UUID) {
		ctx := context.Background()

		summary, err := orchestrator.GetWorkflowSummary(ctx, tenantID)

		// Returns:
		// - Active conversations count
		// - Pending escalations count
		// - Unread messages count
		// - Active posting flows count
	}


WORKFLOW FLOW DIAGRAMS
======================

Posting Flow:
  1. Media Upload → Init Prompt
  2. User Confirms → Media Type Selection (if carousel)
  3. Optional Name → Caption Step
  4. Caption Input → Platform Selection
  5. Platform Selection → Scheduling
  6. Schedule Input → Review
  7. Review → Confirm → Post Scheduled

Conversation Flow:
  1. Customer Message → Intent Detection
  2. AI Analysis → Policy Decision
  3a. Auto-Reply Path: Generate Reply → Send → Save
  3b. Escalation Path: Create Escalation → Notify Tenant → Wait

Escalation Flow:
  1. Escalation Created → Priority Determined
  2. Notification Sent → Tenant Views
  3. Tenant Action:
     - Approve → Send Suggested Reply
     - Edit → Modify Reply → Send
     - Custom → Write New Reply → Send
     - Ignore → Mark Ignored
     - Resolve → Mark Resolved

Dashboard Flow:
  1. Load Conversations → Apply Filters
  2. Display List → User Clicks Conversation
  3. Load Detail → Show Full History
  4. User Replies → Send via Workflow
  5. Update UI → Refresh Status

BEST PRACTICES
==============

1. Always use the orchestrator as the main entry point
2. Store workflow state in Redis for scalability (not in-memory)
3. Handle errors gracefully with user-friendly messages
4. Log all workflow transitions for debugging
5. Use context.Context for request cancellation
6. Validate tenant ownership before any operation
7. Queue heavy operations (AI analysis, message sending)
8. Implement rate limiting for escalation actions
9. Cache frequently accessed conversation data
10. Use WebSockets for real-time dashboard updates
*/

// This file serves as documentation - actual integration happens in your handlers
// See the individual workflow files for implementation details
