package events

// Inbound Payloads
type CustomerMessagePayload struct {
	SessionID  string
	CustomerID string
	MerchantID string
	Message    string
	Platform   string
}

// Interpretation Payloads
type IntentPayload struct {
	SessionID  string
	Intent     string
	Confidence float64
}

// Lifecycle Payloads
type OrderInitializedPayload struct {
	OrderID    string
	SessionID  string
	CustomerID string
	MerchantID string
}

type FulfillmentStartedPayload struct {
	OrderID    string
	SessionID  string
	CustomerID string
	MerchantID string
	Method     string // Payment Method
}
