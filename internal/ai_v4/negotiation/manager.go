package negotiation

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Manager orchestrates negotiation state and decisions
// This is adapted from the existing negotiation.Manager and ai/v3/negotiation.go
type Manager struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewManager creates a new negotiation manager
func NewManager(db *gorm.DB, logger *zap.Logger) *Manager {
	return &Manager{
		db:     db,
		logger: logger,
	}
}

// Decision represents the result of negotiation logic
type Decision struct {
	Phase      Phase
	Offer      float64 // AI's counter-offer (if applicable)
	Reason     string  // For logging
	Escalation EscalationType
	Style      Style
}

// Phase indicates the negotiation phase
type Phase string

const (
	PhaseAcknowledge Phase = "acknowledge"
	PhaseAccept      Phase = "accept" // 🆕 Added PhaseAccept
	PhaseExplore     Phase = "explore"
	PhaseConcede     Phase = "concede"
	PhaseHold        Phase = "hold"
	PhaseEscalate    Phase = "escalate"
)

// EscalationType indicates why escalation is needed
type EscalationType string

const (
	EscalationNone            EscalationType = "none"
	EscalationPricingApproval EscalationType = "pricing_approval"
	EscalationHumanTakeover   EscalationType = "human_takeover"
)

// Style indicates the tone of the negotiation response
type Style string

const (
	StyleStandard       Style = "standard"
	StylePlayfulDeflect Style = "playful_deflect"
	StyleFirmPolite     Style = "firm_polite"
)

// Policy defines negotiation constraints
type Policy struct {
	MaxDiscountPct float64 // e.g. 0.10 for 10%
	MinPrice       float64 // Protected floor
	ProximityPct   float64 // Proximity threshold for pricing approval (e.g. 0.9)
	MaxConcessions int     // Maximum AI concessions allowed
}

// Decide makes a deterministic negotiation decision
func (m *Manager) Decide(ctx context.Context, salesConv *storage.SalesConversation, offer, productPrice float64, policy Policy, customerAskedForNumber bool) (Decision, error) {
	// Sanitize inputs
	if productPrice <= 0 {
		return Decision{Phase: PhaseAcknowledge, Reason: "product price unknown", Escalation: EscalationNone}, nil
	}
	if offer <= 0 && !customerAskedForNumber {
		return Decision{Phase: PhaseAcknowledge, Reason: "no valid offer", Escalation: EscalationNone}, nil
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

	// Calculate floor
	floor := policy.MinPrice
	if floor <= 0 {
		floor = math.Floor(productPrice * (1 - maxDisc))
	}
	ceiling := productPrice
	if floor > ceiling {
		floor = ceiling
	}

	// Handle nil sales conversation
	if salesConv == nil {
		if customerAskedForNumber {
			initialOffer := floor + (ceiling-floor)*0.80
			initialOffer = math.Ceil(initialOffer/100.0) * 100.0
			if initialOffer > ceiling {
				initialOffer = ceiling
			}
			return Decision{Phase: PhaseConcede, Offer: initialOffer, Reason: "best_price_request_initial_offer", Escalation: EscalationNone}, nil
		}
		if offer >= productPrice {
			return Decision{Phase: PhaseAccept, Reason: "initial_accept", Escalation: EscalationNone}, nil
		}
		return Decision{Phase: PhaseAcknowledge, Reason: "initial_acknowledge", Escalation: EscalationNone}, nil
	}

	previousAIOffer := salesConv.LastAIOffer
	// Guardrail: conversation-scoped negotiation state can leak across different items.
	// If the stored LastAIOffer is outside the current item's allowed range, invalidate it so we don't
	// repeat an unrelated (too-low / too-high) price on "best price?" follow-ups.
	if previousAIOffer > 0 && (previousAIOffer < floor || previousAIOffer > ceiling) {
		salesConv.LastAIOffer = 0
		salesConv.LastOfferAmount = 0
		salesConv.SameOfferCount = 0
		salesConv.NegotiationCount = 0
		salesConv.AIConcessionCount = 0
		salesConv.EscalationPending = false
		previousAIOffer = 0
	}
	// Ensure subsequent turns don't keep re-entering the first-turn exploration branch.
	if offer > 0 || customerAskedForNumber {
		salesConv.NegotiationCount++
	}

	// Best price request without a numeric offer in an active negotiation
	if offer <= 0 && customerAskedForNumber {
		if salesConv.LastAIOffer > 0 {
			return Decision{Phase: PhaseHold, Offer: salesConv.LastAIOffer, Reason: "repeat_best_price", Escalation: EscalationNone}, nil
		}
		initialOffer := floor + (ceiling-floor)*0.80
		initialOffer = math.Ceil(initialOffer/100.0) * 100.0
		if initialOffer > ceiling {
			initialOffer = ceiling
		}
		if previousAIOffer > 0 && initialOffer > previousAIOffer {
			initialOffer = previousAIOffer
		}
		salesConv.LastAIOffer = initialOffer
		salesConv.AIConcessionCount++
		return Decision{Phase: PhaseConcede, Offer: initialOffer, Reason: "best_price_request_initial_offer", Escalation: EscalationNone}, nil
	}

	previousCustomerOffer := salesConv.LastOfferAmount

	// Update persistence tracking
	if offer > 0 {
		if previousCustomerOffer == 0 {
			if salesConv.SameOfferCount == 0 {
				salesConv.SameOfferCount = 1
			}
		} else if previousCustomerOffer == offer {
			salesConv.SameOfferCount++
		} else {
			salesConv.SameOfferCount = 1
		}
		salesConv.LastOfferAmount = offer
	}

	// Customer matched (or exceeded) our previously quoted amount: treat as acceptance.
	if offer > 0 && salesConv.LastAIOffer > 0 {
		if math.Abs(offer-salesConv.LastAIOffer) <= 100 || offer > salesConv.LastAIOffer {
			return Decision{Phase: PhaseAccept, Offer: salesConv.LastAIOffer, Reason: "offer_matches_last_ai_offer", Escalation: EscalationNone}, nil
		}
	}

	// If escalation is pending, hold
	if salesConv.EscalationPending {
		if salesConv.LastAIOffer > 0 {
			return Decision{Phase: PhaseHold, Offer: salesConv.LastAIOffer, Reason: "escalation_pending_holding_offer", Escalation: EscalationNone}, nil
		}
		return Decision{Phase: PhaseAcknowledge, Reason: "escalation_pending_acknowledge_only", Escalation: EscalationNone}, nil
	}

	// Exploration: happens only once (NegotiationCount == 0)
	if salesConv.NegotiationCount == 0 {
		if customerAskedForNumber {
			// Offer something between floor and ceiling
			initialOffer := floor + (ceiling-floor)*0.80
			initialOffer = math.Ceil(initialOffer/100.0) * 100.0
			if initialOffer > ceiling {
				initialOffer = ceiling
			}
			if previousAIOffer > 0 && initialOffer > previousAIOffer {
				initialOffer = previousAIOffer
			}
			salesConv.LastAIOffer = initialOffer
			salesConv.AIConcessionCount++
			return Decision{Phase: PhaseConcede, Offer: initialOffer, Reason: "initial_offer_on_request", Escalation: EscalationNone}, nil
		}

		if offer > 0 {
			if offer < floor*0.75 {
				return Decision{Phase: PhaseExplore, Reason: "offer_far_below_floor", Escalation: EscalationNone}, nil
			}

			var counterOffer float64
			if offer < floor {
				// Start at 60% above floor (gradual, not 20% which is too close to floor)
				counterOffer = floor + (ceiling-floor)*0.60
			} else if offer < ceiling {
				// Move 40% toward ceiling (gradual, not 70% which jumps too far)
				counterOffer = offer + (ceiling-offer)*0.40
			} else {
				counterOffer = ceiling
			}
			counterOffer = math.Ceil(counterOffer/100.0) * 100.0
			if counterOffer > ceiling {
				counterOffer = ceiling
			}
			if counterOffer < floor {
				counterOffer = floor
			}
			if previousAIOffer > 0 && counterOffer > previousAIOffer {
				counterOffer = previousAIOffer
			}
			salesConv.LastAIOffer = counterOffer
			salesConv.AIConcessionCount++
			return Decision{Phase: PhaseConcede, Offer: counterOffer, Reason: fmt.Sprintf("initial_counter_offer=%.0f", counterOffer), Escalation: EscalationNone}, nil
		}

		return Decision{Phase: PhaseExplore, Reason: "explore_once", Escalation: EscalationNone}, nil
	}

	// =================================================================================
	// 🆕 SMART NEGOTIATION LOGIC
	// =================================================================================

	// 1. LOWBALL DEFENSE (The 50% Rule)
	// If offer is < 50% of floor, don't even concede. Hold firmly and educate.
	if offer > 0 && floor > 0 && offer < floor*0.50 {
		return Decision{
			Phase:      PhaseHold,
			Offer:      salesConv.LastAIOffer,
			Reason:     "lowball_defense_educate",
			Escalation: EscalationNone,
		}, nil
	}

	// Concession Handling
	if salesConv.AIConcessionCount < policy.MaxConcessions {
		lastAIOffer := salesConv.LastAIOffer
		if lastAIOffer <= 0 {
			lastAIOffer = ceiling
		}

		var nextOffer float64
		customerDelta := 0.0
		if previousCustomerOffer > 0 && offer > previousCustomerOffer {
			customerDelta = offer - previousCustomerOffer
		}

		// 2. RECIPROCITY ("You Move, I Move")
		// If customer increased offer, we match their move (Tit-for-Tat)
		if customerDelta > 0 {
			// They moved up by X. We move down by X.
			// But maybe slightly less aggressive to save margin? Let's match 100% for fairness/speed.
			nextOffer = lastAIOffer - customerDelta
			// Ensure we don't drop below floor just to match
			if nextOffer < floor {
				nextOffer = floor
			}
		} else {
			// 3. NARROWING WINDOW (Standard Descent)
			// Calculate step based on REMAINING gap (LastAI - Floor).
			// We move 20% of the REMAINING distance.
			gap := lastAIOffer - floor
			if gap > 0 {
				step := gap * 0.20
				// Ensure minimum step of 5% of list price (ceiling) to keep momentum, unless gap is tiny
				minStep := ceiling * 0.05
				if step < minStep && gap > minStep {
					step = minStep
				}
				nextOffer = lastAIOffer - step
			} else {
				nextOffer = floor
			}
		}

		// Rounding
		nextOffer = math.Ceil(nextOffer/100.0) * 100.0

		// Safety Clamps
		if nextOffer > lastAIOffer && lastAIOffer > 0 {
			nextOffer = lastAIOffer // Never go up
		}
		if nextOffer < floor {
			nextOffer = floor
		}
		if nextOffer > ceiling {
			nextOffer = ceiling
		}

		// Determine if we should Concede or Hold
		// Always concede if we calculated a new lower price.
		shouldConcede := nextOffer < lastAIOffer || (offer > 0 && salesConv.AIConcessionCount == 0 && nextOffer < ceiling)

		if shouldConcede {
			if previousAIOffer > 0 && nextOffer > previousAIOffer {
				nextOffer = previousAIOffer
			}
			salesConv.LastAIOffer = nextOffer
			salesConv.AIConcessionCount++
			// Check if we met them (or beat them)
			if offer >= nextOffer {
				return Decision{Phase: PhaseAccept, Offer: nextOffer, Reason: "offer_accepted_reciprocity", Escalation: EscalationNone}, nil
			}
			return Decision{Phase: PhaseConcede, Offer: nextOffer, Reason: fmt.Sprintf("smart_concession_delta=%.0f", customerDelta), Escalation: EscalationNone}, nil
		}
	}

	// 4. SMART ESCALATION (The Close Call)
	// If we are stuck (can't concede further) BUT they are very close (within 5% of floor), Escalate.
	if floor > 0 && offer >= floor*0.95 {
		salesConv.EscalationPending = true
		return Decision{Phase: PhaseEscalate, Reason: "close_call_escalation", Escalation: EscalationPricingApproval}, nil
	}

	// Default Hold
	if salesConv.LastAIOffer > 0 {
		return Decision{Phase: PhaseHold, Offer: salesConv.LastAIOffer, Reason: "holding_limit_reached", Escalation: EscalationNone}, nil
	}
	return Decision{Phase: PhaseHold, Reason: "default_hold", Escalation: EscalationNone}, nil
}

// GetOrCreateState returns the negotiation state for a sales conversation
func (m *Manager) GetOrCreateState(ctx context.Context, salesConvID, productID uuid.UUID) (*storage.NegotiationState, error) {
	var s storage.NegotiationState
	if err := m.db.Where("sales_conversation_id = ? AND product_id = ?", salesConvID, productID).First(&s).Error; err == nil {
		return &s, nil
	} else if err == gorm.ErrRecordNotFound {
		s = storage.NegotiationState{SalesConversationID: salesConvID, ProductID: productID, Status: "idle"}
		if err := m.db.Create(&s).Error; err != nil {
			return nil, err
		}
		return &s, nil
	} else {
		return nil, err
	}
}

// RecordCustomerOffer updates last customer offer
func (m *Manager) RecordCustomerOffer(ctx context.Context, salesConvID, productID uuid.UUID, offer float64) (*storage.NegotiationState, error) {
	s, err := m.GetOrCreateState(ctx, salesConvID, productID)
	if err != nil {
		return nil, err
	}

	if s.LastCustomerOffer == offer && s.FlexibilityAttempted && s.AIConcessionCount > 0 {
		s.SameOfferCount++
	} else {
		s.SameOfferCount = 1
	}
	s.LastCustomerOffer = offer
	if s.Status == "idle" {
		s.Status = "negotiating"
	}

	if err := m.db.Save(s).Error; err != nil {
		return nil, err
	}
	return s, nil
}

// RecordAIConcession records an AI concession
func (m *Manager) RecordAIConcession(ctx context.Context, salesConvID, productID uuid.UUID, aiOffer float64) (*storage.NegotiationState, error) {
	s, err := m.GetOrCreateState(ctx, salesConvID, productID)
	if err != nil {
		return nil, err
	}
	s.AIConcessionCount++
	if s.Status == "idle" {
		s.Status = "negotiating"
	}
	if err := m.db.Save(s).Error; err != nil {
		return nil, err
	}
	return s, nil
}

// DetectOfferInText extracts numeric offer from text
func DetectOfferInText(s string) float64 {
	// Check for k/m suffix patterns first
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			start := i
			for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == ',' || s[i] == '.') {
				i++
			}
			token := strings.ReplaceAll(s[start:i], ",", "")

			// Check for k/m suffix
			var mult float64 = 1.0
			if i < len(s) {
				if s[i] == 'k' || s[i] == 'K' {
					mult = 1000.0
				} else if s[i] == 'm' || s[i] == 'M' {
					mult = 1000000.0
				}
			}

			var v float64
			fmt.Sscanf(token, "%f", &v)
			if v > 0 {
				v *= mult
				if v >= 100 { // Only consider values >= 100 as prices
					return v
				}
			}
		}
	}
	return 0
}

// FormatMoney formats a price for display
func FormatMoney(v float64) string {
	if v >= 1000000 {
		return fmt.Sprintf("₦%.1fm", v/1000000)
	}
	if v >= 1000 {
		return fmt.Sprintf("₦%.0fk", v/1000)
	}
	return fmt.Sprintf("₦%.0f", v)
}
