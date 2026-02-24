package state

import (
	"errors"

	"github.com/similadayo/brancr/internal/domain"
)

var (
	ErrDuplicateOrder           = errors.New("active order already exists for session")
	ErrPaymentUnlock            = errors.New("cannot request payment before price lock")
	ErrNegotationAfterPayment   = errors.New("cannot negotiate after payment requested")
	ErrOrderRecreationForbidden = errors.New("cannot recreate existing order")
)

// GuardSingleOrderPerSession ensures no active order clobbering
func GuardSingleOrderPerSession(sessionID string, existingOrder *domain.OrderState) error {
	if existingOrder != nil {
		return ErrDuplicateOrder
	}
	return nil
}

// GuardPaymentAfterPriceLock ensures we only pay for agreed prices and complete states
func GuardPaymentAfterPriceLock(order domain.OrderState) error {
	// Must be in a Locked negotiation state OR explicit price agreement
	if order.Negotiation.Phase != domain.NegotiationLocked && order.Negotiation.Phase != domain.NegotiationAccepted {
		// Exception: If no negotiation ever happened (Standard Price), it might be valid if order is roughly confirmed?
		// But rigorous rule says: "Payment cannot be requested unless NegotiationPhase == ACCEPTED" (from Phase 1 Invariants if negotiation occurred)
		// For standardized flow without negotiation, Phase might be NONE.
		if order.Negotiation.Phase == domain.NegotiationNone {
			return nil // Allowed for standard purchases
		}
		return ErrPaymentUnlock
	}
	return nil
}

// GuardNoNegotiationAfterPayment ensures we don't change price while paying
func GuardNoNegotiationAfterPayment(order domain.OrderState) error {
	if order.Payment.Status != domain.PaymentNotRequested && order.Payment.Status != domain.PaymentFailed {
		return ErrNegotationAfterPayment
	}
	return nil
}

// GuardNoOrderRecreation ensures immutability of the aggregate root Identity
func GuardNoOrderRecreation(existing domain.OrderState) error {
	if existing.OrderID != "" {
		return ErrOrderRecreationForbidden
	}
	return nil
}
