package guards

import (
	"errors"

	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/types"
)

var ErrPaymentBeforeLock = errors.New("cannot request payment before price lock")

// GuardPaymentAfterPriceLock ensures payment is only requested when price is finalized.
func GuardPaymentAfterPriceLock(ctx types.ResolvedContext) error {
	if ctx.Order == nil {
		return nil // No order, no payment issue yet (other guards handle missing order)
	}

	// Logic mirrors state.GuardPaymentAfterPriceLock but operates on ResolvedContext
	if ctx.Order.Payment.Status == domain.PaymentRequested || ctx.Order.Payment.Status == domain.PaymentPending {
		// Using the domain constants.
		// If payment is active, we check if we were locked.
		if ctx.Order.Negotiation.Phase != domain.NegotiationLocked && ctx.Order.Negotiation.Phase != domain.NegotiationAccepted && ctx.Order.Negotiation.Phase != domain.NegotiationNone {
			return ErrPaymentBeforeLock
		}
	}
	return nil
}
