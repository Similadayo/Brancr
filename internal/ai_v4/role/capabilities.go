package role

// Capabilities represents the resolved set of actions the AI is allowed to perform.
// This is a derived view based on the RoleContract.
type Capabilities struct {
	OrderingEnabled    bool
	NegotiationEnabled bool
	PaymentEnabled     bool
	SalesEnabled       bool
}

// ResolveCapabilities determines what the AI can do based on its contract.
// It is deterministic and read-only.
func ResolveCapabilities(contract *RoleContract) Capabilities {
	if contract == nil {
		return Capabilities{}
	}

	return Capabilities{
		OrderingEnabled:    contract.CanTakeOrders,
		NegotiationEnabled: contract.CanNegotiate && contract.CanSell,
		PaymentEnabled:     contract.CanHandlePayments && !contract.MustEscalateOnPayment,
		SalesEnabled:       contract.CanSell,
	}
}
