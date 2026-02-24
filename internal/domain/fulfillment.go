package domain

type FulfillmentStatus string

const (
	FulfillmentPending    FulfillmentStatus = "PENDING"
	FulfillmentProcessing FulfillmentStatus = "PROCESSING"
	FulfillmentCompleted  FulfillmentStatus = "COMPLETED"
)

type FulfillmentState struct {
	Status FulfillmentStatus
}
