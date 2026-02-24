package storage

import (
	"time"

	"github.com/google/uuid"
)

// Conversation represents a customer conversation thread
type Conversation struct {
	ID               uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID         uuid.UUID `gorm:"index"`
	CustomerID       uuid.UUID `gorm:"index"`
	Platform         string    `gorm:"size:50;index"`  // instagram, facebook, tiktok, youtube, whatsapp
	Source           string    `gorm:"size:50;index"`  // Channel source for attribution (same as platform)
	PlatformThreadID string    `gorm:"size:100;index"` // Platform-specific thread/conversation ID
	Status           string    `gorm:"size:20"`        // active, resolved, archived
	LastMessageAt    time.Time `gorm:"index"`
	AIState          string    `gorm:"type:jsonb;default:'{}'"` // Stores persistent AI state (stage, last domain)
	AIOrderState     string    `gorm:"type:jsonb;default:'{}'"` // Stores persistent Order state (items, subtotal, stage)
	CreatedAt        time.Time `gorm:"autoCreateTime"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime"`
}

// Interaction represents a single message exchange in a conversation
type Interaction struct {
	ID                uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID          uuid.UUID `gorm:"index"`
	CustomerID        uuid.UUID `gorm:"index"`
	ConversationID    uuid.UUID `gorm:"index"`
	Platform          string    `gorm:"size:50"`
	Direction         string    `gorm:"size:20"` // incoming, outgoing
	MessageType       string    `gorm:"size:20"` // text, image, video, comment
	Content           string    `gorm:"type:text"`
	PlatformMessageID string    `gorm:"type:text;index"` // Platform-specific message ID (can be very long for Instagram)

	// AI Analysis
	DetectedIntent string  `gorm:"size:50"` // inquiry, order, payment, complaint, casual, refund, delivery_issue
	DetectedTone   string  `gorm:"size:50"` // positive, neutral, negative, urgent
	Confidence     float64 // 0-1 confidence score

	// Response handling
	ResponseType     string `gorm:"size:20"`   // auto_reply, escalated, manual
	ResponseStatus   string `gorm:"size:20"`   // pending, approved, sent, rejected
	SuggestedReply   string `gorm:"type:text"` // AI-generated suggested reply
	FinalReply       string `gorm:"type:text"` // Final reply sent to customer
	EscalatedTo      int64  `gorm:"index"`     // Telegram ID of tenant who handled it
	EscalatedAt      *time.Time
	EscalationReason string `gorm:"size:255"` // Short reason explaining why message was escalated

	// Metadata
	Metadata  string    `gorm:"type:jsonb"` // Additional platform-specific data
	CreatedAt time.Time `gorm:"autoCreateTime"`

	// Security fields (for payment/financial escalations)
	EncryptedContent string `gorm:"type:text"`                  // Encrypted message content for sensitive escalations
	IsEncrypted      bool   `gorm:"default:false"`              // Flag to indicate if content is encrypted
	SecurityLevel    string `gorm:"size:20;default:'standard'"` // standard, sensitive, payment
}
