package v3

import (
	"fmt"
	"strings"
	"time"

	"github.com/similadayo/brancr/internal/domain"
)

// =============================================================================
// CONVERSATION OBLIGATIONS - Track what's been answered to prevent loops
// =============================================================================

// ConversationObligation represents a question that needs/needed answering
type ConversationObligation struct {
	Question      string    // The customer's question/need
	QuestionType  string    // product_inquiry, recommendation, pricing, etc.
	Answered      bool      // Has this been answered?
	AnsweredAt    time.Time // When it was answered
	AnswerSummary string    // Brief summary of what was answered
}

// RecommendationState tracks the validity of the current suggestion
type RecommendationState struct {
	Product string
	Valid   bool
}

// ObligationsTracker tracks conversational state to prevent loops and repetition
type ObligationsTracker struct {
	// What questions have been answered
	ClosedObligations  []ConversationObligation
	PendingObligations []ConversationObligation // Questions waiting for an answer

	// Conversation facts
	ProductsShown       bool   // Have we shown the product list already?
	ProductsShownCount  int    // How many times we've listed products
	LastRecommendation  string // What we last recommended
	RecommendationGiven bool   // Have we made a recommendation?

	// Active Recommendation (Phase 3)
	ActiveRecommendation RecommendationState

	// Conversation phase
	GreetingDone bool // Have we greeted the customer?

	// Rejection Memory (Phase 2)
	RejectedProducts     []string // Items the customer said "no" to
	UserRejectedLastItem bool     // Did user reject the *last* specific pitch?

	HasAskedQualification bool      // Step 2: Track if we've asked a qualifying question
	LastAIMessageAt       time.Time // Step 7: Last time the AI responded
	HasReengaged          bool      // Step 7: Have we re-engaged once?
	NegotiationRounds     int       // Step 8: Track negotiation intensity
	HasShownOfferings     bool
	IsRefusal             bool   // Step 4: Track refusal state
	IsBoundaryResponse    bool   // Step 5: Track boundary response state
	HasClosed             bool   // Step 6: Track if we have closed
	InsightStatus         string // Fix 4.3: Async insight status

	LastShownOfferings []domain.Offering // Guard 3: Single Source of Truth
}

// NewObligationsTracker creates a new tracker
func NewObligationsTracker() *ObligationsTracker {
	return &ObligationsTracker{
		ClosedObligations:    make([]ConversationObligation, 0),
		PendingObligations:   make([]ConversationObligation, 0),
		ActiveRecommendation: RecommendationState{Valid: false},
	}
}

// AddPending adds a new obligation to the tracker
func (t *ObligationsTracker) AddPending(question, cat string) {
	t.PendingObligations = append(t.PendingObligations, ConversationObligation{
		Question:     question,
		QuestionType: cat,
		Answered:     false,
	})
}

// MarkProductsShown records that we've shown the product list
func (t *ObligationsTracker) MarkProductsShown() {
	t.ProductsShown = true
	t.ProductsShownCount++
}

// MarkRecommendation records a recommendation made
func (t *ObligationsTracker) MarkRecommendation(productName string) {
	t.LastRecommendation = productName
	t.RecommendationGiven = true
	t.ActiveRecommendation = RecommendationState{
		Product: productName,
		Valid:   true,
	}
	t.ClosedObligations = append(t.ClosedObligations, ConversationObligation{
		Question:      "recommendation",
		QuestionType:  "recommendation",
		Answered:      true,
		AnsweredAt:    time.Now(),
		AnswerSummary: "Recommended " + productName,
	})
}

// MarkGreetingDone records that greeting is complete
func (t *ObligationsTracker) MarkGreetingDone() {
	t.GreetingDone = true
}

// HasAlreadyRecommended checks if we've made a recommendation
func (t *ObligationsTracker) HasAlreadyRecommended() bool {
	return t.RecommendationGiven
}

// HasAlreadyShownProducts checks if products have been listed
func (t *ObligationsTracker) HasAlreadyShownProducts() bool {
	return t.ProductsShown
}

// MarkProductRejected records an item that didn't land
func (t *ObligationsTracker) MarkProductRejected(productName string) {
	// Avoid duplicates
	for _, p := range t.RejectedProducts {
		if strings.EqualFold(p, productName) {
			return
		}
	}
	t.RejectedProducts = append(t.RejectedProducts, productName)
}

// GetConversationState returns a human-readable summary for LLM context
func (t *ObligationsTracker) GetConversationState() string {
	var state []string

	if len(t.PendingObligations) > 0 {
		state = append(state, "PENDING OBLIGATIONS (MUST ANSWER):")
		for _, o := range t.PendingObligations {
			state = append(state, fmt.Sprintf("- [ ] %s (%s)", o.Question, o.QuestionType))
		}
		state = append(state, "")
	}

	if len(t.RejectedProducts) > 0 {
		state = append(state, "CUSTOMER DISLIKES (DO NOT PITCH):")
		for _, p := range t.RejectedProducts {
			state = append(state, fmt.Sprintf("- %s", p))
		}
		state = append(state, "")
	}

	if t.UserRejectedLastItem {
		state = append(state, "!!!!!!!!!!!! IMPORTANT !!!!!!!!!!!!")
		state = append(state, "CUSTOMER REJECTED LAST SUGGESTION.")
		state = append(state, "DO NOT REPEAT IT. SUGGEST SOMETHING ELSE.")
		state = append(state, "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		state = append(state, "")
	}

	state = append(state, "Conversation History:")
	if t.GreetingDone {
		state = append(state, "- Greeting completed")
	}

	if t.ProductsShown {
		state = append(state, fmt.Sprintf("- Products/menu shown (%d times)", t.ProductsShownCount))
	}

	if t.RecommendationGiven {
		if t.ActiveRecommendation.Valid {
			state = append(state, "- Active Rec: "+t.ActiveRecommendation.Product)
		} else {
			state = append(state, "- Previous Rec: "+t.LastRecommendation+" (Rejected/Invalid)")
		}
	}

	if len(state) == 1 { // Only header
		return "- Fresh conversation, nothing discussed yet"
	}

	return strings.Join(state, "\n")
}

// =============================================================================
// OBLIGATION INFERENCE FROM RECENT MESSAGES
// =============================================================================

// InferObligationsFromHistory analyzes recent messages to build obligations state
func InferObligationsFromHistory(messages []Message) *ObligationsTracker {
	tracker := NewObligationsTracker()

	for _, msg := range messages {
		content := strings.ToLower(msg.Content)

		// Check if AI has shown products
		if msg.Role == "assistant" {
			if strings.Contains(content, "ngn") ||
				strings.Contains(content, "₦") ||
				strings.Contains(content, "price") {
				tracker.MarkProductsShown()
			}

			// Check for recommendations
			if strings.Contains(content, "recommend") ||
				strings.Contains(content, "i'd go with") ||
				strings.Contains(content, "i'd suggest") ||
				strings.Contains(content, "i'd pick") {

				// Heuristic: Extract product name (very crude)
				// Assume product name is "banana bread" or "jollof"
				// For now, we set a placeholder, assuming LLM context carries the string
				extracted := "item"
				// NOTE: In a real system, we'd persist the exact ItemID in metadata.
				// Here we just mark that a rec happened.
				// If we want to support specifically "Banana Bread", we rely on the LLM reading the history.

				tracker.MarkRecommendation(extracted)
			}

			// Check for greetings
			if strings.Contains(content, "hello") ||
				strings.Contains(content, "hi ") ||
				strings.Contains(content, "welcome") ||
				strings.Contains(content, "hey") {
				tracker.MarkGreetingDone()
			}

			// Reset rejection flag on new AI turn
			tracker.UserRejectedLastItem = false
		}

		if msg.Role == "user" {
			// Check for rejection of previous recommendation
			// e.g. "no", "I don't like that", "something else"
			if tracker.RecommendationGiven && tracker.ActiveRecommendation.Valid {
				rejectionKw := []string{"no", "nah", "pass", "don't like", "something else", "other", "different", "change"}
				for _, kw := range rejectionKw {
					if strings.Contains(content, kw) {
						tracker.UserRejectedLastItem = true

						// Phase 3: Invalidate
						tracker.ActiveRecommendation.Valid = false

						// Phase 2: Add to Rejected
						if tracker.ActiveRecommendation.Product != "" {
							tracker.MarkProductRejected(tracker.ActiveRecommendation.Product) // Adds to list
						}
						break
					}
				}
			}
		}
	}

	// If the LAST message was from the user and contains a question, it is pending
	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if lastMsg.Role == "user" {
			content := strings.ToLower(lastMsg.Content)
			if strings.Contains(content, "menu") ||
				strings.Contains(content, "have") ||
				strings.Contains(content, "list") ||
				strings.Contains(content, "catalogue") ||
				strings.Contains(content, "available") ||
				strings.Contains(content, "options") ||
				(strings.Contains(content, "items") && !strings.Contains(content, "cart")) {
				// FORCE MENU OBLIGATION (Phase 4)
				tracker.AddPending(lastMsg.Content, "menu_inquiry")
			} else if strings.Contains(content, "recommend") || strings.Contains(content, "suggest") {
				tracker.AddPending(lastMsg.Content, "recommendation_request")
			} else if strings.Contains(content, "price") || strings.Contains(content, "cost") || strings.Contains(content, "how much") {
				tracker.AddPending(lastMsg.Content, "price_inquiry")
			}
		}
	}

	return tracker
}
