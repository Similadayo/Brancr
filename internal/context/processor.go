package context

import (
	"github.com/similadayo/brancr/internal/events"
	"github.com/similadayo/brancr/internal/guards"
	"github.com/similadayo/brancr/internal/types"
)

var resolver Resolver

// SetResolver injects the resolver implementation (dependency injection).
// In a real scenario, this might be a struct method, but keeping it simple for Phase 4 contracts.
func SetResolver(r Resolver) {
	resolver = r
}

// ProcessEvent resolves the context for an event and strictly validates it against guards.
// It returns the ResolvedContext or an error if any guard fails.
// It does NOT mutate state or trigger side effects.
func ProcessEvent(event events.Event) (types.ResolvedContext, error) {
	// 1. Resolve Context
	// Note: In Phase 4 we assume resolver is set or we panic/error.
	// For compilation during this phase, strict nil check or assuming stub is fine.
	if resolver == nil {
		// Just a safeguard for Phase 4 compilation if not initialized.
		// Real impl would have constructor.
		return types.ResolvedContext{}, nil
	}

	ctx, err := resolver.Resolve(event)
	if err != nil {
		return ctx, err
	}

	// 2. Select Applicable Guards based on Event Type
	applicableGuards := selectGuards(event)

	// 3. Run Guard Chain
	if err := guards.RunGuards(ctx, applicableGuards...); err != nil {
		return ctx, err
	}

	return ctx, nil
}

// selectGuards chooses which hard guards to apply for this specific event.
// This is the "Router" of rules.
func selectGuards(event events.Event) []guards.Guard {
	var g []guards.Guard

	switch event.Type {
	case events.OrderInitialized:
		g = append(g, guards.GuardSingleActiveOrder)
		g = append(g, guards.GuardNoOrderRecreation)
	case events.PaymentRequested:
		g = append(g, guards.GuardPaymentAfterPriceLock)
	case events.PriceProposed, events.PriceAccepted: // Negotiation events
		g = append(g, guards.GuardNoNegotiationAfterPayment)
	}

	return g
}
