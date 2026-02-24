package domain

type PaymentStatus string

const (
	PaymentNotRequested PaymentStatus = "NOT_REQUESTED"
	PaymentRequested    PaymentStatus = "REQUESTED"
	PaymentPending      PaymentStatus = "PENDING"
	PaymentCompleted    PaymentStatus = "COMPLETED"
	PaymentFailed       PaymentStatus = "FAILED"
)

type PaymentState struct {
	Status        PaymentStatus
	PaymentMethod string
	Reference     string
}
