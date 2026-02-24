package v3

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/similadayo/brancr/internal/storage"
	"github.com/similadayo/brancr/internal/utils"
)

// Negotiation phases (refactored canonical phases)
const (
	PhaseAcknowledge = "acknowledge"
	PhaseExplore     = "explore"
	PhaseConcede     = "concede"
	PhaseHold        = "hold"
	PhaseEscalate    = "escalate"
)

// EscalationType represents the type of escalation when PhaseEscalate is returned
type EscalationType string

const (
	EscalationNone            EscalationType = "none"
	EscalationPricingApproval EscalationType = "pricing_approval"
	EscalationHumanTakeover   EscalationType = "human_takeover"
)

// NegotiationStyle defines the tone of the negotiation response
type NegotiationStyle string

const (
	StyleStandard       NegotiationStyle = "standard"
	StylePlayfulDeflect NegotiationStyle = "playful_deflect"
	StyleFirmPolite     NegotiationStyle = "firm_polite"
)

// NegotiationDecision is the result of the manager's decision logic
type NegotiationDecision struct {
	Phase      string
	Offer      float64          // The numeric offer AI suggests when PhaseConcede
	Reason     string           // Logging reason
	Escalation EscalationType   // if PhaseEscalate
	Style      NegotiationStyle // Tone of the response
}

// NegotiationPolicy defines allowed negotiation behavior for a product/tenant
type NegotiationPolicy struct {
	MaxDiscountPct float64 // e.g. 0.10 for 10%
	MinPrice       float64 // protected floor (internal)
	ProximityPct   float64 // proximity threshold for pricing approval (e.g. 0.9)
	MaxConcessions int     // maximum AI concessions allowed
}

// ============================================================
// NEGOTIATION ACTIVATION GATE (Blueprint Section 12.2)
// ============================================================

// NegotiationGateResult contains the result of the activation check
type NegotiationGateResult struct {
	CanActivate bool   // Whether negotiation can proceed
	Reason      string // Reason for denial (if any)
	Message     string // Suggested response message (if denied)
}

// CanActivateNegotiation checks if negotiation is allowed in current conversation state
// Implements Blueprint Section 12.2: "Negotiation can ONLY activate when State >= INTENT_FORMATION"
// This prevents awkward early negotiation before product context is established
func CanActivateNegotiation(currentStage string, productPrice float64) NegotiationGateResult {
	// Allowed states for negotiation
	allowedStates := map[string]bool{
		"intent_formation":   true,
		"commitment_signals": true,
		"order_confirmation": true,
		"ordering":           true,
		"negotiation":        true, // Already in negotiation is ok
	}

	// Check 1: State requirement
	if currentStage != "" {
		if !allowedStates[currentStage] {
			return NegotiationGateResult{
				CanActivate: false,
				Reason:      "state_too_early",
				Message:     "Let me first confirm a few details, then we can talk price.",
			}
		}
	}

	// Check 2: Product context requirement
	// Simplified for V3: we trust intent classifier mostly, but check price
	noProductContext := productPrice <= 0
	if noProductContext {
		return NegotiationGateResult{
			CanActivate: false,
			Reason:      "no_product_context",
			Message:     "Which item are you interested in? I can check the price for you.",
		}
	}

	return NegotiationGateResult{
		CanActivate: true,
		Reason:      "",
		Message:     "",
	}
}

// Decide makes a deterministic negotiation decision using SalesConversation state and request
// Now enhanced with Social Strategy (Phase 4)
func Decide(ctx context.Context, sc *storage.SalesConversation, offer, productPrice float64, policy NegotiationPolicy, customerAskedForNumber bool, social SocialSignals) (NegotiationDecision, error) {
	// Sanitize inputs
	if offer <= 0 {
		return NegotiationDecision{Phase: PhaseAcknowledge, Reason: "no valid offer", Escalation: EscalationNone}, nil
	}
	if productPrice <= 0 {
		return NegotiationDecision{Phase: PhaseAcknowledge, Reason: "product price unknown", Escalation: EscalationNone}, nil
	}

	// Default policy values
	maxDisc := policy.MaxDiscountPct
	if maxDisc <= 0 || maxDisc > 0.5 {
		maxDisc = 0.10
	}
	if policy.MaxConcessions <= 0 {
		policy.MaxConcessions = 2
	}
	proximity := policy.ProximityPct
	if proximity <= 0 {
		proximity = 0.90
	}

	// ============================================================
	// SOCIAL STRATEGY LAYER (Phase 4)
	// ============================================================
	// Detect "Friend Price" / "Playful" attempts
	// If customer claims to be a friend but offers below floor, deflect playfully

	// Calculate floor for social check
	floor := policy.MinPrice
	if floor <= 0 {
		floor = math.Floor(productPrice * (1 - maxDisc))
	}

	if social.IsFriend {
		// If friend asks for huge discount (below floor), deflect playfully
		if offer < floor {
			return NegotiationDecision{
				Phase:  PhaseHold,
				Reason: "social_deflect_price_too_low",
				Style:  StylePlayfulDeflect, // New style
			}, nil
		}
	}

	// Update persistence tracking in-memory (caller should persist)
	if sc != nil {
		// If LastOfferAmount is zero it likely means the test or DB pre-seeded SameOfferCount
		// so preserve an existing SameOfferCount instead of resetting it.
		if sc.LastOfferAmount == 0 {
			if sc.SameOfferCount == 0 {
				sc.SameOfferCount = 1
			}
		} else if sc.LastOfferAmount == offer {
			sc.SameOfferCount++
		} else {
			sc.SameOfferCount = 1
		}
		sc.LastOfferAmount = offer
	}

	// CRITICAL: If escalation is pending, stop negotiation immediately
	// BUT allow acknowledgment/holding responses - don't escalate further if already escalated
	// The caller should have already handled the escalation, this just prevents new negotiations
	if sc != nil && sc.EscalationPending {
		// If we have a last offer, hold with it (don't escalate again)
		if sc.LastAIOffer > 0 {
			return NegotiationDecision{Phase: PhaseHold, Offer: sc.LastAIOffer, Reason: "escalation_pending_holding_offer", Escalation: EscalationNone}, nil
		}
		// Otherwise, acknowledge but don't negotiate further
		return NegotiationDecision{Phase: PhaseAcknowledge, Reason: "escalation_pending_acknowledge_only", Escalation: EscalationNone}, nil
	}

	// Treat a nil sales conversation as a fresh conversation: acknowledge strong offers
	if sc == nil {
		if offer >= productPrice {
			return NegotiationDecision{Phase: PhaseAcknowledge, Reason: "initial_accept", Escalation: EscalationNone}, nil
		}
		return NegotiationDecision{Phase: PhaseAcknowledge, Reason: "initial_acknowledge", Escalation: EscalationNone}, nil
	}

	// Calculate negotiation bounds
	// floor is already calculated above in Social Strategy
	ceiling := productPrice // Maximum we can offer (list price)

	// Exploration: happens only once (NegotiationCount == 0)
	if sc.NegotiationCount == 0 {
		// If customer explicitly asks for a number, make a proactive offer
		if customerAskedForNumber {
			// Offer something between floor and ceiling, starting closer to ceiling
			// Use 80% of the way from floor to ceiling as initial offer
			initialOffer := floor + (ceiling-floor)*0.80
			initialOffer = math.Ceil(initialOffer/100.0) * 100.0

			// Ensure we don't exceed ceiling
			if initialOffer > ceiling {
				initialOffer = ceiling
			}

			// CRITICAL: Track this as the AI's first offer (prevents backtracking)
			sc.LastAIOffer = initialOffer

			return NegotiationDecision{Phase: PhaseConcede, Offer: initialOffer, Reason: "initial_offer_on_request", Escalation: EscalationNone}, nil
		}

		// Customer made an offer - negotiate proactively
		if offer > 0 {
			// If offer is far below floor, don't counter yet - explore first
			if offer < floor*0.75 {
				return NegotiationDecision{Phase: PhaseExplore, Reason: "offer_far_below_floor", Escalation: EscalationNone}, nil
			}

			// Calculate a reasonable counter-offer
			// If customer offer is below floor, start with floor + 20% of range
			// If customer offer is above floor, meet them closer to their offer but still above floor
			var counterOffer float64
			if offer < floor {
				// Customer offer is too low, start negotiation higher
				counterOffer = floor + (ceiling-floor)*0.20 // 20% above floor
			} else if offer < ceiling {
				// Customer offer is reasonable, counter with something between their offer and ceiling
				// Meet them 70% of the way from their offer to ceiling
				counterOffer = offer + (ceiling-offer)*0.70
			} else {
				// Customer offer is at or above ceiling, accept it
				counterOffer = ceiling
			}
			counterOffer = math.Ceil(counterOffer/100.0) * 100.0

			// Ensure we don't exceed ceiling or go below floor
			if counterOffer > ceiling {
				counterOffer = ceiling
			}
			if counterOffer < floor {
				counterOffer = floor
			}

			sc.LastAIOffer = counterOffer
			return NegotiationDecision{Phase: PhaseConcede, Offer: counterOffer, Reason: fmt.Sprintf("initial_counter_offer=%.0f", counterOffer), Escalation: EscalationNone}, nil
		}

		// Otherwise, explore (only once)
		return NegotiationDecision{Phase: PhaseExplore, Reason: "explore_once", Escalation: EscalationNone}, nil
	}

	// Concession: only if AIConcessionCount < policy.MaxConcessions
	if sc.AIConcessionCount < policy.MaxConcessions {
		// Calculate where we can go from our last offer
		lastOffer := sc.LastAIOffer
		if lastOffer <= 0 {
			// No previous offer, start from ceiling
			lastOffer = ceiling
		}

		// Calculate next concession - move toward floor but stay above it
		// Each concession moves 30% closer to floor from current position
		var nextOffer float64
		if offer > 0 && offer < floor {
			// Customer offer is below floor, hold at floor
			nextOffer = floor
		} else if offer >= floor && offer < lastOffer {
			// Customer offer is reasonable (above floor but below our last offer)
			// Meet them partway - move 40% toward their offer from our last offer
			nextOffer = lastOffer - (lastOffer-offer)*0.40
		} else if offer >= lastOffer {
			// Customer offer is at or above our last offer - this is good, accept it
			nextOffer = math.Min(offer, ceiling)
		} else {
			// Calculate progressive concession: move 30% toward floor
			nextOffer = lastOffer - (lastOffer-floor)*0.30
		}

		nextOffer = math.Ceil(nextOffer/100.0) * 100.0

		// CRITICAL PRICE BACKTRACKING PREVENTION:
		// Once AI has offered a price, it can NEVER offer a higher price.
		if nextOffer > lastOffer && lastOffer > 0 {
			// Don't backtrack - use last offer
			nextOffer = lastOffer
		}

		// Ensure we stay within bounds
		if nextOffer < floor {
			nextOffer = floor
		}
		if nextOffer > ceiling {
			nextOffer = ceiling
		}

		// Only make a concession if:
		// 1. Customer explicitly asked for a number, OR
		// 2. Customer's offer is reasonable (above floor), OR
		// 3. We can move from our last offer (there's room to negotiate)
		if customerAskedForNumber || (offer > 0 && offer >= floor) || (lastOffer > floor && nextOffer < lastOffer) {
			// Make the concession
			sc.LastAIOffer = nextOffer
			return NegotiationDecision{Phase: PhaseConcede, Offer: nextOffer, Reason: fmt.Sprintf("concession_offer=%.0f", nextOffer), Escalation: EscalationNone}, nil
		}

		// Otherwise, hold with last offer (customer offer is too low)
		if lastOffer > 0 {
			return NegotiationDecision{Phase: PhaseHold, Offer: lastOffer, Reason: fmt.Sprintf("holding_last_offer_%.0f_customer_offer_%.0f_below_floor", lastOffer, offer), Escalation: EscalationNone}, nil
		}
		return NegotiationDecision{Phase: PhaseHold, Reason: "offer_too_low_to_concede", Escalation: EscalationNone}, nil
	}

	// Escalation: only if SameOfferCount >= 3
	// CRITICAL: Escalation means we have exhausted the AI's flexibility and need a human decision.
	// Pricing approval is used when the offer is close enough to the minimum floor (policy-defined or calculated).
	if sc.SameOfferCount >= 3 {
		floor := policy.MinPrice
		if floor <= 0 {
			floor = math.Floor(productPrice * (1 - maxDisc))
		}

		// CRITICAL: Mark escalation as pending to stop further negotiation
		sc.EscalationPending = true

		// Use the calculated floor value (which may be from policy.MinPrice or calculated from productPrice)
		// instead of only checking policy.MinPrice, to ensure consistent behavior.
		if floor > 0 && offer >= floor*proximity {
			return NegotiationDecision{Phase: PhaseEscalate, Reason: "pricing_approval_needed", Escalation: EscalationPricingApproval}, nil
		}
		return NegotiationDecision{Phase: PhaseEscalate, Reason: "human_takeover_needed", Escalation: EscalationHumanTakeover}, nil
	}

	// If we have a LastAIOffer but can't concede further, hold with that price
	if sc.LastAIOffer > 0 {
		return NegotiationDecision{Phase: PhaseHold, Offer: sc.LastAIOffer, Reason: "holding_last_offer_final", Escalation: EscalationNone}, nil
	}

	// Default: hold
	return NegotiationDecision{Phase: PhaseHold, Reason: "default_hold", Escalation: EscalationNone}, nil
}

// helper: parse amounts expressed like "550k" or "1.2m" into float
func ParseAmount(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	mult := 1.0
	last := s[len(s)-1]
	if last == 'k' || last == 'K' {
		mult = 1000.0
		s = s[:len(s)-1]
	} else if last == 'm' || last == 'M' {
		mult = 1000000.0
		s = s[:len(s)-1]
	}
	// remove commas
	s = strings.ReplaceAll(s, ",", "")
	var v float64
	_, err := fmt.Sscanf(s, "%f", &v)
	if err != nil {
		return 0
	}
	return v * mult
}

// DetectOfferInText finds the first numeric token in text and returns its float value (supports k/m shorthand)
// Safety: prefer explicit currency tokens or 'k'/'m' suffixes. Ignore plain small integers unless clearly currency-marked.
func DetectOfferInText(s string) float64 {
	// First, prefer NGN-aware extraction which ignores small unlabeled numbers
	if amt := utils.ExtractLikelyAmountNGN(s); amt > 0 {
		return amt
	}

	// Product spec context words to guard against (case-insensitive)
	specWords := []string{"gb", "ram", "storage", "units", "pcs", "piece", "pieces", "core", "cores", "inch", "inches"}

	// Fallback: look for k/m shorthand or large numbers (>=1000), but skip product specs
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			start = i
			j := i
			for j < len(s) {
				d := s[j]
				if (d >= '0' && d <= '9') || d == ',' || d == '.' {
					j++
					continue
				}
				// allow trailing k/m which indicates magnitude
				if d == 'k' || d == 'K' || d == 'm' || d == 'M' {
					j++
				}
				break
			}
			token := strings.TrimSpace(s[start:j])
			if token != "" {
				// Check for product spec context (e.g., '256GB', '8GB RAM', etc.)
				lower := strings.ToLower(s[j:])
				for _, w := range specWords {
					if strings.HasPrefix(lower, w) || strings.Contains(lower, " "+w) {
						return 0 // treat as product spec, not money
					}
				}

				// if token contains k/m, parse and return immediately
				last := token[len(token)-1]
				if last == 'k' || last == 'K' || last == 'm' || last == 'M' {
					return ParseAmount(token)
				}
				// otherwise parse raw number and only accept large numbers (>=1000) to avoid model/spec numbers
				v := ParseAmount(token)
				if v >= 1000 {
					return v
				}
			}
			break
		}
	}
	return 0
}

// FormatMoney returns a simple Naira-prefixed string for integers
func FormatMoney(v float64) string {
	if v >= 1000000 {
		return fmt.Sprintf("₦%.1fm", v/1000000)
	}
	if v >= 1000 {
		return fmt.Sprintf("₦%.0fk", v/1000)
	}
	return fmt.Sprintf("₦%.0f", v)
}
