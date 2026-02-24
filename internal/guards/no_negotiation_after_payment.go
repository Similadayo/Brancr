package guards

import (
	"errors"

	"github.com/similadayo/brancr/internal/domain"
	"github.com/similadayo/brancr/internal/types"
)

var ErrNegotiationAfterPayment = errors.New("cannot negotiate after payment requested")

func GuardNoNegotiationAfterPayment(ctx types.ResolvedContext) error {
	if ctx.Order == nil {
		return nil
	}

	// If Payment is requested/pending/completed, we should NOT be in Negotiation phase effectively.
	// But this guard checks if the STATE is valid.
	// If payment is active...
	if ctx.Order.Payment.Status != domain.PaymentNotRequested && ctx.Order.Payment.Status != domain.PaymentFailed {
		// ...and we attempt negotiation? (This guard validates the static context state? Or implies a check against intent?)
		// Phase 4 says "Validate whether an action is allowed".
		// But RunGuards takes (ctx). It validates constraints on the loaded context.
		// "GuardNoNegotiationAfterPayment" implies: The Context shouldn't have both "Payment Active" AND "Negotiation Active".
		// Let's check for that invalid state.
		if ctx.Order.Negotiation.Phase == domain.NegotiationStarted || ctx.Order.Negotiation.Phase == domain.NegotiationCountered {
			return ErrNegotiationAfterPayment
		}
	}
	return nil
}
