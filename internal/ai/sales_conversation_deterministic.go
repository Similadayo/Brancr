package ai

import (
	"context"
	"strings"

	"github.com/similadayo/brancr/internal/storage"
)

// ApplyDeterministicSignals updates SalesConversation state from high-signal extracted events.
//
// This is intentionally conservative: it should improve routing and context without
// triggering unsafe downstream automation (e.g., order/payment auto-creation) when
// amounts/details are unknown.
func (m *SalesConversationManager) ApplyDeterministicSignals(
	ctx context.Context,
	tenantID, customerID, conversationID uint,
	detectedIntent string,
	events []Event,
) (*storage.SalesConversation, error) {
	salesConv, err := m.GetOrCreateSalesConversation(ctx, tenantID, customerID, conversationID)
	if err != nil {
		return nil, err
	}

	updates := SalesConversationUpdatesFromSignals(salesConv, detectedIntent, events)
	if len(updates) == 0 {
		return salesConv, nil
	}

	if err := m.DB.Model(&storage.SalesConversation{}).
		Where("id = ?", salesConv.ID).
		Updates(updates).Error; err != nil {
		return nil, err
	}

	// Reload to return up-to-date state.
	if err := m.DB.First(salesConv, salesConv.ID).Error; err != nil {
		return nil, err
	}

	return salesConv, nil
}

func SalesConversationUpdatesFromSignals(
	salesConv *storage.SalesConversation,
	detectedIntent string,
	events []Event,
) map[string]interface{} {
	if salesConv == nil {
		return nil
	}

	updates := map[string]interface{}{}

	if detectedIntent != "" {
		updates["last_intent"] = detectedIntent
	}

	newStage, stageChanged := nextStageFromEvents(salesConv.Stage, events)
	if stageChanged {
		updates["stage"] = newStage
	}

	newLevel, levelChanged := nextCommitmentFromEvents(salesConv.CommitmentLevel, events)
	if levelChanged {
		updates["commitment_level"] = newLevel
	}

	if len(updates) == 0 {
		return nil
	}
	return updates
}

func nextStageFromEvents(current string, events []Event) (string, bool) {
	if len(events) == 0 {
		return "", false
	}

	// Highest priority routing states first.
	if hasEvent(events, EventHumanRequest) {
		return "needs_human", !strings.EqualFold(current, "needs_human")
	}
	if hasEvent(events, EventRefundDemand) {
		return "refund", !strings.EqualFold(current, "refund")
	}
	if hasEvent(events, EventPaymentConfirmation) {
		return "payment_confirmation", !strings.EqualFold(current, "payment_confirmation")
	}

	// Informational and progression states.
	if hasEvent(events, EventPaymentInquiry) {
		return "payment_instructions", !strings.EqualFold(current, "payment_instructions")
	}
	if hasEvent(events, EventOrderSelection) {
		return "commitment_confirmation", !strings.EqualFold(current, "commitment_confirmation")
	}
	if hasEvent(events, EventPODCommitment) {
		return "commitment_confirmation", !strings.EqualFold(current, "commitment_confirmation")
	}
	if hasEvent(events, EventRefundPolicy) {
		return "refund_policy", !strings.EqualFold(current, "refund_policy")
	}
	if hasEvent(events, EventInvoiceRequest) {
		return "billing", !strings.EqualFold(current, "billing")
	}

	return "", false
}

func nextCommitmentFromEvents(current string, events []Event) (string, bool) {
	if len(events) == 0 {
		return "", false
	}

	cur := strings.ToLower(strings.TrimSpace(current))
	// Avoid bumping commitment too far; these fields can trigger automation elsewhere.
	// We only gently move from exploring -> interested on clear selection signals.
	if hasEvent(events, EventOrderSelection) || hasEvent(events, EventPODCommitment) {
		if cur == "" || cur == "exploring" {
			return "interested", true
		}
	}

	return "", false
}
