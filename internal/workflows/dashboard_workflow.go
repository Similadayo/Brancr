package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// DashboardWorkflow handles dashboard UI workflows for managing conversations, escalations, and posts
type DashboardWorkflow struct {
	DB            *gorm.DB
	Logger        *zap.Logger
	MessageSender MessageSender // Interface for sending messages to customers
}

// ConversationDashboardData represents data structure for conversation dashboard
type ConversationDashboardData struct {
	Conversations  []ConversationListItem
	TotalCount     int64
	ActiveCount    int64
	UnreadCount    int64
	EscalatedCount int64
	Filters        ConversationFilters
	Pagination     PaginationInfo
}

type ConversationListItem struct {
	ID             uint
	CustomerName   string
	CustomerAvatar string
	Platform       string
	PlatformIcon   string
	LastMessage    string
	LastMessageAt  time.Time
	Status         string
	UnreadCount    int
	Priority       string
	Intent         string
	Tags           []string
	Preview        string
}

type ConversationFilters struct {
	Status   string // active, resolved, escalated, archived
	Platform string // instagram, facebook, whatsapp, etc.
	Priority string // low, normal, high, urgent
	Search   string // search query
	DateFrom time.Time
	DateTo   time.Time
}

type PaginationInfo struct {
	Page       int
	PerPage    int
	TotalPages int
	TotalItems int64
}

// GetConversationDashboard retrieves conversation dashboard data
func (w *DashboardWorkflow) GetConversationDashboard(ctx context.Context, tenantID uint, filters ConversationFilters, page int, perPage int) (*ConversationDashboardData, error) {
	// Build query
	query := w.DB.Model(&storage.Conversation{}).Where("tenant_id = ?", tenantID)

	// Apply filters
	if filters.Status != "" {
		query = query.Where("status = ?", filters.Status)
	}
	if filters.Platform != "" {
		query = query.Where("platform = ?", filters.Platform)
	}
	if !filters.DateFrom.IsZero() {
		query = query.Where("created_at >= ?", filters.DateFrom)
	}
	if !filters.DateTo.IsZero() {
		query = query.Where("created_at <= ?", filters.DateTo)
	}

	// Get total count
	var totalCount int64
	if err := query.Count(&totalCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count conversations: %w", err)
	}

	// Get active count
	var activeCount int64
	w.DB.Model(&storage.Conversation{}).Where("tenant_id = ? AND status = ?", tenantID, "active").Count(&activeCount)

	// Get unread count (conversations with pending interactions)
	var unreadCount int64
	w.DB.Model(&storage.Conversation{}).
		Joins("JOIN interactions ON interactions.conversation_id = conversations.id").
		Where("conversations.tenant_id = ? AND interactions.direction = ? AND interactions.response_status = ?", tenantID, "incoming", "pending").
		Group("conversations.id").
		Count(&unreadCount)

	// Get escalated count
	var escalatedCount int64
	w.DB.Model(&storage.Conversation{}).Where("tenant_id = ? AND status = ?", tenantID, "escalated").Count(&escalatedCount)

	// Paginate
	offset := (page - 1) * perPage
	var conversations []storage.Conversation
	if err := query.Order("last_message_at DESC").Limit(perPage).Offset(offset).Find(&conversations).Error; err != nil {
		return nil, fmt.Errorf("failed to load conversations: %w", err)
	}

	// Convert to list items
	listItems := make([]ConversationListItem, 0, len(conversations))
	for _, conv := range conversations {
		item, err := w.convertConversationToListItem(ctx, tenantID, &conv, filters.Search)
		if err != nil {
			w.Logger.Warn("failed to convert conversation", zap.Error(err), zap.Uint("conversation_id", conv.ID))
			continue
		}
		listItems = append(listItems, *item)
	}

	totalPages := int((totalCount + int64(perPage) - 1) / int64(perPage))

	return &ConversationDashboardData{
		Conversations:  listItems,
		TotalCount:     totalCount,
		ActiveCount:    activeCount,
		UnreadCount:    unreadCount,
		EscalatedCount: escalatedCount,
		Filters:        filters,
		Pagination: PaginationInfo{
			Page:       page,
			PerPage:    perPage,
			TotalPages: totalPages,
			TotalItems: totalCount,
		},
	}, nil
}

// convertConversationToListItem converts a conversation to a dashboard list item
func (w *DashboardWorkflow) convertConversationToListItem(ctx context.Context, tenantID uint, conv *storage.Conversation, searchQuery string) (*ConversationListItem, error) {
	// Load customer
	var customer storage.Customer
	if err := w.DB.First(&customer, conv.CustomerID).Error; err != nil {
		return nil, fmt.Errorf("customer not found: %w", err)
	}

	customerName := customer.DisplayName
	if customerName == "" {
		customerName = customer.Username
	}
	if customerName == "" {
		customerName = fmt.Sprintf("Customer (%s)", customer.PlatformID[:min(8, len(customer.PlatformID))])
	}

	// Get last message
	var lastInteraction storage.Interaction
	w.DB.Where("conversation_id = ?", conv.ID).Order("created_at DESC").First(&lastInteraction)

	lastMessage := ""
	lastMessageAt := conv.LastMessageAt
	if lastInteraction.ID > 0 {
		lastMessage = lastInteraction.Content
		if len(lastMessage) > 100 {
			lastMessage = lastMessage[:100] + "..."
		}
		lastMessageAt = lastInteraction.CreatedAt
	}

	// Get unread count
	var unreadCount int64
	w.DB.Model(&storage.Interaction{}).
		Where("conversation_id = ? AND direction = ? AND response_status = ?", conv.ID, "incoming", "pending").
		Count(&unreadCount)

	// Get priority from last interaction
	priority := "normal"
	intent := "unknown"
	if lastInteraction.ID > 0 {
		if lastInteraction.DetectedIntent == "payment" || lastInteraction.DetectedIntent == "refund" {
			priority = "critical"
		} else if lastInteraction.DetectedIntent == "complaint" {
			priority = "high"
		}
		intent = lastInteraction.DetectedIntent
	}

	// Platform icon mapping
	platformIcons := map[string]string{
		"instagram": "📷",
		"facebook":  "👥",
		"whatsapp":  "💬",
		"tiktok":    "🎵",
		"youtube":   "📺",
	}

	return &ConversationListItem{
		ID:            conv.ID,
		CustomerName:  customerName,
		Platform:      conv.Platform,
		PlatformIcon:  platformIcons[conv.Platform],
		LastMessage:   lastMessage,
		LastMessageAt: lastMessageAt,
		Status:        conv.Status,
		UnreadCount:   int(unreadCount),
		Priority:      priority,
		Intent:        intent,
		Preview:       lastMessage,
	}, nil
}

// GetConversationDetail retrieves full conversation details for dashboard
type ConversationDetailData struct {
	Conversation   *storage.Conversation
	Customer       *storage.Customer
	Interactions   []InteractionDetail
	SuggestedReply string
	CanReply       bool
	Status         string
	Priority       string
}

type InteractionDetail struct {
	ID             uint
	Direction      string // incoming, outgoing
	Content        string
	MessageType    string
	CreatedAt      time.Time
	ResponseStatus string
	DetectedIntent string
	DetectedTone   string
	Confidence     float64
	IsEscalated    bool
}

// GetConversationDetail loads full conversation with all interactions
func (w *DashboardWorkflow) GetConversationDetail(ctx context.Context, tenantID uint, conversationID uint) (*ConversationDetailData, error) {
	// Load conversation
	var conversation storage.Conversation
	if err := w.DB.First(&conversation, conversationID).Error; err != nil {
		return nil, fmt.Errorf("conversation not found: %w", err)
	}

	if conversation.TenantID != tenantID {
		return nil, fmt.Errorf("unauthorized: conversation belongs to different tenant")
	}

	// Load customer
	var customer storage.Customer
	if err := w.DB.First(&customer, conversation.CustomerID).Error; err != nil {
		return nil, fmt.Errorf("customer not found: %w", err)
	}

	// Load all interactions
	var interactions []storage.Interaction
	if err := w.DB.Where("conversation_id = ?", conversationID).
		Order("created_at ASC").
		Find(&interactions).Error; err != nil {
		return nil, fmt.Errorf("failed to load interactions: %w", err)
	}

	// Convert to detail format
	interactionDetails := make([]InteractionDetail, 0, len(interactions))
	for _, interaction := range interactions {
		interactionDetails = append(interactionDetails, InteractionDetail{
			ID:             interaction.ID,
			Direction:      interaction.Direction,
			Content:        interaction.Content,
			MessageType:    interaction.MessageType,
			CreatedAt:      interaction.CreatedAt,
			ResponseStatus: interaction.ResponseStatus,
			DetectedIntent: interaction.DetectedIntent,
			DetectedTone:   interaction.DetectedTone,
			Confidence:     interaction.Confidence,
			IsEscalated:    interaction.ResponseType == "escalated",
		})
	}

	// Get suggested reply from last pending interaction
	suggestedReply := ""
	canReply := false
	var lastPendingInteraction storage.Interaction
	if err := w.DB.Where("conversation_id = ? AND direction = ? AND response_status = ?", conversationID, "incoming", "pending").
		Order("created_at DESC").
		First(&lastPendingInteraction).Error; err == nil {
		suggestedReply = lastPendingInteraction.SuggestedReply
		canReply = true
	}

	// Determine priority
	priority := "normal"
	if conversation.Status == "escalated" {
		priority = "high"
	}

	return &ConversationDetailData{
		Conversation:   &conversation,
		Customer:       &customer,
		Interactions:   interactionDetails,
		SuggestedReply: suggestedReply,
		CanReply:       canReply,
		Status:         conversation.Status,
		Priority:       priority,
	}, nil
}

// SendReplyFromDashboard sends a reply to customer from dashboard
func (w *DashboardWorkflow) SendReplyFromDashboard(ctx context.Context, tenantID uint, conversationID uint, reply string, interactionID uint) error {
	// Load conversation
	var conversation storage.Conversation
	if err := w.DB.First(&conversation, conversationID).Error; err != nil {
		return fmt.Errorf("conversation not found: %w", err)
	}

	if conversation.TenantID != tenantID {
		return fmt.Errorf("unauthorized")
	}

	// Create outgoing interaction
	var interaction storage.Interaction
	if interactionID > 0 {
		if err := w.DB.First(&interaction, interactionID).Error; err != nil {
			return fmt.Errorf("interaction not found: %w", err)
		}
	}

	outgoingInteraction := storage.Interaction{
		TenantID:       tenantID,
		CustomerID:     conversation.CustomerID,
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
		return fmt.Errorf("failed to create interaction: %w", err)
	}

	// Update original interaction if responding to one
	if interaction.ID > 0 {
		interaction.ResponseStatus = "sent"
		interaction.FinalReply = reply
		interaction.ResponseType = "manual"
		now := time.Now()
		interaction.EscalatedAt = &now
		w.DB.Save(&interaction)
	}

	// Update conversation
	conversation.Status = "waiting"
	conversation.LastMessageAt = time.Now()
	w.DB.Save(&conversation)

	// Actually send reply via social media API
	if w.MessageSender != nil {
		// Load customer for platform ID
		var customer storage.Customer
		if err := w.DB.First(&customer, conversation.CustomerID).Error; err != nil {
			w.Logger.Error("failed to load customer for sending reply", zap.Error(err))
			return fmt.Errorf("customer not found: %w", err)
		}

		threadID := conversation.PlatformThreadID
		if threadID == "" {
			threadID = customer.PlatformID
		}

		switch conversation.Platform {
		case "whatsapp":
			if err := w.MessageSender.SendWhatsAppReply(context.Background(), tenantID, customer.PlatformID, reply); err != nil {
				w.Logger.Error("failed to send reply to customer",
					zap.Error(err),
					zap.Uint("conversation_id", conversationID),
					zap.String("customer_platform_id", customer.PlatformID))
				return fmt.Errorf("failed to send reply: %w", err)
			}
		case "instagram", "facebook":
			if err := w.MessageSender.SendReply(context.Background(), tenantID, conversation.Platform, threadID, customer.PlatformID, reply); err != nil {
				w.Logger.Error("failed to send reply to customer",
					zap.Error(err),
					zap.Uint("conversation_id", conversationID),
					zap.String("customer_platform_id", customer.PlatformID),
					zap.String("thread_id", threadID))
				return fmt.Errorf("failed to send reply: %w", err)
			}
		default:
			w.Logger.Warn("reply sending not implemented for platform",
				zap.String("platform", conversation.Platform),
				zap.Uint("conversation_id", conversationID))
		}
	}

	w.Logger.Info("reply sent from dashboard",
		zap.Uint("tenant_id", tenantID),
		zap.Uint("conversation_id", conversationID),
	)

	return nil
}

// GetEscalationDashboard retrieves escalation dashboard data
type EscalationDashboardData struct {
	Escalations   []EscalationListItem
	TotalCount    int64
	PendingCount  int64
	ResolvedCount int64
	Filters       EscalationFilters
	Pagination    PaginationInfo
	Stats         EscalationStats
}

type EscalationListItem struct {
	InteractionID  uint
	ConversationID uint
	CustomerName   string
	Platform       string
	Message        string
	SuggestedReply string
	Intent         string
	Tone           string
	Priority       string
	CreatedAt      time.Time
	Age            time.Duration
}

type EscalationFilters struct {
	Priority string
	Intent   string
	Platform string
	Status   string // pending, resolved, ignored
	DateFrom time.Time
	DateTo   time.Time
}

type EscalationStats struct {
	TotalPending    int64
	CriticalPending int64
	AvgResponseTime time.Duration
	ResolvedToday   int64
}

// GetEscalationDashboard retrieves escalation dashboard data
func (w *DashboardWorkflow) GetEscalationDashboard(ctx context.Context, tenantID uint, filters EscalationFilters, page int, perPage int) (*EscalationDashboardData, error) {
	// Build query
	query := w.DB.Model(&storage.Interaction{}).Where("tenant_id = ? AND response_type = ?", tenantID, "escalated")

	// Apply filters
	if filters.Status != "" {
		query = query.Where("response_status = ?", filters.Status)
	} else {
		query = query.Where("response_status = ?", "pending") // Default to pending
	}
	if filters.Platform != "" {
		query = query.Where("platform = ?", filters.Platform)
	}
	if filters.Intent != "" {
		query = query.Where("detected_intent = ?", filters.Intent)
	}
	if !filters.DateFrom.IsZero() {
		query = query.Where("created_at >= ?", filters.DateFrom)
	}
	if !filters.DateTo.IsZero() {
		query = query.Where("created_at <= ?", filters.DateTo)
	}

	// Get counts
	var totalCount int64
	query.Count(&totalCount)

	var pendingCount int64
	w.DB.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND response_type = ? AND response_status = ?", tenantID, "escalated", "pending").
		Count(&pendingCount)

	var resolvedCount int64
	w.DB.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND response_type = ? AND response_status = ?", tenantID, "escalated", "resolved").
		Count(&resolvedCount)

	// Paginate
	offset := (page - 1) * perPage
	var interactions []storage.Interaction
	if err := query.Order("created_at DESC").Limit(perPage).Offset(offset).Find(&interactions).Error; err != nil {
		return nil, fmt.Errorf("failed to load escalations: %w", err)
	}

	// Convert to list items
	listItems := make([]EscalationListItem, 0, len(interactions))
	for _, interaction := range interactions {
		item, err := w.convertInteractionToEscalationItem(ctx, tenantID, &interaction)
		if err != nil {
			w.Logger.Warn("failed to convert escalation", zap.Error(err))
			continue
		}
		listItems = append(listItems, *item)
	}

	// Calculate stats
	stats := EscalationStats{
		TotalPending:  pendingCount,
		ResolvedToday: 0, // TODO: Calculate from timestamps
	}

	// Get critical pending count
	var criticalPending int64
	w.DB.Model(&storage.Interaction{}).
		Where("tenant_id = ? AND response_type = ? AND response_status = ? AND detected_intent IN ?", tenantID, "escalated", "pending", []string{"payment", "refund"}).
		Count(&criticalPending)
	stats.CriticalPending = criticalPending

	totalPages := int((totalCount + int64(perPage) - 1) / int64(perPage))

	return &EscalationDashboardData{
		Escalations:   listItems,
		TotalCount:    totalCount,
		PendingCount:  pendingCount,
		ResolvedCount: resolvedCount,
		Filters:       filters,
		Pagination: PaginationInfo{
			Page:       page,
			PerPage:    perPage,
			TotalPages: totalPages,
			TotalItems: totalCount,
		},
		Stats: stats,
	}, nil
}

// convertInteractionToEscalationItem converts interaction to escalation list item
func (w *DashboardWorkflow) convertInteractionToEscalationItem(ctx context.Context, tenantID uint, interaction *storage.Interaction) (*EscalationListItem, error) {
	// Load customer
	var customer storage.Customer
	if err := w.DB.First(&customer, interaction.CustomerID).Error; err != nil {
		return nil, fmt.Errorf("customer not found: %w", err)
	}

	customerName := customer.DisplayName
	if customerName == "" {
		customerName = customer.Username
	}
	if customerName == "" {
		customerName = fmt.Sprintf("Customer (%s)", customer.PlatformID[:min(8, len(customer.PlatformID))])
	}

	// Determine priority
	priority := "normal"
	if interaction.DetectedIntent == "payment" || interaction.DetectedIntent == "refund" {
		priority = "critical"
	} else if interaction.DetectedIntent == "complaint" {
		priority = "high"
	}

	message := interaction.Content
	if len(message) > 150 {
		message = message[:150] + "..."
	}

	suggestedReply := interaction.SuggestedReply
	if len(suggestedReply) > 100 {
		suggestedReply = suggestedReply[:100] + "..."
	}

	age := time.Since(interaction.CreatedAt)

	return &EscalationListItem{
		InteractionID:  interaction.ID,
		ConversationID: interaction.ConversationID,
		CustomerName:   customerName,
		Platform:       interaction.Platform,
		Message:        message,
		SuggestedReply: suggestedReply,
		Intent:         interaction.DetectedIntent,
		Tone:           interaction.DetectedTone,
		Priority:       priority,
		CreatedAt:      interaction.CreatedAt,
		Age:            age,
	}, nil
}

// Dashboard API Handlers

// ConversationDashboardHandler is an HTTP handler for conversation dashboard
func (w *DashboardWorkflow) ConversationDashboardHandler(tenantID uint) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Parse query parameters
		page := parseIntQuery(r, "page", 1)
		perPage := parseIntQuery(r, "per_page", 20)

		filters := ConversationFilters{
			Status:   r.URL.Query().Get("status"),
			Platform: r.URL.Query().Get("platform"),
			Priority: r.URL.Query().Get("priority"),
			Search:   r.URL.Query().Get("search"),
		}

		data, err := w.GetConversationDashboard(ctx, tenantID, filters, page, perPage)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(data)
	}
}

// EscalationDashboardHandler is an HTTP handler for escalation dashboard
func (w *DashboardWorkflow) EscalationDashboardHandler(tenantID uint) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		page := parseIntQuery(r, "page", 1)
		perPage := parseIntQuery(r, "per_page", 20)

		filters := EscalationFilters{
			Priority: r.URL.Query().Get("priority"),
			Intent:   r.URL.Query().Get("intent"),
			Platform: r.URL.Query().Get("platform"),
			Status:   r.URL.Query().Get("status"),
		}

		data, err := w.GetEscalationDashboard(ctx, tenantID, filters, page, perPage)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(data)
	}
}

// Helper functions

func parseIntQuery(r *http.Request, key string, defaultValue int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return defaultValue
	}
	var result int
	fmt.Sscanf(value, "%d", &result)
	if result < 1 {
		return defaultValue
	}
	return result
}
