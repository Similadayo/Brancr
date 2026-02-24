// Package types provides shared types for the AI V4 pipeline.
// It processes customer messages and generates responses while proposing
// actions for the core system to execute.
//
// Architecture:
//
//	Message → Memory → Context → Intent → Policy → Composer → Humanizer → Response
//
// Key principle: AI proposes, Core executes
package types

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ============================================================
// MEMORY SCOPE (Isolates data per conversation)
// ============================================================

// MemoryScope uniquely identifies a conversation for memory isolation.
// Data is scoped: Tenant → Customer → Conversation
type MemoryScope struct {
	TenantID       uuid.UUID
	CustomerID     string // Platform-specific ID (WhatsApp number, Instagram ID, etc.)
	ConversationID uuid.UUID
	Platform       string // whatsapp, instagram, facebook, telegram
}

// ============================================================
// AI REQUEST / RESPONSE
// ============================================================

// AIRequest is the input to the AI pipeline
type AIRequest struct {
	Scope       MemoryScope
	MessageText string
	MediaItems  []MediaItem
	Timestamp   time.Time
}

// MediaItem represents an attached media file
type MediaItem struct {
	Type     string // image, video, audio, document
	URL      string
	MimeType string
	Caption  string
}

// AIResponse is the output of the AI pipeline.
// AI proposes actions and responses; Core validates and executes.
type AIResponse struct {
	// What to say to the customer
	ReplyText     string
	ReplySegments []string // Multi-message support for natural delivery

	// Proposed actions (Core validates and executes)
	ProposedActions []ProposedAction

	// Escalation request (if AI cannot handle)
	Escalation *EscalationRequest

	// State updates
	NewState ConversationState
	NewMode  ConversationMode

	// Debug/observability
	Reasoning string
	Metadata  map[string]any

	// Emotion detected in the response
	Emotion Emotion
}

// ============================================================
// ACTIONS
// ============================================================

// ProposedAction represents an action the AI proposes.
// Core system validates and executes these actions.
type ProposedAction struct {
	Type       ActionType
	Payload    map[string]any
	Confidence float64
	Reasoning  string
}

// ActionType defines the types of actions AI can propose
type ActionType string

const (
	ActionSendOfferings           ActionType = "send_offerings"
	ActionInitializeOrder         ActionType = "initialize_order"
	ActionAddToOrder              ActionType = "add_to_order"
	ActionUpdateOrder             ActionType = "update_order"
	ActionConfirmOrder            ActionType = "confirm_order"
	ActionRequestPayment          ActionType = "request_payment"
	ActionCancelOrder             ActionType = "cancel_order"
	ActionEscalate                ActionType = "escalate"
	ActionNoAction                ActionType = "no_action"
	ActionSendPaymentInstructions ActionType = "send_payment_instructions"
	ActionCreatePendingPayment    ActionType = "create_pending_payment"
)

// ============================================================
// ESCALATION
// ============================================================

// EscalationRequest triggers alerts to dashboard and Telegram bot
type EscalationRequest struct {
	Reason      string
	Priority    EscalationPriority
	Context     string // Summary of conversation context
	CustomerMsg string // The triggering message
}

// EscalationPriority defines urgency levels
type EscalationPriority string

const (
	PriorityHigh   EscalationPriority = "high"
	PriorityMedium EscalationPriority = "medium"
	PriorityLow    EscalationPriority = "low"
)

// ============================================================
// INTENT
// ============================================================

// Intent represents classified customer intent
type Intent string

const (
	IntentGreeting     Intent = "greeting"
	IntentInquiry      Intent = "inquiry"
	IntentPriceCheck   Intent = "price_check"
	IntentPurchase     Intent = "purchase"
	IntentConfirmOrder Intent = "confirm_order"
	IntentOrderStatus  Intent = "order_status"
	IntentComplaint    Intent = "complaint"
	IntentRefund       Intent = "refund"
	IntentNegotiation  Intent = "negotiation"
	IntentHumanRequest Intent = "human_request"
	IntentThankYou     Intent = "thank_you"
	IntentGoodbye      Intent = "goodbye"
	IntentUnknown      Intent = "unknown"

	// 🆕 CATALOG INTENTS (Step 1)
	IntentMenuRequest    Intent = "menu_request"
	IntentProductRequest Intent = "product_request"
	IntentServiceRequest Intent = "service_request"
	IntentPriceRequest   Intent = "price_request"
	IntentCatalogExplain Intent = "catalog_explain"
)

// IsValidIntent checks if an intent string is a valid canonical intent.
func IsValidIntent(i Intent) bool {
	switch i {
	case IntentGreeting, IntentInquiry, IntentPriceCheck, IntentPurchase,
		IntentConfirmOrder, IntentOrderStatus, IntentComplaint, IntentRefund,
		IntentNegotiation, IntentHumanRequest, IntentThankYou, IntentGoodbye,
		IntentUnknown:
		return true
	case IntentMenuRequest, IntentProductRequest, IntentServiceRequest, IntentPriceRequest, IntentCatalogExplain:
		return true
	default:
		return false
	}
}

// Helper to safely get UUID string
func UUIDToStringOrNil(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

// IntentResult is the output of intent classification
type IntentResult struct {
	Primary    Intent
	Secondary  []Intent
	Confidence float64
	Entities   map[string]any // Extracted entities (products, quantities, prices)
}

// ============================================================
// CONVERSATION STATE (Canonical Chat Flow State Machine)
// ============================================================

// ConversationMode represents the current mode in the chat flow state machine.
// Flow: DISCOVERY → SUPPORT → SALES → NEGOTIATION → ORDER_COLLECTION → PAYMENT_AWARENESS → ESCALATION
type ConversationMode string

const (
	// ModeDiscovery is the entry state. AI figures out why the customer is here.
	ModeDiscovery ConversationMode = "discovery"

	// ModeSupport is for answering questions and building trust. No selling.
	ModeSupport ConversationMode = "support"

	// ModeSales is for guiding customer toward a purchase decision.
	ModeSales ConversationMode = "sales"

	// ModeNegotiation is for handling price objections within policy bounds.
	ModeNegotiation ConversationMode = "negotiation"

	// ModeOrderCollection is for collecting and verifying order details.
	ModeOrderCollection ConversationMode = "order_collection"

	// ModePaymentAwareness is for handling payment intent. Always escalates.
	ModePaymentAwareness ConversationMode = "payment_awareness"

	// ModeEscalation is the terminal state. AI hands off to human and stops.
	ModeEscalation ConversationMode = "escalation"
)

// ConversationState is kept for backward compatibility, maps to Mode
type ConversationState string

const (
	StateNew             ConversationState = "new"              // Maps to ModeDiscovery
	StateGreeting        ConversationState = "greeting"         // Maps to ModeDiscovery
	StateBrowsing        ConversationState = "browsing"         // Maps to ModeSupport
	StateOrdering        ConversationState = "ordering"         // Maps to ModeOrderCollection
	StateNegotiating     ConversationState = "negotiating"      // Maps to ModeNegotiation
	StateAwaitingPayment ConversationState = "awaiting_payment" // Maps to ModePaymentAwareness
	StateCompleted       ConversationState = "completed"        // Post-escalation
	StateEscalated       ConversationState = "escalated"        // Maps to ModeEscalation
)

// ============================================================
// MEMORY
// ============================================================

// ConversationMemory stores conversation state and history
type ConversationMemory struct {
	ConversationID    uuid.UUID
	Messages          []Message
	CustomerName      string
	CustomerPhone     string
	ActiveOrderID     *uuid.UUID
	LastIntent        Intent
	State             ConversationState
	Mode              ConversationMode // Current mode in the state machine
	IsEscalated       bool             // True if AI has handed off to human
	NegotiationRounds int
	LastOfferAmount   float64
	Metadata          map[string]any
}

// Fix 6 (Set 10): Canonical Order Snapshot
type OrderSnapshot struct {
	OrderID  uuid.UUID   `json:"order_id"`
	Items    []OrderItem `json:"items"`
	Subtotal float64     `json:"subtotal"`
	Tax      float64     `json:"tax"`
	Total    float64     `json:"total"`
	Currency string      `json:"currency"`
	Status   string      `json:"status"` // collecting | confirmed | paid
}

type OrderItem struct {
	Name      string  `json:"name"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
	LineTotal float64 `json:"line_total"`
}

// Message represents a single message in conversation history
type Message struct {
	Role      MessageRole
	Content   string
	Timestamp time.Time
}

// MessageRole identifies message sender
type MessageRole string

const (
	RoleCustomer  MessageRole = "customer"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
)

// ============================================================
// CONTEXT
// ============================================================

// ResolvedContext contains all data needed for AI processing
type ResolvedContext struct {
	// Business data (authoritative, read-only)
	Business  BusinessProfile
	Offerings []Offering
	Payment   PaymentConfig

	// Customer data
	Customer CustomerInfo
	Memory   *ConversationMemory

	// Current request
	Platform string
	Message  string

	// Computed signals
	Signals *ContextSignals
}

// CatalogKind defines the type of catalog the business operates (Step 1)
type CatalogKind string

const (
	CatalogMenu    CatalogKind = "menu"
	CatalogProduct CatalogKind = "product"
	CatalogService CatalogKind = "service"
)

// CatalogKindFromIndustry maps industry string to catalog type
// Uses case-insensitive substring matching for flexibility
func CatalogKindFromIndustry(industry string) CatalogKind {
	lower := strings.ToLower(industry)

	// Menu-based businesses
	if strings.Contains(lower, "food") || strings.Contains(lower, "restaurant") ||
		strings.Contains(lower, "cafe") || strings.Contains(lower, "fast food") ||
		strings.Contains(lower, "bakery") || strings.Contains(lower, "beverage") ||
		strings.Contains(lower, "catering") || strings.Contains(lower, "kitchen") {
		return CatalogMenu
	}

	// Service-based businesses
	// IMPORTANT: Many service businesses include words like "tech" in their industry name
	// (e.g. "Tech Services", "IT Services"). Treat explicit service/agency indicators as Service.
	if strings.Contains(lower, "service") || strings.Contains(lower, "services") ||
		strings.Contains(lower, "agency") || strings.Contains(lower, "studio") ||
		strings.Contains(lower, "consult") || strings.Contains(lower, "consulting") ||
		strings.Contains(lower, "freelance") || strings.Contains(lower, "contractor") ||
		strings.Contains(lower, "marketing") || strings.Contains(lower, "branding") ||
		strings.Contains(lower, "design") || strings.Contains(lower, "creative") ||
		strings.Contains(lower, "photography") || strings.Contains(lower, "videography") ||
		strings.Contains(lower, "events") || strings.Contains(lower, "event") ||
		strings.Contains(lower, "logistics") || strings.Contains(lower, "cleaning") ||
		strings.Contains(lower, "repair") || strings.Contains(lower, "maintenance") ||
		strings.Contains(lower, "training") || strings.Contains(lower, "tutoring") ||
		strings.Contains(lower, "legal") || strings.Contains(lower, "accounting") ||
		strings.Contains(lower, "health") || strings.Contains(lower, "beauty") ||
		strings.Contains(lower, "spa") || strings.Contains(lower, "salon") {
		return CatalogService
	}

	// Product-based businesses
	if strings.Contains(lower, "ecommerce") || strings.Contains(lower, "e-commerce") ||
		strings.Contains(lower, "retail") || strings.Contains(lower, "fashion") ||
		strings.Contains(lower, "electronics") ||
		strings.Contains(lower, "gadget") || strings.Contains(lower, "store") ||
		strings.Contains(lower, "shop") || strings.Contains(lower, "product") ||
		strings.Contains(lower, "phone") || strings.Contains(lower, "computer") ||
		strings.Contains(lower, "accessory") || strings.Contains(lower, "accessories") {
		return CatalogProduct
	}

	// Default to Service
	return CatalogService
}

// CatalogQueryMode defines the mode of catalog interaction (Step 2)
type CatalogQueryMode string

const (
	CatalogBrowse CatalogQueryMode = "browse"
	CatalogDetail CatalogQueryMode = "detail"
)

// NormalizeCatalogIntent translates raw intents to the correct catalog type (Step 2)
func NormalizeCatalogIntent(rawIntent string, catalog CatalogKind) string {
	switch catalog {
	case CatalogMenu:
		if rawIntent == "menu_request" || rawIntent == "browse_request" {
			return "menu_request"
		}
	case CatalogProduct:
		if rawIntent == "menu_request" || rawIntent == "browse_request" {
			return "product_request"
		}
	case CatalogService:
		if rawIntent == "menu_request" || rawIntent == "browse_request" {
			return "service_request"
		}
	}
	return rawIntent
}

// BusinessProfile contains business information
type BusinessProfile struct {
	ID          uuid.UUID
	Name        string
	Industry    string
	Description string
	Tone        string
	Timezone    string
	CatalogKind CatalogKind // Populated from Industry
}

// Offering represents a product, menu item, or service
type Offering struct {
	ID                  uuid.UUID
	Type                string // product, menu_item, service
	Name                string
	Description         string
	Price               float64
	Currency            string
	Category            string
	NegotiationMinPrice float64
	NegotiationMaxPrice float64
	IsAvailable         bool
	ImageURL            string
}

// PaymentConfig contains payment settings
type PaymentConfig struct {
	BankName      string
	AccountNumber string
	AccountName   string
	Methods       []string // bank_transfer, cash, card, mobile_money
}

// CustomerInfo contains customer details
type CustomerInfo struct {
	ID          uuid.UUID
	Name        string
	Phone       string
	DisplayName string
}

// ContextSignals contains computed signals about the conversation.
// Used by the state machine to determine mode transitions.
type ContextSignals struct {
	// Emotion/engagement signals
	DetectedEmotion Emotion
	EngagementLevel EngagementLevel

	// Order/product signals
	HasActiveOrder      bool
	ProductsDiscussed   []string
	PriceRangeMentioned bool
	ItemsSelected       bool // Fix 2: Explicitly signal implicit item selection
	OrderFlowLocked     bool // Fix 6: Lock flow to order execution
	ExplicitCommit      bool // Fix 2 (Set 3): Explicit "I want to order" signal
	OrderCommitted      bool // Fix 1 (Set 7): Explicit "Yes/Proceed" confirmation of order

	// State machine transition signals
	HasBuyingIntent       bool // Customer wants to purchase
	NegotiationDeadlock   bool // Negotiation went too many rounds or customer frustrated
	AIConfidenceLow       bool // AI is unsure how to proceed
	HumanRequested        bool // Customer asked for human help
	PaymentIntentDetected bool // Payment-related action detected
	PaymentRequested      bool // Explicit payment request
	PaymentProofReceived  bool // Customer says they have paid
	RefundRequested       bool // Refund or complaint detected
}

// Emotion represents detected customer emotion
type Emotion string

const (
	EmotionHappy      Emotion = "happy"
	EmotionNeutral    Emotion = "neutral"
	EmotionFrustrated Emotion = "frustrated"
	EmotionAngry      Emotion = "angry"
	EmotionConfused   Emotion = "confused"
)

// EngagementLevel represents customer engagement
type EngagementLevel string

const (
	EngagementHigh   EngagementLevel = "high"
	EngagementMedium EngagementLevel = "medium"
	EngagementLow    EngagementLevel = "low"
)

// ============================================================
// POLICY
// ============================================================

// PolicyDecision is the result of policy evaluation
type PolicyDecision struct {
	MustEscalate   bool
	EscalateReason string
	AllowedActions []ActionType
	BlockedActions []ActionType
	Constraints    *ActionConstraints
}

// ActionConstraints defines limits on AI behavior
type ActionConstraints struct {
	MaxDiscount     float64 // For negotiation
	RequireApproval []ActionType
	ForbiddenTopics []string
}

// ============================================================
// LLM CLIENT INTERFACE
// ============================================================

// LLMClient is the interface for LLM operations
// LLMClient is the interface for LLM operations
type LLMClient interface {
	// ClassifyIntent classifies customer intent from message
	ClassifyIntent(message string, context string) (*IntentResult, error)

	// Generate generates a response text
	Generate(prompt string) (string, error)

	// ExtractEntities extracts entities from message
	ExtractEntities(message string) (map[string]any, error)

	// GenerateWithContext generates a context-aware response using V4 endpoint
	GenerateWithContext(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)

	// InterpretSemantic calls the dedicated semantic understanding endpoint
	InterpretSemantic(ctx context.Context, message string, lastIntent string, businessType string) (*SemanticUnderstanding, error)
}

// GenerateRequest matches Python /api/v4/generate request format
type GenerateRequest struct {
	Message             string         `json:"message"`
	BusinessName        string         `json:"business_name"`
	BusinessDescription string         `json:"business_description"`
	Industry            string         `json:"industry"`
	BusinessTone        string         `json:"business_tone"`
	CustomerName        string         `json:"customer_name"`
	Platform            string         `json:"platform"`
	ConversationState   string         `json:"conversation_state"`
	ConversationMode    string         `json:"conversation_mode"`
	Intent              string         `json:"intent"`
	CartSummary         string         `json:"cart_summary,omitempty"`
	OrderSummary        string         `json:"order_summary,omitempty"`
	ProductsContext     string         `json:"products_context,omitempty"`
	BaseReply           string         `json:"base_reply,omitempty"`
	MustInclude         []string       `json:"must_include,omitempty"`
	NegotiationCtx      map[string]any `json:"negotiation_context,omitempty"`
}

// GenerateResponse matches Python /api/v4/generate response format
type GenerateResponse struct {
	Reply            string   `json:"reply"`
	Emotion          string   `json:"emotion"`
	Confidence       float64  `json:"confidence"`
	SuggestedActions []string `json:"suggested_actions"`
	ProcessingMs     int      `json:"processing_ms"`
}

// EmotionRequest for /api/v4/emotion endpoint
type EmotionRequest struct {
	Message string `json:"message"`
	Context string `json:"context,omitempty"`
}

// EmotionResponse from /api/v4/emotion endpoint
type EmotionResponse struct {
	Emotion    string  `json:"emotion"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// SplitRequest for /api/v4/split endpoint
type SplitRequest struct {
	Message   string `json:"message"`
	Platform  string `json:"platform"`
	MaxLength int    `json:"max_length"`
}

// SplitResponse from /api/v4/split endpoint
type SplitResponse struct {
	Messages       []string `json:"messages"`
	OriginalLength int      `json:"original_length"`
	ChunkCount     int      `json:"chunk_count"`
}

// NegotiateRequest for /api/v4/negotiate endpoint
type NegotiateRequest struct {
	CustomerOffer    float64 `json:"customer_offer"`
	ListPrice        float64 `json:"list_price"`
	MinPrice         float64 `json:"min_price"`
	CustomerName     string  `json:"customer_name"`
	NegotiationRound int     `json:"negotiation_round"`
	BusinessTone     string  `json:"business_tone"`
}

// NegotiateResponse from /api/v4/negotiate endpoint
type NegotiateResponse struct {
	Reply        string   `json:"reply"`
	CounterOffer *float64 `json:"counter_offer,omitempty"`
	Action       string   `json:"action"` // accept, counter, reject, escalate
}

// ============================================================
// SEMANTIC UNDERSTANDING
// ============================================================

// SemanticUnderstanding represents the soft meaning of a user's message,
// distinct from hard Intent classifications.
type SemanticUnderstanding struct {
	Meaning         string  `json:"meaning"`          // Summary of what they are saying
	UserGoal        string  `json:"user_goal"`        // greeting, business_info, support, order, payment, unknown
	UserTone        string  `json:"user_tone"`        // DEPRECATED: Handled by Meaning/Goal implicitly
	ReferencedTopic string  `json:"referenced_topic"` // Deprecated but carrying over for now
	SuggestedIntent string  `json:"suggested_intent"` // INQUIRY, GREETING, PURCHASE, SUPPORT, UNKNOWN
	Confidence      float64 `json:"confidence"`
}

// ============================================================
// CONTEXT KEYS (For passing platform-specific IDs through context)
// ============================================================

// ContextKey is a type for context keys to avoid collisions
type ContextKey string

const (
	// CtxKeyThreadID is the context key for the platform-specific thread/recipient ID
	// For Instagram: This is the IGSID (Instagram-Scoped ID)
	// For WhatsApp: This is the customer's phone number
	// For Telegram: This is the chat ID
	CtxKeyThreadID ContextKey = "brancr_thread_id"

	// CtxKeyPlatformCustomerID is the context key for the platform-specific customer ID
	// This is used when threadID differs from customerID (e.g., Instagram)
	CtxKeyPlatformCustomerID ContextKey = "brancr_platform_customer_id"
)

// WithThreadID adds the threadID to the context
func WithThreadID(ctx context.Context, threadID string) context.Context {
	return context.WithValue(ctx, CtxKeyThreadID, threadID)
}

// GetThreadID retrieves the threadID from the context
func GetThreadID(ctx context.Context) (string, bool) {
	if v := ctx.Value(CtxKeyThreadID); v != nil {
		if s, ok := v.(string); ok {
			return s, true
		}
	}
	return "", false
}

// WithPlatformCustomerID adds the platform customer ID to the context
func WithPlatformCustomerID(ctx context.Context, customerID string) context.Context {
	return context.WithValue(ctx, CtxKeyPlatformCustomerID, customerID)
}

// GetPlatformCustomerID retrieves the platform customer ID from the context
func GetPlatformCustomerID(ctx context.Context) (string, bool) {
	if v := ctx.Value(CtxKeyPlatformCustomerID); v != nil {
		if s, ok := v.(string); ok {
			return s, true
		}
	}
	return "", false
}
