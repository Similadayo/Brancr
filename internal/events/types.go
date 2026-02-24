package events

type EventType string

const (
	// Inbound
	CustomerMessageReceived EventType = "CUSTOMER_MESSAGE_RECEIVED"

	// Interpretation
	IntentIdentified EventType = "INTENT_IDENTIFIED"

	// Order lifecycle
	OrderInitialized EventType = "ORDER_INITIALIZED"
	PriceProposed    EventType = "PRICE_PROPOSED"
	PriceAccepted    EventType = "PRICE_ACCEPTED"
	OrderCancelled   EventType = "ORDER_CANCELLED"

	// Payment
	PaymentRequested EventType = "PAYMENT_REQUESTED"
	PaymentCompleted EventType = "PAYMENT_COMPLETED"
	PaymentFailed    EventType = "PAYMENT_FAILED"

	// Fulfillment
	FulfillmentStarted   EventType = "FULFILLMENT_STARTED"
	FulfillmentCompleted EventType = "FULFILLMENT_COMPLETED"

	// Conversational
	ConversationAcknowledged EventType = "CONVERSATION_ACKNOWLEDGED"
	ConversationResumed      EventType = "CONVERSATION_RESUMED"
	NextStepSuggested        EventType = "NEXT_STEP_SUGGESTED"

	// Qualification
	QualificationAsked EventType = "QUALIFICATION_ASKED"
)
