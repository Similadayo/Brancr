package domain

type CustomerMemory struct {
	CustomerID string

	Preferences       []string
	PaymentPreference string
	PriceSensitivity  string
	BehavioralNotes   []string
}
