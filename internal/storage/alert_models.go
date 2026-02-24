package storage

import (
	"time"

	"github.com/google/uuid"
)

// TenantAlert represents an alert sent from Brancr to a tenant
type TenantAlert struct {
	Base
	TenantID uuid.UUID `gorm:"index"`
	Tenant   Tenant    `gorm:"foreignKey:TenantID"`
	Type     string    `gorm:"size:50;index"` // system, billing, quota, security, maintenance, feature
	Severity string    `gorm:"size:20;index"` // info, warning, critical
	Title    string    `gorm:"size:255"`
	Message  string    `gorm:"type:text"`

	// Delivery channels
	SentEmail    bool `gorm:"default:false"`
	SentTelegram bool `gorm:"default:false"`
	SentInApp    bool `gorm:"default:false"`

	// Delivery tracking
	EmailSentAt    *time.Time
	TelegramSentAt *time.Time
	InAppCreatedAt *time.Time

	// Read tracking
	ReadAt *time.Time

	// Action tracking
	ActionURL   string `gorm:"type:text"` // Optional CTA link
	ActionLabel string `gorm:"size:100"`

	Metadata string `gorm:"type:jsonb;default:'{}'"`
}

// NewsletterSubscription tracks tenant newsletter subscriptions
type NewsletterSubscription struct {
	Base
	TenantID         uuid.UUID `gorm:"uniqueIndex"` // One subscription per tenant
	Tenant           Tenant    `gorm:"foreignKey:TenantID"`
	Email            string    `gorm:"size:255;index"`
	Subscribed       bool      `gorm:"default:true"`
	SubscribedAt     time.Time
	UnsubscribedAt   *time.Time
	UnsubscribeToken string `gorm:"uniqueIndex;size:64"` // For unsubscribe links

	// Preferences
	Categories string `gorm:"type:text"` // JSON array of categories they're interested in
}

// Newsletter represents a newsletter/broadcast email sent to all subscribers
type Newsletter struct {
	Base
	Title      string `gorm:"size:255"`
	Subject    string `gorm:"size:255"`  // Email subject line
	Content    string `gorm:"type:text"` // HTML content
	Categories string `gorm:"type:text"` // JSON array of categories (optional filter)

	// Status
	Status      string `gorm:"size:50;index"` // draft, scheduled, sending, sent, failed
	ScheduledAt *time.Time
	SentAt      *time.Time

	// Stats
	TotalRecipients int `gorm:"default:0"`
	SentCount       int `gorm:"default:0"`
	DeliveredCount  int `gorm:"default:0"`
	OpenedCount     int `gorm:"default:0"`
	ClickedCount    int `gorm:"default:0"`
	FailedCount     int `gorm:"default:0"`

	// Metadata
	CreatedBy string `gorm:"size:255"` // Admin email who created it
	Metadata  string `gorm:"type:jsonb;default:'{}'"`
}

// NewsletterDelivery tracks individual newsletter deliveries
type NewsletterDelivery struct {
	Base
	NewsletterID uuid.UUID  `gorm:"index"`
	Newsletter   Newsletter `gorm:"foreignKey:NewsletterID"`
	TenantID     uuid.UUID  `gorm:"index"`
	Tenant       Tenant     `gorm:"foreignKey:TenantID"`
	Email        string     `gorm:"size:255;index"`

	Status      string `gorm:"size:50;index"` // pending, sent, delivered, opened, clicked, failed
	SentAt      *time.Time
	DeliveredAt *time.Time
	OpenedAt    *time.Time
	ClickedAt   *time.Time
	FailedAt    *time.Time
	Error       string `gorm:"type:text"`

	// Tracking
	OpenTrackingToken  string `gorm:"uniqueIndex;size:64"`
	ClickTrackingToken string `gorm:"uniqueIndex;size:64"`

	Metadata string `gorm:"type:jsonb;default:'{}'"`
}
