package storage

import (
	"time"
)

// Conversation represents a customer conversation thread
type Conversation struct {
	ID               uint      `gorm:"primaryKey"`
	TenantID         uint      `gorm:"index"`
	CustomerID       uint      `gorm:"index"`
	Platform         string    `gorm:"size:50;index"`  // instagram, facebook, tiktok, youtube, whatsapp
	PlatformThreadID string    `gorm:"size:100;index"` // Platform-specific thread/conversation ID
	Status           string    `gorm:"size:20"`        // active, resolved, archived
	LastMessageAt    time.Time `gorm:"index"`
	CreatedAt        time.Time `gorm:"autoCreateTime"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime"`
}

// Interaction represents a single message exchange in a conversation
type Interaction struct {
	ID                uint   `gorm:"primaryKey"`
	TenantID          uint   `gorm:"index"`
	CustomerID        uint   `gorm:"index"`
	ConversationID    uint   `gorm:"index"`
	Platform          string `gorm:"size:50"`
	Direction         string `gorm:"size:20"` // incoming, outgoing
	MessageType       string `gorm:"size:20"` // text, image, video, comment
	Content           string `gorm:"type:text"`
	PlatformMessageID string `gorm:"type:text;index"` // Platform-specific message ID (can be very long for Instagram)

	// AI Analysis
	DetectedIntent string  `gorm:"size:50"` // inquiry, order, payment, complaint, casual, refund, delivery_issue
	DetectedTone   string  `gorm:"size:50"` // positive, neutral, negative, urgent
	Confidence     float64 // 0-1 confidence score

	// Response handling
	ResponseType   string `gorm:"size:20"`   // auto_reply, escalated, manual
	ResponseStatus string `gorm:"size:20"`   // pending, approved, sent, rejected
	SuggestedReply string `gorm:"type:text"` // AI-generated suggested reply
	FinalReply     string `gorm:"type:text"` // Final reply sent to customer
	EscalatedTo    int64  `gorm:"index"`     // Telegram ID of tenant who handled it
	EscalatedAt    *time.Time

	// Metadata
	Metadata  string    `gorm:"type:jsonb"` // Additional platform-specific data
	CreatedAt time.Time `gorm:"autoCreateTime"`

	// Security fields (for payment/financial escalations)
	EncryptedContent string `gorm:"type:text"`                  // Encrypted message content for sensitive escalations
	IsEncrypted      bool   `gorm:"default:false"`              // Flag to indicate if content is encrypted
	SecurityLevel    string `gorm:"size:20;default:'standard'"` // standard, sensitive, payment
}
