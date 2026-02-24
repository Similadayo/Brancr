package ai

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// SessionConversationState is the EPHEMERAL state that drives all decisions.
// This is separate from persisted SalesConversation (history/memory).
//
// KEY PRINCIPLE: Persisted state may NEVER escalate intent.
// Historical commitment ≠ current intent.
// Order existence ≠ permission to act.
type SessionConversationState struct {
	SessionID      string    `json:"session_id"`
	TenantID       uuid.UUID `json:"tenant_id"`
	CustomerID     uuid.UUID `json:"customer_id"`
	ConversationID uuid.UUID `json:"conversation_id"`

	// Current session state - resets on new session
	State           string `json:"state"`            // DISCOVERY, SALES_GUIDANCE, PAYMENT_PENDING
	CommitmentLevel string `json:"commitment_level"` // none | tentative | committed

	// UIAG Flags - User-Initiated Action Gate
	UserRequestedPayment bool `json:"user_requested_payment"` // True only if user explicitly asked
	UserRequestedOrder   bool `json:"user_requested_order"`   // True only if user explicitly asked

	// Greeting Lock
	HasGreeted bool `json:"has_greeted"` // True after first greeting exchange

	// Cold Start Gate
	IsFresh      bool `json:"is_fresh"`      // True for first message in session
	MessageCount int  `json:"message_count"` // Messages in this session

	// Timing
	SessionStartedAt time.Time `json:"session_started_at"`
	LastActivityAt   time.Time `json:"last_activity_at"`

	// ===================================================
	// CONVERSATION AUTHORITY (Decision Power)
	// ===================================================
	// This is the layer that separates Order Context from action permission.
	// Order existence ≠ permission to act.
	// UI buttons must inject intent, not execute actions.
	// Only explicit user triggers unlock these gates.
	Authority ConversationAuthority `json:"authority"`
}

// ConversationAuthority controls what actions the AI is ALLOWED to perform.
// All gates default to FALSE. Only explicit user triggers can open them.
//
// GOLDEN RULE:
// No message may cause a transactional response unless it maps to an
// explicit user intent in the current session.
// Not order state. Not UI state. Not database state. Only current intent.
type ConversationAuthority struct {
	// Payment Actions
	CanSendPayment   bool `json:"can_send_payment"`   // Can AI send payment details?
	CanResendPayment bool `json:"can_resend_payment"` // Can AI resend payment (must ask first)?

	// Order Actions
	CanSummarizeOrder bool `json:"can_summarize_order"` // Can AI summarize order?
	CanRequestPayment bool `json:"can_request_payment"` // Can AI ask for payment?
	CanCreateOrder    bool `json:"can_create_order"`    // Can AI create a new order?

	// Last User Trigger (for audit/debugging)
	LastUserTrigger  string    `json:"last_user_trigger"` // What triggered the last action?
	TriggerTimestamp time.Time `json:"trigger_timestamp"`
}

// ResetAuthority resets all authority gates to false (new session)
func (a *ConversationAuthority) ResetAuthority() {
	a.CanSendPayment = false
	a.CanResendPayment = false
	a.CanSummarizeOrder = false
	a.CanRequestPayment = false
	a.CanCreateOrder = false
	a.LastUserTrigger = ""
	a.TriggerTimestamp = time.Time{}
}

// GrantPaymentAuthority opens payment gates (user explicitly asked)
func (a *ConversationAuthority) GrantPaymentAuthority(trigger string) {
	a.CanSendPayment = true
	a.CanResendPayment = true
	a.LastUserTrigger = trigger
	a.TriggerTimestamp = time.Now()
}

// GrantOrderAuthority opens order gates (user explicitly confirmed)
func (a *ConversationAuthority) GrantOrderAuthority(trigger string) {
	a.CanCreateOrder = true
	a.CanSummarizeOrder = true
	a.CanRequestPayment = true
	a.LastUserTrigger = trigger
	a.TriggerTimestamp = time.Now()
}

// SessionExpiryDuration is the maximum inactivity before session expires
const SessionExpiryDuration = 24 * time.Hour

// IsExpired returns true if session has exceeded 24h inactivity
func (s *SessionConversationState) IsExpired() bool {
	return time.Since(s.LastActivityAt) > SessionExpiryDuration
}

// IsTransactional returns true if commitment level allows transactional actions
func (s *SessionConversationState) IsTransactional() bool {
	return s.CommitmentLevel == "committed" && s.UserRequestedPayment
}

// CanSendPaymentDetails implements UIAG for payment actions
func (s *SessionConversationState) CanSendPaymentDetails() bool {
	return s.UserRequestedPayment && !s.IsFresh
}

// CanCreateOrder implements UIAG for order creation
func (s *SessionConversationState) CanCreateOrder() bool {
	return s.UserRequestedOrder && s.CommitmentLevel == "committed" && !s.IsFresh
}

// MarkActive marks the session as no longer fresh
func (s *SessionConversationState) MarkActive() {
	s.IsFresh = false
	s.MessageCount++
	s.LastActivityAt = time.Now()
}

// SessionManager manages ephemeral session state
// Uses in-memory storage with optional Redis backing for distributed systems
type SessionManager struct {
	sessions map[string]*SessionConversationState
	mu       sync.RWMutex
	logger   *zap.Logger
}

// NewSessionManager creates a new session manager
func NewSessionManager(logger *zap.Logger) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*SessionConversationState),
		logger:   logger,
	}
}

// sessionKey generates a unique key for tenant+customer+conversation
func sessionKey(tenantID, customerID, conversationID uuid.UUID) string {
	return tenantID.String() + ":" + customerID.String() + ":" + conversationID.String()
}

// GetOrCreateSession returns existing session or creates a fresh one
// This is where session expiry and terminal state reset happens
func (m *SessionManager) GetOrCreateSession(
	ctx context.Context,
	tenantID, customerID, conversationID uuid.UUID,
) *SessionConversationState {
	key := sessionKey(tenantID, customerID, conversationID)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for existing session
	if session, exists := m.sessions[key]; exists {
		// Check if expired
		if session.IsExpired() {
			if m.logger != nil {
				m.logger.Info("session_expired_creating_new",
					zap.String("session_id", session.SessionID),
					zap.Duration("age", time.Since(session.SessionStartedAt)))
			}
			// Create fresh session
			return m.createFreshSession(key, tenantID, customerID, conversationID)
		}
		return session
	}

	// No existing session - create fresh
	return m.createFreshSession(key, tenantID, customerID, conversationID)
}

// createFreshSession initializes a brand new session with zero state
func (m *SessionManager) createFreshSession(
	key string,
	tenantID, customerID, conversationID uuid.UUID,
) *SessionConversationState {
	session := &SessionConversationState{
		SessionID:            uuid.New().String(),
		TenantID:             tenantID,
		CustomerID:           customerID,
		ConversationID:       conversationID,
		State:                "DISCOVERY", // Always start in discovery
		CommitmentLevel:      "none",      // No commitment assumed
		UserRequestedPayment: false,       // UIAG: Must be explicitly triggered
		UserRequestedOrder:   false,       // UIAG: Must be explicitly triggered
		HasGreeted:           false,       // Greeting lock off
		IsFresh:              true,        // Cold start gate active
		MessageCount:         0,
		SessionStartedAt:     time.Now(),
		LastActivityAt:       time.Now(),
	}

	m.sessions[key] = session

	if m.logger != nil {
		m.logger.Info("fresh_session_created",
			zap.String("session_id", session.SessionID),
			zap.Stringer("tenant_id", tenantID),
			zap.Stringer("customer_id", customerID))
	}

	return session
}

// ResetSession forces a session reset (e.g., after terminal order state)
func (m *SessionManager) ResetSession(tenantID, customerID, conversationID uuid.UUID) {
	key := sessionKey(tenantID, customerID, conversationID)

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, key)

	if m.logger != nil {
		m.logger.Info("session_reset",
			zap.Stringer("tenant_id", tenantID),
			zap.Stringer("customer_id", customerID))
	}
}

// UpdateSession updates session state
func (m *SessionManager) UpdateSession(session *SessionConversationState) {
	key := sessionKey(session.TenantID, session.CustomerID, session.ConversationID)

	m.mu.Lock()
	defer m.mu.Unlock()

	session.LastActivityAt = time.Now()
	m.sessions[key] = session
}

// =====================================================================
// FIX 2: Split payment detection into TWO intents
// =====================================================================
// Payment Inquiry = asking about payment (NO authority, respond with "When ready, I can send details")
// Payment Request = explicitly requesting details (authority granted)

// DetectPaymentInquiry checks if user is ASKING about payment (NOT requesting details)
// Response should be: "We accept bank transfer. When you're ready, I can send the details."
// NO authority granted - this is just a question
func DetectPaymentInquiry(message string) bool {
	inquiries := []string{
		"how do i pay", "how do I pay",
		"do you accept transfer", "do you accept bank",
		"what payment", "which payment",
		"can i pay", "can I pay",
		"payment method", "payment option",
	}

	msgLower := strings.ToLower(message)
	for _, inquiry := range inquiries {
		if strings.Contains(msgLower, inquiry) {
			// Make sure it's not also a request (request takes priority)
			if DetectPaymentRequest(message) {
				return false
			}
			return true
		}
	}
	return false
}

// DetectPaymentRequest checks if user is EXPLICITLY requesting payment details
// ONLY these grant authority to send bank details
func DetectPaymentRequest(message string) bool {
	requests := []string{
		"send payment details", "send me payment",
		"send account", "send the account", "send your account",
		"give me account", "give me the account",
		"please send account", "pls send account",
		"i'm ready to pay", "im ready to pay", "ready to pay now",
		"i want to pay now", "let me pay",
		"send bank details", "send the bank",
		"share payment", "share account",
	}

	msgLower := strings.ToLower(message)
	for _, request := range requests {
		if strings.Contains(msgLower, request) {
			return true
		}
	}
	return false
}

// DetectPaymentTrigger - DEPRECATED: Use DetectPaymentRequest instead
// Kept for backward compatibility but now routes to DetectPaymentRequest
func DetectPaymentTrigger(message string) bool {
	return DetectPaymentRequest(message)
}

// DetectOrderTrigger checks if message contains UIAG order trigger
func DetectOrderTrigger(message string) bool {
	triggers := []string{
		"i want to order", "place order", "make order",
		"i'll take", "i'll buy", "i want to buy",
		"confirm order", "proceed with order",
		"send it", "deliver it", "i'll get",
	}

	msgLower := strings.ToLower(message)
	for _, trigger := range triggers {
		if strings.Contains(msgLower, trigger) {
			return true
		}
	}
	return false
}

// IsGreeting checks if message is a pure greeting
func IsGreeting(message string) bool {
	greetings := []string{
		"hello", "hi", "hey", "good morning", "good afternoon", "good evening",
		"hi there", "hello there", "hey there", "howdy", "holla", "yo",
		"how are you", "how's it going", "what's up", "sup", "whats up",
		"greetings", "hiii", "hiiii", "helloo", "hellooo",
	}

	cleaned := strings.TrimSpace(strings.ToLower(message))
	cleaned = strings.TrimRight(cleaned, "!?.,")

	for _, g := range greetings {
		if cleaned == g {
			return true
		}
	}
	return false
}
