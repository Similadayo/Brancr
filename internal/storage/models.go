package storage

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// =======================
//  TENANT / BUSINESS
// =======================

type Tenant struct {
	gorm.Model
	Name               string
	Email              string
	OwnerName          string
	OwnerTelegramID    int64  `gorm:"index"` // identifies the business owner in Telegram
	TelegramBotToken   string // encrypted token, optional in dashboard sign-up
	BotUsername        string `gorm:"index"`
	PlanType           string
	Status             string // active, suspended, pending
	BusinessProfile    BusinessProfile
	PasswordHash       string
	OwnerPhone         string
	OnboardingStep     string `gorm:"default:business_profile"` // business_profile, persona, business_details, social_connect, complete
	OnboardingComplete bool   `gorm:"default:false"`
	Timezone           string `gorm:"size:50;default:'UTC'"` // e.g., "America/New_York", "Europe/London"
	IndustryID         uint   `gorm:"index"`                 // Links to Industry model
	IndustrySettings   string `gorm:"type:jsonb;default:'{}'"` // Industry-specific custom settings
	Metadata           string `gorm:"type:jsonb;default:'{}'"` // Additional tenant settings (webhook URLs, etc.)
	WebhookSecret      string // Secret key for HMAC signature verification
}

func (t *Tenant) BeforeSave(tx *gorm.DB) error {
	if strings.TrimSpace(t.IndustrySettings) == "" {
		t.IndustrySettings = "{}"
	}
	if strings.TrimSpace(t.Metadata) == "" {
		t.Metadata = "{}"
	}
	return nil
}

// BusinessProfile represents the personality and niche of the tenant’s bot
type BusinessProfile struct {
	gorm.Model
	TenantID       uint `gorm:"index"`
	Name           string
	Niche          string
	Tone           string
	Description    string
	MemoryVector   string
	Industry       string
	OperatingHours string `gorm:"type:text"`
	Location       string
	Website        string
	Keywords       string `gorm:"type:text"`
	KnowledgeBase  string `gorm:"type:text"`
}

// BusinessMenuItem represents a product or service offering with optional pricing.
type BusinessMenuItem struct {
	gorm.Model
	TenantID            uint `gorm:"index"`
	Name                string
	Category            string
	Price               string
	NegotiationMode     string  `gorm:"size:20;default:'default'"`
	NegotiationMinPrice float64 `gorm:"default:0"`
	NegotiationMaxPrice float64 `gorm:"default:0"`
	Description         string  `gorm:"type:text"`
	Tags                string  `gorm:"type:text"`
}

// BusinessFAQ captures frequently asked questions and scripted answers.
type BusinessFAQ struct {
	gorm.Model
	TenantID uint   `gorm:"index"`
	Question string `gorm:"type:text"`
	Answer   string `gorm:"type:text"`
}

// =======================
//  INDUSTRY & PRODUCTS
// =======================

// Industry represents a business industry category
type Industry struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"size:100;uniqueIndex"` // e.g., "E-commerce", "Restaurant", "Consultant"
	Category    string `gorm:"size:50"`              // e.g., "retail", "food", "professional_services"
	Description string `gorm:"type:text"`
	HasProducts bool   `gorm:"default:false"` // Whether this industry uses products/menu
	HasMenu     bool   `gorm:"default:false"` // Whether this industry uses menu items
	HasServices bool   `gorm:"default:false"` // Whether this industry uses services
	Settings    string `gorm:"type:jsonb"`    // Industry-specific settings (sales style, negotiation rules, etc.)
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Product represents a product for e-commerce businesses
type Product struct {
	gorm.Model
	TenantID            uint   `gorm:"index"`
	Name                string `gorm:"size:255;index"`
	Description         string `gorm:"type:text"`
	Price               float64
	Currency            string  `gorm:"size:10;default:'NGN'"`
	NegotiationMode     string  `gorm:"size:20;default:'default'"`
	NegotiationMinPrice float64 `gorm:"default:0"`
	NegotiationMaxPrice float64 `gorm:"default:0"`
	Category            string  `gorm:"size:100;index"`
	Images              string  `gorm:"type:jsonb"`                 // JSON array of image URLs
	Variants            string  `gorm:"type:jsonb"`                 // JSON: {"size": ["S", "M", "L"], "color": ["Red", "Blue"]}
	Tags                string  `gorm:"type:jsonb"`                 // JSON array of tags for search
	StockCount          int     `gorm:"default:-1"`                 // -1 = unlimited, 0+ = actual stock
	Availability        string  `gorm:"size:20;default:'in_stock'"` // in_stock, out_of_stock, low_stock
	IsActive            bool    `gorm:"default:true"`
	SKU                 string  `gorm:"size:100;index"` // Stock Keeping Unit (optional)
}

// MenuItem represents a menu item for restaurant businesses
type MenuItem struct {
	gorm.Model
	TenantID            uint   `gorm:"index"`
	Name                string `gorm:"size:255;index"`
	Description         string `gorm:"type:text"`
	Price               float64
	Currency            string  `gorm:"size:10;default:'NGN'"`
	NegotiationMode     string  `gorm:"size:20;default:'default'"`
	NegotiationMinPrice float64 `gorm:"default:0"`
	NegotiationMaxPrice float64 `gorm:"default:0"`
	Category            string  `gorm:"size:100;index"` // appetizer, main, dessert, drink, etc.
	Images              string  `gorm:"type:jsonb"`     // JSON array of image URLs
	DietaryInfo         string  `gorm:"type:jsonb"`     // JSON: ["vegetarian", "vegan", "gluten-free"]
	PreparationTime     int     // Minutes
	Availability        string  `gorm:"size:20;default:'available'"` // available, unavailable, limited
	IsActive            bool    `gorm:"default:true"`
	SpiceLevel          string  `gorm:"size:20"` // mild, medium, hot, etc. (optional)
}

// Service represents a service for consultant/agency businesses
type Service struct {
	gorm.Model
	TenantID            uint    `gorm:"index"`
	Name                string  `gorm:"size:255;index"`
	Description         string  `gorm:"type:text"`
	Pricing             string  `gorm:"type:jsonb"` // JSON: {"type": "fixed", "amount": 50000} or {"type": "hourly", "rate": 5000}
	NegotiationMode     string  `gorm:"size:20;default:'default'"`
	NegotiationMinPrice float64 `gorm:"default:0"`
	NegotiationMaxPrice float64 `gorm:"default:0"`
	Packages            string  `gorm:"type:jsonb"` // JSON array of package options
	Duration            string  `gorm:"size:50"`    // e.g., "1 hour", "2 weeks", "1 month"
	Deliverables        string  `gorm:"type:text"`  // What the customer gets
	IsActive            bool    `gorm:"default:true"`
	Category            string  `gorm:"size:100;index"` // consultation, project, retainer, etc.
}

// ADMIN USERS (SYSTEM)

type AdminUser struct {
	gorm.Model
	Email        string `gorm:"uniqueIndex"`
	PasswordHash string
	Role         string // SuperAdmin, Support
	Active       bool   `gorm:"default:true"`
	LastLogin    *time.Time
}

//  CUSTOMERS

type Customer struct {
	ID            uint      `gorm:"primaryKey"`
	TenantID      uint      `gorm:"index:idx_customer_unique,unique"`
	Channel       string    `gorm:"size:50;index:idx_customer_unique,unique"`  // telegram, whatsapp, instagram, facebook
	PlatformID    string    `gorm:"size:100;index:idx_customer_unique,unique"` // platform-specific ID (e.g. Telegram ID, WA number)
	DisplayName   string    `gorm:"size:120"`
	Username      string    `gorm:"size:120"`
	ProfilePicURL string    `gorm:"size:255"`
	LastMessageAt time.Time `gorm:"autoUpdateTime"`
	CreatedAt     time.Time
}

// Order represents a customer order
type Order struct {
	ID                  uint   `gorm:"primaryKey"`
	TenantID            uint   `gorm:"index"`
	CustomerID          uint   `gorm:"index"`
	OrderNumber         string `gorm:"uniqueIndex;size:50"` // "ORD-2024-001234"
	PaymentReference    string `gorm:"uniqueIndex;size:50"` // "BRNCR-123-20241204-ABC123"
	Status              string `gorm:"size:50;index"`       // pending, confirmed, processing, completed, cancelled
	TotalAmount         float64
	Currency            string `gorm:"size:10;default:'NGN'"`
	Items               string `gorm:"type:jsonb"` // JSON array of OrderItem
	CustomerName        string `gorm:"size:255"`
	CustomerPhone       string `gorm:"size:50"`
	CustomerEmail       string `gorm:"size:255"`
	Platform            string `gorm:"size:50"`   // whatsapp, instagram, telegram, facebook
	PaymentInstructions string `gorm:"type:text"` // Bank details, etc. sent to customer
	Notes               string `gorm:"type:text"` // Tenant notes
	CreatedAt           time.Time
	ConfirmedAt         *time.Time // When tenant confirmed payment
	CompletedAt         *time.Time // When order is fulfilled
}

// Payment represents a payment transaction
type Payment struct {
	ID                 uint   `gorm:"primaryKey"`
	TenantID           uint   `gorm:"index"`
	OrderID            uint   `gorm:"index"`
	CustomerID         uint   `gorm:"index"`
	PaymentReference   string `gorm:"uniqueIndex;size:50"` // Links to Order.PaymentReference
	Amount             float64
	Currency           string `gorm:"size:10;default:'NGN'"`
	PaymentMethod      string `gorm:"size:50"`       // bank_transfer, cash, mobile_money, card, other
	Status             string `gorm:"size:50;index"` // pending, verified, confirmed, failed, refunded
	VerificationStatus string `gorm:"size:50;index"` // unverified, tenant_verified, disputed, auto_verified
	ClaimedByCustomer  bool   `gorm:"default:false"`
	ClaimedAt          *time.Time
	VerifiedBy         *int64 `gorm:"index"` // Tenant's Telegram ID who verified
	VerifiedAt         *time.Time
	ReceiptID          string     `gorm:"uniqueIndex;size:50"`
	ReceiptURL         string     `gorm:"type:text"`
	PortalToken        string     `gorm:"uniqueIndex;size:100"` // Unique token for customer portal access
	PortalURL          string     `gorm:"type:text"`            // Full portal URL with token
	TokenExpiresAt     *time.Time `gorm:"index"`                // Token expiration date (optional)
	Metadata           string     `gorm:"type:jsonb"`           // Additional payment data
	CreatedAt          time.Time
	CompletedAt        *time.Time // When payment was fully processed/confirmed
}

// OrderItem represents a single item in an order (used in Order.Items JSONB)
type OrderItem struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	TotalPrice  float64 `json:"total_price"`
}

// TenantPaymentAccount stores tenant's payment account details
type TenantPaymentAccount struct {
	gorm.Model
	TenantID            uint   `gorm:"index"`
	AccountType         string `gorm:"size:50"` // bank, mobile_money, cash
	BankName            string `gorm:"size:100"`
	AccountNumber       string `gorm:"type:text"` // Encrypted
	AccountName         string `gorm:"size:255"`
	MobileMoneyProvider string `gorm:"size:100"` // For mobile money (MTN, Airtel, etc.)
	PhoneNumber         string `gorm:"size:50"`  // For mobile money
	IsDefault           bool   `gorm:"default:false"`
	IsActive            bool   `gorm:"default:true"`
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// SalesConversation tracks sales conversation state
type SalesConversation struct {
	gorm.Model
	ConversationID     uint    `gorm:"index"` // Links to Conversation
	TenantID           uint    `gorm:"index"`
	CustomerID         uint    `gorm:"index"`
	Stage              string  `gorm:"size:50"`    // greeting, product_inquiry, negotiation, commitment_confirmation, payment_instructions
	CommitmentLevel    string  `gorm:"size:50"`    // exploring, interested, considering, committed, payment_ready
	ItemsDiscussed     string  `gorm:"type:jsonb"` // JSON array of products/services discussed
	PriceDiscussed     float64 // Price discussed (may change during negotiation)
	NegotiationCount   int     `gorm:"default:0"`  // Number of negotiation rounds
	ObjectionsRaised   string  `gorm:"type:jsonb"` // JSON array of objections
	LastIntent         string  `gorm:"size:50"`    // Last detected intent
	PendingOrderDraft  string  `gorm:"type:jsonb"` // Draft order JSON awaiting tenant confirmation
	PendingOrderStatus string  `gorm:"size:50"`    // pending_confirmation, approved, declined, none
	PendingOrderSetAt  *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// ConversationUserState tracks per-user read/unread state for a conversation
type ConversationUserState struct {
	gorm.Model
	TenantID       uint `gorm:"index"`
	ConversationID uint `gorm:"index"`
	UserID         uint `gorm:"index"`
	LastReadAt     *time.Time
	UnreadCount    int `gorm:"default:0"`
}

// Notification tracks dashboard notifications for a tenant
type Notification struct {
	gorm.Model
	TenantID       uint   `gorm:"index"`
	ConversationID uint   `gorm:"index"`
	InteractionID  uint   `gorm:"index"`
	Type           string `gorm:"size:50;index"` // message, escalation, payment, system
	Title          string `gorm:"size:255"`
	Body           string `gorm:"type:text"`
	Severity       string `gorm:"size:20"` // info, warning, critical
	ReadAt         *time.Time
	Metadata       string `gorm:"type:jsonb"`
}

// NegotiationHistory tracks negotiation attempts
type NegotiationHistory struct {
	gorm.Model
	SalesConversationID uint    `gorm:"index"`   // Links to SalesConversation
	Type                string  `gorm:"size:50"` // price, quantity, terms
	CustomerOffer       float64 // What customer offered
	CounterOffer        float64 // What AI countered with
	FinalAgreement      float64 // Final agreed price (if any)
	Status              string  `gorm:"size:50"` // ongoing, agreed, rejected
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// TenantNotificationSettings stores tenant-configurable notification preferences.
// This is used by order/payment notification delivery (email + webhooks).
type TenantNotificationSettings struct {
	gorm.Model
	TenantID uint `gorm:"uniqueIndex"`

	// Email notifications
	EmailNotificationsEnabled bool `gorm:"default:true"`
	EmailOrderStatusChanges   bool `gorm:"default:true"`
	EmailPaymentConfirmations bool `gorm:"default:true"`
	EmailReceiptGenerated     bool `gorm:"default:true"`

	// Webhooks
	WebhookEnabled bool   `gorm:"default:false"`
	WebhookURL     string `gorm:"type:text"`
	WebhookEvents  string `gorm:"type:jsonb"` // JSON array of event types
	WebhookSecret  string `gorm:"size:255"`   // For webhook signing

	LastTestAt     *time.Time
	LastTestStatus string `gorm:"size:50"` // success, failed
}

// AdminAuditLog records admin actions for compliance and traceability.
// Content access should be logged explicitly in addition to request logs.
type AdminAuditLog struct {
	gorm.Model
	AdminEmail string `gorm:"size:255;index"`
	AdminRole  string `gorm:"size:50;index"`
	Action     string `gorm:"size:100;index"`
	Method     string `gorm:"size:10"`
	Path       string `gorm:"type:text"`
	TenantID   *uint  `gorm:"index"`
	Resource   string `gorm:"size:50;index"`  // tenant, integration, post, message, billing, system
	ResourceID string `gorm:"size:100;index"` // string for flexibility
	Success    bool   `gorm:"default:true"`
	StatusCode int
	IP         string `gorm:"size:64"`
	UserAgent  string `gorm:"type:text"`
	Metadata   string `gorm:"type:jsonb"` // optional JSON
}

// ACTIVITY & LOGGING

type EventLog struct {
	gorm.Model
	TenantID   uint
	EventType  string // payment, delivery, appointment, other
	SenderName string
	Message    string
	Forwarded  bool      `gorm:"default:false"`
	CreatedAt  time.Time `gorm:"autoCreateTime"`
}

type LogEntry struct {
	gorm.Model
	Level     string    `gorm:"index"` // INFO, WARN, ERROR
	Source    string    // "monitor", "webhook", "scheduler", etc.
	Message   string    `gorm:"type:text"`
	Meta      string    `gorm:"type:text"` // JSON-encoded details
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

//  MEDIA

type MediaItem struct {
	gorm.Model
	TenantID     uint
	SessionID    string `gorm:"index"` // Telegram MediaGroupID or session
	URL          string
	ResourceType string // image, video
	FileID       string
	Status       string // pending, processed
}

type MediaAsset struct {
	gorm.Model
	TenantID     uint
	Type         string // image, video, carousel
	Name         string
	CarouselName string
	Caption      string `gorm:"type:text"`
	URLs         string `gorm:"type:text"` // JSON array
	Status       string // uploaded, scheduled, posted
	ScheduledAt  *time.Time
	Platforms    string // JSON array
	Tags         string `gorm:"type:text"` // JSON array of tags
	Campaign     string // Optional campaign name
	BulkUploadID string `gorm:"index"`      // Links assets from same bulk upload
	Metadata     string `gorm:"type:jsonb"` // Platform-specific metadata (e.g., TikTok posting options)
}

// POST SCHEDULING

type ScheduledPost struct {
	gorm.Model
	TenantID       uint `gorm:"index"`
	Name           string
	Caption        string     `gorm:"type:text"`
	MediaAssetIDs  string     `gorm:"type:text"` // e.g. [1,2,3]
	Platforms      string     `gorm:"type:text"` // e.g. ["instagram","facebook"]
	ScheduledAt    *time.Time `gorm:"index"`
	Status         string     // scheduled, posting, posted, failed, cancelled
	Attempts       int
	LastError      string `gorm:"type:text"`
	PostedAt       *time.Time
	Timezone       string     `gorm:"size:50"` // Tenant's timezone when scheduled
	OptimalTime    *time.Time // AI-suggested optimal posting time
	Priority       int        `gorm:"default:0"`     // 0=normal, 1=high, 2=urgent
	PostingWindow  string     `gorm:"type:text"`     // JSON: {"start": "09:00", "end": "17:00"}
	EnhanceCaption bool       `gorm:"default:false"` // If true, enhance existing caption with AI (Mode 3); if false, use caption as-is (Mode 2)
}

// SOCIAL ACCOUNTS

type SocialAccount struct {
	gorm.Model
	TenantID           uint
	Platform           string // instagram, facebook, whatsapp, tiktok, youtube
	AccessToken        string `gorm:"type:text"`
	RefreshToken       string `gorm:"type:text"`
	ExpiresAt          *time.Time
	Username           string
	PageID             string // For Facebook: page_id, for Instagram: instagram_business_account.id, for WhatsApp: phone_number_id
	ExternalID         string `gorm:"index"` // Platform-specific external ID used for webhook routing (page_id, instagram_business_account.id, phone_number_id)
	Connected          bool
	Mode               string // manual, sandbox, oauth2, provider_owned
	LoginMethod        string `gorm:"size:50"`    // facebook_login, instagram_login (for Instagram: determines which OAuth flow was used)
	WebhookVerifyToken string `gorm:"size:100"`   // Verification token for webhook setup (Meta requires this)
	Metadata           string `gorm:"type:jsonb"` // Platform-specific metadata (e.g., TikTok authorized scopes, posting options, login method)
	CreatedAt          time.Time
}

// WhatsAppPhoneNumber represents a phone number in Brancr's provider-owned WABA
type WhatsAppPhoneNumber struct {
	gorm.Model
	PhoneNumberID string `gorm:"uniqueIndex"` // Meta's phone_number_id (used for webhook routing)
	PhoneNumber   string // Display format: +1234567890
	WABAID        string `gorm:"index"` // Which WABA this number belongs to
	TenantID      *uint  `gorm:"index"` // NULL = available, assigned to tenant if set
	DisplayName   string // Tenant's business name (when assigned)
	VerifiedName  string // From Meta API
	QualityRating string // Meta quality rating
	Status        string // available, assigned, suspended
	AssignedAt    *time.Time
}

// WhatsAppPhoneNumberRequest represents a tenant's request to add their own phone number
type WhatsAppPhoneNumberRequest struct {
	gorm.Model
	TenantID         uint       `gorm:"index"` // Tenant who requested the number
	PhoneNumber      string     // Display format: +1234567890
	DisplayName      string     // Tenant's business name
	Status           string     // pending, added_to_meta, verification_code_sent, verified, rejected
	PhoneNumberID    string     // Meta's phone_number_id (filled after admin adds to Meta)
	VerificationCode string     // Code provided by tenant (filled when tenant provides code)
	RejectedReason   string     // Reason if rejected
	ProcessedAt      *time.Time // When admin processed the request
}

// WhatsAppApp represents a Gupshup Partner app for a tenant (multi-tenant WhatsApp)
type WhatsAppApp struct {
	gorm.Model
	TenantID           uint   `gorm:"index;not null"`
	GupshupAppID       string `gorm:"type:text;not null;index"` // UUID from Gupshup redirect callback
	GupshupAppToken    string `gorm:"type:text;not null"`       // Encrypted token from callback
	WABAID             string `gorm:"type:text"`
	BusinessID         string `gorm:"type:text"`
	PhoneNumberID      string `gorm:"type:text"`
	PhoneNumber        string `gorm:"type:text"`                            // From redirect callback
	Status             string `gorm:"size:50;default:'pending_onboarding'"` // pending_onboarding, pending_verification, live, failed
	Mode               string `gorm:"size:50;default:'gupshup_partner'"`
	SubscriptionID     string `gorm:"type:text"`     // Subscription ID from Subscription API
	SubscriptionActive bool   `gorm:"default:false"` // Whether subscription is active
	WebhookURL         string `gorm:"type:text"`     // Webhook URL configured for this app
	DefaultTemplateID  string `gorm:"type:text"`     // Default text template ID for AI replies (template with variable parameter)
}

// GupshupWebhookEvent stores incoming webhook events from Gupshup
type GupshupWebhookEvent struct {
	gorm.Model
	TenantID    uint   `gorm:"index"`
	AppID       string `gorm:"index"`
	EventType   string // message, delivery_status, read_status
	Payload     string `gorm:"type:text"` // Full JSON payload
	Processed   bool   `gorm:"default:false"`
	ProcessedAt *time.Time
	ReceivedAt  time.Time
}

// Persona — each tenant’s bot personality profile
type Persona struct {
	TenantID   uint   `gorm:"primaryKey"`
	BotName    string `gorm:"size:50"`  // e.g. "Luna", "Aiden", "Mimi"
	Tone       string `gorm:"size:120"` // "friendly, witty, warm"
	Language   string `gorm:"size:30"`  // "en", "en-NG", "fr"
	Humor      bool   `gorm:"default:true"`
	StyleNotes string `gorm:"type:text"` // Additional style hints
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// MemoryItem — contextual memory vectors or text
type MemoryItem struct {
	ID          uint      `gorm:"primaryKey"`
	TenantID    uint      `gorm:"index"`
	CustomerID  uint      `gorm:"index"`
	Text        string    `gorm:"type:text"`
	Embedding   []float32 `gorm:"type:float[]"`
	Category    string    `gorm:"size:50"`
	ContextHash string    `gorm:"uniqueIndex"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
}

type IntentCatalog struct {
	ID        uint `gorm:"primaryKey"`
	TenantID  uint
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type TenantPolicy struct {
	ID                 uint `gorm:"primaryKey"`
	TenantID           uint
	AutoReplyThreshold float64 // 0-1
	EscalateEmotions   string  // JSON array of labels
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Escalation struct {
	ID         uint `gorm:"primaryKey"`
	TenantID   uint
	UserID     string
	Platform   string
	Message    string `gorm:"type:text"`
	DraftReply string `gorm:"type:text"`
	Status     string // pending/approved/sent
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// PostLog tracks detailed posting activity for analytics
type PostLog struct {
	ID               uint `gorm:"primaryKey"`
	TenantID         uint `gorm:"index"`
	ScheduledPostID  uint `gorm:"index"`
	Platform         string
	Action           string `gorm:"size:50"` // optimizing, posting, posted, failed, retrying
	OriginalCaption  string `gorm:"type:text"`
	OptimizedCaption string `gorm:"type:text"`
	Status           string `gorm:"size:20"` // success, failed, partial
	ErrorMessage     string `gorm:"type:text"`
	Improvements     string `gorm:"type:text"`  // AI improvements made
	Metadata         string `gorm:"type:jsonb"` // Additional data (hashtags, timing, etc.)
	CreatedAt        time.Time
}

// BulkUploadSession tracks bulk media uploads
type BulkUploadSession struct {
	gorm.Model
	TenantID         uint   `gorm:"index"`
	SessionID        string `gorm:"uniqueIndex"` // Unique session identifier
	TotalItems       int
	ProcessedItems   int
	Status           string // pending, processing, completed, failed
	SplitStrategy    string // equal, by_count, by_date, ai_organized
	CarouselCount    int    // Number of carousels to create
	ItemsPerCarousel int
	ScheduleStrategy string // spread, custom, optimal
	Metadata         string `gorm:"type:text"` // JSON metadata
}

// RecurringTemplate for recurring posts
type RecurringTemplate struct {
	gorm.Model
	TenantID        uint `gorm:"index"`
	Name            string
	Frequency       string // daily, weekly, monthly, custom
	Schedule        string `gorm:"type:text"` // JSON: {"days": [1,3,5], "time": "10:00", "timezone": "America/New_York"}
	MediaTemplate   string `gorm:"type:text"` // Template for media selection
	CaptionTemplate string `gorm:"type:text"`
	Platforms       string `gorm:"type:text"` // JSON array
	Active          bool   `gorm:"default:true"`
	NextRun         *time.Time
	LastRun         *time.Time
}

// ContentCalendar entry
type ContentCalendar struct {
	gorm.Model
	TenantID uint      `gorm:"index"`
	Date     time.Time `gorm:"index"`
	Posts    string    `gorm:"type:text"` // JSON array of ScheduledPost IDs
	Theme    string    // Optional theme for the day
	Notes    string    `gorm:"type:text"`
	Status   string    // planned, scheduled, posted, cancelled
}

// TenantQuota tracks resource usage and limits for tenants
type TenantQuota struct {
	gorm.Model
	TenantID uint `gorm:"uniqueIndex"` // One quota per tenant

	// Limits (0 = unlimited)
	AICallsLimit   int `gorm:"default:100"`  // Daily AI API calls limit
	PostsLimit     int `gorm:"default:50"`   // Daily posts limit
	StorageLimitMB int `gorm:"default:1000"` // Storage limit in MB
	APICallsLimit  int `gorm:"default:1000"` // Daily API calls limit

	// Current usage (reset daily)
	AICallsUsed   int `gorm:"default:0"`
	PostsUsed     int `gorm:"default:0"`
	StorageUsedMB int `gorm:"default:0"`
	APICallsUsed  int `gorm:"default:0"`

	// Reset tracking
	LastResetAt    time.Time `gorm:"default:CURRENT_TIMESTAMP"`
	ResetFrequency string    `gorm:"default:'daily'"` // daily, weekly, monthly

	// Plan type (free, pro, enterprise)
	PlanType string `gorm:"size:50;default:'free'"`
}
