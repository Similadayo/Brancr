package storage

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Base contains common columns for all tables, using UUID as primary key
type Base struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// =======================
//  TENANT / BUSINESS
// =======================

type Tenant struct {
	Base
	Name             string
	Email            string
	OwnerName        string
	OwnerTelegramID  int64  `gorm:"index"` // identifies the business owner in Telegram
	TelegramBotToken string // encrypted token, optional in dashboard sign-up
	BotUsername      string `gorm:"index"`
	PlanType         string
	Status           string // active, suspended, pending
	Blocked          bool   `gorm:"default:false"` // Admin-blocked (cannot access platform)
	Suspended        bool   `gorm:"default:false"` // Admin-suspended (temporary restriction)

	// Telegram deep link connection
	TelegramConnectToken   string     `gorm:"size:64;index"` // One-time token for deep link connection
	TelegramConnectExpires *time.Time // Token expiry (15 min)

	// Subscription fields
	SubscriptionExpiresAt         *time.Time `gorm:"index"` // When subscription expires
	SubscriptionLastPaymentAt     *time.Time // Last payment date
	SubscriptionLastPaymentAmount *float64   // Last payment amount
	TrialEndsAt                   *time.Time `gorm:"index"` // When trial period ends (if applicable)

	BusinessProfile BusinessProfile
	PasswordHash    string
	EmailVerified   bool       `gorm:"default:false"` // Whether email has been verified
	EmailVerifiedAt *time.Time // When email was verified
	OwnerPhone      string

	// KYB (Know Your Business) fields
	KYBStatus            string `gorm:"size:50;default:'not_submitted'"` // not_submitted, pending, verified, rejected
	KYBData              string `gorm:"type:jsonb;default:'{}'"`         // Submitted KYB data (documents, form fields)
	KYBSubmittedAt       *time.Time
	KYBVerifiedAt        *time.Time
	KYBVerifiedByAdminID *uuid.UUID

	OnboardingStep     string         `gorm:"default:business_profile"` // business_profile, persona, business_details, social_connect, complete
	OnboardingComplete bool           `gorm:"default:false"`
	Timezone           string         `gorm:"size:50;default:'UTC'"`                     // e.g., "America/New_York", "Europe/London"
	AIHumanMode        string         `gorm:"size:20;default:'ai'" json:"ai_human_mode"` // 'ai' or 'human'
	IndustryID         uuid.UUID      `gorm:"index"`                                     // Links to Industry model
	IndustrySettings   string         `gorm:"type:jsonb;default:'{}'"`                   // Industry-specific custom settings
	Metadata           string         `gorm:"type:jsonb;default:'{}'"`                   // Additional tenant settings (webhook URLs, etc.)
	WebhookSecret      string         // Secret key for HMAC signature verification
	AdminTags          pq.StringArray `gorm:"type:text[]"` // Admin-managed tags (at_risk, likely_to_pay, needs_followup, high_cost, misconfigured)
}

func (t *Tenant) BeforeSave(tx *gorm.DB) error {
	if strings.TrimSpace(t.IndustrySettings) == "" {
		t.IndustrySettings = "{}"
	}
	if strings.TrimSpace(t.Metadata) == "" {
		t.Metadata = "{}"
	}
	if t.AdminTags == nil {
		t.AdminTags = pq.StringArray{}
	}
	return nil
}

// Plan represents a subscription plan with pricing and limits
type Plan struct {
	ID                 uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Code               string    `gorm:"size:50;uniqueIndex"`
	Name               string    `gorm:"size:100"`
	Description        string    `gorm:"type:text"`
	Price              float64   `gorm:"default:0"`
	Currency           string    `gorm:"size:10;default:'USD'"`
	BillingPeriod      string    `gorm:"size:20;default:'monthly'"`
	ConversationsLimit int       `gorm:"default:500"`
	SeatsLimit         int       `gorm:"default:2"`
	AIRequestsLimit    int       `gorm:"default:1000"`
	StorageLimitMB     int       `gorm:"default:100"`
	Features           string    `gorm:"type:jsonb;default:'[]'"`
	IsActive           bool      `gorm:"default:true"`
	IsTrial            bool      `gorm:"default:false"`
	TrialDays          int       `gorm:"default:0"`
	SortOrder          int       `gorm:"default:0"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// BusinessProfile represents the personality and niche of the tenant’s bot
type BusinessProfile struct {
	Base
	TenantID       uuid.UUID `gorm:"index"`
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
	DeliveryConfig string `gorm:"type:jsonb;default:'{}'"` // JSON: DeliveryConfig struct
}

type DeliveryConfig struct {
	DeliveryEnabled    bool           `json:"delivery_enabled"`
	PickupEnabled      bool           `json:"pickup_enabled"`
	PaymentMethods     []string       `json:"payment_methods"` // ["pod", "bank_transfer", "card"]
	PickupAddress      string         `json:"pickup_address"`
	PickupInstructions string         `json:"pickup_instructions"`
	PickupTemplate     string         `json:"pickup_template"` // Optional custom pickup message template
	DeliveryAreas      []DeliveryArea `json:"delivery_areas"`
	DefaultDeliveryFee float64        `json:"default_delivery_fee"` // Fallback fee when address doesn't match any area
}

type DeliveryArea struct {
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

// BusinessMenuItem represents a product or service offering with optional pricing.
// BusinessMenuItem is deprecated - use Product, MenuItem, or Service instead based on industry
// This model is kept temporarily for data migration purposes only
// DO NOT USE in new code - it will be removed after migration
type BusinessMenuItem struct {
	Base
	TenantID            uuid.UUID `gorm:"index"`
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
	Base
	TenantID uuid.UUID `gorm:"index"`
	Question string    `gorm:"type:text"`
	Answer   string    `gorm:"type:text"`
}

// Draft represents user/tenant drafts saved to persist progress across devices
// Content is stored as JSONB to allow structured draft state per page/flow.
type Draft struct {
	Base
	TenantID uuid.UUID  `gorm:"index"`
	OwnerID  *uuid.UUID `gorm:"index"`          // optional: user ID or null for tenant-wide drafts
	Key      string     `gorm:"size:255;index"` // logical identifier (e.g., "compose.post", "onboarding.form")
	Content  string     `gorm:"type:jsonb"`
	Metadata string     `gorm:"type:jsonb;default:'{}'"`
}

func (d *Draft) BeforeSave(tx *gorm.DB) error {
	// Ensure Metadata is always valid JSON (empty string becomes "{}")
	if strings.TrimSpace(d.Metadata) == "" {
		d.Metadata = "{}"
	} else {
		// Validate that it's valid JSON by attempting to unmarshal
		var test map[string]interface{}
		if err := json.Unmarshal([]byte(d.Metadata), &test); err != nil {
			// If invalid, default to empty object
			d.Metadata = "{}"
		}
	}

	// Ensure Content is also valid JSON if provided
	if strings.TrimSpace(d.Content) == "" {
		d.Content = "{}"
	} else {
		var test map[string]interface{}
		if err := json.Unmarshal([]byte(d.Content), &test); err != nil {
			// If invalid, default to empty object
			d.Content = "{}"
		}
	}

	return nil
}

// =======================
//  INDUSTRY & PRODUCTS
// =======================

// Industry represents a business industry category
type Industry struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Name        string    `gorm:"size:100;uniqueIndex"` // e.g., "E-commerce", "Food & Beverage", "Consultant"
	Category    string    `gorm:"size:50"`              // e.g., "retail", "food", "professional_services"
	Description string    `gorm:"type:text"`
	HasProducts bool      `gorm:"default:false"` // Whether this industry uses products/menu
	HasMenu     bool      `gorm:"default:false"` // Whether this industry uses menu items
	HasServices bool      `gorm:"default:false"` // Whether this industry uses services
	Settings    string    `gorm:"type:jsonb"`    // Industry-specific settings (sales style, negotiation rules, etc.)
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IndustryTemplate stores pre-built message templates for each industry
// These are copied to a tenant when they apply an industry pack during onboarding
type IndustryTemplate struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	IndustryID uuid.UUID `gorm:"index"`
	Name       string    `gorm:"size:100"`  // e.g., "Price Inquiry Response"
	Category   string    `gorm:"size:50"`   // greeting, inquiry, support, escalation
	Trigger    string    `gorm:"type:text"` // Keywords or intent that triggers this template
	Message    string    `gorm:"type:text"` // The actual template message with placeholders
	IsActive   bool      `gorm:"default:true"`
	SortOrder  int       `gorm:"default:0"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// IndustryDefaults stores default settings to apply when a tenant selects an industry
type IndustryDefaults struct {
	ID                 uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	IndustryID         uuid.UUID `gorm:"uniqueIndex"`
	DefaultTone        string    `gorm:"size:120"`                    // e.g., "Friendly, professional"
	DefaultStyle       string    `gorm:"type:text"`                   // Style notes for AI
	EscalationBehavior string    `gorm:"size:50;default:'always_on'"` // always_on, configurable
	EscalationTriggers string    `gorm:"type:jsonb"`                  // JSON array of keywords that trigger escalation
	DefaultResponse    string    `gorm:"size:50"`                     // e.g. "product_advisory"
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// ServiceKnowledge stores knowledge base items for consultancies and agencies
// This enables AI to answer customer questions using tenant-provided information
type ServiceKnowledge struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID  uuid.UUID `gorm:"index"`
	Category  string    `gorm:"size:50;index"`  // services, processes, faqs, policies
	Title     string    `gorm:"size:255;index"` // Short searchable title
	Content   string    `gorm:"type:text"`      // Rich text content (markdown supported)
	Keywords  string    `gorm:"type:jsonb"`     // JSON array of keywords for matching
	IsActive  bool      `gorm:"default:true"`
	SortOrder int       `gorm:"default:0"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Appointment represents a scheduled consultation or meeting for service-based businesses
type Appointment struct {
	ID             uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID       uuid.UUID `gorm:"index"`
	CustomerID     uuid.UUID `gorm:"index"`
	ConversationID uuid.UUID `gorm:"index"`

	// Appointment details
	ServiceType string     `gorm:"size:100"`      // "Discovery Call", "Full Consultation"
	Status      string     `gorm:"size:50;index"` // scheduled, confirmed, completed, cancelled, no_show
	ScheduledAt *time.Time `gorm:"index"`         // When appointment is scheduled
	Duration    int        `gorm:"default:30"`    // Duration in minutes
	MeetingType string     `gorm:"size:20"`       // online, in_person, phone
	MeetingLink string     `gorm:"size:500"`      // Zoom/Meet link if online
	Location    string     `gorm:"type:text"`     // Address if in-person

	// Notes
	CustomerNotes string `gorm:"type:text"` // What customer wants to discuss
	TenantNotes   string `gorm:"type:text"` // Internal notes

	// Reminders
	ReminderSentAt *time.Time // When reminder was sent to customer

	// Timestamps
	CreatedAt   time.Time
	ConfirmedAt *time.Time // When customer/tenant confirmed
	CompletedAt *time.Time // When appointment was completed
}

// Product represents a product for e-commerce businesses
type Product struct {
	Base
	TenantID            uuid.UUID `gorm:"index"`
	Name                string    `gorm:"size:255;index"`
	Description         string    `gorm:"type:text"`
	Price               float64
	Currency            string         `gorm:"size:10;default:'NGN'"`
	NegotiationMode     string         `gorm:"size:20;default:'default'"`
	NegotiationMinPrice float64        `gorm:"default:0"`
	NegotiationMaxPrice float64        `gorm:"default:0"`
	Category            string         `gorm:"size:100;index"`
	Images              string         `gorm:"type:jsonb"`                 // JSON array of image URLs
	Variants            string         `gorm:"type:jsonb"`                 // JSON: {"size": ["S", "M", "L"], "color": ["Red", "Blue"]}
	Tags                string         `gorm:"type:jsonb"`                 // JSON array of tags for search
	AIBehavior          datatypes.JSON `gorm:"type:jsonb"`                 // JSON for AI behavior flags and settings
	StockCount          int            `gorm:"default:-1"`                 // -1 = unlimited, 0+ = actual stock
	Availability        string         `gorm:"size:20;default:'in_stock'"` // in_stock, out_of_stock, low_stock
	IsActive            bool           `gorm:"default:true"`
	SKU                 string         `gorm:"size:100;index"` // Stock Keeping Unit (optional)
}

// MenuItem represents a menu item for food & beverage businesses (restaurants, bakeries, cafes, etc.)
type MenuItem struct {
	Base
	TenantID            uuid.UUID `gorm:"index"`
	Name                string    `gorm:"size:255;index"`
	Description         string    `gorm:"type:text"`
	Price               float64
	Currency            string         `gorm:"size:10;default:'NGN'"`
	NegotiationMode     string         `gorm:"size:20;default:'default'"`
	NegotiationMinPrice float64        `gorm:"default:0"`
	NegotiationMaxPrice float64        `gorm:"default:0"`
	Category            string         `gorm:"size:100;index"` // appetizer, main, dessert, drink, etc.
	Images              string         `gorm:"type:jsonb"`     // JSON array of image URLs
	DietaryInfo         string         `gorm:"type:jsonb"`     // JSON: ["vegetarian", "vegan", "gluten-free"]
	AIBehavior          datatypes.JSON `gorm:"type:jsonb"`     // JSON for AI behavior settings (e.g., negotiation flags)
	PreparationTime     int            // Minutes
	Availability        string         `gorm:"size:20;default:'available'"` // available, unavailable, limited
	IsActive            bool           `gorm:"default:true"`
	SpiceLevel          string         `gorm:"size:20"` // mild, medium, hot, etc. (optional)
}

// Service represents a service for consultant/agency businesses
type Service struct {
	Base
	TenantID            uuid.UUID      `gorm:"index"`
	Name                string         `gorm:"size:255;index"`
	Description         string         `gorm:"type:text"`
	Pricing             string         `gorm:"type:jsonb"` // JSON: {"type": "fixed", "amount": 50000} or {"type": "hourly", "rate": 5000}
	NegotiationMode     string         `gorm:"size:20;default:'default'"`
	NegotiationMinPrice float64        `gorm:"default:0"`
	NegotiationMaxPrice float64        `gorm:"default:0"`
	AIBehavior          datatypes.JSON `gorm:"type:jsonb"` // JSON for AI behavior settings (e.g., negotiation flags)
	Packages            string         `gorm:"type:jsonb"` // JSON array of package options
	Duration            string         `gorm:"size:50"`    // e.g., "1 hour", "2 weeks", "1 month"
	Deliverables        string         `gorm:"type:text"`  // What the customer gets
	IsActive            bool           `gorm:"default:true"`
	Category            string         `gorm:"size:100;index"` // consultation, project, retainer, etc.
}

// ADMIN USERS (SYSTEM)

type AdminUser struct {
	Base
	Email        string `gorm:"uniqueIndex"`
	PasswordHash string
	Role         string // SuperAdmin, Support
	Active       bool   `gorm:"default:true"`
	LastLogin    *time.Time
}

//  CUSTOMERS

type Customer struct {
	ID            uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID      uuid.UUID `gorm:"index:idx_customer_unique,unique"`
	Channel       string    `gorm:"size:50;index:idx_customer_unique,unique"`  // telegram, whatsapp, instagram, facebook
	PlatformID    string    `gorm:"size:100;index:idx_customer_unique,unique"` // platform-specific ID (e.g. Telegram ID, WA number)
	DisplayName   string    `gorm:"size:120"`
	Username      string    `gorm:"size:120"`
	ProfilePicURL string    `gorm:"type:text"`
	LastMessageAt time.Time `gorm:"autoUpdateTime"`
	CreatedAt     time.Time
}

// CustomerInsight stores summarized customer personality traits and preferences
// This is updated after conversations to help AI personalize future interactions
type CustomerInsight struct {
	Base
	TenantID       uuid.UUID `gorm:"uniqueIndex:idx_customer_insight;not null"`
	CustomerID     uuid.UUID `gorm:"uniqueIndex:idx_customer_insight;not null"`
	Summary        string    `gorm:"type:text"` // "Price-sensitive, prefers delivery"
	Tags           string    `gorm:"type:text"` // JSON array: ["price_sensitive","repeat_buyer"]
	BuyerType      string    `gorm:"size:50"`   // "premium", "budget", "undecided"
	PreferredTime  string    `gorm:"size:50"`   // "morning", "evening"
	TotalPurchases int       `gorm:"default:0"` // Number of completed orders
	LastUpdated    time.Time
}

// Order represents a customer order
type Order struct {
	ID                  uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID            uuid.UUID `gorm:"index"`
	CustomerID          uuid.UUID `gorm:"index"`
	ConversationID      uuid.UUID `gorm:"index"`               // CRITICAL: Links order to specific conversation - prevents "last order" guessing
	OrderNumber         string    `gorm:"uniqueIndex;size:50"` // "ORD-2024-001234"
	PaymentReference    string    `gorm:"uniqueIndex;size:50"` // "BRNCR-123-20241204-ABC123"
	Status              string    `gorm:"size:50;index"`       // pending, confirmed, processing, completed, cancelled
	TotalAmount         float64
	Currency            string `gorm:"size:10;default:'NGN'"`
	Items               string `gorm:"type:jsonb"` // JSON array of OrderItem
	CustomerName        string `gorm:"size:255"`
	CustomerPhone       string `gorm:"size:50"`
	CustomerEmail       string `gorm:"size:255"`
	Platform            string `gorm:"size:50"`   // whatsapp, instagram, telegram, facebook
	PaymentInstructions string `gorm:"type:text"` // Bank details, etc. sent to customer
	Notes               string `gorm:"type:text"` // Tenant notes

	// Delivery Information
	DeliveryAddress string `gorm:"type:text"` // Full delivery address
	DeliveryNotes   string `gorm:"type:text"` // Special delivery instructions

	// Pickup/Appointment
	IsPickup        bool       `gorm:"default:false"` // True if pickup order
	AppointmentTime *time.Time // Scheduled pickup time
	ReminderSentAt  *time.Time // When reminder was sent

	CreatedAt   time.Time
	ConfirmedAt *time.Time // When tenant confirmed payment
	CompletedAt *time.Time // When order is fulfilled
}

// Payment represents a payment transaction
type Payment struct {
	ID                 uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID           uuid.UUID `gorm:"index"`
	OrderID            uuid.UUID `gorm:"index"`
	CustomerID         uuid.UUID `gorm:"index"`
	PaymentReference   string    `gorm:"uniqueIndex;size:50"` // Links to Order.PaymentReference
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
	PortalToken        string     `gorm:"uniqueIndex;size:100"`    // Unique token for customer portal access
	PortalURL          string     `gorm:"type:text"`               // Full portal URL with token
	TokenExpiresAt     *time.Time `gorm:"index"`                   // Token expiration date (optional)
	Metadata           string     `gorm:"type:jsonb;default:'{}'"` // Additional payment data
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
	Base
	TenantID            uuid.UUID `gorm:"index"`
	AccountType         string    `gorm:"size:50"` // bank, mobile_money, cash
	BankName            string    `gorm:"size:100"`
	AccountNumber       string    `gorm:"type:text"` // Encrypted
	AccountName         string    `gorm:"size:255"`
	MobileMoneyProvider string    `gorm:"size:100"` // For mobile money (MTN, Airtel, etc.)
	PhoneNumber         string    `gorm:"size:50"`  // For mobile money
	IsDefault           bool      `gorm:"default:false"`
	IsActive            bool      `gorm:"default:true"`
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// SalesConversation tracks sales conversation state
type SalesConversation struct {
	Base
	ConversationID          uuid.UUID `gorm:"index"` // Links to Conversation
	TenantID                uuid.UUID `gorm:"index"`
	CustomerID              uuid.UUID `gorm:"index"`
	Stage                   string    `gorm:"size:50"`                 // greeting, product_inquiry, negotiation, commitment_confirmation, payment_instructions
	CommitmentLevel         string    `gorm:"size:50"`                 // exploring, interested, considering, committed, payment_ready
	ItemsDiscussed          string    `gorm:"type:jsonb;default:'[]'"` // JSON array of products/services discussed
	PriceDiscussed          float64   // Price discussed (may change during negotiation)
	NegotiationCount        int       `gorm:"default:0"` // Number of negotiation rounds
	SameOfferCount          int       `gorm:"default:0"` // Number of repeated identical offers seen
	LastOfferAmount         float64   // Last numeric offer observed from customer
	LastAIOffer             float64   // Last/best price AI has offered (prevents backtracking - can only stay same or improve)
	EscalationPending       bool      `gorm:"default:false"`           // True if escalation mentioned - negotiation should pause
	AIConcessionCount       int       `gorm:"default:0"`               // Number of AI concessions made in this conversation
	AIPersuasionCount       int       `gorm:"default:0"`               // Number of persuasion attempts made by AI in this conversation
	FlexibilityAttempted    bool      `gorm:"default:false"`           // Whether AI attempted a non-price flexibility lever
	CustomerRequestedNumber bool      `gorm:"default:false"`           // Whether customer explicitly requested a number
	ObjectionsRaised        string    `gorm:"type:jsonb;default:'[]'"` // JSON array of objections
	LastIntent              string    `gorm:"size:50"`                 // Last detected intent
	PendingOrderDraft       string    `gorm:"type:jsonb;default:'{}'"` // Draft order JSON awaiting tenant confirmation
	PendingOrderStatus      string    `gorm:"size:50"`                 // pending_confirmation, approved, declined, none
	PendingOrderSetAt       *time.Time

	// Customer Details Collected
	DeliveryAddress string `gorm:"type:text"`     // Collected during conversation
	CustomerPhone   string `gorm:"size:50"`       // Collected phone number
	PickupRequested bool   `gorm:"default:false"` // Customer wants pickup
	PickupTime      string `gorm:"size:100"`      // Requested pickup time (raw text)

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ConversationUserState tracks per-user read/unread state for a conversation
type ConversationUserState struct {
	Base
	TenantID       uuid.UUID `gorm:"index"`
	ConversationID uuid.UUID `gorm:"index"`
	UserID         uuid.UUID `gorm:"index"`
	LastReadAt     *time.Time
	UnreadCount    int `gorm:"default:0"`
}

// Notification tracks dashboard notifications for a tenant
type Notification struct {
	Base
	TenantID       uuid.UUID `gorm:"index"`
	ConversationID uuid.UUID `gorm:"index"`
	InteractionID  uuid.UUID `gorm:"index"`
	Type           string    `gorm:"size:50;index"` // message, escalation, payment, system
	Title          string    `gorm:"size:255"`
	Body           string    `gorm:"type:text"`
	Severity       string    `gorm:"size:20"` // info, warning, critical
	ReadAt         *time.Time
	Metadata       string `gorm:"type:jsonb;default:'{}'"`
}

// NegotiationHistory tracks negotiation attempts
type NegotiationHistory struct {
	Base
	SalesConversationID uuid.UUID `gorm:"index"`   // Links to SalesConversation
	Type                string    `gorm:"size:50"` // price, quantity, terms
	CustomerOffer       float64   // What customer offered
	CounterOffer        float64   // What AI countered with
	FinalAgreement      float64   // Final agreed price (if any)
	Status              string    `gorm:"size:50"` // ongoing, agreed, rejected
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Booking represents a consultation/booking request for services
// Used by Consultant/Agency industries to handle time-based offers
type Booking struct {
	Base
	TenantID   uuid.UUID `gorm:"index"`
	CustomerID uuid.UUID `gorm:"index"` // optional: link to Customer table
	ServiceID  uuid.UUID `gorm:"index"` // optional: link to Service
	StartAt    time.Time // requested start time
	EndAt      time.Time // optional end time; if zero, use Service.Duration
	Duration   string    `gorm:"size:50"`                     // human readable duration (fallback)
	Status     string    `gorm:"size:50;default:'requested'"` // requested, confirmed, cancelled
	Platform   string    `gorm:"size:50"`                     // source: web, telegram, whatsapp
	CreatedBy  uuid.UUID `gorm:"index"`                       // admin/user who created/confirmed
	Notes      string    `gorm:"type:text"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// TenantNotificationSettings stores tenant-configurable notification preferences.
// This is used by order/payment notification delivery (email + webhooks).
type TenantNotificationSettings struct {
	Base
	TenantID uuid.UUID `gorm:"uniqueIndex"`

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
	Base
	AdminEmail string     `gorm:"size:255;index"`
	AdminRole  string     `gorm:"size:50;index"`
	Action     string     `gorm:"size:100;index"`
	Method     string     `gorm:"size:10"`
	Path       string     `gorm:"type:text"`
	TenantID   *uuid.UUID `gorm:"index"`
	Resource   string     `gorm:"size:50;index"`  // tenant, integration, post, message, billing, system
	ResourceID string     `gorm:"size:100;index"` // string for flexibility
	Success    bool       `gorm:"default:true"`
	StatusCode int
	IP         string `gorm:"size:64"`
	UserAgent  string `gorm:"type:text"`
	Metadata   string `gorm:"type:jsonb"` // optional JSON
}

// ACTIVITY & LOGGING

type EventLog struct {
	Base
	TenantID   uuid.UUID
	EventType  string // payment, delivery, appointment, other
	SenderName string
	Message    string
	Forwarded  bool      `gorm:"default:false"`
	CreatedAt  time.Time `gorm:"autoCreateTime"`
}

type LogEntry struct {
	Base
	TenantID  *uuid.UUID `gorm:"index"` // Optional tenant context
	Level     string     `gorm:"index"` // INFO, WARN, ERROR
	Source    string     // "monitor", "webhook", "scheduler", etc.
	Message   string     `gorm:"type:text"`
	Meta      string     `gorm:"type:text"` // JSON-encoded details
	CreatedAt time.Time  `gorm:"autoCreateTime"`
}

//  MEDIA

type MediaItem struct {
	Base
	TenantID     uuid.UUID
	SessionID    string `gorm:"index"` // Telegram MediaGroupID or session
	URL          string
	ResourceType string // image, video
	FileID       string
	Status       string // pending, processed
}

type MediaAsset struct {
	Base
	TenantID     uuid.UUID
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
	BulkUploadID string `gorm:"index"`                   // Links assets from same bulk upload
	Metadata     string `gorm:"type:jsonb;default:'{}'"` // Platform-specific metadata (e.g., TikTok posting options)
}

// POST SCHEDULING

type ScheduledPost struct {
	Base
	TenantID       uuid.UUID `gorm:"index"`
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
	Base
	TenantID           uuid.UUID
	Platform           string // instagram, facebook, whatsapp, tiktok, youtube
	AccessToken        string `gorm:"type:text"`
	RefreshToken       string `gorm:"type:text"`
	ExpiresAt          *time.Time
	Username           string
	PageID             string // For Facebook: page_id, for Instagram: instagram_business_account.id, for WhatsApp: phone_number_id
	ExternalID         string `gorm:"index"` // Platform-specific external ID used for webhook routing (page_id, instagram_business_account.id, phone_number_id)
	Connected          bool
	Mode               string // manual, sandbox, oauth2, provider_owned
	AiMode             string `gorm:"size:20" json:"ai_mode"`  // per-inbox AI mode: "ai" or "human"; if empty => inherit tenant
	LoginMethod        string `gorm:"size:50"`                 // facebook_login, instagram_login (for Instagram: determines which OAuth flow was used)
	WebhookVerifyToken string `gorm:"size:100"`                // Verification token for webhook setup (Meta requires this)
	Metadata           string `gorm:"type:jsonb;default:'{}'"` // Platform-specific metadata (e.g., TikTok authorized scopes, posting options, login method)
	CreatedAt          time.Time
}

// WhatsAppPhoneNumber represents a phone number in Brancr's provider-owned WABA
type WhatsAppPhoneNumber struct {
	Base
	PhoneNumberID string     `gorm:"uniqueIndex"` // Meta's phone_number_id (used for webhook routing)
	PhoneNumber   string     // Display format: +1234567890
	WABAID        string     `gorm:"index"` // Which WABA this number belongs to
	TenantID      *uuid.UUID `gorm:"index"` // NULL = available, assigned to tenant if set
	DisplayName   string     // Tenant's business name (when assigned)
	VerifiedName  string     // From Meta API
	QualityRating string     // Meta quality rating
	Status        string     // available, assigned, suspended
	AssignedAt    *time.Time
}

// WhatsAppPhoneNumberRequest represents a tenant's request to add their own phone number
type WhatsAppPhoneNumberRequest struct {
	Base
	TenantID         uuid.UUID  `gorm:"index"` // Tenant who requested the number
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
	Base
	TenantID           uuid.UUID `gorm:"index;not null"`
	GupshupAppID       string    `gorm:"type:text;not null;index"` // UUID from Gupshup redirect callback
	GupshupAppToken    string    `gorm:"type:text;not null"`       // Encrypted token from callback
	WABAID             string    `gorm:"type:text"`
	BusinessID         string    `gorm:"type:text"`
	PhoneNumberID      string    `gorm:"type:text"`
	PhoneNumber        string    `gorm:"type:text"`                            // From redirect callback
	Status             string    `gorm:"size:50;default:'pending_onboarding'"` // pending_onboarding, pending_verification, live, failed
	Mode               string    `gorm:"size:50;default:'gupshup_partner'"`
	SubscriptionID     string    `gorm:"type:text"`     // Subscription ID from Subscription API
	SubscriptionActive bool      `gorm:"default:false"` // Whether subscription is active
	WebhookURL         string    `gorm:"type:text"`     // Webhook URL configured for this app
	DefaultTemplateID  string    `gorm:"type:text"`     // Default text template ID for AI replies (template with variable parameter)
}

// GupshupWebhookEvent stores incoming webhook events from Gupshup
type GupshupWebhookEvent struct {
	Base
	TenantID    uuid.UUID `gorm:"index"`
	AppID       string    `gorm:"index"`
	EventType   string    // message, delivery_status, read_status
	Payload     string    `gorm:"type:text"` // Full JSON payload
	Processed   bool      `gorm:"default:false"`
	ProcessedAt *time.Time
	ReceivedAt  time.Time
}

// Persona — each tenant’s bot personality profile
type Persona struct {
	TenantID   uuid.UUID `gorm:"type:uuid;primary_key"`
	BotName    string    `gorm:"size:50"`  // e.g. "Luna", "Aiden", "Mimi"
	Tone       string    `gorm:"size:120"` // "friendly, witty, warm"
	Language   string    `gorm:"size:30"`  // "en", "en-NG", "fr"
	Humor      bool      `gorm:"default:true"`
	StyleNotes string    `gorm:"type:text"` // Additional style hints
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// VoiceFingerprint captures the learnt communication style of a tenant.
// It is updated deterministically from human replies.
type VoiceFingerprint struct {
	TenantID uuid.UUID `gorm:"type:uuid;primary_key"`

	// All values normalized 0.0 – 1.0
	EmojiRate      float64 `gorm:"default:0.1"` // How often emojis appear
	SlangRate      float64 `gorm:"default:0.0"` // Informal language usage
	Formality      float64 `gorm:"default:0.5"` // Sentence structure / politeness (1.0 = Very Formal)
	Directness     float64 `gorm:"default:0.5"` // Avoids hedging ("maybe", "let me check")
	SentenceLength float64 `gorm:"default:0.4"` // Normalized avg sentence length
	QuestionRate   float64 `gorm:"default:0.2"` // How often questions are asked

	UpdatedAt time.Time
}

// MemoryItem — contextual memory vectors or text
type MemoryItem struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID    uuid.UUID `gorm:"index"`
	CustomerID  uuid.UUID `gorm:"index"`
	Text        string    `gorm:"type:text"`
	Embedding   []float32 `gorm:"type:float[]"`
	Category    string    `gorm:"size:50"`
	ContextHash string    `gorm:"uniqueIndex"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
}

type IntentCatalog struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID  uuid.UUID
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type TenantPolicy struct {
	ID                 uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID           uuid.UUID
	AutoReplyThreshold float64 // 0-1
	EscalateEmotions   string  // JSON array of labels
	// Negotiation thresholds (moved out of installment_terms)
	NegotiationSameOfferRequired  int     `gorm:"default:3"`    // how many times same offer must repeat
	NegotiationPersuasionRequired int     `gorm:"default:3"`    // how many AI persuasion attempts required
	NegotiationProximityPct       float64 `gorm:"default:0.10"` // proximity (fraction) to consider offer close
	// AllowInstallments indicates whether tenant permits suggesting installment/split payment options
	AllowInstallments bool `gorm:"default:false"`
	// ReplyDelayMs adds human-like pacing: 0 = instant, 500-2000 = natural feel
	ReplyDelayMs int `gorm:"default:0"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// TenantOffer stores promotional offers provided by tenant via Telegram
// AI can ONLY suggest offers that are stored here - never invent offers
type TenantOffer struct {
	Base
	TenantID    uuid.UUID `gorm:"index"`
	Title       string    `gorm:"size:255"`     // e.g., "Buy 2 Get 1 Free"
	Description string    `gorm:"type:text"`    // Full description of the offer
	IsActive    bool      `gorm:"default:true"` // Whether offer is currently active
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Escalation struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID   uuid.UUID
	UserID     string
	Platform   string
	Message    string `gorm:"type:text"`
	DraftReply string `gorm:"type:text"`
	Status     string // pending/approved/sent
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// PendingEscalation tracks escalations waiting for tenant approval (replaces in-memory map)
type PendingEscalation struct {
	Base
	InteractionID  uuid.UUID `gorm:"uniqueIndex;not null"` // Unique per interaction
	TenantID       uuid.UUID `gorm:"index;not null"`
	CustomerID     uuid.UUID `gorm:"index;not null"`
	Platform       string    `gorm:"size:50;not null"`
	Message        string    `gorm:"type:text;not null"`
	SuggestedReply string    `gorm:"type:text;not null"`
	ChatID         int64     `gorm:"not null"` // Telegram chat ID where escalation was sent
	Intent         string    `gorm:"size:50"`  // payment, refund, complaint, etc.
	Tone           string    `gorm:"size:50"`  // friendly, urgent, etc.
	Confidence     float64   // AI confidence score
	Reason         string    `gorm:"type:text"` // Reason for escalation

	// Type-specific fields
	EscalationType    string `gorm:"size:50;default:'general';index"` // payment, complaint, booking, inquiry, general
	ResolutionStatus  string `gorm:"size:50;default:'pending'"`       // pending, approved, resolved, confirmed, ignored
	ResolutionMessage string `gorm:"type:text"`                       // Message sent to customer on resolution
	ResolvedAt        *time.Time
	ResolvedBy        string `gorm:"size:255"` // Who resolved (admin email or "owner")

	// Payment-specific
	PaymentAmount float64
	OrderID       *uuid.UUID `gorm:"index"`
	ReceiptSent   bool       `gorm:"default:false"`

	// Booking-specific
	BookingID     *uuid.UUID `gorm:"index"`
	ProposedTime  *time.Time
	ConfirmedTime *time.Time
}

// EscalationType constants
const (
	EscalationTypePayment   = "payment"
	EscalationTypeComplaint = "complaint"
	EscalationTypeBooking   = "booking"
	EscalationTypeInquiry   = "inquiry"
	EscalationTypeGeneral   = "general"
)

// ResolutionStatus constants
const (
	ResolutionPending   = "pending"
	ResolutionApproved  = "approved"  // For payments: confirmed + receipt sent
	ResolutionResolved  = "resolved"  // For complaints: issue handled
	ResolutionConfirmed = "confirmed" // For bookings: appointment confirmed
	ResolutionIgnored   = "ignored"   // Dismissed without action
)

// PostSession tracks post creation flow state (replaces in-memory map)
type PostSession struct {
	Base
	ChatID               int64     `gorm:"uniqueIndex;not null"` // Telegram chat ID
	TenantID             uuid.UUID `gorm:"index;not null"`
	Step                 int       `gorm:"not null;default:0"`
	MediaAssetID         uuid.UUID `gorm:"default:null"`
	MediaType            string    `gorm:"size:50"` // image, video, carousel
	IsCarousel           bool      `gorm:"default:false"`
	Name                 string    `gorm:"size:255"`
	Caption              string    `gorm:"type:text"`
	UseAICaption         bool      `gorm:"default:false"`
	CaptionMode          string    `gorm:"size:50"`    // "write", "keywords", "ai", ""
	Platforms            string    `gorm:"type:jsonb"` // JSON array of selected platforms
	ScheduleInput        string    `gorm:"type:text"`
	BatchSchedule        bool      `gorm:"default:false"`
	BatchSpacing         string    `gorm:"size:100"`
	OriginalInputs       string    `gorm:"type:jsonb"` // JSON array for batch scheduling
	TikTokDisableDuet    bool      `gorm:"default:false"`
	TikTokDisableStitch  bool      `gorm:"default:false"`
	TikTokDisableComment bool      `gorm:"default:false"`
	TikTokScheduleTime   *time.Time
	TikTokOptionsAsked   bool       `gorm:"default:false"`
	ExpiresAt            *time.Time `gorm:"index"` // Auto-cleanup old sessions
}

// BusinessSession tracks business profile update flow state (replaces in-memory map)
type BusinessSession struct {
	Base
	ChatID    int64      `gorm:"uniqueIndex;not null"` // Telegram chat ID
	TenantID  uuid.UUID  `gorm:"index;not null"`
	Mode      string     `gorm:"size:50;not null"` // add_menu, edit_menu, add_faq, etc.
	Step      int        `gorm:"not null;default:1"`
	MenuItem  string     `gorm:"type:jsonb"` // JSON serialized BusinessMenuItem
	FAQ       string     `gorm:"type:jsonb"` // JSON serialized BusinessFAQ
	ExpiresAt *time.Time `gorm:"index"`      // Auto-cleanup old sessions
}

// EscalationResolutionSession tracks escalation resolution flow state (replaces in-memory map)
type EscalationResolutionSession struct {
	Base
	ChatID        int64      `gorm:"uniqueIndex;not null"` // Telegram chat ID
	TenantID      uuid.UUID  `gorm:"index;not null"`
	InteractionID uuid.UUID  `gorm:"index;not null"`
	CustomerID    uuid.UUID  `gorm:"index;not null"`
	Platform      string     `gorm:"size:50;not null"`
	StartedAt     time.Time  `gorm:"not null"`
	ExpiresAt     *time.Time `gorm:"index"` // Auto-cleanup old sessions
}

// PostSessionV2 tracks the new simplified posting flow state
type PostSessionV2 struct {
	Base
	ChatID        int64      `gorm:"uniqueIndex;not null"` // Telegram chat ID
	TenantID      uuid.UUID  `gorm:"index;not null"`
	Step          int        `gorm:"not null;default:0"` // 0=platform, 1=caption, 2=schedule
	MediaAssetIDs string     `gorm:"type:jsonb"`         // JSON array of media asset IDs
	MediaType     string     `gorm:"size:50"`            // image, video, carousel
	Platforms     string     `gorm:"type:jsonb"`         // JSON array of selected platforms
	CaptionMode   string     `gorm:"size:50"`            // "user", "ai", "same"
	Captions      string     `gorm:"type:jsonb"`         // JSON array of captions (one per media)
	CaptionTheme  string     `gorm:"size:255"`           // Theme for AI caption generation
	ScheduleMode  string     `gorm:"size:50"`            // "now", "specific", "spaced"
	ScheduleTimes string     `gorm:"type:jsonb"`         // JSON array of schedule times
	IsBatch       bool       `gorm:"default:false"`      // Multiple images being scheduled
	BatchSpacing  string     `gorm:"size:50"`            // "24h", "12h", "custom"
	LastPlatforms string     `gorm:"type:jsonb"`         // Remembered last used platforms
	ExpiresAt     *time.Time `gorm:"index"`              // Auto-cleanup old sessions
}

// PostLog tracks detailed posting activity for analytics
type PostLog struct {
	ID               uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID         uuid.UUID `gorm:"index"`
	ScheduledPostID  uuid.UUID `gorm:"index"`
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
	Base
	TenantID         uuid.UUID `gorm:"index"`
	SessionID        string    `gorm:"uniqueIndex"` // Unique session identifier
	TotalItems       int
	ProcessedItems   int
	Status           string // pending, processing, completed, failed
	SplitStrategy    string // equal, by_count, by_date, ai_organized
	CarouselCount    int    // Number of carousels to create
	ItemsPerCarousel int
	ScheduleStrategy string // spread, custom, optimal
	Metadata         string `gorm:"type:text"` // JSON metadata
}

// GenericSession provides flexible key-value session storage for V3 flows
type GenericSession struct {
	Base
	Key       string     `gorm:"uniqueIndex;not null;size:255"` // Session key (e.g., "postv3:12345")
	Data      string     `gorm:"type:text"`                     // JSON serialized data
	ExpiresAt *time.Time `gorm:"index"`                         // Auto-cleanup
}

// RecurringTemplate for recurring posts
type RecurringTemplate struct {
	Base
	TenantID        uuid.UUID `gorm:"index"`
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
	Base
	TenantID uuid.UUID `gorm:"index"`
	Date     time.Time `gorm:"index"`
	Posts    string    `gorm:"type:text"` // JSON array of ScheduledPost IDs
	Theme    string    // Optional theme for the day
	Notes    string    `gorm:"type:text"`
	Status   string    // planned, scheduled, posted, cancelled
}

// TenantQuota tracks resource usage and limits for tenants
type TenantQuota struct {
	Base
	TenantID uuid.UUID `gorm:"uniqueIndex"` // One quota per tenant

	// Limits (0 = unlimited)
	AICallsLimit   int `gorm:"default:100"`  // Daily AI API calls limit
	PostsLimit     int `gorm:"default:50"`   // Daily posts limit
	StorageLimitMB int `gorm:"default:1000"` // Storage limit in MB
	APICallsLimit  int `gorm:"default:1000"` // Daily API calls limit

	// Platforms limit (max connected social accounts)
	PlatformsLimit int `gorm:"default:5"`

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

	// Cost control (in USD)
	CostLimit float64 `gorm:"default:0"` // 0 = unlimited, cost limit per reset period
	CostUsed  float64 `gorm:"default:0"` // Current cost used in this period (in USD)
}

// TableName explicitly sets the table name for TenantQuota
func (TenantQuota) TableName() string {
	return "tenant_quotas"
}

// =======================
//  BILLING & PLANS
// =======================

// PlanTemplate represents admin-defined plan templates (defaults for new tenants)
type PlanTemplate struct {
	Base
	Name string `gorm:"size:100;uniqueIndex;not null"` // Trial, Starter, Pro

	// Quota defaults
	DefaultAIReplyQuota         int `gorm:"default:0"` // 0 = unlimited
	DefaultWhatsAppMessageQuota int `gorm:"default:0"`
	DefaultPostQuota            int `gorm:"default:0"`
	DefaultStorageLimitMB       int `gorm:"default:1000"`
	DefaultAPICallsLimit        int `gorm:"default:1000"`
	DefaultPlatformsLimit       int `gorm:"default:5"`

	// Feature flags
	AINegotiationEnabled bool   `gorm:"default:false"`
	EscalationBehavior   string `gorm:"size:50;default:'always_on'"` // always_on, configurable, advanced
	OveragesAllowed      bool   `gorm:"default:false"`               // Manual approval only

	// Pricing
	DefaultMonthlyPrice float64 `gorm:"default:0"` // In NGN (₦)

	// Status
	Status string `gorm:"size:50;default:'active';index"` // active, deprecated

	// Metadata
	Description string `gorm:"type:text"`
	Metadata    string `gorm:"type:jsonb;default:'{}'"`
}

// TenantPlan represents a tenant's actual plan assignment (snapshot of template)
// This is the SOURCE OF TRUTH for tenant limits and pricing
type TenantPlan struct {
	Base
	TenantID       uuid.UUID `gorm:"uniqueIndex;not null"` // One active plan per tenant
	PlanTemplateID uuid.UUID `gorm:"index"`                // Reference to template (for admin reference)
	PlanName       string    `gorm:"size:100;not null"`    // Copied from template name

	// Actual quotas (snapshot values, can be overridden by admin)
	AIReplyQuota         int `gorm:"default:0"`
	WhatsAppMessageQuota int `gorm:"default:0"`
	PostQuota            int `gorm:"default:0"`
	StorageLimitMB       int `gorm:"default:1000"`
	APICallsLimit        int `gorm:"default:1000"`
	PlatformsLimit       int `gorm:"default:5"`

	// Feature flags (snapshot)
	AINegotiationEnabled bool   `gorm:"default:false"`
	EscalationBehavior   string `gorm:"size:50;default:'always_on'"`
	OveragesAllowed      bool   `gorm:"default:false"`

	// Pricing (can be overridden by admin)
	MonthlyPrice float64 `gorm:"default:0"` // In NGN (₦)

	// Billing cycle
	BillingCycleStart time.Time  `gorm:"index"` // When current billing cycle started
	BillingCycleEnd   *time.Time `gorm:"index"` // When current billing cycle ends (nil = ongoing)

	// Billing status
	BillingStatus string `gorm:"size:50;default:'trial';index"` // trial, active, paused, cancelled

	// Payment tracking
	PaystackCustomerID  string `gorm:"size:255;index"`            // Paystack customer ID
	PaymentStatus       string `gorm:"size:50;default:'pending'"` // pending, paid, failed, overdue
	LastPaymentDate     *time.Time
	NextBillingDate     *time.Time `gorm:"index"`
	LastPaymentAmount   *float64
	PaymentFailureCount int `gorm:"default:0"`

	// Trial tracking (if applicable)
	TrialEndsAt *time.Time `gorm:"index"`

	// Admin overrides (track if admin manually changed values)
	QuotasOverridden bool `gorm:"default:false"` // True if admin manually set quotas
	PriceOverridden  bool `gorm:"default:false"` // True if admin manually set price

	// Metadata
	Metadata string `gorm:"type:jsonb;default:'{}'"`
}

// UsageEvent tracks individual usage events for metering
type UsageEvent struct {
	Base
	TenantID      uuid.UUID `gorm:"index;not null"`
	EventType     string    `gorm:"size:50;index;not null"`  // ai_reply_sent, ai_tokens_used, whatsapp_message_sent, post_published
	Quantity      int       `gorm:"default:1"`               // Number of units (e.g., messages, tokens)
	EstimatedCost float64   `gorm:"default:0"`               // Estimated cost in USD
	Metadata      string    `gorm:"type:jsonb;default:'{}'"` // Additional event data (model name, tokens, etc.)
	EventDate     time.Time `gorm:"index"`                   // Date of event (for aggregation)
}

// UsageAggregation stores daily/monthly aggregated usage
type UsageAggregation struct {
	Base
	TenantID    uuid.UUID `gorm:"uniqueIndex:idx_tenant_period;not null"`
	PeriodType  string    `gorm:"size:20;uniqueIndex:idx_tenant_period;not null"` // daily, monthly
	PeriodStart time.Time `gorm:"uniqueIndex:idx_tenant_period;index;not null"`
	PeriodEnd   time.Time `gorm:"index;not null"`

	// Aggregated counts
	AIReplyCount         int `gorm:"default:0"`
	AITokensUsed         int `gorm:"default:0"`
	WhatsAppMessageCount int `gorm:"default:0"`
	PostCount            int `gorm:"default:0"`

	// Aggregated cost
	TotalCostUSD float64 `gorm:"default:0"`

	// Metadata
	Metadata string `gorm:"type:jsonb;default:'{}'"`
}

// GupshupUsage stores daily WhatsApp usage data from Gupshup Partner API
type GupshupUsage struct {
	Base
	TenantID                    uuid.UUID `gorm:"index;not null"`
	GupshupAppID                string    `gorm:"type:text;index;not null"`
	Date                        string    `gorm:"size:10;uniqueIndex:idx_app_date;not null"` // YYYY-MM-DD format, unique per app
	Authentication              int       `gorm:"default:0"`
	CumulativeBill              float64   `gorm:"default:0"`
	Currency                    string    `gorm:"size:10;default:'USD'"`
	Discount                    float64   `gorm:"default:0"`
	FEP                         int       `gorm:"default:0"` // Free Entry Points
	FTC                         int       `gorm:"default:0"` // Free Template Count
	GSCap                       float64   `gorm:"default:0"`
	GSFees                      float64   `gorm:"default:0"`
	IncomingMsg                 int       `gorm:"default:0"`
	OutgoingMsg                 int       `gorm:"default:0"`
	OutgoingMediaMsg            int       `gorm:"default:0"`
	Marketing                   int       `gorm:"default:0"`
	Service                     int       `gorm:"default:0"`
	TemplateMsg                 int       `gorm:"default:0"`
	TemplateMediaMsg            int       `gorm:"default:0"`
	TotalFees                   float64   `gorm:"default:0"`
	TotalMsg                    int       `gorm:"default:0"`
	Utility                     int       `gorm:"default:0"`
	WAFees                      float64   `gorm:"default:0"`
	InternationalAuthentication int       `gorm:"default:0"`
}

// GupshupDiscount stores daily discount and billing details from Gupshup Partner API
type GupshupDiscount struct {
	Base
	TenantID       uuid.UUID `gorm:"index;not null"`
	GupshupAppID   string    `gorm:"type:text;index;not null"`
	Year           int       `gorm:"uniqueIndex:idx_app_year_month_day;not null"`
	Month          int       `gorm:"uniqueIndex:idx_app_year_month_day;not null"` // 1-12
	Day            int       `gorm:"uniqueIndex:idx_app_year_month_day;not null"` // 1-31
	CumulativeBill float64   `gorm:"default:0"`
	DailyBill      float64   `gorm:"default:0"`
	Discount       float64   `gorm:"default:0"`
	GSCap          float64   `gorm:"default:0"`
	GSFees         float64   `gorm:"default:0"`
	PartnerID      int       `gorm:"default:0"`
}

// AlertTemplate stores customizable alert message templates that admins can edit
// Used for automated user action notifications (signup, onboarding, billing, etc.)
type AlertTemplate struct {
	Base
	Key         string `gorm:"uniqueIndex;size:100;not null"` // e.g., "signup_welcome", "trial_expiring"
	Name        string `gorm:"size:100;not null"`             // Human-readable name for admin UI
	Type        string `gorm:"size:50;not null;index"`        // system, billing, security, feature, quota
	Severity    string `gorm:"size:20;not null"`              // info, warning, critical
	Title       string `gorm:"size:255;not null"`             // Alert title (supports {{.Name}} placeholders)
	Message     string `gorm:"type:text;not null"`            // Alert message body (supports {{.Name}}, {{.Days}} etc.)
	ActionURL   string `gorm:"size:255"`                      // Optional action button URL
	ActionLabel string `gorm:"size:100"`                      // Optional action button label
	IsActive    bool   `gorm:"default:true"`                  // Enable/disable this alert
	IsEditable  bool   `gorm:"default:true"`                  // Can admins edit this template?
	Description string `gorm:"type:text"`                     // Help text for admins
}

// Feature 8: Follow-Up Scheduler
type FollowUp struct {
	Base
	TenantID            uuid.UUID  `gorm:"type:uuid;index"`
	CustomerID          uuid.UUID  `gorm:"type:uuid;index"`
	SalesConversationID *uuid.UUID `gorm:"type:uuid;index"`
	ScheduledAt         time.Time  `gorm:"index"`
	Type                string     `gorm:"size:50"`                         // reminder, check_in
	Status              string     `gorm:"size:50;default:'pending';index"` // pending, sent, cancelled
	Metadata            string     `gorm:"type:jsonb;default:'{}'"`
}

// =============================================================================
// V3 AI OBSERVABILITY
// =============================================================================

// DecisionLog tracks AI decision points for debugging and audit
// Used by V3 pipeline for structured logging of intents, actions, and outcomes
type DecisionLog struct {
	ID             uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TenantID       uuid.UUID `gorm:"index;not null"`
	CustomerID     uuid.UUID `gorm:"index"`
	ConversationID uuid.UUID `gorm:"index;not null"`

	// Decision context
	Stage        string  `gorm:"size:50;index;not null"` // 'intent', 'resolve', 'execute', 'compose'
	Input        string  `gorm:"type:text"`              // What came in (message, intent, etc.)
	Output       string  `gorm:"type:text"`              // What was decided (intent type, action, etc.)
	Confidence   float64 `gorm:"type:decimal(3,2)"`      // Confidence score if applicable
	Source       string  `gorm:"size:50"`                // 'heuristic', 'llm', etc.
	DurationMs   int     `gorm:""`                       // Processing time
	Success      bool    `gorm:"default:true"`           // Whether action succeeded
	ErrorMessage string  `gorm:"type:text"`              // Error if any

	// Additional context (JSON)
	Metadata string `gorm:"type:jsonb;default:'{}'"`

	CreatedAt time.Time `gorm:"index"`
}
