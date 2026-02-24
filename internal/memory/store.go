package memory

// Store defines the interface for persisting and retrieving long-term memory.
type Store interface {
	Save(record MemoryRecord) error
	GetByCustomer(customerID, merchantID string) ([]MemoryRecord, error)
}
