package storage

import (
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
	IndustrySettings   string `gorm:"type:jsonb"`            // Industry-specific custom settings
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
	TenantID    uint `gorm:"index"`
	Name        string
	Category    string
	Price       string
	Description string `gorm:"type:text"`
	Tags        string `gorm:"type:text"`
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
	TenantID     uint   `gorm:"index"`
	Name         string `gorm:"size:255;index"`
	Description  string `gorm:"type:text"`
	Price        float64
	Currency     string `gorm:"size:10;default:'NGN'"`
	Category     string `gorm:"size:100;index"`
	Images       string `gorm:"type:jsonb"`                 // JSON array of image URLs
	Variants     string `gorm:"type:jsonb"`                 // JSON: {"size": ["S", "M", "L"], "color": ["Red", "Blue"]}
	Tags         string `gorm:"type:jsonb"`                 // JSON array of tags for search
	StockCount   int    `gorm:"default:-1"`                 // -1 = unlimited, 0+ = actual stock
	Availability string `gorm:"size:20;default:'in_stock'"` // in_stock, out_of_stock, low_stock
	IsActive     bool   `gorm:"default:true"`
	SKU          string `gorm:"size:100;index"` // Stock Keeping Unit (optional)
}

// MenuItem represents a menu item for restaurant businesses
type MenuItem struct {
	gorm.Model
	TenantID        uint   `gorm:"index"`
	Name            string `gorm:"size:255;index"`
	Description     string `gorm:"type:text"`
	Price           float64
	Currency        string `gorm:"size:10;default:'NGN'"`
	Category        string `gorm:"size:100;index"` // appetizer, main, dessert, drink, etc.
	Images          string `gorm:"type:jsonb"`     // JSON array of image URLs
	DietaryInfo     string `gorm:"type:jsonb"`     // JSON: ["vegetarian", "vegan", "gluten-free"]
	PreparationTime int    // Minutes
	Availability    string `gorm:"size:20;default:'available'"` // available, unavailable, limited
	IsActive        bool   `gorm:"default:true"`
	SpiceLevel      string `gorm:"size:20"` // mild, medium, hot, etc. (optional)
}

// Service represents a service for consultant/agency businesses
type Service struct {
	gorm.Model
	TenantID     uint   `gorm:"index"`
	Name         string `gorm:"size:255;index"`
	Description  string `gorm:"type:text"`
	Pricing      string `gorm:"type:jsonb"` // JSON: {"type": "fixed", "amount": 50000} or {"type": "hourly", "rate": 5000}
	Packages     string `gorm:"type:jsonb"` // JSON array of package options
	Duration     string `gorm:"size:50"`    // e.g., "1 hour", "2 weeks", "1 month"
	Deliverables string `gorm:"type:text"`  // What the customer gets
	IsActive     bool   `gorm:"default:true"`
	Category     string `gorm:"size:100;index"` // consultation, project, retainer, etc.
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
	TenantID      uint `gorm:"index"`
	Name          string
	Caption       string     `gorm:"type:text"`
	MediaAssetIDs string     `gorm:"type:text"` // e.g. [1,2,3]
	Platforms     string     `gorm:"type:text"` // e.g. ["instagram","facebook"]
	ScheduledAt   *time.Time `gorm:"index"`
	Status        string     // scheduled, posting, posted, failed, cancelled
	Attempts      int
	LastError     string `gorm:"type:text"`
	PostedAt      *time.Time
	Timezone      string     `gorm:"size:50"` // Tenant's timezone when scheduled
	OptimalTime   *time.Time // AI-suggested optimal posting time
	Priority      int        `gorm:"default:0"` // 0=normal, 1=high, 2=urgent
	PostingWindow string     `gorm:"type:text"` // JSON: {"start": "09:00", "end": "17:00"}
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
	WebhookVerifyToken string `gorm:"size:100"` // Verification token for webhook setup (Meta requires this)
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
