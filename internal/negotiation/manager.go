package negotiation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/similadayo/brancr/internal/storage"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Manager orchestrates negotiation state transitions and escalation eligibility
type Manager struct {
	DB     *gorm.DB
	Logger *zap.Logger
}

func NewManager(db *gorm.DB, logger *zap.Logger) *Manager {
	return &Manager{DB: db, Logger: logger}
}

// GetOrCreateState returns the negotiation state for a sales conversation and product
func (m *Manager) GetOrCreateState(ctx context.Context, salesConvID, productID uuid.UUID) (*storage.NegotiationState, error) {
	var s storage.NegotiationState
	if err := m.DB.Where("sales_conversation_id = ? AND product_id = ?", salesConvID, productID).First(&s).Error; err == nil {
		return &s, nil
	} else if err == gorm.ErrRecordNotFound {
		s = storage.NegotiationState{SalesConversationID: salesConvID, ProductID: productID, Status: "idle"}
		if err := m.DB.Create(&s).Error; err != nil {
			return nil, err
		}
		return &s, nil
	} else {
		return nil, err
	}
}

// RecordCustomerOffer updates last customer offer and same-offer counting
// Note: same_offer_count increments only if flexibilityAttempted && aiConcessionCount > 0
func (m *Manager) RecordCustomerOffer(ctx context.Context, salesConvID, productID uuid.UUID, offer float64) (*storage.NegotiationState, error) {
	s, err := m.GetOrCreateState(ctx, salesConvID, productID)
	if err != nil {
		return nil, err
	}

	// Determine if same offer as last
	if s.LastCustomerOffer == offer && s.FlexibilityAttempted && s.AIConcessionCount > 0 {
		s.SameOfferCount += 1
	} else {
		s.SameOfferCount = 1
	}
	s.LastCustomerOffer = offer
	// If currently idle, move to negotiating
	if s.Status == "idle" {
		s.Status = "negotiating"
	}

	if err := m.DB.Save(s).Error; err != nil {
		return nil, err
	}
	return s, nil
}

// RecordAIConcession records an AI concession (price change), increments AIConcessionCount
func (m *Manager) RecordAIConcession(ctx context.Context, salesConvID, productID uuid.UUID, aiOffer float64) (*storage.NegotiationState, error) {
	s, err := m.GetOrCreateState(ctx, salesConvID, productID)
	if err != nil {
		return nil, err
	}
	s.AIConcessionCount += 1
	// After concession, stay in negotiating (or move to final if reach max; client code decides)
	if s.Status == "idle" {
		s.Status = "negotiating"
	}
	if err := m.DB.Save(s).Error; err != nil {
		return nil, err
	}
	return s, nil
}

// MarkFlexibilityAttempted sets the flag that a non-price lever was attempted
func (m *Manager) MarkFlexibilityAttempted(ctx context.Context, salesConvID, productID uuid.UUID) error {
	s, err := m.GetOrCreateState(ctx, salesConvID, productID)
	if err != nil {
		return err
	}
	s.FlexibilityAttempted = true
	return m.DB.Save(s).Error
}

// CheckEscalationEligibility evaluates the exact escalation conditions and returns (eligible, reason)
func (m *Manager) CheckEscalationEligibility(ctx context.Context, salesConvID, productID uuid.UUID, customerOffer float64, buyingIntent bool, proximityPct float64, maxConcessions int, productMinPrice float64) (bool, string, error) {
	s, err := m.GetOrCreateState(ctx, salesConvID, productID)
	if err != nil {
		return false, "", err
	}
	// Conditions:
	// 1) persistence reached
	if s.SameOfferCount < 3 {
		return false, "persistence_not_reached", nil
	}
	// 2) offer proximity to min price
	if productMinPrice <= 0 {
		return false, "no_product_min_price", nil
	}
	if customerOffer < productMinPrice*proximityPct {
		// not close enough
		return false, "proximity_not_met", nil
	}
	// 3) buying intent
	if !buyingIntent {
		return false, "no_buying_intent", nil
	}
	// 4) AI exhausted flexibility (ai_concession_count >= max_concessions)
	if s.AIConcessionCount < maxConcessions {
		return false, "ai_concessions_not_exhausted", nil
	}
	// All conditions met -> update status to escalation_eligible
	s.Status = "escalation_eligible"
	if err := m.DB.Save(s).Error; err != nil {
		return false, "save_failed", err
	}
	return true, "escalation_eligible_all_conditions", nil
}

// ExecuteEscalation will create an EscalationEvent, lock AI price changes, and mark state as escalated
func (m *Manager) ExecuteEscalation(ctx context.Context, tenantID uuid.UUID, salesConvID, productID uuid.UUID, listPrice, minPrice float64, customerOffers []float64, aiOffers []float64, reason string) (*storage.EscalationEvent, error) {
	// create event
	custJSON, _ := json.Marshal(customerOffers)
	aiJSON, _ := json.Marshal(aiOffers)
	e := &storage.EscalationEvent{
		TenantID:            tenantID,
		SalesConversationID: salesConvID,
		ProductID:           productID,
		ListPrice:           listPrice,
		MinPrice:            minPrice,
		CustomerOffers:      string(custJSON),
		AIOffers:            string(aiJSON),
		Reason:              reason,
		Status:              "pending",
	}
	if err := m.DB.Create(e).Error; err != nil {
		return nil, err
	}
	// lock negotiation state
	if s, err := m.GetOrCreateState(ctx, salesConvID, productID); err == nil {
		s.LockAIPriceChanges = true
		s.Status = "escalated"
		if err := m.DB.Save(s).Error; err != nil {
			m.Logger.Error("failed to save negotiation state during escalation", zap.Error(err))
		}
	} else {
		m.Logger.Error("failed to get negotiation state during execute escalation", zap.Error(err))
	}
	return e, nil
}

// Helper: convert []float64 to json string - used above if needed (kept inline for brevity)

func (m *Manager) String() string {
	return fmt.Sprintf("NegotiationManager(db)")
}
