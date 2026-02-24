package state

type OrderPhase string

const (
	OrderInitialized    OrderPhase = "INITIALIZED"
	OrderNegotiating    OrderPhase = "NEGOTIATING"
	OrderPriceLocked    OrderPhase = "PRICE_LOCKED"
	OrderPaymentPending OrderPhase = "PAYMENT_PENDING"
	OrderConfirmed      OrderPhase = "CONFIRMED"
	OrderCancelled      OrderPhase = "CANCELLED"
)
