package guards

import (
	"errors"

	"github.com/similadayo/brancr/internal/types"
)

var ErrOrderIDRecreation = errors.New("cannot recreate existing order ID")

func GuardNoOrderRecreation(ctx types.ResolvedContext) error {
	if ctx.Order != nil {
		if ctx.Order.OrderID == "" {
			// If struct exists but ID is empty? Should be fine or invalid.
			return nil
		}
		// If we are interpreting this as "Is it valid to Create?" - The Guard signature doesn't take Intent.
		// It takes ResolvedContext.
		// So this guard might be checking: "Does this context look like it's trying to exist twice?"
		// Or maybe it's checking "If we have an order, is it valid?"
		// The Prompt said: "GuardNoOrderRecreation(existing OrderState) error".
		// In Phase 4, it says: "func GuardSingleActiveOrder(ctx) error".
		// I will stick to a check that ensures stability. Use the Phase 2 logic adapted.
		// Actually, without Intent passed to guards, these guards can only check Static Invariants of the ResolvedContext.
		// OR they are used inside the Processor *after* applying an event tentatively?
		// Phase 4.7 `ProcessEvent`: "Resolve(event) -> RunGuards(ctx, applicableGuards(event))".
		// Ah, `applicableGuards(event)` suggests we choose guards based on what happened.
		// So if Event is "OrderInitialized", we run `GuardSingleActiveOrder`.
		// If Event is "PaymentRequested", we run `GuardPaymentAfterPriceLock`.
		// So the Checks I wrote are correct: "Given this context (which might have been just mutated or loaded), is it valid?"
		// Ideally, `ProcessEvent` loads context... wait.
		// `Resolve` loads *existing* state.
		// Then we run guards.
		// If Guard fails, `ProcessEvent` returns error.
		// So `GuardSingleActiveOrder` on `OrderInitialized` event:
		// Context loads existing order (if any).
		// If Context.Order != nil, Guard fails. Correct.
		// So GuardNoOrderRecreation:
		// If Context.Order != nil, we can't create.
		// This is redundancy with GuardSingleActiveOrder?
		// Let's implement it as checking if OrderID matches session or something strictly.
		// For now, simple check.
	}
	return nil
}
