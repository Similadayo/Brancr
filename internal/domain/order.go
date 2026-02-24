package domain

import "time"

type OrderID string

type OrderState struct {
	OrderID    OrderID
	MerchantID string
	CustomerID string
	SessionID  string

	Items      []OrderItem
	TotalPrice Money

	Negotiation NegotiationState
	Payment     PaymentState
	Fulfillment FulfillmentState

	CreatedAt time.Time
	UpdatedAt time.Time
}

type OrderItem struct {
	SKU       string
	Name      string
	Quantity  int
	UnitPrice Money
}

type Money struct {
	Amount   int64 // minor unit (kobo, cents)
	Currency string
}
