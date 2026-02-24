package memory

import "time"

// MemoryRecord represents a single, atomic fact about a customer.
// It is derived from confirmed Observation, never from raw chat.
type MemoryRecord struct {
	CustomerID string
	MerchantID string

	Key   string
	Value string

	Confidence float64 // 0.0 to 1.0 (Probabilistic)
	Source     string  // "OBSERVED", "EXPLICIT"
	CreatedAt  time.Time
}
