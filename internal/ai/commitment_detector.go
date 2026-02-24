package ai

import (
	"regexp"
	"strings"
)

// ============================================================
// COMMITMENT DETECTION (Blueprint Section 8)
// Purpose: Detect when customer is ready to commit to an order
// Order is NOT created on intent - only on commitment signals
// ============================================================

// CommitmentSignals tracks customer commitment indicators
type CommitmentSignals struct {
	AddressProvided      bool `json:"address_provided"`       // Customer gave delivery address
	DeliveryTimeAccepted bool `json:"delivery_time_accepted"` // Customer confirmed delivery time
	QuantityConfirmed    bool `json:"quantity_confirmed"`     // Customer confirmed quantity
	ReadyPhrase          bool `json:"ready_phrase"`           // Customer said "I'm ready", "send payment", etc.
}

// Count returns the number of commitment signals present
func (cs *CommitmentSignals) Count() int {
	count := 0
	if cs.AddressProvided {
		count++
	}
	if cs.DeliveryTimeAccepted {
		count++
	}
	if cs.QuantityConfirmed {
		count++
	}
	if cs.ReadyPhrase {
		count++
	}
	return count
}

// ShouldLockOrder determines if order should be locked (Blueprint: Any 2 of 4 signals)
func (cs *CommitmentSignals) ShouldLockOrder() bool {
	return cs.Count() >= 2
}

// Commitment detection patterns
var (
	// Address patterns (Nigerian locations, common street identifiers)
	addressPatterns = regexp.MustCompile(`(?i)(street|road|avenue|close|estate|lekki|ikeja|vi\b|ikoyi|ajah|yaba|surulere|lagos|abuja|ph\b|port harcourt|mainland|island|opposite|beside|behind|near|along|off|by|junction|bus stop|block|flat|house\s*(no\.?|number)?|no\.?\s*\d+)`)

	// Delivery time acceptance patterns
	timeAcceptancePatterns = regexp.MustCompile(`(?i)(that('s| is)? (fine|ok|okay|good|perfect|great)|works (for me)?|suits me|agreed|accept|confirm|yes.*(time|deliver)|deliver.*(yes|ok|okay|fine)|tomorrow.*(fine|ok|okay|good|works)|today.*(fine|ok|okay|good|works))`)

	// Quantity confirmation patterns
	quantityPatterns = regexp.MustCompile(`(?i)(^(yes|yeah|yep|yup|correct|right|exactly|that'?s? (right|correct|it))\s*$|\d+\s*(pieces?|pcs|units?|items?|orders?|portions?|servings?|plates?|packs?|boxes?|bags?|bottles?|cans?|cups?|bowls?|slices?)|(one|two|three|four|five|six|seven|eight|nine|ten)\s*(pieces?|pcs|units?|items?))`)

	// Ready commitment phrases
	readyPhrases = []string{
		"i'm ready",
		"im ready",
		"ready to pay",
		"send payment",
		"payment details",
		"how do i pay",
		"where do i pay",
		"let me pay",
		"i want to pay",
		"proceed",
		"let's proceed",
		"lets proceed",
		"let's go",
		"lets go",
		"confirm order",
		"confirm my order",
		"place order",
		"place my order",
		"finalize",
		"that's all",
		"thats all",
		"that is all",
		"nothing else",
		"no, that's all",
		"done",
		"i'm done",
		"send it",
		"deliver it",
		"wrap it up",
	}
)

// DetectCommitmentSignals analyzes a customer message for commitment indicators
func DetectCommitmentSignals(message string, existingSignals *CommitmentSignals) *CommitmentSignals {
	if existingSignals == nil {
		existingSignals = &CommitmentSignals{}
	}

	// Create a copy to avoid mutating the input
	signals := *existingSignals
	messageLower := strings.ToLower(strings.TrimSpace(message))

	// Check for address (once detected, stays true)
	if !signals.AddressProvided && addressPatterns.MatchString(message) {
		signals.AddressProvided = true
	}

	// Check for delivery time acceptance
	if !signals.DeliveryTimeAccepted && timeAcceptancePatterns.MatchString(message) {
		signals.DeliveryTimeAccepted = true
	}

	// Check for quantity confirmation
	if !signals.QuantityConfirmed && quantityPatterns.MatchString(message) {
		signals.QuantityConfirmed = true
	}

	// Check for ready phrases
	if !signals.ReadyPhrase {
		for _, phrase := range readyPhrases {
			if strings.Contains(messageLower, phrase) {
				signals.ReadyPhrase = true
				break
			}
		}
	}

	return &signals
}

// AccumulateCommitmentSignals accumulates signals across multiple messages
func AccumulateCommitmentSignals(messages []string) *CommitmentSignals {
	signals := &CommitmentSignals{}
	for _, msg := range messages {
		signals = DetectCommitmentSignals(msg, signals)
	}
	return signals
}
