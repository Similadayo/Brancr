package v3

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/ai/response"
	"github.com/similadayo/brancr/internal/conversation"
	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/persona"
	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/tenant"
	"gorm.io/gorm"
)

// =============================================================================
// CORE TYPES - V3 AI Conversation Pipeline
// =============================================================================

// ConversationRequest is the input to the pipeline
type ConversationRequest struct {
	TenantID       uuid.UUID
	CustomerID     uuid.UUID
	ConversationID uuid.UUID
	Message        string
	Platform       string // "whatsapp", "instagram", "facebook", "web"
	Timestamp      time.Time
}

// Response is the output from the pipeline
type Response struct {
	Messages       []string // 1-3 short messages
	MediaURLs      []string // Product images if relevant
	QuickReplies   []string // Suggested next actions
	Metadata       ResponseMetadata
	HiddenCommands []HiddenCommand // Commands extracted from LLM response
}

type HiddenCommand struct {
	Type    string // "ORDER", "DELIVERY", "PAY_METHOD"
	Payload string // JSON payload
}

// ResponseMetadata contains non-user-facing data
type ResponseMetadata struct {
	IntentType   IntentType
	ActionType   ActionType
	OrderID      *uuid.UUID
	Confidence   float64
	Goal         conversation.Goal
	ResponseMode ResponseMode
	ProcessingMs int64
}

// =============================================================================
// CONTEXT TYPES - RAG, Memory, Role
// =============================================================================

// ConversationContext contains all grounded knowledge for a conversation
type ConversationContext struct {
	// Identity
	TenantID       uuid.UUID
	CustomerID     uuid.UUID
	ConversationID uuid.UUID
	Platform       string

	// Intent: The driver of the current turn
	CurrentIntent IntentType

	// RAG: Grounded knowledge (from DB/vector store)
	Products  []Product
	Services  []Service
	FAQs      []FAQ
	Policies  []Policy
	Grounding GroundingResult

	// Memory: Conversation continuity
	RecentMessages []Message

	// PendingCart: In-memory cart (NOT saved to DB until payment request)
	// This is the cart that AI builds conversationally
	PendingCart []PendingCartItem

	// Emotion: Detected from current message
	Emotion EmotionState

	// Business Info
	BusinessName        string
	BusinessType        string // Industry name (e.g., "Food & Beverage", "E-commerce")
	BusinessDescription string // What the business does
	BusinessTone        string // "casual", "professional" (from DB)
	BusinessNiche       string // Specific niche
	OperatingHours      string // When the business is open
	Location            string // Where the business is located
	Website             string // Business website
	KnowledgeBase       string // Custom knowledge/info the business wants AI to know
	PaymentInfo         *PaymentConfig
	DeliveryConfig      *storage.DeliveryConfig // From Tenant Configuration
	DeliveryState       *DeliveryState          // Current conversation state

	// Negotiation State
	SalesConversation *storage.SalesConversation

	// Industry Info - determines what type of offerings the business has
	IndustryCategory    string // retail, food, professional_services
	IndustryHasProducts bool   // E-commerce products
	IndustryHasMenu     bool   // F&B menu items
	IndustryHasServices bool   // Consulting/Agency services

	// Conversation Obligations - tracks what's been answered to prevent loops
	Obligations *ObligationsTracker

	// V2 Integration: State-driven behavior
	// Models (Raw)
	CurrentOrder    *Order
	CustomerHistory *CustomerData
	RoleProfile     RoleProfile // Legacy, migrating to RuntimeProfile

	// Tenant Reality (New Phase 15)
	RuntimeProfile tenant.RuntimeProfile

	// State Integration (V2)
	StructuredState      interface{} // v2.StructuredConversationState
	CompiledKnowledge    interface{} // v2.CompiledTenantKnowledge
	KnowledgeBaseContent string      // Raw text content for convenient RAG

	// VoiceFingerprint: Learnt communication style
	// Added in Phase 3
	VoiceFingerprint *storage.VoiceFingerprint

	// CurrentPhase tracks conversation phase (derived from StructuredState)
	CurrentPhase string // greeting, discovery, assistance, transaction, follow_up

	// ActiveRole determines AI behavior mode
	ActiveRole string // sales_rep, customer_service, order_manager, negotiator

	// =========================================================================
	// PHASE 1: ALIVE ASSISTANT (Free Response)
	// =========================================================================
	// AllowFreeResponse = !RequiresTransaction && !RequiresHuman
	// If true, AI ignores rigid templates and uses grounded knowledge to reply.
	AllowFreeResponse bool

	// =========================================================================
	// PHASE 1: HUMAN-LEVEL MENTAL MODEL
	// =========================================================================

	// CommitmentGradient: 0.0 (Browsing) -> 1.0 (Paid)
	// Determines if we can perform transactional actions
	CommitmentGradient float64

	// ConversationPhase: The "macro" state of the conversation
	Phase ConversationPhase // discovery, comparison, negotiation, fulfillment

	// PHASE 2: CONVERSATION MODE (Strict State)
	Mode ConversationMode // discovery, ordering, confirming, payment

	// PHASE 1: AUTHORITATIVE STATE
	StateSnapshot *OrderContextSnapshot

	// PHASE 1: Transaction State (Passive Tracking)
	TransactionState domain.TransactionState

	// EmotionalTone: The "vibe" of the customer
	EmotionalTone string

	// UserSignals: Replaces "Intent" as the primary driver (Analysis output)
	UserSignals UserSignals

	// LastSocialMove: What the AI did socially in the last turn
	// Used to prevent repetition and know when to stay silent
	LastSocialMove SocialMove

	// ExpectedNextMove: What the customer anticipates next
	// Used to ensure AI responds to expectations, not just intent
	ExpectedNextMove SocialExpectation
}

// ConversationPhase represents the macro flow of the conversation
type ConversationPhase string

const (
	PhaseDiscovery    ConversationPhase = "discovery"    // What do you sell?
	PhaseComparison   ConversationPhase = "comparison"   // How much is X vs Y?
	PhaseNegotiation  ConversationPhase = "negotiation"  // Can you do better price?
	PhaseCommitment   ConversationPhase = "commitment"   // I'll take it / Send acct
	PhasePayment      ConversationPhase = "payment"      // Paying now
	PhaseFulfillment  ConversationPhase = "fulfillment"  // Phase 6: Post-payment reassurance
	PhaseRelationship ConversationPhase = "relationship" // Just chatting / praise
)

// UserSignals wraps the "Think" layer output
// Demotes "Intent" to just one of many signals
type UserSignals struct {
	PrimaryIntent  Intent // The old Intent struct
	RawIntent      string // The string representation
	SocialSignals  SocialSignals
	Momentum       float64 // -1.0 (Stalled) to +1.0 (Accelerating)
	RiskAssessment []string
}

// SocialSignals captures non-transactional cues
type SocialSignals struct {
	IsFriend  bool
	IsPlayful bool
	HasEmoji  bool
	Urgency   string // low, medium, high
}

// PendingCartItem represents an item in the in-memory cart
// This is held in memory until customer asks for payment
type PendingCartItem struct {
	ProductID   uuid.UUID
	ProductName string
	Quantity    int
	UnitPrice   float64
	Currency    string
	Modifiers   []string // e.g. ["Extra Cheese", "No Onions"]
	Description string   // Formatted details
}

// Product represents a tenant's catalog item
type Product struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Price       float64   `json:"price"`
	Currency    string    `json:"currency"`
	Category    string    `json:"category"`
	ImageURL    string    `json:"image_url"`
	InStock     bool      `json:"in_stock"`
}

// Service represents a tenant's service offering
type Service struct {
	ID          uuid.UUID
	Name        string
	Description string
	Price       float64
	Currency    string
	Duration    string // e.g. "1 hour"
}

// FAQ represents a pre-approved answer
type FAQ struct {
	Question string
	Answer   string
	Keywords []string
}

// Policy represents a business rule
type Policy struct {
	Type    string // "return", "delivery", "payment", etc.
	Content string
}

// Message represents a conversation message
type Message struct {
	Role      string // "customer", "assistant"
	Content   string
	Timestamp time.Time
}

// Order represents the current order state
type Order struct {
	ID          uuid.UUID
	OrderNumber string
	State       OrderState
	Items       []OrderItem
	Total       float64
	Currency    string
	Address     string
	CreatedAt   time.Time
	Snapshot    *OrderSnapshot // Phase 3 (Old): Immutable items for Confirm
}

// =============================================================================
// PHASE 1-3: CONVERSATIONAL REALITY STRUCTS
// =============================================================================

// ConversationMode defines the strict behavioral mode of the system
type ConversationMode string

const (
	ModeDiscovery    ConversationMode = "discovery"    // Recommendations allowed
	ModeOrdering     ConversationMode = "ordering"     // Recommendations DISABLED, Cart mutable
	ModeConfirming   ConversationMode = "confirming"   // Summaries only, Cart locked
	ModeRequirements ConversationMode = "requirements" // Collecting fulfillment requirements (products/menu)
	ModePayment      ConversationMode = "payment"      // Locked for payment
)

// OrderContextSnapshot is the SINGLE SOURCE OF TRUTH for the conversation.
// The LLM must grounded in this, never inferring from text.
type OrderContextSnapshot struct {
	ConversationMode ConversationMode

	ItemsConfirmed bool
	Items          []OrderItem
	TotalQuantity  int
	TotalAmount    float64

	// RejectedItems tracks what user explicitly said "No" to.
	// Map of ProductID (string) -> true
	RejectedItems map[string]bool

	Locked bool
}

// OrderMutationRequest enforces the "No Surprise" contract.
// Only explicit user statements can mutate the order.
type OrderMutationRequest struct {
	Source        string // "user" | "system"
	Explicit      bool   // MUST be true to mutate (e.g. "Add 2", not just "Yes")
	ParsedFromMsg string
	Intent        IntentType
}

// OrderItem represents an item in the order
type OrderItem struct {
	ProductID   uuid.UUID
	ProductName string
	Quantity    int
	UnitPrice   float64
	TotalPrice  float64
}

// CustomerData represents customer history
type CustomerData struct {
	Name              string
	Phone             string
	Email             string
	PreviousOrders    int
	TotalSpend        float64
	PreferredProducts []string
	LastOrderDate     *time.Time
	Insights          *CustomerInsights // Phase 8: Long-term memory
}

// CustomerInsights captures long-term memory about the customer
// Fetching from storage.CustomerInsight
type CustomerInsights struct {
	Summary           string   // "Price-sensitive, prefers delivery"
	BuyerType         string   // "premium", "budget", "undecided"
	PreferredProducts []string // Inferred from past orders
}

// PaymentConfig represents tenant payment settings
type PaymentConfig struct {
	BankName      string
	AccountName   string
	AccountNumber string
	Provider      string // "paystack", "flutterwave", etc.
}

// =============================================================================
// ROLE PROFILE TYPES
// =============================================================================

// RoleProfile defines industry-specific behavior
type RoleProfile struct {
	Industry        string
	Tone            string // Legacy: "casual", "professional"
	TonePreset      persona.TonePreset
	DefaultResponse string // IntentType string (e.g. "product_advisory")
	MaxMsgLen       int
	UpsellOK        bool
	Emojis          bool
	Phrases         PhraseSet
	Behavior        RoleBehavior        // Phase 9: Industry-specific behavior flags
	Style           ConversationalStyle // Phase 11: Human Presence Layer
}

// Phase 11: Human Presence Layer
type ConversationalStyle struct {
	Verbosity string // "low" (bursts), "medium", "high"
	Pace      string // "fast", "relaxed", "delayed"
	Warmth    string // "neutral", "friendly", "empathetic"
}

// RoleBehavior defines structural behavior changes per industry
type RoleBehavior struct {
	ShowMenuOnGreet  bool // Food: Show menu immediately on hello
	AskDimensions    bool // Fashion: Ask for size early
	ProactiveSupport bool // Consulting: "How can I help" vs sales push
}

// PhraseSet contains industry-specific phrases
type PhraseSet struct {
	Greeting []string
	Confirm  []string
	Empathy  []string
	Error    []string
	TimeEst  []string
}

// =============================================================================
// EMOTION TYPES
// =============================================================================

// EmotionType represents detected customer emotion
type EmotionType string

const (
	EmotionNeutral    EmotionType = "neutral"
	EmotionFrustrated EmotionType = "frustrated"
	EmotionEager      EmotionType = "eager"
	EmotionConfused   EmotionType = "confused"
	EmotionHappy      EmotionType = "happy"
	EmotionHesitant   EmotionType = "hesitant"
)

// EmotionState represents the detected emotional state
type EmotionState struct {
	Type      EmotionType
	Intensity float64 // 0.0 - 1.0
}

// =============================================================================
// INTENT TYPES
// =============================================================================

// IntentType represents what the customer wants
type IntentType string

const (
	// Commands (highest priority)
	IntentCancel   IntentType = "cancel"
	IntentHelp     IntentType = "help"
	IntentGreeting IntentType = "greeting"

	// Cart operations
	IntentAddItem    IntentType = "add_item"
	IntentRemoveItem IntentType = "remove_item"
	IntentViewCart   IntentType = "view_cart"
	IntentClearCart  IntentType = "clear_cart"

	// Checkout flow
	IntentCheckout       IntentType = "checkout"
	IntentPaymentQuery   IntentType = "payment_query"
	IntentConfirmPayment IntentType = "confirm_payment"

	// Confirmation Flow
	IntentAffirm IntentType = "affirm" // "Yes", "Okay", "Go ahead"
	IntentReject IntentType = "reject" // "No", "Wait", "Cancel"

	// Inquiries
	IntentProductInquiry  IntentType = "product_inquiry"
	IntentProductAdvisory IntentType = "product_advisory" // "What should I buy?"
	IntentServiceInquiry  IntentType = "service_inquiry"  // "Tell me about consulting"
	IntentPriceInquiry    IntentType = "price_inquiry"
	IntentStatusInquiry   IntentType = "status_inquiry"

	// Negotiation
	IntentNegotiation IntentType = "negotiation"

	// Fallback
	IntentContextual IntentType = "contextual" // Replaces Unknown: "We know the context, so we answer"
	IntentUnknown    IntentType = "unknown"    // DEPRECATED: Do not use in new logic
)

// Intent represents the classified user intent
type Intent struct {
	Type       IntentType
	Confidence float64
	Entities   Entities
	Source     string // "heuristic", "llm"
	Emotion    EmotionState
}

// Entities represents extracted entities from the message
type Entities struct {
	Product  *ProductEntity
	Quantity int
	Address  string
	Phone    string
	Custom   map[string]string
}

// ProductEntity represents a matched product
type ProductEntity struct {
	ID       uuid.UUID
	Name     string
	Price    float64
	Currency string
	Matched  string // Original text that matched
}

// =============================================================================
// ORDER STATE MACHINE
// =============================================================================

// OrderState represents the order lifecycle
type OrderState string

const (
	StateIdle            OrderState = "idle"             // No active order
	StateBrowsing        OrderState = "browsing"         // Looking at products
	StateDraft           OrderState = "draft"            // Cart has items (was Building)
	StateCheckout        OrderState = "checkout"         // Address collected
	StateQuoted          OrderState = "quoted"           // Pro-Services quote generated
	StateConfirmed       OrderState = "confirmed"        // User said "Yes" to summary - LOCKED
	StateAwaitingPayment OrderState = "awaiting_payment" // Payment instruction sent (was Payment)
	StatePaid            OrderState = "paid"             // Payment verified
	StateCompleted       OrderState = "completed"        // Order fulfilled
	StateCancelled       OrderState = "cancelled"        // Order cancelled

	// STRICT ALIASES (User Requirement)
	OrderIdle      OrderState = StateIdle
	OrderDraft     OrderState = StateDraft
	OrderQuoted    OrderState = StateQuoted
	OrderConfirmed OrderState = StateConfirmed
	OrderPaying    OrderState = StateAwaitingPayment
	OrderCompleted OrderState = StateCompleted

	// Aliases for backward compatibility
	StateBuilding OrderState = StateDraft
	StatePayment  OrderState = StateAwaitingPayment
)

// PHASE 4: OBLIGATION TRACKING
// Assistant can only ask ONE obligation at a time.
type Obligation struct {
	Question string
	Type     string // price, delivery, payment, confirmation
	Answered bool
}

// OrderSnapshot represents the frozen contract of a confirmed order
// The LLM is NOT allowed to recalculate this.
type OrderSnapshot struct {
	Items    []OrderItem
	Total    float64
	Currency string
	LockedAt time.Time
}

// =============================================================================
// ACTION TYPES
// =============================================================================

// ActionType represents what the system should do
type ActionType string

const (
	// Responses
	ActionGreet            ActionType = "greet"
	ActionShowProducts     ActionType = "show_products" // Generic fallback
	ActionShowMenu         ActionType = "show_menu"     // Explicit listing
	ActionPitchProduct     ActionType = "pitch_product" // Specific recommendation
	ActionShowCart         ActionType = "show_cart"
	ActionAskClarification ActionType = "ask_clarification"
	ActionAnswerQuestion   ActionType = "answer_question"
	ActionFreeResponse     ActionType = "free_response" // Phase 1: Alive Assistant (Grounded Chat)

	// Acknowledgement
	ActionAck ActionType = "ack"

	// Cart mutations
	ActionAddDiscount    ActionType = "add_discount" // Apply a discount
	ActionAddToCart      ActionType = "add_to_cart"
	ActionRemoveFromCart ActionType = "remove_from_cart"
	ActionClearCart      ActionType = "clear_cart"
	ActionRejectItem     ActionType = "reject_item" // Phase 5: Never mention again

	// Checkout
	ActionCollectAddress ActionType = "collect_address"
	ActionShowPayment    ActionType = "show_payment"
	ActionConfirmOrder   ActionType = "confirm_order"

	// Order management
	ActionCancelOrder ActionType = "cancel_order"
	ActionShowStatus  ActionType = "show_status"

	// Escalation
	ActionEscalate ActionType = "escalate"

	// Negotiation
	ActionNegotiate ActionType = "negotiate"

	// Strategic Silence
	ActionDefer ActionType = "defer" // Phase 11: "Let me check...", "One sec"

	// Conversion/Legacy Leaks
	ActionExplainPrice ActionType = "EXPLAIN_PRICE" // From Rules Engine

	// Requirements collection (pre-payment for products/menu)
	ActionCollectRequirements ActionType = "collect_requirements"

	// Error
	ActionError ActionType = "error"
)

// DeliveryState tracks the delivery/payment selection flow
type DeliveryState struct {
	Method        string                // "delivery" or "pickup"
	Address       string                // Full address if delivery
	SelectedArea  *storage.DeliveryArea // The matched area
	DeliveryFee   float64
	PaymentMethod string // "pod" or "prepaid" (bank_transfer/card)
	IsComplete    bool   // True when all info is collected
}

// Action represents the resolved action to take
type Action struct {
	Type       ActionType
	Allowed    bool
	Reason     string
	RequiresTx bool // Needs DB transaction?
	Data       ActionData
}

// ActionData contains action-specific parameters
type ActionData struct {
	ProductID uuid.UUID
	Quantity  int
	Address   string
	Message   string
	Explicit  bool // Phase 3/4: Must be true for mutations
}

// =============================================================================
// EXECUTION RESULT
// =============================================================================

// =============================================================================
// EXECUTION RESULT (Strict Contract)
// =============================================================================

// ExecutionResult is the sealed interface for all action outcomes
type ExecutionResult interface {
	isExecutionResult()
	ActionType() ActionType
}

// ShowProductsResult carries the product catalog
type ShowProductsResult struct {
	Products []Product
}

func (ShowProductsResult) isExecutionResult()     {}
func (ShowProductsResult) ActionType() ActionType { return ActionShowProducts }

// ShowCartResult carries the current cart state
type ShowCartResult struct {
	Items []PendingCartItem
	Total float64
}

func (ShowCartResult) isExecutionResult()     {}
func (ShowCartResult) ActionType() ActionType { return ActionShowCart }

// ShowPaymentResult requests payment UI
type ShowPaymentResult struct {
	Total              float64
	Bank               PaymentConfig
	LockedPaymentBlock string // IMMUTABLE text block the AI must recite
}

func (ShowPaymentResult) isExecutionResult()     {}
func (ShowPaymentResult) ActionType() ActionType { return ActionShowPayment }

// ShowQuoteResult displays a formal quote (for consultancy/services)
type ShowQuoteResult struct {
	Order   *Order
	Message string
}

func (ShowQuoteResult) isExecutionResult()     {}
func (ShowQuoteResult) ActionType() ActionType { return ActionShowPayment } // Reuses show payment action type for now? Or ActionShowStatus?
// Let's create a new ActionType ActionShowQuote if needed, or reuse.
// Ideally ActionShowPayment triggers it, but the RESULT differentiates the UI.

// AckResult is a generic acknowledgement (e.g. "Item added")
type AckResult struct {
	Message string
}

func (AckResult) isExecutionResult()     {}
func (AckResult) ActionType() ActionType { return ActionAck }

// ShowStatusResult carries the order status
type ShowStatusResult struct {
	Order   *Order
	Message string
}

func (ShowStatusResult) isExecutionResult()     {}
func (ShowStatusResult) ActionType() ActionType { return ActionShowStatus }

// ErrorResult represents a failure
type ErrorResult struct {
	Error error
}

func (ErrorResult) isExecutionResult()     {}
func (ErrorResult) ActionType() ActionType { return ActionError }

// NegotiateResult carries the negotiation state
type NegotiateResult struct {
	Offer   float64
	Message string
	Phase   string
}

func (NegotiateResult) isExecutionResult()     {}
func (NegotiateResult) ActionType() ActionType { return ActionNegotiate }

// CollectRequirementsResult signals that requirements need to be collected before payment
// Used for products/menu items - not for services
type CollectRequirementsResult struct {
	Order          *Order
	Requirement    RequirementInfo // The next requirement to ask
	Message        string          // Confirmation message before asking requirement
	TotalRemaining int             // How many requirements remain
}

// RequirementInfo contains info about a requirement to collect
type RequirementInfo struct {
	ID          string
	Label       string
	Description string
	Type        string // text, file, select
}

func (CollectRequirementsResult) isExecutionResult()     {}
func (CollectRequirementsResult) ActionType() ActionType { return ActionCollectRequirements }

// =============================================================================
// RESPONSE STYLE
// =============================================================================

// ResponseStyle defines how to format the response
type ResponseStyle struct {
	Tone   string // "supportive", "enthusiastic", "professional"
	Prefix string // Optional prefix like "I hear you. "
	MaxLen int
	Emojis bool
}

// =============================================================================
// INTERFACES
// =============================================================================

// ContextBuilder gathers all context for the conversation
type ContextBuilder interface {
	Build(ctx context.Context, req ConversationRequest) (*ConversationContext, error)
	DerivePhaseAndRole(convCtx *ConversationContext)
	UpdateTransactionState(ctx context.Context, conversationID uuid.UUID, state domain.TransactionState) error
}

// IntentExtractor classifies user intent
type IntentExtractor interface {
	Classify(ctx *ConversationContext, message string) Intent
}

// ActionResolver decides what action to take
type ActionResolver interface {
	Decide(ctx *ConversationContext, intent Intent) Action
}

// ActionExecutor performs the action
type ActionExecutor interface {
	Execute(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext, action Action) ExecutionResult
	ExecuteHiddenCommands(ctx context.Context, tx *gorm.DB, convCtx *ConversationContext, commands []HiddenCommand) error
}

// ResponseComposer generates the response
type ResponseComposer interface {
	Generate(ctx *ConversationContext, action Action, result ExecutionResult) Response
}

// Orchestrater interface
type Pipeline interface {
	Process(ctx context.Context, req ConversationRequest) (*response.AIResponse, error)

	// Shadow Mode Safety Controls
	DisableExecution()
	DisableEvents()
	DisablePersistence()
}

// Negotiator defines the interface for price negotiation logic
type Negotiator interface {
	Decide(ctx context.Context, convCtx *ConversationContext, offer float64, productPrice float64) (NegotiationResult, error)
}

// NegotiationResult contains the decision from the Negotiator
type NegotiationResult struct {
	Phase   string // "concede", "hold", "escalate"
	Offer   float64
	Style   string // "playful_deflect", "generic"
	Message string
}

// =============================================================================
// CONVERSATION GROUNDING TYPES
// =============================================================================

// GroundingType represents the social/situational context of a message
type GroundingType string

const (
	GroundingGreeting      GroundingType = "greeting"
	GroundingSmallTalk     GroundingType = "small_talk"
	GroundingBrowsing      GroundingType = "browsing"
	GroundingDecisionHelp  GroundingType = "decision_help"
	GroundingConfusion     GroundingType = "confusion"
	GroundingSupport       GroundingType = "support"
	GroundingOrderProgress GroundingType = "order_progress"
	GroundingPostPurchase  GroundingType = "post_purchase"
	GroundingNone          GroundingType = ""
)

// GroundingResult captures the output of the ConversationGrounder
type GroundingResult struct {
	Type        GroundingType
	Confidence  float64
	Reason      string
	MustRespond bool
}

// =============================================================================
// GROUNDED CONTEXT V2 (Core Contract)
// =============================================================================

// ConversationType determines what kind of conversation this is
type ConversationType string

const (
	ConversationSales    ConversationType = "sales"
	ConversationSupport  ConversationType = "support"
	ConversationAdvisory ConversationType = "advisory"
	ConversationGreeting ConversationType = "greeting"
)

// EmojiBias controls emoji usage
type EmojiBias int

const (
	EmojiNone EmojiBias = iota
	EmojiLight
	EmojiExpressive
)

// SlangLevel controls slang usage
type SlangLevel int

const (
	SlangNone SlangLevel = iota
	SlangLight
	SlangLocal
)

// TenantVoiceFingerprint compiles persona into actionable voice
type TenantVoiceFingerprint struct {
	BotName    string
	Tone       string // friendly, calm, premium
	Language   string // en-NG
	Humor      bool
	EmojiBias  EmojiBias
	SlangLevel SlangLevel
	Decisive   bool // true = short sentences, no hedges
}

// MissingDatum represents data the system needs to answer
type MissingDatum struct {
	Field  string // e.g. "delivery_address"
	Prompt string // e.g. "What's your delivery address?"
}

// GroundedContext is the HARD CONTRACT for response generation
// No response can be generated without this context
type GroundedContext struct {
	// Tenant reality
	TenantID     uuid.UUID
	BusinessName string
	Industry     string

	// Available data
	Products []Product
	Services []Service
	FAQs     []FAQ
	Policies []Policy

	// Persona / voice
	Voice TenantVoiceFingerprint

	// Classification
	ConversationType ConversationType
	Answerable       bool
	MissingData      []MissingDatum
	ResponseMode     GroundingMode

	// Payment info (if available)
	PaymentInfo *PaymentConfig
}

// GroundingMode determines the grounding state logic (Legacy)
type GroundingMode string

const (
	GroundingDirect   GroundingMode = "direct"   // Answer now
	GroundingClarify  GroundingMode = "clarify"  // Ask ONE specific question
	GroundingEscalate GroundingMode = "escalate" // Human needed
)
