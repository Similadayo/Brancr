package ai

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MessageIngestion is Stage 1 of the v2.0 pipeline.
// It normalizes raw platform input into a standard format.
// NO AI. NO LOGIC. NO MEMORY.
type MessageIngestion struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

// NewMessageIngestion creates a new message ingestion stage.
func NewMessageIngestion(db *gorm.DB, logger *zap.Logger) *MessageIngestion {
	return &MessageIngestion{
		DB:     db,
		Logger: logger,
	}
}

// RawMessage is the input from any platform (Telegram, WhatsApp, Instagram, etc.)
type RawMessage struct {
	Platform   string                 // "telegram", "whatsapp", "instagram"
	TenantID   uuid.UUID              // From webhook/routing
	CustomerID string                 // Platform-specific ID (chat_id, wa_id, etc.)
	ThreadID   string                 // Platform conversation ID
	Content    string                 // Raw message text
	Timestamp  time.Time              // When message was received
	Metadata   map[string]interface{} // Platform-specific extras
}

// NormalizedMessage is the output — clean, internal representation.
type NormalizedMessage struct {
	TenantID       uuid.UUID
	CustomerID     uuid.UUID // Resolved to internal UUID
	ConversationID uuid.UUID // Resolved to internal UUID
	Platform       string    // Normalized platform name
	Text           string    // Cleaned message text
	Timestamp      time.Time
}

// Ingest normalizes a raw message into internal format.
// This is purely deterministic — no AI, no business logic.
func (m *MessageIngestion) Ingest(ctx context.Context, raw *RawMessage) (*NormalizedMessage, error) {
	// Step 1: Resolve or create customer
	customerID, err := m.resolveCustomer(ctx, raw.TenantID, raw.CustomerID, raw.Platform)
	if err != nil {
		return nil, err
	}

	// Step 2: Resolve or create conversation
	conversationID, err := m.resolveConversation(ctx, raw.TenantID, customerID, raw.Platform, raw.ThreadID)
	if err != nil {
		return nil, err
	}

	// Step 3: Clean message text
	cleanedText := m.cleanText(raw.Content)

	// Step 4: Normalize platform name
	platform := m.normalizePlatform(raw.Platform)

	m.Logger.Debug("📥 Message ingested",
		zap.Stringer("tenant_id", raw.TenantID),
		zap.Stringer("customer_id", customerID),
		zap.Stringer("conversation_id", conversationID),
		zap.String("platform", platform),
		zap.String("text_preview", truncate(cleanedText, 50)))

	return &NormalizedMessage{
		TenantID:       raw.TenantID,
		CustomerID:     customerID,
		ConversationID: conversationID,
		Platform:       platform,
		Text:           cleanedText,
		Timestamp:      raw.Timestamp,
	}, nil
}

// resolveCustomer finds or creates a customer from platform ID.
func (m *MessageIngestion) resolveCustomer(ctx context.Context, tenantID uuid.UUID, platformID, platform string) (uuid.UUID, error) {
	var customer storage.Customer

	// Try to find existing customer
	err := m.DB.WithContext(ctx).
		Where("tenant_id = ? AND platform_id = ? AND channel = ?", tenantID, platformID, platform).
		First(&customer).Error

	if err == nil {
		return customer.ID, nil
	}

	if err != gorm.ErrRecordNotFound {
		return uuid.Nil, err
	}

	// Create new customer
	customer = storage.Customer{
		TenantID:   tenantID,
		PlatformID: platformID,
		Channel:    platform,
		CreatedAt:  time.Now(),
	}

	if err := m.DB.WithContext(ctx).Create(&customer).Error; err != nil {
		return uuid.Nil, err
	}

	m.Logger.Info("👤 New customer created",
		zap.Stringer("customer_id", customer.ID),
		zap.String("platform_id", platformID),
		zap.String("platform", platform))

	return customer.ID, nil
}

// resolveConversation finds or creates a conversation.
func (m *MessageIngestion) resolveConversation(ctx context.Context, tenantID, customerID uuid.UUID, platform, threadID string) (uuid.UUID, error) {
	var conversation storage.Conversation

	// Try to find existing active conversation
	err := m.DB.WithContext(ctx).
		Where("tenant_id = ? AND customer_id = ? AND platform = ?", tenantID, customerID, platform).
		Where("status NOT IN ?", []string{"closed", "archived"}).
		Order("created_at DESC").
		First(&conversation).Error

	if err == nil {
		return conversation.ID, nil
	}

	if err != gorm.ErrRecordNotFound {
		return uuid.Nil, err
	}

	// Create new conversation
	conversation = storage.Conversation{
		TenantID:   tenantID,
		CustomerID: customerID,
		Platform:   platform,
		Status:     "active",
		CreatedAt:  time.Now(),
	}

	if err := m.DB.WithContext(ctx).Create(&conversation).Error; err != nil {
		return uuid.Nil, err
	}

	m.Logger.Info("💬 New conversation created",
		zap.Stringer("conversation_id", conversation.ID),
		zap.Stringer("customer_id", customerID),
		zap.String("platform", platform))

	return conversation.ID, nil
}

// cleanText normalizes message text.
func (m *MessageIngestion) cleanText(text string) string {
	// Trim whitespace
	text = strings.TrimSpace(text)

	// Normalize line breaks
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	// Collapse multiple newlines
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}

	return text
}

// normalizePlatform standardizes platform names.
func (m *MessageIngestion) normalizePlatform(platform string) string {
	switch strings.ToLower(platform) {
	case "telegram", "tg":
		return "telegram"
	case "whatsapp", "wa":
		return "whatsapp"
	case "instagram", "ig", "insta":
		return "instagram"
	case "facebook", "fb", "messenger":
		return "facebook"
	case "web", "website", "chat":
		return "web"
	default:
		return strings.ToLower(platform)
	}
}
