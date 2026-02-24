package domain

import "time"

type ConversationState struct {
	SessionID             string
	CustomerID            string
	LastIntent            string
	MessageCount          int
	LastMessageTime       time.Time // User's last message time
	LastAIMessageAt       time.Time // Step 7: AI's last message time
	HasAskedQualification bool      // Step 2: Persisted flag
	ReflectionUsed        bool      // Step 3: Track if reflection was used this turn
	ObjectionNormalized   bool      // Step 3: Track if objection was normalized
	IsRefusal             bool      // Step 4: Track if conversation is in refusal state
	IsBoundaryResponse    bool      // Step 5: Track if boundary response was used
	HasClosed             bool      // Step 6: Track if clean closure was applied
	HasPendingAction      bool      // Step 6: Track if there is a pending action (transient)
	HasReengaged          bool      // Step 7: Track if silence re-engagement has occurred
	NegotiationRounds     int       // Step 8: Track negotiation intensity/duration
	HasShownOfferings     bool      // Fix 2.1 State Guard
	Emotion               string    // Fix 2.5: Urgent/Frustrated detection
	HasRecommended        bool      // Fix 3.9: Recommendation state guard

	// Fix 4: Escalation State
	ExplicitHumanRequest bool // User explicitly asked for human
	FrustrationTurns     int  // Count contentiously frustrated turns
	HasProgress          bool // Track negotiation progress
	IsHighRiskTopic      bool // Legal, refunds, etc.
	LowConfidence        bool // System confidence flag
	HasSuggestedHuman    bool // Guide rail
	IsHandedOff          bool // Terminal state

	// Fix 4.3: Insight Status
	InsightStatus string // "fresh", "stale", "unknown"

	// Guard 3: Single Source of Truth
	LastShownOfferings []Offering // Persisted offerings from last shown menu

	// Step 11: Silence Handling (Step 5)
	SilenceState SilenceState // Defined in domain/silence.go

	// Step 12: Confidence & Emotion (Step 6)
	ConfidenceLevel ConfidenceLevel
	TurnCount       int
	DetectedStyle   string // CASUAL, FORMAL, NEUTRAL (Phase 18)
	// NegotiationRounds already exists (line 20)

	// Phase 1: Transaction State (New Architecture)
	// Determines memory visibility, independent of intent.
	TransactionState TransactionState

	// Phase 5: Commitment Tracking
	CommittedAt *time.Time // Set when entering Committed, cleared on reversal

	// Phase 7: Payment Finalization
	PaidAt *time.Time // Set when entering Paid, never cleared

	// Phase 4.8: Confirmation Latch (Prevents Confirmation Loops)
	CartConfirmed   bool       // Cart snapshot has been confirmed
	CartHash        string     // Hash of confirmed cart (ProductID + Quantity + Variants)
	CartConfirmedAt *time.Time // When cart was confirmed

	// V3 Consolidation: Product Binding (from V2 ActiveProduct)
	ActiveProductID   string // Currently discussed product ID (locked during negotiation)
	ActiveProductName string // Currently discussed product name
	ProductLocked     bool   // Product cannot be switched during negotiation
}

// TransactionState represents the progress of a customer transaction.
// It determines what context remains visible to the AI.
type TransactionState string

const (
	// TransactionBrowsing: Customer exploring menu or asking questions.
	// Context: Catalog, general info.
	TransactionBrowsing TransactionState = "browsing"

	// TransactionCommitted: Customer has verbally confirmed items.
	// Context: Cart limits, order details.
	TransactionCommitted TransactionState = "committed"

	// TransactionAwaitingPayment: Payment method discussed but not settled.
	// Context: Payment instructions, exact amount.
	TransactionAwaitingPayment TransactionState = "awaiting_payment"

	// TransactionPaid: Transaction completed.
	// Context: Receipt, post-purchase info.
	TransactionPaid TransactionState = "paid"
)

const (
	InsightStatusFresh   = "fresh"
	InsightStatusStale   = "stale"
	InsightStatusUnknown = "unknown"
)
